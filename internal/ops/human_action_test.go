package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D65 / ADR-0172: the human ask of a blocked episode, as mark-blocked records
// it and assess-blocked sets, carries, replaces and clears it.

const testHumanAsk = "grant the CI deploy key read access to the fixtures repo, then add a human note"

func markBlockedFixture(t *testing.T) (root, stateFile string) {
	t.Helper()
	root = t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ = testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())}
	testhelpers.WriteInitialState(t, stateFile, state)
	return root, stateFile
}

func TestMarkBlockedWithOptions_HumanActionRecordedOnTheBlockedEntry(t *testing.T) {
	t.Parallel()
	root, stateFile := markBlockedFixture(t)

	_, err := MarkBlockedWithOptions(root, "task-1", "fixtures repo unreadable", []string{"Who grants access?"}, "coder-1",
		MarkBlockedOptions{HumanAction: testHumanAsk})
	if err != nil {
		t.Fatalf("MarkBlockedWithOptions() error: %v", err)
	}

	task := readStateForTest(t, stateFile).FindTask("task-1")
	entry := task.History[len(task.History)-1]
	if entry.Event != models.TaskEventBlocked || models.AwaitingHumanAskOf(&entry) != testHumanAsk {
		t.Fatalf("last entry = %s %v, want blocked with the ask", entry.Event, entry.Extra)
	}
	if human, ok := models.CurrentAwaitingHuman(task); !ok || human.Ask != testHumanAsk {
		t.Fatalf("CurrentAwaitingHuman() = %+v, %v", human, ok)
	}
}

func TestMarkBlocked_WithoutHumanActionIsAgentOwned(t *testing.T) {
	t.Parallel()
	root, stateFile := markBlockedFixture(t)

	if _, err := MarkBlocked(root, "task-1", "spec ambiguity", []string{"Which format?"}, "coder-1"); err != nil {
		t.Fatalf("MarkBlocked() error: %v", err)
	}

	task := readStateForTest(t, stateFile).FindTask("task-1")
	if entry := task.History[len(task.History)-1]; entry.Extra[models.AwaitingHumanExtraKey] != nil {
		t.Fatalf("blocked entry extra = %v, want no ask", entry.Extra)
	}
	if _, ok := models.CurrentAwaitingHuman(task); ok {
		t.Fatal("agent-owned block reads as awaiting a human")
	}
}

func TestMarkBlockedWithOptions_RejectsMalformedHumanAction(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ ask, want string }{
		"blank":      {"   ", "must not be blank"},
		"multi-line": {"first\nsecond", "must be a single line"},
		"oversized":  {strings.Repeat("a", 4097), "must be at most 4096 bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			root, stateFile := markBlockedFixture(t)
			before := readStateForTest(t, stateFile).FindTask("task-1")

			_, err := MarkBlockedWithOptions(root, "task-1", "blocked", []string{"q?"}, "coder-1", MarkBlockedOptions{HumanAction: tc.ask})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if after := readStateForTest(t, stateFile).FindTask("task-1"); after.Status != before.Status || len(after.History) != len(before.History) {
				t.Fatal("rejected call changed the task")
			}
		})
	}
}

// The ask is part of the request identity only when present, so a request
// without it keeps the digest earlier versions computed.
func TestMarkBlockedPayloadIdentityUnchangedWithoutHumanAction(t *testing.T) {
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, time.Now().UTC())
	legacy := struct {
		Reason       string
		Questions    []string
		Repair       *models.RepairRequest
		Dependencies []string
	}{"r", []string{"q"}, nil, nil}
	current := struct {
		Reason       string
		Questions    []string
		Repair       *models.RepairRequest
		Dependencies []string
		HumanAction  string `json:",omitempty"`
	}{"r", []string{"q"}, nil, nil, ""}
	a, errA := NewLifecycleRequest("mark-blocked", &task, "coder-1", nil, LifecycleRequestOptions{}, legacy)
	b, errB := NewLifecycleRequest("mark-blocked", &task, "coder-1", nil, LifecycleRequestOptions{}, current)
	if errA != nil || errB != nil || a.PayloadDigest != b.PayloadDigest {
		t.Fatalf("digests = %q / %q (%v, %v), want equal", a.PayloadDigest, b.PayloadDigest, errA, errB)
	}
}

func targetAsk(t *testing.T, stateFile string) (string, bool) {
	t.Helper()
	human, ok := models.CurrentAwaitingHuman(readStateForTest(t, stateFile).FindTask("target"))
	return human.Ask, ok
}

func TestAssessBlocked_HumanActionSetCarryReplaceClear(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)

	// Set on an agent-owned episode.
	result, err := assessTarget(root, "needs credentials", AssessBlockedOptions{HumanAction: testHumanAsk})
	if err != nil || result.Outcome != models.LifecycleCompleted || result.HumanAction != testHumanAsk {
		t.Fatalf("set: %+v %v", result, err)
	}
	if ask, ok := targetAsk(t, stateFile); !ok || ask != testHumanAsk {
		t.Fatalf("after set: ask %q %v", ask, ok)
	}
	// The wake reader agrees with the writer: nothing material changed.
	if targetActionable(t, stateFile) {
		t.Fatal("task actionable right after the assessment that set the ask")
	}

	// A note-only re-check carries the ask: unchanged, nothing appended.
	artifacts := metadataArtifactSnapshot(t, root)
	result, err = assessTarget(root, "needs credentials", AssessBlockedOptions{})
	assertAssessmentNoChange(t, root, artifacts, result, err)
	if result.HumanAction != testHumanAsk {
		t.Fatalf("carried ask = %q", result.HumanAction)
	}

	// A different note still carries it onto the new entry.
	if _, err = assessTarget(root, "still waiting on the operator", AssessBlockedOptions{}); err != nil {
		t.Fatal(err)
	}
	if ask, ok := targetAsk(t, stateFile); !ok || ask != testHumanAsk {
		t.Fatalf("after carry: ask %q %v", ask, ok)
	}
	if targetActionable(t, stateFile) {
		t.Fatal("task actionable after a carrying assessment")
	}

	// A different ask is a material change.
	result, err = assessTarget(root, "still waiting on the operator", AssessBlockedOptions{HumanAction: "rotate the key instead"})
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("replace: %+v %v", result, err)
	}
	if ask, _ := targetAsk(t, stateFile); ask != "rotate the key instead" {
		t.Fatalf("after replace: ask %q", ask)
	}

	// Clearing is a material change and drops the ask.
	result, err = assessTarget(root, "still waiting on the operator", AssessBlockedOptions{ClearHumanAction: true})
	if err != nil || result.Outcome != models.LifecycleCompleted || result.HumanAction != "" {
		t.Fatalf("clear: %+v %v", result, err)
	}
	if ask, ok := targetAsk(t, stateFile); ok {
		t.Fatalf("after clear: ask %q", ask)
	}
	if targetActionable(t, stateFile) {
		t.Fatal("task actionable right after the clearing assessment")
	}
}

func TestAssessBlocked_HumanNoteStillWakesAHumanOwnedBlock(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	if _, err := assessTarget(root, "needs credentials", AssessBlockedOptions{HumanAction: testHumanAsk}); err != nil {
		t.Fatal(err)
	}

	addTargetNote(t, stateFile)

	if !targetActionable(t, stateFile) {
		t.Fatal("a human note targeting the task did not make it actionable")
	}
}

// An ask the doer recorded on the blocked entry survives the orchestrator's
// note-only triage, and the next poll does not re-wake it.
func TestAssessBlocked_NoteOnlyCarriesTheBlockedEntryAsk(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	modifyAwaitedState(t, stateFile, func(state *models.State) {
		task := state.FindTask("target")
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: time.Now().UTC(), Event: models.TaskEventBlocked,
			Extra: map[string]any{models.AwaitingHumanExtraKey: testHumanAsk},
		})
	})

	result, err := assessTarget(root, "operator-owned; nothing for agents", AssessBlockedOptions{})
	if err != nil || result.HumanAction != testHumanAsk {
		t.Fatalf("assessment: %+v %v", result, err)
	}
	if ask, ok := targetAsk(t, stateFile); !ok || ask != testHumanAsk {
		t.Fatalf("ask after triage = %q %v", ask, ok)
	}
	if targetActionable(t, stateFile) {
		t.Fatal("note-only triage of a human-owned block left it actionable")
	}
}

func TestAssessBlocked_HumanActionAndClearAreExclusive(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	before := readStateForTest(t, stateFile).FindTask("target")

	_, err := assessTarget(root, "", AssessBlockedOptions{HumanAction: testHumanAsk, ClearHumanAction: true})
	if err == nil || !strings.Contains(err.Error(), "clear_human_action") {
		t.Fatalf("error = %v, want the conflict named", err)
	}
	if after := readStateForTest(t, stateFile).FindTask("target"); len(after.History) != len(before.History) {
		t.Fatal("rejected assessment appended history")
	}
}

// An ask is material; whitespace in it is not. Without one, the material is
// pinned to the pre-ask digest by TestAssessmentFingerprintWithoutAwaitedSetIsUnchanged.
func TestAssessmentFingerprintHumanActionIsMaterial(t *testing.T) {
	_, stateFile := assessmentIdempotencyFixture(t)
	state, err := db.For(stateFile).Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("target")
	base := AssessmentFingerprintCandidate{Reason: "r", Questions: []string{"q"}, Note: "n"}
	withAsk, spaced := base, base
	withAsk.HumanAction = testHumanAsk
	spaced.HumanAction = "  " + testHumanAsk + " "
	if BuildAssessmentFingerprint(state, task, base) == BuildAssessmentFingerprint(state, task, withAsk) {
		t.Fatal("an ask did not change the digest")
	}
	if BuildAssessmentFingerprint(state, task, withAsk) != BuildAssessmentFingerprint(state, task, spaced) {
		t.Fatal("surrounding whitespace in the ask changed the digest")
	}
}
