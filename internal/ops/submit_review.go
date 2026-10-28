package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/functionalclusters"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/stacklit"
)

const integrationOperationSubmitForReview = "submit-for-review"

// operationalCodeStateWrite is the OperationalError.Code of a failed state
// write during submission.
const operationalCodeStateWrite = "state_write"

// SubmitForReviewResult contains the outcome of submitting a task for review.
type SubmitForReviewResult struct {
	models.LifecycleOutcome
	TaskID       string   `json:"task_id"`
	ReviewCommit string   `json:"review_commit"`
	AgentID      string   `json:"agent_id"`
	Warnings     []string `json:"warnings,omitempty"`
	// DeferredCommands lists the canonical commands admission skipped; the
	// deferred-validation batch runs them on integration (ADR-0204).
	DeferredCommands []models.AcceptanceDeferredCommand `json:"deferred_commands,omitempty"`
}

var (
	submitReviewRefreshIndexes                 = scipsearch.RefreshIndexes
	submitReviewRefreshStacklitIndex           = stacklit.RefreshIndex
	submitReviewRefreshFunctionalClustersIndex = functionalclusters.RefreshIndex
	submitReviewBeforeModifyTestHook           func()
	// submitReviewNewGit is the single Git entry point of the submission
	// boundary, so a test can prove that a rejected payload reaches no Git work.
	submitReviewNewGit = gitpkg.New
)

// SubmitForReviewPayload builds the canonical object of the submit-for-review
// operation. The preflight command and this mutation boundary validate the
// same object through the same schema, so neither can accept what the other
// rejects for a structural reason.
// submitReviewCauseLimit bounds the write-state cause carried to the agent.
// Large enough for a validation message with its field path, small enough that
// a wrapped chain cannot flood a prompt.
const submitReviewCauseLimit = 512

func SubmitForReviewPayload(commitRef string) map[string]any {
	return map[string]any{"commit_ref": commitRef}
}

// rejectInvalidLifecyclePayload runs an operation's canonical object through
// its registered schema and turns a structural rejection into the INVALID_INPUT
// result the caller must correct against. It reads the payload and nothing
// else, so a mutation boundary can call it before any state, lock or Git
// access. A valid payload yields nil.
func rejectInvalidLifecyclePayload(operation string, payload map[string]any) error {
	version, diagnostics, err := payloadschema.Validate(operation, payload)
	if err != nil {
		return err
	}
	if len(diagnostics) == 0 {
		return nil
	}
	return NewLifecycleInvalidInputError(operation, nil, diagnostics,
		fmt.Errorf("payload rejected by the %s schema, version %d: %s %s",
			operation, version, diagnostics[0].Field, diagnostics[0].Constraint))
}

// SubmitForReview validates that commitRef resolves to the worktree HEAD before rebase,
// rebases the task branch onto the integration branch to catch conflicts early,
// then atomically transitions the task to READY_FOR_REVIEW.
// No terminal I/O.
func SubmitForReview(projectRoot, taskID, commitRef, agentID string) (*SubmitForReviewResult, error) {
	return submitForReviewLifecycle(projectRoot, taskID, commitRef, agentID, nil, LifecycleRequestOptions{})
}

// SubmitForReviewWithAuthority is the authenticated command entry point. The
// caller-held generation is revalidated inside every state transaction.
func SubmitForReviewWithAuthority(projectRoot, taskID, commitRef string, authority models.AgentAuthority) (*SubmitForReviewResult, error) {
	return submitForReviewLifecycle(projectRoot, taskID, commitRef, authority.ID, &authority, LifecycleRequestOptions{})
}

func prepareSubmitForReview(projectRoot, taskID, commitRef, agentID string, authority *models.AgentAuthority, opts LifecycleRequestOptions, invocation *submissionInvocation) (*preparedSubmission, error) {
	s := &submissionRun{projectRoot: projectRoot, taskID: taskID, commitRef: commitRef, agentID: agentID, authority: authority, opts: opts, invocation: invocation}

	// Preconditions from one state snapshot.
	if replay, err := s.readSnapshot(); replay != nil || err != nil {
		return replay, err
	}
	if err := s.resolvePipeline(); err != nil {
		return nil, err
	}
	if err := s.checkOwnership(); err != nil {
		return nil, err
	}

	// Git and command work under the task lock, never the blackboard lock.
	if err := s.openWorktree(); err != nil {
		return nil, err
	}
	if replay, err := s.resolveCandidate(); replay != nil || err != nil {
		return replay, err
	}
	for _, phase := range []func() error{s.checkCandidate, s.resolveRebaseBase, s.loadAcceptance, s.planAdmission, s.recordPreparation, s.rebase, s.checkRebased} {
		if err := phase(); err != nil {
			return nil, err
		}
	}
	return &preparedSubmission{acceptanceBatch: s.acceptanceBatch(), refresh: s.refreshIndexes, complete: s.finalize}, nil
}

func refreshSubmitReviewScipIndexes(projectRoot, worktreePath string, configuredLanguages []string) []string {
	result, err := submitReviewRefreshIndexes(scipsearch.RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          worktreePath,
		ConfiguredLanguages: configuredLanguages,
	})
	warnings := scipRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("scip-search: %v", err))
	}
	return warnings
}

func refreshSubmitReviewStacklitIndex(projectRoot, worktreePath string) []string {
	result, err := submitReviewRefreshStacklitIndex(stacklit.RefreshOptions{
		ProjectRoot: projectRoot,
		TargetRoot:  worktreePath,
	})
	warnings := stacklitRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("stacklit: %v", err))
	}
	return warnings
}

func refreshSubmitReviewFunctionalClustersIndex(projectRoot, worktreePath string, configuredLanguages []string) []string {
	result, err := submitReviewRefreshFunctionalClustersIndex(functionalclusters.RefreshOptions{
		ProjectRoot:         projectRoot,
		TargetRoot:          worktreePath,
		ConfiguredLanguages: configuredLanguages,
	})
	warnings := functionalClustersRefreshWarnings(result)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("functional-clusters: %v", err))
	}
	return warnings
}

func scipRefreshWarnings(result scipsearch.RefreshResult) []string {
	warnings := make([]string, 0, len(result.Failures))
	for _, failure := range result.Failures {
		warnings = append(warnings, fmt.Sprintf("scip-search %s: %s", failure.Language, failure.Diagnostic))
	}
	return warnings
}

// refreshIndexes refreshes the warning-only repository indexes of the rebased
// worktree. It runs without task locking, so the candidate is rechecked after.
func (s *submissionRun) refreshIndexes() []string {
	warnings := refreshSubmitReviewScipIndexes(s.projectRoot, s.worktreePath, s.state.Config.ScipSearch)
	warnings = append(warnings, refreshSubmitReviewStacklitIndex(s.projectRoot, s.worktreePath)...)
	return append(warnings, refreshSubmitReviewFunctionalClustersIndex(s.projectRoot, s.worktreePath, s.state.Config.ScipSearch)...)
}
