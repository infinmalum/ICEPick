package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

type WorkerTrace struct {
	ID           string `json:"id"`
	RunID        string `json:"runId"`
	TaskID       string `json:"taskId"`
	StepID       string `json:"stepId"`
	Tool         string `json:"tool"`
	Arguments    string `json:"arguments,omitempty"`
	Content      string `json:"content,omitempty"`
	Error        string `json:"error,omitempty"`
	Scope        string `json:"scope"`
	State        string `json:"state"`
	Verification string `json:"verification"`
	CreatedAt    string `json:"createdAt"`
}

func (s *Store) WorkerTraces(ctx context.Context, viewer, q string) ([]WorkerTrace, error) {
	items := []WorkerTrace{}
	if len(q) > 200 {
		return items, errors.New("过程检索词过长")
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		root, err := rootInTx(ctx, tx, viewer)
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `WITH RECURSIVE tree(id) AS(SELECT ? UNION ALL SELECT r.id FROM runs r JOIN tree ON r.parent_run_id=tree.id)
SELECT c.id,c.run_id,COALESCE(a.task_id,''),c.step_id,c.tool_name,c.arguments,c.result,c.error,c.source_scope,c.state,c.created_at FROM tool_calls c JOIN runs r ON r.id=c.run_id LEFT JOIN task_attempts a ON a.run_id=r.id WHERE c.run_id IN tree AND c.run_id<>? AND r.component='worker' AND r.worker_name<>'@planner' AND (?='' OR instr(lower(c.result||c.error||c.arguments),lower(?))>0)
UNION ALL SELECT o.id,o.run_id,COALESCE(a.task_id,''),'','observation','',o.content,'',o.source_scope,'recorded',o.created_at FROM worker_observations o LEFT JOIN task_attempts a ON a.run_id=o.run_id WHERE o.run_id IN tree AND o.run_id<>? AND (?='' OR instr(lower(o.content),lower(?))>0) ORDER BY 11 DESC,1 LIMIT 200`, root, viewer, q, q, viewer, q, q)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item WorkerTrace
			if err := rows.Scan(&item.ID, &item.RunID, &item.TaskID, &item.StepID, &item.Tool, &item.Arguments, &item.Content, &item.Error, &item.Scope, &item.State, &item.CreatedAt); err != nil {
				return err
			}
			if item.Scope == "" {
				item.Scope = ScopeUnknown
			}
			item.Verification = "unreviewed"
			if item.State == CallExecutionUnknow {
				item.Verification = "unknown_outcome"
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	return items, err
}
func (s *Store) RecordObservation(ctx context.Context, run, content, scope string) (WorkerTrace, error) {
	var item WorkerTrace
	if strings.TrimSpace(content) == "" || len(content) > 8000 {
		return item, errors.New("观察内容必须为 1 至 8000 字节")
	}
	id, err := newID()
	if err != nil {
		return item, err
	}
	item = WorkerTrace{ID: id, RunID: run, Tool: "observation", Content: content, Scope: scope, State: "recorded", Verification: "unreviewed", CreatedAt: now()}
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var status, kind string
		if err := tx.QueryRowContext(ctx, `SELECT status,component FROM runs WHERE id=?`, run).Scan(&status, &kind); err != nil {
			return err
		}
		if status != "running" || kind != "worker" {
			return errors.New("只有运行中的 worker 可以记录观察")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO worker_observations(id,run_id,content,source_scope,created_at) VALUES(?,?,?,?,?)`, id, run, content, scope, item.CreatedAt); err != nil {
			return err
		}
		root, err := rootInTx(ctx, tx, run)
		if err != nil {
			return err
		}
		return signalCoordination(ctx, tx, root, "observation")
	})
	return item, err
}
