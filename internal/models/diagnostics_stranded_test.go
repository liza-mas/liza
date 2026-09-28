package models

import (
	"testing"
	"time"
)

// D75 residual: an executing doer claim whose task lease has expired and whose
// holder no longer has a live registration is stranded work. It must be ready
// for the doer role — so autorepair and idle doers see it — while a claim whose
// holder is still registered, whose lease is live, which cannot continue as a
// preserved branch, or whose dependencies are unmet stays unready.
func TestStrandedExecutingDoerClaimReadiness(t *testing.T) {
	pr := &mockPipelineResolver{
		doer:      "coder",
		reviewer:  "code-reviewer",
		initial:   TaskStatusReady,
		rejected:  TaskStatusRejected,
		submitted: TaskStatusReadyForReview,
		reviewing: TaskStatusReviewing,
		executing: TaskStatusImplementing,
		approved:  TaskStatusApproved,
	}
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	live := now.Add(time.Hour)
	holder := "coder-1"
	worktree := ".worktrees/stranded"
	base := "base-sha"

	stranded := func(id string) Task {
		return Task{
			ID: id, Status: TaskStatusImplementing, RolePair: "coding-pair",
			AssignedTo: &holder, LeaseExpires: &expired, Worktree: &worktree, BaseCommit: &base,
		}
	}
	liveLease := stranded("task-lease-live")
	liveLease.LeaseExpires = &live
	noBase := stranded("no-base-commit")
	noBase.BaseCommit = nil
	unmetDependency := stranded("unmet-dependency")
	unmetDependency.DependsOn = []string{"producer"}

	holderGone := &State{Agents: map[string]Agent{}, Tasks: []Task{stranded("holder-gone"), liveLease, noBase,
		unmetDependency, {ID: "producer", Status: TaskStatusImplementing, RolePair: "coding-pair"}}}
	holderExpired := &State{Agents: map[string]Agent{holder: {Role: "coder", Generation: "g1", Heartbeat: expired, LeaseExpires: &expired}},
		Tasks: []Task{stranded("holder-expired")}}
	holderLive := &State{Agents: map[string]Agent{holder: {Role: "coder", Generation: "g1", Heartbeat: now, LeaseExpires: &live}},
		Tasks: []Task{stranded("holder-live")}}

	tests := []struct {
		name  string
		state *State
		task  string
		want  bool
	}{
		{"holder unregistered", holderGone, "holder-gone", true},
		{"holder registration expired", holderExpired, "holder-expired", true},
		{"holder registration live", holderLive, "holder-live", false},
		{"task lease live", holderGone, "task-lease-live", false},
		{"no base commit to continue from", holderGone, "no-base-commit", false},
		{"unmet dependency", holderGone, "unmet-dependency", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := tt.state.FindTask(tt.task)
			if got := StrandedDoerClaimReason(tt.state, task, pr, now) != ""; got != tt.want {
				t.Errorf("StrandedDoerClaimReason() stranded = %v, want %v", got, tt.want)
			}
			if got := IsRoleTaskReady(tt.state, task, RoleCoder, pr, now); got != tt.want {
				t.Errorf("IsRoleTaskReady(coder) = %v, want %v", got, tt.want)
			}
			reason := DoerClaimBlockedReason(tt.state, task, RoleCoder, "coder-2", pr, now)
			if (reason == "") != tt.want {
				t.Errorf("DoerClaimBlockedReason(coder-2) = %q, want claimable=%v", reason, tt.want)
			}
		})
	}
}

// TestDoerClaimabilityWithoutResolverFailsClosed pins the nil-resolver
// contract ("no work visible"): a pipeline load failure leaves supervisors with
// a nil resolver, and the stranded-claim check must not turn that into a panic.
func TestDoerClaimabilityWithoutResolverFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	holder, worktree, base := "coder-1", ".worktrees/stranded", "base-sha"
	state := &State{Agents: map[string]Agent{}, Tasks: []Task{{
		ID: "stranded", Status: TaskStatusImplementing, RolePair: "coding-pair",
		AssignedTo: &holder, LeaseExpires: &expired, Worktree: &worktree, BaseCommit: &base,
	}}}

	if reason := DoerClaimBlockedReason(state, &state.Tasks[0], RoleCoder, "coder-2", nil, now); reason == "" {
		t.Error("DoerClaimBlockedReason(nil resolver) = \"\", want a blocked reason")
	}
	if got := CountDoerClaimableTasksForAgent(state, RoleCoder, "coder-2", nil); got != 0 {
		t.Errorf("CountDoerClaimableTasksForAgent(nil resolver) = %d, want 0", got)
	}
	if IsRoleTaskReady(state, &state.Tasks[0], RoleCoder, nil, now) {
		t.Error("IsRoleTaskReady(nil resolver) = true, want false")
	}
}

// TestAgentRegistrationLive pins the liveness rule the pool repair used before
// it moved here: a lease decides when present, otherwise a recent heartbeat.
func TestAgentRegistrationLive(t *testing.T) {
	now := time.Now().UTC()
	window := time.Minute
	future, past := now.Add(time.Hour), now.Add(-time.Second)
	tests := []struct {
		name  string
		agent Agent
		want  bool
	}{
		{"lease live", Agent{Role: "coder", LeaseExpires: &future}, true},
		{"lease expired despite a fresh heartbeat", Agent{Role: "coder", LeaseExpires: &past, Heartbeat: now}, false},
		{"no lease, heartbeat in window", Agent{Role: "coder", Heartbeat: now.Add(-window / 2)}, true},
		{"no lease, heartbeat out of window", Agent{Role: "coder", Heartbeat: now.Add(-2 * window)}, false},
		{"no lease, no heartbeat", Agent{Role: "coder"}, false},
		{"no role", Agent{LeaseExpires: &future}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentRegistrationLive(tt.agent, now, window); got != tt.want {
				t.Errorf("AgentRegistrationLive() = %v, want %v", got, tt.want)
			}
		})
	}
}
