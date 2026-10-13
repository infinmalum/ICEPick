package app

import (
	"context"

	"icepick/internal/storage"
)

func (s *Service) ListConversations() ([]storage.Conversation, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListConversations(context.Background())
}

func (s *Service) GetConversation(id string) (storage.Conversation, error) {
	if err := s.checkReady(); err != nil {
		return storage.Conversation{}, err
	}
	return s.store.GetConversation(context.Background(), id)
}

func (s *Service) CreateConversation(profileID, reuseSession, title string) (storage.Conversation, error) {
	if err := s.checkReady(); err != nil {
		return storage.Conversation{}, err
	}
	return s.store.CreateConversation(context.Background(), profileID, reuseSession, title)
}

func (s *Service) SelectConversationProfile(id, profileID string) (storage.Conversation, error) {
	if err := s.checkReady(); err != nil {
		return storage.Conversation{}, err
	}
	return s.store.SelectConversationProfile(context.Background(), id, profileID)
}

func (s *Service) GetLocalContext(id string) (storage.LocalContext, error) {
	if err := s.checkReady(); err != nil {
		return storage.LocalContext{}, err
	}
	return s.store.GetLocalContext(context.Background(), id)
}

func (s *Service) UpdateContextPolicy(id string, policy storage.AgentPolicy) (storage.LocalContext, error) {
	if err := s.checkReady(); err != nil {
		return storage.LocalContext{}, err
	}
	a, err := s.store.GetAgent(context.Background(), id)
	if err != nil {
		return storage.LocalContext{}, err
	}
	if _, err = s.store.UpdateAgentLocal(context.Background(), id, a.Name, policy); err != nil {
		return storage.LocalContext{}, err
	}
	return s.GetLocalContext(id)
}

func (s *Service) ArchiveProfile(id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.ArchiveProfile(context.Background(), id)
}

func (s *Service) ProfileReferences(id string) ([]storage.ProfileReference, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ProfileReferences(context.Background(), id)
}

func (s *Service) ArchiveProfileReplacing(id, replacement string, own bool, expected []storage.ProfileReference) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.ArchiveProfileReplacing(context.Background(), id, replacement, own, expected)
}

func (s *Service) CreateTemplateProfile(templateID, name string) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	return s.store.CreateTemplateProfile(context.Background(), templateID, name)
}

func (s *Service) RestoreTemplateProfile(id string, revision int, groups []string) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	return s.store.RestoreTemplateProfile(context.Background(), id, revision, groups)
}

func (s *Service) StartConversationTask(id, input string) (storage.Run, error) {
	c, err := s.GetConversation(id)
	if err != nil {
		return storage.Run{}, err
	}
	return s.StartAgentTask(c.ContextID, id, input)
}
