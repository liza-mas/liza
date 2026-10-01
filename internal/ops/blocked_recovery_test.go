package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Blocked-recovery rules at each replacement entry point (ADR-0171). The cap
// itself is exercised end to end in internal/integration.

func writeBlockedSource(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	source := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	source.History = append(source.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventBlocked})
	state.Tasks = []models.Task{source, testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, time.Now().UTC())}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func TestSupersedeTask_BlockedRecoveryRequiresAndRecordsChange(t *testing.T) {
	t.Parallel()

	t.Run("refuses a BLOCKED source without a stated change", func(t *testing.T) {
		t.Parallel()
		root, stateFile := writeBlockedSource(t)
		before, _ := db.New(stateFile).Read()
		_, err := SupersedeTask(root, "task-1", []string{"task-2"}, "replace", "orchestrator-1")
		testhelpers.RequireErrorContains(t, err, "requires stating what changed")
		testhelpers.RequireErrorContains(t, err, "Blocked on clarification")
		after, _ := db.New(stateFile).Read()
		if !reflect.DeepEqual(before.Tasks, after.Tasks) {
			t.Fatal("refused supersession changed tasks")
		}
	})

	t.Run("non-BLOCKED and unreplaced sources need no change and get no marker", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		testhelpers.SetupTestGitRepo(t, root)
		stateFile, _ := testhelpers.SetupLizaDir(t, root)
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{
			testhelpers.BuildTaskByStatus("ready", models.TaskStatusReady, time.Now().UTC()),
			testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, time.Now().UTC()),
			testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, time.Now().UTC()),
		}
		testhelpers.WriteInitialState(t, stateFile, state)
		if _, err := SupersedeTask(root, "ready", []string{"task-2"}, "rescope", "orchestrator-1"); err != nil {
			t.Fatalf("READY source: %v", err)
		}
		if _, err := SupersedeTaskWithOptions(root, "blocked", nil, "already merged", "orchestrator-1",
			SupersedeTaskOptions{RecoverabilityCommand: "liza recover-task blocked"}); err != nil {
			t.Fatalf("unreplaced BLOCKED source: %v", err)
		}
		state, _ = db.New(stateFile).Read()
		for _, id := range []string{"ready", "blocked"} {
			if record := models.BlockedRecovery(state.FindTask(id)); record != nil {
				t.Fatalf("%s marker = %v, want none", id, record)
			}
		}
	})

	t.Run("a different change is a different lifecycle request", func(t *testing.T) {
		t.Parallel()
		root, stateFile := writeBlockedSource(t)
		state, _ := db.New(stateFile).Read()
		opts := LifecycleRequestOptions{RequestID: "supersede-1", ExpectedTransition: models.TaskTransitionID(state.FindTask("task-1"))}
		if _, err := SupersedeTaskWithOptions(root, "task-1", []string{"task-2"}, "replace", "orchestrator-1",
			SupersedeTaskOptions{Request: opts, Changed: "first statement"}); err != nil {
			t.Fatal(err)
		}
		replay, err := SupersedeTaskWithOptions(root, "task-1", []string{"task-2"}, "replace", "orchestrator-1",
			SupersedeTaskOptions{Request: opts, Changed: "first statement"})
		if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted {
			t.Fatalf("identical retry = %+v, %v; want a replay", replay, err)
		}
		_, err = SupersedeTaskWithOptions(root, "task-1", []string{"task-2"}, "replace", "orchestrator-1",
			SupersedeTaskOptions{Request: opts, Changed: "second statement"})
		// changed is part of the request fingerprint, so the identity is reused.
		testhelpers.RequireErrorContains(t, err, "already used with a different payload")
	})
}

// Not parallel: the masker reads secret values from the environment.
func TestSupersedeTask_BlockedRecoveryRecordsCauseAndMaskedChange(t *testing.T) {
	t.Setenv("RECOVER_TOKEN", "secret-value-123")
	root, stateFile := writeBlockedSource(t)
	if _, err := SupersedeTaskWithOptions(root, "task-1", []string{"task-2"}, "replace", "orchestrator-1",
		SupersedeTaskOptions{Changed: "use secret-value-123 against the fixed interface"}); err != nil {
		t.Fatal(err)
	}
	state, _ := db.New(stateFile).Read()
	record := models.BlockedRecovery(state.FindTask("task-1"))
	if record["blocked_reason"] != "Blocked on clarification" || record["blocked_at"] == nil {
		t.Fatalf("marker = %v, want blocked cause and episode", record)
	}
	changed, _ := record["changed"].(string)
	if strings.Contains(changed, "secret-value-123") || !strings.Contains(changed, "fixed interface") {
		t.Fatalf("changed = %q, want masked statement", changed)
	}
}

func TestReplaceTask_BlockedSourceRequiresChange(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) replacementFixture {
		f := newReplacementFixture(t)
		s := replacementState(t, f)
		blocked := testhelpers.BuildTaskByStatus("source", models.TaskStatusBlocked, time.Now().UTC())
		blocked.History = append(blocked.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventBlocked})
		*s.FindTask("source") = blocked
		testhelpers.WriteInitialState(t, f.statePath, s)
		f.opts.ExpectedTransition = models.TaskTransitionID(s.FindTask("source"))
		return f
	}

	t.Run("refused without changed", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		before := replacementBytes(t, f.statePath)
		_, err := f.run()
		testhelpers.RequireErrorContains(t, err, "requires stating what changed")
		if string(before) != string(replacementBytes(t, f.statePath)) {
			t.Fatal("refused replacement changed state")
		}
	})

	t.Run("recorded with changed", func(t *testing.T) {
		t.Parallel()
		f := setup(t)
		f.input.Changed = "split along the corrected boundary"
		if _, err := f.run(); err != nil {
			t.Fatal(err)
		}
		record := models.BlockedRecovery(replacementState(t, f).FindTask("source"))
		if record["changed"] != "split along the corrected boundary" {
			t.Fatalf("marker = %v", record)
		}
	})
}

// cappedLineage returns cap-a -> cap-b -> cap-c, each replaced while BLOCKED,
// with cap-c BLOCKED at the cap.
func cappedLineage() []models.Task {
	now := time.Now().UTC()
	var tasks []models.Task
	for i, id := range []string{"cap-a", "cap-b"} {
		task := codingTask(id, models.TaskStatusBlocked)
		task.History = append(task.History, models.TaskHistoryEntry{Time: now.Add(time.Duration(i) * time.Minute), Event: models.TaskEventBlocked})
		record := models.BlockedRecoveryRecord(&task, "attempt "+id)
		reason := "replaced"
		task.Status, task.RescopeReason, task.AssignedTo, task.Worktree = models.TaskStatusSuperseded, &reason, nil, nil
		task.History = append(task.History, models.TaskHistoryEntry{Time: now.Add(time.Duration(i)*time.Minute + time.Second), Event: models.TaskEventSuperseded,
			Extra: map[string]any{models.BlockedRecoveryKey: record}})
		tasks = append(tasks, task)
	}
	tasks[0].SupersededBy = []string{"cap-b"}
	tasks[1].SupersededBy = []string{"cap-c"}
	head := codingTask("cap-c", models.TaskStatusBlocked)
	head.History = append(head.History, models.TaskHistoryEntry{Time: now.Add(5 * time.Minute), Event: models.TaskEventBlocked})
	return append(tasks, head)
}

func TestSetTaskOutput_SupersedesRequiresChangeAndRespectsCap(t *testing.T) {
	t.Parallel()
	planner := testhelpers.BuildTaskByStatus("planner", models.TaskStatusCodePlanning, time.Now().UTC())
	planner.RolePair = "code-planning-pair"
	assignee := "code-planner-1"
	planner.AssignedTo = &assignee
	tasks := append([]models.Task{planner, codingTask("live", models.TaskStatusReady)}, cappedLineage()...)

	unstated := replacingOutput("live")
	unstated.Changed = ""
	for _, tc := range []struct {
		name   string
		output models.OutputEntry
		want   string
	}{
		{"no stated change, even for a live original", unstated, "requires changed"},
		{"BLOCKED original at the cap", replacingOutput("cap-c"), "blocked-recovery cap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, stateFile := setupPlanReplacementTest(t, tasks...)
			before := readReplacementState(t, stateFile)
			err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "planner", AgentID: assignee, Output: []models.OutputEntry{tc.output}})
			testhelpers.RequireErrorContains(t, err, tc.want)
			if after := readReplacementState(t, stateFile); !reflect.DeepEqual(before.Tasks, after.Tasks) {
				t.Fatal("rejected output changed state")
			}
		})
	}
}

func TestPlanReplacement_BlockedOriginal(t *testing.T) {
	t.Parallel()

	t.Run("records the marker with the output's change", func(t *testing.T) {
		t.Parallel()
		root, stateFile := setupPlanReplacementTest(t,
			replacementPlan(models.PlanCheckPassed, replacingOutput("stuck")),
			codingTask("stuck", models.TaskStatusBlocked),
		)
		if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
			t.Fatal(err)
		}
		state := readReplacementState(t, stateFile)
		requireRetiredBy(t, state, "stuck", replacementChildID(0))
		if record := models.BlockedRecovery(state.FindTask("stuck")); record["changed"] != "corrected interface for stuck" {
			t.Fatalf("marker = %v", record)
		}
	})

	t.Run("a legacy output without a change is refused atomically", func(t *testing.T) {
		t.Parallel()
		legacy := replacingOutput("stuck")
		legacy.Changed = ""
		root, stateFile := setupPlanReplacementTest(t,
			replacementPlan(models.PlanCheckPassed, legacy),
			codingTask("stuck", models.TaskStatusBlocked),
		)
		before := readReplacementState(t, stateFile)
		report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
		if err != nil {
			t.Fatal(err)
		}
		if len(report.Failures) == 0 || !strings.Contains(report.Failures[0].Error, "requires stating what changed") {
			t.Fatalf("report = %+v, want a refusal naming the missing change", report)
		}
		if after := readReplacementState(t, stateFile); !reflect.DeepEqual(before.Tasks, after.Tasks) {
			t.Fatal("refused generation changed tasks")
		}
	})
}

// freshFailureHead writes a capped lineage cap-a -> cap-b -> task-review whose
// head is in review with a real worktree, then drives the real failed fresh
// recovery, which enters BLOCKED through TaskEventRecoveryFreshFailed rather
// than TaskEventBlocked. prepare may edit the state first.
func freshFailureHead(t *testing.T, prepare func(*models.State)) (string, string) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	gw := git.New(root)
	baseCommit, err := gw.CreateWorktree("task-review", "integration")
	if err != nil {
		t.Fatal(err)
	}
	reviewCommit, err := gw.GetWorktreeHEAD("task-review")
	if err != nil {
		t.Fatal(err)
	}
	worktree := ".worktrees/task-review"
	reviewer := "code-reviewer-1"
	head := codingTask("task-review", models.TaskStatusReviewing)
	head.ReviewingBy, head.ReviewCommit, head.BaseCommit, head.Worktree = &reviewer, &reviewCommit, &baseCommit, &worktree
	head.AssignedTo, head.BlockedReason, head.BlockedQuestions = nil, nil, nil
	lineage := cappedLineage()[:2]
	lineage[1].SupersededBy = []string{"task-review"}
	state := testhelpers.CreateValidState()
	state.Agents[reviewer] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing, CurrentTask: &head.ID}
	state.Tasks = append(lineage, head, testhelpers.BuildTaskByStatus("next", models.TaskStatusReady, time.Now().UTC()))
	if prepare != nil {
		prepare(state)
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	testRecoverTaskHooks = &recoverTaskTestHooks{beforeFreshCreate: func() {
		if err := os.MkdirAll(filepath.Join(root, ".worktrees", "task-review"), 0755); err != nil {
			panic(err)
		}
	}}
	defer func() { testRecoverTaskHooks = nil }()
	if _, err := RecoverTaskWithOptions(root, "task-review", "fresh creation race", RecoverTaskOptions{Fresh: true}); err == nil || !strings.Contains(err.Error(), "task marked BLOCKED") {
		t.Fatalf("fresh recovery error = %v, want a BLOCKED failure", err)
	}
	got := readReplacementState(t, stateFile).FindTask("task-review")
	if got.Status != models.TaskStatusBlocked || got.History[len(got.History)-1].Event != models.TaskEventRecoveryFreshFailed {
		t.Fatalf("head = %s, want BLOCKED by the fresh-failure event", got.Status)
	}
	return root, stateFile
}

// Not parallel: the fresh-recovery failure hook is package state.
func TestBlockedRecoveryCap_FailedFreshRecoveryEpisode(t *testing.T) {
	supersedeHead := func(root string) error {
		_, err := SupersedeTaskWithOptions(root, "task-review", []string{"next"}, "replace", "orchestrator-1",
			SupersedeTaskOptions{Changed: "new approach"})
		return err
	}

	t.Run("its halt carries the episode and resume releases it", func(t *testing.T) {
		root, stateFile := freshFailureHead(t, nil)
		testhelpers.RequireErrorContains(t, supersedeHead(root), "blocked-recovery cap")
		if _, err := Analyze(root); err != nil {
			t.Fatal(err)
		}
		state := readReplacementState(t, stateFile)
		subject := state.CircuitBreaker.CurrentResponse.Subject
		if subject == nil || subject.TaskID != "task-review" || !subject.BlockedAt.Equal(models.LatestHistoryTime(state.FindTask("task-review"), models.TaskEventRecoveryFreshFailed)) {
			t.Fatalf("subject = %+v, want the fresh-failure episode", subject)
		}
		if _, err := Resume(root, "operator-1"); err != nil {
			t.Fatal(err)
		}
		if err := supersedeHead(root); err != nil {
			t.Fatalf("released replacement refused: %v", err)
		}
	})

	t.Run("an earlier resolved episode does not release it", func(t *testing.T) {
		earlier := time.Now().UTC().Add(-time.Hour)
		root, _ := freshFailureHead(t, func(s *models.State) {
			head := s.FindTask("task-review")
			head.History = append(head.History,
				models.TaskHistoryEntry{Time: earlier, Event: models.TaskEventBlocked},
				models.TaskHistoryEntry{Time: earlier.Add(time.Minute), Event: models.TaskEventUnblocked})
			pattern, resolved := models.BlockedReplacementChainPattern, earlier.Add(30*time.Second)
			s.CircuitBreaker.History = append(s.CircuitBreaker.History, models.CircuitBreakerHistory{
				Timestamp: earlier, Pattern: &pattern, Result: "TRIGGERED", Response: models.CircuitBreakerResponseHalt,
				ResolvedAt: &resolved, Subject: &models.CircuitBreakerSubject{TaskID: "task-review", BlockedAt: earlier}})
		})
		testhelpers.RequireErrorContains(t, supersedeHead(root), "blocked-recovery cap")
	})
}

// The taught single-replacement path checks the cap in the same transaction
// that would create the replacement: a refusal leaves no runnable task behind.
func TestReplaceTask_CappedSourceCreatesNoReplacement(t *testing.T) {
	t.Parallel()
	f := newReplacementFixture(t)
	s := replacementState(t, f)
	lineage := cappedLineage()[:2]
	lineage[1].SupersededBy = []string{"source"}
	source := testhelpers.BuildTaskByStatus("source", models.TaskStatusBlocked, time.Now().UTC())
	source.History = append(source.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventBlocked})
	*s.FindTask("source") = source
	s.Tasks = append(s.Tasks, lineage...)
	testhelpers.WriteInitialState(t, f.statePath, s)
	f.opts.ExpectedTransition = models.TaskTransitionID(s.FindTask("source"))
	f.input.Changed = "another approach"

	before := replacementBytes(t, f.statePath)
	_, err := f.run()
	testhelpers.RequireErrorContains(t, err, "blocked-recovery cap")
	if string(before) != string(replacementBytes(t, f.statePath)) {
		t.Fatal("refused replacement changed state")
	}
	if replacementState(t, f).FindTask("replacement") != nil {
		t.Fatal("refused replacement left a runnable task")
	}
}
