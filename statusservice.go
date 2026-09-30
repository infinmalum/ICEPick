package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"icepick/internal/agent"
	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

type AppStatus struct {
	Name            string       `json:"name"`
	StorageReady    bool         `json:"storageReady"`
	AgentReady      bool         `json:"agentReady"`
	ModelConnected  bool         `json:"modelConnected"`
	Model           config.Model `json:"model"`
	AvailableModels []string     `json:"availableModels"`
	Message         string       `json:"message"`
}

type StatusService struct {
	store      *storage.Store
	initErr    error
	configPath string
	mu         sync.RWMutex
	config     config.Config
	discovery  model.Discovery
	runs       *agent.Runs
	tools      *mcptools.Hub
	skills     *skills.Catalog
	chat       agent.ChatModel
	taskMu     sync.Mutex
	running    map[string]string
}

func (s *StatusService) GetStatus() AppStatus {
	s.mu.RLock()
	source := s.config.Model
	s.mu.RUnlock()
	status := AppStatus{Name: "ICEPick", StorageReady: s.initErr == nil && s.store != nil, AgentReady: s.initErr == nil && s.store != nil && source.Name != "" && s.tools != nil, Model: source, AvailableModels: []string{}}
	if s.initErr != nil {
		status.Message = "本地数据初始化失败：" + s.initErr.Error()
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

func (s *StatusService) SaveModelConfig(next config.Model) (AppStatus, error) {
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

func (s *StatusService) checkReady() error {
	if s.initErr != nil {
		return s.initErr
	}
	if s.store == nil {
		return errors.New("本地数据库未就绪")
	}
	return nil
}

func (s *StatusService) ListSessions() ([]storage.Session, error) {
	return s.ListAgentSessions(storage.DefaultAgentID)
}

func (s *StatusService) ListAgents() ([]storage.AgentProfile, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListAgents(context.Background())
}

func (s *StatusService) CreateAgent(name, fromID string) (storage.AgentProfile, error) {
	if err := s.checkReady(); err != nil {
		return storage.AgentProfile{}, err
	}
	if fromID == "" {
		p := storage.BlankProfile()
		p.Name = strings.TrimSpace(name)
		return s.store.CreateAgent(context.Background(), "", p)
	}
	p, err := s.store.CreateAgent(context.Background(), fromID, storage.AgentProfile{})
	if err != nil {
		return p, err
	}
	if strings.TrimSpace(name) != "" {
		p.Name = strings.TrimSpace(name)
		return s.store.UpdateAgent(context.Background(), p)
	}
	return p, nil
}

func (s *StatusService) UpdateAgent(p storage.AgentProfile) (storage.AgentProfile, error) {
	if err := s.checkReady(); err != nil {
		return storage.AgentProfile{}, err
	}
	return s.store.UpdateAgent(context.Background(), p)
}

func (s *StatusService) RestoreAgentPreset(id string, revision int) (storage.AgentProfile, error) {
	if err := s.checkReady(); err != nil {
		return storage.AgentProfile{}, err
	}
	return s.store.RestorePreset(context.Background(), id, revision)
}

func (s *StatusService) ListAgentSessions(agentID string) ([]storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	return s.store.ListAgentSessions(context.Background(), agentID)
}

func (s *StatusService) CreateSession() (storage.Session, error) {
	return s.CreateAgentSession(storage.DefaultAgentID)
}

func (s *StatusService) CreateAgentSession(agentID string) (storage.Session, error) {
	if err := s.checkReady(); err != nil {
		return storage.Session{}, err
	}
	return s.store.CreateAgentSession(context.Background(), agentID, "新会话")
}

func (s *StatusService) ListMessages(agentID, sessionID string) ([]storage.Message, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, sessionID); err != nil {
		return nil, err
	}
	return s.store.ListMessages(context.Background(), sessionID)
}

func (s *StatusService) ListRuns(agentID, sessionID string) ([]storage.Run, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if err := s.checkSessionAgent(agentID, sessionID); err != nil {
		return nil, err
	}
	return s.store.ListRuns(context.Background(), sessionID)
}

func (s *StatusService) GetRun(agentID, id string) (storage.Run, error) {
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

func (s *StatusService) checkSessionAgent(agentID, sessionID string) error {
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

func (s *StatusService) StartTask(sessionID, input string) (storage.Run, error) {
	return s.StartAgentTask(storage.DefaultAgentID, sessionID, input)
}

func (s *StatusService) StartAgentTask(agentID, sessionID, input string) (storage.Run, error) {
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
	if agentID == "" || agentID != profile.ID {
		return storage.Run{}, errors.New("会话不属于所选 Agent")
	}
	s.mu.RLock()
	source := s.config.Model
	s.mu.RUnlock()
	if profile.Model.Provider != "" {
		source = profile.Model
	}
	if source.Name == "" {
		return storage.Run{}, errors.New("请先选择模型 ID")
	}
	if s.runs == nil || s.tools == nil || s.chat == nil {
		return storage.Run{}, errors.New("Agent 尚未初始化")
	}
	s.taskMu.Lock()
	defer s.taskMu.Unlock()
	if s.running == nil {
		s.running = map[string]string{}
	}
	if previous := s.running[sessionID]; previous != "" {
		prior, err := s.store.GetRun(context.Background(), previous)
		if err != nil || prior.Status == "running" {
			return storage.Run{}, errors.New("该会话已有运行中的任务")
		}
		delete(s.running, sessionID)
	}
	item, runCtx, err := s.runs.StartAtRevision(context.Background(), sessionID, profile.Revision)
	if err != nil {
		return storage.Run{}, err
	}
	if _, err := s.store.AddMessage(context.Background(), sessionID, "user", input); err != nil {
		_ = s.runs.Finish(item.ID, "failed", err.Error())
		return storage.Run{}, err
	}
	s.running[sessionID] = item.ID
	go s.executeTask(runCtx, item.ID, sessionID, source, profile)
	return item, nil
}

func (s *StatusService) executeTask(ctx context.Context, runID, sessionID string, source config.Model, profile storage.AgentProfile) {
	defer func() {
		s.taskMu.Lock()
		if s.running[sessionID] == runID {
			delete(s.running, sessionID)
		}
		s.taskMu.Unlock()
	}()
	stored, err := s.store.ListMessages(ctx, sessionID)
	if err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	history := []model.Message{{Role: "system", Content: profile.SystemPrompt}}
	if summary := s.skills.SummaryFor(profile.EnabledSkills); summary != "" {
		history[0].Content += "\n可用 Agent Skills（仅元数据）：\n" + summary + "\n相关任务先调用 local__load_skill 读取完整 SKILL.md。"
	}
	for _, item := range stored {
		if item.Role == "user" || item.Role == "assistant" {
			history = append(history, model.Message{Role: item.Role, Content: item.Content})
		}
	}
	allowedTools := []string{}
	for _, name := range profile.AllowedTools {
		if name == "local__current_time" || name == "local__load_skill" {
			allowedTools = append(allowedTools, name)
		}
	}
	engine := agent.Engine{Model: s.chat, Tools: s.tools, AllowedTools: allowedTools, CanCall: func(name, arguments string) bool {
		current, err := s.store.GetAgent(context.Background(), profile.ID)
		if err != nil {
			return false
		}
		found := false
		for _, tool := range current.AllowedTools {
			if tool == name {
				found = true
				break
			}
		}
		if !found || (name != "local__current_time" && name != "local__load_skill") {
			return false
		}
		if name == "local__load_skill" {
			var args struct {
				Name string `json:"name"`
			}
			if json.Unmarshal([]byte(arguments), &args) != nil {
				return false
			}
			for _, skill := range current.EnabledSkills {
				if skill == args.Name {
					return true
				}
			}
			return false
		}
		return true
	}, OnTool: func(name, result string) error {
		_, err := s.store.AddMessage(context.Background(), sessionID, "tool", name+": "+result)
		return err
	}}
	answer, err := engine.Run(ctx, source, history)
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
	if _, err := s.store.AddMessage(context.Background(), sessionID, "assistant", answer); err != nil {
		_ = s.runs.Finish(runID, "failed", err.Error())
		return
	}
	_ = s.runs.Finish(runID, "completed", "")
}

func (s *StatusService) CancelTask(agentID, id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	if _, err := s.GetRun(agentID, id); err != nil {
		return err
	}
	return s.runs.Cancel(id)
}
