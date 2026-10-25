package ops

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// The spellings below are persisted, hashed or matched by other areas, so each
// typed vocabulary is pinned literal by literal: renaming a constant must not
// change what it spells.

func TestK7VocabularySpelling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "ReviewClaimClass",
			got: []string{
				string(ReviewClaimClassAcceptanceEvidence), string(ReviewClaimClassReviewBoundaryRepair),
				string(ReviewClaimClassWorktreeContext), string(ReviewClaimClassGitOperation),
				string(ReviewClaimClassIntegrationFailed), string(ReviewClaimClassCandidateFailures),
				string(ReviewClaimClassAuthority), string(ReviewClaimClassDegraded),
				string(ReviewClaimClassNoWork), string(ReviewClaimClassUnclassified),
			},
			want: []string{
				"acceptance_evidence", "review_boundary_repair", "worktree_context", "git_operation",
				"integration_failed", "candidate_failures", "authority", "degraded", "no_work", "unclassified",
			},
		},
		{
			name: "AgentDegradedReason",
			got: []string{
				string(AgentDegradedWorktreeCreateFailed), string(AgentDegradedWorktreeReadonly),
				string(AgentDegradedWorktreePermissionDenied), string(AgentDegradedWorktreeSetupFailed),
			},
			want: []string{
				"claim_worktree_create_failed", "claim_worktree_readonly",
				"claim_worktree_permission_denied", "claim_worktree_setup_failed",
			},
		},
		{
			name: "runtimeInputCode",
			got: []string{
				string(runtimeInputCodeConsumed), string(runtimeInputCodeInvalidated),
				string(runtimeInputCodeMissing), string(runtimeInputCodeUnavailable),
			},
			want: []string{
				"runtime_input_consumed", "runtime_input_invalidated",
				"runtime_input_missing", "runtime_input_unavailable",
			},
		},
		{
			name: "stateLockHoldReason",
			got: []string{
				string(stateLockHoldRepairRequired), string(stateLockHoldHumanRequired),
				string(stateLockHoldRejectionRCARequired), string(stateLockHoldPreparationPending),
				string(stateLockHoldAwaitedTasksPending), string(stateLockHoldInvalidDependencies),
				string(stateLockHoldDependenciesPending), string(stateLockHoldChanged),
				string(stateLockHoldPublicationPending), string(stateLockHoldSystemNotRunning),
				string(stateLockHoldSprintNotRunning), string(stateLockHoldInvalidRolePair),
				string(stateLockHoldInvalidDependencyDir), string(stateLockHoldWorktreeMetadataInvalid),
				string(stateLockHoldWorktreeBaseInvalid), string(stateLockHoldWorktreeUnhealthy),
				string(stateLockHoldWorktreeBranchChanged), string(stateLockHoldUnblockDomainRefused),
				string(stateLockHoldUnblockNoChange),
			},
			want: []string{
				"repair_required", "human_required", "rejection_rca_required", "preparation_pending",
				"awaited_tasks_pending", "invalid_dependencies", "dependencies_pending", "hold_changed",
				"publication_pending", "system_not_running", "sprint_not_running", "invalid_role_pair",
				"invalid_dependency_direction", "worktree_metadata_invalid", "worktree_base_invalid",
				"worktree_unhealthy", "worktree_branch_changed", "unblock_domain_refused", "unblock_no_change",
			},
		},
		{
			name: "assessmentOutcome",
			got: []string{
				string(assessmentOutcomeMissing), string(assessmentOutcomeSatisfied),
				string(assessmentOutcomeFailedOrBlocked), string(assessmentOutcomeReplaced),
				string(assessmentOutcomeUnknown), string(assessmentOutcomePending),
			},
			want: []string{"missing", "satisfied", "failed_or_blocked", "replaced", "unknown", "pending"},
		},
		{
			name: "awaitedState",
			got:  []string{string(awaitedPending), string(awaitedSatisfied), string(awaitedFailed)},
			want: []string{"pending", "satisfied", "failed"},
		},
		{
			name: "configSetOutcome",
			got: []string{
				string(configSetUnchanged), string(configSetConflict), string(configSetSet),
				string(configSetReplaced), string(configSetRemoved), string(configSetCleared),
				string(configSetFailed),
			},
			want: []string{"unchanged", "conflict", "set", "replaced", "removed", "cleared", "failed"},
		},
		{
			name: "preservedClaimRecovery",
			got: []string{
				string(preservedClaimRetryRebase), string(preservedClaimResolveConflict),
				string(preservedClaimRepairBase), string(preservedClaimCleanWorktree),
			},
			want: []string{
				"retry_preserved_claim_rebase", "resolve_preserved_claim_rebase_conflict",
				"repair_preserved_claim_base", "clean_preserved_claim_worktree",
			},
		},
		{
			name: "named values",
			got: []string{
				claimTaskOperation, claimTaskProjectLockOperation, claimTaskWorktreeLockOperation,
				claimTaskTakeoverLockOperation, degradedByClaimTask, claimTaskLockInfix,
				recoverAgentOperation, recoverTaskOperation, recoverTaskLockOperation,
				handoffOperation, markBlockedOperation, releaseClaimOperation,
				transitionAttemptOperation, pauseOperation, resumeOperation,
				projectCleanupLockOperation, updateSprintMetricsOperation, recordLifecycleOutcomeLockOperation,
				actorOperator, actorHuman, actorSystem, notFoundEntityTask, notFoundEntityAgent,
				alertCategoryUnresolvedBlocked, alertCategoryBlocked, humanNoteSourceOperatorCLI,
				humanNoteForAll, operationalCodeGitOperation, gitModeRegularFile, gitModeExecutableFile,
				circuitBreakerStatusOK, circuitBreakerStatusTriggered, workAdmissionLockName,
				workAdmissionBarrierSuffix, projectLifecycleLockName, agentLifecycleLockPrefix,
				admissionQueueDirName, admissionLockFileSuffix, admissionOwnerFileSuffix,
				validationContextChanged, interruptedOperationRebase, goalCompleteRestoredBy,
				goalCompleteStopLinearization, goalCompleteRestoreLinearization, integrationStateChangedCode,
				lifecycleMetricsFileExt, postWorktreeDetectionMatch, configConflictExistingValue,
				runtimeInputNoBoundInstance, stateLockHoldUnblockPrefix, stateLockHoldRequestPrefix,
				string(lifecycleTaskStatusUnobserved),
			},
			want: []string{
				"claim-task", "task-claim-worktree", "claim-task-worktree",
				"claim-task-takeover", "claim_task", ".claim-",
				"recover-agent", "recover-task", "task-recover-worktree",
				"handoff", "mark-blocked", "release-claim",
				"transition-attempt", "pause", "resume",
				"project-cleanup", "update-sprint-metrics", "record-lifecycle-outcome",
				"operator", "human", "system", "task", "agent",
				"UNRESOLVED BLOCKED", "BLOCKED", "operator_cli",
				"all", "git_operation", "100644", "100755",
				"OK", "TRIGGERED", "work-admission",
				"-barrier", "project-lifecycle", "agent-lifecycle-",
				"queue", ".lock", ".lock.owner.json",
				"context_changed", "rebase", "system:integration-head-verification",
				"goal-complete stop", "restore stale goal-complete stop", "integration_state_changed",
				".json", "match", "existing_value",
				"none", "unblock_", "state-lock-hold-",
				"UNKNOWN",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if len(tt.got) != len(tt.want) {
				t.Fatalf("got %d spellings, want %d", len(tt.got), len(tt.want))
			}
			for i := range tt.want {
				if tt.got[i] != tt.want[i] {
					t.Errorf("spelling %d = %q, want %q", i, tt.got[i], tt.want[i])
				}
			}
		})
	}
}

func TestK7VocabularyNumericValues(t *testing.T) {
	t.Parallel()

	for name, c := range map[string][2]int{
		"runtime input run id bytes": {runtimeInputRunIDBytes, 8},
		"claim lock key bytes":       {claimTaskLockKeyBytes, 8},
		"preserved evidence limit":   {preservedClaimEvidenceLimit, 2000},
		"rebase skipped note limit":  {rebaseSkippedNoteLimit, 500},
		"unblock rebase excerpt":     {unblockRebaseExcerptLimit, 2000},
		"max blocked questions":      {maxBlockedQuestions, 3},
		"hypothesis failures":        {hypothesisExhaustionFailures, 2},
		"max task attempt":           {models.MaxTaskAttempt, 2},
	} {
		if c[0] != c[1] {
			t.Errorf("%s = %d, want %d", name, c[0], c[1])
		}
	}
}

// The new vocabulary types are plain string types: a method on one would be a
// serialization hook that changes persisted bytes.
func TestK7VocabularyTypesCarryNoMethods(t *testing.T) {
	t.Parallel()

	for _, typ := range []reflect.Type{
		reflect.TypeFor[ReviewClaimClass](),
		reflect.TypeFor[AgentDegradedReason](),
		reflect.TypeFor[runtimeInputCode](),
		reflect.TypeFor[stateLockHoldReason](),
		reflect.TypeFor[assessmentOutcome](),
		reflect.TypeFor[awaitedState](),
		reflect.TypeFor[configSetOutcome](),
		reflect.TypeFor[preservedClaimRecovery](),
	} {
		if typ.Kind() != reflect.String {
			t.Errorf("%s kind = %s, want string", typ, typ.Kind())
		}
		if n := typ.NumMethod() + reflect.PointerTo(typ).NumMethod(); n != 0 {
			t.Errorf("%s has %d methods, want none", typ, n)
		}
	}
}

// Boundary characterisations: each named threshold accepts the value just
// under it and refuses the value at it.
func TestK7BlockerQuestionBoundary(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		questions int
		wantErr   bool
	}{
		{questions: maxBlockedQuestions},
		{questions: maxBlockedQuestions + 1, wantErr: true},
	} {
		call := &assessBlockedCall{taskID: "task-1", opts: AssessBlockedOptions{
			Reason:    "needs input",
			Questions: make([]string, tt.questions),
		}}
		err := call.normalizeBlocker()
		if !tt.wantErr {
			if err != nil {
				t.Errorf("%d questions: unexpected error %v", tt.questions, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "maximum 3 questions allowed per blocking protocol") {
			t.Errorf("%d questions: error = %v, want the maximum-3 refusal", tt.questions, err)
		}
	}
}

func TestK7HypothesisExhaustionBoundary(t *testing.T) {
	t.Parallel()

	transitions := map[models.TaskStatus][]models.TaskStatus{
		models.TaskStatusImplementing: {models.TaskStatusBlocked},
		models.TaskStatusBlocked:      nil,
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tt := range []struct {
		failedBy []string
		want     bool
	}{
		{failedBy: nil},
		{failedBy: []string{"coder-1"}},
		{failedBy: []string{"coder-1", "coder-2"}, want: true},
		{failedBy: []string{"coder-1", "coder-2", "coder-3"}, want: true},
	} {
		task := &models.Task{ID: "task-1", Status: models.TaskStatusImplementing, FailedBy: tt.failedBy}
		state := &models.State{Tasks: []models.Task{*task}}
		got, err := blockTaskForHypothesisExhaustion(state, task, "orch", transitions, now)
		if err != nil {
			t.Fatalf("failed_by %v: %v", tt.failedBy, err)
		}
		if got != tt.want {
			t.Errorf("failed_by %v: blocked = %v, want %v", tt.failedBy, got, tt.want)
		}
		if tt.want && task.Status != models.TaskStatusBlocked {
			t.Errorf("failed_by %v: status = %s, want BLOCKED", tt.failedBy, task.Status)
		}
	}
}

func TestK7LimitEscalationAttemptBoundary(t *testing.T) {
	t.Parallel()

	first, hit := classifyLimitEscalation(5, 5, 0, 10, models.MaxTaskAttempt-1)
	if !hit || first.action != LimitActionNewAttempt {
		t.Errorf("attempt %d: hit=%v action=%v, want new attempt", models.MaxTaskAttempt-1, hit, first.action)
	}
	last, hit := classifyLimitEscalation(5, 5, 0, 10, models.MaxTaskAttempt)
	if !hit || last.action != LimitActionBlocked {
		t.Errorf("attempt %d: hit=%v action=%v, want blocked", models.MaxTaskAttempt, hit, last.action)
	}
}
