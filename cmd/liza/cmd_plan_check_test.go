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

func TestPlanCheckCLI_RetiresOriginalByMergedCorrection(t *testing.T) {
	root := setupPlanCheckCLI(t)
	bb := db.For(paths.New(root).StatePath())
	if err := bb.Modify(func(state *models.State) error {
		correction := state.Tasks[0]
		correction.ID = "correction"
		correction.DependsOn = []string{"plan-1"}
		state.Tasks = append(state.Tasks, correction)
		state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, correction.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--replaced-by", "correction", "--json")
	if err != nil {
		t.Fatalf("operator retirement: %v (%s)", err, stdout)
	}
	result := parseEnvelope(t, stdout)["result"].(map[string]any)
	if result["verdict"] != "replaced" || result["changed"] != true {
		t.Fatalf("retirement result = %v", result)
	}
	state, err := bb.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.FindTask("plan-1").Status != models.TaskStatusMerged ||
		len(state.FindTask("plan-1").TransitionsExecuted) != 0 ||
		len(state.FindTask("correction").DependsOn) != 1 || state.FindTask("correction").DependsOn[0] != "plan-1" {
		t.Fatal("retirement changed status, pretended expansion, or rewrote dependencies")
	}
}

func TestPlanCheckCLI_RetiresHeldPlanByRuling(t *testing.T) {
	root := setupPlanCheckCLI(t)
	t.Setenv(brand.EnvName("AGENT_GENERATION"), testhelpers.TestAgentGeneration)
	if stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--hold", "retire per D83", "--agent-id", "orchestrator-1", "--json"); err != nil {
		t.Fatalf("hold: %v (%s)", err, stdout)
	}

	// An agent session cannot retire a hand-off.
	t.Setenv(brand.EnvName("AGENT_ID"), "orchestrator-1")
	resetRootCmdForTest(t)
	if stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--retire", "--ruling", "D83", "--json"); err == nil || !strings.Contains(stdout, "operator-only") {
		t.Fatalf("retire from an agent session = %v (%s), want operator-only refusal", err, stdout)
	}
	t.Setenv(brand.EnvName("AGENT_ID"), "")
	resetRootCmdForTest(t)
	stdout, err := executeRootCommandCapture(t, root, "plan-check", "plan-1", "--retire", "--ruling", "D83", "--json")
	if err != nil {
		t.Fatalf("operator retire: %v (%s)", err, stdout)
	}
	result := parseEnvelope(t, stdout)["result"].(map[string]any)
	if result["verdict"] != "retired" || result["ruling"] != "D83" || result["changed"] != true {
		t.Fatalf("retire result = %v", result)
	}
	state, err := db.For(paths.New(root).StatePath()).Read()
	if err != nil {
		t.Fatal(err)
	}
	plan := state.FindTask("plan-1")
	if plan.Status != models.TaskStatusMerged || len(plan.TransitionsExecuted) != 0 || plan.PlanCheck.Ask != "" {
		t.Fatalf("retire changed status, pretended expansion, or kept the hold: %+v", plan.PlanCheck)
	}
}

func TestPlanCheckCLI_OperatorActionsRejectExplicitAgentID(t *testing.T) {
	for _, action := range [][]string{
		{"--clear"},
		{"--replaced-by", "correction"},
		{"--retire", "--ruling", "D83"},
	} {
		for _, agentID := range []string{"orchestrator-1", ""} {
			t.Run(strings.Join(action, " ")+"/agent="+agentID, func(t *testing.T) {
				root := setupPlanCheckCLI(t)
				bb := db.For(paths.New(root).StatePath())
				before, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				args := append([]string{"plan-check", "plan-1"}, action...)
				args = append(args, "--agent-id", agentID, "--json")
				stdout, err := executeRootCommandCapture(t, root, args...)
				if err == nil || !strings.Contains(stdout, "operator-only") {
					t.Fatalf("%v: err=%v stdout=%s, want operator-only refusal", args, err, stdout)
				}
				after, err := bb.Read()
				if err != nil {
					t.Fatal(err)
				}
				if after.MutationSequence != before.MutationSequence || after.FindTask("plan-1").PlanCheck != nil {
					t.Fatal("refused operator action changed state")
				}
			})
		}
	}
}

func TestPlanCheckCLI_RetireRequiresRuling(t *testing.T) {
	for _, args := range [][]string{
		{"plan-check", "plan-1", "--retire", "--json"},
		{"plan-check", "plan-1", "--retire", "--ruling", " ", "--json"},
		{"plan-check", "plan-1", "--clear", "--ruling", "D83", "--json"},
		{"plan-check", "plan-1", "--retire", "--ruling", "D83", "--clear", "--json"},
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
