package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// Claim loss is an expected coordination outcome, not a provider crash.
var errReviewOwnershipLost = errors.New("review ownership lost")

// reviewExecution binds a provider turn to state ownership, never to an
// observer's namespace-relative PID classification.
type reviewExecution struct {
	authority       models.AgentAuthority
	taskID          string
	role            string
	resolver        models.PipelineResolver
	startedAt       time.Time
	historyBoundary int
}

func newReviewExecution(state *models.State, config SupervisorConfig, taskID string) (*reviewExecution, error) {
	if taskID == "" {
		return nil, nil
	}
	pr, err := ops.LoadResolverForModels(config.ProjectRoot)
	if err != nil {
		return nil, err
	}
	a := state.Agents[config.AgentID]
	role := config.Role
	if role == "" {
		role = a.Role
	}
	roleType, err := pr.RoleType(role)
	if err != nil {
		return nil, err
	}
	if roleType != "reviewer" {
		return nil, nil
	}
	guard := &reviewExecution{
		authority: config.Authority, taskID: taskID, role: role,
		resolver: pr, startedAt: time.Now().UTC(),
	}
	task := state.FindTask(taskID)
	if err := ops.RequireAgentAuthority(state, config.Authority); err != nil {
		return nil, err
	}
	if task == nil || !guard.registrationCurrent(a, guard.startedAt) || !guard.ownsReview(task, a, guard.startedAt, true) {
		return nil, fmt.Errorf("%w before provider start for task %s", errReviewOwnershipLost, taskID)
	}
	guard.historyBoundary = len(task.History)
	return guard, nil
}

func (g *reviewExecution) registrationCurrent(a models.Agent, now time.Time) bool {
	return a.Role == g.role && !a.Heartbeat.IsZero() && a.LeaseExpires != nil && a.LeaseExpires.After(now)
}

func (g *reviewExecution) ownsReview(task *models.Task, a models.Agent, now time.Time, activeOnly bool) bool {
	role, err := g.resolver.ReviewerRole(task.RolePair)
	if err != nil || role != g.role || task.ReviewingBy == nil || *task.ReviewingBy != g.authority.ID ||
		a.CurrentTask == nil || *a.CurrentTask != task.ID || task.ReviewLeaseExpires == nil || !task.ReviewLeaseExpires.After(now) {
		return false
	}
	active, _, err := ops.ResolveReviewerReleaseStatus(task, g.resolver)
	if err == nil && active != "" && a.Status == models.AgentStatusReviewing {
		return true
	}
	if activeOnly || a.Status != models.AgentStatusWaiting {
		return false
	}
	rejected, err := g.resolver.RejectedStatus(task.RolePair)
	return (err == nil && task.Status == rejected) || models.IsSubmittedStatus(task, g.resolver) || models.IsExecutingStatus(task, g.resolver)
}

func (g *reviewExecution) current(state *models.State, now time.Time) bool {
	if ops.RequireAgentAuthority(state, g.authority) != nil {
		return false
	}
	a := state.Agents[g.authority.ID]
	task := state.FindTask(g.taskID)
	if task == nil || !g.registrationCurrent(a, now) {
		return false
	}
	if g.ownsReview(task, a, now, true) {
		// History is append-only. Reacquiring active review retires any verdict
		// from the previous review, even within the same provider turn.
		g.historyBoundary = len(task.History)
		return true
	}
	if g.ownsReview(task, a, now, false) {
		return true
	}
	if task.ReviewingBy != nil || a.CurrentTask != nil || (a.Status != models.AgentStatusIdle && a.Status != models.AgentStatusWaiting) {
		return false
	}
	// A successful verdict releases ownership before the provider finishes its
	// reply or enters await-resubmission. Only this turn's own verdict permits it.
	ownVerdict := false
	for i := g.historyBoundary; i < len(task.History); i++ {
		event := task.History[i]
		if event.Time.Before(g.startedAt) || event.Agent == nil {
			continue
		}
		if event.Event == models.TaskEventClaimed {
			roleType, _ := g.resolver.RoleType(state.Agents[*event.Agent].Role)
			if *event.Agent == g.authority.ID || roleType == "reviewer" {
				ownVerdict = false
			}
		}
		if event.Event == models.TaskEventApproved || event.Event == models.TaskEventRejected {
			ownVerdict = *event.Agent == g.authority.ID
		}
	}
	return ownVerdict
}

func startReviewExecutionWatchdog(ctx context.Context, config SupervisorConfig, taskID string, interval time.Duration, cancelExec context.CancelFunc) (func() bool, error) {
	if taskID == "" {
		return func() bool { return false }, nil
	}
	bb := db.For(config.StatePath)
	state, err := bb.ReadContext(ctx)
	if err != nil {
		return nil, err
	}
	guard, err := newReviewExecution(state, config, taskID)
	if err != nil {
		return nil, err
	}
	if guard == nil {
		return func() bool { return false }, nil
	}
	watchCtx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	lost := false // Read only after joining the observer.
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				current, err := bb.ReadContext(watchCtx)
				if err != nil {
					if watchCtx.Err() == nil {
						GetLogger().Warn("Review ownership observation failed; will retry", "task_id", taskID, "error", err)
					}
					continue
				}
				if !guard.current(current, time.Now().UTC()) {
					lost = true
					GetLogger().Warn("Review ownership lost; cancelling provider turn", "task_id", taskID, "agent_id", config.AgentID)
					cancelExec()
					return
				}
			}
		}
	}()
	return sync.OnceValue(func() bool { stop(); <-done; return lost }), nil
}
