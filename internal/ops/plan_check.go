package ops

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// PlanCheckAction is one plan-check mutation.
type PlanCheckAction string

const (
	PlanCheckActionPass    PlanCheckAction = "pass"
	PlanCheckActionHold    PlanCheckAction = "hold"
	PlanCheckActionClear   PlanCheckAction = "clear"
	PlanCheckActionReplace PlanCheckAction = "replace"
	PlanCheckActionRetire  PlanCheckAction = "retire"
)

// PlanCheckInput holds one plan-check request. Pass and hold are the
// orchestrator's and need Authority; clear, replace and retire are operator
// actions and need ChangedBy.
type PlanCheckInput struct {
	TaskID     string
	Action     PlanCheckAction
	Ask        string
	ReplacedBy string
	Ruling     string
	Authority  *models.AgentAuthority
	ChangedBy  string
	Notes      []models.PlanValidationNote
}

// PlanCheckResult reports the disposition after the request.
type PlanCheckResult struct {
	TaskID     string                  `json:"task_id"`
	Action     PlanCheckAction         `json:"action"`
	Verdict    models.PlanCheckVerdict `json:"verdict,omitempty"`
	ReplacedBy string                  `json:"replaced_by,omitempty"`
	Ruling     string                  `json:"ruling,omitempty"`
	Class      PlanHandoffClass        `json:"class"`
	Blocker    string                  `json:"blocker,omitempty"`
	Changed    bool                    `json:"changed"`
	Warnings   []string                `json:"warnings,omitempty"`
}

// RecordPlanCheck applies a pass, hold, clear or retirement to a merged hand-off.
// Rules, all evaluated under the state lock:
//   - only a plan in the reviewed hand-off domain with unconsumed output can
//     carry a disposition;
//   - pass is refused while any in-domain upstream is replanned, held or not
//     yet reviewed, and on a held plan (only an operator clear releases a hold);
//   - hold replays with the same ask, is refused with a different one, and may
//     tighten a passed plan;
//   - pass on a passed plan and clear without a disposition are no-ops;
//   - clear releases a hold even on a plan that no longer awaits hand-off
//     (expanded, or no longer in the domain), which keeps it so;
//   - replace and retire end an unused hand-off for good; retire cites the
//     ruling that leaves it with no replacement and, unlike replace, also
//     ends a hold, since a retired plan is expanded by no path.
func RecordPlanCheck(projectRoot string, input PlanCheckInput) (*PlanCheckResult, error) {
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}
	actor, err := planCheckActor(resolver, input)
	if err != nil {
		return nil, err
	}
	domain := NewPlanHandoffDomain(resolver)
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())

	result := &PlanCheckResult{TaskID: input.TaskID, Action: input.Action}
	mutate := func(state *models.State) error {
		*result = PlanCheckResult{TaskID: input.TaskID, Action: input.Action}
		task, class, blocker, err := admitPlanCheck(state, resolver, domain, input)
		if err != nil {
			return err
		}
		next, err := decidePlanCheck(state, resolver, domain, task, class, blocker, input, actor)
		if err != nil {
			return err
		}
		persistPlanCheck(task, next, actor)
		projectPlanCheckResult(result, state, domain, task, next.changed)
		return nil
	}
	if input.Authority != nil {
		err = ModifyWithAgentAuthority(bb, *input.Authority, mutate)
	} else {
		err = bb.Modify(mutate)
	}
	if err != nil {
		return nil, fmt.Errorf("plan-check failed: %w", err)
	}
	return finishPlanCheck(lp, actor, input, result)
}

// admitPlanCheck is the locked admission of a request, in its refusal order:
// the provider graph a pass relies on, the task, the amendment fence, the note
// selection and a retired hand-off, which only a retiring action may revisit.
// It returns the task with its domain classification for the decision step.
func admitPlanCheck(state *models.State, resolver *pipeline.Resolver, domain PlanHandoffDomain, input PlanCheckInput) (*models.Task, PlanHandoffClass, string, error) {
	if input.Action == PlanCheckActionPass {
		if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
			return nil, "", "", &PreconditionError{Reason: fmt.Sprintf("plan-check provider dependencies: %v", err)}
		}
	}
	task := state.FindTask(input.TaskID)
	if task == nil {
		return nil, "", "", &PreconditionError{Reason: fmt.Sprintf("task %q not found", input.TaskID)}
	}
	if task.AmendsPlan != "" || task.PlanGenerationFenced() && (input.Action == PlanCheckActionPass || input.Action == PlanCheckActionReplace || input.Action == PlanCheckActionRetire) {
		return nil, "", "", &PreconditionError{Reason: "plan-check cannot pass/retire a pending amendment or disposition a correction task"}
	}
	if err := validatePlanNoteSelection(state, resolver, domain, task, input.Notes); err != nil {
		return nil, "", "", &PreconditionError{Reason: err.Error()}
	}
	class, blocker := domain.Classify(state, task)
	retiring := input.Action == PlanCheckActionReplace || input.Action == PlanCheckActionRetire
	if task.PlanHandoffRetired() && !retiring {
		return nil, "", "", &PreconditionError{Reason: fmt.Sprintf("task %s handoff was retired by %s; it cannot be revived", task.ID, task.RetiredBy())}
	}
	return task, class, blocker, nil
}

// persistPlanCheck stores a changed disposition with its history entry. A
// replay or no-op decision persists nothing.
func persistPlanCheck(task *models.Task, next planCheckChange, actor string) {
	if !next.changed {
		return
	}
	task.PlanCheck = next.check
	note := next.note
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  time.Now().UTC(),
		Event: models.TaskEventPlanCheck,
		Agent: &actor,
		Note:  &note,
	})
}

// projectPlanCheckResult reports the disposition after the request and
// re-classifies the plan against the post-change state.
func projectPlanCheckResult(result *PlanCheckResult, state *models.State, domain PlanHandoffDomain, task *models.Task, changed bool) {
	result.Changed = changed
	result.Verdict = task.PlanCheckVerdictOf()
	if task.PlanCheck != nil {
		result.ReplacedBy = task.PlanCheck.ReplacedBy
		result.Ruling = task.PlanCheck.Ruling
	}
	result.Class, result.Blocker = domain.Classify(state, task)
}

// finishPlanCheck runs after the commit: the activity log, whose failure is a
// warning, then the post-write validation of a changed state.
func finishPlanCheck(lp paths.LizaPaths, actor string, input PlanCheckInput, result *PlanCheckResult) (*PlanCheckResult, error) {
	if !result.Changed {
		return result, nil
	}
	entry := log.Entry{
		Timestamp: time.Now().UTC(),
		Agent:     actor,
		Action:    "plan_check",
		Task:      &result.TaskID,
		Detail:    fmt.Sprintf("plan-check %s → %s", input.Action, planCheckVerdictLabel(result.Verdict)),
	}
	if logErr := log.New(lp.LogPath()).Append(entry); logErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", logErr))
	}
	if valErr := statevalidate.ValidateStateFile(lp.StatePath(), false, io.Discard); valErr != nil {
		return nil, &PostWriteValidationError{Err: valErr}
	}
	return result, nil
}

// validatePlanNoteSelection uses the same kind deduplication as generation.
func validatePlanNoteSelection(state *models.State, resolver *pipeline.Resolver, domain PlanHandoffDomain, task *models.Task, notes []models.PlanValidationNote) error {
	if err := models.ValidatePlanValidationNotes(notes, len(task.Output)); err != nil {
		return err
	}
	if len(notes) == 0 {
		return nil
	}
	for _, name := range domain.selectedGatedTransitions(task) {
		definition, err := buildTransitionDefFromPipeline(resolver, name)
		if err != nil {
			return err
		}
		if definition.cardinality != models.CardinalityPerSubtask {
			return fmt.Errorf("validation notes require a per-subtask hand-off")
		}
		_, _, remap := resolvePerSubtaskSiblings(task.Output, collectNonTerminalByKind(state, declaredOriginals(task.Output)), task.ID, definition.taskSlug)
		for _, note := range notes {
			if _, deduped := remap[note.OutputIndex]; deduped {
				return fmt.Errorf("notes select deduplicated output[%d]; no new child would receive them", note.OutputIndex)
			}
		}
	}
	return nil
}

// planCheckActor authorizes the request shape: pass and hold are decisions of
// a role configured with the orchestrator type; clear releases a human hold
// and replace and retire end a hand-off, so they are operator actions.
func planCheckActor(resolver *pipeline.Resolver, input PlanCheckInput) (string, error) {
	if len(input.Notes) > 0 && input.Action != PlanCheckActionPass {
		return "", &PreconditionError{Reason: "validation notes require plan-check --pass"}
	}
	switch input.Action {
	case PlanCheckActionPass, PlanCheckActionHold:
		if input.Authority == nil || input.Authority.ID == "" {
			return "", &PreconditionError{Reason: "plan-check --pass/--hold requires an orchestrator agent ID"}
		}
		if err := requireOrchestratorType(resolver, input.Authority.ID); err != nil {
			return "", &PreconditionError{Reason: fmt.Sprintf("plan-check --pass/--hold is an orchestrator decision: %v", err)}
		}
		if input.Action == PlanCheckActionHold && strings.TrimSpace(input.Ask) == "" {
			return "", &PreconditionError{Reason: "plan-check --hold requires the human action being awaited"}
		}
		return input.Authority.ID, nil
	case PlanCheckActionClear, PlanCheckActionReplace, PlanCheckActionRetire:
		if input.Authority != nil {
			return "", &PreconditionError{Reason: "plan-check --clear/--replaced-by/--retire is an operator action"}
		}
		if input.Action == PlanCheckActionReplace && strings.TrimSpace(input.ReplacedBy) == "" {
			return "", &PreconditionError{Reason: "plan-check --replaced-by requires a merged correction task ID"}
		}
		if input.Action == PlanCheckActionRetire && strings.TrimSpace(input.Ruling) == "" {
			return "", &PreconditionError{Reason: "plan-check --retire requires the --ruling that retires the hand-off"}
		}
		if input.ChangedBy == "" {
			return "", &PreconditionError{Reason: "changed_by is required"}
		}
		return input.ChangedBy, nil
	default:
		return "", &PreconditionError{Reason: fmt.Sprintf("unknown plan-check action %q", input.Action)}
	}
}

// requireOrchestratorType checks the configured type of agentID's role, so a
// custom role of the orchestrator type qualifies.
func requireOrchestratorType(resolver *pipeline.Resolver, agentID string) error {
	role, err := identity.ExtractRole(agentID)
	if err != nil {
		return err
	}
	roleType, err := resolver.RoleType(role)
	if err != nil {
		return err
	}
	if roleType != roles.RoleTypeOrchestrator {
		return fmt.Errorf("role %s has type %s", role, roleType)
	}
	return nil
}

func planCheckVerdictLabel(verdict models.PlanCheckVerdict) string {
	if verdict == "" {
		return "none"
	}
	return string(verdict)
}

// GetWarnings implements the CLI's warning accessor.
func (r *PlanCheckResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}
