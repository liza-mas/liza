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
// matter, and the split itself counts as awaited work that is still pending.
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
	if !targetActionable(t, stateFile) {
		t.Fatal("a replacement of the awaited task merging did not wake the waiting task")
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

	// An assessment without --awaits clears the set, and only the latest
	// entry ever carries one.
	if result := assessAwaiting(t, root); result.Outcome != models.LifecycleCompleted {
		t.Fatalf("clearing assessment outcome = %s, want COMPLETED", result.Outcome)
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
