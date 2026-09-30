package commands

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// deadPID is a PID no test process holds; leased rows with it still occupy
// capacity under the lease-first ownership rule.
const deadPID = 999999999

func poolDoer(now time.Time, pid int, currentTask string) models.Agent {
	agent := models.Agent{
		Role:         "coder",
		Status:       models.AgentStatusIdle,
		Provider:     "claude",
		PID:          pid,
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: testhelpers.TimePtr(now.Add(30 * time.Minute)),
	}
	if currentTask != "" {
		agent.Status = models.AgentStatusWorking
		agent.CurrentTask = testhelpers.StringPtr(currentTask)
	}
	return agent
}

func readyTasks(now time.Time, ids ...string) []models.Task {
	tasks := make([]models.Task, 0, len(ids))
	for _, id := range ids {
		tasks = append(tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusReady, now))
	}
	return tasks
}

// maxInstancesOverride gives one role its own max-instances, as a pipeline
// role definition would.
type maxInstancesOverride struct {
	models.PipelineResolver
	role string
	max  int
}

func (r maxInstancesOverride) MaxInstances(role string) (int, error) {
	if role == r.role {
		return r.max, nil
	}
	return 0, nil
}

func loadRepairResolver(t *testing.T) models.PipelineResolver {
	t.Helper()
	root := writeRepairAgentPoolState(t, testhelpers.CreateValidState())
	pr, err := ops.LoadResolverForModels(root)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}
	return pr
}

func coderDeficit(t *testing.T, missing []MissingRoleWork) (MissingRoleWork, bool) {
	t.Helper()
	for _, roleWork := range missing {
		if roleWork.Role == "coder" {
			return roleWork, true
		}
	}
	return MissingRoleWork{}, false
}

func TestFindRoleCapacityDeficits_DoerDemandWithinCap(t *testing.T) {
	now := time.Now().UTC()
	pr := loadRepairResolver(t)

	tests := []struct {
		name         string
		tasks        []string
		agents       map[string]models.Agent
		maxInstances int
		wantSpawn    int // 0 means the role is not reported
		wantTasks    int
	}{
		{name: "no agents spawns one per task", tasks: []string{"t1", "t2", "t3"}, wantSpawn: 3, wantTasks: 3},
		{name: "default cap bounds spawns", tasks: []string{"t1", "t2", "t3", "t4"}, wantSpawn: 3, wantTasks: 4},
		{
			name:      "idle claim-valid doer covers one task",
			tasks:     []string{"t1", "t2", "t3"},
			agents:    map[string]models.Agent{"coder-1": poolDoer(now, os.Getpid(), "")},
			wantSpawn: 2, wantTasks: 2,
		},
		{
			name:      "busy doer covers nothing but occupies a slot",
			tasks:     []string{"t1", "t2", "t3"},
			agents:    map[string]models.Agent{"coder-1": poolDoer(now, os.Getpid(), "other")},
			wantSpawn: 2, wantTasks: 3,
		},
		{
			name:  "full role starts nothing",
			tasks: []string{"t1", "t2", "t3", "t4", "t5"},
			agents: map[string]models.Agent{
				"coder-1": poolDoer(now, os.Getpid(), ""),
				"coder-2": poolDoer(now, os.Getpid(), "other-a"),
				"coder-3": poolDoer(now, os.Getpid(), "other-b"),
			},
		},
		{
			name:      "leased doer with a dead process occupies but does not cover",
			tasks:     []string{"t1"},
			agents:    map[string]models.Agent{"coder-1": poolDoer(now, deadPID, "")},
			wantSpawn: 1, wantTasks: 1,
		},
		{
			name:         "project default below the built-in cap",
			tasks:        []string{"t1", "t2"},
			agents:       map[string]models.Agent{"coder-1": poolDoer(now, os.Getpid(), "other")},
			maxInstances: 1,
		},
		{name: "project default above the built-in cap", tasks: []string{"t1", "t2", "t3", "t4", "t5"}, maxInstances: 4, wantSpawn: 4, wantTasks: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// GIVEN
			state := testhelpers.CreateValidState()
			state.Config.MaxInstances = tt.maxInstances
			state.Tasks = readyTasks(now, tt.tasks...)
			state.Agents = map[string]models.Agent{}
			for id, agent := range tt.agents {
				state.Agents[id] = agent
			}

			// WHEN
			missing, unservable := FindRoleCapacityDeficits(state, pr, RepairCLI{Explicit: "claude"}, nil, now)

			// THEN
			if len(unservable) != 0 {
				t.Fatalf("unservable = %+v, want none for doer work", unservable)
			}
			got, ok := coderDeficit(t, missing)
			if tt.wantSpawn == 0 {
				if ok {
					t.Fatalf("coder deficit = %+v, want none", got)
				}
				return
			}
			if !ok {
				t.Fatalf("missing = %+v, want a coder deficit", missing)
			}
			if got.SpawnCount != tt.wantSpawn || got.TaskCount != tt.wantTasks {
				t.Fatalf("coder deficit = %+v, want spawn %d for %d uncovered task(s)", got, tt.wantSpawn, tt.wantTasks)
			}
		})
	}
}

func TestFindRoleCapacityDeficits_RoleMaxInstancesOverridesProjectDefault(t *testing.T) {
	// GIVEN a project default of 5 and a coder role capped at 2
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxInstances = 5
	state.Tasks = readyTasks(now, "t1", "t2", "t3", "t4")
	state.Agents = map[string]models.Agent{}
	pr := maxInstancesOverride{PipelineResolver: loadRepairResolver(t), role: "coder", max: 2}

	// WHEN
	missing, _ := FindRoleCapacityDeficits(state, pr, RepairCLI{Explicit: "claude"}, nil, now)

	// THEN
	got, ok := coderDeficit(t, missing)
	if !ok || got.SpawnCount != 2 {
		t.Fatalf("coder deficit = %+v (found %v), want spawn 2 from the role cap", got, ok)
	}
}

func TestFindRoleCapacityDeficits_FailedValidationIsNotDemand(t *testing.T) {
	// GIVEN an idle coder whose session failed this task's validation and a
	// second ready task it can take
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	failed := testhelpers.BuildTaskByStatus("failed-task", models.TaskStatusReady, now)
	failed.Validation = []string{"canonical check"}
	failed.ValidationPrerequisites = []models.ValidationPrerequisite{{Command: "canonical check", Env: []string{"REQUIRED"}}}
	state.Tasks = []models.Task{failed, testhelpers.BuildTaskByStatus("other-task", models.TaskStatusReady, now)}
	agent := poolDoer(now, os.Getpid(), "")
	agent.Generation = "generation-current"
	state.Agents = map[string]models.Agent{"coder-1": agent}
	state.ValidationReadiness = map[string]map[string]models.ValidationReadiness{"coder-1": {failed.ID: {
		TaskID: failed.ID, Generation: agent.Generation, Commit: "worktree-head",
		Digest:    models.ValidationPrerequisiteDigest(failed.Validation, failed.ValidationPrerequisites),
		CheckedAt: now, Result: "failed", Code: "missing_env",
	}}}

	// WHEN
	missing, _ := FindRoleCapacityDeficits(state, loadRepairResolver(t), RepairCLI{Explicit: "claude"}, nil, now)

	// THEN the failed task starts no replacement and the idle coder covers the other
	if got, ok := coderDeficit(t, missing); ok {
		t.Fatalf("coder deficit = %+v, want none", got)
	}
}

func TestRepairAgentPool_PendingSpawnsCoverDemand(t *testing.T) {
	// GIVEN three uncovered tasks and two started-but-unregistered coders
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = readyTasks(now, "t1", "t2", "t3")
	root := writeRepairAgentPoolState(t, state)
	var calls []spawnedAgentCall
	withFakeRepairSpawner(t, &calls, nil)

	// WHEN
	result, err := RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, CLI: "claude", PendingSpawns: map[string]int{"coder": 2}})

	// THEN only the remaining gap is started
	if err != nil {
		t.Fatalf("RepairAgentPool() error = %v", err)
	}
	if len(calls) != 1 || calls[0].role != "coder" {
		t.Fatalf("spawn calls = %+v, want one coder", calls)
	}
	if got, ok := coderDeficit(t, result.Missing); !ok || got.SpawnCount != 1 {
		t.Fatalf("coder deficit = %+v, want spawn 1 after pending", got)
	}

	// WHEN pending covers everything
	calls = nil
	result, err = RepairAgentPool(RepairAgentPoolOptions{ProjectRoot: root, CLI: "claude", PendingSpawns: map[string]int{"coder": 3}})

	// THEN nothing starts
	if err != nil || len(calls) != 0 || slices.ContainsFunc(result.Missing, func(w MissingRoleWork) bool { return w.Role == "coder" }) {
		t.Fatalf("err=%v calls=%+v missing=%+v, want no coder start", err, calls, result.Missing)
	}
}
