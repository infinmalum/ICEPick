package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
)

type ChatModel interface {
	Chat(context.Context, config.Model, []model.Message, []model.Tool) (model.ChatResponse, error)
}

type Engine struct {
	Model        ChatModel
	Tools        *mcptools.Hub
	OnTool       func(name, result string) error
	AllowedTools []string
	CanCall      func(name, arguments string) bool
}

const maxTurns = 8
const maxToolOutput = 16 * 1024

func (e Engine) Run(ctx context.Context, source config.Model, history []model.Message) (string, error) {
	if e.Model == nil || e.Tools == nil {
		return "", errors.New("Agent 尚未初始化")
	}
	messages := append([]model.Message(nil), history...)
	definitions, err := e.Tools.Definitions(ctx)
	if err != nil {
		return "", err
	}
	configured := map[string]bool{}
	for _, name := range e.AllowedTools {
		configured[name] = true
	}
	allowed := make(map[string]bool, len(definitions))
	visible := definitions[:0]
	for _, definition := range definitions {
		name := definition.Function.Name
		if e.AllowedTools != nil && !configured[name] {
			continue
		}
		allowed[name] = true
		visible = append(visible, definition)
	}
	definitions = visible
	for turn := 0; turn < maxTurns; turn++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		answer, err := e.Model.Chat(ctx, source, messages, definitions)
		if err != nil {
			return "", err
		}
		if len(answer.Message.ToolCalls) == 0 {
			if answer.FinishReason == "tool_calls" || strings.TrimSpace(answer.Message.Content) == "" {
				return "", errors.New("模型未返回有效回答")
			}
			return answer.Message.Content, nil
		}
		messages = append(messages, answer.Message)
		seen := map[string]bool{}
		for _, call := range answer.Message.ToolCalls {
			if call.ID == "" || seen[call.ID] {
				return "", errors.New("模型工具调用 ID 缺失或重复")
			}
			seen[call.ID] = true
			var result string
			var err error
			if !allowed[call.Function.Name] || (e.CanCall != nil && !e.CanCall(call.Function.Name, call.Function.Arguments)) {
				err = fmt.Errorf("未提供的 MCP 工具 %q", call.Function.Name)
			} else {
				result, err = e.Tools.Call(ctx, call.Function.Name, call.Function.Arguments)
			}
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if err != nil {
				result = "工具错误：" + err.Error()
			}
			if len(result) > maxToolOutput {
				result = result[:maxToolOutput] + "\n[输出已截断]"
			}
			if e.OnTool != nil {
				if err := e.OnTool(call.Function.Name, result); err != nil {
					return "", fmt.Errorf("保存工具结果: %w", err)
				}
			}
			messages = append(messages, model.Message{Role: "tool", ToolCallID: call.ID, Content: result})
		}
	}
	return "", fmt.Errorf("Agent 超过 %d 轮上限", maxTurns)
}
