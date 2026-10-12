// Package app hosts the UI-agnostic application service: run orchestration,
// session and agent management, startup recovery and the single-host
// guarantee. The Wails binding layer and the future CLI/IPC client both talk
// to this service instead of owning any run state themselves.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

// Event is a post-commit notification about persisted state. Clients may
// receive duplicates; consumers deduplicate by session sequence and run
// status when they need exactly-once semantics.
type Event struct {
	Type      string `json:"type"` // run.started, run.updated, message.appended
	SessionID string `json:"sessionId"`
	RunID     string `json:"runId"`
	Detail    string `json:"detail,omitempty"`
}

type Options struct {
	Store      *storage.Store
	Runs       *agent.Runs
	Tools      agent.ToolExecutor
	Skills     *skills.Catalog
	Chat       agent.ChatModel
	Discovery  model.ModelLister
	Config     config.Config
	ConfigPath string
	// DataDir is the application data directory; it is protected as a
	// credential source by the tool classifier.
	DataDir string
	// MCPEndpoints is the attached MCP hub surface used to display servers
	// and keep their fingerprints registered. *mcptools.Hub satisfies it.
	MCPEndpoints MCPEndpointSource
	// CredentialPath is the data-directory file holding remote provider API
	// keys; empty disables the credential store (tests).
	CredentialPath string
	// InitError carries a startup failure (e.g. the database could not be
	// opened) so the UI can surface it while remaining operational enough
	// to report status.
	InitError error
}

type Service struct {
	store      *storage.Store
	runs       *agent.Runs
	tools      agent.ToolExecutor
	skills     *skills.Catalog
	chat       agent.ChatModel
	discovery  model.ModelLister
	configPath string
	initErr    error
	credStore  *credentialStore
	// protectedPaths are the locations the tool classifier always treats as
	// credential sources (the data dir, conventional credential stores).
	protectedPaths []string
	endpoints      MCPEndpointSource

	mu         sync.RWMutex
	config     config.Config
	mcpChanged map[string]bool

	// muStreams guards the in-memory stream merge buffer: per active run,
	// the assistant text streamed so far that is not yet durably committed.
	// Reconnecting clients read it together with the persisted state; a
	// host crash loses it by design (the run is marked interrupted).
	muStreams sync.Mutex
	streams   map[string]*strings.Builder

	// ApprovalTimeout bounds each tool approval wait inside the run loop.
	// Tests shrink it to exercise the timeout path.
	ApprovalTimeout time.Duration

	taskMu  sync.Mutex
	running map[string]string

	muApprovals sync.Mutex
	waiters     map[string]chan struct{}

	muSubs  sync.Mutex
	subs    map[int]func(Event)
	nextSub int
}

func New(opts Options) *Service {
	timeout := 5 * time.Minute
	return &Service{
		store: opts.Store, runs: opts.Runs, tools: opts.Tools, skills: opts.Skills,
		chat: opts.Chat, discovery: opts.Discovery, config: opts.Config,
		configPath: opts.ConfigPath, initErr: opts.InitError, running: map[string]string{},
		credStore:       newCredentialStore(opts.CredentialPath),
		protectedPaths:  mcptools.DefaultProtectedPaths(opts.DataDir),
		endpoints:       opts.MCPEndpoints,
		mcpChanged:      map[string]bool{},
		ApprovalTimeout: timeout,
		streams:         map[string]*strings.Builder{},
		waiters:         map[string]chan struct{}{},
		subs:            map[int]func(Event){},
	}
}

// Subscribe registers a listener for post-commit events and returns an
// unsubscribe function.
func (s *Service) Subscribe(fn func(Event)) func() {
	s.muSubs.Lock()
	defer s.muSubs.Unlock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	return func() {
		s.muSubs.Lock()
		defer s.muSubs.Unlock()
		delete(s.subs, id)
	}
}

func (s *Service) publish(evt Event) {
	s.muSubs.Lock()
	defer s.muSubs.Unlock()
	for _, fn := range s.subs {
		fn(evt)
	}
}

// Recover converges records left behind by an abrupt previous exit: running
// runs become interrupted, in-flight tool calls become execution_unknown and
// un-dispatched calls become cancelled. It must run before any new Run starts.
func (s *Service) Recover(ctx context.Context) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.RecoverInterrupted(ctx)
}

func (s *Service) checkReady() error {
	if s.store == nil {
		return errors.New("本地数据库未就绪")
	}
	return nil
}

func (s *Service) GetStatus() AppStatus {
	s.mu.RLock()
	source := s.config.Model
	s.mu.RUnlock()
	status := AppStatus{Name: "ICEPick", StorageReady: s.initErr == nil && s.store != nil, AgentReady: s.initErr == nil && s.store != nil && source.Name != "" && s.tools != nil, Model: source, AvailableModels: []string{}}
	if s.initErr != nil {
		status.Message = "本地数据初始化失败：" + s.initErr.Error()
		return status
	}
	if s.discovery == nil {
		status.Message = "模型发现未配置"
		return status
	}
	models, err := s.discovery.List(context.Background(), source)
	if err != nil {
		status.Message = "模型未连接：" + err.Error()
		return status
	}
	status.ModelConnected = true
	status.AvailableModels = models
	if len(models) == 0 {
		status.Message = "模型服务已连接，但未发现模型。请在模型服务中加载模型。"
	} else {
		status.Message = fmt.Sprintf("模型服务已连接，发现 %d 个模型。选好模型后可发送任务。", len(models))
	}
	return status
}

type AppStatus struct {
	Name            string       `json:"name"`
	StorageReady    bool         `json:"storageReady"`
	AgentReady      bool         `json:"agentReady"`
	ModelConnected  bool         `json:"modelConnected"`
	Model           config.Model `json:"model"`
	AvailableModels []string     `json:"availableModels"`
	Message         string       `json:"message"`
}

func (s *Service) SaveModelConfig(next config.Model) (AppStatus, error) {
	if err := s.checkReady(); err != nil {
		return AppStatus{}, err
	}
	s.mu.RLock()
	updated := s.config
	s.mu.RUnlock()
	updated.Model = next
	if err := config.Save(s.configPath, updated); err != nil {
		return AppStatus{}, err
	}
	s.mu.Lock()
	s.config.Model = next
	s.mu.Unlock()
	return s.GetStatus(), nil
}

func (s *Service) ListSessions() ([]storage.Session, error) {
	return s.ListAgentSessions(storage.DefaultAgentID)
}

func (s *Service) ListAgentSessions(agentID string) ([]storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListAgentSessions(context.Background(), agentID)
}

func (s *Service) CreateSession() (storage.Session, error) {
	return s.CreateAgentSession(storage.DefaultAgentID)
}

func (s *Service) CreateAgentSession(agentID string) (storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return storage.Session{}, err
	}
	return s.store.CreateAgentSession(context.Background(), agentID, "新会话")
}

// ListMessages returns the display projection of a session: the persisted
// messages plus one synthetic entry per tool call. The synthetic entries keep
// the transcript readable without reintroducing free-form tool rows; the
// authoritative records stay in messages and tool_calls.
func (s *Service) ListMessages(agentID, sessionID string) ([]storage.Message, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, sessionID); err != nil {
		return nil, err
	}
	ctx := context.Background()
	messages, err := s.store.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	calls, err := s.store.ListSessionToolCalls(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	byMessage := map[string][]storage.ToolCall{}
	for _, call := range calls {
		byMessage[call.MessageID] = append(byMessage[call.MessageID], call)
	}
	display := make([]storage.Message, 0, len(messages)+len(calls))
	for _, item := range messages {
		display = append(display, item)
		for _, call := range byMessage[item.ID] {
			display = append(display, storage.Message{
				ID: call.ID, SessionID: sessionID, AgentID: item.AgentID, RunID: call.RunID,
				Role: storage.RoleTool, Content: displayToolCall(call), CreatedAt: call.CreatedAt,
			})
		}
	}
	return display, nil
}

func displayToolCall(call storage.ToolCall) string {
	switch call.State {
	case storage.CallSucceeded:
		return call.ToolName + ": " + call.Result
	case storage.CallFailed:
		return call.ToolName + ": 工具错误：" + call.Error
	case storage.CallCancelled:
		return call.ToolName + ": 调用已取消：" + call.Error
	case storage.CallExecuting:
		return call.ToolName + ": 执行中…"
	case storage.CallApproved:
		return call.ToolName + ": 已批准，待执行"
	case storage.CallPending:
		return call.ToolName + ": 待审批"
	case storage.CallExecutionUnknow:
		return call.ToolName + ": 结果未知：" + call.Error
	default:
		return call.ToolName + ": 未知状态 " + call.State
	}
}

func (s *Service) ListRuns(agentID, sessionID string) ([]storage.Run, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, sessionID); err != nil {
		return nil, err
	}
	return s.store.ListRuns(context.Background(), sessionID)
}

func (s *Service) GetRun(agentID, id string) (storage.Run, error) {
	if err := s.checkReady(); err != nil {
		return storage.Run{}, err
	}
	run, err := s.store.GetRun(context.Background(), id)
	if err != nil {
		return storage.Run{}, err
	}
	if err := s.checkSessionAgent(agentID, run.SessionID); err != nil {
		return storage.Run{}, err
	}
	return run, nil
}

// ListChildRuns returns the delegated subtask runs of one run, oldest first.
// The task tree renders them under their parent with target Agent, status
// and error.
func (s *Service) ListChildRuns(agentID, runID string) ([]storage.Run, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	run, err := s.store.GetRun(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, run.SessionID); err != nil {
		return nil, err
	}
	return s.store.ListChildRuns(context.Background(), runID)
}

// ListToolCalls returns the persisted tool call records of a session, oldest
// first. These are the authoritative records the model history is rebuilt
// from.
func (s *Service) ListToolCalls(agentID, sessionID string) ([]storage.ToolCall, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, sessionID); err != nil {
		return nil, err
	}
	return s.store.ListSessionToolCalls(context.Background(), sessionID)
}

// PendingApprovals walks every session owned by the agent — including the
// hidden delegation sessions — and returns the calls needing user action:
// pending approvals and unresolved unknown-result calls.
func (s *Service) PendingApprovals(agentID string) ([]storage.ToolCall, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	if _, err := s.store.GetAgent(ctx, agentID); err != nil {
		return nil, err
	}
	sessions, err := s.store.ListAgentSessionsAll(ctx, agentID)
	if err != nil {
		return nil, err
	}
	pending := []storage.ToolCall{}
	for _, session := range sessions {
		calls, err := s.store.ListSessionToolCalls(ctx, session.ID)
		if err != nil {
			return nil, err
		}
		for _, call := range calls {
			if call.State == storage.CallPending || call.State == storage.CallExecutionUnknow {
				pending = append(pending, call)
			}
		}
	}
	return pending, nil
}

// ResolveToolCall records an explicit user disposition for a call whose
// result is unknown. Only the owning Agent may resolve it.
func (s *Service) ResolveToolCall(agentID, callID, state, result, note string) (storage.ToolCall, error) {
	if err := s.checkReady(); err != nil {
		return storage.ToolCall{}, err
	}
	call, err := s.store.GetToolCall(context.Background(), callID)
	if err != nil {
		return storage.ToolCall{}, err
	}
	run, err := s.store.GetRun(context.Background(), call.RunID)
	if err != nil {
		return storage.ToolCall{}, err
	}
	if err := s.checkSessionAgent(agentID, run.SessionID); err != nil {
		return storage.ToolCall{}, err
	}
	if err := s.store.ResolveToolCall(context.Background(), callID, state, result, note); err != nil {
		return storage.ToolCall{}, err
	}
	return s.store.GetToolCall(context.Background(), callID)
}

func (s *Service) checkSessionAgent(agentID, sessionID string) error {
	if agentID == "" {
		return errors.New("请选择 Agent")
	}
	session, err := s.store.GetSession(context.Background(), sessionID)
	if err != nil {
		return err
	}
	if session.AgentID != agentID {
		return errors.New("会话不属于所选 Agent")
	}
	return nil
}

func (s *Service) StartTask(sessionID, input string) (storage.Run, error) {
	return s.StartAgentTask(storage.DefaultAgentID, sessionID, input)
}

func (s *Service) StartAgentTask(agentID, sessionID, input string) (storage.Run, error) {
	if err := s.checkReady(); err != nil {
		return storage.Run{}, err
	}
	input = strings.TrimSpace(input)
	if input == "" || len(input) > 10000 {
		return storage.Run{}, errors.New("任务内容长度必须在 1 到 10000 字节之间")
	}
	profile, err := s.store.AgentForSession(context.Background(), sessionID)
	if err != nil {
		return storage.Run{}, err
	}
	if agentID == "" || agentID != profile.AgentID {
		return storage.Run{}, errors.New("会话不属于所选 Agent")
	}
	if s.runs == nil || s.tools == nil || s.chat == nil {
		return storage.Run{}, errors.New("Agent 尚未初始化")
	}
	// A session with unresolved tool calls must not auto-advance: the model
	// could continue a task whose side effects are unaccounted for, and a
	// retry under a fresh call ID would dodge that accounting.
	unresolved, err := s.store.HasUnresolvedToolCalls(context.Background(), sessionID)
	if err != nil {
		return storage.Run{}, err
	}
	if unresolved {
		return storage.Run{}, errors.New("会话存在结果未知的工具调用，请先处置后继续")
	}
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	if previous := s.running[sessionID]; previous != "" {
		prior, err := s.store.GetRun(context.Background(), previous)
		if err != nil || prior.Status == "running" {
			return storage.Run{}, errors.New("该会话已有运行中的任务")
		}
		delete(s.running, sessionID)
	}
	item, runCtx, resolved, err := s.runs.Start(context.Background(), sessionID)
	if err != nil {
		return storage.Run{}, err
	}
	if _, err := s.store.AddMessage(context.Background(), sessionID, item.ID, storage.RoleUser, input); err != nil {
		_ = s.runs.Finish(item.ID, "failed", err.Error())
		return storage.Run{}, err
	}
	s.mu.RLock()
	source := s.config.Model
	s.mu.RUnlock()
	if resolved.Model.Provider != "" {
		source = resolved.Model
	}
	if source.Name == "" {
		_ = s.runs.Finish(item.ID, "failed", "请先选择模型 ID")
		return storage.Run{}, errors.New("请先选择模型 ID")
	}
	var initialGrant storage.ExfilGrant
	if origin, local, err := endpointOrigin(source.EffectiveBaseURL()); err != nil {
		_ = s.runs.Finish(item.ID, "failed", err.Error())
		return storage.Run{}, err
	} else if !local {
		var grantErr error
		initialGrant, _, grantErr = s.store.GetExfilGrant(context.Background(), agentID, origin)
		if grantErr != nil {
			_ = s.runs.Finish(item.ID, "failed", grantErr.Error())
			return storage.Run{}, grantErr
		}
	}
	s.running[sessionID] = item.ID
	s.publish(Event{Type: "run.started", SessionID: sessionID, RunID: item.ID})
	go s.executeTask(runCtx, item.ID, sessionID, source, resolved, initialGrant)
	return item, nil
}

func (s *Service) executeTask(ctx context.Context, runID, sessionID string, source config.Model, profile storage.AgentProfile, initialGrant storage.ExfilGrant) {
	defer func() {
		s.taskMu.Lock()
		if s.running[sessionID] == runID {
			delete(s.running, sessionID)
		}
		s.taskMu.Unlock()
		s.clearStream(runID)
	}()
	history, err := s.buildRunContext(sessionID, profile)
	if err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	runTools := s.tools
	if hub, ok := s.tools.(*mcptools.Hub); ok {
		all, names := profile.AllMCPServers, profile.EnabledMCPServers
		if len(profile.AllowedTools) > 0 {
			all = false
			names = nil
			seen := map[string]bool{}
			for _, tool := range profile.AllowedTools {
				if profile.AllowsTool(tool) {
					server := serverOf(tool)
					if !seen[server] {
						names = append(names, server)
						seen[server] = true
					}
				}
			}
		}
		runTools = hub.ForServers(all, names)
	}
	runner := bookkeepingTools{inner: runTools, svc: s, profile: profile, sessionID: sessionID, runID: runID}
	definitions, err := runner.Definitions(ctx)
	if err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	if err := s.SyncMCPEndpoints(ctx); err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	allowedTools := []string{}
	for _, definition := range definitions {
		if profile.AllowsTool(definition.Function.Name) {
			allowedTools = append(allowedTools, definition.Function.Name)
		}
	}
	guarded := guardedChat{inner: s.chat, payloadCheck: func(c context.Context, m []model.Message, t []model.Tool) error {
		return s.checkPayload(c, source, profile.AgentID, sessionID, m, t)
	}, check: func(checkCtx context.Context) error {
		// Every model request re-checks the exfiltration scope: tool
		// results and policy changes since the previous turn apply.
		if err := s.exfilCheck(checkCtx, sessionID, source, profile); err != nil {
			return err
		}
		return s.checkInitialGrant(checkCtx, sessionID, source, profile, initialGrant)
	}}
	engine := agent.Engine{
		Model:        guarded,
		Tools:        runner,
		Recorder:     runRecorder{svc: s, runID: runID, sessionID: sessionID, classifier: s.classifierFor(profile)},
		AllowedTools: allowedTools,
		Gate:         toolGate{svc: s, agentID: profile.AgentID, snapshot: &profile, runID: runID},
		Approver:     s,
		OnDelta: func(text string) error {
			// Streamed fragments are broadcast live and merged in host
			// memory; the durable message commit stays with Recorder.
			s.appendStream(runID, sessionID, text)
			return nil
		},
		Compactor:      sessionCompactor{svc: s, runID: runID, sessionID: sessionID, chat: guarded},
		ContextBudget:  profile.ContextBudget,
		InitialSummary: history.summary,
	}
	s.mu.RLock()
	engine.ApprovalTimeout = s.ApprovalTimeout
	s.mu.RUnlock()
	_, err = engine.Run(ctx, source, history.history)
	s.publish(Event{Type: "run.updated", SessionID: sessionID, RunID: runID, Detail: "模型循环结束"})
	if errors.Is(ctx.Err(), context.Canceled) {
		return
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		_ = s.runs.Finish(runID, "failed", "任务超过 10 分钟上限")
		return
	}
	if err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	_ = s.runs.Finish(runID, "completed", "")
}

// appendStream merges one streamed fragment into the run's in-memory buffer
// and broadcasts it. The buffer only ever holds text that is not yet durably
// committed; Recorder clears it the moment the message lands.
func (s *Service) appendStream(runID, sessionID, text string) {
	s.muStreams.Lock()
	buffer, ok := s.streams[runID]
	if !ok {
		buffer = &strings.Builder{}
		s.streams[runID] = buffer
	}
	buffer.WriteString(text)
	s.muStreams.Unlock()
	s.publish(Event{Type: "stream.delta", SessionID: sessionID, RunID: runID, Detail: text})
}

// clearStream drops the run's in-memory text; it is either durable by now or
// the run is over.
func (s *Service) clearStream(runID string) {
	s.muStreams.Lock()
	delete(s.streams, runID)
	s.muStreams.Unlock()
}

// PendingStreamText returns the assistant text streamed for a run that has
// not been committed yet. Reconnecting clients render it together with the
// persisted messages; an empty result means everything is durable or the run
// is not streaming.
func (s *Service) PendingStreamText(agentID, runID string) (string, error) {
	if err := s.checkReady(); err != nil {
		return "", err
	}
	run, err := s.store.GetRun(context.Background(), runID)
	if err != nil {
		return "", err
	}
	if err := s.checkSessionAgent(agentID, run.SessionID); err != nil {
		return "", err
	}
	s.muStreams.Lock()
	defer s.muStreams.Unlock()
	if buffer, ok := s.streams[runID]; ok {
		return buffer.String(), nil
	}
	return "", nil
}

// classifierFor builds the effect/sensitivity classifier for one run from
// the agent's permission scope; the run keeps using this snapshot even if
// the policy changes mid-run.
func (s *Service) classifierFor(profile storage.AgentProfile) mcptools.Classifier {
	return mcptools.Classifier{Workspace: profile.Policy.Permissions.WorkspacePaths, Protected: s.protectedPaths}
}

func (s *Service) CancelTask(agentID, id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	run, err := s.GetRun(agentID, id)
	if err != nil {
		return err
	}
	if err := s.runs.Cancel(id); err != nil {
		return err
	}
	s.publish(Event{Type: "run.updated", SessionID: run.SessionID, RunID: id, Detail: "已请求取消"})
	return nil
}

// WaitIdle blocks until no session owned by this service is executing. Tests
// use it to avoid sleeping on the asynchronous run loop.
func (s *Service) WaitIdle() {
	for {
		s.taskMu.Lock()
		count := len(s.running)
		s.taskMu.Unlock()
		if count == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}
