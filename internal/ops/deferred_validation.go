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
	IntegrationProgressWaitingDeferredValidation = string(integrationProgressWaitingDeferred)

	// The deferred-validation members of the integration progress code set
	// (integrationProgressCode); the gate converts them where it assigns
	// IntegrationProgressReason.Code.
	integrationProgressWaitingDeferred          integrationProgressCode = "deferred_validation_pending"
	integrationProgressBlockedDeferredFailed    integrationProgressCode = "deferred_validation_failed"
	integrationProgressBlockedDeferredInfra     integrationProgressCode = "deferred_validation_infra"
	integrationProgressBlockedDeferredUnsettled integrationProgressCode = "deferred_validation_unsettled"
	integrationProgressBlockedDeferredAborted   integrationProgressCode = "deferred_validation_aborted"

	// deferredValidationRunnerLockName is the runner's OS lock file in the
	// deferred-validation directory, and the labels its holders record.
	deferredValidationRunnerLockName  = "runner"
	lockLabelDeferredValidationRun    = "deferred-validation-run"
	lockLabelDeferredValidationProbe  = "deferred-validation-probe"
	lockLabelDeferredValidationSettle = "deferred-validation-settle"
	lockLabelDeferredValidationAbort  = "deferred-validation-abort"
	// deferredValidationRunnerLogName is the runner's diagnostics file in the
	// deferred-validation directory.
	deferredValidationRunnerLogName = "runner.log"
	// deferredValidationDirMode and deferredValidationLogFileMode are the
	// permissions of the deferred-validation directory and the runner log.
	deferredValidationDirMode     = 0o755
	deferredValidationLogFileMode = 0o644
	// The lane worktrees under the deferred-validation directory: lane 0 keeps
	// the plain name, later lanes number it, and the glob finds the numbered
	// leftovers of a larger earlier batch.
	deferredLaneWorktreeName   = "worktree"
	deferredLaneWorktreeFormat = "worktree-%d"
	deferredLaneWorktreeGlob   = "worktree-*"
	// A scope or inert-path entry opening with deferredPathspecMagicPrefix
	// carries its own pathspec magic and is refused; an inert path is excluded
	// with deferredPathspecExcludePrefix.
	deferredPathspecMagicPrefix   = ":"
	deferredPathspecExcludePrefix = ":(exclude)"
	// deferredAlertOnceKeyFormat dedupes the alert of one culprit of one batch,
	// and deferredDiscoveryIDFormat names the discovery recorded for the
	// batch's n-th culprit.
	deferredAlertOnceKeyFormat = "deferred-validation:%d:%s"
	deferredDiscoveryIDFormat  = "deferred-validation-%d-%d"
	// deferredGitExitNegative is the exit status a quiet Git query returns for
	// a no answer: not an ancestor, or differences exist.
	deferredGitExitNegative = 1
	// deferredValidationStatusBatches is how many of the latest batches the
	// status report carries.
	deferredValidationStatusBatches = 5

	// The deferred-validation subcommands and flag that guidance text and the
	// runner spawn name through the branded command line.
	deferredValidationSubcommandStatus = "status"
	deferredValidationSubcommandRun    = "run"
	deferredValidationFlagForce        = "--force"

	// The keys of the Extra map of a discovery a failed batch records for a
	// culprit; deferredFixInFlight and deferredDiscoveryExists read the owner
	// and command keys back.
	deferredDiscoveryKeyBatch         = "batch"
	deferredDiscoveryKeyBatchCommit   = "batch_commit"
	deferredDiscoveryKeyCommandSHA256 = "command_sha256"
	deferredDiscoveryKeyCulpritTask   = "culprit_task"
	deferredDiscoveryKeyMergeCommit   = "merge_commit"
)

// deferredValidationUnattributedReason is why a culprit carries no task: the
// persisted DeferredValidationCulprit.Reason keeps its string type and takes
// these values by conversion. A setup that cannot be prepared at a commit
// reasons with deferredValidationReasonSetupUnavailableFormat instead.
type deferredValidationUnattributedReason string

const (
	deferredValidationUnattributedNoBaseline          deferredValidationUnattributedReason = "no_baseline"
	deferredValidationUnattributedBaselineNotAncestor deferredValidationUnattributedReason = "baseline_not_in_ancestry"
	deferredValidationUnattributedHistoryUnavailable  deferredValidationUnattributedReason = "history_unavailable"
	deferredValidationUnattributedFailsAtBaseline     deferredValidationUnattributedReason = "fails_at_baseline"
	deferredValidationUnattributedFailsAfterLastMerge deferredValidationUnattributedReason = "fails_after_last_merge"

	deferredValidationReasonSetupUnavailableFormat = "setup_unavailable at %s"
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

	// The reasons deferredValidationWanted gives when no batch is due.
	deferredValidationSkipNothingDeferred   deferredValidationSkipReason = "nothing_deferred"
	deferredValidationSkipAttemptsExhausted deferredValidationSkipReason = "attempts_exhausted"
	deferredValidationSkipNotDue            deferredValidationSkipReason = "not_due"
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
