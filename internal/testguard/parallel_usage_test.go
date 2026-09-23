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

// minParallelCallsInTests is a floor ratchet: it should only increase as more
// stateless tests opt into t.Parallel(). Lower it only when tests genuinely
// need sequential execution (e.g. shared process-global state).
const minParallelCallsInTests = 1315

// sourceDirs lists directories containing Go source. Walking only these
// prevents transient directories (worktrees, backups) from affecting the count.
var sourceDirs = []string{"cmd", "internal", "plugin"}

func TestParallelUsageBudget(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	parallelCalls := 0

	for _, dir := range sourceDirs {
		root := filepath.Join(repoRoot, dir)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if path == thisFile {
				return nil
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			parallelCalls += bytes.Count(data, []byte("t.Parallel()"))
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s failed: %v", dir, err)
		}
	}

	if parallelCalls < minParallelCallsInTests {
		t.Fatalf("t.Parallel() usage below minimum: got %d, want >= %d", parallelCalls, minParallelCallsInTests)
	}
}
