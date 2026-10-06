package worktreeexclude

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/filelock"
	"github.com/liza-mas/liza/internal/paths"
)

func TestEnsurePrivateExcludeIdempotent(t *testing.T) {
	repo := newGitRepoWithWorktrees(t, "task-one")
	worktree := repo.worktrees["task-one"]
	privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")
	commonExclude := filepath.Join(repo.root, ".git", "info", "exclude")
	commonBefore := readFileString(t, commonExclude)

	if err := os.MkdirAll(filepath.Dir(privateExclude), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(privateExclude), err)
	}
	if err := os.WriteFile(privateExclude, []byte("# existing\nbuild/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", privateExclude, err)
	}

	for i := 0; i < 2; i++ {
		if err := EnsurePrivateExclude(worktree, paths.ProjectDirName()+"/scip/", ".sembleignore"); err != nil {
			t.Fatalf("EnsurePrivateExclude() iteration %d error = %v", i+1, err)
		}
	}

	content := readFileString(t, privateExclude)
	for _, want := range []string{"# existing\n", "build/\n"} {
		if !strings.Contains(content, want) {
			t.Fatalf("private exclude content = %q, want preserved %q", content, want)
		}
	}
	assertLineCount(t, content, paths.ProjectDirName()+"/scip/", 1)
	assertLineCount(t, content, ".sembleignore", 1)
	if got := gitOutput(t, worktree, "config", "--worktree", "--get", "core.excludesFile"); filepath.Clean(got) != filepath.Clean(privateExclude) {
		t.Fatalf("core.excludesFile = %q, want %q", got, privateExclude)
	}
	if got := readFileString(t, commonExclude); got != commonBefore {
		t.Fatalf("common exclude = %q, want unchanged %q", got, commonBefore)
	}
}

func TestEnsurePrivateExcludeReportsConflictingCoreExcludesFile(t *testing.T) {
	t.Run("worktree config", func(t *testing.T) {
		repo := newGitRepoWithWorktrees(t, "task-one")
		worktree := repo.worktrees["task-one"]
		privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")
		conflictingExclude := filepath.Join(t.TempDir(), "other-exclude")

		gitRun(t, worktree, "config", "extensions.worktreeConfig", "true")
		gitRun(t, worktree, "config", "--worktree", "core.excludesFile", conflictingExclude)

		err := EnsurePrivateExclude(worktree, ".sembleignore")
		assertConflictPreserved(t, err, privateExclude, conflictingExclude)
		if got := gitOutput(t, worktree, "config", "--worktree", "--get", "core.excludesFile"); got != conflictingExclude {
			t.Fatalf("core.excludesFile = %q, want preserved conflict %q", got, conflictingExclude)
		}
		assertPrivateExcludeNotWritten(t, privateExclude)
	})

	t.Run("effective repo config before worktree config enabled", func(t *testing.T) {
		repo := newGitRepoWithWorktrees(t, "task-one")
		worktree := repo.worktrees["task-one"]
		privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")
		conflictingExclude := filepath.Join(t.TempDir(), "other-exclude")

		gitRun(t, worktree, "config", "core.excludesFile", conflictingExclude)

		err := EnsurePrivateExclude(worktree, ".sembleignore")
		assertConflictPreserved(t, err, privateExclude, conflictingExclude)
		if got := gitOutput(t, worktree, "config", "--get", "core.excludesFile"); got != conflictingExclude {
			t.Fatalf("effective core.excludesFile = %q, want preserved conflict %q", got, conflictingExclude)
		}
		assertGitConfigUnset(t, worktree, "extensions.worktreeConfig")
		assertPrivateExcludeNotWritten(t, privateExclude)
	})
}

func TestEnsurePrivateExcludeConcurrentSetup(t *testing.T) {
	repo := newGitRepoWithWorktrees(t, "task-one")
	worktree := repo.worktrees["task-one"]
	privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")

	const calls = 24
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for i := 0; i < calls; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- EnsurePrivateExclude(worktree, paths.ProjectDirName()+"/scip/", ".sembleignore")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("EnsurePrivateExclude() concurrent error = %v", err)
		}
	}

	content := readFileString(t, privateExclude)
	assertLineCount(t, content, paths.ProjectDirName()+"/scip/", 1)
	assertLineCount(t, content, ".sembleignore", 1)
	if got := gitOutput(t, worktree, "config", "--worktree", "--get", "core.excludesFile"); filepath.Clean(got) != filepath.Clean(privateExclude) {
		t.Fatalf("core.excludesFile = %q, want %q", got, privateExclude)
	}
}

func TestEnsurePrivateExcludeEnablesWorktreeConfig(t *testing.T) {
	repo := newGitRepoWithWorktrees(t, "task-one")
	worktree := repo.worktrees["task-one"]
	privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")

	if err := EnsurePrivateExclude(worktree, ".sembleignore"); err != nil {
		t.Fatalf("EnsurePrivateExclude() error = %v", err)
	}

	if got := gitOutput(t, worktree, "config", "--get", "extensions.worktreeConfig"); got != "true" {
		t.Fatalf("extensions.worktreeConfig = %q, want true", got)
	}
	if got := gitOutput(t, worktree, "config", "--worktree", "--get", "core.excludesFile"); filepath.Clean(got) != filepath.Clean(privateExclude) {
		t.Fatalf("core.excludesFile = %q, want %q", got, privateExclude)
	}
}

func TestEnsureRepoExcludeKeepsUserExcludesAndIsIdempotent(t *testing.T) {
	userExclude := filepath.Join(t.TempDir(), "user-exclude")
	if err := os.WriteFile(userExclude, []byte("user-owned/\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", userExclude, err)
	}
	// Let Git write the value: a raw Windows path would be read as config escapes.
	configDir := t.TempDir()
	globalConfig := filepath.Join(configDir, "gitconfig")
	gitRun(t, configDir, "config", "--file", globalConfig, "core.excludesFile", userExclude)
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	repo := newGitRepoWithWorktrees(t)
	worktreeExclude := filepath.Join(t.TempDir(), "worktree-exclude")
	gitRun(t, repo.root, "config", "extensions.worktreeConfig", "true")
	gitRun(t, repo.root, "config", "--worktree", "core.excludesFile", worktreeExclude)
	repoExclude := filepath.Join(repo.root, ".git", "info", "exclude")
	if err := os.WriteFile(repoExclude, []byte("# existing\nbuild/"), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", repoExclude, err)
	}
	pattern := "/" + paths.ProjectDirName() + "/"

	for i := 0; i < 2; i++ {
		if err := EnsureRepoExclude(repo.root, pattern); err != nil {
			t.Fatalf("EnsureRepoExclude() iteration %d error = %v", i+1, err)
		}
	}

	content := readFileString(t, repoExclude)
	if !strings.HasPrefix(content, "# existing\nbuild/\n") {
		t.Fatalf("repo exclude content = %q, want existing lines preserved", content)
	}
	assertLineCount(t, content, pattern, 1)
	if got := gitOutput(t, repo.root, "config", "--global", "--get", "core.excludesFile"); got != userExclude {
		t.Fatalf("global core.excludesFile = %q, want unchanged %q", got, userExclude)
	}
	if got := gitOutput(t, repo.root, "config", "--worktree", "--get", "core.excludesFile"); got != worktreeExclude {
		t.Fatalf("worktree core.excludesFile = %q, want unchanged %q", got, worktreeExclude)
	}
	gitRun(t, repo.root, "check-ignore", "-q", paths.ProjectDirName()+"/state.yaml")
}

func TestEnsureRepoExcludeFromLinkedWorktreeWritesSharedExclude(t *testing.T) {
	repo := newGitRepoWithWorktrees(t, "task-one")
	worktree := repo.worktrees["task-one"]
	privateExclude := filepath.Join(revParseGitDir(t, worktree), "info", "exclude")
	pattern := "/" + paths.ProjectDirName() + "/"

	if err := EnsureRepoExclude(worktree, pattern); err != nil {
		t.Fatalf("EnsureRepoExclude() error = %v", err)
	}

	assertLineCount(t, readFileString(t, filepath.Join(repo.root, ".git", "info", "exclude")), pattern, 1)
	assertPrivateExcludeNotWritten(t, privateExclude)
	for _, dir := range []string{repo.root, worktree} {
		gitRun(t, dir, "check-ignore", "-q", paths.ProjectDirName()+"/state.yaml")
	}
}

func TestD47RepoExcludeSerializesAcrossProcesses(t *testing.T) {
	repo := newGitRepoWithWorktrees(t, "task-one", "task-two")
	exclude := filepath.Join(repo.root, ".git", "info", "exclude")
	before := readFileString(t, exclude)
	err := filelock.New(exclude).WithLock(func() error {
		cmd := exec.Command(os.Args[0], "-test.run=^TestD47RepoExcludeLockHelper$")
		cmd.Env = append(os.Environ(), "D47_EXCLUDE_ROOT="+repo.worktrees["task-one"])
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child must time out on the parent's common exclude lock: %v\n%s", err, output)
		}
		if got := readFileString(t, exclude); got != before {
			t.Fatalf("exclude changed while parent held lock: %q", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, worktree := range repo.worktrees {
		if err := EnsureRepoExclude(worktree, "/"+name+"/"); err != nil {
			t.Fatal(err)
		}
	}
	for name := range repo.worktrees {
		assertLineCount(t, readFileString(t, exclude), "/"+name+"/", 1)
	}
}

func TestD47RepoExcludeLockHelper(t *testing.T) {
	root := os.Getenv("D47_EXCLUDE_ROOT")
	if root == "" {
		return
	}
	err := EnsureRepoExclude(root, "/child/")
	if !filelock.IsLockErrorType(err, filelock.LockErrorTimeout) {
		t.Fatalf("EnsureRepoExclude error = %v, want common exclude lock timeout", err)
	}
}

func TestEnsureRepoExcludeRejectsInvalidPatterns(t *testing.T) {
	repo := newGitRepoWithWorktrees(t)
	repoExclude := filepath.Join(repo.root, ".git", "info", "exclude")
	before := readFileString(t, repoExclude)

	for _, pattern := range []string{"", "  ", "/a/\n/b/"} {
		if err := EnsureRepoExclude(repo.root, pattern); err == nil {
			t.Fatalf("EnsureRepoExclude(%q) error = nil, want rejection", pattern)
		}
	}
	if got := readFileString(t, repoExclude); got != before {
		t.Fatalf("repo exclude = %q, want unchanged %q", got, before)
	}
}

type gitRepoFixture struct {
	root      string
	worktrees map[string]string
}

func newGitRepoWithWorktrees(t *testing.T, names ...string) gitRepoFixture {
	t.Helper()

	parent := t.TempDir()
	repo := filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", repo, err)
	}
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "liza@example.invalid")
	gitRun(t, repo, "config", "user.name", "Liza Test")
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.test/repo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(main.go) error = %v", err)
	}
	gitRun(t, repo, "add", "go.mod", "main.go")
	gitRun(t, repo, "commit", "-m", "initial")
	gitRun(t, repo, "branch", "-M", "main")

	fixture := gitRepoFixture{root: repo, worktrees: make(map[string]string, len(names))}
	for _, name := range names {
		worktree := filepath.Join(parent, name)
		gitRun(t, repo, "worktree", "add", "-b", name, worktree, "main")
		fixture.worktrees[name] = worktree
	}
	return fixture
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v", strings.Join(args, " "), dir, err)
	}
	return strings.TrimSpace(string(output))
}

func revParseGitDir(t *testing.T, worktree string) string {
	t.Helper()

	gitDir := gitOutput(t, worktree, "rev-parse", "--git-dir")
	if filepath.IsAbs(gitDir) {
		return gitDir
	}
	return filepath.Clean(filepath.Join(worktree, gitDir))
}

func readFileString(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return string(content)
}

func assertLineCount(t *testing.T, content, entry string, want int) {
	t.Helper()

	var count int
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == entry {
			count++
		}
	}
	if count != want {
		t.Fatalf("%q appears %d times, want %d; content: %q", entry, count, want, content)
	}
}

func assertConflictPreserved(t *testing.T, err error, privateExclude, conflictingExclude string) {
	t.Helper()

	if err == nil {
		t.Fatal("EnsurePrivateExclude() error = nil, want conflict")
	}
	for _, want := range []string{"core.excludesFile", conflictingExclude, privateExclude} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("EnsurePrivateExclude() error = %q, want to contain %q", err, want)
		}
	}
}

func assertPrivateExcludeNotWritten(t *testing.T, privateExclude string) {
	t.Helper()

	if _, statErr := os.Stat(privateExclude); statErr == nil {
		t.Fatalf("private exclude %q exists after conflict, want no write", privateExclude)
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("Stat(%q) error = %v", privateExclude, statErr)
	}
}

func assertGitConfigUnset(t *testing.T, dir, key string) {
	t.Helper()

	cmd := exec.Command("git", "config", "--get", key)
	cmd.Dir = dir
	output, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		if strings.TrimSpace(string(output)) != "" {
			t.Fatalf("git config --get %s output = %q, want empty", key, output)
		}
		return
	}
	if err != nil {
		t.Fatalf("git config --get %s failed: %v", key, err)
	}
	t.Fatalf("git config --get %s = %q, want unset", key, strings.TrimSpace(string(output)))
}
