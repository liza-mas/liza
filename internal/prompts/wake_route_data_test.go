package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/embedded"
)

func TestBuildWakeTemplateDataRouteData(t *testing.T) {
	projectRoot := setupPipelineConfig(t)

	data, err := buildWakeTemplateData("specs/goal.md", "", projectRoot)
	if err != nil {
		t.Fatalf("buildWakeTemplateData: %v", err)
	}

	entryPoints := wakeEntryPointsByName(t, data.EntryPoints)
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
			simpleIDPrefix: "epic-planning",
			fanOutRolePair: "epic-planning-main-pair",
			fanOutTaskType: "epic-planning",
			fanOutDisplay:  "Epic Planner",
			fanOutIDPrefix: "epic-planning-main",
		},
		{
			name:           "functional-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleDisplay:  "Architect",
			simpleIDPrefix: "architecture",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutDisplay:  "Architect",
			fanOutIDPrefix: "architecture-main",
		},
		{
			name:           "detailed-spec",
			simpleRolePair: "architecture-pair",
			simpleTaskType: "architecture",
			simpleDisplay:  "Architect",
			simpleIDPrefix: "architecture",
			fanOutRolePair: "architecture-main-pair",
			fanOutTaskType: "architecture",
			fanOutDisplay:  "Architect",
			fanOutIDPrefix: "architecture-main",
		},
		{
			name:           "technical-spec",
			simpleRolePair: "code-planning-pair",
			simpleTaskType: "planning",
			simpleDisplay:  "Code Planner",
			simpleIDPrefix: "code-planning",
			fanOutRolePair: "code-planning-main-pair",
			fanOutTaskType: "planning",
			fanOutDisplay:  "Code Planner",
			fanOutIDPrefix: "code-planning-main",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := entryPoints[tt.name]
			assertWakeRouteData(t, ep, tt.simpleRolePair, tt.simpleTaskType, tt.simpleDisplay, tt.simpleIDPrefix, tt.fanOutRolePair, tt.fanOutTaskType, tt.fanOutDisplay, tt.fanOutIDPrefix)

			explicit, err := buildWakeTemplateData("specs/goal.md", tt.name, projectRoot)
			if err != nil {
				t.Fatalf("buildWakeTemplateData explicit %q: %v", tt.name, err)
			}
			if explicit.ResolvedEntryPoint.Name != tt.name {
				t.Fatalf("ResolvedEntryPoint.Name = %q, want %q", explicit.ResolvedEntryPoint.Name, tt.name)
			}
			assertWakeRouteData(t, explicit.ResolvedEntryPoint, tt.simpleRolePair, tt.simpleTaskType, tt.simpleDisplay, tt.simpleIDPrefix, tt.fanOutRolePair, tt.fanOutTaskType, tt.fanOutDisplay, tt.fanOutIDPrefix)
			if explicit.ResolvedRolePair != tt.simpleRolePair {
				t.Errorf("ResolvedRolePair = %q, want %q", explicit.ResolvedRolePair, tt.simpleRolePair)
			}
			if explicit.ResolvedTaskType != tt.simpleTaskType {
				t.Errorf("ResolvedTaskType = %q, want %q", explicit.ResolvedTaskType, tt.simpleTaskType)
			}
			if explicit.ResolvedDisplayName != tt.simpleDisplay {
				t.Errorf("ResolvedDisplayName = %q, want %q", explicit.ResolvedDisplayName, tt.simpleDisplay)
			}
			if explicit.ResolvedTaskIDPrefix != tt.simpleIDPrefix {
				t.Errorf("ResolvedTaskIDPrefix = %q, want %q", explicit.ResolvedTaskIDPrefix, tt.simpleIDPrefix)
			}
			if !explicit.ResolvedHasFanOutTarget {
				t.Fatal("ResolvedHasFanOutTarget = false, want true")
			}
			if explicit.ResolvedFanOutRolePair != tt.fanOutRolePair {
				t.Errorf("ResolvedFanOutRolePair = %q, want %q", explicit.ResolvedFanOutRolePair, tt.fanOutRolePair)
			}
			if explicit.ResolvedFanOutTaskType != tt.fanOutTaskType {
				t.Errorf("ResolvedFanOutTaskType = %q, want %q", explicit.ResolvedFanOutTaskType, tt.fanOutTaskType)
			}
			if explicit.ResolvedFanOutDisplayName != tt.fanOutDisplay {
				t.Errorf("ResolvedFanOutDisplayName = %q, want %q", explicit.ResolvedFanOutDisplayName, tt.fanOutDisplay)
			}
			if explicit.ResolvedFanOutTaskIDPrefix != tt.fanOutIDPrefix {
				t.Errorf("ResolvedFanOutTaskIDPrefix = %q, want %q", explicit.ResolvedFanOutTaskIDPrefix, tt.fanOutIDPrefix)
			}
		})
	}
}

func TestBuildWakeTemplateDataMissingFanOutMapping(t *testing.T) {
	content := strings.ReplaceAll(string(embedded.PipelineConfigContent()), "      decomposition-root: true\n", "")
	projectRoot := setupPipelineConfigContent(t, []byte(content))

	data, err := buildWakeTemplateData("specs/goal.md", "functional-spec", projectRoot)
	if err != nil {
		t.Fatalf("buildWakeTemplateData: %v", err)
	}

	for _, ep := range data.EntryPoints {
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
	if data.ResolvedEntryPoint.HasFanOutTarget {
		t.Fatal("ResolvedEntryPoint.HasFanOutTarget = true, want false")
	}
	if data.ResolvedFanOutRolePair != "" {
		t.Fatalf("ResolvedFanOutRolePair = %q, want empty", data.ResolvedFanOutRolePair)
	}
}

func TestBuildWakeTemplateDataPropagatesResolverErrors(t *testing.T) {
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

	_, err := buildWakeTemplateData("specs/goal.md", "", projectRoot)
	if err == nil {
		t.Fatal("buildWakeTemplateData: expected resolver error, got nil")
	}
	if !strings.Contains(err.Error(), "multiple decomposition roots") {
		t.Fatalf("buildWakeTemplateData error = %q, want multiple decomposition roots", err.Error())
	}
}

func wakeEntryPointsByName(t *testing.T, entryPoints []wakeEntryPointData) map[string]wakeEntryPointData {
	t.Helper()
	got := make(map[string]wakeEntryPointData, len(entryPoints))
	for _, ep := range entryPoints {
		got[ep.Name] = ep
	}
	if len(got) != 4 {
		t.Fatalf("entry point count = %d, want 4", len(got))
	}
	return got
}

func assertWakeRouteData(t *testing.T, ep wakeEntryPointData, simpleRolePair, simpleTaskType, simpleDisplay, simpleIDPrefix, fanOutRolePair, fanOutTaskType, fanOutDisplay, fanOutIDPrefix string) {
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
	if ep.RolePair != simpleRolePair {
		t.Errorf("RolePair alias = %q, want %q", ep.RolePair, simpleRolePair)
	}
	if ep.TaskType != simpleTaskType {
		t.Errorf("TaskType alias = %q, want %q", ep.TaskType, simpleTaskType)
	}
	if ep.DisplayName != simpleDisplay {
		t.Errorf("DisplayName alias = %q, want %q", ep.DisplayName, simpleDisplay)
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
	lizaDir := filepath.Join(dir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("mkdir .liza: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), content, 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}
	return dir
}
