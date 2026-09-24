package ops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// Terminal-task fields leave live state as immutable, content-addressed
// objects under <runtime>/archive/objects/<sha[0:2]>/<sha>.json. The object is
// named by the SHA-256 of its exact bytes and never rewritten, so every state
// snapshot that references it stays restorable. Objects are JSON because
// command output is exactly the text YAML block scalars mangle.

const archiveObjectFormatVersion = 1

// ErrArchiveObjectConflict reports an existing archive object whose bytes
// differ from the object being written under the same digest name.
var ErrArchiveObjectConflict = errors.New("archive object conflict: existing object differs")

// ArchiveObjectError reports an archived field that cannot be restored. It is
// never downgraded to an absent field.
type ArchiveObjectError struct {
	TaskID string
	SHA256 string
	Path   string
	Reason string
}

func (e *ArchiveObjectError) Error() string {
	return fmt.Sprintf("task %s archived object %s (%s): %s", e.TaskID, e.SHA256, e.Path, e.Reason)
}

type archiveObject struct {
	FormatVersion int                       `json:"format_version"`
	TaskID        string                    `json:"task_id"`
	Field         string                    `json:"field"`
	Value         *models.AcceptanceReceipt `json:"value"`
}

// syncArchiveDir is the directory fsync primitive; tests replace it to record
// or fail the durability barrier.
var syncArchiveDir = syncDir

func syncDir(dir string) error {
	// Windows cannot open a directory for fsync; crash ordering there is
	// best-effort, as for state publication itself.
	if runtime.GOOS == "windows" {
		return nil
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(syncErr, closeErr)
}

func isArchiveDigest(sha string) bool {
	if len(sha) != sha256.Size*2 {
		return false
	}
	for _, ch := range sha {
		if !('0' <= ch && ch <= '9') && !('a' <= ch && ch <= 'f') {
			return false
		}
	}
	return true
}

// archiveObjectPath derives an object's path from its digest. Only a
// lowercase SHA-256 is accepted, so state cannot direct I/O elsewhere.
func archiveObjectPath(projectRoot, sha string) (string, error) {
	if !isArchiveDigest(sha) {
		return "", fmt.Errorf("invalid archive object digest %q", sha)
	}
	return filepath.Join(paths.New(projectRoot).ArchiveDir(), "objects", sha[:2], sha+".json"), nil
}

// encodeArchiveObject returns the deterministic bytes of a receipt object.
func encodeArchiveObject(taskID string, receipt *models.AcceptanceReceipt) ([]byte, error) {
	if receipt == nil {
		return nil, fmt.Errorf("task %s has no acceptance receipt to archive", taskID)
	}
	data, err := json.Marshal(archiveObject{
		FormatVersion: archiveObjectFormatVersion,
		TaskID:        taskID,
		Field:         models.ArchivedFieldAcceptanceReceipt,
		Value:         receipt,
	})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// writeArchiveObject installs data under its digest and completes the
// durability barrier before returning, including when the object already
// existed: a retry must not skip a barrier an earlier attempt failed.
func writeArchiveObject(projectRoot string, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path, err := archiveObjectPath(projectRoot, sha)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(path)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if !bytes.Equal(existing, data) {
			return "", fmt.Errorf("%w: %s", ErrArchiveObjectConflict, path)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("create archive directory: %w", err)
		}
		if err := installArchiveObject(dir, path, data); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("read archive object: %w", err)
	}
	if err := syncArchiveFile(path); err != nil {
		return "", fmt.Errorf("sync archive object: %w", err)
	}
	// Every directory the object or its new parents may have been created in
	// lies on this chain; the runtime directory already holds the state.
	archiveDir := paths.New(projectRoot).ArchiveDir()
	for _, d := range []string{dir, filepath.Dir(dir), archiveDir, filepath.Dir(archiveDir)} {
		if err := syncArchiveDir(d); err != nil {
			return "", fmt.Errorf("sync archive directory %s: %w", d, err)
		}
	}
	return sha, nil
}

// installArchiveObject writes a temp file and links it into place, so an
// existing object is never replaced. A concurrent identical install is
// benign because names are content-derived.
func installArchiveObject(dir, path string, data []byte) error {
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return fmt.Errorf("set archive object permissions: %w", err)
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write archive object: %w", err)
	}
	if err := linkArchiveObject(tmp, path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("install archive object: %w", err)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, data) {
			return fmt.Errorf("%w: %s", ErrArchiveObjectConflict, path)
		}
	}
	return nil
}

func linkArchiveObject(tmp, path string) error {
	if runtime.GOOS == "windows" {
		// No hard-link guarantee; the caller holds the state lock and
		// checked absence, and names are content-derived.
		if _, err := os.Stat(path); err == nil {
			return os.ErrExist
		}
		return os.Rename(tmp, path)
	}
	return os.Link(tmp, path)
}

func syncArchiveFile(path string) error {
	// Windows FlushFileBuffers requires a write-capable handle; O_RDWR without
	// O_TRUNC leaves the immutable object's bytes untouched.
	flag := os.O_RDONLY
	if runtime.GOOS == "windows" {
		flag = os.O_RDWR
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(syncErr, closeErr)
}

// readArchivedReceipt loads and verifies the object ref names for task.
func readArchivedReceipt(projectRoot, taskID string, ref models.ArchivedFieldRef) (*models.AcceptanceReceipt, error) {
	fail := func(path, reason string) error {
		return &ArchiveObjectError{TaskID: taskID, SHA256: ref.SHA256, Path: path, Reason: reason}
	}
	path, err := archiveObjectPath(projectRoot, ref.SHA256)
	if err != nil {
		return nil, fail("", "invalid digest")
	}
	if ref.Field != models.ArchivedFieldAcceptanceReceipt {
		return nil, fail(path, fmt.Sprintf("unsupported archived field %q", ref.Field))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fail(path, "object missing")
		}
		return nil, fail(path, err.Error())
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, fail(path, "digest mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var object archiveObject
	if err := decoder.Decode(&object); err != nil {
		return nil, fail(path, "decode: "+err.Error())
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fail(path, "trailing data after the object")
	}
	switch {
	case object.FormatVersion != archiveObjectFormatVersion:
		return nil, fail(path, fmt.Sprintf("unsupported format version %d", object.FormatVersion))
	case object.TaskID != taskID:
		return nil, fail(path, fmt.Sprintf("object belongs to task %q", object.TaskID))
	case object.Field != ref.Field:
		return nil, fail(path, fmt.Sprintf("object holds field %q", object.Field))
	case object.Value == nil:
		return nil, fail(path, "empty value")
	}
	return object.Value, nil
}

// HydrateArchivedFields restores task's archived fields from their objects.
// Callers pass a copy: live state keeps its compact form. The refs remain on
// the task for traceability.
func HydrateArchivedFields(projectRoot string, task *models.Task) error {
	seen := map[string]bool{}
	for _, ref := range task.Archived {
		fail := func(reason string) error {
			return &ArchiveObjectError{TaskID: task.ID, SHA256: ref.SHA256, Reason: reason}
		}
		if seen[ref.Field] {
			return fail(fmt.Sprintf("more than one archived %s", ref.Field))
		}
		seen[ref.Field] = true
		if ref.Field == models.ArchivedFieldAcceptanceReceipt && task.AcceptanceReceipt != nil {
			return fail("archived acceptance_receipt conflicts with a live receipt")
		}
	}
	for _, ref := range task.Archived {
		receipt, err := readArchivedReceipt(projectRoot, task.ID, ref)
		if err != nil {
			return err
		}
		task.AcceptanceReceipt = receipt
	}
	return nil
}
