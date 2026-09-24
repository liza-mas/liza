package commands

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// orchestratorTestResolver answers only the role-typing questions the
// orchestrator presence check asks; any other resolver call panics.
type orchestratorTestResolver struct {
	models.PipelineResolver
	roleTypes map[string]string
}

func (r orchestratorTestResolver) RoleType(role string) (string, error) {
	roleType, ok := r.roleTypes[role]
	if !ok {
		return "", fmt.Errorf("unknown role %q", role)
	}
	return roleType, nil
}

func (r orchestratorTestResolver) AllRoleNames() []string {
	names := make([]string, 0, len(r.roleTypes))
	for name := range r.roleTypes {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func defaultOrchestratorTestResolver() orchestratorTestResolver {
	return orchestratorTestResolver{roleTypes: map[string]string{
		"orchestrator":  "orchestrator",
		"coder":         "doer",
		"code-reviewer": "reviewer",
	}}
}

func orchestratorTestState(agents map[string]models.Agent) *models.State {
	state := testhelpers.CreateValidState()
	state.Agents = agents
	return state
}

func occupiedAgent(role string, now time.Time, pid int) models.Agent {
	return models.Agent{
		Role:         role,
		Status:       models.AgentStatusIdle,
		LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
		Heartbeat:    now,
		PID:          pid,
	}
}

func isolateOrchestratorProcfs(t *testing.T) string {
	t.Helper()
	procRoot := t.TempDir()
	t.Cleanup(ops.SetAgentProcessProcRootForTest(procRoot))
	return procRoot
}

func TestCheckMissingOrchestrator_OnceAfterGracePerEpisode(t *testing.T) {
	isolateOrchestratorProcfs(t)
	pr := defaultOrchestratorTestResolver()
	cache := make(map[string]time.Time)
	t0 := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	absent := orchestratorTestState(nil)

	steps := []struct {
		name  string
		state *models.State
		now   time.Time
		want  int
	}{
		{"absence first observed", absent, t0, 0},
		{"within grace", absent, t0.Add(59 * time.Second), 0},
		{"grace elapsed", absent, t0.Add(60 * time.Second), 1},
		{"continued absence", absent, t0.Add(5 * time.Minute), 0},
		{"orchestrator back", orchestratorTestState(map[string]models.Agent{
			"orchestrator-1": occupiedAgent("orchestrator", t0.Add(6*time.Minute), 987654321),
		}), t0.Add(6 * time.Minute), 0},
		{"new absence first observed", absent, t0.Add(7 * time.Minute), 0},
		{"new absence within grace", absent, t0.Add(7*time.Minute + 59*time.Second), 0},
		{"new absence grace elapsed", absent, t0.Add(8 * time.Minute), 1},
	}
	for _, step := range steps {
		alerts := checkMissingOrchestrator(step.state, pr, cache, step.now)
		if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != step.want {
			t.Fatalf("%s: ORCHESTRATOR MISSING alerts = %d, want %d; alerts: %v", step.name, got, step.want, alerts)
		}
		if step.want == 1 && alerts[0].Level != AlertLevelCritical {
			t.Fatalf("%s: Level = %q, want %q", step.name, alerts[0].Level, AlertLevelCritical)
		}
	}
}

func TestCheckMissingOrchestrator_LeaseFirstPresence(t *testing.T) {
	now := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	tests := []struct {
		name       string
		agent      models.Agent
		argv       []string
		wantAlerts int
		wantInMsg  []string
	}{
		{
			name:       "fresh lease with dead pid keeps effective ownership",
			agent:      occupiedAgent("orchestrator", now, 987654321),
			wantAlerts: 0,
		},
		{
			name:       "fresh lease with mismatched pid keeps effective ownership",
			agent:      occupiedAgent("orchestrator", now, 1234),
			argv:       []string{"go", "test"},
			wantAlerts: 0,
		},
		{
			name: "expired lease is absent",
			agent: models.Agent{
				Role:         "orchestrator",
				Status:       models.AgentStatusIdle,
				LeaseExpires: testhelpers.TimePtr(now.Add(-3 * time.Minute)),
				Heartbeat:    now.Add(-31 * time.Minute),
				PID:          987654321,
			},
			wantAlerts: 1,
			wantInMsg:  []string{"orchestrator-1", string(ops.AgentOwnershipLeaseExpiredOrStale)},
		},
		{
			name: "fresh lease without heartbeat is absent",
			agent: models.Agent{
				Role:         "orchestrator",
				Status:       models.AgentStatusIdle,
				LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
				PID:          987654321,
			},
			wantAlerts: 1,
			wantInMsg:  []string{"orchestrator-1", string(ops.AgentOwnershipLeaseExpiredOrStale)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			procRoot := isolateOrchestratorProcfs(t)
			if len(tt.argv) > 0 {
				writeWatchProcCmdline(t, procRoot, tt.agent.PID, tt.argv)
			}
			pr := defaultOrchestratorTestResolver()
			cache := make(map[string]time.Time)
			state := orchestratorTestState(map[string]models.Agent{"orchestrator-1": tt.agent})

			checkMissingOrchestrator(state, pr, cache, now.Add(-2*time.Minute))
			alerts := checkMissingOrchestrator(state, pr, cache, now)
			if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != tt.wantAlerts {
				t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want %d; alerts: %v", got, tt.wantAlerts, alerts)
			}
			for _, want := range tt.wantInMsg {
				if !strings.Contains(alerts[0].Message, want) {
					t.Errorf("Message = %q, want it to contain %q", alerts[0].Message, want)
				}
			}
		})
	}
}

func TestCheckMissingOrchestrator_RecognisesOrchestratorByType(t *testing.T) {
	isolateOrchestratorProcfs(t)
	now := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	pr := orchestratorTestResolver{roleTypes: map[string]string{
		"conductor": "orchestrator",
		"coder":     "doer",
	}}

	t.Run("custom orchestrator role holding ownership is present", func(t *testing.T) {
		cache := make(map[string]time.Time)
		state := orchestratorTestState(map[string]models.Agent{
			"conductor-1": occupiedAgent("conductor", now, 987654321),
		})
		checkMissingOrchestrator(state, pr, cache, now.Add(-2*time.Minute))
		if alerts := checkMissingOrchestrator(state, pr, cache, now); len(alerts) != 0 {
			t.Fatalf("alerts = %v, want none", alerts)
		}
	})

	t.Run("recovery guidance names the configured role", func(t *testing.T) {
		cache := make(map[string]time.Time)
		state := orchestratorTestState(map[string]models.Agent{
			"coder-1": occupiedAgent("coder", now, 987654321),
		})
		checkMissingOrchestrator(state, pr, cache, now.Add(-2*time.Minute))
		alerts := checkMissingOrchestrator(state, pr, cache, now)
		if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != 1 {
			t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want 1; alerts: %v", got, alerts)
		}
		if want := brand.Command("agent", "conductor"); !strings.Contains(alerts[0].Message, want) {
			t.Errorf("Message = %q, want recovery command %q", alerts[0].Message, want)
		}
	})
}

func TestCheckMissingOrchestrator_DefaultRecoveryCommand(t *testing.T) {
	isolateOrchestratorProcfs(t)
	now := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	pr := defaultOrchestratorTestResolver()
	cache := make(map[string]time.Time)
	state := orchestratorTestState(nil)

	checkMissingOrchestrator(state, pr, cache, now.Add(-2*time.Minute))
	alerts := checkMissingOrchestrator(state, pr, cache, now)
	if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != 1 {
		t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want 1; alerts: %v", got, alerts)
	}
	if want := brand.Command("agent", "orchestrator"); !strings.Contains(alerts[0].Message, want) {
		t.Errorf("Message = %q, want recovery command %q", alerts[0].Message, want)
	}
}

func TestCheckMissingOrchestrator_RequirementCeasedEndsEpisode(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*models.State)
		resolve orchestratorTestResolver
	}{
		{
			name:    "goal completed",
			mutate:  func(s *models.State) { s.Goal.Status = models.GoalStatusCompleted },
			resolve: defaultOrchestratorTestResolver(),
		},
		{
			name:    "system paused",
			mutate:  func(s *models.State) { s.Config.Mode = models.SystemModePaused },
			resolve: defaultOrchestratorTestResolver(),
		},
		{
			name:    "pipeline declares no orchestrator-type role",
			mutate:  func(*models.State) {},
			resolve: orchestratorTestResolver{roleTypes: map[string]string{"coder": "doer"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateOrchestratorProcfs(t)
			pr := defaultOrchestratorTestResolver()
			cache := make(map[string]time.Time)
			t0 := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
			required := orchestratorTestState(nil)
			ceased := orchestratorTestState(nil)
			tt.mutate(ceased)

			checkMissingOrchestrator(required, pr, cache, t0)
			if alerts := checkMissingOrchestrator(ceased, tt.resolve, cache, t0.Add(30*time.Second)); len(alerts) != 0 {
				t.Fatalf("alerts while not required = %v, want none", alerts)
			}
			if alerts := checkMissingOrchestrator(required, pr, cache, t0.Add(61*time.Second)); len(alerts) != 0 {
				t.Fatalf("alerts right after requirement returned = %v, want none (grace restarts)", alerts)
			}
			alerts := checkMissingOrchestrator(required, pr, cache, t0.Add(121*time.Second))
			if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != 1 {
				t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want 1 after a fresh grace; alerts: %v", got, alerts)
			}
		})
	}
}

func TestCheckMissingOrchestrator_UnloadablePipelinePreservesEpisode(t *testing.T) {
	isolateOrchestratorProcfs(t)
	pr := defaultOrchestratorTestResolver()
	cache := make(map[string]time.Time)
	t0 := time.Date(2026, 9, 24, 4, 4, 18, 0, time.UTC)
	absent := orchestratorTestState(nil)

	checkMissingOrchestrator(absent, pr, cache, t0)
	if alerts := checkMissingOrchestrator(absent, nil, cache, t0.Add(30*time.Second)); len(alerts) != 0 {
		t.Fatalf("alerts without a pipeline resolver = %v, want none", alerts)
	}
	alerts := checkMissingOrchestrator(absent, pr, cache, t0.Add(61*time.Second))
	if got := countAlertsByCategory(alerts, "ORCHESTRATOR MISSING"); got != 1 {
		t.Fatalf("ORCHESTRATOR MISSING alerts = %d, want 1 (episode kept its start); alerts: %v", got, alerts)
	}
}
