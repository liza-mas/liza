package ops

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// --- Start ---

func TestStart_FromStopped(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Start(tmpDir, "resuming work", "human")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if result.Previous != models.SystemModeStopped {
		t.Errorf("Previous = %v, want STOPPED", result.Previous)
	}
	if result.New != models.SystemModeRunning {
		t.Errorf("New = %v, want RUNNING", result.New)
	}
	if result.ChangedBy != "human" {
		t.Errorf("ChangedBy = %q, want %q", result.ChangedBy, "human")
	}

	// Verify persisted state
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModeRunning {
		t.Errorf("Persisted mode = %v, want RUNNING", readState.Config.Mode)
	}
	if readState.Config.ModeChangedBy == nil || *readState.Config.ModeChangedBy != "human" {
		t.Error("ModeChangedBy not set")
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Start(tmpDir, "reason", "human")
	if err == nil {
		t.Fatal("Expected error when already RUNNING")
	}
	if !strings.Contains(err.Error(), "already RUNNING") {
		t.Errorf("Error = %q, want to contain 'already RUNNING'", err.Error())
	}
}

func TestStart_FromPaused(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Start(tmpDir, "reason", "human")
	if err == nil {
		t.Fatal("Expected error when PAUSED")
	}
	if !strings.Contains(err.Error(), "PAUSED") {
		t.Errorf("Error = %q, want to contain 'PAUSED'", err.Error())
	}
}

func TestCircuitBreakerHaltSurvivesStopStart(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Anomalies = []models.Anomaly{
		retryLoopAnomaly(now, "coder-1"),
		retryLoopAnomaly(now.Add(time.Minute), "coder-1"),
		retryLoopAnomaly(now.Add(2*time.Minute), "coder-1"),
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	analyzeResult, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if analyzeResult.Pattern != "retry_cluster" || analyzeResult.Response != models.CircuitBreakerResponseHalt || !analyzeResult.Triggered {
		t.Fatalf("Analyze() result = {pattern:%q response:%q triggered:%v}, want retry_cluster/HALT/true", analyzeResult.Pattern, analyzeResult.Response, analyzeResult.Triggered)
	}

	beforeStop, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read active HALT state: %v", err)
	}
	if beforeStop.CircuitBreaker.CurrentTrigger == nil || beforeStop.CircuitBreaker.CurrentResponse == nil {
		t.Fatalf("Analyze() did not persist trigger and response: %+v", beforeStop.CircuitBreaker)
	}
	trigger := *beforeStop.CircuitBreaker.CurrentTrigger
	response := *beforeStop.CircuitBreaker.CurrentResponse
	historyLen := len(beforeStop.CircuitBreaker.History)
	if historyLen == 0 {
		t.Fatal("Analyze() did not append a HALT history boundary")
	}
	boundaryIndex := historyLen - 1
	boundary := beforeStop.CircuitBreaker.History[boundaryIndex]
	if boundary.Resolution != nil || boundary.ResolvedAt != nil || !boundary.Timestamp.Equal(response.Timestamp) || boundary.Response != response.Response || boundary.Pattern == nil || *boundary.Pattern != response.Pattern {
		t.Fatalf("Analyze() history boundary does not match active response: response=%+v boundary=%+v", response, boundary)
	}

	if _, err := Stop(tmpDir, "maintenance", "human"); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	assertStoppedHaltPreserved := func(stage string) {
		t.Helper()
		current, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("%s: read state: %v", stage, err)
		}
		if current.Config.Mode != models.SystemModeStopped {
			t.Errorf("%s: mode = %s, want STOPPED", stage, current.Config.Mode)
		}
		if !reflect.DeepEqual(current.CircuitBreaker.CurrentTrigger, &trigger) {
			t.Errorf("%s: current trigger changed: got %+v, want %+v", stage, current.CircuitBreaker.CurrentTrigger, trigger)
		}
		if !reflect.DeepEqual(current.CircuitBreaker.CurrentResponse, &response) {
			t.Errorf("%s: current response changed: got %+v, want %+v", stage, current.CircuitBreaker.CurrentResponse, response)
		}
		if len(current.CircuitBreaker.History) != historyLen {
			t.Fatalf("%s: history length = %d, want %d", stage, len(current.CircuitBreaker.History), historyLen)
		}
		if !reflect.DeepEqual(current.CircuitBreaker.History[boundaryIndex], boundary) {
			t.Errorf("%s: unresolved history boundary changed: got %+v, want %+v", stage, current.CircuitBreaker.History[boundaryIndex], boundary)
		}
	}

	assertStoppedHaltPreserved("after Stop")

	if _, err := Start(tmpDir, "restart", "human"); err == nil {
		t.Fatal("Start() released an unresolved HALT")
	} else if !strings.Contains(err.Error(), "HALT") || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("Start() error = %q, want active HALT acknowledgement guidance", err)
	}
	assertStoppedHaltPreserved("after rejected Start")

	if _, err := AutoResume(tmpDir, "auto-resume"); err == nil {
		t.Fatal("AutoResume() acknowledged a STOPPED active HALT")
	} else if !strings.Contains(err.Error(), "STOPPED") {
		t.Fatalf("AutoResume() error = %q, want STOPPED precondition", err)
	}
	assertStoppedHaltPreserved("after rejected automatic resume")

	resumeResult, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if resumeResult.ResumedFrom != "active HALT response" {
		t.Errorf("Resume().ResumedFrom = %q, want active HALT response", resumeResult.ResumedFrom)
	}

	afterResume, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read acknowledged HALT state: %v", err)
	}
	if afterResume.Config.Mode != models.SystemModeStopped {
		t.Errorf("mode after Resume() = %s, want STOPPED", afterResume.Config.Mode)
	}
	if afterResume.CircuitBreaker.Status != "OK" || afterResume.CircuitBreaker.CurrentTrigger != nil || afterResume.CircuitBreaker.CurrentResponse != nil {
		t.Errorf("Resume() left active HALT state: %+v", afterResume.CircuitBreaker)
	}
	if len(afterResume.CircuitBreaker.History) != historyLen {
		t.Fatalf("history length after Resume() = %d, want %d", len(afterResume.CircuitBreaker.History), historyLen)
	}
	resolvedBoundary := afterResume.CircuitBreaker.History[boundaryIndex]
	if resolvedBoundary.Resolution == nil || *resolvedBoundary.Resolution != "resumed by human" || resolvedBoundary.ResolvedAt == nil {
		t.Fatalf("Resume() did not resolve the exact HALT boundary: %+v", resolvedBoundary)
	}
	resolvedBoundary.Resolution = nil
	resolvedBoundary.ResolvedAt = nil
	if !reflect.DeepEqual(resolvedBoundary, boundary) {
		t.Errorf("Resume() changed HALT boundary evidence: got %+v, want %+v", resolvedBoundary, boundary)
	}

	if _, err := Start(tmpDir, "restart after acknowledgement", "human"); err != nil {
		t.Fatalf("Start() after Resume() error: %v", err)
	}
	started, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read started state: %v", err)
	}
	if started.Config.Mode != models.SystemModeRunning {
		t.Errorf("mode after acknowledged Start() = %s, want RUNNING", started.Config.Mode)
	}
}

func TestLegacyCircuitBreakerHaltSurvivesStopStart(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	triggeredAt := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	pattern := "retry_cluster"
	severity := "HIGH"
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeCircuitBreakerTripped
	state.CircuitBreaker = models.CircuitBreaker{
		LastCheck: triggeredAt.Add(time.Minute),
		Status:    "TRIGGERED",
		CurrentTrigger: &models.CircuitBreakerTrigger{
			Timestamp:  triggeredAt,
			Pattern:    pattern,
			Severity:   severity,
			ReportFile: ".liza/circuit_breaker_report.md",
			Extra:      map[string]any{"legacy_trigger_field": "preserved"},
		},
		History: []models.CircuitBreakerHistory{
			{
				Timestamp: triggeredAt,
				Pattern:   &pattern,
				Severity:  &severity,
				Result:    "TRIGGERED",
				Extra:     map[string]any{"legacy_history_field": "preserved"},
			},
		},
		Extra: map[string]any{"legacy_breaker_field": "preserved"},
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	beforeStop, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read legacy active HALT state: %v", err)
	}
	if beforeStop.CircuitBreaker.CurrentResponse != nil {
		t.Fatalf("legacy fixture has typed current response: %+v", beforeStop.CircuitBreaker.CurrentResponse)
	}

	if _, err := Stop(tmpDir, "maintenance", "human"); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	stopped, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read stopped legacy HALT state: %v", err)
	}
	if stopped.Config.Mode != models.SystemModeStopped {
		t.Fatalf("mode after Stop() = %s, want STOPPED", stopped.Config.Mode)
	}
	if !reflect.DeepEqual(stopped.CircuitBreaker, beforeStop.CircuitBreaker) {
		t.Fatalf("Stop() changed legacy circuit-breaker state: got %+v, want %+v", stopped.CircuitBreaker, beforeStop.CircuitBreaker)
	}

	assertStoppedStateUnchanged := func(stage string) {
		t.Helper()
		current, err := db.New(stateFile).Read()
		if err != nil {
			t.Fatalf("%s: read state: %v", stage, err)
		}
		if !reflect.DeepEqual(current, stopped) {
			t.Errorf("%s mutated stopped legacy HALT state: got %+v, want %+v", stage, current, stopped)
		}
	}

	if _, err := Start(tmpDir, "restart", "human"); err == nil {
		t.Fatal("Start() released an unresolved legacy HALT")
	} else if !strings.Contains(err.Error(), "HALT") || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("Start() error = %q, want active HALT acknowledgement guidance", err)
	}
	assertStoppedStateUnchanged("rejected Start")

	if _, err := AutoResume(tmpDir, "auto-resume"); err == nil {
		t.Fatal("AutoResume() acknowledged a STOPPED legacy HALT")
	} else if !strings.Contains(err.Error(), "STOPPED") {
		t.Fatalf("AutoResume() error = %q, want STOPPED precondition", err)
	}
	assertStoppedStateUnchanged("rejected automatic resume")

	resumeResult, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if resumeResult.ResumedFrom != "active HALT response" || !resumeResult.SystemRemainsStopped {
		t.Errorf("Resume() result = %+v, want acknowledged HALT while stopped", resumeResult)
	}

	afterResume, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read acknowledged legacy HALT state: %v", err)
	}
	if !reflect.DeepEqual(afterResume.Config, stopped.Config) {
		t.Errorf("Resume() changed stopped mode config: got %+v, want %+v", afterResume.Config, stopped.Config)
	}
	expectedBreaker := stopped.CircuitBreaker
	expectedBreaker.Status = "OK"
	expectedBreaker.CurrentTrigger = nil
	if !reflect.DeepEqual(afterResume.CircuitBreaker, expectedBreaker) {
		t.Errorf("Resume() changed legacy data beyond trigger acknowledgement: got %+v, want %+v", afterResume.CircuitBreaker, expectedBreaker)
	}

	if _, err := Start(tmpDir, "restart after acknowledgement", "human"); err != nil {
		t.Fatalf("Start() after legacy HALT acknowledgement error: %v", err)
	}
	started, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read started state: %v", err)
	}
	if started.Config.Mode != models.SystemModeRunning {
		t.Errorf("mode after acknowledged Start() = %s, want RUNNING", started.Config.Mode)
	}
}

// --- Stop ---

func TestStop_FromRunning(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Stop(tmpDir, "end of day", "human")
	if err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if result.Previous != models.SystemModeRunning {
		t.Errorf("Previous = %v, want RUNNING", result.Previous)
	}
	if result.New != models.SystemModeStopped {
		t.Errorf("New = %v, want STOPPED", result.New)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModeStopped {
		t.Errorf("Persisted mode = %v, want STOPPED", readState.Config.Mode)
	}
}

func TestStop_AlreadyStopped(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Stop(tmpDir, "reason", "human")
	if err == nil {
		t.Fatal("Expected error when already STOPPED")
	}
	if !strings.Contains(err.Error(), "already STOPPED") {
		t.Errorf("Error = %q, want to contain 'already STOPPED'", err.Error())
	}
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Errorf("Expected PreconditionError, got %T", err)
	}
}

func TestLinearizableGoalCompleteStop(t *testing.T) {
	const (
		operationID1 = "AAAAAAAAAAAAAAAAAAAAAA"
		operationID2 = "AAAAAAAAAAAAAAAAAAAAAQ"
	)

	t.Run("current clean evidence writes a reserved source-bound token", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		installGoalCompleteStopTestSeams(t, time.Unix(1_700_000_000, 0).UTC(), operationID1)

		result, err := StopForGoalCompletion(fixture.projectRoot, "goal complete")
		if err != nil {
			t.Fatalf("StopForGoalCompletion() error = %v", err)
		}
		state := fixture.readState(t)
		if result.New != models.SystemModeStopped || state.Config.Mode != models.SystemModeStopped || state.Config.ModeChangedBy == nil {
			t.Fatalf("goal completion stop result=%#v mode=%s changed_by=%v", result, state.Config.Mode, state.Config.ModeChangedBy)
		}
		token, ok := decodeGoalCompleteStopToken(*state.Config.ModeChangedBy)
		if !ok || token.AnalysisKey != state.Goal.Integration.Closure.AnalysisKey ||
			token.Generation != state.Goal.Integration.Closure.Generation ||
			token.SourceCommit != state.Goal.Integration.Closure.SourceCommit || token.OperationID != operationID1 {
			t.Fatalf("automatic stop token = %#v valid=%v", token, ok)
		}
		snapshot, err := readEffectiveIntegrationCompletionSnapshot(fixture.projectRoot)
		if err != nil || !snapshot.decision.IntegrationComplete || snapshot.closure.SourceCommit != token.SourceCommit {
			t.Fatalf("post-stop completion snapshot=%#v error=%v", snapshot, err)
		}
	})

	t.Run("generic stop rejects the reserved namespace", func(t *testing.T) {
		validToken, err := encodeGoalCompleteStopToken(goalCompleteStopToken{
			AnalysisKey: "global:1", Generation: 1, SourceCommit: "source-a", OperationID: operationID1,
		})
		if err != nil {
			t.Fatalf("encodeGoalCompleteStopToken() error = %v", err)
		}
		for _, changedBy := range []string{goalCompleteStopReservedNamespace, validToken} {
			t.Run(changedBy, func(t *testing.T) {
				fixture := newEffectiveCompletionFixture(t, true)
				_, stopErr := Stop(fixture.projectRoot, "forged automatic stop", changedBy)
				var precondition *PreconditionError
				if !errors.As(stopErr, &precondition) {
					t.Fatalf("Stop() error = %T %v, want PreconditionError", stopErr, stopErr)
				}
				if state := fixture.readState(t); state.Config.Mode != models.SystemModeRunning {
					t.Fatalf("reserved generic stop changed mode to %s", state.Config.Mode)
				}
			})
		}
	})

	t.Run("public mutation after authorization wins before the mode write", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		installGoalCompleteStopTestSeams(t, time.Unix(1_700_000_001, 0).UTC(), operationID1)
		previousHook := afterGoalCompleteStopAuthorizationTestHook
		afterGoalCompleteStopAuthorizationTestHook = func() {
			taskID, agentID := fixture.installPublicIntegrationMutation(t)
			if _, err := MergeWorktree(fixture.projectRoot, taskID, agentID); err != nil {
				t.Fatalf("MergeWorktree() error = %v", err)
			}
		}
		t.Cleanup(func() { afterGoalCompleteStopAuthorizationTestHook = previousHook })

		_, err := StopForGoalCompletion(fixture.projectRoot, "goal complete")
		requireEffectiveCompletionPrecondition(t, err)
		if state := fixture.readState(t); state.Config.Mode != models.SystemModeRunning {
			t.Fatalf("mutation-before-write left mode %s", state.Config.Mode)
		}
	})

	t.Run("public mutation after the mode write invalidates the exact automatic stop", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		installGoalCompleteStopTestSeams(t, time.Unix(1_700_000_002, 0).UTC(), operationID1)
		writeStages := observeGoalCompleteStopWritesOutsideMutationLock(t, fixture.projectRoot)
		receiptWrite := observeMutationReceiptWriteOutsideMutationLock(t, fixture.projectRoot)
		receiptObserved := make(chan string, 1)
		mergeDone := make(chan error, 1)
		var taskID string
		previousHook := afterGoalCompleteStopModeWriteTestHook
		afterGoalCompleteStopModeWriteTestHook = func(string) {
			var agentID string
			taskID, agentID = fixture.installPublicIntegrationMutation(t)
			go func() {
				_, err := MergeWorktree(fixture.projectRoot, taskID, agentID)
				mergeDone <- err
			}()
			select {
			case observedTaskID := <-receiptWrite:
				receiptObserved <- observedTaskID
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for post-stop ref mutation")
			}
		}
		t.Cleanup(func() { afterGoalCompleteStopModeWriteTestHook = previousHook })

		_, err := StopForGoalCompletion(fixture.projectRoot, "goal complete")
		requireEffectiveCompletionPrecondition(t, err)
		select {
		case mergeErr := <-mergeDone:
			if mergeErr != nil {
				t.Fatalf("MergeWorktree() error = %v", mergeErr)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for post-stop public mutation")
		}
		if state := fixture.readState(t); state.Config.Mode != models.SystemModeRunning {
			t.Fatalf("mutation-after-write left mode %s", state.Config.Mode)
		}
		if got := <-writeStages; got != goalCompleteStopWriteStop {
			t.Fatalf("first state write stage = %q", got)
		}
		if got := <-receiptObserved; got != taskID {
			t.Fatalf("receipt write task = %q, want %q", got, taskID)
		}
	})

	t.Run("mutation after a completed automatic stop reactivates running", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		installGoalCompleteStopTestSeams(t, time.Unix(1_700_000_004, 0).UTC(), operationID1)
		if _, err := StopForGoalCompletion(fixture.projectRoot, "goal complete"); err != nil {
			t.Fatalf("StopForGoalCompletion() error = %v", err)
		}
		taskID, agentID := fixture.installPublicIntegrationMutation(t)
		if _, err := MergeWorktree(fixture.projectRoot, taskID, agentID); err != nil {
			t.Fatalf("MergeWorktree() error = %v", err)
		}
		if state := fixture.readState(t); state.Config.Mode != models.SystemModeRunning {
			t.Fatalf("post-completion mutation left mode %s", state.Config.Mode)
		}
	})

	t.Run("later mutation preserves an ordinary operator stop", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		taskID, agentID := fixture.installPublicIntegrationMutation(t)
		if _, err := Stop(fixture.projectRoot, "operator shutdown", "operator-1"); err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
		if _, err := MergeWorktree(fixture.projectRoot, taskID, agentID); err != nil {
			t.Fatalf("MergeWorktree() error = %v", err)
		}
		state := fixture.readState(t)
		if state.Config.Mode != models.SystemModeStopped || state.Config.ModeChangedBy == nil || *state.Config.ModeChangedBy != "operator-1" {
			t.Fatalf("mutation overwrote operator stop: mode=%s changed_by=%v", state.Config.Mode, state.Config.ModeChangedBy)
		}
	})

	t.Run("operation ID failure writes no stop", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		previousGenerator := generateGoalCompleteStopOperationID
		generateGoalCompleteStopOperationID = func() (string, error) { return "", errors.New("entropy unavailable") }
		t.Cleanup(func() { generateGoalCompleteStopOperationID = previousGenerator })

		_, err := StopForGoalCompletion(fixture.projectRoot, "goal complete")
		if err == nil || !strings.Contains(err.Error(), "entropy unavailable") {
			t.Fatalf("StopForGoalCompletion() error = %v, want entropy failure", err)
		}
		state := fixture.readState(t)
		if state.Config.Mode != models.SystemModeRunning || state.Config.ModeChangedBy != nil {
			t.Fatalf("entropy failure wrote mode=%s changed_by=%v", state.Config.Mode, state.Config.ModeChangedBy)
		}
	})

	t.Run("stale post-check cannot overwrite a newer repeated-timestamp automatic stop", func(t *testing.T) {
		fixture := newEffectiveCompletionFixture(t, true)
		fixedTime := time.Unix(1_700_000_003, 0).UTC()
		installGoalCompleteStopTestSeams(t, fixedTime, operationID1, operationID2)
		var taskID, agentID string
		firstWritten := make(chan string, 1)
		releaseFirst := make(chan struct{})
		previousHook := afterGoalCompleteStopModeWriteTestHook
		afterGoalCompleteStopModeWriteTestHook = func(rawToken string) {
			token, ok := decodeGoalCompleteStopToken(rawToken)
			if ok && token.OperationID == operationID1 {
				taskID, agentID = fixture.installPublicIntegrationMutation(t)
				firstWritten <- rawToken
				<-releaseFirst
			}
		}
		t.Cleanup(func() { afterGoalCompleteStopModeWriteTestHook = previousHook })

		firstDone := make(chan error, 1)
		go func() {
			_, err := StopForGoalCompletion(fixture.projectRoot, "first goal completion")
			firstDone <- err
		}()
		var firstToken string
		select {
		case firstToken = <-firstWritten:
		case err := <-firstDone:
			close(releaseFirst)
			t.Fatalf("first StopForGoalCompletion() returned before mode-write hook: %v", err)
		case <-time.After(2 * time.Second):
			close(releaseFirst)
			t.Fatal("timed out waiting for first automatic stop")
		}
		firstState := fixture.readState(t)
		if firstState.Config.ModeChangedAt == nil || !firstState.Config.ModeChangedAt.Equal(fixedTime) {
			close(releaseFirst)
			t.Fatalf("first stop timestamp = %v, want %v", firstState.Config.ModeChangedAt, fixedTime)
		}
		if _, err := MergeWorktree(fixture.projectRoot, taskID, agentID); err != nil {
			close(releaseFirst)
			t.Fatalf("MergeWorktree() error = %v", err)
		}
		newHead := testhelpers.MustGit(t, fixture.projectRoot, "rev-parse", "refs/heads/integration")
		installNextCleanGoalClosure(t, fixture, newHead)
		if _, err := StopForGoalCompletion(fixture.projectRoot, "second goal completion"); err != nil {
			close(releaseFirst)
			t.Fatalf("second StopForGoalCompletion() error = %v", err)
		}
		secondState := fixture.readState(t)
		if secondState.Config.ModeChangedBy == nil || *secondState.Config.ModeChangedBy == firstToken ||
			secondState.Config.ModeChangedAt == nil || !secondState.Config.ModeChangedAt.Equal(fixedTime) {
			close(releaseFirst)
			t.Fatalf("second stop token/timestamp = %v %v", secondState.Config.ModeChangedBy, secondState.Config.ModeChangedAt)
		}
		secondToken := *secondState.Config.ModeChangedBy

		close(releaseFirst)
		select {
		case err := <-firstDone:
			requireEffectiveCompletionPrecondition(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for stale first post-check")
		}
		finalState := fixture.readState(t)
		if finalState.Config.Mode != models.SystemModeStopped || finalState.Config.ModeChangedBy == nil || *finalState.Config.ModeChangedBy != secondToken {
			t.Fatalf("stale post-check overwrote newer stop: mode=%s changed_by=%v want=%q", finalState.Config.Mode, finalState.Config.ModeChangedBy, secondToken)
		}
	})
}

func installGoalCompleteStopTestSeams(t *testing.T, timestamp time.Time, operationIDs ...string) {
	t.Helper()
	previousNow := goalCompleteStopNow
	previousGenerator := generateGoalCompleteStopOperationID
	goalCompleteStopNow = func() time.Time { return timestamp }
	ids := make(chan string, len(operationIDs))
	for _, operationID := range operationIDs {
		ids <- operationID
	}
	generateGoalCompleteStopOperationID = func() (string, error) {
		select {
		case operationID := <-ids:
			return operationID, nil
		default:
			return "", errors.New("no deterministic operation ID available")
		}
	}
	t.Cleanup(func() {
		goalCompleteStopNow = previousNow
		generateGoalCompleteStopOperationID = previousGenerator
	})
}

func observeGoalCompleteStopWritesOutsideMutationLock(t *testing.T, projectRoot string) <-chan string {
	t.Helper()
	stages := make(chan string, 4)
	previousHook := beforeGoalCompleteStopStateWriteTestHook
	beforeGoalCompleteStopStateWriteTestHook = func(stage string) {
		if err := withIntegrationMutationLockTimeout(projectRoot, "goal-stop-write-test", 250*time.Millisecond, func() error { return nil }); err != nil {
			t.Errorf("goal-complete state write %q held integration mutation lock: %v", stage, err)
		}
		stages <- stage
	}
	t.Cleanup(func() { beforeGoalCompleteStopStateWriteTestHook = previousHook })
	return stages
}

func observeMutationReceiptWriteOutsideMutationLock(t *testing.T, projectRoot string) <-chan string {
	t.Helper()
	taskIDs := make(chan string, 2)
	previousHook := integrationMutationReceiptPersistTestHook
	integrationMutationReceiptPersistTestHook = func(receipt models.IntegrationMutationReceipt) {
		if err := withIntegrationMutationLockTimeout(projectRoot, "goal-stop-receipt-test", 250*time.Millisecond, func() error { return nil }); err != nil {
			t.Errorf("mutation receipt state write held integration mutation lock: %v", err)
		}
		taskIDs <- receipt.TaskID
	}
	t.Cleanup(func() { integrationMutationReceiptPersistTestHook = previousHook })
	return taskIDs
}

func installNextCleanGoalClosure(t *testing.T, fixture *effectiveCompletionFixture, sourceCommit string) {
	t.Helper()
	fixture.mutateState(t, func(state *models.State) {
		previous := state.FindTask("integration-global-1")
		if previous == nil {
			t.Fatal("global generation 1 task missing")
		}
		analysis := *previous
		analysis.ID = "integration-global-2"
		analysis.IntegrationAnalysis = &models.IntegrationAnalysisMetadata{
			Key: "global:2", Phase: models.IntegrationAnalysisPhaseGlobal, Generation: 2, SourceCommit: sourceCommit,
		}
		analysis.ReviewCommit = progressString("global-report-2")
		state.Tasks = append(state.Tasks, analysis)
		state.Goal.Integration.GlobalGenerations = append(state.Goal.Integration.GlobalGenerations, models.IntegrationGlobalGeneration{
			Generation: 2, AnalysisTaskID: analysis.ID, AnalysisKey: "global:2",
			Verdict: models.IntegrationAnalysisVerdictClean, SourceCommit: sourceCommit, ReportCommit: "global-report-2",
		})
		state.Goal.Integration.Closure = &models.IntegrationClosure{
			Status: models.IntegrationClosureStatusClean, Generation: 2, AnalysisKey: "global:2", SourceCommit: sourceCommit,
		}
	})
}

// --- Pause ---

func TestPause_FromRunning(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Pause(tmpDir, "lunch break", "human")
	if err != nil {
		t.Fatalf("Pause() error: %v", err)
	}

	if result.Previous != models.SystemModeRunning {
		t.Errorf("Previous = %v, want RUNNING", result.Previous)
	}
	if result.New != models.SystemModePaused {
		t.Errorf("New = %v, want PAUSED", result.New)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModePaused {
		t.Errorf("Persisted mode = %v, want PAUSED", readState.Config.Mode)
	}
}

func TestPause_AlreadyPaused(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Pause(tmpDir, "reason", "human")
	if err == nil {
		t.Fatal("Expected error when already PAUSED")
	}
	if !strings.Contains(err.Error(), "already PAUSED") {
		t.Errorf("Error = %q, want to contain 'already PAUSED'", err.Error())
	}
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Errorf("Expected PreconditionError, got %T", err)
	}
}

func TestPause_FromStopped(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Pause(tmpDir, "reason", "human")
	if err == nil {
		t.Fatal("Expected error when STOPPED")
	}
	if !strings.Contains(err.Error(), "STOPPED") {
		t.Errorf("Error = %q, want to contain 'STOPPED'", err.Error())
	}
}

// --- Resume ---

func TestResumeAcknowledgesCircuitBreakerResponse(t *testing.T) {
	observedAt := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	registeredAt := observedAt.Add(-time.Hour)

	for _, response := range []models.CircuitBreakerResponseType{
		models.CircuitBreakerResponseCheckpoint,
		models.CircuitBreakerResponseHalt,
	} {
		t.Run(string(response), func(t *testing.T) {
			tmpDir := t.TempDir()
			stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
			state := testhelpers.CreateValidState()
			state.Anomalies = []models.Anomaly{
				providerAuditAnomaly(observedAt, "coder-1"),
				providerAuditAnomaly(observedAt.Add(time.Minute), "coder-2"),
			}
			if response == models.CircuitBreakerResponseHalt {
				setAnalyzeProviderAuditEpochs(state, "codex", models.AgentHealthDegraded, registeredAt)
			}
			testhelpers.WriteInitialState(t, stateFile, state)

			analyzeResult, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("Analyze() error: %v", err)
			}
			if analyzeResult.Response != response {
				t.Fatalf("Analyze() response = %q, want %q", analyzeResult.Response, response)
			}

			beforeResume, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("read active response: %v", err)
			}
			if beforeResume.CircuitBreaker.CurrentResponse == nil {
				t.Fatal("Analyze() did not persist an active response")
			}
			boundary := beforeResume.CircuitBreaker.CurrentResponse.Timestamp

			if _, err := Resume(tmpDir, "human"); err != nil {
				t.Fatalf("Resume() error: %v", err)
			}

			afterResume, err := db.New(stateFile).Read()
			if err != nil {
				t.Fatalf("read resumed state: %v", err)
			}
			if afterResume.CircuitBreaker.CurrentResponse != nil || afterResume.CircuitBreaker.CurrentTrigger != nil {
				t.Errorf("resume left active circuit-breaker state: %+v", afterResume.CircuitBreaker)
			}
			if afterResume.CircuitBreaker.Status != "OK" || afterResume.Config.Mode != models.SystemModeRunning {
				t.Errorf("resume status/mode = %s/%s, want OK/RUNNING", afterResume.CircuitBreaker.Status, afterResume.Config.Mode)
			}

			var resolved *models.CircuitBreakerHistory
			for i := range afterResume.CircuitBreaker.History {
				entry := &afterResume.CircuitBreaker.History[i]
				if entry.Timestamp.Equal(boundary) && entry.Response == response {
					resolved = entry
					break
				}
			}
			if resolved == nil || resolved.ResolvedAt == nil || resolved.Resolution == nil {
				t.Fatalf("resume did not resolve response at committed boundary %s: %+v", boundary, afterResume.CircuitBreaker.History)
			}

			reanalyzed, err := Analyze(tmpDir)
			if err != nil {
				t.Fatalf("Analyze() after resume error: %v", err)
			}
			if reanalyzed.Triggered || reanalyzed.Response != models.CircuitBreakerResponseWarning || reanalyzed.Classification != models.CircuitBreakerEvidenceAcknowledgedHistorical {
				t.Errorf("unchanged re-analysis = {triggered:%v response:%q classification:%q}, want historical WARNING", reanalyzed.Triggered, reanalyzed.Response, reanalyzed.Classification)
			}
		})
	}
}

func TestResume_FromPaused(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.ResumedFrom != "PAUSED mode" {
		t.Errorf("ResumedFrom = %q, want %q", result.ResumedFrom, "PAUSED mode")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModeRunning {
		t.Errorf("Persisted mode = %v, want RUNNING", readState.Config.Mode)
	}
}

func TestResume_FromCircuitBreaker(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeCircuitBreakerTripped
	state.CircuitBreaker.Status = "TRIGGERED"
	trigger := &models.CircuitBreakerTrigger{Pattern: "retry_cluster", Severity: "HIGH"}
	state.CircuitBreaker.CurrentTrigger = trigger
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.ResumedFrom != "CIRCUIT_BREAKER_TRIPPED mode" {
		t.Errorf("ResumedFrom = %q, want %q", result.ResumedFrom, "CIRCUIT_BREAKER_TRIPPED mode")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModeRunning {
		t.Errorf("Persisted mode = %v, want RUNNING", readState.Config.Mode)
	}
	if readState.CircuitBreaker.Status != "OK" {
		t.Errorf("CircuitBreaker status = %q, want %q", readState.CircuitBreaker.Status, "OK")
	}
	if readState.CircuitBreaker.CurrentTrigger != nil {
		t.Error("CircuitBreaker trigger should be cleared")
	}
}

func TestResume_FromCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCheckpoint
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.ResumedFrom != "CHECKPOINT" {
		t.Errorf("ResumedFrom = %q, want %q", result.ResumedFrom, "CHECKPOINT")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
}

func TestResume_PlanningCheckpointExecutesTransitionsMidSprint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerPlanningComplete

	now := time.Now().UTC()
	readyPlan := models.Task{
		ID:          "plan-ready",
		Type:        models.TaskTypePlanning,
		RolePair:    "code-planning-pair",
		Description: "Ready coding plan",
		Status:      models.TaskStatusMerged,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Plan approved",
		Scope:       "pkg/x",
		Output: []models.OutputEntry{{
			Desc:     "Implement X",
			DoneWhen: "tests pass",
			Scope:    "pkg/x",
			SpecRef:  "specs/x.md",
		}},
		History: []models.TaskHistoryEntry{},
	}
	activePlan := models.Task{
		ID:          "plan-active",
		Type:        models.TaskTypePlanning,
		RolePair:    "code-planning-pair",
		Description: "Still planning",
		Status:      models.TaskStatusCodePlanning,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Plan approved",
		Scope:       "pkg/y",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, readyPlan, activePlan)
	state.Sprint.Scope.Planned = []string{"plan-ready", "plan-active"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.TransitionsExecuted != 1 {
		t.Fatalf("TransitionsExecuted = %d, want 1", result.TransitionsExecuted)
	}
	if result.TransitionError != "" {
		t.Fatalf("TransitionError = %q, want empty", result.TransitionError)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q, want cleared", readState.Sprint.CheckpointTrigger)
	}

	source := readState.FindTask("plan-ready")
	if source == nil || !source.TransitionsExecuted["code-plan-to-coding"] {
		t.Fatalf("source transition not recorded: %+v", source)
	}

	childID := "plan-ready-code-0"
	child := readState.FindTask(childID)
	if child == nil {
		t.Fatalf("child task %q not found", childID)
	}
	if child.RolePair != "coding-pair" {
		t.Errorf("child role_pair = %q, want coding-pair", child.RolePair)
	}
	if child.Status != models.TaskStatusReady {
		t.Errorf("child status = %s, want %s", child.Status, models.TaskStatusReady)
	}

	foundChildInScope := false
	for _, id := range readState.Sprint.Scope.Planned {
		if id == childID {
			foundChildInScope = true
			break
		}
	}
	if !foundChildInScope {
		t.Errorf("child %q not in sprint scope: %v", childID, readState.Sprint.Scope.Planned)
	}
}

func TestResume_ManyToOneCheckpointExecutesTransitionsMidSprint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerManyToOneReady

	now := time.Now().UTC()
	cohort := makeManyToOneCohort("epic-plan-1", "us-writing-pair", models.TaskStatusMerged, "README.md", 2)
	activePlan := models.Task{
		ID:          "plan-active",
		Type:        models.TaskTypePlanning,
		RolePair:    "code-planning-pair",
		Description: "Still planning",
		Status:      models.TaskStatusCodePlanning,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Plan approved",
		Scope:       "pkg/y",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, cohort[0], cohort[1], activePlan)
	state.Sprint.Scope.Planned = []string{cohort[0].ID, cohort[1].ID, activePlan.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.TransitionsExecuted != 1 {
		t.Fatalf("TransitionsExecuted = %d, want 1", result.TransitionsExecuted)
	}
	if result.TransitionError != "" {
		t.Fatalf("TransitionError = %q, want empty", result.TransitionError)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q, want cleared", readState.Sprint.CheckpointTrigger)
	}

	childID := "epic-plan-1-arm"
	child := readState.FindTask(childID)
	if child == nil {
		t.Fatalf("child task %q not found", childID)
	}
	if child.RolePair != "architecture-main-pair" {
		t.Errorf("child role_pair = %q, want architecture-main-pair", child.RolePair)
	}
	if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE_MAIN") {
		t.Errorf("child status = %s, want DRAFT_ARCHITECTURE_MAIN", child.Status)
	}
	for _, member := range cohort {
		source := readState.FindTask(member.ID)
		if source == nil || !source.TransitionsExecuted["us-to-coding"] {
			t.Fatalf("source transition not recorded for %s: %+v", member.ID, source)
		}
	}
}

func TestResume_ManyToOneCheckpointExecutesTransitionsWhenAllPlannedTerminal(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCheckpoint
	state.Sprint.CheckpointTrigger = models.CheckpointTriggerManyToOneReady

	cohort := makeManyToOneCohort("epic-plan-1", "us-writing-pair", models.TaskStatusMerged, "README.md", 2)
	state.Tasks = append(state.Tasks, cohort[0], cohort[1])
	state.Sprint.Scope.Planned = []string{cohort[0].ID, cohort[1].ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.SprintAdvanced != nil {
		t.Fatalf("SprintAdvanced = %+v, want nil", result.SprintAdvanced)
	}
	if result.TransitionsExecuted != 1 {
		t.Fatalf("TransitionsExecuted = %d, want 1", result.TransitionsExecuted)
	}
	if result.TransitionError != "" {
		t.Fatalf("TransitionError = %q, want empty", result.TransitionError)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if readState.Sprint.CheckpointTrigger != "" {
		t.Errorf("CheckpointTrigger = %q, want cleared", readState.Sprint.CheckpointTrigger)
	}

	childID := "epic-plan-1-arm"
	child := readState.FindTask(childID)
	if child == nil {
		t.Fatalf("child task %q not found", childID)
	}
	if child.RolePair != "architecture-main-pair" {
		t.Errorf("child role_pair = %q, want architecture-main-pair", child.RolePair)
	}
	if child.Status != models.TaskStatus("DRAFT_ARCHITECTURE_MAIN") {
		t.Errorf("child status = %s, want DRAFT_ARCHITECTURE_MAIN", child.Status)
	}
	if !slices.Contains(readState.Sprint.Scope.Planned, childID) {
		t.Errorf("child %q not in sprint scope: %v", childID, readState.Sprint.Scope.Planned)
	}
	for _, member := range cohort {
		source := readState.FindTask(member.ID)
		if source == nil || !source.TransitionsExecuted["us-to-coding"] {
			t.Fatalf("source transition not recorded for %s: %+v", member.ID, source)
		}
	}
}

func TestResume_PausedAndCheckpoint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModePaused
	state.Sprint.Status = models.SprintStatusCheckpoint
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if !strings.Contains(result.ResumedFrom, "PAUSED") || !strings.Contains(result.ResumedFrom, "CHECKPOINT") {
		t.Errorf("ResumedFrom = %q, want to contain both PAUSED and CHECKPOINT", result.ResumedFrom)
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Config.Mode != models.SystemModeRunning {
		t.Errorf("Persisted mode = %v, want RUNNING", readState.Config.Mode)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
}

func TestResume_CheckpointAllTerminal_MarksCompleted(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCheckpoint

	// Add a sprint-terminal task (MERGED is the universal sprint-terminal)
	now := time.Now().UTC()
	mergeCommit := "abc123"
	task := models.Task{
		ID:          "plan-1",
		Type:        models.TaskTypeCoding,
		Description: "Plan task",
		Status:      models.TaskStatusMerged,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Approved",
		Scope:       "scope",
		MergeCommit: &mergeCommit,
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.ResumedFrom != "CHECKPOINT" {
		t.Errorf("ResumedFrom = %q, want %q", result.ResumedFrom, "CHECKPOINT")
	}
	// Should NOT advance (no SprintAdvanced)
	if result.SprintAdvanced != nil {
		t.Error("Expected no sprint advance when transitioning to COMPLETED")
	}

	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusCompleted {
		t.Errorf("Sprint status = %v, want COMPLETED", readState.Sprint.Status)
	}
}

func TestResume_FromCompleted_AdvancesSprint(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCompleted

	// Parent task at sprint-terminal, plus child at DRAFT
	now := time.Now().UTC()
	parentID := "plan-1"
	reviewCommit := "abc123"
	parentTask := models.Task{
		ID:                  parentID,
		Type:                models.TaskTypeCoding,
		Description:         "Plan task",
		Status:              models.TaskStatusCodingPlanApproved,
		Priority:            1,
		Created:             now,
		SpecRef:             "README.md",
		DoneWhen:            "Approved",
		Scope:               "scope",
		ReviewCommit:        &reviewCommit,
		TransitionsExecuted: map[string]bool{"code-plan-to-coding": true},
		History:             []models.TaskHistoryEntry{},
	}
	childTask := models.Task{
		ID:          "plan-1-code-plan-to-coding-0",
		Type:        models.TaskTypeCoding,
		Description: "Child task",
		Status:      models.TaskStatusDraft,
		Priority:    1,
		Created:     now,
		ParentTasks: []string{parentID},
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, parentTask, childTask)
	state.Sprint.Scope.Planned = []string{parentID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.ResumedFrom != "COMPLETED sprint" {
		t.Errorf("ResumedFrom = %q, want %q", result.ResumedFrom, "COMPLETED sprint")
	}
	if result.SprintAdvanced == nil {
		t.Fatal("Expected sprint advance")
	}
	if result.SprintAdvanced.NewSprintNumber != 2 {
		t.Errorf("NewSprintNumber = %d, want 2", result.SprintAdvanced.NewSprintNumber)
	}

	// Child task should be in new sprint's planned scope
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusInProgress {
		t.Errorf("Sprint status = %v, want IN_PROGRESS", readState.Sprint.Status)
	}
	if readState.Sprint.Number != 2 {
		t.Errorf("Sprint number = %d, want 2", readState.Sprint.Number)
	}
	// The child task (DRAFT, non-terminal) should be carried forward
	found := false
	for _, id := range readState.Sprint.Scope.Planned {
		if id == "plan-1-code-plan-to-coding-0" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Child task not in new sprint planned scope: %v", readState.Sprint.Scope.Planned)
	}
}

func TestResume_FromCompleted_AllTerminal_EmptyCarriedTasks(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusCompleted

	// All tasks terminal (MERGED) — nothing to carry forward
	now := time.Now().UTC()
	task := models.Task{
		ID:          "task-1",
		Type:        models.TaskTypeCoding,
		Description: "Done task",
		Status:      models.TaskStatusMerged,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, task)
	state.Sprint.Scope.Planned = []string{"task-1"}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "auto-resume")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.SprintAdvanced == nil {
		t.Fatal("Expected sprint advance")
	}
	if len(result.SprintAdvanced.CarriedTasks) != 0 {
		t.Errorf("CarriedTasks = %v, want empty", result.SprintAdvanced.CarriedTasks)
	}
	if result.TransitionsExecuted != 0 {
		t.Errorf("TransitionsExecuted = %d, want 0", result.TransitionsExecuted)
	}
}

func TestResume_NoFollowUpSkipsPipelineTransitionsAfterSprintAdvance(t *testing.T) {
	tmpDir, stateFile := setupPhase2PipelineProceedTest(t)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Config.NoFollowUp = true
	state.Sprint.Status = models.SprintStatusCompleted

	now := time.Now().UTC()
	pipelineTask := models.Task{
		ID:          "us-task-1",
		Type:        models.TaskTypePlanning,
		RolePair:    "us-writing-pair",
		Description: "Done task",
		Status:      models.TaskStatusMerged,
		Priority:    1,
		Created:     now,
		ParentTasks: []string{"epic-plan-1"},
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "scope",
		History:     []models.TaskHistoryEntry{},
	}
	subpipelineTask := models.Task{
		ID:          "plan-task-1",
		Type:        models.TaskTypePlanning,
		RolePair:    "code-planning-pair",
		Description: "Done plan task",
		Status:      models.TaskStatusMerged,
		Priority:    1,
		Created:     now,
		SpecRef:     "README.md",
		DoneWhen:    "Done",
		Scope:       "scope",
		Output: []models.OutputEntry{
			{Desc: "Implement login", DoneWhen: "Login works", Scope: "auth", SpecRef: "specs/auth.md#login"},
		},
		History: []models.TaskHistoryEntry{},
	}
	state.Tasks = append(state.Tasks, pipelineTask, subpipelineTask)
	state.Sprint.Scope.Planned = []string{pipelineTask.ID, subpipelineTask.ID}
	testhelpers.WriteInitialState(t, stateFile, state)

	result, err := Resume(tmpDir, "auto-resume")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	if result.SprintAdvanced == nil {
		t.Fatal("Expected sprint advance")
	}
	if result.TransitionsExecuted != 1 {
		t.Errorf("TransitionsExecuted = %d, want 1", result.TransitionsExecuted)
	}

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if child := readState.FindTask("epic-plan-1-us-to-coding"); child != nil {
		t.Fatalf("pipeline-transition child should not be created: %+v", child)
	}
	if child := readState.FindTask("plan-task-1-code-plan-to-coding-0"); child == nil {
		t.Fatal("subpipeline transition child should be created")
	}
}

func TestResume_FromStopped(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Resume(tmpDir, "human")
	if err == nil {
		t.Fatal("Expected error when STOPPED")
	}
	if !strings.Contains(err.Error(), "STOPPED") {
		t.Errorf("Error = %q, want to contain 'STOPPED'", err.Error())
	}
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Errorf("Expected PreconditionError, got %T", err)
	}
}

func TestResume_StoppedWithCheckpoint_RejectsBeforeSprintMutation(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	state.Sprint.Status = models.SprintStatusCheckpoint
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Resume(tmpDir, "human")
	if err == nil {
		t.Fatal("Expected error when STOPPED+CHECKPOINT")
	}
	if !strings.Contains(err.Error(), "STOPPED") {
		t.Errorf("Error = %q, want to contain 'STOPPED'", err.Error())
	}

	// Verify sprint status was NOT mutated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusCheckpoint {
		t.Errorf("Sprint status mutated to %v, want CHECKPOINT (unchanged)", readState.Sprint.Status)
	}
	if readState.Config.Mode != models.SystemModeStopped {
		t.Errorf("Mode mutated to %v, want STOPPED (unchanged)", readState.Config.Mode)
	}
}

func TestResume_StoppedWithCompleted_RejectsBeforeSprintMutation(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeStopped
	state.Sprint.Status = models.SprintStatusCompleted
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Resume(tmpDir, "human")
	if err == nil {
		t.Fatal("Expected error when STOPPED+COMPLETED")
	}
	if !strings.Contains(err.Error(), "STOPPED") {
		t.Errorf("Error = %q, want to contain 'STOPPED'", err.Error())
	}

	// Verify sprint status was NOT mutated
	bb := db.New(stateFile)
	readState, err := bb.Read()
	if err != nil {
		t.Fatalf("Failed to read state: %v", err)
	}
	if readState.Sprint.Status != models.SprintStatusCompleted {
		t.Errorf("Sprint status mutated to %v, want COMPLETED (unchanged)", readState.Sprint.Status)
	}
	if readState.Config.Mode != models.SystemModeStopped {
		t.Errorf("Mode mutated to %v, want STOPPED (unchanged)", readState.Config.Mode)
	}
}

func TestResume_NothingToResume(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.Sprint.Status = models.SprintStatusInProgress
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := Resume(tmpDir, "human")
	if err == nil {
		t.Fatal("Expected error when nothing to resume")
	}
	var pe *PreconditionError
	if !errors.As(err, &pe) {
		t.Errorf("Expected PreconditionError, got %T", err)
	}
}

func TestResume_CompletedSprintReportsStuckFanIn(t *testing.T) {
	// GIVEN a completed sprint whose replaced story lost its cohort lineage
	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	state := testhelpers.CreateValidState()
	state.Config.Mode = models.SystemModeRunning
	state.PipelineVersion = 2
	state.Sprint.Status = models.SprintStatusCompleted
	for _, task := range supersededCohort("epic-plan-1", false) {
		task.SpecRef = "README.md"
		state.Tasks = append(state.Tasks, task)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, task.ID)
	}
	testhelpers.WriteInitialState(t, stateFile, state)

	// WHEN the human resumes
	result, err := Resume(tmpDir, "human")
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}

	// THEN the sprint still advances, but the stuck fan-in is reported, not silent
	if result.SprintAdvanced == nil || result.TransitionsExecuted != 0 {
		t.Fatalf("SprintAdvanced = %+v, TransitionsExecuted = %d; want an advance with no transitions", result.SprintAdvanced, result.TransitionsExecuted)
	}
	for _, want := range []string{
		`task epic-plan-1-us-1 transition us-to-coding: many-to-one cohort detection failed: many-to-one cohort member "epic-plan-1-us-0" was superseded by "epic-plan-1-us-0-fix", which does not carry cohort lineage`,
		`task epic-plan-1-us-0-fix transition us-to-coding: many-to-one cohort detection failed: trigger task "epic-plan-1-us-0-fix" has no parent_task`,
	} {
		if !strings.Contains(result.TransitionError, want) {
			t.Fatalf("TransitionError = %q, want %q", result.TransitionError, want)
		}
	}
}
