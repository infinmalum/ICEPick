package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"icepick/internal/agent"
	"icepick/internal/app"
	"icepick/internal/config"
	"icepick/internal/ipc"
	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

type chatSteps struct {
	mu   sync.Mutex
	next int
}

func (m *chatSteps) Chat(_ context.Context, _ config.Model, _ []model.Message, _ []model.Tool) (model.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	switch m.next {
	case 1:
		call := model.ToolCall{ID: "call-1", Type: "function"}
		call.Function.Name = "extsvc__ping"
		call.Function.Arguments = "{}"
		return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
	case 2:
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "ping 完成"}}, nil
	default:
		return model.ChatResponse{}, errors.New("脚本用尽")
	}
}

type echoTools struct{}

func (echoTools) Definitions(context.Context) ([]model.Tool, error) {
	definition := model.Tool{Type: "function"}
	definition.Function.Name = "extsvc__ping"
	return []model.Tool{definition}, nil
}

func (echoTools) Call(_ context.Context, _, _ string) (string, error) { return "pong", nil }

func newCLIHost(t *testing.T, chat agent.ChatModel, approvalTimeout time.Duration) (*ipc.Client, *app.Service) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "cli.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	catalog, err := skills.Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runs := agent.NewRuns(store)
	t.Cleanup(runs.Close)
	source := config.DefaultModel("lmstudio")
	source.Name = "test-model"
	svc := app.New(app.Options{
		Store: store, Runs: runs, Tools: echoTools{}, Skills: catalog, Chat: chat,
		Config: config.Config{Model: source},
	})
	svc.ApprovalTimeout = approvalTimeout
	dataDir := t.TempDir()
	server, err := ipc.Start(svc, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	client, err := ipc.Connect(dataDir, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client, svc
}

func startSessionTask(t *testing.T, client *ipc.Client) (string, string) {
	t.Helper()
	var session storage.Session
	if err := client.Call("sessions.create", map[string]any{"agentId": storage.DefaultAgentID}, &session); err != nil {
		t.Fatal(err)
	}
	var run storage.Run
	if err := client.Call("tasks.start", map[string]any{"agentId": storage.DefaultAgentID, "sessionId": session.ID, "input": "ping"}, &run); err != nil {
		t.Fatal(err)
	}
	return session.ID, run.ID
}

// 非交互执行遇到审批请求绝不默认放行：请求被报告，调用未被批准也未执行，
// 直到宿主审批超时将其落盘为取消；模型收到拒绝文本后可正常结束运行。
func TestWaitTaskNonInteractiveNeverApproves(t *testing.T) {
	client, _ := newCLIHost(t, &chatSteps{}, 300*time.Millisecond)
	sessionID, runID := startSessionTask(t, client)

	var out bytes.Buffer
	waitTask(client, storage.DefaultAgentID, sessionID, runID, nil, &out)
	text := out.String()
	if !strings.Contains(text, "非交互模式不会自动批准") {
		t.Fatalf("non-interactive notice missing:\n%s", text)
	}
	var calls []storage.ToolCall
	if err := client.Call("calls.list", map[string]any{"agentId": storage.DefaultAgentID, "sessionId": sessionID}, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].State != storage.CallCancelled {
		t.Fatalf("call after timeout: %+v", calls)
	}
	if calls[0].Result != "" || calls[0].ApprovalSource == storage.ApprovalUser {
		t.Fatalf("call was executed or user-approved: %+v", calls[0])
	}
}

// 交互模式通过注入的 prompter 批准调用，任务正常完成。
func TestWaitTaskInteractiveApproval(t *testing.T) {
	client, _ := newCLIHost(t, &chatSteps{}, 5*time.Second)
	sessionID, runID := startSessionTask(t, client)

	prompt := func(call storage.ToolCall) (bool, string) {
		if call.ToolName != "extsvc__ping" {
			t.Fatalf("unexpected call: %+v", call)
		}
		return true, ""
	}
	var out bytes.Buffer
	code := waitTask(client, storage.DefaultAgentID, sessionID, runID, prompt, &out)
	if code != 0 {
		t.Fatalf("waitTask exit = %d, output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "已批准") {
		t.Fatalf("approval missing from output:\n%s", out.String())
	}
	var run storage.Run
	if err := client.Call("runs.get", map[string]any{"agentId": storage.DefaultAgentID, "runId": runID}, &run); err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" {
		t.Fatalf("run status: %+v", run)
	}
	var calls []storage.ToolCall
	if err := client.Call("calls.list", map[string]any{"agentId": storage.DefaultAgentID, "sessionId": sessionID}, &calls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0].State != storage.CallSucceeded || calls[0].Result != "pong" {
		t.Fatalf("call after approval: %+v", calls)
	}
}
