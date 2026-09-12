package ops

import (
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestDeleteTaskQuarantinedVerdictsPreservesUnrelatedLifecycle(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusRejected, models.TaskStatusBlocked, models.TaskStatusMerged} {
		t.Run(string(status), func(t *testing.T) {
			const remainingID = "remaining-review"
			const deletedID = "deleted-evidence"
			root, statePath := setupMergeTestRepo(t, remainingID, "coder-1")
			f := quarantineFixture{
				root: root, statePath: statePath, taskID: remainingID,
				commit:       *readStateForTest(t, statePath).FindTask(remainingID).ReviewCommit,
				current:      models.AgentAuthority{ID: "code-reviewer-1", Generation: testhelpers.TestAgentGeneration},
				stale:        models.AgentAuthority{ID: "code-reviewer-1", Generation: "deleted-task-old-reviewer"},
				orchestrator: models.AgentAuthority{ID: "orchestrator-1", Generation: testhelpers.TestAgentGeneration},
			}
			f.mutate(t, func(state *models.State) {
				now := time.Now().UTC()
				registerClaimTaskTestAgents(state)
				reviewer := testhelpers.RegisteredTestAgent("code-reviewer")
				reviewer.Status = models.AgentStatusReviewing
				reviewer.CurrentTask = &f.taskID
				state.Agents[f.current.ID] = reviewer
				state.Agents[f.orchestrator.ID] = testhelpers.RegisteredTestAgent("orchestrator")
				task := state.FindTask(remainingID)
				task.Status = models.TaskStatusReviewing
				task.ApprovedBy = nil
				task.ReviewingBy = &f.current.ID
				lease := now.Add(time.Hour)
				task.ReviewLeaseExpires = &lease
				task.HandoffEvents = []models.HandoffEvent{{Timestamp: now, Agent: "coder-1", Trigger: models.HandoffTriggerSubmission}}
				deleted := testhelpers.BuildTaskByStatus(deletedID, status, now)
				deleted.ReviewCommit = &f.commit
				state.Tasks = append(state.Tasks, deleted, testhelpers.BuildTaskByStatus("remaining-ready", models.TaskStatusReady, now))
			})

			// Persist real fenced evidence and reconciliation audit for a different task.
			finding := f.capture(t, "REJECTED", "retained independent finding", f.commit)
			if err := ReconcileVerdict(root, remainingID, finding.ID, "refuted", "verified against the reviewed artifact", f.orchestrator); err != nil {
				t.Fatal(err)
			}
			retained := f.read(t).QuarantinedVerdicts[0]
			deletedFixture := f
			deletedFixture.taskID = deletedID
			deletedFixture.capture(t, "REJECTED", "deleted task matched finding", f.commit)
			deletedFixture.capture(t, "REJECTED", "deleted task unmatched finding", strings.Repeat("d", 40))

			result, err := DeleteTask(root, deletedID, status == models.TaskStatusMerged, false, "remove obsolete task and its evidence")
			if err != nil {
				t.Fatalf("DeleteTask: %v", err)
			}
			if result.PreviousStatus != status {
				t.Fatalf("deleted status = %s, want %s", result.PreviousStatus, status)
			}
			after := f.read(t)
			if after.FindTask(deletedID) != nil {
				t.Fatal("deleted task remains")
			}
			if !reflect.DeepEqual(after.QuarantinedVerdicts, []models.QuarantinedVerdict{retained}) {
				t.Fatal("deletion retained orphaned findings or changed unrelated evidence/audit")
			}
			if err := statevalidate.ValidateState(after, root, true, io.Discard); err != nil {
				t.Fatalf("deletion left invalid state: %v", err)
			}

			if _, err := ClaimTask(root, "remaining-ready", "coder-2"); err != nil {
				t.Fatalf("unrelated claim after deletion: %v", err)
			}
			if _, err := SubmitVerdictWithAuthority(root, remainingID, "APPROVED", "", f.current, "", f.commit); err != nil {
				t.Fatalf("unrelated approval after deletion: %v", err)
			}
			if _, err := MergeWorktree(root, remainingID, "coder-1"); err != nil {
				t.Fatalf("unrelated merge after deletion: %v", err)
			}
			after = f.read(t)
			if after.FindTask("remaining-ready").Status != models.TaskStatusImplementing || after.FindTask(remainingID).Status != models.TaskStatusMerged {
				t.Fatal("unrelated lifecycle did not reach claimed and merged states")
			}
			if !reflect.DeepEqual(after.QuarantinedVerdicts, []models.QuarantinedVerdict{retained}) {
				t.Fatal("subsequent lifecycle changed retained evidence/audit")
			}
			if err := statevalidate.ValidateState(after, root, true, io.Discard); err != nil {
				t.Fatalf("subsequent lifecycle left invalid state: %v", err)
			}
		})
	}
}

func TestDeleteTaskQuarantinedVerdictsRejectedDeletionPreservesEvidence(t *testing.T) {
	for _, status := range []models.TaskStatus{models.TaskStatusReviewing, models.TaskStatusMerged} {
		t.Run(string(status), func(t *testing.T) {
			f := newQuarantineFixture(t)
			f.mutate(t, func(state *models.State) { state.FindTask(f.taskID).Status = status })
			f.capture(t, "REJECTED", "evidence must survive rejected deletion", f.commit)
			before := readStateBytes(t, f.statePath)
			if _, err := DeleteTask(f.root, f.taskID, false, false, "guard must reject"); err == nil || !strings.Contains(err.Error(), "cannot delete") {
				t.Fatalf("expected guarded deletion error, got %v", err)
			}
			if !bytes.Equal(before, readStateBytes(t, f.statePath)) {
				t.Fatal("rejected deletion changed task or quarantined evidence")
			}
		})
	}
}
