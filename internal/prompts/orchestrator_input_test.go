package prompts

import (
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func dashboardInput(state *models.State) *OrchestratorDashboardInput {
	return &OrchestratorDashboardInput{State: state, AgentID: "orchestrator-1", WakeOpen: true}
}

// SelectOrchestratorWake is the standalone precedence over a resolved record;
// it reads nothing but the record and the snapshot.
func TestSelectOrchestratorWake(t *testing.T) {
	now := time.Now().UTC()
	merged := func(ids ...string) *models.State {
		state := testhelpers.CreateValidState()
		for _, id := range ids {
			state.Tasks = append(state.Tasks, testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now))
			state.Sprint.Scope.Planned = append(state.Sprint.Scope.Planned, id)
		}
		return state
	}
	withBase := func(state *models.State) *models.State {
		base := "base"
		state.Goal.BaseCommit = &base
		return state
	}
	plan := []PlanningTask{{TaskID: "plan-1"}}
	tests := []struct {
		name   string
		state  *models.State
		mutate func(*OrchestratorDashboardInput)
		want   WakeTrigger
	}{
		{"no tasks", func() *models.State { s := testhelpers.CreateValidState(); s.Tasks = nil; return s }(), nil, WakeInitialPlanning},
		{"actionable blocked outranks planning", merged("a"), func(in *OrchestratorDashboardInput) {
			in.ActionableBlockedIDs, in.PlanningTasks = []string{"a"}, plan
		}, WakeBlockedTasks},
		{"blocked waiting only for a hand-off yields planning complete", merged("a"), func(in *OrchestratorDashboardInput) {
			in.ActionableBlockedIDs, in.PlanningTasks, in.BlockedAwaitPlanningHandoff = []string{"a"}, plan, true
		}, WakePlanningComplete},
		{"hand-off exception needs a planning task", merged("a"), func(in *OrchestratorDashboardInput) {
			in.ActionableBlockedIDs, in.BlockedAwaitPlanningHandoff = []string{"a"}, true
		}, WakeBlockedTasks},
		{"actionable hypothesis exhaustion", merged("a"), func(in *OrchestratorDashboardInput) { in.ActionableHypothesisExhausted = 1 }, WakeHypothesisExhausted},
		{"unseen note", merged("a"), func(in *OrchestratorDashboardInput) { in.UnseenNotes = []models.HumanNote{{Message: "m"}} }, WakeHumanNote},
		{"planning tasks", merged("a"), func(in *OrchestratorDashboardInput) { in.PlanningTasks = plan }, WakePlanningComplete},
		{"ready many-to-one", merged("a"), func(in *OrchestratorDashboardInput) { in.ReadyManyToOne = 2 }, WakeManyToOneReady},
		{"sprint complete", merged("a"), nil, WakeSprintComplete},
		{"coding complete", withBase(merged("a")), nil, WakeCodingComplete},
		{"held plan keeps the sprint open", merged("a"), func(in *OrchestratorDashboardInput) { in.HeldPlan = true }, WakeUnknown},
		{"closed sprint is not complete", merged("a"), func(in *OrchestratorDashboardInput) { in.WakeOpen = false }, WakeNone},
		{"unterminated sprint", func() *models.State {
			s := merged("a")
			s.Tasks = append(s.Tasks, testhelpers.BuildTaskByStatus("b", models.TaskStatusReady, now))
			s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, "b")
			return s
		}(), nil, WakeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := dashboardInput(tt.state)
			if tt.mutate != nil {
				tt.mutate(in)
			}
			if got := SelectOrchestratorWake(in); got != tt.want {
				t.Fatalf("SelectOrchestratorWake = %q, want %q", got, tt.want)
			}
		})
	}
}

// The resolved entry-point flattens into the fields the INITIAL_PLANNING
// template reads; an absent route leaves them zero.
func TestOrchestratorDashboardInputFlattensResolvedRoute(t *testing.T) {
	ep := WakeEntryPoint{
		Name: "functional-spec", SimpleRolePair: "architecture-pair", SimpleDisplayName: "Architect",
		SimpleTaskType: "architecture", SimpleTaskIDPrefix: "ar", HasFanOutTarget: true,
		FanOutRolePair: "architecture-main-pair", FanOutDisplayName: "Architect Master",
		FanOutTaskType: "architecture", FanOutTaskIDPrefix: "arm",
	}
	state := testhelpers.CreateValidState()
	state.Goal.SpecRef, state.Goal.EntryPoint = "specs/goal.md", "functional-spec"
	in := dashboardInput(state)
	in.Routes = WakeRoutes{EntryPoints: []WakeEntryPoint{ep}, Resolved: &ep}
	in.TestFilePatterns = "*_test.go"

	data := in.wakeTemplateData(EffectiveIntegrationCompletion{Status: IntegrationViewWaiting})
	want := wakeTemplateData{
		GoalSpecRef: "specs/goal.md", GoalEntryPoint: "functional-spec",
		ResolvedRolePair: "architecture-pair", ResolvedDisplayName: "Architect", ResolvedTaskIDPrefix: "ar",
		ResolvedTaskType: "architecture", ResolvedEntryPoint: ep, ResolvedHasFanOutTarget: true,
		ResolvedFanOutRolePair: "architecture-main-pair", ResolvedFanOutDisplayName: "Architect Master",
		ResolvedFanOutTaskIDPrefix: "arm", ResolvedFanOutTaskType: "architecture",
		EntryPoints:      []WakeEntryPoint{ep},
		Integration:      EffectiveIntegrationCompletion{Status: IntegrationViewWaiting},
		TestFilePatterns: "*_test.go",
	}
	if data.ResolvedEntryPoint != want.ResolvedEntryPoint || data.ResolvedRolePair != want.ResolvedRolePair ||
		data.ResolvedDisplayName != want.ResolvedDisplayName || data.ResolvedTaskIDPrefix != want.ResolvedTaskIDPrefix ||
		data.ResolvedTaskType != want.ResolvedTaskType || data.ResolvedHasFanOutTarget != want.ResolvedHasFanOutTarget ||
		data.ResolvedFanOutRolePair != want.ResolvedFanOutRolePair || data.ResolvedFanOutDisplayName != want.ResolvedFanOutDisplayName ||
		data.ResolvedFanOutTaskIDPrefix != want.ResolvedFanOutTaskIDPrefix || data.ResolvedFanOutTaskType != want.ResolvedFanOutTaskType ||
		data.GoalSpecRef != want.GoalSpecRef || data.GoalEntryPoint != want.GoalEntryPoint ||
		data.TestFilePatterns != want.TestFilePatterns || data.Integration.Status != want.Integration.Status || len(data.EntryPoints) != 1 {
		t.Fatalf("flattened data = %+v, want %+v", data, want)
	}

	in.Routes = WakeRoutes{EntryPoints: []WakeEntryPoint{ep}}
	if bare := in.wakeTemplateData(EffectiveIntegrationCompletion{}); bare.ResolvedRolePair != "" || bare.ResolvedHasFanOutTarget || bare.ResolvedEntryPoint != (WakeEntryPoint{}) {
		t.Fatalf("an unresolved route must leave the resolved fields zero: %+v", bare)
	}
}

// Rendering reads only the record and the decision: the commands come from the
// embedded template under the runtime brand, the BLOCKED_TASKS scope is the
// supplied set, and the matcher text is the supplied text.
func TestRenderOrchestratorDashboardFromExplicitInput(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName, brand.NameTitle = "acme", "Acme"
		brand.ProjectDirName, brand.GlobalDirName = ".acme", ".acme"
	})
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{testhelpers.BuildTaskByStatus("blocked-1", models.TaskStatusBlocked, now)}

	in := dashboardInput(state)
	in.AgentID = "orchestrator-7"
	in.CycleBlocked = 2
	in.UnseenNotes = []models.HumanNote{{Message: "m"}, {Message: "n"}}
	in.ActionableBlockedIDs = []string{"blocked-1"}
	dashboard, instruction, err := RenderOrchestratorDashboard(in, OrchestratorWakeDecision{Trigger: WakeBlockedTasks})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"WAKE TRIGGER: BLOCKED_TASKS\n", "- Blocked: 1\n", "- Unseen operator notes: 2\n", "- Cycle-blocked planning: 2\n",
		"\nORCHESTRATOR COMMANDS:\n- acme add-tasks — Add one or more tasks",
		`acme add-tasks --tasks-file <path.json> --agent-id "orchestrator-7" --json`,
		"create them in topological order — dependency-free tasks first, then tasks that depend on them. acme add-tasks validates that all `depends` IDs",
		"Do NOT exit until all commands have been run.\n\t",
	} {
		if !strings.Contains(dashboard, want) {
			t.Errorf("dashboard missing %q:\n%s", want, dashboard)
		}
	}
	if strings.Contains(dashboard+instruction, "liza") || strings.Contains(dashboard+instruction, "Liza") {
		t.Error("render leaked the default brand")
	}
	if !strings.HasSuffix(dashboard, "run.\n\t") || !strings.HasPrefix(instruction, "INSTRUCTIONS:\n") {
		t.Errorf("dashboard must end with the frozen commands text and the instruction must carry its prefix; ends %q", dashboard[len(dashboard)-12:])
	}
	if !strings.Contains(instruction, "blocked-1") {
		t.Errorf("instruction must name the supplied scope:\n%s", instruction)
	}

	in.ActionableBlockedIDs = nil
	_, empty, err := RenderOrchestratorDashboard(in, OrchestratorWakeDecision{Trigger: WakeBlockedTasks})
	if err != nil {
		t.Fatal(err)
	}
	stateless, err := RenderWakeInstructions(WakeBlockedTasks, "orchestrator-7", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(empty, "Record no assessment") || strings.Contains(stateless, "Record no assessment") {
		t.Errorf("a computed empty scope means no assessment; the stateless nil scope does not:\nempty: %s\nstateless: %s", empty, stateless)
	}

	in.State.Sprint.Scope.Planned = []string{"plan-1"}
	in.TestFilePatterns = "*_marker.go, *.spec.x"
	in.PlanningTasks = []PlanningTask{{TaskID: "plan-1", Class: models.PlanHandoffNeedsReview, PlanRefs: []string{"specs/plan.md"}}}
	_, planning, err := RenderOrchestratorDashboard(in, OrchestratorWakeDecision{Trigger: WakePlanningComplete})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(planning, "test-file pattern (*_marker.go, *.spec.x).") || !strings.Contains(planning, "- plan-1: specs/plan.md") {
		t.Errorf("planning wake must render the supplied matcher text and task:\n%s", planning)
	}
}

// The typed vocabulary keeps the strings the templates, status output and
// persisted comparisons spell.
func TestWakeVocabularyValues(t *testing.T) {
	for constant, want := range map[WakeTrigger]string{
		WakeInitialPlanning: "INITIAL_PLANNING", WakeBlockedTasks: "BLOCKED_TASKS", WakeHypothesisExhausted: "HYPOTHESIS_EXHAUSTED",
		WakeImmediateDiscovery: "IMMEDIATE_DISCOVERY", WakeHumanNote: "HUMAN_NOTE", WakePlanningComplete: "PLANNING_COMPLETE",
		WakeManyToOneReady: "MANY_TO_ONE_READY", WakeCodingComplete: "CODING_COMPLETE", WakeSprintComplete: "SPRINT_COMPLETE",
		WakeIntegrationWaiting: "INTEGRATION_WAITING", WakeIntegrationBlocked: "INTEGRATION_BLOCKED",
		WakeIntegrationExhausted: "INTEGRATION_EXHAUSTED", WakeIntegrationUnavailable: "INTEGRATION_UNAVAILABLE",
		WakeUnknown: "UNKNOWN", WakeNone: "NONE",
	} {
		if string(constant) != want {
			t.Errorf("wake trigger %q, want %q", constant, want)
		}
	}
	for constant, want := range map[IntegrationViewStatus]string{
		IntegrationViewUnavailable: "unavailable", IntegrationViewComplete: "complete", IntegrationViewBlocked: "blocked",
		IntegrationViewExhausted: "exhausted", IntegrationViewReconciliationNeeded: "reconciliation_needed", IntegrationViewWaiting: "waiting",
	} {
		if string(constant) != want {
			t.Errorf("integration status %q, want %q", constant, want)
		}
	}
	if ReasonIntegrationProgressUnavailable != "integration_progress_unavailable" || ReasonIntegrationProgressIncomplete != "integration_progress_incomplete" {
		t.Error("integration view reason codes changed")
	}
	if activeTaskDigestLimit != 12 || blockerDigestLimit != 220 || hypothesisExhaustionFailures != 2 || integrationRolePair != "integration-pair" {
		t.Error("dashboard digest limits and the integration role-pair must keep their values")
	}
}
