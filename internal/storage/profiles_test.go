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

func TestProfilesAgentsAndSessionIsolation(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "split.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Fresh databases carry exactly the built-in default pair.
	profiles, err := s.ListProfiles(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].ID != DefaultProfileID || !profiles[0].IsDefault || profiles[0].Revision != 1 {
		t.Fatalf("built-in profile: %+v %v", profiles, err)
	}
	agents, err := s.ListAgents(ctx)
	if err != nil || len(agents) != 1 || agents[0].ID != DefaultAgentID || agents[0].ProfileID != DefaultProfileID {
		t.Fatalf("built-in agent: %+v %v", agents, err)
	}
	if agents[0].Policy.ApprovalMode != "semi" || !agents[0].Policy.Permissions.Unrestricted {
		t.Fatalf("default agent policy: %+v", agents[0].Policy)
	}
	resolved, err := s.ResolvedAgentProfile(ctx, DefaultAgentID)
	if err != nil || resolved.ProfileRevision != 1 {
		t.Fatalf("resolved default: %+v %v", resolved, err)
	}

	// A new agent references an existing profile and initializes its own
	// restrictive policy — never a copy of another instance's authorization.
	data := BlankProfile()
	data.SystemPrompt = "研究提示词"
	profile, err := s.CreateProfile(ctx, "Research", data)
	if err != nil {
		t.Fatal(err)
	}
	if profile.IsDefault {
		t.Fatal("created profile must not be the default")
	}
	created, err := s.CreateAgent(ctx, "研究员", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Policy.ApprovalMode != "manual" || created.Policy.Permissions.Unrestricted {
		t.Fatalf("new agent policy must start restrictive: %+v", created.Policy)
	}

	// Sessions, messages and runs stay owned by the instance.
	mainSession, err := s.CreateAgentSession(ctx, DefaultAgentID, "main task")
	if err != nil {
		t.Fatal(err)
	}
	customSession, err := s.CreateAgentSession(ctx, created.ID, "research task")
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.AddMessage(ctx, customSession.ID, "", "user", "hello")
	if err != nil || message.AgentID != created.ID {
		t.Fatalf("custom message: %+v %v", message, err)
	}
	run, runProfile, err := s.CreateRun(ctx, customSession.ID)
	if err != nil || run.AgentID != created.ID || run.ProfileID != profile.ID || run.ProfileRevision != 1 {
		t.Fatalf("custom run: %+v %v", run, err)
	}
	if runProfile.SystemPrompt != "研究提示词" || runProfile.AgentName != "研究员" {
		t.Fatalf("run profile snapshot: %+v", runProfile)
	}
	if err := s.SetRunStatus(ctx, run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE messages SET agent_id='default' WHERE id=?`, message.ID); err == nil {
		t.Fatal("cross-Agent message rewrite accepted")
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET agent_id='default' WHERE id=?`, customSession.ID); err == nil {
		t.Fatal("session with history reassigned")
	}
	mainSessions, err := s.ListAgentSessions(ctx, DefaultAgentID)
	if err != nil || len(mainSessions) != 1 || mainSessions[0].ID != mainSession.ID {
		t.Fatalf("default sessions: %+v %v", mainSessions, err)
	}
	if _, err := s.CreateAgentSession(ctx, "missing", "invalid"); err == nil {
		t.Fatal("unknown agent accepted")
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

	// Switching keeps the agent's sessions and policy.
	switched, err := s.SwitchAgentProfile(ctx, created.ID, DefaultProfileID)
	if err != nil || switched.ProfileID != DefaultProfileID {
		t.Fatalf("switch: %+v %v", switched, err)
	}
	customSessions, err := s.ListAgentSessions(ctx, created.ID)
	if err != nil || len(customSessions) != 1 || customSessions[0].ID != customSession.ID {
		t.Fatalf("sessions must survive a profile switch: %+v %v", customSessions, err)
	}
	renamed, err := s.UpdateAgentLocal(ctx, created.ID, "研究员2号", AgentPolicy{ApprovalMode: "automatic", Permissions: PermissionScope{Unrestricted: true}})
	if err != nil || renamed.Name != "研究员2号" || renamed.Policy.ApprovalMode != "automatic" {
		t.Fatalf("agent update: %+v %v", renamed, err)
	}
	if _, err := s.UpdateAgentLocal(ctx, created.ID, "研究员2号", AgentPolicy{ApprovalMode: "chaotic"}); err == nil {
		t.Fatal("invalid policy accepted")
	}

	// Delete rules: referenced profiles are protected, agents with history
	// are protected, unreferenced profiles delete cleanly.
	if err := s.DeleteProfile(ctx, DefaultProfileID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAgent(ctx, created.ID); err == nil {
		t.Fatal("agent with sessions deleted")
	}
	if err := s.DeleteProfile(ctx, profile.ID); err != nil {
		t.Fatalf("unreferenced profile delete refused: %v", err)
	}
	if err := s.DeleteProfile(ctx, "missing"); err == nil {
		t.Fatal("missing profile delete accepted")
	}
}
