package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

func readFileDef() []model.Tool {
	definition := model.Tool{Type: "function"}
	definition.Function.Name = "local__read_file"
	return []model.Tool{definition}
}

func writeFileForTest(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

// delegateCall builds a local__delegate_task model tool call.
func delegateCall(id, agentRef, task, materials string) model.ToolCall {
	call := model.ToolCall{ID: id, Type: "function"}
	call.Function.Name = "local__delegate_task"
	args := map[string]string{"task": task}
	if agentRef != "" {
		args["agent"] = agentRef
	}
	if materials != "" {
		args["materials"] = materials
	}
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	call.Function.Arguments = string(raw)
	return call
}

// setupDelegation wires the standard two-agent delegation fixture: the
// default agent (semi) may delegate to a freshly created target agent, which
// runs with its own (automatic) policy on the same default profile. The
// returned chat handle lets a test install its script after both agents and
// their ids are known.
func setupDelegation(t *testing.T) (*Service, *storage.Store, string, storage.Agent) {
	t.Helper()
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{respond: func(string, string) (string, error) { return "", nil }})
	target, err := svc.CreateAgent("target-worker", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) {
		p.ApprovalMode = "semi"
		p.DelegationTargets = []string{target.ID}
	})
	setAgentPolicy(t, svc, target.ID, func(p *storage.AgentPolicy) {
		p.ApprovalMode = "automatic"
	})
	session, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, session.ID, target
}

func waitForChildRun(t *testing.T, svc *Service, parentRunID string) storage.Run {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		children, err := svc.store.ListChildRuns(context.Background(), parentRunID)
		if err != nil {
			t.Fatal(err)
		}
		if len(children) > 0 {
			return children[0]
		}
		if time.Now().After(deadline) {
			t.Fatal("委派调用未产生子任务运行")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// approveParentDelegation waits for the parent's delegate call to reach
// pending and approves it, releasing the child run.
func approveParentDelegation(t *testing.T, svc *Service, sessionID string) {
	t.Helper()
	call := waitForCallState(t, svc, storage.DefaultAgentID, sessionID, storage.CallPending)
	if call.ToolName != "local__delegate_task" {
		t.Fatalf("expected delegate call, got %s", call.ToolName)
	}
	if _, err := svc.ApproveToolCall(storage.DefaultAgentID, call.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDelegationRunsIndependentSubtask(t *testing.T) {
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		// Parent turn 1: delegate to the target by name.
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", "target-worker", "统计总数", "输入数据：1,2,3")}}}, nil
		},
		// Child turn: sees the delegation note and only the passed
		// task/materials, then answers.
		func(messages []model.Message) (model.ChatResponse, error) {
			if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
				t.Fatalf("child context: %+v", messages)
			}
			if !strings.Contains(messages[0].Content, "委派说明") {
				t.Fatalf("child system prompt missing delegation note: %q", messages[0].Content)
			}
			if !strings.Contains(messages[1].Content, "统计总数") || !strings.Contains(messages[1].Content, "输入数据：1,2,3") {
				t.Fatalf("child user message missing task or materials: %+v", messages[1])
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务完成：总数 6"}}, nil
		},
		// Parent turn 2: receives the child's result as the tool output.
		func(messages []model.Message) (model.ChatResponse, error) {
			last := messages[len(messages)-1]
			if last.Role != "tool" || !strings.Contains(last.Content, "子任务完成：总数 6") {
				t.Fatalf("parent did not receive child result: %+v", last)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "总数是 6"}}, nil
		},
	}}
	svc, store, sessionID, target := setupDelegation(t)
	svc.chat = chat
	run, err := svc.StartAgentTask(storage.DefaultAgentID, sessionID, "请算出总数")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, sessionID)
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")

	parentRun, err := store.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parentRun.Depth != 0 || parentRun.ParentRunID != "" {
		t.Fatalf("parent run linkage: %+v", parentRun)
	}
	child := waitForChildRun(t, svc, run.ID)
	if child.ParentRunID != run.ID || child.Depth != 1 || child.AgentID != target.ID || child.Status != "completed" {
		t.Fatalf("child run: %+v", child)
	}
	childSession, err := store.GetSession(context.Background(), child.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if childSession.Kind != storage.SessionKindDelegation || childSession.AgentID != target.ID {
		t.Fatalf("child session: %+v", childSession)
	}
	// The delegation session is hidden from the agent's normal list.
	sessions, err := store.ListAgentSessions(context.Background(), target.ID)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("delegation session leaked into normal list: %+v %v", sessions, err)
	}
	all, err := store.ListAgentSessionsAll(context.Background(), target.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("delegation session missing from full list: %+v %v", all, err)
	}
}

func TestDelegatedChildHasNoMemoryAccess(t *testing.T) {
	svc, _, _, target := setupDelegation(t)
	// Give the TARGET agent a long-term memory: the child runs on the
	// target's profile but must never see it, and the child cannot write.
	if _, err := svc.store.AddAgentMemory(context.Background(), target.ID, "机密偏好：总是先汇报上级", storage.MemorySourceUser, "", storage.ScopePersonal); err != nil {
		t.Fatal(err)
	}
	memoryCall := model.ToolCall{ID: "m1", Type: "function"}
	memoryCall.Function.Name = "local__memory_save"
	memoryCall.Function.Arguments = `{"content":"子任务想偷记一条"}`
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "做点事", "")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			if strings.Contains(messages[0].Content, "机密偏好") {
				t.Fatalf("child context leaked target memory: %q", messages[0].Content)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{memoryCall}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务结束"}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat
	session, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAgentTask(storage.DefaultAgentID, session.ID, "委派一个任务")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session.ID)
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
	child := waitForChildRun(t, svc, run.ID)
	// The memory_save attempt must have been denied: the tool is not
	// offered to delegated subtasks at all.
	calls, err := svc.store.ListSessionToolCalls(context.Background(), child.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].State != storage.CallFailed || !strings.Contains(calls[0].Error, "未提供的 MCP 工具") {
		t.Fatalf("child memory_save not denied: %+v", calls)
	}
	// Nothing was stored under the target agent.
	memories, err := svc.store.ListAgentMemories(context.Background(), target.ID, 10)
	if err != nil || len(memories) != 1 {
		t.Fatalf("target memories changed: %+v %v", memories, err)
	}
}

func TestDelegationApprovalTakesStrictestMode(t *testing.T) {
	svc, _, _, target := setupDelegation(t)
	// Parent is manual (setup set semi); target is automatic. The child must
	// take max(manual, automatic) = manual and hold its own tool call for
	// approval even though the target's own mode would auto-execute it.
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) {
		p.ApprovalMode = "manual"
	})
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "查时间", "")}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{toolCall("c1")}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务完成"}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务完成"}}, nil
		},
	}}
	svc.chat = chat
	svc.tools = &scriptTools{defs: timeToolDef(), respond: func(string, string) (string, error) { return "12:00", nil }}
	session, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAgentTask(storage.DefaultAgentID, session.ID, "委派查时间")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session.ID)
	child := waitForChildRun(t, svc, run.ID)
	// The child's own pure tool call must be pending: manual mode inherited
	// from the parent task overrides the target's automatic policy.
	deadline := time.Now().Add(3 * time.Second)
	var childCall storage.ToolCall
	for {
		calls, err := svc.store.ListSessionToolCalls(context.Background(), child.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) > 0 && calls[0].State == storage.CallPending {
			childCall = calls[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child call never pending: %+v", calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childCall.CallID != "c1" {
		t.Fatalf("unexpected child call: %+v", childCall)
	}
	// The parent's approval of the delegation does NOT cover the child's
	// call: it needs its own explicit approval.
	if _, err := svc.ApproveToolCall(target.ID, childCall.ID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
}

// blockingChildChat blocks in the child turn until its context is cancelled,
// simulating a long subtask whose cancellation can be observed.
type blockingChildChat struct {
	parent       *scriptModel
	childStarted chan struct{}
}

func (m *blockingChildChat) Chat(ctx context.Context, source config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	if m.parent.next == 1 {
		close(m.childStarted)
		<-ctx.Done()
		return model.ChatResponse{}, ctx.Err()
	}
	return m.parent.Chat(ctx, source, messages, tools)
}

func TestDelegationCancelPropagates(t *testing.T) {
	svc, _, _, target := setupDelegation(t)
	childStarted := make(chan struct{})
	chat := &blockingChildChat{childStarted: childStarted, parent: &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "长任务", "")}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "不应到达"}}, nil
		},
	}}}
	svc.chat = chat
	session, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAgentTask(storage.DefaultAgentID, session.ID, "启动一个会被取消的委派")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session.ID)
	child := waitForChildRun(t, svc, run.ID)
	<-childStarted
	if err := svc.CancelTask(storage.DefaultAgentID, run.ID); err != nil {
		t.Fatal(err)
	}
	svc.WaitIdle()
	// Parent and child are both terminal; no subtask survives the parent.
	parentRun, err := svc.GetRun(storage.DefaultAgentID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parentRun.Status != "cancelled" {
		t.Fatalf("parent run: %+v", parentRun)
	}
	finalChild, err := svc.store.GetRun(context.Background(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalChild.Status != "cancelled" {
		t.Fatalf("child run not cancelled: %+v", finalChild)
	}
	children, err := svc.store.ListChildRuns(context.Background(), run.ID)
	if err != nil || len(children) != 1 || children[0].Status != "cancelled" {
		t.Fatalf("surviving children: %+v %v", children, err)
	}
}

func TestDelegationRefusals(t *testing.T) {
	svc, _, sessionID, target := setupDelegation(t)
	// Depth 0: delegation enabled but no depth budget — the tool is not
	// even offered to the run.
	profile, err := svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.Delegation.MaxDepth = 0
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "任务一", "")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			last := messages[len(messages)-1]
			if !strings.Contains(last.Content, "未提供的 MCP 工具") {
				t.Fatalf("depth budget not enforced: %+v", last)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat
	run, err := svc.StartAgentTask(storage.DefaultAgentID, sessionID, "深度受限的委派")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")

	// Restore depth, shrink the allowlist to an outsider only: delegating
	// to target (outside the list) must be refused by the gate.
	outsider, err := svc.CreateAgent("outsider", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile, err = svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.Delegation.MaxDepth = 1
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) {
		p.DelegationTargets = []string{outsider.ID}
	})
	session2, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	chat2 := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d2", target.ID, "任务二", "")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			last := messages[len(messages)-1]
			if !strings.Contains(last.Content, "不在该 Agent 的委派允许列表") {
				t.Fatalf("allowlist refusal missing: %+v", last)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat2
	run2, err := svc.StartAgentTask(storage.DefaultAgentID, session2.ID, "列表外目标")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run2.ID, "completed")
}

func TestDelegationDuplicateTaskRefused(t *testing.T) {
	svc, _, sessionID, target := setupDelegation(t)
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		// First delegation goes through.
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "同样的任务", "")}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务结果"}}, nil
		},
		// Second delegation with the identical task is refused.
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d2", target.ID, "同样的任务", "")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			last := messages[len(messages)-1]
			if !strings.Contains(last.Content, "重复任务") {
				t.Fatalf("duplicate refusal missing: %+v", last)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat
	run, err := svc.StartAgentTask(storage.DefaultAgentID, sessionID, "重复委派")
	if err != nil {
		t.Fatal(err)
	}
	// Two delegation calls, two approvals: the second call must get its own
	// approval binding.
	approveParentDelegation(t, svc, sessionID)
	approveParentDelegation(t, svc, sessionID)
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
}

func TestDelegationCycleRefused(t *testing.T) {
	svc, _, sessionID, target := setupDelegation(t)
	// Depth 2 and mutual allowlists: default → target → default is a cycle.
	profile, err := svc.GetProfile(storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	profile.Data.Delegation.MaxDepth = 2
	if _, err := svc.UpdateProfile(profile); err != nil {
		t.Fatal(err)
	}
	setAgentPolicy(t, svc, target.ID, func(p *storage.AgentPolicy) {
		p.ApprovalMode = "semi"
		p.DelegationTargets = []string{storage.DefaultAgentID}
	})
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "往深处走", "")}}}, nil
		},
		// Child (target agent) delegates back to the default agent.
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d2", storage.DefaultAgentID, "绕回去", "")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			last := messages[len(messages)-1]
			if !strings.Contains(last.Content, "拒绝循环委派") {
				t.Fatalf("cycle refusal missing: %+v", last)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务结束"}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat
	run, err := svc.StartAgentTask(storage.DefaultAgentID, sessionID, "循环委派")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, sessionID)
	// The child's own delegation call runs under the target agent's session;
	// wait for it and approve it so the refusal path is exercised at the
	// gate, not at the approval stage.
	child := waitForChildRun(t, svc, run.ID)
	deadline := time.Now().Add(3 * time.Second)
	for {
		calls, err := svc.store.ListSessionToolCalls(context.Background(), child.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) > 0 && calls[0].State == storage.CallPending {
			if _, err := svc.ApproveToolCall(target.ID, calls[0].ID); err != nil {
				t.Fatal(err)
			}
			break
		}
		if len(calls) > 0 && calls[0].State == storage.CallFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child delegation call never resolved: %+v", calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
	children, err := svc.store.ListChildRuns(context.Background(), child.ID)
	if err != nil || len(children) != 0 {
		t.Fatalf("cycle created a child run: %+v %v", children, err)
	}
}

func TestDelegationWorkspaceIntersection(t *testing.T) {
	svc, _, _, target := setupDelegation(t)
	parentDir := t.TempDir()
	targetDir := t.TempDir()
	// Parent only allows parentDir (and is not unrestricted); the target's
	// own policy allows targetDir. The child's effective workspace is the
	// (empty) intersection, so a target-allowed path is still denied.
	setAgentPolicy(t, svc, storage.DefaultAgentID, func(p *storage.AgentPolicy) {
		p.Permissions.Unrestricted = false
		p.Permissions.WorkspacePaths = []string{parentDir}
	})
	setAgentPolicy(t, svc, target.ID, func(p *storage.AgentPolicy) {
		p.Permissions.WorkspacePaths = []string{targetDir}
	})
	if err := writeFileForTest(filepath.Join(targetDir, "secret.txt"), "内部数据"); err != nil {
		t.Fatal(err)
	}
	readCall := model.ToolCall{ID: "r1", Type: "function"}
	readCall.Function.Name = "local__read_file"
	readCall.Function.Arguments = `{"path":"` + filepath.Join(targetDir, "secret.txt") + `"}`
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "读文件", "")}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{readCall}}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "子任务结束"}}, nil
		},
		func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		},
	}}
	svc.chat = chat
	svc.tools = &scriptTools{defs: readFileDef(), respond: func(string, string) (string, error) { return "内部数据", nil }}
	session, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartAgentTask(storage.DefaultAgentID, session.ID, "委派读文件")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session.ID)
	waitForRun(t, svc, storage.DefaultAgentID, run.ID, "completed")
	child := waitForChildRun(t, svc, run.ID)
	calls, err := svc.store.ListSessionToolCalls(context.Background(), child.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].State != storage.CallFailed || !strings.Contains(calls[0].Error, "路径超出") {
		t.Fatalf("child read not denied by intersection: %+v", calls)
	}
}

// The delegated subtask sends to the target profile's model endpoint under
// the stricter of the two exfiltration authorizations: either the parent or
// the target lacking the grant stops the send.
func TestDelegationExfilIntersection(t *testing.T) {
	svc, _, _, target := setupDelegation(t)
	// A remote profile for the target agent; the parent stays on the local
	// default model.
	remote, err := svc.CreateProfile("remote", "")
	if err != nil {
		t.Fatal(err)
	}
	remote.Data.Model = config.Model{Provider: "openai", Name: "gpt-test"}
	if _, err := svc.UpdateProfile(remote); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SwitchAgentProfile(target.ID, remote.ID); err != nil {
		t.Fatal(err)
	}
	origin := "https://api.openai.com"
	newPhaseScript := func(withChild bool) *scriptModel {
		steps := []func([]model.Message) (model.ChatResponse, error){
			func([]model.Message) (model.ChatResponse, error) {
				return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{delegateCall("d1", target.ID, "远端任务", "")}}}, nil
			},
		}
		if withChild {
			steps = append(steps, func([]model.Message) (model.ChatResponse, error) {
				return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "远端子任务结果"}}, nil
			})
		}
		steps = append(steps, func([]model.Message) (model.ChatResponse, error) {
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "父任务结束"}}, nil
		})
		return &scriptModel{steps: steps}
	}
	svc.chat = newPhaseScript(false)
	// Phase 1: no grants at all — the child must refuse to start.
	session1, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run1, err := svc.StartAgentTask(storage.DefaultAgentID, session1.ID, "委派到远端")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session1.ID)
	waitForRun(t, svc, storage.DefaultAgentID, run1.ID, "completed")
	child1 := waitForChildRun(t, svc, run1.ID)
	if child1.Status != "failed" || !strings.Contains(child1.Error, "未授权向 "+origin) {
		t.Fatalf("child without grants: %+v", child1)
	}
	// Phase 2: only the target has a grant — the parent's missing grant is
	// the stricter side and must still stop the send.
	if _, err := svc.GrantExfil(target.ID, origin, []string{storage.ScopePersonal, storage.ScopeUnknown}); err != nil {
		t.Fatal(err)
	}
	svc.chat = newPhaseScript(false)
	session2, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run2, err := svc.StartAgentTask(storage.DefaultAgentID, session2.ID, "再委派一次")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session2.ID)
	waitForRun(t, svc, storage.DefaultAgentID, run2.ID, "completed")
	child2 := waitForChildRun(t, svc, run2.ID)
	if child2.Status != "failed" || !strings.Contains(child2.Error, "未授权向 "+origin) {
		t.Fatalf("child with only target grant: %+v", child2)
	}
	// Phase 3: both agents grant the endpoint — the child runs and returns
	// its result to the parent.
	if _, err := svc.GrantExfil(storage.DefaultAgentID, origin, []string{storage.ScopePersonal, storage.ScopeUnknown}); err != nil {
		t.Fatal(err)
	}
	svc.chat = newPhaseScript(true)
	session3, err := svc.CreateAgentSession(storage.DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	run3, err := svc.StartAgentTask(storage.DefaultAgentID, session3.ID, "双方授权后再委派")
	if err != nil {
		t.Fatal(err)
	}
	approveParentDelegation(t, svc, session3.ID)
	waitForRun(t, svc, storage.DefaultAgentID, run3.ID, "completed")
	child3 := waitForChildRun(t, svc, run3.ID)
	if child3.Status != "completed" {
		t.Fatalf("child with both grants: %+v", child3)
	}
}
