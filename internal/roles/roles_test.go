package roles

import (
	"reflect"
	"slices"
	"testing"
)

func TestConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"Coder", Coder, "coder"},
		{"CodeReviewer", CodeReviewer, "code-reviewer"},
		{"Orchestrator", Orchestrator, "orchestrator"},
		{"CodePlanner", CodePlanner, "code-planner"},
		{"CodePlanReviewer", CodePlanReviewer, "code-plan-reviewer"},
		{"EpicPlanner", EpicPlanner, "epic-planner"},
		{"EpicPlanReviewer", EpicPlanReviewer, "epic-plan-reviewer"},
		{"USWriter", USWriter, "us-writer"},
		{"USReviewer", USReviewer, "us-reviewer"},
		{"IntegrationAnalyst", IntegrationAnalyst, "integration-analyst"},
		{"IntegrationReviewer", IntegrationReviewer, "integration-reviewer"},
		{"Architect", Architect, "architect"},
		{"ArchitectureReviewer", ArchitectureReviewer, "architecture-reviewer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("%s = %q, want %q", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role string
		want bool
	}{
		{Coder, Coder, true},
		{CodeReviewer, CodeReviewer, true},
		{Orchestrator, Orchestrator, true},
		{CodePlanner, CodePlanner, true},
		{CodePlanReviewer, CodePlanReviewer, true},
		{EpicPlanner, EpicPlanner, true},
		{EpicPlanReviewer, EpicPlanReviewer, true},
		{USWriter, USWriter, true},
		{USReviewer, USReviewer, true},
		{IntegrationAnalyst, IntegrationAnalyst, true},
		{IntegrationReviewer, IntegrationReviewer, true},
		{Architect, Architect, true},
		{ArchitectureReviewer, ArchitectureReviewer, true},
		{"underscore form invalid", "code_reviewer", false},
		{"unknown", "unknown", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.role); got != tt.want {
				t.Errorf("IsValid(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}

func TestAll(t *testing.T) {
	t.Parallel()

	got := All()
	want := []string{
		Coder, CodeReviewer, Orchestrator,
		CodePlanner, CodePlanReviewer,
		EpicPlanner, EpicPlanReviewer,
		USWriter, USReviewer,
		IntegrationAnalyst, IntegrationReviewer,
		Architect, ArchitectureReviewer,
	}

	if len(got) != len(want) {
		t.Errorf("All() returned %d roles, want %d", len(got), len(want))
	}

	for _, role := range want {
		if !slices.Contains(got, role) {
			t.Errorf("All() missing role %q", role)
		}
	}
}

// TestNoWorkflowPrefix verifies that no exported constant uses the deprecated Workflow* prefix.
// This is a done_when criterion: constants must be Coder, CodeReviewer, etc. — not WorkflowCoder.
func TestNoWorkflowPrefix(t *testing.T) {
	t.Parallel()

	for _, role := range All() {
		if !IsValid(role) {
			t.Errorf("All() contains invalid role %q", role)
		}
	}
}

func TestNormalizeRoleName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"coder unchanged", "coder", "coder"},
		{"code_reviewer to code-reviewer", "code_reviewer", "code-reviewer"},
		{"orchestrator unchanged", "orchestrator", "orchestrator"},
		{"code_planner to code-planner", "code_planner", "code-planner"},
		{"code_plan_reviewer to code-plan-reviewer", "code_plan_reviewer", "code-plan-reviewer"},
		{"epic_planner to epic-planner", "epic_planner", "epic-planner"},
		{"epic_plan_reviewer to epic-plan-reviewer", "epic_plan_reviewer", "epic-plan-reviewer"},
		{"us_writer to us-writer", "us_writer", "us-writer"},
		{"us_reviewer to us-reviewer", "us_reviewer", "us-reviewer"},
		{"integration_analyst to integration-analyst", "integration_analyst", "integration-analyst"},
		{"integration_reviewer to integration-reviewer", "integration_reviewer", "integration-reviewer"},
		{"architect unchanged", "architect", "architect"},
		{"architecture_reviewer to architecture-reviewer", "architecture_reviewer", "architecture-reviewer"},
		{"already hyphenated passes through", "code-reviewer", "code-reviewer"},
		{"unknown name passes through", "custom_role", "custom_role"},
		{"empty string passes through", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeRoleName(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeRoleName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestAllRolesAreHyphenated verifies the single-name-form invariant:
// all role values use hyphenated form, no underscores.
func TestAllRolesAreHyphenated(t *testing.T) {
	t.Parallel()

	for _, role := range All() {
		for _, c := range role {
			if c == '_' {
				t.Errorf("role %q contains underscore — expected hyphenated form only", role)
				break
			}
		}
	}
}

func TestClaimAndRoleTypeConstantValues(t *testing.T) {
	t.Parallel()

	claims := map[ClaimType]string{ClaimDoer: "doer", ClaimReviewer: "reviewer", ClaimBoth: "both"}
	for got, want := range claims {
		if string(got) != want {
			t.Errorf("claim type constant = %q, want %q", got, want)
		}
	}
	roleTypes := map[RoleType]string{RoleTypeDoer: "doer", RoleTypeReviewer: "reviewer", RoleTypeOrchestrator: "orchestrator"}
	for got, want := range roleTypes {
		if string(got) != want {
			t.Errorf("role type constant = %q, want %q", got, want)
		}
	}
}

// The two vocabularies share spellings but not identity: converting between
// them is an explicit act at a call site, never an accident of assignment.
func TestClaimTypeAndRoleTypeAreDistinct(t *testing.T) {
	t.Parallel()

	if reflect.TypeOf(ClaimDoer) == reflect.TypeOf(RoleTypeDoer) {
		t.Fatal("ClaimType and RoleType are the same type")
	}
	if string(ClaimDoer) != string(RoleTypeDoer) || string(ClaimReviewer) != string(RoleTypeReviewer) {
		t.Error("shared spellings diverged between claim types and role types")
	}
}

// Vocabulary values reach string-keyed persistence by kind (internal/db
// projects string kinds and treats method-bearing types as opaque), so no
// vocabulary type may declare a serialization or zero-value method.
func TestVocabularyTypesDeclareNoSerializationMethods(t *testing.T) {
	t.Parallel()

	forbidden := []string{"MarshalYAML", "UnmarshalYAML", "MarshalText", "UnmarshalText", "MarshalJSON", "UnmarshalJSON", "IsZero"}
	for _, typ := range []reflect.Type{reflect.TypeOf(ClaimType("")), reflect.TypeOf(RoleType(""))} {
		for _, candidate := range []reflect.Type{typ, reflect.PointerTo(typ)} {
			for _, name := range forbidden {
				if _, found := candidate.MethodByName(name); found {
					t.Errorf("%s declares %s", candidate, name)
				}
			}
		}
	}
}
