package ops

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

var inflightTestAuthority = models.AgentAuthority{ID: "coder-1", Generation: "generation-1"}

func TestInflightSubmitMarkerRoundTrip(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now().UTC()

	remove := writeInflightSubmitMarker(root, inflightTestAuthority, "task-1", 30*time.Second, now)

	deadline, ok := InflightSubmitDeadline(root, inflightTestAuthority, "task-1", now)
	if want := now.Add(30*time.Second + submitGraceMargin); !ok || !deadline.Equal(want) {
		t.Fatalf("InflightSubmitDeadline = (%s, %v), want (%s, true)", deadline, ok, want)
	}
	remove()
	if _, ok := InflightSubmitDeadline(root, inflightTestAuthority, "task-1", now); ok {
		t.Fatal("removed marker still reports an in-flight submit")
	}
	entries, err := os.ReadDir(paths.New(root).InflightSubmitDir())
	if err != nil || len(entries) != 0 {
		t.Fatalf("marker directory entries = %v, err = %v; want empty, no temporary files", entries, err)
	}
}

func TestInflightSubmitDeadlineRefusesMarkersItCannotTrust(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	valid := inflightSubmitMarker{TaskID: "task-1", PID: os.Getpid(), GenerationDigest: lifecycleDigest([]byte(inflightTestAuthority.Generation)), StartedAt: now, Deadline: now.Add(time.Minute)}
	cases := map[string]struct {
		mutate    func(*inflightSubmitMarker)
		taskID    string
		authority models.AgentAuthority
		raw       string
	}{
		"other task":       {taskID: "task-2"},
		"other generation": {authority: models.AgentAuthority{ID: inflightTestAuthority.ID, Generation: "generation-2"}},
		"expired deadline": {mutate: func(m *inflightSubmitMarker) { m.Deadline = now.Add(-time.Second) }},
		"deadline beyond the maximum grace": {mutate: func(m *inflightSubmitMarker) {
			m.Deadline = m.StartedAt.Add(MaxInflightSubmitGrace + time.Second)
		}},
		"dead process":         {mutate: func(m *inflightSubmitMarker) { m.PID = exitedProcessPID(t) }},
		"undecodable contents": {raw: "{"},
		"unsafe agent id":      {authority: models.AgentAuthority{ID: "../coder-1", Generation: inflightTestAuthority.Generation}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			marker := valid
			if tc.mutate != nil {
				tc.mutate(&marker)
			}
			data, err := json.Marshal(marker)
			if err != nil {
				t.Fatal(err)
			}
			if tc.raw != "" {
				data = []byte(tc.raw)
			}
			path := filepath.Join(paths.New(root).InflightSubmitDir(), inflightTestAuthority.ID+".fixture.json")
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0644); err != nil {
				t.Fatal(err)
			}
			taskID, authority := "task-1", inflightTestAuthority
			if tc.taskID != "" {
				taskID = tc.taskID
			}
			if tc.authority.ID != "" {
				authority = tc.authority
			}

			if deadline, ok := InflightSubmitDeadline(root, authority, taskID, now); ok {
				t.Fatalf("InflightSubmitDeadline accepted the marker (deadline %s)", deadline)
			}
		})
	}
}

// An older submit returning after a replacement registration of the same agent
// ID started its own submit must not erase the replacement's marker.
func TestInflightSubmitCleanupPreservesOverlappingInvocationMarker(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now().UTC()
	replacement := models.AgentAuthority{ID: inflightTestAuthority.ID, Generation: "generation-2"}
	removeOld := writeInflightSubmitMarker(root, inflightTestAuthority, "task-1", time.Minute, now)
	removeReplacement := writeInflightSubmitMarker(root, replacement, "task-2", time.Minute, now)
	defer removeReplacement()

	removeOld()

	if _, ok := InflightSubmitDeadline(root, replacement, "task-2", now); !ok {
		t.Fatal("the older submit's cleanup removed the replacement's marker")
	}
	if _, ok := InflightSubmitDeadline(root, inflightTestAuthority, "task-1", now); ok {
		t.Fatal("the older submit's marker survived its own cleanup")
	}
}

// Concurrent submits of one registration each keep the session alive; the
// latest deadline wins until its own submit ends.
func TestInflightSubmitDeadlineReportsLatestLiveMarker(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	now := time.Now().UTC()
	removeShort := writeInflightSubmitMarker(root, inflightTestAuthority, "task-1", time.Minute, now)
	defer removeShort()
	removeLong := writeInflightSubmitMarker(root, inflightTestAuthority, "task-1", 2*time.Minute, now)

	if deadline, ok := InflightSubmitDeadline(root, inflightTestAuthority, "task-1", now); !ok || !deadline.Equal(now.Add(2*time.Minute+submitGraceMargin)) {
		t.Fatalf("InflightSubmitDeadline = (%s, %v), want the longer marker's deadline", deadline, ok)
	}
	removeLong()
	if deadline, ok := InflightSubmitDeadline(root, inflightTestAuthority, "task-1", now); !ok || !deadline.Equal(now.Add(time.Minute+submitGraceMargin)) {
		t.Fatalf("InflightSubmitDeadline = (%s, %v), want the remaining marker's deadline", deadline, ok)
	}
}

func exitedProcessPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("run short-lived process: %v", err)
	}
	return cmd.Process.Pid
}

func TestInflightSubmitMarkerIsRemovedWhenValidationFails(t *testing.T) {
	t.Parallel()
	requirePosixShell(t)
	scenario := testhelpers.SetupAcceptanceSubmitScenario(t, "exit 3\n", 30)

	if _, err := SubmitForReviewWithAuthority(scenario.Root, scenario.TaskID, scenario.Commit, scenario.Authority); err == nil {
		t.Fatal("submit with a failing canonical command succeeded")
	}

	if _, ok := InflightSubmitDeadline(scenario.Root, scenario.Authority, scenario.TaskID, time.Now()); ok {
		t.Fatal("a failed submit left its in-flight marker behind")
	}
	if entries, err := os.ReadDir(paths.New(scenario.Root).InflightSubmitDir()); err == nil && len(entries) != 0 {
		t.Fatalf("marker directory entries = %v, want none", entries)
	}
}

func TestAcceptanceOwnershipFenceIgnoresUnreadableState(t *testing.T) {
	previous := acceptanceOwnershipPollInterval
	acceptanceOwnershipPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { acceptanceOwnershipPollInterval = previous })
	missing := db.For(filepath.Join(t.TempDir(), "missing", "state.yaml"))
	agentID := "coder-1"

	ctx, stop := startAcceptanceOwnershipFence(missing, "task-1", models.TaskStatusImplementing, agentID, nil)

	// Ten poll intervals of failed reads.
	select {
	case <-ctx.Done():
		t.Fatal("an unreadable state snapshot cancelled canonical validation")
	case <-time.After(100 * time.Millisecond):
	}
	if stop() {
		t.Fatal("fence reported lost ownership without observing it")
	}
	if ctx.Err() == nil {
		t.Fatal("stop did not release the fence context")
	}
}
