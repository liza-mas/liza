package statevalidate

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
func validateRequiredFields(v *violations, state *models.State, projectRoot string, skipSpecFileCheck bool) {
	if state.Version == 0 {
		v.add(fmt.Errorf("missing required field 'version'"))
	}

	if state.Goal.ID == "" {
		v.add(fmt.Errorf("missing required field 'goal'"))
	}

	if state.Tasks == nil {
		v.add(fmt.Errorf("missing required field 'tasks'"))
	}

	if state.Agents == nil {
		v.add(fmt.Errorf("missing required field 'agents'"))
	}

	if state.Config.IntegrationBranch == "" {
		v.add(fmt.Errorf("missing required field 'config'"))
	}

	if state.Sprint.ID == "" {
		v.add(fmt.Errorf("missing required field 'sprint'"))
	}

	if !skipSpecFileCheck && state.Goal.SpecRef != "" {
		if err := checkSpecFileExists(projectRoot, state.Goal.SpecRef, state.Config.IntegrationBranch); err != nil {
			v.add(fmt.Errorf("goal %w", err))
		}
	}
}

// validateTaskStates ensures every task has a valid status (either hardcoded or
// pipeline-declared), a valid task type, and — for pipeline-configured goals —
// a role_pair that maps to a known pipeline role pair. Prevents tasks from
// entering undefined lifecycle states.
func validateTaskStates(v *violations, state *models.State, resolver *pipeline.Resolver) {
	for _, task := range state.Tasks {
		statusValid := task.Status.IsValid()
		if !statusValid && resolver != nil {
			// Accept pipeline-declared states and cross-cutting meta-states
			statusValid = task.Status.IsPipelineValid(resolver.AllDeclaredStates())
		}
		if !statusValid {
			v.add(fmt.Errorf("unknown task status '%s' for task %s", task.Status, task.ID))
		}
		if !task.EffectiveType().IsValid() {
			v.add(fmt.Errorf("unknown task type '%s' for task %s", task.Type, task.ID))
		}

		// Pipeline-goal tasks: role_pair is required unconditionally
		if resolver != nil {
			if task.RolePair == "" {
				v.add(fmt.Errorf("task %s missing role_pair (required for pipeline-configured goals)", task.ID))
			} else if _, err := resolver.InitialStatus(task.RolePair); err != nil {
				v.add(fmt.Errorf("task %s has invalid role_pair %q: %w", task.ID, task.RolePair, err))
			}
		}
	}
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
// Each constraint is checked on its own, so one defect of a task does not hide
// another.
func validateTaskInvariants(v *violations, state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) {
	assignments := make(map[string][]string) // agent ID -> task IDs
	taskIDs := buildTaskIDSet(state.Tasks)
	sc := newStatusClassifier(resolver, cfg)

	for _, task := range state.Tasks {
		validateTaskLifecycle(v, &task)
		validateStatusFields(v, &task, &sc)
		for _, err := range models.ValidationSafetyViolations("validation", task.Validation, task.DestructiveDB) {
			v.add(fmt.Errorf("task %s %w", task.ID, err))
		}
		for _, err := range models.ValidationPrerequisiteViolations(task.Validation, task.ValidationPrerequisites) {
			v.add(fmt.Errorf("task %s %w", task.ID, err))
		}
		for _, err := range models.RuntimeInputViolations(task.Validation, task.RuntimeInputs) {
			v.add(fmt.Errorf("task %s %w", task.ID, err))
		}

		// Track assignments for duplicate check (executing tasks count as active)
		if task.AssignedTo != nil && sc.IsExecuting(task.Status) {
			assignments[*task.AssignedTo] = append(assignments[*task.AssignedTo], task.ID)
		}

		// Executing task worktree path must exist (only check if projectRoot is not empty to allow tests)
		if sc.IsExecuting(task.Status) && task.Worktree != nil && projectRoot != "" {
			wtPath := filepath.Join(projectRoot, *task.Worktree)
			if _, err := os.Stat(wtPath); os.IsNotExist(err) {
				// The status is context, not the constraint: moving between
				// executing statuses keeps the same missing directory.
				v.addID(fmt.Sprintf("task %s worktree=%s directory does not exist", task.ID, *task.Worktree),
					fmt.Errorf("%s task %s has worktree=%s but directory does not exist", task.Status, task.ID, *task.Worktree))
			}
		}

		if requiresCompletionFields(task.Status, resolver, cfg) {
			if task.DoneWhen == "" {
				v.add(fmt.Errorf("non-DRAFT task missing done_when: %s", task.ID))
			}
			if task.SpecRef == "" {
				v.add(fmt.Errorf("non-DRAFT task missing spec_ref: %s", task.ID))
			}
		}

		if !artifactRefsRetired(task) {
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
				v.add(fmt.Errorf("task %s has integration_fix:true but no INTEGRATION_FAILED event in history", task.ID))
			}
		}

		// failed_by must have unique agent IDs
		if len(task.FailedBy) > 0 {
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

		// parent_task / parent_tasks must reference existing tasks
		for _, parentID := range task.EffectiveParentTasks() {
			if !taskIDs[parentID] {
				v.add(fmt.Errorf("task %s has parent_task referencing non-existent task '%s'", task.ID, parentID))
			}
		}

		validateTaskOutput(v, &task, !artifactRefsRetired(task))
		validateAcceptanceState(v, &task)
		validatePlanCheck(v, &task, resolver)

		// Attempt must be 0 (unset/legacy), 1, or 2
		if task.Attempt < 0 || task.Attempt > 2 {
			v.add(fmt.Errorf("task %s has invalid attempt value %d", task.ID, task.Attempt))
		}

		// Cross-check: attempt 2 in initial status must have reset counters
		if task.Attempt == 2 && sc.IsInitial(task.Status) {
			if task.Iteration != 0 {
				v.add(fmt.Errorf("task %s at attempt 2 in initial status has non-zero iteration %d", task.ID, task.Iteration))
			}
			if task.ReviewCyclesCurrent != 0 {
				v.add(fmt.Errorf("task %s at attempt 2 in initial status has non-zero review_cycles_current %d", task.ID, task.ReviewCyclesCurrent))
			}
		}
	}

	// Check for duplicate assignments
	agents := make([]string, 0, len(assignments))
	for agent := range assignments {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		taskIDs := assignments[agent]
		sort.Strings(taskIDs)
		// One violation per conflicting pair: releasing one task leaves the
		// other pairs' identities intact, and assigning a further task adds
		// pairs of its own.
		for i := range taskIDs {
			for _, other := range taskIDs[i+1:] {
				v.add(fmt.Errorf("agent %s assigned to multiple active tasks simultaneously: [%s %s]", agent, taskIDs[i], other))
			}
		}
	}
}

// validatePlanCheck checks the persisted shape of an orchestrator plan
// disposition. Admission rules (domain, dependencies, sticky holds) live in ops.
func validatePlanCheck(v *violations, task *models.Task, resolver *pipeline.Resolver) {
	check := task.PlanCheck
	if check == nil {
		return
	}
	switch check.Verdict {
	case models.PlanCheckPassed:
	case models.PlanCheckHeld:
		if strings.TrimSpace(check.Ask) == "" {
			v.add(fmt.Errorf("task %s plan_check held requires ask", task.ID))
		}
	default:
		v.add(fmt.Errorf("task %s plan_check has invalid verdict %q", task.ID, check.Verdict))
	}
	if check.By == "" {
		v.add(fmt.Errorf("task %s plan_check requires by and at (missing by)", task.ID))
	}
	if check.At.IsZero() {
		v.add(fmt.Errorf("task %s plan_check requires by and at (missing at)", task.ID))
	}
	if task.Status != models.TaskStatusMerged {
		v.addID(fmt.Sprintf("task %s plan_check requires MERGED status", task.ID),
			fmt.Errorf("task %s plan_check requires MERGED status, got %s", task.ID, task.Status))
	}
	if resolver != nil && !resolver.TransitionSourcePairs()[task.RolePair] {
		v.add(fmt.Errorf("task %s plan_check on non-planning role_pair %q", task.ID, task.RolePair))
	}
}

// Admission performs repository checks. State validation only checks persisted
// shape, leaving missing receipts repairable through update-review-commit.
func validateAcceptanceState(v *violations, task *models.Task) {
	validateArchivedFields(v, task)
	source := task.AcceptanceSource
	if source == nil {
		if task.AcceptanceReceipt != nil || len(task.Archived) > 0 {
			v.add(fmt.Errorf("task %s acceptance_receipt requires acceptance_source", task.ID))
		}
		return
	}
	if source.Ref == "" {
		v.add(fmt.Errorf("task %s acceptance_source requires ref and parent_task (missing ref)", task.ID))
	}
	if source.ParentTask == "" {
		v.add(fmt.Errorf("task %s acceptance_source requires ref and parent_task (missing parent_task)", task.ID))
	}
	for _, id := range []struct{ field, value string }{
		{"commit", source.Commit},
		{"blob", source.Blob},
		{"parent_review_commit", source.ParentReviewCommit},
	} {
		if _, err := hex.DecodeString(id.value); err != nil || len(id.value) != 40 || strings.ToLower(id.value) != id.value {
			v.add(fmt.Errorf("task %s acceptance_source requires immutable lowercase object IDs (%s)", task.ID, id.field))
		}
	}
	receipt := task.AcceptanceReceipt
	if receipt == nil {
		return
	}
	for _, check := range []struct {
		name   string
		broken bool
	}{
		{"version", receipt.Version != 1},
		{"review_commit", task.ReviewCommit == nil || receipt.ReviewCommit != *task.ReviewCommit},
		{"source", receipt.Source != *source},
	} {
		if check.broken {
			v.add(fmt.Errorf("task %s acceptance_receipt does not match its source and review_commit (%s)", task.ID, check.name))
		}
	}
	for _, check := range []struct {
		name   string
		broken bool
	}{
		{"manifest_path", receipt.ManifestPath == ""},
		{"mappings", len(receipt.Mappings) == 0 || len(receipt.Mappings) > 256},
		{"commands", len(receipt.Commands) > 64},
	} {
		if check.broken {
			v.add(fmt.Errorf("task %s acceptance_receipt has invalid manifest or result bounds (%s)", task.ID, check.name))
		}
	}
	if _, err := hex.DecodeString(receipt.ManifestBlob); err != nil || len(receipt.ManifestBlob) != 40 {
		v.add(fmt.Errorf("task %s acceptance_receipt requires manifest blob identity", task.ID))
	}
	totalOutput := 0
	// A receipt is written once and never edited, so a command's index is a
	// stable owner: the same defect on two commands stays two violations.
	for i, command := range receipt.Commands {
		totalOutput += len(command.Output)
		v.within(fmt.Sprintf("task %s acceptance_receipt command %d", task.ID, i), func(v *violations) {
			for _, check := range []struct {
				name   string
				broken bool
			}{
				{"exit code", command.ExitCode != 0},
				{"started_at", command.StartedAt.IsZero()},
				{"finished_at", command.FinishedAt.Before(command.StartedAt)},
			} {
				if check.broken {
					v.add(fmt.Errorf("task %s acceptance_receipt contains unsuccessful execution (command %d %s)", task.ID, i, check.name))
				}
			}
			if _, err := hex.DecodeString(command.CommandSHA256); err != nil || len(command.CommandSHA256) != 64 {
				v.add(fmt.Errorf("task %s acceptance_receipt requires canonical command identity (command %d)", task.ID, i))
			}
		})
	}
	if totalOutput > 1024*1024 {
		v.add(fmt.Errorf("task %s acceptance_receipt output exceeds 1 MiB", task.ID))
	}
}

// validateArchivedFields checks the refs of fields moved to archive objects.
// Like receipts, it checks shape only and opens no files: one ref per field,
// only on terminal tasks, never alongside the live value it replaces.
func validateArchivedFields(v *violations, task *models.Task) {
	if len(task.Archived) == 0 {
		return
	}
	if !task.Status.IsTerminal() {
		v.addID(fmt.Sprintf("task %s has archived fields but is not terminal", task.ID),
			fmt.Errorf("task %s in status %s has archived fields; only terminal tasks may", task.ID, task.Status))
	}
	seen := map[string]bool{}
	for _, ref := range task.Archived {
		if ref.Field != models.ArchivedFieldAcceptanceReceipt {
			v.add(fmt.Errorf("task %s archived field %q is not archivable", task.ID, ref.Field))
		}
		if seen[ref.Field] {
			v.add(fmt.Errorf("task %s has more than one archived %s", task.ID, ref.Field))
		}
		seen[ref.Field] = true
		if _, err := hex.DecodeString(ref.SHA256); err != nil || len(ref.SHA256) != 64 || strings.ToLower(ref.SHA256) != ref.SHA256 {
			v.add(fmt.Errorf("task %s archived %s requires a lowercase SHA-256 digest", task.ID, ref.Field))
		}
		if ref.ArchivedAt.IsZero() {
			v.add(fmt.Errorf("task %s archived %s requires archived_at", task.ID, ref.Field))
		}
	}
	if seen[models.ArchivedFieldAcceptanceReceipt] && task.AcceptanceReceipt != nil {
		v.add(fmt.Errorf("task %s has both a live and an archived acceptance_receipt", task.ID))
	}
}

// validateStatusFields checks that each task status has the fields required by
// its lifecycle phase (e.g. executing tasks need assigned_to, worktree,
// base_commit, and lease_expires; reviewing tasks need reviewing_by and
// review_lease_expires). Prevents tasks from entering states without the
// metadata needed for agents to operate on them.
//
// Messages name the status for the operator. Identities name the phase and
// the field instead, so a task moving between statuses of the same phase keeps
// its violation rather than trading an old one for a new one.
func validateStatusFields(v *violations, task *models.Task, sc *statusClassifier) {
	missing := func(phase, field string) {
		v.addID(fmt.Sprintf("task %s: %s task without %s", task.ID, phase, field),
			fmt.Errorf("%s task without %s: %s", task.Status, field, task.ID))
	}

	if sc.IsInitial(task.Status) && task.AssignedTo != nil {
		v.addID(fmt.Sprintf("task %s: initial task with assigned_to", task.ID),
			fmt.Errorf("%s task with assigned_to: %s", task.Status, task.ID))
	}

	if sc.IsExecuting(task.Status) {
		if task.AssignedTo == nil {
			missing("executing", "assigned_to")
		}
		if task.Worktree == nil {
			missing("executing", "worktree")
		}
		if !task.IntegrationFix && task.BaseCommit == nil {
			missing("executing", "base_commit")
		}
		if task.LeaseExpires == nil {
			missing("executing", "lease_expires")
		}
	}

	if sc.IsSubmitted(task.Status) && task.ReviewCommit == nil {
		missing("submitted", "review_commit")
	}

	if sc.IsReviewing(task.Status) {
		if task.ReviewingBy == nil {
			missing("reviewing", "reviewing_by")
		}
		if task.ReviewLeaseExpires == nil {
			missing("reviewing", "review_lease_expires")
		}
		if task.ReviewCommit == nil {
			missing("reviewing", "review_commit")
		}
	}

	if sc.IsApproved(task.Status) && task.ReviewCommit == nil {
		missing("approved", "review_commit")
	}
	if task.IntegrationFailure != nil && disallowsIntegrationFailure(task.Status, sc) {
		v.addID(fmt.Sprintf("task %s: stale integration_failure outside integration recovery", task.ID),
			fmt.Errorf("%s task has stale integration_failure outside integration recovery: %s", task.Status, task.ID))
	}

	if task.Status == models.TaskStatusMerged && task.Worktree != nil {
		v.add(fmt.Errorf("MERGED task still has worktree: %s", task.ID))
	}

	if task.Status == models.TaskStatusBlocked {
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

	if sc.IsRejected(task.Status) {
		if task.RejectionReason == nil {
			missing("rejected", "rejection_reason")
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

	if task.Status == models.TaskStatusSuperseded {
		if task.RescopeReason == nil {
			v.add(fmt.Errorf("SUPERSEDED task without rescope_reason: %s", task.ID))
		}
	}

	validateTaskRejectionRCA(v, task)
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
func validateTaskRejectionRCA(v *violations, task *models.Task) {
	validateRejectionRCAGateReason(v, task)
	record := task.RejectionRCA
	if record == nil {
		return
	}
	validateRejectionRCASeededFields(v, task.ID, record)
	// A fingerprint is written only together with the caller fields, so it is
	// the marker that distinguishes a seeded record from a recorded one.
	recorded := record.Fingerprint != ""
	if recorded {
		validateRejectionRCARecordedFields(v, task.ID, record)
	}
	validateRejectionRCADisposition(v, task.ID, record.Disposition, recorded)
}

func validateRejectionRCAGateReason(v *violations, task *models.Task) {
	if task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() {
		return
	}
	reason := ""
	if task.BlockedReason != nil {
		reason = strings.TrimSpace(*task.BlockedReason)
	}
	if strings.HasPrefix(reason, models.BlockedReasonRejectionRCARequired) {
		return
	}
	v.add(fmt.Errorf("BLOCKED task with an open rejection_rca gate requires a blocked_reason starting with %s: %s",
		models.BlockedReasonRejectionRCARequired, task.ID))
}

func validateRejectionRCASeededFields(v *violations, taskID string, record *models.RejectionRCARecord) {
	if record.SchemaVersion != models.RejectionRCASchemaVersion {
		v.add(fmt.Errorf("task %s rejection_rca schema_version must be %d", taskID, models.RejectionRCASchemaVersion))
	}
	if record.Threshold < 1 {
		v.add(fmt.Errorf("task %s rejection_rca threshold must be at least 1", taskID))
	}
	if record.RejectionCount < record.Threshold {
		v.add(fmt.Errorf("task %s rejection_rca rejection_count must be at least its threshold: %d", taskID, record.Threshold))
	}
	if record.GatedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca requires gated_at", taskID))
	}
}

func validateRejectionRCARecordedFields(v *violations, taskID string, record *models.RejectionRCARecord) {
	for _, diagnostic := range ValidateRejectionRCARequest(record.Request()) {
		v.add(fmt.Errorf("task %s rejection_rca %s violates: %s", taskID, diagnostic.Field, diagnostic.Constraint))
	}
	if strings.TrimSpace(record.RecordedBy) == "" {
		v.add(fmt.Errorf("task %s rejection_rca requires recorded_by", taskID))
	}
	if record.RecordedAt == nil || record.RecordedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca requires recorded_at", taskID))
	}
}

func validateRejectionRCADisposition(v *violations, taskID string, disposition *models.RejectionRCADisposition, recorded bool) {
	if disposition == nil {
		return
	}
	if !recorded {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires a recorded RCA", taskID))
	}
	if !models.IsRecoveryPath(disposition.RecoveryPath) {
		v.add(fmt.Errorf("task %s rejection_rca disposition has an unknown recovery_path", taskID))
	} else if disposition.RestoreMode != models.RejectionRCARestoreMode(disposition.RecoveryPath) {
		v.add(fmt.Errorf("task %s rejection_rca disposition restore_mode does not match its recovery_path", taskID))
	}
	if strings.TrimSpace(disposition.Actor) == "" {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires actor", taskID))
	}
	if disposition.DecidedAt.IsZero() {
		v.add(fmt.Errorf("task %s rejection_rca disposition requires decided_at", taskID))
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
