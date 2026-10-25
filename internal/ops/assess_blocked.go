package ops

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/roles"
)

// DependencyDescendantWakeSnapshotExtraKey identifies the retired assessment
// cursor, removed from earlier entries when a new fingerprint is recorded.
const DependencyDescendantWakeSnapshotExtraKey = "dependency_descendant_wake_snapshot_v1"

// Like receipt replay, content equivalence aborts serialization without
// changing the task, its lifecycle boundary, or its retained receipts.
var errAssessmentNoChange = stderrors.New("blocked assessment unchanged")

func isAssessmentNoChange(err error) bool { return stderrors.Is(err, errAssessmentNoChange) }

// DependencyDescendantWakeSnapshotEntry fingerprints one descendant beneath a
// canonical dependency root. LifecycleVersion excludes assessment-only events.
type DependencyDescendantWakeSnapshotEntry struct {
	TaskID           string    `json:"task_id" yaml:"task_id"`
	Created          time.Time `json:"created" yaml:"created"`
	Status           string    `json:"status" yaml:"status"`
	LifecycleVersion int       `json:"lifecycle_version" yaml:"lifecycle_version"`
}

// BuildDependencyDescendantWakeSnapshot returns a deterministic baseline for
// descendants beneath the task's resolver-selected dependency roots.
func BuildDependencyDescendantWakeSnapshot(state *models.State, task *models.Task) []DependencyDescendantWakeSnapshotEntry {
	descendants := dependencyDescendantTasks(state, task)
	snapshot := make([]DependencyDescendantWakeSnapshotEntry, 0, len(descendants))
	for _, descendant := range descendants {
		lifecycleVersion := 0
		for i := range descendant.History {
			if descendant.History[i].Event != models.TaskEventOrchestratorAssessment {
				lifecycleVersion++
			}
		}
		snapshot = append(snapshot, DependencyDescendantWakeSnapshotEntry{
			TaskID:           descendant.ID,
			Created:          descendant.Created,
			Status:           string(descendant.Status),
			LifecycleVersion: lifecycleVersion,
		})
	}
	return snapshot
}

// dropSupersededWakeSnapshots removes wake snapshots and fingerprints from
// a task's existing orchestrator_assessment entries. Wake detection reads only
// the most recent assessment (isTaskActionableSinceAssessment), so once a newer
// assessment is recorded the earlier cursors are dead payload that every state
// read, parse and write still pays for.
func dropSupersededWakeSnapshots(task *models.Task) {
	for i := range task.History {
		entry := &task.History[i]
		if entry.Event != models.TaskEventOrchestratorAssessment {
			continue
		}
		delete(entry.Extra, DependencyDescendantWakeSnapshotExtraKey)
		delete(entry.Extra, legacyAssessmentFingerprintExtraKey)
		delete(entry.Extra, AssessmentFingerprintExtraKey)
		delete(entry.Extra, AwaitedTasksExtraKey)
		if len(entry.Extra) == 0 {
			entry.Extra = nil
		}
	}
}

// NormalizeDependencyDescendantWakeSnapshot accepts both the producer's typed
// value and the generic maps/slices produced by YAML decoding.
func NormalizeDependencyDescendantWakeSnapshot(value any) ([]DependencyDescendantWakeSnapshotEntry, bool) {
	if value == nil {
		return nil, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	var snapshot []DependencyDescendantWakeSnapshotEntry
	if err := json.Unmarshal(data, &snapshot); err != nil || snapshot == nil {
		return nil, false
	}
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].TaskID < snapshot[j].TaskID })
	for i := range snapshot {
		if snapshot[i].TaskID == "" || snapshot[i].LifecycleVersion < 0 {
			return nil, false
		}
		if i > 0 && snapshot[i-1].TaskID == snapshot[i].TaskID {
			return nil, false
		}
	}
	return snapshot, true
}

func dependencyDescendantTasks(state *models.State, task *models.Task) []*models.Task {
	if state == nil || task == nil {
		return nil
	}
	childrenByParent := make(map[string][]*models.Task)
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		for _, parentID := range candidate.EffectiveParentTasks() {
			childrenByParent[parentID] = append(childrenByParent[parentID], candidate)
		}
	}

	resolver := models.NewDependencyResolver(state)
	rootSet := make(map[string]struct{})
	for _, dependencyID := range task.DependsOn {
		for _, pathID := range resolver.Resolve(dependencyID).Path {
			candidate := state.FindTask(pathID)
			if candidate != nil && candidate.Status != models.TaskStatusSuperseded {
				rootSet[pathID] = struct{}{}
			}
		}
	}
	roots := make([]string, 0, len(rootSet))
	for rootID := range rootSet {
		roots = append(roots, rootID)
	}
	sort.Strings(roots)

	visited := make(map[string]struct{}, len(roots))
	queue := append([]string(nil), roots...)
	for _, rootID := range roots {
		visited[rootID] = struct{}{}
	}
	var descendants []*models.Task
	for len(queue) > 0 {
		parentID := queue[0]
		queue = queue[1:]
		children := childrenByParent[parentID]
		sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
		for _, child := range children {
			if _, seen := visited[child.ID]; seen {
				continue
			}
			visited[child.ID] = struct{}{}
			descendants = append(descendants, child)
			queue = append(queue, child.ID)
		}
	}
	sort.Slice(descendants, func(i, j int) bool { return descendants[i].ID < descendants[j].ID })
	return descendants
}

// AssessBlockedResult contains the outcome of recording an orchestrator assessment.
type AssessBlockedResult struct {
	models.LifecycleOutcome
	TaskID               string                `json:"task_id"`
	Reason               string                `json:"reason,omitempty"`
	Questions            []string              `json:"questions,omitempty"`
	RepairRequest        *models.RepairRequest `json:"repair_request,omitempty"`
	AwaitedTasks         []string              `json:"awaited_tasks,omitempty"`
	AwaitedCarried       bool                  `json:"awaited_carried,omitempty"`
	HumanAction          string                `json:"human_action,omitempty"`
	Warnings             []string              `json:"warnings,omitempty"`
	SuppressedEntryBytes int                   `json:"suppressed_entry_bytes,omitempty"`
}

func (r *AssessBlockedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// AssessBlockedOptions contains canonical blocker metadata for a structured
// reassessment. Supplying Reason, Questions or RepairRequest enables
// reconciliation mode, which requires both Reason and one to three Questions.
// A nil RepairRequest clears any prior request from the canonical blocker
// state. AwaitedTasks works in either mode and replaces the awaited set. An
// assessment without it keeps the current episode's set, minus satisfied
// members, unless ClearAwaited drops it; the two are mutually exclusive.
// HumanAction and ClearHumanAction treat the episode's human ask the same way
// (ADR-0172): set or replace it, carry it when neither is given, or drop it.
type AssessBlockedOptions struct {
	Request          LifecycleRequestOptions
	Reason           string
	Questions        []string
	RepairRequest    *models.RepairRequest
	AwaitedTasks     []string
	ClearAwaited     bool
	HumanAction      string
	ClearHumanAction bool
	StateLockTimeout bool
}

// AssessBlocked records that the orchestrator has assessed a BLOCKED task.
// Appends an orchestrator_assessment only when its material inputs have changed.
// This prevents the wake-detection loop where the orchestrator repeatedly wakes
// for blocked tasks it has already triaged.
func AssessBlocked(projectRoot, taskID, note, agentID string) (*AssessBlockedResult, error) {
	return AssessBlockedWithOptions(projectRoot, taskID, note, agentID, AssessBlockedOptions{})
}

// AssessBlockedWithOptions records an orchestrator assessment and optionally
// replaces the BLOCKED task's canonical blocker metadata in one validated state
// transaction. The zero-value options preserve AssessBlocked's history-only
// behavior.
func AssessBlockedWithOptions(projectRoot, taskID, note, agentID string, opts AssessBlockedOptions) (*AssessBlockedResult, error) {
	return assessBlockedWithOptionalAuthority(projectRoot, taskID, note, agentID, opts, nil)
}

// AssessBlockedWithAuthority fences the assessment write with the
// orchestrator's registration generation.
func AssessBlockedWithAuthority(projectRoot, taskID, note string, authority models.AgentAuthority, opts AssessBlockedOptions) (*AssessBlockedResult, error) {
	return assessBlockedWithOptionalAuthority(projectRoot, taskID, note, authority.ID, opts, &authority)
}

const assessBlockedOperation = "assess-blocked"

// assessBlockedCall carries one assessment across its phases: caller and payload
// validation, input normalization, the locked mutation and the post-commit alert.
type assessBlockedCall struct {
	projectRoot, taskID, note, agentID string
	opts                               AssessBlockedOptions
	authority                          *models.AgentAuthority

	reconcile     bool
	repairRequest *models.RepairRequest
	awaited       []string
	bb            *db.Blackboard
	now           time.Time

	result   AssessBlockedResult
	observed *models.Task
	effects  LifecycleEffects
}

func assessBlockedWithOptionalAuthority(projectRoot, taskID, note, agentID string, opts AssessBlockedOptions, authority *models.AgentAuthority) (returned *AssessBlockedResult, retErr error) {
	call := &assessBlockedCall{projectRoot: projectRoot, taskID: taskID, note: note, agentID: agentID, opts: opts, authority: authority,
		result: AssessBlockedResult{TaskID: taskID}, effects: LifecycleEffectsNone}
	var invocation *LifecycleInvocation
	defer func() {
		retErr = WrapLifecycleError(assessBlockedOperation, call.observed, retErr, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		if invocation != nil {
			invocation.FinishResult(assessBlockedOperation, outcome, &retErr, warnings)
		}
	}()
	if err := call.validateCaller(); err != nil {
		return nil, err
	}
	if err := call.validatePayload(); err != nil {
		return nil, err
	}
	// Sprint capture reads under the state lock too. Invalid payloads must
	// return before telemetry initializes, not just before the mutation.
	// Trade-off: earlier request-option, missing-agent, role and payload
	// rejections do not increment lifecycle counters. Restore their telemetry
	// when sprint identity can be captured without acquiring the state lock.
	invocation = NewLifecycleInvocation(projectRoot)
	// Legacy defense-in-depth guard; the schema above already rejects an empty
	// task ID through both exported entry points.
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if err := call.normalizeBlocker(); err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	call.bb = RequestBlackboard(lp.StatePath(), authority, opts.Request)
	call.now = time.Now().UTC()

	err := lifecycleMutation(call.bb, authority)(call.mutate)
	if isLifecycleReplay(err) || isAssessmentNoChange(err) {
		return &call.result, nil
	}

	if err != nil {
		return nil, WrapLifecycleError(assessBlockedOperation, call.observed, fmt.Errorf("failed to assess blocked task: %w", err), models.LifecycleStateChanged, LifecycleSafeActionRequery, call.effects)
	}

	call.result.Warnings = call.alertUnresolved(lp)
	return &call.result, nil
}

// validateCaller rejects a call that is not an identified orchestrator before
// any payload is looked at.
func (c *assessBlockedCall) validateCaller() error {
	if err := ValidateLifecycleRequestOptions(c.opts.Request); err != nil {
		return err
	}
	if c.agentID == "" {
		return &PreconditionError{Reason: "agent ID is required"}
	}
	// Defense-in-depth: orchestrator_assessment history entries suppress future wakes,
	// so this must be restricted to orchestrator agents even though the MCP handler
	// also gates via resolveOrchestratorID.
	if err := identity.ValidateRole(c.agentID, roles.Orchestrator); err != nil {
		return WrapLifecycleError(assessBlockedOperation, nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can assess blocked tasks: %v", err)}, models.LifecycleForbidden, LifecycleSafeActionStop, LifecycleEffectsNone)
	}
	return nil
}

func (c *assessBlockedCall) validatePayload() error {
	payload := payloadschema.WithAssessBlockedHumanAction(
		payloadschema.AssessBlockedPayload(c.taskID, c.note, c.opts.Reason, c.opts.Questions, c.opts.RepairRequest, c.opts.AwaitedTasks, c.opts.ClearAwaited),
		c.opts.HumanAction, c.opts.ClearHumanAction)
	if c.opts.StateLockTimeout {
		payload["state_lock_timeout"] = true
	}
	return rejectInvalidLifecyclePayload(assessBlockedOperation, payload)
}

// normalizeBlocker selects history-only or reconciliation mode and normalizes
// the repair request and awaited set the assessment may replace.
func (c *assessBlockedCall) normalizeBlocker() error {
	c.reconcile = c.opts.Reason != "" || len(c.opts.Questions) > 0 || c.opts.RepairRequest != nil
	if c.reconcile {
		if c.opts.Reason == "" {
			return &PreconditionError{Reason: "reason is required"}
		}
		if len(c.opts.Questions) == 0 {
			return &PreconditionError{Reason: "at least 1 question is required"}
		}
		if len(c.opts.Questions) > 3 {
			return &PreconditionError{Reason: "maximum 3 questions allowed per blocking protocol"}
		}
		var err error
		c.repairRequest, err = normalizeRepairRequest(c.opts.RepairRequest, c.taskID)
		if err != nil {
			return err
		}
	}
	var err error
	c.awaited, err = normalizeAwaitedTasks(c.opts.AwaitedTasks)
	return err
}

// alertUnresolved raises the UNRESOLVED BLOCKED alert for a committed assessment.
func (c *assessBlockedCall) alertUnresolved(lp paths.LizaPaths) []string {
	message := c.taskID
	if c.note != "" {
		message = fmt.Sprintf("%s — %s", c.taskID, c.note)
	}
	var warnings []string
	if err := alerts.Write(lp.AlertsLogPath(), alerts.Alert{
		Timestamp: c.now,
		Level:     alerts.AlertLevelCritical,
		Category:  "UNRESOLVED BLOCKED",
		Message:   message,
	}); err != nil {
		warnings = append(warnings, fmt.Sprintf("alert write failed: %v", err))
	}
	return warnings
}
