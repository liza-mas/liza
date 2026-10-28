package agent

import (
	"context"
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/sessionvalidation"
)

// errLaunchHalted reports a provider start refused because the system entered
// a halt mode after the claim. No provider ran; the supervisor releases the
// claim without charging any budget and parks at the pause gate.
var errLaunchHalted = errors.New("provider start refused: system halted")

var errLaunchContended = errors.New("provider start delayed: lock contention")

func newProviderLaunchGate(config SupervisorConfig) LLMAgentLaunchGate {
	return newTaskProviderLaunchGate(config, "", nil)
}

func newTaskProviderLaunchGate(config SupervisorConfig, taskID string, validation *ops.ValidationPreflight) LLMAgentLaunchGate {
	return func(ctx context.Context, start func() error) error {
		return ops.WithAgentLifecycleLock(ctx, config.ProjectRoot, config.Authority.ID, "provider-start", func() error {
			// Held shared across the mode check and start, so a halt's barrier
			// orders every start before or after it (ADR-0173).
			return ops.WithWorkAdmissionSharedLock(ctx, config.ProjectRoot, "provider-start", func() error {
				// Registration and recover-agent take this agent's lifecycle lock, so
				// the snapshot cannot miss a generation change for it.
				state, err := readStateSnapshot(ctx, db.For(config.StatePath))
				if err != nil {
					return fmt.Errorf("read current agent authority before provider start: %w", err)
				}
				if err := ops.RequireAgentAuthority(state, config.Authority); err != nil {
					return err
				}
				if _, err := newReviewExecution(state, config, taskID); err != nil {
					return err
				}
				// A legacy task can acquire prerequisites after preparation. Such a
				// change needs a fresh observation before any provider process starts.
				if task := state.FindTask(taskID); task != nil && len(task.ValidationPrerequisites) > 0 && validation == nil {
					return &sessionvalidation.Error{Code: "context_changed", CommandIndex: -1, CheckIndex: -1}
				}
				if err := validation.CheckLaunchCurrent(state); err != nil {
					return err
				}
				if err := ops.RequireWorkAdmitted(state, "provider-start"); err != nil {
					return err
				}
				return start()
			})
		})
	}
}
