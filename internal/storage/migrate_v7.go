package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// migrateApprovalsV7 adds the approval lifecycle columns to tool_calls, the
// per-session data scope accounting inputs, the persistent exfiltration
// grants and the MCP server fingerprint registry used by the P2 permission
// gate. The tool_calls state CHECK from v6 already allows 'approved', so no
// table rebuild is required.
func migrateApprovalsV7(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tool_calls ADD COLUMN approval_source TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tool_calls ADD COLUMN approved_at TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tool_calls ADD COLUMN args_hash TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tool_calls ADD COLUMN source_scope TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE exfil_grants (destination TEXT PRIMARY KEY, scopes TEXT NOT NULL, created_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE mcp_endpoints (name TEXT PRIMARY KEY, command TEXT NOT NULL, binary_sha256 TEXT NOT NULL, schema_sha256 TEXT NOT NULL, version TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX tool_calls_state ON tool_calls(state)`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `PRAGMA user_version = 7`); err != nil {
		return fmt.Errorf("审批与权限迁移: %w", err)
	}
	return nil
}
