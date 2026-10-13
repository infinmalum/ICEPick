package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"icepick/internal/storage"
)

// The P4 knowledge service surface: per-Agent long-term memory management,
// the per-session compaction summary view and the resumable plan. Memory is
// agent-owned data — listing, editing and deleting never cross agents, and
// a profile change neither shares nor clears anything.

func (s *Service) ListMemories(agentID string) ([]storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if _, err := s.store.GetAgent(context.Background(), agentID); err != nil {
		return nil, err
	}
	return s.store.ListAgentMemories(context.Background(), agentID, 0)
}

// AddMemory records a user-authored memory. It is personal data by
// definition: the user typed it here, for this agent.
func (s *Service) AddMemory(agentID, content string) (storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return storage.Memory{}, err
	}
	if _, err := s.store.GetAgent(context.Background(), agentID); err != nil {
		return storage.Memory{}, err
	}
	return s.store.AddAgentMemory(context.Background(), agentID, content, storage.MemorySourceUser, "", storage.ScopePersonal)
}

func (s *Service) UpdateMemory(agentID, id, content string) (storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return storage.Memory{}, err
	}
	return s.store.UpdateAgentMemory(context.Background(), agentID, id, content)
}

func (s *Service) DeleteMemory(agentID, id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.DeleteAgentMemory(context.Background(), agentID, id)
}

// ExportMemories renders the agent's memory as a portable markdown
// document. Export carries the source and scope annotations so the origin
// of every item survives the copy.
func (s *Service) ExportMemories(agentID string) (string, error) {
	memories, err := s.ListMemories(agentID)
	if err != nil {
		return "", err
	}
	agent, err := s.store.GetAgent(context.Background(), agentID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# ICEPick 长期记忆导出\n\n- Agent: %s (%s)\n- 导出时间: %s\n- 条数: %d\n", agent.Name, agent.ID, time.Now().UTC().Format(time.RFC3339), len(memories))
	for _, memory := range memories {
		session := memory.SourceSessionID
		if session == "" {
			session = "—"
		}
		fmt.Fprintf(&b, "\n## %s\n\n- 来源: %s\n- 数据范围: %s\n- 记录时间: %s\n- 来源会话: %s\n\n%s\n", memory.ID, memory.Source, memory.Scope, memory.CreatedAt, session, memory.Content)
	}
	return b.String(), nil
}

// GetSessionSummary returns the session's compaction summary; an absent
// summary is an error the clients render as "no summary".
func (s *Service) GetSessionSummary(agentID, sessionID string) (storage.SessionSummary, error) {
	if err := s.checkSession(agentID, sessionID); err != nil {
		return storage.SessionSummary{}, err
	}
	sum, found, err := s.store.GetSessionSummary(context.Background(), sessionID)
	if err != nil {
		return storage.SessionSummary{}, err
	}
	if !found {
		return storage.SessionSummary{}, fmt.Errorf("会话没有压缩摘要")
	}
	return sum, nil
}

// SessionPlanView is the client projection of a session plan: the parsed
// steps plus the metadata the review panel shows.
type SessionPlanView struct {
	SessionID string             `json:"sessionId"`
	AgentID   string             `json:"agentId"`
	UpdatedAt string             `json:"updatedAt"`
	Steps     []storage.PlanStep `json:"steps"`
	Revision  int                `json:"revision"`
}

func (s *Service) GetSessionPlan(agentID, sessionID string) (SessionPlanView, error) {
	if err := s.checkSession(agentID, sessionID); err != nil {
		return SessionPlanView{}, err
	}
	plan, found, err := s.store.GetSessionPlan(context.Background(), sessionID)
	if err != nil {
		return SessionPlanView{}, err
	}
	if !found {
		return SessionPlanView{}, fmt.Errorf("会话没有进行中的计划")
	}
	var steps []storage.PlanStep
	if err := json.Unmarshal([]byte(plan.Plan), &steps); err != nil {
		return SessionPlanView{}, fmt.Errorf("解析计划: %w", err)
	}
	return SessionPlanView{SessionID: plan.SessionID, AgentID: plan.AgentID, UpdatedAt: plan.UpdatedAt, Steps: steps, Revision: plan.Revision}, nil
}

// ClearSessionPlan removes the session's plan. It never touches approvals,
// permissions or history: a plan is bookkeeping, not authority.
func (s *Service) ClearSessionPlan(agentID, sessionID string) error {
	if err := s.checkSession(agentID, sessionID); err != nil {
		return err
	}
	return s.store.ClearSessionPlan(context.Background(), sessionID)
}

func (s *Service) checkSession(agentID, sessionID string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.checkSessionAgent(agentID, sessionID)
}
