package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

const lifecycleMetricsMaxBytes = 64 * 1024

var lifecycleMetricOperations = [...]string{
	"submit-for-review", "submit-verdict", "mark-blocked", "assess-blocked",
	"assess-hypothesis-exhausted", "claim-task", "claim-reviewer-task", "release-claim",
	"wt-merge", "recover-task", "retarget-dependency", "apply-dependency-repair",
	"repair-superseded-dependencies", "cancel-task", "supersede-task", "unblock-task",
	"set-task-output", "handoff", "recover-agent", "transition-attempt",
}

var lifecycleMetricOutcomes = [...]string{
	"COMPLETED", "ALREADY_COMPLETED", "ALREADY_TRANSITIONED", "STALE_CALLER",
	"STATE_CHANGED", "RETRYABLE", "INVALID_INPUT", "FORBIDDEN",
}

// LifecycleSprintIdentity is captured from the invocation's authoritative state.
// Keep it until completion: rereading the current sprint would misattribute late
// observations after sprint rollover.
type LifecycleSprintIdentity struct {
	ID      string    `json:"id"`
	Number  int       `json:"number"`
	Started time.Time `json:"started"`
}

// CaptureLifecycleSprint extracts the immutable identity of an observed sprint.
func CaptureLifecycleSprint(sprint models.Sprint) LifecycleSprintIdentity {
	return LifecycleSprintIdentity{ID: sprint.ID, Number: sprint.Number, Started: sprint.Timeline.Started.UTC()}
}

func (s LifecycleSprintIdentity) valid() bool {
	return s.ID != "" && len(s.ID) <= 256 && s.Number > 0 && !s.Started.IsZero()
}

func (s LifecycleSprintIdentity) matches(other LifecycleSprintIdentity) bool {
	return s.ID == other.ID && s.Number == other.Number && s.Started.Equal(other.Started)
}

func lifecycleMetricsPath(projectRoot string, sprint LifecycleSprintIdentity) string {
	sprint.Started = sprint.Started.UTC()
	data, _ := json.Marshal(sprint)
	digest := sha256.Sum256(data)
	return filepath.Join(paths.New(projectRoot).LifecycleMetricsDir(), hex.EncodeToString(digest[:])+".json")
}

type lifecycleCounterFile struct {
	Version       int                          `json:"version"`
	Sprint        LifecycleSprintIdentity      `json:"sprint"`
	ObservedSince time.Time                    `json:"observed_since"`
	LastUpdated   time.Time                    `json:"last_updated"`
	Counts        map[string]map[string]uint64 `json:"counts"`
}

func newLifecycleCounters(sprint LifecycleSprintIdentity, now time.Time) lifecycleCounterFile {
	counters := lifecycleCounterFile{Version: 1, Sprint: sprint, ObservedSince: now, LastUpdated: now,
		Counts: make(map[string]map[string]uint64, len(lifecycleMetricOperations))}
	for _, operation := range lifecycleMetricOperations {
		row := make(map[string]uint64, len(lifecycleMetricOutcomes))
		for _, outcome := range lifecycleMetricOutcomes {
			row[outcome] = 0
		}
		counters.Counts[operation] = row
	}
	return counters
}

// RecordLifecycleOutcome records exactly one outer invocation. Call only after
// releasing all operation locks; its telemetry lock is a leaf. An error is an
// observability warning and must never change or retry the operation's result.
func RecordLifecycleOutcome(projectRoot string, sprint LifecycleSprintIdentity, operation, outcome string) error {
	if !sprint.valid() {
		return errors.New("lifecycle metrics unavailable: unknown sprint identity")
	}
	if !slices.Contains(lifecycleMetricOperations[:], operation) || !slices.Contains(lifecycleMetricOutcomes[:], outcome) {
		return errors.New("lifecycle metrics unavailable: unknown operation or outcome")
	}
	filename := lifecycleMetricsPath(projectRoot, sprint)
	if err := os.MkdirAll(filepath.Dir(filename), 0700); err != nil {
		return fmt.Errorf("lifecycle metrics unavailable: %w", err)
	}
	err := filelock.New(filename).WithLockOperation("record-lifecycle-outcome", func() error {
		counters, err := readLifecycleCounterFile(filename, sprint)
		now := time.Now().UTC()
		if errors.Is(err, os.ErrNotExist) {
			counters = newLifecycleCounters(sprint, now)
		} else if err != nil {
			return err // Existing invalid data must never be silently reset.
		}
		if counters.Counts[operation][outcome] == math.MaxUint64 {
			return errors.New("counter capacity exhausted")
		}
		counters.Counts[operation][outcome]++
		counters.LastUpdated = now
		return writeLifecycleCounterFile(filename, counters)
	})
	if err != nil {
		return fmt.Errorf("lifecycle metrics unavailable: %w", err)
	}
	return nil
}

// ReadLifecycleOutcomes takes a bounded snapshot before any state-write lock.
// Atomic replacement gives readers a complete old or new file without another
// lock. Missing or damaged data is unavailable rather than a zero count.
func ReadLifecycleOutcomes(projectRoot string, sprint LifecycleSprintIdentity) *models.LifecycleOutcomeMetrics {
	result := &models.LifecycleOutcomeMetrics{}
	if !sprint.valid() {
		result.Warning = "lifecycle metrics unavailable: unknown sprint identity"
		return result
	}
	counters, err := readLifecycleCounterFile(lifecycleMetricsPath(projectRoot, sprint), sprint)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.Warning = "lifecycle metrics unavailable: no observation window"
		} else {
			result.Warning = "lifecycle metrics unavailable: " + err.Error()
		}
		return result
	}
	result.Available = true
	result.ObservedSince = &counters.ObservedSince
	result.LastUpdated = &counters.LastUpdated
	result.Counts = counters.Counts
	return result
}

func readLifecycleCounterFile(filename string, sprint LifecycleSprintIdentity) (lifecycleCounterFile, error) {
	var counters lifecycleCounterFile
	f, err := os.Open(filename)
	if err != nil {
		return counters, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, lifecycleMetricsMaxBytes+1))
	if err != nil {
		return counters, err
	}
	if len(data) > lifecycleMetricsMaxBytes {
		return counters, errors.New("counter file exceeds size limit")
	}
	if err := json.Unmarshal(data, &counters); err != nil {
		return counters, errors.New("invalid counter file encoding")
	}
	if counters.Version != 1 || !counters.Sprint.matches(sprint) || counters.ObservedSince.IsZero() || counters.LastUpdated.IsZero() {
		return counters, errors.New("invalid counter file identity or observation window")
	}
	if len(counters.Counts) != len(lifecycleMetricOperations) {
		return counters, errors.New("invalid operation counter matrix")
	}
	for _, operation := range lifecycleMetricOperations {
		row := counters.Counts[operation]
		if len(row) != len(lifecycleMetricOutcomes) {
			return counters, errors.New("invalid outcome counter matrix")
		}
		for _, outcome := range lifecycleMetricOutcomes {
			if _, exists := row[outcome]; !exists {
				return counters, errors.New("invalid outcome counter matrix")
			}
		}
	}
	return counters, nil
}

func writeLifecycleCounterFile(filename string, counters lifecycleCounterFile) error {
	data, err := json.Marshal(counters)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(filename), ".lifecycle-counters-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filename)
}

// LifecycleSprintChangedError prevents a counter snapshot from being attached
// to a different sprint after rollover.
type LifecycleSprintChangedError struct{}

func (*LifecycleSprintChangedError) Error() string {
	return "sprint changed while computing metrics; requery current state"
}

// SafeDetails exposes actionable recovery without implying a metric write.
func (*LifecycleSprintChangedError) SafeDetails() map[string]any {
	return map[string]any{"operation": "update-sprint-metrics", "outcome": "STATE_CHANGED", "safe_action": "requery", "effects": "none"}
}

func applyLifecycleSprintMetrics(state *models.State, sprint LifecycleSprintIdentity, metrics models.SprintMetrics) error {
	if !CaptureLifecycleSprint(state.Sprint).matches(sprint) {
		return &LifecycleSprintChangedError{}
	}
	state.Sprint.Metrics = metrics
	return nil
}
