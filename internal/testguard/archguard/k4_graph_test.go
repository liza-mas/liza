package archguard

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const k4FixtureModule = "example.test/m"

// writeSource creates a file (slash path relative to root) with the given body.
func writeSource(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// k4Source renders a Go file importing the given paths (a spec may carry an
// alias, "_" or "." before the path).
func k4Source(imports ...string) string {
	var b strings.Builder
	b.WriteString("package x\n\nimport (\n")
	for _, imp := range imports {
		alias, path, found := strings.Cut(imp, " ")
		if !found {
			alias, path = "", imp
		}
		b.WriteString("\t" + alias + " \"" + path + "\"\n")
	}
	b.WriteString(")\n")
	return b.String()
}

func buildFixtureGraph(t *testing.T, root string) k4Graph {
	t.Helper()
	files, err := parseProduction(root)
	if err != nil {
		t.Fatalf("parseProduction: %v", err)
	}
	return buildK4Graph(files, k4FixtureModule)
}

func sortedPackages(g k4Graph) []string {
	var pkgs []string
	for p := range g.Packages {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	return pkgs
}

func TestBuildK4GraphReadsEveryNonTestFileRegardlessOfBuildConstraints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/a.go", k4Source(k4FixtureModule+"/internal/b"))
	writeSource(t, root, "internal/a/a_windows.go", "//go:build windows\n\n"+k4Source(k4FixtureModule+"/internal/c"))
	writeSource(t, root, "internal/b/b.go", "package x\n")
	writeSource(t, root, "internal/c/c.go", "package x\n")

	g := buildFixtureGraph(t, root)

	if want := []string{"internal/a", "internal/b", "internal/c"}; !reflect.DeepEqual(sortedPackages(g), want) {
		t.Errorf("packages = %v, want %v", sortedPackages(g), want)
	}
	for _, e := range []k4Edge{{"internal/a", "internal/b"}, {"internal/a", "internal/c"}} {
		if _, ok := g.Edges[e]; !ok {
			t.Errorf("missing edge %s → %s (build-constrained file must count)", e.From, e.To)
		}
	}
	if got := g.Edges[k4Edge{"internal/a", "internal/c"}]; !reflect.DeepEqual(got, []string{"internal/a/a_windows.go"}) {
		t.Errorf("importing files of the windows-only edge = %v", got)
	}
}

func TestBuildK4GraphIgnoresTestImports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/a.go", "package x\n")
	writeSource(t, root, "internal/a/a_test.go", k4Source(k4FixtureModule+"/internal/b"))
	writeSource(t, root, "internal/b/b.go", "package x\n")

	g := buildFixtureGraph(t, root)

	if len(g.Edges) != 0 {
		t.Errorf("edges = %v, want none: _test.go imports are not checked", g.Edges)
	}
}

func TestBuildK4GraphClassifiesOnlyModuleInternalImports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/a.go", k4Source(
		"fmt",
		"github.com/other/dep/internal/b",
		k4FixtureModule+"x/internal/b", // module-path prefix without a separator
		k4FixtureModule,                // the module root itself is not a package edge
		k4FixtureModule+"/internal/b",
	))
	writeSource(t, root, "internal/b/b.go", "package x\n")

	g := buildFixtureGraph(t, root)

	want := map[k4Edge][]string{{"internal/a", "internal/b"}: {"internal/a/a.go"}}
	if !reflect.DeepEqual(g.Edges, want) {
		t.Errorf("edges = %v, want %v", g.Edges, want)
	}
}

func TestBuildK4GraphCountsBlankDotAndAliasedImports(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/a.go", k4Source(
		"_ "+k4FixtureModule+"/internal/b",
		". "+k4FixtureModule+"/internal/c",
		"d "+k4FixtureModule+"/internal/d",
	))
	for _, p := range []string{"b", "c", "d"} {
		writeSource(t, root, "internal/"+p+"/"+p+".go", "package x\n")
	}

	g := buildFixtureGraph(t, root)

	for _, to := range []string{"internal/b", "internal/c", "internal/d"} {
		if _, ok := g.Edges[k4Edge{"internal/a", to}]; !ok {
			t.Errorf("missing edge internal/a → %s", to)
		}
	}
}

func TestBuildK4GraphRecordsEveryImportingFileSorted(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/z.go", k4Source(k4FixtureModule+"/internal/b"))
	writeSource(t, root, "internal/a/m.go", k4Source(k4FixtureModule+"/internal/b"))
	writeSource(t, root, "internal/b/b.go", "package x\n")

	g := buildFixtureGraph(t, root)

	want := []string{"internal/a/m.go", "internal/a/z.go"}
	if got := g.Edges[k4Edge{"internal/a", "internal/b"}]; !reflect.DeepEqual(got, want) {
		t.Errorf("importing files = %v, want %v", got, want)
	}
}

func TestBuildK4GraphTestOnlyDirectoryIsNoPackage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSource(t, root, "internal/a/a.go", "package x\n")
	writeSource(t, root, "internal/onlytests/x_test.go", "package x\n")

	g := buildFixtureGraph(t, root)

	if want := []string{"internal/a"}; !reflect.DeepEqual(sortedPackages(g), want) {
		t.Errorf("packages = %v, want %v", sortedPackages(g), want)
	}
}

func TestBuildK4GraphExemptPackagesAreNoProductionPackages(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Only internal/integration reaches the graph builder: the other exempt
	// directories are already skipped by productionFiles.
	writeSource(t, root, "internal/integration/i.go", k4Source(k4FixtureModule+"/internal/a"))
	writeSource(t, root, "internal/a/a.go", k4Source(k4FixtureModule+"/internal/integration"))

	g := buildFixtureGraph(t, root)

	if want := []string{"internal/a"}; !reflect.DeepEqual(sortedPackages(g), want) {
		t.Errorf("packages = %v, want %v", sortedPackages(g), want)
	}
	if _, ok := g.Edges[k4Edge{"internal/integration", "internal/a"}]; ok {
		t.Error("an exempt package must not be an importer")
	}
	// The edge into an exempt package is kept; checkK4 does not classify it.
	if _, ok := g.Edges[k4Edge{"internal/a", "internal/integration"}]; !ok {
		t.Error("edge into an exempt package is missing from the graph")
	}
}

func TestK4Exempt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pkg  string
		want bool
	}{
		{"internal/testhelpers", true},
		{"internal/testhelpers/gitfixture", true},
		{"internal/testguard", true},
		{"internal/testguard/archguard", true},
		{"internal/integration", true},
		{"internal/prompts/promptbench", true},
		{"internal/prompts/rolebudget", false},
		{"internal/prompts", false},
		{"internal/testhelpersx", false},
		{"internal/integrationx", false},
		{"cmd/liza", false},
	}
	for _, tc := range tests {
		if got := k4Exempt(tc.pkg); got != tc.want {
			t.Errorf("k4Exempt(%q) = %v, want %v", tc.pkg, got, tc.want)
		}
	}
}
