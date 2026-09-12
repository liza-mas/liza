package ops

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

func TestAwaitResubmissionRefusalPreservesLifecycleBoundaries(t *testing.T) {
	for _, completed := range []bool{false, true} {
		name := "temporary-ownership-only"
		if completed {
			name = "concurrent-completion"
		}
		t.Run(name, func(t *testing.T) {
			root, taskID, commit, doer, bb := completeAcceptanceScenario(t)
			if _, err := SubmitForReview(root, taskID, commit, doer); err != nil {
				t.Fatal(err)
			}
			reviewer := "code-reviewer-1"
			registerAcceptanceReviewer(t, bb, reviewer)
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask(taskID)
				task.Status = models.TaskStatusRejected
				task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &reviewer})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readAcceptanceState(t, bb)
			beforeToken := models.TaskTransitionID(before.FindTask(taskID))
			var waitingToken string
			var completedRequest LifecycleRequest
			var completedReceipt models.LifecycleReceipt
			previousWatcher := newAwaitResubmissionWatcher
			t.Cleanup(func() { newAwaitResubmissionWatcher = previousWatcher })
			newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
				if err := bb.Modify(func(state *models.State) error {
					task := state.FindTask(taskID)
					waitingToken = models.TaskTransitionID(task)
					var err error
					completedRequest, err = NewLifecycleRequest("submit-for-review", task, doer, nil,
						LifecycleRequestOptions{RequestID: "resubmission", ExpectedTransition: waitingToken}, struct{}{})
					if err != nil {
						return err
					}
					task.Status = models.TaskStatusReadyForReview
					task.AcceptanceReceipt = nil
					if completed {
						if _, err := CompleteLifecycleRequest(task, completedRequest, models.LifecycleProjection{ReviewCommit: commit}); err != nil {
							return err
						}
						completedReceipt = task.Lifecycle.Receipts[len(task.Lifecycle.Receipts)-1]
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				return nil, errors.New("test polling fallback")
			}
			_, err := AwaitResubmissionWithOptions(context.Background(), root, taskID, reviewer, time.Second,
				AwaitResubmissionOptions{FallbackPollInterval: time.Millisecond})
			requireAcceptanceError(t, err, taskID)
			after := readAcceptanceState(t, bb)
			task := after.FindTask(taskID)
			if task.ReviewingBy != nil || task.ReviewLeaseExpires != nil ||
				!reflect.DeepEqual(before.Agents, after.Agents) {
				t.Fatal("refusal retained temporary review ownership")
			}
			if completed {
				receipt, err := CheckLifecycleRequest(task, completedRequest)
				if err != nil || receipt == nil || !reflect.DeepEqual(*receipt, completedReceipt) {
					t.Fatalf("refusal lost or changed concurrent completion: receipt=%+v err=%v", receipt, err)
				}
			} else if !reflect.DeepEqual(task.Lifecycle, before.FindTask(taskID).Lifecycle) {
				t.Fatal("refusal retained its temporary lifecycle mutation")
			}
			for _, token := range []string{beforeToken, waitingToken} {
				request, err := NewLifecycleRequest("mark-blocked", task, reviewer, nil,
					LifecycleRequestOptions{RequestID: "obsolete-wait", ExpectedTransition: token}, struct{}{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := CheckLifecycleRequest(task, request); err == nil {
					t.Fatal("refusal revived an obsolete transition token")
				}
			}
		})
	}
}
