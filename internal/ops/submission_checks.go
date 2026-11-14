package ops

import "fmt"

// submissionStage groups the checks of one submission by what they read.
type submissionStage string

const (
	// stageSnapshot checks read one state snapshot.
	stageSnapshot submissionStage = "snapshot"
	// stageCandidate checks read the candidate, the worktree's HEAD, before any rebase.
	stageCandidate submissionStage = "candidate"
	// stageRebase is the rebase of the task branch onto the integration HEAD.
	stageRebase submissionStage = "rebase"
	// stagePublished checks read the commit the submission would publish.
	stagePublished submissionStage = "published"
	// stageCanonical runs what only execution can prove.
	stageCanonical submissionStage = "canonical"
)

// The names of the submission checks, in gate order. They are the closed set
// check-submission reports and the CONTRACT of ADR-0252 declares.
const (
	checkAgentAuthority                 = "agent_authority"
	checkPipeline                       = "pipeline"
	checkOwnership                      = "ownership"
	checkWorktreeBranchName             = "worktree_branch"
	checkPlanningOutputAcceptanceName   = "planning_output_acceptance"
	checkPlanningOutputRuntimeInputName = "planning_output_runtime_inputs"
	checkOutputRefFragmentsName         = "output_ref_fragments"
	checkTDD                            = "tdd"
	checkPreventionRecordsName          = "prevention_records"
	checkCandidateCarriers              = "candidate_carriers"
	checkAcceptanceContract             = "acceptance_contract"
	checkRebase                         = "rebase"
	checkPublishedPlanningOutputName    = "published_planning_output"
	checkPublishedOutputRefFragsName    = "published_output_ref_fragments"
	checkPublishedCarriersName          = "published_carriers"
	checkPlanAmendmentName              = "plan_amendment"
	checkBoundedContractCorrectionName  = "bounded_contract_correction"
	checkReviewBoundaryName             = "review_boundary"
	checkCanonicalValidation            = "canonical_validation"
)

// submissionCheck is one entry of the shared check list: a named check that
// refuses with the gate's own error, bound to the stage it belongs to.
type submissionCheck struct {
	name  string
	stage submissionStage
	run   func(*submissionRun) error
}

// submissionChecks is the one declaration of what submission checks, in gate
// order. submit-for-review and check-submission both evaluate it; each entry
// is a single function, and what differs between the two runs is a mode of
// that function, never a copy (ADR-0252). Effects of the gate (preparation
// marker, rebase, index refresh, runtime-input spending, receipts, publication)
// stay in the gate's runner between entries.
var submissionChecks = []submissionCheck{
	{checkAgentAuthority, stageSnapshot, (*submissionRun).checkAgentAuthority},
	{checkPipeline, stageSnapshot, (*submissionRun).resolvePipeline},
	{checkOwnership, stageSnapshot, (*submissionRun).checkOwnership},
	{checkWorktreeBranchName, stageCandidate, (*submissionRun).checkWorktreeBranch},
	{checkPlanningOutputAcceptanceName, stageCandidate, (*submissionRun).checkPlanningOutputAcceptance},
	{checkPlanningOutputRuntimeInputName, stageCandidate, (*submissionRun).checkPlanningOutputRuntimeInputs},
	{checkOutputRefFragmentsName, stageCandidate, (*submissionRun).checkOutputRefFragments},
	{checkTDD, stageCandidate, (*submissionRun).enforceTDD},
	{checkPreventionRecordsName, stageCandidate, (*submissionRun).checkPreventionRecords},
	{checkCandidateCarriers, stageCandidate, (*submissionRun).resolveRebaseBase},
	{checkAcceptanceContract, stageCandidate, (*submissionRun).loadAcceptance},
	{checkRebase, stageRebase, (*submissionRun).rebaseCandidate},
	{checkPublishedPlanningOutputName, stagePublished, (*submissionRun).checkPublishedPlanningOutput},
	{checkPublishedOutputRefFragsName, stagePublished, (*submissionRun).checkPublishedOutputRefFragments},
	{checkPublishedCarriersName, stagePublished, (*submissionRun).checkPublishedCarriers},
	{checkPlanAmendmentName, stagePublished, (*submissionRun).checkPlanAmendment},
	{checkBoundedContractCorrectionName, stagePublished, (*submissionRun).checkBoundedContractCorrection},
	{checkReviewBoundaryName, stagePublished, (*submissionRun).checkReviewBoundary},
	{checkCanonicalValidation, stageCanonical, (*submissionRun).canonicalValidation},
}

// afterMarker reports whether the gate has written its preparation marker when
// it runs the entry: entries 13-19, the published and canonical stages.
func (c submissionCheck) afterMarker() bool {
	return c.stage == stagePublished || c.stage == stageCanonical
}

// submissionEntryTestHook observes each entry the gate runner starts; tests use
// it to prove the gate evaluates the list in order.
var submissionEntryTestHook func(name string)

func submissionCheckIndex(name string) int {
	for i, entry := range submissionChecks {
		if entry.name == name {
			return i
		}
	}
	panic(fmt.Sprintf("ops: %q is not a submission check", name))
}

// runEntries evaluates the entries from first through last, in list order, and
// returns the first refusal. The gate runner calls it between its own effects.
func (s *submissionRun) runEntries(first, last string) error {
	for _, entry := range submissionChecks[submissionCheckIndex(first) : submissionCheckIndex(last)+1] {
		if submissionEntryTestHook != nil {
			submissionEntryTestHook(entry.name)
		}
		if err := entry.run(s); err != nil {
			return err
		}
	}
	return nil
}

// canonicalValidation is entry canonical_validation: the strict gate's
// canonical commands, their prerequisites, deferrals, budget and diagnostics.
func (s *submissionRun) canonicalValidation() error {
	receipt, err := s.executeAcceptance()
	s.receipt = receipt
	return err
}
