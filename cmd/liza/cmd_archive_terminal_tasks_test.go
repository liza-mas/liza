package main

import (
	"bytes"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestTerminalArchiveCLIDrainsEvidenceAndPreservesLogicalTasks(t *testing.T) {
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root, statePath := setupArchiveCLIProject(t, "done-1", "done-2")
	bb := db.For(statePath)
	if err := bb.Modify(func(state *models.State) error {
		for i := range state.Tasks {
			for event := 0; event < 60; event++ {
				reason := strings.Repeat("terminal-archive-evidence ", 50)
				state.Tasks[i].History = append(state.Tasks[i].History, models.TaskHistoryEntry{
					Time: time.Date(2026, 9, 24, 11, 0, event, 0, time.UTC), Event: models.TaskEventPreExecutionCheckpoint, Reason: &reason,
				})
			}
		}
		state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("active", models.TaskStatusReady, time.Now().UTC()))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	beforeBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	stdout, err := executeRootCommandCapture(t, root, "archive-terminal-tasks", "--max-tasks", "1", "--json")
	if err != nil {
		t.Fatalf("terminal maintenance command did not archive the backlog: %v", err)
	}
	envelope := parseEnvelope(t, stdout)
	result, ok := envelope["result"].(map[string]any)
	if !ok || envelope["ok"] != true {
		t.Fatal("terminal maintenance did not return a successful structured result")
	}
	archived, ok := result["archived"].([]any)
	if !ok || len(archived) != 2 || result["batches"] != float64(2) {
		t.Fatalf("maintenance did not drain two one-task batches: archived=%v batches=%v", result["archived"], result["batches"])
	}
	afterBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(afterBytes, []byte("terminal-archive-evidence")) || len(afterBytes) >= len(beforeBytes)/4 {
		t.Fatalf("terminal evidence remained in hot YAML: before=%d after=%d", len(beforeBytes), len(afterBytes))
	}
	// A fresh DB exercises cold object decoding rather than relying on the
	// maintenance process's warmed decoded-object cache.
	after, err := db.New(statePath).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"done-1", "done-2", "active"} {
		prior, got := before.FindTask(id), after.FindTask(id)
		if got == nil || models.TaskTransitionID(got) != models.TaskTransitionID(prior) ||
			!reflect.DeepEqual(got.History, prior.History) || !reflect.DeepEqual(got.Output, prior.Output) ||
			!reflect.DeepEqual(got.Lifecycle, prior.Lifecycle) {
			t.Fatalf("archival changed logical evidence or transition identity for %s", id)
		}
	}
}

func TestTerminalArchiveCLIRequiresOperator(t *testing.T) {
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	// The branded and legacy names are identical in a default-brand build.
	t.Setenv(brand.EnvName("AGENT_ID"), "coder-1")
	root, statePath := setupArchiveCLIProject(t, "done")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = executeRootCommandCapture(t, root, "archive-terminal-tasks")
	if err == nil || !strings.Contains(err.Error(), "operator-only") {
		t.Fatalf("terminal maintenance from an agent did not receive operator-only refusal: %v", err)
	}
	after, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("refused maintenance changed state: %v", err)
	}
}
