package ops

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/errors"
)

// The A6 sets and names below reach persisted batches, discoveries, outcome
// files, the integration progress codes the supervisor and prompts match, and
// the branded command lines guidance prints. Each is compared with the literal
// it carries on the wire, independently of the constant, so a rename cannot
// change what it spells.

func TestA6DeferredValidationVocabularyKeepsWireSpelling(t *testing.T) {
	t.Parallel()
	tests := map[string]struct{ got, want string }{
		"skip: nothing deferred":      {string(deferredValidationSkipNothingDeferred), "nothing_deferred"},
		"skip: attempts exhausted":    {string(deferredValidationSkipAttemptsExhausted), "attempts_exhausted"},
		"skip: not due":               {string(deferredValidationSkipNotDue), "not_due"},
		"skip: runner active":         {string(deferredValidationSkipRunnerActive), "runner_active"},
		"skip: halted":                {string(deferredValidationSkipHalted), "halted"},
		"skip: covered":               {string(deferredValidationSkipCovered), "covered"},
		"skip: failed at head":        {string(deferredValidationSkipFailedAtHead), "failed_at_head"},
		"skip: aborted at head":       {string(deferredValidationSkipAborted), "aborted_at_head"},
		"unattributed: no baseline":   {string(deferredValidationUnattributedNoBaseline), "no_baseline"},
		"unattributed: not ancestor":  {string(deferredValidationUnattributedBaselineNotAncestor), "baseline_not_in_ancestry"},
		"unattributed: no history":    {string(deferredValidationUnattributedHistoryUnavailable), "history_unavailable"},
		"unattributed: fails at base": {string(deferredValidationUnattributedFailsAtBaseline), "fails_at_baseline"},
		"unattributed: fails after":   {string(deferredValidationUnattributedFailsAfterLastMerge), "fails_after_last_merge"},
		"setup unavailable format":    {deferredValidationReasonSetupUnavailableFormat, "setup_unavailable at %s"},
		"discovery key batch":         {deferredDiscoveryKeyBatch, "batch"},
		"discovery key batch commit":  {deferredDiscoveryKeyBatchCommit, "batch_commit"},
		"discovery key command hash":  {deferredDiscoveryKeyCommandSHA256, "command_sha256"},
		"discovery key culprit task":  {deferredDiscoveryKeyCulpritTask, "culprit_task"},
		"discovery key merge commit":  {deferredDiscoveryKeyMergeCommit, "merge_commit"},
		"status subcommand":           {deferredValidationSubcommandStatus, "status"},
		"run subcommand":              {deferredValidationSubcommandRun, "run"},
		"force flag":                  {deferredValidationFlagForce, "--force"},
		"settle subcommand":           {string(rulingActionSettle), "settle"},
		"task env suffix":             {taskIDEnvSuffix, "TASK_ID"},
		"alert once-key format":       {deferredAlertOnceKeyFormat, "deferred-validation:%d:%s"},
		"discovery ID format":         {deferredDiscoveryIDFormat, "deferred-validation-%d-%d"},
		"lane worktree name":          {deferredLaneWorktreeName, "worktree"},
		"lane worktree format":        {deferredLaneWorktreeFormat, "worktree-%d"},
		"lane worktree glob":          {deferredLaneWorktreeGlob, "worktree-*"},
		"pathspec magic prefix":       {deferredPathspecMagicPrefix, ":"},
		"pathspec exclude prefix":     {deferredPathspecExcludePrefix, ":(exclude)"},
		"integration mutation lock":   {integrationMutationLockName, "integration-mutation"},
		"verify clean lock label":     {integrationLockVerifyClean, "verify clean integration source"},
		"worktree create operation":   {worktreeCreateOperation, "worktree-create"},
		"worktree setup operation":    {worktreeSetupLockOperation, "worktree-setup"},
		"setup output extension":      {worktreeSetupOutputExt, ".log"},
		"setup outcome extension":     {worktreeSetupOutcomeExt, ".outcome"},
		"worktree hooks dir suffix":   {worktreeHooksDirSuffix, "-hooks"},
		"worktree setup purpose":      {worktreeSetupLockPurposePrefix, "worktree-setup-"},
		"env file name":               {worktreeEnvFileName, ".env"},
		"env file prefix":             {worktreeEnvFilePrefix, ".env."},
		"env file suffix":             {worktreeEnvFileSuffix, ".env"},
		"direnv file name":            {worktreeEnvrcFileName, ".envrc"},
		"run-live shell flag":         {runLiveShellFlag, "-c"},
		"semble warning ellipsis":     {sembleWarningEllipsis, "..."},
	}
	for name, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", name, tc.got, tc.want)
		}
	}
	if os.FileMode(deferredValidationDirMode) != 0o755 || os.FileMode(deferredValidationLogFileMode) != 0o644 {
		t.Errorf("deferred-validation modes = %o, %o", deferredValidationDirMode, deferredValidationLogFileMode)
	}
	if got := fmt.Sprint(runLiveShells); got != "[sh bash zsh dash]" {
		t.Errorf("run-live shells = %s", got)
	}
	if runLiveShellWrapperArgs != 3 || worktreeSetupOutcomeFields != 3 {
		t.Errorf("record arities = %d, %d, want 3, 3", runLiveShellWrapperArgs, worktreeSetupOutcomeFields)
	}
	if postWorktreeSetupDefaultRetryDelay != 2*time.Second || worktreeSetupDefaultLockSlack != time.Minute {
		t.Errorf("setup defaults = %v, %v", postWorktreeSetupDefaultRetryDelay, worktreeSetupDefaultLockSlack)
	}
	if acceptanceExitUnknown != -1 || deferredGitExitNegative != 1 || sembleLsFilesUnmatchedExit != 1 || integrationAnalysisTaskPriority != 1 {
		t.Errorf("sentinels = %d, %d, %d, %d", acceptanceExitUnknown, deferredGitExitNegative, sembleLsFilesUnmatchedExit, integrationAnalysisTaskPriority)
	}
	if deferredValidationStatusBatches != 5 {
		t.Errorf("status batches = %d, want 5", deferredValidationStatusBatches)
	}
}

// TestA6TypedSetsKeepWireSpelling pins the A6 sets typed by the concern tasks:
// batch end reasons and run outcomes land in persisted batch records and JSON
// results, ruling actions and outcomes in the status JSON and audit log, and
// the evaluator's progress codes in IntegrationProgressReason.Code, which the
// supervisor and prompts match.
func TestA6TypedSetsKeepWireSpelling(t *testing.T) {
	t.Parallel()
	sets := map[string]map[string]string{
		"end reason": {
			"halted":                     string(deferredValidationReasonHalted),
			"runner_died":                string(deferredValidationReasonRunnerDied),
			"runner_settled_by_operator": string(deferredValidationReasonRunnerSettled),
			"descendants_unstoppable":    string(deferredValidationReasonUnstoppable),
			"admission_slot_error":       string(deferredValidationReasonAdmissionSlotError),
			"aborted_by_operator":        string(deferredValidationReasonOperatorAbort),
		},
		"run outcome": {
			"ran":     string(deferredValidationOutcomeRan),
			"skipped": string(deferredValidationOutcomeSkipped),
		},
		"ruling action": {
			"rule":   string(rulingActionRule),
			"unrule": string(rulingActionUnrule),
			"retire": string(rulingActionRetire),
			"settle": string(rulingActionSettle),
			"abort":  string(rulingActionAbort),
		},
		"ruling outcome": {
			"unchanged": string(rulingOutcomeUnchanged),
			"set":       string(rulingOutcomeSet),
			"removed":   string(rulingOutcomeRemoved),
			"retired":   string(rulingOutcomeRetired),
			"settled":   string(rulingOutcomeSettled),
			"requested": string(rulingOutcomeRequested),
		},
		"progress code": {
			"planning_unsettled":           string(integrationProgressWaitingPlanning),
			"slice_coverage_pending":       string(integrationProgressWaitingSliceCoverage),
			"coding_work_pending":          string(integrationProgressWaitingCoding),
			"integration_repairs_pending":  string(integrationProgressWaitingRepairs),
			"global_analysis_pending":      string(integrationProgressWaitingGlobalAnalysis),
			"closure_projection_pending":   string(integrationProgressWaitingClosure),
			"integration_repair_blocked":   string(integrationProgressBlockedRepair),
			"integration_analysis_blocked": string(integrationProgressBlockedAnalysis),
			"global_generations_exhausted": string(integrationProgressBlockedExhausted),
			"pipeline_upgrade_required":    string(integrationProgressBlockedCapability),
		},
	}
	for set, values := range sets {
		for want, got := range values {
			if got != want {
				t.Errorf("%s %q = %q", set, want, got)
			}
		}
	}
}

func TestA6ProgressCodesKeepWireSpelling(t *testing.T) {
	t.Parallel()
	// The exported wait is a plain string for its A9 callers; the blocked codes
	// are the evaluator's typed set and reach IntegrationProgressReason.Code by
	// conversion.
	var exported string = IntegrationProgressWaitingDeferredValidation
	if exported != "deferred_validation_pending" {
		t.Errorf("IntegrationProgressWaitingDeferredValidation = %q", exported)
	}
	for want, got := range map[string]integrationProgressCode{
		"deferred_validation_pending":   integrationProgressWaitingDeferred,
		"deferred_validation_failed":    integrationProgressBlockedDeferredFailed,
		"deferred_validation_infra":     integrationProgressBlockedDeferredInfra,
		"deferred_validation_unsettled": integrationProgressBlockedDeferredUnsettled,
		"deferred_validation_aborted":   integrationProgressBlockedDeferredAborted,
	} {
		if string(got) != want {
			t.Errorf("progress code %q = %q", want, got)
		}
	}
	if progressReason(integrationProgressWaitingDeferred).Code != IntegrationProgressWaitingDeferredValidation {
		t.Error("the typed wait and the exported wait spell different codes")
	}
}

func TestA6WorktreeSetupOutcomeStatusKeepsWireSpelling(t *testing.T) {
	t.Parallel()
	if worktreeSetupOutcomeOK != "ok" || worktreeSetupOutcomeFailed != "failed" {
		t.Fatalf("outcome statuses = %q, %q", worktreeSetupOutcomeOK, worktreeSetupOutcomeFailed)
	}
	// The record format is "<seq> <status> <RFC3339Nano>\n": a record written
	// with the typed statuses reads back, and an unknown status is malformed.
	path := t.TempDir() + "/outcome"
	seq, err := recordWorktreeSetupOutcome(path, true, mustParseSetupTime(t))
	if err != nil || seq != 1 {
		t.Fatalf("record ok = %d, %v", seq, err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "1 ok 2026-01-02T03:04:05Z\n" {
		t.Fatalf("ok record bytes = %q", raw)
	}
	if _, err := recordWorktreeSetupOutcome(path, false, mustParseSetupTime(t)); err != nil {
		t.Fatalf("record failed: %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "2 failed 2026-01-02T03:04:05Z\n" {
		t.Fatalf("failed record bytes = %q", raw)
	}
	outcome, ok, err := readWorktreeSetupOutcome(path)
	if err != nil || !ok || outcome.OK || outcome.Seq != 2 {
		t.Fatalf("read back = %+v, %v, %v", outcome, ok, err)
	}
	if err := os.WriteFile(path, []byte("3 unknown 2026-01-02T03:04:05Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readWorktreeSetupOutcome(path); err == nil {
		t.Fatal("an unknown status must be a malformed record")
	}
	if worktreeSetupDigestBytes != 8 {
		t.Errorf("digest bytes = %d, want 8", worktreeSetupDigestBytes)
	}
	if os.FileMode(worktreeSetupDirMode) != 0o700 || os.FileMode(worktreeSetupFileMode) != 0o600 {
		t.Errorf("setup modes = %o, %o", worktreeSetupDirMode, worktreeSetupFileMode)
	}
}

// TestA6AdoptedOwnerValuesKeepTheirSpelling ties the A6 sites that adopted an
// owner constant to the literal they replaced.
func TestA6AdoptedOwnerValuesKeepTheirSpelling(t *testing.T) {
	t.Parallel()
	if notFoundEntityTask != "task" {
		t.Errorf("notFoundEntityTask = %q", notFoundEntityTask)
	}
	if err := (&errors.NotFoundError{Entity: notFoundEntityTask, ID: "T-1"}).Error(); err != "task not found: T-1" {
		t.Errorf("not-found text = %q", err)
	}
	if gitModeRegularFile != "100644" || gitModeExecutableFile != "100755" {
		t.Errorf("git file modes = %q, %q", gitModeRegularFile, gitModeExecutableFile)
	}
	if codePlanningRolePair != "code-planning-pair" {
		t.Errorf("code planning role pair = %q", codePlanningRolePair)
	}
	if actorOperator != "operator" {
		t.Errorf("actorOperator = %q", actorOperator)
	}
	if slicedCohortMinScopes != 2 {
		t.Errorf("sliced cohort minimum = %d, want 2", slicedCohortMinScopes)
	}
	// The wt-merge lifecycle results name the A3 kernel constants.
	for want, got := range map[string]string{
		"stop":          string(LifecycleSafeActionStop),
		"requery":       string(LifecycleSafeActionRequery),
		"correct_input": string(LifecycleSafeActionCorrectInput),
		"none":          string(LifecycleEffectsNone),
	} {
		if got != want {
			t.Errorf("lifecycle vocabulary %q = %q", want, got)
		}
	}
}

func mustParseSetupTime(t *testing.T) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, "2026-01-02T03:04:05Z")
	if err != nil {
		t.Fatal(err)
	}
	return at
}
