package ops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-169: the acceptance size bound applies to the section a Direct Reference
// cites, not to the whole file carrying it. The file read keeps a sanity cap.
// Literal values on purpose: the tests state the policy, not its constants.
const (
	referenceSectionBound = 256 * 1024 // cited section, after line-ending normalization
	referenceFileBound    = 4 << 20    // the file carrying it, checked before it is read
)

// boundFiller returns lines whose normalized length totals exactly n bytes.
func boundFiller(n int, eol string) string {
	var b strings.Builder
	for n > 0 {
		line := min(n, 64)
		b.WriteString(strings.Repeat("x", line-1))
		b.WriteString(eol)
		n -= line
	}
	return b.String()
}

// boundIdentitySection is the scenario's Identity section at exactly size
// bytes once line endings are normalized, trailing blank line included.
func boundIdentitySection(size int, eol string) string {
	head := "## Identity" + eol
	return head + boundFiller(size-len("## Identity\n")-1, eol) + eol
}

const smallIdentitySection = "## Identity\nReject malformed identity.\n\n"

// boundGoal is the scenario's reference file with the given Identity section.
func boundGoal(identity, eol string) string {
	return "# Boundary" + eol + eol + identity + "## Replay" + eol + "Exercise independent concurrent replay." + eol
}

// padReferenceFile appends an appendix after every cited section so the file
// is exactly size bytes; the cited sections keep their bytes.
func padReferenceFile(content string, size int) string {
	appendix := "## Appendix\n"
	return content + appendix + boundFiller(size-len(content)-len(appendix), "\n")
}

var sourceRevisionLine = regexp.MustCompile(`Source revision: "[0-9a-f]{40}"`)

// repinAcceptanceGoal commits goal as the referenced file and re-pins the plan
// to it. The pin sits in Source References, outside the Task 1 span, so the
// parent's reviewed allocation still matches at integration.
func repinAcceptanceGoal(t *testing.T, root, goal string) string {
	t.Helper()
	writeDriftFile(t, root, "specs/acceptance-goal.md", goal)
	testhelpers.MustGit(t, root, "add", "specs/acceptance-goal.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: grow the referenced file")
	pin := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	repinned := sourceRevisionLine.ReplaceAll(plan, fmt.Appendf(nil, "Source revision: %q", pin))
	if bytes.Equal(repinned, plan) {
		t.Fatal("plan declares no Source revision to re-pin")
	}
	writeDriftFile(t, root, "specs/acceptance-plan.md", string(repinned))
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: re-pin the plan to the grown file")
	return testhelpers.MustGit(t, root, "rev-parse", "integration")
}

func TestAcceptanceReferenceBoundAppliesToCitedSection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		goal     string
		identity string // normalized section expected to resolve; empty = refusal expected
		class    AcceptanceFault
		reason   []string
	}{
		{name: "small section in a file over the old whole-file bound",
			goal: padReferenceFile(boundGoal(smallIdentitySection, "\n"), 300*1024), identity: smallIdentitySection},
		{name: "small section in a file at the cap",
			goal: padReferenceFile(boundGoal(smallIdentitySection, "\n"), referenceFileBound), identity: smallIdentitySection},
		{name: "section at the bound",
			goal: boundGoal(boundIdentitySection(referenceSectionBound, "\n"), "\n"), identity: boundIdentitySection(referenceSectionBound, "\n")},
		{name: "CRLF section at the normalized bound",
			goal: boundGoal(boundIdentitySection(referenceSectionBound, "\r\n"), "\r\n"), identity: boundIdentitySection(referenceSectionBound, "\n")},
		{name: "file over the cap",
			goal:  padReferenceFile(boundGoal(smallIdentitySection, "\n"), referenceFileBound+1),
			class: AcceptanceFaultContent, reason: []string{"cannot resolve pinned reference identity", fmt.Sprintf("exceeds %d KiB", referenceFileBound/1024), fmt.Sprintf("%d bytes", referenceFileBound+1)}},
		{name: "section over the bound",
			goal:  boundGoal(boundIdentitySection(referenceSectionBound+1, "\n"), "\n"),
			class: AcceptanceFaultContent, reason: []string{"identity", fmt.Sprint(referenceSectionBound + 1), fmt.Sprint(referenceSectionBound)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a merged plan whose Direct Reference file is re-pinned to tc.goal.
			root, taskID, _, _, bb := completeAcceptanceScenario(t)
			integration := repinAcceptanceGoal(t, root, tc.goal)
			if size := len(testhelpers.MustGit(t, root, "show", integration+":specs/acceptance-goal.md")); size <= referenceSectionBound {
				t.Fatalf("premise: committed reference file is %d bytes, want it over the old whole-file bound", size)
			}
			state := readAcceptanceState(t, bb)

			// WHEN claim-time acceptance resolves the task's references.
			input, err := loadAcceptanceInput(root, state, state.FindTask(taskID), integration)

			// THEN only the cited section and the file cap decide.
			if tc.identity != "" {
				if err != nil || input == nil {
					t.Fatalf("loadAcceptanceInput() = %v, %v; want the allocation adopted", input, err)
				}
				refs, ok := carrierReferences(root, integration, "specs/acceptance-plan.md")
				if !ok {
					t.Fatal("carrier references do not parse at integration")
				}
				if section, found := resolveDeclaredReference(root, refs, "identity"); !found || section != tc.identity {
					t.Fatalf("resolveDeclaredReference(identity) found=%v, %d bytes; want the %d-byte normalized section", found, len(section), len(tc.identity))
				}
				return
			}
			var refusal *AcceptanceEvidenceError
			if !errors.As(err, &refusal) || input != nil {
				t.Fatalf("loadAcceptanceInput() = %v, %v; want an acceptance refusal", input, err)
			}
			if refusal.Class != tc.class {
				t.Errorf("class = %q, want %q (reason %q)", refusal.Class, tc.class, refusal.Reason)
			}
			for _, want := range tc.reason {
				if !strings.Contains(refusal.Reason, want) {
					t.Errorf("reason %q does not name %q", refusal.Reason, want)
				}
			}
		})
	}
}

// The plan carrier keeps the documented 256 KiB whole-file bound.
func TestAcceptanceCarrierKeepsItsWholeFileBound(t *testing.T) {
	root, taskID, _, _, bb := completeAcceptanceScenario(t)
	planPath := filepath.Join(root, "specs", "acceptance-plan.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	writeDriftFile(t, root, "specs/acceptance-plan.md", padReferenceFile(string(plan), 256*1024+1))
	testhelpers.MustGit(t, root, "add", "specs/acceptance-plan.md")
	testhelpers.MustGit(t, root, "commit", "-m", "test: grow the carrier past its bound")
	state := readAcceptanceState(t, bb)

	_, err = loadAcceptanceInput(root, state, state.FindTask(taskID), testhelpers.MustGit(t, root, "rev-parse", "integration"))

	var refusal *AcceptanceEvidenceError
	if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, "exceeds 256 KiB") || refusal.Class != AcceptanceFaultContent {
		t.Fatalf("loadAcceptanceInput() error = %v; want the carrier refused at 256 KiB as a content fault", err)
	}
}

// Merge-time drift reads the integration side of a reference through the
// same section bound, so a large file's unrelated growth is not drift while
// an edit inside the pinned section still is.
func TestObligationDriftIgnoresGrowthOutsideTheSectionOfALargeFile(t *testing.T) {
	const source = "# Source\n\n## Counters\nCount observations.\n\n## Other\nUnrelated.\n"
	for _, tc := range []struct {
		name        string
		counters    string
		wantDrifted bool
	}{
		{name: "pinned section unchanged", counters: "## Counters\nCount observations.\n\n"},
		{name: "pinned section edited", counters: "## Counters\nCount observations and skips.\n\n", wantDrifted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a carrier pinned to a small file that later grows past 256 KiB.
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			writeDriftFile(t, root, "specs/source.md", source)
			testhelpers.MustGit(t, root, "add", "--all")
			testhelpers.MustGit(t, root, "commit", "-m", "test: record source")
			pinned := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
			writeDriftFile(t, root, "specs/plan-a.md", driftCarrier(pinned, "specs/source.md#Counters", "AC-1"))
			testhelpers.MustGit(t, root, "add", "--all")
			testhelpers.MustGit(t, root, "commit", "-m", "test: reviewed carrier")
			reviewCommit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
			grown := padReferenceFile("# Source\n\n"+tc.counters+"## Other\nUnrelated.\n", 300*1024)
			writeDriftFile(t, root, "specs/source.md", grown)
			testhelpers.MustGit(t, root, "add", "--all")
			testhelpers.MustGit(t, root, "commit", "-m", "test: grow the source past the old bound")
			integration := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

			// WHEN merge-time drift detection runs.
			drifts := detectObligationDrift(driftState(reviewCommit, "specs/plan-a.md#Task 1"), root, integration)

			// THEN only an edit inside the pinned section is drift, with its current section read.
			if !tc.wantDrifted {
				if len(drifts) != 0 {
					t.Fatalf("detected %+v; want no drift for growth outside the pinned section", drifts)
				}
				return
			}
			if len(drifts) != 1 || drifts[0].change != obligationDriftStale || drifts[0].key.current == "" {
				t.Fatalf("detected %+v; want one stale drift whose current section was read", drifts)
			}
		})
	}
}

// D-171: planning submission applies claim's reference bounds, so a plan whose
// coding children claim would refuse is refused before review, not after merge.
func TestPlanningSubmissionRefusesReferencesClaimWouldRefuse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		goal   string
		reason []string // empty = submission admitted
	}{
		{name: "small section in a file over the old whole-file bound",
			goal: padReferenceFile(boundGoal(smallIdentitySection, "\n"), 300*1024)},
		{name: "section at the bound",
			goal: boundGoal(boundIdentitySection(referenceSectionBound, "\n"), "\n")},
		{name: "CRLF section at the normalized bound",
			goal: boundGoal(boundIdentitySection(referenceSectionBound, "\r\n"), "\r\n")},
		{name: "file over the cap",
			goal:   padReferenceFile(boundGoal(smallIdentitySection, "\n"), referenceFileBound+1),
			reason: []string{"identity", fmt.Sprintf("exceeds %d KiB", referenceFileBound/1024), fmt.Sprintf("%d bytes", referenceFileBound+1)}},
		{name: "section over the bound",
			goal:   boundGoal(boundIdentitySection(referenceSectionBound+1, "\n"), "\n"),
			reason: []string{"identity", fmt.Sprint(referenceSectionBound + 1), fmt.Sprint(referenceSectionBound)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// GIVEN a planner's committed acceptance carrier pinned to tc.goal.
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			writeDriftFile(t, root, "specs/acceptance-goal.md", tc.goal)
			testhelpers.MustGit(t, root, "add", "--all")
			testhelpers.MustGit(t, root, "commit", "-m", "test: record the referenced file")
			pin := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
			writeDriftFile(t, root, "specs/acceptance-plan.md", fmt.Sprintf("# Code plan\n\n## Source References\nSource revision: %q\n\n### Direct References\n- \"identity\": \"specs/acceptance-goal.md#Identity\"\n\n### Obligation Coverage\n- \"AC-identity\" -> \"identity\"\n\n## Task 1\n\n### Acceptance Contract\n```json\n{\"version\":1,\"manifest\":\"acceptance/task-1.json\",\"obligations\":[\"AC-identity\"],\"validation\":[\"sh boundary_test.sh\"],\"timeout_seconds\":10,\"approved_proofs\":[]}\n```\n", pin))
			testhelpers.MustGit(t, root, "add", "--all")
			testhelpers.MustGit(t, root, "commit", "-m", "test: declare the child's acceptance")
			commit := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
			task := &models.Task{ID: "planner", Type: models.TaskTypePlanning, Output: []models.OutputEntry{{
				Desc: "Reject malformed identity", PlanRef: "specs/acceptance-plan.md#Task 1", Validation: []string{"sh boundary_test.sh"},
			}}}

			// WHEN the planning submission validates its output's acceptance.
			err := validatePlanningOutputAcceptance(root, task, commit)

			// THEN it refuses exactly what claim would refuse, naming reference, size and bound.
			if len(tc.reason) == 0 {
				if err != nil {
					t.Fatalf("validatePlanningOutputAcceptance() = %v; want the plan admitted", err)
				}
				return
			}
			if err == nil {
				t.Fatal("validatePlanningOutputAcceptance() = nil; want the oversized reference refused at submission")
			}
			for _, want := range tc.reason {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
		})
	}
}
