package storage

import (
	"context"

	"testing"
)

func TestConversationIsolationSelectionAndArchive(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	p, err := s.CreateTemplateProfile(ctx, "daily", "")
	if err != nil {
		t.Fatal(err)
	}
	q, err := s.CreateTemplateProfile(ctx, "writing", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateConversation(ctx, p.ID, "", "A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateConversation(ctx, p.ID, "", "B")
	if err != nil {
		t.Fatal(err)
	}
	if a.ContextID == b.ContextID {
		t.Fatal("same Profile merged contexts")
	}
	local, err := s.GetLocalContext(ctx, a.ContextID)
	if err != nil {
		t.Fatal(err)
	}
	if local.Policy.ApprovalMode != "semi" || local.Policy.Permissions.Unrestricted || local.Policy.Memory {
		t.Fatalf("new context has unexpected grants: %+v", local)
	}
	if _, err := s.AddAgentMemory(ctx, a.ContextID, "private", "user", a.ID, ScopePersonal); err != nil {
		t.Fatal(err)
	}
	if err := s.SetExfilGrant(ctx, ExfilGrant{AgentID: a.ContextID, Destination: "https://example.com", Scopes: []string{ScopePersonal}}); err != nil {
		t.Fatal(err)
	}
	memories, err := s.ListAgentMemories(ctx, b.ContextID, 50)
	if err != nil || len(memories) != 0 {
		t.Fatalf("memory leaked: %+v %v", memories, err)
	}
	grants, err := s.ListExfilGrants(ctx, b.ContextID)
	if err != nil || len(grants) != 0 {
		t.Fatalf("grant leaked: %+v %v", grants, err)
	}
	reused, err := s.CreateConversation(ctx, q.ID, a.ID, "explicit reuse")
	if err != nil {
		t.Fatal(err)
	}
	if reused.ContextID != a.ContextID {
		t.Fatal("explicit reuse lost context")
	}
	run, snapshot, err := s.CreateRun(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectConversationProfile(ctx, a.ID, q.ID); err != nil {
		t.Fatal(err)
	}
	old, found, err := s.ProfileSnapshot(ctx, run.ID)
	if err != nil || !found || old.SystemPrompt != snapshot.SystemPrompt {
		t.Fatal("in-flight snapshot changed")
	}
	if err := s.ArchiveProfile(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConversation(ctx, b.ID); err != nil {
		t.Fatal("archive deleted history", err)
	}
	if _, _, err := s.CreateRun(ctx, b.ID); err == nil {
		t.Fatal("archived profile silently reused")
	}
	if _, err := s.CreateConversation(ctx, p.ID, "", ""); err == nil {
		t.Fatal("archived profile activated")
	}
	if _, err := s.CreateTemplateProfile(ctx, "daily", ""); err != nil {
		t.Fatal("deleted Profile broke factory", err)
	}
	if err := s.SetRunStatus(ctx, run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	next, resolved, err := s.CreateRun(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ProfileID != q.ID || resolved.AgentID != a.ContextID {
		t.Fatal("selection changed identity or used old Profile")
	}
	// Shared-context conversations retain their individual Profile choice.
	if _, err := s.SelectConversationProfile(ctx, reused.ID, DefaultProfileID); err != nil {
		t.Fatal(err)
	}
	current, _ := s.GetConversation(ctx, a.ID)
	if current.ProfileID != q.ID {
		t.Fatal("selection changed sibling conversation")
	}
}

func TestComponentProjectionAndTemplateRestore(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	p, err := s.CreateTemplateProfile(ctx, "daily", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Data.Execution.Main.SystemPrompt = "main override"
	p.Data.Execution.Main.RestrictTools = true
	p.Data.Execution.Main.AllowedTools = []string{}
	p.Data.Execution.Worker.SystemPrompt = "worker override"
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	main, err := p.Data.ResolveComponent("main")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := p.Data.ResolveComponent("worker")
	if err != nil {
		t.Fatal(err)
	}
	if main.SystemPrompt != "main override" || main.AllowsTool("local__current_time") || worker.SystemPrompt != "worker override" || !worker.AllowsTool("local__current_time") {
		t.Fatal("component projection broadened tools or changed sibling")
	}
	p.Data.Execution.Main.AllowedTools = []string{"external__execute"}
	if _, err := s.UpdateProfile(ctx, p); err == nil {
		t.Fatal("component added an unavailable tool")
	}
	p.Data.Execution.Main.AllowedTools = nil
	p.Data.Execution.Workflow = "planned"
	if _, err := s.UpdateProfile(ctx, p); err == nil {
		t.Fatal("unimplemented workflow accepted")
	}
	p.Data.Execution.Workflow = "direct"
	oldRevision := p.Revision
	restored, err := s.RestoreTemplateProfile(ctx, p.ID, p.Revision, []string{"execution"})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Data.Execution.Main.SystemPrompt != "" || restored.Data.SystemPrompt != p.Data.SystemPrompt {
		t.Fatal("partial restore changed unrelated configuration")
	}
	if _, err := s.RestoreTemplateProfile(ctx, p.ID, oldRevision, nil); err == nil {
		t.Fatal("stale restore overwritten new revision")
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM profile_revisions WHERE profile_id=?`, p.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("revision history missing: %d %v", n, err)
	}
}
