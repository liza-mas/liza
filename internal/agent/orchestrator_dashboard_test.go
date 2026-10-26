package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestRenderOrchestratorDashboard(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)
	replacementLineagePolicy := []string{
		"Preserve an independently reviewable replacement lineage for each independently scoped original",
		"one original may split into several cohesive replacements",
		"Do not map several independent originals to one shared replacement or replan",
		"A shared replan is allowed only when corrections are semantically inseparable",
		"Record an explicit reason explaining why; convenience or a shared incident is insufficient",
		"Reason replacement dependencies within each preserved lineage, not from the shared incident",
		"Later specialized outputs do not restore collapsed supersession provenance",
	}

	tests := []struct {
		name           string
		state          *models.State
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "initial planning trigger (no tasks)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{}
				return state
			}(),
			wantContains: []string{
				"=== ORCHESTRATOR CONTEXT ===",
				"WAKE TRIGGER: INITIAL_PLANNING",
				"SPRINT STATE:",
				"- Total tasks: 0",
				"- Merged: 0",
				"- In progress: 0",
				"- Unclaimed: 0",
				"- Blocked: 0",
				"- Integration failed: 0",
				"- Hypothesis exhausted: 0",
				"- Immediate discoveries: 0",
				"ORCHESTRATOR COMMANDS:",
				brand.BinaryName + " add-tasks",
				brand.BinaryName + " assess-blocked",
				brand.BinaryName + " supersede-task",
				brand.BinaryName + " wt-delete",
				brand.BinaryName + " supersede-task <task-id> [replacement-task-ids] --reason \"...\" --agent-id \"orchestrator-1\" --json",
				brand.BinaryName + " supersede-task <task-id> --reason \"Work completed externally\" --recoverability-command \"" + brand.BinaryName + " recover-task <task-id>\" --agent-id \"orchestrator-1\" --json",
				brand.BinaryName + " wt-delete <task-id> --json",
				brand.BinaryName + " sprint-checkpoint — Create sprint checkpoint for human review",
				brand.BinaryName + " update-sprint-metrics — Recompute sprint metrics",
				"This is initial planning",
				"Classify the input document and choose the appropriate entry-point",
				"AVAILABLE ENTRY-POINTS:",
				"Create exactly one planning task",
				"Choose the chosen entry-point's fan-out target for a fan-out or uncertain goal when it is listed.",
				"Fan-out or uncertain goals use the chosen fan-out role_pair when available.",
			},
			wantNotContain: []string{
				`--replacement-ids`,
				brand.BinaryName + " wt-delete <task-id> --agent-id",
			},
		},
		{
			name: "blocked tasks trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReady, now),
				}
				return state
			}(),
			wantContains: append([]string{
				"WAKE TRIGGER: BLOCKED_TASKS",
				"- Total tasks: 2",
				"- Blocked: 1",
				"Tasks are BLOCKED. Analyze and resolve immediately:",
				brand.BinaryName + " get human_notes --json",
				"Read operator input before recording another assessment",
				"a claim failure may occur before BLOCKED",
				"A note grants no approval or permission to bypass review",
				"Read the listed tasks from blackboard",
				brand.BinaryName + " replace-task --replacement-file <path.json>",
				"changed (what differs from the blocked attempt)",
				"Never add-tasks the replacements first",
				brand.BinaryName + " assess-blocked",
				"Before either stored apply-dependency-repair or direct retarget-dependency operation",
				"Re-read every affected task and its planning/decomposition context",
				"Verify provider-before-consumer direction for every proposed edge",
				"For any inverse edge, confirm the stored repair request or direct repair reason identifies the explicit relationship",
				"Do not run either repair operation until this shared semantic gate passes",
				"The repair reason must identify the verified direction or name the explicit relationship that justifies an inverse edge",
				"If graph repair leaves a current blocker, reconcile its canonical metadata",
				brand.BinaryName + " assess-blocked <task-id> --reason \"<current blocker>\" --question \"<current question>\"",
				"If graph repair resolves every blocker, use " + brand.BinaryName + " unblock-task",
			}, replacementLineagePolicy...),
			// The two-step add-tasks + supersede-task example escaped the cap (ADR-0171).
			wantNotContain: []string{"Write a tasks JSON file as a bare array", brand.BinaryName + " supersede-task <task-id> <new-id-1>"},
		},
		{
			name: "hypothesis exhaustion trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantContains: append([]string{
				"WAKE TRIGGER: HYPOTHESIS_EXHAUSTED",
				"- Hypothesis exhausted: 1",
				"Multiple coders failed on same task. Re-evaluate and act NOW:",
				brand.BinaryName + " replace-task --replacement-file <path.json>",
				"changed (what differs from the failed attempts)",
				"Never add-tasks the replacements first",
			}, replacementLineagePolicy...),
			wantNotContain: []string{"Write a tasks JSON file as a bare array", brand.BinaryName + " supersede-task <old-id> <new-id-1>"},
		},
		{
			name: "immediate discovery trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				// Need at least one task to avoid INITIAL_PLANNING trigger
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				state.Discovered = []models.Discovery{
					{
						ID:             "disc-1",
						By:             "coder-1",
						During:         "task-1",
						Description:    "Critical bug",
						Severity:       "critical",
						Urgency:        "immediate",
						Recommendation: "Fix immediately",
						Created:        now,
					},
				}
				return state
			}(),
			wantContains: append([]string{
				"WAKE TRIGGER: IMMEDIATE_DISCOVERY",
				"- Immediate discoveries: 1",
				"Urgent discoveries need immediate action:",
			}, replacementLineagePolicy...),
		},
		{
			name: "mixed task statuses (in progress calculation)",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusImplementing, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusReadyForReview, now),
					testhelpers.BuildTaskByStatus("task-3", models.TaskStatusApproved, now),
					testhelpers.BuildTaskByStatus("task-4", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-5", models.TaskStatusReady, now),
				}
				return state
			}(),
			wantContains: []string{
				"- Total tasks: 5",
				"- Merged: 1",
				"- In progress: 3", // IMPLEMENTING + READY_FOR_REVIEW + APPROVED
				"- Unclaimed: 1",
			},
		},
		{
			name: "planning complete trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-plan-1", "task-code-1"}
				planningTask := testhelpers.BuildTaskByStatus("task-plan-1", models.TaskStatusMerged, now)
				planningTask.RolePair = "code-planning-pair"
				planningTask.Output = []models.OutputEntry{
					{Desc: "Implement auth", DoneWhen: "Auth works", Scope: "internal/auth"},
				}
				codingTask := testhelpers.BuildTaskByStatus("task-code-1", models.TaskStatusMerged, now)
				state.Tasks = []models.Task{planningTask, codingTask}
				return state
			}(),
			wantContains: []string{
				"WAKE TRIGGER: PLANNING_COMPLETE",
				"- Total tasks: 2",
				"- Merged: 2",
				"Planning tasks have been merged with output[]. You own their hand-off",
				"automatic paths create children only from plans you pass",
				brand.BinaryName + " sprint-checkpoint",
			},
		},
		{
			name: "partial planning complete trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-plan-1", "task-plan-2"}
				planningTask := testhelpers.BuildTaskByStatus("task-plan-1", models.TaskStatusMerged, now)
				planningTask.RolePair = "code-planning-pair"
				planningTask.Output = []models.OutputEntry{
					{Desc: "Implement auth", DoneWhen: "Auth works", Scope: "internal/auth"},
				}
				activeTask := testhelpers.BuildTaskByStatus("task-plan-2", models.TaskStatusDraftCodingPlan, now)
				activeTask.RolePair = "code-planning-pair"
				state.Tasks = []models.Task{planningTask, activeTask}
				return state
			}(),
			wantContains: []string{
				"WAKE TRIGGER: PLANNING_COMPLETE",
				"- Total tasks: 2",
				"- Merged: 1",
				"Planning tasks have been merged with output[]. You own their hand-off",
				"PLANS TO REVIEW:",
				"Create child tasks directly or edit merged plan files yourself — amend-plan/replan route material corrections to ordinary planner/reviewer review.",
			},
			wantNotContain: []string{
				"WAKE TRIGGER: UNKNOWN",
			},
		},
		{
			name: "checkpoint suppresses planning complete trigger",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Status = models.SprintStatusCheckpoint
				state.Sprint.Scope.Planned = []string{"task-plan-1"}
				planningTask := testhelpers.BuildTaskByStatus("task-plan-1", models.TaskStatusMerged, now)
				planningTask.RolePair = "code-planning-pair"
				planningTask.Output = []models.OutputEntry{
					{Desc: "Implement auth", DoneWhen: "Auth works", Scope: "internal/auth"},
				}
				state.Tasks = []models.Task{planningTask}
				return state
			}(),
			wantContains: []string{
				"WAKE TRIGGER: NONE",
				"- Total tasks: 1",
				"- Merged: 1",
			},
			wantNotContain: []string{
				"WAKE TRIGGER: PLANNING_COMPLETE",
				"WAKE TRIGGER: SPRINT_COMPLETE",
				"Planning tasks have been merged with output[]. You own their hand-off",
				"automatic paths create children only from plans you pass",
			},
		},
		{
			name: "terminal tasks without authoritative integration evidence fail closed",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-1", "task-2"}
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusMerged, now),
					testhelpers.BuildTaskByStatus("task-2", models.TaskStatusMerged, now),
				}
				return state
			}(),
			wantContains: []string{
				"WAKE TRIGGER: INTEGRATION_UNAVAILABLE",
				"- Total tasks: 2",
				"- Merged: 2",
				"- Status: unavailable",
				"- Reason: integration_progress_unavailable",
				"Authoritative integration completion is not effective.",
			},
			wantNotContain: []string{
				"SPRINT_COMPLETE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dashboard, wakeInstr, err := RenderOrchestratorDashboard(tt.state, projectRoot, "orchestrator-1")
			if err != nil {
				t.Fatalf("RenderOrchestratorDashboard() error: %v", err)
			}

			result := dashboard + "\n" + wakeInstr

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("RenderOrchestratorDashboard() missing expected content:\n%q", want)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(result, notWant) {
					t.Errorf("RenderOrchestratorDashboard() contains unexpected content:\n%q", notWant)
				}
			}
		})
	}
}

func TestRenderOrchestratorDashboardSelectedWake(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName, brand.NameTitle = "acme", "Acme"
		brand.ProjectDirName, brand.GlobalDirName = ".acme", ".acme"
	})
	root := setupPipelineConfig(t)
	state := testhelpers.CreateValidState()
	plan := testhelpers.BuildTaskByStatus("plan", models.TaskStatusMerged, time.Now().UTC())
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{Desc: "implement", DoneWhen: "tests pass", Scope: "pkg/x"}}
	state.Tasks = []models.Task{plan, testhelpers.BuildTaskByStatus("blocked", models.TaskStatusBlocked, plan.Created)}
	state.Sprint.Scope.Planned = []string{"plan", "blocked"}
	dashboard, instruction, err := renderOrchestratorDashboardForWake(state, root, "orchestrator-1", OrchestratorWakeResult{Trigger: WakeTriggerPlanningComplete})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dashboard, "WAKE TRIGGER: PLANNING_COMPLETE") || !strings.Contains(instruction, "If any plan is passed or ready: acme sprint-checkpoint") || !strings.Contains(instruction, "- plan:") || strings.Contains(instruction, "Do NOT call") {
		t.Fatalf("selected planning decision not rendered consistently: %s\n%s", dashboard, instruction)
	}
	if strings.Contains(dashboard+instruction, "liza") || strings.Contains(dashboard+instruction, "Liza") {
		t.Fatal("selected wake leaked default brand")
	}
	dashboard, _, err = RenderOrchestratorDashboard(state, root, "orchestrator-1")
	if err != nil || !strings.Contains(dashboard, "WAKE TRIGGER: BLOCKED_TASKS") {
		t.Fatalf("snapshot-only rendering must retain ordinary priority: %s (%v)", dashboard, err)
	}
}

func TestRenderOrchestratorDashboard_AssessedTasksAllowPlanningHandoff(t *testing.T) {
	assessedAt := time.Date(2026, time.September, 13, 13, 0, 0, 0, time.UTC)
	projectRoot := setupPipelineConfig(t)
	for _, kind := range []struct {
		name    string
		status  models.TaskStatus
		trigger string
		counter string
	}{
		{"blocked", models.TaskStatusBlocked, "BLOCKED_TASKS", "- Blocked: 1"},
		{"exhausted", models.TaskStatusReady, "HYPOTHESIS_EXHAUSTED", "- Hypothesis exhausted: 1"},
	} {
		for _, activity := range []string{"unassessed", "assessed", "assessment only", "new task activity"} {
			t.Run(kind.name+"/"+activity, func(t *testing.T) {
				state := testhelpers.CreateValidState()
				planning := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, assessedAt.Add(-time.Hour))
				planning.RolePair = "code-planning-pair"
				planning.Output = []models.OutputEntry{{Desc: "Implement provider", DoneWhen: "Provider contract passes", Scope: "provider"}}
				consumer := testhelpers.BuildTaskByStatus("consumer", kind.status, assessedAt.Add(-time.Hour))
				consumer.FailedBy = []string{"coder-1", "coder-2"}
				consumer.DependsOn = []string{planning.ID}
				state.Tasks = []models.Task{planning, consumer}
				var assessmentExtra map[string]any
				if consumer.Status == models.TaskStatusBlocked {
					// Capture the baseline before any later task activity.
					assessmentExtra = map[string]any{
						ops.AssessmentFingerprintExtraKey: ops.BuildAssessmentFingerprint(state, &consumer, ops.AssessmentFingerprintCandidate{
							Reason: *consumer.BlockedReason, Questions: consumer.BlockedQuestions,
						}),
					}
				}
				if activity != "unassessed" {
					consumer.History = append(consumer.History, models.TaskHistoryEntry{
						Time: assessedAt, Event: models.TaskEventOrchestratorAssessment,
						Extra: assessmentExtra,
					})
				}
				switch activity {
				case "assessment only":
					consumer.History = append(consumer.History, models.TaskHistoryEntry{
						Time: assessedAt.Add(time.Minute), Event: models.TaskEventOrchestratorAssessment,
						Extra: assessmentExtra,
					})
				case "new task activity":
					consumer.History = append(consumer.History, models.TaskHistoryEntry{
						Time: assessedAt.Add(time.Minute), Event: models.TaskEventDependenciesRewritten,
					})
				}
				state.Tasks = []models.Task{planning, consumer}
				state.Sprint.Scope.Planned = []string{planning.ID, consumer.ID}

				dashboard, instruction, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
				if err != nil {
					t.Fatal(err)
				}
				wantTrigger := kind.trigger
				// An actionable blocker waiting on the merged planner prefers the
				// handoff it waits for (D45); exhausted tasks keep their rank.
				if kind.status == models.TaskStatusBlocked {
					wantTrigger = "PLANNING_COMPLETE"
				}
				if activity == "assessed" || activity == "assessment only" {
					wantTrigger = "PLANNING_COMPLETE"
					if !strings.Contains(instruction, "If any plan is passed or ready: "+brand.BinaryName+" sprint-checkpoint") ||
						strings.Contains(instruction, "Do NOT call "+brand.BinaryName+" sprint-checkpoint") {
						t.Errorf("assessed unchanged task must permit planning handoff; instruction: %s", instruction)
					}
				}
				if !strings.Contains(dashboard, "WAKE TRIGGER: "+wantTrigger+"\n") {
					t.Errorf("expected wake %s; dashboard: %s", wantTrigger, dashboard)
				}
				if !strings.Contains(dashboard, kind.counter) {
					t.Errorf("assessment must preserve total task counts: missing %q", kind.counter)
				}
			})
		}
	}
}

func TestRenderOrchestratorDashboard_BlockedDependencyRepairsRequireSemanticDirection(t *testing.T) {
	now := time.Now().UTC()
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{
		testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
	}

	_, wakeInstr, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatalf("RenderOrchestratorDashboard() error: %v", err)
	}

	semanticGate := "Before either stored apply-dependency-repair or direct retarget-dependency operation:"
	gateRequirements := []string{
		semanticGate,
		"Re-read every affected task and its planning/decomposition context.",
		"Verify provider-before-consumer direction for every proposed edge",
		"For any inverse edge, confirm the stored repair request or direct repair reason identifies the explicit relationship that independently requires the provider to depend on the consumer.",
		"Do not run either repair operation until this shared semantic gate passes.",
	}
	if count := strings.Count(wakeInstr, semanticGate); count != 1 {
		t.Fatalf("blocked-task wake prompt contains %d shared semantic gates, want 1:\n%s", count, wakeInstr)
	}

	operations := []string{
		brand.BinaryName + " apply-dependency-repair <blocked-task-id>",
		brand.BinaryName + " retarget-dependency <task-id> <old-dep-id> <new-dep-id[,new-dep-id]>",
	}
	for _, operation := range operations {
		operationIndex := strings.Index(wakeInstr, operation)
		if operationIndex == -1 {
			t.Fatalf("blocked-task wake prompt missing repair operation %q:\n%s", operation, wakeInstr)
		}
		for _, requirement := range gateRequirements {
			requirementIndex := strings.Index(wakeInstr, requirement)
			if requirementIndex == -1 {
				t.Fatalf("blocked-task wake prompt missing shared semantic gate requirement %q:\n%s", requirement, wakeInstr)
			}
			if requirementIndex > operationIndex {
				t.Fatalf("repair operation %q appears before shared semantic gate requirement %q:\n%s", operation, requirement, wakeInstr)
			}
		}
	}

	reconciliation := "If graph repair leaves a current blocker, reconcile its canonical metadata with:"
	reconciliationIndex := strings.Index(wakeInstr, reconciliation)
	if reconciliationIndex == -1 {
		t.Fatalf("blocked-task wake prompt missing blocker-metadata reconciliation:\n%s", wakeInstr)
	}
	for _, operation := range operations {
		if reconciliationIndex < strings.Index(wakeInstr, operation) {
			t.Fatalf("blocker-metadata reconciliation appears before repair operation %q:\n%s", operation, wakeInstr)
		}
	}
}

func TestRenderOrchestratorDashboard_EntryPoints(t *testing.T) {
	tests := []struct {
		name           string
		entryPoint     string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:       "explicit entry-point general-objective dispatches to epic-planning-pair",
			entryPoint: "general-objective",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"epic-planning-pair\"",
				"Epic Planner",
			},
			wantNotContain: []string{
				"Classify the input document",
				"code-planning-pair",
			},
		},
		{
			name:       "explicit entry-point functional-spec dispatches to architecture-pair",
			entryPoint: "functional-spec",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"architecture-pair\"",
				"Architect",
				"\"type\": \"architecture\"",
			},
			wantNotContain: []string{
				"Classify the input document",
				"epic-planning-pair",
				"\"type\": \"coding\"",
			},
		},
		{
			name:       "explicit entry-point technical-spec dispatches to code-planning-pair",
			entryPoint: "technical-spec",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"code-planning-pair\"",
				"Code Planner",
				"\"type\": \"planning\"",
			},
			wantNotContain: []string{
				"Classify the input document",
				"architecture-pair",
				"\"type\": \"architecture\"",
			},
		},
		{
			name:       "legacy detailed-spec dispatches to architecture-pair",
			entryPoint: "detailed-spec",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"architecture-pair\"",
				"Architect",
				"\"type\": \"architecture\"",
			},
			wantNotContain: []string{
				"Classify the input document",
				"epic-planning-pair",
				"\"type\": \"coding\"",
			},
		},
		{
			name:       "no entry-point shows classification with task types",
			entryPoint: "",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"general-objective",
				"functional-spec",
				"technical-spec",
				"detailed-spec",
				"epic-planning-pair",
				"architecture-pair",
				"code-planning-pair",
				"legacy alias",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := setupPipelineConfig(t)

			state := testhelpers.CreateValidState()
			state.Tasks = []models.Task{}
			state.Goal.EntryPoint = tt.entryPoint

			dashboard, wakeInstr, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
			if err != nil {
				t.Fatalf("RenderOrchestratorDashboard() error: %v", err)
			}

			result := dashboard + "\n" + wakeInstr

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("missing expected content: %q\n\nFull output:\n%s", want, result)
				}
			}
			for _, notWant := range tt.wantNotContain {
				if strings.Contains(result, notWant) {
					t.Errorf("unexpected content found: %q", notWant)
				}
			}
		})
	}
}

func TestResolveTaskType(t *testing.T) {
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatalf("LoadFromBytes: %v", err)
	}
	resolver := pipeline.NewResolver(cfg)

	tests := []struct {
		rolePair string
		want     string
	}{
		{"coding-pair", "coding"},
		{"architecture-pair", "architecture"},
		{"unknown-pair", "coding"},
	}
	for _, tt := range tests {
		t.Run(tt.rolePair, func(t *testing.T) {
			got := resolveWakeTaskType(resolver, tt.rolePair)
			if got != tt.want {
				t.Errorf("resolveWakeTaskType(%q) = %q, want %q", tt.rolePair, got, tt.want)
			}
		})
	}
}

func TestRenderOrchestratorDashboard_AutonomyForAllWakeTriggers(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)

	tests := []struct {
		name         string
		state        *models.State
		wantTrigger  string
		wantContains []string
	}{
		{
			name: "BLOCKED_TASKS has immediate action language",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusBlocked, now),
				}
				return state
			}(),
			wantTrigger: "BLOCKED_TASKS",
			wantContains: []string{
				"Analyze and resolve immediately",
				brand.BinaryName + " add-tasks --tasks-file",
				brand.BinaryName + " replace-task --replacement-file <path.json>",
				"run commands NOW",
				"Do NOT call " + brand.BinaryName + " sprint-checkpoint",
			},
		},
		{
			name: "HYPOTHESIS_EXHAUSTED has immediate action language",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				task := testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now)
				task.FailedBy = []string{"coder-1", "coder-2"}
				state.Tasks = []models.Task{task}
				return state
			}(),
			wantTrigger: "HYPOTHESIS_EXHAUSTED",
			wantContains: []string{
				"Re-evaluate and act NOW",
				"execute NOW",
				"update NOW",
				brand.BinaryName + " replace-task --replacement-file <path.json>",
				"Execute changes",
				"create them all in this session",
				"All state modifications must be executed before you exit",
				"Do NOT call " + brand.BinaryName + " sprint-checkpoint",
			},
		},
		{
			name: "IMMEDIATE_DISCOVERY has immediate action language",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Tasks = []models.Task{
					testhelpers.BuildTaskByStatus("task-1", models.TaskStatusReady, now),
				}
				state.Discovered = []models.Discovery{
					{
						ID:             "disc-1",
						By:             "coder-1",
						During:         "task-1",
						Description:    "Critical issue",
						Severity:       "critical",
						Urgency:        "immediate",
						Recommendation: "Fix now",
						Created:        now,
					},
				}
				return state
			}(),
			wantTrigger: "IMMEDIATE_DISCOVERY",
			wantContains: []string{
				"Urgent discoveries need immediate action",
				"execute decision NOW",
				brand.BinaryName + " add-tasks --tasks-file",
				brand.BinaryName + " set-discovery-disposition",
				"All commands must be executed in this session",
				"Do NOT call " + brand.BinaryName + " sprint-checkpoint",
			},
		},
		{
			name: "PLANNING_COMPLETE has checkpoint language",
			state: func() *models.State {
				state := testhelpers.CreateValidState()
				state.Sprint.Scope.Planned = []string{"task-plan-1", "task-code-1"}
				planningTask := testhelpers.BuildTaskByStatus("task-plan-1", models.TaskStatusMerged, now)
				planningTask.RolePair = "code-planning-pair"
				planningTask.Output = []models.OutputEntry{
					{Desc: "Implement feature", DoneWhen: "Feature works", Scope: "internal/"},
				}
				codingTask := testhelpers.BuildTaskByStatus("task-code-1", models.TaskStatusMerged, now)
				state.Tasks = []models.Task{planningTask, codingTask}
				return state
			}(),
			wantTrigger: "PLANNING_COMPLETE",
			wantContains: []string{
				"Planning tasks have been merged with output[]. You own their hand-off",
				"automatic paths create children only from plans you pass",
				"FULL autonomy to run CLI commands immediately",
				brand.BinaryName + " sprint-checkpoint",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dashboard, wakeInstr, err := RenderOrchestratorDashboard(tt.state, projectRoot, "orchestrator-1")
			if err != nil {
				t.Fatalf("RenderOrchestratorDashboard() error: %v", err)
			}

			result := dashboard + "\n" + wakeInstr

			// Verify correct trigger
			if !strings.Contains(result, "WAKE TRIGGER: "+tt.wantTrigger) {
				t.Errorf("Expected wake trigger %s not found", tt.wantTrigger)
			}

			// Verify all required action-oriented phrases
			for _, phrase := range tt.wantContains {
				if !strings.Contains(result, phrase) {
					t.Errorf("Missing expected action-oriented phrase: %s", phrase)
				}
			}
		})
	}
}

func TestRenderOrchestratorDashboard_CycleBlocked(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)

	t.Run("mixed: normal + cycle-blocked planning → only normal in PLANNING_COMPLETE", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Sprint.Scope.Planned = []string{"plan-normal", "plan-cycled", "code-done"}

		normalPlan := testhelpers.BuildTaskByStatus("plan-normal", models.TaskStatusMerged, now)
		normalPlan.RolePair = "code-planning-pair"
		normalPlan.Output = []models.OutputEntry{
			{Desc: "Normal output", DoneWhen: "done", Scope: "s"},
		}

		cycledPlan := testhelpers.BuildTaskByStatus("plan-cycled", models.TaskStatusMerged, now)
		cycledPlan.RolePair = "code-planning-pair"
		cycledPlan.Output = []models.OutputEntry{
			{Desc: "Cycled output", DoneWhen: "done", Scope: "s"},
		}
		cycledPlan.History = append(cycledPlan.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventTransitionCycleBlocked,
			Extra: map[string]any{"transition": "code-plan-to-coding", "cycle_members": []string{"plan-cycled"}},
		})

		codeDone := testhelpers.BuildTaskByStatus("code-done", models.TaskStatusMerged, now)

		state.Tasks = []models.Task{normalPlan, cycledPlan, codeDone}

		dashboard, wakeInstr, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
		if err != nil {
			t.Fatalf("RenderOrchestratorDashboard: %v", err)
		}
		result := dashboard + "\n" + wakeInstr

		if !strings.Contains(result, "WAKE TRIGGER: PLANNING_COMPLETE") {
			t.Error("expected PLANNING_COMPLETE trigger (normal plan has unconsumed output)")
		}
		if !strings.Contains(result, "Cycle-blocked planning: 1") {
			t.Error("expected cycle-blocked count in dashboard")
		}
		// PLANNING_COMPLETE fires (normal plan counted) not SPRINT_COMPLETE
		if strings.Contains(result, "SPRINT_COMPLETE") {
			t.Error("should NOT trigger SPRINT_COMPLETE when normal planning output exists")
		}
	})

	t.Run("all cycle-blocked fails closed without integration evidence", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Sprint.Scope.Planned = []string{"plan-cycled", "code-done"}

		cycledPlan := testhelpers.BuildTaskByStatus("plan-cycled", models.TaskStatusMerged, now)
		cycledPlan.RolePair = "code-planning-pair"
		cycledPlan.Output = []models.OutputEntry{
			{Desc: "Cycled output", DoneWhen: "done", Scope: "s"},
		}
		cycledPlan.History = append(cycledPlan.History, models.TaskHistoryEntry{
			Time:  now,
			Event: models.TaskEventTransitionCycleBlocked,
			Extra: map[string]any{"transition": "code-plan-to-coding", "cycle_members": []string{"plan-cycled"}},
		})

		codeDone := testhelpers.BuildTaskByStatus("code-done", models.TaskStatusMerged, now)

		state.Tasks = []models.Task{cycledPlan, codeDone}

		dashboard, wakeInstr, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
		if err != nil {
			t.Fatalf("RenderOrchestratorDashboard: %v", err)
		}
		result := dashboard + "\n" + wakeInstr

		if !strings.Contains(result, "WAKE TRIGGER: INTEGRATION_UNAVAILABLE") {
			t.Errorf("expected INTEGRATION_UNAVAILABLE without authoritative evidence, got:\n%s", result)
		}
		if strings.Contains(result, "SPRINT_COMPLETE") || strings.Contains(result, "PLANNING_COMPLETE") {
			t.Error("should not project terminal or planning completion when all planning is cycle-blocked")
		}
		if !strings.Contains(result, "Cycle-blocked planning: 1") {
			t.Error("expected cycle-blocked count in dashboard for operator visibility")
		}
	})
}

func TestRenderOrchestratorDashboard_ManyToOneReady(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	now := time.Now().UTC()

	state := testhelpers.CreateValidState()
	parentID := "epic-1"
	us1 := testhelpers.BuildTaskByStatus("us-1", models.TaskStatusMerged, now)
	us1.RolePair = "us-writing-pair"
	us1.ParentTask = &parentID
	us2 := testhelpers.BuildTaskByStatus("us-2", models.TaskStatusMerged, now)
	us2.RolePair = "us-writing-pair"
	us2.ParentTask = &parentID
	us3 := testhelpers.BuildTaskByStatus("us-3", models.TaskStatusMerged, now)
	us3.RolePair = "us-writing-pair"
	us3.ParentTask = &parentID
	state.Tasks = []models.Task{us1, us2, us3}
	state.Sprint.Scope.Planned = []string{"us-1", "us-2", "us-3"}

	dashboard, wakeInstr, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
	if err != nil {
		t.Fatalf("RenderOrchestratorDashboard: %v", err)
	}
	if !strings.Contains(dashboard, "WAKE TRIGGER: MANY_TO_ONE_READY") {
		t.Errorf("dashboard missing MANY_TO_ONE_READY trigger\ngot dashboard:\n%s", dashboard)
	}
	if !strings.Contains(wakeInstr, "checkpoint") {
		t.Errorf("wake instructions missing checkpoint guidance\ngot:\n%s", wakeInstr)
	}
}

// D-66: a BLOCKED_TASKS wake names the tasks actionable under the wake rules
// and never widens to unchanged holds, even when nothing is actionable.
func TestRenderOrchestratorDashboard_BlockedTasksScopedToActionable(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.BinaryName, brand.NameTitle = "acme", "Acme"
		brand.ProjectDirName, brand.GlobalDirName = ".acme", ".acme"
	})
	root := setupPipelineConfig(t)
	now := time.Now().UTC()
	assessedHeld := func(state *models.State) models.Task {
		held := testhelpers.BuildTaskByStatus("held-blocked", models.TaskStatusBlocked, now.Add(-time.Hour))
		held.History = append(held.History, models.TaskHistoryEntry{
			Time: now.Add(-time.Minute), Event: models.TaskEventOrchestratorAssessment,
			Extra: map[string]any{
				ops.AssessmentFingerprintExtraKey: ops.BuildAssessmentFingerprint(state, &held, ops.AssessmentFingerprintCandidate{
					Reason: *held.BlockedReason, Questions: held.BlockedQuestions,
				}),
			},
		})
		return held
	}
	render := func(t *testing.T, state *models.State, selected bool) string {
		t.Helper()
		var dashboard, instruction string
		var err error
		if selected {
			dashboard, instruction, err = renderOrchestratorDashboardForWake(state, root, "orchestrator-1", OrchestratorWakeResult{Trigger: WakeTriggerBlocked})
		} else {
			dashboard, instruction, err = RenderOrchestratorDashboard(state, root, "orchestrator-1")
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(dashboard, "WAKE TRIGGER: BLOCKED_TASKS\n") {
			t.Fatalf("expected BLOCKED_TASKS wake; dashboard: %s", dashboard)
		}
		if strings.Contains(dashboard+instruction, "liza") || strings.Contains(dashboard+instruction, "Liza") {
			t.Fatal("blocked wake leaked default brand")
		}
		return instruction
	}

	for _, selected := range []bool{false, true} {
		t.Run(fmt.Sprintf("actionable subset/selected=%t", selected), func(t *testing.T) {
			state := testhelpers.CreateValidState()
			changed := testhelpers.BuildTaskByStatus("changed-blocked", models.TaskStatusBlocked, now.Add(-time.Hour))
			state.Tasks = []models.Task{changed, assessedHeld(state)}
			instruction := render(t, state, selected)
			if !strings.Contains(instruction, "Assess only these BLOCKED tasks") || !strings.Contains(instruction, "changed-blocked") {
				t.Errorf("instruction must scope assessment to the actionable task: %s", instruction)
			}
			if strings.Contains(instruction, "held-blocked") {
				t.Errorf("instruction must not name an unchanged hold: %s", instruction)
			}
		})
	}

	t.Run("selected wake with empty actionable set", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{assessedHeld(state)}
		instruction := render(t, state, true)
		if !strings.Contains(instruction, "Record no assessment") {
			t.Errorf("empty actionable set must mean no assessments: %s", instruction)
		}
		if strings.Contains(instruction, "held-blocked") || strings.Contains(instruction, "decide action") {
			t.Errorf("empty actionable set must not widen to unchanged holds: %s", instruction)
		}
	})
}

func TestPlanningCompleteRequiresAppliedCorrectivePlacement(t *testing.T) {
	now := time.Now().UTC()
	task := testhelpers.BuildTaskByStatus("fix-cp", models.TaskStatusMerged, now)
	task.RolePair = "code-planning-pair"
	task.Output = []models.OutputEntry{{Desc: "one", DoneWhen: "d", Scope: "s", PlanRef: "specs/plans/fix-cp.md#Task 1"}}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{task}
	state.Sprint.Scope.Planned = []string{"fix-cp"}

	_, instruction, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(instruction), " ")
	for _, want := range []string{
		"f. A correction writer joining an ordered shared-file chain (not an ordinary admission) needs Placement; absent → amend-plan/replan.",
		"recheck each successor was never claimed and is unstarted, then " + brand.Command("reserve-provider") + " it onto this task; pass only once all hold.",
		"On failure keep it unpassed and its reservations:",
		"STALE_CALLER → stop;",
		"lock → bounded retry;",
		"bad transition/cap/cycle → amend-plan/replan;",
		"started successor, no legal slot or no rule correction case → plan-check --hold.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("instruction missing %q", want)
		}
	}
}

func TestProviderDependencyPlanningCompleteGuidanceBranded(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme-cli"
		brand.GlobalDirName = ".acme"
		brand.ProjectDirName = ".acme-run"
	})
	state := testhelpers.CreateValidState()
	plan := testhelpers.BuildTaskByStatus("consumer-plan", models.TaskStatusMerged, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC))
	plan.RolePair = "code-planning-pair"
	plan.Output = []models.OutputEntry{{
		Desc: "Consumer", DoneWhen: "Tests pass", Scope: "bounded consumer",
		PlanRef: "specs/plans/consumer.md#Task 1",
	}}
	state.Tasks = []models.Task{plan}
	state.Sprint.Scope.Planned = []string{plan.ID}
	_, output, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"provider_dependencies (existing provider, configured per-subtask transition, selected outputs)",
		"unborn children and cross-stage waits",
		"reviewed reauthoring/replacement before restoring a legacy blocked consumer",
		"merely unblocking against a merged architecture or APPROVED plan is insufficient",
		"future production/inheritance cycles",
		"runtime does not parse prose",
		"acme-cli plan-check <task-id> --pass",
		"acme-cli replan <task-id>",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("planning-complete guidance missing %q", want)
		}
	}
	for _, literal := range []string{"Liza", "LIZA", "liza", "{{binaryName}}"} {
		if strings.Contains(output, literal) {
			t.Errorf("non-default wake guidance leaks %q", literal)
		}
	}
}

// D73: the PLANNING_COMPLETE turn lists each plan to review with its plan
// files, the executability checks, and the three supported dispositions.
func TestPlanningCompleteInstructionsReviewDispositions(t *testing.T) {
	now := time.Now().UTC()
	plan := func(id string, deps ...string) models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusMerged, now)
		task.RolePair = "code-planning-pair"
		task.DependsOn = deps
		task.Output = []models.OutputEntry{
			{Desc: "one", DoneWhen: "d", Scope: "s", PlanRef: "specs/plans/" + id + ".md#Task 1"},
			{Desc: "two", DoneWhen: "d", Scope: "s", PlanRef: "specs/plans/" + id + ".md#Task 2"},
		}
		return task
	}
	upstream := plan("upstream")
	upstream.TransitionsExecuted = map[string]bool{"replanned": true, "code-plan-to-coding": true}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{plan("fresh"), plan("consumer", "upstream"), upstream}
	state.Sprint.Scope.Planned = []string{"fresh", "consumer", "upstream"}

	_, instruction, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PLANS TO REVIEW:\n- fresh: specs/plans/fresh.md\n",
		"- consumer: specs/plans/consumer.md (a pass is refused while upstream upstream was replanned)",
		"`pre-commit run --files <paths>`",
		strings.Join(ops.TestFileMatcherPatterns(), ", "),
		"plan_ref anchor names exactly one ATX heading",
		"has a producer (a task, a committed generator, or an explicit human step)",
		"its instances are recorded after the children exist, so do not hold for them",
		"use reviewed amend-plan or replan to declare runtime_inputs when",
		"Notes never excuse missing DB/venv/container producers, durable runtime_inputs or human provisioning",
		brand.Command("plan-check") + " <task-id> --pass --notes-file <file>",
		brand.Command("amend-plan") + " <task-id> --apply <correction-id>",
		brand.Command("replan") + ` <task-id> --reason "<check>: <evidence> → <required correction>" --changed-by orchestrator-1`,
		brand.Command("plan-check") + ` <task-id> --hold "<exact human action>" --agent-id orchestrator-1 --json`,
		brand.Command("plan-check") + " <task-id> --pass --agent-id orchestrator-1 --json",
		"If every plan awaits amendment, was replanned or held, do not checkpoint.",
	} {
		if !strings.Contains(instruction, want) {
			t.Errorf("instruction missing %q:\n%s", want, instruction)
		}
	}
	if strings.Contains(instruction, "READY") || strings.Contains(instruction, "- upstream") {
		t.Errorf("a replanned plan is not handed off and nothing is ready:\n%s", instruction)
	}
}

// D45: the snapshot dashboard path must agree with the live selector. A
// blocker that depends on untransitioned planning output renders the
// PLANNING_COMPLETE instructions, not a BLOCKED_TASKS turn that forbids
// sprint-checkpoint.
func TestRenderOrchestratorDashboard_BlockedOnPlannerRendersPlanningComplete(t *testing.T) {
	mergedAt := time.Date(2026, time.September, 24, 7, 0, 0, 0, time.UTC)
	planner := testhelpers.BuildTaskByStatus("provider-plan", models.TaskStatusMerged, mergedAt.Add(-time.Hour))
	planner.RolePair = "code-planning-pair"
	planner.Output = []models.OutputEntry{{Desc: "Implement provider", DoneWhen: "Provider contract passes", Scope: "provider"}}
	planner.History = append(planner.History, models.TaskHistoryEntry{Time: mergedAt, Event: models.TaskEventMerged})
	consumer := testhelpers.BuildTaskByStatus("consumer", models.TaskStatusBlocked, mergedAt.Add(-2*time.Hour))
	consumer.DependsOn = []string{planner.ID}
	state := testhelpers.CreateValidState()
	state.Tasks = []models.Task{planner, consumer}
	state.Sprint.Scope.Planned = []string{planner.ID, consumer.ID}

	dashboard, instruction, err := RenderOrchestratorDashboard(state, setupPipelineConfig(t), "orchestrator-1")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(dashboard, "WAKE TRIGGER: PLANNING_COMPLETE\n") {
		t.Errorf("expected PLANNING_COMPLETE for a blocker waiting on planning output; dashboard: %s", dashboard)
	}
	if !strings.Contains(instruction, "If any plan is passed or ready:") || !strings.Contains(instruction, "sprint-checkpoint --json") {
		t.Errorf("instructions must permit the checkpoint that materializes the children; got: %s", instruction)
	}
}

func withPromptBrandValues(t *testing.T, mutate func()) {
	t.Helper()
	oldNameTitle := brand.NameTitle
	oldBinaryName := brand.BinaryName
	oldGlobalDirName := brand.GlobalDirName
	oldProjectDirName := brand.ProjectDirName
	mutate()
	t.Cleanup(func() {
		brand.NameTitle = oldNameTitle
		brand.BinaryName = oldBinaryName
		brand.GlobalDirName = oldGlobalDirName
		brand.ProjectDirName = oldProjectDirName
	})
}
