package archguard

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// k4TestLayers is a miniature layer map covering every layer.
var k4TestLayers = map[string]Layer{
	"internal/models":             layerDomain,
	"internal/db":                 layerInfrastructure,
	"internal/git":                layerInfrastructure,
	"internal/paths":              layerLeaf,
	"internal/ops":                layerApplication,
	"internal/agent":              layerRuntime,
	"internal/agent/sub":          layerRuntime,
	"internal/process":            layerRuntime,
	"internal/prompts":            layerPrompts,
	"internal/prompts/rolebudget": layerTestSupport,
	"cmd/liza":                    layerPresentation,
	"internal/commands":           layerPresentation,
	"internal/jsonout":            layerPresentation,
	"internal/tui":                layerPresentation,
}

// k4TestAreas places the packages of k4TestLayers in area data by layer: A7
// presentation, A8 prompts and test-support, A9 runtime, A3 application, A1 the
// rest. Allowlist entries go to A9.
func k4TestAreas(allow ...AllowEntry) []*Area {
	a1, a3, a7, a8, a9 := &Area{ID: "A1"}, &Area{ID: "A3"}, &Area{ID: "A7"}, &Area{ID: "A8"}, &Area{ID: "A9"}
	pkgs := make([]string, 0, len(k4TestLayers))
	for p := range k4TestLayers {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	for _, p := range pkgs {
		l := k4TestLayers[p]
		e := LayerEntry{Package: p, Layer: l}
		switch l {
		case layerPresentation:
			a7.Layers = append(a7.Layers, e)
		case layerPrompts, layerTestSupport:
			a8.Layers = append(a8.Layers, e)
		case layerRuntime:
			a9.Layers = append(a9.Layers, e)
		case layerApplication:
			a3.Layers = append(a3.Layers, e)
		default:
			a1.Layers = append(a1.Layers, e)
		}
	}
	a9.Allowlist = allow
	return []*Area{a1, a3, a7, a8, a9}
}

// k4TestGraph builds a graph whose packages are every mapped package plus
// extra, and whose edges are "From>To" specs (importing file From/x.go).
func k4TestGraph(extra []string, edges ...string) k4Graph {
	g := k4Graph{Packages: map[string]bool{}, Edges: map[k4Edge][]string{}}
	for p := range k4TestLayers {
		g.Packages[p] = true
	}
	for _, p := range extra {
		g.Packages[p] = true
	}
	for _, spec := range edges {
		from, to, _ := strings.Cut(spec, ">")
		g.Edges[k4Edge{from, to}] = []string{from + "/x.go"}
	}
	return g
}

func allowEntry(from, to string) AllowEntry {
	return AllowEntry{Importer: from, Imported: to, Owner: "Scope 99", Rationale: "test crossing"}
}

func rulesOf(vs []k4Violation) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func TestCheckK4ForbiddenEdgePerRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rule string
		edge string
		file string // area file holding the importer's layer entry
	}{
		{"R1", "internal/models>internal/ops", "area_a1_test.go"},
		{"R1", "internal/models>internal/agent", "area_a1_test.go"},
		{"R1", "internal/models>internal/commands", "area_a1_test.go"},
		{"R1", "internal/models>internal/prompts", "area_a1_test.go"},
		{"R1", "internal/db>internal/ops", "area_a1_test.go"},
		{"R1", "internal/git>internal/agent", "area_a1_test.go"},
		{"R1", "internal/paths>internal/commands", "area_a1_test.go"},
		{"R1", "internal/paths>internal/prompts", "area_a1_test.go"},
		{"R2", "internal/ops>internal/agent", "area_a3_test.go"},
		{"R2", "internal/ops>internal/commands", "area_a3_test.go"},
		{"R2", "internal/ops>internal/prompts", "area_a3_test.go"},
		{"R3", "internal/prompts>internal/ops", "area_a8_test.go"},
		{"R3", "internal/prompts>internal/agent", "area_a8_test.go"},
		{"R3", "internal/prompts>internal/commands", "area_a8_test.go"},
		{"R4", "internal/agent>internal/commands", "area_a9_test.go"},
		{"R4", "internal/process>cmd/liza", "area_a9_test.go"},
		{"R5", "internal/commands>internal/agent", "area_a7_test.go"},
		{"R5", "internal/tui>internal/agent", "area_a7_test.go"},
		{"R5", "internal/tui>internal/agent/sub", "area_a7_test.go"},
		{"R6", "internal/jsonout>internal/ops", "area_a7_test.go"},
		{"R7", "internal/agent>internal/db", "area_a9_test.go"},
		{"R7", "internal/agent/sub>internal/db", "area_a9_test.go"},
	}
	for _, tc := range tests {
		t.Run(tc.rule+" "+tc.edge, func(t *testing.T) {
			t.Parallel()
			got := checkK4(k4TestGraph(nil, tc.edge), k4TestAreas())
			if len(got) != 1 {
				t.Fatalf("violations = %v, want exactly one", got)
			}
			v := got[0]
			if v.Rule != tc.rule {
				t.Errorf("rule = %s, want %s: %s", v.Rule, tc.rule, v.Msg)
			}
			if want := areaFilePrefix + tc.file; v.File != want {
				t.Errorf("file = %q, want %q", v.File, want)
			}
		})
	}
}

func TestCheckK4PermittedNeighbours(t *testing.T) {
	t.Parallel()
	edges := []string{
		"cmd/liza>internal/agent", // composition root may import any layer
		"cmd/liza>internal/ops",
		"cmd/liza>internal/prompts",
		"internal/commands>internal/ops", // presentation → application
		"internal/tui>internal/ops",
		"internal/commands>internal/db",      // presentation → infrastructure
		"internal/commands>internal/process", // presentation → runtime other than agent
		"internal/jsonout>internal/paths",    // jsonout → leaf
		"internal/jsonout>internal/models",
		"internal/process>internal/db", // only agent is barred from db
		"internal/process>internal/agent",
		"internal/agent>internal/ops", // runtime → application
		"internal/agent>internal/prompts",
		"internal/agent>internal/process",
		"internal/ops>internal/db", // application → infrastructure and domain
		"internal/ops>internal/git",
		"internal/ops>internal/models",
		"internal/prompts>internal/models", // prompts → domain and leaf
		"internal/prompts>internal/paths",
		"internal/db>internal/models", // infrastructure → domain, leaf, infrastructure
		"internal/db>internal/paths",
		"internal/db>internal/git",
		"internal/models>internal/paths",
		"internal/paths>internal/models",
		"internal/prompts/rolebudget>internal/ops", // test-support: no rule on either side
		"internal/agent>internal/prompts/rolebudget",
		"internal/prompts/rolebudget>internal/prompts/promptbench",
	}
	if got := checkK4(k4TestGraph(nil, edges...), k4TestAreas()); len(got) != 0 {
		t.Errorf("violations = %v, want none", got)
	}
}

func TestCheckK4EdgesWithAnExemptOrUnmappedEndAreNotClassified(t *testing.T) {
	t.Parallel()
	g := k4TestGraph(nil,
		"internal/ops>internal/integration", // exempt target
		"internal/ops>internal/testguard/x", // exempt target
		"internal/models>plugin/acp",        // not under cmd/ or internal/
	)
	if got := checkK4(g, k4TestAreas()); len(got) != 0 {
		t.Errorf("violations = %v, want none", got)
	}
}

func TestCheckK4NewForbiddenEdgeFailsWhileAllowlistedOneStaysQuiet(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas(allowEntry("internal/commands", "internal/agent"))
	edges := []string{"internal/commands>internal/agent"}
	if got := checkK4(k4TestGraph(nil, edges...), areas); len(got) != 0 {
		t.Fatalf("allowlisted edge reported: %v", got)
	}

	edges = append(edges, "internal/tui>internal/agent")
	got := checkK4(k4TestGraph(nil, edges...), areas)

	if len(got) != 1 || got[0].Rule != "R5" || !strings.Contains(got[0].Msg, "internal/tui → internal/agent") {
		t.Errorf("violations = %v, want the new R5 tui edge only", got)
	}
}

func TestCheckK4AllowlistEntryIsExactToTheEdge(t *testing.T) {
	t.Parallel()
	// The entry names commands → agent; the same rule broken by another
	// importer, or by the same importer toward another package, is not covered.
	areas := k4TestAreas(allowEntry("internal/commands", "internal/agent"))
	g := k4TestGraph(nil, "internal/commands>internal/agent/sub", "internal/tui>internal/agent")

	got := checkK4(g, areas)

	// Two uncovered R5 edges, plus the entry itself, which no longer matches.
	if want := []string{"R5", "R5", "ALLOW-stale"}; !reflect.DeepEqual(sortedCopy(rulesOf(got)), sortedCopy(want)) {
		t.Errorf("rules = %v, want %v: %v", rulesOf(got), want, got)
	}
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func TestCheckK4UnmappedProductionPackageFails(t *testing.T) {
	t.Parallel()
	got := checkK4(k4TestGraph([]string{"internal/brandnew"}), k4TestAreas())

	if len(got) != 1 || got[0].Rule != "R8" {
		t.Fatalf("violations = %v, want one R8", got)
	}
	if !strings.Contains(got[0].Msg, "internal/brandnew") || !strings.Contains(got[0].Msg, "layer entry") {
		t.Errorf("message does not name the package and the fix: %s", got[0].Msg)
	}
	if got[0].File != "" {
		t.Errorf("file = %q: no area file is derivable for an unmapped package", got[0].File)
	}
}

func TestCheckK4EdgeOfAnUnmappedPackageOnlyReportsTheUnmappedPackage(t *testing.T) {
	t.Parallel()
	g := k4TestGraph([]string{"internal/brandnew"}, "internal/brandnew>internal/agent", "internal/models>internal/brandnew")

	got := checkK4(g, k4TestAreas())

	if want := []string{"R8"}; !reflect.DeepEqual(rulesOf(got), want) {
		t.Errorf("rules = %v, want %v: %v", rulesOf(got), want, got)
	}
}

func TestCheckK4StaleAllowlistEntryFails(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		edges []string
		entry AllowEntry
	}{
		{"edge vanished", nil, allowEntry("internal/commands", "internal/agent")},
		{"edge exists but no rule forbids it", []string{"internal/ops>internal/db"}, allowEntry("internal/ops", "internal/db")},
		{"importer package vanished", nil, allowEntry("internal/gone", "internal/agent")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := checkK4(k4TestGraph(nil, tc.edges...), k4TestAreas(tc.entry))

			if len(got) != 1 || got[0].Rule != "ALLOW-stale" {
				t.Fatalf("violations = %v, want one ALLOW-stale", got)
			}
			if want := areaFilePrefix + "area_a9_test.go"; got[0].File != want {
				t.Errorf("file = %q, want the file holding the entry, %q", got[0].File, want)
			}
		})
	}
}

func TestCheckK4StaleLayerEntryFails(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas()
	areas[0].Layers = append(areas[0].Layers,
		LayerEntry{Package: "internal/gone", Layer: layerLeaf},
		LayerEntry{Package: "internal/integration", Layer: layerTestSupport}, // exempt: not a production package
	)

	got := checkK4(k4TestGraph(nil), areas)

	if want := []string{"R8-stale", "R8-stale"}; !reflect.DeepEqual(rulesOf(got), want) {
		t.Fatalf("rules = %v, want %v: %v", rulesOf(got), want, got)
	}
	for _, v := range got {
		if v.File != areaFilePrefix+"area_a1_test.go" {
			t.Errorf("file = %q, want the file holding the entry", v.File)
		}
	}
}

func TestCheckK4PackageMappedTwiceFails(t *testing.T) {
	t.Parallel()
	t.Run("in one area", func(t *testing.T) {
		t.Parallel()
		areas := k4TestAreas()
		areas[0].Layers = append(areas[0].Layers, LayerEntry{Package: "internal/models", Layer: layerDomain})
		got := checkK4(k4TestGraph(nil), areas)
		if want := []string{"R8-dup"}; !reflect.DeepEqual(rulesOf(got), want) {
			t.Errorf("rules = %v, want %v: %v", rulesOf(got), want, got)
		}
	})
	t.Run("across areas", func(t *testing.T) {
		t.Parallel()
		areas := k4TestAreas()
		areas[2].Layers = append(areas[2].Layers, LayerEntry{Package: "internal/models", Layer: layerPresentation})
		got := checkK4(k4TestGraph(nil), areas)
		if want := []string{"R8-dup"}; !reflect.DeepEqual(rulesOf(got), want) {
			t.Fatalf("rules = %v, want %v: %v", rulesOf(got), want, got)
		}
		if !strings.Contains(got[0].Msg, "area_a1_test.go") || !strings.Contains(got[0].Msg, "area_a7_test.go") {
			t.Errorf("message names neither area file: %s", got[0].Msg)
		}
	})
}

func TestCheckK4UnknownLayerValueFails(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas()
	areas[0].Layers[0].Layer = "bogus"

	got := checkK4(k4TestGraph(nil), areas)

	if want := []string{"R8-layer"}; !reflect.DeepEqual(rulesOf(got), want) {
		t.Errorf("rules = %v, want %v: %v", rulesOf(got), want, got)
	}
}

func TestCheckK4AllowlistEntryWithEmptyFieldFails(t *testing.T) {
	t.Parallel()
	blank := func(f func(*AllowEntry)) AllowEntry {
		e := allowEntry("internal/commands", "internal/agent")
		f(&e)
		return e
	}
	tests := map[string]AllowEntry{
		"importer":  blank(func(e *AllowEntry) { e.Importer = "" }),
		"imported":  blank(func(e *AllowEntry) { e.Imported = "" }),
		"owner":     blank(func(e *AllowEntry) { e.Owner = "" }),
		"rationale": blank(func(e *AllowEntry) { e.Rationale = "  " }),
	}
	for field, entry := range tests {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			got := checkK4(k4TestGraph(nil, "internal/commands>internal/agent"), k4TestAreas(entry))

			// The malformed entry reports itself and does not cover the edge.
			if want := []string{"ALLOW-entry", "R5"}; !reflect.DeepEqual(sortedCopy(rulesOf(got)), want) {
				t.Fatalf("rules = %v, want %v: %v", rulesOf(got), want, got)
			}
			for _, v := range got {
				if v.Rule == "ALLOW-entry" && !strings.Contains(v.Msg, field) {
					t.Errorf("message does not name the empty field %q: %s", field, v.Msg)
				}
			}
		})
	}
}

func TestCheckK4DuplicateAllowlistEntryFails(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas(allowEntry("internal/commands", "internal/agent"), allowEntry("internal/commands", "internal/agent"))
	areas[2].Allowlist = []AllowEntry{allowEntry("internal/commands", "internal/agent")}

	got := checkK4(k4TestGraph(nil, "internal/commands>internal/agent"), areas)

	// Three entries for one edge: the first is the entry, the other two duplicate it.
	if want := []string{"ALLOW-entry", "ALLOW-entry"}; !reflect.DeepEqual(rulesOf(got), want) {
		t.Errorf("rules = %v, want %v: %v", rulesOf(got), want, got)
	}
}

func TestCheckK4ReportsEveryViolationInOneRunSorted(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas(allowEntry("internal/gone", "internal/agent"))
	areas[0].Layers = append(areas[0].Layers, LayerEntry{Package: "internal/gone2", Layer: layerLeaf})
	g := k4TestGraph([]string{"internal/brandnew"}, "internal/ops>internal/agent", "internal/jsonout>internal/ops")

	got := checkK4(g, areas)

	if len(got) != 5 {
		t.Fatalf("violations = %v, want 5 (R2, R6, R8, R8-stale, ALLOW-stale)", got)
	}
	keys := make([]string, len(got))
	for i, v := range got {
		keys[i] = v.File + "\x00" + v.Rule + "\x00" + v.Subject
	}
	if !sort.StringsAreSorted(keys) {
		t.Errorf("violations not sorted by file, rule, subject: %v", keys)
	}
	seen := map[string]bool{}
	for _, r := range rulesOf(got) {
		seen[r] = true
	}
	for _, r := range []string{"R2", "R6", "R8", "R8-stale", "ALLOW-stale"} {
		if !seen[r] {
			t.Errorf("rule %s missing from %v", r, rulesOf(got))
		}
	}
}

func TestCheckK4MessagesNameRuleEdgeLayersAndAreaFileWithoutAbsolutePath(t *testing.T) {
	t.Parallel()
	areas := k4TestAreas(allowEntry("internal/gone", "internal/agent"))
	g := k4TestGraph([]string{"internal/brandnew"}, "internal/commands>internal/agent")

	got := checkK4(g, areas)

	byRule := map[string]k4Violation{}
	for _, v := range got {
		byRule[v.Rule] = v
		if strings.Contains(v.Msg, "/home/") || strings.HasPrefix(v.Msg, "/") || strings.Contains(v.Msg, " /") {
			t.Errorf("message carries an absolute path: %s", v.Msg)
		}
		if !strings.Contains(v.Msg, v.Rule) {
			t.Errorf("message does not name its rule %s: %s", v.Rule, v.Msg)
		}
	}
	edge := byRule["R5"].Msg
	for _, want := range []string{"internal/commands → internal/agent", "internal/commands/x.go", "presentation", "runtime", areaFilePrefix + "area_a7_test.go"} {
		if !strings.Contains(edge, want) {
			t.Errorf("R5 message lacks %q: %s", want, edge)
		}
	}
	if stale := byRule["ALLOW-stale"].Msg; !strings.Contains(stale, areaFilePrefix+"area_a9_test.go") || !strings.Contains(stale, "internal/gone → internal/agent") {
		t.Errorf("ALLOW-stale message lacks the entry's file or edge: %s", stale)
	}
}
