package commands_test

import (
	"testing"

	"github.com/liza-mas/liza/internal/commands"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
)

// The builder is the agent's preflight seam: what it returns must be exactly
// what the set-task-output schema validates at the mutation boundary.
func TestSetTaskOutputPayload(t *testing.T) {
	t.Parallel()

	entry := models.OutputEntry{
		Desc:     "Implement storage",
		DoneWhen: "Storage tests pass",
		Scope:    "internal/storage",
		SpecRef:  "specs/storage.md#Storage",
	}

	result, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload([]models.OutputEntry{entry}))
	if err != nil {
		t.Fatalf("a valid manifest was rejected: %v", err)
	}
	if result.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", result.SchemaVersion)
	}

	// Clearing a task's output is an empty manifest, not an absent one.
	if _, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload(nil)); err != nil {
		t.Fatalf("an empty manifest was rejected: %v", err)
	}

	entry.SpecRef = ""
	if _, err := commands.ValidatePayload(payloadschema.SetTaskOutputOperation, commands.SetTaskOutputPayload([]models.OutputEntry{entry})); err == nil {
		t.Fatal("a manifest without spec_ref was accepted")
	}
}
