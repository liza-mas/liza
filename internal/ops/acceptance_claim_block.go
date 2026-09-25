package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
)

// AcceptanceAllocationRefusalThreshold is how many consecutive identical
// allocation refusals a doer supervisor observes before escalating. It matches
// the reviewer claim breaker (ADR-0140) and, like it, describes the supervisor
// loop rather than the user's stack, so it is a constant.
const AcceptanceAllocationRefusalThreshold = 3

// acceptanceBlockedReasonLimit bounds the refusal text a blocked reason carries.
const acceptanceBlockedReasonLimit = 2000

type acceptanceClaimBlockTestHooks struct {
	afterIntegrationEqualityCheck func()
}

var testAcceptanceClaimBlockHooks *acceptanceClaimBlockTestHooks

// observeAcceptanceClaimRefusal marks an acceptance refusal as claim-stage and
// records what it validated against, so a supervisor can escalate it without
// re-deriving the observation from a later, possibly repaired, state.
func observeAcceptanceClaimRefusal(err error, state *models.State, task *models.Task, integrationCommit string) {
	var refusal *AcceptanceEvidenceError
	if !stderrors.As(err, &refusal) {
		return
	}
	refusal.Claim = &AcceptanceClaimObservation{
		AllocationRef:     acceptanceAllocationRef(task),
		IntegrationCommit: integrationCommit,
		Digest:            AcceptanceObservation(state, task),
	}
}

// AcceptanceObservation digests every state input acceptance validation reads
// for task: the whole task record, the whole record of each effective parent
// (a missing parent is recorded as such) and the proof reaffirmations recorded
// against those parents. Whole records rather than a field list keep it
// conservative: any change to them, including a repair, yields a different
// digest. It returns "" when the records cannot be encoded, which never matches.
func AcceptanceObservation(state *models.State, task *models.Task) string {
	if state == nil || task == nil {
		return ""
	}
	parentIDs := task.EffectiveParentTasks()
	parents := make([]any, 0, len(parentIDs))
	for _, id := range parentIDs {
		if parent := state.FindTask(id); parent != nil {
			parents = append(parents, parent)
		} else {
			parents = append(parents, "missing:"+id)
		}
	}
	var reaffirmations []models.ProofReaffirmation
	for _, reaffirmation := range state.ProofReaffirmations {
		if slices.Contains(parentIDs, reaffirmation.ParentTask) {
			reaffirmations = append(reaffirmations, reaffirmation)
		}
	}
	payload, err := json.Marshal(struct {
		Task           *models.Task
		Parents        []any
		Reaffirmations []models.ProofReaffirmation
	}{task, parents, reaffirmations})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// BlockAcceptanceRefusedTask escalates a claim-stage acceptance refusal to
// BLOCKED so the orchestrator is woken to repair it, instead of the task
// staying claimable and being refused on every poll (D63). It blocks only when
// the refusal is still current: integration has not moved from the commit the
// refusal validated against — checked under the same linearization integration
// merges take, and released before the state write — and the task, its parents
// and their reaffirmations are unchanged. A stale refusal returns false with no
// effect. Only content and allocation refusals escalate; the caller decides
// when an allocation refusal has repeated enough.
func BlockAcceptanceRefusedTask(projectRoot string, authority models.AgentAuthority, refusal *AcceptanceEvidenceError) (bool, error) {
	if refusal == nil || refusal.Claim == nil || refusal.Claim.Digest == "" {
		return false, nil
	}
	reason, question, ok := acceptanceRefusalBlockText(refusal)
	if !ok {
		return false, nil
	}
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return false, err
	}
	pipelineTransitions := BuildPipelineTransitions(resolver)
	bb := db.For(paths.New(projectRoot).StatePath())
	state, err := bb.Read()
	if err != nil {
		return false, err
	}
	integrationBranch := state.Config.IntegrationBranch
	taskID := refusal.TaskID

	blocked := false
	err = withEffectiveIntegrationCompletionLinearization(projectRoot, "block acceptance-refused task "+taskID, func() error {
		var current string
		if lockErr := withIntegrationMutationLock(projectRoot, "verify acceptance refusal integration "+taskID, func() error {
			var resolveErr error
			current, resolveErr = git.New(projectRoot).GetCommitSHA(integrationBranch)
			return resolveErr
		}); lockErr != nil {
			return fmt.Errorf("verify acceptance refusal integration ref: %w", lockErr)
		}
		if current != refusal.Claim.IntegrationCommit {
			return nil
		}
		if testAcceptanceClaimBlockHooks != nil && testAcceptanceClaimBlockHooks.afterIntegrationEqualityCheck != nil {
			testAcceptanceClaimBlockHooks.afterIntegrationEqualityCheck()
		}
		return lifecycleMutation(bb, &authority)(func(state *models.State) error {
			// Reset per attempt: a re-run transaction reports its own outcome.
			blocked = false
			task := state.FindTask(taskID)
			if task == nil || !acceptanceRefusalBlockableStatus(task, resolver) {
				return nil
			}
			if AcceptanceObservation(state, task) != refusal.Claim.Digest {
				return nil
			}
			if err := task.TransitionWith(models.TaskStatusBlocked, pipelineTransitions); err != nil {
				return err
			}
			task.BlockedReason = &reason
			task.BlockedQuestions = []string{question}
			task.AssignedTo = nil
			task.LeaseExpires = nil
			models.AdvanceLifecycle(task)
			releaseAgentsForTask(state, taskID)
			task.History = append(task.History, models.TaskHistoryEntry{
				Time:   time.Now().UTC(),
				Event:  models.TaskEventBlocked,
				Agent:  &authority.ID,
				Reason: &reason,
				Extra: map[string]any{
					"claim_refusal":      "acceptance_evidence",
					"fault_class":        string(refusal.Class),
					"integration_commit": refusal.Claim.IntegrationCommit,
				},
			})
			blocked = true
			return nil
		})
	})
	if err != nil {
		return false, err
	}
	return blocked, nil
}

// acceptanceRefusalBlockText renders the blocked reason and the orchestrator's
// question. A content refusal asserts the allocation is invalid; an allocation
// refusal only reports that it repeated, since its helpers cannot tell an
// invalid allocation from a failed read.
func acceptanceRefusalBlockText(refusal *AcceptanceEvidenceError) (string, string, bool) {
	fault := fmt.Sprintf("%s (%s): %s", refusal.Field, refusal.Claim.AllocationRef, refusal.Reason)
	unblock := brand.Command("unblock-task", refusal.TaskID)
	var reason, question string
	switch refusal.Class {
	case AcceptanceFaultContent:
		reason = "acceptance_evidence_invalid: " + fault
		question = fmt.Sprintf("Correct the task's acceptance allocation (plan_ref/spec_ref, validation, parent tasks) to match the reviewed section — normally with %s, which supersedes this task — or repair its integration-side cause and, keeping this task, run %s to restore it for a new claim.", brand.Command("replace-task"), unblock)
	case AcceptanceFaultAllocation:
		reason = fmt.Sprintf("acceptance_claim_refused_repeatedly: %d identical refusals at %s: %s — may be an invalid allocation or a persistent repository read failure", AcceptanceAllocationRefusalThreshold, refusal.Claim.IntegrationCommit, fault)
		question = fmt.Sprintf("Check the allocation (parent tasks and their approval and output, plan_ref, validation, proof reaffirmations) and repository health; replace this task with %s, or repair the cause and, keeping this task, run %s to restore it for a new claim.", brand.Command("replace-task"), unblock)
	default:
		return "", "", false
	}
	reason = truncateForDiagnostics(acceptanceExecutionMask(os.Environ())(reason), acceptanceBlockedReasonLimit)
	return reason, question, true
}

// acceptanceRefusalBlockableStatus reports whether task is where a doer claim
// finds it: its role-pair initial status, or rejected awaiting a reclaim.
func acceptanceRefusalBlockableStatus(task *models.Task, resolver *pipeline.Resolver) bool {
	if task.RolePair == "" {
		return task.Status == models.TaskStatusReady || task.Status == models.TaskStatusRejected
	}
	if initial, err := resolver.InitialStatus(task.RolePair); err == nil && task.Status == initial {
		return true
	}
	rejected, err := resolver.RejectedStatus(task.RolePair)
	return err == nil && task.Status == rejected
}
