package ops

import (
	"slices"
	"testing"
)

// contractEntryNames is the closed set of ADR-0252's CONTRACT, in gate order.
var contractEntryNames = []string{
	"agent_authority", "pipeline", "ownership", "worktree_branch",
	"planning_output_acceptance", "planning_output_runtime_inputs", "output_ref_fragments", "tdd",
	"prevention_records", "candidate_carriers", "acceptance_contract", "rebase",
	"published_planning_output", "published_output_ref_fragments", "published_carriers",
	"plan_amendment", "bounded_contract_correction", "review_boundary", "canonical_validation",
}

// contractNotApplicable are the gate's steps with no candidate verdict, in the
// order the local report places them.
var contractNotApplicable = []string{"submit_hold", "lifecycle_request", "preparation", "index_refresh", "publication_guards"}

// entryCoverage says how an entry is proven: parity or disclosure rows, or a
// declared subsumption by the rows of the entries whose function it applies.
type entryCoverage struct {
	rows       []string
	subsumedBy []string
	because    string
}

var entryCoverageTable = map[string]entryCoverage{
	checkAgentAuthority:                 {rows: []string{rowE01WrongGeneration}},
	checkPipeline:                       {rows: []string{rowE02NoRolePair}},
	checkOwnership:                      {rows: []string{rowE03aNotAssigned, rowE03bNoCheckpoint}},
	checkWorktreeBranchName:             {rows: []string{rowE04OtherBranch}},
	checkPlanningOutputAcceptanceName:   {rows: []string{rowE05UnparsableChildContract}},
	checkPlanningOutputRuntimeInputName: {rows: []string{rowE06RuntimeInputWithoutRecipe}},
	checkOutputRefFragmentsName:         {rows: []string{rowE07SlugFragment}},
	checkTDD:                            {rows: []string{rowE08NoTestFile}},
	checkPreventionRecordsName:          {rows: []string{rowE09FixCommitWithoutRecord}},
	checkCandidateCarriers:              {rows: []string{rowE10CarrierPinNamesNoCommit}},
	checkAcceptanceContract:             {rows: []string{rowE11ManifestMissingAMapping}},
	checkRebase:                         {rows: []string{rowREBUnstaged, rowREBStaged, rowREBMergeHead, rowREBPreRebaseHook, rowREBBehind, rowREBMergeCommit, rowREBUntrackedOnly}},
	checkPublishedPlanningOutputName: {subsumedBy: []string{checkPlanningOutputAcceptanceName},
		because: "applies entry planning_output_acceptance's function to the same commit when the rebase is a no-op"},
	checkPublishedOutputRefFragsName: {subsumedBy: []string{checkOutputRefFragmentsName},
		because: "applies entry output_ref_fragments' function to the same commit when the rebase is a no-op"},
	checkPublishedCarriersName: {subsumedBy: []string{checkCandidateCarriers},
		because: "applies entry candidate_carriers' function to the same range when the rebase is a no-op"},
	checkPlanAmendmentName:             {rows: []string{rowE16PendingPlanAmendment}},
	checkBoundedContractCorrectionName: {rows: []string{rowE17BoundedCorrectionOutsideContract}},
	checkReviewBoundaryName: {subsumedBy: []string{checkOwnership, checkWorktreeBranchName},
		because: "checks the boundary the runner itself builds, so it refuses only where ownership and worktree_branch refuse first, or operationally"},
	checkCanonicalValidation: {rows: []string{rowE19aFailingCommand, rowE19bDirtyAfterCommand, rowPREARuntimeInputUnavailable, rowPREBExecutableMissing,
		rowD7FailingOutput, rowDISSingleUseFirstFails, rowDISSingleUseFirstPasses, rowDISAdmissionFull, rowF01AllPass}},
}

// declaredRowIDs is every row the parity and disclosure tests run.
func declaredRowIDs() []string {
	ids := []string{rowF01AllPass, rowREBUntrackedOnly, rowE19aFailingCommand, rowE19bDirtyAfterCommand, rowPREBExecutableMissing, rowD7FailingOutput, rowPREARuntimeInputUnavailable}
	for _, tc := range parityRefusalCases() {
		ids = append(ids, tc.id)
	}
	return append(ids, disclosureRowIDs...)
}

func TestCheckSubmissionEveryEntryHasARow(t *testing.T) {
	// The list is exactly the CONTRACT's closed set, in gate order.
	var names []string
	for _, entry := range submissionChecks {
		names = append(names, entry.name)
	}
	if !slices.Equal(names, contractEntryNames) {
		t.Fatalf("submission checks = %v, want the CONTRACT's %v", names, contractEntryNames)
	}
	rows := declaredRowIDs()
	for _, name := range contractEntryNames {
		coverage, ok := entryCoverageTable[name]
		if !ok {
			t.Fatalf("entry %s has no row, declared subsumption or disclosure", name)
		}
		if len(coverage.rows) == 0 && (len(coverage.subsumedBy) == 0 || coverage.because == "") {
			t.Fatalf("entry %s names neither a row nor a reasoned subsumption", name)
		}
		for _, row := range coverage.rows {
			if !slices.Contains(rows, row) {
				t.Fatalf("entry %s names row %q, which no test runs", name, row)
			}
		}
		for _, by := range coverage.subsumedBy {
			if len(entryCoverageTable[by].rows) == 0 {
				t.Fatalf("entry %s is subsumed by %s, which has no row of its own", name, by)
			}
		}
	}
	if len(entryCoverageTable) != len(contractEntryNames) {
		t.Fatalf("coverage table names %d entries, the CONTRACT has %d", len(entryCoverageTable), len(contractEntryNames))
	}
}

func TestCheckSubmissionGateRunsTheListInOrder(t *testing.T) {
	f := baseFixture(t)
	var trace []string
	submissionEntryTestHook = func(name string) { trace = append(trace, name) }
	t.Cleanup(func() { submissionEntryTestHook = nil })
	if _, err := f.gate(); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if !slices.Equal(trace, contractEntryNames) {
		t.Fatalf("the gate evaluated %v, want the list in order %v", trace, contractEntryNames)
	}
}

func TestCheckSubmissionReportsEveryNameInGateOrder(t *testing.T) {
	f := baseFixture(t)
	result := f.local(t)
	requireVerdict(t, result, SubmissionVerdictPass, "")
	var got, notApplicable []string
	for _, check := range result.Checks {
		got = append(got, check.Name)
		if check.Status == SubmissionCheckNotApplicable {
			notApplicable = append(notApplicable, check.Name)
		}
	}
	if !slices.Equal(notApplicable, contractNotApplicable) {
		t.Fatalf("not_applicable = %v, want %v", notApplicable, contractNotApplicable)
	}
	var withoutN []string
	for _, name := range got {
		if !slices.Contains(contractNotApplicable, name) {
			withoutN = append(withoutN, name)
		}
	}
	if !slices.Equal(withoutN, contractEntryNames) || len(got) != len(contractEntryNames)+len(contractNotApplicable) {
		t.Fatalf("checks = %v, want every CONTRACT name in gate order", got)
	}
	if result.CandidateCommit != f.commit || result.IntegrationHead == "" || result.TaskID != f.taskID || result.AgentID != f.agentID {
		t.Fatalf("result identity = %+v", result)
	}
	for _, check := range result.Checks {
		if check.Stage == "" || (check.Status != SubmissionCheckPassed && check.Status != SubmissionCheckNotApplicable) {
			t.Fatalf("check %+v on a passing candidate", check)
		}
	}
}

// TestCheckSubmissionPostMarkerRowsAreEntries13To19 ties the rows compared under
// the lifecycle exception to the entries the gate runs after its preparation
// marker, in both directions: no silent exception, no silently whole comparison.
func TestCheckSubmissionPostMarkerRowsAreEntries13To19(t *testing.T) {
	wantRows := []string{
		rowE16PendingPlanAmendment, rowE17BoundedCorrectionOutsideContract, rowE19aFailingCommand, rowE19bDirtyAfterCommand,
		rowPREBExecutableMissing, rowD7FailingOutput, rowDISSingleUseFirstFails, rowPREARuntimeInputUnavailable,
	}
	var gotRows []string
	for row := range postMarkerRows {
		gotRows = append(gotRows, row)
	}
	slices.Sort(gotRows)
	slices.Sort(wantRows)
	if !slices.Equal(gotRows, wantRows) {
		t.Fatalf("post-marker rows = %v, want %v", gotRows, wantRows)
	}
	// Entries 13-19 are the published and canonical stages, list positions 13-19.
	first, last := submissionCheckIndex(checkPublishedPlanningOutputName), submissionCheckIndex(checkCanonicalValidation)
	if first+1 != 13 || last+1 != 19 {
		t.Fatalf("published and canonical entries span %d-%d, want 13-19", first+1, last+1)
	}
	// Rows of these entries that never refuse: they pass or are incomplete.
	nonRefusing := []string{rowF01AllPass, rowDISSingleUseFirstPasses, rowDISAdmissionFull}
	for row, entry := range postMarkerRows {
		if index := submissionCheckIndex(entry); index < first || index > last || !entryRefusesAfterMarker(entry) {
			t.Fatalf("row %s is declared post-marker at entry %s, which is not entry 13-19", row, entry)
		}
		if !slices.Contains(entryCoverageTable[entry].rows, row) {
			t.Fatalf("row %s is not declared for entry %s in the coverage table", row, entry)
		}
	}
	for index := first; index <= last; index++ {
		name := submissionChecks[index].name
		for _, row := range entryCoverageTable[name].rows {
			if _, post := postMarkerRows[row]; !post && !slices.Contains(nonRefusing, row) {
				t.Fatalf("row %s of entry %s (%d) refuses after the marker but is not declared post-marker", row, name, index+1)
			}
		}
	}
	for index := 0; index < first; index++ {
		for _, row := range entryCoverageTable[submissionChecks[index].name].rows {
			if _, post := postMarkerRows[row]; post {
				t.Fatalf("row %s of entry %s (%d) is before the marker but declared post-marker", row, submissionChecks[index].name, index+1)
			}
		}
	}
}
