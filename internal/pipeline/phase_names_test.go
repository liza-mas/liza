package pipeline

import (
	"slices"
	"sort"
	"testing"

	"github.com/liza-mas/liza/internal/models"
)

// The phase spellings are literals on purpose: they are the pipeline YAML
// vocabulary, independent of the StateCategory constants that name them.
var phaseSpellings = []string{
	"initial", "executing", "submitted", "reviewing", "approved",
	"rejected", "partially-approved", "reviewing-2", "clean",
}

func TestValidPhasesAreTheNineStateCategories(t *testing.T) {
	var got []string
	for phase, ok := range validPhases {
		if ok {
			got = append(got, phase)
		}
	}
	sort.Strings(got)
	want := slices.Sorted(slices.Values(phaseSpellings))
	if !slices.Equal(got, want) {
		t.Fatalf("validPhases = %v, want %v", got, want)
	}
}

func TestResolvePhaseMapsEachSpellingToItsDeclaredState(t *testing.T) {
	states := RolePairStates{
		Initial: "S_INITIAL", Executing: "S_EXECUTING", Submitted: "S_SUBMITTED",
		Reviewing: "S_REVIEWING", Approved: "S_APPROVED", Rejected: "S_REJECTED",
		PartiallyApproved: "S_PARTIAL", Reviewing2: "S_REVIEWING_2", Clean: "S_CLEAN",
	}
	r := NewResolver(&PipelineConfig{Pipeline: Pipeline{
		RolePairs: map[string]RolePairDef{"pair": {States: states}},
	}})
	want := map[string]models.TaskStatus{
		"initial": "S_INITIAL", "executing": "S_EXECUTING", "submitted": "S_SUBMITTED",
		"reviewing": "S_REVIEWING", "approved": "S_APPROVED", "rejected": "S_REJECTED",
		"partially-approved": "S_PARTIAL", "reviewing-2": "S_REVIEWING_2", "clean": "S_CLEAN",
	}
	for _, phase := range phaseSpellings {
		if got := r.resolvePhase("pair", phase); got != want[phase] {
			t.Errorf("resolvePhase(pair, %q) = %q, want %q", phase, got, want[phase])
		}
	}
	if got := r.resolvePhase("pair", "unknown-phase"); got != "" {
		t.Errorf("resolvePhase(pair, unknown-phase) = %q, want empty", got)
	}
}
