package ops

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// validateBoundedContractCorrection checks the real candidate tree at both
// submission and adoption. Keeping an envelope unchanged alone is insufficient:
// an allocation or accepted proof can change inside a referenced Markdown file.
func validateBoundedContractCorrection(root string, state *models.State, original, correction *models.Task, candidate string) error {
	if correction.AmendmentMode == "" {
		return nil
	}
	refuse := func(reason string) error { return &PreconditionError{Reason: "bounded contract amendment: " + reason} }
	if original == nil || correction.BaseCommit == nil {
		return refuse("missing original or candidate base")
	}
	if err := models.ValidateAmendmentOutputForMode(original.Output, correction.Output, correction.AmendmentMode); err != nil {
		return err
	}
	previous, err := EffectivePlanReview(state, original)
	if err != nil || previous.ReviewCommit == nil {
		return refuse("missing prior effective reviewed authority")
	}
	if err := ValidatePlanReviewHistory(root, state, original, candidate); err != nil {
		return err
	}
	architecture := original.EffectiveType() == models.TaskTypeArchitecture
	allowed := map[string][]string{}
	add := func(ref string) {
		if ref != "" {
			path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
			allowed[path] = append(allowed[path], heading)
		}
	}
	add(original.ArchRef)
	if correction.AmendmentMode == models.PlanAmendmentPreserveIdentity && !architecture {
		add(original.PlanRef)
	}
	for _, output := range original.Output {
		add(output.ArchRef)
		if correction.AmendmentMode == models.PlanAmendmentPreserveIdentity && !architecture {
			add(output.PlanRef)
		}
		ref := output.PlanRef
		if ref == "" {
			ref = output.SpecRef
		}
		path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
		if referencecontract.ValidateAcceptancePath(path) != nil {
			continue
		}
		before, _, err := readAcceptanceBlob(root, *previous.ReviewCommit, path)
		if err != nil {
			return refuse(err.Error())
		}
		contract, err := referencecontract.ParseAcceptance(before, heading)
		if err != nil {
			return refuse(err.Error())
		}
		if contract == nil {
			continue
		}
		after, _, err := readAcceptanceBlob(root, candidate, path)
		if err != nil {
			return refuse(err.Error())
		}
		oldSpan, ok := carrierSpan(before, heading)
		newSpan, newOK := carrierSpan(after, heading)
		if !ok || !newOK || oldSpan != newSpan {
			return refuse("acceptance allocation changed: " + ref)
		}
		if alike, _ := compareReviewedReferences(&models.State{}, original.ID, root, *previous.ReviewCommit, candidate, path, heading, contract); !alike {
			return refuse("approved proof content changed: " + ref)
		}
	}
	g := git.New(root)
	changed, err := g.DiffFiles(root, *correction.BaseCommit, candidate)
	if err != nil {
		return err
	}
	for _, path := range changed {
		headings, ok := allowed[path]
		if !ok {
			return refuse("candidate changes a file outside referenced contract prose: " + path)
		}
		before, _, err := readAcceptanceBlob(root, *correction.BaseCommit, path)
		if err != nil {
			return refuse(err.Error())
		}
		after, _, err := readAcceptanceBlob(root, candidate, path)
		if err != nil {
			return refuse(err.Error())
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
			return refuse("contract file mode changed: " + path)
		}
		oldOutside, err := contractOutsideSections(before, headings, architecture || correction.AmendmentMode == models.PlanAmendmentContract)
		if err != nil {
			return refuse(err.Error())
		}
		newOutside, err := contractOutsideSections(after, headings, architecture || correction.AmendmentMode == models.PlanAmendmentContract)
		if err != nil || oldOutside != newOutside {
			return refuse("candidate changes text outside referenced contract sections: " + path)
		}
	}
	return nil
}

// Architecture allows only the exact CONTRACT subsection inside an anchored
// Scope. Scope ownership, direct references, boundaries and acceptance stay
// immutable even when the output envelope happens to be unchanged.
func contractOutsideSections(content string, headings []string, contractOnly bool) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	var spans []string
	replacements := map[string]string{}
	for _, heading := range headings {
		if heading == "" {
			if contractOnly {
				return "", fmt.Errorf("architecture contract correction requires an exact Scope heading")
			}
			return "", nil
		}
		span, ok := carrierSpan(content, heading)
		if contractOnly && ok {
			contract, found := carrierSpan(span, "CONTRACT")
			ok = found
			if found {
				replacements[span] = strings.Replace(span, contract, "", 1)
			}
		}
		if !ok || strings.Count(content, span) != 1 {
			return "", fmt.Errorf("contract section %q is missing or ambiguous", heading)
		}
		spans = append(spans, span)
	}
	sort.Slice(spans, func(i, j int) bool { return len(spans[i]) > len(spans[j]) })
	for _, span := range spans {
		content = strings.Replace(content, span, replacements[span], 1)
	}
	return content, nil
}
