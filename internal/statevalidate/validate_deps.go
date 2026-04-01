package statevalidate

import (
	"fmt"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateDependencies checks referential integrity and ordering constraints
// for task dependencies: every depends_on entry must reference an existing task,
// executing tasks must have all dependencies in MERGED or SUPERSEDED status, and
// the dependency graph must be acyclic. Prevents agents from starting work on
// tasks whose prerequisites are incomplete and detects dependency cycles that
// would deadlock the scheduler.
func validateDependencies(state *models.State, projectRoot string, skipSpecFileCheck bool, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig) error {
	taskIDs := buildTaskIDSet(state.Tasks)
	sc := newStatusClassifier(resolver, cfg)

	for _, task := range state.Tasks {
		if len(task.DependsOn) == 0 {
			continue
		}

		// All dependencies must reference existing tasks
		for _, depID := range task.DependsOn {
			if !taskIDs[depID] {
				return fmt.Errorf("task %s has depends_on referencing non-existent task '%s'", task.ID, depID)
			}
		}

		// Executing tasks must have all dependencies satisfied (MERGED or SUPERSEDED)
		if sc.IsExecuting(task.Status) {
			var unmet []string
			for _, depID := range task.DependsOn {
				depTask := state.FindTask(depID)
				if depTask != nil && depTask.Status != models.TaskStatusMerged && depTask.Status != models.TaskStatusSuperseded {
					unmet = append(unmet, depID)
				}
			}
			if len(unmet) > 0 {
				return fmt.Errorf("executing task %s has unmet dependencies: %s (must be MERGED or SUPERSEDED)", task.ID, strings.Join(unmet, ", "))
			}
		}
	}

	for _, task := range state.Tasks {
		if len(task.DependsOn) == 0 {
			continue
		}

		visited := make(map[string]bool)
		if err := checkCircular(task.ID, task.ID, visited, state); err != nil {
			return err
		}
	}

	return nil
}

// checkCircular performs a depth-first traversal of the dependency graph
// starting from 'start', detecting if any path leads back to it. Returns an
// error describing the cycle when one is found.
func checkCircular(start, current string, visited map[string]bool, state *models.State) error {
	task := state.FindTask(current)
	if task == nil || len(task.DependsOn) == 0 {
		return nil
	}

	for _, depID := range task.DependsOn {
		if depID == start {
			return fmt.Errorf("circular dependency detected: %s eventually depends on itself", start)
		}
		if !visited[depID] {
			visited[depID] = true
			if err := checkCircular(start, depID, visited, state); err != nil {
				return err
			}
		}
	}

	return nil
}
