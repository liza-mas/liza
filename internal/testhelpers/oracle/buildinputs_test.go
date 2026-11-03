package oracle

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

//go:embed buildinputs.go
var buildInputsSource []byte

// TestRegisterBuildInputsInvalidatesCachedResult proves the hazard and its
// cure end to end in a fixture module: a test that registers the inputs of a
// main package it would build re-runs after that package's source changes,
// instead of replaying "(cached)". The fixture uses a copy of the real
// buildinputs.go because another module cannot import this internal package.
func TestRegisterBuildInputsInvalidatesCachedResult(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                 "module example.com/fixture\n\ngo 1.21\n",
		"harness/buildinputs.go": string(buildInputsSource),
		"harness/tail.go":        "package oracle\n\nfunc boundedTail(s string) string { return s }\n",
		"app/main.go":            "package main\n\nfunc main() { println(\"v1\") }\n",
		"check/check_test.go": `package check

import (
	"path/filepath"
	"testing"

	oracle "example.com/fixture/harness"
)

func TestRegistersApp(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oracle.RegisterBuildInputs(nil, root, "./app"); err != nil {
		t.Fatal(err)
	}
}
`,
	}
	// Go refuses to cache a result that read a file modified in the last two
	// seconds, so the fixture starts in the past.
	past := time.Now().Add(-time.Hour)
	for name, content := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, past, past); err != nil {
			t.Fatal(err)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// An explicit environment (GUARDRAILS G1.4): ChildEnv's GOFLAGS also
	// replaces CI's -count=1, which would disable the cache under test, and
	// GOWORK=off keeps a host workspace from claiming the fixture module.
	env, err := ChildEnv(EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: t.TempDir(), Go: true}, "GOWORK=off")
	if err != nil {
		t.Fatal(err)
	}
	goTest := func() string {
		t.Helper()
		cmd := exec.Command("go", "test", "./check")
		cmd.Dir = root
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go test ./check: %v\n%s", err, out)
		}
		return string(out)
	}
	warm := func() {
		t.Helper()
		goTest()
		if out := goTest(); !strings.Contains(out, "(cached)") {
			t.Fatalf("unchanged repeat was not cached, so the fixture cannot show invalidation:\n%s", out)
		}
	}
	mustRerun := func(change string) {
		t.Helper()
		if out := goTest(); strings.Contains(out, "(cached)") {
			t.Fatalf("result replayed from cache after %s:\n%s", change, out)
		}
	}
	warm()
	extra := filepath.Join(root, "app", "extra.go")
	if err := os.WriteFile(extra, []byte("package main\n\nfunc init() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(extra, past, past); err != nil { // only the directory listing changes
		t.Fatal(err)
	}
	mustRerun("a source file was added")
	warm()
	if err := os.WriteFile(filepath.Join(root, "app", "main.go"), []byte("package main\n\nfunc main() { println(\"v2\") }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRerun("a source file was edited")
}
