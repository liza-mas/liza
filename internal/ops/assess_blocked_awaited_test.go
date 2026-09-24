package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// awaitedFixture reproduces the shape that burned wakes in a MAS run: a
// BLOCKED plan whose dependency ("provider") is merged and has generated
// seven pending tasks, of which the plan really waits for the last one.
func awaitedFixture(t *testing.T) (root, stateFile string) {
	t.Helper()
	root, stateFile = assessmentIdempotencyFixture(t)
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		provider := state.FindTask("provider")
		provider.Status = models.TaskStatusMerged
		for i := 1; i <= 7; i++ {
			child := testhelpers.BuildTaskByStatus(fmt.Sprintf("gen-%d", i), models.TaskStatusReady, provider.Created)
			child.ParentTasks = []string{"provider"}
			child.SpecRef = state.Goal.SpecRef
			state.Tasks = append(state.Tasks, child)
		}
	})
	return root, stateFile
}

func modifyAwaitedState(t *testing.T, stateFile string, mutate func(*models.State)) {
	t.Helper()
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		mutate(state)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assessAwaiting(t *testing.T, root string, awaited ...string) *AssessBlockedResult {
	t.Helper()
	result, err := AssessBlockedWithOptions(root, "target", "waits on the generated UI", "orchestrator-1", AssessBlockedOptions{AwaitedTasks: awaited})
	if err != nil {
		t.Fatalf("assess-blocked --awaits %v: %v", awaited, err)
	}
	return result
}

func targetActionable(t *testing.T, stateFile string) bool {
	t.Helper()
	state := readStateForTest(t, stateFile)
	return isTaskActionableSinceAssessment(state.FindTask("target"), state)
}

func setTaskStatus(t *testing.T, stateFile, taskID string, status models.TaskStatus) {
	t.Helper()
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		state.FindTask(taskID).Status = status
	})
}

// Six unrelated generated tasks merging used to wake the plan six times; with
// the awaited set only the task it waits for does.
func TestAwaitedSetWakesOnlyWhenAwaitedWorkSettles(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-7")

	for i := 1; i <= 6; i++ {
		setTaskStatus(t, stateFile, fmt.Sprintf("gen-%d", i), models.TaskStatusMerged)
		if targetActionable(t, stateFile) {
			t.Fatalf("merge of unrelated gen-%d woke the waiting task", i)
		}
	}
	setTaskStatus(t, stateFile, "gen-7", models.TaskStatusImplementing)
	if targetActionable(t, stateFile) {
		t.Fatal("intermediate progress of the awaited task woke the waiting task")
	}
	setTaskStatus(t, stateFile, "gen-7", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("merge of the awaited task did not wake the waiting task")
	}
}

// Without an awaited set the same unrelated merge still wakes the task: the
// default projection is unchanged.
func TestNoAwaitedSetKeepsDescendantWakes(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root)
	setTaskStatus(t, stateFile, "gen-1", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("descendant merge did not wake a task without an awaited set")
	}
}

func TestAwaitedSetWakesOnFailureAndHumanNotes(t *testing.T) {
	for _, change := range []string{"awaited blocked", "human note"} {
		t.Run(change, func(t *testing.T) {
			root, stateFile := awaitedFixture(t)
			assessAwaiting(t, root, "gen-6", "gen-7")
			modifyAwaitedState(t, stateFile, func(state *models.State) {
				switch change {
				case "awaited blocked":
					// One of two awaited tasks failing is not hidden by the other
					// still running.
					state.FindTask("gen-6").Status = models.TaskStatusBlocked
				case "human note":
					at := lastOrchestratorAssessment(state.FindTask("target")).Time
					state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: "target", Timestamp: at, Message: "Reassess"})
				}
			})
			if !targetActionable(t, stateFile) {
				t.Fatalf("%s did not wake the waiting task", change)
			}
		})
	}
}

// The awaited set replaces only the descendants input: a direct dependency's
// outcome still wakes the task.
func TestAwaitedSetKeepsDirectDependencyWakes(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-7")
	setTaskStatus(t, stateFile, "provider", models.TaskStatusAbandoned)
	if !targetActionable(t, stateFile) {
		t.Fatal("direct dependency outcome change did not wake a task with an awaited set")
	}
}

// An awaited task that was split is followed to its replacements: only they
// matter, the split itself counts as awaited work that is still pending, and
// it is satisfied only once every replacement has merged.
func TestAwaitedSetFollowsReplacements(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		split := state.FindTask("gen-7")
		split.Status = models.TaskStatusSuperseded
		split.SupersededBy = []string{"gen-7a", "gen-7b"}
		for _, id := range split.SupersededBy {
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, split.Created))
		}
	})
	assessAwaiting(t, root, "gen-7")

	setTaskStatus(t, stateFile, "gen-1", models.TaskStatusMerged)
	if targetActionable(t, stateFile) {
		t.Fatal("unrelated merge woke a task awaiting a split task")
	}
	setTaskStatus(t, stateFile, "gen-7a", models.TaskStatusMerged)
	if targetActionable(t, stateFile) {
		t.Fatal("one of two replacements merging woke the waiting task")
	}
	setTaskStatus(t, stateFile, "gen-7b", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("the last replacement of the awaited task merging did not wake the waiting task")
	}
}

// Repeated and whitespace-padded IDs pass payload preflight, so the mutation
// merges them into one set instead of rejecting what preflight accepted.
func TestAwaitedSetMergesRepeatedIDs(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	result := assessAwaiting(t, root, "gen-7", " gen-7 ", "gen-6", "gen-7")
	if !slices.Equal(result.AwaitedTasks, []string{"gen-6", "gen-7"}) {
		t.Fatalf("result awaited set = %v, want [gen-6 gen-7]", result.AwaitedTasks)
	}
	task := readStateForTest(t, stateFile).FindTask("target")
	if got, _ := awaitedTasksFrom(lastOrchestratorAssessment(task)); !slices.Equal(got, []string{"gen-6", "gen-7"}) {
		t.Fatalf("stored awaited set = %v, want [gen-6 gen-7]", got)
	}
	if again := assessAwaiting(t, root, "gen-6", "gen-7"); again.Outcome != models.LifecycleNoChange {
		t.Fatalf("same set without repeats outcome = %s, want NO_CHANGE", again.Outcome)
	}
}

func TestAwaitedSetPersistenceAndClearing(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6")

	if result := assessAwaiting(t, root, "gen-6"); result.Outcome != models.LifecycleNoChange {
		t.Fatalf("unchanged assessment outcome = %s, want NO_CHANGE", result.Outcome)
	}
	// The same note with a different set must be recorded, not suppressed.
	if result := assessAwaiting(t, root, "gen-7"); result.Outcome != models.LifecycleCompleted {
		t.Fatalf("changed awaited set outcome = %s, want COMPLETED", result.Outcome)
	}
	task := readStateForTest(t, stateFile).FindTask("target")
	if got, _ := awaitedTasksFrom(lastOrchestratorAssessment(task)); !slices.Equal(got, []string{"gen-7"}) {
		t.Fatalf("latest awaited set = %v, want [gen-7]", got)
	}

	// An explicit clear drops the set, and only the latest entry ever
	// carries one.
	if result, err := assessTarget(root, "waits on the generated UI", AssessBlockedOptions{ClearAwaited: true}); err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("clearing assessment = %+v, %v; want COMPLETED", result, err)
	}
	task = readStateForTest(t, stateFile).FindTask("target")
	for _, entry := range task.History {
		if _, ok := entry.Extra[AwaitedTasksExtraKey]; ok {
			t.Fatalf("assessment at %s still carries an awaited set", entry.Time)
		}
	}
}

func TestAwaitedSetValidation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		awaited []string
		want    string
	}{
		{"self", []string{"target"}, "cannot await itself"},
		{"missing", []string{"nope"}, `"nope" does not exist`},
		{"blank", []string{" "}, "/awaited_tasks/0 must be a non-blank task ID"},
		{"settled", []string{"provider", "gen-1"}, "no longer pending: provider (MERGED)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, _ := awaitedFixture(t)
			_, err := AssessBlockedWithOptions(root, "target", "note", "orchestrator-1", AssessBlockedOptions{AwaitedTasks: tt.awaited})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

// Validation runs before the no-change comparison: an otherwise unchanged
// assessment is rejected once its awaited task has settled.
func TestAwaitedSetUnchangedAssessmentRejectedAfterSettling(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-7")
	setTaskStatus(t, stateFile, "gen-7", models.TaskStatusMerged)
	_, err := AssessBlockedWithOptions(root, "target", "waits on the generated UI", "orchestrator-1", AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}})
	if err == nil || !strings.Contains(err.Error(), "no longer pending: gen-7 (MERGED)") {
		t.Fatalf("error = %v, want gen-7 named as settled", err)
	}
}

// An exact request replay is answered from its receipt even after the awaited
// task settled.
func TestAwaitedSetReplayAfterSettling(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	transition := models.TaskTransitionID(readStateForTest(t, stateFile).FindTask("target"))
	opts := AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}, Request: LifecycleRequestOptions{RequestID: "await-1", ExpectedTransition: transition}}
	if _, err := AssessBlockedWithOptions(root, "target", "note", "orchestrator-1", opts); err != nil {
		t.Fatal(err)
	}
	setTaskStatus(t, stateFile, "gen-7", models.TaskStatusMerged)
	result, err := AssessBlockedWithOptions(root, "target", "note", "orchestrator-1", opts)
	if err != nil || result.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("replay = %+v, %v; want ALREADY_COMPLETED", result, err)
	}
}

func TestAwaitedSetDeadlockGuard(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*models.State)
		// cycle is the rejection's path; empty when the wait is accepted.
		cycle string
	}{
		{
			name: "awaited task depends on the waiter",
			mutate: func(state *models.State) {
				state.FindTask("gen-7").DependsOn = []string{"target"}
			},
			cycle: "target -> gen-7 -> target",
		},
		{
			// A awaits C, C depends on B, and B is BLOCKED awaiting A.
			name: "transitive through a blocked task's awaited set",
			mutate: func(state *models.State) {
				blocked := state.FindTask("gen-6")
				blocked.Status = models.TaskStatusBlocked
				blocked.History = append(blocked.History, awaitingAssessment("target"))
				state.FindTask("gen-7").DependsOn = []string{"gen-6"}
			},
			cycle: "target -> gen-7 -> gen-6 -> target",
		},
		{
			// The awaited task's prerequisite was replaced by the waiter, so
			// the cycle closes through supersession, not a dependency edge.
			name: "prerequisite superseded by the waiter",
			mutate: func(state *models.State) {
				replaced := testhelpers.BuildTaskByStatus("x", models.TaskStatusSuperseded, state.FindTask("gen-7").Created)
				replaced.SupersededBy = []string{"target"}
				state.Tasks = append(state.Tasks, replaced)
				state.FindTask("gen-7").DependsOn = []string{"x"}
			},
			cycle: "target -> gen-7 -> x -> target",
		},
		{
			// The same shape after B was unblocked: its last assessment still
			// names A, but nothing waits through it any more.
			name: "stale awaited set after unblock",
			mutate: func(state *models.State) {
				state.FindTask("gen-6").History = append(state.FindTask("gen-6").History, awaitingAssessment("target"))
				state.FindTask("gen-7").DependsOn = []string{"gen-6"}
			},
		},
		{
			name: "chain through merged work",
			mutate: func(state *models.State) {
				state.FindTask("gen-6").Status = models.TaskStatusMerged
				state.FindTask("gen-6").DependsOn = []string{"target"}
				state.FindTask("gen-7").DependsOn = []string{"gen-6"}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, stateFile := awaitedFixture(t)
			modifyAwaitedState(t, stateFile, tt.mutate)
			_, err := AssessBlockedWithOptions(root, "target", "note", "orchestrator-1", AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}})
			if tt.cycle == "" {
				if err != nil {
					t.Fatalf("error = %v, want the wait accepted", err)
				}
				return
			}
			if want := "awaiting would deadlock: " + tt.cycle; err == nil || !strings.HasSuffix(err.Error(), want) {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func awaitingAssessment(ids ...string) models.TaskHistoryEntry {
	agent := "orchestrator-1"
	return models.TaskHistoryEntry{
		Event: models.TaskEventOrchestratorAssessment,
		Agent: &agent,
		Extra: map[string]any{AwaitedTasksExtraKey: ids},
	}
}

// The write-time guard cannot see later graph edits. When one makes the wait
// lead back to the waiter, the reader drops the set, so the task wakes
// instead of sleeping forever.
func TestAwaitedSetReaderFailsOpen(t *testing.T) {
	for _, change := range []string{"deadlock formed later", "malformed set"} {
		t.Run(change, func(t *testing.T) {
			root, stateFile := awaitedFixture(t)
			modifyAwaitedState(t, stateFile, func(state *models.State) {
				state.FindTask("gen-7").DependsOn = []string{"gen-6"}
			})
			assessAwaiting(t, root, "gen-7")
			modifyAwaitedState(t, stateFile, func(state *models.State) {
				switch change {
				case "deadlock formed later":
					// One step beyond the awaited task, so its own outcome
					// record, and with it the digest, stays the same.
					state.FindTask("gen-6").DependsOn = []string{"target"}
				case "malformed set":
					lastOrchestratorAssessment(state.FindTask("target")).Extra[AwaitedTasksExtraKey] = []any{"gen-7", 42}
				}
			})
			if !targetActionable(t, stateFile) {
				t.Fatalf("%s left the waiting task silent", change)
			}
		})
	}
}

// Without an awaited set the fingerprint material keeps exactly its original
// six inputs, so digests recorded before awaited sets existed stay valid.
func TestAssessmentFingerprintWithoutAwaitedSetIsUnchanged(t *testing.T) {
	_, stateFile := awaitedFixture(t)
	state := readStateForTest(t, stateFile)
	task := state.FindTask("target")
	candidate := AssessmentFingerprintCandidate{Reason: "blocked", Note: "note"}

	data, err := json.Marshal(map[string]any{
		"self": map[string]any{
			"status":                       task.Status,
			"non_assessment_history_count": assessmentHistoryCount(task),
			"depends_on":                   uniqueSortedStrings(task.DependsOn),
		},
		"blocker": map[string]any{
			"reason":         "blocked",
			"questions":      normalizeAssessmentStrings(nil),
			"repair_request": normalizeAssessmentRepair(nil),
		},
		"disposition":  "note",
		"dependencies": assessmentDependencies(state, task.DependsOn),
		"descendants":  assessmentDescendants(state, task),
		"human":        0,
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	want := hex.EncodeToString(digest[:])
	for _, awaited := range [][]string{nil, {}} {
		candidate.Awaited = awaited
		if got := BuildAssessmentFingerprint(state, task, candidate); got != want {
			t.Fatalf("fingerprint with awaited=%#v changed the original material", awaited)
		}
	}
}

// D70 regressions: an awaited set is one all-of wait, and it outlives a
// re-assessment that does not restate it.

func assessTarget(root, note string, opts AssessBlockedOptions) (*AssessBlockedResult, error) {
	return AssessBlockedWithOptions(root, "target", note, "orchestrator-1", opts)
}

func storedAwaited(t *testing.T, stateFile string) []string {
	t.Helper()
	got, _ := awaitedTasksFrom(lastOrchestratorAssessment(readStateForTest(t, stateFile).FindTask("target")))
	return got
}

func addTargetNote(t *testing.T, stateFile string) {
	t.Helper()
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		at := lastOrchestratorAssessment(state.FindTask("target")).Time
		state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: "target", Timestamp: at, Message: "Reassess"})
	})
}

func targetTransition(t *testing.T, stateFile string) string {
	t.Helper()
	return models.TaskTransitionID(readStateForTest(t, stateFile).FindTask("target"))
}

// appendNewBlockedEpisode unblocks and re-blocks taskID at the same instant as
// its latest assessment, so only history order separates the episodes.
func appendNewBlockedEpisode(state *models.State, taskID string) {
	task := state.FindTask(taskID)
	at := lastOrchestratorAssessment(task).Time
	task.History = append(task.History,
		models.TaskHistoryEntry{Time: at, Event: models.TaskEventUnblocked},
		models.TaskHistoryEntry{Time: at, Event: models.TaskEventBlocked},
	)
}

// R1: a hold on two tasks wakes when both have settled, not once per task.
func TestAwaitedSetWakesOnceWhenAllSettle(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6", "gen-7")
	setTaskStatus(t, stateFile, "gen-6", models.TaskStatusMerged)
	if targetActionable(t, stateFile) {
		t.Fatal("one of two awaited tasks merging woke the waiting task; the hold needs both")
	}
	setTaskStatus(t, stateFile, "gen-7", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("the last awaited task merging did not wake the waiting task")
	}
}

// R2: the incident shape. The awaited fix tasks are also direct dependencies,
// so their per-task dependency records must not wake the hold either.
func TestAwaitedSetCoversAwaitedDependencies(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		target := state.FindTask("target")
		for _, id := range []string{"fix-a", "fix-b"} {
			fix := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, target.Created)
			fix.SpecRef = state.Goal.SpecRef
			state.Tasks = append(state.Tasks, fix)
		}
		// Appending may move the tasks slice; edit the target through a fresh lookup.
		state.FindTask("target").DependsOn = append(target.DependsOn, "fix-a", "fix-b")
	})
	assessAwaiting(t, root, "fix-a", "fix-b")
	setTaskStatus(t, stateFile, "fix-a", models.TaskStatusMerged)
	if targetActionable(t, stateFile) {
		t.Fatal("an awaited dependency merging woke the waiting task while the other is pending")
	}
	setTaskStatus(t, stateFile, "fix-b", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("the last awaited dependency merging did not wake the waiting task")
	}
}

// R3: a re-check without --awaits keeps the set instead of reverting to
// generated-descendant wakes.
func TestAssessmentWithoutAwaitsCarriesSetForward(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6", "gen-7")
	result, err := assessTarget(root, "re-checked: still waiting", AssessBlockedOptions{})
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("re-check = %+v, %v; want COMPLETED", result, err)
	}
	want := []string{"gen-6", "gen-7"}
	if !slices.Equal(result.AwaitedTasks, want) || !result.AwaitedCarried {
		t.Fatalf("result awaited=%v carried=%v, want %v carried", result.AwaitedTasks, result.AwaitedCarried, want)
	}
	if got := storedAwaited(t, stateFile); !slices.Equal(got, want) {
		t.Fatalf("stored awaited set = %v, want %v", got, want)
	}
	setTaskStatus(t, stateFile, "gen-1", models.TaskStatusMerged)
	if targetActionable(t, stateFile) {
		t.Fatal("unrelated generated work woke the task after a re-check without --awaits")
	}
}

// R4: satisfied members leave the carried set; the rest stays awaited.
func TestCarriedSetDropsSatisfiedMembers(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6", "gen-7")
	setTaskStatus(t, stateFile, "gen-6", models.TaskStatusMerged)
	addTargetNote(t, stateFile)
	result, err := assessTarget(root, "human note read; still waiting", AssessBlockedOptions{})
	if err != nil {
		t.Fatalf("re-check: %v", err)
	}
	if got := storedAwaited(t, stateFile); !slices.Equal(got, []string{"gen-7"}) || !result.AwaitedCarried {
		t.Fatalf("stored awaited set = %v carried=%v, want [gen-7] carried", got, result.AwaitedCarried)
	}
}

// R5: a carried set with a failed member is rejected, not silently kept or
// dropped, and nothing is recorded.
func TestCarriedSetWithFailedMemberRejected(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6", "gen-7")
	setTaskStatus(t, stateFile, "gen-6", models.TaskStatusBlocked)
	before := len(readStateForTest(t, stateFile).FindTask("target").History)
	_, err := assessTarget(root, "re-checked", AssessBlockedOptions{})
	if err == nil || !strings.Contains(err.Error(), "gen-6") || !strings.Contains(err.Error(), "--clear-awaits") {
		t.Fatalf("error = %v, want gen-6 named with the --clear-awaits remedy", err)
	}
	if after := len(readStateForTest(t, stateFile).FindTask("target").History); after != before {
		t.Fatalf("rejected re-check appended history: %d -> %d entries", before, after)
	}
}

// R6: an explicit clear drops the set; clearing and naming a set conflict.
func TestClearAwaitsDropsSet(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6")
	if _, err := assessTarget(root, "waits on the generated UI", AssessBlockedOptions{ClearAwaited: true, AwaitedTasks: []string{"gen-7"}}); err == nil {
		t.Fatal("clearing and declaring an awaited set in one assessment was accepted")
	}
	result, err := assessTarget(root, "no longer waiting on named work", AssessBlockedOptions{ClearAwaited: true})
	if err != nil || result.Outcome != models.LifecycleCompleted || result.AwaitedCarried {
		t.Fatalf("clear = %+v, %v; want COMPLETED without a carried set", result, err)
	}
	if got := storedAwaited(t, stateFile); len(got) != 0 {
		t.Fatalf("stored awaited set after clear = %v", got)
	}
	setTaskStatus(t, stateFile, "gen-1", models.TaskStatusMerged)
	if !targetActionable(t, stateFile) {
		t.Fatal("after clearing, a descendant merge did not wake the task")
	}
}

// R7: the carry never crosses into a new BLOCKED episode, even when the
// unblock and re-block share the assessment's timestamp.
func TestCarryStopsAtNewBlockedEpisode(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	assessAwaiting(t, root, "gen-6", "gen-7")
	modifyAwaitedState(t, stateFile, func(state *models.State) { appendNewBlockedEpisode(state, "target") })
	result, err := assessTarget(root, "blocked again", AssessBlockedOptions{})
	if err != nil {
		t.Fatalf("assessment in the new episode: %v", err)
	}
	if got := storedAwaited(t, stateFile); len(got) != 0 || result.AwaitedCarried {
		t.Fatalf("previous episode's set carried into a new one: stored=%v carried=%v", got, result.AwaitedCarried)
	}
}

// R8: a dependency split into x and y, with x awaited alongside gen-7. x is
// covered by the set; y is not, so y's outcome still wakes the task.
func TestAwaitedSetCoversOverlappingReplacementPaths(t *testing.T) {
	fixture := func(t *testing.T) (string, string) {
		root, stateFile := awaitedFixture(t)
		modifyAwaitedState(t, stateFile, func(state *models.State) {
			target := state.FindTask("target")
			dep := testhelpers.BuildTaskByStatus("dep", models.TaskStatusSuperseded, target.Created)
			dep.SupersededBy = []string{"x", "y"}
			dep.SpecRef = state.Goal.SpecRef
			state.Tasks = append(state.Tasks, dep)
			for _, id := range dep.SupersededBy {
				replacement := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, target.Created)
				replacement.SpecRef = state.Goal.SpecRef
				state.Tasks = append(state.Tasks, replacement)
			}
			state.FindTask("target").DependsOn = append(target.DependsOn, "dep")
		})
		assessAwaiting(t, root, "gen-7", "x")
		return root, stateFile
	}
	t.Run("covered replacement is partial progress", func(t *testing.T) {
		_, stateFile := fixture(t)
		setTaskStatus(t, stateFile, "x", models.TaskStatusMerged)
		if targetActionable(t, stateFile) {
			t.Fatal("covered replacement x merging woke the task while gen-7 is pending")
		}
		setTaskStatus(t, stateFile, "gen-7", models.TaskStatusMerged)
		if !targetActionable(t, stateFile) {
			t.Fatal("completing the awaited set did not wake the task")
		}
	})
	t.Run("uncovered replacement still wakes", func(t *testing.T) {
		_, stateFile := fixture(t)
		setTaskStatus(t, stateFile, "y", models.TaskStatusBlocked)
		if !targetActionable(t, stateFile) {
			t.Fatal("uncovered replacement y blocking did not wake the task")
		}
	})
}

// R9: the deadlock search follows another BLOCKED task's awaited set only in
// that task's current episode (closes ADR-0157's stale-set limit).
func TestDeadlockGuardIgnoresStaleEpisodeSet(t *testing.T) {
	// gen-6 is BLOCKED awaiting target; gen-7 is pending and depends on gen-6.
	shape := func(state *models.State) {
		other := state.FindTask("gen-6")
		other.Status = models.TaskStatusBlocked
		other.History = append(other.History, awaitingAssessment("target"))
		state.FindTask("gen-7").DependsOn = []string{"gen-6"}
	}
	t.Run("predicate", func(t *testing.T) {
		_, stateFile := awaitedFixture(t)
		modifyAwaitedState(t, stateFile, shape)
		if _, ok := awaitLeadsBackTo(readStateForTest(t, stateFile), "target", []string{"gen-7"}); !ok {
			t.Fatal("current-episode awaited set was not followed")
		}
		modifyAwaitedState(t, stateFile, func(state *models.State) { appendNewBlockedEpisode(state, "gen-6") })
		if path, ok := awaitLeadsBackTo(readStateForTest(t, stateFile), "target", []string{"gen-7"}); ok {
			t.Fatalf("stale awaited set was followed: %v", path)
		}
	})
	t.Run("admission", func(t *testing.T) {
		root, stateFile := awaitedFixture(t)
		modifyAwaitedState(t, stateFile, shape)
		if _, err := assessTarget(root, "note", AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}}); err == nil || !strings.Contains(err.Error(), "awaiting would deadlock") {
			t.Fatalf("error = %v, want the current-episode cycle rejected", err)
		}
		modifyAwaitedState(t, stateFile, func(state *models.State) { appendNewBlockedEpisode(state, "gen-6") })
		if _, err := assessTarget(root, "note", AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}}); err != nil {
			t.Fatalf("error = %v, want the wait accepted once gen-6's set is stale", err)
		}
		// The pending guard still refuses to await BLOCKED work directly.
		if _, err := assessTarget(root, "note", AssessBlockedOptions{AwaitedTasks: []string{"gen-6"}}); err == nil || !strings.Contains(err.Error(), "no longer pending: gen-6 (BLOCKED)") {
			t.Fatalf("error = %v, want gen-6 rejected as no longer pending", err)
		}
	})
}

// R10: request identity holds only explicit inputs. A carried set is derived
// from state, so a bare request still replays after that set has changed.
func TestCarriedSetStaysOutOfRequestIdentity(t *testing.T) {
	root, stateFile := awaitedFixture(t)
	explicit := AssessBlockedOptions{AwaitedTasks: []string{"gen-6", "gen-7"}, Request: LifecycleRequestOptions{RequestID: "await-explicit", ExpectedTransition: targetTransition(t, stateFile)}}
	if _, err := assessTarget(root, "waits on the generated UI", explicit); err != nil {
		t.Fatal(err)
	}
	bare := AssessBlockedOptions{Request: LifecycleRequestOptions{RequestID: "await-bare", ExpectedTransition: targetTransition(t, stateFile)}}
	second, err := assessTarget(root, "re-checked", bare)
	if err != nil || !second.AwaitedCarried {
		t.Fatalf("bare re-check = %+v, %v; want the set carried", second, err)
	}
	setTaskStatus(t, stateFile, "gen-6", models.TaskStatusMerged)
	for name, request := range map[string]AssessBlockedOptions{"bare": bare, "explicit": explicit} {
		note := map[string]string{"bare": "re-checked", "explicit": "waits on the generated UI"}[name]
		result, err := assessTarget(root, note, request)
		if err != nil || result.Outcome != models.LifecycleAlreadyCompleted {
			t.Fatalf("%s replay = %+v, %v; want ALREADY_COMPLETED", name, result, err)
		}
	}
}

// R11: a member whose replacement path has failed work cannot complete an
// all-of wait without reassessment, so it is refused, explicit or carried.
func TestAwaitedSetRejectsFailedBranch(t *testing.T) {
	split := func(state *models.State, secondBranch models.TaskStatus) {
		gen7 := state.FindTask("gen-7")
		gen7.Status = models.TaskStatusSuperseded
		gen7.SupersededBy = []string{"gen-7a", "gen-7b"}
		created := gen7.Created // gen7 may be stale once the slice grows
		for id, status := range map[string]models.TaskStatus{"gen-7a": models.TaskStatusReady, "gen-7b": secondBranch} {
			replacement := testhelpers.BuildTaskByStatus(id, status, created)
			replacement.SpecRef = state.Goal.SpecRef
			state.Tasks = append(state.Tasks, replacement)
		}
	}
	t.Run("explicit", func(t *testing.T) {
		root, stateFile := awaitedFixture(t)
		modifyAwaitedState(t, stateFile, func(state *models.State) { split(state, models.TaskStatusBlocked) })
		if _, err := assessTarget(root, "note", AssessBlockedOptions{AwaitedTasks: []string{"gen-7"}}); err == nil || !strings.Contains(err.Error(), "gen-7b (BLOCKED)") {
			t.Fatalf("error = %v, want gen-7b named as failed work", err)
		}
		if _, err := assessTarget(root, "note", AssessBlockedOptions{AwaitedTasks: []string{"gen-7a"}}); err != nil {
			t.Fatalf("awaiting the pending replacement directly: %v", err)
		}
	})
	t.Run("carried", func(t *testing.T) {
		root, stateFile := awaitedFixture(t)
		modifyAwaitedState(t, stateFile, func(state *models.State) { split(state, models.TaskStatusReady) })
		assessAwaiting(t, root, "gen-6", "gen-7")
		setTaskStatus(t, stateFile, "gen-7b", models.TaskStatusBlocked)
		addTargetNote(t, stateFile)
		_, err := assessTarget(root, "re-checked", AssessBlockedOptions{})
		if err == nil || !strings.Contains(err.Error(), "gen-7b (BLOCKED)") || !strings.Contains(err.Error(), "--awaits gen-6") || !strings.Contains(err.Error(), "--clear-awaits") {
			t.Fatalf("error = %v, want gen-7b named with --awaits gen-6 and --clear-awaits remedies", err)
		}
	})
}
