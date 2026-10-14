package cli

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
		return usage("profiles 需要子命令 list|create|copy|edit|restore|export|import|delete")
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
		parsed, err := parseCLI(args[1:])
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
		var source storage.Profile
		if err := client.Call("profiles.get", map[string]any{"id": profileID}, &source); err != nil {
			return fail("profiles.get", err)
		}
		method := "templates.restore"
		if source.Data.TemplateID == "" {
			if profileID != storage.DefaultProfileID {
				return usage("此 Profile 没有出厂模板来源")
			}
			method = "profiles.restore"
		}
		var groups []string
		if group := parsed.value("group"); group != "" {
			groups = []string{group}
		}
		var restored storage.Profile
		if err := client.Call(method, map[string]any{"revision": revision, "id": profileID, "groups": groups}, &restored); err != nil {
			return fail("profiles.restore", err)
		}
		fmt.Printf("已恢复内置内容（%s，v%d）\n", restored.Name, restored.Revision)
		return 0
	case "export":
		parsed, err := parseCLI(args[1:], "include-workers")
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: profiles export <id> [--out <文件.zip>] [--include-workers]")
		}
		method := "profiles.export"
		if parsed.flags["include-workers"] {
			method = "profiles.exportWorkers"
		}
		var result app.ExportResult
		if err := client.Call(method, map[string]any{"id": parsed.arg(0)}, &result); err != nil {
			return fail("profiles.export", err)
		}
		out := parsed.value("out")
		if out == "" {
			out = result.Filename
		}
		data, err := base64.StdEncoding.DecodeString(result.Data)
		if err != nil {
			return fail("解码分享包", err)
		}
		if err := os.WriteFile(out, data, 0600); err != nil {
			return fail("写入分享包", err)
		}
		fmt.Printf("已导出 Profile 到 %s（%d 字节）。包内不含授权、会话、记忆或密钥。\n", out, len(data))
		return 0
	case "import":
		return importProfile(client, args[1:])
	case "delete":
		parsed, err := parseCLI(args[1:], "restore-workers")
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: profiles delete <id>")
		}
		var refs []storage.ProfileReference
		if err := client.Call("profiles.references", map[string]any{"id": parsed.arg(0)}, &refs); err != nil {
			return fail("profiles.references", err)
		}
		method := "profiles.archive"
		if len(refs) > 0 {
			if parsed.value("replace") == "" && !parsed.flags["restore-workers"] {
				return usage("此 Profile 被 worker 引用；删除需使用 --replace <ProfileID> 或 --restore-workers")
			}
			method = "profiles.replaceArchive"
		}
		if err := client.Call(method, map[string]any{"id": parsed.arg(0), "replacement": parsed.value("replace"), "own": parsed.flags["restore-workers"], "expected": refs}, nil); err != nil {
			return fail("profiles.delete", err)
		}
		fmt.Println("已删除 Profile", parsed.arg(0))
		return 0
	default:
		return usage(fmt.Sprintf("未知 profiles 子命令 %q", args[0]))
	}
}

// importProfile reads a share package (zip file or directory with the same
// layout), shows the dependency preview and — only with --yes — imports it.
// The preview step is mandatory: import never auto-installs, auto-authorizes
// or switches agents.
func importProfile(client *ipc.Client, args []string) int {
	parsed, err := parseCLI(args, "yes")
	if err != nil || parsed.arg(0) == "" {
		return usage("用法: profiles import <包路径.zip 或目录> [--yes]")
	}
	data, err := readSharePackage(parsed.arg(0))
	if err != nil {
		return fail("读取分享包", err)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	var preview app.ImportPreview
	if err := client.Call("profiles.importPreview", map[string]any{"data": encoded}, &preview); err != nil {
		return fail("profiles.importPreview", err)
	}
	printImportPreview(preview)
	if !parsed.flags["yes"] {
		fmt.Println("\n预览完成。确认后追加 --yes 执行导入。")
		return 0
	}
	var imported struct {
		Profile storage.Profile   `json:"profile"`
		Preview app.ImportPreview `json:"preview"`
	}
	if err := client.Call("profiles.import", map[string]any{"data": encoded}, &imported); err != nil {
		return fail("profiles.import", err)
	}
	created := imported.Profile
	fmt.Printf("\n已导入为 Profile %s（%s，v%d）。导入不安装资源、不授权；请用 conversations select 为对话选择它。\n", created.ID, created.Name, created.Revision)
	return 0
}

func printImportPreview(p app.ImportPreview) {
	fmt.Printf("Profile 名称: %s\n说明: %s\n模型: %s/%s（%s）\n上下文预算: %d tokens\n人格 %d 字符 / 提示词 %d 字符\n委派: %v（深度上限 %d，并发 %d，单任务 %d 秒）\n记忆偏好: %v · 规划: %v\n",
		p.Name, p.Description, p.Model.Provider, p.Model.Name, p.Model.EffectiveBaseURL(),
		p.ContextBudget, p.PersonaChars, p.PromptChars,
		p.Delegation.Enabled, p.Delegation.MaxDepth, p.Delegation.MaxConcurrency, p.Delegation.MaxSeconds,
		p.MemoryEnabled, p.PlanEnabled)
	if len(p.MCPServers) > 0 {
		fmt.Println("MCP 依赖:")
		for _, server := range p.MCPServers {
			state := "未接入本机"
			if server.Configured {
				state = "已接入"
			}
			adjusted := ""
			if server.CommandAdjusted {
				adjusted = "（导出时去除了本机路径，请核对命令）"
			}
			fmt.Printf("  - %s（命令 %q，%s）%s\n", server.Name, server.Command, state, adjusted)
		}
	}
	if len(p.Skills) > 0 {
		fmt.Println("Skills:")
		for _, skill := range p.Skills {
			state := "未安装"
			if skill.Installed {
				state = "已安装"
			}
			bundled := ""
			if skill.Bundled {
				bundled = "，包含在包内"
			}
			fmt.Printf("  - %s（%s%s）\n", skill.Name, state, bundled)
		}
	}
	for _, warning := range p.Warnings {
		fmt.Println("注意:", warning)
	}
}

// readSharePackage loads a share bundle from a zip file or from a directory
// holding the same layout; directories are packed in memory.
func readSharePackage(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return os.ReadFile(path)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = filepath.WalkDir(path, func(item string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(path, item)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(item)
		if err != nil {
			return err
		}
		writer, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = writer.Write(data)
		return err
	})
	if err != nil {
		zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// editProfile edits the profile fields the CLI can express on the command
// line; the desktop covers the full combination editor (MCP/Skills 工具选择).
func editProfile(client *ipc.Client, args []string) int {
	parsed, err := parseCLI(args)
	if err != nil || parsed.arg(0) == "" {
		return usage("用法: profiles edit <id> [--name N] [--description D] [--persona P] [--prompt P] [--model 提供方/模型ID] [--base-url URL] [--budget N]")
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
	if v := parsed.value("persona"); v != "" {
		profile.Data.Persona = v
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

func fail(operation string, err error) int {
	fmt.Fprintf(os.Stderr, "%s 失败: %v\n", operation, err)
	return 1
}
