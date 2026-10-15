package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"icepick/internal/ipc"
	"icepick/internal/storage"
)

func cmdProfileDraft(client *ipc.Client, args []string) int {
	parsed, err := parseCLI(args)
	if err != nil || len(parsed.positional) != 2 {
		return usage("profiles draft <conversationID> <配置需求> [--target <ProfileID>] [--evidence <runID,runID>]")
	}
	if err = parsed.allowValues("target", "evidence"); err != nil {
		return usage(err.Error())
	}
	var run storage.Run
	if err = client.Call("drafts.start", map[string]any{"sessionId": parsed.arg(0), "target": parsed.value("target"), "request": parsed.arg(1), "evidenceIds": draftEvidenceIDs(parsed.value("evidence"))}, &run); err != nil {
		return fail("生成配置草案", err)
	}
	fmt.Printf("草案生成任务 %s；完成后用 profiles drafts list 查看。未采用配置。\n", run.ID)
	return 0
}
func cmdProfileDrafts(client *ipc.Client, args []string) int {
	if len(args) == 0 {
		return usage("profiles drafts list | show <id> | edit <id> <proposal.json> --revision N | apply|discard|revert <id> --revision N")
	}
	var drafts []storage.ProfileDraft
	if err := client.Call("drafts.list", nil, &drafts); err != nil {
		return fail("草案列表", err)
	}
	if args[0] == "list" {
		rows := [][]string{{"ID", "状态", "草案修订", "目标", "说明"}}
		for _, d := range drafts {
			rows = append(rows, []string{d.ID, d.State, strconv.Itoa(d.Revision), d.TargetProfileID, d.Proposal.Summary})
		}
		printTable(rows)
		return 0
	}
	parsed, err := parseCLI(args[1:])
	if err != nil || parsed.arg(0) == "" {
		return usage("请提供草案ID")
	}
	if err = parsed.allowValues("revision"); err != nil {
		return usage(err.Error())
	}
	var draft storage.ProfileDraft
	for _, d := range drafts {
		if d.ID == parsed.arg(0) {
			draft = d
		}
	}
	if draft.ID == "" {
		return usage("草案不存在")
	}
	if args[0] == "show" {
		raw, _ := json.MarshalIndent(draft, "", "  ")
		fmt.Println(string(raw))
		fmt.Println("草案尚未经过真实场景验收；采用前检查私人信息、差异和依赖。影响范围包括引用 Profile 的后续对话及 worker，在途任务保留快照。")
		return 0
	}
	revision, err := strconv.Atoi(parsed.value("revision"))
	if err != nil || revision < 1 {
		return usage("请用 --revision N 指定已查看的草案修订")
	}
	params := map[string]any{"id": draft.ID, "revision": revision}
	switch args[0] {
	case "edit":
		raw, err := os.ReadFile(parsed.arg(1))
		if err != nil {
			return fail("读取草案JSON", err)
		}
		if len(raw) > 512*1024 {
			return usage("草案文件超过512KiB")
		}
		var proposal storage.ProfileProposal
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&proposal); err != nil {
			return fail("解析草案", err)
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			return usage("JSON后有多余内容")
		}
		params["proposal"] = proposal
		var saved storage.ProfileDraft
		if err = client.Call("drafts.edit", params, &saved); err != nil {
			return fail("编辑草案", err)
		}
		fmt.Printf("草案已保存，修订%d；尚未采用。\n", saved.Revision)
	case "apply", "revert":
		var profile storage.Profile
		if err = client.Call("drafts."+args[0], params, &profile); err != nil {
			return fail(args[0], err)
		}
		fmt.Printf("Profile %s 已发布为v%d；仅影响后续运行。\n", profile.ID, profile.Revision)
	case "discard":
		if err = client.Call("drafts.discard", params, nil); err != nil {
			return fail("丢弃草案", err)
		}
		fmt.Println("草案已丢弃")
	default:
		return usage("未知草案子命令")
	}
	return 0
}

func draftEvidenceIDs(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	out := strings.Split(value, ",")
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return out
}
