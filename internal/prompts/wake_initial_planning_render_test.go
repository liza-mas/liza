package prompts

import (
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
)

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
			simpleIDPrefix: "epic-planning",
			simpleDisplay:  "Epic Planner",
			fanOutRolePair: "epic-planning-main-pair",
			fanOutTaskType: "epic-planning",
			fanOutIDPrefix: "epic-planning-main",
			fanOutDisplay:  "Epic Planner",
		},
		{
			name:           "functional spec",
			entryPoint:     "functional-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleIDPrefix: "architecture",
			simpleDisplay:  "Architect",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutIDPrefix: "architecture-main",
			fanOutDisplay:  "Architect",
		},
		{
			name:           "detailed spec",
			entryPoint:     "detailed-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleIDPrefix: "architecture",
			simpleDisplay:  "Architect",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutIDPrefix: "architecture-main",
			fanOutDisplay:  "Architect",
		},
		{
			name:           "technical spec",
			entryPoint:     "technical-spec",
			simpleRolePair: "code-planning-pair",
			simpleTaskType: "planning",
			simpleIDPrefix: "code-planning",
			simpleDisplay:  "Code Planner",
			fanOutRolePair: "code-planning-main-pair",
			fanOutTaskType: "planning",
			fanOutIDPrefix: "code-planning-main",
			fanOutDisplay:  "Code Planner",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := buildWakeTemplateData("specs/goal.md", tt.entryPoint, projectRoot)
			if err != nil {
				t.Fatalf("buildWakeTemplateData: %v", err)
			}
			rendered, err := buildInstructionsForWakeTrigger("INITIAL_PLANNING", "orchestrator-1", data, nil)
			if err != nil {
				t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
			}

			assertContainsAll(t, rendered, []string{
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
	data, err := buildWakeTemplateData("specs/goal.md", "", projectRoot)
	if err != nil {
		t.Fatalf("buildWakeTemplateData: %v", err)
	}
	rendered, err := buildInstructionsForWakeTrigger("INITIAL_PLANNING", "orchestrator-1", data, nil)
	if err != nil {
		t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
	}

	assertContainsAll(t, rendered, []string{
		"- \"general-objective\": simple role_pair \"epic-planning-pair\", type \"epic-planning\", display \"Epic Planner\", id prefix \"epic-planning\"; fan-out role_pair \"epic-planning-main-pair\", type \"epic-planning\", display \"Epic Planner\", id prefix \"epic-planning-main\"",
		"- \"functional-spec\": simple role_pair \"architecture-pair\", type \"architecture\", display \"Architect\", id prefix \"architecture\"; fan-out role_pair \"architecture-main-pair\", type \"architecture\", display \"Architect\", id prefix \"architecture-main\"",
		"- \"detailed-spec\": simple role_pair \"architecture-pair\", type \"architecture\", display \"Architect\", id prefix \"architecture\"; fan-out role_pair \"architecture-main-pair\", type \"architecture\", display \"Architect\", id prefix \"architecture-main\"",
		"- \"technical-spec\": simple role_pair \"code-planning-pair\", type \"planning\", display \"Code Planner\", id prefix \"code-planning\"; fan-out role_pair \"code-planning-main-pair\", type \"planning\", display \"Code Planner\", id prefix \"code-planning-main\"",
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
	data, err := buildWakeTemplateData("specs/goal.md", "functional-spec", projectRoot)
	if err != nil {
		t.Fatalf("buildWakeTemplateData: %v", err)
	}
	rendered, err := buildInstructionsForWakeTrigger("INITIAL_PLANNING", "orchestrator-1", data, nil)
	if err != nil {
		t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
	}

	assertContainsAll(t, rendered, []string{
		"Entry-point \"functional-spec\" was specified. Dispatch simple goals to Architect.",
		"SIMPLE GOAL TASK EXAMPLE",
		"\"id\": \"architecture-1\"",
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
		"\"id\": \"architecture-2\"",
	})
}

func TestWakeInitialPlanningRejectsOldMultiTaskGuidance(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	for _, entryPoint := range []string{"", "general-objective", "functional-spec", "detailed-spec", "technical-spec"} {
		t.Run(entryPoint, func(t *testing.T) {
			data, err := buildWakeTemplateData("specs/goal.md", entryPoint, projectRoot)
			if err != nil {
				t.Fatalf("buildWakeTemplateData: %v", err)
			}
			rendered, err := buildInstructionsForWakeTrigger("INITIAL_PLANNING", "orchestrator-1", data, nil)
			if err != nil {
				t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
			}

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

func assertContainsAll(t *testing.T, s string, wants []string) {
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
