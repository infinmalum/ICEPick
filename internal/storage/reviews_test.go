package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func reviewStorageFixture(t *testing.T, checks []string) (*Store, Conversation, Run, Run) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "reviews.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	p, err := s.CreateTemplateProfile(ctx, "daily", "")
	if err != nil {
		t.Fatal(err)
	}
	p.Data.Execution.Review = ReviewPolicy{Mode: "required", RequiredChecks: checks}
	p, err = s.UpdateProfile(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateConversation(ctx, p.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := s.CreateRunWithOptions(ctx, c.ID, CreateRunOptions{CaptureResources: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddMessage(ctx, c.ID, root.ID, RoleUser, "目标"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddMessage(ctx, c.ID, root.ID, RoleAssistant, "交付"); err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateDelegationSession(ctx, c.ContextID, "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	reviewer, _, err := s.CreateRunWithOptions(ctx, child.ID, CreateRunOptions{ParentRunID: root.ID, Depth: 1, WorkerName: "@reviewer", Component: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	return s, c, root, reviewer
}
func TestRequiredReviewCannotBypassOrReuseChangedDelivery(t *testing.T) {
	s, c, root, reviewer := reviewStorageFixture(t, nil)
	ctx := context.Background()
	if err := s.SetRunStatus(ctx, root.ID, "completed", ""); err == nil {
		t.Fatal("required review bypassed")
	}
	snap, err := s.ReviewSnapshot(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := ReviewResult{Status: "passed", Summary: "满足目标"}
	if _, err = s.SaveTaskReview(ctx, root.ID, reviewer.ID, snap.Binding, result); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddMessage(ctx, c.ID, root.ID, RoleAssistant, "修改后的交付"); err != nil {
		t.Fatal(err)
	}
	if err = s.SetRunStatus(ctx, root.ID, "completed", ""); err == nil {
		t.Fatal("old conclusion reused")
	}
	reviews, err := s.ListTaskReviews(ctx, root.ID)
	if err != nil || len(reviews) != 1 || reviews[0].Current {
		t.Fatal(reviews, err)
	}
	if _, err = s.SaveTaskReview(ctx, root.ID, reviewer.ID, snap.Binding, result); err == nil {
		t.Fatal("stale save accepted")
	}
}
func TestRequiredChecksNeedMachineReadableSuccess(t *testing.T) {
	for _, tc := range []struct {
		result string
		want   string
	}{{`{"passed":false}`, "inconclusive"}, {`tests passed`, "inconclusive"}, {`{"passed":true}`, "passed"}} {
		t.Run(tc.result, func(t *testing.T) {
			s, c, root, reviewer := reviewStorageFixture(t, []string{"tests__run"})
			ctx := context.Background()
			_, calls, err := s.AddAssistantMessage(ctx, c.ID, root.ID, "", []MessageToolCall{{ID: "test", Name: "tests__run", Arguments: "{}"}})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.BeginToolCall(ctx, calls[0].ID); err != nil {
				t.Fatal(err)
			}
			if err = s.FinishToolCall(ctx, calls[0].ID, CallSucceeded, tc.result, "", "public"); err != nil {
				t.Fatal(err)
			}
			snap, err := s.ReviewSnapshot(ctx, root.ID)
			if err != nil {
				t.Fatal(err)
			}
			review, err := s.SaveTaskReview(ctx, root.ID, reviewer.ID, snap.Binding, ReviewResult{Status: "passed", Summary: "审查", EvidenceCallIDs: []string{calls[0].ID}})
			if err != nil || review.Result.Status != tc.want {
				t.Fatal(review, err)
			}
		})
	}
}

func TestNonemptyV16ReviewMigration(t *testing.T) {
	s, _, root, reviewer := reviewStorageFixture(t, nil)
	ctx := context.Background()
	// The prior schema had only planner overrides. Preserve an existing logical
	// planner child plus all root configuration/messages during this upgrade.
	if _, err := s.db.Exec(`UPDATE run_components SET role='planner' WHERE run_id=?`, reviewer.ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TABLE review_requests`, `DROP TABLE task_reviews`, `PRAGMA user_version=16`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	path := s.path
	s.Close()
	next, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	child, err := next.GetRun(ctx, reviewer.ID)
	if err != nil || child.Component != "planner" {
		t.Fatal(child, err)
	}
	snap, err := next.ReviewSnapshot(ctx, root.ID)
	if err != nil || snap.Delivery.Content != "交付" || snap.Policy.Mode != "required" {
		t.Fatal(snap, err)
	}
	reviews, err := next.ListTaskReviews(ctx, root.ID)
	if err != nil || len(reviews) != 0 {
		t.Fatal(reviews, err)
	}
}
