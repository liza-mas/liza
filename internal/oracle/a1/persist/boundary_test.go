package persist

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOraclePersistenceAdapterBoundary(t *testing.T) {
	t.Parallel()
	const module = "github.com/liza-mas/liza/"
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			t.Errorf("non-test Go file %s", path)
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		// Match future oraclestart twins too: legal call-site-only adapter
		// splits must not require weakening this oracle's boundary assertion.
		isAdapter := strings.HasPrefix(filepath.Base(path), "adapter_")
		productionAliases := map[string]bool{}
		for _, imported := range file.Imports {
			value, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if value == module+"internal/testhelpers/oracle" || !strings.Contains(strings.Split(value, "/")[0], ".") {
				continue
			}
			if !isAdapter {
				t.Errorf("production import outside allocated adapter: %s: %s", path, value)
			}
			alias := filepath.Base(value)
			if imported.Name != nil {
				alias = imported.Name.Name
			}
			productionAliases[alias] = true
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fn.Type, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if ok {
					alias, ok := selector.X.(*ast.Ident)
					if ok && productionAliases[alias.Name] {
						t.Errorf("production type crosses adapter signature: %s %s", path, fn.Name.Name)
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
