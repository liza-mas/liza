package commands

import (
	"cmp"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/roles"
)

// RunBudgetReport is the run-wide agent budget repair allocated against
// (ADR-0246): max_agents, the slots non-orchestrator roles share, those
// occupied, the pending starts, the roles holding a reservation, and the
// slots free for starts outside reservations before this run allocated any.
// HeldRoles had demand the run budget left without a start.
type RunBudgetReport struct {
	MaxAgents    int      `json:"max_agents"`
	Budget       int      `json:"budget"`
	Occupied     int      `json:"occupied"`
	Pending      int      `json:"pending"`
	Reservations []string `json:"reservations,omitempty"`
	Free         int      `json:"free"`
	HeldRoles    []string `json:"held_roles,omitempty"`
}

// allocateRunBudget bounds the starts in missing by the run-wide budget, the
// one place every repair entry point (watcher, manual, dry run) passes through
// after pending starts are subtracted. Each reservation holds one slot: the
// i-th (1-based) is funded when occupied + unreserved pending + i fits the
// budget, and a funded reservation with no pending start gets one start. The
// first pending start of a reserved role fills its reservation and counts
// once. Other starts, including further starts of a reserved role, take the
// slots left after every reservation, in wait-age order. Orchestrator work is
// outside the budget: its slot is held back. pending covers every role, not
// only those in missing, since every pending start consumes a slot.
func allocateRunBudget(missing []MissingRoleWork, state *models.State, pr models.PipelineResolver, pending map[string][]int, now time.Time) ([]MissingRoleWork, RunBudgetReport) {
	budget := ops.ObserveRunPoolBudget(state, pr, "", now)
	report := RunBudgetReport{
		MaxAgents: models.EffectiveMaxAgents(state.Config.MaxAgents),
		Budget:    budget.Budget,
		Occupied:  budget.Occupied,
	}
	unreservedPending := 0
	for role, starts := range pending {
		if isOrchestratorRoleWork(pr, role) {
			continue
		}
		report.Pending += len(starts)
		unreservedPending += len(starts)
		if budget.Reserved(role) && len(starts) > 0 {
			unreservedPending--
		}
	}
	funded := make(map[string]bool, len(budget.Reservations))
	for i, reservation := range budget.Reservations {
		report.Reservations = append(report.Reservations, reservation.Role)
		funded[reservation.Role] = budget.Occupied+unreservedPending+i+1 <= budget.Budget
	}
	free := max(0, budget.Budget-budget.Occupied-unreservedPending-len(budget.Reservations))
	report.Free = free

	ordered := sortByWait(missing, state, now)
	var kept []MissingRoleWork
	for _, roleWork := range ordered {
		if isOrchestratorRoleWork(pr, roleWork.Role) {
			kept = append(kept, roleWork)
			continue
		}
		starts := 0
		if funded[roleWork.Role] && len(pending[roleWork.Role]) == 0 {
			starts = 1
		}
		extra := min(roleWork.SpawnCount-starts, free)
		starts += extra
		free -= extra
		if starts < roleWork.SpawnCount {
			report.HeldRoles = append(report.HeldRoles, roleWork.Role)
		}
		if starts <= 0 {
			continue
		}
		roleWork.SpawnCount = starts
		roleWork.AgentIDs = roleWork.AgentIDs[:min(len(roleWork.AgentIDs), starts)]
		roleWork.Items = roleWork.Items[:min(len(roleWork.Items), starts)]
		kept = append(kept, roleWork)
	}
	return kept, report
}

func isOrchestratorRoleWork(pr models.PipelineResolver, role string) bool {
	roleType, err := pr.RoleType(role)
	return err == nil && roleType == roles.RoleTypeOrchestrator
}

// sortByWait orders role work by how long its oldest task has waited, then by
// role name; retirement, admission and repair share this order through the
// reservations it selects.
func sortByWait(missing []MissingRoleWork, state *models.State, now time.Time) []MissingRoleWork {
	ordered := slices.Clone(missing)
	since := make(map[string]time.Time, len(ordered))
	for _, roleWork := range ordered {
		since[roleWork.Role] = roleWaitingSince(state, roleWork.TaskIDs, now)
	}
	slices.SortStableFunc(ordered, func(a, b MissingRoleWork) int {
		return cmp.Or(since[a.Role].Compare(since[b.Role]), cmp.Compare(a.Role, b.Role))
	})
	return ordered
}

// roleWaitingSince is the earliest time one of taskIDs became ready, or now
// when none is known.
func roleWaitingSince(state *models.State, taskIDs []string, now time.Time) time.Time {
	earliest := now
	for _, id := range taskIDs {
		if task := state.FindTask(id); task != nil {
			if ready := taskReadySince(state, task); !ready.IsZero() && ready.Before(earliest) {
				earliest = ready
			}
		}
	}
	return earliest
}

// taskReadySince approximates when task became actionable: its last history
// entry, or a dependency's last history entry when later (the dependency's
// merge is what made the task ready, and is recorded on the dependency only).
func taskReadySince(state *models.State, task *models.Task) time.Time {
	ready := lastHistoryTime(task)
	for _, dependencyID := range task.DependsOn {
		if dependency := state.FindTask(dependencyID); dependency != nil {
			if at := lastHistoryTime(dependency); at.After(ready) {
				ready = at
			}
		}
	}
	return ready
}

func lastHistoryTime(task *models.Task) time.Time {
	if len(task.History) == 0 {
		return time.Time{}
	}
	return task.History[len(task.History)-1].Time
}
