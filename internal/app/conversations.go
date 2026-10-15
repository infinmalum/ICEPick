package app

import (
	"context"
	"errors"
	"icepick/internal/materials"

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

func (s *Service) UpdateContextPolicy(id string, policy storage.ContextPolicy) (storage.LocalContext, error) {
	if err := s.checkReady(); err != nil {
		return storage.LocalContext{}, err
	}
	return s.store.UpdateContextPolicy(context.Background(), id, policy)
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

func (s *Service) RenameConversation(id, title string) (storage.Conversation, error) {
	if err := s.checkReady(); err != nil {
		return storage.Conversation{}, err
	}
	return s.store.RenameConversation(context.Background(), id, title)
}
func (s *Service) DeleteConversation(id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	return s.store.DeleteConversation(context.Background(), id)
}

func (s *Service) MessageAttachment(messageID string, index int) (materials.File, error) {
	if err := s.checkReady(); err != nil {
		return materials.File{}, err
	}
	files, err := s.store.MessageFiles(context.Background(), messageID)
	if err != nil {
		return materials.File{}, err
	}
	if index < 0 || index >= len(files) {
		return materials.File{}, errors.New("找不到这个附件")
	}
	return files[index], nil
}

type ChatMessagePage struct {
	Messages  []storage.Message `json:"messages"`
	HasMore   bool              `json:"hasMore"`
	BeforeSeq int               `json:"beforeSeq"`
}

func (s *Service) ListChatMessages(sessionID string, before, limit int) (ChatMessagePage, error) {
	if err := s.checkReady(); err != nil {
		return ChatMessagePage{}, err
	}
	if limit < 1 || limit > 100 {
		return ChatMessagePage{}, errors.New("每页最多100条消息")
	}
	if err := s.checkSessionExists(sessionID); err != nil {
		return ChatMessagePage{}, err
	}
	items, err := s.store.ChatMessages(context.Background(), sessionID, before, limit+1)
	if err != nil {
		return ChatMessagePage{}, err
	}
	page := ChatMessagePage{Messages: items, HasMore: len(items) > limit}
	if page.HasMore {
		page.Messages = items[len(items)-limit:]
	}
	if len(page.Messages) > 0 {
		page.BeforeSeq = page.Messages[0].Seq
	}
	return page, nil
}
