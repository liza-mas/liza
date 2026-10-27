package ops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizaerrors "github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const c6Generation = "c6-generation"

// c6Fixture is a disposable project: a state file, the frozen pipeline and the
// release inputs a caller would load outside the lock.
type c6Fixture struct {
	root      string
	statePath string
	bb        *db.Blackboard
	resolver  *pipeline.Resolver
	release   ClaimReleasePipeline
}

func newC6Fixture(t *testing.T, mutate func(*models.State)) *c6Fixture {
	t.Helper()
	t.Cleanup(SetAgentProcessProcRootForTest(filepath.Join(t.TempDir(), "missing-proc")))
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	mutate(state)
	bb := testhelpers.WriteInitialState(t, statePath, state)
	cfg, err := pipeline.LoadFrozen(root)
	if err != nil {
		t.Fatalf("load pipeline: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)
	return &c6Fixture{
		root: root, statePath: statePath, bb: bb, resolver: resolver,
		release: ClaimReleasePipeline{Transitions: BuildPipelineTransitions(resolver), Resolver: resolver},
	}
}

func (f *c6Fixture) read(t *testing.T) *models.State {
	t.Helper()
	state, err := f.bb.Read()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	return state
}

func (f *c6Fixture) bytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(f.statePath)
	if err != nil {
		t.Fatalf("read state bytes: %v", err)
	}
	return data
}

// requireUnchanged asserts that a refused operation left the state file as it was.
func (f *c6Fixture) requireUnchanged(t *testing.T, before []byte, what string) {
	t.Helper()
	if after := f.bytes(t); string(after) != string(before) {
		t.Fatalf("%s changed the state\nbefore:\n%s\nafter:\n%s", what, before, after)
	}
}

func c6LiveAgent(role string, now time.Time) models.Agent {
	lease := now.Add(30 * time.Minute)
	return models.Agent{
		Role: role, Status: models.AgentStatusIdle, Generation: c6Generation,
		Heartbeat: now, RegisteredAt: now, LeaseExpires: &lease, Provider: "claude", PID: os.Getpid(),
	}
}

func c6Authority(agentID string) models.AgentAuthority {
	return models.AgentAuthority{ID: agentID, Generation: c6Generation}
}

func requireAuthorityError(t *testing.T, err error, agentID string) {
	t.Helper()
	var authorityErr *AgentAuthorityError
	if !errors.As(err, &authorityErr) || authorityErr.AgentID != agentID {
		t.Fatalf("error = %T %v, want *AgentAuthorityError for %s", err, err, agentID)
	}
}

func c6Register(role, id string, resolver *pipeline.Resolver) RegisterAgentInput {
	return RegisterAgentInput{
		AgentID: id, Role: role, Terminal: "terminal-1", Provider: "claude", Model: "model-x",
		LeaseDurationSeconds: 600, Resolver: resolver,
	}
}

func TestC6RegisterAgentWritesIdleRowWithAuthority(t *testing.T) {
	f := newC6Fixture(t, func(s *models.State) { s.Config.MaxInstances = 3 })
	// A liveness record no registered (ID, generation) owns is swept in the
	// registration transaction (ADR-0177).
	orphan := f.statePath + ".liveness-0123456789abcdef"
	if err := os.WriteFile(orphan, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write orphan record: %v", err)
	}
	before := time.Now().UTC()

	got, err := RegisterAgent(f.bb, c6Register("coder", "coder-1", f.resolver))
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if got.TookOver || got.SweepErr != nil {
		t.Fatalf("fresh registration reported takeover %v / sweep error %v", got.TookOver, got.SweepErr)
	}
	agent, ok := f.read(t).Agents["coder-1"]
	if !ok {
		t.Fatal("registered agent row missing")
	}
	if got.Authority.ID != "coder-1" || got.Authority.Generation == "" || got.Authority.Generation != agent.Generation {
		t.Fatalf("returned authority %+v does not match the stored generation %q", got.Authority, agent.Generation)
	}
	if agent.Role != "coder" || agent.Status != models.AgentStatusIdle || agent.Terminal != "terminal-1" ||
		agent.Provider != "claude" || agent.Model != "model-x" || agent.PID != os.Getpid() || agent.CurrentTask != nil {
		t.Fatalf("registered row = %+v", agent)
	}
	if agent.LeaseExpires == nil || agent.LeaseExpires.Sub(agent.RegisteredAt) != 600*time.Second {
		t.Fatalf("lease %v is not registration time + 600s (registered %v)", agent.LeaseExpires, agent.RegisteredAt)
	}
	if agent.RegisteredAt.Before(before) || !agent.Heartbeat.Equal(agent.RegisteredAt) || agent.RegisteredAt.Location() != time.UTC {
		t.Fatalf("registration clock %v / heartbeat %v not a UTC read after %v", agent.RegisteredAt, agent.Heartbeat, before)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan liveness record survived the sweep: %v", err)
	}
}

func TestC6RegisterAgentRefusesLiveCollisionAndLeavesState(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) { s.Agents["coder-1"] = c6LiveAgent("coder", now) })
	before := f.bytes(t)

	_, err := RegisterAgent(f.bb, c6Register("coder", "coder-1", f.resolver))
	if !lizaerrors.IsAgentCollision(err) {
		t.Fatalf("error = %v, want an agent collision", err)
	}
	f.requireUnchanged(t, before, "collision refusal")
}

func TestC6RegisterAgentTakesOverExpiredRegistration(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		expired := now.Add(-time.Hour)
		old := c6LiveAgent("coder", now.Add(-2*time.Hour))
		old.LeaseExpires = &expired
		old.PID = 0
		s.Agents["coder-1"] = old
	})

	got, err := RegisterAgent(f.bb, c6Register("coder", "coder-1", f.resolver))
	if err != nil {
		t.Fatalf("RegisterAgent over an expired registration: %v", err)
	}
	if !got.TookOver || got.PreviousOwnership == "" {
		t.Fatalf("takeover = %v / previous %q, want a reported takeover", got.TookOver, got.PreviousOwnership)
	}
	if f.read(t).Agents["coder-1"].Generation != got.Authority.Generation || got.Authority.Generation == c6Generation {
		t.Fatal("takeover did not mint a new generation")
	}
}

func TestC6RegisterAgentEnforcesRoleCapacityAndSingularity(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		s.Config.MaxInstances = 1
		s.Agents["coder-1"] = c6LiveAgent("coder", now)
		s.Agents["orchestrator-1"] = c6LiveAgent("orchestrator", now)
	})
	before := f.bytes(t)

	_, err := RegisterAgent(f.bb, c6Register("coder", "coder-2", f.resolver))
	if err == nil || !strings.Contains(err.Error(), "role coder already has 1 live agent(s) (max 1)") || !strings.Contains(err.Error(), "coder-1:") {
		t.Fatalf("capacity error = %v", err)
	}
	_, err = RegisterAgent(f.bb, c6Register("orchestrator", "orchestrator-2", f.resolver))
	if err == nil || !strings.Contains(err.Error(), "type orchestrator already has 1 live agent(s); only 1 instance(s) allowed") {
		t.Fatalf("singularity error = %v", err)
	}
	f.requireUnchanged(t, before, "capacity refusal")

	// Without a resolver the role checks are skipped, as for unclassified callers.
	if _, err := RegisterAgent(f.bb, c6Register("coder", "coder-2", nil)); err != nil {
		t.Fatalf("nil-resolver registration: %v", err)
	}
}

func TestC6PoolSurplusAndRetireSurplusAgent(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name         string
		maxInstances int
		mutate       func(*models.State)
		want         bool
	}{
		{name: "idle at the lowered cap", maxInstances: 2, want: true},
		{name: "below the cap keeps", maxInstances: 3, want: false},
		{name: "current task keeps", maxInstances: 2, want: false, mutate: func(s *models.State) {
			task := "task-1"
			agent := s.Agents["coder-1"]
			agent.CurrentTask = &task
			s.Agents["coder-1"] = agent
		}},
		{name: "executing doer assignment keeps", maxInstances: 2, want: false, mutate: func(s *models.State) {
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now)
			s.Tasks = []models.Task{task}
		}},
		{name: "submitted attribution does not pin", maxInstances: 2, want: true, mutate: func(s *models.State) {
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
			s.Tasks = []models.Task{task}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newC6Fixture(t, func(s *models.State) {
				s.Config.MaxInstances = tc.maxInstances
				for _, id := range []string{"coder-1", "coder-2", "coder-3"} {
					s.Agents[id] = c6LiveAgent("coder", now)
				}
				if tc.mutate != nil {
					tc.mutate(s)
				}
			})
			if got := PoolSurplus(f.read(t), f.resolver, "coder-1", time.Now().UTC()); got != tc.want {
				t.Fatalf("PoolSurplus = %v, want %v", got, tc.want)
			}
			before := f.bytes(t)
			released, err := RetireSurplusAgent(f.bb, c6Authority("coder-1"), f.resolver)
			if err != nil || released != tc.want {
				t.Fatalf("RetireSurplusAgent = (%v, %v), want (%v, nil)", released, err, tc.want)
			}
			_, present := f.read(t).Agents["coder-1"]
			if present == tc.want {
				t.Fatalf("agent row present = %v after release = %v", present, tc.want)
			}
			if !tc.want {
				f.requireUnchanged(t, before, "kept registration")
			}
		})
	}
}

func TestC6RetireSurplusAgentFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		s.Config.MaxInstances = 1
		s.Agents["coder-1"] = c6LiveAgent("coder", now)
		s.Agents["coder-2"] = c6LiveAgent("coder", now)
	})
	before := f.bytes(t)
	if released, err := RetireSurplusAgent(f.bb, c6Authority("coder-1"), nil); released || err != nil {
		t.Fatalf("nil resolver = (%v, %v), want (false, nil)", released, err)
	}
	released, err := RetireSurplusAgent(f.bb, models.AgentAuthority{ID: "coder-1", Generation: "replaced"}, f.resolver)
	requireAuthorityError(t, err, "coder-1")
	if released {
		t.Fatal("a fenced-out caller retired a registration")
	}
	f.requireUnchanged(t, before, "refused retirement")
}

func TestC6OwnedPendingMergeFingerprintKeepsOwnerRegistered(t *testing.T) {
	now := time.Now().UTC()
	f := newC6Fixture(t, func(s *models.State) {
		approver := "code-reviewer-1"
		task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusApproved, now)
		task.ApprovedBy = &approver
		task.Approvals = []models.Approval{{Agent: approver, Provider: "claude", Timestamp: now}}
		task.Worktree = nil
		s.Tasks = []models.Task{task}
		s.Agents[approver] = c6LiveAgent("code-reviewer", now)
		s.Config.MaxInstances = 1
		s.Agents["code-reviewer-2"] = c6LiveAgent("code-reviewer", now)
	})
	state := f.read(t)
	if got := OwnedPendingMergeFingerprint(state, "code-reviewer-1", nil); got != "" {
		t.Fatalf("nil resolver fingerprint = %q, want empty", got)
	}
	want := "task-1@" + models.TaskTransitionID(&state.Tasks[0])
	if got := OwnedPendingMergeFingerprint(state, "code-reviewer-1", f.resolver); got != want {
		t.Fatalf("owner fingerprint = %q, want %q", got, want)
	}
	if got := OwnedPendingMergeFingerprint(state, "code-reviewer-2", f.resolver); got != "" {
		t.Fatalf("non-owner fingerprint = %q, want empty", got)
	}
	if PoolSurplus(state, f.resolver, "code-reviewer-1", time.Now().UTC()) {
		t.Fatal("an owner of a pending approved merge must not be surplus")
	}
	if !PoolSurplus(state, f.resolver, "code-reviewer-2", time.Now().UTC()) {
		t.Fatal("the non-owner at the cap should be surplus, proving the owner is kept by its merge")
	}
}
