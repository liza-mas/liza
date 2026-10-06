package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/archiveobject"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"gopkg.in/yaml.v3"
)

const terminalArchiveCacheMaxBytes = 16 << 20

type terminalArchiveObject struct {
	FormatVersion int    `json:"format_version"`
	TaskID        string `json:"task_id"`
	Field         string `json:"field"`
	ValueYAML     string `json:"value_yaml"`
}

type terminalArchiveCacheEntry struct {
	task  *models.Task // immutable; readers receive deep copies
	bytes int
}

// TerminalArchiveError locates evidence that cannot be restored. It never
// degrades to an absent field, including after a decoded-object cache hit.
type TerminalArchiveError struct {
	TaskID string
	SHA256 string
	Path   string
	Reason string
}

func (e *TerminalArchiveError) Error() string {
	return fmt.Sprintf("task %s terminal archive %s (%s): %s", e.TaskID, e.SHA256, e.Path, e.Reason)
}

// terminalArchiveSyncDir is caller-local fault injection for durability tests.
var terminalArchiveSyncDir = archiveobject.SyncDir

func (bb *Blackboard) terminalArchiveDir() string {
	return filepath.Join(filepath.Dir(bb.statePath), paths.ArchiveDirName)
}

func terminalPayload(task *models.Task) models.Task {
	copy := *task
	copy.TerminalArchive = nil
	copy.TerminalArchiveRestored = false
	return copy
}

func cloneTerminalTask(task *models.Task) models.Task {
	var copy models.Task
	deepCopyValue(reflect.ValueOf(task).Elem(), reflect.ValueOf(&copy).Elem())
	return copy
}

// EncodeTerminalTask returns canonical envelope bytes for a complete terminal
// task. Callers can use the length for bounded maintenance selection.
func EncodeTerminalTask(task *models.Task) ([]byte, error) {
	if task == nil || !task.Status.IsTerminal() {
		return nil, fmt.Errorf("only terminal tasks may be archived")
	}
	if task.TerminalArchive != nil && !task.TerminalArchiveRestored {
		return nil, fmt.Errorf("task %s terminal archive is not restored; use an archive-aware database decode before writing", task.ID)
	}
	copy := terminalPayload(task)
	yamlBytes, err := marshalWriteValue(reflect.ValueOf(copy))
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(terminalArchiveObject{FormatVersion: 2, TaskID: task.ID, Field: "terminal_task", ValueYAML: string(yamlBytes)})
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ArchiveTerminalTask installs a durable object before marking this complete
// logical task archived. The caller must already hold the state transaction.
func (bb *Blackboard) ArchiveTerminalTask(task *models.Task, archivedAt time.Time) (int, error) {
	data, err := EncodeTerminalTask(task)
	if err != nil {
		return 0, err
	}
	if archivedAt.IsZero() {
		return 0, fmt.Errorf("task %s terminal archive requires archived_at", task.ID)
	}
	digest, err := archiveobject.Write(bb.terminalArchiveDir(), data, terminalArchiveSyncDir)
	if err != nil {
		return 0, err
	}
	bb.rememberTerminalTask(digest, task, len(data))
	task.TerminalArchive = &models.TerminalArchiveRef{SHA256: digest, ArchivedAt: archivedAt.UTC()}
	task.TerminalArchiveRestored = true
	return len(data), nil
}

// Cache budgeting counts the retained decoded representation and its encoded
// size. Very large objects bypass the cache; eviction never removes evidence.
func terminalRetainedBytes(value reflect.Value) int {
	if !value.IsValid() {
		return 0
	}
	size := int(value.Type().Size())
	switch value.Kind() {
	case reflect.String:
		return size + value.Len()
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			size += terminalRetainedBytes(value.Elem())
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			size += terminalRetainedBytes(value.Index(i))
		}
	case reflect.Map:
		iter := value.MapRange()
		for iter.Next() {
			size += terminalRetainedBytes(iter.Key()) + terminalRetainedBytes(iter.Value()) + 32
		}
	case reflect.Struct:
		if hasUnexportedFields(value.Type()) {
			break // time.Time is copied by value and contains no mutable payload.
		}
		for i := 0; i < value.NumField(); i++ {
			size += terminalRetainedBytes(value.Field(i))
		}
	}
	return size
}

func (bb *Blackboard) rememberTerminalTask(digest string, task *models.Task, encodedBytes int) {
	copy := terminalPayload(task)
	cost := encodedBytes + terminalRetainedBytes(reflect.ValueOf(copy))
	if cost > terminalArchiveCacheMaxBytes {
		return
	}
	copy = cloneTerminalTask(&copy)
	bb.archiveMu.Lock()
	defer bb.archiveMu.Unlock()
	if bb.archiveCache == nil {
		bb.archiveCache = make(map[string]terminalArchiveCacheEntry)
	}
	if _, present := bb.archiveCache[digest]; present {
		return
	}
	for key, entry := range bb.archiveCache {
		if bb.archiveCacheBytes+cost <= terminalArchiveCacheMaxBytes {
			break
		}
		delete(bb.archiveCache, key)
		bb.archiveCacheBytes -= entry.bytes
	}
	bb.archiveCache[digest] = terminalArchiveCacheEntry{task: &copy, bytes: cost}
	bb.archiveCacheBytes += cost
}

func (bb *Blackboard) cachedTerminalTask(digest string) *models.Task {
	bb.archiveMu.Lock()
	defer bb.archiveMu.Unlock()
	return bb.archiveCache[digest].task
}

func (bb *Blackboard) readTerminalTask(stub *models.Task) (models.Task, error) {
	ref := stub.TerminalArchive
	path, pathErr := archiveobject.Path(bb.terminalArchiveDir(), ref.SHA256)
	fail := func(reason string) (models.Task, error) {
		return models.Task{}, &TerminalArchiveError{TaskID: stub.ID, SHA256: ref.SHA256, Path: path, Reason: reason}
	}
	if pathErr != nil || ref.ArchivedAt.IsZero() || !stub.Status.IsTerminal() {
		return fail("invalid reference or non-terminal task")
	}
	if !stub.TerminalArchiveRestored {
		identity := models.Task{ID: stub.ID, Status: stub.Status, Created: stub.Created, TerminalArchive: ref}
		if !reflect.DeepEqual(stub, &identity) {
			return fail("physical terminal row contains live payload fields")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fail(err.Error())
	}
	if archiveobject.Digest(data) != ref.SHA256 {
		return fail("digest mismatch")
	}
	cached := bb.cachedTerminalTask(ref.SHA256)
	cacheMiss := cached == nil
	if cached == nil {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		var object terminalArchiveObject
		if err := decoder.Decode(&object); err != nil {
			return fail("decode: " + err.Error())
		}
		if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
			return fail("trailing data after object")
		}
		if object.FormatVersion != 2 || object.Field != "terminal_task" || object.TaskID != stub.ID || object.ValueYAML == "" {
			return fail("unsupported envelope or task identity")
		}
		var task models.Task
		yamlDecoder := yaml.NewDecoder(strings.NewReader(object.ValueYAML))
		if err := yamlDecoder.Decode(&task); err != nil {
			return fail("decode task: " + err.Error())
		}
		var trailing any
		if err := yamlDecoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return fail("trailing task YAML document")
		}
		if task.TerminalArchive != nil {
			return fail("recursive terminal archive")
		}
		cached = &task
	}
	if cached.ID != stub.ID || cached.Status != stub.Status || !cached.Created.Equal(stub.Created) || !cached.Status.IsTerminal() {
		return fail("task identity, status or created timestamp mismatch")
	}
	if cacheMiss {
		// Envelope task_id already matched this stub. Cache only after its
		// payload matches too, so a failed read cannot authorize another ID.
		bb.rememberTerminalTask(ref.SHA256, cached, len(data))
	}
	task := cloneTerminalTask(cached)
	copyRef := *ref
	task.TerminalArchive = &copyRef
	task.TerminalArchiveRestored = true
	return task, nil
}

func (bb *Blackboard) restoreTerminalTasks(state *models.State) error {
	for i := range state.Tasks {
		if state.Tasks[i].TerminalArchive == nil {
			continue
		}
		task, err := bb.readTerminalTask(&state.Tasks[i])
		if err != nil {
			return err
		}
		state.Tasks[i] = task
	}
	return nil
}

// DecodeUnnormalized restores logical records while retaining legacy fields
// migration needs to detect. It does not overlay liveness or normalize roles.
func (bb *Blackboard) DecodeUnnormalized(data []byte) (*models.State, error) {
	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state: %w", err)
	}
	if err := bb.restoreTerminalTasks(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (bb *Blackboard) decodeLogicalState(data []byte, operation string) (*models.State, error) {
	state, err := decodeState(data, operation)
	if err != nil {
		return nil, err
	}
	if err := bb.restoreTerminalTasks(state); err != nil {
		return nil, err
	}
	normalizeTaskAttempts(state)
	return state, nil
}

var terminalPhysicalStateType = func() reflect.Type {
	stateType := reflect.TypeFor[models.State]()
	fields := make([]reflect.StructField, stateType.NumField())
	for i := range fields {
		fields[i] = stateType.Field(i)
		if fields[i].Name == "Tasks" {
			fields[i].Type = reflect.TypeFor[[]any]()
		}
	}
	return reflect.StructOf(fields)
}()

func (bb *Blackboard) marshalLogicalState(state *models.State) ([]byte, error) {
	hasArchive := false
	for i := range state.Tasks {
		hasArchive = hasArchive || state.Tasks[i].TerminalArchive != nil
	}
	if !hasArchive {
		return marshalStateForWrite(state)
	}
	if err := statehygiene.ValidateState(state); err != nil {
		return nil, fmt.Errorf("state hygiene validation failed: %w", err)
	}
	physical := reflect.New(terminalPhysicalStateType).Elem()
	source := reflect.ValueOf(state).Elem()
	for i := 0; i < source.NumField(); i++ {
		if source.Type().Field(i).Name != "Tasks" {
			physical.Field(i).Set(source.Field(i))
		}
	}
	tasks := make([]any, len(state.Tasks))
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.TerminalArchive == nil {
			tasks[i] = task
			continue
		}
		if !task.TerminalArchiveRestored || !task.Status.IsTerminal() {
			return nil, fmt.Errorf("task %s terminal archive is not a restored terminal task; use an archive-aware database decode before writing", task.ID)
		}
		if !state.Config.TerminalTaskArchival {
			// Disabling the capability is an explicit storage rollback: preserve
			// the full logical record, removing only archive bookkeeping.
			payload := terminalPayload(task)
			tasks[i] = &payload
			continue
		}
		ref := *task.TerminalArchive
		if ref.ArchivedAt.IsZero() || !archiveobject.IsDigest(ref.SHA256) {
			return nil, fmt.Errorf("task %s terminal archive has an invalid reference", task.ID)
		}
		payload := terminalPayload(task)
		cached := bb.cachedTerminalTask(ref.SHA256)
		if cached == nil || !reflect.DeepEqual(&payload, cached) {
			data, err := EncodeTerminalTask(task)
			if err != nil {
				return nil, err
			}
			digest := archiveobject.Digest(data)
			if digest != ref.SHA256 {
				if _, err := archiveobject.Write(bb.terminalArchiveDir(), data, terminalArchiveSyncDir); err != nil {
					return nil, err
				}
				ref.SHA256, ref.ArchivedAt = digest, time.Now().UTC()
			}
			bb.rememberTerminalTask(digest, task, len(data))
		}
		tasks[i] = map[string]any{"id": task.ID, "status": task.Status, "created": task.Created, "terminal_archive": ref}
	}
	physical.FieldByName("Tasks").Set(reflect.ValueOf(tasks))
	return marshalWriteValue(physical)
}
