package oracle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	buildTimeout = 5 * time.Minute
	runTimeout   = 2 * time.Minute
)

// runRoot is the per-test-binary temporary root owned by RunMain.
var runRoot string

// RunMain is the body of an oracle package's TestMain: it creates the
// per-binary temporary root that holds the built CLIs, runs the tests, removes
// the root and exits.
func RunMain(m *testing.M) {
	root, err := os.MkdirTemp("", "oracle-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "oracle: create temporary root:", err)
		os.Exit(2)
	}
	runRoot = root
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

type build struct {
	once  sync.Once
	seq   int
	bin   string
	err   error
	count int
}

var (
	buildsMu sync.Mutex
	builds   = map[string]*build{}
)

// BuildCLI builds the main package mainPkg (a path such as ./cmd/liza,
// relative to the module root) from the tree under test, once per test binary,
// into the temporary root, and returns the binary path.
func BuildCLI(t testing.TB, mainPkg string) string {
	t.Helper()
	if runRoot == "" {
		t.Fatal("oracle: BuildCLI needs RunMain in the package's TestMain")
	}
	buildsMu.Lock()
	b, ok := builds[mainPkg]
	if !ok {
		b = &build{seq: len(builds)}
		builds[mainPkg] = b
	}
	buildsMu.Unlock()
	b.once.Do(func() {
		b.count++
		b.bin, b.err = buildMain(mainPkg, b.seq)
	})
	if b.err != nil {
		t.Fatal(b.err)
	}
	return b.bin
}

func buildMain(mainPkg string, seq int) (string, error) {
	modRoot, err := moduleRoot()
	if err != nil {
		return "", err
	}
	bin := filepath.Join(runRoot, fmt.Sprintf("cli%d", seq))
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = runRoot
	}
	env, err := ChildEnv(EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: runRoot, Go: true})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", bin, mainPkg)
	cmd.Dir = modRoot
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("oracle: build %s failed: %v\n%s", mainPkg, err, boundedTail(out.String()))
	}
	return bin, nil
}

// moduleRoot walks up from the working directory (the package under test) to
// the directory holding go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("oracle: no go.mod above the package directory")
		}
		dir = parent
	}
}

func boundedTail(s string) string {
	const limit = 4000
	if len(s) > limit {
		return "..." + s[len(s)-limit:]
	}
	return s
}

// RunOptions tunes Run and RunInProject; the zero value runs with no input and
// no extra environment.
type RunOptions struct {
	// Env holds extra NAME=value pairs appended to the policy environment.
	Env []string
	// Stdin is the child's standard input.
	Stdin []byte
}

// Result is one CLI run in a disposable project directory.
type Result struct {
	Stdout, Stderr []byte
	ExitCode       int
	// Project, Home and Tmp are the disposable directories of the run.
	Project, Home, Tmp string
	// Norm maps those directories to <PROJECT>, <HOME> and <TMP>.
	Norm *Normaliser
}

// Run executes bin with args in a fresh disposable project directory under an
// environment built only from ChildEnv: the disposable home and temporary
// directories, the host PATH and opts.Env. The host environment is never
// inherited.
func Run(t testing.TB, bin string, args []string, opts RunOptions) Result {
	t.Helper()
	root := t.TempDir()
	res := Result{
		Project: filepath.Join(root, "project"),
		Home:    filepath.Join(root, "home"),
		Tmp:     filepath.Join(root, "tmp"),
	}
	for _, d := range []string{res.Project, res.Home, res.Tmp} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("oracle: create disposable directory %s: %v", filepath.Base(d), err)
		}
	}
	return executeCLI(t, bin, args, res.Project, EnvBase{Home: res.Home, Path: os.Getenv("PATH"), Tmp: res.Tmp}, opts)
}

// RunInProject executes one invocation in a caller-prepared disposable project.
// The caller owns creation, contents, sequencing and cleanup of project,
// base.Home and base.Tmp. All three must be existing absolute directories;
// this helper never creates, clears or replaces them. ChildEnv uses base and
// opts.Env explicitly, without inheriting the host environment.
func RunInProject(t testing.TB, bin string, args []string, project string, base EnvBase, opts RunOptions) Result {
	t.Helper()
	for _, dir := range []struct{ name, path string }{
		{"project", project}, {"HOME", base.Home}, {"TMPDIR", base.Tmp},
	} {
		if !filepath.IsAbs(dir.path) {
			t.Fatalf("oracle: %s must be an existing absolute directory", dir.name)
		}
		info, err := os.Stat(dir.path)
		if err != nil || !info.IsDir() {
			t.Fatalf("oracle: %s must be an existing absolute directory", dir.name)
		}
	}
	return executeCLI(t, bin, args, project, base, opts)
}

// executeCLI is shared so both runners retain the same environment, timeout,
// output, exit-code and normaliser behavior.
func executeCLI(t testing.TB, bin string, args []string, project string, base EnvBase, opts RunOptions) Result {
	t.Helper()
	res := Result{Project: project, Home: base.Home, Tmp: base.Tmp}
	env, err := ChildEnv(base, opts.Env...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = res.Project
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(opts.Stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		t.Fatalf("oracle: run %s %s failed to start: %v", filepath.Base(bin), strings.Join(args, " "), runErr)
	}
	res.Stdout, res.Stderr = stdout.Bytes(), stderr.Bytes()
	res.Norm = NewNormaliser()
	res.Norm.Dir(res.Project, TokenProject)
	res.Norm.Dir(res.Home, TokenHome)
	res.Norm.Dir(res.Tmp, TokenTmp)
	return res
}
