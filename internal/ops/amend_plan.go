package ops

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

type AmendPlanInput struct {
	TaskID, Reason, Apply, ReplacePending, ChangedBy string
	Authority                                        *models.AgentAuthority
}

type AmendPlanResult struct {
	TaskID       string `json:"task_id"`
	CorrectionID string `json:"correction_id"`
	Changed      bool   `json:"changed"`
}

// AmendPlan fences an unused source, requests normal independent review, and
// adopts only that reviewed correction. It never retires the provider identity.
func AmendPlan(root string, input AmendPlanInput) (*AmendPlanResult, error) {
	resolver, _, err := loadResolver(root)
	if err != nil {
		return nil, err
	}
	actor := input.ChangedBy
	if input.Authority != nil {
		actor = input.Authority.ID
		if err := requireOrchestratorType(resolver, actor); err != nil {
			return nil, err
		}
	}
	if actor == "" || input.TaskID == "" || input.Apply != "" && input.ReplacePending != "" {
		return nil, &PreconditionError{Reason: "amend-plan requires a task, actor and one action"}
	}
	if input.Apply == "" && (strings.TrimSpace(input.Reason) == "" || len(input.Reason) > 4096) {
		return nil, &PreconditionError{Reason: "amend-plan requires a nonempty reason of at most 4096 bytes"}
	}
	bb := db.For(paths.New(root).StatePath())
	result := &AmendPlanResult{TaskID: input.TaskID}
	mutate := func(state *models.State) error {
		*result = AmendPlanResult{TaskID: input.TaskID}
		original := state.FindTask(input.TaskID)
		if input.Apply != "" {
			return applyPlanAmendment(root, state, resolver, original, input.Apply, actor, result, bb)
		}
		if err := unusedAmendmentSource(state, resolver, original); err != nil {
			return err
		}
		if input.ReplacePending == "" {
			if original.PlanCheckVerdictOf() == models.PlanCheckHeld {
				return &PreconditionError{Reason: "human hold requires operator plan-check --clear before beginning an amendment"}
			}
			if original.PlanAmendment != nil && original.PlanAmendment.Pending != "" {
				return &PreconditionError{Reason: "a correction is already pending; apply it or replace-pending after merge/cancellation"}
			}
		} else {
			if original.PlanAmendment == nil || original.PlanAmendment.Pending != input.ReplacePending {
				return &PreconditionError{Reason: "replace-pending must name the exact pending correction"}
			}
			old := state.FindTask(input.ReplacePending)
			if old == nil || old.AmendsPlan != original.ID || old.Status != models.TaskStatusMerged && old.Status != models.TaskStatusAbandoned || slices.Contains(original.PlanAmendment.Applied, old.ID) || hasPlanChildren(state, old) {
				return &PreconditionError{Reason: "replace-pending requires an unapplied MERGED or ABANDONED correction without children"}
			}
			if old.Status == models.TaskStatusMerged {
				old.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: original.ID, By: actor, At: time.Now().UTC()}
			}
			original.PlanAmendment.Quarantined = append(original.PlanAmendment.Quarantined, old.ID)
		}
		initial, err := resolver.InitialStatus(original.RolePair)
		if err != nil {
			return err
		}
		if original.PlanAmendment == nil {
			original.PlanAmendment = &models.PlanAmendment{OriginalOutput: cloneAmendmentOutput(original.Output)}
		}
		id := ""
		for n := 1; id == ""; n++ {
			candidate := fmt.Sprintf("%s-amend-%d", original.ID, n)
			if state.FindTask(candidate) == nil {
				id = candidate
			}
		}
		now := time.Now().UTC()
		correction := models.Task{ID: id, Type: models.TaskTypePlanning, RolePair: original.RolePair, Status: initial, Priority: original.Priority,
			AmendsPlan: original.ID, Description: fmt.Sprintf("%s\n\nReviewed amendment of %s: %s. Read its current output manifest. Preserve every existing output slot identity; append new producers only. Reconcile prior merged correction artifacts. After independent review/merge run %s.", original.Description, original.ID, strings.TrimSpace(input.Reason), brand.Command("amend-plan", original.ID, "--apply", id)),
			SpecRef: original.SpecRef, EpicRef: original.EpicRef, PlanRef: original.PlanRef, ArchRef: original.ArchRef, DoneWhen: original.DoneWhen, Scope: original.Scope,
			ParentTask: original.ParentTask, ParentTasks: slices.Clone(original.ParentTasks), DependsOn: slices.Clone(original.DependsOn),
			Validation: slices.Clone(original.Validation), ValidationPrerequisites: models.CloneValidationPrerequisites(original.ValidationPrerequisites), RuntimeInputs: models.CloneRuntimeInputs(original.RuntimeInputs), DestructiveDB: original.DestructiveDB, RCARequired: original.RCARequired,
			ProviderDependencies: models.CloneProviderDependencies(original.ProviderDependencies), DescendantDependencies: models.CloneDescendantDependencies(original.DescendantDependencies), ProviderReservations: slices.Clone(original.ProviderReservations), MaxOutputs: original.MaxOutputs,
			Created: now, History: []models.TaskHistoryEntry{}}
		original.PlanAmendment.Pending = id
		original.PlanAmendment.Corrections = append(original.PlanAmendment.Corrections, id)
		if original.PlanCheckVerdictOf() != models.PlanCheckHeld {
			original.PlanCheck = nil
		}
		note := "reviewed correction pending: " + id
		original.History = append(original.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventPlanAmendment, Agent: &actor, Note: &note})
		state.Tasks = append(state.Tasks, correction)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, id)
		if state.Sprint.Status == models.SprintStatusCheckpoint {
			state.Sprint.Status = models.SprintStatusInProgress
			state.Sprint.CheckpointTrigger = ""
		}
		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, root, true, os.Stderr); err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("amend-plan candidate: %v", err)}
		}
		result.CorrectionID, result.Changed = id, true
		return nil
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

func unusedAmendmentSource(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	if task == nil || task.Status != models.TaskStatusMerged || task.AmendsPlan != "" || len(task.Output) == 0 || task.PlanHandoffRetired() || len(task.TransitionsExecuted) > 0 || hasPlanChildren(state, task) {
		return &PreconditionError{Reason: "amendment requires an unused MERGED planning original without children or executed transitions"}
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
	if err := unusedAmendmentSource(state, resolver, original); err != nil {
		return err
	}
	if record == nil || record.Pending != id {
		return &PreconditionError{Reason: "apply must name the exact pending correction"}
	}
	correction := state.FindTask(id)
	if correction == nil || correction.Status != models.TaskStatusMerged || correction.AmendsPlan != original.ID || correction.RolePair != original.RolePair || correction.PlanHandoffRetired() || correction.PlanCheckVerdictOf() == models.PlanCheckHeld || len(correction.TransitionsExecuted) > 0 || hasPlanChildren(state, correction) {
		return &PreconditionError{Reason: "apply requires the independent MERGED pending correction without children, retirement or hold"}
	}
	if err := models.ValidateAmendmentOutput(original.Output, correction.Output); err != nil {
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
