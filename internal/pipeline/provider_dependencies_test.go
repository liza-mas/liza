package pipeline

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestProviderTransitionProjectsConfiguredIdentity(t *testing.T) {
	for _, tc := range []struct{ name, slug, expectedSlug string }{
		{"configured slug", "contract", "contract"},
		{"name fallback", "", "release-contracts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &PipelineConfig{Pipeline: Pipeline{
				SubPipelines: map[string]SubPipeline{"custom": {Transitions: []TransitionDef{
					{Name: "release-contracts", TaskSlug: tc.slug, From: "contract-author.approved", To: "contract-consumer.initial", Cardinality: "per-subtask"},
				}}},
			}}
			pr := NewResolver(cfg)
			dep := models.ProviderDependency{ProviderTask: "provider", Transition: "release-contracts", Outputs: []int{0, 2}}
			td, children, err := models.ProviderDependencyChildren(dep, pr)
			if err != nil {
				t.Fatal(err)
			}
			want := models.ProviderTransition{SourceRolePair: "contract-author", TargetRolePair: "contract-consumer", TaskSlug: tc.expectedSlug, Cardinality: "per-subtask"}
			if td != want || !reflect.DeepEqual(children, []string{"provider-" + tc.expectedSlug + "-0", "provider-" + tc.expectedSlug + "-2"}) {
				t.Fatalf("projected transition/children = %+v %v, want %+v", td, children, want)
			}
		})
	}
}

func TestProviderTransitionHandlesCrossPipelineReferences(t *testing.T) {
	pr := NewResolver(&PipelineConfig{Pipeline: Pipeline{PipelineTransitions: []TransitionDef{
		{Name: "handoff", TaskSlug: "contract", From: "first.provider.approved", To: "second.consumer.initial", Cardinality: "per-subtask"},
	}}})
	got, err := pr.ProviderTransition("handoff")
	if err != nil || got.SourceRolePair != "provider" || got.TargetRolePair != "consumer" || got.TaskSlug != "contract" {
		t.Fatalf("cross-pipeline projection = %+v, %v", got, err)
	}
	if _, err := pr.ProviderTransition("unknown"); err == nil {
		t.Fatal("unknown transition projected successfully")
	}
	var missing *Resolver
	if _, err := missing.ProviderTransition("handoff"); err == nil {
		t.Fatal("nil configuration projected successfully")
	}
}

func TestProviderDependencyChildrenRefusesNonPerSubtaskTransition(t *testing.T) {
	pr := NewResolver(&PipelineConfig{Pipeline: Pipeline{PipelineTransitions: []TransitionDef{
		{Name: "handoff", From: "first.provider.approved", To: "second.consumer.initial", Cardinality: "many-to-one"},
	}}})
	if _, _, err := models.ProviderDependencyChildren(models.ProviderDependency{ProviderTask: "provider", Transition: "handoff", Outputs: []int{0}}, pr); err == nil {
		t.Fatal("provider selection projected a non-per-subtask transition")
	}
}
