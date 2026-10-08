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
	"github.com/liza-mas/liza/internal/statevalidate"
)

const deferProviderDependencyOperation = "defer-provider-dependency"

// DeferProviderDependencyResult reports the declaration an operator deferral moved.
type DeferProviderDependencyResult struct {
	TaskID       string   `json:"task_id"`
	ProviderTask string   `json:"provider_task"`
	Transition   string   `json:"transition"`
	AtTransition string   `json:"at_transition"`
	Warnings     []string `json:"warnings,omitempty"`
}

// DeferProviderDependency moves one task-level provider declaration of a task
// nobody has started into its descendant declarations (D-79, ADR-0193). A
// writer-order wait placed on a plan holds planning until the provider's
// writers merge; deferred, it holds only the writers the plan generates. The
// move is in place: the reviewed parent output, decomposition and arch_ref
// are untouched, and the generated-declaration match accepts the move. Only a
// wait between tasks of the same stage can move: the provider children it
// selects must share the role pair of the writers it will hold, so a planning
// prerequisite is never loosened. The CLI owns the operator boundary.
func DeferProviderDependency(projectRoot, taskID, providerTask, transition, reason string) (*DeferProviderDependencyResult, error) {
	for _, field := range []struct{ name, value string }{{"task ID", taskID}, {"provider task", providerTask}, {"transition", transition}, {"reason", reason}} {
		if strings.TrimSpace(field.value) == "" {
			return nil, &PreconditionError{Reason: field.name + " is required"}
		}
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	var result *DeferProviderDependencyResult
	err = bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if !models.UnstartedProviderConsumer(task, resolver) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not unstarted (assigned, leased, holding a worktree or hand-off, claimed before, or past its initial status); %s only moves a declaration of a task nobody has started", taskID, deferProviderDependencyOperation)}
		}
		index := slices.IndexFunc(task.ProviderDependencies, func(dep models.ProviderDependency) bool {
			return dep.ProviderTask == providerTask && dep.Transition == transition
		})
		if index < 0 {
			return &PreconditionError{Reason: fmt.Sprintf("task %s does not declare provider_dependencies on %s %s", taskID, providerTask, transition)}
		}
		atTransition, err := statevalidate.DescendantTransition(resolver, task.RolePair, false)
		if err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s: %v", taskID, err)}
		}
		selected, err := resolver.TransitionTargetRolePair(transition)
		if err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		held, err := resolver.TransitionTargetRolePair(atTransition)
		if err != nil {
			return err
		}
		if selected != held {
			return &PreconditionError{Reason: fmt.Sprintf("provider_dependencies on %s %s selects %s tasks, but deferring it to %s would hold %s tasks; only a wait between tasks of the same stage can be deferred", providerTask, transition, selected, atTransition, held)}
		}

		dep := task.ProviderDependencies[index]
		task.ProviderDependencies = slices.Delete(slices.Clone(task.ProviderDependencies), index, index+1)
		task.DescendantDependencies = models.CloneDescendantDependencies(task.DescendantDependencies)
		if at := slices.IndexFunc(task.DescendantDependencies, func(d models.DescendantDependency) bool { return d.AtTransition == atTransition }); at >= 0 {
			task.DescendantDependencies[at].ProviderDependencies = append(task.DescendantDependencies[at].ProviderDependencies, dep)
		} else {
			task.DescendantDependencies = append(task.DescendantDependencies, models.DescendantDependency{AtTransition: atTransition, ProviderDependencies: []models.ProviderDependency{dep}})
		}
		note := fmt.Sprintf("provider wait on %s %s outputs %v deferred to the outputs generated at %s", providerTask, transition, dep.Outputs, atTransition)
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  string(models.TaskEventDependencyDeferred),
			Reason: &reason,
			Note:   &note,
			Extra: map[string]any{
				"operation":     deferProviderDependencyOperation,
				"source":        "operator_cli",
				"provider_task": providerTask,
				"transition":    transition,
				"outputs":       slices.Clone(dep.Outputs),
				"at_transition": atTransition,
			},
		})
		result = &DeferProviderDependencyResult{TaskID: taskID, ProviderTask: providerTask, Transition: transition, AtTransition: atTransition}
		return statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr)
	})
	if err != nil {
		return nil, fmt.Errorf("defer provider dependency: %w", err)
	}

	if err := log.New(lp.LogPath()).Append(log.Entry{
		Timestamp: now,
		Agent:     "operator",
		Action:    string(models.TaskEventDependencyDeferred),
		Task:      &taskID,
		Detail:    fmt.Sprintf("%s %s -> %s: %s", providerTask, transition, result.AtTransition, reason),
	}); err != nil {
		result.Warnings = append(result.Warnings, "deferral persisted; activity log write failed. Do not retry the deferral.")
	}
	return result, nil
}
