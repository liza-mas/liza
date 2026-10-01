//go:build e2e

package integration

import (
	"io"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestTerminalDependencyRecovery(t *testing.T) {
	t.Run("supported supersession preserves replacement rewrites", func(t *testing.T) {
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

		now := time.Now().UTC()
		retiringPlan := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusBlocked, now)
		retiringPlan.RolePair = "code-planning-pair"
		retiringPlan.DependsOn = []string{"coding-a", "coding-b"}
		replacement := testhelpers.BuildTaskByStatus("plan-replacement", models.TaskStatusDraftCodingPlan, now)
		consumer := testhelpers.BuildTaskByStatus("coding-consumer", models.TaskStatusReady, now)
		consumer.DependsOn = []string{"plan-old"}

		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			retiringPlan,
			testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
			testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
			replacement,
			consumer,
		}
		setTerminalRecoverySpecRefs(state)
		blackboard := testhelpers.WriteInitialState(t, statePath, state)

		if _, err := ops.SupersedeTaskWithOptions(projectRoot, "plan-old", []string{"plan-replacement"}, "Replace blocked plan", "orchestrator-1", ops.SupersedeTaskOptions{Changed: "test: changed since block"}); err != nil {
			t.Fatalf("SupersedeTask() error: %v", err)
		}

		persisted, err := blackboard.Read()
		if err != nil {
			t.Fatalf("read superseded state: %v", err)
		}
		retired := persisted.FindTask("plan-old")
		if retired == nil || retired.Status != models.TaskStatusSuperseded || len(retired.DependsOn) != 0 {
			t.Fatalf("retired planning task = %#v, want terminal task without downstream dependencies", retired)
		}
		if !slices.Equal(retired.SupersededBy, []string{"plan-replacement"}) {
			t.Fatalf("superseded_by = %v, want [plan-replacement]", retired.SupersededBy)
		}
		updatedConsumer := persisted.FindTask("coding-consumer")
		if updatedConsumer == nil || !slices.Equal(updatedConsumer.DependsOn, []string{"plan-replacement"}) {
			t.Fatalf("coding consumer = %#v, want dependency rewritten to replacement", updatedConsumer)
		}
		assertTerminalRecoveryStateValid(t, persisted, projectRoot)
	})

	t.Run("audited repair restores corrupted terminal metadata", func(t *testing.T) {
		projectRoot := t.TempDir()
		statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
		testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")

		now := time.Now().UTC()
		corrupted := testhelpers.BuildTaskByStatus("plan-old", models.TaskStatusSuperseded, now)
		corrupted.RolePair = "code-planning-pair"
		corrupted.DependsOn = []string{"coding-a", "coding-b"}
		corrupted.SupersededBy = []string{"plan-replacement"}
		corrupted.RescopeReason = testhelpers.StringPtr("Replaced blocked plan")
		replacement := testhelpers.BuildTaskByStatus("plan-replacement", models.TaskStatusDraftCodingPlan, now)
		consumer := testhelpers.BuildTaskByStatus("coding-consumer", models.TaskStatusReady, now)
		consumer.DependsOn = []string{"plan-replacement"}

		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			corrupted,
			testhelpers.BuildTaskByStatus("coding-a", models.TaskStatusReady, now),
			testhelpers.BuildTaskByStatus("coding-b", models.TaskStatusReady, now),
			replacement,
			consumer,
		}
		setTerminalRecoverySpecRefs(state)
		blackboard := testhelpers.WriteInitialState(t, statePath, state)
		beforeReplacement := *state.FindTask("plan-replacement")
		beforeConsumer := *state.FindTask("coding-consumer")

		if _, err := ops.RepairSupersededDependencies(projectRoot, "plan-old", "Repair terminal dependency metadata", "orchestrator-1"); err != nil {
			t.Fatalf("RepairSupersededDependencies() error: %v", err)
		}

		persisted, err := blackboard.Read()
		if err != nil {
			t.Fatalf("read repaired state: %v", err)
		}
		repaired := persisted.FindTask("plan-old")
		if repaired == nil || repaired.Status != models.TaskStatusSuperseded || len(repaired.DependsOn) != 0 {
			t.Fatalf("repaired planning task = %#v, want terminal task without downstream dependencies", repaired)
		}
		if !slices.Equal(repaired.SupersededBy, []string{"plan-replacement"}) {
			t.Fatalf("superseded_by = %v, want [plan-replacement]", repaired.SupersededBy)
		}
		lastHistory := repaired.History[len(repaired.History)-1]
		if lastHistory.Event != models.TaskEventDependenciesRewritten || lastHistory.Extra["operation"] != "repair-superseded-dependencies" {
			t.Fatalf("repair audit history = %#v", lastHistory)
		}
		persistedReplacement := persisted.FindTask("plan-replacement")
		if persistedReplacement == nil || !reflect.DeepEqual(*persistedReplacement, beforeReplacement) {
			t.Fatal("repair changed replacement task")
		}
		persistedConsumer := persisted.FindTask("coding-consumer")
		if persistedConsumer == nil || !reflect.DeepEqual(*persistedConsumer, beforeConsumer) {
			t.Fatal("repair changed downstream consumer")
		}
		assertTerminalRecoveryStateValid(t, persisted, projectRoot)
	})
}

func setTerminalRecoverySpecRefs(state *models.State) {
	for i := range state.Tasks {
		state.Tasks[i].SpecRef = state.Goal.SpecRef
	}
}

func assertTerminalRecoveryStateValid(t *testing.T, state *models.State, projectRoot string) {
	t.Helper()
	if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
		t.Fatalf("persisted state validation failed: %v", err)
	}
}
