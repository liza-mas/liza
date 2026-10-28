package models

import (
	"strings"
	"time"
)

// Approval records a single review approval with provider metadata.
// Used by review quorum to track who approved and from which provider.
type Approval struct {
	Agent     string    `yaml:"agent"`
	Provider  string    `yaml:"provider"`
	Model     string    `yaml:"model,omitempty"`
	Timestamp time.Time `yaml:"timestamp"`
}

// RepairOperationApplyDependencyRepair identifies a declarative dependency repair request.
const RepairOperationApplyDependencyRepair = "apply-dependency-repair"

// DependencyUpdate describes the complete expected and desired direct
// dependency state for one task in a declarative graph repair.
type DependencyUpdate struct {
	TaskID            string   `yaml:"task_id" json:"task_id"`
	ExpectedDependsOn []string `yaml:"expected_depends_on" json:"expected_depends_on"`
	DesiredDependsOn  []string `yaml:"desired_depends_on" json:"desired_depends_on"`
}

// RepairRequest carries a structured request from a blocked doer to the
// orchestrator for state repairs that the doer is not authorized to perform.
type RepairRequest struct {
	Operation         string             `yaml:"operation" json:"operation"`
	Target            string             `yaml:"target,omitempty" json:"target,omitempty"`
	Command           string             `yaml:"command,omitempty" json:"command,omitempty"`
	DependencyUpdates []DependencyUpdate `yaml:"dependency_updates,omitempty" json:"dependency_updates,omitempty"`
	Evidence          []string           `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Validation        []string           `yaml:"validation,omitempty" json:"validation,omitempty"`
}

// HasStructuredFailureEvidence reports whether evidence contains either a
// process-style failure record (exit_code plus stderr/error) or a tool/provider
// failure record (error without a process exit code).
func (r RepairRequest) HasStructuredFailureEvidence() bool {
	for _, value := range r.Evidence {
		lower := strings.ToLower(value)
		hasError := strings.Contains(lower, "error=")
		if hasError && !strings.Contains(lower, "exit_code=") {
			return true
		}
		if strings.Contains(lower, "exit_code=") &&
			(strings.Contains(lower, "stderr=") || hasError) {
			return true
		}
	}
	return false
}

// Task represents a single task in the Liza system
type Task struct {
	ID                      string                   `yaml:"id"`
	Type                    TaskType                 `yaml:"type,omitempty"`
	RolePair                string                   `yaml:"role_pair,omitempty"`
	Description             string                   `yaml:"description"`
	Status                  TaskStatus               `yaml:"status"`
	Priority                int                      `yaml:"priority"`
	AssignedTo              *string                  `yaml:"assigned_to,omitempty"`
	Worktree                *string                  `yaml:"worktree,omitempty"`
	BaseCommit              *string                  `yaml:"base_commit,omitempty"`
	Iteration               int                      `yaml:"iteration,omitempty"`
	Continuation            bool                     `yaml:"continuation,omitempty"` // next claim resumes the current iteration; set by unblock-task
	Output                  []OutputEntry            `yaml:"output,omitempty"`
	Decomposition           *DecompositionManifest   `yaml:"decomposition,omitempty" json:"decomposition,omitempty"`
	ParentTask              *string                  `yaml:"parent_task,omitempty"`
	ParentTasks             []string                 `yaml:"parent_tasks,omitempty"`
	TransitionsExecuted     map[string]bool          `yaml:"transitions_executed,omitempty"`
	PlanCheck               *PlanCheck               `yaml:"plan_check,omitempty" json:"plan_check,omitempty"`
	AmendsPlan              string                   `yaml:"amends_plan,omitempty" json:"amends_plan,omitempty"`
	AmendmentMode           PlanAmendmentMode        `yaml:"amendment_mode,omitempty" json:"amendment_mode,omitempty"`
	PlanningChange          *PlanningChange          `yaml:"planning_change,omitempty" json:"planning_change,omitempty"`
	PlanAmendment           *PlanAmendment           `yaml:"plan_amendment,omitempty" json:"plan_amendment,omitempty"`
	ValidationNotes         []ValidationNote         `yaml:"validation_notes,omitempty" json:"validation_notes,omitempty"`
	Exit42RestartCount      int                      `yaml:"exit42_restart_count,omitempty"`
	ReviewCyclesCurrent     int                      `yaml:"review_cycles_current,omitempty"`
	ReviewCyclesTotal       int                      `yaml:"review_cycles_total,omitempty"`
	ReviewCommit            *string                  `yaml:"review_commit,omitempty"`
	AcceptanceSource        *AcceptanceSource        `yaml:"acceptance_source,omitempty" json:"acceptance_source,omitempty"`
	AcceptanceReceipt       *AcceptanceReceipt       `yaml:"acceptance_receipt,omitempty" json:"acceptance_receipt,omitempty"`
	Archived                []ArchivedFieldRef       `yaml:"archived,omitempty" json:"archived,omitempty"`
	TerminalArchive         *TerminalArchiveRef      `yaml:"terminal_archive,omitempty" json:"terminal_archive,omitempty"`
	TerminalArchiveRestored bool                     `yaml:"-" json:"-"`
	ReviewingBy             *string                  `yaml:"reviewing_by,omitempty"`
	ReviewLeaseExpires      *time.Time               `yaml:"review_lease_expires,omitempty"`
	ApprovedBy              *string                  `yaml:"approved_by,omitempty"`
	Approvals               []Approval               `yaml:"approvals,omitempty"`
	MergeCommit             *string                  `yaml:"merge_commit,omitempty"`
	LeaseExpires            *time.Time               `yaml:"lease_expires,omitempty"`
	SpecRef                 string                   `yaml:"spec_ref"`
	EpicRef                 string                   `yaml:"epic_ref,omitempty"`
	PlanRef                 string                   `yaml:"plan_ref,omitempty"`
	ArchRef                 string                   `yaml:"arch_ref,omitempty"`
	Kind                    string                   `yaml:"kind,omitempty"`
	RCARequired             bool                     `yaml:"rca_required,omitempty" json:"rca_required,omitempty"`
	DoneWhen                string                   `yaml:"done_when"`
	Validation              []string                 `yaml:"validation,omitempty" json:"validation,omitempty"`
	ValidationPrerequisites []ValidationPrerequisite `yaml:"validation_prerequisites,omitempty" json:"validation_prerequisites,omitempty"`
	RuntimeInputs           []RuntimeInput           `yaml:"runtime_inputs,omitempty" json:"runtime_inputs,omitempty"`
	DestructiveDB           bool                     `yaml:"destructive_db,omitempty" json:"destructive_db,omitempty"`
	Scope                   string                   `yaml:"scope"`
	RejectionReason         *string                  `yaml:"rejection_reason,omitempty"`
	BlockedReason           *string                  `yaml:"blocked_reason,omitempty"`
	BlockedQuestions        []string                 `yaml:"blocked_questions,omitempty"`
	StateLockHold           *StateLockHold           `yaml:"state_lock_hold,omitempty" json:"state_lock_hold,omitempty"`
	RepairRequest           *RepairRequest           `yaml:"repair_request,omitempty" json:"repair_request,omitempty"`
	RejectionRCA            *RejectionRCARecord      `yaml:"rejection_rca,omitempty" json:"rejection_rca,omitempty"`
	SupersededBy            []string                 `yaml:"superseded_by,omitempty"`
	Supersedes              *string                  `yaml:"supersedes,omitempty"`
	RescopeReason           *string                  `yaml:"rescope_reason,omitempty"`
	FailedBy                []string                 `yaml:"failed_by,omitempty"`
	IntegrationFailure      map[string]any           `yaml:"integration_failure,omitempty" json:"integration_failure,omitempty"`

	IntegrationAnalysis *IntegrationAnalysisMetadata `yaml:"integration_analysis,omitempty" json:"integration_analysis,omitempty"`
	Lifecycle           *TaskLifecycle               `yaml:"lifecycle,omitempty" json:"lifecycle,omitempty"`

	Attempt              int                  `yaml:"attempt,omitempty"`
	DependsOn            []string             `yaml:"depends_on,omitempty"`
	ProviderDependencies []ProviderDependency `yaml:"provider_dependencies,omitempty" json:"provider_dependencies,omitempty"`
	// DescendantDependencies are applied to every output this task writes for
	// the named transition (D-79); they never hold this task's own claim.
	DescendantDependencies []DescendantDependency `yaml:"descendant_dependencies,omitempty" json:"descendant_dependencies,omitempty"`
	// ProviderReservations hold this writer until every child each named
	// provider generates has merged (D-80); never copied from a parent output.
	ProviderReservations []ProviderReservation `yaml:"provider_reservations,omitempty" json:"provider_reservations,omitempty"`
	// MaxOutputs caps this task's output entries; 0 is unbounded (D-80).
	MaxOutputs     int                `yaml:"max_outputs,omitempty" json:"max_outputs,omitempty"`
	IntegrationFix bool               `yaml:"integration_fix,omitempty"`
	HandoffPending bool               `yaml:"handoff_pending,omitempty"`
	HandoffEvents  []HandoffEvent     `yaml:"handoff_events,omitempty"`
	MaxIterations  int                `yaml:"max_iterations,omitempty"`
	Created        time.Time          `yaml:"created"`
	History        []TaskHistoryEntry `yaml:"history"`
	Extra          map[string]any     `yaml:",inline"`
}

// EffectiveParentTasks returns the list of parent task IDs.
// Returns ParentTasks if non-empty. Otherwise, wraps ParentTask
// into a single-element slice for backward compatibility.
// Returns nil if the task has no parents.
func (t *Task) EffectiveParentTasks() []string {
	if len(t.ParentTasks) > 0 {
		return t.ParentTasks
	}
	if t.ParentTask != nil && *t.ParentTask != "" {
		return []string{*t.ParentTask}
	}
	return nil
}

// CohortParentID returns the shared parent used to identify a many-to-one cohort.
// Many-to-one children are siblings created from the same upstream task, so
// EffectiveParentTasks()[0] is the cohort anchor. Returns "" if no parents exist.
func (t *Task) CohortParentID() string {
	parents := t.EffectiveParentTasks()
	if len(parents) == 0 {
		return ""
	}
	return parents[0]
}

// EffectiveType returns the task's type, defaulting to TaskTypeCoding when empty (backward compat).
func (t *Task) EffectiveType() TaskType {
	if t.Type == "" {
		return TaskTypeCoding
	}
	return t.Type
}

// MaxTaskAttempt is the highest attempt number a task carries: a task starts a
// fresh attempt once and then blocks.
const MaxTaskAttempt = 2

// EffectiveAttempt returns the task's attempt number, defaulting to 1 when
// Attempt is 0 (unset) for backward compatibility with existing tasks.
// When Attempt is unset and legacy Extra["attempted"] exists, derives the
// attempt count from len(attempted) + 1.
func (t *Task) EffectiveAttempt() int {
	if t.Attempt >= 1 {
		return t.Attempt
	}
	if attempted, ok := t.legacyAttemptedList(); ok && len(attempted) > 0 {
		return min(len(attempted)+1, MaxTaskAttempt)
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
		t.Attempt = min(len(list)+1, MaxTaskAttempt)
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

// minDiverseApprovals is the approval count below which diversity cannot hold.
const minDiverseApprovals = 2

// HasProviderDiversity returns true if approvals come from at least 2 distinct providers.
func (t *Task) HasProviderDiversity() bool {
	if len(t.Approvals) < minDiverseApprovals {
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

// HasApprovalFromAgent reports whether the given agent has already recorded
// an approval on this task. Used to prevent a reviewer from re-claiming
// (and double-approving) a task they previously voted on — the second
// review must come from a different agent to satisfy independent review.
func (t *Task) HasApprovalFromAgent(agentID string) bool {
	if agentID == "" {
		return false
	}
	for _, a := range t.Approvals {
		if a.Agent == agentID {
			return true
		}
	}
	return false
}
