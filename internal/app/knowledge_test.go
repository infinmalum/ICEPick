package app

import (
	"context"
	"strings"
	"testing"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

// buildClosedTurnHistory crafts a session with one closed first task and an
// open current task, so summary coverage can be exercised end to end.
func buildClosedTurnHistory(t *testing.T, svc *Service, store *storage.Store) (string, string) {
	t.Helper()
	ctx := context.Background()
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, storage.RoleUser, "第一问"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []storage.MessageToolCall{
		{ID: "c1", Name: "local__current_time", Arguments: "{}"},
	}); err != nil {
		t.Fatal(err)
	}
	call, err := store.FindRunToolCall(ctx, run.ID, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, call.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, call.ID, storage.CallSucceeded, "12:00", "", storage.ScopePublic); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, storage.RoleAssistant, "现在是 12 点"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, storage.RoleUser, "新任务"); err != nil {
		t.Fatal(err)
	}
	return session.ID, run.ID
}

// The run context sends only the uncovered history, carries the summary text
// for the engine to mirror, injects memories (accumulating their scopes for
// the exfiltration gate) and the resumable plan.
func TestRunContextSummaryMemoryAndPlan(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	ctx := context.Background()
	sessionID, _ := buildClosedTurnHistory(t, svc, store)
	profile, err := store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	if !profile.MemoryEnabled || !profile.PlanEnabled {
		t.Fatalf("default profile runtime prefs: %+v", profile.ProfileData)
	}
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) { p.Memory = true })
	// The run reads the live policy together with the profile snapshot.
	profile, err = store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.SaveSessionSummaryCovering(ctx, sessionID, 4, "第一问已询问时间并得到回答"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMemory(storage.DefaultAgentID, "用户偏好深色主题"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddAgentMemory(ctx, storage.DefaultAgentID, "凭据备份放在保险柜", storage.MemorySourceModel, sessionID, storage.ScopeCredential); err != nil {
		t.Fatal(err)
	}
	steps := []storage.PlanStep{{Title: "回答新任务", Status: storage.PlanInProgress}}
	if _, err := store.SetSessionPlan(ctx, sessionID, steps); err != nil {
		t.Fatal(err)
	}

	window, err := svc.buildRunContext(sessionID, profile)
	if err != nil {
		t.Fatal(err)
	}
	// Four entries are covered; only the current task remains.
	if window.summary != "第一问已询问时间并得到回答" {
		t.Fatalf("summary: %q", window.summary)
	}
	if len(window.history) != 2 || window.history[1].Role != "user" || window.history[1].Content != "新任务" {
		t.Fatalf("history after summary: %+v", window.history)
	}
	system := window.history[0].Content
	if !strings.Contains(system, "用户偏好深色主题") || !strings.Contains(system, "凭据备份放在保险柜") {
		t.Fatalf("memories not injected: %q", system)
	}
	if !strings.Contains(system, "回答新任务") {
		t.Fatalf("plan not injected: %q", system)
	}
	// Memory scopes accumulate into the session, so a remote send without a
	// credential grant is refused even though the session itself only read
	// public data.
	hasUser, toolScopes, err := store.SessionDataScopes(ctx, sessionID)
	if err != nil || !hasUser {
		t.Fatalf("scopes: %v %v", toolScopes, err)
	}
	joined := strings.Join(toolScopes, ",")
	if !strings.Contains(joined, storage.ScopeCredential) {
		t.Fatalf("memory scope not accumulated: %v", toolScopes)
	}
	remote := config.Model{Provider: "openai", BaseURL: "https://example.com/v1", Name: "m"}
	if err := svc.exfilCheck(ctx, sessionID, remote, profile); err == nil {
		t.Fatal("memory content bypassed the exfiltration gate")
	}

	// With memory authorization off, nothing is injected and no scope
	// accumulates in a fresh session.
	other, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) { p.Memory = false })
	profile, err = store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	window, err = svc.buildRunContext(other.ID, profile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(window.history[0].Content, "深色主题") {
		t.Fatal("memory injected although the agent revoked it")
	}
	_, toolScopes, err = store.SessionDataScopes(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range toolScopes {
		if scope == storage.ScopeCredential {
			t.Fatalf("memory scope accumulated without injection: %v", toolScopes)
		}
	}
}

// The memory_save tool is offered only to authorized agents, needs a user
// approval in semi mode, and stores the session's data scope with the item.
func TestMemorySaveToolApprovalAndScope(t *testing.T) {
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			call := model.ToolCall{ID: "m1", Type: "function"}
			call.Function.Name = "local__memory_save"
			call.Function.Arguments = `{"content":"记住用户偏好深色主题"}`
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			if !strings.Contains(messages[len(messages)-1].Content, "已保存") {
				t.Fatalf("save result not returned: %+v", messages)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "完成"}}, nil
		},
	}}
	svc, _ := newTestService(t, chat, &scriptTools{})
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) { p.Memory = true })
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartTask(session.ID, "记住这个偏好")
	if err != nil {
		t.Fatal(err)
	}
	call := waitForCallState(t, svc, storage.DefaultAgentID, session.ID, storage.CallPending)
	if _, err := svc.ApproveToolCall(storage.DefaultAgentID, call.ID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
	memories, err := svc.ListMemories(storage.DefaultAgentID)
	if err != nil || len(memories) != 1 {
		t.Fatalf("memories: %+v %v", memories, err)
	}
	// The memory inherits the session's accumulated scope. This session
	// carried model-written tool arguments, which are unclassified input, so
	// the conservative scope is unknown rather than personal.
	if memories[0].Content != "记住用户偏好深色主题" || memories[0].Source != storage.MemorySourceModel || memories[0].Scope != storage.ScopeUnknown {
		t.Fatalf("memory row: %+v", memories[0])
	}

	// Without the agent authorization the tool is never offered; the call
	// comes back as an unprovided-tool denial and nothing is stored.
	chat2 := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			call := model.ToolCall{ID: "m2", Type: "function"}
			call.Function.Name = "local__memory_save"
			call.Function.Arguments = `{"content":"不应该被保存"}`
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			if !strings.Contains(messages[len(messages)-1].Content, "未提供") {
				t.Fatalf("expected denial: %+v", messages)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "了解"}}, nil
		},
	}}
	svc2, _ := newTestService(t, chat2, &scriptTools{})
	session2, err := svc2.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	run2, err := svc2.StartTask(session2.ID, "记住这个偏好")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc2, storage.DefaultAgentID, run2.ID, "completed")
	left, err := svc2.ListMemories(storage.DefaultAgentID)
	if err != nil || len(left) != 0 {
		t.Fatalf("unauthorized memory saved: %+v %v", left, err)
	}
}

// The plan tool needs no approval (pure bookkeeping), persists resumable
// state and shows up in the next run's prompt until the user clears it.
func TestPlanToolPersistsAndReviews(t *testing.T) {
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			call := model.ToolCall{ID: "p1", Type: "function"}
			call.Function.Name = "local__update_plan"
			call.Function.Arguments = `{"steps":[{"title":"查时间","status":"done"},{"title":"汇报","status":"in_progress","note":"等待结果"}]}`
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "完成"}}, nil
		},
	}}
	svc, store := newTestService(t, chat, &scriptTools{})
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartTask(session.ID, "查时间并汇报")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
	plan, err := svc.GetSessionPlan(storage.DefaultAgentID, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Status != storage.PlanDone || plan.Steps[1].Note != "等待结果" {
		t.Fatalf("plan: %+v", plan)
	}
	profile, err := store.ResolvedAgentProfile(context.Background(), storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	window, err := svc.buildRunContext(session.ID, profile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(window.history[0].Content, "查时间") {
		t.Fatal("plan fragment missing from the next run")
	}
	if err := svc.ClearSessionPlan(storage.DefaultAgentID, session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GetSessionPlan(storage.DefaultAgentID, session.ID); err == nil {
		t.Fatal("cleared plan still readable")
	}
}

// The compactor's summary request is an ordinary send: credential-scoped
// history cannot be summarized towards a remote endpoint without a grant,
// while the local endpoint stays exempt.
func TestSessionCompactorGatedByExfil(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	ctx := context.Background()
	sessionID, runID := buildClosedTurnHistory(t, svc, store)
	// Make the covered history carry a credential scope.
	if _, _, err := store.AddAssistantMessage(ctx, sessionID, runID, "", []storage.MessageToolCall{
		{ID: "k1", Name: "local__read_file", Arguments: `{"path":"~/.ssh/id_rsa"}`},
	}); err != nil {
		t.Fatal(err)
	}
	call, err := store.FindRunToolCall(ctx, runID, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, call.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, call.ID, storage.CallSucceeded, "PRIVATE KEY", "", storage.ScopeCredential); err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolvedAgentProfile(ctx, storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	local := config.DefaultModel("lmstudio")
	local.Name = "local-model"
	remote := config.Model{Provider: "openai", BaseURL: "https://example.com/v1", Name: "m"}

	compactor := func(source config.Model) sessionCompactor {
		return sessionCompactor{svc: svc, runID: runID, sessionID: sessionID, chat: guardedChat{
			inner: &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
				func([]model.Message) (model.ChatResponse, error) {
					return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "摘要"}}, nil
				},
			}},
			payloadCheck: func(c context.Context, m []model.Message, tl []model.Tool) error {
				return svc.checkPayload(c, source, profile.AgentID, sessionID, m, tl)
			},
			check: func(c context.Context) error {
				return svc.exfilCheck(c, sessionID, source, profile)
			},
		}}
	}
	older := []model.Message{{Role: "user", Content: "第一问"}, {Role: "tool", ToolCallID: "k1", Content: "PRIVATE KEY"}}
	if _, err := compactor(remote).Summarize(ctx, remote, older); err == nil {
		t.Fatal("credential history summarized towards a remote endpoint")
	}
	if _, err := compactor(local).Summarize(ctx, local, older); err != nil {
		t.Fatalf("local summarization blocked: %v", err)
	}
	// Grant every scope the session accumulated (the older tool result is
	// public, the fresh one credential, and the session carries the
	// conservative unknown from unclassified input) and the remote path opens.
	if _, err := svc.GrantExfil(storage.DefaultAgentID, remote.BaseURL, []string{
		storage.ScopePersonal, storage.ScopeCredential, storage.ScopePublic, storage.ScopeUnknown,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := compactor(remote).Summarize(ctx, remote, older); err != nil {
		t.Fatalf("granted summarization blocked: %v", err)
	}
}

// Memories are agent-owned: one agent cannot list, edit or delete another's.
func TestMemoryServiceIsolation(t *testing.T) {
	svc, _ := newTestService(t, &scriptModel{}, &scriptTools{})
	other, err := svc.CreateAgent("other", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := svc.AddMemory(storage.DefaultAgentID, "我的记忆")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddMemory(other.ID, strings.Repeat("超", storage.MaxMemoryContent+1)); err == nil {
		t.Fatal("oversized memory accepted")
	}
	if list, err := svc.ListMemories(other.ID); err != nil || len(list) != 0 {
		t.Fatalf("cross-agent list: %+v %v", list, err)
	}
	if _, err := svc.UpdateMemory(other.ID, mine.ID, "劫持"); err == nil {
		t.Fatal("cross-agent edit allowed")
	}
	if err := svc.DeleteMemory(other.ID, mine.ID); err == nil {
		t.Fatal("cross-agent delete allowed")
	}
	if err := svc.DeleteMemory(storage.DefaultAgentID, mine.ID); err != nil {
		t.Fatal(err)
	}
	if list, err := svc.ListMemories(storage.DefaultAgentID); err != nil || len(list) != 0 {
		t.Fatalf("delete failed: %+v %v", list, err)
	}
}
