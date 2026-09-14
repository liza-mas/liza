package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Test 8 (W6): replanning an upstream retires selections that named it,
// including on a MERGED producer.
//
// MERGED is terminal and is also the status children are generated from, so a
// merged producer's output[] is live input to generation. The retarget loop
// above this pass skips terminal tasks deliberately — a terminal task's own
// DependsOn will not be claimed again — but applying that same filter here
// would leave a stale selection on exactly the producers that still generate
// children.
//
// The assertions below are deliberately honest about what degradation buys.
// It does not recover the prerequisite: the producer's DependsOn still names
// the replanned task, and a replanned task has no children by design. What it
// buys is that the persisted intent stops claiming a selection that can no
// longer be honored, and that the retirement is recorded. The unrecovered
// barrier is tracked in TECH_DEBT.md.
func TestReplan_RetiresSelectionsOnMergedProducer(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupReplanTest(t)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete

	upstream := buildMergedPlanningTask("code-planning-1", now)

	// The producer is MERGED — the case a terminal filter would skip — and
	// carries a selection naming the task about to be replanned.
	producer := buildMergedPlanningTask("code-planning-2", now)
	producer.DependsOn = []string{"code-planning-1"}
	producer.Output = []models.OutputEntry{
		{
			Desc: "selective child", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md",
			InheritInputs: &models.InheritInputs{
				Mode: models.InheritModeSelected,
				Selections: []models.InputSelection{
					{UpstreamTask: "code-planning-1", Outputs: []int{0}},
				},
			},
		},
		{
			Desc: "untouched child", DoneWhen: "tests pass", Scope: "docs/", SpecRef: "README.md",
			InheritInputs: &models.InheritInputs{
				Mode: models.InheritModeSelected,
				Selections: []models.InputSelection{
					{UpstreamTask: "some-other-plan", Outputs: []int{0}},
				},
			},
		},
	}

	state.Tasks = []models.Task{upstream, producer}
	state.Sprint.Scope.Planned = []string{"code-planning-1", "code-planning-2"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Replan(tmpDir, &ReplanInput{TaskID: "code-planning-1", ChangedBy: "human"})
	if err != nil {
		t.Fatalf("Replan() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	got := readState.FindTask("code-planning-2")
	if got == nil {
		t.Fatal("producer not found")
	}

	// Entry 0 named the replanned task: retired to the whole-phase barrier.
	retired := got.Output[0].InheritInputs
	if retired == nil || retired.Mode != models.InheritModeAll {
		t.Fatalf("output[0].inherit_inputs = %+v, want mode %q on a MERGED producer",
			retired, models.InheritModeAll)
	}
	if len(retired.Selections) != 0 {
		t.Errorf("retired entry kept selections %+v; indexes cannot survive a replan", retired.Selections)
	}

	// Entry 1 named a different upstream and must be untouched.
	untouched := got.Output[1].InheritInputs
	if untouched == nil || untouched.Mode != models.InheritModeSelected {
		t.Fatalf("output[1].inherit_inputs = %+v, want an unmodified selection", untouched)
	}
	if len(untouched.Selections) != 1 || untouched.Selections[0].UpstreamTask != "some-other-plan" {
		t.Errorf("output[1] selections = %+v; only selections naming the replanned task may be retired",
			untouched.Selections)
	}

	// The retirement is recorded rather than silent.
	var recorded bool
	for _, w := range result.Warnings {
		if strings.Contains(w, "code-planning-2 output[0]") && strings.Contains(w, "retired") {
			recorded = true
		}
	}
	if !recorded {
		t.Errorf("retirement not recorded in warnings: %v", result.Warnings)
	}
}

// A non-terminal producer is retired by the same pass. Without this the
// MERGED-only test could pass against an implementation that handled only the
// terminal case.
func TestReplan_RetiresSelectionsOnNonTerminalProducer(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupReplanTest(t)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete

	upstream := buildMergedPlanningTask("code-planning-1", now)

	producer := testhelpers.BuildTaskByStatus("code-planning-3", models.TaskStatusReady, now)
	producer.RolePair = "code-planning-pair"
	producer.DependsOn = []string{"code-planning-1"}
	producer.Output = []models.OutputEntry{{
		Desc: "selective child", DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md",
		InheritInputs: &models.InheritInputs{
			Mode: models.InheritModeSelected,
			Selections: []models.InputSelection{
				{UpstreamTask: "code-planning-1", Outputs: []int{0}},
			},
		},
	}}

	state.Tasks = []models.Task{upstream, producer}
	state.Sprint.Scope.Planned = []string{"code-planning-1", "code-planning-3"}
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := Replan(tmpDir, &ReplanInput{TaskID: "code-planning-1", ChangedBy: "human"}); err != nil {
		t.Fatalf("Replan() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	got := readState.FindTask("code-planning-3")
	if got.Output[0].InheritInputs.Mode != models.InheritModeAll {
		t.Errorf("non-terminal producer selection not retired: %+v", got.Output[0].InheritInputs)
	}

	// The non-terminal producer's DependsOn is retargeted onto the
	// replacement by the existing loop, so the whole-phase barrier it fell
	// back to is a real barrier against the new plan's children.
	if !slices.Contains(got.DependsOn, "code-planning-1-replan-1") {
		t.Errorf("DependsOn = %v, want retarget onto the replacement", got.DependsOn)
	}
}
