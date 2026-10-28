package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/models"
)

// Replacement-lineage and repair resolution of the integration progress
// evaluator.

type lineageResolution struct {
	settled bool
	merged  bool
	blocked bool
	taskIDs []string
}

func (e *integrationProgressEvaluator) resolveLineage(rootID string, abandonedBlocks bool) (lineageResolution, error) {
	return e.resolveLineageWithStack(rootID, abandonedBlocks, make(map[string]bool))
}

func (e *integrationProgressEvaluator) resolveLineageWithStack(
	taskID string,
	abandonedBlocks bool,
	visiting map[string]bool,
) (lineageResolution, error) {
	if visiting[taskID] {
		return lineageResolution{}, fmt.Errorf("integration progress: replacement cycle includes %q", taskID)
	}
	task := e.tasks[taskID]
	if task == nil {
		return lineageResolution{}, fmt.Errorf("integration progress: replacement task %q is missing", taskID)
	}
	switch task.Status {
	case models.TaskStatusMerged:
		return lineageResolution{settled: true, merged: true}, nil
	case models.TaskStatusAbandoned:
		if abandonedBlocks {
			return lineageResolution{settled: true, blocked: true, taskIDs: []string{taskID}}, nil
		}
		return lineageResolution{settled: true}, nil
	case models.TaskStatusBlocked:
		if abandonedBlocks {
			return lineageResolution{settled: true, blocked: true, taskIDs: []string{taskID}}, nil
		}
		return lineageResolution{taskIDs: []string{taskID}}, nil
	case models.TaskStatusSuperseded:
		if len(task.SupersededBy) == 0 {
			// supersede-task allows no replacements (work done elsewhere, with a recorded
			// recoverability command): the lineage settles as an abandoned one does.
			if abandonedBlocks {
				return lineageResolution{settled: true, blocked: true, taskIDs: []string{taskID}}, nil
			}
			return lineageResolution{settled: true}, nil
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		combined := lineageResolution{settled: true}
		for _, replacementID := range uniqueSortedStrings(task.SupersededBy) {
			resolution, err := e.resolveLineageWithStack(replacementID, abandonedBlocks, visiting)
			if err != nil {
				return lineageResolution{}, err
			}
			combined.settled = combined.settled && resolution.settled
			combined.merged = combined.merged || resolution.merged
			combined.blocked = combined.blocked || resolution.blocked
			combined.taskIDs = append(combined.taskIDs, resolution.taskIDs...)
		}
		combined.taskIDs = uniqueSortedStrings(combined.taskIDs)
		return combined, nil
	default:
		return lineageResolution{taskIDs: []string{taskID}}, nil
	}
}

func (e *integrationProgressEvaluator) resolveAnalysisRepairs(analysisTaskID string) (lineageResolution, error) {
	children := make([]string, 0)
	for _, childID := range e.children[analysisTaskID] {
		if e.tasks[childID].IntegrationAnalysis == nil {
			children = append(children, childID)
		}
	}
	children = uniqueSortedStrings(children)
	if len(children) == 0 {
		return lineageResolution{taskIDs: []string{analysisTaskID}}, nil
	}

	leaves, err := e.repairLeaves(children)
	if err != nil {
		return lineageResolution{}, err
	}
	result := lineageResolution{settled: true, merged: true}
	for _, leafID := range leaves {
		leaf := e.tasks[leafID]
		if leaf.RolePair == codePlanningRolePair && len(leaf.Output) > 0 {
			result.settled = false
			result.merged = false
			result.taskIDs = append(result.taskIDs, leafID)
			continue
		}
		switch leaf.Status {
		case models.TaskStatusMerged:
		case models.TaskStatusBlocked, models.TaskStatusAbandoned:
			result.blocked = true
			result.taskIDs = append(result.taskIDs, leafID)
		default:
			result.settled = false
			result.merged = false
			result.taskIDs = append(result.taskIDs, leafID)
		}
	}
	result.taskIDs = uniqueSortedStrings(result.taskIDs)
	return result, nil
}

func (e *integrationProgressEvaluator) repairLeaves(roots []string) ([]string, error) {
	leaves := make([]string, 0)
	visiting := make(map[string]bool)
	visited := make(map[string]bool)
	var walk func(string) error
	walk = func(taskID string) error {
		if visiting[taskID] {
			return fmt.Errorf("integration progress: repair lineage cycle includes %q", taskID)
		}
		if visited[taskID] {
			return nil
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		children := make([]string, 0)
		for _, childID := range e.children[taskID] {
			if e.tasks[childID].IntegrationAnalysis == nil {
				children = append(children, childID)
			}
		}
		children = uniqueSortedStrings(children)
		if len(children) == 0 {
			if task := e.tasks[taskID]; task.Status == models.TaskStatusSuperseded {
				return fmt.Errorf("integration progress: superseded repair %q has no replacement leaves", taskID)
			}
			leaves = append(leaves, taskID)
		} else {
			for _, childID := range children {
				if err := walk(childID); err != nil {
					return err
				}
			}
		}
		visited[taskID] = true
		return nil
	}
	for _, rootID := range roots {
		if err := walk(rootID); err != nil {
			return nil, err
		}
	}
	return uniqueSortedStrings(leaves), nil
}

func (e *integrationProgressEvaluator) mergedLineageLeaves(rootID string) ([]string, error) {
	leaves := make([]string, 0)
	var walk func(string, map[string]bool) error
	walk = func(taskID string, visiting map[string]bool) error {
		if visiting[taskID] {
			return fmt.Errorf("integration progress: replacement cycle includes %q", taskID)
		}
		task := e.tasks[taskID]
		if task == nil {
			return fmt.Errorf("integration progress: replacement task %q is missing", taskID)
		}
		if task.Status != models.TaskStatusSuperseded {
			if task.Status == models.TaskStatusMerged {
				leaves = append(leaves, taskID)
			}
			return nil
		}
		if len(task.SupersededBy) == 0 {
			// Superseded without replacements: no merged leaf on this branch.
			return nil
		}
		visiting[taskID] = true
		defer delete(visiting, taskID)
		for _, replacementID := range uniqueSortedStrings(task.SupersededBy) {
			if err := walk(replacementID, visiting); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootID, make(map[string]bool)); err != nil {
		return nil, err
	}
	return uniqueSortedStrings(leaves), nil
}

func (e *integrationProgressEvaluator) lineageContains(rootID, candidateID string) bool {
	if rootID == candidateID {
		return true
	}
	visited := make(map[string]bool)
	queue := []string{rootID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if visited[current] {
			continue
		}
		visited[current] = true
		for _, childID := range e.children[current] {
			if !slices.Contains(e.tasks[current].SupersededBy, childID) {
				continue
			}
			if childID == candidateID {
				return true
			}
			queue = append(queue, childID)
		}
	}
	return false
}
