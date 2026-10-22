package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

var suiteAdmissionLimit = models.AdmissionLimit{Class: "suite", MaxConcurrent: 1, Match: []string{"boundary_test"}}

// markerCopyScript is a canonical command that copies the submit's in-flight
// marker beside the project, so a test can read the window it was given.
func markerCopyScript() string {
	// Shell path, not filepath.Join: Windows separators would be eaten as
	// escapes by the POSIX shell this runs under (Git Bash on Windows).
	dir := "../../" + paths.ProjectDirName() + "/inflight-submit"
	return fmt.Sprintf("cat %s/*.json > ../../marker-copy.json\n", dir)
}

func copiedMarkerWindow(t *testing.T, root string) time.Duration {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "marker-copy.json"))
	if err != nil {
		t.Fatal(err)
	}
	var marker inflightSubmitMarker
	if err := json.Unmarshal(data, &marker); err != nil {
		t.Fatal(err)
	}
	return marker.Deadline.Sub(marker.StartedAt)
}

func admissionSubmitScenario(t *testing.T, script string, timeoutSeconds int, limits ...models.AdmissionLimit) testhelpers.AcceptanceSubmitScenario {
	t.Helper()
	requirePosixShell(t)
	shortAdmissionSlotPoll(t)
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, script, timeoutSeconds)
	if err := scenario.BB.Modify(func(state *models.State) error {
		state.Config.AdmissionLimits = limits
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return scenario
}

func shortAdmissionSlotCap(t *testing.T, cap time.Duration) {
	t.Helper()
	previous := admissionSlotCap
	admissionSlotCap = cap
	t.Cleanup(func() { admissionSlotCap = previous })
}

func TestSubmitAdmissionSlotRefusalIsRetryableBeforeRebase(t *testing.T) {
	scenario := admissionSubmitScenario(t, "exit 0\n", 30, suiteAdmissionLimit)
	shortAdmissionSlotCap(t, 30*time.Second+300*time.Millisecond)
	shortSubmitSlotWaitFloor(t, 300*time.Millisecond)
	// Integration moves on, so a rebase would rewrite the submitted candidate.
	if err := os.WriteFile(filepath.Join(scenario.Root, "unrelated.txt"), []byte("moved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, scenario.Root, "add", "unrelated.txt")
	testhelpers.MustGit(t, scenario.Root, "commit", "-m", "test: integration advances")
	held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)

	before := readAcceptanceState(t, scenario.BB).FindTask(scenario.TaskID)
	request := LifecycleRequestOptions{RequestID: "contended-submit", ExpectedTransition: models.TaskTransitionID(before)}
	result, err := SubmitForReviewWithAuthorityAndOptions(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority, request)
	var lifecycleErr *LifecycleError
	var evidence *AcceptanceEvidenceError
	if result != nil || !errors.As(err, &lifecycleErr) || !errors.As(err, &evidence) || evidence.Class != AcceptanceFaultAdmissionSlot {
		t.Fatalf("contended submit = %+v, %v; want an admission-slot refusal", result, err)
	}
	if outcome := lifecycleErr.Outcome; outcome.Outcome != models.LifecycleRetryable || outcome.SafeAction != "retry" || outcome.Effects != "none" {
		t.Fatalf("contended submit outcome = %+v, want RETRYABLE/retry/none", outcome)
	}
	after := readAcceptanceState(t, scenario.BB).FindTask(scenario.TaskID)
	if after.Status != before.Status || models.TaskTransitionID(after) != request.ExpectedTransition ||
		(after.Lifecycle != nil && after.Lifecycle.Preparation != nil) {
		t.Fatalf("refusal changed the task: status %s, transition %s, lifecycle %+v", after.Status, models.TaskTransitionID(after), after.Lifecycle)
	}
	if head := testhelpers.MustGit(t, filepath.Join(scenario.Root, ".worktrees", scenario.TaskID), "rev-parse", "HEAD"); head != scenario.Commit {
		t.Fatalf("refusal rebased the worktree: HEAD %s, submitted %s", head, scenario.Commit)
	}
	if _, ok := InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now()); ok {
		t.Fatal("refused submit left its in-flight marker")
	}

	held.release()
	result, err = SubmitForReviewWithAuthorityAndOptions(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority, request)
	if err != nil || result == nil || result.LifecycleOutcome.Outcome != models.LifecycleCompleted {
		t.Fatalf("same-request retry = %+v, %v; want COMPLETED", result, err)
	}
	submitted := readAcceptanceState(t, scenario.BB).FindTask(scenario.TaskID)
	if submitted.ReviewCommit == nil || *submitted.ReviewCommit == scenario.Commit {
		t.Fatalf("retry did not rebase onto the moved integration branch: review commit %v", submitted.ReviewCommit)
	}
}

func TestSubmitSlotWaitIsOutsideTheBatchBudgetAndInsideTheMarker(t *testing.T) {
	scenario := admissionSubmitScenario(t, markerCopyScript(), 1, suiteAdmissionLimit)
	shortAdmissionSlotCap(t, 20*time.Second)
	shortSubmitSlotWaitFloor(t, 5*time.Second) // Below cap - budget: the cap governs.
	held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)

	type outcome struct {
		result *SubmitForReviewResult
		err    error
	}
	done := make(chan outcome, 1)
	started := time.Now()
	go func() {
		result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
		done <- outcome{result, err}
	}()
	waitForTickets(t, scenario.Root, suiteAdmissionLimit.Class, 1)
	if _, ok := InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now()); !ok {
		t.Fatal("a submit waiting for its slot has no in-flight marker")
	}
	time.Sleep(1500 * time.Millisecond) // Longer than the 1 s batch budget.
	held.release()
	got := <-done
	if got.err != nil {
		t.Fatalf("submit after a %s slot wait failed: %v", time.Since(started), got.err)
	}
	if waited := time.Since(started); waited < 1500*time.Millisecond {
		t.Fatalf("submit finished after %s, before its slot was free", waited)
	}
	if window := copiedMarkerWindow(t, scenario.Root); window != admissionSlotCap+submitGraceMargin {
		t.Fatalf("marker window = %s, want the cap %s plus grace", window, admissionSlotCap)
	}
}

func shortSubmitSlotWaitFloor(t *testing.T, floor time.Duration) {
	t.Helper()
	previous := submitSlotWaitFloor
	submitSlotWaitFloor = floor
	t.Cleanup(func() { submitSlotWaitFloor = previous })
}

// TestSubmitBudgetedAtTheCapQueuesForAFreedSlot pins D-178: a batch budgeted
// at the cap used to get a zero wait, so a full class refused it at once.
func TestSubmitBudgetedAtTheCapQueuesForAFreedSlot(t *testing.T) {
	scenario := admissionSubmitScenario(t, markerCopyScript(), 30, suiteAdmissionLimit)
	shortAdmissionSlotCap(t, 30*time.Second)
	shortSubmitSlotWaitFloor(t, 20*time.Second)
	held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)

	done := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
		done <- err
	}()
	for queuedTickets(t, scenario.Root, suiteAdmissionLimit.Class) == 0 {
		select {
		case err := <-done:
			t.Fatalf("submit budgeted at the cap returned without queueing: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
		if time.Since(started) > 10*time.Second {
			t.Fatal("submit budgeted at the cap never queued")
		}
	}
	if deadline, ok := InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now()); !ok || deadline.Before(started.Add(50*time.Second)) {
		t.Fatalf("queued submit's marker deadline %v (ok=%v) does not cover the floor and the batch", deadline, ok)
	}
	select {
	case err := <-done:
		t.Fatalf("submit returned while its class was full: %v", err)
	default:
	}
	held.release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("submit after its slot freed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("submit did not finish after its slot freed")
	}
	if window := copiedMarkerWindow(t, scenario.Root); window != 30*time.Second+20*time.Second+submitGraceMargin {
		t.Fatalf("marker window = %s, want the batch plus the floor plus grace", window)
	}
}

func TestSubmitWithoutAMatchingLimitKeepsItsMarkerWindow(t *testing.T) {
	other := models.AdmissionLimit{Class: "other", MaxConcurrent: 1, Match: []string{"no-such-command"}}
	scenario := admissionSubmitScenario(t, markerCopyScript(), 30, other)
	holdAdmissionSlots(t, scenario.Root, other)
	if _, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority); err != nil {
		t.Fatalf("unmatched submit waited on or failed for another class: %v", err)
	}
	if window := copiedMarkerWindow(t, scenario.Root); window != 30*time.Second+submitGraceMargin {
		t.Fatalf("marker window = %s, want the unchanged batch window", window)
	}
}

func TestSubmitLosingItsClaimWhileWaitingIsNotRetryable(t *testing.T) {
	scenario := admissionSubmitScenario(t, "exit 0\n", 30, suiteAdmissionLimit)
	previous := acceptanceOwnershipPollInterval
	acceptanceOwnershipPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { acceptanceOwnershipPollInterval = previous })
	holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)

	done := make(chan error, 1)
	go func() {
		_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
		done <- err
	}()
	waitForTickets(t, scenario.Root, suiteAdmissionLimit.Class, 1)
	other := "coder-2"
	if err := scenario.BB.Modify(func(state *models.State) error {
		state.FindTask(scenario.TaskID).AssignedTo = &other
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a submit whose claim was released kept waiting for its slot")
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged ||
		lifecycleErr.Outcome.SafeAction != "requery" || lifecycleErr.Outcome.Effects != "none" {
		t.Fatalf("lost claim while waiting = %v, want STATE_CHANGED/requery/none", err)
	}
	if got := queuedTickets(t, scenario.Root, suiteAdmissionLimit.Class); got != 0 {
		t.Fatalf("lost waiter left %d tickets", got)
	}
}

// TestSubmitFallbackMarkerCoversAResidualSlotWait covers a submit whose
// pre-admission could not compute its classes (no early marker, no slots):
// the marker written after preparation must outlast the wait execution still
// faces, like the lifecycle writes it.
func TestSubmitFallbackMarkerCoversAResidualSlotWait(t *testing.T) {
	scenario := admissionSubmitScenario(t, "exit 0\n", 30, suiteAdmissionLimit)
	shortAdmissionSlotCap(t, 32*time.Second)
	shortSubmitSlotWaitFloor(t, time.Second) // Below cap - budget: the cap governs.
	invocation := &submissionInvocation{admission: &submissionAdmission{config: models.Config{AdmissionLimits: []models.AdmissionLimit{suiteAdmissionLimit}}}}
	prepared, err := prepareSubmitForReview(scenario.Root, scenario.TaskID, scenario.Commit, scenario.AgentID, &scenario.Authority, LifecycleRequestOptions{}, invocation)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.acceptanceBatch != admissionSlotCap {
		t.Fatalf("fallback marker window = %s, want batch plus slot wait = %s", prepared.acceptanceBatch, admissionSlotCap)
	}
	held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)
	written := time.Now()
	removeMarker := writeInflightSubmitMarker(scenario.Root, scenario.Authority, scenario.TaskID, prepared.acceptanceBatch, written.UTC())
	defer removeMarker()

	done := make(chan error, 1)
	go func() {
		_, err := prepared.complete()
		done <- err
	}()
	waitForTickets(t, scenario.Root, suiteAdmissionLimit.Class, 1)
	// The residual wait may last until execution start + 2 s, then the 30 s
	// batch; the marker must not expire before both.
	deadline, ok := InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now())
	if !ok || deadline.Before(time.Now().Add(submitAdmissionSlotWait(30)+30*time.Second)) {
		t.Fatalf("marker deadline %v (ok=%v) expires before the residual wait and batch", deadline, ok)
	}
	held.release()
	if err := <-done; err != nil {
		t.Fatalf("complete after the residual wait: %v", err)
	}

	// Without a matching class the window stays the batch budget.
	other := &submissionInvocation{admission: &submissionAdmission{config: models.Config{AdmissionLimits: []models.AdmissionLimit{{Class: "other", MaxConcurrent: 1, Match: []string{"no-such-command"}}}}}}
	unmatched := admissionSubmitScenario(t, "exit 0\n", 30)
	prepared, err = prepareSubmitForReview(unmatched.Root, unmatched.TaskID, unmatched.Commit, unmatched.AgentID, &unmatched.Authority, LifecycleRequestOptions{}, other)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.acceptanceBatch != 30*time.Second {
		t.Fatalf("unmatched marker window = %s, want the 30 s batch", prepared.acceptanceBatch)
	}
}
