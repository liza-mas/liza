package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestContractAmendmentExpandedSourceBoundary(t *testing.T) {
	t.Parallel()
	config, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(config)
	parent := "arch"
	original := models.Task{ID: parent, Type: models.TaskTypeArchitecture, RolePair: "architecture-pair", Status: models.TaskStatusMerged, Output: []models.OutputEntry{{Desc: "scope"}}, TransitionsExecuted: map[string]bool{"architecture-to-code-plan": true}}
	state := &models.State{Tasks: []models.Task{original, {ID: "child", ParentTask: &parent}}}
	for _, mode := range []models.PlanAmendmentMode{models.PlanAmendmentContract, models.PlanAmendmentPreserveIdentity} {
		if err := amendmentSource(state, resolver, &original, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := unusedAmendmentSource(state, resolver, &original); err == nil {
		t.Fatal("legacy amendment accepted expanded original")
	}
	original.TransitionsExecuted["replanned"] = true
	if err := amendmentSource(state, resolver, &original, models.PlanAmendmentContract); err == nil {
		t.Fatal("retired original accepted")
	}
}

func TestReplanPreservingOutputIdentityKeepsConsumerSelectors(t *testing.T) {
	t.Parallel()
	root, statePath := setupReplanTest(t)
	state := testhelpers.CreateValidState()
	state.Sprint.Status = models.SprintStatusInProgress
	parent := buildMergedPlanningTask("provider", time.Now().UTC())
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, time.Now().UTC())
	consumer.DependsOn = []string{parent.ID}
	consumer.ProviderDependencies = []models.ProviderDependency{{ProviderTask: parent.ID, Transition: "code-plan-to-coding", Outputs: []int{0}}}
	consumer.History = append(consumer.History, models.TaskHistoryEntry{Event: models.TaskEventClaimed})
	state.Tasks = []models.Task{parent, consumer}
	state.Sprint.Scope.Planned = []string{parent.ID, consumer.ID}
	testhelpers.WriteInitialState(t, statePath, state)
	result, err := Replan(root, &ReplanInput{TaskID: parent.ID, ChangedBy: "human", PreserveOutputIdentity: true, Trigger: "handoff"})
	if err != nil {
		t.Fatal(err)
	}
	after, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	original := after.FindTask(parent.ID)
	correction := after.FindTask(result.NewTaskID)
	if result.NewTaskID != "provider-replan-1" || original.TransitionsExecuted["replanned"] || original.PlanAmendment.Pending != correction.ID || correction.AmendsPlan != parent.ID || correction.AmendmentMode != models.PlanAmendmentPreserveIdentity {
		t.Fatalf("identity lost: %+v %+v", original, correction)
	}
	if !reflect.DeepEqual(&consumer, after.FindTask(consumer.ID)) {
		t.Fatal("reviewed consumer selectors changed")
	}
	if correction.PlanningChange == nil || correction.PlanningChange.Kind != models.PlanningChangeReplan {
		t.Fatal("replacement creation not attributed as replan")
	}
}

func TestStartedAcceptanceChildRetainsHistoricalReview(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed allocation"}[changed], func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			state := replacementState(t, f)
			child := state.FindTask("source")
			parent := state.FindTask("acceptance-parent")
			input, err := loadAcceptanceInput(f.root, state, child, *parent.MergeCommit)
			if err != nil || input == nil {
				t.Fatalf("initial source: %v", err)
			}
			child.AcceptanceSource = &input.source
			child.AcceptanceReceipt = &models.AcceptanceReceipt{Version: 1, Source: input.source}
			before := *child.AcceptanceSource
			receipt := child.AcceptanceReceipt
			correction := appendAcceptanceAmendment(t, f, state, "correction", changed)
			child = state.FindTask("source")
			got, err := loadAcceptanceInput(f.root, state, child, *correction.MergeCommit)
			if changed {
				if err == nil || got != nil {
					t.Fatal("changed allocation laundered into historical child")
				}
			} else if err != nil || got == nil || !reflect.DeepEqual(before, got.source) {
				t.Fatalf("unchanged receipt authority moved: %+v %v", got, err)
			}
			if !reflect.DeepEqual(before, *child.AcceptanceSource) || !reflect.DeepEqual(receipt, child.AcceptanceReceipt) {
				t.Fatal("stored acceptance evidence rewritten")
			}
		})
	}
}

func TestDirectArchitectureAcceptanceRequiresExplicitReviewedAllocation(t *testing.T) {
	for _, marked := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary architecture refused", true: "direct coding allocation accepted"}[marked], func(t *testing.T) {
			f := newAcceptanceCreationFixture(t)
			state := replacementState(t, f)
			parent := state.FindTask("acceptance-parent")
			child := state.FindTask("source")
			parent.Type = models.TaskTypeArchitecture
			child.ArchRef = "specs/architecture.md#Scope 1"
			for index := range parent.Output {
				parent.Output[index].CodingAllocation = marked
				parent.Output[index].ArchRef = child.ArchRef
			}
			input, err := loadAcceptanceInput(f.root, state, child, *parent.MergeCommit)
			if marked && (err != nil || input == nil || input.source.ParentTask != parent.ID) {
				t.Fatalf("reviewed direct architecture allocation refused: %v", err)
			}
			if !marked && (err == nil || input != nil) {
				t.Fatal("ordinary architecture bypassed planning allocation authority")
			}
		})
	}
}

func TestBoundedContractCandidateRejectsUnreferencedChanges(t *testing.T) {
	f := newAcceptanceCreationFixture(t)
	state := replacementState(t, f)
	parent := state.FindTask("acceptance-parent")
	parent.Type = models.TaskTypeArchitecture
	parent.ArchRef = "specs/contract.md#Scope 0"
	contractPath := filepath.Join(f.root, "specs/contract.md")
	originalContract := "# Contract\n\n## Scope 0\n**Boundary:** Original ownership.\n\n### CONTRACT\nOriginal contract.\n\n### Acceptance\nOriginal acceptance.\n\n## Scope 1\nOther scope.\n"
	if err := os.WriteFile(contractPath, []byte(originalContract), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "specs/contract.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: reviewed contract")
	base := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	parent.ReviewCommit, parent.MergeCommit = &base, &base
	correction := models.Task{ID: "correction", AmendsPlan: parent.ID, AmendmentMode: models.PlanAmendmentContract, BaseCommit: &base, Output: cloneAmendmentOutput(parent.Output)}
	bounded := strings.Replace(originalContract, "Original contract.", "Bounded correction.", 1)
	if err := os.WriteFile(contractPath, []byte(bounded), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "specs/contract.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: bounded correction")
	review := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	if err := validateBoundedContractCorrection(f.root, state, parent, &correction, review); err != nil {
		t.Fatalf("valid correction refused: %v", err)
	}
	parent.ArchRef = "specs/contract.md"
	if err := validateBoundedContractCorrection(f.root, state, parent, &correction, review); err == nil {
		t.Fatal("whole-document architecture reference allowed mutation")
	}
	parent.ArchRef = "specs/contract.md#Scope 0"
	if err := os.WriteFile(contractPath, []byte(strings.Replace(bounded, "Original ownership.", "Changed ownership.", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "specs/contract.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: changed scope ownership")
	outsideScope := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	if err := validateBoundedContractCorrection(f.root, state, parent, &correction, outsideScope); err == nil || !strings.Contains(err.Error(), "outside referenced contract sections") {
		t.Fatalf("scope ownership change accepted: %v", err)
	}
	// Isolate the unrelated-file refusal from the preceding ownership refusal.
	if err := os.WriteFile(contractPath, []byte(bounded), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "unrelated.txt"), []byte("outside contract\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, f.root, "add", "unrelated.txt", "specs/contract.md")
	testhelpers.MustGit(t, f.root, "commit", "-m", "test: unapproved scope")
	bad := testhelpers.MustGit(t, f.root, "rev-parse", "HEAD")
	if err := validateBoundedContractCorrection(f.root, state, parent, &correction, bad); err == nil || !strings.Contains(err.Error(), "outside referenced contract prose") {
		t.Fatalf("unbounded correction: %v", err)
	}
}
