package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

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
