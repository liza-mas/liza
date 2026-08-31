package ops

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
)

func TestAgentProcessStatusOwnership(t *testing.T) {
	now := time.Now().UTC()

	// The two "no procfs" cases below exercise the real native liveness
	// probe (procscan.ProcessAlive), not a stub, so the source label it
	// reports is genuinely platform-specific: Unix names its probe
	// "signal(0)", Windows "OpenProcess+GetExitCodeProcess".
	nativeProbeSource := "signal(0)"
	if runtime.GOOS == "windows" {
		nativeProbeSource = "OpenProcess+GetExitCodeProcess"
	}

	tests := []struct {
		name                  string
		recordedPID           int
		recordedArgv          []string
		procfsUnavailable     bool
		candidatePIDs         []int
		wantRawState          procscan.AgentProcessState
		wantRawSource         string
		wantRawDetailContains string
		wantEffective         AgentOwnershipState
		wantOccupied          bool
		wantCorrelation       string
	}{
		{
			name:                  "matching recorded pid remains live while lease is fresh",
			recordedPID:           1234,
			recordedArgv:          []string{"liza", "agent", "orchestrator", "--agent-id", "orchestrator-1"},
			wantRawState:          procscan.AgentProcessLiveMatching,
			wantRawSource:         "procfs",
			wantRawDetailContains: "cmdline matches expected agent supervisor",
			wantEffective:         AgentOwnershipLive,
			wantOccupied:          true,
			wantCorrelation:       "correlation unavailable",
		},
		{
			name:                  "mismatched recorded pid with sorted explicit candidates",
			recordedPID:           1234,
			recordedArgv:          []string{"go", "test"},
			candidatePIDs:         []int{42, 7},
			wantRawState:          procscan.AgentProcessMismatched,
			wantRawSource:         "procfs",
			wantRawDetailContains: "pid exists but cmdline does not match expected agent supervisor",
			wantEffective:         AgentOwnershipUnknownDegraded,
			wantOccupied:          true,
			wantCorrelation:       "observer-visible matching pids [7 42]",
		},
		{
			name:                  "pid not found remains degraded while lease is fresh",
			recordedPID:           987654321,
			wantRawState:          procscan.AgentProcessDead,
			wantRawSource:         nativeProbeSource,
			wantRawDetailContains: "process",
			wantEffective:         AgentOwnershipUnknownDegraded,
			wantOccupied:          true,
			wantCorrelation:       "correlation unavailable",
		},
		{
			name:                  "signal alive with unavailable procfs remains degraded while lease is fresh",
			recordedPID:           os.Getpid(),
			procfsUnavailable:     true,
			wantRawState:          procscan.AgentProcessUnknown,
			wantRawSource:         nativeProbeSource,
			wantRawDetailContains: "procfs unavailable",
			wantEffective:         AgentOwnershipUnknownDegraded,
			wantOccupied:          true,
			wantCorrelation:       "correlation unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procRoot := t.TempDir()
			if tt.procfsUnavailable {
				procRoot = filepath.Join(procRoot, "unavailable")
			}
			t.Cleanup(SetAgentProcessProcRootForTest(procRoot))
			if len(tt.recordedArgv) > 0 {
				writeAgentProcessCmdline(t, procRoot, tt.recordedPID, tt.recordedArgv)
			}
			for _, pid := range tt.candidatePIDs {
				writeAgentProcessCmdline(t, procRoot, pid, []string{
					"liza", "agent", "orchestrator", "--agent-id", "orchestrator-1",
				})
			}
			if !tt.procfsUnavailable {
				writeAgentProcessCmdline(t, procRoot, 8, []string{
					"liza", "agent", "orchestrator", "--agent-id", "orchestrator-2",
				})
				writeAgentProcessCmdline(t, procRoot, 9, []string{
					"liza", "agent", "orchestrator",
				})
			}

			leaseExpires := now.Add(10 * time.Minute)
			agent := models.Agent{
				Role:         "orchestrator",
				Heartbeat:    now,
				LeaseExpires: &leaseExpires,
				PID:          tt.recordedPID,
			}

			got := AgentProcessOwnership("orchestrator-1", agent, now)
			if got.Raw.State != tt.wantRawState || got.Raw.Source != tt.wantRawSource || !strings.Contains(got.Raw.Detail, tt.wantRawDetailContains) {
				t.Fatalf("raw status = %+v, want state=%s source=%q detail containing %q", got.Raw, tt.wantRawState, tt.wantRawSource, tt.wantRawDetailContains)
			}
			if got.Effective != tt.wantEffective || got.Occupied() != tt.wantOccupied {
				t.Fatalf("effective ownership = %q occupied=%v, want %q occupied=%v", got.Effective, got.Occupied(), tt.wantEffective, tt.wantOccupied)
			}
			wantCandidates := append([]int(nil), tt.candidatePIDs...)
			if len(wantCandidates) == 2 && wantCandidates[0] > wantCandidates[1] {
				wantCandidates[0], wantCandidates[1] = wantCandidates[1], wantCandidates[0]
			}
			if !reflect.DeepEqual(got.CandidatePIDs, wantCandidates) {
				t.Fatalf("candidate pids = %v, want %v", got.CandidatePIDs, wantCandidates)
			}
			if diagnostic := got.Diagnostic(agent.PID); !strings.Contains(diagnostic, tt.wantCorrelation) {
				t.Fatalf("diagnostic = %q, want %q", diagnostic, tt.wantCorrelation)
			}

			expired := now.Add(-time.Minute)
			agent.LeaseExpires = &expired
			expiredObservation := AgentProcessOwnership("orchestrator-1", agent, now)
			if expiredObservation.Raw != got.Raw {
				t.Fatalf("expired raw status = %+v, want preserved %+v", expiredObservation.Raw, got.Raw)
			}
			if expiredObservation.Effective != AgentOwnershipLeaseExpiredOrStale || expiredObservation.Occupied() {
				t.Fatalf("expired effective ownership = %q occupied=%v, want lease_expired_or_stale and unoccupied", expiredObservation.Effective, expiredObservation.Occupied())
			}
		})
	}
}

func writeAgentProcessCmdline(t *testing.T, procRoot string, pid int, argv []string) {
	t.Helper()
	procDir := filepath.Join(procRoot, strconv.Itoa(pid))
	if err := os.MkdirAll(procDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "cmdline"), []byte(strings.Join(argv, "\x00")+"\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
}
