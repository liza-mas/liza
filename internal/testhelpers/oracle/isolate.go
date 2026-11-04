package oracle

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// IsolateEnv names the top-level test that a re-executed test binary runs for
// its parent (Isolate).
const IsolateEnv = "ORACLE_ISOLATED_TEST"

// prebuiltEnv hands the parent's built CLIs to an isolated child as
// "mainPkg=path" entries joined by the path list separator, so the children
// reuse one build instead of relinking it each.
const prebuiltEnv = "ORACLE_PREBUILT_CLI"

// isolateMargin is how much of the parent's deadline the child leaves for the
// parent to report its outcome.
const isolateMargin = 10 * time.Second

// Isolate runs the calling top-level test in a child process of its own and
// reports whether the caller is the parent, which must then return at once:
//
//	if oracle.Isolate(t, "./cmd/liza") {
//		return
//	}
//
// In the child, where IsolateEnv names the test, it returns false and the test
// body runs alone in its process. Every process-wide resource the body claims
// (the child subreaper, every child of the process, the environment) is
// therefore its own, while the parent runs in parallel with other tests.
// Isolation comes from the process boundary instead of Go's serial phase. The
// child applies the parent's subtest selection (-run below the top level, and
// -skip).
//
// The parent builds mainPkgs first and hands them down. When go test records
// the parent's inputs (-test.testlogfile), the child records its own and the
// parent replays them: go test caches a result on what the parent process
// read, and a file or variable only the child read would otherwise let a stale
// "(cached)" pass replay after it changed. The child's coverage is not
// collected.
func Isolate(t *testing.T, mainPkgs ...string) bool {
	t.Helper()
	if os.Getenv(IsolateEnv) == t.Name() {
		return false
	}
	if strings.Contains(t.Name(), "/") {
		t.Fatal("oracle: Isolate needs a top-level test")
	}
	t.Parallel()
	prebuilt := make([]string, 0, len(mainPkgs))
	for _, pkg := range mainPkgs {
		prebuilt = append(prebuilt, pkg+"="+BuildCLI(t, pkg))
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("oracle: test executable: %v", err)
	}
	run, err := childRunPattern(t.Name(), flag.Lookup("test.run").Value.String())
	if err != nil {
		t.Fatalf("oracle: -test.run: %v", err)
	}
	args := []string{"-test.run=" + run, "-test.count=1", "-test.v=true"}
	if skip := flag.Lookup("test.skip").Value.String(); skip != "" {
		args = append(args, "-test.skip="+skip)
	}
	if testing.Short() {
		args = append(args, "-test.short")
	}
	inputs := ""
	if flag.Lookup("test.testlogfile").Value.String() != "" {
		inputs = filepath.Join(t.TempDir(), "testlog")
		args = append(args, "-test.testlogfile="+inputs)
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("oracle: working directory: %v", err)
	}
	ctx := context.Background()
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-isolateMargin/2))
		defer cancel()
		args = append(args, "-test.timeout="+max(time.Until(deadline)-isolateMargin, time.Second).String())
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Env = append(os.Environ(), IsolateEnv+"="+t.Name(), prebuiltEnv+"="+strings.Join(prebuilt, string(os.PathListSeparator)))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("oracle: isolated %s failed: %v\n%s", t.Name(), err, out)
	}
	if inputs != "" {
		if err := replayInputs(inputs, dir); err != nil {
			t.Fatalf("oracle: isolated %s inputs: %v", t.Name(), err)
		}
	}
	switch {
	case bytes.Contains(out, []byte("--- SKIP: "+t.Name()+" ")):
		t.Skipf("oracle: isolated %s skipped:\n%s", t.Name(), boundedTail(string(out)))
	case !bytes.Contains(out, []byte("--- PASS: "+t.Name()+" ")):
		t.Fatalf("oracle: isolated %s reported no result:\n%s", t.Name(), out)
	case testing.Verbose():
		t.Logf("oracle: isolated %s output:\n%s", t.Name(), out)
	}
	return true
}

// childRunPattern narrows the parent's -test.run pattern to the top-level test
// name, keeping the subtest levels of every alternative that selected it, so
// the child runs the same subtests of that test and no other test.
func childRunPattern(name, pattern string) (string, error) {
	top := "^" + regexp.QuoteMeta(name) + "$"
	var kept []string
	for _, alt := range splitRunPattern(pattern) {
		ok, err := regexp.MatchString(alt[0], name)
		if err != nil {
			return "", err
		}
		switch {
		case !ok:
		case len(alt) == 1:
			return top, nil // this alternative selects every subtest
		default:
			kept = append(kept, top+"/"+strings.Join(alt[1:], "/"))
		}
	}
	if len(kept) == 0 {
		return top, nil
	}
	return strings.Join(kept, "|"), nil
}

// splitRunPattern splits a -test.run pattern as go test does (testing's
// splitRegexp): into "|" alternatives, each a list of "/" levels, splitting
// only outside brackets and parentheses and never on an escaped character.
func splitRunPattern(s string) [][]string {
	var alts [][]string
	var levels []string
	brackets, parens := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[':
			brackets++
		case ']':
			brackets = max(brackets-1, 0) // an unmatched ']' is legal
		case '(':
			if brackets == 0 {
				parens++
			}
		case ')':
			if brackets == 0 {
				parens--
			}
		case '\\':
			i++
		case '/', '|':
			if brackets == 0 && parens == 0 {
				levels = append(levels, s[:i])
				if s[i] == '|' {
					alts, levels = append(alts, levels), nil
				}
				s, i = s[i+1:], -1
			}
		}
	}
	return append(alts, append(levels, s))
}

// prebuiltCLI returns the path an isolating parent built for mainPkg.
func prebuiltCLI(mainPkg string) (string, bool) {
	for _, entry := range filepath.SplitList(os.Getenv(prebuiltEnv)) {
		if pkg, path, ok := strings.Cut(entry, "="); ok && pkg == mainPkg {
			return path, true
		}
	}
	return "", false
}

// replayInputs repeats in this process every input a child test process
// recorded in its test log (testing's -test.testlogfile: "getenv", "open",
// "stat" and "chdir" lines), so go test records them for this process's
// result. Relative paths resolve against dir, the child's starting directory,
// as later updated by its recorded chdirs (always absolute); the child's own
// directory changes stay out of this process.
func replayInputs(path, dir string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		op, name, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		if op == "chdir" {
			dir = name
			continue
		}
		if (op == "open" || op == "stat") && !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		if seen[op+" "+name] {
			continue
		}
		seen[op+" "+name] = true
		switch op {
		case "getenv":
			os.Getenv(name)
		case "stat":
			_, _ = os.Stat(name)
		case "open":
			if f, err := os.Open(name); err == nil {
				_ = f.Close()
			}
		}
	}
	return nil
}
