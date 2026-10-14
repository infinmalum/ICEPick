package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

type workerScript struct {
	calls int
	t     *testing.T
}

func (m *workerScript) Chat(_ context.Context, source config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	m.calls++
	switch m.calls {
	case 1:
		if source.Name != "test-model" {
			m.t.Errorf("main default changed: %s", source.Name)
		}
		found := false
		for _, tool := range tools {
			if tool.Function.Name == "local__delegate_task" && strings.Contains(string(tool.Function.Parameters), "writer") {
				found = true
			}
		}
		if !found {
			m.t.Error("worker choices not advertised")
		}
		call := delegateCall("delegate", "", "写作任务", "")
		call.Function.Arguments = `{"worker":"writer","task":"写作任务"}`
		return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
	case 2:
		if source.Name != "old-worker" || !strings.Contains(messages[0].Content, "old prompt") || strings.Contains(messages[0].Content, "new prompt") {
			m.t.Errorf("worker configuration changed: %s %s", source.Name, messages[0].Content)
		}
		if strings.Contains(messages[0].Content, "private memory") {
			m.t.Error("source context memory leaked")
		}
		if !strings.Contains(messages[0].Content, "old description") || strings.Contains(messages[0].Content, "new description") {
			m.t.Error("Skill metadata changed")
		}
		call := model.ToolCall{ID: "skill", Type: "function"}
		call.Function.Name = "local__load_skill"
		call.Function.Arguments = `{"name":"note"}`
		return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
	case 3:
		last := messages[len(messages)-1].Content
		if !strings.Contains(last, "old body") || strings.Contains(last, "new body") {
			m.t.Errorf("Skill body changed: %s", last)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "worker completed"}}, nil
	default:
		if source.Name != "test-model" {
			m.t.Errorf("root model changed: %s", source.Name)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "main completed"}}, nil
	}
}

func TestWorkerPinsProfileAndSkill(t *testing.T) {
	script := &workerScript{t: t}
	svc, store := newTestService(t, script, &scriptTools{})
	root := t.TempDir()
	path := filepath.Join(root, "note", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: note\ndescription: old description\n---\nold body"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, err := skills.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	svc.skills = catalog
	parent, err := svc.CreateTemplateProfile("daily", "parent")
	if err != nil {
		t.Fatal(err)
	}
	source, err := svc.CreateTemplateProfile("daily", "source")
	if err != nil {
		t.Fatal(err)
	}
	source.Data.SystemPrompt = "old prompt"
	source.Data.EnabledSkills = []string{"note"}
	source.Data.Model = config.Model{Provider: "lmstudio", BaseURL: "http://127.0.0.1:1234/v1", Name: "old-worker"}
	source, err = svc.UpdateProfile(source)
	if err != nil {
		t.Fatal(err)
	}
	private, err := svc.CreateAgent("source private", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddAgentMemory(context.Background(), private.ID, "private memory", "user", "", storage.ScopePersonal); err != nil {
		t.Fatal(err)
	}
	parent.Data.Delegation = storage.DelegationPolicy{Enabled: true, MaxDepth: 1}
	parent.Data.EnabledSkills = []string{"note"}
	parent.Data.Execution.WorkerSources = []storage.WorkerSource{{Name: "writer", ProfileID: source.ID}}
	parent, err = svc.UpdateProfile(parent)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := svc.CreateConversation(parent.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.StartConversationTask(conversation.ID, "write")
	if err != nil {
		t.Fatal(err)
	}
	call := waitForCallState(t, svc, conversation.ContextID, conversation.ID, storage.CallPending)
	source.Data.SystemPrompt = "new prompt"
	source.Data.Model.Name = "new-worker"
	if _, err := svc.UpdateProfile(source); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: note\ndescription: new description\n---\nnew body"), 0600); err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	svc.config.Model.Name = "new-global"
	svc.mu.Unlock()
	if _, err := svc.ApproveToolCall(conversation.ContextID, call.ID); err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, conversation.ContextID, run.ID, "completed")
	children, err := store.ListChildRuns(context.Background(), run.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("children: %+v %v", children, err)
	}
	child := children[0]
	if child.AgentID != conversation.ContextID || child.AgentID == private.ID || child.WorkerName != "writer" || child.ProfileRevision != source.Revision {
		t.Fatalf("worker identity/snapshot: %+v", child)
	}
	resources, err := store.RunResources(context.Background(), child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resources.Workers["writer"].Skills[0].SHA256 == "" {
		t.Fatal("missing Skill digest")
	}
	if script.calls != 4 {
		t.Fatal("unexpected model calls", script.calls)
	}
}

func TestWorkerShareHasPortableConfiguration(t *testing.T) {
	svc, _ := newShareService(t, nil)
	root, err := svc.CreateTemplateProfile("daily", "root")
	if err != nil {
		t.Fatal(err)
	}
	source, err := svc.CreateTemplateProfile("writing", "source")
	if err != nil {
		t.Fatal(err)
	}
	root.Data.Delegation = storage.DelegationPolicy{Enabled: true, MaxDepth: 1}
	root.Data.Execution.WorkerSources = []storage.WorkerSource{{Name: "writer", ProfileID: source.ID}}
	root, err = svc.UpdateProfile(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ExportProfile(root.ID); err == nil {
		t.Fatal("dependency included without explicit choice")
	}
	exported, err := svc.ExportProfileIncludingWorkers(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(exported.Data)
	if err != nil {
		t.Fatal(err)
	}
	files, err := readShareFiles(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files["profile.json"]), source.ID) {
		t.Fatal("local dependency identity exported")
	}
	copy, preview, err := svc.ImportProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	slot := copy.Data.Execution.WorkerSources[0]
	if slot.ProfileID != "" || slot.Configuration == nil || slot.Configuration.SystemPrompt != source.Data.SystemPrompt || len(preview.Workers) != 1 {
		t.Fatalf("portable worker: %+v %+v", slot, preview)
	}
	malicious := zipBytes(t, map[string]string{"manifest.json": `{"formatVersion":3,"name":"x"}`, "profile.json": `{"execution":{"workflow":"direct","workerSources":[{"name":"x","profileId":"local-id"}]}}`})
	if _, _, err := svc.ImportProfile(malicious); err == nil {
		t.Fatal("import bound an unreviewed local Profile")
	}
	encoded, err := json.Marshal(copy.Data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "source private") {
		t.Fatal("private data transported")
	}
}

func delegateCall(id, worker, task, materials string) model.ToolCall {
	call := model.ToolCall{ID: id, Type: "function"}
	call.Function.Name = "local__delegate_task"
	raw, _ := json.Marshal(delegateArgs{Worker: worker, Task: task, Materials: materials})
	call.Function.Arguments = string(raw)
	return call
}
