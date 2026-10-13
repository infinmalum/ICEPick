package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"icepick/internal/agent"
	"icepick/internal/mcptools"
	"icepick/internal/model"
	"icepick/internal/storage"
)

// The delegation built-in (P5): local__delegate_task starts one independent
// subtask run on a target Agent of the same host. The subtask reuses the
// ordinary run loop with the target's current Profile snapshot; it never
// creates a Profile or Agent, never touches the target's normal sessions or
// long-term memory, and can never widen permissions — the effective
// authorization is the intersection of the parent task's and the target
// Agent's own, with the stricter approval mode and exfiltration grants.

// maxDelegationMaterials bounds the explicitly passed material text.
const maxDelegationMaterials = 64 * 1024

var delegateDefinition = func() model.Tool {
	t := model.Tool{Type: "function"}
	t.Function.Name = "local__delegate_task"
	t.Function.Description = "把一个明确的子任务委派给允许列表中的另一个 Agent 执行。子任务拥有独立上下文，不会访问目标的会话或长期记忆，只能使用此处明确给出的任务描述和材料；返回子任务的最终结果。"
	t.Function.Parameters = json.RawMessage(`{"type":"object","properties":{"agent":{"type":"string","description":"目标 Agent 的名称或 ID；未填写时由宿主按配置选择"},"task":{"type":"string","description":"交给子任务的完整任务描述，自包含"},"materials":{"type":"string","description":"执行任务所需的明确材料（可选）"}},"required":["task"]}`)
	return t
}()

type delegateArgs struct {
	TaskID          string `json:"taskId"`
	TaskFingerprint string `json:"taskFingerprint"`
	Worker          string `json:"worker"`
	Agent           string `json:"agent"`
	Task            string `json:"task"`
	Materials       string `json:"materials"`
}

// delegationOffered reports whether a run may offer the delegation tool at
// all: the Profile switch, the agent's allowlist and the depth budget must
// all allow it. The gate re-checks every call against the live policy.
func delegationOffered(ctx context.Context, svc *Service, profile storage.AgentProfile, runID string) bool {
	if !profile.Delegation.Enabled || !profile.AllowsTool(delegateDefinition.Function.Name) {
		return false
	}
	resources, err := svc.resourcesFor(ctx, profile, runID)
	if err == nil && len(resources.Workers) > 0 && runID != "" {
		run, err := svc.store.GetRun(ctx, runID)
		if err == nil && run.Depth == 0 && run.Depth+1 <= profile.Delegation.MaxDepth {
			return true
		}
	}
	if profile.Delegation.Selection == storage.DelegationPinnedSelection {
		if profile.Delegation.TargetAgentID == "" {
			return false
		}
		target, err := svc.store.GetAgent(ctx, profile.Delegation.TargetAgentID)
		if err != nil {
			return false
		}
		return policyAllowsTarget(profile.Policy, target.ID)
	}
	if len(profile.Policy.DelegationTargets) == 0 {
		return false
	}
	if runID == "" {
		return true
	}
	run, err := svc.store.GetRun(ctx, runID)
	if err != nil {
		return false
	}
	return run.Depth+1 <= profile.Delegation.MaxDepth
}

// policyAllowsTarget reports whether the agent's own allowlist names the
// target. The list is the machine-local authorization; selection can only
// pick inside it.
func policyAllowsTarget(policy storage.AgentPolicy, targetID string) bool {
	for _, id := range policy.DelegationTargets {
		if id == targetID {
			return true
		}
	}
	return false
}

// resolveDelegationTarget maps a delegation call to its target Agent. Pinned
// profiles ignore the model's choice; otherwise the reference is an Agent ID
// first and a unique Agent name second.
func (s *Service) resolveDelegationTarget(ctx context.Context, profile storage.AgentProfile, reference string) (storage.Agent, error) {
	if profile.Delegation.Selection == storage.DelegationPinnedSelection {
		reference = profile.Delegation.TargetAgentID
		if reference == "" {
			return storage.Agent{}, errors.New("该 Profile 指定了固定委派目标，但未配置目标 Agent")
		}
	}
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return storage.Agent{}, errors.New("委派缺少目标 Agent，请在参数中给出名称或 ID")
	}
	if target, err := s.store.GetAgent(ctx, reference); err == nil {
		return target, nil
	}
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return storage.Agent{}, err
	}
	matches := []storage.Agent{}
	for _, candidate := range agents {
		if candidate.Name == reference {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return storage.Agent{}, fmt.Errorf("未找到目标 Agent %q", reference)
	default:
		return storage.Agent{}, fmt.Errorf("目标 Agent 名称 %q 不唯一，请改用 Agent ID", reference)
	}
}

// delegationRefusal runs the host-side limits for one concrete delegation:
// depth budget, allowlist membership, delegation cycles, duplicate tasks and
// the concurrency budget. The allowlist is checked against the Agent's live
// policy — the same source the gate uses — so revocation applies immediately.
// An empty string means the call may proceed.
func (s *Service) delegationRefusal(ctx context.Context, parentProfile storage.AgentProfile, livePolicy storage.AgentPolicy, parentRun storage.Run, target storage.Agent, task string) string {
	if !parentProfile.Delegation.Enabled {
		return "该 Profile 未启用委派"
	}
	if parentRun.Depth+1 > parentProfile.Delegation.MaxDepth {
		return fmt.Sprintf("超过委派深度上限（%d）", parentProfile.Delegation.MaxDepth)
	}
	if !policyAllowsTarget(livePolicy, target.ID) {
		return fmt.Sprintf("目标 Agent %q 不在该 Agent 的委派允许列表中", target.Name)
	}
	// A delegation chain must never return to an Agent that already appears
	// above the calling run; self-delegation by the calling Agent itself
	// stays allowed and is bounded by the depth budget. The chain is oldest
	// ancestor first and ends with the calling run.
	ancestors, err := s.store.AncestorRunIDs(ctx, parentRun.ID)
	if err != nil {
		return "读取委派链路失败: " + err.Error()
	}
	for i, ancestorID := range ancestors {
		if i == len(ancestors)-1 {
			continue
		}
		ancestor, err := s.store.GetRun(ctx, ancestorID)
		if err != nil {
			continue
		}
		if ancestor.AgentID == target.ID {
			return "拒绝循环委派：目标 Agent 已出现在此任务的委派链路中"
		}
	}
	// One identical (target, task) delegation per parent run: repeated calls
	// must not silently re-run the same subtask.
	children, err := s.store.ListChildRuns(ctx, parentRun.ID)
	if err != nil {
		return "读取子任务失败: " + err.Error()
	}
	for _, child := range children {
		if child.AgentID != target.ID {
			continue
		}
		previous, err := s.store.FirstUserMessage(ctx, child.SessionID)
		if err != nil {
			continue
		}
		if strings.TrimSpace(previous) == strings.TrimSpace(task) {
			return "重复任务：同一目标已委派过相同的任务描述"
		}
	}
	concurrency := parentProfile.Delegation.MaxConcurrency
	if concurrency <= 0 {
		concurrency = storage.DefaultDelegationConcurrency
	}
	running := 0
	for _, child := range children {
		if child.Status == "running" {
			running++
		}
	}
	if running >= concurrency {
		return fmt.Sprintf("并发子任务已达上限（%d）", concurrency)
	}
	return ""
}

// executeDelegation runs one delegated subtask synchronously inside the
// parent's tool call. The child run is a first-class persisted run: it has
// its own delegation session (hidden from the normal list), its own Profile
// snapshot of the target Agent, its own recorder, gate and guarded sends.
func (s *Service) executeDelegation(ctx context.Context, parentProfile storage.AgentProfile, parentSessionID, parentRunID string, raw string) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	var args delegateArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", errors.New("delegate_task 参数必须是 JSON 对象")
	}
	var planned *storage.TaskSpec
	var err error
	args, planned, err = s.resolvePlannedTask(ctx, parentProfile, parentRunID, args)
	if err != nil {
		return "", err
	}
	task := strings.TrimSpace(args.Task)
	if task == "" || len(task) > 10000 {
		return "", errors.New("子任务描述长度必须在 1 到 10000 字节之间")
	}
	materials := strings.TrimSpace(args.Materials)
	if len(materials) > maxDelegationMaterials {
		return "", fmt.Errorf("传入材料超过 %d 字节上限", maxDelegationMaterials)
	}
	userContent := task
	if materials != "" {
		userContent += "\n\n[父任务提供的材料]\n" + materials
	}
	name, worker, selected, err := s.selectedWorker(ctx, parentProfile, parentRunID, args)
	if err != nil {
		return "", err
	}
	if selected {
		return s.executeWorker(ctx, parentProfile, parentSessionID, parentRunID, name, worker, userContent, planned)
	}
	parentRun, err := s.store.GetRun(ctx, parentRunID)
	if err != nil {
		return "", err
	}
	target, err := s.resolveFrozenLegacy(ctx, parentProfile, parentRunID, args.Agent)
	if err != nil {
		return "", err
	}
	liveAgent, err := s.store.GetAgent(ctx, parentProfile.AgentID)
	if err != nil {
		return "", err
	}
	if refusal := s.delegationRefusal(ctx, parentProfile, liveAgent.Policy, parentRun, target, userContent); refusal != "" {
		return "", errors.New(refusal)
	}
	// The passed materials carry the parent context's provenance: the child
	// session accumulates it, so every later send — including the child's
	// own model requests — must be covered by both agents' grants.
	materialScope, err := s.sessionScopeMax(ctx, parentSessionID, parentProfile)
	if err != nil {
		return "", err
	}
	childSession, err := s.store.CreateDelegationSession(ctx, target.ID, "委派子任务 → "+target.Name)
	if err != nil {
		return "", err
	}
	childRun, childCtx, childProfile, err := s.runs.StartChild(ctx, childSession.ID, parentRunID, parentRun.Depth+1)
	if err != nil {
		return "", err
	}
	result, runErr := s.runDelegatedSubtask(ctx, childCtx, childRun, childSession, childProfile, parentProfile, parentSessionID, target, userContent, materialScope)
	// Finalize the child run. A direct user cancel already set a terminal
	// state, so Finish may fail harmlessly; the record stays authoritative.
	return result, runErr
}

// runDelegatedSubtask executes the child loop and maps its terminal state to
// the parent-visible tool result. `parentCtx` is the delegating run's
// context, `ctx` the child's own (parent-derived, budget-bounded) context.
func (s *Service) runDelegatedSubtask(parentCtx context.Context, ctx context.Context, childRun storage.Run, childSession storage.Session, childProfile storage.AgentProfile, parentProfile storage.AgentProfile, parentSessionID string, target storage.Agent, userContent, materialScope string) (string, error) {
	finalize := func(status, detail string) {
		_ = s.runs.Finish(childRun.ID, status, detail)
		s.publish(Event{Type: "run.updated", SessionID: childRun.SessionID, RunID: childRun.ID, Detail: "委派子任务结束"})
	}
	s.publish(Event{Type: "run.started", SessionID: childRun.SessionID, RunID: childRun.ID})
	defer finalizeOnPanic(s, childRun.ID)
	if _, err := s.store.AddMessage(ctx, childRun.SessionID, childRun.ID, storage.RoleUser, userContent); err != nil {
		finalize("failed", err.Error())
		return "", fmt.Errorf("子任务写入失败: %w", err)
	}
	parentScopes, err := s.sessionScopes(ctx, parentSessionID, parentProfile)
	if err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	if err := s.store.AccumulateContextScopes(ctx, childRun.SessionID, parentScopes); err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	if err := s.store.AccumulateContextScopes(ctx, childRun.SessionID, []string{materialScope}); err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	s.mu.RLock()
	source := s.config.Model
	timeout := s.ApprovalTimeout
	s.mu.RUnlock()
	if childProfile.Model.Provider != "" {
		source = childProfile.Model
	}
	if source.Name == "" {
		finalize("failed", "请先选择模型 ID")
		return "", errors.New("子任务无法启动：目标 Agent 没有可用模型")
	}
	// Fail fast when the child's remote endpoint lacks either grant; the
	// per-turn dual check below re-validates it on every request.
	if origin, local, err := endpointOrigin(source.EffectiveBaseURL()); err != nil {
		finalize("failed", err.Error())
		return "", err
	} else if !local {
		for _, grantAgent := range []string{childProfile.AgentID, parentProfile.AgentID} {
			if _, found, err := s.store.GetExfilGrant(ctx, grantAgent, origin); err != nil {
				finalize("failed", err.Error())
				return "", err
			} else if !found {
				err := fmt.Errorf("子任务无法启动：Agent %s 未授权向 %s 外发数据", grantAgent, origin)
				finalize("failed", err.Error())
				return "", err
			}
		}
	}
	budgetSeconds := childProfile.Delegation.MaxSeconds
	if budgetSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(budgetSeconds)*time.Second)
		defer cancel()
	}
	resources, err := s.resourcesFor(ctx, parentProfile, childRun.ParentRunID)
	if err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	structured := resources.Root.Data.Execution.Workflow == "dynamic" && childRun.WorkerName != ""
	history := []model.Message{{Role: "system", Content: s.delegationSystemPrompt(parentProfile, childProfile)}}
	if structured {
		history[0].Content += "\n\n" + workerOutcomeContract
	}
	history = append(history, model.Message{Role: "user", Content: userContent})
	runTools := s.tools
	if hub, ok := s.tools.(*mcptools.Hub); ok {
		all, names := childProfile.AllMCPServers, childProfile.EnabledMCPServers
		if len(childProfile.AllowedTools) > 0 {
			all = false
			names = nil
			seen := map[string]bool{}
			for _, tool := range childProfile.AllowedTools {
				if childProfile.AllowsTool(tool) {
					server := serverOf(tool)
					if !seen[server] {
						names = append(names, server)
						seen[server] = true
					}
				}
			}
		}
		runTools = hub.ForServers(all, names)
	}
	if spec, ok, err := s.store.RunTaskSpec(ctx, childRun.ID); err != nil {
		finalize("failed", err.Error())
		return "", err
	} else if ok && spec.AccessMode == "read" {
		if hub, ok := s.tools.(*mcptools.Hub); ok {
			runTools = hub.ForServers(false, []string{"local"})
		}
	}
	runner := bookkeepingTools{inner: runTools, svc: s, profile: childProfile, sessionID: childRun.SessionID, runID: childRun.ID, delegated: true}
	definitions, err := runner.Definitions(ctx)
	if err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	if err := s.SyncMCPEndpoints(ctx); err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	allowedTools := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if childProfile.AllowsTool(definition.Function.Name) {
			allowedTools = append(allowedTools, definition.Function.Name)
		}
	}
	actorIDs, err := s.runActorIDs(ctx, childRun.ID)
	if err != nil {
		finalize("failed", err.Error())
		return "", err
	}
	guarded := guardedChat{svc: s, runID: childRun.ID, inner: s.chat, payloadCheck: func(c context.Context, m []model.Message, t []model.Tool) error {
		return s.checkPayloadFor(c, source, actorIDs, childRun.SessionID, m, t)
	}, check: func(checkCtx context.Context) error {
		// The stricter exfiltration rule: both the target Agent's and the
		// parent task's grants must cover everything the child sends.
		return s.exfilCheckRun(checkCtx, childRun.ID, childRun.SessionID, source, childProfile)
	}}
	engine := agent.Engine{
		Model:        guarded,
		Tools:        runner,
		Recorder:     runRecorder{svc: s, runID: childRun.ID, sessionID: childRun.SessionID, classifier: s.classifierFor(childProfile)},
		AllowedTools: allowedTools,
		Gate:         toolGate{svc: s, agentID: childProfile.AgentID, snapshot: &childProfile, runID: childRun.ID, parent: &parentProfile},
		Approver:     s,
		OnDelta: func(text string) error {
			s.appendStream(childRun.ID, childRun.SessionID, text)
			return nil
		},
		Compactor:       sessionCompactor{svc: s, runID: childRun.ID, sessionID: childRun.SessionID, chat: guarded},
		ContextBudget:   childProfile.ContextBudget,
		ApprovalTimeout: timeout,
	}
	finalText, runErr := engine.Run(ctx, source, history)
	scopes, scopeErr := s.sessionScopes(context.WithoutCancel(ctx), childRun.SessionID, childProfile)
	if scopeErr == nil {
		scopeErr = s.store.AccumulateContextScopes(context.WithoutCancel(ctx), parentSessionID, scopes)
	}
	if scopeErr != nil {
		finalize("failed", scopeErr.Error())
		return "", scopeErr
	}
	if structured && runErr == nil {
		outcome, err := s.validateWorkerOutcome(ctx, childRun.ID, finalText)
		if err != nil {
			runErr = err
		} else if outcome.Status != "succeeded" {
			runErr = fmt.Errorf("worker %s：%s", outcome.Status, outcome.Content)
		} else {
			finalText = outcome.Content
		}
	}
	switch {
	case runErr == nil:
		scopes, err := s.sessionScopes(ctx, childRun.SessionID, childProfile)
		if err != nil {
			finalize("failed", err.Error())
			return "", err
		}
		if err := s.store.AccumulateContextScopes(ctx, parentSessionID, scopes); err != nil {
			finalize("failed", err.Error())
			return "", err
		}
		finalize("completed", "")
		return finalText, nil
	case parentCtx.Err() != nil:
		// The parent context died (user cancel, timeout, host shutdown): the
		// child is cancelled with it — a known outcome, never unknown.
		finalize("cancelled", "父任务已取消")
		return "子任务已取消。", nil
	case errors.Is(runErr, context.Canceled):
		finalize("cancelled", "")
		return "子任务已被用户取消。", nil
	case errors.Is(runErr, context.DeadlineExceeded):
		finalize("failed", "子任务超过时间预算")
		return "", errors.New("子任务超过时间预算")
	default:
		finalize("failed", runErr.Error())
		return "", fmt.Errorf("子任务失败：%s", runErr)
	}
}

func finalizeOnPanic(s *Service, runID string) {
	if recovered := recover(); recovered != nil {
		_ = s.runs.Finish(runID, "failed", fmt.Sprintf("子任务内部错误: %v", recovered))
		panic(recovered)
	}
}

// delegationSystemPrompt composes the child's system prompt: the target
// profile's own prompt plus a fixed note that this is a delegated subtask
// working only from explicitly passed materials. Memory and plan injection
// never run for delegated sessions.
func (s *Service) delegationSystemPrompt(parentProfile, childProfile storage.AgentProfile) string {
	prompt := s.systemPrompt(childProfile)
	note := fmt.Sprintf("[委派说明] 本会话是 Agent「%s」委派的子任务：只使用用户消息中给出的任务描述与材料完成工作，不要假设可以访问你既有的会话、记忆或计划。", parentProfile.AgentName)
	if prompt == "" {
		return note
	}
	return prompt + "\n\n" + note
}

// intersectPermissions is the effective machine authorization of a delegated
// subtask: the intersection of the parent task's and the target Agent's own
// permission scopes. An empty list means "nothing granted".
func intersectPermissions(parent, target storage.PermissionScope) storage.PermissionScope {
	out := storage.PermissionScope{}
	out.Unrestricted = parent.Unrestricted && target.Unrestricted
	switch {
	case parent.Unrestricted:
		out.WorkspacePaths = append([]string(nil), target.WorkspacePaths...)
	case target.Unrestricted:
		out.WorkspacePaths = append([]string(nil), parent.WorkspacePaths...)
	default:
		b := map[string]bool{}
		for _, path := range target.WorkspacePaths {
			b[path] = true
		}
		for _, path := range parent.WorkspacePaths {
			if b[path] {
				out.WorkspacePaths = append(out.WorkspacePaths, path)
			}
		}
	}
	out.Commands = intersectLists(parent.Commands, target.Commands)
	out.NetworkHosts = intersectLists(parent.NetworkHosts, target.NetworkHosts)
	return out
}

func intersectLists(a, b []string) []string {
	set := map[string]bool{}
	for _, item := range b {
		set[item] = true
	}
	out := []string{}
	for _, item := range a {
		if set[item] {
			out = append(out, item)
		}
	}
	return out
}
