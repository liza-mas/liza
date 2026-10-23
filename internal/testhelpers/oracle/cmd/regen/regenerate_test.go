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

func TestRegenNamesAlteredSampleGolden(t *testing.T) {
	// The command under test mutates only this shared clone, including its
	// detached worktrees; the source repository is queried read-only.
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
	s.commit("test oracle overlay")
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
