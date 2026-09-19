package ops

import (
	stderrors "errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const (
	retargetDependencyOperation              = "retarget-dependency"
	retargetDependencyRejectedAction         = "retarget_dependency_rejected"
	retargetDependencyCandidateValidation    = "candidate-state-validation"
	retargetDependencyRejectedDetailMaxBytes = 2048
)

// RetargetDependencyResult contains the outcome of retargeting one task
// dependency edge.
type RetargetDependencyResult struct {
	models.LifecycleOutcome
	TaskID                string   `json:"task_id"`
	OldDependency         string   `json:"old_dependency"`
	NewDependencies       []string `json:"new_dependencies"`
	CanonicalDependencies []string `json:"canonical_dependencies"`
	RepairRequestCleared  bool     `json:"repair_request_cleared"`
	Warnings              []string `json:"warnings,omitempty"`
}

func (r *RetargetDependencyResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// RetargetDependency replaces one non-terminal task's direct depends_on edge
// with one or more direct dependencies. It is a metadata repair operation: it
// does not unblock the task or execute repair validation commands.
func RetargetDependency(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil)
}

// RetargetDependencyWithAuthority fences the dependency rewrite with the
// orchestrator's registration generation.
func RetargetDependencyWithAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority)
}

// RetargetDependencyWithOptions identifies a dependency replacement invocation.
func RetargetDependencyWithOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, agentID, nil, opts)
}

// RetargetDependencyWithAuthorityAndOptions fences an identified replacement.
func RetargetDependencyWithAuthorityAndOptions(projectRoot, taskID, oldDependency string, newDependencies []string, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*RetargetDependencyResult, error) {
	return retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency, newDependencies, reason, authority.ID, &authority, opts)
}

func retargetDependencyWithOptionalAuthority(projectRoot, taskID, oldDependency string, newDependencies []string, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *RetargetDependencyResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(retargetDependencyOperation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(retargetDependencyOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if taskID == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	if oldDependency == "" {
		return nil, &PreconditionError{Reason: "old dependency is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}

	normalizedNewDeps, err := normalizeRetargetNewDependencies(newDependencies)
	if err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(retargetDependencyOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}

	var result RetargetDependencyResult
	now := time.Now().UTC()

	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(retargetDependencyOperation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		copy := *task
		observed = &copy
		request, err := NewLifecycleRequest(retargetDependencyOperation, task, agentID, authority, opts, struct {
			OldDependency   string
			NewDependencies []string
			Reason          string
		}{oldDependency, normalizedNewDeps, reason})
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(task, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result = RetargetDependencyResult{TaskID: taskID, LifecycleOutcome: LifecycleReplayOutcome(task, receipt, agentID)}
			return errLifecycleReplay
		}
		if task.Status.IsTerminal() {
			return WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("cannot retarget dependencies on terminal task %s (%s)", taskID, task.Status)}, models.LifecycleAlreadyTransitioned, "stop", "none")
		}
		if !slices.Contains(task.DependsOn, oldDependency) {
			return WrapLifecycleError(retargetDependencyOperation, task, &PreconditionError{Reason: fmt.Sprintf("task %s does not depend on %s", taskID, oldDependency)}, models.LifecycleStateChanged, "requery", "none")
		}
		for _, depID := range normalizedNewDeps {
			if depID == task.ID {
				return &PreconditionError{Reason: fmt.Sprintf("task %s cannot depend on itself", task.ID)}
			}
			if state.FindTask(depID) == nil {
				return &PreconditionError{Reason: fmt.Sprintf("new dependency %q does not exist", depID)}
			}
		}

		replaced := replaceDependency(task.DependsOn, oldDependency, normalizedNewDeps)
		canonical, _, err := canonicalizeConcreteDependencyList(state, resolver, task.ID, task.RolePair, replaced)
		if err != nil {
			return err
		}
		if len(canonical) == 0 {
			return &PreconditionError{Reason: "retarget-dependency cannot remove all dependencies; use an explicit dependency-removal operation instead"}
		}

		repairExtra := matchingRetargetRepairExtra(task.RepairRequest, taskID, oldDependency, canonical)
		repairRequestCleared := repairExtra != nil
		if repairRequestCleared {
			task.RepairRequest = nil
		}

		task.DependsOn = canonical
		extra := map[string]any{
			"manual":                 true,
			"operation":              retargetDependencyOperation,
			"old_dependency":         oldDependency,
			"new_dependencies":       append([]string(nil), normalizedNewDeps...),
			"canonical_dependencies": append([]string(nil), canonical...),
			"repair_request_cleared": repairRequestCleared,
		}
		for key, value := range repairExtra {
			extra[key] = value
		}
		note := fmt.Sprintf("retargeted dependency %s -> %s", oldDependency, strings.Join(normalizedNewDeps, ", "))
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDependenciesRewritten,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &note,
			Extra:  extra,
		})

		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return err
		}

		result = RetargetDependencyResult{
			TaskID:                taskID,
			OldDependency:         oldDependency,
			NewDependencies:       append([]string(nil), normalizedNewDeps...),
			CanonicalDependencies: append([]string(nil), canonical...),
			RepairRequestCleared:  repairRequestCleared,
		}
		result.LifecycleOutcome, err = CompleteLifecycleRequest(task, request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		var cycleErr *statevalidate.DependencyCycleError
		if stderrors.As(err, &cycleErr) {
			cyclePath := slices.Clone(cycleErr.CyclePath)
			recordRetargetDependencyRejection(lp.LogPath(), now, agentID, taskID, oldDependency, normalizedNewDeps, cyclePath, reason, err)
			return nil, WrapLifecycleError(retargetDependencyOperation, observed, &OperationalError{
				Code:    "validation",
				Phase:   retargetDependencyCandidateValidation,
				Message: "retarget dependency rejected because the candidate state contains a dependency cycle",
				Details: map[string]any{
					"operation":         retargetDependencyOperation,
					"task_id":           taskID,
					"old_dependency":    oldDependency,
					"new_dependencies":  append([]string(nil), normalizedNewDeps...),
					"cycle_path":        cyclePath,
					"diagnostic_action": retargetDependencyRejectedAction,
				},
				Err: err,
			}, models.LifecycleInvalidInput, "correct_input", "none")
		}
		return nil, WrapLifecycleError(retargetDependencyOperation, observed, fmt.Errorf("failed to retarget dependency: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	logger := log.New(lp.LogPath())
	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    retargetDependencyOperation,
		Task:      &taskID,
		Detail:    fmt.Sprintf("%s -> %s: %s", oldDependency, strings.Join(normalizedNewDeps, ","), reason),
	}
	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return &result, nil
}

func recordRetargetDependencyRejection(logPath string, timestamp time.Time, agentID, taskID, oldDependency string, newDependencies, cyclePath []string, reason string, err error) {
	detail := fmt.Sprintf(
		"operation=%s phase=%s task_id=%s old_dependency=%s new_dependencies=%s cycle_path=%s reason=%s error=%v",
		retargetDependencyOperation,
		retargetDependencyCandidateValidation,
		taskID,
		oldDependency,
		strings.Join(newDependencies, ","),
		strings.Join(cyclePath, " -> "),
		reason,
		err,
	)
	// boundedString appends this marker after applying its byte limit, so reserve
	// the marker bytes and discard any partial UTF-8 rune at the cut point.
	const truncationMarker = "... [truncated]"
	detail = strings.ToValidUTF8(
		boundedMaskedString(detail, retargetDependencyRejectedDetailMaxBytes-len(truncationMarker)),
		"",
	)
	// The candidate mutation is already rejected; a secondary audit-log failure
	// must not replace the validation result.
	_ = log.New(logPath).Append(log.Entry{
		Timestamp: timestamp,
		Agent:     agentID,
		Action:    retargetDependencyRejectedAction,
		Task:      &taskID,
		Detail:    detail,
	})
}

func normalizeRetargetNewDependencies(values []string) ([]string, error) {
	var normalized []string
	seen := make(map[string]bool)
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			depID := strings.TrimSpace(part)
			if depID == "" {
				return nil, &PreconditionError{Reason: "new dependency entries cannot be empty"}
			}
			if seen[depID] {
				return nil, &PreconditionError{Reason: fmt.Sprintf("duplicate new dependency %q", depID)}
			}
			seen[depID] = true
			normalized = append(normalized, depID)
		}
	}
	if len(normalized) == 0 {
		return nil, &PreconditionError{Reason: "at least one new dependency is required"}
	}
	return normalized, nil
}

func replaceDependency(deps []string, oldDependency string, newDependencies []string) []string {
	replaced := make([]string, 0, len(deps)+len(newDependencies)-1)
	for _, depID := range deps {
		if depID == oldDependency {
			replaced = append(replaced, newDependencies...)
			continue
		}
		replaced = append(replaced, depID)
	}
	return dedupeStrings(replaced)
}

func matchingRetargetRepairExtra(request *models.RepairRequest, taskID, oldDependency string, canonicalDependencies []string) map[string]any {
	if request == nil || request.Operation != retargetDependencyOperation || request.Target != taskID {
		return nil
	}
	if slices.Contains(canonicalDependencies, oldDependency) {
		return nil
	}
	if !repairCommandMatchesRetargetEdge(request.Command, taskID, oldDependency) {
		return nil
	}

	return map[string]any{
		"repair_operation":  request.Operation,
		"repair_target":     request.Target,
		"repair_command":    request.Command,
		"repair_evidence":   append([]string(nil), request.Evidence...),
		"repair_validation": append([]string(nil), request.Validation...),
	}
}

func repairCommandMatchesRetargetEdge(command, taskID, oldDependency string) bool {
	fields := strings.Fields(command)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] != retargetDependencyOperation {
			continue
		}
		if fields[i+1] != taskID || fields[i+2] != oldDependency {
			continue
		}
		return true
	}
	return false
}
