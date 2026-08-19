package models

// IntegrationCoverageKind identifies the evidence variant stored for a
// contributing plan scope.
type IntegrationCoverageKind string

const (
	IntegrationCoverageApprovalAttestation IntegrationCoverageKind = "approval_attestation"
	IntegrationCoverageSliceReport         IntegrationCoverageKind = "slice_report"
)

// IsValid reports whether the coverage kind is part of the persisted schema.
func (kind IntegrationCoverageKind) IsValid() bool {
	return kind == IntegrationCoverageApprovalAttestation || kind == IntegrationCoverageSliceReport
}

// IntegrationAnalysisPhase identifies the integration boundary an analysis
// task reviews.
type IntegrationAnalysisPhase string

const (
	IntegrationAnalysisPhaseSlice  IntegrationAnalysisPhase = "slice"
	IntegrationAnalysisPhaseGlobal IntegrationAnalysisPhase = "global"
)

// IsValid reports whether the analysis phase is part of the persisted schema.
func (phase IntegrationAnalysisPhase) IsValid() bool {
	return phase == IntegrationAnalysisPhaseSlice || phase == IntegrationAnalysisPhaseGlobal
}

// IntegrationAnalysisVerdict records the result of an immutable integration
// analysis.
type IntegrationAnalysisVerdict string

const (
	IntegrationAnalysisVerdictClean    IntegrationAnalysisVerdict = "clean"
	IntegrationAnalysisVerdictFindings IntegrationAnalysisVerdict = "findings"
)

// IsValid reports whether the analysis verdict is part of the persisted schema.
func (verdict IntegrationAnalysisVerdict) IsValid() bool {
	return verdict == IntegrationAnalysisVerdictClean || verdict == IntegrationAnalysisVerdictFindings
}

// IntegrationClosureStatus records the current terminal projection of the
// integration lifecycle.
type IntegrationClosureStatus string

const (
	IntegrationClosureStatusClean     IntegrationClosureStatus = "clean"
	IntegrationClosureStatusBlocked   IntegrationClosureStatus = "blocked"
	IntegrationClosureStatusExhausted IntegrationClosureStatus = "exhausted"
)

// IsValid reports whether the closure status is part of the persisted schema.
func (status IntegrationClosureStatus) IsValid() bool {
	return status == IntegrationClosureStatusClean ||
		status == IntegrationClosureStatusBlocked ||
		status == IntegrationClosureStatusExhausted
}

// IntegrationLifecycle is the goal-scoped ledger of immutable integration
// evidence and the current closure projection.
type IntegrationLifecycle struct {
	ContributingSet   *IntegrationContributingSet   `yaml:"contributing_set,omitempty" json:"contributing_set,omitempty"`
	Coverage          []IntegrationCoverageRecord   `yaml:"coverage,omitempty" json:"coverage,omitempty"`
	GlobalGenerations []IntegrationGlobalGeneration `yaml:"global_generations,omitempty" json:"global_generations,omitempty"`
	MutationReceipts  []IntegrationMutationReceipt  `yaml:"mutation_receipts,omitempty" json:"mutation_receipts,omitempty"`
	Closure           *IntegrationClosure           `yaml:"closure,omitempty" json:"closure,omitempty"`
}

// IntegrationContributingSet freezes the plan scopes that contribute merged
// coding work to an integration lifecycle.
type IntegrationContributingSet struct {
	Scopes []IntegrationScopeSnapshot `yaml:"scopes" json:"scopes"`
}

// IntegrationScopeSnapshot binds a contributing plan to its distinct root
// coding-task lineages.
type IntegrationScopeSnapshot struct {
	PlanTaskID  string   `yaml:"plan_task_id" json:"plan_task_id"`
	RootTaskIDs []string `yaml:"root_task_ids" json:"root_task_ids"`
}

// IntegrationCoverageRecord is the tagged local-coverage union for one
// contributing plan.
type IntegrationCoverageRecord struct {
	PlanTaskID           string                           `yaml:"plan_task_id" json:"plan_task_id"`
	Kind                 IntegrationCoverageKind          `yaml:"kind" json:"kind"`
	ApprovalAttestations []IntegrationApprovalAttestation `yaml:"approval_attestations,omitempty" json:"approval_attestations,omitempty"`
	SliceReport          *IntegrationSliceReport          `yaml:"slice_report,omitempty" json:"slice_report,omitempty"`
}

// IntegrationApprovalAttestation reuses the immutable approval evidence for a
// contributing scope with one coding lineage.
type IntegrationApprovalAttestation struct {
	ReviewedTaskID     string   `yaml:"reviewed_task_id" json:"reviewed_task_id"`
	AcceptanceCriteria string   `yaml:"acceptance_criteria" json:"acceptance_criteria"`
	ReviewedCommit     string   `yaml:"reviewed_commit" json:"reviewed_commit"`
	Approver           string   `yaml:"approver" json:"approver"`
	Validation         []string `yaml:"validation" json:"validation"`
	MergeCommit        string   `yaml:"merge_commit" json:"merge_commit"`
}

// IntegrationSliceReport records the immutable result of one slice analysis.
// SourceCommit is the analyzed integration source; ReportCommit is the
// reviewed analyst artifact.
type IntegrationSliceReport struct {
	AnalysisTaskID string                     `yaml:"analysis_task_id" json:"analysis_task_id"`
	AnalysisKey    string                     `yaml:"analysis_key" json:"analysis_key"`
	Verdict        IntegrationAnalysisVerdict `yaml:"verdict" json:"verdict"`
	SourceCommit   string                     `yaml:"source_commit" json:"source_commit"`
	ReportCommit   string                     `yaml:"report_commit" json:"report_commit"`
}

// IntegrationGlobalGeneration records one ordered aggregate analysis.
type IntegrationGlobalGeneration struct {
	Generation     int                        `yaml:"generation" json:"generation"`
	AnalysisTaskID string                     `yaml:"analysis_task_id" json:"analysis_task_id"`
	AnalysisKey    string                     `yaml:"analysis_key" json:"analysis_key"`
	Verdict        IntegrationAnalysisVerdict `yaml:"verdict" json:"verdict"`
	SourceCommit   string                     `yaml:"source_commit" json:"source_commit"`
	ReportCommit   string                     `yaml:"report_commit" json:"report_commit"`
}

// IntegrationMutationReceipt records a task-attributed integration-ref change.
type IntegrationMutationReceipt struct {
	TaskID       string `yaml:"task_id" json:"task_id"`
	BeforeCommit string `yaml:"before_commit" json:"before_commit"`
	AfterCommit  string `yaml:"after_commit" json:"after_commit"`
}

// IntegrationClosure projects the lifecycle's current terminal state. Clean
// closure fields identify the exact clean global generation and source.
type IntegrationClosure struct {
	Status       IntegrationClosureStatus `yaml:"status" json:"status"`
	Generation   int                      `yaml:"generation,omitempty" json:"generation,omitempty"`
	AnalysisKey  string                   `yaml:"analysis_key,omitempty" json:"analysis_key,omitempty"`
	SourceCommit string                   `yaml:"source_commit,omitempty" json:"source_commit,omitempty"`
	Reason       string                   `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// IntegrationDescendantChange attributes one descendant task's merged commit
// to an analysis review surface.
type IntegrationDescendantChange struct {
	TaskID string `yaml:"task_id" json:"task_id"`
	Commit string `yaml:"commit" json:"commit"`
}

// IntegrationAnalysisMetadata gives an analysis task a deterministic identity
// and immutable source surface.
type IntegrationAnalysisMetadata struct {
	Key                   string                        `yaml:"key" json:"key"`
	Phase                 IntegrationAnalysisPhase      `yaml:"phase" json:"phase"`
	Generation            int                           `yaml:"generation,omitempty" json:"generation,omitempty"`
	OriginatingPlanTaskID string                        `yaml:"originating_plan_task_id,omitempty" json:"originating_plan_task_id,omitempty"`
	RootTaskIDs           []string                      `yaml:"root_task_ids,omitempty" json:"root_task_ids,omitempty"`
	DescendantChanges     []IntegrationDescendantChange `yaml:"descendant_changes,omitempty" json:"descendant_changes,omitempty"`
	SourceCommit          string                        `yaml:"source_commit" json:"source_commit"`
	AffectedPaths         []string                      `yaml:"affected_paths,omitempty" json:"affected_paths,omitempty"`
	SourceSnapshotPaths   []string                      `yaml:"source_snapshot_paths,omitempty" json:"source_snapshot_paths,omitempty"`
}
