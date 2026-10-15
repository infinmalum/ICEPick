package app

import (
	"context"
	"errors"
	"icepick/internal/storage"
	"sort"
)

type SendConsent struct {
	Provider    string   `json:"provider"`
	Destination string   `json:"destination"`
	Scopes      []string `json:"scopes"`
	Missing     []string `json:"missing"`
}

func (s *Service) PreviewSend(session string, attachments bool, role string) ([]SendConsent, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if role != "main" && role != "improver" {
		return nil, errors.New("无效的发送类型")
	}
	ctx := context.Background()
	s.mu.RLock()
	source := s.config.Model
	s.mu.RUnlock()
	resources, err := s.store.PreviewResources(ctx, session, source, s.skills.Snapshot, role)
	if err != nil {
		return nil, err
	}
	conversation, err := s.store.GetConversation(ctx, session)
	if err != nil {
		return nil, err
	}
	p := storage.RunProfile{ContextID: resources.RootContextID, ProfileData: resources.Root.Data, Policy: resources.RootPolicy, ResourcesFrozen: true, FrozenSkills: resources.Root.Skills, ConfigScopes: resources.Root.Scopes, Resources: &resources}
	scopes, err := s.sessionScopes(ctx, session, p)
	if err != nil {
		return nil, err
	}
	required := map[string]bool{storage.ScopePersonal: true}
	if role == "improver" {
		required[storage.ScopeWorkspace] = true
	}
	for _, scope := range scopes {
		required[scope] = true
	}
	profiles := []storage.FrozenProfile{resources.Root}
	if resources.Planner.ID != "" {
		profiles = append(profiles, resources.Planner)
	}
	if resources.Reviewer.ID != "" {
		profiles = append(profiles, resources.Reviewer)
	}
	for _, w := range resources.Workers {
		profiles = append(profiles, w)
	}
	for _, f := range profiles {
		for _, scope := range f.Scopes {
			required[scope] = true
		}
		for _, skill := range f.Skills {
			for _, scope := range skill.Scopes {
				required[scope] = true
			}
			required[storage.ScopeWorkspace] = true
		}
		if f.Data.AllMCPServers || len(f.Data.EnabledMCPServers) > 0 || len(f.Data.AllowedTools) > 0 {
			required[storage.ScopeWorkspace] = true
			required[storage.ScopeUnknown] = true
		}
	}
	if attachments || conversation.Options.WebSearch {
		required[storage.ScopeUnknown] = true
	}
	if p.MemoryEnabled && p.Policy.Memory {
		memories, err := s.store.ListContextMemories(ctx, p.ContextID, 0)
		if err != nil {
			return nil, err
		}
		for _, m := range memories {
			required[m.Scope] = true
		}
	}
	all := []string{}
	for v := range required {
		all = append(all, v)
	}
	sort.Strings(all)
	results := []SendConsent{}
	seen := map[string]bool{}
	for _, f := range profiles {
		destination, local, err := endpointOrigin(f.Data.Model.EffectiveBaseURL())
		if err != nil {
			return nil, err
		}
		if local || seen[destination] {
			continue
		}
		seen[destination] = true
		grant, _, err := s.store.GetExfilGrant(ctx, p.ContextID, destination)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, v := range grant.Scopes {
			allowed[v] = true
		}
		item := SendConsent{Provider: f.Data.Model.Provider, Destination: destination, Scopes: all, Missing: []string{}}
		for _, v := range all {
			if !allowed[v] {
				item.Missing = append(item.Missing, v)
			}
		}
		results = append(results, item)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Destination < results[j].Destination })
	return results, nil
}
