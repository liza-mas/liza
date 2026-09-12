package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
)

// doerStrategy handles task-implementing roles: coder, code-planner, epic-planner, us-writer.
type doerStrategy struct {
	role             string             // role name in hyphenated form (e.g. "coder")
	resolver         *pipeline.Resolver // pipeline resolver for context sections
	executionTimeout time.Duration      // from YAML; 0 = use type default
	yamlPollSec      int                // from YAML; 0 = use type default
	yamlMaxWaitSec   int                // from YAML; 0 = use type default
}

const defaultDoerTimeout = 2 * time.Hour

func (s *doerStrategy) DefaultTimeout() time.Duration {
	if s.executionTimeout > 0 {
		return s.executionTimeout
	}
	return defaultDoerTimeout
}

func (s *doerStrategy) WaitConfig(state *models.State) (pollInterval, maxWait time.Duration) {
	poll := nonZeroOr(state.Config.CoderPollInterval, nonZeroOr(s.yamlPollSec, models.DefaultCoderPollInterval))
	max := nonZeroOr(
		state.Config.DoerMaxWait,
		nonZeroOr(state.Config.DeprecatedCoderMaxWait, nonZeroOr(s.yamlMaxWaitSec, models.DefaultDoerMaxWait)),
	)
	return time.Duration(poll) * time.Second, time.Duration(max) * time.Second
}

// ApplyYAMLTimeouts sets YAML-sourced timeout overrides on a strategy.
// Used by RunSupervisor to inject role-specific timeouts from pipeline config.
func ApplyYAMLTimeouts(s RoleStrategy, execution, pollInterval, maxWait time.Duration) {
	execDur := execution
	pollSec := int(pollInterval.Seconds())
	maxWaitSec := int(maxWait.Seconds())

	switch v := s.(type) {
	case *doerStrategy:
		v.executionTimeout = execDur
		v.yamlPollSec = pollSec
		v.yamlMaxWaitSec = maxWaitSec
	case *reviewerStrategy:
		v.executionTimeout = execDur
		v.yamlPollSec = pollSec
		v.yamlMaxWaitSec = maxWaitSec
	case *orchestratorStrategy:
		v.executionTimeout = execDur
		v.yamlPollSec = pollSec
		v.yamlMaxWaitSec = maxWaitSec
	}
}

func (s *doerStrategy) PreWork(_ context.Context, _ *db.Blackboard, _ SupervisorConfig) (bool, error) {
	return false, nil
}

func (s *doerStrategy) WaitForWork(ctx context.Context, bb *db.Blackboard, config SupervisorConfig, pollInterval, maxWait time.Duration) (bool, error) {
	pr := loadResolver(config.ProjectRoot)
	return waitForWorkEventDriven(ctx, bb, config.ProjectRoot, pollInterval, maxWait,
		func(state *models.State) (bool, string) {
			claimable := models.CountDoerClaimableTasksForAgent(state, s.role, config.AgentID, pr)
			resumableHandoffs := countResumableHandoffTasks(state, config.AgentID, pr)
			resumableOwned := ops.CountResumableOwnedTasks(state, config.AgentID, pr)

			logMsg := fmt.Sprintf("%s: %d claimable, %d resumable handoffs, %d resumable owned", s.role, claimable, resumableHandoffs, resumableOwned)

			// Use richer diagnostics for coder role
			if s.role == "coder" {
				logMsg = models.GetCoderWorkDiagnosticsForAgent(state, config.AgentID, pr)
				if resumableHandoffs > 0 {
					handoffMsg := fmt.Sprintf("Found %d resumable handoff task(s) for %s", resumableHandoffs, config.AgentID)
					if logMsg != "" {
						logMsg = handoffMsg + "; " + logMsg
					} else {
						logMsg = handoffMsg
					}
				}
			}
			if resumableOwned > 0 {
				ownedMsg := fmt.Sprintf("Found %d resumable owned task(s) for %s", resumableOwned, config.AgentID)
				if logMsg != "" {
					logMsg = ownedMsg + "; " + logMsg
				} else {
					logMsg = ownedMsg
				}
			}

			return claimable > 0 || resumableHandoffs > 0 || resumableOwned > 0, logMsg
		})
}

func (s *doerStrategy) ClaimTask(config SupervisorConfig, bb *db.Blackboard) (string, string, error) {
	session, err := prepareClaimSession(config, bb)
	if err != nil {
		return "", "", err
	}
	taskID, _, err := claimDoerTaskWithAuthority(config.ProjectRoot, config.Authority, s.role, bb, session)
	if err != nil {
		return "", "", err
	}
	return taskID, taskID, nil
}

func (s *doerStrategy) PreExecution(_ *db.Blackboard, _ SupervisorConfig) error {
	return nil
}

func (s *doerStrategy) BuildPrompt(state *models.State, config SupervisorConfig, taskID string) (string, error) {
	return buildPromptWithContext(state, config, taskID, s.resolver)
}

func (s *doerStrategy) PostExecution(bb *db.Blackboard, config SupervisorConfig, _ string, claimedTaskID string, _ *models.State) error {
	if claimedTaskID == "" {
		return nil
	}

	pr, err := ops.LoadResolverForModels(config.ProjectRoot)
	if err != nil {
		GetLogger().Warn("Failed to load pipeline resolver — skipping submission log", "error", err)
		return nil
	}

	if err := logTaskSubmissionIfCompleted(bb, claimedTaskID, config.AgentID, pr); err != nil {
		GetLogger().Warn("Failed to log task submission", "error", err, "task_id", claimedTaskID)
	}
	return nil
}
