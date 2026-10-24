package ops

import (
	"fmt"
	"io"
	"os"
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
	PlanningChange          *models.PlanningChange          `json:"planning_change,omitempty"`
	ID                      string                          `json:"id"`
	Type                    string                          `json:"type,omitempty"`
	RolePair                string                          `json:"role_pair,omitempty"`
	Description             string                          `json:"desc"`
	SpecRef                 string                          `json:"spec"`
	PlanRef                 string                          `json:"plan_ref,omitempty"`
	DoneWhen                string                          `json:"done"`
	Validation              []string                        `json:"validation,omitempty"`
	ValidationPrerequisites []models.ValidationPrerequisite `json:"validation_prerequisites,omitempty"`
	RuntimeInputs           []models.RuntimeInput           `json:"runtime_inputs,omitempty"`
	DestructiveDB           bool                            `json:"destructive_db,omitempty"`
	Scope                   string                          `json:"scope"`
	Priority                int                             `json:"priority"`
	RCARequired             bool                            `json:"rca_required,omitempty"`
	DependsOn               []string                        `json:"depends,omitempty"`
	ProviderDependencies    []models.ProviderDependency     `json:"provider_dependencies,omitempty"`
	DescendantDependencies  []models.DescendantDependency   `json:"descendant_dependencies,omitempty"`
	// ReserveSuccessors are existing unstarted writers placed after this task
	// in the same transaction that creates it (D-80): each waits for every
	// child this task generates. add-task and add-tasks only.
	ReserveSuccessors []string `json:"reserve_successors,omitempty"`
	// MaxOutputs caps this task's output entries (D-80); 0 is unbounded.
	MaxOutputs int `json:"max_outputs,omitempty"`
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

	// Derive the project root from the runtime state path.
	projectRoot := filepath.Dir(filepath.Dir(statePath))
	newTask, resolver, err := prepareAddTask(statePath, projectRoot, input)
	if err != nil {
		return nil, err
	}

	bb := db.For(statePath)
	var postValidationErr error
	err = lifecycleMutation(bb, authority)(func(state *models.State) error {
		// add-task cannot carry the delivery task a dead-end stage needs (D-33).
		if req := deliveryRequirementFor(resolver, newTask.RolePair, state.Config.NoFollowUp); req != nil {
			return req.refusal(newTask.ID, newTask.RolePair)
		}
		if err := insertTaskInState(state, projectRoot, newTask, input, resolver); err != nil {
			return err
		}
		if err := reserveSuccessorsInState(state, resolver, &newTask, input.ReserveSuccessors, orchestratorID); err != nil {
			return err
		}
		if models.HasProviderDependencies(state) {
			if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, true, os.Stderr); err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("add-task provider dependencies: %v", err)}
			}
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			postValidationErr = err
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to add task: %w", err)
	}

	return addedTaskResult(logPath, orchestratorID, newTask, input, postValidationErr), nil
}

// prepareAddTask runs the creation checks that precede the state lock:
// structural preflight, pipeline resolution, task construction and the
// acceptance and runtime-input admission checks.
func prepareAddTask(statePath, projectRoot string, input *AddTaskInput) (models.Task, *pipeline.Resolver, error) {
	// Structural preflight precedes pipeline reads and the state lock.
	_, diagnostics, err := payloadschema.Validate("add-task", input)
	if err != nil {
		return models.Task{}, nil, err
	}
	if len(diagnostics) > 0 {
		return models.Task{}, nil, NewLifecycleInvalidInputError("add-task", nil, diagnostics, &PreconditionError{Reason: diagnostics[0].Constraint})
	}

	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return models.Task{}, nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	newTask, err := buildReplacementTask(input, resolver)
	if err != nil {
		return models.Task{}, nil, err
	}

	if diagnostics := runtimeInputsNeedStrictCarrier(&newTask); diagnostics != nil {
		return models.Task{}, nil, runtimeInputAdmissionError("add-task", nil, diagnostics, nil)
	}
	// An ad-hoc task has no parent, so a snapshot supplies only configuration;
	// the Git reads stay outside the state lock, as at claim.
	if acceptanceCreationApplies(&newTask) || len(newTask.ValidationPrerequisites) > 0 {
		snapshot, err := db.For(statePath).ReadSnapshot()
		if err != nil {
			return models.Task{}, nil, fmt.Errorf("failed to read state for the acceptance check: %w", err)
		}
		if acceptanceCreationApplies(&newTask) {
			if err := checkCreatedTaskAcceptance(projectRoot, snapshot, &newTask).lifecycleError("add-task", nil); err != nil {
				return models.Task{}, nil, err
			}
		}
		diagnostics, err := checkRuntimeInputDeclarations(projectRoot, snapshot, []string{newTask.ID}, []runtimeInputCarrier{{inputs: newTask.RuntimeInputs, prerequisites: newTask.ValidationPrerequisites}})
		if err := runtimeInputAdmissionError("add-task", nil, diagnostics, err); err != nil {
			return models.Task{}, nil, err
		}
	}
	return newTask, resolver, nil
}

// addedTaskResult reports a committed task and appends its activity-log entry.
func addedTaskResult(logPath, orchestratorID string, task models.Task, input *AddTaskInput, postValidationErr error) *AddTaskResult {
	result := &AddTaskResult{TaskID: input.ID}
	if postValidationErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("state remains degraded after add-task; full validation failed: %v", postValidationErr))
	}

	logger := log.New(logPath)
	logEntry := log.Entry{
		Timestamp: task.Created,
		Agent:     orchestratorID,
		Action:    "task_added",
		Task:      &input.ID,
		Detail:    input.Description,
	}

	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return result
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
	if err := models.ValidateRuntimeInputs(input.Validation, input.RuntimeInputs); err != nil {
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
	if input.MaxOutputs < 0 {
		return &PreconditionError{Reason: fmt.Sprintf("max_outputs must be nonnegative (0 is unbounded), got %d", input.MaxOutputs)}
	}
	for i, successor := range input.ReserveSuccessors {
		if err := paths.ValidateTaskID(successor); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("reserve_successors[%d]: %v", i, err)}
		}
		if successor == input.ID || slices.Contains(input.ReserveSuccessors[:i], successor) {
			return &PreconditionError{Reason: "reserve_successors must name distinct existing tasks other than the new task"}
		}
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
		PlanningChange:          input.PlanningChange,
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
		RuntimeInputs:           models.CloneRuntimeInputs(input.RuntimeInputs),
		DestructiveDB:           input.DestructiveDB,
		RCARequired:             input.RCARequired,
		Scope:                   input.Scope,
		DependsOn:               normalizedDeps,
		ProviderDependencies:    models.CloneProviderDependencies(input.ProviderDependencies),
		DescendantDependencies:  models.CloneDescendantDependencies(input.DescendantDependencies),
		MaxOutputs:              input.MaxOutputs,
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
	if task.PlanningChange != nil {
		original := state.FindTask(task.PlanningChange.OriginalTaskID)
		if original == nil || !IsPlanningPair(original.RolePair, resolver.TransitionSourcePairs()) || !IsPlanningPair(task.RolePair, resolver.TransitionSourcePairs()) {
			return &PreconditionError{Reason: "planning_change requires an existing original plan and a planning correction task"}
		}
		for _, carrier := range []*models.Task{original, &task} {
			switch carrier.EffectiveType() {
			case models.TaskTypePlanning, models.TaskTypeArchitecture, models.TaskTypeEpicPlanning, models.TaskTypeUSWriting:
			default:
				return &PreconditionError{Reason: "planning_change is reserved for corrective planning work"}
			}
		}
	}
	state.Tasks = append(state.Tasks, task)

	if !slices.Contains(state.Sprint.Scope.Planned, task.ID) {
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}

	// Keep the description on the task and in the activity log, not in the
	// size-limited alignment summary.
	alignmentEntry := models.AlignmentHistory{
		Timestamp: task.Created,
		Event:     string(models.TaskEventPlanning),
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
	// Items commit one by one, so a placed writer must be commissioned alone:
	// its creation and its successors' reservations share one transaction.
	if len(input.Tasks) > 1 {
		for _, item := range input.Tasks {
			if len(item.ReserveSuccessors) > 0 || item.MaxOutputs != 0 {
				return nil, &PreconditionError{Reason: fmt.Sprintf("task %s carries reserve_successors or max_outputs; commission placed writers one per add-tasks request", item.ID)}
			}
		}
	}
	units := planDeliveryUnits(statePath, input.Tasks)
	if units == nil {
		result := &AddTasksResult{Results: make([]AddTaskItemResult, 0, len(input.Tasks))}
		for i := range input.Tasks {
			item, err := addOneOfTasks(statePath, logPath, &input.Tasks[i], orchestratorID, authority)
			if err != nil {
				return nil, err
			}
			result.Results = append(result.Results, item)
		}
		return result, nil
	}

	// Results stay positional whatever order the units commit in.
	results := make([]AddTaskItemResult, len(input.Tasks))
	for _, unit := range units {
		if len(unit) == 1 {
			item, err := addOneOfTasks(statePath, logPath, &input.Tasks[unit[0]], orchestratorID, authority)
			if err != nil {
				return nil, err
			}
			results[unit[0]] = item
			continue
		}
		items, err := addDeliveryGroup(statePath, logPath, input.Tasks, unit, orchestratorID, authority)
		if err != nil {
			return nil, err
		}
		for k, idx := range unit {
			results[idx] = items[k]
		}
	}
	return &AddTasksResult{Results: results}, nil
}

// addOneOfTasks adds one batch item on its own. Only an authority loss is
// returned as an error: it stops the batch.
func addOneOfTasks(statePath, logPath string, input *AddTaskInput, orchestratorID string, authority *models.AgentAuthority) (AddTaskItemResult, error) {
	var r *AddTaskResult
	var err error
	if authority == nil {
		r, err = AddTask(statePath, logPath, input, orchestratorID)
	} else {
		r, err = AddTaskWithAuthority(statePath, logPath, input, *authority)
		if IsAgentAuthorityError(err) {
			return AddTaskItemResult{}, err
		}
	}
	item := AddTaskItemResult{TaskID: input.ID}
	if err != nil {
		item.Error = err.Error()
	} else {
		item.Success = true
		item.TaskID = r.TaskID
		item.Warnings = r.Warnings
	}
	return item, nil
}

// deliveryRequirement describes a dead-end stage (D-33): every transition out
// of the role-pair is many-to-one, keyed on a cohort parent that an added task
// never has, so the task can move on only through a delivery task that depends
// on it and was commissioned with it.
type deliveryRequirement struct {
	transitions []string
	targets     []string
}

// deliveryRequirementFor returns the requirement for an added task of rolePair,
// or nil when the stage can move on alone: it has a per-subtask or one-to-one
// transition, or none at all (a terminal stage, or one whose pipeline
// transitions config.no_follow_up suppresses).
func deliveryRequirementFor(resolver *pipeline.Resolver, rolePair string, noFollowUp bool) *deliveryRequirement {
	req := &deliveryRequirement{}
	for _, td := range resolver.AllTransitions() {
		if noFollowUp && resolver.IsPipelineTransition(td.Name) {
			continue
		}
		if source, err := resolver.TransitionSourceRolePair(td.Name); err != nil || source != rolePair {
			continue
		}
		if td.Cardinality != "many-to-one" {
			return nil
		}
		req.transitions = append(req.transitions, td.Name)
		if target, err := resolver.TransitionTargetRolePair(td.Name); err == nil && !slices.Contains(req.targets, target) {
			req.targets = append(req.targets, target)
		}
	}
	if len(req.transitions) == 0 {
		return nil
	}
	return req
}

func (r *deliveryRequirement) refusal(taskID, rolePair string) error {
	return &PreconditionError{Reason: fmt.Sprintf(
		"task %s (role_pair %s) cannot be delivered on its own: its only onward transition (%s) is many-to-one and needs a cohort parent that an added task does not have; commission it with %s in one request together with a %s task that depends on it",
		taskID, rolePair, strings.Join(r.transitions, ", "), brand.Command("add-tasks"), strings.Join(r.targets, " or "),
	)}
}

// deliveredBy reports whether candidate delivers source: it belongs to a
// delivery target role-pair and depends on source.
func (r *deliveryRequirement) deliveredBy(source string, candidate *AddTaskInput) bool {
	if !slices.Contains(r.targets, candidate.RolePair) {
		return false
	}
	return slices.ContainsFunc(candidate.DependsOn, func(dep string) bool { return strings.TrimSpace(dep) == source })
}

// planDeliveryUnits returns the commit order of a batch holding a dead-end
// task, or nil when it holds none and the plain item loop applies unchanged.
// Each unit lists request indices: a single item commits alone; several commit
// in one transaction. A dead-end task and its delivery companions form a
// group; units whose in-request dependencies form a cycle through a group
// merge into one unit, so grouping never orders a member before a
// prerequisite. Units then commit in a stable dependency order: the ready unit
// with the lowest request index first.
func planDeliveryUnits(statePath string, tasks []AddTaskInput) [][]int {
	resolver, _, err := loadResolver(filepath.Dir(filepath.Dir(statePath)))
	if err != nil {
		return nil
	}
	snapshot, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return nil
	}
	noFollowUp := snapshot.Config.NoFollowUp

	unit := make([]int, len(tasks)) // union-find parent over request indices
	for i := range unit {
		unit[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if unit[i] != i {
			unit[i] = find(unit[i])
		}
		return unit[i]
	}
	union := func(a, b int) { unit[find(a)] = find(b) }

	grouped := map[int]bool{}
	for s := range tasks {
		req := deliveryRequirementFor(resolver, tasks[s].RolePair, noFollowUp)
		if req == nil {
			continue
		}
		for c := range tasks {
			if c != s && req.deliveredBy(tasks[s].ID, &tasks[c]) {
				union(s, c)
				grouped[s], grouped[c] = true, true
			}
		}
	}
	if len(grouped) == 0 {
		// A dead-end task without a companion is refused by its own add-task.
		return nil
	}

	index := map[string]int{}
	for i, task := range tasks {
		if _, seen := index[task.ID]; !seen {
			index[task.ID] = i
		}
	}
	dependsOn := func(i int) []int {
		var prereqs []int
		for _, dep := range tasks[i].DependsOn {
			if j, ok := index[strings.TrimSpace(dep)]; ok && j != i {
				prereqs = append(prereqs, j)
			}
		}
		return prereqs
	}

	// Merge every cycle of units that runs through a group.
	edges := make([][]int, len(tasks))
	for i := range tasks {
		for _, j := range dependsOn(i) {
			if find(i) != find(j) {
				edges[find(i)] = append(edges[find(i)], find(j))
			}
		}
	}
	var roots []int
	groupRoot := map[int]bool{}
	for i := range tasks {
		if find(i) == i {
			roots = append(roots, i)
		}
		if grouped[i] {
			groupRoot[find(i)] = true
		}
	}
	for _, scc := range findSCCs(roots, edges) {
		if len(scc) > 1 && slices.ContainsFunc(scc, func(r int) bool { return groupRoot[r] }) {
			for _, r := range scc[1:] {
				union(r, scc[0])
			}
		}
	}

	members := map[int][]int{}
	for i := range tasks {
		members[find(i)] = append(members[find(i)], i)
	}
	var order [][]int
	for _, unitIdx := range stableDependencyOrder(len(tasks), func(i int) bool { return find(i) == i }, func(r int) []int {
		var prereqs []int
		for _, m := range members[r] {
			for _, j := range dependsOn(m) {
				if find(j) != r {
					prereqs = append(prereqs, find(j))
				}
			}
		}
		return prereqs
	}, func(r int) int { return members[r][0] }) {
		ms := members[unitIdx]
		if len(ms) > 1 {
			// Inside a unit, members insert after their in-unit prerequisites.
			inUnit := map[int]bool{}
			for _, m := range ms {
				inUnit[m] = true
			}
			ms = stableDependencyOrder(len(tasks), func(i int) bool { return inUnit[i] }, func(m int) []int {
				var prereqs []int
				for _, j := range dependsOn(m) {
					if inUnit[j] {
						prereqs = append(prereqs, j)
					}
				}
				return prereqs
			}, func(m int) int { return m })
		}
		order = append(order, ms)
	}
	return order
}

// stableDependencyOrder orders the nodes in [0,n) that include accepts:
// repeatedly the ready node (every prerequisite already placed) with the
// lowest anchor, or, when a cycle leaves none ready, the lowest anchor.
func stableDependencyOrder(n int, include func(int) bool, prereqs func(int) []int, anchor func(int) int) []int {
	var pending, order []int
	for i := 0; i < n; i++ {
		if include(i) {
			pending = append(pending, i)
		}
	}
	slices.SortStableFunc(pending, func(a, b int) int { return anchor(a) - anchor(b) })
	placed := map[int]bool{}
	for len(pending) > 0 {
		pick := 0
		for k, node := range pending {
			if !slices.ContainsFunc(prereqs(node), func(p int) bool { return !placed[p] }) {
				pick = k
				break
			}
		}
		placed[pending[pick]] = true
		order = append(order, pending[pick])
		pending = slices.Delete(pending, pick, pick+1)
	}
	return order
}

// addDeliveryGroup commits the batch items at indices in one transaction
// (D-33): every member persists, or none does and each reports the group
// error. Only an authority loss is returned as an error.
func addDeliveryGroup(statePath, logPath string, tasks []AddTaskInput, indices []int, orchestratorID string, authority *models.AgentAuthority) ([]AddTaskItemResult, error) {
	ids := make([]string, len(indices))
	for k, idx := range indices {
		ids[k] = tasks[idx].ID
	}
	fail := func(memberID string, err error) []AddTaskItemResult {
		results := make([]AddTaskItemResult, len(indices))
		for k := range indices {
			results[k] = AddTaskItemResult{TaskID: ids[k], Error: fmt.Sprintf("delivery group [%s]: %s: %v", strings.Join(ids, ", "), memberID, err)}
		}
		return results
	}

	projectRoot := filepath.Dir(filepath.Dir(statePath))
	built := make([]models.Task, len(indices))
	var resolver *pipeline.Resolver
	for k, idx := range indices {
		task, r, err := prepareAddTask(statePath, projectRoot, &tasks[idx])
		if err != nil {
			return fail(ids[k], err), nil
		}
		built[k], resolver = task, r
	}

	bb := db.For(statePath)
	var postValidationErr error
	var failedMember string
	err := lifecycleMutation(bb, authority)(func(state *models.State) error {
		for k, idx := range indices {
			req := deliveryRequirementFor(resolver, built[k].RolePair, state.Config.NoFollowUp)
			if req != nil && !slices.ContainsFunc(indices, func(c int) bool { return c != idx && req.deliveredBy(ids[k], &tasks[c]) }) {
				failedMember = ids[k]
				return req.refusal(ids[k], built[k].RolePair)
			}
		}
		for k, idx := range indices {
			failedMember = ids[k]
			if err := insertTaskInState(state, projectRoot, built[k], &tasks[idx], resolver); err != nil {
				return err
			}
			if err := reserveSuccessorsInState(state, resolver, &built[k], tasks[idx].ReserveSuccessors, orchestratorID); err != nil {
				return err
			}
		}
		failedMember = ""
		if models.HasProviderDependencies(state) {
			if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, true, os.Stderr); err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("add-task provider dependencies: %v", err)}
			}
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			postValidationErr = err
		}
		return nil
	})
	if err != nil {
		if authority != nil && IsAgentAuthorityError(err) {
			return nil, err
		}
		if failedMember == "" {
			failedMember = strings.Join(ids, ", ")
		}
		return fail(failedMember, fmt.Errorf("failed to add task: %w", err)), nil
	}

	results := make([]AddTaskItemResult, len(indices))
	for k, idx := range indices {
		r := addedTaskResult(logPath, orchestratorID, built[k], &tasks[idx], postValidationErr)
		results[k] = AddTaskItemResult{TaskID: r.TaskID, Success: true, Warnings: r.Warnings}
	}
	return results, nil
}
