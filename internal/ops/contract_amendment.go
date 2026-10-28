package ops

import (
	"fmt"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// validateBoundedContractCorrection checks the real candidate tree at both
// submission and adoption. Keeping an envelope unchanged alone is insufficient:
// an allocation or accepted proof can change inside a referenced Markdown file.
func validateBoundedContractCorrection(root string, state *models.State, original, correction *models.Task, candidate string) error {
	if correction.AmendmentMode == "" {
		return nil
	}
	if original == nil || correction.BaseCommit == nil {
		return refuseBoundedContract("missing original or candidate base")
	}
	if err := models.ValidateAmendmentOutputForMode(original.Output, correction.Output, correction.AmendmentMode); err != nil {
		return err
	}
	previous, err := EffectivePlanReview(state, original)
	if err != nil || previous.ReviewCommit == nil {
		return refuseBoundedContract("missing prior effective reviewed authority")
	}
	if err := ValidatePlanReviewHistory(root, state, original, candidate); err != nil {
		return err
	}
	architecture := original.EffectiveType() == models.TaskTypeArchitecture
	allowed := allowedContractSections(original, correction.AmendmentMode, architecture)
	if err := compareReviewedAcceptance(root, original, *previous.ReviewCommit, candidate); err != nil {
		return err
	}
	return compareChangedContractFiles(root, correction, candidate, allowed, architecture || correction.AmendmentMode == models.PlanAmendmentContract)
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
