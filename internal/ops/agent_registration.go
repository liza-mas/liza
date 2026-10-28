package ops

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// RegisterAgentInput is the identity and lease an agent registers with.
// Resolver classifies the role for the singularity and capacity checks; nil
// skips them.
type RegisterAgentInput struct {
	AgentID              string
	Role                 string
	Terminal             string
	Provider             string
	Model                string
	LeaseDurationSeconds int
	Resolver             *pipeline.Resolver
}

// RegisterAgentResult carries the new registration's authority and the
// nonfatal observations the caller reports outside the transaction.
type RegisterAgentResult struct {
	Authority models.AgentAuthority
	// TookOver is set when an expired or stale registration of the same ID was
	// replaced; PreviousOwnership is its effective ownership state.
	TookOver          bool
	PreviousOwnership string
	// SweepErr is the failure of the liveness-record sweep, which never fails
	// the registration.
	SweepErr error
}

// RegisterAgent registers an agent directly as IDLE in one atomic transaction:
// collision detection, role singularity and capacity, a fresh generation, the
// PID and the liveness-record sweep. The caller holds the project and agent
// lifecycle locks and clears stale review claims after a reviewer registers.
func RegisterAgent(bb *db.Blackboard, in RegisterAgentInput) (RegisterAgentResult, error) {
	now := time.Now().UTC()
	leaseExpires := now.Add(time.Duration(in.LeaseDurationSeconds) * time.Second)
	var result RegisterAgentResult

	err := bb.Modify(func(state *models.State) error {
		result = RegisterAgentResult{}
		livenessWindow := models.AgentLivenessWindow(state.Config)
		if existing, exists := state.Agents[in.AgentID]; exists {
			observation := AgentProcessOwnership(in.AgentID, existing, now, livenessWindow)
			if observation.Occupied() {
				return fmt.Errorf("%w; %s", &lizaerrors.AgentCollisionError{AgentID: in.AgentID}, observation.Diagnostic(existing.PID))
			}
			result.TookOver = true
			result.PreviousOwnership = string(observation.Effective)
		}

		if err := requireRoleCapacity(state, in, now, livenessWindow); err != nil {
			return err
		}

		generation, err := models.NewAgentGeneration()
		if err != nil {
			return err
		}

		state.Agents[in.AgentID] = models.Agent{
			Role:         in.Role,
			Status:       models.AgentStatusIdle,
			Generation:   generation,
			Heartbeat:    now,
			RegisteredAt: now,
			Terminal:     in.Terminal,
			Provider:     in.Provider,
			Model:        in.Model,
			LeaseExpires: &leaseExpires,
			PID:          os.Getpid(),
		}
		result.Authority = models.AgentAuthority{ID: in.AgentID, Generation: generation}

		// Under the state lock, so every other row's record is kept; the
		// sweep's ceilings are documented on SweepLivenessRecords.
		result.SweepErr = db.SweepLivenessRecords(bb.GetStatePath(), state)
		return nil
	})
	if err != nil {
		return RegisterAgentResult{}, err
	}
	return result, nil
}

// requireRoleCapacity enforces the singularity check via the resolver: at most
// N instances per role. For orchestrator roles, singularity is enforced by
// resolved type (not role key) so that two different orchestrator role keys
// cannot coexist. Non-orchestrator roles use per-role-key counting, capped by
// the role's max-instances or else the project default.
func requireRoleCapacity(state *models.State, in RegisterAgentInput, now time.Time, livenessWindow time.Duration) error {
	if in.Resolver == nil {
		return nil
	}
	roleMax, err := in.Resolver.MaxInstances(in.Role)
	if err != nil {
		return nil
	}
	maxInst := models.EffectiveMaxInstances(roleMax, state.Config.MaxInstances)
	roleType, _ := in.Resolver.RoleType(in.Role)
	occupiedDetails := occupiedRolePeers(state, in.Resolver, in.Role, string(roleType), in.AgentID, now, livenessWindow)
	occupiedCount := len(occupiedDetails)
	if occupiedCount < maxInst {
		return nil
	}
	if roleType == roles.RoleTypeOrchestrator {
		return fmt.Errorf("type orchestrator already has %d live agent(s); only %d instance(s) allowed; %s",
			occupiedCount, maxInst, strings.Join(occupiedDetails, "; "))
	}
	return fmt.Errorf("role %s already has %d live agent(s) (max %d); only %d instance(s) allowed; %s",
		in.Role, occupiedCount, maxInst, maxInst, strings.Join(occupiedDetails, "; "))
}

// occupiedRolePeers returns a diagnostic for each registration other than
// selfID that holds capacity for role, in ID order. Orchestrator roles count
// by resolved type, so different orchestrator role keys share one cap; other
// roles count by role key. Occupancy is lease-first (AgentProcessOwnership),
// so a leased registration whose process died still holds its slot.
func occupiedRolePeers(state *models.State, resolver *pipeline.Resolver, role, roleType, selfID string, now time.Time, livenessWindow time.Duration) []string {
	agentIDs := make([]string, 0, len(state.Agents))
	for id := range state.Agents {
		agentIDs = append(agentIDs, id)
	}
	sort.Strings(agentIDs)

	var occupied []string
	for _, id := range agentIDs {
		if id == selfID {
			continue
		}
		agent := state.Agents[id]
		matchesRole := agent.Role == role
		if roleType == string(roles.RoleTypeOrchestrator) {
			resolvedType, resolveErr := resolver.RoleType(agent.Role)
			matchesRole = resolveErr == nil && resolvedType == roles.RoleTypeOrchestrator
		}
		if !matchesRole {
			continue
		}
		observation := AgentProcessOwnership(id, agent, now, livenessWindow)
		if !observation.Occupied() {
			continue
		}
		occupied = append(occupied, fmt.Sprintf("%s: %s", id, observation.Diagnostic(agent.PID)))
	}
	return occupied
}

// PoolSurplus reports whether agentID should leave its role's pool: the role
// is not an orchestrator, its other live registrations already fill the
// effective max-instances, and the agent holds no in-flight ownership.
// In-flight ownership is a current task, an executing (or handoff) doer
// assignment, rejected rework under an unexpired lease (reserved for this
// agent), any review claim, or a pending approved merge it owns. Retained
// attribution on submitted, approved or terminal tasks does not pin capacity:
// retirement releases it exactly as an idle exit does. A nil resolver or an
// unresolvable role fails closed. It is read-only, for snapshot callers and
// for the locked recheck of RetireSurplusAgent.
func PoolSurplus(state *models.State, resolver *pipeline.Resolver, agentID string, now time.Time) bool {
	if state == nil || resolver == nil {
		return false
	}
	agent, ok := state.Agents[agentID]
	if !ok || (agent.CurrentTask != nil && *agent.CurrentTask != "") {
		return false
	}
	roleType, err := resolver.RoleType(agent.Role)
	if err != nil || roleType == roles.RoleTypeOrchestrator {
		return false
	}
	roleMax, err := resolver.MaxInstances(agent.Role)
	if err != nil {
		return false
	}
	// Count peers first: it is the cheap test, and waiting agents evaluate
	// this on every state read.
	peers := occupiedRolePeers(state, resolver, agent.Role, string(roleType), agentID, now, models.AgentLivenessWindow(state.Config))
	if len(peers) < models.EffectiveMaxInstances(roleMax, state.Config.MaxInstances) {
		return false
	}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.ReviewingBy != nil && *task.ReviewingBy == agentID {
			return false
		}
		if task.AssignedTo == nil || *task.AssignedTo != agentID {
			continue
		}
		if models.IsExecutingStatus(task, resolver) {
			return false
		}
		if rejected, err := resolver.RejectedStatus(task.RolePair); err == nil && task.Status == rejected &&
			task.LeaseExpires != nil && task.LeaseExpires.After(now) {
			return false
		}
	}
	return OwnedPendingMergeFingerprint(state, agentID, resolver) == ""
}

// RetireSurplusAgent retires the agent when PoolSurplus holds under the state
// lock: in one generation-fenced transaction it re-evaluates the predicate on
// the locked state and, if still true, applies the same release-and-remove
// mutation as UnregisterAgent. Serial transactions mean each surplus decision
// sees the removals before it, so concurrent retirements cannot take a role
// below its cap. Returns false with state unchanged when the agent is not
// surplus. resolver is the caller's, so the locked recheck evaluates the same
// pipeline as the caller's snapshot check.
func RetireSurplusAgent(bb *db.Blackboard, authority models.AgentAuthority, resolver *pipeline.Resolver) (bool, error) {
	if resolver == nil {
		return false, nil
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)
	released := false
	err := ModifyWithAgentAuthority(bb, authority, func(state *models.State) error {
		// Read the clock under the lock: a peer whose lease expired while this
		// call waited must not count toward the cap.
		now := time.Now().UTC()
		if !PoolSurplus(state, resolver, authority.ID, now) {
			return nil
		}
		if err := unregisterInState(state, authority.ID, pipelineTransitions, resolver, now); err != nil {
			return err
		}
		released = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return released, nil
}

// OwnedPendingMergeFingerprint identifies the approved merges this agent owns,
// each at its current task boundary. Empty means it owns none. A reviewer can
// become the owner without any reviewable work appearing — its approver left
// the registry — so a change in this value is itself work for merge handling.
func OwnedPendingMergeFingerprint(state *models.State, agentID string, pr models.PipelineResolver) string {
	if pr == nil {
		return ""
	}
	var owned []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if ApprovedMergePending(task, state, agentID, pr) {
			owned = append(owned, task.ID+"@"+models.TaskTransitionID(task))
		}
	}
	sort.Strings(owned)
	return strings.Join(owned, ",")
}

// ApprovedMergePending reports whether task is an approved merge still
// outstanding that agentID owns.
func ApprovedMergePending(task *models.Task, state *models.State, agentID string, pr models.PipelineResolver) bool {
	return models.ApprovedMergeOutstanding(task, pr) &&
		models.ApprovedMergeOwner(state, task, pr, time.Now().UTC()) == agentID
}
