package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/projectdetect"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// maxIntegrationDiagnosticOutputExcerptBytes bounds test output persisted into
// task history. Full output remains transient on IntegrationFailedError.
const maxIntegrationDiagnosticOutputExcerptBytes = 4096

const integrationTestCommand = "scripts/integration-test.sh"

// DefaultIntegrationTestTimeout bounds how long integration tests may run
// before the merge pipeline kills them. A hanging test without this timeout
// would block the entire merge queue indefinitely.
//
// Exported var (not const) so tests can override. Not parallel-safe:
// tests that override this must not run concurrently with other tests
// that call MergeWorktree. Current wt_merge tests are inherently serial
// (each creates a temp git repo), so this is safe in practice.
var DefaultIntegrationTestTimeout = 10 * time.Minute

// statIntegrationScript is a seam for the branch that distinguishes a missing
// integration script from a stat that failed for another reason. A fixture can
// produce that on POSIX by putting a regular file where scripts/ belongs
// (ENOTDIR), but Windows reports the same layout as "path not found", which
// os.IsNotExist accepts, and an unprivileged process cannot deny itself access
// to a path it owns. Same seam as statWorktreePath in submit_verdict.go.
var statIntegrationScript = os.Stat

var mergeFinalStateTestHook func()

// mergePreparedTestHook is a test-only hook invoked right after a merge's
// preparation commits, before any integration ref effect.
var mergePreparedTestHook func(taskID string)

// SetMergePreparedTestHookForTest installs mergePreparedTestHook for tests in
// other packages and returns a restore function. Not for concurrent use.
func SetMergePreparedTestHookForTest(hook func(taskID string)) func() {
	previous := mergePreparedTestHook
	mergePreparedTestHook = hook
	return func() { mergePreparedTestHook = previous }
}

// Integration failure reason constants.
const (
	IntegrationReasonHEADMismatch           = "worktree HEAD mismatch"
	IntegrationReasonMergeConflict          = "merge conflict"
	IntegrationReasonReviewBoundaryMismatch = "review boundary mismatch"
	IntegrationReasonTestsFailed            = "integration tests failed"
	IntegrationReasonStateInvalid           = "post-merge state validation failed"
)

const (
	integrationOperationWTMerge = "wt-merge"
)

// IntegrationFailedError indicates the merge or integration tests failed.
// State has been updated to INTEGRATION_FAILED appropriately.
type IntegrationFailedError struct {
	Reason        string
	TestOutput    string // non-empty when failure is from integration tests
	RollbackError error  // non-nil if rollback (ResetHard) also failed — integration branch may contain failing code
	Cause         error  // non-nil when the integration failure wraps an underlying cause
}

func (e *IntegrationFailedError) Error() string {
	msg := fmt.Sprintf("integration failed: %s", e.Reason)
	if e.Cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, e.Cause)
	}
	if e.RollbackError != nil {
		return fmt.Sprintf("%s (rollback also failed: %v)", msg, e.RollbackError)
	}
	return msg
}

func (e *IntegrationFailedError) Unwrap() error {
	return e.Cause
}

// MergeResult contains the outcome of a successful worktree merge.
type MergeResult struct {
	models.LifecycleOutcome
	TaskID            string   `json:"task_id"`
	MergeCommit       string   `json:"merge_commit"`
	FastForward       bool     `json:"fast_forward"`
	TestsRan          bool     `json:"tests_ran"`
	NoTestScriptFound bool     `json:"no_test_script_found"` // true when integration test script was missing (distinguishes from "tests ran")
	TestOutput        string   `json:"test_output"`          // captured stdout+stderr from integration tests (if any)
	Warnings          []string `json:"warnings"`             // non-fatal warnings from cleanup/metrics
}

// appendUniqueAgentID adds an agent ID to failed_by if not already present
func appendUniqueAgentID(failedBy []string, agentID string) []string {
	if slices.Contains(failedBy, agentID) {
		return failedBy
	}
	return append(failedBy, agentID)
}

// pipelineTransition applies a status transition using the pipeline transition map.
func pipelineTransition(t *models.Task, to models.TaskStatus, pb *pipelineBundle) error {
	return t.TransitionWith(to, pb.transitions)
}

// markIntegrationFailed transitions a task to INTEGRATION_FAILED under lock.
// Re-validates the task is still in an approved state to prevent concurrent transitions.
// If mergeCommit is non-empty, it's recorded on both the task and the history entry.
func markIntegrationFailed(bb *db.Blackboard, taskID, agentID, reason, mergeCommit string, pb *pipelineBundle) error {
	return markIntegrationFailedWithDiagnostic(
		bb,
		taskID,
		agentID,
		reason,
		mergeCommit,
		pb,
		integrationFailureDiagnostic(reason, mergeCommit, "", nil),
	)
}

func markIntegrationFailedWithAuthority(bb *db.Blackboard, taskID string, authority models.AgentAuthority, reason, mergeCommit string, pb *pipelineBundle) error {
	return markIntegrationFailedWithDiagnosticAuthority(
		bb,
		taskID,
		authority.ID,
		&authority,
		reason,
		mergeCommit,
		pb,
		integrationFailureDiagnostic(reason, mergeCommit, "", nil),
	)
}

func markIntegrationFailedWithDiagnostic(
	bb *db.Blackboard,
	taskID, agentID, reason, mergeCommit string,
	pb *pipelineBundle,
	diagnostic map[string]any,
) error {
	return markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, nil, reason, mergeCommit, pb, diagnostic)
}

func markIntegrationFailedWithDiagnosticAuthority(
	bb *db.Blackboard,
	taskID, agentID string,
	authority *models.AgentAuthority,
	reason, mergeCommit string,
	pb *pipelineBundle,
	diagnostic map[string]any,
	request ...LifecycleRequest,
) error {
	var pr models.PipelineResolver
	if pb != nil {
		pr = pb.pr
	}
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !models.IsApprovedForMerge(t, pr) {
			return fmt.Errorf("task %s status changed concurrently (now %s)", taskID, t.Status)
		}
		if len(request) > 0 {
			if err := ValidateLifecyclePreparation(t, request[0]); err != nil {
				return err
			}
		}
		if err := pipelineTransition(t, models.TaskStatusIntegrationFailed, pb); err != nil {
			return err
		}
		models.AdvanceLifecycle(t)
		t.FailedBy = appendUniqueAgentID(t.FailedBy, agentID)
		now := time.Now().UTC()
		entry := models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventIntegrationFailed,
			Agent:  &agentID,
			Reason: &reason,
		}
		if mergeCommit != "" {
			t.MergeCommit = &mergeCommit
			entry.Commit = &mergeCommit
		}
		if len(diagnostic) > 0 {
			t.IntegrationFailure = cloneMapForTaskDiagnostic(diagnostic)
			entry.Extra = map[string]any{
				"diagnostic": cloneMapForTaskDiagnostic(diagnostic),
			}
		}
		t.History = append(t.History, entry)
		blocked, err := blockTaskForHypothesisExhaustion(s, t, agentID, pb.transitions, now)
		if err != nil {
			return err
		}

		if !blocked {
			// Refresh lease — task stays assigned to original coder for conflict resolution.
			renewLease(s, t)
			if t.AssignedTo != nil {
				if agent, ok := s.Agents[*t.AssignedTo]; ok {
					agent.LeaseExpires = t.LeaseExpires
					s.Agents[*t.AssignedTo] = agent
				}
			}
		}
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: time.Now().UTC(),
			Agent:     agentID,
			Trigger:   models.HandoffTriggerSubmission,
			Failed:    []string{reason},
			NextStep:  integrationFailureRecoveryHint(reason),
		})

		return nil
	})
}

func integrationFailureDiagnostic(reason, mergeCommit, testOutput string, rollbackErr error) map[string]any {
	diagnostic := map[string]any{
		"operation":     integrationOperationWTMerge,
		"reason":        reason,
		"recovery_hint": integrationFailureRecoveryHint(reason),
	}
	if mergeCommit != "" {
		diagnostic["merge_commit"] = mergeCommit
	}
	if testOutput != "" {
		outputExcerpt, truncated := persistedOutputExcerpt(secretmask.New().MaskText(testOutput), maxIntegrationDiagnosticOutputExcerptBytes)
		diagnostic["test_output_excerpt"] = outputExcerpt
		diagnostic["output_truncated"] = truncated
		// Raw byte count is pre-redaction; it describes the transient command output size.
		diagnostic["output_bytes"] = len([]byte(testOutput))
		diagnostic["failing_command"] = integrationTestCommand
	}
	if rollbackErr != nil {
		diagnostic["rollback_error"] = rollbackErr.Error()
	}
	return diagnostic
}

func cloneMapForTaskDiagnostic(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func integrationFailureDiagnosticWithDetail(reason, detail, mergeCommit, testOutput string, rollbackErr error) map[string]any {
	diagnostic := integrationFailureDiagnostic(reason, mergeCommit, testOutput, rollbackErr)
	if detail != "" {
		diagnostic["detail"] = detail
	}
	return diagnostic
}

func persistedOutputExcerpt(output string, maxBytes int) (string, bool) {
	if maxBytes <= 0 || len([]byte(output)) <= maxBytes {
		return output, false
	}

	excerpt := []byte(output)
	excerpt = excerpt[:maxBytes]
	for !utf8.Valid(excerpt) && len(excerpt) > 0 {
		excerpt = excerpt[:len(excerpt)-1]
	}
	return string(excerpt), true
}

func integrationFailureRecoveryHint(reason string) string {
	switch reason {
	case IntegrationReasonHEADMismatch:
		return "verify the task worktree HEAD matches review_commit before retrying integration"
	case IntegrationReasonMergeConflict:
		return "resolve the integration conflict in the task worktree, resubmit, and retry merge"
	case IntegrationReasonTestsFailed:
		return "inspect the integration test output, fix the task branch, resubmit, and retry merge"
	case IntegrationReasonStateInvalid:
		return "restore or preserve referenced artifacts, resubmit, and retry merge"
	default:
		return "inspect the integration failure details before retrying merge"
	}
}

// interruptedMergePreparation reports whether the refusal in the caller's way is
// an unresolved wt-merge preparation at this exact boundary, left by any actor.
// mergeWorktreeLifecycle is the only creator of wt-merge preparations and holds
// the task review lock for its whole call, so a preparation seen under that
// lock belongs to an invocation that has ended. Its Git effect is settled from
// Git and receipts, never from the preparer's identity or liveness: a merge
// owner handed over from a departed approver must be able to finish it.
func interruptedMergePreparation(task *models.Task, request LifecycleRequest) bool {
	if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
		return false
	}
	p := task.Lifecycle.Preparation
	return p.Operation == integrationOperationWTMerge && preparationStillCurrent(task, request, nil)
}

// retireProvenMergePreparation clears an interrupted wt-merge preparation whose
// external effect is settled — proven landed, or proven absent — so the merge
// can be finished or retried instead of requerying forever. The fence it lifts
// exists because an unresolved preparation means uncertain effects; the checks
// in resumeInterruptedMerge remove that uncertainty. Exactly-once does not rest
// on this fence — the locked approved-status recheck before publication refuses
// any duplicate.
func retireProvenMergePreparation(bb *db.Blackboard, taskID string, authority *models.AgentAuthority, prepared models.LifecyclePreparation, request LifecycleRequest) error {
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		// Retirement belongs to a retry, which is new work: a halt committed
		// after the retry's first read leaves the preparation for resume.
		if err := RequireWorkAdmitted(state, integrationOperationWTMerge); err != nil {
			return err
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !interruptedMergePreparation(task, request) ||
			!sameLifecycleRequest(task.Lifecycle.Preparation.LifecycleIdentity, prepared.LifecycleIdentity) ||
			task.Lifecycle.Preparation.Boundary != prepared.Boundary {
			return fmt.Errorf("interrupted merge preparation changed before it could be retired")
		}
		models.AdvanceLifecycle(task)
		return nil
	})
}

// resumeInterruptedMerge admits a retry that CheckLifecycleRequest refused,
// when the refusal is an interrupted wt-merge whose integration effect is
// settled: a receipt proves the merge landed, or integration does not carry the
// approved commit, so no effect exists to duplicate. When integration carries
// the approved commit without an attributable receipt the effect is unknown; it
// stays fenced for inspection. It reports false, with no state change, for
// every other refusal.
func resumeInterruptedMerge(bb *db.Blackboard, projectRoot string, state *models.State, task *models.Task, request LifecycleRequest, authority *models.AgentAuthority) (bool, error) {
	if !interruptedMergePreparation(task, request) || task.ReviewCommit == nil {
		return false, nil
	}
	gitWrapper := git.New(projectRoot)
	expectedCommit, err := gitWrapper.GetCommitSHA(*task.ReviewCommit)
	if err != nil {
		return false, nil // An unresolvable review commit is not proof of anything.
	}
	integrationBranch := state.Config.IntegrationBranch
	if integrationBranch == "" {
		integrationBranch = "main"
	}
	integrationRef := integrationBranchRefPrefix + integrationBranch
	proven, err := provenMergeEffect(state, gitWrapper, integrationRef, task.ID, expectedCommit)
	if err != nil {
		return false, err
	}
	preparer := task.Lifecycle.Preparation.Actor
	if proven == nil {
		head, err := gitWrapper.GetCommitSHA(integrationRef)
		if err != nil {
			return false, fmt.Errorf("failed to get integration HEAD: %w", err)
		}
		carried, err := gitWrapper.IsAncestor(expectedCommit, head)
		if err != nil {
			return false, fmt.Errorf("failed to check approved commit reachability: %w", err)
		}
		if carried {
			return false, fmt.Errorf("wt-merge preparation by %s left approved commit %s on integration without an attributable receipt; inspect before recovery", preparer, shortSHA(expectedCommit))
		}
		log.Printf("wt-merge %s: retiring orphaned merge preparation by %s — integration does not carry %s", task.ID, preparer, shortSHA(expectedCommit))
		if err := retireProvenMergePreparation(bb, task.ID, authority, *task.Lifecycle.Preparation, request); err != nil {
			return false, err
		}
		return true, nil
	}
	log.Printf("wt-merge %s: resuming interrupted merge by %s — integration already carries %s", task.ID, preparer, shortSHA(proven.afterCommit))
	if err := retireProvenMergePreparation(bb, task.ID, authority, *task.Lifecycle.Preparation, request); err != nil {
		return false, err
	}
	return true, nil
}

// shortSHAWidth is how many leading characters of a SHA log and diagnostic
// messages show.
const shortSHAWidth = 7

// shortSHA truncates a SHA to shortSHAWidth characters for log messages.
func shortSHA(s string) string {
	if len(s) > shortSHAWidth {
		return s[:shortSHAWidth]
	}
	return s
}

// startIndexRefresh launches the post-merge repo-root index refresh. Tests
// replace it to observe launches without starting a coordinator.
var startIndexRefresh = pairingindex.StartRefresh

// MergeWorktree merges an approved task into the integration branch.
// This is the final step in the task lifecycle, integrating completed work.
// Returns IntegrationFailedError if merge conflicts or integration tests fail.
//
// No terminal I/O — integration test output is captured and returned in the result or error.
func MergeWorktree(projectRoot, taskID, agentID string, mergeExtra ...map[string]any) (*MergeResult, error) {
	return mergeWorktreeLifecycle(projectRoot, taskID, agentID, nil, LifecycleRequestOptions{}, mergeExtra...)
}

// MergeWorktreeWithAuthority is the authenticated command entry point. Every
// state write in the merge, rollback, failure, and finalization paths checks
// the caller-held generation in its own transaction.
func MergeWorktreeWithAuthority(projectRoot, taskID string, authority models.AgentAuthority, mergeExtra ...map[string]any) (*MergeResult, error) {
	return MergeWorktreeWithAuthorityAndOptions(projectRoot, taskID, authority, LifecycleRequestOptions{}, mergeExtra...)
}

func MergeWorktreeWithAuthorityAndOptions(projectRoot, taskID string, authority models.AgentAuthority, opts LifecycleRequestOptions, mergeExtra ...map[string]any) (*MergeResult, error) {
	return mergeWorktreeLifecycle(projectRoot, taskID, authority.ID, &authority, opts, mergeExtra...)
}

func mergeWorktreeLifecycle(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, mergeExtra ...map[string]any) (result *MergeResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	defer func() {
		retErr = WrapLifecycleError(integrationOperationWTMerge, nil, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(integrationOperationWTMerge, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	retErr = WithProjectLifecycleSharedLock(projectRoot, integrationOperationWTMerge, func() error {
		return withTaskReviewLock(projectRoot, taskID, integrationOperationWTMerge, func() error {
			var err error
			result, err = mergeWorktree(projectRoot, taskID, agentID, authority, opts, mergeExtra...)
			return err
		})
	})
	return result, retErr
}

func mergeWorktree(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, mergeExtra ...map[string]any) (result *MergeResult, retErr error) {
	// Setup paths
	statePath := paths.New(projectRoot).StatePath()

	// Read state
	bb := RequestBlackboard(statePath, authority, opts)
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
	}
	// Fast refusal before interrupted-preparation recovery, so a refused merge
	// changes nothing; the preparation write below is the authoritative check.
	if err := RequireWorkAdmitted(state, integrationOperationWTMerge); err != nil {
		return nil, err
	}
	request, err := NewLifecycleRequest(integrationOperationWTMerge, task, agentID, authority, opts, mergeExtra)
	if err != nil {
		return nil, err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		// A caller that pinned an expected transition asked to act on the exact
		// boundary it inspected. Resuming retires the preparation and advances
		// that boundary, so such a caller requeries instead — and must see no
		// state change from having asked.
		if opts.ExpectedTransition != "" {
			return nil, err
		}
		resumed, resumeErr := resumeInterruptedMerge(bb, projectRoot, state, task, request, authority)
		if resumeErr != nil {
			return nil, errors.Join(err, resumeErr)
		}
		if !resumed {
			return nil, err
		}
		// Retiring the preparation advanced the task boundary, so the request
		// built against the old one is stale. Rebuild and re-check against the
		// state the merge will actually finish from.
		if state, task, err = readTaskState(bb, taskID); err != nil {
			return nil, err
		}
		if request, err = NewLifecycleRequest(integrationOperationWTMerge, task, agentID, authority, opts, mergeExtra); err != nil {
			return nil, err
		}
		if receipt, err = CheckLifecycleRequest(task, request, state.Agents); err != nil {
			return nil, err
		}
	}
	if receipt != nil {
		return &MergeResult{LifecycleOutcome: LifecycleReplayOutcome(task, receipt, agentID), TaskID: taskID, MergeCommit: receipt.Projection.MergeCommit}, nil
	}
	effects := LifecycleEffectsNone
	defer func() {
		if retErr != nil {
			task = readLifecycleTask(projectRoot, taskID, authority, opts)
			outcome, action := models.LifecycleStateChanged, LifecycleSafeActionRequery
			var invalid *PreconditionError
			if effects == LifecycleEffectsNone && errors.As(retErr, &invalid) {
				outcome, action = models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput
			}
			retErr = WrapLifecycleError(integrationOperationWTMerge, task, retErr, outcome, action, effects)
		}
	}()

	// Load pipeline bundle once for all pipeline-aware checks
	pb, pbErr := loadPipelineBundle(projectRoot)
	if pbErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", pbErr)
	}
	pr := pb.pr

	if !models.IsApprovedForMerge(task, pr) {
		return nil, WrapLifecycleError(integrationOperationWTMerge, task, &PreconditionError{Reason: fmt.Sprintf("task must be in an approved state to merge (current status: %s)", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}

	if task.ReviewCommit == nil {
		return nil, &PreconditionError{Reason: "task has no review_commit"}
	}
	if err := requireReconciledVerdicts(state, task, *task.ReviewCommit); err != nil {
		return nil, err
	}

	// Initialize git wrapper
	gitWrapper := git.New(projectRoot)

	// Normalize review_commit to full SHA.
	reviewCommit := *task.ReviewCommit
	expectedCommit, err := gitWrapper.GetCommitSHA(reviewCommit)
	if err != nil {
		return nil, fmt.Errorf("review_commit (%s) not found in repository: %w", reviewCommit, err)
	}

	// Worktree-present path: verify HEAD matches review_commit to detect tampering.
	// Worktree-absent path (e.g. cleared by task recovery after Ctrl-C): skip HEAD
	// verification — the approved commit is still in git and safe to merge.
	if task.Worktree != nil {
		wtHEAD, err := gitWrapper.GetWorktreeHEAD(taskID)
		if err != nil {
			return nil, fmt.Errorf("failed to get worktree HEAD: %w", err)
		}

		if wtHEAD != expectedCommit {
			// HEAD mismatch indicates state corruption — stops retry loops, preserves worktree
			detail := fmt.Sprintf("worktree HEAD (%s) does not match approved commit (%s)", shortSHA(wtHEAD), shortSHA(expectedCommit))
			diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonHEADMismatch, detail, "", "", nil)
			if err := markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, authority, IntegrationReasonHEADMismatch, "", pb, diagnostic); err != nil {
				return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", err)
			}
			effects = LifecycleEffectsCommitted

			return nil, &IntegrationFailedError{Reason: IntegrationReasonHEADMismatch}
		}
	} else {
		log.Printf("wt-merge %s: WARNING — worktree missing (cleared by recovery?), proceeding with review_commit %s", taskID, shortSHA(expectedCommit))
	}

	// Get integration branch
	integrationBranch := state.Config.IntegrationBranch
	if integrationBranch == "" {
		integrationBranch = "main"
	}

	integrationRef := integrationBranchRefPrefix + integrationBranch
	// Persist the request before touching the integration ref. A concurrent or
	// restarted invocation must requery while this preparation is unresolved.
	var preparation models.LifecyclePreparation
	err = modifyLifecycleState(bb, authority, func(s *models.State) error {
		// Admission point: a preparation committed before a halt may finish;
		// none is started after it.
		if err := RequireWorkAdmitted(s, integrationOperationWTMerge); err != nil {
			return err
		}
		live := s.FindTask(taskID)
		if live == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !models.IsApprovedForMerge(live, pr) || live.ReviewCommit == nil || *live.ReviewCommit != reviewCommit {
			return WrapLifecycleError(integrationOperationWTMerge, live, fmt.Errorf("approved review boundary changed"), models.LifecycleStateChanged, "requery", "none")
		}
		if err := PrepareLifecycleRequest(live, request, state.Agents); err != nil {
			return err
		}
		preparation = *live.Lifecycle.Preparation
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mergePreparedTestHook != nil {
		mergePreparedTestHook(taskID)
	}
	effects = LifecycleEffectsUnknown
	casStarted := false
	forwardRolledBack := false
	forwardUnpublished := false
	defer func() {
		// Once CAS starts, a returned error can leave the ref or main checkout
		// partially updated. Preserve that fence for inspected recovery unless
		// this invocation rewound the ref it moved, or provably moved nothing:
		// otherwise its own same-generation retries requery until a restart
		// (D77). Retirement usually follows a state-lock timeout, so it waits
		// the patient budget.
		if retErr != nil && (!casStarted || forwardRolledBack || forwardUnpublished) {
			retErr = retireFailedLifecyclePreparation(bb.Patient(), taskID, authority, &preparation, retErr, effects)
		}
	}()

	artifactGuardHook := buildArtifactGuardHook(bb, projectRoot, gitWrapper, taskID)
	var outcome *casMergeOutcome
	var forwardMutation *integrationRefMutation
	err = withEffectiveIntegrationCompletionLinearization(projectRoot, integrationLockForward+taskID, func() error {
		mutationErr := withIntegrationMutationLock(projectRoot, integrationLockForward+taskID, func() error {
			var mergeErr error
			casStarted = true
			outcome, mergeErr = performCASMerge(gitWrapper, integrationRef, expectedCommit, taskID, artifactGuardHook)
			if mergeErr != nil || outcome.conflict {
				return mergeErr
			}
			if outcome.preMergeHEAD != outcome.mergeCommit {
				forwardMutation = &integrationRefMutation{
					taskID:       taskID,
					beforeCommit: outcome.preMergeHEAD,
					afterCommit:  outcome.mergeCommit,
				}
			}
			if syncErr := gitWrapper.SyncMergedFiles(outcome.preMergeHEAD, outcome.mergeCommit); syncErr != nil {
				return fmt.Errorf("failed to sync working tree after merge: %w", syncErr)
			}
			return nil
		})
		if receiptErr := persistIntegrationMutationReceipt(bb, forwardMutation, authority); receiptErr != nil {
			receiptErr = fmt.Errorf("failed to persist integration mutation receipt: %w", receiptErr)
			rollbackMutation, rollbackErr := rollbackMergedCommit(
				projectRoot,
				gitWrapper,
				integrationRef,
				outcome.preMergeHEAD,
				outcome.mergeCommit,
				"HEAD",
				taskID,
			)
			if rollbackErr == nil && rollbackMutation == nil {
				rollbackErr = fmt.Errorf("failed to roll back integration ref after receipt persistence failure: ref changed from %s", shortSHA(outcome.mergeCommit))
			}
			forwardRolledBack = rollbackErr == nil && rollbackMutation != nil
			return errors.Join(mutationErr, receiptErr, rollbackErr)
		}
		return mutationErr
	})
	if err != nil {
		// Two failures prove nothing was published: the forward lock timed out
		// before its callback ran, or the guard, which runs before update-ref,
		// could not read state. Neither says anything about the candidate, so
		// the task stays approved (D78). Retiring the preparation advances the
		// boundary, hence requery; only lock contention is RETRYABLE.
		var unavailable *candidateArtifactGuardUnavailableError
		lockTimeout := filelock.IsLockErrorType(err, filelock.LockErrorTimeout)
		if errors.As(err, &unavailable) || (!casStarted && lockTimeout) {
			forwardUnpublished = true
			effects = LifecycleEffectsNone
			result := models.LifecycleStateChanged
			if lockTimeout {
				result = models.LifecycleRetryable
			}
			return nil, &LifecycleError{Outcome: NewLifecycleOutcome(integrationOperationWTMerge, nil, result, "requery", effects), Err: err}
		}
		var artifactErr *candidateArtifactGuardError
		if errors.As(err, &artifactErr) {
			diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonStateInvalid, err.Error(), "", "", nil)
			if updateErr := markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, authority, IntegrationReasonStateInvalid, "", pb, diagnostic, request); updateErr != nil {
				return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", updateErr)
			}
			return nil, &IntegrationFailedError{Reason: IntegrationReasonStateInvalid, Cause: err}
		}
		return nil, err
	}
	if outcome.conflict {
		if updateErr := markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, authority, IntegrationReasonMergeConflict, "", pb, integrationFailureDiagnostic(IntegrationReasonMergeConflict, "", "", nil), request); updateErr != nil {
			return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", updateErr)
		}
		return nil, &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
	}

	mergeCommit := outcome.mergeCommit
	preMergeHEAD := outcome.preMergeHEAD
	fastForward := outcome.fastForward

	// CAS found the approved commit already merged, so outcome.mergeCommit is
	// whatever the integration ref points at now — which is another task's merge
	// commit if one landed since. Where a receipt records what this task
	// published, that is the commit to attribute to it.
	// rollbackBaseline is the commit a failed validation must rewind the
	// integration ref to. On the forward path that is the HEAD this invocation
	// merged onto; on a resume the ref already carries the merge, so only the
	// recorded receipt knows what preceded it.
	rollbackBaseline := preMergeHEAD
	if outcome.preMergeHEAD == outcome.mergeCommit {
		recordedState, readErr := bb.Read()
		if readErr != nil {
			return nil, fmt.Errorf("failed to read state for merge attribution: %w", readErr)
		}
		recorded, provenErr := provenMergeEffect(recordedState, gitWrapper, integrationRef, taskID, expectedCommit)
		if provenErr != nil {
			return nil, provenErr
		}
		if recorded != nil {
			mergeCommit = recorded.afterCommit
			rollbackBaseline = recorded.beforeCommit
		}
	}

	// Detect current branch early — needed for working tree restore on both
	// success and rollback paths.
	var warnings []string
	currentBranch, branchErr := gitWrapper.GetCurrentBranch()
	if branchErr != nil {
		warnings = append(warnings, fmt.Sprintf("skipped working tree restore (branch detection failed: %v)", branchErr))
	}
	rollbackRestoreRef := ""
	if branchErr == nil && currentBranch != integrationBranch {
		rollbackRestoreRef = "HEAD"
	}

	if artifactGuardPostUpdateTestHook != nil {
		if err := artifactGuardPostUpdateTestHook(); err != nil {
			return nil, fmt.Errorf("artifact guard post-update test hook failed: %w", err)
		}
	}

	currentState, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state for post-merge artifact validation: %w", err)
	}
	if err := statevalidate.ValidateMergeArtifactRefs(currentState, projectRoot, taskID); err != nil {
		rewound, rollbackErr := rollbackMergedCommitAndPersist(bb, projectRoot, gitWrapper, integrationRef, rollbackBaseline, mergeCommit, rollbackRestoreRef, taskID, authority)
		detail := err.Error()
		if retained := integrationRetentionDetail(rewound, rollbackErr, mergeCommit); retained != "" {
			detail = detail + "; " + retained
		}
		diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonStateInvalid, detail, mergeCommit, "", rollbackErr)
		if updateErr := markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, authority, IntegrationReasonStateInvalid, mergeCommit, pb, diagnostic, request); updateErr != nil {
			return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", updateErr)
		}
		return nil, &IntegrationFailedError{
			Reason:        IntegrationReasonStateInvalid,
			RollbackError: rollbackErr,
		}
	}

	// Run integration tests if they exist
	var testsRan bool
	var noTestScriptFound bool
	var testOutput string
	integrationTestScript := filepath.Join(projectRoot, integrationTestCommand)
	if _, statErr := statIntegrationScript(integrationTestScript); statErr == nil {
		testsRan = true
		var combinedOutput bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), DefaultIntegrationTestTimeout)
		defer cancel()
		// Hand the script to a shell rather than exec it directly: it is a POSIX
		// script, and Windows cannot fork/exec one — the run failed there with no
		// output at all, so a project's integration tests never ran. The command
		// stays repo-relative and the shell resolves it against cmd.Dir, which
		// keeps a native path out of a shell string.
		cmd, runErr := shellCommandContext(ctx, integrationTestCommand, projectRoot)
		if runErr == nil {
			cmd.Stdout = &combinedOutput
			cmd.Stderr = &combinedOutput
			// Kill the entire process tree on timeout on both Unix and Windows.
			// WaitDelay ensures cmd.Wait returns even if child processes hold
			// pipes open after kill.
			configProcessGroupKill(cmd)
			cmd.WaitDelay = 5 * time.Second
			runErr = cmd.Run()
		} else {
			combinedOutput.WriteString(runErr.Error())
		}

		if runErr != nil {
			testOutput = combinedOutput.String()
			if ctx.Err() == context.DeadlineExceeded {
				testOutput += fmt.Sprintf("\n[%s] integration test killed after %s timeout", brand.BinaryName, DefaultIntegrationTestTimeout)
			}

			// CAS rollback: only rewind if ref still points to our merge commit.
			// If someone else merged on top, rewinding would drop their work.
			rewound, rollbackErr := rollbackMergedCommitAndPersist(bb, projectRoot, gitWrapper, integrationRef, rollbackBaseline, mergeCommit, rollbackRestoreRef, taskID, authority)

			diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonTestsFailed,
				integrationRetentionDetail(rewound, rollbackErr, mergeCommit), mergeCommit, testOutput, rollbackErr)
			if updateErr := markIntegrationFailedWithDiagnosticAuthority(bb, taskID, agentID, authority, IntegrationReasonTestsFailed, mergeCommit, pb, diagnostic, request); updateErr != nil {
				return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", updateErr)
			}

			return nil, &IntegrationFailedError{
				Reason:        IntegrationReasonTestsFailed,
				TestOutput:    testOutput,
				RollbackError: rollbackErr,
			}
		}

		testOutput = combinedOutput.String()
	} else if errors.Is(statErr, os.ErrNotExist) {
		// Integration test script not found — log warning for audit trail.
		noTestScriptFound = true
		log.Printf("wt-merge %s: WARNING — integration test script not found at %s, proceeding without tests", taskID, integrationTestScript)
	} else {
		// Distinguish actual stat failures from true missing-script cases.
		log.Printf("wt-merge %s: WARNING — unable to stat integration test script at %s: %v; proceeding without tests", taskID, integrationTestScript, statErr)
	}

	var detectedPostWorktreeCmd string
	var detectedNodeSubdirs []string
	var postWorktreeCmdAmbiguous bool
	if state.Config.PostWorktreeCmd == nil {
		detectedPostWorktreeCmd = projectdetect.DetectPostWorktreeCmd(projectRoot)
		if detectedPostWorktreeCmd == "" {
			detectedNodeSubdirs = projectdetect.DetectNodeSubdirs(projectRoot)
			postWorktreeCmdAmbiguous = len(detectedNodeSubdirs) > 1
		}
	}

	// Restore working tree when checked-out branch differs from integration.
	if branchErr == nil && currentBranch != integrationBranch {
		syncErr := withIntegrationMutationLock(projectRoot, integrationLockRestore+taskID, func() error {
			return gitWrapper.RestoreSyncedFiles(preMergeHEAD, mergeCommit, "HEAD")
		})
		if syncErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to restore working tree: %v", syncErr))
		}
	}

	// Update state to MERGED (before worktree cleanup — if write fails,
	// worktree still exists for investigation; reverse order would lose the worktree
	// while state still says APPROVED)
	var autoConfiguredPostWorktreeCmd bool
	var lifecycleOutcome models.LifecycleOutcome
	if mergeFinalStateTestHook != nil {
		mergeFinalStateTestHook()
	}
	err = modifyLifecycleState(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if err := ValidateLifecyclePreparation(t, request); err != nil {
			return err
		}
		// Re-validate status under lock to prevent concurrent transition
		if !models.IsApprovedForMerge(t, pr) {
			return fmt.Errorf("task %s status changed concurrently (now %s)", taskID, t.Status)
		}
		if err := pipelineTransition(t, models.TaskStatusMerged, pb); err != nil {
			return err
		}
		if s.Config.PostWorktreeCmd == nil && detectedPostWorktreeCmd != "" {
			cmd := detectedPostWorktreeCmd
			s.Config.PostWorktreeCmd = &cmd
			autoConfiguredPostWorktreeCmd = true
		}
		t.Worktree = nil
		t.MergeCommit = &mergeCommit
		t.IntegrationFailure = nil
		// Deferred commands enter the goal ledger here, before post-merge
		// archival can move the receipt out of live state (ADR-0204).
		recordMergedDeferrals(s, t, mergeCommit, time.Now().UTC())

		// Record completion handoff event — audit trail for sprint analysis.
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: time.Now(),
			Agent:     agentID,
			Trigger:   models.HandoffTriggerCompletion,
		})

		// Release the assigned agent — only if still working on this task.
		// After submission the coder's CurrentTask is cleared; if the coder
		// has since claimed another task we must not blow them to IDLE.
		if t.AssignedTo != nil {
			if a, ok := s.Agents[*t.AssignedTo]; ok {
				if a.CurrentTask != nil && *a.CurrentTask == taskID {
					s.ReleaseAgent(*t.AssignedTo)
				}
			}
		}

		// Add history entry with merge-gate extra fields
		extra := map[string]any{"tests_ran": testsRan}
		if len(mergeExtra) > 0 && mergeExtra[0] != nil {
			maps.Copy(extra, mergeExtra[0])
		}
		historyEntry := models.TaskHistoryEntry{
			Time:   time.Now(),
			Event:  models.TaskEventMerged,
			Agent:  &agentID,
			Commit: &mergeCommit,
			Extra:  extra,
		}
		t.History = append(t.History, historyEntry)

		var completeErr error
		lifecycleOutcome, completeErr = CompleteLifecycleRequest(t, request, models.LifecycleProjection{ReviewCommit: expectedCommit, MergeCommit: mergeCommit}, state.Agents)
		return completeErr
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update state to MERGED: %w", err)
	}
	effects = LifecycleEffectsCommitted
	if postWorktreeCmdAmbiguous && !autoConfiguredPostWorktreeCmd {
		warnings = append(warnings, fmt.Sprintf(
			"post_worktree_cmd remains unset: detected Node projects in %s; configure manually",
			strings.Join(detectedNodeSubdirs, ", "),
		))
	}

	// Cleanup: Remove worktree directory and branch (after state commit — safe now).
	// Errors are non-fatal — state is already committed, collect as warnings.
	if err := gitWrapper.RemoveWorktreeDir(taskID); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to remove worktree directory: %v", err))
	}
	taskBranch := paths.TaskBranchPrefix + taskID
	if exists, err := gitWrapper.BranchExists(taskBranch); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check branch %s: %v", taskBranch, err))
	} else if exists {
		if err := gitWrapper.DeleteBranch(taskBranch); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, err))
		}
	}

	// Check if this merged task was the last active successor of a
	// superseded predecessor — if so, clean up the predecessor's branch.
	warnings = append(warnings, cleanupPredecessorBranches(bb, gitWrapper, taskID)...)

	// Update sprint metrics — non-fatal
	if _, err := UpdateSprintMetrics(projectRoot); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to update sprint metrics: %v", err))
	}

	// Report obligation drift this merge introduced — non-fatal, and after the
	// state commit on purpose. A merge is the only way the content an approved
	// obligation rests on can move, and surfacing it must never be able to
	// refuse the merge that surfaced it.
	if drifted, err := RecordObligationContentDrift(bb, projectRoot, mergeCommit, agentID); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to check obligation drift: %v", err))
	} else {
		for _, section := range drifted {
			warnings = append(warnings, fmt.Sprintf(
				"obligation content drifted since approval: %s — recorded for review", section))
		}
	}

	// Refresh repo-root indexes for the merged content — non-fatal. wt-merge
	// moves the integration branch with update-ref, which fires no Git hook.
	if err := startIndexRefresh(projectRoot, "merge"); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to start repo-root index refresh: %v", err))
	}

	// Start a periodic deferred-validation batch when one is due — non-fatal;
	// the next merge or integration reconciliation reconsiders (ADR-0204).
	if _, err := StartDeferredValidationIfDue(projectRoot); err != nil {
		warnings = append(warnings, fmt.Sprintf("failed to start deferred validation: %v", err))
	}

	// Shrink live state by archiving terminal acceptance receipts — non-fatal.
	// Only a newly committed merge gets here (a replay returns earlier), and the
	// write carries the caller's authority so the generation fence still holds.
	if lifecycleOutcome.Outcome == models.LifecycleCompleted {
		if postMergeArchiveTestHook != nil {
			postMergeArchiveTestHook()
		}
		if _, err := archiveTerminalAcceptanceReceipts(projectRoot, authority, DefaultArchiveLimit, opts); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to archive terminal acceptance receipts: %v", err))
		}
		if _, err := archiveTerminalTasks(projectRoot, authority, DefaultArchiveLimit, opts); err != nil {
			warnings = append(warnings, fmt.Sprintf("failed to archive terminal tasks: %v", err))
		}
	}

	return &MergeResult{
		LifecycleOutcome:  lifecycleOutcome,
		TaskID:            taskID,
		MergeCommit:       mergeCommit,
		FastForward:       fastForward,
		TestsRan:          testsRan,
		NoTestScriptFound: noTestScriptFound,
		TestOutput:        testOutput,
		Warnings:          warnings,
	}, nil
}
