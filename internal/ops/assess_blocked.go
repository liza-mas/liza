package ops

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/liza-mas/liza/internal/alerts"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/identity"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// DependencyDescendantWakeSnapshotExtraKey identifies the versioned blocked
// assessment cursor consumed by orchestrator wake detection.
const DependencyDescendantWakeSnapshotExtraKey = "dependency_descendant_wake_snapshot_v1"

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

// dropSupersededWakeSnapshots removes dependency-descendant wake snapshots from
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
		if _, ok := entry.Extra[DependencyDescendantWakeSnapshotExtraKey]; !ok {
			continue
		}
		delete(entry.Extra, DependencyDescendantWakeSnapshotExtraKey)
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

// DependencyDescendantWakeSnapshotChanged compares a persisted baseline with
// the current canonical descendant state. Malformed persisted values fail open
// so the next assessment can replace them with a valid cursor.
func DependencyDescendantWakeSnapshotChanged(state *models.State, task *models.Task, recorded any) bool {
	baseline, ok := NormalizeDependencyDescendantWakeSnapshot(recorded)
	if !ok {
		return true
	}
	current := BuildDependencyDescendantWakeSnapshot(state, task)
	if len(baseline) != len(current) {
		return true
	}
	for i := range current {
		if baseline[i] != current[i] {
			return true
		}
	}
	return false
}

// DependencyDescendantChangedAfter reports legacy timestamp evidence for a
// relevant descendant creation or non-assessment lifecycle event.
func DependencyDescendantChangedAfter(state *models.State, task *models.Task, after time.Time) bool {
	for _, descendant := range dependencyDescendantTasks(state, task) {
		if descendant.Created.After(after) {
			return true
		}
		for i := range descendant.History {
			entry := descendant.History[i]
			if entry.Event != models.TaskEventOrchestratorAssessment && entry.Time.After(after) {
				return true
			}
		}
	}
	return false
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
	TaskID        string                `json:"task_id"`
	Reason        string                `json:"reason,omitempty"`
	Questions     []string              `json:"questions,omitempty"`
	RepairRequest *models.RepairRequest `json:"repair_request,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
}

func (r *AssessBlockedResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// AssessBlockedOptions contains canonical blocker metadata for a structured
// reassessment. Supplying any field enables reconciliation mode, which requires
// both Reason and one to three Questions. A nil RepairRequest clears any prior
// request from the canonical blocker state.
type AssessBlockedOptions struct {
	Request       LifecycleRequestOptions
	Reason        string
	Questions     []string
	RepairRequest *models.RepairRequest
}

// AssessBlocked records that the orchestrator has assessed a BLOCKED task.
// Appends an orchestrator_assessment history entry without changing task status.
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

func assessBlockedWithOptionalAuthority(projectRoot, taskID, note, agentID string, opts AssessBlockedOptions, authority *models.AgentAuthority) (returned *AssessBlockedResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	const operation = "assess-blocked"
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(operation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(operation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts.Request); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "agent ID is required"}
	}
	// Defense-in-depth: orchestrator_assessment history entries suppress future wakes,
	// so this must be restricted to orchestrator agents even though the MCP handler
	// also gates via resolveOrchestratorID.
	if err := identity.ValidateRole(agentID, roles.Orchestrator); err != nil {
		return nil, WrapLifecycleError(operation, nil, &PreconditionError{Reason: fmt.Sprintf("only orchestrator agents can assess blocked tasks: %v", err)}, models.LifecycleForbidden, "stop", "none")
	}

	reconcile := opts.Reason != "" || len(opts.Questions) > 0 || opts.RepairRequest != nil
	var repairRequest *models.RepairRequest
	if reconcile {
		if opts.Reason == "" {
			return nil, &PreconditionError{Reason: "reason is required"}
		}
		if len(opts.Questions) == 0 {
			return nil, &PreconditionError{Reason: "at least 1 question is required"}
		}
		if len(opts.Questions) > 3 {
			return nil, &PreconditionError{Reason: "maximum 3 questions allowed per blocking protocol"}
		}
		var err error
		repairRequest, err = normalizeRepairRequest(opts.RepairRequest, taskID)
		if err != nil {
			return nil, err
		}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	result := AssessBlockedResult{TaskID: taskID}

	err := lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(operation, task, agentID, authority, opts.Request, struct {
			Note, Reason  string
			Questions     []string
			RepairRequest *models.RepairRequest
		}{note, opts.Reason, opts.Questions, repairRequest})
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request)
		if err != nil {
			return err
		}
		if receipt != nil {
			result.LifecycleOutcome = LifecycleReplayOutcome(task, receipt, agentID)
			return errLifecycleReplay
		}

		if task.Status != models.TaskStatusBlocked {
			return WrapLifecycleError(operation, task, &PreconditionError{Reason: fmt.Sprintf("task must be in BLOCKED status to assess, current status: %s", task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}

		entry := models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventOrchestratorAssessment,
			Agent: &agentID,
			Extra: map[string]any{
				DependencyDescendantWakeSnapshotExtraKey: BuildDependencyDescendantWakeSnapshot(state, task),
			},
		}
		if note != "" {
			entry.Note = &note
		}
		if reconcile {
			reason := opts.Reason
			questions := append([]string(nil), opts.Questions...)
			task.BlockedReason = &reason
			task.BlockedQuestions = questions
			task.RepairRequest = repairRequest
			entry.Reason = &reason
			entry.Extra["blocked_questions"] = append([]string(nil), questions...)
			entry.Extra["repair_request"] = repairRequest
		}

		dropSupersededWakeSnapshots(task)
		task.History = append(task.History, entry)
		if reconcile {
			if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
				return err
			}
			result.Reason = opts.Reason
			result.Questions = append([]string(nil), opts.Questions...)
			result.RepairRequest = repairRequest
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{})
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}

	if err != nil {
		return nil, WrapLifecycleError(operation, observed, fmt.Errorf("failed to assess blocked task: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	message := taskID
	if note != "" {
		message = fmt.Sprintf("%s — %s", taskID, note)
	}
	var warnings []string
	if err := alerts.Write(lp.AlertsLogPath(), alerts.Alert{
		Timestamp: now,
		Level:     alerts.AlertLevelCritical,
		Category:  "UNRESOLVED BLOCKED",
		Message:   message,
	}); err != nil {
		warnings = append(warnings, fmt.Sprintf("alert write failed: %v", err))
	}

	result.Warnings = warnings
	return &result, nil
}
