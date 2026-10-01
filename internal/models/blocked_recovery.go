package models

import (
	"strings"
	"time"
)

// Blocked-recovery cap (ADR-0171). Replacing a BLOCKED task is a recovery
// attempt; a lineage that keeps blocking after replacement is not converging.
// Agents may perform at most MaxAgentBlockedRecoveries such replacements per
// lineage. The next one is refused until a human resolves the
// blocked_replacement_chain halt reported for that exact task and blocked
// episode. Supersession, plan-output admission and circuit-breaker detection
// all decide through BlockedRecoveryCapped so they cannot disagree.
const (
	// MaxAgentBlockedRecoveries is the number of blocked-recovery replacements
	// a lineage may accumulate before further agent replacement is refused.
	MaxAgentBlockedRecoveries = 2
	// BlockedReplacementChainPattern names the circuit-breaker pattern that
	// reports a capped lineage and whose resolution releases it.
	BlockedReplacementChainPattern = "blocked_replacement_chain"
	// BlockedRecoveryKey marks a superseded history entry whose task was
	// BLOCKED when it was replaced. Unmarked (including legacy) entries do not
	// count toward the cap.
	BlockedRecoveryKey = "blocked_recovery"
)

// BlockedRecoveryRecord returns the marker value for superseding task while it
// is BLOCKED: the cause being replaced, the blocked episode and the stated
// change. changed must already be masked by the caller.
func BlockedRecoveryRecord(task *Task, changed string) map[string]any {
	record := map[string]any{}
	if task.BlockedReason != nil && *task.BlockedReason != "" {
		record["blocked_reason"] = *task.BlockedReason
	}
	if blockedAt := BlockedEpisodeAt(task); !blockedAt.IsZero() {
		record["blocked_at"] = blockedAt.UTC().Format(time.RFC3339Nano)
	}
	if changed = strings.TrimSpace(changed); changed != "" {
		record["changed"] = changed
	}
	return record
}

// BlockedEpisodeAt identifies a BLOCKED task's current blocked episode: when it
// entered BLOCKED, i.e. its most recent status-transition event. Every path
// into BLOCKED records one — TaskEventBlocked, or TaskEventRecoveryFreshFailed
// for a failed fresh recovery. Zero when the task is not BLOCKED or has none.
func BlockedEpisodeAt(task *Task) time.Time {
	if task.Status != TaskStatusBlocked {
		return time.Time{}
	}
	for i := len(task.History) - 1; i >= 0; i-- {
		if transition, _ := IsStatusTransitionEvent(task.History[i].Event); transition {
			return task.History[i].Time
		}
	}
	return time.Time{}
}

// BlockedRecovery returns the marker recorded when task was superseded while
// BLOCKED, or nil when its retirement was not a blocked recovery.
func BlockedRecovery(task *Task) map[string]any {
	for i := len(task.History) - 1; i >= 0; i-- {
		entry := task.History[i]
		if entry.Event != TaskEventSuperseded {
			continue
		}
		record, _ := entry.Extra[BlockedRecoveryKey].(map[string]any)
		return record
	}
	return nil
}

// BlockedRecoveryChain is the supersession lineage behind a task.
type BlockedRecoveryChain struct {
	// Path runs from the oldest predecessor to the task itself, for diagnostics.
	Path []string
	// Recoveries lists the predecessors on Path retired by a blocked-recovery
	// supersession. Its length is what the cap counts; the task itself and
	// unmarked intermediates are excluded.
	Recoveries []string
}

// BlockedRecoveryLineage returns the lineage of task with the most
// blocked-recovery predecessors. Predecessors are read from both directions
// because supersede-task records only superseded_by, replan only supersedes.
func BlockedRecoveryLineage(state *State, task *Task) BlockedRecoveryChain {
	predecessors := make(map[string][]string)
	add := func(successor, predecessor string) {
		if successor != "" && predecessor != "" && successor != predecessor {
			predecessors[successor] = append(predecessors[successor], predecessor)
		}
	}
	for i := range state.Tasks {
		t := &state.Tasks[i]
		for _, successor := range t.SupersededBy {
			add(successor, t.ID)
		}
		if t.Supersedes != nil {
			add(t.ID, *t.Supersedes)
		}
	}
	onPath := map[string]bool{}
	var walk func(id string) BlockedRecoveryChain
	walk = func(id string) BlockedRecoveryChain {
		onPath[id] = true
		defer delete(onPath, id)
		var best BlockedRecoveryChain
		found := false
		seen := map[string]bool{}
		for _, predecessorID := range predecessors[id] {
			if seen[predecessorID] || onPath[predecessorID] {
				continue
			}
			seen[predecessorID] = true
			chain := walk(predecessorID)
			if predecessor := state.FindTask(predecessorID); predecessor != nil && BlockedRecovery(predecessor) != nil {
				chain.Recoveries = append(chain.Recoveries, predecessorID)
			}
			if !found || len(chain.Recoveries) > len(best.Recoveries) || len(chain.Recoveries) == len(best.Recoveries) && len(chain.Path) > len(best.Path) {
				best, found = chain, true
			}
		}
		best.Path = append(best.Path, id)
		return best
	}
	return walk(task.ID)
}

// BlockedRecoveryReleased reports whether a human resolved the
// blocked_replacement_chain response reported for task's current blocked
// episode. A response without a subject, for another task or for an earlier
// episode never releases; neither does a task with no recorded episode.
func BlockedRecoveryReleased(state *State, task *Task) bool {
	blockedAt := BlockedEpisodeAt(task)
	if blockedAt.IsZero() {
		return false
	}
	for _, entry := range state.CircuitBreaker.History {
		if entry.Pattern == nil || *entry.Pattern != BlockedReplacementChainPattern || entry.ResolvedAt == nil || entry.Subject == nil {
			continue
		}
		if entry.Subject.TaskID == task.ID && entry.Subject.BlockedAt.Equal(blockedAt) {
			return true
		}
	}
	return false
}

// BlockedRecoveryCapped reports whether replacing task now would exceed the
// agent blocked-recovery cap, returning the lineage for diagnostics.
func BlockedRecoveryCapped(state *State, task *Task) (BlockedRecoveryChain, bool) {
	if task.Status != TaskStatusBlocked {
		return BlockedRecoveryChain{}, false
	}
	chain := BlockedRecoveryLineage(state, task)
	if len(chain.Recoveries) < MaxAgentBlockedRecoveries || BlockedRecoveryReleased(state, task) {
		return chain, false
	}
	return chain, true
}
