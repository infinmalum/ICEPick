package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"icepick/internal/config"
	"icepick/internal/skills"
)

type FrozenProfile struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Revision int               `json:"revision"`
	Data     ProfileData       `json:"data"`
	Skills   []skills.Snapshot `json:"skills"`
}

type RunResources struct {
	Options       ConversationOptions      `json:"options"`
	SearchTools   []string                 `json:"searchTools"`
	Grants        map[string]ExfilGrant    `json:"grants"`
	RootContextID string                   `json:"rootContextId"`
	Reviewer      FrozenProfile            `json:"reviewer"`
	Planner       FrozenProfile            `json:"planner"`
	Root          FrozenProfile            `json:"root"`
	RootPolicy    ContextPolicy            `json:"rootPolicy"`
	Workers       map[string]FrozenProfile `json:"workers"`
}

type SkillSnapshotter func(bool, []string) ([]skills.Snapshot, error)

func validateWorkerReferences(ctx context.Context, tx *sql.Tx, id string, data ProfileData) error {
	for _, source := range data.Execution.WorkerSources {
		if source.ProfileID == "" {
			continue
		}
		if source.ProfileID == id {
			return errors.New("引用自身请使用当前 Profile 的 worker")
		}
		if err := activeProfile(ctx, tx, source.ProfileID); err != nil {
			return fmt.Errorf("worker %q: %w", source.Name, err)
		}
	}
	return nil
}

func readFrozenProfile(ctx context.Context, tx *sql.Tx, id string) (FrozenProfile, error) {
	var item FrozenProfile
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT id,name,revision,config FROM profiles WHERE id=? AND archived_at IS NULL`, id).Scan(&item.ID, &item.Name, &item.Revision, &raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &item.Data)
	}
	return item, err
}

func freezeSkills(item *FrozenProfile, snapshot SkillSnapshotter) error {
	if snapshot == nil || !item.Data.AllowsTool("local__load_skill") {
		item.Skills = []skills.Snapshot{}
		return nil
	}
	var err error
	item.Skills, err = snapshot(item.Data.AllSkills, item.Data.EnabledSkills)
	return err
}

// Worker capabilities cannot exceed the root's common candidate selection.
// The main component may be narrower (for example delegation-only).
func WorkerProjection(root, source ProfileData) (ProfileData, error) {
	child, err := source.ResolveComponent("worker")
	if err != nil {
		return ProfileData{}, err
	}
	if !root.AllMCPServers {
		if child.AllMCPServers {
			child.EnabledMCPServers = append([]string(nil), root.EnabledMCPServers...)
		} else {
			child.EnabledMCPServers = intersectStrings(child.EnabledMCPServers, root.EnabledMCPServers)
		}
		child.AllMCPServers = false
	}
	if len(root.AllowedTools) > 0 {
		var names []string
		for _, name := range root.AllowedTools {
			if child.AllowsTool(name) {
				names = append(names, name)
			}
		}
		child.AllowedTools = names
		if len(names) == 0 {
			child.AllMCPServers = false
			child.EnabledMCPServers = nil
		}
	}
	if !root.AllSkills {
		if child.AllSkills {
			child.EnabledSkills = append([]string(nil), root.EnabledSkills...)
		} else {
			child.EnabledSkills = intersectStrings(child.EnabledSkills, root.EnabledSkills)
		}
		child.AllSkills = false
	}
	child.MemoryEnabled = false
	child.PlanEnabled = false
	child.Delegation = DelegationPolicy{MaxSeconds: root.Delegation.MaxSeconds}
	child.Execution = Execution{Workflow: "direct"}
	return child, nil
}

func intersectStrings(a, b []string) []string {
	set := map[string]bool{}
	for _, v := range b {
		set[v] = true
	}
	var out []string
	for _, v := range a {
		if set[v] {
			out = append(out, v)
		}
	}
	return out
}

func captureRunResources(ctx context.Context, tx *sql.Tx, root FrozenProfile, contextID string, policy ContextPolicy, defaultModel config.Model, snapshot SkillSnapshotter) (RunResources, error) {
	resources := RunResources{RootContextID: contextID, RootPolicy: policy, Workers: map[string]FrozenProfile{}}
	rawRoot := root.Data
	// Bound retained content while capturing, before allocating the final JSON.
	snapshotBytes := 0
	captureSkills := func(item *FrozenProfile) error {
		if err := freezeSkills(item, snapshot); err != nil {
			return err
		}
		for _, skill := range item.Skills {
			snapshotBytes += len(skill.Content) + len(skill.Description)
		}
		if snapshotBytes > 16<<20 {
			return errors.New("任务依赖快照超过 16 MiB，请收窄 Skills 或 worker 配置")
		}
		return nil
	}
	main, err := root.Data.ResolveComponent("main")
	if err != nil {
		return resources, err
	}
	if main.Model.Provider == "" {
		main.Model = defaultModel
	}
	root.Data = main
	if err := captureSkills(&root); err != nil {
		return resources, err
	}
	resources.Root = root
	if rawRoot.Execution.Workflow == "dynamic" {
		planned, err := rawRoot.ResolveComponent("planner")
		if err != nil {
			return resources, err
		}
		if planned.Model.Provider == "" {
			planned.Model = defaultModel
		}
		resources.Planner = FrozenProfile{ID: root.ID, Name: root.Name, Revision: root.Revision, Data: planned}
	}
	if rawRoot.Execution.Review.Mode != "" && rawRoot.Execution.Review.Mode != "off" {
		reviewed, err := rawRoot.ResolveComponent("reviewer")
		if err != nil {
			return resources, err
		}
		if reviewed.Model.Provider == "" {
			reviewed.Model = defaultModel
		}
		resources.Reviewer = FrozenProfile{ID: root.ID, Name: root.Name, Revision: root.Revision, Data: reviewed}
	}
	for _, slot := range rawRoot.Execution.WorkerSources {
		item := FrozenProfile{ID: root.ID, Name: root.Name, Revision: root.Revision, Data: rawRoot}
		if slot.Configuration != nil {
			item.Data = slot.Configuration.ProfileData()
			item.Name = slot.Configuration.Name
		}
		if slot.ProfileID != "" {
			item, err = readFrozenProfile(ctx, tx, slot.ProfileID)
			if err != nil {
				return resources, fmt.Errorf("冻结 worker %q: %w", slot.Name, err)
			}
		}
		item.Data, err = WorkerProjection(rawRoot, item.Data)
		if err != nil {
			return resources, err
		}
		if item.Data.Model.Provider == "" {
			item.Data.Model = defaultModel
		}
		if err := captureSkills(&item); err != nil {
			return resources, err
		}
		resources.Workers[slot.Name] = item
	}
	resources.Grants = map[string]ExfilGrant{}
	rows, err := tx.QueryContext(ctx, `SELECT context_id,destination,scopes,created_at FROM exfil_grants WHERE context_id=?`, contextID)
	if err != nil {
		return resources, err
	}
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			rows.Close()
			return resources, err
		}
		resources.Grants[grant.Destination] = grant
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return resources, err
	}
	encoded, err := json.Marshal(resources)
	if err != nil {
		return resources, err
	}
	if len(encoded) > 16<<20 {
		return resources, errors.New("任务依赖快照超过 16 MiB，请收窄 Skills 或 worker 配置")
	}
	return resources, nil
}

func resourcesInTx(ctx context.Context, tx *sql.Tx, runID string) (RunResources, error) {
	var resources RunResources
	seen := map[string]bool{}
	for runID != "" {
		if seen[runID] {
			return resources, errors.New("运行父链循环")
		}
		seen[runID] = true
		var raw string
		var parent sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT resources_snapshot,parent_run_id FROM runs WHERE id=?`, runID).Scan(&raw, &parent); err != nil {
			return resources, err
		}
		if raw != "" {
			return resources, json.Unmarshal([]byte(raw), &resources)
		}
		runID = parent.String
	}
	return resources, nil
}

func (s *Store) RunResources(ctx context.Context, runID string) (RunResources, error) {
	var item RunResources
	err := s.withTx(ctx, func(tx *sql.Tx) error { var err error; item, err = resourcesInTx(ctx, tx, runID); return err })
	return item, err
}
