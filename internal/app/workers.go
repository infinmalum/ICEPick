package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"icepick/internal/model"
	"icepick/internal/storage"
)

func (s *Service) resourcesFor(ctx context.Context, p storage.AgentProfile, runID string) (storage.RunResources, error) {
	if p.Resources != nil {
		return *p.Resources, nil
	}
	if runID == "" {
		return storage.RunResources{}, nil
	}
	return s.store.RunResources(ctx, runID)
}

func (s *Service) delegationDefinition(ctx context.Context, p storage.AgentProfile, runID string) model.Tool {
	d := delegateDefinition
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil || len(resources.Workers) == 0 {
		return d
	}
	names := []string{}
	for name := range resources.Workers {
		names = append(names, name)
	}
	sort.Strings(names)
	d.Function.Description = "把明确的子任务交给已配置的 worker；只传入必要材料，等待其独立执行结果。worker 使用本任务起始配置，不读取其他对话或长期记忆。"
	d.Function.Parameters, _ = json.Marshal(map[string]any{"type": "object", "properties": map[string]any{
		"worker":          map[string]any{"type": "string", "enum": names, "description": "worker 槽位；只有一个槽位时可省略"},
		"agent":           map[string]any{"type": "string", "description": "仅用于已有本机授权的旧版委派目标；不能与 worker 同时使用"},
		"taskFingerprint": map[string]any{"type": "string", "description": "执行计划返回的任务条件指纹，审批绑定该指纹"},
		"taskId":          map[string]any{"type": "string", "description": "按执行计划领取任务，只提供 taskId，宿主使用已保存的执行条件与依赖结果"},
		"task":            map[string]any{"type": "string", "description": "完整、自包含的子任务"},
		"materials":       map[string]any{"type": "string", "description": "明确提供的文本材料"}}, "anyOf": []any{map[string]any{"required": []string{"task"}}, map[string]any{"required": []string{"taskId", "taskFingerprint"}}}})
	return d
}

func (s *Service) selectedWorker(ctx context.Context, p storage.AgentProfile, runID string, args delegateArgs) (string, storage.FrozenProfile, bool, error) {
	if args.Worker != "" && args.Agent != "" {
		return "", storage.FrozenProfile{}, true, errors.New("worker 与旧 agent 目标不能同时指定")
	}
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil {
		return "", storage.FrozenProfile{}, false, err
	}
	if args.Agent != "" || (args.Worker == "" && len(resources.Workers) == 0) {
		return "", storage.FrozenProfile{}, false, nil
	}
	name := strings.TrimSpace(args.Worker)
	if name == "" {
		if len(resources.Workers) != 1 {
			return "", storage.FrozenProfile{}, true, errors.New("请明确选择 worker 槽位")
		}
		for key := range resources.Workers {
			name = key
		}
	}
	worker, ok := resources.Workers[name]
	if !ok {
		return "", storage.FrozenProfile{}, true, errors.New("worker 不在本任务起始配置中")
	}
	return name, worker, true, nil
}

func (s *Service) workerLimit(ctx context.Context, p storage.AgentProfile, runID string, name string) string {
	if !p.Delegation.Enabled || !p.AllowsTool("local__delegate_task") {
		return "该 Profile 未允许委派"
	}
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err.Error()
	}
	if run.Depth != 0 || run.Depth+1 > p.Delegation.MaxDepth {
		return "worker 槽位仅在主任务的委派深度内使用"
	}
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil {
		return err.Error()
	}
	if _, ok := resources.Workers[name]; !ok {
		return "worker 不在任务起始快照中"
	}
	return ""
}

func (s *Service) executeWorker(ctx context.Context, parent storage.AgentProfile, sessionID, runID string, name string, worker storage.FrozenProfile, task string, planned *storage.TaskSpec) (string, error) {
	if deny := s.workerLimit(ctx, parent, runID, name); deny != "" {
		return "", errors.New(deny)
	}
	children, err := s.store.ListChildRuns(ctx, runID)
	if err != nil {
		return "", err
	}
	running := 0
	for _, child := range children {
		if child.Status == "running" {
			running++
		}
		if planned == nil && child.WorkerName == name {
			previous, err := s.store.FirstUserMessage(ctx, child.SessionID)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(previous) == strings.TrimSpace(task) {
				return "", errors.New("重复 worker 任务")
			}
		}
	}
	limit := parent.Delegation.MaxConcurrency
	if limit <= 0 {
		limit = storage.DefaultDelegationConcurrency
	}
	if running >= limit {
		return "", errors.New("并发子任务已达上限")
	}
	scope, err := s.sessionScopeMax(ctx, sessionID, parent)
	if err != nil {
		return "", err
	}
	childSession, err := s.store.CreateDelegationSession(ctx, parent.AgentID, "子任务 → "+name+"（"+worker.Name+"）")
	if err != nil {
		return "", err
	}
	var childRun storage.Run
	var childCtx context.Context
	var childProfile storage.AgentProfile
	if planned != nil {
		childRun, childCtx, childProfile, err = s.runs.StartPlannedWorker(ctx, childSession.ID, runID, *planned)
	} else {
		childRun, childCtx, childProfile, err = s.runs.StartWorker(ctx, childSession.ID, runID, 1, name)
	}
	if err != nil {
		return "", err
	}
	target := storage.Agent{ID: parent.AgentID, Name: name}
	return s.runDelegatedSubtask(ctx, childCtx, childRun, childSession, childProfile, parent, sessionID, target, task, scope)
}

// Returns the immutable legacy identity/name, while dispatch still reads the
// live context policy. A rename or Profile switch cannot change the target.
func (s *Service) resolveFrozenLegacy(ctx context.Context, p storage.AgentProfile, runID, reference string) (storage.Agent, error) {
	resources, err := s.resourcesFor(ctx, p, runID)
	if err != nil {
		return storage.Agent{}, err
	}
	if resources.Version == 0 {
		return s.resolveDelegationTarget(ctx, p, reference)
	}
	if p.Delegation.Selection == storage.DelegationPinnedSelection {
		reference = p.Delegation.TargetAgentID
	}
	var matched []storage.LegacyWorker
	for id, item := range resources.Legacy {
		if id == reference {
			return storage.Agent{ID: id, Name: item.Name, Policy: item.Policy, ProfileID: item.Profile.ID}, nil
		}
		if item.Name == reference {
			matched = append(matched, item)
		}
	}
	if len(matched) != 1 {
		return storage.Agent{}, fmt.Errorf("旧委派目标 %q 不在该 Agent 的委派允许列表起始依赖中或名称不唯一", reference)
	}
	item := matched[0]
	return storage.Agent{ID: item.ContextID, Name: item.Name, Policy: item.Policy, ProfileID: item.Profile.ID}, nil
}
