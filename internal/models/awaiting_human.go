package models

import (
	"strings"
	"time"
)

// AwaitingHumanExtraKey is the history Extra key naming the action only a
// human can take to clear a BLOCKED episode (ADR-0172). The blocking entry
// carries it when the block is human-owned from the start; an orchestrator
// assessment carries the current ask forward, replaces it, or omits it to
// clear it.
const AwaitingHumanExtraKey = "awaiting_human"

// AwaitingHumanOccurrence is the current human ask of a BLOCKED task and the
// history entry where it began.
type AwaitingHumanOccurrence struct {
	Ask   string
	Index int
	At    time.Time
}

// CurrentAwaitingHuman returns the ask of a BLOCKED task's current episode,
// if any. The episode runs from its most recent status-transition (or
// unclassified) event; within it, the episode's opening entry and each later
// orchestrator assessment set the ask, and the last of them decides. The
// occurrence starts at the first entry of the final unbroken run of that same
// ask, so carrying an ask forward keeps its occurrence while clearing or
// changing it and coming back starts a new one. History is append-only, so the
// index identifies the occurrence even between entries with equal times.
//
// Legacy state, and blocks recorded without an ask, have no key: not awaiting
// a human.
func CurrentAwaitingHuman(task *Task) (AwaitingHumanOccurrence, bool) {
	if task == nil || task.Status != TaskStatusBlocked {
		return AwaitingHumanOccurrence{}, false
	}
	start := -1
	for i := len(task.History) - 1; i >= 0; i-- {
		if transition, classified := IsStatusTransitionEvent(task.History[i].Event); transition || !classified {
			start = i
			break
		}
	}
	// Without a transition event the whole history is the episode, and only
	// its assessments decide.
	var current AwaitingHumanOccurrence
	for i := max(start, 0); i < len(task.History); i++ {
		entry := &task.History[i]
		if i != start && entry.Event != TaskEventOrchestratorAssessment {
			continue
		}
		ask := AwaitingHumanAskOf(entry)
		switch {
		case ask == "":
			current = AwaitingHumanOccurrence{}
		case ask != current.Ask:
			current = AwaitingHumanOccurrence{Ask: ask, Index: i, At: entry.Time}
		}
	}
	return current, current.Ask != ""
}

// AwaitingHumanAskOf returns the ask recorded on one history entry, or "".
func AwaitingHumanAskOf(entry *TaskHistoryEntry) string {
	ask, _ := entry.Extra[AwaitingHumanExtraKey].(string)
	return strings.TrimSpace(ask)
}
