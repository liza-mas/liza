// Package pipeline provides types, parsing, validation, and state resolution
// for declarative pipeline configurations.
package pipeline

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
	"gopkg.in/yaml.v3"
)

// ErrConfigNotFound is returned when no pipeline config file exists.
// Callers can use errors.Is to distinguish absent config (legacy project)
// from parse/validation errors.
var ErrConfigNotFound = errors.New("pipeline config not found")

// PipelineConfig is the top-level structure of a pipeline YAML file.
type PipelineConfig struct {
	Pipeline Pipeline `yaml:"pipeline"`
}

// SlicedIntegrationCapability reports whether a frozen pipeline can run the
// sliced integration lifecycle.
type SlicedIntegrationCapability struct {
	Available bool
	Code      string
	Guidance  string
}

// SlicedIntegrationUpgradeRequired is returned when a frozen pipeline lacks
// the complete sliced integration topology.
const SlicedIntegrationUpgradeRequired = "pipeline_upgrade_required"

// Pipeline defines roles, role-pairs, sub-pipelines, and entry-points.
type Pipeline struct {
	Roles               map[string]RoleDef     `yaml:"roles,omitempty"`
	RolePairs           map[string]RolePairDef `yaml:"role-pairs"`
	SubPipelines        map[string]SubPipeline `yaml:"sub-pipelines"`
	PipelineTransitions []TransitionDef        `yaml:"pipeline-transitions"`
	EntryPoints         map[string]string      `yaml:"entry-points"`
}

// RoleDef defines a role's properties declaratively.
type RoleDef struct {
	Type              string      `yaml:"type"`
	DisplayName       string      `yaml:"display-name"`
	Description       string      `yaml:"description,omitempty"`
	MaxInstances      int         `yaml:"max-instances,omitempty"`
	Timeouts          *TimeoutDef `yaml:"timeouts,omitempty"`
	ContextSections   []string    `yaml:"context-sections,omitempty"`
	AllowedOperations []string    `yaml:"allowed-operations,omitempty"`
	Skills            []string    `yaml:"skills,omitempty"`
	MandatoryDocs     []string    `yaml:"mandatory-docs,omitempty"`
}

// TimeoutDef holds duration strings for role-specific timeouts.
type TimeoutDef struct {
	Execution    string `yaml:"execution,omitempty"`
	PollInterval string `yaml:"poll-interval,omitempty"`
	MaxWait      string `yaml:"max-wait,omitempty"`
}

// validRoleTypes lists the allowed values for RoleDef.Type.
var validRoleTypes = map[string]bool{
	"doer":         true,
	"reviewer":     true,
	"orchestrator": true,
}

// ReviewPolicyOverrideDef defines quorum overrides for a specific impact level.
type ReviewPolicyOverrideDef struct {
	Quorum            int    `yaml:"quorum"`
	ProviderDiversity string `yaml:"provider-diversity,omitempty"`
}

// ReviewPolicyDef defines the review quorum policy for a role-pair.
type ReviewPolicyDef struct {
	Quorum             int                      `yaml:"quorum"`
	ProviderDiversity  string                   `yaml:"provider-diversity,omitempty"`
	SignificantChange  *ReviewPolicyOverrideDef `yaml:"significant-change,omitempty"`
	ArchitectureImpact *ReviewPolicyOverrideDef `yaml:"architecture-impact,omitempty"`
}

// validProviderDiversity lists the allowed values for provider-diversity.
var validProviderDiversity = map[string]bool{
	"":          true, // omitted / not configured
	"preferred": true,
}

var validDecompositionOutputRefs = map[string]bool{
	"spec_ref": true,
	"epic_ref": true,
	"plan_ref": true,
	"arch_ref": true,
}

// RolePairDef defines a doer-reviewer pair and its state names.
type RolePairDef struct {
	Doer                   string           `yaml:"doer"`
	Reviewer               string           `yaml:"reviewer"`
	TaskSlug               string           `yaml:"task-slug,omitempty"` // prefix used for initial task IDs (falls back to role-pair name)
	DecompositionRoot      bool             `yaml:"decomposition-root,omitempty"`
	DecompositionOutputRef string           `yaml:"decomposition-output-ref,omitempty"`
	ReviewPolicy           *ReviewPolicyDef `yaml:"review-policy,omitempty"`
	States                 RolePairStates   `yaml:"states"`
}

// TaskSlugOrName returns TaskSlug if set, otherwise the role-pair name without
// its conventional "-pair" suffix.
func (r RolePairDef) TaskSlugOrName(name string) string {
	if r.TaskSlug != "" {
		return r.TaskSlug
	}
	return strings.TrimSuffix(name, "-pair")
}

// RolePairStates holds the state names for a role-pair's lifecycle.
// The six base states are required. PartiallyApproved and Reviewing2 are
// optional — only needed when the effective quorum can exceed 1.
type RolePairStates struct {
	Initial           string `yaml:"initial"`
	Executing         string `yaml:"executing"`
	Submitted         string `yaml:"submitted"`
	Reviewing         string `yaml:"reviewing"`
	Approved          string `yaml:"approved"`
	Rejected          string `yaml:"rejected"`
	PartiallyApproved string `yaml:"partially-approved,omitempty"`
	Reviewing2        string `yaml:"reviewing-2,omitempty"`
	Clean             string `yaml:"clean,omitempty"`
}

// SubPipeline defines an ordered sequence of role-pair steps and transitions between them.
type SubPipeline struct {
	Steps       []string        `yaml:"steps"`
	Transitions []TransitionDef `yaml:"transitions"`
}

// TransitionDef describes a cross-pair transition within a sub-pipeline.
type TransitionDef struct {
	Name        string `yaml:"name"`
	TaskSlug    string `yaml:"task-slug,omitempty"` // segment used in child task IDs (falls back to Name)
	From        string `yaml:"from"`                // e.g., "code-planning-pair.approved"
	To          string `yaml:"to"`                  // e.g., "coding-pair.initial"
	Trigger     string `yaml:"trigger"`             // "manual" or "auto"
	Cardinality string `yaml:"cardinality"`         // "per-subtask", "one-to-one", or "many-to-one"
}

// TaskSlugOrName returns TaskSlug if set, otherwise Name.
func (t TransitionDef) TaskSlugOrName() string {
	if t.TaskSlug != "" {
		return t.TaskSlug
	}
	return t.Name
}

// LoadFrozen loads the frozen pipeline config from the project runtime directory.
// After loading, it migrates missing allowed-operations from the embedded
// default config to ensure older workspaces gain new operations without
// requiring re-initialization.
// Returns ErrConfigNotFound (wrapped) when no pipeline config exists.
// Callers can use errors.Is(err, ErrConfigNotFound) to distinguish absent
// config from parse/validation errors.
func LoadFrozen(projectRoot string) (*PipelineConfig, error) {
	path := filepath.Join(paths.New(projectRoot).LizaDir(), "pipeline.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("%w at %s (run %q to create workspace)", ErrConfigNotFound, path, brand.Command("init"))
	}
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	// Migrate missing operations from the embedded default.
	refCfg, refErr := LoadEmbeddedReference()
	if refErr == nil {
		MigrateOperations(cfg, refCfg)
		// Task-slugs are NOT migrated: old workspaces keep their existing role-pair
		// prefixes and transition segments. New workspaces get task-slugs frozen at
		// init time from the embedded config.
	}

	return cfg, nil
}

// LoadFromBytes parses and validates a pipeline config from raw YAML bytes.
func LoadFromBytes(data []byte) (*PipelineConfig, error) {
	var cfg PipelineConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing pipeline config: %w", err)
	}
	applyCompatibilityDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating pipeline config: %w", err)
	}
	return &cfg, nil
}

// Load parses and validates a pipeline config from the given path.
func Load(path string) (*PipelineConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading pipeline config: %w", err)
	}
	var cfg PipelineConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing pipeline config: %w", err)
	}
	applyCompatibilityDefaults(&cfg)
	if err := validate(&cfg); err != nil {
		return nil, fmt.Errorf("validating pipeline config: %w", err)
	}
	return &cfg, nil
}

// validate checks a PipelineConfig for structural correctness.
func validate(cfg *PipelineConfig) error {
	p := &cfg.Pipeline

	if len(p.RolePairs) == 0 {
		return fmt.Errorf("pipeline must define at least one role-pair")
	}

	// Validate roles section if present.
	for name, role := range p.Roles {
		if role.Type == "" {
			return fmt.Errorf("role %q: type field is required", name)
		}
		if !validRoleTypes[role.Type] {
			return fmt.Errorf("role %q: invalid type %q (must be doer, reviewer, or orchestrator)", name, role.Type)
		}
	}

	for name, rp := range p.RolePairs {
		if rp.TaskSlug != "" && !kebabCaseRe.MatchString(rp.TaskSlug) {
			return fmt.Errorf("role-pair %q: task-slug %q must be lowercase kebab-case (e.g. %q)", name, rp.TaskSlug, "code-planning")
		}
		doer, ok := p.Roles[rp.Doer]
		if !ok {
			return fmt.Errorf("role-pair %q: doer %q not found in roles", name, rp.Doer)
		}
		if doer.Type != "doer" {
			return fmt.Errorf("role-pair %q: doer role %q has type %q, expected %q", name, rp.Doer, doer.Type, "doer")
		}
		reviewer, ok := p.Roles[rp.Reviewer]
		if !ok {
			return fmt.Errorf("role-pair %q: reviewer %q not found in roles", name, rp.Reviewer)
		}
		if reviewer.Type != "reviewer" {
			return fmt.Errorf("role-pair %q: reviewer role %q has type %q, expected %q", name, rp.Reviewer, reviewer.Type, "reviewer")
		}
		if err := validateReviewPolicy(name, rp.ReviewPolicy); err != nil {
			return err
		}
		if err := validateQuorumStates(name, rp); err != nil {
			return err
		}
	}

	stateOwner := make(map[string]string) // state name → role-pair name
	for name, rp := range p.RolePairs {
		states := []struct {
			phase    string
			value    string
			required bool
		}{
			{"initial", rp.States.Initial, true},
			{"executing", rp.States.Executing, true},
			{"submitted", rp.States.Submitted, true},
			{"reviewing", rp.States.Reviewing, true},
			{"approved", rp.States.Approved, true},
			{"rejected", rp.States.Rejected, true},
			{"partially-approved", rp.States.PartiallyApproved, false},
			{"reviewing-2", rp.States.Reviewing2, false},
			{"clean", rp.States.Clean, false},
		}
		for _, s := range states {
			if s.required && s.value == "" {
				return fmt.Errorf("role-pair %q: %s state is empty", name, s.phase)
			}
			if s.value == "" {
				continue // optional state not declared
			}
			if owner, exists := stateOwner[s.value]; exists {
				return fmt.Errorf("duplicate state name %q: used by role-pairs %q and %q", s.value, owner, name)
			}
			stateOwner[s.value] = name
		}
	}

	transitionOwner := make(map[string]string) // transition name → sub-pipeline name

	for spName, sp := range p.SubPipelines {
		if len(sp.Steps) == 0 {
			return fmt.Errorf("sub-pipeline %q: must have at least one step", spName)
		}
		for _, step := range sp.Steps {
			if _, ok := p.RolePairs[step]; !ok {
				return fmt.Errorf("sub-pipeline %q: step %q not found in role-pairs", spName, step)
			}
		}

		for _, t := range sp.Transitions {
			if err := validateTransition(t, p, sp.Steps); err != nil {
				return fmt.Errorf("sub-pipeline %q transition %q: %w", spName, t.Name, err)
			}
			if owner, exists := transitionOwner[t.Name]; exists {
				return fmt.Errorf("duplicate transition name %q: used by sub-pipelines %q and %q", t.Name, owner, spName)
			}
			transitionOwner[t.Name] = spName
		}
	}

	for _, t := range p.PipelineTransitions {
		if err := validatePipelineTransition(t, p); err != nil {
			return fmt.Errorf("pipeline-transition %q: %w", t.Name, err)
		}
		if owner, exists := transitionOwner[t.Name]; exists {
			return fmt.Errorf("duplicate transition name %q: used by %q and pipeline-transitions", t.Name, owner)
		}
		transitionOwner[t.Name] = "pipeline-transitions"
	}

	if err := validateDecompositionRoots(p); err != nil {
		return err
	}

	for epName, epValue := range p.EntryPoints {
		if err := validateEntryPoint(epName, epValue, p); err != nil {
			return err
		}
	}

	return nil
}

func validateDecompositionRoots(p *Pipeline) error {
	for rpName, rp := range p.RolePairs {
		if !rp.DecompositionRoot {
			if rp.DecompositionOutputRef != "" {
				return fmt.Errorf("role-pair %q: decomposition-output-ref requires decomposition-root: true", rpName)
			}
			continue
		}
		if !validDecompositionOutputRefs[rp.DecompositionOutputRef] {
			return fmt.Errorf("role-pair %q decomposition-root: decomposition-output-ref must be one of spec_ref, epic_ref, plan_ref, or arch_ref, got %q", rpName, rp.DecompositionOutputRef)
		}

		targets, err := decompositionRootTransitionTargets(p, rpName)
		if err != nil {
			return err
		}

		if len(targets) == 0 {
			for _, t := range p.PipelineTransitions {
				_, fromPair, _, err := parse3PartRef(t.From)
				if err != nil {
					return fmt.Errorf("role-pair %q decomposition-root pipeline-transition %q: from: %w", rpName, t.Name, err)
				}
				if fromPair == rpName {
					return fmt.Errorf("role-pair %q decomposition-root: decomposition transition must target another role-pair in the same sub-pipeline; pipeline-transition %q crosses sub-pipelines", rpName, t.Name)
				}
			}
			return fmt.Errorf("role-pair %q decomposition-root: expected exactly one outgoing decomposition transition, got 0", rpName)
		}
		if len(targets) > 1 {
			return fmt.Errorf("role-pair %q decomposition-root: expected exactly one outgoing decomposition transition, got %d", rpName, len(targets))
		}
	}
	return nil
}

func decompositionRootTransitionTargets(p *Pipeline, rootRolePair string) ([]string, error) {
	var targets []string
	for spName, sp := range p.SubPipelines {
		if !slices.Contains(sp.Steps, rootRolePair) {
			continue
		}
		for _, t := range sp.Transitions {
			fromPair, fromPhase, err := parseRef(t.From)
			if err != nil {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: from: %w", rootRolePair, t.Name, err)
			}
			if fromPair != rootRolePair {
				continue
			}
			if fromPhase != "approved" {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: from phase must be approved, got %q", rootRolePair, t.Name, fromPhase)
			}
			if t.Trigger != "auto" {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: trigger must be auto, got %q", rootRolePair, t.Name, t.Trigger)
			}
			if t.Cardinality != "per-subtask" {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: cardinality must be per-subtask, got %q", rootRolePair, t.Name, t.Cardinality)
			}

			toPair, _, err := parseRef(t.To)
			if err != nil {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: to: %w", rootRolePair, t.Name, err)
			}
			if toPair == rootRolePair {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: target role-pair must be another role-pair", rootRolePair, t.Name)
			}
			if !slices.Contains(sp.Steps, toPair) {
				return nil, fmt.Errorf("role-pair %q decomposition-root transition %q: target role-pair %q must be in the same sub-pipeline %q", rootRolePair, t.Name, toPair, spName)
			}

			targets = append(targets, toPair)
		}
	}
	return targets, nil
}

// validateReviewPolicy validates a role-pair's review-policy if present.
func validateReviewPolicy(rpName string, rp *ReviewPolicyDef) error {
	if rp == nil {
		return nil
	}
	if rp.Quorum < 1 {
		return fmt.Errorf("role-pair %q: review-policy quorum must be >= 1, got %d", rpName, rp.Quorum)
	}
	if !validProviderDiversity[rp.ProviderDiversity] {
		return fmt.Errorf("role-pair %q: review-policy provider-diversity %q is invalid (must be \"preferred\" or omitted)", rpName, rp.ProviderDiversity)
	}
	if err := validateReviewPolicyOverride(rpName, "significant-change", rp.SignificantChange); err != nil {
		return err
	}
	if err := validateReviewPolicyOverride(rpName, "architecture-impact", rp.ArchitectureImpact); err != nil {
		return err
	}
	return nil
}

// validateReviewPolicyOverride validates a single review-policy override.
func validateReviewPolicyOverride(rpName, overrideName string, override *ReviewPolicyOverrideDef) error {
	if override == nil {
		return nil
	}
	if override.Quorum < 1 {
		return fmt.Errorf("role-pair %q: review-policy %s quorum must be >= 1, got %d", rpName, overrideName, override.Quorum)
	}
	if !validProviderDiversity[override.ProviderDiversity] {
		return fmt.Errorf("role-pair %q: review-policy %s provider-diversity %q is invalid (must be \"preferred\" or omitted)", rpName, overrideName, override.ProviderDiversity)
	}
	return nil
}

// validateQuorumStates checks that a role-pair declares quorum states
// (partially-approved and reviewing-2) when the effective quorum can exceed 1.
// This includes both the base quorum and any override quorums.
func validateQuorumStates(rpName string, rp RolePairDef) error {
	if rp.ReviewPolicy == nil {
		return nil
	}
	maxQuorum := rp.ReviewPolicy.Quorum
	if rp.ReviewPolicy.SignificantChange != nil && rp.ReviewPolicy.SignificantChange.Quorum > maxQuorum {
		maxQuorum = rp.ReviewPolicy.SignificantChange.Quorum
	}
	if rp.ReviewPolicy.ArchitectureImpact != nil && rp.ReviewPolicy.ArchitectureImpact.Quorum > maxQuorum {
		maxQuorum = rp.ReviewPolicy.ArchitectureImpact.Quorum
	}
	if maxQuorum > 1 {
		if rp.States.PartiallyApproved == "" {
			return fmt.Errorf("role-pair %q: quorum states (partially-approved, reviewing-2) required when effective quorum can exceed 1", rpName)
		}
		if rp.States.Reviewing2 == "" {
			return fmt.Errorf("role-pair %q: quorum states (partially-approved, reviewing-2) required when effective quorum can exceed 1", rpName)
		}
		if rp.States.Clean != "" {
			return fmt.Errorf("role-pair %q: clean state is not supported with quorum > 1 (reviewing-2 → clean transition not implemented)", rpName)
		}
	}
	return nil
}

// parseRef splits a dotted reference like "role-pair.phase" into its components.
func parseRef(ref string) (string, string, error) {
	parts := strings.SplitN(ref, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid reference %q: expected format <name>.<phase>", ref)
	}
	return parts[0], parts[1], nil
}

// parse3PartRef splits a dotted reference like "sub-pipeline.role-pair.phase" into its components.
// Used for pipeline-transitions that reference states across sub-pipeline boundaries.
func parse3PartRef(ref string) (subPipeline, rolePair, phase string, err error) {
	parts := strings.SplitN(ref, ".", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", fmt.Errorf("invalid 3-part reference %q: expected format <sub-pipeline>.<role-pair>.<phase>", ref)
	}
	return parts[0], parts[1], parts[2], nil
}

// validPhases lists the allowed phase names for role-pair state references.
var validPhases = map[string]bool{
	"initial": true, "executing": true, "submitted": true,
	"reviewing": true, "approved": true, "rejected": true,
	"partially-approved": true, "reviewing-2": true,
	"clean": true,
}

var kebabCaseRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// validateTransitionHeader checks the common fields shared by all transition types:
// non-empty name, valid trigger, and valid cardinality.
func validateTransitionHeader(t TransitionDef) error {
	if t.Name == "" {
		return fmt.Errorf("transition name is empty")
	}
	if t.TaskSlug != "" && !kebabCaseRe.MatchString(t.TaskSlug) {
		return fmt.Errorf("task-slug %q must be lowercase kebab-case (e.g. %q)", t.TaskSlug, "code-planning")
	}
	if t.Trigger != "manual" && t.Trigger != "auto" {
		return fmt.Errorf("trigger must be %q or %q, got %q", "manual", "auto", t.Trigger)
	}
	if t.Cardinality != "per-subtask" && t.Cardinality != "one-to-one" && t.Cardinality != "many-to-one" {
		return fmt.Errorf("cardinality must be %q, %q, or %q, got %q", "per-subtask", "one-to-one", "many-to-one", t.Cardinality)
	}
	return nil
}

// validateTransition checks a single transition definition.
func validateTransition(t TransitionDef, p *Pipeline, steps []string) error {
	if err := validateTransitionHeader(t); err != nil {
		return err
	}

	fromPair, fromPhase, err := parseRef(t.From)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	if _, ok := p.RolePairs[fromPair]; !ok {
		return fmt.Errorf("from: role-pair %q not found", fromPair)
	}
	if !validPhases[fromPhase] {
		return fmt.Errorf("from: invalid phase %q", fromPhase)
	}

	toPair, toPhase, err := parseRef(t.To)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	if _, ok := p.RolePairs[toPair]; !ok {
		return fmt.Errorf("to: role-pair %q not found", toPair)
	}
	if !validPhases[toPhase] {
		return fmt.Errorf("to: invalid phase %q", toPhase)
	}

	if !slices.Contains(steps, fromPair) {
		return fmt.Errorf("from role-pair %q is not a step of this sub-pipeline", fromPair)
	}
	if !slices.Contains(steps, toPair) {
		return fmt.Errorf("to role-pair %q is not a step of this sub-pipeline", toPair)
	}

	return nil
}

// validatePipelineTransition checks a single pipeline-transition definition.
// Pipeline-transitions use 3-part refs and must reference different sub-pipelines.
func validatePipelineTransition(t TransitionDef, p *Pipeline) error {
	if err := validateTransitionHeader(t); err != nil {
		return err
	}

	fromSP, fromPair, fromPhase, err := parse3PartRef(t.From)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	sp, ok := p.SubPipelines[fromSP]
	if !ok {
		return fmt.Errorf("from: sub-pipeline %q not found", fromSP)
	}
	if !slices.Contains(sp.Steps, fromPair) {
		return fmt.Errorf("from: role-pair %q is not a step of sub-pipeline %q", fromPair, fromSP)
	}
	if !validPhases[fromPhase] {
		return fmt.Errorf("from: invalid phase %q", fromPhase)
	}

	toSP, toPair, toPhase, err := parse3PartRef(t.To)
	if err != nil {
		return fmt.Errorf("to: %w", err)
	}
	sp, ok = p.SubPipelines[toSP]
	if !ok {
		return fmt.Errorf("to: sub-pipeline %q not found", toSP)
	}
	if !slices.Contains(sp.Steps, toPair) {
		return fmt.Errorf("to: role-pair %q is not a step of sub-pipeline %q", toPair, toSP)
	}
	if !validPhases[toPhase] {
		return fmt.Errorf("to: invalid phase %q", toPhase)
	}

	if fromSP == toSP {
		return fmt.Errorf("from and to must reference different sub-pipelines (both reference %q)", fromSP)
	}

	return nil
}

// validateEntryPoint validates a single entry-point definition.
func validateEntryPoint(epName, epValue string, p *Pipeline) error {
	spName, rpName, err := parseRef(epValue)
	if err != nil {
		return fmt.Errorf("entry-point %q: %w", epName, err)
	}

	sp, ok := p.SubPipelines[spName]
	if !ok {
		return fmt.Errorf("entry-point %q: sub-pipeline %q not found", epName, spName)
	}

	if !slices.Contains(sp.Steps, rpName) {
		return fmt.Errorf("entry-point %q: role-pair %q is not a step of sub-pipeline %q", epName, rpName, spName)
	}

	return nil
}
