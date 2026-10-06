package ops

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestD42AwaitPauseExits(t *testing.T) {
	for _, mode := range []models.SystemMode{models.SystemModePaused, models.SystemModeCircuitBreakerTripped} {
		for _, caller := range []string{"doer", "reviewer"} {
			for _, route := range []string{"initial", "watcher", "event", "fallback"} {
				t.Run(string(mode)+"/"+caller+"/"+route, func(t *testing.T) {
					root, bb := setupReservationFixture(t)
					agentID := "reviewer-1"
					if caller == "doer" {
						agentID = "coder-1"
						if err := bb.Modify(func(s *models.State) error {
							task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, time.Now().UTC())
							task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(),
								Event: models.TaskEventSubmittedForReview, Agent: strPtr(agentID)})
							s.Tasks = []models.Task{task}
							s.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusIdle}
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					}
					before, err := bb.Read()
					if err != nil {
						t.Fatal(err)
					}
					pause := func() {
						t.Helper()
						if err := bb.Modify(func(s *models.State) error {
							if route != "initial" {
								a := s.Agents[agentID]
								if a.Status != models.AgentStatusWaiting || a.CurrentTask == nil || *a.CurrentTask != "task-1" {
									t.Fatal("pause injection must happen after await ownership acquisition")
								}
							}
							s.Config.Mode = mode
							return nil
						}); err != nil {
							t.Fatal(err)
						}
					}
					if route == "initial" {
						pause()
					} else if caller == "doer" {
						previous := newAwaitVerdictWatcher
						t.Cleanup(func() { newAwaitVerdictWatcher = previous })
						newAwaitVerdictWatcher = func(*db.Blackboard) (awaitVerdictWatcher, error) {
							pause()
							if route == "fallback" {
								return nil, errors.New("test polling fallback")
							}
							if route == "event" {
								return d42ReadyWatcher(), nil
							}
							return silentAwaitVerdictWatcher{}, nil
						}
					} else {
						previous := newAwaitResubmissionWatcher
						t.Cleanup(func() { newAwaitResubmissionWatcher = previous })
						newAwaitResubmissionWatcher = func(*db.Blackboard) (awaitResubmissionWatcher, error) {
							pause()
							if route == "fallback" {
								return nil, errors.New("test polling fallback")
							}
							if route == "event" {
								return d42ReadyWatcher(), nil
							}
							return silentAwaitVerdictWatcher{}, nil
						}
					}
					var outcome any
					timeout, abortInterval := 80*time.Millisecond, time.Millisecond
					if route == "event" {
						timeout, abortInterval = time.Second, time.Hour
					}
					if caller == "doer" {
						outcome, err = AwaitVerdictWithOptions(context.Background(), root, "task-1", agentID,
							timeout, AwaitVerdictOptions{AbortPollInterval: abortInterval, FallbackPollInterval: time.Millisecond})
					} else {
						outcome, err = AwaitResubmissionWithOptions(context.Background(), root, "task-1", agentID,
							timeout, AwaitResubmissionOptions{AbortPollInterval: abortInterval, FallbackPollInterval: time.Millisecond})
					}
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := json.Marshal(outcome)
					if err != nil {
						t.Fatal(err)
					}
					var fields map[string]any
					if err := json.Unmarshal(encoded, &fields); err != nil {
						t.Fatal(err)
					}
					if fields["verdict"] != "PAUSED" || fields["safe_action"] != "stop" ||
						!strings.Contains(fields["reason"].(string), "resume") {
						t.Fatalf("paused await = %s; want PAUSED with stop/resume guidance", encoded)
					}
					after, err := bb.Read()
					if err != nil {
						t.Fatal(err)
					}
					priorTask, task := before.FindTask("task-1"), after.FindTask("task-1")
					if task.Status != priorTask.Status || !reflect.DeepEqual(task.Worktree, priorTask.Worktree) ||
						!reflect.DeepEqual(task.ReviewCommit, priorTask.ReviewCommit) || !reflect.DeepEqual(task.BaseCommit, priorTask.BaseCommit) {
						t.Fatal("paused await changed the submitted attempt")
					}
					if after.Agents[agentID].CurrentTask != nil || (caller == "reviewer" && task.ReviewingBy != nil) ||
						(caller == "doer" && task.AssignedTo != nil) {
						t.Fatal("paused session retained its departed claim")
					}
				})
			}
		}
	}
}

type d42EventWatcher struct{ events chan struct{} }

func d42ReadyWatcher() d42EventWatcher {
	events := make(chan struct{}, 1)
	events <- struct{}{}
	return d42EventWatcher{events: events}
}

func (w d42EventWatcher) Events() <-chan struct{} { return w.events }
func (d42EventWatcher) Errors() <-chan error      { return nil }
func (d42EventWatcher) Close() error              { return nil }

func TestD42EarlyResubmissionPauseBeforeAndAfterPreflight(t *testing.T) {
	for _, timing := range []string{"initial", "after-preflight"} {
		t.Run(timing, func(t *testing.T) {
			f := newAssignmentPreflightFixture(t, models.TaskStatusReadyForReview)
			authority := models.AgentAuthority{ID: "code-reviewer-1", Generation: lifecycleGenerationA}
			if err := f.bb.Modify(func(s *models.State) error {
				task := s.FindTask("task-1")
				task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: &authority.ID})
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			t.Setenv(brand.EnvName("AGENT_ID"), authority.ID)
			t.Setenv(brand.EnvName("AGENT_GENERATION"), authority.Generation)
			t.Setenv("VALIDATION_TEST_REQUIRED", "fixture-value")
			before := f.state(t).FindTask("task-1")
			pause := func() {
				if err := f.bb.Modify(func(s *models.State) error {
					if timing == "after-preflight" && s.ValidationReadiness[authority.ID]["task-1"].Result != "passed" {
						t.Fatal("pause race did not follow a successful real preflight")
					}
					s.Config.Mode = models.SystemModePaused
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if timing == "initial" {
				pause()
			} else {
				armBeforeReclaim(t, pause)
			}
			result, err := AwaitResubmissionWithAuthority(context.Background(), f.root, "task-1", authority, time.Second)
			if err != nil || result.Verdict != ResubmissionPaused || result.SafeAction != SafeActionStop ||
				!strings.Contains(result.Reason, brand.Command("resume")) {
				t.Fatalf("early reclaim = %+v, %v; want PAUSED/stop/resume", result, err)
			}
			after := f.state(t)
			task := after.FindTask("task-1")
			if task.Status != models.TaskStatusReadyForReview || task.ReviewingBy != nil ||
				!reflect.DeepEqual(task.Worktree, before.Worktree) || !reflect.DeepEqual(task.ReviewCommit, before.ReviewCommit) ||
				after.Agents[authority.ID].CurrentTask != nil {
				t.Fatal("paused early reclaim changed submitted evidence or acquired review ownership")
			}
		})
	}
}

func TestD42PausedDoerPreservesAnotherCurrentTask(t *testing.T) {
	oldBinary := brand.BinaryName
	brand.BinaryName = "acme"
	t.Cleanup(func() { brand.BinaryName = oldBinary })
	root, bb := setupReservationFixture(t)
	if err := bb.Modify(func(s *models.State) error {
		now, lease := time.Now().UTC(), time.Now().Add(30*time.Minute)
		submitted := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
		submitted.AssignedTo, submitted.LeaseExpires = strPtr("coder-1"), &lease
		submitted.History = append(submitted.History, models.TaskHistoryEntry{Time: now,
			Event: models.TaskEventSubmittedForReview, Agent: strPtr("coder-1")})
		other := testhelpers.BuildTaskByStatus("task-2", models.TaskStatusImplementing, now)
		other.AssignedTo, other.LeaseExpires = strPtr("coder-1"), &lease
		s.Tasks = []models.Task{submitted, other}
		a := testhelpers.RegisteredTestAgent(models.RoleCoder)
		a.Status, a.CurrentTask = models.AgentStatusWorking, strPtr("task-2")
		s.Agents["coder-1"] = a
		s.Config.Mode = models.SystemModePaused
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	result, err := AwaitVerdict(context.Background(), root, "task-1", "coder-1", time.Second)
	if err != nil || result.Verdict != VerdictPaused || !strings.Contains(result.Reason, "acme resume") || strings.Contains(result.Reason, "liza") {
		t.Fatalf("paused doer = %+v, %v; want branded PAUSED guidance", result, err)
	}
	after, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before.Agents["coder-1"], after.Agents["coder-1"]) ||
		!reflect.DeepEqual(before.FindTask("task-2"), after.FindTask("task-2")) || after.FindTask("task-1").AssignedTo != nil {
		t.Fatal("paused exit cleared a different active task or retained the departed assignment")
	}
}

func TestD42PausedAwaitPreservesResumedDoerClaim(t *testing.T) {
	for _, route := range []string{"initial", "watcher", "event", "fallback", "deadline", "cleanup-race", "generation-race", "resubmitted"} {
		t.Run(route, func(t *testing.T) {
			root, bb := setupReservationFixture(t)
			authority := models.AgentAuthority{ID: "coder-1", Generation: testhelpers.TestAgentGeneration}
			if err := bb.Modify(func(s *models.State) error {
				now, lease := time.Now().UTC(), time.Now().Add(30*time.Minute)
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, now)
				task.AssignedTo, task.LeaseExpires = strPtr(authority.ID), &lease
				task.History = append(task.History, models.TaskHistoryEntry{Time: now,
					Event: models.TaskEventSubmittedForReview, Agent: &authority.ID})
				s.Tasks = []models.Task{task}
				agent := testhelpers.RegisteredTestAgent(models.RoleCoder)
				agent.Status = models.AgentStatusWaiting
				s.Agents[authority.ID] = agent
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			var resumed *models.State
			replace := func() {
				t.Helper()
				if err := bb.Modify(func(s *models.State) error {
					task := s.FindTask("task-1")
					task.Status = models.TaskStatusImplementing
					task.History = append(task.History,
						models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventRejected, Agent: strPtr("reviewer-1")},
						models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventClaimed, Agent: &authority.ID})
					models.AdvanceLifecycle(task)
					agent := s.Agents[authority.ID]
					agent.Status, agent.CurrentTask = models.AgentStatusWorking, strPtr(task.ID)
					if route == "generation-race" {
						agent.Generation = lifecycleGenerationB
					}
					s.Agents[authority.ID] = agent
					if route == "resubmitted" {
						task.Status = models.TaskStatusReadyForReview
						task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(),
							Event: models.TaskEventSubmittedForReview, Agent: &authority.ID})
						agent.Status = models.AgentStatusWaiting
						s.Agents[authority.ID] = agent
					}
					preparingAuthority := models.AgentAuthority{ID: authority.ID, Generation: agent.Generation}
					request, err := NewLifecycleRequest("submit-for-review", task, authority.ID, &preparingAuthority,
						LifecycleRequestOptions{RequestID: "resumed-submit", ExpectedTransition: models.TaskTransitionID(task)}, struct{}{})
					if err != nil {
						return err
					}
					if err := PrepareLifecycleRequest(task, request, s.Agents); err != nil {
						return err
					}
					s.Config.Mode = models.SystemModePaused
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var err error
				resumed, err = bb.Read()
				if err != nil {
					t.Fatal(err)
				}
			}
			if route == "initial" {
				replace()
			} else {
				previous := newAwaitVerdictWatcher
				t.Cleanup(func() { newAwaitVerdictWatcher = previous })
				newAwaitVerdictWatcher = func(*db.Blackboard) (awaitVerdictWatcher, error) {
					if route == "cleanup-race" || route == "generation-race" {
						if err := bb.Modify(func(s *models.State) error { s.Config.Mode = models.SystemModePaused; return nil }); err != nil {
							t.Fatal(err)
						}
						armBeforeReclaim(t, replace)
					} else {
						replace()
					}
					if route == "fallback" {
						return nil, errors.New("test polling fallback")
					}
					if route == "event" {
						return d42ReadyWatcher(), nil
					}
					return silentAwaitVerdictWatcher{}, nil
				}
			}
			timeout, abort := time.Second, time.Millisecond
			if route == "event" {
				abort = time.Hour
			} else if route == "deadline" {
				timeout, abort = 20*time.Millisecond, time.Hour
			}
			result, err := AwaitVerdictWithAuthorityOptions(context.Background(), root, "task-1", authority,
				timeout, AwaitVerdictOptions{AbortPollInterval: abort, FallbackPollInterval: time.Millisecond})
			if route == "generation-race" {
				var fence *AgentAuthorityError
				if !errors.As(err, &fence) {
					t.Fatalf("paused cleanup error = %T; want registration fence", err)
				}
			} else if err != nil || result.Verdict != VerdictPaused || result.SafeAction != SafeActionStop {
				t.Fatalf("paused await = %+v, %v; want read-only PAUSED/stop", result, err)
			}
			after, err := bb.Read()
			if err != nil {
				t.Fatal(err)
			}
			if resumed == nil || !reflect.DeepEqual(resumed, after) {
				t.Fatal("stale paused await changed the resumed claim, lease, current task or prepared submission")
			}
		})
	}
}
