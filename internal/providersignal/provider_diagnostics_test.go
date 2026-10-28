package providersignal

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Providers print typographic quotes; patterns are written with ASCII ones.
func TestProviderDiagnosticLinesNormaliseTypographicQuotes(t *testing.T) {
	const typographic = "You’ve ‘hit’ youʼre “limit”"
	const want = `You've 'hit' you're "limit"`
	quoted, err := json.Marshal(typographic)
	if err != nil {
		t.Fatal(err)
	}
	for name, output := range map[string]string{
		"plain diagnostic": typographic,
		"error event":      fmt.Sprintf(`{"type":"error","message":%s}`, quoted),
		"nested error":     fmt.Sprintf(`{"type":"error","error":{"message":%s}}`, quoted),
		"failed turn":      fmt.Sprintf(`{"type":"turn.failed","error":{"message":%s}}`, quoted),
		"failed result":    fmt.Sprintf(`{"type":"result","is_error":true,"result":%s}`, quoted),
	} {
		t.Run(name, func(t *testing.T) {
			if got := DiagnosticLines(output); len(got) != 1 || got[0] != want {
				t.Fatalf("DiagnosticLines = %q, want [%q]", got, want)
			}
		})
	}
}
