package cli

import (
	"fmt"
	"os"
	"strings"

	"icepick/internal/app"
	"icepick/internal/config"
	"icepick/internal/ipc"
	"icepick/internal/storage"
)

func dataDir() (string, error) {
	paths := config.UserPaths()
	if err := config.EnsureDirs(paths); err != nil {
		return "", fmt.Errorf("初始化数据目录失败: %w", err)
	}
	return paths.Data, nil
}

func cmdStatus(client *ipc.Client) int {
	var status app.AppStatus
	if err := client.Call("status", nil, &status); err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
		return 1
	}
	fmt.Printf("存储: %s\nAgent: %s\n模型: %s\n提供方: %s\n模型地址: %s\n模型 ID: %s\n",
		boolText(status.StorageReady, "已就绪", "异常"),
		boolText(status.AgentReady, "已就绪", "未配置"),
		boolText(status.ModelConnected, "已连接", "未连接"),
		status.Model.Provider, status.Model.EffectiveBaseURL(), status.Model.Name)
	if status.Message != "" {
		fmt.Println("消息:", status.Message)
	}
	if len(status.AvailableModels) > 0 {
		fmt.Println("可见模型:", strings.Join(status.AvailableModels, "、"))
	}
	return 0
}

func boolText(value bool, whenTrue, whenFalse string) string {
	if value {
		return whenTrue
	}
	return whenFalse
}

func cmdProfiles(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("profiles 需要子命令 list|create|copy|edit|restore|delete")
	}
	switch args[0] {
	case "list":
		var profiles []storage.Profile
		if err := client.Call("profiles.list", nil, &profiles); err != nil {
			return fail("profiles.list", err)
		}
		rows := [][]string{{"ID", "名称", "版本", "模型", "默认"}}
		for _, p := range profiles {
			rows = append(rows, []string{p.ID, p.Name, fmt.Sprintf("v%d", p.Revision), p.Data.Model.Provider + "/" + p.Data.Model.Name, boolText(p.IsDefault, "是", "")})
		}
		printTable(rows)
		return 0
	case "create":
		parsed, err := parseCLI(args[1:], "from")
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: profiles create <名称> [--from <id>]")
		}
		var created storage.Profile
		if err := client.Call("profiles.create", map[string]any{"name": parsed.arg(0), "fromId": parsed.value("from")}, &created); err != nil {
			return fail("profiles.create", err)
		}
		fmt.Printf("已创建 Profile %s（%s，v%d）\n", created.ID, created.Name, created.Revision)
		return 0
	case "copy":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" || parsed.arg(1) == "" {
			return usage("用法: profiles copy <来源ID> <新名称>")
		}
		var created storage.Profile
		if err := client.Call("profiles.create", map[string]any{"name": parsed.arg(1), "fromId": parsed.arg(0)}, &created); err != nil {
			return fail("profiles.copy", err)
		}
		fmt.Printf("已复制为 Profile %s（%s，v%d）\n", created.ID, created.Name, created.Revision)
		return 0
	case "edit":
		return editProfile(client, args[1:])
	case "restore":
		parsed, err := parseCLI(args[1:])
		if err != nil {
			return 2
		}
		profileID := parsed.value("id")
		if profileID == "" {
			profileID = storage.DefaultProfileID
		}
		revision := 0
		if parsed.value("revision") != "" {
			if _, err := fmt.Sscanf(parsed.value("revision"), "%d", &revision); err != nil || revision <= 0 {
				return usage("--revision 需要正整数（当前版本号，乐观锁）")
			}
		}
		if revision <= 0 {
			var current storage.Profile
			if err := client.Call("profiles.get", map[string]any{"id": profileID}, &current); err != nil {
				return fail("profiles.get", err)
			}
			revision = current.Revision
		}
		var restored storage.Profile
		if err := client.Call("profiles.restore", map[string]any{"revision": revision, "id": profileID}, &restored); err != nil {
			return fail("profiles.restore", err)
		}
		fmt.Printf("已恢复内置内容（%s，v%d）\n", restored.Name, restored.Revision)
		return 0
	case "delete":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: profiles delete <id>")
		}
		if err := client.Call("profiles.delete", map[string]any{"id": parsed.arg(0)}, nil); err != nil {
			return fail("profiles.delete", err)
		}
		fmt.Println("已删除 Profile", parsed.arg(0))
		return 0
	default:
		return usage(fmt.Sprintf("未知 profiles 子命令 %q", args[0]))
	}
}

// editProfile edits the profile fields the CLI can express on the command
// line; the desktop covers the full combination editor (MCP/Skills 工具选择).
func editProfile(client *ipc.Client, args []string) int {
	parsed, err := parseCLI(args)
	if err != nil || parsed.arg(0) == "" {
		return usage("用法: profiles edit <id> [--name N] [--description D] [--prompt P] [--model 提供方/模型ID] [--base-url URL] [--budget N]")
	}
	var profile storage.Profile
	if err := client.Call("profiles.get", map[string]any{"id": parsed.arg(0)}, &profile); err != nil {
		return fail("profiles.get", err)
	}
	if v := parsed.value("name"); v != "" {
		profile.Name = v
	}
	if v := parsed.value("description"); v != "" {
		profile.Data.Description = v
	}
	if v := parsed.value("prompt"); v != "" {
		profile.Data.SystemPrompt = v
	}
	baseURL := parsed.value("base-url")
	if v := parsed.value("model"); v != "" {
		parts := strings.SplitN(v, "/", 2)
		if len(parts) != 2 || !config.IsProvider(parts[0]) {
			return usage("--model 需要 提供方/模型ID 形式，提供方支持 ollama|lmstudio|openai|anthropic|gemini")
		}
		profile.Data.Model = config.Model{Provider: parts[0], BaseURL: baseURL, Name: parts[1]}
	} else if baseURL != "" {
		profile.Data.Model.BaseURL = baseURL
	}
	if v := parsed.value("budget"); v != "" {
		budget := 0
		if _, err := fmt.Sscanf(v, "%d", &budget); err != nil || budget <= 0 {
			return usage("--budget 需要正整数")
		}
		profile.Data.ContextBudget = budget
	}
	var saved storage.Profile
	if err := client.Call("profiles.update", map[string]any{"profile": profile}, &saved); err != nil {
		return fail("profiles.update", err)
	}
	fmt.Printf("已保存 Profile %s（v%d）\n", saved.ID, saved.Revision)
	return 0
}

func cmdAgents(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("agents 需要子命令 list|create|switch|update|delete")
	}
	switch args[0] {
	case "list":
		var agents []storage.Agent
		if err := client.Call("agents.list", nil, &agents); err != nil {
			return fail("agents.list", err)
		}
		var profiles []storage.Profile
		_ = client.Call("profiles.list", nil, &profiles)
		profileNames := map[string]string{}
		for _, p := range profiles {
			profileNames[p.ID] = p.Name
		}
		rows := [][]string{{"ID", "名称", "Profile", "审批模式"}}
		for _, a := range agents {
			rows = append(rows, []string{a.ID, a.Name, fmt.Sprintf("%s (%s)", profileNames[a.ProfileID], a.ProfileID), a.Policy.ApprovalMode})
		}
		printTable(rows)
		return 0
	case "create":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: agents create <名称> [--profile <id>]")
		}
		profileID := parsed.value("profile")
		if profileID == "" {
			profileID = storage.DefaultProfileID
		}
		var created storage.Agent
		if err := client.Call("agents.create", map[string]any{"name": parsed.arg(0), "profileId": profileID}, &created); err != nil {
			return fail("agents.create", err)
		}
		fmt.Printf("已创建 Agent %s（%s，策略初始化为全人工）\n", created.ID, created.Name)
		return 0
	case "switch":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" || parsed.arg(1) == "" {
			return usage("用法: agents switch <agentID> <profileID>")
		}
		var updated storage.Agent
		if err := client.Call("agents.switchProfile", map[string]any{"agentId": parsed.arg(0), "profileId": parsed.arg(1)}, &updated); err != nil {
			return fail("agents.switch", err)
		}
		fmt.Printf("Agent %s 现在使用 Profile %s（会话与授权不变）\n", updated.ID, updated.ProfileID)
		return 0
	case "update":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: agents update <agentID> [--name N] [--mode manual|semi|automatic]")
		}
		if parsed.value("name") == "" && parsed.value("mode") == "" {
			return usage("agents update 需要至少一个 --name 或 --mode")
		}
		var resolved storage.AgentProfile
		if err := client.Call("agents.profile", map[string]any{"agentId": parsed.arg(0)}, &resolved); err != nil {
			return fail("agents.profile", err)
		}
		if v := parsed.value("name"); v != "" {
			resolved.AgentName = v
		}
		if v := parsed.value("mode"); v != "" {
			resolved.Policy.ApprovalMode = v
		}
		var updated storage.Agent
		if err := client.Call("agents.update", map[string]any{"agentId": parsed.arg(0), "name": resolved.AgentName, "policy": resolved.Policy}, &updated); err != nil {
			return fail("agents.update", err)
		}
		fmt.Printf("已更新 Agent %s（审批模式 %s）\n", updated.ID, updated.Policy.ApprovalMode)
		return 0
	case "delete":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: agents delete <agentID>")
		}
		if err := client.Call("agents.delete", map[string]any{"agentId": parsed.arg(0)}, nil); err != nil {
			return fail("agents.delete", err)
		}
		fmt.Println("已删除 Agent", parsed.arg(0))
		return 0
	default:
		return usage(fmt.Sprintf("未知 agents 子命令 %q", args[0]))
	}
}

func fail(operation string, err error) int {
	fmt.Fprintf(os.Stderr, "%s 失败: %v\n", operation, err)
	return 1
}
