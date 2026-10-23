package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

const deadPID = 99999999 // Above Linux's maximum pid_max (4194304).

func linuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("OC3: A3 lifecycle process and timezone proof is Linux only")
	}
}

type environment struct {
	project, home, tmp, path string
	env                      []string
}

func newEnvironment(t *testing.T, caller string) environment {
	t.Helper()
	root := t.TempDir()
	r := environment{project: filepath.Join(root, "project"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), path: filepath.Join(root, "bin")}
	for _, dir := range []string{r.project, r.home, r.tmp, r.path} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("create %s: %v", filepath.Base(dir), err)
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal("host git unavailable")
	}
	if err := os.Symlink(git, filepath.Join(r.path, "git")); err != nil {
		t.Fatal("create git symlink:", err)
	}
	// TC3/TC9: intentionally asserting the default-brand CLI environment.
	r.env = []string{"HOME=" + r.home, "PATH=" + r.path, "TMPDIR=" + r.tmp, "TZ=Etc/GMT-2", "LIZA_SKIP_AUTO_UPDATE=1", "LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid", "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z"}
	if caller != "" {
		r.env = append(r.env, "LIZA_AGENT_ID="+caller, "LIZA_AGENT_GENERATION=gen-1")
	}
	return r
}

type commandResult struct {
	stdout, stderr string
	exit           int
}

func (r environment) command(t *testing.T, bin string, args ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = r.project, r.env
	var out, errout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errout
	err := cmd.Run()
	result := commandResult{stdout: out.String(), stderr: errout.String()}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		result.exit = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("start %s: %v", filepath.Base(bin), err)
	}
	return result
}

func forceAllowed(args []string, targetDead bool) bool {
	return !slices.Contains(args, "--force") || targetDead
}
