package embedded

import (
	"strings"
	"testing"
)

func TestConsoleShDeprecationNotice(t *testing.T) {
	script := string(consoleScriptContent)

	// Deprecation notice must be present
	if !strings.Contains(script, "deprecated") {
		t.Fatal("console.sh missing deprecation notice")
	}

	// Must mention liza tui as replacement
	if !strings.Contains(script, "liza tui") {
		t.Fatal("deprecation notice must mention 'liza tui' as replacement")
	}

	// Deprecation notice must appear before the first dashboard command
	deprecIdx := strings.Index(script, "deprecated")
	dashboardIdx := strings.Index(script, "liza status")
	if deprecIdx < 0 || dashboardIdx < 0 {
		t.Fatal("could not locate deprecation notice or dashboard command")
	}
	if deprecIdx >= dashboardIdx {
		t.Fatal("deprecation notice must appear before dashboard output (liza status)")
	}
}
