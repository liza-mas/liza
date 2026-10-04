package agent

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestExitResetOutlastsLockTimeoutAndPreservesWorkCycle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	state := testhelpers.CreateValidState()
	task := testhelpers.BuildTaskByStatus("interrupted", models.TaskStatusImplementing, time.Now().UTC())
	task.Iteration = 3
	task.AssignedTo = testhelpers.StringPtr("coder-1")
	state.Tasks = []models.Task{task}
	state.Agents["coder-1"] = models.Agent{
		Role: "coder", Status: models.AgentStatusWorking,
		CurrentTask: &task.ID, Heartbeat: time.Now().UTC(),
	}
	bb := testhelpers.WriteInitialState(t, path, state).WithLockTimeout(50 * time.Millisecond)
	authority := testAgentAuthority(t, bb, "coder-1")
	release := testhelpers.HoldFileLock(t, path)
	time.AfterFunc(350*time.Millisecond, release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := resetAgentAfterExit(bb.WithLockRetryContext(ctx), authority, root); err != nil {
		t.Fatalf("reset abandoned after contention: %v", err)
	}
	got, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	owned := got.FindTask(task.ID)
	if owned.AssignedTo != nil || owned.Status != models.TaskStatusReady || !owned.Continuation || owned.Iteration != 3 {
		t.Fatalf("interrupted work cycle lost: status=%s assigned=%v continuation=%v iteration=%d", owned.Status, owned.AssignedTo, owned.Continuation, owned.Iteration)
	}
	if got.Agents[authority.ID].Status != models.AgentStatusIdle || got.Agents[authority.ID].CurrentTask != nil {
		t.Fatal("reset left stale agent ownership")
	}
	if len(owned.History) != len(task.History)+1 {
		t.Fatal("reset did not publish exactly one release")
	}
}

func TestRuntimeInputDiscoveryUnderContentionStillScrubs(t *testing.T) {
	// Not parallel: shorten newly-created singleton lock waits to expose the
	// old pre-launch Read failure without a ten-second test.
	t.Cleanup(db.SetDefaultLockTimeoutForTest(50 * time.Millisecond))
	root := t.TempDir()
	path, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{{ID: "fixture-task", RuntimeInputs: []models.RuntimeInput{{Env: []string{"FIXTURE_VALUE"}}}}}
	testhelpers.WriteInitialState(t, path, state)
	testhelpers.HoldFileLock(t, path)
	got, err := scrubRuntimeInputNames(root, []string{"FIXTURE_VALUE=private-fixture", "KEEP=ordinary"})
	if err != nil || !reflect.DeepEqual(got, []string{"KEEP=ordinary"}) {
		t.Fatalf("contention stopped launch discovery or bypassed scrub: env=%v err=%v", got, err)
	}
	// A corrupt publication must still refuse launch, rather than fail open.
	if err := os.WriteFile(path, []byte("tasks: [invalid yaml"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := scrubRuntimeInputNames(root, []string{"FIXTURE_VALUE=private-fixture"}); err == nil {
		t.Fatal("unreadable declarations allowed an unscrubbed session")
	}
}

func TestExecuteAgentRetriesOnlyContentionBeforeProviderStart(t *testing.T) {
	for _, beforeStart := range []bool{true, false} {
		name := "after_start"
		if beforeStart {
			name = "before_start"
		}
		t.Run(name, func(t *testing.T) {
			config, _, runtime := reviewExecutionFixture(t)
			inner := filelock.NewLockTimeout(errors.New("external boundary contention"))
			provider := &reviewExecutionProvider{run: func(context.Context) error { return inner }}
			if beforeStart {
				provider.beforeLaunch = func() error { return inner }
			}
			config.LLMAgent = provider
			_, _, err := executeAgent(context.Background(), config, "review", nil, "review-task", runtime)
			if !errors.Is(err, inner) || errors.Is(err, errLaunchContended) != beforeStart {
				t.Fatalf("beforeStart=%v err=%v, want contention retry only before actual start", beforeStart, err)
			}
		})
	}
}
