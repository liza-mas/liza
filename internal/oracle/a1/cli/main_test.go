package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestMain(m *testing.M) {
	if line := windowsExclusion(runtime.GOOS); line != "" {
		fmt.Println(line)
		os.Exit(0)
	}
	oracle.RunMain(m)
}

// windowsExclusion returns the one-line notice printed instead of running the
// package, or "" where the package runs. A runtime guard, not a build
// constraint: TestOracleCLIBoundary rejects build restrictions (D57).
func windowsExclusion(goos string) string {
	if goos == "windows" {
		return "A1 CLI oracle not run on Windows (D57): D31 git-only PATH isolation is unproven there"
	}
	return ""
}

func TestOracleCLIWindowsExclusion(t *testing.T) {
	line := windowsExclusion("windows")
	if line == "" || strings.Contains(line, "\n") {
		t.Fatalf("windows exclusion must be one non-empty line, got %d bytes", len(line))
	}
	for _, ref := range []string{"D57", "D31"} {
		if !strings.Contains(line, ref) {
			t.Errorf("windows exclusion line does not name %s", ref)
		}
	}
	for _, goos := range []string{"linux", "darwin"} {
		if got := windowsExclusion(goos); got != "" {
			t.Errorf("windowsExclusion(%q) must be empty, got %d bytes", goos, len(got))
		}
	}
}
