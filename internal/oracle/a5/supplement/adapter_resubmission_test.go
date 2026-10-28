package supplement

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
)

func init() { adapters["await-resubmission-change"] = awaitResubmissionChangeStep }

// awaitResubmissionBudget is the wait the reviewer asks for. The case ends the
// wait itself, so the budget only bounds a case that failed to.
const awaitResubmissionBudget = time.Minute

// awaitResubmissionStepTimeout bounds each event the adapter waits for.
const awaitResubmissionStepTimeout = 30 * time.Second

// awaitResubmissionChange is the state change that ends the reviewer's wait.
type awaitResubmissionChange struct {
	// Change is "terminal" (the task is abandoned) or "paused" (the system halts).
	Change string `json:"change"`
}

type awaitResubmissionOutcome struct {
	result *ops.AwaitResubmissionResult
	err    error
}

// awaitResubmissionChangeStep runs the exported await-resubmission operation as
// the reviewer, waits until the reviewer's reservation is on the task, applies
// the case's state change and records the result. It waits on state-file change
// events, never on a clock, and starts the change only after the reservation is
// observable, so the outcome does not depend on timing.
func awaitResubmissionChangeStep(t *testing.T, f *fixture, s step) {
	t.Helper()
	var p awaitResubmissionChange
	if err := json.Unmarshal(s.Params, &p); err != nil {
		t.Fatal(err)
	}
	bb := db.For(paths.New(f.project).StatePath())
	watcher, err := bb.WatchForChanges()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()

	authority := models.AgentAuthority{ID: s.Agent, Generation: "generation-1"}
	done := make(chan awaitResubmissionOutcome, 1)
	go func() {
		result, err := ops.AwaitResubmissionWithAuthorityOptions(context.Background(), f.project, "task-1", authority, awaitResubmissionBudget, ops.AwaitResubmissionOptions{})
		done <- awaitResubmissionOutcome{result, err}
	}()

	reserved := func() bool {
		state, err := bb.Read()
		if err != nil {
			t.Fatal(err)
		}
		task := state.FindTask("task-1")
		return task != nil && task.ReviewingBy != nil && *task.ReviewingBy == s.Agent
	}
	deadline := time.After(awaitResubmissionStepTimeout)
	for !reserved() {
		select {
		case <-watcher.Events():
		case out := <-done:
			t.Fatalf("the wait ended before the reservation was observable: %v %v", out.result, out.err)
		case <-deadline:
			t.Fatal("the reviewer's reservation did not appear")
		}
	}

	if err := bb.Modify(func(state *models.State) error {
		switch p.Change {
		case "terminal":
			state.FindTask("task-1").Status = models.TaskStatusAbandoned
		case "paused":
			state.Config.Mode = models.SystemModePaused
		default:
			return fmt.Errorf("unknown change %q", p.Change)
		}
		return nil
	}); err != nil {
		t.Fatalf("apply %s change: %v", p.Change, err)
	}

	select {
	case out := <-done:
		f.result = awaitResubmissionObservation(out)
	case <-time.After(awaitResubmissionStepTimeout):
		t.Fatal("the wait did not end after the state change")
	}
}

// awaitResubmissionObservation renders every field of the result, so a changed
// verdict, status, reason, commit, cycle or safe action fails the comparison.
func awaitResubmissionObservation(out awaitResubmissionOutcome) []byte {
	if out.err != nil {
		return fmt.Appendf(nil, "error: %v\n", out.err)
	}
	r := out.result
	return fmt.Appendf(nil, "verdict: %s\ntask_status: %s\nreason: %s\nbase_commit: %s\nreview_commit: %s\nreview_cycle: %d\nsafe_action: %s\n",
		r.Verdict, r.TaskStatus, r.Reason, r.BaseCommit, r.ReviewCommit, r.ReviewCycle, r.SafeAction)
}
