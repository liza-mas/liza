package pipeline

import (
	"slices"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

func TestReviewedCodingAllocationRoutesAreExclusive(t *testing.T) {
	cfg, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	r := NewResolver(cfg)
	for _, tc := range []struct{ source, legacy, direct, legacyTarget string }{
		{"architecture-pair", "architecture-to-code-plan", "architecture-to-coding", "code-planning-pair"},
		{"architecture-main-pair", "arch-decompose", "architecture-main-to-coding", "architecture-pair"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			marked := []models.OutputEntry{{CodingAllocation: true}}
			if !r.TransitionApplies(tc.legacy, nil) || r.TransitionApplies(tc.direct, nil) || r.TransitionApplies(tc.legacy, marked) || !r.TransitionApplies(tc.direct, marked) {
				t.Fatal("allocation routes are not exclusive")
			}
			for _, selection := range []struct {
				output []models.OutputEntry
				want   string
			}{{nil, tc.legacyTarget}, {marked, "coding-pair"}} {
				consumers, err := r.OutputConsumerRolePairsForOutput(tc.source, selection.output)
				if err != nil || !slices.Equal(consumers, []string{selection.want}) {
					t.Fatalf("consumers %v: %v", consumers, err)
				}
			}
			direct, _ := r.Transition(tc.direct)
			if direct.Trigger != "manual" || direct.Cardinality != "per-subtask" {
				t.Fatal("direct allocation bypasses manual handoff")
			}
		})
	}
	legacy := NewResolver(loadPhase2Config(t))
	if consumers, err := legacy.OutputConsumerRolePairsForOutput("architecture-pair", []models.OutputEntry{{CodingAllocation: true}}); err == nil && len(consumers) != 0 {
		t.Fatal("legacy pipeline inferred a direct route")
	}
}

func TestCodingAllocationConfigRejectsAutomaticBypass(t *testing.T) {
	cfg, err := LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cfg.Pipeline.PipelineTransitions {
		if cfg.Pipeline.PipelineTransitions[i].When == "coding-allocation" {
			cfg.Pipeline.PipelineTransitions[i].Trigger = "auto"
			break
		}
	}
	if err := validate(cfg); err == nil {
		t.Fatal("automatic direct coding route accepted")
	}
}
