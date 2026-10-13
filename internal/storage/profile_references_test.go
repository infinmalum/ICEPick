package storage

import (
	"context"
	"testing"
)

func TestProfileReplacementIsRevisionBoundAndAtomic(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	target, err := s.CreateTemplateProfile(ctx, "writing", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateTemplateProfile(ctx, "daily", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Data.Execution.WorkerSources = []WorkerSource{{Name: "writer", ProfileID: target.ID}}
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveProfile(ctx, target.ID); err == nil {
		t.Fatal("referenced Profile silently archived")
	}
	refs, err := s.ProfileReferences(ctx, target.ID)
	if err != nil || len(refs) != 1 {
		t.Fatalf("references: %+v %v", refs, err)
	}
	p.Data.Description = "concurrent edit"
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveProfileReplacing(ctx, target.ID, "", true, refs); err == nil {
		t.Fatal("unreviewed revision replaced")
	}
	refs, err = s.ProfileReferences(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER fail_revision BEFORE INSERT ON profile_revisions BEGIN SELECT RAISE(ABORT,'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveProfileReplacing(ctx, target.ID, "", true, refs); err == nil {
		t.Fatal("partial replacement accepted")
	}
	remaining, _ := s.GetProfile(ctx, p.ID)
	if remaining.Revision != p.Revision || remaining.Data.Execution.WorkerSources[0].ProfileID != target.ID {
		t.Fatal("failed transaction changed dependent config")
	}
	if _, err := s.db.Exec(`DROP TRIGGER fail_revision`); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveProfileReplacing(ctx, target.ID, "", true, refs); err != nil {
		t.Fatal(err)
	}
	remaining, err = s.GetProfile(ctx, p.ID)
	if err != nil || remaining.Data.Execution.WorkerSources[0].ProfileID != "" || remaining.Revision != p.Revision+1 {
		t.Fatal("replacement not published", err)
	}
}
