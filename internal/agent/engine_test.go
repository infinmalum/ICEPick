package agent

import (
	"context"
	"strings"
	"testing"

	"icepick/internal/config"
	"icepick/internal/mcptools"
	"icepick/internal/model"
)

type fakeChat struct {
	calls int
	chat  func(int, []model.Message) (model.ChatResponse, error)
	tools func([]model.Tool)
}

func (f *fakeChat) Chat(_ context.Context, _ config.Model, messages []model.Message, tools []model.Tool) (model.ChatResponse, error) {
	f.calls++
	if f.tools != nil {
		f.tools(tools)
	}
	return f.chat(f.calls, messages)
}

func TestEnginePairsMultipleToolResults(t *testing.T) {
	registry, err := mcptools.NewLocal(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	first := model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "one"}, {ID: "two"}}}}
	for i := range first.Message.ToolCalls {
		first.Message.ToolCalls[i].Type = "function"
		first.Message.ToolCalls[i].Function.Name = "local__current_time"
		first.Message.ToolCalls[i].Function.Arguments = "{}"
	}
	fake := &fakeChat{chat: func(turn int, messages []model.Message) (model.ChatResponse, error) {
		if turn == 1 {
			return first, nil
		}
		if len(messages) != 4 || messages[2].ToolCallID != "one" || messages[3].ToolCallID != "two" {
			t.Fatalf("tool result pairing: %+v", messages)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "完成"}}, nil
	}}
	var observed []string
	engine := Engine{Model: fake, Tools: registry, OnTool: func(name, result string) error {
		observed = append(observed, name+":"+result)
		return nil
	}}
	answer, err := engine.Run(context.Background(), config.DefaultModel("lmstudio"), []model.Message{{Role: "user", Content: "几点了"}})
	if err != nil || answer != "完成" || fake.calls != 2 || len(observed) != 2 {
		t.Fatalf("answer=%q calls=%d tools=%+v err=%v", answer, fake.calls, observed, err)
	}
}

func TestEngineHidesAndRechecksTools(t *testing.T) {
	registry, err := mcptools.NewLocal(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	call := model.ToolCall{ID: "one", Type: "function"}
	call.Function.Name, call.Function.Arguments = "local__current_time", "{}"
	fake := &fakeChat{tools: func(tools []model.Tool) {
		if len(tools) != 0 {
			t.Fatalf("tool exposed: %+v", tools)
		}
	}, chat: func(turn int, messages []model.Message) (model.ChatResponse, error) {
		if turn == 1 {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		}
		if !strings.Contains(messages[len(messages)-1].Content, "未提供") {
			t.Fatalf("missing denial: %+v", messages)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "denied"}}, nil
	}}
	answer, err := (Engine{Model: fake, Tools: registry, AllowedTools: []string{}}).Run(context.Background(), config.DefaultModel("lmstudio"), []model.Message{{Role: "user", Content: "test"}})
	if err != nil || answer != "denied" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}

	fake = &fakeChat{tools: func(tools []model.Tool) {
		if len(tools) != 1 {
			t.Fatalf("expected one visible tool: %+v", tools)
		}
	}, chat: func(turn int, messages []model.Message) (model.ChatResponse, error) {
		if turn == 1 {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		}
		if !strings.Contains(messages[len(messages)-1].Content, "未提供") {
			t.Fatalf("missing late denial: %+v", messages)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "denied"}}, nil
	}}
	answer, err = (Engine{Model: fake, Tools: registry, AllowedTools: []string{"local__current_time"}, CanCall: func(_, _ string) bool { return false }}).Run(context.Background(), config.DefaultModel("lmstudio"), []model.Message{{Role: "user", Content: "test"}})
	if err != nil || answer != "denied" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestEngineReturnsMCPErrorToModel(t *testing.T) {
	registry, err := mcptools.NewLocal(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	call := model.ToolCall{ID: "one", Type: "function"}
	call.Function.Name = "local__current_time"
	call.Function.Arguments = "broken"
	fake := &fakeChat{chat: func(turn int, messages []model.Message) (model.ChatResponse, error) {
		if turn == 1 {
			return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
		}
		if !strings.Contains(messages[len(messages)-1].Content, "工具错误") {
			t.Fatalf("plugin error not returned: %+v", messages)
		}
		return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "参数有误"}}, nil
	}}
	answer, err := (Engine{Model: fake, Tools: registry}).Run(context.Background(), config.DefaultModel("lmstudio"), []model.Message{{Role: "user", Content: "test"}})
	if err != nil || answer != "参数有误" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}
