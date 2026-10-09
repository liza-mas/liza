package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Each revision has its own real immutable Git range and independent review
// history. The initial allocation fixture supplies the earlier authored span.
func appendAcceptanceAmendment(t *testing.T, f replacementFixture, state *models.State, id string, changeAllocation bool) *models.Task {
	t.Helper()
	parent := state.FindTask("acceptance-parent")
	base := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	if changeAllocation {
		path := filepath.Join(f.root, "specs/acceptance-plan.md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content = []byte(strings.Replace(string(content), "\"timeout_seconds\":10", "\"timeout_seconds\":20", 1))
		if err := os.WriteFile(path, content, 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", "specs/acceptance-plan.md")
	} else {
		path := id + ".txt"
		if err := os.WriteFile(filepath.Join(f.root, path), []byte("Reviewed scheduling correction\n"), 0644); err != nil {
			t.Fatal(err)
		}
		testhelpers.MustGit(t, f.root, "add", path)
	}
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: review "+id)
	review := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	testhelpers.MustGit(t, f.root, "branch", "-f", "integration", review)
	planner, approver := "code-planner-1", "code-plan-reviewer-1"
	correction := models.Task{
		ID: id, Type: models.TaskTypePlanning, RolePair: parent.RolePair, Status: models.TaskStatusMerged,
		AmendsPlan: parent.ID, AssignedTo: &planner, ApprovedBy: &approver,
		BaseCommit: &base, ReviewCommit: &review, MergeCommit: &review,
		Output:    append([]models.OutputEntry(nil), parent.Output...),
		Approvals: []models.Approval{{Agent: approver, Timestamp: time.Now().UTC()}},
		History: []models.TaskHistoryEntry{
			{Event: models.TaskEventSubmittedForReview, Agent: &planner, Commit: &review},
			{Event: models.TaskEventApproved, Agent: &approver, Commit: &review},
			{Event: models.TaskEventMerged, Agent: &approver, Commit: &review},
		},
	}
	if parent.PlanAmendment == nil {
		parent.PlanAmendment = &models.PlanAmendment{OriginalOutput: append([]models.OutputEntry(nil), parent.Output...)}
	}
	parent.PlanAmendment.Corrections = append(parent.PlanAmendment.Corrections, id)
	parent.PlanAmendment.Applied = append(parent.PlanAmendment.Applied, id)
	state.Tasks = append(state.Tasks, correction)
	return state.FindTask(id)
}

func TestPlanAmendmentAcceptanceUnchangedAndRepeatedAllocations(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed=%t", changed), func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			state := replacementState(t, f)
			first := appendAcceptanceAmendment(t, f, state, "correction-1", changed)
			if !changed {
				baseSpan, _ := acceptanceCarrierSpan(f.root, *first.BaseCommit, "specs/acceptance-plan.md", "Task 1")
				reviewSpan, _ := acceptanceCarrierSpan(f.root, *first.ReviewCommit, "specs/acceptance-plan.md", "Task 1")
				if baseSpan != reviewSpan {
					t.Fatal("fixture failed to preserve the inherited allocation")
				}
			}
			for _, id := range []string{"correction-1", "correction-2"} {
				if id == "correction-2" {
					appendAcceptanceAmendment(t, f, state, id, false)
				}
				latest := state.FindTask(id)
				input, err := loadAcceptanceInput(f.root, state, state.FindTask("source"), *latest.MergeCommit)
				if err != nil || input == nil {
					t.Fatalf("%s allocation refused: %v", id, err)
				}
				if input.source.ParentTask != "acceptance-parent" || input.source.ParentReviewCommit != *latest.ReviewCommit {
					t.Fatalf("wrong effective authority: %+v", input.source)
				}
				parent, review, err := acceptanceReviewParent(f.root, state, state.FindTask("source"), "specs/acceptance-plan.md", "Task 1", *latest.MergeCommit)
				if err != nil || parent != input.source.ParentTask || review != input.source.ParentReviewCommit {
					t.Fatalf("reaffirmation disagrees: %s %s %v", parent, review, err)
				}
			}
		})
	}
}

func TestPlanAmendmentAcceptanceQuarantinedOriginRequiresFreshAppliedReview(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	appendAcceptanceAmendment(t, f, state, "quarantined", true)
	parent := state.FindTask("acceptance-parent")
	parent.PlanAmendment.Applied = nil
	parent.PlanAmendment.Quarantined = []string{"quarantined"}
	quarantined := state.FindTask("quarantined")
	if input, err := loadAcceptanceInput(f.root, state, state.FindTask("source"), *quarantined.MergeCommit); err == nil || input != nil {
		t.Fatal("quarantined correction became current authority")
	}
	appendAcceptanceAmendment(t, f, state, "recovery", false)
	recovery := state.FindTask("recovery")
	input, err := loadAcceptanceInput(f.root, state, state.FindTask("source"), *recovery.MergeCommit)
	if err != nil || input == nil || input.source.ParentReviewCommit != *recovery.ReviewCommit {
		t.Fatalf("fresh review did not retain quarantined allocation authorship: %+v %v", input, err)
	}
	// A forged historical manifest cannot claim authorship merely because its
	// committed carrier contains the allocation.
	state.FindTask("quarantined").Output[0].PlanRef = "specs/other.md#Task 1"
	if input, err := loadAcceptanceInput(f.root, state, state.FindTask("source"), *recovery.MergeCommit); err == nil || input != nil {
		t.Fatal("quarantined carrier laundered an allocation absent from its own manifest")
	}
}

func TestPlanAmendmentAcceptanceRefusesForgedEvidence(t *testing.T) {
	cases := map[string]func(*models.State){
		"wrong original": func(s *models.State) { s.FindTask("correction").AmendsPlan = "unrelated" },
		"draft applied":  func(s *models.State) { s.FindTask("correction").Status = models.TaskStatusDraftCodingPlan },
		"self approval": func(s *models.State) {
			c := s.FindTask("correction")
			c.ApprovedBy, c.Approvals = c.AssignedTo, nil
		},
		"missing submitted evidence": func(s *models.State) { s.FindTask("correction").History = s.FindTask("correction").History[1:] },
		"symbolic commit":            func(s *models.State) { s.FindTask("correction").ReviewCommit = testhelpers.StringPtr("integration") },
		"duplicate applied": func(s *models.State) {
			s.FindTask("acceptance-parent").PlanAmendment.Applied = []string{"correction", "correction"}
		},
		"misordered base": func(s *models.State) {
			s.FindTask("correction").BaseCommit = s.FindTask("acceptance-parent").BaseCommit
		},
		"latest does not allocate": func(s *models.State) { s.FindTask("correction").Output[0].Validation = []string{"sh other.sh"} },
		"original never allocated": func(s *models.State) {
			s.FindTask("acceptance-parent").PlanAmendment.OriginalOutput[0].PlanRef = "specs/other.md#Task 1"
		},
		"unreachable merge": func(s *models.State) {
			c := s.FindTask("correction")
			c.MergeCommit = testhelpers.StringPtr(strings.Repeat("f", 40))
			c.History[2].Commit = c.MergeCommit
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			state := replacementState(t, f)
			latest := appendAcceptanceAmendment(t, f, state, "correction", false)
			integration := *latest.MergeCommit
			mutate(state)
			if input, err := loadAcceptanceInput(f.root, state, state.FindTask("source"), integration); err == nil || input != nil {
				t.Fatalf("forged %s supplied authority: %+v %v", name, input, err)
			}
		})
	}
}

func TestPlanAmendmentEvidenceDigestsAndRepair(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	appendAcceptanceAmendment(t, f, state, "correction", false)
	child := state.FindTask("source")
	creationBefore, err := acceptanceParentFence(state, child)
	if err != nil {
		t.Fatal(err)
	}
	claimBefore := AcceptanceObservation(state, child)
	state.FindTask("correction").Description = "Evidence record changed during validation"
	creationAfter, err := acceptanceParentFence(state, child)
	if err != nil || creationBefore == creationAfter || claimBefore == AcceptanceObservation(state, child) {
		t.Fatal("acceptance fences ignored referenced correction mutation")
	}
	testhelpers.WriteInitialState(t, f.statePath, state)
	before := replacementBytes(t, f.statePath)
	_, err = RepairAcceptanceCommits(db.For(f.statePath), f.root, "integration")
	if err == nil || !strings.Contains(err.Error(), "amend-plan") || !reflect.DeepEqual(before, replacementBytes(t, f.statePath)) {
		t.Fatalf("unsupported repair must refuse before writing: %v", err)
	}
}
