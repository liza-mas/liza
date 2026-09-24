package models

import (
	"testing"
	"time"
)

func TestInVerdictHandoffBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	withLast := func(event string, at time.Time) *Task {
		return &Task{History: []TaskHistoryEntry{
			{Time: now.Add(-time.Hour), Event: TaskEventSubmittedForReview},
			{Time: at, Event: event},
		}}
	}

	tests := []struct {
		name string
		task *Task
		want bool
	}{
		{"rejection at now", withLast(TaskEventRejected, now), true},
		{"approval at now", withLast(TaskEventApproved, now), true},
		{"verdict exactly G ago", withLast(TaskEventRejected, now.Add(-VerdictHandoffGrace)), true},
		{"verdict G+1ns ago", withLast(TaskEventRejected, now.Add(-VerdictHandoffGrace-time.Nanosecond)), false},
		{"verdict 1s in the future", withLast(TaskEventRejected, now.Add(time.Second)), false},
		{"zero verdict time", withLast(TaskEventRejected, time.Time{}), false},
		{"last entry is not a verdict", withLast(TaskEventBlocked, now), false},
		{"no history", &Task{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InVerdictHandoff(tt.task, now); got != tt.want {
				t.Errorf("InVerdictHandoff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLatestHistoryTimeReturnsMostRecentMatchingEntry(t *testing.T) {
	first := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	task := &Task{History: []TaskHistoryEntry{
		{Time: first, Event: TaskEventBlocked},
		{Time: first.Add(time.Minute), Event: TaskEventUnblocked},
		{Time: second, Event: TaskEventBlocked},
		{Time: second.Add(time.Minute), Event: TaskEventOutputSet},
	}}

	if got := LatestHistoryTime(task, TaskEventBlocked); !got.Equal(second) {
		t.Errorf("LatestHistoryTime(blocked) = %s, want %s", got, second)
	}
	if got := LatestHistoryTime(task, TaskEventRejected); !got.IsZero() {
		t.Errorf("LatestHistoryTime(rejected) = %s, want zero", got)
	}
}
