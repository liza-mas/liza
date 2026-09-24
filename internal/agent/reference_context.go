package agent

import (
	"fmt"
	"path/filepath"
	"sort"

	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/referencecontract"
)

type referenceContextRepository interface {
	referencecontract.Repository
	DiffFiles(dir, commitA, commitB string) ([]string, error)
	TreePathMode(treeish, path string) (mode string, present bool, err error)
}

func buildResolvedReferenceContext(task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, error) {
	repo := gitpkg.New(config.ProjectRoot)
	context, _, err := buildReferenceContextWithRepository(repo, task, state, config, roleType)
	return context, err
}

func buildResolvedReferenceContextWithRepository(repo referenceContextRepository, task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, error) {
	context, _, err := buildReferenceContextWithRepository(repo, task, state, config, roleType)
	return context, err
}

func buildReferenceContext(task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, []prompts.LegacyArtifactReference, error) {
	repo := gitpkg.New(config.ProjectRoot)
	return buildReferenceContextWithRepository(repo, task, state, config, roleType)
}

func buildReferenceContextWithRepository(repo referenceContextRepository, task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, []prompts.LegacyArtifactReference, error) {
	var head string
	resolveHead := func() (string, error) {
		if head != "" {
			return head, nil
		}
		resolved, err := repo.ResolveCommit(state.Config.IntegrationBranch)
		if err != nil {
			return "", fmt.Errorf("capture integration HEAD %q: %w", state.Config.IntegrationBranch, err)
		}
		head = resolved
		return head, nil
	}

	proofs := allocationProofPolicy(repo, task)

	var observations []referencecontract.Carrier
	var legacyReferences []prompts.LegacyArtifactReference
	// The most specific strict scalar carrier is the task's assigned artifact
	// and keeps its declared references inlined; the other scalar carriers
	// are ancestors whose references render as pointers. Parent and review
	// carriers are always assigned. When the assigned artifact also arrives
	// through a parent range, that observation wins the path and is assigned
	// anyway, so the scalar route is the fallback for a parent that is not
	// merged or has no reviewed range.
	assignedIndex, assignedRank := -1, -1
	assignedHeading := ""
	for _, scalar := range []struct {
		field string
		ref   string
		rank  int // specificity: spec < epic < arch < plan
	}{
		{field: "spec_ref", ref: task.SpecRef, rank: 0},
		{field: "epic_ref", ref: task.EpicRef, rank: 1},
		{field: "plan_ref", ref: task.PlanRef, rank: 3},
		{field: "arch_ref", ref: task.ArchRef, rank: 2},
	} {
		if scalar.ref == "" {
			continue
		}
		head, err := resolveHead()
		if err != nil {
			return "", nil, err
		}
		observation, strict, loadErr := loadScalarCarrier(repo, head, scalar.ref, proofs)
		if loadErr != nil {
			return "", nil, loadErr
		}
		if strict {
			observation.ElideRefs = true
			if scalar.rank > assignedRank {
				assignedIndex, assignedRank = len(observations), scalar.rank
				assignedHeading = paths.SplitRefFragment(scalar.ref)
			}
			observations = append(observations, observation)
			continue
		}
		legacyReferences = append(legacyReferences, prompts.LegacyArtifactReference{
			Field: scalar.field,
			Ref:   scalar.ref,
			File:  paths.SplitRefFile(scalar.ref),
		})
	}
	if assignedIndex >= 0 {
		observations[assignedIndex].ElideRefs = false
	}

	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if parent == nil {
			return "", nil, fmt.Errorf("direct parent %q not found", parentID)
		}
		if parent.Status != models.TaskStatusMerged {
			continue
		}
		base, review, present, rangeErr := ops.MergedReviewedRange(parent)
		if rangeErr != nil {
			return "", nil, rangeErr
		}
		if !present {
			continue
		}
		head, err := resolveHead()
		if err != nil {
			return "", nil, err
		}
		found, discoverErr := loadDiffCarriers(repo, config.ProjectRoot, base, review, head, referencecontract.CarrierParent, true, proofs)
		if discoverErr != nil {
			return "", nil, fmt.Errorf("direct parent %q: %w", parentID, discoverErr)
		}
		observations = append(observations, found...)
	}

	if roleType == "reviewer" {
		if task.BaseCommit == nil || *task.BaseCommit == "" || task.ReviewCommit == nil || *task.ReviewCommit == "" {
			return "", nil, fmt.Errorf("review task %q lacks BaseCommit..ReviewCommit attribution", task.ID)
		}
		head, err := resolveHead()
		if err != nil {
			return "", nil, err
		}
		found, discoverErr := loadDiffCarriers(repo, config.ProjectRoot, *task.BaseCommit, *task.ReviewCommit, head, referencecontract.CarrierReview, false, proofs)
		if discoverErr != nil {
			return "", nil, discoverErr
		}
		observations = append(observations, found...)
	}

	// The assigned ref's fragment narrows whichever observation of that path
	// wins: a parent range that rediscovers the same artifact would otherwise
	// inline the whole file and drop the section the task was pointed at.
	if assignedIndex >= 0 && assignedHeading != "" {
		assignedPath := observations[assignedIndex].Path
		for index := range observations {
			if observations[index].Path == assignedPath {
				observations[index].AssignedHeading = assignedHeading
			}
		}
	}

	context, err := referencecontract.RenderCarriers(observations)
	if err != nil {
		return "", nil, err
	}
	strictCarrierPaths := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		strictCarrierPaths[observation.Path] = struct{}{}
	}
	filteredLegacy := legacyReferences[:0]
	for _, legacy := range legacyReferences {
		if _, suppliedByStrictCarrier := strictCarrierPaths[legacy.File]; !suppliedByStrictCarrier {
			filteredLegacy = append(filteredLegacy, legacy)
		}
	}
	return context, filteredLegacy, nil
}

// proofPolicy answers, for a merged carrier observed at path with the given
// contents, which declared reference IDs still refuse section drift under
// their pin.
type proofPolicy func(path string, contents ...string) func(referenceID string) bool

func refuseEveryDrift(string) bool { return true }

func refuseNoDrift(string) bool { return false }

// allocationProofPolicy keeps drift refusing only for the approved proofs of
// the task's own allocation — the one class acceptance gates by content
// (ADR-0133). Everything else a merged carrier pins is disclosed instead,
// because its pin cannot move without a new merge and refusing strands every
// child of the plan (D76).
//
// Proof IDs are unioned over every version of the allocation the task could
// be judged by — the observed content, the parent's reviewed content, and the
// adopted source — so a later edit that drops a proof never relaxes it. A
// declaration that cannot be read or parsed refuses every drift: it cannot say
// which references it meant to prove.
func allocationProofPolicy(repo referenceContextRepository, task *models.Task) proofPolicy {
	allocation := ops.AcceptanceAllocationRef(task)
	allocationPath, heading := paths.SplitRefFile(allocation), paths.SplitRefFragment(allocation)
	return func(path string, contents ...string) func(string) bool {
		if allocationPath == "" || path != allocationPath {
			return refuseNoDrift
		}
		if source := task.AcceptanceSource; source != nil && source.ParentReviewCommit != "" && paths.SplitRefFile(source.Ref) == allocationPath {
			adopted, err := repo.ReadBlob(source.ParentReviewCommit, allocationPath)
			if err != nil {
				return refuseEveryDrift
			}
			contents = append(contents, adopted)
		}
		proofs := map[string]bool{}
		for _, content := range contents {
			ids, err := referencecontract.ApprovedProofReferenceIDs(content, heading)
			if err != nil {
				return refuseEveryDrift
			}
			for id := range ids {
				proofs[id] = true
			}
		}
		return func(referenceID string) bool { return proofs[referenceID] }
	}
}

func loadScalarCarrier(repo referenceContextRepository, head, ref string, proofs proofPolicy) (referencecontract.Carrier, bool, error) {
	path := paths.SplitRefFile(ref)
	fragment := paths.SplitRefFragment(ref)
	_, present, err := repo.TreePathMode(head, path)
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("inspect scalar carrier %q at integration HEAD: %w", path, err)
	}
	if !present {
		// Existing scalar refs may exist only in the task worktree. Without a
		// HEAD object there is no strict marker to enforce, so preserve the
		// legacy worktree-first route.
		return referencecontract.Carrier{}, false, nil
	}
	content, err := repo.ReadBlob(head, path)
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("read scalar carrier %q: %w", path, err)
	}
	contract, err := referencecontract.Parse(content)
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("parse scalar carrier %q: %w", path, err)
	}
	if contract == nil {
		return referencecontract.Carrier{}, false, nil
	}
	span := content
	if fragment != "" {
		span, err = referencecontract.ExtractSection(content, fragment)
		if err != nil {
			return referencecontract.Carrier{}, false, fmt.Errorf("select strict scalar carrier %q: %w", ref, err)
		}
	}
	oid, err := repo.BlobOID(head, path)
	if err != nil {
		return referencecontract.Carrier{}, false, err
	}
	refs, err := resolveDeclaredReferences(repo, head, "", "", contract, proofs(path, content))
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("scalar carrier %q: %w", path, err)
	}
	return referencecontract.Carrier{Path: path, Span: span, Revision: head, Class: referencecontract.CarrierScalar, BlobOID: oid, Refs: refs}, true, nil
}

func loadDiffCarriers(repo referenceContextRepository, root, base, review, head string, class referencecontract.CarrierClass, requireFresh bool, proofs proofPolicy) ([]referencecontract.Carrier, error) {
	changed, err := repo.DiffFiles(root, base, review)
	if err != nil {
		return nil, fmt.Errorf("enumerate reviewed range %s..%s: %w", base, review, err)
	}
	sort.Strings(changed)
	var observations []referencecontract.Carrier
	for _, path := range changed {
		if filepath.Ext(path) != ".md" {
			continue
		}
		mode, present, modeErr := repo.TreePathMode(review, path)
		if modeErr != nil {
			return nil, modeErr
		}
		if !present || (mode != "100644" && mode != "100755") {
			continue
		}
		content, readErr := repo.ReadBlob(review, path)
		if readErr != nil {
			return nil, readErr
		}
		contract, parseErr := referencecontract.Parse(content)
		if parseErr != nil {
			return nil, fmt.Errorf("parse reviewed carrier %q: %w", path, parseErr)
		}
		if contract == nil {
			continue
		}
		oid, oidErr := repo.BlobOID(review, path)
		if oidErr != nil {
			return nil, oidErr
		}
		revision := review
		// A current-review carrier is unmerged: its author can still re-pin, so
		// every drift under it refuses. A merged parent's pins cannot move.
		refuses := refuseEveryDrift
		if requireFresh {
			reviewedContent := content
			// A merged parent's carrier can be edited by later merges. The
			// integrated version is the current agreed content, so adopt it
			// rather than refusing to build context; only deletion blocks.
			_, presentAtHead, headErr := repo.TreePathMode(head, path)
			if headErr != nil {
				return nil, fmt.Errorf("freshness check %q: %w", path, headErr)
			}
			if !presentAtHead {
				return nil, fmt.Errorf("reviewed carrier %q was deleted at integration HEAD", path)
			}
			headOID, headErr := repo.BlobOID(head, path)
			if headErr != nil {
				return nil, fmt.Errorf("freshness check %q: %w", path, headErr)
			}
			if oid != headOID {
				headContent, readHeadErr := repo.ReadBlob(head, path)
				if readHeadErr != nil {
					return nil, readHeadErr
				}
				headContract, parseHeadErr := referencecontract.Parse(headContent)
				if parseHeadErr != nil {
					return nil, fmt.Errorf("parse reviewed carrier %q at integration HEAD: %w", path, parseHeadErr)
				}
				if headContract == nil {
					return nil, fmt.Errorf("reviewed carrier %q lost its Source References at integration HEAD", path)
				}
				content, contract, oid, revision = headContent, headContract, headOID, head
			}
			refuses = proofs(path, reviewedContent, content)
		}
		// Current-review carriers may declare references to paths their own
		// reviewed range introduced; those resolve at the review commit.
		localBase, localReview := "", ""
		if !requireFresh {
			localBase, localReview = base, review
		}
		refs, resolveErr := resolveDeclaredReferences(repo, head, localBase, localReview, contract, refuses)
		if resolveErr != nil {
			return nil, fmt.Errorf("reviewed carrier %q: %w", path, resolveErr)
		}
		observations = append(observations, referencecontract.Carrier{Path: path, Span: content, Revision: revision, Class: class, BlobOID: oid, Refs: refs})
	}
	return observations, nil
}

// resolveDeclaredReferences pins each direct reference at its declared revision
// and compares the referenced section with integration HEAD (blob identity is
// the fast path; an unrelated edit elsewhere in the file is not drift). When
// localBase/localReview are non-empty, a path absent at both HEAD and
// localBase was introduced in the reviewed range and matches at localReview
// instead; a path present at localBase but absent at HEAD was deleted.
//
// Drift refuses when refuses(referenceID) holds. Otherwise the reference is
// kept with its drift disclosed (ADR-0133): the current section when it still
// resolves, the pinned one with the reason when it does not. A pin that cannot
// be read refuses regardless — there is nothing to disclose.
func resolveDeclaredReferences(repo referenceContextRepository, head, localBase, localReview string, contract *referencecontract.Contract, refuses func(referenceID string) bool) ([]referencecontract.Reference, error) {
	refs := make([]referencecontract.Reference, 0, len(contract.DirectReferences))
	for _, ref := range contract.DirectReferences {
		revision, err := repo.ResolveCommit(ref.EffectiveRevision(contract.SourceRevision))
		if err != nil {
			return nil, err
		}
		content, err := repo.ReadBlob(revision, ref.Path)
		if err != nil {
			return nil, err
		}
		pinnedOID, err := repo.BlobOID(revision, ref.Path)
		if err != nil {
			return nil, err
		}
		pinned := referencecontract.Reference{Path: ref.Path, Heading: ref.Heading, Revision: revision, BlobOID: pinnedOID}
		// unresolvable keeps the pinned text when the section cannot be read
		// at the fresh revision, or refuses with err.
		unresolvable := func(reason string, err error) error {
			if refuses(ref.ID) {
				return err
			}
			pinned.PinnedRevision, pinned.Unresolved = revision, reason
			refs = append(refs, pinned)
			return nil
		}
		_, presentAtHead, err := repo.TreePathMode(head, ref.Path)
		if err != nil {
			return nil, err
		}
		freshRevision, where := head, "integration HEAD"
		if !presentAtHead && localReview != "" {
			_, presentAtBase, baseErr := repo.TreePathMode(localBase, ref.Path)
			if baseErr != nil {
				return nil, baseErr
			}
			if presentAtBase {
				return nil, fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID)
			}
			freshRevision, where = localReview, "review commit"
		}
		pinned.Span, err = referencecontract.ExtractSection(content, ref.Heading)
		if err != nil {
			return nil, err
		}
		if !presentAtHead && localReview == "" {
			if err := unresolvable("path deleted", fmt.Errorf("direct reference %q was deleted at integration HEAD", ref.ID)); err != nil {
				return nil, err
			}
			continue
		}
		freshOID, err := repo.BlobOID(freshRevision, ref.Path)
		if err != nil {
			return nil, err
		}
		if pinnedOID != freshOID {
			freshContent, readErr := repo.ReadBlob(freshRevision, ref.Path)
			if readErr != nil {
				return nil, readErr
			}
			freshSpan, spanErr := referencecontract.ExtractSection(freshContent, ref.Heading)
			if spanErr != nil {
				if err := unresolvable(spanErr.Error(), fmt.Errorf("direct reference %q is stale at %s: %w", ref.ID, where, spanErr)); err != nil {
					return nil, err
				}
				continue
			}
			if freshSpan != pinned.Span {
				if refuses(ref.ID) {
					return nil, fmt.Errorf("direct reference %q is stale at %s", ref.ID, where)
				}
				refs = append(refs, referencecontract.Reference{
					Path: ref.Path, Heading: ref.Heading, Revision: freshRevision, BlobOID: freshOID,
					Span: freshSpan, PinnedRevision: revision,
				})
				continue
			}
		}
		refs = append(refs, pinned)
	}
	return refs, nil
}
