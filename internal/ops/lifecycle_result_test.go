package ops

import (
	"encoding/json"
	"errors"
	"fmt"
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
