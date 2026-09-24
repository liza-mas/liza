package ops

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
	"gopkg.in/yaml.v3"
)

// setupArchiveProject writes a state holding tasks and returns the project
// root and state path.
func setupArchiveProject(t *testing.T, tasks ...models.Task) (string, string) {
	t.Helper()
	root := t.TempDir()
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	state := testhelpers.CreateValidState()
	state.Tasks = tasks
	testhelpers.WriteInitialState(t, statePath, state)
	return root, statePath
}

func taskYAML(t *testing.T, task models.Task) string {
	t.Helper()
	data, err := yaml.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestArchiveTerminalAcceptanceReceiptsIsLosslessAndTerminalOnly(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupArchiveProject(t,
		archiveTestTask("merged", models.TaskStatusMerged, now),
		archiveTestTask("superseded", models.TaskStatusSuperseded, now),
		archiveTestTask("abandoned", models.TaskStatusAbandoned, now),
		archiveTestTask("active", models.TaskStatusReadyForReview, now),
	)
	before := readStateForTest(t, statePath)

	result, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit)
	if err != nil {
		t.Fatalf("ArchiveTerminalAcceptanceReceipts() error = %v", err)
	}
	if want := []string{"merged", "superseded", "abandoned"}; !slices.Equal(result.Archived, want) || result.Remaining != 0 || result.Bytes <= 0 {
		t.Fatalf("result = %+v, want archived %v and nothing remaining", result, want)
	}
	after := readStateForTest(t, statePath)
	if active := after.FindTask("active"); active.AcceptanceReceipt == nil || len(active.Archived) != 0 {
		t.Fatalf("active task was archived: %+v", active)
	}
	for _, id := range result.Archived {
		compact := *after.FindTask(id)
		if compact.AcceptanceReceipt != nil || len(compact.Archived) != 1 || compact.Archived[0].Field != models.ArchivedFieldAcceptanceReceipt || compact.Archived[0].ArchivedAt.IsZero() {
			t.Fatalf("task %s compact form = receipt %v, refs %+v", id, compact.AcceptanceReceipt, compact.Archived)
		}
		if err := HydrateArchivedFields(root, &compact); err != nil {
			t.Fatalf("hydrate %s: %v", id, err)
		}
		compact.Archived = nil
		if got, want := taskYAML(t, compact), taskYAML(t, *before.FindTask(id)); got != want {
			t.Fatalf("task %s round trip differs:\n got: %s\nwant: %s", id, got, want)
		}
	}
	again, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit)
	if err != nil || len(again.Archived) != 0 || again.Remaining != 0 {
		t.Fatalf("second run = %+v, %v; want a no-op", again, err)
	}
}

func TestArchiveTerminalAcceptanceReceiptsHonorsLimits(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, _ := setupArchiveProject(t,
		archiveTestTask("t1", models.TaskStatusMerged, now),
		archiveTestTask("t2", models.TaskStatusMerged, now),
		archiveTestTask("t3", models.TaskStatusMerged, now),
	)
	first, err := ArchiveTerminalAcceptanceReceipts(root, nil, ArchiveLimit{MaxTasks: 2, MaxBytes: 1 << 20})
	if err != nil || !slices.Equal(first.Archived, []string{"t1", "t2"}) || first.Remaining != 1 {
		t.Fatalf("task-limited run = %+v, %v", first, err)
	}
}

func TestArchiveTerminalAcceptanceReceiptsByteBudgetIsSoft(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupArchiveProject(t,
		archiveTestTask("t1", models.TaskStatusMerged, now),
		archiveTestTask("t2", models.TaskStatusMerged, now),
		archiveTestTask("t3", models.TaskStatusMerged, now),
	)
	// The byte budget stops the batch even though the task budget allows all
	// three; it is soft only in that the first oversize object still progresses.
	result, err := ArchiveTerminalAcceptanceReceipts(root, nil, ArchiveLimit{MaxTasks: 8, MaxBytes: 1})
	if err != nil || !slices.Equal(result.Archived, []string{"t1"}) || result.Remaining != 2 {
		t.Fatalf("byte-limited run = %+v, %v; want only t1 archived and 2 remaining", result, err)
	}
	state := readStateForTest(t, statePath)
	for _, id := range []string{"t2", "t3"} {
		if task := state.FindTask(id); task.AcceptanceReceipt == nil || len(task.Archived) != 0 {
			t.Fatalf("task %s archived beyond the byte budget", id)
		}
	}
}

// A batch stops at the first task that does not fit, even when a later,
// smaller task would: batches follow state order and encode nothing beyond.
func TestArchiveTerminalAcceptanceReceiptsBatchStopsAtFirstNonFittingTask(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	small1 := archiveTestTask("t1", models.TaskStatusMerged, now)
	large := archiveTestTask("t2", models.TaskStatusMerged, now)
	large.AcceptanceReceipt.Commands[0].Output = strings.Repeat("x", 4096)
	small3 := archiveTestTask("t3", models.TaskStatusMerged, now)
	size := func(task models.Task) int {
		data, err := encodeArchiveObject(task.ID, task.AcceptanceReceipt)
		if err != nil {
			t.Fatal(err)
		}
		return len(data)
	}
	budget := size(small1) + size(small3)
	if size(large) <= size(small3) {
		t.Fatal("fixture: t2 must be larger than t3")
	}
	root, statePath := setupArchiveProject(t, small1, large, small3)
	result, err := ArchiveTerminalAcceptanceReceipts(root, nil, ArchiveLimit{MaxTasks: 8, MaxBytes: budget})
	if err != nil || !slices.Equal(result.Archived, []string{"t1"}) || result.Remaining != 2 {
		t.Fatalf("result = %+v, %v; want only t1 archived and 2 remaining", result, err)
	}
	if task := readStateForTest(t, statePath).FindTask("t3"); task.AcceptanceReceipt == nil {
		t.Fatal("selection skipped ahead of the non-fitting t2 to archive t3")
	}
}

func TestArchiveTerminalAcceptanceReceiptsAbortedTransactionReusesObjects(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupArchiveProject(t, archiveTestTask("merged", models.TaskStatusMerged, now))
	beforeBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected failure after objects")
	archiveAfterObjectsTestHook = func() error { return injected }
	t.Cleanup(func() { archiveAfterObjectsTestHook = nil })
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); !errors.Is(err, injected) {
		t.Fatalf("aborted run error = %v, want %v", err, injected)
	}
	if afterBytes, _ := os.ReadFile(statePath); !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("aborted transaction changed state")
	}
	objects := archiveObjectFiles(t, root)
	if len(objects) != 1 {
		t.Fatalf("objects after abort = %v, want the one durable orphan", objects)
	}
	archiveAfterObjectsTestHook = nil
	result, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit)
	if err != nil || len(result.Archived) != 1 {
		t.Fatalf("retry = %+v, %v", result, err)
	}
	if got := archiveObjectFiles(t, root); !slices.Equal(got, objects) {
		t.Fatalf("retry objects = %v, want reuse of %v", got, objects)
	}
	if ref := readStateForTest(t, statePath).FindTask("merged").Archived[0]; filepath.Base(objects[0]) != ref.SHA256+".json" {
		t.Fatalf("stub %s does not name the reused object %s", ref.SHA256, objects[0])
	}
}

func TestArchiveTerminalAcceptanceReceiptsBarrierFailureRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory fsync is not available on Windows")
	}
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	failing := true
	restore := recordArchiveDirSyncs(t, func(string) error {
		if failing {
			return errors.New("injected directory sync failure")
		}
		return nil
	})
	defer restore()
	root, statePath := setupArchiveProject(t, archiveTestTask("merged", models.TaskStatusMerged, now))
	beforeBytes, _ := os.ReadFile(statePath)
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err == nil {
		t.Fatal("barrier failure did not abort the archive")
	}
	if afterBytes, _ := os.ReadFile(statePath); !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("barrier failure published a stub")
	}
	failing = false
	*archiveDirSyncsForTest = nil
	if result, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err != nil || len(result.Archived) != 1 {
		t.Fatalf("retry = %+v, %v", result, err)
	}
	if len(*archiveDirSyncsForTest) == 0 {
		t.Fatal("retry reused the object without re-running the durability barrier")
	}
}

func TestArchiveTerminalAcceptanceReceiptsStatePublicationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions do not block publication on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupArchiveProject(t, archiveTestTask("merged", models.TaskStatusMerged, now))
	beforeBytes, _ := os.ReadFile(statePath)
	stateDir := filepath.Dir(statePath)
	archiveAfterObjectsTestHook = func() error { return os.Chmod(stateDir, 0o555) }
	t.Cleanup(func() {
		archiveAfterObjectsTestHook = nil
		_ = os.Chmod(stateDir, 0o755)
	})
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err == nil {
		t.Fatal("state publication failure was not reported")
	}
	if afterBytes, _ := os.ReadFile(statePath); !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatal("failed publication changed state")
	}
	objects := archiveObjectFiles(t, root)
	if len(objects) != 1 {
		t.Fatalf("objects after failed publication = %v, want one durable object", objects)
	}
	archiveAfterObjectsTestHook = nil
	if err := os.Chmod(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if result, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err != nil || len(result.Archived) != 1 {
		t.Fatalf("retry = %+v, %v", result, err)
	}
	if got := archiveObjectFiles(t, root); !slices.Equal(got, objects) {
		t.Fatalf("retry objects = %v, want reuse of %v", got, objects)
	}
}

func TestArchiveTerminalAcceptanceReceiptsOldSnapshotStaysRestorable(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	root, statePath := setupArchiveProject(t,
		archiveTestTask("t1", models.TaskStatusMerged, now),
		archiveTestTask("t2", models.TaskStatusMerged, now),
	)
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, ArchiveLimit{MaxTasks: 1, MaxBytes: 1 << 20}); err != nil {
		t.Fatal(err)
	}
	snapshot := readStateForTest(t, statePath)
	if _, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit); err != nil {
		t.Fatal(err)
	}
	old := *snapshot.FindTask("t1")
	if err := HydrateArchivedFields(root, &old); err != nil || old.AcceptanceReceipt == nil {
		t.Fatalf("old snapshot hydration = %v, receipt %v", err, old.AcceptanceReceipt)
	}
}

func TestArchiveTerminalAcceptanceReceiptsNoWorkTakesNoLock(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	root, statePath := setupArchiveProject(t, archiveTestTask("active", models.TaskStatusReadyForReview, now))
	release := testhelpers.HoldFileLock(t, statePath+".lock")
	defer release()
	restoreTimeout := db.SetDefaultLockTimeoutForTest(200 * time.Millisecond)
	defer restoreTimeout()
	result, err := ArchiveTerminalAcceptanceReceipts(root, nil, DefaultArchiveLimit)
	if err != nil || len(result.Archived) != 0 || result.Remaining != 0 {
		t.Fatalf("no-work run under a held lock = %+v, %v; want an immediate no-op", result, err)
	}
	if _, err := os.Stat(paths.New(root).ArchiveDir()); !os.IsNotExist(err) {
		t.Fatalf("no-work run created the archive directory: %v", err)
	}
}

func archiveObjectFiles(t *testing.T, root string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(paths.New(root).ArchiveDir(), "objects", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(matches)
	return matches
}
