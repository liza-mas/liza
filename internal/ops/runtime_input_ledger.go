package ops

import (
	"crypto/rand"
	"encoding/hex"
	stderrors "errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/runtimeinputs"
)

// Refusal codes a runtime input can be refused with (ADR-0169). Each is a
// precondition: it costs no review cycle and executes no command.
const (
	runtimeInputCodeConsumed    = "runtime_input_consumed"
	runtimeInputCodeInvalidated = "runtime_input_invalidated"
	runtimeInputCodeMissing     = "runtime_input_missing"
	runtimeInputCodeUnavailable = "runtime_input_unavailable"
)

// runtimeInputKeyPath locates the operator key; tests redirect it.
var runtimeInputKeyPath = runtimeinputs.DefaultKeyPath

// runtimeInputForbiddenRoots are the locations an artifact must stay out of:
// the repository checkout, whose worktrees live beneath it.
func runtimeInputForbiddenRoots(projectRoot string) []string {
	return []string{projectRoot}
}

// loadRuntimeInputKey returns the operator key the ledger was recorded with.
// A missing key is created only while the ledger is empty; with instances
// present, a missing, malformed or different key fails closed, since a new
// key could not recognize spent materializations.
func loadRuntimeInputKey(state *models.State, create bool) (*runtimeinputs.Key, error) {
	path, err := runtimeInputKeyPath()
	if err != nil {
		return nil, err
	}
	key, err := runtimeinputs.LoadKey(path)
	if err != nil {
		if _, statErr := os.Stat(path); create && len(state.RuntimeInputs) == 0 && stderrors.Is(statErr, os.ErrNotExist) {
			key, err = runtimeinputs.CreateKey(path)
			if err != nil {
				key, err = runtimeinputs.LoadKey(path) // Another recorder won the race.
			}
		}
		if err != nil {
			return nil, err
		}
	}
	for _, instance := range state.RuntimeInputs {
		if instance.KeyID != key.ID {
			return nil, fmt.Errorf("%w: the key does not match the one the ledger was recorded with; restore the backed-up key", runtimeinputs.ErrKeyUnavailable)
		}
	}
	return key, nil
}

// runtimeInputResolution is one declared input resolved against the ledger.
type runtimeInputResolution struct {
	declaration     models.RuntimeInput
	instanceID      string
	materialization *runtimeinputs.Materialization
	// code is empty when the instance is usable.
	code string
	// invalidate lists available instances whose artifact no longer
	// matches the recorded identity; they are invalidated even when a later
	// instance is usable.
	invalidate []string
	// bound names the most recently recorded instance bound to the task for
	// this input, or "none"; it re-arms anomaly deduplication.
	bound string
}

// resolveRuntimeInputs selects, for each declaration, the oldest bound
// available instance whose artifact still verifies, trying newer ones when an
// older artifact changed or is unreadable. It reads files but never
// state beyond the given snapshot, and writes nothing.
func resolveRuntimeInputs(projectRoot string, state *models.State, taskID string, declarations []models.RuntimeInput, key *runtimeinputs.Key, keyErr error) []runtimeInputResolution {
	resolutions := make([]runtimeInputResolution, 0, len(declarations))
	for _, declaration := range declarations {
		resolution := runtimeInputResolution{declaration: declaration, bound: "none"}
		var available []string
		latest := time.Time{}
		latestState := ""
		for _, id := range sortedLedgerIDs(state.RuntimeInputs) {
			instance := state.RuntimeInputs[id]
			if instance.InputID != declaration.ID || !instance.BoundTo(taskID) {
				continue
			}
			if resolution.bound == "none" || instance.RegisteredAt.After(latest) {
				resolution.bound, latest, latestState = id, instance.RegisteredAt, instance.State
			}
			if instance.State == models.RuntimeInputAvailable {
				available = append(available, id)
			}
		}
		sort.SliceStable(available, func(i, j int) bool {
			return state.RuntimeInputs[available[i]].RegisteredAt.Before(state.RuntimeInputs[available[j]].RegisteredAt)
		})
		switch {
		case len(available) == 0 && latestState == models.RuntimeInputConsumed:
			resolution.code = runtimeInputCodeConsumed
		case len(available) == 0 && latestState == models.RuntimeInputInvalidated:
			resolution.code = runtimeInputCodeInvalidated
		case len(available) == 0:
			resolution.code = runtimeInputCodeMissing
		case keyErr != nil:
			resolution.code = runtimeInputCodeUnavailable
		default:
			unreadable := false
			for _, id := range available {
				materialization, err := runtimeinputs.Materialize(key, declaration, state.RuntimeInputs[id].Envelope, runtimeInputForbiddenRoots(projectRoot))
				switch {
				case stderrors.Is(err, runtimeinputs.ErrUnavailable):
					unreadable = true
				case err != nil || materialization.Identity != id:
					resolution.invalidate = append(resolution.invalidate, id)
				default:
					resolution.instanceID, resolution.materialization = id, materialization
				}
				if resolution.materialization != nil {
					break
				}
			}
			switch {
			case resolution.materialization != nil:
			case unreadable:
				resolution.code = runtimeInputCodeUnavailable
			default:
				resolution.code = runtimeInputCodeInvalidated
			}
		}
		resolutions = append(resolutions, resolution)
	}
	return resolutions
}

func sortedLedgerIDs(instances map[string]models.RuntimeInputInstance) []string {
	ids := make([]string, 0, len(instances))
	for id := range instances {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// runtimeInputGrant is what a gate run may deliver: the scrub set, the
// variables each canonical command receives, and the values masking hides.
type runtimeInputGrant struct {
	deny     map[string]bool
	overlays map[string][]string
	secrets  []string
}

// commandEnvironment scrubs every reserved name from base, then overlays
// exactly the variables of inputs that list command.
func (g *runtimeInputGrant) commandEnvironment(base []string, command string) []string {
	if g == nil {
		return base
	}
	return append(models.ScrubEnvironment(base, g.deny), g.overlays[command]...)
}

func (g *runtimeInputGrant) secretValues() []string {
	if g == nil {
		return nil
	}
	return g.secrets
}

// runtimeInputGate carries what an acquisition needs from its gate call site.
type runtimeInputGate struct {
	projectRoot string
	bb          *db.Blackboard
	authority   *models.AgentAuthority
	actor       string
	operation   string
	// onRefusal runs inside the refusing transaction, on the live task.
	onRefusal func(state *models.State, task *models.Task) error
}

// acquire is the pre-launch step of the strict gate (ADR-0169). It runs
// after every non-executing precondition and immediately before the first
// command, outside the state lock except for one transaction that verifies
// each selected instance is still available, invalidates changed artifacts,
// and either records the refusal signal or consumes every single_use
// instance. Consumption is durable before any command launches; nothing
// returns it.
func (gate runtimeInputGate) acquire(task *models.Task) (*runtimeInputGrant, error) {
	snapshot, err := gate.bb.Read()
	if err != nil {
		return nil, err
	}
	grant := &runtimeInputGrant{deny: models.RuntimeInputDenyNames(snapshot), overlays: map[string][]string{}}
	if len(task.RuntimeInputs) == 0 {
		return grant, nil
	}
	key, keyErr := loadRuntimeInputKey(snapshot, false)
	resolutions := resolveRuntimeInputs(gate.projectRoot, snapshot, task.ID, task.RuntimeInputs, key, keyErr)
	runID := newRuntimeInputRunID()
	var refusals []runtimeInputResolution
	err = lifecycleMutation(gate.bb, gate.authority)(func(state *models.State) error {
		refusals = nil // A re-run transaction reports its own outcome.
		live := state.FindTask(task.ID)
		if live == nil || !models.RuntimeInputsEqual(live.RuntimeInputs, task.RuntimeInputs) {
			return &PreconditionError{Reason: fmt.Sprintf("task %s runtime_inputs changed before the gate launched", task.ID)}
		}
		now := time.Now().UTC()
		for _, resolution := range resolutions {
			if resolution.code == "" {
				instance, ok := state.RuntimeInputs[resolution.instanceID]
				if !ok || instance.State != models.RuntimeInputAvailable || !instance.BoundTo(task.ID) {
					resolution.code = runtimeInputCodeConsumed
					if ok && instance.State == models.RuntimeInputInvalidated {
						resolution.code = runtimeInputCodeInvalidated
					}
				}
			}
			for _, id := range resolution.invalidate {
				if instance, ok := state.RuntimeInputs[id]; ok && instance.State == models.RuntimeInputAvailable {
					instance.State = models.RuntimeInputInvalidated
					instance.Invalidated = &models.RuntimeInputInvalidation{Reason: models.RuntimeInputInvalidatedArtifactChanged, At: now}
					state.RuntimeInputs[id] = instance
				}
			}
			if resolution.code != "" {
				refusals = append(refusals, resolution)
			}
		}
		if len(refusals) > 0 {
			for _, refusal := range refusals {
				appendRuntimeInputAnomaly(state, task.ID, refusal.declaration.ID, refusal.code, gate.operation, refusal.bound, gate.actor, now)
			}
			if gate.onRefusal != nil {
				return gate.onRefusal(state, live)
			}
			return nil
		}
		for _, resolution := range resolutions {
			instance := state.RuntimeInputs[resolution.instanceID]
			if instance.Consumption != models.RuntimeInputSingleUse {
				continue
			}
			instance.State = models.RuntimeInputConsumed
			instance.Consumed = &models.RuntimeInputConsumption{Task: task.ID, RunID: runID, Agent: gate.actor, At: now}
			state.RuntimeInputs[resolution.instanceID] = instance
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(refusals) > 0 {
		return nil, runtimeInputRefusalError(task.ID, refusals, nil)
	}
	for _, resolution := range resolutions {
		entries := resolution.materialization.Environment()
		for _, command := range resolution.declaration.Commands {
			grant.overlays[command] = append(grant.overlays[command], entries...)
		}
		if resolution.declaration.Secret {
			grant.secrets = append(grant.secrets, resolution.materialization.SecretValues()...)
		}
	}
	return grant, nil
}

func newRuntimeInputRunID() string {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

// appendRuntimeInputAnomaly records the operator signal for one refused
// input unless the latest one for the same task and input already says the
// same thing about the same bound instance. Recording a new instance changes
// the bound instance, so a later refusal is announced again.
func appendRuntimeInputAnomaly(state *models.State, taskID, inputID, code, operation, bound, reporter string, now time.Time) {
	for i := len(state.Anomalies) - 1; i >= 0; i-- {
		anomaly := state.Anomalies[i]
		if anomaly.Type != models.AnomalyTypeRuntimeInputUnavailable || anomaly.Details["task_id"] != taskID || anomaly.Details["input_id"] != inputID {
			continue
		}
		if anomaly.Details["code"] == code && anomaly.Details["bound_instance"] == bound {
			return
		}
		break
	}
	if reporter == "" {
		reporter = "operator"
	}
	state.Anomalies = append(state.Anomalies, models.Anomaly{
		Timestamp: now,
		Task:      taskID,
		Reporter:  reporter,
		Type:      models.AnomalyTypeRuntimeInputUnavailable,
		Details: map[string]any{
			"task_id":        taskID,
			"input_id":       inputID,
			"code":           code,
			"operation":      operation,
			"bound_instance": bound,
			"action":         runtimeInputOperatorAction(taskID, inputID),
		},
	})
}

// runtimeInputOperatorAction is the one human step every refusal names.
// Agent-facing text carries the input id, never an artifact path.
func runtimeInputOperatorAction(taskID, inputID string) string {
	return fmt.Sprintf("record an instance with %s", brand.Command("provision", "--record", "--task", taskID, "--input", inputID, "--file", "<envelope>"))
}

// runtimeInputRefusalError builds the refusal an agent receives. claim is set
// for claim-stage refusals, which a supervisor escalates to BLOCKED.
func runtimeInputRefusalError(taskID string, refusals []runtimeInputResolution, claim *AcceptanceClaimObservation) *AcceptanceEvidenceError {
	codes := make([]string, len(refusals))
	for i, refusal := range refusals {
		codes[i] = refusal.code + ":" + refusal.declaration.ID
	}
	return &AcceptanceEvidenceError{
		TaskID: taskID, Field: "runtime_inputs", Reason: strings.Join(codes, ", "),
		Class: AcceptanceFaultRuntimeInput, Input: refusals[0].declaration.ID, Claim: claim,
	}
}

// runtimeInputsForRole selects the inputs a session must be able to resolve:
// every input for the doer, only reusable ones for a reviewer, whose
// single_use inputs are proven by the receipt (ADR-0169).
func runtimeInputsForRole(inputs []models.RuntimeInput, reviewer bool) []models.RuntimeInput {
	if !reviewer {
		return inputs
	}
	var reusable []models.RuntimeInput
	for _, input := range inputs {
		if input.Consumption == models.RuntimeInputReusable {
			reusable = append(reusable, input)
		}
	}
	return reusable
}

// checkRuntimeInputReadiness verifies, without writing anything, that every
// input the role needs resolves to a usable instance. It is the claim and
// launch counterpart of gate acquisition, so a missing input is found before
// a coding turn is spent rather than at submit.
func checkRuntimeInputReadiness(projectRoot string, state *models.State, task *models.Task, reviewer bool) *AcceptanceEvidenceError {
	refusals := runtimeInputReadinessRefusals(projectRoot, state, task, reviewer)
	if len(refusals) == 0 {
		return nil
	}
	return runtimeInputRefusalError(task.ID, refusals, nil)
}

func runtimeInputReadinessRefusals(projectRoot string, state *models.State, task *models.Task, reviewer bool) []runtimeInputResolution {
	inputs := runtimeInputsForRole(task.RuntimeInputs, reviewer)
	if len(inputs) == 0 {
		return nil
	}
	key, keyErr := loadRuntimeInputKey(state, false)
	var refusals []runtimeInputResolution
	for _, resolution := range resolveRuntimeInputs(projectRoot, state, task.ID, inputs, key, keyErr) {
		if resolution.code != "" {
			refusals = append(refusals, resolution)
		}
	}
	return refusals
}

// retireRuntimeInputBindings unbinds a retiring task from its available
// instances (ADR-0169). A binding moves to the first replacement that
// declares the same input field for field; otherwise an orphaned single_use
// instance is invalidated, while an orphaned reusable one stays available so
// it can be recorded for another task. Consumed and invalidated instances are
// audit records and keep their bindings.
func retireRuntimeInputBindings(state *models.State, task *models.Task, replacementIDs []string, now time.Time) {
	for _, id := range sortedLedgerIDs(state.RuntimeInputs) {
		instance := state.RuntimeInputs[id]
		if instance.State != models.RuntimeInputAvailable || !instance.BoundTo(task.ID) {
			continue
		}
		tasks := make([]string, 0, len(instance.Tasks))
		for _, bound := range instance.Tasks {
			if bound != task.ID {
				tasks = append(tasks, bound)
			}
		}
		if successor := runtimeInputSuccessor(state, task, instance.InputID, replacementIDs); successor != "" && !slices.Contains(tasks, successor) {
			tasks = append(tasks, successor)
			slices.Sort(tasks)
		}
		instance.Tasks = tasks
		if len(tasks) == 0 && instance.Consumption == models.RuntimeInputSingleUse {
			instance.State = models.RuntimeInputInvalidated
			instance.Invalidated = &models.RuntimeInputInvalidation{Reason: models.RuntimeInputInvalidatedTaskRetired, At: now}
		}
		state.RuntimeInputs[id] = instance
	}
}

func runtimeInputSuccessor(state *models.State, task *models.Task, inputID string, replacementIDs []string) string {
	declared := models.FindRuntimeInput(task.RuntimeInputs, inputID)
	if declared == nil {
		return ""
	}
	for _, replacementID := range replacementIDs {
		replacement := state.FindTask(replacementID)
		if replacement == nil || replacement.Status.IsTerminal() {
			continue
		}
		if candidate := models.FindRuntimeInput(replacement.RuntimeInputs, inputID); candidate != nil && models.RuntimeInputEqual(*candidate, *declared) {
			return replacementID
		}
	}
	return ""
}
