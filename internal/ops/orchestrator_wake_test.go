package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestActionableBlockedAssessment(t *testing.T) {
	for _, reconcile := range []bool{false, true} {
		name := "note"
		if reconcile {
			name = "reconcile"
		}
		t.Run(name, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			opts := AssessBlockedOptions{}
			if reconcile {
				opts = AssessBlockedOptions{Reason: "current blocker", Questions: []string{"Who repairs it?"}, RepairRequest: &models.RepairRequest{
					Operation: "recover-task", Target: "target", Command: "repair target", Evidence: []string{"error=provider unavailable", " "}, Validation: []string{"verify target"},
				}}
			}
			result, err := AssessBlockedWithOptions(root, "target", "await provider recovery", "orchestrator-1", opts)
			if err != nil || result.Outcome != models.LifecycleCompleted {
				t.Fatalf("assessment: %+v %v", result, err)
			}
			state := readStateForTest(t, stateFile)
			task := state.FindTask("target")
			if isTaskActionableSinceAssessment(task, state) {
				t.Fatal("unchanged persisted assessment is actionable")
			}
			// Canonical fields may change independently of the historical payload.
			for _, field := range []string{"reason", "questions", "repair"} {
				t.Run(field, func(t *testing.T) {
					changed := readStateForTest(t, stateFile)
					target := changed.FindTask("target")
					switch field {
					case "reason":
						reason := "different canonical blocker"
						target.BlockedReason = &reason
					case "questions":
						target.BlockedQuestions = []string{"New question?"}
					case "repair":
						target.RepairRequest = &models.RepairRequest{Operation: "different repair"}
					}
					if !isTaskActionableSinceAssessment(target, changed) {
						t.Fatal("canonical blocker change did not wake")
					}
				})
			}
		})
	}
}

func TestActionableBlockedMaterialChanges(t *testing.T) {
	for _, kind := range []string{"own history", "dependency satisfied", "descendant", "human task", "human all", "unsatisfied dependency"} {
		t.Run(kind, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			if _, err := AssessBlocked(root, "target", "await provider", "orchestrator-1"); err != nil {
				t.Fatal(err)
			}
			before := readStateForTest(t, stateFile)
			if err := db.For(stateFile).Modify(func(state *models.State) error {
				task := state.FindTask("target")
				at := lastOrchestratorAssessment(task).Time
				switch kind {
				case "own history":
					task.History = append(task.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventBlocked})
				case "dependency satisfied":
					provider := state.FindTask("provider")
					provider.Status = models.TaskStatusMerged
					provider.History = append(provider.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventMerged})
				case "unsatisfied dependency":
					provider := state.FindTask("provider")
					provider.History = append(provider.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventBlocked})
				case "descendant":
					child := testhelpers.BuildTaskByStatus("child", models.TaskStatusReady, task.Created)
					child.ParentTasks = []string{"provider"}
					state.Tasks = append(state.Tasks, child)
				case "human task", "human all":
					target := task.ID
					if kind == "human all" {
						target = "all"
					}
					state.HumanNotes = append(state.HumanNotes, models.HumanNote{For: target, Timestamp: at, Message: "Reassess now"})
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			changed := readStateForTest(t, stateFile)
			if !isTaskActionableSinceAssessment(changed.FindTask("target"), changed) {
				t.Fatal("material change did not wake")
			}
			result, err := AssessBlocked(root, "target", "await provider", "orchestrator-1")
			if err != nil || result.Outcome != models.LifecycleCompleted {
				t.Fatalf("changed assessment: %+v %v", result, err)
			}
			after := readStateForTest(t, stateFile)
			if !TasksAssessedBetween(before, after)["target"] {
				t.Fatal("appended assessment not reported to human-note consumer")
			}
			if isTaskActionableSinceAssessment(after.FindTask("target"), after) {
				t.Fatal("recorded change remains actionable")
			}
		})
	}
}

func TestActionableBlockedLegacyBaseline(t *testing.T) {
	for _, fingerprint := range []any{nil, "malformed", 42} {
		root, stateFile := assessmentIdempotencyFixture(t)
		if err := db.For(stateFile).Modify(func(state *models.State) error {
			task := state.FindTask("target")
			note := "legacy note"
			extra := map[string]any{DependencyDescendantWakeSnapshotExtraKey: BuildDependencyDescendantWakeSnapshot(state, task)}
			if fingerprint != nil {
				extra[AssessmentFingerprintExtraKey] = fingerprint
			}
			task.History = append(task.History, models.TaskHistoryEntry{Time: task.Created, Event: models.TaskEventOrchestratorAssessment, Note: &note, Extra: extra})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before := readStateForTest(t, stateFile)
		if !isTaskActionableSinceAssessment(before.FindTask("target"), before) {
			t.Fatal("legacy or malformed fingerprint must fail open")
		}
		result, err := AssessBlocked(root, "target", "legacy note", "orchestrator-1")
		if err != nil || result.Outcome != models.LifecycleCompleted {
			t.Fatalf("baseline: %+v %v", result, err)
		}
		after := readStateForTest(t, stateFile)
		task := after.FindTask("target")
		if countOrchestratorAssessments(task) != countOrchestratorAssessments(before.FindTask("target"))+1 || isTaskActionableSinceAssessment(task, after) {
			t.Fatal("baseline must append exactly once and suppress wake")
		}
		for _, entry := range task.History {
			if _, exists := entry.Extra[DependencyDescendantWakeSnapshotExtraKey]; exists {
				t.Fatal("retired cursor remains after baseline append")
			}
		}
		if _, valid := IsAssessmentFingerprint(lastOrchestratorAssessment(task).Extra[AssessmentFingerprintExtraKey]); !valid {
			t.Fatal("baseline lacks fingerprint")
		}
		repeated, err := AssessBlocked(root, "target", "legacy note", "orchestrator-1")
		if err != nil || repeated.Outcome != models.LifecycleNoChange {
			t.Fatalf("baseline repeated: %+v %v", repeated, err)
		}
	}
}

func TestActionableBlockedMarkBlocked(t *testing.T) {
	root, stateFile := assessmentIdempotencyFixture(t)
	if _, err := AssessBlocked(root, "target", "await provider", "orchestrator-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		task := state.FindTask("target")
		// Model resumed execution before a fresh mark-blocked call.
		task.Status = models.TaskStatusImplementing
		coder := "coder-1"
		task.AssignedTo = &coder
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	result, err := MarkBlocked(root, "target", "different blocker", []string{"Who can fix it?"}, "coder-1")
	if err != nil || result.Outcome != models.LifecycleCompleted {
		t.Fatalf("mark-blocked: %+v %v", result, err)
	}
	state := readStateForTest(t, stateFile)
	if *state.FindTask("target").BlockedReason != "different blocker" || !isTaskActionableSinceAssessment(state.FindTask("target"), state) {
		t.Fatal("mark-blocked did not change blocker and wake")
	}
}

func TestActionableBlockedHypothesisPredicateUnchanged(t *testing.T) {
	state := testhelpers.CreateValidState()
	at := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	task := testhelpers.BuildTaskByStatus("target", models.TaskStatusReady, at)
	task.FailedBy = []string{"coder-1", "coder-2"}
	task.History = append(task.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventOrchestratorAssessment})
	state.Tasks = []models.Task{task}
	if CountActionableHypothesisExhaustedTasks(state) != 0 {
		t.Fatal("nonblocked hypothesis task must not require a fingerprint")
	}
	state.Tasks[0].History = append(state.Tasks[0].History, models.TaskHistoryEntry{Time: at.Add(time.Second), Event: models.TaskEventRejected})
	if CountActionableHypothesisExhaustedTasks(state) != 1 {
		t.Fatal("new rejection must still wake hypothesis exhaustion")
	}
}
