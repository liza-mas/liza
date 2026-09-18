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
		observation, strict, loadErr := loadScalarCarrier(repo, head, scalar.ref)
		if loadErr != nil {
			return "", nil, loadErr
		}
		if strict {
			observation.ElideRefs = true
			if scalar.rank > assignedRank {
				assignedIndex, assignedRank = len(observations), scalar.rank
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
		found, discoverErr := loadDiffCarriers(repo, config.ProjectRoot, base, review, head, referencecontract.CarrierParent, true)
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
		found, discoverErr := loadDiffCarriers(repo, config.ProjectRoot, *task.BaseCommit, *task.ReviewCommit, head, referencecontract.CarrierReview, false)
		if discoverErr != nil {
			return "", nil, discoverErr
		}
		observations = append(observations, found...)
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

func loadScalarCarrier(repo referenceContextRepository, head, ref string) (referencecontract.Carrier, bool, error) {
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
	refs, err := resolveDeclaredReferences(repo, head, "", "", contract)
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("scalar carrier %q: %w", path, err)
	}
	return referencecontract.Carrier{Path: path, Span: span, Revision: head, Class: referencecontract.CarrierScalar, BlobOID: oid, Refs: refs}, true, nil
}

func loadDiffCarriers(repo referenceContextRepository, root, base, review, head string, class referencecontract.CarrierClass, requireFresh bool) ([]referencecontract.Carrier, error) {
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
		if requireFresh {
			headOID, headErr := repo.BlobOID(head, path)
			if headErr != nil {
				return nil, fmt.Errorf("freshness check %q: %w", path, headErr)
			}
			if oid != headOID {
				return nil, fmt.Errorf("reviewed carrier %q is stale at integration HEAD", path)
			}
		}
		// Current-review carriers may declare references to paths their own
		// reviewed range introduced; those resolve at the review commit.
		localBase, localReview := "", ""
		if !requireFresh {
			localBase, localReview = base, review
		}
		refs, resolveErr := resolveDeclaredReferences(repo, head, localBase, localReview, contract)
		if resolveErr != nil {
			return nil, fmt.Errorf("reviewed carrier %q: %w", path, resolveErr)
		}
		observations = append(observations, referencecontract.Carrier{Path: path, Span: content, Revision: review, Class: class, BlobOID: oid, Refs: refs})
	}
	return observations, nil
}

// resolveDeclaredReferences pins each direct reference at its declared revision
// and requires blob identity at integration HEAD. When localBase/localReview
// are non-empty, a path absent at both HEAD and localBase was introduced in
// the reviewed range and matches at localReview instead; a path present at
// localBase but absent at HEAD was deleted and still blocks.
func resolveDeclaredReferences(repo referenceContextRepository, head, localBase, localReview string, contract *referencecontract.Contract) ([]referencecontract.Reference, error) {
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
		freshOID, err := repo.BlobOID(freshRevision, ref.Path)
		if err != nil {
			return nil, err
		}
		if pinnedOID != freshOID {
			return nil, fmt.Errorf("direct reference %q is stale at %s", ref.ID, where)
		}
		span, err := referencecontract.ExtractSection(content, ref.Heading)
		if err != nil {
			return nil, err
		}
		refs = append(refs, referencecontract.Reference{Path: ref.Path, Heading: ref.Heading, Revision: revision, BlobOID: pinnedOID, Span: span})
	}
	return refs, nil
}
