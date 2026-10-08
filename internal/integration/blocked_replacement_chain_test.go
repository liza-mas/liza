package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// blockedChainFixture drives replacement lineages through the real
// supersede-task, analyze and resume operations (ADR-0171).
type blockedChainFixture struct {
	t         *testing.T
	root      string
	stateFile string
	clock     time.Time
}

func newBlockedChainFixture(t *testing.T) *blockedChainFixture {
	t.Helper()
	root := t.TempDir()
	stateFile, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = nil
	testhelpers.WriteInitialState(t, stateFile, state)
	return &blockedChainFixture{t: t, root: root, stateFile: stateFile, clock: time.Now().UTC().Add(-time.Hour)}
}

func (f *blockedChainFixture) modify(fn func(*models.State)) {
	f.t.Helper()
	if err := db.New(f.stateFile).Modify(func(s *models.State) error { fn(s); return nil }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *blockedChainFixture) read() *models.State {
	f.t.Helper()
	state, err := db.New(f.stateFile).Read()
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}

func (f *blockedChainFixture) addReady(id string) {
	f.modify(func(s *models.State) {
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, f.clock))
	})
}

// block moves an existing task (or a new one) into a fresh blocked episode.
func (f *blockedChainFixture) block(id string) {
	f.t.Helper()
	f.clock = f.clock.Add(time.Minute)
	f.modify(func(s *models.State) {
		blocked := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, f.clock)
		reason := id + " blocked on the shared interface"
		blocked.BlockedReason = &reason
		existing := s.FindTask(id)
		if existing != nil {
			blocked.History = existing.History
			*existing = blocked
		} else {
			s.Tasks = append(s.Tasks, blocked)
			existing = &s.Tasks[len(s.Tasks)-1]
		}
		existing.History = append(existing.History, models.TaskHistoryEntry{Time: f.clock, Event: models.TaskEventBlocked})
	})
}

func (f *blockedChainFixture) supersede(id, replacement string) error {
	f.t.Helper()
	f.addReady(replacement)
	_, err := ops.SupersedeTaskWithOptions(f.root, id, []string{replacement}, "replace blocked task", "orchestrator-1",
		ops.SupersedeTaskOptions{Changed: "new approach for " + replacement})
	return err
}

// cappedChain builds prefix-a -> prefix-b -> head, each replaced while
// BLOCKED, and leaves head BLOCKED at the cap.
func (f *blockedChainFixture) cappedChain(prefix, head string) {
	f.t.Helper()
	f.block(prefix + "-a")
	if err := f.supersede(prefix+"-a", prefix+"-b"); err != nil {
		f.t.Fatalf("first blocked recovery: %v", err)
	}
	f.block(prefix + "-b")
	if err := f.supersede(prefix+"-b", head); err != nil {
		f.t.Fatalf("second blocked recovery: %v", err)
	}
	f.block(head)
}

func requireCapRefusal(t *testing.T, err error, f *blockedChainFixture, id string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "blocked-recovery cap") {
		t.Fatalf("supersede %s error = %v, want blocked-recovery cap refusal", id, err)
	}
	if status := f.read().FindTask(id).Status; status != models.TaskStatusBlocked {
		t.Fatalf("%s status = %s after refusal, want BLOCKED", id, status)
	}
}

func analyzeReports(t *testing.T, f *blockedChainFixture, id string) {
	t.Helper()
	result, err := ops.Analyze(f.root)
	if err != nil {
		t.Fatalf("Analyze() error: %v", err)
	}
	if !result.Triggered || result.Pattern != models.BlockedReplacementChainPattern || !strings.Contains(result.Evidence, "task "+id+" ") {
		t.Fatalf("analyze = %+v, want %s reporting %s", result, models.BlockedReplacementChainPattern, id)
	}
	subject := f.read().CircuitBreaker.CurrentResponse.Subject
	if subject == nil || subject.TaskID != id {
		t.Fatalf("current response subject = %+v, want %s", subject, id)
	}
}

func resume(t *testing.T, f *blockedChainFixture) {
	t.Helper()
	if _, err := ops.Resume(f.root, "operator-1"); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
}

// F1 regression: a capped head cannot be replaced before analysis, so the
// halt is reported on current evidence; resume authorizes exactly one more
// replacement and the cap engages again when that successor blocks.
func TestBlockedReplacementChain_CapHoldsUntilHumanRelease(t *testing.T) {
	f := newBlockedChainFixture(t)
	f.cappedChain("chain", "chain-c")

	requireCapRefusal(t, f.supersede("chain-c", "chain-d"), f, "chain-c")
	analyzeReports(t, f, "chain-c")
	if mode := f.read().Config.Mode; mode != models.SystemModeCircuitBreakerTripped {
		t.Fatalf("mode = %s, want %s", mode, models.SystemModeCircuitBreakerTripped)
	}
	resume(t, f)

	if err := f.supersede("chain-c", "chain-e"); err != nil {
		t.Fatalf("released replacement refused: %v", err)
	}
	f.block("chain-e")
	requireCapRefusal(t, f.supersede("chain-e", "chain-f"), f, "chain-e")
}

func TestBlockedReplacementChain_ReleaseCoversOnlyReportedEpisode(t *testing.T) {
	t.Run("independent capped chain stays capped and is reported next", func(t *testing.T) {
		f := newBlockedChainFixture(t)
		f.cappedChain("one", "one-c")
		f.cappedChain("two", "two-c")
		analyzeReports(t, f, "one-c")
		resume(t, f)

		requireCapRefusal(t, f.supersede("two-c", "two-d"), f, "two-c")
		analyzeReports(t, f, "two-c")
	})

	t.Run("a chain capped between analyze and resume is not released", func(t *testing.T) {
		f := newBlockedChainFixture(t)
		f.cappedChain("one", "one-c")
		f.block("two-a")
		if err := f.supersede("two-a", "two-b"); err != nil {
			t.Fatal(err)
		}
		f.block("two-b")
		if err := f.supersede("two-b", "two-c"); err != nil {
			t.Fatal(err)
		}
		analyzeReports(t, f, "one-c")
		f.block("two-c")
		resume(t, f)

		requireCapRefusal(t, f.supersede("two-c", "two-d"), f, "two-c")
		if err := f.supersede("one-c", "one-d"); err != nil {
			t.Fatalf("reported chain not released: %v", err)
		}
	})

	t.Run("a same-task re-block after release is capped again", func(t *testing.T) {
		f := newBlockedChainFixture(t)
		f.cappedChain("one", "one-c")
		analyzeReports(t, f, "one-c")
		resume(t, f)
		f.block("one-c")

		requireCapRefusal(t, f.supersede("one-c", "one-d"), f, "one-c")
		analyzeReports(t, f, "one-c")
	})
}

func (f *blockedChainFixture) addProviderAuditEvidence(at time.Time) {
	f.modify(func(s *models.State) {
		for i, agentID := range []string{"coder-1", "coder-2"} {
			s.Anomalies = append(s.Anomalies, models.Anomaly{
				Timestamp: at.Add(time.Duration(i) * time.Second), Reporter: agentID, Type: "provider_audit_degraded",
				Details: map[string]any{"provider": "codex", "agent_id": agentID, "message": "failed to record rollout items"},
			})
		}
	})
}

// D-86 regression: provider-audit evidence that selects WARNING or CHECKPOINT
// must not hide a capped chain, so analyze then resume still releases exactly
// the reported episode.
func TestBlockedReplacementChain_ProviderAuditDoesNotMaskRelease(t *testing.T) {
	t.Run("acknowledged provider evidence", func(t *testing.T) {
		f := newBlockedChainFixture(t)
		boundary := f.clock
		resolvedAt := boundary.Add(30 * time.Second)
		pattern := "provider_audit_degradation"
		f.addProviderAuditEvidence(boundary.Add(-2 * time.Minute))
		f.modify(func(s *models.State) {
			s.CircuitBreaker.Status = "OK"
			s.CircuitBreaker.History = append(s.CircuitBreaker.History, models.CircuitBreakerHistory{
				Timestamp: boundary, Pattern: &pattern, Result: "CHECKPOINT", Response: models.CircuitBreakerResponseCheckpoint,
				Classification: models.CircuitBreakerEvidenceNew, ResolvedAt: &resolvedAt,
			})
		})
		f.cappedChain("one", "one-c")
		f.cappedChain("two", "two-c")

		analyzeReports(t, f, "one-c")
		if mode := f.read().Config.Mode; mode != models.SystemModeCircuitBreakerTripped {
			t.Fatalf("mode = %s, want %s", mode, models.SystemModeCircuitBreakerTripped)
		}
		resume(t, f)

		if err := f.supersede("one-c", "one-d"); err != nil {
			t.Fatalf("released replacement refused: %v", err)
		}
		requireCapRefusal(t, f.supersede("two-c", "two-d"), f, "two-c")
		analyzeReports(t, f, "two-c")
	})

	t.Run("active provider checkpoint is superseded by the chain halt", func(t *testing.T) {
		f := newBlockedChainFixture(t)
		f.addProviderAuditEvidence(f.clock)
		checkpoint, err := ops.Analyze(f.root)
		if err != nil || checkpoint.Pattern != "provider_audit_degradation" || checkpoint.Response != models.CircuitBreakerResponseCheckpoint {
			t.Fatalf("Analyze() = %+v, %v; want provider CHECKPOINT", checkpoint, err)
		}
		f.cappedChain("one", "one-c")

		analyzeReports(t, f, "one-c")
		superseded := false
		for _, entry := range f.read().CircuitBreaker.History {
			if entry.Pattern != nil && *entry.Pattern == "provider_audit_degradation" && entry.Response == models.CircuitBreakerResponseCheckpoint {
				superseded = entry.SupersededByResponse == models.CircuitBreakerResponseHalt && entry.ResolvedAt == nil
			}
		}
		if !superseded {
			t.Fatalf("provider checkpoint history = %+v, want it superseded by HALT and unresolved", f.read().CircuitBreaker.History)
		}
		resume(t, f)

		if err := f.supersede("one-c", "one-d"); err != nil {
			t.Fatalf("released replacement refused: %v", err)
		}
	})
}
