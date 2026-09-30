package ops

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// validateOutputRefFragments resolves each output ref fragment the way child
// prompts will (ADR-0133). A fragment into a strict carrier present at the
// submitted commit must select exactly one eligible heading; rejecting it here
// keeps an unresolvable anchor, typically a slug, from blocking every child
// after merge.
func validateOutputRefFragments(root string, task *models.Task, commit string) error {
	g := git.New(root)
	for i, output := range task.Output {
		for _, ref := range []struct {
			field string
			value string
		}{
			{field: "spec_ref", value: output.SpecRef},
			{field: "epic_ref", value: output.EpicRef},
			{field: "plan_ref", value: output.PlanRef},
			{field: "arch_ref", value: output.ArchRef},
		} {
			if err := ResolveRefFragmentAt(g, commit, ref.value); err != nil {
				reason := err.Error()
				var headingErr *referencecontract.HeadingMatchError
				if errors.As(err, &headingErr) {
					reason += "; the fragment must be the exact heading text, not a slug"
				}
				return &PreconditionError{Reason: fmt.Sprintf("task %s: output[%d].%s %q: %s", task.ID, i, ref.field, ref.value, reason)}
			}
		}
	}
	return nil
}

// validateReviewCarriers builds the reviewed range base..review the way the
// reviewer's prompt context will (ADR-0133): every strict carrier the range
// changed must parse and resolve its direct references against head. Content
// the author can correct is refused as input; a Git failure stays operational,
// since editing the carrier would not fix it. Integration moving after
// submission can still fail the reviewer's build, which remains authoritative.
func validateReviewCarriers(repo referencecontract.ReviewRepository, root, taskID, base, review, head string) error {
	// A current-review range never consults drift proofs, so none is supplied.
	_, err := referencecontract.LoadDiffCarriers(repo, root, base, review, head, referencecontract.CarrierReview, false, nil)
	if err == nil {
		return nil
	}
	var invalid *referencecontract.InvalidCarrierError
	if errors.As(err, &invalid) {
		return &PreconditionError{Reason: fmt.Sprintf("task %s: reviewed carriers: %v; fix the carrier and resubmit", taskID, err)}
	}
	return &OperationalError{
		Code:    "git_operation",
		Phase:   "review-carriers",
		Message: "failed to build the reviewed carriers",
		Details: map[string]any{
			"operation":     integrationOperationSubmitForReview,
			"task_id":       taskID,
			"recovery_hint": "Inspect the repository objects for the reviewed range, then retry submit-for-review.",
		},
		Err: err,
	}
}

// ResolveRefFragmentAt resolves ref's fragment at commit the way prompt context
// does. Refs without a fragment, and refs whose file is absent at commit, keep
// the legacy route and resolve trivially.
func ResolveRefFragmentAt(g *git.Git, commit, ref string) error {
	fragment := paths.SplitRefFragment(ref)
	if fragment == "" {
		return nil
	}
	path := paths.SplitRefFile(ref)
	_, present, err := g.TreePathMode(commit, path)
	if err != nil {
		return fmt.Errorf("cannot inspect %s: %w", path, err)
	}
	if !present {
		return nil
	}
	content, err := g.ReadBlob(commit, path)
	if err != nil {
		return err
	}
	return referencecontract.ResolveScalarFragment(content, fragment)
}

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
