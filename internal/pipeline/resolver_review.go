package pipeline

import (
	"fmt"
)

// ReviewPolicy returns the review-policy for the named role-pair.
// Returns nil (no error) if the role-pair has no review-policy configured.
func (r *Resolver) ReviewPolicy(rolePair string) (*ReviewPolicyDef, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return nil, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.ReviewPolicy, nil
}

// EffectiveQuorum returns the effective quorum for the given role-pair and impact level.
// Impact values: ReviewImpactStandard (or empty) uses the base quorum,
// ReviewImpactSignificant uses the significant-change override,
// ReviewImpactArchitecture uses the architecture-impact override.
// Falls back to the base quorum when no matching override exists.
// Returns 1 when no review-policy is configured.
func (r *Resolver) EffectiveQuorum(rolePair string, impact ReviewImpact) (int, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return 0, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	if rp.ReviewPolicy == nil {
		return 1, nil
	}
	switch impact {
	case ReviewImpactSignificant:
		if rp.ReviewPolicy.SignificantChange != nil {
			return rp.ReviewPolicy.SignificantChange.Quorum, nil
		}
	case ReviewImpactArchitecture:
		if rp.ReviewPolicy.ArchitectureImpact != nil {
			return rp.ReviewPolicy.ArchitectureImpact.Quorum, nil
		}
	}
	return rp.ReviewPolicy.Quorum, nil
}

// ProviderDiversity returns the provider-diversity setting for the given
// role-pair at the specified impact level.
// Override levels (ReviewImpactSignificant, ReviewImpactArchitecture) take precedence when configured;
// otherwise falls through to the base-level review-policy value.
// Returns ProviderDiversityNone when no diversity is configured.
func (r *Resolver) ProviderDiversity(rolePair string, impact ReviewImpact) (ProviderDiversityMode, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return ProviderDiversityNone, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	if rp.ReviewPolicy == nil {
		return ProviderDiversityNone, nil
	}
	switch impact {
	case ReviewImpactSignificant:
		if rp.ReviewPolicy.SignificantChange != nil && rp.ReviewPolicy.SignificantChange.ProviderDiversity != ProviderDiversityNone {
			return rp.ReviewPolicy.SignificantChange.ProviderDiversity, nil
		}
	case ReviewImpactArchitecture:
		if rp.ReviewPolicy.ArchitectureImpact != nil && rp.ReviewPolicy.ArchitectureImpact.ProviderDiversity != ProviderDiversityNone {
			return rp.ReviewPolicy.ArchitectureImpact.ProviderDiversity, nil
		}
	}
	return rp.ReviewPolicy.ProviderDiversity, nil
}
