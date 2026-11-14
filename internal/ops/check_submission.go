package ops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// Verdicts of a local submission check (ADR-0252).
const (
	SubmissionVerdictPass       = "pass"
	SubmissionVerdictRefused    = "refused"
	SubmissionVerdictIncomplete = "incomplete"
)

// Statuses of one check in a local submission check.
const (
	SubmissionCheckPassed        = "passed"
	SubmissionCheckRefused       = "refused"
	SubmissionCheckNotRun        = "not_run"
	SubmissionCheckNotApplicable = "not_applicable"
)

// Reasons a check was not run. A reason that names a detail carries it after a
// colon: rebase_unverified:<condition>, single_use_input:<command index>.
const (
	NotRunAfterRefusal       = "after_refusal"
	NotRunRebasePending      = "rebase_pending"
	NotRunRebaseUnverified   = "rebase_unverified"
	NotRunSingleUseInput     = "single_use_input"
	NotRunAdmissionFull      = "admission_full"
	rebaseConditionTracked   = "tracked_changes"
	rebaseConditionOperation = "operation_in_progress"
	rebaseConditionHook      = "pre_rebase_hook"
)

// The submission steps check-submission reports as not applicable: they have
// no candidate verdict, only effects or races of the gate (ADR-0252).
const (
	notApplicableSubmitHold        = "submit_hold"
	notApplicableLifecycleRequest  = "lifecycle_request"
	notApplicablePreparation       = "preparation"
	notApplicableIndexRefresh      = "index_refresh"
	notApplicablePublicationGuards = "publication_guards"
)

// operationalCodeLocalCheckChanged is the OperationalError.Code of a local
// check that found the worktree's branch or HEAD changed by the check itself.
const operationalCodeLocalCheckChanged = "worktree_changed"

// CheckSubmissionInput names the task, the agent the gate is evaluated for and
// the local slot wait.
type CheckSubmissionInput struct {
	TaskID  string
	AgentID string
	// Authority is set for an agent caller; an operator naming the agent
	// leaves it nil and needs none, the check being read-only.
	Authority *models.AgentAuthority
	// MaxWait bounds the local-tier admission slot wait.
	MaxWait time.Duration
}

// SubmissionCheckResult is one line of a local submission check.
type SubmissionCheckResult struct {
	Name   string `json:"name"`
	Stage  string `json:"stage"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// CheckSubmissionResult is the local verdict. Refusal is the error
// submit-for-review would return for the first refusing check, classified as
// the gate classifies it with no effect; the command renders it.
type CheckSubmissionResult struct {
	TaskID           string                             `json:"task_id"`
	AgentID          string                             `json:"agent_id"`
	CandidateCommit  string                             `json:"candidate_commit"`
	IntegrationHead  string                             `json:"integration_head"`
	Verdict          string                             `json:"verdict"`
	Checks           []SubmissionCheckResult            `json:"checks"`
	Refusal          error                              `json:"-"`
	DeferredCommands []models.AcceptanceDeferredCommand `json:"deferred_commands,omitempty"`
	Warnings         []string                           `json:"warnings,omitempty"`
}

// localSubmission is what makes a submissionRun a local one: the gate's checks
// run in their read-only modes and the effects stay out (ADR-0252).
type localSubmission struct {
	maxWait         time.Duration
	integrationHead string
}

// submissionIncomplete is a check the local run cannot run exactly as the gate
// would: the verdict is incomplete, never pass (SC3).
type submissionIncomplete struct{ reason string }

func (e *submissionIncomplete) Error() string { return "submission check not run: " + e.reason }

// integrationHead resolves the integration branch HEAD. The local run resolves
// it once, so every check sees the same commit.
func (s *submissionRun) integrationHead(branch string) (string, error) {
	if s.local != nil && s.local.integrationHead != "" {
		return s.local.integrationHead, nil
	}
	head, err := s.git.GetCommitSHA(branch)
	if err == nil && s.local != nil {
		s.local.integrationHead = head
	}
	return head, err
}

// submissionListing is one line of the local report: a check of the list, or a
// step of the gate that has no candidate verdict.
type submissionListing struct {
	name  string
	stage submissionStage
	entry *submissionCheck
}

// submissionListings places each not-applicable step where the gate takes it.
func submissionListings() []submissionListing {
	notApplicable := func(name string, stage submissionStage) submissionListing {
		return submissionListing{name: name, stage: stage}
	}
	after := map[string]submissionListing{
		checkAgentAuthority:                            notApplicable(notApplicableLifecycleRequest, stageSnapshot),
		checkAcceptanceContract:                        notApplicable(notApplicablePreparation, stageRebase),
		checkReviewBoundaryName:                        notApplicable(notApplicableIndexRefresh, stageCanonical),
		submissionChecks[len(submissionChecks)-1].name: notApplicable(notApplicablePublicationGuards, stageCanonical),
	}
	listings := []submissionListing{notApplicable(notApplicableSubmitHold, stageSnapshot)}
	for i := range submissionChecks {
		entry := &submissionChecks[i]
		listings = append(listings, submissionListing{name: entry.name, stage: entry.stage, entry: entry})
		if extra, ok := after[entry.name]; ok {
			listings = append(listings, extra)
		}
	}
	return listings
}

// CheckSubmission evaluates the submission check list for a task and an agent
// without changing state: the gate's checks, in the gate's order, in their
// read-only modes. The one write is a failing command's masked output, which
// the shared canonical_validation keeps in the tool-result store exactly as the
// gate does. The candidate is the task worktree's HEAD; the rebase is never
// performed, only proven a no-op (ADR-0252).
func CheckSubmission(projectRoot string, in CheckSubmissionInput) (*CheckSubmissionResult, error) {
	if in.TaskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if in.AgentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	if in.MaxWait < 0 || in.MaxWait > AdmitMaxWait {
		return nil, &PreconditionError{Reason: fmt.Sprintf("max wait must be between 0 and %d seconds", int(AdmitMaxWait/time.Second))}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	s := &submissionRun{
		projectRoot: projectRoot, taskID: in.TaskID, agentID: in.AgentID, authority: in.Authority,
		invocation: &submissionInvocation{}, bb: bb, readState: bb.ReadSnapshot,
		local: &localSubmission{maxWait: in.MaxWait},
	}
	result := &CheckSubmissionResult{TaskID: in.TaskID, AgentID: in.AgentID}
	refused, cause := false, ""
	for _, listing := range submissionListings() {
		line := SubmissionCheckResult{Name: listing.name, Stage: string(listing.stage)}
		switch {
		case listing.entry == nil:
			line.Status = SubmissionCheckNotApplicable
		case refused:
			line.Status, line.Reason = SubmissionCheckNotRun, NotRunAfterRefusal
		case cause != "":
			line.Status, line.Reason = SubmissionCheckNotRun, cause
		default:
			err := listing.entry.run(s)
			if err == nil && listing.name == checkAcceptanceContract {
				err = s.planAdmission()
			}
			if err == nil && listing.name == checkCanonicalValidation && s.receipt != nil {
				result.DeferredCommands = s.receipt.Deferred
			}
			if reason, incomplete := notRunReason(err); incomplete {
				line.Status, line.Reason, cause = SubmissionCheckNotRun, reason, reason
				break
			}
			if err != nil {
				if submissionCommandFailure(err) {
					return nil, err
				}
				line.Status, refused = SubmissionCheckRefused, true
				result.Refusal = classifyLocalRefusal(s.observedTask(), listing.name, err)
				break
			}
			line.Status = SubmissionCheckPassed
		}
		result.Checks = append(result.Checks, line)
	}
	if err := s.verifyUnchanged(); err != nil {
		return nil, err
	}
	result.CandidateCommit, result.IntegrationHead = s.preRebaseCommit, s.rebaseBase
	result.Verdict = SubmissionVerdictPass
	switch {
	case refused:
		result.Verdict = SubmissionVerdictRefused
	case cause != "":
		result.Verdict = SubmissionVerdictIncomplete
	}
	return result, nil
}

// classifyLocalRefusal gives a refusal by the named entry the lifecycle the
// local run can truthfully report. Before the gate's preparation marker (entries
// 1-11) that is the gate's own policy with no effect. After it (entries 13-19)
// the gate reports STATE_CHANGED, requery and unknown effects because its marker
// was written; the local run wrote none and changed nothing, so it reports
// INVALID_INPUT, correct_input and no effects, whatever the cause's type, with
// the same reason (ADR-0252, D3).
func classifyLocalRefusal(task *models.Task, entry string, err error) error {
	if submissionChecks[submissionCheckIndex(entry)].afterMarker() {
		// WrapLifecycleError keeps a lifecycle the cause already carries, such as
		// the ownership fence's STATE_CHANGED / requery / unknown, which describes
		// a gate run with a marker. Drop those wrappers; the refusal keeps the
		// underlying cause and its reason.
		for lifecycle := (*LifecycleError)(nil); errors.As(err, &lifecycle) && lifecycle.Err != nil; {
			err = lifecycle.Err
		}
		return WrapLifecycleError(integrationOperationSubmitForReview, task, err, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}
	return classifySubmissionError(task, err, false)
}

// notRunReason reports whether err is a check the local run could not run
// exactly, and why.
func notRunReason(err error) (string, bool) {
	var incomplete *submissionIncomplete
	if errors.As(err, &incomplete) {
		return incomplete.reason, true
	}
	var stop *acceptanceInspectStop
	if errors.As(err, &stop) {
		return NotRunSingleUseInput + ":" + strconv.Itoa(stop.Index), true
	}
	var evidence *AcceptanceEvidenceError
	if errors.As(err, &evidence) && evidence.Class == AcceptanceFaultAdmissionSlot {
		return NotRunAdmissionFull, true
	}
	return "", false
}

// submissionCommandFailure reports an error that is the command's own failure,
// not the candidate's refusal: an operational failure, an unknown task or an
// error no check refuses with.
func submissionCommandFailure(err error) bool {
	var (
		operational *OperationalError
		notFound    *lizaerrors.NotFoundError
		precondtion *PreconditionError
		evidence    *AcceptanceEvidenceError
		worktree    *lizaerrors.WorktreeContextError
		lifecycle   *LifecycleError
	)
	switch {
	case errors.As(err, &operational), errors.As(err, &notFound):
		return true
	case errors.As(err, &precondtion), errors.As(err, &evidence), errors.As(err, &worktree), errors.As(err, &lifecycle), IsAgentAuthorityError(err):
		return false
	}
	return true
}

// observedTask is the committed task a refusal's outcome describes, read
// without a lock and only for a caller whose authority still holds.
func (s *submissionRun) observedTask() *models.Task {
	state, err := s.bb.ReadSnapshot()
	if err != nil || (s.authority != nil && RequireAgentAuthority(state, *s.authority) != nil) {
		return nil
	}
	return state.FindTask(s.taskID)
}

// verifyUnchanged re-reads the worktree's branch and HEAD: the local run
// promises to leave both as it found them.
func (s *submissionRun) verifyUnchanged() error {
	if s.git == nil || s.preRebaseCommit == "" {
		return nil
	}
	branch, branchErr := s.git.GetWorktreeBranch(s.worktreePath)
	head, headErr := s.git.GetWorktreeHEAD(s.taskID)
	if branchErr != nil || headErr != nil || branch != s.expectedBranch || head != s.preRebaseCommit {
		return &OperationalError{
			Code:    operationalCodeLocalCheckChanged,
			Phase:   "verify-worktree-unchanged",
			Message: "the worktree's branch or HEAD changed during the local submission check",
			Details: map[string]any{
				"operation":     integrationOperationCheckSubmission,
				"task_id":       s.taskID,
				"recovery_hint": "Inspect the task worktree: a canonical command changed its branch or HEAD, which the gate would refuse.",
			},
		}
	}
	return nil
}

// integrationOperationCheckSubmission labels the local check in diagnostics.
const integrationOperationCheckSubmission = "check-submission"

// proveNoopRebase is entry rebase in the local run (D1). The local run never
// rebases: it proves, read-only, that the gate's rebase would publish HEAD
// itself (C1) and would succeed (C2-C4). Otherwise the check is incomplete.
func (s *submissionRun) proveNoopRebase() error {
	ancestor, err := s.git.IsAncestor(s.rebaseBase, s.preRebaseCommit)
	if err != nil {
		return s.rebaseProbeFailure(err)
	}
	if !ancestor {
		return &submissionIncomplete{reason: NotRunRebasePending}
	}
	merges, err := s.probe("rev-list", "--merges", "--count", s.rebaseBase+".."+s.preRebaseCommit)
	if err != nil {
		return s.rebaseProbeFailure(err)
	}
	if strings.TrimSpace(merges) != "0" {
		return &submissionIncomplete{reason: NotRunRebasePending}
	}
	tracked, err := s.probe("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return s.rebaseProbeFailure(err)
	}
	if strings.TrimSpace(tracked) != "" {
		return &submissionIncomplete{reason: NotRunRebaseUnverified + ":" + rebaseConditionTracked}
	}
	inProgress, err := s.operationInProgress()
	if err != nil {
		return s.rebaseProbeFailure(err)
	}
	if inProgress {
		return &submissionIncomplete{reason: NotRunRebaseUnverified + ":" + rebaseConditionOperation}
	}
	hook, err := s.preRebaseHookPresent()
	if err != nil {
		return s.rebaseProbeFailure(err)
	}
	if hook {
		return &submissionIncomplete{reason: NotRunRebaseUnverified + ":" + rebaseConditionHook}
	}
	s.postRebaseCommit = s.preRebaseCommit
	s.fixBoundary()
	return nil
}

// probe runs one read-only git query in the task worktree.
func (s *submissionRun) probe(args ...string) (string, error) {
	output, err := gitenv.Output(s.worktreePath, args...)
	return string(output), err
}

// gitPath resolves a path inside the worktree's git directory.
func (s *submissionRun) gitPath(name string) (string, error) {
	output, err := s.probe("rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(output)
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.worktreePath, path)
	}
	return path, nil
}

// operationInProgress reports a merge, cherry-pick, revert, rebase or am state
// in the worktree's git directory.
func (s *submissionRun) operationInProgress() (bool, error) {
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"} {
		path, err := s.gitPath(name)
		if err != nil {
			return false, err
		}
		if _, err := os.Lstat(path); err == nil {
			return true, nil
		}
	}
	return false, nil
}

// preRebaseHookPresent reports an executable pre-rebase hook in the effective
// hooks directory. Git resolves core.hooksPath itself (relative, home-relative
// or absolute), so its answer is used as is.
func (s *submissionRun) preRebaseHookPresent() (bool, error) {
	dir, err := s.gitPath("hooks")
	if err != nil {
		return false, err
	}
	info, err := os.Stat(filepath.Join(dir, "pre-rebase"))
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0, nil
}

// rebaseProbeFailure reports a git failure of the read-only rebase proof; git's
// text goes to stderr only, as the gate's git checks do.
func (s *submissionRun) rebaseProbeFailure(err error) error {
	fmt.Fprintf(os.Stderr, "WARNING: task %s: rebase eligibility check failed: %v\n", s.taskID, err)
	return &OperationalError{
		Code:    operationalCodeGitOperation,
		Phase:   "check-rebase-eligibility",
		Message: "failed to prove the submission's rebase is a no-op",
		Details: map[string]any{
			"operation":     integrationOperationCheckSubmission,
			"task_id":       s.taskID,
			"recovery_hint": "Inspect the task worktree git state, then rerun check-submission.",
		},
		Err: err,
	}
}
