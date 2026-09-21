package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

func TestReplaceTaskTextResult(t *testing.T) {
	for _, tc := range []struct {
		outcome string
		action  string
		fresh   bool
	}{
		{models.LifecycleCompleted, "continue", true},
		{models.LifecycleAlreadyCompleted, "stop", false},
	} {
		t.Run(tc.outcome, func(t *testing.T) {
			result := &ops.ReplaceTaskResult{
				LifecycleOutcome: ops.NewLifecycleOutcome("replace-task", nil, tc.outcome, tc.action, "none"),
				SourceTaskID:     "source", ReplacementTaskID: "replacement",
			}
			output := captureStdout(t, func() {
				if err := printReplaceTaskResult(result, nil); err != nil {
					t.Fatal(err)
				}
			})
			for _, want := range []string{"outcome: " + tc.outcome, "safe_action: " + tc.action} {
				if !strings.Contains(output, want) {
					t.Errorf("output %q lacks %q", output, want)
				}
			}
			if strings.Contains(output, "Replaced task source with replacement") != tc.fresh {
				t.Fatalf("fresh effect message incorrect: %s", output)
			}
		})
	}
}

func TestReplaceTaskTextErrorPreservesPolicy(t *testing.T) {
	cause := ops.NewLifecycleConflictError("replace-task", nil, []models.FieldDiagnostic{{
		SchemaVersion: 1, Field: "replacement.id", Constraint: "unique ID",
		ValueClass: models.FieldValueClassConflict, SafeAction: models.FieldDiagnosticRequery,
	}}, errors.New("replacement ID already exists"))
	output := captureStdout(t, func() {
		err := printReplaceTaskResult(nil, cause)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "replace task:") {
			t.Fatalf("lost error identity/context: %v", err)
		}
	})
	if output != "" {
		t.Fatalf("error adapter must leave failure rendering to CLI: %q", output)
	}
}
