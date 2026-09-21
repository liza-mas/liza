package commands

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestUnblockTaskWithOptionsCommand_UnassignedPendingDependencySaysDependencyHeld(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	task.Worktree = nil
	task.BaseCommit = nil
	task.AssignedTo = nil
	task.LeaseExpires = nil
	task.DependsOn = []string{"dep-1"}
	dependency := testhelpers.BuildTaskByStatus("dep-1", models.TaskStatusImplementing, now)
	dependency.RolePair = "code-planning-pair"
	state.Tasks = []models.Task{task, dependency}
	testhelpers.WriteInitialState(t, statePath, state)

	output, err := captureUnblockTaskStdout(t, func() error {
		return UnblockTaskWithOptionsCommand(
			projectRoot,
			"task-1",
			"repair verified",
			"orchestrator-1",
			ops.UnblockTaskOptions{},
		)
	})
	if err != nil {
		t.Fatalf("UnblockTaskWithOptionsCommand() error: %v", err)
	}
	if !strings.Contains(output, "dependency-held") {
		t.Fatalf("output = %q, want dependency-held outcome", output)
	}
	if strings.Contains(output, ", claimable") {
		t.Fatalf("output = %q, must not report pending-dependency task as claimable", output)
	}
}

func captureUnblockTaskStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	os.Stdout = writer

	runErr := fn()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = oldStdout

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(output), runErr
}

// TestPrintUnblockTaskGateRefusal asserts the command surface hands the agent
// the outstanding step rather than a generic failure: printUnblockTaskResult
// wraps the ops refusal, so the two RCA command names must survive to the CLI.
func TestPrintUnblockTaskGateRefusal(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.RolePair = "code-planning-pair"
	reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
	task.BlockedReason = &reason
	task.ReviewCyclesTotal = 4
	task.RejectionRCA = &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      4,
		RejectionCount: 4,
		GatedAt:        now.Add(-time.Minute),
	}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, statePath, state)

	_, err := captureUnblockTaskStdout(t, func() error {
		return UnblockTaskWithOptionsCommand(
			projectRoot,
			"task-1",
			"repair verified",
			"orchestrator-1",
			ops.UnblockTaskOptions{},
		)
	})
	if err == nil {
		t.Fatal("UnblockTaskWithOptionsCommand() succeeded, want a gate refusal")
	}
	for _, want := range []string{"record-rejection-rca", "resume-rejection-rca"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to name %s", err.Error(), want)
		}
	}
}
