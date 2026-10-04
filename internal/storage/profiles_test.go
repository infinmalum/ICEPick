package storage

import (
	"context"
	"database/sql"
	"encoding/json"
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

func TestVersionFourPresetsMergeIntoDefault(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v4.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"chat", "work"} {
		data := defaultProfileData()
		data.SystemPrompt = "提示词 " + id
		policyJSON, err := json.Marshal(AgentPolicy{ApprovalMode: "semi", Permissions: PermissionScope{Unrestricted: true}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO profiles (id, name, revision, is_default, created_at, updated_at) VALUES (?,?,?,0,?,?)`, id, id, 1, now(), now()); err != nil {
			t.Fatal(err)
		}
		if err := s.withTx(ctx, func(tx *sql.Tx) error { return saveProfileComponents(ctx, tx, id, data) }); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO agents (id, name, profile_id, policy, created_at, updated_at) VALUES (?,?,?,?,?,?)`, id, id, id, string(policyJSON), now(), now()); err != nil {
			t.Fatal(err)
		}
		session, err := s.CreateAgentSession(ctx, id, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AddMessage(ctx, session.ID, "", "user", id); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.CreateRun(ctx, session.ID); err != nil {
			t.Fatal(err)
		}
	}
	// Roll the file back to its version 4 shape so the re-open exercises
	// the real v4-to-current migration path.
	downgradeToVersion(t, s, 4)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	agents, err := s.ListAgents(ctx)
	if err != nil || len(agents) != 1 || agents[0].ID != DefaultAgentID {
		t.Fatalf("merged agents: %+v %v", agents, err)
	}
	profiles, err := s.ListProfiles(ctx)
	if err != nil || len(profiles) != 1 || profiles[0].ID != DefaultProfileID {
		t.Fatalf("merged profiles: %+v %v", profiles, err)
	}
	sessions, err := s.ListAgentSessions(ctx, DefaultAgentID)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("merged sessions: %+v %v", sessions, err)
	}
	for _, session := range sessions {
		messages, err := s.ListMessages(ctx, session.ID)
		if err != nil || len(messages) != 1 || messages[0].AgentID != DefaultAgentID {
			t.Fatalf("merged messages: %+v %v", messages, err)
		}
		runs, err := s.ListRuns(ctx, session.ID)
		if err != nil || len(runs) != 1 || runs[0].AgentID != DefaultAgentID || runs[0].ProfileID != DefaultProfileID {
			t.Fatalf("merged runs: %+v %v", runs, err)
		}
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
	if err != nil || resolved.AgentName != "默认 Agent" || resolved.ProfileName != "默认 Profile" || resolved.ProfileRevision != 1 {
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
	if err := s.DeleteProfile(ctx, DefaultProfileID); err == nil {
		t.Fatal("referenced profile deleted")
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

func TestVersionTwoMigrationAssignsDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY, title TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, error TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions VALUES ('old', 'legacy', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages VALUES ('m', 'old', 'user', 'hello', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs VALUES ('r', 'old', 'completed', '2026-01-01', '2026-01-01', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	session, err := s.GetSession(ctx, "old")
	if err != nil || session.AgentID != DefaultAgentID {
		t.Fatalf("migrated session: %+v %v", session, err)
	}
	messages, err := s.ListMessages(ctx, "old")
	if err != nil || len(messages) != 1 || messages[0].Content != "hello" || messages[0].AgentID != DefaultAgentID {
		t.Fatalf("messages: %+v %v", messages, err)
	}
	run, err := s.GetRun(ctx, "r")
	if err != nil || run.Status != "completed" || run.AgentID != DefaultAgentID || run.ProfileRevision != 1 || run.ProfileID != DefaultProfileID {
		t.Fatalf("run: %+v %v", run, err)
	}
}
