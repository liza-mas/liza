package models_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

func TestProviderDependencyDecodingRejectsNonIntegerSelections(t *testing.T) {
	for _, codec := range []struct {
		name      string
		unmarshal func([]byte, any) error
		manifest  func(string) string
	}{
		{"json", json.Unmarshal, func(value string) string {
			return fmt.Sprintf(`[{"provider_dependencies":[{"provider_task":"provider","transition":"fan-out","outputs":[%s]}]}]`, value)
		}},
		{"yaml", yaml.Unmarshal, func(value string) string {
			return fmt.Sprintf(`- provider_dependencies: [{provider_task: provider, transition: fan-out, outputs: [%s]}]`, value)
		}},
	} {
		for _, scalar := range []string{"null", "0.5", `"0"`, "true", "{}", "[]", "0, null"} {
			t.Run(codec.name+"/"+scalar, func(t *testing.T) {
				var entries []models.OutputEntry
				if err := codec.unmarshal([]byte(codec.manifest(scalar)), &entries); err == nil || !strings.Contains(err.Error(), "int") {
					t.Fatalf("decoded non-integer provider selection %s: %v", scalar, err)
				}
			})
		}
		t.Run(codec.name+"/valid zero", func(t *testing.T) {
			var entries []models.OutputEntry
			if err := codec.unmarshal([]byte(codec.manifest("0")), &entries); err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || len(entries[0].ProviderDependencies) != 1 || len(entries[0].ProviderDependencies[0].Outputs) != 1 || entries[0].ProviderDependencies[0].Outputs[0] != 0 {
				t.Fatalf("valid zero selection changed: %+v", entries)
			}
			if err := models.ValidateProviderDependencies(entries[0].ProviderDependencies); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProviderDependencyDecodingPreservesShapeValidation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		unmarshal func([]byte, any) error
		payload   string
	}{
		{"json missing", json.Unmarshal, `{"provider_task":"provider","transition":"fan-out"}`},
		{"json empty", json.Unmarshal, `{"provider_task":"provider","transition":"fan-out","outputs":[]}`},
		{"json null array", json.Unmarshal, `{"provider_task":"provider","transition":"fan-out","outputs":null}`},
		{"yaml missing", yaml.Unmarshal, `{provider_task: provider, transition: fan-out}`},
		{"yaml empty", yaml.Unmarshal, `{provider_task: provider, transition: fan-out, outputs: []}`},
		{"yaml null array", yaml.Unmarshal, `{provider_task: provider, transition: fan-out, outputs: null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dep models.ProviderDependency
			if err := tc.unmarshal([]byte(tc.payload), &dep); err != nil {
				t.Fatalf("shape error moved into decoding: %v", err)
			}
			if err := models.ValidateProviderDependencies([]models.ProviderDependency{dep}); err == nil || !strings.Contains(err.Error(), "outputs") {
				t.Fatalf("empty selection lost its shape diagnostic: %v", err)
			}
		})
	}
}

func TestProviderDependencyYAMLAliasesPreserveIntegerChecks(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		valid         bool
	}{
		{"integer alias", "index: &index 0\nprovider_task: provider\ntransition: fan-out\noutputs: [*index]\n", true},
		{"null alias", "index: &index null\nprovider_task: provider\ntransition: fan-out\noutputs: [*index]\n", false},
		{"merged null selection", "defaults: &defaults {outputs: [null]}\nprovider_task: provider\ntransition: fan-out\n<<: *defaults\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var dep models.ProviderDependency
			err := yaml.Unmarshal([]byte(tc.payload), &dep)
			if tc.valid {
				if err != nil || len(dep.Outputs) != 1 || dep.Outputs[0] != 0 {
					t.Fatalf("valid integer alias changed: %+v, %v", dep, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "integer") {
				t.Fatalf("non-integer alias decoded as a provider index: %+v, %v", dep, err)
			}
		})
	}
}
