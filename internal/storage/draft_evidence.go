package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type DraftEvidence struct {
	RootRunID string                `json:"rootRunId"`
	Binding   string                `json:"binding"`
	Snapshot  RetrospectiveSnapshot `json:"snapshot"`
}

func (s *Store) CaptureDraftEvidence(ctx context.Context, session string, ids []string) ([]DraftEvidence, error) {
	out := []DraftEvidence{}
	if len(ids) > 3 {
		return nil, errors.New("最多选择3个已结束任务作为证据")
	}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var owner string
		if err := tx.QueryRowContext(ctx, `SELECT context_id FROM sessions WHERE id=?`, session).Scan(&owner); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, id := range ids {
			if id == "" || seen[id] {
				return errors.New("任务证据ID为空或重复")
			}
			seen[id] = true
			var targetOwner string
			if err := tx.QueryRowContext(ctx, `SELECT s.context_id FROM runs r JOIN sessions s ON s.id=r.session_id WHERE r.id=? AND r.parent_run_id IS NULL AND r.status<>'running'`, id).Scan(&targetOwner); err != nil {
				return err
			}
			if owner != targetOwner {
				return errors.New("任务证据不属于当前上下文")
			}
			snap, err := retrospectiveSnapshotInTx(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, DraftEvidence{RootRunID: id, Binding: snap.Binding, Snapshot: snap})
		}
		return nil
	})
	return out, err
}
func (s *Store) SaveDraftEvidence(ctx context.Context, run string, evidence []DraftEvidence) error {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	if len(raw) > 512*1024 {
		return errors.New("任务证据超过512KiB")
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM runs WHERE id=? AND component='improver' AND status='running' AND parent_run_id IS NULL`, run).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("配置草案运行无效")
		}
		if err := validateEvidenceInTx(ctx, tx, evidence); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO draft_evidence(run_id,payload) VALUES(?,?)`, run, string(raw))
		return err
	})
}
func validateEvidenceInTx(ctx context.Context, tx *sql.Tx, evidence []DraftEvidence) error {
	for _, item := range evidence {
		current, err := retrospectiveSnapshotInTx(ctx, tx, item.RootRunID)
		if err != nil {
			return err
		}
		if current.Binding != item.Binding {
			return errors.New("选定任务证据已变化，请重新生成草案")
		}
	}
	return nil
}
func validateDraftEvidenceInTx(ctx context.Context, tx *sql.Tx, run string) error {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT payload FROM draft_evidence WHERE run_id=?`, run).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var items []DraftEvidence
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return err
	}
	return validateEvidenceInTx(ctx, tx, items)
}
func (s *Store) DraftEvidence(ctx context.Context, run string) ([]DraftEvidence, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM draft_evidence WHERE run_id=?`, run).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []DraftEvidence{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []DraftEvidence
	err = json.Unmarshal([]byte(raw), &items)
	return items, err
}

func validateDraftRefsInTx(ctx context.Context, tx *sql.Tx, run string, refs []string) error {
	if len(refs) > 64 {
		return errors.New("草案证据引用过多")
	}
	allowed := map[string]bool{"request": true, "base": true}
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT payload FROM draft_evidence WHERE run_id=?`, run).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		var evidence []DraftEvidence
		if err = json.Unmarshal([]byte(raw), &evidence); err != nil {
			return err
		}
		for _, e := range evidence {
			for _, r := range e.Snapshot.Runs {
				allowed["run:"+r.ID] = true
			}
			for _, s := range e.Snapshot.Steps {
				allowed["step:"+s.ID] = true
			}
			for _, m := range e.Snapshot.Messages {
				allowed["message:"+m.ID] = true
			}
			for _, c := range e.Snapshot.Calls {
				allowed["call:"+c.ID] = true
			}
			for _, t := range e.Snapshot.Graph.Tasks {
				allowed["task:"+t.Spec.ID] = true
			}
			for _, a := range e.Snapshot.Graph.Artifacts {
				allowed["artifact:"+a.ID] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if !allowed[ref] || seen[ref] {
			return errors.New("草案证据引用不存在、未获选择或重复")
		}
		seen[ref] = true
	}
	return nil
}
