package storage

import (
	"context"
	"testing"
)

func TestExfilGrantCRUDAndValidation(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if err := store.SetExfilGrant(ctx, ExfilGrant{Destination: "http://api.example.com:8080", Scopes: []string{ScopePersonal}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetExfilGrant(ctx, ExfilGrant{Destination: "", Scopes: []string{ScopePersonal}}); err == nil {
		t.Fatal("empty destination accepted")
	}
	if err := store.SetExfilGrant(ctx, ExfilGrant{Destination: "http://api.example.com:8080", Scopes: nil}); err == nil {
		t.Fatal("empty scopes accepted")
	}
	if err := store.SetExfilGrant(ctx, ExfilGrant{Destination: "http://x:1", Scopes: []string{"galaxy"}}); err == nil {
		t.Fatal("unknown scope accepted")
	}
	// Upsert extends the scope list in place.
	if err := store.SetExfilGrant(ctx, ExfilGrant{Destination: "http://api.example.com:8080", Scopes: []string{ScopePersonal, ScopeWorkspace}}); err != nil {
		t.Fatal(err)
	}
	grant, found, err := store.GetExfilGrant(ctx, "http://api.example.com:8080")
	if err != nil || !found || len(grant.Scopes) != 2 {
		t.Fatalf("grant: %+v found=%v %v", grant, found, err)
	}
	items, err := store.ListExfilGrants(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %+v %v", items, err)
	}
	if err := store.DeleteExfilGrant(ctx, "http://other.example.com"); err == nil {
		t.Fatal("deleting unknown grant accepted")
	}
	if err := store.DeleteExfilGrant(ctx, "http://api.example.com:8080"); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := store.GetExfilGrant(ctx, "http://api.example.com:8080"); found {
		t.Fatal("grant survived deletion")
	}
}

func TestSessionDataScopes(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	session, err := store.CreateSession(ctx, "test")
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := store.CreateRun(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{{ID: "c1", Name: "local__read_file", Arguments: "{}"}}); err != nil {
		t.Fatal(err)
	}
	calls, _ := store.ListSessionToolCalls(ctx, session.ID)
	if err := store.BeginToolCall(ctx, calls[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolCall(ctx, calls[0].ID, CallSucceeded, "content", "", ScopeCredential); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, session.ID, run.ID, RoleUser, "帮我看看"); err != nil {
		t.Fatal(err)
	}
	hasUser, scopes, err := store.SessionDataScopes(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasUser || len(scopes) != 1 || scopes[0] != ScopeCredential {
		t.Fatalf("scopes: hasUser=%v scopes=%v", hasUser, scopes)
	}
	// A failed result contributes no scope: only content that actually
	// entered the model context as a result counts.
	if _, _, err := store.AddAssistantMessage(ctx, session.ID, run.ID, "", []MessageToolCall{{ID: "c2", Name: "local__read_file", Arguments: "{}"}}); err != nil {
		t.Fatal(err)
	}
	calls, _ = store.ListSessionToolCalls(ctx, session.ID)
	if err := store.FinishToolCall(ctx, calls[1].ID, CallFailed, "", "boom", ScopeCredential); err != nil {
		t.Fatal(err)
	}
	hasUser, scopes, err = store.SessionDataScopes(ctx, session.ID)
	if err != nil || len(scopes) != 1 {
		t.Fatalf("failed result must not add scope: %v %v", scopes, err)
	}
}

func TestMCPEndpointRegistry(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	if _, err := store.GetMCPEndpoint(ctx, "files"); err == nil {
		t.Fatal("unknown endpoint returned")
	}
	if err := store.SetMCPEndpoint(ctx, MCPEndpoint{Name: "files", Command: "files-mcp", BinarySHA256: "aaa", SchemaSHA256: "111", Version: "1.0"}); err != nil {
		t.Fatal(err)
	}
	item, err := store.GetMCPEndpoint(ctx, "files")
	if err != nil || item.BinarySHA256 != "aaa" || item.Version != "1.0" {
		t.Fatalf("endpoint: %+v %v", item, err)
	}
	if err := store.SetMCPEndpoint(ctx, MCPEndpoint{Name: "files", Command: "files-mcp", BinarySHA256: "bbb", SchemaSHA256: "111", Version: "1.1"}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListMCPEndpoints(ctx)
	if err != nil || len(items) != 1 || items[0].BinarySHA256 != "bbb" || items[0].Version != "1.1" {
		t.Fatalf("upsert: %+v %v", items, err)
	}
	if err := store.SetMCPEndpoint(ctx, MCPEndpoint{Name: "", Command: "x"}); err == nil {
		t.Fatal("empty name accepted")
	}
}
