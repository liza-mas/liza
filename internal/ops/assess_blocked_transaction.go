package ops

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// assessedEpisode is what one assessment decides about the blocked episode: the
// signals it carries forward, the history entry it would record and the
// fingerprint that decides whether recording it is a no-op.
type assessedEpisode struct {
	effective   []string
	humanAction string
	fingerprint string
	entry       models.TaskHistoryEntry
	adoptHold   bool
}

// mutate is the assessment transaction body: it resolves the request identity,
// decides what the assessment would record, suppresses it when nothing changed
// and otherwise records it.
func (c *assessBlockedCall) mutate(state *models.State) (callbackErr error) {
	defer func() {
		callbackErr = WrapLifecycleError(assessBlockedOperation, c.observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
	}()
	task := state.FindTask(c.taskID)
	if task == nil {
		return &errors.NotFoundError{Entity: "task", ID: c.taskID}
	}
	copy := *task
	c.observed = &copy
	request, err := c.newRequest(task)
	if err != nil {
		return err
	}
	receipt, err := CheckLifecycleRequest(task, request, state.Agents)
	if err != nil {
		return err
	}
	if receipt != nil {
		c.result.LifecycleOutcome = LifecycleReplayOutcome(task, receipt, c.agentID)
		return errLifecycleReplay
	}

	if task.Status != models.TaskStatusBlocked {
		return WrapLifecycleError(assessBlockedOperation, task, &PreconditionError{Reason: fmt.Sprintf("task must be in BLOCKED status to assess, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
	}
	episode, err := c.assess(state, task)
	if err != nil {
		return err
	}
	if err := c.suppressUnchanged(task, request, episode); err != nil {
		return err
	}
	return c.record(state, task, request, episode)
}

func (c *assessBlockedCall) newRequest(task *models.Task) (LifecycleRequest, error) {
	return NewLifecycleRequest(assessBlockedOperation, task, c.agentID, c.authority, c.opts.Request, struct {
		Note, Reason  string
		Questions     []string
		RepairRequest *models.RepairRequest
		// Explicit inputs only, omitted when empty so earlier requests
		// keep their identity. A carried set is derived from state and
		// never part of identity.
		AwaitedTasks     []string `json:",omitempty"`
		ClearAwaited     bool     `json:",omitempty"`
		HumanAction      string   `json:",omitempty"`
		ClearHumanAction bool     `json:",omitempty"`
		StateLockTimeout bool     `json:",omitempty"`
	}{c.note, c.opts.Reason, c.opts.Questions, c.repairRequest, c.awaited, c.opts.ClearAwaited, c.opts.HumanAction, c.opts.ClearHumanAction, c.opts.StateLockTimeout})
}

// assess decides what the assessment would record for the episode: the awaited
// set and human ask it carries, the fingerprint of the canonical blocker it
// would leave, its history entry, and whether it adopts a state lock hold.
func (c *assessBlockedCall) assess(state *models.State, task *models.Task) (*assessedEpisode, error) {
	effective, humanAction, err := c.carriedSignals(state, task)
	if err != nil {
		return nil, err
	}
	candidate := c.candidate(task, effective, humanAction)
	fingerprint := BuildAssessmentFingerprint(state, task, candidate)
	entry := c.historyEntry(fingerprint, effective, humanAction)
	preview := *task
	preview.BlockedReason = &candidate.Reason
	preview.BlockedQuestions = candidate.Questions
	preview.RepairRequest = candidate.RepairRequest
	preview.History = append(append([]models.TaskHistoryEntry(nil), task.History...), entry)
	adoptHold := c.opts.StateLockTimeout && !models.CurrentStateLockHold(&preview)
	if c.opts.StateLockTimeout {
		resolver, _, err := loadResolver(c.projectRoot)
		if err != nil {
			return nil, err
		}
		if reason := stateLockHoldGate(state, &preview, resolver); reason != "" {
			return nil, &PreconditionError{Reason: "state lock hold requires infrastructure-only wait: " + reason}
		}
	}
	return &assessedEpisode{effective: effective, humanAction: humanAction, fingerprint: fingerprint, entry: entry, adoptHold: adoptHold}, nil
}

// carriedSignals resolves the awaited set and human ask the assessment keeps.
func (c *assessBlockedCall) carriedSignals(state *models.State, task *models.Task) (effective []string, humanAction string, err error) {
	// Without --awaits the current episode's set carries forward, minus
	// satisfied members, so a re-check does not silently widen the wake
	// signal back to every generated descendant.
	effective, carried := c.awaited, false
	if len(effective) == 0 && !c.opts.ClearAwaited {
		effective = carriedAwaitedTasks(state, task)
		carried = len(effective) > 0
	}
	// Before the no-change comparison: an unchanged assessment must not be
	// accepted once an awaited task has settled or failed, or the wait
	// would deadlock.
	if len(effective) > 0 {
		if err := validateAwaitedTasks(state, task, effective); err != nil {
			if carried {
				return nil, "", rejectCarriedAwaitedTasks(state, effective, err)
			}
			return nil, "", err
		}
	}
	c.result.AwaitedTasks = append([]string(nil), effective...)
	c.result.AwaitedCarried = carried
	// Without either human-ask option the current ask carries forward, so a
	// note-only re-check neither drops nor re-raises it.
	humanAction = strings.TrimSpace(c.opts.HumanAction)
	if humanAction == "" && !c.opts.ClearHumanAction {
		if current, ok := models.CurrentAwaitingHuman(task); ok {
			humanAction = current.Ask
		}
	}
	c.result.HumanAction = humanAction
	return effective, humanAction, nil
}

// candidate is the canonical blocker the task would hold after the assessment.
func (c *assessBlockedCall) candidate(task *models.Task, effective []string, humanAction string) AssessmentFingerprintCandidate {
	candidate := AssessmentFingerprintCandidate{
		Questions: task.BlockedQuestions, RepairRequest: task.RepairRequest, Note: c.note, Awaited: effective,
		HumanAction: humanAction,
	}
	if task.BlockedReason != nil {
		candidate.Reason = *task.BlockedReason
	}
	if c.reconcile {
		candidate.Reason = c.opts.Reason
		candidate.Questions = c.opts.Questions
		candidate.RepairRequest = c.repairRequest
	}
	return candidate
}

func (c *assessBlockedCall) historyEntry(fingerprint string, effective []string, humanAction string) models.TaskHistoryEntry {
	entry := models.TaskHistoryEntry{
		Time:  c.now,
		Event: models.TaskEventOrchestratorAssessment,
		Agent: &c.agentID,
		Extra: map[string]any{
			AssessmentFingerprintExtraKey: fingerprint,
		},
	}
	if c.note != "" {
		entry.Note = &c.note
	}
	if len(effective) > 0 {
		entry.Extra[AwaitedTasksExtraKey] = append([]string(nil), effective...)
	}
	if humanAction != "" {
		entry.Extra[models.AwaitingHumanExtraKey] = humanAction
	}
	if c.reconcile {
		entry.Reason = &c.opts.Reason
		entry.Extra["blocked_questions"] = append([]string(nil), c.opts.Questions...)
		entry.Extra["repair_request"] = c.repairRequest
	}
	return entry
}

// suppressUnchanged aborts the transaction with errAssessmentNoChange when the
// previous assessment already recorded this fingerprint and no hold is adopted.
func (c *assessBlockedCall) suppressUnchanged(task *models.Task, request LifecycleRequest, episode *assessedEpisode) error {
	previous := lastOrchestratorAssessment(task)
	if previous == nil {
		return nil
	}
	recorded, valid := IsAssessmentFingerprint(previous.Extra[AssessmentFingerprintExtraKey])
	if !valid || recorded != episode.fingerprint || episode.adoptHold {
		return nil
	}
	encoded, err := yaml.Marshal(episode.entry)
	if err != nil {
		return fmt.Errorf("encode suppressed assessment: %w", err)
	}
	c.result.LifecycleOutcome = NewLifecycleNoChangeOutcome(assessBlockedOperation, task)
	c.result.RequestID = request.RequestID
	c.result.SuppressedEntryBytes = len(encoded)
	return errAssessmentNoChange
}

// record publishes the assessment: the reconciled blocker, the history entry and
// the hold it clears or adopts, validated against the baseline when it
// reconciles.
func (c *assessBlockedCall) record(state *models.State, task *models.Task, request LifecycleRequest, episode *assessedEpisode) error {
	if c.reconcile {
		reason := c.opts.Reason
		questions := append([]string(nil), c.opts.Questions...)
		task.BlockedReason = &reason
		task.BlockedQuestions = questions
		task.RepairRequest = c.repairRequest
	}

	dropSupersededWakeSnapshots(task)
	task.History = append(task.History, episode.entry)
	if !models.CurrentStateLockHold(task) || episode.humanAction != "" || task.RepairRequest != nil || len(episode.effective) > 0 {
		task.StateLockHold = nil
	}
	if episode.adoptHold {
		installStateLockHold(state, task)
	}
	if c.reconcile {
		if err := statevalidate.ValidateCandidate(state, c.bb.ReadSnapshot, c.projectRoot, false, os.Stderr); err != nil {
			return err
		}
		c.result.Reason = c.opts.Reason
		c.result.Questions = append([]string(nil), c.opts.Questions...)
		c.result.RepairRequest = c.repairRequest
	}
	var err error
	c.result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
	if err == nil {
		c.effects = "unknown"
	}
	return err
}
