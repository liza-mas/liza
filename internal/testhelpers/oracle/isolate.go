package oracle

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
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
// Isolation comes from the process boundary instead of Go's serial phase.
//
// The parent builds mainPkgs first and hands them down. It also registers the
// package's testdata as test inputs and reads WriteEnv: go test caches a result
// on what its own process opens and reads, and a child's golden reads would
// otherwise let a stale "(cached)" pass replay. The child's coverage is not
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
	if err := registerTree("testdata"); err != nil {
		t.Fatal(err)
	}
	_ = os.Getenv(WriteEnv)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("oracle: test executable: %v", err)
	}
	args := []string{"-test.run=^" + regexp.QuoteMeta(t.Name()) + "$", "-test.count=1", "-test.v=true"}
	if testing.Short() {
		args = append(args, "-test.short")
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

// prebuiltCLI returns the path an isolating parent built for mainPkg.
func prebuiltCLI(mainPkg string) (string, bool) {
	for _, entry := range filepath.SplitList(os.Getenv(prebuiltEnv)) {
		if pkg, path, ok := strings.Cut(entry, "="); ok && pkg == mainPkg {
			return path, true
		}
	}
	return "", false
}

// registerTree opens every directory and file under root so that go test
// records them as inputs of the result; a missing root registers nothing.
func registerTree(root string) error {
	err := filepath.WalkDir(root, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		return f.Close()
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
