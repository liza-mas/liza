package pipeline

// Resolver wraps a PipelineConfig for state resolution queries.
type Resolver struct {
	config              *PipelineConfig
	transitionSources   map[string]bool            // lazy-init cache for TransitionSourcePairs
	downstreamRolePairs map[string]map[string]bool // lazy-init cache for DownstreamRolePairs
	noFollowUp          bool
}

// ResolverOption configures runtime resolver behavior.
type ResolverOption func(*Resolver)

// WithNoFollowUp suppresses top-level pipeline-transitions from runtime
// availability checks while keeping structural topology queries intact.
func WithNoFollowUp() ResolverOption {
	return func(r *Resolver) {
		r.noFollowUp = true
	}
}

// NewResolver creates a Resolver from a validated PipelineConfig.
func NewResolver(config *PipelineConfig, opts ...ResolverOption) *Resolver {
	r := &Resolver{config: config}
	for _, opt := range opts {
		opt(r)
	}
	return r
}
