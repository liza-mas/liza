package commands

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func TestActiveInspectionIncludesPendingSelectedHandoffsAndCorrections(t *testing.T) {
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	plan := func(id string, direct bool) models.Task {
		return models.Task{ID: id, RolePair: "architecture-pair", Status: models.TaskStatusMerged,
			Output: []models.OutputEntry{{CodingAllocation: direct}}, TransitionsExecuted: map[string]bool{}}
	}
	undecided := plan("undecided", false)
	held := plan("held", false)
	held.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld}
	refused := plan("refused", true)
	refused.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed}
	refused.History = []models.TaskHistoryEntry{{Event: models.TaskEventTransitionFailed}}
	complete := plan("complete-direct", true)
	complete.TransitionsExecuted["architecture-to-coding"] = true
	legacyComplete := plan("complete-legacy", false)
	legacyComplete.TransitionsExecuted["architecture-to-code-plan"] = true
	retired := plan("retired", true)
	retired.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: "replacement"}
	replanned := plan("replanned", false)
	replanned.TransitionsExecuted["replanned"] = true
	expanded := plan("expanded-pending", true)
	expanded.TransitionsExecuted["architecture-to-coding"] = true
	expanded.PlanAmendment = &models.PlanAmendment{Pending: "pending-correction"}
	pending := plan("pending-correction", true)
	pending.AmendsPlan = expanded.ID
	applied := plan("applied-correction", true)
	applied.AmendsPlan = complete.ID
	quarantined := plan("quarantined-correction", true)
	quarantined.AmendsPlan = expanded.ID
	unexpanded := plan("unexpanded-pending", false)
	unexpanded.PlanAmendment = &models.PlanAmendment{Pending: "unmerged-correction"}
	state := &models.State{Tasks: []models.Task{undecided, held, refused, complete, legacyComplete, retired,
		replanned, expanded, pending, applied, quarantined, unexpanded,
		{ID: "ordinary", Status: models.TaskStatusBlocked},
		{ID: "done", Status: models.TaskStatusMerged}}}
	want := []string{"undecided", "held", "refused", "expanded-pending", "pending-correction", "unexpanded-pending", "ordinary"}
	for _, projection := range []string{"full", "summary", "output-summary", "fields"} {
		t.Run(projection, func(t *testing.T) {
			opts := inspectTasksOptions{Format: "json", Active: true, PipelineResolver: resolver,
				Summary: projection == "summary", OutputSummary: projection == "output-summary"}
			if projection == "fields" {
				opts.Fields = []string{"id"}
			}
			output, err := inspectTasks(state, opts)
			if err != nil {
				t.Fatal(err)
			}
			var rows []struct{ ID string }
			if err := json.Unmarshal([]byte(output.(string)), &rows); err != nil {
				t.Fatal(err)
			}
			ids := make([]string, 0, len(rows))
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			if !slices.Equal(ids, want) {
				t.Fatalf("visible tasks=%v, want %v", ids, want)
			}
			opts.Active = false
			output, err = inspectTasks(state, opts)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(output.(string)), &rows); err != nil || len(rows) != len(state.Tasks) {
				t.Fatalf("complete listing lost history: %v, %v", rows, err)
			}
		})
	}
	// These MERGED plans stay terminal to assignment and claim callers.
	for i := range state.Tasks {
		if state.Tasks[i].Status == models.TaskStatusMerged && !models.IsOperationallyTerminal(&state.Tasks[i], resolver) {
			t.Fatalf("inspection altered shared terminal semantics for %s", state.Tasks[i].ID)
		}
	}
}
