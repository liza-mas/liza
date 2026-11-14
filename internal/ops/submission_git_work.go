package ops

import (
	stderrors "errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/preventionrecord"
	"github.com/liza-mas/liza/internal/roles"
)

// submitRebaseDiagnosticLimit bounds the rebase output a failure detail carries.
const submitRebaseDiagnosticLimit = 2000

// openWorktree locates the task worktree and refuses one that is missing or
// on another branch. Git work holds the task lock, never the blackboard lock.
func (s *submissionRun) openWorktree() error {
	s.git = submitReviewNewGit(s.projectRoot)
	s.worktreePath = s.git.GetWorktreePath(s.taskID)

	if _, err := os.Stat(s.worktreePath); os.IsNotExist(err) {
		return &errors.WorktreeContextError{
			Operation: integrationOperationSubmitForReview,
			TaskID:    s.taskID,
			Reason:    "worktree directory does not exist",
			Err:       err,
		}
	}

	wtBranch, err := s.git.GetWorktreeBranch(s.worktreePath)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "resolve-worktree-branch",
			Message: "failed to determine worktree branch",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"recovery_hint": "Inspect the task worktree git metadata, ensure the branch resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}

	s.expectedBranch = paths.TaskBranchPrefix + s.taskID
	if wtBranch != s.expectedBranch {
		if wtBranch == "" {
			return &PreconditionError{Reason: fmt.Sprintf("worktree is in detached HEAD state (expected branch: %s)", s.expectedBranch)}
		}
		return &PreconditionError{Reason: fmt.Sprintf("worktree is on branch %s (expected: %s)", wtBranch, s.expectedBranch)}
	}
	return nil
}

// checkWorktreeBranch is entry worktree_branch: the task worktree exists and is
// on the task branch, and its HEAD, the candidate, can be read.
func (s *submissionRun) checkWorktreeBranch() error {
	if err := s.openWorktree(); err != nil {
		return err
	}
	preRebaseCommit, err := s.git.GetWorktreeHEAD(s.taskID)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "pre-rebase-head",
			Message: "failed to read worktree HEAD",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"recovery_hint": "Inspect the task worktree git state, ensure HEAD resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	s.preRebaseCommit = preRebaseCommit
	return nil
}

// resolveCommitRef requires the submitted ref to be the candidate. A legacy
// call that named HEAD persists the immutable input and may then replay a
// retained receipt: the returned submission is final. Gate only.
func (s *submissionRun) resolveCommitRef() (*preparedSubmission, error) {
	resolvedCommit, err := s.git.ResolveWorktreeCommit(s.taskID, s.commitRef)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("provided commit ref %q could not be resolved in worktree", s.commitRef)}
	}
	if resolvedCommit != s.preRebaseCommit {
		return nil, &PreconditionError{Reason: fmt.Sprintf("provided commit ref %q resolved to %s, which does not match worktree HEAD %s", s.commitRef, resolvedCommit, s.preRebaseCommit)}
	}
	if s.opts.RequestID == "" {
		// Persist the immutable input even when the first legacy call used HEAD.
		request, receipt, err := submissionRequest(s.task, resolvedCommit, s.agentID, s.authority, s.opts, s.state.Agents)
		if err != nil {
			return nil, err
		}
		s.request = request
		if receipt != nil {
			return replaySubmission(s.task, receipt, s.agentID), nil
		}
	}
	return nil, nil
}

// checkPlanningOutputAcceptance is entry planning_output_acceptance: the
// candidate's planning output declares parseable child Acceptance Contracts.
func (s *submissionRun) checkPlanningOutputAcceptance() error {
	return validatePlanningOutputAcceptance(s.projectRoot, s.task, s.preRebaseCommit)
}

// checkPlanningOutputRuntimeInputs is entry planning_output_runtime_inputs.
func (s *submissionRun) checkPlanningOutputRuntimeInputs() error {
	return validatePlanningOutputRuntimeInputs(s.projectRoot, s.state, s.task)
}

// checkOutputRefFragments is entry output_ref_fragments: every output ref
// fragment resolves in the candidate.
func (s *submissionRun) checkOutputRefFragments() error {
	return validateOutputRefFragments(s.projectRoot, s.task, s.preRebaseCommit)
}

// enforceTDD requires test files in the candidate range of a doer's code task.
func (s *submissionRun) enforceTDD() error {
	roleType, _ := s.resolver.RoleType(s.runtimeRole)
	requiresTDD, err := taskRequiresTDD(s.task, s.resolver)
	if err != nil {
		return err
	}
	if roleType != roles.RoleTypeDoer || !requiresTDD || s.task.BaseCommit == nil {
		return nil
	}
	testDiagnostics, err := AnalyzeTestFiles(s.git, s.taskID, *s.task.BaseCommit, s.preRebaseCommit)
	if err != nil {
		// Err is withheld from the JSON/MCP envelope, which carries git's exit
		// status and a closed failure class instead of git's text. The text
		// goes to stderr directly, not through the logger that --json
		// discards, as submit progress does (ADR-0210), so every output mode
		// keeps it for whoever reads the CLI or test output.
		fmt.Fprintf(os.Stderr, "WARNING: task %s: test-file check failed: %v\n", s.taskID, err)
		exitCode, failure := gitpkg.ClassifyFailure(err)
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "check-test-files",
			Message: "failed to check for test files",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"git_exit_code": exitCode,
				"git_failure":   string(failure),
				"recovery_hint": "Inspect the task worktree git history and retry submit-for-review after the diff range can be analyzed.",
			},
			Err: err,
		}
	}
	if len(testDiagnostics.TestFilesMatched) == 0 && GetTDDWaiver(s.task.History, s.agentID) == "" {
		return &PreconditionError{
			Reason:  fmt.Sprintf("task %s: code tasks must include test files (e.g. *_test.go, *.test.ts, test_*.py) — TDD is mandatory. For non-behavioral documentation/config/spec-only work, submit a pre-execution checkpoint with --tdd-not-required <justification>; reviewers verify the justification.", s.taskID),
			Details: testDiagnostics.Details(),
		}
	}
	return nil
}

// Bounds of the prevention record refusal: commits listed in its reason, in
// its details, and characters of a subject in its reason.
const (
	preventionReasonCommitLimit  = 10
	preventionDetailsCommitLimit = 50
	preventionSubjectLimit       = 100
	preventionSHAPrefix          = 12
)

// checkPreventionRecords refuses, as a correctable input error, a doer's
// candidate whose published range holds a fix commit without a well-formed
// prevention record, or with an agent's accepted disposition. It runs before
// any effect, so a refusal leaves the task as it was.
func (s *submissionRun) checkPreventionRecords() error {
	roleType, _ := s.resolver.RoleType(s.runtimeRole)
	if roleType != roles.RoleTypeDoer || s.task.BaseCommit == nil {
		return nil
	}
	integrationHead, err := s.integrationHead(s.state.Config.IntegrationBranch)
	if err != nil {
		return s.preventionRecordFailure(err, "failed to resolve integration branch HEAD",
			"Ensure the configured integration branch exists and is fetchable, then retry submit-for-review.")
	}
	result, err := preventionrecord.Check(s.worktreePath, integrationHead, s.preRebaseCommit, *s.task.BaseCommit)
	if err != nil {
		return s.preventionRecordFailure(err, "failed to check prevention records",
			"Ensure the configured integration branch exists and the task worktree history can be read, then retry submit-for-review.")
	}
	if len(result.Offenders) == 0 {
		return nil
	}
	return preventionRecordRefusal(result, s.taskID, s.worktreePath, s.expectedBranch)
}

// preventionRecordFailure reports a git failure of the record check. The
// message and recovery hint name the failing step: resolving the integration
// branch or reading the range. As with the test-file check, git's text goes to
// stderr only; the envelope carries its exit status and a closed failure class.
func (s *submissionRun) preventionRecordFailure(err error, message, recoveryHint string) error {
	fmt.Fprintf(os.Stderr, "WARNING: task %s: prevention record check failed: %v\n", s.taskID, err)
	exitCode, failure := gitpkg.ClassifyFailure(err)
	return &OperationalError{
		Code:    operationalCodeGitOperation,
		Phase:   "check-prevention-records",
		Message: message,
		Details: map[string]any{
			"operation":     integrationOperationSubmitForReview,
			"task_id":       s.taskID,
			"git_exit_code": exitCode,
			"git_failure":   string(failure),
			"recovery_hint": recoveryHint,
		},
		Err: err,
	}
}

// preventionRecordRefusal words the refusal: self-sufficient text naming the
// offending commits, the installed record document and an executable repair,
// and details holding names only, never a record value.
func preventionRecordRefusal(result preventionrecord.Result, taskID, worktreePath, branch string) *PreconditionError {
	document := "~/" + paths.GlobalDirName() + "/skills/shared/references/prevention-record.md"

	var listed []string
	accepted := false
	for i, offender := range result.Offenders {
		var names []string
		for _, finding := range offender.Findings {
			name := string(finding.Problem)
			if finding.Key != "" {
				name += " (" + finding.Key + ")"
			}
			names = append(names, name)
			accepted = accepted || finding.Problem == preventionrecord.DispositionAccepted
		}
		if i < preventionReasonCommitLimit {
			listed = append(listed, fmt.Sprintf("%s %q: %s", truncateRunes(offender.SHA, preventionSHAPrefix), truncateRunes(offender.Subject, preventionSubjectLimit), strings.Join(names, ", ")))
		}
	}
	if more := len(result.Offenders) - preventionReasonCommitLimit; more > 0 {
		listed = append(listed, fmt.Sprintf("and %d more", more))
	}

	var reason strings.Builder
	fmt.Fprintf(&reason, "task %s: %d fix commit(s) in the submitted range lack a well-formed prevention record in their final trailer block: %s. The record format is in %s.",
		taskID, len(result.Offenders), strings.Join(listed, "; "), document)
	if accepted {
		reason.WriteString(" An agent never records Prevention-Disposition: accepted; record scheduled with Prevention-Owner: human.")
	}
	fmt.Fprintf(&reason, " Repair, per commit <sha>, without an interactive rebase: `git -C %[1]s log -1 --format=%%B <sha> > <file>`; edit <file> so the record is the message's last paragraph, in the same paragraph as any other trailer (no blank line before Co-Authored-By); then for HEAD run `git -C %[1]s commit --amend -F <file>`, for an earlier commit run `git -C %[1]s checkout --detach <sha>`, `git -C %[1]s commit --amend -F <file>`, `git -C %[1]s rebase --onto HEAD <sha> %[2]s`. Later commits get new SHAs; resubmit with the new HEAD SHA.",
		worktreePath, branch)

	var commits []map[string]any
	for i, offender := range result.Offenders {
		if i == preventionDetailsCommitLimit {
			break
		}
		problems := make([]map[string]string, 0, len(offender.Findings))
		for _, finding := range offender.Findings {
			problems = append(problems, map[string]string{"problem": string(finding.Problem), "key": finding.Key})
		}
		commits = append(commits, map[string]any{"sha": offender.SHA, "subject": offender.Subject, "problems": problems})
	}
	return &PreconditionError{
		Reason: reason.String(),
		Details: map[string]any{
			"prevention_record_document": document,
			"fix_commits_checked":        result.Checked,
			"offending_commit_count":     len(result.Offenders),
			"offending_commits":          commits,
		},
	}
}

func truncateRunes(s string, limit int) string {
	if r := []rune(s); len(r) > limit {
		return string(r[:limit])
	}
	return s
}

// resolveRebaseBase pins the integration HEAD the rebase targets and checks the
// candidate's carriers against it.
func (s *submissionRun) resolveRebaseBase() error {
	s.integrationBranch = s.state.Config.IntegrationBranch
	rebaseBase, err := s.integrationHead(s.integrationBranch)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "resolve-integration-head",
			Message: "failed to resolve integration branch HEAD",
			Details: map[string]any{
				"operation":          integrationOperationSubmitForReview,
				"task_id":            s.taskID,
				"integration_branch": s.integrationBranch,
				"recovery_hint":      "Ensure the configured integration branch exists and is fetchable, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	s.rebaseBase = rebaseBase
	// Before publication, so correctable carrier content is refused while the
	// worktree is still unrebased; the rebased range is checked below. The range
	// starts at the merge-base, as the review boundary does: a worktree the
	// author already rebased must not claim integration's carriers as its own.
	candidateBase, err := s.git.GetMergeBase(s.preRebaseCommit, rebaseBase)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "candidate-merge-base",
			Message: "failed to resolve the candidate's merge-base with the integration branch",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"recovery_hint": "Inspect the task worktree and integration branch history, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	return validateReviewCarriers(s.git, s.projectRoot, s.taskID, candidateBase, s.preRebaseCommit, rebaseBase)
}

// loadAcceptance reads the declared acceptance contract at the pinned
// integration HEAD and prepares its receipt for the unrebased candidate.
func (s *submissionRun) loadAcceptance() error {
	acceptance, err := loadAcceptanceInput(s.projectRoot, s.state, s.task, s.rebaseBase)
	if err != nil {
		return err
	}
	s.acceptance = acceptance
	if _, err := prepareAcceptanceReceipt(s.projectRoot, s.task, acceptance, s.preRebaseCommit); err != nil {
		return err
	}
	if acceptance != nil {
		return checkAcceptanceWorktree(s.projectRoot, s.task.ID, s.preRebaseCommit)
	}
	return nil
}

// recordPreparation commits the preparation marker that fences this
// invocation's boundary before any effect, then marks the invocation as having
// effects.
func (s *submissionRun) recordPreparation() error {
	var preparation models.LifecyclePreparation
	if err := modifyLifecycleState(s.bb, s.authority, func(current *models.State) error {
		live := current.FindTask(s.taskID)
		if live == nil {
			return &errors.NotFoundError{Entity: notFoundEntityTask, ID: s.taskID}
		}
		if live.Status != s.expectedStatus || live.AssignedTo == nil || *live.AssignedTo != s.agentID {
			return &LifecycleError{Outcome: NewLifecycleOutcome(s.request.Operation, live, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone), Err: fmt.Errorf("submission ownership changed before preparation")}
		}
		if err := validateOutputArtifactRefScalars(s.taskID, live.Output); err != nil {
			return err
		}
		if err := rejectStaleDraftOutput(current, s.resolver, live); err != nil {
			return err
		}
		if err := checkPlanningOutputSnapshot(s.task, live); err != nil {
			return err
		}
		if err := PrepareLifecycleRequest(live, s.request, s.state.Agents); err != nil {
			return err
		}
		preparation = *live.Lifecycle.Preparation
		return nil
	}); err != nil {
		return err
	}
	s.invocation.preparation = &preparation
	s.invocation.effects = true
	return nil
}

// rebase rebases the task branch onto the pinned integration HEAD. A true merge
// conflict aborts the rebase, leaves the worktree intact and routes the task to
// INTEGRATION_FAILED; any other failure returns as is so the agent can retry
// without a state transition.
func (s *submissionRun) rebase() error {
	err := s.git.RebaseOnto(s.worktreePath, s.rebaseBase)
	if err == nil {
		return nil
	}
	// Abort rebase to restore clean worktree state — don't leave agents
	// in a mid-rebase state where they struggle with --continue/--abort.
	if abortErr := s.git.AbortRebase(s.worktreePath); abortErr != nil {
		log.Printf("WARNING: failed to abort rebase in %s: %v", s.worktreePath, abortErr)
	}

	// Only transition to INTEGRATION_FAILED for true merge conflicts.
	// Generic rebase failures (tool/env issues) are returned as-is so the
	// agent can retry without a state transition.
	var rebaseConflict *gitpkg.RebaseConflictError
	if !stderrors.As(err, &rebaseConflict) {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "rebase",
			Message: "rebase failed (not a merge conflict)",
			Details: rebaseFailureDetails(err, s.integrationBranch, s.rebaseBase, s.preRebaseCommit),
			Err:     err,
		}
	}

	// Transition to INTEGRATION_FAILED so the orchestrator re-queues the task.
	// This catches conflicts early (before review), avoiding a wasted review cycle.
	// See also: markIntegrationFailed in wt_merge.go (sibling for post-review merge path).
	markErr := markSubmitRebaseConflict(s.bb, s.taskID, s.agentID, s.authority, s.transitions, s.request)
	if markErr != nil {
		return &OperationalError{
			Code:    operationalCodeStateWrite,
			Phase:   "mark-integration-failed",
			Message: fmt.Sprintf("rebase conflict on %s: transition to INTEGRATION_FAILED also failed — worktree is intact (rebase aborted), check task state with %q before retrying", s.taskID, brand.Command("get", "tasks/"+s.taskID)),
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"recovery_hint": "Re-read task state, resolve the state transition failure, then retry or mark the task blocked with the captured failure.",
			},
			Err: markErr,
		}
	}
	return &IntegrationFailedError{Reason: IntegrationReasonMergeConflict}
}

// rebaseCandidate is entry rebase at the gate: the rebase, its conflict route,
// the rebased HEAD and the boundary the final transaction will write. The local
// runner substitutes read-only eligibility proofs (check_submission.go).
func (s *submissionRun) rebaseCandidate() error {
	if s.local != nil {
		return s.proveNoopRebase()
	}
	if err := s.rebase(); err != nil {
		return err
	}
	postRebaseCommit, err := s.git.GetWorktreeHEAD(s.taskID)
	if err != nil {
		return &OperationalError{
			Code:    operationalCodeGitOperation,
			Phase:   "post-rebase-head",
			Message: "failed to read worktree HEAD after rebase",
			Details: map[string]any{
				"operation":     integrationOperationSubmitForReview,
				"task_id":       s.taskID,
				"recovery_hint": "Inspect the task worktree git state, ensure HEAD resolves, then retry submit-for-review.",
			},
			Err: err,
		}
	}
	s.postRebaseCommit = postRebaseCommit
	s.fixBoundary()
	return nil
}

// fixBoundary records the boundary the final transaction will write; the state
// copy still contains the pre-submit claim base.
func (s *submissionRun) fixBoundary() {
	s.boundaryTask = *s.task
	s.boundaryTask.BaseCommit = &s.rebaseBase
}

// checkPublishedPlanningOutput is entry published_planning_output.
func (s *submissionRun) checkPublishedPlanningOutput() error {
	return validatePlanningOutputAcceptance(s.projectRoot, s.task, s.postRebaseCommit)
}

// checkPublishedOutputRefFragments is entry published_output_ref_fragments.
func (s *submissionRun) checkPublishedOutputRefFragments() error {
	return validateOutputRefFragments(s.projectRoot, s.task, s.postRebaseCommit)
}

// checkPublishedCarriers is entry published_carriers: the reviewer's carrier
// context over the range the submission publishes.
func (s *submissionRun) checkPublishedCarriers() error {
	return validateReviewCarriers(s.git, s.projectRoot, s.taskID, s.rebaseBase, s.postRebaseCommit, s.rebaseBase)
}

// checkPlanAmendment is entry plan_amendment.
func (s *submissionRun) checkPlanAmendment() error {
	if original := models.PendingPlanAmendment(s.state, s.task); original != "" {
		return &PreconditionError{Reason: "reviewed plan amendment pending for " + original}
	}
	return nil
}

// checkBoundedContractCorrection is entry bounded_contract_correction.
func (s *submissionRun) checkBoundedContractCorrection() error {
	return validateBoundedContractCorrection(s.projectRoot, s.state, s.state.FindTask(s.task.AmendsPlan), &s.boundaryTask, s.postRebaseCommit)
}

// checkReviewBoundary is entry review_boundary.
func (s *submissionRun) checkReviewBoundary() error {
	return validateReviewBoundaryCommit(s.projectRoot, &s.boundaryTask, s.postRebaseCommit, s.rebaseBase)
}

func taskRequiresTDD(task *models.Task, resolver models.PipelineResolver) (bool, error) {
	if task.RolePair != "" {
		doerRole, err := resolver.DoerRole(task.RolePair)
		if err != nil {
			return false, &PreconditionError{Reason: fmt.Sprintf("task %s has invalid role_pair %q: %v", task.ID, task.RolePair, err)}
		}
		return models.TaskTypeForRole(doerRole) == models.TaskTypeCoding, nil
	}
	return task.EffectiveType() == models.TaskTypeCoding, nil
}

func rebaseFailureDetails(err error, integrationBranch, rebaseBase, preRebaseCommit string) map[string]any {
	details := map[string]any{
		"rebase_base_ref":       integrationBranch,
		"rebase_base_commit":    rebaseBase,
		"integration_branch":    integrationBranch,
		"integration_head":      rebaseBase,
		"pre_rebase_head":       preRebaseCommit,
		"recovery_hint":         "Inspect the task worktree, resolve the git rebase failure or abort any stale rebase state, then retry submit-for-review.",
		"stdout_stderr_excerpt": truncateForDiagnostics(err.Error(), submitRebaseDiagnosticLimit),
	}

	var rebaseErr *gitpkg.RebaseError
	if stderrors.As(err, &rebaseErr) {
		details["command"] = strings.Join(rebaseErr.Command, " ")
		details["stdout_stderr_excerpt"] = truncateForDiagnostics(rebaseErr.Output, submitRebaseDiagnosticLimit)
	}
	return details
}

func truncateForDiagnostics(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	if limit <= 3 {
		return s[:limit]
	}
	return s[:limit-3] + "..."
}

// markSubmitRebaseConflict transitions a task from IMPLEMENTING (or pipeline executing
// state) to INTEGRATION_FAILED when a rebase conflict is detected during submission.
// Releases the agent so the orchestrator can re-assign a coder for conflict resolution.
//
// Sibling: markIntegrationFailed in wt_merge.go handles the post-review merge path.
// Both share the pattern: transition → append FailedBy → write history entry.
// They differ in pre-conditions (approved vs implementing) and post-actions (agent release).
func markSubmitRebaseConflict(bb *db.Blackboard, taskID, agentID string, authority *models.AgentAuthority, pipelineTransitions map[models.TaskStatus][]models.TaskStatus, request LifecycleRequest) error {
	reason := string(IntegrationReasonMergeConflict)
	return modifyLifecycleState(bb, authority, func(s *models.State) error {
		t := s.FindTask(taskID)
		if t == nil {
			return &errors.NotFoundError{Entity: notFoundEntityTask, ID: taskID}
		}
		if err := ValidateLifecyclePreparation(t, request); err != nil {
			return err
		}
		if err := t.TransitionWith(models.TaskStatusIntegrationFailed, pipelineTransitions); err != nil {
			return err
		}
		t.FailedBy = appendUniqueAgentID(t.FailedBy, agentID)
		t.IntegrationFix = false
		t.AssignedTo = nil
		t.LeaseExpires = nil
		models.AdvanceLifecycle(t)

		now := time.Now().UTC()
		diagnostic := map[string]any{
			"operation":     integrationOperationSubmitForReview,
			"reason":        reason,
			"recovery_hint": "resolve the submit-time rebase conflict in the task worktree, then resubmit for review",
		}
		entry := models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventIntegrationFailed,
			Agent:  &agentID,
			Reason: &reason,
			Extra: map[string]any{
				"diagnostic": diagnostic,
			},
		}
		t.IntegrationFailure = cloneMapForTaskDiagnostic(diagnostic)
		t.History = append(t.History, entry)
		if _, err := blockTaskForHypothesisExhaustion(s, t, agentID, pipelineTransitions, now); err != nil {
			return err
		}
		t.HandoffEvents = append(t.HandoffEvents, models.HandoffEvent{
			Timestamp: now,
			Agent:     agentID,
			Trigger:   models.HandoffTriggerSubmission,
			Failed:    []string{reason},
			NextStep:  "resolve integration conflict and resubmit",
		})

		// Release the agent so it can pick up other work
		if agent, ok := s.Agents[agentID]; ok {
			agent.Status = models.AgentStatusWaiting
			agent.CurrentTask = nil
			agent.LeaseExpires = nil
			s.Agents[agentID] = agent
		}

		return nil
	})
}
