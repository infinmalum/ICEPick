package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Data scope tokens used by the exfiltration gate. A scope describes where
// content in a session came from: user input is personal, tool results carry
// the sensitivity their tool access touched, and skills metadata is
// workspace-scoped.
const (
	ScopePublic     = "public"
	ScopeWorkspace  = "workspace"
	ScopePersonal   = "personal"
	ScopeCredential = "credential"
	ScopeUnknown    = "unknown"
)

var validScopes = map[string]bool{
	ScopePublic: true, ScopeWorkspace: true, ScopePersonal: true, ScopeCredential: true, ScopeUnknown: true,
}

// ValidScope reports whether scope is a known data scope token.
func ValidScope(scope string) bool { return validScopes[scope] }

// ExfilGrant is a persistent user authorization to send one destination
// (endpoint origin) the listed data scopes. A grant never widens what a tool
// may do; it only records that the user accepted this data leaving the
// machine for that endpoint.
type ExfilGrant struct {
	Destination string   `json:"destination"`
	Scopes      []string `json:"scopes"`
	CreatedAt   string   `json:"createdAt"`
}

func (s *Store) ListExfilGrants(ctx context.Context) ([]ExfilGrant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT destination, scopes, created_at FROM exfil_grants ORDER BY destination`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ExfilGrant{}
	for rows.Next() {
		item, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanGrant(scanner interface{ Scan(...any) error }) (ExfilGrant, error) {
	var item ExfilGrant
	var payload string
	if err := scanner.Scan(&item.Destination, &payload, &item.CreatedAt); err != nil {
		return ExfilGrant{}, err
	}
	if err := json.Unmarshal([]byte(payload), &item.Scopes); err != nil {
		return ExfilGrant{}, fmt.Errorf("解析外发授权范围: %w", err)
	}
	return item, nil
}

func (s *Store) GetExfilGrant(ctx context.Context, destination string) (ExfilGrant, bool, error) {
	item, err := scanGrant(s.db.QueryRowContext(ctx, `SELECT destination, scopes, created_at FROM exfil_grants WHERE destination = ?`, destination))
	if errors.Is(err, sql.ErrNoRows) {
		return ExfilGrant{}, false, nil
	}
	if err != nil {
		return ExfilGrant{}, false, err
	}
	return item, true, nil
}

// SetExfilGrant upserts the authorization for one destination.
func (s *Store) SetExfilGrant(ctx context.Context, grant ExfilGrant) error {
	if grant.Destination == "" || len(grant.Scopes) == 0 {
		return errors.New("外发授权需要目的地和至少一个数据范围")
	}
	for _, scope := range grant.Scopes {
		if !ValidScope(scope) {
			return fmt.Errorf("未知的数据范围 %q", scope)
		}
	}
	payload, err := json.Marshal(grant.Scopes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO exfil_grants (destination, scopes, created_at) VALUES (?, ?, ?)
		ON CONFLICT(destination) DO UPDATE SET scopes = excluded.scopes`, grant.Destination, string(payload), now())
	return err
}

func (s *Store) DeleteExfilGrant(ctx context.Context, destination string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM exfil_grants WHERE destination = ?`, destination)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err == nil && n == 0 {
		return errors.New("该目的地没有已登记的外发授权")
	}
	return nil
}

// SessionDataScopes reconstructs the accumulated data scope of a session's
// context from the persisted records: whether user input exists and which
// sensitivities successful tool results carried. The system prompt's skills
// summary is accounted for by the caller, which knows the profile.
func (s *Store) SessionDataScopes(ctx context.Context, sessionID string) (hasUserInput bool, toolScopes []string, err error) {
	var userCount int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE session_id = ? AND role = 'user' AND legacy = 0`, sessionID).Scan(&userCount); err != nil {
		return false, nil, err
	}
	hasUserInput = userCount > 0
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT c.source_scope FROM tool_calls c JOIN runs r ON r.id = c.run_id
		WHERE r.session_id = ? AND c.state = 'succeeded' AND c.source_scope <> '' ORDER BY c.source_scope`, sessionID)
	if err != nil {
		return hasUserInput, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return hasUserInput, nil, err
		}
		toolScopes = append(toolScopes, scope)
	}
	return hasUserInput, toolScopes, rows.Err()
}

// MCPEndpoint is the fingerprint registry of one configured stdio MCP
// server: the binary hash, the tools/schema hash observed at registration
// and the server-reported version. A mismatch against a live connection
// pauses automatic execution based on the old registration.
type MCPEndpoint struct {
	Name         string `json:"name"`
	Command      string `json:"command"`
	BinarySHA256 string `json:"binarySha256"`
	SchemaSHA256 string `json:"schemaSha256"`
	Version      string `json:"version"`
	UpdatedAt    string `json:"updatedAt"`
}

func (s *Store) GetMCPEndpoint(ctx context.Context, name string) (MCPEndpoint, error) {
	var item MCPEndpoint
	err := s.db.QueryRowContext(ctx, `SELECT name, command, binary_sha256, schema_sha256, version, updated_at FROM mcp_endpoints WHERE name = ?`, name).
		Scan(&item.Name, &item.Command, &item.BinarySHA256, &item.SchemaSHA256, &item.Version, &item.UpdatedAt)
	return item, err
}

func (s *Store) SetMCPEndpoint(ctx context.Context, item MCPEndpoint) error {
	if item.Name == "" {
		return errors.New("MCP 服务名为空")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_endpoints (name, command, binary_sha256, schema_sha256, version, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET command = excluded.command, binary_sha256 = excluded.binary_sha256, schema_sha256 = excluded.schema_sha256, version = excluded.version, updated_at = excluded.updated_at`,
		item.Name, item.Command, item.BinarySHA256, item.SchemaSHA256, item.Version, now())
	return err
}

func (s *Store) ListMCPEndpoints(ctx context.Context) ([]MCPEndpoint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, command, binary_sha256, schema_sha256, version, updated_at FROM mcp_endpoints ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MCPEndpoint{}
	for rows.Next() {
		var item MCPEndpoint
		if err := rows.Scan(&item.Name, &item.Command, &item.BinarySHA256, &item.SchemaSHA256, &item.Version, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
