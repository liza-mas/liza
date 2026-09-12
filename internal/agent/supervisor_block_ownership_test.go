package agent

import (
	"reflect"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestSupervisorBlockReleasesAllTaskOwners(t *testing.T) {
	for _, reviewing := range []bool{false, true} {
		name := "doer"
		if reviewing {
			name = "reviewer"
		}
		t.Run(name, func(t *testing.T) {
			bb, root, taskID, doerID := buildPromptFailureFixture(t, "main")
			reviewerID := "architecture-reviewer-1"
			passiveID := "architecture-reviewer-2"
			unrelatedID := "architect-2"
			worktree, base, review := ".worktrees/"+taskID, "base-commit", "review-commit"
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask(taskID)
				task.Worktree, task.BaseCommit, task.ReviewCommit = &worktree, &base, &review
				task.ReviewingBy, task.ReviewLeaseExpires = &reviewerID, task.LeaseExpires
				if reviewing {
					task.Status = "REVIEWING_ARCHITECTURE"
				}
				doer := testhelpers.RegisteredTestAgent("architect")
				doer.Status = models.AgentStatusWorking
				doer.CurrentTask, doer.LeaseExpires = &taskID, task.LeaseExpires
				state.Agents[doerID] = doer
				for _, id := range []string{reviewerID, passiveID} {
					agent := testhelpers.RegisteredTestAgent("architecture-reviewer")
					agent.Status = models.AgentStatusWaiting
					agent.CurrentTask, agent.LeaseExpires = &taskID, task.LeaseExpires
					if reviewing && id == reviewerID {
						agent.Status = models.AgentStatusReviewing
					}
					state.Agents[id] = agent
				}
				state.Agents[unrelatedID] = testhelpers.RegisteredTestAgent("architect")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			caller := doerID
			if reviewing {
				caller = reviewerID
			}
			authority := testSupervisorAuthority(t, bb, caller)
			before, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if err := blockTaskFromSupervisor(bb, root, taskID, authority, "prompt context build failed"); err != nil {
				t.Fatal(err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{doerID, reviewerID, passiveID} {
				owner := after.Agents[id]
				if owner.Status != models.AgentStatusIdle || owner.CurrentTask != nil || owner.LeaseExpires != nil {
					t.Errorf("linked agent %s not released: status=%s current_task=%v lease=%v", id, owner.Status, owner.CurrentTask, owner.LeaseExpires)
				}
			}
			if !reflect.DeepEqual(after.Agents[unrelatedID], before.Agents[unrelatedID]) {
				t.Error("blocking changed an unrelated agent")
			}
			task := after.FindTask(taskID)
			if task.Status != models.TaskStatusBlocked || task.AssignedTo != nil || task.LeaseExpires != nil || task.ReviewingBy != nil || task.ReviewLeaseExpires != nil {
				t.Error("blocking did not clear both task ownership slots")
			}
			original := before.FindTask(taskID)
			// Supervisor blocking already deletes the worktree; commit attribution remains.
			if task.Worktree != nil || !reflect.DeepEqual(task.BaseCommit, original.BaseCommit) || !reflect.DeepEqual(task.ReviewCommit, original.ReviewCommit) {
				t.Error("blocking changed established worktree cleanup or commit attribution")
			}
			if err := statevalidate.ValidateState(after, root, true, nil); err != nil {
				t.Errorf("blocked state is invalid before any subsequent supervisor poll: %v", err)
			}
		})
	}
}
