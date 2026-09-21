package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
)

func TestLifecycleErrorPreservesCauseAndFieldDiagnostics(t *testing.T) {
	t.Parallel()
	cause := &PreconditionError{Reason: "missing validation plan", Details: map[string]any{"field": "validation_plan", "safe_action": "retry", "current_assignee": "unverified-owner"}}
	err := WrapLifecycleError("submit-for-review", nil, fmt.Errorf("submission: %w", cause), models.LifecycleInvalidInput, "correct_input", "none")
	var wrapped *LifecycleError
	var original *PreconditionError
	if !errors.As(err, &wrapped) || !errors.As(err, &original) || original != cause {
		t.Fatal("error cause lost")
	}
	details := wrapped.SafeDetails()
	if details["field"] != "validation_plan" || details["safe_action"] != "correct_input" {
		t.Fatalf("wrong diagnostics: %v", details)
	}
	if _, exists := details["current_assignee"]; exists {
		t.Fatal("cause supplied unverified current ownership")
	}
	if cause.Details["safe_action"] != "retry" {
		t.Fatal("wrapping modified the original diagnostic map")
	}
	if again := WrapLifecycleError("other", nil, err, models.LifecycleRetryable, "retry", "none"); again != err {
		t.Fatal("explicit lifecycle outcome was replaced")
	}
}

func TestWrapLifecycleErrorRespectsEffectBoundary(t *testing.T) {
	t.Parallel()
	for _, effects := range []string{"none", "unknown", "committed"} {
		t.Run(effects, func(t *testing.T) {
			err := WrapLifecycleError("wt-merge", nil, filelock.NewLockTimeout(errors.New("busy")), models.LifecycleStateChanged, "requery", effects)
			var lifecycle *LifecycleError
			if !errors.As(err, &lifecycle) {
				t.Fatal("missing lifecycle result")
			}
			wantOutcome, wantAction := models.LifecycleStateChanged, "requery"
			if effects == "none" {
				wantOutcome, wantAction = models.LifecycleRetryable, "retry"
			}
			if lifecycle.Outcome.Outcome != wantOutcome || lifecycle.Outcome.SafeAction != wantAction || lifecycle.Outcome.Effects != effects {
				t.Fatalf("unsafe lock recovery result: %+v", lifecycle.Outcome)
			}
		})
	}
}

func TestWrapLifecycleAuthorityDoesNotExposeTaskOrGenerations(t *testing.T) {
	t.Parallel()
	owner := "private-owner"
	task := &models.Task{ID: "private-task", AssignedTo: &owner, Status: models.TaskStatusImplementing}
	cause := &AgentAuthorityError{AgentID: "coder-1", LosingGeneration: "losing-generation", CurrentGeneration: "current-generation"}
	err := WrapLifecycleError("claim-task", task, cause, models.LifecycleInvalidInput, "correct_input", "unknown")
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) {
		t.Fatal("missing lifecycle error")
	}
	if lifecycle.Outcome.Outcome != models.LifecycleStaleCaller || lifecycle.Outcome.SafeAction != "stop" || lifecycle.Outcome.Effects != "unknown" {
		t.Fatalf("wrong stale result: %+v", lifecycle.Outcome)
	}
	data, marshalErr := json.Marshal(lifecycle.SafeDetails())
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, forbidden := range []string{owner, task.ID, cause.LosingGeneration, cause.CurrentGeneration} {
		if strings.Contains(string(data), forbidden) || strings.Contains(err.Error(), forbidden) {
			t.Fatal("authority rejection disclosed protected state")
		}
	}
}

func TestLifecycleErrorPreservesOuterSafeContext(t *testing.T) {
	t.Parallel()
	private := errors.New("private underlying diagnostic")
	original := WrapLifecycleError("retarget-dependency", nil, private, models.LifecycleInvalidInput, "correct_input", "none")
	context := &OperationalError{Message: "candidate contains a dependency cycle", Phase: "candidate-validation", Details: map[string]any{"cycle_path": []string{"a", "b", "a"}}, Err: original}
	err := WrapLifecycleError("retarget-dependency", nil, context, models.LifecycleStateChanged, "requery", "unknown")
	var lifecycle *LifecycleError
	if !errors.As(err, &lifecycle) || !errors.Is(err, private) {
		t.Fatal("wrapped cause or recovery policy was lost")
	}
	if lifecycle.Outcome.Outcome != models.LifecycleInvalidInput || lifecycle.Outcome.SafeAction != "correct_input" {
		t.Fatalf("explicit policy was changed: %+v", lifecycle.Outcome)
	}
	if !strings.Contains(err.Error(), context.Message) || strings.Contains(err.Error(), private.Error()) {
		t.Fatalf("safe context was lost or private cause exposed: %s", err)
	}
	details := lifecycle.SafeDetails()
	if details["phase"] != context.Phase || details["cycle_path"] == nil {
		t.Fatalf("outer diagnostics lost: %v", details)
	}
}

func TestLifecycleDiagnosticsHelpers(t *testing.T) {
	t.Parallel()
	owner, reviewer := "coder-1", "reviewer-1"
	task := &models.Task{ID: "task-1", Status: models.TaskStatusImplementing, AssignedTo: &owner, ReviewingBy: &reviewer}
	diagnostics := []models.FieldDiagnostic{
		{SchemaVersion: 3, Field: "/replacement/id", Constraint: "unique", ValueClass: models.FieldValueClassConflict, SafeAction: "bogus"},
		{SchemaVersion: 3, Field: strings.Repeat("x", 300), Constraint: "c", ValueClass: models.FieldValueClassOversized, SafeAction: models.FieldDiagnosticRequery},
	}
	wantNormalized := models.NormalizeFieldDiagnostics(diagnostics)

	// GIVEN a no-change outcome for an observed task
	noChange := NewLifecycleNoChangeOutcome("assess-blocked", task)
	if noChange.Outcome != models.LifecycleNoChange || noChange.SafeAction != "stop" || noChange.Effects != "none" {
		t.Fatalf("no-change policy: %+v", noChange)
	}
	if noChange.Changed == nil || *noChange.Changed {
		t.Fatalf("no-change changed = %v", noChange.Changed)
	}
	if noChange.TaskID != task.ID || noChange.TaskStatus != task.Status || noChange.CurrentAssignee != owner ||
		noChange.CurrentReviewer != reviewer || noChange.TransitionID != models.TaskTransitionID(task) {
		t.Fatalf("no-change lost task observation: %+v", noChange)
	}
	if len(noChange.Diagnostics) != 0 {
		t.Fatal("no-change carried diagnostics")
	}

	// GIVEN the two diagnostic-bearing error helpers
	cause := errors.New("private cause")
	for _, tc := range []struct {
		name          string
		err           *LifecycleError
		outcome, want string
	}{
		{"invalid input", NewLifecycleInvalidInputError("validate-payload", task, diagnostics, cause), models.LifecycleInvalidInput, "correct_input"},
		{"conflict", NewLifecycleConflictError("replace-task", task, diagnostics, cause), models.LifecycleConflict, "stop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := tc.err.Outcome
			if o.Outcome != tc.outcome || o.SafeAction != tc.want || o.Effects != "none" {
				t.Fatalf("policy: %+v", o)
			}
			if o.Changed != nil {
				t.Fatalf("failure outcome carried changed=%v", *o.Changed)
			}
			if o.TaskID != task.ID || o.TransitionID != models.TaskTransitionID(task) {
				t.Fatalf("task observation lost: %+v", o)
			}
			if !reflect.DeepEqual(o.Diagnostics, wantNormalized) {
				t.Fatalf("diagnostics not normalized: %+v", o.Diagnostics)
			}
			if o.Diagnostics[0].SafeAction != models.FieldDiagnosticCorrectInput || len(o.Diagnostics[1].Field) != models.LifecycleDiagnosticMaxBytes {
				t.Fatalf("normalization missing: %+v", o.Diagnostics)
			}
			if !errors.Is(tc.err, cause) {
				t.Fatal("cause unreachable")
			}
			// THEN wrapping passes the typed outcome and its diagnostics through unchanged
			wrapped := WrapLifecycleError("outer", nil, tc.err, models.LifecycleStateChanged, "requery", "unknown")
			var again *LifecycleError
			if !errors.As(wrapped, &again) || !reflect.DeepEqual(again.Outcome, o) {
				t.Fatalf("wrap altered outcome: %+v", wrapped)
			}
			if got := NewLifecycleInvalidInputError("op", nil, nil, nil); got.Outcome.Diagnostics != nil || got.Outcome.TaskStatus != "UNKNOWN" || got.Err != nil {
				t.Fatalf("nil handling: %+v", got)
			}
		})
	}

	// THEN a replay reports changed=false and existing pairs gain only the changed key
	replay := LifecycleReplayOutcome(task, &models.LifecycleReceipt{LifecycleIdentity: models.LifecycleIdentity{Operation: "submit-for-review"}, TransitionID: "old"}, owner)
	if replay.Outcome != models.LifecycleAlreadyCompleted || replay.Changed == nil || *replay.Changed {
		t.Fatalf("replay changed = %v: %+v", replay.Changed, replay)
	}
	existing := []struct{ outcome, action, effects string }{
		{models.LifecycleCompleted, "continue", "committed"},
		{models.LifecycleAlreadyTransitioned, "stop", "none"},
		{models.LifecycleStaleCaller, "stop", "none"},
		{models.LifecycleStateChanged, "requery", "unknown"},
		{models.LifecycleRetryable, "retry", "none"},
		{models.LifecycleInvalidInput, "correct_input", "none"},
		{models.LifecycleForbidden, "stop", "none"},
	}
	for _, pair := range existing {
		got := NewLifecycleOutcome("submit-for-review", task, pair.outcome, pair.action, pair.effects)
		want := models.LifecycleOutcome{Operation: "submit-for-review", TaskID: task.ID, Outcome: pair.outcome, SafeAction: pair.action,
			TaskStatus: task.Status, CurrentAssignee: owner, CurrentReviewer: reviewer, TransitionID: models.TaskTransitionID(task), Effects: pair.effects}
		if pair.outcome == models.LifecycleCompleted {
			changed := true
			want.Changed = &changed
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s changed beyond the changed key:\n got %+v\nwant %+v", pair.outcome, got, want)
		}
	}
	if pre := NewLifecycleOutcome("claim-task", nil, models.LifecycleCompleted, "continue", "committed"); pre.Changed == nil || !*pre.Changed {
		t.Fatal("nil-task completion did not derive changed")
	}
}

func TestLifecycleDiagnosticsSafeDetails(t *testing.T) {
	t.Parallel()
	diagnostics := []models.FieldDiagnostic{{SchemaVersion: 1, Field: "/a", Constraint: "required", ValueClass: models.FieldValueClassMissing, SafeAction: models.FieldDiagnosticCorrectInput}}
	cause := &PreconditionError{Reason: "bad payload", Details: map[string]any{"diagnostics": "cause-supplied", "changed": "cause-supplied", "field": "/a"}}

	// GIVEN an invalid-input error whose cause supplies conflicting keys
	err := NewLifecycleInvalidInputError("set-task-output", nil, diagnostics, cause)
	details := err.SafeDetails()
	if !reflect.DeepEqual(details["diagnostics"], diagnostics) {
		t.Fatalf("diagnostics = %#v", details["diagnostics"])
	}
	if _, present := details["changed"]; present {
		t.Fatalf("absent changed rendered from cause: %v", details["changed"])
	}
	if details["field"] != "/a" || details["outcome"] != models.LifecycleInvalidInput {
		t.Fatalf("safe details lost: %v", details)
	}

	// GIVEN a completed outcome carrying changed and no diagnostics
	completed := &LifecycleError{Outcome: NewLifecycleOutcome("wt-merge", nil, models.LifecycleCompleted, "continue", "committed"), Err: cause}
	details = completed.SafeDetails()
	if details["changed"] != true {
		t.Fatalf("changed = %#v", details["changed"])
	}
	if _, present := details["diagnostics"]; present {
		t.Fatal("cause-supplied diagnostics leaked")
	}

	// GIVEN a plain failure with neither field
	plain := WrapLifecycleError("wt-merge", nil, errors.New("x"), models.LifecycleForbidden, "stop", "none")
	var lifecycle *LifecycleError
	if !errors.As(plain, &lifecycle) {
		t.Fatal("missing lifecycle error")
	}
	details = lifecycle.SafeDetails()
	for _, key := range []string{"changed", "diagnostics"} {
		if _, present := details[key]; present {
			t.Fatalf("%s rendered when absent", key)
		}
	}

	// THEN the envelope result (LifecycleResult is what jsonout emits as result) carries result.diagnostics
	data, marshalErr := json.Marshal(map[string]any{"ok": false, "result": err.LifecycleResult()})
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	var envelope struct {
		Result struct {
			Outcome     string                   `json:"outcome"`
			Diagnostics []models.FieldDiagnostic `json:"diagnostics"`
			Changed     *bool                    `json:"changed"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Result.Outcome != models.LifecycleInvalidInput || !reflect.DeepEqual(envelope.Result.Diagnostics, diagnostics) || envelope.Result.Changed != nil {
		t.Fatalf("envelope result: %s", data)
	}
}
