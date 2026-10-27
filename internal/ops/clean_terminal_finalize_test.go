package ops

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func c6CleanTerminalFixture(t *testing.T, agentTask string) *c6Fixture {
	t.Helper()
	now := time.Now().UTC()
	return newC6Fixture(t, func(s *models.State) {
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusDraft, now)
		task.AssignedTo = testhelpers.StringPtr("coder-1")
		s.Tasks = []models.Task{task}
		coder := c6LiveAgent("coder", now)
		coder.Status = models.AgentStatusWorking
		coder.CurrentTask = testhelpers.StringPtr(agentTask)
		s.Agents["coder-1"] = coder
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", now)
	})
}

func TestC6FinalizeCleanTerminalTaskReleasesMatchingAgentAndRecordsCompletion(t *testing.T) {
	f := c6CleanTerminalFixture(t, "task-1")
	started := time.Now().Add(-time.Second)

	if err := FinalizeCleanTerminalTask(f.bb, c6Authority("orchestrator-1"), "task-1"); err != nil {
		t.Fatalf("FinalizeCleanTerminalTask: %v", err)
	}

	state := f.read(t)
	if coder := state.Agents["coder-1"]; coder.CurrentTask != nil || coder.Status != models.AgentStatusIdle {
		t.Fatalf("assigned agent = %+v, want released", coder)
	}
	task := state.FindTask("task-1")
	if len(task.HandoffEvents) != 1 {
		t.Fatalf("handoff events = %+v, want exactly the completion handoff", task.HandoffEvents)
	}
	event := task.HandoffEvents[0]
	if event.Agent != "system" || event.Trigger != models.HandoffTriggerCompletion || event.Timestamp.Before(started) {
		t.Fatalf("handoff event = %+v, want a completion handoff by system", event)
	}
}

func TestC6FinalizeCleanTerminalTaskKeepsAnAgentWorkingElsewhere(t *testing.T) {
	f := c6CleanTerminalFixture(t, "task-2")

	if err := FinalizeCleanTerminalTask(f.bb, c6Authority("orchestrator-1"), "task-1"); err != nil {
		t.Fatalf("FinalizeCleanTerminalTask: %v", err)
	}

	state := f.read(t)
	coder := state.Agents["coder-1"]
	if coder.CurrentTask == nil || *coder.CurrentTask != "task-2" || coder.Status != models.AgentStatusWorking {
		t.Fatalf("agent working on another task was released: %+v", coder)
	}
	if got := len(state.FindTask("task-1").HandoffEvents); got != 1 {
		t.Fatalf("handoff events = %d, want the completion handoff still recorded", got)
	}
}

func TestC6FinalizeCleanTerminalTaskMissingTaskAndStaleAuthority(t *testing.T) {
	f := c6CleanTerminalFixture(t, "task-1")
	before := f.bytes(t)

	if err := FinalizeCleanTerminalTask(f.bb, c6Authority("orchestrator-1"), "no-such-task"); err != nil {
		t.Fatalf("missing task = %v, want a no-op", err)
	}
	f.requireUnchanged(t, before, "finalizing a missing task")

	stale := models.AgentAuthority{ID: "orchestrator-1", Generation: "replaced"}
	requireAuthorityError(t, FinalizeCleanTerminalTask(f.bb, stale, "task-1"), "orchestrator-1")
	f.requireUnchanged(t, before, "finalizing by a replaced generation")
}
