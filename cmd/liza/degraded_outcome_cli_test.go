package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// exitDegraded is the exit code init and setup return when they complete
// with at least one check that is not ok (DEV-945).
const exitDegraded = 4

// degradedFixture is an isolated machine: its own HOME, a committed git
// repository, and the built command. The child environment is built
// explicitly; no host variable reaches it (G1.6).
// degradedTestCLIBuild is the default-brand CLI shared by this file's
// process-level tests; it is built once per test binary.
var degradedTestCLIBuild struct {
	sync.Once
	binary string
	output []byte
	err    error
}

func degradedTestCLI(t *testing.T) string {
	t.Helper()
	b := &degradedTestCLIBuild
	b.Do(func() {
		dir, err := os.MkdirTemp("", "degraded-outcome-cli-")
		if err != nil {
			b.err = err
			return
		}
		b.binary = filepath.Join(dir, "cli")
		if runtime.GOOS == "windows" {
			b.binary += ".exe"
		}
		b.output, b.err = exec.Command("go", "build", "-o", b.binary, ".").CombinedOutput()
	})
	if b.err != nil {
		t.Fatalf("building the command failed: %v\n%s", b.err, b.output)
	}
	return b.binary
}

type degradedFixture struct {
	bin  string
	home string
	repo string
	env  []string
}

func newDegradedFixture(t *testing.T) *degradedFixture {
	t.Helper()
	root := t.TempDir()
	f := &degradedFixture{
		bin:  degradedTestCLI(t),
		home: filepath.Join(root, "home"),
		repo: filepath.Join(root, "repo"),
	}
	for _, dir := range []string{f.home, f.repo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: f.home, Path: os.Getenv("PATH"), Tmp: root},
		brand.EnvName("SKIP_AUTO_UPDATE")+"=1",
		brand.EnvName("PROVIDER_CATALOG_URL")+"=http://127.0.0.1:1/catalog.yaml",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+filepath.Join(f.home, ".gitconfig"),
	)
	if err != nil {
		t.Fatal(err)
	}
	f.env = env
	f.git(t, "init", "-q")
	f.git(t, "-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	return f
}

func (f *degradedFixture) git(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir, cmd.Env = f.repo, f.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// run executes the command in the repository and returns stdout, stderr and
// the exit code.
func (f *degradedFixture) run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(f.bin, args...)
	cmd.Dir, cmd.Env = f.repo, f.env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s: %v", strings.Join(args, " "), err)
	}
	return stdout.String(), stderr.String(), code
}

func (f *degradedFixture) setup(t *testing.T) {
	t.Helper()
	if _, stderr, code := f.run(t, "setup", "--yes", "--claude"); code != 0 {
		t.Fatalf("setup precondition: exit %d\n%s", code, stderr)
	}
}

// occupyGlobalClaudeContract puts a user-owned file where Claude's global
// contract would be linked.
func (f *degradedFixture) occupyGlobalClaudeContract(t *testing.T) string {
	t.Helper()
	path := filepath.Join(f.home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("user instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// decodeSingleEnvelope requires stdout to be exactly one JSON document.
func decodeSingleEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var env map[string]any
	if err := dec.Decode(&env); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v\nstdout:\n%s", err, stdout)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("stdout holds more than one JSON document (second decode: %v)\nstdout:\n%s", err, stdout)
	}
	return env
}

// findCheck returns the first check in result.checks with the given id and
// provider, or nil.
func findCheck(t *testing.T, env map[string]any, id, provider string) map[string]any {
	t.Helper()
	result, _ := env["result"].(map[string]any)
	checks, _ := result["checks"].([]any)
	for _, raw := range checks {
		check, _ := raw.(map[string]any)
		if check["id"] == id && check["provider"] == provider {
			return check
		}
	}
	return nil
}

func assertDegradedCheck(t *testing.T, env map[string]any, id, provider, path string) {
	t.Helper()
	if env["ok"] != true {
		t.Fatalf("envelope ok = %v, want true: %v", env["ok"], env)
	}
	check := findCheck(t, env, id, provider)
	if check == nil {
		t.Fatalf("no %s check for provider %s in %v", id, provider, env["result"])
	}
	if check["status"] == "ok" || check["severity"] != "warn" || check["path"] != path {
		t.Fatalf("%s check = %v, want non-ok warn at %s", id, check, path)
	}
}

// TestInitOccupiedGlobalContractIsDegraded covers DEV-945's done-when case:
// init against an occupied global contract location exits with the degraded
// code and reports a non-ok check naming the provider and the path.
func TestInitOccupiedGlobalContractIsDegraded(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		f := newDegradedFixture(t)
		f.setup(t)
		global := f.occupyGlobalClaudeContract(t)
		stdout, _, code := f.run(t, "init", "--claude", "--yes", "--json")
		if code != exitDegraded {
			t.Fatalf("init --json exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
		}
		assertDegradedCheck(t, decodeSingleEnvelope(t, stdout), "contract.placement", "claude", global)
	})
	t.Run("human", func(t *testing.T) {
		f := newDegradedFixture(t)
		f.setup(t)
		f.occupyGlobalClaudeContract(t)
		_, stderr, code := f.run(t, "init", "--claude", "--yes")
		if code != exitDegraded {
			t.Fatalf("init exit = %d, want %d", code, exitDegraded)
		}
		if !strings.Contains(stderr, "retaining repo activation") {
			t.Fatalf("human warning missing from stderr:\n%s", stderr)
		}
	})
}

// TestSetupSkippedProviderLinkIsDegraded: setup that cannot link a provider
// skill because a regular file holds the path exits with the degraded code.
func TestSetupSkippedProviderLinkIsDegraded(t *testing.T) {
	occupySkillLink := func(t *testing.T, f *degradedFixture) string {
		t.Helper()
		path := filepath.Join(f.home, ".claude", "skills", "debugging")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("user file\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Run("json", func(t *testing.T) {
		f := newDegradedFixture(t)
		link := occupySkillLink(t, f)
		stdout, _, code := f.run(t, "setup", "--yes", "--claude", "--json")
		if code != exitDegraded {
			t.Fatalf("setup --json exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
		}
		assertDegradedCheck(t, decodeSingleEnvelope(t, stdout), "setup.provider-link", "claude", link)
	})
	t.Run("human", func(t *testing.T) {
		f := newDegradedFixture(t)
		occupySkillLink(t, f)
		if _, _, code := f.run(t, "setup", "--yes", "--claude"); code != exitDegraded {
			t.Fatalf("setup exit = %d, want %d", code, exitDegraded)
		}
	})
}

// TestToolchainConfigureActivationJSON: configure's project activation runs
// pairing init; in JSON mode stdout must stay one envelope, and a degraded
// activation exits with the degraded code.
func TestToolchainConfigureActivationJSON(t *testing.T) {
	configure := func(t *testing.T, f *degradedFixture) (string, int) {
		t.Helper()
		stdout, _, code := f.run(t, "toolchain", "configure", "--profile", "lean",
			"--install-dir", filepath.Join(f.home, "bin"),
			"--project", f.repo, "--agents", "claude", "--json")
		return stdout, code
	}
	t.Run("clean", func(t *testing.T) {
		f := newDegradedFixture(t)
		f.setup(t)
		stdout, code := configure(t, f)
		if code != 0 {
			t.Fatalf("configure exit = %d, want 0\nstdout:\n%s", code, stdout)
		}
		env := decodeSingleEnvelope(t, stdout)
		if env["ok"] != true {
			t.Fatalf("envelope ok = %v, want true", env["ok"])
		}
		if check := findCheck(t, env, "contract.placement", "claude"); check == nil || check["status"] != "ok" {
			t.Fatalf("contract.placement check = %v, want ok", check)
		}
	})
	t.Run("degraded", func(t *testing.T) {
		f := newDegradedFixture(t)
		f.setup(t)
		global := f.occupyGlobalClaudeContract(t)
		stdout, code := configure(t, f)
		if code != exitDegraded {
			t.Fatalf("configure exit = %d, want %d\nstdout:\n%s", code, exitDegraded, stdout)
		}
		assertDegradedCheck(t, decodeSingleEnvelope(t, stdout), "contract.placement", "claude", global)
	})
	t.Run("hard failure", func(t *testing.T) {
		f := newDegradedFixture(t)
		// No setup: pairing init cannot find the global contract.
		stdout, code := configure(t, f)
		if code != 1 {
			t.Fatalf("configure exit = %d, want 1\nstdout:\n%s", code, stdout)
		}
		if env := decodeSingleEnvelope(t, stdout); env["ok"] != false {
			t.Fatalf("envelope ok = %v, want false", env["ok"])
		}
	})
}
