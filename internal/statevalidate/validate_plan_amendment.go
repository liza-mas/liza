package statevalidate

import (
	"fmt"
	"reflect"
	"slices"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

func validatePlanAmendment(v *violations, task *models.Task, state *models.State, resolver *pipeline.Resolver) {
	if !task.AmendmentMode.IsValid() || task.AmendmentMode != "" && task.AmendsPlan == "" {
		v.add(fmt.Errorf("task %s has invalid amendment mode", task.ID))
	}
	if task.AmendsPlan != "" {
		original := state.FindTask(task.AmendsPlan)
		if original == nil || original.ID == task.ID || original.PlanAmendment == nil || !slices.Contains(original.PlanAmendment.Corrections, task.ID) || original.RolePair != task.RolePair || task.PlanAmendment != nil {
			v.add(fmt.Errorf("task %s has invalid reciprocal amends_plan lineage", task.ID))
			return
		}
		if len(task.TransitionsExecuted) > 0 || amendmentHasChildren(state, task.ID) {
			v.add(fmt.Errorf("correction %s cannot generate children or execute transitions", task.ID))
		}
		if task.MaxOutputs != original.MaxOutputs {
			v.add(fmt.Errorf("correction %s cannot change max_outputs", task.ID))
		}
		if task.PlanCheck != nil && (task.PlanCheck.Verdict != models.PlanCheckReplaced || task.PlanCheck.ReplacedBy != original.ID || !slices.Contains(original.PlanAmendment.Applied, task.ID) && !slices.Contains(original.PlanAmendment.Quarantined, task.ID)) {
			v.add(fmt.Errorf("correction %s cannot receive an ordinary plan-check disposition", task.ID))
		}
	}
	record := task.PlanAmendment
	if record == nil {
		return
	}
	if task.Status != models.TaskStatusMerged || task.AmendsPlan != "" || len(record.OriginalOutput) == 0 || resolver != nil && !resolver.TransitionSourcePairs()[task.RolePair] {
		v.add(fmt.Errorf("task %s plan_amendment requires a MERGED planning original with retained output", task.ID))
	}
	seen := map[string]bool{}
	previous := record.OriginalOutput
	var applied, quarantined []string
	for _, id := range record.Corrections {
		if seen[id] || id == task.ID {
			v.add(fmt.Errorf("task %s has duplicate/self correction %s", task.ID, id))
			continue
		}
		seen[id] = true
		correction := state.FindTask(id)
		if correction == nil || correction.AmendsPlan != task.ID || correction.RolePair != task.RolePair {
			v.add(fmt.Errorf("task %s correction %s is missing or has inconsistent lineage", task.ID, id))
			continue
		}
		isApplied, isQuarantined := slices.Contains(record.Applied, id), slices.Contains(record.Quarantined, id)
		if isApplied && isQuarantined || isApplied && record.Pending == id || isQuarantined && record.Pending == id || !isApplied && !isQuarantined && record.Pending != id {
			v.add(fmt.Errorf("task %s correction %s must have exactly one disposition", task.ID, id))
		}
		if len(correction.Output) > 0 {
			if err := models.ValidateAmendmentOutputForMode(previous, correction.Output, correction.AmendmentMode); err != nil {
				v.add(fmt.Errorf("correction %s: %w", id, err))
			}
		}
		if isApplied {
			applied = append(applied, id)
			if correction.Status != models.TaskStatusMerged || len(correction.Output) == 0 || !correction.PlanHandoffRetired() {
				v.add(fmt.Errorf("applied correction %s must remain MERGED with retired handoff and reviewed output", id))
			}
			previous = correction.Output
		}
		if isQuarantined {
			quarantined = append(quarantined, id)
			if correction.Status != models.TaskStatusMerged && correction.Status != models.TaskStatusAbandoned || correction.Status == models.TaskStatusMerged && !correction.PlanHandoffRetired() {
				v.add(fmt.Errorf("quarantined correction %s must remain MERGED/ABANDONED with fenced handoff", id))
			}
		}
	}
	if !slices.Equal(applied, record.Applied) || !slices.Equal(quarantined, record.Quarantined) {
		v.add(fmt.Errorf("task %s amendment dispositions must follow unique creation order", task.ID))
	}
	if !reflect.DeepEqual(task.Output, previous) {
		v.add(fmt.Errorf("task %s output does not match its latest applied amendment manifest", task.ID))
	}
	if record.Pending != "" {
		if len(record.Corrections) == 0 || record.Corrections[len(record.Corrections)-1] != record.Pending || !seen[record.Pending] {
			v.add(fmt.Errorf("task %s pending amendment must be its newest correction", task.ID))
		}
		pending := state.FindTask(record.Pending)
		expanded := len(task.TransitionsExecuted) > 0 || amendmentHasChildren(state, task.ID)
		if pending != nil && (pending.AmendmentMode == models.PlanAmendmentContract || expanded && pending.AmendmentMode != "") && task.EffectiveType() != models.TaskTypeArchitecture {
			v.add(fmt.Errorf("task %s expanded contract amendment requires architecture type", task.ID))
		}
		if pending != nil && pending.AmendmentMode == "" && expanded || task.PlanHandoffRetired() || task.TransitionsExecuted["replanned"] || task.PlanCheckVerdictOf() == models.PlanCheckPassed {
			v.add(fmt.Errorf("task %s pending amendment requires an unused original without a pass or retirement", task.ID))
		}
	}
}

func amendmentHasChildren(state *models.State, id string) bool {
	for i := range state.Tasks {
		if slices.Contains(state.Tasks[i].EffectiveParentTasks(), id) {
			return true
		}
	}
	return false
}

// Project a draft correction onto the original provider namespace. A correction
// never owns child identities; consumers continue selecting original slots.
func amendmentProviderProjection(state *models.State) *models.State {
	if state == nil {
		return nil
	}
	projected := *state
	projected.Tasks = slices.Clone(state.Tasks)
	for i := range projected.Tasks {
		original := &projected.Tasks[i]
		if original.PlanAmendment == nil || original.PlanAmendment.Pending == "" {
			continue
		}
		if correction := state.FindTask(original.PlanAmendment.Pending); correction != nil && len(correction.Output) > 0 {
			original.Output = correction.Output
		}
	}
	return &projected
}
