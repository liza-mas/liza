package main

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestTaskListDefaultsToActiveAndAllRetainsHistory(t *testing.T) {
	root := setupPlanCheckCLI(t)
	statePath := paths.New(root).StatePath()
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	state.Tasks = append(state.Tasks, models.Task{ID: "terminal-history", Status: models.TaskStatusMerged, Created: time.Now().UTC()})
	testhelpers.WriteInitialState(t, statePath, state)
	for _, entry := range [][]string{{"get", "tasks"}, {"get-tasks"}} {
		for _, projection := range [][]string{{}, {"--summary"}, {"--output-summary"}, {"--field", "id"}} {
			args := append(append([]string{}, entry...), projection...)
			args = append(args, "--json")
			for _, selection := range [][]string{{}, {"--active"}, {"--all"}} {
				resetRootCmdForTest(t)
				output, err := executeRootCommandCapture(t, root, append(append([]string{}, args...), selection...)...)
				if err != nil {
					t.Fatalf("%v %v: %v (%s)", args, selection, err, output)
				}
				envelope := parseEnvelope(t, output)
				rows, ok := envelope["result"].([]any)
				if !ok || envelope["ok"] != true {
					t.Fatalf("invalid task envelope: %s", output)
				}
				want := 1
				if len(selection) > 0 && selection[0] == "--all" {
					want = 2
				}
				if len(rows) != want || rows[0].(map[string]any)["id"] != "plan-1" {
					t.Fatalf("%v %v tasks=%v, want count %d with pending plan", args, selection, rows, want)
				}
			}
		}
	}
	for _, query := range [][]string{{"get", "tasks", "terminal-history"}, {"get-tasks", "terminal-history"}, {"get", "terminal-history"}, {"get", "tasks.terminal-history.status"}} {
		resetRootCmdForTest(t)
		output, err := executeRootCommandCapture(t, root, append(query, "--json")...)
		if err != nil || !strings.Contains(output, "MERGED") {
			t.Fatalf("explicit terminal lookup %v: %v (%s)", query, err, output)
		}
	}
	resetRootCmdForTest(t)
	output, err := executeRootCommandCapture(t, root, "get", "tasks", "--all", "--active", "--json")
	if err == nil || !strings.Contains(output, "mutually exclusive") {
		t.Fatalf("contradictory selectors accepted: %v (%s)", err, output)
	}
}
