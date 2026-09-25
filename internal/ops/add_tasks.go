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
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// AddTaskInput represents the input parameters for adding a task.
type AddTaskInput struct {
	ID                      string                          `json:"id"`
	Type                    string                          `json:"type,omitempty"`
	RolePair                string                          `json:"role_pair,omitempty"`
	Description             string                          `json:"desc"`
	SpecRef                 string                          `json:"spec"`
	PlanRef                 string                          `json:"plan_ref,omitempty"`
	DoneWhen                string                          `json:"done"`
	Validation              []string                        `json:"validation,omitempty"`
	ValidationPrerequisites []models.ValidationPrerequisite `json:"validation_prerequisites,omitempty"`
	DestructiveDB           bool                            `json:"destructive_db,omitempty"`
	Scope                   string                          `json:"scope"`
	Priority                int                             `json:"priority"`
	RCARequired             bool                            `json:"rca_required,omitempty"`
	DependsOn               []string                        `json:"depends,omitempty"`
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
	return addTaskWithOptionalAuthority(statePath, logPath, input, orchestratorID, nil)
}

// AddTaskWithAuthority fences task creation with the orchestrator's current
// registration generation.
func AddTaskWithAuthority(statePath, logPath string, input *AddTaskInput, authority models.AgentAuthority) (*AddTaskResult, error) {
	return addTaskWithOptionalAuthority(statePath, logPath, input, authority.ID, &authority)
}

func addTaskWithOptionalAuthority(statePath, logPath string, input *AddTaskInput, orchestratorID string, authority *models.AgentAuthority) (*AddTaskResult, error) {
	if orchestratorID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	// Structural preflight precedes pipeline reads and the state lock.
	_, diagnostics, err := payloadschema.Validate("add-task", input)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		return nil, NewLifecycleInvalidInputError("add-task", nil, diagnostics, &PreconditionError{Reason: diagnostics[0].Constraint})
	}

	// Derive the project root from the runtime state path.
	projectRoot := filepath.Dir(filepath.Dir(statePath))
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	newTask, err := buildReplacementTask(input, resolver)
	if err != nil {
		return nil, err
	}

	bb := db.For(statePath)

	// An ad-hoc task has no parent, so a snapshot supplies only configuration;
	// the Git reads stay outside the state lock, as at claim.
	if acceptanceCreationApplies(&newTask) {
		snapshot, err := bb.ReadSnapshot()
		if err != nil {
			return nil, fmt.Errorf("failed to read state for the acceptance check: %w", err)
		}
		if err := checkCreatedTaskAcceptance(projectRoot, snapshot, &newTask).lifecycleError("add-task", nil); err != nil {
			return nil, err
		}
	}

	var postValidationErr error
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		if err := insertTaskInState(state, projectRoot, newTask, input, resolver); err != nil {
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
		Timestamp: newTask.Created,
		Agent:     orchestratorID,
		Action:    "task_added",
		Task:      &input.ID,
		Detail:    input.Description,
	}

	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return result, nil
}

// validateAddTaskInput checks the creation fields that need no pipeline config.
func validateAddTaskInput(input *AddTaskInput) error {
	if err := paths.ValidateTaskID(input.ID); err != nil {
		return fmt.Errorf("invalid task ID: %w", err)
	}
	if input.Description == "" {
		return &PreconditionError{Reason: "description is required"}
	}
	if input.SpecRef == "" {
		return &PreconditionError{Reason: "spec_ref is required"}
	}
	if err := statevalidate.ValidateArtifactRefScalar("spec_ref", input.SpecRef, input.ID); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if err := statevalidate.ValidateArtifactRefScalar("plan_ref", input.PlanRef, input.ID); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if input.DoneWhen == "" {
		return &PreconditionError{Reason: "done_when is required"}
	}
	if err := models.ValidateValidationSafety("validation", input.Validation, input.DestructiveDB); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if err := models.ValidateValidationPrerequisites(input.Validation, input.ValidationPrerequisites); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if input.Scope == "" {
		return &PreconditionError{Reason: "scope is required"}
	}
	if input.Priority < 1 {
		return &PreconditionError{Reason: fmt.Sprintf("priority must be positive, got %d", input.Priority)}
	}
	if input.Type != "" && !models.TaskType(input.Type).IsValid() {
		return &PreconditionError{Reason: fmt.Sprintf("unknown task type %q; valid types: %s",
			input.Type, strings.Join(models.ValidTaskTypeNames(), ", "))}
	}
	return nil
}

// buildReplacementTask validates creation input and builds the task value that
// insertTaskInState commits. It touches no state and acquires no lock, so a
// composing transaction can construct its replacement before taking the state
// lock. It normalizes input.Type when the caller left it empty.
func buildReplacementTask(input *AddTaskInput, resolver *pipeline.Resolver) (models.Task, error) {
	if err := validateAddTaskInput(input); err != nil {
		return models.Task{}, err
	}

	var taskType models.TaskType
	if input.Type != "" {
		taskType = models.TaskType(input.Type)
	}

	if input.RolePair == "" {
		return models.Task{}, &PreconditionError{
			Reason: fmt.Sprintf("role_pair is required; available: %s",
				strings.Join(resolver.RolePairNames(), ", ")),
		}
	}
	rp, rpErr := resolver.RolePair(input.RolePair)
	if rpErr != nil {
		return models.Task{}, &PreconditionError{
			Reason: fmt.Sprintf("unknown role_pair %q; available role_pairs: %s",
				input.RolePair, strings.Join(resolver.RolePairNames(), ", ")),
		}
	}

	expectedTaskType := models.TaskTypeForRole(rp.Doer)
	if input.Type == "" {
		taskType = expectedTaskType
		input.Type = string(taskType)
	} else if taskType != expectedTaskType {
		return models.Task{}, &PreconditionError{Reason: fmt.Sprintf("task type %q conflicts with role_pair %q (expected %q)",
			input.Type, input.RolePair, expectedTaskType)}
	}

	normalizedDeps := []string{}
	for _, dep := range input.DependsOn {
		trimmed := strings.TrimSpace(dep)
		if trimmed != "" {
			normalizedDeps = append(normalizedDeps, trimmed)
		}
	}

	initialStatus, err := resolver.InitialStatus(input.RolePair)
	if err != nil {
		return models.Task{}, fmt.Errorf("failed to resolve initial status for role-pair %q: %w", input.RolePair, err)
	}

	return models.Task{
		ID:                      input.ID,
		Type:                    taskType,
		RolePair:                input.RolePair,
		Description:             input.Description,
		Status:                  initialStatus,
		Priority:                input.Priority,
		SpecRef:                 paths.NormalizeSpecRef(input.SpecRef),
		PlanRef:                 paths.NormalizeSpecRef(input.PlanRef),
		DoneWhen:                input.DoneWhen,
		Validation:              slices.Clone(input.Validation),
		ValidationPrerequisites: models.CloneValidationPrerequisites(input.ValidationPrerequisites),
		DestructiveDB:           input.DestructiveDB,
		RCARequired:             input.RCARequired,
		Scope:                   input.Scope,
		DependsOn:               normalizedDeps,
		Created:                 time.Now().UTC(),
		History:                 []models.TaskHistoryEntry{},
	}, nil
}

// insertTaskInState commits one built task into an already-locked candidate
// state: duplicate-ID and pipeline-child rejection, task append, sprint scope,
// the goal alignment entry and added-task validation. It leaves full-state
// validation to the caller, whose posture differs per operation.
func insertTaskInState(state *models.State, projectRoot string, task models.Task, input *AddTaskInput, resolver *pipeline.Resolver) error {
	if state.FindTask(task.ID) != nil {
		return &PreconditionError{Reason: fmt.Sprintf("task '%s' already exists", task.ID)}
	}
	if err := rejectManualPipelineChildTask(state, input, resolver); err != nil {
		return err
	}
	state.Tasks = append(state.Tasks, task)

	if !slices.Contains(state.Sprint.Scope.Planned, task.ID) {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	// Keep the description on the task and in the activity log, not in the
	// size-limited alignment summary.
	alignmentEntry := models.AlignmentHistory{
		Timestamp: task.Created,
		Event:     models.TaskEventPlanning,
		Summary:   fmt.Sprintf("Added task %s", task.ID),
	}
	state.Goal.AlignmentHistory = append(state.Goal.AlignmentHistory, alignmentEntry)

	return statevalidate.ValidateAddedTask(state, projectRoot, task.ID, false, io.Discard)
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
	return addTasksWithOptionalAuthority(statePath, logPath, input, nil)
}

// AddTasksWithAuthority fences every independent item write with the same
// current orchestrator generation and stops the batch on authority loss.
func AddTasksWithAuthority(statePath, logPath string, input *AddTasksInput, authority models.AgentAuthority) (*AddTasksResult, error) {
	return addTasksWithOptionalAuthority(statePath, logPath, input, &authority)
}

func addTasksWithOptionalAuthority(statePath, logPath string, input *AddTasksInput, authority *models.AgentAuthority) (*AddTasksResult, error) {
	if len(input.Tasks) == 0 {
		return nil, &PreconditionError{Reason: "at least one task is required"}
	}
	orchestratorID := input.OrchestratorID
	if authority != nil {
		orchestratorID = authority.ID
	}
	if orchestratorID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	result := &AddTasksResult{Results: make([]AddTaskItemResult, 0, len(input.Tasks))}
	for i := range input.Tasks {
		var r *AddTaskResult
		var err error
		if authority == nil {
			r, err = AddTask(statePath, logPath, &input.Tasks[i], orchestratorID)
		} else {
			r, err = AddTaskWithAuthority(statePath, logPath, &input.Tasks[i], *authority)
			if IsAgentAuthorityError(err) {
				return nil, err
			}
		}
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
