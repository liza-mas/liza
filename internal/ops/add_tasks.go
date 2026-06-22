package ops

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// AddTaskInput represents the input parameters for adding a task.
type AddTaskInput struct {
	ID            string   `json:"id"`
	Type          string   `json:"type,omitempty"`
	RolePair      string   `json:"role_pair,omitempty"`
	Description   string   `json:"desc"`
	SpecRef       string   `json:"spec"`
	PlanRef       string   `json:"plan_ref,omitempty"`
	DoneWhen      string   `json:"done"`
	Validation    []string `json:"validation,omitempty"`
	DestructiveDB bool     `json:"destructive_db,omitempty"`
	Scope         string   `json:"scope"`
	Priority      int      `json:"priority"`
	DependsOn     []string `json:"depends,omitempty"`
}

// AddTaskResult contains the outcome of adding a task.
type AddTaskResult struct {
	TaskID   string   `json:"task_id"`
	Warnings []string `json:"warnings"`
}

// AddTask atomically persists a new task after validating inputs and checking
// for duplicates. Also updates sprint.scope.planned, goal.alignment_history,
// and appends to the activity log. No terminal I/O.
func AddTask(statePath, logPath string, input *AddTaskInput, orchestratorID string) (*AddTaskResult, error) {
	if orchestratorID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	if err := paths.ValidateTaskID(input.ID); err != nil {
		return nil, fmt.Errorf("invalid task ID: %w", err)
	}
	if input.Description == "" {
		return nil, &PreconditionError{Reason: "description is required"}
	}
	if input.SpecRef == "" {
		return nil, &PreconditionError{Reason: "spec_ref is required"}
	}
	if err := statevalidate.ValidateArtifactRefScalar("spec_ref", input.SpecRef, input.ID); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if err := statevalidate.ValidateArtifactRefScalar("plan_ref", input.PlanRef, input.ID); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.DoneWhen == "" {
		return nil, &PreconditionError{Reason: "done_when is required"}
	}
	if err := models.ValidateValidationSafety("validation", input.Validation, input.DestructiveDB); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.Scope == "" {
		return nil, &PreconditionError{Reason: "scope is required"}
	}
	if input.Priority < 1 {
		return nil, &PreconditionError{Reason: fmt.Sprintf("priority must be positive, got %d", input.Priority)}
	}

	var taskType models.TaskType
	if input.Type != "" {
		taskType = models.TaskType(input.Type)
		if !taskType.IsValid() {
			return nil, &PreconditionError{Reason: fmt.Sprintf("unknown task type %q; valid types: %s",
				input.Type, strings.Join(models.ValidTaskTypeNames(), ", "))}
		}
	}

	// Derive project root from state path (.liza/state.yaml → project root)
	projectRoot := filepath.Dir(filepath.Dir(statePath))
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	if input.RolePair == "" {
		return nil, &PreconditionError{
			Reason: fmt.Sprintf("role_pair is required; available: %s",
				strings.Join(resolver.RolePairNames(), ", ")),
		}
	}
	rp, rpErr := resolver.RolePair(input.RolePair)
	if rpErr != nil {
		return nil, &PreconditionError{
			Reason: fmt.Sprintf("unknown role_pair %q; available role_pairs: %s",
				input.RolePair, strings.Join(resolver.RolePairNames(), ", ")),
		}
	}

	expectedTaskType := models.TaskTypeForRole(rp.Doer)
	if input.Type == "" {
		taskType = expectedTaskType
		input.Type = string(taskType)
	} else if taskType != expectedTaskType {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task type %q conflicts with role_pair %q (expected %q)",
			input.Type, input.RolePair, expectedTaskType)}
	}

	normalizedDeps := []string{}
	for _, dep := range input.DependsOn {
		trimmed := strings.TrimSpace(dep)
		if trimmed != "" {
			normalizedDeps = append(normalizedDeps, trimmed)
		}
	}

	now := time.Now().UTC()
	agentID := orchestratorID

	bb := db.For(statePath)

	initialStatus, err := resolver.InitialStatus(input.RolePair)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve initial status for role-pair %q: %w", input.RolePair, err)
	}

	newTask := models.Task{
		ID:            input.ID,
		Type:          taskType,
		RolePair:      input.RolePair,
		Description:   input.Description,
		Status:        initialStatus,
		Priority:      input.Priority,
		SpecRef:       paths.NormalizeSpecRef(input.SpecRef),
		PlanRef:       paths.NormalizeSpecRef(input.PlanRef),
		DoneWhen:      input.DoneWhen,
		Validation:    slices.Clone(input.Validation),
		DestructiveDB: input.DestructiveDB,
		Scope:         input.Scope,
		DependsOn:     normalizedDeps,
		Created:       now,
		History:       []models.TaskHistoryEntry{},
	}

	var postValidationErr error
	err = bb.Modify(func(state *models.State) error {
		if state.FindTask(input.ID) != nil {
			return &PreconditionError{Reason: fmt.Sprintf("task '%s' already exists", input.ID)}
		}
		if err := rejectManualPipelineChildTask(state, input, resolver); err != nil {
			return err
		}
		state.Tasks = append(state.Tasks, newTask)

		if !slices.Contains(state.Sprint.Scope.Planned, input.ID) {
			state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, input.ID)
		}

		alignmentEntry := models.AlignmentHistory{
			Timestamp: now,
			Event:     models.TaskEventPlanning,
			Summary:   fmt.Sprintf("Added task %s: %s", input.ID, input.Description),
		}
		state.Goal.AlignmentHistory = append(state.Goal.AlignmentHistory, alignmentEntry)

		if err := statevalidate.ValidateAddedTask(state, projectRoot, input.ID, false, io.Discard); err != nil {
			return err
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			postValidationErr = err
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to add task: %w", err)
	}

	result := &AddTaskResult{TaskID: input.ID}
	if postValidationErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("state remains degraded after add-task; full validation failed: %v", postValidationErr))
	}

	logger := log.New(logPath)
	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    "task_added",
		Task:      &input.ID,
		Detail:    input.Description,
	}

	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return result, nil
}

func rejectManualPipelineChildTask(state *models.State, input *AddTaskInput, resolver *pipeline.Resolver) error {
	for _, td := range resolver.AllTransitions() {
		tDef, err := buildTransitionDefFromPipeline(resolver, td.Name)
		if err != nil {
			continue
		}
		for i := range state.Tasks {
			source := &state.Tasks[i]
			if source.RolePair != tDef.sourceRolePair {
				continue
			}
			switch td.Cardinality {
			case "per-subtask":
				for outputIdx := range source.Output {
					if isTransitionChildID(input.ID,
						perSubtaskChildID(source.ID, td.Name, outputIdx),
						perSubtaskChildID(source.ID, tDef.taskSlug, outputIdx),
					) {
						return &PreconditionError{Reason: fmt.Sprintf(
							"task %q shadows pipeline transition child %q[%d]; use %s/%s for transition %q instead of add-tasks",
							input.ID, source.ID, outputIdx, brand.Command("proceed"), brand.Command("resume"), td.Name,
						)}
					}
				}
			case "one-to-one":
				if isTransitionChildID(input.ID,
					oneToOneChildID(source.ID, td.Name),
					oneToOneChildID(source.ID, tDef.taskSlug),
				) {
					return &PreconditionError{Reason: fmt.Sprintf(
						"task %q shadows pipeline transition child %q; use %s/%s for transition %q instead of add-tasks",
						input.ID, source.ID, brand.Command("proceed"), brand.Command("resume"), td.Name,
					)}
				}
			case "many-to-one":
				cohortParentID := source.CohortParentID()
				if cohortParentID == "" {
					continue
				}
				if isTransitionChildID(input.ID,
					manyToOneChildID(cohortParentID, td.Name),
					manyToOneChildID(cohortParentID, tDef.taskSlug),
				) {
					return &PreconditionError{Reason: fmt.Sprintf(
						"task %q shadows pipeline transition child cohort %q; use %s/%s for transition %q instead of add-tasks",
						input.ID, cohortParentID, brand.Command("proceed"), brand.Command("resume"), td.Name,
					)}
				}
			}
		}
	}
	return nil
}

func isTransitionChildID(inputID string, candidates ...string) bool {
	for _, candidate := range candidates {
		if inputID == candidate {
			return true
		}
	}
	return false
}

// AddTasksInput represents the input for batch task creation.
type AddTasksInput struct {
	Tasks          []AddTaskInput
	OrchestratorID string
}

// AddTasksResult contains the outcome of batch task creation.
type AddTasksResult struct {
	Results []AddTaskItemResult `json:"results"`
}

// AddTaskItemResult contains the outcome of adding a single task in a batch.
type AddTaskItemResult struct {
	TaskID   string   `json:"task_id"`
	Success  bool     `json:"success"`
	Error    string   `json:"error"` // empty on success
	Warnings []string `json:"warnings"`
}

// AddTasks adds multiple tasks in a single call. Each task is added
// independently; failed tasks don't block subsequent ones.
func AddTasks(statePath, logPath string, input *AddTasksInput) (*AddTasksResult, error) {
	if len(input.Tasks) == 0 {
		return nil, &PreconditionError{Reason: "at least one task is required"}
	}
	orchestratorID := input.OrchestratorID
	if orchestratorID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	result := &AddTasksResult{Results: make([]AddTaskItemResult, 0, len(input.Tasks))}
	for i := range input.Tasks {
		r, err := AddTask(statePath, logPath, &input.Tasks[i], orchestratorID)
		item := AddTaskItemResult{TaskID: input.Tasks[i].ID}
		if err != nil {
			item.Error = err.Error()
		} else {
			item.Success = true
			item.TaskID = r.TaskID
			item.Warnings = r.Warnings
		}
		result.Results = append(result.Results, item)
	}
	return result, nil
}
