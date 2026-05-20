package ops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
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
}

func (e *IntegrationFailedError) Error() string {
	if e.RollbackError != nil {
		return fmt.Sprintf("integration failed: %s (rollback also failed: %v)", e.Reason, e.RollbackError)
	}
	return fmt.Sprintf("integration failed: %s", e.Reason)
}

// MergeResult contains the outcome of a successful worktree merge.
type MergeResult struct {
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

func markIntegrationFailedWithDiagnostic(
	bb *db.Blackboard,
	taskID, agentID, reason, mergeCommit string,
	pb *pipelineBundle,
	diagnostic map[string]any,
) error {
	var pr models.PipelineResolver
	if pb != nil {
		pr = pb.pr
	}
	return bb.Modify(func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !models.IsApprovedForMerge(t, pr) {
			return fmt.Errorf("task %s status changed concurrently (now %s)", taskID, t.Status)
		}
		if err := pipelineTransition(t, models.TaskStatusIntegrationFailed, pb); err != nil {
			return err
		}
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

func rollbackMergedCommit(gitWrapper *git.Git, integrationRef, preMergeHEAD, mergeCommit, restoreRef, taskID string) error {
	if err := gitWrapper.UpdateRef(integrationRef, preMergeHEAD, mergeCommit); err != nil {
		var casErr *git.RefConflictError
		if errors.As(err, &casErr) {
			log.Printf("wt-merge %s: skipping rollback — another merge landed on top of %s", taskID, shortSHA(mergeCommit))
			return nil
		}
		return err
	}

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
					return nil, err
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
				return nil, err
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

// MergeWorktree merges an approved task into the integration branch.
// This is the final step in the task lifecycle, integrating completed work.
// Returns IntegrationFailedError if merge conflicts or integration tests fail.
//
// No terminal I/O — integration test output is captured and returned in the result or error.
func MergeWorktree(projectRoot, taskID, agentID string, mergeExtra ...map[string]any) (*MergeResult, error) {
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}

	// Setup paths
	statePath := paths.New(projectRoot).StatePath()

	// Read state
	bb := db.For(statePath)
	state, task, err := readTaskState(bb, taskID)
	if err != nil {
		return nil, err
	}

	// Load pipeline bundle once for all pipeline-aware checks
	pb, pbErr := loadPipelineBundle(projectRoot)
	if pbErr != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", pbErr)
	}
	pr := pb.pr

	if !models.IsApprovedForMerge(task, pr) {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task must be in an approved state to merge (current status: %s)", task.Status)}
	}

	if task.ReviewCommit == nil {
		return nil, &PreconditionError{Reason: "task has no review_commit"}
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
			if err := markIntegrationFailedWithDiagnostic(bb, taskID, agentID, IntegrationReasonHEADMismatch, "", pb, diagnostic); err != nil {
				return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", err)
			}

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

	outcome, err := performCASMerge(gitWrapper, integrationRef, expectedCommit, taskID, nil)
	if err != nil {
		return nil, err
	}
	if outcome.conflict {
		if updateErr := markIntegrationFailed(bb, taskID, agentID, "merge conflict", "", pb); updateErr != nil {
			return nil, fmt.Errorf("failed to update state to INTEGRATION_FAILED: %w", updateErr)
		}
		return nil, &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
	}

	mergeCommit := outcome.mergeCommit
	preMergeHEAD := outcome.preMergeHEAD
	fastForward := outcome.fastForward

	// Sync files changed by the merge into the main working tree.
	// update-ref only moves the ref pointer; without this, files added/modified
	// by the merged commit are absent from the working directory. This is required
	// both for integration test correctness (tests run in projectRoot) and so the
	// working tree reflects what's committed after merge.
	// Only touches merge-affected files — safe for working trees with unrelated
	// pending changes (e.g. .liza/state.yaml).
	if err := gitWrapper.SyncMergedFiles(preMergeHEAD, mergeCommit); err != nil {
		return nil, fmt.Errorf("failed to sync working tree after merge: %w", err)
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

	currentState, err := bb.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state for post-merge artifact validation: %w", err)
	}
	if err := statevalidate.ValidateArtifactRefs(currentState, projectRoot); err != nil {
		rollbackErr := rollbackMergedCommit(gitWrapper, integrationRef, preMergeHEAD, mergeCommit, rollbackRestoreRef, taskID)
		diagnostic := integrationFailureDiagnosticWithDetail(IntegrationReasonStateInvalid, err.Error(), mergeCommit, "", rollbackErr)
		if updateErr := markIntegrationFailedWithDiagnostic(bb, taskID, agentID, IntegrationReasonStateInvalid, mergeCommit, pb, diagnostic); updateErr != nil {
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
	if _, statErr := os.Stat(integrationTestScript); statErr == nil {
		testsRan = true
		var combinedOutput bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), DefaultIntegrationTestTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, integrationTestScript)
		cmd.Dir = projectRoot
		cmd.Stdout = &combinedOutput
		cmd.Stderr = &combinedOutput
		// Kill the entire process tree on timeout (Unix: process group kill;
		// Windows: default CommandContext kill). WaitDelay ensures cmd.Wait
		// returns even if child processes hold pipes open after kill.
		configProcessGroupKill(cmd)
		cmd.WaitDelay = 5 * time.Second

		if runErr := cmd.Run(); runErr != nil {
			testOutput = combinedOutput.String()
			if ctx.Err() == context.DeadlineExceeded {
				testOutput += fmt.Sprintf("\n[liza] integration test killed after %s timeout", DefaultIntegrationTestTimeout)
			}

			// CAS rollback: only rewind if ref still points to our merge commit.
			// If someone else merged on top, rewinding would drop their work.
			rollbackErr := rollbackMergedCommit(gitWrapper, integrationRef, preMergeHEAD, mergeCommit, rollbackRestoreRef, taskID)

			diagnostic := integrationFailureDiagnostic(IntegrationReasonTestsFailed, mergeCommit, testOutput, rollbackErr)
			if updateErr := markIntegrationFailedWithDiagnostic(bb, taskID, agentID, IntegrationReasonTestsFailed, mergeCommit, pb, diagnostic); updateErr != nil {
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
		if syncErr := gitWrapper.RestoreSyncedFiles(preMergeHEAD, mergeCommit, "HEAD"); syncErr != nil {
			warnings = append(warnings, fmt.Sprintf("failed to restore working tree: %v", syncErr))
		}
	}

	// Update state to MERGED (before worktree cleanup — if write fails,
	// worktree still exists for investigation; reverse order would lose the worktree
	// while state still says APPROVED)
	var autoConfiguredPostWorktreeCmd bool
	err = bb.Modify(func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &lizaerrors.NotFoundError{Entity: "task", ID: taskID}
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

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update state to MERGED: %w", err)
	}
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

	return &MergeResult{
		TaskID:            taskID,
		MergeCommit:       mergeCommit,
		FastForward:       fastForward,
		TestsRan:          testsRan,
		NoTestScriptFound: noTestScriptFound,
		TestOutput:        testOutput,
		Warnings:          warnings,
	}, nil
}
