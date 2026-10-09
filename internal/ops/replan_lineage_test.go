package ops

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-60: replan lineage (a replanned task and the replacement whose supersedes
// names it) is followed when a dependency names the replanned task.

func lineagePlan(id string, deps ...string) models.Task {
	task := providerOpsTask(id, "architecture-pair", models.TaskStatusMerged)
	task.DependsOn = deps
	task.Output = []models.OutputEntry{providerOpsOutput()}
	return task
}

func replannedLineagePlan(id string, deps ...string) models.Task {
	task := lineagePlan(id, deps...)
	task.TransitionsExecuted = map[string]bool{"replanned": true, providerOpsTransition: true}
	return task
}

func lineageSuccessor(task models.Task, originalID string) models.Task {
	task.Supersedes = testhelpers.StringPtr(originalID)
	return task
}

// transitionedLineagePlan is a MERGED plan whose hand-off already produced
// child <id>-cp-0.
func transitionedLineagePlan(id string) (models.Task, models.Task) {
	task := lineagePlan(id)
	task.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	child := providerOpsTask(id+"-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	child.ParentTasks = []string{id}
	return task, child
}

// ep-10 shape, step 4 of the D-60 RCA: the consumer was MERGED when its
// upstream was replanned, so its edge was left stale; replanning the consumer
// must not copy that stale edge into the new, re-authored task.
func TestReplan_ResolvesReplannedDependencyToSuccessor(t *testing.T) {
	t.Parallel()
	upstream := lineagePlan("a")
	consumer := lineagePlan("b", "a")
	root, statePath, _ := setupProviderOpsTest(t, upstream, consumer)

	upstreamReplan, err := Replan(root, &ReplanInput{TaskID: "a", ChangedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadTask(t, statePath, "b").DependsOn; !slices.Equal(got, []string{"a"}) {
		t.Fatalf("precondition: MERGED consumer depends_on = %v, want the untouched [a]", got)
	}

	consumerReplan, err := Replan(root, &ReplanInput{TaskID: "b", ChangedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	replacement := mustReadTask(t, statePath, consumerReplan.NewTaskID)
	if !slices.Equal(replacement.DependsOn, []string{upstreamReplan.NewTaskID}) {
		t.Fatalf("replacement depends_on = %v, want [%s]: a replanned upstream was copied instead of its successor",
			replacement.DependsOn, upstreamReplan.NewTaskID)
	}
}

// ep-10 exact shape (I-347): the MERGED replacement plan's reviewed output
// already targets the upstream's successor, through task_depends_on and a
// provider declaration; only its task-level depends_on is stale.
func TestRetargetDependency_RepairsMergedPlanReplanLineage(t *testing.T) {
	t.Parallel()
	successor, successorChild := transitionedLineagePlan("a-replan-1")
	successor = lineageSuccessor(successor, "a")
	stale := lineageSuccessor(lineagePlan("b-replan-1", "a"), "b")
	stale.Output[0].TaskDependsOn = []string{successor.ID}
	stale.Output[0].ProviderDependencies = providerOpsDependency(successor.ID, 0)
	stale.Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: successor.ID, Outputs: []int{0}}}}
	root, statePath, _ := setupProviderOpsTest(t,
		replannedLineagePlan("a"), successor, successorChild,
		replannedLineagePlan("b", "a"), stale,
	)

	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: stale.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "upstream a was replanned")

	if _, err := RetargetDependency(root, stale.ID, "a", []string{successor.ID}, "repair stale replan lineage edge", "orchestrator-1"); err != nil {
		t.Fatalf("lineage repair of a MERGED plan with an unexecuted hand-off refused: %v", err)
	}
	repaired := mustReadTask(t, statePath, stale.ID)
	if !slices.Equal(repaired.DependsOn, []string{successor.ID}) {
		t.Fatalf("depends_on = %v, want [%s]", repaired.DependsOn, successor.ID)
	}
	if !reflect.DeepEqual(repaired.Output, stale.Output) {
		t.Fatalf("repair rewrote reviewed output: %+v, want %+v", repaired.Output, stale.Output)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: stale.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
		t.Fatalf("repaired plan still refused: %v", err)
	}
}

// The repaired edge must carry the whole-phase barrier into generated
// children: the hand-off inherits the successor's children.
func TestRetargetDependency_MergedPlanRepairRestoresInheritedBarrier(t *testing.T) {
	t.Parallel()
	successor, successorChild := transitionedLineagePlan("a-replan-1")
	successor = lineageSuccessor(successor, "a")
	stale := lineageSuccessor(lineagePlan("b-replan-1", "a"), "b")
	stale.Output[0].TaskDependsOn = []string{successor.ID}
	root, statePath, bb := setupProviderOpsTest(t,
		replannedLineagePlan("a"), successor, successorChild,
		replannedLineagePlan("b", "a"), stale,
	)

	if _, err := RetargetDependency(root, stale.ID, "a", []string{successor.ID}, "repair stale replan lineage edge", "orchestrator-1"); err != nil {
		t.Fatalf("lineage repair refused: %v", err)
	}
	if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: stale.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
		t.Fatalf("repaired plan refused: %v", err)
	}
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	generated, err := Proceed(root, stale.ID, providerOpsTransition)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(generated.ChildTaskIDs, []string{"b-replan-1-cp-0"}) {
		t.Fatalf("generated children = %v", generated.ChildTaskIDs)
	}
	if deps := mustReadTask(t, statePath, "b-replan-1-cp-0").DependsOn; !slices.Contains(deps, successorChild.ID) {
		t.Fatalf("generated child depends_on = %v, want the successor's child %s inherited", deps, successorChild.ID)
	}
}

// I-367 shape: arm-cp-0 was replanned; its replacement keeps arm as parent.
func replannedProviderChildTasks(replacementStatus models.TaskStatus) []models.Task {
	provider, _ := transitionedLineagePlan("arm")
	original := providerOpsTask("arm-cp-0", "code-planning-pair", models.TaskStatusMerged)
	original.ParentTasks = []string{"arm"}
	original.Output = []models.OutputEntry{providerOpsOutput()}
	original.TransitionsExecuted = map[string]bool{"replanned": true, "code-plan-to-coding": true}
	replacement := providerOpsTask("arm-cp-0-replan-1", "code-planning-pair", replacementStatus)
	replacement.ParentTasks = []string{"arm"}
	replacement.Supersedes = testhelpers.StringPtr(original.ID)
	return []models.Task{provider, original, replacement}
}

func TestSetTaskOutputProviderDependencies_ReplannedChildResolvesToSuccessor(t *testing.T) {
	t.Parallel()
	owner := providerOpsTask("owner", "architecture-pair", models.TaskStatus("ARCHITECTING"))
	owner.AssignedTo = testhelpers.StringPtr("architect-1")
	root, _, _ := setupProviderOpsTest(t, append(replannedProviderChildTasks(models.TaskStatusMerged), owner)...)
	output := providerOpsOutput()
	output.ProviderDependencies = providerOpsDependency("arm", 0)
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: owner.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}}); err != nil {
		t.Fatalf("declaration on a replanned child slot refused: %v", err)
	}
}

func TestClaimTaskProviderDependencies_ReplannedChildGatesOnSuccessor(t *testing.T) {
	t.Parallel()
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency("arm", 0)
	root, _, bb := setupProviderOpsTest(t, append(replannedProviderChildTasks(models.TaskStatusDraftCodingPlan), consumer)...)

	_, err := ClaimTask(root, consumer.ID, "architect-1")
	if err == nil || !strings.Contains(err.Error(), "arm-cp-0-replan-1") || strings.Contains(err.Error(), "retired") {
		t.Fatalf("claim error = %v, want a pending wait on successor arm-cp-0-replan-1", err)
	}
	if err := bb.Modify(func(s *models.State) error {
		*s.FindTask("arm-cp-0-replan-1") = replannedProviderChildTasks(models.TaskStatusMerged)[2]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTask(root, consumer.ID, "architect-1"); err != nil {
		t.Fatalf("merged successor did not release the consumer: %v", err)
	}
}

// repairShape is the ep-10 shape with a held plan, so every refusal also
// proves the disposition is left alone.
func repairShape() []models.Task {
	successor, successorChild := transitionedLineagePlan("a-replan-1")
	successor = lineageSuccessor(successor, "a")
	stale := lineageSuccessor(lineagePlan("b-replan-1", "a", "x"), "b")
	stale.Output[0].TaskDependsOn = []string{successor.ID}
	stale = withPlanCheck(stale, models.PlanCheckHeld, "reconcile the producing-task lineage")
	return []models.Task{replannedLineagePlan("a"), successor, successorChild, replannedLineagePlan("b", "a"), lineagePlan("x"), stale}
}

func TestRetargetDependency_MergedPlanRepairRefusalsAreAtomic(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mutate  func(tasks []models.Task)
		old     string
		newDeps []string
		want    string
	}{
		"output still names the replanned upstream": {
			mutate: func(tasks []models.Task) { tasks[5].Output[0].TaskDependsOn = []string{"a-replan-1", "a"} },
			want:   "output[0] still names a",
		},
		"output names a replanned upstream through inheritance": {
			mutate: func(tasks []models.Task) {
				tasks[5].Output[0].InheritInputs = &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "a", Outputs: []int{0}}}}
			},
			want: "output[0] still names a",
		},
		"output predates the replan": {
			mutate: func(tasks []models.Task) { tasks[5].Output[0].TaskDependsOn = nil },
			want:   "no output names replan successor a-replan-1",
		},
		"successor not merged": {
			mutate: func(tasks []models.Task) {
				tasks[1] = lineageSuccessor(providerOpsTask("a-replan-1", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE")), "a")
				tasks[2].ParentTasks = []string{"x"}
			},
			want: "is DRAFT_ARCHITECTURE, not MERGED",
		},
		"hand-off already ran": {
			mutate: func(tasks []models.Task) { tasks[5].TransitionsExecuted = map[string]bool{providerOpsTransition: true} },
			want:   "hand-off already ran",
		},
		"not retired lineage": {
			old:  "x",
			want: "x has no live replan successor",
		},
		"not the successor": {
			newDeps: []string{"a-replan-1", "x"},
			want:    "only be retargeted to its replan successor a-replan-1",
		},
		"not merged": {
			mutate: func(tasks []models.Task) { tasks[5].Status = models.TaskStatusSuperseded },
			want:   "only a MERGED planning task",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tasks := repairShape()
			if tc.mutate != nil {
				tc.mutate(tasks)
			}
			old, newDeps := "a", []string{"a-replan-1"}
			if tc.old != "" {
				old = tc.old
			}
			if tc.newDeps != nil {
				newDeps = tc.newDeps
			}
			root, statePath, _ := setupProviderOpsTest(t, tasks...)
			before := replacementBytes(t, statePath)
			_, err := RetargetDependency(root, "b-replan-1", old, newDeps, "repair stale replan lineage edge", "orchestrator-1")
			requireProviderOpsAtomicRefusal(t, statePath, before, err, "terminal task b-replan-1", tc.want)
		})
	}
}

func TestRetargetDependency_MergedPlanRepairKeepsHold(t *testing.T) {
	t.Parallel()
	root, statePath, _ := setupProviderOpsTest(t, repairShape()...)
	before := *mustReadTask(t, statePath, "b-replan-1").PlanCheck

	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: "b-replan-1", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "retarget-dependency b-replan-1 a a-replan-1")

	if _, err := RetargetDependency(root, "b-replan-1", "a", []string{"a-replan-1"}, "repair stale replan lineage edge", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	repaired := mustReadTask(t, statePath, "b-replan-1")
	if !slices.Equal(repaired.DependsOn, []string{"a-replan-1", "x"}) || !reflect.DeepEqual(*repaired.PlanCheck, before) {
		t.Fatalf("repair changed more than the edge: depends_on=%v plan_check=%+v", repaired.DependsOn, repaired.PlanCheck)
	}
	last := repaired.History[len(repaired.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten || last.Extra["merged_plan_lineage_repair"] != true {
		t.Fatalf("repair audit = %+v", last)
	}
	_, err = RecordPlanCheck(root, PlanCheckInput{TaskID: "b-replan-1", Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	testhelpers.RequireErrorContains(t, err, "is held")
	if strings.Contains(err.Error(), "retarget-dependency") {
		t.Fatalf("repaired plan still offered a repair: %v", err)
	}
}

// D-77 shape (I-525): the reviewed output names the successor's child, never
// the successor itself, so it was written after the successor's hand-off.
// depends_on keeps the replanned upstream, alone or beside the successor.
func TestRetargetDependency_RepairsMergedPlanNamingSuccessorChildren(t *testing.T) {
	t.Parallel()
	for name, deps := range map[string][]string{
		"stale edge only":             {"a"},
		"stale edge beside successor": {"a", "a-replan-1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			successor, successorChild := transitionedLineagePlan("a-replan-1")
			successor = lineageSuccessor(successor, "a")
			stale := lineageSuccessor(lineagePlan("b-replan-1", deps...), "b")
			stale.Output[0].TaskDependsOn = []string{successorChild.ID}
			root, statePath, _ := setupProviderOpsTest(t,
				replannedLineagePlan("a"), successor, successorChild,
				replannedLineagePlan("b", "a"), stale,
			)

			_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: stale.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
			testhelpers.RequireErrorContains(t, err, "retarget-dependency b-replan-1 a a-replan-1")

			if _, err := RetargetDependency(root, stale.ID, "a", []string{successor.ID}, "repair stale replan lineage edge", "orchestrator-1"); err != nil {
				t.Fatalf("lineage repair of a plan naming the successor's child refused: %v", err)
			}
			repaired := mustReadTask(t, statePath, stale.ID)
			if !slices.Equal(repaired.DependsOn, []string{successor.ID}) {
				t.Fatalf("depends_on = %v, want [%s]", repaired.DependsOn, successor.ID)
			}
			if !reflect.DeepEqual(repaired.Output, stale.Output) {
				t.Fatalf("repair rewrote reviewed output: %+v, want %+v", repaired.Output, stale.Output)
			}
			if _, err := RecordPlanCheck(root, PlanCheckInput{TaskID: stale.ID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()}); err != nil {
				t.Fatalf("repaired plan still refused: %v", err)
			}
		})
	}
}

// A child of the retired lineage is as stale as the replanned task itself,
// even when the output also names the successor's child, or when that child
// also has the replanned task as a parent.
func TestRetargetDependency_RefusesOutputNamingRetiredLineageChild(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		mutate func(retiredChild, successorChild, stale *models.Task)
		want   string
	}{
		"child of the replanned upstream beside the successor's child": {
			mutate: func(retiredChild, successorChild, stale *models.Task) {
				stale.Output[0].TaskDependsOn = []string{successorChild.ID, retiredChild.ID}
			},
			want: "output[0] still names a-cp-0",
		},
		"successor's child also parented by the replanned upstream": {
			mutate: func(_, successorChild, stale *models.Task) {
				successorChild.ParentTasks = []string{"a-replan-1", "a"}
				stale.Output[0].TaskDependsOn = []string{successorChild.ID}
			},
			want: "output[0] still names a-replan-1-cp-0",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			successor, successorChild := transitionedLineagePlan("a-replan-1")
			successor = lineageSuccessor(successor, "a")
			retiredChild := providerOpsTask("a-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
			retiredChild.ParentTasks = []string{"a"}
			stale := lineageSuccessor(lineagePlan("b-replan-1", "a"), "b")
			tc.mutate(&retiredChild, &successorChild, &stale)
			root, statePath, _ := setupProviderOpsTest(t,
				replannedLineagePlan("a"), retiredChild, successor, successorChild,
				replannedLineagePlan("b", "a"), stale,
			)
			before := replacementBytes(t, statePath)
			_, err := RetargetDependency(root, stale.ID, "a", []string{successor.ID}, "repair stale replan lineage edge", "orchestrator-1")
			requireProviderOpsAtomicRefusal(t, statePath, before, err, "terminal task b-replan-1", tc.want)
		})
	}
}

// An upstream whose successor was retired keeps the stale edge: its reconcile
// signal must stay visible.
func TestReplan_KeepsDependencyWithoutLiveSuccessor(t *testing.T) {
	t.Parallel()
	retired := lineageSuccessor(providerOpsTask("a-replan-1", "architecture-pair", models.TaskStatusAbandoned), "a")
	root, statePath, _ := setupProviderOpsTest(t, replannedLineagePlan("a"), retired, lineagePlan("b", "a"))
	result, err := Replan(root, &ReplanInput{TaskID: "b", ChangedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadTask(t, statePath, result.NewTaskID).DependsOn; !slices.Equal(got, []string{"a"}) {
		t.Fatalf("depends_on = %v, want the stale [a] kept", got)
	}
}

func TestReplan_RecordsSuccessorDependency(t *testing.T) {
	t.Parallel()
	successor := lineageSuccessor(lineagePlan("a-replan-1"), "a")
	root, statePath, _ := setupProviderOpsTest(t, replannedLineagePlan("a"), successor, lineagePlan("b", "a"))
	result, err := Replan(root, &ReplanInput{TaskID: "b", ChangedBy: "human"})
	if err != nil {
		t.Fatal(err)
	}
	history := mustReadTask(t, statePath, result.NewTaskID).History
	if len(history) != 1 || history[0].Extra["replanned_dependency"] != "a" || history[0].Extra["replacement_dependency"] != "a-replan-1" {
		t.Fatalf("history = %+v", history)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "follows its successor a-replan-1") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestProviderDependencies_ReplannedChildRefusals(t *testing.T) {
	t.Parallel()
	declare := func(t *testing.T, tasks []models.Task) (string, []byte, error) {
		owner := providerOpsTask("owner", "architecture-pair", models.TaskStatus("ARCHITECTING"))
		owner.AssignedTo = testhelpers.StringPtr("architect-1")
		root, statePath, _ := setupProviderOpsTest(t, append(tasks, owner)...)
		before := replacementBytes(t, statePath)
		output := providerOpsOutput()
		output.ProviderDependencies = providerOpsDependency("arm", 0)
		return statePath, before, SetTaskOutput(root, &SetTaskOutputInput{TaskID: owner.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}})
	}
	t.Run("retired successor", func(t *testing.T) {
		t.Parallel()
		statePath, before, err := declare(t, replannedProviderChildTasks(models.TaskStatusAbandoned))
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "selected child arm-cp-0-replan-1 was retired")
	})
	t.Run("replanned provider is not remapped", func(t *testing.T) {
		t.Parallel()
		tasks := replannedProviderChildTasks(models.TaskStatusMerged)
		tasks[0].TransitionsExecuted["replanned"] = true
		tasks = append(tasks, lineageSuccessor(lineagePlan("arm-replan-1"), "arm"))
		statePath, before, err := declare(t, tasks)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "retired provider arm")
	})
}

// The retirement barrier names the effective child a live declaration waits on.
func TestReplanProviderDependencies_RefusesRetiringEffectiveChild(t *testing.T) {
	t.Parallel()
	tasks := replannedProviderChildTasks(models.TaskStatusMerged)
	tasks[2].Output = []models.OutputEntry{providerOpsOutput()}
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency("arm", 0)
	root, statePath, _ := setupProviderOpsTest(t, append(tasks, consumer)...)
	before := replacementBytes(t, statePath)
	_, err := Replan(root, &ReplanInput{TaskID: "arm-cp-0-replan-1", ChangedBy: "human"})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "live provider_dependencies", consumer.ID)
}

// Cycle detection sees the effective child: consumer → arm-cp-0-replan-1 →
// consumer closes through the replan successor, not the retired projection.
func TestRetargetDependency_RejectsCycleThroughReplannedProviderChild(t *testing.T) {
	t.Parallel()
	tasks := replannedProviderChildTasks(models.TaskStatusDraftCodingPlan)
	tasks[2].DependsOn = []string{"x"}
	consumer := providerOpsTask("consumer", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	consumer.ProviderDependencies = providerOpsDependency("arm", 0)
	root, statePath, _ := setupProviderOpsTest(t, append(tasks, lineagePlan("x"), consumer)...)
	before := replacementBytes(t, statePath)
	_, err := RetargetDependency(root, "arm-cp-0-replan-1", "x", []string{consumer.ID}, "close a cycle", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cycle")
	var operational *OperationalError
	if !errors.As(err, &operational) {
		t.Fatalf("error = %T %v, want an operational cycle envelope", err, err)
	}
	if path, _ := operational.Details["cycle_path"].([]string); !slices.Contains(path, "arm-cp-0-replan-1") || !slices.Contains(path, consumer.ID) {
		t.Fatalf("cycle_path = %v, want it through consumer and the replan successor", operational.Details["cycle_path"])
	}
}
