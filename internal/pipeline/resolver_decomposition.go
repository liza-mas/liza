package pipeline

import (
	"fmt"
)

// IsDecompositionRoot reports whether rolePair is explicitly marked as a
// decomposition-root role-pair in the topology.
func (r *Resolver) IsDecompositionRoot(rolePair string) (bool, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return false, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.DecompositionRoot, nil
}

// DecompositionOutputRef returns the output-entry artifact ref field required
// for a decomposition-root role-pair.
func (r *Resolver) DecompositionOutputRef(rolePair string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	if !rp.DecompositionRoot {
		return "", fmt.Errorf("role-pair %q is not a decomposition-root role-pair", rolePair)
	}
	if rp.DecompositionOutputRef == "" {
		return "", fmt.Errorf("decomposition-root role-pair %q has no decomposition-output-ref configured", rolePair)
	}
	return rp.DecompositionOutputRef, nil
}

// DecompositionRootForTarget returns the explicit decomposition-root role-pair
// whose auto per-subtask transition targets targetRolePair.
func (r *Resolver) DecompositionRootForTarget(targetRolePair string) (string, bool, error) {
	if _, ok := r.config.Pipeline.RolePairs[targetRolePair]; !ok {
		return "", false, fmt.Errorf("unknown role-pair %q", targetRolePair)
	}

	var roots []string
	for _, rootRolePair := range r.RolePairNames() {
		rp := r.config.Pipeline.RolePairs[rootRolePair]
		if !rp.DecompositionRoot {
			continue
		}
		rootTargets, err := decompositionRootTransitionTargets(&r.config.Pipeline, rootRolePair)
		if err != nil {
			return "", false, err
		}
		if len(rootTargets) == 0 {
			return "", false, fmt.Errorf("decomposition-root role-pair %q has no decomposition target", rootRolePair)
		}
		if len(rootTargets) > 1 {
			return "", false, fmt.Errorf("decomposition-root role-pair %q has multiple decomposition targets: %v", rootRolePair, rootTargets)
		}
		if rootTargets[0] == targetRolePair {
			roots = append(roots, rootRolePair)
		}
	}

	switch len(roots) {
	case 0:
		return "", false, nil
	case 1:
		return roots[0], true, nil
	default:
		return "", false, fmt.Errorf("role-pair %q has multiple decomposition roots: %v", targetRolePair, roots)
	}
}
