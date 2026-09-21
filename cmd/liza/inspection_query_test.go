package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/brandrender"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestInspectionQueryCLI(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	reason := "Missing recovery test\nKeep the original boundary."
	now := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)
	state.Tasks = []models.Task{{ID: "work.1", Status: models.TaskStatusRejected, RejectionReason: &reason,
		History: []models.TaskHistoryEntry{{Time: now, Event: models.TaskEventRejected, Reason: &reason}}}}
	testhelpers.WriteInitialState(t, statePath, state)

	for _, args := range [][]string{
		{"get", "tasks", "--field", "id,rejection_reason", "--json"},
		{"get-tasks", "--field", "id", "--field", "rejection_reason", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			output, err := executeRootCommandCapture(t, root, args...)
			if err != nil {
				t.Fatalf("%v: %s", err, output)
			}
			env := parseEnvelope(t, output)
			rows := env["result"].([]any)
			if env["ok"] != true || len(rows) != 1 {
				t.Fatalf("unexpected envelope: %s", output)
			}
			row := rows[0].(map[string]any)
			if len(row) != 2 || row["id"] != "work.1" || row["rejection_reason"] != reason {
				t.Fatalf("unexpected projection: %v", row)
			}
		})
	}
	for _, query := range []string{"tasks.work.1.rejection_reason", "work.1.rejection_reason", "task.work.1.rejection_reason"} {
		output, err := executeRootCommandCapture(t, root, "get", query, "--json")
		if err != nil {
			t.Fatalf("%s: %v: %s", query, err, output)
		}
		if env := parseEnvelope(t, output); env["ok"] != true || env["result"] != reason {
			t.Fatalf("query %q: %s", query, output)
		}
	}
	for _, args := range [][]string{
		{"get", "tasks.work.1.history", "--json"},
		{"get-tasks", "work.1", "--field", "history", "--json"},
	} {
		output, err := executeRootCommandCapture(t, root, args...)
		if err != nil {
			t.Fatal(err)
		}
		result := parseEnvelope(t, output)["result"]
		if row, ok := result.(map[string]any); ok {
			result = row["history"]
		}
		entry := result.([]any)[0].(map[string]any)
		if len(entry) != 3 || entry["time"] != now.Format(time.RFC3339) || entry["event"] != models.TaskEventRejected || entry["reason"] != reason {
			t.Fatalf("history JSON uses unexpected keys: %s", output)
		}
	}
	for _, args := range [][]string{
		{"get-tasks", "--field=", "--json"},
		{"get-tasks", "--field", "typo", "--json"},
		{"get", "tasks", "work.1", "ignored", "--json"},
		{"get", "config.mode", "--field", "id", "--json"},
	} {
		output, err := executeRootCommandCapture(t, root, args...)
		if err == nil {
			t.Fatalf("invalid query %v succeeded: %s", args, output)
		}
		if env := parseEnvelope(t, output); env["ok"] != false || env["error"] == nil {
			t.Fatalf("invalid query %v has no structured error: %s", args, output)
		}
	}
}

func TestInspectionSnapshotCLIWhileLocked(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{ID: "task-1", RolePair: "us-writing-pair", Status: "US_APPROVED"}}
	testhelpers.WriteInitialState(t, statePath, state)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	go func() {
		done <- filelock.New(statePath).WithLockOperation("independent CLI test writer", func() error {
			close(held)
			<-release
			return nil
		})
	}()
	defer func() {
		unlock()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not acquire lock")
	}
	// Release on failure to keep the CLI test bounded without abandoning a
	// command goroutine or leaking Cobra/global stdout state into later tests.
	timer := time.AfterFunc(5*time.Second, unlock)
	defer timer.Stop()
	for _, args := range [][]string{
		{"status", "--json"}, {"status", "--format", "json"},
		{"status", "--format", "yaml"}, {"status"},
		{"get", "tasks", "--json"},
		{"get", "tasks.task-1.history", "--json"},
		{"get-tasks", "--field", "id,rejection_reason,rejection_rca", "--json"},
	} {
		if output, err := executeRootCommandCapture(t, root, args...); err != nil {
			t.Fatalf("%v: %v: %s", args, err, output)
		}
		select {
		case <-release:
			t.Fatalf("%v did not return while the writer held the lock", args)
		default:
		}
	}
}

func TestInspectionNonDefaultBrand(t *testing.T) {
	bin := buildNonDefaultBrandBinary(t)
	for _, command := range []string{"get", "get-tasks"} {
		help := runBrandSmokeCommand(t, bin, command, "--help")
		assertContains(t, help, "acme-agent")
		assertContains(t, help, "rejection_rca")
		assertNoDefaultBrandLeaks(t, command+" help", help)
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "support-docs", "SUPPORT.md"))
	if err != nil {
		t.Fatal(err)
	}
	values := brand.RuntimeValues()
	values.NameLower, values.NameUpper, values.NameTitle = "acme-agent", "ACME_AGENT", "Acme Agent"
	values.BinaryName, values.ProjectDirName, values.GlobalDirName = "acme-agent", ".acme-agent", ".acme-agent"
	values.EnvPrefix = "ACME_AGENT"
	rendered, err := brandrender.RenderBytes(content, values)
	if err != nil {
		t.Fatal(err)
	}
	assertContains(t, string(rendered), "acme-agent get-tasks --field id,rejection_reason,rejection_rca --json")
	assertNoDefaultBrandLeaks(t, "rendered support reference", string(rendered))
}
