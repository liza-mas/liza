package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// PoolReservationTTL is how long a reservation holds its slot without a
// refresh; the watcher refreshes it at half-life, so it lapses only when the
// watcher stops ticking (ADR-0245).
const PoolReservationTTL = 5 * time.Minute

const autoRepairRunCapPrefix = "auto-repair-run-cap:"

// reconcilePoolReservations is pool repair's reservation step (ADR-0245). It
// keeps each reservation whose role still has no occupied registration and
// either demand or a pending start, and is not suppressed; it then reserves,
// in wait-age order, further roles with demand and no occupied registration,
// up to the non-orchestrator budget. demand is repair's view before pending
// starts and the run budget are applied, so a full run still yields
// candidates and a pending start keeps its role's protection until it
// registers. The list is written only when it or an expiry changed. It
// returns one RUN CAP alert per reservation episode that needs another role to
// give up a slot.
func reconcilePoolReservations(state *models.State, pr models.PipelineResolver, demand []MissingRoleWork, pending map[string][]int, config WatchConfig, now time.Time) []Alert {
	budget := ops.ObserveRunPoolBudget(state, pr, "", now)
	demandByRole := make(map[string]MissingRoleWork, len(demand))
	for _, roleWork := range demand {
		if !isOrchestratorRoleWork(pr, roleWork.Role) {
			demandByRole[roleWork.Role] = roleWork
		}
	}
	suppressed := func(role string) bool {
		return autoRepairStartCount(config.StateCache, role) >= AutoRepairAgentPoolMaxStarts
	}

	var next []models.PoolReservation
	for _, reservation := range budget.Reservations {
		_, hasDemand := demandByRole[reservation.Role]
		if (!hasDemand && len(pending[reservation.Role]) == 0) || suppressed(reservation.Role) {
			continue
		}
		if reservation.Until.Sub(now) < PoolReservationTTL/2 {
			reservation.Until = now.Add(PoolReservationTTL)
		}
		next = append(next, reservation)
	}
	var candidates []MissingRoleWork
	for role, roleWork := range demandByRole {
		reserved := slices.ContainsFunc(next, func(r models.PoolReservation) bool { return r.Role == role })
		if !reserved && budget.ByRole[role] == 0 && !suppressed(role) {
			candidates = append(candidates, roleWork)
		}
	}
	for _, roleWork := range sortByWait(candidates, state, now) {
		if len(next) >= budget.Budget {
			break
		}
		next = append(next, models.PoolReservation{Role: roleWork.Role, SelectedAt: now, Until: now.Add(PoolReservationTTL)})
	}

	if !slices.Equal(next, state.PoolReservations) {
		if err := ops.SetPoolReservations(config.ProjectRoot, next); err != nil {
			fmt.Fprintf(config.WarnWriter, "WARNING: failed to write pool reservations: %v\n", err)
		}
	}
	return runCapAlerts(next, budget, demandByRole, config.StateCache, now)
}

// runCapAlerts raises one RUN CAP warning per reservation that only another
// role giving up a slot can fund, and forgets episodes that ended.
func runCapAlerts(reservations []models.PoolReservation, budget ops.RunPoolBudget, demand map[string]MissingRoleWork, cache map[string]time.Time, now time.Time) []Alert {
	current := make(map[string]bool, len(reservations))
	var out []Alert
	for i, reservation := range reservations {
		if budget.Occupied+i+1 <= budget.Budget {
			continue
		}
		key := autoRepairRunCapPrefix + reservation.Role + "@" + reservation.SelectedAt.UTC().Format(time.RFC3339Nano)
		current[key] = true
		if _, seen := cache[key]; seen {
			continue
		}
		cache[key] = now
		out = append(out, Alert{
			Timestamp: now,
			Level:     AlertLevelWarning,
			Category:  alerts.CategoryRunCap.String(),
			Message: fmt.Sprintf("role %s has %d task(s) waiting and no agent, and max_agents is full (%d non-orchestrator slot(s)); a slot is held for it, and the next agent of %s to reach a task boundary gives one up",
				reservation.Role, demand[reservation.Role].TaskCount, budget.Budget, strings.Join(donorRoles(budget), ", ")),
		})
	}
	for key := range cache {
		if strings.HasPrefix(key, autoRepairRunCapPrefix) && !current[key] {
			delete(cache, key)
		}
	}
	return out
}

// donorRoles are the roles with the most occupied registrations, by name.
func donorRoles(budget ops.RunPoolBudget) []string {
	most := 0
	for _, count := range budget.ByRole {
		most = max(most, count)
	}
	var donors []string
	for role, count := range budget.ByRole {
		if count == most && most > 0 {
			donors = append(donors, role)
		}
	}
	slices.Sort(donors)
	return donors
}
