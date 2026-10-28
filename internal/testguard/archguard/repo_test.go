package archguard

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// productionRoots are the top-level directories holding production Go.
var productionRoots = []string{"cmd", "internal"}

// excludedDirs are repo-relative directories that are not production code
// (test support). Directories named testdata are skipped at any depth.
var excludedDirs = []string{
	"internal/testhelpers",
	"internal/testguard",
	"internal/prompts/promptbench",
}

// repoRoot locates the repository from this file's own location.
func repoRoot() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("archguard: runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// modulePath reads the module path from root/go.mod.
func modulePath(root string) (string, error) {
	f, err := os.Open(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("open go.mod: %w", bareErr(err))
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 && fields[0] == "module" {
			return strings.Trim(fields[1], `"`), nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("read go.mod: %w", bareErr(err))
	}
	return "", errors.New("go.mod has no module line")
}

// productionFiles returns the repo-relative slash paths of every production
// Go file under root's cmd/ and internal/ trees, sorted: non-test .go files,
// outside excludedDirs and testdata. Build constraints are ignored.
func productionFiles(root string) ([]string, error) {
	var rels []string
	for _, top := range productionRoots {
		err := filepath.WalkDir(filepath.Join(root, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "testdata" || isExcludedDir(rel) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(rel, ".go") && !strings.HasSuffix(rel, "_test.go") {
				rels = append(rels, rel)
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("walk %s: %w", top, bareErr(err))
		}
	}
	sort.Strings(rels)
	return rels, nil
}

// bareErr strips the path an *fs.PathError carries, so messages name only
// repo-relative paths and never an absolute location.
func bareErr(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

func isExcludedDir(rel string) bool {
	for _, ex := range excludedDirs {
		if rel == ex {
			return true
		}
	}
	return false
}

// prodFile is a parsed production file.
type prodFile struct {
	Rel  string
	Src  []byte
	Fset *token.FileSet
	AST  *ast.File
}

// parseProduction reads and parses every production file under root. Errors
// name repo-relative paths only.
func parseProduction(root string) ([]prodFile, error) {
	rels, err := productionFiles(root)
	if err != nil {
		return nil, err
	}
	files := make([]prodFile, 0, len(rels))
	for _, rel := range rels {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", rel, bareErr(err))
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		files = append(files, prodFile{Rel: rel, Src: src, Fset: fset, AST: f})
	}
	return files, nil
}

// liveTree parses the repository's own production tree once per test binary.
var liveTree = sync.OnceValues(func() ([]prodFile, error) {
	return parseProduction(repoRoot())
})

// lineCount is the physical line count of src: newlines, plus one when the
// last byte is not a newline.
func lineCount(src []byte) int {
	n := bytes.Count(src, []byte("\n"))
	if len(src) > 0 && src[len(src)-1] != '\n' {
		n++
	}
	return n
}
