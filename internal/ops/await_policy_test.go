package ops

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestAwaitPollingPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                     string
		seconds                                  int
		abort, fallback, wantAbort, wantFallback time.Duration
	}{
		{name: "legacy unset", wantAbort: 10 * time.Second, wantFallback: 10 * time.Second},
		{name: "configured", seconds: 23, wantAbort: 23 * time.Second, wantFallback: 23 * time.Second},
		{name: "abort override", seconds: 23, abort: time.Millisecond, wantAbort: time.Millisecond, wantFallback: 23 * time.Second},
		{name: "fallback override", seconds: 23, fallback: 2 * time.Millisecond, wantAbort: 23 * time.Second, wantFallback: 2 * time.Millisecond},
		{name: "both overrides", abort: time.Millisecond, fallback: 2 * time.Millisecond, wantAbort: time.Millisecond, wantFallback: 2 * time.Millisecond},
		{name: "nonpositive overrides", seconds: 23, abort: -time.Second, fallback: -time.Second, wantAbort: 23 * time.Second, wantFallback: 23 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := models.Config{AwaitPollInterval: tc.seconds}
			verdict, err := (AwaitVerdictOptions{AbortPollInterval: tc.abort, FallbackPollInterval: tc.fallback}).normalized(config)
			if err != nil || verdict.AbortPollInterval != tc.wantAbort || verdict.FallbackPollInterval != tc.wantFallback {
				t.Fatalf("verdict policy = %+v, %v; want %s/%s", verdict, err, tc.wantAbort, tc.wantFallback)
			}
			resubmission, err := (AwaitResubmissionOptions{AbortPollInterval: tc.abort, FallbackPollInterval: tc.fallback, PollOnTimeout: true}).normalized(config)
			if err != nil || resubmission.AbortPollInterval != tc.wantAbort || resubmission.FallbackPollInterval != tc.wantFallback || !resubmission.PollOnTimeout {
				t.Fatalf("resubmission policy = %+v, %v; want %s/%s and retained slice policy", resubmission, err, tc.wantAbort, tc.wantFallback)
			}
		})
	}
}

func invalidAwaitIntervals() []int {
	values := []int{-1}
	overflow := int64(math.MaxInt64/int64(time.Second)) + 1
	if int64(int(overflow)) == overflow {
		values = append(values, int(overflow))
	}
	return values
}

func TestAwaitPollingPolicyRejectsInvalidConfigBeforeOwnership(t *testing.T) {
	t.Parallel()
	for _, seconds := range invalidAwaitIntervals() {
		for _, caller := range []string{"doer", "reviewer"} {
			t.Run(caller+"/"+time.Duration(seconds).String(), func(t *testing.T) {
				root, bb, agentID := setupAwaitPolicyFixture(t, caller, seconds)
				before, err := bb.ReadRaw()
				if err != nil {
					t.Fatal(err)
				}
				if caller == "doer" {
					_, err = AwaitVerdict(context.Background(), root, "task-1", agentID, time.Second)
				} else {
					_, err = AwaitResubmission(context.Background(), root, "task-1", agentID, time.Second)
				}
				var precondition *PreconditionError
				if !errors.As(err, &precondition) || !strings.Contains(err.Error(), AwaitPollIntervalConfigKey) || !strings.Contains(err.Error(), "config set") {
					t.Fatalf("invalid config error = %v; want named precondition with repair command", err)
				}
				after, err := bb.ReadRaw()
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatal("invalid interval changed ownership/state")
				}
			})
		}
	}
}

func setupAwaitPolicyFixture(t *testing.T, caller string, seconds int) (string, *db.Blackboard, string) {
	t.Helper()
	root, bb := setupReservationFixture(t)
	agentID := "reviewer-1"
	if caller == "doer" {
		agentID = "coder-1"
	}
	if err := bb.Modify(func(s *models.State) error {
		s.Config.AwaitPollInterval = seconds
		if caller == "doer" {
			task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReadyForReview, time.Now().UTC())
			task.History = append(task.History, models.TaskHistoryEntry{Time: time.Now().UTC(), Event: models.TaskEventSubmittedForReview, Agent: strPtr(agentID)})
			s.Tasks = []models.Task{task}
			s.Agents[agentID] = models.Agent{Role: "coder", Status: models.AgentStatusIdle}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return root, bb, agentID
}

// These tests replace process-global watcher constructors and run sequentially.
func TestAwaitPollingPolicyRoutes(t *testing.T) {
	for _, caller := range []string{"doer", "reviewer"} {
		for _, route := range []string{"event", "periodic", "fallback", "long-periodic", "deadline", "cancellation"} {
			t.Run(caller+"/"+route, func(t *testing.T) {
				seconds := 60
				if route == "periodic" || route == "fallback" {
					seconds = 1
				}
				root, bb, agentID := setupAwaitPolicyFixture(t, caller, seconds)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				inject := func() {
					t.Helper()
					if route == "cancellation" {
						cancel()
						return
					}
					if route == "deadline" {
						return
					}
					if err := bb.Modify(func(s *models.State) error {
						s.Config.Mode = models.SystemModePaused
						// A running wait keeps the interval captured before this write.
						s.Config.AwaitPollInterval = 60
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
				if caller == "doer" {
					previous := newAwaitVerdictWatcher
					t.Cleanup(func() { newAwaitVerdictWatcher = previous })
					newAwaitVerdictWatcher = func(bb *db.Blackboard) (awaitVerdictWatcher, error) {
						if route == "event" {
							watcher, err := previous(bb)
							if err != nil {
								return nil, err
							}
							inject()
							return watcher, nil
						}
						inject()
						if route == "fallback" {
							return nil, errors.New("test watcher unavailable")
						}
						return silentAwaitVerdictWatcher{}, nil
					}
				} else {
					previous := newAwaitResubmissionWatcher
					t.Cleanup(func() { newAwaitResubmissionWatcher = previous })
					newAwaitResubmissionWatcher = func(bb *db.Blackboard) (awaitResubmissionWatcher, error) {
						if route == "event" {
							watcher, err := previous(bb)
							if err != nil {
								return nil, err
							}
							inject()
							return watcher, nil
						}
						inject()
						if route == "fallback" {
							return nil, errors.New("test watcher unavailable")
						}
						return silentAwaitVerdictWatcher{}, nil
					}
				}
				timeout := 4 * time.Second
				if route == "deadline" {
					timeout = 30 * time.Millisecond
				} else if route == "long-periodic" {
					timeout = 2 * time.Second
				}
				verdict := ""
				var err error
				started := time.Now()
				if caller == "doer" {
					var result *AwaitVerdictResult
					result, err = AwaitVerdict(ctx, root, "task-1", agentID, timeout)
					if result != nil {
						verdict = result.Verdict
					}
				} else {
					var result *AwaitResubmissionResult
					result, err = AwaitResubmission(ctx, root, "task-1", agentID, timeout)
					if result != nil {
						verdict = result.Verdict
					}
				}
				if route == "cancellation" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error = %v, want cancellation before first tick", err)
					}
					return
				}
				want := VerdictPaused
				if caller == "reviewer" {
					want = ResubmissionPaused
				}
				if route == "deadline" {
					want = VerdictTimeout
					if caller == "reviewer" {
						want = ResubmissionTimeout
					}
				}
				// The deadline performs a fresh pause check. With a 60s cadence,
				// the silent watcher's pause must not be observed at the old 1s tick.
				if route == "long-periodic" && time.Since(started) < timeout {
					t.Fatal("pause observed by an early periodic check instead of the deadline")
				}
				if err != nil || verdict != want {
					t.Fatalf("outcome = %q, %v; want %s before the configured long interval", verdict, err, want)
				}
			})
		}
	}
}

func TestSetAwaitPollInterval(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	initial := testhelpers.CreateValidState()
	testhelpers.WriteInitialState(t, statePath, initial)
	for _, tc := range []struct {
		name                       string
		value                      int
		replace                    bool
		reason, outcome, wantError string
	}{
		{"set", 12, false, "", "set", ""},
		{"repeat", 12, false, "", "unchanged", ""},
		{"conflict", 20, false, "", "", "already set"},
		{"missing reason", 20, true, "", "", "--reason"},
		{"replace", 20, true, "less frequent checks", "replaced", ""},
		{"zero", 0, true, "invalid", "", "at least 1"},
		{"negative", -1, true, "invalid", "", "at least 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := readStateForTest(t, statePath)
			result, err := SetAwaitPollInterval(root, tc.value, tc.replace, tc.reason)
			after := readStateForTest(t, statePath)
			if tc.wantError != "" {
				var precondition *PreconditionError
				if !errors.As(err, &precondition) || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("error = %v, want %s", err, tc.wantError)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatal("refused configuration changed state")
				}
				return
			}
			if err != nil || result.Key != AwaitPollIntervalConfigKey || result.Outcome != tc.outcome || after.Config.AwaitPollInterval != tc.value {
				t.Fatalf("result=%+v err=%v stored=%d", result, err, after.Config.AwaitPollInterval)
			}
			wantSequence := before.MutationSequence
			if tc.outcome != "unchanged" {
				wantSequence++
			}
			if after.MutationSequence != wantSequence {
				t.Fatal("unexpected publication count")
			}
			after.MutationSequence, after.Config.AwaitPollInterval = before.MutationSequence, before.Config.AwaitPollInterval
			if !reflect.DeepEqual(before, after) {
				t.Fatal("configuration changed unrelated state")
			}
		})
	}
	for _, seconds := range invalidAwaitIntervals() {
		before := readStateForTest(t, statePath)
		if _, err := SetAwaitPollInterval(root, seconds, true, "invalid"); err == nil {
			t.Fatal("invalid duration accepted")
		}
		if !reflect.DeepEqual(before, readStateForTest(t, statePath)) {
			t.Fatal("invalid duration changed state")
		}
	}
}
