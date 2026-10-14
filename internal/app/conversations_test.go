package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

func TestConversationWithHTTPModelService(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string          `json:"model"`
			Messages []model.Message `json:"messages"`
		}
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if len(body.Messages) == 0 || body.Model != "component-model" {
			http.Error(w, "wrong model", 400)
			return
		}
		requests <- body.Messages[0].Content
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"HTTP 模拟响应\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	svc, _ := newTestService(t, model.Client{HTTP: server.Client()}, &scriptTools{})
	p, err := svc.CreateTemplateProfile("writing", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Data.Execution.Main.Model = &config.Model{Provider: "lmstudio", BaseURL: server.URL + "/v1", Name: "component-model"}
	p.Data.Execution.Main.SystemPrompt = "HTTP component prompt"
	p, err = svc.UpdateProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.CreateConversation(p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartConversationTask(c.ID, "write")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, c.ContextID, run.ID, "completed")
	if got := <-requests; got != "HTTP component prompt" {
		t.Fatal("model override lost", got)
	}
	history, err := svc.ListMessages(c.ContextID, c.ID)
	if err != nil || len(history) != 2 || history[1].Content != "HTTP 模拟响应" {
		t.Fatalf("protocol result: %+v %v", history, err)
	}
}

func TestProfileFirstTaskUsesComponentAndAuthorizedTool(t *testing.T) {
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func(messages []model.Message) (model.ChatResponse, error) {
			// The component override leads the prompt; host runtime capabilities
			// may follow it without replacing the user configuration.
			if !strings.HasPrefix(messages[0].Content, "component prompt") {
				t.Errorf("component prompt not used: %s", messages[0].Content)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{toolCall("time-1")}}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			if messages[len(messages)-1].Content != "12:00" {
				t.Error("tool result not returned")
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "现在是 12 点"}}, nil
		},
	}}
	tools := &scriptTools{defs: timeToolDef(), respond: func(_, _ string) (string, error) { return "12:00", nil }}
	svc, store := newTestService(t, chat, tools)
	p, err := svc.CreateTemplateProfile("daily", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Data.Execution.Main.SystemPrompt = "component prompt"
	p, err = svc.UpdateProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.CreateConversation(p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartConversationTask(c.ID, "现在几点？")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, c.ContextID, run.ID, "completed")
	if len(tools.calls) != 1 || run.Component != "main" {
		t.Fatalf("execution: %+v %v", run, tools.calls)
	}
	if err := svc.ArchiveProfile(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartConversationTask(c.ID, "继续"); err == nil {
		t.Fatal("deleted configuration executed")
	}
	history, err := svc.ListMessages(c.ContextID, c.ID)
	if err != nil || len(history) == 0 {
		t.Fatal("archive hid transcript", err)
	}
	if _, err := store.GetLocalContext(context.Background(), c.ContextID); err != nil {
		t.Fatal("archive removed permissions", err)
	}
}

func TestChildTranscriptCannotStartOrdinaryTask(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	child, err := store.CreateDelegationSession(context.Background(), storage.DefaultAgentID, "child")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.StartConversationTask(child.ID, "read target memory"); err == nil {
		t.Fatal("child transcript admitted an ordinary root run")
	}
}
