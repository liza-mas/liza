package ops

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
)

// Deferred validation (ADR-0204) runs the canonical commands admission
// deferred as one batch at integration HEAD, outside any task session and
// outside the state lock, and bisects a failing command over the merges since
// it last passed. Each command runs in its own lane, a worktree where it is
// prepared, probed and bisected; lanes run concurrently, bounded only by the
// admission classes (D-210), and prepare one at a time (D-223). One runner at
// a time holds an OS lock that every child it starts inherits where the
// platform allows, so no successor can touch a lane worktree while a
// predecessor's process still runs.

const (
	deferredValidationOperation      = "deferred-validation"
	deferredValidationMaxRunsPerCall = 3
	deferredValidationGitTimeout     = 5 * time.Minute
	deferredValidationDirName        = "deferred-validation"
	// deferredValidationWaitDelay is how long a step's pipes may stay open
	// after its process ended.
	deferredValidationWaitDelay = 5 * time.Second
	// deferredValidationAlertCategory is the alert category of a failed batch.
	deferredValidationAlertCategory = "DEFERRED VALIDATION FAILED"
	// deferredValidationUnattributedOwner is the discovery owner of a culprit
	// no task could be named for.
	deferredValidationUnattributedOwner = "unattributed"
	// deferredValidationTokenBytes is the random size of a runner token.
	deferredValidationTokenBytes = 16
	// IntegrationProgressWaitingDeferredValidation is the stable integration
	// wait on which the orchestrator supervisor retries a pending batch.
	IntegrationProgressWaitingDeferredValidation = "deferred_validation_pending"

	integrationProgressBlockedDeferredFailed    = "deferred_validation_failed"
	integrationProgressBlockedDeferredInfra     = "deferred_validation_infra"
	integrationProgressBlockedDeferredUnsettled = "deferred_validation_unsettled"
	integrationProgressBlockedDeferredAborted   = "deferred_validation_aborted"

	// deferredValidationRunnerLogName is the runner's diagnostics file in the
	// deferred-validation directory.
	deferredValidationRunnerLogName = "runner.log"
)

// deferredValidationEndReason is why a batch or runner record ended without a
// verdict. Persisted batch and runner fields keep their string type and take
// these values by conversion.
type deferredValidationEndReason string

const (
	// deferredValidationReasonHalted ends a batch a halt or stop interrupted.
	deferredValidationReasonHalted deferredValidationEndReason = "halted"
	// deferredValidationReasonRunnerDied closes the batch a dead runner left.
	deferredValidationReasonRunnerDied deferredValidationEndReason = "runner_died"
	// deferredValidationReasonRunnerSettled closes the batch of a runner the
	// operator settled.
	deferredValidationReasonRunnerSettled deferredValidationEndReason = "runner_settled_by_operator"
	// deferredValidationReasonUnstoppable ends a batch whose step left
	// processes the runner could not establish had ended.
	deferredValidationReasonUnstoppable deferredValidationEndReason = "descendants_unstoppable"
	// deferredValidationReasonAdmissionSlotError ends a batch whose probe could
	// not use its admission slot files.
	deferredValidationReasonAdmissionSlotError deferredValidationEndReason = "admission_slot_error"
	// deferredValidationReasonOperatorAbort ends a batch an operator aborted
	// (D-181); no trigger retries it at the same HEAD.
	deferredValidationReasonOperatorAbort deferredValidationEndReason = "aborted_by_operator"
)

// deferredValidationRunOutcome and deferredValidationSkipReason are the values
// of RunDeferredValidationResult.Outcome and .Reason, which keep their string
// type.
type (
	deferredValidationRunOutcome string
	deferredValidationSkipReason string
)

const (
	deferredValidationOutcomeRan     deferredValidationRunOutcome = "ran"
	deferredValidationOutcomeSkipped deferredValidationRunOutcome = "skipped"

	deferredValidationSkipRunnerActive deferredValidationSkipReason = "runner_active"
	deferredValidationSkipHalted       deferredValidationSkipReason = "halted"
	deferredValidationSkipCovered      deferredValidationSkipReason = "covered"
	deferredValidationSkipFailedAtHead deferredValidationSkipReason = "failed_at_head"
	deferredValidationSkipAborted      deferredValidationSkipReason = "aborted_at_head"
)

// errDeferredValidationHalted marks a start refused by a halt or stop.
var errDeferredValidationHalted = errors.New("deferred validation halted")

// errDeferredValidationUnstoppable stops every later start once a step's
// descendants could not be shown to have ended.
var errDeferredValidationUnstoppable = errors.New("descendants of a batch step outlived it and could not be stopped")

// errDeferredValidationBatchEnded refuses a lane's start once another lane
// ended the batch; it is a halt like a committed one.
var errDeferredValidationBatchEnded = fmt.Errorf("%w: another lane ended the batch", errDeferredValidationHalted)

// deferredValidationPlatformSupported reports whether the runner can
// establish that a step's descendants ended. Windows cannot: its process
// tree is unreachable by a possibly reused PID once the root exited.
func deferredValidationPlatformSupported() bool { return runtime.GOOS != "windows" }

// deferredValidationHooks are the platform facilities a runner uses; tests
// substitute them per invocation.
type deferredValidationHooks struct {
	reap              func(pid int, wait time.Duration) bool
	platformSupported bool
	// log receives the runner's diagnostics; nil means stderr, which is
	// runner.log when spawned.
	log io.Writer
}

// deferredValidationHaltPollInterval is how often a lane waiting for its
// admission slots re-reads the mode; tests shorten it.
var deferredValidationHaltPollInterval = 10 * time.Second

// deferredValidationHaltObserved runs once a waiting lane's halt check has
// ended the batch; tests use it to interleave a release and resume.
var deferredValidationHaltObserved = func() {}

// deferredValidationLockFollowsChildren reports whether a child started with
// the runner lock's descriptor keeps the lock held (Unix). Where it does not
// (Windows), a dead runner's live child would lose the fence, so predecessor
// recovery requires operator settlement. A variable so tests can force either.
var deferredValidationLockFollowsChildren = func(held *filelock.Held) bool { return held.File() != nil }

// startDeferredValidationProcess spawns the detached runner. A variable so
// tests can observe spawning without starting a process.
var startDeferredValidationProcess = spawnDeferredValidationRunner
