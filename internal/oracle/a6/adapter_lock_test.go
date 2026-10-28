package a6

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/filelock"
)

// holdOracleRunnerLock keeps the production runner fence held until the matching
// CLI invocation returns. The persisted record alone never simulates liveness.
func holdOracleRunnerLock(t *testing.T, root string) func() {
	t.Helper()
	dir := filepath.Join(root, ".liza", "deferred-validation")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	held, acquired, err := filelock.New(filepath.Join(dir, "runner")).TryHold("oracle-runner")
	if err != nil || !acquired {
		t.Fatalf("acquire oracle runner fence: acquired=%t error=%v", acquired, err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			if err := held.Release(); err != nil {
				t.Errorf("release oracle runner fence: %v", err)
			}
		})
	}
	t.Cleanup(release)
	return release
}
