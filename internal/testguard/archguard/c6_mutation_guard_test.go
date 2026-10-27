package archguard

import (
	"fmt"
	"go/token"
	"go/types"
	"path"
	"sort"
	"strings"
	"testing"
	"time"
)

// C6 mutation guard: no production code outside internal/ops and internal/db
// mutates the Blackboard or hands it a mutation callback. It is a source check
// over go/types, not a runtime validator: reflection, unsafe and generated
// assembly are outside it.
//
// Rules, each applied to every use (call, method value, method expression,
// promoted method, plain reference) the type checker resolves:
//
//	M1  a *db.Blackboard method the classification table marks mutating
//	M2  an interface method whose name and signature equal a mutating method
//	C1  an ops or db function or method with a func-typed parameter taking
//	    *models.State, *models.Task or *models.Agent (the mutation callback)
//	K1  an exported *db.Blackboard method missing from the table, or a table
//	    entry naming no method: a new mutator cannot fall outside the guard
//	B1  a production file no configuration type-checked (unknown build tag)
//	S1  db.Blackboard or the ops callback API unresolved: never passes vacuously
//
// No baseline exception exists. Ceiling: C1 would also flag a future ops
// snapshot-predicate API taking *models.State; the trigger to refine it is the
// first such API.

// c6Kind classifies an exported *db.Blackboard method.
type c6Kind int

const (
	c6ReadOnly c6Kind = iota
	c6Mutating
)

// c6BlackboardMethods classifies every exported *db.Blackboard method. A
// mutator is a method that changes persisted state, including
// ArchiveTerminalTask, which publishes an archive object and rewrites its task
// argument without rewriting state.yaml. Everything else reads, configures the
// handle's acquisition policy, or is cache/metrics housekeeping, none of which
// is a persisted-state mutation.
var c6BlackboardMethods = map[string]c6Kind{
	"ArchiveTerminalTask": c6Mutating,
	"Modify":              c6Mutating,
	"UpdateAgent":         c6Mutating,
	"UpdateTask":          c6Mutating,
	"Write":               c6Mutating,

	"DecodeUnnormalized":   c6ReadOnly,
	"DisableMetrics":       c6ReadOnly,
	"EnableMetrics":        c6ReadOnly,
	"GetAgent":             c6ReadOnly,
	"GetMetricsRecorder":   c6ReadOnly,
	"GetStatePath":         c6ReadOnly,
	"GetTask":              c6ReadOnly,
	"InvalidateCache":      c6ReadOnly,
	"Patient":              c6ReadOnly,
	"Read":                 c6ReadOnly,
	"ReadAdmissionLimits":  c6ReadOnly,
	"ReadCached":           c6ReadOnly,
	"ReadContext":          c6ReadOnly,
	"ReadContextPatient":   c6ReadOnly,
	"ReadRaw":              c6ReadOnly,
	"ReadSnapshot":         c6ReadOnly,
	"RefreshSnapshot":      c6ReadOnly,
	"WatchForChanges":      c6ReadOnly,
	"WithLockRetryContext": c6ReadOnly,
	"WithLockTimeout":      c6ReadOnly,
}

// c6TableFile names the file K1 diagnostics point at.
const c6TableFile = "internal/testguard/archguard/c6_mutation_guard_test.go"

// c6Boundary completes every diagnostic.
const c6Boundary = "outside internal/ops and internal/db"

// c6Run loads the module at root and applies every rule. Diagnostics are
// sorted and unique; an error means the guard could not run (go list, parse or
// type-check failure), which is never a pass.
func c6Run(root string, table map[string]c6Kind) ([]string, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	loaded, err := c6Load(root, module)
	if err != nil {
		return nil, err
	}
	files, err := productionFiles(root)
	if err != nil {
		return nil, err
	}
	return c6Evaluate(loaded, table, files), nil
}

// c6Evaluate applies the rules to a loaded module.
func c6Evaluate(l *c6Loaded, table map[string]c6Kind, files []string) []string {
	found := map[string]bool{}
	add := func(line string) { found[line] = true }

	for _, plat := range l.Platforms {
		for _, what := range plat.Unresolved {
			add(fmt.Sprintf("%s: C6 S1: %s unresolved (GOOS=%s); the guard cannot prove anything", c6TableFile, what, plat.GOOS))
		}
	}
	c6CheckTable(l, table, add)
	for _, rel := range files {
		dir := path.Dir(rel)
		if c6IsExempt(dir) || l.Typed[dir][path.Base(rel)] {
			continue
		}
		add(fmt.Sprintf("%s: C6 B1: production file is not type-checked by any of %s (unknown build constraint); the guard cannot see it",
			rel, strings.Join(c6GOOS, ", ")))
	}
	for _, unit := range l.Units {
		c6CheckUses(l.Module, unit, table, add)
	}

	lines := make([]string, 0, len(found))
	for line := range found {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	return lines
}

// c6CheckTable applies K1: the table and the union of the configurations'
// exported method sets must agree.
func c6CheckTable(l *c6Loaded, table map[string]c6Kind, add func(string)) {
	methods := map[string]bool{}
	for _, plat := range l.Platforms {
		for name := range plat.Methods {
			methods[name] = true
		}
	}
	for _, name := range c6SortedKeys(methods) {
		if _, ok := table[name]; !ok {
			add(fmt.Sprintf("%s: C6 K1: exported db.Blackboard.%s has no classification; add it to c6BlackboardMethods as read-only or mutating", c6TableFile, name))
		}
	}
	for _, name := range c6SortedKeys(table) {
		if len(l.Platforms) > 0 && !methods[name] {
			add(fmt.Sprintf("%s: C6 K1: classification names db.Blackboard.%s, which is not an exported method; remove the entry", c6TableFile, name))
		}
	}
}

// c6CheckUses applies M1, M2 and C1 to every resolved use in a unit.
func c6CheckUses(module string, unit c6Unit, table map[string]c6Kind, add func(string)) {
	dbPath := c6Path(module, "internal/db")
	opsPath := c6Path(module, "internal/ops")
	modelsPath := c6Path(module, "internal/models")
	report := func(pos token.Pos, rule, symbol string) {
		p := unit.Fset.Position(pos)
		add(fmt.Sprintf("%s:%d:%d: C6 %s: %s %s", p.Filename, p.Line, p.Column, rule, symbol, c6Boundary))
	}
	for id, obj := range unit.Info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}
		recv := c6Receiver(sig)
		switch {
		case recv != nil && c6IsNamed(recv, dbPath, "Blackboard") && table[fn.Name()] == c6Mutating && c6Has(table, fn.Name()):
			report(id.Pos(), "M1", "db.Blackboard."+fn.Name())
			continue
		case recv != nil && c6IsInterface(sig) && c6MatchesMutator(unit.Platform, table, fn.Name(), sig):
			report(id.Pos(), "M2", c6Symbol(fn, recv)+" (same name and signature as a mutating db.Blackboard method)")
			continue
		}
		if c6InBoundary(fn.Pkg().Path(), opsPath, dbPath) && c6TakesStateCallback(sig, modelsPath) {
			report(id.Pos(), "C1", c6Symbol(fn, recv)+" (mutation callback)")
		}
	}
}

func c6Has(table map[string]c6Kind, name string) bool {
	_, ok := table[name]
	return ok
}

// c6Receiver returns the named type a method is declared on, or nil for a
// function, a method on an unnamed type, or a method of an anonymous interface.
func c6Receiver(sig *types.Signature) *types.Named {
	r := sig.Recv()
	if r == nil {
		return nil
	}
	t := r.Type()
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}

// c6IsInterface reports whether a method belongs to an interface type.
func c6IsInterface(sig *types.Signature) bool {
	r := sig.Recv()
	if r == nil {
		return false
	}
	_, ok := r.Type().Underlying().(*types.Interface)
	return ok
}

func c6IsNamed(n *types.Named, pkgPath, name string) bool {
	obj := n.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == pkgPath && obj.Name() == name
}

// c6MatchesMutator reports whether an interface method repeats the name and
// signature of a mutating db.Blackboard method, which would let a caller reach
// the mutator through the interface.
func c6MatchesMutator(plat *c6Platform, table map[string]c6Kind, name string, sig *types.Signature) bool {
	if table[name] != c6Mutating || !c6Has(table, name) {
		return false
	}
	bb, ok := plat.Methods[name]
	if !ok {
		return false
	}
	return types.Identical(c6WithoutReceiver(sig), c6WithoutReceiver(bb.Type().(*types.Signature)))
}

func c6WithoutReceiver(sig *types.Signature) *types.Signature {
	return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

// c6InBoundary reports whether a package is ops, db or one of their children.
func c6InBoundary(pkgPath string, owners ...string) bool {
	for _, owner := range owners {
		if pkgPath == owner || strings.HasPrefix(pkgPath, owner+"/") {
			return true
		}
	}
	return false
}

// c6TakesStateCallback reports whether a signature has a func-typed parameter
// (also as a variadic element) that itself takes *models.State, *models.Task
// or *models.Agent: the shape of a mutation callback.
func c6TakesStateCallback(sig *types.Signature, modelsPath string) bool {
	for i := 0; i < sig.Params().Len(); i++ {
		t := sig.Params().At(i).Type()
		if slice, ok := t.(*types.Slice); ok && sig.Variadic() && i == sig.Params().Len()-1 {
			t = slice.Elem()
		}
		inner, ok := t.Underlying().(*types.Signature)
		if !ok {
			continue
		}
		for j := 0; j < inner.Params().Len(); j++ {
			if c6IsModelsPointer(inner.Params().At(j).Type(), modelsPath) {
				return true
			}
		}
	}
	return false
}

func c6IsModelsPointer(t types.Type, modelsPath string) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}
	for _, name := range []string{"State", "Task", "Agent"} {
		if c6IsNamed(named, modelsPath, name) {
			return true
		}
	}
	return false
}

// c6Symbol names a function or method for a diagnostic: package.Func or
// package.Type.Method.
func c6Symbol(fn *types.Func, recv *types.Named) string {
	if recv != nil {
		return fn.Pkg().Name() + "." + recv.Obj().Name() + "." + fn.Name()
	}
	return fn.Pkg().Name() + "." + fn.Name()
}

// TestC6MutationGuard holds the C6 acceptance criterion on the live tree:
// no production code outside internal/ops and internal/db mutates the
// Blackboard or supplies it a mutation callback. It runs in `make lint`
// through lint-archguard.
func TestC6MutationGuard(t *testing.T) {
	start := time.Now()
	violations, err := c6Run(repoRoot(), c6BlackboardMethods)
	if err != nil {
		t.Fatalf("C6 mutation guard could not run: %v", err)
	}
	t.Logf("C6 mutation guard: %d violations in %s", len(violations), time.Since(start).Round(time.Millisecond))
	for _, v := range violations {
		t.Error(v)
	}
}
