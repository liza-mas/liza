package ops

import (
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// refuseBoundedContract is the one refusal every bounded-contract comparison
// reports through.
func refuseBoundedContract(reason string) error {
	return &PreconditionError{Reason: "bounded contract amendment: " + reason}
}

// allowedContractSections collects, per file, the headings the original's refs
// let a correction edit. Identity-preserving corrections of a non-architecture
// original may also edit the plan sections.
func allowedContractSections(original *models.Task, mode models.PlanAmendmentMode, architecture bool) map[string][]string {
	allowed := map[string][]string{}
	add := func(ref string) {
		if ref != "" {
			path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
			allowed[path] = append(allowed[path], heading)
		}
	}
	editsPlan := mode == models.PlanAmendmentPreserveIdentity && !architecture
	add(original.ArchRef)
	if editsPlan {
		add(original.PlanRef)
	}
	for _, output := range original.Output {
		add(output.ArchRef)
		if editsPlan {
			add(output.PlanRef)
		}
	}
	return allowed
}

// compareReviewedAcceptance holds each output's acceptance allocation and
// approved proofs to the previous effective review: the candidate may not
// change the carrier span or the resolved proof content.
func compareReviewedAcceptance(root string, original *models.Task, previousReview, candidate string) error {
	for _, output := range original.Output {
		ref := output.PlanRef
		if ref == "" {
			ref = output.SpecRef
		}
		path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
		if referencecontract.ValidateAcceptancePath(path) != nil {
			continue
		}
		before, _, err := readAcceptanceBlob(root, previousReview, path)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		contract, err := referencecontract.ParseAcceptance(before, heading)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		if contract == nil {
			continue
		}
		after, _, err := readAcceptanceBlob(root, candidate, path)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		oldSpan, ok := carrierSpan(before, heading)
		newSpan, newOK := carrierSpan(after, heading)
		if !ok || !newOK || oldSpan != newSpan {
			return refuseBoundedContract("acceptance allocation changed: " + ref)
		}
		if alike, _ := compareReviewedReferences(&models.State{}, original.ID, root, previousReview, candidate, path, heading, contract); !alike {
			return refuseBoundedContract("approved proof content changed: " + ref)
		}
	}
	return nil
}

// compareChangedContractFiles compares the candidate with the correction's own
// base: every changed file must be an allowed path with an unchanged mode and
// unchanged text outside its referenced sections.
func compareChangedContractFiles(root string, correction *models.Task, candidate string, allowed map[string][]string, contractOnly bool) error {
	g := git.New(root)
	changed, err := g.DiffFiles(root, *correction.BaseCommit, candidate)
	if err != nil {
		return err
	}
	for _, path := range changed {
		headings, ok := allowed[path]
		if !ok {
			return refuseBoundedContract("candidate changes a file outside referenced contract prose: " + path)
		}
		before, _, err := readAcceptanceBlob(root, *correction.BaseCommit, path)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		after, _, err := readAcceptanceBlob(root, candidate, path)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		oldEntry, _, err := g.TreeEntryAt(*correction.BaseCommit, path)
		if err != nil {
			return err
		}
		newEntry, _, err := g.TreeEntryAt(candidate, path)
		if err != nil {
			return err
		}
		if oldEntry.Mode != newEntry.Mode {
			return refuseBoundedContract("contract file mode changed: " + path)
		}
		oldOutside, err := contractOutsideSections(before, headings, contractOnly)
		if err != nil {
			return refuseBoundedContract(err.Error())
		}
		newOutside, err := contractOutsideSections(after, headings, contractOnly)
		if err != nil || oldOutside != newOutside {
			return refuseBoundedContract("candidate changes text outside referenced contract sections: " + path)
		}
	}
	return nil
}
