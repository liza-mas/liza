package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const rejectionRCATestActor = "orchestrator-1"

// rejectionRCAFixture is one gated project: a BLOCKED coding task whose gate
// fired at threshold 4 on its fourth durable rejection, with the seeded record
// and nothing recorded yet.
type rejectionRCAFixture struct {
	projectRoot string
	statePath   string
	gatedAt     time.Time
}

func newRejectionRCAFixture(t *testing.T, mutate func(*models.Task)) rejectionRCAFixture {
	t.Helper()
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	now := time.Now().UTC().Truncate(time.Second)
	gatedAt := now.Add(-time.Minute)

	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.AssignedTo = nil
	reason := models.BlockedReasonRejectionRCARequired + ": 4 durable rejections reached threshold 4"
	task.BlockedReason = &reason
	task.Iteration = 3
	task.ReviewCyclesCurrent = 4
	task.ReviewCyclesTotal = 4
	task.Lifecycle = &models.TaskLifecycle{Revision: 7}
	task.RejectionRCA = &models.RejectionRCARecord{
		SchemaVersion:  models.RejectionRCASchemaVersion,
		Threshold:      4,
		RejectionCount: 4,
		GatedAt:        gatedAt,
		GatingCommit:   "0123456789abcdef0123456789abcdef01234567",
	}
	if mutate != nil {
		mutate(&task)
	}

	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, statePath, state)
	return rejectionRCAFixture{projectRoot: projectRoot, statePath: statePath, gatedAt: gatedAt}
}

// recordedRejectionRCA seeds a fully recorded RCA so resume tests do not
// depend on the record operation.
func recordedRejectionRCA(request models.RejectionRCARequest, recordedBy string, at time.Time) func(*models.Task) {
	return func(task *models.Task) {
		normalized := models.NormalizeRejectionRCARequest(request)
		task.RejectionRCA.Summary = normalized.Summary
		task.RejectionRCA.Contributions = normalized.Contributions
		task.RejectionRCA.Fingerprint = models.RejectionRCAFingerprint(request)
		task.RejectionRCA.RecordedBy = recordedBy
		task.RejectionRCA.RecordedAt = &at
	}
}

func (f rejectionRCAFixture) readTask(t *testing.T) *models.Task {
	t.Helper()
	state, err := db.New(f.statePath).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	task := state.FindTask("task-1")
	if task == nil {
		t.Fatal("task-1 disappeared")
	}
	return task
}

func (f rejectionRCAFixture) stateBytes(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mixedCauseRequest() models.RejectionRCARequest {
	return models.RejectionRCARequest{
		SchemaVersion: models.RejectionRCASchemaVersion,
		Summary:       "Two product defects, one reviewer unable to run Postgres, one stale-ownership retry.",
		Contributions: []models.RejectionRCAContribution{
			{RejectionIndex: 1, Categories: []string{models.RejectionCauseProductDefect}, Evidence: []string{"verdict 1: identity scalar mismatch"}},
			{RejectionIndex: 2, Categories: []string{models.RejectionCauseCapabilityFailure, models.RejectionCauseProductDefect}, Evidence: []string{"verdict 2: reviewer could not start Postgres", "verdict 2: concurrency defect"}},
			{RejectionIndex: 3, Categories: []string{models.RejectionCauseLifecycleRetry}, Evidence: []string{"submit-review STALE_CALLER after reclaim"}},
			{RejectionIndex: 4, Categories: []string{models.RejectionCauseProductDefect}, Evidence: []string{"verdict 4: missing migration"}},
		},
	}
}

// payloadObject is the canonical object a request file decodes to.
func payloadObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return payloadFromJSON(t, string(data))
}

func payloadFromJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func dispositionPayload(path, rationale string) map[string]any {
	object := map[string]any{"schema_version": float64(models.RejectionRCASchemaVersion), "recovery_path": path}
	if rationale != "" {
		object["rationale"] = rationale
	}
	return object
}

func historyEntries(task *models.Task, event models.TaskEventName) []models.TaskHistoryEntry {
	var entries []models.TaskHistoryEntry
	for _, entry := range task.History {
		if entry.Event == event {
			entries = append(entries, entry)
		}
	}
	return entries
}

// extraStrings reads a string list detail key back through the YAML round
// trip, which yields []any.
func extraStrings(t *testing.T, extra map[string]any, key string) []string {
	t.Helper()
	raw, ok := extra[key].([]any)
	if !ok {
		t.Fatalf("detail %s = %#v, want a list", key, extra[key])
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		values = append(values, value.(string))
	}
	return values
}

func requireDetailKeys(t *testing.T, extra map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := extra[key]; !ok {
			t.Errorf("detail %s missing from %#v", key, extra)
		}
	}
}

func requirePrecondition(t *testing.T, err error) models.LifecycleOutcome {
	t.Helper()
	if err == nil {
		t.Fatal("expected a refusal")
	}
	var precondition *PreconditionError
	if !errors.As(err, &precondition) {
		t.Fatalf("refusal is not a precondition failure: %v", err)
	}
	var lifecycleErr *LifecycleError
	if !errors.As(err, &lifecycleErr) {
		t.Fatalf("refusal carries no lifecycle outcome: %v", err)
	}
	if len(lifecycleErr.Outcome.Diagnostics) != 0 {
		t.Fatalf("precondition failure carries diagnostics: %#v", lifecycleErr.Outcome.Diagnostics)
	}
	if lifecycleErr.Outcome.Effects != "none" {
		t.Fatalf("effects = %s, want none", lifecycleErr.Outcome.Effects)
	}
	return lifecycleErr.Outcome
}

func TestRecordRejectionRCA(t *testing.T) {
	t.Parallel()

	t.Run("mixed-cause request persists with bounded evidence", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, nil)
		request := mixedCauseRequest()

		result, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, request), rejectionRCATestActor, RejectionRCAOptions{})
		if err != nil {
			t.Fatalf("RecordRejectionRCA() error: %v", err)
		}
		if result.Outcome != models.LifecycleCompleted || result.SafeAction != "continue" || result.Changed == nil || !*result.Changed {
			t.Fatalf("outcome = %#v, want COMPLETED/continue/changed", result.LifecycleOutcome)
		}

		task := fixture.readTask(t)
		if task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() {
			t.Fatalf("status/gate = %s/%v, want BLOCKED with the gate open", task.Status, task.RejectionRCAGateOpen())
		}
		record := task.RejectionRCA
		if record.Threshold != 4 || record.RejectionCount != 4 || !record.GatedAt.Equal(fixture.gatedAt) || record.GatingCommit != "0123456789abcdef0123456789abcdef01234567" {
			t.Fatalf("seeded fields changed: %#v", record)
		}
		normalized := models.NormalizeRejectionRCARequest(request)
		if record.Summary != normalized.Summary || !reflect.DeepEqual(record.Contributions, normalized.Contributions) {
			t.Fatalf("stored caller fields = %q %#v, want the normalized request", record.Summary, record.Contributions)
		}
		if record.Fingerprint != models.RejectionRCAFingerprint(request) {
			t.Fatalf("fingerprint = %s, want the request fingerprint", record.Fingerprint)
		}
		if record.RecordedBy != rejectionRCATestActor || record.RecordedAt == nil || record.RecordedAt.IsZero() {
			t.Fatalf("recorded_by/recorded_at = %q/%v", record.RecordedBy, record.RecordedAt)
		}
		if !reflect.DeepEqual(result.RejectionRCA, record) {
			t.Fatalf("result record %#v differs from stored %#v", result.RejectionRCA, record)
		}

		entries := historyEntries(task, models.TaskEventRejectionRCARecorded)
		if len(entries) != 1 {
			t.Fatalf("rejection_rca_recorded entries = %d, want 1", len(entries))
		}
		entry := entries[0]
		if entry.Agent == nil || *entry.Agent != rejectionRCATestActor {
			t.Errorf("entry agent = %v", entry.Agent)
		}
		requireDetailKeys(t, entry.Extra, "fingerprint", "threshold", "rejection_count", "gated_at", "causes", "contribution_count", "recorded_by")
		if entry.Extra["fingerprint"] != record.Fingerprint || entry.Extra["threshold"] != 4 || entry.Extra["rejection_count"] != 4 ||
			entry.Extra["gated_at"] != fixture.gatedAt.Format(time.RFC3339) || entry.Extra["contribution_count"] != 4 || entry.Extra["recorded_by"] != rejectionRCATestActor {
			t.Errorf("detail values = %#v", entry.Extra)
		}
		wantCauses := []string{models.RejectionCauseCapabilityFailure, models.RejectionCauseLifecycleRetry, models.RejectionCauseProductDefect}
		if got := extraStrings(t, entry.Extra, "causes"); !reflect.DeepEqual(got, wantCauses) {
			t.Errorf("causes = %v, want %v", got, wantCauses)
		}
	})

	t.Run("unrecognized cause is stored and reported verbatim", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, nil)
		request := models.RejectionRCARequest{
			SchemaVersion: models.RejectionRCASchemaVersion,
			Summary:       "CI flake dominated the loop.",
			Contributions: []models.RejectionRCAContribution{
				{RejectionIndex: 1, Categories: []string{"flaky_ci", models.RejectionCauseProductDefect}},
				{RejectionIndex: 2, Categories: []string{"flaky_ci"}},
			},
		}

		if _, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, request), rejectionRCATestActor, RejectionRCAOptions{}); err != nil {
			t.Fatalf("RecordRejectionRCA() error: %v", err)
		}

		task := fixture.readTask(t)
		if got := task.RejectionRCA.Contributions[1].Categories; !reflect.DeepEqual(got, []string{"flaky_ci"}) {
			t.Fatalf("stored categories = %v, want the verbatim unknown cause", got)
		}
		entries := historyEntries(task, models.TaskEventRejectionRCARecorded)
		if len(entries) != 1 {
			t.Fatalf("rejection_rca_recorded entries = %d, want 1", len(entries))
		}
		if got := extraStrings(t, entries[0].Extra, "causes"); !reflect.DeepEqual(got, []string{"flaky_ci", models.RejectionCauseProductDefect}) {
			t.Fatalf("causes = %v, want the unknown cause verbatim", got)
		}
	})

	t.Run("rejection_index above the durable count is a precondition failure", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, nil)
		request := mixedCauseRequest()
		request.Contributions = append(request.Contributions, models.RejectionRCAContribution{RejectionIndex: 5, Categories: []string{models.RejectionCauseProductDefect}})
		before := fixture.stateBytes(t)

		_, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, request), rejectionRCATestActor, RejectionRCAOptions{})
		outcome := requirePrecondition(t, err)
		if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != "correct_input" {
			t.Fatalf("outcome = %s/%s, want INVALID_INPUT/correct_input", outcome.Outcome, outcome.SafeAction)
		}
		if !strings.Contains(err.Error(), "rejection_index") {
			t.Fatalf("refusal does not name rejection_index: %v", err)
		}
		if fixture.stateBytes(t) != before {
			t.Fatal("a refused record changed state.yaml")
		}
	})

	t.Run("non-gated task is refused with no state change", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, func(task *models.Task) {
			task.RejectionRCA = nil
			reason := "ordinary blocker"
			task.BlockedReason = &reason
		})
		before := fixture.stateBytes(t)

		_, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, mixedCauseRequest()), rejectionRCATestActor, RejectionRCAOptions{})
		outcome := requirePrecondition(t, err)
		if outcome.Outcome != models.LifecycleStateChanged || outcome.SafeAction != "requery" {
			t.Fatalf("outcome = %s/%s, want STATE_CHANGED/requery", outcome.Outcome, outcome.SafeAction)
		}
		if fixture.stateBytes(t) != before {
			t.Fatal("a refused record changed state.yaml")
		}
	})

	t.Run("non-orchestrator caller is forbidden", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, nil)
		before := fixture.stateBytes(t)

		_, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, mixedCauseRequest()), "coder-1", RejectionRCAOptions{})
		var lifecycleErr *LifecycleError
		if !errors.As(err, &lifecycleErr) || lifecycleErr.Outcome.Outcome != models.LifecycleForbidden || lifecycleErr.Outcome.SafeAction != "stop" {
			t.Fatalf("err = %v, want FORBIDDEN/stop", err)
		}
		if fixture.stateBytes(t) != before {
			t.Fatal("a forbidden record changed state.yaml")
		}
	})
}

func TestRecordRejectionRCAIdempotent(t *testing.T) {
	t.Parallel()
	fixture := newRejectionRCAFixture(t, nil)
	request := mixedCauseRequest()

	first, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, request), rejectionRCATestActor, RejectionRCAOptions{})
	if err != nil {
		t.Fatalf("first RecordRejectionRCA() error: %v", err)
	}
	after := fixture.stateBytes(t)

	// Key order differs, whitespace is padded and categories are reordered:
	// the same identity as the request file above.
	equivalent := payloadFromJSON(t, `{
	  "contributions": [
	    {"evidence": ["verdict 4:   missing migration"], "categories": ["product_defect"], "rejection_index": 4},
	    {"categories": ["lifecycle_retry"], "rejection_index": 3, "evidence": ["  submit-review STALE_CALLER after reclaim "]},
	    {"rejection_index": 2, "categories": ["product_defect", "capability_failure"], "evidence": ["verdict 2: reviewer could not start Postgres", "verdict 2: concurrency defect"]},
	    {"rejection_index": 1, "categories": ["product_defect"], "evidence": ["verdict 1: identity scalar mismatch"]}
	  ],
	  "summary": "  Two product defects, one reviewer unable to run Postgres,   one stale-ownership retry. ",
	  "schema_version": 1
	}`)
	for name, payload := range map[string]map[string]any{"identical": payloadObject(t, request), "equivalent": equivalent} {
		result, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payload, rejectionRCATestActor, RejectionRCAOptions{})
		if err != nil {
			t.Fatalf("%s resubmission error: %v", name, err)
		}
		if result.Outcome != models.LifecycleNoChange || result.SafeAction != "stop" || result.Changed == nil || *result.Changed {
			t.Fatalf("%s resubmission outcome = %#v, want NO_CHANGE/stop/changed=false", name, result.LifecycleOutcome)
		}
		if result.RejectionRCA == nil || result.RejectionRCA.Fingerprint != first.RejectionRCA.Fingerprint {
			t.Fatalf("%s resubmission record = %#v, want the stored record", name, result.RejectionRCA)
		}
		if fixture.stateBytes(t) != after {
			t.Fatalf("%s resubmission changed state.yaml", name)
		}
	}
	if entries := historyEntries(fixture.readTask(t), models.TaskEventRejectionRCARecorded); len(entries) != 1 {
		t.Fatalf("rejection_rca_recorded entries = %d after resubmission, want 1", len(entries))
	}

	different := mixedCauseRequest()
	different.Summary = "Revised: the fourth rejection was a lifecycle retry, not a defect."
	different.Contributions[3].Categories = []string{models.RejectionCauseLifecycleRetry}
	result, err := RecordRejectionRCA(fixture.projectRoot, "task-1", payloadObject(t, different), rejectionRCATestActor, RejectionRCAOptions{})
	if err != nil {
		t.Fatalf("different RecordRejectionRCA() error: %v", err)
	}
	if result.Outcome != models.LifecycleCompleted {
		t.Fatalf("different request outcome = %s, want COMPLETED", result.Outcome)
	}
	task := fixture.readTask(t)
	record := task.RejectionRCA
	if record.Fingerprint != models.RejectionRCAFingerprint(different) || record.Summary != different.Summary {
		t.Fatalf("caller fields not replaced: %#v", record)
	}
	if record.Threshold != 4 || record.RejectionCount != 4 || !record.GatedAt.Equal(fixture.gatedAt) {
		t.Fatalf("seeded fields changed on replacement: %#v", record)
	}
	entries := historyEntries(task, models.TaskEventRejectionRCARecorded)
	if len(entries) != 2 {
		t.Fatalf("rejection_rca_recorded entries = %d after replacement, want 2", len(entries))
	}
	if entries[1].Extra["fingerprint"] != record.Fingerprint {
		t.Fatalf("second event fingerprint = %v, want %s", entries[1].Extra["fingerprint"], record.Fingerprint)
	}
}

func TestResumeRejectionRCADispositions(t *testing.T) {
	t.Parallel()
	recordedAt := time.Now().UTC().Truncate(time.Second)

	cases := []struct {
		path            string
		rationale       string
		restoreMode     string
		iterationExempt bool
		reviewCycles    int
	}{
		{path: models.RecoveryImplementationCorrection, rationale: "defects dominate", restoreMode: models.RestoreModeClaimable, reviewCycles: 4},
		{path: models.RecoveryCapabilityReroute, rationale: "route validation to a Postgres-capable reviewer", restoreMode: models.RestoreModeAssign, iterationExempt: true},
		{path: models.RecoveryLifecycleRepair, restoreMode: models.RestoreModeAssign, iterationExempt: true},
		{path: models.RecoveryRescope, rationale: "split the task", restoreMode: models.RestoreModeNone, reviewCycles: 4},
		{path: models.RecoveryHumanOverride, rationale: "operator accepts the residual risk", restoreMode: models.RestoreModeClaimable, reviewCycles: 4},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			fixture := newRejectionRCAFixture(t, recordedRejectionRCA(mixedCauseRequest(), rejectionRCATestActor, recordedAt))
			fingerprint := fixture.readTask(t).RejectionRCA.Fingerprint

			result, err := ResumeRejectionRCA(fixture.projectRoot, "task-1", dispositionPayload(tc.path, tc.rationale), rejectionRCATestActor, RejectionRCAOptions{})
			if err != nil {
				t.Fatalf("ResumeRejectionRCA() error: %v", err)
			}
			if result.Outcome != models.LifecycleCompleted || result.Changed == nil || !*result.Changed {
				t.Fatalf("outcome = %#v, want COMPLETED/changed", result.LifecycleOutcome)
			}

			task := fixture.readTask(t)
			if task.Status != models.TaskStatusBlocked {
				t.Fatalf("status = %s, want BLOCKED", task.Status)
			}
			if task.RejectionRCAGateOpen() {
				t.Fatal("gate still open after resume")
			}
			if task.Iteration != 3 {
				t.Fatalf("iteration = %d, want 3 untouched", task.Iteration)
			}
			if task.ReviewCyclesCurrent != tc.reviewCycles {
				t.Fatalf("review_cycles_current = %d, want %d", task.ReviewCyclesCurrent, tc.reviewCycles)
			}
			if task.RejectionRCA.Fingerprint != fingerprint || task.RejectionRCA.RecordedBy != rejectionRCATestActor {
				t.Fatalf("recorded RCA changed by resume: %#v", task.RejectionRCA)
			}
			disposition := task.RejectionRCA.Disposition
			if disposition.RecoveryPath != tc.path || disposition.RestoreMode != tc.restoreMode || disposition.Actor != rejectionRCATestActor ||
				disposition.LifecycleVersion != 7 || disposition.DecidedAt.IsZero() || disposition.Rationale != tc.rationale || disposition.IterationExempt != tc.iterationExempt {
				t.Fatalf("disposition = %#v", disposition)
			}
			if !reflect.DeepEqual(result.RejectionRCA, task.RejectionRCA) {
				t.Fatalf("result record %#v differs from stored %#v", result.RejectionRCA, task.RejectionRCA)
			}

			entries := historyEntries(task, models.TaskEventRejectionRCAResumed)
			if len(entries) != 1 {
				t.Fatalf("rejection_rca_resumed entries = %d, want 1", len(entries))
			}
			extra := entries[0].Extra
			requireDetailKeys(t, extra, "fingerprint", "recovery_path", "restore_mode", "actor", "lifecycle_version", "decided_at", "iteration_exempt", "gated_at")
			if extra["fingerprint"] != fingerprint || extra["recovery_path"] != tc.path || extra["restore_mode"] != tc.restoreMode || extra["actor"] != rejectionRCATestActor ||
				extra["lifecycle_version"] != 7 || extra["decided_at"] != disposition.DecidedAt.Format(time.RFC3339) || extra["iteration_exempt"] != tc.iterationExempt ||
				extra["gated_at"] != fixture.gatedAt.Format(time.RFC3339) {
				t.Fatalf("detail values = %#v", extra)
			}
		})
	}

	t.Run("human_override requires a rationale", func(t *testing.T) {
		t.Parallel()
		fixture := newRejectionRCAFixture(t, recordedRejectionRCA(mixedCauseRequest(), rejectionRCATestActor, recordedAt))
		before := fixture.stateBytes(t)

		_, err := ResumeRejectionRCA(fixture.projectRoot, "task-1", dispositionPayload(models.RecoveryHumanOverride, "   "), rejectionRCATestActor, RejectionRCAOptions{})
		var lifecycleErr *LifecycleError
		if !errors.As(err, &lifecycleErr) {
			t.Fatalf("expected a lifecycle refusal, got %v", err)
		}
		outcome := lifecycleErr.Outcome
		if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != "correct_input" {
			t.Fatalf("outcome = %s/%s, want INVALID_INPUT/correct_input", outcome.Outcome, outcome.SafeAction)
		}
		want := []models.FieldDiagnostic{{SchemaVersion: 1, Field: "/rationale", Constraint: "a non-empty value is required",
			ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput}}
		if !reflect.DeepEqual(outcome.Diagnostics, want) || outcome.Effects != "none" {
			t.Fatalf("outcome = %+v, want rationale diagnostic with no effects", outcome)
		}
		if fixture.stateBytes(t) != before {
			t.Fatal("a refused resume changed state.yaml")
		}
		if fixture.readTask(t).RejectionRCA.Disposition != nil {
			t.Fatal("disposition recorded without a rationale")
		}
	})
}

func TestResumeRejectionRCAWithoutRecord(t *testing.T) {
	t.Parallel()
	recordedAt := time.Now().UTC().Truncate(time.Second)

	cases := []struct {
		name   string
		mutate func(*models.Task)
	}{
		{name: "seeded but unrecorded gate", mutate: nil},
		{name: "no record at all", mutate: func(task *models.Task) {
			task.RejectionRCA = nil
			reason := "ordinary blocker"
			task.BlockedReason = &reason
		}},
		{name: "gate already closed", mutate: func(task *models.Task) {
			recordedRejectionRCA(mixedCauseRequest(), rejectionRCATestActor, recordedAt)(task)
			task.RejectionRCA.Disposition = &models.RejectionRCADisposition{
				RecoveryPath: models.RecoveryRescope, RestoreMode: models.RestoreModeNone,
				Actor: rejectionRCATestActor, DecidedAt: recordedAt,
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRejectionRCAFixture(t, tc.mutate)
			before := fixture.stateBytes(t)

			_, err := ResumeRejectionRCA(fixture.projectRoot, "task-1", dispositionPayload(models.RecoveryImplementationCorrection, "fix it"), rejectionRCATestActor, RejectionRCAOptions{})
			outcome := requirePrecondition(t, err)
			if outcome.Outcome != models.LifecycleStateChanged || outcome.SafeAction != "requery" {
				t.Fatalf("outcome = %s/%s, want STATE_CHANGED/requery", outcome.Outcome, outcome.SafeAction)
			}
			if fixture.stateBytes(t) != before {
				t.Fatal("a refused resume changed state.yaml")
			}
		})
	}
}

func TestRejectionRCAInvalidPayload(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("x", 4097)
	cases := []struct {
		name      string
		operation string
		payload   any
		field     string
	}{
		{name: "record: not an object", operation: payloadschema.RecordRejectionRCAOperation, payload: []any{"summary"}, field: "/"},
		{name: "record: wrong schema version", operation: payloadschema.RecordRejectionRCAOperation,
			payload: payloadObject(t, models.RejectionRCARequest{SchemaVersion: 2, Summary: "s", Contributions: mixedCauseRequest().Contributions}), field: "/schema_version"},
		{name: "record: empty summary", operation: payloadschema.RecordRejectionRCAOperation,
			payload: payloadObject(t, models.RejectionRCARequest{SchemaVersion: 1, Contributions: mixedCauseRequest().Contributions}), field: "/summary"},
		{name: "record: no contributions", operation: payloadschema.RecordRejectionRCAOperation,
			payload: payloadObject(t, models.RejectionRCARequest{SchemaVersion: 1, Summary: "s"}), field: "/contributions"},
		{name: "record: seeded key supplied", operation: payloadschema.RecordRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"summary":"s","threshold":9,"contributions":[{"rejection_index":1,"categories":["product_defect"]}]}`), field: "/threshold"},
		{name: "record: mistyped contribution index", operation: payloadschema.RecordRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"summary":"s","contributions":[{"rejection_index":"one","categories":["product_defect"]}]}`), field: "/contributions/0/rejection_index"},
		{name: "resume: unknown recovery path", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: dispositionPayload("retry_harder", ""), field: "/recovery_path"},
		{name: "resume: oversized rationale", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: dispositionPayload(models.RecoveryRescope, oversized), field: "/rationale"},
		{name: "resume: missing override rationale", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"recovery_path":"human_override"}`), field: "/rationale"},
		{name: "resume: empty override rationale", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"recovery_path":"human_override","rationale":""}`), field: "/rationale"},
		{name: "resume: whitespace override rationale", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"recovery_path":"human_override","rationale":"   "}`), field: "/rationale"},
		{name: "resume: derived key supplied", operation: payloadschema.ResumeRejectionRCAOperation,
			payload: payloadFromJSON(t, `{"schema_version":1,"recovery_path":"rescope","restore_mode":"claimable"}`), field: "/restore_mode"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := newRejectionRCAFixture(t, recordedRejectionRCA(mixedCauseRequest(), rejectionRCATestActor, time.Now().UTC()))
			before := fixture.stateBytes(t)
			// Fixture setup has finished using the lock. Any later state read
			// or mutation would recreate it, even if state bytes stay unchanged.
			lockPath := fixture.statePath + ".lock"
			if err := os.Remove(lockPath); err != nil {
				t.Fatal(err)
			}

			version, preflight, preflightErr := payloadschema.Validate(tc.operation, tc.payload)
			if preflightErr != nil {
				t.Fatal(preflightErr)
			}
			if version != 1 {
				t.Fatalf("schema version = %d, want 1", version)
			}

			var err error
			if tc.operation == payloadschema.RecordRejectionRCAOperation {
				_, err = RecordRejectionRCA(fixture.projectRoot, "task-1", tc.payload, rejectionRCATestActor, RejectionRCAOptions{})
			} else {
				_, err = ResumeRejectionRCA(fixture.projectRoot, "task-1", tc.payload, rejectionRCATestActor, RejectionRCAOptions{})
			}
			if _, statErr := os.Stat(lockPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("structurally invalid payload reached the state lock: %v", statErr)
			}
			if err == nil {
				t.Fatal("mutation boundary accepted a payload the preflight rejects")
			}
			var lifecycleErr *LifecycleError
			if !errors.As(err, &lifecycleErr) {
				t.Fatalf("error carries no lifecycle outcome: %v", err)
			}
			outcome := lifecycleErr.Outcome
			if outcome.Outcome != models.LifecycleInvalidInput || outcome.SafeAction != "correct_input" || outcome.Effects != "none" {
				t.Fatalf("outcome = %s/%s/%s, want INVALID_INPUT/correct_input/none", outcome.Outcome, outcome.SafeAction, outcome.Effects)
			}
			if len(outcome.Diagnostics) == 0 {
				t.Fatal("INVALID_INPUT without diagnostics")
			}
			if !reflect.DeepEqual(outcome.Diagnostics, preflight) {
				t.Fatalf("diagnostics diverge from the preflight:\nmutation %#v\npreflight %#v", outcome.Diagnostics, preflight)
			}
			found := false
			for _, diagnostic := range outcome.Diagnostics {
				if diagnostic.SchemaVersion != 1 {
					t.Errorf("diagnostic schema version = %d, want 1", diagnostic.SchemaVersion)
				}
				if diagnostic.Field == tc.field {
					found = true
				}
				if strings.Contains(diagnostic.Constraint, oversized[:64]) {
					t.Fatalf("diagnostic echoes the rejected value: %#v", diagnostic)
				}
			}
			if !found {
				t.Fatalf("no diagnostic on %s: %#v", tc.field, outcome.Diagnostics)
			}
			if fixture.stateBytes(t) != before {
				t.Fatal("a rejected payload changed state.yaml")
			}
		})
	}
}

// gateLoopFixture drives one reviewed task through the real lifecycle
// operations — claim, submit-for-review, reviewer claim, verdict — against a
// git-backed project, so the end-to-end gate tests exercise the blackboard as
// the CLI would rather than seeding rejection history.
type gateLoopFixture struct {
	t         *testing.T
	root      string
	statePath string
	taskID    string
	role      gateVerdictRole
	resolver  models.PipelineResolver
	commits   int
}

func newGateLoopFixture(t *testing.T, role gateVerdictRole, mutate func(*models.Task)) *gateLoopFixture {
	t.Helper()
	disableLifecycleTestIndexes(t)
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")

	resolver, _, err := loadResolver(root)
	if err != nil {
		t.Fatalf("loadResolver() error: %v", err)
	}
	initial, err := resolver.InitialStatus(role.rolePair)
	if err != nil {
		t.Fatal(err)
	}

	taskID := "gate-loop-" + strings.ReplaceAll(role.name, " ", "-")
	now := time.Now().UTC()
	task := models.Task{
		ID: taskID, Type: role.taskType, RolePair: role.rolePair, Description: "Gate loop task",
		Status: initial, Priority: 1, Created: now, SpecRef: "README.md", DoneWhen: "Task is complete", Scope: "Test scope",
	}
	if mutate != nil {
		mutate(&task)
	}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Agents[role.doer] = testhelpers.RegisteredTestAgent(role.doerRole)
	state.Agents[role.reviewer] = testhelpers.RegisteredTestAgent(role.reviewerRole)
	testhelpers.WriteInitialState(t, statePath, state)
	return &gateLoopFixture{t: t, root: root, statePath: statePath, taskID: taskID, role: role, resolver: resolver}
}

func (f *gateLoopFixture) status(name string, resolve func(string) (models.TaskStatus, error)) models.TaskStatus {
	f.t.Helper()
	status, err := resolve(f.role.rolePair)
	if err != nil {
		f.t.Fatalf("%s status for %s: %v", name, f.role.rolePair, err)
	}
	return status
}

func (f *gateLoopFixture) task() *models.Task {
	f.t.Helper()
	return mustReadTask(f.t, f.statePath, f.taskID)
}

func (f *gateLoopFixture) stateBytes() string {
	f.t.Helper()
	return string(readStateBytes(f.t, f.statePath))
}

func (f *gateLoopFixture) claim() {
	f.t.Helper()
	if _, err := ClaimTask(f.root, f.taskID, f.role.doer); err != nil {
		f.t.Fatalf("ClaimTask() error: %v", err)
	}
	if task := f.task(); task.Status != f.status("executing", f.resolver.ExecutingStatus) || task.AssignedTo == nil || *task.AssignedTo != f.role.doer {
		f.t.Fatalf("after claim: status=%s assigned_to=%v", task.Status, task.AssignedTo)
	}
}

// commit adds one more file to the task worktree so every submission carries
// a fresh immutable boundary; the first commit includes a test file so the
// coding pair satisfies TDD admission.
func (f *gateLoopFixture) commit() string {
	f.t.Helper()
	wt := git.New(f.root).GetWorktreePath(f.taskID)
	f.commits++
	if f.commits == 1 {
		writeAndCommit(f.t, wt, "feature_test.go", "package main\n", "add tests")
	}
	writeAndCommit(f.t, wt, fmt.Sprintf("change-%d.txt", f.commits), "change\n", fmt.Sprintf("iteration %d", f.commits))
	return testhelpers.MustGit(f.t, wt, "rev-parse", "HEAD")
}

func (f *gateLoopFixture) checkpoint() {
	f.t.Helper()
	err := WriteCheckpoint(f.root, &WriteCheckpointInput{
		TaskID: f.taskID, AgentID: f.role.doer, Intent: "drive the gate loop",
		ValidationPlan: "verdict sequence", FilesToModify: []string{"change-1.txt"},
	})
	if err != nil {
		f.t.Fatalf("WriteCheckpoint() error: %v", err)
	}
}

func (f *gateLoopFixture) submit(sha string) {
	f.t.Helper()
	result, err := SubmitForReview(f.root, f.taskID, sha, f.role.doer)
	if err != nil {
		f.t.Fatalf("SubmitForReview() error: %v", err)
	}
	if result.Outcome != models.LifecycleCompleted {
		f.t.Fatalf("SubmitForReview outcome = %s, want COMPLETED", result.Outcome)
	}
}

func (f *gateLoopFixture) review() {
	f.t.Helper()
	if _, err := ClaimReviewerTask(ClaimReviewerTaskInput{ProjectRoot: f.root, AgentID: f.role.reviewer, TaskID: f.taskID}); err != nil {
		f.t.Fatalf("ClaimReviewerTask() error: %v", err)
	}
}

func (f *gateLoopFixture) verdict(verdict, reason string) *VerdictResult {
	f.t.Helper()
	result, err := SubmitVerdict(f.root, f.taskID, verdict, reason, f.role.reviewer, "")
	if err != nil {
		f.t.Fatalf("SubmitVerdict(%s) error: %v", verdict, err)
	}
	return result
}

// iterate is one doer/reviewer cycle: (re)claim, commit, submit, reviewer
// claim, verdict.
func (f *gateLoopFixture) iterate(verdict, reason string) *VerdictResult {
	f.t.Helper()
	f.claim()
	sha := f.commit()
	if f.commits == 1 {
		f.checkpoint()
	}
	f.submit(sha)
	f.review()
	return f.verdict(verdict, reason)
}

// rejectUntilGated drives rejections through real verdicts until the default
// threshold gates the task, checking that no earlier rejection does.
func (f *gateLoopFixture) rejectUntilGated() {
	f.t.Helper()
	threshold := models.DefaultHighChurnRejectionThreshold
	for i := 1; i <= threshold; i++ {
		result := f.iterate("REJECTED", fmt.Sprintf("rejection %d", i))
		task := f.task()
		if i < threshold {
			if result.EscalatedToBlocked || task.Status != f.status("rejected", f.resolver.RejectedStatus) || task.RejectionRCA != nil {
				f.t.Fatalf("rejection %d: escalated=%v status=%s record=%v, want a plain rejection", i, result.EscalatedToBlocked, task.Status, task.RejectionRCA)
			}
			continue
		}
		if !result.RejectionRCAGated || task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() {
			f.t.Fatalf("rejection %d: gated=%v status=%s open=%v, want the gate", i, result.RejectionRCAGated, task.Status, task.RejectionRCAGateOpen())
		}
		if task.BlockedReason == nil || !strings.HasPrefix(*task.BlockedReason, models.BlockedReasonRejectionRCARequired) {
			f.t.Fatalf("blocked_reason = %v, want the typed prefix", task.BlockedReason)
		}
		record := task.RejectionRCA
		if record.Threshold != threshold || record.RejectionCount != threshold || task.DurableRejectionCount() != threshold || record.GatingCommit == "" {
			f.t.Fatalf("seeded record = %+v (durable %d), want threshold/count %d with the gating commit", record, task.DurableRejectionCount(), threshold)
		}
		if task.Iteration != threshold || task.AssignedTo != nil {
			f.t.Fatalf("at gate: iteration=%d assigned_to=%v, want %d/nil", task.Iteration, task.AssignedTo, threshold)
		}
	}
}

// recordFromFile submits the RCA the way the CLI does: the request is written
// to a file and its decoded JSON object is the payload.
func (f *gateLoopFixture) recordFromFile(request models.RejectionRCARequest) {
	f.t.Helper()
	data, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(f.t.TempDir(), "rca.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		f.t.Fatal(err)
	}
	result, err := RecordRejectionRCA(f.root, f.taskID, payload, rejectionRCATestActor, RejectionRCAOptions{})
	if err != nil {
		f.t.Fatalf("RecordRejectionRCA() error: %v", err)
	}
	if result.Outcome != models.LifecycleCompleted {
		f.t.Fatalf("record outcome = %s, want COMPLETED", result.Outcome)
	}
	task := f.task()
	if task.RejectionRCA.Fingerprint != models.RejectionRCAFingerprint(request) || len(task.RejectionRCA.Contributions) != len(request.Contributions) {
		f.t.Fatalf("stored record %+v does not reproduce the request file", task.RejectionRCA)
	}
	entries := historyEntries(task, models.TaskEventRejectionRCARecorded)
	if len(entries) != 1 {
		f.t.Fatalf("rejection_rca_recorded entries = %d, want 1", len(entries))
	}
	wantCauses := []string{models.RejectionCauseCapabilityFailure, models.RejectionCauseLifecycleRetry, models.RejectionCauseProductDefect}
	if got := extraStrings(f.t, entries[0].Extra, "causes"); !reflect.DeepEqual(got, wantCauses) {
		f.t.Fatalf("causes = %v, want the mixed set %v", got, wantCauses)
	}
}

func (f *gateLoopFixture) resume(path string) {
	f.t.Helper()
	result, err := ResumeRejectionRCA(f.root, f.taskID, dispositionPayload(path, "disposition for "+path), rejectionRCATestActor, RejectionRCAOptions{})
	if err != nil {
		f.t.Fatalf("ResumeRejectionRCA(%s) error: %v", path, err)
	}
	if result.Outcome != models.LifecycleCompleted {
		f.t.Fatalf("resume outcome = %s, want COMPLETED", result.Outcome)
	}
	task := f.task()
	if task.Status != models.TaskStatusBlocked || task.RejectionRCAGateOpen() {
		f.t.Fatalf("after resume: status=%s open=%v, want BLOCKED with the gate closed", task.Status, task.RejectionRCAGateOpen())
	}
	disposition := task.RejectionRCA.Disposition
	if disposition.RecoveryPath != path || disposition.RestoreMode != models.RejectionRCARestoreMode(path) || disposition.Actor != rejectionRCATestActor || disposition.LifecycleVersion == 0 {
		f.t.Fatalf("disposition = %+v", disposition)
	}
}

// restore takes the unblock form the recorded restore mode authorizes and
// leaves the task executing under the doer.
func (f *gateLoopFixture) restore(mode string) {
	f.t.Helper()
	switch mode {
	case models.RestoreModeAssign:
		result, err := UnblockTask(f.root, f.taskID, f.role.doer, "restore authorized by the disposition", rejectionRCATestActor)
		if err != nil {
			f.t.Fatalf("UnblockTask(--assign-to) error: %v", err)
		}
		if result.ToStatus != f.status("executing", f.resolver.ExecutingStatus) {
			f.t.Fatalf("assign restore ToStatus = %s, want the executing status", result.ToStatus)
		}
	case models.RestoreModeClaimable:
		result, err := UnblockTaskWithOptions(f.root, f.taskID, "restore authorized by the disposition", rejectionRCATestActor, UnblockTaskOptions{})
		if err != nil {
			f.t.Fatalf("UnblockTaskWithOptions() error: %v", err)
		}
		if result.ToStatus != f.status("initial", f.resolver.InitialStatus) {
			f.t.Fatalf("claimable restore ToStatus = %s, want the initial status", result.ToStatus)
		}
	default:
		f.t.Fatalf("restore mode %q has no unblock form", mode)
	}
	if task := f.task(); task.RejectionRCA == nil || task.RejectionRCAGateOpen() {
		f.t.Fatalf("record after unblock = %+v, want it retained with the gate closed", task.RejectionRCA)
	}
}

// approve is the post-restore convergence: one more submission that the
// reviewer approves.
func (f *gateLoopFixture) approve() {
	f.t.Helper()
	sha := f.commit()
	f.submit(sha)
	f.review()
	result := f.verdict("APPROVED", "")
	if result.Outcome != models.LifecycleCompleted || result.EscalatedToBlocked {
		f.t.Fatalf("approval result = %+v", result)
	}
	task := f.task()
	if task.Status != f.status("approved", f.resolver.ApprovedStatus) || len(task.Approvals) != 1 || task.ReviewCommit == nil || *task.ReviewCommit != sha {
		f.t.Fatalf("after approval: status=%s approvals=%d review_commit=%v, want approved on %s", task.Status, len(task.Approvals), task.ReviewCommit, sha)
	}
}

// TestRejectionRCAGateEndToEnd drives the whole loop for every recovery
// category over an implementation task and a planning task: real rejections up
// to the threshold, the gate, a mixed-cause RCA from a request file, the
// disposition, the authorized restore (or supersession for rescope) and a
// subsequent successful submission and approval.
func TestRejectionRCAGateEndToEnd(t *testing.T) {
	coding, planning := gateVerdictRoles[0], gateVerdictRoles[1]
	threshold := models.DefaultHighChurnRejectionThreshold

	rows := []struct {
		name string
		role gateVerdictRole
		path string
		// iterationAfterRestore is the product iteration once the task is
		// executing again: unchanged for an assign restore, one more for a
		// claimable restore whose next claim iterates.
		iterationAfterRestore int
	}{
		{name: "coding task product_defect -> implementation_correction", role: coding, path: models.RecoveryImplementationCorrection, iterationAfterRestore: threshold + 1},
		{name: "coding task capability_reroute", role: coding, path: models.RecoveryCapabilityReroute, iterationAfterRestore: threshold},
		{name: "planning task lifecycle_repair", role: planning, path: models.RecoveryLifecycleRepair, iterationAfterRestore: threshold},
		{name: "planning task human_override", role: planning, path: models.RecoveryHumanOverride, iterationAfterRestore: threshold + 1},
		{name: "planning task product_defect -> implementation_correction", role: planning, path: models.RecoveryImplementationCorrection, iterationAfterRestore: threshold + 1},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			fixture := newGateLoopFixture(t, row.role, nil)
			fixture.rejectUntilGated()
			fixture.recordFromFile(mixedCauseRequest())
			fixture.resume(row.path)
			exempt := models.RejectionRCARestoreMode(row.path) == models.RestoreModeAssign
			if task := fixture.task(); task.RejectionRCA.Disposition.IterationExempt != exempt || task.Iteration != threshold {
				t.Fatalf("after resume: iteration_exempt=%v iteration=%d, want %v/%d", task.RejectionRCA.Disposition.IterationExempt, task.Iteration, exempt, threshold)
			}

			fixture.restore(models.RejectionRCARestoreMode(row.path))
			if !exempt {
				fixture.claim()
			}
			if task := fixture.task(); task.Iteration != row.iterationAfterRestore {
				t.Fatalf("iteration after restore = %d, want %d", task.Iteration, row.iterationAfterRestore)
			}

			fixture.approve()
			task := fixture.task()
			if task.Iteration != row.iterationAfterRestore {
				t.Fatalf("iteration after approval = %d, want %d — approval must not iterate", task.Iteration, row.iterationAfterRestore)
			}
			if exempt && task.Iteration != threshold {
				t.Fatalf("%s consumed a product iteration: %d, want %d across the whole loop", row.path, task.Iteration, threshold)
			}
			if got := len(historyEntries(task, models.TaskEventBlocked)); got != 1 {
				t.Fatalf("blocked entries = %d, want the single gate escalation", got)
			}
			if got := len(historyEntries(task, models.TaskEventRejectionRCAResumed)); got != 1 {
				t.Fatalf("rejection_rca_resumed entries = %d, want 1", got)
			}
		})
	}

	t.Run("coding task rescope ends at supersession", func(t *testing.T) {
		fixture := newGateLoopFixture(t, coding, nil)
		fixture.rejectUntilGated()
		fixture.recordFromFile(mixedCauseRequest())
		fixture.resume(models.RecoveryRescope)

		// No unblock form restores a rescoped task; supersession is the exit.
		_, err := UnblockTaskWithOptions(fixture.root, fixture.taskID, "rescoped", rejectionRCATestActor, UnblockTaskOptions{})
		if outcome := requirePrecondition(t, err); outcome.SafeAction != "stop" {
			t.Fatalf("unblock safe_action = %s, want stop", outcome.SafeAction)
		}
		_, err = UnblockTask(fixture.root, fixture.taskID, coding.doer, "rescoped", rejectionRCATestActor)
		requirePrecondition(t, err)

		result, err := SupersedeTask(fixture.root, fixture.taskID, []string{fixture.taskID + "-split-a", fixture.taskID + "-split-b"}, "split after the RCA", rejectionRCATestActor)
		if err != nil {
			t.Fatalf("SupersedeTask() error: %v", err)
		}
		if result.OriginalStatus != models.TaskStatusBlocked {
			t.Fatalf("OriginalStatus = %s, want BLOCKED", result.OriginalStatus)
		}
		task := fixture.task()
		if task.Status != models.TaskStatusSuperseded || len(task.SupersededBy) != 2 || task.Iteration != threshold {
			t.Fatalf("after supersession: status=%s superseded_by=%v iteration=%d", task.Status, task.SupersededBy, task.Iteration)
		}
		if task.RejectionRCA == nil || task.RejectionRCA.Disposition == nil || task.RejectionRCA.Disposition.RecoveryPath != models.RecoveryRescope {
			t.Fatalf("rescope disposition lost at supersession: %+v", task.RejectionRCA)
		}
	})
}

// TestRejectionRCAGateConcurrentResubmission proves that a gated task refuses
// the two normal continuations a doer would attempt while the gate is open.
//
// Barrier "bothStartAtGate": both callers wait on one channel closed after each
// goroutine is scheduled, so the claim and the resubmission run against the
// same observed BLOCKED task instead of one following the other.
//
// Observation: both operations return a refusal produced by the state machine
// — the claim names the non-claimable status, the submission reports
// ALREADY_TRANSITIONED/stop — and state.yaml is byte-identical afterwards, so
// neither wrote a receipt, a history entry or an ownership change.
func TestRejectionRCAGateConcurrentResubmission(t *testing.T) {
	fixture := newGateLoopFixture(t, gateVerdictRoles[0], nil)
	fixture.rejectUntilGated()
	// The doer "fixes" and resubmits a fresh commit rather than replaying the
	// gating one, so the submission is a new intent, not a receipt replay.
	sha := fixture.commit()
	before := fixture.stateBytes()

	start := make(chan struct{})
	var wg sync.WaitGroup
	var claimErr, submitErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, claimErr = ClaimTask(fixture.root, fixture.taskID, fixture.role.doer)
	}()
	go func() {
		defer wg.Done()
		<-start
		_, submitErr = SubmitForReview(fixture.root, fixture.taskID, sha, fixture.role.doer)
	}()
	close(start)
	wg.Wait()

	var claimPrecondition *PreconditionError
	if !errors.As(claimErr, &claimPrecondition) || !strings.Contains(claimErr.Error(), string(models.TaskStatusBlocked)) {
		t.Fatalf("concurrent claim = %v, want a precondition refusal naming BLOCKED", claimErr)
	}
	var submitLifecycle *LifecycleError
	if !errors.As(submitErr, &submitLifecycle) || submitLifecycle.Outcome.Outcome != models.LifecycleAlreadyTransitioned || submitLifecycle.Outcome.SafeAction != "stop" {
		t.Fatalf("concurrent submit-for-review = %v, want ALREADY_TRANSITIONED/stop", submitErr)
	}
	if after := fixture.stateBytes(); after != before {
		t.Fatal("a refused claim or resubmission changed state.yaml")
	}
	task := fixture.task()
	if task.Status != models.TaskStatusBlocked || !task.RejectionRCAGateOpen() || task.AssignedTo != nil {
		t.Fatalf("after refusals: status=%s open=%v assigned_to=%v", task.Status, task.RejectionRCAGateOpen(), task.AssignedTo)
	}
}

// TestRejectionRCAGateBackstopsRemainActive proves the gate does not replace
// the existing limits: after a resume the review budget still escalates at
// its own cap, and planning_review_churn still fires for a planning task.
func TestRejectionRCAGateBackstopsRemainActive(t *testing.T) {
	t.Run("review budget escalates at its cap after a resume", func(t *testing.T) {
		// Attempt 2 makes the budget cap a BLOCKED escalation rather than a
		// new attempt, so the reason is observable on the task.
		fixture := newGateLoopFixture(t, gateVerdictRoles[0], func(task *models.Task) { task.Attempt = 2 })
		fixture.rejectUntilGated()
		fixture.recordFromFile(mixedCauseRequest())
		fixture.resume(models.RecoveryImplementationCorrection)
		fixture.restore(models.RestoreModeClaimable)
		if task := fixture.task(); task.ReviewCyclesCurrent != models.DefaultHighChurnRejectionThreshold {
			t.Fatalf("review_cycles_current = %d, want the budget untouched by an implementation_correction resume", task.ReviewCyclesCurrent)
		}

		result := fixture.iterate("REJECTED", "fifth rejection")
		state, err := db.New(fixture.statePath).Read()
		if err != nil {
			t.Fatal(err)
		}
		limit := effectiveReviewCycleLimit(state.Config)
		wantReason := reviewBudgetExhaustedReason(limit, limit)
		if !result.EscalatedToBlocked || result.RejectionRCAGated || result.BlockedReason != wantReason {
			t.Fatalf("result = %+v, want the review-budget escalation %q", result, wantReason)
		}
		task := state.FindTask(fixture.taskID)
		if task.Status != models.TaskStatusBlocked || task.BlockedReason == nil || *task.BlockedReason != wantReason {
			t.Fatalf("task = %s/%v, want BLOCKED by the review budget", task.Status, task.BlockedReason)
		}
		if task.RejectionRCAGateOpen() || task.RejectionRCA.Disposition == nil {
			t.Fatalf("budget escalation re-opened or cleared the gate: %+v", task.RejectionRCA)
		}
	})

	t.Run("planning_review_churn still triggers for a resumed planning task", func(t *testing.T) {
		fixture := newGateLoopFixture(t, gateVerdictRoles[1], nil)
		fixture.rejectUntilGated()
		fixture.recordFromFile(mixedCauseRequest())
		fixture.resume(models.RecoveryLifecycleRepair)

		result, err := Analyze(fixture.root)
		if err != nil {
			t.Fatalf("Analyze() error: %v", err)
		}
		if !result.Triggered || result.Pattern != "planning_review_churn" || !strings.Contains(result.Evidence, fixture.taskID) {
			t.Fatalf("analyze = %+v, want planning_review_churn on %s", result, fixture.taskID)
		}
		if result.RejectionRCA == nil || result.RejectionRCA.GateCycles != 1 || result.RejectionRCA.RecoveryPaths[models.RecoveryLifecycleRepair] != 1 {
			t.Fatalf("rejection_rca telemetry = %+v, want one gate cycle resumed by lifecycle_repair", result.RejectionRCA)
		}
	})
}
