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

// IntegrationPlanSummary is the bounded originating-plan view for a slice.
type IntegrationPlanSummary struct {
	ID          string
	Description string
	DoneWhen    string
	SpecRef     string
	PlanRef     string
	ArchRef     string
}

// IntegrationDescendantSummary binds persisted change attribution to the
// acceptance and decomposition context of one metadata-named task.
type IntegrationDescendantSummary struct {
	ID            string
	Description   string
	DoneWhen      string
	SpecRef       string
	Commit        string
	DependsOn     []string
	Decomposition *models.DecompositionManifest
}

// IntegrationApprovalSummary is the compact prompt view of reused coding
// review evidence for one contributing scope.
type IntegrationApprovalSummary struct {
	ReviewedTaskID     string
	AcceptanceCriteria string
	ReviewedCommit     string
	Approver           string
	Validation         []string
	MergeCommit        string
}

// IntegrationSliceReportSummary is the compact prompt view of slice evidence.
type IntegrationSliceReportSummary struct {
	AnalysisTaskID string
	AnalysisKey    string
	Verdict        string
	SourceCommit   string
	ReportCommit   string
}

// IntegrationCoverageSummary is one tagged row in the global coverage map.
type IntegrationCoverageSummary struct {
	PlanTaskID           string
	Kind                 string
	ApprovalAttestations []IntegrationApprovalSummary
	SliceReport          *IntegrationSliceReportSummary
}

// TaskGraphDigest provides bounded task graph context so agents can load exact
// related tasks instead of pulling the full task list.
type TaskGraphDigest struct {
	Entries []TaskGraphEntry
	Omitted TaskGraphOmittedSummary
}

// TaskGraphEntry is a compact, prompt-safe summary of a task related to the
// assigned task.
type TaskGraphEntry struct {
	ID                string
	Description       string
	Status            string
	Relations         []string
	SharedRefs        []string
	Children          []TaskGraphChildSummary
	RemainingChildren int
	BlockedReason     string
	RepairOperation   string
}

// TaskGraphChildSummary is a bounded summary of a direct generated child task.
type TaskGraphChildSummary struct {
	ID                 string
	Status             string
	RolePair           string
	DependsOn          []string
	RemainingDependsOn int
}

// TaskGraphOmittedSummary summarizes low-salience related tasks omitted from
// the rendered digest to keep prompt size bounded.
type TaskGraphOmittedSummary struct {
	Count              int
	StatusCounts       []TaskGraphCount
	RelationCounts     []TaskGraphCount
	SampleIDs          []string
	RemainingSampleIDs int
}

// TaskGraphCount is a stable key/count pair for prompt rendering.
type TaskGraphCount struct {
	Name  string
	Count int
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

// ScipIndexRef carries prompt-safe metadata for one available SCIP index.
type ScipIndexRef struct {
	Language string
	Path     string
}

// StacklitIndexRef carries prompt-safe metadata for one available Stacklit index.
type StacklitIndexRef struct {
	Path string
}

// FunctionalClusterIndexRef carries prompt-safe metadata for one available
// Functional Clusters artifact.
type FunctionalClusterIndexRef struct {
	Path string
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
	ValidationCommands    []string
	DestructiveDB         bool
	TaskDecomposition     *models.DecompositionManifest
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
	GoalSpecRef          string
	TotalPlanTasks       int
	TaskOrdinal          int // 1-based position in visible sprint plan
	DependsOn            []string
	TaskRolePair         string
	DecompositionRoot    bool
	MasterOutputRefField string
	PhaseDependencyTasks []SiblingTaskSummary
	TaskGraph            TaskGraphDigest

	// Architecture-specific (populated for architect role)
	ParentTaskContexts         []ParentTaskContext
	PreCommitConfigExists      bool
	PreCommitBootstrapInFlight bool
	PreCommitKind              string // canonical marker string, mirrors precommit.Kind

	// Integration-specific (populated for integration-analyst and integration-reviewer)
	GoalBaseCommit             string
	CompletedTasks             []CompletedTaskSummary
	IntegrationPhase           models.IntegrationAnalysisPhase
	IntegrationGeneration      int
	IntegrationSourceCommit    string
	IntegrationOriginatingPlan *IntegrationPlanSummary
	IntegrationRootTaskIDs     []string
	IntegrationDescendants     []IntegrationDescendantSummary
	IntegrationAffectedPaths   []string
	IntegrationSnapshotPaths   []string
	IntegrationCoverage        []IntegrationCoverageSummary

	// Task artifact / integration context
	IntegrationBranch string

	// Coder-specific
	IntegrationFix bool // whether task is in integration fix mode
	HandoffNote    *models.HandoffEvent

	// Orchestrator-specific (pre-rendered content strings)
	DashboardOutput    string
	WakeInstruction    string
	AgentStates        string
	SprintMetrics      string
	ActivePolicies     string
	BlockedTasks       string
	CheckpointSummary  string
	PipelineConfig     string
	ScipIndexes        []ScipIndexRef
	StacklitIndexes    []StacklitIndexRef
	FunctionalClusters []FunctionalClusterIndexRef

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
