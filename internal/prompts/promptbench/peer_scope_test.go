package promptbench_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/prompts/promptbench"
	"github.com/liza-mas/liza/internal/referencecontract"
)

type peerScopeRow struct {
	Role            string `json:"role"`
	Variant         string `json:"variant"`
	BeforeBytes     int    `json:"before_bytes"`
	AfterBytes      int    `json:"after_bytes"`
	DeltaBytes      int    `json:"delta_bytes"`
	ReferenceBefore int    `json:"reference_before_bytes"`
	ReferenceAfter  int    `json:"reference_after_bytes"`
}

// TestPeerScopePayload isolates a2349382's peer elision on current templates,
// not historical prompt totals. Every task variant receives the same assigned
// fragment, so uniform absolute savings do not establish runtime applicability.
func TestPeerScopePayload(t *testing.T) {
	shape := promptbench.CalibratedShape()
	beforeContext, afterContext := peerScopeContexts(t, shape)
	config, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(config)
	root := t.TempDir()
	if err := promptbench.WriteFixtureProjectRoot(root); err != nil {
		t.Fatal(err)
	}
	dashboard, _, err := prompts.RenderOrchestratorDashboard(promptbench.FixtureState(shape), root, "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}

	var rows []peerScopeRow
	measure := func(role, roleType, pair, wake string) {
		t.Helper()
		var err error
		data := promptbench.FixtureRoleContext(shape, role, role+"-1", roleType)
		data.TaskRolePair = pair
		for i := range data.PhaseDependencyTasks {
			data.PhaseDependencyTasks[i].RolePair = pair
		}
		data.Skills, err = resolver.Skills(role)
		if err != nil {
			t.Fatal(err)
		}
		data.MandatoryDocs, err = resolver.MandatoryDocs(role)
		if err != nil {
			t.Fatal(err)
		}
		sections, err := resolver.ContextSections(role)
		if err != nil {
			t.Fatal(err)
		}
		sections, err = agent.TaskContextSections(sections, &models.Task{ID: data.TaskID, RolePair: pair}, data, resolver)
		if err != nil {
			t.Fatal(err)
		}
		variant := pair
		if roleType == "orchestrator" {
			data.TaskID = ""
			data.DashboardOutput = dashboard
			data.WakeInstruction, err = prompts.RenderWakeInstructions(wake, data.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			variant = "wake " + wake
		}
		base, err := prompts.BuildBasePrompt(prompts.BasePromptConfig{
			Role: role, AgentID: data.AgentID, TaskID: data.TaskID, SpecsDir: "specs",
			ProjectRoot: "/generated", StatePath: "/generated/state.yaml",
			GoalDesc: "generated goal", GoalSpecRef: data.GoalSpecRef,
		})
		if err != nil {
			t.Fatal(err)
		}
		render := func(context string) string {
			t.Helper()
			data.ResolvedReferenceContext = context
			body, err := prompts.BuildRoleContext(role, sections, data)
			if err != nil {
				t.Fatal(err)
			}
			return base + body
		}
		before, after := render(beforeContext), render(afterContext)
		beforeReport, afterReport := promptbench.MeasureRendered(before), promptbench.MeasureRendered(after)
		row := peerScopeRow{
			Role: role, Variant: variant, BeforeBytes: len(before), AfterBytes: len(after),
			DeltaBytes:      len(after) - len(before),
			ReferenceBefore: beforeReport.CarrierBlock.Bytes, ReferenceAfter: afterReport.CarrierBlock.Bytes,
		}
		if roleType == "orchestrator" {
			if before != after || row.ReferenceBefore != 0 || row.ReferenceAfter != 0 {
				t.Fatalf("%s: orchestrator unexpectedly includes the assigned payload", variant)
			}
		} else {
			if strings.Count(before, beforeContext) != 1 || strings.Count(after, afterContext) != 1 {
				t.Fatalf("%s/%s: expected exactly one reference context in each prompt", role, variant)
			}
			if strings.Replace(before, beforeContext, "<context>", 1) != strings.Replace(after, afterContext, "<context>", 1) {
				t.Fatalf("%s/%s: instructions outside reference context changed", role, variant)
			}
			if row.DeltaBytes >= 0 || row.DeltaBytes != len(afterContext)-len(beforeContext) ||
				row.DeltaBytes != row.ReferenceAfter-row.ReferenceBefore {
				t.Fatalf("%s/%s: reduction must be entirely in reference context: %+v", role, variant, row)
			}
		}
		rows = append(rows, row)
		t.Logf("%-24s %-36s %d -> %d (%+d, %+.2f%%)", role, variant,
			row.BeforeBytes, row.AfterBytes, row.DeltaBytes, 100*float64(row.DeltaBytes)/float64(row.BeforeBytes))
	}

	// Enumerate actual pairs rather than assuming which roles have root variants.
	// Every configured role must be measured, including future unpaired roles.
	measured := map[string]bool{}
	var pairs []string
	for pair := range config.Pipeline.RolePairs {
		pairs = append(pairs, pair)
	}
	sort.Strings(pairs)
	for _, pair := range pairs {
		definition := config.Pipeline.RolePairs[pair]
		for _, role := range []string{definition.Doer, definition.Reviewer} {
			roleType, err := resolver.RoleType(role)
			if err != nil {
				t.Fatal(err)
			}
			measure(role, roleType, pair, "")
			measured[role] = true
		}
	}
	var roles []string
	for role := range config.Pipeline.Roles {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		if measured[role] {
			continue
		}
		roleType, err := resolver.RoleType(role)
		if err != nil {
			t.Fatal(err)
		}
		if roleType != "orchestrator" {
			measure(role, roleType, "", "")
			continue
		}
		for _, wake := range prompts.WakeTriggers {
			measure(role, roleType, "", wake)
		}
	}

	report := struct {
		Scope   string         `json:"scope"`
		Fixture string         `json:"fixture"`
		Rows    []peerScopeRow `json:"rows"`
	}{
		Scope:   "Isolated peer-elision off/on comparison on current templates, not historical totals. The same synthetic assigned-fragment payload is supplied to every task variant: absolute savings are uniform by construction; only totals and percentages vary. Orchestrator wakes omit the payload. Runtime fragment applicability per role, pointed-section rereads and token/cost savings are not measured.",
		Fixture: "CalibratedShape and GenerateCarriers; only AssignedHeading is cleared in the control. Ancestor-reference and duplicate-reference elision remain enabled in both arms.",
		Rows:    rows,
	}
	got, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const artifact = "testdata/peer-scope.json"
	// Unlike TestBaseline, a missing artifact intentionally fails: ordinary
	// validation must not manufacture the expected evidence it compares against.
	if os.Getenv("PROMPTBENCH_UPDATE") != "" {
		if err := os.WriteFile(artifact, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("peer-scope measurement changed; inspect the difference, then deliberately regenerate with PROMPTBENCH_UPDATE=1 and -run '^TestPeerScopePayload$'")
	}
}

func peerScopeContexts(t *testing.T, shape promptbench.Shape) (string, string) {
	t.Helper()
	treatment := promptbench.GenerateCarriers(shape)
	control := slices.Clone(treatment)
	assignedCount := 0
	for i := range control {
		if control[i].AssignedHeading != "" {
			assignedCount++
		}
		control[i].AssignedHeading = ""
	}
	if assignedCount != 1 {
		t.Fatalf("fixture must exercise exactly one assigned carrier, got %d", assignedCount)
	}
	// Rendering sorts its input, so preserve the original fixture sets.
	before := promptbench.RenderResolvedReferenceContext(slices.Clone(control))
	after := promptbench.RenderResolvedReferenceContext(slices.Clone(treatment))
	for _, carrier := range treatment {
		kept := carrier.Span
		if carrier.AssignedHeading != "" {
			var err error
			kept, err = referencecontract.ExtractSection(carrier.Span, carrier.AssignedHeading)
			if err != nil {
				t.Fatal(err)
			}
		}
		if !strings.Contains(after, strings.TrimSpace(kept)) {
			t.Fatalf("lost assigned section or unassigned carrier %s", carrier.Path)
		}
	}
	// These calibrated shared/peer sections surround the assigned Section 25.
	// The runtime tests cover arbitrary Markdown layouts; here pin the fixture
	// so a narrower input cannot silently manufacture a better measurement.
	for _, section := range []int{0, 24, 25, 29} {
		if !strings.Contains(after, fmt.Sprintf("c1-s%d line 0:", section)) {
			t.Fatalf("shared or assigned Section %d was lost", section)
		}
	}
	for _, section := range []int{26, 27, 28} {
		if strings.Contains(after, fmt.Sprintf("c1-s%d line 0:", section)) ||
			!strings.Contains(after, fmt.Sprintf("fixture-carrier-1.md#Section %d\" @ fixture-head — peer", section)) {
			t.Fatalf("peer Section %d must become a pinned pointer", section)
		}
	}
	b, a := promptbench.MeasureRendered(before), promptbench.MeasureRendered(after)
	if !reflect.DeepEqual(b.ReferencesByCarrier, a.ReferencesByCarrier) ||
		b.DuplicateReferences.ElidedPointers != a.DuplicateReferences.ElidedPointers {
		t.Fatal("peer elision changed declared-reference payload")
	}
	if !strings.Contains(after, "earlier revision") {
		t.Fatal("stale reference disappeared")
	}
	return before, after
}
