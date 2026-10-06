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

	"github.com/liza-mas/liza/internal/archiveobject"
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
var ErrArchiveObjectConflict = archiveobject.ErrConflict

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

// syncArchiveDir remains caller-local so receipt durability fault injection
// does not affect other archive users.
var syncArchiveDir = archiveobject.SyncDir

func archiveObjectPath(projectRoot, sha string) (string, error) {
	return archiveobject.Path(paths.New(projectRoot).ArchiveDir(), sha)
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

// writeArchiveObject completes durability on every install/reuse attempt.
func writeArchiveObject(projectRoot string, data []byte) (string, error) {
	return archiveobject.Write(paths.New(projectRoot).ArchiveDir(), data, syncArchiveDir)
}

// The verdict outbox uses the same file flush primitive.
func syncArchiveFile(path string) error {
	return archiveobject.SyncFile(path)
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
