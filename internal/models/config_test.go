package models_test

import (
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

func TestGlobalIntegrationGenerationLimitDefaults(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
	}{
		{name: "absent", data: "{}\n", want: 3},
		{name: "zero", data: "max_global_integration_generations: 0\n", want: 3},
		{name: "negative", data: "max_global_integration_generations: -1\n", want: 3},
		{name: "positive", data: "max_global_integration_generations: 7\n", want: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config models.Config
			if err := yaml.Unmarshal([]byte(tt.data), &config); err != nil {
				t.Fatalf("unmarshal config: %v", err)
			}

			config.MaxGlobalIntegrationGenerations = models.NormalizeGlobalIntegrationGenerationLimit(config.MaxGlobalIntegrationGenerations)
			if config.MaxGlobalIntegrationGenerations != tt.want {
				t.Fatalf("normalized limit = %d, want %d", config.MaxGlobalIntegrationGenerations, tt.want)
			}

			encoded, err := yaml.Marshal(config)
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}

			var persisted map[string]any
			if err := yaml.Unmarshal(encoded, &persisted); err != nil {
				t.Fatalf("unmarshal persisted config: %v", err)
			}
			if got := persisted["max_global_integration_generations"]; got != tt.want {
				t.Fatalf("persisted max_global_integration_generations = %v, want %d", got, tt.want)
			}

			var roundTripped models.Config
			if err := yaml.Unmarshal(encoded, &roundTripped); err != nil {
				t.Fatalf("unmarshal round-tripped config: %v", err)
			}
			if got := models.NormalizeGlobalIntegrationGenerationLimit(roundTripped.MaxGlobalIntegrationGenerations); got != tt.want {
				t.Fatalf("round-tripped normalized limit = %d, want %d", got, tt.want)
			}
		})
	}

	state := testhelpers.CreateValidState()
	if got := state.Config.MaxGlobalIntegrationGenerations; got != 3 {
		t.Fatalf("CreateValidState limit = %d, want 3", got)
	}
}

func TestEffectiveHighChurnRejectionThreshold(t *testing.T) {
	tests := []struct {
		name string
		data string
		want int
	}{
		{name: "absent", data: "{}\n", want: models.DefaultHighChurnRejectionThreshold},
		{name: "zero", data: "high_churn_rejection_threshold: 0\n", want: models.DefaultHighChurnRejectionThreshold},
		{name: "negative", data: "high_churn_rejection_threshold: -3\n", want: models.DefaultHighChurnRejectionThreshold},
		{name: "configured", data: "high_churn_rejection_threshold: 6\n", want: 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config models.Config
			if err := yaml.Unmarshal([]byte(tt.data), &config); err != nil {
				t.Fatalf("unmarshal config: %v", err)
			}
			if got := models.EffectiveHighChurnRejectionThreshold(config); got != tt.want {
				t.Fatalf("EffectiveHighChurnRejectionThreshold() = %d, want %d", got, tt.want)
			}

			encoded, err := yaml.Marshal(config)
			if err != nil {
				t.Fatalf("marshal config: %v", err)
			}
			var persisted map[string]any
			if err := yaml.Unmarshal(encoded, &persisted); err != nil {
				t.Fatalf("unmarshal persisted config: %v", err)
			}
			key, present := persisted["high_churn_rejection_threshold"]
			if config.HighChurnRejectionThreshold == 0 && present {
				t.Fatalf("persisted high_churn_rejection_threshold = %v, want the key omitted when unset", key)
			}
			if config.HighChurnRejectionThreshold != 0 && key != config.HighChurnRejectionThreshold {
				t.Fatalf("persisted high_churn_rejection_threshold = %v, want %d", key, config.HighChurnRejectionThreshold)
			}

			var roundTripped models.Config
			if err := yaml.Unmarshal(encoded, &roundTripped); err != nil {
				t.Fatalf("unmarshal round-tripped config: %v", err)
			}
			if got := models.EffectiveHighChurnRejectionThreshold(roundTripped); got != tt.want {
				t.Fatalf("round-tripped EffectiveHighChurnRejectionThreshold() = %d, want %d", got, tt.want)
			}
		})
	}

	if models.DefaultHighChurnRejectionThreshold != 4 {
		t.Fatalf("DefaultHighChurnRejectionThreshold = %d, want 4", models.DefaultHighChurnRejectionThreshold)
	}
}

func TestEffectiveMaxInstances(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		roleMax int
		want    int
	}{
		{name: "absent project default", data: "{}\n", want: models.DefaultMaxInstances},
		{name: "non-positive project default", data: "max_instances: -1\n", want: models.DefaultMaxInstances},
		{name: "project default", data: "max_instances: 5\n", want: 5},
		{name: "role value wins", data: "max_instances: 5\n", roleMax: 2, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config models.Config
			if err := yaml.Unmarshal([]byte(tt.data), &config); err != nil {
				t.Fatalf("unmarshal config: %v", err)
			}
			if got := models.EffectiveMaxInstances(tt.roleMax, config.MaxInstances); got != tt.want {
				t.Fatalf("EffectiveMaxInstances() = %d, want %d", got, tt.want)
			}
		})
	}
}
