package app

import (
	"context"
	"icepick/internal/storage"
	"icepick/internal/testfixture"
)

func (s *Service) CreateSession() (storage.Session, error) {
	return testfixture.Session(s.store, context.Background(), "test")
}
func (s *Service) createContextSession(id string) (storage.Session, error) {
	return testfixture.ContextSession(s.store, context.Background(), id, "test")
}
