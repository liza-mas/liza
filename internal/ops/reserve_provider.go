package ops

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const reserveProviderOperation = "reserve-provider"

// ReserveProviderInput places an existing writer after every child a provider
// generates (D-80, ADR-0195), or withdraws that placement.
type ReserveProviderInput struct {
	TaskID       string
	ProviderTask string
	Transition   string
	Reason       string
	Release      bool
	// ProviderMaxOutputs sets or tightens the provider's max_outputs in the
	// same transaction; 0 leaves it unchanged.
	ProviderMaxOutputs int
}

// ReserveProviderResult reports the committed reservation change.
type ReserveProviderResult struct {
	TaskID             string   `json:"task_id"`
	ProviderTask       string   `json:"provider_task"`
	Transition         string   `json:"transition"`
	Released           bool     `json:"released"`
	Changed            bool     `json:"changed"`
	CapChanged         bool     `json:"cap_changed,omitempty"`
	ProviderMaxOutputs int      `json:"provider_max_outputs,omitempty"`
	Warnings           []string `json:"warnings,omitempty"`
}

func (r *ReserveProviderResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// ReserveProviderWithAuthority adds or releases one reservation under the
// orchestrator's registration fence. Adding only holds a writer longer; it
// waives no admission policy. Releasing withdraws a placement and is a policy
// decision, never a step of a provider retirement: replan and same-pair
// replace-task carry the reservation to the successor. A retry of a committed
// change is a no-op.
func ReserveProviderWithAuthority(projectRoot string, input ReserveProviderInput, authority models.AgentAuthority) (*ReserveProviderResult, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	res := models.ProviderReservation{ProviderTask: input.ProviderTask, Transition: input.Transition}
	if err := paths.ValidateTaskID(input.TaskID); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task ID: %v", err)}
	}
	if err := models.ValidateProviderReservations([]models.ProviderReservation{res}); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.ProviderMaxOutputs < 0 || (input.Release && input.ProviderMaxOutputs != 0) {
		return nil, &PreconditionError{Reason: "--provider-max-outputs must be positive and cannot be combined with --release"}
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	reservationAdded := false
	result := &ReserveProviderResult{TaskID: input.TaskID, ProviderTask: input.ProviderTask, Transition: input.Transition, Released: input.Release}
	err = lifecycleMutation(bb, &authority)(func(state *models.State) error {
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: input.TaskID}
		}
		if input.Release {
			index := slices.Index(task.ProviderReservations, res)
			if index < 0 {
				return nil
			}
			if task.Status.IsTerminal() {
				return &PreconditionError{Reason: fmt.Sprintf("task %s is terminal (%s); its reservations are audit history", task.ID, task.Status)}
			}
			task.ProviderReservations = slices.Delete(task.ProviderReservations, index, index+1)
			recordReservationChange(task, res, authority.ID, input.Reason, now, "provider_reservation_released")
			result.Changed = true
		} else {
			changed, err := addReservation(state, resolver, task, res, authority.ID, input.Reason, now)
			if err != nil {
				return err
			}
			result.Changed = changed
			reservationAdded = changed
			if input.ProviderMaxOutputs > 0 {
				capped, err := tightenProviderCap(state, res, input.ProviderMaxOutputs, authority.ID, input.Reason, now)
				if err != nil {
					return err
				}
				result.CapChanged = capped
				result.Changed = result.Changed || capped
				result.ProviderMaxOutputs = input.ProviderMaxOutputs
			}
		}
		if !result.Changed {
			return nil
		}
		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("reservation leaves an invalid state: %v", err)}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", reserveProviderOperation, err)
	}
	if result.Changed {
		action := "provider_reservation_added"
		switch {
		case input.Release:
			action = "provider_reservation_released"
		case result.CapChanged && reservationAdded:
			action = "provider_reservation_added_with_cap"
		case result.CapChanged:
			action = "provider_max_outputs_tightened"
		}
		if err := log.New(lp.LogPath()).Append(log.Entry{
			Timestamp: now, Agent: authority.ID, Action: action, Task: &input.TaskID,
			Detail: fmt.Sprintf("%s at %s: %s", input.ProviderTask, input.Transition, input.Reason),
		}); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
		}
	}
	return result, nil
}

// reserveSuccessorsInState places each named successor after every child
// provider generates at its sole per-subtask transition, in the transaction
// creating provider. Any ineligible successor refuses the whole request.
func reserveSuccessorsInState(state *models.State, resolver *pipeline.Resolver, provider *models.Task, successors []string, agentID string) error {
	if len(successors) == 0 {
		return nil
	}
	transition, err := soleOutgoingSubtaskTransition(resolver, provider.RolePair)
	if err != nil {
		return err
	}
	res := models.ProviderReservation{ProviderTask: provider.ID, Transition: transition}
	now := time.Now().UTC()
	reason := fmt.Sprintf("placed after %s by its commissioning add-task", provider.ID)
	for _, id := range successors {
		successor := state.FindTask(id)
		if successor == nil {
			return &PreconditionError{Reason: fmt.Sprintf("reserve_successors names %s, which does not exist", id)}
		}
		if _, err := addReservation(state, resolver, successor, res, agentID, reason, now); err != nil {
			return err
		}
	}
	return nil
}

// addReservation appends res to task, refusing a writer that has started work
// or is terminal: placement before it is no longer possible. Holding res
// already is a no-op.
func addReservation(state *models.State, resolver *pipeline.Resolver, task *models.Task, res models.ProviderReservation, agentID, reason string, now time.Time) (bool, error) {
	if slices.Contains(task.ProviderReservations, res) {
		return false, nil
	}
	if res.ProviderTask == task.ID {
		return false, &PreconditionError{Reason: fmt.Sprintf("task %s cannot be reserved behind itself", task.ID)}
	}
	if task.Status.IsTerminal() {
		return false, &PreconditionError{Reason: fmt.Sprintf("writer %s is terminal (%s); there is nothing left to place", task.ID, task.Status)}
	}
	if !reservationPlaceable(task, resolver) {
		return false, &PreconditionError{Reason: fmt.Sprintf("writer %s is %s and assigned or leased; placement before a writer that started is no longer possible, so the placement needs a new decision", task.ID, task.Status)}
	}
	task.ProviderReservations = append(task.ProviderReservations, res)
	recordReservationChange(task, res, agentID, reason, now, "provider_reservation_added")
	return true, nil
}

// reservationPlaceable reports a writer not working now: in its role pair's
// initial or rejected status or BLOCKED, with no assignee or lease. A writer
// released after an earlier claim is still placeable.
func reservationPlaceable(task *models.Task, resolver *pipeline.Resolver) bool {
	if (task.AssignedTo != nil && *task.AssignedTo != "") || task.LeaseExpires != nil {
		return false
	}
	if task.Status == models.TaskStatusBlocked {
		return true
	}
	if initial, err := resolver.InitialStatus(task.RolePair); err == nil && task.Status == initial {
		return true
	}
	rejected, err := resolver.RejectedStatus(task.RolePair)
	return err == nil && task.Status == rejected
}

// tightenProviderCap sets or lowers the effective provider's max_outputs,
// recording the change on the provider; a cap can never rise or be removed
// here.
func tightenProviderCap(state *models.State, res models.ProviderReservation, maxOutputs int, agentID, reason string, now time.Time) (bool, error) {
	provider := state.FindTask(models.EffectiveReservationProvider(state, res.ProviderTask))
	if provider == nil {
		return false, &PreconditionError{Reason: fmt.Sprintf("provider %s does not exist", res.ProviderTask)}
	}
	if provider.MaxOutputs == maxOutputs {
		return false, nil
	}
	if provider.MaxOutputs > 0 && maxOutputs > provider.MaxOutputs {
		return false, &PreconditionError{Reason: fmt.Sprintf("provider %s max_outputs is %d; %s only tightens a cap, never raises it", provider.ID, provider.MaxOutputs, reserveProviderOperation)}
	}
	if len(provider.Output) > maxOutputs {
		return false, &PreconditionError{Reason: fmt.Sprintf("provider %s already has %d output entries; max_outputs %d would not cover them", provider.ID, len(provider.Output), maxOutputs)}
	}
	note := fmt.Sprintf("max_outputs %d -> %d for reservations at %s", provider.MaxOutputs, maxOutputs, res.Transition)
	provider.History = append(provider.History, models.TaskHistoryEntry{
		Time: now, Event: models.TaskEventDependenciesRewritten, Agent: &agentID, Reason: &reason, Note: &note,
		Extra: map[string]any{
			"operation":          reserveProviderOperation,
			"max_outputs":        map[string]any{"previous": provider.MaxOutputs, "current": maxOutputs},
			"rewrote_depends_on": false,
		},
	})
	provider.MaxOutputs = maxOutputs
	return true, nil
}

func recordReservationChange(task *models.Task, res models.ProviderReservation, agentID, reason string, now time.Time, change string) {
	note := fmt.Sprintf("%s: %s at %s", change, res.ProviderTask, res.Transition)
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  models.TaskEventDependenciesRewritten,
		Agent:  &agentID,
		Reason: &reason,
		Note:   &note,
		Extra: map[string]any{
			"operation":          reserveProviderOperation,
			change:               map[string]any{"provider_task": res.ProviderTask, "transition": res.Transition},
			"rewrote_depends_on": false,
		},
	})
}

// rejectDroppedReservations refuses superseding a holder by successors that do
// not hold each of its unsatisfied reservations: they would race the writer it
// was placed after. It runs only at the supersession, so a later release on a
// successor stays an explicit policy decision.
func rejectDroppedReservations(state *models.State, resolver *pipeline.Resolver, task *models.Task, replacementIDs []string) error {
	for j, res := range task.ProviderReservations {
		if models.ResolveReservation(res, state, resolver).Satisfied() {
			continue
		}
		provider := models.EffectiveReservationProvider(state, res.ProviderTask)
		for _, successorID := range replacementIDs {
			successor := state.FindTask(successorID)
			if successor != nil && slices.ContainsFunc(successor.ProviderReservations, func(held models.ProviderReservation) bool {
				return held.Transition == res.Transition && models.EffectiveReservationProvider(state, held.ProviderTask) == provider
			}) {
				continue
			}
			return &PreconditionError{Reason: fmt.Sprintf("task %s cannot be superseded by %s while its provider_reservations[%d] on %s is unsatisfied; the successor would race that writer: release the reservation first or replace the task with replace-task, which carries it", task.ID, successorID, j, provider)}
		}
	}
	return nil
}

// soleOutgoingSubtaskTransition names the one per-subtask transition from
// rolePair: the transition whose children a reservation covers.
func soleOutgoingSubtaskTransition(resolver *pipeline.Resolver, rolePair string) (string, error) {
	var found []string
	for _, transition := range resolver.AllTransitions() {
		source, err := resolver.TransitionSourceRolePair(transition.Name)
		if err == nil && source == rolePair && transition.Cardinality == "per-subtask" {
			found = append(found, transition.Name)
		}
	}
	if len(found) != 1 {
		return "", &PreconditionError{Reason: fmt.Sprintf("reserve_successors needs a provider with one per-subtask transition: %s has %d", rolePair, len(found))}
	}
	return found[0], nil
}
