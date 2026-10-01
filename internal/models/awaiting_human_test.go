package models

import (
	"testing"
	"time"
)

func humanAskEntry(event TaskEventName, at time.Time, ask string) TaskHistoryEntry {
	entry := TaskHistoryEntry{Time: at, Event: event}
	if ask != "" {
		entry.Extra = map[string]any{AwaitingHumanExtraKey: ask}
	}
	return entry
}

func TestCurrentAwaitingHuman(t *testing.T) {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	at := func(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }
	claimed := TaskHistoryEntry{Time: at(-10), Event: TaskEventClaimed}
	blocked := func(ask string) TaskHistoryEntry { return humanAskEntry(TaskEventBlocked, at(0), ask) }
	assessed := func(minutes int, ask string) TaskHistoryEntry {
		return humanAskEntry(TaskEventOrchestratorAssessment, at(minutes), ask)
	}

	tests := []struct {
		name      string
		status    TaskStatus
		history   []TaskHistoryEntry
		wantAsk   string
		wantIndex int
	}{
		{name: "agent-owned block", status: TaskStatusBlocked, history: []TaskHistoryEntry{claimed, blocked("")}},
		{name: "ask on the blocked entry", status: TaskStatusBlocked, history: []TaskHistoryEntry{claimed, blocked("A")}, wantAsk: "A", wantIndex: 1},
		{name: "not blocked", status: TaskStatusReady, history: []TaskHistoryEntry{claimed, blocked("A")}},
		{name: "carry keeps the occurrence", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), assessed(1, "A"), assessed(2, "A")}, wantAsk: "A", wantIndex: 1},
		{name: "assessment sets an ask", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked(""), assessed(1, "A")}, wantAsk: "A", wantIndex: 2},
		{name: "assessment clears the ask", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), assessed(1, "")}},
		{name: "A clear A is a new occurrence", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), assessed(1, ""), assessed(2, "A")}, wantAsk: "A", wantIndex: 3},
		{name: "A B A is a new occurrence", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), assessed(1, "B"), assessed(2, "A")}, wantAsk: "A", wantIndex: 3},
		{name: "non-assessment entries in the episode do not decide", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), {Time: at(1), Event: TaskEventDependenciesRewritten}}, wantAsk: "A", wantIndex: 1},
		{name: "a new episode does not inherit", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), {Time: at(1), Event: TaskEventUnblocked}, {Time: at(2), Event: TaskEventClaimed}, humanAskEntry(TaskEventBlocked, at(3), "")}},
		{name: "a new episode with the same ask is a new occurrence", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked("A"), {Time: at(1), Event: TaskEventUnblocked}, {Time: at(2), Event: TaskEventClaimed}, humanAskEntry(TaskEventBlocked, at(3), "A")},
			wantAsk: "A", wantIndex: 4},
		{name: "no transition event: assessments decide", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{humanAskEntry(TaskEventCreated, at(-20), "ignored"), assessed(1, "A")}, wantAsk: "A", wantIndex: 1},
		{name: "legacy history has no ask", status: TaskStatusBlocked,
			history: []TaskHistoryEntry{claimed, blocked(""), assessed(1, "")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{ID: "task-1", Status: tt.status, History: tt.history}
			got, ok := CurrentAwaitingHuman(task)
			if ok != (tt.wantAsk != "") || got.Ask != tt.wantAsk {
				t.Fatalf("CurrentAwaitingHuman() = %+v, %v; want ask %q", got, ok, tt.wantAsk)
			}
			if ok && (got.Index != tt.wantIndex || !got.At.Equal(tt.history[tt.wantIndex].Time)) {
				t.Fatalf("occurrence = index %d at %s, want index %d at %s", got.Index, got.At, tt.wantIndex, tt.history[tt.wantIndex].Time)
			}
		})
	}
}

// Two assessments recorded in the same instant with different asks are two
// occurrences: the index tells them apart.
func TestCurrentAwaitingHuman_SameTimeDistinctOccurrences(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	task := &Task{ID: "task-1", Status: TaskStatusBlocked, History: []TaskHistoryEntry{
		humanAskEntry(TaskEventBlocked, at, "A"),
		humanAskEntry(TaskEventOrchestratorAssessment, at, "B"),
	}}
	first, _ := CurrentAwaitingHuman(&Task{ID: task.ID, Status: task.Status, History: task.History[:1]})
	second, _ := CurrentAwaitingHuman(task)
	if first.Index == second.Index || second.Ask != "B" {
		t.Fatalf("occurrences = %+v then %+v, want distinct indexes", first, second)
	}
}
