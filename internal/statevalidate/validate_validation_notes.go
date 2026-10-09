package statevalidate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateValidationNotes keeps advisory records bounded and tied to their source.
func validateValidationNotes(v *violations, task *models.Task, state *models.State, resolver *pipeline.Resolver) {
	if task.PlanCheck != nil {
		if len(task.PlanCheck.Notes) > 0 && task.PlanCheck.Verdict != models.PlanCheckPassed {
			v.add(fmt.Errorf("task %s plan_check notes require passed verdict", task.ID))
		}
		if err := models.ValidatePlanValidationNotes(task.PlanCheck.Notes, len(task.Output)); err != nil {
			v.add(fmt.Errorf("task %s plan_check: %w", task.ID, err))
		}
	}
	childNotes := make([]models.PlanValidationNote, len(task.ValidationNotes))
	for i, note := range task.ValidationNotes {
		childNotes[i] = models.PlanValidationNote{OutputIndex: i, Message: note.Message}
		parent := state.FindTask(note.ParentTask)
		if strings.TrimSpace(note.ParentTask) == "" || !slices.Contains(task.EffectiveParentTasks(), note.ParentTask) || parent == nil {
			v.add(fmt.Errorf("task %s validation_notes[%d] requires its originating parent", task.ID, i))
			continue
		}
		if err := models.ValidatePlanValidationNotes([]models.PlanValidationNote{{OutputIndex: note.OutputIndex, Message: note.Message}}, len(parent.Output)); err != nil {
			v.add(fmt.Errorf("task %s validation_notes[%d]: %w", task.ID, i, err))
		}
		if !slices.Equal(task.ValidationNotes, parent.ValidationNotesForOutput(note.OutputIndex)) {
			v.add(fmt.Errorf("task %s validation_notes must match the originating plan disposition", task.ID))
		}
		if !validationNoteChildIdentity(task, parent, note.OutputIndex, state, resolver) {
			v.add(fmt.Errorf("task %s validation_notes[%d] selects another output's child", task.ID, i))
		}
	}
	if err := models.ValidatePlanValidationNotes(childNotes, len(childNotes)); err != nil {
		v.add(fmt.Errorf("task %s validation_notes: %w", task.ID, err))
	}
}

func validationNoteChildIdentity(task, parent *models.Task, index int, state *models.State, resolver *pipeline.Resolver) bool {
	if resolver == nil || len(task.EffectiveParentTasks()) != 1 {
		return false
	}
	for transition, executed := range parent.TransitionsExecuted {
		if !executed {
			continue
		}
		declaration := models.ProviderDependency{ProviderTask: parent.ID, Transition: transition, Outputs: []int{index}}
		definition, ids, err := models.ProviderDependencyChildren(declaration, resolver)
		if err != nil || definition.SourceRolePair != parent.RolePair || definition.TargetRolePair != task.RolePair {
			continue
		}
		if ids[0] == task.ID {
			return true
		}
		if successor, found := models.ProviderChildSuccessor(state, ids[0]); found && successor.ID == task.ID {
			return true
		}
	}
	return false
}
