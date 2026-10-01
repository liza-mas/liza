package agent

import (
	"encoding/json"
	"strings"
)

// typographicQuotes maps the quotes providers print (Codex writes "You’ve") to
// the ASCII ones diagnostic patterns are written with.
var typographicQuotes = strings.NewReplacer("\u2018", "'", "\u2019", "'", "\u02BC", "'", "\u201C", `"`, "\u201D", `"`)

// providerDiagnosticLines separates provider failures from quoted tool output and
// assistant text. Never recursively search a structured transcript for errors.
// Returned lines have typographic quotes normalised to ASCII.
func providerDiagnosticLines(output string) []string {
	var diagnostics []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "[") {
			diagnostics = append(diagnostics, typographicQuotes.Replace(line))
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
		// Normalise after parsing: mapping “ to " in the raw line would break the JSON.
		diagnostics = append(diagnostics, strings.Split(typographicQuotes.Replace(message), "\n")...)
	}
	return diagnostics
}
