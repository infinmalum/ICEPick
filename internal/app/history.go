package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"icepick/internal/model"
	"icepick/internal/skills"
	"icepick/internal/storage"
)

// sessionWindow is the model-facing context of one run: the rendered
// history (system prompt first, then every history entry the session
// summary does not cover) and the summary text replacing that prefix. The
// engine mirrors the summary into the system prompt, so every model request
// snapshot stays auditable while the covered originals stay on disk.
type sessionWindow struct {
	history []model.Message
	summary string
}

// memoryRecallLimit bounds how many memories one run injects, and
// memoryPromptBudget caps their combined text.
const (
	memoryRecallLimit  = 50
	memoryPromptBudget = 16 * 1024
)

// buildRunContext assembles what a run sends to the model: the persisted
// history minus the compacted prefix, plus — when the profile and the
// agent's own authorization allow it — the agent's long-term memories, and
// the session's resumable plan. Nothing here modifies persisted records.
func (s *Service) buildRunContext(sessionID string, profile storage.AgentProfile) (sessionWindow, error) {
	ctx := context.Background()
	entries, err := s.store.HistoryEntries(ctx, sessionID)
	if err != nil {
		return sessionWindow{}, err
	}
	window := sessionWindow{history: []model.Message{{Role: "system", Content: s.systemPrompt(profile)}}}
	if sum, found, err := s.store.GetSessionSummary(ctx, sessionID); err != nil {
		return sessionWindow{}, err
	} else if found {
		// A stale boundary (data drifted, or an older binary wrote it) is
		// ignored: the full history goes out and the next compaction
		// regenerates the summary.
		if covered, ok := storage.SummaryCoverage(entries, sum); ok {
			entries = entries[covered:]
			window.summary = sum.Summary
		}
	}
	for _, entry := range entries {
		switch entry.Role {
		case storage.RoleUser:
			window.history = append(window.history, model.Message{Role: "user", Content: entry.Content})
		case storage.RoleAssistant:
			turn := model.Message{Role: "assistant", Content: entry.Content}
			if len(entry.ProviderState) > 0 {
				prior, found, err := s.store.ProfileSnapshot(ctx, entry.RunID)
				if err != nil {
					return sessionWindow{}, err
				}
				if found && prior.Model.Provider == profile.Model.Provider && prior.Model.Name == profile.Model.Name && strings.TrimRight(prior.Model.EffectiveBaseURL(), "/") == strings.TrimRight(profile.Model.EffectiveBaseURL(), "/") {
					turn.ProviderState = entry.ProviderState
				}
			}
			for _, call := range entry.Calls {
				turn.ToolCalls = append(turn.ToolCalls, model.ToolCall{ID: call.ID, Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: call.Name, Arguments: call.Arguments}})
			}
			window.history = append(window.history, turn)
		case storage.RoleTool:
			window.history = append(window.history, model.Message{Role: "tool", ToolCallID: entry.CallID, Content: entry.Content})
		}
	}
	if err := s.injectMemories(ctx, sessionID, profile, &window); err != nil {
		return sessionWindow{}, err
	}
	s.injectPlan(ctx, sessionID, profile, &window)
	if profile.Resources != nil && profile.Resources.Options.WebSearch {
		window.history[0].Content += "\n" + searchInstruction(model.NativeSearchSupported(profile.Model) && allowsNativeSearch(profile.ProfileData))
	}
	return window, nil
}

// injectMemories appends the agent's long-term memories to the system
// prompt. Memory content counts as session input from the moment it is
// injected: its data scopes accumulate into the session, so later sends —
// including to a remote model — must clear exactly what the memories carry.
func (s *Service) injectMemories(ctx context.Context, sessionID string, profile storage.AgentProfile, window *sessionWindow) error {
	if !memoryAllowed(profile) {
		return nil
	}
	newest, err := s.store.ListAgentMemories(ctx, profile.AgentID, memoryRecallLimit)
	if err != nil {
		return err
	}
	if len(newest) == 0 {
		return nil
	}
	selected := make([]storage.Memory, 0, len(newest))
	used := 0
	for _, memory := range newest {
		if used+len(memory.Content) > memoryPromptBudget {
			break
		}
		selected = append(selected, memory)
		used += len(memory.Content)
	}
	scopes := make([]string, 0, len(selected))
	for _, memory := range selected {
		scopes = append(scopes, memory.Scope)
	}
	if err := s.store.AccumulateContextScopes(ctx, sessionID, scopes); err != nil {
		return err
	}
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	var b strings.Builder
	b.WriteString("长期记忆（此 Agent 此前沉淀的跨会话记忆，来源与数据范围已标注）：")
	for _, memory := range selected {
		fmt.Fprintf(&b, "\n- [%s · %s] %s", memory.Source, memory.Scope, memory.Content)
	}
	window.history[0].Content += "\n\n" + b.String()
	return nil
}

// injectPlan appends the session's resumable plan to the system prompt when
// the profile enables the plan capability. The plan is bookkeeping the
// model maintains through local__update_plan; it grants no permission.
func (s *Service) injectPlan(ctx context.Context, sessionID string, profile storage.AgentProfile, window *sessionWindow) {
	if !profile.PlanEnabled {
		return
	}
	plan, found, err := s.store.GetSessionPlan(ctx, sessionID)
	if err != nil {
		return
	}
	if !found {
		window.history[0].Content += fmt.Sprintf("\n\n当前计划为空；local__update_plan 使用 expected_revision=%d。", plan.Revision)
		return
	}
	var steps []storage.PlanStep
	if json.Unmarshal([]byte(plan.Plan), &steps) != nil || len(steps) == 0 {
		return
	}
	marker := map[string]string{storage.PlanPending: "[ ]", storage.PlanInProgress: "[~]", storage.PlanDone: "[x]"}
	var b strings.Builder
	fmt.Fprintf(&b, "当前计划（修订 %d；更新使用 expected_revision=%d，保留任务 ID；已开始任务不可删除或改写执行条件）：", plan.Revision, plan.Revision)
	for _, step := range steps {
		note := ""
		if step.Note != "" {
			note = "（" + step.Note + "）"
		}
		raw, _ := json.Marshal(step)
		fmt.Fprintf(&b, "\n%s %s%s · %s", marker[step.Status], step.Title, note, raw)
	}
	window.history[0].Content += "\n\n" + b.String()
}

// memoryAllowed is the effective memory authorization: the profile's
// preference and the agent's own policy must both permit it.
func memoryAllowed(profile storage.AgentProfile) bool {
	return profile.MemoryEnabled && profile.Policy.Memory
}

// systemPrompt composes the profile prompt with the enabled Agent Skills
// metadata, mirroring the pre-P1 behavior.
func (s *Service) systemPrompt(profile storage.AgentProfile) string {
	prompt := profile.Persona
	if profile.SystemPrompt != "" {
		if prompt != "" {
			prompt += "\n\n"
		}
		prompt += profile.SystemPrompt
	}
	if profile.AllowsTool("local__exec") || profile.AllowsTool("local__write_file") || profile.AllowsTool("local__edit_file") {
		if profile.Policy.Permissions.Unrestricted {
			prompt += "\n本机一般资源范围不限；文件操作使用绝对路径，命令明确给出工作目录。缺少任务目录时先询问用户。"
		} else {
			prompt += "\n本次任务允许的工作区根：" + strings.Join(profile.Policy.Permissions.WorkspacePaths, ", ") + "；允许的可执行文件（精确匹配）：" + strings.Join(profile.Policy.Permissions.Commands, ", ") + "。文件使用绝对路径，命令明确给出工作目录。权限会在实际执行前重新核对。"
		}
	}
	if profile.AllowsTool("local__load_skill") {
		summary := s.profileSkillSummary(profile)
		if summary != "" {
			prompt += "\n可用 Agent Skills（仅元数据）：\n" + summary + "\n相关任务先调用 local__load_skill 读取完整 SKILL.md。"
		}
	}
	return prompt
}

func (s *Service) profileSkillSummary(profile storage.AgentProfile) string {
	if profile.ResourcesFrozen {
		return skills.SnapshotSummary(profile.FrozenSkills)
	}
	if profile.AllSkills {
		return s.skills.Summary()
	}
	return s.skills.SummaryFor(profile.EnabledSkills)
}
