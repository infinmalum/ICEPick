package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFileOperationsContractsAndContainment(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "sample.txt")
	if _, err := WriteFile(ctx, WriteArgs{Path: file, Content: "你好\nfirst\n"}, false, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile(ctx, WriteArgs{Path: file, Content: "oops"}, false, []string{dir}); err == nil {
		t.Fatal("overwrote without contract")
	}
	stale := "stale"
	if _, err := WriteFile(ctx, WriteArgs{Path: file, Content: "oops", ExpectedContent: &stale}, false, []string{dir}); err == nil {
		t.Fatal("stale write accepted")
	}
	old := "你好\nfirst\n"
	if _, err := WriteFile(ctx, WriteArgs{Path: file, Content: "你好\nsecond\n", ExpectedContent: &old}, false, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := EditFile(ctx, EditArgs{Path: file, OldText: "second", NewText: "third"}, false, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := EditFile(ctx, EditArgs{Path: file, OldText: "missing", NewText: "oops"}, false, []string{dir}); err == nil {
		t.Fatal("missing edit accepted")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "你好\nthird\n" {
		t.Fatal(string(data))
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err != nil {
		t.Skip(err)
	}
	escaped := filepath.Join(dir, "escape", "new.txt")
	if _, err := WriteFile(ctx, WriteArgs{Path: escaped, Content: "escape"}, false, []string{dir}); err == nil {
		t.Fatal("symlink escaped root")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("outside file touched")
	}
}
func TestEditMultipleMatchesAndCancelledWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "repeat.txt")
	os.WriteFile(file, []byte("repeat repeat"), 0640)
	if _, err := EditFile(context.Background(), EditArgs{Path: file, OldText: "repeat", NewText: "x"}, false, []string{dir}); err == nil {
		t.Fatal("ambiguous edit accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	old := "repeat repeat"
	if _, err := WriteFile(ctx, WriteArgs{Path: file, Content: "changed", ExpectedContent: &old}, false, []string{dir}); err == nil {
		t.Fatal("cancelled write accepted")
	}
	data, _ := os.ReadFile(file)
	if string(data) != old {
		t.Fatal("file changed")
	}
}
func TestExecHelperProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" && i+1 < len(os.Args) {
			switch os.Args[i+1] {
			case "success":
				os.Stdout.WriteString("你好\n" + os.Getenv("TMPDIR"))
				os.Exit(0)
			case "failure":
				os.Stderr.WriteString("failed")
				os.Exit(7)
			case "timeout":
				time.Sleep(30 * time.Second)
				os.Exit(0)
			case "large":
				os.Stdout.WriteString(strings.Repeat("\x01", 20000))
				os.Exit(0)
			}
			return
		}
	}
}
func TestExecResultsTimeoutAndBoundedJSON(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		code    int
		timeout bool
	}{{"success", 0, false}, {"failure", 7, false}, {"timeout", -1, true}, {"large", 0, false}} {
		t.Run(tc.mode, func(t *testing.T) {
			timeout := 3
			if tc.timeout {
				timeout = 1
			}
			raw, err := Exec(context.Background(), ExecArgs{Executable: os.Args[0], Args: []string{"-test.run=TestExecHelperProcess", "--", tc.mode}, Cwd: t.TempDir(), TimeoutSeconds: timeout})
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				ExitCode  int    `json:"exitCode"`
				Stdout    string `json:"stdout"`
				Stderr    string `json:"stderr"`
				Passed    bool   `json:"passed"`
				TimedOut  bool   `json:"timedOut"`
				Truncated bool   `json:"truncated"`
			}
			if err := json.Unmarshal([]byte(raw), &result); err != nil {
				t.Fatal("invalid JSON", err)
			}
			if len(raw) > 15000 || result.ExitCode != tc.code || result.TimedOut != tc.timeout || result.Passed != (tc.code == 0 && !tc.timeout) {
				t.Fatalf("unexpected command outcome: code=%d timeout=%t passed=%t bytes=%d", result.ExitCode, result.TimedOut, result.Passed, len(raw))
			}
			// The child must get Exec's private exec-tmp dir, never the
			// shared default /tmp. The test binary itself lives under a
			// temporary directory, so the path prefix proves nothing; only
			// the bare default directory is the regression.
			if tc.mode == "success" && (!strings.Contains(result.Stdout, "你好") || strings.HasSuffix(result.Stdout, "\n/tmp")) {
				t.Fatal("temporary directory not persistent", raw)
			}
			if tc.mode == "large" && !result.Truncated {
				t.Fatal("output not bounded")
			}
		})
	}
}

func TestExecCancellationPreservesUnknownOutcome(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	_, err := Exec(ctx, ExecArgs{Executable: os.Args[0], Args: []string{"-test.run=TestExecHelperProcess", "--", "timeout"}, Cwd: t.TempDir(), TimeoutSeconds: 5})
	var unknown *UnknownOutcomeError
	if !errors.As(err, &unknown) || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost uncertainty", err)
	}
}
func TestConcurrentFileCompareAndReplaceHasOneWinner(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "concurrent.txt")
	if err := os.WriteFile(file, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	old := "base"
	results := make(chan error, 2)
	for _, content := range []string{"one", "two"} {
		go func(content string) {
			_, err := WriteFile(context.Background(), WriteArgs{Path: file, Content: content, ExpectedContent: &old}, false, []string{dir})
			results <- err
		}(content)
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("compare-and-replace winners", successes)
	}
}
