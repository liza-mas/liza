package ops

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

func TestValidationPreflightPreservesPreparationUntilContractChanges(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusImplementing)
	var request LifecycleRequest
	if err := f.bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		request = lifecycleTestRequest(t, task, "submit-for-review", "pending-validation", f.authority.Generation, nil)
		return PrepareLifecycleRequest(task, request)
	}); err != nil {
		t.Fatal(err)
	}
	before := f.state(t).FindTask("task-1")
	token := models.TaskTransitionID(before)
	var preflight *ValidationPreflight
	for check := 0; check < 2; check++ {
		var err error
		preflight, err = PrepareValidationPreflight(f.root, before.ID, f.authority.ID, "", f.session(true))
		if err != nil {
			t.Fatalf("readiness check %d: %v", check, err)
		}
		state := f.state(t)
		task := state.FindTask(before.ID)
		if !bytes.Equal(lifecycleTaskBytes(t, before), lifecycleTaskBytes(t, task)) || models.TaskTransitionID(task) != token {
			t.Fatal("passive readiness probe changed the task or its transition")
		}
		if err := ValidateLifecyclePreparation(task, request); err != nil {
			t.Fatalf("readiness probe invalidated live preparation: %v", err)
		}
		record := state.ValidationReadiness[f.authority.ID][before.ID]
		if record.Result != "passed" || record.CheckedAt.IsZero() {
			t.Fatal("preflight did not publish its passive readiness observation")
		}
		if err := f.bb.Modify(func(state *models.State) error {
			record := state.ValidationReadiness[f.authority.ID][before.ID]
			record.CheckedAt = record.CheckedAt.Add(time.Minute)
			state.ValidationReadiness[f.authority.ID][before.ID] = record
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.bb.Modify(func(state *models.State) error {
		state.FindTask(before.ID).ValidationPrerequisites[0].Env[0] = "NEW_VALIDATION_REQUIREMENT"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changed := f.state(t)
	if models.TaskTransitionID(changed.FindTask(before.ID)) == token {
		t.Fatal("changed prerequisite contract retained the old lifecycle boundary")
	}
	requireLifecycleError(t, ValidateLifecyclePreparation(changed.FindTask(before.ID), request), models.LifecycleStateChanged, "requery", "unknown")
	requireAssignmentPreflightError(t, preflight.CheckCurrent(changed))
}

func TestReleaseValidationOwnershipRetiresOnlyReleasedTaskPreparation(t *testing.T) {
	for _, scenario := range []string{"doer", "reviewer", "unowned task"} {
		t.Run(scenario, func(t *testing.T) {
			status, releasedStatus := models.TaskStatusImplementing, models.TaskStatusReady
			if scenario == "reviewer" {
				status, releasedStatus = models.TaskStatusReviewing, models.TaskStatusReadyForReview
			}
			f := newAssignmentPreflightFixture(t, status)
			releaseAuthority := f.authority
			if scenario != "doer" {
				releaseAuthority.ID = "code-reviewer-1"
			}
			markerAuthority := f.authority
			operation := "submit-for-review"
			if scenario == "reviewer" {
				markerAuthority = releaseAuthority
				operation = "submit-verdict"
			}
			var request LifecycleRequest
			if err := f.bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				agent := state.Agents[releaseAuthority.ID]
				agent.CurrentTask = &task.ID
				state.Agents[releaseAuthority.ID] = agent
				var err error
				request, err = NewLifecycleRequest(operation, task, markerAuthority.ID, &markerAuthority,
					LifecycleRequestOptions{RequestID: "pending-release", ExpectedTransition: models.TaskTransitionID(task)}, nil)
				if err != nil {
					return err
				}
				return PrepareLifecycleRequest(task, request)
			}); err != nil {
				t.Fatal(err)
			}
			before := f.state(t).FindTask("task-1")
			token := models.TaskTransitionID(before)
			if err := ReleaseValidationOwnership(f.root, before.ID, releaseAuthority.ID, &releaseAuthority); err != nil {
				t.Fatal(err)
			}
			afterState := f.state(t)
			after := afterState.FindTask(before.ID)
			if afterState.Agents[releaseAuthority.ID].CurrentTask != nil {
				t.Fatal("validation ownership release retained the agent's task mirror")
			}
			if scenario == "unowned task" {
				if !bytes.Equal(lifecycleTaskBytes(t, before), lifecycleTaskBytes(t, after)) || models.TaskTransitionID(after) != token {
					t.Fatal("agent bookkeeping cleanup changed another owner's task boundary")
				}
				if err := ValidateLifecyclePreparation(after, request); err != nil {
					t.Fatalf("bookkeeping cleanup retired live work: %v", err)
				}
				return
			}
			if after.Status != releasedStatus || after.Lifecycle.Preparation != nil || after.Lifecycle.Revision != before.Lifecycle.Revision+1 || models.TaskTransitionID(after) == token {
				t.Fatal("actual validation ownership release failed to retire the boundary")
			}
			if len(after.Lifecycle.Receipts) != 0 || after.Lifecycle.CompletionSequence != 0 || !reflect.DeepEqual(after.ValidationPrerequisites, before.ValidationPrerequisites) || !reflect.DeepEqual(after.Worktree, before.Worktree) {
				t.Fatal("ownership release invented completion or changed preserved work/contract")
			}
			requireLifecycleError(t, ValidateLifecyclePreparation(after, request), models.LifecycleStateChanged, "requery", "unknown")
			if err := ReleaseValidationOwnership(f.root, before.ID, releaseAuthority.ID, &releaseAuthority); err != nil {
				t.Fatal(err)
			}
			if repeated := f.state(t).FindTask(before.ID); !bytes.Equal(lifecycleTaskBytes(t, after), lifecycleTaskBytes(t, repeated)) {
				t.Fatal("already released ownership advanced lifecycle again")
			}
		})
	}
}
