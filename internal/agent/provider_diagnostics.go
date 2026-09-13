package agent

import (
	"encoding/json"
	"strings"
)

// providerDiagnosticLines separates provider failures from quoted tool output and
// assistant text. Never recursively search a structured transcript for errors.
func providerDiagnosticLines(output string) []string {
	var diagnostics []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[") {
			diagnostics = append(diagnostics, line)
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			// An incomplete transcript event is not a plain provider diagnostic.
			continue
		}
		var message string
		switch event.Type {
		case "error":
			message = event.Message
			if message == "" {
				message = event.Error.Message
			}
		case "turn.failed":
			message = event.Error.Message
		case "result":
			if event.IsError {
				message = event.Result
			}
		}
		diagnostics = append(diagnostics, strings.Split(message, "\n")...)
	}
	return diagnostics
}
