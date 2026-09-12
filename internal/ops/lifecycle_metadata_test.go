package ops

import (
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func metadataLifecycleFixture(t *testing.T, operation string) (string, string, LifecycleRequestOptions) {
	t.Helper()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("target", models.TaskStatusBlocked, now)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task, testhelpers.BuildTaskByStatus("replacement", models.TaskStatusMerged, now)}
	switch operation {
	case "assess-hypothesis-exhausted":
		state.Tasks[0].FailedBy = []string{"coder-1", "coder-2"}
	case "retarget-dependency":
		state.Tasks[0].DependsOn = []string{"old-missing-dependency"}
	case "apply-dependency-repair":
		state.Tasks[0].RepairRequest = dependencyRepairRequest([]models.DependencyUpdate{{TaskID: "target", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{"replacement"}}})
		state.Tasks[0].RepairRequest.Target = "target"
	case "repair-superseded-dependencies":
		state.Tasks[0] = testhelpers.BuildTaskByStatus("target", models.TaskStatusSuperseded, now)
		state.Tasks[0].RolePair = "code-planning-pair"
		state.Tasks[0].DependsOn = []string{"coding"}
		state.Tasks[0].SupersededBy = []string{"replacement-plan"}
		state.Tasks[0].RescopeReason = testhelpers.StringPtr("Replace old plan")
		state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("coding", models.TaskStatusReady, now), testhelpers.BuildTaskByStatus("replacement-plan", models.TaskStatusDraftCodingPlan, now))
	}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)
	stored := readStateForTest(t, stateFile)
	return root, stateFile, LifecycleRequestOptions{RequestID: "metadata-request", ExpectedTransition: models.TaskTransitionID(stored.FindTask("target"))}
}

func invokeMetadataLifecycle(root, operation, reason string, request LifecycleRequestOptions) (models.LifecycleOutcome, error) {
	switch operation {
	case "assess-blocked":
		r, err := AssessBlockedWithOptions(root, "target", reason, "orchestrator-1", AssessBlockedOptions{Request: request})
		if err != nil {
			return models.LifecycleOutcome{}, err
		}
		return r.LifecycleOutcome, nil
	case "assess-hypothesis-exhausted":
		r, err := AssessHypothesisExhaustedWithOptions(root, "target", reason, "orchestrator-1", request)
		if err != nil {
			return models.LifecycleOutcome{}, err
		}
		return r.LifecycleOutcome, nil
	case "retarget-dependency":
		r, err := RetargetDependencyWithOptions(root, "target", "old-missing-dependency", []string{"replacement"}, reason, "orchestrator-1", request)
		if err != nil {
			return models.LifecycleOutcome{}, err
		}
		return r.LifecycleOutcome, nil
	case "apply-dependency-repair":
		r, err := ApplyDependencyRepairWithOptions(root, "target", reason, "orchestrator-1", request)
		if err != nil {
			return models.LifecycleOutcome{}, err
		}
		return r.LifecycleOutcome, nil
	case "repair-superseded-dependencies":
		r, err := RepairSupersededDependenciesWithOptions(root, "target", reason, "orchestrator-1", request)
		if err != nil {
			return models.LifecycleOutcome{}, err
		}
		return r.LifecycleOutcome, nil
	default:
		return models.LifecycleOutcome{}, fmt.Errorf("unsupported test operation %s", operation)
	}
}

func metadataArtifactSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	lp := paths.New(root)
	result := make(map[string]string)
	for _, filename := range []string{lp.StatePath(), lp.LogPath(), lp.AlertsLogPath()} {
		data, err := os.ReadFile(filename)
		if errors.Is(err, os.ErrNotExist) {
			result[filename] = "missing"
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		result[filename] = string(data)
	}
	return result
}

func assertMetadataArtifactsUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	for path, after := range metadataArtifactSnapshot(t, root) {
		if before[path] != after {
			t.Fatalf("retry modified artifact %s", path)
		}
	}
}

func TestLifecycleMetadataExactReplayAndPayloadConflict(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"assess-blocked", "assess-hypothesis-exhausted", "retarget-dependency", "apply-dependency-repair", "repair-superseded-dependencies"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			root, stateFile, request := metadataLifecycleFixture(t, operation)
			first, err := invokeMetadataLifecycle(root, operation, "inspected repair", request)
			if err != nil || first.Outcome != models.LifecycleCompleted || first.CompletedTransitionID == "" {
				t.Fatalf("first invocation: %+v %v", first, err)
			}
			before := metadataArtifactSnapshot(t, root)
			beforeInfo, err := os.Stat(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := invokeMetadataLifecycle(root, operation, "inspected repair", request)
			if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted || replay.CompletedTransitionID != first.CompletedTransitionID || replay.SafeAction != "stop" {
				t.Fatalf("exact replay: %+v %v", replay, err)
			}
			assertMetadataArtifactsUnchanged(t, root, before)
			afterInfo, err := os.Stat(stateFile)
			if err != nil || !os.SameFile(beforeInfo, afterInfo) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatalf("replay replaced or rewrote state file: %v", err)
			}
			_, err = invokeMetadataLifecycle(root, operation, "different intent", request)
			var conflict *LifecycleError
			if !errors.As(err, &conflict) || conflict.Outcome.Outcome != models.LifecycleInvalidInput || conflict.Outcome.SafeAction != "correct_input" {
				t.Fatalf("conflicting request accepted or misclassified: %v", err)
			}
			assertMetadataArtifactsUnchanged(t, root, before)
			metrics := ReadLifecycleOutcomes(root, CaptureLifecycleSprint(readStateForTest(t, stateFile).Sprint))
			if !metrics.Available || metrics.Counts[operation][models.LifecycleCompleted] != 1 || metrics.Counts[operation][models.LifecycleAlreadyCompleted] != 1 || metrics.Counts[operation][models.LifecycleInvalidInput] != 1 {
				t.Fatalf("outer outcome counters missing or counted more than once: %+v", metrics)
			}
			stored := readStateForTest(t, stateFile).FindTask("target")
			if stored.Lifecycle.Revision != 1 || len(stored.Lifecycle.Receipts) != 1 {
				t.Fatalf("retry advanced metadata: %+v", stored.Lifecycle)
			}
			if operation == "apply-dependency-repair" {
				if stored.RepairRequest != nil || stored.History[len(stored.History)-1].Extra["repair_request_digest"] == nil {
					t.Fatal("consumed repair identity not retained before clearing")
				}
			}
		})
	}
}

func TestLifecycleMetadataExpiredAssessmentDoesNotRepeat(t *testing.T) {
	t.Parallel()
	root, stateFile, original := metadataLifecycleFixture(t, "assess-blocked")
	for i := 0; i <= models.LifecycleReceiptsPerOperation; i++ {
		request := original
		if i > 0 {
			state := readStateForTest(t, stateFile)
			request = LifecycleRequestOptions{RequestID: fmt.Sprintf("assessment-%d", i), ExpectedTransition: models.TaskTransitionID(state.FindTask("target"))}
		}
		if _, err := invokeMetadataLifecycle(root, "assess-blocked", "inspected repair", request); err != nil {
			t.Fatal(err)
		}
	}
	before := metadataArtifactSnapshot(t, root)
	_, err := invokeMetadataLifecycle(root, "assess-blocked", "inspected repair", original)
	var expired *LifecycleError
	if !errors.As(err, &expired) || expired.Outcome.Outcome != models.LifecycleStateChanged || expired.Outcome.SafeAction != "requery" {
		t.Fatalf("expired request did not requery: %v", err)
	}
	assertMetadataArtifactsUnchanged(t, root, before)
}

func TestLifecycleMetadataStaleDependencyExpectationDoesNotMutate(t *testing.T) {
	t.Parallel()
	root, stateFile, request := metadataLifecycleFixture(t, "apply-dependency-repair")
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		state.FindTask("target").DependsOn = []string{"replacement"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Dependency expectations are checked even when legacy metadata changed
	// without updating the task's transition history/token.
	before := metadataArtifactSnapshot(t, root)
	_, err := invokeMetadataLifecycle(root, "apply-dependency-repair", "repair", request)
	var changed *LifecycleError
	if !errors.As(err, &changed) || changed.Outcome.Outcome != models.LifecycleStateChanged || changed.Outcome.SafeAction != "requery" || changed.Outcome.Effects != "none" {
		t.Fatalf("dependency race did not requery without effects: %v", err)
	}
	assertMetadataArtifactsUnchanged(t, root, before)
}
