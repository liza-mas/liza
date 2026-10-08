package ops

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/taskkind"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// D-80: a correction inserted into an ordered shared-file writer chain holds
// every later unstarted writer through a provider reservation: the writer
// waits for every child the correction generates, not only for its plan.

var d80Authority = models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}

// reservation is the persisted shape of one provider reservation.
func reservation(provider, transition string) []map[string]any {
	return []map[string]any{{"provider_task": provider, "transition": transition}}
}

// setTaskYAMLField writes key: value onto one task of the state file, for
// fields the fixture structs do not carry.
func setTaskYAMLField(t *testing.T, statePath, taskID, key string, value any) {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	tasks := yamlMappingValue(doc.Content[0], "tasks")
	if tasks == nil {
		t.Fatal("state file has no tasks")
	}
	for _, item := range tasks.Content {
		if id := yamlMappingValue(item, "id"); id == nil || id.Value != taskID {
			continue
		}
		var valueNode yaml.Node
		if err := valueNode.Encode(value); err != nil {
			t.Fatal(err)
		}
		item.Content = append(item.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &valueNode)
		out, err := yaml.Marshal(&doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(statePath, out, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("task %s not in state file", taskID)
}

// taskYAMLField reads key of one task from the state file, nil when absent.
func taskYAMLField(t *testing.T, statePath, taskID, key string) any {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Tasks []map[string]any `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, task := range raw.Tasks {
		if task["id"] == taskID {
			return task[key]
		}
	}
	t.Fatalf("task %s not in state file", taskID)
	return nil
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// decodeAddTaskInput builds an add-task payload from JSON, as the CLI does,
// so payload fields are exercised through the same decoding.
func decodeAddTaskInput(t *testing.T, payload string) AddTaskInput {
	t.Helper()
	var input AddTaskInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

const d80CorrectionPayload = `{"id":"corr2","role_pair":"architecture-pair","desc":"correction writer","spec":"README.md","done":"correction reviewed","scope":"contract","priority":1,"reserve_successors":["vb-cp-0"]}`

// chainFixture is the D-80 shape: vb-cp-0 (Unit 5) is the unstarted generated
// child of MERGED plan vb, and corr is a correction architecture in its
// initial status whose generated children later writers must wait for.
func chainFixture(t *testing.T, extra ...models.Task) (string, string) {
	t.Helper()
	vb, unit5 := transitionedLineagePlan("vb")
	corr := providerOpsTask("corr", "architecture-pair", models.TaskStatus("DRAFT_ARCHITECTURE"))
	root, statePath, bb := setupProviderOpsTest(t, append([]models.Task{vb, unit5, corr}, extra...)...)
	if err := bb.Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusCompleted; return nil }); err != nil {
		t.Fatal(err)
	}
	return root, statePath
}

// reservedChainFixture adds Unit 5's reservation on corr's generated children.
func reservedChainFixture(t *testing.T, extra ...models.Task) (string, string) {
	t.Helper()
	root, statePath := chainFixture(t, extra...)
	setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("corr", providerOpsTransition))
	return root, statePath
}

func requireClaimRefusedNaming(t *testing.T, root, statePath, taskID, agentID string, fragments ...string) {
	t.Helper()
	before := replacementBytes(t, statePath)
	_, err := ClaimTask(root, taskID, agentID)
	requireProviderOpsAtomicRefusal(t, statePath, before, err, fragments...)
}

// mergeTask simulates task id passing review and merging: it keeps the whole
// task, including declarations, caps and reservations, sets only the merge
// result fields, releases its assigned agent, then applies mutate.
func mergeTask(t *testing.T, statePath, id string, mutate func(*models.Task)) {
	t.Helper()
	if err := db.New(statePath).Modify(func(s *models.State) error {
		task := s.FindTask(id)
		if task == nil {
			t.Fatalf("task %s missing", id)
		}
		if task.AssignedTo != nil {
			agent := s.Agents[*task.AssignedTo]
			agent.Status, agent.CurrentTask = models.AgentStatusWaiting, nil
			s.Agents[*task.AssignedTo] = agent
		}
		result := providerOpsTask(task.ID, task.RolePair, models.TaskStatusMerged)
		task.Status = models.TaskStatusMerged
		task.ApprovedBy, task.MergeCommit = result.ApprovedBy, result.MergeCommit
		task.HandoffEvents = append(task.HandoffEvents, result.HandoffEvents...)
		task.AssignedTo, task.LeaseExpires, task.Worktree = nil, nil, nil
		task.ReviewingBy, task.ReviewLeaseExpires = nil, nil
		if mutate != nil {
			mutate(task)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func twoOutputs(task *models.Task) {
	task.Output = []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}
}

// R1/R2: the reservation survives mutations, leaves its generated holder's
// parent complete, and holds the holder until every provider child merges.
func TestReservationHoldsWriterUntilEveryProviderChildMerges_D80(t *testing.T) {
	t.Parallel()
	root, statePath := reservedChainFixture(t)

	// GIVEN an unrelated mutation, the reservation is still persisted
	if err := db.New(statePath).Modify(func(s *models.State) error { s.FindTask("corr").Priority = 2; return nil }); err != nil {
		t.Fatal(err)
	}
	if taskYAMLField(t, statePath, "vb-cp-0", "provider_reservations") == nil {
		t.Fatal("a state mutation dropped the reservation")
	}
	// AND the holder's generating parent stays complete
	state := readClaimStateForTest(t, statePath)
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if isTransitionIncomplete(state, state.FindTask("vb"), providerOpsTransition, resolver) {
		t.Fatal("a reservation on a generated child made its parent's transition look incomplete")
	}

	// WHEN/THEN Unit 5 cannot start while the correction is unplanned
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr")
	// nor after the correction merges, before its hand-off
	mergeTask(t, statePath, "corr", twoOutputs)
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr")
	// nor while either generated correction child is unmerged
	if _, err := Proceed(root, "corr", providerOpsTransition); err != nil {
		t.Fatal(err)
	}
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr-cp-0")
	mergeTask(t, statePath, "corr-cp-0", nil)
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr-cp-1")
	mergeTask(t, statePath, "corr-cp-1", nil)
	if _, err := ClaimTask(root, "vb-cp-0", "code-planner-1"); err != nil {
		t.Fatalf("every correction child merged, but Unit 5 stays held: %v", err)
	}
}

// R10: a reservation that cannot name generated children fails closed; a
// valid MERGED provider whose hand-off generated nothing satisfies it.
func TestReservationResolutionFailsClosed_D80(t *testing.T) {
	t.Parallel()
	for name, transition := range map[string]string{
		"unknown transition":     "no-such-transition",
		"many-to-one":            "us-to-coding",
		"wrong source role pair": "code-plan-to-coding",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, statePath := chainFixture(t)
			setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("corr", transition))
			requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr")
		})
	}
	t.Run("missing provider", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("no-such-task", providerOpsTransition))
		requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "no-such-task")
	})
	t.Run("zero-output provider satisfies (control)", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		mergeTask(t, statePath, "corr", func(task *models.Task) {
			task.Output = nil
			task.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
		})
		if _, err := ClaimTask(root, "vb-cp-0", "code-planner-1"); err != nil {
			t.Fatalf("a hand-off that generated nothing still holds Unit 5: %v", err)
		}
	})
}

// R5′: replacing the reserved provider moves the hold to its successor in the
// same transaction; a permanent retirement is refused naming the holder.
func TestReservationFollowsProviderReplacement_D80(t *testing.T) {
	t.Parallel()
	root, statePath := reservedChainFixture(t)
	replacement := AddTaskInput{ID: "corr-r1", RolePair: "architecture-pair", Description: "re-authored correction", SpecRef: "README.md", DoneWhen: "correction reviewed", Scope: "contract", Priority: 1}

	// WHEN the correction is replaced by a same-pair successor
	if err := replaceTaskForTest(root, statePath, "corr", replacement, "scope re-authored"); err != nil {
		t.Fatalf("replace-task of the reserved correction: %v", err)
	}

	// THEN Unit 5 waits for the successor in the committed state
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr-r1")
	// AND cancelling the successor while it holds Unit 5 is refused
	before := replacementBytes(t, statePath)
	_, err := CancelTask(root, "corr-r1", "correction withdrawn", "orchestrator-1")
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "vb-cp-0", "provider_reservations")
}

// R6: a replaced reserved writer keeps its placement; a supersession that
// would drop it is refused.
func TestReservedWriterLineageKeepsPlacement_D80(t *testing.T) {
	t.Parallel()
	t.Run("replace-task carries the reservation", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		if err := replaceCodePlanForTest(root, statePath, "vb-cp-0", "vb-cp-0-r1", "scope re-authored"); err != nil {
			t.Fatalf("replace-task of the reserved writer: %v", err)
		}
		if taskYAMLField(t, statePath, "vb-cp-0-r1", "provider_reservations") == nil {
			t.Fatal("the replacement lost the writer's placement")
		}
		requireClaimRefusedNaming(t, root, statePath, "vb-cp-0-r1", "code-planner-1", "corr")
	})
	t.Run("supersede-task without carrying successor is refused", func(t *testing.T) {
		t.Parallel()
		other := providerOpsTask("other", "code-planning-pair", models.TaskStatusDraftCodingPlan)
		root, statePath := reservedChainFixture(t, other)
		before := replacementBytes(t, statePath)
		_, err := SupersedeTask(root, "vb-cp-0", []string{"other"}, "replaced elsewhere", "orchestrator-1")
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "provider_reservations")
	})
}

// C2/R9/R13: commissioning installs the successors' reservations in the same
// transaction as the correction, or creates nothing.
func TestCommissioningReservesSuccessorsAtomically_D80(t *testing.T) {
	t.Parallel()
	t.Run("add-task reserves the successor", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		input := decodeAddTaskInput(t, d80CorrectionPayload)
		if _, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority); err != nil {
			t.Fatalf("commissioning refused: %v", err)
		}
		requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", "corr2")
	})
	t.Run("executing successor refuses the whole commission", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		if _, err := ClaimTask(root, "vb-cp-0", "code-planner-1"); err != nil {
			t.Fatal(err)
		}
		before := replacementBytes(t, statePath)
		input := decodeAddTaskInput(t, d80CorrectionPayload)
		_, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "vb-cp-0")
	})
	t.Run("batch carrying a reservation is refused before any item", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		before := replacementBytes(t, statePath)
		plain := decodeAddTaskInput(t, `{"id":"plain","role_pair":"architecture-pair","desc":"unrelated","spec":"README.md","done":"reviewed","scope":"other","priority":1}`)
		input := AddTasksInput{Tasks: []AddTaskInput{plain, decodeAddTaskInput(t, d80CorrectionPayload)}}
		_, err := AddTasksWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "reserve_successors")
	})
}

// C4/C5: a placed writer selected by output index is capped, and every
// selection covers its whole cap.
func TestPlacedWriterCardinality_D80(t *testing.T) {
	t.Parallel()
	t.Run("capped placed writer cannot author an extra output", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		if err := db.New(statePath).Modify(func(s *models.State) error {
			corr := s.FindTask("corr")
			corr.Status = models.TaskStatus("ARCHITECTING")
			corr.AssignedTo = testhelpers.StringPtr("architect-1")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("corr", providerOpsTransition))
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "max_outputs")
	})
	selector := `{"id":"q","role_pair":"code-planning-pair","desc":"chained writer","spec":"README.md","done":"reviewed","scope":"chained","priority":1,"provider_dependencies":[{"provider_task":"corr","transition":"` + providerOpsTransition + `","outputs":[0]}]}`
	t.Run("selection of an uncapped placed writer is refused", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		before := replacementBytes(t, statePath)
		input := decodeAddTaskInput(t, selector)
		_, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "corr", "max_outputs")
	})
	t.Run("partial selection of a capped placed writer is refused", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 2)
		before := replacementBytes(t, statePath)
		input := decodeAddTaskInput(t, selector)
		_, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "corr", "max_outputs")
	})
	t.Run("covering selection of a capped placed writer is accepted (control)", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		input := decodeAddTaskInput(t, selector)
		if _, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority); err != nil {
			t.Fatalf("covering selection refused: %v", err)
		}
	})
	t.Run("partial selection of an unplaced provider stays legal (control)", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		input := decodeAddTaskInput(t, selector)
		if _, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority); err != nil {
			t.Fatalf("ordinary partial selection refused: %v", err)
		}
	})
}

// R16: the cap survives a same-pair replacement of the placed writer, and a
// replacement cannot widen it under an existing chained selection.
func TestPlacedWriterCapFollowsReplacement_D80(t *testing.T) {
	t.Parallel()
	chained := providerOpsTask("q", "code-planning-pair", models.TaskStatusDraftCodingPlan)
	chained.ProviderDependencies = providerOpsDependency("corr", 0)
	replacement := `{"id":"corr-r1","role_pair":"architecture-pair","desc":"re-authored correction","spec":"README.md","done":"correction reviewed","scope":"contract","priority":1`
	t.Run("omitted cap is inherited", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t, chained)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		if err := replaceTaskForTest(root, statePath, "corr", decodeAddTaskInput(t, replacement+`}`), "scope re-authored"); err != nil {
			t.Fatalf("replace-task of the capped correction: %v", err)
		}
		if got := taskYAMLField(t, statePath, "corr-r1", "max_outputs"); got != 1 {
			t.Fatalf("successor max_outputs = %v, want inherited 1", got)
		}
	})
	t.Run("wider cap under a chained selection is refused", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t, chained)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		before := replacementBytes(t, statePath)
		err := replaceTaskForTest(root, statePath, "corr", decodeAddTaskInput(t, replacement+`,"max_outputs":2}`), "scope re-authored")
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "max_outputs")
	})
}

// R5′/R16: replanning an unexpanded reserved writer moves the hold and the
// cap to its successor in the same transaction.
func TestReservationAndCapFollowProviderReplan_D80(t *testing.T) {
	t.Parallel()
	root, statePath := reservedChainFixture(t)
	mergeTask(t, statePath, "corr", func(task *models.Task) { task.Output = []models.OutputEntry{providerOpsOutput()} })
	setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
	if err := db.New(statePath).Modify(func(s *models.State) error { s.Sprint.Status = models.SprintStatusInProgress; return nil }); err != nil {
		t.Fatal(err)
	}

	// WHEN the MERGED, unexpanded correction is replanned
	result, err := Replan(root, &ReplanInput{TaskID: "corr", ChangedBy: "human"})
	if err != nil {
		t.Fatalf("replan of the reserved correction: %v", err)
	}

	// THEN Unit 5 waits for the successor and the successor keeps the cap
	requireClaimRefusedNaming(t, root, statePath, "vb-cp-0", "code-planner-1", result.NewTaskID)
	if got := taskYAMLField(t, statePath, result.NewTaskID, "max_outputs"); got != 1 {
		t.Fatalf("replan successor max_outputs = %v, want 1", got)
	}
}

// R9: one ineligible successor refuses the whole commission.
func TestCommissioningRefusesWhenAnySuccessorIsIneligible_D80(t *testing.T) {
	t.Parallel()
	running := providerOpsTask("running", "code-planning-pair", models.TaskStatusCodePlanning)
	running.AssignedTo = testhelpers.StringPtr("code-planner-1")
	root, statePath := chainFixture(t, running)
	before := replacementBytes(t, statePath)
	input := decodeAddTaskInput(t, `{"id":"corr2","role_pair":"architecture-pair","desc":"correction writer","spec":"README.md","done":"correction reviewed","scope":"contract","priority":1,"reserve_successors":["vb-cp-0","running"]}`)
	_, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "running")
}

// R17/C5: selections evolve under the invariant: authored output cannot
// select part of a capped placed writer, a full selection can, and a capped
// but unplaced provider keeps ordinary partial-selection legality.
func TestPlacedWriterSelectionEvolution_D80(t *testing.T) {
	t.Parallel()
	author := func(t *testing.T, outputs ...int) (string, string, error) {
		t.Helper()
		owner := providerOpsTask("owner", "architecture-pair", models.TaskStatus("ARCHITECTING"))
		owner.AssignedTo = testhelpers.StringPtr("architect-1")
		root, statePath := reservedChainFixture(t, owner)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 2)
		output := providerOpsOutput()
		output.ProviderDependencies = providerOpsDependency("corr", outputs...)
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "owner", AgentID: "architect-1", Output: []models.OutputEntry{output}})
		if err != nil && string(before) != string(replacementBytes(t, statePath)) {
			t.Fatal("refused output changed state")
		}
		return root, statePath, err
	}
	t.Run("partial selection in authored output is refused", func(t *testing.T) {
		t.Parallel()
		_, _, err := author(t, 0)
		if err == nil {
			t.Fatal("output selecting part of capped placed corr was accepted")
		}
	})
	t.Run("full selection of cap 2 is accepted (control)", func(t *testing.T) {
		t.Parallel()
		if _, _, err := author(t, 0, 1); err != nil {
			t.Fatalf("covering selection refused: %v", err)
		}
	})
	t.Run("capped unplaced provider keeps partial selections (control)", func(t *testing.T) {
		t.Parallel()
		root, statePath := chainFixture(t)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 2)
		input := decodeAddTaskInput(t, `{"id":"q","role_pair":"code-planning-pair","desc":"chained writer","spec":"README.md","done":"reviewed","scope":"chained","priority":1,"provider_dependencies":[{"provider_task":"corr","transition":"`+providerOpsTransition+`","outputs":[0]}]}`)
		if _, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority); err != nil {
			t.Fatalf("ordinary partial selection refused: %v", err)
		}
	})
}

// d65Commission is the single add-task commissioning a correction code plan
// placed after Unit 4 (u4-code-0, selected through its plan u4), after the
// earlier corrections it lists, and before Unit 5 (vb-code-0).
func d65Commission(t *testing.T, id string, maxOutputs int, after ...string) AddTaskInput {
	t.Helper()
	waits := []map[string]any{{"provider_task": "u4", "transition": descendantTransition, "outputs": []int{0}}}
	for _, earlier := range after {
		waits = append(waits, map[string]any{"provider_task": earlier, "transition": descendantTransition, "outputs": []int{0}})
	}
	fields := map[string]any{
		"id": id, "role_pair": "code-planning-pair", "desc": "corrective writer " + id, "spec": "README.md",
		"done": "one reviewed coding output", "scope": id, "priority": 1,
		"reserve_successors":      []string{"vb-code-0"},
		"descendant_dependencies": []map[string]any{{"at_transition": descendantTransition, "provider_dependencies": waits}},
	}
	if maxOutputs > 0 {
		fields["max_outputs"] = maxOutputs
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return decodeAddTaskInput(t, string(payload))
}

// codingChain is Unit 4 (u4-code-0) and Unit 5 (vb-code-0, after Unit 4):
// both unstarted coding children of MERGED, expanded code plans.
func codingChain() []models.Task {
	var tasks []models.Task
	for _, id := range []string{"u4", "vb"} {
		plan := providerOpsTask(id, "code-planning-pair", models.TaskStatusMerged)
		plan.Output = []models.OutputEntry{providerOpsOutput()}
		plan.TransitionsExecuted = map[string]bool{descendantTransition: true}
		code := providerOpsTask(id+"-code-0", "coding-pair", descendantCodingDraft)
		code.ParentTasks = []string{id}
		if id == "vb" {
			code.DependsOn = []string{"u4-code-0"}
		}
		tasks = append(tasks, plan, code)
	}
	return tasks
}

// R7′: the D65 chain Unit 4 -> falsy -> DC-E1 -> Unit 5, commissioned through
// add-task and driven through planning, hand-off and merges. A claim of each
// later writer is attempted at every committed boundary.
func TestD65CorrectionChainIsSerialized_D80(t *testing.T) {
	t.Parallel()
	root, statePath, bb := setupProviderOpsTest(t, codingChain()...)
	completeSprintForHandoff(t, bb)
	if err := bb.Modify(func(s *models.State) error {
		s.Agents["coder-2"] = testhelpers.RegisteredTestAgent("coder")
		s.Agents["code-planner-2"] = testhelpers.RegisteredTestAgent("code-planner")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	logPath := paths.New(root).LogPath()
	unit5Held := func(blocker string) {
		t.Helper()
		requireClaimRefusedNaming(t, root, statePath, "vb-code-0", "coder-2", blocker)
	}

	// Commission both corrections, each in one add-task.
	falsy := d65Commission(t, "falsy", 1)
	if _, err := AddTaskWithAuthority(statePath, logPath, &falsy, d80Authority); err != nil {
		t.Fatalf("commission falsy: %v", err)
	}
	unit5Held("u4-code-0")
	dce1 := d65Commission(t, "dce1", 1, "falsy")
	if _, err := AddTaskWithAuthority(statePath, logPath, &dce1, d80Authority); err != nil {
		t.Fatalf("commission dce1: %v", err)
	}
	unit5Held("u4-code-0")

	// Unit 4 merges: Unit 5 now waits only for the corrections.
	mergeTask(t, statePath, "u4-code-0", nil)
	unit5Held("falsy")

	// Both corrections plan in parallel; the cap refuses a second writer.
	for _, plan := range []struct{ id, agent string }{{"falsy", "code-planner-1"}, {"dce1", "code-planner-2"}} {
		if _, err := ClaimTask(root, plan.id, plan.agent); err != nil {
			t.Fatalf("correction planner %s held: %v", plan.id, err)
		}
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.id, AgentID: plan.agent, Output: []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "max_outputs")
		if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: plan.id, AgentID: plan.agent, Output: []models.OutputEntry{providerOpsOutput()}}); err != nil {
			t.Fatalf("%s output refused: %v", plan.id, err)
		}
		unit5Held(plan.id)
	}

	// Plans merge and hand off; coding writers are generated with their waits.
	for _, id := range []string{"falsy", "dce1"} {
		mergeTask(t, statePath, id, nil)
		unit5Held(id)
		if _, err := Proceed(root, id, descendantTransition); err != nil {
			t.Fatalf("hand-off of %s: %v", id, err)
		}
		unit5Held(id + "-code-0")
	}
	requireClaimRefusedNaming(t, root, statePath, "dce1-code-0", "coder-1", "falsy-code-0")

	// falsy coding merges: DC-E1 coding may start; Unit 5 still waits for it.
	mergeTask(t, statePath, "falsy-code-0", nil)
	unit5Held("dce1-code-0")
	if _, err := ClaimTask(root, "dce1-code-0", "coder-1"); err != nil {
		t.Fatalf("DC-E1 coding held after falsy merged: %v", err)
	}
	unit5Held("dce1-code-0")

	// DC-E1 coding merges: Unit 5 is finally admitted.
	mergeTask(t, statePath, "dce1-code-0", nil)
	if _, err := ClaimTask(root, "vb-code-0", "coder-2"); err != nil {
		t.Fatalf("Unit 5 held after both corrections merged: %v", err)
	}
}

// R12′: a chained writer's descendant selection of a placed writer must
// cover the placed writer's cap.
func TestChainedDescendantSelectionCoversPlacedWriter_D80(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T, falsyCap int) (string, string) {
		t.Helper()
		root, statePath, _ := setupProviderOpsTest(t, codingChain()...)
		falsy := d65Commission(t, "falsy", falsyCap)
		if _, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &falsy, d80Authority); err != nil {
			t.Fatalf("commission falsy: %v", err)
		}
		return root, statePath
	}
	commission := func(t *testing.T, root, statePath string, outputs ...int) error {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"id": "dce1", "role_pair": "code-planning-pair", "desc": "chained writer", "spec": "README.md", "done": "reviewed", "scope": "dce1", "priority": 1,
			"descendant_dependencies": []map[string]any{{"at_transition": descendantTransition, "provider_dependencies": []map[string]any{{"provider_task": "falsy", "transition": descendantTransition, "outputs": outputs}}}},
		})
		input := decodeAddTaskInput(t, string(payload))
		_, err := AddTaskWithAuthority(statePath, paths.New(root).LogPath(), &input, d80Authority)
		return err
	}
	for name, tc := range map[string]struct {
		cap     int
		outputs []int
		refused bool
	}{
		"uncapped placed writer": {0, []int{0}, true},
		"partial of cap 2":       {2, []int{0}, true},
		"full of cap 2":          {2, []int{0, 1}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, statePath := setup(t, tc.cap)
			before := replacementBytes(t, statePath)
			err := commission(t, root, statePath, tc.outputs...)
			if tc.refused {
				requireProviderOpsAtomicRefusal(t, statePath, before, err, "falsy", "max_outputs")
			} else if err != nil {
				t.Fatalf("covering descendant selection refused: %v", err)
			}
		})
	}
}

// R4: a reserved provider cannot author a Kind output: deduplication could map
// it to a foreign incumbent, so the reservation could not name the child it
// waits for.
func TestReservedProviderRefusesKindOutput_D80(t *testing.T) {
	t.Parallel()
	vb, unit5 := transitionedLineagePlan("vb")
	corr := startExecuting(providerOpsTask("corr", "architecture-pair", models.TaskStatus("ARCHITECTING")), "architect-1")
	root, statePath, _ := setupDescendantTest(t, vb, unit5, corr)
	setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("corr", providerOpsTransition))
	output := providerOpsOutput()
	output.Kind = taskkind.PreCommitBootstrap

	before := replacementBytes(t, statePath)
	err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{output}})
	requireProviderOpsAtomicRefusal(t, statePath, before, err, "kind", "vb-cp-0")

	// Control: the same output without Kind is accepted.
	output.Kind = ""
	if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{output}}); err != nil {
		t.Fatalf("plain output of a reserved provider refused: %v", err)
	}
}

// replace-task builds its replacement from AddTaskInput but has no
// commissioning transaction for successors: the field must be refused, not
// silently dropped.
func TestReplaceTaskRefusesReserveSuccessors_D80(t *testing.T) {
	err := validateReplaceTaskInput(ReplaceTaskInput{
		SourceTaskID: "corr",
		Reason:       "narrower repair cannot work",
		Replacement:  AddTaskInput{ID: "corr-2", ReserveSuccessors: []string{"vb-cp-0"}},
	})
	var precondition *PreconditionError
	if !errors.As(err, &precondition) || !strings.Contains(err.Error(), "reserve_successors") {
		t.Fatalf("replace-task accepted reserve_successors: %v", err)
	}
}

// C6: max_outputs binds output authoring whether or not any declaration or
// reservation exists, including after the last reservation is released.
func TestOutputCapBindsWithoutDeclarations_D80(t *testing.T) {
	t.Parallel()
	t.Run("standalone cap", func(t *testing.T) {
		t.Parallel()
		corr := startExecuting(providerOpsTask("corr", "architecture-pair", models.TaskStatus("ARCHITECTING")), "architect-1")
		root, statePath, _ := setupDescendantTest(t, corr)
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "max_outputs")
		// Control: output within the cap is accepted.
		if err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{providerOpsOutput()}}); err != nil {
			t.Fatalf("output within the cap refused: %v", err)
		}
	})
	t.Run("cap survives the last release", func(t *testing.T) {
		t.Parallel()
		vb, unit5 := transitionedLineagePlan("vb")
		corr := startExecuting(providerOpsTask("corr", "architecture-pair", models.TaskStatus("ARCHITECTING")), "architect-1")
		root, statePath, _ := setupDescendantTest(t, vb, unit5, corr)
		setTaskYAMLField(t, statePath, "vb-cp-0", "provider_reservations", reservation("corr", providerOpsTransition))
		setTaskYAMLField(t, statePath, "corr", "max_outputs", 1)
		if _, err := ReserveProviderWithAuthority(root, ReserveProviderInput{
			TaskID: "vb-cp-0", ProviderTask: "corr", Transition: providerOpsTransition, Reason: "placement withdrawn", Release: true,
		}, d80Authority); err != nil {
			t.Fatalf("release refused: %v", err)
		}
		if got := taskYAMLField(t, statePath, "corr", "max_outputs"); got != 1 {
			t.Fatalf("release changed corr max_outputs to %v", got)
		}
		before := replacementBytes(t, statePath)
		err := SetTaskOutput(root, &SetTaskOutputInput{TaskID: "corr", AgentID: "architect-1", Output: []models.OutputEntry{providerOpsOutput(), providerOpsOutput()}})
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "max_outputs")
	})
	t.Run("ops and YAML input refuse a negative cap", func(t *testing.T) {
		t.Parallel()
		input := codePlanReplacementInput("capped")
		input.MaxOutputs = -1
		if err := validateAddTaskInput(&input); err == nil || !strings.Contains(err.Error(), "max_outputs") {
			t.Fatalf("negative max_outputs accepted: %v", err)
		}
		input.MaxOutputs = 0
		input.ReserveSuccessors = []string{"vb-cp-0", "vb-cp-0"}
		if err := validateAddTaskInput(&input); err == nil || !strings.Contains(err.Error(), "reserve_successors") {
			t.Fatalf("duplicate reserve_successors accepted: %v", err)
		}
	})
}

// C7: dropping a placement is refused when the holder is superseded, never
// later: a release on the live successor is an explicit policy decision,
// including after a second lawful replacement.
func TestReleaseAfterHolderReplacement_D80(t *testing.T) {
	t.Parallel()
	release := func(t *testing.T, root, holder string) error {
		t.Helper()
		_, err := ReserveProviderWithAuthority(root, ReserveProviderInput{
			TaskID: holder, ProviderTask: "corr", Transition: providerOpsTransition, Reason: "human withdraws placement", Release: true,
		}, d80Authority)
		return err
	}
	t.Run("release on the replacement", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		if err := replaceCodePlanForTest(root, statePath, "vb-cp-0", "vb-cp-0-r1", "re-authored writer"); err != nil {
			t.Fatal(err)
		}
		if err := release(t, root, "vb-cp-0-r1"); err != nil {
			t.Fatalf("release on the live replacement holder refused: %v", err)
		}
		if got := taskYAMLField(t, statePath, "vb-cp-0-r1", "provider_reservations"); got != nil {
			t.Fatalf("release left reservation: %v", got)
		}
	})
	t.Run("release after a second replacement", func(t *testing.T) {
		t.Parallel()
		root, statePath := reservedChainFixture(t)
		if err := replaceCodePlanForTest(root, statePath, "vb-cp-0", "vb-cp-0-r1", "re-authored writer"); err != nil {
			t.Fatal(err)
		}
		if err := replaceCodePlanForTest(root, statePath, "vb-cp-0-r1", "vb-cp-0-r2", "re-authored again"); err != nil {
			t.Fatal(err)
		}
		if taskYAMLField(t, statePath, "vb-cp-0-r2", "provider_reservations") == nil {
			t.Fatal("second replacement lost the placement")
		}
		if err := release(t, root, "vb-cp-0-r2"); err != nil {
			t.Fatalf("release after a second replacement refused: %v", err)
		}
	})
	t.Run("superseding the replacement by a non-holder is refused", func(t *testing.T) {
		t.Parallel()
		other := providerOpsTask("other", "code-planning-pair", models.TaskStatusDraftCodingPlan)
		root, statePath := reservedChainFixture(t, other)
		if err := replaceCodePlanForTest(root, statePath, "vb-cp-0", "vb-cp-0-r1", "re-authored writer"); err != nil {
			t.Fatal(err)
		}
		before := replacementBytes(t, statePath)
		_, err := SupersedeTask(root, "vb-cp-0-r1", []string{"other"}, "replaced elsewhere", "orchestrator-1")
		requireProviderOpsAtomicRefusal(t, statePath, before, err, "provider_reservations", "other")
	})
}

// A cap-only change is recorded on the provider, with the previous value.
func TestCapChangeIsRecorded_D80(t *testing.T) {
	t.Parallel()
	root, statePath := reservedChainFixture(t)
	result, err := ReserveProviderWithAuthority(root, ReserveProviderInput{
		TaskID: "vb-cp-0", ProviderTask: "corr", Transition: providerOpsTransition, Reason: "cap the correction", ProviderMaxOutputs: 1,
	}, d80Authority)
	if err != nil || !result.CapChanged || !result.Changed {
		t.Fatalf("cap-only change = %+v, %v", result, err)
	}
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	history := state.FindTask("corr").History
	last := history[len(history)-1]
	capChange, ok := last.Extra["max_outputs"].(map[string]any)
	if last.Event != models.TaskEventDependenciesRewritten || !ok || capChange["current"] != 1 || capChange["previous"] != 0 {
		t.Fatalf("cap change not recorded on the provider: %+v", last)
	}
}
