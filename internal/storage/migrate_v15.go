package storage

import (
	"context"
	"database/sql"
)

func migrateTaskGraphV15(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE task_roots(run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,goal TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE task_items(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,id TEXT NOT NULL,position INTEGER NOT NULL,spec TEXT NOT NULL CHECK(json_valid(spec)),state TEXT NOT NULL CHECK(state IN ('pending','running','succeeded','failed','cancelled','interrupted','execution_unknown')),PRIMARY KEY(root_run_id,id))`,
		`CREATE TABLE task_attempts(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL,task_id TEXT NOT NULL,run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,status TEXT NOT NULL CHECK(status IN ('running','succeeded','failed','cancelled','interrupted','execution_unknown')),created_at TEXT NOT NULL,updated_at TEXT NOT NULL,FOREIGN KEY(root_run_id,task_id) REFERENCES task_items(root_run_id,id) ON DELETE CASCADE,UNIQUE(root_run_id,task_id))`,
		`CREATE TABLE task_plan_revisions(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,revision INTEGER NOT NULL,plan TEXT NOT NULL CHECK(json_valid(plan)),created_at TEXT NOT NULL,PRIMARY KEY(root_run_id,revision))`,
		`INSERT INTO task_roots(run_id,goal) SELECT id,COALESCE((SELECT content FROM messages WHERE run_id=runs.id AND role='user' ORDER BY seq LIMIT 1),'') FROM runs WHERE parent_run_id IS NULL OR parent_run_id=''`,
		`PRAGMA user_version=15`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
