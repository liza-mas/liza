package ops

import (
	"encoding/json"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/statevalidate"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// C5 pins the zone of the two merge timestamps written by recordMergeCompletion
// (merge_finalize.go): the completion handoff timestamp and the merged history
// time. Only the zone of newly written values changes; stored offsets are never
// rewritten and still load as the same instant.

const (
	c5ChildMarker = "C5_UTC_CHILD"
	// c5ChildZone is a fixed +02:00 zone without daylight saving.
	c5ChildZone    = "Etc/GMT-2"
	c5OffsetSuffix = "+02:00"
)

// c5Offset is the zone of the local-offset fixture values.
var c5Offset = time.FixedZone("", 2*60*60)

// c5RawTask is the on-disk shape of the two persisted merge timestamps, read
// without the models decoder so a test sees the stored scalar, not an instant.
type c5RawTask struct {
	ID            string `yaml:"id"`
	HandoffEvents []struct {
		Trigger   string `yaml:"trigger"`
		Timestamp string `yaml:"timestamp"`
	} `yaml:"handoff_events"`
	History []struct {
		Event string `yaml:"event"`
		Time  string `yaml:"time"`
	} `yaml:"history"`
}

// c5Scalars returns the raw completion handoff timestamp and merged history
// time of task.
func (task c5RawTask) c5Scalars(t *testing.T) (handoff, merged string) {
	t.Helper()
	for _, event := range task.HandoffEvents {
		if event.Trigger == string(models.HandoffTriggerCompletion) {
			handoff = event.Timestamp
		}
	}
	for _, entry := range task.History {
		if entry.Event == string(models.TaskEventMerged) {
			merged = entry.Time
		}
	}
	if handoff == "" || merged == "" {
		t.Fatalf("task %s: completion handoff present = %t, merged history present = %t", task.ID, handoff != "", merged != "")
	}
	return handoff, merged
}

// c5StateScalars reads the raw scalars of taskID from state.yaml.
func c5StateScalars(t *testing.T, statePath, taskID string) (handoff, merged string) {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var raw struct {
		Tasks []c5RawTask `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode raw state: %v", err)
	}
	for _, task := range raw.Tasks {
		if task.ID == taskID {
			return task.c5Scalars(t)
		}
	}
	t.Fatalf("task %s absent from raw state", taskID)
	return "", ""
}

// c5OffsetMergedTask is a MERGED task, valid for full validation (it carries a
// submission handoff), whose two merge timestamps are the instant written with
// a +02:00 offset.
func c5OffsetMergedTask(id string, instant time.Time) models.Task {
	task := archiveTestTask(id, models.TaskStatusMerged, instant)
	local := instant.In(c5Offset)
	for i := range task.HandoffEvents {
		if task.HandoffEvents[i].Trigger == models.HandoffTriggerCompletion {
			task.HandoffEvents[i].Timestamp = local
		}
	}
	agent, commit := "code-reviewer-1", "merge456"
	task.History = append(task.History, models.TaskHistoryEntry{
		Time: local, Event: models.TaskEventMerged, Agent: &agent, Commit: &commit,
	})
	return task
}

// c5RequireOffsetInstants fails unless task's two merge timestamps equal instant.
func c5RequireOffsetInstants(t *testing.T, task *models.Task, instant time.Time) {
	t.Helper()
	if task == nil {
		t.Fatal("task absent after read")
	}
	var handoffOK, mergedOK bool
	for _, event := range task.HandoffEvents {
		if event.Trigger == models.HandoffTriggerCompletion {
			handoffOK = event.Timestamp.Equal(instant)
		}
	}
	for _, entry := range task.History {
		if entry.Event == models.TaskEventMerged {
			mergedOK = entry.Time.Equal(instant)
		}
	}
	if !handoffOK || !mergedOK {
		t.Fatalf("task %s: completion handoff instant equal = %t, merged history instant equal = %t, want both (fixture instant %s)",
			task.ID, handoffOK, mergedOK, instant.Format(time.RFC3339))
	}
}

func c5RequireOffsetScalars(t *testing.T, where, handoff, merged string) {
	t.Helper()
	if !strings.HasSuffix(handoff, c5OffsetSuffix) || !strings.HasSuffix(merged, c5OffsetSuffix) {
		t.Fatalf("%s: completion handoff %q and merged history %q must both end in %s", where, handoff, merged, c5OffsetSuffix)
	}
}

// c5LaterWrite registers an unrelated agent through a state write.
func c5LaterWrite(t *testing.T, statePath string, at time.Time) {
	t.Helper()
	err := db.New(statePath).Modify(func(s *models.State) error {
		s.Agents["c5-later-writer"] = models.Agent{
			Role: "coder", Status: models.AgentStatusIdle, Heartbeat: at, RegisteredAt: at, Terminal: "terminal-1",
		}
		return nil
	})
	if err != nil {
		t.Fatalf("later state write: %v", err)
	}
	if _, ok := readStateForTest(t, statePath).Agents["c5-later-writer"]; !ok {
		t.Fatal("later write did not persist its agent")
	}
}

func TestC5MergeTimestampsAreUTCInMemory(t *testing.T) {
	t.Parallel()
	task := &models.Task{ID: "c5-memory"}

	before := time.Now().UTC()
	recordMergeCompletion(task, "code-reviewer-1", "merge-commit", nil)
	after := time.Now().UTC()

	if len(task.HandoffEvents) != 1 || len(task.History) != 1 {
		t.Fatalf("handoff events = %d, history entries = %d, want 1 and 1", len(task.HandoffEvents), len(task.History))
	}
	handoff, merged := task.HandoffEvents[0].Timestamp, task.History[0].Time
	if handoff.Location() != time.UTC {
		t.Errorf("handoff_events.timestamp location is UTC = false")
	}
	if merged.Location() != time.UTC {
		t.Errorf("history.time location is UTC = false")
	}
	if handoff.Before(before) || handoff.After(after) {
		t.Errorf("handoff_events.timestamp %s outside [%s, %s]", handoff.Format(time.RFC3339Nano), before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
	}
	if merged.Before(before) || merged.After(after) {
		t.Errorf("history.time %s outside [%s, %s]", merged.Format(time.RFC3339Nano), before.Format(time.RFC3339Nano), after.Format(time.RFC3339Nano))
	}
}

// c5ChildEnvironment builds the child's environment from names only: the host
// environment is never forwarded wholesale.
func c5ChildEnvironment() []string {
	env := []string{c5ChildMarker + "=1", "TZ=" + c5ChildZone}
	for _, name := range []string{"HOME", "PATH", "TMPDIR", "TEMP", "TMP", "SystemRoot", "USERPROFILE"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// TestC5MergeTimestampsPersistWithZUnderNonUTCZone runs a real merge in a child
// test process whose local zone is +02:00. Go fixes time.Local at first use, so
// the zone cannot change inside this process without a data race.
func TestC5MergeTimestampsPersistWithZUnderNonUTCZone(t *testing.T) {
	if os.Getenv(c5ChildMarker) == "1" {
		c5PersistWithZChild(t)
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("Go on Windows takes the local zone from the system and ignores TZ; TestC5MergeTimestampsAreUTCInMemory covers the zone there")
	}
	t.Parallel()

	const name = "TestC5MergeTimestampsPersistWithZUnderNonUTCZone"
	cmd := exec.Command(os.Args[0], "-test.run=^"+name+"$", "-test.v")
	cmd.Env = c5ChildEnvironment()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child process failed: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--- PASS: "+name) {
		t.Fatalf("child process did not run %s to a PASS line:\n%s", name, output)
	}
}

func c5PersistWithZChild(t *testing.T) {
	if _, offset := time.Now().Zone(); offset == 0 {
		t.Fatalf("child local zone offset is zero: the %s zone was not applied", c5ChildZone)
	}
	recordIndexRefreshes(t, nil)

	const mergedID, seededID, agentID = "c5-persist", "c5-seeded", "coder-1"
	root, statePath := setupMergeTestRepo(t, mergedID, agentID)
	instant := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)
	state := readStateForTest(t, statePath)
	seeded := c5OffsetMergedTask(seededID, instant)
	state.Tasks = append(state.Tasks, seeded)
	testhelpers.WriteInitialState(t, statePath, state)

	seededHandoff, seededMerged := c5StateScalars(t, statePath, seededID)
	c5RequireOffsetScalars(t, "seeded task before merge", seededHandoff, seededMerged)

	if _, err := MergeWorktree(root, mergedID, agentID); err != nil {
		t.Fatalf("MergeWorktree() error = %v", err)
	}

	handoff, merged := c5StateScalars(t, statePath, mergedID)
	if !strings.HasSuffix(handoff, "Z") || !strings.HasSuffix(merged, "Z") {
		t.Errorf("merged task: completion handoff %q and merged history %q must both end in Z", handoff, merged)
	}
	gotHandoff, gotMerged := c5StateScalars(t, statePath, seededID)
	if gotHandoff != seededHandoff || gotMerged != seededMerged {
		t.Errorf("seeded task scalars changed by the merge write: handoff %q -> %q, merged %q -> %q", seededHandoff, gotHandoff, seededMerged, gotMerged)
	}
	c5RequireOffsetInstants(t, readStateForTest(t, statePath).FindTask(seededID), instant)
}

func TestC5LocalOffsetMergeTimestampsLoadAndSurviveWrites(t *testing.T) {
	instant := time.Date(2026, 10, 6, 8, 0, 0, 0, time.UTC)

	t.Run("live", func(t *testing.T) {
		const id = "c5-live"
		_, statePath := setupArchiveProject(t, c5OffsetMergedTask(id, instant))
		handoff, merged := c5StateScalars(t, statePath, id)
		c5RequireOffsetScalars(t, "live state", handoff, merged)

		c5RequireOffsetInstants(t, readStateForTest(t, statePath).FindTask(id), instant)
		if err := statevalidate.ValidateStateFile(statePath, true, nil); err != nil {
			t.Fatalf("ValidateStateFile() error = %v", err)
		}

		c5LaterWrite(t, statePath, instant)
		gotHandoff, gotMerged := c5StateScalars(t, statePath, id)
		if gotHandoff != handoff || gotMerged != merged {
			t.Errorf("live scalars changed by a later write: handoff %q -> %q, merged %q -> %q", handoff, gotHandoff, merged, gotMerged)
		}
	})

	t.Run("archived", func(t *testing.T) {
		const id = "c5-archived"
		root, statePath := setupArchiveProject(t, c5OffsetMergedTask(id, instant))
		result, err := EnableAndArchiveTerminalTasks(root, DefaultArchiveLimit)
		if err != nil || len(result.Archived) != 1 || result.Archived[0] != id {
			t.Fatalf("EnableAndArchiveTerminalTasks() = %+v, %v; want %s archived", result, err, id)
		}

		digest := c5ArchiveDigest(t, statePath, id)
		objectScalars := func() (string, string) {
			path, err := archiveObjectPath(root, digest)
			if err != nil {
				t.Fatalf("archive object path: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read archive object: %v", err)
			}
			var object struct {
				ValueYAML string `json:"value_yaml"`
			}
			if err := json.Unmarshal(data, &object); err != nil {
				t.Fatalf("decode archive object: %v", err)
			}
			var task c5RawTask
			if err := yaml.Unmarshal([]byte(object.ValueYAML), &task); err != nil {
				t.Fatalf("decode archived task: %v", err)
			}
			return task.c5Scalars(t)
		}
		handoff, merged := objectScalars()
		c5RequireOffsetScalars(t, "archive object", handoff, merged)

		restored := readStateForTest(t, statePath).FindTask(id)
		if restored == nil || restored.TerminalArchive == nil {
			t.Fatal("Read did not restore the archived task with its archive reference")
		}
		c5RequireOffsetInstants(t, restored, instant)
		if err := statevalidate.ValidateStateFile(statePath, true, nil); err != nil {
			t.Fatalf("ValidateStateFile() error = %v", err)
		}

		c5LaterWrite(t, statePath, instant)
		if got := c5ArchiveDigest(t, statePath, id); got != digest {
			t.Errorf("archive digest changed by a later write: %q -> %q", digest, got)
		}
		gotHandoff, gotMerged := objectScalars()
		if gotHandoff != handoff || gotMerged != merged {
			t.Errorf("archive object scalars changed by a later write: handoff %q -> %q, merged %q -> %q", handoff, gotHandoff, merged, gotMerged)
		}
	})
}

// c5ArchiveDigest returns the archive digest of taskID from the state stub.
func c5ArchiveDigest(t *testing.T, statePath, taskID string) string {
	t.Helper()
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var raw struct {
		Tasks []struct {
			ID              string `yaml:"id"`
			TerminalArchive *struct {
				SHA256 string `yaml:"sha256"`
			} `yaml:"terminal_archive"`
		} `yaml:"tasks"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatalf("decode raw state: %v", err)
	}
	for _, task := range raw.Tasks {
		if task.ID == taskID && task.TerminalArchive != nil && task.TerminalArchive.SHA256 != "" {
			return task.TerminalArchive.SHA256
		}
	}
	t.Fatalf("task %s has no archive stub in state", taskID)
	return ""
}
