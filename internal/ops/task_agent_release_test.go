package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMarkBlocked_ReleasesAssignedAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-1"
	agentID := "coder-1"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(taskID, models.TaskStatusImplementing, now),
	}
	state.Agents[agentID] = workingAgentForTask(taskID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := MarkBlocked(tmpDir, taskID, "Missing API spec", []string{"What is the API format?"}, agentID)
	if err != nil {
		t.Fatalf("MarkBlocked() error = %v", err)
	}

	assertAgentReleasedFromTask(t, stateFile, agentID)
}

func TestSupersedeTask_ReleasesAssignedAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-1"
	agentID := "coder-1"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(taskID, models.TaskStatusBlocked, now),
	}
	state.Agents[agentID] = workingAgentForTask(taskID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := SupersedeTaskWithOptions(tmpDir, taskID, []string{"task-2"}, "Split into smaller tasks", "orchestrator-1", SupersedeTaskOptions{Changed: "test: changed since block"})
	if err != nil {
		t.Fatalf("SupersedeTask() error = %v", err)
	}

	assertAgentReleasedFromTask(t, stateFile, agentID)
}

func TestCancelTask_ReleasesAssignedAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-1"
	agentID := "coder-1"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(taskID, models.TaskStatusBlocked, now),
	}
	state.Agents[agentID] = workingAgentForTask(taskID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := CancelTask(tmpDir, taskID, "No longer needed", "orchestrator-1")
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}

	assertAgentReleasedFromTask(t, stateFile, agentID)
}

func TestReconcileMerged_ReleasesAssignedAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-1"
	agentID := "coder-1"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus(taskID, models.TaskStatusIntegrationFailed, now),
	}
	state.Agents[agentID] = workingAgentForTask(taskID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	mergeCommit := testhelpers.MustGit(t, tmpDir, "rev-parse", "HEAD")
	_, err := ReconcileMerged(tmpDir, taskID, mergeCommit, "", "PR merged externally", "orchestrator-1")
	if err != nil {
		t.Fatalf("ReconcileMerged() error = %v", err)
	}

	assertAgentReleasedFromTask(t, stateFile, agentID)
}

func TestAssessHypothesisExhausted_ReleasesAssignedAgent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, tmpDir)

	taskID := "task-1"
	agentID := "coder-1"
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReady, now)
	task.FailedBy = []string{"coder-1", "coder-2"}
	state.Tasks = []models.Task{task}
	state.Agents[agentID] = workingAgentForTask(taskID, now)
	testhelpers.WriteInitialState(t, stateFile, state)

	_, err := AssessHypothesisExhausted(tmpDir, taskID, "Needs spec revision", "orchestrator-1")
	if err != nil {
		t.Fatalf("AssessHypothesisExhausted() error = %v", err)
	}

	assertAgentReleasedFromTask(t, stateFile, agentID)
}

func workingAgentForTask(taskID string, now time.Time) models.Agent {
	return models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusWorking,
		CurrentTask:  &taskID,
		LeaseExpires: testhelpers.TimePtr(now.Add(-time.Hour)),
		Heartbeat:    now.Add(-time.Hour),
	}
}

func assertAgentReleasedFromTask(t *testing.T, stateFile, agentID string) {
	t.Helper()

	readState, err := db.New(stateFile).Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	agent, ok := readState.Agents[agentID]
	if !ok {
		t.Fatalf("agent %s missing", agentID)
	}
	if agent.Status != models.AgentStatusIdle {
		t.Fatalf("agent status = %s, want %s", agent.Status, models.AgentStatusIdle)
	}
	if agent.CurrentTask != nil {
		t.Fatalf("agent current_task = %s, want nil", *agent.CurrentTask)
	}
	if agent.LeaseExpires != nil {
		t.Fatalf("agent lease_expires = %v, want nil", *agent.LeaseExpires)
	}
}
