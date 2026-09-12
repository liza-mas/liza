package ops

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRetireFailedLifecyclePreparation(t *testing.T) {
	for _, scenario := range []string{
		"own marker", "replacement request", "moved task boundary",
		"missing current marker", "missing observed marker", "stale authority",
	} {
		t.Run(scenario, func(t *testing.T) {
			_, statePath, bb, authority := setupOwnershipLifecycleClaim(t)
			var preparation models.LifecyclePreparation
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				*task = testhelpers.BuildTaskByStatus(task.ID, models.TaskStatusImplementing, task.Created)
				request := lifecycleTestRequest(t, task, "submit-for-review", "failed-submission", authority.Generation, strings.Repeat("a", 40))
				if err := PrepareLifecycleRequest(task, request); err != nil {
					return err
				}
				preparation = *task.Lifecycle.Preparation
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			const replacementGeneration = "retirement-replacement-fixture"
			if scenario != "own marker" && scenario != "missing observed marker" {
				if err := bb.Modify(func(state *models.State) error {
					task := state.FindTask("task-1")
					switch scenario {
					case "replacement request":
						task.Lifecycle.Preparation.RequestID = "replacement-submission"
					case "moved task boundary":
						// A legacy writer can advance history without retiring metadata.
						task.History = append(task.History, models.TaskHistoryEntry{Time: task.Created, Event: "ownership_changed"})
					case "missing current marker":
						models.AdvanceLifecycle(task)
					case "stale authority":
						agent := state.Agents[authority.ID]
						agent.Generation = replacementGeneration
						state.Agents[authority.ID] = agent
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			before := ownershipStateBytes(t, statePath)
			beforeTask, err := bb.GetTask("task-1")
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("external validation failed")
			original := WrapLifecycleError("submit-for-review", beforeTask, cause, models.LifecycleStateChanged, "requery", "unknown")
			observed := &preparation
			if scenario == "missing observed marker" {
				observed = nil
			}
			err = retireFailedLifecyclePreparation(bb, "task-1", &authority, observed, original, "unknown")
			if !errors.Is(err, cause) {
				t.Fatal("retirement lost the original operation failure")
			}
			after := ownershipStateBytes(t, statePath)
			if scenario != "own marker" {
				if !bytes.Equal(before, after) {
					t.Fatal("cleanup changed a replacement, moved, absent or unauthorized preparation")
				}
				if scenario != "stale authority" {
					if err != original {
						t.Fatal("no-op cleanup replaced the original error")
					}
					return
				}
				requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "unknown")
				var authorityErr *AgentAuthorityError
				if !errors.As(err, &authorityErr) {
					t.Fatal("cleanup lost the authority rejection")
				}
				var lifecycleErr *LifecycleError
				if !errors.As(err, &lifecycleErr) {
					t.Fatal("cleanup lost its lifecycle policy")
				}
				outcome := lifecycleErr.Outcome
				if outcome.TaskStatus != "UNKNOWN" || outcome.TaskID != "" || outcome.TransitionID != "" || outcome.CurrentAssignee != "" || outcome.CurrentReviewer != "" || outcome.CompletedTransitionID != "" {
					t.Fatalf("stale cleanup advertised current task state: %+v", outcome)
				}
				details, marshalErr := json.Marshal(lifecycleErr.SafeDetails())
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				for _, generation := range []string{authority.Generation, replacementGeneration} {
					for _, forbidden := range []string{generation, fmt.Sprintf("%x", sha256.Sum256([]byte(generation)))} {
						if strings.Contains(err.Error(), forbidden) || strings.Contains(string(details), forbidden) {
							t.Fatal("cleanup exposed a registration generation or fingerprint")
						}
					}
				}
				return
			}

			requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
			afterTask, readErr := bb.GetTask("task-1")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if afterTask.Lifecycle.Preparation != nil || afterTask.Lifecycle.Revision != beforeTask.Lifecycle.Revision+1 || len(afterTask.Lifecycle.Receipts) != 0 || afterTask.Lifecycle.CompletionSequence != 0 {
				t.Fatal("own cleanup failed to retire its marker or invented completion")
			}
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) {
				t.Fatal("cleanup lost its lifecycle result")
			}
			outcome := lifecycleErr.Outcome
			if outcome.TransitionID != models.TaskTransitionID(afterTask) || outcome.TransitionID == preparation.Boundary || outcome.RequestID != preparation.RequestID || outcome.CompletedTransitionID != "" {
				t.Fatalf("cleanup did not publish its fresh non-completion boundary: %+v", outcome)
			}
			if outcome.TaskStatus != models.TaskStatusImplementing || outcome.CurrentAssignee != authority.ID || afterTask.Status != beforeTask.Status || len(afterTask.History) != len(beforeTask.History) {
				t.Fatal("cleanup changed or misreported domain progress")
			}
			requireLifecycleError(t, ValidateLifecyclePreparation(afterTask, preparation.LifecycleIdentity), models.LifecycleStateChanged, "requery", "unknown")
		})
	}
}
