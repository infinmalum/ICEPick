package storage

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func storageDraftFixture(t *testing.T, s *Store) (Profile, Conversation, Run, ProfileDraft) {
	t.Helper()
	ctx := context.Background()
	p, err := s.CreateTemplateProfile(ctx, "daily", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := s.CreateRunWithOptions(ctx, c.ID, CreateRunOptions{Component: "improver"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetRunStatus(ctx, r.ID, "completed", ""); err == nil {
		t.Fatal("improver completed without candidate")
	}
	data := p.Data
	data.TemplateID = ""
	data.TemplateVersion = 0
	data.SystemPrompt = "candidate"
	d, err := s.SaveProfileDraft(ctx, r.ID, p, "改进说明", ProfileProposal{Name: p.Name, Data: data, Summary: "清晰表达"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyProfileDraft(ctx, d.ID, d.Revision); err == nil {
		t.Fatal("running generator published")
	}
	if err = s.SetRunStatus(ctx, r.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	return p, c, r, d
}
func TestProfileDraftPersistenceAtomicPublicationAndSingleAdoption(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "drafts.db")
	s, err := testOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	p, _, _, d := storageDraftFixture(t, s)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = testOpen(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.GetProfileDraft(ctx, d.ID)
	if err != nil || d.BaseRevision != p.Revision || d.Proposal.Data.SystemPrompt != "candidate" {
		t.Fatal(d, err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER refuse_draft BEFORE UPDATE ON profile_drafts WHEN NEW.state='applied' BEGIN SELECT RAISE(ABORT,'reject publication'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyProfileDraft(ctx, d.ID, d.Revision); err == nil {
		t.Fatal("publication ignored transaction failure")
	}
	after, _ := s.GetProfile(ctx, p.ID)
	if after.Revision != p.Revision || after.Data.SystemPrompt != p.Data.SystemPrompt {
		t.Fatal("partial publication", after)
	}
	draft, _ := s.GetProfileDraft(ctx, d.ID)
	if draft.State != "draft" {
		t.Fatal(draft)
	}
	if _, err = s.db.Exec(`DROP TRIGGER refuse_draft`); err != nil {
		t.Fatal(err)
	}
	var adopted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.ApplyProfileDraft(ctx, d.ID, d.Revision); err == nil {
				adopted.Add(1)
			}
		}()
	}
	wg.Wait()
	if adopted.Load() != 1 {
		t.Fatal("adopted more than once", adopted.Load())
	}
	after, _ = s.GetProfile(ctx, p.ID)
	if after.Revision != p.Revision+1 {
		t.Fatal(after)
	}
	d, _ = s.GetProfileDraft(ctx, d.ID)
	after.Data.Persona = "later human edit"
	if _, err = s.UpdateProfile(ctx, after); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RevertProfileDraft(ctx, d.ID, d.Revision); err == nil {
		t.Fatal("revert overwrote human edit")
	}
}
func TestProfileDraftDiscardAndCancellationPreventAdoption(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_, c, _, d := storageDraftFixture(t, s)
	if err := s.DiscardProfileDraft(ctx, d.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyProfileDraft(ctx, d.ID, d.Revision+1); err == nil {
		t.Fatal("discarded candidate applied")
	}
	r, _, err := s.CreateRunWithOptions(ctx, c.ID, CreateRunOptions{Component: "improver"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetRunStatus(ctx, r.ID, "cancelled", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveProfileDraft(ctx, r.ID, d.Base, "after cancellation", d.Proposal); err == nil {
		t.Fatal("cancelled run saved candidate")
	}
}
