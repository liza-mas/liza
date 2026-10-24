package statevalidate

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// statusClassifier resolves whether a TaskStatus belongs to a given lifecycle
// phase using pipeline-declared statuses. Built once and shared by
// validateTaskInvariants and validateDependencies.
type statusClassifier struct {
	executing []models.TaskStatus
	initial   []models.TaskStatus
	submitted []models.TaskStatus
	reviewing []models.TaskStatus
	approved  []models.TaskStatus
	partial   []models.TaskStatus
	rejected  []models.TaskStatus
}

// newStatusClassifier constructs a statusClassifier from a pipeline resolver
// and configuration. Returns an empty classifier when resolver or config is nil.
func newStatusClassifier(resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) statusClassifier {
	sc := statusClassifier{}
	if resolver == nil || cfg == nil {
		return sc
	}
	for rpName := range cfg.Pipeline.RolePairs {
		if s, err := resolver.ExecutingStatus(rpName); err == nil {
			sc.executing = append(sc.executing, s)
		}
		if s, err := resolver.InitialStatus(rpName); err == nil {
			sc.initial = append(sc.initial, s)
		}
		if s, err := resolver.SubmittedStatus(rpName); err == nil {
			sc.submitted = append(sc.submitted, s)
		}
		if s, err := resolver.ReviewingStatus(rpName); err == nil {
			sc.reviewing = append(sc.reviewing, s)
		}
		// reviewing-2 reuses the review lease mechanism — classify as reviewing.
		if s, err := resolver.Reviewing2Status(rpName); err == nil {
			sc.reviewing = append(sc.reviewing, s)
		}
		if s, err := resolver.ApprovedStatus(rpName); err == nil {
			sc.approved = append(sc.approved, s)
		}
		if s, err := resolver.PartiallyApprovedStatus(rpName); err == nil {
			sc.partial = append(sc.partial, s)
		}
		if s, err := resolver.RejectedStatus(rpName); err == nil {
			sc.rejected = append(sc.rejected, s)
		}
	}
	return sc
}

// containsStatus returns true if the given status appears in the list.
// Used by statusClassifier methods to check pipeline-declared statuses.
func containsStatus(list []models.TaskStatus, s models.TaskStatus) bool {
	for _, v := range list {
		if s == v {
			return true
		}
	}
	return false
}

func (sc *statusClassifier) IsExecuting(s models.TaskStatus) bool {
	return containsStatus(sc.executing, s)
}

func (sc *statusClassifier) IsInitial(s models.TaskStatus) bool {
	return containsStatus(sc.initial, s)
}

func (sc *statusClassifier) IsSubmitted(s models.TaskStatus) bool {
	return containsStatus(sc.submitted, s)
}

func (sc *statusClassifier) IsReviewing(s models.TaskStatus) bool {
	return containsStatus(sc.reviewing, s)
}

func (sc *statusClassifier) IsApproved(s models.TaskStatus) bool {
	return containsStatus(sc.approved, s)
}

func (sc *statusClassifier) IsPartiallyApproved(s models.TaskStatus) bool {
	return containsStatus(sc.partial, s)
}

func (sc *statusClassifier) IsRejected(s models.TaskStatus) bool {
	return s == models.TaskStatusRejected || s == models.TaskStatusCodingPlanRejected || containsStatus(sc.rejected, s)
}

// missingStatusField reports a field a lifecycle phase requires. Messages name
// the status for the operator. Identities name the phase and the field
// instead, so a task moving between statuses of the same phase keeps its
// violation rather than trading an old one for a new one.
func missingStatusField(v *violations, task *models.Task, phase, field string) {
	v.addID(fmt.Sprintf("task %s: %s task without %s", task.ID, phase, field),
		fmt.Errorf("%s task without %s: %s", task.Status, field, task.ID))
}

func validateInitialStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if sc.IsInitial(task.Status) && task.AssignedTo != nil {
		v.addID(fmt.Sprintf("task %s: initial task with assigned_to", task.ID),
			fmt.Errorf("%s task with assigned_to: %s", task.Status, task.ID))
	}
}

func validateExecutingStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if !sc.IsExecuting(task.Status) {
		return
	}
	if task.AssignedTo == nil {
		missingStatusField(v, task, "executing", "assigned_to")
	}
	if task.Worktree == nil {
		missingStatusField(v, task, "executing", "worktree")
	}
	if !task.IntegrationFix && task.BaseCommit == nil {
		missingStatusField(v, task, "executing", "base_commit")
	}
	if task.LeaseExpires == nil {
		missingStatusField(v, task, "executing", "lease_expires")
	}
}

func validateSubmittedStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if sc.IsSubmitted(task.Status) && task.ReviewCommit == nil {
		missingStatusField(v, task, "submitted", "review_commit")
	}
}

func validateReviewingStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if !sc.IsReviewing(task.Status) {
		return
	}
	if task.ReviewingBy == nil {
		missingStatusField(v, task, "reviewing", "reviewing_by")
	}
	if task.ReviewLeaseExpires == nil {
		missingStatusField(v, task, "reviewing", "review_lease_expires")
	}
	if task.ReviewCommit == nil {
		missingStatusField(v, task, "reviewing", "review_commit")
	}
}

func validateApprovedStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if sc.IsApproved(task.Status) && task.ReviewCommit == nil {
		missingStatusField(v, task, "approved", "review_commit")
	}
}

func validateIntegrationFailureStatus(v *violations, task *models.Task, sc *statusClassifier) {
	if task.IntegrationFailure != nil && disallowsIntegrationFailure(task.Status, sc) {
		v.addID(fmt.Sprintf("task %s: stale integration_failure outside integration recovery", task.ID),
			fmt.Errorf("%s task has stale integration_failure outside integration recovery: %s", task.Status, task.ID))
	}
}

func validateMergedStatusFields(v *violations, task *models.Task) {
	if task.Status == models.TaskStatusMerged && task.Worktree != nil {
		v.add(fmt.Errorf("MERGED task still has worktree: %s", task.ID))
	}
}

func validateBlockedStatusFields(v *violations, task *models.Task) {
	if task.Status != models.TaskStatusBlocked {
		return
	}
	if task.BlockedReason == nil {
		v.add(fmt.Errorf("BLOCKED task without blocked_reason: %s", task.ID))
	}
	if len(task.BlockedQuestions) == 0 {
		v.add(fmt.Errorf("BLOCKED task without blocked_questions: %s", task.ID))
	}
	if request := task.RepairRequest; request != nil {
		if strings.TrimSpace(request.Operation) == "" {
			v.add(fmt.Errorf("BLOCKED task repair_request without operation: %s", task.ID))
		}
		if strings.TrimSpace(request.Target) == "" {
			v.add(fmt.Errorf("BLOCKED task repair_request without target: %s", task.ID))
		}
		validateRepairRequestShape(v, task)
		if len(nonEmptyStrings(request.Evidence)) == 0 {
			v.add(fmt.Errorf("BLOCKED task repair_request without evidence: %s", task.ID))
		}
		if len(nonEmptyStrings(request.Validation)) == 0 {
			v.add(fmt.Errorf("BLOCKED task repair_request without validation: %s", task.ID))
		}
	}
}

func validateRejectedStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	if !sc.IsRejected(task.Status) {
		return
	}
	if task.RejectionReason == nil {
		missingStatusField(v, task, "rejected", "rejection_reason")
	}
	if task.Worktree != nil {
		canonicalWorktree := filepath.ToSlash(filepath.Join(paths.WorktreesDirName, task.ID))
		if *task.Worktree != canonicalWorktree {
			v.addID(fmt.Sprintf("task %s: rejected task worktree=%q, want %q", task.ID, *task.Worktree, canonicalWorktree),
				fmt.Errorf("%s task has worktree=%q, want %q", task.Status, *task.Worktree, canonicalWorktree))
		}
	}
	if task.AssignedTo != nil && task.LeaseExpires == nil {
		v.addID(fmt.Sprintf("task %s: rejected task has assigned_to without lease_expires", task.ID),
			fmt.Errorf("%s task has assigned_to without lease_expires: %s", task.Status, task.ID))
	}
	if task.AssignedTo == nil && task.LeaseExpires != nil {
		v.addID(fmt.Sprintf("task %s: rejected task has lease_expires without assigned_to", task.ID),
			fmt.Errorf("%s task has lease_expires without assigned_to: %s", task.Status, task.ID))
	}
	if task.AssignedTo == nil {
		if task.Worktree != nil && task.BaseCommit == nil {
			v.addID(fmt.Sprintf("task %s: released rejected task has worktree without base_commit", task.ID),
				fmt.Errorf("%s released task has worktree without base_commit: %s", task.Status, task.ID))
		}
		if task.Worktree == nil && task.BaseCommit != nil {
			v.addID(fmt.Sprintf("task %s: released rejected task has base_commit without worktree", task.ID),
				fmt.Errorf("%s released task has base_commit without worktree: %s", task.Status, task.ID))
		}
	}
}

func validateSupersededStatusFields(v *violations, task *models.Task) {
	if task.Status == models.TaskStatusSuperseded && task.RescopeReason == nil {
		v.add(fmt.Errorf("SUPERSEDED task without rescope_reason: %s", task.ID))
	}
}

func validateRepairRequestShape(v *violations, task *models.Task) {
	request := task.RepairRequest
	if request.Operation != models.RepairOperationApplyDependencyRepair {
		if strings.TrimSpace(request.Command) == "" {
			v.add(fmt.Errorf("BLOCKED task repair_request without command: %s", task.ID))
		}
		if request.DependencyUpdates != nil {
			v.add(fmt.Errorf("BLOCKED task command-based repair_request must not include dependency_updates: %s", task.ID))
		}
		return
	}

	if strings.TrimSpace(request.Target) != task.ID {
		v.add(fmt.Errorf("BLOCKED task declarative repair_request target must match blocked task: %s", task.ID))
	}
	if strings.TrimSpace(request.Command) != "" {
		v.add(fmt.Errorf("BLOCKED task declarative repair_request must not include command: %s", task.ID))
	}
	if len(request.DependencyUpdates) == 0 {
		v.add(fmt.Errorf("BLOCKED task declarative repair_request without dependency_updates: %s", task.ID))
	}

	seenTasks := make(map[string]bool, len(request.DependencyUpdates))
	for i, update := range request.DependencyUpdates {
		updateTaskID := strings.TrimSpace(update.TaskID)
		if updateTaskID == "" {
			v.add(fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].task_id is required: %s", i, task.ID))
		} else if seenTasks[updateTaskID] {
			v.add(fmt.Errorf("BLOCKED task repair_request has duplicate dependency update task_id %q: %s", updateTaskID, task.ID))
		}
		seenTasks[updateTaskID] = true

		validateExplicitDependencyList(v, update.ExpectedDependsOn, "expected_depends_on", i, task.ID)
		validateExplicitDependencyList(v, update.DesiredDependsOn, "desired_depends_on", i, task.ID)
	}
}

func validateExplicitDependencyList(v *violations, values []string, field string, updateIndex int, blockedTaskID string) {
	if values == nil {
		v.add(fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].%s must be an explicit list: %s", updateIndex, field, blockedTaskID))
		return
	}

	seen := make(map[string]bool, len(values))
	for _, value := range values {
		dependencyID := strings.TrimSpace(value)
		if dependencyID == "" {
			v.add(fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].%s contains an empty task ID: %s", updateIndex, field, blockedTaskID))
			continue
		}
		if seen[dependencyID] {
			v.add(fmt.Errorf("BLOCKED task repair_request dependency_updates[%d] has duplicate %s entry %q: %s", updateIndex, field, dependencyID, blockedTaskID))
		}
		seen[dependencyID] = true
	}
}

func disallowsIntegrationFailure(status models.TaskStatus, sc *statusClassifier) bool {
	// Belt-and-suspenders: explicit legacy/built-in statuses remain protected
	// even if no active pipeline resolver declares the matching lifecycle state.
	switch status {
	case models.TaskStatusReadyForReview,
		models.TaskStatusLegacyReadyForReview,
		models.TaskStatusReviewing,
		models.TaskStatusPartiallyApproved,
		models.TaskStatusReviewingCode2,
		models.TaskStatusApproved,
		models.TaskStatusCodingPlanToReview,
		models.TaskStatusReviewingCodingPlan,
		models.TaskStatusCodingPlanApproved:
		return true
	}
	return sc.IsSubmitted(status) || sc.IsReviewing(status) || sc.IsPartiallyApproved(status) || sc.IsApproved(status)
}

func nonEmptyStrings(values []string) []string {
	var nonEmpty []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			nonEmpty = append(nonEmpty, value)
		}
	}
	return nonEmpty
}
