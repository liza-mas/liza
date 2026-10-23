package oracle

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const harnessImportPrefix = "github.com/liza-mas/liza/internal/testhelpers/oracle"

// stdlibOrHarness reports whether an import path is a standard-library path
// (its first element has no dot) or lies under the harness tree.
func stdlibOrHarness(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	if !strings.Contains(first, ".") {
		return true
	}
	return path == harnessImportPrefix || strings.HasPrefix(path, harnessImportPrefix+"/")
}

func TestStdlibOrHarnessClassification(t *testing.T) {
	for path, want := range map[string]bool{
		"fmt":                                   true,
		"encoding/json":                         true,
		harnessImportPrefix:                     true,
		harnessImportPrefix + "/sub":            true,
		harnessImportPrefix + "x":               false,
		"github.com/liza-mas/liza/internal/ops": false,
		"golang.org/x/sys":                      false,
	} {
		if got := stdlibOrHarness(path); got != want {
			t.Errorf("%s: got %v, want %v", path, got, want)
		}
	}
}

// TestHarnessImportsStdlibOnly keeps the harness compilable against the
// starting commit: every .go file of the harness tree, tests and fixtures
// included, imports only the standard library or the harness itself.
func TestHarnessImportsStdlibOnly(t *testing.T) {
	fset := token.NewFileSet()
	count := 0
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return err
		}
		count++
		f, perr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if perr != nil {
			t.Errorf("%s: %v", p, perr)
			return nil
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if !stdlibOrHarness(path) {
				t.Errorf("%s imports %s: not stdlib and not under the harness tree", p, path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatal("no Go file found: the walk is not rooted at the harness package")
	}
}
