package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Selective dependency generation (W6).
//
// The behavior under test is claimability, not edge count. A plan that merely
// produces fewer edges is not the goal — a child that becomes claimable while
// a prerequisite it named is unmerged is a defect, and a child held by a
// provider it never needed is the waste this exists to remove.

const (
	selTransition = "code-plan-to-coding"
	selUpstream   = "plan-1"
	selDownstream = "plan-2"
)

// selState builds an upstream that has fanned out to n children, plus a
// downstream producer that depends on it.
func selState(t *testing.T, upstreamOutputs int, downstreamEntries []models.OutputEntry) *models.State {
	t.Helper()

	upstream := models.Task{
		ID:                  selUpstream,
		Status:              models.TaskStatusMerged,
		RolePair:            "code-planning-pair",
		TransitionsExecuted: map[string]bool{selTransition: true},
	}
	tasks := []models.Task{}
	for i := range upstreamOutputs {
		upstream.Output = append(upstream.Output, models.OutputEntry{
			Desc: "u", DoneWhen: "u", Scope: "u", SpecRef: "s.md",
		})
		tasks = append(tasks, models.Task{
			ID:     perSubtaskChildID(selUpstream, selTransition, i),
			Status: models.TaskStatus("DRAFT_CODE"),
		})
	}

	downstream := models.Task{
		ID:        selDownstream,
		Status:    models.TaskStatusMerged,
		RolePair:  "code-planning-pair",
		DependsOn: []string{selUpstream},
		Output:    downstreamEntries,
	}
	return &models.State{Tasks: append([]models.Task{upstream}, append(tasks, downstream)...)}
}

func selResolve(t *testing.T, s *models.State, entry models.OutputEntry) ([]string, error) {
	t.Helper()
	tmpDir, _ := setupPipelineProceedTest(t)
	resolver, _, err := loadResolver(tmpDir)
	if err != nil {
		t.Fatalf("loadResolver: %v", err)
	}
	set, err := computeInheritedDeps(s, s.FindTask(selDownstream), selTransition, resolver)
	if err != nil {
		return nil, err
	}
	return set.forEntry(entry, 0)
}

func selSelecting(outputs ...int) models.OutputEntry {
	return models.OutputEntry{
		Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md",
		InheritInputs: &models.InheritInputs{
			Mode:       models.InheritModeSelected,
			Selections: []models.InputSelection{{UpstreamTask: selUpstream, Outputs: outputs}},
		},
	}
}

// Test 1 (W6): a child that needs nothing from a provider stays claimable
// while that provider is unmerged.
//
// The documentation child selects nothing from the upstream, so it inherits no
// edge to it. Under the whole-phase barrier it would wait for all three.
func TestSelectiveInputs_UnrelatedChildNotHeldByProvider(t *testing.T) {
	t.Parallel()

	unrelated := models.OutputEntry{
		Desc: "document the feature", DoneWhen: "docs updated", Scope: "docs/", SpecRef: "s.md",
		InheritInputs: &models.InheritInputs{
			Mode: models.InheritModeSelected,
			Selections: []models.InputSelection{
				// Names the upstream but selects only output 0, so the
				// children of outputs 1 and 2 are not prerequisites.
				{UpstreamTask: selUpstream, Outputs: []int{0}},
			},
		},
	}
	deps, err := selResolve(t, selState(t, 3, []models.OutputEntry{unrelated}), unrelated)
	if err != nil {
		t.Fatalf("forEntry: %v", err)
	}

	want := perSubtaskChildID(selUpstream, selTransition, 0)
	if len(deps) != 1 || deps[0] != want {
		t.Fatalf("deps = %v, want exactly [%s]", deps, want)
	}
	for _, unwanted := range []int{1, 2} {
		id := perSubtaskChildID(selUpstream, selTransition, unwanted)
		if slices.Contains(deps, id) {
			t.Errorf("child inherited %s, which it did not select — the barrier was not narrowed", id)
		}
	}
}

// Test 2 (W6): a child that names a required contract is held by it.
func TestSelectiveInputs_RequiredContractIsPreserved(t *testing.T) {
	t.Parallel()

	entry := selSelecting(1)
	deps, err := selResolve(t, selState(t, 3, []models.OutputEntry{entry}), entry)
	if err != nil {
		t.Fatalf("forEntry: %v", err)
	}
	want := perSubtaskChildID(selUpstream, selTransition, 1)
	if !slices.Contains(deps, want) {
		t.Fatalf("deps = %v, missing required prerequisite %s", deps, want)
	}
}

// Test 3 (W6): a required provider that remains unmerged still blocks.
//
// Claimability is asserted through the real predicate rather than by
// inspecting the edge list, because "fewer edges" is not the property that
// matters.
func TestSelectiveInputs_UnmergedRequiredProviderStillBlocks(t *testing.T) {
	t.Parallel()

	entry := selSelecting(2)
	s := selState(t, 3, []models.OutputEntry{entry})

	// The selected provider is the one left unmerged.
	provider := s.FindTask(perSubtaskChildID(selUpstream, selTransition, 2))
	provider.Status = models.TaskStatus("DRAFT_CODE")
	for _, i := range []int{0, 1} {
		s.FindTask(perSubtaskChildID(selUpstream, selTransition, i)).Status = models.TaskStatusMerged
	}

	deps, err := selResolve(t, s, entry)
	if err != nil {
		t.Fatalf("forEntry: %v", err)
	}
	if !slices.Contains(deps, provider.ID) {
		t.Fatalf("deps = %v, must retain unmerged selected provider %s", deps, provider.ID)
	}

	allMerged := true
	for _, dep := range deps {
		if s.FindTask(dep).Status != models.TaskStatusMerged {
			allMerged = false
		}
	}
	if allMerged {
		t.Error("every selected dependency is MERGED; the child would be claimable despite an unmerged provider")
	}
}

// Test 4 (W6): omitted inherit_inputs reproduces the pre-existing edges
// exactly. This is the backward-compatibility guarantee that lets existing and
// frozen runs keep their semantics with no migration.
func TestSelectiveInputs_OmittedIntentMatchesWholePhaseBarrier(t *testing.T) {
	t.Parallel()

	plain := models.OutputEntry{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"}
	deps, err := selResolve(t, selState(t, 3, []models.OutputEntry{plain}), plain)
	if err != nil {
		t.Fatalf("forEntry: %v", err)
	}
	for i := range 3 {
		id := perSubtaskChildID(selUpstream, selTransition, i)
		if !slices.Contains(deps, id) {
			t.Errorf("omitted intent dropped %s; it must inherit the whole upstream phase", id)
		}
	}
	if len(deps) != 3 {
		t.Errorf("deps = %v, want all 3 upstream children", deps)
	}
}

// Test 5 (W6): mode "all" is byte-identical to omitting the field, so a
// planner can record a deliberate barrier without changing behavior.
func TestSelectiveInputs_ExplicitAllEqualsOmitted(t *testing.T) {
	t.Parallel()

	plain := models.OutputEntry{Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md"}
	explicit := plain
	explicit.InheritInputs = &models.InheritInputs{Mode: models.InheritModeAll}

	s := selState(t, 3, []models.OutputEntry{plain})
	omitted, err := selResolve(t, s, plain)
	if err != nil {
		t.Fatalf("omitted: %v", err)
	}
	stated, err := selResolve(t, s, explicit)
	if err != nil {
		t.Fatalf("explicit all: %v", err)
	}
	if !slices.Equal(omitted, stated) {
		t.Errorf("mode %q = %v, omitted = %v; they must be identical",
			models.InheritModeAll, stated, omitted)
	}
}

// Test 6 (W6): an out-of-range selection fails the transition rather than
// yielding a smaller dependency set.
//
// Silently dropping would produce a child that runs without an input its
// planner declared, which is the failure selective inputs must not introduce.
func TestSelectiveInputs_OutOfRangeSelectionFailsClosed(t *testing.T) {
	t.Parallel()

	entry := selSelecting(7)
	_, err := selResolve(t, selState(t, 3, []models.OutputEntry{entry}), entry)
	if err == nil {
		t.Fatal("out-of-range selection was accepted; it must fail the transition")
	}
	if !strings.Contains(err.Error(), "cannot select index 7") {
		t.Errorf("error %q does not name the offending index", err)
	}
}

// A selection naming an upstream that contributes no children fails closed for
// the same reason: the named prerequisite does not exist and cannot be waited
// for, so proceeding would silently drop it.
func TestSelectiveInputs_UnknownUpstreamFailsClosed(t *testing.T) {
	t.Parallel()

	entry := models.OutputEntry{
		Desc: "d", DoneWhen: "d", Scope: "d", SpecRef: "s.md",
		InheritInputs: &models.InheritInputs{
			Mode:       models.InheritModeSelected,
			Selections: []models.InputSelection{{UpstreamTask: "never-a-dependency", Outputs: []int{0}}},
		},
	}
	_, err := selResolve(t, selState(t, 3, []models.OutputEntry{entry}), entry)
	if err == nil {
		t.Fatal("selection naming an unknown upstream was accepted; it must fail the transition")
	}
	if !strings.Contains(err.Error(), "supplies no inherited children") {
		t.Errorf("error %q does not explain why the upstream is unusable", err)
	}
}

// Test 9 (W6): single-edge cardinalities have no fan-out to narrow, so a
// selection is inert there rather than an error. Inheriting the whole edge is
// a superset, so nothing is dropped.
func TestSelectiveInputs_InertOnSingleEdgeCardinality(t *testing.T) {
	t.Parallel()

	set := inheritedDepSet{
		all:        []string{"one-to-one-child"},
		byUpstream: map[string][]string{selUpstream: {"one-to-one-child"}},
		selectable: false,
	}
	deps, err := set.forEntry(selSelecting(0), 0)
	if err != nil {
		t.Fatalf("forEntry on a single-edge cardinality: %v", err)
	}
	if !slices.Equal(deps, set.all) {
		t.Errorf("deps = %v, want the whole edge %v: a selection must be inert, not narrowing, "+
			"where there is no fan-out", deps, set.all)
	}
}

// Authoring-time structural validation. Index bounds are deliberately absent
// here — the upstream's output[] does not exist yet — and are covered by the
// fail-closed generation tests above.
// mode "none": the child declared no external prerequisite, so it inherits
// no phase-gate edge at all. Its ordering is carried by sibling and
// task_depends_on edges only.
func TestSelectiveInputs_NoneInheritsNoPhaseEdge(t *testing.T) {
	t.Parallel()

	entry := models.OutputEntry{
		Desc: "tooling", DoneWhen: "d", Scope: "d", SpecRef: "s.md",
		InheritInputs: &models.InheritInputs{Mode: models.InheritModeNone},
	}
	deps, err := selResolve(t, selState(t, 3, []models.OutputEntry{entry}), entry)
	if err != nil {
		t.Fatalf("forEntry: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("deps = %v, want none", deps)
	}
}

func TestValidateInheritInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		inherit *models.InheritInputs
		wantErr string
	}{
		{name: "omitted is valid", inherit: nil},
		{name: "all is valid", inherit: &models.InheritInputs{Mode: models.InheritModeAll}},
		{name: "none is valid", inherit: &models.InheritInputs{Mode: models.InheritModeNone}},
		{
			name:    "none must not carry selections",
			inherit: &models.InheritInputs{Mode: models.InheritModeNone, Selections: []models.InputSelection{{UpstreamTask: "u", Outputs: []int{0}}}},
			wantErr: "must not carry selections",
		},
		{
			name:    "unknown mode",
			inherit: &models.InheritInputs{Mode: "some"},
			wantErr: "mode must be",
		},
		{
			name:    "all must not carry selections",
			inherit: &models.InheritInputs{Mode: models.InheritModeAll, Selections: []models.InputSelection{{UpstreamTask: "u", Outputs: []int{0}}}},
			wantErr: "must not carry selections",
		},
		{
			name:    "selected requires a selection",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected},
			wantErr: "requires at least one selection",
		},
		{
			name:    "empty outputs is not an empty dependency",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "u"}}},
			wantErr: "selects no outputs",
		},
		{
			name:    "negative index",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "u", Outputs: []int{-1}}}},
			wantErr: "negative output index",
		},
		{
			name:    "duplicate upstream",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "u", Outputs: []int{0}}, {UpstreamTask: "u", Outputs: []int{1}}}},
			wantErr: "duplicate upstream_task",
		},
		{
			name:    "duplicate output index",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: "u", Outputs: []int{0, 0}}}},
			wantErr: "duplicate output index",
		},
		{
			name:    "untrimmed upstream",
			inherit: &models.InheritInputs{Mode: models.InheritModeSelected, Selections: []models.InputSelection{{UpstreamTask: " u", Outputs: []int{0}}}},
			wantErr: "non-empty and trimmed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := models.ValidateInheritInputs(tc.inherit, 0)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("want error containing %q, got nil", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

// Test 7 (W6): crash recovery gives a surviving selective child exactly the
// dependencies an uninterrupted generation would have — not the whole-phase
// barrier merged back in.
//
// Recovery's existing-child branch and its missing-child branch must resolve
// selections through the same path. If they diverge, the children that
// existed at the crash carry the full barrier while the ones recreated carry
// the selection, and INVARIANTS.md §3.4's "crash recovery validates the same
// final child depends_on set" is false.
func TestSelectiveInputs_CrashRecoveryPreservesSelection(t *testing.T) {
	t.Parallel()

	tmpDir, stateFile := setupPipelineProceedTest(t)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusInProgress

	// Upstream plan-1 already fanned out to three children.
	plan1 := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, now)
	plan1.RolePair = "code-planning-pair"
	plan1.Output = []models.OutputEntry{
		{Desc: "u0", DoneWhen: "u", Scope: "u", SpecRef: "s.md"},
		{Desc: "u1", DoneWhen: "u", Scope: "u", SpecRef: "s.md"},
		{Desc: "u2", DoneWhen: "u", Scope: "u", SpecRef: "s.md"},
	}
	plan1.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}
	var upstreamChildren []models.Task
	for i := range 3 {
		c := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-1", "code-plan-to-coding", i), models.TaskStatus("DRAFT_CODE"), now)
		c.RolePair = "coding-pair"
		upstreamChildren = append(upstreamChildren, c)
	}
	selectedID := upstreamChildren[1].ID

	// plan-2 selects only output 1 of plan-1 for both its children. Its
	// transition is marked executed but crashed after child-0 was written
	// (without inherited deps) and before child-1 was.
	selecting := func(desc string) models.OutputEntry {
		return models.OutputEntry{
			Desc: desc, DoneWhen: "d", Scope: "d", SpecRef: "s.md",
			InheritInputs: &models.InheritInputs{
				Mode:       models.InheritModeSelected,
				Selections: []models.InputSelection{{UpstreamTask: "plan-1", Outputs: []int{1}}},
			},
		}
	}
	plan2 := testhelpers.BuildTaskByStatus("plan-2", models.TaskStatusMerged, now)
	plan2.RolePair = "code-planning-pair"
	plan2.Output = []models.OutputEntry{selecting("d0"), selecting("d1")}
	plan2.DependsOn = []string{"plan-1"}
	plan2.TransitionsExecuted = map[string]bool{"code-plan-to-coding": true}

	survivor := testhelpers.BuildTaskByStatus(perSubtaskChildID("plan-2", "code-plan-to-coding", 0), models.TaskStatus("DRAFT_CODE"), now)
	survivor.RolePair = "coding-pair"

	state.Tasks = append(state.Tasks, plan1)
	state.Tasks = append(state.Tasks, upstreamChildren...)
	state.Tasks = append(state.Tasks, plan2, survivor)
	state.Sprint.Scope.Planned = []string{"plan-1", "plan-2", survivor.ID}
	for _, c := range upstreamChildren {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, c.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	results, err := ExecuteAvailableTransitions(tmpDir, "manual")
	if err != nil {
		t.Fatalf("ExecuteAvailableTransitions: %v", err)
	}
	if len(results) != 1 || len(results[0].ChildTaskIDs) != 1 {
		t.Fatalf("expected one recovered child, got %+v", results)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	recovered := readState.FindTask(results[0].ChildTaskIDs[0])
	patched := readState.FindTask(survivor.ID)
	if recovered == nil || patched == nil {
		t.Fatal("recovered or surviving child not found")
	}

	// Both carry exactly the selected upstream child and nothing else from
	// plan-1.
	for name, child := range map[string]*models.Task{"recovered": recovered, "survivor": patched} {
		if !slices.Contains(child.DependsOn, selectedID) {
			t.Errorf("%s child missing selected dep %s; DependsOn = %v", name, selectedID, child.DependsOn)
		}
		for _, unwanted := range []string{upstreamChildren[0].ID, upstreamChildren[2].ID} {
			if slices.Contains(child.DependsOn, unwanted) {
				t.Errorf("%s child inherited %s, which it did not select — the whole-phase barrier leaked back in; DependsOn = %v",
					name, unwanted, child.DependsOn)
			}
		}
	}

	// And they are identical to each other: recovery did not produce two
	// different answers for the same intent.
	if !slices.Equal(sortedCopy(recovered.DependsOn), sortedCopy(patched.DependsOn)) {
		t.Errorf("recovered %v != survivor %v; crash recovery must yield the same final depends_on set",
			recovered.DependsOn, patched.DependsOn)
	}
}

func sortedCopy(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return out
}
