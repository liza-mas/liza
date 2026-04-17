package prompts

import (
	"github.com/liza-mas/liza/internal/models"
)

// CompletedTaskSummary provides context about completed tasks for integration analysis.
type CompletedTaskSummary struct {
	ID          string
	Description string
	DoneWhen    string
	SpecRef     string
}

// ParentTaskContext provides context about a parent task for architecture consolidation.
type ParentTaskContext struct {
	ID          string
	Description string
	DoneWhen    string
	SpecRef     string
	EpicRef     string
	PlanRef     string
}

// RoleContextData is the unified template data type for all role template blocks.
// Each field group is populated as appropriate for the role being rendered.
// Fields not relevant to a particular role remain at their zero value.
type RoleContextData struct {
	// Identity
	Role     string // canonical role name (e.g., "coder", "code-reviewer")
	AgentID  string // agent instance ID (e.g., "coder-1")
	RoleType string // "doer", "reviewer", or "orchestrator"

	// Task (populated for doer and reviewer roles)
	TaskID                string
	Description           string
	DoneWhen              string
	Scope                 string
	SpecRef               string
	EpicRef               string // epic document path only (no fragment)
	EpicSection           string // epic anchor fragment (capability section), empty if none
	EpicSlug              string // filesystem-safe slug derived from EpicRef filename
	PlanRef               string // coding plan path only (no fragment)
	PlanSection           string // coding plan anchor fragment, empty if none
	ArchRef               string // path to architecture document, empty if none
	ValidationPlan        string
	Worktree              string // resolved absolute path
	IterationNum          int
	AttemptNum            int
	PriorRejection        string // empty if no prior rejection
	PriorAttemptOutcome   string // reason from prior attempt (empty unless AttemptNum == 2)
	PriorAttemptRejection string // reviewer feedback from prior attempt (empty unless AttemptNum == 2 and Note present)

	// Review (populated for reviewer roles)
	BaseCommit      string // git diff base for reviewer
	ReviewCommit    string // git diff target for reviewer
	AssignedTo      string // code author being reviewed
	ReviewCycles    int
	ScopeExtensions []map[string]string

	// Plan scoping (populated for task-aware roles)
	GoalSpecRef    string
	SiblingTasks   []SiblingTaskSummary
	TotalPlanTasks int
	TaskOrdinal    int // 1-based position in sprint plan
	DependsOn      []string
	TaskRolePair   string

	// Architecture-specific (populated for architect role)
	ParentTaskContexts         []ParentTaskContext
	PreCommitConfigExists      bool
	PreCommitBootstrapInFlight bool
	PreCommitKind              string // canonical marker string, mirrors precommit.Kind

	// Integration-specific (populated for integration-analyst and integration-reviewer)
	GoalBaseCommit string
	CompletedTasks []CompletedTaskSummary

	// Coder-specific
	IntegrationBranch string
	IntegrationFix    bool // whether task is in integration fix mode
	HandoffNote       *models.HandoffEvent

	// Orchestrator-specific (pre-rendered content strings)
	DashboardOutput   string
	WakeInstruction   string
	AgentStates       string
	SprintMetrics     string
	ActivePolicies    string
	BlockedTasks      string
	CheckpointSummary string
	PipelineConfig    string

	// Config/state
	ProjectRoot string
	StatePath   string
	SpecsDir    string
	GoalDesc    string
	GoalSlug    string
	// Declarative (from pipeline YAML)
	MandatoryDocs []string
	Skills        []string
}
