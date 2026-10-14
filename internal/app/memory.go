package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"icepick/internal/storage"
)

// The knowledge service surface: per-Context long-term memory management,
// the per-session compaction summary view and the resumable plan. Memory is
// context-owned data — listing, editing and deleting never cross contexts, and
// a profile change neither shares nor clears anything.

func (s *Service) ListMemories(contextID string) ([]storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return nil, err
	}
	if _, err := s.store.GetLocalContext(context.Background(), contextID); err != nil {
		return nil, err
	}
	return s.store.ListContextMemories(context.Background(), contextID, 0)
}

// AddMemory records a user-authored memory. It is personal data by
// definition: the user typed it here, for this local.
func (s *Service) AddMemory(contextID, content string) (storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return storage.Memory{}, err
	}
	if _, err := s.store.GetLocalContext(context.Background(), contextID); err != nil {
		return storage.Memory{}, err
	}
	return s.store.AddContextMemory(context.Background(), contextID, content, storage.MemorySourceUser, "", storage.ScopePersonal)
}

func (s *Service) UpdateMemory(contextID, id, content string) (storage.Memory, error) {
	if err := s.checkReady(); err != nil {
		return storage.Memory{}, err
	}
	return s.store.UpdateContextMemory(context.Background(), contextID, id, content)
}

func (s *Service) DeleteMemory(contextID, id string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.store.DeleteContextMemory(context.Background(), contextID, id)
}

// ExportMemories renders the context's memory as a portable markdown
// document. Export carries the source and scope annotations so the origin
// of every item survives the copy.
func (s *Service) ExportMemories(contextID string) (string, error) {
	memories, err := s.ListMemories(contextID)
	if err != nil {
		return "", err
	}
	local, err := s.store.GetLocalContext(context.Background(), contextID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# ICEPick 长期记忆导出\n\n- Context: %s (%s)\n- 导出时间: %s\n- 条数: %d\n", local.Name, local.ID, time.Now().UTC().Format(time.RFC3339), len(memories))
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
func (s *Service) GetSessionSummary(sessionID string) (storage.SessionSummary, error) {
	if err := s.checkSession(sessionID); err != nil {
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
	ContextID string             `json:"contextId"`
	UpdatedAt string             `json:"updatedAt"`
	Steps     []storage.PlanStep `json:"steps"`
	Revision  int                `json:"revision"`
}

func (s *Service) GetSessionPlan(sessionID string) (SessionPlanView, error) {
	if err := s.checkSession(sessionID); err != nil {
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
	return SessionPlanView{SessionID: plan.SessionID, ContextID: plan.ContextID, UpdatedAt: plan.UpdatedAt, Steps: steps, Revision: plan.Revision}, nil
}

// ClearSessionPlan removes the session's plan. It never touches approvals,
// permissions or history: a plan is bookkeeping, not authority.
func (s *Service) ClearSessionPlan(sessionID string) error {
	if err := s.checkSession(sessionID); err != nil {
		return err
	}
	return s.store.ClearSessionPlan(context.Background(), sessionID)
}

func (s *Service) checkSession(sessionID string) error {
	if err := s.checkReady(); err != nil {
		return err
	}
	return s.checkSessionExists(sessionID)
}
