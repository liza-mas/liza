package supplement

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
)

func init() { adapters["heal-orphaned-base"] = healOrphanedBaseStep }

// healPreparationOperation is the lifecycle operation name a heal reserves under.
const healPreparationOperation = "heal-orphaned-review-base"

// healParams configures one heal step. PreparedTarget names the labelled
// integration commit of a heal preparation an ended invocation left behind;
// empty means a fresh heal.
type healParams struct {
	PreparedTarget string `json:"prepared_target,omitempty"`
}

// healPayload is the heal's intended effect as the preparation digests it.
type healPayload struct {
	OldReviewCommit string `json:"old_review_commit"`
	OldBaseCommit   string `json:"old_base_commit"`
	Target          string `json:"target"`
}

// healOrphanedBaseStep runs the exported heal as the registered reviewer, after
// seeding the preparation of an ended heal when the case asks for one, and
// records the outcome, the error, the task branch and its replayed range.
func healOrphanedBaseStep(t *testing.T, f *fixture, s step) {
	t.Helper()
	var p healParams
	if len(s.Params) > 0 {
		if err := json.Unmarshal(s.Params, &p); err != nil {
			t.Fatal(err)
		}
	}
	authority := models.AgentAuthority{ID: s.Agent, Generation: "generation-1"}
	bb := db.For(paths.New(f.project).StatePath())
	if p.PreparedTarget != "" {
		if err := bb.Modify(func(state *models.State) error {
			task := state.FindTask("task-1")
			request, err := ops.NewLifecycleRequest(healPreparationOperation, task, s.Agent, &authority, ops.LifecycleRequestOptions{},
				healPayload{OldReviewCommit: *task.ReviewCommit, OldBaseCommit: *task.BaseCommit, Target: f.labels[p.PreparedTarget]})
			if err != nil {
				return err
			}
			return ops.PrepareLifecycleRequest(task, request, state.Agents)
		}); err != nil {
			t.Fatalf("seed the heal preparation: %v", err)
		}
	}

	outcome, healErr := ops.HealOrphanedReviewBase(ops.OrphanedBaseHealInput{
		ProjectRoot: f.project, AgentID: s.Agent, Role: models.RoleCodeReviewer, Authority: &authority, TaskID: "task-1",
	})

	wt := filepath.Join(f.project, ".worktrees/task-1")
	head := f.git(t, wt, "rev-parse", "HEAD")
	headName := "unlabelled"
	for label, sha := range f.labels {
		if sha == head {
			headName = label
		}
	}
	if headName == "unlabelled" {
		// The replayed commits carry the run's own timestamps, so the new HEAD is
		// named by role instead of by value.
		f.labels["healed"] = head
		headName = "healed"
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	task := state.FindTask("task-1")
	base := *task.BaseCommit
	onIntegration := f.gitSucceeds(wt, "merge-base", "--is-ancestor", base, "integration")
	subjects := f.git(t, wt, "log", "--format=%s", base+"..HEAD")
	errText := "<none>"
	if healErr != nil {
		errText = healErr.Error()
	}
	f.result = fmt.Appendf(nil, "outcome: %s\nerror: %s\nworktree_head: %s\nreview_commit_is_head: %t\nbase_on_integration: %t\nreplayed_range: %s\n",
		outcome, errText, headName, *task.ReviewCommit == head, onIntegration, strings.ReplaceAll(subjects, "\n", ", "))
}
