package ops

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
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
	// AcceptanceFaultRuntimeInput is a declared runtime input with no usable
	// ledger instance (ADR-0169). Only an operator can record one, so it
	// escalates at once like a content refusal, and no agent retry helps.
	AcceptanceFaultRuntimeInput AcceptanceFault = "runtime_input"
	// AcceptanceFaultAdmissionSlot is an admission limit whose class had no
	// free slot in time (ADR-0209). No command ran; a later retry can pass.
	AcceptanceFaultAdmissionSlot AcceptanceFault = "admission_slot"
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
	// Input names the first refused runtime input of a runtime-input refusal.
	Input string
	// Blocked reports that the refusing submission also moved the task to
	// BLOCKED; BlockFailed that it tried and could not.
	Blocked     bool
	BlockFailed bool
}

func (e *AcceptanceEvidenceError) Error() string {
	message := fmt.Sprintf("task %s %s: %s — correct the evidence and submit, or run %s for a submitted task", e.TaskID, e.Field, e.Reason, brand.Command("update-review-commit", e.TaskID))
	if e.Class == AcceptanceFaultRuntimeInput {
		message = fmt.Sprintf("task %s %s: %s — operator action required: %s. No command ran and no review cycle was spent. Do not retry, change evidence or run %s: an agent cannot provide this input",
			e.TaskID, e.Field, e.Reason, runtimeInputOperatorAction(e.TaskID, e.Input), brand.Command("update-review-commit"))
		switch {
		case e.Blocked:
			message += "; the task is now BLOCKED until the operator records the input and runs " + brand.Command("unblock-task", e.TaskID)
		case e.BlockFailed:
			message += fmt.Sprintf("; blocking the task failed, so run %s with this reason and stop", brand.Command("mark-blocked", e.TaskID))
		}
		return acceptanceExecutionMask(os.Environ())(message)
	}
	if e.Class == AcceptanceFaultAdmissionSlot {
		message = fmt.Sprintf("task %s %s: %s — the operator's admission limit was full. No command ran and no review cycle was spent; do not change the evidence: retry the same commit", e.TaskID, e.Field, e.Reason)
		return acceptanceExecutionMask(os.Environ())(message)
	}
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
	// batchTimeoutSeconds is the admission batch budget: the reviewed
	// timeout_seconds raised by any recorded budget exception (ADR-0207).
	// contract keeps the reviewed value for receipts and deferrals.
	batchTimeoutSeconds int
	// deferred is derived from the contract and the current operator rulings;
	// admission skips these commands (ADR-0204).
	deferred []models.AcceptanceDeferredCommand
}

// executedCommands returns the canonical commands admission runs, in order,
// with each one's canonical index so diagnostics name the reviewed position.
func (input *acceptanceInput) executedCommands() (commands []string, indices []int) {
	skipped := make(map[int]bool, len(input.deferred))
	for _, deferral := range input.deferred {
		skipped[deferral.Index] = true
	}
	for i, command := range input.contract.Validation {
		if !skipped[i] {
			commands = append(commands, command)
			indices = append(indices, i)
		}
	}
	return commands, indices
}

// acceptanceDeferrals derives the commands admission skips: the contract's
// reviewed deferred indices, plus commands an operator ruling defers. A
// command a runtime input lists runs only in the gate (ADR-0169): the contract
// may not defer it, and a ruling does not apply to it.
func acceptanceDeferrals(config models.Config, contract *referencecontract.AcceptanceContract, runtimeInputs []models.RuntimeInput) ([]models.AcceptanceDeferredCommand, error) {
	gateOnly := map[string]bool{}
	for _, input := range runtimeInputs {
		for _, command := range input.Commands {
			gateOnly[command] = true
		}
	}
	mask := acceptanceExecutionMask(os.Environ())
	var deferred []models.AcceptanceDeferredCommand
	for i, command := range contract.Validation {
		authority := models.AcceptanceDeferralAuthority("")
		switch {
		case contract.IsDeferred(i):
			if gateOnly[command] {
				return nil, fmt.Errorf("acceptance.deferred: validation[%d] is used by a runtime input and must run in the gate", i)
			}
			authority = models.AcceptanceDeferralContract
		case !gateOnly[command] && models.DeferredValidationRulingFor(config, command) != nil:
			authority = models.AcceptanceDeferralOperator
		default:
			continue
		}
		deferred = append(deferred, models.AcceptanceDeferredCommand{
			Index:          i,
			Command:        mask(command),
			CommandSHA256:  fmt.Sprintf("%x", sha256.Sum256([]byte(command))),
			TimeoutSeconds: contract.TimeoutSeconds,
			Authority:      authority,
		})
	}
	return deferred, nil
}

// sameDeferrals compares deferral identity. The masked display text depends
// on the comparing process's environment, so it is not identity.
func sameDeferrals(want, got []models.AcceptanceDeferredCommand) bool {
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		w, g := want[i], got[i]
		if w.Index != g.Index || w.CommandSHA256 != g.CommandSHA256 || w.TimeoutSeconds != g.TimeoutSeconds || w.Authority != g.Authority {
			return false
		}
	}
	return true
}

// loadAcceptanceInput derives authority from the allocated, independently
// reviewed source at integration. Candidate worktree bytes cannot supply it.
// A task that declares runtime inputs must turn out strict: only the strict
// gate runs the canonical commands that consume them (ADR-0169).
func loadAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	input, err := loadDeclaredAcceptanceInput(root, state, task, integrationCommit)
	if err == nil && input == nil && len(task.RuntimeInputs) > 0 {
		return nil, &AcceptanceEvidenceError{TaskID: task.ID, Field: "runtime_inputs", Reason: runtimeInputsStrictReason, Class: AcceptanceFaultContent}
	}
	return input, err
}

func loadDeclaredAcceptanceInput(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceInput, error) {
	// Planning can consume documents containing coding allocations. Do not
	// adopt those contracts, but never let a type change erase prior adoption.
	if task.EffectiveType() != models.TaskTypeCoding && task.AcceptanceSource == nil {
		return nil, nil
	}
	ref := acceptanceAllocationRef(task)
	if original := models.PendingPlanAmendment(state, task); original != "" {
		return nil, &AcceptanceEvidenceError{TaskID: task.ID, Field: "acceptance.source", Reason: "reviewed plan amendment pending for " + original, Class: AcceptanceFaultAllocation}
	}
	failAs := func(class AcceptanceFault, reason string) (*acceptanceInput, error) {
		return nil, &AcceptanceEvidenceError{TaskID: task.ID, Field: "acceptance.source", Reason: reason, Class: class}
	}
	// fail is for a failed Git read; content and allocation name the sites
	// that refuse what they read.
	fail := func(reason string) (*acceptanceInput, error) { return failAs("", reason) }
	content := func(reason string) (*acceptanceInput, error) { return failAs(AcceptanceFaultContent, reason) }
	allocation := func(reason string) (*acceptanceInput, error) { return failAs(AcceptanceFaultAllocation, reason) }
	// readRefusal classifies a bounded-reader error: the reader's own refusal
	// is content, anything else a failed read.
	readRefusal := func(reason string, err error) (*acceptanceInput, error) {
		var refused *acceptanceArtifactRefusal
		if errors.As(err, &refused) {
			return content(reason)
		}
		return fail(reason)
	}
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
	entry, present, err := g.TreeEntryAt(integrationCommit, path)
	if err != nil {
		return fail("cannot inspect integration source")
	}
	if !present {
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return content("adopted source is missing from integration")
	}
	if entry.Mode != "100644" && entry.Mode != "100755" {
		return content("source must be a regular committed file")
	}
	carrier, _, err := readAcceptanceEntry(g, path, entry, present, nil)
	if err != nil {
		return readRefusal(err.Error(), err)
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
	deferred, err := acceptanceDeferrals(state.Config, contract, task.RuntimeInputs)
	if err != nil {
		return content(err.Error())
	}
	refs, err := referencecontract.Parse(strings.ReplaceAll(carrier, "\r\n", "\n"))
	if err != nil || refs == nil {
		return content("strict Source References are required")
	}
	if reason, readFailure := referenceBoundsRefusal(root, refs); reason != "" {
		if readFailure {
			return fail(reason)
		}
		return content(reason)
	}

	var source *models.AcceptanceSource
	var driftedReference string
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		review := parentAllocationReview(g, root, state, task, parent, path, heading, integrationSpan, integrationCommit)
		if review == nil {
			continue
		}
		// The span excludes Source References, which is where the contract's
		// obligations and approved proofs resolve. Comparing only the span
		// would let an edit there redirect an approved reference at content no
		// reviewer saw, so every reference the reviewed contract depends on is
		// compared by what it resolves to, not by its declared ID.
		if alike, drifted := compareReviewedReferences(state, parent.ID, root, *review.ReviewCommit, integrationCommit, path, heading, contract); !alike {
			if drifted != "" {
				driftedReference = drifted
			}
			continue
		}
		if source != nil {
			return content("multiple reviewed parents claim the same allocation")
		}
		source = &models.AcceptanceSource{Ref: ref, Commit: integrationCommit, Blob: spanIdentity, ParentTask: parent.ID, ParentReviewCommit: *review.ReviewCommit}
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
		batchTimeoutSeconds: admissionTimeoutSeconds(contract.TimeoutSeconds, task.History),
		deferred:            deferred,
	}, nil
}

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

// referenceSection extracts the section a Direct Reference cites and bounds
// it. Line endings are normalized first, as for carrier spans, so the bound
// applies to the same bytes a section's identity is computed over and a CRLF
// conversion cannot change whether a reference is admissible.
func referenceSection(content, heading string) (string, error) {
	section, err := referencecontract.ExtractSection(strings.ReplaceAll(content, "\r\n", "\n"), heading)
	if err != nil {
		return "", err
	}
	if len(section) > referencecontract.ReferenceSectionMaxBytes {
		return "", fmt.Errorf("cited section is %d bytes, over the %d-byte bound", len(section), referencecontract.ReferenceSectionMaxBytes)
	}
	return section, nil
}

// referenceBoundsRefusal resolves every Direct Reference of an acceptance
// carrier under the bounds claim adopts it with: the file under
// ReferenceFileMaxBytes, the cited section under ReferenceSectionMaxBytes.
// Claim and planning submission both call it, so a plan whose coding children
// claim would refuse is refused before review instead of after merge (D-171).
// It returns "" when every reference resolves; otherwise the reason, and
// whether it is a failed Git read rather than content the author can fix.
func referenceBoundsRefusal(root string, refs *referencecontract.Contract) (reason string, readFailure bool) {
	for _, direct := range refs.DirectReferences {
		revision := direct.EffectiveRevision(refs.SourceRevision)
		content, _, err := readReferenceBlob(root, revision, direct.Path)
		if err != nil {
			var refused *acceptanceArtifactRefusal
			return "cannot resolve pinned reference " + direct.ID + ": " + err.Error(), !errors.As(err, &refused)
		}
		if _, err := referenceSection(content, direct.Heading); err != nil {
			var heading *referencecontract.HeadingMatchError
			if errors.As(err, &heading) {
				return "cannot resolve exact heading for reference " + direct.ID + ": " + err.Error(), false
			}
			return fmt.Sprintf("reference %s: %v", direct.ID, err), false
		}
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

// sameBlobAt reports whether path resolves to the same blob at both commits.
// Any lookup failure answers false, so callers fall through to their full
// comparison exactly as when either side cannot be resolved. Both sides are
// resolved by one git process.
func sameBlobAt(g *git.Git, commitA, commitB, path string) bool {
	oids, err := g.BlobOIDs([]git.BlobPath{{Revision: commitA, Path: path}, {Revision: commitB, Path: path}})
	if err != nil {
		return false
	}
	return oids[0].Err == nil && oids[1].Err == nil && oids[0].OID == oids[1].OID
}

// acceptanceArtifactRefusal is a readBoundedBlob refusal decided by the path
// or the committed tree entry, so an unchanged commit is refused identically.
// Any other reader error is a failed Git read.
type acceptanceArtifactRefusal struct{ reason string }

func (r *acceptanceArtifactRefusal) Error() string { return r.reason }

// readAcceptanceBlob bounds the object before reading it and never consults
// the worktree or follows symlinks. Used for carriers and manifests; Direct
// Reference targets go through readReferenceBlob.
func readAcceptanceBlob(root, commit, path string) (string, string, error) {
	return readBoundedBlob(root, commit, path, referencecontract.AcceptanceMaxBytes)
}

// readReferenceBlob reads a Direct Reference's file under the larger file cap;
// referenceSection then bounds the section the reference actually cites.
func readReferenceBlob(root, commit, path string) (string, string, error) {
	return readBoundedBlob(root, commit, path, referencecontract.ReferenceFileMaxBytes)
}

func readBoundedBlob(root, commit, path string, limit int64) (string, string, error) {
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		return "", "", &acceptanceArtifactRefusal{"invalid acceptance artifact path"}
	}
	g := git.New(root)
	// One ls-tree reports mode, type, object ID and size together; the blob
	// itself is read only after the size bound holds.
	entry, present, err := g.TreeEntryAt(commit, path)
	return readBoundedEntry(g, path, entry, present, err, limit)
}

// readAcceptanceEntry applies readAcceptanceBlob's bounds to a tree entry the
// caller already looked up, so a caller that inspected the entry itself does
// not pay a second ls-tree for the same path.
func readAcceptanceEntry(g *git.Git, path string, entry git.TreeEntry, present bool, err error) (string, string, error) {
	return readBoundedEntry(g, path, entry, present, err, referencecontract.AcceptanceMaxBytes)
}

func readBoundedEntry(g *git.Git, path string, entry git.TreeEntry, present bool, err error, limit int64) (string, string, error) {
	if err != nil {
		return "", "", fmt.Errorf("cannot inspect acceptance artifact: %s: %w", path, err)
	}
	if !present || (entry.Mode != "100644" && entry.Mode != "100755") {
		return "", "", &acceptanceArtifactRefusal{"acceptance artifact must be a present regular committed file: " + path}
	}
	if entry.Type != "blob" || entry.OID == "" {
		return "", "", &acceptanceArtifactRefusal{"cannot resolve acceptance artifact: " + path}
	}
	blob := entry.OID
	if entry.Size < 0 {
		return "", "", &acceptanceArtifactRefusal{"acceptance artifact cannot be sized: " + path}
	}
	if entry.Size > limit {
		return "", "", &acceptanceArtifactRefusal{fmt.Sprintf("acceptance artifact exceeds %d KiB (%d bytes): %s", limit/1024, entry.Size, path)}
	}
	content, err := g.ReadBlobObject(blob)
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
	return &models.AcceptanceReceipt{Version: 1, ReviewCommit: commit, Source: input.source, ManifestPath: input.contract.Manifest, ManifestBlob: blob, Mappings: manifest.Mappings, Deferred: slices.Clone(input.deferred)}, nil
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

// executeAcceptanceReceipt runs the strict gate. Runtime inputs are acquired
// here, after every non-executing precondition, so a refused manifest or a
// dirty worktree never spends a single_use input (ADR-0169). Admission slots
// of config's limits are taken just before, so a full class spends none
// either; slots already in held (submit's pre-admission) stay the caller's to
// release, otherwise they are released on return (ADR-0209). The batch budget
// starts once they are held. Cancelling ctx stops the wait or the command.
func executeAcceptanceReceipt(ctx context.Context, root string, task *models.Task, input *acceptanceInput, commit string, gate runtimeInputGate, config models.Config, held *admissionSlots) (*models.AcceptanceReceipt, error) {
	receipt, err := prepareAcceptanceReceipt(root, task, input, commit)
	if err != nil || receipt == nil {
		return receipt, err
	}
	if err := checkAcceptanceWorktree(root, task.ID, commit); err != nil {
		return nil, err
	}
	commands, indices := input.executedCommands()
	if held == nil {
		held = newAdmissionSlots(input.batchTimeoutSeconds)
		defer held.release()
	}
	if err := held.acquire(ctx, root, models.AdmissionLimitsFor(config, commands)); err != nil {
		return nil, admissionSlotRefusal(task.ID, err)
	}
	grant, err := gate.acquire(task)
	if err != nil {
		return nil, err
	}
	budget := acceptanceBatchTimeout(input.batchTimeoutSeconds)
	held.progress.set(fmt.Sprintf("running canonical validation, batch budget %s", budget), time.Now().Add(budget))
	receipt.Commands, err = executeAcceptanceCommandsWith(ctx, task.ID, git.New(root).GetWorktreePath(task.ID), commands, indices, input.batchTimeoutSeconds, grant, held.files())
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
	if sameDeferrals(expected.Deferred, receipt.Deferred) {
		expected.Deferred = receipt.Deferred
	}
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
	expected.Deferred = receipt.Deferred
	if !reflect.DeepEqual(expected, receipt) {
		return acceptanceError(task.ID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	return validateReceiptExecutions(task.ID, input, receipt)
}

// validateReceiptExecutions binds every canonical command to either a
// successful execution or a deferral that current authority still grants. A
// command executed although a later ruling defers it remains valid evidence,
// so a new ruling never strands an already-submitted receipt; a deferral whose
// ruling was withdrawn does not.
func validateReceiptExecutions(taskID string, input *acceptanceInput, receipt *models.AcceptanceReceipt) error {
	granted := make(map[int]models.AcceptanceDeferredCommand, len(input.deferred))
	for _, deferral := range input.deferred {
		granted[deferral.Index] = deferral
	}
	skipped := make(map[int]bool, len(receipt.Deferred))
	for i, deferral := range receipt.Deferred {
		current, ok := granted[deferral.Index]
		if !ok || (i > 0 && deferral.Index <= receipt.Deferred[i-1].Index) || !sameDeferrals([]models.AcceptanceDeferredCommand{current}, []models.AcceptanceDeferredCommand{deferral}) {
			return acceptanceError(taskID, "acceptance.receipt.deferred", "deferral is not granted by the reviewed contract or a current operator ruling")
		}
		skipped[deferral.Index] = true
	}
	for index := range granted {
		if granted[index].Authority == models.AcceptanceDeferralContract && !skipped[index] {
			return acceptanceError(taskID, "acceptance.receipt.deferred", "receipt omits a deferral the reviewed contract declares")
		}
	}
	if len(receipt.Commands)+len(receipt.Deferred) != len(input.contract.Validation) {
		return acceptanceError(taskID, "acceptance.receipt", "receipt does not match validated manifest and canonical commands")
	}
	next := 0
	for i, command := range input.contract.Validation {
		if skipped[i] {
			continue
		}
		result := receipt.Commands[next]
		next++
		commandHash := fmt.Sprintf("%x", sha256.Sum256([]byte(command)))
		if result.CommandSHA256 != commandHash || result.ExitCode != 0 || result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) {
			return acceptanceError(taskID, "acceptance.receipt.commands", "missing or unsuccessful canonical execution")
		}
	}
	return nil
}
