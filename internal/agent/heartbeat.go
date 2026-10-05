package agent

import (
	"context"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/db"
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

	mu   sync.Mutex
	last db.LivenessRecord // last published record
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
				if ops.IsAgentAuthorityError(err) {
					return err
				}
				// Non-fatal: supervisors detect stale agents via watch command
				logger.Error("Heartbeat update failed", "error", err, "agent_id", h.authority.ID)
			}
		}
	}
}

// beatBeforePublishHook runs between the authority check and the record
// publish, so tests can interleave a re-registration there.
var beatBeforePublishHook func()

// beat publishes this generation's liveness record instead of rewriting the
// state under its lock (ADR-0177). Reads apply the record's agent and task
// lease renewals; the next mutation folds it into state.yaml.
func (h *Heartbeat) beat() error {
	state, err := h.bb.ReadSnapshot()
	if err != nil {
		return err
	}
	if err := ops.CheckAgentAuthoritySnapshot(state, h.authority); err != nil {
		return err
	}
	if beatBeforePublishHook != nil {
		beatBeforePublishHook()
	}

	now := time.Now().UTC()
	lease := now.Add(h.leaseDuration)

	h.mu.Lock()
	defer h.mu.Unlock()
	// Seq orders this generation's records against the seq a mutation folded;
	// it stays monotonic when the wall clock steps back.
	seq := max(now.UnixNano(), h.last.Seq+1)
	record := db.LivenessRecord{
		AgentID:      h.authority.ID,
		Generation:   h.authority.Generation,
		Seq:          seq,
		Heartbeat:    now,
		LeaseExpires: lease,
		// Carry the latest review renewal: this record replaces the previous
		// one, which may not be folded yet.
		ReviewTask:         h.last.ReviewTask,
		ReviewLeaseExpires: h.last.ReviewLeaseExpires,
		ReviewSeq:          h.last.ReviewSeq,
	}
	if h.activeProviderTask != nil {
		if task := h.activeProviderTask(); task != "" {
			record.ReviewTask, record.ReviewLeaseExpires, record.ReviewSeq = task, lease, seq
		}
	}
	if err := db.WriteLivenessRecord(h.bb.GetStatePath(), record); err != nil {
		return err
	}
	h.last = record
	return nil
}
