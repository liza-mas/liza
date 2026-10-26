package commands

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/procscan"
)

func TestInspectFormatValuesKeepFormerSpelling(t *testing.T) {
	got := []string{string(inspectFormatJSON), string(inspectFormatYAML), string(inspectFormatTable), string(inspectFormatValue)}
	if want := []string{"json", "yaml", "table", "value"}; !slices.Equal(got, want) {
		t.Fatalf("inspect formats = %v, want %v", got, want)
	}
	// Validate accepted exactly these four spellings and the empty default.
	accepted := make([]string, len(inspectFormats))
	for i, format := range inspectFormats {
		accepted[i] = string(format)
	}
	if want := []string{"json", "yaml", "table", "value", ""}; !slices.Equal(accepted, want) {
		t.Fatalf("accepted formats = %v, want %v", accepted, want)
	}
	for _, format := range []string{"json", "yaml", "table", "value", ""} {
		opts := InspectOptions{Format: format}
		if err := opts.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want accepted", format, err)
		}
	}
	opts := InspectOptions{Format: "xml"}
	if err := opts.Validate(); err == nil || err.Error() != "invalid format: xml (must be json, yaml, table, or value)" {
		t.Errorf("Validate(xml) = %v, want the former invalid-format message", err)
	}
}

func TestInspectFormatStructuredIsJSONAndYAMLOnly(t *testing.T) {
	for format, want := range map[inspectFormat]bool{
		inspectFormatJSON: true, inspectFormatYAML: true,
		inspectFormatTable: false, inspectFormatValue: false, "": false,
	} {
		if got := format.isStructured(); got != want {
			t.Errorf("%q.isStructured() = %v, want %v", format, got, want)
		}
	}
}

func TestInspectTargetsKeepFormerSpelling(t *testing.T) {
	want := []string{"config", "sprint", "tasks", "agents", "metrics", "anomalies", "quarantined_verdicts", "proof_reaffirmations", "human_notes"}
	got := make([]string, len(inspectTargets))
	for i, target := range inspectTargets {
		got[i] = string(target)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("inspect targets = %v, want %v", got, want)
	}
	for _, name := range want {
		if !isInspectTarget(name) {
			t.Errorf("isInspectTarget(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "task", "agent", "version", "Tasks", "tasks.completion_rate"} {
		if isInspectTarget(name) {
			t.Errorf("isInspectTarget(%q) = true, want false", name)
		}
	}
}

func TestTruncateTableDescriptionKeepsFormerBounds(t *testing.T) {
	exact := strings.Repeat("a", 50)
	if got := truncateTableDescription(exact); got != exact {
		t.Errorf("50-byte description changed to %q", got)
	}
	long := strings.Repeat("b", 51)
	if got, want := truncateTableDescription(long), strings.Repeat("b", 47)+"..."; got != want {
		t.Errorf("51-byte description = %q, want %q", got, want)
	}

	table := formatTasksTable([]taskInfo{{ID: "t-1", Description: long}})
	if !strings.Contains(table, strings.Repeat("b", 47)+"...") || strings.Contains(table, strings.Repeat("b", 48)) {
		t.Errorf("task table did not truncate the description to 47 bytes plus an ellipsis: %s", table)
	}
	outputs := formatTaskOutputSummariesTable([]taskOutputSummaryInfo{{
		ID:     "t-1",
		Output: []outputEntrySummaryInfo{{Index: 0, Desc: long}},
	}})
	if !strings.Contains(outputs, strings.Repeat("b", 47)+"...") || strings.Contains(outputs, strings.Repeat("b", 48)) {
		t.Errorf("output-summary table did not truncate the description: %s", outputs)
	}
}

func TestStatusFormatValuesKeepFormerSpelling(t *testing.T) {
	if statusFormatJSON != "json" || statusFormatYAML != "yaml" {
		t.Fatalf("status formats = %q, %q; want json, yaml", statusFormatJSON, statusFormatYAML)
	}
}

func TestRenderStatusSelectsFormat(t *testing.T) {
	status := statusData{Goal: goalStatus{Description: "ship it"}}

	rendered, err := renderStatus(status, statusFormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(rendered), &decoded); err != nil {
		t.Fatalf("json format is not JSON: %v", err)
	}

	yamlOut, err := renderStatus(status, statusFormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(strings.TrimSpace(yamlOut), "{") || !strings.Contains(yamlOut, "ship it") {
		t.Errorf("yaml format = %q, want YAML carrying the goal", yamlOut)
	}

	dashboard, err := renderStatus(status, "dashboard")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := renderStatus(status, "")
	if err != nil {
		t.Fatal(err)
	}
	if dashboard != empty || dashboard == rendered || dashboard == yamlOut {
		t.Errorf("dashboard and empty formats must render the same dashboard, distinct from json and yaml")
	}
}

func TestPhaseHandoffStatesKeepFormerSpelling(t *testing.T) {
	got := []string{
		string(phaseHandoffReady), string(phaseHandoffPartialReady), string(phaseHandoffCheckpointed),
		string(phaseHandoffCompleted), string(phaseHandoffDispositionRequired),
		string(phaseHandoffRepairRequired), string(phaseHandoffMergeRequired),
	}
	want := []string{"READY", "PARTIAL_READY", "CHECKPOINTED", "COMPLETED", "DISPOSITION_REQUIRED", "REPAIR_REQUIRED", "MERGE_REQUIRED"}
	if !slices.Equal(got, want) {
		t.Fatalf("phase hand-off states = %v, want %v", got, want)
	}
	if phaseHandoffMissingTaskStatus != "MISSING" {
		t.Errorf("missing-task status = %q, want MISSING", phaseHandoffMissingTaskStatus)
	}
}

func TestProcessStatusRunningIsTheLiveProcessDisplayStatus(t *testing.T) {
	live := procscan.AgentProcessStatus{State: procscan.AgentProcessLiveMatching}
	if got := live.DisplayStatus(); got != processStatusRunning {
		t.Fatalf("live matching process displays %q, want %q", got, processStatusRunning)
	}
}

func TestClassifyPhaseHandoffPrecedenceAndExplanations(t *testing.T) {
	ready := []string{"plan-a", "plan-b"}
	blocker := []phaseHandoffTask{{ID: "work-1", Status: "IMPLEMENTING_CODE"}}
	failed := []ops.PlanHandoffFailure{{TaskID: "plan-a", Transition: "expand", Error: "boom"}}
	undecided := []ops.PlanHandoffUndecided{{TaskID: "plan-b"}}
	dispositionHint := fmt.Sprintf("%s or expand them with %s", brand.Command("add-human-note", "<task-id>", "--note-file", "<path>"), brand.Command("proceed", "<task-id>", "<transition>"))

	tests := []struct {
		name        string
		sprint      models.SprintStatus
		scan        plannedHandoffScan
		wantState   phaseHandoffState
		wantExplain string
	}{
		{
			name:        "ready",
			scan:        plannedHandoffScan{ready: ready},
			wantState:   "READY",
			wantExplain: "2 merged planning task(s) have unconsumed output and are ready for transition execution.",
		},
		{
			name:        "partial ready while planned work is active",
			scan:        plannedHandoffScan{ready: ready, blockers: blocker},
			wantState:   "PARTIAL_READY",
			wantExplain: fmt.Sprintf("2 merged planning task(s) have unconsumed output; 1 non-terminal planned task(s) are still active. %s can checkpoint PLANNING_COMPLETE and create implementation tasks after resume without waiting for the active tasks to finish.", brand.NameTitle),
		},
		{
			name:        "checkpoint overrides active work",
			sprint:      models.SprintStatusCheckpoint,
			scan:        plannedHandoffScan{ready: ready, blockers: blocker},
			wantState:   "CHECKPOINTED",
			wantExplain: "2 merged planning task(s) are waiting behind a checkpoint; resume the sprint to execute their pipeline transitions.",
		},
		{
			name:        "completed sprint",
			sprint:      models.SprintStatusCompleted,
			scan:        plannedHandoffScan{ready: ready},
			wantState:   "COMPLETED",
			wantExplain: "2 merged planning task(s) are waiting in a completed sprint; resume/advance to execute their pipeline transitions.",
		},
		{
			name:        "undecided overrides sprint status",
			sprint:      models.SprintStatusCompleted,
			scan:        plannedHandoffScan{ready: ready[:1], undecided: undecided},
			wantState:   "DISPOSITION_REQUIRED",
			wantExplain: "1 plan(s) were left undecided by the orchestrator and will not re-wake it until their inputs change; ask for a disposition with " + dispositionHint + ". 1 other plan(s) are ready.",
		},
		{
			name:        "failed overrides undecided",
			sprint:      models.SprintStatusCompleted,
			scan:        plannedHandoffScan{ready: ready[:1], failed: failed, undecided: undecided},
			wantState:   "REPAIR_REQUIRED",
			wantExplain: "1 planning hand-off(s) failed with unchanged inputs; repair inputs or explicitly retire unused hand-offs by a merged correction. 1 plan(s) await a disposition; 1 other plan(s) are ready.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{Sprint: models.Sprint{Status: tt.sprint}}
			got := classifyPhaseHandoff(state, tt.scan)
			if got.State != tt.wantState || got.Explanation != tt.wantExplain {
				t.Fatalf("classifyPhaseHandoff() = %q / %q\nwant %q / %q", got.State, got.Explanation, tt.wantState, tt.wantExplain)
			}
			if !slices.Equal(got.ReadyPlanningTasks, tt.scan.ready) || len(got.BlockingTasks) != len(tt.scan.blockers) {
				t.Errorf("classifyPhaseHandoff() dropped scan results: %+v", got)
			}
			if len(got.MergeRequired) != 0 {
				t.Errorf("classifyPhaseHandoff() reported merge-required entries: %+v", got.MergeRequired)
			}
		})
	}
}

func TestMergeRequiredHandoffListsEveryApprovedPlanInOrder(t *testing.T) {
	scan := plannedHandoffScan{ready: []string{"plan-c"}, blockers: []phaseHandoffTask{{ID: "work-1"}}}
	got := mergeRequiredHandoff(scan, []string{"plan-b", "plan-a"})
	if got.State != "MERGE_REQUIRED" || got.Explanation != "2 approved planning task(s) must be merged before the completed sprint can advance." {
		t.Fatalf("mergeRequiredHandoff() = %q / %q", got.State, got.Explanation)
	}
	want := []phaseMergeRequired{
		{TaskID: "plan-b", Action: brand.Command("wt-merge", "plan-b")},
		{TaskID: "plan-a", Action: brand.Command("wt-merge", "plan-a")},
	}
	if !slices.Equal(got.MergeRequired, want) {
		t.Errorf("merge required = %+v, want %+v", got.MergeRequired, want)
	}
	if !slices.Equal(got.ReadyPlanningTasks, scan.ready) || len(got.BlockingTasks) != 1 {
		t.Errorf("mergeRequiredHandoff() dropped scan results: %+v", got)
	}
	if len(got.UndecidedPlanningTasks) != 0 {
		t.Errorf("mergeRequiredHandoff() carried undecided plans: %+v", got.UndecidedPlanningTasks)
	}
}

func TestOrchestratorStatusReasonsKeepFormerTriggerSpellings(t *testing.T) {
	tests := []struct {
		trigger agent.OrchestratorWakeTrigger
		count   int
		reason  string
	}{
		{agent.WakeTriggerInitialPlanning, 0, "No tasks exist; initial planning needed"},
		{agent.WakeTriggerBlocked, 2, "2 task(s) are blocked and need attention"},
		{wakeTriggerIntegrationFailed, 3, "3 task(s) failed integration"},
		{agent.WakeTriggerHypothesisExhausted, 1, "1 task(s) exhausted hypotheses (2+ failures)"},
		{agent.WakeTriggerImmediateDiscovery, 4, "4 immediate discovery(ies) need to be converted to tasks"},
		{agent.WakeTriggerHumanNote, 2, "2 operator note(s) not yet rendered in a completed turn"},
		{agent.WakeTriggerPlanningComplete, 1, "1 planning task(s) merged with output[]; ready for coding task expansion"},
		{agent.WakeTriggerManyToOneReady, 1, "1 many-to-one cohort(s) ready for consolidation transition"},
		{agent.WakeTriggerSprintComplete, 5, "All 5 planned task(s) reached terminal state; sprint complete"},
		{agent.WakeTriggerNone, 0, "No triggers; orchestrator is idle"},
		{"SOMETHING_ELSE", 0, "Unknown trigger"},
	}
	spelled := map[agent.OrchestratorWakeTrigger]string{
		agent.WakeTriggerInitialPlanning: "INITIAL_PLANNING", agent.WakeTriggerBlocked: "BLOCKED_TASKS",
		wakeTriggerIntegrationFailed:         "INTEGRATION_FAILED",
		agent.WakeTriggerHypothesisExhausted: "HYPOTHESIS_EXHAUSTED", agent.WakeTriggerImmediateDiscovery: "IMMEDIATE_DISCOVERY",
		agent.WakeTriggerHumanNote: "HUMAN_NOTE", agent.WakeTriggerPlanningComplete: "PLANNING_COMPLETE",
		agent.WakeTriggerManyToOneReady: "MANY_TO_ONE_READY", agent.WakeTriggerSprintComplete: "SPRINT_COMPLETE",
		agent.WakeTriggerNone: "NONE",
	}
	for _, tt := range tests {
		t.Run(string(tt.trigger), func(t *testing.T) {
			if want, ok := spelled[tt.trigger]; ok && string(tt.trigger) != want {
				t.Fatalf("trigger spelled %q, want %q", tt.trigger, want)
			}
			got := buildOrchestratorStatusFromWakeResult(&models.State{}, agent.OrchestratorWakeResult{Trigger: tt.trigger, Count: tt.count})
			if got.Trigger != string(tt.trigger) || got.TriggerCount != tt.count || got.Reason != tt.reason {
				t.Fatalf("status = %+v, want trigger %q count %d reason %q", got, tt.trigger, tt.count, tt.reason)
			}
		})
	}
}

func TestIsStaleAssignmentRequiresLiveClaimWithoutRunningProcess(t *testing.T) {
	current := "work-1"
	other := "work-2"
	assigned := "coder-1"
	task := &models.Task{ID: current, AssignedTo: &assigned}
	tests := []struct {
		name   string
		agents map[string]models.Agent
		status string
		want   bool
	}{
		{"stopped process on its current task", map[string]models.Agent{assigned: {Status: models.AgentStatusWorking, CurrentTask: &current}}, "stopped", true},
		{"running process", map[string]models.Agent{assigned: {Status: models.AgentStatusWorking, CurrentTask: &current}}, processStatusRunning, false},
		{"idle agent", map[string]models.Agent{assigned: {Status: models.AgentStatusIdle, CurrentTask: &current}}, "stopped", false},
		{"agent works on another task", map[string]models.Agent{assigned: {Status: models.AgentStatusWorking, CurrentTask: &other}}, "stopped", false},
		{"agent has no current task", map[string]models.Agent{assigned: {Status: models.AgentStatusWorking}}, "stopped", false},
		{"agent absent from state", map[string]models.Agent{}, "stopped", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &models.State{Agents: tt.agents}
			got := isStaleAssignment(state, task, phaseHandoffTask{AgentProcessStatus: tt.status})
			if got != tt.want {
				t.Fatalf("isStaleAssignment() = %v, want %v", got, tt.want)
			}
		})
	}
}
