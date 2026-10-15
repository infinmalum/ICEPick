package storage

import (
	"context"
	"database/sql"
)

func profileScopesInTx(ctx context.Context, tx *sql.Tx, id string, revision int) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope FROM profile_revision_scopes WHERE profile_id=? AND revision=? ORDER BY scope`, id, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var scope string
		if err = rows.Scan(&scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, rows.Err()
}
func (s *Store) ProfileRevisionScopes(ctx context.Context, id string, revision int) ([]string, error) {
	var out []string
	err := s.withTx(ctx, func(tx *sql.Tx) error { var err error; out, err = profileScopesInTx(ctx, tx, id, revision); return err })
	return out, err
}
func copyProfileScopesInTx(ctx context.Context, tx *sql.Tx, source string, from int, target string, to int) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO profile_revision_scopes(profile_id,revision,scope) SELECT ?,?,scope FROM profile_revision_scopes WHERE profile_id=? AND revision=?`, target, to, source, from)
	return err
}
func runScopesInTx(ctx context.Context, tx *sql.Tx, run string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT s.scope FROM session_context_scopes s JOIN runs r ON r.session_id=s.session_id WHERE r.id=? ORDER BY s.scope`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var scope string
		if err = rows.Scan(&scope); err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	return out, rows.Err()
}
func publishDraftScopesInTx(ctx context.Context, tx *sql.Tx, run, target string, revision int) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO profile_revision_scopes(profile_id,revision,scope) SELECT ?,?,s.scope FROM session_context_scopes s JOIN runs r ON r.session_id=s.session_id WHERE r.id=?`, target, revision, run)
	return err
}
