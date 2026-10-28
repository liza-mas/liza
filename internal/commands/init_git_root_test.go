package commands

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/gitenv"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// These tests pin C4: the init branch Git subprocesses run through gitenv's
// timed runners in the discovered project root. They change the process
// directory, PATH and gitenv package variables, so none runs in parallel.

// c4InitRepo returns a disposable repository with a committed spec and
// pre-commit config, no integration branch, and a fake global install.
func c4InitRepo(t *testing.T) string {
	t.Helper()
	root := setupGitRepo(t)
	setupGlobalLiza(t)
	testhelpers.CreateCommittedSpecFile(t, root, "vision.md", "# Vision\n")
	return root
}

// c4LinkedWorktree adds a linked worktree of root on its own branch.
func c4LinkedWorktree(t *testing.T, root string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	linked := filepath.Join(parent, "linked")
	testhelpers.MustGit(t, root, "worktree", "add", "-b", "linked-branch", linked)
	return linked
}

func c4Init(t *testing.T, root, branch string) error {
	t.Helper()
	return InitCommandWithConfig(InitParams{
		Description: "C4 goal",
		SpecRef:     filepath.Join(root, "specs", "vision.md"),
		Branch:      branch,
	})
}

// c4RefExists reports whether ref resolves in dir.
func c4RefExists(t *testing.T, dir, ref string) bool {
	t.Helper()
	_, err := gitenv.Output(dir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

func TestInitGitTargetsProjectRoot_BranchCreatedAtRootHEAD(t *testing.T) {
	root := c4InitRepo(t)
	linked := c4LinkedWorktree(t, root)
	if err := os.WriteFile(filepath.Join(linked, "ahead.txt"), []byte("ahead\n"), 0644); err != nil {
		t.Fatal(err)
	}
	testhelpers.MustGit(t, linked, "add", "ahead.txt")
	testhelpers.MustGit(t, linked, "commit", "-m", "linked worktree ahead of root")
	rootHead := testhelpers.MustGit(t, root, "rev-parse", "HEAD")
	if linkedHead := testhelpers.MustGit(t, linked, "rev-parse", "HEAD"); linkedHead == rootHead {
		t.Fatal("fixture: linked worktree HEAD equals root HEAD")
	}

	t.Chdir(linked)
	if err := c4Init(t, root, "integration"); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	if got := testhelpers.MustGit(t, root, "rev-parse", "refs/heads/integration"); got != rootHead {
		t.Errorf("integration branch at %s, want the root HEAD %s", got, rootHead)
	}
}

func TestInitGitTargetsProjectRoot_ExistingRootRefDetected(t *testing.T) {
	root := c4InitRepo(t)
	linked := c4LinkedWorktree(t, root)
	const branch = "worktree/shared"
	testhelpers.MustGit(t, root, "update-ref", "refs/"+branch, "HEAD")
	if c4RefExists(t, linked, branch) {
		t.Fatal("fixture: the root's per-worktree ref is visible from the linked worktree")
	}
	if !c4RefExists(t, root, branch) {
		t.Fatal("fixture: the per-worktree ref is missing in the root")
	}

	t.Chdir(linked)
	if err := c4Init(t, root, branch); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	if c4RefExists(t, root, "refs/heads/"+branch) {
		t.Error("init created refs/heads/" + branch + " although the root already resolves the branch name")
	}
}

func TestInitGitTargetsProjectRoot_BornRootHEADFromUnbornWorktree(t *testing.T) {
	root := c4InitRepo(t)
	linked := c4LinkedWorktree(t, root)
	testhelpers.MustGit(t, linked, "checkout", "--orphan", "unborn-branch")
	if c4RefExists(t, linked, "HEAD") {
		t.Fatal("fixture: the linked worktree HEAD is born")
	}
	rootHead := testhelpers.MustGit(t, root, "rev-parse", "HEAD")

	t.Chdir(linked)
	if err := c4Init(t, root, "integration"); err != nil {
		t.Fatalf("InitCommandWithConfig() error = %v", err)
	}

	if got := testhelpers.MustGit(t, root, "rev-parse", "refs/heads/integration"); got != rootHead {
		t.Errorf("integration branch at %s, want the root HEAD %s", got, rootHead)
	}
}

// c4StubGit puts a git stub first on PATH. script runs with $REAL_GIT set to the
// real git, resolved before PATH changes; it must end by exec'ing it.
func c4StubGit(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake git is Unix-specific")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	body := "#!/bin/sh\nREAL_GIT=" + c4ShellQuote(realGit) + "\n" + script
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func c4ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func TestInitGitTargetsProjectRoot_BranchNameValidation(t *testing.T) {
	tests := []struct {
		name string
		cwd  func(t *testing.T, root string) string
	}{
		{"linked worktree", c4LinkedWorktree},
		{"subdirectory", func(t *testing.T, root string) string {
			sub := filepath.Join(root, "specs")
			if err := os.MkdirAll(sub, 0755); err != nil {
				t.Fatal(err)
			}
			return sub
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := c4InitRepo(t)
			cwd := tt.cwd(t, root)
			record := filepath.Join(t.TempDir(), "check-ref-format-dirs")
			c4StubGit(t, `if [ "$1" = check-ref-format ] && [ "$2" = --branch ]; then pwd -P >> `+c4ShellQuote(record)+`; fi
exec "$REAL_GIT" "$@"
`)

			t.Chdir(cwd)
			if err := c4Init(t, root, "integration"); err != nil {
				t.Fatalf("InitCommandWithConfig() error = %v", err)
			}

			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatalf("branch-name validation never ran git check-ref-format --branch: %v", err)
			}
			wantRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			for dir := range strings.FieldsSeq(string(data)) {
				if dir != wantRoot {
					t.Errorf("check-ref-format ran in %s, want the project root %s", dir, wantRoot)
				}
			}
		})
	}
}

// TestInitBranchValidationPrecedence_NonRepository guards the error order of a
// directory with no project root: branch name, then pipeline config, then the
// project discovery failure.
func TestInitBranchValidationPrecedence_NonRepository(t *testing.T) {
	setupGlobalLiza(t)
	t.Chdir(t.TempDir())
	badConfig := filepath.Join(t.TempDir(), "pipeline.yaml")
	if err := os.WriteFile(badConfig, []byte("not: a pipeline\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		params InitParams
		want   []string
	}{
		{"invalid name first", InitParams{Branch: "bad..name", ConfigPath: badConfig}, []string{"invalid branch name", "not a valid git branch name"}},
		{"invalid config before discovery", InitParams{Branch: "integration", ConfigPath: badConfig}, []string{"invalid pipeline config:"}},
		{"discovery failure last", InitParams{Branch: "integration"}, []string{"failed to setup paths:"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.params.Description = "C4 goal"
			tt.params.SpecRef = "specs/vision.md"
			err := InitCommandWithConfig(tt.params)
			if err == nil {
				t.Fatal("InitCommandWithConfig() succeeded outside a repository")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestInitGitTimeout_HelpersReportTimeout(t *testing.T) {
	const branch = "c4-timeout-branch"
	argvs := [][]string{
		{"check-ref-format", "--branch", branch},
		{"rev-parse", "--verify", branch},
		{"rev-parse", "--verify", "HEAD"},
		{"branch", branch, "HEAD"},
	}
	for _, argv := range argvs {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			root := c4InitRepo(t)
			record := filepath.Join(t.TempDir(), "lc-all-set")
			c4StubGit(t, `if [ "$*" = `+c4ShellQuote(strings.Join(argv, " "))+` ]; then
  if [ "$LC_ALL" = C ]; then echo true >> `+c4ShellQuote(record)+`; else echo false >> `+c4ShellQuote(record)+`; fi
  exec sleep 8
fi
exec "$REAL_GIT" "$@"
`)
			previousTimeout, previousDelay := gitenv.DefaultCommandTimeout, gitenv.DefaultCommandWaitDelay
			gitenv.DefaultCommandTimeout, gitenv.DefaultCommandWaitDelay = 3*time.Second, 50*time.Millisecond
			t.Cleanup(func() {
				gitenv.DefaultCommandTimeout, gitenv.DefaultCommandWaitDelay = previousTimeout, previousDelay
			})

			t.Chdir(root)
			err := c4Init(t, root, branch)
			if err == nil {
				t.Fatal("InitCommandWithConfig() succeeded although git hung past the timeout")
			}

			var timeout *gitenv.TimeoutError
			if !errors.As(err, &timeout) {
				t.Fatalf("error = %v, want one wrapping *gitenv.TimeoutError", err)
			}
			if !slices.Equal(timeout.Args, argv) {
				t.Errorf("timeout Args = %v, want %v", timeout.Args, argv)
			}
			if timeout.Dir != root {
				t.Errorf("timeout Dir = %s, want the project root %s", timeout.Dir, root)
			}
			for _, answer := range []string{"not a valid git branch name", "repo has no commits", "invalid branch name"} {
				if strings.Contains(err.Error(), answer) {
					t.Errorf("timeout was read as an answer: error contains %q", answer)
				}
			}
			data, readErr := os.ReadFile(record)
			if readErr != nil {
				t.Fatalf("stub never ran for %v: %v", argv, readErr)
			}
			if got := strings.Fields(string(data)); !slices.Contains(got, "true") || slices.Contains(got, "false") {
				t.Errorf("LC_ALL=C set per call = %v, want only true", got)
			}
		})
	}
}
