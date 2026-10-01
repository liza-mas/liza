package models

import (
	"slices"
	"testing"
	"time"
)

var recoveryEpoch = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

// recoveryTask builds a task whose history ends with a blocked episode at the
// given minute; superseded tasks also carry the superseded entry, marked as a
// blocked recovery when marked is set.
func recoveryTask(id string, status TaskStatus, blockedMinute int, marked bool) Task {
	task := Task{ID: id, Status: status}
	if blockedMinute >= 0 {
		reason := id + " blocked"
		task.BlockedReason = &reason
		task.History = append(task.History, TaskHistoryEntry{Time: recoveryEpoch.Add(time.Duration(blockedMinute) * time.Minute), Event: TaskEventBlocked})
	}
	if status == TaskStatusSuperseded {
		entry := TaskHistoryEntry{Time: recoveryEpoch.Add(time.Duration(blockedMinute+1) * time.Minute), Event: TaskEventSuperseded}
		if marked {
			entry.Extra = map[string]any{BlockedRecoveryKey: BlockedRecoveryRecord(&task, "changed "+id)}
		}
		task.History = append(task.History, entry)
	}
	return task
}

// chainState links tasks oldest first through superseded_by only.
func chainState(tasks ...Task) *State {
	for i := 0; i+1 < len(tasks); i++ {
		tasks[i].SupersededBy = []string{tasks[i+1].ID}
	}
	return &State{Tasks: tasks}
}

func releaseFor(task *Task, resolved bool) CircuitBreakerHistory {
	pattern := BlockedReplacementChainPattern
	entry := CircuitBreakerHistory{Timestamp: recoveryEpoch.Add(time.Hour), Pattern: &pattern, Result: "TRIGGERED",
		Subject: &CircuitBreakerSubject{TaskID: task.ID, BlockedAt: LatestHistoryTime(task, TaskEventBlocked)}}
	if resolved {
		at := recoveryEpoch.Add(2 * time.Hour)
		entry.ResolvedAt = &at
	}
	return entry
}

func TestBlockedRecoveryLineage(t *testing.T) {
	t.Run("counts marked predecessors through superseded_by", func(t *testing.T) {
		state := chainState(recoveryTask("a", TaskStatusSuperseded, 0, true), recoveryTask("b", TaskStatusSuperseded, 2, true), recoveryTask("c", TaskStatusBlocked, 4, false))
		chain := BlockedRecoveryLineage(state, state.FindTask("c"))
		if !slices.Equal(chain.Path, []string{"a", "b", "c"}) || !slices.Equal(chain.Recoveries, []string{"a", "b"}) {
			t.Fatalf("chain = %+v", chain)
		}
	})

	t.Run("reads supersedes when superseded_by is absent", func(t *testing.T) {
		a, b, c := recoveryTask("a", TaskStatusSuperseded, 0, true), recoveryTask("b", TaskStatusSuperseded, 2, true), recoveryTask("c", TaskStatusBlocked, 4, false)
		b.Supersedes, c.Supersedes = &a.ID, &b.ID
		state := &State{Tasks: []Task{a, b, c}}
		if chain := BlockedRecoveryLineage(state, state.FindTask("c")); len(chain.Recoveries) != 2 {
			t.Fatalf("chain = %+v, want 2 recoveries", chain)
		}
	})

	t.Run("excludes unmarked intermediates and the head", func(t *testing.T) {
		// b was replaced while not BLOCKED (or before markers existed).
		state := chainState(recoveryTask("a", TaskStatusSuperseded, 0, true), recoveryTask("b", TaskStatusSuperseded, -1, false), recoveryTask("c", TaskStatusBlocked, 4, false))
		chain := BlockedRecoveryLineage(state, state.FindTask("c"))
		if !slices.Equal(chain.Path, []string{"a", "b", "c"}) || !slices.Equal(chain.Recoveries, []string{"a"}) {
			t.Fatalf("chain = %+v", chain)
		}
	})

	t.Run("a shared ancestor does not hide the longer path", func(t *testing.T) {
		// root splits into x and y; y continues to z. z's lineage is root -> y -> z.
		root, x, y, z := recoveryTask("root", TaskStatusSuperseded, 0, true), recoveryTask("x", TaskStatusBlocked, 2, false), recoveryTask("y", TaskStatusSuperseded, 3, true), recoveryTask("z", TaskStatusBlocked, 5, false)
		root.SupersededBy = []string{"x", "y"}
		y.SupersededBy = []string{"z"}
		state := &State{Tasks: []Task{root, x, y, z}}
		if chain := BlockedRecoveryLineage(state, state.FindTask("z")); !slices.Equal(chain.Recoveries, []string{"root", "y"}) {
			t.Fatalf("z chain = %+v", chain)
		}
		if chain := BlockedRecoveryLineage(state, state.FindTask("x")); !slices.Equal(chain.Recoveries, []string{"root"}) {
			t.Fatalf("x chain = %+v", chain)
		}
	})

	t.Run("a malformed cycle terminates", func(t *testing.T) {
		a, b := recoveryTask("a", TaskStatusSuperseded, 0, true), recoveryTask("b", TaskStatusBlocked, 2, false)
		a.SupersededBy, b.SupersededBy = []string{"b"}, []string{"a"}
		state := &State{Tasks: []Task{a, b}}
		if chain := BlockedRecoveryLineage(state, state.FindTask("b")); len(chain.Path) > 2 {
			t.Fatalf("chain = %+v", chain)
		}
	})
}

func TestBlockedRecoveryCapped(t *testing.T) {
	capped := func() *State {
		return chainState(recoveryTask("a", TaskStatusSuperseded, 0, true), recoveryTask("b", TaskStatusSuperseded, 2, true), recoveryTask("c", TaskStatusBlocked, 4, false))
	}

	t.Run("caps a BLOCKED head after two blocked recoveries", func(t *testing.T) {
		state := capped()
		if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); !ok {
			t.Fatal("not capped")
		}
	})

	t.Run("one blocked recovery stays below the cap", func(t *testing.T) {
		state := chainState(recoveryTask("b", TaskStatusSuperseded, 2, true), recoveryTask("c", TaskStatusBlocked, 4, false))
		if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); ok {
			t.Fatal("capped after one recovery")
		}
	})

	t.Run("a non-BLOCKED head is never capped", func(t *testing.T) {
		state := capped()
		state.FindTask("c").Status = TaskStatusReady
		if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); ok {
			t.Fatal("READY head capped")
		}
	})

	t.Run("first block after two unmarked replacements is not capped", func(t *testing.T) {
		state := chainState(recoveryTask("a", TaskStatusSuperseded, -1, false), recoveryTask("b", TaskStatusSuperseded, -1, false), recoveryTask("c", TaskStatusBlocked, 4, false))
		if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); ok {
			t.Fatal("first block capped")
		}
	})

	t.Run("a resolved release for this task and episode lifts the cap", func(t *testing.T) {
		state := capped()
		state.CircuitBreaker.History = []CircuitBreakerHistory{releaseFor(state.FindTask("c"), true)}
		if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); ok {
			t.Fatal("released chain still capped")
		}
	})

	t.Run("releases that do not match stay capped", func(t *testing.T) {
		cases := map[string]func(*State) CircuitBreakerHistory{
			"unresolved": func(s *State) CircuitBreakerHistory { return releaseFor(s.FindTask("c"), false) },
			"other task": func(s *State) CircuitBreakerHistory {
				e := releaseFor(s.FindTask("c"), true)
				e.Subject.TaskID = "z"
				return e
			},
			"earlier episode": func(s *State) CircuitBreakerHistory {
				e := releaseFor(s.FindTask("c"), true)
				e.Subject.BlockedAt = recoveryEpoch
				return e
			},
			"no subject": func(s *State) CircuitBreakerHistory {
				e := releaseFor(s.FindTask("c"), true)
				e.Subject = nil
				return e
			},
			"other pattern": func(s *State) CircuitBreakerHistory {
				e := releaseFor(s.FindTask("c"), true)
				other := "planning_review_churn"
				e.Pattern = &other
				return e
			},
		}
		for name, release := range cases {
			t.Run(name, func(t *testing.T) {
				state := capped()
				state.CircuitBreaker.History = []CircuitBreakerHistory{release(state)}
				if _, ok := BlockedRecoveryCapped(state, state.FindTask("c")); !ok {
					t.Fatal("not capped")
				}
			})
		}
	})

	t.Run("a re-block after release is capped again", func(t *testing.T) {
		state := capped()
		c := state.FindTask("c")
		state.CircuitBreaker.History = []CircuitBreakerHistory{releaseFor(c, true)}
		c.History = append(c.History, TaskHistoryEntry{Time: recoveryEpoch.Add(3 * time.Hour), Event: TaskEventBlocked})
		if _, ok := BlockedRecoveryCapped(state, c); !ok {
			t.Fatal("re-blocked task not capped")
		}
	})

	t.Run("a task with no recorded block is never released", func(t *testing.T) {
		state := capped()
		c := state.FindTask("c")
		state.CircuitBreaker.History = []CircuitBreakerHistory{releaseFor(c, true)}
		c.History = nil
		if BlockedRecoveryReleased(state, c) {
			t.Fatal("released without a blocked episode")
		}
	})
}
