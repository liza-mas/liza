package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
)

// RunPoolBudget is the run-wide agent budget observed on one state (ADR-0245).
// Non-orchestrator roles share Budget slots: max_agents minus the slot always
// held for the orchestrator. Occupancy is lease-first, as for the per-role cap.
type RunPoolBudget struct {
	Budget int
	// Occupied counts occupied non-orchestrator registrations; ByRole splits
	// it per role.
	Occupied int
	ByRole   map[string]int
	// Reservations are the unexpired reservations whose role has no occupied
	// registration, in reservation order. Each holds one slot.
	Reservations []models.PoolReservation
}

// ObserveRunPoolBudget counts the run-wide budget on state, leaving out
// selfID's own registration when it is set. A registration whose role does not
// resolve counts as a non-orchestrator, so it never frees a slot by mistake.
func ObserveRunPoolBudget(state *models.State, resolver models.PipelineResolver, selfID string, now time.Time) RunPoolBudget {
	budget := RunPoolBudget{
		Budget: models.NonOrchestratorAgentBudget(state.Config),
		ByRole: make(map[string]int),
	}
	window := models.AgentLivenessWindow(state.Config)
	for id, agent := range state.Agents {
		if id == selfID || isOrchestratorRole(resolver, agent.Role) {
			continue
		}
		if AgentProcessOwnership(id, agent, now, window).Occupied() {
			budget.Occupied++
			budget.ByRole[agent.Role]++
		}
	}
	for _, reservation := range models.UnexpiredPoolReservations(state, now) {
		if budget.ByRole[reservation.Role] == 0 {
			budget.Reservations = append(budget.Reservations, reservation)
		}
	}
	return budget
}

func isOrchestratorRole(resolver models.PipelineResolver, role string) bool {
	if resolver == nil {
		return false
	}
	roleType, err := resolver.RoleType(role)
	return err == nil && roleType == roles.RoleTypeOrchestrator
}

// Reserved reports whether role holds one of the budget's reservations.
func (b RunPoolBudget) Reserved(role string) bool {
	return slices.ContainsFunc(b.Reservations, func(r models.PoolReservation) bool { return r.Role == role })
}

func (b RunPoolBudget) reservedRoles() string {
	names := make([]string, 0, len(b.Reservations))
	for _, reservation := range b.Reservations {
		names = append(names, reservation.Role)
	}
	return strings.Join(names, ", ")
}

// isDonorRole reports whether role has the most occupied registrations among
// non-orchestrator roles; tied roles all qualify.
func (b RunPoolBudget) isDonorRole(role string) bool {
	most := 0
	for _, count := range b.ByRole {
		most = max(most, count)
	}
	return most > 0 && b.ByRole[role] == most
}

// requireRunCapacity refuses a non-orchestrator registration that would take
// the run past max_agents, or take a slot held for another role. The
// orchestrator's slot is held back from this budget, so the orchestrator keeps
// only its singleton check.
func requireRunCapacity(state *models.State, in RegisterAgentInput, now time.Time) error {
	if in.Resolver == nil || isOrchestratorRole(in.Resolver, in.Role) {
		return nil
	}
	budget := ObserveRunPoolBudget(state, in.Resolver, in.AgentID, now)
	maxAgents := models.EffectiveMaxAgents(state.Config.MaxAgents)
	if budget.Occupied >= budget.Budget {
		return fmt.Errorf("run already has %d live non-orchestrator agent(s); max_agents %d holds one slot for the orchestrator, leaving %d",
			budget.Occupied, maxAgents, budget.Budget)
	}
	if !budget.Reserved(in.Role) && budget.Occupied+len(budget.Reservations) >= budget.Budget {
		return fmt.Errorf("run has %d live non-orchestrator agent(s) and %d slot(s) held for role(s) %s waiting for an agent (max_agents %d)",
			budget.Occupied, len(budget.Reservations), budget.reservedRoles(), maxAgents)
	}
	return nil
}

// runSurplus reports whether agentID's role should give up a run-wide slot:
// the run is over max_agents (a lowered cap), or a reserved role is waiting
// for a slot the run cannot otherwise give. Only an agent of a role with the
// most occupied registrations gives one up. A handoff to a reserved role also
// requires that the agent did something since it registered, so a newly
// admitted agent cannot hand its slot straight back. In-flight pinning is the
// caller's.
func runSurplus(state *models.State, resolver *pipeline.Resolver, agentID string, agent models.Agent, now time.Time) bool {
	// Waiting agents evaluate this on every state read. Every row counted as
	// occupied, with every unexpired reservation, bounds O + |R| from above
	// without observing any process; under that bound nothing is surplus.
	rows := 0
	for _, other := range state.Agents {
		if !isOrchestratorRole(resolver, other.Role) {
			rows++
		}
	}
	if rows+len(models.UnexpiredPoolReservations(state, now)) <= models.NonOrchestratorAgentBudget(state.Config) {
		return false
	}
	budget := ObserveRunPoolBudget(state, resolver, "", now)
	if !budget.isDonorRole(agent.Role) {
		return false
	}
	if budget.Occupied > budget.Budget {
		return true
	}
	return budget.Occupied+len(budget.Reservations) > budget.Budget && actedSinceRegistration(state, agentID, agent.RegisteredAt)
}

// actedSinceRegistration reports whether a task history entry attributed to
// agentID is dated at or after registeredAt: a claim, submission, verdict or
// merge.
func actedSinceRegistration(state *models.State, agentID string, registeredAt time.Time) bool {
	for i := range state.Tasks {
		for _, entry := range state.Tasks[i].History {
			if entry.Agent != nil && *entry.Agent == agentID && !entry.Time.Before(registeredAt) {
				return true
			}
		}
	}
	return false
}

// SetPoolReservations replaces the run's pool reservations (ADR-0245). Pool
// auto-repair is the only caller: it decides on a snapshot and writes only
// when the ordered list or an expiry changed, so the write itself does not
// compare.
func SetPoolReservations(projectRoot string, reservations []models.PoolReservation) error {
	bb := db.For(paths.New(projectRoot).StatePath())
	return bb.Modify(func(state *models.State) error {
		state.PoolReservations = slices.Clone(reservations)
		return nil
	})
}

// RunCapYieldDue reports whether agentID, blocked in await-verdict or
// await-resubmission, should end its wait so its slot goes to the run-wide
// budget (ADR-0245): the run is over max_agents, or a reserved role waits and
// this agent, of a role with the most agents, has acted since it registered.
// Those waits hold the task (CurrentTask) or the review claim, which pins the
// agent at its task boundary; without this a run full of waiting doers or
// reviewers never frees a slot for the role they wait on.
func RunCapYieldDue(state *models.State, resolver *pipeline.Resolver, agentID string, now time.Time) bool {
	if state == nil || resolver == nil {
		return false
	}
	agent, ok := state.Agents[agentID]
	if !ok || isOrchestratorRole(resolver, agent.Role) {
		return false
	}
	return runSurplus(state, resolver, agentID, agent, now)
}

// runCapYieldReason explains an await ended by RunCapYieldDue.
const runCapYieldReason = "the run-wide max_agents cap is full and another role waits for a slot; this agent gives up its slot and its work stays on the task"
