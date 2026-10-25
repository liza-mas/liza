package ops

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/secretmask"
)

// Diagnostic field names a replacement refusal can point at.
const (
	replaceFieldRequestID          = "request_id"
	replaceFieldExpectedTransition = "expected_transition"
	replaceFieldReplacementID      = "replacement.id"
)

// checkReplaceTaskRequest refuses a malformed request before any invocation
// exists: identity fields, options, then payload schema.
func checkReplaceTaskRequest(input ReplaceTaskInput, opts LifecycleRequestOptions) error {
	if opts.RequestID == "" || opts.ExpectedTransition == "" {
		field := replaceFieldRequestID
		if opts.RequestID != "" {
			field = replaceFieldExpectedTransition
		}
		return NewLifecycleInvalidInputError(replaceTaskOperation, nil, []models.FieldDiagnostic{{SchemaVersion: 1, Field: field, Constraint: "required for replacement request identity", ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput}}, fmt.Errorf("replacement requires request-id and expected-transition"))
	}
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return WrapLifecycleError(replaceTaskOperation, nil, err, models.LifecycleInvalidInput, LifecycleSafeActionCorrectInput, LifecycleEffectsNone)
	}
	// Even telemetry capture reads under the blackboard lock. Malformed
	// payloads must be rejected before constructing the invocation.
	_, diagnostics, err := payloadschema.Validate(replaceTaskOperation, input)
	if err != nil {
		return err
	}
	if len(diagnostics) > 0 {
		return NewLifecycleInvalidInputError(replaceTaskOperation, nil, diagnostics, &PreconditionError{Reason: diagnostics[0].Constraint})
	}
	return nil
}

// admit reads the source outside the lock, checks the caller's authority and
// gathers every verdict that needs Git or the project tree. The preserved-base
// error and the acceptance and runtime-input verdicts are held, not returned:
// like the base, they are reported only after replay, so a completed request
// still replays when today's carrier would refuse it.
func (p *replacePass) admit(opts LifecycleRequestOptions) error {
	if err := validateReplaceTaskInput(p.input); err != nil {
		return err
	}
	p.input.Changed = secretmask.New().MaskText(strings.TrimSpace(p.input.Changed))
	p.bb = RequestBlackboard(paths.New(p.projectRoot).StatePath(), &p.authority, opts)
	state, source, err := readTaskState(p.bb, p.input.SourceTaskID)
	if err != nil {
		return err
	}
	if err := RequireAgentAuthority(state, p.authority); err != nil {
		return err
	}
	p.observed = source
	if p.pb, err = loadPipelineBundle(p.projectRoot); err != nil {
		return err
	}
	if p.request, err = NewLifecycleRequest(replaceTaskOperation, source, p.authority.ID, &p.authority, opts, p.input); err != nil {
		return err
	}
	// Git stays outside the state lock, but its admission error must not
	// invalidate a retained receipt after the successor worktree is cleaned up.
	p.base, p.baseErr = validateReplacementBase(p.projectRoot, p.input)
	p.acceptance = checkReplacementAcceptance(p.projectRoot, state, source, p.input, p.pb)
	p.runtimeInputVerdict = checkReplacementRuntimeInputs(p.projectRoot, state, source, p.input, p.pb)
	return nil
}

// replayCompleted answers the source's retained receipt inside the lock. An
// exact replay projects the original completion into p.result; a reused
// identity names the field that differs.
func (p *replacePass) replayCompleted(source *models.Task) (bool, error) {
	receipt, err := checkOwnerEndingRequest(source, p.request)
	if err != nil {
		if errors.Is(err, ErrLifecycleIdentityReused) {
			// Receipts retain a digest, not the original payload. Name the
			// conflicting identity unless durable lineage proves an ID change.
			field := replaceFieldRequestID
			if len(source.SupersededBy) == 1 && source.SupersededBy[0] != p.input.Replacement.ID {
				field = replaceFieldReplacementID
			}
			return false, replacementConflict(source, p.request, field, err)
		}
		return false, err
	}
	if receipt == nil {
		return false, nil
	}
	if len(source.SupersededBy) != 1 || source.SupersededBy[0] != p.input.Replacement.ID {
		return false, lifecycleRequestError(source, p.request, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone, "replacement lineage no longer matches the completed request")
	}
	outcome := LifecycleReplayOutcome(source, receipt, p.authority.ID)
	outcome.Effects = string(LifecycleEffectsNone)
	consumers, ok := replacementCompletionConsumers(source, receipt.TransitionID)
	if !ok {
		return false, lifecycleRequestError(source, p.request, models.LifecycleStateChanged, LifecycleSafeActionRequery, LifecycleEffectsNone, "replacement completion audit is unavailable")
	}
	p.result = &ReplaceTaskResult{LifecycleOutcome: outcome, SourceTaskID: source.ID, ReplacementTaskID: source.SupersededBy[0], SourceOriginalStatus: receipt.Projection.SourceStatus, RetargetedConsumers: consumers}
	return true, nil
}

// checkReplacementAcceptance runs the creation acceptance check on the
// replacement as the transaction will build it, inherited lineage included.
func checkReplacementAcceptance(projectRoot string, state *models.State, source *models.Task, input ReplaceTaskInput, pb *pipelineBundle) acceptanceCreationCheck {
	replacementInput := input.Replacement
	replacement, err := buildReplacementTask(&replacementInput, pb.resolver)
	if err != nil {
		// The transaction rebuilds it and reports this error in its own order.
		return acceptanceCreationCheck{}
	}
	inheritReplacementLineage(&replacement, source)
	return checkCreatedTaskAcceptance(projectRoot, state, &replacement)
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
	err := NewLifecycleConflictError(replaceTaskOperation, source, []models.FieldDiagnostic{{SchemaVersion: 1, Field: field, Constraint: "must not reuse an identity for a different replacement intent", ValueClass: models.FieldValueClassConflict, SafeAction: models.FieldDiagnosticRequery}}, cause)
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
	if len(input.Replacement.ReserveSuccessors) > 0 {
		return &PreconditionError{Reason: "replacement.reserve_successors is add-tasks only: the replacement inherits the source's provider_reservations; place other writers with reserve-provider"}
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

func validateReplacementBase(projectRoot string, input ReplaceTaskInput) (*PreservedTaskBase, error) {
	if input.PreservedBase == nil {
		return nil, nil
	}
	base := *input.PreservedBase
	invalid := func(field, reason string) (*PreservedTaskBase, error) {
		return nil, NewLifecycleInvalidInputError(replaceTaskOperation, nil, []models.FieldDiagnostic{{SchemaVersion: 1, Field: "preserved_base." + field, Constraint: reason, ValueClass: models.FieldValueClassMalformed, SafeAction: models.FieldDiagnosticCorrectInput}}, fmt.Errorf("invalid preserved base: %s", reason))
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
