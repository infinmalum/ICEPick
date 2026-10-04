package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"icepick/internal/app"
	"icepick/internal/ipc"
	"icepick/internal/storage"
)

func cmdSessions(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("sessions 需要子命令 list|create|show")
	}
	switch args[0] {
	case "list":
		parsed, err := parseCLI(args[1:])
		if err != nil {
			return 2
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		var sessions []storage.Session
		if err := client.Call("sessions.list", map[string]any{"agentId": agentID}, &sessions); err != nil {
			return fail("sessions.list", err)
		}
		rows := [][]string{{"ID", "标题", "创建时间"}}
		for _, s := range sessions {
			rows = append(rows, []string{s.ID, s.Title, s.CreatedAt})
		}
		printTable(rows)
		return 0
	case "create":
		parsed, err := parseCLI(args[1:])
		if err != nil {
			return 2
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		var session storage.Session
		if err := client.Call("sessions.create", map[string]any{"agentId": agentID}, &session); err != nil {
			return fail("sessions.create", err)
		}
		fmt.Printf("已创建会话 %s（%s）\n", session.ID, session.Title)
		return 0
	case "show":
		parsed, err := parseCLI(args[1:], "calls")
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: sessions show <sessionID> [--agent <agentID>] [--calls]")
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		sessionID := parsed.arg(0)
		var messages []storage.Message
		if err := client.Call("sessions.messages", map[string]any{"agentId": agentID, "sessionId": sessionID}, &messages); err != nil {
			return fail("sessions.messages", err)
		}
		// The compaction summary and the resumable plan are part of what a
		// session carries; show them before the raw transcript.
		var summary storage.SessionSummary
		if err := client.Call("summaries.get", map[string]any{"agentId": agentID, "sessionId": sessionID}, &summary); err == nil {
			fmt.Printf("--- 压缩摘要（覆盖至 #%d，范围 %s）---\n%s\n", summary.CoversSeq, summary.Scope, summary.Summary)
		}
		var plan app.SessionPlanView
		if err := client.Call("plans.get", map[string]any{"agentId": agentID, "sessionId": sessionID}, &plan); err == nil {
			fmt.Println("--- 当前计划 ---")
			for _, step := range plan.Steps {
				note := ""
				if step.Note != "" {
					note = "（" + step.Note + "）"
				}
				fmt.Printf("%s %s%s\n", map[string]string{storage.PlanPending: "[ ]", storage.PlanInProgress: "[~]", storage.PlanDone: "[x]"}[step.Status], step.Title, note)
			}
		}
		for _, message := range messages {
			switch message.Role {
			case storage.RoleUser:
				fmt.Printf("[你] %s\n", message.Content)
			case storage.RoleAssistant:
				fmt.Printf("[ICEPick] %s\n", message.Content)
			default:
				fmt.Printf("[%s] %s\n", message.Role, message.Content)
			}
		}
		if parsed.flags["calls"] {
			var calls []storage.ToolCall
			if err := client.Call("calls.list", map[string]any{"agentId": agentID, "sessionId": sessionID}, &calls); err != nil {
				return fail("calls.list", err)
			}
			fmt.Println("--- 工具调用 ---")
			for _, call := range calls {
				fmt.Printf("%s %s [%s] %s\n", call.ID, call.ToolName, call.State, call.Result)
			}
		}
		return 0
	default:
		return usage(fmt.Sprintf("未知 sessions 子命令 %q", args[0]))
	}
}

func cmdTasks(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("tasks 需要子命令 start|status|cancel")
	}
	switch args[0] {
	case "start":
		return startTask(client, args[1:])
	case "status":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: tasks status <runID> [--agent <agentID>]")
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		var run storage.Run
		if err := client.Call("runs.get", map[string]any{"agentId": agentID, "runId": parsed.arg(0)}, &run); err != nil {
			return fail("runs.get", err)
		}
		printRun(run)
		return 0
	case "cancel":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: tasks cancel <runID> [--agent <agentID>]")
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		if err := client.Call("tasks.cancel", map[string]any{"agentId": agentID, "runId": parsed.arg(0)}, nil); err != nil {
			return fail("tasks.cancel", err)
		}
		fmt.Println("已请求取消，宿主将在安全点落盘结果。")
		return 0
	default:
		return usage(fmt.Sprintf("未知 tasks 子命令 %q", args[0]))
	}
}

func startTask(client *ipc.Client, args []string) int {
	parsed, err := parseCLI(args, "wait")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(parsed.positional) < 3 {
		return usage("用法: tasks start <agentID> <sessionID> <任务内容…> [--wait]")
	}
	agentID := parsed.arg(0)
	sessionID := parsed.arg(1)
	input := joinArgs(parsed.positional[2:])
	var run storage.Run
	if err := client.Call("tasks.start", map[string]any{"agentId": agentID, "sessionId": sessionID, "input": input}, &run); err != nil {
		return fail("tasks.start", err)
	}
	fmt.Printf("任务已启动：run %s\n", run.ID)
	if !parsed.flags["wait"] {
		fmt.Println("提示：用 tasks status 跟踪进度，或加 --wait 跟随到结束。")
		return 0
	}
	var prompt approvalPrompter
	if interactiveTerminal() {
		prompt = terminalPrompter()
	}
	return waitTask(client, agentID, sessionID, run.ID, prompt, os.Stdout)
}

func joinArgs(parts []string) string {
	return trimText(strings.Join(parts, " "))
}

func cmdApprovals(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("approvals 需要子命令 list|approve|deny")
	}
	switch args[0] {
	case "list":
		parsed, err := parseCLI(args[1:])
		if err != nil {
			return 2
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		return listApprovals(client, agentID)
	case "approve":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: approvals approve <callID> [--agent <agentID>]")
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		var call storage.ToolCall
		if err := client.Call("calls.approve", map[string]any{"agentId": agentID, "callId": parsed.arg(0)}, &call); err != nil {
			return fail("calls.approve", err)
		}
		fmt.Printf("已批准 %s（%s）\n", call.ToolName, call.ID)
		return 0
	case "deny":
		parsed, err := parseCLI(args[1:])
		if err != nil || parsed.arg(0) == "" {
			return usage("用法: approvals deny <callID> [原因] [--agent <agentID>]")
		}
		agentID := parsed.value("agent")
		if agentID == "" {
			agentID = storage.DefaultAgentID
		}
		note := joinArgs(parsed.positional[1:])
		var call storage.ToolCall
		if err := client.Call("calls.deny", map[string]any{"agentId": agentID, "callId": parsed.arg(0), "note": note}, &call); err != nil {
			return fail("calls.deny", err)
		}
		fmt.Printf("已拒绝 %s（%s）\n", call.ToolName, call.ID)
		return 0
	default:
		return usage(fmt.Sprintf("未知 approvals 子命令 %q", args[0]))
	}
}

// listApprovals walks every session of the agent and prints its pending
// approval requests and unresolved unknown-result calls.
func listApprovals(client *ipc.Client, agentID string) int {
	var sessions []storage.Session
	if err := client.Call("sessions.list", map[string]any{"agentId": agentID}, &sessions); err != nil {
		return fail("sessions.list", err)
	}
	found := 0
	for _, session := range sessions {
		var calls []storage.ToolCall
		if err := client.Call("calls.list", map[string]any{"agentId": agentID, "sessionId": session.ID}, &calls); err != nil {
			continue
		}
		for _, call := range calls {
			if call.State == storage.CallPending {
				found++
				fmt.Printf("[待审批] %s (%s) 会话 %s\n  工具: %s\n  参数: %s\n", call.ID, session.Title, session.ID, call.ToolName, call.Arguments)
			}
			if call.State == storage.CallExecutionUnknow {
				found++
				fmt.Printf("[结果未知] %s (%s) 会话 %s\n  工具: %s\n  说明: %s\n", call.ID, session.Title, session.ID, call.ToolName, call.Error)
			}
		}
	}
	if found == 0 {
		fmt.Println("当前没有待处理的审批或结果未知调用。")
	}
	return 0
}

// approvalPrompter decides one pending call: approve=true grants it;
// approve=false with a note denies it. The skipNote sentinel (or a nil
// prompter, i.e. non-interactive mode) reports the request and leaves it
// pending — nothing is ever auto-approved.
type approvalPrompter func(call storage.ToolCall) (bool, string)

const skipNote = "__skip__"

// waitTask follows a run until it finishes, surfacing pending approvals.
// In non-interactive mode (prompter == nil) pending calls are printed and
// left untouched — they will time out or be handled on the desktop; nothing
// is auto-approved.
func waitTask(client *ipc.Client, agentID, sessionID, runID string, prompt approvalPrompter, out io.Writer) int {
	asked := map[string]bool{}
	for {
		var run storage.Run
		err := client.Call("runs.get", map[string]any{"agentId": agentID, "runId": runID}, &run)
		if err != nil {
			fmt.Fprintf(out, "查询任务失败: %v\n", err)
			return 1
		}
		var calls []storage.ToolCall
		if err := client.Call("calls.list", map[string]any{"agentId": agentID, "sessionId": sessionID}, &calls); err == nil {
			for _, call := range calls {
				if call.State != storage.CallPending || asked[call.ID] {
					continue
				}
				asked[call.ID] = true
				fmt.Fprintf(out, "需要审批：%s %s\n  参数: %s\n", call.ToolName, call.ID, call.Arguments)
				if prompt == nil {
					// 非交互：绝不默认放行，等待宿主超时或桌面端处理。
					fmt.Fprintln(out, "  非交互模式不会自动批准；可在桌面端或 approvals approve 命令中处理。")
					continue
				}
				approved, note := prompt(call)
				switch {
				case note == skipNote:
					fmt.Fprintln(out, "  保留待审批，稍后再处理。")
				case approved:
					if err := client.Call("calls.approve", map[string]any{"agentId": agentID, "callId": call.ID}, nil); err != nil {
						fmt.Fprintf(out, "  批准失败: %v\n", err)
					} else {
						fmt.Fprintln(out, "  已批准。")
					}
				default:
					if err := client.Call("calls.deny", map[string]any{"agentId": agentID, "callId": call.ID, "note": note}, nil); err != nil {
						fmt.Fprintf(out, "  拒绝失败: %v\n", err)
					} else {
						fmt.Fprintln(out, "  已拒绝。")
					}
				}
			}
			for _, call := range calls {
				if call.State == storage.CallExecutionUnknow && !asked[call.ID] {
					asked[call.ID] = true
					fmt.Fprintf(out, "结果未知：%s %s\n  说明: %s\n  请在桌面端说明依据后处置；任务在处置前无法继续。\n", call.ToolName, call.ID, call.Error)
				}
			}
		}
		if run.Status != "running" {
			printRun(run)
			return runStatusExitCode(run)
		}
		select {
		case <-client.Closed():
			fmt.Fprintln(out, "宿主连接已断开；任务仍由宿主持有，重启宿主后可读取最终状态。")
			return 1
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func printRun(run storage.Run) {
	fmt.Printf("run %s\n状态: %s\n错误: %s\nProfile: %s (v%d)\n开始: %s\n更新: %s\n",
		run.ID, run.Status, run.Error, run.ProfileID, run.ProfileRevision, run.CreatedAt, run.UpdatedAt)
}

func runStatusExitCode(run storage.Run) int {
	if run.Status == "completed" {
		return 0
	}
	return 1
}

func trimText(text string) string {
	start, end := 0, len(text)
	for start < end && (text[start] == ' ' || text[start] == '\n' || text[start] == '\t' || text[start] == '\r') {
		start++
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\n' || text[end-1] == '\t' || text[end-1] == '\r') {
		end--
	}
	return text[start:end]
}

// interactiveTerminal reports whether stdin is an interactive terminal, so
// only real users get approval prompts — pipes and CI never auto-approve.
func interactiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// terminalPrompter asks the user on the terminal for each pending call.
func terminalPrompter() approvalPrompter {
	reader := bufio.NewReader(os.Stdin)
	return func(call storage.ToolCall) (bool, string) {
		fmt.Printf("批准 %s 的这次调用？参数:\n%s\n[y/回车=批准, n=拒绝, s=跳过（保持待审批）] ", call.ToolName, call.Arguments)
		line, err := reader.ReadString('\n')
		if err != nil {
			return false, "CLI 输入结束"
		}
		switch trimText(line) {
		case "", "y", "Y", "yes":
			return true, ""
		case "n", "N", "no":
			fmt.Print("拒绝原因（可留空）: ")
			note, _ := reader.ReadString('\n')
			return false, trimText(note)
		default:
			return false, skipNote
		}
	}
}

func cmdLogs(args []string) int {
	lines := 50
	if len(args) > 0 {
		if _, err := fmt.Sscanf(args[0], "%d", &lines); err != nil || lines <= 0 {
			return usage("用法: logs [行数]")
		}
	}
	dataDir, err := dataDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	data, err := os.ReadFile(logPath(dataDir))
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取日志失败:", err)
		return 1
	}
	all := splitLines(string(data))
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	for _, line := range all[start:] {
		fmt.Println(line)
	}
	return 0
}
