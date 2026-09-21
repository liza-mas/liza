package commands

import (
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// AnalyzeCommand runs circuit breaker analysis and prints the result to stdout.
// Delegates business logic to ops.Analyze.
func AnalyzeCommand(projectRoot string) error {
	result, err := ops.Analyze(projectRoot)
	if err != nil {
		return fmt.Errorf("analyze: %w", err)
	}
	writeAnalyzeResult(os.Stdout, result)
	return nil
}

func writeAnalyzeResult(w io.Writer, result *ops.AnalyzeResult) {
	writeCircuitBreakerResult(w, result)
	writeRejectionRCATelemetry(w, result.RejectionRCA)
}

func writeCircuitBreakerResult(w io.Writer, result *ops.AnalyzeResult) {
	if result.Pattern == "" {
		fmt.Fprintln(w, "Circuit breaker: OK — no patterns detected")
		return
	}

	if result.Response == models.CircuitBreakerResponseHalt {
		fmt.Fprintln(w, "🚨 CIRCUIT BREAKER TRIGGERED — HALT")
	} else {
		fmt.Fprintf(w, "Circuit breaker response: %s\n", result.Response)
	}
	fmt.Fprintf(w, "Pattern: %s\n", result.Pattern)
	fmt.Fprintf(w, "Severity: %s\n", result.Severity)
	fmt.Fprintf(w, "Response: %s\n", result.Response)
	fmt.Fprintf(w, "Evidence class: %s\n", result.Classification)
	fmt.Fprintf(w, "Evidence: %s\n", result.Evidence)
	fmt.Fprintf(w, "Explanation: %s\n", result.Explanation)

	switch result.Response {
	case models.CircuitBreakerResponseWarning:
		fmt.Fprintln(w, "State action: none — this evidence was already acknowledged")
	case models.CircuitBreakerResponseCheckpoint:
		fmt.Fprintln(w, "State action: sprint moved to CHECKPOINT")
		fmt.Fprintf(w, "Recovery: run `%s` to continue\n", brand.Command("resume"))
	case models.CircuitBreakerResponseHalt:
		fmt.Fprintln(w, "State action: execution halted")
		fmt.Fprintf(w, "Recovery: run `%s` after remediation\n", brand.Command("resume"))
	}
	if result.ReportPath != "" {
		fmt.Fprintf(w, "\nReport written to: %s\n", result.ReportPath)
	}
}

// writeRejectionRCATelemetry renders the gate block after the circuit-breaker
// output. Token consumption belongs to the usage report and is not a column here.
func writeRejectionRCATelemetry(w io.Writer, telemetry *ops.RejectionRCATelemetry) {
	if telemetry.IsEmpty() {
		fmt.Fprintln(w, "\nRejection RCA gate: no gated tasks")
		return
	}
	fmt.Fprintln(w, "\nRejection RCA gate:")
	fmt.Fprintf(w, "Tasks gated: %d (%d cycles), currently gated: %d\n", telemetry.TasksGated, telemetry.GateCycles, telemetry.CurrentlyGated)
	fmt.Fprintf(w, "Causes: %s (unrecognized folded into unknown: %d)\n", formatCounts(telemetry.Causes), telemetry.UnrecognizedCauses)
	fmt.Fprintf(w, "Time to gate: median %s, max %s\n",
		time.Duration(telemetry.MedianSecondsToGate)*time.Second, time.Duration(telemetry.MaxSecondsToGate)*time.Second)
	fmt.Fprintf(w, "Recovery paths: %s\n", formatCounts(telemetry.RecoveryPaths))
	fmt.Fprintf(w, "Convergence after resume: terminal=%d re-gated=%d still open=%d\n",
		telemetry.ResumedThenTerminal, telemetry.ResumedThenRegated, telemetry.ResumedStillOpen)
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	var parts []string
	for _, key := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, " ")
}
