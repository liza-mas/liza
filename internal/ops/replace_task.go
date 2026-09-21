package ops

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// PreservedTaskBase declares an existing claimable worktree and its base.
// Both fields are required: a base without a worktree is overwritten at claim.
type PreservedTaskBase struct {
	BaseCommit string `json:"base_commit"`
	Worktree   string `json:"worktree"`
}

// ReplaceTaskInput binds creation, consumer updates and retirement in one intent.
type ReplaceTaskInput struct {
	SourceTaskID  string                    `json:"source_task_id"`
	Reason        string                    `json:"reason"`
	Replacement   AddTaskInput              `json:"replacement"`
	Consumers     []models.DependencyUpdate `json:"consumers"`
	PreservedBase *PreservedTaskBase        `json:"preserved_base,omitempty"`
}

// ReplaceTaskResult reports the committed replacement graph and source boundary.
type ReplaceTaskResult struct {
	models.LifecycleOutcome
	SourceTaskID         string            `json:"source_task_id"`
	ReplacementTaskID    string            `json:"replacement_task_id"`
	SourceOriginalStatus models.TaskStatus `json:"source_original_status"`
	RetargetedConsumers  []string          `json:"retargeted_consumers"`
	Warnings             []string          `json:"warnings,omitempty"`
}

// Per-blackboard barriers permit observing and rejecting a partially composed
// candidate in tests without changing the shared mutation/locking primitives.
var replaceTaskCandidateTestHooks sync.Map

type replaceTaskTestHooks struct {
	beforeLock   func()
	afterUpdates func(*models.State) error
}

// ReplaceTaskWithAuthorityAndOptions validates and commits one replacement under
// the source ownership lock and one generation-fenced state transaction. External
// worktree validation precedes locking; cleanup and telemetry follow persistence.
func ReplaceTaskWithAuthorityAndOptions(projectRoot string, input ReplaceTaskInput, authority models.AgentAuthority, opts LifecycleRequestOptions) (result *ReplaceTaskResult, retErr error) {
	const operation = "replace-task"
	if opts.RequestID == "" || opts.ExpectedTransition == "" {
		field := "request_id"
		if opts.RequestID != "" {
			field = "expected_transition"
		}
		return nil, NewLifecycleInvalidInputError(operation, nil, []models.FieldDiagnostic{{SchemaVersion: 1, Field: field, Constraint: "required for replacement request identity", ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput}}, fmt.Errorf("replacement requires request-id and expected-transition"))
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, WrapLifecycleError(operation, nil, err, models.LifecycleInvalidInput, "correct_input", "none")
	}
	// Even telemetry capture reads under the blackboard lock. Malformed
	// payloads must be rejected before constructing the invocation.
	_, diagnostics, err := payloadschema.Validate(operation, input)
	if err != nil {
		return nil, err
	}
	if len(diagnostics) > 0 {
		return nil, NewLifecycleInvalidInputError(operation, nil, diagnostics, &PreconditionError{Reason: diagnostics[0].Constraint})
	}
	invocation := NewLifecycleInvocation(projectRoot)
	var observed *models.Task
	defer func() {
		retErr = WrapLifecycleError(operation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if result != nil {
			outcome, warnings = result.LifecycleOutcome, &result.Warnings
		}
		invocation.FinishResult(operation, outcome, &retErr, warnings)
	}()
	if err := validateReplaceTaskInput(input); err != nil {
		return nil, err
	}
	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	state, source, err := readTaskState(bb, input.SourceTaskID)
	if err != nil {
		return nil, err
	}
	if err := RequireAgentAuthority(state, authority); err != nil {
		return nil, err
	}
	observed = source
	pb, err := loadPipelineBundle(projectRoot)
	if err != nil {
		return nil, err
	}
	request, err := NewLifecycleRequest(operation, source, authority.ID, &authority, opts, input)
	if err != nil {
		return nil, err
	}
	// Git stays outside the state lock, but its admission error must not
	// invalidate a retained receipt after the successor worktree is cleaned up.
	base, baseErr := validateReplacementBase(projectRoot, input)
	var hadWorktree bool
	if hook, ok := replaceTaskCandidateTestHooks.Load(bb); ok && hook.(replaceTaskTestHooks).beforeLock != nil {
		hook.(replaceTaskTestHooks).beforeLock()
	}
	err = withOwnershipTaskLock(projectRoot, input.SourceTaskID, operation, func() error {
		return lifecycleMutation(bb, &authority)(func(candidate *models.State) (callbackErr error) {
			source := candidate.FindTask(input.SourceTaskID)
			if source == nil {
				return WrapLifecycleError(operation, nil, fmt.Errorf("source task disappeared"), models.LifecycleStateChanged, "requery", "none")
			}
			// Preserve the durable observation: candidate mutation is not evidence
			// of a committed transition if any subsequent validation rejects it.
			copy := *source
			copy.Lifecycle = cloneTaskLifecycle(source.Lifecycle)
			observed = &copy
			defer func() {
				callbackErr = WrapLifecycleError(operation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
			}()
			receipt, err := checkOwnerEndingRequest(source, request)
			if err != nil {
				if errors.Is(err, ErrLifecycleIdentityReused) {
					// Receipts retain a digest, not the original payload. Name the
					// conflicting identity unless durable lineage proves an ID change.
					field := "request_id"
					if len(source.SupersededBy) == 1 && source.SupersededBy[0] != input.Replacement.ID {
						field = "replacement.id"
					}
					return replacementConflict(source, request, field, err)
				}
				return err
			}
			if receipt != nil {
				if len(source.SupersededBy) != 1 || source.SupersededBy[0] != input.Replacement.ID {
					return lifecycleRequestError(source, request, models.LifecycleStateChanged, "requery", "none", "replacement lineage no longer matches the completed request")
				}
				outcome := LifecycleReplayOutcome(source, receipt, authority.ID)
				outcome.Effects = "none"
				consumers, ok := replacementCompletionConsumers(source, receipt.TransitionID)
				if !ok {
					return lifecycleRequestError(source, request, models.LifecycleStateChanged, "requery", "none", "replacement completion audit is unavailable")
				}
				result = &ReplaceTaskResult{LifecycleOutcome: outcome, SourceTaskID: source.ID, ReplacementTaskID: source.SupersededBy[0], SourceOriginalStatus: receipt.Projection.SourceStatus, RetargetedConsumers: consumers}
				return errLifecycleReplay
			}
			if baseErr != nil {
				return baseErr
			}
			if !replacementSourceEligible(source, pb) {
				return WrapLifecycleError(operation, observed, fmt.Errorf("source is no longer eligible for replacement"), models.LifecycleAlreadyTransitioned, "stop", "none")
			}
			if candidate.FindTask(input.Replacement.ID) != nil {
				return replacementConflict(source, request, "replacement.id", fmt.Errorf("replacement task ID already exists"))
			}
			replacement, err := buildReplacementTask(&input.Replacement, pb.resolver)
			if err != nil {
				return err
			}
			if base != nil {
				replacement.BaseCommit = &base.BaseCommit
				replacement.Worktree = &base.Worktree
			}
			if err := insertTaskInState(candidate, projectRoot, replacement, &input.Replacement, pb.resolver); err != nil {
				return err
			}
			// Appending to Tasks may move its backing array.
			source = candidate.FindTask(input.SourceTaskID)
			now := time.Now().UTC()
			// Both cores append dependency-rewrite events, including output-only
			// canonicalization. Snapshot cursors so old rewrites cannot leak in.
			historyStarts := make(map[string]int, len(candidate.Tasks))
			for _, task := range candidate.Tasks {
				historyStarts[task.ID] = len(task.History)
			}
			_, err = applyDependencyUpdatesInState(candidate, pb.resolver, source, operation, input.Consumers, authority.ID, input.Reason, now, map[string]any{"operation": operation})
			if err != nil {
				return err
			}
			if hook, ok := replaceTaskCandidateTestHooks.Load(bb); ok && hook.(replaceTaskTestHooks).afterUpdates != nil {
				if err := hook.(replaceTaskTestHooks).afterUpdates(candidate); err != nil {
					return err
				}
			}
			hadWorktree = source.Worktree != nil
			originalStatus := source.Status
			if _, err := supersedeTaskInState(candidate, pb, source, []string{input.Replacement.ID}, input.Reason, authority.ID, nil, now); err != nil {
				return err
			}
			consumers := replacementRewrittenConsumers(candidate, source.ID, historyStarts)
			extra := map[string]any{"source_task_id": source.ID, "replacement_task_id": replacement.ID, "source_prior_transition_id": request.ExpectedTransition, "retargeted_consumers": consumers, "request_id": request.RequestID}
			if base != nil {
				extra["preserved_base_commit"] = base.BaseCommit
			}
			source.History = append(source.History, models.TaskHistoryEntry{Time: now, Event: models.TaskEventReplacementCommitted, Agent: &authority.ID, Extra: extra})
			if err := statevalidate.ValidateState(candidate, projectRoot, false, io.Discard); err != nil {
				return err
			}
			outcome, err := CompleteLifecycleRequest(source, request, models.LifecycleProjection{SourceStatus: originalStatus}, candidate.Agents)
			if err != nil {
				return err
			}
			// Extra is excluded from the transition digest; append the event before
			// completion, then fill its new token without changing the boundary.
			extra["source_new_transition_id"] = outcome.TransitionID
			result = &ReplaceTaskResult{LifecycleOutcome: outcome, SourceTaskID: source.ID, ReplacementTaskID: replacement.ID, SourceOriginalStatus: originalStatus, RetargetedConsumers: consumers}
			return nil
		})
	})
	if isLifecycleReplay(err) {
		return result, nil
	}
	if err != nil {
		// A nil callback followed by a persistence failure is uncertain, never
		// an INVALID_INPUT instruction to blindly rewrite the same intent.
		if result != nil {
			return nil, WrapLifecycleError(operation, observed, err, models.LifecycleStateChanged, "requery", "unknown")
		}
		return nil, err
	}
	gw := git.New(projectRoot)
	if hadWorktree {
		if err := gw.RemoveWorktreeDir(input.SourceTaskID); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("failed to remove source worktree directory: %v", err))
		}
	}
	// A replacement exists, so keep the source branch for successor salvage.
	result.Warnings = append(result.Warnings, cleanupPredecessorBranches(bb, gw, input.SourceTaskID)...)
	if err := log.New(lp.LogPath()).Append(log.Entry{Timestamp: time.Now().UTC(), Agent: authority.ID, Action: operation, Task: &input.SourceTaskID, Detail: "replacement=" + input.Replacement.ID}); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}
	return result, nil
}

func replacementRewrittenConsumers(state *models.State, sourceID string, historyStarts map[string]int) []string {
	consumers := []string{}
	for _, task := range state.Tasks {
		if task.ID == sourceID {
			continue
		}
		for _, entry := range task.History[historyStarts[task.ID]:] {
			if entry.Event == models.TaskEventDependenciesRewritten {
				consumers = append(consumers, task.ID)
				break // Explicit updates and supersession may rewrite the same task.
			}
		}
	}
	slices.Sort(consumers)
	return consumers
}

func replacementCompletionConsumers(source *models.Task, transitionID string) ([]string, bool) {
	for _, entry := range source.History {
		if entry.Event == models.TaskEventReplacementCommitted && entry.Extra["source_new_transition_id"] == transitionID {
			consumers, ok := entry.Extra["retargeted_consumers"]
			// Return a copy of the original report, never infer from today's graph.
			return append([]string{}, extraToStringSlice(consumers)...), ok
		}
	}
	return nil, false
}

func replacementConflict(source *models.Task, request LifecycleRequest, field string, cause error) *LifecycleError {
	err := NewLifecycleConflictError("replace-task", source, []models.FieldDiagnostic{{SchemaVersion: 1, Field: field, Constraint: "must not reuse an identity for a different replacement intent", ValueClass: models.FieldValueClassConflict, SafeAction: models.FieldDiagnosticRequery}}, cause)
	err.Outcome.RequestID = request.RequestID
	return err
}

func validateReplaceTaskInput(input ReplaceTaskInput) error {
	if err := paths.ValidateTaskID(input.SourceTaskID); err != nil {
		return err
	}
	if strings.TrimSpace(input.Reason) == "" {
		return &PreconditionError{Reason: "replacement reason is required"}
	}
	if err := validateAddTaskInput(&input.Replacement); err != nil {
		return err
	}
	if input.Replacement.ID == input.SourceTaskID {
		return &PreconditionError{Reason: "replacement must differ from source"}
	}
	seen := make(map[string]bool, len(input.Consumers))
	for _, update := range input.Consumers {
		if err := paths.ValidateTaskID(update.TaskID); err != nil {
			return err
		}
		if seen[update.TaskID] || update.TaskID == input.SourceTaskID || update.TaskID == input.Replacement.ID {
			return &PreconditionError{Reason: "consumers must be unique existing tasks distinct from source and replacement"}
		}
		seen[update.TaskID] = true
	}
	return nil
}

func replacementSourceEligible(task *models.Task, pb *pipelineBundle) bool {
	if task.Status == models.TaskStatusBlocked || task.Status == models.TaskStatusIntegrationFailed {
		return true
	}
	if task.RolePair == "" {
		return task.Status == models.TaskStatusReady || task.Status == models.TaskStatusRejected
	}
	initial, err := pb.resolver.InitialStatus(task.RolePair)
	if err == nil && task.Status == initial {
		return true
	}
	rejected, err := pb.resolver.RejectedStatus(task.RolePair)
	return err == nil && task.Status == rejected
}

func validateReplacementBase(projectRoot string, input ReplaceTaskInput) (*PreservedTaskBase, error) {
	if input.PreservedBase == nil {
		return nil, nil
	}
	base := *input.PreservedBase
	invalid := func(field, reason string) (*PreservedTaskBase, error) {
		return nil, NewLifecycleInvalidInputError("replace-task", nil, []models.FieldDiagnostic{{SchemaVersion: 1, Field: "preserved_base." + field, Constraint: reason, ValueClass: models.FieldValueClassMalformed, SafeAction: models.FieldDiagnosticCorrectInput}}, fmt.Errorf("invalid preserved base: %s", reason))
	}
	if base.BaseCommit == "" {
		return invalid("base_commit", "a resolvable commit is required")
	}
	canonical := filepath.Join(paths.WorktreesDirName, input.Replacement.ID)
	if base.Worktree != canonical {
		return invalid("worktree", "canonical replacement worktree is required")
	}
	gw := git.New(projectRoot)
	commit, err := gw.GetCommitSHA(base.BaseCommit + "^{commit}")
	if err != nil {
		return invalid("base_commit", "must resolve to a commit")
	}
	if err := gw.ValidateWorktreeHealth(input.Replacement.ID); err != nil {
		return invalid("worktree", "must exist and be healthy")
	}
	branch, err := gw.GetWorktreeBranch(gw.GetWorktreePath(input.Replacement.ID))
	if err != nil || branch != paths.TaskBranchPrefix+input.Replacement.ID {
		return invalid("worktree", "must be on the replacement task branch")
	}
	head, err := gw.GetWorktreeHEAD(input.Replacement.ID)
	if err != nil {
		return invalid("worktree", "HEAD must resolve")
	}
	ancestor, err := gw.IsAncestor(commit, head)
	if err != nil || !ancestor {
		return invalid("base_commit", "must be an ancestor of worktree HEAD")
	}
	base.BaseCommit = commit
	return &base, nil
}
