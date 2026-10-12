package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// The v11 delegation structures: hidden delegation sessions, parent-child
// run linkage and the agent-side delegation allowlist.
func TestDelegationStorage(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()

	target, err := store.CreateAgent(ctx, "worker", DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	parentSession, err := store.CreateAgentSession(ctx, DefaultAgentID, "父会话")
	if err != nil {
		t.Fatal(err)
	}
	// A delegation session belongs to the target agent and is hidden from
	// the normal list, visible in the full sweep.
	delegationSession, err := store.CreateDelegationSession(ctx, target.ID, "委派子任务 → worker")
	if err != nil {
		t.Fatal(err)
	}
	if delegationSession.Kind != SessionKindDelegation {
		t.Fatalf("delegation session kind: %+v", delegationSession)
	}
	normal, err := store.ListAgentSessions(ctx, target.ID)
	if err != nil || len(normal) != 0 {
		t.Fatalf("delegation session leaked into normal list: %+v %v", normal, err)
	}
	all, err := store.ListAgentSessionsAll(ctx, target.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("delegation session missing from full list: %+v %v", all, err)
	}

	parentRun, _, err := store.CreateRun(ctx, parentSession.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parentRun.Depth != 0 || parentRun.ParentRunID != "" {
		t.Fatalf("top-level run linkage: %+v", parentRun)
	}
	childRun, childResolved, err := store.CreateRunWithOptions(ctx, delegationSession.ID, CreateRunOptions{ParentRunID: parentRun.ID, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if childRun.ParentRunID != parentRun.ID || childRun.Depth != 1 {
		t.Fatalf("child run linkage: %+v", childRun)
	}
	if childResolved.AgentID != target.ID {
		t.Fatalf("child run resolved the wrong agent: %+v", childResolved)
	}
	children, err := store.ListChildRuns(ctx, parentRun.ID)
	if err != nil || len(children) != 1 || children[0].ID != childRun.ID {
		t.Fatalf("ListChildRuns: %+v %v", children, err)
	}
	if _, err := store.AddMessage(ctx, delegationSession.ID, childRun.ID, RoleUser, "同样的任务"); err != nil {
		t.Fatal(err)
	}
	first, err := store.FirstUserMessage(ctx, delegationSession.ID)
	if err != nil || first != "同样的任务" {
		t.Fatalf("FirstUserMessage: %q %v", first, err)
	}
	chain, err := store.AncestorRunIDs(ctx, childRun.ID)
	if err != nil || len(chain) != 2 || chain[0] != parentRun.ID || chain[1] != childRun.ID {
		t.Fatalf("AncestorRunIDs: %+v %v", chain, err)
	}
}

// The delegation allowlist is agent-local authorization over real instances:
// unknown targets are refused on save, and an agent still listed as a target
// cannot be deleted.
func TestDelegationTargetsPolicy(t *testing.T) {
	store := openTestStore(t)
	defer store.Close()
	ctx := context.Background()

	target, err := store.CreateAgent(ctx, "worker", DefaultProfileID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.GetAgent(ctx, DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	agent.Policy.DelegationTargets = []string{target.ID, "missing-agent"}
	if _, err := store.UpdateAgentLocal(ctx, agent.ID, agent.Name, agent.Policy); err == nil || !strings.Contains(err.Error(), "委派目标不存在") {
		t.Fatalf("unknown delegation target accepted: %v", err)
	}
	agent.Policy.DelegationTargets = []string{target.ID}
	if _, err := store.UpdateAgentLocal(ctx, agent.ID, agent.Name, agent.Policy); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetAgent(ctx, DefaultAgentID)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(saved.Policy)
	if !strings.Contains(string(raw), target.ID) {
		t.Fatalf("targets not persisted: %s", raw)
	}
	if err := store.DeleteAgent(ctx, target.ID); err == nil || !strings.Contains(err.Error(), "委派允许列表") {
		t.Fatalf("deleting a referenced target succeeded: %v", err)
	}
	agent.Policy.DelegationTargets = nil
	if _, err := store.UpdateAgentLocal(ctx, agent.ID, agent.Name, agent.Policy); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAgent(ctx, target.ID); err != nil {
		t.Fatalf("delete after target removal: %v", err)
	}
}
