package ops

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAdjacentLifecycleExactRequestReplay(t *testing.T) {
	for _, operation := range []string{"cancel-task", "supersede-task", "unblock-task", "handoff", "set-task-output", "recover-agent"} {
		t.Run(operation, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			stateFile, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)
			state := testhelpers.CreateValidState()
			status := models.TaskStatusBlocked
			if operation == "handoff" || operation == "set-task-output" {
				status = models.TaskStatusImplementing
			}
			if operation == "recover-agent" {
				status = models.TaskStatusReviewing
			}
			task := testhelpers.BuildTaskByStatus("task-1", status, time.Now().UTC())
			if operation == "supersede-task" {
				task.RolePair = ""
			}
			if operation == "unblock-task" {
				task.RolePair = "code-planning-pair"
				task.Worktree = nil
				task.BaseCommit = nil
			}
			if operation == "recover-agent" {
				task.ReviewingBy = strPtr("code-reviewer-1")
				state.Agents["code-reviewer-1"] = models.Agent{Role: "code-reviewer", Status: models.AgentStatusReviewing, CurrentTask: &task.ID}
			}
			state.Tasks = []models.Task{task}
			testhelpers.WriteInitialState(t, stateFile, state)
			// Derive the token from serialized state, as a command caller does.
			stored, err := db.For(stateFile).Read()
			if err != nil {
				t.Fatal(err)
			}
			opts := LifecycleRequestOptions{RequestID: "adjacent-replay", ExpectedTransition: models.TaskTransitionID(stored.FindTask(task.ID))}
			invoke := func(reason string) (models.LifecycleOutcome, error) {
				switch operation {
				case "cancel-task":
					r, err := CancelTaskWithOptions(root, task.ID, reason, "orchestrator-1", opts)
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				case "supersede-task":
					r, err := SupersedeTaskWithOptions(root, task.ID, []string{"replacement"}, reason, "orchestrator-1", SupersedeTaskOptions{Request: opts})
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				case "unblock-task":
					r, err := UnblockTaskWithOptions(root, task.ID, reason, "orchestrator-1", UnblockTaskOptions{Request: opts})
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				case "handoff":
					r, err := Handoff(&HandoffInput{ProjectRoot: root, TaskID: task.ID, AgentID: "coder-1", Summary: reason, NextAction: "continue from checkpoint", Request: opts})
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				case "set-task-output":
					r, err := SetTaskOutputWithOptions(root, &SetTaskOutputInput{TaskID: task.ID, AgentID: "coder-1", Request: opts, Output: []models.OutputEntry{{Desc: reason, DoneWhen: "tests pass", Scope: "component", SpecRef: "specs/component.md"}}})
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				default:
					r, err := RecoverAgentWithOptions(root, "code-reviewer-1", false, reason, opts)
					if r != nil {
						return r.LifecycleOutcome, err
					}
					return models.LifecycleOutcome{}, err
				}
			}
			first, err := invoke("validated change")
			if err != nil {
				t.Fatal(err)
			}
			if first.Outcome != models.LifecycleCompleted || first.TransitionID == opts.ExpectedTransition {
				t.Fatalf("first outcome = %+v", first)
			}
			before, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := invoke("validated change")
			if err != nil {
				t.Fatal(err)
			}
			if replay.Outcome != models.LifecycleAlreadyCompleted || replay.CompletedTransitionID != first.TransitionID {
				t.Fatalf("replay = %+v, first = %+v", replay, first)
			}
			after, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("exact retry changed persisted state")
			}
			_, err = invoke("different intent")
			var conflict *LifecycleError
			if !errors.As(err, &conflict) || conflict.Outcome.Outcome != models.LifecycleInvalidInput {
				t.Fatalf("conflicting payload error = %v", err)
			}
			afterConflict, err := os.ReadFile(stateFile)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, afterConflict) {
				t.Fatal("conflicting retry changed persisted state")
			}
		})
	}
}

func TestRecoverAgentExplicitRequestNeedsTaskBoundary(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = nil
	testhelpers.WriteInitialState(t, stateFile, state)
	before, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = RecoverAgentWithOptions(root, "coder-unknown", false, "crashed", LifecycleRequestOptions{RequestID: "recovery", ExpectedTransition: strings.Repeat("a", 64)})
	var failure *LifecycleError
	if !errors.As(err, &failure) || failure.Outcome.Outcome != models.LifecycleInvalidInput || !strings.Contains(err.Error(), "expected_transition") {
		t.Fatalf("error = %v", err)
	}
	after, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid request changed state")
	}
}
