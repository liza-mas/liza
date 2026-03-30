package prompts

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestBuildBasePrompt(t *testing.T) {
	tests := []struct {
		name           string
		config         BasePromptConfig
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "basic base prompt with all required fields",
			config: BasePromptConfig{
				Role:        "code-coder",
				AgentID:     "coder-1",
				TaskID:      "task-1",
				SpecsDir:    "/project/specs",
				ProjectRoot: "/project",
				StatePath:   "/project/.liza/state.yaml",
				GoalDesc:    "Build a web API",
				GoalSpecRef: "specs/vision.md",
			},
			wantContains: []string{
				"You are a Liza code-coder agent",
				"Agent ID: coder-1",
				"ROLE: code-coder",
				"PROJECT_SPECS: /project/specs",
				"PROJECT: /project",
				"BLACKBOARD: /project/.liza/state.yaml",
				"GOAL: Build a web API",
				"APPROVED: use MCP tools with escalated permissions",
				"TWO .liza/ directories exist",
				"~/.liza/ = installed contracts & skills",
				"/project/.liza/ = runtime state & blackboard",
				"You have FULL read access to both .liza/ directories",
				"For READING state: use liza_get with targeted queries",
				"For MODIFYING state: use role-specific MCP tools ONLY",
				"NEVER edit state.yaml directly",
				"Execute commands immediately",
				"DO proceed with tool execution",
				"QUERY TOOLS",
				"liza_get",
				"liza_status",
				"liza_validate",
				"COMMUNICATION:",
				"FORBIDDEN:",
				"Do NOT attempt to claim tasks",
				"SESSION EXIT CODES",
				"TIMESTAMPS:",
				"FIRST ACTIONS:",
				`Query your assigned task: liza_get {"query": "tasks/task-1"}`,
				"Read the goal spec: specs/vision.md",
				"lessons/agents/",
				"GUARDRAILS.md",
			},
			wantNotContain: []string{
				// Role-specific tools should NOT be in base prompt
				"liza_add_tasks",
				"liza_submit_for_review",
				"liza_submit_verdict",
				// shared_reference content should NOT be in base prompt
				"TASK STATE MACHINE:",
				"BLACKBOARD FIELDS:",
				"ANOMALY TYPES:",
				"LEASE MODEL:",
				"HELPER COMMANDS",
			},
		},
		{
			name: "role title formatting for multi-word roles",
			config: BasePromptConfig{
				Role:        "code-reviewer",
				AgentID:     "code-reviewer-1",
				SpecsDir:    "/specs",
				ProjectRoot: "/project",
				StatePath:   "/project/.liza/state.yaml",
				GoalDesc:    "Test goal",
				GoalSpecRef: "specs/test.md",
			},
			wantContains: []string{
				"You are a Liza code-reviewer agent",
				"QUERY TOOLS",
			},
		},
		{
			name: "orchestrator role formatting",
			config: BasePromptConfig{
				Role:        "orchestrator",
				AgentID:     "orchestrator-1",
				SpecsDir:    "/specs",
				ProjectRoot: "/project",
				StatePath:   "/project/.liza/state.yaml",
				GoalDesc:    "Test",
				GoalSpecRef: "specs/vision.md",
			},
			wantContains: []string{
				"You are a Liza orchestrator agent",
				"QUERY TOOLS",
				`Query workspace state: liza_get {"query": "tasks"}`,
				"FORBIDDEN:",
				"Do NOT manually modify task status",
				"Do NOT make architecture decisions",
			},
			wantNotContain: []string{
				"Query your assigned task",
				"Do NOT attempt to claim tasks",
				"Do NOT skip worktrees",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := BuildBasePrompt(tt.config)
			if err != nil {
				t.Fatalf("BuildBasePrompt() error: %v", err)
			}

			for _, want := range tt.wantContains {
				if !strings.Contains(result, want) {
					t.Errorf("BuildBasePrompt() missing expected content:\n%q", want)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if strings.Contains(result, notWant) {
					t.Errorf("BuildBasePrompt() contains unexpected content:\n%q", notWant)
				}
			}
		})
	}
}

func TestRenderOrchestratorDashboard(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)

	tests := []struct {
		name         string
		state        *models.State
		wantContains []string
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
				"ORCHESTRATOR COMMANDS (resolve AFTER initialization:",
				"liza_add_tasks",
				"liza_assess_blocked",
				"liza_supersede_task",
				`liza_wt_delete`,
				`Tool parameters: {"task_id": "...", "agent_id": "orchestrator-1"}`,
				`liza_sprint_checkpoint — Create sprint checkpoint for human review`,
				`Tool parameters: {"agent_id": "orchestrator-1"}`,
				`liza_update_sprint_metrics — Recompute sprint metrics`,
				`Tool parameters: {"agent_id": "orchestrator-1"}`,
				"This is initial planning",
				"Classify the input document and choose the appropriate entry-point",
				"AVAILABLE ENTRY-POINTS:",
				"Phases must partition the goal",
				"role-pair-derived prefix with sequential suffixes",
				"All tasks use the chosen role_pair matching the entry-point",
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
			wantContains: []string{
				"WAKE TRIGGER: BLOCKED_TASKS",
				"- Total tasks: 2",
				"- Blocked: 1",
				"Tasks are BLOCKED. Analyze and resolve immediately:",
				"Read blocked tasks from blackboard",
				"liza_assess_blocked",
			},
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
			wantContains: []string{
				"WAKE TRIGGER: HYPOTHESIS_EXHAUSTED",
				"- Hypothesis exhausted: 1",
				"Multiple coders failed on same task. Re-evaluate and act NOW:",
			},
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
			wantContains: []string{
				"WAKE TRIGGER: IMMEDIATE_DISCOVERY",
				"- Immediate discoveries: 1",
				"Urgent discoveries need immediate action:",
			},
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
				"Planning sprint tasks have been merged with output[] entries",
				"Pipeline transitions will execute automatically after checkpoint and human resume",
				`liza_sprint_checkpoint`,
			},
		},
		{
			name: "sprint complete trigger",
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
				"WAKE TRIGGER: SPRINT_COMPLETE",
				"- Total tasks: 2",
				"- Merged: 2",
				"All planned sprint tasks have reached terminal state",
				`liza_update_sprint_metrics with {"agent_id": "orchestrator-1"}`,
				`liza_sprint_checkpoint with {"agent_id": "orchestrator-1"}`,
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
		})
	}
}

// setupPipelineConfig writes the production embedded pipeline.yaml into a temp
// directory's .liza/ folder and returns the temp dir as projectRoot.
func setupPipelineConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	lizaDir := filepath.Join(dir, ".liza")
	if err := os.MkdirAll(lizaDir, 0o755); err != nil {
		t.Fatalf("mkdir .liza: %v", err)
	}
	if err := os.WriteFile(filepath.Join(lizaDir, "pipeline.yaml"), embedded.PipelineConfigContent(), 0o644); err != nil {
		t.Fatalf("write pipeline.yaml: %v", err)
	}
	return dir
}

// testPipelineResolver returns a pipeline.Resolver built from the production
// embedded pipeline YAML. Tests use this to load section names dynamically
// rather than hardcoding them.
func testPipelineResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatalf("testPipelineResolver: %v", err)
	}
	return pipeline.NewResolver(cfg)
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
				"classify",
				"code-planning-pair",
			},
		},
		{
			name:       "explicit entry-point detailed-spec dispatches to code-planning-pair",
			entryPoint: "detailed-spec",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"code-planning-pair\"",
				"Code Planner",
			},
			wantNotContain: []string{
				"classify",
				"epic-planning-pair",
			},
		},
		{
			name:       "no entry-point shows classification instructions",
			entryPoint: "",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"general-objective",
				"detailed-spec",
				"epic-planning-pair",
				"code-planning-pair",
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

// TestBasePromptRegressionGuard is a comprehensive regression test for the base prompt.
// The base prompt is the foundation for ALL agent roles. A regression here silently
// degrades every agent in the system. Each section is tested independently so failures
// pinpoint exactly what broke.
func TestBasePromptRegressionGuard(t *testing.T) {
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-42",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	// Helper to check a batch of required phrases with a section label
	assertSection := func(section string, phrases []string) {
		t.Helper()
		for _, phrase := range phrases {
			if !strings.Contains(prompt, phrase) {
				t.Errorf("[%s] missing: %q", section, phrase)
			}
		}
	}

	// Helper to check phrases that must NOT appear
	assertAbsent := func(section string, phrases []string) {
		t.Helper()
		for _, phrase := range phrases {
			if strings.Contains(prompt, phrase) {
				t.Errorf("[%s] must not contain: %q", section, phrase)
			}
		}
	}

	// --- BOOTSTRAP CONTEXT: template variables resolve correctly ---
	assertSection("bootstrap", []string{
		"You are a Liza code-coder agent",
		"Agent ID: coder-1",
		"ROLE: code-coder",
		"PROJECT_SPECS: /project/specs",
		"PROJECT: /project",
		"BLACKBOARD: /project/.liza/state.yaml",
		"GOAL: Build a web API",
	})

	// --- OPERATIONAL RULES: .liza/ directory disambiguation ---
	assertSection("liza-dirs", []string{
		"TWO .liza/ directories exist",
		"~/.liza/ = installed contracts & skills",
		"/project/.liza/ = runtime state & blackboard",
		"FULL read access to both .liza/ directories",
	})

	// --- STATE ACCESS: liza_get over state.yaml ---
	assertSection("state-access", []string{
		"use liza_get with targeted queries",
		"NEVER read state.yaml directly",
		"liza_get returns only the requested slice",
		"NEVER edit state.yaml directly",
	})

	// --- AUTONOMY: agents must not hesitate ---
	assertSection("autonomy", []string{
		"Your authority is pre-approved",
		"Execute commands immediately",
		"DO proceed with tool execution",
	})

	// --- BASH CONSTRAINTS: universal safety rules ---
	assertSection("bash-constraints", []string{
		"BASH CONSTRAINTS",
		"NEVER combine cd and git in one command",
		"git -C <path> <cmd>",
		"NEVER use $() command substitution",
		"ANSI-C quoting",
		"NEVER attempt to install, bootstrap, or fix system-level tooling",
		`NEVER use "git add -A" or "git add ."`,
		"stage specific files by name",
		"liza_* operations are MCP tool calls",
		"NEVER via shell commands",
	})

	// --- QUERY TOOLS: available to all roles ---
	assertSection("query-tools", []string{
		"QUERY TOOLS",
		"liza_get",
		"liza_status",
		"liza_validate",
	})

	// --- COMMUNICATION: blackboard-only ---
	assertSection("communication", []string{
		"Agents communicate via blackboard only",
		"MCP tools",
		"not direct interaction",
	})

	// --- FORBIDDEN: hard prohibitions ---
	assertSection("forbidden", []string{
		"FORBIDDEN:",
		"Do NOT attempt to claim tasks",
		"Do NOT manually modify task status",
		"Do NOT skip worktrees",
		"Do NOT make architecture decisions",
	})

	// --- SESSION EXIT CODES: supervisor protocol ---
	assertSection("exit-codes", []string{
		"SESSION EXIT CODES",
		"Session ended normally",
		"Graceful abort",
		"Restart with backoff",
	})

	// --- FIRST ACTIONS: boot sequence ---
	assertSection("first-actions", []string{
		"FIRST ACTIONS:",
		`Query your assigned task: liza_get {"query": "tasks/task-42"}`,
		"Read the goal spec: specs/vision.md",
		"lessons/agents/",
		"GUARDRAILS.md",
		"Execute your role's protocol",
	})

	// --- ENVIRONMENT LESSONS ---
	assertSection("env-lessons", []string{
		"ENVIRONMENT LESSONS",
		"skill: lesson-capture",
	})

	// --- CODEBASE EXPLORATION: context-saving delegation ---
	assertSection("codebase-exploration", []string{
		"CODEBASE EXPLORATION",
		"AGENT_TOOLS.md",
	})

	// --- NEGATIVE: role-specific content must NOT leak into base ---
	assertAbsent("no-role-leak", []string{
		"liza_add_tasks",
		"liza_submit_for_review",
		"liza_submit_verdict",
		"WORKTREE RULES",
		"IMPLEMENTATION PHASE",
		"REVIEW CHECKLIST",
		"VERDICT SUBMISSION",
	})
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
				"execute liza_add_tasks tool NOW",
				"execute tools NOW",
				"Do NOT call liza_sprint_checkpoint",
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
				"Execute changes",
				"create them all in this session",
				"All state modifications must be executed before you exit",
				"Do NOT call liza_sprint_checkpoint",
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
				"execute liza_add_tasks tool NOW",
				"liza_set_discovery_disposition",
				"All tools must be executed in this session",
				"Do NOT call liza_sprint_checkpoint",
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
				"Planning sprint tasks have been merged with output[] entries",
				"Pipeline transitions will execute automatically after checkpoint and human resume",
				"FULL autonomy to execute MCP tools immediately",
				"liza_sprint_checkpoint",
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

func TestBlockMandatoryDocs_Populated(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/mandatory_docs.tmpl"))

	data := RoleContextData{
		MandatoryDocs: []string{
			"docs/architecture.md",
			"docs/api-reference.md",
			"specs/security-policy.md",
		},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "mandatory-docs", &data)
	if err != nil {
		t.Fatalf("failed to execute mandatory-docs template: %v", err)
	}

	result := buf.String()

	if !strings.Contains(result, "MANDATORY DOCUMENTS") {
		t.Error("expected MANDATORY DOCUMENTS section header")
	}
	for _, doc := range data.MandatoryDocs {
		if !strings.Contains(result, "- "+doc) {
			t.Errorf("expected mandatory doc %q in output", doc)
		}
	}
}

func TestBlockMandatoryDocs_Empty(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/mandatory_docs.tmpl"))

	data := RoleContextData{
		MandatoryDocs: nil,
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "mandatory-docs", &data)
	if err != nil {
		t.Fatalf("failed to execute mandatory-docs template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for nil MandatoryDocs, got %q", buf.String())
	}
}

func TestBlockMandatoryDocs_EmptySlice(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/mandatory_docs.tmpl"))

	data := RoleContextData{
		MandatoryDocs: []string{},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "mandatory-docs", &data)
	if err != nil {
		t.Fatalf("failed to execute mandatory-docs template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for empty MandatoryDocs slice, got %q", buf.String())
	}
}

func TestBlockSkillsAffinity_Populated(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/skills_affinity.tmpl"))

	data := RoleContextData{
		Skills: []string{
			"debugging",
			"testing",
			"clean-code",
		},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "skills-affinity", &data)
	if err != nil {
		t.Fatalf("failed to execute skills-affinity template: %v", err)
	}

	result := buf.String()

	if !strings.Contains(result, "SKILLS AFFINITY") {
		t.Error("expected SKILLS AFFINITY section header")
	}
	for _, skill := range data.Skills {
		if !strings.Contains(result, "- "+skill) {
			t.Errorf("expected skill %q in output", skill)
		}
	}
}

func TestBlockSkillsAffinity_Empty(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/skills_affinity.tmpl"))

	data := RoleContextData{
		Skills: nil,
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "skills-affinity", &data)
	if err != nil {
		t.Fatalf("failed to execute skills-affinity template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for nil Skills, got %q", buf.String())
	}
}

func TestBlockSkillsAffinity_EmptySlice(t *testing.T) {
	tmpl := template.Must(template.ParseFiles("templates/blocks/skills_affinity.tmpl"))

	data := RoleContextData{
		Skills: []string{},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "skills-affinity", &data)
	if err != nil {
		t.Fatalf("failed to execute skills-affinity template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for empty Skills slice, got %q", buf.String())
	}
}

// TestBuildRoleContext_AllRoles verifies that BuildRoleContext produces expected key
// content strings for each of the 9 standard roles using block templates.
// Section lists are loaded from the embedded pipeline YAML via the resolver,
// so adding or removing a section in the YAML automatically affects test coverage.
func TestBuildRoleContext_AllRoles(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	makeDoerTask := func(id string) *models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusImplementing, now)
		task.Description = "Implement feature X"
		task.DoneWhen = "Feature X works correctly"
		task.Scope = "internal/feature"
		task.Iteration = 2
		reason := "Missing error handling"
		task.RejectionReason = &reason
		worktree := ".worktrees/" + id
		task.Worktree = &worktree
		return &task
	}

	makeReviewerTask := func(id string) *models.Task {
		task := testhelpers.BuildTaskByStatus(id, models.TaskStatusReadyForReview, now)
		task.Description = "Implement feature X"
		task.DoneWhen = "Feature X works correctly"
		task.Scope = "internal/feature"
		task.Iteration = 2
		reason := "Missing error handling"
		task.RejectionReason = &reason
		worktree := ".worktrees/" + id
		task.Worktree = &worktree
		baseCommit := "abc1234"
		task.BaseCommit = &baseCommit
		reviewCommit := "def5678"
		task.ReviewCommit = &reviewCommit
		agent := "coder-1"
		task.AssignedTo = &agent
		return &task
	}

	siblings := []SiblingTaskSummary{
		{ID: "task-0", Description: "Setup infrastructure", Status: "MERGED"},
	}

	_ = makeDoerTask     // used in subtests
	_ = makeReviewerTask // used in subtests

	t.Run("coder", func(t *testing.T) {
		_ = makeDoerTask("task-coder")
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-coder", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:          projectRoot + "/.worktrees/task-coder",
			IterationNum:      2,
			PriorRejection:    "Missing error handling",
			IntegrationBranch: "integration",
			GoalSpecRef:       "specs/goal.md",
			SiblingTasks:      siblings,
			TotalPlanTasks:    3, TaskOrdinal: 2,
			ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("coder")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED TASK ===",
			"TASK ID: task-coder",
			"CODER STATE TRANSITIONS:",
			"IMPLEMENTING_CODE",
			"CODER TOOLS (resolve AFTER initialization:",
			"liza_submit_for_review",
			"liza_handoff",
			"liza_mark_blocked",
			"ANOMALY LOGGING:",
			"BLOCKING PROTOCOL:",
			"WORKTREE RULES:",
			"COMMIT WORKFLOW:",
			"IMPLEMENTATION PHASE",
			"SUBMISSION (MANDATORY",
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION FEEDBACK (MUST ADDRESS)",
			"Missing error handling",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("code-reviewer", func(t *testing.T) {
		_ = makeReviewerTask("task-reviewer")
		data := &RoleContextData{
			Role: "code-reviewer", AgentID: "code-reviewer-1", RoleType: "reviewer",
			TaskID: "task-reviewer", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-reviewer",
			IterationNum: 2, PriorRejection: "Missing error handling",
			BaseCommit: "abc1234", ReviewCommit: "def5678", AssignedTo: "coder-1",
			GoalSpecRef: "specs/goal.md", SiblingTasks: siblings,
			TotalPlanTasks: 3, TaskOrdinal: 2, ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("code-reviewer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("code-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== REVIEW TASK ===",
			"TASK ID: task-reviewer",
			"BASE COMMIT: abc1234",
			"REVIEW COMMIT: def5678",
			"AUTHOR: coder-1",
			"REVIEWER STATE TRANSITIONS:",
			"REVIEWING_CODE",
			"CODE_APPROVED",
			"REVIEWER TOOL (resolve AFTER initialization:",
			"liza_submit_verdict",
			"liza_await_resubmission",
			"ANOMALY LOGGING:",
			"WORKTREE RULES:",
			"REVIEW SCOPE:",
			"REJECTION FORMAT",
			"VERDICT SUBMISSION",
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION (iteration 1)",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("orchestrator", func(t *testing.T) {
		state := testhelpers.CreateValidState()
		state.Tasks = []models.Task{}
		dashboard, wakeInstr, err := RenderOrchestratorDashboard(state, projectRoot, "orchestrator-1")
		if err != nil {
			t.Fatalf("RenderOrchestratorDashboard: %v", err)
		}

		data := &RoleContextData{
			Role: "orchestrator", AgentID: "orchestrator-1", RoleType: "orchestrator",
			DashboardOutput: dashboard,
			WakeInstruction: wakeInstr,
			ProjectRoot:     projectRoot,
		}
		sections, err := resolver.ContextSections("orchestrator")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("orchestrator", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ORCHESTRATOR CONTEXT ===",
			"WAKE TRIGGER:",
			"SPRINT STATE:",
			"ORCHESTRATOR COMMANDS (resolve AFTER initialization:",
			"liza_add_tasks",
			"ANOMALY LOGGING:",
			"INSTRUCTIONS:",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("code-planner", func(t *testing.T) {
		_ = makeDoerTask("task-planner")
		data := &RoleContextData{
			Role: "code-planner", AgentID: "code-planner-1", RoleType: "doer",
			TaskID: "task-planner", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-planner",
			IterationNum: 2, PriorRejection: "Missing error handling",
			GoalSpecRef: "specs/goal.md", SiblingTasks: siblings,
			TotalPlanTasks: 3, TaskOrdinal: 2, ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("code-planner")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("code-planner", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED CODE PLANNING TASK ===",
			"TASK ID: task-planner",
			"CODE PLANNER STATE TRANSITIONS:",
			"CODE PLANNER TOOLS (resolve AFTER initialization:",
			"liza_set_task_output",
			"WORKTREE RULES:",
			"TASK DECOMPOSITION PRINCIPLE:",
			"IMPLEMENTATION PHASE:",
			"TIMESTAMP-task-planner.md", // canonical plan file path with task ID
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION FEEDBACK (MUST ADDRESS)",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("code-plan-reviewer", func(t *testing.T) {
		_ = makeReviewerTask("task-cpr")
		data := &RoleContextData{
			Role: "code-plan-reviewer", AgentID: "code-plan-reviewer-1", RoleType: "reviewer",
			TaskID: "task-cpr", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-cpr",
			IterationNum: 2, PriorRejection: "Missing error handling",
			BaseCommit: "abc1234", ReviewCommit: "def5678", AssignedTo: "coder-1",
			GoalSpecRef: "specs/goal.md", SiblingTasks: siblings,
			TotalPlanTasks: 3, TaskOrdinal: 2, ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("code-plan-reviewer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("code-plan-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED CODE PLAN REVIEW TASK ===",
			"TASK ID: task-cpr",
			"CODE PLAN REVIEWER STATE TRANSITIONS:",
			"REVIEWING_CODING_PLAN",
			"CODE PLAN REVIEWER TOOLS (resolve AFTER initialization:",
			"liza_submit_verdict",
			"liza_await_resubmission",
			"REVIEW CHECKLIST:",
			"TIMESTAMP-task-cpr",           // interpolated task ID in reviewer gate
			"Plan file location",           // gate label present in checklist
			"Plan artifact not in diff at", // gate condition wording
			"VERDICT SUBMISSION",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("epic-planner", func(t *testing.T) {
		_ = makeDoerTask("task-ep")
		data := &RoleContextData{
			Role: "epic-planner", AgentID: "epic-planner-1", RoleType: "doer",
			TaskID: "task-ep", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-ep",
			IterationNum: 2, PriorRejection: "Missing error handling",
			ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("epic-planner")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("epic-planner", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED EPIC PLANNING TASK ===",
			"TASK ID: task-ep",
			"EPIC PLANNER STATE TRANSITIONS:",
			"EPIC PLANNER TOOLS (resolve AFTER initialization:",
			"liza_set_task_output",
			"WORKTREE RULES:",
			"EPIC-WRITING SKILL:",
			"IMPLEMENTATION PHASE:",
			"PRIOR REJECTION FEEDBACK (MUST ADDRESS)",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("epic-plan-reviewer", func(t *testing.T) {
		_ = makeReviewerTask("task-epr")
		data := &RoleContextData{
			Role: "epic-plan-reviewer", AgentID: "epic-plan-reviewer-1", RoleType: "reviewer",
			TaskID: "task-epr", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-epr",
			IterationNum: 2, PriorRejection: "Missing error handling",
			BaseCommit: "abc1234", ReviewCommit: "def5678", AssignedTo: "coder-1",
			ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("epic-plan-reviewer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("epic-plan-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED EPIC PLAN REVIEW TASK ===",
			"TASK ID: task-epr",
			"EPIC PLAN REVIEWER STATE TRANSITIONS:",
			"REVIEWING_EPIC_PLAN",
			"EPIC PLAN REVIEWER TOOLS (resolve AFTER initialization:",
			"liza_submit_verdict",
			"liza_await_resubmission",
			"EPIC-WRITING SKILL:",
			"REVIEW CHECKLIST:",
			"VERDICT SUBMISSION",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("us-writer", func(t *testing.T) {
		_ = makeDoerTask("task-usw")
		data := &RoleContextData{
			Role: "us-writer", AgentID: "us-writer-1", RoleType: "doer",
			TaskID: "task-usw", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			SpecRef:      "README.md",
			PlanRef:      "specs/epics/ep-001.md",
			PlanSection:  "capability-cap-001---task-creation",
			Worktree:     projectRoot + "/.worktrees/task-usw",
			IterationNum: 2, PriorRejection: "Missing error handling",
			GoalSpecRef: "specs/goal.md", SiblingTasks: siblings,
			TotalPlanTasks: 3, TaskOrdinal: 2, ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("us-writer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("us-writer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED US WRITING TASK ===",
			"TASK ID: task-usw",
			"US WRITER STATE TRANSITIONS:",
			"US WRITER TOOLS (resolve AFTER initialization:",
			"WORKTREE RULES:",
			"USER-STORY-WRITING SKILL:",
			"CAPABILITY SCOPING:",
			"IMPLEMENTATION PHASE:",
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION FEEDBACK (MUST ADDRESS)",
			"specs/epics/ep-001.md",
			"#capability-cap-001---task-creation",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("us-reviewer", func(t *testing.T) {
		_ = makeReviewerTask("task-usr")
		data := &RoleContextData{
			Role: "us-reviewer", AgentID: "us-reviewer-1", RoleType: "reviewer",
			TaskID: "task-usr", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			SpecRef:      "README.md",
			PlanRef:      "specs/epics/ep-001.md",
			PlanSection:  "capability-cap-001---task-creation",
			Worktree:     projectRoot + "/.worktrees/task-usr",
			IterationNum: 2, PriorRejection: "Missing error handling",
			BaseCommit: "abc1234", ReviewCommit: "def5678", AssignedTo: "coder-1",
			GoalSpecRef: "specs/goal.md", SiblingTasks: siblings,
			TotalPlanTasks: 3, TaskOrdinal: 2, ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("us-reviewer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("us-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED US REVIEW TASK ===",
			"TASK ID: task-usr",
			"US REVIEWER STATE TRANSITIONS:",
			"REVIEWING_US",
			"US REVIEWER TOOLS (resolve AFTER initialization:",
			"liza_submit_verdict",
			"liza_await_resubmission",
			"SPEC-REVIEW SKILL:",
			"USER-STORY ANTI-PATTERNS",
			"QUALITY GATES:",
			"CAPABILITY SCOPING:",
			"VERDICT SUBMISSION",
			"specs/epics/ep-001.md",
			"#capability-cap-001---task-creation",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})
}

func TestBuildRoleContext_PlanRefAndValidationPlan(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	t.Run("coder with PlanRef renders plan context", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:          projectRoot + "/.worktrees/task-1",
			IterationNum:      1,
			IntegrationBranch: "integration",
			PlanRef:           "specs/plans/20260317-plan.md",
			ProjectRoot:       projectRoot,
		}
		sections, _ := resolver.ContextSections("coder")
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "specs/plans/20260317-plan.md") {
			t.Error("output missing PlanRef path")
		}
		if !strings.Contains(output, "implementation plan") {
			t.Error("output missing plan context text")
		}
	})

	t.Run("code-reviewer with PlanRef and ValidationPlan", func(t *testing.T) {
		data := &RoleContextData{
			Role: "code-reviewer", AgentID: "code-reviewer-1", RoleType: "reviewer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-1",
			IterationNum: 1,
			BaseCommit:   "abc", ReviewCommit: "def", AssignedTo: "coder-1",
			PlanRef:        "specs/plans/20260317-plan.md",
			ValidationPlan: "run go test ./... and verify all pass",
			ProjectRoot:    projectRoot,
		}
		sections, _ := resolver.ContextSections("code-reviewer")
		output, err := BuildRoleContext("code-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "specs/plans/20260317-plan.md") {
			t.Error("output missing PlanRef path")
		}
		if !strings.Contains(output, "DOER VALIDATION PLAN:") {
			t.Error("output missing DOER VALIDATION PLAN section")
		}
		if !strings.Contains(output, "run go test ./... and verify all pass") {
			t.Error("output missing validation plan content")
		}
	})

	t.Run("code-plan-reviewer with ValidationPlan", func(t *testing.T) {
		data := &RoleContextData{
			Role: "code-plan-reviewer", AgentID: "code-plan-reviewer-1", RoleType: "reviewer",
			TaskID: "task-1", Description: "Plan feature X",
			DoneWhen: "Plan approved", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-1",
			IterationNum: 1,
			BaseCommit:   "abc", ReviewCommit: "def", AssignedTo: "code-planner-1",
			ValidationPlan: "verify plan file exists and output[] populated",
			ProjectRoot:    projectRoot,
		}
		sections, _ := resolver.ContextSections("code-plan-reviewer")
		output, err := BuildRoleContext("code-plan-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "DOER VALIDATION PLAN:") {
			t.Error("output missing DOER VALIDATION PLAN section")
		}
		if !strings.Contains(output, "verify plan file exists and output[] populated") {
			t.Error("output missing validation plan content")
		}
	})

	t.Run("coder without PlanRef omits plan context", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:          projectRoot + "/.worktrees/task-1",
			IterationNum:      1,
			IntegrationBranch: "integration",
			ProjectRoot:       projectRoot,
		}
		sections, _ := resolver.ContextSections("coder")
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if strings.Contains(output, "implementation plan at") {
			t.Error("output should NOT contain plan reference when PlanRef is empty")
		}
	})
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

	t.Run("all cycle-blocked → SPRINT_COMPLETE not PLANNING_COMPLETE", func(t *testing.T) {
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

		if !strings.Contains(result, "WAKE TRIGGER: SPRINT_COMPLETE") {
			t.Errorf("expected SPRINT_COMPLETE (all planning is cycle-blocked), got:\n%s", result)
		}
		if strings.Contains(result, "PLANNING_COMPLETE") {
			t.Error("should NOT trigger PLANNING_COMPLETE when all planning is cycle-blocked")
		}
		if !strings.Contains(result, "Cycle-blocked planning: 1") {
			t.Error("expected cycle-blocked count in dashboard for operator visibility")
		}
	})
}

func TestCollectivePlanScoping_PhaseConsistencyRule(t *testing.T) {
	t.Run("with DependsOn matching same-role-pair sibling → renders phase-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "code-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    2,
			GoalSpecRef:    "specs/goal.md",
			TaskRolePair:   "code-planning-pair",
			DependsOn:      []string{"plan-1"},
			SiblingTasks: []SiblingTaskSummary{
				{ID: "plan-1", Description: "Phase 1 planning", Status: "MERGED", PlanRef: "specs/plan-phase1.md", RolePair: "code-planning-pair"},
			},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "PHASE CONSISTENCY RULE") {
			t.Error("expected phase-consistency rule to render")
		}
		if !strings.Contains(output, "plan-1") {
			t.Error("expected prior phase task ID in rule")
		}
		if !strings.Contains(output, "specs/plan-phase1.md") {
			t.Error("expected prior phase PlanRef in rule")
		}
		if !strings.Contains(output, "liza_mark_blocked") {
			t.Error("expected BLOCKED instruction in rule")
		}
	})

	t.Run("without DependsOn → no phase-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "code-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    1,
			GoalSpecRef:    "specs/goal.md",
			SiblingTasks: []SiblingTaskSummary{
				{ID: "plan-2", Description: "Phase 2 planning", Status: "DRAFT_CODING_PLAN"},
			},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if strings.Contains(output, "PHASE CONSISTENCY RULE") {
			t.Error("should NOT render phase-consistency rule without DependsOn")
		}
	})

	t.Run("DependsOn on different-role-pair sibling → no phase-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "code-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    2,
			GoalSpecRef:    "specs/goal.md",
			TaskRolePair:   "code-planning-pair",
			DependsOn:      []string{"coding-task-1"},
			SiblingTasks: []SiblingTaskSummary{
				{ID: "coding-task-1", Description: "Implement feature", Status: "MERGED", RolePair: "coding-pair"},
			},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if strings.Contains(output, "PHASE CONSISTENCY RULE") {
			t.Error("should NOT render phase-consistency rule for different-role-pair dependency")
		}
	})

	t.Run("epic-planner role branch", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "epic-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    1,
			GoalSpecRef:    "specs/goal.md",
			SiblingTasks: []SiblingTaskSummary{
				{ID: "plan-2", Description: "Phase 2", Status: "DRAFT_EPIC_PLAN"},
			},
		}

		output, err := BuildRoleContext("epic-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "Do NOT plan capabilities that belong to a sibling phase") {
			t.Error("expected epic-planner scope restriction")
		}
	})

	t.Run("epic-plan-reviewer role branch", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "epic-plan-reviewer",
			RoleType:       "reviewer",
			TotalPlanTasks: 2,
			TaskOrdinal:    1,
			GoalSpecRef:    "specs/goal.md",
			SiblingTasks: []SiblingTaskSummary{
				{ID: "plan-2", Description: "Phase 2", Status: "DRAFT_EPIC_PLAN"},
			},
		}

		output, err := BuildRoleContext("epic-plan-reviewer", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "epic stays within scope") {
			t.Error("expected epic-plan-reviewer scope verification language")
		}
	})
}
