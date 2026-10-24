package a5

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func runtimeDirectory() string   { return brand.ProjectDirName }
func generationVariable() string { return brand.EnvPrefix + "_AGENT_GENERATION" }

// Merge only committed fixture objects; arrays deliberately replace wholesale.
// Production models and YAML serialization remain confined to this adapter.
func mergeFixture(t testing.TB, base, delta []byte) []byte {
	t.Helper()
	var b, d map[string]any
	if err := json.Unmarshal(base, &b); err != nil {
		t.Fatal(err)
	}
	if len(delta) > 0 {
		if err := json.Unmarshal(delta, &d); err != nil {
			t.Fatal(err)
		}
	}
	var merge func(map[string]any, map[string]any)
	merge = func(dst, src map[string]any) {
		for k, v := range src {
			if m, ok := v.(map[string]any); ok {
				if old, ok := dst[k].(map[string]any); ok {
					merge(old, m)
					continue
				}
			}
			dst[k] = v
		}
	}
	merge(b, d)
	result, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func writeFixtureState(t testing.TB, f *fixture, input []byte) {
	t.Helper()
	// Fixture keys are the persisted YAML names, which differ from Go's JSON
	// field names. Decode through YAML before emitting the canonical state form.
	var state models.State
	if err := yaml.Unmarshal(input, &state); err != nil {
		t.Fatal(err)
	}
	// Claim operations require a live registered process; the test process is
	// the only deterministic choice. capture replaces the number with a token.
	for id, agent := range state.Agents {
		agent.PID = os.Getpid()
		state.Agents[id] = agent
	}
	b, err := yaml.Marshal(&state)
	if err != nil {
		t.Fatal(err)
	}
	write(t, f.statePath(), b)
	write(t, filepath.Join(f.project, runtimeDirectory(), "pipeline.yaml"), embedded.PipelineConfigContent())
}
