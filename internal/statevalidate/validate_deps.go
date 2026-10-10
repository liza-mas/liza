package statevalidate

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// DependencyCycleError reports an ordered, closed path through a dependency cycle.
type DependencyCycleError struct {
	CyclePath []string
}

func (e *DependencyCycleError) Error() string {
	if len(e.CyclePath) == 0 {
		return "circular dependency detected"
	}
	return fmt.Sprintf("circular dependency detected: %s eventually depends on itself", e.CyclePath[0])
}

// validateDependencies checks referential integrity and ordering constraints
// for task dependencies: every depends_on entry must reference an existing task,
// executing tasks must have all dependencies satisfied, and the dependency graph
// must be acyclic. Prevents agents from starting work on tasks whose
// prerequisites are incomplete and detects dependency cycles that would
// deadlock the scheduler.
func validateDependencies(v *violations, state *models.State, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig, warnWriter io.Writer) {
	for _, task := range state.Tasks {
		if err := models.ValidateCodingAllocationOutput(&task, task.Output); err != nil {
			v.add(fmt.Errorf("task %s: %w", task.ID, err))
		}
		validateDependenciesForTask(v, state, resolver, cfg, warnWriter, &task)
	}

	validateDependencyCycles(v, state)
	validateProviderDependencies(v, state, resolver)
	// A cap binds with or without provider declarations (D-80).
	validateOutputCaps(v, state, resolver)
}

// validateDependencyCycles reports every depends_on edge that lies on a cycle,
// one violation per edge, identified by the edge. A per-task search stops at
// the first cycle it meets, so an old cycle through a task would hide a new
// one through the same task; edges do not: closing a new cycle, even among
// tasks already on other cycles, makes at least one edge cyclic that was not,
// and breaking a cycle retires its edges without making any other look new.
// Each violation carries the cycle through its edge: the edge, then the
// shortest path back.
func validateDependencyCycles(v *violations, state *models.State) {
	// A duplicated ID resolves to its first task, as State.FindTask and so
	// every operation resolve it; later copies are the duplicate-ID
	// violation's concern, not edges of the graph operations follow.
	edges := make(map[string][]string, len(state.Tasks))
	var order []string
	for _, task := range state.Tasks {
		if _, exists := edges[task.ID]; exists {
			continue
		}
		edges[task.ID] = uniqueStrings(task.DependsOn)
		order = append(order, task.ID)
	}
	for _, taskID := range order {
		for _, depID := range edges[taskID] {
			path := shortestDependencyPath(edges, depID, taskID)
			if path == nil {
				continue
			}
			v.addID(fmt.Sprintf("dependency cycle edge %s -> %s", taskID, depID),
				&DependencyCycleError{CyclePath: append([]string{taskID}, path...)})
		}
	}
}

// shortestDependencyPath returns the shortest depends_on path from one task to
// another, both ends included, or nil when there is none. Neighbours are
// visited in depends_on order, so the path is deterministic.
func shortestDependencyPath(edges map[string][]string, from, to string) []string {
	if from == to {
		return []string{from}
	}
	parent := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range edges[current] {
			if _, seen := parent[next]; seen {
				continue
			}
			parent[next] = current
			if next == to {
				path := []string{to}
				for node := current; node != ""; node = parent[node] {
					path = append([]string{node}, path...)
				}
				return path
			}
			queue = append(queue, next)
		}
	}
	return nil
}

func uniqueStrings(values []string) []string {
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !slices.Contains(unique, value) {
			unique = append(unique, value)
		}
	}
	return unique
}

// validateDependenciesForTask checks each depends_on entry of task on its own,
// so one bad entry does not hide another.
func validateDependenciesForTask(v *violations, state *models.State, resolver *pipeline.Resolver, cfg *pipeline.PipelineConfig, warnWriter io.Writer, task *models.Task) {
	if task == nil || len(task.DependsOn) == 0 {
		return
	}

	taskIDs := buildTaskIDSet(state.Tasks)
	sc := newStatusClassifier(resolver, cfg)
	depResolver := models.NewDependencyResolver(state)
	seenDeps := make(map[string]bool, len(task.DependsOn))
	// Guard: an entry that is not a reference to another existing task has
	// nothing to resolve; resolving it would only restate the same defect.
	unresolvable := make(map[string]bool)
	for _, depID := range task.DependsOn {
		switch {
		case strings.TrimSpace(depID) != depID || depID == "":
			v.add(fmt.Errorf("task %s has invalid depends_on entry %q (must be non-empty and trimmed)", task.ID, depID))
			unresolvable[depID] = true
		case depID == task.ID:
			v.add(fmt.Errorf("task %s has depends_on referencing itself", task.ID))
			unresolvable[depID] = true
		case seenDeps[depID]:
			v.add(fmt.Errorf("task %s has duplicate depends_on entry %q", task.ID, depID))
		case !taskIDs[depID]:
			v.add(fmt.Errorf("task %s has depends_on referencing non-existent task '%s'", task.ID, depID))
			unresolvable[depID] = true
		}
		seenDeps[depID] = true
	}

	var unmet []string
	resolved := make(map[string]bool, len(task.DependsOn))
	for _, depID := range task.DependsOn {
		if unresolvable[depID] || resolved[depID] {
			continue
		}
		resolved[depID] = true
		result := depResolver.Resolve(depID)
		if result.Invalid() {
			v.add(fmt.Errorf("task %s has invalid dependency %s", task.ID, result.Summary()))
			continue
		}
		if !task.Status.IsTerminal() {
			depTask := state.FindTask(depID)
			if depTask != nil && depTask.Status.IsTerminal() && depTask.Status != models.TaskStatusMerged {
				v.add(fmt.Errorf("non-terminal task %s depends on terminal non-merged task %s (%s)", task.ID, depID, depTask.Status))
			}
		}
		v.add(validateDependencyDirection(state, resolver, task, depID, result))
		if result.Kind == models.DependencySatisfiedViaSupersession && warnWriter != nil {
			fmt.Fprintf(warnWriter, "WARNING: task %s dependency %s satisfied via supersession path: %s\n", task.ID, depID, strings.Join(result.Path, " -> "))
		}
		if !result.Satisfied() {
			unmet = append(unmet, result.Summary())
			if !sc.IsExecuting(task.Status) && warnWriter != nil && result.ViaSupersession() {
				fmt.Fprintf(warnWriter, "WARNING: task %s dependency %s is not satisfied via supersession path: %s; blocking: %s\n",
					task.ID, depID, strings.Join(result.Path, " -> "), strings.Join(result.BlockingIDs, ", "))
			}
		}
	}

	if sc.IsExecuting(task.Status) {
		// One violation per unmet dependency: satisfying one of several is a
		// repair, and a newly unmet one is new.
		for _, summary := range unmet {
			v.add(fmt.Errorf("executing task %s has unmet dependencies: %s", task.ID, summary))
		}
	}
}

func validateDependencyDirection(state *models.State, resolver *pipeline.Resolver, task *models.Task, depID string, result models.DependencySatisfaction) error {
	if state == nil || resolver == nil || task == nil || task.RolePair == "" {
		return nil
	}
	depTask := state.FindTask(depID)
	if depTask == nil || depTask.RolePair == "" {
		return nil
	}
	downstream, err := resolver.IsRolePairDownstream(task.RolePair, depTask.RolePair)
	if err != nil {
		return err
	}
	if downstream {
		return fmt.Errorf("task %s has downstream dependency %s: role_pair %s is downstream of %s",
			task.ID, depTask.ID, depTask.RolePair, task.RolePair)
	}
	return nil
}

func warnBlockedReasonMissingDependsOn(state *models.State, warnWriter io.Writer) {
	if warnWriter == nil {
		return
	}
	var taskIDs []string
	for _, task := range state.Tasks {
		if task.ID != "" {
			taskIDs = append(taskIDs, task.ID)
		}
	}
	slices.Sort(taskIDs)
	for _, task := range state.Tasks {
		if task.Status != models.TaskStatusBlocked || len(task.DependsOn) > 0 || task.BlockedReason == nil {
			continue
		}
		reason := *task.BlockedReason
		for _, id := range taskIDs {
			if id == "" || id == task.ID {
				continue
			}
			if containsTaskID(reason, id) {
				fmt.Fprintf(warnWriter, "WARNING: BLOCKED task %s blocked_reason references task %s but depends_on is empty; add depends_on so orchestrator can re-wake when the blocker changes\n", task.ID, id)
				break
			}
		}
	}
}

func containsTaskID(text, id string) bool {
	if id == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(text[start:], id)
		if idx < 0 {
			return false
		}
		matchStart := start + idx
		matchEnd := matchStart + len(id)
		if hasTaskIDBoundary(text, matchStart-1) && hasTaskIDBoundary(text, matchEnd) {
			return true
		}
		start = matchStart + 1
	}
}

func hasTaskIDBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	return !isTaskIDChar(text[index])
}

func isTaskIDChar(ch byte) bool {
	return ch >= 'a' && ch <= 'z' ||
		ch >= 'A' && ch <= 'Z' ||
		ch >= '0' && ch <= '9' ||
		ch == '.' ||
		ch == '_' ||
		ch == '-'
}

// checkCircular performs a depth-first traversal of the dependency graph
// starting from 'start', detecting if any path leads back to it. Returns an
// error describing the cycle when one is found.
func checkCircular(start, current string, visited map[string]bool, state *models.State) error {
	return checkCircularPath(start, current, visited, state, []string{current})
}

func checkCircularPath(start, current string, visited map[string]bool, state *models.State, path []string) error {
	task := state.FindTask(current)
	if task == nil || len(task.DependsOn) == 0 {
		return nil
	}

	for _, depID := range task.DependsOn {
		if depID == start {
			cyclePath := append(slices.Clone(path), start)
			return &DependencyCycleError{CyclePath: cyclePath}
		}
		if !visited[depID] {
			visited[depID] = true
			nextPath := append(slices.Clone(path), depID)
			if err := checkCircularPath(start, depID, visited, state, nextPath); err != nil {
				return err
			}
		}
	}

	return nil
}
