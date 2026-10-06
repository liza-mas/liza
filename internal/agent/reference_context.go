package agent

import (
	"fmt"
	"strings"

	gitpkg "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/referencecontract"
)

type referenceContextRepository = referencecontract.ReviewRepository

func buildResolvedReferenceContext(task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, error) {
	repo := gitpkg.New(config.ProjectRoot)
	context, _, err := buildReferenceContextWithRepository(repo, task, state, config, roleType, false)
	return context, err
}

func buildResolvedReferenceContextWithRepository(repo referenceContextRepository, task *models.Task, state *models.State, config SupervisorConfig, roleType string) (string, error) {
	context, _, err := buildReferenceContextWithRepository(repo, task, state, config, roleType, false)
	return context, err
}

func buildReferenceContext(task *models.Task, state *models.State, config SupervisorConfig, roleType string, decompositionRoot bool) (string, []prompts.LegacyArtifactReference, error) {
	repo := gitpkg.New(config.ProjectRoot)
	return buildReferenceContextWithRepository(repo, task, state, config, roleType, decompositionRoot)
}

func buildReferenceContextWithRepository(repo referenceContextRepository, task *models.Task, state *models.State, config SupervisorConfig, roleType string, decompositionRoot bool) (string, []prompts.LegacyArtifactReference, error) {
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
	// carriers are assigned, with the decomposition-root exception below.
	// When the assigned artifact also arrives
	// through a parent range, that observation wins the path and is assigned
	// anyway, so the scalar route is the fallback for a parent that is not
	// merged or has no reviewed range.
	//
	// Scalar refs naming one path read one blob at the captured HEAD, so they
	// form one observation. It narrows to a fragment only when every ref on
	// the path names that fragment: a fragment-less ref assigns the whole file,
	// and narrowing to one of two fragments would drop the other.
	assignedIndex, assignedRank := -1, -1
	scalarIndex := make(map[string]int)
	agreedFragment := make(map[string]string) // "" renders the whole file
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
			fragment := paths.SplitRefFragment(scalar.ref)
			index, seen := scalarIndex[observation.Path]
			if !seen {
				index = len(observations)
				scalarIndex[observation.Path], agreedFragment[observation.Path] = index, fragment
				observation.ElideRefs = true
				observations = append(observations, observation)
			} else if agreedFragment[observation.Path] != fragment {
				agreedFragment[observation.Path] = ""
			}
			if scalar.rank > assignedRank {
				assignedIndex, assignedRank = index, scalar.rank
			}
			continue
		}
		legacyReferences = append(legacyReferences, prompts.LegacyArtifactReference{
			Field: scalar.field,
			Ref:   scalar.ref,
			File:  paths.SplitRefFile(scalar.ref),
		})
	}
	for path, index := range scalarIndex {
		if fragment := agreedFragment[path]; fragment != "" {
			// loadScalarCarrier already resolved this fragment in the same blob.
			span, err := referencecontract.ExtractSection(observations[index].Span, fragment)
			if err != nil {
				return "", nil, fmt.Errorf("select strict scalar carrier %q: %w", path+"#"+fragment, err)
			}
			observations[index].Span = span
		}
	}
	assignedHeading := ""
	if assignedIndex >= 0 {
		observations[assignedIndex].ElideRefs = false
		assignedHeading = agreedFragment[observations[assignedIndex].Path]
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
		found, discoverErr := referencecontract.LoadDiffCarriers(repo, config.ProjectRoot, base, review, head, referencecontract.CarrierParent, true, proofs)
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
		found, discoverErr := referencecontract.LoadDiffCarriers(repo, config.ProjectRoot, *task.BaseCommit, *task.ReviewCommit, head, referencecontract.CarrierReview, false, proofs)
		if discoverErr != nil {
			return "", nil, discoverErr
		}
		observations = append(observations, found...)
	}

	// A decomposition root needs its parent's child carrier bodies, but their
	// declared references are second-hop context. Classify only after all
	// observations have been validated. The assigned scalar path retains full
	// references even when a parent observation wins it; current-review master
	// declarations likewise retain full authority and once-only emission.
	if decompositionRoot {
		for index := range observations {
			if observations[index].Class == referencecontract.CarrierParent &&
				(assignedIndex < 0 || observations[index].Path != observations[assignedIndex].Path) {
				observations[index].ElideRefs = true
			}
		}
	}

	// The assigned ref's fragment narrows whichever observation of that path
	// wins: a parent range that rediscovers the same artifact would otherwise
	// inline the whole file and drop the section the task was pointed at.
	presentationNotice := ""
	if assignedIndex >= 0 && assignedHeading != "" {
		assignedPath := observations[assignedIndex].Path
		winner := observations[assignedIndex]
		for index := range observations {
			if observations[index].Path == assignedPath {
				observations[index].AssignedHeading = assignedHeading
				if observations[index].Class > winner.Class {
					winner = observations[index]
				}
			}
		}
		if roleType == "doer" && (config.Role == models.RoleArchitect || config.Role == models.RoleCodePlanner) {
			ids, present, err := referencecontract.SectionReferenceIDs(winner.Span, assignedHeading)
			if err != nil {
				// Historical merged declarations cannot be repaired at launch;
				// retain full context rather than guessing an empty read set.
				presentationNotice = fmt.Sprintf("SECTION READ SET NOTICE: %v; legacy full context retained.\n", err)
			} else if present {
				selected := make(map[string]bool, len(ids))
				known := make(map[string]bool, len(winner.Refs))
				for _, ref := range winner.Refs {
					known[ref.ID] = true
				}
				var unknown []string
				for _, id := range ids {
					selected[id] = true
					if !known[id] {
						unknown = append(unknown, id)
					}
				}
				if len(unknown) > 0 {
					presentationNotice = fmt.Sprintf("SECTION READ SET NOTICE: undeclared IDs %q; inspect the pinned carrier's Source References.\n", strings.Join(unknown, ", "))
				}
				for index := range observations {
					if observations[index].Path == assignedPath {
						observations[index].SectionOnly = true
						observations[index].InlineReferenceIDs = selected
						observations[index].ElideRefs = false
					} else {
						observations[index].PointerOnly = true
						observations[index].ElideRefs = true
					}
				}
			}
		}
	}

	context, err := referencecontract.RenderCarriers(observations)
	if err != nil {
		return "", nil, err
	}
	context = presentationNotice + context
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
func allocationProofPolicy(repo referenceContextRepository, task *models.Task) referencecontract.ProofPolicy {
	allocation := ops.AcceptanceAllocationRef(task)
	allocationPath, heading := paths.SplitRefFile(allocation), paths.SplitRefFragment(allocation)
	return func(path string, contents ...string) func(string) bool {
		if allocationPath == "" || path != allocationPath {
			return referencecontract.RefuseNoDrift
		}
		if source := task.AcceptanceSource; source != nil && source.ParentReviewCommit != "" && paths.SplitRefFile(source.Ref) == allocationPath {
			adopted, err := repo.ReadBlob(source.ParentReviewCommit, allocationPath)
			if err != nil {
				return referencecontract.RefuseEveryDrift
			}
			contents = append(contents, adopted)
		}
		proofs := map[string]bool{}
		for _, content := range contents {
			ids, err := referencecontract.ApprovedProofReferenceIDs(content, heading)
			if err != nil {
				return referencecontract.RefuseEveryDrift
			}
			for id := range ids {
				proofs[id] = true
			}
		}
		return func(referenceID string) bool { return proofs[referenceID] }
	}
}

func loadScalarCarrier(repo referenceContextRepository, head, ref string, proofs referencecontract.ProofPolicy) (referencecontract.Carrier, bool, error) {
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
	// The caller narrows the span once it knows every scalar ref on this path.
	if fragment != "" {
		if _, err := referencecontract.ExtractSection(content, fragment); err != nil {
			return referencecontract.Carrier{}, false, fmt.Errorf("select strict scalar carrier %q: %w", ref, err)
		}
	}
	oid, err := repo.BlobOID(head, path)
	if err != nil {
		return referencecontract.Carrier{}, false, err
	}
	refs, err := referencecontract.ResolveDirectReferences(repo, head, "", "", contract, proofs(path, content))
	if err != nil {
		return referencecontract.Carrier{}, false, fmt.Errorf("scalar carrier %q: %w", path, err)
	}
	return referencecontract.Carrier{Path: path, Span: content, Revision: head, Class: referencecontract.CarrierScalar, BlobOID: oid, Refs: refs}, true, nil
}
