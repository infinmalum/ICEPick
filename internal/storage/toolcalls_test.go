package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "calls.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestToolCallLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, RoleUser, "几点了"); err != nil {
		t.Fatal(err)
	}
	message, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{
		{ID: "call-1", Name: "local__current_time", Arguments: "{}"},
		{ID: "call-2", Name: "local__current_time", Arguments: "{}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.Seq != 2 || len(message.ToolCalls) != 2 {
		t.Fatalf("assistant message: %+v", message)
	}
	if len(calls) != 2 || calls[0].State != CallPending || calls[0].CallID != "call-1" {
		t.Fatalf("pending calls: %+v", calls)
	}
	if calls[0].ID == "" || calls[0].ID == calls[1].ID {
		t.Fatal("operation IDs missing or colliding")
	}

	// Duplicate or empty model call IDs are rejected before anything is
	// dispatched.
	if _, _, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{{ID: "call-1", Name: "x", Arguments: "{}"}, {ID: "call-1", Name: "x", Arguments: "{}"}}); err == nil {
		t.Fatal("duplicate call ID accepted")
	}

	if err := store.BeginToolCall(ctx, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[0].ID); err == nil {
		t.Fatal("double dispatch accepted")
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, CallSucceeded, "12:00", "", ScopePublic); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, CallFailed, "", "again", ""); err == nil {
		t.Fatal("terminal call changed state")
	}

	if err := store.BeginToolCall(ctx, calls[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[1].ID, CallExecutionUnknow, "", "执行中丢失", ""); err != nil {
		t.Fatal(err)
	}
	unresolved, err := store.HasUnresolvedToolCalls(ctx, session.ID)
	if err != nil || !unresolved {
		t.Fatalf("unresolved check: %v %v", unresolved, err)
	}

	if err := store.ResolveToolCall(ctx, calls[1].ID, CallSucceeded, "", "no result"); err == nil {
		t.Fatal("success disposition without result accepted")
	}
	if err := store.ResolveToolCall(ctx, calls[1].ID, CallFailed, "", "用户确认命令未执行"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveToolCall(ctx, calls[1].ID, CallFailed, "", "again"); err == nil {
		t.Fatal("resolved call re-disposition accepted")
	}
	unresolved, err = store.HasUnresolvedToolCalls(ctx, session.ID)
	if err != nil || unresolved {
		t.Fatalf("unresolved after resolution: %v %v", unresolved, err)
	}
	kept, err := store.GetToolCall(ctx, calls[0].ID)
	if err != nil || kept.State != CallSucceeded || kept.Result != "12:00" {
		t.Fatalf("succeeded result not kept: %+v %v", kept, err)
	}
	resolved, err := store.GetToolCall(ctx, calls[1].ID)
	if err != nil || resolved.State != CallFailed || !strings.Contains(resolved.Error, "用户处置") {
		t.Fatalf("disposition not recorded: %+v %v", resolved, err)
	}
	steps, err := store.ListRunToolCalls(ctx, run.ID)
	if err != nil || len(steps) != 2 {
		t.Fatalf("run tool calls: %+v %v", steps, err)
	}
}

// Finishing a run sweeps non-terminal calls in the same transaction: pending
// calls were never dispatched and become cancelled, executing calls may have
// taken effect and become execution_unknown.
func TestFinishRunSweepsNonTerminalCalls(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{
		{ID: "call-1", Name: "local__current_time", Arguments: "{}"},
		{ID: "call-2", Name: "local__current_time", Arguments: "{}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunStatus(ctx, run.ID, "failed", "进程被杀"); err != nil {
		t.Fatal(err)
	}
	first, err := store.GetToolCall(ctx, calls[0].ID)
	if err != nil || first.State != CallCancelled {
		t.Fatalf("pending call not cancelled: %+v %v", first, err)
	}
	second, err := store.GetToolCall(ctx, calls[1].ID)
	if err != nil || second.State != CallExecutionUnknow {
		t.Fatalf("executing call not marked unknown: %+v %v", second, err)
	}
	unresolved, err := store.HasUnresolvedToolCalls(ctx, session.ID)
	if err != nil || !unresolved {
		t.Fatalf("unknown result must block the session: %v %v", unresolved, err)
	}
}

// Recovering after an abrupt exit must converge records into explicit
// states: running runs become interrupted, executing calls become
// execution_unknown, persisted-but-undispatched calls become cancelled, and
// successful results are kept.
func TestRecoverInterruptedConvergesArtifacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "crash.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, RoleUser, "hello"); err != nil {
		t.Fatal(err)
	}
	_, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{
		{ID: "call-1", Name: "local__current_time", Arguments: "{}"},
		{ID: "call-2", Name: "local__current_time", Arguments: "{}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, CallSucceeded, "12:00", "", ScopePublic); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[1].ID); err != nil {
		t.Fatal(err)
	}
	// Simulate the crash: no sweep, no recovery, just close.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.RecoverInterrupted(ctx); err != nil {
		t.Fatal(err)
	}
	recoveredRun, err := store.GetRun(ctx, run.ID)
	if err != nil || recoveredRun.Status != "interrupted" {
		t.Fatalf("run after recovery: %+v %v", recoveredRun, err)
	}
	kept, err := store.GetToolCall(ctx, calls[0].ID)
	if err != nil || kept.State != CallSucceeded || kept.Result != "12:00" {
		t.Fatalf("succeeded result not kept: %+v %v", kept, err)
	}
	unknown, err := store.GetToolCall(ctx, calls[1].ID)
	if err != nil || unknown.State != CallExecutionUnknow {
		t.Fatalf("executing call not marked unknown: %+v %v", unknown, err)
	}
	unresolved, err := store.HasUnresolvedToolCalls(ctx, session.ID)
	if err != nil || !unresolved {
		t.Fatalf("session must be blocked after recovery: %v %v", unresolved, err)
	}
}

// The approval lifecycle: pending → approved binds the decision to the exact
// arguments via a content hash; approved → executing re-verifies the hash so
// tampered arguments cannot ride on an old approval; denial and expiry are
// terminal from pending only.
func TestApprovalLifecycle(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{
		{ID: "call-1", Name: "local__read_file", Arguments: `{"path":"/tmp/a.txt"}`},
		{ID: "call-2", Name: "local__read_file", Arguments: `{"path":"/tmp/b.txt"}`},
		{ID: "call-3", Name: "local__read_file", Arguments: `{"path":"/tmp/c.txt"}`},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.ApproveToolCall(ctx, calls[0].ID, "robot"); err == nil {
		t.Fatal("invalid approval source accepted")
	}
	if err := store.ApproveToolCall(ctx, calls[0].ID, ApprovalUser); err != nil {
		t.Fatal(err)
	}
	approved, err := store.GetToolCall(ctx, calls[0].ID)
	if err != nil || approved.State != CallApproved || approved.ApprovalSource != ApprovalUser || approved.ArgsHash == "" || approved.ApprovedAt == "" {
		t.Fatalf("approval not recorded: %+v %v", approved, err)
	}
	// The dispatch re-verifies the argument hash; a mutated call cannot use
	// the old approval.
	if _, err := store.db.ExecContext(ctx, `UPDATE tool_calls SET arguments = ? WHERE id = ?`, `{"path":"/etc/shadow"}`, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[0].ID); err == nil || !strings.Contains(err.Error(), "重新确认") {
		t.Fatalf("tampered arguments accepted: %v", err)
	}
	// Approving an already-approved call is rejected, not doubled.
	if err := store.ApproveToolCall(ctx, calls[0].ID, ApprovalUser); err == nil {
		t.Fatal("double approval accepted")
	}
	// Denial only applies to pending calls.
	if err := store.DenyToolCall(ctx, calls[0].ID, "不愿意"); err == nil {
		t.Fatal("denial of approved call accepted")
	}
	if err := store.DenyToolCall(ctx, calls[1].ID, ""); err != nil {
		t.Fatal(err)
	}
	denied, err := store.GetToolCall(ctx, calls[1].ID)
	if err != nil || denied.State != CallFailed || !strings.Contains(denied.Error, "用户拒绝") {
		t.Fatalf("denial not recorded: %+v %v", denied, err)
	}
	if err := store.CancelPendingCall(ctx, calls[2].ID, "审批超时，调用未执行"); err != nil {
		t.Fatal(err)
	}
	expired, err := store.GetToolCall(ctx, calls[2].ID)
	if err != nil || expired.State != CallCancelled || !strings.Contains(expired.Error, "审批超时") {
		t.Fatalf("expiry not recorded: %+v %v", expired, err)
	}
	// An untampered approved call dispatches cleanly from approved.
	if err := store.ApproveToolCall(ctx, calls[2].ID, ApprovalAuto); err == nil {
		t.Fatal("approval after expiry accepted")
	}
}
