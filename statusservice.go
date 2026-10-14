package main

import (
	"encoding/base64"

	"icepick/internal/app"
	"icepick/internal/config"
	"icepick/internal/storage"
)

// StatusService is the Wails binding surface. It owns no run state: every
// method delegates to the UI-agnostic app service so the CLI/IPC client
// (P3c) drives the exact same code path.
type StatusService struct{ svc *app.Service }

type AppStatus = app.AppStatus

func (s *StatusService) GetStatus() AppStatus { return s.svc.GetStatus() }

func (s *StatusService) SaveModelConfig(next config.Model) (AppStatus, error) {
	return s.svc.SaveModelConfig(next)
}

// ListProfiles returns every profile (the shareable, versioned combinations),
// built-in default first.
func (s *StatusService) ListProfiles() ([]storage.Profile, error) {
	return s.svc.ListProfiles()
}

// CreateProfile saves a new profile: blank template when fromID is empty,
// otherwise an independent copy of the referenced profile.
func (s *StatusService) CreateProfile(name, fromID string) (storage.Profile, error) {
	return s.svc.CreateProfile(name, fromID)
}

// UpdateProfile saves a new combination atomically and checks that the
// selected skills and MCP servers actually exist on this host.
func (s *StatusService) UpdateProfile(p storage.Profile) (storage.Profile, error) {
	return s.svc.UpdateProfile(p)
}

// RestoreProfileDefault resets the built-in default profile to the shipped
// combination.
func (s *StatusService) RestoreProfileDefault(revision int) (storage.Profile, error) {
	return s.svc.RestoreProfileDefault(revision)
}

// DeleteProfile archives a profile while preserving existing history.
func (s *StatusService) DeleteProfile(id string) error {
	return s.svc.DeleteProfile(id)
}

// ListMessages returns the display projection of a session: persisted
// messages plus one synthetic entry per tool call so the transcript shows
// tool activity without storing free-form tool rows again.
func (s *StatusService) ListMessages(agentID, sessionID string) ([]storage.Message, error) {
	return s.svc.ListMessages(agentID, sessionID)
}

func (s *StatusService) ListToolCalls(agentID, sessionID string) ([]storage.ToolCall, error) {
	return s.svc.ListToolCalls(agentID, sessionID)
}

// ResolveToolCall records a user disposition for a result-unknown call.
func (s *StatusService) ResolveToolCall(agentID, callID, state, result, note string) (storage.ToolCall, error) {
	return s.svc.ResolveToolCall(agentID, callID, state, result, note)
}

// ApproveToolCall grants one pending tool call; the approval binds to that
// exact call and its arguments.
func (s *StatusService) ApproveToolCall(agentID, callID string) (storage.ToolCall, error) {
	return s.svc.ApproveToolCall(agentID, callID)
}

// DenyToolCall refuses one pending tool call; the denial is returned to the
// model as a tool error.
func (s *StatusService) DenyToolCall(agentID, callID, note string) (storage.ToolCall, error) {
	return s.svc.DenyToolCall(agentID, callID, note)
}

// ListExfilGrants shows the destinations data may be sent to and the scopes
// each grant covers.
func (s *StatusService) ListExfilGrants(agentID string) ([]storage.ExfilGrant, error) {
	return s.svc.ListExfilGrants(agentID)
}

// GrantExfil authorizes sending the listed data scopes to one endpoint.
func (s *StatusService) GrantExfil(agentID, destination string, scopes []string) (storage.ExfilGrant, error) {
	return s.svc.GrantExfil(agentID, destination, scopes)
}

// RevokeExfil removes the authorization for one destination.
func (s *StatusService) RevokeExfil(agentID, destination string) error {
	return s.svc.RevokeExfil(agentID, destination)
}

// SaveCredential stores one remote provider's API key in the protected data
// directory. The key is write-only: no binding ever returns it.
func (s *StatusService) SaveCredential(provider, apiKey string) error {
	return s.svc.SaveCredential(provider, apiKey)
}

// DeleteCredential removes one remote provider's stored key.
func (s *StatusService) DeleteCredential(provider string) error {
	return s.svc.DeleteCredential(provider)
}

// ListCredentialStatus reports which remote providers have a stored key.
func (s *StatusService) ListCredentialStatus() ([]app.CredentialStatus, error) {
	return s.svc.ListCredentialStatus()
}

// PendingStreamText returns the streamed-but-uncommitted assistant text of a
// run, so a reconnecting client can pick the live transcript back up.
func (s *StatusService) PendingStreamText(agentID, runID string) (string, error) {
	return s.svc.PendingStreamText(agentID, runID)
}

// ListMemories returns one agent's long-term memories, newest first.
func (s *StatusService) ListMemories(agentID string) ([]storage.Memory, error) {
	return s.svc.ListMemories(agentID)
}

// AddMemory records a user-authored memory for the agent.
func (s *StatusService) AddMemory(agentID, content string) (storage.Memory, error) {
	return s.svc.AddMemory(agentID, content)
}

// UpdateMemory edits the content of one memory.
func (s *StatusService) UpdateMemory(agentID, id, content string) (storage.Memory, error) {
	return s.svc.UpdateMemory(agentID, id, content)
}

// DeleteMemory removes one memory after the user's explicit decision.
func (s *StatusService) DeleteMemory(agentID, id string) error {
	return s.svc.DeleteMemory(agentID, id)
}

// ExportMemories renders the agent's memory as a portable markdown document
// that keeps the source and scope annotations.
func (s *StatusService) ExportMemories(agentID string) (string, error) {
	return s.svc.ExportMemories(agentID)
}

// GetSessionSummary returns the session's compaction summary with its
// covered range and scope; an absent summary is an error.
func (s *StatusService) GetSessionSummary(agentID, sessionID string) (storage.SessionSummary, error) {
	return s.svc.GetSessionSummary(agentID, sessionID)
}

// GetSessionPlan returns the session's resumable plan with parsed steps.
func (s *StatusService) GetSessionPlan(agentID, sessionID string) (app.SessionPlanView, error) {
	return s.svc.GetSessionPlan(agentID, sessionID)
}

// ClearSessionPlan removes the session's plan; it never touches approvals,
// permissions or history.
func (s *StatusService) ClearSessionPlan(agentID, sessionID string) error {
	return s.svc.ClearSessionPlan(agentID, sessionID)
}

// ListMCPEndpoints shows each attached MCP server's source, process command,
// tool list and fingerprints, with a change marker when a fingerprint no
// longer matches the registry.
func (s *StatusService) ListMCPEndpoints() ([]app.MCPEndpointStatus, error) {
	return s.svc.ListMCPEndpoints()
}

// ListRuns returns the runs of one session, newest first.
func (s *StatusService) ListRuns(agentID, sessionID string) ([]storage.Run, error) {
	return s.svc.ListRuns(agentID, sessionID)
}

func (s *StatusService) GetRun(agentID, id string) (storage.Run, error) {
	return s.svc.GetRun(agentID, id)
}

// ListChildRuns returns the delegated subtask runs of one run for the task
// tree; children belong to the target Agent.
func (s *StatusService) ListChildRuns(agentID, runID string) ([]storage.Run, error) {
	return s.svc.ListChildRuns(agentID, runID)
}

// PendingApprovals sweeps every session of the agent — normal and hidden
// delegation sessions — for calls needing user action.
func (s *StatusService) PendingApprovals(agentID string) ([]storage.ToolCall, error) {
	return s.svc.PendingApprovals(agentID)
}

// ExportProfile bundles one profile into a zip share package (base64).
// Identity, policy, sessions, memories, credentials and approvals never
// enter the package; the pinned delegation target is stripped.
func (s *StatusService) ExportProfile(profileID string) (app.ExportResult, error) {
	return s.svc.ExportProfile(profileID)
}

// PreviewProfileImport parses a share package (base64) and reports the
// combination it would create plus its missing dependencies and
// authorization recommendations.
func (s *StatusService) PreviewProfileImport(dataBase64 string) (app.ImportPreview, error) {
	raw, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return app.ImportPreview{}, err
	}
	return s.svc.PreviewProfileImport(raw)
}

// ImportProfile creates a new profile from a share package (base64). It
// never creates or switches agents, never installs resources and never
// grants authorization.
func (s *StatusService) ImportProfile(dataBase64 string) (app.ImportResult, error) {
	raw, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return app.ImportResult{}, err
	}
	profile, preview, err := s.svc.ImportProfile(raw)
	if err != nil {
		return app.ImportResult{}, err
	}
	return app.ImportResult{Profile: profile, Preview: preview}, nil
}

func (s *StatusService) CancelTask(agentID, id string) error {
	return s.svc.CancelTask(agentID, id)
}
