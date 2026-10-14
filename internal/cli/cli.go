// Package cli is the command-line client of the running ICEPick host. It
// owns no run state: every command goes over IPC to the same application
// service the desktop UI drives, so approvals, ownership checks and the
// exfiltration gate behave identically in both frontends.
package cli

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"icepick/internal/app"
	"icepick/internal/host"
	"icepick/internal/ipc"
)

// Run dispatches one CLI invocation and returns the process exit code.
// `serve` boots (or, for a data directory that already has one, refuses to
// run) the headless host; every other command connects to the running host
// of the user's data directory.
func Run(args []string) int {
	if len(args) == 0 {
		return usage("")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "help", "-h", "--help":
		return usage("")
	case "-v", "--version":
		fmt.Println("ICEPick CLI (host protocol", ipc.ProtocolVersion, ")")
		return 0
	}
	client, code := connect()
	if client == nil {
		return code
	}
	defer client.Close()
	return dispatch(client, args)
}

func usage(errDetail string) int {
	out := os.Stdout
	code := 0
	if errDetail != "" {
		out = os.Stderr
		fmt.Fprintf(out, "%s\n\n", errDetail)
		code = 2
	}
	fmt.Fprintln(out, `用法：
  icepick serve                    启动无头宿主（含 IPC；桌面端同时内置）
  icepick status                   查看宿主与模型状态
  icepick templates list | create <模板ID> [名称]
  icepick conversations list | create --profile <id> [--reuse <对话ID>]
  icepick conversations select <对话ID> --profile <id>
  icepick conversations start <对话ID> <任务内容…> [--wait] | show <对话ID>
  icepick conversations search <对话ID> [on|off|records]
  icepick conversations permissions <对话ID> [--mode semi] [--workspace <路径>]
  icepick profiles list | create <名称> [--from <id>] | copy <id> <新名称>
  icepick profiles edit <id> [--name N] [--description D] [--prompt P] [--model 提供方/模型ID] [--budget N]
  icepick profiles restore | delete <id> [--replace <ProfileID> | --restore-workers]
  icepick profiles export <id> [--out <文件.zip>] [--include-workers] | import <包.zip|目录> [--yes]
  icepick tasks status <runID> --agent <agentID> | review <sessionID> <runID> | cancel <runID> --agent <agentID>
  icepick approvals list [agentID] | approve <callID> --agent <agentID> | deny <callID> [原因] --agent <agentID>
  icepick memory list <agentID> | add <agentID> <内容…> | edit <agentID> <id> <内容…> | delete <agentID> <id> | export <agentID> [--out <文件>]
  icepick logs [行数]`)
	return code
}

// serve runs the headless host: full startup, IPC listener, then wait for a
// termination signal. Client disconnects never cancel running tasks.
func serve(args []string) int {
	h, err := host.Start()
	if err != nil {
		fmt.Fprintln(os.Stderr, "宿主启动失败:", err)
		return 1
	}
	defer h.Close()
	if h.InitError != nil {
		fmt.Fprintln(os.Stderr, "宿主初始化失败:", h.InitError)
		return 1
	}
	server, err := ipc.Start(h.Service, h.DataDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, "IPC 启动失败:", err)
		return 1
	}
	defer server.Close()
	fmt.Println("ICEPick 宿主已启动（数据目录", h.DataDir(), "）；Ctrl+C 退出。")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	fmt.Println("正在退出宿主…")
	return 0
}

// connect dials the host with a short retry so a host that is still booting
// does not produce spurious failures.
func connect() (*ipc.Client, int) {
	dataDir, err := dataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return nil, 1
	}
	client, err := ipc.Connect(dataDir, 2*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "提示：先运行 `icepick serve`（或桌面端）再执行 CLI 命令。")
		return nil, 1
	}
	return client, 0
}

func dispatch(client *ipc.Client, args []string) int {
	switch args[0] {
	case "conversations":
		return cmdConversations(client, args[1:])
	case "templates":
		return cmdTemplates(client, args[1:])
	case "status":
		return cmdStatus(client)
	case "profiles":
		return cmdProfiles(client, args[1:])
	case "tasks":
		return cmdTasks(client, args[1:])
	case "approvals":
		return cmdApprovals(client, args[1:])
	case "memory":
		return cmdMemory(client, args[1:])
	case "logs":
		return cmdLogs(args[1:])
	case "events":
		return cmdEvents(client)
	default:
		return usage(fmt.Sprintf("未知命令 %q", args[0]))
	}
}

func cmdEvents(client *ipc.Client) int {
	client.OnEvent(func(evt app.Event) {
		fmt.Printf("[%s] run=%s session=%s %s\n", evt.Type, evt.RunID, evt.SessionID, evt.Detail)
	})
	fmt.Println("已订阅宿主事件，Ctrl+C 退出。")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-signals:
	case <-client.Closed():
		fmt.Fprintln(os.Stderr, "宿主连接已断开。")
		return 1
	}
	return 0
}

func printTable(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if n := len([]rune(cell)); i < len(widths) && n > widths[i] {
				widths[i] = n
			}
		}
	}
	for _, row := range rows {
		line := make([]string, 0, len(row))
		for i, cell := range row {
			pad := widths[i] - len([]rune(cell))
			if pad < 0 {
				pad = 0
			}
			line = append(line, cell+strings.Repeat(" ", pad))
		}
		fmt.Println(strings.Join(line, "  "))
	}
}
