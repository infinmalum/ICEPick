package main

import (
	"encoding/base64"

	"icepick/internal/app"
	"icepick/internal/config"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

// StatusService is the Wails binding surface. It owns no run state: every
// method delegates to the UI-agnostic app service so the CLI/IPC client
// drives the exact same code path.
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
func (s *StatusService) ListMessages(sessionID string) ([]storage.Message, error) {
	return s.svc.ListMessages(sessionID)
}

func (s *StatusService) ListToolCalls(sessionID string) ([]storage.ToolCall, error) {
	return s.svc.ListToolCalls(sessionID)
}

// ResolveToolCall records a user disposition for a result-unknown call.
func (s *StatusService) ResolveToolCall(callID, state, result, note string) (storage.ToolCall, error) {
	return s.svc.ResolveToolCall(callID, state, result, note)
}

// ApproveToolCall grants one pending tool call; the approval binds to that
// exact call and its arguments.
func (s *StatusService) ApproveToolCall(callID string) (storage.ToolCall, error) {
	return s.svc.ApproveToolCall(callID)
}

// DenyToolCall refuses one pending tool call; the denial is returned to the
// model as a tool error.
func (s *StatusService) DenyToolCall(callID, note string) (storage.ToolCall, error) {
	return s.svc.DenyToolCall(callID, note)
}

// ListExfilGrants shows the destinations data may be sent to and the scopes
// each grant covers.
func (s *StatusService) ListExfilGrants(contextID string) ([]storage.ExfilGrant, error) {
	return s.svc.ListExfilGrants(contextID)
}

// GrantExfil authorizes sending the listed data scopes to one endpoint.
func (s *StatusService) GrantExfil(contextID, destination string, scopes []string) (storage.ExfilGrant, error) {
	return s.svc.GrantExfil(contextID, destination, scopes)
}

// RevokeExfil removes the authorization for one destination.
func (s *StatusService) RevokeExfil(contextID, destination string) error {
	return s.svc.RevokeExfil(contextID, destination)
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
func (s *StatusService) PendingStreamText(runID string) (string, error) {
	return s.svc.PendingStreamText(runID)
}

// ListMemories returns one context's long-term memories, newest first.
func (s *StatusService) ListMemories(contextID string) ([]storage.Memory, error) {
	return s.svc.ListMemories(contextID)
}

// AddMemory records a user-authored memory for the agent.
func (s *StatusService) AddMemory(contextID, content string) (storage.Memory, error) {
	return s.svc.AddMemory(contextID, content)
}

// UpdateMemory edits the content of one memory.
func (s *StatusService) UpdateMemory(contextID, id, content string) (storage.Memory, error) {
	return s.svc.UpdateMemory(contextID, id, content)
}

// DeleteMemory removes one memory after the user's explicit decision.
func (s *StatusService) DeleteMemory(contextID, id string) error {
	return s.svc.DeleteMemory(contextID, id)
}

// ExportMemories renders the context's memory as a portable markdown document
// that keeps the source and scope annotations.
func (s *StatusService) ExportMemories(contextID string) (string, error) {
	return s.svc.ExportMemories(contextID)
}

// GetSessionSummary returns the session's compaction summary with its
// covered range and scope; an absent summary is an error.
func (s *StatusService) GetSessionSummary(sessionID string) (storage.SessionSummary, error) {
	return s.svc.GetSessionSummary(sessionID)
}

// GetSessionPlan returns the session's resumable plan with parsed steps.
func (s *StatusService) GetSessionPlan(sessionID string) (app.SessionPlanView, error) {
	return s.svc.GetSessionPlan(sessionID)
}

// ClearSessionPlan removes the session's plan; it never touches approvals,
// permissions or history.
func (s *StatusService) ClearSessionPlan(sessionID string) error {
	return s.svc.ClearSessionPlan(sessionID)
}

// ListMCPEndpoints shows each attached MCP server's source, process command,
// tool list and fingerprints, with a change marker when a fingerprint no
// longer matches the registry.
func (s *StatusService) ListMCPEndpoints() ([]app.MCPEndpointStatus, error) {
	return s.svc.ListMCPEndpoints()
}

// ListConfiguredMCPServers returns the configured MCP server catalog that
// assistant editing picks enabled servers from.
func (s *StatusService) ListConfiguredMCPServers() []config.MCPServer {
	return s.svc.ListConfiguredMCPServers()
}

// SaveMCPServer validates, persists and connects one MCP server: adding it,
// or replacing the entry with the same name. The command runs immediately so
// configuration mistakes surface here and not mid-task.
func (s *StatusService) SaveMCPServer(server config.MCPServer) ([]config.MCPServer, error) {
	return s.svc.SaveMCPServer(server)
}

// DeleteMCPServer removes one configured server, stops its process and drops
// its fingerprint registration.
func (s *StatusService) DeleteMCPServer(name string) ([]config.MCPServer, error) {
	return s.svc.DeleteMCPServer(name)
}

// ConfirmMCPEndpoint re-trusts one server's current fingerprints after the
// user reviewed the change; automatic execution resumes.
func (s *StatusService) ConfirmMCPEndpoint(name string) ([]app.MCPEndpointStatus, error) {
	return s.svc.ConfirmMCPEndpoint(name)
}

// ListSkills returns the installed skill catalog that assistant editing
// picks enabled skills from.
func (s *StatusService) ListSkills() ([]app.SkillInfo, error) {
	return s.svc.ListSkills()
}

// ReadHostLogs returns the recent lines of the host log, oldest first —
// the desktop counterpart of `icepick logs`.
func (s *StatusService) ReadHostLogs(limit int) ([]string, error) {
	return s.svc.ReadHostLogs(limit)
}

// ListWorkerTraces returns one root run's worker work history for the
// developer view: delegated tool calls and explicit observations.
func (s *StatusService) ListWorkerTraces(runID, query string) ([]storage.WorkerTrace, error) {
	return s.svc.ListWorkerTraces(runID, query)
}

// ListRuns returns the runs of one session, newest first.
func (s *StatusService) ListRuns(sessionID string) ([]storage.Run, error) {
	return s.svc.ListRuns(sessionID)
}

func (s *StatusService) GetRun(id string) (storage.Run, error) {
	return s.svc.GetRun(id)
}

// ListChildRuns returns the delegated subtask runs of one run for the task
// tree; all components share the root Context.
func (s *StatusService) ListChildRuns(runID string) ([]storage.Run, error) {
	return s.svc.ListChildRuns(runID)
}

// PendingApprovals sweeps every session of the context — normal and hidden
// delegation sessions — for calls needing user action.
func (s *StatusService) PendingApprovals(contextID string) ([]storage.ToolCall, error) {
	return s.svc.PendingApprovals(contextID)
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
// never creates or switches contexts, never installs resources and never
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

func (s *StatusService) CancelTask(id string) error {
	return s.svc.CancelTask(id)
}

func (s *StatusService) StartProfileDraft(sessionID, target, request string, evidenceIDs []string) (storage.Run, error) {
	return s.svc.StartProfileDraft(sessionID, target, request, evidenceIDs)
}
func (s *StatusService) ListProfileDrafts() ([]storage.ProfileDraft, error) {
	return s.svc.ListProfileDrafts()
}
func (s *StatusService) EditProfileDraft(id string, revision int, proposal storage.ProfileProposal) (storage.ProfileDraft, error) {
	return s.svc.EditProfileDraft(id, revision, proposal)
}
func (s *StatusService) ApplyProfileDraft(id string, revision int) (storage.Profile, error) {
	return s.svc.ApplyProfileDraft(id, revision)
}
func (s *StatusService) DiscardProfileDraft(id string, revision int) error {
	return s.svc.DiscardProfileDraft(id, revision)
}
func (s *StatusService) RevertProfileDraft(id string, revision int) (storage.Profile, error) {
	return s.svc.RevertProfileDraft(id, revision)
}

func (s *StatusService) StartSkillDraft(sessionID, sourceName, request string, evidenceIDs []string) (storage.Run, error) {
	return s.svc.StartSkillDraft(sessionID, sourceName, request, evidenceIDs)
}
func (s *StatusService) ListSkillDrafts() ([]storage.SkillDraft, error) {
	return s.svc.ListSkillDrafts()
}
func (s *StatusService) EditSkillDraft(id string, revision int, proposal storage.SkillProposal) (storage.SkillDraft, error) {
	return s.svc.EditSkillDraft(id, revision, proposal)
}
func (s *StatusService) ApplySkillDraft(id string, revision int) (skills.Snapshot, error) {
	return s.svc.ApplySkillDraft(id, revision)
}
func (s *StatusService) DiscardSkillDraft(id string, revision int) error {
	return s.svc.DiscardSkillDraft(id, revision)
}
func (s *StatusService) WithdrawSkillDraft(id string, revision int) error {
	return s.svc.WithdrawSkillDraft(id, revision)
}
