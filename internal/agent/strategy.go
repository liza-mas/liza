package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// RoleStrategy encapsulates all role-specific behavior in the supervisor loop.
type RoleStrategy interface {
	// DefaultTimeout returns the execution timeout when SupervisorConfig.ExecutionTimeout is unset.
	DefaultTimeout() time.Duration

	// WaitConfig returns the poll interval and max wait for this role,
	// resolved from state configuration with role-appropriate defaults.
	WaitConfig(state *models.State) (pollInterval, maxWait time.Duration)

	// PreWork runs actions before waiting for work (e.g., merge handling for reviewers).
	// Returns shouldContinue=true to restart the loop iteration (e.g., pending merge retry).
	PreWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig) (shouldContinue bool, err error)

	// WaitForWork blocks until role-specific work is available.
	WaitForWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pollInterval, maxWait time.Duration) (bool, error)

	// ClaimTask claims a task for execution.
	// Returns (taskID, claimedTaskID, err). Orchestrator returns ("", "", nil).
	ClaimTask(config SupervisorConfig, bb *db.Blackboard) (taskID, claimedTaskID string, err error)

	// PreExecution runs setup after claiming but before execution (e.g., orchestrator PLANNING status).
	PreExecution(bb *db.Blackboard, config SupervisorConfig) error

	// BuildPrompt constructs the role-specific prompt.
	BuildPrompt(state *models.State, config SupervisorConfig, taskID string) (string, error)

	// PostExecution runs actions after exit code 0 (e.g., submission logging, state verification).
	PostExecution(bb *db.Blackboard, config SupervisorConfig, taskID, claimedTaskID string, stateBefore *models.State) error
}

// NewRoleStrategy creates the appropriate strategy for the given runtime role.
// The resolver determines the role's type (doer/reviewer/orchestrator) from the
// pipeline YAML, enabling custom YAML-defined roles to get the correct strategy
// without modifying this function. The resolver is stored in each strategy for
// use during prompt building (BuildRoleContext via ContextSections).
func NewRoleStrategy(role string, resolver *pipeline.Resolver) (RoleStrategy, error) {
	roleType, err := resolver.RoleType(role)
	if err != nil {
		return nil, fmt.Errorf("unknown role %q: %w", role, err)
	}

	switch roleType {
	case roles.RoleTypeDoer:
		return &doerStrategy{role: role, resolver: resolver}, nil
	case roles.RoleTypeReviewer:
		return &reviewerStrategy{role: role, resolver: resolver}, nil
	case roles.RoleTypeOrchestrator:
		return &orchestratorStrategy{resolver: resolver}, nil
	default:
		return nil, fmt.Errorf("unsupported role type %q for role %q", roleType, role)
	}
}
