package models_test

import (
	"fmt"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"gopkg.in/yaml.v3"
)

func TestProviderDependenciesHoldConsumerUntilSelectedPlanMerged(t *testing.T) {
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(cfg)
	for _, tc := range []struct {
		name        string
		declaration string
		expanded    bool
		childStatus models.TaskStatus
		claimable   bool
	}{
		{name: "unborn plan", declaration: "provider_dependencies: [{provider_task: provider, transition: architecture-to-code-plan, outputs: [0]}]"},
		{name: "pending plan", declaration: "provider_dependencies: [{provider_task: provider, transition: architecture-to-code-plan, outputs: [0]}]", expanded: true, childStatus: models.TaskStatus("DRAFT_CODING_PLAN")},
		{name: "approved but unmerged plan", declaration: "provider_dependencies: [{provider_task: provider, transition: architecture-to-code-plan, outputs: [0]}]", expanded: true, childStatus: models.TaskStatus("CODING_PLAN_APPROVED")},
		{name: "merged selected plan", declaration: "provider_dependencies: [{provider_task: provider, transition: architecture-to-code-plan, outputs: [0]}]", expanded: true, childStatus: models.TaskStatusMerged, claimable: true},
		{name: "legacy architecture dependency", claimable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Raw YAML keeps this reproduction compile-clean on the source that
			// predates the new scheduling field. It exercises actual readiness.
			var state models.State
			err := yaml.Unmarshal([]byte(fmt.Sprintf(`tasks:
  - id: provider
    role_pair: architecture-pair
    status: MERGED
    transitions_executed: {architecture-to-code-plan: %t}
    output:
      - {desc: provider plan, done_when: plan reviewed, scope: provider, spec_ref: specs/provider.md}
  - id: consumer
    role_pair: architecture-pair
    status: DRAFT_ARCHITECTURE
    depends_on: [provider]
    scope: Requires the provider code plan to be approved before work starts.
    %s
`, tc.expanded, tc.declaration)), &state)
			if err != nil {
				t.Fatal(err)
			}
			if tc.childStatus != "" {
				state.Tasks = append(state.Tasks, models.Task{
					ID: "provider-cp-0", RolePair: "code-planning-pair",
					Status: tc.childStatus, ParentTasks: []string{"provider"},
				})
			}
			consumer := state.FindTask("consumer")
			if got := consumer.IsClaimable("architect", state.Tasks, resolver); got != tc.claimable {
				t.Fatalf("consumer claimable = %t, want %t (provider expansion=%t, selected child=%s)", got, tc.claimable, tc.expanded, tc.childStatus)
			}
		})
	}
}
