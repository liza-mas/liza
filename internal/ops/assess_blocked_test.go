package ops

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/payloadschema"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAssessBlockedSchemaParity(t *testing.T) {
	t.Parallel()
	repairWith := func(change func(*models.RepairRequest)) *models.RepairRequest {
		repair := testAssessBlockedRepairRequest()
		if change != nil {
			change(repair)
		}
		return repair
	}
	reconcile := func(repair *models.RepairRequest) AssessBlockedOptions {
		return AssessBlockedOptions{Reason: "provider unavailable", Questions: []string{"Retry?"}, RepairRequest: repair}
	}
	// F1-F15 mirror the reviewed plan's shared table, including both
	// history-only forms and repair-only reconciliation.
	tests := []struct {
		id, taskID, note string
		opts             AssessBlockedOptions
		fields           []string
	}{
		{"F1", "target", "still blocked", AssessBlockedOptions{}, nil},
		{"F2", "target", "", AssessBlockedOptions{}, nil},
		{"F3", "target", "", reconcile(nil), nil},
		{"F4", "target", "", AssessBlockedOptions{Reason: "blocked", Questions: []string{"q1", "q2", "q3"}, RepairRequest: repairWith(nil)}, nil},
		{"F5", "", "blocked", AssessBlockedOptions{}, []string{"/task_id"}},
		{"F6", "target", "", AssessBlockedOptions{RepairRequest: repairWith(nil)}, []string{"/reason", "/questions"}},
		{"F7", "target", "", AssessBlockedOptions{Reason: "blocked"}, []string{"/questions"}},
		{"F8", "target", "", AssessBlockedOptions{Questions: []string{"q"}}, []string{"/reason"}},
		{"F9", "target", "", AssessBlockedOptions{Reason: "blocked", Questions: []string{"q1", "q2", "q3", "q4"}}, []string{"/questions"}},
		{"F10", "target", "", AssessBlockedOptions{Reason: "blocked", Questions: []string{"  "}}, []string{"/questions/0"}},
		{"F11", "target", "", reconcile(repairWith(func(r *models.RepairRequest) { r.Operation = "" })), []string{"/repair_request/operation"}},
		{"F12", "target", "", reconcile(repairWith(func(r *models.RepairRequest) { r.Evidence = nil })), []string{"/repair_request/evidence"}},
		{"F13", "target", "", reconcile(repairWith(func(r *models.RepairRequest) { r.Validation = nil })), []string{"/repair_request/validation"}},
		{"F14", "target", "", reconcile(repairWith(func(r *models.RepairRequest) {
			r.DependencyUpdates = []models.DependencyUpdate{{TaskID: "target", ExpectedDependsOn: []string{}, DesiredDependsOn: []string{}}}
		})), []string{"/repair_request/command"}},
		{"F15", "target", "", reconcile(repairWith(func(r *models.RepairRequest) { r.Command = "" })), []string{"/repair_request/command"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			before := metadataArtifactSnapshot(t, root)
			payload := payloadschema.AssessBlockedPayload(tt.taskID, tt.note, tt.opts.Reason, tt.opts.Questions, tt.opts.RepairRequest, tt.opts.AwaitedTasks)
			version, diagnostics, err := payloadschema.Validate("assess-blocked", payload)
			if err != nil || version != 1 {
				t.Fatalf("preflight version=%d error=%v", version, err)
			}
			var fields []string
			for _, diagnostic := range diagnostics {
				fields = append(fields, diagnostic.Field)
			}
			if !reflect.DeepEqual(fields, tt.fields) {
				t.Fatalf("preflight fields=%v, want %v", fields, tt.fields)
			}
			result, err := AssessBlockedWithOptions(root, tt.taskID, tt.note, "orchestrator-1", tt.opts)
			if len(tt.fields) == 0 {
				if err != nil || result == nil || result.Outcome != models.LifecycleCompleted {
					t.Fatalf("valid assessment rejected: result=%+v error=%v", result, err)
				}
				if lastOrchestratorAssessment(readAssessBlockedTask(t, stateFile, tt.taskID)) == nil {
					t.Fatal("accepted assessment was not persisted")
				}
				return
			}
			var lifecycle *LifecycleError
			if result != nil || !stderrors.As(err, &lifecycle) {
				t.Fatalf("invalid assessment: result=%+v error=%v", result, err)
			}
			if lifecycle.Outcome.Outcome != models.LifecycleInvalidInput || lifecycle.Outcome.SafeAction != "correct_input" || lifecycle.Outcome.Effects != "none" {
				t.Fatalf("invalid-input contract: %+v", lifecycle.Outcome)
			}
			if !reflect.DeepEqual(lifecycle.Outcome.Diagnostics, diagnostics) {
				t.Fatalf("mutation diagnostics=%+v, preflight=%+v", lifecycle.Outcome.Diagnostics, diagnostics)
			}
			assertMetadataArtifactsUnchanged(t, root, before)
		})
	}
	t.Run("unregistered operation fails loudly", func(t *testing.T) {
		err := rejectInvalidLifecyclePayload("unregistered-assess-blocked", payloadschema.AssessBlockedPayload("target", "", "", nil, nil, nil))
		if !stderrors.Is(err, payloadschema.ErrUnknownOperation) {
			t.Fatalf("unknown operation error=%v", err)
		}
	})
}

func TestAssessBlockedRejectsBeforeLock(t *testing.T) {
	t.Parallel()
	for _, entrypoint := range []string{"options", "authority"} {
		t.Run(entrypoint, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			before := metadataArtifactSnapshot(t, root)
			t.Cleanup(setLifecycleMutationTestHook(db.For(stateFile), func() { t.Error("invalid payload reached mutation") }))
			// Oversized note is accepted by the old preconditions: only the
			// registry can reject it. Hold the real lock to catch early reads,
			// including telemetry's sprint capture, as well as mutation.
			note := strings.Repeat("x", 4097)
			err := taskSchemaCallWithStateLocked(t, stateFile, func() error {
				if entrypoint == "authority" {
					_, err := AssessBlockedWithAuthority(root, "target", note, models.AgentAuthority{ID: "orchestrator-1"}, AssessBlockedOptions{})
					return err
				}
				_, err := AssessBlockedWithOptions(root, "target", note, "orchestrator-1", AssessBlockedOptions{})
				return err
			})
			var lifecycle *LifecycleError
			if !stderrors.As(err, &lifecycle) || lifecycle.Outcome.Outcome != models.LifecycleInvalidInput || lifecycle.Outcome.SafeAction != "correct_input" || lifecycle.Outcome.Effects != "none" {
				t.Fatalf("invalid-input error=%v", err)
			}
			_, diagnostics, schemaErr := payloadschema.Validate("assess-blocked", payloadschema.AssessBlockedPayload("target", note, "", nil, nil, nil))
			if schemaErr != nil || len(diagnostics) != 1 || diagnostics[0].Field != "/note" || !reflect.DeepEqual(lifecycle.Outcome.Diagnostics, diagnostics) {
				t.Fatalf("mutation diagnostics=%+v, preflight=%+v error=%v", lifecycle.Outcome.Diagnostics, diagnostics, schemaErr)
			}
			assertMetadataArtifactsUnchanged(t, root, before)
		})
	}
}

func assessmentIdempotencyFixture(t *testing.T) (string, string) {
	t.Helper()
	root, stateFile, _ := metadataLifecycleFixture(t, "assess-blocked")
	if err := db.For(stateFile).Modify(func(state *models.State) error {
		task := state.FindTask("target")
		task.AssignedTo, task.Worktree = nil, nil
		task.DependsOn = []string{"provider"}
		state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus("provider", models.TaskStatusBlocked, task.Created))
		setTaskSpecRefs(state)
		state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, stateFile
}

func assertAssessmentNoChange(t *testing.T, root string, before map[string]string, result *AssessBlockedResult, err error) {
	t.Helper()
	if err != nil || result == nil {
		t.Fatalf("assessment: result=%+v error=%v", result, err)
	}
	if result.Outcome != models.LifecycleNoChange || result.SafeAction != "stop" || result.Effects != "none" || result.Changed == nil || *result.Changed {
		t.Fatalf("no-change contract: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	avoided, ok := fields["suppressed_entry_bytes"].(float64)
	if !ok || avoided <= 0 || fields["changed"] != false {
		t.Fatalf("missing suppression evidence: %s", encoded)
	}
	assertMetadataArtifactsUnchanged(t, root, before)
}

func TestAssessBlockedIdempotent(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	first, err := AssessBlocked(root, "target", "await provider", "orchestrator-1")
	if err != nil || first.Outcome != models.LifecycleCompleted {
		t.Fatalf("first assessment: %+v %v", first, err)
	}
	before := metadataArtifactSnapshot(t, root)
	stored := readStateForTest(t, stateFile).FindTask("target")
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", AssessBlockedOptions{
		Request: LifecycleRequestOptions{RequestID: "new-equivalent-request", ExpectedTransition: models.TaskTransitionID(stored)},
	})
	assertAssessmentNoChange(t, root, before, second, err)
	afterInfo, err := os.Stat(stateFile)
	if err != nil || !os.SameFile(info, afterInfo) || !info.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("no-change rewrote state file: %v", err)
	}
	after := readStateForTest(t, stateFile).FindTask("target")
	if len(after.History) != len(stored.History) || second.TransitionID != models.TaskTransitionID(stored) {
		t.Fatal("no-change advanced history or transition")
	}
	metrics := ReadLifecycleOutcomes(root, CaptureLifecycleSprint(readStateForTest(t, stateFile).Sprint))
	if !metrics.Available || metrics.Counts["assess-blocked"][models.LifecycleCompleted] != 1 || metrics.Counts["assess-blocked"][models.LifecycleNoChange] != 1 {
		t.Fatalf("outer outcome counters: %+v", metrics)
	}
}

func TestAssessBlockedIdempotentNormalization(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	var repair models.RepairRequest
	if err := json.Unmarshal([]byte(`{"operation":"recover-task","target":"target","command":"repair target","evidence":["error=provider unavailable","  "],"validation":["verify target"]}`), &repair); err != nil {
		t.Fatal(err)
	}
	opts := AssessBlockedOptions{Reason: "provider unavailable", Questions: []string{"Can it recover?"}, RepairRequest: &repair}
	first, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", opts)
	if err != nil || first.Outcome != models.LifecycleCompleted {
		t.Fatalf("initial reconcile: %+v %v", first, err)
	}
	stored := readStateForTest(t, stateFile)
	task := stored.FindTask("target")
	if !reflect.DeepEqual(task.RepairRequest.Evidence, []string{"error=provider unavailable"}) {
		t.Fatalf("repair not normalized: %+v", task.RepairRequest)
	}
	want := BuildAssessmentFingerprint(stored, task, AssessmentFingerprintCandidate{
		Reason: *task.BlockedReason, Questions: task.BlockedQuestions, RepairRequest: task.RepairRequest, Note: "await provider",
	})
	if got := lastOrchestratorAssessment(task).Extra[AssessmentFingerprintExtraKey]; got != want {
		t.Fatalf("writer fingerprint=%v, durable candidate=%s", got, want)
	}
	before := metadataArtifactSnapshot(t, root)
	repeat, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", opts)
	assertAssessmentNoChange(t, root, before, repeat, err)
	// Key order, whitespace, and the blank entry removed by normalization
	// cannot distinguish this payload from the original reconcile request.
	if err := json.Unmarshal([]byte(`{"validation":[" verify target "],"evidence":["error=provider   unavailable"],"command":"repair   target","target":"target","operation":"recover-task"}`), &repair); err != nil {
		t.Fatal(err)
	}
	opts.Reason, opts.Questions = " provider\n unavailable ", []string{" Can  it recover? "}
	repeat, err = AssessBlockedWithOptions(root, "target", " await\tprovider ", "orchestrator-1", opts)
	assertAssessmentNoChange(t, root, before, repeat, err)
	// Reconstruct the complete state through YAML and a fresh disk read.
	data, err := yaml.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var restored models.State
	if err := yaml.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	testhelpers.WriteInitialState(t, stateFile, &restored)
	before = metadataArtifactSnapshot(t, root)
	repeat, err = AssessBlocked(root, "target", "await provider", "orchestrator-1")
	assertAssessmentNoChange(t, root, before, repeat, err)
}

func TestAssessBlockedIdempotentMaterialChanges(t *testing.T) {
	t.Parallel()
	for _, change := range []struct {
		name  string
		apply func(*models.State, *AssessBlockedOptions)
	}{
		{"dependency lifecycle", func(s *models.State, _ *AssessBlockedOptions) {
			provider := s.FindTask("provider")
			*provider = testhelpers.BuildTaskByStatus("provider", models.TaskStatusMerged, provider.Created)
			provider.SpecRef = s.Goal.SpecRef
		}},
		{"reason", func(_ *models.State, o *AssessBlockedOptions) { o.Reason = "provider retired" }},
		{"question", func(_ *models.State, o *AssessBlockedOptions) { o.Questions = []string{"Can we replace it?"} }},
		{"repair", func(_ *models.State, o *AssessBlockedOptions) {
			o.RepairRequest = &models.RepairRequest{Operation: "recover-task", Target: "target", Command: "repair target", Evidence: []string{"error=provider retired"}, Validation: []string{"verify target"}}
		}},
		{"human note", func(s *models.State, _ *AssessBlockedOptions) {
			s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: "target", Message: "reassess", Timestamp: time.Now().UTC()})
		}},
		{"dependency-free rejection RCA", func(s *models.State, _ *AssessBlockedOptions) {
			task := s.FindTask("target")
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejectionRCARecorded})
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			if change.name == "dependency-free rejection RCA" {
				if err := db.For(stateFile).Modify(func(s *models.State) error { s.FindTask("target").DependsOn = nil; return nil }); err != nil {
					t.Fatal(err)
				}
			}
			opts := AssessBlockedOptions{Reason: "provider unavailable", Questions: []string{"Can it recover?"}}
			if _, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", opts); err != nil {
				t.Fatal(err)
			}
			if err := db.For(stateFile).Modify(func(s *models.State) error { change.apply(s, &opts); return nil }); err != nil {
				t.Fatal(err)
			}
			before := readStateForTest(t, stateFile).FindTask("target")
			result, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", opts)
			if err != nil || result.Outcome != models.LifecycleCompleted {
				t.Fatalf("material change: %+v %v", result, err)
			}
			after := readStateForTest(t, stateFile).FindTask("target")
			if len(after.History) != len(before.History)+1 {
				t.Fatal("material change did not append exactly once")
			}
			for _, entry := range after.History[:len(after.History)-1] {
				if _, exists := entry.Extra[AssessmentFingerprintExtraKey]; exists {
					t.Fatal("superseded fingerprint retained")
				}
			}
			artifacts := metadataArtifactSnapshot(t, root)
			repeat, err := AssessBlockedWithOptions(root, "target", "await provider", "orchestrator-1", opts)
			assertAssessmentNoChange(t, root, artifacts, repeat, err)
		})
	}
}

func TestAssessBlockedIdempotentLegacyBaseline(t *testing.T) {
	t.Parallel()
	for _, value := range []any{nil, "bad", 42, strings.Repeat("A", 64)} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			if err := db.For(stateFile).Modify(func(s *models.State) error {
				task := s.FindTask("target")
				extra := map[string]any{"keep": "audit"}
				if value != nil {
					extra[AssessmentFingerprintExtraKey] = value
				}
				task.History = append(task.History, models.TaskHistoryEntry{Time: task.Created, Event: models.TaskEventOrchestratorAssessment, Extra: extra})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := readStateForTest(t, stateFile).FindTask("target")
			result, err := AssessBlocked(root, "target", "await provider", "orchestrator-1")
			if err != nil || result.Outcome != models.LifecycleCompleted {
				t.Fatalf("legacy: %+v %v", result, err)
			}
			after := readStateForTest(t, stateFile).FindTask("target")
			if len(after.History) != len(before.History)+1 || after.History[len(before.History)-1].Extra["keep"] != "audit" {
				t.Fatal("legacy baseline damaged history")
			}
			artifacts := metadataArtifactSnapshot(t, root)
			result, err = AssessBlocked(root, "target", "await provider", "orchestrator-1")
			assertAssessmentNoChange(t, root, artifacts, result, err)
		})
	}
}

func TestAssessBlockedIdempotentConcurrent(t *testing.T) {
	t.Parallel()
	root, stateFile := assessmentIdempotencyFixture(t)
	before := readStateForTest(t, stateFile).FindTask("target")
	entered, release := make(chan struct{}, 2), make(chan struct{})
	t.Cleanup(setLifecycleMutationTestHook(db.For(stateFile), func() { entered <- struct{}{}; <-release }))
	type attempt struct {
		result *AssessBlockedResult
		err    error
	}
	results := make(chan attempt, 2)
	for range 2 {
		go func() {
			result, err := AssessBlockedWithAuthority(root, "target", "await provider", models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration}, AssessBlockedOptions{})
			results <- attempt{result, err}
		}()
	}
	<-entered
	<-entered
	close(release)
	outcomes := map[string]int{}
	for range 2 {
		a := <-results
		if a.err != nil {
			t.Fatal(a.err)
		}
		outcomes[a.result.Outcome]++
	}
	if outcomes[models.LifecycleCompleted] != 1 || outcomes[models.LifecycleNoChange] != 1 {
		t.Fatalf("concurrent outcomes: %v", outcomes)
	}
	after := readStateForTest(t, stateFile).FindTask("target")
	if len(after.History) != len(before.History)+1 {
		t.Fatal("concurrent calls appended more than once")
	}
	alerts, err := os.ReadFile(paths.New(root).AlertsLogPath())
	if err != nil || strings.Count(string(alerts), "UNRESOLVED BLOCKED") != 1 {
		t.Fatalf("alerts: %s %v", alerts, err)
	}
}

func TestAssessBlockedIdempotentWakeSuperset(t *testing.T) {
	t.Parallel()
	for _, change := range []struct {
		name  string
		apply func(*models.State, time.Time)
	}{
		{"own history", func(s *models.State, at time.Time) {
			task := s.FindTask("target")
			task.History = append(task.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventRejectionRCARecorded})
		}},
		{"dependency satisfaction", func(s *models.State, at time.Time) {
			task := s.FindTask("provider")
			task.Status = models.TaskStatusMerged
			task.History = append(task.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventMerged})
		}},
		{"descendant creation", func(s *models.State, at time.Time) {
			task := testhelpers.BuildTaskByStatus("child", models.TaskStatusReady, at)
			task.ParentTasks = []string{"provider"}
			s.Tasks = append(s.Tasks, task)
		}},
		{"human instruction", func(s *models.State, at time.Time) {
			s.HumanNotes = append(s.HumanNotes, models.HumanNote{For: "all", Message: "reassess", Timestamp: at})
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, stateFile := assessmentIdempotencyFixture(t)
			if _, err := AssessBlocked(root, "target", "await provider", "orchestrator-1"); err != nil {
				t.Fatal(err)
			}
			state := readStateForTest(t, stateFile)
			task := state.FindTask("target")
			entry := lastOrchestratorAssessment(task)
			candidate := AssessmentFingerprintCandidate{Reason: *task.BlockedReason, Questions: task.BlockedQuestions, RepairRequest: task.RepairRequest, Note: *entry.Note}
			before := BuildAssessmentFingerprint(state, task, candidate)
			if isTaskActionableSinceAssessment(task, state) {
				t.Fatal("unchanged baseline already actionable")
			}
			change.apply(state, entry.Time.Add(time.Second))
			task = state.FindTask("target")
			if !isTaskActionableSinceAssessment(task, state) {
				t.Fatal("fixture did not trigger existing wake condition")
			}
			if after := BuildAssessmentFingerprint(state, task, candidate); after == before {
				t.Fatal("wake condition failed to change fingerprint")
			}
		})
	}
}

func TestAssessBlocked_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		agentID     string
		errContains string
	}{
		{
			name:        "empty task ID",
			agentID:     "orchestrator-1",
			errContains: "/task_id must not be empty",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-1",
			errContains: "agent ID is required",
		},
		{
			name:        "non-orchestrator agent ID",
			taskID:      "task-1",
			agentID:     "coder-1",
			errContains: "only orchestrator agents can assess blocked tasks",
		},
		{
			name:        "reviewer agent ID",
			taskID:      "task-1",
			agentID:     "code-reviewer-1",
			errContains: "only orchestrator agents can assess blocked tasks",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AssessBlocked("/nonexistent", tt.taskID, "", tt.agentID)
			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("Error = %q, want to contain %q", err.Error(), tt.errContains)
			}
		})
	}
}

func TestAssessBlocked_TaskNotFound(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "nonexistent", "", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for nonexistent task")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestAssessBlocked_WrongStatus(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "task-1", "", "orchestrator-1")
	if err == nil {
		t.Fatal("Expected error for non-BLOCKED task")
	}
	if !strings.Contains(err.Error(), "BLOCKED status") {
		t.Errorf("Error = %q, want to contain 'BLOCKED status'", err.Error())
	}
}

func TestAssessBlocked_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := AssessBlocked(tmpDir, "task-1", "Cannot resolve without external API", "orchestrator-1")
	if err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	if result.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", result.TaskID, "task-1")
	}

	// Verify history entry
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	if task == nil {
		t.Fatal("Task not found")
	}
	// Status should remain BLOCKED
	if task.Status != models.TaskStatusBlocked {
		t.Errorf("Status = %v, want BLOCKED", task.Status)
	}

	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Event != models.TaskEventOrchestratorAssessment {
		t.Errorf("History event = %q, want %q", lastHistory.Event, models.TaskEventOrchestratorAssessment)
	}
	if lastHistory.Agent == nil || *lastHistory.Agent != "orchestrator-1" {
		t.Errorf("Expected agent orchestrator-1 in history, got %v", lastHistory.Agent)
	}
	if lastHistory.Note == nil || *lastHistory.Note != "Cannot resolve without external API" {
		t.Errorf("Expected note in history, got %v", lastHistory.Note)
	}

	data, err := os.ReadFile(paths.New(tmpDir).AlertsLogPath())
	if err != nil {
		t.Fatalf("Read alerts.log: %v", err)
	}
	if !strings.Contains(string(data), "UNRESOLVED BLOCKED: task-1 — Cannot resolve without external API") {
		t.Fatalf("alerts.log missing unresolved alert:\n%s", string(data))
	}
}

func TestAssessBlocked_SuccessWithoutNote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessBlocked(tmpDir, "task-1", "", "orchestrator-1")
	if err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	lastHistory := task.History[len(task.History)-1]
	if lastHistory.Note != nil {
		t.Errorf("Expected nil note, got %v", lastHistory.Note)
	}
}

func TestAssessBlocked_Idempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// First assessment
	_, err := AssessBlocked(tmpDir, "task-1", "first", "orchestrator-1")
	if err != nil {
		t.Fatalf("First AssessBlocked() error: %v", err)
	}

	// Second assessment
	_, err = AssessBlocked(tmpDir, "task-1", "second", "orchestrator-1")
	if err != nil {
		t.Fatalf("Second AssessBlocked() error: %v", err)
	}

	// Verify two entries
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}

	task := readState.FindTask("task-1")
	assessmentCount := 0
	for _, entry := range task.History {
		if entry.Event == models.TaskEventOrchestratorAssessment {
			assessmentCount++
		}
	}
	if assessmentCount != 2 {
		t.Errorf("Expected 2 assessment entries, got %d", assessmentCount)
	}
}

func TestAssessBlocked_ReconcilesCanonicalMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		repairRequest     *models.RepairRequest
		wantRepairRequest *models.RepairRequest
	}{
		{
			name: "clears prior repair request",
		},
		{
			name: "normalizes declarative replacement",
			repairRequest: &models.RepairRequest{
				Operation: "  " + models.RepairOperationApplyDependencyRepair + "  ",
				Target:    " task-1 ",
				DependencyUpdates: []models.DependencyUpdate{
					{
						TaskID:            " task-1 ",
						ExpectedDependsOn: []string{" old-dependency "},
						DesiredDependsOn:  []string{" replacement-dependency "},
					},
				},
				Evidence:   []string{"", " error=provider unavailable "},
				Validation: []string{" verify repaired dependency graph "},
			},
			wantRepairRequest: &models.RepairRequest{
				Operation: models.RepairOperationApplyDependencyRepair,
				Target:    "task-1",
				DependencyUpdates: []models.DependencyUpdate{
					{
						TaskID:            "task-1",
						ExpectedDependsOn: []string{"old-dependency"},
						DesiredDependsOn:  []string{"replacement-dependency"},
					},
				},
				Evidence:   []string{"error=provider unavailable"},
				Validation: []string{"verify repaired dependency graph"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
			now := time.Now().UTC()
			state := testhelpers.CreateValidState()
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
			task.AssignedTo = nil
			oldReason := "obsolete blocker"
			task.BlockedReason = &oldReason
			task.BlockedQuestions = []string{"obsolete question"}
			task.RepairRequest = testAssessBlockedRepairRequest()
			state.Tasks = []models.Task{task}
			setTaskSpecRefs(state)
			testhelpers.WriteInitialState(t, stateFile, state)

			result, err := AssessBlockedWithOptions(
				tmpDir,
				"task-1",
				"remaining blocker reassessed",
				"orchestrator-1",
				AssessBlockedOptions{
					Reason:        "current blocker",
					Questions:     []string{"first current question", "second current question"},
					RepairRequest: tt.repairRequest,
				},
			)
			if err != nil {
				t.Fatalf("AssessBlockedWithOptions() error: %v", err)
			}

			if result.TaskID != "task-1" || result.Reason != "current blocker" {
				t.Fatalf("result identity/reason = %#v, want task-1/current blocker", result)
			}
			if !reflect.DeepEqual(result.Questions, []string{"first current question", "second current question"}) {
				t.Errorf("result questions = %#v", result.Questions)
			}
			if !reflect.DeepEqual(result.RepairRequest, tt.wantRepairRequest) {
				t.Errorf("result repair request = %#v, want %#v", result.RepairRequest, tt.wantRepairRequest)
			}

			readState, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("Read() error: %v", err)
			}
			got := readState.FindTask("task-1")
			if got.Status != models.TaskStatusBlocked {
				t.Errorf("status = %s, want BLOCKED", got.Status)
			}
			if got.BlockedReason == nil || *got.BlockedReason != "current blocker" {
				t.Errorf("blocked reason = %v, want current blocker", got.BlockedReason)
			}
			if !reflect.DeepEqual(got.BlockedQuestions, []string{"first current question", "second current question"}) {
				t.Errorf("blocked questions = %#v", got.BlockedQuestions)
			}
			if !reflect.DeepEqual(got.RepairRequest, tt.wantRepairRequest) {
				t.Errorf("canonical repair request = %#v, want %#v", got.RepairRequest, tt.wantRepairRequest)
			}

			last := got.History[len(got.History)-1]
			if last.Event != models.TaskEventOrchestratorAssessment || last.Reason == nil || *last.Reason != "current blocker" {
				t.Errorf("assessment history = %#v", last)
			}
			if !reflect.DeepEqual(last.Extra["blocked_questions"], []any{"first current question", "second current question"}) {
				t.Errorf("history blocked_questions = %#v", last.Extra["blocked_questions"])
			}
			_, hasRepairRequest := last.Extra["repair_request"]
			if !hasRepairRequest {
				t.Error("assessment history must record resulting repair_request state")
			}
		})
	}
}

func TestAssessBlocked_RecordsDependencyDescendantWakeSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	baseTime := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	blocked := testhelpers.BuildTaskByStatus("blocked-evaluator", models.TaskStatusBlocked, baseTime)
	blocked.AssignedTo = nil
	blocked.DependsOn = []string{"provider-plan"}
	oldReason := "existing reason"
	blocked.BlockedReason = &oldReason
	blocked.BlockedQuestions = []string{"existing question"}
	blocked.RepairRequest = testAssessBlockedRepairRequest()
	provider := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, baseTime)
	zChild := testhelpers.BuildTaskByStatus("z-child", models.TaskStatusReady, baseTime)
	zChild.ParentTasks = []string{"provider-plan"}
	aChild := testhelpers.BuildTaskByStatus("a-child", models.TaskStatusReady, baseTime)
	aChild.ParentTasks = []string{"provider-plan"}
	state.Tasks = []models.Task{blocked, provider, zChild, aChild}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	wantRaw := BuildDependencyDescendantWakeSnapshot(state, &state.Tasks[0])
	want, ok := NormalizeDependencyDescendantWakeSnapshot(wantRaw)
	if !ok {
		t.Fatalf("NormalizeDependencyDescendantWakeSnapshot(producer value) rejected %#v", wantRaw)
	}
	if len(want) != 2 || want[0].TaskID != "a-child" || want[1].TaskID != "z-child" {
		t.Fatalf("producer snapshot order = %#v, want a-child then z-child", want)
	}

	if _, err := AssessBlocked(tmpDir, "blocked-evaluator", "note only", "orchestrator-1"); err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}
	afterNote := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	if !reflect.DeepEqual(afterNote.DependsOn, []string{"provider-plan"}) {
		t.Fatalf("note-only depends_on = %#v, want provider-plan", afterNote.DependsOn)
	}
	if afterNote.BlockedReason == nil || *afterNote.BlockedReason != oldReason ||
		!reflect.DeepEqual(afterNote.BlockedQuestions, []string{"existing question"}) ||
		!reflect.DeepEqual(afterNote.RepairRequest, testAssessBlockedRepairRequest()) {
		t.Fatalf("note-only assessment changed canonical blocker fields: %#v", afterNote)
	}
	noteEntry := afterNote.History[len(afterNote.History)-1]
	noteFingerprint, ok := IsAssessmentFingerprint(noteEntry.Extra[AssessmentFingerprintExtraKey])
	if !ok {
		t.Fatalf("note-only assessment lacks fingerprint: %#v", noteEntry.Extra)
	}

	if _, err := AssessBlockedWithOptions(
		tmpDir,
		"blocked-evaluator",
		"reconciled",
		"orchestrator-1",
		AssessBlockedOptions{Reason: "current reason", Questions: []string{"current question"}},
	); err != nil {
		t.Fatalf("AssessBlockedWithOptions() error: %v", err)
	}
	afterReconcile := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	if !reflect.DeepEqual(afterReconcile.DependsOn, []string{"provider-plan"}) {
		t.Fatalf("reconciliation depends_on = %#v, want provider-plan", afterReconcile.DependsOn)
	}
	if afterReconcile.BlockedReason == nil || *afterReconcile.BlockedReason != "current reason" ||
		!reflect.DeepEqual(afterReconcile.BlockedQuestions, []string{"current question"}) ||
		afterReconcile.RepairRequest != nil {
		t.Fatalf("reconciled canonical blocker fields = %#v", afterReconcile)
	}
	reconcileEntry := afterReconcile.History[len(afterReconcile.History)-1]
	reconcileFingerprint, ok := IsAssessmentFingerprint(reconcileEntry.Extra[AssessmentFingerprintExtraKey])
	if !ok {
		t.Fatalf("reconciliation lacks fingerprint: %#v", reconcileEntry.Extra)
	}
	if noteFingerprint == reconcileFingerprint {
		t.Fatal("changed disposition and canonical blocker retained the old fingerprint")
	}
}

func TestAssessBlocked_PrunesSupersededWakeSnapshots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.CreateSpecFile(t, tmpDir, "vision.md", "# Vision\n")
	baseTime := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	state := testhelpers.CreateValidState()
	blocked := testhelpers.BuildTaskByStatus("blocked-evaluator", models.TaskStatusBlocked, baseTime)
	blocked.AssignedTo = nil
	blocked.DependsOn = []string{"provider-plan"}
	provider := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, baseTime)
	child := testhelpers.BuildTaskByStatus("child", models.TaskStatusReady, baseTime)
	child.ParentTasks = []string{"provider-plan"}
	state.Tasks = []models.Task{blocked, provider, child}
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)

	for _, note := range []string{"first", "second", "third"} {
		if _, err := AssessBlocked(tmpDir, "blocked-evaluator", note, "orchestrator-1"); err != nil {
			t.Fatalf("AssessBlocked(%q) error: %v", note, err)
		}
	}

	task := readAssessBlockedTask(t, stateFile, "blocked-evaluator")
	var assessments []int
	for i := range task.History {
		if task.History[i].Event == models.TaskEventOrchestratorAssessment {
			assessments = append(assessments, i)
		}
	}
	if len(assessments) != 3 {
		t.Fatalf("assessment entries = %d, want 3", len(assessments))
	}
	for _, i := range assessments[:len(assessments)-1] {
		if _, ok := task.History[i].Extra[AssessmentFingerprintExtraKey]; ok {
			t.Errorf("superseded assessment at history[%d] still carries a fingerprint", i)
		}
		if task.History[i].Note == nil {
			t.Errorf("pruning dropped the note of superseded assessment at history[%d]", i)
		}
	}
	last := task.History[assessments[len(assessments)-1]]
	if _, ok := IsAssessmentFingerprint(last.Extra[AssessmentFingerprintExtraKey]); !ok {
		t.Fatalf("latest assessment lost its fingerprint: %#v", last.Extra)
	}

	// Wake suppression depends on the latest snapshot only; pruning the
	// earlier ones must not make an already-triaged task actionable again.
	currentState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if isTaskActionableSinceAssessment(currentState.FindTask("blocked-evaluator"), currentState) {
		t.Error("task became actionable again after superseded snapshots were pruned")
	}
}

func TestAssessBlocked_CandidateValidationRollback(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now)
	task.AssignedTo = nil
	task.RepairRequest = testAssessBlockedRepairRequest()
	state.Tasks = []models.Task{task}
	state.Goal.SpecRef = "specs/missing-goal-spec.md"
	setTaskSpecRefs(state)
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readAssessBlockedTask(t, stateFile, "task-1")

	_, err := AssessBlockedWithOptions(
		tmpDir,
		"task-1",
		"must roll back",
		"orchestrator-1",
		AssessBlockedOptions{
			Reason:    "new reason",
			Questions: []string{"new question"},
		},
	)
	if err == nil {
		t.Fatal("AssessBlockedWithOptions() error = nil, want full-state validation failure")
	}

	after := readAssessBlockedTask(t, stateFile, "task-1")
	assertAssessBlockedStateUnchanged(t, before, after)
}

func TestAssessBlockedWithOptions_FailuresPreserveState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		taskID      string
		agentID     string
		status      models.TaskStatus
		opts        AssessBlockedOptions
		errContains string
	}{
		{
			name:        "empty task ID",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "/task_id must not be empty",
		},
		{
			name:        "empty agent ID",
			taskID:      "task-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "agent ID is required",
		},
		{
			name:        "unauthorized role",
			taskID:      "task-1",
			agentID:     "coder-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "only orchestrator agents",
		},
		{
			name:        "reason without questions",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason"},
			errContains: "at least 1 question",
		},
		{
			name:        "questions without reason",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Questions: []string{"new question"}},
			errContains: "reason is required",
		},
		{
			name:        "too many questions",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusBlocked,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"one", "two", "three", "four"}},
			errContains: "/questions must contain at most 3 questions",
		},
		{
			name:    "invalid replacement repair request",
			taskID:  "task-1",
			agentID: "orchestrator-1",
			status:  models.TaskStatusBlocked,
			opts: AssessBlockedOptions{
				Reason:        "new reason",
				Questions:     []string{"new question"},
				RepairRequest: &models.RepairRequest{Operation: "retarget-dependency", Target: "task-1"},
			},
			errContains: "/repair_request/command exactly one of command or dependency_updates is required",
		},
		{
			name:        "wrong status",
			taskID:      "task-1",
			agentID:     "orchestrator-1",
			status:      models.TaskStatusReady,
			opts:        AssessBlockedOptions{Reason: "new reason", Questions: []string{"new question"}},
			errContains: "BLOCKED status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			task := testhelpers.BuildTaskByStatus("task-1", tt.status, time.Now().UTC())
			task.RepairRequest = testAssessBlockedRepairRequest()
			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)
			before := readAssessBlockedTask(t, stateFile, "task-1")

			_, err := AssessBlockedWithOptions(tmpDir, tt.taskID, "reassessment", tt.agentID, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.errContains) {
				t.Fatalf("AssessBlockedWithOptions() error = %v, want containing %q", err, tt.errContains)
			}

			after := readAssessBlockedTask(t, stateFile, "task-1")
			assertAssessBlockedStateUnchanged(t, before, after)
		})
	}
}

func TestAssessBlocked_NoteOnlyPreservesCanonicalMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, time.Now().UTC())
	task.RepairRequest = testAssessBlockedRepairRequest()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	testhelpers.WriteInitialState(t, stateFile, state)
	before := readAssessBlockedTask(t, stateFile, "task-1")

	if _, err := AssessBlocked(tmpDir, "task-1", "history only", "orchestrator-1"); err != nil {
		t.Fatalf("AssessBlocked() error: %v", err)
	}

	after := readAssessBlockedTask(t, stateFile, "task-1")
	if !reflect.DeepEqual(before.BlockedReason, after.BlockedReason) ||
		!reflect.DeepEqual(before.BlockedQuestions, after.BlockedQuestions) ||
		!reflect.DeepEqual(before.RepairRequest, after.RepairRequest) {
		t.Fatalf("note-only assessment changed canonical metadata: before=%#v after=%#v", before, after)
	}
	if len(after.History) != len(before.History)+1 {
		t.Fatalf("history length = %d, want %d", len(after.History), len(before.History)+1)
	}
}

func testAssessBlockedRepairRequest() *models.RepairRequest {
	return &models.RepairRequest{
		Operation:  "retarget-dependency",
		Target:     "task-1",
		Command:    "liza retarget-dependency task-1 old-dependency replacement-dependency",
		Evidence:   []string{"command=retarget exit_code=1 stderr=repair required"},
		Validation: []string{"liza get task-1 --json"},
	}
}

func readAssessBlockedTask(t *testing.T, stateFile, taskID string) *models.Task {
	t.Helper()
	state, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	task := state.FindTask(taskID)
	if task == nil {
		t.Fatalf("task %s not found", taskID)
	}
	return task
}

func assertAssessBlockedStateUnchanged(t *testing.T, before, after *models.Task) {
	t.Helper()
	if !reflect.DeepEqual(before.BlockedReason, after.BlockedReason) ||
		!reflect.DeepEqual(before.BlockedQuestions, after.BlockedQuestions) ||
		!reflect.DeepEqual(before.RepairRequest, after.RepairRequest) ||
		!reflect.DeepEqual(before.History, after.History) {
		t.Fatalf("blocked metadata/history changed after rejected assessment:\nbefore=%#v\nafter=%#v", before, after)
	}
}
