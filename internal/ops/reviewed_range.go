package ops

import (
	"fmt"
	"slices"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// PlanReviewedRange retains one revision's own reviewed attribution. Amendments
// add ranges; they never rewrite the original historical task's range.
type PlanReviewedRange struct {
	TaskID, BaseCommit, ReviewCommit, MergeCommit string
}

// EffectivePlanReview selects only the latest applied revision. Pending and
// quarantined corrections cannot become current authority.
func EffectivePlanReview(state *models.State, parent *models.Task) (*models.Task, error) {
	revisions, err := planReviewHistory(state, parent)
	if err != nil {
		return nil, err
	}
	return revisions[len(revisions)-1], nil
}

// PlanReviewedRanges includes merged tracked predecessors through the latest
// applied correction, including quarantined work that the fresh review retained.
// It excludes draft and future pending work. Legacy unattributed parents keep
// their existing scalar-reference-only context.
func PlanReviewedRanges(state *models.State, parent *models.Task) ([]PlanReviewedRange, error) {
	revisions, err := planReviewHistory(state, parent)
	if err != nil {
		return nil, err
	}
	var ranges []PlanReviewedRange
	for _, revision := range revisions {
		base, review, present, err := MergedReviewedRange(revision)
		if err != nil {
			return nil, err
		}
		if present {
			ranges = append(ranges, PlanReviewedRange{revision.ID, base, review, *revision.MergeCommit})
		}
	}
	return ranges, nil
}

func planReviewHistory(state *models.State, parent *models.Task) ([]*models.Task, error) {
	if parent == nil {
		return nil, fmt.Errorf("planning parent is missing")
	}
	if parent.AmendsPlan != "" {
		return nil, fmt.Errorf("correction %s cannot allocate as a planning parent", parent.ID)
	}
	revisions := []*models.Task{parent}
	amendment := parent.PlanAmendment
	if amendment == nil {
		return revisions, nil
	}
	invalid := func() ([]*models.Task, error) {
		return nil, fmt.Errorf("task %s has invalid amendment review lineage", parent.ID)
	}
	if state == nil || len(amendment.OriginalOutput) == 0 {
		return invalid()
	}
	positions := map[string]int{}
	for i, id := range amendment.Corrections {
		correction := state.FindTask(id)
		if id == parent.ID || correction == nil || correction.AmendsPlan != parent.ID || correction.RolePair != parent.RolePair || correction.PlanAmendment != nil || !correction.AmendmentMode.IsValid() {
			return invalid()
		}
		if _, duplicate := positions[id]; duplicate {
			return invalid()
		}
		positions[id] = i
	}
	last := -1
	for _, id := range amendment.Applied {
		position, found := positions[id]
		if !found || position <= last || slices.Contains(amendment.Quarantined, id) || id == amendment.Pending || state.FindTask(id).Status != models.TaskStatusMerged {
			return invalid()
		}
		last = position
	}
	seen := map[string]bool{}
	for _, id := range amendment.Quarantined {
		if _, found := positions[id]; !found || seen[id] || id == amendment.Pending {
			return invalid()
		}
		seen[id] = true
	}
	if amendment.Pending != "" {
		if position, found := positions[amendment.Pending]; !found || position != len(amendment.Corrections)-1 {
			return invalid()
		}
	}
	// The original manifest, not the currently adopted output, proves what
	// the original review allocated.
	original := *parent
	original.Output = amendment.OriginalOutput
	revisions[0] = &original
	adoptedOutput := original.Output
	for _, id := range amendment.Corrections[:last+1] {
		correction := state.FindTask(id)
		if !slices.Contains(amendment.Applied, id) && !slices.Contains(amendment.Quarantined, id) {
			return invalid()
		}
		if correction.Status == models.TaskStatusMerged {
			if slices.Contains(amendment.Applied, id) {
				if models.ValidateAmendmentOutputForMode(adoptedOutput, correction.Output, correction.AmendmentMode) != nil {
					return invalid()
				}
				adoptedOutput = correction.Output
			}
			if !recordedIndependentPlanReview(correction) {
				return invalid()
			}
			revisions = append(revisions, correction)
		} else if correction.Status != models.TaskStatusAbandoned {
			return invalid()
		}
	}
	return revisions, nil
}

// ValidatePlanReviewHistory verifies immutable evidence and integration
// ancestry for every effective amendment predecessor. Callers capture HEAD
// once and use that same commit when building their context or allocation.
func ValidatePlanReviewHistory(root string, state *models.State, parent *models.Task, integrationCommit string) error {
	revisions, err := planReviewHistory(state, parent)
	if err != nil {
		return err
	}
	return validatePlanReviewAncestry(git.New(root), revisions, integrationCommit)
}

// ValidatePlanAmendmentArtifacts refuses adoption after a relevant reviewed
// section drifted on integration. Unrelated sections of an anchored artifact
// remain independent; approved-proof references retain the acceptance fence.
func ValidatePlanAmendmentArtifacts(root string, state *models.State, parent *models.Task, integrationCommit string) error {
	latest, err := EffectivePlanReview(state, parent)
	if err != nil {
		return err
	}
	if latest.ReviewCommit == nil {
		return fmt.Errorf("task %s has no effective reviewed commit", parent.ID)
	}
	g := git.New(root)
	seen := map[string]bool{}
	checkArtifact := func(ref, label string) error {
		if ref == "" || seen[ref] {
			return nil
		}
		seen[ref] = true
		path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
		var reviewedContent string
		for _, commit := range []string{*latest.ReviewCommit, integrationCommit} {
			entry, present, readErr := g.TreeEntryAt(commit, path)
			content, _, readErr := readAcceptanceEntry(g, path, entry, present, readErr)
			if readErr != nil {
				return fmt.Errorf("amendment %s artifact %q: %w", label, ref, readErr)
			}
			span, ok := carrierSpan(content, heading)
			if !ok {
				return fmt.Errorf("amendment %s artifact %q has no unique reviewed section", label, ref)
			}
			if commit == *latest.ReviewCommit {
				reviewedContent = span
			} else if span != reviewedContent {
				return fmt.Errorf("amendment %s artifact %q drifted after correction review; obtain a fresh reviewed correction", label, ref)
			}
		}
		return nil
	}
	// Bounded modes may edit original scalar refs even when no output names
	// them. Validate the same mutable authority at current integration before
	// releasing pending, rather than checking only its reviewed candidate.
	if latest.AmendmentMode != "" {
		if err := checkArtifact(parent.ArchRef, "arch_ref"); err != nil {
			return err
		}
		if latest.AmendmentMode == models.PlanAmendmentPreserveIdentity && parent.EffectiveType() != models.TaskTypeArchitecture {
			if err := checkArtifact(parent.PlanRef, "plan_ref"); err != nil {
				return err
			}
		}
	}
	for index, output := range latest.Output {
		for _, ref := range []string{output.SpecRef, output.PlanRef, output.ArchRef, output.EpicRef} {
			if err := checkArtifact(ref, fmt.Sprintf("output[%d]", index)); err != nil {
				return err
			}
		}
		ref := output.PlanRef
		if ref == "" {
			ref = output.SpecRef
		}
		path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
		if referencecontract.ValidateAcceptancePath(path) != nil {
			continue
		}
		content, _, err := readAcceptanceBlob(root, *latest.ReviewCommit, path)
		if err != nil {
			return err
		}
		contract, err := referencecontract.ParseAcceptance(content, heading)
		if err != nil {
			return err
		}
		if contract == nil {
			continue
		}
		if alike, _ := compareReviewedReferences(state, parent.ID, root, *latest.ReviewCommit, integrationCommit, path, heading, contract); !alike {
			return fmt.Errorf("amendment output[%d] approved proof references drifted after review", index)
		}
	}
	return nil
}

func validatePlanReviewAncestry(g *git.Git, revisions []*models.Task, integrationCommit string) error {
	for i, revision := range revisions {
		if !independentlyApprovedPlan(revision) || !validAcceptanceParentHistory(g, revision) {
			return fmt.Errorf("task %s lacks independent immutable planning review evidence", revision.ID)
		}
		ancestor, err := g.IsAncestor(*revision.MergeCommit, integrationCommit)
		if err != nil || !ancestor {
			return fmt.Errorf("task %s reviewed merge is absent from integration ancestry", revision.ID)
		}
		if i > 0 {
			ancestor, err := g.IsAncestor(*revisions[i-1].MergeCommit, *revision.BaseCommit)
			if err != nil || !ancestor {
				return fmt.Errorf("task %s amendment review precedes its predecessor's merge", revision.ID)
			}
		}
	}
	return nil
}

func recordedIndependentPlanReview(task *models.Task) bool {
	if !independentlyApprovedPlan(task) || !models.IsFullReviewCommit(*task.BaseCommit) || !models.IsFullReviewCommit(*task.ReviewCommit) || !models.IsFullReviewCommit(*task.MergeCommit) {
		return false
	}
	author := acceptanceParentAuthor(task)
	var submitted, approved, merged bool
	for _, entry := range task.History {
		if entry.Agent == nil || entry.Commit == nil {
			continue
		}
		switch entry.Event {
		case models.TaskEventSubmittedForReview:
			submitted = submitted || (*entry.Commit == *task.ReviewCommit && *entry.Agent == author)
		case models.TaskEventApproved:
			approved = approved || (*entry.Commit == *task.ReviewCommit && *entry.Agent != author &&
				((task.ApprovedBy != nil && *entry.Agent == *task.ApprovedBy) || task.HasApprovalFromAgent(*entry.Agent)))
		case models.TaskEventMerged:
			merged = merged || *entry.Commit == *task.MergeCommit
		}
	}
	return submitted && approved && merged
}

// planEvidenceRecords covers all referenced records, including missing ones,
// for race digests. A future correction is not authority, but changing it must
// invalidate an observation that read its crosslink.
func planEvidenceRecords(state *models.State, parentIDs []string) []*models.Task {
	var records []*models.Task
	for _, id := range parentIDs {
		parent := state.FindTask(id)
		records = append(records, parent)
		if parent != nil && parent.PlanAmendment != nil {
			for _, correctionID := range parent.PlanAmendment.Corrections {
				records = append(records, state.FindTask(correctionID))
			}
		}
	}
	return records
}

// MergedReviewedRange classifies the retained review attribution of a merged task.
// Older tasks with no attribution remain on the legacy path; partial attribution
// is corrupt and fails closed.
func MergedReviewedRange(task *models.Task) (base, review string, present bool, err error) {
	if task == nil {
		return "", "", false, fmt.Errorf("merged task is nil")
	}
	merge := derefNonEmpty(task.MergeCommit)
	base = derefNonEmpty(task.BaseCommit)
	review = derefNonEmpty(task.ReviewCommit)
	if merge == "" && base == "" && review == "" {
		return "", "", false, nil
	}
	if merge == "" || base == "" || review == "" {
		return "", "", false, fmt.Errorf("merged task %q lacks reviewed change attribution", task.ID)
	}
	return base, review, true, nil
}

func derefNonEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
