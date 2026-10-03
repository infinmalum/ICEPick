package app

import (
	"context"
	"database/sql"
	"errors"
	"log"

	"icepick/internal/mcptools"
	"icepick/internal/storage"
)

// MCPEndpointSource is the hub surface the service needs to display MCP
// servers and keep their fingerprints registered. *mcptools.Hub satisfies
// it; tests can supply fakes.
type MCPEndpointSource interface {
	EndpointInfos() []mcptools.EndpointInfo
}

// MCPEndpointStatus is what the UI shows per attached MCP server: where it
// comes from (command), what it offers (tools), its fingerprints and whether
// either fingerprint changed since the last registration (in which case
// automatic execution is suspended until re-confirmed).
type MCPEndpointStatus struct {
	Name         string   `json:"name"`
	Command      string   `json:"command"`
	Args         []string `json:"args"`
	Version      string   `json:"version"`
	BinarySHA256 string   `json:"binarySha256"`
	SchemaSHA256 string   `json:"schemaSha256"`
	Tools        []string `json:"tools"`
	Changed      bool     `json:"changed"`
}

// SyncMCPEndpoints compares each attached server against the fingerprint
// registry and records what is now live. A mismatch marks the endpoint as
// changed: the permission gate then suspends automatic execution for its
// tools until a user re-confirms. It must run after the hub connected and
// after Definitions populated the schema hashes.
func (s *Service) SyncMCPEndpoints(ctx context.Context) error {
	if s.store == nil || s.endpoints == nil {
		return nil
	}
	// Populate the per-endpoint tool lists and schema hashes first.
	if _, err := s.tools.Definitions(ctx); err != nil {
		return err
	}
	infos := s.endpoints.EndpointInfos()
	changed := map[string]bool{}
	for _, info := range infos {
		stored, err := s.store.GetMCPEndpoint(ctx, info.Name)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && (stored.BinarySHA256 != info.BinarySHA256 || stored.SchemaSHA256 != info.SchemaSHA256) {
			changed[info.Name] = true
			log.Printf("MCP %s 指纹变化：二进制 %s→%s，schema %s→%s", info.Name, shortHash(stored.BinarySHA256), shortHash(info.BinarySHA256), shortHash(stored.SchemaSHA256), shortHash(info.SchemaSHA256))
		}
		if err := s.store.SetMCPEndpoint(ctx, storage.MCPEndpoint{
			Name: info.Name, Command: info.Command, BinarySHA256: info.BinarySHA256,
			SchemaSHA256: info.SchemaSHA256, Version: info.Version,
		}); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.mcpChanged = changed
	s.mu.Unlock()
	return nil
}

func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12] + "…"
	}
	return hash
}

func (s *Service) endpointChanged(server string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mcpChanged[server]
}

// ListMCPEndpoints merges the live hub view with the persisted registry for
// display. The local in-process server is reported like any other source.
func (s *Service) ListMCPEndpoints() ([]MCPEndpointStatus, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	infos := []mcptools.EndpointInfo{}
	if s.endpoints != nil {
		infos = s.endpoints.EndpointInfos()
	}
	items := make([]MCPEndpointStatus, 0, len(infos))
	for _, info := range infos {
		items = append(items, MCPEndpointStatus{
			Name: info.Name, Command: info.Command, Args: info.Args, Version: info.Version,
			BinarySHA256: info.BinarySHA256, SchemaSHA256: info.SchemaSHA256,
			Tools: info.Tools, Changed: s.endpointChanged(info.Name),
		})
	}
	return items, nil
}
