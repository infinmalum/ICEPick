package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"icepick/internal/model"
	"icepick/internal/skills"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

type endpoint struct {
	name    string
	session *mcp.ClientSession
	close   func() error
}

type Hub struct {
	mu        sync.RWMutex
	endpoints []endpoint
}

func NewLocal(ctx context.Context, catalog *skills.Catalog) (*Hub, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "icepick-local", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "current_time", Description: "读取本机当前时间（含时区）"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: time.Now().Format(time.RFC3339)}}}, nil, ctx.Err()
	})
	if catalog != nil && catalog.Summary() != "" {
		mcp.AddTool(server, &mcp.Tool{Name: "load_skill", Description: "按名称读取已发现的 Agent Skill 的 SKILL.md 指令"}, func(_ context.Context, _ *mcp.CallToolRequest, args struct {
			Name string `json:"name"`
		}) (*mcp.CallToolResult, any, error) { content, err := catalog.Load(args.Name); if err != nil {
			return nil, nil, err
		}; return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: content}}}, nil, nil })
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "icepick", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		serverSession.Close()
		return nil, err
	}
	h := &Hub{}
	h.endpoints = append(h.endpoints, endpoint{name: "local", session: clientSession, close: func() error {
		err := clientSession.Close()
		serverSession.Close()
		return err
	}})
	return h, nil
}

// ConnectCommand attaches an explicitly configured stdio MCP server.
func (h *Hub) ConnectCommand(ctx context.Context, name, command string, args []string) error {
	if name == "" || strings.Contains(name, "__") || name == "local" || command == "" {
		return errors.New("MCP 服务名称或命令无效")
	}
	h.mu.Lock()
	for _, ep := range h.endpoints {
		if ep.name == name {
			h.mu.Unlock()
			return fmt.Errorf("MCP 服务 %q 已存在", name)
		}
	}
	h.mu.Unlock()
	client := mcp.NewClient(&mcp.Implementation{Name: "icepick", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(command, args...)}, nil)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.endpoints = append(h.endpoints, endpoint{name: name, session: session, close: session.Close})
	h.mu.Unlock()
	return nil
}

func (h *Hub) Definitions(ctx context.Context) ([]model.Tool, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	var definitions []model.Tool
	for _, ep := range h.endpoints {
		for tool, err := range ep.session.Tools(ctx, nil) {
			if err != nil {
				return nil, fmt.Errorf("MCP %s tools/list: %w", ep.name, err)
			}
			schema, err := json.Marshal(tool.InputSchema)
			if err != nil {
				return nil, err
			}
			definition := model.Tool{Type: "function"}
			definition.Function.Name = ep.name + "__" + tool.Name
			definition.Function.Description = tool.Description
			definition.Function.Parameters = schema
			definitions = append(definitions, definition)
		}
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].Function.Name < definitions[j].Function.Name })
	return definitions, nil
}

func (h *Hub) Call(ctx context.Context, name, arguments string) (string, error) {
	parts := strings.SplitN(name, "__", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("未知 MCP 工具 %q", name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(arguments), &args); err != nil || args == nil {
		return "", errors.New("MCP 工具参数必须是 JSON 对象")
	}
	h.mu.RLock()
	var session *mcp.ClientSession
	for _, ep := range h.endpoints {
		if ep.name == parts[0] {
			session = ep.session
			break
		}
	}
	h.mu.RUnlock()
	if session == nil {
		return "", fmt.Errorf("未知 MCP 服务 %q", parts[0])
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: parts[1], Arguments: args})
	if err != nil {
		return "", err
	}
	var texts []string
	for _, content := range result.Content {
		if item, ok := content.(*mcp.TextContent); ok {
			texts = append(texts, item.Text)
		}
	}
	if len(texts) == 0 && result.StructuredContent != nil {
		data, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", err
		}
		texts = append(texts, string(data))
	}
	output := strings.Join(texts, "\n")
	if result.IsError {
		return "", fmt.Errorf("MCP 工具错误: %s", output)
	}
	return output, nil
}

func (h *Hub) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var first error
	for _, ep := range h.endpoints {
		if err := ep.close(); err != nil && first == nil {
			first = err
		}
	}
	h.endpoints = nil
	return first
}
