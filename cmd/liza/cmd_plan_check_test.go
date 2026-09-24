package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func setupPlanCheckCLI(t *testing.T) string {
	t.Helper()
	resetRootCmdForTest(t)
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	t.Setenv(brand.LegacyEnvName("AGENT_ID"), "")
	root := t.TempDir()
	testhelpers.SetupTestGitRepo(t, root)
	statePath, _ := testhelpers.SetupLizaDir(t, root)
	testhelpers.CreateSpecFile(t, root, "vision.md", "# Vision\n")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# README\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := testhelpers.CreateValidState()
	state.Agents["orchestrator-1"] = testhelpers.RegisteredTestAgent("orchestrator")
	plan := testhelpers.BuildTaskByStatus("plan-1", models.TaskStatusMerged, time.Now().UTC())
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "tests pass", Scope: "pkg/x", SpecRef: "README.md"}}
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{"plan-1"}
	testhelpers.WriteInitialState(t, statePath, state)
	return root
}

func TestPlanCheckCLI_HoldThenOperatorClear(t *testing.T) {
	root := setupPlanCheckCLI(t)
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)

	stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--hold", "inject smoke credentials", "--agent-id", "orchestrator-1", "--json")
	if err != nil {
		t.Fatalf("hold: %v (%s)", err, stdout)
	}
	result := parseEnvelope(t, stdout)["result"].(map[string]any)
	if result["verdict"] != "held" || result["class"] != "held" || result["changed"] != true {
		t.Fatalf("hold result = %v", result)
	}

	// An agent session cannot release a human hold.
	t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
	resetRootCmdForTest(t)
	if stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--clear", "--json"); err == nil || !strings.Contains(stdout, "operator-only") {
		t.Fatalf("clear from an agent session = %v (%s), want operator-only refusal", err, stdout)
	}
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	resetRootCmdForTest(t)
	if stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--clear", "--json"); err != nil {
		t.Fatalf("operator clear: %v (%s)", err, stdout)
	}
	state, err := db.For(paths.New(root).StatePath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.FindTask("plan-1").PlanCheck != nil {
		t.Fatal("plan_check not cleared")
	}
}

func TestPlanCheckCLI_RequiresExactlyOneAction(t *testing.T) {
	for _, args := range [][]string{
		{"plan-check", "plan-1", "--json"},
		{"plan-check", "plan-1", "--pass", "--clear", "--json"},
		{"plan-check", "plan-1", "--hold", " ", "--json"},
	} {
		root := setupPlanCheckCLI(t)
		stdout, err := executeRootCommandCapture(t, root, args...)
		if err == nil || !strings.Contains(stdout, `"code":"validation"`) {
			t.Errorf("%v: err=%v stdout=%s, want a validation error", args, err, stdout)
		}
		state, readErr := db.For(paths.New(root).StatePath()).Read()
		if readErr != nil || state.FindTask("plan-1").PlanCheck != nil {
			t.Errorf("%v: plan_check written on invalid input (%v)", args, readErr)
		}
	}
}
