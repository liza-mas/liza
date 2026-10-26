package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pairingindex"
)

const integrationTestCommand = "scripts/integration-test.sh"

// mergeFallbackIntegrationBranch is the branch a merge integrates into when
// state configures none. It differs from defaultIntegrationBranch, which names
// the integration branch for transition passes.
const mergeFallbackIntegrationBranch = "main"

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

// IntegrationReason is why a merge or submission was refused as an integration
// failure. Its values are persisted in history and diagnostics, so each keeps
// its spelling; they cross into those string-typed records by conversion.
type IntegrationReason string

// Integration failure reason constants.
const (
	IntegrationReasonHEADMismatch           IntegrationReason = "worktree HEAD mismatch"
	IntegrationReasonMergeConflict          IntegrationReason = "merge conflict"
	IntegrationReasonReviewBoundaryMismatch IntegrationReason = "review boundary mismatch"
	IntegrationReasonTestsFailed            IntegrationReason = "integration tests failed"
	IntegrationReasonStateInvalid           IntegrationReason = "post-merge state validation failed"
)

const (
	integrationOperationWTMerge = "wt-merge"
)

// IntegrationFailedError indicates the merge or integration tests failed.
// State has been updated to INTEGRATION_FAILED appropriately.
type IntegrationFailedError struct {
	Reason        IntegrationReason
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
		retErr = WrapLifecycleError(integrationOperationWTMerge, nil, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
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

// mergeInvocation carries one wt-merge call's values between its phases, so a
// phase takes the invocation instead of a long parameter list. The phases keep
// the call's lock order; none opens a lock the entry function does not name,
// and the transactions among them receive the state they edit.
type mergeInvocation struct {
	projectRoot string
	taskID      string
	agentID     string
	authority   *models.AgentAuthority
	opts        LifecycleRequestOptions
	mergeExtra  []map[string]any

	bb      *db.Blackboard
	git     *git.Git
	state   *models.State
	task    *models.Task
	request LifecycleRequest
	pb      *pipelineBundle

	reviewCommit      string
	expectedCommit    string
	integrationBranch string
	integrationRef    string
	preparation       models.LifecyclePreparation

	// effects is what the invocation is known to have done; the entry function
	// reports it on failure.
	effects LifecycleEffects
	// Publication flags decide whether a failed invocation may retire its
	// preparation: see preparationRetirable.
	casStarted         bool
	forwardRolledBack  bool
	forwardUnpublished bool

	outcome          *casMergeOutcome
	mergeCommit      string
	rollbackBaseline string
	restoreRef       string
	warnings         []string

	tests               integrationScriptRun
	setup               postMergeSetup
	autoConfiguredSetup bool
	lifecycle           models.LifecycleOutcome
}

func mergeWorktree(projectRoot, taskID, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, mergeExtra ...map[string]any) (result *MergeResult, retErr error) {
	m := &mergeInvocation{
		projectRoot: projectRoot,
		taskID:      taskID,
		agentID:     agentID,
		authority:   authority,
		opts:        opts,
		mergeExtra:  mergeExtra,
		git:         git.New(projectRoot),
	}
	replay, err := m.admit()
	if err != nil || replay != nil {
		return replay, err
	}
	m.effects = LifecycleEffectsNone
	defer func() {
		if retErr != nil {
			retErr = m.wrapFailure(retErr)
		}
	}()

	if err := m.checkPreconditions(); err != nil {
		return nil, err
	}
	if err := m.prepare(); err != nil {
		return nil, err
	}
	defer func() {
		if retErr != nil && m.preparationRetirable() {
			// Retirement usually follows a state-lock timeout, so it waits the
			// patient budget.
			retErr = retireFailedLifecyclePreparation(m.bb.Patient(), taskID, authority, &m.preparation, retErr, m.effects)
		}
	}()

	if err := m.publish(); err != nil {
		return nil, err
	}
	if err := m.attribute(); err != nil {
		return nil, err
	}
	m.detectCheckout()
	if err := m.validateArtifacts(); err != nil {
		return nil, err
	}
	if err := m.runIntegrationTests(); err != nil {
		return nil, err
	}
	m.setup = detectPostMergeSetup(projectRoot, m.state.Config.PostWorktreeCmd)
	m.restoreCheckout()
	if err := m.commitMerged(); err != nil {
		return nil, err
	}
	m.runPostCommitEffects()
	return m.result(), nil
}

// preparationRetirable reports whether a failed invocation may retire its
// preparation. Once CAS starts, a returned error can leave the ref or main
// checkout partially updated. That fence stays for inspected recovery unless
// this invocation rewound the ref it moved, or provably moved nothing:
// otherwise its own same-generation retries requery until a restart (D77).
func (m *mergeInvocation) preparationRetirable() bool {
	return !m.casStarted || m.forwardRolledBack || m.forwardUnpublished
}

// integrationBranchOf is the configured integration branch, or the default.
func integrationBranchOf(state *models.State) string {
	if state.Config.IntegrationBranch == "" {
		return mergeFallbackIntegrationBranch
	}
	return state.Config.IntegrationBranch
}
