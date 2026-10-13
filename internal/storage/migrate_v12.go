package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func executionPayload(data ProfileData) ([]byte, error) {
	return json.Marshal(map[string]any{"schemaVersion": data.SchemaVersion, "execution": data.Execution, "templateId": data.TemplateID, "templateVersion": data.TemplateVersion})
}

func saveExecutionComponent(ctx context.Context, tx *sql.Tx, id string, data ProfileData) error {
	raw, err := executionPayload(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO profile_execution(profile_id,payload) VALUES(?,?) ON CONFLICT(profile_id) DO UPDATE SET payload=excluded.payload WHERE payload<>excluded.payload`, id, string(raw))
	return err
}

func saveProfileRevision(ctx context.Context, tx *sql.Tx, id string, revision int, name string, data ProfileData) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO profile_revisions(profile_id,revision,name,payload,created_at) VALUES(?,?,?,?,?)`, id, revision, name, string(raw), now())
	return err
}

// Physical agent IDs remain the compatibility keys of histories and grants.
// The context view is one-to-one; sharing a Profile never merges identities.
func migrateContextsV12(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,name,revision,config FROM resolved_profiles`)
	if err != nil {
		return err
	}
	type entry struct {
		id, name string
		revision int
		data     ProfileData
	}
	var entries []entry
	for rows.Next() {
		var e entry
		var raw string
		if err := rows.Scan(&e.id, &e.name, &e.revision, &raw); err != nil {
			rows.Close()
			return err
		}
		if err := json.Unmarshal([]byte(raw), &e.data); err != nil {
			rows.Close()
			return err
		}
		e.data = normalizeProfile(e.data)
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, query := range []string{
		`ALTER TABLE profiles ADD COLUMN archived_at TEXT`,
		`ALTER TABLE sessions ADD COLUMN profile_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE runs ADD COLUMN component TEXT NOT NULL DEFAULT 'main' CHECK(component IN ('main','worker'))`,
		`ALTER TABLE runs ADD COLUMN component_version INTEGER NOT NULL DEFAULT 1`,
		`UPDATE runs SET component='worker' WHERE parent_run_id IS NOT NULL AND parent_run_id<>''`,
		`CREATE TABLE profile_execution(profile_id TEXT PRIMARY KEY REFERENCES profiles(id) ON DELETE CASCADE,payload TEXT NOT NULL CHECK(json_valid(payload)))`,
		`CREATE TABLE profile_revisions(profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,revision INTEGER NOT NULL,name TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),created_at TEXT NOT NULL,PRIMARY KEY(profile_id,revision))`,
		`CREATE VIEW contexts AS SELECT id,name,policy,created_at,updated_at FROM agents`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	for _, e := range entries {
		if err := saveExecutionComponent(ctx, tx, e.id, e.data); err != nil {
			return err
		}
		if err := saveProfileRevision(ctx, tx, e.id, e.revision, e.name, e.data); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP VIEW resolved_profiles`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE VIEW resolved_profiles AS SELECT p.*,
 json_patch(json_patch(json_patch(json_patch(json_patch(i.payload,t.payload),m.payload),c.payload),r.payload),x.payload) AS config
 FROM profiles p
 JOIN profile_components i ON i.profile_id=p.id AND i.kind='persona'
 JOIN profile_components t ON t.profile_id=p.id AND t.kind='prompt'
 JOIN profile_components m ON m.profile_id=p.id AND m.kind='model'
 JOIN profile_components c ON c.profile_id=p.id AND c.kind='capabilities'
 JOIN profile_components r ON r.profile_id=p.id AND r.kind='runtime'
 JOIN profile_execution x ON x.profile_id=p.id`); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `PRAGMA user_version=12`)
	return err
}
