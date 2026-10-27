package archguard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// c6GOOS are the configurations the C6 guard type-checks, so a platform-specific
// file is checked by the configuration that builds it (GOARCH amd64, no cgo).
var c6GOOS = []string{"linux", "darwin", "windows"}

// c6StderrLimit bounds the go list stderr carried into a failure message.
const c6StderrLimit = 2048

// c6Listed is the subset of `go list -json` the guard reads.
type c6Listed struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
	DepOnly    bool
	ImportMap  map[string]string
	Module     *struct {
		Main      bool
		GoVersion string
	}
	Error *struct{ Err string }
}

// c6Platform is the type-checked view of one configuration: the db.Blackboard
// method set and the ops callback API the rules compare against.
type c6Platform struct {
	GOOS string
	// Methods are the exported methods of *db.Blackboard by name.
	Methods map[string]*types.Func
	// Unresolved lists what the self-check (rule S1) could not resolve.
	Unresolved []string
}

// c6Unit is one package type-checked under one configuration.
type c6Unit struct {
	Platform *c6Platform
	Fset     *token.FileSet
	Info     *types.Info
}

// c6Loaded is everything the rules read: every checked package under every
// configuration, and the production files some configuration type-checked.
type c6Loaded struct {
	Module    string
	Platforms []*c6Platform
	Units     []c6Unit
	// Typed maps a package directory (repo-relative, slash) to the base names
	// of the files at least one configuration type-checked.
	Typed map[string]map[string]bool
}

// c6Path returns the import path of a module-relative package directory.
func c6Path(module, dir string) string { return module + "/" + dir }

// c6IsExempt reports whether a repo-relative package directory is outside the
// rule: the owners of the boundary and test support.
func c6IsExempt(dir string) bool {
	for _, owner := range []string{"internal/ops", "internal/db"} {
		if dir == owner || strings.HasPrefix(dir, owner+"/") {
			return true
		}
	}
	for _, ex := range excludedDirs {
		if dir == ex || strings.HasPrefix(dir, ex+"/") {
			return true
		}
	}
	return false
}

// c6List runs go list for one configuration and decodes its package stream.
// Failures name the configuration and a bounded, root-relative stderr; they
// never carry an environment value.
func c6List(root, goos string) ([]c6Listed, error) {
	cmd := exec.Command("go", "list", "-e", "-export", "-deps", "-buildvcs=false", "-json", "./cmd/...", "./internal/...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		status := "failed to start"
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			status = fmt.Sprintf("exit status %d", exit.ExitCode())
		}
		return nil, fmt.Errorf("go list (GOOS=%s) %s: %s", goos, status, c6Sanitize(stderr.String(), root))
	}
	var pkgs []c6Listed
	dec := json.NewDecoder(&stdout)
	for dec.More() {
		var p c6Listed
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("go list (GOOS=%s) output: %w", goos, err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// c6Sanitize makes tool output safe for a failure message: paths under root
// become relative and the text is bounded.
func c6Sanitize(text, root string) string {
	text = strings.ReplaceAll(text, root, ".")
	text = strings.TrimSpace(text)
	if len(text) > c6StderrLimit {
		text = text[:c6StderrLimit] + " [truncated]"
	}
	return text
}

// c6Importer resolves a package's imports through its go list ImportMap and
// the shared export-data importer of the configuration.
type c6Importer struct {
	imp types.Importer
	m   map[string]string
}

func (i c6Importer) Import(p string) (*types.Package, error) {
	if resolved, ok := i.m[p]; ok {
		p = resolved
	}
	return i.imp.Import(p)
}

// c6Load lists and type-checks every non-exempt production package of the
// module at root under each configuration in c6GOOS.
func c6Load(root, module string) (*c6Loaded, error) {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", bareErr(err))
	}
	root = resolved
	loaded := &c6Loaded{Module: module, Typed: map[string]map[string]bool{}}
	for _, goos := range c6GOOS {
		pkgs, err := c6List(root, goos)
		if err != nil {
			return nil, err
		}
		exports := map[string]string{}
		for _, p := range pkgs {
			if p.Export != "" {
				exports[p.ImportPath] = p.Export
			}
		}
		fset := token.NewFileSet()
		imp := importer.ForCompiler(fset, "gc", func(p string) (io.ReadCloser, error) {
			file, ok := exports[p]
			if !ok {
				return nil, fmt.Errorf("no export data for %s", p)
			}
			f, err := os.Open(file)
			if err != nil {
				return nil, bareErr(err)
			}
			return f, nil
		})
		plat := &c6Platform{GOOS: goos}
		c6Resolve(plat, imp, module)
		loaded.Platforms = append(loaded.Platforms, plat)
		for _, p := range pkgs {
			if p.Module == nil || !p.Module.Main || p.DepOnly {
				continue
			}
			dir, err := filepath.Rel(root, p.Dir)
			if err != nil {
				return nil, fmt.Errorf("package %s: %w", p.ImportPath, err)
			}
			dir = filepath.ToSlash(dir)
			if c6IsExempt(dir) {
				continue
			}
			unit, err := c6TypeCheck(root, goos, fset, imp, p, dir)
			if err != nil {
				return nil, err
			}
			unit.Platform = plat
			loaded.Units = append(loaded.Units, unit)
			if loaded.Typed[dir] == nil {
				loaded.Typed[dir] = map[string]bool{}
			}
			for _, f := range p.GoFiles {
				loaded.Typed[dir][f] = true
			}
		}
	}
	return loaded, nil
}

// c6TypeCheck parses and type-checks one listed package; any parse or type error
// fails the guard, since an unchecked package would hide a write.
func c6TypeCheck(root, goos string, fset *token.FileSet, imp types.Importer, p c6Listed, dir string) (c6Unit, error) {
	if p.Error != nil {
		return c6Unit{}, fmt.Errorf("package %s (GOOS=%s): %s", dir, goos, c6Sanitize(p.Error.Err, root))
	}
	var files []*ast.File
	for _, name := range p.GoFiles {
		src, err := os.ReadFile(filepath.Join(p.Dir, name))
		if err != nil {
			return c6Unit{}, fmt.Errorf("read %s/%s: %w", dir, name, bareErr(err))
		}
		f, err := parser.ParseFile(fset, path.Join(dir, name), src, parser.SkipObjectResolution)
		if err != nil {
			return c6Unit{}, fmt.Errorf("parse %s/%s (GOOS=%s): %w", dir, name, goos, err)
		}
		files = append(files, f)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
	var first error
	conf := types.Config{
		Importer: c6Importer{imp: imp, m: p.ImportMap},
		Error: func(err error) {
			if first == nil {
				first = err
			}
		},
	}
	if p.Module.GoVersion != "" {
		conf.GoVersion = "go" + p.Module.GoVersion
	}
	_, _ = conf.Check(p.ImportPath, fset, files, info)
	if first != nil {
		return c6Unit{}, fmt.Errorf("type-check %s (GOOS=%s): %s", dir, goos, c6Sanitize(first.Error(), root))
	}
	return c6Unit{Fset: fset, Info: info}, nil
}

// c6Resolve imports db and ops under one configuration and records the
// *db.Blackboard method set. What cannot be resolved is recorded for the
// self-check instead of failing here, so the guard never passes vacuously.
func c6Resolve(plat *c6Platform, imp types.Importer, module string) {
	plat.Methods = map[string]*types.Func{}
	dbPkg, err := imp.Import(c6Path(module, "internal/db"))
	if err != nil {
		plat.Unresolved = append(plat.Unresolved, "package internal/db")
	} else if tn, ok := dbPkg.Scope().Lookup("Blackboard").(*types.TypeName); !ok {
		plat.Unresolved = append(plat.Unresolved, "type db.Blackboard")
	} else {
		set := types.NewMethodSet(types.NewPointer(tn.Type()))
		for i := 0; i < set.Len(); i++ {
			if fn, ok := set.At(i).Obj().(*types.Func); ok && fn.Exported() {
				plat.Methods[fn.Name()] = fn
			}
		}
	}
	opsPkg, err := imp.Import(c6Path(module, "internal/ops"))
	if err != nil {
		plat.Unresolved = append(plat.Unresolved, "package internal/ops")
	} else if _, ok := opsPkg.Scope().Lookup("ModifyWithAgentAuthority").(*types.Func); !ok {
		plat.Unresolved = append(plat.Unresolved, "func ops.ModifyWithAgentAuthority")
	}
}

// c6SortedKeys returns the keys of m in order.
func c6SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
