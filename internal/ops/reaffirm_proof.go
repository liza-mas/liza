package ops

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/statehygiene"
)

// ReaffirmProofResult reports what the decision was recorded against, so the
// caller can show which transition it authorized rather than only that it
// succeeded.
type ReaffirmProofResult struct {
	TaskID          string `json:"task_id"`
	ParentTask      string `json:"parent_task"`
	CarrierPath     string `json:"carrier_path"`
	Heading         string `json:"heading"`
	ReferenceID     string `json:"reference_id"`
	ReviewedSection string `json:"reviewed_section"`
	CurrentSection  string `json:"current_section"`
}

// ReaffirmProof records an authorized decision that one approved proof still
// holds against content that changed after the approval.
//
// Both section identities are derived here, from the reviewed carrier and from
// integration, and never taken from the caller as the values to record. The
// caller supplies expectedSection only as a precondition: the identity it
// inspected. Deriving without that check would record whatever integration
// happens to hold at invocation, so a merge landing between inspection and
// invocation would silently authorize content nobody looked at — the failure
// this record exists to prevent. On mismatch nothing is recorded.
func ReaffirmProof(projectRoot, taskID, referenceID, expectedSection, reason string, authority models.AgentAuthority) (*ReaffirmProofResult, error) {
	if strings.TrimSpace(referenceID) == "" {
		return nil, &PreconditionError{Reason: "re-affirmation requires the reference ID the proof cites"}
	}
	if strings.TrimSpace(reason) == "" || len(reason) > statehygiene.MaxStateTextBytes || !utf8.ValidString(reason) {
		return nil, &PreconditionError{Reason: "re-affirmation requires a nonempty UTF-8 reason of at most 4096 bytes"}
	}

	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, err
	}
	var result *ReaffirmProofResult
	err = db.For(paths.New(projectRoot).StatePath()).Modify(func(state *models.State) error {
		if err := RequireAgentAuthority(state, authority); err != nil {
			return err
		}
		capabilities, err := resolver.EffectiveRoleCapabilities(state.Agents[authority.ID].Role)
		if err != nil || capabilities.RoleType != "orchestrator" || !slices.Contains(capabilities.AllowedOperations, "reaffirm-proof") {
			return &PreconditionError{Reason: "reaffirm-proof requires a registered orchestrator with the reaffirm-proof capability"}
		}
		task := state.FindTask(taskID)
		if task == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %s not found", taskID)}
		}
		observed, err := observeProofDrift(projectRoot, state, task, referenceID)
		if err != nil {
			return err
		}
		if expectedSection != observed.CurrentSection {
			if strings.TrimSpace(expectedSection) == "" {
				return &PreconditionError{Reason: fmt.Sprintf(
					"re-affirmation must name the section identity it inspected; %s#%s currently resolves reference %q to %s",
					observed.CarrierPath, observed.Heading, referenceID, observed.CurrentSection)}
			}
			return &PreconditionError{Reason: fmt.Sprintf(
				"the inspected content is no longer current: expected %s, integration now resolves reference %q to %s; inspect the current content and re-run",
				expectedSection, referenceID, observed.CurrentSection)}
		}
		if existing := state.FindProofReaffirmation(observed.ParentTask, observed.CarrierPath, observed.Heading,
			referenceID, observed.ReviewedSection, observed.CurrentSection); existing != nil {
			// Idempotent: the same transition is already authorized. Appending
			// again would grow state without changing what is permitted.
			result = observed
			return nil
		}
		state.ProofReaffirmations = append(state.ProofReaffirmations, models.ProofReaffirmation{
			ParentTask:      observed.ParentTask,
			CarrierPath:     observed.CarrierPath,
			Heading:         observed.Heading,
			ReferenceID:     referenceID,
			ReviewedSection: observed.ReviewedSection,
			CurrentSection:  observed.CurrentSection,
			Actor:           authority.ID,
			Timestamp:       time.Now().UTC(),
			Reason:          reason,
		})
		result = observed
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// observeProofDrift resolves the transition a re-affirmation would authorize,
// and refuses every case where there is nothing to decide: a task with no
// allocation, a reference the contract asserts no proof against, a reference
// that does not resolve on both sides, or content that has not moved.
func observeProofDrift(projectRoot string, state *models.State, task *models.Task, referenceID string) (*ReaffirmProofResult, error) {
	// The same selection acceptance uses, so recovery works before a source is
	// adopted and for allocations carried by spec_ref.
	ref := acceptanceAllocationRef(task)
	path, heading := paths.SplitRefFile(ref), paths.SplitRefFragment(ref)
	if path == "" || heading == "" {
		return nil, &PreconditionError{Reason: fmt.Sprintf("task %s names no allocation section to re-affirm against", task.ID)}
	}

	integrationCommit, err := git.New(projectRoot).ResolveCommit(state.Config.IntegrationBranch)
	if err != nil {
		return nil, err
	}
	parent, reviewCommit, err := acceptanceReviewParent(projectRoot, state, task, path, heading, integrationCommit)
	if err != nil {
		return nil, err
	}

	reviewedContent, _, err := readAcceptanceBlob(projectRoot, reviewCommit, path)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot read reviewed carrier %s", path)}
	}
	contract, err := referencecontract.ParseAcceptance(reviewedContent, heading)
	if err != nil || contract == nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("reviewed carrier %s declares no acceptance contract at %q", path, heading)}
	}
	asserted := false
	for _, proof := range contract.ApprovedProofs {
		if proof.ReferenceID == referenceID {
			asserted = true
			break
		}
	}
	if !asserted {
		return nil, &PreconditionError{Reason: fmt.Sprintf("the reviewed contract asserts no approved proof against reference %q; only proofs are compared by content", referenceID)}
	}

	reviewedRefs, ok := carrierReferences(projectRoot, reviewCommit, path)
	if !ok {
		return nil, &PreconditionError{Reason: fmt.Sprintf("reviewed carrier %s has no readable Source References", path)}
	}
	integrationRefs, ok := carrierReferences(projectRoot, integrationCommit, path)
	if !ok {
		return nil, &PreconditionError{Reason: fmt.Sprintf("carrier %s has no readable Source References at integration", path)}
	}
	reviewed, reviewedFound := resolveDeclaredReference(projectRoot, reviewedRefs, referenceID)
	current, currentFound := resolveDeclaredReference(projectRoot, integrationRefs, referenceID)
	if !reviewedFound || !currentFound {
		return nil, &PreconditionError{Reason: fmt.Sprintf("reference %q does not resolve at both the reviewed commit and integration; a missing or unresolvable reference is not a drift decision", referenceID)}
	}
	if reviewed == current {
		return nil, &PreconditionError{Reason: fmt.Sprintf("reference %q already resolves to the approved content; there is nothing to re-affirm", referenceID)}
	}
	return &ReaffirmProofResult{
		TaskID: task.ID, ParentTask: parent, CarrierPath: path, Heading: heading, ReferenceID: referenceID,
		ReviewedSection: spanObjectID(reviewed), CurrentSection: spanObjectID(current),
	}, nil
}

// acceptanceReviewParent returns the merged planning parent that actually
// allocates this task, using the same predicate acceptance uses.
//
// Ordering is not a selection criterion: a child with several merged parents
// must be re-affirmed against the one whose allocation is being refused, or the
// grant lands on a parent that never allocated it and the task stays refused.
func acceptanceReviewParent(projectRoot string, state *models.State, task *models.Task, path, heading, integrationCommit string) (string, string, error) {
	integrationContent, _, err := readAcceptanceBlob(projectRoot, integrationCommit, path)
	if err != nil {
		return "", "", &PreconditionError{Reason: fmt.Sprintf("cannot read carrier %s at integration", path)}
	}
	integrationSpan, ok := carrierSpan(integrationContent, heading)
	if !ok {
		return "", "", &PreconditionError{Reason: fmt.Sprintf("carrier %s has no unique section %q at integration", path, heading)}
	}
	g := git.New(projectRoot)
	var parentID, reviewCommit string
	for _, candidateID := range task.EffectiveParentTasks() {
		candidate := state.FindTask(candidateID)
		if !parentAllocatesTask(g, projectRoot, task, candidate, path, heading, integrationSpan, integrationCommit) {
			continue
		}
		if parentID != "" {
			return "", "", &PreconditionError{Reason: fmt.Sprintf("task %s has multiple parents claiming the same allocation; acceptance refuses that state and re-affirmation cannot choose between them", task.ID)}
		}
		parentID, reviewCommit = candidate.ID, *candidate.ReviewCommit
	}
	if parentID == "" {
		return "", "", &PreconditionError{Reason: fmt.Sprintf("task %s has no merged planning parent that allocates it; the refusal is not an approved-proof drift and re-affirmation cannot clear it", task.ID)}
	}
	return parentID, reviewCommit, nil
}
