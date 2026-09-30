package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/storage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestSaveLMStudioAndReadStatus(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	svc := &StatusService{
		store: store, configPath: path, config: config.Config{Model: config.DefaultModel("ollama"), MCPServers: []config.MCPServer{{Name: "example", Command: "/usr/bin/example"}}},
		discovery: model.Discovery{Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/v1/models" {
				t.Fatalf("wrong path: %s", req.URL.Path)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"local-model"}]}`)), Header: http.Header{}}, nil
		})}},
	}
	next := config.DefaultModel("lmstudio")
	next.Name = "local-model"
	got, err := svc.SaveModelConfig(next)
	if err != nil || !got.ModelConnected || got.Model.Provider != "lmstudio" || len(got.AvailableModels) != 1 {
		t.Fatalf("status: %+v, %v", got, err)
	}
	saved, err := config.Load(path)
	if err != nil || saved.Model != next || len(saved.MCPServers) != 1 || saved.MCPServers[0].Name != "example" {
		t.Fatalf("persisted config: %+v, %v", saved, err)
	}
}

type taskModel struct {
	started chan struct{}
	block   bool
}

func (m taskModel) Chat(ctx context.Context, _ config.Model, _ []model.Message, _ []model.Tool) (model.ChatResponse, error) {
	if m.started != nil {
		close(m.started)
	}
	if m.block {
		<-ctx.Done()
		return model.ChatResponse{}, ctx.Err()
	}
	return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "你好"}}, nil
}

func TestTaskPersistenceAndCancellation(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := mcptools.NewLocal(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	source := config.DefaultModel("lmstudio")
	source.Name = "test"
	svc := &StatusService{store: store, config: config.Config{Model: source}, runs: agent.NewRuns(store), tools: registry, chat: taskModel{}}
	defer svc.runs.Close()
	session, err := svc.CreateSession()
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartTask(session.ID, "你好")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := svc.GetRun("chat", run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task not completed: %+v", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	history, err := svc.ListMessages("chat", session.ID)
	if err != nil || len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("history: %+v, %v", history, err)
	}
	started := make(chan struct{})
	svc.chat = taskModel{started: started, block: true}
	run, err = svc.StartTask(session.ID, "等待")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if err := svc.CancelTask("chat", run.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetRun("chat", run.ID)
	if err != nil || got.Status != "cancelled" {
		t.Fatalf("cancelled run: %+v, %v", got, err)
	}
}

func TestServiceRejectsOtherAgentSession(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := &StatusService{store: store}
	work, err := svc.CreateAgentSession("work")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ListMessages("chat", work.ID); err == nil {
		t.Fatal("other Agent messages exposed")
	}
	if _, err := svc.ListRuns("chat", work.ID); err == nil {
		t.Fatal("other Agent runs exposed")
	}
	if _, err := svc.StartAgentTask("chat", work.ID, "hello"); err == nil {
		t.Fatal("other Agent task accepted")
	}
	if _, err := svc.ListMessages("work", work.ID); err != nil {
		t.Fatal(err)
	}
}
