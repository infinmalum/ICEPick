package app

import (
	"context"
	"fmt"
	"strings"

	"icepick/internal/storage"
)

// ListProfiles returns every profile, built-in default first.
func (s *Service) ListProfiles() ([]storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListProfiles(context.Background())
}

// GetProfile returns one profile for clients that need the current revision
// before an edit.
func (s *Service) GetProfile(id string) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	return s.store.GetProfile(context.Background(), id)
}

// CreateProfile saves a new profile: blank template when fromID is empty,
// otherwise an independent copy of the referenced profile. A copy keeps the
// resource references (MCP servers, skills) but never instance identity,
// history or authorization, because a profile holds none of those.
func (s *Service) CreateProfile(name, fromID string) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	ctx := context.Background()
	if strings.TrimSpace(fromID) == "" {
		return s.store.CreateProfile(ctx, name, storage.BlankProfile())
	}
	source, err := s.store.GetProfile(ctx, fromID)
	if err != nil {
		return storage.Profile{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = source.Name + " 副本"
	}
	return s.store.CreateProfile(ctx, name, source.Data)
}

// UpdateProfile saves a new combination atomically. Before persisting, the
// capability references are checked against what this host actually offers:
// enabled skills must be installed and enabled MCP servers must be
// configured, so a saved profile never points at resources that cannot load.
func (s *Service) UpdateProfile(p storage.Profile) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	if err := s.validateProfileResources(p.Data); err != nil {
		return storage.Profile{}, err
	}
	return s.store.UpdateProfile(context.Background(), p)
}

// RestoreProfileDefault resets the built-in default profile's combination to
// the shipped content.
func (s *Service) RestoreProfileDefault(revision int) (storage.Profile, error) {
	if err := s.checkReady(); err != nil {
		return storage.Profile{}, err
	}
	return s.store.RestoreDefaultProfileData(context.Background(), revision)
}

// DeleteProfile archives a profile while preserving existing history.
func (s *Service) DeleteProfile(id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.DeleteProfile(context.Background(), id)
}

// validateProfileResources checks the profile's capability selections against
// the resources this host currently offers.
func (s *Service) validateProfileResources(data storage.ProfileData) error {
	if !data.AllSkills {
		for _, name := range data.EnabledSkills {
			if !s.skills.Installed(name) {
				return fmt.Errorf("未安装的 Skill %q", name)
			}
		}
	}
	if !data.AllMCPServers {
		s.mu.RLock()
		cfg := s.config
		s.mu.RUnlock()
		configured := map[string]bool{"local": true}
		for _, server := range cfg.MCPServers {
			configured[server.Name] = true
		}
		for _, name := range data.EnabledMCPServers {
			if !configured[name] {
				return fmt.Errorf("未接入的 MCP 服务 %q", name)
			}
		}
	}
	return nil
}
