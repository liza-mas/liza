package testguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// TestTrackedPathsAreWindowsSafe fails when Git tracks a path that a Windows
// checkout cannot create (D53). It never skips: a missing Git or repository is
// a failure. It reports offending paths only, never environment values (G1.4).
func TestTrackedPathsAreWindowsSafe(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))

	env, err := oracle.ChildEnv(oracle.EnvBase{Home: t.TempDir(), Path: os.Getenv("PATH"), Tmp: t.TempDir()})
	if err != nil {
		t.Fatalf("build git environment: %v", err)
	}
	cmd := exec.Command("git", "-C", repoRoot, "ls-files", "-z")
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files -z in %s failed: %v", repoRoot, err)
	}

	var offending []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" {
			continue
		}
		if problem := oracle.WindowsPathProblem(p); problem != "" {
			offending = append(offending, p+": "+problem)
		}
	}
	if len(offending) > 0 {
		t.Fatalf("%d tracked path(s) are invalid on Windows:\n  %s", len(offending), strings.Join(offending, "\n  "))
	}
}
