package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const testTimeoutRuling = "D123"

// timeoutRetryScenario is a strict submit scenario whose canonical command
// counts its runs in a file outside the worktree before running body. With
// limits it shortens the global slot poll, so such tests must not be parallel.
func timeoutRetryScenario(t *testing.T, body string, budgetSeconds int, policy *models.AdmissionTimeoutRetry, limits ...models.AdmissionLimit) (testhelpers.AcceptanceSubmitScenario, func() int) {
	t.Helper()
	requirePosixShell(t)
	if len(limits) > 0 {
		shortAdmissionSlotPoll(t)
	}
	// Shell path relative to the worktree, as markerCopyScript does.
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "echo run >> ../../runs.txt\n"+body, budgetSeconds)
	if err := scenario.BB.Modify(func(state *models.State) error {
		state.Config.AdmissionTimeoutRetry = policy
		state.Config.AdmissionLimits = limits
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	runs := func() int {
		data, err := os.ReadFile(filepath.Join(scenario.Root, "runs.txt"))
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "run\n")
	}
	return scenario, runs
}

func retryPolicy(factor int) *models.AdmissionTimeoutRetry {
	return &models.AdmissionTimeoutRetry{BudgetFactor: factor, Ruling: testTimeoutRuling}
}

func requireBatchTimeoutRefusal(t *testing.T, err error) *AcceptanceEvidenceError {
	t.Helper()
	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != AcceptanceFaultBatchTimeout {
		t.Fatalf("error = %v, want a batch-timeout refusal", err)
	}
	return refusal
}

// ADR-0216: a batch that times out re-runs once in full at the policy's
// budget, in the same submit, and the receipt records the retry.
func TestSubmitTimeoutRetryPassesAndRecordsTheRetry(t *testing.T) {
	t.Parallel()
	// GIVEN a 1 s budget, a command needing 2 s and a 10x policy
	scenario, runs := timeoutRetryScenario(t, "sleep 2\n", 1, retryPolicy(10))

	// WHEN the coder submits once
	result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	// THEN the retry passes and is recorded on the receipt
	if err != nil || result == nil || result.LifecycleOutcome.Outcome != models.LifecycleCompleted {
		t.Fatalf("submit = %+v, %v; want COMPLETED after a retry", result, err)
	}
	if got := runs(); got != 2 {
		t.Fatalf("canonical command ran %d times, want 2 (first run and retry)", got)
	}
	task, err := scenario.BB.GetTask(scenario.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	want := models.AcceptanceTimeoutRetry{TimedOutIndex: 0, TimeoutSeconds: 1, RetryTimeoutSeconds: 10, ConfiguredTimeoutSeconds: 10, Ruling: testTimeoutRuling}
	if task.AcceptanceReceipt == nil || task.AcceptanceReceipt.TimeoutRetry == nil || *task.AcceptanceReceipt.TimeoutRetry != want {
		t.Fatalf("receipt retry record = %+v, want %+v", task.AcceptanceReceipt, want)
	}
	if len(task.AcceptanceReceipt.Commands) != 1 || task.AcceptanceReceipt.Commands[0].ExitCode != 0 {
		t.Fatalf("receipt commands = %+v, want the retry's one successful execution", task.AcceptanceReceipt.Commands)
	}
}

// When the retry also times out, the submit blocks the task for a budget
// ruling instead of leaving the coder to do it.
func TestSubmitTimeoutRetryExhaustedBlocksTheTask(t *testing.T) {
	t.Parallel()
	scenario, runs := timeoutRetryScenario(t, "sleep 10\n", 1, retryPolicy(2))

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	refusal := requireBatchTimeoutRefusal(t, err)
	if !refusal.Blocked || refusal.BlockFailed || refusal.BlockSkipped != "" {
		t.Fatalf("refusal block flags = blocked %v, failed %v, skipped %q; want blocked", refusal.Blocked, refusal.BlockFailed, refusal.BlockSkipped)
	}
	for _, want := range []string{"after 1s", "retry at 2s also timed out", "ruling " + testTimeoutRuling, "now BLOCKED", "--budget-exception"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if got := runs(); got != 2 {
		t.Fatalf("canonical command ran %d times, want 2", got)
	}
	task, getErr := scenario.BB.GetTask(scenario.TaskID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || !strings.Contains(*task.BlockedReason, "retry at 2s also timed out") ||
		!strings.Contains(*task.BlockedReason, "(ruling "+testTimeoutRuling+")") {
		t.Fatalf("task status %s, blocked reason %v; want BLOCKED naming both budgets and the ruling", task.Status, task.BlockedReason)
	}
}

// A timed-out command that leaves files behind cannot be re-run as is: no
// retry, and the task is blocked with that reason.
func TestSubmitTimeoutRetryRefusesADirtyWorktree(t *testing.T) {
	t.Parallel()
	scenario, runs := timeoutRetryScenario(t, "touch leftover\nsleep 5\n", 1, retryPolicy(10))

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	refusal := requireBatchTimeoutRefusal(t, err)
	if !strings.Contains(refusal.Reason, "left the worktree changed") || !refusal.Blocked {
		t.Fatalf("refusal = %+v; want a blocked no-retry for the changed worktree", refusal)
	}
	if got := runs(); got != 1 {
		t.Fatalf("canonical command ran %d times, want 1", got)
	}
}

// Without a policy a timeout is reported as before: no retry, no block.
func TestSubmitTimeoutWithoutPolicyIsUnchanged(t *testing.T) {
	t.Parallel()
	scenario, runs := timeoutRetryScenario(t, "sleep 2\n", 1, nil)

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != "" || !strings.Contains(err.Error(), "command batch timed out") || strings.Contains(err.Error(), "admission_timeout_retry") {
		t.Fatalf("error = %v, want today's unclassified batch timeout", err)
	}
	if got := runs(); got != 1 {
		t.Fatalf("canonical command ran %d times, want 1", got)
	}
	if task, _ := scenario.BB.GetTask(scenario.TaskID); task.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want it still executing", task.Status)
	}
}

// An invalid policy behaves as none, and the timeout says why.
func TestSubmitTimeoutUnderInvalidPolicyNamesTheField(t *testing.T) {
	t.Parallel()
	scenario, runs := timeoutRetryScenario(t, "sleep 2\n", 1, retryPolicy(0))

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != "" || !strings.Contains(err.Error(), "admission_timeout_retry is set but invalid (budget_factor") {
		t.Fatalf("error = %v, want an unclassified timeout naming the invalid field", err)
	}
	if got := runs(); got != 1 {
		t.Fatalf("canonical command ran %d times, want 1", got)
	}
}

// A failing command is a content failure: the policy never re-runs it.
func TestSubmitTimeoutRetryIgnoresFailingCommands(t *testing.T) {
	t.Parallel()
	scenario, runs := timeoutRetryScenario(t, "exit 3\n", 30, retryPolicy(2))

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || refusal.Class != "" || !strings.Contains(err.Error(), "exit 3") {
		t.Fatalf("error = %v, want the unclassified command failure", err)
	}
	if got := runs(); got != 1 {
		t.Fatalf("canonical command ran %d times, want 1", got)
	}
	if task, _ := scenario.BB.GetTask(scenario.TaskID); task.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want it still executing", task.Status)
	}
}

// The early marker covers the slot wait, the batch and the retry, and the
// retry is clipped to that envelope rather than claiming the configured one.
func TestSubmitTimeoutRetryMarkerCoversWaitBatchAndRetry(t *testing.T) {
	scenario, runs := timeoutRetryScenario(t, markerCopyScript()+"sleep 2\n", 1, retryPolicy(10), suiteAdmissionLimit)
	shortAdmissionSlotCap(t, 30*time.Second)
	shortSubmitSlotWaitFloor(t, 5*time.Second)
	// wait = cap - budget = 29 s; envelope = 30 + 5 - 29 - 1 = 5 s < 10 s.

	result, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	if err != nil || result == nil {
		t.Fatalf("submit = %+v, %v; want a passing retry", result, err)
	}
	if window, want := copiedMarkerWindow(t, scenario.Root), 35*time.Second+submitGraceMargin; window != want {
		t.Fatalf("marker window = %s, want wait 29 s + batch 1 s + retry 5 s plus grace = %s", window, want)
	}
	task, err := scenario.BB.GetTask(scenario.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	want := models.AcceptanceTimeoutRetry{TimedOutIndex: 0, TimeoutSeconds: 1, RetryTimeoutSeconds: 5, ConfiguredTimeoutSeconds: 10, ClippedBy: models.AdmissionRetryClippedByEnvelope, Ruling: testTimeoutRuling}
	if got := task.AcceptanceReceipt.TimeoutRetry; got == nil || *got != want {
		t.Fatalf("retry record = %+v, want %+v", got, want)
	}
	if got := runs(); got != 2 {
		t.Fatalf("canonical command ran %d times, want 2", got)
	}
}

// A budget that changes while the submit waits for its slot no longer fits
// the marker published for the old one: the submit refuses before any
// command runs. A changed live policy does not matter: the snapshot taken
// with the marker governs.
func TestSubmitRefusesABudgetChangedAfterTheMarkerWasPublished(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(state *models.State, task *models.Task)
		refuse bool
	}{
		"budget exception raises the batch budget": {func(_ *models.State, task *models.Task) {
			task.History = append(task.History, models.TaskHistoryEntry{
				Time: time.Now().UTC(), Event: models.TaskEventUnblocked, Agent: testhelpers.StringPtr("operator"),
				Extra: map[string]any{historyExtraBudgetException: BudgetException{3, "D122"}.historyValue()},
			})
		}, true},
		"live policy factor changes": {func(state *models.State, _ *models.Task) {
			state.Config.AdmissionTimeoutRetry = retryPolicy(5)
		}, false},
		"nothing changes": {func(*models.State, *models.Task) {}, false},
	} {
		t.Run(name, func(t *testing.T) {
			scenario, runs := timeoutRetryScenario(t, "exit 0\n", 1, retryPolicy(2), suiteAdmissionLimit)
			shortAdmissionSlotCap(t, 20*time.Second)
			shortSubmitSlotWaitFloor(t, 5*time.Second)
			held := holdAdmissionSlots(t, scenario.Root, suiteAdmissionLimit)
			done := make(chan error, 1)
			go func() {
				_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)
				done <- err
			}()
			waitForTickets(t, scenario.Root, suiteAdmissionLimit.Class, 1)
			time.Sleep(1500 * time.Millisecond) // A substantial part of the wait elapses.
			if err := scenario.BB.Modify(func(state *models.State) error {
				tc.change(state, state.FindTask(scenario.TaskID))
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			held.release()
			err := <-done

			if !tc.refuse {
				if err != nil || runs() != 1 {
					t.Fatalf("submit = %v after %d runs; want it to pass under the published budgets", err, runs())
				}
				return
			}
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleStateChanged ||
				lifecycleErr.Outcome.SafeAction != "requery" || !strings.Contains(err.Error(), "budget changed after the submit queued") {
				t.Fatalf("submit = %v; want STATE_CHANGED/requery for the changed budget", err)
			}
			if got := runs(); got != 0 {
				t.Fatalf("canonical command ran %d times after the budget changed, want 0", got)
			}
			if head := testhelpers.MustGit(t, filepath.Join(scenario.Root, ".worktrees", scenario.TaskID), "rev-parse", "HEAD"); head != scenario.Commit {
				t.Fatalf("refusal changed the worktree: HEAD %s, submitted %s", head, scenario.Commit)
			}
		})
	}
}

// The block is fenced to the transition the timed-out submit left: a task
// released and reclaimed by the same agent meanwhile is not blocked.
func TestSubmitTimeoutBlockSkipsATaskThatMovedOn(t *testing.T) {
	scenario, _ := timeoutRetryScenario(t, "sleep 10\n", 1, retryPolicy(2))
	previous := submitBatchTimeoutBlockTestHook
	submitBatchTimeoutBlockTestHook = func() {
		if err := scenario.BB.Modify(func(state *models.State) error {
			task := state.FindTask(scenario.TaskID)
			agent := scenario.Authority.ID
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: &agent})
			models.AdvanceLifecycle(task)
			return nil
		}); err != nil {
			t.Errorf("reclaim: %v", err)
		}
	}
	t.Cleanup(func() { submitBatchTimeoutBlockTestHook = previous })

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	refusal := requireBatchTimeoutRefusal(t, err)
	if refusal.Blocked || refusal.BlockFailed || refusal.BlockSkipped != "the task moved on after this submit" {
		t.Fatalf("refusal block flags = blocked %v, failed %v, skipped %q; want skipped as moved on", refusal.Blocked, refusal.BlockFailed, refusal.BlockSkipped)
	}
	if task, _ := scenario.BB.GetTask(scenario.TaskID); task.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want the newer claim still executing", task.Status)
	}
}

// A block that fails for an operational reason, the task unchanged, is not
// reported as the task having moved on: the doer is told to block it.
func TestSubmitTimeoutBlockFailureIsNotMistakenForMovement(t *testing.T) {
	scenario, _ := timeoutRetryScenario(t, "sleep 10\n", 1, retryPolicy(2))
	previous := submitBatchTimeoutBlockTestHook
	submitBatchTimeoutBlockTestHook = func() {
		// mark-blocked cannot load the pipeline, before it reads the task.
		if err := os.WriteFile(filepath.Join(paths.New(scenario.Root).LizaDir(), "pipeline.yaml"), []byte(": not yaml ["), 0o644); err != nil {
			t.Errorf("break pipeline: %v", err)
		}
	}
	t.Cleanup(func() { submitBatchTimeoutBlockTestHook = previous })

	_, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority)

	refusal := requireBatchTimeoutRefusal(t, err)
	if refusal.Blocked || !refusal.BlockFailed || refusal.BlockSkipped != "" || !strings.Contains(err.Error(), "blocking the task failed") {
		t.Fatalf("refusal block flags = blocked %v, failed %v, skipped %q; error %v; want a failed block", refusal.Blocked, refusal.BlockFailed, refusal.BlockSkipped, err)
	}
	if task, _ := scenario.BB.GetTask(scenario.TaskID); task.Status != models.TaskStatusImplementing {
		t.Fatalf("task status = %s, want it still executing for the doer to block", task.Status)
	}
}

// Only the batch's own deadline is a batch timeout.
func TestAcceptanceBatchTimeoutIsTypedOnlyForItsOwnDeadline(t *testing.T) {
	t.Parallel()
	requirePosixShell(t)
	worktree := t.TempDir()
	run := func(ctx context.Context, command string, budgetSeconds int) error {
		_, err := executeAcceptanceCommandsWith(ctx, "task-1", worktree, []string{"true", command}, nil, budgetSeconds, nil, nil)
		return err
	}
	var timeout *acceptanceBatchTimeoutError
	if err := run(context.Background(), "sleep 5", 1); !errors.As(err, &timeout) || timeout.Index != 1 || !errors.Is(err, errAcceptanceBatchTimedOut) {
		t.Fatalf("own deadline: %v, want a typed timeout at command 1", err)
	}
	cancelled, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := run(cancelled, "sleep 5", 60); err == nil || errors.Is(err, errAcceptanceBatchTimedOut) {
		t.Fatalf("cancelled caller: %v, want an untyped error", err)
	}
	if err := run(context.Background(), "head -c 2000000 /dev/zero", 60); err == nil || errors.Is(err, errAcceptanceBatchTimedOut) {
		t.Fatalf("output overflow: %v, want an untyped error", err)
	}
}

func TestPlanTimeoutRetryBudgets(t *testing.T) {
	t.Parallel()
	config := func(policy *models.AdmissionTimeoutRetry) models.Config {
		return models.Config{AdmissionTimeoutRetry: policy}
	}
	withMax := func(factor, maxSeconds int) *models.AdmissionTimeoutRetry {
		return &models.AdmissionTimeoutRetry{BudgetFactor: factor, MaxTimeoutSeconds: maxSeconds, Ruling: testTimeoutRuling}
	}
	for name, tc := range map[string]struct {
		policy             *models.AdmissionTimeoutRetry
		budget             int
		limited            bool
		seconds, configure int
		clipped            string
	}{
		"run's 2x/7200 policy at 3600 s":   {withMax(2, 7200), 3600, true, 7200, 7200, ""},
		"envelope clips a 7200 s budget":   {withMax(2, 0), 7200, true, 7200, 14400, models.AdmissionRetryClippedByEnvelope},
		"no room after a 10800 s budget":   {withMax(2, 0), 10800, true, 0, 21600, models.AdmissionRetryClippedByEnvelope},
		"unlimited default budget doubles": {withMax(2, 0), 0, false, 1200, 1200, ""},
		"maximum clips":                    {withMax(10, 3000), 600, false, 3000, 6000, models.AdmissionRetryClippedByMax},
		"engine cap clips":                 {withMax(100, 0), 600, false, 14400, 60000, models.AdmissionRetryClippedByCap},
		"factor 1 is an unchanged rerun":   {withMax(1, 0), 3600, true, 3600, 3600, ""},
		"largest factor does not overflow": {withMax(referencecontract.AcceptanceMaxTimeoutSeconds, 0), referencecontract.AcceptanceMaxTimeoutSeconds, false, 0, referencecontract.AcceptanceMaxTimeoutSeconds * referencecontract.AcceptanceMaxTimeoutSeconds, models.AdmissionRetryClippedByEnvelope},
	} {
		plan := planTimeoutRetry(config(tc.policy), roles.Coder, nil, tc.budget, tc.limited)
		if !plan.applies() || plan.seconds != tc.seconds || plan.configured != tc.configure || plan.clippedBy != tc.clipped || (tc.seconds == 0) != (plan.noRetry != "") {
			t.Errorf("%s: plan = %+v, want %d s (configured %d, clipped %q)", name, plan, tc.seconds, tc.configure, tc.clipped)
		}
	}

	if plan := planTimeoutRetry(config(nil), roles.Coder, nil, 600, false); plan != nil {
		t.Errorf("no policy: plan = %+v, want nil", plan)
	}
	for name, policy := range map[string]*models.AdmissionTimeoutRetry{
		"zero factor":   withMax(0, 0),
		"huge factor":   withMax(referencecontract.AcceptanceMaxTimeoutSeconds+1, 0),
		"max above cap": withMax(2, referencecontract.AcceptanceMaxTimeoutSeconds+1),
		"blank ruling":  {BudgetFactor: 2},
	} {
		if plan := planTimeoutRetry(config(policy), roles.Coder, nil, 600, false); plan == nil || plan.invalid == nil || plan.applies() {
			t.Errorf("%s: plan = %+v, want invalid", name, plan)
		}
	}
	if plan := planTimeoutRetry(config(withMax(2, 0)), "code-planner", nil, 600, false); plan.seconds != 0 || !strings.Contains(plan.noRetry, "coding tasks only") {
		t.Errorf("non-coding doer: plan = %+v, want no retry", plan)
	}
	singleUse := []models.RuntimeInput{{ID: "fixture", Consumption: models.RuntimeInputSingleUse}}
	if plan := planTimeoutRetry(config(withMax(2, 0)), roles.Coder, singleUse, 600, false); plan.seconds != 0 || !strings.Contains(plan.noRetry, "fixture is single_use") {
		t.Errorf("single_use input: plan = %+v, want no retry", plan)
	}
}

// Whatever the budget, wait + batch + retry stay within cap + floor, and a
// retry is never shorter than the budget that ran out.
func TestPlanTimeoutRetryStaysInsideTheSubmitEnvelope(t *testing.T) {
	t.Parallel()
	config := models.Config{AdmissionTimeoutRetry: retryPolicy(3)}
	for budget := 1; budget <= referencecontract.AcceptanceMaxTimeoutSeconds; budget += 7 {
		for _, limited := range []bool{false, true} {
			plan := planTimeoutRetry(config, roles.Coder, nil, budget, limited)
			if window := submitAdmissionWindow(budget, limited, plan); window > admissionSlotCap+submitSlotWaitFloor {
				t.Fatalf("budget %d limited %v: window %s exceeds cap plus floor", budget, limited, window)
			}
			if plan.seconds != 0 && plan.seconds < budget {
				t.Fatalf("budget %d limited %v: retry %d s shorter than the budget", budget, limited, plan.seconds)
			}
		}
	}
}

func TestValidateTimeoutRetryRecord(t *testing.T) {
	t.Parallel()
	valid := models.AcceptanceTimeoutRetry{TimedOutIndex: 1, TimeoutSeconds: 600, RetryTimeoutSeconds: 1200, ConfiguredTimeoutSeconds: 1200, Ruling: testTimeoutRuling}
	executed := map[int]bool{0: true, 1: true, 2: false}
	for name, mutate := range map[string]func(*models.AcceptanceTimeoutRetry){
		"deferred index":     func(r *models.AcceptanceTimeoutRetry) { r.TimedOutIndex = 2 },
		"retry below budget": func(r *models.AcceptanceTimeoutRetry) { r.RetryTimeoutSeconds = 300 },
		"above cap": func(r *models.AcceptanceTimeoutRetry) {
			r.RetryTimeoutSeconds, r.ConfiguredTimeoutSeconds = 20000, 20000
		},
		"clipped without bound": func(r *models.AcceptanceTimeoutRetry) { r.ConfiguredTimeoutSeconds = 2400 },
		"unknown bound":         func(r *models.AcceptanceTimeoutRetry) { r.ConfiguredTimeoutSeconds, r.ClippedBy = 2400, "mood" },
		"bound without clip":    func(r *models.AcceptanceTimeoutRetry) { r.ClippedBy = models.AdmissionRetryClippedByCap },
		"blank ruling":          func(r *models.AcceptanceTimeoutRetry) { r.Ruling = "" },
	} {
		record := valid
		mutate(&record)
		if err := validateTimeoutRetryRecord("task-1", &models.AcceptanceReceipt{TimeoutRetry: &record}, executed); err == nil {
			t.Errorf("%s: record %+v accepted", name, record)
		}
	}
	clipped := valid
	clipped.ConfiguredTimeoutSeconds, clipped.ClippedBy = 6000, models.AdmissionRetryClippedByMax
	for _, record := range []*models.AcceptanceTimeoutRetry{nil, &valid, &clipped} {
		if err := validateTimeoutRetryRecord("task-1", &models.AcceptanceReceipt{TimeoutRetry: record}, executed); err != nil {
			t.Errorf("record %+v refused: %v", record, err)
		}
	}
}
