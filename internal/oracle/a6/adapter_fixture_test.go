package a6

import (
	"fmt"

	"github.com/liza-mas/liza/internal/models"
	"gopkg.in/yaml.v3"
)

// Fixture decoding is isolated from the stdlib-only drivers. Lists in family
// inputs replace the common list; nested mappings override only named fields.
func fixtureState(common, delta []byte) ([]byte, error) {
	var base, overlay map[string]any
	if err := yaml.Unmarshal(common, &base); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(delta, &overlay); err != nil {
		return nil, err
	}
	mergeFixtureMappings(base, overlay)
	return yaml.Marshal(base)
}

func mergeFixtureMappings(base, overlay map[string]any) {
	for key, value := range overlay {
		left, lok := base[key].(map[string]any)
		right, rok := value.(map[string]any)
		if lok && rok {
			mergeFixtureMappings(left, right)
		} else {
			base[key] = value
		}
	}
}

func fixtureHasNoDeferredCommands(data []byte) error {
	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Goal.Integration != nil && state.Goal.Integration.DeferredValidation != nil {
		if len(state.Goal.Integration.DeferredValidation.EffectiveCommands()) != 0 {
			return fmt.Errorf("A6 isolation guard: fixture has effective deferred commands")
		}
	}
	return nil
}
