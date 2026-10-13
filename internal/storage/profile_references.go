package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type ProfileReference struct {
	ProfileID string   `json:"profileId"`
	Name      string   `json:"name"`
	Revision  int      `json:"revision"`
	Workers   []string `json:"workers"`
}

func referencedProfiles(ctx context.Context, tx *sql.Tx, target string) ([]Profile, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,name,revision,config FROM resolved_profiles WHERE archived_at IS NULL AND id<>? ORDER BY id`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := []Profile{}
	for rows.Next() {
		var p Profile
		var raw string
		if err := rows.Scan(&p.ID, &p.Name, &p.Revision, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &p.Data); err != nil {
			return nil, err
		}
		for _, slot := range p.Data.Execution.WorkerSources {
			if slot.ProfileID == target {
				profiles = append(profiles, p)
				break
			}
		}
	}
	return profiles, rows.Err()
}

func (s *Store) ProfileReferences(ctx context.Context, id string) ([]ProfileReference, error) {
	refs := []ProfileReference{}
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		profiles, err := referencedProfiles(ctx, tx, id)
		if err != nil {
			return err
		}
		for _, p := range profiles {
			ref := ProfileReference{ProfileID: p.ID, Name: p.Name, Revision: p.Revision, Workers: []string{}}
			for _, slot := range p.Data.Execution.WorkerSources {
				if slot.ProfileID == id {
					ref.Workers = append(ref.Workers, slot.Name)
				}
			}
			refs = append(refs, ref)
		}
		return nil
	})
	return refs, err
}

// A concrete reference set/revision binds the user's replacement decision.
// Every dependent update and the archive commit together or not at all.
func (s *Store) ArchiveProfileReplacing(ctx context.Context, id, replacement string, own bool, expected []ProfileReference) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if err := activeProfile(ctx, tx, id); err != nil {
			return err
		}
		profiles, err := referencedProfiles(ctx, tx, id)
		if err != nil {
			return err
		}
		if replacement != "" && (own || replacement == id) {
			return errors.New("替代来源无效")
		}
		if len(profiles) > 0 {
			if replacement == "" && !own {
				return errors.New("Profile 仍被 worker 引用；请显式选择替代配置或恢复这些 worker 为各自 Profile 的配置")
			}
			if len(expected) != len(profiles) {
				return errors.New("Profile 引用已变化，请刷新后重试")
			}
			revisions := map[string]int{}
			for _, ref := range expected {
				revisions[ref.ProfileID] = ref.Revision
			}
			for _, p := range profiles {
				if revisions[p.ID] != p.Revision {
					return errors.New("被引用 Profile 已修改，请刷新后重试")
				}
			}
		}
		if replacement != "" {
			if err := activeProfile(ctx, tx, replacement); err != nil {
				return err
			}
		}
		for _, p := range profiles {
			for i, slot := range p.Data.Execution.WorkerSources {
				if slot.ProfileID == id {
					p.Data.Execution.WorkerSources[i].ProfileID = replacement
					if own || replacement == p.ID {
						p.Data.Execution.WorkerSources[i].ProfileID = ""
					}
				}
			}
			if err := validateProfileData(p.Data); err != nil {
				return err
			}
			if err := validateWorkerReferences(ctx, tx, p.ID, p.Data); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `UPDATE profiles SET revision=revision+1,updated_at=? WHERE id=? AND revision=?`, now(), p.ID, p.Revision)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil || count != 1 {
				return fmt.Errorf("发布替代配置失败: %v", err)
			}
			if err := saveProfileComponents(ctx, tx, p.ID, p.Data); err != nil {
				return err
			}
			if err := saveProfileRevision(ctx, tx, p.ID, p.Revision+1, p.Name, p.Data); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE profiles SET archived_at=?,updated_at=? WHERE id=?`, now(), now(), id)
		return err
	})
}
