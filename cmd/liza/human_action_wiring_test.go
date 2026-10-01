package main

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D65: --human-action on mark-blocked and assess-blocked records the episode's
// human ask; --clear-human-action drops it.

const cliHumanAsk = "provision the staging database user, then add a human note"

func TestJSON_MarkBlocked_HumanAction(t *testing.T) {
	resetFlagIfPresent(markBlockedCmd, "human-action")
	t.Cleanup(func() { resetFlagIfPresent(markBlockedCmd, "human-action") })
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-human", models.TaskStatusImplementing, time.Now().UTC())}
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-human", "--agent-id", "coder-1",
		"--reason", "staging database user missing",
		"--questions", "Who provisions the staging user?",
		"--human-action", cliHumanAsk, "--json")
	if err != nil {
		t.Fatalf("mark-blocked --human-action failed: %v\n%s", err, stdout)
	}
	task := readState(t, statePath).FindTask("task-human")
	if human, ok := models.CurrentAwaitingHuman(task); !ok || human.Ask != cliHumanAsk {
		t.Fatalf("CurrentAwaitingHuman() = %+v, %v; want the ask", human, ok)
	}
}

func TestJSON_MarkBlocked_EmptyHumanActionIsRejected(t *testing.T) {
	resetFlagIfPresent(markBlockedCmd, "human-action")
	t.Cleanup(func() { resetFlagIfPresent(markBlockedCmd, "human-action") })
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("task-human", models.TaskStatusImplementing, time.Now().UTC())}
		state.Agents["coder-1"] = testhelpers.RegisteredTestAgent("coder")
	})

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"mark-blocked", "task-human", "--agent-id", "coder-1",
		"--reason", "r", "--questions", "q?", "--human-action", "", "--json")
	if err == nil || !strings.Contains(stdout, "--human-action must not be empty") {
		t.Fatalf("empty --human-action = %v\n%s; want a validation error", err, stdout)
	}
	if task := readState(t, statePath).FindTask("task-human"); task.Status != models.TaskStatusImplementing {
		t.Fatalf("status = %s, want the task untouched", task.Status)
	}
}

func TestJSON_AssessBlocked_HumanAction(t *testing.T) {
	for _, flag := range []string{"human-action", "clear-human-action", "note"} {
		resetFlagIfPresent(assessBlockedCmd, flag)
		t.Cleanup(func() { resetFlagIfPresent(assessBlockedCmd, flag) })
	}
	projectRoot, statePath := setupMutationTestProject(t, func(state *models.State) {
		task := testhelpers.BuildTaskByStatus("task-human", models.TaskStatusBlocked, time.Now().UTC())
		task.AssignedTo, task.Worktree = nil, nil
		state.Tasks = []models.Task{task}
		state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	})
	testhelpers.CreateSpecFile(t, projectRoot, "vision.md", "# Vision\n")
	ask := func() (string, bool) {
		human, ok := models.CurrentAwaitingHuman(readState(t, statePath).FindTask("task-human"))
		return human.Ask, ok
	}

	stdout, err := executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-human", "--agent-id", "orchestrator-1",
		"--note", "operator-owned", "--human-action", cliHumanAsk, "--json")
	if err != nil {
		t.Fatalf("assess-blocked --human-action failed: %v\n%s", err, stdout)
	}
	if result := parseEnvelope(t, stdout)["result"].(map[string]any); result["human_action"] != cliHumanAsk {
		t.Fatalf("result human_action = %#v", result["human_action"])
	}
	if got, ok := ask(); !ok || got != cliHumanAsk {
		t.Fatalf("stored ask = %q, %v", got, ok)
	}

	stdout, err = executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-human", "--agent-id", "orchestrator-1",
		"--human-action", cliHumanAsk, "--clear-human-action", "--json")
	if err == nil || !strings.Contains(stdout, "--clear-human-action cannot be combined with --human-action") {
		t.Fatalf("--human-action with --clear-human-action = %v\n%s; want a validation error", err, stdout)
	}

	resetFlagIfPresent(assessBlockedCmd, "human-action")
	stdout, err = executeRootCommandCapture(t, projectRoot,
		"assess-blocked", "task-human", "--agent-id", "orchestrator-1",
		"--note", "agents own the repair now", "--clear-human-action", "--json")
	if err != nil {
		t.Fatalf("assess-blocked --clear-human-action failed: %v\n%s", err, stdout)
	}
	if got, ok := ask(); ok {
		t.Fatalf("ask after --clear-human-action = %q", got)
	}
}
