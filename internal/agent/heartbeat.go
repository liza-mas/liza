package agent

import (
	"context"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

const (
	// DefaultHeartbeatInterval derives from models to maintain a single source of truth.
	DefaultHeartbeatInterval = time.Duration(models.DefaultHeartbeatIntervalSec) * time.Second
	DefaultLeaseDuration     = time.Duration(models.DefaultLeaseDurationSeconds) * time.Second
)

type HeartbeatConfig struct {
	AgentID            string
	Authority          models.AgentAuthority
	StatePath          string
	Interval           time.Duration
	LeaseDuration      time.Duration
	State              *models.State // Optional: if provided, interval is read from state.Config.HeartbeatInterval
	ActiveProviderTask func() string // Review ownership is renewed only during this task's provider session.
}

type Heartbeat struct {
	authority          models.AgentAuthority
	bb                 *db.Blackboard
	interval           time.Duration
	leaseDuration      time.Duration
	activeProviderTask func() string
}

// providerSessionActivity belongs to one supervisor, outside persisted agent
// metadata: failed status cleanup must never impersonate a running session.
type providerSessionActivity struct {
	mu     sync.RWMutex
	taskID string
}

func (a *providerSessionActivity) start(taskID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.taskID = taskID
	a.mu.Unlock()
}

func (a *providerSessionActivity) stop() {
	if a == nil {
		return
	}
	a.start("")
}

func (a *providerSessionActivity) currentTask() string {
	if a == nil {
		return ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.taskID
}

func NewHeartbeat(config HeartbeatConfig) *Heartbeat {
	interval := config.Interval

	if config.State != nil {
		interval = models.NormalizeHeartbeatInterval(config.State.Config.HeartbeatInterval)
	}

	if interval == 0 {
		interval = DefaultHeartbeatInterval
	}

	leaseDuration := config.LeaseDuration
	if leaseDuration == 0 {
		leaseDuration = DefaultLeaseDuration
	}

	authority := config.Authority
	if authority.ID == "" {
		authority.ID = config.AgentID
	}

	return &Heartbeat{
		authority:          authority,
		bb:                 db.For(config.StatePath),
		interval:           interval,
		leaseDuration:      leaseDuration,
		activeProviderTask: config.ActiveProviderTask,
	}
}

func startSupervisorHeartbeat(
	ctx context.Context,
	start func(context.Context) error,
	onError func(error),
) func() {
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		if err := start(heartbeatCtx); err != nil && err != context.Canceled {
			onError(err)
		}
	}()

	return func() {
		cancelHeartbeat()
		<-heartbeatDone
	}
}

func (h *Heartbeat) Start(ctx context.Context) error {
	logger := GetLogger()
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := h.beat(); err != nil {
				if errors.IsNotFound(err) || ops.IsAgentAuthorityError(err) {
					return err
				}
				// Non-fatal: supervisors detect stale agents via watch command
				logger.Error("Heartbeat update failed", "error", err, "agent_id", h.authority.ID)
			}
		}
	}
}

func (h *Heartbeat) beat() error {
	now := time.Now().UTC()
	newLease := now.Add(h.leaseDuration)

	return ops.ModifyWithAgentAuthority(h.bb, h.authority, func(state *models.State) error {
		agentID := h.authority.ID
		agent, exists := state.Agents[agentID]
		if !exists {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		agent.Heartbeat = now
		agent.LeaseExpires = &newLease
		state.Agents[agentID] = agent

		// Renew task lease if agent is actively assigned
		if agent.CurrentTask != nil {
			if task := state.FindTask(*agent.CurrentTask); task != nil {
				if task.AssignedTo != nil && *task.AssignedTo == agentID && task.LeaseExpires != nil {
					task.LeaseExpires = &newLease
				}
				if task.ReviewingBy != nil && *task.ReviewingBy == agentID && task.ReviewLeaseExpires != nil &&
					h.activeProviderTask != nil && h.activeProviderTask() == task.ID {
					task.ReviewLeaseExpires = &newLease
				}
			}
		}

		return nil
	})
}
