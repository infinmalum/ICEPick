package cli

import (
	"context"
	"testing"
	"time"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

type conversationModel struct{}

func (conversationModel) Chat(_ context.Context, _ config.Model, _ []model.Message, _ []model.Tool) (model.ChatResponse, error) {
	return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "模拟服务结果"}}, nil
}

func TestProfileCLI(t *testing.T) {
	client, svc := newCLIHost(t, conversationModel{}, time.Second)
	if code := dispatch(client, []string{"templates", "create", "daily", "CLI场景"}); code != 0 {
		t.Fatal("template creation", code)
	}
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var profile storage.Profile
	for _, p := range profiles {
		if p.Name == "CLI场景" {
			profile = p
		}
	}
	if profile.ID == "" {
		t.Fatal("template profile not stored")
	}
	if code := dispatch(client, []string{"conversations", "create", "--profile", profile.ID}); code != 0 {
		t.Fatal("conversation creation", code)
	}
	items, err := svc.ListConversations()
	if err != nil || len(items) != 1 {
		t.Fatalf("conversations: %+v %v", items, err)
	}
	c := items[0]
	if code := dispatch(client, []string{"conversations", "permissions", c.ID, "--mode", "manual", "--workspace", "/project", "--command", "/bin/sh"}); code != 0 {
		t.Fatal("permission update", code)
	}
	local, err := svc.GetLocalContext(c.ContextID)
	if err != nil || local.Policy.ApprovalMode != "manual" || local.Policy.Permissions.Unrestricted || len(local.Policy.Permissions.Commands) != 1 || local.Policy.Permissions.Commands[0] != "/bin/sh" {
		t.Fatal("permissions not applied", err)
	}
	if code := dispatch(client, []string{"conversations", "start", c.ID, "hello", "--wait"}); code != 0 {
		t.Fatal("simulated task", code)
	}
	history, err := svc.ListMessages(c.ID)
	if err != nil || len(history) != 2 || history[1].Content != "模拟服务结果" {
		t.Fatalf("missing model response: %+v %v", history, err)
	}
	runs, err := svc.ListRuns(c.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs: %+v %v", runs, err)
	}
	var graph storage.TaskGraph
	if err := client.Call("tasks.graph", map[string]any{"runId": runs[0].ID}, &graph); err != nil || graph.Goal != "hello" || graph.RunStatus != "completed" {
		t.Fatalf("IPC root graph: %+v %v", graph, err)
	}
	if err := client.Call("tasks.graph", map[string]any{"runId": "missing"}, &graph); err == nil {
		t.Fatal("missing graph accepted")
	}
	if code := dispatch(client, []string{"tasks", "status", runs[0].ID}); code != 0 {
		t.Fatal("CLI task graph status", code)
	}
	if code := dispatch(client, []string{"profiles", "create", "copy", "--from", profile.ID}); code != 0 {
		t.Fatal("copy flag", code)
	}
	if code := dispatch(client, []string{"profiles", "restore", "--id", profile.ID, "--group", "prompt"}); code != 0 {
		t.Fatal("template restore", code)
	}
	if code := dispatch(client, []string{"profiles", "delete", profile.ID}); code != 0 {
		t.Fatal("archive in-use profile", code)
	}
	archived, err := svc.GetConversation(c.ID)
	if err != nil || !archived.ProfileArchived {
		t.Fatalf("archive: %+v %v", archived, err)
	}
	if code := dispatch(client, []string{"conversations", "show", c.ID}); code != 0 {
		t.Fatal("archived history", code)
	}
}

func TestTemplateGuidesAndWritingCLI(t *testing.T) {
	client, svc := newCLIHost(t, conversationModel{}, time.Second)
	for _, id := range []string{"daily", "writing", "research", "development"} {
		if code := dispatch(client, []string{"templates", "show", id}); code != 0 {
			t.Fatal(id, code)
		}
	}
	if code := dispatch(client, []string{"templates", "show", "missing"}); code == 0 {
		t.Fatal("missing template accepted")
	}
	if code := dispatch(client, []string{"templates", "create", "writing", "写作场景"}); code != 0 {
		t.Fatal(code)
	}
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	var p storage.Profile
	for _, item := range profiles {
		if item.Name == "写作场景" {
			p = item
		}
	}
	if code := dispatch(client, []string{"conversations", "create", "--profile", p.ID}); code != 0 {
		t.Fatal(code)
	}
	items, err := svc.ListConversations()
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	guide, _ := storage.ProfileTemplateByID("writing")
	if code := dispatch(client, []string{"conversations", "start", items[0].ID, guide.Guide.Example, "--wait"}); code != 0 {
		t.Fatal(code)
	}
	runs, err := svc.ListRuns(items[0].ID)
	if err != nil || len(runs) != 1 || runs[0].Status != "completed" {
		t.Fatal(runs, err)
	}
	reviews, err := svc.ListTaskReviews(runs[0].ID)
	if err != nil || len(reviews) != 0 {
		t.Fatal("on demand scene reviewed automatically", reviews, err)
	}
}
