package archguard

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The layers of the import-direction rules (architecture plan SI-1).
// test-support is the layer of internal/prompts/rolebudget only: no rule has
// it on either side.
const (
	layerDomain         Layer = "domain"
	layerApplication    Layer = "application"
	layerRuntime        Layer = "runtime"
	layerPrompts        Layer = "prompts"
	layerPresentation   Layer = "presentation"
	layerLeaf           Layer = "leaf"
	layerInfrastructure Layer = "infrastructure"
	layerTestSupport    Layer = "test-support"
)

var validLayers = map[Layer]bool{
	layerDomain: true, layerApplication: true, layerRuntime: true, layerPrompts: true,
	layerPresentation: true, layerLeaf: true, layerInfrastructure: true, layerTestSupport: true,
}

// Packages the layer map neither maps nor classifies as import targets:
// test support. internal/testhelpers and internal/testguard cover their
// sub-packages; internal/integration and internal/prompts/promptbench are
// single packages.
func k4Exempt(pkg string) bool {
	for _, tree := range []string{"internal/testhelpers", "internal/testguard"} {
		if pkg == tree || strings.HasPrefix(pkg, tree+"/") {
			return true
		}
	}
	return pkg == "internal/integration" || pkg == "internal/prompts/promptbench"
}

// Special packages of the rules.
const (
	k4CompositionRoot = "cmd/liza"
	k4AgentTree       = "internal/agent"
	k4JSONOut         = "internal/jsonout"
	k4DB              = "internal/db"
)

// k4Edge is a package-level import: From imports To (module-relative paths).
type k4Edge struct{ From, To string }

// k4Graph is the production import graph. Packages holds every non-exempt
// directory with a non-test Go file; Edges maps each module-internal import to
// the sorted files that make it. An edge may end in an exempt or unknown
// package; only the classification step decides what it means.
type k4Graph struct {
	Packages map[string]bool
	Edges    map[k4Edge][]string
}

// buildK4Graph reads the imports of every parsed production file, whatever its
// build constraints. Standard-library and foreign imports are ignored; blank,
// dot and aliased imports count.
func buildK4Graph(files []prodFile, module string) k4Graph {
	g := k4Graph{Packages: map[string]bool{}, Edges: map[k4Edge][]string{}}
	prefix := module + "/"
	for _, f := range files {
		pkg := path.Dir(f.Rel)
		if k4Exempt(pkg) {
			continue
		}
		g.Packages[pkg] = true
		for _, imp := range f.AST.Imports {
			ip, err := strconv.Unquote(imp.Path.Value)
			if err != nil || !strings.HasPrefix(ip, prefix) {
				continue
			}
			e := k4Edge{From: pkg, To: strings.TrimPrefix(ip, prefix)}
			if importers := g.Edges[e]; len(importers) == 0 || importers[len(importers)-1] != f.Rel {
				g.Edges[e] = append(importers, f.Rel)
			}
		}
	}
	for _, importers := range g.Edges {
		sort.Strings(importers)
	}
	return g
}

// k4Violation is one K4 guard failure. File is the area data file to change,
// empty when none is derivable.
type k4Violation struct {
	Rule    string
	File    string
	Subject string
	Msg     string
}

// Integrity rule IDs besides R1–R7.
const (
	ruleUnmapped     = "R8"
	ruleMappedTwice  = "R8-dup"
	ruleStaleLayer   = "R8-stale"
	ruleUnknownLayer = "R8-layer"
	ruleStaleAllow   = "ALLOW-stale"
	ruleBadAllow     = "ALLOW-entry"
)

func inTree(pkg, root string) bool {
	return pkg == root || strings.HasPrefix(pkg, root+"/")
}

func layerIn(l Layer, set ...Layer) bool { return slices.Contains(set, l) }

// k4Rule returns the ID of the rule that forbids the import from → to between
// mapped packages of layers lf and lt, or "" when none does. The rules have
// disjoint importer or target sets, so at most one matches.
func k4Rule(from, to string, lf, lt Layer) string {
	switch {
	case layerIn(lf, layerDomain, layerInfrastructure, layerLeaf) &&
		layerIn(lt, layerApplication, layerRuntime, layerPresentation, layerPrompts):
		return "R1"
	case lf == layerApplication && layerIn(lt, layerRuntime, layerPresentation, layerPrompts):
		return "R2"
	case lf == layerPrompts && layerIn(lt, layerApplication, layerRuntime, layerPresentation):
		return "R3"
	case lf == layerRuntime && lt == layerPresentation:
		return "R4"
	case lf == layerPresentation && from != k4CompositionRoot && inTree(to, k4AgentTree):
		return "R5"
	case from == k4JSONOut && lt == layerApplication:
		return "R6"
	case inTree(from, k4AgentTree) && to == k4DB:
		return "R7"
	}
	return ""
}

type k4Mapping struct {
	Layer Layer
	File  string
}

// checkK4 evaluates the layer rules R1–R7 over the graph against the area
// data, plus the integrity rules (R8 and the allowlist checks). It returns
// every violation, sorted by area file, rule and subject.
func checkK4(g k4Graph, areas []*Area) []k4Violation {
	var out []k4Violation
	add := func(rule, file, subject, format string, args ...any) {
		out = append(out, k4Violation{
			Rule: rule, File: file, Subject: subject,
			Msg: fmt.Sprintf("[%s] ", rule) + fmt.Sprintf(format, args...),
		})
	}

	mapped := map[string]k4Mapping{}
	for _, a := range areas {
		file := areaFile(a.ID)
		for _, le := range a.Layers {
			if first, dup := mapped[le.Package]; dup {
				add(ruleMappedTwice, file, le.Package,
					"%s is mapped twice (first in %s, again in %s): keep one layer entry", le.Package, first.File, file)
				continue
			}
			mapped[le.Package] = k4Mapping{Layer: le.Layer, File: file}
			if !validLayers[le.Layer] {
				add(ruleUnknownLayer, file, le.Package,
					"%s has unknown layer %q: use one of the SI-1 layers in %s", le.Package, le.Layer, file)
			}
			if !g.Packages[le.Package] {
				add(ruleStaleLayer, file, le.Package,
					"layer entry %s names no production package (removed, renamed or exempt): delete it from %s", le.Package, file)
			}
		}
	}
	for _, pkg := range sortedKeys(g.Packages) {
		if _, ok := mapped[pkg]; !ok {
			add(ruleUnmapped, "", pkg,
				"%s is a production package with no layer entry: add one in the data file of the area creating it (%sarea_aN_test.go)",
				pkg, areaFilePrefix)
		}
	}

	allowedIn := wellFormedAllowEntries(areas, add)

	violating := map[k4Edge]bool{}
	for _, e := range sortedEdges(g.Edges) {
		from, okFrom := mapped[e.From]
		to, okTo := mapped[e.To]
		if !okFrom || !okTo || k4Exempt(e.To) {
			continue
		}
		rule := k4Rule(e.From, e.To, from.Layer, to.Layer)
		if rule == "" {
			continue
		}
		violating[e] = true
		if _, ok := allowedIn[e]; ok {
			continue
		}
		add(rule, from.File, edgeString(e),
			"%s (%s → %s; first import in %s): remove the import or add an allowlist entry with owner and rationale in %s",
			edgeString(e), from.Layer, to.Layer, g.Edges[e][0], from.File)
	}
	for _, e := range sortedEdges(allowedIn) {
		if !violating[e] {
			file := allowedIn[e]
			add(ruleStaleAllow, file, edgeString(e),
				"allowlist entry %s matches no existing violating import (edge gone or no longer forbidden): delete it from %s",
				edgeString(e), file)
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Subject < b.Subject
	})
	return out
}

// wellFormedAllowEntries returns the data file holding the first well-formed
// entry of each edge. It reports entries with an empty field and repeated
// entries through add; those entries allow nothing.
func wellFormedAllowEntries(areas []*Area, add func(rule, file, subject, format string, args ...any)) map[k4Edge]string {
	allowed := map[k4Edge]string{}
	for _, a := range areas {
		file := areaFile(a.ID)
		for _, ae := range a.Allowlist {
			e := k4Edge{From: ae.Importer, To: ae.Imported}
			var empty []string
			for _, f := range []struct{ name, val string }{
				{"importer", ae.Importer}, {"imported", ae.Imported}, {"owner", ae.Owner}, {"rationale", ae.Rationale},
			} {
				if strings.TrimSpace(f.val) == "" {
					empty = append(empty, f.name)
				}
			}
			switch first, dup := allowed[e]; {
			case len(empty) > 0:
				add(ruleBadAllow, file, edgeString(e),
					"allowlist entry %s has empty field(s) %s: complete it or delete it in %s",
					edgeString(e), strings.Join(empty, ", "), file)
			case dup:
				add(ruleBadAllow, file, edgeString(e),
					"allowlist entry %s is repeated (first in %s): keep one entry in %s", edgeString(e), first, file)
			default:
				allowed[e] = file
			}
		}
	}
	return allowed
}

func edgeString(e k4Edge) string { return e.From + " → " + e.To }

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedEdges[V any](m map[k4Edge]V) []k4Edge {
	edges := make([]k4Edge, 0, len(m))
	for e := range m {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func TestK4ImportDirectionGuard(t *testing.T) {
	files, err := liveTree()
	if err != nil {
		t.Fatalf("parse production tree: %v", err)
	}
	module, err := modulePath(repoRoot())
	if err != nil {
		t.Fatalf("module path: %v", err)
	}
	for _, v := range checkK4(buildK4Graph(files, module), areas) {
		t.Errorf("K4 %s", v.Msg)
	}
}
