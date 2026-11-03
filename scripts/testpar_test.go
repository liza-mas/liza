package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var splitEntry = regexp.MustCompile(`(?m)^\t(\./[^=\s]+)=\d+$`)

// testParModule builds a disposable module holding the script's SPLIT packages,
// a root package and one unsplit package, each with a passing test, so a run
// goes through split listing, rest listing and execution. It returns the
// module directory and the SPLIT package paths.
func testParModule(t *testing.T) (string, []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test-par.sh is a bash script for Unix hosts")
	}
	script, err := os.ReadFile("test-par.sh")
	if err != nil {
		t.Fatal(err)
	}
	var split []string
	for _, m := range splitEntry.FindAllStringSubmatch(string(script), -1) {
		split = append(split, m[1])
	}
	if len(split) == 0 {
		t.Fatal("no SPLIT entries found in test-par.sh")
	}
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/testpar\n\ngo 1.21\n")
	writeFile(t, dir, "root_test.go", passingTest("testpar"))
	writeFile(t, dir, "other/ok_test.go", passingTest("other"))
	for _, p := range split {
		writeFile(t, dir, filepath.Join(p, "ok_test.go"), passingTest("split"))
	}
	return dir, split
}

func passingTest(pkg string) string {
	return "package " + pkg + "\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n"
}

func failingTaggedTest(pkg, msg string) string {
	return "//go:build e2e\n\npackage " + pkg + "\n\nimport \"testing\"\n\nfunc TestTagged(t *testing.T) { t.Fatal(\"" + msg + "\") }\n"
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTestPar(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	script, err := filepath.Abs("test-par.sh")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	// The script's own version guard is the single source of the requirement.
	if strings.Contains(string(out), "test-par: needs bash") {
		t.Skip(strings.TrimSpace(string(out)))
	}
	return string(out), err
}

// A file without a package clause makes go list ./... exit 1 with no package
// at all; the runner must fail rather than test only what remains.
func TestTestParFailsWhenDiscoveryFails(t *testing.T) {
	t.Parallel()
	dir, _ := testParModule(t)
	writeFile(t, dir, "broken/b.go", "garbage\n")
	out, err := runTestPar(t, dir)
	if err == nil || !strings.Contains(out, "broken") {
		t.Fatalf("test-par.sh: err=%v, want a failure naming the broken package; output:\n%s", err, out)
	}
}

// Tagged tests in a split package and a package made only of tagged files must
// both be discovered and run when the tags are forwarded.
func TestTestParForwardsBuildTagsToDiscovery(t *testing.T) {
	t.Parallel()
	dir, split := testParModule(t)
	writeFile(t, dir, filepath.Join(split[0], "tagged_test.go"), failingTaggedTest("split", "tagged test ran"))
	writeFile(t, dir, "taggedonly/tagged_test.go", failingTaggedTest("taggedonly", "tagged-only package ran"))
	out, err := runTestPar(t, dir, "-tags", "e2e")
	if err == nil {
		t.Fatalf("test-par.sh -tags e2e passed with failing tagged tests; output:\n%s", out)
	}
	for _, want := range []string{"tagged test ran", "tagged-only package ran"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
