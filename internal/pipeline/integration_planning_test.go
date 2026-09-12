package pipeline

import (
	"reflect"
	"testing"
)

func TestIntegrationPlanningFollowsUpstreamTopology(t *testing.T) {
	cfg := &PipelineConfig{Pipeline: Pipeline{
		RolePairs: map[string]RolePairDef{
			"upstream": {}, "middle": {}, "code-planning-pair": {}, "coding-pair": {},
			"integration-pair": {}, "slice-integration-pair": {}, "unrelated": {}, "sink": {},
		},
		SubPipelines: map[string]SubPipeline{"custom": {Transitions: []TransitionDef{
			{Name: "custom-fan-out", From: "upstream.approved", To: "middle.initial", Cardinality: "per-subtask"},
			{Name: "implement", From: "code-planning-pair.approved", To: "coding-pair.initial", Cardinality: "per-subtask"},
			{Name: "repair", From: "integration-pair.approved", To: "coding-pair.initial", Cardinality: "per-subtask"},
			{Name: "slice-repair", From: "slice-integration-pair.approved", To: "coding-pair.initial", Cardinality: "per-subtask"},
			{Name: "unrelated-output", From: "unrelated.approved", To: "sink.initial", Cardinality: "one-to-one"},
		}}},
		PipelineTransitions: []TransitionDef{
			{Name: "cross-pipeline", From: "custom.middle.approved", To: "coding.code-planning-pair.initial", Cardinality: "many-to-one"},
		},
	}}
	capability, err := NewResolver(cfg).SlicedIntegrationCapability()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]TransitionDef{
		"upstream":           {cfg.Pipeline.SubPipelines["custom"].Transitions[0]},
		"middle":             {cfg.Pipeline.PipelineTransitions[0]},
		"code-planning-pair": {cfg.Pipeline.SubPipelines["custom"].Transitions[1]},
	}
	if !reflect.DeepEqual(capability.PreIntegrationPlanningTransitions, want) {
		t.Fatalf("upstream topology = %+v, want %+v", capability.PreIntegrationPlanningTransitions, want)
	}
}
