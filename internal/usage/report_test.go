package usage

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

// Report fixture helpers carry an rp prefix (see transitions_test.go).
var rpT0 = time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)

func rpAt(hours int) time.Time { return rpT0.Add(time.Duration(hours) * time.Hour) }

func rpTask(id string, entries ...models.TaskHistoryEntry) models.Task {
	return models.Task{ID: id, Lifecycle: &models.TaskLifecycle{Revision: 7}, History: entries}
}

func rpEv(hours int, event string) models.TaskHistoryEntry {
	return models.TaskHistoryEntry{Time: rpAt(hours), Event: event}
}

// rpRec builds an authoritative record; its id is unique per (task, role, start).
func rpRec(taskID, role string, startHour, fresh, cacheRead, output int) Record {
	started := rpAt(startHour)
	return Record{
		SchemaVersion: SchemaVersion, RecordID: NewRecordID(role, taskID, "s", "p", started),
		TaskID: taskID, Role: role, StartedAt: started, EndedAt: started.Add(time.Minute),
		FreshInputTokens: fresh, CacheReadTokens: cacheRead, OutputTokens: output,
		Provenance: ProvenanceTerminalAuthoritative,
	}
}

func rpAvailable(records []Record) LoadStats {
	return LoadStats{Available: true, Records: len(records)}
}

func rpBuild(t *testing.T, in Input) Report {
	t.Helper()
	report, err := Build(in)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return report
}

func rpRow(t *testing.T, report Report, outcome OutcomeClass, role string) OutcomeRow {
	t.Helper()
	for _, row := range report.Outcomes {
		if row.Outcome == outcome && row.Role == role {
			return row
		}
	}
	t.Fatalf("no outcome row %s/%s in %+v", outcome, role, report.Outcomes)
	return OutcomeRow{}
}

func rpTail(report Report, role string, category FailureCategory) (PostTransitionRow, bool) {
	for _, row := range report.PostTransition {
		if row.Role == role && row.Category == category {
			return row, true
		}
	}
	return PostTransitionRow{}, false
}

func rpHasWarning(report Report, fragment string) bool {
	for _, w := range report.Warnings {
		if strings.Contains(w, fragment) {
			return true
		}
	}
	return false
}

// Promote the generation-3 Build regression to the normal suite, including
// both directions of repair and the last-useful evidence on a nonempty tail.
func TestUsageReportHistoricalDependencyRepair(t *testing.T) {
	for _, removal := range []bool{true, false} {
		name := "addition"
		if removal {
			name = "removal"
		}
		t.Run(name, func(t *testing.T) {
			task := rpTask("T", rpEv(0, models.TaskEventCreated))
			beforeDeps, afterDeps := []string{}, []string{"D"}
			if removal {
				beforeDeps, afterDeps = afterDeps, beforeDeps
			}
			task.DependsOn = beforeDeps
			state := &models.State{Tasks: []models.Task{
				task, rpTask("D", rpEv(0, models.TaskEventCreated), rpEv(2, models.TaskEventMerged)),
			}}
			records := []Record{rpRec("T", "coder", 1, 100, 0, 0), rpRec("T", "coder", 3, 20, 0, 0)}
			in := Input{State: state, Records: records, LoadStats: rpAvailable(records),
				Window: Window{Since: rpAt(0), Until: rpAt(3), AsOf: rpAt(3)}}
			before := rpBuild(t, in)
			wantTokens, wantRecords, wantTask, wantEvent, wantHour := 120, 2, "T", models.TaskEventCreated, 0
			if removal {
				wantTokens, wantRecords, wantTask, wantEvent, wantHour = 20, 1, "D", models.TaskEventMerged, 2
			}
			tail, ok := rpTail(before, "coder", FailureRetryTail)
			if !ok || tail.Tokens != wantTokens || tail.Records != wantRecords || len(tail.Tasks) != 1 {
				t.Fatalf("historical tail = %+v; want %d tokens/%d records", tail, wantTokens, wantRecords)
			}
			if lu := tail.Tasks[0].LastUseful; lu == nil || lu.TaskID != wantTask || lu.Event != wantEvent || !lu.Time.Equal(rpAt(wantHour)) {
				t.Fatalf("historical last useful = %+v; want %s/%s at hour %d", lu, wantTask, wantEvent, wantHour)
			}
			// Match apply-dependency-repair's durable audit shape.
			repair := rpEv(4, models.TaskEventDependenciesRewritten)
			repair.Extra = map[string]any{"expected_dependencies": beforeDeps,
				"desired_dependencies": afterDeps, "canonical_dependencies": afterDeps}
			state.Tasks[0].DependsOn = afterDeps
			state.Tasks[0].History = append(state.Tasks[0].History, repair)
			for _, decoded := range []bool{false, true} {
				if decoded {
					data, err := json.Marshal(state)
					if err != nil {
						t.Fatal(err)
					}
					var loaded models.State
					if err := json.Unmarshal(data, &loaded); err != nil {
						t.Fatal(err)
					}
					in.State = &loaded
				}
				after := rpBuild(t, in)
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("same as_of changed after repair (decoded=%v): before=%+v after=%+v", decoded, before, after)
				}
			}
			// A second repair must not replace the first repair's pre-change snapshot.
			second := rpEv(5, models.TaskEventDependenciesRewritten)
			second.Extra = map[string]any{"expected_dependencies": afterDeps,
				"desired_dependencies": beforeDeps, "canonical_dependencies": beforeDeps}
			state.Tasks[0].History = append(state.Tasks[0].History, second)
			state.Tasks[0].DependsOn = beforeDeps
			in.State = state
			if after := rpBuild(t, in); !reflect.DeepEqual(before, after) {
				t.Fatalf("historical report changed after second repair: before=%+v after=%+v", before, after)
			}
			// At the repair boundary its own useful event supersedes the old merge.
			in.Window.AsOf = rpAt(4)
			if atRepair := rpBuild(t, in); len(atRepair.PostTransition) != 0 {
				t.Fatalf("repair event must be included at as_of: %+v", atRepair.PostTransition)
			}
			// A later dependency merge must use the currently repaired graph.
			state.Tasks[1].History = append(state.Tasks[1].History, rpEv(7, models.TaskEventMerged))
			in.Records = []Record{rpRec("T", "coder", 6, 50, 0, 0), rpRec("T", "coder", 8, 10, 0, 0)}
			in.Window = Window{Since: rpAt(0), Until: rpAt(9)}
			present := rpBuild(t, in)
			presentTail, ok := rpTail(present, "coder", FailureRetryTail)
			wantTokens, wantRecords, wantTask, wantEvent, wantHour = 60, 2, "T", models.TaskEventDependenciesRewritten, 5
			if removal { // The second repair restored D.
				wantTokens, wantRecords, wantTask, wantEvent, wantHour = 10, 1, "D", models.TaskEventMerged, 7
			}
			if !ok || presentTail.Tokens != wantTokens || presentTail.Records != wantRecords || len(presentTail.Tasks) != 1 {
				t.Fatalf("present tail = %+v; want %d tokens/%d records", presentTail, wantTokens, wantRecords)
			}
			if lu := presentTail.Tasks[0].LastUseful; lu == nil || lu.TaskID != wantTask || lu.Event != wantEvent || !lu.Time.Equal(rpAt(wantHour)) {
				t.Fatalf("present last useful = %+v; want %s/%s at hour %d", lu, wantTask, wantEvent, wantHour)
			}
			baseline := in.Window
			baseline.Until, baseline.AsOf = rpAt(3), rpAt(3)
			in.Baseline = &baseline
			in.Records = append(in.Records, records...)
			comparison := rpBuild(t, in)
			// Counter projection/source warnings belong only to the primary report.
			before.SuppressedCalls, before.Warnings = nil, nil
			if !reflect.DeepEqual(comparison.Baseline, &before) {
				t.Fatalf("baseline changed after repairs: %+v", comparison.Baseline)
			}
			if !reflect.DeepEqual(state.Tasks[0].DependsOn, beforeDeps) || len(state.Tasks[0].History) != 3 {
				t.Fatal("Build mutated source dependency graph or history")
			}
		})
	}
}

func TestUsageReportHistoricalDependencyEvidenceUnavailable(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"absent":          nil,
		"null":            {"expected_dependencies": nil},
		"scalar":          {"expected_dependencies": "D"},
		"invalid element": {"expected_dependencies": []any{"D", 7}},
	} {
		t.Run(name, func(t *testing.T) {
			repair := rpEv(4, models.TaskEventDependenciesRewritten)
			repair.Extra = extra
			state := &models.State{Tasks: []models.Task{rpTask("T", rpEv(0, models.TaskEventCreated), repair)}}
			records := []Record{rpRec("T", "coder", 1, 100, 0, 0)}
			in := Input{State: state, Records: records, LoadStats: rpAvailable(records), Window: Window{Until: rpAt(3)}}
			if _, err := Build(in); err == nil || !strings.Contains(err.Error(), "historical dependencies unavailable") || !strings.Contains(err.Error(), "T") {
				t.Fatalf("missing graph evidence must be explicit, got %v", err)
			}
			in.Window = Window{} // Unbounded present reading needs no reconstruction.
			if report := rpBuild(t, in); len(report.PostTransition) != 0 {
				t.Fatalf("current reading ignored repair: %+v", report.PostTransition)
			}
			baseline := Window{Until: rpAt(3)}
			in.Baseline = &baseline
			if _, err := Build(in); err == nil || !strings.HasPrefix(err.Error(), "baseline: usage report: historical dependencies unavailable") {
				t.Fatalf("missing baseline evidence must not produce deltas, got %v", err)
			}
			in.Baseline = nil
			in.Window = Window{Until: rpAt(3)}
			in.Records = []Record{rpRec("unrelated", "coder", 1, 100, 0, 0)}
			if report := rpBuild(t, in); len(report.Outcomes) != 1 || report.Outcomes[0].Outcome != OutcomeUnattributed {
				t.Fatalf("unreferenced task blocked report: %+v", report)
			}
		})
	}
}

func TestUsageReportHistoricalOutputOnlyRewrite(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"output dependencies": {"rewrote_depends_on": false, "rewrote_task_depends_on": true},
		"inherited inputs":    {"rewrote_inherit_inputs": true},
	} {
		t.Run(name, func(t *testing.T) {
			task := rpTask("T", rpEv(0, models.TaskEventCreated))
			task.DependsOn = []string{"D"}
			state := &models.State{Tasks: []models.Task{task, rpTask("D", rpEv(2, models.TaskEventMerged))}}
			records := []Record{rpRec("T", "coder", 1, 100, 0, 0)}
			in := Input{State: state, Records: records, LoadStats: rpAvailable(records), Window: Window{Until: rpAt(3)}}
			before := rpBuild(t, in)
			rewrite := rpEv(4, models.TaskEventDependenciesRewritten)
			rewrite.Extra = extra
			state.Tasks[0].History = append(state.Tasks[0].History, rewrite)
			if after := rpBuild(t, in); !reflect.DeepEqual(before, after) {
				t.Fatalf("output-only rewrite changed dependency attribution: before=%+v after=%+v", before, after)
			}
		})
	}
}

// rpOutcomeFixture: three merged tasks, one blocked and one superseded, across
// coder and reviewer roles. Every record precedes its task's terminal event.
func rpOutcomeFixture() Input {
	merged := func(id string) models.Task {
		return rpTask(id, rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed), rpEv(10, models.TaskEventMerged))
	}
	state := &models.State{Tasks: []models.Task{
		merged("M1"), merged("M2"), merged("M3"),
		rpTask("B1", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed), rpEv(5, models.TaskEventBlocked)),
		rpTask("S1", rpEv(0, models.TaskEventCreated), rpEv(5, models.TaskEventSuperseded)),
	}}
	records := []Record{
		rpRec("M1", "coder", 2, 100, 1000, 10),
		rpRec("M1", "coder", 3, 50, 500, 5),
		rpRec("M2", "coder", 2, 300, 3000, 30),
		rpRec("M3", "coder", 2, 200, 2100, 20),
		rpRec("M1", "reviewer", 4, 20, 200, 2),
		rpRec("M2", "reviewer", 4, 10, 100, 1),
		rpRec("B1", "coder", 2, 40, 400, 4),
		rpRec("S1", "coder", 2, 1280, 700, 7),
	}
	return Input{
		Records: records, LoadStats: rpAvailable(records), State: state,
		Counters: &models.LifecycleOutcomeMetrics{Available: true},
		Window:   Window{Since: rpT0, Until: rpAt(24)},
	}
}

func TestUsageReportOutcomeDistributions(t *testing.T) {
	report := rpBuild(t, rpOutcomeFixture())

	coder := rpRow(t, report, OutcomeMerged, "coder")
	// Per-task coder units: M1 150/1500/15, M2 300/3000/30, M3 200/2100/20.
	if coder.Tasks != 3 || coder.Records != 4 {
		t.Fatalf("merged/coder tasks=%d records=%d; want 3 and 4", coder.Tasks, coder.Records)
	}
	assertDistribution(t, "merged/coder fresh", coder.FreshTokens, Distribution{Total: 650, Median: 200, P95: 300})
	assertDistribution(t, "merged/coder cache-read", coder.CacheReadTokens, Distribution{Total: 6600, Median: 2100, P95: 3000})
	assertDistribution(t, "merged/coder output", coder.OutputTokens, Distribution{Total: 65, Median: 20, P95: 30})

	reviewer := rpRow(t, report, OutcomeMerged, "reviewer")
	if reviewer.Tasks != 2 {
		t.Fatalf("merged/reviewer tasks=%d; want 2", reviewer.Tasks)
	}
	assertDistribution(t, "merged/reviewer fresh", reviewer.FreshTokens, Distribution{Total: 30, Median: 10, P95: 20})
	assertDistribution(t, "merged/reviewer cache-read", reviewer.CacheReadTokens, Distribution{Total: 300, Median: 100, P95: 200})

	blocked := rpRow(t, report, OutcomeBlocked, "coder")
	assertDistribution(t, "blocked/coder fresh", blocked.FreshTokens, Distribution{Total: 40, Median: 40, P95: 40})
	assertDistribution(t, "blocked/coder cache-read", blocked.CacheReadTokens, Distribution{Total: 400, Median: 400, P95: 400})

	superseded := rpRow(t, report, OutcomeSuperseded, "coder")
	assertDistribution(t, "superseded/coder fresh", superseded.FreshTokens, Distribution{Total: 1280, Median: 1280, P95: 1280})
	assertDistribution(t, "superseded/coder cache-read", superseded.CacheReadTokens, Distribution{Total: 700, Median: 700, P95: 700})

	if len(report.Outcomes) != 4 {
		t.Fatalf("outcome rows = %+v; want merged coder/reviewer, blocked coder, superseded coder", report.Outcomes)
	}

	per := report.PerCompletedTask
	if per == nil || per.MergedTasks != 3 {
		t.Fatalf("per_completed_task = %+v; want 3 merged tasks", per)
	}
	// Merged cache-read (6600 + 300) over three tasks; overall 8000 cache-read of 10000 input.
	if per.CacheReadTokensPerMergedTask == nil || *per.CacheReadTokensPerMergedTask != 2300 {
		t.Fatalf("cache_read_tokens_per_merged_task = %v; want 2300", per.CacheReadTokensPerMergedTask)
	}
	if per.CacheHitPercent == nil || *per.CacheHitPercent != 80 {
		t.Fatalf("cache_hit_percent = %v; want 80", per.CacheHitPercent)
	}
}

func assertDistribution(t *testing.T, label string, got, want Distribution) {
	t.Helper()
	if got != want {
		t.Fatalf("%s distribution = %+v; want %+v", label, got, want)
	}
}

func TestUsageReportPostTransitionTail(t *testing.T) {
	state := &models.State{Tasks: []models.Task{
		// R stops changing state after its claim; later turns are an unchanged retry tail.
		rpTask("R", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed)),
		rpTask("Q", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed),
			rpEv(2, models.TaskEventRejected), rpEv(3, models.TaskEventRejectionRCARecorded)),
		rpTask("M", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed), rpEv(5, models.TaskEventMerged)),
	}}
	records := []Record{
		rpRec("R", "code-reviewer", 2, 10, 100, 1),
		rpRec("R", "code-reviewer", 3, 20, 200, 2),
		rpRec("Q", "coder", 4, 5, 50, 5),
		rpRec("M", "coder", 2, 1000, 1000, 10),
	}
	report := rpBuild(t, Input{
		Records: records, LoadStats: rpAvailable(records), State: state,
		Counters: &models.LifecycleOutcomeMetrics{Available: true},
		Window:   Window{Since: rpT0, Until: rpAt(24)},
	})

	retry, ok := rpTail(report, "code-reviewer", FailureRetryTail)
	if !ok || retry.Tokens != 333 || retry.Records != 2 {
		t.Fatalf("retry_tail code-reviewer row = %+v (found=%v); want 333 tokens over 2 records", retry, ok)
	}
	if len(retry.Tasks) != 1 || retry.Tasks[0].TaskID != "R" || retry.Tasks[0].Tokens != 333 {
		t.Fatalf("retry_tail detail = %+v; want task R with 333 tokens", retry.Tasks)
	}
	if lu := retry.Tasks[0].LastUseful; lu == nil || lu.Event != models.TaskEventClaimed || !lu.Time.Equal(rpAt(1)) || lu.Revision != 7 {
		t.Fatalf("retry_tail last useful transition = %+v; want claimed at +1h revision 7", lu)
	}

	rca, ok := rpTail(report, "coder", FailureRejectionRCA)
	if !ok || rca.Tokens != 60 || rca.Records != 1 || len(rca.Tasks) != 1 || rca.Tasks[0].TaskID != "Q" {
		t.Fatalf("rejection_rca coder row = %+v (found=%v); want task Q with 60 tokens", rca, ok)
	}

	if len(report.PostTransition) != 2 {
		t.Fatalf("post_transition rows = %+v; want only retry_tail and rejection_rca", report.PostTransition)
	}
	mergedTail := 0
	for _, row := range report.PostTransition {
		for _, detail := range row.Tasks {
			if detail.TaskID == "M" {
				mergedTail += detail.Tokens
			}
		}
	}
	if mergedTail != 0 {
		t.Fatalf("clean merged task M has a %d-token tail; want 0", mergedTail)
	}
}

func TestUsageReportProvenance(t *testing.T) {
	root := t.TempDir()
	identity := func(runID string, startHour, fresh, cacheRead int, provenance Provenance) Record {
		return Record{
			TaskID: "H", Role: "coder", AgentID: "coder-1", SupervisorRunID: runID, SessionID: "H", Provider: "cli",
			StartedAt: rpAt(startHour), EndedAt: rpAt(startHour).Add(time.Minute),
			FreshInputTokens: fresh, CacheReadTokens: cacheRead, Provenance: provenance,
		}
	}
	appends := []Record{
		identity("run-a", 2, 100, 1000, ProvenanceTerminalAuthoritative),
		// A handoff: a second supervisor process on the same task.
		identity("run-b", 3, 200, 2000, ProvenanceTerminalAuthoritative),
		// Replay of the first session's record: one record, never counted twice.
		identity("run-a", 2, 100, 1000, ProvenanceTerminalAuthoritative),
		identity("run-c", 4, 0, 0, ProvenanceUnknown),
		identity("run-d", 5, 7, 0, ProvenancePartial),
		identity("run-e", 6, 50, 500, ProvenanceTerminalAuthoritative),
		identity("run-e", 6, 60, 600, ProvenanceTerminalAuthoritative),
	}
	for _, r := range appends {
		if err := Append(root, r); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	records, stats, err := Load(root, rpT0, rpAt(24))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	state := &models.State{Tasks: []models.Task{
		rpTask("H", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed),
			rpEv(2, models.TaskEventHandoffInitiated), rpEv(10, models.TaskEventMerged)),
	}}
	report := rpBuild(t, Input{
		Records: records, LoadStats: stats, State: state,
		Counters: &models.LifecycleOutcomeMetrics{Available: true},
		Window:   Window{Since: rpT0, Until: rpAt(24)},
	})

	row := rpRow(t, report, OutcomeMerged, "coder")
	if row.Tasks != 1 || row.Records != 2 {
		t.Fatalf("merged/coder tasks=%d records=%d; want one task over the two handoff sessions", row.Tasks, row.Records)
	}
	if row.FreshTokens.Total != 300 || row.CacheReadTokens.Total != 3000 {
		t.Fatalf("authoritative totals fresh=%d cache-read=%d; want 300 and 3000 (each session once, non-authoritative excluded)",
			row.FreshTokens.Total, row.CacheReadTokens.Total)
	}
	want := ProvenanceCounts{
		TerminalAuthoritative: 2, Partial: 1, Unknown: 1, Conflicting: 1,
		DuplicatesCollapsed: 1, ConflictingRecords: 1,
	}
	if report.Provenance == nil || *report.Provenance != want {
		t.Fatalf("provenance = %+v; want %+v", report.Provenance, want)
	}
	if per := report.PerCompletedTask; per == nil || per.CacheReadTokensPerMergedTask == nil || *per.CacheReadTokensPerMergedTask != 3000 {
		t.Fatalf("per_completed_task = %+v; want 3000 cache-read tokens for the one merged task", per)
	}
}

func TestUsageReportComparison(t *testing.T) {
	baseline := Window{Since: rpT0, Until: rpAt(24)}
	primary := Window{Since: rpAt(48), Until: rpAt(72)}
	state := &models.State{Tasks: []models.Task{
		rpTask("A", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed), rpEv(60, models.TaskEventMerged)),
		rpTask("Bonly", rpEv(0, models.TaskEventCreated), rpEv(1, models.TaskEventClaimed), rpEv(10, models.TaskEventMerged)),
	}}
	// One record set over the union of both windows.
	records := []Record{
		rpRec("A", "coder", 2, 100, 1000, 0),
		rpRec("Bonly", "coder", 5, 999, 0, 0),
		rpRec("A", "coder", 50, 30, 300, 0),
	}
	counters := &models.LifecycleOutcomeMetrics{Available: true, Counts: map[string]map[string]uint64{
		"assess-blocked":   {models.LifecycleNoChange: 3, models.LifecycleCompleted: 9},
		"validate-payload": {models.LifecycleInvalidInput: 2, models.LifecycleCompleted: 4},
		"submit-verdict":   {models.LifecycleCompleted: 5, models.LifecycleAlreadyTransitioned: 1, models.LifecycleStaleCaller: 0},
		"claim-task":       {models.LifecycleRetryable: 8},
	}}
	in := Input{
		Records: records, LoadStats: rpAvailable(records), State: state, Counters: counters,
		Window: primary, Baseline: &baseline,
	}
	report := rpBuild(t, in)

	t.Run("baseline-only records stay out of the primary rows", func(t *testing.T) {
		if len(report.Outcomes) != 1 {
			t.Fatalf("primary outcome rows = %+v; want only merged/coder from task A", report.Outcomes)
		}
		row := rpRow(t, report, OutcomeMerged, "coder")
		if row.Tasks != 1 || row.FreshTokens.Total != 30 {
			t.Fatalf("primary merged/coder = %+v; want task A with 30 fresh tokens", row)
		}
		if len(report.PostTransition) != 0 {
			t.Fatalf("primary post_transition = %+v; want none", report.PostTransition)
		}
	})

	t.Run("baseline carries its own reading and no nested baseline", func(t *testing.T) {
		b := report.Baseline
		if b == nil || b.Baseline != nil || b.Deltas != nil || b.SuppressedCalls != nil {
			t.Fatalf("baseline = %+v; want a flat report", b)
		}
		// At the baseline as_of, A is still active and its record is a retry tail.
		if row := rpRow(t, *b, OutcomeActive, "coder"); row.FreshTokens.Total != 100 {
			t.Fatalf("baseline active/coder = %+v; want 100 fresh tokens", row)
		}
		if row := rpRow(t, *b, OutcomeMerged, "coder"); row.FreshTokens.Total != 999 {
			t.Fatalf("baseline merged/coder = %+v; want 999 fresh tokens", row)
		}
		if tail, ok := rpTail(*b, "coder", FailureRetryTail); !ok || tail.Tokens != 1100 {
			t.Fatalf("baseline retry_tail = %+v (found=%v); want 1100 tokens", tail, ok)
		}
	})

	t.Run("deltas per outcome, role and failure category", func(t *testing.T) {
		want := []Delta{
			{Kind: DeltaKindOutcome, Outcome: OutcomeMerged, Role: "coder", Metric: MetricFreshTokens, Baseline: 999, Primary: 30, Change: -969},
			{Kind: DeltaKindOutcome, Outcome: OutcomeMerged, Role: "coder", Metric: MetricCacheReadTokens, Baseline: 0, Primary: 300, Change: 300},
			{Kind: DeltaKindOutcome, Outcome: OutcomeMerged, Role: "coder", Metric: MetricOutputTokens, Baseline: 0, Primary: 0, Change: 0},
			{Kind: DeltaKindOutcome, Outcome: OutcomeActive, Role: "coder", Metric: MetricFreshTokens, Baseline: 100, Primary: 0, Change: -100},
			{Kind: DeltaKindOutcome, Outcome: OutcomeActive, Role: "coder", Metric: MetricCacheReadTokens, Baseline: 1000, Primary: 0, Change: -1000},
			{Kind: DeltaKindOutcome, Outcome: OutcomeActive, Role: "coder", Metric: MetricOutputTokens, Baseline: 0, Primary: 0, Change: 0},
			{Kind: DeltaKindPostTransition, Role: "coder", Category: FailureRetryTail, Metric: MetricTokens, Baseline: 1100, Primary: 0, Change: -1100},
		}
		if !reflect.DeepEqual(report.Deltas, want) {
			t.Fatalf("deltas =\n%+v\nwant\n%+v", report.Deltas, want)
		}
	})

	t.Run("suppressed calls project the counter file", func(t *testing.T) {
		want := &SuppressedCalls{Available: true, Rows: []SuppressedCallRow{
			{Operation: "assess-blocked", Outcome: models.LifecycleNoChange, Count: 3},
			{Operation: "validate-payload", Outcome: models.LifecycleInvalidInput, Count: 2},
			{Operation: "submit-verdict", Outcome: models.LifecycleAlreadyTransitioned, Count: 1},
			{Operation: "submit-verdict", Outcome: models.LifecycleStaleCaller, Count: 0},
		}}
		if !reflect.DeepEqual(report.SuppressedCalls, want) {
			t.Fatalf("suppressed_calls = %+v; want %+v", report.SuppressedCalls, want)
		}
	})

	t.Run("unavailable counter projection warns instead of reporting zero", func(t *testing.T) {
		for name, counters := range map[string]*models.LifecycleOutcomeMetrics{
			"absent":      nil,
			"unavailable": {Available: false, Warning: "counter file truncated"},
		} {
			degraded := in
			degraded.Counters = counters
			got := rpBuild(t, degraded)
			if got.SuppressedCalls == nil || got.SuppressedCalls.Available || got.SuppressedCalls.Rows != nil {
				t.Fatalf("%s: suppressed_calls = %+v; want unavailable without rows", name, got.SuppressedCalls)
			}
			if !rpHasWarning(got, "lifecycle counters unavailable") {
				t.Fatalf("%s: warnings = %v; want a counter warning", name, got.Warnings)
			}
		}
		degraded := in
		degraded.Counters = &models.LifecycleOutcomeMetrics{Warning: "counter file truncated"}
		if got := rpBuild(t, degraded); !rpHasWarning(got, "counter file truncated") || got.SuppressedCalls.Warning != "counter file truncated" {
			t.Fatalf("counter warning not carried: %+v / %v", got.SuppressedCalls, got.Warnings)
		}
	})

	t.Run("unavailable store warns instead of reporting zero", func(t *testing.T) {
		degraded := in
		degraded.Records = nil
		degraded.LoadStats = LoadStats{Warning: "usage store unavailable: no records directory"}
		got := rpBuild(t, degraded)
		if got.Outcomes != nil || got.PostTransition != nil || got.Provenance != nil || got.PerCompletedTask != nil || got.Deltas != nil {
			t.Fatalf("unavailable store produced figures: %+v", got)
		}
		if got.Baseline == nil || got.Baseline.Outcomes != nil || got.Baseline.Provenance != nil {
			t.Fatalf("unavailable store baseline = %+v; want no figures", got.Baseline)
		}
		if !rpHasWarning(got, "usage store unavailable") {
			t.Fatalf("warnings = %v; want the store warning", got.Warnings)
		}
		encoded, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{`"outcomes"`, `"provenance"`, `"per_completed_task"`, `"post_transition"`} {
			if strings.Contains(string(encoded), key) {
				t.Fatalf("unavailable store renders %s: %s", key, encoded)
			}
		}
	})
}

func TestUsageReportSummarize(t *testing.T) {
	in := rpOutcomeFixture()
	in.LoadStats.MalformedLines = 2
	report := rpBuild(t, in)
	summary := Summarize(report)

	wantOutcomes := []SummaryOutcome{
		{Outcome: OutcomeMerged, Tasks: 3, FreshTokens: 680, CacheReadTokens: 6900, OutputTokens: 68},
		{Outcome: OutcomeSuperseded, Tasks: 1, FreshTokens: 1280, CacheReadTokens: 700, OutputTokens: 7},
		{Outcome: OutcomeBlocked, Tasks: 1, FreshTokens: 40, CacheReadTokens: 400, OutputTokens: 4},
	}
	if !reflect.DeepEqual(report.OutcomeTotals, wantOutcomes) || !reflect.DeepEqual(summary.Outcomes, wantOutcomes) {
		t.Fatalf("outcome totals report=%+v summary=%+v; want %+v", report.OutcomeTotals, summary.Outcomes, wantOutcomes)
	}
	if summary.Window != report.Window {
		t.Fatalf("summary window = %+v; want %+v", summary.Window, report.Window)
	}
	if summary.CacheReadTokensPerMergedTask == nil || *summary.CacheReadTokensPerMergedTask != 2300 ||
		summary.CacheHitPercent == nil || *summary.CacheHitPercent != 80 {
		t.Fatalf("summary ratios = %v / %v; want 2300 and 80", summary.CacheReadTokensPerMergedTask, summary.CacheHitPercent)
	}
	if summary.Provenance == nil || *summary.Provenance != *report.Provenance || summary.Provenance.MalformedLines != 2 {
		t.Fatalf("summary provenance = %+v; want %+v", summary.Provenance, report.Provenance)
	}
	if !reflect.DeepEqual(summary.Warnings, report.Warnings) || !rpHasWarning(report, "malformed") {
		t.Fatalf("summary warnings = %v; want %v including the malformed-line warning", summary.Warnings, report.Warnings)
	}

	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"median"`, `"p95"`, `"post_transition"`, `"deltas"`, `"baseline"`} {
		if strings.Contains(string(encoded), key) {
			t.Fatalf("summary carries %s: %s", key, encoded)
		}
	}
}
