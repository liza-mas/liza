package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRetireMergedTaskRemovesWorktreeBranchAndPredecessor(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	worktree := filepath.Join(tmpDir, ".worktrees", "successor-1")
	testhelpers.MustGit(t, tmpDir, "worktree", "add", worktree, "-b", "task/successor-1")
	testhelpers.MustGit(t, tmpDir, "branch", "task/predecessor-1")

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	predecessor := testhelpers.BuildTaskByStatus("predecessor-1", models.TaskStatusSuperseded, now)
	predecessor.SupersededBy = []string{"successor-1"}
	state.Tasks = []models.Task{predecessor, testhelpers.BuildTaskByStatus("successor-1", models.TaskStatusMerged, now)}
	testhelpers.WriteInitialState(t, stateFile, state)

	gw := git.New(tmpDir)
	if warnings := retireMergedTask(db.New(stateFile), gw, "successor-1"); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if _, err := os.Stat(worktree); !os.IsNotExist(err) {
		t.Errorf("worktree directory survived retirement: %v", err)
	}
	for _, branch := range []string{"task/successor-1", "task/predecessor-1"} {
		if exists, err := gw.BranchExists(branch); err != nil || exists {
			t.Errorf("BranchExists(%s) = %v, %v; want it retired", branch, exists, err)
		}
	}
}

// The warning text and order are shared by the merge and by reconciliation:
// the worktree step reports first, the predecessor step last.
func TestRetireMergedTaskKeepsWarningTextAndOrder(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)

	warnings := retireMergedTask(db.New(filepath.Join(tmpDir, "missing", "state.yaml")), git.New(tmpDir), "bad/id")
	if len(warnings) < 2 {
		t.Fatalf("warnings = %v, want a worktree warning and a predecessor warning", warnings)
	}
	if !strings.HasPrefix(warnings[0], "failed to remove worktree directory: ") {
		t.Errorf("first warning = %q, want the worktree removal warning", warnings[0])
	}
	if last := warnings[len(warnings)-1]; !strings.HasPrefix(last, "predecessor cleanup: failed to read state: ") {
		t.Errorf("last warning = %q, want the predecessor cleanup warning", last)
	}
}

func TestRecordMergeCompletionAppendsBothMergeRecords(t *testing.T) {
	t.Parallel()
	task := &models.Task{ID: "task-1"}
	before := time.Now()
	recordMergeCompletion(task, "reviewer-1", "abc123", map[string]any{"tests_ran": true})
	after := time.Now()

	if len(task.HandoffEvents) != 1 || len(task.History) != 1 {
		t.Fatalf("records = %d handoff events, %d history entries; want one each", len(task.HandoffEvents), len(task.History))
	}
	handoff, entry := task.HandoffEvents[0], task.History[0]
	if handoff.Agent != "reviewer-1" || handoff.Trigger != models.HandoffTriggerCompletion {
		t.Errorf("handoff = %+v", handoff)
	}
	if entry.Event != models.TaskEventMerged || entry.Agent == nil || *entry.Agent != "reviewer-1" || entry.Commit == nil || *entry.Commit != "abc123" || entry.Extra["tests_ran"] != true {
		t.Errorf("history entry = %+v", entry)
	}
	for name, stamp := range map[string]time.Time{"handoff": handoff.Timestamp, "history": entry.Time} {
		if stamp.Before(before) || stamp.After(after) {
			t.Errorf("%s timestamp %v outside [%v, %v]", name, stamp, before, after)
		}
	}
}

func TestIntegrationReasonValuesAndDiagnosticType(t *testing.T) {
	t.Parallel()
	for reason, want := range map[IntegrationReason]string{
		IntegrationReasonHEADMismatch:           "worktree HEAD mismatch",
		IntegrationReasonMergeConflict:          "merge conflict",
		IntegrationReasonReviewBoundaryMismatch: "review boundary mismatch",
		IntegrationReasonTestsFailed:            "integration tests failed",
		IntegrationReasonStateInvalid:           "post-merge state validation failed",
	} {
		if string(reason) != want {
			t.Errorf("reason %q, want %q", reason, want)
		}
		// AC3: the persisted diagnostic holds a plain string, not the defined type.
		if _, ok := integrationFailureDiagnostic(reason, "", "", nil)["reason"].(string); !ok {
			t.Errorf("diagnostic reason for %q is not a string", reason)
		}
	}
	if got := (&IntegrationFailedError{Reason: IntegrationReasonTestsFailed}).Error(); got != "integration failed: integration tests failed" {
		t.Errorf("Error() = %q", got)
	}
}

func TestMergeFlowNamedValues(t *testing.T) {
	t.Parallel()
	if indexRefreshTriggerMerge != "merge" {
		t.Errorf("index refresh trigger = %q", indexRefreshTriggerMerge)
	}
	if integrationTestWaitDelay != 5*time.Second {
		t.Errorf("integration test wait delay = %v", integrationTestWaitDelay)
	}
	if got := integrationBranchOf(&models.State{}); got != "main" {
		t.Errorf("fallback integration branch = %q, want main", got)
	}
	state := &models.State{}
	state.Config.IntegrationBranch = "integration"
	if got := integrationBranchOf(state); got != "integration" {
		t.Errorf("configured integration branch = %q", got)
	}
}

func TestPreparationRetirable(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		m    mergeInvocation
		want bool
	}{
		{"cas never started", mergeInvocation{}, true},
		{"cas started and ref left moved", mergeInvocation{casStarted: true}, false},
		{"own merge rolled back", mergeInvocation{casStarted: true, forwardRolledBack: true}, true},
		{"provably nothing published", mergeInvocation{casStarted: true, forwardUnpublished: true}, true},
	} {
		if got := c.m.preparationRetirable(); got != c.want {
			t.Errorf("%s: preparationRetirable() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFillDetectedPostWorktreeCmdIsUnsetOnly(t *testing.T) {
	t.Parallel()
	detected := postMergeSetup{cmd: "npm install"}

	unset := &models.State{}
	m := &mergeInvocation{setup: detected}
	m.fillDetectedPostWorktreeCmd(unset)
	if unset.Config.PostWorktreeCmd == nil || *unset.Config.PostWorktreeCmd != "npm install" || !m.autoConfiguredSetup {
		t.Fatalf("unset configuration was not filled: %v, auto=%v", unset.Config.PostWorktreeCmd, m.autoConfiguredSetup)
	}

	explicit := "make setup"
	configured := &models.State{}
	configured.Config.PostWorktreeCmd = &explicit
	m = &mergeInvocation{setup: detected}
	m.fillDetectedPostWorktreeCmd(configured)
	if *configured.Config.PostWorktreeCmd != "make setup" || m.autoConfiguredSetup {
		t.Fatalf("explicit configuration was overwritten: %q, auto=%v", *configured.Config.PostWorktreeCmd, m.autoConfiguredSetup)
	}
	if got := detectPostMergeSetup(t.TempDir(), &explicit); got.cmd != "" || got.ambiguous {
		t.Errorf("detection ran for a configured value: %+v", got)
	}
}

func TestRunIntegrationScript(t *testing.T) {
	writeScript := func(t *testing.T, body string) string {
		t.Helper()
		root := t.TempDir()
		path := filepath.Join(root, filepath.FromSlash(integrationTestCommand))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("missing script proceeds without tests", func(t *testing.T) {
		run := runIntegrationScript(t.TempDir(), "task-1")
		if !run.notFound || run.ran || run.failed || run.output != "" {
			t.Fatalf("run = %+v", run)
		}
	})
	t.Run("passing script keeps its output", func(t *testing.T) {
		run := runIntegrationScript(writeScript(t, "#!/bin/sh\necho passed-output\n"), "task-1")
		if !run.ran || run.failed || !strings.Contains(run.output, "passed-output") {
			t.Fatalf("run = %+v", run)
		}
	})
	t.Run("failing script is a failure with its output", func(t *testing.T) {
		run := runIntegrationScript(writeScript(t, "#!/bin/sh\necho failing-output\nexit 3\n"), "task-1")
		if !run.ran || !run.failed || !strings.Contains(run.output, "failing-output") {
			t.Fatalf("run = %+v", run)
		}
	})
	t.Run("unstattable script proceeds without tests and is not missing", func(t *testing.T) {
		previous := statIntegrationScript
		statIntegrationScript = func(string) (os.FileInfo, error) { return nil, errors.New("stat denied") }
		t.Cleanup(func() { statIntegrationScript = previous })
		run := runIntegrationScript(t.TempDir(), "task-1")
		if run.ran || run.notFound || run.failed {
			t.Fatalf("run = %+v", run)
		}
	})
}
