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

// Scalar ref specificity: the highest rank among the strict carriers is the
// task's assigned artifact.
const (
	referenceRankSpec = iota
	referenceRankEpic
	referenceRankArch
	referenceRankPlan
)

// referenceHead captures the integration HEAD lazily, once per build, so every
// collection stage reads the same snapshot and a build with no strict ref
// never touches the repository.
type referenceHead struct {
	repo   referenceContextRepository
	branch string
	value  string
}

func (h *referenceHead) resolve() (string, error) {
	if h.value != "" {
		return h.value, nil
	}
	resolved, err := h.repo.ResolveCommit(h.branch)
	if err != nil {
		return "", fmt.Errorf("capture integration HEAD %q: %w", h.branch, err)
	}
	h.value = resolved
	return h.value, nil
}

// scalarCarriers is the outcome of walking the task's scalar refs: the strict
// carriers, the refs that stay legacy worktree-first references, and which
// carrier is the assigned artifact.
type scalarCarriers struct {
	observations    []referencecontract.Carrier
	legacy          []prompts.LegacyArtifactReference
	assignedIndex   int // -1 when no scalar ref is a strict carrier
	assignedHeading string
}

// buildReferenceContextWithRepository orchestrates the reference stages at one
// integration HEAD: collection and validation of every carrier first, then
// elision and presentation selection, then rendering and legacy filtering.
func buildReferenceContextWithRepository(repo referenceContextRepository, task *models.Task, state *models.State, config SupervisorConfig, roleType string, decompositionRoot bool) (string, []prompts.LegacyArtifactReference, error) {
	head := &referenceHead{repo: repo, branch: state.Config.IntegrationBranch}
	proofs := allocationProofPolicy(repo, task)

	scalar, err := collectScalarCarriers(repo, head, task, proofs)
	if err != nil {
		return "", nil, err
	}
	observations := scalar.observations
	parents, err := collectParentCarriers(repo, head, task, state, config, proofs)
	if err != nil {
		return "", nil, err
	}
	observations = append(observations, parents...)
	if roleType == "reviewer" {
		reviewed, err := collectReviewCarriers(repo, head, task, config, proofs)
		if err != nil {
			return "", nil, err
		}
		observations = append(observations, reviewed...)
	}

	if decompositionRoot {
		elideDecompositionParents(observations, scalar.assignedIndex)
	}
	notice := selectAssignedPresentation(observations, scalar.assignedIndex, scalar.assignedHeading, roleType, config.Role)

	context, err := referencecontract.RenderCarriers(observations)
	if err != nil {
		return "", nil, err
	}
	return notice + context, filterLegacyReferences(scalar.legacy, observations), nil
}

// collectScalarCarriers walks the task's scalar refs. The most specific strict
// scalar carrier is the task's assigned artifact and keeps its declared
// references inlined; the other scalar carriers are ancestors whose references
// render as pointers. Parent and review carriers are assigned, with the
// decomposition-root exception. When the assigned artifact also arrives
// through a parent range, that observation wins the path and is assigned
// anyway, so the scalar route is the fallback for a parent that is not merged
// or has no reviewed range.
//
// Scalar refs naming one path read one blob at the captured HEAD, so they
// form one observation. It narrows to a fragment only when every ref on
// the path names that fragment: a fragment-less ref assigns the whole file,
// and narrowing to one of two fragments would drop the other.
func collectScalarCarriers(repo referenceContextRepository, head *referenceHead, task *models.Task, proofs referencecontract.ProofPolicy) (scalarCarriers, error) {
	found := scalarCarriers{assignedIndex: -1}
	assignedRank := -1
	scalarIndex := make(map[string]int)
	agreedFragment := make(map[string]string) // "" renders the whole file
	for _, scalar := range []struct {
		field string
		ref   string
		rank  int
	}{
		{field: "spec_ref", ref: task.SpecRef, rank: referenceRankSpec},
		{field: "epic_ref", ref: task.EpicRef, rank: referenceRankEpic},
		{field: "plan_ref", ref: task.PlanRef, rank: referenceRankPlan},
		{field: "arch_ref", ref: task.ArchRef, rank: referenceRankArch},
	} {
		if scalar.ref == "" {
			continue
		}
		revision, err := head.resolve()
		if err != nil {
			return scalarCarriers{}, err
		}
		observation, strict, loadErr := loadScalarCarrier(repo, revision, scalar.ref, proofs)
		if loadErr != nil {
			return scalarCarriers{}, loadErr
		}
		if strict {
			fragment := paths.SplitRefFragment(scalar.ref)
			index, seen := scalarIndex[observation.Path]
			if !seen {
				index = len(found.observations)
				scalarIndex[observation.Path], agreedFragment[observation.Path] = index, fragment
				observation.ElideRefs = true
				found.observations = append(found.observations, observation)
			} else if agreedFragment[observation.Path] != fragment {
				agreedFragment[observation.Path] = ""
			}
			if scalar.rank > assignedRank {
				found.assignedIndex, assignedRank = index, scalar.rank
			}
			continue
		}
		found.legacy = append(found.legacy, prompts.LegacyArtifactReference{
			Field: scalar.field,
			Ref:   scalar.ref,
			File:  paths.SplitRefFile(scalar.ref),
		})
	}
	for path, index := range scalarIndex {
		if fragment := agreedFragment[path]; fragment != "" {
			// loadScalarCarrier already resolved this fragment in the same blob.
			span, err := referencecontract.ExtractSection(found.observations[index].Span, fragment)
			if err != nil {
				return scalarCarriers{}, fmt.Errorf("select strict scalar carrier %q: %w", path+"#"+fragment, err)
			}
			found.observations[index].Span = span
		}
	}
	if found.assignedIndex >= 0 {
		found.observations[found.assignedIndex].ElideRefs = false
		found.assignedHeading = agreedFragment[found.observations[found.assignedIndex].Path]
	}
	return found, nil
}

// collectParentCarriers loads the strict carriers of every merged direct
// parent's reviewed ranges, validating amended review history first.
func collectParentCarriers(repo referenceContextRepository, head *referenceHead, task *models.Task, state *models.State, config SupervisorConfig, proofs referencecontract.ProofPolicy) ([]referencecontract.Carrier, error) {
	var observations []referencecontract.Carrier
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if parent == nil {
			return nil, fmt.Errorf("direct parent %q not found", parentID)
		}
		if parent.Status != models.TaskStatusMerged {
			continue
		}
		ranges, rangeErr := ops.PlanReviewedRanges(state, parent)
		if rangeErr != nil {
			return nil, rangeErr
		}
		if len(ranges) == 0 {
			continue
		}
		revision, err := head.resolve()
		if err != nil {
			return nil, err
		}
		if parent.PlanAmendment != nil && len(parent.PlanAmendment.Applied) > 0 {
			if err := ops.ValidatePlanReviewHistory(config.ProjectRoot, state, parent, revision); err != nil {
				return nil, err
			}
		}
		for _, reviewed := range ranges {
			found, discoverErr := referencecontract.LoadDiffCarriers(repo, config.ProjectRoot, reviewed.BaseCommit, reviewed.ReviewCommit, revision, referencecontract.CarrierParent, true, proofs)
			if discoverErr != nil {
				return nil, fmt.Errorf("direct parent %q revision %q: %w", parentID, reviewed.TaskID, discoverErr)
			}
			observations = append(observations, found...)
		}
	}
	return observations, nil
}

// collectReviewCarriers loads the strict carriers of the current review range;
// a reviewer without its full attribution is refused.
func collectReviewCarriers(repo referenceContextRepository, head *referenceHead, task *models.Task, config SupervisorConfig, proofs referencecontract.ProofPolicy) ([]referencecontract.Carrier, error) {
	if task.BaseCommit == nil || *task.BaseCommit == "" || task.ReviewCommit == nil || *task.ReviewCommit == "" {
		return nil, fmt.Errorf("review task %q lacks BaseCommit..ReviewCommit attribution", task.ID)
	}
	revision, err := head.resolve()
	if err != nil {
		return nil, err
	}
	return referencecontract.LoadDiffCarriers(repo, config.ProjectRoot, *task.BaseCommit, *task.ReviewCommit, revision, referencecontract.CarrierReview, false, proofs)
}

// elideDecompositionParents renders the parent carriers a decomposition root
// needs as bodies with second-hop references. It runs only after every
// observation has been validated. The assigned scalar path retains full
// references even when a parent observation wins it; current-review master
// declarations likewise retain full authority and once-only emission.
func elideDecompositionParents(observations []referencecontract.Carrier, assignedIndex int) {
	for index := range observations {
		if observations[index].Class == referencecontract.CarrierParent &&
			(assignedIndex < 0 || observations[index].Path != observations[assignedIndex].Path) {
			observations[index].ElideRefs = true
		}
	}
}

// selectAssignedPresentation narrows every observation of the assigned path to
// the assigned ref's fragment: a parent range that rediscovers the same
// artifact would otherwise inline the whole file and drop the section the task
// was pointed at. An architect or code-planner doer whose winning carrier
// declares a section read set additionally inlines only the selected
// references and points at the rest. It returns the notice to prepend.
func selectAssignedPresentation(observations []referencecontract.Carrier, assignedIndex int, assignedHeading, roleType, role string) string {
	if assignedIndex < 0 || assignedHeading == "" {
		return ""
	}
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
	if roleType != "doer" || (role != models.RoleArchitect && role != models.RoleCodePlanner) {
		return ""
	}
	ids, present, err := referencecontract.SectionReferenceIDs(winner.Span, assignedHeading)
	if err != nil {
		// Historical merged declarations cannot be repaired at launch;
		// retain full context rather than guessing an empty read set.
		return fmt.Sprintf("SECTION READ SET NOTICE: %v; legacy full context retained.\n", err)
	}
	if !present {
		return ""
	}
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
	notice := ""
	if len(unknown) > 0 {
		notice = fmt.Sprintf("SECTION READ SET NOTICE: undeclared IDs %q; inspect the pinned carrier's Source References.\n", strings.Join(unknown, ", "))
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
	return notice
}

// filterLegacyReferences drops the legacy refs whose file a strict carrier
// already supplies.
func filterLegacyReferences(legacyReferences []prompts.LegacyArtifactReference, observations []referencecontract.Carrier) []prompts.LegacyArtifactReference {
	strictCarrierPaths := make(map[string]struct{}, len(observations))
	for _, observation := range observations {
		strictCarrierPaths[observation.Path] = struct{}{}
	}
	filtered := legacyReferences[:0]
	for _, legacy := range legacyReferences {
		if _, suppliedByStrictCarrier := strictCarrierPaths[legacy.File]; !suppliedByStrictCarrier {
			filtered = append(filtered, legacy)
		}
	}
	return filtered
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
