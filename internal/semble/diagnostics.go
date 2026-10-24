package semble

import (
	"fmt"
	"strings"
)

const maxDiagnosticBytes = 1024

type DiagnosticKind string

const (
	DiagnosticMissingExecutable       DiagnosticKind = "missing_executable"
	DiagnosticModelUnavailableOffline DiagnosticKind = "model_unavailable_offline"
	DiagnosticExecutionFailure        DiagnosticKind = "execution_failure"
)

// Diagnostic is a bounded operator-visible validation diagnostic.
type Diagnostic struct {
	Kind    DiagnosticKind
	Message string
}

func boundedCommandDiagnostic(kind DiagnosticKind, result CommandResult, err error) string {
	prefix := "semble: execution failed"
	if kind == DiagnosticModelUnavailableOffline {
		prefix = "semble: model unavailable offline"
	}
	var parts []string
	if err != nil {
		parts = append(parts, err.Error())
	}
	if result.ExitCode != 0 {
		parts = append(parts, fmt.Sprintf("exit code %d", result.ExitCode))
	}
	if output := strings.TrimSpace(strings.Join([]string{result.Stdout, result.Stderr}, "\n")); output != "" {
		parts = append(parts, output)
	}
	return boundedDiagnosticMessage(prefix, strings.Join(parts, ": "))
}

func boundedDiagnosticMessage(prefix, detail string) string {
	message := strings.TrimSpace(prefix)
	if detail = strings.TrimSpace(detail); detail != "" {
		message += ": " + detail
	}
	if len(message) <= maxDiagnosticBytes {
		return message
	}
	return message[:maxDiagnosticBytes]
}

func isOfflineModelFailure(result CommandResult, err error) bool {
	text := strings.ToLower(strings.Join([]string{result.Stdout, result.Stderr}, "\n"))
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}
	markers := []string{
		"localentrynotfounderror",
		"hf_hub_offline",
		"offline",
		"not in cache",
		"from_pretrained",
	}
	for _, marker := range markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
