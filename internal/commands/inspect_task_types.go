package commands

import (
	"github.com/liza-mas/liza/internal/models"
)

// taskInfo represents task information with computed fields
type taskInfo struct {
	ID                string                    `json:"id" yaml:"id"`
	TransitionID      string                    `json:"transition_id" yaml:"transition_id"`
	Description       string                    `json:"description" yaml:"description"`
	Status            string                    `json:"status" yaml:"status"`
	Priority          int                       `json:"priority" yaml:"priority"`
	AssignedTo        *string                   `json:"assigned_to,omitempty" yaml:"assigned_to,omitempty"`
	ReviewingBy       *string                   `json:"reviewing_by,omitempty" yaml:"reviewing_by,omitempty"`
	DependsOn         []string                  `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	WaitingOn         *dependencyWaitInfo       `json:"waiting_on,omitempty" yaml:"waiting_on,omitempty"`
	Age               string                    `json:"age" yaml:"age"`                       // Computed: time since created
	TimeInStatus      string                    `json:"time_in_status" yaml:"time_in_status"` // Computed: time in current status
	BlockedReason     *string                   `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	BlockedQuestions  []string                  `json:"blocked_questions,omitempty" yaml:"blocked_questions,omitempty"`
	RepairRequest     *models.RepairRequest     `json:"repair_request,omitempty" yaml:"repair_request,omitempty"`
	DependencyReceipt *dependencyRepairReceipt  `json:"dependency_repair_receipt,omitempty" yaml:"dependency_repair_receipt,omitempty"`
	Iteration         int                       `json:"iteration,omitempty" yaml:"iteration,omitempty"`
	ReviewCycles      int                       `json:"review_cycles,omitempty" yaml:"review_cycles,omitempty"`
	LeaseExpires      *string                   `json:"lease_expires,omitempty" yaml:"lease_expires,omitempty"`
	Worktree          *string                   `json:"worktree,omitempty" yaml:"worktree,omitempty"`
	DoneWhen          string                    `json:"done_when,omitempty" yaml:"done_when,omitempty"`
	Validation        []string                  `json:"validation,omitempty" yaml:"validation,omitempty"`
	ValidationNotes   []models.ValidationNote   `json:"validation_notes,omitempty" yaml:"validation_notes,omitempty"`
	AmendsPlan        string                    `json:"amends_plan,omitempty" yaml:"amends_plan,omitempty"`
	AmendmentMode     models.PlanAmendmentMode  `json:"amendment_mode,omitempty" yaml:"amendment_mode,omitempty"`
	PlanAmendment     *models.PlanAmendment     `json:"plan_amendment,omitempty" yaml:"plan_amendment,omitempty"`
	PlanningChange    *models.PlanningChange    `json:"planning_change,omitempty" yaml:"planning_change,omitempty"`
	AcceptanceSource  *models.AcceptanceSource  `json:"acceptance_source,omitempty" yaml:"acceptance_source,omitempty"`
	AcceptanceReceipt *models.AcceptanceReceipt `json:"acceptance_receipt,omitempty" yaml:"acceptance_receipt,omitempty"`
	Archived          []models.ArchivedFieldRef `json:"archived,omitempty" yaml:"archived,omitempty"`
	DestructiveDB     bool                      `json:"destructive_db,omitempty" yaml:"destructive_db,omitempty"`
	RCARequired       bool                      `json:"rca_required,omitempty" yaml:"rca_required,omitempty"`
	Scope             string                    `json:"scope,omitempty" yaml:"scope,omitempty"`
	SpecRef           string                    `json:"spec_ref,omitempty" yaml:"spec_ref,omitempty"`
	PlanRef           string                    `json:"plan_ref,omitempty" yaml:"plan_ref,omitempty"`
	ParentTasks       []string                  `json:"parent_tasks,omitempty" yaml:"parent_tasks,omitempty"`
	MergeCommit       *string                   `json:"merge_commit,omitempty" yaml:"merge_commit,omitempty"`
	PRURL             *string                   `json:"pr_url,omitempty" yaml:"pr_url,omitempty"`
	RejectionReason   *string                   `json:"rejection_reason,omitempty" yaml:"rejection_reason,omitempty"`
	// RejectionRCA carries the gate record, including its disposition.
	RejectionRCA       *models.RejectionRCARecord    `json:"rejection_rca,omitempty" yaml:"rejection_rca,omitempty"`
	IntegrationFailure map[string]any                `json:"integration_failure,omitempty" yaml:"integration_failure,omitempty"`
	Output             []models.OutputEntry          `json:"output,omitempty" yaml:"output,omitempty"`
	Decomposition      *models.DecompositionManifest `json:"decomposition,omitempty" yaml:"decomposition,omitempty"`
	AttemptNum         int                           `json:"attempt_num,omitempty" yaml:"attempt_num,omitempty"`
}

// dependencyWaitInfo is what holds a task's admission: the claim predicate's
// unmet dependencies and the chain heads they lead to. It reports dependency
// holds only, not status or ownership claimability; heads are diagnosis, not
// await targets, and a head's claimable is the role-level claim predicate.
type dependencyWaitInfo struct {
	Unmet []dependencyWaitUnmetInfo `json:"unmet" yaml:"unmet"`
	Heads []dependencyWaitHeadInfo  `json:"heads" yaml:"heads"`
}

type dependencyWaitUnmetInfo struct {
	Dependency string   `json:"dependency" yaml:"dependency"`
	Kind       string   `json:"kind" yaml:"kind"`
	Blocking   []string `json:"blocking,omitempty" yaml:"blocking,omitempty"`
	Reason     string   `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type dependencyWaitHeadInfo struct {
	ID        string `json:"id" yaml:"id"`
	Status    string `json:"status,omitempty" yaml:"status,omitempty"`
	Claimable bool   `json:"claimable" yaml:"claimable"`
	Reason    string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type dependencyRepairReceipt struct {
	Operation       string   `json:"operation" yaml:"operation"`
	AffectedTaskIDs []string `json:"affected_task_ids" yaml:"affected_task_ids"`
	Validation      []string `json:"validation" yaml:"validation"`
}

// taskSummaryInfo is a compact task projection for agent orchestration.
type taskSummaryInfo struct {
	ID               string                `json:"id" yaml:"id"`
	TransitionID     string                `json:"transition_id" yaml:"transition_id"`
	Status           string                `json:"status" yaml:"status"`
	RolePair         string                `json:"role_pair,omitempty" yaml:"role_pair,omitempty"`
	Priority         int                   `json:"priority" yaml:"priority"`
	AssignedTo       *string               `json:"assigned_to,omitempty" yaml:"assigned_to,omitempty"`
	ReviewingBy      *string               `json:"reviewing_by,omitempty" yaml:"reviewing_by,omitempty"`
	DependsOn        []string              `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	WaitingOn        *dependencyWaitInfo   `json:"waiting_on,omitempty" yaml:"waiting_on,omitempty"`
	Attempt          int                   `json:"attempt" yaml:"attempt"`
	ReviewCycles     int                   `json:"review_cycles,omitempty" yaml:"review_cycles,omitempty"`
	LeaseExpires     *string               `json:"lease_expires,omitempty" yaml:"lease_expires,omitempty"`
	BlockedReason    *string               `json:"blocked_reason,omitempty" yaml:"blocked_reason,omitempty"`
	BlockedQuestions []string              `json:"blocked_questions,omitempty" yaml:"blocked_questions,omitempty"`
	RepairRequest    *models.RepairRequest `json:"repair_request,omitempty" yaml:"repair_request,omitempty"`
	RejectionReason  *string               `json:"rejection_reason,omitempty" yaml:"rejection_reason,omitempty"`
	FailedBy         []string              `json:"failed_by,omitempty" yaml:"failed_by,omitempty"`
	OutputCount      int                   `json:"output_count,omitempty" yaml:"output_count,omitempty"`
	OutputKinds      []string              `json:"output_kinds,omitempty" yaml:"output_kinds,omitempty"`
}

// taskOutputSummaryInfo is a compact projection of output[] for downstream orientation.
type taskOutputSummaryInfo struct {
	ID           string                   `json:"id" yaml:"id"`
	TransitionID string                   `json:"transition_id" yaml:"transition_id"`
	Status       string                   `json:"status" yaml:"status"`
	RolePair     string                   `json:"role_pair,omitempty" yaml:"role_pair,omitempty"`
	Output       []outputEntrySummaryInfo `json:"output" yaml:"output"`
}

type outputEntrySummaryInfo struct {
	CodingAllocation bool                          `json:"coding_allocation,omitempty" yaml:"coding_allocation,omitempty"`
	Index            int                           `json:"index" yaml:"index"`
	Desc             string                        `json:"desc,omitempty" yaml:"desc,omitempty"`
	Kind             string                        `json:"kind,omitempty" yaml:"kind,omitempty"`
	SpecRef          string                        `json:"spec_ref,omitempty" yaml:"spec_ref,omitempty"`
	EpicRef          string                        `json:"epic_ref,omitempty" yaml:"epic_ref,omitempty"`
	PlanRef          string                        `json:"plan_ref,omitempty" yaml:"plan_ref,omitempty"`
	ArchRef          string                        `json:"arch_ref,omitempty" yaml:"arch_ref,omitempty"`
	Validation       []string                      `json:"validation,omitempty" yaml:"validation,omitempty"`
	DestructiveDB    bool                          `json:"destructive_db,omitempty" yaml:"destructive_db,omitempty"`
	DependsOn        []string                      `json:"depends_on,omitempty" yaml:"depends_on,omitempty"`
	TaskDependsOn    []string                      `json:"task_depends_on,omitempty" yaml:"task_depends_on,omitempty"`
	Decomposition    *models.DecompositionManifest `json:"decomposition,omitempty" yaml:"decomposition,omitempty"`
}
