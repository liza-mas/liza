package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"github.com/liza-mas/liza/internal/usage"
)

// urT0 is the fixture sprint start; hours are offsets from it.
var urT0 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

func urAt(hours int) time.Time { return urT0.Add(time.Duration(hours) * time.Hour) }

func urEv(hours int, event string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{Time: urAt(hours), Event: event}
}

func urTask(id string, status models.TaskStatus, entries ...models.TaskHistoryEntry) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, urT0)
	task.Lifecycle = &models.TaskLifecycle{Revision: 3}
	task.History = entries
	return task
}

func urMerged(id string, mergedHour int) models.Task {
	return urTask(id, models.TaskStatusMerged,
		urEv(0, models.TaskEventCreated), urEv(1, models.TaskEventClaimed), urEv(mergedHour, models.TaskEventMerged))
}

// urRecord builds an authoritative record for one provider turn.
func urRecord(taskID, role string, startHour, fresh, cacheRead, output int) usage.Record {
	started := urAt(startHour)
	return usage.Record{
		TaskID: taskID, Role: role, AgentID: role + "-1", SupervisorRunID: "run-" + taskID,
		SessionID: taskID, Provider: "provider", StartedAt: started, EndedAt: started.Add(time.Minute),
		FreshInputTokens: fresh, CacheReadTokens: cacheRead, OutputTokens: output,
		Provenance: usage.ProvenanceTerminalAuthoritative,
	}
}

// urProject writes a fixture project holding only the runtime directory with
// state.yaml: no Makefile, no go.mod, no pipeline config, no git repository.
func urProject(t *testing.T, mutate func(*models.State)) string {
	t.Helper()
	projectRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	statePath := paths.New(projectRoot).StatePath()
	if err := os.MkdirAll(filepath.Dir(statePath), 0755); err != nil {
		t.Fatalf("create runtime dir: %v", err)
	}
	state := testhelpers.CreateValidState()
	state.Sprint.Timeline.Started = urT0
	state.Sprint.Metrics.LifecycleOutcomes = &models.LifecycleOutcomeMetrics{
		Available: true,
		Counts:    map[string]map[string]uint64{"assess-blocked": {models.LifecycleNoChange: 2}},
	}
	if mutate != nil {
		mutate(state)
	}
	testhelpers.WriteInitialState(t, statePath, state)
	return projectRoot
}

func urAppend(t *testing.T, projectRoot string, records ...usage.Record) {
	t.Helper()
	for _, r := range records {
		if err := usage.Append(projectRoot, r); err != nil {
			t.Fatalf("append record: %v", err)
		}
	}
}

func urReport(t *testing.T, opts UsageReportOptions) usage.Report {
	t.Helper()
	opts.Internal = true
	result, err := UsageReportCommand(opts)
	if err != nil {
		t.Fatalf("UsageReportCommand: %v", err)
	}
	report, ok := result.(usage.Report)
	if !ok {
		t.Fatalf("internal result is %T, want usage.Report", result)
	}
	return report
}

func urRow(t *testing.T, rows []usage.OutcomeRow, outcome usage.OutcomeClass, role string) usage.OutcomeRow {
	t.Helper()
	for _, row := range rows {
		if row.Outcome == outcome && row.Role == role {
			return row
		}
	}
	t.Fatalf("no %s/%s row in %+v", outcome, role, rows)
	return usage.OutcomeRow{}
}

func TestUsageReportCommandJSONFields(t *testing.T) {
	// GIVEN a fixture project with only state.yaml and the usage store
	projectRoot := urProject(t, func(state *models.State) {
		state.Tasks = []models.Task{urMerged("M1", 10)}
	})
	urAppend(t, projectRoot,
		urRecord("M1", "coder", 2, 100, 1000, 10),
		urRecord("M1", "coder", 3, 50, 500, 5))

	// WHEN the report is rendered as JSON
	result, err := UsageReportCommand(UsageReportOptions{ProjectRoot: projectRoot, Format: "json"})
	if err != nil {
		t.Fatalf("UsageReportCommand: %v", err)
	}
	rendered, ok := result.(string)
	if !ok {
		t.Fatalf("formatted result is %T, want string", result)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rendered), &top); err != nil {
		t.Fatalf("report is not a JSON object: %v\n%s", err, rendered)
	}

	// THEN the documented top-level keys are present
	for _, key := range []string{"window", "outcomes", "per_completed_task", "provenance", "suppressed_calls"} {
		if _, present := top[key]; !present {
			t.Errorf("missing top-level key %q in %s", key, rendered)
		}
	}
	for _, key := range []string{"baseline", "deltas", "warnings"} {
		if _, present := top[key]; present {
			t.Errorf("unexpected top-level key %q without a baseline or warning: %s", key, rendered)
		}
	}
	var outcomes []map[string]any
	if err := json.Unmarshal(top["outcomes"], &outcomes); err != nil {
		t.Fatalf("outcomes: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %v, want one merged/coder row", outcomes)
	}
	row := outcomes[0]
	if row["outcome"] != "merged" || row["role"] != "coder" || row["tasks"] != float64(1) {
		t.Fatalf("outcome row = %v, want merged/coder with 1 task", row)
	}
	fresh, _ := row["fresh_tokens"].(map[string]any)
	if fresh["total"] != float64(150) || fresh["median"] != float64(150) || fresh["p95"] != float64(150) {
		t.Fatalf("fresh_tokens = %v, want total/median/p95 150", fresh)
	}
	var window map[string]string
	if err := json.Unmarshal(top["window"], &window); err != nil {
		t.Fatalf("window: %v", err)
	}
	for _, key := range []string{"since", "until", "as_of"} {
		if window[key] == "" {
			t.Errorf("window.%s missing in %s", key, top["window"])
		}
	}

	// AND no project build file existed or was created (G1.1)
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		t.Fatalf("read project root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != paths.ProjectDirName() {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("project root holds %v, want only %s", names, paths.ProjectDirName())
	}
}

func TestUsageReportCommandWindow(t *testing.T) {
	ended := urAt(30)
	projectRoot := urProject(t, func(state *models.State) {
		state.Sprint.Timeline.Ended = &ended
		state.Tasks = []models.Task{urMerged("M1", 10), urMerged("M2", 40)}
	})
	urAppend(t, projectRoot,
		urRecord("M1", "coder", 2, 100, 1000, 10),
		urRecord("M2", "coder", 35, 7, 70, 1))

	t.Run("default window is the sprint timeline", func(t *testing.T) {
		report := urReport(t, UsageReportOptions{ProjectRoot: projectRoot})
		if !report.Window.Since.Equal(urT0) || !report.Window.Until.Equal(ended) || !report.Window.AsOf.Equal(ended) {
			t.Fatalf("window = %+v, want sprint %s..%s", report.Window, urT0, ended)
		}
		row := urRow(t, report.Outcomes, usage.OutcomeMerged, "coder")
		if row.Tasks != 1 || row.FreshTokens.Total != 100 {
			t.Fatalf("merged/coder = %+v, want only M1 inside the sprint window", row)
		}
	})

	t.Run("running sprint reads up to now", func(t *testing.T) {
		running := urProject(t, func(state *models.State) {
			state.Tasks = []models.Task{urMerged("M2", 40)}
		})
		urAppend(t, running, urRecord("M2", "coder", 35, 7, 70, 1))
		before := time.Now().UTC()
		report := urReport(t, UsageReportOptions{ProjectRoot: running})
		if !report.Window.Since.Equal(urT0) || report.Window.Until.Before(before) || report.Window.Until.After(time.Now().UTC()) {
			t.Fatalf("window = %+v, want %s..now", report.Window, urT0)
		}
		if row := urRow(t, report.Outcomes, usage.OutcomeMerged, "coder"); row.FreshTokens.Total != 7 {
			t.Fatalf("merged/coder = %+v, want M2", row)
		}
	})

	t.Run("since, until and as-of override the default", func(t *testing.T) {
		report := urReport(t, UsageReportOptions{
			ProjectRoot: projectRoot,
			Since:       urAt(34).Format(time.RFC3339),
			Until:       urAt(36).Format(time.RFC3339),
			AsOf:        urAt(38).Format(time.RFC3339),
		})
		if !report.Window.Since.Equal(urAt(34)) || !report.Window.Until.Equal(urAt(36)) || !report.Window.AsOf.Equal(urAt(38)) {
			t.Fatalf("window = %+v, want 34h..36h as of 38h", report.Window)
		}
		// M2 merged at 40h: as of 38h it is still active.
		row := urRow(t, report.Outcomes, usage.OutcomeActive, "coder")
		if row.Tasks != 1 || row.FreshTokens.Total != 7 {
			t.Fatalf("active/coder = %+v, want M2 with 7 fresh tokens", row)
		}
		if len(report.Outcomes) != 1 {
			t.Fatalf("outcomes = %+v, want M1 excluded by --since", report.Outcomes)
		}
	})

	t.Run("date-only bounds are UTC midnight", func(t *testing.T) {
		report := urReport(t, UsageReportOptions{ProjectRoot: projectRoot, Since: "2026-09-11", Until: "2026-09-12"})
		if !report.Window.Since.Equal(urAt(24)) || !report.Window.Until.Equal(urAt(48)) {
			t.Fatalf("window = %+v, want 2026-09-11..2026-09-12 UTC", report.Window)
		}
	})

	t.Run("unparsable time is rejected before any read", func(t *testing.T) {
		for _, opts := range []UsageReportOptions{
			{Since: "yesterday"}, {Until: "2026-13-01"}, {AsOf: "12:00"},
			{BaselineSince: "last week"}, {BaselineUntil: "soon"},
		} {
			opts.ProjectRoot = filepath.Join(t.TempDir(), "absent")
			if err := opts.Validate(); err == nil {
				t.Errorf("Validate(%+v) accepted an unparsable time", opts)
			}
			_, err := UsageReportCommand(opts)
			if err == nil || strings.Contains(err.Error(), "state") {
				t.Errorf("UsageReportCommand(%+v) err = %v, want a time validation error before reading state", opts, err)
			}
		}
	})

	t.Run("since after until is rejected", func(t *testing.T) {
		opts := UsageReportOptions{ProjectRoot: filepath.Join(t.TempDir(), "absent"),
			Since: urAt(2).Format(time.RFC3339), Until: urAt(1).Format(time.RFC3339)}
		if err := opts.Validate(); err == nil {
			t.Fatal("Validate accepted since after until")
		}
	})

	t.Run("invalid format is rejected", func(t *testing.T) {
		opts := UsageReportOptions{ProjectRoot: projectRoot, Format: "csv"}
		if err := opts.Validate(); err == nil {
			t.Fatal("Validate accepted format csv")
		}
	})
}

func TestUsageReportCommandBaselineWindow(t *testing.T) {
	// GIVEN a baseline window earlier than the primary one, one record inside
	// the baseline only, and one baseline record appended twice
	projectRoot := urProject(t, func(state *models.State) {
		state.Tasks = []models.Task{urMerged("A", 60), urMerged("Bonly", 10), urMerged("R", 60)}
	})
	duplicate := urRecord("A", "coder", 2, 100, 1000, 0)
	urAppend(t, projectRoot,
		duplicate, duplicate,
		urRecord("Bonly", "coder", 5, 999, 0, 0),
		urRecord("A", "coder", 50, 30, 300, 0),
		urRecord("R", "reviewer", 51, 11, 110, 0))

	report := urReport(t, UsageReportOptions{
		ProjectRoot: projectRoot,
		Since:       urAt(48).Format(time.RFC3339), Until: urAt(72).Format(time.RFC3339),
		BaselineSince: urT0.Format(time.RFC3339), BaselineUntil: urAt(24).Format(time.RFC3339),
	})

	// THEN baseline-only records are under baseline and deltas, not the primary rows
	if len(report.Outcomes) != 2 {
		t.Fatalf("primary outcomes = %+v, want only A and R", report.Outcomes)
	}
	if row := urRow(t, report.Outcomes, usage.OutcomeMerged, "coder"); row.Tasks != 1 || row.FreshTokens.Total != 30 {
		t.Fatalf("primary merged/coder = %+v, want task A with 30 fresh tokens", row)
	}
	if report.Baseline == nil {
		t.Fatal("baseline report missing")
	}
	if !report.Baseline.Window.Since.Equal(urT0) || !report.Baseline.Window.Until.Equal(urAt(24)) {
		t.Fatalf("baseline window = %+v", report.Baseline.Window)
	}
	// Read as of the baseline's end, A (merged at 60h) is still active.
	if row := urRow(t, report.Baseline.Outcomes, usage.OutcomeMerged, "coder"); row.Tasks != 1 || row.FreshTokens.Total != 999 {
		t.Fatalf("baseline merged/coder = %+v, want Bonly with 999 fresh tokens", row)
	}
	if row := urRow(t, report.Baseline.Outcomes, usage.OutcomeActive, "coder"); row.Tasks != 1 || row.FreshTokens.Total != 100 {
		t.Fatalf("baseline active/coder = %+v, want A with 100 fresh tokens", row)
	}
	if report.Baseline.Baseline != nil || len(report.Baseline.Deltas) != 0 {
		t.Fatalf("baseline report nests a baseline: %+v", report.Baseline)
	}
	var freshDelta *usage.Delta
	for i := range report.Deltas {
		d := &report.Deltas[i]
		if d.Kind == "outcome" && d.Outcome == usage.OutcomeMerged && d.Role == "coder" && d.Metric == "fresh_tokens" {
			freshDelta = d
		}
	}
	if freshDelta == nil || freshDelta.Baseline != 999 || freshDelta.Primary != 30 || freshDelta.Change != -969 {
		t.Fatalf("merged/coder fresh delta = %+v, want 999 -> 30", freshDelta)
	}

	// AND both windows come from one Load over the union: the duplicate that
	// lives in the baseline day file is collapsed in the primary provenance too
	if report.Provenance == nil || report.Provenance.DuplicatesCollapsed != 1 {
		t.Fatalf("primary provenance = %+v, want duplicates_collapsed 1 from the union load", report.Provenance)
	}
	if report.Baseline.Provenance == nil || report.Baseline.Provenance.DuplicatesCollapsed != 1 {
		t.Fatalf("baseline provenance = %+v, want duplicates_collapsed 1", report.Baseline.Provenance)
	}

	t.Run("role and task filters apply to both windows", func(t *testing.T) {
		filtered := urReport(t, UsageReportOptions{
			ProjectRoot: projectRoot, Role: "coder", TaskID: "A",
			Since: urAt(48).Format(time.RFC3339), Until: urAt(72).Format(time.RFC3339),
			BaselineSince: urT0.Format(time.RFC3339), BaselineUntil: urAt(24).Format(time.RFC3339),
		})
		if len(filtered.Outcomes) != 1 || filtered.Outcomes[0].Role != "coder" {
			t.Fatalf("primary outcomes = %+v, want the reviewer row filtered out", filtered.Outcomes)
		}
		if len(filtered.Baseline.Outcomes) != 1 {
			t.Fatalf("baseline outcomes = %+v, want Bonly filtered out", filtered.Baseline.Outcomes)
		}
		if row := urRow(t, filtered.Baseline.Outcomes, usage.OutcomeActive, "coder"); row.Tasks != 1 || row.FreshTokens.Total != 100 {
			t.Fatalf("baseline active/coder = %+v, want only task A", row)
		}
	})
}

func TestUsageReportCommandUnavailable(t *testing.T) {
	// GIVEN a project whose usage store was never written
	projectRoot := urProject(t, func(state *models.State) {
		state.Tasks = []models.Task{urMerged("M1", 10)}
	})

	// WHEN the report is built
	report := urReport(t, UsageReportOptions{ProjectRoot: projectRoot})

	// THEN it succeeds with a warning and no zero rows
	if len(report.Warnings) == 0 || !strings.Contains(strings.Join(report.Warnings, "\n"), "usage store unavailable") {
		t.Fatalf("warnings = %v, want the store unavailability", report.Warnings)
	}
	if report.Outcomes != nil || report.Provenance != nil || report.PerCompletedTask != nil || report.PostTransition != nil {
		t.Fatalf("unavailable store rendered figures: %+v", report)
	}

	result, err := UsageReportCommand(UsageReportOptions{ProjectRoot: projectRoot, Format: "json"})
	if err != nil {
		t.Fatalf("json report: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal([]byte(result.(string)), &top); err != nil {
		t.Fatalf("parse json report: %v", err)
	}
	if _, present := top["outcomes"]; present {
		t.Fatalf("json report carries outcomes without a store: %v", top)
	}
	if _, present := top["warnings"]; !present {
		t.Fatalf("json report carries no warnings: %v", top)
	}
}
