package ops

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const releaseBlockedChainOperation = "release-blocked-chain"

// ReleaseBlockedChainResult reports the blocked episode an operator released.
type ReleaseBlockedChainResult struct {
	TaskID     string    `json:"task_id"`
	BlockedAt  time.Time `json:"blocked_at"`
	Lineage    []string  `json:"lineage"`
	Recoveries []string  `json:"recoveries"`
	Warnings   []string  `json:"warnings,omitempty"`
}

// ReleaseBlockedChain releases the blocked-recovery cap for a capped BLOCKED
// task's current episode without tripping the circuit breaker (D-146,
// ADR-0208). It records blocked_recovery_released in the task's history, which
// BlockedRecoveryCapped honors until the episode ends; it changes no status,
// mode or circuit-breaker state. Only a task at the cap with a recorded,
// unreleased episode can be released. The CLI owns the operator boundary.
func ReleaseBlockedChain(projectRoot, taskID, reason string) (*ReleaseBlockedChainResult, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, &PreconditionError{Reason: "task ID is required"}
	}
	reason = secretmask.New().MaskText(strings.TrimSpace(reason))
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	now := time.Now().UTC()
	var result *ReleaseBlockedChainResult
	err := bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		if task.Status != models.TaskStatusBlocked {
			return &PreconditionError{Reason: fmt.Sprintf("task %s is %s, not BLOCKED; %s releases only a BLOCKED task's current episode", taskID, task.Status, releaseBlockedChainOperation)}
		}
		blockedAt := models.BlockedEpisodeAt(task)
		if blockedAt.IsZero() {
			return &PreconditionError{Reason: fmt.Sprintf("task %s has no recorded blocked episode (no status-transition event in its history); nothing can be released", taskID)}
		}
		chain, capped := models.BlockedRecoveryCapped(state, task)
		if !capped {
			if models.BlockedRecoveryReleased(state, task) {
				return &PreconditionError{Reason: fmt.Sprintf("task %s's current blocked episode (%s) is already released", taskID, blockedAt.Format(time.RFC3339Nano))}
			}
			return &PreconditionError{Reason: fmt.Sprintf("task %s is not at the blocked-recovery cap (%d of %d blocked recoveries in lineage %s); agents can already replace it",
				taskID, len(chain.Recoveries), models.MaxAgentBlockedRecoveries, strings.Join(chain.Path, " -> "))}
		}

		note := "operator released one further blocked-recovery replacement for this blocked episode"
		task.History = append(task.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventBlockedRecoveryReleased,
			Reason: &reason,
			Note:   &note,
			Extra: map[string]any{
				"operation":  releaseBlockedChainOperation,
				"source":     "operator_cli",
				"blocked_at": blockedAt.UTC().Format(time.RFC3339Nano),
				"lineage":    slices.Clone(chain.Path),
				"recoveries": slices.Clone(chain.Recoveries),
			},
		})
		result = &ReleaseBlockedChainResult{TaskID: taskID, BlockedAt: blockedAt, Lineage: chain.Path, Recoveries: chain.Recoveries}
		return statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr)
	})
	if err != nil {
		return nil, fmt.Errorf("release blocked chain: %w", err)
	}

	if err := log.New(lp.LogPath()).Append(log.Entry{
		Timestamp: now,
		Agent:     "operator",
		Action:    string(models.TaskEventBlockedRecoveryReleased),
		Task:      &taskID,
		Detail:    fmt.Sprintf("episode %s, lineage %s: %s", result.BlockedAt.UTC().Format(time.RFC3339Nano), strings.Join(result.Lineage, " -> "), reason),
	}); err != nil {
		result.Warnings = append(result.Warnings, "release persisted; activity log write failed. Do not retry the release.")
	}
	return result, nil
}
