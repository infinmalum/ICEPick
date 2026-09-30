package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"icepick/internal/config"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	Message      Message
	FinishReason string
	Usage        Usage
}

type ChatClient struct {
	Client *http.Client
}

// Chat uses the common non-streaming endpoint supported by Ollama and LM Studio.
// The agent loop owns tool validation, execution, and follow-up turns.
func (c ChatClient) Chat(ctx context.Context, source config.Model, messages []Message, tools []Tool) (ChatResponse, error) {
	if err := source.Validate(); err != nil {
		return ChatResponse{}, err
	}
	if source.Name == "" {
		return ChatResponse{}, errors.New("请先选择模型 ID")
	}
	if len(messages) == 0 {
		return ChatResponse{}, errors.New("消息列表不能为空")
	}
	request := struct {
		Model    string    `json:"model"`
		Messages []Message `json:"messages"`
		Tools    []Tool    `json:"tools,omitempty"`
		Stream   bool      `json:"stream"`
	}{Model: source.Name, Messages: messages, Tools: tools, Stream: false}
	data, err := json.Marshal(request)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("编码模型请求: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(source.BaseURL, "/")+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ChatResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.Client
	if client == nil {
		client = &http.Client{
			Timeout:       5 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("模型请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("模型服务返回 HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
		Usage Usage `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&payload); err != nil {
		return ChatResponse{}, fmt.Errorf("解析模型回答: %w", err)
	}
	if len(payload.Choices) == 0 {
		return ChatResponse{}, errors.New("模型回答没有 choices")
	}
	return ChatResponse{Message: payload.Choices[0].Message, FinishReason: payload.Choices[0].FinishReason, Usage: payload.Usage}, nil
}
