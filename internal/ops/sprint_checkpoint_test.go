package ops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSprintCheckpoint_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-2 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(6 * time.Hour)
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
		testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	if result.CheckpointAt.IsZero() {
		t.Error("CheckpointAt should not be zero")
	}
	if result.ReportPath == "" {
		t.Error("ReportPath should not be empty")
	}

	// Verify report file was written
	content, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("Failed to read report: %v", err)
	}
	if !strings.Contains(string(content), "Sprint Summary") {
		t.Error("Report should contain 'Sprint Summary'")
	}
	if !strings.Contains(string(content), "MERGED") {
		t.Error("Report should contain task status table")
	}

	// Verify state updated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusCheckpoint {
		t.Errorf("Sprint status = %v, want CHECKPOINT", readState.Sprint.Status)
	}
	if readState.Sprint.Timeline.CheckpointAt == nil {
		t.Error("CheckpointAt should be set in state")
	}
}

func TestSprintCheckpoint_StoresTrigger(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = time.Now().UTC().Add(5 * time.Hour)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "PLANNING_COMPLETE")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "PLANNING_COMPLETE" {
		t.Errorf("CheckpointTrigger = %q, want %q", readState.Sprint.CheckpointTrigger, "PLANNING_COMPLETE")
	}
}

func TestSprintCheckpoint_EmptyTrigger(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = time.Now().UTC().Add(5 * time.Hour)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q, want empty", readState.Sprint.CheckpointTrigger)
	}
}

func TestSprintCheckpoint_AutoDetectsPlanningComplete(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)

	// Add a merged planning task with unconsumed output
	task := models.Task{
		ID:          "plan-1",
		Status:      models.TaskStatusMerged,
		Description: "Plan feature X",
		Created:     now,
		SpecRef:     "specs/x.md",
		DoneWhen:    "Plan approved",
		Scope:       "specs/",
		RolePair:    "code-planning-pair",
		Output: []models.OutputEntry{
			{Desc: "impl X", DoneWhen: "tests pass", Scope: "pkg/x"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Sprint.Scope.Planned = []string{"plan-1"}
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)

	// Pass empty trigger — should auto-detect PLANNING_COMPLETE
	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != "PLANNING_COMPLETE" {
		t.Errorf("CheckpointTrigger = %q, want %q (auto-detect)", readState.Sprint.CheckpointTrigger, "PLANNING_COMPLETE")
	}
}

func TestSprintCheckpoint_AutoDetectsManyToOneReady(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-1 * time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)

	cohort := makeManyToOneCohort("epic-plan-1", "us-writing-pair", models.TaskStatusMerged, "README.md", 2)
	state.Tasks = []models.Task{cohort[0], cohort[1]}
	state.Sprint.Scope.Planned = []string{cohort[0].ID, cohort[1].ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.CheckpointTrigger != models.CheckpointTriggerManyToOneReady {
		t.Errorf("CheckpointTrigger = %q, want %q (auto-detect)", readState.Sprint.CheckpointTrigger, models.CheckpointTriggerManyToOneReady)
	}
}

func TestSprintCheckpoint_AlreadyAtCheckpoint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCheckpoint
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error when already at CHECKPOINT")
	}
	if !errors.Is(err, ErrSprintAlreadyCheckpoint) {
		t.Fatalf("error = %v, want errors.Is(..., ErrSprintAlreadyCheckpoint)", err)
	}
}

func TestSprintCheckpoint_CompletedSprint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error for COMPLETED sprint")
	}
	if !strings.Contains(err.Error(), "COMPLETED") {
		t.Errorf("Error = %q, want to contain 'COMPLETED'", err.Error())
	}
}

func TestSprintCheckpoint_AbortedSprint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusAborted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SprintCheckpoint(tmpDir, "")
	if err == nil {
		t.Fatal("Expected error for ABORTED sprint")
	}
	if !strings.Contains(err.Error(), "ABORTED") {
		t.Errorf("Error = %q, want to contain 'ABORTED'", err.Error())
	}
}

// The steering-report obligation records the previous checkpoint, so the
// automatic summary covers only work merged since then (#171).
func TestSprintCheckpoint_ObligationRecordsThePreviousCheckpoint(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	previous := time.Now().UTC().Add(-30 * time.Minute).Truncate(time.Second)
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-2 * time.Hour)
	state.Sprint.Timeline.CheckpointAt = &previous
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := SprintCheckpoint(tmpDir, "PLANNING_COMPLETE")
	if err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	pending := readState.PendingCheckpointSummary
	if pending == nil || !pending.At.Equal(result.CheckpointAt) {
		t.Fatalf("obligation = %+v, want one at %v", pending, result.CheckpointAt)
	}
	if pending.Since == nil || !pending.Since.Equal(previous) {
		t.Errorf("Since = %v, want the previous checkpoint %v", pending.Since, previous)
	}
}

// A first checkpoint has no previous one; the summary falls back to the sprint
// start.
func TestSprintCheckpoint_FirstObligationHasNoSince(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = time.Now().UTC().Add(-2 * time.Hour)
	state.Sprint.Timeline.CheckpointAt = nil
	testhelpers.WriteInitialState(t, stateFile, state)

	if _, err := SprintCheckpoint(tmpDir, ""); err != nil {
		t.Fatalf("SprintCheckpoint() error: %v", err)
	}
	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.PendingCheckpointSummary == nil || readState.PendingCheckpointSummary.Since != nil {
		t.Errorf("obligation = %+v, want one without Since", readState.PendingCheckpointSummary)
	}
}

// passedPlanState persists an IN_PROGRESS sprint whose planned task "a" is a
// merged plan in the reviewed hand-off domain, passed a minute ago and never
// attempted (D-219). mutate adjusts the state before it is written.
func passedPlanState(t *testing.T, mutate func(*models.State)) (root, statePath string) {
	t.Helper()
	root = t.TempDir()
	statePath, _ = testhelpers.SetupLizaDir(t, root)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Sprint.Timeline.Started = now.Add(-time.Hour)
	state.Sprint.Timeline.Deadline = now.Add(5 * time.Hour)
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	plan := testhelpers.BuildTaskByStatus("a", models.TaskStatusMerged, now.Add(-10*time.Minute))
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{Desc: "implement a", DoneWhen: "tests pass", Scope: "pkg/a", SpecRef: "README.md"}}
	passedAt := now.Add(-time.Minute)
	orchestrator := "orchestrator-1"
	note := "passed"
	plan.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckPassed, By: orchestrator, At: passedAt}
	plan.History = append(plan.History, models.TaskHistoryEntry{Time: passedAt, Event: models.TaskEventPlanCheck, Agent: &orchestrator, Note: &note})
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{"a"}
	if mutate != nil {
		mutate(state)
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return root, statePath
}

// D-219: a plan an orchestrator turn left passed and unattempted gets the
// PLANNING_COMPLETE checkpoint, whichever trigger woke the turn.
func TestCheckpointPassedPlansCreatesPlanningCheckpoint(t *testing.T) {
	t.Parallel()
	root, statePath := passedPlanState(t, nil)

	ids, err := CheckpointPassedPlans(root)
	if err != nil || !slices.Equal(ids, []string{"a"}) {
		t.Fatalf("CheckpointPassedPlans = %v, %v; want [a], nil", ids, err)
	}
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Sprint.Status != models.SprintStatusCheckpoint || state.Sprint.CheckpointTrigger != models.CheckpointTriggerPlanningComplete || state.PendingCheckpointSummary == nil {
		t.Fatalf("sprint %s trigger %q obligation %+v; want a PLANNING_COMPLETE checkpoint with its summary obligation",
			state.Sprint.Status, state.Sprint.CheckpointTrigger, state.PendingCheckpointSummary)
	}
}

// The automatic checkpoint is admitted again under the lock that writes it: a
// competing write committed after the preliminary read wins, and nothing of
// the checkpoint (state or summary report) is written.
func TestCheckpointPassedPlansRevalidatesUnderLock(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	competingAt := now.Add(-5 * time.Minute)
	cases := []struct {
		name  string
		write func(*models.State)
	}{
		{"hard checkpoint", func(s *models.State) {
			s.Sprint.Status = models.SprintStatusCheckpoint
			s.Sprint.CheckpointTrigger = ""
			s.Sprint.Timeline.CheckpointAt = &competingAt
			s.PendingCheckpointSummary = &models.PendingCheckpointSummary{At: competingAt}
		}},
		{"sprint completed", func(s *models.State) { s.Sprint.Status = models.SprintStatusCompleted }},
		{"system paused", func(s *models.State) { s.Config.Mode = models.SystemModePaused }},
		{"plan held", func(s *models.State) {
			s.FindTask("a").PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: "provision the target", By: "orchestrator-1", At: now}
		}},
		{"transition attempted", func(s *models.State) { s.Sprint.Timeline.TransitionsAttemptedAt = &now }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a qualifying plan and a competing write landing between the
			// preliminary read and the locked write
			root, statePath := passedPlanState(t, nil)
			bb := db.New(statePath)
			var competing *models.State
			sprintCheckpointBeforeModifyTestHook = func() {
				if err := bb.Modify(func(s *models.State) error { tc.write(s); return nil }); err != nil {
					t.Errorf("competing write: %v", err)
				}
				competing, _ = bb.Read()
			}
			t.Cleanup(func() { sprintCheckpointBeforeModifyTestHook = nil })

			// WHEN the automatic checkpoint runs
			_, err := CheckpointPassedPlans(root)

			// THEN it is refused and the competing state is kept as written
			if competing == nil {
				t.Fatal("the preliminary read did not admit the checkpoint, so the lock was never reached")
			}
			if !errors.Is(err, ErrNoPassedPlanCheckpoint) {
				t.Fatalf("err = %v, want ErrNoPassedPlanCheckpoint", err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if after.Sprint.Status != competing.Sprint.Status || after.Sprint.CheckpointTrigger != competing.Sprint.CheckpointTrigger ||
				!timePtrEqual(after.Sprint.Timeline.CheckpointAt, competing.Sprint.Timeline.CheckpointAt) ||
				!pendingSummaryEqual(after.PendingCheckpointSummary, competing.PendingCheckpointSummary) {
				t.Fatalf("sprint %s/%q at %v obligation %+v; want the competing %s/%q at %v obligation %+v",
					after.Sprint.Status, after.Sprint.CheckpointTrigger, after.Sprint.Timeline.CheckpointAt, after.PendingCheckpointSummary,
					competing.Sprint.Status, competing.Sprint.CheckpointTrigger, competing.Sprint.Timeline.CheckpointAt, competing.PendingCheckpointSummary)
			}
			if _, err := os.Stat(paths.New(root).SprintSummaryPath()); !os.IsNotExist(err) {
				t.Errorf("sprint summary written for a refused checkpoint (stat err %v)", err)
			}
		})
	}
}

// Plans that need more than a checkpoint, and hard gates, admit none.
func TestCheckpointPassedPlansRefusesUnqualifiedState(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	cases := []struct {
		name   string
		mutate func(*models.State)
		refuse bool // persist a hand-off refusal before the call
	}{
		{name: "attempted after its pass", mutate: func(s *models.State) {
			attempted := s.Tasks[0].PlanCheck.At.Add(time.Minute)
			s.Sprint.Timeline.TransitionsAttemptedAt = &attempted
		}},
		{name: "attempted after its pass, fixture built late", mutate: func(s *models.State) {
			movePassRecordLater(&s.Tasks[0], 2*time.Minute)
			attempted := s.Tasks[0].PlanCheck.At.Add(time.Minute)
			s.Sprint.Timeline.TransitionsAttemptedAt = &attempted
		}},
		{name: "held", mutate: func(s *models.State) {
			s.Tasks[0].PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: "provision the target", By: "orchestrator-1", At: now}
		}},
		{name: "refused hand-off", refuse: true, mutate: func(s *models.State) {
			s.Tasks[0].Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "absent", Outputs: []int{0}}}}
		}},
		{name: "paused", mutate: func(s *models.State) { s.Config.Mode = models.SystemModePaused }},
		{name: "stopped", mutate: func(s *models.State) { s.Config.Mode = models.SystemModeStopped }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, statePath := passedPlanState(t, tc.mutate)
			if tc.refuse {
				if report, err := ExecuteTransitionsReportWith(root, "manual", AdmitReviewed); err != nil || len(report.Failures) != 1 {
					t.Fatalf("precondition: refusal = %+v, %v", report, err)
				}
			}
			if _, err := CheckpointPassedPlans(root); !errors.Is(err, ErrNoPassedPlanCheckpoint) {
				t.Fatalf("err = %v, want ErrNoPassedPlanCheckpoint", err)
			}
			state, err := db.New(statePath).Read()
			if err != nil {
				t.Fatal(err)
			}
			if state.Sprint.Status != models.SprintStatusInProgress {
				t.Fatalf("sprint = %s, want IN_PROGRESS", state.Sprint.Status)
			}
		})
	}
}

// movePassRecordLater shifts the fixture's recorded pass, PlanCheck.At and its
// plan_check history entry, to emulate a fixture built later than its parent.
func movePassRecordLater(task *models.Task, d time.Duration) {
	task.PlanCheck.At = task.PlanCheck.At.Add(d)
	for i := range task.History {
		if task.History[i].Event == models.TaskEventPlanCheck {
			task.History[i].Time = task.History[i].Time.Add(d)
		}
	}
}

func timePtrEqual(a, b *time.Time) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

func pendingSummaryEqual(a, b *models.PendingCheckpointSummary) bool {
	return a == nil && b == nil || a != nil && b != nil && a.At.Equal(b.At) && a.Trigger == b.Trigger && timePtrEqual(a.Since, b.Since)
}

func TestResolveCheckpointTrigger(t *testing.T) {
	t.Run("a given trigger is kept even when a hand-off is pending", func(t *testing.T) {
		root, statePath := passedPlanState(t, nil)
		state, err := db.New(statePath).Read()
		if err != nil {
			t.Fatal(err)
		}
		if got := resolveCheckpointTrigger(root, state, ""); got != models.CheckpointTriggerPlanningComplete {
			t.Fatalf("detected trigger = %q, want %q", got, models.CheckpointTriggerPlanningComplete)
		}
		if got := resolveCheckpointTrigger(root, state, "CUSTOM"); got != "CUSTOM" {
			t.Fatalf("given trigger = %q, want it kept", got)
		}
	})

	t.Run("a project without a pipeline keeps the trigger empty", func(t *testing.T) {
		root := t.TempDir()
		if got := resolveCheckpointTrigger(root, testhelpers.CreateValidState(), ""); got != "" {
			t.Fatalf("trigger = %q, want empty", got)
		}
	})
}

func TestStageSprintSummary(t *testing.T) {
	t.Run("stages the report beside its destination, readable by others", func(t *testing.T) {
		dir := t.TempDir()
		staged, err := stageSprintSummary(dir, "# report\n")
		if err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(staged); err != nil || string(got) != "# report\n" {
			t.Fatalf("staged content = %q, %v", got, err)
		}
		info, err := os.Stat(staged)
		if err != nil || !strings.HasPrefix(info.Name(), ".sprint-summary-") {
			t.Fatalf("staged file %v, %v: want the staging prefix", info, err)
		}
		// Windows reports a writable file as 0666: Chmod only clears its read-only attribute.
		want := os.FileMode(0o644)
		if runtime.GOOS == "windows" {
			want = 0o666
		}
		if info.Mode().Perm() != want {
			t.Fatalf("staged file mode = %v, want %v", info.Mode().Perm(), want)
		}
	})

	t.Run("an unwritable directory is reported as a staging failure", func(t *testing.T) {
		_, err := stageSprintSummary(t.TempDir()+"/missing", "# report\n")
		if err == nil || !strings.Contains(err.Error(), "failed to stage sprint summary") {
			t.Fatalf("err = %v, want a staging failure", err)
		}
	})
}

// A checkpoint refused under the lock publishes no report and leaves no
// staged file behind.
func TestSprintCheckpointRefusedUnderLockLeavesNoStagedReport(t *testing.T) {
	root, statePath := passedPlanState(t, nil)
	bb := db.New(statePath)
	sprintCheckpointBeforeModifyTestHook = func() {
		if err := bb.Modify(func(s *models.State) error {
			s.Sprint.Status = models.SprintStatusCompleted
			return nil
		}); err != nil {
			t.Errorf("competing write: %v", err)
		}
	}
	t.Cleanup(func() { sprintCheckpointBeforeModifyTestHook = nil })

	if _, err := CheckpointPassedPlans(root); !errors.Is(err, ErrNoPassedPlanCheckpoint) {
		t.Fatalf("err = %v, want ErrNoPassedPlanCheckpoint", err)
	}
	entries, err := os.ReadDir(filepath.Dir(paths.New(root).SprintSummaryPath()))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".sprint-summary-") || entry.Name() == "sprint_summary.md" {
			t.Errorf("%s left behind by a refused checkpoint", entry.Name())
		}
	}
}
