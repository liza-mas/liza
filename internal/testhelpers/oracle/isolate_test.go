package oracle

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const isolateProbeEnv = "ORACLE_ISOLATE_PROBE"

// The body runs only in the child: the parent returns before it, and only the
// child's environment names the test.
func TestIsolateRunsBodyInChildProcess(t *testing.T) {
	if Isolate(t) {
		return
	}
	if os.Getenv(IsolateEnv) != t.Name() {
		t.Fatal("isolated body ran outside its child process")
	}
}

// TestIsolateProbe is driven by TestIsolatePropagatesOutcome; alone it passes.
func TestIsolateProbe(t *testing.T) {
	if Isolate(t) {
		return
	}
	switch os.Getenv(isolateProbeEnv) {
	case "fail":
		t.Fatal("probe: controlled failure")
	case "skip":
		t.Skip("probe: controlled skip")
	}
}

// A child's failure fails the parent, and a child's skip skips it, each with
// the child's output.
func TestIsolatePropagatesOutcome(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		mode, want string
		code       int
	}{
		{"fail", "oracle: isolated TestIsolateProbe failed", 1},
		{"skip", "oracle: isolated TestIsolateProbe skipped", 0},
		{"pass", "--- PASS: TestIsolateProbe", 0},
	} {
		t.Run(c.mode, func(t *testing.T) {
			cmd := exec.Command(exe, "-test.run=^TestIsolateProbe$", "-test.count=1", "-test.v=true")
			cmd.Env = append(os.Environ(), isolateProbeEnv+"="+c.mode)
			out, err := cmd.CombinedOutput()
			code := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != c.code || !strings.Contains(string(out), c.want) {
				t.Fatalf("exit %d, want %d with %q:\n%s", code, c.code, c.want, out)
			}
			if c.mode != "pass" && !strings.Contains(string(out), "probe: controlled "+c.mode) {
				t.Fatalf("parent did not carry the child's output:\n%s", out)
			}
		})
	}
}

// TestIsolateSubtestProbe is driven by TestIsolateForwardsSubtestFilter.
func TestIsolateSubtestProbe(t *testing.T) {
	if Isolate(t) {
		return
	}
	t.Run("a", func(*testing.T) {})
	t.Run("b", func(*testing.T) {})
}

// The child runs only the subtests that the parent's -run and -skip select.
func TestIsolateForwardsSubtestFilter(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, run, skip, ran, notRan string }{
		{"run", "^TestIsolateSubtestProbe$/^a$", "", "TestIsolateSubtestProbe/a", "TestIsolateSubtestProbe/b"},
		{"skip", "^TestIsolateSubtestProbe$", "TestIsolateSubtestProbe/a", "TestIsolateSubtestProbe/b", "TestIsolateSubtestProbe/a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := exec.Command(exe, "-test.run="+c.run, "-test.skip="+c.skip, "-test.count=1", "-test.v=true").CombinedOutput()
			if err != nil {
				t.Fatalf("%v:\n%s", err, out)
			}
			if !strings.Contains(string(out), "--- PASS: "+c.ran) || strings.Contains(string(out), c.notRan) {
				t.Fatalf("child must run %s and not %s:\n%s", c.ran, c.notRan, out)
			}
		})
	}
}

func TestChildRunPattern(t *testing.T) {
	for _, c := range []struct{ pattern, want string }{
		{"", "^X$"},
		{"X", "^X$"},
		{"X/a", "^X$/a"},
		{"X/a/b", "^X$/a/b"},
		{"Y|X/a", "^X$/a"},
		{"X/a|X/b", "^X$/a|^X$/b"},
		{"X|X/a", "^X$"},
		{"(X|Z)/a", "^X$/a"},
		{"X/[a/b]", "^X$/[a/b]"},
		{`X/a\/b`, `^X$/a\/b`},
	} {
		if got, err := childRunPattern("X", c.pattern); err != nil || got != c.want {
			t.Errorf("childRunPattern(X, %q) = %q, %v; want %q", c.pattern, got, err, c.want)
		}
	}
}

func TestPrebuiltCLI(t *testing.T) {
	sep := string(os.PathListSeparator)
	t.Setenv(prebuiltEnv, "./cmd/a=/x/cli0"+sep+"./cmd/b=/x/cli1")
	if got, ok := prebuiltCLI("./cmd/b"); !ok || got != "/x/cli1" {
		t.Fatalf("prebuiltCLI(./cmd/b) = %q, %v", got, ok)
	}
	if _, ok := prebuiltCLI("./cmd/c"); ok {
		t.Fatal("an unlisted package must build")
	}
	t.Setenv(prebuiltEnv, "")
	if _, ok := prebuiltCLI("./cmd/a"); ok {
		t.Fatal("without a parent every package must build")
	}
}

// TestIsolateInputProbe reads one variable and one file in its child; it is
// driven by TestIsolateRecordsChildInputs.
func TestIsolateInputProbe(t *testing.T) {
	if Isolate(t) {
		return
	}
	_ = os.Getenv("ORACLE_ISOLATE_INPUT_PROBE")
	if _, err := os.ReadFile("isolate_test.go"); err != nil {
		t.Fatal(err)
	}
}

// What only the child read reaches the parent's test log, which go test uses
// to decide whether a cached result still holds.
func TestIsolateRecordsChildInputs(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "testlog")
	out, err := exec.Command(exe, "-test.run=^TestIsolateInputProbe$", "-test.count=1", "-test.testlogfile="+logPath).CombinedOutput()
	if err != nil {
		t.Fatalf("%v:\n%s", err, out)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"getenv ORACLE_ISOLATE_INPUT_PROBE\n", "open " + filepath.Join(wd, "isolate_test.go") + "\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("parent test log lacks %q:\n%s", want, data)
		}
	}
}
