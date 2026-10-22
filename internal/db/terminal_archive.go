package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/liza-mas/liza/internal/archiveobject"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/statehygiene"
	"gopkg.in/yaml.v3"
)

// Every read restores every archived row, so the cache must hold the whole
// working set to avoid re-decoding. 128 MiB is ~4x the 32 MiB measured for 615
// archived tasks (TECH_DEBT: terminal archive cache sized to working set).
var terminalArchiveCacheMaxBytes = 128 << 20

func setTerminalArchiveCacheMaxBytesForTest(limit int) func() {
	previous := terminalArchiveCacheMaxBytes
	terminalArchiveCacheMaxBytes = limit
	return func() {
		terminalArchiveCacheMaxBytes = previous
	}
}

type terminalArchiveObject struct {
	FormatVersion int    `json:"format_version"`
	TaskID        string `json:"task_id"`
	Field         string `json:"field"`
	ValueYAML     string `json:"value_yaml"`
}

// terminalArchiveCache holds decoded archive objects by digest. A Blackboard
// shares it with the instances derived from it, so a fenced or patient
// transaction does not decode again what its process already decoded.
type terminalArchiveCache struct {
	mu      sync.Mutex
	entries map[string]terminalArchiveCacheEntry
	bytes   int
}

type terminalArchiveCacheEntry struct {
	task  *models.Task // immutable; readers receive deep copies
	bytes int
	// validated records that this payload passed state hygiene validation in
	// this process, so writes that leave it unchanged need not validate it again.
	validated bool
}

// terminalDecodeTestHook observes each archive object decoded on a cache miss.
var terminalDecodeTestHook func(digest string)

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
// size. Very large objects bypass the cache; a full cache admits nothing more.
// Admission never affects evidence checks, which run before the cache lookup.
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
	cache := bb.archive
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.entries == nil {
		cache.entries = make(map[string]terminalArchiveCacheEntry)
	}
	if _, present := cache.entries[digest]; present {
		return
	}
	// Every read scans all archived rows: evicting to admit would churn a
	// partial fit into near-zero hits, while refusing keeps ~limit/working set.
	// Entries for terminal tasks rewritten after archival stay until exit.
	if cache.bytes+cost > terminalArchiveCacheMaxBytes {
		return
	}
	cache.entries[digest] = terminalArchiveCacheEntry{task: &copy, bytes: cost}
	cache.bytes += cost
}

func (bb *Blackboard) cachedTerminalTask(digest string) *models.Task {
	entry, _ := bb.cachedTerminalEntry(digest)
	return entry.task
}

func (bb *Blackboard) cachedTerminalEntry(digest string) (terminalArchiveCacheEntry, bool) {
	bb.archive.mu.Lock()
	defer bb.archive.mu.Unlock()
	entry, ok := bb.archive.entries[digest]
	return entry, ok
}

// markTerminalTaskValidated records a successful hygiene validation of the
// cached payload for digest. Objects the cache refused stay unmarked, so they
// are validated on every write as before.
func (bb *Blackboard) markTerminalTaskValidated(digest string) {
	bb.archive.mu.Lock()
	defer bb.archive.mu.Unlock()
	if entry, ok := bb.archive.entries[digest]; ok {
		entry.validated = true
		bb.archive.entries[digest] = entry
	}
}

var (
	terminalArchiveKey = []byte("terminal_archive:")
	terminalDigestKey  = []byte("sha256:")
)

// needsTerminalArchiveWarmup reports whether a published state may reference an
// archive object this cache has not validated. It scans bytes instead of parsing
// YAML and is only a cost hint: a "terminal_archive:" key without a nearby digest
// is ordinary text, while an unreadable digest after one triggers a full warm-up.
func (bb *Blackboard) needsTerminalArchiveWarmup(data []byte) bool {
	var digests []string
	for rest, found := data, false; ; {
		if _, rest, found = bytes.Cut(rest, terminalArchiveKey); !found {
			break
		}
		_, value, hasDigest := bytes.Cut(rest[:min(len(rest), 256)], terminalDigestKey)
		if !hasDigest {
			continue
		}
		value = bytes.TrimLeft(value, " \"'")
		digest := string(value[:min(len(value), 64)])
		if !archiveobject.IsDigest(digest) {
			return true
		}
		digests = append(digests, digest)
	}
	bb.archive.mu.Lock()
	defer bb.archive.mu.Unlock()
	for _, digest := range digests {
		if !bb.archive.entries[digest].validated {
			return true
		}
	}
	return false
}

// warmTerminalArchive decodes and hygiene-validates, without the state lock,
// the archived objects the current publication references that this cache has
// not validated yet, so the transaction that follows finds them decoded and
// validated. It runs on every Modify, so digests published since an earlier
// call are covered. It authorizes nothing: the locked read re-verifies every
// object's bytes, digest and identity, and reports the errors ignored here.
// Objects published after this read are decoded under the lock as before.
func (bb *Blackboard) warmTerminalArchive() {
	ctx := bb.retryCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return
	}
	data, err := readStateFile(bb.statePath)
	if err != nil || !bb.needsTerminalArchiveWarmup(data) {
		return
	}
	state, err := decodeState(data, "archive warm-up")
	if err != nil {
		return
	}
	for i := range state.Tasks {
		stub := &state.Tasks[i]
		if stub.TerminalArchive == nil {
			continue
		}
		if entry, _ := bb.cachedTerminalEntry(stub.TerminalArchive.SHA256); entry.validated {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		task, err := bb.verifiedTerminalTask(stub)
		if err != nil {
			continue
		}
		if statehygiene.ValidateState(&models.State{Tasks: []models.Task{*task}}) == nil {
			bb.markTerminalTaskValidated(stub.TerminalArchive.SHA256)
		}
	}
}

func (bb *Blackboard) readTerminalTask(stub *models.Task) (models.Task, error) {
	cached, err := bb.verifiedTerminalTask(stub)
	if err != nil {
		return models.Task{}, err
	}
	task := cloneTerminalTask(cached)
	copyRef := *stub.TerminalArchive
	task.TerminalArchive = &copyRef
	task.TerminalArchiveRestored = true
	return task, nil
}

// verifiedTerminalTask verifies a task's archive object bytes, digest and
// identity, as every read must (ADR-0183), and returns the decoded object
// from the cache, decoding it on a miss. The result is shared: clone it
// before handing it out.
func (bb *Blackboard) verifiedTerminalTask(stub *models.Task) (*models.Task, error) {
	ref := stub.TerminalArchive
	path, pathErr := archiveobject.Path(bb.terminalArchiveDir(), ref.SHA256)
	fail := func(reason string) (*models.Task, error) {
		return nil, &TerminalArchiveError{TaskID: stub.ID, SHA256: ref.SHA256, Path: path, Reason: reason}
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
		if terminalDecodeTestHook != nil {
			terminalDecodeTestHook(ref.SHA256)
		}
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
	return cached, nil
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
	// An archived row whose payload equals its cached object, already validated
	// in this process, is validated as its stub; every other row is validated in
	// full at its own index. unchanged also spares the projection a comparison.
	unchanged := make([]bool, len(state.Tasks))
	view := *state
	view.Tasks = slices.Clone(state.Tasks)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.TerminalArchive == nil || !state.Config.TerminalTaskArchival {
			continue
		}
		entry, ok := bb.cachedTerminalEntry(task.TerminalArchive.SHA256)
		if !ok {
			continue
		}
		payload := terminalPayload(task)
		unchanged[i] = reflect.DeepEqual(&payload, entry.task)
		if unchanged[i] && entry.validated {
			view.Tasks[i] = models.Task{ID: task.ID, Status: task.Status, Created: task.Created, TerminalArchive: task.TerminalArchive}
		}
	}
	if err := statehygiene.ValidateState(&view); err != nil {
		return nil, fmt.Errorf("state hygiene validation failed: %w", err)
	}
	for i := range state.Tasks {
		if unchanged[i] {
			bb.markTerminalTaskValidated(state.Tasks[i].TerminalArchive.SHA256)
		}
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
		if !unchanged[i] {
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
			// The view above validated this payload in full.
			bb.rememberTerminalTask(digest, task, len(data))
			bb.markTerminalTaskValidated(digest)
		}
		tasks[i] = map[string]any{"id": task.ID, "status": task.Status, "created": task.Created, "terminal_archive": ref}
	}
	physical.FieldByName("Tasks").Set(reflect.ValueOf(tasks))
	return marshalWriteValue(physical)
}
