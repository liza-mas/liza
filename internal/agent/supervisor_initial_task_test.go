package agent

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// A claim spends InitialTask. A turn that ends in an observed CLI failure
// retries without reaching the loop's end; the reviewer must then claim among
// all reviewable work, not retry only the initial task (in claim cooldown after
// its failed turn), and later prompts must not claim to resume it.
func TestRunSupervisorInitialTaskSpentByClaim(t *testing.T) {
	project := setupReviewClaimProject(t)
	project.addReviewableTask(t, "task-a", false)
	project.addReviewableTask(t, "task-b", false)
	if err := project.bb.Modify(func(state *models.State) error {
		state.Config.SpinningRestartThreshold = 1
		delete(state.Agents, project.reviewerID) // the loop registers its own agent
		return nil
	}); err != nil {
		t.Fatalf("configure fixture: %v", err)
	}
	recordSupervisorDelays(t, nil)
	// task-a stays in claim cooldown after its failed turn; a reviewer still
	// targeting it waits that out, so the run ends at the first task-b turn.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	mock := &MockLLMAgent{Output: `{"ok":false,"result":null,"error":{"code":"internal","message":"internal error"}}` + "\n"}
	mock.OnExecute = func(context.Context, string, string, string, string, []string) error {
		if calls := mock.GetCalls(); calls[len(calls)-1].TaskID == "task-b" {
			cancel()
		}
		return nil
	}

	err := RunSupervisor(ctx, SupervisorConfig{
		AgentID:          project.reviewerID,
		Role:             models.RoleCodeReviewer,
		ProjectRoot:      project.root,
		StatePath:        project.statePath,
		LogPath:          filepath.Join(project.root, paths.ProjectDirName(), "log.yaml"),
		SpecsDir:         filepath.Join(project.root, "specs"),
		CLIName:          "codex",
		InitialTask:      "task-a",
		LLMAgent:         mock,
		ExecutionTimeout: 5 * time.Second,
	})
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunSupervisor: %v", err)
	}

	calls := mock.GetCalls()
	tasks := make([]string, len(calls))
	for i, call := range calls {
		tasks[i] = call.TaskID
	}
	if len(calls) == 0 || calls[0].TaskID != "task-a" {
		t.Fatalf("provider turns on %v, want the first on the initial task-a", tasks)
	}
	if !slices.Contains(tasks, "task-b") {
		t.Fatalf("provider turns on %v, want task-b reviewed while task-a cools down", tasks)
	}
	const resume = "Resuming task: task-a"
	if !strings.Contains(calls[0].Prompt, resume) {
		t.Fatalf("first prompt lacks %q", resume)
	}
	for i, call := range calls[1:] {
		if strings.Contains(call.Prompt, resume) {
			t.Fatalf("prompt %d (task %s) still carries %q after the claim spent it", i+2, call.TaskID, resume)
		}
	}
}
