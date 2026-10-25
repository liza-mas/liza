package ops

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestPlanAmendmentCorrectionIDSuffixes(t *testing.T) {
	t.Parallel()
	if amendCorrectionIDSuffix != "amend" || replanCorrectionIDSuffix != "replan" {
		t.Fatalf("correction ID suffixes changed: %q %q", amendCorrectionIDSuffix, replanCorrectionIDSuffix)
	}
	state := &models.State{Tasks: []models.Task{{ID: "p-amend-1"}, {ID: "p-amend-2"}, {ID: "p-replan-1"}}}
	for suffix, want := range map[string]string{"amend": "p-amend-3", "replan": "p-replan-2"} {
		if got := nextCorrectionID(state, "p", suffix); got != want {
			t.Fatalf("%s correction ID = %s, want %s", suffix, got, want)
		}
	}
	original := &models.Task{ID: "p", Type: models.TaskTypePlanning, RolePair: "code-planning-pair"}
	state = &models.State{Tasks: []models.Task{*original}}
	plain := newAmendmentCorrection(state, original, "", AmendPlanInput{Reason: " why "}, models.TaskStatusReady)
	if plain.ID != "p-amend-1" || plain.PlanningChange == nil || plain.PlanningChange.Kind != models.PlanningChangeCorrection {
		t.Fatalf("plain correction %s: %+v", plain.ID, plain.PlanningChange)
	}
	preserve := newAmendmentCorrection(state, original, models.PlanAmendmentPreserveIdentity, AmendPlanInput{Reason: "why"}, models.TaskStatusReady)
	if preserve.ID != "p-replan-1" || preserve.PlanningChange == nil || preserve.PlanningChange.Kind != models.PlanningChangeReplan {
		t.Fatalf("preserve correction %s: %+v", preserve.ID, preserve.PlanningChange)
	}
	if !strings.Contains(plain.Description, "amendment of p: why.") || !strings.Contains(preserve.Description, "Mode preserve-output-identity") {
		t.Fatalf("descriptions lost their mode text: %q %q", plain.Description, preserve.Description)
	}
}

func TestPlanAmendmentModeFollowsReplacedPending(t *testing.T) {
	t.Parallel()
	state := &models.State{Tasks: []models.Task{{ID: "old", AmendmentMode: models.PlanAmendmentContract}}}
	for _, c := range []struct {
		name  string
		input AmendPlanInput
		want  models.PlanAmendmentMode
		fail  bool
	}{
		{"inherits", AmendPlanInput{ReplacePending: "old"}, models.PlanAmendmentContract, false},
		{"mismatch", AmendPlanInput{ReplacePending: "old", Mode: models.PlanAmendmentPreserveIdentity}, "", true},
		{"unknown pending keeps request", AmendPlanInput{ReplacePending: "gone", Mode: models.PlanAmendmentPreserveIdentity}, models.PlanAmendmentPreserveIdentity, false},
		{"begin keeps request", AmendPlanInput{Mode: models.PlanAmendmentContract}, models.PlanAmendmentContract, false},
	} {
		got, err := resolveAmendmentMode(state, c.input)
		if c.fail != (err != nil) || got != c.want {
			t.Fatalf("%s: mode %q err %v", c.name, got, err)
		}
		if c.fail && !strings.Contains(err.Error(), "replacement must preserve pending amendment mode") {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
}

func TestPlanAmendmentBeginEligibility(t *testing.T) {
	t.Parallel()
	held := &models.PlanCheck{Verdict: models.PlanCheckHeld}
	build := func(status models.TaskStatus) (*models.State, *models.Task) {
		state := &models.State{Tasks: []models.Task{
			{ID: "p", PlanAmendment: &models.PlanAmendment{Pending: "p-amend-1", Corrections: []string{"p-amend-1"}}},
			{ID: "p-amend-1", AmendsPlan: "p", Status: status},
		}}
		return state, state.FindTask("p")
	}
	state, original := build(models.TaskStatusMerged)
	for reason, setup := range map[string]func(){
		"human hold requires operator plan-check --clear": func() { original.PlanCheck = held; original.PlanAmendment.Pending = "" },
		"a correction is already pending":                 func() { original.PlanCheck = nil },
	} {
		setup()
		err := checkAmendmentBeginEligibility(state, original, "", "op")
		if err == nil || !strings.Contains(err.Error(), reason) {
			t.Fatalf("want %q, got %v", reason, err)
		}
		original.PlanAmendment.Pending = "p-amend-1"
	}
	if err := checkAmendmentBeginEligibility(state, original, "other", "op"); err == nil || !strings.Contains(err.Error(), "replace-pending must name the exact pending correction") {
		t.Fatalf("inexact replace-pending: %v", err)
	}
	if err := checkAmendmentBeginEligibility(state, original, "p-amend-1", "op"); err != nil {
		t.Fatal(err)
	}
	if old := state.FindTask("p-amend-1"); old.PlanCheck == nil || old.PlanCheck.Verdict != models.PlanCheckReplaced || old.PlanCheck.ReplacedBy != "p" || !slices.Equal(original.PlanAmendment.Quarantined, []string{"p-amend-1"}) {
		t.Fatalf("merged correction not marked replaced and quarantined: %+v %v", old.PlanCheck, original.PlanAmendment.Quarantined)
	}
	state, original = build(models.TaskStatusAbandoned)
	if err := checkAmendmentBeginEligibility(state, original, "p-amend-1", "op"); err != nil {
		t.Fatal(err)
	}
	if old := state.FindTask("p-amend-1"); old.PlanCheck != nil || !slices.Equal(original.PlanAmendment.Quarantined, []string{"p-amend-1"}) {
		t.Fatalf("abandoned correction must be quarantined without a plan check: %+v", old.PlanCheck)
	}
}

func TestPlanAmendmentAdoptionKeepsHoldAndResumesCheckpoint(t *testing.T) {
	t.Parallel()
	for _, held := range []bool{false, true} {
		original := models.Task{ID: "p", PlanAmendment: &models.PlanAmendment{}, PlanCheck: &models.PlanCheck{Verdict: models.PlanCheckPassed}}
		if held {
			original.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld}
		}
		state := &models.State{Tasks: []models.Task{original}}
		state.Sprint.Status, state.Sprint.CheckpointTrigger = models.SprintStatusCheckpoint, "PLANNING_COMPLETE"
		adoptAmendmentCorrection(state, state.FindTask("p"), models.Task{ID: "p-amend-1"}, "op")
		got := state.FindTask("p")
		if got.PlanAmendment.Pending != "p-amend-1" || !slices.Equal(got.PlanAmendment.Corrections, []string{"p-amend-1"}) || !slices.Equal(state.Sprint.Scope.Planned, []string{"p-amend-1"}) {
			t.Fatalf("fence not recorded: %+v planned %v", got.PlanAmendment, state.Sprint.Scope.Planned)
		}
		if held != (got.PlanCheckVerdictOf() == models.PlanCheckHeld) || !held && got.PlanCheck != nil {
			t.Fatalf("hold=%v: plan check after adoption %+v", held, got.PlanCheck)
		}
		if state.Sprint.Status != models.SprintStatusInProgress || state.Sprint.CheckpointTrigger != "" || state.FindTask("p-amend-1") == nil {
			t.Fatalf("checkpoint not resumed or correction not scheduled: %+v", state.Sprint)
		}
	}
}

func TestBoundedContractAllowedSections(t *testing.T) {
	t.Parallel()
	original := &models.Task{ArchRef: "specs/a.md#Scope 1", PlanRef: "specs/p.md#Task 1", Output: []models.OutputEntry{{ArchRef: "specs/a.md#Scope 2", PlanRef: "specs/p.md#Task 2"}, {}}}
	for _, c := range []struct {
		name         string
		mode         models.PlanAmendmentMode
		architecture bool
		want         map[string][]string
	}{
		{"contract mode edits architecture only", models.PlanAmendmentContract, false, map[string][]string{"specs/a.md": {"Scope 1", "Scope 2"}}},
		{"preserve-identity edits plan sections of a planning original", models.PlanAmendmentPreserveIdentity, false, map[string][]string{"specs/a.md": {"Scope 1", "Scope 2"}, "specs/p.md": {"Task 1", "Task 2"}}},
		{"preserve-identity of an architecture original edits architecture only", models.PlanAmendmentPreserveIdentity, true, map[string][]string{"specs/a.md": {"Scope 1", "Scope 2"}}},
	} {
		if got := allowedContractSections(original, c.mode, c.architecture); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
