package ops

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcceptanceExecutionSuccess(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()
	commands := []string{"printf first > order; printf stdout; printf stderr >&2", "cat order; printf second >> order"}
	results, err := executeAcceptanceCommands("task-1", dir, commands, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Output != "stdoutstderr" || results[1].Output != "first" {
		t.Fatalf("unexpected command results: %#v", results)
	}
	for i, result := range results {
		if result.Command != commands[i] || result.ExitCode != 0 || result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) || result.StartedAt.Location() != time.UTC {
			t.Fatalf("invalid execution record: %#v", result)
		}
	}
	if results[1].StartedAt.Before(results[0].FinishedAt) {
		t.Fatal("canonical commands ran out of order")
	}
	contents, err := os.ReadFile(filepath.Join(dir, "order"))
	if err != nil || string(contents) != "firstsecond" {
		t.Fatalf("commands did not share the specified worktree: %q, %v", contents, err)
	}
}

func TestAcceptanceExecutionFailureStopsBatch(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()
	results, err := executeAcceptanceCommands("task-failure", dir, []string{"touch first-completed", "printf diagnostic >&2; exit 7", "touch should-not-exist"}, 5)
	if err == nil || !strings.Contains(err.Error(), "acceptance.execution[1]") || !strings.Contains(err.Error(), "exit 7") {
		t.Fatalf("want actionable failed-command error, got %v", err)
	}
	if results != nil {
		t.Fatal("failed batch returned partial execution results")
	}
	if _, err := os.Stat(filepath.Join(dir, "first-completed")); err != nil {
		t.Fatalf("first command did not complete before later failure: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatalf("batch continued after failure: %v", err)
	}
}

func TestAcceptanceExecutionOneBatchTimeout(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()
	start := time.Now()
	results, err := executeAcceptanceCommands("task-timeout", dir, []string{"sleep 0.6; touch first-completed", "sleep 0.6"}, 1)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want one deadline across both commands, got %#v, %v", results, err)
	}
	if results != nil {
		t.Fatal("timed-out batch returned partial execution results")
	}
	if _, err := os.Stat(filepath.Join(dir, "first-completed")); err != nil {
		t.Fatalf("first command did not complete before batch deadline: %v", err)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("timeout did not bound command execution")
	}
}

func TestAcceptanceExecutionTimeoutKillsChildren(t *testing.T) {
	requirePosixShell(t)
	dir := t.TempDir()
	results, err := executeAcceptanceCommands("task-child", dir, []string{"(sleep 2; printf survived > survivor) & wait"}, 1)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout, got %v", err)
	}
	if results != nil {
		t.Fatal("timed-out process tree returned execution results")
	}
	// An orphaned child would write after the runner returned. Observe past its
	// scheduled write time, rather than accepting a fast parent exit as proof.
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "survivor")); !os.IsNotExist(err) {
		t.Fatalf("descendant survived cancellation: %v", err)
	}
}

func TestAcceptanceExecutionOutputLimitIsAggregate(t *testing.T) {
	requirePosixShell(t)
	results, err := executeAcceptanceCommands("task-noisy", t.TempDir(), []string{"head -c 600000 /dev/zero", "head -c 600000 /dev/zero"}, 5)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("want aggregate output rejection, got %v", err)
	}
	if results != nil {
		t.Fatal("output overflow returned partial execution results")
	}
}

func TestAcceptanceExecutionOutputLimitCancelsUnboundedProducer(t *testing.T) {
	requirePosixShell(t)
	start := time.Now()
	results, err := executeAcceptanceCommands("task-unbounded", t.TempDir(), []string{"while :; do head -c 600000 /dev/zero; done"}, 10)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("want output overflow, got %v", err)
	}
	if results != nil {
		t.Fatal("unbounded producer returned execution results")
	}
	if time.Since(start) > 6*time.Second {
		t.Fatal("output overflow did not cancel unbounded producer")
	}
}

func TestAcceptanceExecutionMasksCredentials(t *testing.T) {
	requirePosixShell(t)
	t.Setenv("ACCEPTANCE_TEST_TOKEN", "synthetic-token-152")
	t.Setenv("ACCEPTANCE_DB_URL", "postgres://tester:q7@localhost/sample")
	t.Setenv("ACCEPTANCE_DB_DSN", "host=localhost password='synthetic dsn password' dbname=sample")
	command := `printf '%s\n' "$ACCEPTANCE_TEST_TOKEN" "$ACCEPTANCE_DB_URL" "$ACCEPTANCE_DB_DSN" 'q7' 'synthetic dsn password'`
	results, err := executeAcceptanceCommands("task-mask", t.TempDir(), []string{command}, 5)
	if err != nil || len(results) != 1 {
		t.Fatalf("expected masked successful result, got %d results, %v", len(results), err)
	}
	combined := results[0].Command + results[0].Output
	for _, value := range []string{"synthetic-token-152", "postgres://tester:q7@localhost/sample", "q7", "synthetic dsn password"} {
		if strings.Contains(combined, value) {
			t.Fatal("execution record or error exposed a synthetic credential")
		}
	}
	if strings.Count(results[0].Output, "***") != 5 {
		t.Fatal("execution output was not retained with each known credential masked")
	}
	if results[0].Command == command || results[0].CommandSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(command))) {
		t.Fatal("masked display lost canonical command identity")
	}
	results, err = executeAcceptanceCommands("task-synthetic-token-152", t.TempDir(), []string{"exit 9"}, 5)
	if err == nil || !strings.Contains(err.Error(), "exit 9") || strings.Contains(err.Error(), "synthetic-token-152") || results != nil {
		t.Fatal("failed batch did not return only its masked error")
	}
}
