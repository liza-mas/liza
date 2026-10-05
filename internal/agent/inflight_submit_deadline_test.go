//go:build !windows

package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-38 (scaled): a doer that submits late in its session, whose quiet canonical
// validation outlasts both the remaining execution budget and the progress
// interval, must still publish before the supervisor ends the session.
func TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks(t *testing.T) {
	// GIVEN a quiet 4 s canonical command, a 2 s execution budget and a 1 s
	// progress timeout; the session spends 0.5 s before submitting
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "sleep 4\nprintf 'PASS identity assertion\\n'\n", 30)
	previous := executionDeadlineRecheckInterval
	executionDeadlineRecheckInterval = 100 * time.Millisecond
	t.Cleanup(func() { executionDeadlineRecheckInterval = previous })

	var (
		cancelledBeforeSubmit bool
		submitErr             error
		submitReturned        time.Time
	)
	mock := &MockLLMAgent{
		OnExecute: func(ctx context.Context, _ string, _ string, _ string, _ string, _ []string) error {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
				cancelledBeforeSubmit = true
				return ctx.Err()
			}
			submitted := make(chan error, 1)
			go func() {
				_, err := ops.SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
				submitted <- err
			}()
			select {
			case submitErr = <-submitted:
				submitReturned = time.Now()
			case <-ctx.Done():
				cancelledBeforeSubmit = true
				submitErr = <-submitted
				return ctx.Err()
			}
			// The agent is still writing its reply when the session ends.
			<-ctx.Done()
			return ctx.Err()
		},
	}
	state, err := scenario.BB.Read()
	if err != nil {
		t.Fatal(err)
	}
	config := SupervisorConfig{
		AgentID:                  scenario.AgentID,
		Authority:                scenario.Authority,
		Role:                     models.RoleCoder,
		ProjectRoot:              scenario.Root,
		StatePath:                scenario.StatePath,
		CLIName:                  "codex",
		LLMAgent:                 mock,
		ExecutionTimeout:         2 * time.Second,
		ExecutionProgressTimeout: time.Second,
	}

	// WHEN the supervisor runs the session
	exitCode, _, execErr := executeAgent(context.Background(), config, "prompt", nil, scenario.TaskID, state.Config)
	returned := time.Now()

	// THEN the session outlived the submit, which published at its candidate
	if cancelledBeforeSubmit {
		t.Fatalf("supervisor cancelled the session before submit returned (submit error: %v)", submitErr)
	}
	if submitErr != nil {
		t.Fatalf("submit error = %v, want publication", submitErr)
	}
	after, err := scenario.BB.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := after.FindTask(scenario.TaskID)
	if task.ReviewCommit == nil || task.AcceptanceReceipt == nil || task.AcceptanceReceipt.ReviewCommit != *task.ReviewCommit {
		t.Fatalf("task status=%s review_commit=%v receipt=%v, want a receipt at the review commit", task.Status, task.ReviewCommit, task.AcceptanceReceipt)
	}
	if last := task.History[len(task.History)-1]; last.Event != models.TaskEventSubmittedForReview {
		t.Fatalf("last task event = %s (status %s), want %s", last.Event, task.Status, models.TaskEventSubmittedForReview)
	}
	for _, event := range task.History {
		if event.Event == models.TaskEventClaimReleased || event.Event == models.TaskEventBlocked {
			t.Fatalf("task history has %s before or around publication: %+v", event.Event, event)
		}
	}
	// AND the session still ends promptly, as an execution timeout, once the
	// submit is over
	if execErr != nil || exitCode != 1 {
		t.Fatalf("executeAgent = (%d, %v), want execution timeout exit 1", exitCode, execErr)
	}
	if wait := returned.Sub(submitReturned); wait > time.Second {
		t.Fatalf("session ended %s after submit returned, want within 1s", wait)
	}
	entries, err := os.ReadDir(paths.New(scenario.Root).InflightSubmitDir())
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("in-flight submit markers left behind: %v", entries)
	}
}
