package payloadschema_test

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/payloadschema"
)

func TestProviderDependencyPayloadSchemas(t *testing.T) {
	valid := models.ProviderDependency{ProviderTask: "provider", Transition: "architecture-to-code-plan", Outputs: []int{0, 2}}
	for _, tc := range []struct {
		name  string
		deps  []models.ProviderDependency
		valid bool
	}{
		{name: "omitted", valid: true},
		{name: "unborn output selection", deps: []models.ProviderDependency{valid}, valid: true},
		{name: "unsafe provider", deps: []models.ProviderDependency{{ProviderTask: "../provider", Transition: valid.Transition, Outputs: []int{0}}}},
		{name: "missing transition", deps: []models.ProviderDependency{{ProviderTask: valid.ProviderTask, Outputs: []int{0}}}},
		{name: "empty outputs", deps: []models.ProviderDependency{{ProviderTask: valid.ProviderTask, Transition: valid.Transition}}},
		{name: "negative output", deps: []models.ProviderDependency{{ProviderTask: valid.ProviderTask, Transition: valid.Transition, Outputs: []int{-1}}}},
		{name: "duplicate output", deps: []models.ProviderDependency{{ProviderTask: valid.ProviderTask, Transition: valid.Transition, Outputs: []int{0, 0}}}},
		{name: "duplicate declaration", deps: []models.ProviderDependency{valid, valid}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := validEntry()
			entry.ProviderDependencies = tc.deps
			input := map[string]any{
				"id": "consumer", "role_pair": "architecture-pair", "desc": entry.Desc,
				"done": entry.DoneWhen, "scope": entry.Scope, "spec": entry.SpecRef,
				"priority": 1, "provider_dependencies": tc.deps,
			}
			for _, operation := range []struct {
				name    string
				payload any
			}{
				{payloadschema.SetTaskOutputOperation, payloadschema.SetTaskOutputPayload([]models.OutputEntry{entry})},
				{"add-task", input},
			} {
				_, diagnostics, err := payloadschema.Validate(operation.name, operation.payload)
				if err != nil {
					t.Fatal(err)
				}
				if got := len(diagnostics) == 0; got != tc.valid {
					t.Fatalf("%s valid=%t, want %t; diagnostics=%v", operation.name, got, tc.valid, diagnostics)
				}
			}
		})
	}
}
