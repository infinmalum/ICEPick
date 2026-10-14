package app

import (
	"context"
	"strings"
	"testing"

	"icepick/internal/model"
	"icepick/internal/storage"
)

func findProfile(t *testing.T, svc *Service, id string) storage.Profile {
	t.Helper()
	profiles, err := svc.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range profiles {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("profile %s not found", id)
	return storage.Profile{}
}

// A system-prompt script asserts, on its second turn, which prompt the run
// is actually executing with.
func promptScript(t *testing.T, call model.ToolCall, wantPrompt string, firstTurnCallsTool bool) *scriptModel {
	chat := &scriptModel{steps: []func([]model.Message) (model.ChatResponse, error){
		func([]model.Message) (model.ChatResponse, error) {
			if firstTurnCallsTool {
				return model.ChatResponse{Message: model.Message{Role: "assistant", ToolCalls: []model.ToolCall{call}}}, nil
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "好的"}}, nil
		},
		func(messages []model.Message) (model.ChatResponse, error) {
			if !strings.Contains(messages[0].Content, wantPrompt) {
				t.Fatalf("run used prompt %q, want %q", messages[0].Content, wantPrompt)
			}
			return model.ChatResponse{Message: model.Message{Role: "assistant", Content: "收到"}}, nil
		},
	}}
	if firstTurnCallsTool && !strings.HasPrefix(call.Function.Name, "local__") {
		chat.steps = append([]func([]model.Message) (model.ChatResponse, error){discoverToolStep(call.Function.Name)}, chat.steps...)
	}
	return chat
}

// Switching an agent to another profile keeps sessions and history while the
// next run starts executing with the newly selected combination.
func TestAgentProfileSwitching(t *testing.T) {
	svc, store := newTestService(t, &scriptModel{}, &scriptTools{})
	data := storage.BlankProfile()
	data.SystemPrompt = "专属提示词 A"
	profile, err := store.CreateProfile(context.Background(), "A", data)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := svc.CreateAgent("甲", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateAgentSession(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc.chat = promptScript(t, extToolCall("c", "local__current_time", "{}"), "专属提示词 A", false)
	run, err := svc.StartAgentTask(agent.ID, session.ID, "你好")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, agent.ID, run.ID, "completed")
	runRow, err := svc.GetRun(agent.ID, run.ID)
	if err != nil || runRow.ProfileID != profile.ID || runRow.ProfileRevision != 1 {
		t.Fatalf("run profile record: %+v %v", runRow, err)
	}

	if _, err := svc.SwitchAgentProfile(agent.ID, storage.DefaultProfileID); err != nil {
		t.Fatal(err)
	}
	// Sessions and history stay with the agent across the switch.
	messages, err := svc.ListMessages(agent.ID, session.ID)
	if err != nil || len(messages) == 0 {
		t.Fatalf("history must survive the switch: %+v %v", messages, err)
	}
	svc.chat = promptScript(t, extToolCall("c2", "local__current_time", "{}"), "你是 ICEPick 助手", false)
	run2, err := svc.StartAgentTask(agent.ID, session.ID, "再问")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, agent.ID, run2.ID, "completed")
}

// Editing a shared profile changes every referencing agent's subsequent runs.
func TestSharedProfileEditAffectsSubsequentRuns(t *testing.T) {
	svc, _ := newTestService(t, &scriptModel{}, &scriptTools{})
	agentA, err := svc.CreateAgent("甲", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	agentB, err := svc.CreateAgent("乙", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	sessionA, err := svc.CreateAgentSession(agentA.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessionB, err := svc.CreateAgentSession(agentB.ID)
	if err != nil {
		t.Fatal(err)
	}
	def := findProfile(t, svc, storage.DefaultProfileID)
	def.Data.SystemPrompt = "共享提示词 v2"
	if _, err := svc.UpdateProfile(def); err != nil {
		t.Fatal(err)
	}
	svc.chat = promptScript(t, extToolCall("c", "local__current_time", "{}"), "共享提示词 v2", false)
	for _, tc := range []struct {
		agentID   string
		sessionID string
	}{{agentA.ID, sessionA.ID}, {agentB.ID, sessionB.ID}} {
		run, err := svc.StartAgentTask(tc.agentID, tc.sessionID, "你好")
		if err != nil {
			t.Fatal(err)
		}
		waitForRun(t, svc, tc.agentID, run.ID, "completed")
	}
}

// An in-flight run keeps executing with the profile snapshot it started
// with, even after the profile is edited while a tool call waits for
// approval; the next run uses the new content. The run row records both the
// profile id and the revision it snapshotted.
func TestInFlightRunKeepsProfileSnapshot(t *testing.T) {
	release := make(chan struct{})
	tools := &scriptTools{defs: extToolDef("extsvc__ping"), respond: func(name, _ string) (string, error) {
		<-release
		return "pong", nil
	}}
	svc, store := newTestService(t, &scriptModel{}, tools)
	data := storage.BlankProfile()
	data.SystemPrompt = "旧提示词"
	data.AllMCPServers = true
	profile, err := store.CreateProfile(context.Background(), "P", data)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := svc.CreateAgent("甲", profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateAgentSession(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	setAgentPolicy(t, svc, agent.ID, func(p *storage.AgentPolicy) { p.ApprovalMode = "semi" })
	svc.chat = promptScript(t, extToolCall("call-1", "extsvc__ping", "{}"), "旧提示词", true)
	run, err := svc.StartAgentTask(agent.ID, session.ID, "ping 一下")
	if err != nil {
		t.Fatal(err)
	}
	call := waitForCallState(t, svc, agent.ID, session.ID, storage.CallPending)

	// Edit the shared profile while the run waits for approval.
	target := findProfile(t, svc, profile.ID)
	target.Data.SystemPrompt = "新提示词"
	target.Data.AllMCPServers = false
	target.Data.EnabledMCPServers = nil
	if _, err := svc.UpdateProfile(target); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveToolCall(agent.ID, call.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForRun(t, svc, agent.ID, run.ID, "completed")
	runRow, err := svc.GetRun(agent.ID, run.ID)
	if err != nil || runRow.ProfileID != profile.ID || runRow.ProfileRevision != 1 {
		t.Fatalf("in-flight run must record its snapshot revision: %+v %v", runRow, err)
	}
	// The next run resolves the newly committed content.
	svc.chat = promptScript(t, extToolCall("call-2", "local__current_time", "{}"), "新提示词", false)
	run2, err := svc.StartAgentTask(agent.ID, session.ID, "再问")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, agent.ID, run2.ID, "completed")
}

// A profile copy is an independent combination: editing the copy leaves the
// original untouched, and copy edits do not leak into other agents.
func TestProfileCopyIndependent(t *testing.T) {
	svc, _ := newTestService(t, &scriptModel{}, &scriptTools{})
	copied, err := svc.CreateProfile("", storage.DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	if copied.ID == storage.DefaultProfileID || copied.IsDefault || !strings.HasSuffix(copied.Name, "副本") {
		t.Fatalf("copy identity: %+v", copied)
	}
	copied.Data.SystemPrompt = "副本提示词"
	if _, err := svc.UpdateProfile(copied); err != nil {
		t.Fatal(err)
	}
	original := findProfile(t, svc, storage.DefaultProfileID)
	if strings.Contains(original.Data.SystemPrompt, "副本提示词") {
		t.Fatalf("copy edit leaked into the original: %+v", original)
	}
	agent, err := svc.CreateAgent("甲", copied.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.CreateAgentSession(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	svc.chat = promptScript(t, extToolCall("c", "local__current_time", "{}"), "副本提示词", false)
	run, err := svc.StartAgentTask(agent.ID, session.ID, "你好")
	if err != nil {
		t.Fatal(err)
	}
	waitForRun(t, svc, agent.ID, run.ID, "completed")
}

// Saving a combination whose capability references do not exist on this host
// is rejected before anything is persisted.
func TestProfileResourceValidation(t *testing.T) {
	svc, _ := newTestService(t, &scriptModel{}, &scriptTools{})
	def := findProfile(t, svc, storage.DefaultProfileID)
	def.Data.AllSkills = false
	def.Data.EnabledSkills = []string{"no-such-skill"}
	if _, err := svc.UpdateProfile(def); err == nil || !strings.Contains(err.Error(), "Skill") {
		t.Fatalf("missing skill accepted: %v", err)
	}
	def = findProfile(t, svc, storage.DefaultProfileID)
	def.Data.AllMCPServers = false
	def.Data.EnabledMCPServers = []string{"no-such-server"}
	if _, err := svc.UpdateProfile(def); err == nil || !strings.Contains(err.Error(), "MCP") {
		t.Fatalf("missing MCP server accepted: %v", err)
	}
	// The stored combination is unchanged after the failed saves.
	def = findProfile(t, svc, storage.DefaultProfileID)
	if !def.Data.AllSkills || !def.Data.AllMCPServers || def.Revision != 1 {
		t.Fatalf("failed save must leave the profile untouched: %+v", def)
	}
}
