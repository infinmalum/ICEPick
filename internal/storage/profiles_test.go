package storage

import (
	"context"

	"path/filepath"
	"testing"
)

func TestSharedCatalogSelections(t *testing.T) {
	def := defaultProfileData()
	if !def.AllowsTool("local__current_time") || !def.AllowsTool("example__search") || !def.EnablesSkill("review-code") {
		t.Fatal("default Profile should enable shared catalogs")
	}
	custom := BlankProfile()
	if custom.AllowsTool("local__current_time") || custom.EnablesSkill("review-code") {
		t.Fatal("blank Profile unexpectedly enabled catalog entries")
	}
	custom.EnabledMCPServers = []string{"example"}
	custom.EnabledSkills = []string{"review-code"}
	if !custom.AllowsTool("example__search") || custom.AllowsTool("other__search") || !custom.EnablesSkill("review-code") || custom.EnablesSkill("other") {
		t.Fatal("per-Profile selection failed")
	}
	custom.AllowedTools = []string{"example__read"}
	if custom.AllowsTool("example__search") || !custom.AllowsTool("example__read") {
		t.Fatal("tool allowlist failed")
	}
}

func TestProfileRevisionAndContextIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := testOpen(filepath.Join(t.TempDir(), "split.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Fresh databases carry exactly the built-in default pair.
	profiles, err := s.ListProfiles(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].ID != DefaultProfileID || !profiles[0].IsDefault || profiles[0].Revision != 1 {
		t.Fatalf("built-in profile: %+v %v", profiles, err)
	}
	data := BlankProfile()
	data.SystemPrompt = "研究提示词"
	profile, err := s.CreateProfile(ctx, "Research", data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.IsDefault {
		t.Fatal("created profile must not be the default")
	}
	created := newTestContext(t, s, "研究员", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Policy.ApprovalMode != "semi" || created.Policy.Permissions.Unrestricted {
		t.Fatalf("new local policy must start restrictive: %+v", created.Policy)
	}

	// Sessions, messages and runs stay owned by the instance.
	mainSession, err := s.CreateContextSession(ctx, testContextID, DefaultProfileID, "main task")
	if err != nil {
		t.Fatal(err)
	}
	customSession, err := s.CreateContextSession(ctx, created.ID, profile.ID, "research task")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.AddMessage(ctx, customSession.ID, "", "user", "hello")
	if err != nil || message.ContextID != created.ID {
		t.Fatalf("custom message: %+v %v", message, err)
	}
	run, runProfile, err := s.CreateRun(ctx, customSession.ID)
	if err != nil || run.ContextID != created.ID || run.ProfileID != profile.ID || run.ProfileRevision != 1 {
		t.Fatalf("custom run: %+v %v", run, err)
	}
	if runProfile.SystemPrompt != "研究提示词" || runProfile.ContextName != "研究员" {
		t.Fatalf("run profile snapshot: %+v", runProfile)
	}
	if err := s.SetRunStatus(ctx, run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET context_id='default' WHERE id=?`, customSession.ID); err == nil {
		t.Fatal("session with history reassigned")
	}
	mainSessions, err := s.ListContextSessions(ctx, testContextID)
	if err != nil || len(mainSessions) != 1 || mainSessions[0].ID != mainSession.ID {
		t.Fatalf("default sessions: %+v %v", mainSessions, err)
	}
	if _, err := s.CreateContextSession(ctx, "missing", DefaultProfileID, "invalid"); err == nil {
		t.Fatal("unknown local accepted")
	}

	// Editing the profile bumps the revision atomically; the new run picks
	// up the latest committed content while the earlier run's snapshot and
	// revision remain readable.
	edited := profile
	edited.Data.SystemPrompt = "提示词 v2"
	updated, err := s.UpdateProfile(ctx, edited)
	if err != nil || updated.Revision != 2 {
		t.Fatalf("profile update: %+v %v", updated, err)
	}
	if _, err := s.UpdateProfile(ctx, profile); err == nil {
		t.Fatal("stale revision accepted")
	}
	run2, runProfile2, err := s.CreateRun(ctx, customSession.ID)
	if err != nil || runProfile2.ProfileRevision != 2 || runProfile2.SystemPrompt != "提示词 v2" {
		t.Fatalf("run after profile edit: %+v %+v %v", run2, runProfile2, err)
	}
	if err := s.SetRunStatus(ctx, run2.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := s.ProfileSnapshot(ctx, run.ID)
	if err != nil || !ok || snapshot.SystemPrompt != "研究提示词" {
		t.Fatalf("first run snapshot: %+v ok=%v %v", snapshot, ok, err)
	}

	// Conversation profile selection preserves context policy and existing snapshots.
	switched, err := s.SelectConversationProfile(ctx, customSession.ID, DefaultProfileID)
	if err != nil || switched.ContextID != created.ID {
		t.Fatalf("selection: %+v %v", switched, err)
	}
	updatedContext, err := s.UpdateContextPolicy(ctx, created.ID, ContextPolicy{ApprovalMode: "automatic", Permissions: PermissionScope{Unrestricted: true}})
	if err != nil || updatedContext.Policy.ApprovalMode != "automatic" {
		t.Fatalf("policy: %+v %v", updatedContext, err)
	}
	if _, err := s.UpdateContextPolicy(ctx, created.ID, ContextPolicy{ApprovalMode: "invalid"}); err == nil {
		t.Fatal("invalid policy accepted")
	}
	if err := s.DeleteProfile(ctx, profile.ID); err != nil {
		t.Fatalf("unreferenced profile delete refused: %v", err)
	}
	if err := s.DeleteProfile(ctx, "missing"); err == nil {
		t.Fatal("missing profile delete accepted")
	}
}
