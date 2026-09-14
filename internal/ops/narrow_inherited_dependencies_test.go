package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Post-hoc narrowing (ADR-0137 applied after generation).
//
// The fixture is built by the real generation path so the children carry
// exactly the whole-phase barrier a run accumulates: plan-1 fans out to three
// coding children, plan-2 depends on plan-1 and fans out to two children that
// each inherit all three of plan-1's children.

const (
	narrowUpstream   = "plan-1"
	narrowDownstream = "plan-2"
)

func narrowChild(plan string, i int) string {
	return perSubtaskChildID(plan, selTransition, i)
}

func narrowPlanTask(id string, dependsOn []string, outputs []models.OutputEntry, now time.Time) models.Task {
	reviewCommit := "abc123"
	return models.Task{
		ID:           id,
		Type:         models.TaskTypeCoding,
		RolePair:     "code-planning-pair",
		Description:  "plan " + id,
		Status:       models.TaskStatus("CODING_PLAN_APPROVED"),
		Priority:     1,
		Created:      now,
		SpecRef:      "README.md",
		DoneWhen:     "plan approved",
		Scope:        "scope",
		ReviewCommit: &reviewCommit,
		DependsOn:    dependsOn,
		Output:       outputs,
		History:      []models.TaskHistoryEntry{},
	}
}

func narrowOutputs(n int, extra ...func(i int, e *models.OutputEntry)) []models.OutputEntry {
	outputs := make([]models.OutputEntry, 0, n)
	for i := range n {
		entry := models.OutputEntry{Desc: "child", DoneWhen: "done", Scope: "scope", SpecRef: "README.md#a"}
		for _, fn := range extra {
			fn(i, &entry)
		}
		outputs = append(outputs, entry)
	}
	return outputs
}

// setupNarrowFixture generates both plans' children through Proceed and then
// marks the plans MERGED, which is the state a run is in when the operator
// wants to narrow.
func setupNarrowFixture(t *testing.T, downstreamOutputs []models.OutputEntry) (string, string) {
	t.Helper()
	tmpDir, stateFile := setupPipelineProceedTest(t)
	testhelpers.SetupTestGitRepo(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Goal.SpecRef = "README.md"
	state.Sprint.Status = models.SprintStatusCompleted
	state.Tasks = append(state.Tasks,
		narrowPlanTask(narrowUpstream, nil, narrowOutputs(3), now),
		narrowPlanTask(narrowDownstream, []string{narrowUpstream}, downstreamOutputs, now),
	)
	state.Sprint.Scope.Planned = []string{narrowUpstream, narrowDownstream}
	testhelpers.WriteInitialState(t, stateFile, state)

	for _, plan := range []string{narrowUpstream, narrowDownstream} {
		if _, err := Proceed(tmpDir, plan, selTransition); err != nil {
			t.Fatalf("Proceed(%s): %v", plan, err)
		}
	}

	bb := db.New(stateFile)
	if err := bb.Modify(func(s *models.State) error {
		for _, plan := range []string{narrowUpstream, narrowDownstream} {
			task := s.FindTask(plan)
			task.Status = models.TaskStatusMerged
			task.HandoffEvents = append(task.HandoffEvents,
				models.HandoffEvent{Timestamp: now, Agent: "code-planner-1", Trigger: models.HandoffTriggerSubmission},
				models.HandoffEvent{Timestamp: now, Agent: "code-plan-reviewer-1", Trigger: models.HandoffTriggerCompletion},
			)
		}
		return nil
	}); err != nil {
		t.Fatalf("mark plans merged: %v", err)
	}
	return tmpDir, stateFile
}

func readNarrowState(t *testing.T, stateFile string) *models.State {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func selecting(index int, outputs ...int) NarrowSelection {
	return NarrowSelection{Index: index, InheritInputs: &models.InheritInputs{
		Mode:       models.InheritModeSelected,
		Selections: []models.InputSelection{{UpstreamTask: narrowUpstream, Outputs: outputs}},
	}}
}

func TestNarrowInheritedDependencies_RemovesUnselectedBarrierEdges(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(2))

	// Precondition: generation gave both children the whole-phase barrier.
	before := readNarrowState(t, stateFile)
	for i := range 2 {
		child := before.FindTask(narrowChild(narrowDownstream, i))
		for j := range 3 {
			if !slices.Contains(child.DependsOn, narrowChild(narrowUpstream, j)) {
				t.Fatalf("fixture: %s lacks barrier edge to upstream child %d: %v", child.ID, j, child.DependsOn)
			}
		}
	}

	result, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, "", []NarrowSelection{selecting(0, 1)}, "docs child needs only the contract", "orchestrator-1")
	if err != nil {
		t.Fatalf("NarrowInheritedDependencies: %v", err)
	}
	if result.Transition != selTransition {
		t.Fatalf("Transition = %q, want auto-detected %q", result.Transition, selTransition)
	}
	if result.Narrowed != 1 || result.Unchanged != 0 || result.Skipped != 0 {
		t.Fatalf("counts = narrowed %d unchanged %d skipped %d, want 1/0/0", result.Narrowed, result.Unchanged, result.Skipped)
	}

	after := readNarrowState(t, stateFile)
	narrowed := after.FindTask(narrowChild(narrowDownstream, 0))
	want := []string{narrowChild(narrowUpstream, 1)}
	if !slices.Equal(narrowed.DependsOn, want) {
		t.Fatalf("narrowed DependsOn = %v, want %v", narrowed.DependsOn, want)
	}
	last := narrowed.History[len(narrowed.History)-1]
	if last.Event != models.TaskEventDependenciesRewritten || last.Extra["operation"] != narrowInheritedDependenciesOperation {
		t.Fatalf("history = %+v, want %s/%s", last, models.TaskEventDependenciesRewritten, narrowInheritedDependenciesOperation)
	}

	// The unselected sibling keeps the whole barrier: no intent was authored for it.
	untouched := after.FindTask(narrowChild(narrowDownstream, 1))
	if !slices.Equal(untouched.DependsOn, before.FindTask(untouched.ID).DependsOn) {
		t.Fatalf("output 1 was rewritten without a selection: %v", untouched.DependsOn)
	}

	// The intent is persisted on the producer so the rewrite is reproducible.
	producer := after.FindTask(narrowDownstream)
	if !producer.Output[0].InheritInputs.IsSelective() {
		t.Fatal("producer output[0].inherit_inputs was not persisted")
	}
	if producer.Output[1].InheritInputs != nil {
		t.Fatal("producer output[1].inherit_inputs was authored without a selection")
	}
}

func TestNarrowInheritedDependencies_NeverRemovesSiblingOrConcreteEdges(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(2, func(i int, e *models.OutputEntry) {
		if i == 1 {
			e.DependsOn = []string{"0"} // sibling edge to output 0
		}
	}))

	_, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{selecting(1, 2)}, "keep only the schema", "orchestrator-1")
	if err != nil {
		t.Fatalf("NarrowInheritedDependencies: %v", err)
	}

	child := readNarrowState(t, stateFile).FindTask(narrowChild(narrowDownstream, 1))
	if !slices.Contains(child.DependsOn, narrowChild(narrowDownstream, 0)) {
		t.Fatalf("sibling edge was removed: %v", child.DependsOn)
	}
	if !slices.Contains(child.DependsOn, narrowChild(narrowUpstream, 2)) {
		t.Fatalf("selected upstream edge missing: %v", child.DependsOn)
	}
	for _, unwanted := range []int{0, 1} {
		if slices.Contains(child.DependsOn, narrowChild(narrowUpstream, unwanted)) {
			t.Fatalf("unselected upstream edge %d survived: %v", unwanted, child.DependsOn)
		}
	}
}

func TestNarrowInheritedDependencies_SkipsChildrenNotInInitialStatus(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(2))

	agent := "coder-1"
	lease := time.Now().UTC().Add(time.Hour)
	if err := db.New(stateFile).Modify(func(s *models.State) error {
		// An executing child has satisfied dependencies by construction.
		for i := range 3 {
			upstreamChild := s.FindTask(narrowChild(narrowUpstream, i))
			upstreamChild.Status = models.TaskStatusMerged
			upstreamChild.HandoffEvents = append(upstreamChild.HandoffEvents,
				models.HandoffEvent{Timestamp: lease, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission},
				models.HandoffEvent{Timestamp: lease, Agent: "code-reviewer-1", Trigger: models.HandoffTriggerCompletion},
			)
		}
		task := narrowChild(narrowDownstream, 0)
		s.Agents[agent] = models.Agent{
			Role: "coder", Status: models.AgentStatusWorking, Provider: "codex", PID: 1234,
			Heartbeat: lease, RegisteredAt: lease, CurrentTask: &task, LeaseExpires: &lease,
		}
		executing := s.FindTask(task)
		executing.Status = models.TaskStatusImplementing
		executing.AssignedTo = &agent
		executing.LeaseExpires = &lease
		base := "abc1234"
		executing.BaseCommit = &base
		worktree := ".worktrees/" + executing.ID
		executing.Worktree = &worktree
		return os.MkdirAll(filepath.Join(tmpDir, worktree), 0o755)
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}
	before := readNarrowState(t, stateFile).FindTask(narrowChild(narrowDownstream, 0)).DependsOn

	result, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{selecting(0, 0), selecting(1, 0)}, "narrow", "orchestrator-1")
	if err != nil {
		t.Fatalf("NarrowInheritedDependencies: %v", err)
	}
	if result.Skipped != 1 || result.Narrowed != 1 {
		t.Fatalf("counts = skipped %d narrowed %d, want 1/1: %+v", result.Skipped, result.Narrowed, result.Children)
	}
	skipped := result.Children[0]
	if skipped.Action != "skipped" || !strings.Contains(skipped.SkipReason, "IMPLEMENTING_CODE") {
		t.Fatalf("child 0 = %+v, want skipped for executing status", skipped)
	}
	after := readNarrowState(t, stateFile).FindTask(narrowChild(narrowDownstream, 0)).DependsOn
	if !slices.Equal(before, after) {
		t.Fatalf("executing child was rewritten: %v -> %v", before, after)
	}
}

func TestNarrowInheritedDependencies_FailsClosedOnOutOfRangeSelection(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(2))
	before := readNarrowState(t, stateFile)

	// plan-1 has three outputs; index 5 names nothing.
	_, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{selecting(0, 1), selecting(1, 5)}, "narrow", "orchestrator-1")
	if err == nil {
		t.Fatal("expected an error for an out-of-range selection")
	}
	if !strings.Contains(err.Error(), "cannot select index 5") {
		t.Fatalf("error = %v, want out-of-range diagnostic", err)
	}

	after := readNarrowState(t, stateFile)
	for i := range 2 {
		id := narrowChild(narrowDownstream, i)
		if !slices.Equal(before.FindTask(id).DependsOn, after.FindTask(id).DependsOn) {
			t.Fatalf("%s was rewritten despite the failed operation", id)
		}
	}
	if after.FindTask(narrowDownstream).Output[0].InheritInputs != nil {
		t.Fatal("a valid selection was persisted although the operation failed")
	}
}

func TestNarrowInheritedDependencies_ModeAllIsUnchanged(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(1))
	before := readNarrowState(t, stateFile).FindTask(narrowChild(narrowDownstream, 0)).DependsOn

	result, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{{
		Index: 0, InheritInputs: &models.InheritInputs{Mode: models.InheritModeAll},
	}}, "explicit barrier", "orchestrator-1")
	if err != nil {
		t.Fatalf("NarrowInheritedDependencies: %v", err)
	}
	if result.Unchanged != 1 || result.Narrowed != 0 {
		t.Fatalf("counts = unchanged %d narrowed %d, want 1/0", result.Unchanged, result.Narrowed)
	}
	after := readNarrowState(t, stateFile).FindTask(narrowChild(narrowDownstream, 0)).DependsOn
	if !slices.Equal(before, after) {
		t.Fatalf("mode all changed dependencies: %v -> %v", before, after)
	}
}

func TestNarrowInheritedDependencies_NoneKeepsOnlySiblingAndConcreteEdges(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(2, func(i int, e *models.OutputEntry) {
		if i == 1 {
			e.DependsOn = []string{"0"}
			e.TaskDependsOn = []string{narrowChild(narrowUpstream, 2)}
		}
	}))

	result, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{
		{Index: 0, InheritInputs: &models.InheritInputs{Mode: models.InheritModeNone}},
		{Index: 1, InheritInputs: &models.InheritInputs{Mode: models.InheritModeNone}},
	}, "external prerequisites: none added", "orchestrator-1")
	if err != nil {
		t.Fatalf("NarrowInheritedDependencies: %v", err)
	}
	if result.Narrowed != 2 {
		t.Fatalf("narrowed = %d, want 2: %+v", result.Narrowed, result.Children)
	}

	after := readNarrowState(t, stateFile)
	if deps := after.FindTask(narrowChild(narrowDownstream, 0)).DependsOn; len(deps) != 0 {
		t.Fatalf("child 0 should have no dependencies left, got %v", deps)
	}
	want := []string{narrowChild(narrowDownstream, 0), narrowChild(narrowUpstream, 2)}
	got := after.FindTask(narrowChild(narrowDownstream, 1)).DependsOn
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("child 1 deps = %v, want sibling + concrete only %v", got, want)
	}
}

func TestNarrowInheritedDependencies_RefusesReplannedProducer(t *testing.T) {
	t.Parallel()
	tmpDir, stateFile := setupNarrowFixture(t, narrowOutputs(1))
	if err := db.New(stateFile).Modify(func(s *models.State) error {
		s.FindTask(narrowDownstream).TransitionsExecuted["replanned"] = true
		return nil
	}); err != nil {
		t.Fatalf("modify: %v", err)
	}

	_, err := NarrowInheritedDependencies(tmpDir, narrowDownstream, selTransition, []NarrowSelection{selecting(0, 0)}, "narrow", "orchestrator-1")
	if err == nil || !strings.Contains(err.Error(), "replanned") {
		t.Fatalf("err = %v, want replanned refusal", err)
	}
}

func TestLoadNarrowSelectionsFile_ParsesYAML(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "narrow.yaml")
	content := "outputs:\n  - index: 2\n    inherit_inputs:\n      mode: selected\n      selections:\n        - upstream_task: plan-1\n          outputs: [0, 2]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	selections, err := LoadNarrowSelectionsFile(path)
	if err != nil {
		t.Fatalf("LoadNarrowSelectionsFile: %v", err)
	}
	if len(selections) != 1 || selections[0].Index != 2 || !selections[0].InheritInputs.IsSelective() {
		t.Fatalf("selections = %+v", selections)
	}
	if got := selections[0].InheritInputs.Selections[0].Outputs; !slices.Equal(got, []int{0, 2}) {
		t.Fatalf("outputs = %v, want [0 2]", got)
	}
}
