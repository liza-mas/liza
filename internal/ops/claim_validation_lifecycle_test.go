package ops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

func TestClaimValidationLifecycleRetryAndReplay(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusReady)
	setupMarker := filepath.Join(t.TempDir(), "setup")
	probeMarker := filepath.Join(t.TempDir(), "probe")
	if err := f.bb.Modify(func(state *models.State) error {
		setup := fmt.Sprintf("printf x >> %q", setupMarker)
		state.Config.PostWorktreeCmd = &setup
		state.FindTask("task-1").ValidationPrerequisites[0].Probes = [][]string{
			{"/bin/sh", "-c", `printf x >> "$1"`, "probe", probeMarker},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := f.state(t).FindTask("task-1")
	opts := LifecycleRequestOptions{RequestID: "missing-prerequisite", ExpectedTransition: models.TaskTransitionID(before)}
	_, err := ClaimTaskWithRequest(f.root, "task-1", f.authority.ID, &f.authority, opts, f.session(false))
	requireAssignmentPreflightError(t, err)
	failed := f.state(t).FindTask("task-1")
	if failed.Status != before.Status || failed.AssignedTo != nil || failed.Iteration != before.Iteration ||
		failed.Lifecycle == nil || failed.Lifecycle.Preparation != nil || len(failed.Lifecycle.Receipts) != 0 ||
		models.TaskTransitionID(failed) == opts.ExpectedTransition {
		t.Fatalf("failed validation published ownership or retained its ordinary-return reservation: %+v", failed)
	}
	_, err = ClaimTaskWithRequest(f.root, "task-1", f.authority.ID, &f.authority, opts, f.session(true))
	requireLifecycleError(t, err, models.LifecycleStateChanged, "requery", "none")

	opts = LifecycleRequestOptions{RequestID: "repaired-prerequisite", ExpectedTransition: models.TaskTransitionID(failed)}
	claimed, err := ClaimTaskWithRequest(f.root, "task-1", f.authority.ID, &f.authority, opts, f.session(true))
	if err != nil || claimed == nil || claimed.Outcome != models.LifecycleCompleted {
		t.Fatalf("repaired session did not complete the claim: %+v, %v", claimed, err)
	}
	statePath := paths.New(f.root).StatePath()
	beforeReplay := ownershipStateBytes(t, statePath)
	invalidSession := f.session(false)
	invalidSession.PreparationError = errors.New("replay must not prepare a new provider session")
	replayed, err := ClaimTaskWithRequest(f.root, "task-1", f.authority.ID, &f.authority, opts, invalidSession)
	if err != nil || replayed == nil || replayed.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("exact claim receipt did not replay: %+v, %v", replayed, err)
	}
	if !bytes.Equal(beforeReplay, ownershipStateBytes(t, statePath)) {
		t.Fatal("claim replay rewrote readiness, ownership, or receipts")
	}
	for marker, want := range map[string]string{setupMarker: "xx", probeMarker: "x"} {
		content, err := os.ReadFile(marker)
		if err != nil || string(content) != want {
			t.Fatalf("setup/probe repeated during stale request or replay: %s = %q, want %q; %v", marker, content, want, err)
		}
	}
}

func TestClaimValidationLifecycleGenerationChangesBeforeAssignment(t *testing.T) {
	f := newAssignmentPreflightFixture(t, models.TaskStatusReady)
	previous := testClaimTaskHooks
	t.Cleanup(func() { testClaimTaskHooks = previous })
	testClaimTaskHooks = &claimTaskTestHooks{beforePhase3Modify: func() {
		if err := f.bb.Modify(func(state *models.State) error {
			agent := state.Agents[f.authority.ID]
			agent.Generation = lifecycleGenerationB
			state.Agents[f.authority.ID] = agent
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}}
	opts := LifecycleRequestOptions{RequestID: "generation-drift", ExpectedTransition: models.TaskTransitionID(f.state(t).FindTask("task-1"))}
	_, err := ClaimTaskWithRequest(f.root, "task-1", f.authority.ID, &f.authority, opts, f.session(true))
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "unknown")
	state := f.state(t)
	task := state.FindTask("task-1")
	if task.Status != models.TaskStatusReady || task.AssignedTo != nil || task.Lifecycle == nil ||
		task.Lifecycle.Preparation == nil || len(task.Lifecycle.Receipts) != 0 || state.Agents[f.authority.ID].Status == models.AgentStatusWorking {
		t.Fatalf("stale preflight published a claim or retired another generation's fence: %+v", task)
	}
}
