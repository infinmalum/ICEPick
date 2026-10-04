package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateVersionOneRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
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
	if _, err := db.Exec(`CREATE TABLE runs (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions VALUES ('s', 'old', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO runs VALUES ('r', 's', 'running', '2026-01-01', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SetRunStatus(context.Background(), "r", "failed", "model unavailable"); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetRun(context.Background(), "r")
	if err != nil || run.Error != "model unavailable" {
		t.Fatalf("migrated run: %+v, %v", run, err)
	}
}

// downgradeToVersion strips the version 6, 7 and 8 structures from a live
// database so an older migration path can be exercised against a genuine
// old-shape file.
func downgradeToVersion(t *testing.T, store *Store, version int) {
	t.Helper()
	ctx := context.Background()
	downgradeToV7(t, store)
	drop := []struct {
		statement string
	}{
		{`DROP TABLE tool_calls`},
		{`DROP TABLE steps`},
		{`DROP TABLE exfil_grants`},
		{`DROP TABLE mcp_endpoints`},
		{`DROP INDEX messages_session_seq`},
	}
	for _, item := range drop {
		if _, err := store.db.ExecContext(ctx, item.statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"run_id", "seq", "tool_calls", "legacy"} {
		if _, err := store.db.ExecContext(ctx, `ALTER TABLE messages DROP COLUMN `+column); err != nil {
			t.Fatal(err)
		}
	}
	switch version {
	case 4:
		if _, err := store.db.ExecContext(ctx, `PRAGMA user_version = 4`); err != nil {
			t.Fatal(err)
		}
	default:
		if _, err := store.db.ExecContext(ctx, `PRAGMA user_version = 5`); err != nil {
			t.Fatal(err)
		}
	}
}

// downgradeToV7 reverses the v8 profile/agent split on a live database so the
// v7→v8 migration can be exercised against a genuine old-shape file.
func downgradeToV7(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	type legacyRow struct {
		id, name, config string
		revision         int
		preset, created  string
		updated          string
	}
	var rows []legacyRow
	iter, err := store.db.QueryContext(ctx, `SELECT a.id, a.name, p.config, p.revision, CASE WHEN p.is_default THEN p.id ELSE '' END, p.created_at, p.updated_at, a.policy FROM agents a JOIN resolved_profiles p ON p.id = a.profile_id`)
	if err != nil {
		t.Fatal(err)
	}
	for iter.Next() {
		var id, name, configJSON, policyJSON, preset, created, updated string
		var revision int
		if err := iter.Scan(&id, &name, &configJSON, &revision, &preset, &created, &updated, &policyJSON); err != nil {
			iter.Close()
			t.Fatal(err)
		}
		// Rebuild the pre-v8 combined record from the split halves.
		var data ProfileData
		var policy AgentPolicy
		if err := json.Unmarshal([]byte(configJSON), &data); err != nil {
			iter.Close()
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(policyJSON), &policy); err != nil {
			iter.Close()
			t.Fatal(err)
		}
		combined, err := json.Marshal(legacyAgentProfile{
			Description:       data.Description,
			SystemPrompt:      data.SystemPrompt,
			Model:             data.Model,
			AllMCPServers:     data.AllMCPServers,
			EnabledMCPServers: data.EnabledMCPServers,
			AllowedTools:      data.AllowedTools,
			AllSkills:         data.AllSkills,
			EnabledSkills:     data.EnabledSkills,
			Permissions:       policy.Permissions,
			ApprovalMode:      policy.ApprovalMode,
			Delegation:        data.Delegation,
			ContextBudget:     data.ContextBudget,
		})
		if err != nil {
			iter.Close()
			t.Fatal(err)
		}
		rows = append(rows, legacyRow{id: id, name: name, config: string(combined), revision: revision, preset: preset, created: created, updated: updated})
	}
	if err := iter.Err(); err != nil {
		iter.Close()
		t.Fatal(err)
	}
	iter.Close()
	for _, statement := range []struct {
		sql string
	}{
		{`DROP TRIGGER IF EXISTS sessions_agent_insert`},
		{`DROP TRIGGER IF EXISTS sessions_agent_update`},
		{`CREATE TABLE agent_profiles (id TEXT PRIMARY KEY, name TEXT NOT NULL, config TEXT NOT NULL, revision INTEGER NOT NULL, preset TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`},
	} {
		if _, err := store.db.ExecContext(ctx, statement.sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range rows {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO agent_profiles (id,name,config,revision,preset,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`, row.id, row.name, row.config, row.revision, row.preset, row.created, row.updated); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []struct {
		sql string
	}{
		{`DROP TABLE session_context_scopes`},
		{`DROP VIEW resolved_profiles`},
		{`DROP TABLE profile_components`},
		{`DROP TABLE exfil_grants`},
		{`DROP TABLE agent_memories`},
		{`DROP TABLE session_summaries`},
		{`DROP TABLE session_plans`},
		{`ALTER TABLE legacy_exfil_grants RENAME TO exfil_grants`},
		{`DROP TABLE agents`},
		{`DROP TABLE profiles`},
		{`ALTER TABLE runs DROP COLUMN profile_id`},
		{`ALTER TABLE runs DROP COLUMN profile_snapshot`},
		{`CREATE TRIGGER sessions_agent_insert BEFORE INSERT ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`},
		{`CREATE TRIGGER sessions_agent_update BEFORE UPDATE OF agent_id ON sessions WHEN NOT EXISTS (SELECT 1 FROM agent_profiles WHERE id = NEW.agent_id) BEGIN SELECT RAISE(ABORT, 'unknown agent'); END`},
		{`PRAGMA user_version = 7`},
	} {
		if _, err := store.db.ExecContext(ctx, statement.sql); err != nil {
			t.Fatal(err)
		}
	}
}

// A v5 database keeps a restorable pre-migration file snapshot next to the
// live database, legacy free-form tool messages are flagged for display only,
// and existing conversations get stable sequence numbers.
func TestMigrateV5BackupLegacyAndSequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO sessions (id, title, created_at) VALUES ('s', 'old', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct {
		id      string
		role    string
		content string
		at      string
	}{{"m1", RoleUser, "几点了", "2026-01-01T00:00:01Z"}, {"m2", RoleAssistant, "", "2026-01-01T00:00:02Z"}, {"m3", RoleTool, "local__current_time: 12:00", "2026-01-01T00:00:03Z"}, {"m4", RoleAssistant, "现在是12点", "2026-01-01T00:00:04Z"}} {
		if _, err := store.db.ExecContext(ctx, `INSERT INTO messages (id, session_id, seq, role, content, created_at) VALUES (?, 's', ?, ?, ?, ?)`, item.id, i+1, item.role, item.content, item.at); err != nil {
			t.Fatal(err)
		}
	}
	downgradeToVersion(t, store, 5)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// The backup snapshot must exist and still carry the pre-migration
	// schema version so the previous binary can restore it.
	if store.BackupPath() == "" {
		t.Fatal("no pre-migration backup kept")
	}
	backup, err := sql.Open("sqlite3", store.BackupPath())
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var backupVersion int
	if err := backup.QueryRow("PRAGMA user_version").Scan(&backupVersion); err != nil || backupVersion != 5 {
		t.Fatalf("backup version: %d %v", backupVersion, err)
	}

	messages, err := store.ListMessages(ctx, "s")
	if err != nil || len(messages) != 4 {
		t.Fatalf("messages: %+v %v", messages, err)
	}
	for i, want := range []struct {
		role   string
		legacy bool
		seq    int
	}{{RoleUser, false, 1}, {RoleAssistant, false, 2}, {RoleTool, true, 3}, {RoleAssistant, false, 4}} {
		if messages[i].Role != want.role || messages[i].Legacy != want.legacy || messages[i].Seq != want.seq {
			t.Fatalf("message %d: %+v, want %+v", i, messages[i], want)
		}
	}
	// The legacy tool message keeps its content for display but produces no
	// call record; nothing fabricates call IDs or results.
	calls, err := store.ListSessionToolCalls(ctx, "s")
	if err != nil || len(calls) != 0 {
		t.Fatalf("legacy tool messages must not become call records: %+v %v", calls, err)
	}
	if !strings.Contains(messages[2].Content, "local__current_time") {
		t.Fatalf("legacy content lost: %+v", messages[2])
	}
}

// A v6 database gains the approval columns, exfiltration grants and MCP
// fingerprint registry on migration to v7, with a restorable backup.
func TestMigrateV7ApprovalColumnsAndPolicyTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v6.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	session, err := store.CreateSession(ctx, "old session")
	if err != nil {
		t.Fatal(err)
	}
	downgradeToV6(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.BackupPath() == "" {
		t.Fatal("no pre-migration backup kept")
	}
	// Exercise the new columns through the approval lifecycle.
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, calls, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{{ID: "c1", Name: "local__current_time", Arguments: "{}"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ApproveToolCall(ctx, calls[0].ID, ApprovalUser); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginToolCall(ctx, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, CallSucceeded, "12:00", "", ScopeWorkspace); err != nil {
		t.Fatal(err)
	}
	if err := store.SetExfilGrant(ctx, ExfilGrant{AgentID: DefaultAgentID, Destination: "http://m.example.com:9", Scopes: []string{ScopePersonal}}); err != nil {
		t.Fatal(err)
	}
}

// downgradeToV6 removes the v7 structures from a live database after
// reverting the v8 split, leaving a genuine v6-shaped file.
func downgradeToV6(t *testing.T, store *Store) {
	t.Helper()
	downgradeToV7(t, store)
	ctx := context.Background()
	for _, statement := range []struct {
		sql string
	}{
		{`DROP TABLE exfil_grants`},
		{`DROP TABLE mcp_endpoints`},
		{`DROP INDEX tool_calls_state`},
		{`ALTER TABLE tool_calls DROP COLUMN approval_source`},
		{`ALTER TABLE tool_calls DROP COLUMN approved_at`},
		{`ALTER TABLE tool_calls DROP COLUMN args_hash`},
		{`ALTER TABLE tool_calls DROP COLUMN source_scope`},
		{`PRAGMA user_version = 6`},
	} {
		if _, err := store.db.ExecContext(ctx, statement.sql); err != nil {
			t.Fatal(err)
		}
	}
}

// A v7 database is split into profiles and agents on migration to v8: the
// instance keeps its id, name, profile reference and machine policy, sessions
// and runs keep their ownership, and runs learn which profile they used. A
// restorable pre-migration backup is kept.
func TestMigrateV8SplitsProfilesAndAgents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v7.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data := BlankProfile()
	data.SystemPrompt = "拆分提示词"
	profile, err := store.CreateProfile(ctx, "Research", data)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.CreateAgent(ctx, "研究员", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateAgentSession(ctx, agent.ID, "拆分会话")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, "", RoleUser, "hello"); err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetRunStatus(ctx, run.ID, "completed", ""); err != nil {
		t.Fatal(err)
	}
	downgradeToV7(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if store.BackupPath() == "" {
		t.Fatal("no pre-migration backup kept")
	}
	split, err := store.GetAgent(ctx, agent.ID)
	if err != nil || split.Name != "研究员" || split.Policy.ApprovalMode != "manual" {
		t.Fatalf("split agent: %+v %v", split, err)
	}
	// The legacy single-id shape gives the split pair the agent's id, so the
	// profile content must have followed it under that id.
	profiles, err := store.ListProfiles(ctx)
	if err != nil || len(profiles) != 2 {
		t.Fatalf("split profiles: %+v %v", profiles, err)
	}
	researched := false
	for _, p := range profiles {
		if p.ID == agent.ID {
			// The legacy single-name shape carries the agent's name; the
			// combination content itself must survive.
			if p.Data.SystemPrompt != "拆分提示词" || p.Name != "研究员" || p.IsDefault {
				t.Fatalf("split profile content: %+v", p)
			}
			researched = true
		}
	}
	if !researched {
		t.Fatal("custom profile lost in migration")
	}
	sessions, err := store.ListAgentSessions(ctx, agent.ID)
	if err != nil || len(sessions) != 1 || sessions[0].ID != session.ID {
		t.Fatalf("split sessions: %+v %v", sessions, err)
	}
	messages, err := store.ListMessages(ctx, session.ID)
	if err != nil || len(messages) != 1 || messages[0].AgentID != agent.ID {
		t.Fatalf("split messages: %+v %v", messages, err)
	}
	got, err := store.GetRun(ctx, run.ID)
	if err != nil || got.AgentID != agent.ID || got.ProfileID != agent.ID || got.ProfileRevision != 1 {
		t.Fatalf("split run: %+v %v", got, err)
	}
	if _, _, err := store.CreateRun(ctx, session.ID); err != nil {
		t.Fatalf("post-migration run needs the resolved profile: %v", err)
	}
	// The recreated triggers still guard agent references.
	if _, err := store.CreateAgentSession(ctx, "unknown-agent", "x"); err == nil {
		t.Fatal("unknown agent accepted after migration")
	}
}
