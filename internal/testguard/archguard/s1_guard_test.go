package archguard

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// S1 thresholds (goal Success Criteria S1). An offender measures strictly
// more than its threshold.
const (
	maxFuncLines = 100
	maxFileLines = 800
)

// Violation kinds reported by checkS1.
const (
	vUnlisted       = "unlisted"
	vOverrun        = "overrun"
	vStale          = "stale"
	vNotInSnapshot  = "not-in-snapshot"
	vAboveSnapshot  = "above-snapshot"
	vDuplicate      = "duplicate"
	vInvalidS2Entry = "invalid-s2-entry"
)

// violation is one S1 guard failure.
type violation struct {
	Kind string
	Key  Key
	Msg  string
}

// threshold returns the S1 limit that applies to a key.
func threshold(k Key) int {
	if k.Func == "" {
		return maxFileLines
	}
	return maxFuncLines
}

// funcLines is the measurement of one function declaration with a body.
type funcLines struct {
	Name  string
	Lines int
}

// measureFuncs measures every function declaration with a body: declaration
// line to closing brace line, inclusive. Names follow Key.Func.
func measureFuncs(fset *token.FileSet, f *ast.File) []funcLines {
	var out []funcLines
	seen := map[string]int{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		name := funcName(fd)
		seen[name]++
		if n := seen[name]; n > 1 {
			name = fmt.Sprintf("%s#%d", name, n)
		}
		lines := fset.Position(fd.Body.Rbrace).Line - fset.Position(fd.Pos()).Line + 1
		out = append(out, funcLines{Name: name, Lines: lines})
	}
	return out
}

// funcName is the function name, or Recv.Name for a method.
func funcName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return fd.Name.Name
	}
	return receiverName(fd.Recv.List[0].Type) + "." + fd.Name.Name
}

// receiverName strips pointer, parentheses and type parameters from a
// receiver type expression.
func receiverName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.ParenExpr:
		return receiverName(t.X)
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

// measureOffenders returns every file and function strictly above its S1
// threshold, with its measured lines.
func measureOffenders(files []prodFile) map[Key]int {
	out := map[Key]int{}
	for _, pf := range files {
		if n := lineCount(pf.Src); n > maxFileLines {
			out[Key{File: pf.Rel}] = n
		}
		for _, fl := range measureFuncs(pf.Fset, pf.AST) {
			if fl.Lines > maxFuncLines {
				out[Key{File: pf.Rel, Func: fl.Name}] = fl.Lines
			}
		}
	}
	return out
}

// listing is where a key appears in area data.
type listing struct {
	area AreaID
	list string // "ratchet" or "S2"
}

func (l listing) String() string {
	return fmt.Sprintf("%s (%s %s)", areaFile(l.area), l.area, l.list)
}

// checkS1 compares the measured offenders with the snapshot and the area
// data and returns every violation, sorted. measured holds only keys above
// their threshold.
func checkS1(measured, snapshot map[Key]int, areas []*Area) []violation {
	var out []violation
	add := func(kind string, k Key, format string, args ...any) {
		out = append(out, violation{Kind: kind, Key: k, Msg: fmt.Sprintf(format, args...)})
	}

	listed := map[Key][]listing{}
	for _, a := range areas {
		for _, r := range a.Ratchet {
			listed[r.Key] = append(listed[r.Key], listing{a.ID, "ratchet"})
		}
		for _, e := range a.S2 {
			listed[e.Key] = append(listed[e.Key], listing{a.ID, "S2"})
		}
	}
	for k, ls := range listed {
		if len(ls) > 1 {
			add(vDuplicate, k, "%s (%s) is listed %d times: %s; keep one entry", k, measuredNote(k, measured), len(ls), joinListings(ls))
		}
	}

	for _, a := range areas {
		for _, r := range a.Ratchet {
			checkRatchetEntry(a, r, measured, snapshot, add)
		}
		for _, e := range a.S2 {
			checkS2Entry(a, e, measured, add)
		}
	}

	for k, n := range measured {
		if len(listed[k]) == 0 {
			add(vUnlisted, k, "%s measures %d lines, above the %d-line limit and in no ratchet or S2 list: reduce it, or add an S2 entry in the data file of the area that owns it (%sarea_aN_test.go)",
				k, n, threshold(k), areaFilePrefix)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Key != b.Key {
			if a.Key.File != b.Key.File {
				return a.Key.File < b.Key.File
			}
			return a.Key.Func < b.Key.Func
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Msg < b.Msg
	})
	return out
}

func checkRatchetEntry(a *Area, r RatchetEntry, measured, snapshot map[Key]int, add func(string, Key, string, ...any)) {
	file := areaFile(a.ID)
	k := r.Key
	snap, inSnap := snapshot[k]
	switch {
	case !inSnap:
		add(vNotInSnapshot, k, "ratchet entry %s in %s (%s) is not in the start snapshot: a new, moved or renamed offender cannot be ratchet-listed; reduce it, or add an S2 entry", k, file, measuredNote(k, measured))
	case r.Ceiling > snap:
		add(vAboveSnapshot, k, "ratchet entry %s in %s (%s) has ceiling %d above its start-snapshot value %d; lower it", k, file, measuredNote(k, measured), r.Ceiling, snap)
	}
	m, over := measured[k]
	switch {
	case !over:
		add(vStale, k, "stale ratchet entry %s in %s: no longer above the %d-line limit; remove it", k, file, threshold(k))
	case m > r.Ceiling:
		add(vOverrun, k, "%s measures %d lines, above its ceiling %d (limit %d); reduce it, ceiling is in %s", k, m, r.Ceiling, threshold(k), file)
	}
}

func checkS2Entry(a *Area, e S2Entry, measured map[Key]int, add func(string, Key, string, ...any)) {
	file := areaFile(a.ID)
	k := e.Key
	note := measuredNote(k, measured)
	switch e.Category {
	case S2Table, S2Registration, S2Generated:
	case S2HumanRuling:
		if strings.TrimSpace(e.Ruling) == "" {
			add(vInvalidS2Entry, k, "S2 entry %s in %s (%s) has category %q but no ruling reference", k, file, note, e.Category)
		}
	default:
		add(vInvalidS2Entry, k, "S2 entry %s in %s (%s) has unknown category %q", k, file, note, e.Category)
	}
	if strings.TrimSpace(e.Rationale) == "" {
		add(vInvalidS2Entry, k, "S2 entry %s in %s (%s) has no rationale", k, file, note)
	}
	if _, over := measured[k]; !over {
		add(vStale, k, "stale S2 entry %s in %s: no longer above the %d-line limit; remove it", k, file, threshold(k))
	}
}

// measuredNote states a key's measurement and S1 limit for a message. An
// absent measurement is reported as such, never as a count.
func measuredNote(k Key, measured map[Key]int) string {
	if n, ok := measured[k]; ok {
		return fmt.Sprintf("measured %d lines, limit %d", n, threshold(k))
	}
	return fmt.Sprintf("not measured above its %d-line limit", threshold(k))
}

func joinListings(ls []listing) string {
	parts := make([]string, len(ls))
	for i, l := range ls {
		parts[i] = l.String()
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// TestS1SizeGuard measures the live production tree against the area data.
func TestS1SizeGuard(t *testing.T) {
	files, err := liveTree()
	if err != nil {
		t.Fatalf("parse production tree: %v", err)
	}
	for _, v := range checkS1(measureOffenders(files), startSnapshot, areas) {
		t.Errorf("S1 %s: %s", v.Kind, v.Msg)
	}
}
