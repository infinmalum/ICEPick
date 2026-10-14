// Package testfixture builds explicit local contexts for integration tests.
package testfixture

import (
	"context"
	"database/sql"
	"icepick/internal/storage"
)

const ContextID = "test-context"

func Open(path string) (*storage.Store, error) {
	s, err := storage.Open(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		s.Close()
		return nil, err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT OR IGNORE INTO contexts(id,name,policy,created_at,updated_at) VALUES(?, 'Test context', '{"approvalMode":"semi","permissions":{"unrestricted":true}}', '', '')`, ContextID)
	if err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
func Session(s *storage.Store, ctx context.Context, title string) (storage.Session, error) {
	return s.CreateContextSession(ctx, ContextID, storage.DefaultProfileID, title)
}
func Context(s *storage.Store, ctx context.Context, name, profileID string) (storage.LocalContext, error) {
	c, err := s.CreateConversation(ctx, profileID, "", name)
	if err != nil {
		return storage.LocalContext{}, err
	}
	return s.GetLocalContext(ctx, c.ContextID)
}
func ContextSession(s *storage.Store, ctx context.Context, id, title string) (storage.Session, error) {
	cs, err := s.ListContextSessions(ctx, id)
	if err != nil {
		return storage.Session{}, err
	}
	profile := storage.DefaultProfileID
	if len(cs) > 0 {
		c, err := s.GetConversation(ctx, cs[0].ID)
		if err != nil {
			return storage.Session{}, err
		}
		profile = c.ProfileID
	}
	return s.CreateContextSession(ctx, id, profile, title)
}
func Profile(s *storage.Store, ctx context.Context, id string) (storage.RunProfile, error) {
	cs, err := s.ListContextSessions(ctx, id)
	if err != nil {
		return storage.RunProfile{}, err
	}
	if len(cs) > 0 {
		return s.ResolveRunProfile(ctx, cs[0].ID)
	}
	session, err := ContextSession(s, ctx, id, "fixture")
	if err != nil {
		return storage.RunProfile{}, err
	}
	return s.ResolveRunProfile(ctx, session.ID)
}
