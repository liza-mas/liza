package agent

import (
	"context"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// Drive each real supervisor strategy entrypoint. The detector observes the
// post-recovery snapshot, and no provider wake is selected for the retired hold.
func TestStateLockHoldOrchestratorMechanicalRecovery(t *testing.T) {
	for _, entrypoint := range []string{"prework", "idle wait"} {
		t.Run(entrypoint, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupPipelineConfig(t, root)
			bb := newOrchestratorScipTestBlackboard(t, root, func(state *models.State) {
				task := testhelpers.BuildTaskByStatus("hold", models.TaskStatusBlocked, time.Now().UTC())
				task.RolePair = "coding-pair"
				task.AssignedTo, task.LeaseExpires, task.Worktree, task.BaseCommit = nil, nil, nil, nil
				reason := "state lock timeout"
				task.BlockedReason = &reason
				task.BlockedQuestions = []string{"Resume after a healthy publication?"}
				task.History = []models.TaskHistoryEntry{{Time: task.Created, Event: models.TaskEventBlocked, Reason: &reason}}
				task.StateLockHold = &models.StateLockHold{EpisodeAt: task.Created, BlockerDigest: models.StateLockBlockerDigest(&task), AfterSequence: 42}
				state.MutationSequence = 43
				state.Tasks = []models.Task{task}
			})
			strategy := &orchestratorStrategy{}
			config := orchestratorScipConfig(root)
			if entrypoint == "prework" {
				if _, err := strategy.PreWork(context.Background(), bb, config); err != nil {
					t.Fatal(err)
				}
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				prior := orchestratorWaitForWorkDetector
				t.Cleanup(func() { orchestratorWaitForWorkDetector = prior })
				observed := false
				orchestratorWaitForWorkDetector = func(_ string, state *models.State, _ []models.TaskStatus, _ map[string]bool, _ []ops.ManyToOneTransitionInfo) OrchestratorWakeResult {
					observed = true
					if state.FindTask("hold").Status != models.TaskStatusReady || ops.CountActionableBlockedTasks(state) != 0 {
						t.Error("detector received unrecovered contention hold")
					}
					cancel()
					return OrchestratorWakeResult{Trigger: WakeTriggerNone}
				}
				if work, _ := strategy.WaitForWork(ctx, bb, config, time.Millisecond, time.Second); work {
					t.Fatal("mechanical recovery selected a provider wake")
				}
				if !observed {
					t.Fatal("idle strategy skipped detector after recovery")
				}
			}
			if task := mustReadState(t, bb).FindTask("hold"); task.Status != models.TaskStatusReady || task.StateLockHold != nil || !task.Continuation {
				t.Fatalf("strategy did not mechanically resume hold: %+v", task)
			}
		})
	}
}
