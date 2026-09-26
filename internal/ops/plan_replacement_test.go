package ops

import (
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/taskkind"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D62: a plan output that names the task it replaces retires that task and
// retargets its consumers in the transaction that generates the replacement,
// or generates nothing.

const replacementPlanID = "plan"

func replacementChildID(index int) string {
	return perSubtaskChildID(replacementPlanID, "code-plan-to-coding", index)
}

func replacingOutput(supersedes string) models.OutputEntry {
	return models.OutputEntry{Desc: "replace " + supersedes, DoneWhen: "tests pass", Scope: "internal/", SpecRef: "README.md", Supersedes: supersedes}
}

func replacementPlan(verdict models.PlanCheckVerdict, outputs ...models.OutputEntry) models.Task {
	plan := handoffPlan(replacementPlanID, "code-planning-pair")
	if verdict != "" {
		plan = withPlanCheck(plan, verdict, "")
	}
	plan.Output = outputs
	return plan
}

func codingTask(id string, status models.TaskStatus, deps ...string) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, time.Now().UTC())
	task.RolePair = "coding-pair"
	task.DependsOn = deps
	return task
}

func setupPlanReplacementTest(t *testing.T, tasks ...models.Task) (string, string) {
	t.Helper()
	root, stateFile := setupPhase2PipelineProceedTest(t)
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
	state.Tasks = tasks
	for _, task := range tasks {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func readReplacementState(t *testing.T, stateFile string) *models.State {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func requireRetiredBy(t *testing.T, state *models.State, originalID string, replacements ...string) {
	t.Helper()
	original := state.FindTask(originalID)
	if original.Status != models.TaskStatusSuperseded || !slices.Equal(original.SupersededBy, replacements) {
		t.Fatalf("%s: status %s superseded_by %v, want SUPERSEDED by %v", originalID, original.Status, original.SupersededBy, replacements)
	}
	for _, id := range replacements {
		child := state.FindTask(id)
		if child == nil {
			t.Fatalf("replacement %s was not generated", id)
		}
		if child.Supersedes == nil || *child.Supersedes != originalID {
			t.Fatalf("replacement %s supersedes = %v, want %s", id, child.Supersedes, originalID)
		}
	}
}

// requireNothingGenerated asserts a refused replacing transition left the plan,
// its originals and the task list exactly as they were.
func requireNothingGenerated(t *testing.T, before, after *models.State) {
	t.Helper()
	if !reflect.DeepEqual(before.Tasks, after.Tasks) {
		t.Fatalf("refused plan replacement changed tasks\nbefore: %#v\nafter:  %#v", before.Tasks, after.Tasks)
	}
}

func requireFailureNaming(t *testing.T, report TransitionReport, want string) {
	t.Helper()
	for _, failure := range report.Failures {
		if failure.SourceTaskID == replacementPlanID && strings.Contains(failure.Error, want) {
			return
		}
	}
	t.Fatalf("failures = %v, want one for %s naming %q", report.Failures, replacementPlanID, want)
}

// R1: the replacement is generated, the original retired and its consumer
// retargeted in one pass; the result is a valid state.
func TestPlanReplacement_GenerationRetiresOriginalAndRetargetsConsumers(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("original")),
		codingTask("original", models.TaskStatusReady),
		codingTask("consumer", models.TaskStatusReady, "original"),
	)

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
		t.Fatalf("report = %+v, want the plan transitioned without failures", report)
	}

	state := readReplacementState(t, stateFile)
	requireRetiredBy(t, state, "original", replacementChildID(0))
	if got := state.FindTask("consumer").DependsOn; !slices.Equal(got, []string{replacementChildID(0)}) {
		t.Fatalf("consumer depends_on = %v, want [%s]", got, replacementChildID(0))
	}
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("state after plan replacement is invalid: %v", err)
	}
}

// The manual hand-off takes the same transaction as the automatic pass.
func TestPlanReplacement_ManualProceedRetiresOriginal(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("original")),
		codingTask("original", models.TaskStatusReady),
		codingTask("consumer", models.TaskStatusReady, "original"),
	)
	state := readReplacementState(t, stateFile)
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Proceed(root, replacementPlanID, "code-plan-to-coding")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.RetiredTaskIDs, []string{"original"}) {
		t.Fatalf("retired = %v, want [original]", result.RetiredTaskIDs)
	}
	state = readReplacementState(t, stateFile)
	requireRetiredBy(t, state, "original", replacementChildID(0))
	if got := state.FindTask("consumer").DependsOn; !slices.Equal(got, []string{replacementChildID(0)}) {
		t.Fatalf("consumer depends_on = %v, want [%s]", got, replacementChildID(0))
	}
}

// A corrective plan usually depends on the plan whose children it replaces.
// With default inheritance each replacement would wait on every original,
// which supersession turns into self and sibling cycles; the retired originals
// are excluded while an unreplaced upstream child keeps its barrier. A
// non-replacing output keeps waiting for the work: its edges to the originals
// are retargeted to their replacements.
func TestPlanReplacement_DefaultInheritanceExcludesRetiredOriginals(t *testing.T) {
	t.Parallel()
	upstream := handoffPlan("upstream", "code-planning-pair")
	upstream.Output = []models.OutputEntry{upstream.Output[0], upstream.Output[0], upstream.Output[0]}
	upstream.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	upstreamChild := func(i int) models.Task {
		child := codingTask(perSubtaskChildID("upstream", "code-plan-to-coding", i), models.TaskStatusReady)
		child.ParentTasks = []string{"upstream"}
		return child
	}
	x0, x1, kept := upstreamChild(0), upstreamChild(1), upstreamChild(2)
	publish := replacingOutput("")
	publish.Desc = "publish the corrected work"
	plan := replacementPlan(models.PlanCheckPassed, replacingOutput(x0.ID), replacingOutput(x1.ID), publish)
	plan.DependsOn = []string{"upstream"}
	root, stateFile := setupPlanReplacementTest(t, upstream, x0, x1, kept, plan)

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
		t.Fatalf("report = %+v, want the plan transitioned without failures", report)
	}
	state := readReplacementState(t, stateFile)
	requireRetiredBy(t, state, x0.ID, replacementChildID(0))
	requireRetiredBy(t, state, x1.ID, replacementChildID(1))
	for i := range 2 {
		if got := state.FindTask(replacementChildID(i)).DependsOn; !slices.Equal(got, []string{kept.ID}) {
			t.Fatalf("replacement %d depends_on = %v, want only the unreplaced upstream child [%s]", i, got, kept.ID)
		}
	}
	got := slices.Sorted(slices.Values(state.FindTask(replacementChildID(2)).DependsOn))
	want := slices.Sorted(slices.Values([]string{replacementChildID(0), replacementChildID(1), kept.ID}))
	if !slices.Equal(got, want) {
		t.Fatalf("non-replacing output depends_on = %v, want the replacements and the unreplaced child %v", got, want)
	}
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("state after plan replacement is invalid: %v", err)
	}
}

// R2: one original split across two outputs is retired by both children.
func TestPlanReplacement_SplitOriginalRetiredByEveryChild(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("original"), replacingOutput("original")),
		codingTask("original", models.TaskStatusReady),
	)

	if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	requireRetiredBy(t, readReplacementState(t, stateFile), "original", replacementChildID(0), replacementChildID(1))
}

// R3: Kind dedup must not remap a replacement onto the original it retires.
func TestPlanReplacement_KindDedupDoesNotRemapOntoOriginal(t *testing.T) {
	t.Parallel()
	original := codingTask("original", models.TaskStatusReady)
	original.Kind = taskkind.PreCommitBootstrap
	output := replacingOutput("original")
	output.Kind = taskkind.PreCommitBootstrap
	root, stateFile := setupPlanReplacementTest(t, replacementPlan(models.PlanCheckPassed, output), original)

	if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	requireRetiredBy(t, readReplacementState(t, stateFile), "original", replacementChildID(0))
}

// R4: a delivered original makes the plan stale. It is visible before
// generation, and an operator-admitted pass refuses it without side effects.
func TestPlanReplacement_MergedOriginalNeedsReconciliation(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("delivered")),
		codingTask("delivered", models.TaskStatusMerged),
	)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := readReplacementState(t, stateFile)
	class, blocker := domain.Classify(state, state.FindTask(replacementPlanID))
	if class != PlanHandoffNeedsReconciliation || !strings.Contains(blocker, "delivered") {
		t.Fatalf("passed plan class = %q blocker %q, want needs_reconciliation naming delivered", class, blocker)
	}
	undecided := replacementPlan("", replacingOutput("delivered"))
	if class, blocker := domain.Classify(state, &undecided); class != PlanHandoffNeedsReview || !strings.Contains(blocker, "delivered") {
		t.Fatalf("undecided plan class = %q blocker %q, want needs_review with a blocker naming delivered", class, blocker)
	}

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, "delivered")
	requireNothingGenerated(t, state, readReplacementState(t, stateFile))
}

// R5: an original still in flight holds the passed plan as a wait, not a
// refusal; once it becomes supersedable the automatic pass retires it.
func TestPlanReplacement_InFlightOriginalWaitsThenRetires(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("in-flight")),
		codingTask("in-flight", models.TaskStatusImplementing),
	)
	domain, err := LoadPlanHandoffDomain(root)
	if err != nil {
		t.Fatal(err)
	}
	state := readReplacementState(t, stateFile)
	if class, blocker := domain.Classify(state, state.FindTask(replacementPlanID)); class != PlanHandoffWaitingUpstream || !strings.Contains(blocker, "in-flight") {
		t.Fatalf("passed plan class = %q blocker %q, want waiting_upstream naming in-flight", class, blocker)
	}
	undecided := replacementPlan("", replacingOutput("in-flight"))
	if class, blocker := domain.Classify(state, &undecided); class != PlanHandoffNeedsReview || blocker != "" {
		t.Fatalf("undecided plan class = %q blocker %q, want needs_review without a blocker (a pass stays allowed)", class, blocker)
	}

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 0 || len(report.Failures) != 0 {
		t.Fatalf("report = %+v, want a silent wait", report)
	}

	if err := db.For(stateFile).Modify(func(s *models.State) error {
		blocked := codingTask("in-flight", models.TaskStatusBlocked)
		*s.FindTask("in-flight") = blocked
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	requireRetiredBy(t, readReplacementState(t, stateFile), "in-flight", replacementChildID(0))
}

// R6: crash recovery recreates a missing replacement without retiring its
// original a second time.
func TestPlanReplacement_RecoveryDoesNotRetireTwice(t *testing.T) {
	t.Parallel()
	plan := replacementPlan(models.PlanCheckPassed, replacingOutput("original"), replacingOutput(""))
	plan.Output[1].Desc = "independent work"
	plan.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	original := codingTask("original", models.TaskStatusSuperseded)
	original.SupersededBy = []string{replacementChildID(0)}
	reason := "replaced by plan"
	original.RescopeReason = &reason
	original.History = []models.TaskHistoryEntry{{Time: time.Now().UTC(), Event: models.TaskEventSuperseded}}
	root, stateFile := setupPlanReplacementTest(t, plan, original, codingTask(replacementChildID(1), models.TaskStatusReady))

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Failures) != 0 {
		t.Fatalf("failures = %v", report.Failures)
	}
	state := readReplacementState(t, stateFile)
	requireRetiredBy(t, state, "original", replacementChildID(0))
	if got := len(state.FindTask("original").History); got != 1 {
		t.Fatalf("original history has %d entries, want the single original supersession", got)
	}
}

// R6 variant: a live original under an executed marker can only come from a
// hand-edited state; recovery reports it instead of generating a duplicate.
func TestPlanReplacement_RecoveryRefusesLiveOriginalUnderMarker(t *testing.T) {
	t.Parallel()
	plan := replacementPlan(models.PlanCheckPassed, replacingOutput("original"))
	plan.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	root, stateFile := setupPlanReplacementTest(t, plan, codingTask("original", models.TaskStatusReady))
	before := readReplacementState(t, stateFile)

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, "original")
	requireNothingGenerated(t, before, readReplacementState(t, stateFile))
}

// R10: a failure after the first retirement leaves no original retired, no
// children and no marker: the replacing transition is all-or-nothing.
func TestPlanReplacement_PartialFailureCommitsNothing(t *testing.T) {
	t.Parallel()
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("first"), replacingOutput("second")),
		codingTask("first", models.TaskStatusReady),
		codingTask("second", models.TaskStatusReady),
		codingTask("consumer", models.TaskStatusReady, "first", "second"),
	)
	before := readReplacementState(t, stateFile)
	retired := 0
	planReplacementTestHooks.Store(db.For(stateFile), planReplacementTestHook{afterRetire: func(string) error {
		retired++
		if retired == 1 {
			return errors.New("injected failure after the first retirement")
		}
		return nil
	}})

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, "injected failure")
	requireNothingGenerated(t, before, readReplacementState(t, stateFile))
}

// R11: a refused replacing transition does not hold back another plan's
// transition in the same pass.
func TestPlanReplacement_RefusalIsolatedFromOtherTransitions(t *testing.T) {
	t.Parallel()
	other := withPlanCheck(handoffPlan("other", "code-planning-pair"), models.PlanCheckPassed, "")
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, replacingOutput("delivered")),
		codingTask("delivered", models.TaskStatusMerged),
		other,
	)

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, "delivered")
	state := readReplacementState(t, stateFile)
	if state.FindTask(perSubtaskChildID("other", "code-plan-to-coding", 0)) == nil {
		t.Fatal("the unrelated plan's child was not committed")
	}
	if state.FindTask(replacementChildID(0)) != nil || state.FindTask(replacementPlanID).TransitionsExecuted["code-plan-to-coding"] {
		t.Fatal("the refused plan generated children or recorded its marker")
	}
}

// R12: a child whose own dependency resolves through the original it retires
// would depend on itself; the candidate fails validation and nothing persists.
func TestPlanReplacement_SelfDependencyThroughOriginalRejected(t *testing.T) {
	t.Parallel()
	output := replacingOutput("original")
	output.TaskDependsOn = []string{"alias"}
	alias := codingTask("alias", models.TaskStatusSuperseded)
	alias.SupersededBy = []string{"original"}
	aliasReason := "replaced by original"
	alias.RescopeReason = &aliasReason
	root, stateFile := setupPlanReplacementTest(t,
		replacementPlan(models.PlanCheckPassed, output),
		codingTask("original", models.TaskStatusReady),
		alias,
	)
	before := readReplacementState(t, stateFile)

	report, err := ExecuteAvailableTransitionsReport(root, "")
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, report, replacementChildID(0)+" has depends_on referencing itself")
	requireNothingGenerated(t, before, readReplacementState(t, stateFile))
}

// R7: authoring rejects a supersedes target generation could never retire.
func TestSetTaskOutput_RejectsUnretirableSupersedes(t *testing.T) {
	t.Parallel()
	planner := testhelpers.BuildTaskByStatus("planner", models.TaskStatusCodePlanning, time.Now().UTC())
	planner.RolePair = "code-planning-pair"
	assignee := "code-planner-1"
	planner.AssignedTo = &assignee
	otherPlan := testhelpers.BuildTaskByStatus("other-plan", models.TaskStatusDraftCodingPlan, time.Now().UTC())
	otherPlan.RolePair = "code-planning-pair"
	tasks := []models.Task{
		planner,
		otherPlan,
		codingTask("live", models.TaskStatusReady),
		codingTask("delivered", models.TaskStatusMerged),
	}

	selfDependent := replacingOutput("live")
	selfDependent.TaskDependsOn = []string{"live"}
	for _, tc := range []struct {
		name   string
		output models.OutputEntry
		want   string
	}{
		{"unknown task", replacingOutput("missing"), `"missing"`},
		{"terminal task", replacingOutput("delivered"), `"delivered"`},
		{"the plan itself", replacingOutput("planner"), `"planner"`},
		{"wrong role pair", replacingOutput("other-plan"), `"other-plan"`},
		{"child depends on its original", selfDependent, `"live"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, stateFile := setupPlanReplacementTest(t, tasks...)
			before := readReplacementState(t, stateFile)
			err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "planner", AgentID: assignee, Output: []models.OutputEntry{tc.output}})
			testhelpers.RequireErrorContains(t, err, "supersedes")
			testhelpers.RequireErrorContains(t, err, tc.want)
			if after := readReplacementState(t, stateFile); !reflect.DeepEqual(before.Tasks, after.Tasks) {
				t.Fatal("rejected output changed state")
			}
		})
	}

	// An original still in flight may become supersedable before generation,
	// so authoring accepts it; the hand-off classifier makes the plan wait.
	for _, accepted := range []string{"live", "in-flight"} {
		t.Run(accepted+" same-pair task accepted", func(t *testing.T) {
			t.Parallel()
			root, stateFile := setupPlanReplacementTest(t, append(slices.Clone(tasks), codingTask("in-flight", models.TaskStatusImplementing))...)
			if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "planner", AgentID: assignee, Output: []models.OutputEntry{replacingOutput(accepted)}}); err != nil {
				t.Fatalf("valid supersedes refused: %v", err)
			}
			if got := readReplacementState(t, stateFile).FindTask("planner").Output[0].Supersedes; got != accepted {
				t.Fatalf("persisted supersedes = %q, want %s", got, accepted)
			}
		})
	}
}
