package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D73: one hand-off classification feeds wake rendering, the verifier,
// plan-check and transition admission.

func handoffPlan(id, rolePair string, deps ...string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, time.Now().UTC())
	task.RolePair = rolePair
	task.DependsOn = deps
	task.Output = []models.OutputEntry{{Desc: "implement " + id, DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md"}}
	return task
}

func withPlanCheck(task models.Task, verdict models.PlanCheckVerdict, ask string) models.Task {
	task.PlanCheck = &models.PlanCheck{Verdict: verdict, Ask: ask, By: "orchestrator-1", At: time.Now().UTC()}
	return task
}

func replannedPlan(task models.Task) models.Task {
	task.TransitionsExecuted = map[string]bool{"replanned": true, "code-plan-to-coding": true}
	return task
}

func TestPlanHandoffDomainClassify(t *testing.T) {
	t.Parallel()
	root, _ := setupReplanTest(t)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}

	const plan = "code-planning-pair"
	transitioned := handoffPlan("transitioned", plan)
	transitioned.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	unmerged := handoffPlan("unmerged", plan)
	unmerged.Status = models.TaskStatusCodingPlanApproved

	cases := []struct {
		name        string
		task        models.Task
		others      []models.Task
		want        PlanHandoffClass
		wantBlocker string
	}{
		{name: "not merged", task: unmerged, want: PlanHandoffNotSource},
		{name: "replanned", task: replannedPlan(handoffPlan("p", plan)), want: PlanHandoffNotSource},
		{name: "empty output", task: func() models.Task { p := handoffPlan("p", plan); p.Output = nil; return p }(), want: PlanHandoffNotSource},
		{name: "manual per-subtask code plan", task: handoffPlan("p", plan), want: PlanHandoffNeedsReview},
		{name: "manual per-subtask architecture plan", task: handoffPlan("p", "architecture-pair"), want: PlanHandoffNeedsReview},
		{name: "many-to-one source", task: handoffPlan("p", "us-writing-pair"), want: PlanHandoffOutOfDomain},
		{name: "auto-only source", task: handoffPlan("p", "integration-pair"), want: PlanHandoffOutOfDomain},
		{name: "auto master decomposition", task: handoffPlan("p", "code-planning-main-pair"), want: PlanHandoffOutOfDomain},
		{name: "held", task: withPlanCheck(handoffPlan("p", plan), models.PlanCheckHeld, "provision credentials"), want: PlanHandoffHeld, wantBlocker: "provision credentials"},
		{name: "passed", task: withPlanCheck(handoffPlan("p", plan), models.PlanCheckPassed, ""), want: PlanHandoffPassed},
		{
			name: "passed with transitioned upstream", task: withPlanCheck(handoffPlan("p", plan, "transitioned"), models.PlanCheckPassed, ""),
			others: []models.Task{transitioned}, want: PlanHandoffPassed,
		},
		{
			name: "passed with out-of-domain upstream", task: withPlanCheck(handoffPlan("p", plan, "us"), models.PlanCheckPassed, ""),
			others: []models.Task{handoffPlan("us", "us-writing-pair")}, want: PlanHandoffPassed,
		},
		{
			name: "passed with unreviewed upstream", task: withPlanCheck(handoffPlan("p", plan, "a"), models.PlanCheckPassed, ""),
			others: []models.Task{handoffPlan("a", plan)}, want: PlanHandoffWaitingUpstream, wantBlocker: "upstream a awaits plan review",
		},
		{
			name: "passed with replanned upstream", task: withPlanCheck(handoffPlan("p", plan, "a"), models.PlanCheckPassed, ""),
			others: []models.Task{replannedPlan(handoffPlan("a", plan))}, want: PlanHandoffNeedsReconciliation, wantBlocker: "upstream a was replanned",
		},
		{
			name: "passed with held upstream", task: withPlanCheck(handoffPlan("p", plan, "a"), models.PlanCheckPassed, ""),
			others: []models.Task{withPlanCheck(handoffPlan("a", plan), models.PlanCheckHeld, "seed smoke users")}, want: PlanHandoffNeedsReconciliation, wantBlocker: "upstream a is held: seed smoke users",
		},
		{
			name: "blocker survives a longer chain", task: withPlanCheck(handoffPlan("p", plan, "c"), models.PlanCheckPassed, ""),
			others: []models.Task{
				withPlanCheck(handoffPlan("c", plan, "a"), models.PlanCheckPassed, ""),
				replannedPlan(handoffPlan("a", plan)),
			},
			want: PlanHandoffNeedsReconciliation, wantBlocker: "upstream c: upstream a was replanned",
		},
		{
			name: "reconciliation outranks waiting", task: withPlanCheck(handoffPlan("p", plan, "w", "r"), models.PlanCheckPassed, ""),
			others: []models.Task{handoffPlan("w", plan), replannedPlan(handoffPlan("r", plan))},
			want:   PlanHandoffNeedsReconciliation, wantBlocker: "upstream r was replanned",
		},
		{
			name: "unreviewed plan reports the blocker a pass would hit", task: handoffPlan("p", plan, "a"),
			others: []models.Task{replannedPlan(handoffPlan("a", plan))}, want: PlanHandoffNeedsReview, wantBlocker: "upstream a was replanned",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := testhelpers.CreateValidState()
			state.Tasks = append([]models.Task{tc.task}, tc.others...)
			class, blocker := domain.Classify(state, &state.Tasks[0])
			if class != tc.want || blocker != tc.wantBlocker {
				t.Errorf("Classify = (%q, %q), want (%q, %q)", class, blocker, tc.want, tc.wantBlocker)
			}
		})
	}
}

func orchestratorAuthority() *models.AgentAuthority {
	return &models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}
}

func setupPlanCheckTest(t *testing.T, tasks ...models.Task) (string, string) {
	t.Helper()
	root, stateFile := setupReplanTest(t)
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	state.Tasks = tasks
	for _, task := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func readPlanCheck(t *testing.T, stateFile, taskID string) *models.PlanCheck {
	t.Helper()
	state, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state.FindTask(taskID).PlanCheck
}

func TestRecordPlanCheck_PassHoldClear(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanCheckTest(t, handoffPlan("p", "code-planning-pair"))

	result, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	if err != nil || !result.Changed || result.Class != PlanHandoffPassed {
		t.Fatalf("pass = %+v, %v; want changed, passed", result, err)
	}
	if replay, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil || replay.Changed {
		t.Fatalf("pass replay = %+v, %v; want unchanged success", replay, err)
	}
	// A passed plan can still be tightened to a hold before it transitions.
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionHold, Ask: "inject credentials", Authority: orchestratorAuthority()}); err != nil {
		t.Fatalf("hold on passed: %v", err)
	}
	if check := readPlanCheck(t, stateFile, "p"); check.Verdict != models.PlanCheckHeld || check.Ask != "inject credentials" || check.By != "orchestrator-1" {
		t.Fatalf("plan_check = %+v, want held by orchestrator-1", check)
	}
	if replay, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionHold, Ask: "inject credentials", Authority: orchestratorAuthority()}); err != nil || replay.Changed {
		t.Fatalf("hold replay = %+v, %v; want unchanged success", replay, err)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionClear, ChangedBy: "human"}); err != nil {
		t.Fatalf("operator clear: %v", err)
	}
	if check := readPlanCheck(t, stateFile, "p"); check != nil {
		t.Fatalf("plan_check = %+v after clear, want none", check)
	}
	if replay, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionClear, ChangedBy: "human"}); err != nil || replay.Changed || replay.Class != PlanHandoffNeedsReview {
		t.Fatalf("clear replay = %+v, %v; want unchanged, back to review", replay, err)
	}
}

// R6: a human hold is sticky; only an operator clear releases it.
func TestRecordPlanCheck_HoldIsSticky(t *testing.T) {
	t.Parallel()
	held := withPlanCheck(handoffPlan("p", "code-planning-pair"), models.PlanCheckHeld, "seed smoke users")
	root, stateFile := setupPlanCheckTest(t, held)

	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "held for human action")
	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionHold, Ask: "something else", Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "already held")
	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionClear, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "operator action")
	if check := readPlanCheck(t, stateFile, "p"); check.Verdict != models.PlanCheckHeld || check.Ask != "seed smoke users" {
		t.Fatalf("plan_check = %+v, want the original hold", check)
	}
}

// R4: a pass is refused while an in-domain upstream is replanned, held or unreviewed.
func TestRecordPlanCheck_PassRefusedByUpstream(t *testing.T) {
	t.Parallel()
	for name, upstream := range map[string]models.Task{
		"replanned":  replannedPlan(handoffPlan("a", "code-planning-pair")),
		"held":       withPlanCheck(handoffPlan("a", "code-planning-pair"), models.PlanCheckHeld, "provision"),
		"unreviewed": handoffPlan("a", "code-planning-pair"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, stateFile := setupPlanCheckTest(t, upstream, handoffPlan("b", "code-planning-pair", "a"))
			_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "b", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
			testhelpers.RequireErrorContains(t, err, "cannot pass while upstream a")
			if check := readPlanCheck(t, stateFile, "b"); check != nil {
				t.Fatalf("plan_check = %+v after refused pass", check)
			}
		})
	}
}

func TestRecordPlanCheck_RefusesTasksOutsideTheHandoff(t *testing.T) {
	t.Parallel()
	transitioned := handoffPlan("done", "code-planning-pair")
	transitioned.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	root, _ := setupPlanCheckTest(t, handoffPlan("us", "us-writing-pair"), transitioned)

	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "us", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "no reviewed hand-off")
	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "done", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "no plan awaiting hand-off")
	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "us", Action: PlanCheckActionPass, Authority: &models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration}})
	testhelpers.RequireErrorContains(t, err, "orchestrator decision")
}

func setupHandoffExecutorTest(t *testing.T, tasks ...models.Task) (string, string) {
	t.Helper()
	root, stateFile := setupPhase2PipelineProceedTest(t)
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress
	state.Tasks = tasks
	for _, task := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func transitionedSources(t *testing.T, report TransitionReport) map[string]bool {
	t.Helper()
	sources := make(map[string]bool)
	for _, result := range report.Results {
		sources[result.SourceTaskID] = true
	}
	return sources
}

// R2/R3: automatic passes expand only passed plans, judged under the lock that
// creates the children, so an undispositioned or late plan gets no children.
func TestExecuteTransitionsReportWith_ReviewedAdmission(t *testing.T) {
	t.Parallel()
	root, stateFile := setupHandoffExecutorTest(t,
		withPlanCheck(handoffPlan("passed", "code-planning-pair"), models.PlanCheckPassed, ""),
		handoffPlan("late", "code-planning-pair"),
		withPlanCheck(handoffPlan("held", "code-planning-pair"), models.PlanCheckHeld, "provision"),
		withPlanCheck(handoffPlan("reconcile", "code-planning-pair", "replanned"), models.PlanCheckPassed, ""),
		replannedPlan(handoffPlan("replanned", "code-planning-pair")),
	)

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if got := transitionedSources(t, report); len(got) != 1 || !got["passed"] {
		t.Fatalf("transitioned sources = %v, want only the passed plan", got)
	}
	if len(report.Failures) != 0 {
		t.Errorf("failures = %v; unadmitted plans are a normal wait", report.Failures)
	}
	state, _ := db.For(stateFile).Read()
	for _, id := range []string{"late", "held", "reconcile"} {
		if len(state.FindTask(id).TransitionsExecuted) != 0 {
			t.Errorf("%s transitioned under reviewed admission", id)
		}
	}
}

// An operator resume authorizes undispositioned plans but never a held one.
func TestExecuteTransitionsReportWith_OperatorAdmission(t *testing.T) {
	t.Parallel()
	root, _ := setupHandoffExecutorTest(t,
		handoffPlan("undecided", "code-planning-pair"),
		withPlanCheck(handoffPlan("held", "code-planning-pair"), models.PlanCheckHeld, "provision"),
	)

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := transitionedSources(t, report); len(got) != 1 || !got["undecided"] {
		t.Fatalf("transitioned sources = %v, want only the undispositioned plan", got)
	}
}

// R7: transitions outside the reviewed hand-off keep their behavior.
func TestExecuteTransitionsReportWith_OutOfDomainUnaffected(t *testing.T) {
	t.Parallel()
	us := handoffPlan("us-task-1", "us-writing-pair")
	us.Output = nil
	us.ParentTasks = []string{"epic-plan-1"}
	root, _ := setupHandoffExecutorTest(t, us)

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if got := transitionedSources(t, report); !got["us-task-1"] {
		t.Fatalf("transitioned sources = %v (failures %v), want the many-to-one cohort without a disposition", got, report.Failures)
	}
}

func TestProceed_RefusesHeldPlan(t *testing.T) {
	t.Parallel()
	root, stateFile := setupHandoffExecutorTest(t,
		withPlanCheck(handoffPlan("held", "code-planning-pair"), models.PlanCheckHeld, "provision"),
	)
	state, _ := db.For(stateFile).Read()
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Proceed(root, "held", "code-plan-to-coding")
	if err == nil || !strings.Contains(err.Error(), "held for human action") {
		t.Fatalf("Proceed on a held plan = %v, want refusal", err)
	}
}

// R7: an auto-only source (integration-to-fix) keeps firing from reviewer
// PreWork's reviewed pass without any disposition.
func TestExecuteTransitionsReportWith_AutoTransitionUnaffected(t *testing.T) {
	t.Parallel()
	root, stateFile := setupIntegrationPipelineProceedTest(t)
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress
	integration := handoffPlan("integration-task-1", "integration-pair")
	integration.Type = models.TaskTypeIntegration
	state.Tasks = []models.Task{integration}
	state.Sprint.Scope.Planned = []string{integration.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	report, err := ExecuteTransitionsReportWith(root, "auto", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if got := transitionedSources(t, report); !got["integration-task-1"] {
		t.Fatalf("transitioned sources = %v (failures %v), want the auto transition", got, report.Failures)
	}
}

// setupMixedSourceTest adds an auto one-to-one transition next to the manual
// code-plan-to-coding hand-off, so one planning source has both.
func setupMixedSourceTest(t *testing.T, plan models.Task) (string, string) {
	t.Helper()
	root, stateFile := setupPhase2PipelineProceedTest(t)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pipelinePath := filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml")
	config, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "        - name: code-plan-to-coding\n"
	if strings.Count(string(config), anchor) != 1 {
		t.Fatalf("pipeline fixture lacks %q", anchor)
	}
	mixed := strings.Replace(string(config), anchor, `        - name: code-plan-audit
          from: code-planning-pair.approved
          to: coding-pair.initial
          trigger: auto
          cardinality: one-to-one
`+anchor, 1)
	if err := os.WriteFile(pipelinePath, []byte(mixed), 0o644); err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Config.Mode = models.SystemModeRunning
	state.Config.AutoResume = true
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{plan.ID}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func executedTransitions(t *testing.T, stateFile, taskID string) map[string]bool {
	t.Helper()
	state, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state.FindTask(taskID).TransitionsExecuted
}

// Code review R2: an auto transition firing first leaves the manual hand-off
// pending. It still needs a disposition, wakes PLANNING_COMPLETE, and runs on
// its pass through checkpoint and auto-resume.
func TestPlanHandoff_MixedSourceKeepsManualHandoffReviewed(t *testing.T) {
	t.Parallel()
	root, stateFile := setupMixedSourceTest(t, handoffPlan("p", "code-planning-pair"))

	// Reviewer PreWork's auto-only pass.
	if _, err := ExecuteTransitionsReportWith(root, "auto", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	if got := executedTransitions(t, stateFile, "p"); !got["code-plan-audit"] || got["code-plan-to-coding"] {
		t.Fatalf("after auto pass: transitions %v, want only code-plan-audit", got)
	}
	// Orchestrator PreWork's reviewed pass must not bypass review.
	if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	if executedTransitions(t, stateFile, "p")["code-plan-to-coding"] {
		t.Fatal("undispositioned manual hand-off ran after the auto transition")
	}
	detCtx, err := LoadDetectionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := db.For(stateFile).Read()
	if !detCtx.PlanHandoff.PlanningCompleteEligible(state, state.FindTask("p")) {
		t.Fatal("pending manual hand-off must stay eligible for PLANNING_COMPLETE review")
	}

	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
		t.Fatalf("pass after the auto transition: %v", err)
	}
	checkpoint, err := SprintCheckpoint(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Trigger != models.CheckpointTriggerPlanningComplete {
		t.Fatalf("checkpoint trigger = %q, want PLANNING_COMPLETE for the pending hand-off", checkpoint.Trigger)
	}
	if _, err := AutoResume(root, "auto-resume"); err != nil {
		t.Fatal(err)
	}
	if !executedTransitions(t, stateFile, "p")["code-plan-to-coding"] {
		t.Fatal("passed manual hand-off did not run on auto-resume")
	}
}

func TestPlanHandoff_MixedSourceHeldHandoffStaysHeld(t *testing.T) {
	t.Parallel()
	held := withPlanCheck(handoffPlan("p", "code-planning-pair"), models.PlanCheckHeld, "provision")
	held.TransitionsExecuted = map[string]bool{"code-plan-audit": true}
	root, stateFile := setupMixedSourceTest(t, held)

	for _, admission := range []TransitionAdmission{AdmitReviewed, AdmitOperator} {
		if _, err := ExecuteTransitionsReportWith(root, "", admission); err != nil {
			t.Fatal(err)
		}
	}
	if executedTransitions(t, stateFile, "p")["code-plan-to-coding"] {
		t.Fatal("held manual hand-off ran after the auto transition")
	}
}

// Code review R3: a role configured with the orchestrator type records
// dispositions; the role name is not the authority.
func TestRecordPlanCheck_ConfiguredOrchestratorRole(t *testing.T) {
	t.Parallel()
	root, stateFile := setupReplanTest(t)
	pipelinePath := filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml")
	config, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatal(err)
	}
	const roleKey = "\n    orchestrator:\n      type: orchestrator\n"
	if strings.Count(string(config), roleKey) != 1 {
		t.Fatalf("pipeline fixture lacks %q", roleKey)
	}
	custom := strings.Replace(string(config), roleKey, "\n    lead:\n      type: orchestrator\n", 1)
	if err := os.WriteFile(pipelinePath, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["lead-1"] = testhelpers.RegisteredTestAgent("lead")
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	state.Tasks = []models.Task{handoffPlan("p", "code-planning-pair")}
	state.Sprint.Scope.Planned = []string{"p"}
	testhelpers.WriteInitialState(t, stateFile, state)

	lead := &models.AgentAuthority{ID: "lead-1", Generation: testhelpers.TestAgentGeneration}
	stale := &models.AgentAuthority{ID: "lead-1", Generation: testhelpers.TestAgentGeneration + "-stale"}
	coder := &models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration}

	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: coder})
	testhelpers.RequireErrorContains(t, err, "orchestrator decision")
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionHold, Ask: "provision", Authority: stale}); err == nil {
		t.Fatal("hold with a stale generation succeeded")
	}
	if check := readPlanCheck(t, stateFile, "p"); check != nil {
		t.Fatalf("plan_check = %+v after refused requests", check)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionPass, Authority: lead}); err != nil {
		t.Fatalf("pass by configured orchestrator: %v", err)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "p", Action: PlanCheckActionHold, Ask: "provision", Authority: lead}); err != nil {
		t.Fatalf("hold by configured orchestrator: %v", err)
	}
	if check := readPlanCheck(t, stateFile, "p"); check.Verdict != models.PlanCheckHeld || check.By != "lead-1" {
		t.Fatalf("plan_check = %+v, want held by lead-1", check)
	}
}
