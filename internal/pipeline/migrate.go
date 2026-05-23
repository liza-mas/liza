package pipeline

import (
	"slices"

	"github.com/liza-mas/liza/internal/embedded"
)

var legacyDecompositionOutputRefs = map[string]string{
	"epic-planning-main-pair": "plan_ref",
	"architecture-main-pair":  "arch_ref",
	"code-planning-main-pair": "plan_ref",
}

// LoadEmbeddedReference loads and parses the embedded default pipeline config.
// Used as the reference for operation migration.
func LoadEmbeddedReference() (*PipelineConfig, error) {
	return LoadFromBytes(embedded.PipelineConfigContent())
}

// applyCompatibilityDefaults patches in-memory defaults for configs that were
// valid under an older schema but need derived fields before validation.
func applyCompatibilityDefaults(cfg *PipelineConfig) {
	for rolePair, outputRef := range legacyDecompositionOutputRefs {
		rp, ok := cfg.Pipeline.RolePairs[rolePair]
		if !ok || !rp.DecompositionRoot || rp.DecompositionOutputRef != "" {
			continue
		}
		rp.DecompositionOutputRef = outputRef
		cfg.Pipeline.RolePairs[rolePair] = rp
	}
}

// MigrateOperations patches missing allowed-operations from the reference config
// into a frozen config. Only adds operations — never removes them.
// This ensures that frozen workspace configs gain new operations introduced in
// newer Liza versions without requiring re-initialization.
func MigrateOperations(frozen, reference *PipelineConfig) bool {
	changed := false
	for roleName, refRole := range reference.Pipeline.Roles {
		frozenRole, ok := frozen.Pipeline.Roles[roleName]
		if !ok {
			continue
		}
		for _, op := range refRole.AllowedOperations {
			if !slices.Contains(frozenRole.AllowedOperations, op) {
				frozenRole.AllowedOperations = append(frozenRole.AllowedOperations, op)
				changed = true
			}
		}
		if changed {
			frozen.Pipeline.Roles[roleName] = frozenRole
		}
	}
	return changed
}
