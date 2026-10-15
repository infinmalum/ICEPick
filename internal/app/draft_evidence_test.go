package app

import (
	"context"
	"encoding/json"
	"icepick/internal/config"
	"icepick/internal/model"
	"icepick/internal/storage"
	"strings"
	"testing"
)

type evidenceDraftModel struct {
	target string
	mutate func() error
	forged bool
}

func (m *evidenceDraftModel) Chat(_ context.Context, _ config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	if len(tools) != 0 {
		panic("tools exposed")
	}
	var payload struct {
		Evidence []storage.DraftEvidence `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(messages[1].Content), &payload); err != nil {
		return model.ChatResponse{}, err
	}
	if len(payload.Evidence) != 1 || payload.Evidence[0].RootRunID != m.target || !strings.Contains(messages[1].Content, "SELECTED_TASK_OBSERVATION") || strings.Contains(messages[1].Content, "UNRELATED_HISTORY_SECRET") {
		panic("wrong evidence scope")
	}
	if m.mutate != nil {
		if err := m.mutate(); err != nil {
			return model.ChatResponse{}, err
		}
	}
	p := storage.ProfileProposal{Name: "证据改进", Data: storage.BlankProfile(), Summary: "依据选定任务提出建议", ExpectedChecks: []string{"输出标明未知效果"}, EvidenceRefs: []string{"run:" + m.target}}
	if m.forged {
		p.EvidenceRefs = []string{"run:unselected"}
	}
	raw, _ := json.Marshal(p)
	return model.ChatResponse{Message: model.Message{Role: "assistant", Content: string(raw)}, Usage: model.Usage{TotalTokens: 10}}, nil
}
func evidenceTarget(t *testing.T, s *Service, c storage.Conversation) (storage.Run, storage.ToolCall) {
	t.Helper()
	ctx := context.Background()
	r, _, _, err := s.runs.StartPrepared(ctx, c.ID, s.config.Model, s.skills.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.AddMessage(ctx, c.ID, r.ID, storage.RoleUser, "SELECTED_TASK_OBSERVATION"); err != nil {
		t.Fatal(err)
	}
	_, calls, err := s.store.AddAssistantMessage(ctx, c.ID, r.ID, "", []storage.MessageToolCall{{ID: "unknown", Name: "remote__write", Arguments: "{}"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.BeginToolCall(ctx, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err = s.store.FinishToolCall(ctx, calls[0].ID, storage.CallExecutionUnknown, "", "响应丢失", storage.ScopeCredential); err != nil {
		t.Fatal(err)
	}
	if err = s.runs.Finish(r.ID, "failed", "未知效果"); err != nil {
		t.Fatal(err)
	}
	return r, calls[0]
}
func TestDraftEvidenceIsolationScopesAndFreshness(t *testing.T) {
	for _, mode := range []string{"valid", "changed", "forged"} {
		t.Run(mode, func(t *testing.T) {
			s, c, p := draftFixture(t, &draftModel{})
			target, call := evidenceTarget(t, s, c)
			request, err := s.CreateConversation(p.ID, c.ID, "使用选定证据")
			if err != nil {
				t.Fatal(err)
			}
			m := &evidenceDraftModel{target: target.ID, forged: mode == "forged"}
			s.chat = m
			if mode == "changed" {
				m.mutate = func() error {
					return s.store.ResolveToolCall(context.Background(), call.ID, storage.CallFailed, "", "确认未生效")
				}
			}
			r, err := s.StartProfileDraft(request.ID, "", "依据选定任务改进", []string{target.ID})
			if err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if mode != "valid" {
				want = "failed"
			}
			waitForRun(t, s, r.ID, want)
			_, scopes, err := s.store.SessionDataScopes(context.Background(), request.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(scopes, ","), storage.ScopeCredential) {
				t.Fatal("evidence scope lost", scopes)
			}
			drafts, err := s.ListProfileDrafts()
			if err != nil {
				t.Fatal(err)
			}
			if mode != "valid" {
				if len(drafts) != 0 {
					t.Fatal(drafts)
				}
				return
			}
			if len(drafts) != 1 || len(drafts[0].Evidence) != 1 || drafts[0].Proposal.EvidenceRefs[0] != "run:"+target.ID {
				t.Fatal(drafts)
			}
			if err = s.store.ResolveToolCall(context.Background(), call.ID, storage.CallFailed, "", "确认未生效"); err != nil {
				t.Fatal(err)
			}
			if _, err = s.ApplyProfileDraft(drafts[0].ID, drafts[0].Revision); err == nil {
				t.Fatal("stale task evidence adopted")
			}
		})
	}
}
func TestDraftEvidenceRejectsOtherContextAndRunningTasks(t *testing.T) {
	s, c, p := draftFixture(t, &draftModel{})
	target, _ := evidenceTarget(t, s, c)
	other, err := s.CreateConversation(p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartProfileDraft(other.ID, "", "跨上下文", []string{target.ID}); err == nil {
		t.Fatal("cross context evidence allowed")
	}
	r, _, _, err := s.runs.StartPrepared(context.Background(), c.ID, s.config.Model, s.skills.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartSkillDraft(c.ID, "", "运行中", []string{r.ID}); err == nil {
		t.Fatal("active evidence allowed")
	}
	_ = s.runs.Finish(r.ID, "failed", "fixture")
	if _, err = s.StartProfileDraft(c.ID, "", "重复", []string{target.ID, target.ID}); err == nil {
		t.Fatal("duplicate evidence allowed")
	}
}

func TestDraftDerivedConfigurationKeepsSourceScopesAcrossReuse(t *testing.T) {
	ctx := context.Background()
	s, c, _ := draftFixture(t, &draftModel{})
	target, _ := evidenceTarget(t, s, c)
	s.chat = &evidenceDraftModel{target: target.ID}
	r, err := s.StartProfileDraft(c.ID, "", "依据任务生成", []string{target.ID})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, s, r.ID, "completed")
	drafts, _ := s.ListProfileDrafts()
	profile, err := s.ApplyProfileDraft(drafts[0].ID, drafts[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	clone, err := s.CreateProfile("派生副本", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	clone.Data.Persona = "手动微调"
	clone, err = s.UpdateProfile(clone)
	if err != nil {
		t.Fatal(err)
	}
	newConversation, err := s.CreateConversation(clone.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, _, resolved, err := s.runs.StartPrepared(ctx, newConversation.ID, s.config.Model, s.skills.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err := s.sessionScopes(ctx, newConversation.ID, resolved)
	if err != nil || !strings.Contains(strings.Join(scopes, ","), storage.ScopeCredential) {
		t.Fatal("Profile provenance lost", scopes, err)
	}
	_ = s.runs.Finish(run.ID, "failed", "fixture")
	exported, err := s.ExportProfile(clone.ID)
	if err != nil || len(exported.ReviewNotes) == 0 {
		t.Fatal("share privacy review missing", exported.ReviewNotes, err)
	}
	// A managed Skill carries the same provenance into independent contexts.
	s.chat = &skillDraftModel{}
	r, err = s.StartSkillDraft(c.ID, "", "从选定任务提取方法", []string{target.ID})
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, s, r.ID, "completed")
	skillDrafts, _ := s.ListSkillDrafts()
	copy, err := s.ApplySkillDraft(skillDrafts[0].ID, skillDrafts[0].Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(copy.Scopes, ","), storage.ScopeCredential) {
		t.Fatal("Skill publication lost source scope", copy.Scopes)
	}
	clean, err := s.CreateTemplateProfile("daily", "")
	if err != nil {
		t.Fatal(err)
	}
	clean.Data.EnabledSkills = []string{copy.Name}
	clean, err = s.UpdateProfile(clean)
	if err != nil {
		t.Fatal(err)
	}
	independent, err := s.CreateConversation(clean.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	run, _, resolved, err = s.runs.StartPrepared(ctx, independent.ID, s.config.Model, s.skills.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	scopes, err = s.sessionScopes(ctx, independent.ID, resolved)
	if err != nil || !strings.Contains(strings.Join(scopes, ","), storage.ScopeCredential) {
		t.Fatal("Skill provenance lost on frozen execution", scopes, err)
	}
	_ = s.runs.Finish(run.ID, "failed", "fixture")
}
