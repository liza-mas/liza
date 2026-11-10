package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Issue #172: ownership stays lease-first (ADR-0130), so a registration whose
// liveness lapsed is taken over even when its recorded PID still runs a
// matching supervisor. That takeover fences a working agent, so it must not be
// silent: it logs at Warn and writes a LIVE AGENT TAKEOVER alert. Taking over
// a dead or unverifiable PID stays an Info-level recovery without an alert.

const (
	takeoverAgentID = "code-planner-1"
	takeoverRole    = "code-planner"
	// takeoverPID is a fake procfs entry, never a real process of this test.
	takeoverPID = 4242
	// liveTakeoverCategory is the alerts.log category, spelled as an operator
	// reads it.
	liveTakeoverCategory = "LIVE AGENT TAKEOVER"
)

// expiredRegistration is a row whose lease and heartbeat both lapsed.
func expiredRegistration(pid int) models.Agent {
	now := time.Now().UTC()
	expired := now.Add(-time.Hour)
	return models.Agent{
		Role:         takeoverRole,
		Status:       models.AgentStatusIdle,
		Generation:   "gen-old",
		Heartbeat:    now.Add(-2 * time.Hour),
		RegisteredAt: now.Add(-3 * time.Hour),
		LeaseExpires: &expired,
		Provider:     "claude",
		PID:          pid,
	}
}

// fakeProcRoot redirects process inspection to a procfs tree that holds
// takeoverPID running the given argv, or nothing when argv is nil.
func fakeProcRoot(t *testing.T, argv []string) {
	t.Helper()
	procRoot := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(procRoot))
	if argv == nil {
		return
	}
	procDir := filepath.Join(procRoot, strconv.Itoa(takeoverPID))
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func matchingSupervisorArgv() []string {
	return []string{"liza", "agent", takeoverRole, "--agent-id", takeoverAgentID}
}

func readAlertsLog(t *testing.T, projectRoot string) string {
	t.Helper()
	data, err := os.ReadFile(paths.New(projectRoot).AlertsLogPath())
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatalf("read alerts log: %v", err)
	}
	return string(data)
}

func TestRegisterAgentAlertsWhenTakingOverLiveMatchingSupervisor(t *testing.T) {
	tests := []struct {
		name      string
		procArgv  []string
		wantAlert bool
		wantLevel string
	}{
		{name: "live matching recorded pid warns and alerts", procArgv: matchingSupervisorArgv(), wantAlert: true, wantLevel: "WARN"},
		{name: "dead recorded pid stays a silent info recovery", procArgv: nil, wantLevel: "INFO"},
		{name: "mismatched recorded pid stays a silent info recovery", procArgv: []string{"go", "test"}, wantLevel: "INFO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN an expired registration whose recorded pid is in the given state
			fakeProcRoot(t, tt.procArgv)
			logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
			projectRoot := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			testhelpers.SetupPipelineConfig(t, projectRoot)
			state := testhelpers.CreateValidState()
			state.Agents[takeoverAgentID] = expiredRegistration(takeoverPID)
			bb := testhelpers.WriteInitialState(t, statePath, state)

			// WHEN a new supervisor registers the same ID
			authority, err := registerAgentWithAuthority(bb, projectRoot, takeoverAgentID, takeoverRole, "terminal-2", 1800, "claude", "", testResolver(t))

			// THEN lease-first ownership still lets it take over
			if err != nil {
				t.Fatalf("registerAgentWithAuthority() error = %v, want lease-first takeover", err)
			}
			if authority.Generation == "" || authority.Generation == "gen-old" {
				t.Fatalf("authority generation = %q, want a new generation", authority.Generation)
			}

			// AND only a live matching pid raises the alert and the warning
			alertsLog := readAlertsLog(t, projectRoot)
			alertLines := countLogLines(alertsLog, liveTakeoverCategory)
			if tt.wantAlert {
				if alertLines != 1 || countLogLines(alertsLog, liveTakeoverCategory, takeoverAgentID, "pid "+strconv.Itoa(takeoverPID)) != 1 {
					t.Fatalf("alerts.log = %q, want one %s alert naming %s and pid %d", alertsLog, liveTakeoverCategory, takeoverAgentID, takeoverPID)
				}
			} else if alertLines != 0 {
				t.Fatalf("alerts.log = %q, want no %s alert", alertsLog, liveTakeoverCategory)
			}
			takeoverLogs := countLogLines(logs.String(), "level="+tt.wantLevel, "Taking over", takeoverAgentID)
			if takeoverLogs != 1 {
				t.Fatalf("logs = %q, want one level=%s takeover line for %s", logs.String(), tt.wantLevel, takeoverAgentID)
			}
			otherLevel := "WARN"
			if tt.wantLevel == "WARN" {
				otherLevel = "INFO"
			}
			if n := countLogLines(logs.String(), "level="+otherLevel, "Taking over", takeoverAgentID); n != 0 {
				t.Fatalf("logs = %q, want no level=%s takeover line", logs.String(), otherLevel)
			}
		})
	}
}

// An alert that cannot be written is logged and never fails the registration.
func TestRegisterAgentLiveTakeoverSurvivesAlertWriteFailure(t *testing.T) {
	// GIVEN a live matching expired registration and an unwritable alerts log
	fakeProcRoot(t, matchingSupervisorArgv())
	logs := captureAgentLogsAtLevel(t, slog.LevelInfo)
	projectRoot := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	if err := os.MkdirAll(paths.New(projectRoot).AlertsLogPath(), 0o755); err != nil {
		t.Fatalf("make alerts log path a directory: %v", err)
	}
	state := testhelpers.CreateValidState()
	state.Agents[takeoverAgentID] = expiredRegistration(takeoverPID)
	bb := testhelpers.WriteInitialState(t, statePath, state)

	// WHEN a new supervisor registers the same ID
	authority, err := registerAgentWithAuthority(bb, projectRoot, takeoverAgentID, takeoverRole, "terminal-2", 1800, "claude", "", testResolver(t))

	// THEN registration returns the new authority and logs the alert failure
	if err != nil {
		t.Fatalf("registerAgentWithAuthority() error = %v, want takeover despite the alert failure", err)
	}
	stored, readErr := bb.ReadSnapshot()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := stored.Agents[takeoverAgentID].Generation; got != authority.Generation || got == "gen-old" {
		t.Fatalf("stored generation = %q, want the returned new generation %q", got, authority.Generation)
	}
	if countLogLines(logs.String(), "level=WARN", "alert", takeoverAgentID, "error=") == 0 {
		t.Fatalf("logs = %q, want a Warn line reporting the failed %s alert", logs.String(), liveTakeoverCategory)
	}
}

// Issue #172's reported shape on current main: a reviewer's verdict cleared
// its lease and time passed, so state.yaml alone reads as lapsed, while
// another process holds the state lock. The live supervisor's heartbeat still
// publishes its lock-free liveness record (D-41), and registration folds it,
// so a same-ID spawn collides instead of fencing the live agent. The D-17
// nil-lease window is pinned separately by
// TestRegisterAgentLeaseLessRegistrationOwnedForLivenessWindow.
func TestRegisterAgentKeepsLeaseLessRegistrationRenewedOnlyByLivenessRecord(t *testing.T) {
	for _, beats := range []bool{true, false} {
		name := "heartbeat record published under state-lock contention keeps the registration"
		if !beats {
			name = "without the record the lapsed registration is taken over"
		}
		t.Run(name, func(t *testing.T) {
			t.Cleanup(ops.SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
			projectRoot := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, projectRoot)
			testhelpers.SetupPipelineConfig(t, projectRoot)
			bb := testhelpers.WriteInitialState(t, statePath, testhelpers.CreateValidState())
			resolver := testResolver(t)

			// GIVEN a live registration G1 that a verdict left lease-less, with
			// its last folded heartbeat past the liveness window
			g1, err := registerAgentWithAuthority(bb, projectRoot, takeoverAgentID, takeoverRole, "terminal-1", 1800, "claude", "", resolver)
			if err != nil {
				t.Fatalf("register G1: %v", err)
			}
			window := models.AgentLivenessWindow(testhelpers.CreateValidState().Config)
			if err := bb.Modify(func(s *models.State) error {
				agent := s.Agents[takeoverAgentID]
				agent.LeaseExpires = nil
				agent.Heartbeat = time.Now().UTC().Add(-window - time.Minute)
				s.Agents[takeoverAgentID] = agent
				return nil
			}); err != nil {
				t.Fatalf("simulate verdict lease clear: %v", err)
			}

			// AND, while another process holds the state lock, G1's heartbeat beats
			if beats {
				acquired, release := make(chan struct{}), make(chan struct{})
				holderDone := make(chan error, 1)
				go func() {
					holderDone <- filelock.New(statePath).WithLockOperation("holder", func() error {
						close(acquired)
						<-release
						return nil
					})
				}()
				<-acquired
				hb := NewHeartbeat(HeartbeatConfig{Authority: g1, StatePath: statePath, LeaseDuration: 30 * time.Minute})
				beatErr := hb.beat()
				close(release)
				if err := <-holderDone; err != nil {
					t.Fatalf("lock holder: %v", err)
				}
				if beatErr != nil {
					t.Fatalf("G1 beat under state-lock contention: %v", beatErr)
				}
			}

			// WHEN a new supervisor registers the same ID
			_, err = registerAgentWithAuthority(bb, projectRoot, takeoverAgentID, takeoverRole, "terminal-2", 1800, "claude", "", resolver)

			// THEN the live registration is kept only when its record renewed it
			stored, readErr := bb.ReadSnapshot()
			if readErr != nil {
				t.Fatal(readErr)
			}
			generation := stored.Agents[takeoverAgentID].Generation
			if beats {
				if !errors.IsAgentCollision(err) {
					t.Fatalf("registration error = %v, want AgentCollisionError for a supervisor its liveness record renewed", err)
				}
				if generation != g1.Generation {
					t.Fatalf("stored generation = %q, want G1 %q kept", generation, g1.Generation)
				}
				return
			}
			if err != nil {
				t.Fatalf("registration error = %v, want takeover of a lapsed lease-less registration", err)
			}
			if generation == g1.Generation {
				t.Fatalf("stored generation = %q, want a new generation after takeover", generation)
			}
		})
	}
}
