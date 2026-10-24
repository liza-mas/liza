package models

import (
	"slices"

	"github.com/liza-mas/liza/internal/roles"
)

// TaskType represents the kind of task, determining which roles participate in its lifecycle.
type TaskType string

const (
	TaskTypeCoding       TaskType = "coding"
	TaskTypePlanning     TaskType = "planning"
	TaskTypeEpicPlanning TaskType = "epic-planning"
	TaskTypeUSWriting    TaskType = "us-writing"
	TaskTypeIntegration  TaskType = "integration"
	TaskTypeArchitecture TaskType = "architecture"
)

// Role name constants used in task definitions.
// These are aliases for the canonical definitions in the roles package.
const (
	RoleCoder                = roles.Coder
	RoleCodeReviewer         = roles.CodeReviewer
	RoleOrchestrator         = roles.Orchestrator
	RoleCodePlanner          = roles.CodePlanner
	RoleCodePlanReviewer     = roles.CodePlanReviewer
	RoleEpicPlanner          = roles.EpicPlanner
	RoleEpicPlanReviewer     = roles.EpicPlanReviewer
	RoleUSWriter             = roles.USWriter
	RoleUSReviewer           = roles.USReviewer
	RoleIntegrationAnalyst   = roles.IntegrationAnalyst
	RoleIntegrationReviewer  = roles.IntegrationReviewer
	RoleArchitect            = roles.Architect
	RoleArchitectureReviewer = roles.ArchitectureReviewer
)

// taskWorkflows maps each TaskType to its ordered role sequence.
// This is the single source of truth for which roles participate in a task type's lifecycle.
// Access via RoleWorkflow(), HasRole(), and IsValid() — not directly.
var taskWorkflows = map[TaskType][]string{
	TaskTypeCoding:       {RoleCoder, RoleCodeReviewer},
	TaskTypePlanning:     {RoleCodePlanner, RoleCodePlanReviewer},
	TaskTypeEpicPlanning: {RoleEpicPlanner, RoleEpicPlanReviewer},
	TaskTypeUSWriting:    {RoleUSWriter, RoleUSReviewer},
	TaskTypeIntegration:  {RoleIntegrationAnalyst, RoleIntegrationReviewer},
	TaskTypeArchitecture: {RoleArchitect, RoleArchitectureReviewer},
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

// TaskTypeForRole returns the task type whose workflow includes the given role.
// Falls back to TaskTypeCoding for unknown roles (backward compatibility).
func TaskTypeForRole(role string) TaskType {
	for tt, wf := range taskWorkflows {
		if slices.Contains(wf, role) {
			return tt
		}
	}
	return TaskTypeCoding
}
