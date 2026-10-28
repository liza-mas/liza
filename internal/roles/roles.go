// Package roles provides unified role name constants used throughout the system.
// All roles use the hyphenated form (e.g. "code-reviewer") as the single canonical name.
package roles

// ClaimType selects which claim slot ReleaseClaim releases. It is a closed
// vocabulary; role names are open (ADR-0045) and stay plain strings.
type ClaimType string

// Claim-type selectors used by ReleaseClaim to indicate which claim slot to release.
// These are NOT role names — a code-planner releases its claim with ClaimDoer,
// a code-plan-reviewer with ClaimReviewer.
const (
	ClaimDoer     ClaimType = "doer"
	ClaimReviewer ClaimType = "reviewer"
	ClaimBoth     ClaimType = "both"
)

// RoleType is the adversarial category a pipeline role declares (ADR-0042,
// ADR-0045). It is distinct from ClaimType even where the spellings match.
type RoleType string

// Role types accepted by pipeline role definitions.
const (
	RoleTypeDoer         RoleType = "doer"
	RoleTypeReviewer     RoleType = "reviewer"
	RoleTypeOrchestrator RoleType = "orchestrator"
)

// Unified role name constants. Single hyphenated form used everywhere.
const (
	Coder                = "coder"
	CodeReviewer         = "code-reviewer"
	Orchestrator         = "orchestrator"
	CodePlanner          = "code-planner"
	CodePlanReviewer     = "code-plan-reviewer"
	EpicPlanner          = "epic-planner"
	EpicPlanReviewer     = "epic-plan-reviewer"
	USWriter             = "us-writer"
	USReviewer           = "us-reviewer"
	IntegrationAnalyst   = "integration-analyst"
	IntegrationReviewer  = "integration-reviewer"
	Architect            = "architect"
	ArchitectureReviewer = "architecture-reviewer"
)

// validRoles is the set of all valid role names.
var validRoles = map[string]bool{
	Coder:                true,
	CodeReviewer:         true,
	Orchestrator:         true,
	CodePlanner:          true,
	CodePlanReviewer:     true,
	EpicPlanner:          true,
	EpicPlanReviewer:     true,
	USWriter:             true,
	USReviewer:           true,
	IntegrationAnalyst:   true,
	IntegrationReviewer:  true,
	Architect:            true,
	ArchitectureReviewer: true,
}

// IsValid checks if the given role is a valid role name.
func IsValid(role string) bool {
	return validRoles[role]
}

// All returns all valid role names.
func All() []string {
	return []string{
		Coder, CodeReviewer, Orchestrator,
		CodePlanner, CodePlanReviewer,
		EpicPlanner, EpicPlanReviewer,
		USWriter, USReviewer,
		IntegrationAnalyst, IntegrationReviewer,
		Architect, ArchitectureReviewer,
	}
}

// underscoreToHyphenated maps deprecated underscore-form role names to their
// canonical hyphenated form. Used only for migration/normalization.
var underscoreToHyphenated = map[string]string{
	"coder":                 Coder,
	"code_reviewer":         CodeReviewer,
	"orchestrator":          Orchestrator,
	"code_planner":          CodePlanner,
	"code_plan_reviewer":    CodePlanReviewer,
	"epic_planner":          EpicPlanner,
	"epic_plan_reviewer":    EpicPlanReviewer,
	"us_writer":             USWriter,
	"us_reviewer":           USReviewer,
	"integration_analyst":   IntegrationAnalyst,
	"integration_reviewer":  IntegrationReviewer,
	"architect":             Architect,
	"architecture_reviewer": ArchitectureReviewer,
}

// NormalizeRoleName converts a known underscore-form role name to its
// canonical hyphenated form. Unknown names are returned unchanged.
func NormalizeRoleName(name string) string {
	if normalized, ok := underscoreToHyphenated[name]; ok {
		return normalized
	}
	return name
}
