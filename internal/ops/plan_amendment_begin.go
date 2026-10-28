package ops

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// Correction ID suffixes: an ordinary correction is "<original>-amend-N", an
// identity-preserving one is "<original>-replan-N".
const (
	amendCorrectionIDSuffix  = "amend"
	replanCorrectionIDSuffix = "replan"
)

// beginPlanAmendment fences a live source and commissions its reviewed
// correction inside the caller's locked transaction. The steps keep the order
// the refusals were always reached in: mode, source, hold/pending (or replace),
// construction, fence adoption, full-candidate validation.
func beginPlanAmendment(root string, state *models.State, resolver *pipeline.Resolver, original *models.Task, input AmendPlanInput, actor string, result *AmendPlanResult, bb *db.Blackboard) error {
	mode, err := resolveAmendmentMode(state, input)
	if err != nil {
		return err
	}
	if mode == models.PlanAmendmentPreserveIdentity && state.Sprint.Status != models.SprintStatusCheckpoint && state.Sprint.Status != models.SprintStatusInProgress {
		return &PreconditionError{Reason: "sprint must be at CHECKPOINT or IN_PROGRESS"}
	}
	if err := amendmentSource(state, resolver, original, mode); err != nil {
		return err
	}
	if err := checkAmendmentBeginEligibility(state, original, input.ReplacePending, actor); err != nil {
		return err
	}
	initial, err := resolver.InitialStatus(original.RolePair)
	if err != nil {
		return err
	}
	if original.PlanAmendment == nil {
		original.PlanAmendment = &models.PlanAmendment{OriginalOutput: cloneAmendmentOutput(original.Output)}
	}
	correction := newAmendmentCorrection(state, original, mode, input, initial)
	adoptAmendmentCorrection(state, original, correction, actor)
	if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, root, true, os.Stderr); err != nil {
		return &PreconditionError{Reason: fmt.Sprintf("amend-plan candidate: %v", err)}
	}
	result.CorrectionID, result.Changed = correction.ID, true
	return nil
}

// resolveAmendmentMode returns the requested mode; a replacement must keep the
// mode of the pending correction it replaces.
func resolveAmendmentMode(state *models.State, input AmendPlanInput) (models.PlanAmendmentMode, error) {
	mode := input.Mode
	if input.ReplacePending == "" {
		return mode, nil
	}
	if old := state.FindTask(input.ReplacePending); old != nil {
		if mode == "" {
			mode = old.AmendmentMode
		}
		if mode != old.AmendmentMode {
			return "", &PreconditionError{Reason: "replacement must preserve pending amendment mode"}
		}
	}
	return mode, nil
}

// checkAmendmentBeginEligibility refuses a begin over a human hold or an
// existing pending correction, or quarantines the exact pending correction a
// replacement names. A hold is never cleared here.
func checkAmendmentBeginEligibility(state *models.State, original *models.Task, replacePending, actor string) error {
	if replacePending == "" {
		if original.PlanCheckVerdictOf() == models.PlanCheckHeld {
			return &PreconditionError{Reason: "human hold requires operator plan-check --clear before beginning an amendment"}
		}
		if original.PlanAmendment != nil && original.PlanAmendment.Pending != "" {
			return &PreconditionError{Reason: "a correction is already pending; apply it or replace-pending after merge/cancellation"}
		}
		return nil
	}
	if original.PlanAmendment == nil || original.PlanAmendment.Pending != replacePending {
		return &PreconditionError{Reason: "replace-pending must name the exact pending correction"}
	}
	old := state.FindTask(replacePending)
	if old == nil || old.AmendsPlan != original.ID || old.Status != models.TaskStatusMerged && old.Status != models.TaskStatusAbandoned || slices.Contains(original.PlanAmendment.Applied, old.ID) || hasPlanChildren(state, old) {
		return &PreconditionError{Reason: "replace-pending requires an unapplied MERGED or ABANDONED correction without children"}
	}
	if old.Status == models.TaskStatusMerged {
		old.PlanCheck = &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: original.ID, By: actor, At: time.Now().UTC()}
	}
	original.PlanAmendment.Quarantined = append(original.PlanAmendment.Quarantined, old.ID)
	return nil
}

// newAmendmentCorrection builds the correction task: a copy of the original's
// scheduling, validation and declaration fields with its own ID and review
// description. Nested declarations are cloned so the two never alias.
func newAmendmentCorrection(state *models.State, original *models.Task, mode models.PlanAmendmentMode, input AmendPlanInput, initial models.TaskStatus) models.Task {
	suffix, kind := amendCorrectionIDSuffix, models.PlanningChangeCorrection
	if mode == models.PlanAmendmentPreserveIdentity {
		suffix, kind = replanCorrectionIDSuffix, models.PlanningChangeReplan
	}
	id := nextCorrectionID(state, original.ID, suffix)
	correctionType := models.TaskTypePlanning
	if mode != "" {
		correctionType = original.EffectiveType()
	}
	return models.Task{ID: id, Type: correctionType, RolePair: original.RolePair, Status: initial, Priority: original.Priority,
		AmendmentMode: mode, PlanningChange: models.NewPlanningChange(kind, input.Trigger, original.ID),
		AmendsPlan: original.ID, Description: amendmentDescription(original, mode, correctionType, id, input.Reason),
		SpecRef: original.SpecRef, EpicRef: original.EpicRef, PlanRef: original.PlanRef, ArchRef: original.ArchRef, DoneWhen: original.DoneWhen, Scope: original.Scope,
		ParentTask: original.ParentTask, ParentTasks: slices.Clone(original.ParentTasks), DependsOn: slices.Clone(original.DependsOn),
		Validation: slices.Clone(original.Validation), ValidationPrerequisites: models.CloneValidationPrerequisites(original.ValidationPrerequisites), RuntimeInputs: models.CloneRuntimeInputs(original.RuntimeInputs), DestructiveDB: original.DestructiveDB, RCARequired: original.RCARequired,
		ProviderDependencies: models.CloneProviderDependencies(original.ProviderDependencies), DescendantDependencies: models.CloneDescendantDependencies(original.DescendantDependencies), ProviderReservations: slices.Clone(original.ProviderReservations), MaxOutputs: original.MaxOutputs,
		Created: time.Now().UTC(), History: []models.TaskHistoryEntry{}}
}

// nextCorrectionID is the first unused "<original>-<suffix>-N".
func nextCorrectionID(state *models.State, originalID, suffix string) string {
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s-%s-%d", originalID, suffix, n)
		if state.FindTask(candidate) == nil {
			return candidate
		}
	}
}

func amendmentDescription(original *models.Task, mode models.PlanAmendmentMode, correctionType models.TaskType, id, reason string) string {
	reason = strings.TrimSpace(reason)
	if mode == "" {
		return fmt.Sprintf("%s\n\nReviewed amendment of %s: %s. Read its current output manifest. Preserve every existing output slot identity; append new producers only. Reconcile prior merged correction artifacts. After independent review/merge run %s.", original.Description, original.ID, reason, brand.Command("amend-plan", original.ID, "--apply", id))
	}
	description := fmt.Sprintf("%s\n\nReviewed replacement of %s: %s. Mode %s: retain the complete existing output manifest, including allocation/proof and permission boundaries. Change only referenced contract prose; do not change accepted allocations or resolved proofs. Original provider/output identities remain authoritative. After independent review and merge apply with %s.", original.Description, original.ID, reason, mode, brand.Command("amend-plan", original.ID, "--apply", id))
	if correctionType == models.TaskTypeArchitecture {
		description += " Architecture changes must stay inside existing exact CONTRACT subsections of anchored Scope references; scope ownership, read sets and acceptance remain unchanged."
	}
	return description
}

// adoptAmendmentCorrection records the pending fence on the original, resets
// its plan check unless a human hold stands, and schedules the correction,
// resuming a checkpointed sprint.
func adoptAmendmentCorrection(state *models.State, original *models.Task, correction models.Task, actor string) {
	original.PlanAmendment.Pending = correction.ID
	original.PlanAmendment.Corrections = append(original.PlanAmendment.Corrections, correction.ID)
	if original.PlanCheckVerdictOf() != models.PlanCheckHeld {
		original.PlanCheck = nil
	}
	note := "reviewed correction pending: " + correction.ID
	original.History = append(original.History, models.TaskHistoryEntry{Time: correction.Created, Event: models.TaskEventPlanAmendment, Agent: &actor, Note: &note})
	state.Tasks = append(state.Tasks, correction)
	state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, correction.ID)
	if state.Sprint.Status == models.SprintStatusCheckpoint {
		state.Sprint.Status = models.SprintStatusInProgress
		state.Sprint.CheckpointTrigger = ""
	}
}
