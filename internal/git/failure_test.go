package git

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Each class comes from a real git failure in a scratch repository.
func TestClassifyFailureNamesRealGitFailures(t *testing.T) {
	t.Parallel()
	repoDir := setupTestRepo(t)
	g := New(repoDir)

	_, err := g.DiffFiles(repoDir, "0000000000000000000000000000000000000001", "main")
	if code, class := ClassifyFailure(err); code != 128 || class != FailureBadRevision {
		t.Errorf("missing base: ClassifyFailure = %d, %s; want 128, %s (err %v)", code, class, FailureBadRevision, err)
	}

	lock := filepath.Join(repoDir, ".git", "config.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = g.exec("config", "liza.probe", "1")
	if _, class := ClassifyFailure(err); class != FailureLockContention {
		t.Errorf("held config.lock: class %s, want %s (err %v)", class, FailureLockContention, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}

	// A config that cannot be opened, the shape a Windows reader meets while
	// another process replaces the file.
	config := filepath.Join(repoDir, ".git", "config")
	if err := os.Rename(config, config+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(config, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = g.DiffFiles(repoDir, "main", "main")
	if runtime.GOOS != "windows" { // Windows git may read a directory as an empty config.
		if code, class := ClassifyFailure(err); code != 128 || class != FailureConfigUnreadable {
			t.Errorf("unreadable config: ClassifyFailure = %d, %s; want 128, %s (err %v)", code, class, FailureConfigUnreadable, err)
		}
	}
}

func TestClassifyFailureWithoutExitStatus(t *testing.T) {
	t.Parallel()
	if code, class := ClassifyFailure(errors.New("git [diff] failed: exec: not found")); code != -1 || class != FailureNotRun {
		t.Fatalf("ClassifyFailure = %d, %s; want -1, %s", code, class, FailureNotRun)
	}
}
