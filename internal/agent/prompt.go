package agent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/functionalclusters"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/semble"
	"github.com/liza-mas/liza/internal/stacklit"
)

var (
	buildSemblePromptMetadata          = semble.BuildPromptMetadata
	scipAvailableIndexes               = scipsearch.AvailableIndexes
	stacklitAvailableIndexes           = stacklit.AvailableIndexes
	functionalClustersAvailableIndexes = functionalclusters.AvailableIndexes
)

// baseConfigFrom constructs the BasePromptConfig shared by all roles.
func baseConfigFrom(state *models.State, config SupervisorConfig, taskID string, scipIndexes []prompts.ScipSearchIndex, stacklitIndexes []prompts.StacklitIndex, functionalClusterIndexes []prompts.FunctionalClusterIndex, sembleSearch prompts.SembleSearchMetadata) prompts.BasePromptConfig {
	return prompts.BasePromptConfig{
		Role:               config.Role,
		AgentID:            config.AgentID,
		TaskID:             taskID,
		SpecsDir:           config.SpecsDir,
		ProjectRoot:        config.ProjectRoot,
		StatePath:          config.StatePath,
		GoalDesc:           state.Goal.Description,
		GoalSpecRef:        state.Goal.SpecRef,
		ScipSearchIndexes:  scipIndexes,
		StacklitIndexes:    stacklitIndexes,
		FunctionalClusters: functionalClusterIndexes,
		SembleSearch:       sembleSearch,
	}
}

// buildPromptWithContext builds a complete prompt for any task-based role:
// base prompt + task lookup + role-specific context via BuildRoleContext + InitialTask suffix.
func buildPromptWithContext(state *models.State, config SupervisorConfig, taskID string, resolver *pipeline.Resolver) (string, error) {
	task := state.FindTask(taskID)
	if task == nil {
		return "", &errors.NotFoundError{Entity: "task", ID: taskID}
	}

	data, err := buildTaskRoleContextData(task, state, config, resolver)
	if err != nil {
		return "", err
	}

	prompt, err := prompts.BuildBasePrompt(baseConfigFrom(
		state,
		config,
		taskID,
		toBasePromptScipSearchIndexes(data.ScipIndexes),
		toBasePromptStacklitIndexes(data.StacklitIndexes),
		toBasePromptFunctionalClusterIndexes(data.FunctionalClusters),
		availablePromptSembleSearchMetadata(data.Worktree, semble.TargetKindTaskWorktree),
	))
	if err != nil {
		return "", fmt.Errorf("building base prompt: %w", err)
	}

	sections, err := resolver.ContextSections(config.Role)
	if err != nil {
		return "", fmt.Errorf("context sections for role %q: %w", config.Role, err)
	}
	sections, err = taskContextSections(sections, task, data, resolver)
	if err != nil {
		return "", err
	}

	context, err := prompts.BuildRoleContext(config.Role, sections, data)
	if err != nil {
		return "", err
	}
	prompt += context

	if config.InitialTask != "" {
		prompt += fmt.Sprintf("\n\n=== RESUME CONTEXT ===\nResuming task: %s\n", config.InitialTask)
	}

	return prompt, nil
}

// buildOrchestratorPromptContext builds the complete prompt for the orchestrator role.
// Unlike task-based roles, the orchestrator has no task to look up. Dashboard and wake
// instruction content is pre-rendered and passed through block templates.
func buildOrchestratorPromptContext(state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (string, error) {
	data, err := buildOrchestratorRoleContextData(state, config, resolver)
	if err != nil {
		return "", err
	}

	prompt, err := prompts.BuildBasePrompt(baseConfigFrom(
		state,
		config,
		"",
		toBasePromptScipSearchIndexes(data.ScipIndexes),
		toBasePromptStacklitIndexes(data.StacklitIndexes),
		toBasePromptFunctionalClusterIndexes(data.FunctionalClusters),
		availablePromptSembleSearchMetadata(config.ProjectRoot, semble.TargetKindProjectRoot),
	))
	if err != nil {
		return "", fmt.Errorf("building base prompt: %w", err)
	}

	sections, err := resolver.ContextSections(config.Role)
	if err != nil {
		return "", fmt.Errorf("context sections for role %q: %w", config.Role, err)
	}

	context, err := prompts.BuildRoleContext(config.Role, sections, data)
	if err != nil {
		return "", err
	}
	prompt += context

	if config.InitialTask != "" {
		prompt += fmt.Sprintf("\n\n=== RESUME CONTEXT ===\nResuming task: %s\n", config.InitialTask)
	}

	return prompt, nil
}

func buildOrchestratorRoleContextData(state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (*prompts.RoleContextData, error) {
	dashboard, wakeInstruction, err := prompts.RenderOrchestratorDashboard(state, config.ProjectRoot, config.AgentID)
	if err != nil {
		return nil, err
	}

	availableIndexes := availablePromptScipIndexRefs(state, config.ProjectRoot)
	availableStacklitIndexes := availablePromptStacklitIndexRefs(config.ProjectRoot)
	availableFunctionalClusters := availablePromptFunctionalClusterIndexRefs(config.ProjectRoot)

	skills, _ := resolver.Skills(config.Role)
	mandatoryDocs, _ := resolver.MandatoryDocs(config.Role)

	return &prompts.RoleContextData{
		Role:               config.Role,
		AgentID:            config.AgentID,
		RoleType:           "orchestrator",
		DashboardOutput:    dashboard,
		WakeInstruction:    wakeInstruction,
		ScipIndexes:        availableIndexes,
		StacklitIndexes:    availableStacklitIndexes,
		FunctionalClusters: availableFunctionalClusters,
		ProjectRoot:        config.ProjectRoot,
		StatePath:          config.StatePath,
		SpecsDir:           config.SpecsDir,
		GoalDesc:           state.Goal.Description,
		Skills:             skills,
		MandatoryDocs:      mandatoryDocs,
	}, nil
}

func toPromptStacklitIndexRefs(indexes []stacklit.IndexRef) []prompts.StacklitIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.StacklitIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.StacklitIndexRef{Path: index.Path})
	}
	return refs
}

func toPromptFunctionalClusterIndexRefs(indexes []functionalclusters.IndexRef) []prompts.FunctionalClusterIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.FunctionalClusterIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.FunctionalClusterIndexRef{Path: index.Path})
	}
	return refs
}

func toPromptScipIndexRefs(indexes []scipsearch.IndexRef) []prompts.ScipIndexRef {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.ScipIndexRef, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.ScipIndexRef{
			Language: index.Language,
			Path:     index.Path,
		})
	}
	return refs
}

func toBasePromptScipSearchIndexes(indexes []prompts.ScipIndexRef) []prompts.ScipSearchIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.ScipSearchIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.ScipSearchIndex{
			Language:  index.Language,
			IndexPath: index.Path,
		})
	}
	return refs
}

func toBasePromptStacklitIndexes(indexes []prompts.StacklitIndexRef) []prompts.StacklitIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.StacklitIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.StacklitIndex{
			IndexPath: index.Path,
		})
	}
	return refs
}

func toBasePromptFunctionalClusterIndexes(indexes []prompts.FunctionalClusterIndexRef) []prompts.FunctionalClusterIndex {
	if len(indexes) == 0 {
		return nil
	}
	refs := make([]prompts.FunctionalClusterIndex, 0, len(indexes))
	for _, index := range indexes {
		refs = append(refs, prompts.FunctionalClusterIndex{
			IndexPath: index.Path,
		})
	}
	return refs
}

func availablePromptScipIndexRefs(state *models.State, targetRoot string) []prompts.ScipIndexRef {
	if targetRoot == "" || !scipsearch.RuntimeEnabled(state.Config.ScipSearch) {
		return nil
	}
	availableIndexes, err := scipAvailableIndexes(scipsearch.RuntimePlanOptions{
		TargetRoot:          targetRoot,
		ConfiguredLanguages: state.Config.ScipSearch,
	})
	if err != nil {
		return nil
	}
	return toPromptScipIndexRefs(availableIndexes)
}

func availablePromptStacklitIndexRefs(targetRoot string) []prompts.StacklitIndexRef {
	if targetRoot == "" || !stacklit.RuntimeEnabled() {
		return nil
	}
	availableIndexes, err := stacklitAvailableIndexes(stacklit.RuntimePlanOptions{
		TargetRoot: targetRoot,
	})
	if err != nil {
		return nil
	}
	return toPromptStacklitIndexRefs(availableIndexes)
}

func availablePromptFunctionalClusterIndexRefs(targetRoot string) []prompts.FunctionalClusterIndexRef {
	if targetRoot == "" || !functionalclusters.RuntimeEnabled() {
		return nil
	}
	availableIndexes, err := functionalClustersAvailableIndexes(functionalclusters.RuntimePlanOptions{
		TargetRoot: targetRoot,
	})
	if err != nil {
		return nil
	}
	return toPromptFunctionalClusterIndexRefs(availableIndexes)
}

func availablePromptSembleSearchMetadata(targetRoot string, kind semble.TargetKind) prompts.SembleSearchMetadata {
	if targetRoot == "" {
		return prompts.SembleSearchMetadata{}
	}
	opts := semble.PromptMetadataOptions{
		Kind:       kind,
		TargetRoot: targetRoot,
	}
	if kind == semble.TargetKindTaskWorktree {
		opts.ExpectedWorktreeRoot = targetRoot
	}
	metadata, ok := buildSemblePromptMetadata(opts)
	if !ok {
		return prompts.SembleSearchMetadata{}
	}
	return prompts.SembleSearchMetadata{
		TargetRoot:      metadata.TargetRoot,
		ShellTargetRoot: metadata.ShellTargetRoot,
	}
}

// buildTaskRoleContextData constructs RoleContextData for task-based roles (doers and reviewers).
func buildTaskRoleContextData(task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (*prompts.RoleContextData, error) {
	roleType, _ := resolver.RoleType(config.Role)

	totalPlanTasks, taskOrdinal := collectPlanPosition(state, task.ID)

	data := &prompts.RoleContextData{
		// Identity
		Role:     config.Role,
		AgentID:  config.AgentID,
		RoleType: roleType,

		// Task
		TaskID:             task.ID,
		Description:        task.Description,
		DoneWhen:           task.DoneWhen,
		Scope:              task.Scope,
		SpecRef:            task.SpecRef,
		EpicRef:            paths.SplitRefFile(task.EpicRef),
		EpicSection:        paths.SplitRefFragment(task.EpicRef),
		EpicSlug:           paths.GoalSlug(paths.SplitRefFile(task.EpicRef)),
		PlanRef:            paths.SplitRefFile(task.PlanRef),
		PlanSection:        paths.SplitRefFragment(task.PlanRef),
		ArchRef:            paths.SplitRefFile(task.ArchRef),
		RCARequired:        task.RCARequired,
		ValidationCommands: slices.Clone(task.Validation),
		DestructiveDB:      task.DestructiveDB,
		TaskDecomposition:  task.Decomposition,
		Worktree:           resolveWorktreePath(config.ProjectRoot, task.Worktree),
		IterationNum:       task.Iteration,
		AttemptNum:         task.EffectiveAttempt(),

		// Plan scoping
		GoalSpecRef:          state.Goal.SpecRef,
		TotalPlanTasks:       totalPlanTasks,
		TaskOrdinal:          taskOrdinal,
		DependsOn:            task.DependsOn,
		TaskRolePair:         task.RolePair,
		PhaseDependencyTasks: collectPhaseDependencyTasks(state, task),
		TaskGraph:            buildRelevantTaskGraph(state, task),

		// Config/state
		ProjectRoot: config.ProjectRoot,
		StatePath:   config.StatePath,
		SpecsDir:    config.SpecsDir,
		GoalDesc:    state.Goal.Description,
		GoalSlug:    paths.GoalSlug(state.Goal.SpecRef),

		IntegrationBranch: state.Config.IntegrationBranch,
	}

	data.ScipIndexes = availablePromptScipIndexRefs(state, data.Worktree)
	data.StacklitIndexes = availablePromptStacklitIndexRefs(data.Worktree)
	data.FunctionalClusters = availablePromptFunctionalClusterIndexRefs(data.Worktree)

	// Prior rejection
	if task.Iteration > 1 && task.RejectionReason != nil && *task.RejectionReason != "" && *task.RejectionReason != "null" {
		data.PriorRejection = *task.RejectionReason
	}

	// Prior attempt outcome (attempt 2 only)
	if data.AttemptNum == 2 {
		for i := len(task.History) - 1; i >= 0; i-- {
			if task.History[i].Event == models.TaskEventNewAttempt && task.History[i].Reason != nil {
				data.PriorAttemptOutcome = *task.History[i].Reason
				if task.History[i].Note != nil {
					data.PriorAttemptRejection = *task.History[i].Note
				}
				break
			}
		}
	}

	// Doer-specific: coder fields
	if roleType == "doer" && config.Role == "coder" {
		data.IntegrationFix = task.IntegrationFix
		// Find the last context_exhaustion HandoffEvent for resume context
		for i := len(task.HandoffEvents) - 1; i >= 0; i-- {
			if task.HandoffEvents[i].Trigger == models.HandoffTriggerContextExhaustion {
				evt := task.HandoffEvents[i]
				data.HandoffNote = &evt
				break
			}
		}
	}

	// Reviewer-specific fields
	if roleType == "reviewer" {
		data.BaseCommit = derefString(task.BaseCommit)
		data.ReviewCommit = derefString(task.ReviewCommit)
		data.AssignedTo = derefString(task.AssignedTo)
		if task.AssignedTo != nil {
			data.ScopeExtensions = ops.GetLatestScopeExtensions(task.History, *task.AssignedTo)
			data.ValidationPlan = ops.GetValidationPlan(task.History, *task.AssignedTo)
		}
	}

	// Integration-specific: branch context for analyst and reviewer
	if config.Role == roles.IntegrationAnalyst || config.Role == roles.IntegrationReviewer {
		if err := populateIntegrationContext(task, state, resolver, data); err != nil {
			return nil, err
		}
	}

	// Declarative fields from pipeline YAML
	if skills, err := resolver.Skills(config.Role); err == nil {
		data.Skills = skills
	}
	if mandatoryDocs, err := resolver.MandatoryDocs(config.Role); err == nil {
		data.MandatoryDocs = mandatoryDocs
	}

	// Architect-specific context
	if config.Role == roles.Architect {
		for _, parentID := range task.EffectiveParentTasks() {
			parent := state.FindTask(parentID)
			if parent != nil {
				data.ParentTaskContexts = append(data.ParentTaskContexts, prompts.ParentTaskContext{
					ID:          parent.ID,
					Description: prompts.TruncateText(parent.Description, 500),
					DoneWhen:    parent.DoneWhen,
					SpecRef:     parent.SpecRef,
					EpicRef:     parent.EpicRef,
					PlanRef:     parent.PlanRef,
				})
			}
		}

		exists, err := precommit.ConfigExistsOnIntegration(config.ProjectRoot, state.Config.IntegrationBranch)
		if err != nil {
			return nil, fmt.Errorf("precommit config check: %w", err)
		}
		data.PreCommitConfigExists = exists
		data.PreCommitBootstrapInFlight = precommit.BootstrapInFlight(state)
		data.PreCommitKind = precommit.Kind
	}

	return data, nil
}

func populateIntegrationContext(task *models.Task, state *models.State, resolver *pipeline.Resolver, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	if metadata == nil {
		if state.Goal.BaseCommit != nil {
			data.GoalBaseCommit = *state.Goal.BaseCommit
		}
		data.CompletedTasks = collectCompletedTasks(state)
		return nil
	}
	if !metadata.Phase.IsValid() {
		return fmt.Errorf("integration context for task %s has invalid phase %q", task.ID, metadata.Phase)
	}
	if metadata.SourceCommit == "" {
		return fmt.Errorf("integration context for task %s has empty source commit", task.ID)
	}

	data.IntegrationPhase = metadata.Phase
	data.IntegrationGeneration = metadata.Generation
	data.IntegrationSourceCommit = metadata.SourceCommit

	switch metadata.Phase {
	case models.IntegrationAnalysisPhaseSlice:
		return populateSliceIntegrationContext(task, state, resolver, data)
	case models.IntegrationAnalysisPhaseGlobal:
		return populateGlobalIntegrationContext(task, state, data)
	default:
		return fmt.Errorf("integration context for task %s has unsupported phase %q", task.ID, metadata.Phase)
	}
}

func populateSliceIntegrationContext(task *models.Task, state *models.State, resolver *pipeline.Resolver, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	capability, err := resolver.SlicedIntegrationCapability()
	if err != nil {
		return fmt.Errorf("resolve sliced integration capability: %w", err)
	}
	if !capability.Available {
		return fmt.Errorf("slice integration context unavailable (%s): %s", capability.Code, capability.Guidance)
	}
	if metadata.Generation != 0 {
		return fmt.Errorf("slice integration context for task %s has generation %d", task.ID, metadata.Generation)
	}
	if metadata.OriginatingPlanTaskID == "" {
		return fmt.Errorf("slice integration context for task %s has no originating plan", task.ID)
	}
	originatingPlan := state.FindTask(metadata.OriginatingPlanTaskID)
	if originatingPlan == nil {
		return fmt.Errorf("slice integration context for task %s references missing plan %q", task.ID, metadata.OriginatingPlanTaskID)
	}
	data.IntegrationOriginatingPlan = &prompts.IntegrationPlanSummary{
		ID:          originatingPlan.ID,
		Description: originatingPlan.Description,
		DoneWhen:    originatingPlan.DoneWhen,
		SpecRef:     originatingPlan.SpecRef,
		PlanRef:     paths.SplitRefFile(originatingPlan.PlanRef),
		ArchRef:     paths.SplitRefFile(originatingPlan.ArchRef),
	}

	rootIDs, err := sortedUniquePromptStrings(metadata.RootTaskIDs, "root task")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	if len(rootIDs) == 0 {
		return fmt.Errorf("slice integration context for task %s has no root tasks", task.ID)
	}
	for _, rootID := range rootIDs {
		if state.FindTask(rootID) == nil {
			return fmt.Errorf("slice integration context for task %s references missing root task %q", task.ID, rootID)
		}
	}
	data.IntegrationRootTaskIDs = rootIDs

	changes := slices.Clone(metadata.DescendantChanges)
	sort.Slice(changes, func(i, j int) bool { return changes[i].TaskID < changes[j].TaskID })
	seenDescendants := make(map[string]struct{}, len(changes))
	for _, change := range changes {
		if change.TaskID == "" || change.Commit == "" {
			return fmt.Errorf("slice integration context for task %s has incomplete descendant attribution", task.ID)
		}
		if _, duplicate := seenDescendants[change.TaskID]; duplicate {
			return fmt.Errorf("slice integration context for task %s repeats descendant %q", task.ID, change.TaskID)
		}
		seenDescendants[change.TaskID] = struct{}{}
		descendant := state.FindTask(change.TaskID)
		if descendant == nil {
			return fmt.Errorf("slice integration context for task %s references missing descendant task %q", task.ID, change.TaskID)
		}
		if descendant.MergeCommit == nil || *descendant.MergeCommit != change.Commit {
			return fmt.Errorf("slice integration context for task %s descendant %q commit contradicts persisted task evidence", task.ID, change.TaskID)
		}
		dependsOn := slices.Clone(descendant.DependsOn)
		sort.Strings(dependsOn)
		data.IntegrationDescendants = append(data.IntegrationDescendants, prompts.IntegrationDescendantSummary{
			ID:            descendant.ID,
			Description:   prompts.TruncateText(descendant.Description, 200),
			DoneWhen:      descendant.DoneWhen,
			SpecRef:       descendant.SpecRef,
			Commit:        change.Commit,
			DependsOn:     dependsOn,
			Decomposition: cloneDecompositionManifest(descendant.Decomposition),
		})
	}

	affectedPaths, err := sortedUniquePromptStrings(metadata.AffectedPaths, "affected path")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	snapshotPaths, err := sortedUniquePromptStrings(metadata.SourceSnapshotPaths, "snapshot path")
	if err != nil {
		return fmt.Errorf("slice integration context for task %s: %w", task.ID, err)
	}
	affected := make(map[string]struct{}, len(affectedPaths))
	for _, path := range affectedPaths {
		affected[path] = struct{}{}
	}
	for _, path := range snapshotPaths {
		if _, ok := affected[path]; !ok {
			return fmt.Errorf("slice integration context for task %s snapshot path %q is not attributable", task.ID, path)
		}
	}
	data.IntegrationAffectedPaths = affectedPaths
	data.IntegrationSnapshotPaths = snapshotPaths
	return nil
}

func populateGlobalIntegrationContext(task *models.Task, state *models.State, data *prompts.RoleContextData) error {
	metadata := task.IntegrationAnalysis
	if metadata.Generation <= 0 {
		return fmt.Errorf("global integration context for task %s has invalid generation %d", task.ID, metadata.Generation)
	}
	if metadata.OriginatingPlanTaskID != "" || len(metadata.RootTaskIDs) != 0 || len(metadata.DescendantChanges) != 0 || len(metadata.AffectedPaths) != 0 || len(metadata.SourceSnapshotPaths) != 0 {
		return fmt.Errorf("global integration context for task %s contains slice-only metadata", task.ID)
	}
	if state.Goal.BaseCommit == nil || *state.Goal.BaseCommit == "" {
		return fmt.Errorf("global integration context for task %s has no goal base commit", task.ID)
	}
	data.GoalBaseCommit = *state.Goal.BaseCommit
	if state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil {
		return fmt.Errorf("global integration context for task %s has no frozen contributing set", task.ID)
	}

	coverageByPlan := make(map[string]models.IntegrationCoverageRecord, len(state.Goal.Integration.Coverage))
	for _, record := range state.Goal.Integration.Coverage {
		if record.PlanTaskID == "" {
			return fmt.Errorf("global integration context for task %s has coverage with no plan", task.ID)
		}
		if _, duplicate := coverageByPlan[record.PlanTaskID]; duplicate {
			return fmt.Errorf("global integration context for task %s repeats coverage plan %q", task.ID, record.PlanTaskID)
		}
		coverageByPlan[record.PlanTaskID] = record
	}

	scopes := slices.Clone(state.Goal.Integration.ContributingSet.Scopes)
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].PlanTaskID < scopes[j].PlanTaskID })
	seenPlans := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope.PlanTaskID == "" {
			return fmt.Errorf("global integration context for task %s has contributing scope with no plan", task.ID)
		}
		if _, duplicate := seenPlans[scope.PlanTaskID]; duplicate {
			return fmt.Errorf("global integration context for task %s repeats contributing plan %q", task.ID, scope.PlanTaskID)
		}
		seenPlans[scope.PlanTaskID] = struct{}{}
		if state.FindTask(scope.PlanTaskID) == nil {
			return fmt.Errorf("global integration context for task %s references missing contributing plan %q", task.ID, scope.PlanTaskID)
		}
		record, ok := coverageByPlan[scope.PlanTaskID]
		if !ok {
			if len(scopes) >= 2 {
				return fmt.Errorf("global integration context for task %s lacks coverage for plan %q", task.ID, scope.PlanTaskID)
			}
			continue
		}
		summary, err := integrationCoverageSummary(state, scope, record)
		if err != nil {
			return fmt.Errorf("global integration context for task %s: %w", task.ID, err)
		}
		data.IntegrationCoverage = append(data.IntegrationCoverage, summary)
	}
	for planTaskID := range coverageByPlan {
		if _, ok := seenPlans[planTaskID]; !ok {
			return fmt.Errorf("global integration context for task %s contains coverage outside the frozen contributing set", task.ID)
		}
	}
	return nil
}

func integrationCoverageSummary(state *models.State, scope models.IntegrationScopeSnapshot, record models.IntegrationCoverageRecord) (prompts.IntegrationCoverageSummary, error) {
	summary := prompts.IntegrationCoverageSummary{PlanTaskID: record.PlanTaskID, Kind: string(record.Kind)}
	switch record.Kind {
	case models.IntegrationCoverageApprovalAttestation:
		if len(record.ApprovalAttestations) == 0 || record.SliceReport != nil {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("approval coverage for plan %q has contradictory payload", record.PlanTaskID)
		}
		attestations := slices.Clone(record.ApprovalAttestations)
		sort.Slice(attestations, func(i, j int) bool { return attestations[i].ReviewedTaskID < attestations[j].ReviewedTaskID })
		for _, attestation := range attestations {
			if attestation.ReviewedTaskID == "" || state.FindTask(attestation.ReviewedTaskID) == nil {
				return prompts.IntegrationCoverageSummary{}, fmt.Errorf("approval coverage for plan %q references missing task %q", record.PlanTaskID, attestation.ReviewedTaskID)
			}
			summary.ApprovalAttestations = append(summary.ApprovalAttestations, prompts.IntegrationApprovalSummary{
				ReviewedTaskID:     attestation.ReviewedTaskID,
				AcceptanceCriteria: attestation.AcceptanceCriteria,
				ReviewedCommit:     attestation.ReviewedCommit,
				Approver:           attestation.Approver,
				Validation:         slices.Clone(attestation.Validation),
				MergeCommit:        attestation.MergeCommit,
			})
		}
	case models.IntegrationCoverageSliceReport:
		if record.SliceReport == nil || len(record.ApprovalAttestations) != 0 {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q has contradictory payload", record.PlanTaskID)
		}
		report := record.SliceReport
		analysis := state.FindTask(report.AnalysisTaskID)
		if analysis == nil || analysis.IntegrationAnalysis == nil {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q references missing analysis task %q", record.PlanTaskID, report.AnalysisTaskID)
		}
		analysisMetadata := analysis.IntegrationAnalysis
		if analysisMetadata.Phase != models.IntegrationAnalysisPhaseSlice || analysisMetadata.Key != report.AnalysisKey || analysisMetadata.OriginatingPlanTaskID != scope.PlanTaskID || analysisMetadata.SourceCommit != report.SourceCommit {
			return prompts.IntegrationCoverageSummary{}, fmt.Errorf("slice coverage for plan %q contradicts analysis task %q", record.PlanTaskID, report.AnalysisTaskID)
		}
		summary.SliceReport = &prompts.IntegrationSliceReportSummary{
			AnalysisTaskID: report.AnalysisTaskID,
			AnalysisKey:    report.AnalysisKey,
			Verdict:        string(report.Verdict),
			SourceCommit:   report.SourceCommit,
			ReportCommit:   report.ReportCommit,
		}
	default:
		return prompts.IntegrationCoverageSummary{}, fmt.Errorf("coverage for plan %q has invalid kind %q", record.PlanTaskID, record.Kind)
	}
	return summary, nil
}

func sortedUniquePromptStrings(values []string, label string) ([]string, error) {
	result := slices.Clone(values)
	sort.Strings(result)
	for i, value := range result {
		if value == "" {
			return nil, fmt.Errorf("%s is empty", label)
		}
		if i > 0 && value == result[i-1] {
			return nil, fmt.Errorf("duplicate %s %q", label, value)
		}
	}
	return result, nil
}

func cloneDecompositionManifest(manifest *models.DecompositionManifest) *models.DecompositionManifest {
	if manifest == nil {
		return nil
	}
	clone := *manifest
	clone.OwnedFiles = slices.Clone(manifest.OwnedFiles)
	clone.OwnedModules = slices.Clone(manifest.OwnedModules)
	clone.ReadOnlyDependsOn = slices.Clone(manifest.ReadOnlyDependsOn)
	clone.ReadOnlyTaskDependsOn = slices.Clone(manifest.ReadOnlyTaskDependsOn)
	clone.InterfacesOwned = slices.Clone(manifest.InterfacesOwned)
	clone.InterfacesConsumed = slices.Clone(manifest.InterfacesConsumed)
	return &clone
}

func taskContextSections(base []string, task *models.Task, data *prompts.RoleContextData, resolver *pipeline.Resolver) ([]string, error) {
	sections := append([]string(nil), base...)
	if task.RolePair == "" || (data.RoleType != "doer" && data.RoleType != "reviewer") {
		return sections, nil
	}

	isRoot, err := resolver.IsDecompositionRoot(task.RolePair)
	if err != nil {
		return nil, err
	}
	if !isRoot {
		return sections, nil
	}

	refField, err := resolver.DecompositionOutputRef(task.RolePair)
	if err != nil {
		return nil, err
	}

	data.DecompositionRoot = true
	data.MasterOutputRefField = refField
	if data.RoleType == "doer" {
		if data.Role == models.RoleCodePlanner {
			sections = slices.DeleteFunc(sections, func(section string) bool {
				return section == "task-decomposition" || section == "implementation-phase"
			})
		}
		sections = append(sections, "master-decomposition-mandate")
	} else {
		if data.Role == models.RoleCodePlanReviewer {
			sections = slices.DeleteFunc(sections, func(section string) bool {
				return section == "review-instructions"
			})
		}
		sections = append(sections, "master-decomposition-review")
	}
	return sections, nil
}

// collectCompletedTasks returns summaries of all MERGED tasks for integration context.
func collectCompletedTasks(state *models.State) []prompts.CompletedTaskSummary {
	var tasks []prompts.CompletedTaskSummary
	for _, t := range state.Tasks {
		if t.Status == models.TaskStatusMerged {
			tasks = append(tasks, prompts.CompletedTaskSummary{
				ID:          t.ID,
				Description: prompts.TruncateText(t.Description, 200),
				DoneWhen:    t.DoneWhen,
				SpecRef:     t.SpecRef,
			})
		}
	}
	return tasks
}

// resolveWorktreePath returns the absolute worktree path, or "" if worktree is nil.
// The stored worktree value is always slash-separated; joining it with filepath.Join
// yields the platform-native path the agent's tools and the init gate expect.
func resolveWorktreePath(projectRoot string, worktree *string) string {
	if worktree == nil {
		return ""
	}
	return filepath.Join(projectRoot, *worktree)
}

// derefString returns the value pointed to by s, or "" if s is nil.
func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// collectPlanPosition returns the visible count of planned tasks and the 1-based
// ordinal position of currentTaskID in the visible plan.
// Returns 0, 0 if no planned tasks or if currentTaskID is not in the planned list
// (e.g. mid-sprint replacement tasks created outside the original plan).
//
// Note: tasks not found by FindTask are silently skipped. This assumes the orchestrator keeps
// Sprint.Scope.Planned in sync with the task list (archived/removed tasks are pruned from planned[]).
func collectPlanPosition(state *models.State, currentTaskID string) (int, int) {
	planned := state.Sprint.Scope.Planned
	if len(planned) == 0 {
		return 0, 0
	}

	ordinal := 0
	visibleTotal := 0
	for _, id := range planned {
		task := state.FindTask(id)
		if task == nil {
			continue
		}
		if id == currentTaskID {
			visibleTotal++
			ordinal = visibleTotal
			continue
		}
		if isDeadPathPlanSibling(task.Status) {
			continue
		}
		visibleTotal++
	}

	// Suppress scoping for tasks not in the plan (mid-sprint replacements).
	// Returning 0 for totalPlanTasks ensures the template condition is false.
	if ordinal == 0 {
		return 0, 0
	}

	return visibleTotal, ordinal
}

func isDeadPathPlanSibling(status models.TaskStatus) bool {
	return status == models.TaskStatusAbandoned || status == models.TaskStatusSuperseded
}

func siblingTaskSummary(task *models.Task) prompts.SiblingTaskSummary {
	return prompts.SiblingTaskSummary{
		ID:          task.ID,
		Description: prompts.TruncateText(task.Description, 96),
		Status:      string(task.Status),
		PlanRef:     task.PlanRef,
		RolePair:    task.RolePair,
	}
}

func collectPhaseDependencyTasks(state *models.State, current *models.Task) []prompts.SiblingTaskSummary {
	if len(current.DependsOn) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var dependencies []prompts.SiblingTaskSummary
	for _, depID := range current.DependsOn {
		dep := state.FindTask(depID)
		if dep == nil || dep.RolePair != current.RolePair || seen[dep.ID] {
			continue
		}
		dependencies = append(dependencies, siblingTaskSummary(dep))
		seen[dep.ID] = true
	}
	return dependencies
}

var (
	backtickRefPattern = regexp.MustCompile("`([^`]+)`")
	// Heuristic path extraction can match URL or date-like fragments; only
	// shared refs are surfaced, so most one-off false positives drop out.
	pathRefPattern = regexp.MustCompile(`[A-Za-z0-9_.@+/-]+/[A-Za-z0-9_.@+/-]+|[A-Za-z0-9_.@+-]+\.[A-Za-z0-9][A-Za-z0-9_.@+-]*`)
)

const (
	maxTaskGraphChildrenPerEntry = 8
	maxTaskGraphChildDeps        = 3
	maxTaskGraphEntries          = 16
	maxTaskGraphOmittedSampleIDs = 8
)

func buildRelevantTaskGraph(state *models.State, current *models.Task) prompts.TaskGraphDigest {
	currentRefs := taskScopeRefs(current)
	childrenByParent := taskChildrenByParent(state)
	entries := make(map[string]*prompts.TaskGraphEntry)
	var ordered []*prompts.TaskGraphEntry

	addTask := func(task *models.Task, relation string, sharedRefs []string) {
		if task == nil {
			return
		}
		entry := entries[task.ID]
		if entry == nil {
			newEntry := taskGraphEntry(task)
			entry = &newEntry
			entries[task.ID] = entry
			ordered = append(ordered, entry)
		}
		entry.Relations = appendUniqueString(entry.Relations, relation)
		entry.SharedRefs = appendUniqueStrings(entry.SharedRefs, sharedRefs...)
	}

	dependencies := make([]*models.Task, 0, len(current.DependsOn))
	for _, depID := range current.DependsOn {
		dep := state.FindTask(depID)
		if dep == nil {
			continue
		}
		dependencies = append(dependencies, dep)
		addTask(dep, "dependency", nil)
		if dep.Status == models.TaskStatusBlocked {
			addTask(dep, "blocked", nil)
		}
	}

	siblings := plannedSiblings(state, current.ID)
	for _, sibling := range siblings {
		if sibling.Status == models.TaskStatusBlocked {
			addTask(sibling, "blocked", nil)
		}
	}

	for _, sibling := range siblings {
		if sibling.Status.IsTerminal() {
			continue
		}
		sharedRefs := intersectRefs(currentRefs, taskScopeRefs(sibling))
		if len(sharedRefs) > 0 {
			addTask(sibling, "sibling", nil)
			addTask(sibling, "file-overlap", sharedRefs)
		}
	}

	for _, dep := range dependencies {
		if taskHasProducedOutputRefs(dep) {
			addTask(dep, "artifact-producer", nil)
		}
		if !isCompletedForDigest(state, dep) {
			continue
		}
		if taskHasTaskArtifactRefs(dep) {
			addTask(dep, "artifact-ref", nil)
		}
	}

	for _, sibling := range siblings {
		if taskHasProducedOutputRefs(sibling) {
			addTask(sibling, "artifact-producer", nil)
		}
		if !isCompletedForDigest(state, sibling) {
			continue
		}
		if taskHasTaskArtifactRefs(sibling) {
			addTask(sibling, "artifact-ref", nil)
		}
	}

	for _, sibling := range siblings {
		if !isDeadPathPlanSibling(sibling.Status) {
			addTask(sibling, "sibling", nil)
		}
	}

	digest := prompts.TaskGraphDigest{
		Entries: make([]prompts.TaskGraphEntry, 0, len(ordered)),
	}
	for _, entry := range boundedTaskGraphEntries(ordered, &digest) {
		entry.Children, entry.RemainingChildren = summarizeTaskGraphChildren(childrenByParent[entry.ID])
		digest.Entries = append(digest.Entries, *entry)
	}
	return digest
}

func boundedTaskGraphEntries(ordered []*prompts.TaskGraphEntry, digest *prompts.TaskGraphDigest) []*prompts.TaskGraphEntry {
	if len(ordered) <= maxTaskGraphEntries {
		return ordered
	}

	ranked := append([]*prompts.TaskGraphEntry(nil), ordered...)
	sort.SliceStable(ranked, func(i, j int) bool {
		// Equal scores keep the existing candidate discovery order.
		return taskGraphPriority(ranked[i]) > taskGraphPriority(ranked[j])
	})

	selectedIDs := make(map[string]bool, maxTaskGraphEntries)
	for _, entry := range ranked[:maxTaskGraphEntries] {
		selectedIDs[entry.ID] = true
	}

	var bounded []*prompts.TaskGraphEntry
	var omitted []prompts.TaskGraphEntry
	for _, entry := range ordered {
		if selectedIDs[entry.ID] {
			bounded = append(bounded, entry)
			continue
		}
		omitted = append(omitted, *entry)
	}
	digest.Omitted = summarizeOmittedTaskGraphEntries(omitted)
	return bounded
}

func taskGraphPriority(entry *prompts.TaskGraphEntry) int {
	if entry == nil {
		return 0
	}
	if hasTaskGraphRelation(entry, "dependency") {
		return 100
	}
	if hasTaskGraphRelation(entry, "blocked") || entry.BlockedReason != "" || entry.RepairOperation != "" {
		return 90
	}
	if hasTaskGraphRelation(entry, "file-overlap") {
		return 80
	}
	if isArtifactOnlyTaskGraphEntry(entry) {
		return 10
	}
	if hasTaskGraphRelation(entry, "sibling") {
		return 50
	}
	return 30
}

func isArtifactOnlyTaskGraphEntry(entry *prompts.TaskGraphEntry) bool {
	if entry == nil {
		return false
	}
	if hasTaskGraphRelation(entry, "dependency") ||
		hasTaskGraphRelation(entry, "blocked") ||
		hasTaskGraphRelation(entry, "file-overlap") ||
		entry.BlockedReason != "" ||
		entry.RepairOperation != "" {
		return false
	}
	return hasTaskGraphRelation(entry, "artifact-producer") || hasTaskGraphRelation(entry, "artifact-ref")
}

func hasTaskGraphRelation(entry *prompts.TaskGraphEntry, relation string) bool {
	return slices.Contains(entry.Relations, relation)
}

func summarizeOmittedTaskGraphEntries(entries []prompts.TaskGraphEntry) prompts.TaskGraphOmittedSummary {
	summary := prompts.TaskGraphOmittedSummary{Count: len(entries)}
	if len(entries) == 0 {
		return summary
	}

	statusCounts := make(map[string]int)
	relationCounts := make(map[string]int)
	for _, entry := range entries {
		statusCounts[entry.Status]++
		for _, relation := range entry.Relations {
			relationCounts[relation]++
		}
		if len(summary.SampleIDs) < maxTaskGraphOmittedSampleIDs {
			summary.SampleIDs = append(summary.SampleIDs, entry.ID)
		}
	}
	summary.RemainingSampleIDs = len(entries) - len(summary.SampleIDs)
	summary.StatusCounts = sortedTaskGraphCounts(statusCounts)
	summary.RelationCounts = sortedTaskGraphCounts(relationCounts)
	return summary
}

func sortedTaskGraphCounts(counts map[string]int) []prompts.TaskGraphCount {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	sorted := make([]prompts.TaskGraphCount, 0, len(keys))
	for _, key := range keys {
		sorted = append(sorted, prompts.TaskGraphCount{Name: key, Count: counts[key]})
	}
	return sorted
}

func taskChildrenByParent(state *models.State) map[string][]*models.Task {
	childrenByParent := make(map[string][]*models.Task)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for _, parentID := range task.EffectiveParentTasks() {
			if parentID == "" {
				continue
			}
			childrenByParent[parentID] = append(childrenByParent[parentID], task)
		}
	}
	return childrenByParent
}

func summarizeTaskGraphChildren(children []*models.Task) ([]prompts.TaskGraphChildSummary, int) {
	limit := min(len(children), maxTaskGraphChildrenPerEntry)
	summaries := make([]prompts.TaskGraphChildSummary, 0, limit)
	for _, child := range children[:limit] {
		depLimit := min(len(child.DependsOn), maxTaskGraphChildDeps)
		summaries = append(summaries, prompts.TaskGraphChildSummary{
			ID:                 child.ID,
			Status:             string(child.Status),
			RolePair:           child.RolePair,
			DependsOn:          child.DependsOn[:depLimit],
			RemainingDependsOn: len(child.DependsOn) - depLimit,
		})
	}
	return summaries, len(children) - limit
}

func plannedSiblings(state *models.State, currentTaskID string) []*models.Task {
	inPlan := false
	for _, id := range state.Sprint.Scope.Planned {
		if id == currentTaskID {
			inPlan = true
			break
		}
	}
	if !inPlan {
		return nil
	}

	var siblings []*models.Task
	for _, id := range state.Sprint.Scope.Planned {
		if id == currentTaskID {
			continue
		}
		if task := state.FindTask(id); task != nil {
			siblings = append(siblings, task)
		}
	}
	return siblings
}

func taskGraphEntry(task *models.Task) prompts.TaskGraphEntry {
	entry := prompts.TaskGraphEntry{
		ID:          task.ID,
		Description: prompts.TruncateText(task.Description, 96),
		Status:      string(task.Status),
	}
	if task.BlockedReason != nil {
		entry.BlockedReason = prompts.TruncateText(*task.BlockedReason, 120)
	}
	if task.RepairRequest != nil {
		entry.RepairOperation = task.RepairRequest.Operation
		if task.RepairRequest.Target != "" {
			entry.RepairOperation += " -> " + task.RepairRequest.Target
		}
	}
	return entry
}

func taskScopeRefs(task *models.Task) []string {
	refs := extractFileRefs(task.Scope)
	refs = appendUniqueStrings(refs, extractFileRefs(task.DoneWhen)...)
	return refs
}

func extractFileRefs(text string) []string {
	var refs []string
	for _, match := range backtickRefPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			refs = appendPathRef(refs, match[1])
		}
	}
	for _, match := range pathRefPattern.FindAllString(text, -1) {
		refs = appendPathRef(refs, match)
	}
	return refs
}

func appendPathRef(refs []string, ref string) []string {
	ref = strings.Trim(ref, " \t\r\n.,;:()[]{}\"'")
	if ref == "" || strings.Contains(ref, "*") || strings.Contains(ref, " ") {
		return refs
	}
	if !strings.Contains(ref, "/") && !strings.Contains(ref, ".") {
		return refs
	}
	return appendUniqueString(refs, ref)
}

func taskHasTaskArtifactRefs(task *models.Task) bool {
	if task == nil {
		return false
	}
	return task.SpecRef != "" || task.EpicRef != "" || task.PlanRef != "" || task.ArchRef != ""
}

func taskHasProducedOutputRefs(task *models.Task) bool {
	if task == nil {
		return false
	}
	for _, entry := range task.Output {
		if entry.SpecRef != "" || entry.EpicRef != "" || entry.PlanRef != "" || entry.ArchRef != "" {
			return true
		}
	}
	return false
}

func intersectRefs(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	rightSet := make(map[string]bool, len(right))
	for _, ref := range right {
		rightSet[ref] = true
	}
	var shared []string
	for _, ref := range left {
		if rightSet[ref] {
			shared = append(shared, ref)
		}
	}
	return shared
}

func isCompletedForDigest(state *models.State, task *models.Task) bool {
	if state == nil || task == nil {
		return false
	}
	return state.ResolveDependency(task.ID).Satisfied()
}

func appendUniqueStrings(refs []string, candidates ...string) []string {
	for _, candidate := range candidates {
		refs = appendUniqueString(refs, candidate)
	}
	return refs
}

func appendUniqueString(refs []string, candidate string) []string {
	if candidate == "" {
		return refs
	}
	for _, existing := range refs {
		if existing == candidate {
			return refs
		}
	}
	return append(refs, candidate)
}

func savePrompt(promptDir, agentID, prompt string) (string, error) {
	return saveTimestampedFile(promptDir, agentID, "txt", prompt)
}
