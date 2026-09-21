package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/usage"
)

// usageCLIRecord builds one authoritative record for the CLI fixtures.
func usageCLIRecord(taskID, role string, startedAt time.Time, fresh, cacheRead int) usage.Record {
	return usage.Record{
		TaskID: taskID, Role: role, AgentID: role + "-1", SupervisorRunID: "run-1", SessionID: taskID,
		Provider: "provider", StartedAt: startedAt, EndedAt: startedAt.Add(time.Minute),
		FreshInputTokens: fresh, CacheReadTokens: cacheRead, Provenance: usage.ProvenanceTerminalAuthoritative,
	}
}

func usageCLIMergedTask(id string, created, merged time.Time) models.Task {
	return usageE2ETask(id, models.TaskStatusMerged, created, usageE2EEvent(merged, models.TaskEventMerged))
}

// resetUsageReportCLI clears this command's own flags before and after a
// run; the shared reset helper only knows the flag names of older commands.
func resetUsageReportCLI(t *testing.T) {
	t.Helper()
	reset := func() {
		for _, name := range []string{"since", "until", "as-of", "task", "baseline-since", "baseline-until"} {
			resetFlagIfPresent(usageReportCmd, name)
		}
	}
	reset()
	t.Cleanup(reset)
}

func TestUsageCommandWiring(t *testing.T) {
	sprintStart := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

	t.Run("usage report resolves on rootCmd and prints the JSON envelope", func(t *testing.T) {
		resetUsageReportCLI(t)
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			state.Sprint.Timeline.Started = sprintStart
			state.Tasks = []models.Task{usageCLIMergedTask("M1", sprintStart, sprintStart.Add(10*time.Hour))}
		})
		if err := usage.Append(projectRoot, usageCLIRecord("M1", "coder", sprintStart.Add(2*time.Hour), 100, 1000)); err != nil {
			t.Fatalf("append record: %v", err)
		}

		stdout, err := executeRootCommandCapture(t, projectRoot, "usage", "report", "--json")
		if err != nil {
			t.Fatalf("usage report --json: %v\n%s", err, stdout)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != true {
			t.Fatalf("envelope not ok: %s", stdout)
		}
		result, isObject := envelope["result"].(map[string]any)
		if !isObject {
			t.Fatalf("missing result object: %s", stdout)
		}
		for _, key := range []string{"window", "outcomes", "per_completed_task", "provenance", "suppressed_calls"} {
			if _, present := result[key]; !present {
				t.Errorf("result lacks %q: %s", key, stdout)
			}
		}
		outcomes, _ := result["outcomes"].([]any)
		if len(outcomes) != 1 {
			t.Fatalf("outcomes = %v, want one merged/coder row", result["outcomes"])
		}
		row, _ := outcomes[0].(map[string]any)
		if row["outcome"] != "merged" || row["role"] != "coder" {
			t.Fatalf("outcome row = %v", row)
		}
	})

	t.Run("filters and windows reach the command", func(t *testing.T) {
		resetUsageReportCLI(t)
		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			state.Sprint.Timeline.Started = sprintStart
			state.Tasks = []models.Task{usageCLIMergedTask("M1", sprintStart, sprintStart.Add(10*time.Hour))}
		})
		if err := usage.Append(projectRoot, usageCLIRecord("M1", "coder", sprintStart.Add(2*time.Hour), 100, 1000)); err != nil {
			t.Fatalf("append record: %v", err)
		}

		stdout, err := executeRootCommandCapture(t, projectRoot, "usage", "report", "--json",
			"--since", "2026-09-10", "--until", "2026-09-11", "--as-of", "2026-09-11T12:00:00Z",
			"--role", "reviewer", "--task", "M1",
			"--baseline-since", "2026-09-01", "--baseline-until", "2026-09-09")
		if err != nil {
			t.Fatalf("usage report with flags: %v\n%s", err, stdout)
		}
		result, _ := parseEnvelope(t, stdout)["result"].(map[string]any)
		window, _ := result["window"].(map[string]any)
		if window["since"] != "2026-09-10T00:00:00Z" || window["until"] != "2026-09-11T00:00:00Z" || window["as_of"] != "2026-09-11T12:00:00Z" {
			t.Fatalf("window = %v", window)
		}
		if _, present := result["outcomes"]; present {
			t.Fatalf("--role reviewer left coder records in the report: %s", stdout)
		}
		baseline, _ := result["baseline"].(map[string]any)
		baselineWindow, _ := baseline["window"].(map[string]any)
		if baselineWindow["since"] != "2026-09-01T00:00:00Z" || baselineWindow["until"] != "2026-09-09T00:00:00Z" {
			t.Fatalf("baseline window = %v", baselineWindow)
		}
	})

	t.Run("unparsable time is a JSON validation error", func(t *testing.T) {
		resetUsageReportCLI(t)
		projectRoot, _ := setupMutationTestProject(t, nil)
		stdout, err := executeRootCommandCapture(t, projectRoot, "usage", "report", "--json", "--since", "yesterday")
		if err == nil {
			t.Fatalf("unparsable --since accepted:\n%s", stdout)
		}
		envelope := parseEnvelope(t, stdout)
		if envelope["ok"] != false {
			t.Fatalf("envelope ok for an invalid time: %s", stdout)
		}
		errObj, _ := envelope["error"].(map[string]any)
		message, _ := errObj["message"].(string)
		if !strings.Contains(message, "--since") {
			t.Fatalf("error message %q does not name --since", message)
		}
	})

	t.Run("help text carries no raw default-brand literal", func(t *testing.T) {
		resetUsageReportCLI(t)
		originalBinaryName, originalNameTitle := brand.BinaryName, brand.NameTitle
		brand.BinaryName, brand.NameTitle = "acme", "Acme"
		t.Cleanup(func() { brand.BinaryName, brand.NameTitle = originalBinaryName, originalNameTitle })

		// Long texts are built by functions so they can be rendered under a
		// non-default brand here; init() stores the default-brand rendering.
		for _, text := range []string{usageLong(), usageReportLong()} {
			if !strings.Contains(strings.ToLower(text), "acme") {
				t.Errorf("help text is not brand-routed:\n%s", text)
			}
			if strings.Contains(strings.ToLower(text), "liza") {
				t.Errorf("help text carries a raw default-brand literal:\n%s", text)
			}
		}

		resetRootCmdForTest(t)
		var out bytes.Buffer
		rootCmd.SetOut(&out)
		rootCmd.SetErr(&out)
		rootCmd.SetArgs([]string{"usage", "report", "--help"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("usage report --help: %v", err)
		}
		for _, flag := range []string{"--since", "--until", "--as-of", "--role", "--task", "--baseline-since", "--baseline-until", "--format", "--json"} {
			if !strings.Contains(out.String(), flag) {
				t.Errorf("help lacks %s:\n%s", flag, out.String())
			}
		}
	})
}

// --- End-to-end usage attribution fixtures (AC-160-9, AC-160-8) -------------
//
// These fixtures produce records the way the supervisor does — through
// agent.NewUsageEventSink — and read them back through the real CLI, so the
// capture path, the store, the outcome join and the report command are all
// exercised by one test.

// usageE2ETurn drives one provider turn through the real supervisor event sink:
// the started/usage/completed sequence every provider run emits. A zero
// reported usage is the default CLI transport's turn, which pins unknown
// provenance.
func usageE2ETurn(t *testing.T, projectRoot string, cfg agent.UsageSinkConfig, startedAt time.Time, reported agent.LLMAgentUsage) {
	t.Helper()
	cfg.ProjectRoot = projectRoot
	if cfg.SessionID == "" {
		// The default CLI path passes the task id as the session id.
		cfg.SessionID = cfg.TaskID
	}
	sink := agent.NewUsageEventSink(cfg)
	event := func(kind agent.LLMAgentEventKind, at time.Time, payload map[string]any) agent.LLMAgentEvent {
		return agent.LLMAgentEvent{
			Time: at, Kind: kind, BackendName: cfg.Provider, AgentID: cfg.AgentID,
			TaskID: cfg.TaskID, SessionID: cfg.SessionID, Payload: payload,
		}
	}
	ctx := context.Background()
	sink.RecordLLMAgentEvent(ctx, event(agent.LLMAgentEventStarted, startedAt, nil))
	sink.RecordLLMAgentEvent(ctx, event(agent.LLMAgentEventUsage, startedAt, map[string]any{"usage": reported}))
	sink.RecordLLMAgentEvent(ctx, event(agent.LLMAgentEventCompleted, startedAt.Add(time.Minute), map[string]any{"exit_code": 0}))
}

// usageE2EUsage builds one provider usage object. A non-zero input or output
// count is what the sink classifies as terminal-authoritative.
func usageE2EUsage(fresh, cacheRead, output int) agent.LLMAgentUsage {
	return agent.LLMAgentUsage{InputTokens: fresh, CachedReadTokens: cacheRead, OutputTokens: output}
}

// usageE2ETask builds a state-fixture task carrying the durable history the
// report derives its outcome and last useful transition from.
func usageE2ETask(id string, status models.TaskStatus, created time.Time, history ...models.TaskHistoryEntry) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, created)
	task.Lifecycle = &models.TaskLifecycle{Revision: uint64(len(history)) + 1}
	task.History = append([]models.TaskHistoryEntry{{Time: created, Event: models.TaskEventCreated}}, history...)
	return task
}

func usageE2EEvent(at time.Time, event string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{Time: at, Event: event}
}

// usageE2EReport runs usage report --json and returns the result object.
func usageE2EReport(t *testing.T, projectRoot string, args ...string) map[string]any {
	t.Helper()
	stdout, err := executeRootCommandCapture(t, projectRoot, append([]string{"usage", "report", "--json"}, args...)...)
	if err != nil {
		t.Fatalf("usage report --json %v: %v\n%s", args, err, stdout)
	}
	envelope := parseEnvelope(t, stdout)
	if envelope["ok"] != true {
		t.Fatalf("envelope not ok: %s", stdout)
	}
	result, isObject := envelope["result"].(map[string]any)
	if !isObject {
		t.Fatalf("missing result object: %s", stdout)
	}
	return result
}

func usageE2EInt(t *testing.T, object map[string]any, key string) int {
	t.Helper()
	number, isNumber := object[key].(float64)
	if !isNumber {
		t.Fatalf("field %q is %v, want a number", key, object[key])
	}
	return int(number)
}

func usageE2EObjects(t *testing.T, result map[string]any, key string) []map[string]any {
	t.Helper()
	raw, _ := result[key].([]any)
	objects := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		object, isObject := entry.(map[string]any)
		if !isObject {
			t.Fatalf("%s entry is %v, want an object", key, entry)
		}
		objects = append(objects, object)
	}
	return objects
}

// usageE2EOutcomeRow returns the outcome x role row, failing when absent.
func usageE2EOutcomeRow(t *testing.T, result map[string]any, outcome, role string) map[string]any {
	t.Helper()
	for _, row := range usageE2EObjects(t, result, "outcomes") {
		if row["outcome"] == outcome && row["role"] == role {
			return row
		}
	}
	t.Fatalf("no %s/%s outcome row in %v", outcome, role, result["outcomes"])
	return nil
}

// usageE2ETotals reads one row's fresh and cache-read totals.
func usageE2ETotals(t *testing.T, row map[string]any) (fresh, cacheRead int) {
	t.Helper()
	for _, metric := range []string{"fresh_tokens", "cache_read_tokens"} {
		if _, isObject := row[metric].(map[string]any); !isObject {
			t.Fatalf("row lacks %s distribution: %v", metric, row)
		}
	}
	freshDist, _ := row["fresh_tokens"].(map[string]any)
	cacheDist, _ := row["cache_read_tokens"].(map[string]any)
	return usageE2EInt(t, freshDist, "total"), usageE2EInt(t, cacheDist, "total")
}

func TestUsageAttributionEndToEnd(t *testing.T) {
	t.Run("six scenarios from the supervisor sink to the report command", func(t *testing.T) {
		resetUsageReportCLI(t)
		day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
		at := func(d time.Duration) time.Time { return day.Add(d) }

		merged := usageE2ETask("M1", models.TaskStatusMerged, day,
			usageE2EEvent(at(time.Hour), models.TaskEventClaimed),
			usageE2EEvent(at(10*time.Hour), models.TaskEventMerged))
		blocked := usageE2ETask("B1", models.TaskStatusBlocked, day,
			usageE2EEvent(at(time.Hour), models.TaskEventClaimed),
			usageE2EEvent(at(3*time.Hour), models.TaskEventBlocked))
		superseded := usageE2ETask("S1", models.TaskStatusSuperseded, day,
			usageE2EEvent(at(4*time.Hour), models.TaskEventSuperseded))
		superseded.RescopeReason = testhelpers.StringPtr("rescoped by the orchestrator")
		superseded.SupersededBy = nil
		handoff := usageE2ETask("H1", models.TaskStatusMerged, day,
			usageE2EEvent(at(time.Hour), models.TaskEventClaimed),
			usageE2EEvent(at(5*time.Hour+30*time.Minute), models.TaskEventHandoffInitiated),
			usageE2EEvent(at(11*time.Hour), models.TaskEventMerged))
		tail := usageE2ETask("R1", models.TaskStatusReadyForReview, day,
			usageE2EEvent(at(time.Hour), models.TaskEventClaimed),
			usageE2EEvent(at(7*time.Hour), models.TaskEventPreExecutionCheckpoint),
			usageE2EEvent(at(8*time.Hour), models.TaskEventSubmittedForReview))

		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			state.Sprint.Timeline.Started = day
			state.Tasks = []models.Task{merged, blocked, superseded, handoff, tail}
		})

		coder := agent.UsageSinkConfig{AgentID: "coder-1", Role: models.RoleCoder, Provider: "provider", SupervisorRunID: "run-a"}
		reviewer := agent.UsageSinkConfig{AgentID: "code-reviewer-1", Role: models.RoleCodeReviewer, Provider: "provider"}

		withTask := func(cfg agent.UsageSinkConfig, taskID, runID string) agent.UsageSinkConfig {
			cfg.TaskID, cfg.SessionID = taskID, taskID
			if runID != "" {
				cfg.SupervisorRunID = runID
			}
			return cfg
		}

		// Merged task: one authoritative turn, plus one turn whose provider
		// reported no usage at all — the default CLI transport's record.
		usageE2ETurn(t, projectRoot, withTask(coder, "M1", ""), at(2*time.Hour), usageE2EUsage(100, 1000, 10))
		usageE2ETurn(t, projectRoot, withTask(coder, "M1", ""), at(3*time.Hour), agent.LLMAgentUsage{})
		usageE2ETurn(t, projectRoot, withTask(coder, "B1", ""), at(90*time.Minute), usageE2EUsage(40, 200, 4))
		usageE2ETurn(t, projectRoot, withTask(coder, "S1", ""), at(time.Hour), usageE2EUsage(30, 150, 3))
		// Multi-session handoff: two supervisor processes, two run identities,
		// one task — exactly what a handoff or a supervisor restart produces.
		usageE2ETurn(t, projectRoot, withTask(reviewer, "H1", "run-b"), at(5*time.Hour), usageE2EUsage(60, 300, 6))
		usageE2ETurn(t, projectRoot, withTask(reviewer, "H1", "run-c"), at(6*time.Hour), usageE2EUsage(70, 400, 7))
		// Unchanged retry tail: a turn after the task's last useful transition.
		usageE2ETurn(t, projectRoot, withTask(coder, "R1", ""), at(9*time.Hour), usageE2EUsage(25, 500, 5))

		result := usageE2EReport(t, projectRoot,
			"--since", day.Format(time.RFC3339), "--until", at(12*time.Hour).Format(time.RFC3339))

		// Each fixture task appears under its outcome class with its own totals.
		for _, want := range []struct {
			outcome, role    string
			tasks, records   int
			fresh, cacheRead int
		}{
			{"merged", models.RoleCoder, 1, 1, 100, 1000},
			{"merged", models.RoleCodeReviewer, 1, 2, 130, 700},
			{"superseded", models.RoleCoder, 1, 1, 30, 150},
			{"blocked", models.RoleCoder, 1, 1, 40, 200},
			{"active", models.RoleCoder, 1, 1, 25, 500},
		} {
			row := usageE2EOutcomeRow(t, result, want.outcome, want.role)
			fresh, cacheRead := usageE2ETotals(t, row)
			if got := usageE2EInt(t, row, "tasks"); got != want.tasks {
				t.Errorf("%s/%s tasks = %d, want %d", want.outcome, want.role, got, want.tasks)
			}
			if got := usageE2EInt(t, row, "records"); got != want.records {
				t.Errorf("%s/%s records = %d, want %d", want.outcome, want.role, got, want.records)
			}
			if fresh != want.fresh || cacheRead != want.cacheRead {
				t.Errorf("%s/%s fresh/cache-read = %d/%d, want %d/%d",
					want.outcome, want.role, fresh, cacheRead, want.fresh, want.cacheRead)
			}
		}
		if rows := usageE2EObjects(t, result, "outcomes"); len(rows) != 5 {
			t.Errorf("outcome rows = %d, want the five fixture rows: %v", len(rows), result["outcomes"])
		}

		// The handoff's two sessions are counted once each, never twice for the
		// same record: two records, one task, the sum of both turns.
		handoffRow := usageE2EOutcomeRow(t, result, "merged", models.RoleCodeReviewer)
		if got := usageE2EInt(t, handoffRow, "records"); got != 2 {
			t.Errorf("handoff records = %d, want one per session", got)
		}

		// The retry tail is reported under its failure category, with the tokens
		// that the merged rows above already showed it does not contribute to.
		tailRows := usageE2EObjects(t, result, "post_transition")
		if len(tailRows) != 1 {
			t.Fatalf("post_transition rows = %v, want one retry tail", result["post_transition"])
		}
		tailRow := tailRows[0]
		if tailRow["role"] != models.RoleCoder || tailRow["category"] != "retry_tail" {
			t.Errorf("tail row = %v, want coder/retry_tail", tailRow)
		}
		if got := usageE2EInt(t, tailRow, "tokens"); got != 530 {
			t.Errorf("tail tokens = %d, want 25+500+5", got)
		}
		tailTasks := usageE2EObjects(t, tailRow, "tasks")
		if len(tailTasks) != 1 || tailTasks[0]["task_id"] != "R1" {
			t.Errorf("tail tasks = %v, want R1 alone", tailRow["tasks"])
		}
		lastUseful, _ := tailTasks[0]["last_useful"].(map[string]any)
		if lastUseful["event"] != models.TaskEventSubmittedForReview {
			t.Errorf("tail last_useful = %v, want the submitted_for_review transition", tailTasks[0]["last_useful"])
		}

		// The unknown-provenance turn is counted explicitly and excluded from the
		// authoritative totals asserted above.
		provenance, isObject := result["provenance"].(map[string]any)
		if !isObject {
			t.Fatalf("missing provenance block: %v", result)
		}
		for key, want := range map[string]int{
			"terminal_authoritative": 6, "unknown": 1, "partial": 0, "conflicting": 0,
			"duplicates_collapsed": 0, "malformed_lines": 0,
		} {
			if got := usageE2EInt(t, provenance, key); got != want {
				t.Errorf("provenance %s = %d, want %d", key, got, want)
			}
		}

		perTask, isObject := result["per_completed_task"].(map[string]any)
		if !isObject {
			t.Fatalf("missing per_completed_task block: %v", result)
		}
		if got := usageE2EInt(t, perTask, "merged_tasks"); got != 2 {
			t.Errorf("merged_tasks = %d, want M1 and H1", got)
		}
		if got := usageE2EInt(t, perTask, "cache_read_tokens_per_merged_task"); got != 850 {
			t.Errorf("cache_read_tokens_per_merged_task = %d, want (1000+700)/2", got)
		}
	})

	t.Run("before/after delta across two windows separated by a rejection RCA", func(t *testing.T) {
		resetUsageReportCLI(t)
		day := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
		at := func(d time.Duration) time.Time { return day.Add(d) }

		churned := usageE2ETask("C1", models.TaskStatusRejected, day,
			usageE2EEvent(at(time.Hour), models.TaskEventClaimed),
			usageE2EEvent(at(2*time.Hour), models.TaskEventSubmittedForReview),
			usageE2EEvent(at(3*time.Hour), models.TaskEventRejected),
			usageE2EEvent(at(12*time.Hour), models.TaskEventRejectionRCARecorded))

		projectRoot, _ := setupMutationTestProject(t, func(state *models.State) {
			state.Sprint.Timeline.Started = day
			state.Tasks = []models.Task{churned}
		})

		coder := agent.UsageSinkConfig{AgentID: "coder-1", Role: models.RoleCoder, Provider: "provider", TaskID: "C1", SessionID: "C1"}
		before, after := coder, coder
		before.SupervisorRunID, after.SupervisorRunID = "run-before", "run-after"
		// One turn in each window; the gap between them carries no record, so a
		// window's rows can only come from its own records.
		usageE2ETurn(t, projectRoot, before, at(4*time.Hour), usageE2EUsage(200, 2000, 20))
		usageE2ETurn(t, projectRoot, after, at(14*time.Hour), usageE2EUsage(50, 400, 5))

		result := usageE2EReport(t, projectRoot,
			"--since", at(12*time.Hour).Format(time.RFC3339), "--until", at(24*time.Hour).Format(time.RFC3339),
			"--baseline-since", day.Format(time.RFC3339), "--baseline-until", at(6*time.Hour).Format(time.RFC3339))

		primaryFresh, primaryCache := usageE2ETotals(t, usageE2EOutcomeRow(t, result, "active", models.RoleCoder))
		if primaryFresh != 50 || primaryCache != 400 {
			t.Errorf("primary active/coder fresh/cache-read = %d/%d, want the post-RCA turn alone", primaryFresh, primaryCache)
		}
		baseline, isObject := result["baseline"].(map[string]any)
		if !isObject {
			t.Fatalf("missing baseline report: %v", result)
		}
		baselineWindow, _ := baseline["window"].(map[string]any)
		if baselineWindow["since"] != day.Format(time.RFC3339) || baselineWindow["until"] != at(6*time.Hour).Format(time.RFC3339) {
			t.Errorf("baseline window = %v", baselineWindow)
		}
		baselineFresh, baselineCache := usageE2ETotals(t, usageE2EOutcomeRow(t, baseline, "active", models.RoleCoder))
		if baselineFresh != 200 || baselineCache != 2000 {
			t.Errorf("baseline active/coder fresh/cache-read = %d/%d, want the pre-RCA turn alone", baselineFresh, baselineCache)
		}

		// The token dimension issue #161 needs. Each window reads history
		// truncated at its own as_of, so the same tail is an unclassified
		// retry_tail before the durable RCA event and rejection_rca after it:
		// the comparison shows the churn moving between the two categories.
		tailDelta := func(category string) map[string]any {
			t.Helper()
			for _, delta := range usageE2EObjects(t, result, "deltas") {
				if delta["kind"] == "post_transition" && delta["category"] == category && delta["role"] == models.RoleCoder {
					return delta
				}
			}
			t.Fatalf("no coder/%s post-transition delta in %v", category, result["deltas"])
			return nil
		}
		for _, want := range []struct {
			category                  string
			baseline, primary, change int
		}{
			{"rejection_rca", 0, 455, 455},
			{"retry_tail", 2220, 0, -2220},
		} {
			delta := tailDelta(want.category)
			if usageE2EInt(t, delta, "baseline") != want.baseline || usageE2EInt(t, delta, "primary") != want.primary ||
				usageE2EInt(t, delta, "change") != want.change {
				t.Errorf("%s delta = %v, want %d -> %d", want.category, delta, want.baseline, want.primary)
			}
		}

		var freshDelta map[string]any
		for _, delta := range usageE2EObjects(t, result, "deltas") {
			if delta["kind"] == "outcome" && delta["outcome"] == "active" && delta["metric"] == "fresh_tokens" {
				freshDelta = delta
			}
		}
		if freshDelta == nil {
			t.Fatalf("no active fresh_tokens outcome delta in %v", result["deltas"])
		}
		if usageE2EInt(t, freshDelta, "baseline") != 200 || usageE2EInt(t, freshDelta, "primary") != 50 ||
			usageE2EInt(t, freshDelta, "change") != -150 {
			t.Errorf("fresh_tokens delta = %v, want 200 -> 50", freshDelta)
		}
	})
}
