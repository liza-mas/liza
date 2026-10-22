package testguard

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// maxSleepCallsInTests is a ratchet: it should only decrease as sleeps are
// replaced with deterministic synchronization. Raise it only when a new sleep
// is genuinely unavoidable (e.g. testing real wall-clock behavior).
// +2: admission-slot wall-clock deadline and budget-exclusion tests (ADR-0209);
// +1: validation-preflight slow-success probe (D-163);
// +1: no submit progress written after return, an absence over time (ADR-0210);
// +1: a budget changed after part of a submit's real slot wait (ADR-0216).
const maxSleepCallsInTests = 16

func TestSleepUsageBudget(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	sleepCalls := 0

	for _, dir := range sourceDirs {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			sameFile := path == thisFile
			if runtime.GOOS == "windows" {
				sameFile = strings.EqualFold(filepath.Clean(path), filepath.Clean(thisFile))
			}
			if d.IsDir() || !strings.HasSuffix(path, "_test.go") || sameFile {
				return nil
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			sleepCalls += bytes.Count(data, []byte("time.Sleep("))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s failed: %v", dir, err)
		}
	}

	if sleepCalls > maxSleepCallsInTests {
		t.Fatalf("time.Sleep() usage budget exceeded: got %d, want <= %d", sleepCalls, maxSleepCallsInTests)
	}
}
