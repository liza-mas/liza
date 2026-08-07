package acp

import (
	"fmt"
	"sync"
	"time"
)

// nowFunc is a seam for measuring elapsed time. Windows advances its clock in
// steps of up to ~15ms, wall and monotonic alike, so a run that completes in
// microseconds genuinely measures as zero there and a test cannot observe the
// measurement without owning the clock.
var nowFunc = time.Now

type ActiveRun struct {
	TaskID    string
	SessionID string
	Warm      bool
	StartedAt time.Time
	Usage     Usage
	Output    string
}

type runState struct {
	ActiveRun
	startedAt time.Time
}

// RunManager tracks ACP task runs and turns event streams into benchmark metrics.
type RunManager struct {
	mu       sync.Mutex
	sessions *SessionManager
	active   map[string]*runState
	runs     *BenchmarkAccumulator
}

// NewRunManager returns an isolated ACP run manager.
func NewRunManager() *RunManager {
	return &RunManager{
		sessions: NewSessionManager(),
		active:   make(map[string]*runState),
		runs:     NewBenchmarkAccumulator(),
	}
}

// Start opens a new task run and returns its active run metadata.
func (m *RunManager) Start(taskID string) ActiveRun {
	return m.StartWithSessionKey(taskID, taskID)
}

// StartWithSessionKey opens a new task run using a caller-defined session key.
// This supports ACP-style project/agent sessions where multiple task deltas
// share one warm protocol session while keeping per-task metrics distinct.
func (m *RunManager) StartWithSessionKey(taskID string, sessionKey string) ActiveRun {
	sessionID, warm := m.sessions.Start(sessionKey)

	now := nowFunc()
	state := &runState{
		ActiveRun: ActiveRun{
			TaskID:    taskID,
			SessionID: sessionID,
			Warm:      warm,
			StartedAt: now.UTC(),
		},
		startedAt: now,
	}

	m.mu.Lock()
	m.active[taskID] = state
	m.mu.Unlock()

	return state.ActiveRun
}

// HandleEvent records any metrics from an event stream event.
// Unknown events are ignored if a run exists.
func (m *RunManager) HandleEvent(event Event) error {
	if event.TaskID == "" {
		return fmt.Errorf("missing task id")
	}

	m.mu.Lock()
	state := m.active[event.TaskID]
	m.mu.Unlock()
	if state == nil {
		return fmt.Errorf("no active run for task %q", event.TaskID)
	}

	if usage, ok := ParseUsageFromEvent(event); ok {
		m.mu.Lock()
		state.Usage = state.Usage.add(usage)
		m.mu.Unlock()
		return nil
	}

	if event.Message != "" && isContentKind(event.Kind) {
		m.mu.Lock()
		if state.Output != "" {
			state.Output = state.Output + "\n"
		}
		state.Output += event.Message
		m.mu.Unlock()
	}

	return nil
}

// Finish finalizes the active run, persists its metric, and returns it.
func (m *RunManager) Finish(taskID string, exitCode int) (RunMetric, error) {
	m.mu.Lock()
	state := m.active[taskID]
	if state == nil {
		m.mu.Unlock()
		return RunMetric{}, fmt.Errorf("no active run for task %q", taskID)
	}
	delete(m.active, taskID)
	m.mu.Unlock()

	metric := RunMetric{
		TaskID:    state.TaskID,
		SessionID: state.SessionID,
		Warm:      state.Warm,
		ExitCode:  exitCode,
		Duration:  nowFunc().Sub(state.startedAt),
		Output:    state.Output,
		Usage:     state.Usage,
	}
	m.runs.Record(metric)
	return metric, nil
}

// Runs returns completed run metrics.
func (m *RunManager) Runs() []RunMetric {
	return m.runs.Runs()
}

// Summary returns the current aggregate benchmark summary.
func (m *RunManager) Summary() (BenchmarkSummary, error) {
	return m.runs.Summarize()
}

func isContentKind(kind EventKind) bool {
	switch kind {
	case EventMessage, EventThought, EventTrajectory, EventToolCall, EventToolUpdate, EventOutputChunk:
		return true
	default:
		return false
	}
}
