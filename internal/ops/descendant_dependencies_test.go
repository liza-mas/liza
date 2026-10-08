package ops

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/taskkind"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// D-79: descendant_dependencies defer a typed provider wait from a generated
// plan to the outputs that plan generates. Fixtures inject the field through
// the stored YAML representation, so they read the same whether or not the
// Go field is present.

const (
	descendantTransition  = "code-plan-to-coding"
	descendantCodingDraft = models.TaskStatus("DRAFT_CODE")
)

type descendantFixture struct {
	AtTransition         string                      `yaml:"at_transition" json:"at_transition"`
	ProviderDependencies []models.ProviderDependency `yaml:"provider_dependencies" json:"provider_dependencies"`
}

func gateWait(outputs ...int) []models.ProviderDependency {
	return []models.ProviderDependency{{ProviderTask: "gate", Transition: descendantTransition, Outputs: outputs}}
}

func gateDescendants(outputs ...int) []descendantFixture {
	return []descendantFixture{{AtTransition: descendantTransition, ProviderDependencies: gateWait(outputs...)}}
}

// withDescendants returns v re-decoded from its YAML form with
// descendant_dependencies set.
func withDescendants[T any](t *testing.T, v T, deps []descendantFixture) T {
	t.Helper()
	raw, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := yaml.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(deps)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := yaml.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	fields["descendant_dependencies"] = value
	if raw, err = yaml.Marshal(fields); err != nil {
		t.Fatal(err)
	}
	var out T
	if err := yaml.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// descendantsOf reads descendant_dependencies back from v's YAML form.
func descendantsOf(t *testing.T, v any) []descendantFixture {
	t.Helper()
	raw, err := yaml.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Deps []descendantFixture `yaml:"descendant_dependencies"`
	}
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded.Deps
}

// gatePlan is a MERGED code plan with n coding outputs: the writers a
// descendant wait orders after. Unexpanded, it can be replanned; expanded, its
// first writer exists in DRAFT_CODE.
func gatePlan(n int, expanded bool) []models.Task {
	gate := providerOpsTask("gate", "code-planning-pair", models.TaskStatusMerged)
	for range n {
		gate.Output = append(gate.Output, providerOpsOutput())
	}
	if !expanded {
		return []models.Task{gate}
	}
	gate.TransitionsExecuted = map[string]bool{descendantTransition: true}
	tasks := []models.Task{gate}
	for i := range n {
		code := providerOpsTask(perSubtaskChildID(gate.ID, "code", i), "coding-pair", descendantCodingDraft)
		code.ParentTasks = []string{gate.ID}
		tasks = append(tasks, code)
	}
	return tasks
}

func completeSprintForHandoff(t *testing.T, bb *db.Blackboard) {
	t.Helper()
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
}

func executingCodePlan(t *testing.T, id string) models.Task {
	t.Helper()
	plan := startExecuting(providerOpsTask(id, "code-planning-pair", models.TaskStatusCodePlanning), "code-planner-1")
	return withDescendants(t, plan, gateDescendants(0))
}

func requireNoChild(t *testing.T, statePath, id string) {
	t.Helper()
	if readClaimStateForTest(t, statePath).FindTask(id) != nil {
		t.Fatalf("refused hand-off generated %s", id)
	}
}

// startExecuting gives an executing task the fields state validation requires
// of it; setupDescendantTest supplies its worktree directory and working agent.
func startExecuting(task models.Task, agentID string) models.Task {
	task = claimedOnce(task)
	lease := time.Now().UTC().Add(30 * time.Minute)
	task.AssignedTo = testhelpers.StringPtr(agentID)
	task.LeaseExpires = &lease
	task.Worktree = testhelpers.StringPtr(".worktrees/" + task.ID)
	task.BaseCommit = testhelpers.StringPtr("abc1234")
	return task
}

// setupDescendantTest is setupProviderOpsTest with each assigned task's
// worktree directory and its agent working on it.
func setupDescendantTest(t *testing.T, tasks ...models.Task) (string, string, *db.Blackboard) {
	t.Helper()
	root, statePath, bb := setupProviderOpsTest(t, tasks...)
	if err := bb.Modify(func(s *models.State) error {
		for _, task := range tasks {
			if task.AssignedTo == nil {
				continue
			}
			if err := os.MkdirAll(filepath.Join(root, ".worktrees", task.ID), 0o755); err != nil {
				return err
			}
			agent := s.Agents[*task.AssignedTo]
			agent.Status, agent.CurrentTask = models.AgentStatusWorking, testhelpers.StringPtr(task.ID)
			s.Agents[*task.AssignedTo] = agent
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, statePath, bb
}

// --- Generation: planning is not held by a deferred implementation wait ---

func TestDescendantDependenciesGeneratedPlanIsClaimableWhileWriterIsDraft(t *testing.T) {
	t.Parallel()
	// GIVEN an architecture whose output defers the gate writer's wait to its code plan's outputs.
	arch := providerOpsTask("arch", "architecture-pair", models.TaskStatusMerged)
	arch.Output = []models.OutputEntry{withDescendants(t, providerOpsOutput(), gateDescendants(0))}
	root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, true), arch)...)
	completeSprintForHandoff(t, bb)

	// WHEN the architecture hands off.
	if _, err := Proceed(root, arch.ID, providerOpsTransition); err != nil {
		t.Fatal(err)
	}

	// THEN the code plan carries the deferred wait, not as its own admission wait.
	child := mustReadTask(t, statePath, "arch-cp-0")
	if got := descendantsOf(t, child); !reflect.DeepEqual(got, gateDescendants(0)) {
		t.Fatalf("generated code plan descendant_dependencies = %+v, want %+v", got, gateDescendants(0))
	}
	if len(child.ProviderDependencies) != 0 {
		t.Fatalf("deferred wait became the planner's own admission wait: %+v", child.ProviderDependencies)
	}
	// AND planning proceeds while the gate's writer is still DRAFT_CODE.
	if _, err := ClaimTask(root, child.ID, "code-planner-1"); err != nil {
		t.Fatalf("planner held by a deferred implementation wait: %v", err)
	}
	if status := mustReadTask(t, statePath, "gate-code-0").Status; status != descendantCodingDraft {
		t.Fatalf("gate writer status = %s, want %s", status, descendantCodingDraft)
	}
}

// --- Application: every output of the consuming plan carries the wait ---

func TestDescendantDependenciesApplyToEveryOutputOfTheConsumingPlan(t *testing.T) {
	t.Parallel()
	// GIVEN a code plan carrying a deferred wait on gate output 0, and an output already waiting on gate output 1.
	plan := executingCodePlan(t, "cp")
	root, statePath, _ := setupDescendantTest(t, append(gatePlan(2, false), plan)...)
	first := providerOpsOutput()
	second := providerOpsOutput()
	second.ProviderDependencies = gateWait(1)

	// WHEN the planner writes its output.
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.ID, AgentID: "code-planner-1", Output: []models.OutputEntry{first, second}}); err != nil {
		t.Fatalf("set-task-output refused: %v", err)
	}

	// THEN every stored output entry carries the wait, merged with an existing same-provider declaration.
	stored := mustReadTask(t, statePath, plan.ID).Output
	if len(stored) != 2 || !reflect.DeepEqual(stored[0].ProviderDependencies, gateWait(0)) {
		t.Fatalf("output[0] did not receive the deferred wait: %+v", stored)
	}
	if deps := stored[1].ProviderDependencies; len(deps) != 1 || deps[0].ProviderTask != "gate" || deps[0].Transition != descendantTransition {
		t.Fatalf("output[1] declarations = %+v, want one merged gate declaration", deps)
	} else if got := slices.Sorted(slices.Values(deps[0].Outputs)); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("output[1] selected gate outputs = %v, want [0 1]", got)
	}
}

func TestDescendantDependenciesGenerationRefusesOutputThatDroppedTheWait(t *testing.T) {
	for _, carried := range []bool{false, true} {
		t.Run(map[bool]string{false: "dropped", true: "carried"}[carried], func(t *testing.T) {
			t.Parallel()
			// GIVEN a MERGED code plan carrying a deferred wait, with a stored output that does or does not carry it.
			plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
			output := providerOpsOutput()
			if carried {
				output.ProviderDependencies = gateWait(0)
			}
			plan.Output = []models.OutputEntry{output}
			plan = withDescendants(t, plan, gateDescendants(0))
			root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, false), plan)...)
			completeSprintForHandoff(t, bb)
			before := replacementBytes(t, statePath)

			// WHEN it hands off to coding.
			_, err := Proceed(root, plan.ID, descendantTransition)

			// THEN a dropped wait is refused before any writer exists; a carried one reaches the writer.
			if !carried {
				requireProviderOpsAtomicRefusal(t, statePath, before, err, "descendant_dependencies", "gate")
				requireNoChild(t, statePath, "cp-code-0")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if writer := mustReadTask(t, statePath, "cp-code-0"); !reflect.DeepEqual(writer.ProviderDependencies, gateWait(0)) {
				t.Fatalf("writer lost the ordering wait: %+v", writer.ProviderDependencies)
			}
		})
	}
}

// --- Diagnosis: an architecture gating planning on a coding child is warned ---

func TestDescendantDependenciesSetTaskOutputWarnsWhenPlanningWaitsOnCoding(t *testing.T) {
	t.Parallel()
	arch := providerOpsTask("arch", "architecture-pair", models.TaskStatus("ARCHITECTING"))
	arch = startExecuting(arch, "architect-1")
	root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), arch)...)
	output := providerOpsOutput()
	output.ProviderDependencies = gateWait(0)

	result, err := SetTaskOutputWithOptions(root, &SetTaskOutputInput{TaskID: arch.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}})

	if err != nil {
		t.Fatalf("a planning-level wait on coding must stay expressible: %v", err)
	}
	if !reflect.DeepEqual(mustReadTask(t, statePath, arch.ID).Output[0].ProviderDependencies, gateWait(0)) {
		t.Fatal("warned output was not persisted")
	}
	joined := strings.Join(result.Warnings, "\n")
	for _, fragment := range []string{"output[0]", "gate", descendantTransition, "descendant_dependencies"} {
		if !strings.Contains(joined, fragment) {
			t.Fatalf("warnings %q do not name %q", result.Warnings, fragment)
		}
	}
}

// --- Deferred form (defer-provider-dependency effect) on a generated plan ---

// deferredGeneratedPlan is the live D-79 shape after the operator deferral: the
// architecture output still declares the peer and gate waits; its generated
// code plan keeps the peer wait and carries the gate wait as a descendant one.
// The peer provider is satisfied, so only the gate wait could hold the planner.
func deferredGeneratedPlan(t *testing.T, deferred bool) []models.Task {
	t.Helper()
	peer := providerOpsTask("peer", "architecture-pair", models.TaskStatusMerged)
	peer.Output = []models.OutputEntry{providerOpsOutput()}
	peer.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	peerPlan := providerOpsTask("peer-cp-0", "code-planning-pair", models.TaskStatusMerged)
	peerPlan.ParentTasks = []string{peer.ID}
	arch := providerOpsTask("arch", "architecture-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = append(providerOpsDependency(peer.ID, 0), gateWait(0)...)
	arch.Output = []models.OutputEntry{output}
	arch.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	child := providerOpsTask("arch-cp-0", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	child.ParentTasks = []string{arch.ID}
	child.ProviderDependencies = providerOpsDependency(peer.ID, 0)
	if deferred {
		child = withDescendants(t, child, gateDescendants(0))
	}
	return append(gatePlan(1, false), peer, peerPlan, arch, child)
}

func TestDescendantDependenciesDeferredDeclarationMatchesReviewedOutput(t *testing.T) {
	t.Parallel()
	root, statePath, _ := setupDescendantTest(t, deferredGeneratedPlan(t, true)...)

	// WHEN the planner claims and writes its output.
	if _, err := ClaimTask(root, "arch-cp-0", "code-planner-1"); err != nil {
		t.Fatalf("deferred declaration reported as a generation mismatch or held planning: %v", err)
	}
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "arch-cp-0", AgentID: "code-planner-1", Output: []models.OutputEntry{providerOpsOutput()}}); err != nil {
		t.Fatal(err)
	}

	// THEN the writer-level wait lands on its output.
	if got := mustReadTask(t, statePath, "arch-cp-0").Output[0].ProviderDependencies; !reflect.DeepEqual(got, gateWait(0)) {
		t.Fatalf("deferred wait did not reach the plan's output: %+v", got)
	}
}

func TestDescendantDependenciesRecoveryPreservesDeferredDeclaration(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "claimed"}[claimed], func(t *testing.T) {
			t.Parallel()
			root, statePath, bb := setupDescendantTest(t, deferredGeneratedPlan(t, true)...)
			if claimed {
				if _, err := ClaimTask(root, "arch-cp-0", "code-planner-1"); err != nil {
					t.Fatal(err)
				}
			}
			completeSprintForHandoff(t, bb)

			// WHEN the architecture's transition is re-driven (crash-recovery path).
			if _, err := Proceed(root, "arch", providerOpsTransition); err != nil && !errors.Is(err, errTransitionAlreadyExecuted) {
				t.Fatalf("recovery refused a deferred child: %v", err)
			}

			// THEN the child keeps its own declarations: the deferred wait is not re-imposed.
			child := mustReadTask(t, statePath, "arch-cp-0")
			if !reflect.DeepEqual(child.ProviderDependencies, providerOpsDependency("peer", 0)) {
				t.Fatalf("recovery rewrote provider_dependencies: %+v", child.ProviderDependencies)
			}
			if got := descendantsOf(t, child); !reflect.DeepEqual(got, gateDescendants(0)) {
				t.Fatalf("recovery rewrote descendant_dependencies: %+v", got)
			}
		})
	}
}

func TestDescendantDependenciesDeferredParentOutputIsFullyMaterialized(t *testing.T) {
	t.Parallel()
	root, statePath, _ := setupDescendantTest(t, deferredGeneratedPlan(t, true)...)
	state := readClaimStateForTest(t, statePath)
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	arch := state.FindTask("arch")

	if isTransitionIncomplete(state, arch, providerOpsTransition, resolver) {
		t.Fatal("authorized deferral left the architecture transition incomplete")
	}
	if operationalOutputMayBeConsumed(state, resolver, arch) {
		t.Fatal("authorized deferral left the architecture output live")
	}
	// The gate is held by the child's descendant declaration, not the consumed architecture output.
	before := replacementBytes(t, statePath)
	_, err = Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "arch-cp-0 descendant_dependencies")
	if strings.Contains(err.Error(), "arch output[0]") {
		t.Fatalf("consumed architecture output still holds the gate: %v", err)
	}
}

func TestDescendantDependenciesDroppedDeclarationRemainsAMismatch(t *testing.T) {
	t.Run("claim", func(t *testing.T) {
		t.Parallel()
		root, statePath, _ := setupDescendantTest(t, deferredGeneratedPlan(t, false)...)
		before := replacementBytes(t, statePath)
		_, err := ClaimTask(root, "arch-cp-0", "code-planner-1")
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "generated provider_dependencies", "arch output[0]")
	})
	t.Run("claimed recovery", func(t *testing.T) {
		t.Parallel()
		tasks := deferredGeneratedPlan(t, false)
		child := &tasks[len(tasks)-1]
		child.Status = models.TaskStatusCodePlanning
		*child = startExecuting(*child, "code-planner-1")
		root, statePath, bb := setupDescendantTest(t, tasks...)
		completeSprintForHandoff(t, bb)
		before := replacementBytes(t, statePath)
		_, err := Proceed(root, "arch", providerOpsTransition)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "conflicting or claimed child", "arch-cp-0")
	})
}

// --- Retirement: descendant declarations hold strictly ---

func TestDescendantDependenciesHoldProviderRetirement(t *testing.T) {
	cases := map[string]func(t *testing.T) (models.Task, string){
		"unstarted plan": func(t *testing.T) (models.Task, string) {
			plan := withDescendants(t, providerOpsTask("cp", "code-planning-pair", models.TaskStatusDraftCodingPlan), gateDescendants(0))
			return plan, "cp descendant_dependencies"
		},
		"planning": func(t *testing.T) (models.Task, string) {
			return executingCodePlan(t, "cp"), "cp descendant_dependencies"
		},
		"draft architecture output": func(t *testing.T) (models.Task, string) {
			arch := providerOpsTask("arch", "architecture-pair", models.TaskStatus("ARCHITECTING"))
			arch = startExecuting(arch, "architect-1")
			arch.Output = []models.OutputEntry{withDescendants(t, providerOpsOutput(), gateDescendants(0))}
			return arch, "arch output[0].descendant_dependencies"
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			holder, label := build(t)
			root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), holder)...)
			before := replacementBytes(t, statePath)
			_, err := Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"})
			requireProviderOpsAtomicRefusal(t, statePath, before, err, label)
		})
	}
}

func TestDescendantDependenciesMergedPlanReleasesToOrdinaryRules(t *testing.T) {
	t.Parallel()
	// GIVEN a MERGED code plan whose output carries the applied wait and whose writer is generated, unstarted.
	plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = gateWait(0)
	plan.Output = []models.OutputEntry{output}
	plan.TransitionsExecuted = map[string]bool{descendantTransition: true}
	plan = withDescendants(t, plan, gateDescendants(0))
	writer := providerOpsTask("cp-code-0", "coding-pair", descendantCodingDraft)
	writer.ParentTasks = []string{plan.ID}
	writer.ProviderDependencies = gateWait(0)
	root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), plan, writer)...)

	// WHEN the gate is replanned.
	if _, err := Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"}); err != nil {
		t.Fatalf("terminal plan's descendant declaration still holds: %v", err)
	}

	// THEN the ordinary unstarted-consumer rule governs the writer (ADR-0190).
	if status := mustReadTask(t, statePath, writer.ID).Status; status != models.TaskStatusBlocked {
		t.Fatalf("writer status = %s, want BLOCKED for re-authoring", status)
	}
}

func TestDescendantDependenciesUnexpandedMergedPlanHoldsRetirementAndStaysReplannable(t *testing.T) {
	t.Parallel()
	// GIVEN a MERGED code plan carrying a deferred wait and its applied output wait, with no writers generated yet.
	plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = gateWait(0)
	plan.Output = []models.OutputEntry{output}
	plan = withDescendants(t, plan, gateDescendants(0))
	root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), plan)...)

	// WHEN the gate is replanned, THEN the plan's still-unapplied descendant declaration holds it.
	before := replacementBytes(t, statePath)
	_, err := Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cp descendant_dependencies")

	// AND the plan's ordinary replan recovery stays usable, its successor keeping the wait.
	if _, err := Replan(root, &ReplanInput{TaskID: plan.ID, ChangedBy: "human", Reason: "re-plan the writers"}); err != nil {
		t.Fatalf("consumer replan refused: %v", err)
	}
	if got := descendantsOf(t, mustReadTask(t, statePath, "cp-replan-1")); !reflect.DeepEqual(got, gateDescendants(0)) {
		t.Fatalf("successor descendant_dependencies = %+v, want %+v", got, gateDescendants(0))
	}
	before = replacementBytes(t, statePath)
	_, err = Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cp-replan-1 descendant_dependencies")
}

// A stored record that escaped validation (written raw here) must not reach a
// writer: the hand-off rechecks its owner's placement before consuming it.
func TestDescendantDependenciesHandoffRefusesMisplacedStoredDeclaration(t *testing.T) {
	misplaced := []descendantFixture{{AtTransition: providerOpsTransition, ProviderDependencies: gateWait(0)}}
	t.Run("generation", func(t *testing.T) {
		t.Parallel()
		plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
		plan.Output = []models.OutputEntry{providerOpsOutput()}
		plan = withDescendants(t, plan, misplaced)
		root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, true), plan)...)
		completeSprintForHandoff(t, bb)
		before := replacementBytes(t, statePath)

		_, err := Proceed(root, plan.ID, descendantTransition)

		requireProviderOpsAtomicRefusal(t, statePath, before, err, "task cp", providerOpsTransition, descendantTransition)
		requireNoChild(t, statePath, "cp-code-0")
	})
	t.Run("recovery", func(t *testing.T) {
		t.Parallel()
		plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
		plan.Output = []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}
		plan.TransitionsExecuted = map[string]bool{descendantTransition: true}
		plan = withDescendants(t, plan, misplaced)
		writer := providerOpsTask("cp-code-0", "coding-pair", descendantCodingDraft)
		writer.ParentTasks = []string{plan.ID}
		root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, true), plan, writer)...)
		completeSprintForHandoff(t, bb)
		before := replacementBytes(t, statePath)

		_, err := Proceed(root, plan.ID, descendantTransition)

		requireProviderOpsAtomicRefusal(t, statePath, before, err, "task cp", providerOpsTransition, descendantTransition)
		requireNoChild(t, statePath, "cp-code-1")
	})
}

func TestDescendantDependenciesReplaceTaskReauthorsTheDeclaration(t *testing.T) {
	t.Parallel()
	source := withDescendants(t, providerOpsTask("cp", "code-planning-pair", models.TaskStatusDraftCodingPlan), gateDescendants(0))
	other := providerOpsTask("other", "code-planning-pair", models.TaskStatusMerged)
	other.Output = []models.OutputEntry{providerOpsOutput()}
	root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), other, source)...)
	var replacement AddTaskInput
	payload := `{"id":"cp-v2","role_pair":"code-planning-pair","desc":"Re-authored plan","spec":"README.md","done":"Plan reviewed","scope":"plan","priority":1,
		"descendant_dependencies":[{"at_transition":"code-plan-to-coding","provider_dependencies":[{"provider_task":"other","transition":"code-plan-to-coding","outputs":[0]}]}]}`
	if err := json.Unmarshal([]byte(payload), &replacement); err != nil {
		t.Fatal(err)
	}
	state := readClaimStateForTest(t, statePath)
	input := ReplaceTaskInput{SourceTaskID: source.ID, Reason: "re-author descendant wait", Replacement: replacement, Consumers: []models.DependencyUpdate{}}
	opts := LifecycleRequestOptions{RequestID: "replace-d79", ExpectedTransition: models.TaskTransitionID(state.FindTask(source.ID))}

	if _, err := ReplaceTaskWithAuthorityAndOptions(root, input, *orchestratorAuthority(), opts); err != nil {
		t.Fatal(err)
	}

	want := []descendantFixture{{AtTransition: descendantTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: "other", Transition: descendantTransition, Outputs: []int{0}}}}}
	if got := descendantsOf(t, mustReadTask(t, statePath, "cp-v2")); !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement descendant_dependencies = %+v, want %+v", got, want)
	}
	if _, err := Replan(root, &ReplanInput{TaskID: "gate", ChangedBy: "human"}); err != nil {
		t.Fatalf("replaced holder still holds the gate: %v", err)
	}
}

// --- Kind: a descendant wait never rides on a deduplicable output ---

func kindIncumbent(id, rolePair string, status models.TaskStatus, claimed bool) models.Task {
	incumbent := providerOpsTask(id, rolePair, status)
	incumbent.Kind = taskkind.PreCommitBootstrap
	if claimed {
		incumbent = startExecuting(incumbent, "coder-1")
	}
	return incumbent
}

func TestDescendantDependenciesRefuseKindAtAuthoring(t *testing.T) {
	t.Run("declared", func(t *testing.T) {
		t.Parallel()
		arch := providerOpsTask("arch", "architecture-pair", models.TaskStatus("ARCHITECTING"))
		arch = startExecuting(arch, "architect-1")
		root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), arch)...)
		output := withDescendants(t, providerOpsOutput(), gateDescendants(0))
		output.Kind = taskkind.PreCommitBootstrap
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: arch.ID, AgentID: "architect-1", Output: []models.OutputEntry{output}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "output[0]", "kind", "descendant")
	})
	t.Run("received", func(t *testing.T) {
		t.Parallel()
		plan := executingCodePlan(t, "cp")
		root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), plan)...)
		output := providerOpsOutput()
		output.Kind = taskkind.PreCommitBootstrap
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.ID, AgentID: "code-planner-1", Output: []models.OutputEntry{providerOpsOutput(), output}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "output[1]", "kind", "descendant")
	})
}

func TestDescendantDependenciesRefuseKindAtHandoff(t *testing.T) {
	type handoff struct {
		owner      models.Task
		transition string
		incumbent  models.Task
		child      string
	}
	build := map[string]func(t *testing.T, claimed bool) handoff{
		"declared": func(t *testing.T, claimed bool) handoff {
			arch := providerOpsTask("arch", "architecture-pair", models.TaskStatusMerged)
			output := withDescendants(t, providerOpsOutput(), gateDescendants(0))
			output.Kind = taskkind.PreCommitBootstrap
			arch.Output = []models.OutputEntry{output}
			return handoff{arch, providerOpsTransition, kindIncumbent("incumbent", "code-planning-pair", models.TaskStatusDraftCodingPlan, false), "arch-cp-0"}
		},
		"received": func(t *testing.T, claimed bool) handoff {
			plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
			output := providerOpsOutput()
			output.Kind = taskkind.PreCommitBootstrap
			output.ProviderDependencies = gateWait(0)
			plan.Output = []models.OutputEntry{output}
			plan = withDescendants(t, plan, gateDescendants(0))
			status := descendantCodingDraft
			if claimed {
				status = models.TaskStatus("IMPLEMENTING_CODE")
			}
			return handoff{plan, descendantTransition, kindIncumbent("incumbent", "coding-pair", status, claimed), "cp-code-0"}
		},
	}
	for name, buildHandoff := range build {
		for _, recovery := range []bool{false, true} {
			for _, claimed := range []bool{false, true} {
				if claimed && name == "declared" {
					continue
				}
				label := name + map[bool]string{false: "/generation", true: "/recovery"}[recovery] + map[bool]string{false: "", true: "/claimed incumbent"}[claimed]
				t.Run(label, func(t *testing.T) {
					t.Parallel()
					h := buildHandoff(t, claimed)
					if recovery {
						h.owner.TransitionsExecuted = map[string]bool{h.transition: true}
					}
					root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, false), h.owner, h.incumbent)...)
					completeSprintForHandoff(t, bb)
					before := replacementBytes(t, statePath)

					_, err := Proceed(root, h.owner.ID, h.transition)

					requireProviderOpsAtomicRefusal(t, statePath, before, err, "output[0]", "kind", "descendant")
					requireNoChild(t, statePath, h.child)
					incumbent := mustReadTask(t, statePath, h.incumbent.ID)
					if len(incumbent.ProviderDependencies) != 0 || len(incumbent.DependsOn) != 0 {
						t.Fatalf("hand-off widened the Kind incumbent: %+v", incumbent)
					}
				})
			}
		}
	}
}

// --- Topology: one intermediate plan on a single per-subtask path ---

func TestDescendantDependenciesRequireSinglePath(t *testing.T) {
	t.Run("fan-out producer", func(t *testing.T) {
		t.Parallel()
		arch := providerOpsTask("arch", "architecture-pair", models.TaskStatus("ARCHITECTING"))
		arch = startExecuting(arch, "architect-1")
		root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), arch)...)
		testhelpers.SetupPipelineConfigBytes(t, root, fanOutPipeline(t))
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: arch.ID, AgentID: "architect-1", Output: []models.OutputEntry{withDescendants(t, providerOpsOutput(), gateDescendants(0))}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "descendant_dependencies", "single per-subtask path", "architecture-pair")
	})
	t.Run("no grandchild transition", func(t *testing.T) {
		t.Parallel()
		plan := startExecuting(providerOpsTask("cp", "code-planning-pair", models.TaskStatusCodePlanning), "code-planner-1")
		root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), plan)...)
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.ID, AgentID: "code-planner-1", Output: []models.OutputEntry{withDescendants(t, providerOpsOutput(), gateDescendants(0))}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "descendant_dependencies", "single per-subtask path", "coding-pair")
	})
	for name, atTransition := range map[string]string{"task level, wrong transition": providerOpsTransition, "task level, sole transition": descendantTransition} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, statePath, _ := setupDescendantTest(t, gatePlan(1, false)...)
			var input AddTaskInput
			payload := `{"id":"cp","role_pair":"code-planning-pair","desc":"Plan","spec":"README.md","done":"Plan reviewed","scope":"plan","priority":1,
				"descendant_dependencies":[{"at_transition":"` + atTransition + `","provider_dependencies":[{"provider_task":"gate","transition":"code-plan-to-coding","outputs":[0]}]}]}`
			if err := json.Unmarshal([]byte(payload), &input); err != nil {
				t.Fatal(err)
			}
			before := replacementBytes(t, statePath)
			_, err := AddTask(statePath, paths.New(root).LogPath(), &input, "orchestrator-1")
			if atTransition == providerOpsTransition {
				requireProviderOpsAtomicRefusal(t, statePath, before, err, "descendant_dependencies", providerOpsTransition)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := descendantsOf(t, mustReadTask(t, statePath, "cp")); !reflect.DeepEqual(got, gateDescendants(0)) {
				t.Fatalf("add-task descendant_dependencies = %+v", got)
			}
		})
	}
}

// fanOutPipeline adds a second per-subtask transition out of architecture-pair.
func fanOutPipeline(t *testing.T) []byte {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupPipelineConfig(t, root)
	raw := replacementBytes(t, filepath.Join(root, paths.ProjectDirName(), "pipeline.yaml"))
	anchor := "\n    - name: architecture-to-code-plan\n"
	if !strings.Contains(string(raw), anchor) {
		t.Fatalf("pipeline fixture anchor %q not found", anchor)
	}
	extra := "\n    - name: architecture-to-coding\n      task-slug: direct\n      from: architecture-subpipeline.architecture-pair.approved\n      to: coding-subpipeline.coding-pair.initial\n      trigger: manual\n      cardinality: per-subtask"
	fanOut := []byte(strings.Replace(string(raw), anchor, extra+anchor, 1))
	if _, err := pipeline.LoadFromBytes(fanOut); err != nil {
		t.Fatalf("fan-out pipeline fixture is invalid: %v", err)
	}
	return fanOut
}

// --- Applied waits stay ordinary declarations: references, cycles, admission ---

func TestDescendantDependenciesRefuseInvalidReferences(t *testing.T) {
	cases := map[string][]descendantFixture{
		"missing provider":    {{AtTransition: descendantTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: "missing", Transition: descendantTransition, Outputs: []int{0}}}}},
		"foreign transition":  {{AtTransition: descendantTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: "gate", Transition: providerOpsTransition, Outputs: []int{0}}}}},
		"output out of range": gateDescendants(3),
	}
	for name, deps := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			arch := startExecuting(providerOpsTask("arch", "architecture-pair", models.TaskStatus("ARCHITECTING")), "architect-1")
			root, statePath, _ := setupDescendantTest(t, append(gatePlan(1, false), arch)...)
			before := replacementBytes(t, statePath)
			err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: arch.ID, AgentID: "architect-1", Output: []models.OutputEntry{withDescendants(t, providerOpsOutput(), deps)}})
			requireProviderOpsAtomicRefusal(t, statePath, before, err, "arch output[0].descendant_dependencies")
		})
	}
}

func TestDescendantDependenciesRefuseCycleWhenApplied(t *testing.T) {
	t.Parallel()
	// GIVEN a gate plan whose own writer waits on the consuming plan's writer.
	gate := providerOpsTask("gate", "code-planning-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = []models.ProviderDependency{{ProviderTask: "cp", Transition: descendantTransition, Outputs: []int{0}}}
	gate.Output = []models.OutputEntry{output}
	plan := executingCodePlan(t, "cp")
	root, statePath, _ := setupDescendantTest(t, gate, plan)
	before := replacementBytes(t, statePath)

	// WHEN the consuming plan writes output that receives the deferred wait on the gate's writer.
	err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.ID, AgentID: "code-planner-1", Output: []models.OutputEntry{providerOpsOutput()}})

	// THEN the closed cycle is refused and no output is published.
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "cycle", "cp-code-0", "gate-code-0")
}

func TestDescendantDependenciesHoldWriterUntilSelectedWriterMerges(t *testing.T) {
	t.Parallel()
	// GIVEN a code plan whose output received the deferred wait, and the gate writer in DRAFT_CODE.
	plan := providerOpsTask("cp", "code-planning-pair", models.TaskStatusMerged)
	output := providerOpsOutput()
	output.ProviderDependencies = gateWait(0)
	plan.Output = []models.OutputEntry{output}
	plan = withDescendants(t, plan, gateDescendants(0))
	root, statePath, bb := setupDescendantTest(t, append(gatePlan(1, true), plan)...)
	completeSprintForHandoff(t, bb)
	if _, err := Proceed(root, plan.ID, descendantTransition); err != nil {
		t.Fatal(err)
	}

	// WHEN a coder tries the generated writer before the gate writer merges.
	_, err := ClaimTask(root, "cp-code-0", "coder-1")

	// THEN coding order holds.
	if err == nil || !strings.Contains(err.Error(), "gate-code-0") {
		t.Fatalf("writer claimed before the selected writer merged: %v", err)
	}
	if err := bb.Modify(func(s *models.State) error {
		s.FindTask("gate-code-0").Status = models.TaskStatusMerged
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimTask(root, "cp-code-0", "coder-1"); err != nil {
		t.Fatalf("merged selected writer did not release the ordered writer: %v", err)
	}
	if assigned := mustReadTask(t, statePath, "cp-code-0").AssignedTo; assigned == nil || *assigned != "coder-1" {
		t.Fatalf("ordered writer was not claimed: %v", assigned)
	}
}
