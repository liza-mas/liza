package ops

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// releaseFixture builds lineages through the real supersession: a capped
// chain a -> b -> c (c BLOCKED at the cap) and one-recovery chain x -> y
// (y BLOCKED below the cap), plus a READY task r (D-146).
func releaseFixture(t *testing.T) (root, statePath string) {
	t.Helper()
	root = t.TempDir()
	statePath, _ = testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = nil
	testhelpers.WriteInitialState(t, statePath, state)

	clock := time.Now().UTC().Add(-time.Hour)
	modify := func(fn func(*models.State)) {
		t.Helper()
		if err := db.New(statePath).Modify(func(s *models.State) error { fn(s); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	block := func(id string) {
		t.Helper()
		clock = clock.Add(time.Minute)
		modify(func(s *models.State) {
			blocked := testhelpers.BuildTaskByStatus(id, models.TaskStatusBlocked, clock)
			reason := id + " blocked on the shared interface"
			blocked.BlockedReason = &reason
			if existing := s.FindTask(id); existing != nil {
				blocked.History = existing.History
				*existing = blocked
			} else {
				s.Tasks = append(s.Tasks, blocked)
			}
			task := s.FindTask(id)
			task.History = append(task.History, models.TaskHistoryEntry{Time: clock, Event: models.TaskEventBlocked})
		})
	}
	replace := func(id, replacement string) {
		t.Helper()
		modify(func(s *models.State) {
			s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus(replacement, models.TaskStatusReady, clock))
		})
		if _, err := SupersedeTaskWithOptions(root, id, []string{replacement}, "replace blocked task", "orchestrator-1",
			SupersedeTaskOptions{Changed: "new approach for " + replacement}); err != nil {
			t.Fatalf("supersede %s: %v", id, err)
		}
	}
	block("a")
	replace("a", "b")
	block("b")
	replace("b", "c")
	block("c")
	block("x")
	replace("x", "y")
	block("y")
	modify(func(s *models.State) {
		s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("r", models.TaskStatusReady, clock))
	})
	return root, statePath
}

func readReleaseState(t *testing.T, statePath string) *models.State {
	t.Helper()
	state, err := db.New(statePath).Read()
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func releaseEntries(task *models.Task) []models.TaskHistoryEntry {
	var entries []models.TaskHistoryEntry
	for _, entry := range task.History {
		if entry.Event == models.TaskEventBlockedRecoveryReleased {
			entries = append(entries, entry)
		}
	}
	return entries
}

func TestReleaseBlockedChainRefusalsLeaveStateUnchanged(t *testing.T) {
	cases := []struct {
		name, task, reason, want string
		setup                    func(t *testing.T, root, statePath string)
	}{
		{name: "unknown task", task: "nope", reason: "ruled", want: "not found"},
		{name: "empty reason", task: "c", reason: "  ", want: "reason is required"},
		{name: "not BLOCKED", task: "r", reason: "ruled", want: "not BLOCKED"},
		{name: "below the cap", task: "y", reason: "ruled", want: "not at the blocked-recovery cap (1 of 2"},
		{name: "already released", task: "c", reason: "ruled again", want: "already released",
			setup: func(t *testing.T, root, _ string) {
				if _, err := ReleaseBlockedChain(root, "c", "ruled"); err != nil {
					t.Fatal(err)
				}
			}},
		{name: "no recorded episode", task: "c", reason: "ruled", want: "no recorded blocked episode",
			setup: func(t *testing.T, _, statePath string) {
				if err := db.New(statePath).Modify(func(s *models.State) error {
					c := s.FindTask("c")
					c.History = slices.DeleteFunc(c.History, func(e models.TaskHistoryEntry) bool {
						transition, _ := models.IsStatusTransitionEvent(e.Event)
						return transition
					})
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, statePath := releaseFixture(t)
			if tc.setup != nil {
				tc.setup(t, root, statePath)
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ReleaseBlockedChain(root, tc.task, tc.reason); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("refused release changed state")
			}
		})
	}
}

func TestReleaseBlockedChainRecordsMaskedReleaseWithoutPausing(t *testing.T) {
	const secret = "sk-ant-d146-release-secret"
	t.Setenv("ANTHROPIC_API_KEY", secret)
	root, statePath := releaseFixture(t)

	before := readReleaseState(t, statePath)
	c := before.FindTask("c")
	// The orchestrator already held c at the cap; nothing woke it since.
	candidate := currentBlockerCandidate(before, c)
	if err := db.New(statePath).Modify(func(s *models.State) error {
		task := s.FindTask("c")
		task.History = append(task.History, models.TaskHistoryEntry{
			Time: time.Now().UTC(), Event: models.TaskEventOrchestratorAssessment,
			Extra: map[string]any{AssessmentFingerprintExtraKey: BuildAssessmentFingerprint(s, task, candidate)},
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before = readReleaseState(t, statePath)
	if slices.Contains(ActionableBlockedTaskIDs(before), "c") {
		t.Fatal("assessed capped task already actionable before release")
	}
	breakerBefore, _ := yaml.Marshal(before.CircuitBreaker)

	result, err := ReleaseBlockedChain(root, "c", "human ruled one more attempt with token "+secret)
	if err != nil {
		t.Fatalf("ReleaseBlockedChain() error: %v", err)
	}
	if !slices.Equal(result.Lineage, []string{"a", "b", "c"}) || !slices.Equal(result.Recoveries, []string{"a", "b"}) || !result.BlockedAt.Equal(models.BlockedEpisodeAt(before.FindTask("c"))) {
		t.Fatalf("result = %+v", result)
	}

	after := readReleaseState(t, statePath)
	task := after.FindTask("c")
	entries := releaseEntries(task)
	if len(entries) != 1 {
		t.Fatalf("release entries = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Reason == nil || strings.Contains(*entry.Reason, secret) || !strings.Contains(*entry.Reason, "***") {
		t.Fatalf("history reason = %v, want masked", entry.Reason)
	}
	if entry.Extra["operation"] != "release-blocked-chain" || entry.Extra["source"] != "operator_cli" ||
		entry.Extra["blocked_at"] != result.BlockedAt.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("history extra = %v", entry.Extra)
	}
	if _, capped := models.BlockedRecoveryCapped(after, task); capped {
		t.Fatal("released task still capped")
	}
	if task.Status != models.TaskStatusBlocked || after.Config.Mode != before.Config.Mode || after.Sprint.Status != before.Sprint.Status {
		t.Fatalf("status/mode/sprint changed: %s %s %s", task.Status, after.Config.Mode, after.Sprint.Status)
	}
	if breakerAfter, _ := yaml.Marshal(after.CircuitBreaker); !bytes.Equal(breakerBefore, breakerAfter) {
		t.Fatalf("circuit breaker changed:\n%s\nwant\n%s", breakerAfter, breakerBefore)
	}
	if !slices.Contains(ActionableBlockedTaskIDs(after), "c") {
		t.Fatal("release did not make the held task actionable for the orchestrator")
	}

	activity, err := os.ReadFile(paths.New(root).LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(activity), string(models.TaskEventBlockedRecoveryReleased)) || strings.Contains(string(activity), secret) {
		t.Fatalf("activity log missing release or leaking secret:\n%s", activity)
	}
}

func TestReleaseBlockedChainConcurrentDuplicateRecordsOnce(t *testing.T) {
	root, statePath := releaseFixture(t)
	const callers = 4
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ReleaseBlockedChain(root, "c", "ruled")
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !strings.Contains(err.Error(), "already released"):
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("%d releases succeeded, want 1", succeeded)
	}
	if entries := releaseEntries(readReleaseState(t, statePath).FindTask("c")); len(entries) != 1 {
		t.Fatalf("release entries = %d, want 1", len(entries))
	}
}
