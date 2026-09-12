package commands

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func TestValidationReadinessInspectionRedactsAuthorityWithoutMutation(t *testing.T) {
	record := models.ValidationReadiness{
		Generation: "test-private-registration", TaskID: "task-1",
		Commit: "worktree-sha", ReviewCommit: "review-sha", IntegrationSHA: "integration-sha",
		Digest: "contract-digest", Fingerprint: "environment-fingerprint",
		CheckedAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC), Method: "direct",
		Result: "failed", Code: "environment_missing", CommandIndex: 2, CheckIndex: 3, Variable: "REQUIRED_URL",
	}
	state := &models.State{ValidationReadiness: map[string]map[string]models.ValidationReadiness{
		"coder-1": {record.TaskID: record}, "coder-2": {record.TaskID: record}, "empty": nil,
	}}
	before, err := yaml.Marshal(state.ValidationReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), record.Generation) {
		t.Fatal("persisted YAML must retain registration authority")
	}
	var persisted map[string]map[string]models.ValidationReadiness
	if err := yaml.Unmarshal(before, &persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(persisted["coder-1"][record.TaskID], record) {
		t.Fatal("YAML persistence changed readiness evidence")
	}
	raw, err := getField(state, "validation_readiness")
	if err != nil {
		t.Fatal(err)
	}
	want := record
	want.Generation = ""
	redacted := raw.(map[string]map[string]models.ValidationReadiness)
	for _, agentID := range []string{"coder-1", "coder-2"} {
		if !reflect.DeepEqual(redacted[agentID][record.TaskID], want) {
			t.Fatal("inspection changed diagnostics beyond registration authority")
		}
	}
	if redacted["empty"] != nil {
		t.Fatal("inspection changed an empty observation map")
	}
	for name, value := range map[string]any{
		"whole-field": raw,
		"single":      normalizeFieldValue(reflect.ValueOf(&record)),
	} {
		for _, format := range []string{"json", "yaml", "value"} {
			t.Run(name+"/"+format, func(t *testing.T) {
				output, err := formatOutput(value, format)
				if err != nil {
					t.Fatal(err)
				}
				for _, forbidden := range []string{record.Generation, "generation"} {
					if strings.Contains(output, forbidden) {
						t.Fatal("inspection exposed persistence-only authority")
					}
				}
				for _, retained := range []string{record.Code, record.Fingerprint, record.Digest, record.Variable} {
					if !strings.Contains(output, retained) {
						t.Fatalf("inspection lost diagnostic %q", retained)
					}
				}
			})
		}
	}
	publicJSON, err := json.Marshal(state.ValidationReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(publicJSON), record.Generation) || strings.Contains(string(publicJSON), "generation") {
		t.Fatal("JSON serialization exposed registration authority")
	}
	after, err := yaml.Marshal(state.ValidationReadiness)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("inspection mutated persisted authority or observation data")
	}
	if _, err := resolveFieldByYAMLPath(reflect.ValueOf(record), []string{"generation"}, "readiness"); err == nil {
		t.Fatal("direct inspection of registration authority must be denied")
	}
	for _, path := range []string{"validation_readiness.coder-1.task-1.generation", "validation_readiness.coder-1"} {
		if _, err := getField(state, path); err == nil {
			t.Fatal("unsupported nested readiness access must not bypass redaction")
		}
	}
}
