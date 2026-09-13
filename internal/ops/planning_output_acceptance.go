package ops

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// validatePlanningOutputAcceptance checks future coding declarations in the
// planner's committed candidate, without adopting parent authority or requiring
// the manifests, proof files, or runners that those coding tasks will create.
func validatePlanningOutputAcceptance(root string, task *models.Task, commit string) error {
	if task.EffectiveType() != models.TaskTypePlanning {
		return nil
	}
	g := git.New(root)
	for i, output := range task.Output {
		ref := output.PlanRef
		if ref == "" {
			ref = output.SpecRef
		}
		path, heading, _ := strings.Cut(ref, "#")
		if referencecontract.ValidateAcceptancePath(path) != nil {
			continue // Legacy refs retain their existing artifact validation route.
		}
		fail := func(reason string) error {
			return acceptanceError(task.ID, fmt.Sprintf("output[%d].acceptance", i), reason)
		}
		_, present, err := g.TreePathMode(commit, path)
		if err != nil {
			return fail("cannot inspect candidate source")
		}
		if !present {
			continue // Match legacy admission when no committed source is adopted.
		}
		content, _, err := readAcceptanceBlob(root, commit, path)
		if err != nil {
			return fail(err.Error())
		}
		contract, err := referencecontract.ParseAcceptance(content, heading)
		if err != nil {
			return fail(err.Error())
		}
		if contract != nil && !slices.Equal(contract.Validation, output.Validation) {
			return fail("declaration must equal output's ordered validation commands")
		}
	}
	return nil
}

// checkPlanningOutputSnapshot prevents publishing an allocation other than the
// one whose candidate declarations were checked outside the state transaction.
func checkPlanningOutputSnapshot(expected, current *models.Task) error {
	if expected.EffectiveType() != models.TaskTypePlanning && current.EffectiveType() != models.TaskTypePlanning {
		return nil
	}
	if expected.EffectiveType() != current.EffectiveType() || !reflect.DeepEqual(expected.Output, current.Output) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s: planning output changed during submission", expected.ID)}
	}
	return nil
}
