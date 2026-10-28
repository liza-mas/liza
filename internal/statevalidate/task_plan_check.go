package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validatePlanCheck checks the persisted shape of an orchestrator plan
// disposition. Admission rules (domain, dependencies, sticky holds) live in ops.
func validatePlanCheck(v *violations, task *models.Task, state *models.State, resolver *pipeline.Resolver) {
	check := task.PlanCheck
	if check == nil {
		return
	}
	switch check.Verdict {
	case models.PlanCheckPassed:
	case models.PlanCheckHeld:
		if strings.TrimSpace(check.Ask) == "" {
			v.add(fmt.Errorf("task %s plan_check held requires ask", task.ID))
		}
	case models.PlanCheckReplaced:
		replacement := state.FindTask(check.ReplacedBy)
		if replacement == nil || replacement.ID == task.ID || replacement.Status != models.TaskStatusMerged || replacement.RolePair != task.RolePair || len(replacement.Output) == 0 {
			v.add(fmt.Errorf("task %s plan_check replaced requires a distinct MERGED replaced_by task with output in the same role_pair", task.ID))
		}
		if check.Ask != "" {
			v.add(fmt.Errorf("task %s plan_check replaced cannot carry ask", task.ID))
		}
	case models.PlanCheckRetired:
		if strings.TrimSpace(check.Ruling) == "" {
			v.add(fmt.Errorf("task %s plan_check retired requires ruling", task.ID))
		}
		if check.Ask != "" {
			v.add(fmt.Errorf("task %s plan_check retired cannot carry ask", task.ID))
		}
	default:
		v.add(fmt.Errorf("task %s plan_check has invalid verdict %q", task.ID, check.Verdict))
	}
	if check.Verdict != models.PlanCheckReplaced && check.ReplacedBy != "" {
		v.add(fmt.Errorf("task %s plan_check replaced_by requires replaced verdict", task.ID))
	}
	if check.Verdict != models.PlanCheckRetired && check.Ruling != "" {
		v.add(fmt.Errorf("task %s plan_check ruling requires retired verdict", task.ID))
	}
	if check.By == "" {
		v.add(fmt.Errorf("task %s plan_check requires by and at (missing by)", task.ID))
	}
	if check.At.IsZero() {
		v.add(fmt.Errorf("task %s plan_check requires by and at (missing at)", task.ID))
	}
	if task.Status != models.TaskStatusMerged {
		v.addID(fmt.Sprintf("task %s plan_check requires MERGED status", task.ID),
			fmt.Errorf("task %s plan_check requires MERGED status, got %s", task.ID, task.Status))
	}
	if resolver != nil && !resolver.TransitionSourcePairs()[task.RolePair] {
		v.add(fmt.Errorf("task %s plan_check on non-planning role_pair %q", task.ID, task.RolePair))
	}
}
