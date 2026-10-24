package statevalidate

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

func validateTerminalArchiveShape(v *violations, task *models.Task) {
	if ref := task.TerminalArchive; ref != nil {
		if raw, err := hex.DecodeString(ref.SHA256); err != nil || len(raw) != sha256Bytes || strings.ToLower(ref.SHA256) != ref.SHA256 || ref.ArchivedAt.IsZero() {
			v.add(fmt.Errorf("task %s terminal_archive requires a lowercase SHA-256 digest and archived_at", task.ID))
		}
	}
}

func validateStateLockHoldShape(v *violations, task *models.Task) {
	if hold := task.StateLockHold; hold != nil {
		if raw, err := hex.DecodeString(hold.BlockerDigest); err != nil || len(raw) != sha256Bytes || strings.ToLower(hold.BlockerDigest) != hold.BlockerDigest || hold.EpisodeAt.IsZero() || hold.AfterSequence == 0 {
			v.add(fmt.Errorf("task %s state_lock_hold requires an episode, lowercase blocker digest and nonzero publication sequence", task.ID))
		}
		if (hold.RefusedFingerprint == "") != (hold.RefusedReason == "") {
			v.add(fmt.Errorf("task %s state_lock_hold refusal requires both fingerprint and reason", task.ID))
		}
	}
}

func validateTaskValidationCommands(v *violations, task *models.Task) {
	for _, err := range models.ValidationSafetyViolations("validation", task.Validation, task.DestructiveDB) {
		v.add(fmt.Errorf("task %s %w", task.ID, err))
	}
	for _, err := range models.ValidationPrerequisiteViolations(task.Validation, task.ValidationPrerequisites) {
		v.add(fmt.Errorf("task %s %w", task.ID, err))
	}
	for _, err := range models.RuntimeInputViolations(task.Validation, task.RuntimeInputs) {
		v.add(fmt.Errorf("task %s %w", task.ID, err))
	}
}

// validateExecutingWorktree requires an executing task's worktree path to
// exist (only checked if projectRoot is not empty to allow tests).
func validateExecutingWorktree(v *violations, task *models.Task, sc *statusClassifier, projectRoot string) {
	if sc.IsExecuting(task.Status) && task.Worktree != nil && projectRoot != "" {
		wtPath := filepath.Join(projectRoot, *task.Worktree)
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			// The status is context, not the constraint: moving between
			// executing statuses keeps the same missing directory.
			v.addID(fmt.Sprintf("task %s worktree=%s directory does not exist", task.ID, *task.Worktree),
				fmt.Errorf("%s task %s has worktree=%s but directory does not exist", task.Status, task.ID, *task.Worktree))
		}
	}
}

func validateCompletionFields(v *violations, task *models.Task, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) {
	if requiresCompletionFields(task.Status, resolver, cfg) {
		if task.DoneWhen == "" {
			v.add(fmt.Errorf("non-DRAFT task missing done_when: %s", task.ID))
		}
		if task.SpecRef == "" {
			v.add(fmt.Errorf("non-DRAFT task missing spec_ref: %s", task.ID))
		}
	}
}

func validateTaskArtifactRefs(v *violations, task *models.Task, state *models.State, projectRoot string, skipSpecFileCheck bool) {
	if artifactRefsRetired(*task) {
		return
	}
	for _, ref := range []struct{ field, value string }{
		{"spec_ref", task.SpecRef},
		{"epic_ref", task.EpicRef},
		{"plan_ref", task.PlanRef},
		{"arch_ref", task.ArchRef},
	} {
		if ref.value == "" {
			continue
		}
		if strings.Contains(ref.value, ".worktrees/") {
			v.add(fmt.Errorf("task %s %s contains worktree prefix (must be repo-relative): %s", task.ID, ref.field, ref.value))
		}
		if !skipSpecFileCheck {
			v.add(checkArtifactRefFileExists(projectRoot, ref.field, ref.value, state.Config.IntegrationBranch, task.ID))
		}
	}
}

// validateIntegrationFixHistory requires a task with integration_fix to carry
// an INTEGRATION_FAILED event in its history.
func validateIntegrationFixHistory(v *violations, task *models.Task) {
	if !task.IntegrationFix {
		return
	}
	hasFailedEvent := false
	for _, entry := range task.History {
		if entry.Event == models.TaskEventIntegrationFailed {
			hasFailedEvent = true
			break
		}
	}
	if !hasFailedEvent {
		v.add(fmt.Errorf("task %s has integration_fix:true but no INTEGRATION_FAILED event in history", task.ID))
	}
}

// validateFailedByUnique requires failed_by to hold unique agent IDs.
func validateFailedByUnique(v *violations, task *models.Task) {
	if len(task.FailedBy) == 0 {
		return
	}
	seen := make(map[string]bool)
	for _, agent := range task.FailedBy {
		if seen[agent] {
			stateRelPath := filepath.ToSlash(filepath.Join(paths.ProjectDirName(), paths.StateFileName))
			v.addID(fmt.Sprintf("task %s duplicate failed_by agent %q", task.ID, agent),
				fmt.Errorf("task %s has duplicate agent IDs in failed_by (manually edit %s to remove duplicates)", task.ID, stateRelPath))
		}
		seen[agent] = true
	}
}

// validateParentTasks requires parent_task / parent_tasks to reference
// existing tasks.
func validateParentTasks(v *violations, task *models.Task, taskIDs map[string]bool) {
	for _, parentID := range task.EffectiveParentTasks() {
		if !taskIDs[parentID] {
			v.add(fmt.Errorf("task %s has parent_task referencing non-existent task '%s'", task.ID, parentID))
		}
	}
}

// validateAttempt requires attempt to be 0 (unset/legacy), 1, or 2, and an
// attempt-2 task in an initial status to have reset counters.
func validateAttempt(v *violations, task *models.Task, sc *statusClassifier) {
	if task.Attempt < 0 || task.Attempt > models.MaxTaskAttempt {
		v.add(fmt.Errorf("task %s has invalid attempt value %d", task.ID, task.Attempt))
	}

	if task.Attempt == models.MaxTaskAttempt && sc.IsInitial(task.Status) {
		if task.Iteration != 0 {
			v.add(fmt.Errorf("task %s at attempt 2 in initial status has non-zero iteration %d", task.ID, task.Iteration))
		}
		if task.ReviewCyclesCurrent != 0 {
			v.add(fmt.Errorf("task %s at attempt 2 in initial status has non-zero review_cycles_current %d", task.ID, task.ReviewCyclesCurrent))
		}
	}
}

// validateTaskOutput checks that each output entry has all required fields
// (desc, done_when, scope, spec_ref) and that spec_ref values are
// repo-relative (not worktree-prefixed). Prevents downstream coding tasks
// from being created with incomplete or unreachable specifications.
func validateTaskOutput(v *violations, task *models.Task, validateArtifactRefs bool) {
	for i, entry := range task.Output {
		for _, field := range []struct{ name, value string }{
			{"desc", entry.Desc},
			{"done_when", entry.DoneWhen},
			{"scope", entry.Scope},
			{"spec_ref", entry.SpecRef},
		} {
			if field.value == "" {
				v.add(fmt.Errorf("task %s output[%d] missing %s", task.ID, i, field.name))
			}
		}
		for _, err := range models.ValidationSafetyViolations(fmt.Sprintf("output[%d].validation", i), entry.Validation, entry.DestructiveDB) {
			v.add(fmt.Errorf("task %s %w", task.ID, err))
		}
		for _, err := range models.ValidationPrerequisiteViolations(entry.Validation, entry.ValidationPrerequisites) {
			v.add(fmt.Errorf("task %s output[%d]: %w", task.ID, i, err))
		}
		for _, err := range models.RuntimeInputViolations(entry.Validation, entry.RuntimeInputs) {
			v.add(fmt.Errorf("task %s output[%d]: %w", task.ID, i, err))
		}
		if !validateArtifactRefs {
			continue
		}
		for _, ref := range []struct{ field, value string }{
			{"spec_ref", entry.SpecRef},
			{"epic_ref", entry.EpicRef},
			{"plan_ref", entry.PlanRef},
			{"arch_ref", entry.ArchRef},
		} {
			if strings.Contains(ref.value, ".worktrees/") {
				v.add(fmt.Errorf("task %s output[%d] %s contains worktree prefix (must be repo-relative): %s", task.ID, i, ref.field, ref.value))
			}
			v.add(ValidateArtifactRefScalar(fmt.Sprintf("output[%d].%s", i, ref.field), ref.value, task.ID))
		}
	}
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
