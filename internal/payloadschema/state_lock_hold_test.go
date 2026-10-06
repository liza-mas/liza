package payloadschema

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestStateLockHoldPayloadSchema(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"mark-blocked", "assess-blocked"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name  string
				extra map[string]any
				valid bool
			}{
				{name: "typed infrastructure hold", extra: map[string]any{"state_lock_timeout": true}, valid: true},
				{name: "legacy payload", extra: map[string]any{}, valid: true},
				{name: "boolean required", extra: map[string]any{"state_lock_timeout": "true"}},
				{name: "human incompatible", extra: map[string]any{"state_lock_timeout": true, "human_action": "Inspect and signal done"}},
				{name: "repair incompatible", extra: map[string]any{"state_lock_timeout": true, "repair_request": &models.RepairRequest{Operation: "repair", Target: "target", Command: "repair", Evidence: []string{"error=broken"}, Validation: []string{"verify"}}}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					payload := map[string]any{"task_id": "target", "reason": "state lock timeout", "questions": []string{"Resume later?"}}
					for key, value := range tc.extra {
						payload[key] = value
					}
					_, diagnostics, err := Validate(operation, payload)
					if err != nil {
						t.Fatal(err)
					}
					if (len(diagnostics) == 0) != tc.valid {
						t.Fatalf("valid=%t diagnostics=%+v", tc.valid, diagnostics)
					}
				})
			}
		})
	}
}
