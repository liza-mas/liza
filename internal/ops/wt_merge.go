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
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/projectdetect"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// maxMergeRetries is the maximum number of CAS retry attempts for the merge loop.
const maxMergeRetries = 3

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

// mergeCASRetryTestHook is a test-only hook invoked after reading integration HEAD
// in each CAS attempt and before merge/ref-update logic runs.
// Production code leaves this nil.
var mergeCASRetryTestHook func(attempt int, integrationRef, preMergeHEAD string) error

// statIntegrationScript is a seam for the branch that distinguishes a missing
// integration script from a stat that failed for another reason. A fixture can
// produce that on POSIX by putting a regular file where scripts/ belongs
// (ENOTDIR), but Windows reports the same layout as "path not found", which
// os.IsNotExist accepts, and an unprivileged process cannot deny itself access
// to a path it owns. Same seam as statWorktreePath in submit_verdict.go.
var statIntegrationScript = os.Stat

// artifactGuardPostUpdateTestHook is a test-only hook invoked after a successful
// CAS merge and before the retained post-merge artifact validation backstop.
// Production code leaves this nil.
var artifactGuardPostUpdateTestHook func() error

// Test seams restore their previous values with t.Cleanup. Production keeps
// the hook nil and uses the shared transition validator directly.
var integrationMutationReceiptPersistTestHook func(models.IntegrationMutationReceipt)
var validateIntegrationLifecycleTransition = statevalidate.ValidateIntegrationLifecycleTransition
var mergeFinalStateTestHook func()

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

type integrationRefMutation struct {
	taskID       string
	beforeCommit string
	afterCommit  string
}

func (mutation *integrationRefMutation) receipt() models.IntegrationMutationReceipt {
	return models.IntegrationMutationReceipt{
		TaskID:       mutation.taskID,
		BeforeCommit: mutation.beforeCommit,
		AfterCommit:  mutation.afterCommit,
	}
}

func persistIntegrationMutationReceipt(bb *db.Blackboard, mutation *integrationRefMutation, authority *models.AgentAuthority) error {
	if mutation == nil || mutation.beforeCommit == mutation.afterCommit {
		return nil
	}
	receipt := mutation.receipt()
	if integrationMutationReceiptPersistTestHook != nil {
		integrationMutationReceiptPersistTestHook(receipt)
	}
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
		previous := *state
		previous.Goal = state.Goal
		if lifecycle := state.Goal.Integration; lifecycle != nil {
			previousLifecycle := *lifecycle
			previousLifecycle.MutationReceipts = slices.Clone(lifecycle.MutationReceipts)
			previous.Goal.Integration = &previousLifecycle
		} else {
			state.Goal.Integration = &models.IntegrationLifecycle{}
		}

		state.Goal.Integration.MutationReceipts = append(state.Goal.Integration.MutationReceipts, receipt)
		if err := invalidateGoalCompleteStopForMutation(state, receipt); err != nil {
			return err
		}
		return validateIntegrationLifecycleTransition(&previous, state)
	})
}

// provenMergeEffect returns the integration-ref movement this task already
// made, or nil when no live proof of it exists.
//
// Between the CAS merge and the MERGED write the integration ref has moved and
// persistIntegrationMutationReceipt has durably recorded that this task moved
// it. Two independent conditions make that record proof rather than a hint: the
// recorded commit must contain the approved review commit, which excludes the
// reverse receipt written by a rollback, and it must still be an ancestor of the
// integration ref, which excludes a merge that was later rolled back or lost.
func provenMergeEffect(state *models.State, gw *git.Git, integrationRef, taskID, expectedCommit string) (*integrationRefMutation, error) {
	if state.Goal.Integration == nil {
		return nil, nil
	}
	var candidate *models.IntegrationMutationReceipt
	for i := range state.Goal.Integration.MutationReceipts {
		if state.Goal.Integration.MutationReceipts[i].TaskID == taskID {
			candidate = &state.Goal.Integration.MutationReceipts[i]
		}
	}
	if candidate == nil || candidate.AfterCommit == "" {
		return nil, nil
	}
	carriesApproved, err := gw.IsAncestor(expectedCommit, candidate.AfterCommit)
	if err != nil {
		return nil, fmt.Errorf("failed to check recorded merge ancestry: %w", err)
	}
	if !carriesApproved {
		return nil, nil
	}
	head, err := gw.GetCommitSHA(integrationRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get integration HEAD: %w", err)
	}
	stillLive, err := gw.IsAncestor(candidate.AfterCommit, head)
	if err != nil {
		return nil, fmt.Errorf("failed to check recorded merge liveness: %w", err)
	}
	if !stillLive {
		return nil, nil
	}
	return &integrationRefMutation{
		taskID:       taskID,
		beforeCommit: candidate.BeforeCommit,
		afterCommit:  candidate.AfterCommit,
	}, nil
}

// interruptedMergePreparation reports whether the refusal in the caller's way is
// this actor's own unresolved wt-merge preparation at this exact boundary — the
// state left behind when the integration ref moved but the MERGED write did not
// land.
func interruptedMergePreparation(task *models.Task, request LifecycleRequest) bool {
	if task == nil || task.Lifecycle == nil || task.Lifecycle.Preparation == nil {
		return false
	}
	p := task.Lifecycle.Preparation
	// Registry omitted deliberately: this path already requires the preparer to
	// be the requester, so the requester's own generation is the authenticated
	// second generation and a registry lookup cannot add evidence.
	return p.Operation == integrationOperationWTMerge && p.Actor == request.Actor &&
		preparationStillCurrent(task, request, nil)
}

// retireProvenMergePreparation clears an interrupted wt-merge preparation whose
// external effect is proven, so the merge can be finished instead of requerying
// forever. The fence it lifts exists because an unresolved preparation means
// uncertain effects; the receipt and ancestry checks in provenMergeEffect remove
// that uncertainty. Exactly-once does not rest on this fence — the locked
// approved-status recheck before publication refuses any duplicate.
func retireProvenMergePreparation(bb *db.Blackboard, taskID string, authority *models.AgentAuthority, prepared models.LifecyclePreparation, request LifecycleRequest) error {
	return modifyLifecycleState(bb, authority, func(state *models.State) error {
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
// when the refusal is this actor's own interrupted wt-merge and the integration
// effect it left behind is proven. It reports false, with no state change, for
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
	proven, err := provenMergeEffect(state, gitWrapper, "refs/heads/"+integrationBranch, task.ID, expectedCommit)
	if err != nil {
		return false, err
	}
	if proven == nil {
		return false, nil
	}
	log.Printf("wt-merge %s: resuming interrupted merge — integration already carries %s", task.ID, shortSHA(proven.afterCommit))
	if err := retireProvenMergePreparation(bb, task.ID, authority, *task.Lifecycle.Preparation, request); err != nil {
		return false, err
	}
	return true, nil
}

func rollbackMergedCommit(projectRoot string, gitWrapper *git.Git, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID string) (*integrationRefMutation, error) {
	var mutation *integrationRefMutation
	err := withIntegrationMutationLock(projectRoot, "rollback "+taskID, func() error {
		if err := gitWrapper.UpdateRef(integrationRef, preMergeHEAD, mergeCommit); err != nil {
			var casErr *git.RefConflictError
			if errors.As(err, &casErr) {
				log.Printf("wt-merge %s: skipping rollback — another merge landed on top of %s", taskID, shortSHA(mergeCommit))
				return nil
			}
			return err
		}
		mutation = &integrationRefMutation{taskID: taskID, beforeCommit: mergeCommit, afterCommit: preMergeHEAD}

		// Ref rolled back — sync working tree to match pre-merge state. This
		// reverse sync undoes the forward SyncMergedFiles step.
		if syncErr := gitWrapper.SyncMergedFiles(mergeCommit, preMergeHEAD); syncErr != nil {
			log.Printf("wt-merge %s: WARNING — failed to sync working tree after rollback: %v", taskID, syncErr)
		}
		if restoreRef != "" {
			if restoreErr := gitWrapper.RestoreSyncedFiles(preMergeHEAD, mergeCommit, restoreRef); restoreErr != nil {
				return fmt.Errorf("failed to restore working tree after rollback: %w", restoreErr)
			}
		}
		return nil
	})
	return mutation, err
}

// rollbackMergedCommitAndPersist reports whether the integration ref actually
// moved back. It does not when another merge landed on top, and it cannot when
// the baseline equals the merge commit — a resumed merge whose own pre-merge
// HEAD is no longer known from this invocation. Callers must not describe those
// outcomes as a rollback: the task's commit is still in the integration branch.
func rollbackMergedCommitAndPersist(bb *db.Blackboard, projectRoot string, gitWrapper *git.Git, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID string, authority *models.AgentAuthority) (bool, error) {
	rewound := false
	err := withEffectiveIntegrationCompletionLinearization(projectRoot, "rollback "+taskID, func() error {
		if preMergeHEAD == "" || preMergeHEAD == mergeCommit {
			return nil
		}
		mutation, rollbackErr := rollbackMergedCommit(projectRoot, gitWrapper, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID)
		rewound = mutation != nil
		if receiptErr := persistIntegrationMutationReceipt(bb, mutation, authority); receiptErr != nil {
			receiptErr = fmt.Errorf("failed to persist rollback integration mutation receipt: %w", receiptErr)
			if rollbackErr != nil {
				return errors.Join(rollbackErr, receiptErr)
			}
			return receiptErr
		}
		return rollbackErr
	})
	return rewound, err
}

// integrationRetentionDetail names the state Git is actually left in when a
// failed merge was not rolled back, so the recorded diagnostic cannot imply a
// rewind that never happened.
func integrationRetentionDetail(rewound bool, rollbackErr error, mergeCommit string) string {
	if rewound || rollbackErr != nil {
		return ""
	}
	return fmt.Sprintf("integration branch retains %s: it was not rolled back", shortSHA(mergeCommit))
}

func buildArtifactGuardHook(bb *db.Blackboard, projectRoot string, gitWrapper *git.Git, taskID string) func(candidateTreeish string) error {
	return func(candidateTreeish string) error {
		if err := validateCandidateArtifactRefsWithFreshState(bb.Read, projectRoot, gitWrapper, candidateTreeish, taskID); err != nil {
			return &candidateArtifactGuardError{err: err}
		}
		return nil
	}
}

type candidateArtifactGuardError struct {
	err error
}

func (e *candidateArtifactGuardError) Error() string {
	return e.err.Error()
}

func (e *candidateArtifactGuardError) Unwrap() error {
	return e.err
}

func validateCandidateArtifactRefsWithFreshState(
	readState func() (*models.State, error),
	projectRoot string,
	lookup statevalidate.CandidateTreeLookup,
	candidateTreeish string,
	taskID string,
) error {
	state, err := readState()
	if err != nil {
		return fmt.Errorf("candidate artifact guard failed to read state: %w", err)
	}

	firstErr := statevalidate.ValidateCandidateMergeArtifactRefs(candidateTreeish, state, projectRoot, taskID, lookup)
	if firstErr == nil {
		return nil
	}

	confirmationState, confirmationReadErr := readState()
	if confirmationReadErr != nil {
		freshnessErr := fmt.Errorf("failed to re-read state for candidate artifact guard freshness: %w", confirmationReadErr)
		return fmt.Errorf("candidate artifact guard failed and state freshness could not be verified: %w", errors.Join(firstErr, freshnessErr))
	}

	if confirmationErr := statevalidate.ValidateCandidateMergeArtifactRefs(candidateTreeish, confirmationState, projectRoot, taskID, lookup); confirmationErr != nil {
		return confirmationErr
	}
	return nil
}

// shortSHA truncates a SHA to 7 characters for log messages.
func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// casMergeOutcome holds the result of a compare-and-swap merge into an integration ref.
type casMergeOutcome struct {
	mergeCommit  string // SHA of the resulting commit (merge commit or fast-forwarded task commit)
	preMergeHEAD string // integration HEAD before the merge (needed for working tree sync and rollback)
	fastForward  bool   // true when the merge was a fast-forward or the commit was already merged
	conflict     bool   // true when merge-tree found conflicts; caller handles state transition
}

// performCASMerge merges expectedCommit into integrationRef using a compare-and-swap
// retry loop to handle concurrent merges. Returns conflict=true when merge-tree
// detects conflicts (caller is responsible for the INTEGRATION_FAILED transition).
func performCASMerge(gw *git.Git, integrationRef, expectedCommit, taskID string, preUpdateHook func(candidateTreeish string) error) (*casMergeOutcome, error) {
	var mergeCommit, preMergeHEAD string
	var fastForward bool

	var attempt int
	for attempt = 0; attempt < maxMergeRetries; attempt++ {
		if attempt > 0 {
			log.Printf("wt-merge %s: CAS retry attempt %d/%d", taskID, attempt+1, maxMergeRetries)
		}

		var err error
		preMergeHEAD, err = gw.GetCommitSHA(integrationRef)
		if err != nil {
			return nil, fmt.Errorf("failed to get integration HEAD: %w", err)
		}
		if mergeCASRetryTestHook != nil {
			if hookErr := mergeCASRetryTestHook(attempt, integrationRef, preMergeHEAD); hookErr != nil {
				return nil, fmt.Errorf("merge CAS retry hook failed: %w", hookErr)
			}
		}

		// Already merged — expectedCommit is ancestor of integration HEAD.
		isAncestor, err := gw.IsAncestor(expectedCommit, preMergeHEAD)
		if err != nil {
			return nil, fmt.Errorf("failed to check ancestry: %w", err)
		}
		if isAncestor {
			return &casMergeOutcome{
				mergeCommit:  preMergeHEAD,
				preMergeHEAD: preMergeHEAD,
				fastForward:  true,
			}, nil
		}

		// Fast-forward: integration HEAD is ancestor of expected commit.
		isFF, err := gw.IsAncestor(preMergeHEAD, expectedCommit)
		if err != nil {
			return nil, fmt.Errorf("failed to check fast-forward: %w", err)
		}
		if isFF {
			if preUpdateHook != nil {
				if err := preUpdateHook(expectedCommit); err != nil {
					retry, hookErr := handlePreUpdateHookFailure(gw, integrationRef, preMergeHEAD, err)
					if retry {
						continue
					}
					return nil, hookErr
				}
			}
			if err := gw.UpdateRef(integrationRef, expectedCommit, preMergeHEAD); err != nil {
				var casErr *git.RefConflictError
				if errors.As(err, &casErr) {
					continue
				}
				return nil, fmt.Errorf("failed to fast-forward integration branch: %w", err)
			}
			return &casMergeOutcome{
				mergeCommit:  expectedCommit,
				preMergeHEAD: preMergeHEAD,
				fastForward:  true,
			}, nil
		}

		// True merge — use merge-tree (no working tree modification).
		treeSHA, clean, err := gw.MergeTree(preMergeHEAD, expectedCommit)
		if err != nil {
			return nil, fmt.Errorf("merge-tree computation failed: %w", err)
		}
		if !clean {
			return &casMergeOutcome{
				preMergeHEAD: preMergeHEAD,
				conflict:     true,
			}, nil
		}

		mergeMsg := "Merge " + taskID + " (task/" + taskID + ")"
		mergeCommit, err = gw.CreateCommitFromTree(treeSHA, []string{preMergeHEAD, expectedCommit}, mergeMsg)
		if err != nil {
			return nil, fmt.Errorf("failed to create merge commit: %w", err)
		}
		fastForward = false

		if preUpdateHook != nil {
			if err := preUpdateHook(mergeCommit); err != nil {
				retry, hookErr := handlePreUpdateHookFailure(gw, integrationRef, preMergeHEAD, err)
				if retry {
					continue
				}
				return nil, hookErr
			}
		}
		if err := gw.UpdateRef(integrationRef, mergeCommit, preMergeHEAD); err != nil {
			var casErr *git.RefConflictError
			if errors.As(err, &casErr) {
				continue
			}
			return nil, fmt.Errorf("failed to update integration branch: %w", err)
		}
		break
	}
	if attempt == maxMergeRetries {
		return nil, fmt.Errorf("merge CAS failed after %d attempts — high contention on %s", maxMergeRetries, integrationRef)
	}

	return &casMergeOutcome{
		mergeCommit:  mergeCommit,
		preMergeHEAD: preMergeHEAD,
		fastForward:  fastForward,
	}, nil
}

func handlePreUpdateHookFailure(gw *git.Git, integrationRef, preMergeHEAD string, hookErr error) (bool, error) {
	currentHEAD, err := gw.GetCommitSHA(integrationRef)
	if err != nil {
		stalenessErr := fmt.Errorf("failed to re-read integration HEAD: %w", err)
		return false, fmt.Errorf("pre-update hook failed and staleness could not be verified: %w", errors.Join(hookErr, stalenessErr))
	}
	if currentHEAD != preMergeHEAD {
		return true, nil
	}
	return false, hookErr
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
	bb := db.For(statePath)
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}
	if authority != nil {
		if err := RequireAgentAuthority(state, *authority); err != nil {
			return nil, err
		}
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
	effects := "none"
	defer func() {
		if retErr != nil {
			task = readLifecycleTask(projectRoot, taskID, authority)
			outcome, action := models.LifecycleStateChanged, "requery"
			var invalid *PreconditionError
			if effects == "none" && errors.As(retErr, &invalid) {
				outcome, action = models.LifecycleInvalidInput, "correct_input"
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
			effects = "committed"

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

	integrationRef := "refs/heads/" + integrationBranch
	// Persist the request before touching the integration ref. A concurrent or
	// restarted invocation must requery while this preparation is unresolved.
	var preparation models.LifecyclePreparation
	err = modifyLifecycleState(bb, authority, func(s *models.State) error {
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
	effects = "unknown"
	casStarted := false
	forwardRolledBack := false
	defer func() {
		// Once CAS starts, a returned error can leave the ref or main checkout
		// partially updated. Preserve that fence for inspected recovery unless
		// this invocation rewound the ref it moved: otherwise its own
		// same-generation retries requery until a restart (D77). Retirement
		// usually follows a state-lock timeout, so it waits the patient budget.
		if retErr != nil && (!casStarted || forwardRolledBack) {
			retErr = retireFailedLifecyclePreparation(bb.Patient(), taskID, authority, &preparation, retErr, effects)
		}
	}()

	artifactGuardHook := buildArtifactGuardHook(bb, projectRoot, gitWrapper, taskID)
	var outcome *casMergeOutcome
	var forwardMutation *integrationRefMutation
	err = withEffectiveIntegrationCompletionLinearization(projectRoot, "forward "+taskID, func() error {
		mutationErr := withIntegrationMutationLock(projectRoot, "forward "+taskID, func() error {
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
		syncErr := withIntegrationMutationLock(projectRoot, "restore "+taskID, func() error {
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
	effects = "committed"
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
