package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

type executionProgressCallbackKey struct{}

func withExecutionProgressCallback(ctx context.Context, mark func()) context.Context {
	if mark == nil {
		return ctx
	}
	return context.WithValue(ctx, executionProgressCallbackKey{}, mark)
}

func executionProgressCallback(ctx context.Context) func() {
	if mark, ok := ctx.Value(executionProgressCallbackKey{}).(func()); ok {
		return mark
	}
	return nil
}

type progressWriter struct {
	mark func()
}

func (w progressWriter) Write(p []byte) (int, error) {
	if len(p) > 0 && w.mark != nil {
		w.mark()
	}
	return len(p), nil
}

type executionProgressWatchdogResult struct {
	Blocked bool
	Reason  string
}

func startExecutionProgressWatchdog(ctx context.Context, config SupervisorConfig, taskID string, progress <-chan struct{}, cancelExec context.CancelFunc) func() executionProgressWatchdogResult {
	if taskID == "" || config.ExecutionProgressTimeout <= 0 || config.ProjectRoot == "" || config.StatePath == "" {
		return func() executionProgressWatchdogResult { return executionProgressWatchdogResult{} }
	}

	pr, err := ops.LoadResolverForModels(config.ProjectRoot)
	if err != nil {
		GetLogger().Warn("Progress watchdog disabled: failed to load pipeline resolver", "error", err, "task_id", taskID)
		return func() executionProgressWatchdogResult { return executionProgressWatchdogResult{} }
	}

	bb := db.For(config.StatePath)
	watchCtx, cancelWatchdog := context.WithCancel(ctx)
	resultCh := make(chan executionProgressWatchdogResult, 1)
	go func() {
		resultCh <- runExecutionProgressWatchdog(watchCtx, bb, config.ProjectRoot, taskID, config.AgentID, config.ExecutionProgressTimeout, pr, progress, cancelExec)
	}()

	return func() executionProgressWatchdogResult {
		cancelWatchdog()
		select {
		case result := <-resultCh:
			return result
		case <-time.After(2 * time.Second):
			GetLogger().Warn("Progress watchdog did not stop promptly", "task_id", taskID, "agent_id", config.AgentID)
			return executionProgressWatchdogResult{}
		}
	}
}

func runExecutionProgressWatchdog(
	ctx context.Context,
	bb *db.Blackboard,
	projectRoot string,
	taskID string,
	agentID string,
	timeout time.Duration,
	pr models.PipelineResolver,
	progress <-chan struct{},
	cancelExec context.CancelFunc,
) executionProgressWatchdogResult {
	lastSignature, eligible, err := readExecutionProgressSnapshot(projectRoot, bb, taskID, agentID, pr)
	if err != nil {
		GetLogger().Warn("Progress watchdog disabled: failed to read initial progress snapshot", "error", err, "task_id", taskID)
		return executionProgressWatchdogResult{}
	}
	if !eligible {
		return executionProgressWatchdogResult{}
	}

	lastProgress := time.Now()
	ticker := time.NewTicker(progressPollInterval(timeout))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return executionProgressWatchdogResult{}
		case <-progress:
			lastProgress = time.Now()
		case <-ticker.C:
			signature, stillEligible, snapErr := readExecutionProgressSnapshot(projectRoot, bb, taskID, agentID, pr)
			if snapErr != nil {
				GetLogger().Warn("Progress watchdog snapshot failed", "error", snapErr, "task_id", taskID)
			} else if !stillEligible {
				return executionProgressWatchdogResult{}
			} else if signature != lastSignature {
				lastSignature = signature
				lastProgress = time.Now()
				continue
			}

			if time.Since(lastProgress) < timeout {
				continue
			}

			reason := fmt.Sprintf("agent execution progress timeout: task %s had no observable state, worktree, or provider-output progress for %s", taskID, timeout)
			GetLogger().Error("Execution progress timeout, blocking task", "task_id", taskID, "agent_id", agentID, "timeout", timeout)
			if alertErr := LogAlert(projectRoot, "🚨", "EXECUTION PROGRESS TIMEOUT", reason); alertErr != nil {
				GetLogger().Warn("Failed to write execution progress alert", "error", alertErr)
			}
			cancelExec()
			return executionProgressWatchdogResult{Blocked: true, Reason: reason}
		}
	}
}

func readExecutionProgressSnapshot(projectRoot string, bb *db.Blackboard, taskID string, agentID string, pr models.PipelineResolver) (string, bool, error) {
	state, err := bb.Read()
	if err != nil {
		return "", false, err
	}
	task := state.FindTask(taskID)
	if task == nil {
		return "", false, nil
	}
	if task.AssignedTo == nil || *task.AssignedTo != agentID || !models.IsExecutingStatus(task, pr) {
		return "", false, nil
	}

	worktreeSignature := "none"
	if task.Worktree != nil && *task.Worktree != "" {
		sig, sigErr := lizagit.New(projectRoot).WorktreeProgressSignature(taskID)
		if sigErr != nil {
			worktreeSignature = "error:" + sigErr.Error()
		} else {
			worktreeSignature = sig
		}
	}

	return exit42TaskProgressSignature(task) + "\nworktree:" + worktreeSignature, true, nil
}

func readTaskStateWorktreeProgressSnapshot(projectRoot string, bb *db.Blackboard, taskID string) (string, bool, error) {
	state, err := bb.Read()
	if err != nil {
		return "", false, err
	}
	task := state.FindTask(taskID)
	if task == nil {
		return "", false, nil
	}

	worktreeSignature := "none"
	if task.Worktree != nil && *task.Worktree != "" {
		sig, sigErr := lizagit.New(projectRoot).WorktreeProgressSignature(taskID)
		if sigErr != nil {
			worktreeSignature = "error:" + sigErr.Error()
		} else {
			worktreeSignature = sig
		}
	}

	return exit42TaskProgressSignature(task) + "\nworktree:" + worktreeSignature, true, nil
}

func progressPollInterval(timeout time.Duration) time.Duration {
	interval := timeout / 4
	if interval < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	if interval > 15*time.Second {
		return 15 * time.Second
	}
	return interval
}
