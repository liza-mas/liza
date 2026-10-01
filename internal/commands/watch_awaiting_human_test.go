package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D65: a BLOCKED task whose current episode names a human action raises one
// AWAITING HUMAN alert naming the task and the ask, and STALLED names it.

const humanAsk = "record the w03 fixture instance, then unblock the task"

// humanOwnedBlockedTask returns a BLOCKED task, as a blocking write leaves it,
// whose blocked entry carries ask under the persisted awaiting_human key; an
// empty ask leaves the key out.
func humanOwnedBlockedTask(blockedAt time.Time, ask string) models.Task {
	task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, blockedAt.Add(-time.Hour))
	task.AssignedTo = nil
	agent := "coder-1"
	entry := models.TaskHistoryEntry{Time: blockedAt, Event: models.TaskEventBlocked, Agent: &agent, Reason: task.BlockedReason}
	if ask != "" {
		entry.Extra = map[string]any{"awaiting_human": ask}
	}
	task.History = []models.TaskHistoryEntry{
		{Time: blockedAt.Add(-50 * time.Minute), Event: models.TaskEventClaimed, Agent: &agent},
		entry,
	}
	return task
}

func awaitingHumanAlerts(snapshot AlertSnapshot) []Alert {
	var found []Alert
	for _, alert := range snapshot.Alerts {
		if alert.Category == "AWAITING HUMAN" {
			found = append(found, alert)
		}
	}
	return found
}

func TestAwaitingHuman_HumanOwnedBlockAlertsOnceWithTheAsk(t *testing.T) {
	// GIVEN a task blocked on a human action
	config := newEmissionWatchConfig(t)
	task := humanOwnedBlockedTask(time.Now().UTC().Add(-time.Minute), humanAsk)

	// WHEN the watch checks twice
	first := awaitingHumanAlerts(RunChecksWithStateSnapshot(stateWithTask(task), config))
	second := awaitingHumanAlerts(RunChecksWithStateSnapshot(stateWithTask(task), config))

	// THEN exactly one critical alert names the task and the ask
	if len(first) != 1 {
		t.Fatalf("AWAITING HUMAN alerts on first check = %v, want one", first)
	}
	if first[0].Level != AlertLevelCritical {
		t.Errorf("level = %s, want critical", first[0].Level)
	}
	for _, want := range []string{"task-1", humanAsk} {
		if !strings.Contains(first[0].Message, want) {
			t.Errorf("message = %q, want it to contain %q", first[0].Message, want)
		}
	}
	if len(second) != 0 {
		t.Fatalf("AWAITING HUMAN alerts on repeat check = %v, want none", second)
	}
}

func TestAwaitingHuman_AgentOwnedBlockRaisesNone(t *testing.T) {
	config := newEmissionWatchConfig(t)
	task := humanOwnedBlockedTask(time.Now().UTC().Add(-time.Minute), "")

	if got := awaitingHumanAlerts(RunChecksWithStateSnapshot(stateWithTask(task), config)); len(got) != 0 {
		t.Fatalf("AWAITING HUMAN alerts = %v, want none for a block without an ask", got)
	}
}

func TestAwaitingHuman_NewEpisodeDoesNotInheritAnEarlierAsk(t *testing.T) {
	// GIVEN an earlier human-owned episode, unblocked, then blocked again
	// without an ask
	config := newEmissionWatchConfig(t)
	now := time.Now().UTC()
	task := humanOwnedBlockedTask(now.Add(-30*time.Minute), humanAsk)
	agent := "coder-1"
	orchestrator := "orchestrator-1"
	task.History = append(task.History,
		models.TaskHistoryEntry{Time: now.Add(-20 * time.Minute), Event: models.TaskEventUnblocked, Agent: &orchestrator},
		models.TaskHistoryEntry{Time: now.Add(-15 * time.Minute), Event: models.TaskEventClaimed, Agent: &agent},
		models.TaskHistoryEntry{Time: now.Add(-time.Minute), Event: models.TaskEventBlocked, Agent: &agent, Reason: task.BlockedReason},
	)

	if got := awaitingHumanAlerts(RunChecksWithStateSnapshot(stateWithTask(task), config)); len(got) != 0 {
		t.Fatalf("AWAITING HUMAN alerts = %v, want none: the current episode names no human action", got)
	}
}

func TestAwaitingHuman_StalledNamesHumanOwnedBlockers(t *testing.T) {
	// GIVEN the only task is blocked on a human action and progress stopped
	// past the stall threshold
	t0 := time.Now().UTC()
	originalNow := watchNow
	t.Cleanup(func() { watchNow = originalNow })
	watchNow = func() time.Time { return t0 }
	config := newEmissionWatchConfig(t)
	task := humanOwnedBlockedTask(t0.Add(-35*time.Minute), humanAsk)

	// WHEN the watch checks
	snapshot := RunChecksWithStateSnapshot(stateWithTask(task), config)

	// THEN STALLED names the human-owned blocker
	var stalled []Alert
	for _, alert := range snapshot.Alerts {
		if alert.Category == "STALLED" {
			stalled = append(stalled, alert)
		}
	}
	if len(stalled) != 1 {
		t.Fatalf("STALLED alerts = %v, want one", stalled)
	}
	if !strings.Contains(stalled[0].Message, "awaiting human: task-1") {
		t.Fatalf("STALLED message = %q, want it to name the human-owned blocker", stalled[0].Message)
	}
}

// Occurrence keys through the real once-ledger: each tick runs the checks and
// writes what they emit, exactly as the watch loop does, into one alerts log.

type awaitingHumanLog struct {
	path string
}

func newAwaitingHumanLog(t *testing.T) awaitingHumanLog {
	return awaitingHumanLog{path: filepath.Join(t.TempDir(), "alerts.log")}
}

func (l awaitingHumanLog) tick(t *testing.T, config WatchConfig, task models.Task) {
	t.Helper()
	for _, alert := range RunChecksWithStateSnapshot(stateWithTask(task), config).Alerts {
		if alert.Category != "AWAITING HUMAN" {
			continue
		}
		if err := WriteAlert(l.path, alert); err != nil {
			t.Fatalf("WriteAlert: %v", err)
		}
	}
}

func (l awaitingHumanLog) lines(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(l.path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "AWAITING HUMAN: ")
}

func withAssessment(task models.Task, at time.Time, ask string) models.Task {
	orchestrator := "orchestrator-1"
	entry := models.TaskHistoryEntry{Time: at, Event: models.TaskEventOrchestratorAssessment, Agent: &orchestrator}
	if ask != "" {
		entry.Extra = map[string]any{"awaiting_human": ask}
	}
	task.History = append(append([]models.TaskHistoryEntry(nil), task.History...), entry)
	return task
}

func TestAwaitingHuman_OccurrenceLedger(t *testing.T) {
	now := time.Now().UTC()
	blocked := humanOwnedBlockedTask(now.Add(-10*time.Minute), "A")
	at := func(minutes int) time.Time { return now.Add(time.Duration(minutes-9) * time.Minute) }

	t.Run("carries are one occurrence across ticks", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		task := blocked
		log.tick(t, config, task)
		task = withAssessment(task, at(1), "A")
		log.tick(t, config, task)
		task = withAssessment(task, at(2), "A")
		log.tick(t, config, task)
		if got := log.lines(t); got != 1 {
			t.Fatalf("lines = %d, want 1", got)
		}
	})

	t.Run("several watchers and a restart write it once", func(t *testing.T) {
		log := newAwaitingHumanLog(t)
		first, second := newEmissionWatchConfig(t), newEmissionWatchConfig(t)
		log.tick(t, first, blocked)
		log.tick(t, second, blocked)
		restarted := newEmissionWatchConfig(t)
		log.tick(t, restarted, blocked)
		if got := log.lines(t); got != 1 {
			t.Fatalf("lines = %d, want 1", got)
		}
	})

	t.Run("A clear A with a tick on the cleared state alerts twice", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		task := blocked
		log.tick(t, config, task)
		task = withAssessment(task, at(1), "")
		log.tick(t, config, task)
		task = withAssessment(task, at(2), "A")
		log.tick(t, config, task)
		if got := log.lines(t); got != 2 {
			t.Fatalf("lines = %d, want 2", got)
		}
	})

	t.Run("A clear A between two ticks alerts twice", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		task := blocked
		log.tick(t, config, task)
		task = withAssessment(withAssessment(task, at(1), ""), at(2), "A")
		log.tick(t, config, task)
		if got := log.lines(t); got != 2 {
			t.Fatalf("lines = %d, want 2", got)
		}
	})

	t.Run("A B A alerts three times", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		task := blocked
		log.tick(t, config, task)
		task = withAssessment(task, at(1), "B")
		log.tick(t, config, task)
		task = withAssessment(task, at(2), "A")
		log.tick(t, config, task)
		if got := log.lines(t); got != 3 {
			t.Fatalf("lines = %d, want 3", got)
		}
	})

	t.Run("same-time assessments with different asks are distinct", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		same := at(1)
		task := withAssessment(blocked, same, "B")
		log.tick(t, config, task)
		task = withAssessment(task, same, "C")
		log.tick(t, config, task)
		if got := log.lines(t); got != 2 {
			t.Fatalf("lines = %d, want 2", got)
		}
	})

	t.Run("a new episode with the same ask alerts again", func(t *testing.T) {
		log, config := newAwaitingHumanLog(t), newEmissionWatchConfig(t)
		task := blocked
		log.tick(t, config, task)
		agent, orchestrator := "coder-1", "orchestrator-1"
		task.History = append(append([]models.TaskHistoryEntry(nil), task.History...),
			models.TaskHistoryEntry{Time: at(1), Event: models.TaskEventUnblocked, Agent: &orchestrator},
			models.TaskHistoryEntry{Time: at(2), Event: models.TaskEventClaimed, Agent: &agent},
			models.TaskHistoryEntry{Time: at(3), Event: models.TaskEventBlocked, Agent: &agent, Extra: map[string]any{"awaiting_human": "A"}},
		)
		log.tick(t, config, task)
		if got := log.lines(t); got != 2 {
			t.Fatalf("lines = %d, want 2", got)
		}
	})
}
