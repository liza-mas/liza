package ops

import (
	"reflect"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// parentAllocationReview returns current reviewed authority, or the unchanged
// historical authority already adopted by this task. Adopted evidence must keep
// exactly the same resolved proofs; it cannot follow a new recovery grant.
//
// For a fresh allocation the proof comparison is left to the caller, because the two callers
// do different things with the same answer: acceptance refuses, and
// re-affirmation decides. Sharing everything up to that point is what keeps a
// recovery grant bound to the parent that actually allocates the task —
// selecting "the first merged parent" instead would let a multi-parent child be
// re-affirmed against a parent that never allocated it, leaving the real one
// unauthorized and the task still refused.
func parentAllocationReview(g *git.Git, root string, state *models.State, task, parent *models.Task, path, heading, integrationSpan, integrationCommit string) *models.Task {
	if parent == nil || (parent.EffectiveType() != models.TaskTypePlanning && (parent.EffectiveType() != models.TaskTypeArchitecture || !models.DirectCodingAllocation(parent.Output))) || parent.Status != models.TaskStatusMerged || !manifestAllocatesTask(parent.Output, task) {
		return nil
	}
	revisions, err := planReviewHistory(state, parent)
	if err != nil || validatePlanReviewAncestry(g, revisions, integrationCommit) != nil {
		return nil
	}
	latest := revisions[len(revisions)-1]
	if !manifestAllocatesTask(latest.Output, task) {
		return nil
	}
	// The latest applied review governs current content and proofs. The origin
	// may be an earlier reviewed revision that authored this exact allocation;
	// each comparison uses that revision's own manifest and Git range.
	reviewedSpan, reviewedOK := acceptanceCarrierSpan(root, *latest.ReviewCommit, path, heading)
	if !reviewedOK || reviewedSpan != integrationSpan {
		return nil
	}
	// A started child keeps its adopted immutable receipt authority only when
	// that exact independently reviewed revision still covers the unchanged
	// allocation and its resolved proofs. Never rewrite evidence to follow a
	// later parent review merely because the selector stayed in position.
	if adopted := task.AcceptanceSource; adopted != nil && adopted.ParentTask == parent.ID {
		for _, revision := range revisions {
			if revision.ReviewCommit == nil || *revision.ReviewCommit != adopted.ParentReviewCommit || !manifestAllocatesTask(revision.Output, task) {
				continue
			}
			span, ok := acceptanceCarrierSpan(root, *revision.ReviewCommit, path, heading)
			if !ok || span != reviewedSpan || adopted.Blob != spanObjectID(span) {
				return nil
			}
			content, _, err := readAcceptanceBlob(root, integrationCommit, path)
			if err != nil {
				return nil
			}
			contract, err := referencecontract.ParseAcceptance(content, heading)
			if err != nil {
				return nil
			}
			// Adoption may already include an authorized proof reaffirmation.
			// Its immutable integration commit is the no-change baseline; the
			// caller still checks parent review authority with the real grants.
			// A grant created later cannot authorize drift since adoption here.
			if alike, _ := compareReviewedReferences(&models.State{}, parent.ID, root, adopted.Commit, integrationCommit, path, heading, contract); !alike {
				return nil
			}
			return revision
		}
		return nil
	}
	for _, origin := range revisions {
		if !manifestAllocatesTask(origin.Output, task) {
			continue
		}
		span, ok := acceptanceCarrierSpan(root, *origin.ReviewCommit, path, heading)
		if !ok || span != reviewedSpan {
			continue
		}
		baseSpan, _ := acceptanceCarrierSpan(root, *origin.BaseCommit, path, heading)
		if baseSpan != span {
			return latest
		}
	}
	return nil
}

func independentlyApprovedPlan(parent *models.Task) bool {
	if (parent.EffectiveType() != models.TaskTypePlanning && parent.EffectiveType() != models.TaskTypeArchitecture) || parent.Status != models.TaskStatusMerged || parent.BaseCommit == nil || parent.ReviewCommit == nil || parent.MergeCommit == nil {
		return false
	}
	author := acceptanceParentAuthor(parent)
	if author == "" {
		return false
	}
	approved := parent.ApprovedBy != nil && *parent.ApprovedBy != "" && *parent.ApprovedBy != author
	for _, approval := range parent.Approvals {
		approved = approved || (approval.Agent != "" && approval.Agent != author)
	}
	return approved
}

func manifestAllocatesTask(output []models.OutputEntry, task *models.Task) bool {
	for _, output := range output {
		if output.PlanRef == task.PlanRef && output.SpecRef == task.SpecRef && reflect.DeepEqual(output.Validation, task.Validation) && output.DestructiveDB == task.DestructiveDB &&
			models.RuntimeInputsEqual(output.RuntimeInputs, task.RuntimeInputs) {
			return true
		}
	}
	return false
}

// acceptanceAllocationRef is the reference acceptance allocates against:
// plan_ref, otherwise spec_ref. Any other order silently changes which carrier
// a task is judged by.
func acceptanceAllocationRef(task *models.Task) string {
	if task.PlanRef != "" {
		return task.PlanRef
	}
	return task.SpecRef
}

// AcceptanceAllocationRef exposes the same selection to prompt construction,
// which must judge approved proofs against the allocation acceptance uses.
func AcceptanceAllocationRef(task *models.Task) string {
	return acceptanceAllocationRef(task)
}

// Ownership can be released after submission without invalidating its review.
// Only submissions of this exact reviewed commit can recover the author; stale
// submissions, claim history and conflicting identities cannot establish it.
func acceptanceParentAuthor(parent *models.Task) string {
	var author string
	if parent.AssignedTo != nil {
		author = *parent.AssignedTo
	}
	for _, entry := range parent.History {
		if entry.Event != models.TaskEventSubmittedForReview || entry.Commit == nil || *entry.Commit != *parent.ReviewCommit {
			continue
		}
		if entry.Agent == nil || *entry.Agent == "" || (author != "" && author != *entry.Agent) {
			return ""
		}
		author = *entry.Agent
	}
	return author
}

// Parent metadata must identify the actual immutable reviewed history, not a
// movable ref or an unrelated commit that happens to contain the same blob.
func validAcceptanceParentHistory(g *git.Git, parent *models.Task) bool {
	commits := []string{*parent.BaseCommit, *parent.ReviewCommit, *parent.MergeCommit}
	resolved, err := g.ResolveCommits(commits)
	if err != nil {
		return false
	}
	for i, commit := range commits {
		if resolved[i].Err != nil || resolved[i].OID != commit {
			return false
		}
	}
	for _, pair := range [][2]string{
		{*parent.BaseCommit, *parent.ReviewCommit},
		{*parent.ReviewCommit, *parent.MergeCommit},
	} {
		ancestor, err := g.IsAncestor(pair[0], pair[1])
		if err != nil || !ancestor {
			return false
		}
	}
	return true
}

// reviewedReferencesResolveAlike reports whether every reference the reviewed
// contract depends on still resolves to the content the reviewer approved.
//
// A reference is a declared ID plus a target: a path, a heading, and an
// optional pinned revision. The contract names obligations and approved proofs
// by ID only, so re-pointing an ID at different content — a different path,
// heading, or revision — substitutes what an approval covers while leaving
// every ID intact. The allocation span cannot see this, because Source
// References is a sibling section outside it.
//
// Only references the contract actually depends on are compared: those backing
// its obligations, and those its approved proofs cite. An unrelated reference
// may be re-pinned freely, which is the repin workflow this check must not
// break.
func reviewedReferencesResolveAlike(state *models.State, parentTask, root, reviewCommit, integrationCommit, path, heading string, contract *referencecontract.AcceptanceContract) bool {
	alike, _ := compareReviewedReferences(state, parentTask, root, reviewCommit, integrationCommit, path, heading, contract)
	return alike
}

// compareReviewedReferences also identifies an unreaffirmed proof drift for
// the refusal diagnostic, using only the existing comparison's observations.
// Unresolvable references refuse without advertising a drift remedy.
func compareReviewedReferences(state *models.State, parentTask, root, reviewCommit, integrationCommit, path, heading string, contract *referencecontract.AcceptanceContract) (bool, string) {
	if contract == nil {
		return true, ""
	}
	// Common case: the carrier file is byte-identical, so nothing it declares
	// can have moved. This keeps the reference walk off the hot path, which
	// runs at submit, recheck and reviewer assignment.
	g := git.New(root)
	if sameBlobAt(g, reviewCommit, integrationCommit, path) {
		return true, ""
	}

	// Scope: the references the contract asserts a proof against, and no more.
	//
	// Widening this to every reference backing the contract's obligations is
	// the stricter reading, and it is the one that matches what approval means
	// — but it makes a re-pin that points an obligation at legitimately
	// extended content indistinguishable from a substitution, and blocks the
	// whole plan's children with no supported way to re-review a merged plan.
	// That tension is real, and the resolution is not to block here but to
	// report: RecordObligationContentDrift runs at merge and records one
	// reviewable anomaly per changed section. This boundary stays a gate for
	// approved proofs only.
	//
	// What still observes obligation drift, precisely: reference_context.go
	// compares a reference's section at its pinned revision against the same
	// path and heading at integration HEAD, so it sees a pin left behind while
	// its target moved — and, under a merged carrier, discloses it in the
	// prompt rather than refusing, except for the task's own approved proofs.
	// The merge-time record reports the same case as "stale". Neither catches
	// a reference repointed at a different target whose content agrees between
	// its new pin and HEAD, because both sides move together; the merge-time
	// record's repin/retarget walk surfaces that substitution instead.
	//
	// An approved proof stays compared by content here, so the substitution
	// this check exists to stop — repointing an approved reference at material
	// no reviewer saw — still fails.
	needed := map[string]bool{}
	for _, proof := range contract.ApprovedProofs {
		needed[proof.ReferenceID] = true
	}
	if len(needed) == 0 {
		// Deliberate, not incidental: with nothing to compare, the parent's
		// Source References is not parsed here either. A reviewed carrier that
		// no longer parses is caught at integration by ParseAcceptance above;
		// this boundary stops asserting anything about the reviewed side when
		// the contract asserts no proof against it.
		return true, ""
	}

	reviewedRefs, ok := carrierReferences(root, reviewCommit, path)
	if !ok {
		return false, ""
	}
	integrationRefs, ok := carrierReferences(root, integrationCommit, path)
	if !ok {
		return false, ""
	}

	for referenceID := range needed {
		reviewed, reviewedFound := resolveDeclaredReference(root, reviewedRefs, referenceID)
		current, currentFound := resolveDeclaredReference(root, integrationRefs, referenceID)
		if !reviewedFound || !currentFound {
			return false, ""
		}
		if reviewed == current {
			continue
		}
		// The content moved. Refusing here is right — this boundary cannot
		// tell a legitimate extension from a substitution — but an authorized
		// re-affirmation of this exact transition is a decision that it was
		// the former, recorded with an actor and a reason. It covers one
		// transition only: identities are compared on both sides, so a later
		// change to the same section refuses again.
		if state == nil ||
			state.FindProofReaffirmation(parentTask, path, heading, referenceID, spanObjectID(reviewed), spanObjectID(current)) == nil {
			return false, referenceID
		}
	}
	return true, ""
}

// carrierReferences parses a carrier's Source References at one commit.
func carrierReferences(root, commit, path string) (*referencecontract.Contract, bool) {
	content, _, err := readAcceptanceBlob(root, commit, path)
	if err != nil {
		return nil, false
	}
	refs, err := referencecontract.Parse(strings.ReplaceAll(content, "\r\n", "\n"))
	if err != nil || refs == nil {
		return nil, false
	}
	return refs, true
}

// resolveDeclaredReference returns the section text a declared reference points
// at, following its effective revision.
func resolveDeclaredReference(root string, refs *referencecontract.Contract, referenceID string) (string, bool) {
	return resolveDeclaredReferenceCached(nil, root, refs, referenceID)
}

// blobCache memoizes referenced file content by revision and path within one
// pass. A nil cache reads every time, which is what the acceptance boundary
// wants: it resolves a handful of references and must not hold stale content
// across calls. A caller comparing many references that cite the same file at
// the same revision passes a cache and pays one read instead of one per
// reference. Ceiling: each entry is at most ReferenceFileMaxBytes, so a pass
// over many distinct large files holds them all until it ends.
type blobCache map[string]string

func (c blobCache) read(root, revision, path string) (string, bool) {
	key := revision + "\x00" + path
	if c != nil {
		if content, ok := c[key]; ok {
			return content, true
		}
	}
	content, _, err := readReferenceBlob(root, revision, path)
	if err != nil {
		return "", false
	}
	if c != nil {
		c[key] = content
	}
	return content, true
}

func resolveDeclaredReferenceCached(cache blobCache, root string, refs *referencecontract.Contract, referenceID string) (string, bool) {
	for _, direct := range refs.DirectReferences {
		if direct.ID != referenceID {
			continue
		}
		content, ok := cache.read(root, direct.EffectiveRevision(refs.SourceRevision), direct.Path)
		if !ok {
			return "", false
		}
		section, err := referenceSection(content, direct.Heading)
		if err != nil {
			return "", false
		}
		return section, true
	}
	return "", false
}
