package main

import (
	"icepick/internal/app"
	"icepick/internal/storage"
)

// Desktop operations use conversations and Profiles.
func (s *StatusService) ListConversations() ([]storage.Conversation, error) {
	return s.svc.ListConversations()
}
func (s *StatusService) GetConversation(id string) (storage.Conversation, error) {
	return s.svc.GetConversation(id)
}
func (s *StatusService) CreateConversation(profileID, reuseSession, title string) (storage.Conversation, error) {
	return s.svc.CreateConversation(profileID, reuseSession, title)
}
func (s *StatusService) SelectConversationProfile(id, profileID string) (storage.Conversation, error) {
	return s.svc.SelectConversationProfile(id, profileID)
}
func (s *StatusService) GetLocalContext(id string) (storage.LocalContext, error) {
	return s.svc.GetLocalContext(id)
}
func (s *StatusService) UpdateContextPolicy(id string, policy storage.ContextPolicy) (storage.LocalContext, error) {
	return s.svc.UpdateContextPolicy(id, policy)
}
func (s *StatusService) ListComponents() []storage.Component { return storage.Components() }
func (s *StatusService) ListProfileTemplates() []storage.ProfileTemplate {
	return storage.ProfileTemplates()
}
func (s *StatusService) CreateTemplateProfile(id, name string) (storage.Profile, error) {
	return s.svc.CreateTemplateProfile(id, name)
}
func (s *StatusService) RestoreTemplateProfile(id string, revision int, groups []string) (storage.Profile, error) {
	return s.svc.RestoreTemplateProfile(id, revision, groups)
}
func (s *StatusService) ArchiveProfile(id string) error { return s.svc.ArchiveProfile(id) }

func (s *StatusService) ProfileReferences(id string) ([]storage.ProfileReference, error) {
	return s.svc.ProfileReferences(id)
}
func (s *StatusService) ArchiveProfileReplacing(id, replacement string, own bool, expected []storage.ProfileReference) error {
	return s.svc.ArchiveProfileReplacing(id, replacement, own, expected)
}
func (s *StatusService) ExportProfileIncludingWorkers(id string) (app.ExportResult, error) {
	return s.svc.ExportProfileIncludingWorkers(id)
}

func (s *StatusService) GetProfile(id string) (storage.Profile, error) { return s.svc.GetProfile(id) }

func (s *StatusService) GetTaskGraph(runID string) (storage.TaskGraph, error) {
	return s.svc.GetTaskGraph(runID)
}

func (s *StatusService) ListTaskReviews(runID string) ([]storage.TaskReview, error) {
	return s.svc.ListTaskReviews(runID)
}
func (s *StatusService) StartConversationReview(sessionID, target string) (storage.Run, error) {
	return s.svc.StartConversationReview(sessionID, target)
}

func (s *StatusService) StartConversationTask(sessionID, input string) (storage.Run, error) {
	return s.svc.StartConversationTask(sessionID, input)
}

func (s *StatusService) SetConversationOptions(id string, options storage.ConversationOptions) (storage.Conversation, error) {
	return s.svc.SetConversationOptions(id, options)
}
func (s *StatusService) ConversationSearchAvailability(id string) (app.SearchAvailability, error) {
	return s.svc.ConversationSearchAvailability(id)
}
func (s *StatusService) ListConversationSearch(id string) ([]storage.SearchRequest, error) {
	return s.svc.ListConversationSearch(id)
}

func (s *StatusService) StartConversationRetrospective(sessionID, target string) (storage.Run, error) {
	return s.svc.StartConversationRetrospective(sessionID, target)
}
func (s *StatusService) ListTaskRetrospectives(runID string) ([]storage.TaskRetrospective, error) {
	return s.svc.ListTaskRetrospectives(runID)
}
