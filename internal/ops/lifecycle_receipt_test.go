package ops

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func lifecycleTestTask() *models.Task {
	owner := "coder-1"
	return &models.Task{ID: "task-1", Status: models.TaskStatusImplementing, AssignedTo: &owner, Created: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
}

func lifecycleTestRequest(t *testing.T, task *models.Task, operation, requestID, generation string, payload any) LifecycleRequest {
	t.Helper()
	opts := LifecycleRequestOptions{}
	if requestID != "" {
		opts.RequestID, opts.ExpectedTransition = requestID, models.TaskTransitionID(task)
	}
	request, err := NewLifecycleRequest(operation, task, "coder-1", &models.AgentAuthority{ID: "coder-1", Generation: generation}, opts, payload)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func lifecycleTaskBytes(t *testing.T, task *models.Task) []byte {
	t.Helper()
	data, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func requireLifecycleError(t *testing.T, err error, outcome, action, effects string) {
	t.Helper()
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatalf("expected lifecycle error, got %v", err)
	}
	if lifecycle.Outcome.Outcome != outcome || lifecycle.Outcome.SafeAction != action || lifecycle.Outcome.Effects != effects {
		t.Fatalf("wrong lifecycle error: %+v", lifecycle.Outcome)
	}
}

func TestLifecycleIdentityReusedSentinel(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		name := "receipt"
		if prepared {
			name = "preparation"
		}
		t.Run(name, func(t *testing.T) {
			task := lifecycleTestTask()
			request := lifecycleTestRequest(t, task, "submit-for-review", "identity-1", "generation-1", "original")
			if prepared {
				if err := PrepareLifecycleRequest(task, request, nil); err != nil {
					t.Fatal(err)
				}
			} else if _, err := CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, nil); err != nil {
				t.Fatal(err)
			}
			before := lifecycleTaskBytes(t, task)
			request.PayloadDigest = lifecycleDigest([]byte("different payload"))
			_, err := CheckLifecycleRequest(task, request, nil)
			requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "none")
			var le *LifecycleError
			if !errors.As(err, &le) || !errors.Is(le, ErrLifecycleIdentityReused) || le.Outcome.RequestID != request.RequestID {
				t.Fatalf("identity error lost sentinel or request ID: %v", err)
			}
			if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
				t.Fatal("identity rejection mutated lifecycle metadata")
			}
		})
	}
}

func TestLifecycleInvalidCompletionDoesNotAdvertiseUncommittedBoundary(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	request := lifecycleTestRequest(t, task, "submit-for-review", "invalid-completion", "generation-1", nil)
	if _, err := CheckLifecycleRequest(task, request, nil); err != nil {
		t.Fatal(err)
	}
	task.Status = models.TaskStatusReadyForReview
	_, err := CompleteLifecycleRequest(task, request, models.LifecycleProjection{ReviewCommit: "short"}, nil)
	requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "unknown")
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatal(err)
	}
	outcome := lifecycleErr.Outcome
	if outcome.TaskStatus != "UNKNOWN" || outcome.TransitionID != "" || outcome.CurrentAssignee != "" || outcome.CompletedTransitionID != "" {
		t.Fatalf("completion failure advertised uncommitted task: %+v", outcome)
	}
	if task.Lifecycle != nil {
		t.Fatal("invalid completion changed lifecycle metadata")
	}
}

func TestLifecycleReceiptReplayIsImmutableAcrossOwnershipChange(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	request := lifecycleTestRequest(t, task, "submit-for-review", "submission-1", "generation-1", map[string]string{"commit": strings.Repeat("a", 40)})
	if _, err := CheckLifecycleRequest(task, request, nil); err != nil {
		t.Fatal(err)
	}
	task.Status = models.TaskStatusReadyForReview
	outcome, err := CompleteLifecycleRequest(task, request, models.LifecycleProjection{InputCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before := lifecycleTaskBytes(t, task)
	receipt, err := CheckLifecycleRequest(task, request, nil)
	if err != nil || receipt == nil {
		t.Fatalf("completion was not replayed: %v", err)
	}
	replay := LifecycleReplayOutcome(task, receipt, request.Actor)
	if replay.SafeAction != "continue" || replay.CompletedTransitionID != outcome.CompletedTransitionID {
		t.Fatalf("wrong current replay: %+v", replay)
	}
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("replay changed persistent state")
	}
	other := "coder-2"
	task.AssignedTo = &other
	models.AdvanceLifecycle(task)
	before = lifecycleTaskBytes(t, task)
	receipt, err = CheckLifecycleRequest(task, request, nil)
	if err != nil || receipt == nil {
		t.Fatalf("historical receipt lost: %v", err)
	}
	replay = LifecycleReplayOutcome(task, receipt, request.Actor)
	if replay.SafeAction != "stop" || replay.CurrentAssignee != other || replay.CompletedTransitionID != outcome.CompletedTransitionID || replay.TransitionID == replay.CompletedTransitionID {
		t.Fatalf("historical completion conferred current authority: %+v", replay)
	}
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("historical replay mutated state")
	}
	conflicting := request
	conflicting.PayloadDigest = lifecycleDigest([]byte("different payload"))
	_, err = CheckLifecycleRequest(task, conflicting, nil)
	requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "none")
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("conflict mutated state")
	}
}

func TestLifecycleRetentionIntersectsOperationAndGlobalWindows(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	original := lifecycleTestRequest(t, task, "submit-for-review", "old-submission", "generation-1", nil)
	if _, err := CompleteLifecycleRequest(task, original, models.LifecycleProjection{}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		request := lifecycleTestRequest(t, task, "assess-blocked", fmt.Sprintf("assessment-%d", i), "generation-1", nil)
		if _, err := CheckLifecycleRequest(task, request, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, nil); err != nil {
			t.Fatal(err)
		}
		// Invalidation is not a completion and must not shorten the receipt window.
		models.AdvanceLifecycle(task)
	}
	if len(task.Lifecycle.Receipts) != 4 {
		t.Fatalf("old other-operation receipt survived the global window: %d receipts", len(task.Lifecycle.Receipts))
	}
	for _, receipt := range task.Lifecycle.Receipts {
		if receipt.Operation != "assess-blocked" || receipt.Sequence < 14 {
			t.Fatalf("wrong retained receipt: %+v", receipt)
		}
	}
	before := lifecycleTaskBytes(t, task)
	receipt, err := CheckLifecycleRequest(task, original, nil)
	if receipt != nil {
		t.Fatal("evicted invocation falsely proved completion")
	}
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("evicted retry mutated state")
	}
}

func TestLifecyclePreparationRequiresAuthenticatedGenerationTurnover(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	old := lifecycleTestRequest(t, task, "submit-for-review", "old", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, old, nil); err != nil {
		t.Fatal(err)
	}
	before := lifecycleTaskBytes(t, task)
	_, err := CheckLifecycleRequest(task, old, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("checking preparation changed state")
	}
	legacy := lifecycleTestRequest(t, task, "submit-for-review", "legacy", "", nil)
	_, err = CheckLifecycleRequest(task, legacy, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	otherActor := lifecycleTestRequest(t, task, "submit-for-review", "other-actor", "another-agent-generation", nil)
	otherActor.Actor = "orchestrator-1"
	_, err = CheckLifecycleRequest(task, otherActor, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	current := lifecycleTestRequest(t, task, "submit-for-review", "new", "generation-2", nil)
	if _, err := CheckLifecycleRequest(task, current, nil); err != nil {
		t.Fatalf("current generation could not retire marker: %v", err)
	}
	if err := PrepareLifecycleRequest(task, current, nil); err != nil {
		t.Fatal(err)
	}
	if task.Lifecycle.Revision != 1 || task.Lifecycle.CompletionSequence != 0 || len(task.Lifecycle.Receipts) != 0 {
		t.Fatal("retirement fabricated a completion")
	}
	if task.Lifecycle.Preparation.ExpectedTransition != current.ExpectedTransition || task.Lifecycle.Preparation.Boundary == current.ExpectedTransition {
		t.Fatal("retirement changed request identity or failed to advance boundary")
	}
	_, err = CheckLifecycleRequest(task, current, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	requireLifecycleError(t, ValidateLifecyclePreparation(task, old), models.LifecycleStateChanged, "requery", "unknown")
	if err := ValidateLifecyclePreparation(task, current); err != nil {
		t.Fatal(err)
	}
	task.Status = models.TaskStatusReadyForReview
	task.History = append(task.History, models.TaskHistoryEntry{Time: task.Created.Add(time.Minute), Event: "submitted_for_review"})
	if _, err := CompleteLifecycleRequest(task, current, models.LifecycleProjection{}, nil); err != nil {
		t.Fatalf("own domain mutation prevented completion: %v", err)
	}
	if task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.CompletionSequence != 1 {
		t.Fatal("completion did not atomically replace preparation")
	}
}

func TestLifecyclePreparationRetiredAtNewOwnershipBoundary(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	old := lifecycleTestRequest(t, task, "claim-task", "first-claim", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, old, nil); err != nil {
		t.Fatal(err)
	}
	models.AdvanceLifecycle(task)
	requireLifecycleError(t, ValidateLifecyclePreparation(task, old), models.LifecycleStateChanged, "requery", "unknown")
	fresh := lifecycleTestRequest(t, task, "claim-task", "reclaim", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, fresh, nil); err != nil {
		t.Fatalf("abandoned marker blocked reclaim: %v", err)
	}
	if err := ValidateLifecyclePreparation(task, fresh); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleEmptyRequestIDNeverProvesReplay(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	first := lifecycleTestRequest(t, task, "assess-blocked", "", "generation-1", nil)
	if _, err := CompleteLifecycleRequest(task, first, models.LifecycleProjection{}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := CheckLifecycleRequest(task, first, nil); receipt != nil || err == nil {
		t.Fatal("empty-ID retry reused a completed receipt")
	}
	fresh := lifecycleTestRequest(t, task, "assess-blocked", "", "generation-1", nil)
	if receipt, err := CheckLifecycleRequest(task, fresh, nil); receipt != nil || err != nil {
		t.Fatalf("new legacy call did not use current boundary: %v", err)
	}
}

func TestLifecycleMetadataCompletionRetiresOnlyObsoletePreparation(t *testing.T) {
	t.Parallel()
	for _, boundaryChange := range []bool{false, true} {
		t.Run(fmt.Sprintf("boundary-change-%v", boundaryChange), func(t *testing.T) {
			task := lifecycleTestTask()
			old := lifecycleTestRequest(t, task, "submit-for-review", "abandoned", "generation-1", nil)
			if err := PrepareLifecycleRequest(task, old, nil); err != nil {
				t.Fatal(err)
			}
			generation := "generation-2"
			if boundaryChange {
				// A legacy writer advanced history without knowing about receipts.
				task.History = append(task.History, models.TaskHistoryEntry{Time: task.Created, Event: "ownership_changed"})
				generation = "generation-1"
			}
			current := lifecycleTestRequest(t, task, "mark-blocked", "block", generation, nil)
			if _, err := CheckLifecycleRequest(task, current, nil); err != nil {
				t.Fatal(err)
			}
			task.Status = models.TaskStatusBlocked
			if _, err := CompleteLifecycleRequest(task, current, models.LifecycleProjection{}, nil); err != nil {
				t.Fatalf("checked metadata mutation could not retire obsolete marker: %v", err)
			}
			if task.Lifecycle.Preparation != nil || len(task.Lifecycle.Receipts) != 1 || task.Lifecycle.Receipts[0].RequestID != "block" {
				t.Fatal("obsolete preparation survived or acquired a false receipt")
			}
		})
	}
	task := lifecycleTestTask()
	owner := lifecycleTestRequest(t, task, "submit-for-review", "live", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, owner, nil); err != nil {
		t.Fatal(err)
	}
	foreign := lifecycleTestRequest(t, task, "mark-blocked", "foreign", "generation-1", nil)
	_, err := CompleteLifecycleRequest(task, foreign, models.LifecycleProjection{}, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
	if task.Lifecycle.Preparation.RequestID != "live" || len(task.Lifecycle.Receipts) != 0 {
		t.Fatal("live foreign preparation was retired")
	}
}

func TestLifecycleRejectsOversizedReceiptWithoutMutatingMetadata(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	request := lifecycleTestRequest(t, task, "wt-merge", strings.Repeat("r", 128), "generation-1", nil)
	request.Actor = strings.Repeat("a", 128)
	commit := strings.Repeat("c", 64)
	projection := models.LifecycleProjection{InputCommit: commit, ReviewCommit: commit, BaseCommit: commit, MergeCommit: commit, SourceStatus: models.TaskStatus(strings.Repeat("s", 128))}
	before := lifecycleTaskBytes(t, task)
	_, err := CompleteLifecycleRequest(task, request, projection, nil)
	requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "unknown")
	if !strings.Contains(err.Error(), "1 KiB") {
		t.Fatalf("expected serialized-size rejection, got %v", err)
	}
	if !bytes.Equal(before, lifecycleTaskBytes(t, task)) {
		t.Fatal("failed completion mutated receipt metadata")
	}
}

func TestLifecycleRequestOptionsRequireStablePair(t *testing.T) {
	t.Parallel()
	for _, opts := range []LifecycleRequestOptions{
		{RequestID: "request"}, {ExpectedTransition: strings.Repeat("a", 64)},
		{RequestID: "request", ExpectedTransition: "short"},
		{RequestID: "bad request", ExpectedTransition: strings.Repeat("a", 64)},
		{RequestID: strings.Repeat("x", 129), ExpectedTransition: strings.Repeat("a", 64)},
	} {
		if err := ValidateLifecycleRequestOptions(opts); err == nil {
			t.Fatalf("invalid request pair accepted: %+v", opts)
		}
	}
	if err := ValidateLifecycleRequestOptions(LifecycleRequestOptions{RequestID: "sha:" + strings.Repeat("a", 40), ExpectedTransition: strings.Repeat("b", 64)}); err != nil {
		t.Fatal(err)
	}
}

// A preparation left by an agent generation that is gone must not block every
// other agent forever: identity supersession is same-actor only, so without the
// registry check a dead planner's claim-task preparation strands the task.
func TestPreparationFromRetiredGenerationDoesNotBlockAnotherActor(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	prepared := lifecycleTestRequest(t, task, "claim-task", "first", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, prepared, nil); err != nil {
		t.Fatal(err)
	}
	other, err := NewLifecycleRequest("claim-task", task, "coder-2",
		&models.AgentAuthority{ID: "coder-2", Generation: "generation-2"},
		LifecycleRequestOptions{RequestID: "second", ExpectedTransition: models.TaskTransitionID(task)}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Registry still authenticates the preparing generation: it may be doing
	// external work, so another actor must wait.
	live := map[string]models.Agent{"coder-1": {Generation: "generation-1"}}
	_, err = CheckLifecycleRequest(task, other, live)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")

	// Restarted under a new generation: the registry authenticates that the
	// preparing generation is gone, so any actor may proceed.
	restarted := map[string]models.Agent{"coder-1": {Generation: "generation-9"}}
	if _, err := CheckLifecycleRequest(task, other, restarted); err != nil {
		t.Fatalf("restarted preparing agent still blocked another actor: %v", err)
	}
	// Absence from the registry is NOT retirement: process abandonment retains
	// the marker until inspected recovery (lifecycle-results.md).
	_, err = CheckLifecycleRequest(task, other, map[string]models.Agent{})
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")

	// No registry supplied stays conservative.
	_, err = CheckLifecycleRequest(task, other, nil)
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "unknown")
}

// A metadata-only operation runs check, mutates, then completes in one
// transaction. Both guards must reach the same retirement conclusion, or the
// caller passes the check, does its work, and is rolled back at completion.
func TestRetiredPreparationClearsCheckAndCompletionForAnotherActor(t *testing.T) {
	t.Parallel()
	task := lifecycleTestTask()
	prepared := lifecycleTestRequest(t, task, "assess-blocked", "first", "generation-1", nil)
	if err := PrepareLifecycleRequest(task, prepared, nil); err != nil {
		t.Fatal(err)
	}
	restarted := map[string]models.Agent{"coder-1": {Generation: "generation-9"}}
	other, err := NewLifecycleRequest("assess-blocked", task, "orchestrator-1",
		&models.AgentAuthority{ID: "orchestrator-1", Generation: "generation-2"},
		LifecycleRequestOptions{RequestID: "second", ExpectedTransition: models.TaskTransitionID(task)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CheckLifecycleRequest(task, other, restarted); err != nil {
		t.Fatalf("check refused a retired preparation: %v", err)
	}
	reason := "blocked by a dependency"
	task.BlockedReason = &reason
	if _, err := CompleteLifecycleRequest(task, other, models.LifecycleProjection{}, restarted); err != nil {
		t.Fatalf("completion refused after the check admitted the request: %v", err)
	}
	if task.Lifecycle.Preparation != nil {
		t.Fatal("completion left the retired preparation in place")
	}
}
