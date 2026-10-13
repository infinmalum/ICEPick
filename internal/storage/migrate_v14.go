package storage

import (
	"context"
	"database/sql"
	"encoding/json"
)

func migratePlansV14(ctx context.Context, tx *sql.Tx) error {
	for _, query := range []string{
		`ALTER TABLE session_plans ADD COLUMN revision INTEGER NOT NULL DEFAULT 1`,
		`CREATE TABLE session_plan_revisions(session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,revision INTEGER NOT NULL,plan TEXT NOT NULL CHECK(json_valid(plan)),created_at TEXT NOT NULL,PRIMARY KEY(session_id,revision))`,
	} {
		if _, err := tx.ExecContext(ctx, query); err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_id,agent_id,plan,updated_at,revision FROM session_plans`)
	if err != nil {
		return err
	}
	var plans []SessionPlan
	for rows.Next() {
		var p SessionPlan
		if err := rows.Scan(&p.SessionID, &p.AgentID, &p.Plan, &p.UpdatedAt, &p.Revision); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range plans {
		var steps []PlanStep
		if err := json.Unmarshal([]byte(p.Plan), &steps); err != nil {
			return err
		}
		steps, err = assignPlanIDs(steps, nil)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(steps)
		if err != nil {
			return err
		}
		p.Plan = string(raw)
		if _, err := tx.ExecContext(ctx, `UPDATE session_plans SET plan=? WHERE session_id=?`, p.Plan, p.SessionID); err != nil {
			return err
		}
		if err := recordPlanRevision(ctx, tx, p); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `PRAGMA user_version=14`)
	return err
}
