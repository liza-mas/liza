package ops

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// AcceptanceFault classifies where an acceptance refusal came from, decided at
// the site that refuses. The zero value covers Git read failures and every site
// whose determinism is not established; a supervisor never escalates it.
type AcceptanceFault string

const (
	// AcceptanceFaultContent is a pure function of bytes and state that were
	// read successfully, so an unchanged claim is refused identically.
	AcceptanceFaultContent AcceptanceFault = "content"
	// AcceptanceFaultAllocation is a parent-allocation or ancestry refusal. The
	// helpers behind it read Git "false" for both a mismatch and a failed read,
	// so repetition can be observed but determinism cannot.
	AcceptanceFaultAllocation AcceptanceFault = "allocation"
)

// AcceptanceClaimObservation is what a refused claim validated against: the
// allocation reference, the integration commit and AcceptanceObservation of
// the task and state. It marks the error as claim-stage.
type AcceptanceClaimObservation struct {
	AllocationRef     string
	IntegrationCommit string
	Digest            string
}

// AcceptanceEvidenceError is a repairable admission precondition, never an
// integration failure. It identifies the task and field requiring correction.
type AcceptanceEvidenceError struct {
	TaskID string
	Field  string
	Reason string
	Class  AcceptanceFault
	// Claim is set only when the refusal came from a claim, whose task is
	// unsubmitted and whose acceptance fields only the orchestrator can change.
	Claim *AcceptanceClaimObservation
}

func (e *AcceptanceEvidenceError) Error() string {
	message := fmt.Sprintf("task %s %s: %s — correct the evidence and submit, or run %s for a submitted task", e.TaskID, e.Field, e.Reason, brand.Command("update-review-commit", e.TaskID))
	if e.Claim != nil {
		message = fmt.Sprintf("task %s %s (%s): %s — the task's acceptance allocation must be corrected by the orchestrator (%s) or its integration-side cause repaired; claiming cannot fix it", e.TaskID, e.Field, e.Claim.AllocationRef, e.Reason, brand.Command("replace-task"))
	}
	return acceptanceExecutionMask(os.Environ())(message)
}

// Unwrap preserves the existing precondition classification for callers that
// do not need the more specific acceptance field diagnostic.
func (e *AcceptanceEvidenceError) Unwrap() error {
	return &PreconditionError{Reason: e.Error()}
}

func acceptanceError(taskID, field, reason string) error {
	return &AcceptanceEvidenceError{TaskID: taskID, Field: field, Reason: reason}
}

type acceptanceInput struct {
	source        models.AcceptanceSource
	contract      *referencecontract.AcceptanceContract
	specRef       string
	destructiveDB bool
}

// loadAcceptanceInput derives authority from the allocated, independently
// reviewed source at integration. Candidate worktree bytes cannot supply it.
func loadAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	// Planning can consume documents containing coding allocations. Do not
	// adopt those contracts, but never let a type change erase prior adoption.
	if task.EffectiveType() != models.TaskTypeCoding && task.AcceptanceSource == nil {
		return nil, nil
	}
	ref := acceptanceAllocationRef(task)
	failAs := func(class AcceptanceFault, reason string) (*acceptanceInput, error) {
		return nil, &AcceptanceEvidenceError{TaskID: task.ID, Field: "acceptance.source", Reason: reason, Class: class}
	}
	// fail is for sites that read Git and cannot tell a failed read from a
	// refusal; content and allocation name the sites that can.
	fail := func(reason string) (*acceptanceInput, error) { return failAs("", reason) }
	content := func(reason string) (*acceptanceInput, error) { return failAs(AcceptanceFaultContent, reason) }
	allocation := func(reason string) (*acceptanceInput, error) { return failAs(AcceptanceFaultAllocation, reason) }
	if ref == "" {
		if task.AcceptanceSource != nil {
			return content("adopted source reference was removed")
		}
		return nil, nil
	}
	path, heading, _ := strings.Cut(ref, "#")
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		// Existing legacy artifact references are validated by their own path.
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return content("adopted source path is invalid")
	}
	g := git.New(root)
	mode, present, err := g.TreePathMode(integrationCommit, path)
	if err != nil {
		return fail("cannot inspect integration source")
	}
	if !present {
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return content("adopted source is missing from integration")
	}
	if mode != "100644" && mode != "100755" {
		return content("source must be a regular committed file")
	}
	carrier, _, err := readAcceptanceBlob(root, integrationCommit, path)
	if err != nil {
		return fail(err.Error())
	}
	contract, err := referencecontract.ParseAcceptance(carrier, heading)
	if err != nil {
		return content(err.Error())
	}
	// ParseAcceptance above already resolved this heading, so neither can fail
	// here; the values are what the parent comparison and the stored identity
	// need.
	integrationSpan, _ := carrierSpan(carrier, heading)
	spanIdentity, _ := carrierSpanIdentity(carrier, heading)
	if contract == nil {
		if task.AcceptanceSource != nil {
			return content("adopted acceptance declaration was removed; downgrade is forbidden")
		}
		return nil, nil
	}
	if !reflect.DeepEqual(contract.Validation, task.Validation) {
		return content("validation must equal the reviewed ordered canonical commands")
	}
	if err := models.ValidateValidationSafety("acceptance.validation", task.Validation, task.DestructiveDB); err != nil {
		return content(err.Error())
	}
	refs, err := referencecontract.Parse(strings.ReplaceAll(carrier, "\r\n", "\n"))
	if err != nil || refs == nil {
		return content("strict Source References are required")
	}
	for _, direct := range refs.DirectReferences {
		revision := direct.EffectiveRevision(refs.SourceRevision)
		span, _, err := readAcceptanceBlob(root, revision, direct.Path)
		if err != nil {
			return fail("cannot resolve pinned reference " + direct.ID)
		}
		if _, err := referencecontract.ExtractSection(span, direct.Heading); err != nil {
			return content("cannot resolve exact heading for reference " + direct.ID)
		}
	}

	var source *models.AcceptanceSource
	var driftedReference string
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		if !parentAllocatesTask(g, root, task, parent, path, heading, integrationSpan, integrationCommit) {
			continue
		}
		// The span excludes Source References, which is where the contract's
		// obligations and approved proofs resolve. Comparing only the span
		// would let an edit there redirect an approved reference at content no
		// reviewer saw, so every reference the reviewed contract depends on is
		// compared by what it resolves to, not by its declared ID.
		if alike, drifted := compareReviewedReferences(state, parent.ID, root, *parent.ReviewCommit, integrationCommit, path, heading, contract); !alike {
			if drifted != "" {
				driftedReference = drifted
			}
			continue
		}
		if source != nil {
			return content("multiple reviewed parents claim the same allocation")
		}
		source = &models.AcceptanceSource{Ref: ref, Commit: integrationCommit, Blob: spanIdentity, ParentTask: parent.ID, ParentReviewCommit: *parent.ReviewCommit}
	}
	if source == nil {
		if driftedReference != "" {
			return allocation(fmt.Sprintf("requires allocation by a direct independently approved merged planning parent; approved-proof reference %q has drifted — an authorized orchestrator may inspect the changed content and use %s to reaffirm this transition", driftedReference, brand.Command("reaffirm-proof", task.ID, driftedReference)))
		}
		return allocation("requires allocation by a direct independently approved merged planning parent")
	}
	// Keep the original carrier identity when unrelated integration work lands.
	//
	// Identity is the allocation itself, not the file that carries it. Reaching
	// this point means the reviewed span and every reference the contract
	// depends on still resolve to what was approved, so an edit elsewhere in
	// the carrier leaves the adopted source intact. Comparing the whole-file
	// blob here instead would re-derive on any carrier edit and strand an
	// already-adopted child at reviewer assignment, where the stored source
	// must still equal the derived one.
	if old := task.AcceptanceSource; old != nil && old.Ref == source.Ref && old.ParentTask == source.ParentTask && old.ParentReviewCommit == source.ParentReviewCommit {
		ancestor, err := g.IsAncestor(old.Commit, integrationCommit)
		if err != nil || !ancestor {
			return allocation("adopted source commit is no longer in integration ancestry")
		}
		source.Commit = old.Commit
	}
	return &acceptanceInput{
		source: *source, contract: contract,
		specRef: task.SpecRef, destructiveDB: task.DestructiveDB,
	}, nil
}

// parentAllocatesTask reports whether this parent authorizes the task's
// allocation on every ground except the approved-proof comparison.
//
// That comparison is deliberately left to the caller, because the two callers
// do different things with the same answer: acceptance refuses, and
// re-affirmation decides. Sharing everything up to that point is what keeps a
// recovery grant bound to the parent that actually allocates the task —
// selecting "the first merged parent" instead would let a multi-parent child be
// re-affirmed against a parent that never allocated it, leaving the real one
// unauthorized and the task still refused.
func parentAllocatesTask(g *git.Git, root string, task, parent *models.Task, path, heading, integrationSpan, integrationCommit string) bool {
	if parent == nil || parent.EffectiveType() != models.TaskTypePlanning || parent.Status != models.TaskStatusMerged || parent.BaseCommit == nil || parent.ReviewCommit == nil || parent.MergeCommit == nil {
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
	if !approved {
		return false
	}
	if !validAcceptanceParentHistory(g, parent) {
		return false
	}
	allocated := false
	for _, output := range parent.Output {
		if output.PlanRef == task.PlanRef && output.SpecRef == task.SpecRef && reflect.DeepEqual(output.Validation, task.Validation) && output.DestructiveDB == task.DestructiveDB {
			allocated = true
		}
	}
	if !allocated {
		return false
	}
	ancestor, ancestorErr := g.IsAncestor(*parent.MergeCommit, integrationCommit)
	if ancestorErr != nil || !ancestor {
		return false
	}
	// The reviewer approved this carrier's allocation section. An edit to an
	// unrelated section of the same file — a repin of another reference, a
	// note appended elsewhere — does not unapprove it, and treating it as a
	// change strands every child of the plan until the plan is re-reviewed.
	// ADR-0133 section 4 already judges the reference path this way.
	reviewedSpan, reviewedOK := acceptanceCarrierSpan(root, *parent.ReviewCommit, path, heading)
	if !reviewedOK || reviewedSpan != integrationSpan {
		return false
	}
	// Inherited shape: a carrier absent at base and one identical at base both
	// fall through here, deliberately — only "unchanged" disqualifies.
	baseSpan, _ := acceptanceCarrierSpan(root, *parent.BaseCommit, path, heading)
	return baseSpan != reviewedSpan // Approval of unchanged content authorizes nothing.
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
	for _, commit := range []string{*parent.BaseCommit, *parent.ReviewCommit, *parent.MergeCommit} {
		resolved, err := g.ResolveCommit(commit)
		if err != nil || resolved != commit {
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

// readAcceptanceBlob bounds the object before reading it and never consults
// the worktree or follows symlinks. Used for source, proof and manifest objects.
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
	reviewedBlob, reviewedErr := g.BlobOID(reviewCommit, path)
	integrationBlob, integrationErr := g.BlobOID(integrationCommit, path)
	if reviewedErr == nil && integrationErr == nil && reviewedBlob == integrationBlob {
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

// blobCache memoizes file content by revision and path within one pass. A nil
// cache reads every time, which is what the acceptance boundary wants: it
// resolves a handful of references and must not hold stale content across
// calls. A caller comparing many references that cite the same file at the
// same revision passes a cache and pays one read instead of one per reference.
type blobCache map[string]string

func (c blobCache) read(root, revision, path string) (string, bool) {
	key := revision + "\x00" + path
	if c != nil {
		if content, ok := c[key]; ok {
			return content, true
		}
	}
	content, _, err := readAcceptanceBlob(root, revision, path)
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
		section, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), direct.Heading)
		if err != nil {
			return "", false
		}
		return section, true
	}
	return "", false
}

// carrierSpanIdentity is the identity of an allocation: the object id of the
// section a verdict covered, not of the file that carries it.
//
// AcceptanceSource.Blob holds this at every boundary — allocation, candidate
// submission, reviewer assignment — so all three agree on what "the reviewed
// source changed" means. Holding a whole-file object id at one boundary and a
// span at another is what lets an unrelated edit strand a child at whichever
// boundary still reads the file.
//
// It is computed exactly as Git hashes a blob, so the value stays a 40-char
// lowercase object id like every other identifier in AcceptanceSource, and
// `git hash-object` on the extracted section reproduces it. A digest of another
// shape would hold, but it would also make the field mean two things at once
// and put it at odds with the state validator's object-id rule.
func carrierSpanIdentity(content, heading string) (string, bool) {
	span, ok := carrierSpan(content, heading)
	if !ok {
		return "", false
	}
	return spanObjectID(span), true
}

// spanObjectID is the Git object id of a section's bytes.
//
// sha1 here is a content address, not an integrity digest: it is what Git
// computes for these same bytes, so `git hash-object` on the extracted section
// reproduces it. The sha256 elsewhere in this file, over canonical commands, is
// the integrity digest. Different jobs — do not unify them.
func spanObjectID(span string) string {
	hasher := sha1.New()
	fmt.Fprintf(hasher, "blob %d\x00", len(span))
	hasher.Write([]byte(span))
	return hex.EncodeToString(hasher.Sum(nil))
}

// carrierSpan isolates the part of a carrier a reviewer's verdict covers: the
// section named by the reference heading, or the whole file when the reference
// names none. Returns false when the heading cannot be resolved uniquely, so
// an ambiguous or missing heading refuses rather than allocating on a guess.
//
// Line endings are normalized on both sides of every comparison, matching the
// parser: a CRLF conversion at integration is a transport change, not a change
// to what a reviewer approved.
func carrierSpan(content, heading string) (string, bool) {
	if heading == "" {
		return content, true
	}
	section, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), heading)
	if err != nil {
		return "", false
	}
	return section, true
}

// acceptanceCarrierSpan reads a carrier at one commit and isolates the same
// section. A carrier absent at that commit yields false, never an empty match.
func acceptanceCarrierSpan(root, commit, path, heading string) (string, bool) {
	content, _, err := readAcceptanceBlob(root, commit, path)
	if err != nil {
		return "", false
	}
	return carrierSpan(content, heading)
}

func readAcceptanceBlob(root, commit, path string) (string, string, error) {
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		return "", "", fmt.Errorf("invalid acceptance artifact path")
	}
	g := git.New(root)
	mode, present, err := g.TreePathMode(commit, path)
	if err != nil {
		return "", "", fmt.Errorf("cannot inspect acceptance artifact: %s: %w", path, err)
	}
	if !present || (mode != "100644" && mode != "100755") {
		return "", "", fmt.Errorf("acceptance artifact must be a present regular committed file: %s", path)
	}
	blob, err := g.BlobOID(commit, path)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve acceptance artifact: %s: %w", path, err)
	}
	sizeOutput, err := gitenv.CombinedOutput(root, "cat-file", "-s", blob)
	if err != nil {
		return "", "", fmt.Errorf("acceptance artifact cannot be sized: %s: %w\nOutput: %s", path, err, sizeOutput)
	}
	size, parseErr := strconv.Atoi(strings.TrimSpace(string(sizeOutput)))
	if parseErr != nil {
		return "", "", fmt.Errorf("acceptance artifact cannot be sized: %s: %w", path, parseErr)
	}
	if size > 256*1024 {
		return "", "", fmt.Errorf("acceptance artifact exceeds 256 KiB: %s", path)
	}
	content, err := g.ReadBlob(commit, path)
	if err != nil {
		return "", "", fmt.Errorf("cannot read acceptance artifact: %s: %w", path, err)
	}
	return content, blob, nil
}

func prepareAcceptanceReceipt(root string, task *models.Task, input *acceptanceInput, commit string) (*models.AcceptanceReceipt, error) {
	if input == nil {
		return nil, nil
	}
	path, heading, _ := strings.Cut(input.source.Ref, "#")
	candidateContent, _, err := readAcceptanceBlob(root, commit, path)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.source", "candidate changes the independently reviewed source")
	}
	// Same identity as allocation: the candidate may carry an unrelated edit to
	// the carrier it rebased onto, but not a change to the allocation itself.
	candidateIdentity, ok := carrierSpanIdentity(candidateContent, heading)
	if !ok || candidateIdentity != input.source.Blob {
		return nil, acceptanceError(task.ID, "acceptance.source", "candidate changes the independently reviewed source")
	}
	content, blob, err := readAcceptanceBlob(root, commit, input.contract.Manifest)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.manifest", err.Error())
	}
	manifest, err := referencecontract.ParseAcceptanceManifest(content)
	if err == nil {
		err = referencecontract.ValidateAcceptanceManifest(input.contract, manifest)
	}
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.mappings", err.Error())
	}
	for _, mapping := range manifest.Mappings {
		if mapping.File == "" {
			continue
		}
		mode, present, err := git.New(root).TreePathMode(commit, mapping.File)
		if err != nil || !present || (mode != "100644" && mode != "100755") {
			return nil, acceptanceError(task.ID, "acceptance.mappings["+mapping.ObligationID+"].file", "must name a regular committed file")
		}
	}
	return &models.AcceptanceReceipt{Version: 1, ReviewCommit: commit, Source: input.source, ManifestPath: input.contract.Manifest, ManifestBlob: blob, Mappings: manifest.Mappings}, nil
}

func checkAcceptanceWorktree(root, taskID, commit string) error {
	g := git.New(root)
	head, err := g.GetWorktreeHEAD(taskID)
	if err != nil || head != commit {
		return acceptanceError(taskID, "acceptance.review_commit", "worktree HEAD changed during validation")
	}
	status, err := g.WorktreeStatusShort(g.GetWorktreePath(taskID))
	if err != nil || status != "" {
		return acceptanceError(taskID, "acceptance.worktree", "validation requires no staged, unstaged or untracked changes")
	}
	return nil
}

func executeAcceptanceReceipt(root string, task *models.Task, input *acceptanceInput, commit string) (*models.AcceptanceReceipt, error) {
	receipt, err := prepareAcceptanceReceipt(root, task, input, commit)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	receipt.Commands, err = executeAcceptanceCommands(task.ID, git.New(root).GetWorktreePath(task.ID), input.contract.Validation, input.contract.TimeoutSeconds)
	if err != nil {
		return nil, acceptanceError(task.ID, "acceptance.execution", err.Error())
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	return receipt, nil
}

// Recheck the source, task allocation and candidate identity in the final
// transaction. Commands ran outside the lock; this closes the stale-input gap.
func recheckAcceptanceReceipt(root string, state *models.State, task *models.Task, input *acceptanceInput, receipt *models.AcceptanceReceipt) error {
	integrationCommit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	current, err := loadAcceptanceInput(root, state, task, integrationCommit)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(current, input) {
		return acceptanceError(task.ID, "acceptance.source", "allocation or reviewed source changed during validation")
	}
	if receipt == nil {
		return nil
	}
	expected, err := prepareAcceptanceReceipt(root, task, current, receipt.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	if !reflect.DeepEqual(expected, receipt) {
		return acceptanceError(task.ID, "acceptance.receipt", "candidate manifest changed during validation")
	}
	return checkAcceptanceWorktree(root, task.ID, receipt.ReviewCommit)
}

func validateAcceptanceForAssignment(root string, state *models.State, task *models.Task) error {
	if task.AcceptanceSource == nil && task.PlanRef == "" && task.SpecRef == "" {
		return nil
	}
	commit, err := git.New(root).GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return acceptanceError(task.ID, "acceptance.source", "cannot resolve integration commit")
	}
	input, err := loadAcceptanceInput(root, state, task, commit)
	if err != nil || input == nil {
		return err
	}
	receipt := task.AcceptanceReceipt
	if task.ReviewCommit == nil || receipt == nil || receipt.ReviewCommit != *task.ReviewCommit || task.AcceptanceSource == nil || !reflect.DeepEqual(*task.AcceptanceSource, input.source) {
		return acceptanceError(task.ID, "acceptance.receipt", "missing or stale receipt at review commit")
	}
	expected, err := prepareAcceptanceReceipt(root, task, input, *task.ReviewCommit)
	if err != nil {
		return err
	}
	expected.Commands = receipt.Commands
	if !reflect.DeepEqual(expected, receipt) || len(receipt.Commands) != len(input.contract.Validation) {
		return acceptanceError(task.ID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	for i, result := range receipt.Commands {
		commandHash := fmt.Sprintf("%x", sha256.Sum256([]byte(input.contract.Validation[i])))
		if result.CommandSHA256 != commandHash || result.ExitCode != 0 || result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) {
			return acceptanceError(task.ID, "acceptance.receipt.commands", "missing or unsuccessful canonical execution")
		}
	}
	return nil
}
