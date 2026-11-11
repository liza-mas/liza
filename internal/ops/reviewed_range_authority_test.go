package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Issue #213: a correction citing a small section of a large spec must apply
// when that section is unchanged. Admission resolves scalar refs with no size
// bound, so the apply-time drift check may not refuse on size either; it
// still refuses when the cited section drifted.
func TestAmendmentArtifactsCheckOversizeScalarSpecBySection(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	parent := state.FindTask("acceptance-parent")
	const specPath = "specs/large-spec.md"
	parent.Output[0].SpecRef = specPath + "#Cited"
	commit := func(content, message string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(f.root, specPath), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", specPath)
		testhelpers.MustGit(t, f.root, "commit", "-m", message)
		return testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	}
	const line = "Unrelated padding line outside the cited section.\n"
	original := "# Large spec\n\n## Cited\nReviewed requirement.\n\n## Padding\n" +
		strings.Repeat(line, referencecontract.ReferenceFileMaxBytes/len(line)+1)
	if len(original) <= referencecontract.ReferenceFileMaxBytes {
		t.Fatalf("fixture spec is %d bytes, not over every reference cap", len(original))
	}
	base := commit(original, "test: oversize spec with a small cited section")
	parent.ReviewCommit, parent.MergeCommit = &base, &base
	reviewed := strings.Replace(original, "Unrelated padding", "Corrected padding", 1)
	review := commit(reviewed, "test: correction edits only the padding")
	correction := *parent
	correction.ID, correction.AmendsPlan, correction.AmendmentMode = "correction", parent.ID, models.PlanAmendmentContract
	correction.BaseCommit, correction.ReviewCommit, correction.MergeCommit = &base, &review, &review
	correction.History = []models.TaskHistoryEntry{
		{Event: models.TaskEventSubmittedForReview, Agent: correction.AssignedTo, Commit: &review},
		{Event: models.TaskEventApproved, Agent: correction.ApprovedBy, Commit: &review},
		{Event: models.TaskEventMerged, Agent: correction.ApprovedBy, Commit: &review},
	}
	parent.PlanAmendment = &models.PlanAmendment{OriginalOutput: cloneAmendmentOutput(parent.Output), Corrections: []string{correction.ID}, Applied: []string{correction.ID}}
	state.Tasks = append(state.Tasks, correction)
	parent = state.FindTask("acceptance-parent")

	if err := ResolveRefFragmentAt(git.New(f.root), review, parent.Output[0].SpecRef); err != nil {
		t.Fatalf("admission refused the oversize scalar ref: %v", err)
	}
	if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, review); err != nil {
		t.Fatalf("unchanged cited section of an oversize spec refused: %v", err)
	}
	unrelated := commit(strings.Replace(reviewed, "Unrelated padding", "Integration padding", 1), "test: unrelated post-review padding edit")
	if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, unrelated); err != nil {
		t.Fatalf("edit outside the cited section refused: %v", err)
	}
	drift := commit(strings.Replace(reviewed, "Reviewed requirement.", "Unreviewed requirement.", 1), "test: cited section drifts after review")
	if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, drift); err == nil || !strings.Contains(err.Error(), "output[0] artifact") || !strings.Contains(err.Error(), "drifted after correction review") {
		t.Fatalf("cited-section drift accepted or misclassified: %v", err)
	}
}

func TestAmendmentArtifactsCheckScalarOnlyArchitectureScope(t *testing.T) {
	for _, mode := range []models.PlanAmendmentMode{models.PlanAmendmentContract, models.PlanAmendmentPreserveIdentity} {
		t.Run(string(mode), func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			state := replacementState(t, f)
			parent := state.FindTask("acceptance-parent")
			parent.Type = models.TaskTypeArchitecture
			parent.ArchRef = "specs/architecture.md#Scope A"
			parent.Output[0].ArchRef = "specs/architecture.md#Scope B"
			path := filepath.Join(f.root, "specs/architecture.md")
			original := "# Architecture\n\n## Scope A\n### CONTRACT\nOriginal scalar contract.\n\n## Scope B\n### CONTRACT\nUnchanged output contract.\n"
			if err := os.WriteFile(path, []byte(original), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, f.root, "add", "specs/architecture.md")
			testhelpers.MustGit(t, f.root, "commit", "-m", "test: reviewed architecture scopes")
			base := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			parent.ReviewCommit, parent.MergeCommit = &base, &base
			corrected := strings.Replace(original, "Original scalar contract.", "Reviewed scalar correction.", 1)
			if err := os.WriteFile(path, []byte(corrected), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, f.root, "add", "specs/architecture.md")
			testhelpers.MustGit(t, f.root, "commit", "-m", "test: bounded scalar contract correction")
			review := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			correction := *parent
			correction.ID, correction.AmendsPlan, correction.AmendmentMode = "correction", parent.ID, mode
			correction.BaseCommit, correction.ReviewCommit, correction.MergeCommit = &base, &review, &review
			correction.History = []models.TaskHistoryEntry{
				{Event: models.TaskEventSubmittedForReview, Agent: correction.AssignedTo, Commit: &review},
				{Event: models.TaskEventApproved, Agent: correction.ApprovedBy, Commit: &review},
				{Event: models.TaskEventMerged, Agent: correction.ApprovedBy, Commit: &review},
			}
			parent.PlanAmendment = &models.PlanAmendment{OriginalOutput: cloneAmendmentOutput(parent.Output), Corrections: []string{correction.ID}, Pending: correction.ID}
			state.Tasks = append(state.Tasks, correction)
			parent = state.FindTask("acceptance-parent")
			if err := validateBoundedContractCorrection(f.root, state, parent, state.FindTask(correction.ID), review); err != nil {
				t.Fatalf("fixture correction is not bounded reviewed contract prose: %v", err)
			}
			parent.PlanAmendment.Pending, parent.PlanAmendment.Applied = "", []string{correction.ID}
			if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, review); err != nil {
				t.Fatalf("unchanged reviewed scalar contract refused: %v", err)
			}
			if err := os.WriteFile(filepath.Join(f.root, "independent.txt"), []byte("Unreferenced integration edit.\n"), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, f.root, "add", "independent.txt")
			testhelpers.MustGit(t, f.root, "commit", "-m", "test: unrelated post-review edit")
			independent := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, independent); err != nil {
				t.Fatalf("unreferenced edit blocked scalar contract adoption: %v", err)
			}
			if err := os.WriteFile(path, []byte(strings.Replace(corrected, "Reviewed scalar correction.", "Unreviewed scalar drift.", 1)), 0644); err != nil {
				t.Fatal(err)
			}
			testhelpers.MustGit(t, f.root, "add", "specs/architecture.md")
			testhelpers.MustGit(t, f.root, "commit", "-m", "test: scalar-only scope drifts after review")
			drift := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
			if err := ValidatePlanAmendmentArtifacts(f.root, state, parent, drift); err == nil || !strings.Contains(err.Error(), "arch_ref artifact") || !strings.Contains(err.Error(), "drifted after correction review") {
				t.Fatalf("scalar-only architecture drift accepted or misclassified: %v", err)
			}
		})
	}
}
