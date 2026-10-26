package models

// IntegrationProgressDecision is the pure projection consumed by integration
// reconciliation and completion gates.
type IntegrationProgressDecision struct {
	PlanningSettled       bool
	FreezeContributingSet bool
	ContributingSet       *IntegrationContributingSet
	Coverage              []IntegrationScopeCoverage
	SliceRequests         []IntegrationAnalysisRequest
	GlobalReady           bool
	GlobalRequest         *IntegrationAnalysisRequest
	IntegrationComplete   bool
	Exhausted             bool
	Waiting               *IntegrationProgressReason
	Blocked               *IntegrationProgressReason
	// DeferredValidationRequest is the integration HEAD at which integration
	// needs a deferred-validation batch verdict (ADR-0204); empty otherwise.
	DeferredValidationRequest string
}

// IntegrationScopeCoverage projects the bounded evidence required for one
// frozen contributing scope.
type IntegrationScopeCoverage struct {
	PlanTaskID           string
	RootTaskIDs          []string
	Kind                 IntegrationCoverageKind
	AnalysisKey          string
	ApprovalAttestations []IntegrationApprovalAttestation
	Effective            bool
	Resolved             bool
}

// IntegrationAnalysisRequest identifies one missing immutable analysis.
type IntegrationAnalysisRequest struct {
	Key                   string
	Phase                 IntegrationAnalysisPhase
	Generation            int
	OriginatingPlanTaskID string
	RootTaskIDs           []string
	SourceCommit          string
}

// IntegrationProgressReason is a stable machine-readable wait or stop reason.
type IntegrationProgressReason struct {
	Code     string
	TaskIDs  []string
	Guidance string
}

// ReconcileIntegrationAnalysesResult reports the durable projection made by
// the integration reconciliation operation.
type ReconcileIntegrationAnalysesResult struct {
	Changed        bool                       `json:"changed"`
	CreatedTaskIDs []string                   `json:"created_task_ids,omitempty"`
	Reason         *IntegrationProgressReason `json:"reason,omitempty"`
}
