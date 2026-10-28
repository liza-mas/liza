package archguard

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const measureFixture = `package p

// Doc comments are outside the measured span.
func a() {
	x := 1
	_ = x
}

func bodiless()

func (s *Store[K]) Get() {}

func (s *Store[K, V]) Multi() {
}

func (Store) Value() {
}

func (s Store) Plain() {
}

func (s *(Store)) Paren() {}

func init() {}

func init() {
	_ = 1
}

func init() {}
`

func TestMeasureFuncs(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", measureFixture, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	got := measureFuncs(fset, f)

	want := []funcLines{
		{"a", 4},
		// bodiless is ignored
		{"Store.Get", 1},
		{"Store.Multi", 2},
		{"Store.Value", 2},
		{"Store.Plain", 2},
		{"Store.Paren", 1},
		{"init", 1},
		{"init#2", 3},
		{"init#3", 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("measureFuncs = %v, want %v", got, want)
	}
}

func TestLineCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"\n\n", 2},
	}
	for _, tc := range tests {
		if got := lineCount([]byte(tc.src)); got != tc.want {
			t.Errorf("lineCount(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

// writeTree creates files (slash paths relative to root) with a trivial body.
func writeTree(t *testing.T, root string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductionFilesAppliesExclusions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root,
		"cmd/liza/main.go",
		"internal/ops/x.go",
		"internal/ops/x_test.go",                // test file
		"internal/ops/notes.md",                 // not Go
		"internal/ops/testdata/fixture.go",      // testdata
		"internal/ops/testdata/deep/fixture.go", // testdata, nested
		"internal/ops/sub/testdata/fixture.go",  // testdata below a package
		"internal/testhelpers/h.go",             // exclusion
		"internal/testhelpers/sub/h.go",         // below an exclusion
		"internal/testguard/g.go",               // exclusion
		"internal/testguard/archguard/g.go",     // below an exclusion
		"internal/prompts/promptbench/p.go",     // exclusion
		"internal/prompts/rolebudget/r.go",      // production: not excluded
		"internal/prompts/prompts.go",           // production
		"plugin/acp/a.go",                       // outside cmd/ and internal/
		"internal/testhelpersX/keep.go",         // prefix lookalike is kept
		"internal/prompts/promptbenchmark/k.go", // prefix lookalike is kept
	)

	got, err := productionFiles(root)
	if err != nil {
		t.Fatalf("productionFiles: %v", err)
	}

	want := []string{
		"cmd/liza/main.go",
		"internal/ops/x.go",
		"internal/prompts/promptbenchmark/k.go",
		"internal/prompts/prompts.go",
		"internal/prompts/rolebudget/r.go",
		"internal/testhelpersX/keep.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("productionFiles = %v, want %v", got, want)
	}
}

func TestProductionFilesToleratesMissingRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, "internal/ops/x.go")

	got, err := productionFiles(root)
	if err != nil {
		t.Fatalf("productionFiles without cmd/: %v", err)
	}
	if want := []string{"internal/ops/x.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("productionFiles = %v, want %v", got, want)
	}
}

func TestParseProductionParsesAndNamesRelativePathOnError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, "internal/ok/ok.go")

	files, err := parseProduction(root)
	if err != nil {
		t.Fatalf("parseProduction: %v", err)
	}
	if len(files) != 1 || files[0].Rel != "internal/ok/ok.go" || files[0].AST == nil || len(files[0].Src) == 0 {
		t.Fatalf("parseProduction = %+v, want one parsed internal/ok/ok.go", files)
	}

	bad := filepath.Join(root, "internal", "bad", "bad.go")
	if err := os.MkdirAll(filepath.Dir(bad), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("package x\nfunc {"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = parseProduction(root)
	if err == nil {
		t.Fatal("parseProduction accepted a syntax error")
	}
	if !strings.Contains(err.Error(), "internal/bad/bad.go") {
		t.Errorf("error %q does not name the repo-relative file", err)
	}
	if strings.Contains(err.Error(), filepath.ToSlash(root)) || strings.Contains(err.Error(), root) {
		t.Errorf("error %q contains the absolute root", err)
	}
}

func TestMeasureOffendersAppliesStrictThresholds(t *testing.T) {
	t.Parallel()
	// funcOf renders a function declaration spanning exactly n lines.
	funcOf := func(name string, n int) string {
		return "func " + name + "() {\n" + strings.Repeat("\t_ = 1\n", n-2) + "}\n"
	}
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/a/funcat.go", "package a\n"+funcOf("atLimit", maxFuncLines))
	write("internal/a/funcover.go", "package a\n"+funcOf("overLimit", maxFuncLines+1))
	write("internal/a/fileat.go", strings.Repeat("\n", maxFileLines-1)+"package a\n") // exactly maxFileLines lines
	write("internal/a/fileover.go", strings.Repeat("\n", maxFileLines)+"package a\n")

	files, err := parseProduction(root)
	if err != nil {
		t.Fatalf("parseProduction: %v", err)
	}

	got := measureOffenders(files)

	want := map[Key]int{
		{File: "internal/a/funcover.go", Func: "overLimit"}: maxFuncLines + 1,
		{File: "internal/a/fileover.go"}:                    maxFileLines + 1,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("measureOffenders = %v, want %v", got, want)
	}
}
