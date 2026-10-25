package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

type AmendPlanInput struct {
	TaskID, Reason, Apply, ReplacePending, WithdrawPending, ChangedBy string
	Authority                                                         *models.AgentAuthority
	Mode                                                              models.PlanAmendmentMode
	Trigger                                                           string
}

type AmendPlanResult struct {
	TaskID       string `json:"task_id"`
	CorrectionID string `json:"correction_id"`
	Changed      bool   `json:"changed"`
}

// AmendPlan fences a live source, requests normal independent review, and
// adopts only that reviewed correction. Expanded sources require an explicit
// bounded architecture mode; provider and output identities remain unchanged.
func AmendPlan(root string, input AmendPlanInput) (*AmendPlanResult, error) {
	resolver, _, err := loadResolver(root)
	if err != nil {
		return nil, err
	}
	actor, err := checkAmendPlanRequest(resolver, input)
	if err != nil {
		return nil, err
	}
	bb := db.For(paths.New(root).StatePath())
	result := &AmendPlanResult{TaskID: input.TaskID}
	mutate := func(state *models.State) error {
		*result = AmendPlanResult{TaskID: input.TaskID}
		original := state.FindTask(input.TaskID)
		switch {
		case input.Apply != "":
			return applyPlanAmendment(root, state, resolver, original, input.Apply, actor, result, bb)
		case input.WithdrawPending != "":
			return withdrawPendingAmendment(root, state, original, input.WithdrawPending, actor, input.Reason, result, bb)
		}
		return beginPlanAmendment(root, state, resolver, original, input, actor, result, bb)
	}
	if input.Authority != nil {
		err = ModifyWithAgentAuthority(bb, *input.Authority, mutate)
	} else {
		err = bb.Modify(mutate)
	}
	if err != nil {
		return nil, fmt.Errorf("amend-plan failed: %w", err)
	}
	return result, nil
}

// checkAmendPlanRequest runs every refusal that needs no state, in the order
// callers have always met them, and returns the acting identity.
func checkAmendPlanRequest(resolver *pipeline.Resolver, input AmendPlanInput) (string, error) {
	actor := input.ChangedBy
	if input.Authority != nil {
		actor = input.Authority.ID
		if err := requireOrchestratorType(resolver, actor); err != nil {
			return "", err
		}
	}
	actions := 0
	for _, action := range []string{input.Apply, input.ReplacePending, input.WithdrawPending} {
		if action != "" {
			actions++
		}
	}
	if actor == "" || input.TaskID == "" || actions > 1 {
		return "", &PreconditionError{Reason: "amend-plan requires a task, actor and one action"}
	}
	if input.WithdrawPending != "" && input.Authority != nil {
		return "", &PreconditionError{Reason: "withdraw-pending is operator-only"}
	}
	if !input.Mode.IsValid() || (input.Apply != "" || input.WithdrawPending != "") && input.Mode != "" {
		return "", &PreconditionError{Reason: "select a valid amendment mode only when beginning a correction"}
	}
	if input.Apply == "" && (strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096) {
		return "", &PreconditionError{Reason: "amend-plan requires a nonempty reason of at most 4096 bytes"}
	}
	return actor, nil
}

func unusedAmendmentSource(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	return amendmentSource(state, resolver, task, "")
}

func amendmentSource(state *models.State, resolver *pipeline.Resolver, task *models.Task, mode models.PlanAmendmentMode) error {
	if task == nil || task.Status != models.TaskStatusMerged || task.AmendsPlan != "" || len(task.Output) == 0 || task.PlanHandoffRetired() || task.TransitionsExecuted[models.ReplannedTransition] {
		return &PreconditionError{Reason: "amendment requires a live MERGED planning original"}
	}
	if mode == "" && (len(task.TransitionsExecuted) > 0 || hasPlanChildren(state, task)) {
		return &PreconditionError{Reason: "amendment requires an unused MERGED planning original without children or executed transitions"}
	}
	if mode == models.PlanAmendmentContract && task.EffectiveType() != models.TaskTypeArchitecture || mode != "" && (len(task.TransitionsExecuted) > 0 || hasPlanChildren(state, task)) && task.EffectiveType() != models.TaskTypeArchitecture {
		return &PreconditionError{Reason: "expanded contract correction requires an architecture original"}
	}
	if !resolver.TransitionSourcePairs()[task.RolePair] {
		return &PreconditionError{Reason: "amendment requires a planning role-pair"}
	}
	return nil
}

func hasPlanChildren(state *models.State, task *models.Task) bool {
	for i := range state.Tasks {
		if slices.Contains(state.Tasks[i].EffectiveParentTasks(), task.ID) {
			return true
		}
	}
	return false
}

func cloneAmendmentOutput(output []models.OutputEntry) []models.OutputEntry {
	// OutputEntry is a serializable state value. Copy nested declarations too:
	// retained allocation history must never alias later prospective edits.
	data, _ := json.Marshal(output)
	var copied []models.OutputEntry
	_ = json.Unmarshal(data, &copied)
	return copied
}

// withdrawPendingAmendment releases a fence only when nothing since the last
// applied correction reached integration. ABANDONED alone does not prove it: a
// skipped rollback or a cancellation after the ref moved keeps the commit, so
// every commit those corrections recorded must be absent from integration
// ancestry. Earlier corrections were reconciled by the applied review (D-108).
func withdrawPendingAmendment(root string, state *models.State, original *models.Task, id, actor, reason string, result *AmendPlanResult, bb *db.Blackboard) error {
	if original == nil || original.PlanAmendment == nil || original.PlanAmendment.Pending != id {
		return &PreconditionError{Reason: "withdraw-pending must name the exact pending correction"}
	}
	record := original.PlanAmendment
	since := record.Corrections
	if n := len(record.Applied); n > 0 {
		since = since[slices.Index(since, record.Applied[n-1])+1:]
	}
	g := git.New(root)
	head, err := g.GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return err
	}
	for _, correctionID := range since {
		correction := state.FindTask(correctionID)
		if correction == nil || correction.Status != models.TaskStatusAbandoned {
			return &PreconditionError{Reason: fmt.Sprintf("withdraw-pending requires every correction since the last applied one ABANDONED; %s may carry merged changes: use --replace-pending", correctionID)}
		}
		for _, entry := range correction.History {
			if entry.Commit == nil {
				continue
			}
			if carried, err := g.IsAncestor(*entry.Commit, head); err != nil || carried {
				return &PreconditionError{Reason: fmt.Sprintf("withdraw-pending cannot prove %s left integration unchanged (commit %s): use --replace-pending", correctionID, *entry.Commit)}
			}
		}
	}
	record.Pending = ""
	record.Quarantined = append(record.Quarantined, id)
	note := fmt.Sprintf("pending correction withdrawn: %s: %s", id, strings.TrimSpace(reason))
	original.History = append(original.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventPlanAmendment, Agent: &actor, Note: &note})
	if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, root, true, os.Stderr); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("withdraw-pending candidate: %v", err)}
	}
	result.CorrectionID, result.Changed = id, true
	return nil
}

func applyPlanAmendment(root string, state *models.State, resolver *pipeline.Resolver, original *models.Task, id, actor string, result *AmendPlanResult, bb *db.Blackboard) error {
	if original == nil {
		return &PreconditionError{Reason: "original task not found"}
	}
	record := original.PlanAmendment
	if record != nil && slices.Contains(record.Applied, id) {
		if record.Applied[len(record.Applied)-1] != id {
			return &PreconditionError{Reason: "only the latest applied correction can replay"}
		}
		if _, err := EffectivePlanReview(state, original); err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		result.CorrectionID = id
		return nil
	}
	if record == nil || record.Pending != id {
		return &PreconditionError{Reason: "apply must name the exact pending correction"}
	}
	correction := state.FindTask(id)
	if correction == nil || correction.Status != models.TaskStatusMerged || correction.AmendsPlan != original.ID || correction.RolePair != original.RolePair || correction.PlanHandoffRetired() || correction.PlanCheckVerdictOf() == models.PlanCheckHeld || len(correction.TransitionsExecuted) > 0 || hasPlanChildren(state, correction) {
		return &PreconditionError{Reason: "apply requires the independent MERGED pending correction without children, retirement or hold"}
	}
	if err := amendmentSource(state, resolver, original, correction.AmendmentMode); err != nil {
		return err
	}
	if err := models.ValidateAmendmentOutputForMode(original.Output, correction.Output, correction.AmendmentMode); err != nil {
		return &PreconditionError{Reason: err.Error()}
	}
	if correction.MaxOutputs != original.MaxOutputs || original.MaxOutputs > 0 && len(correction.Output) > original.MaxOutputs {
		return &PreconditionError{Reason: "amendment cannot exceed the original max_outputs cap"}
	}
	g := git.New(root)
	if correction.BaseCommit == nil || correction.ReviewCommit == nil || correction.MergeCommit == nil || acceptanceParentAuthor(correction) == "" || !validAcceptanceParentHistory(g, correction) {
		return &PreconditionError{Reason: "correction lacks immutable reviewed merge ancestry"}
	}
	author := acceptanceParentAuthor(correction)
	approved := correction.ApprovedBy != nil && *correction.ApprovedBy != "" && *correction.ApprovedBy != author
	for _, approval := range correction.Approvals {
		approved = approved || approval.Agent != "" && approval.Agent != author
	}
	if !approved {
		return &PreconditionError{Reason: "correction requires independent approval"}
	}
	head, err := g.GetCommitSHA(state.Config.IntegrationBranch)
	if err != nil {
		return err
	}
	ancestor, err := g.IsAncestor(*correction.MergeCommit, head)
	if err != nil || !ancestor {
		return &PreconditionError{Reason: "correction merge is not in integration ancestry"}
	}
	if stale := staleOutputDeclaration(state, resolver, correction.Output); stale != "" {
		return &PreconditionError{Reason: stale}
	}
	if err := checkOutputRuntimeInputs(state, resolver, original, correction.Output); err != nil {
		return err
	}
	if err := validateBoundedContractCorrection(root, state, original, correction, *correction.ReviewCommit); err != nil {
		return err
	}
	original.Output = cloneAmendmentOutput(correction.Output)
	record.Applied = append(record.Applied, id)
	record.Pending = ""
	if original.PlanCheckVerdictOf() != models.PlanCheckHeld {
		original.PlanCheck = nil
	}
	now := time.Now().UTC()
	correction.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: original.ID, By: actor, At: now}
	if err := ValidatePlanReviewHistory(root, state, original, head); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("amendment review authority: %v", err)}
	}
	if err := ValidatePlanAmendmentArtifacts(root, state, original, head); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("amendment artifact drift: %v", err)}
	}
	if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("amendment provider graph: %v", err)}
	}
	note := "reviewed correction applied: " + id
	original.History = append(original.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventPlanAmendment, Agent: &actor, Note: &note})
	if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, root, true, os.Stderr); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("amendment candidate: %v", err)}
	}
	result.CorrectionID, result.Changed = id, true
	return nil
}
