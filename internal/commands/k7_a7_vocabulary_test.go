package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/procscan"
	"github.com/liza-mas/liza/internal/scipsearch"
)

// pinSpellings fails on every pair whose constant no longer equals the literal
// the CLI, the state file or a parser accepts. The literals are written out on
// purpose: the test must not read the constant on both sides.
func pinSpellings(t *testing.T, group string, pairs [][2]string) {
	t.Helper()
	for _, pair := range pairs {
		if pair[0] != pair[1] {
			t.Errorf("%s: constant spells %q, want %q", group, pair[0], pair[1])
		}
	}
}

func TestA7InspectVocabularyKeepsAcceptedSpellings(t *testing.T) {
	pinSpellings(t, "format", [][2]string{
		{string(inspectFormatJSON), "json"}, {string(inspectFormatYAML), "yaml"},
		{string(inspectFormatTable), "table"}, {string(inspectFormatValue), "value"},
		{InspectFormatJSON, "json"}, {InspectFormatTable, "table"}, {InspectFormatValue, "value"},
	})
	pinSpellings(t, "target", [][2]string{
		{string(inspectTargetConfig), "config"}, {string(inspectTargetSprint), "sprint"},
		{string(inspectTargetTasks), "tasks"}, {string(inspectTargetAgents), "agents"},
		{string(inspectTargetMetrics), "metrics"}, {string(inspectTargetAnomalies), "anomalies"},
		{InspectTargetTasks, "tasks"}, {InspectTargetAgents, "agents"},
		{InspectTargetMetrics, "metrics"}, {InspectTargetAnomalies, "anomalies"},
	})
	pinSpellings(t, "query root", [][2]string{
		{string(inspectRootAgents), "agents"}, {string(inspectRootTasks), "tasks"},
		{string(inspectRootSprint), "sprint"}, {string(inspectRootAgent), "agent"},
		{string(inspectRootTask), "task"}, {string(inspectRootVersion), "version"},
	})
	pinSpellings(t, "computed field", [][2]string{
		{string(computedFieldActiveCount), "active_count"}, {string(computedFieldUtilization), "utilization"},
		{string(computedFieldCompletionRate), "completion_rate"}, {string(computedFieldAvgIterationCount), "avg_iteration_count"},
		{string(computedFieldElapsed), "elapsed"}, {string(computedFieldRemaining), "remaining"},
		{string(computedFieldProgressPercent), "progress_percent"}, {string(computedFieldTimeSinceHeartbeat), "time_since_heartbeat"},
		{string(computedFieldTimeOnTask), "time_on_task"}, {string(computedFieldTransitionID), "transition_id"},
		{string(computedFieldAge), "age"}, {string(computedFieldTimeInStatus), "time_in_status"},
	})
	pinSpellings(t, "reserved path", [][2]string{
		{tasksComputedQueryPrefix, "tasks."}, {taskQueryPrefix, "task."}, {queryPathSeparator, "."},
		{sprintTimelinePath, "sprint.timeline"}, {taskFieldGenerationDigest, "generation_digest"},
		{inspectTagKey, "inspect"}, {yamlTagKey, "yaml"}, {tagOmitted, "-"},
		{zeroDurationText, "0s"}, {unknownRoleLabel, "unknown"}, {emptyCell, "-"},
	})
	if percentScale != 100 {
		t.Errorf("percentScale = %d, want 100", percentScale)
	}
	if got, want := InspectFormatNames(), []string{"json", "yaml", "table", "value"}; !reflect.DeepEqual(got, want) {
		t.Errorf("InspectFormatNames() = %v, want %v", got, want)
	}
	for _, query := range []string{"tasks.completion_rate", "tasks.avg_iteration_count"} {
		if !isReservedTasksComputedQuery(query) {
			t.Errorf("%q must stay a reserved aggregate query", query)
		}
	}
	for _, query := range []string{"tasks.id", "tasks.completion", "task.completion_rate"} {
		if isReservedTasksComputedQuery(query) {
			t.Errorf("%q must not be a reserved aggregate query", query)
		}
	}
	for _, field := range []string{"age", "time_in_status", "transition_id"} {
		if !isTaskComputedField(field) {
			t.Errorf("%q must stay a task computed field", field)
		}
	}
	if isTaskComputedField("utilization") || isTaskComputedField("") {
		t.Error("only age, time_in_status and transition_id are task computed fields")
	}
}

func TestA7StatusVocabularyKeepsAcceptedSpellings(t *testing.T) {
	pinSpellings(t, "status", [][2]string{
		{string(statusFormatJSON), "json"}, {string(statusFormatYAML), "yaml"}, {string(statusFormatDashboard), "dashboard"},
		{circuitBreakerStatusOK, "OK"}, {circuitBreakerStatusTriggered, "TRIGGERED"},
		{verdictApproved, "APPROVED"}, {processStatusRunning, "running"},
	})
	if got, want := StatusFormatNames(), []string{"json", "yaml", "dashboard"}; !reflect.DeepEqual(got, want) {
		t.Errorf("StatusFormatNames() = %v, want %v", got, want)
	}
	// The state's own validity check accepts exactly the two spellings the views compare.
	for _, status := range []string{circuitBreakerStatusOK, circuitBreakerStatusTriggered} {
		if !(&models.CircuitBreaker{Status: status}).IsValidStatus() {
			t.Errorf("circuit breaker status %q is no longer accepted by the model", status)
		}
	}
	// Both the stored verdict and the await outcome spell an approval the same way.
	if string(ops.VerdictApproved) != verdictApproved {
		t.Errorf("verdictApproved %q differs from the await outcome %q", verdictApproved, ops.VerdictApproved)
	}
	if string(procscan.AgentProcessMismatched) != "mismatched" {
		t.Errorf("procscan mismatch state = %q, want mismatched", procscan.AgentProcessMismatched)
	}
}

func TestA7KernelVocabularyKeepsWireSpellings(t *testing.T) {
	pinSpellings(t, "lifecycle", [][2]string{
		{string(ops.LifecycleSafeActionStop), "stop"}, {string(ops.LifecycleSafeActionContinue), "continue"},
		{string(ops.LifecycleSafeActionRequery), "requery"}, {string(ops.LifecycleEffectsNone), "none"},
		{string(ops.LifecycleEffectsCommitted), "committed"},
	})
	pinSpellings(t, "actors and prompts", [][2]string{
		{OperatorActor, "operator"}, {confirmAnswerY, "y"}, {confirmAnswerYes, "yes"},
		{stopGuardDecisionBlock, "block"}, {stopGuardKeyBackgroundJobs, "background_tasks"},
		{backgroundJobStatusRunning, "running"}, {integrationStatusComplete, "complete"},
		{narrowChildActionNarrowed, "narrowed"}, {narrowChildActionSkipped, "skipped"},
	})
}

func TestA7HistoryExtraKeysKeepTheirSpellings(t *testing.T) {
	pinSpellings(t, "history extra", [][2]string{
		{historyExtraRepairSourceTask, "repair_source_task"}, {historyExtraOperation, "operation"},
		{historyExtraRepairRequestCleared, "repair_request_cleared"}, {historyExtraAffectedTaskIDs, "affected_task_ids"},
		{historyExtraRepairValidation, "repair_validation"},
	})
}

func TestA7WatchVocabularyKeepsKeyShapes(t *testing.T) {
	pinSpellings(t, "watch", [][2]string{
		{sentinelAssigneePrefix, "$"}, {alertIdentitySeparator, "|"},
		{autoRepairLogActor, "system"}, {autoRepairLogActionSpawned, "auto_repair_agent_spawned"},
		{autoRepairPendingEntryFormat, "%s:%d:%s:%d"}, {autoRepairPendingEntrySeparator, ":"},
	})
	if finalTaskAttempt != 2 || hypothesisExhaustionFailures != 2 || autoRepairPendingEntryFields != 4 {
		t.Errorf("thresholds changed: final attempt %d, exhaustion failures %d, pending fields %d",
			finalTaskAttempt, hypothesisExhaustionFailures, autoRepairPendingEntryFields)
	}
	// The pending-entry format and its parser agree on the field count and separator.
	key := autoRepairPendingKey("coder", 4242, "coder-1", 3)
	entry, ok := strings.CutPrefix(key, autoRepairAgentPoolPendingPrefix)
	if !ok {
		t.Fatalf("pending key %q lacks the prefix %q", key, autoRepairAgentPoolPendingPrefix)
	}
	if got, want := strings.Split(entry, autoRepairPendingEntrySeparator), []string{"coder", "4242", "coder-1", "3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pending entry fields = %v, want %v", got, want)
	}
	fields := strings.Split(entry, autoRepairPendingEntrySeparator)
	if fields[pendingEntryRole] != "coder" || fields[pendingEntryPID] != "4242" || fields[pendingEntryAgentID] != "coder-1" || fields[pendingEntryModelsItem] != "3" {
		t.Errorf("pending entry positions do not index role, PID, agent ID and models item: %v", fields)
	}
	if got, want := fmt.Sprintf(registeredProcessKeyFormat, "coder", 4242), "coder:4242"; got != want {
		t.Errorf("registered process key = %q, want %q", got, want)
	}
	if shortCommitWidth != 7 || deleteAgentTerminateWaitSeconds() != 5 {
		t.Errorf("widths changed: short commit %d, delete-agent wait %ds", shortCommitWidth, deleteAgentTerminateWaitSeconds())
	}
}

func deleteAgentTerminateWaitSeconds() int { return int(deleteAgentTerminateWait.Seconds()) }

func TestA7IntegrationFailureFallbackKeepsDiagnosticShape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".worktrees", "present"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		worktree *string
		root     string
		want     map[string]any
	}{
		{"not recorded", nil, root, map[string]any{"worktree_state": "not_recorded", "worktree_recorded": false}},
		{"unchecked", testStrPtr(".worktrees/present"), "", map[string]any{"worktree_state": "recorded_unchecked", "worktree_recorded": true, "worktree": ".worktrees/present"}},
		{"present", testStrPtr(".worktrees/present"), root, map[string]any{"worktree_state": "present", "worktree_missing": false}},
		{"missing", testStrPtr(".worktrees/absent"), root, map[string]any{"worktree_state": "missing", "worktree_missing": true}},
	}
	for _, tc := range cases {
		task := &models.Task{ID: "t-1", Status: models.TaskStatusIntegrationFailed, Worktree: tc.worktree, FailedBy: []string{"coder-1"}}
		got := fallbackIntegrationFailureDiagnostic(task, tc.root)
		for key, want := range tc.want {
			if got[key] != want {
				t.Errorf("%s: diagnostic[%q] = %v, want %v", tc.name, key, got[key], want)
			}
		}
		for key, want := range map[string]any{"operation": "unknown", "status": "INTEGRATION_FAILED"} {
			if got[key] != want {
				t.Errorf("%s: diagnostic[%q] = %v, want %v", tc.name, key, got[key], want)
			}
		}
		for _, key := range []string{"reason", "recovery_hint", "failed_by"} {
			if _, ok := got[key]; !ok {
				t.Errorf("%s: diagnostic lacks key %q", tc.name, key)
			}
		}
	}
	if got := stringDiagnosticValue(map[string]any{}, "operation"); got != "unknown" {
		t.Errorf("absent diagnostic value = %q, want unknown", got)
	}
	pinSpellings(t, "diagnostic", [][2]string{
		{string(worktreeStateNotRecorded), "not_recorded"}, {string(worktreeStateRecordedUnchecked), "recorded_unchecked"},
		{string(worktreeStatePresent), "present"}, {string(worktreeStateMissing), "missing"}, {string(worktreeStateUnknown), "unknown"},
		{historyExtraDiagnostic, "diagnostic"}, {historyExtraPRURL, "pr_url"},
	})
}

func testStrPtr(s string) *string { return &s }

func TestA7DashboardSectionFormatsKeepRendering(t *testing.T) {
	pinSpellings(t, "dashboard format", [][2]string{
		{string(dashboardFormatKeyValue), "kv"}, {string(dashboardFormatTable), "table"}, {string(dashboardFormatText), "text"},
	})
	out := formatDashboard([]dashboardSection{
		{Title: "KV", Format: "kv", Content: map[string]any{"b": 2, "a": nil}},
		{Title: "Text", Format: "text", Content: "plain"},
		{Title: "Table", Format: "table", Content: nil},
	})
	for _, want := range []string{"KV\n==\na: <none>\nb: 2", "Text\n====\nplain", "Table\n====="} {
		if !strings.Contains(out, want) {
			t.Errorf("dashboard output lacks %q:\n%s", want, out)
		}
	}
}

func TestA7ScipPlanSyntaxKeepsLanguageSpellings(t *testing.T) {
	for language, want := range map[scipsearch.Language]string{
		"go":         "--scip-search-plan go=<module-root>",
		"typescript": "--scip-search-plan typescript=<cwd>,<project-root>",
		"python":     "--scip-search-plan python=<cwd>[,<target-only>]",
		"ruby":       "--scip-search-plan <language>=<values>",
	} {
		if got := pairingScipPlanSyntax(string(language)); got != want {
			t.Errorf("pairingScipPlanSyntax(%q) = %q, want %q", language, got, want)
		}
	}
}

func TestA7AutoRepairEnvAcceptsNoAsFalse(t *testing.T) {
	for value, want := range map[string]bool{"no": false, " No ": false, "NO": false, "false": false, "1": true, "yes": true} {
		if got, _ := parseAutoRepairAgentPoolEnv(value, true); got != want {
			t.Errorf("parseAutoRepairAgentPoolEnv(%q) = %v, want %v", value, got, want)
		}
	}
	if _, warning := parseAutoRepairAgentPoolEnv("yes", true); warning == "" {
		t.Error("an unrecognized boolean must still warn")
	}
}

func TestA7StopGuardAwaitSyntaxKeepsItsTokens(t *testing.T) {
	for command, want := range map[string]bool{
		"liza await-verdict task-1":                         true,
		"/usr/bin/liza.exe await-resubmission":              true,
		"liza -C /repo await-verdict":                       true,
		"liza --project-root /repo -v await-verdict":        true,
		"liza --project-root=/repo --verbose await-verdict": true,
		"liza -C=/repo await-verdict":                       true,
		"liza await-verdict; ls":                            false,
		"liza await-verdict | tee out":                      false,
		"liza -x await-verdict":                             false,
	} {
		if got := isAwaitCommand(command, "liza"); got != want {
			t.Errorf("isAwaitCommand(%q) = %v, want %v", command, got, want)
		}
	}
	if reason, block := stopGuardReason([]byte(`{"background_tasks":null}`), "liza"); block || reason != "" {
		t.Errorf("a null job list must not block, got %q %v", reason, block)
	}
}

func TestA7SpawnAndSembleSyntaxKeepSpellings(t *testing.T) {
	pinSpellings(t, "command syntax", [][2]string{
		{agentFlagCLI, "--cli"}, {agentFlagAgentID, "--agent-id"}, {sembleMessagePrefix, "semble:"}, {sembleMessageLead, "semble: "}, {envBooleanNo, "no"},
	})
	want := []string{brand.BinaryName, "agent", "doer", "--agent-id", "doer-1", "--cli", "claude"}
	if got := buildRespawnArgs("doer", "doer-1", "claude"); !reflect.DeepEqual(got, want) {
		t.Errorf("buildRespawnArgs = %v, want %v", got, want)
	}
}
