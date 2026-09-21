package ops

import (
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

type analyzeTestHooks struct {
	beforeModify func()
}

var testAnalyzeHooks *analyzeTestHooks

// AnalyzeResult contains the outcome of a circuit breaker analysis.
type AnalyzeResult struct {
	Triggered      bool                               `json:"triggered"`
	Pattern        string                             `json:"pattern"`
	Severity       string                             `json:"severity"`
	Evidence       string                             `json:"evidence"`
	Response       models.CircuitBreakerResponseType  `json:"response"`
	Classification models.CircuitBreakerEvidenceClass `json:"classification"`
	Explanation    string                             `json:"explanation"`
	ReportPath     string                             `json:"report_path"`
	RejectionRCA   *RejectionRCATelemetry             `json:"rejection_rca"`
}

// RejectionRCATelemetry reports the rejection-RCA gate. Every cumulative
// dimension is derived from immutable task-history entries — the gate's
// TaskEventBlocked, rejection_rca_recorded and rejection_rca_resumed — so a
// re-gated task still reports its earlier cycles; only CurrentlyGated reads the
// live record. Token consumption is reported by the usage report, not here.
type RejectionRCATelemetry struct {
	TasksGated          int            `json:"tasks_gated"`
	GateCycles          int            `json:"gate_cycles"`
	CurrentlyGated      int            `json:"currently_gated"`
	Causes              map[string]int `json:"causes,omitempty"`
	UnrecognizedCauses  int            `json:"unrecognized_causes"`
	MedianSecondsToGate int64          `json:"median_seconds_to_gate"`
	MaxSecondsToGate    int64          `json:"max_seconds_to_gate"`
	RecoveryPaths       map[string]int `json:"recovery_paths,omitempty"`
	ResumedThenTerminal int            `json:"resumed_then_terminal"`
	ResumedThenRegated  int            `json:"resumed_then_regated"`
	ResumedStillOpen    int            `json:"resumed_still_open"`
}

// IsEmpty reports whether no task was ever gated.
func (t *RejectionRCATelemetry) IsEmpty() bool {
	return t == nil || t.GateCycles == 0 && t.CurrentlyGated == 0 && len(t.Causes) == 0 && len(t.RecoveryPaths) == 0
}

// Analyze detects circuit breaker patterns from blackboard anomalies. Generates
// a report and transitions system mode to CIRCUIT_BREAKER_TRIPPED if triggered.
// No terminal I/O.
func Analyze(projectRoot string) (*AnalyzeResult, error) {
	lizaPaths := paths.New(projectRoot)
	statePath := lizaPaths.StatePath()
	reportPath := lizaPaths.CircuitBreakerReportPath()

	blackboard := db.For(statePath)

	if _, err := blackboard.Read(); err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}
	if testAnalyzeHooks != nil && testAnalyzeHooks.beforeModify != nil {
		testAnalyzeHooks.beforeModify()
	}

	var committed analysis.PatternResult
	var committedReportPath string
	var timestamp time.Time
	var rejectionRCA *RejectionRCATelemetry
	err := blackboard.Modify(func(s *models.State) error {
		timestamp = time.Now()
		rejectionRCA = deriveRejectionRCATelemetry(s.Tasks)
		var consideredAnomalies []models.Anomaly
		var suppressedCount int
		committed, consideredAnomalies, suppressedCount = analysis.DetectUnacknowledgedPatterns(s)
		s.CircuitBreaker.LastCheck = timestamp

		activeResponse := s.CircuitBreaker.CurrentResponse
		if activeResponse != nil && activeResponse.Response == models.CircuitBreakerResponseHalt {
			committed = patternResultFromResponse(activeResponse)
			committedReportPath = activeResponse.ReportFile
			return nil
		}
		if activeResponse != nil && activeResponse.Pattern == "provider_audit_degradation" && activeResponse.Response == models.CircuitBreakerResponseCheckpoint {
			if committed.Response != models.CircuitBreakerResponseHalt {
				committed = patternResultFromResponse(activeResponse)
				committedReportPath = activeResponse.ReportFile
				return nil
			}
			if err := supersedeActiveProviderCheckpoint(s, activeResponse); err != nil {
				return err
			}
		}

		if committed.Pattern == "" {
			s.CircuitBreaker.Status = "OK"
			s.CircuitBreaker.CurrentTrigger = nil
			if s.Config.Mode == models.SystemModeCircuitBreakerTripped {
				s.Config.Mode = models.SystemModeRunning
			}
			s.CircuitBreaker.History = append(s.CircuitBreaker.History, models.CircuitBreakerHistory{
				Timestamp: timestamp,
				Result:    "OK",
			})
			return nil
		}

		report := analysis.GenerateReport(committed, consideredAnomalies, timestamp, suppressedCount)
		if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
			return fmt.Errorf("failed to write report: %w", err)
		}
		committedReportPath = reportPath

		applyCircuitBreakerResponse(s, committed, timestamp, reportPath)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update circuit breaker state: %w", err)
	}

	result := &AnalyzeResult{
		Triggered:      committed.Triggered,
		Pattern:        committed.Pattern,
		Severity:       committed.Severity,
		Evidence:       committed.Evidence,
		Response:       committed.Response,
		Classification: committed.Classification,
		Explanation:    committed.Explanation,
		RejectionRCA:   rejectionRCA,
	}
	if committed.Pattern != "" {
		result.ReportPath = committedReportPath
	}
	return result, nil
}

// deriveRejectionRCATelemetry is read-only over the tasks. Convergence is
// judged per rejection_rca_resumed entry: a later gate entry on the same task
// is re-gating, a terminal status is convergence, anything else is still open.
func deriveRejectionRCATelemetry(tasks []models.Task) *RejectionRCATelemetry {
	telemetry := &RejectionRCATelemetry{}
	var intervals []time.Duration
	for i := range tasks {
		task := &tasks[i]
		if task.RejectionRCAGateOpen() {
			telemetry.CurrentlyGated++
		}
		var gatedAt []time.Time
		// A corrected RCA appends another recorded entry for the same cycle;
		// only the last one per gated_at is the cycle's classification.
		cycleCauses := map[string][]string{}
		for _, entry := range task.History {
			switch entry.Event {
			case models.TaskEventBlocked:
				if entry.Extra["blocked_class"] != models.BlockedReasonRejectionRCARequired {
					continue
				}
				at, atOK := historyTime(entry.Extra, "gated_at")
				first, firstOK := historyTime(entry.Extra, "first_rejection_at")
				if atOK && firstOK {
					intervals = append(intervals, at.Sub(first))
				}
				if !atOK {
					at = entry.Time
				}
				gatedAt = append(gatedAt, at)
			case models.TaskEventRejectionRCARecorded:
				cycle, _ := entry.Extra["gated_at"].(string)
				cycleCauses[cycle] = historyStrings(entry.Extra, "causes")
			case models.TaskEventRejectionRCAResumed:
				if path, ok := entry.Extra["recovery_path"].(string); ok && path != "" {
					telemetry.RecoveryPaths = countInto(telemetry.RecoveryPaths, path)
				}
			}
		}
		for _, causes := range cycleCauses {
			for _, cause := range causes {
				if !models.IsKnownRejectionCause(cause) {
					telemetry.UnrecognizedCauses++
					cause = models.RejectionCauseUnknown
				}
				telemetry.Causes = countInto(telemetry.Causes, cause)
			}
		}
		if len(gatedAt) == 0 {
			continue
		}
		telemetry.TasksGated++
		telemetry.GateCycles += len(gatedAt)
		for _, entry := range task.History {
			if entry.Event != models.TaskEventRejectionRCAResumed {
				continue
			}
			switch {
			case slices.ContainsFunc(gatedAt, func(at time.Time) bool { return at.After(entry.Time) }):
				telemetry.ResumedThenRegated++
			case task.Status.IsTerminal():
				telemetry.ResumedThenTerminal++
			default:
				telemetry.ResumedStillOpen++
			}
		}
	}
	if len(intervals) > 0 {
		slices.Sort(intervals)
		median := intervals[len(intervals)/2]
		if len(intervals)%2 == 0 {
			median = (intervals[len(intervals)/2-1] + median) / 2
		}
		telemetry.MedianSecondsToGate = int64(median.Seconds())
		telemetry.MaxSecondsToGate = int64(intervals[len(intervals)-1].Seconds())
	}
	return telemetry
}

func countInto(counts map[string]int, key string) map[string]int {
	if counts == nil {
		counts = map[string]int{}
	}
	counts[key]++
	return counts
}

// historyTime reads an RFC3339 detail key as written by the gate.
func historyTime(extra map[string]any, key string) (time.Time, bool) {
	raw, ok := extra[key].(string)
	if !ok {
		return time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, raw)
	return at, err == nil
}

// historyStrings reads a string-list detail key, tolerating the []any shape a
// YAML round trip produces.
func historyStrings(extra map[string]any, key string) []string {
	switch values := extra[key].(type) {
	case []string:
		return values
	case []any:
		strs := make([]string, 0, len(values))
		for _, value := range values {
			if str, ok := value.(string); ok {
				strs = append(strs, str)
			}
		}
		return strs
	}
	return nil
}

func patternResultFromResponse(response *models.CircuitBreakerResponse) analysis.PatternResult {
	return analysis.PatternResult{
		Triggered:      response.Response == models.CircuitBreakerResponseHalt,
		Pattern:        response.Pattern,
		Severity:       response.Severity,
		Response:       response.Response,
		Classification: response.Classification,
		Explanation:    response.Explanation,
	}
}

func supersedeActiveProviderCheckpoint(state *models.State, response *models.CircuitBreakerResponse) error {
	for i := len(state.CircuitBreaker.History) - 1; i >= 0; i-- {
		entry := &state.CircuitBreaker.History[i]
		if !entry.Timestamp.Equal(response.Timestamp) || entry.Response != response.Response || entry.Pattern == nil || *entry.Pattern != response.Pattern {
			continue
		}
		if entry.Resolution != nil || entry.ResolvedAt != nil {
			return fmt.Errorf("active provider checkpoint history boundary is already acknowledged")
		}
		if entry.SupersededByResponse != "" {
			return fmt.Errorf("active provider checkpoint history boundary is already superseded")
		}
		entry.SupersededByResponse = models.CircuitBreakerResponseHalt
		return nil
	}

	return fmt.Errorf("active provider checkpoint has no matching history boundary")
}

func applyCircuitBreakerResponse(state *models.State, result analysis.PatternResult, timestamp time.Time, reportPath string) {
	pattern := result.Pattern
	severity := result.Severity
	historyResult := string(result.Response)

	switch result.Response {
	case models.CircuitBreakerResponseWarning:
		// Historical evidence is observation-only; preserve all active state.
	case models.CircuitBreakerResponseCheckpoint:
		state.CircuitBreaker.Status = "OK"
		state.CircuitBreaker.CurrentTrigger = nil
		state.Sprint.Status = models.SprintStatusCheckpoint
		// Stamp the checkpoint the way ops.SprintCheckpoint does. Without it a
		// breaker checkpoint is indistinguishable from the previous one, and a
		// consumer keyed on checkpoint identity — the auto checkpoint-summary
		// emitter — cannot tell it apart. CheckpointTrigger is deliberately
		// left alone: it may still carry a transition the orchestrator's
		// PreWork has not executed yet, and overwriting it here would drop
		// that pending transition.
		state.Sprint.Timeline.CheckpointAt = &timestamp
		state.PendingCheckpointSummary = &models.PendingCheckpointSummary{At: timestamp}
		state.CircuitBreaker.CurrentResponse = circuitBreakerResponse(result, timestamp, reportPath)
	case models.CircuitBreakerResponseHalt:
		historyResult = "TRIGGERED"
		state.CircuitBreaker.Status = "TRIGGERED"
		state.CircuitBreaker.CurrentTrigger = &models.CircuitBreakerTrigger{
			Timestamp:  timestamp,
			Pattern:    result.Pattern,
			Severity:   result.Severity,
			ReportFile: reportPath,
		}
		state.CircuitBreaker.CurrentResponse = circuitBreakerResponse(result, timestamp, reportPath)
		state.Config.Mode = models.SystemModeCircuitBreakerTripped
	}

	state.CircuitBreaker.History = append(state.CircuitBreaker.History, models.CircuitBreakerHistory{
		Timestamp:      timestamp,
		Pattern:        &pattern,
		Severity:       &severity,
		Result:         historyResult,
		Response:       result.Response,
		Classification: result.Classification,
		Explanation:    result.Explanation,
	})
}

func circuitBreakerResponse(result analysis.PatternResult, timestamp time.Time, reportPath string) *models.CircuitBreakerResponse {
	return &models.CircuitBreakerResponse{
		Timestamp:      timestamp,
		Pattern:        result.Pattern,
		Severity:       result.Severity,
		Response:       result.Response,
		Classification: result.Classification,
		Explanation:    result.Explanation,
		ReportFile:     reportPath,
	}
}
