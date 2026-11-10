package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// ADR-0245: the run-wide cap at registration and at task boundaries.

func reserve(role string, now time.Time) models.PoolReservation {
	return models.PoolReservation{Role: role, SelectedAt: now, Until: now.Add(5 * time.Minute)}
}

// actedAt records a history entry by agentID on a task, as a claim or a merge
// would.
func actedAt(s *models.State, agentID string, at time.Time) {
	task := testhelpers.BuildTaskByStatus("task-acted-"+agentID, models.TaskStatusReadyForReview, at)
	task.History = append(task.History, models.TaskHistoryEntry{Time: at, Event: models.TaskEventMerged, Agent: &agentID})
	s.Tasks = append(s.Tasks, task)
}

func TestRunCapAdmission(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		maxAgents int
		agents    map[string]string // id -> role
		reserved  []string
		expired   bool
		register  string // role
		wantErr   string
	}{
		{name: "default leaves eight non-orchestrator slots", register: "coder",
			agents: map[string]string{"orchestrator-1": "orchestrator", "coder-1": "coder", "coder-2": "coder", "coder-3": "coder",
				"code-reviewer-1": "code-reviewer", "code-reviewer-2": "code-reviewer", "code-reviewer-3": "code-reviewer", "code-planner-1": "code-planner"}},
		{name: "full run refuses", maxAgents: 3, register: "code-reviewer",
			agents:  map[string]string{"coder-1": "coder", "coder-2": "coder"},
			wantErr: "run already has 2 live non-orchestrator agent(s); max_agents 3 holds one slot for the orchestrator, leaving 2"},
		{name: "orchestrator slot is held back while it is absent", maxAgents: 2, register: "code-reviewer",
			agents:  map[string]string{"coder-1": "coder"},
			wantErr: "run already has 1 live non-orchestrator agent(s)"},
		{name: "orchestrator keeps only its singleton check", maxAgents: 2, register: "orchestrator",
			agents: map[string]string{"coder-1": "coder"}},
		{name: "reserved role takes the held slot", maxAgents: 3, register: "code-reviewer",
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"}},
		{name: "other role cannot take the held slot", maxAgents: 3, register: "coder",
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"},
			wantErr: "1 slot(s) held for role(s) code-reviewer"},
		{name: "expired reservation holds nothing", maxAgents: 3, register: "coder", expired: true,
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"}},
		{name: "reservation of an occupied role holds nothing", maxAgents: 4, register: "coder",
			agents: map[string]string{"coder-1": "coder", "code-reviewer-1": "code-reviewer"}, reserved: []string{"code-reviewer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newC6Fixture(t, func(s *models.State) {
				s.Config.MaxInstances = 5
				s.Config.MaxAgents = tc.maxAgents
				for id, role := range tc.agents {
					s.Agents[id] = c6LiveAgent(role, now)
				}
				for _, role := range tc.reserved {
					r := reserve(role, now)
					if tc.expired {
						r.Until = now.Add(-time.Second)
					}
					s.PoolReservations = append(s.PoolReservations, r)
				}
			})
			before := f.bytes(t)
			_, err := RegisterAgent(f.bb, c6Register(tc.register, tc.register+"-9", f.resolver))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("RegisterAgent: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
			f.requireUnchanged(t, before, "run-cap refusal")
		})
	}
}

func TestRunCapSurplus(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name      string
		maxAgents int
		agents    map[string]string
		reserved  []string
		acted     bool
		mutate    func(*models.State)
		agentID   string
		want      bool
	}{
		{name: "lowered cap retires a most-occupied role", maxAgents: 4, agentID: "coder-1", want: true,
			agents: map[string]string{"coder-1": "coder", "coder-2": "coder", "code-reviewer-1": "code-reviewer", "code-reviewer-2": "code-reviewer"}},
		{name: "lowered cap stops at the budget", maxAgents: 5, agentID: "coder-1", want: false,
			agents: map[string]string{"coder-1": "coder", "coder-2": "coder", "code-reviewer-1": "code-reviewer", "code-reviewer-2": "code-reviewer"}},
		{name: "lowered cap spares a smaller role", maxAgents: 3, agentID: "code-reviewer-1", want: false,
			agents: map[string]string{"coder-1": "coder", "coder-2": "coder", "code-reviewer-1": "code-reviewer"}},
		{name: "handoff after acting", maxAgents: 2, agentID: "coder-1", acted: true, want: true,
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"}},
		{name: "handoff refused before acting", maxAgents: 2, agentID: "coder-1", want: false,
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"}},
		{name: "handoff when every role is down to one", maxAgents: 3, agentID: "code-planner-1", acted: true, want: true,
			agents:   map[string]string{"coder-1": "coder", "code-planner-1": "code-planner"},
			reserved: []string{"code-reviewer"}},
		{name: "no handoff while the run has room", maxAgents: 4, agentID: "coder-1", acted: true, want: false,
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"}},
		{name: "pinned agent never retires", maxAgents: 2, agentID: "coder-1", acted: true, want: false,
			agents: map[string]string{"coder-1": "coder"}, reserved: []string{"code-reviewer"},
			mutate: func(s *models.State) {
				task := "task-1"
				agent := s.Agents["coder-1"]
				agent.CurrentTask = &task
				s.Agents["coder-1"] = agent
			}},
		{name: "orchestrator never retires", maxAgents: 2, agentID: "orchestrator-1", want: false,
			agents: map[string]string{"orchestrator-1": "orchestrator", "coder-1": "coder", "coder-2": "coder"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newC6Fixture(t, func(s *models.State) {
				s.Config.MaxInstances = 5
				s.Config.MaxAgents = tc.maxAgents
				for id, role := range tc.agents {
					s.Agents[id] = c6LiveAgent(role, now.Add(-time.Minute))
				}
				for _, role := range tc.reserved {
					s.PoolReservations = append(s.PoolReservations, reserve(role, now))
				}
				if tc.acted {
					actedAt(s, tc.agentID, now)
				}
				if tc.mutate != nil {
					tc.mutate(s)
				}
			})
			if got := PoolSurplus(f.read(t), f.resolver, tc.agentID, time.Now().UTC()); got != tc.want {
				t.Fatalf("PoolSurplus = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRunCapRetirementStopsAtTheLimit retires surplus agents one locked
// transaction at a time, as concurrent boundaries do: a lowered max_agents
// converges to its budget and no further, and a handoff gives up exactly one
// slot.
func TestRunCapRetirementStopsAtTheLimit(t *testing.T) {
	now := time.Now().UTC()
	t.Run("lowered cap", func(t *testing.T) {
		f := newC6Fixture(t, func(s *models.State) {
			s.Config.MaxInstances = 5
			s.Config.MaxAgents = 4
			for _, id := range []string{"coder-1", "coder-2", "coder-3", "code-reviewer-1", "code-reviewer-2", "code-reviewer-3"} {
				s.Agents[id] = c6LiveAgent(strings.TrimRight(id, "-0123456789"), now)
			}
		})
		retired := retireEach(t, f, "coder-1", "coder-2", "coder-3", "code-reviewer-1", "code-reviewer-2", "code-reviewer-3")
		if retired != 3 || len(f.read(t).Agents) != 3 {
			t.Fatalf("retired %d, %d left; want 3 retired and the budget of 3 left", retired, len(f.read(t).Agents))
		}
	})
	t.Run("handoff", func(t *testing.T) {
		f := newC6Fixture(t, func(s *models.State) {
			s.Config.MaxInstances = 5
			s.Config.MaxAgents = 4
			for _, id := range []string{"coder-1", "coder-2", "coder-3"} {
				s.Agents[id] = c6LiveAgent("coder", now.Add(-time.Minute))
				actedAt(s, id, now)
			}
			s.PoolReservations = []models.PoolReservation{reserve("code-reviewer", now)}
		})
		if retired := retireEach(t, f, "coder-1", "coder-2", "coder-3"); retired != 1 {
			t.Fatalf("retired %d coders for one reservation, want 1", retired)
		}
	})
}

func retireEach(t *testing.T, f *c6Fixture, ids ...string) int {
	t.Helper()
	retired := 0
	for _, id := range ids {
		released, err := RetireSurplusAgent(f.bb, c6Authority(id), f.resolver)
		if err != nil {
			t.Fatalf("RetireSurplusAgent(%s): %v", id, err)
		}
		if released {
			retired++
		}
	}
	return retired
}
