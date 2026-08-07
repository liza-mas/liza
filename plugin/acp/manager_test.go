package acp

import (
	"testing"
	"time"
)

func TestRunManagerLifecycle(t *testing.T) {
	// Own the clock rather than race it: the work below takes microseconds, and
	// Windows measures that as zero.
	clock := stubClock(t, 250*time.Millisecond)

	m := NewRunManager()
	run1 := m.Start("task-1")
	if run1.Warm {
		t.Fatalf("first run must be cold")
	}
	if run1.SessionID == "" {
		t.Fatalf("expected session id")
	}

	err := m.HandleEvent(Event{
		Kind:    EventUsage,
		TaskID:  "task-1",
		Payload: map[string]any{"usage": map[string]any{"input_tokens": 40, "cached_read_tokens": 10}},
	})
	if err != nil {
		t.Fatalf("handle usage: %v", err)
	}

	err = m.HandleEvent(Event{
		Kind:    EventMessage,
		TaskID:  "task-1",
		Message: "first output chunk",
	})
	if err != nil {
		t.Fatalf("handle output: %v", err)
	}

	metric1, err := m.Finish("task-1", 0)
	if err != nil {
		t.Fatalf("finish run: %v", err)
	}
	if metric1.Usage.InputTokens != 40 {
		t.Fatalf("unexpected tokens: %#v", metric1.Usage)
	}
	if metric1.Duration != clock.step {
		t.Fatalf("duration = %s, want the one clock step between start and finish (%s)", metric1.Duration, clock.step)
	}
	if metric1.Output != "first output chunk" {
		t.Fatalf("unexpected output: %q", metric1.Output)
	}

	run2 := m.Start("task-1")
	if !run2.Warm {
		t.Fatalf("second run should be warm")
	}
	if run2.SessionID != run1.SessionID {
		t.Fatalf("expected warm reuse %s, got %s", run1.SessionID, run2.SessionID)
	}
}

func TestRunManagerWarmSessionAcrossDistinctTasks(t *testing.T) {
	m := NewRunManager()

	first := m.StartWithSessionKey("task-1", "project-agent-session")
	if first.Warm {
		t.Fatalf("first run should be cold")
	}
	if _, err := m.Finish("task-1", 0); err != nil {
		t.Fatalf("finish first: %v", err)
	}

	second := m.StartWithSessionKey("task-2", "project-agent-session")
	if !second.Warm {
		t.Fatalf("second distinct task should reuse warm session")
	}
	if second.TaskID != "task-2" {
		t.Fatalf("real task id not preserved: %q", second.TaskID)
	}
	if second.SessionID != first.SessionID {
		t.Fatalf("session not reused: first=%q second=%q", first.SessionID, second.SessionID)
	}
}

func TestRunManagerFinishWithoutStart(t *testing.T) {
	m := NewRunManager()
	if _, err := m.Finish("missing", 0); err == nil {
		t.Fatalf("expected error on missing run")
	}
}

func TestRunManagerSummaryFromEvents(t *testing.T) {
	m := NewRunManager()

	first := m.Start("task-1")
	time.Sleep(2 * time.Millisecond)
	_ = m.HandleEvent(Event{Kind: EventUsage, TaskID: first.TaskID, Payload: map[string]any{"usage": map[string]any{"input_tokens": 220}}})
	_, err := m.Finish("task-1", 0)
	if err != nil {
		t.Fatalf("finish first: %v", err)
	}

	second := m.Start("task-1")
	time.Sleep(2 * time.Millisecond)
	_ = m.HandleEvent(Event{Kind: EventUsage, TaskID: second.TaskID, Payload: map[string]any{"usage": map[string]any{"input_tokens": 8}}})
	_, err = m.Finish("task-1", 0)
	if err != nil {
		t.Fatalf("finish second: %v", err)
	}

	sum, err := m.Summary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.FreshRuns != 1 || sum.WarmRuns != 1 {
		t.Fatalf("unexpected run split %#v", sum)
	}
	if sum.InputTokenSavingsPercent <= 96 {
		t.Fatalf("expected large warm savings, got %f", sum.InputTokenSavingsPercent)
	}
}
