package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"icepick/internal/materials"
)

func (s *Store) MessageFiles(ctx context.Context, id string) ([]materials.File, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM message_files WHERE message_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []materials.File
	err = json.Unmarshal([]byte(raw), &files)
	return files, err
}
func (s *Store) RunInputFiles(ctx context.Context, run string) ([]materials.File, error) {
	root, err := s.RootRunID(ctx, run)
	if err != nil {
		return nil, err
	}
	var id string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM messages WHERE run_id=? AND role='user' ORDER BY seq LIMIT 1`, root).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.MessageFiles(ctx, id)
}
