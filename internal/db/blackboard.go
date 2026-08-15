package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	// We cache raw YAML bytes (not a parsed struct) so that each ReadCached
	// call returns a fresh *models.State. This prevents callers from silently
	// corrupting a shared cached struct.
	cacheMu     sync.RWMutex
	cachedData  []byte
	cachedMtime time.Time
}

// New creates a Blackboard backed by the given state file path.
// Use For() in production code to get a shared process-level singleton.
// New is intended for tests that need independent instances.
func New(statePath string) *Blackboard {
	return &Blackboard{
		statePath: statePath,
		fileLock:  filelock.New(statePath),
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
	cachedData := bb.cachedData
	cachedMtime := bb.cachedMtime
	bb.cacheMu.RUnlock()

	newBB := &Blackboard{
		statePath:   bb.statePath,
		fileLock:    filelock.New(bb.statePath).WithTimeout(timeout),
		cachedData:  cachedData,
		cachedMtime: cachedMtime,
	}
	return newBB
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

// Read returns the current state under an exclusive file lock.
func (bb *Blackboard) Read() (*models.State, error) {
	return bb.ReadContext(context.Background())
}

// ReadContext returns the current state under an exclusive file lock,
// aborting lock acquisition when ctx is canceled.
func (bb *Blackboard) ReadContext(ctx context.Context) (*models.State, error) {
	var state models.State
	err := bb.fileLock.WithLockOperationContext(ctx, "read", func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := os.ReadFile(bb.statePath)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		if err := yaml.Unmarshal(data, &state); err != nil {
			return &errors.StateSchemaError{Operation: "state read", Err: err}
		}

		return nil
	})

	if err != nil {
		return nil, err
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
		data, readErr = os.ReadFile(bb.statePath)
		return readErr
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ReadCached reads the current state with caching based on file mtime.
// This method avoids disk I/O when the file hasn't changed by caching raw
// YAML bytes. Each call returns a freshly-parsed *models.State, so callers
// can safely mutate the result without corrupting other readers.
func (bb *Blackboard) ReadCached() (*models.State, error) {
	fileInfo, err := os.Stat(bb.statePath)
	if err != nil {
		bb.InvalidateCache()
		return nil, err
	}

	currentMtime := fileInfo.ModTime()

	bb.cacheMu.RLock()
	cachedData := bb.cachedData
	cachedMtime := bb.cachedMtime
	bb.cacheMu.RUnlock()

	var data []byte
	if cachedData != nil && currentMtime.Equal(cachedMtime) {
		data = cachedData
	} else {
		data, err = os.ReadFile(bb.statePath)
		if err != nil {
			bb.InvalidateCache()
			return nil, err
		}

		bb.cacheMu.Lock()
		bb.cachedData = data
		bb.cachedMtime = currentMtime
		bb.cacheMu.Unlock()
	}

	var state models.State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, &errors.StateSchemaError{Operation: "state read cached", Err: err}
	}

	normalizeAgentRoles(&state)
	normalizeTaskAttempts(&state)
	return &state, nil
}

// InvalidateCache forces the next ReadCached call to reload from disk.
func (bb *Blackboard) InvalidateCache() {
	bb.cacheMu.Lock()
	bb.cachedData = nil
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
	data, err := yaml.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}
	if err := yamlBytesParse(data); err == nil {
		return data, nil
	}

	// Why: go-yaml can emit broken explicit indent indicators such as `|4-`
	// for leading-blank-line scalars; rewriting only the header preserves the
	// original text while keeping state.yaml parseable.
	rewritten := rewriteUnsafeBlockScalarIndents(data)
	if err := yamlBytesParse(rewritten); err != nil {
		return nil, fmt.Errorf("failed to marshal parseable state YAML: %w", err)
	}
	return rewritten, nil
}

// Write writes the state to the state file atomically with fsync
func (bb *Blackboard) Write(state *models.State) error {
	err := bb.fileLock.WithLockOperation("write", func() error {
		data, err := marshalStateForWrite(state)
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
	err := bb.fileLock.WithLockOperation("modify", func() error {
		data, err := os.ReadFile(bb.statePath)
		if err != nil {
			return fmt.Errorf("failed to read state: %w", err)
		}

		var state models.State
		if err := yaml.Unmarshal(data, &state); err != nil {
			return &errors.StateSchemaError{Operation: "state modify", Err: err}
		}

		normalizeAgentRoles(&state)
		normalizeTaskAttempts(&state)

		if err := fn(&state); err != nil {
			return fmt.Errorf("modification function failed: %w", err)
		}

		data, err = marshalStateForWrite(&state)
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
