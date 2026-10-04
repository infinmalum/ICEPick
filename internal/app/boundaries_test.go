package app

import (
	"context"
	"errors"
	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/storage"
	"testing"
)

func TestRunGateUsesSnapshotButLivePolicy(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	ctx := context.Background()
	snap, err := store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	g := toolGate{svc: svc, agentID: snap.AgentID, snapshot: &snap}
	blank, err := store.CreateProfile(ctx, "blank", storage.BlankProfile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SwitchAgentProfile(snap.AgentID, blank.ID); err != nil {
		t.Fatal(err)
	}
	req := agent.ToolRequest{Tool: "local__current_time", Arguments: "{}"}
	if d := g.Gate(ctx, req); d.Deny != "" || d.Confirm != "" {
		t.Fatalf("switch altered in-flight capabilities: %+v", d)
	}
	setAgentPolicy(t, svc, snap.AgentID, func(p *storage.AgentPolicy) { p.ApprovalMode = "manual"; p.Permissions.Unrestricted = false })
	if d := g.Gate(ctx, req); d.Confirm == "" {
		t.Fatal("live approval tightening ignored")
	}
	if d := g.Recheck(ctx, agent.ToolRequest{Tool: "local__read_file", Arguments: `{"path":"/etc/passwd"}`}); d.Deny == "" {
		t.Fatal("live path revocation ignored")
	}
	// Existing restrictive grants must not expand when the live policy expands.
	snap.Policy.Permissions = storage.PermissionScope{}
	setAgentPolicy(t, svc, snap.AgentID, func(p *storage.AgentPolicy) { p.Permissions.Unrestricted = true })
	if d := g.Recheck(ctx, agent.ToolRequest{Tool: "local__read_file", Arguments: `{"path":"/etc/passwd"}`}); d.Deny == "" {
		t.Fatal("live relaxation expanded old Run")
	}
}

func TestTransportFailureBlocksFurtherRuns(t *testing.T) {
	calls := 0
	tools := &scriptTools{defs: extToolDef("remote__send"), respond: func(string, string) (string, error) {
		calls++
		return "", &mcptools.UnknownOutcomeError{Err: errors.New("EOF after dispatch")}
	}}
	svc, _ := newTestService(t, toolCallScript(t, extToolCall("send", "remote__send", "{}"), "", "must not continue"), tools)
	session, _ := svc.CreateSession()
	run, err := svc.StartTask(session.ID, "send")
	if err != nil {
		t.Fatal(err)
	}
	call := waitForCallState(t, svc, storage.DefaultAgentID, session.ID, storage.CallPending)
	if _, err = svc.ApproveToolCall(storage.DefaultAgentID, call.ID); err != nil {
		t.Fatal(err)
	}
	svc.WaitIdle()
	list, err := svc.ListToolCalls(storage.DefaultAgentID, session.ID)
	if err != nil || len(list) != 1 || list[0].State != storage.CallExecutionUnknow {
		t.Fatalf("outcome: %+v %v", list, err)
	}
	if _, err = svc.StartTask(session.ID, "retry"); err == nil {
		t.Fatal("unknown outcome allowed another Run")
	}
	if calls != 1 {
		t.Fatal("duplicate dispatch")
	}
	_ = run
}

func TestExfilAgentIsolationAndPayload(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	ctx := context.Background()
	other, err := svc.CreateAgent("other", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	session, _ := svc.CreateSession()
	otherSession, _ := svc.CreateAgentSession(other.ID)
	remote := config.Model{Provider: "openai", BaseURL: "https://example.com/v1", Name: "test"}
	if _, err := svc.GrantExfil(storage.DefaultAgentID, remote.BaseURL, []string{storage.ScopePersonal}); err != nil {
		t.Fatal(err)
	}
	prompt := []model.Message{{Role: "system", Content: "private custom persona"}}
	if err := svc.checkPayload(ctx, remote, storage.DefaultAgentID, session.ID, prompt, nil); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkPayload(ctx, remote, other.ID, otherSession.ID, prompt, nil); err == nil {
		t.Fatal("another Agent inherited grant")
	}
	if err := svc.checkPayload(ctx, remote, storage.DefaultAgentID, session.ID, prompt, extToolDef("remote__private_schema")); err == nil {
		t.Fatal("untrusted schema escaped")
	}
	prompt = append(prompt, model.Message{Role: "assistant", ToolCalls: []model.ToolCall{extToolCall("c", "local__read_file", `{"path":"secret"}`)}})
	if err := svc.checkPayload(ctx, remote, storage.DefaultAgentID, session.ID, prompt, nil); err == nil {
		t.Fatal("unclassified arguments escaped")
	}
	run, _, _ := store.CreateRun(ctx, session.ID)
	_, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []storage.MessageToolCall{{ID: "failed", Name: "local__read_file", Arguments: "{}"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, storage.CallFailed, "", "sensitive error", storage.ScopeCredential); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err := svc.exfilCheck(ctx, session.ID, remote, snap); err == nil {
		t.Fatal("failed tool data escaped")
	}
	if err := svc.RevokeExfil(storage.DefaultAgentID, remote.BaseURL); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkPayload(ctx, remote, storage.DefaultAgentID, session.ID, prompt, nil); err == nil {
		t.Fatal("revocation ignored")
	}
}

func TestLocalContextRestrictionsSurviveProfileSwitch(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	session, _ := svc.CreateSession()
	ctx := context.Background()
	local := config.Model{Provider: "ollama", BaseURL: "http://127.0.0.1:11434/v1"}
	remote := config.Model{Provider: "openai", BaseURL: "https://example.com/v1"}
	if err := svc.checkPayload(ctx, local, storage.DefaultAgentID, session.ID, nil, extToolDef("external__private")); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GrantExfil(storage.DefaultAgentID, remote.BaseURL, []string{storage.ScopePersonal}); err != nil {
		t.Fatal(err)
	}
	p, err := store.CreateProfile(ctx, "new", storage.BlankProfile())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SwitchAgentProfile(storage.DefaultAgentID, p.ID); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err := svc.exfilCheck(ctx, session.ID, remote, snap); err == nil {
		t.Fatal("switch discarded prior unknown input")
	}
	initial := storage.ExfilGrant{AgentID: storage.DefaultAgentID, Scopes: []string{storage.ScopePersonal}}
	if _, err := svc.GrantExfil(storage.DefaultAgentID, remote.BaseURL, []string{storage.ScopePersonal, storage.ScopeUnknown}); err != nil {
		t.Fatal(err)
	}
	if err := svc.exfilCheck(ctx, session.ID, remote, snap); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkInitialGrant(ctx, session.ID, remote, snap, initial); err == nil {
		t.Fatal("authorization expansion affected existing Run")
	}
}
