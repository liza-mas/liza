package agent

import (
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// fakeClaimFailureObserver pins the claimFailureObserver declaration to
// claim_breaker.go: the assertion below fails to compile if the interface
// moves or changes shape.
type fakeClaimFailureObserver struct {
	decision claimBreakerDecision
}

func (f *fakeClaimFailureObserver) ObserveClaimFailure(error) claimBreakerDecision {
	return f.decision
}

var _ claimFailureObserver = (*fakeClaimFailureObserver)(nil)

const (
	breakerTestRole = "code-reviewer"
	breakerTestTask = "task-a"
)

var breakerTestNow = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func quarantineFailure(taskID, boundaryVersion string) *ops.ReviewClaimFailure {
	return &ops.ReviewClaimFailure{
		Role:  breakerTestRole,
		Class: ops.ReviewClaimClassCandidateFailures,
		Candidates: []ops.ReviewClaimCandidateFailure{{
			TaskID:          taskID,
			Class:           ops.ReviewClaimClassReviewBoundaryRepair,
			BoundaryVersion: boundaryVersion,
		}},
	}
}

func candidateFreeFailure(class string, transient bool) *ops.ReviewClaimFailure {
	return &ops.ReviewClaimFailure{Role: breakerTestRole, Class: class, Transient: transient}
}

func reviewableTask(id string) models.Task {
	return testhelpers.BuildTaskByStatus(id, models.TaskStatusReadyForReview, breakerTestNow)
}

// breakerTestResolver resolves the shipped pipeline, whose coding-pair
// submitted status is the one BuildTaskByStatus assigns.
func breakerTestResolver(t *testing.T) models.PipelineResolver {
	t.Helper()
	cfg, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatalf("LoadEmbeddedReference() error = %v", err)
	}
	return pipeline.NewResolver(cfg)
}

func TestClaimBreakerObserverInterface(t *testing.T) {
	want := claimBreakerDecision{Stop: true, Delay: time.Second}
	var observer claimFailureObserver = &fakeClaimFailureObserver{decision: want}

	got := observer.ObserveClaimFailure(nil)

	if got.Stop != want.Stop || got.Delay != want.Delay || len(got.Opened) != 0 {
		t.Fatalf("ObserveClaimFailure() = %+v, want %+v", got, want)
	}
}

func TestClaimBreakerOpensAfterThreeIdenticalFailures(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	version := ops.ReviewClaimBoundaryVersion(&task)
	failure := quarantineFailure(breakerTestTask, version)

	for i := 1; i <= 2; i++ {
		decision := breaker.Observe(failure, breakerTestNow)
		if decision.Stop || len(decision.Opened) != 0 {
			t.Fatalf("failure %d: decision = %+v, want no key opened", i, decision)
		}
		if decision.Delay >= claimQuarantineCooldown {
			t.Fatalf("failure %d: delay = %v, want below the cooldown", i, decision.Delay)
		}
		if breaker.Quarantined(&task, breakerTestRole, breakerTestNow) {
			t.Fatalf("failure %d: task quarantined before the threshold", i)
		}
	}

	decision := breaker.Observe(failure, breakerTestNow)

	wantKey := claimBreakerKey{Role: breakerTestRole, TaskID: breakerTestTask, Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: version}
	if len(decision.Opened) != 1 || decision.Opened[0] != wantKey {
		t.Fatalf("Opened = %+v, want [%+v]", decision.Opened, wantKey)
	}
	if decision.Delay != claimQuarantineCooldown {
		t.Fatalf("Delay = %v, want the cooldown %v", decision.Delay, claimQuarantineCooldown)
	}
	if !breaker.Quarantined(&task, breakerTestRole, breakerTestNow) {
		t.Fatal("task not quarantined after three identical failures")
	}
	counters, ok := breaker.Counters(wantKey)
	if !ok {
		t.Fatal("Counters() missing for the opened key")
	}
	if counters.Attempts != 3 || !counters.FirstFailure.Equal(breakerTestNow) || !counters.LastFailure.Equal(breakerTestNow) {
		t.Fatalf("counters = %+v", counters)
	}
	if !counters.CooldownUntil.Equal(breakerTestNow.Add(claimQuarantineCooldown)) {
		t.Fatalf("CooldownUntil = %v, want %v", counters.CooldownUntil, breakerTestNow.Add(claimQuarantineCooldown))
	}
}

func TestClaimBreakerMixedCandidatesOpenOneKeyAndKeepBackoff(t *testing.T) {
	breaker := newClaimBreaker()
	taskA, taskB := reviewableTask("task-a"), reviewableTask("task-b")
	failure := &ops.ReviewClaimFailure{
		Role:  breakerTestRole,
		Class: ops.ReviewClaimClassCandidateFailures,
		Candidates: []ops.ReviewClaimCandidateFailure{
			{TaskID: "task-a", Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: ops.ReviewClaimBoundaryVersion(&taskA)},
			{TaskID: "task-b", Class: ops.ReviewClaimClassGitOperation, Transient: true, BoundaryVersion: ops.ReviewClaimBoundaryVersion(&taskB)},
		},
	}

	var decision claimBreakerDecision
	for i := 0; i < 3; i++ {
		decision = breaker.Observe(failure, breakerTestNow)
	}

	if len(decision.Opened) != 1 || decision.Opened[0].TaskID != "task-a" {
		t.Fatalf("Opened = %+v, want exactly the review_boundary_repair key for task-a", decision.Opened)
	}
	if want := 4 * claimBackoffBase; decision.Delay != want {
		t.Fatalf("Delay = %v, want the grown backoff %v", decision.Delay, want)
	}
	if !breaker.Quarantined(&taskA, breakerTestRole, breakerTestNow) {
		t.Fatal("task-a not quarantined")
	}
	if breaker.Quarantined(&taskB, breakerTestRole, breakerTestNow) {
		t.Fatal("backoff-class candidate task-b must never be quarantined")
	}
}

func TestClaimBreakerChangedBoundaryVersionRestartsCounting(t *testing.T) {
	breaker := newClaimBreaker()
	before := reviewableTask(breakerTestTask)
	after := reviewableTask(breakerTestTask)
	after.History = append(after.History, models.TaskHistoryEntry{Time: breakerTestNow, Event: "update-review-commit"})
	versionBefore, versionAfter := ops.ReviewClaimBoundaryVersion(&before), ops.ReviewClaimBoundaryVersion(&after)
	if versionBefore == versionAfter {
		t.Fatal("fixture: boundary versions must differ")
	}
	for i := 0; i < 3; i++ {
		breaker.Observe(quarantineFailure(breakerTestTask, versionBefore), breakerTestNow)
	}
	if !breaker.Quarantined(&before, breakerTestRole, breakerTestNow) {
		t.Fatal("fixture: key not open before the boundary changed")
	}

	decision := breaker.Observe(quarantineFailure(breakerTestTask, versionAfter), breakerTestNow)

	if len(decision.Opened) != 0 {
		t.Fatalf("Opened = %+v, want none after a boundary change", decision.Opened)
	}
	if breaker.Quarantined(&after, breakerTestRole, breakerTestNow) {
		t.Fatal("task quarantined on the first failure against the new boundary")
	}
	if breaker.Quarantined(&before, breakerTestRole, breakerTestNow) {
		t.Fatal("old boundary key was not dropped")
	}
	if _, ok := breaker.Counters(claimBreakerKey{Role: breakerTestRole, TaskID: breakerTestTask, Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: versionBefore}); ok {
		t.Fatal("Counters() still reports the dropped key")
	}
	counters, ok := breaker.Counters(claimBreakerKey{Role: breakerTestRole, TaskID: breakerTestTask, Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: versionAfter})
	if !ok || counters.Attempts != 1 {
		t.Fatalf("new key counters = %+v, ok=%v; want attempts restarted at 1", counters, ok)
	}

	// Two more identical failures reopen against the new boundary.
	breaker.Observe(quarantineFailure(breakerTestTask, versionAfter), breakerTestNow)
	decision = breaker.Observe(quarantineFailure(breakerTestTask, versionAfter), breakerTestNow)
	if len(decision.Opened) != 1 || decision.Opened[0].BoundaryVersion != versionAfter {
		t.Fatalf("Opened = %+v, want the new-boundary key", decision.Opened)
	}
}

func TestClaimBreakerQuarantineIgnoresTaskWhoseBoundaryMoved(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	for i := 0; i < 3; i++ {
		breaker.Observe(quarantineFailure(breakerTestTask, ops.ReviewClaimBoundaryVersion(&task)), breakerTestNow)
	}

	repaired := "repaired-sha"
	task.ReviewCommit = &repaired

	if breaker.Quarantined(&task, breakerTestRole, breakerTestNow) {
		t.Fatal("task still quarantined after its review_commit changed")
	}
}

func TestClaimBreakerQuarantineExcludesOnlyTheFailingTask(t *testing.T) {
	breaker := newClaimBreaker()
	pr := breakerTestResolver(t)
	quarantined, healthy := reviewableTask("task-a"), reviewableTask("task-b")
	state := &models.State{Tasks: []models.Task{quarantined, healthy}}
	if got := breaker.ClaimableAfterQuarantine(state, breakerTestRole, "code-reviewer-1", pr, breakerTestNow); got != 2 {
		t.Fatalf("fixture: ClaimableAfterQuarantine() = %d, want 2", got)
	}

	for i := 0; i < 3; i++ {
		breaker.Observe(quarantineFailure("task-a", ops.ReviewClaimBoundaryVersion(&quarantined)), breakerTestNow)
	}

	if got := breaker.ClaimableAfterQuarantine(state, breakerTestRole, "code-reviewer-1", pr, breakerTestNow); got != 1 {
		t.Fatalf("ClaimableAfterQuarantine() = %d, want 1 (task-b stays claimable)", got)
	}
	if breaker.Quarantined(&healthy, breakerTestRole, breakerTestNow) {
		t.Fatal("healthy task reported quarantined")
	}
	if got := breaker.ClaimableAfterQuarantine(state, "architecture-reviewer", "code-reviewer-1", pr, breakerTestNow); got != 0 {
		t.Fatalf("ClaimableAfterQuarantine() for another role = %d, want 0 (IsClaimable honored)", got)
	}

	// The count still honors the already-approved filter.
	state.Tasks[1].Approvals = []models.Approval{{Agent: "code-reviewer-1", Provider: "claude"}}
	if got := breaker.ClaimableAfterQuarantine(state, breakerTestRole, "code-reviewer-1", pr, breakerTestNow); got != 0 {
		t.Fatalf("ClaimableAfterQuarantine() = %d, want 0 once task-b is approved by this agent", got)
	}
}

func TestClaimBreakerTransientBackoffGrowsAndResets(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	gitFailure := candidateFreeFailure(ops.ReviewClaimClassGitOperation, true)

	want := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 80 * time.Second, 160 * time.Second, 5 * time.Minute, 5 * time.Minute}
	for i, delay := range want {
		decision := breaker.Observe(gitFailure, breakerTestNow)
		if decision.Delay != delay {
			t.Fatalf("failure %d: Delay = %v, want %v", i+1, decision.Delay, delay)
		}
		if decision.Stop || len(decision.Opened) != 0 {
			t.Fatalf("failure %d: transient class opened a key or stopped: %+v", i+1, decision)
		}
	}
	if breaker.Quarantined(&task, breakerTestRole, breakerTestNow) {
		t.Fatal("transient failures quarantined a task")
	}

	// A change of class restarts the schedule.
	if got := breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassUnclassified, true), breakerTestNow).Delay; got != claimBackoffBase {
		t.Fatalf("after class change Delay = %v, want %v", got, claimBackoffBase)
	}
	if got := breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassUnclassified, true), breakerTestNow).Delay; got != 2*claimBackoffBase {
		t.Fatalf("second failure of the new class Delay = %v, want %v", got, 2*claimBackoffBase)
	}

	// A successful claim restarts it too.
	breaker.ObserveSuccess(breakerTestRole, breakerTestTask)
	if got := breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassUnclassified, true), breakerTestNow).Delay; got != claimBackoffBase {
		t.Fatalf("after success Delay = %v, want %v", got, claimBackoffBase)
	}
}

func TestClaimBreakerBackoffClassCandidatesGrowBackoff(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	failure := &ops.ReviewClaimFailure{
		Role:  breakerTestRole,
		Class: ops.ReviewClaimClassCandidateFailures,
		Candidates: []ops.ReviewClaimCandidateFailure{{
			TaskID: breakerTestTask, Class: ops.ReviewClaimClassIntegrationFailed, BoundaryVersion: ops.ReviewClaimBoundaryVersion(&task),
		}},
	}

	for i, want := range []time.Duration{claimBackoffBase, 2 * claimBackoffBase, 4 * claimBackoffBase, 8 * claimBackoffBase} {
		decision := breaker.Observe(failure, breakerTestNow)
		if decision.Delay != want || len(decision.Opened) != 0 {
			t.Fatalf("failure %d: decision = %+v, want Delay %v and no key", i+1, decision, want)
		}
	}
	if breaker.Quarantined(&task, breakerTestRole, breakerTestNow) {
		t.Fatal("integration_failed candidate must not be quarantined")
	}
}

func TestClaimBreakerStopAndNoWorkClassesTouchNoCounters(t *testing.T) {
	breaker := newClaimBreaker()
	// Grow the backoff first so a no_work failure is observed against live state.
	breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassGitOperation, true), breakerTestNow)
	breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassGitOperation, true), breakerTestNow)

	for _, class := range []string{ops.ReviewClaimClassAuthority, ops.ReviewClaimClassDegraded} {
		decision := breaker.Observe(candidateFreeFailure(class, false), breakerTestNow)
		if !decision.Stop || decision.Delay != 0 || len(decision.Opened) != 0 {
			t.Fatalf("%s: decision = %+v, want Stop only", class, decision)
		}
	}

	noWork := breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassNoWork, false), breakerTestNow)
	if noWork.Stop || noWork.Delay != claimBackoffBase || len(noWork.Opened) != 0 {
		t.Fatalf("no_work: decision = %+v, want the base delay and nothing else", noWork)
	}

	// The git_operation schedule continues where it left off: 5s, 10s, then 20s.
	if got := breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassGitOperation, true), breakerTestNow).Delay; got != 4*claimBackoffBase {
		t.Fatalf("backoff after stop/no_work observations = %v, want %v (counters untouched)", got, 4*claimBackoffBase)
	}

	if got := breaker.Observe(nil, breakerTestNow); got.Stop || got.Delay != 0 || len(got.Opened) != 0 {
		t.Fatalf("Observe(nil) = %+v, want the zero decision", got)
	}
}

func TestClaimBreakerCooldownDoublesPerFailedReprobeUpToCap(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	failure := quarantineFailure(breakerTestTask, ops.ReviewClaimBoundaryVersion(&task))
	now := breakerTestNow
	for i := 0; i < 3; i++ {
		breaker.Observe(failure, now)
	}
	cooldown := claimQuarantineCooldown

	for _, want := range []time.Duration{10 * time.Minute, 20 * time.Minute, 30 * time.Minute, 30 * time.Minute} {
		now = now.Add(cooldown)
		if breaker.Quarantined(&task, breakerTestRole, now) {
			t.Fatalf("task still quarantined once its %v cooldown expired", cooldown)
		}

		decision := breaker.Observe(failure, now)

		if decision.Delay != want || len(decision.Opened) != 1 {
			t.Fatalf("re-probe after %v: decision = %+v, want Delay %v and the key re-opened", cooldown, decision, want)
		}
		counters, _ := breaker.Counters(decision.Opened[0])
		if !counters.CooldownUntil.Equal(now.Add(want)) {
			t.Fatalf("CooldownUntil = %v, want %v", counters.CooldownUntil, now.Add(want))
		}
		if !breaker.Quarantined(&task, breakerTestRole, now.Add(want-time.Second)) {
			t.Fatal("task not quarantined for the doubled cooldown")
		}
		cooldown = want
	}
}

func TestClaimBreakerPermitsExactlyOneProbeAfterExpiry(t *testing.T) {
	breaker := newClaimBreaker()
	task := reviewableTask(breakerTestTask)
	failure := quarantineFailure(breakerTestTask, ops.ReviewClaimBoundaryVersion(&task))
	for i := 0; i < 3; i++ {
		breaker.Observe(failure, breakerTestNow)
	}
	if !breaker.Quarantined(&task, breakerTestRole, breakerTestNow.Add(claimQuarantineCooldown-time.Second)) {
		t.Fatal("task released before its cooldown expired")
	}
	expired := breakerTestNow.Add(claimQuarantineCooldown)

	// Reads do not consume the probe: the task stays offered until a claim resolves it.
	for i := 0; i < 3; i++ {
		if breaker.Quarantined(&task, breakerTestRole, expired) {
			t.Fatalf("read %d after expiry: task still quarantined", i+1)
		}
	}

	// One failed probe re-quarantines immediately; three are not needed again.
	decision := breaker.Observe(failure, expired)
	if len(decision.Opened) != 1 {
		t.Fatalf("Opened = %+v, want the key re-opened after a single failed probe", decision.Opened)
	}
	if !breaker.Quarantined(&task, breakerTestRole, expired) {
		t.Fatal("task not re-quarantined after the failed probe")
	}
	counters, _ := breaker.Counters(decision.Opened[0])
	if counters.Attempts != 4 || !counters.FirstFailure.Equal(breakerTestNow) || !counters.LastFailure.Equal(expired) {
		t.Fatalf("counters = %+v, want attempts 4 with first/last failure preserved", counters)
	}

	// A failure counted while the cooldown is still open neither re-opens nor extends it.
	during := expired.Add(time.Minute)
	decision = breaker.Observe(failure, during)
	if len(decision.Opened) != 0 {
		t.Fatalf("Opened = %+v during an open cooldown, want none", decision.Opened)
	}
	key := claimBreakerKey{Role: breakerTestRole, TaskID: breakerTestTask, Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: ops.ReviewClaimBoundaryVersion(&task)}
	counters, _ = breaker.Counters(key)
	if counters.Attempts != 5 || !counters.CooldownUntil.Equal(expired.Add(2*claimQuarantineCooldown)) {
		t.Fatalf("counters = %+v, want attempts 5 and the cooldown unchanged", counters)
	}

	// A successful probe clears the key for good.
	breaker.ObserveSuccess(breakerTestRole, breakerTestTask)
	if breaker.Quarantined(&task, breakerTestRole, during) {
		t.Fatal("task still quarantined after a successful claim")
	}
	if _, ok := breaker.Counters(key); ok {
		t.Fatal("Counters() still reports a cleared key")
	}
}

func TestClaimBreakerConcurrentReadsAreRaceFree(t *testing.T) {
	breaker := newClaimBreaker()
	pr := breakerTestResolver(t)
	taskA, taskB := reviewableTask("task-a"), reviewableTask("task-b")
	state := &models.State{Tasks: []models.Task{taskA, taskB}}
	failure := quarantineFailure("task-a", ops.ReviewClaimBoundaryVersion(&taskA))
	key := claimBreakerKey{Role: breakerTestRole, TaskID: "task-a", Class: ops.ReviewClaimClassReviewBoundaryRepair, BoundaryVersion: ops.ReviewClaimBoundaryVersion(&taskA)}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				breaker.Quarantined(&taskA, breakerTestRole, breakerTestNow)
				breaker.ClaimableAfterQuarantine(state, breakerTestRole, "code-reviewer-1", pr, breakerTestNow)
				breaker.Counters(key)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 200; j++ {
			breaker.Observe(failure, breakerTestNow)
			breaker.Observe(candidateFreeFailure(ops.ReviewClaimClassGitOperation, true), breakerTestNow)
			if j%50 == 49 {
				breaker.ObserveSuccess(breakerTestRole, "task-a")
			}
		}
	}()
	wg.Wait()

	if breaker.Quarantined(&taskB, breakerTestRole, breakerTestNow) {
		t.Fatal("task-b was never observed failing but is quarantined")
	}
}

// TestClaimBreakerConstantsAreNotConfigurable is the SC-no-config proof: the
// bounds are unexported constants of the policy file, and nothing in that file
// reads a Config field.
func TestClaimBreakerConstantsAreNotConfigurable(t *testing.T) {
	if claimBreakerThreshold != 3 || claimQuarantineCooldown != 5*time.Minute || claimQuarantineCooldownMax != 30*time.Minute ||
		claimBackoffBase != 5*time.Second || claimBackoffMax != 5*time.Minute {
		t.Fatal("claim-breaker constants drifted from the plan's D5 values")
	}
	source, err := os.ReadFile("claim_breaker.go")
	if err != nil {
		t.Fatalf("ReadFile(claim_breaker.go) error = %v", err)
	}
	src := string(source)
	for _, name := range []string{"claimBreakerThreshold", "claimQuarantineCooldown", "claimQuarantineCooldownMax", "claimBackoffBase", "claimBackoffMax"} {
		if !regexp.MustCompile(`(?m)^\s*` + name + `\s*=`).MatchString(src) {
			t.Errorf("%s is not declared as a constant in claim_breaker.go", name)
		}
	}
	if strings.Contains(src, "Config") {
		t.Error("claim_breaker.go reads a Config field; the bounds must stay constants")
	}
}
