package statevalidate

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statehygiene"
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
		if err := ValidateTaskLifecycle(&task); err != nil {
			return err
		}
		if err := validateStatusFields(&task, &sc); err != nil {
			return err
		}
		if err := models.ValidateValidationSafety("validation", task.Validation, task.DestructiveDB); err != nil {
			return fmt.Errorf("task %s %w", task.ID, err)
		}

		if err := models.ValidateValidationPrerequisites(task.Validation, task.ValidationPrerequisites); err != nil {
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
		if err := validateAcceptanceState(&task); err != nil {
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

// Admission performs repository checks. State validation only checks persisted
// shape, leaving missing receipts repairable through update-review-commit.
func validateAcceptanceState(task *models.Task) error {
	if err := validateArchivedFields(task); err != nil {
		return err
	}
	source := task.AcceptanceSource
	if source == nil {
		if task.AcceptanceReceipt != nil || len(task.Archived) > 0 {
			return fmt.Errorf("task %s acceptance_receipt requires acceptance_source", task.ID)
		}
		return nil
	}
	if source.Ref == "" || source.ParentTask == "" {
		return fmt.Errorf("task %s acceptance_source requires ref and parent_task", task.ID)
	}
	for _, value := range []string{source.Commit, source.Blob, source.ParentReviewCommit} {
		if _, err := hex.DecodeString(value); err != nil || len(value) != 40 || strings.ToLower(value) != value {
			return fmt.Errorf("task %s acceptance_source requires immutable lowercase object IDs", task.ID)
		}
	}
	receipt := task.AcceptanceReceipt
	if receipt == nil {
		return nil
	}
	if receipt.Version != 1 || task.ReviewCommit == nil || receipt.ReviewCommit != *task.ReviewCommit || receipt.Source != *source {
		return fmt.Errorf("task %s acceptance_receipt does not match its source and review_commit", task.ID)
	}
	if receipt.ManifestPath == "" || len(receipt.Mappings) == 0 || len(receipt.Mappings) > 256 || len(receipt.Commands) > 64 {
		return fmt.Errorf("task %s acceptance_receipt has invalid manifest or result bounds", task.ID)
	}
	if _, err := hex.DecodeString(receipt.ManifestBlob); err != nil || len(receipt.ManifestBlob) != 40 {
		return fmt.Errorf("task %s acceptance_receipt requires manifest blob identity", task.ID)
	}
	totalOutput := 0
	for _, command := range receipt.Commands {
		totalOutput += len(command.Output)
		if command.ExitCode != 0 || command.StartedAt.IsZero() || command.FinishedAt.Before(command.StartedAt) {
			return fmt.Errorf("task %s acceptance_receipt contains unsuccessful execution", task.ID)
		}
		if _, err := hex.DecodeString(command.CommandSHA256); err != nil || len(command.CommandSHA256) != 64 {
			return fmt.Errorf("task %s acceptance_receipt requires canonical command identity", task.ID)
		}
	}
	if totalOutput > 1024*1024 {
		return fmt.Errorf("task %s acceptance_receipt output exceeds 1 MiB", task.ID)
	}
	return nil
}

// validateArchivedFields checks the refs of fields moved to archive objects.
// Like receipts, it checks shape only and opens no files: one ref per field,
// only on terminal tasks, never alongside the live value it replaces.
func validateArchivedFields(task *models.Task) error {
	if len(task.Archived) == 0 {
		return nil
	}
	if !task.Status.IsTerminal() {
		return fmt.Errorf("task %s in status %s has archived fields; only terminal tasks may", task.ID, task.Status)
	}
	seen := map[string]bool{}
	for _, ref := range task.Archived {
		if ref.Field != models.ArchivedFieldAcceptanceReceipt {
			return fmt.Errorf("task %s archived field %q is not archivable", task.ID, ref.Field)
		}
		if seen[ref.Field] {
			return fmt.Errorf("task %s has more than one archived %s", task.ID, ref.Field)
		}
		seen[ref.Field] = true
		if _, err := hex.DecodeString(ref.SHA256); err != nil || len(ref.SHA256) != 64 || strings.ToLower(ref.SHA256) != ref.SHA256 {
			return fmt.Errorf("task %s archived %s requires a lowercase SHA-256 digest", task.ID, ref.Field)
		}
		if ref.ArchivedAt.IsZero() {
			return fmt.Errorf("task %s archived %s requires archived_at", task.ID, ref.Field)
		}
	}
	if seen[models.ArchivedFieldAcceptanceReceipt] && task.AcceptanceReceipt != nil {
		return fmt.Errorf("task %s has both a live and an archived acceptance_receipt", task.ID)
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
		if task.RepairRequest != nil {
			if err := validateRepairRequestShape(task); err != nil {
				return err
			}
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
	if sc.IsRejected(task.Status) {
		if task.Worktree != nil {
			canonicalWorktree := filepath.ToSlash(filepath.Join(paths.WorktreesDirName, task.ID))
			if *task.Worktree != canonicalWorktree {
				return fmt.Errorf("%s task has worktree=%q, want %q", task.Status, *task.Worktree, canonicalWorktree)
			}
		}
		if task.AssignedTo != nil && task.LeaseExpires == nil {
			return fmt.Errorf("%s task has assigned_to without lease_expires: %s", task.Status, task.ID)
		}
		if task.AssignedTo == nil && task.LeaseExpires != nil {
			return fmt.Errorf("%s task has lease_expires without assigned_to: %s", task.Status, task.ID)
		}
		if task.AssignedTo == nil {
			if task.Worktree != nil && task.BaseCommit == nil {
				return fmt.Errorf("%s released task has worktree without base_commit: %s", task.Status, task.ID)
			}
			if task.Worktree == nil && task.BaseCommit != nil {
				return fmt.Errorf("%s released task has base_commit without worktree: %s", task.Status, task.ID)
			}
		}
	}

	if task.Status == models.TaskStatusSuperseded {
		if task.RescopeReason == nil {
			return fmt.Errorf("SUPERSEDED task without rescope_reason: %s", task.ID)
		}
	}

	return validateTaskRejectionRCA(task)
}

// Structural bounds for the rejection-RCA request payloads. Every bound below
// is evaluated from the request alone, so the preflight and mutation
// boundaries cannot disagree about a payload. A bound that needs task state —
// a rejection_index no higher than the task's durable rejection count — is a
// semantic precondition of the operation, never a structural diagnostic.
const (
	rejectionRCAMaxContributions = 32
	rejectionRCAMaxCategories    = 8
	rejectionRCAMaxCategoryBytes = 64
	rejectionRCAMaxEvidence      = 4
	rejectionRCAMaxEvidenceBytes = 256
)

// ValidateRejectionRCARequest reports the structural defects of a
// record-rejection-rca payload. A nil result means the payload is valid.
func ValidateRejectionRCARequest(request models.RejectionRCARequest) []models.FieldDiagnostic {
	diagnostics := validateRejectionRCASchemaVersion(request.SchemaVersion)
	diagnostics = append(diagnostics, validateBoundedPayloadText("/summary", request.Summary, statehygiene.MaxStateTextBytes, true)...)

	switch {
	case len(request.Contributions) == 0:
		diagnostics = append(diagnostics, payloadDiagnostic("/contributions",
			"at least one contribution is required", models.FieldValueClassMissing))
	case len(request.Contributions) > rejectionRCAMaxContributions:
		diagnostics = append(diagnostics, payloadDiagnostic("/contributions",
			fmt.Sprintf("at most %d contributions are allowed", rejectionRCAMaxContributions),
			models.FieldValueClassOutOfRange))
	default:
		diagnostics = append(diagnostics, validateRejectionRCAContributions(request.Contributions)...)
	}

	return models.NormalizeFieldDiagnostics(diagnostics)
}

// ValidateRejectionRCADispositionRequest reports the structural defects of a
// resume-rejection-rca payload. A nil result means the payload is valid.
func ValidateRejectionRCADispositionRequest(request models.RejectionRCADispositionRequest) []models.FieldDiagnostic {
	diagnostics := validateRejectionRCASchemaVersion(request.SchemaVersion)

	switch {
	case strings.TrimSpace(request.RecoveryPath) == "":
		diagnostics = append(diagnostics, payloadDiagnostic("/recovery_path",
			"a non-empty value is required", models.FieldValueClassMissing))
	case !models.IsRecoveryPath(request.RecoveryPath):
		diagnostics = append(diagnostics, payloadDiagnostic("/recovery_path",
			"must name a known recovery path", models.FieldValueClassUnknownEnum))
	}

	diagnostics = append(diagnostics, validateBoundedPayloadText("/rationale", request.Rationale, statehygiene.MaxStateTextBytes,
		request.RecoveryPath == models.RecoveryHumanOverride)...)
	return models.NormalizeFieldDiagnostics(diagnostics)
}

// ValidateVerdictPayloadShape reports the structural defects of a
// submit-verdict payload, keyed by the command's flag names. An empty
// review_commit stays valid because the unauthenticated legacy call does not
// carry one; a supplied value must be the full immutable SHA reviewed.
func ValidateVerdictPayloadShape(taskID, verdict, reason, agentID, impact, reviewCommit string) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic

	if strings.TrimSpace(taskID) == "" {
		diagnostics = append(diagnostics, payloadDiagnostic("/task_id",
			"a non-empty value is required", models.FieldValueClassMissing))
	}
	if strings.TrimSpace(agentID) == "" {
		diagnostics = append(diagnostics, payloadDiagnostic("/agent_id",
			"a non-empty value is required", models.FieldValueClassMissing))
	}

	switch {
	case strings.TrimSpace(verdict) == "":
		diagnostics = append(diagnostics, payloadDiagnostic("/verdict",
			"a non-empty value is required", models.FieldValueClassMissing))
	case verdict != "APPROVED" && verdict != "REJECTED":
		diagnostics = append(diagnostics, payloadDiagnostic("/verdict",
			"must be APPROVED or REJECTED", models.FieldValueClassUnknownEnum))
	case verdict == "REJECTED":
		diagnostics = append(diagnostics, validateBoundedPayloadText("/reason", reason, statehygiene.MaxStateTextBytes, true)...)
	}

	if impact != "" && !isVerdictImpact(impact) {
		diagnostics = append(diagnostics, payloadDiagnostic("/impact",
			"must be standard, significant or architecture", models.FieldValueClassUnknownEnum))
	}
	if reviewCommit != "" && !models.IsFullReviewCommit(reviewCommit) {
		diagnostics = append(diagnostics, payloadDiagnostic("/review_commit",
			"must be the full immutable commit SHA reviewed", models.FieldValueClassMalformed))
	}

	return models.NormalizeFieldDiagnostics(diagnostics)
}

// isVerdictImpact mirrors the impact classifications submit-verdict accepts.
// The ops package owns the ordering those values carry; this package cannot
// import it, so the vocabulary is repeated here and must change with it.
func isVerdictImpact(impact string) bool {
	switch impact {
	case "standard", "significant", "architecture":
		return true
	}
	return false
}

func validateRejectionRCAContributions(contributions []models.RejectionRCAContribution) []models.FieldDiagnostic {
	var diagnostics []models.FieldDiagnostic
	seen := make(map[int]bool, len(contributions))

	for i, contribution := range contributions {
		prefix := fmt.Sprintf("/contributions/%d", i)
		diagnostics = append(diagnostics, validateRejectionIndex(prefix, contribution.RejectionIndex, seen)...)
		diagnostics = append(diagnostics, validateBoundedPayloadList(prefix+"/categories", contribution.Categories,
			rejectionRCAMaxCategories, rejectionRCAMaxCategoryBytes, true)...)
		diagnostics = append(diagnostics, validateBoundedPayloadList(prefix+"/evidence", contribution.Evidence,
			rejectionRCAMaxEvidence, rejectionRCAMaxEvidenceBytes, false)...)
	}

	return diagnostics
}

// validateRejectionIndex records each accepted index in seen, so a duplicate is
// reported on the later contribution rather than on the one that defined it.
func validateRejectionIndex(prefix string, index int, seen map[int]bool) []models.FieldDiagnostic {
	field := prefix + "/rejection_index"
	switch {
	case index < 1:
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be at least 1", models.FieldValueClassOutOfRange)}
	case seen[index]:
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be unique across contributions", models.FieldValueClassConflict)}
	}
	seen[index] = true
	return nil
}

// validateBoundedPayloadList enforces the cardinality of a string list and the
// bounds of each entry, reporting the cardinality defect alone when it applies
// so one malformed list yields one diagnostic.
func validateBoundedPayloadList(field string, values []string, maxEntries, maxBytes int, required bool) []models.FieldDiagnostic {
	switch {
	case required && len(values) == 0:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			"at least one entry is required", models.FieldValueClassMissing)}
	case len(values) > maxEntries:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			fmt.Sprintf("at most %d entries are allowed", maxEntries), models.FieldValueClassOutOfRange)}
	}
	var diagnostics []models.FieldDiagnostic
	for i, value := range values {
		diagnostics = append(diagnostics, validateBoundedPayloadText(
			fmt.Sprintf("%s/%d", field, i), value, maxBytes, true)...)
	}
	return diagnostics
}

func validateRejectionRCASchemaVersion(version int) []models.FieldDiagnostic {
	if version == models.RejectionRCASchemaVersion {
		return nil
	}
	return []models.FieldDiagnostic{payloadDiagnostic("/schema_version",
		fmt.Sprintf("must be %d", models.RejectionRCASchemaVersion), models.FieldValueClassOutOfRange)}
}

// validateBoundedPayloadText enforces presence, a byte ceiling and UTF-8
// validity on one payload string, reporting the first defect only so a caller
// sees one diagnostic per field.
func validateBoundedPayloadText(field, value string, maxBytes int, required bool) []models.FieldDiagnostic {
	switch {
	case required && strings.TrimSpace(value) == "":
		return []models.FieldDiagnostic{payloadDiagnostic(field, "a non-empty value is required", models.FieldValueClassMissing)}
	case len(value) > maxBytes:
		return []models.FieldDiagnostic{payloadDiagnostic(field,
			fmt.Sprintf("must be at most %d bytes", maxBytes), models.FieldValueClassOversized)}
	case !utf8.ValidString(value):
		return []models.FieldDiagnostic{payloadDiagnostic(field, "must be valid UTF-8", models.FieldValueClassMalformed)}
	}
	return nil
}

// payloadDiagnostic names a rejected field without echoing its value. The
// schema version is stamped by the payload-schema registry, which knows which
// schema produced the diagnostic.
func payloadDiagnostic(field, constraint, valueClass string) models.FieldDiagnostic {
	return models.FieldDiagnostic{
		Field:      field,
		Constraint: constraint,
		ValueClass: valueClass,
		SafeAction: models.FieldDiagnosticCorrectInput,
	}
}

// validateTaskRejectionRCA checks a stored RCA record: its gate-seeded fields,
// its recorded caller fields through the same structural validator both
// request boundaries use, and its disposition. A BLOCKED task whose gate is
// open must additionally carry the typed blocked_reason token, so the reason
// class stays legible to consumers that only read state.
func validateTaskRejectionRCA(task *models.Task) error {
	if err := validateRejectionRCAGateReason(task); err != nil {
		return err
	}
	record := task.RejectionRCA
	if record == nil {
		return nil
	}
	if err := validateRejectionRCASeededFields(task.ID, record); err != nil {
		return err
	}
	// A fingerprint is written only together with the caller fields, so it is
	// the marker that distinguishes a seeded record from a recorded one.
	recorded := record.Fingerprint != ""
	if recorded {
		if err := validateRejectionRCARecordedFields(task.ID, record); err != nil {
			return err
		}
	}
	return validateRejectionRCADisposition(task.ID, record.Disposition, recorded)
}

func validateRejectionRCAGateReason(task *models.Task) error {
	if task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() {
		return nil
	}
	reason := ""
	if task.BlockedReason != nil {
		reason = strings.TrimSpace(*task.BlockedReason)
	}
	if strings.HasPrefix(reason, models.BlockedReasonRejectionRCARequired) {
		return nil
	}
	return fmt.Errorf("BLOCKED task with an open rejection_rca gate requires a blocked_reason starting with %s: %s",
		models.BlockedReasonRejectionRCARequired, task.ID)
}

func validateRejectionRCASeededFields(taskID string, record *models.RejectionRCARecord) error {
	if record.SchemaVersion != models.RejectionRCASchemaVersion {
		return fmt.Errorf("task %s rejection_rca schema_version must be %d", taskID, models.RejectionRCASchemaVersion)
	}
	if record.Threshold < 1 {
		return fmt.Errorf("task %s rejection_rca threshold must be at least 1", taskID)
	}
	if record.RejectionCount < record.Threshold {
		return fmt.Errorf("task %s rejection_rca rejection_count must be at least its threshold: %d", taskID, record.Threshold)
	}
	if record.GatedAt.IsZero() {
		return fmt.Errorf("task %s rejection_rca requires gated_at", taskID)
	}
	return nil
}

func validateRejectionRCARecordedFields(taskID string, record *models.RejectionRCARecord) error {
	if diagnostics := ValidateRejectionRCARequest(record.Request()); len(diagnostics) > 0 {
		return fmt.Errorf("task %s rejection_rca %s violates: %s", taskID, diagnostics[0].Field, diagnostics[0].Constraint)
	}
	if strings.TrimSpace(record.RecordedBy) == "" {
		return fmt.Errorf("task %s rejection_rca requires recorded_by", taskID)
	}
	if record.RecordedAt == nil || record.RecordedAt.IsZero() {
		return fmt.Errorf("task %s rejection_rca requires recorded_at", taskID)
	}
	return nil
}

func validateRejectionRCADisposition(taskID string, disposition *models.RejectionRCADisposition, recorded bool) error {
	if disposition == nil {
		return nil
	}
	if !recorded {
		return fmt.Errorf("task %s rejection_rca disposition requires a recorded RCA", taskID)
	}
	if !models.IsRecoveryPath(disposition.RecoveryPath) {
		return fmt.Errorf("task %s rejection_rca disposition has an unknown recovery_path", taskID)
	}
	if disposition.RestoreMode != models.RejectionRCARestoreMode(disposition.RecoveryPath) {
		return fmt.Errorf("task %s rejection_rca disposition restore_mode does not match its recovery_path", taskID)
	}
	if strings.TrimSpace(disposition.Actor) == "" {
		return fmt.Errorf("task %s rejection_rca disposition requires actor", taskID)
	}
	if disposition.DecidedAt.IsZero() {
		return fmt.Errorf("task %s rejection_rca disposition requires decided_at", taskID)
	}
	return nil
}

func validateRepairRequestShape(task *models.Task) error {
	request := task.RepairRequest
	if request.Operation != models.RepairOperationApplyDependencyRepair {
		if strings.TrimSpace(request.Command) == "" {
			return fmt.Errorf("BLOCKED task repair_request without command: %s", task.ID)
		}
		if request.DependencyUpdates != nil {
			return fmt.Errorf("BLOCKED task command-based repair_request must not include dependency_updates: %s", task.ID)
		}
		return nil
	}

	if strings.TrimSpace(request.Target) != task.ID {
		return fmt.Errorf("BLOCKED task declarative repair_request target must match blocked task: %s", task.ID)
	}
	if strings.TrimSpace(request.Command) != "" {
		return fmt.Errorf("BLOCKED task declarative repair_request must not include command: %s", task.ID)
	}
	if len(request.DependencyUpdates) == 0 {
		return fmt.Errorf("BLOCKED task declarative repair_request without dependency_updates: %s", task.ID)
	}

	seenTasks := make(map[string]bool, len(request.DependencyUpdates))
	for i, update := range request.DependencyUpdates {
		updateTaskID := strings.TrimSpace(update.TaskID)
		if updateTaskID == "" {
			return fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].task_id is required: %s", i, task.ID)
		}
		if seenTasks[updateTaskID] {
			return fmt.Errorf("BLOCKED task repair_request has duplicate dependency update task_id %q: %s", updateTaskID, task.ID)
		}
		seenTasks[updateTaskID] = true

		if err := validateExplicitDependencyList(update.ExpectedDependsOn, "expected_depends_on", i, task.ID); err != nil {
			return err
		}
		if err := validateExplicitDependencyList(update.DesiredDependsOn, "desired_depends_on", i, task.ID); err != nil {
			return err
		}
	}
	return nil
}

func validateExplicitDependencyList(values []string, field string, updateIndex int, blockedTaskID string) error {
	if values == nil {
		return fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].%s must be an explicit list: %s", updateIndex, field, blockedTaskID)
	}

	seen := make(map[string]bool, len(values))
	for _, value := range values {
		dependencyID := strings.TrimSpace(value)
		if dependencyID == "" {
			return fmt.Errorf("BLOCKED task repair_request dependency_updates[%d].%s contains an empty task ID: %s", updateIndex, field, blockedTaskID)
		}
		if seen[dependencyID] {
			return fmt.Errorf("BLOCKED task repair_request dependency_updates[%d] has duplicate %s entry %q: %s", updateIndex, field, dependencyID, blockedTaskID)
		}
		seen[dependencyID] = true
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
		if err := models.ValidateValidationPrerequisites(entry.Validation, entry.ValidationPrerequisites); err != nil {
			return fmt.Errorf("task %s output[%d]: %w", task.ID, i, err)
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
