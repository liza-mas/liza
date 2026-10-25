package ops

import (
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// AgentDegradedReason is a reason ClassifyInfraClaimError assigns to a claim
// failure that degrades an agent. The recorded reason, MarkAgentDegradedInput.Reason
// and models.AgentHealth.Reason, is open text (an operator names its own), so it
// stays a string and takes the converted value.
type AgentDegradedReason string

const (
	AgentDegradedWorktreeCreateFailed     AgentDegradedReason = "claim_worktree_create_failed"
	AgentDegradedWorktreeReadonly         AgentDegradedReason = "claim_worktree_readonly"
	AgentDegradedWorktreePermissionDenied AgentDegradedReason = "claim_worktree_permission_denied"
	AgentDegradedWorktreeSetupFailed      AgentDegradedReason = "claim_worktree_setup_failed"
)

// MarkAgentDegradedInput contains the evidence used to mark an agent epoch as
// unable to provide effective claim capacity.
type MarkAgentDegradedInput struct {
	ProjectRoot    string
	AgentID        string
	Role           string
	Reason         string
	LastTask       string
	CandidateTasks []string
	LastError      string
	RecoverHint    string
	DegradedBy     string
	Authority      *models.AgentAuthority
	// SetupOutcome, for a setup failure, lets the write be skipped when a
	// later recorded setup of the same worktree passed.
	SetupOutcome *PostWorktreeSetupError
}

// ErrSetupFailureSuperseded reports a degrade skipped because a later setup of
// the failed worktree passed: the failure no longer describes the worktree.
var ErrSetupFailureSuperseded = stderrors.New("setup failure superseded by a later passing setup")

// InfraClaimErrorClassification describes a claim failure that indicates the
// agent process cannot provision worktrees/refs for its role.
type InfraClaimErrorClassification struct {
	Reason      AgentDegradedReason
	RecoverHint string
	IsInfra     bool
}

// MarkAgentDegraded records current capacity health for an agent and appends an
// anomaly for durable history. It does not mutate task state or agent status.
func MarkAgentDegraded(input MarkAgentDegradedInput) error {
	if input.AgentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	if input.Role == "" {
		return &PreconditionError{Reason: "agent role is required"}
	}
	if input.Reason == "" {
		return &PreconditionError{Reason: "degraded reason is required"}
	}
	if input.LastError == "" {
		return &PreconditionError{Reason: "last error is required"}
	}
	if input.Authority != nil {
		if err := requireAuthorityActor(*input.Authority, input.AgentID); err != nil {
			return err
		}
	}

	bb := db.For(paths.New(input.ProjectRoot).StatePath())
	now := time.Now().UTC()
	return lifecycleMutation(bb, input.Authority)(func(state *models.State) error {
		// Read under the state lock, so a passing setup's reconcile either
		// already sees this record or this check already sees its outcome.
		if setupFailureSuperseded(input.SetupOutcome) {
			return ErrSetupFailureSuperseded
		}
		agent, hasAgent := state.Agents[input.AgentID]
		if state.AgentHealth == nil {
			state.AgentHealth = make(map[string]models.AgentHealth)
		}

		var registeredAt *time.Time
		if hasAgent && !agent.RegisteredAt.IsZero() {
			value := agent.RegisteredAt
			registeredAt = &value
		}
		provider := ""
		pid := 0
		if hasAgent {
			provider = agent.Provider
			pid = agent.PID
		}

		state.AgentHealth[input.AgentID] = models.AgentHealth{
			State:          models.AgentHealthDegraded,
			Role:           input.Role,
			Provider:       provider,
			PID:            pid,
			RegisteredAt:   registeredAt,
			DegradedAt:     now,
			Reason:         input.Reason,
			LastTask:       input.LastTask,
			CandidateTasks: append([]string(nil), input.CandidateTasks...),
			LastError:      input.LastError,
			RecoverHint:    input.RecoverHint,
			DegradedBy:     input.DegradedBy,
		}

		state.Anomalies = append(state.Anomalies, models.Anomaly{
			Timestamp: now,
			Task:      input.LastTask,
			Reporter:  input.AgentID,
			Type:      models.AnomalyTypeAgentDegraded,
			Details: map[string]any{
				"agent_id":        input.AgentID,
				"role":            input.Role,
				"provider":        provider,
				"pid":             pid,
				"reason":          input.Reason,
				"candidate_tasks": append([]string(nil), input.CandidateTasks...),
				"last_error":      input.LastError,
				"recover_hint":    input.RecoverHint,
				"degraded_by":     input.DegradedBy,
			},
		})
		return nil
	})
}

// ClearAgentDegraded removes current degraded health for an agent ID. It is
// idempotent so callers can clear after a successful claim without pre-checks.
func ClearAgentDegraded(projectRoot, agentID string) error {
	return clearAgentDegraded(projectRoot, agentID, nil)
}

// ClearAgentDegradedWithAuthority fences supervisor-driven health cleanup with
// the caller's current registration generation.
func ClearAgentDegradedWithAuthority(projectRoot string, authority models.AgentAuthority) error {
	return clearAgentDegraded(projectRoot, authority.ID, &authority)
}

func clearAgentDegraded(projectRoot, agentID string, authority *models.AgentAuthority) error {
	if agentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	if authority != nil {
		if err := requireAuthorityActor(*authority, agentID); err != nil {
			return err
		}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	return lifecycleMutation(bb, authority)(func(state *models.State) error {
		if state.AgentHealth == nil {
			return nil
		}
		delete(state.AgentHealth, agentID)
		if len(state.AgentHealth) == 0 {
			state.AgentHealth = nil
		}
		return nil
	})
}

// clearSupersededSetupDegradation removes setup degradation of any agent on
// taskID recorded before the passing outcome seq, while that outcome is still
// the worktree's latest. Both the outcome check and the deletion happen inside
// the caller's generation-fenced state mutation; a degrade published after the
// pass is judged by setupFailureSuperseded instead.
func clearSupersededSetupDegradation(projectRoot, taskID, outcomePath string, seq uint64, authority *models.AgentAuthority) error {
	bb := db.For(paths.New(projectRoot).StatePath())
	snapshot, err := bb.ReadSnapshot()
	if err != nil {
		return err
	}
	if !hasSetupDegradationFor(snapshot.AgentHealth, taskID) {
		return nil
	}
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		latest, exists, err := readWorktreeSetupOutcome(outcomePath)
		if err != nil || !exists || !latest.OK || latest.Seq < seq {
			return errNoSetupDegradationCleared
		}
		cleared := false
		for agentID, health := range state.AgentHealth {
			if isSetupDegradationFor(health, taskID) && health.DegradedAt.Before(latest.Time) {
				delete(state.AgentHealth, agentID)
				cleared = true
			}
		}
		if !cleared {
			return errNoSetupDegradationCleared
		}
		if len(state.AgentHealth) == 0 {
			state.AgentHealth = nil
		}
		return nil
	})
	if stderrors.Is(err, errNoSetupDegradationCleared) {
		return nil
	}
	return err
}

// errNoSetupDegradationCleared aborts a reconcile mutation that changes nothing.
var errNoSetupDegradationCleared = stderrors.New("no setup degradation cleared")

func hasSetupDegradationFor(health map[string]models.AgentHealth, taskID string) bool {
	for _, record := range health {
		if isSetupDegradationFor(record, taskID) {
			return true
		}
	}
	return false
}

func isSetupDegradationFor(health models.AgentHealth, taskID string) bool {
	return health.State == models.AgentHealthDegraded &&
		health.Reason == string(AgentDegradedWorktreeSetupFailed) &&
		health.LastTask == taskID
}

// ClassifyInfraClaimError recognizes narrow infrastructure failures that make a
// doer process unusable for claim capacity. Races and task precondition errors
// intentionally remain unclassified.
func ClassifyInfraClaimError(err error) InfraClaimErrorClassification {
	if err == nil {
		return InfraClaimErrorClassification{}
	}
	// The configured post_worktree_cmd failed. Every worktree this agent
	// provisions will hit the same project-level failure, so claiming again is
	// wasted capacity — degrade instead, which also bounds the claim retry loop.
	var setupErr *PostWorktreeSetupError
	if stderrors.As(err, &setupErr) {
		hint := fmt.Sprintf(
			"run the configured post_worktree_cmd (%s) in %s and fix it, then clear agent health",
			setupErr.Cmd,
			setupErr.Dir,
		)
		if setupErr.OutputPath != "" {
			hint = fmt.Sprintf("read the failed setup's output in %s, or %s", setupErr.OutputPath, hint)
		}
		return InfraClaimErrorClassification{
			Reason:      AgentDegradedWorktreeSetupFailed,
			RecoverHint: hint,
			IsInfra:     true,
		}
	}

	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "cannot lock ref") &&
		(strings.Contains(lower, "operation not permitted") || strings.Contains(lower, "permission denied")):
		return infraClaimClassification(AgentDegradedWorktreeCreateFailed)
	case strings.Contains(lower, "read-only file system"):
		return infraClaimClassification(AgentDegradedWorktreeReadonly)
	case strings.Contains(lower, "permission denied") &&
		(strings.Contains(lower, ".git/refs") || strings.Contains(lower, ".git/worktrees") || strings.Contains(lower, paths.WorktreesDirName)):
		return infraClaimClassification(AgentDegradedWorktreePermissionDenied)
	case strings.Contains(lower, "operation not permitted") &&
		(strings.Contains(lower, ".git/refs") || strings.Contains(lower, ".git/worktrees") || strings.Contains(lower, paths.WorktreesDirName)):
		return infraClaimClassification(AgentDegradedWorktreePermissionDenied)
	}

	return InfraClaimErrorClassification{}
}

func infraClaimClassification(reason AgentDegradedReason) InfraClaimErrorClassification {
	return InfraClaimErrorClassification{
		Reason:      reason,
		RecoverHint: fmt.Sprintf("restart the agent from a process context that can write project git refs and %s", paths.WorktreesDirName),
		IsInfra:     true,
	}
}
