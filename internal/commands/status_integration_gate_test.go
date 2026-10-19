package commands

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/render"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D-225: status shows the live integration gate even when another wake
// trigger wins, bounded for humans and complete for machines.
func TestStatusShowsIntegrationGateIndependentlyOfWakeTrigger(t *testing.T) {
	// GIVEN twelve merged stories holding planning and a BLOCKED task that wins the wake
	projectRoot := t.TempDir()
	testhelpers.SetupTestGitRepo(t, projectRoot)
	testhelpers.SetupPipelineConfig(t, projectRoot)
	state := testhelpers.CreateValidState()
	var storyIDs []string
	for i := 1; i <= 12; i++ {
		story := testhelpers.BuildTaskByStatus(fmt.Sprintf("story-%02d", i), models.TaskStatusMerged, time.Now().UTC())
		story.RolePair = "us-writing-pair"
		state.Tasks = append(state.Tasks, story)
		storyIDs = append(storyIDs, story.ID)
	}
	blocked := testhelpers.BuildTaskByStatus("blocked-task", models.TaskStatusBlocked, time.Now().UTC())
	state.Tasks = append(state.Tasks, blocked)
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	// WHEN status is built and rendered
	data := BuildStatusData(state, false, projectRoot, pr, nil)
	dashboard, err := formatStatusDashboard(data)
	if err != nil {
		t.Fatal(err)
	}
	jsonOutput, err := render.FormatJSON(data)
	if err != nil {
		t.Fatal(err)
	}

	// THEN the dashboard has a bounded INTEGRATION section beside the winning trigger
	if data.OrchestratorState.Trigger != "BLOCKED_TASKS" {
		t.Fatalf("fixture wake trigger = %q, want BLOCKED_TASKS", data.OrchestratorState.Trigger)
	}
	section := renderedSection(t, dashboard, "=== INTEGRATION ===")
	for _, want := range append([]string{"waiting", "planning_unsettled", "(+2 more)", "--retire --ruling"}, storyIDs[:10]...) {
		if !strings.Contains(section, want) {
			t.Errorf("INTEGRATION section missing %q:\n%s", want, section)
		}
	}
	for _, hidden := range storyIDs[10:] {
		if strings.Contains(section, hidden) {
			t.Errorf("INTEGRATION section lists %q beyond the bound:\n%s", hidden, section)
		}
	}
	// AND the JSON carries every ID
	var decoded struct {
		Integration struct {
			ReasonCode string   `json:"reason_code"`
			TaskIDs    []string `json:"task_ids"`
		} `json:"integration"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Integration.ReasonCode != "planning_unsettled" || strings.Join(decoded.Integration.TaskIDs, ",") != strings.Join(storyIDs, ",") {
		t.Fatalf("JSON integration = %+v, want planning_unsettled with all %d stories", decoded.Integration, len(storyIDs))
	}
}

func TestStatusIntegrationGateRendersUnavailableEvaluation(t *testing.T) {
	// GIVEN a project without an integration branch
	projectRoot := t.TempDir()
	testhelpers.SetupPipelineConfig(t, projectRoot)
	state := testhelpers.CreateValidState()
	pr, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatal(err)
	}

	// WHEN status is rendered
	dashboard, err := formatStatusDashboard(BuildStatusData(state, false, projectRoot, pr, nil))
	if err != nil {
		t.Fatal(err)
	}

	// THEN the section reports the evaluation as unavailable
	section := renderedSection(t, dashboard, "=== INTEGRATION ===")
	for _, want := range []string{"unavailable", "integration_progress_unavailable"} {
		if !strings.Contains(section, want) {
			t.Errorf("INTEGRATION section missing %q:\n%s", want, section)
		}
	}
}

func renderedSection(t *testing.T, dashboard, header string) string {
	t.Helper()
	start := strings.Index(dashboard, header)
	if start < 0 {
		t.Fatalf("dashboard has no %s section:\n%s", header, dashboard)
	}
	section := dashboard[start+len(header):]
	if end := strings.Index(section, "\n==="); end >= 0 {
		section = section[:end]
	}
	return section
}
