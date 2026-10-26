package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// renderInitialPlanningWake renders the INITIAL_PLANNING wake instruction for
// a goal that names entryPoint (or none) in a project holding the pipeline.
func renderInitialPlanningWake(t *testing.T, projectRoot, entryPoint string) string {
	t.Helper()
	state := testhelpers.CreateValidState()
	state.Tasks = nil
	state.Goal.SpecRef = "specs/goal.md"
	state.Goal.EntryPoint = entryPoint
	_, instruction, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
	if err != nil {
		t.Fatalf("RenderOrchestratorDashboard: %v", err)
	}
	return instruction
}

// setupPipelineConfig writes the production embedded pipeline.yaml into a temp
// project runtime directory and returns the temp dir as projectRoot.
func setupPipelineConfig(t *testing.T) string {
	t.Helper()
	return setupPipelineConfigContent(t, embedded.PipelineConfigContent())
}

func TestResolveWakeRoutesRouteData(t *testing.T) {
	projectRoot := setupPipelineConfig(t)

	routes, err := resolveWakeRoutes("", projectRoot)
	if err != nil {
		t.Fatalf("resolveWakeRoutes: %v", err)
	}

	entryPoints := wakeEntryPointsByName(t, routes.EntryPoints)
	tests := []struct {
		name           string
		simpleRolePair string
		simpleTaskType string
		simpleDisplay  string
		simpleIDPrefix string
		fanOutRolePair string
		fanOutTaskType string
		fanOutDisplay  string
		fanOutIDPrefix string
	}{
		{
			name:           "general-objective",
			simpleRolePair: "epic-planning-pair",
			simpleTaskType: "epic-planning",
			simpleDisplay:  "Epic Planner",
			simpleIDPrefix: "ep",
			fanOutRolePair: "epic-planning-main-pair",
			fanOutTaskType: "epic-planning",
			fanOutDisplay:  "Epic Planner",
			fanOutIDPrefix: "epm",
		},
		{
			name:           "functional-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleDisplay:  "Architect",
			simpleIDPrefix: "ar",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutDisplay:  "Architect",
			fanOutIDPrefix: "arm",
		},
		{
			name:           "detailed-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleDisplay:  "Architect",
			simpleIDPrefix: "ar",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutDisplay:  "Architect",
			fanOutIDPrefix: "arm",
		},
		{
			name:           "technical-spec",
			simpleRolePair: "code-planning-pair",
			simpleTaskType: "planning",
			simpleDisplay:  "Code Planner",
			simpleIDPrefix: "cp",
			fanOutRolePair: "code-planning-main-pair",
			fanOutTaskType: "planning",
			fanOutDisplay:  "Code Planner",
			fanOutIDPrefix: "cpm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := entryPoints[tt.name]
			assertWakeRouteData(t, ep, tt.simpleRolePair, tt.simpleTaskType, tt.simpleDisplay, tt.simpleIDPrefix, tt.fanOutRolePair, tt.fanOutTaskType, tt.fanOutDisplay, tt.fanOutIDPrefix)

			explicit, err := resolveWakeRoutes(tt.name, projectRoot)
			if err != nil {
				t.Fatalf("resolveWakeRoutes explicit %q: %v", tt.name, err)
			}
			if explicit.Resolved == nil || explicit.Resolved.Name != tt.name {
				t.Fatalf("Resolved = %+v, want entry-point %q", explicit.Resolved, tt.name)
			}
			assertWakeRouteData(t, *explicit.Resolved, tt.simpleRolePair, tt.simpleTaskType, tt.simpleDisplay, tt.simpleIDPrefix, tt.fanOutRolePair, tt.fanOutTaskType, tt.fanOutDisplay, tt.fanOutIDPrefix)
		})
	}
}

func TestResolveWakeRoutesMissingFanOutMapping(t *testing.T) {
	content := strings.ReplaceAll(string(embedded.PipelineConfigContent()), "      decomposition-root: true\n      decomposition-output-ref: plan_ref\n", "")
	content = strings.ReplaceAll(content, "      decomposition-root: true\n      decomposition-output-ref: arch_ref\n", "")
	projectRoot := setupPipelineConfigContent(t, []byte(content))

	routes, err := resolveWakeRoutes("functional-spec", projectRoot)
	if err != nil {
		t.Fatalf("resolveWakeRoutes: %v", err)
	}

	for _, ep := range routes.EntryPoints {
		if ep.HasFanOutTarget {
			t.Errorf("%s HasFanOutTarget = true, want false", ep.Name)
		}
		if ep.FanOutRolePair != "" {
			t.Errorf("%s FanOutRolePair = %q, want empty", ep.Name, ep.FanOutRolePair)
		}
		if strings.Contains(ep.FanOutRolePair, "main") {
			t.Errorf("%s invented fan-out role-pair %q", ep.Name, ep.FanOutRolePair)
		}
	}
	if routes.Resolved == nil {
		t.Fatal("Resolved = nil, want the functional-spec entry-point")
	}
	if routes.Resolved.HasFanOutTarget {
		t.Fatal("Resolved.HasFanOutTarget = true, want false")
	}
	if routes.Resolved.FanOutRolePair != "" {
		t.Fatalf("Resolved.FanOutRolePair = %q, want empty", routes.Resolved.FanOutRolePair)
	}
}

func TestResolveWakeRoutesPropagatesResolverErrors(t *testing.T) {
	projectRoot := setupPipelineConfigContent(t, []byte(`
pipeline:
  roles:
    planner:
      type: doer
      display-name: "Planner"
    reviewer:
      type: reviewer
      display-name: "Reviewer"
  role-pairs:
    root-one-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: ROOT_ONE_INITIAL
        executing: ROOT_ONE_EXECUTING
        submitted: ROOT_ONE_SUBMITTED
        reviewing: ROOT_ONE_REVIEWING
        approved: ROOT_ONE_APPROVED
        rejected: ROOT_ONE_REJECTED
    root-two-pair:
      doer: planner
      reviewer: reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states:
        initial: ROOT_TWO_INITIAL
        executing: ROOT_TWO_EXECUTING
        submitted: ROOT_TWO_SUBMITTED
        reviewing: ROOT_TWO_REVIEWING
        approved: ROOT_TWO_APPROVED
        rejected: ROOT_TWO_REJECTED
    specialized-pair:
      doer: planner
      reviewer: reviewer
      states:
        initial: SPECIALIZED_INITIAL
        executing: SPECIALIZED_EXECUTING
        submitted: SPECIALIZED_SUBMITTED
        reviewing: SPECIALIZED_REVIEWING
        approved: SPECIALIZED_APPROVED
        rejected: SPECIALIZED_REJECTED
  sub-pipelines:
    planning:
      steps: [root-one-pair, root-two-pair, specialized-pair]
      transitions:
        - name: root-one-decompose
          from: root-one-pair.approved
          to: specialized-pair.initial
          trigger: auto
          cardinality: per-subtask
        - name: root-two-decompose
          from: root-two-pair.approved
          to: specialized-pair.initial
          trigger: auto
          cardinality: per-subtask
  entry-points:
    custom: planning.specialized-pair
`))

	_, err := resolveWakeRoutes("", projectRoot)
	if err == nil {
		t.Fatal("resolveWakeRoutes: expected resolver error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple decomposition roots") {
		t.Fatalf("resolveWakeRoutes error = %q, want multiple decomposition roots", err.Error())
	}
}

func wakeEntryPointsByName(t *testing.T, entryPoints []prompts.WakeEntryPoint) map[string]prompts.WakeEntryPoint {
	t.Helper()
	got := make(map[string]prompts.WakeEntryPoint, len(entryPoints))
	for _, ep := range entryPoints {
		got[ep.Name] = ep
	}
	if len(got) != 4 {
		t.Fatalf("entry point count = %d, want 4", len(got))
	}
	return got
}

func assertWakeRouteData(t *testing.T, ep prompts.WakeEntryPoint, simpleRolePair, simpleTaskType, simpleDisplay, simpleIDPrefix, fanOutRolePair, fanOutTaskType, fanOutDisplay, fanOutIDPrefix string) {
	t.Helper()
	if ep.SimpleRolePair != simpleRolePair {
		t.Errorf("SimpleRolePair = %q, want %q", ep.SimpleRolePair, simpleRolePair)
	}
	if ep.SimpleTaskType != simpleTaskType {
		t.Errorf("SimpleTaskType = %q, want %q", ep.SimpleTaskType, simpleTaskType)
	}
	if ep.SimpleDisplayName != simpleDisplay {
		t.Errorf("SimpleDisplayName = %q, want %q", ep.SimpleDisplayName, simpleDisplay)
	}
	if ep.SimpleTaskIDPrefix != simpleIDPrefix {
		t.Errorf("SimpleTaskIDPrefix = %q, want %q", ep.SimpleTaskIDPrefix, simpleIDPrefix)
	}
	if !ep.HasFanOutTarget {
		t.Fatal("HasFanOutTarget = false, want true")
	}
	if ep.FanOutRolePair != fanOutRolePair {
		t.Errorf("FanOutRolePair = %q, want %q", ep.FanOutRolePair, fanOutRolePair)
	}
	if ep.FanOutTaskType != fanOutTaskType {
		t.Errorf("FanOutTaskType = %q, want %q", ep.FanOutTaskType, fanOutTaskType)
	}
	if ep.FanOutDisplayName != fanOutDisplay {
		t.Errorf("FanOutDisplayName = %q, want %q", ep.FanOutDisplayName, fanOutDisplay)
	}
	if ep.FanOutTaskIDPrefix != fanOutIDPrefix {
		t.Errorf("FanOutTaskIDPrefix = %q, want %q", ep.FanOutTaskIDPrefix, fanOutIDPrefix)
	}
}

func setupPipelineConfigContent(t *testing.T, content []byte) string {
	t.Helper()
	dir := t.TempDir()
	lizaDir := filepath.Join(dir, paths.ProjectDirName())
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("mkdir project runtime directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), content, 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}
	return dir
}

func TestWakeInitialPlanningExplicitRoutesRenderResolvedTargets(t *testing.T) {
	projectRoot := setupPipelineConfig(t)

	tests := []struct {
		name           string
		entryPoint     string
		simpleRolePair string
		simpleTaskType string
		simpleIDPrefix string
		simpleDisplay  string
		fanOutRolePair string
		fanOutTaskType string
		fanOutIDPrefix string
		fanOutDisplay  string
	}{
		{
			name:           "general objective",
			entryPoint:     "general-objective",
			simpleRolePair: "epic-planning-pair",
			simpleTaskType: "epic-planning",
			simpleIDPrefix: "ep",
			simpleDisplay:  "Epic Planner",
			fanOutRolePair: "epic-planning-main-pair",
			fanOutTaskType: "epic-planning",
			fanOutIDPrefix: "epm",
			fanOutDisplay:  "Epic Planner",
		},
		{
			name:           "functional spec",
			entryPoint:     "functional-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleIDPrefix: "ar",
			simpleDisplay:  "Architect",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutIDPrefix: "arm",
			fanOutDisplay:  "Architect",
		},
		{
			name:           "detailed spec",
			entryPoint:     "detailed-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleIDPrefix: "ar",
			simpleDisplay:  "Architect",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutIDPrefix: "arm",
			fanOutDisplay:  "Architect",
		},
		{
			name:           "technical spec",
			entryPoint:     "technical-spec",
			simpleRolePair: "code-planning-pair",
			simpleTaskType: "planning",
			simpleIDPrefix: "cp",
			simpleDisplay:  "Code Planner",
			fanOutRolePair: "code-planning-main-pair",
			fanOutTaskType: "planning",
			fanOutIDPrefix: "cpm",
			fanOutDisplay:  "Code Planner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderInitialPlanningWake(t, projectRoot, tt.entryPoint)

			assertWakeContainsAll(t, rendered, []string{
				"Entry-point \"" + tt.entryPoint + "\" was specified. Dispatch simple goals to " + tt.simpleDisplay + ".",
				"SIMPLE GOAL TASK EXAMPLE",
				"\"id\": \"" + tt.simpleIDPrefix + "-1\"",
				"\"type\": \"" + tt.simpleTaskType + "\"",
				"\"role_pair\": \"" + tt.simpleRolePair + "\"",
				"FAN-OUT GOAL TASK EXAMPLE",
				"Fan-out goals dispatch to " + tt.fanOutDisplay + " through the mapped master target.",
				"\"id\": \"" + tt.fanOutIDPrefix + "-1\"",
				"\"type\": \"" + tt.fanOutTaskType + "\"",
				"\"role_pair\": \"" + tt.fanOutRolePair + "\"",
			})
			if got := countJSONRolePair(rendered, tt.simpleRolePair); got != 1 {
				t.Fatalf("simple role_pair %q rendered %d times, want 1\n%s", tt.simpleRolePair, got, rendered)
			}
			if got := countJSONRolePair(rendered, tt.fanOutRolePair); got != 1 {
				t.Fatalf("fan-out role_pair %q rendered %d times, want 1\n%s", tt.fanOutRolePair, got, rendered)
			}
			assertNotContainsAny(t, rendered, []string{
				"\"id\": \"" + tt.simpleIDPrefix + "-2\"",
				"\"id\": \"" + tt.fanOutIDPrefix + "-2\"",
			})
		})
	}
}

func TestWakeInitialPlanningClassificationRendersResolvedRouteData(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	rendered := renderInitialPlanningWake(t, projectRoot, "")

	assertWakeContainsAll(t, rendered, []string{
		"- \"general-objective\": simple role_pair \"epic-planning-pair\", type \"epic-planning\", display \"Epic Planner\", id prefix \"ep\"; fan-out role_pair \"epic-planning-main-pair\", type \"epic-planning\", display \"Epic Planner\", id prefix \"epm\"",
		"- \"functional-spec\": simple role_pair \"architecture-pair\", type \"architecture\", display \"Architect\", id prefix \"ar\"; fan-out role_pair \"architecture-main-pair\", type \"architecture\", display \"Architect\", id prefix \"arm\"",
		"- \"detailed-spec\": simple role_pair \"architecture-pair\", type \"architecture\", display \"Architect\", id prefix \"ar\"; fan-out role_pair \"architecture-main-pair\", type \"architecture\", display \"Architect\", id prefix \"arm\"",
		"- \"technical-spec\": simple role_pair \"code-planning-pair\", type \"planning\", display \"Code Planner\", id prefix \"cp\"; fan-out role_pair \"code-planning-main-pair\", type \"planning\", display \"Code Planner\", id prefix \"cpm\"",
		"Choose the chosen entry-point's simple target for a simple goal.",
		"Choose the chosen entry-point's fan-out target for a fan-out or uncertain goal when it is listed.",
		"\"role_pair\": \"<chosen-simple-role-pair>\"",
		"\"role_pair\": \"<chosen-fan-out-role-pair>\"",
	})
}

func TestWakeInitialPlanningMissingMasterRendersSpecializedFallback(t *testing.T) {
	content := strings.ReplaceAll(string(embedded.PipelineConfigContent()), "      decomposition-root: true\n      decomposition-output-ref: plan_ref\n", "")
	content = strings.ReplaceAll(content, "      decomposition-root: true\n      decomposition-output-ref: arch_ref\n", "")
	projectRoot := setupPipelineConfigContent(t, []byte(content))
	rendered := renderInitialPlanningWake(t, projectRoot, "functional-spec")

	assertWakeContainsAll(t, rendered, []string{
		"Entry-point \"functional-spec\" was specified. Dispatch simple goals to Architect.",
		"SIMPLE GOAL TASK EXAMPLE",
		"\"id\": \"ar-1\"",
		"\"type\": \"architecture\"",
		"\"role_pair\": \"architecture-pair\"",
		"No mapped master planning role-pair is configured for this entry-point.",
		"Specialized fallback remains claimable by Architect.",
	})
	if got := countJSONRolePair(rendered, "architecture-pair"); got != 1 {
		t.Fatalf("specialized role_pair rendered %d times, want 1\n%s", got, rendered)
	}
	assertNotContainsAny(t, rendered, []string{
		"architecture-main-pair",
		"FAN-OUT GOAL TASK EXAMPLE",
		"\"id\": \"ar-2\"",
	})
}

// Initial planning owns objective-level classification only for direct or
// fallback specialized work. Mapped decomposition roots delegate independent
// classification to each output entry.
func TestWakeInitialPlanningRendersDefectClassification(t *testing.T) {
	fallbackContent := strings.ReplaceAll(string(embedded.PipelineConfigContent()), "      decomposition-root: true\n      decomposition-output-ref: plan_ref\n", "")
	fallbackContent = strings.ReplaceAll(fallbackContent, "      decomposition-root: true\n      decomposition-output-ref: arch_ref\n", "")

	tests := []struct {
		name       string
		entryPoint string
		root       func(t *testing.T) string
	}{
		{
			name:       "explicit entry point",
			entryPoint: "technical-spec",
			root:       setupPipelineConfig,
		},
		{
			name:       "inferred entry point",
			entryPoint: "",
			root:       setupPipelineConfig,
		},
		{
			name:       "missing master fallback",
			entryPoint: "functional-spec",
			root: func(t *testing.T) string {
				return setupPipelineConfigContent(t, []byte(fallbackContent))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := tt.root(t)
			rendered := renderInitialPlanningWake(t, projectRoot, tt.entryPoint)

			assertWakeContainsAll(t, rendered, []string{
				"DEFECT CLASSIFICATION:",
				"Classify the objective before dispatch.",
				"a bug, regression, or incident",
				"Direct or fallback specialized tasks",
				"Mapped decomposition-root tasks always use",
				"each output[] entry",
				`set "rca_required": true on that task`,
				`"rca_required": false,`,
				`The objective classification and RCA ownership boundary are stated`,
			})
		})
	}
}

func TestWakeInitialPlanningRejectsOldMultiTaskGuidance(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	for _, entryPoint := range []string{"", "general-objective", "functional-spec", "detailed-spec", "technical-spec"} {
		t.Run(entryPoint, func(t *testing.T) {
			rendered := renderInitialPlanningWake(t, projectRoot, entryPoint)

			assertNotContainsAny(t, rendered, []string{
				"MULTI-TASK PLANNING",
				"Create up to",
				"Create multiple parallel planning tasks",
				"multiple specialized planning tasks",
				"same role-pair",
				"Domain A",
				"Domain B",
				"domain-a",
				"domain-b",
				"\"id\": \"architecture-2\"",
				"\"id\": \"code-planning-2\"",
				"\"id\": \"epic-planning-2\"",
			})
		})
	}
}

func assertWakeContainsAll(t *testing.T, s string, wants []string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(s, want) {
			t.Fatalf("missing expected content %q\n%s", want, s)
		}
	}
}

func assertNotContainsAny(t *testing.T, s string, notWants []string) {
	t.Helper()
	for _, notWant := range notWants {
		if strings.Contains(s, notWant) {
			t.Fatalf("unexpected content %q\n%s", notWant, s)
		}
	}
}

func countJSONRolePair(s, rolePair string) int {
	return strings.Count(s, "\"role_pair\": \""+rolePair+"\"")
}
