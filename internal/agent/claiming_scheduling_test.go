package agent

import (
	"fmt"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestClaimDoerScheduling(t *testing.T) {
	for _, scenario := range []string{"owned rejection", "dependency provider"} {
		t.Run(scenario, func(t *testing.T) {
			// The baseline uniformly shuffles candidates. Every opportunity must
			// favor rework/providers; eight independent claims expose that gap.
			for attempt := 0; attempt < 8; attempt++ {
				t.Run(fmt.Sprint(attempt), func(t *testing.T) {
					root := t.TempDir()
					testhelpers.SetupTestGitRepo(t, root)
					statePath, _ := testhelpers.SetupLizaDir(t, root)
					testhelpers.SetupPipelineConfig(t, root)
					now := time.Now().UTC()
					agentID := "coder-1"
					state := testhelpers.CreateValidState()
					state.Agents[agentID] = testhelpers.RegisteredTestAgent(models.RoleCoder)
					wanted := testhelpers.BuildTaskByStatus("wanted", models.TaskStatusReady, now)
					if scenario == "owned rejection" {
						testhelpers.CreateTestWorktree(t, root, "wanted")
						base, err := git.New(root).GetCommitSHA("HEAD")
						if err != nil {
							t.Fatal(err)
						}
						wanted = testhelpers.BuildTaskByStatus("wanted", models.TaskStatusRejected, now)
						wanted.AssignedTo = &agentID
						wanted.BaseCommit = &base
						wanted.ReviewCommit = &base
					} else {
						consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusReady, now)
						consumer.DependsOn = []string{wanted.ID}
						state.Tasks = append(state.Tasks, consumer)
					}
					for leaf := 0; leaf < 8; leaf++ {
						state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus(fmt.Sprintf("leaf-%d", leaf), models.TaskStatusReady, now))
					}
					state.Tasks = append(state.Tasks, wanted)
					bb := testhelpers.WriteInitialState(t, statePath, state)

					claimed, _, err := claimCoderTask(root, agentID, bb)
					if err != nil {
						t.Fatalf("claim: %v", err)
					}
					if claimed != wanted.ID {
						t.Fatalf("claimed %q ahead of %s %q at equal priority", claimed, scenario, wanted.ID)
					}
					updated, err := bb.ReadSnapshot()
					if err != nil {
						t.Fatal(err)
					}
					got := updated.FindTask(wanted.ID)
					if got.Status != models.TaskStatusImplementing || got.Iteration != wanted.Iteration+1 {
						t.Fatalf("reclaim bypassed execution transition/iteration: status=%s iteration=%d", got.Status, got.Iteration)
					}
					if scenario == "owned rejection" {
						found := false
						for _, entry := range got.History {
							found = found || entry.Event == models.TaskEventReclaimedAfterRejection
						}
						if !found {
							t.Fatal("owned rejection did not use the rejection reclaim transaction")
						}
					}
				})
			}
		})
	}
}

func TestClaimDoerScheduling_FallsBackAfterReworkExhaustion(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	testhelpers.CreateTestWorktree(t, root, "exhausted")
	base, err := git.New(root).GetCommitSHA("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Config.MaxCoderIterations = 1
	state.Agents["coder-1"] = testhelpers.RegisteredTestAgent(models.RoleCoder)
	rework := testhelpers.BuildTaskByStatus("exhausted", models.TaskStatusRejected, now)
	rework.AssignedTo, rework.LeaseExpires = nil, nil
	rework.BaseCommit, rework.ReviewCommit, rework.Attempt = &base, &base, 2
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("fresh", models.TaskStatusReady, now), rework}
	bb := testhelpers.WriteInitialState(t, statePath, state)

	claimed, _, err := claimCoderTask(root, "coder-1", bb)
	if err != nil || claimed != "fresh" {
		t.Fatalf("claim = %q, %v; want fresh fallback after rejected candidate fails", claimed, err)
	}
	updated, err := bb.ReadSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.FindTask("exhausted"); got.Status != models.TaskStatusBlocked || got.Iteration != rework.Iteration {
		t.Fatalf("preferred rework was not attempted through its limit gate: status=%s iteration=%d", got.Status, got.Iteration)
	}
}
