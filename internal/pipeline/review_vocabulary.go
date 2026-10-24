package pipeline

// ReviewImpact is the impact level a doer declares for a submission; it picks
// the review-policy override that applies. The empty value means "not
// declared" and resolves like ReviewImpactStandard.
type ReviewImpact string

// Review impact levels accepted in verdict payloads and review policies.
const (
	ReviewImpactStandard     ReviewImpact = "standard"
	ReviewImpactSignificant  ReviewImpact = "significant"
	ReviewImpactArchitecture ReviewImpact = "architecture"
)

// ProviderDiversityMode is a review policy's provider-diversity setting. The
// empty value means diversity is not configured.
type ProviderDiversityMode string

// Provider-diversity modes accepted by pipeline config validation.
const (
	ProviderDiversityNone      ProviderDiversityMode = ""
	ProviderDiversityPreferred ProviderDiversityMode = "preferred"
)
