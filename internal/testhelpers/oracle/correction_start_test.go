package oracle

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const correctionStart = "96c9ad69aee983ce2b929dd681b6ba43bf920535"

func TestProviderCorrectionA8Start(t *testing.T) {
	runCorrectionAtStart(t, "^(TestRootOnlyBytes|TestRootOnlyAbsentAndEmpty|TestRootOnlyGoldenComparison)$")
}

// runCorrectionAtStart exports the immutable starting tree, overlays only the
// current harness, then requires every selected test to run and pass there.
// Other provider-correction wrappers reuse this helper with their own patterns.
func runCorrectionAtStart(t *testing.T, testPattern string) {
	t.Helper()
	repo, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join("internal", "testhelpers", "oracle")
	wanted := correctionTestNames(t, filepath.Join(repo, harness), testPattern)
	root := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal("cannot resolve HOME for starting-tree Go child")
	}
	env, err := ChildEnv(EnvBase{Home: home, Path: os.Getenv("PATH"), Tmp: root, Go: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	archive := exec.CommandContext(ctx, "git", "-C", repo, "archive", "--format=tar", correctionStart)
	archive.Env = env
	data, err := archive.Output()
	if err != nil {
		t.Fatalf("export immutable starting tree: %v", err)
	}
	tree := filepath.Join(root, "start")
	extractCorrectionStart(t, tree, data)
	target := filepath.Join(tree, harness)
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(target, os.DirFS(filepath.Join(repo, harness))); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-json", "-tags", "oraclestart", "-count=1", "-run", testPattern, "./internal/testhelpers/oracle")
	cmd.Dir, cmd.Env = tree, env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("starting-tree tests failed: %v\n%s\n%s", err, boundedTail(stdout.String()), boundedTail(stderr.String()))
	}
	decoder := json.NewDecoder(&stdout)
	ran, passed := map[string]bool{}, map[string]bool{}
	for {
		var event struct{ Action, Test string }
		if err := decoder.Decode(&event); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		switch event.Action {
		case "run":
			ran[event.Test] = true
		case "pass":
			passed[event.Test] = true
		}
	}
	for _, name := range wanted {
		if !ran[name] || !passed[name] {
			t.Errorf("starting-tree test %s must execute and pass", name)
		} else {
			t.Logf("start %s: %s PASS", correctionStart, name)
		}
	}
}

// Discover expected tests from source so zero tests or an entirely skipped
// selection cannot masquerade as a successful starting-tree proof.
func correctionTestNames(t *testing.T, harness, pattern string) []string {
	t.Helper()
	re := regexp.MustCompile(pattern)
	for _, wrapper := range []string{"TestProviderCorrectionA8Start", "TestProviderCorrectionA5Start", "TestProviderCorrectionA9Start"} {
		if re.MatchString(wrapper) {
			t.Fatal("starting-tree pattern must exclude correction wrappers")
		}
	}
	files, err := filepath.Glob(filepath.Join(harness, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Test") || fn.Name.Name == "TestMain" || !re.MatchString(fn.Name.Name) {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "TestProviderCorrection") {
				t.Fatal("starting-tree pattern selects a correction wrapper")
			}
			names = append(names, fn.Name.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("starting-tree pattern selects no tests")
	}
	return names
}

func extractCorrectionStart(t *testing.T, tree string, data []byte) {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := r.Next()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsLocal(h.Name) {
			t.Fatal("starting-tree archive contains a non-local path")
		}
		p := filepath.Join(tree, filepath.FromSlash(h.Name))
		switch h.Typeflag {
		case tar.TypeXGlobalHeader:
			// Git records the exported commit in global PAX metadata.
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				t.Fatal(err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, content, os.FileMode(h.Mode).Perm()); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unsupported starting-tree archive entry type %d", h.Typeflag)
		}
	}
}
