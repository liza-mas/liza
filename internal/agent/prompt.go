package agent

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/semble"
)

// baseConfigFrom constructs the BasePromptConfig shared by all roles.
func baseConfigFrom(state *models.State, config SupervisorConfig, taskID string, scipIndexes []prompts.ScipSearchIndex, stacklitIndexes []prompts.StacklitIndex, functionalClusterIndexes []prompts.FunctionalClusterIndex, sembleSearch prompts.SembleSearchMetadata, hasMandatoryDocs bool) prompts.BasePromptConfig {
	return prompts.BasePromptConfig{
		Role:               config.Role,
		AgentID:            config.AgentID,
		TaskID:             taskID,
		SpecsDir:           config.SpecsDir,
		ProjectRoot:        config.ProjectRoot,
		StatePath:          config.StatePath,
		GoalDesc:           state.Goal.Description,
		GoalSpecRef:        state.Goal.SpecRef,
		HasMandatoryDocs:   hasMandatoryDocs,
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
		availablePromptSembleSearchMetadata(config.ProjectRoot, semble.TargetKindProjectRoot),
		len(data.MandatoryDocs) > 0,
	))
	if err != nil {
		return "", fmt.Errorf("building base prompt: %w", err)
	}

	sections, err := resolver.ContextSections(config.Role)
	if err != nil {
		return "", fmt.Errorf("context sections for role %q: %w", config.Role, err)
	}
	sections, err = TaskContextSections(sections, task, data, resolver)
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
	return buildOrchestratorPromptForWake(state, config, resolver, nil)
}

func buildOrchestratorPromptForWake(state *models.State, config SupervisorConfig, resolver *pipeline.Resolver, wake *OrchestratorWakeResult) (string, error) {
	data, err := buildOrchestratorRoleContextDataForWake(state, config, resolver, wake)
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
		len(data.MandatoryDocs) > 0,
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
	return buildOrchestratorRoleContextDataForWake(state, config, resolver, nil)
}

func buildOrchestratorRoleContextDataForWake(state *models.State, config SupervisorConfig, resolver *pipeline.Resolver, wake *OrchestratorWakeResult) (*prompts.RoleContextData, error) {
	var dashboard, wakeInstruction string
	var err error
	if wake == nil {
		dashboard, wakeInstruction, err = RenderOrchestratorDashboard(state, config.ProjectRoot, config.AgentID)
	} else {
		dashboard, wakeInstruction, err = renderOrchestratorDashboardForWake(state, config.ProjectRoot, config.AgentID, *wake)
	}
	if err != nil {
		return nil, err
	}

	// The lifecycle hooks publish repo-root SCIP indexes at the root, not
	// under the runtime directory where worktree refreshes write theirs.
	availableIndexes := availablePromptScipIndexRefs(state, config.ProjectRoot, scipAvailableProjectRootIndexes)
	availableStacklitIndexes := availablePromptStacklitIndexRefs(config.ProjectRoot)
	availableFunctionalClusters := availablePromptFunctionalClusterIndexRefs(config.ProjectRoot)

	skills, _ := resolver.Skills(config.Role)
	mandatoryDocs, _ := resolver.MandatoryDocs(config.Role)

	return &prompts.RoleContextData{
		Role:               config.Role,
		AgentID:            config.AgentID,
		RoleType:           string(roles.RoleTypeOrchestrator),
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
		TestFilePatterns:   SubmissionTestPatternsText(),
	}, nil
}

// admissionMatches lists the distinct match substrings of config's valid
// admission limits, the ones admit and canonical admission act on.
func admissionMatches(config models.Config) []string {
	var matches []string
	for _, limit := range config.AdmissionLimits {
		if models.ValidateAdmissionLimit(limit) != nil {
			continue
		}
		for _, match := range limit.Match {
			if !slices.Contains(matches, match) {
				matches = append(matches, match)
			}
		}
	}
	return matches
}

// buildTaskRoleContextData constructs RoleContextData for task-based roles (doers and reviewers).
// The stages run in a fixed order: a stage that fails returns no partial record,
// so the planner navigation stage always answers before the reference stage.
func buildTaskRoleContextData(task *models.Task, state *models.State, config SupervisorConfig, resolver *pipeline.Resolver) (*prompts.RoleContextData, error) {
	roleType, _ := resolver.RoleType(config.Role)

	data := newTaskContextData(task, state, config, roleType)
	populateRepositoryIntelligence(data, task, state)
	populatePriorRejection(data, task)
	if err := populatePlannerContext(task, state, config, data); err != nil {
		return nil, fmt.Errorf("planner navigation context: %w", err)
	}
	populatePriorAttempt(data, task)
	populateCoderRecovery(data, task, config.Role, roleType)
	populateValidationPrerequisites(data, task, state, config, resolver)
	populateReviewerFields(data, task, roleType)

	// Integration-specific: branch context for analyst and reviewer
	if config.Role == roles.IntegrationAnalyst || config.Role == roles.IntegrationReviewer {
		if err := populateIntegrationContext(task, state, resolver, config.ProjectRoot, data); err != nil {
			return nil, err
		}
	}

	populateDeclarativeFields(data, config.Role, resolver)
	if config.Role == roles.Architect {
		if err := populateArchitectContext(data, task, state, config); err != nil {
			return nil, err
		}
	}
	if err := populateReferenceProvenance(data, task, state, config, resolver, roleType); err != nil {
		return nil, err
	}
	return data, nil
}

// consumersValidateLocally reports whether every doer and reviewer role of the
// pairs consuming rolePair's output[] launches by default on a CLI asserting
// validation_execution: local. Declarations under any other policy fail closed
// at claim, so an unresolved or mixed selection is not local.
func consumersValidateLocally(rolePair string, config models.Config, projectRoot string, resolver *pipeline.Resolver, output ...[]models.OutputEntry) bool {
	pairs, err := resolver.OutputConsumerRolePairs(rolePair)
	if len(output) > 0 {
		pairs, err = resolver.OutputConsumerRolePairsForOutput(rolePair, output[0])
	}
	if err != nil || len(pairs) == 0 {
		return false
	}
	var roles []string
	for _, pair := range pairs {
		doer, err := resolver.DoerRole(pair)
		if err != nil {
			return false
		}
		reviewer, err := resolver.ReviewerRole(pair)
		if err != nil {
			return false
		}
		roles = append(roles, doer, reviewer)
	}
	roleModels, err := rolemodels.Load(projectRoot)
	if err != nil {
		return false
	}
	roleCLIs, err := agentlaunch.DefaultRoleCLIs(roles, resolver, config, roleModels)
	return err == nil && agentlaunch.AllValidationLocal(roleCLIs, config)
}

// TaskContextSections extends a role's configured context sections with the
// decomposition-root variant when the task's role pair is a root. Exported so
// the role budget can render the same variants the compositor does.
func TaskContextSections(base []string, task *models.Task, data *prompts.RoleContextData, resolver *pipeline.Resolver) ([]string, error) {
	sections := append([]string(nil), base...)
	if task.RolePair == "" || (data.RoleType != string(roles.RoleTypeDoer) && data.RoleType != string(roles.RoleTypeReviewer)) {
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
	if data.RoleType == string(roles.RoleTypeDoer) {
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

func savePrompt(promptDir, agentID, prompt string) (string, error) {
	return saveTimestampedFile(promptDir, agentID, "txt", prompt)
}
