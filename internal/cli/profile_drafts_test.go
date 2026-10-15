package cli

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

type cliDraftModel struct{}

func (cliDraftModel) Chat(_ context.Context, _ config.Model, _ []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	if len(tools) != 0 {
		panic("improver tools leaked")
	}
	p := storage.ProfileProposal{Name: "CLI候选", Data: storage.BlankProfile(), Summary: "创建解释助手", Reasons: []string{"用户指定"}, Limitations: []string{"未验收"}}
	p.Data.SystemPrompt = "简洁解释"
	raw, _ := json.Marshal(p)
	return model.ChatResponse{Message: model.Message{Role: "assistant", Content: string(raw)}, Usage: model.Usage{TotalTokens: 10}}, nil
}
func TestProfileDraftCLI(t *testing.T) {
	client, s := newCLIHost(t, cliDraftModel{}, time.Second)
	p, err := s.CreateTemplateProfile("daily", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if code := dispatch(client, []string{"profiles", "draft", c.ID, "创建解释助手"}); code != 0 {
		t.Fatal(code)
	}
	s.WaitIdle()
	drafts, err := s.ListProfileDrafts()
	if err != nil || len(drafts) != 1 {
		t.Fatal(drafts, err)
	}
	d := drafts[0]
	if code := dispatch(client, []string{"profiles", "drafts", "show", d.ID}); code != 0 {
		t.Fatal(code)
	}
	if code := dispatch(client, []string{"profiles", "drafts", "apply", d.ID}); code == 0 {
		t.Fatal("adopted without inspected revision")
	}
	if code := dispatch(client, []string{"profiles", "drafts", "apply", d.ID, "--revision", "1"}); code != 0 {
		t.Fatal(code)
	}
	if code := dispatch(client, []string{"profiles", "drafts", "apply", d.ID, "--revision", "1"}); code == 0 {
		t.Fatal("duplicate adoption accepted")
	}
	if err := client.Call("drafts.start", map[string]any{"sessionId": c.ID, "request": "配置", "agentId": "obsolete"}, nil); err == nil {
		t.Fatal("obsolete IPC parameter accepted")
	}
}
