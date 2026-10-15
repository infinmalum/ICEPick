package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// Proposals contain configuration only. Provenance is supplied by the host.
type ProfileProposal struct {
	EvidenceRefs   []string    `json:"evidenceRefs"`
	Name           string      `json:"name"`
	Data           ProfileData `json:"data"`
	Summary        string      `json:"summary"`
	Reasons        []string    `json:"reasons"`
	Limitations    []string    `json:"limitations"`
	ExpectedChecks []string    `json:"expectedChecks"`
}

func (p ProfileProposal) Validate() error {
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 100 || strings.TrimSpace(p.Summary) == "" || len(p.Summary) > 16000 || len(p.Reasons) > 32 || len(p.Limitations) > 32 || len(p.ExpectedChecks) > 32 {
		return errors.New("无效配置草案")
	}
	for _, items := range [][]string{p.Reasons, p.Limitations, p.ExpectedChecks} {
		for _, item := range items {
			if strings.TrimSpace(item) == "" || len(item) > 8000 {
				return errors.New("草案说明为空或过长")
			}
		}
	}
	if p.Data.TemplateID != "" || p.Data.TemplateVersion != 0 {
		return errors.New("AI 草案不能声明出厂模板来源")
	}
	return ValidateProfileData(p.Data)
}

type ProfileDraft struct {
	ID               string             `json:"id"`
	RunID            string             `json:"runId"`
	TargetProfileID  string             `json:"targetProfileId"`
	BaseRevision     int                `json:"baseRevision"`
	Base             Profile            `json:"base"`
	Request          string             `json:"request"`
	Proposal         ProfileProposal    `json:"proposal"`
	Revision         int                `json:"revision"`
	State            string             `json:"state"`
	AppliedProfileID string             `json:"appliedProfileId"`
	AppliedRevision  int                `json:"appliedRevision"`
	CreatedAt        string             `json:"createdAt"`
	UpdatedAt        string             `json:"updatedAt"`
	Evidence         []DraftEvidence    `json:"evidence"`
	Changes          []ProfileChange    `json:"changes"`
	Impact           []ProfileReference `json:"impact"`
	Conversations    int                `json:"conversations"`
}
type ProfileChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func draftChanges(base Profile, proposal ProfileProposal) []ProfileChange {
	old, _ := json.Marshal(base.Data)
	next, _ := json.Marshal(proposal.Data)
	var a, b map[string]json.RawMessage
	_ = json.Unmarshal(old, &a)
	_ = json.Unmarshal(next, &b)
	a["name"], _ = json.Marshal(base.Name)
	b["name"], _ = json.Marshal(proposal.Name)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := []string{}
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	changes := []ProfileChange{}
	for _, key := range sorted {
		if !bytes.Equal(a[key], b[key]) {
			before, after := a[key], b[key]
			if before == nil {
				before = json.RawMessage("null")
			}
			if after == nil {
				after = json.RawMessage("null")
			}
			changes = append(changes, ProfileChange{key, before, after})
		}
	}
	return changes
}

const draftColumns = `id,run_id,target_profile_id,base_revision,base,request,proposal,revision,state,applied_profile_id,applied_revision,created_at,updated_at`

func scanProfileDraft(row interface{ Scan(...any) error }) (ProfileDraft, error) {
	var d ProfileDraft
	var base, proposal string
	err := row.Scan(&d.ID, &d.RunID, &d.TargetProfileID, &d.BaseRevision, &base, &d.Request, &proposal, &d.Revision, &d.State, &d.AppliedProfileID, &d.AppliedRevision, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(base), &d.Base); err != nil {
		return d, err
	}
	if err = json.Unmarshal([]byte(proposal), &d.Proposal); err != nil {
		return d, err
	}
	d.Changes = draftChanges(d.Base, d.Proposal)
	return d, nil
}
func (s *Store) GetProfileDraft(ctx context.Context, id string) (ProfileDraft, error) {
	return scanProfileDraft(s.db.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM profile_drafts WHERE id=?`, id))
}
func (s *Store) ListProfileDrafts(ctx context.Context) ([]ProfileDraft, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+draftColumns+` FROM profile_drafts ORDER BY created_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProfileDraft{}
	for rows.Next() {
		d, e := scanProfileDraft(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].TargetProfileID != "" {
			if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE kind='normal' AND profile_id=?`, out[i].TargetProfileID).Scan(&out[i].Conversations); err != nil {
				return nil, err
			}
		}
	}
	for i := range out {
		out[i].Evidence, err = s.DraftEvidence(ctx, out[i].RunID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (s *Store) SaveProfileDraft(ctx context.Context, run string, base Profile, request string, proposal ProfileProposal) (ProfileDraft, error) {
	if err := proposal.Validate(); err != nil {
		return ProfileDraft{}, err
	}
	if strings.TrimSpace(request) == "" || len(request) > 32000 {
		return ProfileDraft{}, errors.New("配置需求无效")
	}
	id, err := newID()
	if err != nil {
		return ProfileDraft{}, err
	}
	raw, _ := json.Marshal(proposal)
	before, _ := json.Marshal(base)
	created := now()
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		var component, status string
		if err := tx.QueryRowContext(ctx, `SELECT component,status FROM runs WHERE id=? AND parent_run_id IS NULL`, run).Scan(&component, &status); err != nil {
			return err
		}
		if err := validateDraftRefsInTx(ctx, tx, run, proposal.EvidenceRefs); err != nil {
			return err
		}
		if err := validateDraftEvidenceInTx(ctx, tx, run); err != nil {
			return err
		}
		if component != "improver" || status != "running" {
			return errors.New("配置草案运行已经结束或组件无效")
		}
		if base.ID != "" {
			var current string
			if err := tx.QueryRowContext(ctx, `SELECT name FROM profiles WHERE id=?`, base.ID).Scan(&current); err != nil {
				return err
			}
		}
		if err := validateWorkerReferences(ctx, tx, base.ID, proposal.Data); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO profile_drafts(id,run_id,target_profile_id,base_revision,base,request,proposal,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, run, base.ID, base.Revision, string(before), request, string(raw), created, created)
		return err
	})
	if err != nil {
		return ProfileDraft{}, err
	}
	return s.GetProfileDraft(ctx, id)
}
func (s *Store) EditProfileDraft(ctx context.Context, id string, revision int, proposal ProfileProposal) (ProfileDraft, error) {
	if err := proposal.Validate(); err != nil {
		return ProfileDraft{}, err
	}
	raw, _ := json.Marshal(proposal)
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanProfileDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM profile_drafts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.State != "draft" || d.Revision != revision {
			return errors.New("草案已变更或已处理，请刷新")
		}
		if err = validateDraftRefsInTx(ctx, tx, d.RunID, proposal.EvidenceRefs); err != nil {
			return err
		}
		if err = validateWorkerReferences(ctx, tx, d.TargetProfileID, proposal.Data); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE profile_drafts SET proposal=?,revision=revision+1,updated_at=? WHERE id=?`, string(raw), now(), id)
		return err
	})
	if err != nil {
		return ProfileDraft{}, err
	}
	return s.GetProfileDraft(ctx, id)
}
func (s *Store) DiscardProfileDraft(ctx context.Context, id string, revision int) error {
	result, err := s.db.ExecContext(ctx, `UPDATE profile_drafts SET state='discarded',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND state='draft'`, now(), id, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("草案已变更或已处理，请刷新")
	}
	return err
}

// The candidate and destination revision commit together. No automatic merge
// or rebase may overwrite an intervening human edit.
func (s *Store) ApplyProfileDraft(ctx context.Context, id string, revision int) (Profile, error) {
	var profileID string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanProfileDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM profile_drafts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.State != "draft" || d.Revision != revision {
			return errors.New("草案已变更或已处理，请刷新")
		}
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT status FROM runs WHERE id=?`, d.RunID).Scan(&status); err != nil {
			return err
		}
		if status != "completed" {
			return errors.New("草案生成任务未成功结束")
		}
		if err = validateDraftEvidenceInTx(ctx, tx, d.RunID); err != nil {
			return err
		}
		if err = d.Proposal.Validate(); err != nil {
			return err
		}
		profileID = d.TargetProfileID
		nextRevision := d.BaseRevision + 1
		if profileID == "" {
			profileID, err = newID()
			if err != nil {
				return err
			}
			nextRevision = 1
		}
		if err = validateWorkerReferences(ctx, tx, profileID, d.Proposal.Data); err != nil {
			return err
		}
		name := strings.TrimSpace(d.Proposal.Name)
		updated := now()
		if d.TargetProfileID == "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO profiles(id,name,revision,is_default,created_at,updated_at) VALUES(?,?,1,0,?,?)`, profileID, name, updated, updated); err != nil {
				return err
			}
		} else {
			result, err := tx.ExecContext(ctx, `UPDATE profiles SET name=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND archived_at IS NULL`, name, updated, profileID, d.BaseRevision)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return errors.New("目标 Profile 已修改或删除；请重新生成草案")
			}
		}
		if err = saveProfileConfig(ctx, tx, profileID, d.Proposal.Data); err != nil {
			return err
		}
		if err = saveProfileRevision(ctx, tx, profileID, nextRevision, name, d.Proposal.Data); err != nil {
			return err
		}
		if err = publishDraftScopesInTx(ctx, tx, d.RunID, profileID, nextRevision); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE profile_drafts SET state='applied',applied_profile_id=?,applied_revision=?,revision=revision+1,updated_at=? WHERE id=?`, profileID, nextRevision, updated, id)
		return err
	})
	if err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, profileID)
}

// Reverting an adopted change publishes the recorded base as a new revision.
// It cannot discard edits made after adoption or modify an in-flight snapshot.
func (s *Store) RevertProfileDraft(ctx context.Context, id string, revision int) (Profile, error) {
	var target string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		d, err := scanProfileDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM profile_drafts WHERE id=?`, id))
		if err != nil {
			return err
		}
		if d.State != "applied" || d.Revision != revision {
			return errors.New("草案已变更或不能撤销")
		}
		if d.TargetProfileID == "" {
			return errors.New("新建 Profile 可在配置库删除；没有可恢复的基础配置")
		}
		target = d.TargetProfileID
		if err = validateWorkerReferences(ctx, tx, target, d.Base.Data); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE profiles SET name=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND archived_at IS NULL`, d.Base.Name, now(), target, d.AppliedRevision)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("采用后 Profile 已修改或删除，不能覆盖后续编辑")
		}
		if err = saveProfileConfig(ctx, tx, target, d.Base.Data); err != nil {
			return err
		}
		if err = saveProfileRevision(ctx, tx, target, d.AppliedRevision+1, d.Base.Name, d.Base.Data); err != nil {
			return err
		}
		if err = copyProfileScopesInTx(ctx, tx, target, d.BaseRevision, target, d.AppliedRevision+1); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE profile_drafts SET state='reverted',revision=revision+1,updated_at=? WHERE id=?`, now(), id)
		return err
	})
	if err != nil {
		return Profile{}, err
	}
	return s.GetProfile(ctx, target)
}
