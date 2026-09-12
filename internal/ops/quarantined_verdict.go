package ops

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statehygiene"
	"github.com/liza-mas/liza/internal/statevalidate"
)

var errVerdictEvidenceUnchanged = errors.New("verdict evidence unchanged")

// withTaskReviewLock orders evidence capture, approval, reconciliation and the
// entire merge/finalization across processes. Acquire only at public entries:
// task review -> integration completion -> integration mutation -> blackboard
// read. Never acquire agent/project lifecycle locks here, nor write blackboard
// state while holding integration mutation. Private approval recursion retains
// the outer review lock instead of acquiring it again.
func withTaskReviewLock(projectRoot, taskID, operation string, fn func() error) error {
	if strings.TrimSpace(taskID) == "" {
		return &PreconditionError{Reason: "task ID is required"}
	}
	key := fmt.Sprintf("task-review-%x", sha256.Sum256([]byte(taskID)))
	lock, err := projectFileLock(projectRoot, key)
	if errors.Is(err, os.ErrNotExist) {
		// Focused non-Git callers still require cross-process serialization.
		root, absErr := filepath.Abs(projectRoot)
		if absErr != nil {
			return absErr
		}
		lock = filelock.New(filepath.Join(root, strings.TrimPrefix(paths.ProjectDirName(), ".")+"-"+key))
	} else if err != nil {
		return err
	}
	err = lock.WithTimeout(filelock.DefaultLockTimeout).WithLockOperation(operation, fn)
	if filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		return fmt.Errorf("task %s review operation is busy; retry %s (no evidence-save guarantee on timeout): %w", taskID, operation, err)
	}
	return err
}

// QuarantinedVerdictError still rejects the caller's authority, while identifying
// the durable evidence. Neither diagnostics nor provenance expose credentials.
type QuarantinedVerdictError struct {
	Authority *AgentAuthorityError
	FindingID string
	Matched   bool
	PostMerge bool
}

func (e *QuarantinedVerdictError) Error() string {
	message := fmt.Sprintf("%v; evidence saved as quarantined verdict %s; an authorized orchestrator can use %s to record a judgment", e.Authority, e.FindingID, brand.Command("reconcile-verdict"))
	if !e.Matched {
		message += "; unmatched review boundary: retained for diagnosis, does not gate merges"
	}
	if e.PostMerge {
		message += "; task already merged: inspect the finding and route corrective work; this evidence does not reopen or roll back the task"
	}
	return message
}

func (e *QuarantinedVerdictError) Unwrap() error { return e.Authority }

func (e *QuarantinedVerdictError) SafeDetails() map[string]any {
	details := e.Authority.SafeDetails()
	details["quarantined_verdict_id"] = e.FindingID
	details["matched"] = e.Matched
	details["post_merge"] = e.PostMerge
	return details
}

func validateAuthenticatedVerdict(taskID, verdict, reason, impact, reviewCommit string, authority models.AgentAuthority) error {
	if err := validateVerdictInput(taskID, verdict, reason, authority.ID, impact); err != nil {
		return err
	}
	if !models.IsFullReviewCommit(reviewCommit) {
		return &PreconditionError{Reason: "review-commit must be the full immutable commit SHA actually reviewed"}
	}
	if err := validateVerdictReasonSize(reason); err != nil {
		return err
	}
	if !utf8.ValidString(reason) {
		return &PreconditionError{Reason: "verdict reason must be valid UTF-8"}
	}
	return nil
}

func validateVerdictReasonSize(reason string) error {
	if len(reason) <= statehygiene.MaxStateTextBytes {
		return nil
	}
	return &PreconditionError{Reason: fmt.Sprintf(
		"verdict reason is %d bytes, exceeds the %d-byte maximum; store raw evidence under %s/%s/ and submit a bounded summary with an artifact reference",
		len(reason), statehygiene.MaxStateTextBytes, paths.ProjectDirName(), paths.AgentOutputsDirName,
	)}
}

// sanitizeVerdictReason also masks short fixture/legacy generations that the
// generic environment masker intentionally ignores. Longest-first replacement
// prevents one generation from exposing a suffix of another.
func sanitizeVerdictReason(reason string, state *models.State, authority models.AgentAuthority) string {
	values := []string{authority.Generation}
	for _, agent := range state.Agents {
		values = append(values, agent.Generation)
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, value := range values {
		if value != "" {
			reason = strings.ReplaceAll(reason, value, "***")
		}
	}
	reason = strings.TrimSpace(secretmask.New().MaskText(reason))
	if len(reason) > statehygiene.MaxStateTextBytes {
		end := statehygiene.MaxStateTextBytes - len("...")
		for !utf8.RuneStart(reason[end]) {
			end--
		}
		reason = reason[:end] + "..."
	}
	return reason
}

// quarantineFencedVerdict is the sole evidence-only exception to generation
// fencing. Called under the task review lock, it changes only the evidence
// slice, never tasks, leases, approvals, agents, or anomalies.
func quarantineFencedVerdict(bb *db.Blackboard, taskID, verdict, reason, reviewCommit string, authority models.AgentAuthority) error {
	var rejection *QuarantinedVerdictError
	err := bb.Modify(func(state *models.State) error {
		err := RequireAgentAuthority(state, authority)
		var fenced *AgentAuthorityError
		if !errors.As(err, &fenced) || authority.Generation == "" {
			if err != nil {
				return err
			}
			return &PreconditionError{Reason: "registration changed during verdict capture; retry submission"}
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s not found; no verdict evidence saved", taskID)}
		}
		if err := statevalidate.ValidateQuarantinedVerdicts(state); err != nil {
			return err
		}
		safeReason := sanitizeVerdictReason(reason, state, authority)
		key, _ := json.Marshal([]string{taskID, reviewCommit, authority.ID, verdict, safeReason})
		id := fmt.Sprintf("qv-%x", sha256.Sum256(key))
		fingerprint := generationFingerprint(authority.Generation)
		rejection = &QuarantinedVerdictError{Authority: fenced, FindingID: id, PostMerge: task.Status == models.TaskStatusMerged}
		for i := range state.QuarantinedVerdicts {
			finding := &state.QuarantinedVerdicts[i]
			if finding.ID != id {
				continue
			}
			rejection.Matched = finding.Matched
			if slices.Contains(finding.GenerationFingerprints, fingerprint) {
				return errVerdictEvidenceUnchanged
			}
			finding.GenerationFingerprints = append(finding.GenerationFingerprints, fingerprint)
			return nil
		}
		rejection.Matched = taskHasReviewBoundary(task, reviewCommit)
		state.QuarantinedVerdicts = append(state.QuarantinedVerdicts, models.QuarantinedVerdict{
			ID: id, TaskID: taskID, ReviewCommit: reviewCommit, ReviewerID: authority.ID,
			Verdict: verdict, Reason: safeReason, Timestamp: time.Now().UTC(),
			GenerationFingerprints: []string{fingerprint}, Matched: rejection.Matched,
		})
		return nil
	})
	if err != nil && !errors.Is(err, errVerdictEvidenceUnchanged) {
		return fmt.Errorf("quarantined verdict was not saved: %w", err)
	}
	if rejection == nil {
		return fmt.Errorf("quarantined verdict was not saved: no authority rejection captured")
	}
	return rejection
}

func taskHasReviewBoundary(task *models.Task, commit string) bool {
	if task.ReviewCommit != nil && strings.EqualFold(*task.ReviewCommit, commit) {
		return true
	}
	for _, entry := range task.History {
		switch entry.Event {
		case models.TaskEventSubmittedForReview, models.TaskEventApproved, models.TaskEventRejected,
			models.TaskEventReviewVerdictApproved, models.TaskEventReviewVerdictRejected, models.TaskEventReviewCommitUpdated:
			if entry.Commit != nil && strings.EqualFold(*entry.Commit, commit) {
				return true
			}
			if entry.Event == models.TaskEventReviewCommitUpdated && strings.EqualFold(reviewCommitValue(entry.Extra["old_review_commit"]), commit) {
				return true
			}
		}
	}
	return false
}

func reviewCommitValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.ToLower(v)
	case *string:
		if v != nil {
			return strings.ToLower(*v)
		}
	}
	return ""
}

// reviewLineage follows only explicit, chronologically recorded rebase edges.
// A malformed or branching reachable edge cannot silently release a hold.
func reviewLineage(task *models.Task, from, to string) (related, ambiguous bool) {
	from, to = strings.ToLower(from), strings.ToLower(to)
	reached := map[string]bool{from: true}
	successor := map[string]string{}
	edges := map[string][]string{}
	for _, entry := range task.History {
		if entry.Event != models.TaskEventReviewCommitUpdated {
			continue
		}
		old := reviewCommitValue(entry.Extra["old_review_commit"])
		next := reviewCommitValue(entry.Extra["new_review_commit"])
		if models.IsFullReviewCommit(old) && models.IsFullReviewCommit(next) {
			edges[old] = append(edges[old], next)
		}
		if !reached[old] {
			continue
		}
		if !models.IsFullReviewCommit(next) || (successor[old] != "" && successor[old] != next) ||
			(entry.Commit != nil && !strings.EqualFold(*entry.Commit, next)) {
			ambiguous = true
			continue
		}
		successor[old] = next
		reached[next] = true
	}
	// A graph path whose edges cannot be followed in append order is ambiguous,
	// not evidence of an unrelated resubmission. Preserve its hold for judgment.
	if !reached[to] {
		seen := map[string]bool{from: true}
		pending := []string{from}
		for len(pending) > 0 {
			current := pending[0]
			pending = pending[1:]
			for _, next := range edges[current] {
				if !seen[next] {
					seen[next] = true
					pending = append(pending, next)
				}
			}
		}
		ambiguous = ambiguous || seen[to]
	}
	return reached[to], ambiguous
}

// requireReconciledVerdicts guards approval and every merge entry, including
// the already-ancestor recovery path. Rejection remains safe to apply normally.
func requireReconciledVerdicts(state *models.State, task *models.Task, commit string) error {
	if err := statevalidate.ValidateQuarantinedVerdicts(state); err != nil {
		return fmt.Errorf("cannot evaluate quarantined verdict holds: %w", err)
	}
	for _, finding := range state.QuarantinedVerdicts {
		if finding.TaskID != task.ID || !finding.Matched {
			continue
		}
		if n := len(finding.Reconciliations); n > 0 {
			disposition := finding.Reconciliations[n-1].Disposition
			if disposition == "refuted" || disposition == "superseded" {
				continue
			}
		}
		related, ambiguous := reviewLineage(task, finding.ReviewCommit, commit)
		if !related && !ambiguous {
			continue
		}
		conflict := finding.Verdict == "REJECTED"
		for _, entry := range task.History {
			if entry.Event != models.TaskEventRejected || entry.Commit == nil {
				continue
			}
			forward, forwardAmbiguous := reviewLineage(task, finding.ReviewCommit, *entry.Commit)
			backward, backwardAmbiguous := reviewLineage(task, *entry.Commit, finding.ReviewCommit)
			conflict = conflict || forward || backward
			ambiguous = ambiguous || forwardAmbiguous || backwardAmbiguous
		}
		if conflict || ambiguous {
			return &PreconditionError{Reason: fmt.Sprintf("task %s review commit %s is held by quarantined verdict %s; an authorized orchestrator must use %s with a reason before approval or merge (rebase alone does not resolve evidence)", task.ID, commit, finding.ID, brand.Command("reconcile-verdict"))}
		}
	}
	return nil
}

func validateVerdictReviewBoundary(state *models.State, task *models.Task, verdict, reviewedCommit string) error {
	if reviewedCommit != "" && (task.ReviewCommit == nil || !strings.EqualFold(*task.ReviewCommit, reviewedCommit)) {
		return &PreconditionError{Reason: "review-commit does not match the current task review boundary; re-read and review the submitted commit"}
	}
	if verdict == "APPROVED" && task.ReviewCommit != nil {
		return requireReconciledVerdicts(state, task, *task.ReviewCommit)
	}
	return nil
}

// ReconcileVerdict records an authenticated orchestrator judgment. It never
// supplies an approval, releases an agent, or changes the task's status.
func ReconcileVerdict(projectRoot, taskID, findingID, disposition, reason string, authority models.AgentAuthority) error {
	if !models.IsVerdictDisposition(disposition) || strings.TrimSpace(reason) == "" ||
		len(reason) > statehygiene.MaxStateTextBytes || !utf8.ValidString(reason) {
		return &PreconditionError{Reason: "reconciliation requires a supported disposition and a nonempty UTF-8 reason of at most 4096 bytes"}
	}
	return withTaskReviewLock(projectRoot, taskID, "reconcile-verdict", func() error {
		resolver, _, err := loadResolver(projectRoot)
		if err != nil {
			return err
		}
		err = db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
			if err := RequireAgentAuthority(state, authority); err != nil {
				return err
			}
			capabilities, err := resolver.EffectiveRoleCapabilities(state.Agents[authority.ID].Role)
			if err != nil || capabilities.RoleType != "orchestrator" || !slices.Contains(capabilities.AllowedOperations, "reconcile-verdict") {
				return &PreconditionError{Reason: "reconcile-verdict requires a registered orchestrator with the reconcile-verdict capability"}
			}
			if state.FindTask(taskID) == nil {
				return &PreconditionError{Reason: fmt.Sprintf("task %s not found", taskID)}
			}
			if err := statevalidate.ValidateQuarantinedVerdicts(state); err != nil {
				return err
			}
			safeReason := sanitizeVerdictReason(reason, state, authority)
			for i := range state.QuarantinedVerdicts {
				finding := &state.QuarantinedVerdicts[i]
				if finding.ID != findingID || finding.TaskID != taskID {
					continue
				}
				if n := len(finding.Reconciliations); n > 0 {
					last := finding.Reconciliations[n-1]
					if last.Actor == authority.ID && last.Disposition == disposition && last.Reason == safeReason {
						return errVerdictEvidenceUnchanged
					}
				}
				finding.Reconciliations = append(finding.Reconciliations, models.VerdictReconciliation{
					Actor: authority.ID, Timestamp: time.Now().UTC(), Disposition: disposition, Reason: safeReason,
				})
				return nil
			}
			return &PreconditionError{Reason: fmt.Sprintf("quarantined verdict %s not found for task %s", findingID, taskID)}
		})
		if errors.Is(err, errVerdictEvidenceUnchanged) {
			return nil
		}
		return err
	})
}
