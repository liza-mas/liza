package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// D-80: a correction writer joining an ordered shared-file chain is placed in
// its single commissioning add-tasks, never left for its planner to discover.
func TestWriterPlacementWakeContract_D80(t *testing.T) {
	t.Parallel()
	content, err := os.ReadFile(filepath.Join("..", "..", "internal/prompts/templates/wake_blocked_tasks.tmpl"))
	if err != nil {
		t.Fatalf("read blocked-task wake guidance: %v", err)
	}
	text := string(content)
	for _, marker := range []string{
		"reserve_successors",
		"descendant_dependencies",
		"max_outputs",
		"--human-action",
	} {
		if !strings.Contains(text, marker) {
			t.Errorf("wake guidance missing writer-placement marker %q", marker)
		}
	}
}
