package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/referencecontract"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// archiveTestTask returns a task in status carrying a valid acceptance
// receipt whose command output is hostile to YAML block scalars.
func archiveTestTask(id string, status models.TaskStatus, now time.Time) models.Task {
	task := testhelpers.BuildTaskByStatus(id, status, now)
	source := models.AcceptanceSource{
		Ref: "specs/plan.md#Boundary", Commit: strings.Repeat("1", 40), Blob: strings.Repeat("2", 40),
		ParentTask: "plan-1", ParentReviewCommit: strings.Repeat("3", 40),
	}
	reviewCommit := strings.Repeat("4", 40)
	index := 0
	task.ReviewCommit = &reviewCommit
	task.AcceptanceSource = &source
	task.AcceptanceReceipt = &models.AcceptanceReceipt{
		Version: 1, ReviewCommit: reviewCommit, Source: source,
		ManifestPath: "acceptance/boundary.json", ManifestBlob: strings.Repeat("5", 40),
		Mappings: []referencecontract.AcceptanceMapping{{ObligationID: "AC-" + id, File: "tests/boundary.test", Assertion: "boundary holds", CommandIndex: &index}},
		Commands: []models.AcceptanceCommandResult{{
			Command: "run-tests --all", CommandSHA256: strings.Repeat("6", 64), ExitCode: 0,
			StartedAt: now, FinishedAt: now.Add(time.Second),
			Output: "  leading spaces\n|\n---\n\ttab: value\ntrailing newlines\n\n\n",
		}},
	}
	return task
}

func TestArchiveObjectPathRejectsInvalidDigests(t *testing.T) {
	root := t.TempDir()
	valid := strings.Repeat("ab", 32)
	got, err := archiveObjectPath(root, valid)
	if err != nil {
		t.Fatalf("archiveObjectPath(valid) error = %v", err)
	}
	want := filepath.Join(paths.New(root).ArchiveDir(), "objects", "ab", valid+".json")
	if got != want {
		t.Fatalf("archiveObjectPath(valid) = %q, want %q", got, want)
	}
	for _, sha := range []string{"", "../" + valid[3:], strings.ToUpper(valid), valid[:63], valid + "a", strings.Repeat("g", 64), "ab/" + valid[3:]} {
		if path, err := archiveObjectPath(root, sha); err == nil {
			t.Errorf("archiveObjectPath(%q) = %q, want rejection", sha, path)
		}
	}
}

func TestEncodeArchiveObjectIsDeterministicAndLossless(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 123456789, time.UTC)
	task := archiveTestTask("task-a", models.TaskStatusMerged, now)
	first, err := encodeArchiveObject(task.ID, task.AcceptanceReceipt)
	if err != nil {
		t.Fatalf("encodeArchiveObject() error = %v", err)
	}
	second, err := encodeArchiveObject(task.ID, task.AcceptanceReceipt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("archive object encoding is not deterministic")
	}
	var decoded struct {
		FormatVersion int                       `json:"format_version"`
		TaskID        string                    `json:"task_id"`
		Field         string                    `json:"field"`
		Value         *models.AcceptanceReceipt `json:"value"`
	}
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("archive object is not JSON: %v", err)
	}
	if decoded.FormatVersion != 1 || decoded.TaskID != task.ID || decoded.Field != models.ArchivedFieldAcceptanceReceipt || decoded.Value == nil {
		t.Fatalf("archive object envelope = %+v", decoded)
	}
	if decoded.Value.Commands[0].Output != task.AcceptanceReceipt.Commands[0].Output {
		t.Fatalf("command output changed: %q", decoded.Value.Commands[0].Output)
	}
	if !decoded.Value.Commands[0].StartedAt.Equal(now) {
		t.Fatalf("timestamp changed: %v", decoded.Value.Commands[0].StartedAt)
	}
}

func TestWriteArchiveObjectReusesIdenticalAndRejectsConflict(t *testing.T) {
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	data := []byte(`{"format_version":1}` + "\n")
	sha, err := writeArchiveObject(root, data)
	if err != nil {
		t.Fatalf("writeArchiveObject() error = %v", err)
	}
	path, err := archiveObjectPath(root, sha)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("object bytes = %q, %v", got, err)
	}
	if again, err := writeArchiveObject(root, data); err != nil || again != sha {
		t.Fatalf("rewrite identical object = %q, %v; want reuse of %q", again, err, sha)
	}
	if err := os.WriteFile(path, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeArchiveObject(root, data); !errors.Is(err, ErrArchiveObjectConflict) {
		t.Fatalf("write over differing object error = %v, want ErrArchiveObjectConflict", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "tampered" {
		t.Fatal("conflicting object was overwritten")
	}
}

// recordArchiveDirSyncs replaces the directory fsync with a recorder. fail, if
// non-nil, decides per call whether the sync fails.
func recordArchiveDirSyncs(t *testing.T, fail func(dir string) error) func() {
	t.Helper()
	previous := syncArchiveDir
	var synced []string
	syncArchiveDir = func(dir string) error {
		synced = append(synced, dir)
		if fail != nil {
			return fail(dir)
		}
		return nil
	}
	archiveDirSyncsForTest = &synced
	return func() {
		syncArchiveDir = previous
		archiveDirSyncsForTest = nil
	}
}

var archiveDirSyncsForTest *[]string

func TestWriteArchiveObjectSyncsEveryNewDirectoryOnEachWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory fsync is not available on Windows")
	}
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	archiveDir := paths.New(root).ArchiveDir()
	if _, err := os.Stat(archiveDir); !os.IsNotExist(err) {
		t.Fatalf("archive dir must start absent, stat err = %v", err)
	}
	data := []byte(`{"format_version":1,"x":1}` + "\n")
	sha, err := writeArchiveObject(root, data)
	if err != nil {
		t.Fatal(err)
	}
	chain := []string{
		filepath.Join(archiveDir, "objects", sha[:2]),
		filepath.Join(archiveDir, "objects"),
		archiveDir,
		filepath.Dir(archiveDir),
	}
	if got := *archiveDirSyncsForTest; !slices.Equal(got, chain) {
		t.Fatalf("directory syncs = %q, want leaf-to-root chain %q", got, chain)
	}
	*archiveDirSyncsForTest = nil
	if _, err := writeArchiveObject(root, data); err != nil {
		t.Fatal(err)
	}
	if got := *archiveDirSyncsForTest; !slices.Equal(got, chain) {
		t.Fatalf("reused object directory syncs = %q, want the full barrier %q", got, chain)
	}
}

func TestHydrateArchivedFieldsRejectsUnrestorableArchives(t *testing.T) {
	now := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	root := t.TempDir()
	testhelpers.SetupLizaDir(t, root)
	restore := recordArchiveDirSyncs(t, nil)
	defer restore()
	source := archiveTestTask("task-a", models.TaskStatusMerged, now)
	write := func(t *testing.T, data []byte) string {
		t.Helper()
		sha, err := writeArchiveObject(root, data)
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}
	good, err := encodeArchiveObject(source.ID, source.AcceptanceReceipt)
	if err != nil {
		t.Fatal(err)
	}
	goodSHA := write(t, good)
	compact := func(refs ...models.ArchivedFieldRef) models.Task {
		task := source
		task.AcceptanceReceipt = nil
		task.Archived = refs
		return task
	}
	ref := func(sha string) models.ArchivedFieldRef {
		return models.ArchivedFieldRef{Field: models.ArchivedFieldAcceptanceReceipt, SHA256: sha, ArchivedAt: now}
	}

	t.Run("restores", func(t *testing.T) {
		task := compact(ref(goodSHA))
		if err := HydrateArchivedFields(root, &task); err != nil {
			t.Fatalf("HydrateArchivedFields() error = %v", err)
		}
		if task.AcceptanceReceipt == nil || task.AcceptanceReceipt.Commands[0].Output != source.AcceptanceReceipt.Commands[0].Output {
			t.Fatalf("restored receipt = %+v", task.AcceptanceReceipt)
		}
	})

	envelope := func(t *testing.T, mutate func(map[string]any)) string {
		t.Helper()
		var object map[string]any
		if err := json.Unmarshal(good, &object); err != nil {
			t.Fatal(err)
		}
		mutate(object)
		data, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		return write(t, append(data, '\n'))
	}
	missingSHA := strings.Repeat("c", 64)
	tamperedSHA := envelope(t, func(o map[string]any) { o["task_id"] = "tampered-copy" })
	tamperedPath, _ := archiveObjectPath(root, tamperedSHA)
	if err := os.WriteFile(tamperedPath, good, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		task models.Task
	}{
		{"missing object", compact(ref(missingSHA))},
		{"digest mismatch", compact(ref(tamperedSHA))},
		{"other task", compact(ref(envelope(t, func(o map[string]any) { o["task_id"] = "task-b" })))},
		{"other field", compact(models.ArchivedFieldRef{Field: "history", SHA256: goodSHA, ArchivedAt: now})},
		{"envelope field mismatch", compact(ref(envelope(t, func(o map[string]any) { o["field"] = "history" })))},
		{"unknown envelope key", compact(ref(envelope(t, func(o map[string]any) { o["extra"] = true })))},
		{"bad version", compact(ref(envelope(t, func(o map[string]any) { o["format_version"] = 2 })))},
		{"null value", compact(ref(envelope(t, func(o map[string]any) { o["value"] = nil })))},
		{"missing value", compact(ref(envelope(t, func(o map[string]any) { delete(o, "value") })))},
		{"trailing value", compact(ref(write(t, append(append([]byte{}, good...), []byte(`{"extra":1}`+"\n")...))))},
		{"two refs for one field", compact(ref(goodSHA), ref(envelope(t, func(o map[string]any) { o["format_version"] = 1.0 })))},
		{"live value and ref", func() models.Task {
			task := compact(ref(goodSHA))
			task.AcceptanceReceipt = source.AcceptanceReceipt
			return task
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := tc.task
			err := HydrateArchivedFields(root, &task)
			var archiveErr *ArchiveObjectError
			if !errors.As(err, &archiveErr) {
				t.Fatalf("HydrateArchivedFields() error = %v, want *ArchiveObjectError", err)
			}
			if archiveErr.TaskID != source.ID {
				t.Fatalf("error task = %q, want %q", archiveErr.TaskID, source.ID)
			}
		})
	}
}
