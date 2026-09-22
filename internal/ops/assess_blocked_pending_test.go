package ops

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAssessmentPendingProgressDoesNotWake(t *testing.T) {
	for _, id := range []string{"provider", "child-a"} {
		t.Run(id, func(t *testing.T) {
			state, candidate := assessmentFingerprintFixture()
			consumer := &state.Tasks[0]
			consumer.BlockedReason, consumer.BlockedQuestions, consumer.RepairRequest = &candidate.Reason, candidate.Questions, candidate.RepairRequest
			consumer.History = append(consumer.History, models.TaskHistoryEntry{
				Event: models.TaskEventOrchestratorAssessment, Note: &candidate.Note,
				Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(state, consumer, candidate)},
			})
			for _, status := range []models.TaskStatus{models.TaskStatusImplementing, models.TaskStatusReadyForReview, models.TaskStatusReviewing, models.TaskStatusRejected, models.TaskStatusReady, models.TaskStatusImplementing, models.TaskStatusApproved} {
				provider := state.FindTask(id)
				provider.Status = status
				provider.History = append(provider.History, models.TaskHistoryEntry{Event: "progress"})
				if got := CountActionableBlockedTasks(state); got != 0 {
					t.Errorf("%s at %s woke %d already-assessed consumers", id, status, got)
				}
			}
			state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: consumer.ID})
			if got := CountActionableBlockedTasks(state); got != 1 {
				t.Errorf("targeted human note after pending churn must wake consumer, got %d", got)
			}
			state.HumanNotes = nil // Isolate the independent merge control below.
			state.FindTask(id).Status = models.TaskStatusMerged
			if got := CountActionableBlockedTasks(state); got != 1 {
				t.Errorf("provider outcome must wake the consumer, got %d", got)
			}
		})
	}
}

func TestAssessBlockedPendingProgressNoChange(t *testing.T) {
	root, stateFile := assessmentIdempotencyFixture(t)
	bb := db.For(stateFile)
	setProvider := func(status models.TaskStatus) {
		t.Helper()
		if err := bb.Modify(func(s *models.State) error {
			provider := s.FindTask("provider")
			*provider = testhelpers.BuildTaskByStatus("provider", status, provider.Created)
			provider.SpecRef = s.Goal.SpecRef
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	setProvider(models.TaskStatusReady)
	if _, err := AssessBlocked(root, "target", "await provider", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	setProvider(models.TaskStatusImplementing)
	before := metadataArtifactSnapshot(t, root)
	result, err := AssessBlocked(root, "target", "await provider", "orchestrator-1")
	assertAssessmentNoChange(t, root, before, result, err)
	state := readStateForTest(t, stateFile)
	if isTaskActionableSinceAssessment(state.FindTask("target"), state) {
		t.Fatal("wake predicate disagrees with writer after pending progress")
	}
}

func TestAssessmentArchitectureProgressDoesNotWakeOrAppend(t *testing.T) {
	// These are the architecture-pair states in internal/embedded/pipeline.yaml.
	// The code status supplies fixture fields for the equivalent lifecycle step.
	steps := []struct {
		status  models.TaskStatus
		fixture models.TaskStatus
	}{
		{"DRAFT_ARCHITECTURE", models.TaskStatusReady},
		{"ARCHITECTING", models.TaskStatusImplementing},
		{"ARCHITECTURE_TO_REVIEW", models.TaskStatusReadyForReview},
		{"REVIEWING_ARCHITECTURE", models.TaskStatusReviewing},
		{"ARCHITECTURE_REJECTED", models.TaskStatusRejected},
		{"ARCHITECTURE_APPROVED", models.TaskStatusApproved},
	}
	for _, id := range []string{"provider", "descendant"} {
		t.Run(id, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			bb := db.For(stateFile)
			if err := bb.Modify(func(s *models.State) error {
				provider := s.FindTask("provider")
				*provider = testhelpers.BuildTaskByStatus("provider", models.TaskStatusReady, provider.Created)
				provider.SpecRef = s.Goal.SpecRef
				if id == "descendant" {
					child := testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, provider.Created)
					child.ParentTasks, child.SpecRef = []string{"provider"}, s.Goal.SpecRef
					s.Tasks = append(s.Tasks, child)
				}
				task := s.FindTask(id)
				task.Type, task.RolePair, task.Status = models.TaskTypeArchitecture, "architecture-pair", steps[0].status
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := AssessBlocked(root, "target", "await architecture provider", "orchestrator-1"); err != nil {
				t.Fatal(err)
			}
			advance := func(status, fixture models.TaskStatus) {
				t.Helper()
				if err := bb.Modify(func(s *models.State) error {
					task := s.FindTask(id)
					next := testhelpers.BuildTaskByStatus(id, fixture, task.Created)
					next.Type, next.RolePair, next.Status = models.TaskTypeArchitecture, "architecture-pair", status
					next.ParentTasks, next.SpecRef = task.ParentTasks, s.Goal.SpecRef
					next.History = append(task.History, models.TaskHistoryEntry{Event: "architecture_progress"})
					*task = next
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			for _, step := range steps {
				t.Run(string(step.status), func(t *testing.T) {
					advance(step.status, step.fixture)
					state := readStateForTest(t, stateFile)
					if got := CountActionableBlockedTasks(state); got != 0 {
						t.Errorf("pending architecture progress woke %d consumers", got)
					}
					before := metadataArtifactSnapshot(t, root)
					result, err := AssessBlocked(root, "target", "await architecture provider", "orchestrator-1")
					assertAssessmentNoChange(t, root, before, result, err)
				})
			}
			advance(models.TaskStatusMerged, models.TaskStatusMerged)
			if got := CountActionableBlockedTasks(readStateForTest(t, stateFile)); got != 1 {
				t.Fatalf("architecture merge must wake consumer, got %d", got)
			}
			result, err := AssessBlocked(root, "target", "await architecture provider", "orchestrator-1")
			if err != nil || result.Outcome != models.LifecycleCompleted {
				t.Fatalf("architecture merge assessment: %+v, %v", result, err)
			}
			if got := CountActionableBlockedTasks(readStateForTest(t, stateFile)); got != 0 {
				t.Fatalf("assessed merge remains actionable: %d", got)
			}
		})
	}
}

func TestAssessmentDependencyOutcomeCoversDeclaredStatuses(t *testing.T) {
	want := map[models.TaskStatus]string{
		models.TaskStatusDraft: "pending", models.TaskStatusReady: "pending",
		models.TaskStatusImplementing: "pending", models.TaskStatusReadyForReview: "pending",
		models.TaskStatusLegacyReadyForReview: "pending", models.TaskStatusReviewing: "pending",
		models.TaskStatusRejected: "pending", models.TaskStatusApproved: "pending",
		models.TaskStatusDraftCodingPlan: "pending", models.TaskStatusCodePlanning: "pending",
		models.TaskStatusCodingPlanToReview: "pending", models.TaskStatusReviewingCodingPlan: "pending",
		models.TaskStatusCodingPlanApproved: "pending", models.TaskStatusCodingPlanRejected: "pending",
		models.TaskStatusPartiallyApproved: "pending", models.TaskStatusReviewingCode2: "pending",
		models.TaskStatusMerged: "satisfied", models.TaskStatusBlocked: "failed_or_blocked",
		models.TaskStatusAbandoned: "failed_or_blocked", models.TaskStatusIntegrationFailed: "failed_or_blocked",
		models.TaskStatusSuperseded: "failed_or_blocked",
	}
	// Read declarations so a new engine-owned outcome requires an explicit
	// decision here, even though pipeline-defined nonempty states are pending.
	source, err := parser.ParseFile(token.NewFileSet(), "../models/task.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	ast.Inspect(source, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		if len(spec.Names) == 0 || !strings.HasPrefix(spec.Names[0].Name, "TaskStatus") {
			return true
		}
		if len(spec.Names) != len(spec.Values) {
			t.Fatal("TaskStatus declaration needs an explicit classification test")
		}
		for _, value := range spec.Values {
			literal, ok := value.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Fatal("TaskStatus declaration needs an explicit classification test")
			}
			name, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			status := models.TaskStatus(name)
			outcome, ok := want[status]
			if !ok {
				t.Fatalf("declare assessment outcome for new task status %s", status)
			}
			if got := assessmentDependencyOutcome(&models.Task{Status: status}); got != outcome {
				t.Errorf("%s outcome = %s, want %s", status, got, outcome)
			}
			declared++
		}
		return true
	})
	if declared != len(want) {
		t.Fatalf("classified %d declared statuses, want %d", declared, len(want))
	}
}

func TestAssessmentProviderFailureAndRecovery(t *testing.T) {
	for _, id := range []string{"provider", "child-a"} {
		for _, failed := range []models.TaskStatus{models.TaskStatusBlocked, models.TaskStatusAbandoned, models.TaskStatusIntegrationFailed, models.TaskStatusSuperseded} {
			t.Run(id+"/"+string(failed), func(t *testing.T) {
				state, candidate := assessmentFingerprintFixture()
				consumer := state.FindTask("blocked")
				consumer.BlockedReason, consumer.BlockedQuestions, consumer.RepairRequest = &candidate.Reason, candidate.Questions, candidate.RepairRequest
				baseline := func() {
					consumer.History = append(consumer.History, models.TaskHistoryEntry{
						Event: models.TaskEventOrchestratorAssessment, Note: &candidate.Note,
						Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(state, consumer, candidate)},
					})
				}
				baseline()
				provider := state.FindTask(id)
				provider.Status = failed
				if !isTaskActionableSinceAssessment(consumer, state) {
					t.Fatal("pending-to-failed provider must wake consumer")
				}
				baseline()
				provider.Status = models.TaskStatusReady
				if !isTaskActionableSinceAssessment(consumer, state) {
					t.Fatal("provider recovery must wake consumer")
				}
				baseline()
				provider.Status = models.TaskStatusImplementing
				if isTaskActionableSinceAssessment(consumer, state) {
					t.Fatal("pending progress after recovery must stay suppressed")
				}
			})
		}
	}
}

func TestAssessmentDescendantReplacementOutcome(t *testing.T) {
	state, candidate := assessmentFingerprintFixture()
	child := state.FindTask("child-a")
	child.Status, child.SupersededBy = models.TaskStatusSuperseded, []string{"replacement"}
	state.Tasks = append(state.Tasks, models.Task{ID: "replacement", Status: models.TaskStatusReady})
	before := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
	replacement := state.FindTask("replacement")
	replacement.Status = models.TaskStatusImplementing
	if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != before {
		t.Fatal("pending replacement progress changed identity")
	}
	replacement.Status = models.TaskStatusMerged
	if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got == before {
		t.Fatal("replacement outcome outside parent traversal was lost")
	}
}

func TestAssessmentCustomPipelineProgressIsPending(t *testing.T) {
	for _, id := range []string{"provider", "child-a"} {
		t.Run(id, func(t *testing.T) {
			state, candidate := assessmentFingerprintFixture()
			provider := state.FindTask(id)
			provider.RolePair = "custom-pair"
			before := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
			for _, status := range []models.TaskStatus{"DRAFT_CUSTOM", "CUSTOM_RUNNING", "CUSTOM_REVIEW", "CUSTOM_REJECTED", "CUSTOM_APPROVED"} {
				provider.Status = status
				provider.History = append(provider.History, models.TaskHistoryEntry{Event: "custom_progress"})
				if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got != before {
					t.Errorf("pipeline-defined pending state %s changed assessment identity", status)
				}
			}
		})
	}
}

func TestAssessmentEmptyProviderStatusRemainsMaterial(t *testing.T) {
	for _, id := range []string{"provider", "child-a"} {
		t.Run(id, func(t *testing.T) {
			state, candidate := assessmentFingerprintFixture()
			provider := state.FindTask(id)
			before := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
			provider.Status = ""
			after := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
			if before == after {
				t.Fatal("empty status silently classified pending")
			}
			provider.History = append(provider.History, models.TaskHistoryEntry{Event: "progress_without_status"})
			if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got == after {
				t.Fatal("history with no status must remain material")
			}
			after = BuildAssessmentFingerprint(state, &state.Tasks[0], candidate)
			provider.Status = "CUSTOM_RUNNING"
			if got := BuildAssessmentFingerprint(state, &state.Tasks[0], candidate); got == after {
				t.Fatal("restoring a pending status must change identity")
			}
		})
	}
}
