package ops

import (
	"bytes"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestReviewerClaimLifecycleLegacyCommitReplay(t *testing.T) {
	for _, storedCommit := range []string{"abc123", "review123"} {
		t.Run(storedCommit, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)
			state := testhelpers.CreateValidState()
			registerClaimReviewerTaskTestAgents(state)
			state.Tasks = []models.Task{{
				ID: "task-1", Status: models.TaskStatusReadyForReview, RolePair: "coding-pair",
				ReviewCommit: &storedCommit, Created: time.Now().UTC(),
			}}
			bb := testhelpers.WriteInitialState(t, statePath, state)
			input := ClaimReviewerTaskInput{
				ProjectRoot: root, TaskID: "task-1", AgentID: "code-reviewer-1", LeaseDuration: 1800,
				RequestOptions: ownershipRequestOptions(t, bb, "legacy-review-claim"),
			}
			first, err := ClaimReviewerTask(input)
			if err != nil {
				t.Fatal(err)
			}
			if first.ReviewCommit != storedCommit || first.Outcome != models.LifecycleCompleted {
				t.Fatalf("legacy claim changed its stored commit: %+v", first)
			}
			// Replay must use the original receipt even after the live review
			// boundary and ownership have moved to a different reviewer.
			if err := bb.Modify(func(state *models.State) error {
				task := state.FindTask("task-1")
				newCommit, newReviewer := "later-review", "code-reviewer-2"
				task.ReviewCommit, task.ReviewingBy = &newCommit, &newReviewer
				models.AdvanceLifecycle(task)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := ownershipStateBytes(t, statePath)
			replay, err := ClaimReviewerTask(input)
			if err != nil {
				t.Fatal(err)
			}
			if replay.ReviewCommit != storedCommit || replay.CompletedTransitionID != first.CompletedTransitionID ||
				!replay.LeaseExpires.Equal(first.LeaseExpires) || replay.Outcome != models.LifecycleAlreadyCompleted || replay.SafeAction != "stop" {
				t.Fatalf("legacy replay changed its completion or permitted obsolete ownership: %+v", replay)
			}
			if !bytes.Equal(before, ownershipStateBytes(t, statePath)) {
				t.Fatal("legacy reviewer replay rewrote state")
			}
		})
	}
}
