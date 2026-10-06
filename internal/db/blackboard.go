package db

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/roles"
	"github.com/liza-mas/liza/internal/statehygiene"
	"gopkg.in/yaml.v3"
)

// instances holds per-path singleton Blackboard instances.
// All production code should use For() to get a shared instance.
var instances sync.Map

var unsafeBlockScalarIndentRE = regexp.MustCompile(`^(.*?)([|>])([1-9])([+-]?)$`)

// Blackboard provides thread-safe access to the state.yaml file
type Blackboard struct {
	statePath string
	fileLock  *filelock.FileLock

	// Cache fields for performance optimization
	// We cache the parsed, normalized state keyed by file mtime: waiting
	// supervisors re-read unchanged state on every tick, and re-parsing
	// megabyte-scale YAML per call starves the CPU the lock holders need.
	// The cached value is never handed out directly — ReadCached returns a
	// deep copy so callers keep a state they may mutate freely.
	cacheMu           sync.RWMutex
	cachedState       *models.State
	cachedMtime       time.Time
	archiveMu         sync.Mutex
	archiveCache      map[string]terminalArchiveCacheEntry
	archiveCacheBytes int
}

// defaultLockTimeoutNanos overrides the lock wait of Blackboards created by
// New when positive. Only SetDefaultLockTimeoutForTest sets it.
var defaultLockTimeoutNanos atomic.Int64

// New creates a Blackboard backed by the given state file path.
// Use For() in production code to get a shared process-level singleton.
// New is intended for tests that need independent instances.
func New(statePath string) *Blackboard {
	fileLock := filelock.New(statePath)
	if timeout := time.Duration(defaultLockTimeoutNanos.Load()); timeout > 0 {
		fileLock = fileLock.WithTimeout(timeout)
	}
	return &Blackboard{
		statePath: statePath,
		fileLock:  fileLock,
	}
}

// SetDefaultLockTimeoutForTest changes the lock wait of Blackboards created
// afterwards, including instances For creates for new paths. It exists so lock
// contention can be exercised without slow tests; callers must not run in
// parallel with tests that depend on the ordinary wait.
func SetDefaultLockTimeoutForTest(timeout time.Duration) func() {
	previous := defaultLockTimeoutNanos.Swap(int64(timeout))
	return func() {
		defaultLockTimeoutNanos.Store(previous)
	}
}

// For returns a process-level singleton Blackboard for the given state path.
// All callers sharing the same path within a process get the same instance,
// ensuring cache coherence and preventing state fragmentation if Blackboard
// gains in-process state in the future.
//
// The statePath is cleaned via filepath.Clean to ensure callers using
// equivalent paths (e.g. with trailing slashes) share the same instance.
func For(statePath string) *Blackboard {
	key := filepath.Clean(statePath)
	if v, ok := instances.Load(key); ok {
		return v.(*Blackboard)
	}
	bb := New(key)
	actual, _ := instances.LoadOrStore(key, bb)
	return actual.(*Blackboard)
}

// ResetInstances clears all cached singleton instances.
// Intended for test cleanup only.
func ResetInstances() {
	instances.Range(func(key, _ any) bool {
		instances.Delete(key)
		return true
	})
}

// ResetInstance clears the cached singleton for one state path.
// Intended for tests that simulate a process restart without disturbing
// independent fixtures running concurrently.
func ResetInstance(statePath string) {
	instances.Delete(filepath.Clean(statePath))
}

// WithLockTimeout creates a new independent instance with a custom lock timeout;
// cached bytes are copied at creation time but diverge afterward. The returned
// instance is intentionally NOT registered in the singleton map — it is a
// short-lived specialization for callers that need different lock behavior.
func (bb *Blackboard) WithLockTimeout(timeout time.Duration) *Blackboard {
	bb.cacheMu.RLock()
	cachedState := bb.cachedState
	cachedMtime := bb.cachedMtime
	bb.cacheMu.RUnlock()

	newBB := &Blackboard{
		statePath:   bb.statePath,
		fileLock:    bb.fileLock.WithTimeout(timeout),
		cachedState: cachedState,
		cachedMtime: cachedMtime,
	}
	return newBB
}

// WithLockRetryContext returns an independent supervisor instance whose lock
// acquisition retries timeouts with backoff until ctx ends. Mutation callbacks
// still run once, and all state/authority validation stays under the lock.
// It is not cached by For and does not share lock metrics or cached state.
func (bb *Blackboard) WithLockRetryContext(ctx context.Context) *Blackboard {
	return &Blackboard{statePath: bb.statePath, fileLock: bb.fileLock.WithRetryContext(ctx)}
}

// EnableMetrics enables lock metrics collection.
func (bb *Blackboard) EnableMetrics() {
	bb.fileLock.EnableMetrics()
}

// DisableMetrics disables lock metrics collection.
func (bb *Blackboard) DisableMetrics() {
	bb.fileLock.DisableMetrics()
}

// GetMetricsRecorder returns the metrics recorder, or nil if not enabled.
func (bb *Blackboard) GetMetricsRecorder() *filelock.MetricsRecorder {
	return bb.fileLock.GetMetricsRecorder()
}

// readDecodeTestHook runs in ReadContext between the byte read and decoding.
var readDecodeTestHook func()

// Read returns the current state, reading its bytes under the exclusive file
// lock and decoding them after release.
func (bb *Blackboard) Read() (*models.State, error) {
	return bb.ReadContext(context.Background())
}

// ReadContext returns the current state, aborting lock acquisition when ctx is
// canceled. Only the byte read holds the exclusive lock, which orders the read
// after any writer holding it. Writers publish by atomic rename, so the bytes
// are one complete publication; decoding them is pure and takes seconds on a
// large state, so it runs after release instead of delaying writers.
func (bb *Blackboard) ReadContext(ctx context.Context) (*models.State, error) {
	var data []byte
	err := bb.fileLock.WithLockOperationContext(ctx, "read", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		data, err = readStateFile(bb.statePath)
		return err
	})
	if err != nil {
		return nil, err
	}

	if readDecodeTestHook != nil {
		readDecodeTestHook()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return bb.decodeLiveState(data, "state read")
}

// patientReadLockTimeout bounds the lock wait of Patient instances, including
// ReadContextPatient.
var patientReadLockTimeout = 60 * time.Second

// Patient returns an independent instance whose lock acquisition keeps polling
// for the patient wait instead of the ordinary timeout. It is for supervisor
// reads and writes that would otherwise fail while writers merely saturate the
// lock. The wait bounds acquisition only, not the work done under the lock.
// Once it elapses, operations return the ordinary lock-timeout error. Patient
// operations are absent from lock metrics.
func (bb *Blackboard) Patient() *Blackboard {
	return bb.WithLockTimeout(patientReadLockTimeout)
}

// SetPatientReadLockTimeoutForTest shortens the Patient wait so a lock held past
// it can be exercised without slow tests. Callers must not run in parallel with
// other Patient operations.
func SetPatientReadLockTimeoutForTest(timeout time.Duration) func() {
	previous := patientReadLockTimeout
	patientReadLockTimeout = timeout
	return func() {
		patientReadLockTimeout = previous
	}
}

// ReadContextPatient is ReadContext with the Patient lock wait. It remains an
// exclusive read and aborts when ctx is canceled.
func (bb *Blackboard) ReadContextPatient(ctx context.Context) (*models.State, error) {
	return bb.Patient().ReadContext(ctx)
}

// ReadSnapshot reads one complete published state without acquiring the state
// lock or consulting the cache. Writers publish by atomic rename, so readers
// observe either publication, never a partially written state. The file is
// closed before decoding. The result is independent and may already be stale;
// use locked revalidation for decisions that authorize mutations.
func (bb *Blackboard) ReadSnapshot() (*models.State, error) {
	data, err := readStateFile(bb.statePath)
	if err != nil {
		return nil, err
	}
	return bb.decodeLiveState(data, "state snapshot")
}

// decodeLiveState decodes a published state and overlays pending liveness
// records (ADR-0177). Task-sensitive pre-image checks use decodeLogicalState;
// top-level ledger checks use decodeState.
func (bb *Blackboard) decodeLiveState(data []byte, operation string) (*models.State, error) {
	state, err := bb.decodeLogicalState(data, operation)
	if err != nil {
		return nil, err
	}
	applyLiveness(state, bb.statePath)
	return state, nil
}

func decodeState(data []byte, operation string) (*models.State, error) {
	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, &errors.StateSchemaError{Operation: operation, Err: err}
	}
	normalizeAgentRoles(&state)
	normalizeTaskAttempts(&state)
	return &state, nil
}

// ReadRaw reads the raw state.yaml bytes under flock protection.
// Use this when you need the file content without parsing (e.g., serving
// the raw YAML to an external consumer), while still respecting the lock
// to avoid reading partially-written data.
func (bb *Blackboard) ReadRaw() ([]byte, error) {
	var data []byte
	err := bb.fileLock.WithLockOperation("read-raw", func() error {
		var readErr error
		data, readErr = readStateFile(bb.statePath)
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ReadCached reads the current state with caching based on file mtime.
// This method avoids both disk I/O and YAML parsing when the file hasn't
// changed. Each call returns a deep copy of the cached state, so callers can
// safely mutate the result without corrupting other readers.
func (bb *Blackboard) ReadCached() (*models.State, error) {
	fileInfo, err := os.Stat(bb.statePath)
	if err != nil {
		bb.InvalidateCache()
		return nil, err
	}

	currentMtime := fileInfo.ModTime()

	bb.cacheMu.RLock()
	cachedState := bb.cachedState
	cachedMtime := bb.cachedMtime
	bb.cacheMu.RUnlock()

	if cachedState != nil && currentMtime.Equal(cachedMtime) {
		return bb.cachedLogicalState(cachedState)
	}

	data, err := readStateFile(bb.statePath)
	if err != nil {
		bb.InvalidateCache()
		return nil, err
	}

	state, err := decodeState(data, "state read cached")
	if err != nil {
		return nil, err
	}

	bb.cacheMu.Lock()
	bb.cachedState = state
	bb.cachedMtime = currentMtime
	bb.cacheMu.Unlock()

	return bb.cachedLogicalState(state)
}

func (bb *Blackboard) cachedLogicalState(physical *models.State) (*models.State, error) {
	state := CloneState(physical)
	if err := bb.restoreTerminalTasks(state); err != nil {
		return nil, err
	}
	normalizeTaskAttempts(state)
	return bb.withLiveness(state), nil
}

// withLiveness overlays liveness records on a copy handed to a caller. The
// cache keeps the decoded state without them: records change without changing
// state.yaml's mtime, so a cached overlay would hide newer beats.
func (bb *Blackboard) withLiveness(state *models.State) *models.State {
	applyLiveness(state, bb.statePath)
	return state
}

// InvalidateCache forces the next ReadCached call to reload from disk.
func (bb *Blackboard) InvalidateCache() {
	bb.cacheMu.Lock()
	bb.cachedState = nil
	bb.cachedMtime = time.Time{}
	bb.cacheMu.Unlock()
}

// writeStateData writes data to the state file atomically using fsync + rename.
// Must be called while holding the file lock.
// Uses a unique temp file per call to avoid races if the file lock has gaps.
func (bb *Blackboard) writeStateData(data []byte) error {
	dir := filepath.Dir(bb.statePath)
	base := filepath.Base(bb.statePath)

	f, err := os.CreateTemp(dir, base+".tmp.*")
	if err != nil {
		return fmt.Errorf("failed to create temporary state file: %w", err)
	}
	tmpPath := f.Name()

	// CreateTemp uses 0600; match the target file permissions
	if err := f.Chmod(0644); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to set temporary file permissions: %w", err)
	}

	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()

	if writeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write state data: %w", writeErr)
	}
	if syncErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to sync state file: %w", syncErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close state file: %w", closeErr)
	}

	if err := publishState(tmpPath, bb.statePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename state file: %w", err)
	}

	return nil
}

func yamlBytesParse(data []byte) error {
	var probe any
	return yaml.Unmarshal(data, &probe)
}

func rewriteUnsafeBlockScalarIndents(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines)+4)
	for i, line := range lines {
		m := unsafeBlockScalarIndentRE.FindStringSubmatch(line)
		if m == nil {
			out = append(out, line)
			continue
		}

		explicitIndent, err := strconv.Atoi(m[3])
		if err != nil {
			out = append(out, line)
			continue
		}

		nextIndent, found := nextNonEmptyLineIndent(lines, i+1)
		if !found {
			out = append(out, line)
			continue
		}

		requiredIndent := len(leadingWhitespace(line)) + explicitIndent
		if len(nextIndent) >= requiredIndent {
			out = append(out, line)
			continue
		}

		out = append(out, m[1]+m[2]+m[4])
		out = append(out, nextIndent)
	}
	return []byte(strings.Join(out, "\n"))
}

func nextNonEmptyLineIndent(lines []string, start int) (string, bool) {
	for i := start; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		return leadingWhitespace(lines[i]), true
	}
	return "", false
}

func leadingWhitespace(s string) string {
	idx := len(s) - len(strings.TrimLeft(s, " \t"))
	if idx <= 0 {
		return ""
	}
	return s[:idx]
}

func marshalStateForWrite(state *models.State) ([]byte, error) {
	if err := statehygiene.ValidateState(state); err != nil {
		return nil, fmt.Errorf("state hygiene validation failed: %w", err)
	}
	return marshalWriteValue(reflect.ValueOf(state))
}

func marshalWriteValue(value reflect.Value) ([]byte, error) {
	needsParse := false
	view := projectWriteValue(value, &needsParse)
	data, err := yaml.Marshal(view.Interface())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}
	if !needsParse {
		return data, nil
	}
	if err := yamlBytesParse(data); err == nil {
		return data, nil
	}

	// Custom YAML/raw nodes retain the legacy fail-closed publication check.
	// Ordinary model and Extra strings are made safe before emission instead.
	rewritten := rewriteUnsafeBlockScalarIndents(data)
	if err := yamlBytesParse(rewritten); err != nil {
		return nil, fmt.Errorf("failed to marshal parseable state YAML: %w", err)
	}
	return rewritten, nil
}

// checkWrittenAnomalies refuses a transaction that adds an anomaly violation
// (ADR-0166): an unknown type or a missing required detail, by the rule
// validate reports (models.AnomalyViolations). Violations the pre-image
// already had are not the transaction's, so they never block it and may be
// repaired one detail at a time (ADR-0165); identities are counted, so a
// second copy of an invalid record is new. The pre-image is decoded only
// when the candidate has a violation, so the valid path costs one scan.
func checkWrittenAnomalies(preImage []byte, state *models.State) error {
	found := models.AnomalyViolations(state.Anomalies)
	if len(found) == 0 {
		return nil
	}
	before, err := decodeState(preImage, "anomaly pre-image")
	if err != nil {
		return fmt.Errorf("anomaly write refused: pre-image unreadable: %w", err)
	}
	remaining := make(map[string]int)
	for _, violation := range models.AnomalyViolations(before.Anomalies) {
		remaining[violation.ID]++
	}
	var added []string
	for _, violation := range found {
		if remaining[violation.ID] > 0 {
			remaining[violation.ID]--
			continue
		}
		added = append(added, violation.Err.Error())
	}
	if len(added) == 0 {
		return nil
	}
	return fmt.Errorf("anomaly write refused: %s", strings.Join(added, "; "))
}

// checkRuntimeInputTransitions refuses a transaction that breaks the
// consumption ledger's transition rules (ADR-0169): an instance removed, a
// consumed or invalidated instance changed, or an available one changing
// anything but its bindings and state. The rules compare candidate and locked
// pre-image, so every violation is the transaction's own. The pre-image is
// decoded only when either side has a ledger; the top-level key sits at
// column zero, which no nested field does.
func checkRuntimeInputTransitions(preImage []byte, state *models.State) error {
	if len(state.RuntimeInputs) == 0 && !bytes.HasPrefix(preImage, []byte("runtime_inputs:")) &&
		!bytes.Contains(preImage, []byte("\nruntime_inputs:")) {
		return nil
	}
	before, err := decodeState(preImage, "runtime-input pre-image")
	if err != nil {
		return fmt.Errorf("runtime-input write refused: pre-image unreadable: %w", err)
	}
	violations := models.RuntimeInputTransitionViolations(before.RuntimeInputs, state.RuntimeInputs)
	if len(violations) == 0 {
		return nil
	}
	messages := make([]string, len(violations))
	for i, violation := range violations {
		messages[i] = violation.Error()
	}
	return fmt.Errorf("runtime-input write refused: %s", strings.Join(messages, "; "))
}

// checkRuntimeInputNameCollisions refuses a transaction that makes a name
// both a runtime input and a session prerequisite (ADR-0169): the name is
// stripped from every session, so the prerequisite could never pass. It is the
// fence behind the admission diagnostics, covering every writer and any
// concurrent pair of declarations. Only collisions absent from the locked
// pre-image are refused, so legacy states stay writable (ADR-0165).
func (bb *Blackboard) checkRuntimeInputNameCollisions(preImage []byte, state *models.State) error {
	collisions := models.RuntimeInputNameCollisions(state)
	if len(collisions) == 0 {
		return nil
	}
	before, err := bb.decodeLogicalState(preImage, "runtime-input name pre-image")
	if err != nil {
		return fmt.Errorf("runtime-input write refused: pre-image unreadable: %w", err)
	}
	existing := models.RuntimeInputNameCollisions(before)
	var introduced []string
	for _, name := range collisions {
		if !slices.Contains(existing, name) {
			introduced = append(introduced, name)
		}
	}
	if len(introduced) == 0 {
		return nil
	}
	return fmt.Errorf("runtime-input write refused: %s would be both a runtime input and a validation prerequisite; runtime-input names are stripped from sessions, so deliver the variable one way only", strings.Join(introduced, ", "))
}

// Write writes the state to the state file atomically with fsync.
//
// Write does not run checkWrittenAnomalies: it replaces the whole state with
// no transaction pre-image, and its callers are initialization, which writes
// no anomalies, and migration, which repairs legacy records and must still
// write a state that holds others. Runtime writers go through Modify.
func (bb *Blackboard) Write(state *models.State) error {
	err := bb.fileLock.WithLockOperation("write", func() error {
		data, err := bb.marshalLogicalState(state)
		if err != nil {
			return err
		}
		return bb.writeStateData(data)
	})

	if err == nil {
		bb.InvalidateCache()
	}

	return err
}

// Modify performs an atomic read-modify-write operation
func (bb *Blackboard) Modify(fn func(*models.State) error) error {
	published := false
	err := bb.fileLock.WithLockOperation("modify", func() error {
		data, err := readStateFile(bb.statePath)
		if err != nil {
			return fmt.Errorf("failed to read state: %w", err)
		}

		state, err := bb.decodeLogicalState(data, "state modify")
		if err != nil {
			return err
		}
		sequence := state.MutationSequence
		if sequence == math.MaxUint64 {
			return fmt.Errorf("state mutation sequence exhausted")
		}
		// Capture the stored logical state before overlay. A callback that
		// changes nothing still publishes a pending liveness fold (ADR-0177),
		// while stale/refused observations cannot manufacture recovery signals.
		before := CloneState(state)
		applyLiveness(state, bb.statePath)

		preImage := data
		if err := fn(state); err != nil {
			return fmt.Errorf("modification function failed: %w", err)
		}
		// The counter belongs to publication, not to the mutation callback.
		state.MutationSequence = sequence
		if reflect.DeepEqual(before, state) {
			return nil
		}
		if err := checkWrittenAnomalies(preImage, state); err != nil {
			return err
		}
		if err := checkRuntimeInputTransitions(preImage, state); err != nil {
			return err
		}
		if err := bb.checkRuntimeInputNameCollisions(preImage, state); err != nil {
			return err
		}

		// Publication owns this counter; callbacks cannot reset or advance it.
		state.MutationSequence = sequence + 1
		data, err = bb.marshalLogicalState(state)
		if err != nil {
			return err
		}
		if err := bb.writeStateData(data); err != nil {
			return err
		}
		published = true
		return nil
	})

	if err == nil && published {
		bb.InvalidateCache()
	}

	return err
}

// GetTask returns the task with the given ID, or (nil, nil) if not found.
func (bb *Blackboard) GetTask(taskID string) (*models.Task, error) {
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}

	return state.FindTask(taskID), nil
}

// GetAgent returns the agent with the given ID, or (nil, nil) if not found.
func (bb *Blackboard) GetAgent(agentID string) (*models.Agent, error) {
	state, err := bb.Read()
	if err != nil {
		return nil, err
	}

	if agent, ok := state.Agents[agentID]; ok {
		return &agent, nil
	}

	return nil, nil
}

// UpdateTask atomically updates a task by ID
func (bb *Blackboard) UpdateTask(taskID string, fn func(*models.Task) error) error {
	return bb.Modify(func(state *models.State) error {
		task := state.FindTask(taskID)
		if task == nil {
			return &errors.NotFoundError{Entity: "task", ID: taskID}
		}
		return fn(task)
	})
}

// UpdateAgent atomically updates an agent by ID
func (bb *Blackboard) UpdateAgent(agentID string, fn func(*models.Agent) error) error {
	return bb.Modify(func(state *models.State) error {
		agent, ok := state.Agents[agentID]
		if !ok {
			return &errors.NotFoundError{Entity: "agent", ID: agentID}
		}

		if err := fn(&agent); err != nil {
			return err
		}

		state.Agents[agentID] = agent
		return nil
	})
}

// GetStatePath returns the path to the state file.
func (bb *Blackboard) GetStatePath() string {
	return bb.statePath
}

// normalizeTaskAttempts converts the legacy attempted: list into the Attempt
// field in-memory. Does not write back to disk — normalization is read-path only.
func normalizeTaskAttempts(state *models.State) {
	for i := range state.Tasks {
		state.Tasks[i].MigrateAttemptedField()
	}
}

// normalizeAgentRoles converts legacy underscore-form role names to hyphenated
// form in-memory. Does not write back to disk — normalization is read-path only.
func normalizeAgentRoles(state *models.State) {
	for id, agent := range state.Agents {
		normalized := roles.NormalizeRoleName(agent.Role)
		if normalized != agent.Role {
			agent.Role = normalized
			state.Agents[id] = agent
		}
	}
}
