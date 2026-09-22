package ops

import (
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/procscan"
)

var agentProcessProcRoot = "/proc"

// SetAgentProcessProcRootForTest redirects process identity checks to a fake
// procfs tree and returns a cleanup function.
func SetAgentProcessProcRootForTest(procRoot string) func() {
	old := agentProcessProcRoot
	agentProcessProcRoot = procRoot
	return func() {
		agentProcessProcRoot = old
	}
}

func AgentProcessStatus(agentID string, agent models.Agent) procscan.AgentProcessStatus {
	return procscan.AgentProcessStatusForPID(agent.PID, agent.Role, agentID, agentProcessProcRoot)
}

// AgentOwnershipState is the lease-first decision used at registration and
// registered-agent watcher boundaries. Review cleanup separately checks its
// task/agent tuple before preserving ownership under a fresh registration lease.
type AgentOwnershipState string

const (
	AgentOwnershipLive                AgentOwnershipState = "live"
	AgentOwnershipUnknownDegraded     AgentOwnershipState = "unknown/degraded"
	AgentOwnershipLeaseExpiredOrStale AgentOwnershipState = "lease_expired_or_stale"
)

// AgentProcessObservation keeps namespace-relative process evidence separate
// from the effective distributed ownership decision and diagnostic-only PID
// correlation.
type AgentProcessObservation struct {
	Raw           procscan.AgentProcessStatus
	Effective     AgentOwnershipState
	CandidatePIDs []int
}

func (o AgentProcessObservation) Occupied() bool {
	return o.Effective == AgentOwnershipLive || o.Effective == AgentOwnershipUnknownDegraded
}

func (o AgentProcessObservation) Diagnostic(recordedPID int) string {
	correlation := "correlation unavailable"
	if len(o.CandidatePIDs) == 1 {
		correlation = fmt.Sprintf("observer-visible matching pid %d", o.CandidatePIDs[0])
	} else if len(o.CandidatePIDs) > 1 {
		correlation = fmt.Sprintf("observer-visible matching pids %v", o.CandidatePIDs)
	}
	return fmt.Sprintf(
		"effective ownership %s; raw state %s from %s (%s); recorded namespace-local pid %d; %s",
		o.Effective,
		o.Raw.State,
		o.Raw.Source,
		o.Raw.Detail,
		recordedPID,
		correlation,
	)
}

// AgentProcessOwnership projects a lease-first decision while preserving raw
// process evidence. Correlated PIDs are diagnostic only and never affect
// Occupied.
func AgentProcessOwnership(agentID string, agent models.Agent, now time.Time) AgentProcessObservation {
	raw := AgentProcessStatus(agentID, agent)
	effective := AgentOwnershipLeaseExpiredOrStale
	freshLease := agent.LeaseExpires != nil && agent.LeaseExpires.After(now) && !agent.Heartbeat.IsZero()
	if freshLease {
		effective = AgentOwnershipUnknownDegraded
		if raw.IsLiveMatching() {
			effective = AgentOwnershipLive
		}
	}

	var differentCandidates []int
	if freshLease && !raw.IsLiveMatching() {
		candidates := procscan.FindExplicitAgentIdentityPIDs(agent.Role, agentID, agentProcessProcRoot)
		for _, pid := range candidates {
			if pid != agent.PID {
				differentCandidates = append(differentCandidates, pid)
			}
		}
	}

	return AgentProcessObservation{
		Raw:           raw,
		Effective:     effective,
		CandidatePIDs: differentCandidates,
	}
}
