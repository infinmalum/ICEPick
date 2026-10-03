package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"icepick/internal/agent"
	"icepick/internal/mcptools"
	"icepick/internal/storage"
)

// toolGate implements agent.ToolGate against the current agent profile (the
// authoritative revision is re-read on every decision so policy tightening
// applies immediately), the effect/sensitivity registry and the MCP endpoint
// fingerprint state. The gate only decides; execution-layer boundaries are
// enforced by the tools themselves.
type toolGate struct {
	svc     *Service
	agentID string
}

var _ agent.ToolGate = toolGate{}

func (g toolGate) Gate(ctx context.Context, req agent.ToolRequest) agent.Decision {
	profile, err := g.svc.store.ResolvedAgentProfile(ctx, g.agentID)
	if err != nil {
		return agent.Decision{Deny: "读取 Agent 配置失败: " + err.Error()}
	}
	if deny := g.hardLimits(ctx, profile, req); deny != "" {
		return agent.Decision{Deny: deny}
	}
	effect, sensitivity := g.svc.classifierFor(profile).Classify(req.Tool, req.Arguments)
	// A changed binary or tool schema suspends automatic execution based on
	// the old registration, in every approval mode.
	if g.svc.endpointChanged(serverOf(req.Tool)) {
		return agent.Decision{Confirm: "MCP 服务指纹变化，基于旧登记的自动执行已暂停，需要重新确认"}
	}
	switch profile.Policy.ApprovalMode {
	case "manual":
		return agent.Decision{Confirm: "当前 Agent 为全人工审批模式"}
	case "automatic":
		// Unknown reads and credentials never auto-execute, even in
		// automatic mode; everything else policy already allows proceeds.
		if sensitivity == mcptools.SensCredential || sensitivity == mcptools.SensUnknown {
			return agent.Decision{Confirm: "敏感度未知或涉及凭据的访问需要确认，全自动模式也不自动执行"}
		}
		return agent.Decision{}
	default: // semi
		if effect == mcptools.EffectPure {
			return agent.Decision{}
		}
		if effect == mcptools.EffectRead && (sensitivity == mcptools.SensPublic || sensitivity == mcptools.SensWorkspace) {
			return agent.Decision{}
		}
		return agent.Decision{Confirm: confirmReason(effect, sensitivity)}
	}
}

// Recheck re-validates an approved call right before dispatch. It only
// denies: approvals can expire through policy changes, never through
// relaxation here.
func (g toolGate) Recheck(ctx context.Context, req agent.ToolRequest) agent.Decision {
	profile, err := g.svc.store.ResolvedAgentProfile(ctx, g.agentID)
	if err != nil {
		return agent.Decision{Deny: "读取 Agent 配置失败: " + err.Error()}
	}
	return agent.Decision{Deny: g.hardLimits(ctx, profile, req)}
}

// hardLimits returns the non-negotiable refusals: the tool must be offered
// and allowed, skill reads must target an enabled skill, and file reads must
// stay inside the workspace boundary when one is configured. A user
// confirmation can never override these.
func (g toolGate) hardLimits(ctx context.Context, profile storage.AgentProfile, req agent.ToolRequest) string {
	if !profile.AllowsTool(req.Tool) {
		return fmt.Sprintf("未被当前 Agent 允许的工具调用 %q", req.Tool)
	}
	if req.Tool == "local__load_skill" {
		var args struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(req.Arguments), &args) != nil || !profile.EnablesSkill(args.Name) {
			return fmt.Sprintf("未被当前 Agent 启用的 Skill %q", args.Name)
		}
	}
	if req.Tool == "local__read_file" && !profile.Policy.Permissions.Unrestricted {
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(req.Arguments), &args) != nil || args.Path == "" {
			return "读取文件缺少有效路径"
		}
		if !withinAny(args.Path, profile.Policy.Permissions.WorkspacePaths) {
			return "路径超出该 Agent 允许的工作区范围"
		}
	}
	return ""
}

func confirmReason(effect mcptools.Effect, sensitivity mcptools.Sensitivity) string {
	switch effect {
	case mcptools.EffectWrite:
		return "写入操作需要确认"
	case mcptools.EffectExecute:
		return "命令执行需要确认"
	case mcptools.EffectExternal:
		return "外部 MCP 工具的副作用无法由本机约束，需要确认"
	case mcptools.EffectRead:
		return "读取敏感度未知或敏感的数据需要确认"
	default:
		return "效果未知的工具调用需要确认"
	}
}

// serverOf extracts the MCP server prefix of a tool name; built-in tools
// report "local".
func serverOf(tool string) string {
	for i := 0; i < len(tool); i++ {
		if tool[i] == '_' && i+1 < len(tool) && tool[i+1] == '_' {
			return tool[:i]
		}
	}
	return "local"
}

// withinAny reports whether path resolves inside one of the roots.
func withinAny(path string, roots []string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(rootAbs); err == nil {
			rootAbs = resolved
		}
		if under(abs, rootAbs) {
			return true
		}
	}
	return false
}

func under(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
