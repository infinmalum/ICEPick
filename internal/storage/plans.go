package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// PlanStep is one entry of a session's resumable plan. The model maintains
// the whole plan through local__update_plan; the host validates and persists
// it so a restart or a later run picks the plan back up.
type PlanStep struct {
	Title  string `json:"title"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
}

// Valid plan step statuses.
const (
	PlanPending    = "pending"
	PlanInProgress = "in_progress"
	PlanDone       = "done"
)

// SessionPlan is the persisted plan of one session. A session never changes
// its owning Agent, so the plan belongs to that Agent alone.
type SessionPlan struct {
	SessionID string `json:"sessionId"`
	AgentID   string `json:"agentId"`
	Plan      string `json:"plan"` // JSON array of PlanStep
	UpdatedAt string `json:"updatedAt"`
}

// ValidatePlanSteps checks a plan the model or a client submitted: a bounded
// number of steps with real titles and known statuses.
func ValidatePlanSteps(steps []PlanStep) error {
	if len(steps) == 0 || len(steps) > 20 {
		return errors.New("计划步骤数量必须在 1 到 20 之间")
	}
	for _, step := range steps {
		if strings.TrimSpace(step.Title) == "" || len(step.Title) > 500 {
			return errors.New("计划步骤标题长度必须在 1 到 500 字节之间")
		}
		switch step.Status {
		case PlanPending, PlanInProgress, PlanDone:
		default:
			return fmt.Errorf("无效的计划状态 %q", step.Status)
		}
		if len(step.Note) > 1000 {
			return errors.New("计划备注过长")
		}
	}
	return nil
}

// SetSessionPlan replaces the session's plan. The agent ownership is derived
// from the session row in the same statement, so a plan can never be
// attributed to a foreign agent.
func (s *Store) SetSessionPlan(ctx context.Context, sessionID string, steps []PlanStep) (SessionPlan, error) {
	if err := ValidatePlanSteps(steps); err != nil {
		return SessionPlan{}, err
	}
	payload, err := json.Marshal(steps)
	if err != nil {
		return SessionPlan{}, err
	}
	item := SessionPlan{SessionID: sessionID, Plan: string(payload), UpdatedAt: now()}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `INSERT INTO session_plans (session_id, agent_id, plan, updated_at)
			SELECT ?, agent_id, ?, ? FROM sessions WHERE id = ?
			ON CONFLICT(session_id) DO UPDATE SET plan = excluded.plan, updated_at = excluded.updated_at
			RETURNING agent_id`,
			sessionID, item.Plan, item.UpdatedAt, sessionID).Scan(&item.AgentID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return SessionPlan{}, errors.New("会话不存在")
	}
	if err != nil {
		return SessionPlan{}, err
	}
	return item, nil
}

func (s *Store) GetSessionPlan(ctx context.Context, sessionID string) (SessionPlan, bool, error) {
	var item SessionPlan
	err := s.db.QueryRowContext(ctx, `SELECT session_id, agent_id, plan, updated_at FROM session_plans WHERE session_id = ?`, sessionID).
		Scan(&item.SessionID, &item.AgentID, &item.Plan, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SessionPlan{}, false, nil
	}
	if err != nil {
		return SessionPlan{}, false, err
	}
	return item, true, nil
}

// ClearSessionPlan removes the session's plan; clearing a session without a
// plan is a no-op so a stale review panel never errors.
func (s *Store) ClearSessionPlan(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM session_plans WHERE session_id = ?`, sessionID)
	return err
}
