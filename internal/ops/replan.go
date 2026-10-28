package ops

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// ReplanInput holds the parameters for a replan operation.
type ReplanInput struct {
	TaskID    string // optional — auto-detect if empty
	ChangedBy string // required — actor metadata for history/logs
	// Reason is optional. When set it is appended to the replacement's
	// description, so the planner and plan reviewer see what must change.
	Reason                 string
	PreserveOutputIdentity bool
	Trigger                string
}

// ReplanResult contains the outcome of a replan operation.
type ReplanResult struct {
	OriginalTaskID string
	NewTaskID      string
	RolePair       string
	SpecRef        string
	// Resumed reports that the sprint left CHECKPOINT for IN_PROGRESS.
	Resumed  bool
	Warnings []string
}

// replanOperation is the operation value replan records in alignment and
// dependency-rewrite history.
const replanOperation = "replan"

// Replan invalidates a merged planning task's output and creates a new planning
// task so the planner agent re-reads the amended plan. At CHECKPOINT the sprint
// is set back to IN_PROGRESS so agents resume. At IN_PROGRESS (the orchestrator's
// PLANNING_COMPLETE turn, or after an automatic resume) status and checkpoint
// trigger are left alone, so the sprint's other plans still transition. What
// makes replan safe is that no child exists yet, checked under the lock.
func Replan(projectRoot string, input *ReplanInput) (*ReplanResult, error) {
	if input.ChangedBy == "" {
		return nil, &PreconditionError{Reason: "changed_by is required"}
	}

	lp := paths.New(projectRoot)
	statePath := lp.StatePath()

	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline config: %w", err)
	}

	planningPairs, err := loadPlanningPairsForAdvance(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load planning pairs: %w", err)
	}

	bb := db.For(statePath)
	if input.PreserveOutputIdentity {
		return replanPreservingIdentity(projectRoot, bb, planningPairs, input)
	}

	var result ReplanResult
	err = bb.Modify(func(state *models.State) error {
		replanned, err := replanUnderLock(projectRoot, bb, state, resolver, planningPairs, input)
		if err != nil {
			return err
		}
		result = replanned
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("replan failed: %w", err)
	}

	if err := projectReplanResult(lp, input.ChangedBy, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// replanPreservingIdentity routes --preserve-output-identity to a reviewed
// correction of the original instead of a replacement. Loading and the call
// stay outside any state lock: AmendPlan owns its own transaction.
func replanPreservingIdentity(projectRoot string, bb *db.Blackboard, planningPairs map[string]bool, input *ReplanInput) (*ReplanResult, error) {
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}
	if state.Sprint.Status != models.SprintStatusCheckpoint && state.Sprint.Status != models.SprintStatusInProgress {
		return nil, &PreconditionError{Reason: "sprint must be at CHECKPOINT or IN_PROGRESS"}
	}
	original, err := resolveReplanTarget(state, input.TaskID, planningPairs)
	if err != nil {
		return nil, err
	}
	reason := input.Reason
	if strings.TrimSpace(reason) == "" {
		reason = "Review replacement while retaining the existing output selectors"
	}
	correction, err := AmendPlan(projectRoot, AmendPlanInput{TaskID: original.ID, ChangedBy: input.ChangedBy, Reason: reason, Mode: models.PlanAmendmentPreserveIdentity, Trigger: input.Trigger})
	if err != nil {
		return nil, err
	}
	return &ReplanResult{OriginalTaskID: original.ID, NewTaskID: correction.CorrectionID, RolePair: original.RolePair, SpecRef: original.SpecRef, Resumed: state.Sprint.Status == models.SprintStatusCheckpoint}, nil
}

// replanOriginal is what adoption reads of the replanned task once the
// replacement is appended, which may move the state's task slice.
type replanOriginal struct{ id, rolePair, specRef string }

// replanReplacement is a constructed replacement and the warnings its
// construction raised, ready for adoption.
type replanReplacement struct {
	task     models.Task
	warnings []string
}

// replanUnderLock runs every locked step in order: eligibility, lineage
// resolution, original retirement, replacement construction and adoption.
func replanUnderLock(projectRoot string, bb *db.Blackboard, state *models.State, resolver *pipeline.Resolver, planningPairs map[string]bool, input *ReplanInput) (ReplanResult, error) {
	task, err := checkReplanEligibility(state, resolver, planningPairs, input.TaskID)
	if err != nil {
		return ReplanResult{}, err
	}
	atCheckpoint := state.Sprint.Status == models.SprintStatusCheckpoint
	newTaskID, initialStatus, err := resolveReplanLineage(state, resolver, task)
	if err != nil {
		return ReplanResult{}, err
	}

	now := time.Now().UTC()
	reason := strings.TrimSpace(input.Reason)
	if err := retireReplannedOriginal(state, resolver, task, newTaskID, reason, input.ChangedBy, now); err != nil {
		return ReplanResult{}, err
	}
	original := replanOriginal{id: task.ID, rolePair: task.RolePair, specRef: task.SpecRef}
	replacement := buildReplanReplacement(state, task, newTaskID, initialStatus, input, reason, now)
	warnings := adoptReplanReplacement(state, resolver, original, replacement, input.ChangedBy, reason, atCheckpoint, now)

	if models.HasProviderDependencies(state) {
		if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, true, os.Stderr); err != nil {
			return ReplanResult{}, &PreconditionError{Reason: fmt.Sprintf("replan provider dependencies: %v", err)}
		}
	}
	return ReplanResult{
		OriginalTaskID: original.id,
		NewTaskID:      newTaskID,
		RolePair:       original.rolePair,
		SpecRef:        original.specRef,
		Resumed:        atCheckpoint,
		Warnings:       warnings,
	}, nil
}

// checkReplanEligibility resolves the task to replan and refuses, in order,
// every state that makes a replacement unsafe or pointless.
func checkReplanEligibility(state *models.State, resolver *pipeline.Resolver, planningPairs map[string]bool, taskID string) (*models.Task, error) {
	task, err := resolveReplanTarget(state, taskID, planningPairs)
	if err != nil {
		return nil, err
	}
	if state.Sprint.Status != models.SprintStatusCheckpoint && state.Sprint.Status != models.SprintStatusInProgress {
		return nil, &PreconditionError{Reason: fmt.Sprintf(
			"sprint must be at CHECKPOINT or IN_PROGRESS, got %s", state.Sprint.Status)}
	}
	if err := checkReplanTaskState(task, planningPairs); err != nil {
		return nil, err
	}
	if err := checkReplanProviders(state, resolver, task); err != nil {
		return nil, err
	}
	return task, nil
}

// checkReplanTaskState refuses a task that is fenced, retired, unmerged,
// without output, already expanded, outside a planning pair or held.
func checkReplanTaskState(task *models.Task, planningPairs map[string]bool) error {
	if task.PlanGenerationFenced() {
		return &PreconditionError{Reason: "cannot replan a correction or an original with a pending amendment; use amend-plan --replace-pending for recovery"}
	}
	if task.PlanHandoffRetired() {
		return &PreconditionError{Reason: fmt.Sprintf("task %s handoff was retired by %s; it cannot be replanned", task.ID, task.RetiredBy())}
	}
	if task.Status != models.TaskStatusMerged {
		return &PreconditionError{Reason: fmt.Sprintf(
			"task %s must be MERGED, got %s", task.ID, task.Status)}
	}
	if len(task.Output) == 0 {
		return &PreconditionError{Reason: fmt.Sprintf(
			"task %s has no output to replan", task.ID)}
	}
	if len(task.TransitionsExecuted) > 0 {
		return &PreconditionError{Reason: fmt.Sprintf(
			"cannot replan — child tasks already created from task %s output. Cancel children first", task.ID)}
	}
	if !IsPlanningPair(task.RolePair, planningPairs) {
		return &PreconditionError{Reason: fmt.Sprintf(
			"task %s role_pair %q is not a planning pair", task.ID, task.RolePair)}
	}
	// A human hold is released only by an operator clear; a replacement
	// would otherwise mint an unheld identity for the same plan.
	if task.PlanCheckVerdictOf() == models.PlanCheckHeld {
		return &PreconditionError{Reason: fmt.Sprintf(
			"task %s is held for human action (%s); an operator must run plan-check %s --clear before replanning",
			task.ID, task.PlanCheck.Ask, task.ID)}
	}
	return nil
}

// checkReplanProviders refuses a replan that would retire a referenced provider
// or copy a stale provider declaration onto the replacement.
func checkReplanProviders(state *models.State, resolver *pipeline.Resolver, task *models.Task) error {
	if err := rejectReferencedProviderRetirement(state, resolver, task.ID, retireByReplan); err != nil {
		return err
	}
	// An unstarted task may keep a stale declaration (ADR-0187), but new
	// work never inherits one: the replacement would be unclaimable with
	// nothing to wake on it (see TECH_DEBT.md).
	for index, dep := range task.ProviderDependencies {
		if retired, _ := staleProviderReference(state, resolver, dep); retired != "" {
			return &PreconditionError{Reason: fmt.Sprintf(
				"task %s provider_dependencies[%d] names retired %s; replan would copy that stale declaration to the replacement", task.ID, index, retired)}
		}
	}
	return nil
}

// resolveReplanLineage names the replacement (<original-id>-replan-N) and the
// initial status of the original's role pair.
func resolveReplanLineage(state *models.State, resolver *pipeline.Resolver, task *models.Task) (string, models.TaskStatus, error) {
	newTaskID := computeReplanID(state, task.ID)
	initialStatus, err := resolver.InitialStatus(task.RolePair)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve initial status for %q: %w", task.RolePair, err)
	}
	return newTaskID, initialStatus, nil
}

// retireReplannedOriginal invalidates the original's output and blocks all
// outbound transitions, routes stale provider consumers and records the
// replacement in the original's history.
//
// The "replanned" marker alone doesn't prevent real transitions (e.g.
// "epic-to-us") from firing, because availableTransitionsByTrigger checks
// transitionsExecuted[transitionName], not "replanned". Each real transition
// must also be marked executed.
func retireReplannedOriginal(state *models.State, resolver *pipeline.Resolver, task *models.Task, newTaskID, reason, changedBy string, now time.Time) error {
	if task.TransitionsExecuted == nil {
		task.TransitionsExecuted = make(map[string]bool)
	}
	task.TransitionsExecuted[models.ReplannedTransition] = true

	approvedStatus, approvedErr := resolver.ApprovedStatus(task.RolePair)
	if approvedErr == nil {
		for _, txName := range resolver.AvailableManualTransitions(approvedStatus, nil) {
			task.TransitionsExecuted[txName] = true
		}
		for _, txName := range resolver.AvailableAutoTransitions(approvedStatus, nil) {
			task.TransitionsExecuted[txName] = true
		}
	}

	if err := routeStaleProviderConsumers(state, resolver, task.ID, retireByReplan, changedBy, now); err != nil {
		return err
	}
	note := fmt.Sprintf("replaced by %s", newTaskID)
	if reason != "" {
		note += ": " + reason
	}
	task.History = append(task.History, models.TaskHistoryEntry{
		Time:  now,
		Event: models.TaskEventReplanned,
		Note:  &note,
	})
	return nil
}

// buildReplanReplacement clones the original's declarations into the new
// planning task. The replacement is new work to be re-authored. An edge to an
// upstream replanned while the original was MERGED (adoption's retarget loop
// skips terminal tasks) follows that upstream's live successor, as it would
// have had the replacement existed then. Without one the edge is kept, so the
// reconcile signal stays visible.
func buildReplanReplacement(state *models.State, task *models.Task, newTaskID string, initialStatus models.TaskStatus, input *ReplanInput, reason string, now time.Time) replanReplacement {
	dependsOn, successorEdges := replanSuccessorDependencies(state, task.DependsOn)

	originalID := task.ID
	newTask := models.Task{
		PlanningChange:         models.NewPlanningChange(models.PlanningChangeReplan, input.Trigger, task.ID),
		ID:                     newTaskID,
		Type:                   task.Type,
		RolePair:               task.RolePair,
		Description:            replanDescription(task.Description, input.ChangedBy, reason),
		Status:                 initialStatus,
		Priority:               task.Priority,
		ParentTask:             task.ParentTask,
		ParentTasks:            slices.Clone(task.ParentTasks),
		SpecRef:                task.SpecRef,
		EpicRef:                task.EpicRef,
		PlanRef:                task.PlanRef,
		ArchRef:                task.ArchRef,
		Kind:                   task.Kind,
		RCARequired:            task.RCARequired,
		DoneWhen:               task.DoneWhen,
		Scope:                  task.Scope,
		DependsOn:              dependsOn,
		ProviderDependencies:   models.CloneProviderDependencies(task.ProviderDependencies),
		DescendantDependencies: models.CloneDescendantDependencies(task.DescendantDependencies),
		ProviderReservations:   slices.Clone(task.ProviderReservations),
		MaxOutputs:             task.MaxOutputs,
		Supersedes:             &originalID,
		Created:                now,
		History:                []models.TaskHistoryEntry{},
	}
	var warnings []string
	for _, edge := range successorEdges {
		note := fmt.Sprintf("dependency on replanned %s follows its successor %s", edge[0], edge[1])
		newTask.History = append(newTask.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventDependenciesRewritten,
			Agent: &input.ChangedBy,
			Note:  &note,
			Extra: map[string]any{
				"operation":              replanOperation,
				"replanned_dependency":   edge[0],
				"replacement_dependency": edge[1],
			},
		})
		warnings = append(warnings, fmt.Sprintf("task %s: %s", newTaskID, note))
	}
	return replanReplacement{task: newTask, warnings: warnings}
}

// adoptReplanReplacement appends the replacement to the candidate state,
// retargets and retires what referenced the original, extends the sprint scope,
// resumes a checkpointed sprint and records alignment. It returns the warnings
// in the order they were raised.
func adoptReplanReplacement(state *models.State, resolver *pipeline.Resolver, original replanOriginal, replacement replanReplacement, changedBy, reason string, atCheckpoint bool, now time.Time) []string {
	newTaskID := replacement.task.ID
	state.Tasks = append(state.Tasks, replacement.task)

	retargetReplanConsumers(state, original.id, newTaskID)
	warnings := replacement.warnings
	warnings = append(warnings, retireReplanSelections(state, original.id, newTaskID, changedBy, now)...)
	warnings = append(warnings, replanTerminalDependentWarnings(state, original.id)...)
	warnings = append(warnings, staleProviderConsumerWarnings(state, resolver, original.id)...)

	state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, newTaskID)
	if atCheckpoint {
		state.Sprint.Status = models.SprintStatusInProgress
		state.Sprint.CheckpointTrigger = ""
	}

	state.Goal.AlignmentHistory = append(state.Goal.AlignmentHistory, models.AlignmentHistory{
		Timestamp: now,
		Event:     replanOperation,
		Summary: fmt.Sprintf("Replanned task %s → %s (role_pair: %s, spec: %s)%s",
			original.id, newTaskID, original.rolePair, original.specRef, replanReasonSuffix(reason)),
	})
	return warnings
}

// retargetReplanConsumers moves downstream non-terminal tasks' DependsOn from
// the original to the replacement.
func retargetReplanConsumers(state *models.State, originalID, newTaskID string) {
	for i := range state.Tasks {
		if state.Tasks[i].Status.IsTerminal() {
			continue
		}
		changed := false
		for j := range state.Tasks[i].DependsOn {
			if state.Tasks[i].DependsOn[j] == originalID {
				state.Tasks[i].DependsOn[j] = newTaskID
				changed = true
			}
		}
		if changed {
			state.Tasks[i].DependsOn = dedupeStrings(state.Tasks[i].DependsOn)
		}
	}
}

// retireReplanSelections retires selective inherit_inputs that name the
// replanned task and returns one warning per retired selection.
//
// Deliberately NOT filtered by IsTerminal(), unlike retargetReplanConsumers.
// That loop concerns a task's own DependsOn, and a terminal task will not be
// claimed again. Output[].InheritInputs on a MERGED task is different: MERGED
// is terminal *and* is the status children are generated from, so a merged
// producer's output is live input to generation, not historical audit data.
//
// Indexes are not retargeted onto the replacement. Replanning exists to change
// the upstream's output[], so index k no longer denotes what the planner
// selected; carrying it across would silently reinterpret the selection, which
// is worse than losing it because nothing would detect it. Degrading to the
// whole-phase barrier is the safe direction — it is a superset of any
// selection, it cannot fail, and it is the same behavior as omitted intent.
//
// This makes the state honest; it does not recover the prerequisite. When the
// producer is terminal its DependsOn still names the replanned task (the
// retarget loop skipped it), that task has no children by design, and the
// barrier is lost either way. The warning is the existing mitigation for that;
// see TECH_DEBT.md.
func retireReplanSelections(state *models.State, originalID, newTaskID, changedBy string, now time.Time) []string {
	var warnings []string
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		for entryIndex := range candidate.Output {
			entry := &candidate.Output[entryIndex]
			retired, named := entry.InheritInputs.SelectionFor(originalID)
			if !named {
				continue
			}
			entry.InheritInputs = &models.InheritInputs{Mode: models.InheritModeAll}

			// Persisted on the producer, not only returned to the caller:
			// the seam was chosen for a durable record at the moment of
			// cause, and after this command returns the state would
			// otherwise show Mode "all" with no trace it was ever
			// selective or why.
			note := fmt.Sprintf("output[%d] selection into replanned task %s retired to whole-phase inheritance",
				entryIndex, originalID)
			candidate.History = append(candidate.History, models.TaskHistoryEntry{
				Time:  now,
				Event: models.TaskEventDependenciesRewritten,
				Agent: &changedBy,
				Note:  &note,
				Extra: map[string]any{
					"output_index":           entryIndex,
					"replanned_task":         originalID,
					"replacement_task":       newTaskID,
					"retired_output_index":   retired,
					"rewrote_inherit_inputs": true,
				},
			})
			warnings = append(warnings, fmt.Sprintf(
				"task %s output[%d] selected specific outputs of replanned task %s; "+
					"selection retired to whole-phase inheritance (replacement %s)",
				candidate.ID, entryIndex, originalID, newTaskID))
		}
	}
	return warnings
}

// replanTerminalDependentWarnings names terminal tasks that still depend on
// the replanned task.
func replanTerminalDependentWarnings(state *models.State, originalID string) []string {
	var warnings []string
	for i := range state.Tasks {
		if !state.Tasks[i].Status.IsTerminal() {
			continue
		}
		if slices.Contains(state.Tasks[i].DependsOn, originalID) {
			warnings = append(warnings,
				fmt.Sprintf("task %s is %s and depends on replanned task %s — consider replanning %s too",
					state.Tasks[i].ID, state.Tasks[i].Status, originalID, state.Tasks[i].ID))
		}
	}
	return warnings
}

// projectReplanResult is the post-commit tail: the best-effort activity log,
// whose failure becomes a warning, and the post-write state validation.
func projectReplanResult(lp paths.LizaPaths, changedBy string, result *ReplanResult) error {
	logger := log.New(lp.LogPath())
	logEntry := log.Entry{
		Timestamp: time.Now().UTC(),
		Agent:     changedBy,
		Action:    "task_replanned",
		Task:      &result.NewTaskID,
		Detail:    fmt.Sprintf("Replanned %s → %s", result.OriginalTaskID, result.NewTaskID),
	}
	if logErr := logger.Append(logEntry); logErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", logErr))
	}

	if valErr := statevalidate.ValidateStateFile(lp.StatePath(), false, io.Discard); valErr != nil {
		return &PostWriteValidationError{Err: valErr}
	}
	return nil
}

// resolveReplanTarget finds the task to replan. If taskID is provided, looks it
// up directly. Otherwise scans sprint.scope.planned for a single unconsumed
// planning task.
func resolveReplanTarget(state *models.State, taskID string, planningPairs map[string]bool) (*models.Task, error) {
	if taskID != "" {
		task := state.FindTask(taskID)
		if task == nil {
			return nil, &PreconditionError{Reason: fmt.Sprintf("task %q not found", taskID)}
		}
		return task, nil
	}

	// Auto-detect: scan planned tasks for unconsumed planning output
	var matches []*models.Task
	for _, id := range state.Sprint.Scope.Planned {
		task := state.FindTask(id)
		if IsUnconsumedPlanningOutput(task, planningPairs) {
			matches = append(matches, task)
		}
	}

	switch len(matches) {
	case 0:
		return nil, &PreconditionError{Reason: "no planning task with unconsumed output found in current sprint"}
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, t := range matches {
			ids[i] = t.ID
		}
		return nil, &PreconditionError{Reason: fmt.Sprintf(
			"multiple planning tasks found — specify task ID: %s", strings.Join(ids, ", "))}
	}
}

// replanDescription carries the replan reason to the replacement's planner and
// reviewer, who see the task description but not the original's history.
func replanDescription(description, changedBy, reason string) string {
	if reason == "" {
		return description
	}
	return fmt.Sprintf("%s\n\nReplan reason (from %s): %s", description, changedBy, reason)
}

func replanReasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return ": " + reason
}

// liveReplanSuccessor is the replan successor of id unless that successor was
// itself retired (superseded, abandoned or its hand-off replaced).
func liveReplanSuccessor(state *models.State, id string) (*models.Task, bool) {
	successor, ok := models.ReplanSuccessor(state, id)
	if !ok || successor.PlanHandoffRetired() || (successor.Status.IsTerminal() && successor.Status != models.TaskStatusMerged) {
		return nil, false
	}
	return successor, true
}

// replanSuccessorDependencies maps each dependency with a live replan
// successor to that successor and reports the rewritten {old, new} edges.
// Unchanged lists are returned as-is.
func replanSuccessorDependencies(state *models.State, deps []string) ([]string, [][2]string) {
	resolved := slices.Clone(deps)
	var edges [][2]string
	for i, dep := range deps {
		if successor, ok := liveReplanSuccessor(state, dep); ok {
			resolved[i] = successor.ID
			edges = append(edges, [2]string{dep, successor.ID})
		}
	}
	if len(edges) == 0 {
		return resolved, nil
	}
	return dedupeStrings(resolved), edges
}

// dedupeStrings returns a new slice with duplicates removed, preserving order.
func dedupeStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// computeReplanID generates <original-id>-replan-N by counting existing
// replan tasks for the same original.
func computeReplanID(state *models.State, originalID string) string {
	prefix := originalID + "-replan-"
	count := 0
	for _, task := range state.Tasks {
		if strings.HasPrefix(task.ID, prefix) {
			count++
		}
	}
	return fmt.Sprintf("%s%d", prefix, count+1)
}
