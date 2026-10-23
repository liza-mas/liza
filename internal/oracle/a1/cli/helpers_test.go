package cli

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

type observation struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
}

type project struct {
	root, home, tmp, path string
	norm                  selective
}

func newProject(t *testing.T, overlays ...string) project {
	t.Helper()
	root := t.TempDir()
	p := project{root: filepath.Join(root, "project"), home: filepath.Join(root, "home"), tmp: filepath.Join(root, "tmp"), path: filepath.Join(root, "empty-path")}
	for _, d := range []string{p.root, p.home, p.tmp, p.path} {
		if err := os.Mkdir(d, 0755); err != nil {
			t.Fatal(err)
		}
	}
	copyFixture(t, filepath.Join("testdata", "fixtures", "base"), p.root)
	for _, name := range overlays {
		copyFixture(t, filepath.Join("testdata", "fixtures", name), p.root)
	}
	for _, d := range []struct{ path, token string }{{p.root, "<PROJECT>"}, {p.home, "<HOME>"}, {p.tmp, "<TMP>"}} {
		p.norm.dir(d.path, d.token)
	}
	if _, err := os.Stat(filepath.Join(p.root, ".git")); !os.IsNotExist(err) {
		t.Fatal("fixture project must have no Git repository")
	}
	return p
}

func copyFixture(t *testing.T, source, target string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(target, rel)
		if e.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		if !e.Type().IsRegular() {
			return errors.New("fixture must be a regular file")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (p project) run(t *testing.T, generation bool, args ...string) observation {
	t.Helper()
	var extra []string
	if generation {
		extra = []string{"LIZA_AGENT_GENERATION=g1"}
	}
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: p.home, Path: p.path, Tmp: p.tmp}, extra...)
	if err != nil {
		t.Fatal(err)
	}
	assertEnvironment(t, env, generation, false)
	expected := map[string]string{"HOME": p.home, "PATH": p.path, "TMPDIR": p.tmp}
	if runtime.GOOS == "windows" {
		expected["USERPROFILE"], expected["TMP"], expected["TEMP"] = p.home, p.tmp, p.tmp
	}
	for _, kv := range env {
		name, value, _ := strings.Cut(kv, "=")
		if want, ok := expected[name]; ok && value != want {
			t.Fatalf("environment variable %s does not target disposable directory", name)
		}
	}
	if entries, err := os.ReadDir(p.path); err != nil || len(entries) != 0 {
		t.Fatal("CLI PATH must be an empty directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, oracle.BuildCLI(t, "./cmd/liza"), args...)
	cmd.Dir, cmd.Env = p.root, env
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	code := 0
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatal("CLI launch failed")
	}
	if ctx.Err() != nil {
		t.Fatal("CLI run timed out")
	}
	return observation{code, out.String(), stderr.String()}
}

// Assertions mention environment names and booleans only, never host values.
func assertEnvironment(t *testing.T, env []string, generation, toolchain bool) {
	t.Helper()
	allowed := map[string]bool{"HOME": true, "PATH": true, "TMPDIR": true}
	if runtime.GOOS == "windows" {
		for _, k := range []string{"USERPROFILE", "TMP", "TEMP", "SYSTEMROOT"} {
			allowed[k] = true
		}
	}
	if generation {
		allowed["LIZA_AGENT_GENERATION"] = true
	}
	if toolchain {
		for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOFLAGS"} {
			allowed[k] = true
		}
	}
	seen := map[string]bool{}
	for _, kv := range env {
		name, _, ok := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" {
			name = strings.ToUpper(name)
		}
		if !ok || !allowed[name] || seen[name] {
			t.Fatalf("unexpected or duplicate environment variable %s", name)
		}
		seen[name] = true
	}
	for name := range allowed {
		if !seen[name] {
			t.Fatalf("missing environment variable %s", name)
		}
	}
}

func requireExit(t *testing.T, got observation, code int) {
	t.Helper()
	if got.ExitCode != code {
		t.Fatalf("CLI exit = %d, want %d; stdout=%s; stderr=%s", got.ExitCode, code, got.Stdout, got.Stderr)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
