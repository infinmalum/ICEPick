package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// PlanStep is one entry of a session's resumable plan. The model maintains
// the whole plan through local__update_plan; the host validates and persists
// it so a restart or a later run picks the plan back up.
type PlanStep struct {
	ID         string   `json:"id"`
	DependsOn  []string `json:"dependsOn,omitempty"`
	Acceptance string   `json:"acceptance,omitempty"`
	InputRefs  []string `json:"inputRefs,omitempty"`
	Title      string   `json:"title"`
	Status     string   `json:"status"`
	Note       string   `json:"note,omitempty"`
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
	Revision  int    `json:"revision"`
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
	ids := map[string]PlanStep{}
	for _, step := range steps {
		if step.ID == "" {
			continue
		} // New steps may omit ID; the host assigns one before validation.
		if !planID.MatchString(step.ID) {
			return errors.New("任务 ID 需使用文字、数字、下划线或连字符，最多 64 字符")
		}
		if _, exists := ids[step.ID]; exists {
			return errors.New("任务 ID 重复")
		}
		ids[step.ID] = step
	}
	for _, step := range steps {
		if len(step.Acceptance) > 4000 || len(step.InputRefs) > 20 || len(step.DependsOn) > 20 {
			return errors.New("任务条件或引用过长")
		}
		for _, ref := range step.InputRefs {
			if strings.TrimSpace(ref) == "" || len(ref) > 500 {
				return errors.New("无效输入引用")
			}
		}
		seen := map[string]bool{}
		for _, dep := range step.DependsOn {
			parent, ok := ids[dep]
			if !ok || dep == step.ID || seen[dep] {
				return errors.New("任务依赖缺失、自引用或重复")
			}
			seen[dep] = true
			if (step.Status == PlanInProgress || step.Status == PlanDone) && parent.Status != PlanDone {
				return errors.New("前置任务未完成，不能开始或完成后继任务")
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return errors.New("任务依赖存在环")
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range ids[id].DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

var planID = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]{0,63}$`)

func assignPlanIDs(steps []PlanStep) ([]PlanStep, error) {
	result := append([]PlanStep(nil), steps...)
	for i := range result {
		if result[i].ID != "" {
			continue
		}
		if result[i].ID == "" {
			id, err := newID()
			if err != nil {
				return nil, err
			}
			result[i].ID = id
		}
	}
	return result, nil
}

func (s *Store) CompareAndSetSessionPlan(ctx context.Context, sessionID string, steps []PlanStep, expected int) (SessionPlan, error) {
	var item SessionPlan
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		current, _, err := readSessionPlan(ctx, tx, sessionID)
		if err != nil {
			return err
		}
		if expected != current.Revision {
			raw, _ := json.Marshal(current)
			return fmt.Errorf("计划修订冲突；当前状态 %s", raw)
		}
		var old []PlanStep
		if current.Plan != "" {
			if err := json.Unmarshal([]byte(current.Plan), &old); err != nil {
				return err
			}
		}
		steps, err = assignPlanIDs(steps)
		if err != nil {
			return err
		}
		if err := ValidatePlanSteps(steps); err != nil {
			return err
		}
		// Started dependency tasks cannot disappear or change their execution
		// contract in a replacement. Notes and status may still advance.
		{
			next := map[string]PlanStep{}
			for _, step := range steps {
				next[step.ID] = step
			}
			for _, step := range old {
				if step.Status == PlanPending {
					continue
				}
				n, ok := next[step.ID]
				if !ok {
					return errors.New("不能移除已开始的任务；新任务请清除旧计划后创建")
				}
				if n.Status == PlanPending || (step.Status == PlanDone && n.Status != PlanDone) {
					return errors.New("已开始任务不能退回待执行，已完成任务不能重置；返工请添加新任务 ID")
				}
				if step.Title != n.Title || step.Acceptance != n.Acceptance || !samePlanRefs(step.DependsOn, n.DependsOn) || !samePlanRefs(step.InputRefs, n.InputRefs) {
					return errors.New("不能改写已开始任务的输入、依赖或验收条件")
				}
			}
		}
		payload, err := json.Marshal(steps)
		if err != nil {
			return err
		}
		item = SessionPlan{SessionID: sessionID, Plan: string(payload), UpdatedAt: now(), Revision: current.Revision + 1}
		if err := tx.QueryRowContext(ctx, `INSERT INTO session_plans(session_id,agent_id,plan,updated_at,revision)
   SELECT ?,agent_id,?,?,? FROM sessions WHERE id=?
   ON CONFLICT(session_id) DO UPDATE SET plan=excluded.plan,updated_at=excluded.updated_at,revision=excluded.revision
   RETURNING agent_id`, sessionID, item.Plan, item.UpdatedAt, item.Revision, sessionID).Scan(&item.AgentID); err != nil {
			return err
		}
		return recordPlanRevision(ctx, tx, item)
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = errors.New("会话不存在")
	}
	return item, err
}

func samePlanRefs(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

type planReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readSessionPlan(ctx context.Context, db planReader, sessionID string) (SessionPlan, bool, error) {
	item := SessionPlan{SessionID: sessionID, Plan: "[]"}
	err := db.QueryRowContext(ctx, `SELECT session_id,agent_id,plan,updated_at,revision FROM session_plans WHERE session_id=?`, sessionID).Scan(&item.SessionID, &item.AgentID, &item.Plan, &item.UpdatedAt, &item.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return item, false, nil
	}
	return item, err == nil && item.Plan != "[]", err
}
func (s *Store) GetSessionPlan(ctx context.Context, sessionID string) (SessionPlan, bool, error) {
	return readSessionPlan(ctx, s.db, sessionID)
}
func recordPlanRevision(ctx context.Context, tx *sql.Tx, item SessionPlan) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO session_plan_revisions(session_id,revision,plan,created_at) VALUES(?,?,?,?)`, item.SessionID, item.Revision, item.Plan, item.UpdatedAt)
	return err
}

// Clearing leaves a revision tombstone, so stale model updates cannot resurrect
// the old plan. Earlier revisions remain available for recovery and diagnosis.
func (s *Store) ClearSessionPlan(ctx context.Context, sessionID string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		item, found, err := readSessionPlan(ctx, tx, sessionID)
		if err != nil || !found {
			return err
		}
		item.Plan = "[]"
		item.Revision++
		item.UpdatedAt = now()
		if _, err := tx.ExecContext(ctx, `UPDATE session_plans SET plan=?,revision=?,updated_at=? WHERE session_id=?`, item.Plan, item.Revision, item.UpdatedAt, sessionID); err != nil {
			return err
		}
		return recordPlanRevision(ctx, tx, item)
	})
}
