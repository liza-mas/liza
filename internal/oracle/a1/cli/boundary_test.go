package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var inventory = []string{
	"human/analysis-report.txt", "human/payload-accepted.txt", "human/payload-list.txt", "human/payload-rejected.txt",
	"human/pipeline-rbac.txt", "human/usage-json.txt", "human/usage-table.txt", "human/usage-value.txt", "human/usage-yaml.txt",
	"human/validate-invalid.txt", "human/validate-valid.txt",
	"json/payload-accepted.json", "json/payload-list.json", "json/payload-rejected.json", "json/pipeline-rbac.json",
	"json/usage-baseline.json", "json/usage-inverted.json", "json/usage.json", "json/validate-invalid.json", "json/validate-valid.json",
	"state/pipeline-rbac.txt",
}

func TestOracleCLIBoundary(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if !strings.HasSuffix(e.Name(), "_test.go") {
			t.Errorf("production Go in oracle: %s", e.Name())
		}
		data := readFile(t, e.Name())
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "//go:build ") || strings.HasPrefix(line, "// +build ") {
				t.Errorf("build restriction in %s", e.Name())
			}
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), data, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		if f.Name.Name != "cli" {
			t.Errorf("unexpected package in %s", e.Name())
		}
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") && path != "github.com/liza-mas/liza/internal/testhelpers/oracle" {
				t.Errorf("forbidden driver import %s in %s", path, e.Name())
			}
		}
	}
	if os.Getenv("ORACLE_WRITE") == "1" {
		return
	}
	var got []string
	err = filepath.WalkDir(filepath.Join("testdata", "golden"), func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		if !e.Type().IsRegular() {
			t.Errorf("nonregular golden: %s", path)
		}
		rel, err := filepath.Rel(filepath.Join("testdata", "golden"), path)
		if err != nil {
			return err
		}
		got = append(got, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, inventory) {
		t.Fatalf("golden inventory differs: got %v; want %v", got, inventory)
	}
}
