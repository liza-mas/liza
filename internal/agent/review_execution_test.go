package agent

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func reviewExecutionFixture(t *testing.T) (SupervisorConfig, *db.Blackboard, models.Config) {
	t.Helper()
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.SetupPipelineConfig(t, root)
	now := time.Now().UTC()
	lease := now.Add(30 * time.Minute)
	id, taskID := "code-reviewer-1", "review-task"
	state := testhelpers.CreateValidState()
	state.Config.HeartbeatInterval = 1
	task := testhelpers.BuildTaskByStatus(taskID, models.TaskStatusReviewing, now)
	task.ReviewingBy, task.ReviewLeaseExpires = &id, &lease
	state.Tasks = []models.Task{task}
	state.Agents[id] = models.Agent{
		Role: "code-reviewer", Provider: "codex", PID: os.Getpid(),
		Status: models.AgentStatusReviewing, CurrentTask: &taskID,
		Heartbeat: now, LeaseExpires: &lease,
	}
	bb := testhelpers.WriteInitialState(t, statePath, state)
	return SupervisorConfig{
		AgentID: id, Role: "code-reviewer", Authority: testSupervisorAuthority(t, bb, id),
		ProjectRoot: root, StatePath: statePath, CLIName: "codex",
		ExecutionTimeout: 20 * time.Second,
	}, bb, state.Config
}

func TestReviewLaunchRequiresCurrentOwnership(t *testing.T) {
	for _, mutation := range []string{"none", "task_owner", "agent_task", "agent_status", "agent_role", "generation", "review_lease"} {
		t.Run(mutation, func(t *testing.T) {
			config, bb, _ := reviewExecutionFixture(t)
			gate := newTaskProviderLaunchGate(config, "review-task", nil)
			if err := bb.Modify(func(s *models.State) error {
				a := s.Agents[config.AgentID]
				switch mutation {
				case "task_owner":
					s.Tasks[0].ReviewingBy = testhelpers.StringPtr("other-reviewer")
				case "agent_task":
					a.CurrentTask = nil
				case "agent_status":
					a.Status = models.AgentStatusIdle
				case "agent_role":
					a.Role = models.RoleCoder
				case "generation":
					a.Generation = "replacement-generation"
				case "review_lease":
					expired := time.Now().UTC().Add(-time.Minute)
					s.Tasks[0].ReviewLeaseExpires = &expired
				}
				s.Agents[config.AgentID] = a
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			started := false
			err := gate.launch(context.Background(), func() error { started = true; return nil })
			if mutation == "none" {
				if err != nil || !started {
					t.Fatalf("coherent review could not launch: started=%v err=%v", started, err)
				}
			} else if err == nil || started {
				t.Fatalf("obsolete review launched after %s changed: started=%v err=%v", mutation, started, err)
			}
		})
	}
}

func TestReviewLaunchUsesConfiguredReviewerRole(t *testing.T) {
	config, bb, _ := reviewExecutionFixture(t)
	pipelinePath := filepath.Join(config.ProjectRoot, paths.ProjectDirName(), "pipeline.yaml")
	content, err := os.ReadFile(pipelinePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pipelinePath, []byte(strings.ReplaceAll(string(content), "code-reviewer", "quality-auditor")), 0o644); err != nil {
		t.Fatal(err)
	}
	config.Role = "quality-auditor"
	if err := bb.Modify(func(s *models.State) error {
		a := s.Agents[config.AgentID]
		a.Role = config.Role
		s.Agents[config.AgentID] = a
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started := false
	gate := newTaskProviderLaunchGate(config, "review-task", nil)
	err = gate.launch(context.Background(), func() error {
		started = true
		return nil
	})
	if err != nil || !started {
		t.Fatalf("coherent configured reviewer could not launch: started=%v err=%v", started, err)
	}
	if err := bb.Modify(func(s *models.State) error {
		a := s.Agents[config.AgentID]
		a.CurrentTask = nil
		s.Agents[config.AgentID] = a
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	started = false
	err = gate.launch(context.Background(), func() error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("configured reviewer launched without current_task: started=%v err=%v", started, err)
	}
}

func TestReviewExecutionReacquisitionRetiresOwnVerdict(t *testing.T) {
	config, bb, runtimeConfig := reviewExecutionFixture(t)
	config.LLMAgent = &reviewExecutionProvider{run: func(ctx context.Context) error {
		for i, status := range []models.TaskStatus{models.TaskStatusRejected, models.TaskStatusReviewing, models.TaskStatusReadyForReview} {
			if err := bb.Modify(func(s *models.State) error {
				task := &s.Tasks[0]
				a := s.Agents[config.AgentID]
				task.Status = status
				models.AdvanceLifecycle(task)
				switch i {
				case 0:
					a.Status = models.AgentStatusWaiting
					task.History = append(task.History, models.TaskHistoryEntry{
						Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &config.AgentID,
					})
				case 1:
					a.Status = models.AgentStatusReviewing
				case 2:
					task.ReviewingBy, task.ReviewLeaseExpires = nil, nil
					a.Status, a.CurrentTask = models.AgentStatusIdle, nil
				}
				s.Agents[config.AgentID] = a
				return nil
			}); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				if i != 2 {
					t.Errorf("legitimate rejection/resubmission stage %d cancelled", i)
				}
				return nil
			case <-time.After(2500 * time.Millisecond):
				if i == 2 {
					t.Error("old own verdict excused ownership loss after active re-review")
				}
			}
		}
		return nil
	}}
	if _, _, err := executeAgent(context.Background(), config, "review", nil, "review-task", runtimeConfig); !errors.Is(err, errReviewOwnershipLost) {
		t.Fatalf("executeAgent = %v, want ownership loss", err)
	}
}

// Keep provider execution injectable while exercising the real supervisor gate,
// context, and blackboard. The fake provider observes the supplied cancellation.
type reviewExecutionProvider struct {
	MockLLMAgent
	run          func(context.Context) error
	beforeLaunch func() error
	result       LLMAgentRunResult
}

func (p *reviewExecutionProvider) Run(ctx context.Context, req LLMAgentRunRequest) (LLMAgentRunResult, error) {
	if p.beforeLaunch != nil {
		if err := p.beforeLaunch(); err != nil {
			return LLMAgentRunResult{}, err
		}
	}
	if err := req.LaunchGate.launch(ctx, func() error { return nil }); err != nil {
		return LLMAgentRunResult{}, err
	}
	return p.result, p.run(ctx)
}

func TestReviewExecutionOwnershipLifetime(t *testing.T) {
	for _, tc := range []struct {
		name       string
		wantCancel bool
	}{
		{"released", true},
		{"agent_detached", true},
		{"replaced", true},
		{"generation_replaced", true},
		{"task_deleted", true},
		{"registration_expired", true},
		{"old_verdict", true},
		{"foreign_verdict", true},
		{"approved_reply", false},
		{"waiting_resubmission", false},
		{"raw_pid_disagreement", false},
		{"transient_read_error", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config, bb, runtimeConfig := reviewExecutionFixture(t)
			if tc.name == "old_verdict" {
				if err := bb.Modify(func(s *models.State) error {
					s.Tasks[0].History = append(s.Tasks[0].History, models.TaskHistoryEntry{
						Time: time.Now().UTC().Add(-time.Hour), Event: models.TaskEventApproved,
						Agent: &config.AgentID,
					})
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			config.LLMAgent = &reviewExecutionProvider{run: func(ctx context.Context) error {
				if tc.name == "transient_read_error" {
					original, err := os.ReadFile(config.StatePath)
					if err != nil {
						return err
					}
					defer func() {
						if err := os.WriteFile(config.StatePath, original, 0o644); err != nil {
							t.Errorf("restore transient read failure: %v", err)
						}
					}()
					if err := os.WriteFile(config.StatePath, []byte("tasks: [invalid YAML"), 0o644); err != nil {
						return err
					}
				} else if err := bb.Modify(func(s *models.State) error {
					a := s.Agents[config.AgentID]
					task := &s.Tasks[0]
					switch tc.name {
					case "generation_replaced":
						a.Generation = "replacement-generation"
					case "task_deleted":
						s.Tasks = nil
					case "registration_expired":
						expired := time.Now().UTC().Add(-time.Minute)
						a.LeaseExpires = &expired
					case "agent_detached":
						a.CurrentTask = nil
					case "raw_pid_disagreement":
						a.PID = 999999999
					case "waiting_resubmission":
						task.Status = models.TaskStatusRejected
						a.Status = models.AgentStatusWaiting
						task.History = append(task.History, models.TaskHistoryEntry{
							Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &config.AgentID,
						})
					case "replaced":
						task.ReviewingBy = testhelpers.StringPtr("other-reviewer")
					default:
						task.ReviewingBy, task.ReviewLeaseExpires = nil, nil
						task.Status = models.TaskStatusReadyForReview
						a.CurrentTask, a.Status = nil, models.AgentStatusIdle
						if tc.name == "approved_reply" || tc.name == "foreign_verdict" {
							author := config.AgentID
							if tc.name == "foreign_verdict" {
								author = "other-reviewer"
							}
							task.Status = models.TaskStatusApproved
							task.History = append(task.History, models.TaskHistoryEntry{
								Time: time.Now().UTC(), Event: models.TaskEventApproved, Agent: &author,
							})
						}
					}
					s.Agents[config.AgentID] = a
					return nil
				}); err != nil {
					return err
				}

				// Three heartbeat ticks, well below the execution timeout: an
				// execution deadline cannot masquerade as ownership cancellation.
				select {
				case <-ctx.Done():
					if !tc.wantCancel {
						t.Errorf("valid review turn cancelled during %s: %v", tc.name, ctx.Err())
					}
				case <-time.After(3200 * time.Millisecond):
					if tc.wantCancel {
						t.Errorf("provider kept running after ownership loss (%s)", tc.name)
					}
				}
				return nil
			}}
			_, _, err := executeAgent(context.Background(), config, "review", nil, "review-task", runtimeConfig)
			if (tc.wantCancel && !errors.Is(err, errReviewOwnershipLost)) || (!tc.wantCancel && err != nil) {
				t.Fatalf("executeAgent: %v, want ownership loss=%v", err, tc.wantCancel)
			}
			if tc.name == "replaced" {
				after, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				if owner := after.Tasks[0].ReviewingBy; owner == nil || *owner != "other-reviewer" {
					t.Fatalf("old execution changed replacement ownership: %v", owner)
				}
			}
		})
	}
}

func TestRunSupervisorReviewOwnershipLossContinues(t *testing.T) {
	for _, tc := range []struct {
		name         string
		beforeLaunch bool
		exitCode     int
		providerErr  error
	}{
		{name: "launch_rejected", beforeLaunch: true},
		{name: "killed", exitCode: -1},
		{name: "escaped_pipe", providerErr: exec.ErrWaitDelay},
		{name: "closed_reader", providerErr: os.ErrClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath, taskID := setupAgentMergeRepo(t)
			base := testhelpers.MustGit(t, root, "rev-parse", "integration")
			bb := db.For(statePath)
			if err := bb.Modify(func(s *models.State) error {
				s.Agents = map[string]models.Agent{}
				s.Config.HeartbeatInterval = 1
				s.Config.ReviewerPollInterval = 1
				s.Config.ReviewerMaxWait = 1
				task := s.FindTask(taskID)
				task.Status = models.TaskStatusReadyForReview
				task.BaseCommit = &base
				task.ApprovedBy, task.Approvals = nil, nil
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			const agentID, replacementID = "code-reviewer-1", "code-reviewer-2"
			replaceOwner := func() error {
				return bb.Modify(func(s *models.State) error {
					task := s.FindTask(taskID)
					task.ReviewingBy = testhelpers.StringPtr(replacementID)
					a := testhelpers.RegisteredTestAgent("code-reviewer")
					a.Status, a.CurrentTask = models.AgentStatusReviewing, &taskID
					s.Agents[replacementID] = a
					return nil
				})
			}
			provider := &reviewExecutionProvider{result: LLMAgentRunResult{ExitCode: tc.exitCode}}
			if tc.beforeLaunch {
				provider.beforeLaunch = replaceOwner
			}
			provider.run = func(ctx context.Context) error {
				if tc.beforeLaunch {
					t.Error("provider started after ownership was replaced")
					return nil
				}
				if err := replaceOwner(); err != nil {
					return err
				}
				<-ctx.Done()
				return tc.providerErr
			}
			resumed := errors.New("observed next supervisor iteration")
			previousGate := waitWhilePausedForSupervisor
			gateCalls := 0
			waitWhilePausedForSupervisor = func(context.Context, string, string) error {
				gateCalls++
				if gateCalls < 3 { // Two admission checks precede the first turn.
					return nil
				}
				s, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				a, exists := s.Agents[agentID]
				if !exists || a.Status != models.AgentStatusIdle || a.CurrentTask != nil {
					t.Errorf("reviewer registration not retained/reset: exists=%v agent=%+v", exists, a)
				}
				task := s.FindTask(taskID)
				if task.Status != models.TaskStatusReviewing || task.ReviewingBy == nil || *task.ReviewingBy != replacementID ||
					s.Agents[replacementID].Status != models.AgentStatusReviewing {
					t.Errorf("replacement ownership changed: task=%+v", task)
				}
				return resumed
			}
			t.Cleanup(func() { waitWhilePausedForSupervisor = previousGate })
			previousTimer := newSupervisorDelayTimer
			var delays []time.Duration
			newSupervisorDelayTimer = func(delay time.Duration) *time.Timer {
				delays = append(delays, delay)
				return time.NewTimer(0)
			}
			t.Cleanup(func() { newSupervisorDelayTimer = previousTimer })
			var logs bytes.Buffer
			restoreLogger := UseLoggerOutput(&logs)
			defer restoreLogger()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			err := RunSupervisor(ctx, SupervisorConfig{
				AgentID: agentID, Role: "code-reviewer", ProjectRoot: root, StatePath: statePath,
				LogPath: paths.New(root).LogPath(), SpecsDir: filepath.Join(root, "specs"),
				CLIName: "codex", LLMAgent: provider, InitialTask: taskID, ExecutionTimeout: 20 * time.Second,
			})
			if !errors.Is(err, resumed) {
				t.Fatalf("supervisor did not continue: %v\n%s", err, logs.String())
			}
			if len(delays) != 1 || delays[0] != time.Second {
				t.Fatalf("ownership-loss delays = %v, want one configured poll interval", delays)
			}
			if !strings.Contains(logs.String(), "Review ownership lost, checking for more work") ||
				strings.Contains(logs.String(), "Agent crashed") || strings.Contains(logs.String(), "blocking task") {
				t.Fatalf("ownership loss was not classified independently: %s", logs.String())
			}
		})
	}
}
