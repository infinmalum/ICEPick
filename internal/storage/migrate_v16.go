package storage

import (
	"context"
	"database/sql"
)

func migrateCoordinationV16(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`CREATE TABLE run_components(run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,role TEXT NOT NULL CHECK(role IN ('planner')))`,
		`CREATE TABLE task_coordination(root_run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,event_seq INTEGER NOT NULL DEFAULT 1,planned_seq INTEGER NOT NULL DEFAULT 0,rounds INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT 'pending',reason TEXT NOT NULL DEFAULT '',updated_at TEXT NOT NULL)`,
		`CREATE TABLE task_events(root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,seq INTEGER NOT NULL,kind TEXT NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(root_run_id,seq))`,
		`CREATE TABLE root_budgets(root_run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,limits TEXT NOT NULL CHECK(json_valid(limits)),model_calls INTEGER NOT NULL DEFAULT 0,tool_calls INTEGER NOT NULL DEFAULT 0,tokens INTEGER NOT NULL DEFAULT 0,unknown_usage INTEGER NOT NULL DEFAULT 0,deadline TEXT NOT NULL)`,
		`CREATE TABLE model_usage(step_id TEXT PRIMARY KEY REFERENCES steps(id) ON DELETE CASCADE,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,reserved INTEGER NOT NULL,tokens INTEGER NOT NULL DEFAULT 0,known INTEGER NOT NULL DEFAULT 0,settled INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE task_artifacts(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,task_id TEXT NOT NULL,run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,sha256 TEXT NOT NULL,verification TEXT NOT NULL,created_at TEXT NOT NULL)`,
		`CREATE TABLE worker_observations(id TEXT PRIMARY KEY,run_id TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,content TEXT NOT NULL,source_scope TEXT NOT NULL,created_at TEXT NOT NULL)`,
		`INSERT INTO task_coordination(root_run_id,updated_at) SELECT run_id,strftime('%Y-%m-%dT%H:%M:%fZ','now') FROM task_roots`,
		`INSERT INTO root_budgets(root_run_id,limits,deadline) SELECT run_id,'{}',strftime('%Y-%m-%dT%H:%M:%fZ',created_at,'+600 seconds') FROM runs JOIN task_roots ON run_id=id`,
		`PRAGMA user_version=16`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT root_run_id,task_id,run_id FROM task_attempts WHERE status='succeeded'`)
	if err != nil {
		return err
	}
	type prior struct{ root, task, run string }
	var old []prior
	for rows.Next() {
		var p prior
		if err := rows.Scan(&p.root, &p.task, &p.run); err != nil {
			rows.Close()
			return err
		}
		old = append(old, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range old {
		if err := recordArtifact(ctx, tx, p.root, p.task, p.run, "succeeded"); err != nil {
			return err
		}
	}
	return nil
}
