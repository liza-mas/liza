package pipeline

import (
	"fmt"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/models"
)

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

// StateCategory identifies the lifecycle category for a declared role-pair state.
type StateCategory string

const (
	StateCategoryInitial           StateCategory = "initial"
	StateCategoryExecuting         StateCategory = "executing"
	StateCategorySubmitted         StateCategory = "submitted"
	StateCategoryReviewing         StateCategory = "reviewing"
	StateCategoryApproved          StateCategory = "approved"
	StateCategoryRejected          StateCategory = "rejected"
	StateCategoryPartiallyApproved StateCategory = "partially-approved"
	StateCategoryReviewing2        StateCategory = "reviewing-2"
	StateCategoryClean             StateCategory = "clean"
)

// NewResolver creates a Resolver from a validated PipelineConfig.
func NewResolver(config *PipelineConfig, opts ...ResolverOption) *Resolver {
	r := &Resolver{config: config}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *Resolver) lookupStates(rolePair string) (*RolePairStates, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return nil, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return &rp.States, nil
}

// InitialStatus returns the initial state for the given role-pair.
func (r *Resolver) InitialStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Initial), nil
}

// ExecutingStatus returns the executing state for the given role-pair.
func (r *Resolver) ExecutingStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Executing), nil
}

// SubmittedStatus returns the submitted state for the given role-pair.
func (r *Resolver) SubmittedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Submitted), nil
}

// ReviewingStatus returns the reviewing state for the given role-pair.
func (r *Resolver) ReviewingStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Reviewing), nil
}

// ApprovedStatus returns the approved state for the given role-pair.
func (r *Resolver) ApprovedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Approved), nil
}

// RejectedStatus returns the rejected state for the given role-pair.
func (r *Resolver) RejectedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	return models.TaskStatus(s.Rejected), nil
}

// PartiallyApprovedStatus returns the partially-approved state for the given role-pair.
// Returns an error if the role-pair does not declare a partially-approved state.
func (r *Resolver) PartiallyApprovedStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.PartiallyApproved == "" {
		return "", fmt.Errorf("role-pair %q has no partially-approved state declared", rolePair)
	}
	return models.TaskStatus(s.PartiallyApproved), nil
}

// Reviewing2Status returns the reviewing-2 state for the given role-pair.
// Returns an error if the role-pair does not declare a reviewing-2 state.
func (r *Resolver) Reviewing2Status(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.Reviewing2 == "" {
		return "", fmt.Errorf("role-pair %q has no reviewing-2 state declared", rolePair)
	}
	return models.TaskStatus(s.Reviewing2), nil
}

// CleanStatus returns the clean (terminal) state for the given role-pair.
// Returns an error if the role-pair does not declare a clean state.
func (r *Resolver) CleanStatus(rolePair string) (models.TaskStatus, error) {
	s, err := r.lookupStates(rolePair)
	if err != nil {
		return "", err
	}
	if s.Clean == "" {
		return "", fmt.Errorf("role-pair %q has no clean state declared", rolePair)
	}
	return models.TaskStatus(s.Clean), nil
}

// ResolvedTimeouts holds parsed timeout durations for a role.
type ResolvedTimeouts struct {
	Execution    time.Duration
	PollInterval time.Duration
	MaxWait      time.Duration
}

// RoleType returns the type (doer, reviewer, orchestrator) for the named role.
func (r *Resolver) RoleType(name string) (string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return "", fmt.Errorf("unknown role %q", name)
	}
	return role.Type, nil
}

// DoerRoleNames returns the sorted names of all roles with type "doer".
func (r *Resolver) DoerRoleNames() []string {
	return r.roleNamesByType("doer")
}

// ReviewerRoleNames returns the sorted names of all roles with type "reviewer".
func (r *Resolver) ReviewerRoleNames() []string {
	return r.roleNamesByType("reviewer")
}

// AllRoleNames returns the sorted names of all declared roles.
func (r *Resolver) AllRoleNames() []string {
	names := make([]string, 0, len(r.config.Pipeline.Roles))
	for name := range r.config.Pipeline.Roles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// AllowedOperations returns the allowed-operations list for the named role.
func (r *Resolver) AllowedOperations(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.AllowedOperations, nil
}

// RoleTimeouts returns the parsed timeout durations for the named role.
func (r *Resolver) RoleTimeouts(name string) (*ResolvedTimeouts, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	if role.Timeouts == nil {
		return nil, fmt.Errorf("role %q has no timeouts defined", name)
	}
	exec, err := time.ParseDuration(role.Timeouts.Execution)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing execution timeout: %w", name, err)
	}
	poll, err := time.ParseDuration(role.Timeouts.PollInterval)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing poll-interval: %w", name, err)
	}
	maxWait, err := time.ParseDuration(role.Timeouts.MaxWait)
	if err != nil {
		return nil, fmt.Errorf("role %q: parsing max-wait: %w", name, err)
	}
	return &ResolvedTimeouts{
		Execution:    exec,
		PollInterval: poll,
		MaxWait:      maxWait,
	}, nil
}

// MaxInstances returns the max-instances for the named role.
// Orchestrator roles always return 1 regardless of YAML value (spec invariant).
// Returns 0 (unlimited) if the field is unset for non-orchestrator roles.
func (r *Resolver) MaxInstances(name string) (int, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return 0, fmt.Errorf("unknown role %q", name)
	}
	if role.Type == "orchestrator" {
		return 1, nil
	}
	return role.MaxInstances, nil
}

// RoleDisplayName returns the display-name for the named role.
// Returns the role key itself if the role is not found or has no display-name.
func (r *Resolver) RoleDisplayName(name string) string {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok || role.DisplayName == "" {
		return name
	}
	return role.DisplayName
}

// roleNamesByType returns the sorted names of all roles matching the given type.
func (r *Resolver) roleNamesByType(roleType string) []string {
	var names []string
	for name, role := range r.config.Pipeline.Roles {
		if role.Type == roleType {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// ContextSections returns the context-sections list for the named role.
func (r *Resolver) ContextSections(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.ContextSections, nil
}

// Skills returns the skills list for the named role.
func (r *Resolver) Skills(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.Skills, nil
}

// MandatoryDocs returns the mandatory-docs list for the named role.
func (r *Resolver) MandatoryDocs(name string) ([]string, error) {
	role, ok := r.config.Pipeline.Roles[name]
	if !ok {
		return nil, fmt.Errorf("unknown role %q", name)
	}
	return role.MandatoryDocs, nil
}

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
// Impact values: "standard" (or empty) uses the base quorum, "significant" uses the
// significant-change override, "architecture" uses the architecture-impact override.
// Falls back to the base quorum when no matching override exists.
// Returns 1 when no review-policy is configured.
func (r *Resolver) EffectiveQuorum(rolePair, impact string) (int, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return 0, fmt.Errorf("unknown role-pair %q", rolePair)
	}
	if rp.ReviewPolicy == nil {
		return 1, nil
	}
	switch impact {
	case "significant":
		if rp.ReviewPolicy.SignificantChange != nil {
			return rp.ReviewPolicy.SignificantChange.Quorum, nil
		}
	case "architecture":
		if rp.ReviewPolicy.ArchitectureImpact != nil {
			return rp.ReviewPolicy.ArchitectureImpact.Quorum, nil
		}
	}
	return rp.ReviewPolicy.Quorum, nil
}

// ProviderDiversity returns the provider-diversity setting for the given
// role-pair at the specified impact level.
// Override levels ("significant", "architecture") take precedence when configured;
// otherwise falls through to the base-level review-policy value.
// Returns "" when no diversity is configured.
func (r *Resolver) ProviderDiversity(rolePair, impact string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	if rp.ReviewPolicy == nil {
		return "", nil
	}
	switch impact {
	case "significant":
		if rp.ReviewPolicy.SignificantChange != nil && rp.ReviewPolicy.SignificantChange.ProviderDiversity != "" {
			return rp.ReviewPolicy.SignificantChange.ProviderDiversity, nil
		}
	case "architecture":
		if rp.ReviewPolicy.ArchitectureImpact != nil && rp.ReviewPolicy.ArchitectureImpact.ProviderDiversity != "" {
			return rp.ReviewPolicy.ArchitectureImpact.ProviderDiversity, nil
		}
	}
	return rp.ReviewPolicy.ProviderDiversity, nil
}

// TransitionMap generates the intra-pair transition map for all role-pairs.
// The fixed intra-pair flow is: initial→executing→submitted→reviewing→approved|rejected,
// with rejected→initial. When quorum states are declared, reviewing also transitions to
// partially-approved, partially-approved→reviewing-2, and reviewing-2→approved|rejected.
// Cross-cutting meta-states (BLOCKED, ABANDONED, etc.) are not included.
func (r *Resolver) TransitionMap() map[models.TaskStatus][]models.TaskStatus {
	tm := make(map[models.TaskStatus][]models.TaskStatus)
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		initial := models.TaskStatus(s.Initial)
		executing := models.TaskStatus(s.Executing)
		submitted := models.TaskStatus(s.Submitted)
		reviewing := models.TaskStatus(s.Reviewing)
		approved := models.TaskStatus(s.Approved)
		rejected := models.TaskStatus(s.Rejected)

		tm[initial] = []models.TaskStatus{executing}
		tm[executing] = []models.TaskStatus{submitted}
		tm[submitted] = []models.TaskStatus{reviewing}
		tm[reviewing] = []models.TaskStatus{approved, rejected}
		tm[rejected] = []models.TaskStatus{initial}
		tm[approved] = []models.TaskStatus{} // terminal within the pair

		if s.Clean != "" {
			clean := models.TaskStatus(s.Clean)
			tm[clean] = []models.TaskStatus{} // terminal within the pair
		}

		// Quorum state transitions when partially-approved and reviewing-2 are declared.
		if s.PartiallyApproved != "" && s.Reviewing2 != "" {
			partiallyApproved := models.TaskStatus(s.PartiallyApproved)
			reviewing2 := models.TaskStatus(s.Reviewing2)

			tm[reviewing] = append(tm[reviewing], partiallyApproved)
			tm[partiallyApproved] = []models.TaskStatus{reviewing2}
			tm[reviewing2] = []models.TaskStatus{approved, rejected}
		}
	}
	return tm
}

// AllDeclaredStates returns all state names declared across all role-pairs.
func (r *Resolver) AllDeclaredStates() []models.TaskStatus {
	var states []models.TaskStatus
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		states = append(states,
			models.TaskStatus(s.Initial),
			models.TaskStatus(s.Executing),
			models.TaskStatus(s.Submitted),
			models.TaskStatus(s.Reviewing),
			models.TaskStatus(s.Approved),
			models.TaskStatus(s.Rejected),
		)
		if s.PartiallyApproved != "" {
			states = append(states, models.TaskStatus(s.PartiallyApproved))
		}
		if s.Reviewing2 != "" {
			states = append(states, models.TaskStatus(s.Reviewing2))
		}
		if s.Clean != "" {
			states = append(states, models.TaskStatus(s.Clean))
		}
	}
	return states
}

// StateCategories returns all declared role-pair states keyed by lifecycle category.
func (r *Resolver) StateCategories() map[models.TaskStatus]StateCategory {
	categories := make(map[models.TaskStatus]StateCategory)
	for _, rp := range r.config.Pipeline.RolePairs {
		s := rp.States
		categories[models.TaskStatus(s.Initial)] = StateCategoryInitial
		categories[models.TaskStatus(s.Executing)] = StateCategoryExecuting
		categories[models.TaskStatus(s.Submitted)] = StateCategorySubmitted
		categories[models.TaskStatus(s.Reviewing)] = StateCategoryReviewing
		categories[models.TaskStatus(s.Approved)] = StateCategoryApproved
		categories[models.TaskStatus(s.Rejected)] = StateCategoryRejected
		if s.PartiallyApproved != "" {
			categories[models.TaskStatus(s.PartiallyApproved)] = StateCategoryPartiallyApproved
		}
		if s.Reviewing2 != "" {
			categories[models.TaskStatus(s.Reviewing2)] = StateCategoryReviewing2
		}
		if s.Clean != "" {
			categories[models.TaskStatus(s.Clean)] = StateCategoryClean
		}
	}
	return categories
}

// SprintTerminalStates returns the states at which a task is considered complete
// for sprint purposes. For role-pairs whose approved state is the source of a
// transition (i.e., they feed into the next pipeline stage), the approved state
// is sprint-terminal. MERGED is always included as the universal terminal for
// role-pairs at the end of the pipeline.
func (r *Resolver) SprintTerminalStates() []models.TaskStatus {
	sources := r.TransitionSourcePairs()

	var states []models.TaskStatus
	for name, rp := range r.config.Pipeline.RolePairs {
		if sources[name] {
			states = append(states, models.TaskStatus(rp.States.Approved))
		}
		if rp.States.Clean != "" {
			states = append(states, models.TaskStatus(rp.States.Clean))
		}
	}

	states = append(states, models.TaskStatusMerged)

	slices.Sort(states)
	return states
}

// RolePairNames returns the sorted names of all role-pairs in the config.
func (r *Resolver) RolePairNames() []string {
	names := make([]string, 0, len(r.config.Pipeline.RolePairs))
	for name := range r.config.Pipeline.RolePairs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// RolePair returns the definition for the named role-pair.
func (r *Resolver) RolePair(name string) (*RolePairDef, error) {
	rp, ok := r.config.Pipeline.RolePairs[name]
	if !ok {
		return nil, fmt.Errorf("unknown role-pair %q", name)
	}
	return &rp, nil
}

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

// DoerRole returns the doer agent-role key for the given role-pair.
func (r *Resolver) DoerRole(rolePair string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.Doer, nil
}

// ReviewerRole returns the reviewer agent-role key for the given role-pair.
func (r *Resolver) ReviewerRole(rolePair string) (string, error) {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return "", fmt.Errorf("unknown role-pair %q", rolePair)
	}
	return rp.Reviewer, nil
}

// Transition returns the transition definition for the given name.
// It searches across all sub-pipelines and pipeline-transitions.
func (r *Resolver) Transition(name string) (*TransitionDef, error) {
	for _, sp := range r.config.Pipeline.SubPipelines {
		for i := range sp.Transitions {
			if sp.Transitions[i].Name == name {
				return &sp.Transitions[i], nil
			}
		}
	}
	for i := range r.config.Pipeline.PipelineTransitions {
		if r.config.Pipeline.PipelineTransitions[i].Name == name {
			return &r.config.Pipeline.PipelineTransitions[i], nil
		}
	}
	return nil, fmt.Errorf("unknown transition %q", name)
}

// IsPipelineTransition reports whether name is a top-level pipeline-transition.
func (r *Resolver) IsPipelineTransition(name string) bool {
	for i := range r.config.Pipeline.PipelineTransitions {
		if r.config.Pipeline.PipelineTransitions[i].Name == name {
			return true
		}
	}
	return false
}

// PipelineTransitionsSuppressed reports whether runtime availability should skip
// top-level pipeline-transitions.
func (r *Resolver) PipelineTransitionsSuppressed() bool {
	return r.noFollowUp
}

// AvailableManualTransitions returns manual transition names available for a task at
// the given status, excluding transitions already executed.
func (r *Resolver) AvailableManualTransitions(status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	return r.availableTransitionsByTrigger("manual", status, transitionsExecuted)
}

// AvailableAutoTransitions returns auto transition names available for a task at
// the given status, excluding transitions already executed.
func (r *Resolver) AvailableAutoTransitions(status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	return r.availableTransitionsByTrigger("auto", status, transitionsExecuted)
}

// availableTransitionsByTrigger returns transition names matching the given trigger
// type that are available for a task at the given status, excluding already-executed.
func (r *Resolver) availableTransitionsByTrigger(trigger string, status models.TaskStatus, transitionsExecuted map[string]bool) []string {
	var available []string
	for _, sp := range r.config.Pipeline.SubPipelines {
		for _, t := range sp.Transitions {
			if t.Trigger != trigger {
				continue
			}
			if transitionsExecuted[t.Name] {
				continue
			}
			fromPair, fromPhase, err := parseRef(t.From)
			if err != nil {
				continue
			}
			if r.resolvePhase(fromPair, fromPhase) == status {
				available = append(available, t.Name)
			}
		}
	}
	if !r.noFollowUp {
		for _, t := range r.config.Pipeline.PipelineTransitions {
			if t.Trigger != trigger {
				continue
			}
			if transitionsExecuted[t.Name] {
				continue
			}
			if r.resolve3PartPhase(t.From) == status {
				available = append(available, t.Name)
			}
		}
	}
	slices.Sort(available)
	return available
}

// TransitionTargetRolePair returns the target role-pair name for a transition.
// Handles both 2-part refs (sub-pipeline transitions) and 3-part refs (pipeline-transitions).
func (r *Resolver) TransitionTargetRolePair(transitionName string) (string, error) {
	t, err := r.Transition(transitionName)
	if err != nil {
		return "", err
	}
	// Try 3-part ref first (pipeline-transitions use sub-pipeline.role-pair.phase).
	_, toPair, _, err3 := parse3PartRef(t.To)
	if err3 == nil {
		return toPair, nil
	}
	// Fall back to 2-part ref (sub-pipeline transitions use role-pair.phase).
	toPair2, _, err2 := parseRef(t.To)
	if err2 != nil {
		return "", fmt.Errorf("transition %q: invalid to reference: %w", transitionName, err2)
	}
	return toPair2, nil
}

// OutputConsumerRolePairs returns role-pairs that can consume output[] entries
// from sourceRolePair through an outgoing per-subtask transition.
func (r *Resolver) OutputConsumerRolePairs(sourceRolePair string) ([]string, error) {
	if _, ok := r.config.Pipeline.RolePairs[sourceRolePair]; !ok {
		return nil, fmt.Errorf("unknown role-pair %q", sourceRolePair)
	}
	var consumers []string
	for _, t := range r.AllTransitions() {
		if t.Cardinality != "per-subtask" {
			continue
		}
		fromPair, err := transitionFromRolePair(t)
		if err != nil || fromPair != sourceRolePair {
			continue
		}
		toPair, err := transitionToRolePair(t)
		if err != nil {
			return nil, fmt.Errorf("transition %q: invalid to reference: %w", t.Name, err)
		}
		if !slices.Contains(consumers, toPair) {
			consumers = append(consumers, toPair)
		}
	}
	slices.Sort(consumers)
	return consumers, nil
}

// DownstreamRolePairs returns every role-pair reachable from sourceRolePair by
// following configured pipeline transitions. The source role-pair itself is not
// included, even if a cycle exists.
func (r *Resolver) DownstreamRolePairs(sourceRolePair string) (map[string]bool, error) {
	if _, ok := r.config.Pipeline.RolePairs[sourceRolePair]; !ok {
		return nil, fmt.Errorf("unknown role-pair %q", sourceRolePair)
	}
	if r.downstreamRolePairs == nil {
		if err := r.buildDownstreamRolePairCache(); err != nil {
			return nil, err
		}
	}
	result := make(map[string]bool, len(r.downstreamRolePairs[sourceRolePair]))
	for rolePair := range r.downstreamRolePairs[sourceRolePair] {
		result[rolePair] = true
	}
	return result, nil
}

// IsRolePairDownstream reports whether dependencyRolePair is downstream of
// sourceRolePair in the configured transition graph.
func (r *Resolver) IsRolePairDownstream(sourceRolePair, dependencyRolePair string) (bool, error) {
	downstream, err := r.DownstreamRolePairs(sourceRolePair)
	if err != nil {
		return false, err
	}
	if _, ok := r.config.Pipeline.RolePairs[dependencyRolePair]; !ok {
		return false, fmt.Errorf("unknown role-pair %q", dependencyRolePair)
	}
	return downstream[dependencyRolePair], nil
}

func (r *Resolver) buildDownstreamRolePairCache() error {
	adjacency := make(map[string][]string)
	for rolePair := range r.config.Pipeline.RolePairs {
		adjacency[rolePair] = nil
	}
	for _, t := range r.AllTransitions() {
		fromPair, err := transitionFromRolePair(t)
		if err != nil {
			return fmt.Errorf("transition %q: invalid from reference: %w", t.Name, err)
		}
		toPair, err := transitionToRolePair(t)
		if err != nil {
			return fmt.Errorf("transition %q: invalid to reference: %w", t.Name, err)
		}
		if !slices.Contains(adjacency[fromPair], toPair) {
			adjacency[fromPair] = append(adjacency[fromPair], toPair)
		}
	}

	r.downstreamRolePairs = make(map[string]map[string]bool, len(adjacency))
	for source := range adjacency {
		seen := make(map[string]bool)
		var visit func(string)
		visit = func(current string) {
			for _, next := range adjacency[current] {
				if next == source || seen[next] {
					continue
				}
				seen[next] = true
				visit(next)
			}
		}
		visit(source)
		r.downstreamRolePairs[source] = seen
	}
	return nil
}

func transitionFromRolePair(t TransitionDef) (string, error) {
	if _, rolePair, _, err := parse3PartRef(t.From); err == nil {
		return rolePair, nil
	}
	rolePair, _, err := parseRef(t.From)
	return rolePair, err
}

func transitionToRolePair(t TransitionDef) (string, error) {
	if _, rolePair, _, err := parse3PartRef(t.To); err == nil {
		return rolePair, nil
	}
	rolePair, _, err := parseRef(t.To)
	return rolePair, err
}

// resolvePhase returns the concrete status for a role-pair + phase combination.
func (r *Resolver) resolvePhase(rolePair, phase string) models.TaskStatus {
	rp, ok := r.config.Pipeline.RolePairs[rolePair]
	if !ok {
		return ""
	}
	switch phase {
	case "initial":
		return models.TaskStatus(rp.States.Initial)
	case "executing":
		return models.TaskStatus(rp.States.Executing)
	case "submitted":
		return models.TaskStatus(rp.States.Submitted)
	case "reviewing":
		return models.TaskStatus(rp.States.Reviewing)
	case "approved":
		return models.TaskStatus(rp.States.Approved)
	case "rejected":
		return models.TaskStatus(rp.States.Rejected)
	case "partially-approved":
		return models.TaskStatus(rp.States.PartiallyApproved)
	case "reviewing-2":
		return models.TaskStatus(rp.States.Reviewing2)
	case "clean":
		return models.TaskStatus(rp.States.Clean)
	default:
		return ""
	}
}

// resolve3PartPhase parses a 3-part ref (sub-pipeline.role-pair.phase) and
// returns the concrete status. The sub-pipeline prefix is stripped since
// role-pair names are globally unique.
func (r *Resolver) resolve3PartPhase(ref string) models.TaskStatus {
	_, rolePair, phase, err := parse3PartRef(ref)
	if err != nil {
		return ""
	}
	return r.resolvePhase(rolePair, phase)
}

// AllTransitions returns all transition definitions across all sub-pipelines
// and pipeline-transitions. Used for cardinality-based filtering (e.g., finding
// all many-to-one transitions for cohort detection).
func (r *Resolver) AllTransitions() []TransitionDef {
	var all []TransitionDef
	for _, sp := range r.config.Pipeline.SubPipelines {
		all = append(all, sp.Transitions...)
	}
	all = append(all, r.config.Pipeline.PipelineTransitions...)
	return all
}

// TransitionSourceRolePair returns the source role-pair name for a transition.
// Handles both 2-part refs (sub-pipeline transitions) and 3-part refs (pipeline-transitions).
// Mirrors TransitionTargetRolePair for the "from" side.
func (r *Resolver) TransitionSourceRolePair(name string) (string, error) {
	t, err := r.Transition(name)
	if err != nil {
		return "", err
	}
	// Try 3-part ref first (pipeline-transitions).
	_, fromPair, _, err3 := parse3PartRef(t.From)
	if err3 == nil {
		return fromPair, nil
	}
	// Fall back to 2-part ref (sub-pipeline transitions).
	fromPair2, _, err2 := parseRef(t.From)
	if err2 != nil {
		return "", fmt.Errorf("transition %q: invalid from reference: %w", name, err2)
	}
	return fromPair2, nil
}

// IsDeclaredState checks if a status is declared in the pipeline config.
func (r *Resolver) IsDeclaredState(status models.TaskStatus) bool {
	return slices.Contains(r.AllDeclaredStates(), status)
}

// IsTransitionSourcePair checks if a role-pair is the "from" side of any
// transition (sub-pipeline or pipeline-transitions). Such role-pairs produce
// output that feeds the next pipeline stage (e.g. planning → coding).
func (r *Resolver) IsTransitionSourcePair(rolePair string) bool {
	return r.TransitionSourcePairs()[rolePair]
}

// TransitionSourcePairs returns the set of role-pair names that are
// the "from" side of any transition. The result is cached after the first call.
func (r *Resolver) TransitionSourcePairs() map[string]bool {
	if r.transitionSources != nil {
		return r.transitionSources
	}
	sources := make(map[string]bool)
	for _, sp := range r.config.Pipeline.SubPipelines {
		for _, t := range sp.Transitions {
			fromPair, _, _ := parseRef(t.From)
			sources[fromPair] = true
		}
	}
	for _, t := range r.config.Pipeline.PipelineTransitions {
		_, fromPair, _, err := parse3PartRef(t.From)
		if err == nil {
			sources[fromPair] = true
		}
	}
	r.transitionSources = sources
	return sources
}
