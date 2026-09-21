package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// The names this extension adds to the metrics matrix. Kept separate from the
// pre-existing rows so a register regression is visible as a count mismatch.
var (
	lifecycleMetricsAddedOperations = []string{
		"validate-payload", "replace-task", "record-rejection-rca", "resume-rejection-rca",
	}
	lifecycleMetricsAddedOutcomes = []string{models.LifecycleNoChange, models.LifecycleConflict}
)

func lifecycleMetricsSprint() LifecycleSprintIdentity {
	return LifecycleSprintIdentity{ID: "sprint-1", Number: 1, Started: time.Date(2026, 9, 12, 1, 0, 0, 0, time.UTC)}
}

func TestLifecycleMetricsConcurrentAndRestart(t *testing.T) {
	t.Parallel()
	root, sprint := t.TempDir(), lifecycleMetricsSprint()
	const workers = 12
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for _, outcome := range []string{"COMPLETED", "ALREADY_COMPLETED", "INVALID_INPUT"} {
				if err := RecordLifecycleOutcome(root, sprint, "submit-for-review", outcome); err != nil {
					t.Errorf("record: %v", err)
				}
			}
		})
	}
	wg.Wait()
	first := ReadLifecycleOutcomes(root, sprint)
	if !first.Available || first.ObservedSince == nil || first.LastUpdated == nil {
		t.Fatalf("unavailable counters: %+v", first)
	}
	for _, outcome := range []string{"COMPLETED", "ALREADY_COMPLETED", "INVALID_INPUT"} {
		if got := first.Counts["submit-for-review"][outcome]; got != workers {
			t.Errorf("%s count=%d, want %d", outcome, got, workers)
		}
	}
	if first.Counts["submit-verdict"]["COMPLETED"] != 0 {
		t.Fatal("unrelated operation changed")
	}
	// Reopen via fresh calls: all state lives on disk, not in a recorder instance.
	if err := RecordLifecycleOutcome(root, sprint, "submit-for-review", "ALREADY_COMPLETED"); err != nil {
		t.Fatal(err)
	}
	restarted := ReadLifecycleOutcomes(root, sprint)
	if restarted.Counts["submit-for-review"]["ALREADY_COMPLETED"] != workers+1 || !restarted.ObservedSince.Equal(*first.ObservedSince) {
		t.Fatalf("restart lost counts or observation window: %+v", restarted)
	}
	info, err := os.Stat(lifecycleMetricsPath(root, sprint))
	if err != nil || info.Size() > lifecycleMetricsMaxBytes {
		t.Fatalf("unbounded counter file: %v %v", info, err)
	}
}

func TestLifecycleMetricsSprintIsolationAndLogRotation(t *testing.T) {
	t.Parallel()
	root, old := t.TempDir(), lifecycleMetricsSprint()
	current := old
	current.Number++
	current.Started = current.Started.Add(time.Hour)
	for _, event := range []struct {
		sprint  LifecycleSprintIdentity
		outcome string
	}{{old, "COMPLETED"}, {current, "INVALID_INPUT"}, {old, "ALREADY_COMPLETED"}} {
		if err := RecordLifecycleOutcome(root, event.sprint, "submit-for-review", event.outcome); err != nil {
			t.Fatal(err)
		}
	}
	// Rotating/truncating the unrelated activity log cannot affect counters.
	if err := os.WriteFile(paths.New(root).LogPath(), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(paths.New(root).LogPath(), paths.New(root).LogPath()+".old"); err != nil {
		t.Fatal(err)
	}
	oldCounts, currentCounts := ReadLifecycleOutcomes(root, old), ReadLifecycleOutcomes(root, current)
	if oldCounts.Counts["submit-for-review"]["COMPLETED"] != 1 || oldCounts.Counts["submit-for-review"]["ALREADY_COMPLETED"] != 1 || oldCounts.Counts["submit-for-review"]["INVALID_INPUT"] != 0 {
		t.Fatalf("late old-sprint outcome misattributed: %+v", oldCounts)
	}
	if currentCounts.Counts["submit-for-review"]["INVALID_INPUT"] != 1 || currentCounts.Counts["submit-for-review"]["COMPLETED"] != 0 || currentCounts.Counts["submit-for-review"]["ALREADY_COMPLETED"] != 0 {
		t.Fatalf("old sprint leaked into current counters: %+v", currentCounts)
	}
}

func TestLifecycleMetricsMissingAndDeletedWindow(t *testing.T) {
	t.Parallel()
	root, sprint := t.TempDir(), lifecycleMetricsSprint()
	missing := ReadLifecycleOutcomes(root, sprint)
	if missing.Available || missing.Counts != nil || missing.ObservedSince != nil || missing.Warning == "" {
		t.Fatalf("missing must be unavailable, not zero: %+v", missing)
	}
	if err := RecordLifecycleOutcome(root, sprint, "claim-task", "COMPLETED"); err != nil {
		t.Fatal(err)
	}
	first := ReadLifecycleOutcomes(root, sprint)
	if err := os.Remove(lifecycleMetricsPath(root, sprint)); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC()
	if err := RecordLifecycleOutcome(root, sprint, "claim-task", "INVALID_INPUT"); err != nil {
		t.Fatal(err)
	}
	fresh := ReadLifecycleOutcomes(root, sprint)
	if !fresh.Available || fresh.ObservedSince.Before(before) || fresh.ObservedSince.Before(*first.ObservedSince) || fresh.Counts["claim-task"]["COMPLETED"] != 0 || fresh.Counts["claim-task"]["INVALID_INPUT"] != 1 {
		t.Fatalf("deleted store must start an explicit new window: %+v", fresh)
	}
}

func TestLifecycleMetricsCorruptionNeverResets(t *testing.T) {
	t.Parallel()
	sprint := lifecycleMetricsSprint()
	valid := newLifecycleCounters(sprint, time.Now().UTC())
	valid.Counts["claim-task"]["COMPLETED"] = 4
	mismatched := valid
	mismatched.Sprint.Number++
	wrongIdentity, err := json.Marshal(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"empty": {}, "truncated": []byte(`{"version":1`), "malformed": []byte(`not json`),
		"wrong-sprint": wrongIdentity, "oversized": make([]byte, lifecycleMetricsMaxBytes+1),
		"incomplete-matrix": []byte(`{"version":1,"counts":{}}`),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(paths.New(root).LifecycleMetricsDir(), 0700); err != nil {
				t.Fatal(err)
			}
			filename := lifecycleMetricsPath(root, sprint)
			if err := os.WriteFile(filename, data, 0600); err != nil {
				t.Fatal(err)
			}
			got := ReadLifecycleOutcomes(root, sprint)
			if got.Available || got.Counts != nil || got.Warning == "" {
				t.Fatalf("corrupt store presented as counters: %+v", got)
			}
			if err := RecordLifecycleOutcome(root, sprint, "claim-task", "COMPLETED"); err == nil {
				t.Fatal("corrupt store unexpectedly accepted recording")
			}
			after, err := os.ReadFile(filename)
			if err != nil || string(after) != string(data) {
				t.Fatalf("damaged store overwritten: %v", err)
			}
		})
	}
}

func TestLifecycleMetricsUnknownKeysDoNotCreateStore(t *testing.T) {
	t.Parallel()
	root, sprint := t.TempDir(), lifecycleMetricsSprint()
	for _, pair := range [][2]string{{"task-arbitrary", "COMPLETED"}, {"claim-task", "arbitrary"}} {
		if err := RecordLifecycleOutcome(root, sprint, pair[0], pair[1]); err == nil {
			t.Fatal("unknown matrix key accepted")
		}
	}
	if err := RecordLifecycleOutcome(root, LifecycleSprintIdentity{}, "claim-task", "COMPLETED"); err == nil {
		t.Fatal("unknown sprint accepted")
	}
	if _, err := os.Stat(paths.New(root).LifecycleMetricsDir()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid recording created storage: %v", err)
	}
}

func TestLifecycleMetricsUpdatePersistsCurrentSprintSnapshot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)
	sprint := CaptureLifecycleSprint(state.Sprint)
	if err := RecordLifecycleOutcome(root, sprint, "submit-verdict", "ALREADY_COMPLETED"); err != nil {
		t.Fatal(err)
	}
	metrics, err := UpdateSprintMetrics(root)
	if err != nil {
		t.Fatal(err)
	}
	stored := readStateForTest(t, stateFile)
	if metrics.LifecycleOutcomes == nil || !metrics.LifecycleOutcomes.Available || metrics.LifecycleOutcomes.Counts["submit-verdict"]["ALREADY_COMPLETED"] != 1 {
		t.Fatalf("counter snapshot absent: %+v", metrics)
	}
	if !reflect.DeepEqual(stored.Sprint.Metrics.LifecycleOutcomes, metrics.LifecycleOutcomes) {
		t.Fatal("state persisted different telemetry snapshot")
	}
	if metrics.ReviewVerdictCount != 0 {
		t.Fatal("benign retry inflated domain verdict metrics")
	}
}

func TestLifecycleMetricsSprintRaceLeavesStateUnchanged(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	old := CaptureLifecycleSprint(state.Sprint)
	state.Sprint.Number++
	testhelpers.WriteInitialState(t, stateFile, state)
	before, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	err = db.For(stateFile).Modify(func(current *models.State) error {
		return applyLifecycleSprintMetrics(current, old, models.SprintMetrics{TasksDone: 123})
	})
	var changed *LifecycleSprintChangedError
	if !errors.As(err, &changed) || changed.SafeDetails()["safe_action"] != "requery" {
		t.Fatalf("sprint race must requery: %v", err)
	}
	after, readErr := os.ReadFile(stateFile)
	if readErr != nil || string(after) != string(before) {
		t.Fatalf("stale metrics modified new sprint: %v", readErr)
	}
}

func TestLifecycleInvocationCapturesSprintAndRecordsOnlyOnce(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)
	old := CaptureLifecycleSprint(state.Sprint)
	invocation := NewLifecycleInvocation(root)
	if err := db.For(stateFile).Modify(func(s *models.State) error { s.Sprint.Number++; return nil }); err != nil {
		t.Fatal(err)
	}
	outcome := NewLifecycleOutcome("claim-task", nil, models.LifecycleCompleted, "continue", "committed")
	for range 2 {
		if err := invocation.Finish("claim-task", outcome, nil); err != nil {
			t.Fatal(err)
		}
	}
	observed := ReadLifecycleOutcomes(root, old)
	if !observed.Available || observed.Counts["claim-task"][models.LifecycleCompleted] != 1 {
		t.Fatalf("outer invocation counted incorrectly: %+v", observed)
	}
	current := CaptureLifecycleSprint(readStateForTest(t, stateFile).Sprint)
	if ReadLifecycleOutcomes(root, current).Available {
		t.Fatal("late result counted against new sprint")
	}
}

func TestLifecycleInvocationTelemetryFailurePreservesOutcome(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)
	sprint := CaptureLifecycleSprint(state.Sprint)
	if err := os.MkdirAll(paths.New(root).LifecycleMetricsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lifecycleMetricsPath(root, sprint), []byte("truncated"), 0600); err != nil {
		t.Fatal(err)
	}
	outcome := NewLifecycleOutcome("claim-task", nil, models.LifecycleCompleted, "continue", "committed")
	var operationErr error
	var warnings []string
	NewLifecycleInvocation(root).FinishResult("claim-task", outcome, &operationErr, &warnings)
	if operationErr != nil || len(warnings) != 1 || outcome.Outcome != models.LifecycleCompleted {
		t.Fatalf("telemetry changed committed outcome: %v %v %+v", operationErr, warnings, outcome)
	}
	operationErr = WrapLifecycleError("claim-task", nil, errors.New("denied"), models.LifecycleForbidden, "stop", "none")
	NewLifecycleInvocation(root).FinishResult("claim-task", models.LifecycleOutcome{}, &operationErr, nil)
	var typed *LifecycleError
	if !errors.As(operationErr, &typed) || typed.Outcome.Outcome != models.LifecycleForbidden || typed.Outcome.SafeAction != "stop" {
		t.Fatalf("telemetry changed failed outcome: %v", operationErr)
	}
}

// lifecycleLegacyCounters reproduces a counter file written before the matrix
// grew: the operation rows and outcome columns added later are absent.
func lifecycleLegacyCounters(t *testing.T, sprint LifecycleSprintIdentity) lifecycleCounterFile {
	t.Helper()
	counters := newLifecycleCounters(sprint, time.Now().UTC())
	for _, operation := range lifecycleMetricsAddedOperations {
		delete(counters.Counts, operation)
	}
	for _, row := range counters.Counts {
		for _, outcome := range lifecycleMetricsAddedOutcomes {
			delete(row, outcome)
		}
	}
	if len(counters.Counts) == 0 {
		t.Fatal("legacy fixture removed every operation row")
	}
	return counters
}

func writeLifecycleCounters(t *testing.T, root string, sprint LifecycleSprintIdentity, counters lifecycleCounterFile) (string, []byte) {
	t.Helper()
	if err := os.MkdirAll(paths.New(root).LifecycleMetricsDir(), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(counters)
	if err != nil {
		t.Fatal(err)
	}
	filename := lifecycleMetricsPath(root, sprint)
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return filename, data
}

func readLifecycleCountersFixture(t *testing.T, filename string) lifecycleCounterFile {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	var counters lifecycleCounterFile
	if err := json.Unmarshal(data, &counters); err != nil {
		t.Fatal(err)
	}
	return counters
}

func TestLifecycleMetricsToleratesMatrixGrowth(t *testing.T) {
	t.Parallel()
	sprint := lifecycleMetricsSprint()

	t.Run("subset matrix reads available and is materialized on the next record", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		legacy := lifecycleLegacyCounters(t, sprint)
		legacy.Counts["claim-task"]["COMPLETED"] = 4
		filename, _ := writeLifecycleCounters(t, root, sprint, legacy)

		got := ReadLifecycleOutcomes(root, sprint)
		if !got.Available || got.Warning != "" {
			t.Fatalf("pre-extension counters unavailable: %+v", got)
		}
		if got.Counts["claim-task"]["COMPLETED"] != 4 {
			t.Fatalf("existing count lost: %+v", got.Counts["claim-task"])
		}
		for _, operation := range lifecycleMetricOperations {
			for _, outcome := range lifecycleMetricOutcomes {
				count, exists := got.Counts[operation][outcome]
				if !exists {
					t.Fatalf("cell %s/%s missing from projection", operation, outcome)
				}
				var want uint64
				if operation == "claim-task" && outcome == "COMPLETED" {
					want = 4
				}
				if count != want {
					t.Fatalf("cell %s/%s=%d, want %d", operation, outcome, count, want)
				}
			}
		}

		if err := RecordLifecycleOutcome(root, sprint, "replace-task", models.LifecycleConflict); err != nil {
			t.Fatalf("recording against a pre-extension file: %v", err)
		}
		rewritten := readLifecycleCountersFixture(t, filename)
		if len(rewritten.Counts) != len(lifecycleMetricOperations) {
			t.Fatalf("rewrite kept %d operation rows, want %d", len(rewritten.Counts), len(lifecycleMetricOperations))
		}
		for _, operation := range lifecycleMetricOperations {
			if len(rewritten.Counts[operation]) != len(lifecycleMetricOutcomes) {
				t.Fatalf("row %s has %d cells, want %d", operation, len(rewritten.Counts[operation]), len(lifecycleMetricOutcomes))
			}
		}
		if rewritten.Counts["claim-task"]["COMPLETED"] != 4 || rewritten.Counts["replace-task"][models.LifecycleConflict] != 1 {
			t.Fatalf("materialized matrix lost a count: %+v", rewritten.Counts)
		}
	})

	t.Run("row missing one known outcome is tolerated", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		partial := newLifecycleCounters(sprint, time.Now().UTC())
		partial.Counts["claim-task"]["COMPLETED"] = 7
		delete(partial.Counts["claim-task"], "FORBIDDEN")
		filename, _ := writeLifecycleCounters(t, root, sprint, partial)

		got := ReadLifecycleOutcomes(root, sprint)
		if !got.Available || got.Counts["claim-task"]["COMPLETED"] != 7 || got.Counts["claim-task"]["FORBIDDEN"] != 0 {
			t.Fatalf("partial row not tolerated: %+v", got)
		}
		if err := RecordLifecycleOutcome(root, sprint, "claim-task", "FORBIDDEN"); err != nil {
			t.Fatal(err)
		}
		rewritten := readLifecycleCountersFixture(t, filename)
		if rewritten.Counts["claim-task"]["FORBIDDEN"] != 1 || rewritten.Counts["claim-task"]["COMPLETED"] != 7 {
			t.Fatalf("materialized cell wrong: %+v", rewritten.Counts["claim-task"])
		}
	})

	t.Run("out-of-matrix content stays unavailable and unmodified", func(t *testing.T) {
		t.Parallel()
		unknownOperation := newLifecycleCounters(sprint, time.Now().UTC())
		unknownOperation.Counts["task-arbitrary"] = map[string]uint64{"COMPLETED": 1}
		unknownOutcome := newLifecycleCounters(sprint, time.Now().UTC())
		unknownOutcome.Counts["claim-task"]["ARBITRARY"] = 1
		emptyCounts := newLifecycleCounters(sprint, time.Now().UTC())
		emptyCounts.Counts = map[string]map[string]uint64{}
		for name, counters := range map[string]lifecycleCounterFile{
			"unknown-operation": unknownOperation,
			"unknown-outcome":   unknownOutcome,
			"empty-counts":      emptyCounts,
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				filename, before := writeLifecycleCounters(t, root, sprint, counters)
				got := ReadLifecycleOutcomes(root, sprint)
				if got.Available || got.Counts != nil || got.Warning == "" {
					t.Fatalf("out-of-matrix store presented as counters: %+v", got)
				}
				if err := RecordLifecycleOutcome(root, sprint, "claim-task", "COMPLETED"); err == nil {
					t.Fatal("out-of-matrix store unexpectedly accepted recording")
				}
				after, err := os.ReadFile(filename)
				if err != nil || !bytes.Equal(after, before) {
					t.Fatalf("out-of-matrix store overwritten: %v", err)
				}
			})
		}
	})
}

func TestLifecycleMetricsRegistersAgree(t *testing.T) {
	t.Parallel()
	for _, operation := range lifecycleMetricOperations {
		if !models.IsLifecycleOperation(operation) {
			t.Errorf("metric operation %q is not a registered lifecycle operation", operation)
		}
	}
	for _, outcome := range lifecycleMetricOutcomes {
		if !models.IsLifecycleOutcome(outcome) {
			t.Errorf("metric outcome %q is not a registered lifecycle outcome", outcome)
		}
	}
	// The registers grow; nothing that existed before may silently leave them.
	preExistingOperations := []string{
		"submit-for-review", "submit-verdict", "mark-blocked", "assess-blocked",
		"assess-hypothesis-exhausted", "claim-task", "claim-reviewer-task", "release-claim",
		"wt-merge", "recover-task", "retarget-dependency", "narrow-inherited-dependencies",
		"apply-dependency-repair", "repair-superseded-dependencies", "cancel-task",
		"supersede-task", "unblock-task", "set-task-output", "handoff", "recover-agent",
		"transition-attempt",
	}
	preExistingOutcomes := []string{
		models.LifecycleCompleted, models.LifecycleAlreadyCompleted, models.LifecycleAlreadyTransitioned,
		models.LifecycleStaleCaller, models.LifecycleStateChanged, models.LifecycleRetryable,
		models.LifecycleInvalidInput, models.LifecycleForbidden,
	}
	for _, operation := range slices.Concat(preExistingOperations, lifecycleMetricsAddedOperations) {
		if !slices.Contains(lifecycleMetricOperations[:], operation) {
			t.Errorf("operation %q missing from the metrics matrix", operation)
		}
		if !models.IsLifecycleOperation(operation) {
			t.Errorf("operation %q missing from IsLifecycleOperation", operation)
		}
	}
	for _, outcome := range slices.Concat(preExistingOutcomes, lifecycleMetricsAddedOutcomes) {
		if !slices.Contains(lifecycleMetricOutcomes[:], outcome) {
			t.Errorf("outcome %q missing from the metrics matrix", outcome)
		}
	}
	if want := len(preExistingOperations) + len(lifecycleMetricsAddedOperations); len(lifecycleMetricOperations) != want {
		t.Errorf("metrics matrix has %d operations, want %d", len(lifecycleMetricOperations), want)
	}
	if want := len(preExistingOutcomes) + len(lifecycleMetricsAddedOutcomes); len(lifecycleMetricOutcomes) != want {
		t.Errorf("metrics matrix has %d outcomes, want %d", len(lifecycleMetricOutcomes), want)
	}

	reference, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	orchestrator, ok := reference.Pipeline.Roles["orchestrator"]
	if !ok {
		t.Fatal("embedded reference has no orchestrator role")
	}
	for _, gated := range []string{"replace-task", "record-rejection-rca", "resume-rejection-rca", "reaffirm-proof"} {
		if !slices.Contains(orchestrator.AllowedOperations, gated) {
			t.Errorf("orchestrator allowed-operations missing %q", gated)
		}
	}
	// validate-payload is state-free: recorded for counters, never RBAC-gated.
	for role, definition := range reference.Pipeline.Roles {
		if slices.Contains(definition.AllowedOperations, "validate-payload") {
			t.Errorf("role %q gates the state-free validate-payload operation", role)
		}
	}

	root, sprint := t.TempDir(), lifecycleMetricsSprint()
	pairs := [][2]string{
		{"validate-payload", models.LifecycleInvalidInput},
		{"validate-payload", models.LifecycleCompleted},
		{"assess-blocked", models.LifecycleNoChange},
		{"replace-task", models.LifecycleConflict},
		{"record-rejection-rca", models.LifecycleNoChange},
		{"resume-rejection-rca", models.LifecycleCompleted},
	}
	for _, pair := range pairs {
		if err := RecordLifecycleOutcome(root, sprint, pair[0], pair[1]); err != nil {
			t.Fatalf("recording %s/%s: %v", pair[0], pair[1], err)
		}
	}
	counts := ReadLifecycleOutcomes(root, sprint)
	if !counts.Available {
		t.Fatalf("new rows unavailable: %+v", counts)
	}
	for _, pair := range pairs {
		if got := counts.Counts[pair[0]][pair[1]]; got != 1 {
			t.Errorf("%s/%s count=%d, want 1", pair[0], pair[1], got)
		}
	}
}
