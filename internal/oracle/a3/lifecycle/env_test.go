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
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

const deadPID = 99999999 // Above Linux's maximum pid_max (4194304).

func linuxOnly(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("OC3: A3 lifecycle process and timezone proof is Linux only")
	}
}

// gitInstall is how the fixture PATH directory reaches the host Git: its one
// entry, and whether that entry is a copy of the forwarding shim
// (testdata/gitshim) rather than a symlink to Git.
type gitInstall struct {
	entry string
	shim  bool
}

// gitLayout is the install for a platform. Windows launches only executables
// with a PATHEXT extension, and the D31 symlink is not launchable there (D57),
// so it gets the shim as git.exe. Linux and macOS keep the symlink named git.
func gitLayout(goos string) gitInstall {
	if goos == "windows" {
		return shimLayout(goos)
	}
	return gitInstall{entry: "git"}
}

// shimLayout is the shim install under a platform's entry name. A case forces
// it on any host to prove that the shim yields the committed goldens.
func shimLayout(goos string) gitInstall {
	entry := "git"
	if goos == "windows" {
		entry += ".exe"
	}
	return gitInstall{entry: entry, shim: true}
}

type environment struct {
	project, home, tmp, path string
	gitBin                   string // the one entry of path
	env                      []string
}

func newEnvironment(t *testing.T, caller string) environment {
	t.Helper()
	return newEnvironmentWith(t, caller, gitLayout(runtime.GOOS), "")
}

// newEnvironmentWith builds a fixture environment whose PATH directory holds
// the given Git install. A shim install copies shimBin, or the shim built once
// per test binary when shimBin is empty.
func newEnvironmentWith(t *testing.T, caller string, install gitInstall, shimBin string) environment {
	t.Helper()
	root := t.TempDir()
	r := environment{project: filepath.Join(root, "project"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), path: filepath.Join(root, "bin")}
	for _, dir := range []string{r.project, r.home, r.tmp, r.path} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatalf("create %s: %v", filepath.Base(dir), err)
		}
	}
	host, err := exec.LookPath("git")
	if err == nil {
		host, err = filepath.Abs(host)
	}
	if err != nil {
		t.Fatal("host git unavailable")
	}
	r.gitBin = filepath.Join(r.path, install.entry)
	if install.shim {
		if shimBin == "" {
			shimBin = oracle.BuildCLI(t, "./internal/oracle/a3/lifecycle/testdata/gitshim")
		}
		// The sidecar lives beside the PATH directory, which must hold git only.
		writeFile(t, filepath.Join(root, "git-host-path"), []byte(host))
		writeFile(t, r.gitBin, readFile(t, shimBin))
		if err := os.Chmod(r.gitBin, 0755); err != nil {
			t.Fatal("make git shim executable:", err)
		}
	} else if err := os.Symlink(host, r.gitBin); err != nil {
		t.Fatal("create git symlink:", err)
	}
	extras := []string{"TZ=Etc/GMT-2", "LIZA_SKIP_AUTO_UPDATE=1", "LIZA_PROVIDER_CATALOG_URL=http://catalog.invalid/", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Oracle", "GIT_AUTHOR_EMAIL=oracle@example.invalid", "GIT_AUTHOR_DATE=2020-01-01T00:00:00Z", "GIT_COMMITTER_NAME=Oracle", "GIT_COMMITTER_EMAIL=oracle@example.invalid", "GIT_COMMITTER_DATE=2020-01-01T00:00:00Z"}
	if caller != "" {
		extras = append(extras, "LIZA_AGENT_ID="+caller, "LIZA_AGENT_GENERATION=gen-1")
	}
	// TC3/TC9: intentionally asserting the default-brand CLI environment.
	if r.env, err = oracle.ChildEnv(oracle.EnvBase{Home: r.home, Path: r.path, Tmp: r.tmp}, extras...); err != nil {
		t.Fatal("fixture environment:", err)
	}
	return r
}

type commandResult struct {
	stdout, stderr string
	exit           int
}

func (r environment) command(t *testing.T, bin string, args ...string) commandResult {
	t.Helper()
	return r.commandStdin(t, bin, "", args...)
}

func (r environment) commandStdin(t *testing.T, bin, stdin string, args ...string) commandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir, cmd.Env = r.project, r.env
	cmd.Stdin = strings.NewReader(stdin)
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
