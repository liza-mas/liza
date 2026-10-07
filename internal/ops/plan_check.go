package ops

import (
	stderrors "errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// PlanCheckAction is one plan-check mutation.
type PlanCheckAction string

const (
	PlanCheckActionPass    PlanCheckAction = "pass"
	PlanCheckActionHold    PlanCheckAction = "hold"
	PlanCheckActionClear   PlanCheckAction = "clear"
	PlanCheckActionReplace PlanCheckAction = "replace"
)

// PlanCheckInput holds one plan-check request. Pass and hold are the
// orchestrator's and need Authority; clear and replace are operator actions
// and need ChangedBy.
type PlanCheckInput struct {
	TaskID     string
	Action     PlanCheckAction
	Ask        string
	ReplacedBy string
	Authority  *models.AgentAuthority
	ChangedBy  string
}

// PlanCheckResult reports the disposition after the request.
type PlanCheckResult struct {
	TaskID     string                  `json:"task_id"`
	Action     PlanCheckAction         `json:"action"`
	Verdict    models.PlanCheckVerdict `json:"verdict,omitempty"`
	ReplacedBy string                  `json:"replaced_by,omitempty"`
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
//   - pass on a passed plan and clear without a disposition are no-ops.
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
		if input.Action == PlanCheckActionPass {
			if err := statevalidate.ValidateProviderDependencies(state, resolver); err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("plan-check provider dependencies: %v", err)}
			}
		}
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %q not found", input.TaskID)}
		}
		class, blocker := domain.Classify(state, task)
		if task.PlanHandoffRetired() && input.Action != PlanCheckActionReplace {
			return &PreconditionError{Reason: fmt.Sprintf("task %s handoff was retired by %s; it cannot be revived", task.ID, task.PlanCheck.ReplacedBy)}
		}
		var next planCheckChange
		var changeErr error
		if input.Action == PlanCheckActionReplace {
			if err := rejectReferencedProviderRetirement(state, resolver, task.ID, retirePermanently); err != nil {
				return err
			}
			next, changeErr = retirePlanHandoff(state, domain, task, input, actor)
			if changeErr == nil && next.changed {
				changeErr = blockStaleProviderConsumers(state, resolver, task.ID, actor, time.Now().UTC())
			}
		} else {
			switch class {
			case PlanHandoffNotSource:
				return &PreconditionError{Reason: fmt.Sprintf(
					"task %s has no plan awaiting hand-off: it must be a MERGED planning task with output whose children do not exist yet and that was not replanned", task.ID)}
			case PlanHandoffOutOfDomain:
				return &PreconditionError{Reason: fmt.Sprintf(
					"task %s has no reviewed hand-off (its transitions are automatic, many-to-one, or it has no output); it transitions without a plan-check", task.ID)}
			}
			next, changeErr = planCheckTransition(task, class, blocker, input, actor)
			var refusal *PreconditionError
			if input.Action == PlanCheckActionPass && stderrors.As(changeErr, &refusal) {
				refusal.Reason += lineageRepairHint(state, resolver, task)
			}
		}
		if changeErr != nil {
			return changeErr
		}
		if next.changed {
			task.PlanCheck = next.check
			note := next.note
			task.History = append(task.History, models.TaskHistoryEntry{
				Time:  time.Now().UTC(),
				Event: models.TaskEventPlanCheck,
				Agent: &actor,
				Note:  &note,
			})
		}
		result.Changed = next.changed
		result.Verdict = task.PlanCheckVerdictOf()
		if task.PlanCheck != nil {
			result.ReplacedBy = task.PlanCheck.ReplacedBy
		}
		result.Class, result.Blocker = domain.Classify(state, task)
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

	if result.Changed {
		logger := log.New(lp.LogPath())
		entry := log.Entry{
			Timestamp: time.Now().UTC(),
			Agent:     actor,
			Action:    "plan_check",
			Task:      &result.TaskID,
			Detail:    fmt.Sprintf("plan-check %s → %s", input.Action, planCheckVerdictLabel(result.Verdict)),
		}
		if logErr := logger.Append(entry); logErr != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", logErr))
		}
		if valErr := statevalidate.ValidateStateFile(lp.StatePath(), false, io.Discard); valErr != nil {
			return nil, &PostWriteValidationError{Err: valErr}
		}
	}
	return result, nil
}

// planCheckActor authorizes the request shape: pass and hold are decisions of
// a role configured with the orchestrator type; clear releases a human hold
// and so is an operator action.
func planCheckActor(resolver *pipeline.Resolver, input PlanCheckInput) (string, error) {
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
	case PlanCheckActionClear, PlanCheckActionReplace:
		if input.Authority != nil {
			return "", &PreconditionError{Reason: "plan-check --clear/--replaced-by is an operator action"}
		}
		if input.Action == PlanCheckActionReplace && strings.TrimSpace(input.ReplacedBy) == "" {
			return "", &PreconditionError{Reason: "plan-check --replaced-by requires a merged correction task ID"}
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
	if roleType != "orchestrator" {
		return fmt.Errorf("role %s has type %s", role, roleType)
	}
	return nil
}

type planCheckChange struct {
	check   *models.PlanCheck
	note    string
	changed bool
}

func retirePlanHandoff(state *models.State, domain PlanHandoffDomain, original *models.Task, input PlanCheckInput, actor string) (planCheckChange, error) {
	refuse := func(reason string) (planCheckChange, error) {
		return planCheckChange{}, &PreconditionError{Reason: reason}
	}
	if original.PlanHandoffRetired() {
		if original.PlanCheck.ReplacedBy == input.ReplacedBy {
			return planCheckChange{}, nil
		}
		return refuse(fmt.Sprintf("task %s handoff was already retired by %s", original.ID, original.PlanCheck.ReplacedBy))
	}
	if !domain.InDomain(original) || !domain.Pending(original) {
		return refuse(fmt.Sprintf("task %s must have an unused MERGED reviewed handoff", original.ID))
	}
	if original.PlanCheckVerdictOf() == models.PlanCheckHeld {
		return refuse(fmt.Sprintf("task %s is held; operator plan-check %s --clear is required first", original.ID, original.ID))
	}
	for name, executed := range original.TransitionsExecuted {
		if executed {
			return refuse(fmt.Sprintf("task %s already executed %s; delivered output cannot be retired", original.ID, name))
		}
	}
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if slices.Contains(candidate.EffectiveParentTasks(), original.ID) {
			return refuse(fmt.Sprintf("task %s already generated child %s", original.ID, candidate.ID))
		}
		if candidate.PlanHandoffRetired() || candidate.TransitionsExecuted["replanned"] ||
			(candidate.Status.IsTerminal() && !domain.Pending(candidate)) {
			continue
		}
		if candidate.ID != input.ReplacedBy && candidate.RolePair == original.RolePair &&
			slices.Contains(candidate.DependsOn, original.ID) {
			// Its transition inherits only from same-pair upstreams that ran it: the
			// original never will, and nothing redirects the edge to the correction.
			return refuse(fmt.Sprintf("task %s depends on %s and would inherit no phase gate from %s; retarget or replan it before retirement", candidate.ID, original.ID, input.ReplacedBy))
		}
		for index, output := range candidate.Output {
			if output.InheritInputs == nil {
				continue
			}
			for selection, selected := range output.InheritInputs.Selections {
				if selected.UpstreamTask == original.ID {
					return refuse(fmt.Sprintf("task %s output[%d].inherit_inputs.selections[%d] still selects %s; retarget and review that plan before retirement", candidate.ID, index, selection, original.ID))
				}
			}
		}
	}
	correction := state.FindTask(input.ReplacedBy)
	if correction == nil || correction.ID == original.ID || correction.Status != models.TaskStatusMerged ||
		correction.RolePair != original.RolePair || !domain.InDomain(correction) || correction.PlanHandoffRetired() ||
		correction.TransitionsExecuted["replanned"] || correction.PlanCheckVerdictOf() == models.PlanCheckHeld {
		return refuse("replacement must be a distinct MERGED correction with output in the same reviewed role-pair, neither held, replanned nor retired")
	}
	return planCheckChange{
		check: &models.PlanCheck{Verdict: models.PlanCheckReplaced, ReplacedBy: correction.ID, By: actor, At: time.Now().UTC()},
		note:  "unused handoff replaced by " + correction.ID, changed: true,
	}, nil
}

func planCheckTransition(task *models.Task, class PlanHandoffClass, blocker string, input PlanCheckInput, actor string) (planCheckChange, error) {
	current := task.PlanCheckVerdictOf()
	now := time.Now().UTC()
	switch input.Action {
	case PlanCheckActionPass:
		switch {
		case current == models.PlanCheckHeld:
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s is held for human action (%s); only an operator plan-check %s --clear releases it", task.ID, task.PlanCheck.Ask, task.ID)}
		case current == models.PlanCheckPassed:
			return planCheckChange{}, nil
		case class == PlanHandoffNeedsReview && blocker != "":
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s cannot pass while %s; replan or hold it instead", task.ID, blocker)}
		}
		return planCheckChange{
			check:   &models.PlanCheck{Verdict: models.PlanCheckPassed, By: actor, At: now},
			note:    "passed",
			changed: true,
		}, nil
	case PlanCheckActionHold:
		ask := strings.TrimSpace(input.Ask)
		if current == models.PlanCheckHeld {
			if task.PlanCheck.Ask == ask {
				return planCheckChange{}, nil
			}
			return planCheckChange{}, &PreconditionError{Reason: fmt.Sprintf(
				"task %s is already held for %q; an operator must clear it before a different hold", task.ID, task.PlanCheck.Ask)}
		}
		return planCheckChange{
			check:   &models.PlanCheck{Verdict: models.PlanCheckHeld, Ask: ask, By: actor, At: now},
			note:    "held: " + ask,
			changed: true,
		}, nil
	default: // clear
		if current == "" {
			return planCheckChange{}, nil
		}
		return planCheckChange{note: "cleared " + string(current), changed: true}, nil
	}
}

// lineageRepairHint names the retarget-dependency repair for each direct edge
// to a replanned upstream that mergedPlanLineageRepair would admit, so a
// refused pass points at the metadata repair instead of a replan.
func lineageRepairHint(state *models.State, resolver *pipeline.Resolver, task *models.Task) string {
	var hints []string
	for _, dep := range task.DependsOn {
		successor, ok := liveReplanSuccessor(state, dep)
		if ok && mergedPlanLineageRepair(state, resolver, task, dep, []string{successor.ID}) == nil {
			hints = append(hints, fmt.Sprintf("retarget-dependency %s %s %s", task.ID, dep, successor.ID))
		}
	}
	if len(hints) == 0 {
		return ""
	}
	return "; its output already targets the replan successor, so repair the stale edge instead: " + strings.Join(hints, " and ")
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
