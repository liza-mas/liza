package promptbench_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/prompts/promptbench"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// TestDeclaredScopePayload complements TestPeerScopePayload with explicit
// section-only rendering. It isolates presentation on a calibrated synthetic
// carrier set; runtime authoring/expansion applicability is tested in agent.
func TestDeclaredScopePayload(t *testing.T) {
	shape := promptbench.CalibratedShape()
	carriers := promptbench.GenerateCarriers(shape)
	assigned := -1
	for i := range carriers {
		if carriers[i].AssignedHeading != "" {
			if assigned != -1 {
				t.Fatal("fixture has more than one assigned carrier")
			}
			assigned = i
		}
	}
	if assigned == -1 || len(carriers[assigned].Refs) < 2 {
		t.Fatal("fixture needs an assigned carrier and selected/unselected references")
	}
	carrier := &carriers[assigned]
	for i := range carrier.Refs {
		carrier.Refs[i].ID = fmt.Sprintf("product-%d", i)
	}
	const heading = "Scope 1: Fixture assignment"
	needle := carrier.AssignedHeading + "\n"
	if strings.Count(carrier.Span, needle) != 1 {
		t.Fatal("assigned fixture heading must be unique")
	}
	carrier.Span = strings.Replace(carrier.Span, needle,
		heading+"\n\n**Direct references:** [\"product-0\", \"shared\"]\n", 1)
	carrier.AssignedHeading = heading
	const shared = "\n## Shared architecture\n\nshared-authority-owned-boundary\n"
	carrier.Span += shared
	carrier.Refs = append(carrier.Refs, referencecontract.Reference{
		ID: "shared", Path: carrier.Path, Heading: "Shared architecture",
		Revision: carrier.Revision, BlobOID: carrier.BlobOID, Span: shared,
	})
	control, treatment := slices.Clone(carriers), slices.Clone(carriers)
	for i := range treatment {
		if i == assigned {
			treatment[i].SectionOnly = true
			treatment[i].InlineReferenceIDs = map[string]bool{"product-0": true, "shared": true}
		} else {
			treatment[i].PointerOnly = true
		}
	}
	beforeContext := promptbench.RenderResolvedReferenceContext(control)
	afterContext := promptbench.RenderResolvedReferenceContext(treatment)
	if !strings.Contains(afterContext, "**Direct references:** [\"product-0\", \"shared\"]") ||
		!strings.Contains(afterContext, carrier.Refs[0].Span) || !strings.Contains(afterContext, shared) {
		t.Fatal("assigned declaration, selected product span, or same-file shared authority lost")
	}
	if strings.Contains(afterContext, carrier.Refs[1].Span) ||
		!strings.Contains(afterContext, "OTHER SECTIONS AND REFERENCES") ||
		!strings.Contains(afterContext, "product-1") {
		t.Fatal("unselected bulk must become discoverable pinned navigation")
	}
	config, err := pipeline.LoadEmbeddedReference()
	if err != nil {
		t.Fatal(err)
	}
	resolver := pipeline.NewResolver(config)
	for _, role := range []string{"architect", "code-planner"} {
		t.Run(role, func(t *testing.T) {
			data := promptbench.FixtureRoleContext(shape, role, role+"-1", "doer")
			pair := "code-planning-pair"
			if role == "architect" {
				pair = "architecture-pair"
			}
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
			if strings.Count(before, beforeContext) != 1 || strings.Count(after, afterContext) != 1 {
				t.Fatal("expected exactly one reference context per prompt")
			}
			if strings.Replace(before, beforeContext, "<context>", 1) != strings.Replace(after, afterContext, "<context>", 1) {
				t.Fatal("instructions outside reference context changed")
			}
			beforeReport, afterReport := promptbench.MeasureRendered(before), promptbench.MeasureRendered(after)
			delta := len(after) - len(before)
			if delta >= 0 || delta != len(afterContext)-len(beforeContext) ||
				delta != afterReport.CarrierBlock.Bytes-beforeReport.CarrierBlock.Bytes {
				t.Fatal("section-only reduction must be confined to reference context")
			}
			t.Logf("complete %s prompt: %d -> %d (%+d); synthetic renderer comparison, not live savings", role, len(before), len(after), delta)
		})
	}
}

func TestProviderNavigationMeasurement(t *testing.T) {
	data := promptbench.FixtureRoleContext(promptbench.CalibratedShape(), "architect", "architect-1", "doer")
	data.PlannerProviders = []prompts.PlannerProviderSummary{{
		ID: "foundation-ar-0", Status: "MERGED", Reasons: []string{"sibling Scope 0"},
		Artifacts: []prompts.PlannerArtifactPointer{
			{Kind: "architecture", Ref: "specs/foundation.md#Scope 0", Refs: []string{"specs/foundation.md#Scope 0"},
				File: "specs/foundation.md", ProducerID: "foundation-ar-0", Commit: strings.Repeat("a", 40)},
			{Kind: "plan", Ref: "specs/foundation-plan.md", Refs: []string{"specs/foundation-plan.md"},
				File: "specs/foundation-plan.md", ProducerID: "foundation-cp-0", Commit: strings.Repeat("b", 40),
				Units: []prompts.PlannerUnitPointer{{ID: "foundation-code-24", Status: "READY", PlanRef: "specs/foundation-plan.md#Unit 25"}}},
		},
	}}
	rendered, err := prompts.BuildRoleContext("architect", []string{"provider-tasks"}, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"foundation-ar-0 [MERGED]", "foundation-cp-0", "foundation-code-24 [READY]", "#Unit 25", strings.Repeat("a", 40), strings.Repeat("b", 40)} {
		if !strings.Contains(rendered, required) {
			t.Fatalf("provider block lost %q", required)
		}
	}
	block := strings.TrimSpace(rendered) + "\n"
	report := promptbench.MeasureRendered(block + "=== OUTSIDE PROVIDER NAVIGATION ===\nProvider excluded [MERGED]\n")
	if len(report.DependsOnSites) != 5 {
		t.Fatalf("got %d sites, want 5", len(report.DependsOnSites))
	}
	site := report.DependsOnSites[4]
	if site.Site != "site6_provider_artifacts_and_units" || site.Source != "blocks/provider_tasks.tmpl" ||
		site.Occurrences != 1 || site.Bytes != len(block) || site.LongestRun == 0 {
		t.Fatalf("complete provider block measurement incorrect: %+v, want %d bytes", site, len(block))
	}
	if report.DependsOnTotal != site.Bytes {
		t.Fatal("provider bytes must contribute once to dependency total")
	}
}
