package agent

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	lizagit "github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRunSupervisorWaitAdmission(t *testing.T) {
	for _, tc := range []struct {
		name  string
		role  string
		wake  string
		after string
	}{
		{"doer_event_resume", "coder", "event", "resume"},
		{"reviewer_event_resume", "code-reviewer", "event", "resume"},
		{"doer_ticker_resume", "coder", "ticker", "resume"},
		{"reviewer_poll_resume", "code-reviewer", "poll", "resume"},
		{"stop_while_paused", "coder", "event", "stop"},
		{"cancel_while_paused", "code-reviewer", "event", "cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			testhelpers.SetupTestGitRepo(t, root)
			statePath, _ := testhelpers.SetupLizaDir(t, root)
			testhelpers.SetupPipelineConfig(t, root)
			state := testhelpers.CreateValidState()
			state.Config.CoderPollInterval = 1
			state.Config.ReviewerPollInterval = 1
			state.Config.DoerMaxWait = 30
			state.Config.ReviewerMaxWait = 30
			state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("admission-task", models.TaskStatusBlocked, time.Now().UTC())}
			bb := testhelpers.WriteInitialState(t, statePath, state)

			// Park the real role wait before changing mode or making work ready.
			// The injected watcher controls notification only; task detection,
			// pause handling, claiming, and provider admission remain real.
			watcher := newSilentWatcher()
			watcherReady := make(chan struct{})
			var watcherOnce sync.Once
			previousWatcher := newStateWatcher
			newStateWatcher = func(*db.Blackboard) (stateWatcher, error) {
				watcherOnce.Do(func() { close(watcherReady) })
				if tc.wake == "poll" {
					return nil, errors.New("exercise polling fallback")
				}
				return watcher, nil
			}
			t.Cleanup(func() { newStateWatcher = previousWatcher })

			ctx, cancel := context.WithCancel(context.Background())
			providerStarted := make(chan struct{})
			var providerOnce sync.Once
			provider := &MockLLMAgent{OnExecute: func(ctx context.Context, _, _, _, _ string, _ []string) error {
				providerOnce.Do(func() { close(providerStarted) })
				<-ctx.Done()
				return ctx.Err()
			}}
			done := make(chan error, 1)
			go func() {
				done <- RunSupervisor(ctx, SupervisorConfig{
					AgentID: tc.role + "-1", Role: tc.role,
					ProjectRoot: root, StatePath: statePath,
					LogPath: paths.New(root).LogPath(), SpecsDir: filepath.Join(root, "specs"),
					CLIName: "codex", LLMAgent: provider, ExecutionTimeout: 30 * time.Second,
				})
			}()
			finished := false
			t.Cleanup(func() {
				cancel()
				if !finished {
					select {
					case <-done:
					case <-time.After(10 * time.Second):
						t.Error("supervisor did not exit after cancellation")
					}
				}
			})
			select {
			case <-watcherReady:
			case err := <-done:
				finished = true
				t.Fatalf("supervisor exited before parking: %v", err)
			case <-time.After(10 * time.Second):
				t.Fatal("supervisor did not enter work wait")
			}

			readyStatus := models.TaskStatusReady
			if tc.role == "code-reviewer" {
				readyStatus = models.TaskStatusReadyForReview
			}
			readyTask := testhelpers.BuildTaskByStatus("admission-task", readyStatus, time.Now().UTC())
			if tc.role == "code-reviewer" {
				git := lizagit.New(root)
				if _, err := git.CreateWorktree(readyTask.ID, "integration"); err != nil {
					t.Fatal(err)
				}
				sha, err := git.GetCommitSHA("integration")
				if err != nil {
					t.Fatal(err)
				}
				readyTask.BaseCommit, readyTask.ReviewCommit = &sha, &sha
				readyTask.AssignedTo = nil
			}
			if err := bb.Modify(func(s *models.State) error {
				s.Config.Mode = models.SystemModePaused
				s.Tasks[0] = readyTask
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if tc.wake == "event" {
				select {
				case watcher.events <- struct{}{}:
				case <-time.After(5 * time.Second):
					t.Fatal("work wait did not consume its notification")
				}
			}

			// Cover the one-second ticker/poll paths as well as notification.
			select {
			case <-providerStarted:
				t.Fatal("provider started after PAUSED work became available")
			case err := <-done:
				finished = true
				t.Fatalf("paused supervisor should keep waiting: %v", err)
			case <-time.After(1300 * time.Millisecond):
			}
			paused, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if task := paused.FindTask(readyTask.ID); task.Status != readyStatus || task.ReviewingBy != nil || task.AssignedTo != nil {
				t.Fatalf("task claimed while PAUSED: status=%s assigned=%v reviewing=%v", task.Status, task.AssignedTo, task.ReviewingBy)
			}

			if tc.after == "cancel" {
				cancel()
			} else if err := bb.Modify(func(s *models.State) error {
				if tc.after == "stop" {
					s.Config.Mode = models.SystemModeStopped
				} else {
					s.Config.Mode = models.SystemModeRunning
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if tc.after == "resume" {
				select {
				case <-providerStarted:
				case err := <-done:
					finished = true
					t.Fatalf("supervisor exited before resumed launch: %v", err)
				case <-time.After(10 * time.Second):
					t.Fatal("provider did not start after resume")
				}
				return
			}
			select {
			case <-providerStarted:
				t.Fatal("provider started after stop/cancellation")
			case err := <-done:
				finished = true
				if err != nil && !errors.Is(err, context.Canceled) {
					t.Fatalf("stop/cancellation: %v", err)
				}
			case <-time.After(6 * time.Second):
				t.Fatal("supervisor did not stop within the existing pause poll interval")
			}
		})
	}
}
