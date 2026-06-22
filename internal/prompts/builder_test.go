package prompts

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

const awaitResubmissionPassiveGuidance = "If the harness backgrounds await-resubmission and says it will notify on completion, end the turn; do NOT call Monitor, search for Monitor, ScheduleWakeup, or read/tail/sleep/poll the output file."
const awaitResubmissionBoundaryGuidance = "On RESUBMITTED, use the returned `base_commit` and `review_commit` for every diff command in this same session."
const validationCommandShapeRule = "Forbidden validation command shapes: `cd ... &&`, command substitution/backticks, polling or tail pipelines, and task artifact paths outside the worktree."
const validationFallback = "If a stored validation command violates BASH CONSTRAINTS, do not execute it literally; treat it as validation intent, run an equivalent single-purpose command from the worktree/tool working directory, and record both the original command and translated command in validation evidence."

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

func assertAwaitResubmissionPassiveGuidance(t *testing.T, output string, wantGuidanceLines int) {
	t.Helper()

	var guidanceLines []string
	var monitorLines []string
	var scheduleWakeupLines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, awaitResubmissionPassiveGuidance) {
			guidanceLines = append(guidanceLines, line)
		}
		if strings.Contains(strings.ToLower(line), "monitor") {
			monitorLines = append(monitorLines, line)
		}
		if strings.Contains(line, "ScheduleWakeup") {
			scheduleWakeupLines = append(scheduleWakeupLines, line)
		}
	}
	if len(guidanceLines) != wantGuidanceLines {
		t.Fatalf("output contains %d await-resubmission passive guidance lines, want %d: %#v", len(guidanceLines), wantGuidanceLines, guidanceLines)
	}
	if len(monitorLines) != wantGuidanceLines {
		t.Fatalf("output contains %d Monitor guidance lines, want %d negative lines: %#v", len(monitorLines), wantGuidanceLines, monitorLines)
	}
	for _, line := range monitorLines {
		if !strings.Contains(strings.ToLower(line), "do not") {
			t.Errorf("Monitor guidance line is not negative: %q", line)
		}
	}
	if len(scheduleWakeupLines) != wantGuidanceLines {
		t.Fatalf("output contains %d ScheduleWakeup guidance lines, want %d negative lines: %#v", len(scheduleWakeupLines), wantGuidanceLines, scheduleWakeupLines)
	}
	for _, line := range scheduleWakeupLines {
		if !strings.Contains(strings.ToLower(line), "do not") {
			t.Errorf("ScheduleWakeup guidance line is not negative: %q", line)
		}
	}
}

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
				"APPROVED: use CLI commands with escalated permissions",
				"TWO brand data directories exist",
				"~/.liza/ = installed contracts & skills",
				"/project/.liza/ = runtime state & blackboard",
				"Do NOT create, edit, stage, or commit files under /project/.liza/agent-outputs/",
				"runtime log state owned by Liza",
				"You have FULL read access to both brand data directories",
				"For READING state: use liza get --json",
				"For MODIFYING state: use role-specific CLI commands ONLY",
				"NEVER edit state.yaml directly",
				"Execute commands immediately",
				"DO proceed with tool execution",
				"QUERY TOOLS",
				"liza get --json",
				"liza status --json",
				"liza validate --json",
				"COMMUNICATION:",
				"FORBIDDEN:",
				"Do NOT attempt to claim tasks",
				"SESSION EXIT CODES",
				"TIMESTAMPS:",
				"FIRST ACTIONS:",
				`Query your assigned task: liza get task-1 --json`,
				"Read the current spec reference:",
				"Use the assigned task JSON from step 2. Read its `spec_ref`",
				"if it is repo-relative and the task has a worktree, read it from that worktree",
				"If `spec_ref` is empty, run `liza get goal.spec_ref --json`",
				"lessons/agents/",
				"GUARDRAILS.md",
			},
			wantNotContain: []string{
				// Role-specific tools should NOT be in base prompt
				"liza_add_tasks",
				"liza_submit_for_review",
				"liza_submit_verdict",
				"=== SCIP-SEARCH INDEXES ===",
				"=== STACKLIT INDEX ===",
				"=== SEMBLE SEARCH ===",
				"=== QUERY ROUTING ===",
				"Read the goal spec: specs/vision.md",
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
				"Use the orchestrator dashboard and active-task digest below first",
				"Query specific tasks with liza get <task-id> --json",
				"Run `liza get goal.spec_ref --json`, then read the returned ref from the project root",
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

func TestBuildBasePromptUsesDistinctBrandDirectories(t *testing.T) {
	withPromptBrandValues(t, func() {
		brand.NameTitle = "Acme"
		brand.BinaryName = "acme"
		brand.GlobalDirName = ".acme-home"
		brand.ProjectDirName = ".acme-state"
	})

	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.acme-state/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	for _, want := range []string{
		"You are a Acme code-coder agent",
		"TWO brand data directories exist",
		"~/.acme-home/ = installed contracts & skills",
		"/project/.acme-state/ = runtime state & blackboard",
		"Do NOT create, edit, stage, or commit files under /project/.acme-state/agent-outputs/",
		"You have FULL read access to both brand data directories",
		"For READING state: use acme get --json",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildBasePrompt() missing expected branded content %q:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{
		"TWO .acme-state/ directories exist",
		"~/.acme-state/ = installed contracts & skills",
		"You have FULL read access to both .acme-state/ directories",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered misleading directory guidance %q:\n%s", notWant, prompt)
		}
	}
}

func TestBuildBasePromptScipSearchOmittedWhenNoIndexes(t *testing.T) {
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
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

	for _, notWant := range []string{
		"=== SCIP-SEARCH INDEXES ===",
		"scip-search symbols",
		"scip-search references",
		"Generated SCIP indexes are snapshots",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered scip-search content %q with no supplied indexes", notWant)
		}
	}
}

func TestBuildBasePromptScipSearchRendersSuppliedIndexes(t *testing.T) {
	goPath := "/abs/worktree with spaces/.liza/scip/go.scip"
	tsPath := "/abs/worktree/.liza/scip/typescript.scip"
	pythonPath := "/abs/worktree/.liza/scip/python.scip"
	quotedGoPath := "'/abs/worktree with spaces/.liza/scip/go.scip'"
	quotedTSPath := "'/abs/worktree/.liza/scip/typescript.scip'"
	quotedPythonPath := "'/abs/worktree/.liza/scip/python.scip'"
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
		ScipSearchIndexes: []ScipSearchIndex{
			{Language: "python", IndexPath: pythonPath},
			{Language: "go", IndexPath: goPath},
			{Language: "typescript", IndexPath: tsPath},
		},
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	assertContains := func(want string) {
		t.Helper()
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildBasePrompt() missing expected scip-search content:\n%q", want)
		}
	}

	assertContains("=== SCIP-SEARCH INDEXES ===")
	assertContains("Generated SCIP indexes were refreshed before this prompt was built and reflect the current target tree at prompt construction time; they will not reflect subsequent agent edits.")
	assertContains("Use `~/.liza/AGENT_TOOLS.md` for `scip-search` command syntax, routing rules, and freshness caveats.")

	for _, command := range []string{
		"scip-search symbols --index",
		"scip-search references --index",
		"scip-search implementations --index",
		"nl -ba <result-path>",
	} {
		if strings.Contains(prompt, command) {
			t.Fatalf("BuildBasePrompt() rendered reusable scip-search command syntax %q:\n%s", command, prompt)
		}
	}
	if strings.Contains(prompt, "--index <") {
		t.Fatalf("BuildBasePrompt() rendered placeholder index paths:\n%s", prompt)
	}

	goPosition := strings.Index(prompt, "Go index: "+quotedGoPath)
	tsPosition := strings.Index(prompt, "TypeScript index: "+quotedTSPath)
	pythonPosition := strings.Index(prompt, "Python index: "+quotedPythonPath)
	if goPosition == -1 || tsPosition == -1 || pythonPosition == -1 {
		t.Fatalf("BuildBasePrompt() did not render every index label; positions go=%d ts=%d python=%d", goPosition, tsPosition, pythonPosition)
	}
	if !(goPosition < tsPosition && tsPosition < pythonPosition) {
		t.Fatalf("BuildBasePrompt() rendered indexes out of order: go=%d ts=%d python=%d", goPosition, tsPosition, pythonPosition)
	}
}

func TestBuildBasePromptStacklitOmittedWhenNoIndexes(t *testing.T) {
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
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

	for _, notWant := range []string{
		"=== STACKLIT INDEX ===",
		"stacklit derive",
		"stacklit get-module",
		"No Stacklit index path was supplied for this target.",
		"Do not infer or generate stacklit.json",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered stacklit content %q with no supplied indexes", notWant)
		}
	}
}

func TestBuildBasePromptStacklitRendersSuppliedIndex(t *testing.T) {
	indexPath := "/abs/worktree with spaces/stacklit.json"
	quotedPath := "'/abs/worktree with spaces/stacklit.json'"
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
		StacklitIndexes: []StacklitIndex{
			{IndexPath: indexPath},
		},
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	for _, want := range []string{
		"=== STACKLIT INDEX ===",
		"Stacklit index: " + quotedPath,
		"Stacklit index files are available for this target. They are repository snapshots that may lag behind current edits or failed refresh attempts; use them for orientation, then verify against source files before editing.",
		"Use `~/.liza/AGENT_TOOLS.md` for Stacklit command syntax, routing rules, and freshness caveats.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildBasePrompt() missing expected stacklit content:\n%q", want)
		}
	}
	if strings.Contains(prompt, "stacklit derive --ai-summary -i "+quotedPath) ||
		strings.Contains(prompt, "stacklit get-module <module>") {
		t.Fatalf("BuildBasePrompt() rendered reusable Stacklit command syntax:\n%s", prompt)
	}
}

func TestBuildBasePromptStacklitAndScipUnifiedQueryRouting(t *testing.T) {
	stacklitPath := "/project/.worktrees/task-1/stacklit.json"
	scipPath := "/project/.worktrees/task-1/index.scip"
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
		StacklitIndexes: []StacklitIndex{
			{IndexPath: stacklitPath},
		},
		ScipSearchIndexes: []ScipSearchIndex{
			{Language: "Go", IndexPath: scipPath},
		},
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	for _, want := range []string{
		"Stacklit index: '" + stacklitPath + "'",
		"Go index: '" + scipPath + "'",
		"Use `~/.liza/AGENT_TOOLS.md` for Stacklit command syntax, routing rules, and freshness caveats.",
		"Use `~/.liza/AGENT_TOOLS.md` for `scip-search` command syntax, routing rules, and freshness caveats.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("missing supplied index guidance %q", want)
		}
	}
	if strings.Contains(prompt, "=== QUERY ROUTING ===") {
		t.Fatal("prompt should defer reusable query routing guidance to AGENT_TOOLS.md")
	}
}

func TestBuildBasePromptSembleSearchOmittedWhenNoMetadata(t *testing.T) {
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
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

	for _, notWant := range []string{
		"=== SEMBLE SEARCH ===",
		"semble search",
		"semble find-related",
		"Semble returns candidate chunks, not proof",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered Semble content %q with no supplied metadata", notWant)
		}
	}
}

func TestBuildBasePromptSembleSearchRendersPromptMetadata(t *testing.T) {
	targetRoot := "/abs/worktree with spaces"
	quotedRoot := "'/abs/worktree with spaces'"
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
		SembleSearch: SembleSearchMetadata{
			TargetRoot:      targetRoot,
			ShellTargetRoot: quotedRoot,
		},
		StacklitIndexes: []StacklitIndex{{IndexPath: "/abs/worktree with spaces/stacklit.json"}},
		ScipSearchIndexes: []ScipSearchIndex{
			{Language: "go", IndexPath: "/abs/worktree with spaces/.liza/scip/go.scip"},
		},
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	for _, want := range []string{
		"=== SEMBLE SEARCH ===",
		"Semble is available for semantic repository search in this target root:",
		quotedRoot,
		"Use `~/.liza/AGENT_TOOLS.md` for Semble command syntax, content modes, routing rules, and proof requirements.",
		"Stacklit index: '/abs/worktree with spaces/stacklit.json'",
		"Go index: '/abs/worktree with spaces/.liza/scip/go.scip'",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildBasePrompt() missing expected Semble prompt content:\n%q", want)
		}
	}
	for _, notWant := range []string{
		`env HF_HUB_OFFLINE=1 semble search "where is review submission validated?" ` + quotedRoot,
		"env HF_HUB_OFFLINE=1 semble find-related <file_path> <line> " + quotedRoot,
		`semble search "where is review submission validated?" ` + quotedRoot,
		"semble find-related <file_path> <line> " + quotedRoot,
		"stacklit derive --ai-summary -i",
		"scip-search symbols --index",
		"=== QUERY ROUTING ===",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered reusable command/routing guidance %q:\n%s", notWant, prompt)
		}
	}
	if strings.Contains(prompt, "Morph MCP semantic search: broad conceptual search") {
		t.Fatal("Morph must not be positioned as the primary semantic search tool")
	}
}

func TestBuildBasePromptSembleOnlyRoutingOmitsUnavailableOptionalTools(t *testing.T) {
	quotedRoot := "'/abs/worktree with spaces'"
	config := BasePromptConfig{
		Role:        "code-coder",
		AgentID:     "coder-1",
		TaskID:      "task-1",
		SpecsDir:    "/project/specs",
		ProjectRoot: "/project",
		StatePath:   "/project/.liza/state.yaml",
		GoalDesc:    "Build a web API",
		GoalSpecRef: "specs/vision.md",
		SembleSearch: SembleSearchMetadata{
			TargetRoot:      "/abs/worktree with spaces",
			ShellTargetRoot: quotedRoot,
		},
	}

	prompt, err := BuildBasePrompt(config)
	if err != nil {
		t.Fatalf("BuildBasePrompt() error: %v", err)
	}

	for _, want := range []string{
		"=== SEMBLE SEARCH ===",
		"Use `~/.liza/AGENT_TOOLS.md` for Semble command syntax, content modes, routing rules, and proof requirements.",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("BuildBasePrompt() missing Semble-only routing guidance:\n%q", want)
		}
	}
	for _, notWant := range []string{
		"=== STACKLIT INDEX ===",
		"=== SCIP-SEARCH INDEXES ===",
		"stacklit derive",
		"scip-search symbols",
		"=== QUERY ROUTING ===",
	} {
		if strings.Contains(prompt, notWant) {
			t.Fatalf("BuildBasePrompt() rendered unavailable optional-tool guidance %q:\n%s", notWant, prompt)
		}
	}
}

func TestRenderOrchestratorDashboard(t *testing.T) {
	now := time.Now().UTC()
	projectRoot := setupPipelineConfig(t)

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
				"liza add-tasks",
				"liza assess-blocked",
				"liza supersede-task",
				`liza wt-delete`,
				`liza supersede-task <task-id> [replacement-task-ids] --reason "..." --agent-id "orchestrator-1" --json`,
				`liza supersede-task <task-id> --reason "Work completed externally" --recoverability-command "liza recover-task <task-id>" --agent-id "orchestrator-1" --json`,
				`liza wt-delete <task-id> --json`,
				`liza sprint-checkpoint — Create sprint checkpoint for human review`,
				`liza update-sprint-metrics — Recompute sprint metrics`,
				"This is initial planning",
				"Classify the input document and choose the appropriate entry-point",
				"AVAILABLE ENTRY-POINTS:",
				"Create exactly one planning task",
				"Choose the chosen entry-point's fan-out target for a fan-out or uncertain goal when it is listed.",
				"Fan-out or uncertain goals use the chosen fan-out role_pair when available.",
			},
			wantNotContain: []string{
				`--replacement-ids`,
				`liza wt-delete <task-id> --agent-id`,
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
				"Write a tasks JSON file as a bare array",
				`"desc": "<short task description>"`,
				`"done": "<falsifiable completion condition>"`,
				`"role_pair": "coding-pair"`,
				"liza supersede-task <task-id> <new-id-1>,<new-id-2>",
				"liza assess-blocked",
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
				"Write a tasks JSON file as a bare array",
				`"desc": "<short task description>"`,
				`"done": "<falsifiable completion condition>"`,
				`"role_pair": "coding-pair"`,
				"liza supersede-task <old-id> <new-id-1>,<new-id-2>",
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
				`liza sprint-checkpoint`,
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
				"Planning sprint tasks have been merged with output[] entries",
				"No manual task creation is needed.",
				"Pipeline transitions will execute automatically after checkpoint and human resume",
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
				"Planning sprint tasks have been merged with output[] entries",
				"Pipeline transitions will execute automatically after checkpoint and human resume",
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
				`liza update-sprint-metrics --json`,
				`liza sprint-checkpoint --json`,
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
			name:       "explicit entry-point functional-spec dispatches to architecture-pair",
			entryPoint: "functional-spec",
			wantContains: []string{
				"WAKE TRIGGER: INITIAL_PLANNING",
				"role_pair\": \"architecture-pair\"",
				"Architect",
				"\"type\": \"architecture\"",
			},
			wantNotContain: []string{
				"classify",
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
				"classify",
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
				"classify",
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
			got := resolveTaskType(resolver, tt.rolePair)
			if got != tt.want {
				t.Errorf("resolveTaskType(%q) = %q, want %q", tt.rolePair, got, tt.want)
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
		"TWO brand data directories exist",
		"~/.liza/ = installed contracts & skills",
		"/project/.liza/ = runtime state & blackboard",
		"Do NOT create, edit, stage, or commit files under /project/.liza/agent-outputs/",
		"runtime log state owned by Liza",
		"FULL read access to both brand data directories",
	})

	// --- STATE ACCESS: liza get over state.yaml ---
	assertSection("state-access", []string{
		"use liza get --json",
		"NEVER read state.yaml directly",
		"liza get --json returns only the requested slice",
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
		"NEVER use `cd <path> && ...` to locate a command",
		"per-command directory flags",
		"execution tool's working-directory option",
		"NEVER combine cd and git in one command",
		"git -C <path> <cmd>",
		"go -C <path> <cmd>",
		"make -C <path> <target>",
		"npm --prefix <path> <script>",
		"cargo test --manifest-path <path>/Cargo.toml",
		"NEVER use $() command substitution",
		"ANSI-C quoting",
		"NEVER install OS packages, language runtimes, IDE tooling",
		"Project-level config files",
		"mark BLOCKED rather than mutating the host environment",
		`NEVER use "git add -A" or "git add ."`,
		"stage specific files by name",
		"generated dependency/cache directories from broad Glob/rg traversals",
		"exclude node_modules with",
		`--glob '!**/node_modules/**'`,
		"sed/awk for file editing",
	})

	// --- QUERY TOOLS: available to all roles ---
	assertSection("query-tools", []string{
		"QUERY TOOLS",
		"invoke Liza state commands as `liza -C /project ...`",
		"If `-C` is unavailable",
		"`liza -C /project ...`",
		"task worktree or any directory inside it",
		"relative file arguments still resolve against the process working directory",
		"task worktree for file, git, build, and test operations",
		"liza get --json",
		"liza get <id> --output-summary --json",
		"liza status --json",
		"liza validate --json",
	})

	// --- COMMUNICATION: blackboard-only ---
	assertSection("communication", []string{
		"Agents communicate via blackboard only",
		"CLI commands",
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
		`Query your assigned task: liza get task-42 --json`,
		"Read the current spec reference:",
		"Use the assigned task JSON from step 2. Read its `spec_ref`",
		"If `spec_ref` is empty, run `liza get goal.spec_ref --json`",
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
				"liza add-tasks --tasks-file",
				`"desc": "<short task description>"`,
				`"done": "<falsifiable completion condition>"`,
				"liza supersede-task <task-id> <new-id-1>,<new-id-2>",
				"run commands NOW",
				"Do NOT call liza sprint-checkpoint",
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
				`"desc": "<short task description>"`,
				`"done": "<falsifiable completion condition>"`,
				"liza supersede-task <old-id> <new-id-1>,<new-id-2>",
				"Execute changes",
				"create them all in this session",
				"All state modifications must be executed before you exit",
				"Do NOT call liza sprint-checkpoint",
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
				"liza add-tasks --tasks-file",
				"liza set-discovery-disposition",
				"All commands must be executed in this session",
				"Do NOT call liza sprint-checkpoint",
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
				"FULL autonomy to run CLI commands immediately",
				"liza sprint-checkpoint",
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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/mandatory_docs.tmpl"))

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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/mandatory_docs.tmpl"))

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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/mandatory_docs.tmpl"))

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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/skills_affinity.tmpl"))

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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/skills_affinity.tmpl"))

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
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/skills_affinity.tmpl"))

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

func TestBlockParentTasksContext_WithEntries(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles(
		"templates/blocks/artifact_ref_fallback.tmpl",
		"templates/blocks/parent_tasks_context.tmpl",
	))

	data := RoleContextData{
		ParentTaskContexts: []ParentTaskContext{
			{
				ID:          "us-1",
				Description: "User can sign up",
				DoneWhen:    "Signup flow works end-to-end",
				SpecRef:     "specs/goals/feature-x.md",
				PlanRef:     "specs/plans/signup.md",
			},
			{
				ID:          "us-2",
				Description: "User can reset password",
				DoneWhen:    "Password reset flow works",
				SpecRef:     "specs/goals/feature-x.md",
			},
		},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "parent-tasks-context", &data)
	if err != nil {
		t.Fatalf("failed to execute parent-tasks-context template: %v", err)
	}

	output := buf.String()

	for _, key := range []string{
		"PARENT TASKS (2)",
		"--- us-1 ---",
		"DESCRIPTION: User can sign up",
		"DONE WHEN: Signup flow works end-to-end",
		"SPEC: specs/goals/feature-x.md",
		"PLAN: specs/plans/signup.md",
		"--- us-2 ---",
		"DESCRIPTION: User can reset password",
		"DONE WHEN: Password reset flow works",
	} {
		if !strings.Contains(output, key) {
			t.Errorf("output missing key string %q", key)
		}
	}

	// us-2 has no PlanRef — should not render PLAN line for it
	// Count PLAN occurrences: should be exactly 1 (from us-1)
	if strings.Count(output, "PLAN:") != 1 {
		t.Errorf("expected exactly 1 PLAN line, got %d", strings.Count(output, "PLAN:"))
	}
}

func TestBlockParentTasksContext_Empty(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles(
		"templates/blocks/artifact_ref_fallback.tmpl",
		"templates/blocks/parent_tasks_context.tmpl",
	))

	data := RoleContextData{
		ParentTaskContexts: nil,
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "parent-tasks-context", &data)
	if err != nil {
		t.Fatalf("failed to execute parent-tasks-context template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for nil ParentTaskContexts, got %q", buf.String())
	}
}

func TestBlockParentTasksContext_EmptySlice(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles(
		"templates/blocks/artifact_ref_fallback.tmpl",
		"templates/blocks/parent_tasks_context.tmpl",
	))

	data := RoleContextData{
		ParentTaskContexts: []ParentTaskContext{},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "parent-tasks-context", &data)
	if err != nil {
		t.Fatalf("failed to execute parent-tasks-context template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output for empty ParentTaskContexts slice, got %q", buf.String())
	}
}

func TestBuildRoleContext_MasterDecompositionMandate(t *testing.T) {
	data := &RoleContextData{
		Role:                 "architect",
		RoleType:             "doer",
		DecompositionRoot:    true,
		MasterOutputRefField: "arch_ref",
	}

	output, err := BuildRoleContext("architect", []string{"master-decomposition-mandate"}, data)
	if err != nil {
		t.Fatalf("BuildRoleContext: %v", err)
	}

	for _, want := range []string{
		"=== MASTER DECOMPOSITION MANDATE ===",
		"Master Output Contract properties 1-6",
		"1. Non-overlapping scopes.",
		"2. Interface ownership.",
		"3. Shared-file ownership.",
		"4. Dependency ordering.",
		"5. Inherited constraints.",
		"6. Completeness.",
		"`arch_ref`",
		"`Systemic Decomposition Review`",
		"systemic-thinking",
		"before `liza set-task-output` or submission",
		"owned_files",
		"owned_modules",
		"read_only_depends_on",
		"read_only_task_depends_on",
		"interfaces_owned",
		"interfaces_consumed",
		"coverage_notes",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q", want)
		}
	}
}

func TestBuildRoleContext_MasterDecompositionReview(t *testing.T) {
	data := &RoleContextData{
		Role:                 "architecture-reviewer",
		RoleType:             "reviewer",
		DecompositionRoot:    true,
		MasterOutputRefField: "arch_ref",
	}

	output, err := BuildRoleContext("architecture-reviewer", []string{"master-decomposition-review"}, data)
	if err != nil {
		t.Fatalf("BuildRoleContext: %v", err)
	}

	for _, want := range []string{
		"=== MASTER DECOMPOSITION REVIEW ===",
		"Invoke `systemic-thinking` before submitting a verdict",
		"missing `arch_ref`",
		"missing typed decomposition metadata",
		"missing systemic-thinking evidence",
		"violates any Master Output Contract property",
		"1. Non-overlapping scopes.",
		"2. Interface ownership.",
		"3. Shared-file ownership.",
		"4. Dependency ordering.",
		"5. Inherited constraints.",
		"6. Completeness.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q", want)
		}
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
			"CODER TOOLS:",
			"liza submit-for-review",
			"liza handoff",
			"liza mark-blocked",
			"--depends-on",
			"ANOMALY LOGGING:",
			"BLOCKING PROTOCOL:",
			"WORKTREE RULES:",
			"Supervisor verified this worktree before launch; do not run a standalone `/usr/bin/test -d " + data.Worktree + "` probe.",
			"If later worktree operations report the path missing, stop and report worktree drift.",
			"git -C " + data.Worktree,
			"COMMIT WORKFLOW:",
			"IMPLEMENTATION PHASE",
			"Dependency bootstrap exception",
			"liza get config.post_worktree_cmd --json",
			"SUBMISSION (MANDATORY",
			"Submission requires a new worktree commit for this task",
			"exact failing command and stderr/error text",
			"Do NOT submit the pre-change HEAD",
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION FEEDBACK (MUST ADDRESS)",
			"Missing error handling",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		if strings.Contains(output, "cd "+data.Worktree+" &&") {
			t.Error("coder prompt must not teach cd && worktree commands")
		}
		if strings.Contains(output, "First, run: /usr/bin/test -d") {
			t.Error("coder prompt must not require a standalone worktree existence probe")
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
			BaseCommit: "abc1234", ReviewCommit: "def5678", IntegrationBranch: "integration", AssignedTo: "coder-1",
			GoalSpecRef:    "specs/goal.md",
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
			"INTEGRATION BRANCH: integration",
			"REVIEW COMMIT: def5678",
			"AUTHOR: coder-1",
			"REVIEWER STATE TRANSITIONS:",
			"REVIEWING_CODE",
			"CODE_APPROVED",
			"REVIEWER TOOLS:",
			"liza submit-verdict",
			"liza await-resubmission",
			awaitResubmissionBoundaryGuidance,
			"ANOMALY LOGGING:",
			"WORKTREE RULES:",
			"Supervisor verified this worktree before launch; do not run a standalone `/usr/bin/test -d " + data.Worktree + "` probe.",
			"If later worktree operations report the path missing, stop and report worktree drift.",
			"Run tests from the worktree without `cd &&`",
			"REVIEW SCOPE:",
			"Changed-file map and stat first:",
			"git -C " + data.Worktree + " diff --name-only abc1234..def5678",
			"git -C " + data.Worktree + " diff --stat abc1234..def5678",
			"supporting rg/glob only after",
			"Review ALL changes by targeted path/hunk",
			"git -C " + data.Worktree + " diff integration..def5678",
			"Scope findings come exclusively from this scope/workmanship diff",
			"review-range drift / rebase needed",
			"Do not invent or log a new anomaly type for review-range drift",
			"REJECTION FORMAT",
			"VERDICT SUBMISSION",
			"COLLECTIVE PLAN SCOPING",
			"PRIOR REJECTION (iteration 1)",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		if strings.Contains(output, "cd "+data.Worktree+" &&") {
			t.Error("code-reviewer prompt must not teach cd && worktree commands")
		}
		if strings.Contains(output, "First, run: /usr/bin/test -d") {
			t.Error("code-reviewer prompt must not require a standalone worktree existence probe")
		}
		assertAwaitResubmissionPassiveGuidance(t, output, 2)
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
			"ORCHESTRATOR COMMANDS:",
			"liza add-tasks",
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
			GoalSpecRef:    "specs/goal.md",
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
			"CODE PLANNER TOOLS:",
			"liza set-task-output",
			"WORKTREE RULES:",
			"TASK DECOMPOSITION PRINCIPLE:",
			"IMPLEMENTATION PHASE:",
			"You are planning, not implementing",
			"plan from the provided spec artifacts",
			"As code-planner, do not modify those files",
			"TIMESTAMP-task-planner.md", // canonical plan file path with task ID
			"TIMESTAMP-task-planner-output.json",
			"Update only planning artifacts required by DONE WHEN",
			"validation (optional canonical commands",
			"Verify any validation[] command is character-identical to the plan",
			"Submission requires a new worktree commit for this task",
			"Submission proof: `liza submit-for-review` is not optional bookkeeping",
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
			GoalSpecRef:    "specs/goal.md",
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
			"CODE PLAN REVIEWER TOOLS:",
			"liza submit-verdict",
			"liza await-resubmission",
			awaitResubmissionBoundaryGuidance,
			"REVIEW CHECKLIST:",
			"TIMESTAMP-task-cpr",           // interpolated task ID in reviewer gate
			"Plan file location",           // gate label present in checklist
			"Plan artifact not in diff at", // gate condition wording
			"Task-output JSON location",
			"any committed task-output JSON appears under .liza/agent-outputs/",
			"validation satisfiable",
			"durable plan/task text",
			"Changed-file map and stat first:",
			"git -C " + data.Worktree + " diff --name-only abc1234..def5678",
			"git -C " + data.Worktree + " diff --stat abc1234..def5678",
			"supporting rg/glob only after",
			"Inspect worktree changes by targeted path/hunk",
			"git -C " + data.Worktree + " diff abc1234..def5678 -- <path>",
			"VERDICT SUBMISSION",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		assertAwaitResubmissionPassiveGuidance(t, output, 1)
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
			"EPIC PLANNER TOOLS:",
			"liza set-task-output",
			"WORKTREE RULES:",
			"EPIC-WRITING SKILL:",
			"IMPLEMENTATION PHASE:",
			"Any validation[] command is satisfiable for the capability scope",
			"Submission requires a new worktree commit for this task",
			"Submission proof: `liza submit-for-review` must actually run successfully",
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
			"EPIC PLAN REVIEWER TOOLS:",
			"liza submit-verdict",
			"liza await-resubmission",
			awaitResubmissionBoundaryGuidance,
			"EPIC-WRITING SKILL:",
			"REVIEW CHECKLIST:",
			"durable check/hook intent",
			"Changed-file map and stat first:",
			"git -C " + data.Worktree + " diff --name-only abc1234..def5678",
			"git -C " + data.Worktree + " diff --stat abc1234..def5678",
			"supporting rg/glob only after",
			"Inspect worktree changes by targeted path/hunk",
			"git -C " + data.Worktree + " diff abc1234..def5678 -- <path>",
			"VERDICT SUBMISSION",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		assertAwaitResubmissionPassiveGuidance(t, output, 1)
	})

	t.Run("us-writer", func(t *testing.T) {
		_ = makeDoerTask("task-usw")
		data := &RoleContextData{
			Role: "us-writer", AgentID: "us-writer-1", RoleType: "doer",
			TaskID: "task-usw", Description: "Implement feature X",
			DoneWhen: "Feature X works correctly", Scope: "internal/feature",
			SpecRef:      "README.md",
			EpicRef:      "specs/epics/ep-001.md",
			EpicSection:  "capability-cap-001---task-creation",
			EpicSlug:     "ep-001",
			Worktree:     projectRoot + "/.worktrees/task-usw",
			IterationNum: 2, PriorRejection: "Missing error handling",
			GoalSpecRef:    "specs/goal.md",
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
			"US WRITER TOOLS:",
			"WORKTREE RULES:",
			"USER-STORY-WRITING SKILL:",
			"CAPABILITY SCOPING:",
			"IMPLEMENTATION PHASE:",
			"Submission requires a new worktree commit for this task",
			"PRE-SUBMIT SELF-CHECK (MANDATORY",
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
			EpicRef:      "specs/epics/ep-001.md",
			EpicSection:  "capability-cap-001---task-creation",
			EpicSlug:     "ep-001",
			Worktree:     projectRoot + "/.worktrees/task-usr",
			IterationNum: 2, PriorRejection: "Missing error handling",
			BaseCommit: "abc1234", ReviewCommit: "def5678", AssignedTo: "coder-1",
			GoalSpecRef:    "specs/goal.md",
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
			"US REVIEWER TOOLS:",
			"liza submit-verdict",
			"liza await-resubmission",
			awaitResubmissionBoundaryGuidance,
			"SPEC-REVIEW SKILL:",
			"USER-STORY ANTI-PATTERNS",
			"QUALITY GATES:",
			"CAPABILITY SCOPING:",
			"Changed-file map and stat first:",
			"git -C " + data.Worktree + " diff --name-only abc1234..def5678",
			"git -C " + data.Worktree + " diff --stat abc1234..def5678",
			"supporting rg/glob only after",
			"Inspect worktree changes by targeted path/hunk",
			"git -C " + data.Worktree + " diff abc1234..def5678 -- <path>",
			"VERDICT SUBMISSION",
			"specs/epics/ep-001.md",
			"#capability-cap-001---task-creation",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		assertAwaitResubmissionPassiveGuidance(t, output, 1)
	})

	t.Run("architect", func(t *testing.T) {
		_ = makeDoerTask("task-arch")
		data := &RoleContextData{
			Role: "architect", AgentID: "architect-1", RoleType: "doer",
			TaskID: "task-arch", Description: "Define architecture for feature X",
			DoneWhen: "Architecture document covers all components", Scope: "specs/arch-plan",
			SpecRef:      "specs/goals/feature-x.md",
			Worktree:     projectRoot + "/.worktrees/task-arch",
			IterationNum: 1,
			ParentTaskContexts: []ParentTaskContext{
				{ID: "us-1", Description: "User story 1", DoneWhen: "US1 done", SpecRef: "specs/goals/feature-x.md"},
				{ID: "us-2", Description: "User story 2", DoneWhen: "US2 done", SpecRef: "specs/goals/feature-x.md"},
			},
			ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("architect")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("architect", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED ARCHITECTURE TASK ===",
			"TASK ID: task-arch",
			"ARCHITECT STATE TRANSITIONS:",
			"ARCHITECTING",
			"ARCHITECTURE_TO_REVIEW",
			"ARCHITECT TOOLS:",
			"liza set-task-output",
			"arch_ref",
			"IMPLEMENTATION PHASE:",
			"Architecture document",
			"specs/arch-plan",
			"As architect, do not modify those files",
			"Update only architecture artifacts required by DONE WHEN",
			"Any validation[] command is satisfiable for the generated scope",
			"Submission requires a new worktree commit for this task",
			"Submission proof: `liza submit-for-review` must actually run successfully after step 9g",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
	})

	t.Run("architecture-reviewer", func(t *testing.T) {
		_ = makeReviewerTask("task-archr")
		data := &RoleContextData{
			Role: "architecture-reviewer", AgentID: "architecture-reviewer-1", RoleType: "reviewer",
			TaskID: "task-archr", Description: "Review architecture for feature X",
			DoneWhen: "Architecture is coherent and complete", Scope: "specs/arch-plan",
			Worktree:     projectRoot + "/.worktrees/task-archr",
			IterationNum: 1,
			BaseCommit:   "abc1234", ReviewCommit: "def5678", AssignedTo: "architect-1",
			ProjectRoot: projectRoot,
		}
		sections, err := resolver.ContextSections("architecture-reviewer")
		if err != nil {
			t.Fatalf("ContextSections: %v", err)
		}
		output, err := BuildRoleContext("architecture-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		for _, key := range []string{
			"=== ASSIGNED ARCHITECTURE REVIEW TASK ===",
			"TASK ID: task-archr",
			"ARCHITECTURE REVIEWER STATE TRANSITIONS:",
			"REVIEWING_ARCHITECTURE",
			"ARCHITECTURE REVIEWER TOOLS:",
			"liza submit-verdict",
			"liza await-resubmission",
			awaitResubmissionBoundaryGuidance,
			"REVIEW CHECKLIST:",
			"Decomposition completeness",
			"Composability",
			"durable architecture/task text",
			"Changed-file map and stat first:",
			"git -C " + data.Worktree + " diff --name-only abc1234..def5678",
			"git -C " + data.Worktree + " diff --stat abc1234..def5678",
			"supporting rg/glob only after",
			"Inspect worktree changes by targeted path/hunk",
			"git -C " + data.Worktree + " diff abc1234..def5678 -- <path>",
			"VERDICT SUBMISSION",
		} {
			if !strings.Contains(output, key) {
				t.Errorf("output missing key string %q", key)
			}
		}
		assertAwaitResubmissionPassiveGuidance(t, output, 1)
	})
}

func TestBuildRoleContext_AwaitVerdictLoopRendersForAllDoers(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	doerRoles := []struct {
		role    string
		agentID string
		taskID  string
	}{
		{"coder", "coder-1", "task-avl-coder"},
		{"architect", "architect-1", "task-avl-arch"},
		{"code-planner", "code-planner-1", "task-avl-cp"},
		{"epic-planner", "epic-planner-1", "task-avl-ep"},
		{"us-writer", "us-writer-1", "task-avl-usw"},
		{"integration-analyst", "integration-analyst-1", "task-avl-ia"},
	}

	awaitVerdictKeys := []string{
		"liza await-verdict",
		"POLL",
		"timeout_seconds",
		"sole polling primitive",
		"Call await-verdict at most 3 times",
		"If the harness backgrounds await-verdict and says it will notify on completion, end the turn; do NOT call Monitor, search for Monitor, ScheduleWakeup, or read/tail/sleep/poll the output file.",
		"Do NOT poll liza get",
		"Do NOT run more worktree commands after APPROVED, TERMINAL, or ALREADY_TRANSITIONED",
	}
	for _, tc := range doerRoles {
		t.Run(tc.role, func(t *testing.T) {
			data := &RoleContextData{
				Role: tc.role, AgentID: tc.agentID, RoleType: "doer",
				TaskID: tc.taskID, Description: "Test task",
				DoneWhen: "Done", Scope: "internal/test",
				Worktree:          projectRoot + "/.worktrees/" + tc.taskID,
				IterationNum:      1,
				IntegrationBranch: "integration",
				ProjectRoot:       projectRoot,
			}
			sections, err := resolver.ContextSections(tc.role)
			if err != nil {
				t.Fatalf("ContextSections: %v", err)
			}
			output, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatalf("BuildRoleContext: %v", err)
			}

			for _, key := range awaitVerdictKeys {
				if !strings.Contains(output, key) {
					t.Errorf("output missing await-verdict loop key %q", key)
				}
			}
			if tc.role == "integration-analyst" &&
				!strings.Contains(output, "fix-task desc, done_when, or supporting artifact text") {
				t.Fatalf("integration analyst prompt missing durable validation intent source, got:\n%s", output)
			}
			var monitorLines []string
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(strings.ToLower(line), "monitor") {
					monitorLines = append(monitorLines, line)
				}
			}
			if len(monitorLines) != 1 {
				t.Fatalf("output contains %d Monitor guidance lines, want exactly 1 negative line: %#v", len(monitorLines), monitorLines)
			}
			if !strings.Contains(strings.ToLower(monitorLines[0]), "do not") {
				t.Errorf("Monitor guidance line is not negative: %q", monitorLines[0])
			}
			var scheduleWakeupLines []string
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, "ScheduleWakeup") {
					scheduleWakeupLines = append(scheduleWakeupLines, line)
				}
			}
			if len(scheduleWakeupLines) != 1 {
				t.Fatalf("output contains %d ScheduleWakeup guidance lines, want exactly 1 negative line: %#v", len(scheduleWakeupLines), scheduleWakeupLines)
			}
			if !strings.Contains(strings.ToLower(scheduleWakeupLines[0]), "do not") {
				t.Errorf("ScheduleWakeup guidance line is not negative: %q", scheduleWakeupLines[0])
			}
		})
	}
}

func TestBuildRoleContext_PlanRefAndValidationPlan(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	t.Run("coder with PlanRef renders plan context", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:           projectRoot + "/.worktrees/task-1",
			IterationNum:       1,
			IntegrationBranch:  "integration",
			PlanRef:            "specs/plans/20260317-plan.md",
			ValidationCommands: []string{"make test", "pre-commit run --files internal/feature/feature.go"},
			ProjectRoot:        projectRoot,
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
		if !strings.Contains(output, "CANONICAL VALIDATION:") {
			t.Error("output missing canonical validation section")
		}
		if !strings.Contains(output, "- make test") || !strings.Contains(output, "- pre-commit run --files internal/feature/feature.go") {
			t.Error("output missing canonical validation commands")
		}
		if !strings.Contains(output, "Evidence must prove the intended check ran") ||
			!strings.Contains(output, "do not infer local tool paths") ||
			!strings.Contains(output, "not inferred from local tooling") ||
			!strings.Contains(output, validationFallback) {
			t.Error("output missing validation proof guidance")
		}
		if !strings.Contains(output, "\n- make test") || strings.Contains(output, "tooling.-") {
			t.Error("canonical validation note should render before commands with a line break")
		}
	})

	t.Run("unsafe validation commands render with adjacent fallback", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-unsafe-validation", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:          projectRoot + "/.worktrees/task-unsafe-validation",
			IterationNum:      1,
			IntegrationBranch: "integration",
			ValidationCommands: []string{
				"cd services/foo && test",
				"echo $(pwd)",
				"tail -f /tmp/liza-output",
			},
			ProjectRoot: projectRoot,
		}
		sections, _ := resolver.ContextSections("coder")
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		canonicalIdx := strings.Index(output, "CANONICAL VALIDATION:")
		noteIdx := strings.Index(output, validationFallback)
		firstCommandIdx := strings.Index(output, "- cd services/foo && test")
		if canonicalIdx == -1 || noteIdx == -1 || firstCommandIdx == -1 {
			t.Fatalf("canonical validation section missing fallback or raw command:\n%s", output)
		}
		if !(canonicalIdx < noteIdx && noteIdx < firstCommandIdx) {
			t.Fatalf("validation fallback must render between CANONICAL VALIDATION and raw commands:\n%s", output)
		}
		between := output[noteIdx+len(validationFallback) : firstCommandIdx]
		if strings.TrimSpace(between) != "" {
			t.Fatalf("validation fallback should be adjacent to raw validation commands, got intervening text %q", between)
		}
		for _, rawCommand := range []string{
			"- cd services/foo && test",
			"- echo $(pwd)",
			"- tail -f /tmp/liza-output",
		} {
			if !strings.Contains(output, rawCommand) {
				t.Fatalf("raw validation command %q should remain visible:\n%s", rawCommand, output)
			}
		}
	})

	t.Run("destructive db validation renders marker preservation warning", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-destructive-db", Description: "Reset disposable DB",
			DoneWhen: "Reset path is tested", Scope: "internal/dbreset",
			Worktree:           projectRoot + "/.worktrees/task-destructive-db",
			IterationNum:       1,
			IntegrationBranch:  "integration",
			ValidationCommands: []string{"LIZA_ALLOW_DESTRUCTIVE_DB=1 make test ./internal/dbreset"},
			DestructiveDB:      true,
			ProjectRoot:        projectRoot,
		}
		sections, _ := resolver.ContextSections("coder")
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "DESTRUCTIVE DB VALIDATION:") {
			t.Fatalf("output missing destructive DB validation warning:\n%s", output)
		}
		if !strings.Contains(output, "marker is part of each canonical command and must not be translated away") {
			t.Fatalf("output missing marker preservation warning:\n%s", output)
		}
		if !strings.Contains(output, "- LIZA_ALLOW_DESTRUCTIVE_DB=1 make test ./internal/dbreset") {
			t.Fatalf("output missing marked canonical command:\n%s", output)
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
			PlanRef:            "specs/plans/20260317-plan.md",
			ValidationCommands: []string{"make test"},
			ValidationPlan:     "run go test ./... and verify all pass",
			ProjectRoot:        projectRoot,
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
		canonicalIdx := strings.Index(output, "CANONICAL VALIDATION:")
		planIdx := strings.Index(output, "DOER VALIDATION PLAN:")
		if canonicalIdx == -1 || planIdx == -1 || canonicalIdx > planIdx {
			t.Fatalf("canonical validation should render before doer validation plan:\n%s", output)
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

func TestBuildRoleContext_ValidationCommandShapeGuidance(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	for _, tc := range []struct {
		role     string
		agentID  string
		roleType string
	}{
		{role: "code-planner", agentID: "code-planner-1", roleType: "doer"},
		{role: "epic-planner", agentID: "epic-planner-1", roleType: "doer"},
		{role: "architect", agentID: "architect-1", roleType: "doer"},
		{role: "integration-analyst", agentID: "integration-analyst-1", roleType: "doer"},
	} {
		t.Run("producer/"+tc.role, func(t *testing.T) {
			data := &RoleContextData{
				Role: tc.role, AgentID: tc.agentID, RoleType: tc.roleType,
				TaskID: "task-validation-shape", Description: "Produce child tasks",
				DoneWhen: "Output entries are ready", Scope: "specs",
				Worktree:          projectRoot + "/.worktrees/task-validation-shape",
				IterationNum:      1,
				GoalSpecRef:       "specs/goal.md",
				GoalSlug:          "goal-slug",
				IntegrationBranch: "integration",
				ProjectRoot:       projectRoot,
			}
			sections, err := resolver.ContextSections(tc.role)
			if err != nil {
				t.Fatalf("ContextSections: %v", err)
			}
			output, err := BuildRoleContext(tc.role, sections, data)
			if err != nil {
				t.Fatalf("BuildRoleContext: %v", err)
			}
			if !strings.Contains(output, "single-purpose and agent-executable") {
				t.Fatalf("%s prompt missing validation command executability rule:\n%s", tc.role, output)
			}
			if !strings.Contains(output, validationCommandShapeRule) {
				t.Fatalf("%s prompt missing forbidden validation command shapes:\n%s", tc.role, output)
			}
		})
	}

	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))
	for _, role := range []string{"code-plan-reviewer", "epic-plan-reviewer", "architecture-reviewer", "integration-reviewer"} {
		t.Run("reviewer/"+role, func(t *testing.T) {
			data := RoleContextData{
				Role:           role,
				TaskID:         "task-review",
				AgentID:        "reviewer-1",
				Worktree:       "/tmp/worktree",
				BaseCommit:     "base123",
				ReviewCommit:   "review123",
				GoalBaseCommit: "goalbase123",
				GoalSlug:       "goal-slug",
			}
			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data); err != nil {
				t.Fatalf("failed to execute review-instructions template: %v", err)
			}
			output := buf.String()
			if !strings.Contains(output, "single-purpose and agent-executable") {
				t.Fatalf("%s prompt missing validation command executability rule:\n%s", role, output)
			}
			if !strings.Contains(output, validationCommandShapeRule) {
				t.Fatalf("%s prompt missing forbidden validation command shapes:\n%s", role, output)
			}
		})
	}
}

func TestReviewInstructions_PostVerdictResubmissionBoundaryGuidance(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))

	for _, role := range []string{"code-reviewer", "integration-reviewer"} {
		t.Run(role, func(t *testing.T) {
			data := RoleContextData{
				Role:           role,
				TaskID:         "task-review",
				AgentID:        "reviewer-1",
				Worktree:       "/tmp/worktree",
				BaseCommit:     "base123",
				ReviewCommit:   "review123",
				GoalBaseCommit: "goalbase123",
				GoalSlug:       "goal-slug",
			}

			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data); err != nil {
				t.Fatalf("failed to execute review-instructions template: %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, "POST-VERDICT (MANDATORY for REJECTED)") {
				t.Fatalf("%s prompt missing post-verdict block:\n%s", role, output)
			}
			if !strings.Contains(output, "discard prompt-time BASE COMMIT / REVIEW_COMMIT") {
				t.Fatalf("%s prompt missing resubmission boundary refresh guidance:\n%s", role, output)
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
	t.Run("with phase dependency task → renders phase-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:                 "code-planner",
			RoleType:             "doer",
			TotalPlanTasks:       2,
			TaskOrdinal:          2,
			GoalSpecRef:          "specs/goal.md",
			TaskRolePair:         "code-planning-pair",
			DependsOn:            []string{"plan-1"},
			PhaseDependencyTasks: []SiblingTaskSummary{{ID: "plan-1", Description: "Phase 1 planning", Status: "MERGED", PlanRef: "specs/plan-phase1.md", RolePair: "code-planning-pair"}},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "SIBLING CONSISTENCY RULE") {
			t.Error("expected sibling-consistency rule to render")
		}
		if !strings.Contains(output, "plan-1 [MERGED]") {
			t.Error("expected prior task ID in rule")
		}
		if !strings.Contains(output, "specs/plan-phase1.md") {
			t.Error("expected prior task PlanRef in rule")
		}
		if !strings.Contains(output, "liza mark-blocked") {
			t.Error("expected BLOCKED instruction in rule")
		}
		if !strings.Contains(output, "[MERGED") {
			t.Error("expected sibling status tag in task graph digest")
		}
	})

	t.Run("without phase dependency tasks → no sibling-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "code-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    1,
			GoalSpecRef:    "specs/goal.md",
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if strings.Contains(output, "SIBLING CONSISTENCY RULE") {
			t.Error("should NOT render sibling-consistency rule without phase dependency tasks")
		}
	})

	t.Run("matching visible sibling without phase dependency task → no sibling-consistency rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "code-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    2,
			GoalSpecRef:    "specs/goal.md",
			TaskRolePair:   "code-planning-pair",
			DependsOn:      []string{"plan-1"},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if strings.Contains(output, "SIBLING CONSISTENCY RULE") {
			t.Error("should NOT infer sibling-consistency rule from visible siblings alone")
		}
	})

	t.Run("superseded phase dependency hidden from siblings still renders rule", func(t *testing.T) {
		data := &RoleContextData{
			Role:                 "code-planner",
			RoleType:             "doer",
			TotalPlanTasks:       2,
			TaskOrdinal:          1,
			GoalSpecRef:          "specs/goal.md",
			TaskRolePair:         "code-planning-pair",
			DependsOn:            []string{"plan-old"},
			PhaseDependencyTasks: []SiblingTaskSummary{{ID: "plan-old", Description: "Old phase planning", Status: "SUPERSEDED", PlanRef: "specs/plan-old.md", RolePair: "code-planning-pair"}},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "SIBLING CONSISTENCY RULE") {
			t.Error("expected sibling-consistency rule to render for hidden phase dependency")
		}
		if !strings.Contains(output, "plan-old [SUPERSEDED]") {
			t.Error("expected superseded phase dependency in rule")
		}
		if strings.Contains(output, "plan-old [SUPERSEDED]: Old phase planning") {
			t.Error("superseded phase dependency should not render as a plan sibling")
		}
	})

	t.Run("epic-planner role branch", func(t *testing.T) {
		data := &RoleContextData{
			Role:           "epic-planner",
			RoleType:       "doer",
			TotalPlanTasks: 2,
			TaskOrdinal:    1,
			GoalSpecRef:    "specs/goal.md",
		}

		output, err := BuildRoleContext("epic-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "Do NOT plan capabilities that belong to a sibling task") {
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
		}

		output, err := BuildRoleContext("epic-plan-reviewer", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "epic stays within scope") {
			t.Error("expected epic-plan-reviewer scope verification language")
		}
	})

	t.Run("artifact-producer relation keeps produced outputs load hint without inline refs", func(t *testing.T) {
		data := &RoleContextData{
			Role:        "code-planner",
			RoleType:    "doer",
			GoalSpecRef: "specs/goal.md",
			TaskGraph: TaskGraphDigest{
				Entries: []TaskGraphEntry{
					{
						ID:          "plan-1",
						Description: "Completed plan",
						Status:      "MERGED",
						Relations:   []string{"artifact-producer"},
						Children: []TaskGraphChildSummary{
							{
								ID:                 "plan-1-coding-0",
								Status:             "MERGED",
								RolePair:           "coding-pair",
								DependsOn:          []string{"bootstrap-0", "phase-gate-1", "phase-gate-2"},
								RemainingDependsOn: 2,
							},
						},
						RemainingChildren: 3,
					},
				},
			},
		}

		output, err := BuildRoleContext("code-planner", []string{"collective-plan-scoping"}, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}

		if !strings.Contains(output, "Use listed entries before broad state queries. Active tasks fallback: `liza get tasks --active --summary --json`.") {
			t.Error("expected active task command to be framed as fallback")
		}
		if !strings.Contains(output, "Task detail: `liza get <id> --json` for full task state and `artifact-ref` tasks.") {
			t.Error("expected full task detail command hint to remain available")
		}
		if !strings.Contains(output, "Produced outputs: `liza get <id> --output-summary --json` for `artifact-producer` tasks.") {
			t.Error("expected produced outputs command hint for artifact producers")
		}
		if !strings.Contains(output, "plan-1 [MERGED; artifact-producer]: Completed plan") {
			t.Error("expected artifact-producer relation on compact task graph entry")
		}
		if !strings.Contains(output, "children: plan-1-coding-0 [MERGED, coding-pair, deps: bootstrap-0, phase-gate-1, phase-gate-2 (+2 more)] (+3 more)") {
			t.Errorf("expected bounded child summaries to render, got:\n%s", output)
		}
		if strings.Contains(output, "exact refs:") || strings.Contains(output, "output: specs/plans/plan-1.md") {
			t.Errorf("expected produced output refs to stay load-on-demand, got:\n%s", output)
		}
	})
}

func TestBlockBranchIntegrationContext_Populated(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/branch_integration_context.tmpl"))

	data := RoleContextData{
		GoalBaseCommit: "abc123def456",
		Worktree:       "/home/user/.worktrees/task-1",
		CompletedTasks: []CompletedTaskSummary{
			{
				ID:       "task-alpha",
				DoneWhen: "tests pass for alpha feature",
				SpecRef:  "specs/alpha.md",
			},
			{
				ID:       "task-beta",
				DoneWhen: "beta endpoint returns 200",
				SpecRef:  "specs/beta.md",
			},
		},
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "branch-integration-context", &data)
	if err != nil {
		t.Fatalf("failed to execute branch-integration-context template: %v", err)
	}

	result := buf.String()

	if !strings.Contains(result, "BRANCH INTEGRATION CONTEXT") {
		t.Error("expected BRANCH INTEGRATION CONTEXT header")
	}
	nameOnlyDiff := "git -C /home/user/.worktrees/task-1 diff --name-only abc123def456..HEAD"
	if !strings.Contains(result, nameOnlyDiff) {
		t.Error("expected changed-file map diff command with GoalBaseCommit and Worktree path")
	}
	statDiff := "git -C /home/user/.worktrees/task-1 diff --stat abc123def456..HEAD"
	if !strings.Contains(result, statDiff) {
		t.Error("expected diff stat command with GoalBaseCommit and Worktree path")
	}
	targetedDiff := "git -C /home/user/.worktrees/task-1 diff abc123def456..HEAD -- <path>"
	if !strings.Contains(result, targetedDiff) {
		t.Error("expected targeted diff command with GoalBaseCommit and Worktree path")
	}
	unboundedBranchDiff := regexp.MustCompile(`(?m)^\s*git -C /home/user/\.worktrees/task-1 diff abc123def456\.\.HEAD\s*$`)
	if unboundedBranchDiff.MatchString(result) {
		t.Fatalf("branch integration context reintroduced unbounded full diff:\n%s", result)
	}
	if strings.Index(result, nameOnlyDiff) > strings.Index(result, targetedDiff) {
		t.Fatalf("branch integration map should appear before targeted branch diff:\n%s", result)
	}
	if strings.Contains(result, "Run all project test suites") {
		t.Error("branch integration context must not mandate full project test suites")
	}
	if !strings.Contains(result, "VALIDATION (after changed-file and diff inspection)") {
		t.Error("expected validation guidance after changed-file and diff inspection")
	}
	if !strings.Contains(result, "Run the smallest tests covering changed files") {
		t.Error("expected scoped validation guidance")
	}
	if !strings.Contains(result, "new-vs-baseline status") {
		t.Error("expected baseline-aware failure guidance")
	}
	if !strings.Contains(result, "without `cd &&`") {
		t.Error("expected branch integration validation guidance to reject cd &&")
	}
	if strings.Contains(result, "cd /home/user/.worktrees/task-1 &&") {
		t.Error("branch integration context must not teach cd && worktree commands")
	}
	for _, task := range data.CompletedTasks {
		if !strings.Contains(result, task.ID) {
			t.Errorf("expected completed task ID %q in output", task.ID)
		}
		if strings.Contains(result, task.DoneWhen) {
			t.Errorf("did not expect completed task DoneWhen %q in output", task.DoneWhen)
		}
		if !strings.Contains(result, task.SpecRef) {
			t.Errorf("expected completed task SpecRef %q in output", task.SpecRef)
		}
	}
}

func TestBlockBranchIntegrationContext_Empty(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/branch_integration_context.tmpl"))

	data := RoleContextData{
		GoalBaseCommit: "",
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "branch-integration-context", &data)
	if err != nil {
		t.Fatalf("failed to execute branch-integration-context template: %v", err)
	}

	if buf.String() != "" {
		t.Errorf("expected empty output when GoalBaseCommit is empty, got %q", buf.String())
	}
}

func TestBlockBranchIntegrationContext_NoCompletedTasks(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/branch_integration_context.tmpl"))

	data := RoleContextData{
		GoalBaseCommit: "abc123def456",
		Worktree:       "/home/user/.worktrees/task-1",
		CompletedTasks: nil,
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "branch-integration-context", &data)
	if err != nil {
		t.Fatalf("failed to execute branch-integration-context template: %v", err)
	}

	result := buf.String()

	if !strings.Contains(result, "git -C /home/user/.worktrees/task-1 diff --name-only abc123def456..HEAD") {
		t.Error("expected changed-file map diff command in output")
	}
	if !strings.Contains(result, "git -C /home/user/.worktrees/task-1 diff --stat abc123def456..HEAD") {
		t.Error("expected diff stat command in output")
	}
	if !strings.Contains(result, "git -C /home/user/.worktrees/task-1 diff abc123def456..HEAD -- <path>") {
		t.Error("expected targeted diff command in output")
	}
	if !strings.Contains(result, "(no completed tasks found)") {
		t.Error("expected '(no completed tasks found)' when CompletedTasks is nil")
	}
}

func TestBlockReviewInstructions_IntegrationReviewer(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))

	data := RoleContextData{
		Role:           "integration-reviewer",
		GoalBaseCommit: "abc123def456",
		Worktree:       "/home/user/.worktrees/task-1",
		TaskID:         "integration-task-1",
	}

	var buf bytes.Buffer
	err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data)
	if err != nil {
		t.Fatalf("failed to execute review-instructions template: %v", err)
	}

	result := buf.String()

	if !strings.Contains(result, "REVIEW SCOPE") {
		t.Error("expected REVIEW SCOPE header")
	}
	if !strings.Contains(result, "systemic-thinking") {
		t.Error("expected systemic-thinking skill reference")
	}
	if !strings.Contains(result, "git -C /home/user/.worktrees/task-1 diff abc123def456..HEAD") {
		t.Error("expected diff command with GoalBaseCommit and Worktree path")
	}
	if !strings.Contains(result, "output[]") {
		t.Error("expected output[] references")
	}
	if !strings.Contains(result, "durable fix-task text") {
		t.Error("expected fix-task validation satisfiability guidance")
	}
	assertAwaitResubmissionPassiveGuidance(t, result, 1)
}

func TestReviewInstructions_CodeReviewerSkipsIntegrationDriftWhenBranchMissing(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))

	data := RoleContextData{
		Role:         "code-reviewer",
		Worktree:     "/tmp/worktree",
		BaseCommit:   "base123",
		ReviewCommit: "review123",
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data); err != nil {
		t.Fatalf("failed to execute review-instructions template: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "diff ..review123") {
		t.Fatalf("reviewer prompt rendered malformed integration drift command:\n%s", output)
	}
	if strings.Contains(output, "current integration drift check") {
		t.Fatalf("reviewer prompt rendered integration drift check without IntegrationBranch:\n%s", output)
	}
	nameOnlyDiff := "git -C /tmp/worktree diff --name-only base123..review123"
	if !strings.Contains(output, nameOnlyDiff) {
		t.Fatalf("reviewer prompt missing changed-file map diff:\n%s", output)
	}
	statDiff := "git -C /tmp/worktree diff --stat base123..review123"
	if !strings.Contains(output, statDiff) {
		t.Fatalf("reviewer prompt missing changed-file stat diff:\n%s", output)
	}
	targetedDiff := "git -C /tmp/worktree diff base123..review123 -- <path>"
	if !strings.Contains(output, targetedDiff) {
		t.Fatalf("reviewer prompt missing targeted scope/workmanship diff:\n%s", output)
	}
	if strings.Contains(output, "Review ALL changes: git -C /tmp/worktree diff base123..review123") {
		t.Fatalf("reviewer prompt reintroduced unbounded full diff:\n%s", output)
	}
	if strings.Index(output, nameOnlyDiff) > strings.Index(output, targetedDiff) {
		t.Fatalf("changed-file map should appear before targeted scope/workmanship diff:\n%s", output)
	}
}

func TestReviewInstructions_CodeReviewerBoundsIntegrationDriftWhenBranchPresent(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))

	data := RoleContextData{
		Role:              "code-reviewer",
		Worktree:          "/tmp/worktree",
		BaseCommit:        "base123",
		ReviewCommit:      "review123",
		IntegrationBranch: "integration",
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data); err != nil {
		t.Fatalf("failed to execute review-instructions template: %v", err)
	}

	output := buf.String()
	nameOnlyDiff := "git -C /tmp/worktree diff --name-only integration..review123"
	if !strings.Contains(output, nameOnlyDiff) {
		t.Fatalf("reviewer prompt missing integration drift file map diff:\n%s", output)
	}
	statDiff := "git -C /tmp/worktree diff --stat integration..review123"
	if !strings.Contains(output, statDiff) {
		t.Fatalf("reviewer prompt missing integration drift stat diff:\n%s", output)
	}
	targetedDiff := "git -C /tmp/worktree diff integration..review123 -- <path>"
	if !strings.Contains(output, targetedDiff) {
		t.Fatalf("reviewer prompt missing targeted integration drift diff:\n%s", output)
	}
	unboundedIntegrationDiff := regexp.MustCompile(`(?m)^\s*git -C /tmp/worktree diff integration\.\.review123\s*$`)
	if unboundedIntegrationDiff.MatchString(output) {
		t.Fatalf("reviewer prompt reintroduced unbounded integration drift diff:\n%s", output)
	}
	if strings.Index(output, nameOnlyDiff) > strings.Index(output, targetedDiff) {
		t.Fatalf("integration drift map should appear before targeted integration drift diff:\n%s", output)
	}
}

func TestReviewTask_RendersIntegrationBranchOnlyForCodeReviewer(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles(
		"templates/blocks/review_task.tmpl",
		"templates/blocks/task_decomposition_metadata.tmpl",
	))

	for _, tc := range []struct {
		role string
		want bool
	}{
		{role: "code-reviewer", want: true},
		{role: "code-plan-reviewer", want: false},
		{role: "epic-plan-reviewer", want: false},
		{role: "us-reviewer", want: false},
		{role: "architecture-reviewer", want: false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			data := RoleContextData{
				Role:              tc.role,
				TaskID:            "task-review",
				Worktree:          "/tmp/worktree",
				BaseCommit:        "base123",
				ReviewCommit:      "review123",
				IntegrationBranch: "integration",
				AssignedTo:        "coder-1",
			}

			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, "review-task", &data); err != nil {
				t.Fatalf("failed to execute review-task template: %v", err)
			}

			output := buf.String()
			got := strings.Contains(output, "INTEGRATION BRANCH: integration")
			if got != tc.want {
				t.Fatalf("INTEGRATION BRANCH rendered = %v, want %v; output:\n%s", got, tc.want, output)
			}
		})
	}
}

func TestReviewInstructions_OutputReviewersUseFullTaskJSON(t *testing.T) {
	tmpl := template.Must(template.New("").Funcs(funcMap).ParseFiles("templates/blocks/review_instructions.tmpl"))

	for _, role := range []string{"code-plan-reviewer", "epic-plan-reviewer", "architecture-reviewer", "integration-reviewer"} {
		t.Run(role, func(t *testing.T) {
			data := RoleContextData{
				Role:           role,
				TaskID:         "task-review",
				AgentID:        "reviewer-1",
				Worktree:       "/tmp/worktree",
				BaseCommit:     "base123",
				ReviewCommit:   "review123",
				GoalBaseCommit: "goalbase123",
				GoalSlug:       "goal-slug",
			}

			var buf bytes.Buffer
			if err := tmpl.ExecuteTemplate(&buf, "review-instructions", &data); err != nil {
				t.Fatalf("failed to execute review-instructions template: %v", err)
			}

			output := buf.String()
			if !strings.Contains(output, "liza get task-review --json") {
				t.Fatalf("expected full task JSON command, got:\n%s", output)
			}
			outputSummaryCommand := regexp.MustCompile(`liza get[^\n]*--output-summary`)
			if outputSummaryCommand.MatchString(output) {
				t.Fatalf("reviewer prompt should not use output-summary, got:\n%s", output)
			}
			nameOnlyRange := "base123..review123"
			fullDiffRange := "base123..review123"
			if role == "integration-reviewer" {
				nameOnlyRange = "goalbase123..HEAD"
				fullDiffRange = "goalbase123..HEAD"
			}
			nameOnlyDiff := "git -C /tmp/worktree diff --name-only " + nameOnlyRange
			statDiff := "git -C /tmp/worktree diff --stat " + nameOnlyRange
			targetedDiff := "git -C /tmp/worktree diff " + fullDiffRange + " -- <path>"
			if !strings.Contains(output, nameOnlyDiff) {
				t.Fatalf("reviewer prompt missing changed-file map diff %q, got:\n%s", nameOnlyDiff, output)
			}
			if !strings.Contains(output, statDiff) {
				t.Fatalf("reviewer prompt missing changed-file stat diff %q, got:\n%s", statDiff, output)
			}
			if !strings.Contains(output, "supporting rg/glob only after") {
				t.Fatalf("reviewer prompt missing changed-file map search guidance, got:\n%s", output)
			}
			if !strings.Contains(output, targetedDiff) {
				t.Fatalf("reviewer prompt missing targeted diff %q, got:\n%s", targetedDiff, output)
			}
			if strings.Index(output, nameOnlyDiff) > strings.Index(output, targetedDiff) {
				t.Fatalf("changed-file map should appear before targeted diff for %s:\n%s", role, output)
			}
			unboundedDiffs := []string{
				"Inspect worktree changes: git -C /tmp/worktree diff " + fullDiffRange,
				"Read the branch diff: git -C /tmp/worktree diff " + fullDiffRange,
			}
			for _, unbounded := range unboundedDiffs {
				if strings.Contains(output, unbounded) {
					t.Fatalf("reviewer prompt should not instruct unbounded full diff %q, got:\n%s", unbounded, output)
				}
			}
			if role == "integration-reviewer" {
				if !strings.Contains(output, "durable fix-task text") {
					t.Fatalf("integration reviewer prompt missing fix-task validation guidance, got:\n%s", output)
				}
				assertAwaitResubmissionPassiveGuidance(t, output, 1)
			} else if !strings.Contains(output, "durable check/hook intent") {
				t.Fatalf("%s prompt missing output[] validation satisfiability guidance, got:\n%s", role, output)
			}
		})
	}
}

func TestWakeTemplate_CodingComplete(t *testing.T) {
	data := wakeTemplateData{AgentID: "orchestrator-1"}
	result, err := executeTemplate("wake_coding_complete", data)
	if err != nil {
		t.Fatalf("executeTemplate(wake_coding_complete) error: %v", err)
	}

	for _, want := range []string{"integration-pair", "integration", "goal.base_commit", "BLOCKED ESCALATION"} {
		if !strings.Contains(result, want) {
			t.Errorf("wake_coding_complete output missing %q", want)
		}
	}
}

func TestDetermineWakeTrigger_CodingComplete(t *testing.T) {
	// sprintComplete=true, codingComplete=true → CODING_COMPLETE
	got := determineWakeTrigger(5, 0, 0, 0, true, true, nil, 0)
	if got != "CODING_COMPLETE" {
		t.Errorf("expected CODING_COMPLETE, got %s", got)
	}
}

func TestBuildRoleContext_ArchRef(t *testing.T) {
	projectRoot := setupPipelineConfig(t)
	resolver := testPipelineResolver(t)

	t.Run("coder with ArchRef renders architecture document reference", func(t *testing.T) {
		data := &RoleContextData{
			Role: "coder", AgentID: "coder-1", RoleType: "doer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:          projectRoot + "/.worktrees/task-1",
			IterationNum:      1,
			IntegrationBranch: "integration",
			ArchRef:           "specs/arch-plan/feature.md",
			ProjectRoot:       projectRoot,
		}
		sections, _ := resolver.ContextSections("coder")
		output, err := BuildRoleContext("coder", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "architecture document at") {
			t.Error("coder output missing 'architecture document at' when ArchRef is set")
		}
		if !strings.Contains(output, "specs/arch-plan/feature.md") {
			t.Error("coder output missing ArchRef path")
		}
	})

	t.Run("coder without ArchRef omits architecture document", func(t *testing.T) {
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
		if strings.Contains(output, "architecture document") {
			t.Error("coder output should NOT contain 'architecture document' when ArchRef is empty")
		}
	})

	t.Run("code-reviewer with ArchRef includes architectural decisions", func(t *testing.T) {
		data := &RoleContextData{
			Role: "code-reviewer", AgentID: "code-reviewer-1", RoleType: "reviewer",
			TaskID: "task-1", Description: "Implement feature X",
			DoneWhen: "Feature X works", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-1",
			IterationNum: 1,
			BaseCommit:   "abc", ReviewCommit: "def", AssignedTo: "coder-1",
			ArchRef:     "specs/arch-plan/feature.md",
			ProjectRoot: projectRoot,
		}
		sections, _ := resolver.ContextSections("code-reviewer")
		output, err := BuildRoleContext("code-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "architectural decisions") {
			t.Error("code-reviewer output missing 'architectural decisions' when ArchRef is set")
		}
		if !strings.Contains(output, "specs/arch-plan/feature.md") {
			t.Error("code-reviewer output missing ArchRef path")
		}
	})

	t.Run("code-plan-reviewer with ArchRef includes ARCHITECTURE REFERENCE", func(t *testing.T) {
		data := &RoleContextData{
			Role: "code-plan-reviewer", AgentID: "code-plan-reviewer-1", RoleType: "reviewer",
			TaskID: "task-1", Description: "Plan feature X",
			DoneWhen: "Plan approved", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-1",
			IterationNum: 1,
			BaseCommit:   "abc", ReviewCommit: "def", AssignedTo: "code-planner-1",
			ArchRef:     "specs/arch-plan/feature.md",
			ProjectRoot: projectRoot,
		}
		sections, _ := resolver.ContextSections("code-plan-reviewer")
		output, err := BuildRoleContext("code-plan-reviewer", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "ARCHITECTURE REFERENCE") {
			t.Error("code-plan-reviewer output missing 'ARCHITECTURE REFERENCE' when ArchRef is set")
		}
		if !strings.Contains(output, "specs/arch-plan/feature.md") {
			t.Error("code-plan-reviewer output missing ArchRef path")
		}
	})

	t.Run("code-planner with ArchRef includes ARCHITECTURE REFERENCE", func(t *testing.T) {
		data := &RoleContextData{
			Role: "code-planner", AgentID: "code-planner-1", RoleType: "doer",
			TaskID: "task-1", Description: "Plan feature X",
			DoneWhen: "Plan approved", Scope: "internal/feature",
			Worktree:     projectRoot + "/.worktrees/task-1",
			IterationNum: 1,
			ArchRef:      "specs/arch-plan/feature.md",
			ProjectRoot:  projectRoot,
		}
		sections, _ := resolver.ContextSections("code-planner")
		output, err := BuildRoleContext("code-planner", sections, data)
		if err != nil {
			t.Fatalf("BuildRoleContext: %v", err)
		}
		if !strings.Contains(output, "ARCHITECTURE REFERENCE") {
			t.Error("code-planner output missing 'ARCHITECTURE REFERENCE' when ArchRef is set")
		}
		if !strings.Contains(output, "specs/arch-plan/feature.md") {
			t.Error("code-planner output missing ArchRef path")
		}
	})
}

func TestDetermineWakeTrigger_SprintCompleteNotCoding(t *testing.T) {
	// sprintComplete=true, codingComplete=false → SPRINT_COMPLETE
	got := determineWakeTrigger(5, 0, 0, 0, true, false, nil, 0)
	if got != "SPRINT_COMPLETE" {
		t.Errorf("expected SPRINT_COMPLETE, got %s", got)
	}
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

func TestBuildInstructionsForWakeTrigger_ManyToOneReady(t *testing.T) {
	wakeData := wakeTemplateData{AgentID: "orchestrator-1"}
	instructions, err := buildInstructionsForWakeTrigger("MANY_TO_ONE_READY", "orchestrator-1", wakeData, nil)
	if err != nil {
		t.Fatalf("buildInstructionsForWakeTrigger: %v", err)
	}
	if instructions == "" {
		t.Error("expected non-empty instructions for MANY_TO_ONE_READY")
	}
}
