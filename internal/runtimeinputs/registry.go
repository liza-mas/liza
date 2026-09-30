package runtimeinputs

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/models"
)

// MaxRegistryBytes bounds the registry file read at the integration commit.
const MaxRegistryBytes = 256 << 10

// RegistryVersion is the only registry format this build understands.
const RegistryVersion = 1

// Registry is the project's reviewed recipe catalogue. In this version a
// recipe only has to exist: executor fields arrive with the provisioner.
type Registry struct {
	Version int               `yaml:"version"`
	Recipes map[string]Recipe `yaml:"recipes"`
}

// Recipe describes how an input is produced, for the humans who run it.
type Recipe struct {
	Description string `yaml:"description"`
}

// ParseRegistry decodes the registry strictly: unknown keys, another version,
// or a malformed recipe name are errors, so a typo cannot silently admit or
// hide a recipe.
func ParseRegistry(data []byte) (*Registry, error) {
	if len(data) > MaxRegistryBytes {
		return nil, fmt.Errorf("runtime-input registry exceeds %d bytes", MaxRegistryBytes)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var registry Registry
	if err := decoder.Decode(&registry); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("runtime-input registry is empty")
		}
		return nil, fmt.Errorf("runtime-input registry is not valid: %v", err)
	}
	if registry.Version != RegistryVersion {
		return nil, fmt.Errorf("runtime-input registry version must be %d", RegistryVersion)
	}
	for name := range registry.Recipes {
		if !models.RuntimeInputRecipePattern.MatchString(name) {
			return nil, fmt.Errorf("runtime-input registry recipe %q is not a dotted registry name", name)
		}
	}
	return &registry, nil
}

// Has reports whether the registry defines recipe.
func (r *Registry) Has(recipe string) bool {
	if r == nil {
		return false
	}
	_, ok := r.Recipes[recipe]
	return ok
}
