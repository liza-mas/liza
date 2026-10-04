package agent

import (
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestHeartbeatReviewLeaseRequiresMatchingProviderSession(t *testing.T) {
	for _, status := range []models.AgentStatus{models.AgentStatusReviewing, models.AgentStatusWaiting} {
		t.Run(string(status), func(t *testing.T) {
			root := t.TempDir()
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			state := testhelpers.CreateValidState()
			taskID, actor := "task-1", "code-reviewer-1"
			now := time.Now().UTC()
			lease := now.Add(time.Minute)
			taskStatus := models.TaskStatusReviewing
			if status == models.AgentStatusWaiting {
				taskStatus = models.TaskStatusRejected
			}
			state.Tasks = []models.Task{{ID: taskID, Status: taskStatus, ReviewingBy: &actor, ReviewLeaseExpires: &lease}}
			state.Agents[actor] = models.Agent{Role: models.RoleCodeReviewer, Status: status, CurrentTask: &taskID,
				Heartbeat: now, LeaseExpires: &lease, Generation: "session-generation"}
			bb := testhelpers.WriteInitialState(t, statePath, state)
			activity := &providerSessionActivity{}
			hb := NewHeartbeat(HeartbeatConfig{Authority: models.AgentAuthority{ID: actor, Generation: "session-generation"},
				StatePath: statePath, LeaseDuration: time.Hour, ActiveProviderTask: activity.currentTask})
			assertBeat := func(renewed bool) {
				t.Helper()
				before, err := bb.ReadSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				previous := *before.FindTask(taskID).ReviewLeaseExpires
				if err := hb.beat(); err != nil {
					t.Fatal(err)
				}
				after, err := bb.ReadSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				current := *after.FindTask(taskID).ReviewLeaseExpires
				if renewed && !current.After(previous) {
					t.Fatal("running session did not renew review lease")
				}
				if !renewed && !current.Equal(previous) {
					t.Fatal("absent or different session renewed review lease")
				}
				if !after.Agents[actor].LeaseExpires.After(lease) {
					t.Fatal("supervisor liveness lease was not renewed")
				}
			}
			assertBeat(false)
			activity.start("other-task")
			assertBeat(false)
			activity.start(taskID)
			assertBeat(true)
			activity.stop()
			assertBeat(false)
		})
	}
}
