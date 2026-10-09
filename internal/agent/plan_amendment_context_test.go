package agent

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestPlanAmendmentContextRetainsReviewedCarriersAndOriginalOwner(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	writeReferenceFixture(t, root, "specs/source.md", "# Source\n\n## Rule\nCurrent independently inherited rule.\n")
	source := commitReferenceFixture(t, root, "test: record source")
	writeReferenceFixture(t, root, "specs/first.md", strictCarrier(source, "ONE", "specs/source.md", "Rule", "# First\n\nOriginal carrier must remain visible.\n"))
	originalReview := commitReferenceFixture(t, root, "test: review original carrier")
	writeReferenceFixture(t, root, "specs/second.md", strictCarrier(source, "TWO", "specs/source.md", "Rule", "# Second\n\nRecovered correction carrier must remain visible.\n"))
	quarantinedReview := commitReferenceFixture(t, root, "test: merge subsequently quarantined correction")
	writeReferenceFixture(t, root, "specs/third.md", strictCarrier(source, "THREE", "specs/source.md", "Rule", "# Third\n\nLatest approved correction carrier.\n"))
	latestReview := commitReferenceFixture(t, root, "test: merge fresh reconciliation review")
	makeReview := func(id, base, review, amends string, output []models.OutputEntry) models.Task {
		planner, reviewer := "code-planner-1", "code-plan-reviewer-1"
		return models.Task{ID: id, Type: models.TaskTypePlanning, RolePair: "code-planning-pair", Status: models.TaskStatusMerged,
			AssignedTo: &planner, ApprovedBy: &reviewer, BaseCommit: &base, ReviewCommit: &review, MergeCommit: &review,
			Output: output, AmendsPlan: amends, Approvals: []models.Approval{{Agent: reviewer}},
			History: []models.TaskHistoryEntry{
				{Event: models.TaskEventSubmittedForReview, Agent: &planner, Commit: &review},
				{Event: models.TaskEventApproved, Agent: &reviewer, Commit: &review},
				{Event: models.TaskEventMerged, Agent: &reviewer, Commit: &review},
			}}
	}
	originalOutput := []models.OutputEntry{{PlanRef: "specs/first.md"}}
	quarantinedOutput := append(append([]models.OutputEntry(nil), originalOutput...), models.OutputEntry{PlanRef: "specs/second.md"})
	latestOutput := append(append([]models.OutputEntry(nil), quarantinedOutput...), models.OutputEntry{PlanRef: "specs/third.md"})
	original := makeReview("original", source, originalReview, "", latestOutput)
	original.PlanAmendment = &models.PlanAmendment{OriginalOutput: originalOutput, Corrections: []string{"quarantined", "latest", "pending"},
		Applied: []string{"latest"}, Quarantined: []string{"quarantined"}, Pending: "pending"}
	quarantined := makeReview("quarantined", originalReview, quarantinedReview, original.ID, quarantinedOutput)
	latest := makeReview("latest", quarantinedReview, latestReview, original.ID, latestOutput)
	pending := models.Task{ID: "pending", RolePair: original.RolePair, AmendsPlan: original.ID, Status: models.TaskStatusDraftCodingPlan,
		ReviewCommit: testhelpers.StringPtr(strings.Repeat("f", 40)), Output: []models.OutputEntry{{PlanRef: "specs/pending.md"}}}
	child := models.Task{ID: "child", ParentTasks: []string{original.ID}, Type: models.TaskTypeCoding, SpecRef: "specs/source.md"}
	state := referenceTestState(original, quarantined, latest, pending, child)
	context, err := buildResolvedReferenceContext(state.FindTask(child.ID), state, SupervisorConfig{ProjectRoot: root}, "doer")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Original carrier must remain visible.", "Recovered correction carrier must remain visible.", "Latest approved correction carrier."} {
		if !strings.Contains(context, want) {
			t.Fatalf("missing reviewed carrier %q: %s", want, context)
		}
	}
	if strings.Contains(context, "pending.md") {
		t.Fatal("future pending work entered current authority")
	}
	owners := plannerArtifactOwners(state, "plan", "specs/second.md")
	if len(owners) != 1 || owners[0].ID != original.ID {
		t.Fatalf("correction created duplicate artifact ownership: %+v", owners)
	}
	pointer := plannerArtifactPointer(owners[0], "plan", "specs/second.md", state)
	if pointer.ProducerID != original.ID || pointer.Commit != latestReview || pointer.Unresolved != "" {
		t.Fatalf("provider pointer lost original identity or effective evidence: %+v", pointer)
	}
	ranges, err := ops.PlanReviewedRanges(state, state.FindTask(original.ID))
	if err != nil || len(ranges) != 3 || ranges[0].ReviewCommit != originalReview || ranges[1].ReviewCommit != quarantinedReview || ranges[2].ReviewCommit != latestReview {
		t.Fatalf("wrong reviewed context ranges: %+v %v", ranges, err)
	}
	state.FindTask("latest").AmendsPlan = "unrelated"
	if _, err := buildResolvedReferenceContext(state.FindTask(child.ID), state, SupervisorConfig{ProjectRoot: root}, "doer"); err == nil {
		t.Fatal("forged applied lineage supplied reference authority")
	}
}
