package a9

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

// Decision D48 for the C6 exit-release rows (slice finding F1). The cleanup
// itself is the existing orchGuard/orchReap shape, which
// TestOracleA9OrchestratorReap and TestOracleA9OrchestratorAdoptedZombie already
// cover, adopted zombies included. These tests prove that the exit-release rows
// reach it: the rows are serial and start their supervisor only through
// exitReleaseStart, and a held provider left by a failed, cancelled or timed-out
// row does not outlive it.

var exitReleaseConsumers = []string{
	"TestOracleA9HaltRelease",
	"TestOracleA9BlockReleasedStale",
	"TestOracleA9BlockDoerStale",
}

// TestOracleA9ExitReleaseConsumersOwned requires, from the source of
// exit_release_test.go, that each consumer is serial and launches its supervisor
// through exitReleaseStart rather than a start that owns no descendants, and that
// exitReleaseStart is orchStart.
func TestOracleA9ExitReleaseConsumersOwned(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "exit_release_test.go", nil, 0)
	if err != nil {
		t.Fatal("A9 exit-release source: parse")
	}
	funcs := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			funcs[fn.Name.Name] = fn
		}
	}
	// calls returns the called names of a body: "pkg.Name" for a selector, "Name"
	// for an identifier.
	calls := func(fn *ast.FuncDecl) map[string]bool {
		found := map[string]bool{}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				switch f := call.Fun.(type) {
				case *ast.Ident:
					found[f.Name] = true
				case *ast.SelectorExpr:
					found[f.Sel.Name] = true
				}
			}
			return true
		})
		return found
	}
	for _, name := range exitReleaseConsumers {
		fn := funcs[name]
		if fn == nil {
			t.Fatalf("A9 exit-release source: %s is missing", name)
		}
		got := calls(fn)
		if got["Parallel"] {
			t.Errorf("A9 exit-release source: %s must be serial (D48)", name)
		}
		if !got["exitReleaseStart"] {
			t.Errorf("A9 exit-release source: %s must start its supervisor with exitReleaseStart", name)
		}
		for _, unowned := range []string{"start", "startNamed", "launch", "execute"} {
			if got[unowned] {
				t.Errorf("A9 exit-release source: %s starts a process with %s, which owns no descendants", name, unowned)
			}
		}
	}
	start := funcs["exitReleaseStart"]
	if start == nil || !calls(start)["orchStart"] {
		t.Error("A9 exit-release source: exitReleaseStart must use orchStart (orchGuard, own group, wait-then-reap)")
	}
}

const exitReleaseProbeVar = "ORACLE_A9_EXIT_RELEASE_PROBE"

// TestOracleA9ExitReleaseCleanup re-executes the probe, whose rows end with a
// held provider after an assertion failure, a cancelled supervisor and a missed
// deadline. The test process is the subreaper too, so a child the probe leaves
// behind is reparented to it and is found here, after the probe has exited and
// before this test's own cleanup could hide it.
func TestOracleA9ExitReleaseCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("A9: the child subreaper (decision D45) is Linux-only")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal("A9 exit-release probe: test executable")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal("A9 exit-release probe: working directory")
	}
	orchGuard(t)
	env, err := oracle.ChildEnv(oracle.EnvBase{Home: os.Getenv("HOME"), Path: os.Getenv("PATH"), Tmp: t.TempDir()}, exitReleaseProbeVar+"=1")
	if err != nil {
		t.Fatal("A9 exit-release probe: environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestOracleA9ExitReleaseCleanupProbe$", "-test.count=1", "-test.v")
	cmd.Dir, cmd.Env = wd, env
	orchDetach(cmd)
	cmd.Cancel = func() error { orchKillGroup(cmd.Process.Pid); return nil }
	cmd.WaitDelay = 10 * time.Second
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("A9 exit-release probe: the probe must fail on purpose with exit code 1\n%s", out)
	}
	text := string(out)
	for _, want := range []string{
		"A9PROBE held: failed", "A9PROBE held: cancelled", "A9PROBE adopted: cancelled", "A9PROBE held: deadline",
		"controlled failure", "agent exit deadline",
		"--- FAIL: TestOracleA9ExitReleaseCleanupProbe/failed", "--- FAIL: TestOracleA9ExitReleaseCleanupProbe/deadline",
		"--- PASS: TestOracleA9ExitReleaseCleanupProbe/cancelled",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("A9 exit-release probe: output lacks %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "A9 cleanup:") {
		t.Fatalf("A9 exit-release probe: the probe's own cleanup reported a leak\n%s", text)
	}
	if left := orchChildren(); len(left) != 0 {
		t.Fatalf("A9 exit-release probe: %d children of the probe outlived it", len(left))
	}
}

// TestOracleA9ExitReleaseCleanupProbe is the failing test of
// TestOracleA9ExitReleaseCleanup; it does nothing unless its variable is set. It
// runs the consumers' own start with the scenario of the longest-held provider
// (the stub stays held until a release file appears) and ends each row while that
// provider runs.
func TestOracleA9ExitReleaseCleanupProbe(t *testing.T) {
	if os.Getenv(exitReleaseProbeVar) == "" {
		t.Skip("probe of TestOracleA9ExitReleaseCleanup")
	}
	for _, mode := range []string{"failed", "cancelled", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			p := newScenario(t, "i05-block-doer-stale")
			a := exitReleaseStart(t, p, "agent", "coder", "task-1", "--cli", "claude", "--agent-id", "coder-1")
			p.await(t, "provider held", func() bool { return len(p.records(t)) >= 1 })
			fmt.Println("A9PROBE held: " + mode)
			switch mode {
			case "failed":
				t.Fatal("controlled failure")
			case "cancelled":
				a.cancel()
				<-a.done
				// The held provider outlives its supervisor: only the cleanup stops it.
				p.await(t, "held provider adopted", func() bool { return len(orchChildren()) > 0 })
				fmt.Println("A9PROBE adopted: cancelled")
			case "deadline":
				p.finish(t, a, 200*time.Millisecond)
			}
		})
	}
}
