package cli

import (
	"fmt"
	"os"
	"strings"

	"icepick/internal/ipc"
	"icepick/internal/storage"
)

// The memory command group manages one agent's long-term memory: the same
// view/edit/export/delete surface the desktop UI offers, over IPC.
func cmdMemory(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("memory 需要子命令 list|add|edit|delete|export")
	}
	switch args[0] {
	case "list":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: memory list <agentID>")
		}
		var memories []storage.Memory
		if err := client.Call("memory.list", map[string]any{"agentId": parsed.arg(0)}, &memories); err != nil {
			return fail("memory.list", err)
		}
		rows := [][]string{{"ID", "来源", "范围", "内容", "记录时间"}}
		for _, memory := range memories {
			rows = append(rows, []string{shortID(memory.ID), memory.Source, memory.Scope, truncate(memory.Content, 60), memory.CreatedAt})
		}
		printTable(rows)
		if len(memories) == 0 {
			fmt.Println("该 Agent 还没有长期记忆。")
		}
		return 0
	case "add":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" || parsed.arg(1) == "" {
			return usage("用法: memory add <agentID> <内容…>")
		}
		content := strings.Join(parsed.positional[1:], " ")
		var memory storage.Memory
		if err := client.Call("memory.add", map[string]any{"agentId": parsed.arg(0), "content": content}, &memory); err != nil {
			return fail("memory.add", err)
		}
		fmt.Printf("已添加记忆 %s\n", memory.ID)
		return 0
	case "edit":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" || parsed.arg(1) == "" || parsed.arg(2) == "" {
			return usage("用法: memory edit <agentID> <记忆ID> <新内容…>")
		}
		content := strings.Join(parsed.positional[2:], " ")
		var memory storage.Memory
		if err := client.Call("memory.update", map[string]any{"agentId": parsed.arg(0), "id": parsed.arg(1), "content": content}, &memory); err != nil {
			return fail("memory.update", err)
		}
		fmt.Printf("已更新记忆 %s\n", memory.ID)
		return 0
	case "delete":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" || parsed.arg(1) == "" {
			return usage("用法: memory delete <agentID> <记忆ID>")
		}
		if err := client.Call("memory.delete", map[string]any{"agentId": parsed.arg(0), "id": parsed.arg(1)}, nil); err != nil {
			return fail("memory.delete", err)
		}
		fmt.Println("已删除。")
		return 0
	case "export":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: memory export <agentID> [--out <文件>]")
		}
		var document string
		if err := client.Call("memory.export", map[string]any{"agentId": parsed.arg(0)}, &document); err != nil {
			return fail("memory.export", err)
		}
		if out := parsed.value("out"); out != "" {
			if err := os.WriteFile(out, []byte(document), 0600); err != nil {
				return fail("写入导出文件", err)
			}
			fmt.Printf("已导出 %d 字节到 %s\n", len(document), out)
			return 0
		}
		fmt.Print(document)
		return 0
	default:
		return usage(fmt.Sprintf("未知 memory 子命令 %q", args[0]))
	}
}

// shortID keeps table rows readable; full ids stay available via export and
// the desktop UI.
func shortID(id string) string {
	if len(id) > 10 {
		return id[:10] + "…"
	}
	return id
}

// truncate shortens one-line table cells to a readable width.
func truncate(text string, width int) string {
	text = trimText(text)
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}
	return string(runes[:width]) + "…"
}
