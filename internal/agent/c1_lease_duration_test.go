package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// C1 lease cases: the configured lease_duration, and the unset path (0), which
// must give the 1800 s default. Expected deltas are literals, independent of
// the resolver under test.
var c1LeaseCases = []struct {
	name       string
	configured int
	want       time.Duration
}{
	{"configured_600", 600, 600 * time.Second},
	{"default_unset", 0, 1800 * time.Second},
}

func TestC1TerminalPlaceholderKeepsStoredValue(t *testing.T) {
	if terminalPlaceholder != "terminal-1" {
		t.Errorf("terminalPlaceholder = %q, want %q", terminalPlaceholder, "terminal-1")
	}
}

// startC1Lifetime runs the production startup steps of RunSupervisor
// (strategy selection, registration, heartbeat construction) against a
// disposable project.
func startC1Lifetime(t *testing.T, agentID, role string, configured int) (*supervisorLifetime, *db.Blackboard) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	state.Config.LeaseDuration = configured
	bb := testhelpers.WriteInitialState(t, statePath, state)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	lt, err := newSupervisorLifetime(ctx, ctx, cancel, SupervisorConfig{
		AgentID:     agentID,
		Role:        role,
		ProjectRoot: root,
		StatePath:   statePath,
		LogPath:     paths.ProjectDirName() + "/log.yaml",
		CLIName:     "codex",
	})
	if err != nil {
		t.Fatalf("newSupervisorLifetime: %v", err)
	}
	if err := lt.selectStrategy(); err != nil {
		t.Fatalf("selectStrategy: %v", err)
	}
	return lt, bb
}

func TestC1RegistrationHonoursConfiguredLease(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			lt, bb := startC1Lifetime(t, "coder-1", models.RoleCoder, tc.configured)
			if err := lt.register(); err != nil {
				t.Fatalf("register: %v", err)
			}
			t.Cleanup(lt.unregister)

			state, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			agent, ok := state.Agents["coder-1"]
			if !ok || agent.LeaseExpires == nil {
				t.Fatalf("registered row = %+v (present %v), want a lease", agent, ok)
			}
			if got := agent.LeaseExpires.Sub(agent.RegisteredAt); got != tc.want {
				t.Errorf("lease expires %v after registration, want %v", got, tc.want)
			}
			if agent.Terminal != "terminal-1" {
				t.Errorf("stored terminal = %q, want %q", agent.Terminal, "terminal-1")
			}
		})
	}
}

// beatC1 registers the lifetime, lets seed give the registered agent its work
// (the overlay renews only the agent's current task), starts the heartbeat
// wiring with the provider session of activeTask (if any) in progress, and
// publishes exactly one beat from the heartbeat that wiring built.
func beatC1(t *testing.T, lt *supervisorLifetime, bb *db.Blackboard, activeTask string, seed func(*models.State)) {
	t.Helper()
	if err := lt.register(); err != nil {
		t.Fatalf("register: %v", err)
	}
	t.Cleanup(lt.unregister)
	if err := bb.Modify(func(s *models.State) error { seed(s); return nil }); err != nil {
		t.Fatalf("seed work: %v", err)
	}
	if err := lt.start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(lt.shutdown)
	if activeTask != "" {
		lt.config.sessionActivity.start(activeTask)
	}
	if lt.heartbeat == nil {
		t.Fatal("start built no heartbeat")
	}
	if err := lt.heartbeat.beat(); err != nil {
		t.Fatalf("beat: %v", err)
	}
}

// The overlaid read applies the beat's agent lease and the lease of the task
// assigned to the agent; each must be the beat's timestamp plus the duration.
func TestC1HeartbeatHonoursConfiguredLeaseForAgentAndOwnedTask(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			const agentID, taskID = "coder-1", "task-1"
			stale := time.Now().UTC().Add(time.Minute)
			lt, bb := startC1Lifetime(t, agentID, models.RoleCoder, tc.configured)
			beatC1(t, lt, bb, "", func(s *models.State) {
				owner, current := agentID, taskID
				s.Tasks = []models.Task{{ID: taskID, Status: models.TaskStatusImplementing, AssignedTo: &owner, LeaseExpires: &stale}}
				agent := s.Agents[agentID]
				agent.Status, agent.CurrentTask = models.AgentStatusWorking, &current
				s.Agents[agentID] = agent
			})

			state, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			agent, task := state.Agents[agentID], state.FindTask(taskID)
			if agent.LeaseExpires == nil || task.LeaseExpires == nil {
				t.Fatalf("leases after beat: agent=%v task=%v", agent.LeaseExpires, task.LeaseExpires)
			}
			if got := agent.LeaseExpires.Sub(agent.Heartbeat); got != tc.want {
				t.Errorf("agent lease = beat + %v, want + %v", got, tc.want)
			}
			if got := task.LeaseExpires.Sub(agent.Heartbeat); got != tc.want {
				t.Errorf("owned task lease = beat + %v, want + %v", got, tc.want)
			}
		})
	}
}

// The review lease renews only during the provider session of the claimed task.
func TestC1HeartbeatHonoursConfiguredLeaseForActiveReview(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			const agentID, taskID = "code-reviewer-1", "task-1"
			stale := time.Now().UTC().Add(time.Minute)
			lt, bb := startC1Lifetime(t, agentID, models.RoleCodeReviewer, tc.configured)
			beatC1(t, lt, bb, taskID, func(s *models.State) {
				reviewer, current := agentID, taskID
				s.Tasks = []models.Task{{ID: taskID, Status: models.TaskStatusReviewing, ReviewingBy: &reviewer, ReviewLeaseExpires: &stale}}
				agent := s.Agents[agentID]
				agent.Status, agent.CurrentTask = models.AgentStatusReviewing, &current
				s.Agents[agentID] = agent
			})

			state, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			agent, task := state.Agents[agentID], state.FindTask(taskID)
			if task.ReviewLeaseExpires == nil {
				t.Fatal("review lease cleared")
			}
			if got := task.ReviewLeaseExpires.Sub(agent.Heartbeat); got != tc.want {
				t.Errorf("review lease = beat + %v, want + %v", got, tc.want)
			}
		})
	}
}

// The strategy reads the project configuration itself and claims through the
// real typed claim operation.
func TestC1ReviewerClaimHonoursConfiguredLease(t *testing.T) {
	for _, tc := range c1LeaseCases {
		t.Run(tc.name, func(t *testing.T) {
			project := setupReviewClaimProject(t)
			project.addReviewableTask(t, "task-healthy", false)
			if err := project.bb.Modify(func(s *models.State) error {
				s.Config.LeaseDuration = tc.configured
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			config := project.supervisorConfig(t)
			reviewer := newReviewerStrategyForTest(t)

			before := time.Now()
			taskID, _, err := reviewer.ClaimTask(config, project.bb)
			after := time.Now()
			if err != nil || taskID != "task-healthy" {
				t.Fatalf("ClaimTask() = (%q, %v), want (task-healthy, nil)", taskID, err)
			}
			state, err := project.bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			lease := state.FindTask(taskID).ReviewLeaseExpires
			if lease == nil {
				t.Fatal("claimed task has no review lease")
			}
			const slack = 2 * time.Second
			if lo, hi := before.Add(tc.want-slack), after.Add(tc.want+slack); lease.Before(lo) || lease.After(hi) {
				t.Errorf("review lease = %v, want within [%v, %v] (%v after the claim)", lease, lo, hi, tc.want)
			}
		})
	}
}
