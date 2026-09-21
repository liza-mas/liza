package ops

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReviewClaimFailureClassifiesCandidateFreeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantClass     string
		wantTransient bool
	}{
		{
			name:      "authority rejection stops the supervisor",
			err:       &AgentAuthorityError{AgentID: "code-reviewer-1"},
			wantClass: ReviewClaimClassAuthority,
		},
		{
			name:      "wrapped authority rejection keeps its class",
			err:       fmt.Errorf("claim-reviewer-task: %w", &AgentAuthorityError{AgentID: "code-reviewer-1"}),
			wantClass: ReviewClaimClassAuthority,
		},
		{
			name:      "degraded agent keeps the existing exit",
			err:       fmt.Errorf("%w: %w", ErrAgentDegraded, errors.New("worktree setup failed")),
			wantClass: ReviewClaimClassDegraded,
		},
		{
			name:          "git probe failure is transient",
			err:           &OperationalError{Code: "git_operation", Phase: "review-boundary", Message: "failed to resolve effective review base"},
			wantClass:     ReviewClaimClassGitOperation,
			wantTransient: true,
		},
		{
			name:      "precondition without candidates is no work",
			err:       &PreconditionError{Reason: "no reviewable tasks found"},
			wantClass: ReviewClaimClassNoWork,
		},
		{
			name:      "joined precondition is still no work",
			err:       errors.Join(&PreconditionError{Reason: "all reviewable tasks in claim cooldown"}),
			wantClass: ReviewClaimClassNoWork,
		},
		{
			name:          "unknown error is unclassified and retried",
			err:           errors.New("something else went wrong"),
			wantClass:     ReviewClaimClassUnclassified,
			wantTransient: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			failure := ClassifyReviewClaimError("code-reviewer", tt.err)
			if failure == nil {
				t.Fatalf("ClassifyReviewClaimError(%v) = nil, want typed failure", tt.err)
			}
			if failure.Role != "code-reviewer" {
				t.Errorf("Role = %q, want code-reviewer", failure.Role)
			}
			if failure.Class != tt.wantClass {
				t.Errorf("Class = %q, want %q", failure.Class, tt.wantClass)
			}
			if failure.Transient != tt.wantTransient {
				t.Errorf("Transient = %v, want %v", failure.Transient, tt.wantTransient)
			}
			if len(failure.Candidates) != 0 {
				t.Errorf("Candidates = %#v, want none for a candidate-free failure", failure.Candidates)
			}
			if !errors.Is(failure, tt.err) && failure.Error() != tt.err.Error() {
				t.Errorf("Error() = %q, want the wrapped error %q", failure.Error(), tt.err.Error())
			}
		})
	}
}

func TestReviewClaimFailureClassificationIsIdempotent(t *testing.T) {
	t.Parallel()

	candidates := []ReviewClaimCandidateFailure{
		{TaskID: "task-a", Class: ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: "aaa", Recovery: "run update-review-commit task-a"},
		{TaskID: "task-b", Class: ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: "bbb", Recovery: "run update-review-commit task-b"},
	}
	// The wrapped error is the prose precondition the claim path already
	// returns; re-classifying it must not degrade the failure to no_work.
	original := newReviewClaimFailure("", candidates, &PreconditionError{Reason: "review boundary needs repair for task(s): task-a, task-b"})

	failure := ClassifyReviewClaimError("code-reviewer", fmt.Errorf("claim-reviewer-task: %w", original))
	if failure != original {
		t.Fatalf("ClassifyReviewClaimError returned %#v, want the existing failure unchanged", failure)
	}
	if failure.Class != ReviewClaimClassCandidateFailures {
		t.Errorf("Class = %q, want %q", failure.Class, ReviewClaimClassCandidateFailures)
	}
	if failure.Transient {
		t.Error("Transient = true, want false for a candidate-carrying envelope")
	}
	if len(failure.Candidates) != len(candidates) {
		t.Fatalf("Candidates = %#v, want %d preserved", failure.Candidates, len(candidates))
	}
	for i, candidate := range failure.Candidates {
		if candidate != candidates[i] {
			t.Errorf("Candidates[%d] = %#v, want %#v", i, candidate, candidates[i])
		}
	}
	if failure.Role != "code-reviewer" {
		t.Errorf("Role = %q, want the empty role filled with the observer's role", failure.Role)
	}

	failure.Role = "code-plan-reviewer"
	if again := ClassifyReviewClaimError("code-reviewer", failure); again.Role != "code-plan-reviewer" {
		t.Errorf("Role = %q, want the already-recorded role preserved", again.Role)
	}
}

func TestReviewClaimFailureUnwrapPreservesExistingClassification(t *testing.T) {
	t.Parallel()

	candidates := []ReviewClaimCandidateFailure{{TaskID: "task-a", Class: ReviewClaimClassReviewBoundaryRepair}}

	precondition := &PreconditionError{Reason: "review boundary needs repair for task(s): task-a"}
	failure := newReviewClaimFailure("code-reviewer", candidates, precondition)
	var gotPrecondition *PreconditionError
	if !errors.As(failure, &gotPrecondition) || gotPrecondition != precondition {
		t.Fatalf("errors.As(*PreconditionError) = %v, want the wrapped precondition", gotPrecondition)
	}
	if failure.Error() != precondition.Error() {
		t.Errorf("Error() = %q, want the wrapped error text %q", failure.Error(), precondition.Error())
	}

	repairNeeded := &ReviewBoundaryRepairNeededError{TaskID: "task-a", Reason: "review_commit does not match worktree HEAD", RecoveryHint: "run update-review-commit task-a"}
	repairFailure := newReviewClaimFailure("code-reviewer", candidates, repairNeeded)
	var gotRepair *ReviewBoundaryRepairNeededError
	if !errors.As(repairFailure, &gotRepair) || gotRepair != repairNeeded {
		t.Fatalf("errors.As(*ReviewBoundaryRepairNeededError) = %v, want the wrapped repair error", gotRepair)
	}
}

func TestReviewClaimFailureBoundaryVersionTracksStateFields(t *testing.T) {
	t.Parallel()

	baseTask := func() *models.Task {
		return &models.Task{
			ID:           "task-a",
			Status:       models.TaskStatusReadyForReview,
			ReviewCommit: stringPtr("aaaaaaa"),
			BaseCommit:   stringPtr("bbbbbbb"),
			Worktree:     stringPtr(".worktrees/task-a"),
			History:      []models.TaskHistoryEntry{{Time: time.Now().UTC(), Event: models.TaskEventClaimed}},
		}
	}

	want := ReviewClaimBoundaryVersion(baseTask())
	if want == "" {
		t.Fatal("ReviewClaimBoundaryVersion returned an empty version for a complete task")
	}
	if again := ReviewClaimBoundaryVersion(baseTask()); again != want {
		t.Fatalf("ReviewClaimBoundaryVersion is unstable: %q then %q", want, again)
	}

	changes := map[string]func(*models.Task){
		"status":        func(task *models.Task) { task.Status = models.TaskStatusPartiallyApproved },
		"review_commit": func(task *models.Task) { task.ReviewCommit = stringPtr("ccccccc") },
		"base_commit":   func(task *models.Task) { task.BaseCommit = stringPtr("ddddddd") },
		"worktree":      func(task *models.Task) { task.Worktree = stringPtr(".worktrees/other") },
		"history length": func(task *models.Task) {
			task.History = append(task.History, models.TaskHistoryEntry{Event: models.TaskEventClaimed})
		},
	}
	for field, change := range changes {
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			task := baseTask()
			change(task)
			if got := ReviewClaimBoundaryVersion(task); got == want {
				t.Errorf("ReviewClaimBoundaryVersion unchanged after %s changed", field)
			}
		})
	}

	// A cleared field must not collide with an empty one: clearing review_commit
	// is a repair-relevant change, not the same boundary.
	nilTask := baseTask()
	nilTask.ReviewCommit = nil
	emptyTask := baseTask()
	emptyTask.ReviewCommit = stringPtr("")
	if ReviewClaimBoundaryVersion(nilTask) == ReviewClaimBoundaryVersion(emptyTask) {
		t.Error("nil review_commit and empty review_commit share a boundary version")
	}

	// Length-prefixed fields keep a shifted value from colliding.
	shiftedA := baseTask()
	shiftedA.ReviewCommit = stringPtr("aa")
	shiftedA.BaseCommit = stringPtr("bbbb")
	shiftedB := baseTask()
	shiftedB.ReviewCommit = stringPtr("aabb")
	shiftedB.BaseCommit = stringPtr("bb")
	if ReviewClaimBoundaryVersion(shiftedA) == ReviewClaimBoundaryVersion(shiftedB) {
		t.Error("adjacent field values collide across the field boundary")
	}
}

func TestReviewClaimFailureClassifiesRemovalBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		err           error
		wantClass     string
		wantTransient bool
		wantRecovery  string
	}{
		{
			name:      "missing worktree directory",
			err:       &lizaerrors.WorktreeContextError{Operation: "review-assignment", TaskID: "task-a", Reason: "task has recorded worktree but worktree directory does not exist"},
			wantClass: ReviewClaimClassWorktreeContext,
		},
		{
			name: "unreadable worktree",
			err: &OperationalError{Code: "worktree_context", Phase: "review-boundary", Message: "failed to stat task worktree", Details: map[string]any{
				"recovery_hint": "Inspect the task worktree path and filesystem permissions, then retry the review operation.",
			}},
			wantClass:    ReviewClaimClassWorktreeContext,
			wantRecovery: "Inspect the task worktree path and filesystem permissions, then retry the review operation.",
		},
		{
			name: "failed git probe",
			err: &OperationalError{Code: "git_operation", Phase: "review-boundary", Message: "failed to get task worktree HEAD", Details: map[string]any{
				"recovery_hint": "Inspect the task worktree git metadata, ensure HEAD resolves, then retry the review operation.",
			}},
			wantClass:     ReviewClaimClassGitOperation,
			wantTransient: true,
			wantRecovery:  "Inspect the task worktree git metadata, ensure HEAD resolves, then retry the review operation.",
		},
		{
			name:      "anything else leaves the claimable set",
			err:       &PreconditionError{Reason: "task task-a has no review_commit — cannot assign for review"},
			wantClass: ReviewClaimClassIntegrationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			class, transient := classifyReviewBoundaryRemoval(tt.err)
			if class != tt.wantClass {
				t.Errorf("class = %q, want %q", class, tt.wantClass)
			}
			if transient != tt.wantTransient {
				t.Errorf("transient = %v, want %v", transient, tt.wantTransient)
			}
			if got := reviewClaimRecoveryHint(tt.err); got != tt.wantRecovery {
				t.Errorf("recovery hint = %q, want %q", got, tt.wantRecovery)
			}
		})
	}
}

func TestReviewClaimFailureRecordsEachCandidateOnce(t *testing.T) {
	t.Parallel()

	task := &models.Task{ID: "task-a", Status: models.TaskStatusReadyForReview, Worktree: stringPtr(".worktrees/task-a")}
	repairNeeded := &ReviewBoundaryRepairNeededError{TaskID: "task-a", Reason: "review_commit does not match worktree HEAD", RecoveryHint: "run update-review-commit task-a"}

	var failures []ReviewClaimCandidateFailure
	first := newReviewClaimCandidateFailure(task, ReviewClaimClassReviewBoundaryRepair, false, repairNeeded)
	failures = appendReviewClaimCandidateFailure(failures, first)

	// A retry of the same Modify re-observes the candidate; the first
	// classification stays, mirroring the existing repair-needed dedup.
	retried := newReviewClaimCandidateFailure(task, ReviewClaimClassIntegrationFailed, true, errors.New("other"))
	failures = appendReviewClaimCandidateFailure(failures, retried)

	if len(failures) != 1 {
		t.Fatalf("failures = %#v, want one record per task ID", failures)
	}
	if failures[0] != first {
		t.Errorf("failures[0] = %#v, want the first classification %#v", failures[0], first)
	}
	if failures[0].Recovery != repairNeeded.RecoveryHint {
		t.Errorf("Recovery = %q, want the error's own hint %q", failures[0].Recovery, repairNeeded.RecoveryHint)
	}
	if failures[0].BoundaryVersion != ReviewClaimBoundaryVersion(task) {
		t.Error("BoundaryVersion does not match the candidate's state-derived version")
	}
}

// reviewerClaimCircuitOpenFixture registers one reviewer agent and returns the
// project root, its state path and the authority that agent currently holds.
func reviewerClaimCircuitOpenFixture(t *testing.T) (string, string, models.AgentAuthority) {
	t.Helper()

	const agentID = "code-reviewer-1"
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	state := testhelpers.CreateValidState()
	agent := resumableOwnedAgent(models.RoleCodeReviewer, models.AgentStatusIdle, nil, time.Now().UTC())
	agent.Generation = reviewerClaimCircuitOpenGeneration
	state.Agents[agentID] = agent
	testhelpers.WriteInitialState(t, statePath, state)

	return projectRoot, statePath, models.AgentAuthority{ID: agentID, Generation: reviewerClaimCircuitOpenGeneration}
}

const reviewerClaimCircuitOpenGeneration = "reviewer-claim-generation-a"

func reviewerClaimCircuitOpenInput(projectRoot string, authority models.AgentAuthority, now time.Time) ReviewerClaimCircuitOpenInput {
	return ReviewerClaimCircuitOpenInput{
		ProjectRoot:     projectRoot,
		AgentID:         authority.ID,
		Authority:       &authority,
		Role:            "code-reviewer",
		TaskID:          "task-broken-boundary",
		FailureClass:    ReviewClaimClassReviewBoundaryRepair,
		BoundaryVersion: "boundary-version-one",
		Recovery:        "run update-review-commit for the task",
		Err:             &ReviewBoundaryRepairNeededError{TaskID: "task-broken-boundary"},
		Attempts:        3,
		FirstFailure:    now,
		LastFailure:     now,
		CooldownUntil:   now.Add(5 * time.Minute),
	}
}

func readAnomalies(t *testing.T, statePath string) []models.Anomaly {
	t.Helper()

	state, err := db.For(statePath).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state.Anomalies
}

func TestReviewerClaimCircuitOpenRecordsOneAnomalyPerKey(t *testing.T) {
	t.Parallel()

	projectRoot, statePath, authority := reviewerClaimCircuitOpenFixture(t)
	first := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	input := reviewerClaimCircuitOpenInput(projectRoot, authority, first)

	appended, err := RecordReviewerClaimCircuitOpen(input)
	if err != nil {
		t.Fatalf("RecordReviewerClaimCircuitOpen() error = %v", err)
	}
	if !appended {
		t.Fatal("appended = false on the first observation of a key, want true")
	}

	anomalies := readAnomalies(t, statePath)
	if len(anomalies) != 1 {
		t.Fatalf("anomalies = %d, want exactly 1", len(anomalies))
	}
	recorded := anomalies[0]
	if recorded.Type != models.AnomalyTypeReviewerClaimCircuitOpen {
		t.Errorf("Type = %q, want %q", recorded.Type, models.AnomalyTypeReviewerClaimCircuitOpen)
	}
	if recorded.Task != input.TaskID {
		t.Errorf("Task = %q, want %q", recorded.Task, input.TaskID)
	}
	if recorded.Reporter != authority.ID {
		t.Errorf("Reporter = %q, want %q", recorded.Reporter, authority.ID)
	}
	wantDetails := map[string]any{
		"role":             input.Role,
		"failure_class":    input.FailureClass,
		"boundary_version": input.BoundaryVersion,
		"attempts":         3,
		"first_failure":    "2026-09-18T10:00:00Z",
		"last_failure":     "2026-09-18T10:00:00Z",
		"cooldown_until":   "2026-09-18T10:05:00Z",
		"recovery":         input.Recovery,
		"error":            input.Err.Error(),
	}
	for field, want := range wantDetails {
		if got := fmt.Sprint(recorded.Details[field]); got != fmt.Sprint(want) {
			t.Errorf("details[%q] = %v, want %v", field, recorded.Details[field], want)
		}
	}

	// A second observation of the same key advances the counters in place.
	second := first.Add(7 * time.Minute)
	repeat := input
	repeat.Attempts = 4
	repeat.LastFailure = second
	repeat.CooldownUntil = second.Add(10 * time.Minute)
	repeat.Recovery = "a later hint that must not overwrite the first"
	repeat.Err = errors.New("a later error that must not overwrite the first")

	appended, err = RecordReviewerClaimCircuitOpen(repeat)
	if err != nil {
		t.Fatalf("RecordReviewerClaimCircuitOpen() repeat error = %v", err)
	}
	if appended {
		t.Error("appended = true on a repeated key, want false")
	}

	anomalies = readAnomalies(t, statePath)
	if len(anomalies) != 1 {
		t.Fatalf("anomalies after repeat = %d, want exactly 1", len(anomalies))
	}
	updated := anomalies[0]
	advanced := map[string]any{
		"attempts":       4,
		"last_failure":   "2026-09-18T10:07:00Z",
		"cooldown_until": "2026-09-18T10:17:00Z",
	}
	for field, want := range advanced {
		if got := fmt.Sprint(updated.Details[field]); got != fmt.Sprint(want) {
			t.Errorf("advanced details[%q] = %v, want %v", field, updated.Details[field], want)
		}
	}
	for _, field := range []string{"first_failure", "role", "failure_class", "boundary_version", "recovery", "error"} {
		if got, want := fmt.Sprint(updated.Details[field]), fmt.Sprint(recorded.Details[field]); got != want {
			t.Errorf("immutable details[%q] = %q, want %q", field, got, want)
		}
	}
	if !updated.Timestamp.Equal(recorded.Timestamp) {
		t.Errorf("Timestamp = %v, want the first observation %v", updated.Timestamp, recorded.Timestamp)
	}

	// A different boundary version is a different key.
	repaired := input
	repaired.BoundaryVersion = "boundary-version-two"
	appended, err = RecordReviewerClaimCircuitOpen(repaired)
	if err != nil {
		t.Fatalf("RecordReviewerClaimCircuitOpen() after repair error = %v", err)
	}
	if !appended {
		t.Error("appended = false for a changed boundary version, want true")
	}
	if anomalies := readAnomalies(t, statePath); len(anomalies) != 2 {
		t.Fatalf("anomalies after boundary change = %d, want 2", len(anomalies))
	}
}

func TestReviewerClaimCircuitOpenConcurrentWritersAppendOnce(t *testing.T) {
	t.Parallel()

	projectRoot, statePath, authority := reviewerClaimCircuitOpenFixture(t)
	input := reviewerClaimCircuitOpenInput(projectRoot, authority, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC))

	var wg sync.WaitGroup
	results := make([]bool, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = RecordReviewerClaimCircuitOpen(input)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("RecordReviewerClaimCircuitOpen() goroutine %d error = %v", i, err)
		}
	}
	appended := 0
	for _, ok := range results {
		if ok {
			appended++
		}
	}
	if appended != 1 {
		t.Errorf("appended results = %d, want exactly 1", appended)
	}
	if anomalies := readAnomalies(t, statePath); len(anomalies) != 1 {
		t.Fatalf("anomalies = %d, want exactly 1 after racing writers", len(anomalies))
	}
}

func TestReviewerClaimCircuitOpenRejectedAuthorityLeavesStateUnchanged(t *testing.T) {
	t.Parallel()

	projectRoot, statePath, authority := reviewerClaimCircuitOpenFixture(t)
	before := readStateBytes(t, statePath)

	stale := authority
	stale.Generation = "reviewer-claim-generation-stale"
	input := reviewerClaimCircuitOpenInput(projectRoot, stale, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC))

	appended, err := RecordReviewerClaimCircuitOpen(input)
	assertLifecycleAuthorityError(t, err, stale.ID)
	if appended {
		t.Error("appended = true for a rejected authority, want false")
	}
	if after := readStateBytes(t, statePath); !bytes.Equal(after, before) {
		t.Error("rejected authority changed state")
	}
}

func TestReviewerClaimCircuitOpenBoundsAndMasksErrorText(t *testing.T) {
	const secret = "planted-token-value-01234567"
	t.Setenv("REVIEWER_CLAIM_TEST_API_KEY", secret)

	projectRoot, statePath, authority := reviewerClaimCircuitOpenFixture(t)
	input := reviewerClaimCircuitOpenInput(projectRoot, authority, time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC))
	input.Err = fmt.Errorf("review boundary needs repair using %s: %s", secret, strings.Repeat("x", reviewerClaimCircuitOpenErrorLimit))

	if _, err := RecordReviewerClaimCircuitOpen(input); err != nil {
		t.Fatalf("RecordReviewerClaimCircuitOpen() error = %v", err)
	}

	anomalies := readAnomalies(t, statePath)
	if len(anomalies) != 1 {
		t.Fatalf("anomalies = %d, want exactly 1", len(anomalies))
	}
	stored, ok := anomalies[0].Details["error"].(string)
	if !ok {
		t.Fatalf("details[error] = %#v, want a string", anomalies[0].Details["error"])
	}
	if strings.Contains(stored, secret) {
		t.Error("stored error text contains the planted secret")
	}
	if !strings.Contains(stored, "***") {
		t.Errorf("stored error text = %q, want the redaction marker", stored)
	}
	if !strings.HasSuffix(stored, "... [truncated]") {
		t.Errorf("stored error text = %q, want a truncation marker", stored)
	}
	if len(stored) > reviewerClaimCircuitOpenErrorLimit+len("... [truncated]") {
		t.Errorf("stored error text length = %d, want at most the bound plus its marker", len(stored))
	}

	// The registration generation is recorded nowhere: keying on it would
	// multiply anomalies per restart, and it must not reach durable state.
	for field, value := range anomalies[0].Details {
		if strings.Contains(fmt.Sprint(value), reviewerClaimCircuitOpenGeneration) {
			t.Errorf("details[%q] = %v, want no registration generation", field, value)
		}
	}
	if strings.Contains(anomalies[0].Reporter, reviewerClaimCircuitOpenGeneration) {
		t.Errorf("Reporter = %q, want no registration generation", anomalies[0].Reporter)
	}
}
