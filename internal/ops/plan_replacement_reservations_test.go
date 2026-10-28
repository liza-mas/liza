package ops

import (
	"errors"
	"io"
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
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const d89WriterTransition = "code-plan-to-coding"

func d89Reservations(provider string) []models.ProviderReservation {
	return []models.ProviderReservation{{ProviderTask: provider, Transition: d89WriterTransition}}
}

func d89Writer(id string) models.Task {
	return providerOpsTask(id, "code-planning-pair", models.TaskStatusDraftCodingPlan)
}

func d89Plan(originals ...string) models.Task {
	plan := withPlanCheck(handoffPlan(replacementPlanID, "architecture-pair"), models.PlanCheckPassed, "")
	plan.Output = nil
	for _, id := range originals {
		plan.Output = append(plan.Output, replacingOutput(id))
	}
	return plan
}

func d89Successor(index int) string {
	return perSubtaskChildID(replacementPlanID, providerOpsTransition, index)
}

func d89CompleteSprint(t *testing.T, statePath string) {
	t.Helper()
	state := readReplacementState(t, statePath)
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, statePath, state)
}

func TestPlanReplacement_ReservationPlacement(t *testing.T) {
	t.Parallel()
	for _, automatic := range []bool{false, true} {
		name := "manual"
		if automatic {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			original := providerOpsTask("original", "code-planning-pair", models.TaskStatusBlocked)
			original.Worktree = nil
			original.MaxOutputs = 3
			original.ProviderReservations = d89Reservations("earlier")
			holder := d89Writer("later")
			holder.ProviderReservations = d89Reservations(original.ID)
			root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID), d89Writer("earlier"), original, holder)
			if !automatic {
				d89CompleteSprint(t, statePath)
			}
			before := readReplacementState(t, statePath)
			if err := statevalidate.ValidateState(before, root, true, io.Discard); err != nil {
				t.Fatalf("invalid reproduction fixture: %v", err)
			}

			if automatic {
				report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
				if err != nil || len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
					t.Fatalf("valid reserved replacement refused: err=%v report=%+v", err, report)
				}
			} else if _, err := Proceed(root, replacementPlanID, providerOpsTransition); err != nil {
				t.Fatalf("valid reserved replacement refused: %v", err)
			}

			state := readReplacementState(t, statePath)
			successorID := d89Successor(0)
			requireRetiredBy(t, state, original.ID, successorID)
			successor := state.FindTask(successorID)
			if successor.MaxOutputs != 3 || !slices.Equal(successor.ProviderReservations, original.ProviderReservations) {
				t.Fatalf("successor lost writer placement: %+v", successor)
			}
			if !slices.Equal(successor.EffectiveParentTasks(), []string{replacementPlanID}) {
				t.Fatalf("successor lost new plan provenance: %v", successor.EffectiveParentTasks())
			}
			if got := state.FindTask(holder.ID).ProviderReservations; !slices.Equal(got, d89Reservations(successorID)) {
				t.Fatalf("later reservation=%v, want successor %s", got, successorID)
			}
			d89RequireRetargetAudit(t, state.FindTask(holder.ID), original.ID, successorID)
			if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
				t.Fatalf("replacement left invalid state: %v", err)
			}
		})
	}
}

func TestPlanReplacement_ReservationCrossOriginals(t *testing.T) {
	t.Parallel()
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		t.Run(order[0]+"-first", func(t *testing.T) {
			t.Parallel()
			a, b := d89Writer("a"), d89Writer("b")
			a.MaxOutputs, b.MaxOutputs = 3, 3
			b.ProviderReservations = d89Reservations(a.ID)
			root, statePath := setupPlanReplacementTest(t, d89Plan(order...), a, b)
			d89CompleteSprint(t, statePath)
			if err := statevalidate.ValidateState(readReplacementState(t, statePath), root, true, io.Discard); err != nil {
				t.Fatalf("invalid reproduction fixture: %v", err)
			}
			if _, err := Proceed(root, replacementPlanID, providerOpsTransition); err != nil {
				t.Fatalf("cross-original placement refused: %v", err)
			}
			state := readReplacementState(t, statePath)
			aID, bID := d89Successor(slices.Index(order, "a")), d89Successor(slices.Index(order, "b"))
			for _, id := range []string{b.ID, bID} {
				if got := state.FindTask(id).ProviderReservations; !slices.Equal(got, d89Reservations(aID)) {
					t.Fatalf("%s reservations=%v, want %s", id, got, aID)
				}
				d89RequireRetargetAudit(t, state.FindTask(id), a.ID, aID)
			}
			if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
				t.Fatalf("cross-original replacement left invalid state: %v", err)
			}
		})
	}
}

func d89RequireRetargetAudit(t *testing.T, task *models.Task, previous, current string) {
	t.Helper()
	for _, entry := range task.History {
		change, ok := entry.Extra["provider_reservation_retargeted"].(map[string]any)
		if entry.Event == models.TaskEventDependenciesRewritten && ok && change["previous_provider_task"] == previous && change["provider_task"] == current && change["transition"] == d89WriterTransition {
			if entry.Extra["operation"] != "plan-declared-replacement" || entry.Extra["rewrote_depends_on"] != false || entry.Agent == nil || *entry.Agent != "system" || entry.Reason == nil {
				t.Fatalf("incomplete reservation audit: %+v", entry)
			}
			return
		}
	}
	t.Fatalf("%s lacks reservation audit from %s to %s", task.ID, previous, current)
}

func TestPlanReplacement_ReservationWaitsForAllSuccessorWork(t *testing.T) {
	t.Parallel()
	original, holder := d89Writer("original"), d89Writer("later")
	original.MaxOutputs = 3
	holder.ProviderReservations = d89Reservations(original.ID)
	root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID), original, holder)
	d89CompleteSprint(t, statePath)
	if _, err := Proceed(root, replacementPlanID, providerOpsTransition); err != nil {
		t.Fatal(err)
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	requireHeld := func(wantHeld bool) {
		t.Helper()
		state := readReplacementState(t, statePath)
		unmet := models.UnmetProviderReservations(state.FindTask(holder.ID), state, resolver)
		if (len(unmet) != 0) != wantHeld {
			t.Fatalf("later writer held=%v, want %v: %v", len(unmet) != 0, wantHeld, unmet)
		}
	}
	requireHeld(true)
	// The third output is authored after the placement was transferred; it
	// must still belong to the wait, without any reservation rewrite.
	mergeTask(t, statePath, d89Successor(0), func(task *models.Task) {
		task.Output = []models.OutputEntry{providerOpsOutput(), providerOpsOutput(), providerOpsOutput()}
	})
	requireHeld(true)
	result, err := Proceed(root, d89Successor(0), d89WriterTransition)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ChildTaskIDs) != 3 {
		t.Fatalf("generated children=%v, want all three outputs", result.ChildTaskIDs)
	}
	for _, id := range result.ChildTaskIDs[:2] {
		mergeTask(t, statePath, id, nil)
		requireHeld(true)
	}
	mergeTask(t, statePath, result.ChildTaskIDs[2], nil)
	requireHeld(false)
}

func TestPlanReplacement_ReservationAliasesAndDeduplication(t *testing.T) {
	t.Parallel()
	old := providerOpsTask("old", "code-planning-pair", models.TaskStatusSuperseded)
	middle := providerOpsTask("middle", "code-planning-pair", models.TaskStatusSuperseded)
	original, holder := d89Writer("original"), d89Writer("later")
	old.SupersededBy, middle.SupersededBy = []string{middle.ID}, []string{original.ID}
	old.TransitionsExecuted = map[string]bool{"replanned": true}
	middle.Supersedes, original.Supersedes = &old.ID, &middle.ID
	reason := "replacement"
	old.RescopeReason, middle.RescopeReason = &reason, &reason
	middle.History = append(middle.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventReplacementCommitted, Extra: map[string]any{models.ReplacementTaskIDExtra: original.ID}})
	holder.ProviderReservations = append(d89Reservations(old.ID), d89Reservations(original.ID)...)
	root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID), old, middle, original, holder)
	d89CompleteSprint(t, statePath)
	before := readReplacementState(t, statePath)
	if got := models.EffectiveReservationProvider(before, old.ID); got != original.ID {
		t.Fatalf("mixed lineage fixture resolves to %s, want original", got)
	}
	if err := statevalidate.ValidateState(before, root, true, io.Discard); err != nil {
		t.Fatalf("invalid alias fixture: %v", err)
	}
	if _, err := Proceed(root, replacementPlanID, providerOpsTransition); err != nil {
		t.Fatal(err)
	}
	state := readReplacementState(t, statePath)
	if got := state.FindTask(holder.ID).ProviderReservations; !slices.Equal(got, d89Reservations(d89Successor(0))) {
		t.Fatalf("alias reservations did not collapse onto successor: %v", got)
	}
	d89RequireRetargetAudit(t, state.FindTask(holder.ID), old.ID, d89Successor(0))
	d89RequireRetargetAudit(t, state.FindTask(holder.ID), original.ID, d89Successor(0))
}

func TestPlanReplacement_ReservationSplitAndRollback(t *testing.T) {
	t.Parallel()
	for _, placement := range []string{"incoming", "outgoing", "cap", "unplaced"} {
		t.Run(placement, func(t *testing.T) {
			t.Parallel()
			original, holder, earlier := d89Writer("original"), d89Writer("later"), d89Writer("earlier")
			switch placement {
			case "incoming":
				holder.ProviderReservations = d89Reservations(original.ID)
			case "outgoing":
				original.ProviderReservations = d89Reservations(earlier.ID)
			case "cap":
				original.MaxOutputs = 3
			}
			root, statePath := setupPlanReplacementTest(t, d89Plan(original.ID, original.ID), original, holder, earlier)
			d89CompleteSprint(t, statePath)
			before := readReplacementState(t, statePath)
			_, err := Proceed(root, replacementPlanID, providerOpsTransition)
			if placement == "unplaced" {
				if err != nil {
					t.Fatalf("ordinary unplaced split refused: %v", err)
				}
				requireRetiredBy(t, readReplacementState(t, statePath), original.ID, d89Successor(0), d89Successor(1))
				return
			}
			if err == nil || !strings.Contains(err.Error(), "cannot split placed provider") {
				t.Fatalf("placed split error=%v", err)
			}
			requireNothingGenerated(t, before, readReplacementState(t, statePath))
		})
	}
	for _, refusal := range []string{"cycle", "typed-holder", "after-retire"} {
		t.Run(refusal, func(t *testing.T) {
			t.Parallel()
			original, holder := d89Writer("original"), d89Writer("later")
			original.MaxOutputs = 3
			holder.ProviderReservations = d89Reservations(original.ID)
			plan := d89Plan(original.ID)
			typed := d89Writer("typed")
			want := "injected after retirement"
			switch refusal {
			case "cycle":
				plan.Output[0].TaskDependsOn = []string{holder.ID}
				want = "cycle"
			case "typed-holder":
				typed.DescendantDependencies = []models.DescendantDependency{{AtTransition: d89WriterTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: original.ID, Transition: d89WriterTransition, Outputs: []int{0, 1, 2}}}}}
				want = "typed"
			}
			root, statePath := setupPlanReplacementTest(t, plan, original, holder, typed)
			d89CompleteSprint(t, statePath)
			if refusal == "after-retire" {
				bb := db.For(statePath)
				planReplacementTestHooks.Store(bb, planReplacementTestHook{afterRetire: func(string) error { return errors.New(want) }})
				t.Cleanup(func() { planReplacementTestHooks.Delete(bb) })
			}
			before := readReplacementState(t, statePath)
			_, err := Proceed(root, replacementPlanID, providerOpsTransition)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("expected %s refusal, got %v", refusal, err)
			}
			requireNothingGenerated(t, before, readReplacementState(t, statePath))
		})
	}
}

func TestPlanReplacement_ReservationRecoveryAndReplay(t *testing.T) {
	t.Parallel()
	a, b, earlier := d89Writer("a"), d89Writer("b"), d89Writer("earlier")
	a.MaxOutputs, b.MaxOutputs = 3, 3
	a.ProviderReservations = d89Reservations(earlier.ID)
	b.ProviderReservations = d89Reservations(a.ID)
	root, statePath := setupPlanReplacementTest(t, d89Plan(a.ID, b.ID), a, b, earlier)
	d89CompleteSprint(t, statePath)
	if _, err := Proceed(root, replacementPlanID, providerOpsTransition); err != nil {
		t.Fatal(err)
	}
	committed := readReplacementState(t, statePath)
	if _, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed); err != nil {
		t.Fatal(err)
	}
	if replayed := readReplacementState(t, statePath); !reflect.DeepEqual(committed.Tasks, replayed.Tasks) {
		t.Fatal("completed replay changed placement or audit")
	}
	// Simulate a lost child while preserving a later authorized cap/wait change
	// on its surviving sibling. Recovery must not reset that sibling.
	if err := db.New(statePath).Modify(func(state *models.State) error {
		survivor := state.FindTask(d89Successor(0))
		survivor.MaxOutputs, survivor.ProviderReservations = 2, nil
		state.Tasks = slices.DeleteFunc(state.Tasks, func(task models.Task) bool { return task.ID == d89Successor(1) })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := readReplacementState(t, statePath)
	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 {
		t.Fatalf("recovery refused: err=%v failures=%v", err, report.Failures)
	}
	state := readReplacementState(t, statePath)
	if !reflect.DeepEqual(*before.FindTask(d89Successor(0)), *state.FindTask(d89Successor(0))) {
		t.Fatal("recovery reset surviving child's authorized placement")
	}
	for _, id := range []string{a.ID, b.ID} {
		if !reflect.DeepEqual(*before.FindTask(id), *state.FindTask(id)) {
			t.Fatalf("recovery altered terminal original %s", id)
		}
	}
	recovered := state.FindTask(d89Successor(1))
	if recovered == nil || recovered.MaxOutputs != 3 || !slices.Equal(recovered.ProviderReservations, d89Reservations(d89Successor(0))) {
		t.Fatalf("recovered child lost placement: %+v", recovered)
	}
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("recovery left invalid state: %v", err)
	}
}

// D-172 (B-420 shape): the correction replaces a child that its parent's
// reservation selects. The parent stays live, so ADR-0196 transfers nothing.
// A unique successor carries the selected slot and the reservation with it
// (ADR-0214); a split has none, so the barrier must name the holder before
// generation.
const d172Holder = "later provider_reservations on parent at " + providerOpsTransition

func d172Tasks() (parent, child, holder models.Task) {
	parent = withPlanCheck(handoffPlan("parent", "architecture-pair"), models.PlanCheckPassed, "")
	parent.TransitionsExecuted = map[string]bool{providerOpsTransition: true}
	child = d89Writer(perSubtaskChildID(parent.ID, providerOpsTransition, 0))
	child.ParentTask = &parent.ID
	holder = d89Writer("later")
	holder.ProviderReservations = []models.ProviderReservation{{ProviderTask: parent.ID, Transition: providerOpsTransition}}
	return parent, child, holder
}

func d172Alerts(t *testing.T, root string) string {
	t.Helper()
	content, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(content)
}

func TestPlanReplacement_ReservationSelectingReplacedChildFollowsSuccessor(t *testing.T) {
	t.Parallel()
	parent, child, holder := d172Tasks()
	root, statePath := setupPlanReplacementTest(t, parent, child, holder, d89Plan(child.ID))

	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
		t.Fatalf("unique replacement refused: err=%v report=%+v", err, report)
	}
	state := readReplacementState(t, statePath)
	requireRetiredBy(t, state, child.ID, d89Successor(0))
	if !models.HoldsReservation(state.FindTask(holder.ID), parent.ID, providerOpsTransition) {
		t.Fatal("hand-off rewrote the parent reservation")
	}
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	if result := models.ResolveReservation(holder.ProviderReservations[0], state, resolver); result.Kind != models.DependencyUnsatisfiedPending || !slices.Equal(result.BlockingIDs, []string{d89Successor(0)}) {
		t.Fatalf("reservation = %+v, want pending on %s", result, d89Successor(0))
	}
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("hand-off left invalid state: %v", err)
	}
}

func TestPlanReplacement_ReservationSelectingSplitChildRefusesHandoff(t *testing.T) {
	t.Parallel()
	parent, child, holder := d172Tasks()
	root, statePath := setupPlanReplacementTest(t, parent, child, holder, d89Plan(child.ID, child.ID))
	before := readReplacementState(t, statePath)
	if err := statevalidate.ValidateState(before, root, true, io.Discard); err != nil {
		t.Fatalf("invalid reproduction fixture: %v", err)
	}

	first, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil {
		t.Fatal(err)
	}
	requireFailureNaming(t, first, d172Holder)
	state := readReplacementState(t, statePath)
	if state.FindTask(d89Successor(0)) != nil || state.FindTask(child.ID).Status != child.Status || !models.HoldsReservation(state.FindTask(holder.ID), parent.ID, providerOpsTransition) {
		t.Fatal("refusal committed candidate changes")
	}
	plan := state.FindTask(replacementPlanID)
	if got := handoffFailureCount(plan); got != 1 || plan.History[len(plan.History)-1].Extra["failure_class"] != string(handoffProviderRefusal) {
		t.Fatalf("refusal observations = %d, last %+v; want one %s observation", got, plan.History[len(plan.History)-1], handoffProviderRefusal)
	}
	if got := strings.Count(d172Alerts(t, root), "PLAN HANDOFF FAILED"); got != 1 {
		t.Fatalf("PLAN HANDOFF FAILED alerts = %d, want 1", got)
	}

	second, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(second.Results) != 0 || len(second.Failures) != 0 {
		t.Fatalf("unchanged refusal was retried: err=%v report=%+v", err, second)
	}
	requireNothingGenerated(t, state, readReplacementState(t, statePath))
	if got := strings.Count(d172Alerts(t, root), "PLAN HANDOFF FAILED"); got != 1 {
		t.Fatalf("unchanged refusal alerted again: %d alerts", got)
	}

	// Moving the placement behind the correction repairs the input.
	if err := db.For(statePath).Modify(func(s *models.State) error {
		s.FindTask(holder.ID).ProviderReservations = []models.ProviderReservation{{ProviderTask: replacementPlanID, Transition: providerOpsTransition}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	repaired, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(repaired.Failures) != 0 || !transitionedSources(t, repaired)[replacementPlanID] {
		t.Fatalf("repaired hand-off refused: err=%v report=%+v", err, repaired)
	}
	state = readReplacementState(t, statePath)
	requireRetiredBy(t, state, child.ID, d89Successor(0), d89Successor(1))
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("repaired hand-off left invalid state: %v", err)
	}
}

func TestPlanCheckPassRefusesReplacementStrandingReservation(t *testing.T) {
	t.Parallel()
	for _, holding := range []string{"parent of a split", "parent of a unique replacement", "original", "none"} {
		t.Run(holding, func(t *testing.T) {
			t.Parallel()
			parent, child, holder := d172Tasks()
			plan := d89Plan(child.ID)
			switch holding {
			case "parent of a split":
				plan = d89Plan(child.ID, child.ID)
			case "original":
				// ADR-0196 retargets it to the unique successor at hand-off.
				child.MaxOutputs = 3
				holder.ProviderReservations = d89Reservations(child.ID)
			case "none":
				holder.ProviderReservations = nil
			}
			// A unique successor carries the parent's selected slot (ADR-0214).
			root, statePath := setupD172PlanCheck(t, plan, parent, child, holder)
			beforeBytes := replacementBytes(t, statePath)

			_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: replacementPlanID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
			if holding != "parent of a split" {
				requireD172Passed(t, statePath, err)
				return
			}
			requireProviderOpsAtomicRefusal(t, statePath, beforeBytes, err, "cannot pass", child.ID, d172Holder, "reserve-provider")
		})
	}
}

// A typed holder that the same correction retires first no longer holds when
// the provider is retired, so the pass must not refuse what the hand-off runs.
func TestPlanCheckPassAdmitsHolderRetiredFirst(t *testing.T) {
	t.Parallel()
	provider, holder := d89Writer("provider"), d89Writer("holder")
	holder.DescendantDependencies = []models.DescendantDependency{{AtTransition: d89WriterTransition, ProviderDependencies: []models.ProviderDependency{{ProviderTask: provider.ID, Transition: d89WriterTransition, Outputs: []int{0}}}}}
	root, statePath := setupD172PlanCheck(t, d89Plan(holder.ID, provider.ID), holder, provider)

	_, err := RecordPlanCheck(root, PlanCheckInput{TaskID: replacementPlanID, Action: PlanCheckActionPass, Authority: orchestratorAuthority()})
	requireD172Passed(t, statePath, err)
	report, err := ExecuteTransitionsReportWith(root, "", AdmitReviewed)
	if err != nil || len(report.Failures) != 0 || !transitionedSources(t, report)[replacementPlanID] {
		t.Fatalf("passed hand-off refused: err=%v report=%+v", err, report)
	}
	if err := statevalidate.ValidateState(readReplacementState(t, statePath), root, true, io.Discard); err != nil {
		t.Fatalf("hand-off left invalid state: %v", err)
	}
}

// setupD172PlanCheck writes a valid replacement fixture whose plan is
// undispositioned, with the orchestrator and spec files plan-check needs.
func setupD172PlanCheck(t *testing.T, plan models.Task, tasks ...models.Task) (string, string) {
	t.Helper()
	plan.PlanCheck = nil
	root, statePath := setupPlanReplacementTest(t, append(tasks, plan)...)
	state := readReplacementState(t, statePath)
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	testhelpers.WriteInitialState(t, statePath, state)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := statevalidate.ValidateState(state, root, true, io.Discard); err != nil {
		t.Fatalf("invalid reproduction fixture: %v", err)
	}
	return root, statePath
}

func requireD172Passed(t *testing.T, statePath string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("pass refused: %v", err)
	}
	if got := readPlanCheck(t, statePath, replacementPlanID); got == nil || got.Verdict != models.PlanCheckPassed {
		t.Fatalf("plan check = %+v, want passed", got)
	}
}

func TestRetirementBarrierFollowsSameParentReplacement(t *testing.T) {
	t.Parallel()
	root, _ := setupPlanReplacementTest(t)
	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, shape := range []string{"live", "abandoned", "replace-task"} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			parent, child, holder := d172Tasks()
			tasks := []models.Task{parent, holder}
			switch shape {
			case "abandoned":
				child.Status = models.TaskStatusAbandoned
			case "replace-task":
				successor := d89Writer("successor")
				successor.ParentTask = &parent.ID
				successor.Supersedes = &child.ID
				child.Status = models.TaskStatusSuperseded
				child.SupersededBy = []string{successor.ID}
				child.History = append(child.History, models.TaskHistoryEntry{Event: models.TaskEventSuperseded, Time: time.Now().UTC(), Extra: map[string]any{models.ReplacementTaskIDExtra: successor.ID}})
				tasks = append(tasks, successor)
			}
			state := testhelpers.CreateValidState()
			state.Tasks = append(tasks, child)
			err := rejectReferencedProviderRetirement(state, resolver, child.ID, retirePermanently)
			if shape == "replace-task" {
				if err != nil {
					t.Fatalf("same-pair replacement keeps the slot, but the barrier refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), d172Holder) {
				t.Fatalf("barrier error = %v, want %q", err, d172Holder)
			}
		})
	}
}
