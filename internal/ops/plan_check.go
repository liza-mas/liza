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
	"github.com/liza-mas/liza/internal/statevalidate"
)

// PlanCheckAction is one plan-check mutation.
type PlanCheckAction string

const (
	PlanCheckActionPass  PlanCheckAction = "pass"
	PlanCheckActionHold  PlanCheckAction = "hold"
	PlanCheckActionClear PlanCheckAction = "clear"
)

// PlanCheckInput holds one plan-check request. Pass and hold are the
// orchestrator's and need Authority; clear is an operator action and needs
// ChangedBy.
type PlanCheckInput struct {
	TaskID    string
	Action    PlanCheckAction
	Ask       string
	Authority *models.AgentAuthority
	ChangedBy string
}

// PlanCheckResult reports the disposition after the request.
type PlanCheckResult struct {
	TaskID   string                  `json:"task_id"`
	Action   PlanCheckAction         `json:"action"`
	Verdict  models.PlanCheckVerdict `json:"verdict,omitempty"`
	Class    PlanHandoffClass        `json:"class"`
	Blocker  string                  `json:"blocker,omitempty"`
	Changed  bool                    `json:"changed"`
	Warnings []string                `json:"warnings,omitempty"`
}

// RecordPlanCheck applies a pass, hold or clear to a merged plan's hand-off
// disposition. Rules, all evaluated under the state lock:
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
		task := state.FindTask(input.TaskID)
		if task == nil {
			return &PreconditionError{Reason: fmt.Sprintf("task %q not found", input.TaskID)}
		}
		class, blocker := domain.Classify(state, task)
		switch class {
		case PlanHandoffNotSource:
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s has no plan awaiting hand-off: it must be a MERGED planning task with output whose children do not exist yet and that was not replanned", task.ID)}
		case PlanHandoffOutOfDomain:
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s has no reviewed hand-off (its transitions are automatic, many-to-one, or it has no output); it transitions without a plan-check", task.ID)}
		}
		next, changeErr := planCheckTransition(task, class, blocker, input, actor)
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
	case PlanCheckActionClear:
		if input.Authority != nil {
			return "", &PreconditionError{Reason: "plan-check --clear is an operator action"}
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
