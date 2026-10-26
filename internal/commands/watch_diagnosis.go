package commands

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// stallDiagnosis explains a stall in the terms that decide what to do about it.
//
// "No task progress" is the same sentence whether work is unstaffed or being
// refused, and those need opposite responses: spawn capacity, or read why the
// claim fails. Deriving that by hand from supervisor logs cost five hours and
// forty minutes on 2026-09-20, while validate, analyze and anomalies all read
// healthy. Counts come from models.GetTaskReadiness, so they agree with what
// repair-agent-pool would act on rather than inventing a second notion of
// claimable.
//
// Returns "" when no useful distinction can be drawn, leaving the bare message.
//
// now is the lease/heartbeat clock for agent liveness only: models.GetTaskReadiness
// reads wall time internally, so a test that moves now shifts idle-capacity
// liveness without shifting readiness. They agree in production, where both are
// real time; do not build an injected-clock test on the assumption they move
// together.
func stallDiagnosis(state *models.State, pr models.PipelineResolver, now time.Time) string {
	if state == nil || pr == nil {
		return ""
	}
	readiness := models.GetTaskReadiness(state, pr)
	if readiness.Claimable == 0 && readiness.Reviewable == 0 {
		blocked, waiting := countStallHolds(state, pr, now)
		human := ""
		if ids := humanOwnedBlockers(state); len(ids) > 0 {
			human = "; awaiting human: " + strings.Join(ids, ", ")
		}
		switch {
		case blocked > 0 && waiting > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) held (read blocked_reason), %d task(s) waiting on dependencies%s", blocked, waiting, human)
		case blocked > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) held (read blocked_reason)%s", blocked, human)
		case waiting > 0:
			return fmt.Sprintf(" — no claimable work; %d task(s) waiting on dependencies", waiting)
		}
		return " — no claimable work"
	}

	idle := idleAgentsByRole(state, now)
	var refused, unstaffed []string
	for _, role := range append(slices.Clone(readiness.ClaimableByRole), readiness.ReviewableByRole...) {
		if role.Count == 0 {
			continue
		}
		if n := idle[role.Role]; n > 0 {
			refused = append(refused, fmt.Sprintf("%d for %s with %d idle", role.Count, role.Role, n))
			continue
		}
		unstaffed = append(unstaffed, fmt.Sprintf("%d for %s", role.Count, role.Role))
	}

	reason := "read supervisor logs for the refusal reason"
	if failed := failedValidationPreflights(state, pr, now); len(failed) > 0 {
		reason = "validation preflight failed for " + strings.Join(failed, ", ")
	}
	switch {
	case len(refused) > 0 && len(unstaffed) > 0:
		return fmt.Sprintf(" — claims refused (%s) and unstaffed (%s): %s",
			strings.Join(refused, ", "), strings.Join(unstaffed, ", "), reason)
	case len(refused) > 0:
		return fmt.Sprintf(" — %s: claims are being refused, not unstaffed; %s",
			strings.Join(refused, ", "), reason)
	case len(unstaffed) > 0:
		return fmt.Sprintf(" — %s: no live agent for that role", strings.Join(unstaffed, ", "))
	default:
		return ""
	}
}

// failedValidationPreflights names the idle, live agents whose current
// preflight of a task they could claim failed, as "task (agent: code)". Only
// observations CurrentValidationReadiness accepts count: an expired or
// superseded record is history, not the reason for this refusal.
func failedValidationPreflights(state *models.State, pr models.PipelineResolver, now time.Time) []string {
	window := agentLivenessWindow(state.Config)
	var failed []string
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if len(task.ValidationPrerequisites) == 0 {
			continue
		}
		for agentID, agentState := range state.Agents {
			if agentState.Status != models.AgentStatusIdle || !agentHasLiveRegistration(agentState, now, window) ||
				!models.IsRoleTaskReady(state, task, agentState.Role, pr, now) {
				continue
			}
			if record, ok := models.CurrentValidationReadiness(state, task, agentID, now); ok && record.Result == "failed" {
				failed = append(failed, fmt.Sprintf("%s (%s: %s)", task.ID, agentID, record.Code))
			}
		}
	}
	slices.Sort(failed)
	return failed
}

// idleAgentsByRole counts live, healthy, idle agents per role — the capacity
// that should have claimed the ready work and did not.
func idleAgentsByRole(state *models.State, now time.Time) map[string]int {
	idle := make(map[string]int)
	window := agentLivenessWindow(state.Config)
	for agentID, agentState := range state.Agents {
		if agentState.Status != models.AgentStatusIdle {
			continue
		}
		if !agentHasLiveRegistration(agentState, now, window) {
			continue
		}
		if agentHealthIsCurrentDegraded(state.AgentHealth[agentID], agentState) {
			continue
		}
		idle[agentState.Role]++
	}
	return idle
}

// countStallHolds separates the two reasons nothing is claimable, because they
// demand opposite responses and IsTerminal does not tell them apart: BLOCKED is
// not terminal, so a held task is otherwise indistinguishable from one waiting
// on its graph. Calling a BLOCKED task "waiting on dependencies" tells
// the operator there is nothing to do in exactly the case where they must read
// the reason and intervene.
func countStallHolds(state *models.State, pr models.PipelineResolver, now time.Time) (blocked, waiting int) {
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status.IsTerminal() || task.RolePair == "" {
			continue
		}
		// Only BLOCKED lands here. INTEGRATION_FAILED is also non-terminal and
		// also needs attention, but it stays claimable — the doer re-claims it
		// through the integration-fix path — so readiness already reports it as
		// refused or unstaffed, which says more than "held" would.
		if task.Status == models.TaskStatusBlocked {
			blocked++
			continue
		}
		doerRole, err := pr.DoerRole(task.RolePair)
		if err != nil {
			continue
		}
		if !models.IsRoleTaskReady(state, task, doerRole, pr, now) && task.AssignedTo == nil {
			waiting++
		}
	}
	return blocked, waiting
}

// humanOwnedBlockers names the BLOCKED tasks only a human can clear, sorted, so
// a stall caused by them says who must act rather than "read blocked_reason".
func humanOwnedBlockers(state *models.State) []string {
	var ids []string
	for i := range state.Tasks {
		if _, ok := models.CurrentAwaitingHuman(&state.Tasks[i]); ok {
			ids = append(ids, state.Tasks[i].ID)
		}
	}
	slices.Sort(ids)
	return ids
}
