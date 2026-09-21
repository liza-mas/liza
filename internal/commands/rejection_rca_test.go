package commands

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const rejectionRCACommandTask = "task-gated"

// newRejectionRCACommandProject writes a BLOCKED task whose rejection-RCA gate
// fired at threshold 4, plus a registered orchestrator, and returns the
// project root, the state path and the orchestrator's authority.
func newRejectionRCACommandProject(t *testing.T) (string, string, models.AgentAuthority) {
	t.Helper()
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	now := time.Now().UTC().Truncate(time.Second)

	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(rejectionRCACommandTask, models.TaskStatusBlocked, now)
	task.AssignedTo = nil
	task.SpecRef = state.Goal.SpecRef
	reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
	task.BlockedReason = &reason
	task.ReviewCyclesCurrent = 4
	task.ReviewCyclesTotal = 4
	task.RejectionRCA = &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      4,
		RejectionCount: 4,
		GatedAt:        now.Add(-time.Minute),
		GatingCommit:   "0123456789abcdef0123456789abcdef01234567",
	}
	state.Tasks = []models.Task{task}
	orchestrator := testhelpers.RegisteredTestAgent("orchestrator")
	orchestrator.Generation = testhelpers.TestAgentGeneration
	state.Agents["orchestrator-1"] = orchestrator
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot, statePath, models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
}

func rejectionRCARequestPayload() map[string]any {
	return map[string]any{
		"schema_version": float64(models.RejectionRCASchemaVersion),
		"summary":        "One product defect and one reviewer unable to run the database.",
		"contributions": []any{
			map[string]any{"rejection_index": float64(1), "categories": []any{models.RejectionCauseProductDefect}, "evidence": []any{"verdict 1: scalar mismatch"}},
			map[string]any{"rejection_index": float64(2), "categories": []any{models.RejectionCauseCapabilityFailure}, "evidence": []any{"verdict 2: database unavailable"}},
		},
	}
}

func rejectionRCADispositionPayload() map[string]any {
	return map[string]any{
		"schema_version": float64(models.RejectionRCASchemaVersion),
		"recovery_path":  models.RecoveryCapabilityReroute,
		"rationale":      "Reroute validation to a session that can run the database.",
	}
}

// captureRejectionRCAStdout runs one command adapter and returns its stdout and
// error, without failing on the error so refusals can be asserted.
func captureRejectionRCAStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	var runErr error
	stdout := captureStdout(t, func() { runErr = run() })
	return stdout, runErr
}

func requireRenderedOutcome(t *testing.T, stdout, outcome, safeAction string) {
	t.Helper()
	for _, want := range []string{"outcome: " + outcome + "\n", "safe_action: " + safeAction + "\n", "task_id: " + rejectionRCACommandTask + "\n"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("output missing %q:\n%s", want, stdout)
		}
	}
}

func TestRejectionRCACommands(t *testing.T) {
	type adapter func(projectRoot, taskID string, payload any, authority models.AgentAuthority, opts ops.RejectionRCAOptions) error
	record := adapter(RecordRejectionRCACommand)
	resume := adapter(ResumeRejectionRCACommand)

	t.Run("success", func(t *testing.T) {
		projectRoot, _, authority := newRejectionRCACommandProject(t)
		stdout, err := captureRejectionRCAStdout(t, func() error {
			return record(projectRoot, rejectionRCACommandTask, rejectionRCARequestPayload(), authority, ops.RejectionRCAOptions{})
		})
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		requireRenderedOutcome(t, stdout, models.LifecycleCompleted, "continue")
		if !strings.Contains(stdout, "Recorded rejection RCA on "+rejectionRCACommandTask+" (fingerprint ") {
			t.Fatalf("record success message missing:\n%s", stdout)
		}

		stdout, err = captureRejectionRCAStdout(t, func() error {
			return resume(projectRoot, rejectionRCACommandTask, rejectionRCADispositionPayload(), authority, ops.RejectionRCAOptions{})
		})
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		requireRenderedOutcome(t, stdout, models.LifecycleCompleted, "continue")
		for _, want := range []string{
			"recovery_path " + models.RecoveryCapabilityReroute,
			"restore_mode " + models.RestoreModeAssign,
			"Task stays BLOCKED",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("resume success message missing %q:\n%s", want, stdout)
			}
		}
	})

	t.Run("NO_CHANGE", func(t *testing.T) {
		projectRoot, _, authority := newRejectionRCACommandProject(t)
		if _, err := captureRejectionRCAStdout(t, func() error {
			return record(projectRoot, rejectionRCACommandTask, rejectionRCARequestPayload(), authority, ops.RejectionRCAOptions{})
		}); err != nil {
			t.Fatalf("first record: %v", err)
		}
		stdout, err := captureRejectionRCAStdout(t, func() error {
			return record(projectRoot, rejectionRCACommandTask, rejectionRCARequestPayload(), authority, ops.RejectionRCAOptions{})
		})
		if err != nil {
			t.Fatalf("identical record: %v", err)
		}
		requireRenderedOutcome(t, stdout, models.LifecycleNoChange, "stop")
		if strings.Contains(stdout, "Recorded rejection RCA") {
			t.Fatalf("NO_CHANGE printed a success message implying a fresh mutation:\n%s", stdout)
		}

		// resume-rejection-rca has no content-equivalence path in ops (a
		// second resume meets a closed gate), so its printer is driven with the
		// NO_CHANGE outcome the shared lifecycle vocabulary defines.
		task := &models.Task{ID: rejectionRCACommandTask, Status: models.TaskStatusBlocked}
		noChange := &ops.RejectionRCAResult{LifecycleOutcome: ops.NewLifecycleNoChangeOutcome(payloadschema.ResumeRejectionRCAOperation, task)}
		stdout, err = captureRejectionRCAStdout(t, func() error {
			return printResumeRejectionRCAResult(rejectionRCACommandTask, noChange, nil)
		})
		if err != nil {
			t.Fatalf("resume NO_CHANGE rendering: %v", err)
		}
		requireRenderedOutcome(t, stdout, models.LifecycleNoChange, "stop")
		if strings.Contains(stdout, "Resumed rejection RCA") {
			t.Fatalf("NO_CHANGE printed a success message implying a fresh mutation:\n%s", stdout)
		}
	})

	t.Run("INVALID_INPUT", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			run     adapter
			payload map[string]any
		}{
			{"record", record, map[string]any{"schema_version": float64(models.RejectionRCASchemaVersion), "summary": "", "contributions": []any{}}},
			{"resume", resume, map[string]any{"schema_version": float64(models.RejectionRCASchemaVersion), "recovery_path": "reboot"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				projectRoot, statePath, authority := newRejectionRCACommandProject(t)
				before, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				stdout, runErr := captureRejectionRCAStdout(t, func() error {
					return tc.run(projectRoot, rejectionRCACommandTask, tc.payload, authority, ops.RejectionRCAOptions{})
				})
				var failure *ops.LifecycleError
				if !errors.As(runErr, &failure) || failure.Outcome.Outcome != models.LifecycleInvalidInput || len(failure.Outcome.Diagnostics) == 0 {
					t.Fatalf("error = %v, want INVALID_INPUT with diagnostics", runErr)
				}
				for _, want := range []string{"outcome: " + models.LifecycleInvalidInput + "\n", "safe_action: correct_input\n", "effects: none\n"} {
					if !strings.Contains(stdout, want) {
						t.Fatalf("output missing %q:\n%s", want, stdout)
					}
				}
				if strings.Contains(stdout, "reboot") {
					t.Fatalf("rendering echoed the rejected value:\n%s", stdout)
				}
				after, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("INVALID_INPUT changed state")
				}
			})
		}
	})
}
