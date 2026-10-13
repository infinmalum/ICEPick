package storage

import (
	"context"
	"database/sql"
)

func migrateReviewV17(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`ALTER TABLE run_components RENAME TO run_components_v16`,
		`CREATE TABLE run_components(run_id TEXT PRIMARY KEY REFERENCES runs(id) ON DELETE CASCADE,role TEXT NOT NULL CHECK(role IN ('planner','reviewer')))`,
		`INSERT INTO run_components SELECT * FROM run_components_v16`,
		`DROP TABLE run_components_v16`,
		`CREATE TABLE task_reviews(id TEXT PRIMARY KEY,root_run_id TEXT NOT NULL REFERENCES task_roots(run_id) ON DELETE CASCADE,reviewer_run_id TEXT NOT NULL UNIQUE REFERENCES runs(id) ON DELETE CASCADE,binding TEXT NOT NULL,result TEXT NOT NULL CHECK(json_valid(result)),created_at TEXT NOT NULL)`,
		`CREATE TABLE review_requests(run_id TEXT PRIMARY KEY REFERENCES task_roots(run_id) ON DELETE CASCADE,target_run_id TEXT NOT NULL REFERENCES task_roots(run_id))`,
		`CREATE INDEX task_reviews_root ON task_reviews(root_run_id,created_at)`,
		`PRAGMA user_version=17`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
