package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
)

type draftModel struct {
	count   atomic.Int32
	bad     string
	entered chan struct{}
	wait    bool
}

func (m *draftModel) Chat(ctx context.Context, _ config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	m.count.Add(1)
	if m.entered != nil {
		close(m.entered)
	}
	if m.wait {
		<-ctx.Done()
		return model.ChatResponse{}, ctx.Err()
	}
	if len(tools) != 0 || len(messages) != 2 || !strings.Contains(messages[0].Content, profileDraftContract) {
		panic("improver was not isolated")
	}
	if strings.Contains(messages[1].Content, "UNRELATED_HISTORY_SECRET") {
		panic("history leaked")
	}
	var input struct {
		Base storage.Profile `json:"base"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		return model.ChatResponse{}, err
	}
	data := input.Base.Data
	data.TemplateID = ""
	data.TemplateVersion = 0
	data.SystemPrompt = "面向初学者，用简洁示例解释问题。"
	if m.bad == "resource" {
		data.EnabledSkills = []string{"missing-skill"}
	}
	if m.bad == "workflow" {
		data.Execution.Workflow = "nested"
	}
	proposal := storage.ProfileProposal{Name: "初学者助手", Data: data, Summary: "根据需求简化表达", Reasons: []string{"用户希望面向初学者"}, Limitations: []string{"未执行真实场景验收；采用前检查私人内容"}}
	raw, _ := json.Marshal(proposal)
	if m.bad == "extra" {
		raw = append(raw, []byte("{}")...)
	}
	if m.bad == "unknown" {
		raw = []byte(`{"permissions":{},"name":"bad"}`)
	}
	answer := model.ChatResponse{Message: model.Message{Role: "assistant", Content: string(raw)}, Usage: model.Usage{TotalTokens: 10}}
	if m.bad == "tool" {
		answer.Message.ToolCalls = []model.ToolCall{extToolCall("bad", "local__exec", "{}")}
	}
	return answer, nil
}
func draftFixture(t *testing.T, m *draftModel) (*Service, storage.Conversation, storage.Profile) {
	t.Helper()
	s, _ := newTestService(t, m, &scriptTools{})
	p, err := s.CreateTemplateProfile("daily", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.AddMessage(context.Background(), c.ID, "", storage.RoleUser, "UNRELATED_HISTORY_SECRET"); err != nil {
		t.Fatal(err)
	}
	return s, c, p
}
func TestProfileDraftManualPublicationConflictAndRevert(t *testing.T) {
	m := &draftModel{}
	s, c, p := draftFixture(t, m)
	run, err := s.StartProfileDraft(c.ID, p.ID, "改进为适合初学者的解释助手", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, s, run.ID, "completed")
	current, _ := s.GetProfile(p.ID)
	if current.Revision != p.Revision || current.Data.SystemPrompt != p.Data.SystemPrompt {
		t.Fatal("generated draft modified profile", current)
	}
	drafts, err := s.ListProfileDrafts()
	if err != nil || len(drafts) != 1 || drafts[0].BaseRevision != p.Revision || len(drafts[0].Changes) == 0 || drafts[0].Conversations != 1 {
		t.Fatal(drafts, err)
	}
	d := drafts[0]
	g, err := s.GetTaskGraph(run.ID)
	if err != nil || g.Budget.ModelCalls != 1 || g.Budget.ToolCalls != 0 || m.count.Load() != 1 {
		t.Fatal(g, err)
	}
	proposal := d.Proposal
	proposal.Data.SystemPrompt = "用户手动编辑的候选"
	edited, err := s.EditProfileDraft(d.ID, d.Revision, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyProfileDraft(d.ID, d.Revision); err == nil {
		t.Fatal("stale draft revision accepted")
	}
	adopted, err := s.ApplyProfileDraft(d.ID, edited.Revision)
	if err != nil || adopted.Revision != p.Revision+1 || adopted.Data.SystemPrompt != proposal.Data.SystemPrompt {
		t.Fatal(adopted, err)
	}
	if _, err = s.ApplyProfileDraft(d.ID, edited.Revision); err == nil {
		t.Fatal("duplicate adoption accepted")
	}
	saved, _ := s.store.GetProfileDraft(context.Background(), d.ID)
	restored, err := s.RevertProfileDraft(d.ID, saved.Revision)
	if err != nil || restored.Revision != adopted.Revision+1 || restored.Data.SystemPrompt != p.Data.SystemPrompt {
		t.Fatal(restored, err)
	}
	// A subsequent human edit must never be overwritten by an old candidate.
	run, err = s.StartProfileDraft(c.ID, p.ID, "再次改进", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, s, run.ID, "completed")
	drafts, _ = s.ListProfileDrafts()
	d = drafts[0]
	restored.Data.SystemPrompt = "后续人工改动"
	current, err = s.UpdateProfile(restored)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyProfileDraft(d.ID, d.Revision); err == nil {
		t.Fatal("human edit overwritten")
	}
	after, _ := s.GetProfile(p.ID)
	if after.Revision != current.Revision || after.Data.SystemPrompt != current.Data.SystemPrompt {
		t.Fatal(after)
	}
}
func TestProfileDraftCreateInvalidCancellationAndDisabled(t *testing.T) {
	for _, bad := range []string{"", "tool", "resource", "workflow", "extra", "unknown", "cancel", "disabled"} {
		t.Run(bad, func(t *testing.T) {
			m := &draftModel{bad: bad}
			s, c, p := draftFixture(t, m)
			if bad == "disabled" {
				p.Data.Execution.ImproverEnabled = false
				if _, err := s.UpdateProfile(p); err != nil {
					t.Fatal(err)
				}
				if _, err := s.StartProfileDraft(c.ID, "", "新配置", nil); err == nil {
					t.Fatal("disabled improver executed")
				}
				if m.count.Load() != 0 {
					t.Fatal("model called")
				}
				return
			}
			if bad == "cancel" {
				m.entered = make(chan struct{})
				m.wait = true
			}
			r, err := s.StartProfileDraft(c.ID, "", "创建新配置", nil)
			if err != nil {
				t.Fatal(err)
			}
			want := "failed"
			if bad == "" {
				want = "completed"
			}
			if bad == "cancel" {
				<-m.entered
				if err = s.CancelTask(r.ID); err != nil {
					t.Fatal(err)
				}
				want = "cancelled"
			}
			waitForRun(t, s, r.ID, want)
			s.WaitIdle()
			drafts, err := s.ListProfileDrafts()
			if err != nil {
				t.Fatal(err)
			}
			if bad != "" {
				if len(drafts) != 0 {
					t.Fatal("invalid generation published candidate", drafts)
				}
				return
			}
			if len(drafts) != 1 {
				t.Fatal(drafts)
			}
			generated, err := s.ApplyProfileDraft(drafts[0].ID, drafts[0].Revision)
			if err != nil || generated.ID == p.ID || generated.Revision != 1 {
				t.Fatal(generated, err)
			}
			c2, _ := s.GetConversation(c.ID)
			if c2.ProfileID != p.ID {
				t.Fatal("adoption silently switched conversation")
			}
		})
	}
}
func TestProfileDraftRemoteRefusalSendsNothing(t *testing.T) {
	m := &draftModel{}
	s, c, p := draftFixture(t, m)
	p.Data.Execution.Improver.Model = &config.Model{Provider: "openai", BaseURL: "https://api.openai.com/v1", Name: "test-model"}
	if _, err := s.UpdateProfile(p); err != nil {
		t.Fatal(err)
	}
	r, err := s.StartProfileDraft(c.ID, "", "创建配置", nil)
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, s, r.ID, "failed")
	if m.count.Load() != 0 {
		t.Fatal("unauthorized remote send")
	}
}
