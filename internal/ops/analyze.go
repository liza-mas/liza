package ops

import (
	"fmt"
	"os"
	"time"

	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
)

// AnalyzeResult contains the outcome of a circuit breaker analysis.
type AnalyzeResult struct {
	Triggered  bool   `json:"triggered"`
	Pattern    string `json:"pattern"`
	Severity   string `json:"severity"`
	Evidence   string `json:"evidence"`
	ReportPath string `json:"report_path"`
}

// Analyze detects circuit breaker patterns from blackboard anomalies. Generates
// a report and transitions system mode to CIRCUIT_BREAKER_TRIPPED if triggered.
// No terminal I/O.
func Analyze(projectRoot string) (*AnalyzeResult, error) {
	lizaPaths := paths.New(projectRoot)
	statePath := lizaPaths.StatePath()
	reportPath := lizaPaths.CircuitBreakerReportPath()

	blackboard := db.For(statePath)

	state, err := blackboard.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}
	timestamp := time.Now()

	result := analysis.DetectPatterns(state.Anomalies)

	if !result.Triggered {
		err := blackboard.Modify(func(s *models.State) error {
			s.CircuitBreaker.LastCheck = timestamp
			s.CircuitBreaker.Status = "OK"
			s.CircuitBreaker.CurrentTrigger = nil

			// Only reset mode if it was tripped by a previous analysis.
			// Don't un-pause a manually paused system.
			if s.Config.Mode == models.SystemModeCircuitBreakerTripped {
				s.Config.Mode = models.SystemModeRunning
			}

			s.CircuitBreaker.History = append(s.CircuitBreaker.History, models.CircuitBreakerHistory{
				Timestamp: timestamp,
				Pattern:   nil,
				Result:    "OK",
			})

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to update circuit breaker status: %w", err)
		}

		return &AnalyzeResult{
			Triggered: false,
		}, nil
	}

	// Pattern triggered — generate report
	report := analysis.GenerateReport(result, state.Anomalies, timestamp)

	err = os.WriteFile(reportPath, []byte(report), 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write report: %w", err)
	}

	err = blackboard.Modify(func(s *models.State) error {
		s.CircuitBreaker.LastCheck = timestamp
		s.CircuitBreaker.Status = "TRIGGERED"
		s.CircuitBreaker.CurrentTrigger = &models.CircuitBreakerTrigger{
			Timestamp:  timestamp,
			Pattern:    result.Pattern,
			Severity:   result.Severity,
			ReportFile: reportPath,
		}

		s.Config.Mode = models.SystemModeCircuitBreakerTripped

		pattern := result.Pattern
		severity := result.Severity
		s.CircuitBreaker.History = append(s.CircuitBreaker.History, models.CircuitBreakerHistory{
			Timestamp: timestamp,
			Pattern:   &pattern,
			Severity:  &severity,
			Result:    "TRIGGERED",
		})

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to update circuit breaker state: %w", err)
	}

	return &AnalyzeResult{
		Triggered:  true,
		Pattern:    result.Pattern,
		Severity:   result.Severity,
		Evidence:   result.Evidence,
		ReportPath: reportPath,
	}, nil
}
