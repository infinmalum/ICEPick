package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ProfileData is an assembled API/run snapshot. Only its individual components
// are authoritative on disk; the view below assembles them for consistent reads.
const profileView = `CREATE VIEW resolved_profiles AS SELECT p.*,
 json_patch(json_patch(json_patch(json_patch(i.payload,t.payload),m.payload),c.payload),r.payload) AS config
 FROM profiles p
 JOIN profile_components i ON i.profile_id=p.id AND i.kind='persona'
 JOIN profile_components t ON t.profile_id=p.id AND t.kind='prompt'
 JOIN profile_components m ON m.profile_id=p.id AND m.kind='model'
 JOIN profile_components c ON c.profile_id=p.id AND c.kind='capabilities'
 JOIN profile_components r ON r.profile_id=p.id AND r.kind='runtime'`

func saveProfileComponents(ctx context.Context, tx *sql.Tx, id string, data ProfileData) error {
	parts := map[string]any{
		"persona":      map[string]any{"description": data.Description, "persona": data.Persona},
		"prompt":       map[string]any{"systemPrompt": data.SystemPrompt},
		"model":        map[string]any{"model": data.Model},
		"capabilities": map[string]any{"allMcpServers": data.AllMCPServers, "enabledMcpServers": data.EnabledMCPServers, "allowedTools": data.AllowedTools, "allSkills": data.AllSkills, "enabledSkills": data.EnabledSkills, "delegation": data.Delegation},
		"runtime":      map[string]any{"contextBudget": data.ContextBudget, "memoryEnabled": data.MemoryEnabled, "planEnabled": data.PlanEnabled},
	}
	for kind, part := range parts {
		payload, err := json.Marshal(part)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO profile_components(profile_id,kind,payload) VALUES(?,?,?) ON CONFLICT(profile_id,kind) DO UPDATE SET payload=excluded.payload WHERE payload<>excluded.payload`, id, kind, string(payload)); err != nil {
			return err
		}
	}
	return nil
}

func migrateComponentsV9(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE profile_components(profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE, kind TEXT NOT NULL CHECK(kind IN ('persona','prompt','model','capabilities','runtime')), payload TEXT NOT NULL CHECK(json_valid(payload)), PRIMARY KEY(profile_id,kind))`); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,config FROM profiles`)
	if err != nil {
		return err
	}
	type entry struct {
		id   string
		data ProfileData
	}
	var entries []entry
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var data ProfileData
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, entry{id, data})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := saveProfileComponents(ctx, tx, e.id, e.data); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE profiles DROP COLUMN config`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, profileView); err != nil {
		return err
	}
	// Existing global grants cannot be safely attributed to an Agent. Retain
	// them for inspection, but require fresh, explicitly scoped authorization.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE exfil_grants RENAME TO legacy_exfil_grants`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE exfil_grants(agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE, destination TEXT NOT NULL, scopes TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(agent_id,destination))`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE session_context_scopes(session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE, scope TEXT NOT NULL, PRIMARY KEY(session_id,scope))`); err != nil {
		return err
	}
	// Older model requests did not retain complete provenance. Treat their
	// derived context as unknown instead of assuming it became public.
	if _, err := tx.ExecContext(ctx, `INSERT INTO session_context_scopes SELECT DISTINCT session_id,'unknown' FROM runs WHERE EXISTS(SELECT 1 FROM steps WHERE steps.run_id=runs.id)`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `PRAGMA user_version=9`)
	return err
}
