package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/testhelpers/oracle"
)

func TestRegenCommandEnvironment(t *testing.T) {
	t.Setenv("ORACLE_HOST_ONLY", "fixture-host-only")
	c := commands{home: t.TempDir(), path: os.Getenv("PATH"), tmp: t.TempDir()}
	for _, tt := range []struct {
		name   string
		goTool bool
		extra  []string
	}{
		{"git", false, nil}, {"sync", true, nil}, {"go-test", true, []string{"ORACLE_WRITE=1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			name := "git"
			if tt.goTool {
				name = "go"
			}
			cmd, err := c.command(context.Background(), ".", name, tt.extra, "version")
			if err != nil {
				t.Fatal(err)
			}
			want, err := oracle.ChildEnv(oracle.EnvBase{Home: c.home, Path: c.path, Tmp: c.tmp, Go: tt.goTool}, tt.extra...)
			if err != nil {
				t.Fatal(err)
			}
			names := func(env []string) []string {
				var result []string
				for _, kv := range env {
					k, _, _ := strings.Cut(kv, "=")
					if runtime.GOOS == "windows" {
						k = strings.ToUpper(k)
					}
					result = append(result, k)
				}
				sort.Strings(result)
				return result
			}
			if !reflect.DeepEqual(names(cmd.Env), names(want)) {
				t.Fatalf("environment names: got %v, want %v", names(cmd.Env), names(want))
			}
			for _, n := range names(cmd.Env) {
				if n == "ORACLE_HOST_ONLY" {
					t.Fatal("host-only name reached child")
				}
			}
		})
	}
}

func TestRegenDiagnosticNamesAndRedaction(t *testing.T) {
	if os.Getenv("ORACLE_DIAGNOSTIC_HELPER") == "1" {
		fmt.Fprintln(os.Stderr, "--- FAIL: TestC1 "+os.Getenv("HOME"))
		os.Exit(1)
	}
	c := commands{home: t.TempDir(), path: os.Getenv("PATH"), tmp: t.TempDir()}
	_, err := c.output(context.Background(), ".", os.Args[0], nil, []string{"ORACLE_WRITE=1", "ORACLE_DIAGNOSTIC_HELPER=1"}, "-test.run=^TestRegenDiagnosticNamesAndRedaction$")
	if err == nil {
		t.Fatal("child error suppressed")
	}
	if !strings.Contains(err.Error(), "--- FAIL: TestC1") {
		t.Fatal("failing test name lost")
	}
	if strings.Contains(err.Error(), c.home) {
		t.Fatal("diagnostic contains HOME value")
	}
}

// sampleClone returns a scenario over a shared disposable clone of this
// repository, detached at its HEAD, with the working oracle and harness trees
// committed. The command under test mutates only this clone, including its
// detached worktrees; the source repository is queried read-only.
func sampleClone(t *testing.T) *scenario {
	s := newScenario(t)
	source := s.gitSourceRoot()
	clone := filepath.Join(t.TempDir(), "clone")
	s.git("clone", "--shared", "--quiet", source, clone)
	s.dir = clone
	s.git("checkout", "--quiet", "--detach", "HEAD")
	for _, name := range []string{"internal/oracle", "internal/testhelpers/oracle"} {
		dst := filepath.Join(clone, filepath.FromSlash(name))
		if err := os.RemoveAll(dst); err != nil {
			t.Fatal(err)
		}
		if err := os.CopyFS(dst, os.DirFS(filepath.Join(source, filepath.FromSlash(name)))); err != nil {
			t.Fatal(err)
		}
	}
	s.put("regen-test-marker.txt", "commit current oracle overlay")
	// The proofs exercise regeneration, not this repository's history: a fresh
	// root without the ledger, then the ledger created with that root as start,
	// so H1 compares with today's production code and the ledger stays append-only.
	ledger := s.read(ledgerPath)
	s.git("checkout", "--quiet", "--orphan", "regen-test")
	if err := os.Remove(filepath.Join(clone, filepath.FromSlash(ledgerPath))); err != nil {
		t.Fatal(err)
	}
	root := s.commit("test oracle overlay")
	s.put(ledgerPath, "start "+root+ledger[strings.Index(ledger, "\n"):])
	s.commit("test ledger")
	return s
}

func TestRegenNamesAlteredSampleGolden(t *testing.T) {
	s := sampleClone(t)
	clone := s.dir
	altered := "internal/oracle/sample/testdata/golden/json/error-envelope.json"
	p := filepath.Join(clone, filepath.FromSlash(altered))
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	if err := os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"-repo", clone, "-pkg", "./internal/oracle/sample/..."}, &out, &stderr, nil)
	if code != 1 || !strings.Contains(out.String(), "unexplained "+altered) ||
		!strings.Contains(out.String(), "ok internal/oracle/sample/testdata/golden/human/help.txt H1") {
		t.Fatalf("exit %d; stdout:\n%s\nstderr:\n%s", code, &out, &stderr)
	}
	if strings.Count(s.git("worktree", "list", "--porcelain"), "worktree ") != 1 {
		t.Fatal("real regeneration leaked worktree")
	}
}

func (s *scenario) gitSourceRoot() string {
	s.t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		s.t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			s.t.Fatal("module root not found")
		}
		wd = parent
	}
}

// TestRegenReproducesChangedSampleAssertion is the D56 real-regeneration
// proof: in one commit a production line moves, the sample driver asserts the
// new value and its golden follows, with the C1 record.
func TestRegenReproducesChangedSampleAssertion(t *testing.T) {
	s := sampleClone(t)
	pre := s.git("rev-parse", "HEAD")
	golden := "internal/oracle/sample/testdata/golden/json/error-envelope.json"
	for name, replace := range map[string][2]string{
		"internal/jsonout/classify.go":          {`message:  "resource not found"`, `message:  "resource missing"`},
		"internal/oracle/sample/sample_test.go": {"\toracle.Check(t, oracle.JSON, \"error-envelope.json\", errorEnvelope())", "\tif !bytes.Contains(errorEnvelope(), []byte(\"resource missing\")) {\n\t\tt.Fatal(\"envelope lacks the C1 message\")\n\t}\n\toracle.Check(t, oracle.JSON, \"error-envelope.json\", errorEnvelope())"},
		golden:                                  {"resource not found", "resource missing"},
	} {
		p := filepath.Join(s.dir, filepath.FromSlash(name))
		b, err := os.ReadFile(p)
		if err != nil || !strings.Contains(string(b), replace[0]) {
			t.Fatalf("edit anchor missing in %s: %v", name, err)
		}
		s.put(name, strings.Replace(string(b), replace[0], replace[1], 1))
	}
	s.put("internal/oracle/sample/sample_test.go", strings.Replace(s.read("internal/oracle/sample/sample_test.go"), "import (\n", "import (\n\t\"bytes\"\n", 1))
	s.recordPath(golden, "C1")
	c1 := s.commit("C1 changes a production message and the driver that asserts it")
	var out, stderr bytes.Buffer
	code := run(context.Background(), []string{"-repo", s.dir, "-pkg", "./internal/oracle/sample/..."}, &out, &stderr, nil)
	for _, want := range []string{
		"ok " + golden + " chain pre " + pre + " C1 " + c1 + "\n",
		"ok internal/oracle/sample/testdata/golden/human/help.txt chain pre " + pre + "\n",
		"ok internal/oracle/sample package pre " + pre + " C1 " + c1 + "\n",
	} {
		if code != 0 || !strings.Contains(out.String(), want) {
			t.Fatalf("exit %d, missing %q; stdout:\n%s\nstderr:\n%s", code, want, &out, &stderr)
		}
	}
	if strings.Count(s.git("worktree", "list", "--porcelain"), "worktree ") != 1 {
		t.Fatal("real regeneration leaked worktree")
	}
}

// TestRegenExclusionResolvesListedDirs guards the exclusion by directory: go
// list may spell a directory through a symlink (Windows CI keeps the given
// temporary path, others print the resolved one), and both spellings name the
// same package. A directory outside the tree is an error, never a package.
func TestRegenExclusionResolvesListedDirs(t *testing.T) {
	newTree := func() string {
		root := t.TempDir()
		for _, name := range []string{"sample", "other"} {
			if err := os.MkdirAll(filepath.Join(root, "internal", "oracle", name), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}
	tree := newTree()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(tree, link); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(tree)
	if err != nil {
		t.Fatal(err)
	}
	excluded := []string{"./internal/oracle/sample"}
	listing := func(root string) []byte {
		return []byte("go: warning: unused module\n" +
			filepath.Join(root, "internal", "oracle", "sample") + "\n" +
			filepath.Join(root, "internal", "oracle", "other") + "\n")
	}
	for name, root := range map[string]string{"given": link, "resolved": resolved} {
		t.Run(name, func(t *testing.T) {
			kept, err := keptPackages(link, listing(root), excluded)
			if err != nil {
				t.Fatal("keptPackages failed")
			}
			if !reflect.DeepEqual(kept, []string{"./internal/oracle/other"}) {
				t.Fatalf("kept %d packages, want only the unexcluded one", len(kept))
			}
		})
	}
	t.Run("outside", func(t *testing.T) {
		_, err := keptPackages(link, listing(newTree()), excluded)
		if err == nil || !strings.Contains(err.Error(), "listed package outside the materialised tree") {
			t.Fatal("outside directory not rejected")
		}
	})
}

func (s *scenario) read(name string) string {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(name)))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(b)
}
