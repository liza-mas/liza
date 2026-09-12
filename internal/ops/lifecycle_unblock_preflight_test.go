package ops

import (
	"bytes"
	"errors"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestLifecycleUnblockPreservesTargetSessionPreflight(t *testing.T) {
	root, statePath, bb, _ := setupOwnershipLifecycleClaim(t)
	testhelpers.SetupPipelineConfig(t, root)
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "unblock-current"}
	if err := bb.Modify(func(state *models.State) error {
		task := state.FindTask("task-1")
		*task = testhelpers.BuildTaskByStatus(task.ID, models.TaskStatusBlocked, task.Created)
		// This task was blocked before its first claim and has no preserved worktree.
		task.Worktree = nil
		task.Validation = []string{"check"}
		task.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "check", Env: []string{"REQUIRED_ENV"}}}
		agent := testhelpers.RegisteredTestAgent("orchestrator")
		agent.Generation = authority.Generation
		state.Agents[authority.ID] = agent
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := ownershipStateBytes(t, statePath)
	opts := UnblockTaskOptions{AssignTo: "coder-1", Request: ownershipRequestOptions(t, bb, "protected-unblock")}
	stale := authority
	stale.Generation = "unblock-old"
	_, err := UnblockTaskWithAuthority(root, "task-1", "repaired", stale, opts)
	if !IsAgentAuthorityError(err) {
		t.Fatalf("lost authority must win over target-session eligibility: %v", err)
	}
	requireLifecycleError(t, err, models.LifecycleStaleCaller, "stop", "none")
	_, err = UnblockTaskWithAuthority(root, "task-1", "repaired", authority, opts)
	var invalid *PreconditionError
	if !errors.As(err, &invalid) {
		t.Fatalf("direct assignment bypassed protected session preflight: %v", err)
	}
	requireLifecycleError(t, err, models.LifecycleInvalidInput, "correct_input", "none")
	if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
		t.Fatal("rejected unblock changed task, authority, receipt or readiness state")
	}
	opts.AssignTo = ""
	opts.Request.RequestID = "unassigned-unblock"
	result, err := UnblockTaskWithAuthority(root, "task-1", "repaired", authority, opts)
	if err != nil || result == nil || result.Outcome != models.LifecycleCompleted || result.ToStatus != models.TaskStatusReady || result.AssignedTo != "" {
		t.Fatalf("unblock without assignment should permit subsequent session claim: %+v, %v", result, err)
	}
	replayed, err := UnblockTaskWithAuthority(root, "task-1", "repaired", authority, opts)
	if err != nil || replayed == nil || replayed.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("protected unblock replay: %+v, %v", replayed, err)
	}
}
