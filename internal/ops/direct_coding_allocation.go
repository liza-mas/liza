package ops

import (
	"fmt"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// validateDirectCodingAllocation admits only an explicitly configured coding
// route. A marker in a legacy frozen pipeline cannot silently skip its planner.
func validateDirectCodingAllocation(resolver *pipeline.Resolver, task *models.Task, output []models.OutputEntry) error {
	if err := models.ValidateCodingAllocationOutput(task, output); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if !models.HasCodingAllocation(output) {
		return nil
	}
	consumers, err := resolver.OutputConsumerRolePairsForOutput(task.RolePair, output)
	if err != nil {
		return err
	}
	if len(consumers) == 0 {
		return &PreconditionError{Reason: "coding_allocation requires a configured direct coding route"}
	}
	for _, consumer := range consumers {
		role, err := resolver.DoerRole(consumer)
		if err != nil || models.TaskTypeForRole(role) != models.TaskTypeCoding {
			return &PreconditionError{Reason: fmt.Sprintf("coding_allocation consumer %q must generate coding tasks", consumer)}
		}
	}
	return nil
}

// validateDirectCodingGeneration checks committed contracts and independent
// parent review outside the state lock, using the same authority as claim.
type directCodingGenerationCheck struct {
	commit      string
	branch      string
	parentFence string
	err         error
}

func validateDirectCodingGeneration(root string, state *models.State, task *models.Task) directCodingGenerationCheck {
	check := directCodingGenerationCheck{}
	if task == nil || !models.HasCodingAllocation(task.Output) {
		return check
	}
	check.branch = state.Config.IntegrationBranch
	commit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		check.err = err
		return check
	}
	check.commit = commit
	if err := validatePlanningOutputAcceptance(root, task, commit); err != nil {
		check.err = err
		return check
	}
	for i, entry := range task.Output {
		child := models.Task{
			ID: fmt.Sprintf("%s-code-%d", task.ID, i), Type: models.TaskTypeCoding,
			ParentTasks: []string{task.ID}, PlanRef: entry.PlanRef, SpecRef: entry.SpecRef,
			Validation: entry.Validation, RuntimeInputs: entry.RuntimeInputs, DestructiveDB: entry.DestructiveDB,
		}
		accepted := checkCreatedTaskAcceptance(root, state, &child)
		check.parentFence = accepted.parentFence
		if err := accepted.lifecycleError("proceed", task); err != nil {
			check.err = err
			return check
		}
		if accepted.commit != check.commit {
			check.err = fmt.Errorf("integration changed while checking direct coding allocation")
			return check
		}
	}
	return check
}

func (check directCodingGenerationCheck) validateState(state *models.State, task *models.Task) error {
	if check.err != nil {
		return check.err
	}
	if check.branch != state.Config.IntegrationBranch {
		return fmt.Errorf("integration branch changed during direct coding generation")
	}
	if task == nil || check.parentFence == "" {
		return fmt.Errorf("direct coding allocation was not checked before generation")
	}
	fence, err := acceptanceParentFence(state, &models.Task{ParentTasks: []string{task.ID}})
	if err != nil {
		return err
	}
	if fence != check.parentFence {
		return fmt.Errorf("task %s reviewed allocation evidence changed during generation", task.ID)
	}
	return nil
}

// Called under the completion lock and before Modify: cooperating ref writers
// cannot move integration between equality and publication of the children.
func verifyDirectCodingIntegration(root, branch string, checks map[string]directCodingGenerationCheck) error {
	var head string
	err := withIntegrationMutationLock(root, "verify direct coding allocation integration", func() error {
		var err error
		head, err = git.New(root).GetCommitSHA(branch)
		return err
	})
	if err != nil {
		return err
	}
	for _, check := range checks {
		if check.err == nil && check.commit != head {
			return fmt.Errorf("integration changed after checking direct coding allocation")
		}
	}
	return nil
}
