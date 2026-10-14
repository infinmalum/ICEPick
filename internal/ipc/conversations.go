package ipc

import (
	"icepick/internal/storage"
)

// The same application contract is used by Profile-first CLI and desktop.
func (s *Server) dispatchConversation(request Request) (any, error) {
	var params struct {
		Options      storage.ConversationOptions `json:"options"`
		ID           string                      `json:"id"`
		ProfileID    string                      `json:"profileId"`
		ReuseSession string                      `json:"reuseSession"`
		Title        string                      `json:"title"`
		Name         string                      `json:"name"`
		ContextID    string                      `json:"contextId"`
		Input        string                      `json:"input"`
		Revision     int                         `json:"revision"`
		Groups       []string                    `json:"groups"`
		Policy       storage.AgentPolicy         `json:"policy"`
		Replacement  string                      `json:"replacement"`
		Own          bool                        `json:"own"`
		Expected     []storage.ProfileReference  `json:"expected"`
	}
	if err := decodeParams(request.Params, &params); err != nil {
		return nil, err
	}
	switch request.Method {
	case "conversations.options":
		return s.svc.SetConversationOptions(params.ID, params.Options)
	case "conversations.searchAvailability":
		return s.svc.ConversationSearchAvailability(params.ID)
	case "conversations.searchRecords":
		return s.svc.ListConversationSearch(params.ID)
	case "profiles.references":
		return s.svc.ProfileReferences(params.ID)
	case "profiles.replaceArchive":
		return nil, s.svc.ArchiveProfileReplacing(params.ID, params.Replacement, params.Own, params.Expected)
	case "profiles.exportWorkers":
		return s.svc.ExportProfileIncludingWorkers(params.ID)
	case "conversations.list":
		return s.svc.ListConversations()
	case "conversations.get":
		return s.svc.GetConversation(params.ID)
	case "conversations.create":
		return s.svc.CreateConversation(params.ProfileID, params.ReuseSession, params.Title)
	case "conversations.select":
		return s.svc.SelectConversationProfile(params.ID, params.ProfileID)
	case "conversations.start":
		return s.svc.StartConversationTask(params.ID, params.Input)
	case "contexts.get":
		return s.svc.GetLocalContext(params.ID)
	case "contexts.update":
		return s.svc.UpdateContextPolicy(params.ID, params.Policy)
	case "templates.list":
		return storage.ProfileTemplates(), nil
	case "templates.create":
		return s.svc.CreateTemplateProfile(params.ID, params.Name)
	case "templates.restore":
		return s.svc.RestoreTemplateProfile(params.ID, params.Revision, params.Groups)
	case "components.list":
		return storage.Components(), nil
	case "profiles.archive":
		return nil, s.svc.ArchiveProfile(params.ID)
	}
	return nil, nil
}
