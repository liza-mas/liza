package statevalidate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateRequiredFields checks that the top-level state structure contains all
// mandatory fields (version, goal, tasks, agents, config, sprint). Prevents
// operating on a partially-initialised or corrupted state file.
func validateRequiredFields(state *models.State, projectRoot string, skipSpecFileCheck bool) error {
	if state.Version == 0 {
		return fmt.Errorf("missing required field 'version'")
	}

	if state.Goal.ID == "" {
		return fmt.Errorf("missing required field 'goal'")
	}

	if state.Tasks == nil {
		return fmt.Errorf("missing required field 'tasks'")
	}

	if state.Agents == nil {
		return fmt.Errorf("missing required field 'agents'")
	}

	if state.Config.IntegrationBranch == "" {
		return fmt.Errorf("missing required field 'config'")
	}

	if state.Sprint.ID == "" {
		return fmt.Errorf("missing required field 'sprint'")
	}

	if !skipSpecFileCheck && state.Goal.SpecRef != "" {
		if err := checkSpecFileExists(projectRoot, state.Goal.SpecRef, state.Config.IntegrationBranch); err != nil {
			return fmt.Errorf("goal %w", err)
		}
	}

	return nil
}

// validateTaskStates ensures every task has a valid status (either hardcoded or
// pipeline-declared), a valid task type, and — for pipeline-configured goals —
// a role_pair that maps to a known pipeline role pair. Prevents tasks from
// entering undefined lifecycle states.
func validateTaskStates(state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver) error {
	for _, task := range state.Tasks {
		statusValid := task.Status.IsValid()
		if !statusValid && resolver != nil {
			// Accept pipeline-declared states and cross-cutting meta-states
			statusValid = task.Status.IsPipelineValid(resolver.AllDeclaredStates())
		}
		if !statusValid {
			return fmt.Errorf("unknown task status '%s' for task %s", task.Status, task.ID)
		}
		if !task.EffectiveType().IsValid() {
			return fmt.Errorf("unknown task type '%s' for task %s", task.Type, task.ID)
		}

		// Pipeline-goal tasks: role_pair is required unconditionally
		if resolver != nil {
			if task.RolePair == "" {
				return fmt.Errorf("task %s missing role_pair (required for pipeline-configured goals)", task.ID)
			}
			if _, err := resolver.InitialStatus(task.RolePair); err != nil {
				return fmt.Errorf("task %s has invalid role_pair %q: %w", task.ID, task.RolePair, err)
			}
		}
	}
	return nil
}

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

// validateTaskInvariants enforces structural invariants across all tasks:
// status-specific required fields, single-assignment per agent, worktree
// existence for executing tasks, completion field presence, spec_ref validity,
// integration_fix history consistency, failed_by uniqueness, parent_task
// referential integrity, and output entry completeness. Prevents invalid task
// state combinations that would cause downstream agent or merge failures.
func validateTaskInvariants(state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) error {
	assignments := make(map[string][]string) // agent ID -> task IDs
	taskIDs := buildTaskIDSet(state.Tasks)
	sc := newStatusClassifier(resolver, cfg)

	for _, task := range state.Tasks {
		if err := validateStatusFields(&task, &sc); err != nil {
			return err
		}
		if err := models.ValidateValidationSafety("validation", task.Validation, task.DestructiveDB); err != nil {
			return fmt.Errorf("task %s %w", task.ID, err)
		}

		// Track assignments for duplicate check (executing tasks count as active)
		if task.AssignedTo != nil && sc.IsExecuting(task.Status) {
			assignments[*task.AssignedTo] = append(assignments[*task.AssignedTo], task.ID)
		}

		// Executing task worktree path must exist (only check if projectRoot is not empty to allow tests)
		if sc.IsExecuting(task.Status) && task.Worktree != nil && projectRoot != "" {
			wtPath := filepath.Join(projectRoot, *task.Worktree)
			if _, err := os.Stat(wtPath); os.IsNotExist(err) {
				return fmt.Errorf("%s task %s has worktree=%s but directory does not exist", task.Status, task.ID, *task.Worktree)
			}
		}

		if requiresCompletionFields(task.Status, resolver, cfg) {
			if task.DoneWhen == "" {
				return fmt.Errorf("non-DRAFT task missing done_when: %s", task.ID)
			}
			if task.SpecRef == "" {
				return fmt.Errorf("non-DRAFT task missing spec_ref: %s", task.ID)
			}
		}

		validateArtifactRefs := !artifactRefsRetired(task)
		if validateArtifactRefs {
			if task.SpecRef != "" && strings.Contains(task.SpecRef, ".worktrees/") {
				return fmt.Errorf("task %s spec_ref contains worktree prefix (must be repo-relative): %s", task.ID, task.SpecRef)
			}
			if !skipSpecFileCheck && task.SpecRef != "" {
				if err := checkArtifactRefFileExists(projectRoot, "spec_ref", task.SpecRef, state.Config.IntegrationBranch, task.ID); err != nil {
					return err
				}
			}
			if task.EpicRef != "" && strings.Contains(task.EpicRef, ".worktrees/") {
				return fmt.Errorf("task %s epic_ref contains worktree prefix (must be repo-relative): %s", task.ID, task.EpicRef)
			}
			if !skipSpecFileCheck && task.EpicRef != "" {
				if err := checkArtifactRefFileExists(projectRoot, "epic_ref", task.EpicRef, state.Config.IntegrationBranch, task.ID); err != nil {
					return err
				}
			}
			if task.PlanRef != "" && strings.Contains(task.PlanRef, ".worktrees/") {
				return fmt.Errorf("task %s plan_ref contains worktree prefix (must be repo-relative): %s", task.ID, task.PlanRef)
			}
			if !skipSpecFileCheck && task.PlanRef != "" {
				if err := checkArtifactRefFileExists(projectRoot, "plan_ref", task.PlanRef, state.Config.IntegrationBranch, task.ID); err != nil {
					return err
				}
			}
			if task.ArchRef != "" && strings.Contains(task.ArchRef, ".worktrees/") {
				return fmt.Errorf("task %s arch_ref contains worktree prefix (must be repo-relative): %s", task.ID, task.ArchRef)
			}
			if !skipSpecFileCheck && task.ArchRef != "" {
				if err := checkArtifactRefFileExists(projectRoot, "arch_ref", task.ArchRef, state.Config.IntegrationBranch, task.ID); err != nil {
					return err
				}
			}
		}

		// Task with integration_fix must have INTEGRATION_FAILED in history
		if task.IntegrationFix {
			hasFailedEvent := false
			for _, entry := range task.History {
				if entry.Event == models.TaskEventIntegrationFailed {
					hasFailedEvent = true
					break
				}
			}
			if !hasFailedEvent {
				return fmt.Errorf("task %s has integration_fix:true but no INTEGRATION_FAILED event in history", task.ID)
			}
		}

		// failed_by must have unique agent IDs
		if len(task.FailedBy) > 0 {
			seen := make(map[string]bool)
			for _, agent := range task.FailedBy {
				if seen[agent] {
					stateRelPath := filepath.ToSlash(filepath.Join(paths.ProjectDirName(), paths.StateFileName))
					return fmt.Errorf("task %s has duplicate agent IDs in failed_by (manually edit %s to remove duplicates)", task.ID, stateRelPath)
				}
				seen[agent] = true
			}
		}

		// parent_task / parent_tasks must reference existing tasks
		for _, parentID := range task.EffectiveParentTasks() {
			if !taskIDs[parentID] {
				return fmt.Errorf("task %s has parent_task referencing non-existent task '%s'", task.ID, parentID)
			}
		}

		if err := validateTaskOutput(&task, validateArtifactRefs); err != nil {
			return err
		}

		// Attempt must be 0 (unset/legacy), 1, or 2
		if task.Attempt < 0 || task.Attempt > 2 {
			return fmt.Errorf("task %s has invalid attempt value %d", task.ID, task.Attempt)
		}

		// Cross-check: attempt 2 in initial status must have reset counters
		if task.Attempt == 2 && sc.IsInitial(task.Status) {
			if task.Iteration != 0 {
				return fmt.Errorf("task %s at attempt 2 in initial status has non-zero iteration %d", task.ID, task.Iteration)
			}
			if task.ReviewCyclesCurrent != 0 {
				return fmt.Errorf("task %s at attempt 2 in initial status has non-zero review_cycles_current %d", task.ID, task.ReviewCyclesCurrent)
			}
		}
	}

	// Check for duplicate assignments
	for agent, taskIDs := range assignments {
		if len(taskIDs) > 1 {
			return fmt.Errorf("agent %s assigned to multiple active tasks simultaneously: %v", agent, taskIDs)
		}
	}

	return nil
}

// validateStatusFields checks that each task status has the fields required by
// its lifecycle phase (e.g. executing tasks need assigned_to, worktree,
// base_commit, and lease_expires; reviewing tasks need reviewing_by and
// review_lease_expires). Prevents tasks from entering states without the
// metadata needed for agents to operate on them.
func validateStatusFields(task *models.Task, sc *statusClassifier) error {
	if sc.IsInitial(task.Status) && task.AssignedTo != nil {
		return fmt.Errorf("%s task with assigned_to: %s", task.Status, task.ID)
	}

	if sc.IsExecuting(task.Status) {
		if task.AssignedTo == nil {
			return fmt.Errorf("%s task without assigned_to: %s", task.Status, task.ID)
		}
		if task.Worktree == nil {
			return fmt.Errorf("%s task without worktree: %s", task.Status, task.ID)
		}
		if !task.IntegrationFix && task.BaseCommit == nil {
			return fmt.Errorf("%s task without base_commit: %s", task.Status, task.ID)
		}
		if task.LeaseExpires == nil {
			return fmt.Errorf("%s task without lease_expires: %s", task.Status, task.ID)
		}
	}

	if sc.IsSubmitted(task.Status) && task.ReviewCommit == nil {
		return fmt.Errorf("%s task without review_commit: %s", task.Status, task.ID)
	}

	if sc.IsReviewing(task.Status) {
		if task.ReviewingBy == nil {
			return fmt.Errorf("%s task without reviewing_by: %s", task.Status, task.ID)
		}
		if task.ReviewLeaseExpires == nil {
			return fmt.Errorf("%s task without review_lease_expires: %s", task.Status, task.ID)
		}
		if task.ReviewCommit == nil {
			return fmt.Errorf("%s task without review_commit: %s", task.Status, task.ID)
		}
	}

	if sc.IsApproved(task.Status) && task.ReviewCommit == nil {
		return fmt.Errorf("%s task without review_commit: %s", task.Status, task.ID)
	}
	if task.IntegrationFailure != nil && disallowsIntegrationFailure(task.Status, sc) {
		return fmt.Errorf("%s task has stale integration_failure outside integration recovery: %s", task.Status, task.ID)
	}

	if task.Status == models.TaskStatusMerged && task.Worktree != nil {
		return fmt.Errorf("MERGED task still has worktree: %s", task.ID)
	}

	if task.Status == models.TaskStatusBlocked {
		if task.BlockedReason == nil {
			return fmt.Errorf("BLOCKED task without blocked_reason: %s", task.ID)
		}
		if len(task.BlockedQuestions) == 0 {
			return fmt.Errorf("BLOCKED task without blocked_questions: %s", task.ID)
		}
		if task.RepairRequest != nil && strings.TrimSpace(task.RepairRequest.Operation) == "" {
			return fmt.Errorf("BLOCKED task repair_request without operation: %s", task.ID)
		}
		if task.RepairRequest != nil && strings.TrimSpace(task.RepairRequest.Target) == "" {
			return fmt.Errorf("BLOCKED task repair_request without target: %s", task.ID)
		}
		if task.RepairRequest != nil && strings.TrimSpace(task.RepairRequest.Command) == "" {
			return fmt.Errorf("BLOCKED task repair_request without command: %s", task.ID)
		}
		if task.RepairRequest != nil && len(nonEmptyStrings(task.RepairRequest.Evidence)) == 0 {
			return fmt.Errorf("BLOCKED task repair_request without evidence: %s", task.ID)
		}
		if task.RepairRequest != nil && len(nonEmptyStrings(task.RepairRequest.Validation)) == 0 {
			return fmt.Errorf("BLOCKED task repair_request without validation: %s", task.ID)
		}
	}

	if sc.IsRejected(task.Status) && task.RejectionReason == nil {
		return fmt.Errorf("%s task without rejection_reason: %s", task.Status, task.ID)
	}

	if task.Status == models.TaskStatusSuperseded {
		if task.RescopeReason == nil {
			return fmt.Errorf("SUPERSEDED task without rescope_reason: %s", task.ID)
		}
	}

	return nil
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

// validateTaskOutput checks that each output entry has all required fields
// (desc, done_when, scope, spec_ref) and that spec_ref values are
// repo-relative (not worktree-prefixed). Prevents downstream coding tasks
// from being created with incomplete or unreachable specifications.
func validateTaskOutput(task *models.Task, validateArtifactRefs bool) error {
	for i, entry := range task.Output {
		if entry.Desc == "" {
			return fmt.Errorf("task %s output[%d] missing desc", task.ID, i)
		}
		if entry.DoneWhen == "" {
			return fmt.Errorf("task %s output[%d] missing done_when", task.ID, i)
		}
		if entry.Scope == "" {
			return fmt.Errorf("task %s output[%d] missing scope", task.ID, i)
		}
		if entry.SpecRef == "" {
			return fmt.Errorf("task %s output[%d] missing spec_ref", task.ID, i)
		}
		if err := models.ValidateValidationSafety(fmt.Sprintf("output[%d].validation", i), entry.Validation, entry.DestructiveDB); err != nil {
			return fmt.Errorf("task %s %w", task.ID, err)
		}
		if !validateArtifactRefs {
			continue
		}
		if strings.Contains(entry.SpecRef, ".worktrees/") {
			return fmt.Errorf("task %s output[%d] spec_ref contains worktree prefix (must be repo-relative): %s", task.ID, i, entry.SpecRef)
		}
		if err := ValidateArtifactRefScalar(fmt.Sprintf("output[%d].spec_ref", i), entry.SpecRef, task.ID); err != nil {
			return err
		}
		if entry.EpicRef != "" && strings.Contains(entry.EpicRef, ".worktrees/") {
			return fmt.Errorf("task %s output[%d] epic_ref contains worktree prefix (must be repo-relative): %s", task.ID, i, entry.EpicRef)
		}
		if err := ValidateArtifactRefScalar(fmt.Sprintf("output[%d].epic_ref", i), entry.EpicRef, task.ID); err != nil {
			return err
		}
		if entry.PlanRef != "" && strings.Contains(entry.PlanRef, ".worktrees/") {
			return fmt.Errorf("task %s output[%d] plan_ref contains worktree prefix (must be repo-relative): %s", task.ID, i, entry.PlanRef)
		}
		if err := ValidateArtifactRefScalar(fmt.Sprintf("output[%d].plan_ref", i), entry.PlanRef, task.ID); err != nil {
			return err
		}
		if entry.ArchRef != "" && strings.Contains(entry.ArchRef, ".worktrees/") {
			return fmt.Errorf("task %s output[%d] arch_ref contains worktree prefix (must be repo-relative): %s", task.ID, i, entry.ArchRef)
		}
		if err := ValidateArtifactRefScalar(fmt.Sprintf("output[%d].arch_ref", i), entry.ArchRef, task.ID); err != nil {
			return err
		}
	}
	return nil
}

// requiresCompletionFields returns true if a task in the given status must have
// done_when and spec_ref populated. Terminal meta-states (SUPERSEDED, ABANDONED)
// and draft/initial states are exempt because they represent tasks that have not
// yet been fully specified.
func requiresCompletionFields(status models.TaskStatus, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) bool {
	// Terminal meta-states don't require completion fields
	if status == models.TaskStatusSuperseded || status == models.TaskStatusAbandoned {
		return false
	}
	// Hardcoded draft states (also covered by pipeline initial states below)
	if status == models.TaskStatusDraft || status == models.TaskStatusDraftCodingPlan {
		return false
	}
	// Pipeline initial states (drafts)
	if resolver != nil && cfg != nil {
		for rpName := range cfg.Pipeline.RolePairs {
			if initial, err := resolver.InitialStatus(rpName); err == nil && status == initial {
				return false
			}
		}
	}
	return true
}
