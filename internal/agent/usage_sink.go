package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/usage"
)

// supervisorRunIDBytes is the size of a supervisor run identity: 128 bits.
const supervisorRunIDBytes = 16

// newSupervisorRunID mints one opaque 128-bit identity. Each supervisor process
// mints exactly one, so a restart or a handoff is observable in usage records on
// every provider path — including the default CLI one, where session_id is a
// copy of the task id. It is not a registration generation and confers no
// authority (specs/protocols/lifecycle-results.md, Result contract).
func newSupervisorRunID() string {
	var buf [supervisorRunIDBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// A record must never carry an empty identity. Process id plus start
		// time still separates two supervisors on one host.
		return fmt.Sprintf("%08x-%016x", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

var (
	supervisorRunIDOnce  sync.Once
	supervisorRunIDValue string
)

// supervisorRunID is the process-scoped — therefore supervisor-session-scoped —
// run identity every record written by this process carries.
func supervisorRunID() string {
	supervisorRunIDOnce.Do(func() { supervisorRunIDValue = newSupervisorRunID() })
	return supervisorRunIDValue
}

// UsageSinkConfig is the supervisor identity a usage record is attributed to.
// An empty SupervisorRunID falls back to this process's run identity; a caller
// that supplies one can reproduce a handoff or a supervisor restart.
type UsageSinkConfig struct {
	ProjectRoot     string
	AgentID         string
	Role            string
	Provider        string
	SessionID       string
	TaskID          string
	SupervisorRunID string
}

// NewUsageEventSink returns the supervisor event sink for one provider turn. It
// keeps the existing log lines and writes exactly one durable usage record when
// the turn completes. The record is telemetry: a failing append is logged and
// never changes the run's result.
func NewUsageEventSink(cfg UsageSinkConfig) LLMAgentEventSink {
	if cfg.SupervisorRunID == "" {
		cfg.SupervisorRunID = supervisorRunID()
	}
	return &supervisorLLMAgentEventSink{cfg: cfg}
}

type supervisorLLMAgentEventSink struct {
	cfg UsageSinkConfig

	mu        sync.Mutex
	startedAt time.Time
	reported  LLMAgentUsage
	sawUsage  bool
}

func (s *supervisorLLMAgentEventSink) RecordLLMAgentEvent(_ context.Context, event LLMAgentEvent) {
	switch event.Kind {
	case LLMAgentEventStarted:
		logLLMAgentLifecycleEvent(event)
		s.mu.Lock()
		s.startedAt = eventTime(event)
		s.mu.Unlock()
	case LLMAgentEventUsage:
		reported, _ := event.Payload["usage"].(LLMAgentUsage)
		GetLogger().Info("LLM agent usage",
			"backend", event.BackendName,
			"agent_id", event.AgentID,
			"task_id", event.TaskID,
			"session_id", event.SessionID,
			"input_tokens", reported.InputTokens,
			"output_tokens", reported.OutputTokens,
			"cached_read_tokens", reported.CachedReadTokens,
			"cached_write_tokens", reported.CachedWriteTokens)
		s.mu.Lock()
		s.reported, s.sawUsage = reported, true
		s.mu.Unlock()
	case LLMAgentEventCompleted:
		logLLMAgentLifecycleEvent(event)
		s.appendRecord(event)
	default:
		// Provider content chunks stay in the normal output stream/log files. The
		// supervisor metadata sink intentionally avoids duplicating content events.
	}
}

func logLLMAgentLifecycleEvent(event LLMAgentEvent) {
	GetLogger().Info("LLM agent event",
		"kind", string(event.Kind),
		"backend", event.BackendName,
		"agent_id", event.AgentID,
		"task_id", event.TaskID,
		"session_id", event.SessionID)
}

// appendRecord writes this turn's single record. A completed event without a
// preceding started one — the launch-gate failure path — records unknown
// provenance over a zero-length interval rather than no record at all.
func (s *supervisorLLMAgentEventSink) appendRecord(event LLMAgentEvent) {
	endedAt := eventTime(event)
	s.mu.Lock()
	startedAt, reported, sawUsage := s.startedAt, s.reported, s.sawUsage
	s.mu.Unlock()
	if startedAt.IsZero() {
		startedAt = endedAt
	}
	// The provider reports its own session; cfg.SessionID is the caller's
	// fallback, which on the default CLI path is a copy of the task id.
	sessionID := s.cfg.SessionID
	if event.SessionID != "" {
		sessionID = event.SessionID
	}
	record := usage.Record{
		TaskID:          s.cfg.TaskID,
		Role:            s.cfg.Role,
		AgentID:         s.cfg.AgentID,
		SupervisorRunID: s.cfg.SupervisorRunID,
		SessionID:       sessionID,
		Provider:        s.cfg.Provider,
		StartedAt:       startedAt,
		EndedAt:         endedAt,
		// WarmSession stays false: no provider event carries warm-session reuse,
		// and this output adds no LLMAgentEvent field.
		FreshInputTokens: reported.InputTokens,
		CacheReadTokens:  reported.CachedReadTokens,
		CacheWriteTokens: reported.CachedWriteTokens,
		OutputTokens:     reported.OutputTokens,
		Provenance:       classifyUsageProvenance(reported, sawUsage),
		ExitCode:         completedExitCode(event),
	}
	if err := usage.Append(s.cfg.ProjectRoot, record); err != nil {
		GetLogger().Warn("Failed to append usage record",
			"error", err,
			"agent_id", s.cfg.AgentID,
			"task_id", s.cfg.TaskID,
			"provider", s.cfg.Provider)
	}
}

// classifyUsageProvenance pins the record's quality at write time, so a later
// before/after comparison is not silently re-based when log volume changes.
func classifyUsageProvenance(reported LLMAgentUsage, sawUsage bool) usage.Provenance {
	switch {
	case !sawUsage:
		return usage.ProvenanceUnknown
	case reported.InputTokens > 0 || reported.OutputTokens > 0:
		return usage.ProvenanceTerminalAuthoritative
	case reported.CachedReadTokens > 0 || reported.CachedWriteTokens > 0:
		// Some fields arrived; the fresh-input and output counts did not.
		return usage.ProvenancePartial
	default:
		// The provider reports no usage at all, as the CLI transport does.
		return usage.ProvenanceUnknown
	}
}

func completedExitCode(event LLMAgentEvent) int {
	if code, ok := event.Payload["exit_code"].(int); ok {
		return code
	}
	if _, failed := event.Payload["error"]; failed {
		return 1
	}
	return 0
}

func eventTime(event LLMAgentEvent) time.Time {
	if event.Time.IsZero() {
		return time.Now().UTC()
	}
	return event.Time.UTC()
}
