package models

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/roles"
)

// TaskType represents the kind of task, determining which roles participate in its lifecycle.
type TaskType string

const (
	TaskTypeCoding   TaskType = "coding"
	TaskTypePlanning TaskType = "planning"
)

// Role name constants used in task definitions.
// These are aliases for the canonical definitions in the roles package.
const (
	RoleCoder            = roles.Coder
	RoleCodeReviewer     = roles.CodeReviewer
	RoleOrchestrator     = roles.Orchestrator
	RoleCodePlanner      = roles.CodePlanner
	RoleCodePlanReviewer = roles.CodePlanReviewer
	RoleEpicPlanner      = roles.EpicPlanner
	RoleEpicPlanReviewer = roles.EpicPlanReviewer
	RoleUSWriter         = roles.USWriter
	RoleUSReviewer       = roles.USReviewer
)

// taskWorkflows maps each TaskType to its ordered role sequence.
// This is the single source of truth for which roles participate in a task type's lifecycle.
// Access via RoleWorkflow(), HasRole(), and IsValid() — not directly.
var taskWorkflows = map[TaskType][]string{
	TaskTypeCoding:   {RoleCoder, RoleCodeReviewer},
	TaskTypePlanning: {RoleCodePlanner, RoleCodePlanReviewer},
}

// IsValid checks if the task type is known.
func (tt TaskType) IsValid() bool {
	_, ok := taskWorkflows[tt]
	return ok
}

// ValidTaskTypeNames returns sorted names of all valid task types.
func ValidTaskTypeNames() []string {
	names := make([]string, 0, len(taskWorkflows))
	for tt := range taskWorkflows {
		names = append(names, string(tt))
	}
	slices.Sort(names)
	return names
}

// RoleWorkflow returns a copy of the ordered role sequence for this task type.
func (tt TaskType) RoleWorkflow() []string {
	wf := taskWorkflows[tt]
	if wf == nil {
		return nil
	}
	out := make([]string, len(wf))
	copy(out, wf)
	return out
}

// HasRole checks if the given role participates in this task type's workflow.
func (tt TaskType) HasRole(role string) bool {
	return slices.Contains(taskWorkflows[tt], role)
}

// TaskStatus represents the state of a task
type TaskStatus string

const (
	TaskStatusDraft             TaskStatus = "DRAFT"
	TaskStatusReady             TaskStatus = "DRAFT_CODE"
	TaskStatusImplementing      TaskStatus = "IMPLEMENTING_CODE"
	TaskStatusReadyForReview    TaskStatus = "CODE_READY_FOR_REVIEW"
	TaskStatusReviewing         TaskStatus = "REVIEWING_CODE"
	TaskStatusRejected          TaskStatus = "CODE_REJECTED"
	TaskStatusApproved          TaskStatus = "CODE_APPROVED"
	TaskStatusMerged            TaskStatus = "MERGED"
	TaskStatusBlocked           TaskStatus = "BLOCKED"
	TaskStatusAbandoned         TaskStatus = "ABANDONED"
	TaskStatusSuperseded        TaskStatus = "SUPERSEDED"
	TaskStatusIntegrationFailed TaskStatus = "INTEGRATION_FAILED"

	// Code-planning pair states
	TaskStatusDraftCodingPlan     TaskStatus = "DRAFT_CODING_PLAN"
	TaskStatusCodePlanning        TaskStatus = "CODE_PLANNING"
	TaskStatusCodingPlanToReview  TaskStatus = "CODING_PLAN_TO_REVIEW"
	TaskStatusReviewingCodingPlan TaskStatus = "REVIEWING_CODING_PLAN"
	TaskStatusCodingPlanApproved  TaskStatus = "CODING_PLAN_APPROVED"
	TaskStatusCodingPlanRejected  TaskStatus = "CODING_PLAN_REJECTED"

	// Quorum review states (coding-pair)
	TaskStatusPartiallyApproved TaskStatus = "CODE_PARTIALLY_APPROVED"
	TaskStatusReviewingCode2    TaskStatus = "REVIEWING_CODE_2"
)

// IsValid checks if the task status is valid
func (ts TaskStatus) IsValid() bool {
	switch ts {
	case TaskStatusDraft, TaskStatusReady, TaskStatusImplementing,
		TaskStatusReadyForReview, TaskStatusReviewing, TaskStatusRejected,
		TaskStatusApproved, TaskStatusMerged, TaskStatusBlocked,
		TaskStatusAbandoned, TaskStatusSuperseded, TaskStatusIntegrationFailed,
		TaskStatusDraftCodingPlan, TaskStatusCodePlanning,
		TaskStatusCodingPlanToReview, TaskStatusReviewingCodingPlan,
		TaskStatusCodingPlanApproved, TaskStatusCodingPlanRejected,
		TaskStatusPartiallyApproved, TaskStatusReviewingCode2:
		return true
	}
	return false
}

// IsTerminal checks if the task status is terminal (no further transitions)
func (ts TaskStatus) IsTerminal() bool {
	return ts == TaskStatusMerged || ts == TaskStatusAbandoned || ts == TaskStatusSuperseded
}

// IsSprintTerminal checks if the task status is terminal for sprint completion purposes.
// MERGED is the universal sprint-terminal state for all role-pairs.
func (ts TaskStatus) IsSprintTerminal() bool {
	return ts.IsTerminal()
}

// IsPipelineValid checks if the status is valid in a pipeline context.
// A status is valid if it is a hardcoded valid status (legacy) OR appears in
// the provided set of pipeline-declared states OR is a cross-cutting meta-state.
func (ts TaskStatus) IsPipelineValid(declaredStates []TaskStatus) bool {
	if ts.IsValid() {
		return true
	}
	return slices.Contains(declaredStates, ts)
}

// CanPipelineTransition checks if a transition from ts to the given target is
// valid using the provided transition map (built from pipeline config).
func (ts TaskStatus) CanPipelineTransition(to TaskStatus, transitions map[TaskStatus][]TaskStatus) bool {
	return slices.Contains(transitions[ts], to)
}

// IsPipelineSprintTerminal checks if the status is terminal for sprint purposes
// using pipeline-defined terminal states. Universal terminals (MERGED, ABANDONED,
// SUPERSEDED) are always considered sprint-terminal.
func (ts TaskStatus) IsPipelineSprintTerminal(terminalStates []TaskStatus) bool {
	return ts.IsTerminal() || slices.Contains(terminalStates, ts)
}

// Approval records a single review approval with provider metadata.
// Used by review quorum to track who approved and from which provider.
type Approval struct {
	Agent     string    `yaml:"agent"`
	Provider  string    `yaml:"provider"`
	Timestamp time.Time `yaml:"timestamp"`
}

// Task represents a single task in the Liza system
type Task struct {
	ID                  string             `yaml:"id"`
	Type                TaskType           `yaml:"type,omitempty"`
	RolePair            string             `yaml:"role_pair,omitempty"`
	Description         string             `yaml:"description"`
	Status              TaskStatus         `yaml:"status"`
	Priority            int                `yaml:"priority"`
	AssignedTo          *string            `yaml:"assigned_to,omitempty"`
	Worktree            *string            `yaml:"worktree,omitempty"`
	BaseCommit          *string            `yaml:"base_commit,omitempty"`
	Iteration           int                `yaml:"iteration,omitempty"`
	Output              []OutputEntry      `yaml:"output,omitempty"`
	ParentTask          *string            `yaml:"parent_task,omitempty"`
	TransitionsExecuted map[string]bool    `yaml:"transitions_executed,omitempty"`
	Exit42RestartCount  int                `yaml:"exit42_restart_count,omitempty"`
	ReviewCyclesCurrent int                `yaml:"review_cycles_current,omitempty"`
	ReviewCyclesTotal   int                `yaml:"review_cycles_total,omitempty"`
	ReviewCommit        *string            `yaml:"review_commit,omitempty"`
	ReviewingBy         *string            `yaml:"reviewing_by,omitempty"`
	ReviewLeaseExpires  *time.Time         `yaml:"review_lease_expires,omitempty"`
	ApprovedBy          *string            `yaml:"approved_by,omitempty"`
	Approvals           []Approval         `yaml:"approvals,omitempty"`
	MergeCommit         *string            `yaml:"merge_commit,omitempty"`
	LeaseExpires        *time.Time         `yaml:"lease_expires,omitempty"`
	SpecRef             string             `yaml:"spec_ref"`
	PlanRef             string             `yaml:"plan_ref,omitempty"`
	DoneWhen            string             `yaml:"done_when"`
	Scope               string             `yaml:"scope"`
	RejectionReason     *string            `yaml:"rejection_reason,omitempty"`
	BlockedReason       *string            `yaml:"blocked_reason,omitempty"`
	BlockedQuestions    []string           `yaml:"blocked_questions,omitempty"`
	SupersededBy        []string           `yaml:"superseded_by,omitempty"`
	Supersedes          *string            `yaml:"supersedes,omitempty"`
	RescopeReason       *string            `yaml:"rescope_reason,omitempty"`
	FailedBy            []string           `yaml:"failed_by,omitempty"`
	Attempt             int                `yaml:"attempt,omitempty"`
	DependsOn           []string           `yaml:"depends_on,omitempty"`
	IntegrationFix      bool               `yaml:"integration_fix,omitempty"`
	HandoffPending      bool               `yaml:"handoff_pending,omitempty"`
	HandoffEvents       []HandoffEvent     `yaml:"handoff_events,omitempty"`
	MaxIterations       int                `yaml:"max_iterations,omitempty"`
	Created             time.Time          `yaml:"created"`
	History             []TaskHistoryEntry `yaml:"history"`
	Extra               map[string]any     `yaml:",inline"`
}

// OutputEntry represents a structured subtask definition produced by a doer role.
// When a task completes with output[], each entry defines a downstream child task.
type OutputEntry struct {
	Desc      string   `yaml:"desc"`
	DoneWhen  string   `yaml:"done_when"`
	Scope     string   `yaml:"scope"`
	SpecRef   string   `yaml:"spec_ref"`
	PlanRef   string   `yaml:"plan_ref,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty"`
}

// ValidateDependsOn checks that DependsOn indices are valid references within
// [0, totalEntries), are numeric, and don't self-reference. Returns a plain error
// suitable for wrapping by callers into their preferred error type.
func ValidateDependsOn(deps []string, entryIndex, totalEntries int) error {
	for _, ref := range deps {
		idx, err := strconv.Atoi(ref)
		if err != nil {
			return fmt.Errorf("output[%d] depends_on contains non-numeric reference %q", entryIndex, ref)
		}
		if idx < 0 || idx >= totalEntries {
			return fmt.Errorf("output[%d] depends_on reference %q out of range [0, %d)", entryIndex, ref, totalEntries)
		}
		if idx == entryIndex {
			return fmt.Errorf("output[%d] depends_on references itself", entryIndex)
		}
	}
	return nil
}

// PipelineResolver provides pipeline state resolution for tasks with role-pairs.
// Implemented by pipeline.Resolver.
type PipelineResolver interface {
	DoerRole(rolePair string) (string, error)
	ReviewerRole(rolePair string) (string, error)
	InitialStatus(rolePair string) (TaskStatus, error)
	RejectedStatus(rolePair string) (TaskStatus, error)
	SubmittedStatus(rolePair string) (TaskStatus, error)
	ReviewingStatus(rolePair string) (TaskStatus, error)
	ExecutingStatus(rolePair string) (TaskStatus, error)
	ApprovedStatus(rolePair string) (TaskStatus, error)
	PartiallyApprovedStatus(rolePair string) (TaskStatus, error)
	Reviewing2Status(rolePair string) (TaskStatus, error)
}

// EffectiveType returns the task's type, defaulting to TaskTypeCoding when empty (backward compat).
func (t *Task) EffectiveType() TaskType {
	if t.Type == "" {
		return TaskTypeCoding
	}
	return t.Type
}

// EffectiveAttempt returns the task's attempt number, defaulting to 1 when
// Attempt is 0 (unset) for backward compatibility with existing tasks.
// When Attempt is unset and legacy Extra["attempted"] exists, derives the
// attempt count from len(attempted) + 1.
func (t *Task) EffectiveAttempt() int {
	if t.Attempt >= 1 {
		return t.Attempt
	}
	if attempted, ok := t.legacyAttemptedList(); ok && len(attempted) > 0 {
		return min(len(attempted)+1, 2)
	}
	return 1
}

// MigrateAttemptedField converts the legacy Extra["attempted"] list into the
// Attempt field and removes the legacy key. Returns true if the task was changed.
// No-op if the legacy field is absent or Attempt is already set.
func (t *Task) MigrateAttemptedField() bool {
	if t.Attempt >= 1 {
		return false
	}
	if t.Extra == nil {
		return false
	}
	v, exists := t.Extra["attempted"]
	if !exists {
		return false
	}
	list, ok := v.([]any)
	if !ok {
		// Wrong type — clean up the key but don't set Attempt.
		delete(t.Extra, "attempted")
		return true
	}
	if len(list) > 0 {
		t.Attempt = min(len(list)+1, 2)
	}
	delete(t.Extra, "attempted")
	return true
}

// legacyAttemptedList returns the Extra["attempted"] slice and true if it
// exists and is a []any, or nil and false otherwise.
func (t *Task) legacyAttemptedList() ([]any, bool) {
	if t.Extra == nil {
		return nil, false
	}
	v, exists := t.Extra["attempted"]
	if !exists {
		return nil, false
	}
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	return list, true
}

// ApprovalCount returns the number of approvals recorded on this task.
func (t *Task) ApprovalCount() int {
	return len(t.Approvals)
}

// HasProviderDiversity returns true if approvals come from at least 2 distinct providers.
func (t *Task) HasProviderDiversity() bool {
	if len(t.Approvals) < 2 {
		return false
	}
	first := t.Approvals[0].Provider
	for _, a := range t.Approvals[1:] {
		if a.Provider != first {
			return true
		}
	}
	return false
}

// ClearApprovals removes all recorded approvals.
// Used when a rejection at any review stage requires fresh re-review.
func (t *Task) ClearApprovals() {
	t.Approvals = nil
}

// LastApprover returns the agent ID of the most recent approval,
// or empty string if no approvals exist.
func (t *Task) LastApprover() string {
	if len(t.Approvals) == 0 {
		return ""
	}
	return t.Approvals[len(t.Approvals)-1].Agent
}

// TransitionWith validates and applies a status transition using a transition map
// built from pipeline config. The target status must exist as a key in the
// transition map (i.e., be a declared state).
func (t *Task) TransitionWith(to TaskStatus, transitions map[TaskStatus][]TaskStatus) error {
	if !slices.Contains(transitions[t.Status], to) {
		return fmt.Errorf("invalid task transition: %s → %s (task %s)", t.Status, to, t.ID)
	}
	if _, declared := transitions[to]; !declared {
		return fmt.Errorf("target status %s is not a declared state in the transition map (task %s)", to, t.ID)
	}
	t.Status = to
	return nil
}

// IsClaimable checks if a task is claimable by the given role based on its
// pipeline-defined states, type, and dependencies.
// The role parameter uses the unified hyphenated form (e.g. "code-reviewer").
func (t *Task) IsClaimable(role string, allTasks []Task, pr PipelineResolver) bool {
	if t.RolePair == "" || pr == nil {
		return false
	}
	if t.AssignedTo != nil && strings.HasPrefix(*t.AssignedTo, "$") {
		return false
	}
	if !t.isClaimablePipeline(role, pr) {
		return false
	}
	return checkDependencies(t, allTasks)
}

// isClaimablePipeline checks claimability using pipeline-resolved states.
// The role parameter uses the unified hyphenated form, matching what the
// pipeline resolver returns from DoerRole/ReviewerRole.
func (t *Task) isClaimablePipeline(role string, pr PipelineResolver) bool {
	doerRole, err := pr.DoerRole(t.RolePair)
	if err != nil {
		return false
	}
	reviewerRole, err := pr.ReviewerRole(t.RolePair)
	if err != nil {
		return false
	}

	switch role {
	case doerRole:
		initial, err := pr.InitialStatus(t.RolePair)
		if err != nil {
			return false
		}
		rejected, err := pr.RejectedStatus(t.RolePair)
		if err != nil {
			return false
		}
		return t.Status == initial || t.Status == rejected || t.Status == TaskStatusIntegrationFailed

	case reviewerRole:
		// Defense-in-depth: tasks in review-eligible states must have review_commit.
		// Without it, the task is corrupted and should not be claimable.
		if t.ReviewCommit == nil {
			return false
		}
		if t.ReviewingBy != nil {
			if t.ReviewLeaseExpires == nil || t.ReviewLeaseExpires.After(time.Now()) {
				return false // another reviewer is actively awaiting resubmission
			}
			// Lease expired — stale claim, allow reclaiming
		}
		submitted, err := pr.SubmittedStatus(t.RolePair)
		if err != nil {
			return false
		}
		if t.Status == submitted {
			return true
		}
		// Quorum > 1: partially_approved tasks need a second reviewer.
		partiallyApproved, err := pr.PartiallyApprovedStatus(t.RolePair)
		if err == nil && t.Status == partiallyApproved {
			return true
		}
		return false

	default:
		return false
	}
}

// IsApprovedForMerge checks if a task is in an approved state eligible for merge.
func IsApprovedForMerge(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	approved, err := pr.ApprovedStatus(task.RolePair)
	return err == nil && task.Status == approved
}

// IsSubmittedStatus checks if a task is in a submitted state.
func IsSubmittedStatus(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	submitted, err := pr.SubmittedStatus(task.RolePair)
	return err == nil && task.Status == submitted
}

// IsExecutingStatus checks if a task is in an executing state.
func IsExecutingStatus(task *Task, pr PipelineResolver) bool {
	if task.RolePair == "" || pr == nil {
		return false
	}
	executing, err := pr.ExecutingStatus(task.RolePair)
	return err == nil && task.Status == executing
}

// checkDependencies returns true if all dependencies of the task are satisfied
// (MERGED or SUPERSEDED). ABANDONED is terminal but not satisfying — the work
// was dropped, not completed or replaced.
func checkDependencies(t *Task, allTasks []Task) bool {
	if allTasks != nil && len(t.DependsOn) > 0 {
		for _, depID := range t.DependsOn {
			depSatisfied := false
			for _, task := range allTasks {
				if task.ID == depID && (task.Status == TaskStatusMerged || task.Status == TaskStatusSuperseded) {
					depSatisfied = true
					break
				}
			}
			if !depSatisfied {
				return false
			}
		}
	}
	return true
}

// TopPriorityTier returns all candidates that share the highest priority
// (lowest number). Returns nil if candidates is empty.
func TopPriorityTier(candidates []*Task) []*Task {
	if len(candidates) == 0 {
		return nil
	}

	bestPriority := candidates[0].Priority
	for _, t := range candidates[1:] {
		if t.Priority < bestPriority {
			bestPriority = t.Priority
		}
	}

	var tier []*Task
	for _, t := range candidates {
		if t.Priority == bestPriority {
			tier = append(tier, t)
		}
	}

	return tier
}
