package ops

import (
	"fmt"
	"sort"

	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
)

// GlobalIntegrationTaskChange is one merged goal task's reviewed change.
type GlobalIntegrationTaskChange struct {
	TaskID       string
	Description  string
	BaseCommit   string
	ReviewCommit string
	Paths        []string
}

// GlobalIntegrationPlanSurface is the goal-owned change attributed to one
// contributing plan: its merged lineage leaves plus its slice repairs.
type GlobalIntegrationPlanSurface struct {
	PlanTaskID         string
	Tasks              []GlobalIntegrationTaskChange
	PathCount          int
	InterfacesOwned    []string
	InterfacesConsumed []string
}

// GlobalIntegrationSeam is a path or declared interface shared by at least two
// contributing plans.
type GlobalIntegrationSeam struct {
	Name        string
	PlanTaskIDs []string
}

// GlobalIntegrationSurface is the goal-owned review surface of one global
// analysis generation. It is derived from reviewed task ranges, never from the
// integration branch history, so commits that reached the branch through other
// work are absent.
type GlobalIntegrationSurface struct {
	Plans          []GlobalIntegrationPlanSurface
	PriorRepairs   []GlobalIntegrationTaskChange
	SeamPaths      []GlobalIntegrationSeam
	SeamInterfaces []GlobalIntegrationSeam
}

// BuildGlobalIntegrationSurface attributes the goal's reviewed changes to the
// frozen contributing plans and derives the cross-plan seams. Only plans are
// seam identities: prior global repairs are listed for navigation but never
// make a path shared on their own.
func BuildGlobalIntegrationSurface(state *models.State, projectRoot string, generation int) (GlobalIntegrationSurface, error) {
	if state == nil || state.Goal.Integration == nil || state.Goal.Integration.ContributingSet == nil {
		return GlobalIntegrationSurface{}, fmt.Errorf("global integration surface has no frozen contributing set")
	}
	evaluator, err := newIntegrationProgressEvaluator(state)
	if err != nil {
		return GlobalIntegrationSurface{}, err
	}
	gitWrapper := gitpkg.New(projectRoot)

	surface := GlobalIntegrationSurface{}
	pathPlans := make(map[string][]string)
	interfacePlans := make(map[string][]string)
	scopes := append([]models.IntegrationScopeSnapshot(nil), state.Goal.Integration.ContributingSet.Scopes...)
	sort.Slice(scopes, func(i, j int) bool { return scopes[i].PlanTaskID < scopes[j].PlanTaskID })
	for _, scope := range scopes {
		taskIDs := make([]string, 0)
		for _, rootID := range uniqueSortedStrings(scope.RootTaskIDs) {
			leaves, leafErr := evaluator.mergedLineageLeaves(rootID)
			if leafErr != nil {
				return GlobalIntegrationSurface{}, leafErr
			}
			taskIDs = append(taskIDs, leaves...)
		}
		repairs, repairErr := evaluator.mergedRepairLeaves(sliceAnalysisKey(scope.PlanTaskID))
		if repairErr != nil {
			return GlobalIntegrationSurface{}, repairErr
		}
		taskIDs = append(taskIDs, repairs...)
		changes, changeErr := globalTaskChanges(state, uniqueSortedStrings(taskIDs), projectRoot, gitWrapper)
		if changeErr != nil {
			return GlobalIntegrationSurface{}, fmt.Errorf("plan %q: %w", scope.PlanTaskID, changeErr)
		}

		plan := GlobalIntegrationPlanSurface{PlanTaskID: scope.PlanTaskID, Tasks: changes}
		planPaths := make([]string, 0)
		for _, change := range changes {
			planPaths = append(planPaths, change.Paths...)
			if manifest := state.FindTask(change.TaskID).Decomposition; manifest != nil {
				plan.InterfacesOwned = append(plan.InterfacesOwned, manifest.InterfacesOwned...)
				plan.InterfacesConsumed = append(plan.InterfacesConsumed, manifest.InterfacesConsumed...)
			}
		}
		planPaths = uniqueSortedStrings(planPaths)
		plan.PathCount = len(planPaths)
		plan.InterfacesOwned = uniqueSortedStrings(plan.InterfacesOwned)
		plan.InterfacesConsumed = uniqueSortedStrings(plan.InterfacesConsumed)
		for _, path := range planPaths {
			pathPlans[path] = append(pathPlans[path], scope.PlanTaskID)
		}
		for _, name := range uniqueSortedStrings(append(append([]string(nil), plan.InterfacesOwned...), plan.InterfacesConsumed...)) {
			interfacePlans[name] = append(interfacePlans[name], scope.PlanTaskID)
		}
		surface.Plans = append(surface.Plans, plan)
	}

	for prior := 1; prior < generation; prior++ {
		repairs, repairErr := evaluator.mergedRepairLeaves(globalAnalysisKey(prior))
		if repairErr != nil {
			return GlobalIntegrationSurface{}, repairErr
		}
		changes, changeErr := globalTaskChanges(state, repairs, projectRoot, gitWrapper)
		if changeErr != nil {
			return GlobalIntegrationSurface{}, fmt.Errorf("global generation %d repairs: %w", prior, changeErr)
		}
		surface.PriorRepairs = append(surface.PriorRepairs, changes...)
	}

	surface.SeamPaths = sharedAcrossPlans(pathPlans)
	surface.SeamInterfaces = sharedAcrossPlans(interfacePlans)
	return surface, nil
}

// mergedRepairLeaves returns the merged repair leaves created by the analysis
// with the given key, or none when that analysis does not exist.
func (e *integrationProgressEvaluator) mergedRepairLeaves(analysisKey string) ([]string, error) {
	analysis := e.analysisKeys[analysisKey]
	if analysis == nil {
		return nil, nil
	}
	children := make([]string, 0)
	for _, childID := range e.children[analysis.ID] {
		if e.tasks[childID].IntegrationAnalysis == nil {
			children = append(children, childID)
		}
	}
	if len(children) == 0 {
		return nil, nil
	}
	leaves, err := e.repairLeaves(uniqueSortedStrings(children))
	if err != nil {
		return nil, err
	}
	merged := make([]string, 0, len(leaves))
	for _, leafID := range leaves {
		if e.tasks[leafID].Status == models.TaskStatusMerged {
			merged = append(merged, leafID)
		}
	}
	return merged, nil
}

func globalTaskChanges(state *models.State, taskIDs []string, projectRoot string, gitWrapper *gitpkg.Git) ([]GlobalIntegrationTaskChange, error) {
	changes := make([]GlobalIntegrationTaskChange, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		task := state.FindTask(taskID)
		baseCommit, reviewCommit, present, err := MergedReviewedRange(task)
		if err != nil || !present {
			return nil, fmt.Errorf("merged goal task %q lacks reviewed change attribution", taskID)
		}
		paths, err := gitWrapper.DiffFiles(projectRoot, baseCommit, reviewCommit)
		if err != nil {
			return nil, fmt.Errorf("diff goal task %q review range: %w", taskID, err)
		}
		changes = append(changes, GlobalIntegrationTaskChange{
			TaskID:       taskID,
			Description:  task.Description,
			BaseCommit:   baseCommit,
			ReviewCommit: reviewCommit,
			Paths:        uniqueSortedStrings(paths),
		})
	}
	return changes, nil
}

func sharedAcrossPlans(owners map[string][]string) []GlobalIntegrationSeam {
	seams := make([]GlobalIntegrationSeam, 0)
	for name, planIDs := range owners {
		planIDs = uniqueSortedStrings(planIDs)
		if len(planIDs) >= 2 {
			seams = append(seams, GlobalIntegrationSeam{Name: name, PlanTaskIDs: planIDs})
		}
	}
	sort.Slice(seams, func(i, j int) bool { return seams[i].Name < seams[j].Name })
	return seams
}
