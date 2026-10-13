package storage

import (
	"context"
	"database/sql"
)

func migrateRunResourcesV13(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{`ALTER TABLE runs ADD COLUMN resources_snapshot TEXT NOT NULL DEFAULT ''`, `ALTER TABLE runs ADD COLUMN worker_name TEXT NOT NULL DEFAULT ''`, `PRAGMA user_version=13`} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
