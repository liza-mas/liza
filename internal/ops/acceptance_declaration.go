package ops

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// declaredCarrier is the source an acceptance allocation names, read at
// integration.
type declaredCarrier struct {
	ref, path, heading, content string
}

// acceptanceDeclaration is the contract a carrier declares at integration, with
// the identities the parent comparison and the stored source need.
type acceptanceDeclaration struct {
	ref, path, heading string
	contract           *referencecontract.AcceptanceContract
	// deferred is derived from the contract and the current operator rulings.
	deferred []models.AcceptanceDeferredCommand
	// span is the carrier section the allocation covers; spanIdentity is its
	// object id, which AcceptanceSource.Blob holds.
	span, spanIdentity string
}

// acceptanceSourceRefusal is a refusal of the task's acceptance source. The
// class is decided where the refusal is made: AcceptanceFaultContent and
// AcceptanceFaultAllocation name the sites that refuse what they read, and the
// zero class is for a failed Git read.
func acceptanceSourceRefusal(task *models.Task, class AcceptanceFault, reason string) error {
	return &AcceptanceEvidenceError{TaskID: task.ID, Field: "acceptance.source", Reason: reason, Class: class}
}

// readRefusalClass classifies a bounded-reader error: the reader's own refusal
// is content, anything else a failed read.
func readRefusalClass(err error) AcceptanceFault {
	var refused *acceptanceArtifactRefusal
	if errors.As(err, &refused) {
		return AcceptanceFaultContent
	}
	return ""
}

// readAcceptanceDeclaration reads the allocation's carrier at integration and
// parses its acceptance contract. A nil declaration with a nil error means the
// task declares no acceptance contract.
func readAcceptanceDeclaration(root string, state *models.State, task *models.Task, integrationCommit string) (*acceptanceDeclaration, error) {
	carrier, err := readDeclaredCarrier(root, task, integrationCommit)
	if err != nil || carrier == nil {
		return nil, err
	}
	return parseAcceptanceDeclaration(root, state, task, carrier)
}

// readDeclaredCarrier decides whether the task's allocation reference names an
// acceptance carrier and reads it from the integration commit.
func readDeclaredCarrier(root string, task *models.Task, integrationCommit string) (*declaredCarrier, error) {
	ref := acceptanceAllocationRef(task)
	if ref == "" {
		if task.AcceptanceSource != nil {
			return nil, acceptanceSourceRefusal(task, AcceptanceFaultContent, "adopted source reference was removed")
		}
		return nil, nil
	}
	path, heading, _ := strings.Cut(ref, "#")
	if err := referencecontract.ValidateAcceptancePath(path); err != nil {
		// Existing legacy artifact references are validated by their own path.
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return nil, acceptanceSourceRefusal(task, AcceptanceFaultContent, "adopted source path is invalid")
	}
	g := git.New(root)
	entry, present, err := g.TreeEntryAt(integrationCommit, path)
	if err != nil {
		return nil, acceptanceSourceRefusal(task, "", "cannot inspect integration source")
	}
	if !present {
		if task.AcceptanceSource == nil {
			return nil, nil
		}
		return nil, acceptanceSourceRefusal(task, AcceptanceFaultContent, "adopted source is missing from integration")
	}
	if entry.Mode != gitModeRegularFile && entry.Mode != gitModeExecutableFile {
		return nil, acceptanceSourceRefusal(task, AcceptanceFaultContent, "source must be a regular committed file")
	}
	content, _, err := readAcceptanceEntry(g, path, entry, present, nil)
	if err != nil {
		return nil, acceptanceSourceRefusal(task, readRefusalClass(err), err.Error())
	}
	return &declaredCarrier{ref: ref, path: path, heading: heading, content: content}, nil
}

// parseAcceptanceDeclaration parses the carrier's acceptance contract and checks
// it against the task: validation, safety, deferrals and the bounds of the
// references it depends on.
func parseAcceptanceDeclaration(root string, state *models.State, task *models.Task, carrier *declaredCarrier) (*acceptanceDeclaration, error) {
	content := func(reason string) error { return acceptanceSourceRefusal(task, AcceptanceFaultContent, reason) }
	contract, err := referencecontract.ParseAcceptance(carrier.content, carrier.heading)
	if err != nil {
		return nil, content(err.Error())
	}
	// ParseAcceptance above already resolved this heading, so neither can fail
	// here; the values are what the parent comparison and the stored identity
	// need.
	span, _ := carrierSpan(carrier.content, carrier.heading)
	spanIdentity, _ := carrierSpanIdentity(carrier.content, carrier.heading)
	if contract == nil {
		if task.AcceptanceSource != nil {
			return nil, content("adopted acceptance declaration was removed; downgrade is forbidden")
		}
		return nil, nil
	}
	if !reflect.DeepEqual(contract.Validation, task.Validation) {
		return nil, content(acceptanceValidationMismatchReason)
	}
	if err := models.ValidateValidationSafety("acceptance.validation", task.Validation, task.DestructiveDB); err != nil {
		return nil, content(err.Error())
	}
	deferred, err := acceptanceDeferrals(state.Config, contract, task.RuntimeInputs)
	if err != nil {
		return nil, content(err.Error())
	}
	refs, err := referencecontract.Parse(strings.ReplaceAll(carrier.content, "\r\n", "\n"))
	if err != nil || refs == nil {
		return nil, content("strict Source References are required")
	}
	if reason, readFailure := referenceBoundsRefusal(root, refs); reason != "" {
		if readFailure {
			return nil, acceptanceSourceRefusal(task, "", reason)
		}
		return nil, content(reason)
	}
	return &acceptanceDeclaration{ref: carrier.ref, path: carrier.path, heading: carrier.heading, contract: contract, deferred: deferred, span: span, spanIdentity: spanIdentity}, nil
}

// reviewDeclaredAllocation finds the one direct reviewed parent whose approved
// allocation still covers the declaration, and derives the source the task
// adopts from it.
func reviewDeclaredAllocation(g *git.Git, root string, state *models.State, task *models.Task, declared *acceptanceDeclaration, integrationCommit string) (*models.AcceptanceSource, error) {
	var source *models.AcceptanceSource
	var driftedReference string
	for _, parentID := range task.EffectiveParentTasks() {
		parent := state.FindTask(parentID)
		review := parentAllocationReview(g, root, state, task, parent, declared.path, declared.heading, declared.span, integrationCommit)
		if review == nil {
			continue
		}
		// The span excludes Source References, which is where the contract's
		// obligations and approved proofs resolve. Comparing only the span
		// would let an edit there redirect an approved reference at content no
		// reviewer saw, so every reference the reviewed contract depends on is
		// compared by what it resolves to, not by its declared ID.
		if alike, drifted := compareReviewedReferences(state, parent.ID, root, *review.ReviewCommit, integrationCommit, declared.path, declared.heading, declared.contract); !alike {
			if drifted != "" {
				driftedReference = drifted
			}
			continue
		}
		if source != nil {
			return nil, acceptanceSourceRefusal(task, AcceptanceFaultContent, "multiple reviewed parents claim the same allocation")
		}
		source = &models.AcceptanceSource{Ref: declared.ref, Commit: integrationCommit, Blob: declared.spanIdentity, ParentTask: parent.ID, ParentReviewCommit: *review.ReviewCommit}
	}
	if source == nil {
		if driftedReference != "" {
			return nil, acceptanceSourceRefusal(task, AcceptanceFaultAllocation, fmt.Sprintf("requires allocation by a direct independently approved merged planning parent; approved-proof reference %q has drifted — an authorized orchestrator may inspect the changed content and use %s to reaffirm this transition", driftedReference, brand.Command(reaffirmProofOperation, task.ID, driftedReference)))
		}
		return nil, acceptanceSourceRefusal(task, AcceptanceFaultAllocation, "requires allocation by a direct independently approved merged planning parent")
	}
	return source, nil
}

// keepAdoptedSourceCommit keeps the original carrier identity when unrelated
// integration work lands.
//
// Identity is the allocation itself, not the file that carries it. Reaching
// this point means the reviewed span and every reference the contract
// depends on still resolve to what was approved, so an edit elsewhere in
// the carrier leaves the adopted source intact. Comparing the whole-file
// blob here instead would re-derive on any carrier edit and strand an
// already-adopted child at reviewer assignment, where the stored source
// must still equal the derived one.
func keepAdoptedSourceCommit(g *git.Git, task *models.Task, source *models.AcceptanceSource, integrationCommit string) error {
	old := task.AcceptanceSource
	if old == nil || old.Ref != source.Ref || old.ParentTask != source.ParentTask || old.ParentReviewCommit != source.ParentReviewCommit {
		return nil
	}
	ancestor, err := g.IsAncestor(old.Commit, integrationCommit)
	if err != nil || !ancestor {
		return acceptanceSourceRefusal(task, AcceptanceFaultAllocation, "adopted source commit is no longer in integration ancestry")
	}
	source.Commit = old.Commit
	return nil
}
