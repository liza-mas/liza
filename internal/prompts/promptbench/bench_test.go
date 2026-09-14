package promptbench_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/prompts/promptbench"
)

// -state renders a real run's state instead of the committed fixture, as a
// drift check. It is a cross-check, not the instrument of record: the baseline
// comparison runs on the committed fixture so it reproduces anywhere.
var stateFlag = flag.String("state", "", "path to a live state.yaml to measure instead of the fixture")

const baselinePath = "testdata/baseline.json"

// renderFixture renders the sections that carry payload, using the calibrated
// synthetic fixture.
func renderFixture(t *testing.T) string {
	t.Helper()
	shape := promptbench.CalibratedShape()

	phaseDeps := make([]prompts.SiblingTaskSummary, 0, shape.PhaseDependencies)
	for i := range shape.PhaseDependencies {
		phaseDeps = append(phaseDeps, prompts.SiblingTaskSummary{
			ID:       fmt.Sprintf("fixture-plan-%d", i),
			Status:   "MERGED",
			PlanRef:  fmt.Sprintf("specs/generated/plan-%d.md", i),
			RolePair: "code-planning-pair",
		})
	}

	descendants := make([]prompts.IntegrationDescendantSummary, 0, shape.Descendants)
	for i := range shape.Descendants {
		deps := make([]string, 0, shape.DepsPerDescendant)
		for j := range shape.DepsPerDescendant {
			deps = append(deps, fmt.Sprintf("fixture-cp-%d-code-%d", i%3, j))
		}
		descendants = append(descendants, prompts.IntegrationDescendantSummary{
			ID:          fmt.Sprintf("fixture-cp-1-code-%d", i),
			Description: "generated descendant standing in for an attributed task",
			DoneWhen:    "generated acceptance criteria",
			SpecRef:     "specs/generated/goal.md",
			Commit:      fmt.Sprintf("%040x", i+1),
			DependsOn:   deps,
		})
	}

	data := &prompts.RoleContextData{
		Role: "code-plan-reviewer", AgentID: "code-plan-reviewer-1", RoleType: "reviewer",
		TaskID: "fixture-cp-1", Description: "generated task", DoneWhen: "generated",
		SpecRef: "specs/generated/goal.md", GoalSpecRef: "specs/generated/goal.md",
		Worktree: "/generated/worktree", IntegrationBranch: "integration",
		BaseCommit: strings.Repeat("a", 40), ReviewCommit: strings.Repeat("b", 40),

		ResolvedReferenceContext: promptbench.GenerateResolvedReferenceContext(shape),

		TaskRolePair:         "code-planning-pair",
		PhaseDependencyTasks: phaseDeps,
		TotalPlanTasks:       19,
		TaskOrdinal:          3,

		// IntegrationPhase "slice" is what gates the descendant block. The
		// measured run contained no slice-integration prompts, so site 2 is
		// zero there; the fixture still renders it so a reduction targeting
		// site 2 is measurable rather than structurally invisible.
		IntegrationPhase:        models.IntegrationAnalysisPhaseSlice,
		IntegrationDescendants:  descendants,
		IntegrationSourceCommit: strings.Repeat("c", 40),
		IntegrationRootTaskIDs:  []string{"fixture-cp-1", "fixture-cp-4"},
		IntegrationOriginatingPlan: &prompts.IntegrationPlanSummary{
			ID: "fixture-cp-1", Description: "generated originating plan",
			DoneWhen: "generated", SpecRef: "specs/generated/goal.md",
			PlanRef: "specs/generated/plan.md", ArchRef: "specs/generated/arch.md",
		},
	}

	sections := []string{
		"resolved-reference-context",
		"collective-plan-scoping",
		"branch-integration-context",
	}
	rendered, err := prompts.BuildRoleContext(data.Role, sections, data)
	if err != nil {
		t.Fatalf("BuildRoleContext: %v", err)
	}

	// Site 4 renders through the orchestrator dashboard, a separate entry
	// point. Measuring only the role context would report site 4 as zero and
	// look like a finding rather than a gap in the harness.
	state := fixtureState(shape)
	dashboard, _, err := prompts.RenderOrchestratorDashboard(state, fixtureProjectRoot(t), "orchestrator-1")
	if err != nil {
		t.Fatalf("RenderOrchestratorDashboard: %v", err)
	}
	return rendered + "\n" + dashboard
}

// fixtureProjectRoot lays out the minimum on-disk workspace the dashboard
// renderer needs, using the embedded default pipeline so the harness measures
// the shipped topology rather than a hand-written stand-in.
func fixtureProjectRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runtimeDir := filepath.Join(dir, paths.ProjectDirName())
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("mkdir project runtime directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "pipeline.yaml"), embedded.PipelineConfigContent(), 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}
	return dir
}

func fixtureState(shape promptbench.Shape) *models.State {
	state := &models.State{}
	for i := range shape.ActiveTasks {
		deps := make([]string, 0, shape.DepsPerActiveTask)
		for j := range shape.DepsPerActiveTask {
			deps = append(deps, fmt.Sprintf("fixture-cp-%d-code-%d", i%3, j))
		}
		state.Tasks = append(state.Tasks, models.Task{
			ID:        fmt.Sprintf("fixture-active-%d", i),
			Status:    models.TaskStatusReady,
			RolePair:  "coding-pair",
			DependsOn: deps,
		})
	}
	return state
}

// TestBaseline measures the fixture and compares it against the committed
// baseline. Regenerate deliberately with -update after a change that is
// intended to move payload; the diff is the evidence.
func TestBaseline(t *testing.T) {
	if *stateFlag != "" {
		t.Skip("-state set; run TestCalibrationDrift instead")
	}
	report := promptbench.MeasureRendered(renderFixture(t))

	got, err := report.JSON()
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	want, err := os.ReadFile(baselinePath)
	if os.IsNotExist(err) || os.Getenv("PROMPTBENCH_UPDATE") != "" {
		if mkErr := os.MkdirAll(filepath.Dir(baselinePath), 0o755); mkErr != nil {
			t.Fatalf("mkdir testdata: %v", mkErr)
		}
		if wErr := os.WriteFile(baselinePath, append(got, '\n'), 0o644); wErr != nil {
			t.Fatalf("write baseline: %v", wErr)
		}
		t.Logf("baseline written to %s", baselinePath)
		return
	}
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}

	if strings.TrimSpace(string(want)) != strings.TrimSpace(string(got)) {
		t.Errorf("rendered payload differs from committed baseline.\n"+
			"If this change is intended to move payload, regenerate with:\n"+
			"  PROMPTBENCH_UPDATE=1 go test ./internal/prompts/promptbench -run TestBaseline\n"+
			"and let the baseline diff stand as the evidence.\n\ngot:\n%s", got)
	}
}

// TestSitesAreEnumerated asserts the harness reports every dependency render
// site, including sites that measure zero.
//
// A site that renders nothing is a finding — a reduction targeting it cannot
// be validated by this fixture — and an omitted row is indistinguishable from
// a site nobody measured. An earlier measurement of this surface reported a
// single aggregate and was wrong by four orders of magnitude; this test is the
// guard against that recurring.
func TestSitesAreEnumerated(t *testing.T) {
	report := promptbench.MeasureRendered(renderFixture(t))

	want := []string{
		"site2_descendant_dependencies",
		"site3_sibling_consistency_rule",
		"site4_active_task_digest",
		"site5_task_graph_digest",
	}
	if len(report.DependsOnSites) != len(want) {
		t.Fatalf("got %d sites, want %d", len(report.DependsOnSites), len(want))
	}
	for i, name := range want {
		if report.DependsOnSites[i].Site != name {
			t.Errorf("site[%d] = %q, want %q", i, report.DependsOnSites[i].Site, name)
		}
		if report.DependsOnSites[i].Source == "" {
			t.Errorf("site %q has no source reference", name)
		}
	}

	// Sites 2-4 must render non-zero in the fixture, or the fixture cannot
	// validate a reduction that targets them.
	for _, s := range report.DependsOnSites[:3] {
		if s.Bytes == 0 {
			t.Errorf("site %q measured zero in the fixture; it cannot validate a reduction there", s.Site)
		}
	}
}

// TestCarrierStructureIsCalibrated asserts the fixture's carriers have section
// structure, not just bulk.
//
// Without it, section-scoped inclusion is unmeasurable — and the fixture is
// committed before the reduction is chosen, so discovering that later means
// regenerating the fixture and invalidating the baseline it exists to
// preserve.
func TestCarrierStructureIsCalibrated(t *testing.T) {
	report := promptbench.MeasureRendered(renderFixture(t))
	shape := promptbench.CalibratedShape()

	wantHeadings := shape.Carriers * shape.HeadingsPerCarrier
	if report.CarrierHeadings < wantHeadings*9/10 {
		t.Errorf("carrier headings = %d, want ~%d: carriers lack the section structure "+
			"that makes section-scoped inclusion measurable", report.CarrierHeadings, wantHeadings)
	}

	calib := loadCalibration(t)
	wantMean := calib.CarrierStructure.MeanSectionBytes
	if got := report.MeanSectionBytes; got < wantMean/2 || got > wantMean*2 {
		t.Errorf("mean carrier section = %d bytes, calibration says %d; "+
			"fixture section granularity has drifted from the measured run", got, wantMean)
	}
}

type calibration struct {
	CarrierStructure struct {
		TotalHeadings    int `json:"total_headings"`
		MeanSectionBytes int `json:"mean_section_bytes"`
	} `json:"carrier_structure"`
	CarrierBlock struct {
		ShareOfCorpus float64 `json:"share_of_corpus"`
	} `json:"carrier_block"`
}

func loadCalibration(t *testing.T) calibration {
	t.Helper()
	raw, err := os.ReadFile("testdata/run-calibration.json")
	if err != nil {
		t.Fatalf("read calibration: %v", err)
	}
	var c calibration
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse calibration: %v", err)
	}
	return c
}

// TestCarrierDominatesPayload pins the finding the reduction targets: the
// carrier block, not the dependency sites, is where the bytes are.
//
// If a change inverts this, the reduction's premise no longer holds and the
// plan that chose it should be revisited rather than the assertion relaxed.
func TestCarrierDominatesPayload(t *testing.T) {
	report := promptbench.MeasureRendered(renderFixture(t))

	if report.CarrierShare <= report.DependsOnShare {
		t.Errorf("carrier share %.4f <= depends_on share %.4f: the premise that payload "+
			"reduction targets carriers no longer holds", report.CarrierShare, report.DependsOnShare)
	}
	if report.CarrierShare < 0.5 {
		t.Errorf("carrier share %.4f is far below the calibrated %.3f; fixture has drifted",
			report.CarrierShare, loadCalibration(t).CarrierBlock.ShareOfCorpus)
	}
}
