package pipeline

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
)

// The pipeline control vocabulary is validated on string input, so each typed
// constant must equal the value the existing validators accept and message.

func TestReviewVocabularyConstantValues(t *testing.T) {
	t.Parallel()

	impacts := map[ReviewImpact]string{
		ReviewImpactStandard:     "standard",
		ReviewImpactSignificant:  "significant",
		ReviewImpactArchitecture: "architecture",
	}
	for got, want := range impacts {
		if string(got) != want {
			t.Errorf("review impact constant = %q, want %q", got, want)
		}
	}
	modes := map[ProviderDiversityMode]string{
		ProviderDiversityNone:      "",
		ProviderDiversityPreferred: "preferred",
	}
	for got, want := range modes {
		if string(got) != want {
			t.Errorf("provider-diversity constant = %q, want %q", got, want)
		}
	}
}

func TestProviderDiversityValidatorAcceptsExactlyTheConstants(t *testing.T) {
	t.Parallel()

	for _, mode := range []ProviderDiversityMode{ProviderDiversityNone, ProviderDiversityPreferred} {
		policy := &ReviewPolicyDef{Quorum: 1, ProviderDiversity: mode,
			SignificantChange: &ReviewPolicyOverrideDef{Quorum: 1, ProviderDiversity: mode}}
		if err := validateReviewPolicy("coding-pair", policy); err != nil {
			t.Errorf("validateReviewPolicy(%q) = %v, want accepted", mode, err)
		}
	}

	policy := &ReviewPolicyDef{Quorum: 1, ProviderDiversity: "required"}
	err := validateReviewPolicy("coding-pair", policy)
	wantBase := `role-pair "coding-pair": review-policy provider-diversity "required" is invalid (must be "preferred" or omitted)`
	if err == nil || err.Error() != wantBase {
		t.Errorf("unknown base diversity error = %v, want %q", err, wantBase)
	}
	policy = &ReviewPolicyDef{Quorum: 1, ArchitectureImpact: &ReviewPolicyOverrideDef{Quorum: 1, ProviderDiversity: "required"}}
	err = validateReviewPolicy("coding-pair", policy)
	wantOverride := `role-pair "coding-pair": review-policy architecture-impact provider-diversity "required" is invalid (must be "preferred" or omitted)`
	if err == nil || err.Error() != wantOverride {
		t.Errorf("unknown override diversity error = %v, want %q", err, wantOverride)
	}
}

func TestRoleTypeValidatorAcceptsExactlyTheConstants(t *testing.T) {
	t.Parallel()

	for _, roleType := range []roles.RoleType{roles.RoleTypeDoer, roles.RoleTypeReviewer, roles.RoleTypeOrchestrator} {
		p := &Pipeline{Roles: map[string]RoleDef{"any-role": {Type: roleType}}}
		if err := validateRoleDefs(p); err != nil {
			t.Errorf("validateRoleDefs(type %q) = %v, want accepted", roleType, err)
		}
	}

	err := validateRoleDefs(&Pipeline{Roles: map[string]RoleDef{"any-role": {Type: "planner"}}})
	want := `role "any-role": invalid type "planner" (must be doer, reviewer, or orchestrator)`
	if err == nil || err.Error() != want {
		t.Errorf("unknown role type error = %v, want %q", err, want)
	}
	err = validateRoleDefs(&Pipeline{Roles: map[string]RoleDef{"any-role": {}}})
	want = `role "any-role": type field is required`
	if err == nil || err.Error() != want {
		t.Errorf("empty role type error = %v, want %q", err, want)
	}
}

func TestTransitionHeaderValidatorAcceptsExactlyTheConstants(t *testing.T) {
	t.Parallel()

	triggers := []models.TransitionTrigger{models.TransitionTriggerManual, models.TransitionTriggerAuto}
	cardinalities := []models.TransitionCardinality{models.CardinalityPerSubtask, models.CardinalityOneToOne, models.CardinalityManyToOne}
	for _, trigger := range triggers {
		for _, cardinality := range cardinalities {
			header := TransitionDef{Name: "move", Trigger: trigger, Cardinality: cardinality}
			if err := validateTransitionHeader(header); err != nil {
				t.Errorf("validateTransitionHeader(%q, %q) = %v, want accepted", trigger, cardinality, err)
			}
		}
	}
	for _, route := range []models.AllocationRoute{models.AllocationRouteScopeDecomposition, models.AllocationRouteCodingAllocation} {
		header := TransitionDef{Name: "move", When: route, Trigger: models.TransitionTriggerManual, Cardinality: models.CardinalityPerSubtask}
		if err := validateTransitionHeader(header); err != nil {
			t.Errorf("validateTransitionHeader(when %q) = %v, want accepted", route, err)
		}
	}

	tests := []struct {
		name   string
		header TransitionDef
		want   string
	}{
		{"unknown route", TransitionDef{Name: "move", When: "later", Trigger: "auto", Cardinality: "per-subtask"},
			`when must be scope-decomposition or coding-allocation, got "later"`},
		{"route needs per-subtask", TransitionDef{Name: "move", When: models.AllocationRouteCodingAllocation, Trigger: "auto", Cardinality: "one-to-one"},
			"conditional allocation routes require per-subtask cardinality"},
		{"unknown trigger", TransitionDef{Name: "move", Trigger: "sometimes", Cardinality: "per-subtask"},
			`trigger must be "manual" or "auto", got "sometimes"`},
		{"unknown cardinality", TransitionDef{Name: "move", Trigger: "auto", Cardinality: "few-to-one"},
			`cardinality must be "per-subtask", "one-to-one", or "many-to-one", got "few-to-one"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateTransitionHeader(tt.header)
			if err == nil || err.Error() != tt.want {
				t.Errorf("validateTransitionHeader() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTransitionAppliesRoutesByAllocationConstant(t *testing.T) {
	t.Parallel()

	direct := []models.OutputEntry{{CodingAllocation: true}}
	tests := []struct {
		route        models.AllocationRoute
		wantDirect   bool
		wantNoDirect bool
	}{
		{models.AllocationRouteNone, false, true},
		{models.AllocationRouteScopeDecomposition, false, true},
		{models.AllocationRouteCodingAllocation, true, false},
		{"unknown", false, false},
	}
	for _, tt := range tests {
		def := TransitionDef{When: tt.route}
		if got := def.AppliesTo(direct); got != tt.wantDirect {
			t.Errorf("route %q applies to a direct coding allocation = %v, want %v", tt.route, got, tt.wantDirect)
		}
		if got := def.AppliesTo(nil); got != tt.wantNoDirect {
			t.Errorf("route %q applies without one = %v, want %v", tt.route, got, tt.wantNoDirect)
		}
	}
}

func TestEffectiveQuorumAndDiversityByImpactConstant(t *testing.T) {
	t.Parallel()

	r := NewResolver(&PipelineConfig{Pipeline: Pipeline{RolePairs: map[string]RolePairDef{
		"coding-pair": {ReviewPolicy: &ReviewPolicyDef{
			Quorum:             1,
			SignificantChange:  &ReviewPolicyOverrideDef{Quorum: 2, ProviderDiversity: ProviderDiversityPreferred},
			ArchitectureImpact: &ReviewPolicyOverrideDef{Quorum: 3},
		}},
	}}})

	tests := []struct {
		impact        ReviewImpact
		wantQuorum    int
		wantDiversity ProviderDiversityMode
	}{
		{"", 1, ProviderDiversityNone},
		{ReviewImpactStandard, 1, ProviderDiversityNone},
		{ReviewImpactSignificant, 2, ProviderDiversityPreferred},
		{ReviewImpactArchitecture, 3, ProviderDiversityNone},
		{"unrecognized", 1, ProviderDiversityNone},
	}
	for _, tt := range tests {
		quorum, err := r.EffectiveQuorum("coding-pair", tt.impact)
		if err != nil || quorum != tt.wantQuorum {
			t.Errorf("EffectiveQuorum(%q) = %d, %v; want %d", tt.impact, quorum, err, tt.wantQuorum)
		}
		diversity, err := r.ProviderDiversity("coding-pair", tt.impact)
		if err != nil || diversity != tt.wantDiversity {
			t.Errorf("ProviderDiversity(%q) = %q, %v; want %q", tt.impact, diversity, err, tt.wantDiversity)
		}
	}
}

func TestReviewVocabularyTypesDeclareNoSerializationMethods(t *testing.T) {
	t.Parallel()

	forbidden := []string{"MarshalYAML", "UnmarshalYAML", "MarshalText", "UnmarshalText", "MarshalJSON", "UnmarshalJSON", "IsZero"}
	for _, typ := range []reflect.Type{reflect.TypeOf(ReviewImpact("")), reflect.TypeOf(ProviderDiversityMode(""))} {
		for _, candidate := range []reflect.Type{typ, reflect.PointerTo(typ)} {
			for _, name := range forbidden {
				if _, found := candidate.MethodByName(name); found {
					t.Errorf("%s declares %s", candidate, name)
				}
			}
		}
	}
}

// The pipeline YAML spelling of each carrier decodes into the typed field and
// the resolver reports the same type and bytes the file declared.
func TestVocabularyCarriersDecodeFromPipelineYAML(t *testing.T) {
	t.Parallel()

	cfg := loadPhase2Config(t)
	r := NewResolver(cfg)
	for name, role := range cfg.Pipeline.Roles {
		got, err := r.RoleType(name)
		if err != nil || got != role.Type {
			t.Errorf("RoleType(%q) = %q, %v; want declared %q", name, got, err, role.Type)
		}
	}
	for _, transition := range r.AllTransitions() {
		if transition.Trigger != models.TransitionTriggerManual && transition.Trigger != models.TransitionTriggerAuto {
			t.Errorf("transition %q decoded trigger %q outside the vocabulary", transition.Name, transition.Trigger)
		}
		switch transition.Cardinality {
		case models.CardinalityPerSubtask, models.CardinalityOneToOne, models.CardinalityManyToOne:
		default:
			t.Errorf("transition %q decoded cardinality %q outside the vocabulary", transition.Name, transition.Cardinality)
		}
	}
}
