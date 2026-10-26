package agent

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/providers"
	"github.com/liza-mas/liza/internal/roles"
)

const (
	// parentDescriptionLimit truncates a parent task description shown to an architect.
	parentDescriptionLimit = 500
	// priorAttemptNumber is the only attempt that surfaces the previous attempt's outcome.
	priorAttemptNumber = 2
	// nullRejectionReason is the placeholder a cleared rejection reason can
	// carry; it is not feedback.
	nullRejectionReason = "null"
)

// newTaskContextData fills the identity, task, plan-scoping and config values
// a task-based role's context starts from. It reads no repository and fails
// nowhere, so every later stage extends a complete record.
func newTaskContextData(task *models.Task, state *models.State, config SupervisorConfig, roleType roles.RoleType) *prompts.RoleContextData {
	totalPlanTasks, taskOrdinal := collectPlanPosition(state, task.ID)

	return &prompts.RoleContextData{
		// Identity
		Role:                 config.Role,
		AgentID:              config.AgentID,
		RoleType:             string(roleType),
		CLIName:              config.CLIName,
		AwaitIntervalSeconds: int(providers.AwaitInterval(config.CLIName) / time.Second),

		// Task
		TaskID:                 task.ID,
		Description:            task.Description,
		DoneWhen:               task.DoneWhen,
		Scope:                  task.Scope,
		SpecRef:                task.SpecRef,
		EpicRef:                paths.SplitRefFile(task.EpicRef),
		EpicSection:            paths.SplitRefFragment(task.EpicRef),
		EpicSlug:               paths.GoalSlug(paths.SplitRefFile(task.EpicRef)),
		PlanRef:                paths.SplitRefFile(task.PlanRef),
		PlanSection:            paths.SplitRefFragment(task.PlanRef),
		ArchRef:                paths.SplitRefFile(task.ArchRef),
		RCARequired:            task.RCARequired,
		ValidationCommands:     slices.Clone(task.Validation),
		ValidationNotes:        slices.Clone(task.ValidationNotes),
		AcceptanceSource:       task.AcceptanceSource,
		AcceptanceReceipt:      task.AcceptanceReceipt,
		DestructiveDB:          task.DestructiveDB,
		AdmissionMatches:       admissionMatches(state.Config),
		RuntimeInputs:          task.RuntimeInputs,
		ProviderDependencies:   models.CloneProviderDependencies(task.ProviderDependencies),
		DescendantDependencies: models.CloneDescendantDependencies(task.DescendantDependencies),
		TaskDecomposition:      task.Decomposition,
		Worktree:               resolveWorktreePath(config.ProjectRoot, task.Worktree),
		IterationNum:           task.Iteration,
		AttemptNum:             task.EffectiveAttempt(),

		// Plan scoping
		GoalSpecRef:          state.Goal.SpecRef,
		TotalPlanTasks:       totalPlanTasks,
		TaskOrdinal:          taskOrdinal,
		TaskRolePair:         task.RolePair,
		PhaseDependencyTasks: collectPhaseDependencyTasks(state, task),
		TaskGraph:            buildRelevantTaskGraph(state, task),

		// Config/state
		ProjectRoot:       config.ProjectRoot,
		StatePath:         config.StatePath,
		SpecsDir:          config.SpecsDir,
		GoalDesc:          state.Goal.Description,
		GoalSlug:          paths.GoalSlug(state.Goal.SpecRef),
		IntegrationBranch: state.Config.IntegrationBranch,
		TestFilePatterns:  SubmissionTestPatternsText(),
	}
}

// populateRepositoryIntelligence discovers the index artifacts of the task
// worktree and the corrective context state already holds for the task.
func populateRepositoryIntelligence(data *prompts.RoleContextData, task *models.Task, state *models.State) {
	data.ScipIndexes = availablePromptScipIndexRefs(state, data.Worktree, scipAvailableIndexes)
	data.StacklitIndexes = availablePromptStacklitIndexRefs(data.Worktree)
	data.FunctionalClusters = availablePromptFunctionalClusterIndexRefs(data.Worktree)
	data.CorrectiveContext = buildTaskCorrectiveContext(task, state)
}

// populatePriorRejection carries the rejection text a reworked task answers.
func populatePriorRejection(data *prompts.RoleContextData, task *models.Task) {
	if task.Iteration > 1 && task.RejectionReason != nil && *task.RejectionReason != "" && *task.RejectionReason != nullRejectionReason {
		data.PriorRejection = *task.RejectionReason
	}
}

// populatePriorAttempt carries the outcome of the previous attempt, which only
// the second attempt shows.
func populatePriorAttempt(data *prompts.RoleContextData, task *models.Task) {
	if data.AttemptNum != priorAttemptNumber {
		return
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventNewAttempt && task.History[i].Reason != nil {
			data.PriorAttemptOutcome = *task.History[i].Reason
			if task.History[i].Note != nil {
				data.PriorAttemptRejection = *task.History[i].Note
			}
			return
		}
	}
}

// populateCoderRecovery carries the integration-fix flag and the last
// context-exhaustion hand-off a coder resumes from.
func populateCoderRecovery(data *prompts.RoleContextData, task *models.Task, role string, roleType roles.RoleType) {
	if roleType != roles.RoleTypeDoer || role != roles.Coder {
		return
	}
	data.IntegrationFix = task.IntegrationFix
	for i := len(task.HandoffEvents) - 1; i >= 0; i-- {
		if task.HandoffEvents[i].Trigger == models.HandoffTriggerContextExhaustion {
			evt := task.HandoffEvents[i]
			data.HandoffNote = &evt
			return
		}
	}
}

// populateValidationPrerequisites sets the declaration flags planners and plan
// reviewers render, including the direct coding allocation an architecture
// task may offer.
func populateValidationPrerequisites(data *prompts.RoleContextData, task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) {
	if config.Role == models.RoleCodePlanner || config.Role == models.RoleCodePlanReviewer {
		data.DeclareValidationPrerequisites = consumersValidateLocally(task.RolePair, state.Config, config.ProjectRoot, resolver)
		data.DeclareRuntimeInputs = state.Config.RuntimeInputRegistry != ""
	}
	if task.EffectiveType() != models.TaskTypeArchitecture {
		return
	}
	directOutput := []models.OutputEntry{{CodingAllocation: true}}
	consumers, err := resolver.OutputConsumerRolePairsForOutput(task.RolePair, directOutput)
	data.DirectCodingAllocationAvailable = err == nil && len(consumers) > 0
	if data.DirectCodingAllocationAvailable {
		data.DeclareValidationPrerequisites = consumersValidateLocally(task.RolePair, state.Config, config.ProjectRoot, resolver, directOutput)
		data.DeclareRuntimeInputs = state.Config.RuntimeInputRegistry != ""
	}
}

// populateReviewerFields carries the review boundary and the doer's declared
// scope extensions and validation plan.
func populateReviewerFields(data *prompts.RoleContextData, task *models.Task, roleType roles.RoleType) {
	if roleType != roles.RoleTypeReviewer {
		return
	}
	data.BaseCommit = derefString(task.BaseCommit)
	data.ReviewCommit = derefString(task.ReviewCommit)
	data.AssignedTo = derefString(task.AssignedTo)
	if task.AssignedTo != nil {
		data.ScopeExtensions = ops.GetLatestScopeExtensions(task.History, *task.AssignedTo)
		data.ValidationPlan = ops.GetValidationPlan(task.History, *task.AssignedTo)
	}
}

// populateDeclarativeFields reads the role's skills and mandatory documents
// from the pipeline; a role without them keeps the empty value.
func populateDeclarativeFields(data *prompts.RoleContextData, role string, resolver *pipeline.Resolver) {
	if skills, err := resolver.Skills(role); err == nil {
		data.Skills = skills
	}
	if mandatoryDocs, err := resolver.MandatoryDocs(role); err == nil {
		data.MandatoryDocs = mandatoryDocs
	}
}

// populateArchitectContext carries the parent task summaries and the
// pre-commit bootstrap state an architect plans against.
func populateArchitectContext(data *prompts.RoleContextData, task *models.Task, state *models.State, config SupervisorConfig) error {
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if parent != nil {
			data.ParentTaskContexts = append(data.ParentTaskContexts, prompts.ParentTaskContext{
				ID:          parent.ID,
				Description: prompts.TruncateText(parent.Description, parentDescriptionLimit),
				DoneWhen:    parent.DoneWhen,
				SpecRef:     parent.SpecRef,
				EpicRef:     parent.EpicRef,
				PlanRef:     parent.PlanRef,
			})
		}
	}

	exists, err := precommit.ConfigExistsOnIntegration(config.ProjectRoot, state.Config.IntegrationBranch)
	if err != nil {
		return fmt.Errorf("precommit config check: %w", err)
	}
	data.PreCommitConfigExists = exists
	data.PreCommitBootstrapInFlight = precommit.BootstrapInFlight(state)
	data.PreCommitKind = precommit.Kind
	return nil
}

// populateReferenceProvenance resolves the task's reference context at one
// integration HEAD and records either the strict resolved context or the
// legacy references it could not resolve strictly.
func populateReferenceProvenance(data *prompts.RoleContextData, task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver, roleType roles.RoleType) error {
	decompositionRoot := false
	if task.RolePair != "" {
		var err error
		decompositionRoot, err = resolver.IsDecompositionRoot(task.RolePair)
		if err != nil {
			return fmt.Errorf("reference context topology for task %q: %w", task.ID, err)
		}
	}
	resolvedContext, legacyReferences, err := buildReferenceContext(task, state, config, string(roleType), decompositionRoot)
	if err != nil {
		return fmt.Errorf("reference context for task %q: %w: %w", task.ID, precommit.ErrContextBuild, err)
	}
	data.ResolvedReferenceContext = resolvedContext
	data.LegacyArtifactReferences = legacyReferences
	return nil
}
