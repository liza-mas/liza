package ops

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

type indexRefreshLaunch struct {
	repoRoot string
	trigger  string
}

// recordIndexRefreshes replaces the post-merge launcher with a recorder that
// returns launchErr.
func recordIndexRefreshes(t *testing.T, launchErr error) *[]indexRefreshLaunch {
	t.Helper()

	var launches []indexRefreshLaunch
	previous := startIndexRefresh
	startIndexRefresh = func(repoRoot, trigger string) error {
		launches = append(launches, indexRefreshLaunch{repoRoot: repoRoot, trigger: trigger})
		return launchErr
	}
	t.Cleanup(func() { startIndexRefresh = previous })
	return &launches
}

func TestMergeWorktreeStartsIndexRefreshAfterSuccess(t *testing.T) {
	launches := recordIndexRefreshes(t, nil)
	root, _ := setupMergeTestRepo(t, "merge-index", "coder-1")

	result, err := MergeWorktree(root, "merge-index", "coder-1")
	if err != nil {
		t.Fatalf("MergeWorktree() error = %v", err)
	}
	want := []indexRefreshLaunch{{repoRoot: root, trigger: "merge"}}
	if !slices.Equal(*launches, want) {
		t.Fatalf("index refresh launches = %+v, want %+v", *launches, want)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "index refresh") {
			t.Fatalf("unexpected index refresh warning %q", warning)
		}
	}
}

func TestMergeWorktreeReportsIndexRefreshLaunchFailureAsWarning(t *testing.T) {
	recordIndexRefreshes(t, errors.New("coordinator missing"))
	root, statePath := setupMergeTestRepo(t, "merge-index-warn", "coder-1")

	result, err := MergeWorktree(root, "merge-index-warn", "coder-1")
	if err != nil {
		t.Fatalf("MergeWorktree() error = %v, want the merge to succeed", err)
	}
	if !slices.Contains(result.Warnings, "failed to start repo-root index refresh: coordinator missing") {
		t.Fatalf("warnings = %q, want the launch failure", result.Warnings)
	}
	if task := readStateForTest(t, statePath).FindTask("merge-index-warn"); task.Status != models.TaskStatusMerged {
		t.Fatalf("task status = %v, want MERGED", task.Status)
	}
}

// A replayed merge changes nothing, so it has nothing new to index.
func TestMergeWorktreeReplayDoesNotStartIndexRefresh(t *testing.T) {
	launches := recordIndexRefreshes(t, nil)
	root, statePath := setupMergeTestRepo(t, "merge-index-replay", "coder-1")
	bb := db.New(statePath)
	authority := models.AgentAuthority{ID: "orchestrator-1", Generation: "merge-generation"}
	if err := bb.Modify(func(state *models.State) error {
		state.Agents[authority.ID] = models.Agent{Role: models.RoleOrchestrator, Status: models.AgentStatusIdle, Generation: authority.Generation}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	opts := LifecycleRequestOptions{RequestID: "merge-1", ExpectedTransition: models.TaskTransitionID(state.FindTask("merge-index-replay"))}
	if _, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-index-replay", authority, opts); err != nil {
		t.Fatal(err)
	}
	replay, err := MergeWorktreeWithAuthorityAndOptions(root, "merge-index-replay", authority, opts)
	if err != nil || replay.Outcome != models.LifecycleAlreadyCompleted {
		t.Fatalf("merge replay = %+v, %v", replay, err)
	}
	if len(*launches) != 1 {
		t.Fatalf("index refresh launches = %+v, want only the original merge's", *launches)
	}
}
