package app

import (
	"context"
	"fmt"

	"icepick/internal/model"
	"icepick/internal/storage"
)

// buildHistory reconstructs the model history for the next run of a session
// from the persisted ordered records: user and assistant messages plus the
// tool calls each assistant turn requested and their persisted outcomes.
// Display-only legacy tool messages are excluded; nothing is fabricated.
func (s *Service) buildHistory(sessionID string, profile storage.AgentProfile) ([]model.Message, error) {
	ctx := context.Background()
	messages, err := s.store.ListMessages(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	calls, err := s.store.ListSessionToolCalls(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	byMessage := map[string][]storage.ToolCall{}
	for _, call := range calls {
		byMessage[call.MessageID] = append(byMessage[call.MessageID], call)
	}
	history := []model.Message{{Role: "system", Content: s.systemPrompt(profile)}}
	for _, item := range messages {
		if item.Legacy {
			continue
		}
		switch item.Role {
		case storage.RoleUser:
			history = append(history, model.Message{Role: "user", Content: item.Content})
		case storage.RoleAssistant:
			turn := model.Message{Role: "assistant", Content: item.Content}
			for _, call := range item.ToolCalls {
				turn.ToolCalls = append(turn.ToolCalls, model.ToolCall{ID: call.ID, Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: call.Name, Arguments: call.Arguments}})
			}
			history = append(history, turn)
			for _, call := range byMessage[item.ID] {
				history = append(history, model.Message{Role: "tool", ToolCallID: call.CallID, Content: modelVisibleResult(call)})
			}
		}
	}
	return history, nil
}

// systemPrompt composes the profile prompt with the enabled Agent Skills
// metadata, mirroring the pre-P1 behavior.
func (s *Service) systemPrompt(profile storage.AgentProfile) string {
	prompt := profile.SystemPrompt
	if profile.AllowsTool("local__load_skill") {
		summary := s.skills.SummaryFor(profile.EnabledSkills)
		if profile.AllSkills {
			summary = s.skills.Summary()
		}
		if summary != "" {
			prompt += "\n可用 Agent Skills（仅元数据）：\n" + summary + "\n相关任务先调用 local__load_skill 读取完整 SKILL.md。"
		}
	}
	return prompt
}

// modelVisibleResult renders a persisted tool call outcome for the model.
// Denied and failed calls surface their recorded error; an unresolved
// unknown result never reaches this point because such sessions cannot start
// a run, but it is still rendered defensively instead of being dropped.
func modelVisibleResult(call storage.ToolCall) string {
	switch call.State {
	case storage.CallSucceeded:
		return call.Result
	case storage.CallFailed:
		return "工具错误：" + call.Error
	case storage.CallCancelled:
		return "工具错误：调用已取消：" + call.Error
	case storage.CallExecutionUnknow:
		return "工具错误：结果未知，等待处置：" + call.Error
	default:
		return fmt.Sprintf("工具错误：调用处于未完成状态 %q", call.State)
	}
}
