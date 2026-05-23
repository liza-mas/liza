package agent

import (
	stderrors "errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/precommit"
	"github.com/liza-mas/liza/internal/scipsearch"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// testBuildPrompt creates a strategy for config.Role and builds the prompt.
// Strategy creation failure is always fatal; BuildPrompt errors are returned.
func testBuildPrompt(t *testing.T, state *models.State, config SupervisorConfig, taskID string) (string, error) {
	t.Helper()
	resolver := testResolver(t)
	strategy, err := NewRoleStrategy(config.Role, resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy(%q) error = %v", config.Role, err)
	}
	return strategy.BuildPrompt(state, config, taskID)
}

// TestBuildPrompt tests the buildPrompt function
func TestBuildPrompt(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Task is complete",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "main",
		},
	}

	tests := []struct {
		name        string
		role        string
		taskID      string
		initialTask string
		wantErr     bool
		contains    []string
	}{
		{
			name:     "coder prompt",
			role:     "coder",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"coder", "Test goal", "task-1"},
		},
		{
			name:     "code-reviewer prompt",
			role:     "code-reviewer",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"reviewer", "Test goal", "task-1"},
		},
		{
			name:     "orchestrator prompt",
			role:     "orchestrator",
			taskID:   "",
			wantErr:  false,
			contains: []string{"orchestrator", "Test goal"},
		},
		{
			name:     "code-planner prompt",
			role:     "code-planner",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"code-planner", "ASSIGNED CODE PLANNING TASK", "task-1"},
		},
		{
			name:     "code-plan-reviewer prompt",
			role:     "code-plan-reviewer",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"code-plan-reviewer", "ASSIGNED CODE PLAN REVIEW TASK", "task-1"},
		},
		{
			name:     "epic-planner prompt",
			role:     "epic-planner",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"epic-planner", "task-1"},
		},
		{
			name:     "epic-plan-reviewer prompt",
			role:     "epic-plan-reviewer",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"epic-plan-reviewer", "task-1"},
		},
		{
			name:     "us-writer prompt",
			role:     "us-writer",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"us-writer", "task-1"},
		},
		{
			name:     "us-reviewer prompt",
			role:     "us-reviewer",
			taskID:   "task-1",
			wantErr:  false,
			contains: []string{"us-reviewer", "task-1"},
		},
		{
			name:     "coder with non-existent task",
			role:     "coder",
			taskID:   "task-999",
			wantErr:  true,
			contains: nil,
		},
		{
			name:        "coder with initial task",
			role:        "coder",
			taskID:      "task-1",
			initialTask: "task-1",
			wantErr:     false,
			contains:    []string{"coder", "RESUME CONTEXT", "task-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testhelpers.SetupPipelineConfig(t, tmpDir)
			config := SupervisorConfig{
				Role:        tt.role,
				AgentID:     tt.role + "-1",
				ProjectRoot: tmpDir,
				SpecsDir:    filepath.Join(tmpDir, "specs"),
				StatePath:   filepath.Join(tmpDir, "state.yaml"),
				InitialTask: tt.initialTask,
			}

			prompt, err := testBuildPrompt(t, state, config, tt.taskID)

			if (err != nil) != tt.wantErr {
				t.Errorf("buildPrompt() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if !errors.IsNotFound(err) {
					t.Errorf("expected NotFoundError, got %T: %v", err, err)
				}
				return
			}

			// Check that prompt contains expected strings
			for _, expected := range tt.contains {
				if !strings.Contains(prompt, expected) {
					t.Errorf("buildPrompt() prompt should contain %q", expected)
				}
			}

			// Verify prompt is not empty
			if prompt == "" {
				t.Error("buildPrompt() returned empty prompt")
			}
		})
	}
}

func TestBuildOrchestratorRoleContextDataScipIndexesUseProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	writePromptTestFile(t, filepath.Join(tmpDir, "go.mod"), "module example.com/project\n")
	writePromptTestFile(t, filepath.Join(tmpDir, "web.ts"), "export const value = 1\n")
	writePromptTestFile(t, filepath.Join(tmpDir, "tool.py"), "value = 1\n")
	testhelpers.MustGit(t, tmpDir, "add", "go.mod", "web.ts", "tool.py")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Add language fixtures")

	projectGoIndex := filepath.Join(tmpDir, ".liza", "scip", "go.scip")
	writePromptTestFile(t, projectGoIndex, "go index")
	taskTypescriptIndex := filepath.Join(tmpDir, ".worktrees", "task-1", ".liza", "scip", "typescript.scip")
	writePromptTestFile(t, taskTypescriptIndex, "task typescript index")

	state := &models.State{
		Goal: models.Goal{
			Description: "Test goal",
			SpecRef:     "specs/goal.md",
		},
		Config: models.Config{
			ScipSearch: []string{"go", "typescript", "python"},
		},
	}
	config := SupervisorConfig{
		Role:        "orchestrator",
		AgentID:     "orchestrator-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, ".liza", "state.yaml"),
	}

	data, err := buildOrchestratorRoleContextData(state, config, testResolver(t))
	if err != nil {
		t.Fatalf("buildOrchestratorRoleContextData() error = %v", err)
	}

	if len(data.ScipIndexes) != 1 {
		t.Fatalf("ScipIndexes length = %d, want 1: %#v", len(data.ScipIndexes), data.ScipIndexes)
	}
	got := data.ScipIndexes[0]
	if got.Language != "go" {
		t.Errorf("ScipIndexes[0].Language = %q, want go", got.Language)
	}
	if got.Path != projectGoIndex {
		t.Errorf("ScipIndexes[0].Path = %q, want project-root index %q", got.Path, projectGoIndex)
	}
	if !filepath.IsAbs(got.Path) {
		t.Errorf("ScipIndexes[0].Path = %q, want absolute path", got.Path)
	}
	if strings.Contains(got.Path, filepath.Join(".worktrees", "task-1")) {
		t.Errorf("ScipIndexes[0].Path = %q, must not use task worktree index %q", got.Path, taskTypescriptIndex)
	}
}

func TestBuildPromptWithContextScipIndexesUseTaskWorktree(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupPipelineConfig(t, projectRoot)
	taskWorktree := filepath.Join(projectRoot, ".worktrees", "task-1")
	if err := os.MkdirAll(taskWorktree, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", taskWorktree, err)
	}
	testhelpers.SetupTestGitRepo(t, taskWorktree)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	writePromptTestFile(t, filepath.Join(taskWorktree, "go.mod"), "module example.com/task\n")
	writePromptTestFile(t, filepath.Join(taskWorktree, "web.ts"), "export const value = 1\n")
	writePromptTestFile(t, filepath.Join(taskWorktree, "tool.py"), "value = 1\n")
	testhelpers.MustGit(t, taskWorktree, "add", "go.mod", "web.ts", "tool.py")
	testhelpers.MustGit(t, taskWorktree, "commit", "-m", "Add task language fixtures")

	taskGoIndex := filepath.Join(taskWorktree, ".liza", "scip", "go.scip")
	writePromptTestFile(t, taskGoIndex, "task go index")
	taskPythonIndex := filepath.Join(taskWorktree, ".liza", "scip", "python.scip")
	writePromptTestFile(t, taskPythonIndex, "task python index")
	taskTypescriptIndex := filepath.Join(taskWorktree, ".liza", "scip", "typescript.scip")
	projectGoIndex := filepath.Join(projectRoot, ".liza", "scip", "go.scip")
	writePromptTestFile(t, projectGoIndex, "project go index")

	worktree := ".worktrees/task-1"
	state := &models.State{
		Goal: models.Goal{
			Description: "Test goal",
			SpecRef:     "specs/goal.md",
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				DoneWhen:    "Task is complete",
				Worktree:    &worktree,
			},
		},
		Config: models.Config{
			ScipSearch:        []string{"go", "typescript", "python"},
			IntegrationBranch: "main",
		},
	}
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}

	prompt, err := buildPromptWithContext(state, config, "task-1", testResolver(t))
	if err != nil {
		t.Fatalf("buildPromptWithContext() error = %v", err)
	}

	if !strings.Contains(prompt, taskGoIndex) {
		t.Fatalf("prompt missing task worktree SCIP index path %q", taskGoIndex)
	}
	if !strings.Contains(prompt, taskPythonIndex) {
		t.Fatalf("prompt missing task worktree Python SCIP index path %q", taskPythonIndex)
	}
	if strings.Contains(prompt, projectGoIndex) {
		t.Fatalf("prompt contains project-root SCIP index path %q for task prompt", projectGoIndex)
	}
	if strings.Contains(prompt, taskTypescriptIndex) {
		t.Fatalf("prompt contains missing task typescript SCIP index path %q", taskTypescriptIndex)
	}
	if !strings.Contains(prompt, "Go symbols, references, and implementations are supported.") {
		t.Fatalf("prompt missing Go capability metadata")
	}
	if !strings.Contains(prompt, "scip-search implementations is not supported for Python.") {
		t.Fatalf("prompt missing Python capability metadata")
	}
	if strings.Contains(prompt, "scip-search implementations --index "+taskPythonIndex) {
		t.Fatalf("prompt contains unsupported Python implementations command")
	}
}

func TestBuildPromptWithContextScipSearchGateOmitsStaleIndexes(t *testing.T) {
	tests := []struct {
		name       string
		envValue   string
		configured []string
	}{
		{
			name:       "disabled environment",
			envValue:   "false",
			configured: []string{"go"},
		},
		{
			name:       "empty config",
			envValue:   "true",
			configured: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			testhelpers.SetupPipelineConfig(t, projectRoot)
			taskWorktree := filepath.Join(projectRoot, ".worktrees", "task-1")
			writePromptTestFile(t, filepath.Join(taskWorktree, ".liza", "scip", "go.scip"), "stale go index")
			t.Setenv(scipsearch.EnvEnableScipSearch, tt.envValue)

			worktree := ".worktrees/task-1"
			state := &models.State{
				Goal: models.Goal{
					Description: "Test goal",
					SpecRef:     "specs/goal.md",
				},
				Tasks: []models.Task{
					{
						ID:          "task-1",
						Description: "Test task",
						Status:      models.TaskStatusImplementing,
						DoneWhen:    "Task is complete",
						Worktree:    &worktree,
					},
				},
				Config: models.Config{
					ScipSearch:        tt.configured,
					IntegrationBranch: "main",
				},
			}
			config := SupervisorConfig{
				Role:        "coder",
				AgentID:     "coder-1",
				ProjectRoot: projectRoot,
				SpecsDir:    filepath.Join(projectRoot, "specs"),
				StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
			}

			prompt, err := buildPromptWithContext(state, config, "task-1", testResolver(t))
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}
			if strings.Contains(prompt, "scip-search") {
				t.Fatalf("prompt contains scip-search section despite inactive gate:\n%s", prompt)
			}
		})
	}
}

func TestBuildPromptWithContext_DecompositionRootDoerMandate(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		rolePair string
		refField string
	}{
		{name: "epic planning master", role: "epic-planner", rolePair: "epic-planning-main-pair", refField: "plan_ref"},
		{name: "architecture master", role: "architect", rolePair: "architecture-main-pair", refField: "arch_ref"},
		{name: "code planning master", role: "code-planner", rolePair: "code-planning-main-pair", refField: "plan_ref"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			resolver := embeddedPipelineResolver(t)
			worktree := ".worktrees/task-1"
			state := &models.State{
				Goal: models.Goal{
					Description: "Master planning goal",
					SpecRef:     "specs/goals/master.md",
				},
				Tasks: []models.Task{
					{
						ID:          "task-1",
						Description: "Plan master decomposition",
						Status:      models.TaskStatus("EXECUTING"),
						DoneWhen:    "Master decomposition is complete",
						Scope:       "Master decomposition scope",
						RolePair:    tt.rolePair,
						Worktree:    &worktree,
					},
				},
				Config: models.Config{IntegrationBranch: "main"},
			}
			config := SupervisorConfig{
				Role:        tt.role,
				AgentID:     tt.role + "-1",
				ProjectRoot: projectRoot,
				SpecsDir:    filepath.Join(projectRoot, "specs"),
				StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
			}

			prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}

			assertContainsAll(t, prompt,
				"=== MASTER DECOMPOSITION MANDATE ===",
				"Master Output Contract properties 1-6",
				"1. Non-overlapping scopes.",
				"2. Interface ownership.",
				"3. Shared-file ownership.",
				"4. Dependency ordering.",
				"5. Inherited constraints.",
				"6. Completeness.",
				"Systemic Decomposition Review",
				"systemic-thinking",
				"before `liza set-task-output` or submission",
				"typed decomposition metadata",
				"decomposition:",
				"owned_files",
				"owned_modules",
				"read_only_depends_on",
				"read_only_task_depends_on",
				"interfaces_owned",
				"interfaces_consumed",
				"coverage_notes",
				tt.refField,
			)
			assertNotContains(t, prompt, "MASTER DECOMPOSITION REVIEW")
			assertNotContains(t, prompt, "master-decomposition-review")
		})
	}
}

func TestBuildPromptWithContext_DecompositionRootReviewerReview(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		rolePair string
		refField string
	}{
		{name: "epic planning master reviewer", role: "epic-plan-reviewer", rolePair: "epic-planning-main-pair", refField: "plan_ref"},
		{name: "architecture master reviewer", role: "architecture-reviewer", rolePair: "architecture-main-pair", refField: "arch_ref"},
		{name: "code planning master reviewer", role: "code-plan-reviewer", rolePair: "code-planning-main-pair", refField: "plan_ref"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			resolver := embeddedPipelineResolver(t)
			state := &models.State{
				Goal: models.Goal{
					Description: "Master planning goal",
					SpecRef:     "specs/goals/master.md",
				},
				Tasks: []models.Task{
					{
						ID:           "task-1",
						Description:  "Review master decomposition",
						Status:       models.TaskStatus("REVIEWING"),
						DoneWhen:     "Master decomposition is reviewed",
						Scope:        "Master decomposition scope",
						RolePair:     tt.rolePair,
						BaseCommit:   ptrString("base-sha"),
						ReviewCommit: ptrString("review-sha"),
						AssignedTo:   ptrString("planner-1"),
					},
				},
				Config: models.Config{IntegrationBranch: "main"},
			}
			config := SupervisorConfig{
				Role:        tt.role,
				AgentID:     tt.role + "-1",
				ProjectRoot: projectRoot,
				SpecsDir:    filepath.Join(projectRoot, "specs"),
				StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
			}

			prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}

			assertContainsAll(t, prompt,
				"=== MASTER DECOMPOSITION REVIEW ===",
				"Invoke `systemic-thinking` before submitting a verdict",
				"missing `"+tt.refField+"`",
				"missing typed decomposition metadata",
				"missing systemic-thinking evidence",
				"violates any Master Output Contract property",
				"1. Non-overlapping scopes.",
				"2. Interface ownership.",
				"3. Shared-file ownership.",
				"4. Dependency ordering.",
				"5. Inherited constraints.",
				"6. Completeness.",
			)
			assertNotContains(t, prompt, "MASTER DECOMPOSITION MANDATE")
			assertNotContains(t, prompt, "master-decomposition-mandate")
		})
	}
}

func TestBuildPromptWithContext_NonRootDoersRenderNoMasterMandate(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		rolePair string
	}{
		{name: "specialized epic planner", role: "epic-planner", rolePair: "epic-planning-pair"},
		{name: "specialized architect", role: "architect", rolePair: "architecture-pair"},
		{name: "specialized code planner", role: "code-planner", rolePair: "code-planning-pair"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			resolver := embeddedPipelineResolver(t)
			state := &models.State{
				Goal: models.Goal{
					Description: "Specialized planning goal",
					SpecRef:     "specs/goals/specialized.md",
				},
				Tasks: []models.Task{
					{
						ID:          "task-1",
						Description: "Plan specialized scope",
						Status:      models.TaskStatus("EXECUTING"),
						DoneWhen:    "Specialized plan is complete",
						Scope:       "Specialized scope",
						RolePair:    tt.rolePair,
					},
				},
				Config: models.Config{IntegrationBranch: "main"},
			}
			config := SupervisorConfig{
				Role:        tt.role,
				AgentID:     tt.role + "-1",
				ProjectRoot: projectRoot,
				SpecsDir:    filepath.Join(projectRoot, "specs"),
				StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
			}

			prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}

			assertNotContains(t, prompt, "MASTER DECOMPOSITION MANDATE")
			assertNotContains(t, prompt, "Systemic Decomposition Review")
			assertNotContains(t, prompt, "Master Output Contract properties 1-6")
			assertNotContains(t, prompt, "MASTER DECOMPOSITION REVIEW")
		})
	}
}

func TestBuildPromptWithContext_NonRootReviewersRenderNoMasterReview(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		rolePair string
	}{
		{name: "specialized epic plan reviewer", role: "epic-plan-reviewer", rolePair: "epic-planning-pair"},
		{name: "specialized architecture reviewer", role: "architecture-reviewer", rolePair: "architecture-pair"},
		{name: "specialized code plan reviewer", role: "code-plan-reviewer", rolePair: "code-planning-pair"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			testhelpers.SetupTestGitRepo(t, projectRoot)
			resolver := embeddedPipelineResolver(t)
			state := &models.State{
				Goal: models.Goal{
					Description: "Specialized planning goal",
					SpecRef:     "specs/goals/specialized.md",
				},
				Tasks: []models.Task{
					{
						ID:           "task-1",
						Description:  "Review specialized scope",
						Status:       models.TaskStatus("REVIEWING"),
						DoneWhen:     "Specialized plan is reviewed",
						Scope:        "Specialized scope",
						RolePair:     tt.rolePair,
						BaseCommit:   ptrString("base-sha"),
						ReviewCommit: ptrString("review-sha"),
						AssignedTo:   ptrString("planner-1"),
					},
				},
				Config: models.Config{IntegrationBranch: "main"},
			}
			config := SupervisorConfig{
				Role:        tt.role,
				AgentID:     tt.role + "-1",
				ProjectRoot: projectRoot,
				SpecsDir:    filepath.Join(projectRoot, "specs"),
				StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
			}

			prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
			if err != nil {
				t.Fatalf("buildPromptWithContext() error = %v", err)
			}

			assertNotContains(t, prompt, "MASTER DECOMPOSITION REVIEW")
			assertNotContains(t, prompt, "MASTER DECOMPOSITION MANDATE")
			assertNotContains(t, prompt, "Master Output Contract properties 1-6")
		})
	}
}

func TestBuildPromptWithContext_CustomDecompositionRootDoerUsesConfiguredArtifactRef(t *testing.T) {
	projectRoot := t.TempDir()
	resolver := loadTestResolver(t, customMasterRefPromptPipelineYAML)
	state := &models.State{
		Goal: models.Goal{
			Description: "Unknown master goal",
			SpecRef:     "specs/goals/unknown.md",
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Plan custom master decomposition",
				Status:      models.TaskStatus("EXECUTING"),
				DoneWhen:    "Custom decomposition is complete",
				Scope:       "Custom master scope",
				RolePair:    "custom-main-pair",
			},
		},
		Config: models.Config{IntegrationBranch: "main"},
	}
	config := SupervisorConfig{
		Role:        "code-planner",
		AgentID:     "code-planner-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}

	prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
	if err != nil {
		t.Fatalf("buildPromptWithContext() error = %v", err)
	}
	assertContainsAll(t, prompt, "=== MASTER DECOMPOSITION MANDATE ===", "plan_ref")
}

func TestBuildPromptWithContext_CustomDecompositionRootReviewerUsesConfiguredArtifactRef(t *testing.T) {
	projectRoot := t.TempDir()
	resolver := loadTestResolver(t, customMasterRefPromptPipelineYAML)
	state := &models.State{
		Goal: models.Goal{
			Description: "Unknown master goal",
			SpecRef:     "specs/goals/unknown.md",
		},
		Tasks: []models.Task{
			{
				ID:           "task-1",
				Description:  "Review custom master decomposition",
				Status:       models.TaskStatus("REVIEWING"),
				DoneWhen:     "Custom decomposition is reviewed",
				Scope:        "Custom master scope",
				RolePair:     "custom-main-pair",
				BaseCommit:   ptrString("base-sha"),
				ReviewCommit: ptrString("review-sha"),
				AssignedTo:   ptrString("planner-1"),
			},
		},
		Config: models.Config{IntegrationBranch: "main"},
	}
	config := SupervisorConfig{
		Role:        "code-plan-reviewer",
		AgentID:     "code-plan-reviewer-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}

	prompt, err := buildPromptWithContext(state, config, "task-1", resolver)
	if err != nil {
		t.Fatalf("buildPromptWithContext() error = %v", err)
	}
	assertContainsAll(t, prompt, "=== MASTER DECOMPOSITION REVIEW ===", "missing `plan_ref`")
}

func assertContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func assertNotContains(t *testing.T, got, unwanted string) {
	t.Helper()
	if strings.Contains(got, unwanted) {
		t.Fatalf("prompt contains %q", unwanted)
	}
}

func ptrString(value string) *string {
	return &value
}

func embeddedPipelineResolver(t *testing.T) *pipeline.Resolver {
	t.Helper()
	cfg, err := pipeline.LoadFromBytes(embedded.PipelineConfigContent())
	if err != nil {
		t.Fatalf("LoadFromBytes(embedded pipeline): %v", err)
	}
	return pipeline.NewResolver(cfg)
}

var customMasterRefPromptPipelineYAML = `pipeline:
  roles:
    code-planner:
      type: doer
      display-name: "Code Planner"
      context-sections: [assigned-task]
    code-plan-reviewer:
      type: reviewer
      display-name: "Code Plan Reviewer"
  role-pairs:
    custom-main-pair:
      doer: code-planner
      reviewer: code-plan-reviewer
      decomposition-root: true
      decomposition-output-ref: plan_ref
      states: {initial: DRAFT_CUSTOM_MAIN, executing: CUSTOM_PLANNING_MAIN, submitted: CUSTOM_MAIN_TO_REVIEW, reviewing: REVIEWING_CUSTOM_MAIN, approved: CUSTOM_MAIN_APPROVED, rejected: CUSTOM_MAIN_REJECTED}
    code-planning-pair:
      doer: code-planner
      reviewer: code-plan-reviewer
      states: {initial: DRAFT_CODING_PLAN, executing: CODE_PLANNING, submitted: CODING_PLAN_TO_REVIEW, reviewing: REVIEWING_CODING_PLAN, approved: CODING_PLAN_APPROVED, rejected: CODING_PLAN_REJECTED}
  sub-pipelines:
    coding-subpipeline:
      steps: [custom-main-pair, code-planning-pair]
      transitions:
        - {name: custom-decompose, from: custom-main-pair.approved, to: code-planning-pair.initial, trigger: auto, cardinality: per-subtask}
  entry-points:
    technical-spec: coding-subpipeline.code-planning-pair
`

func TestBuildPromptWithContextScipSearchOmitsEmptyAvailableIndexes(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupPipelineConfig(t, projectRoot)
	taskWorktree := filepath.Join(projectRoot, ".worktrees", "task-1")
	if err := os.MkdirAll(taskWorktree, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", taskWorktree, err)
	}
	testhelpers.SetupTestGitRepo(t, taskWorktree)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	writePromptTestFile(t, filepath.Join(taskWorktree, "go.mod"), "module example.com/task\n")
	testhelpers.MustGit(t, taskWorktree, "add", "go.mod")
	testhelpers.MustGit(t, taskWorktree, "commit", "-m", "Add go module")

	worktree := ".worktrees/task-1"
	state := &models.State{
		Goal: models.Goal{
			Description: "Test goal",
			SpecRef:     "specs/goal.md",
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				DoneWhen:    "Task is complete",
				Worktree:    &worktree,
			},
		},
		Config: models.Config{
			ScipSearch:        []string{"go"},
			IntegrationBranch: "main",
		},
	}
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}

	prompt, err := buildPromptWithContext(state, config, "task-1", testResolver(t))
	if err != nil {
		t.Fatalf("buildPromptWithContext() error = %v", err)
	}
	if strings.Contains(prompt, "=== SCIP-SEARCH INDEXES ===") {
		t.Fatalf("prompt contains scip-search section with no available indexes")
	}
}

func TestBuildPromptWithContextScipAvailableIndexErrorPropagates(t *testing.T) {
	projectRoot := t.TempDir()
	testhelpers.SetupPipelineConfig(t, projectRoot)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	worktree := ".worktrees/missing-task"
	state := &models.State{
		Goal: models.Goal{
			Description: "Test goal",
			SpecRef:     "specs/goal.md",
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				DoneWhen:    "Task is complete",
				Worktree:    &worktree,
			},
		},
		Config: models.Config{
			ScipSearch:        []string{"go"},
			IntegrationBranch: "main",
		},
	}
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: projectRoot,
		SpecsDir:    filepath.Join(projectRoot, "specs"),
		StatePath:   filepath.Join(projectRoot, ".liza", "state.yaml"),
	}

	_, err := buildPromptWithContext(state, config, "task-1", testResolver(t))
	if err == nil {
		t.Fatalf("buildPromptWithContext() error = nil, want available-index error")
	}
	if !strings.Contains(err.Error(), "available scip-search indexes") {
		t.Fatalf("buildPromptWithContext() error = %q, want available-index context", err)
	}
}

func TestBuildOrchestratorPromptContextScipIndexesRenderFromProjectRoot(t *testing.T) {
	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	testhelpers.SetupPipelineConfig(t, tmpDir)
	t.Setenv(scipsearch.EnvEnableScipSearch, "true")

	writePromptTestFile(t, filepath.Join(tmpDir, "go.mod"), "module example.com/project\n")
	testhelpers.MustGit(t, tmpDir, "add", "go.mod")
	testhelpers.MustGit(t, tmpDir, "commit", "-m", "Add go module")

	projectGoIndex := filepath.Join(tmpDir, ".liza", "scip", "go.scip")
	writePromptTestFile(t, projectGoIndex, "go index")

	state := &models.State{
		Goal: models.Goal{
			Description: "Test goal",
			SpecRef:     "specs/goal.md",
		},
		Config: models.Config{
			ScipSearch: []string{"go"},
		},
	}
	config := SupervisorConfig{
		Role:        "orchestrator",
		AgentID:     "orchestrator-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, ".liza", "state.yaml"),
	}

	prompt, err := buildOrchestratorPromptContext(state, config, testResolver(t))
	if err != nil {
		t.Fatalf("buildOrchestratorPromptContext() error = %v", err)
	}

	if !strings.Contains(prompt, projectGoIndex) {
		t.Fatalf("prompt missing project-root SCIP index path %q", projectGoIndex)
	}
	if !strings.Contains(prompt, "scip-search symbols --index '"+projectGoIndex+"' --name Foo --name Bar") {
		t.Fatalf("prompt missing scip-search symbols command for project-root index")
	}
	if !strings.Contains(prompt, "Go symbols, references, and implementations are supported.") {
		t.Fatalf("prompt missing Go capability metadata")
	}
}

// TestBuildPrompt_CollectiveScoping verifies that sibling task info flows through
// from state to the rendered coder/reviewer prompts.
func TestBuildPrompt_CollectiveScoping(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Add auth",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Auth works",
				Scope:       "Auth module",
				Iteration:   1,
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: "Add user API",
				Status:      models.TaskStatusReady,
				Priority:    2,
				SpecRef:     "spec.md",
				DoneWhen:    "API works",
				Scope:       "API module",
				Created:     now,
			},
			{
				ID:          "task-3",
				Description: "Add tests",
				Status:      models.TaskStatusMerged,
				Priority:    3,
				SpecRef:     "spec.md",
				DoneWhen:    "Tests pass",
				Scope:       "Test module",
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-1", "task-2", "task-3"},
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "main",
		},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	// Should contain scoping section with correct ordinal and sibling tasks
	wantContains := []string{
		"COLLECTIVE PLAN SCOPING",
		"1 of 3 in the current sprint",
		"specs/vision.md",
		"RELEVANT TASK GRAPH DIGEST",
		"Plan siblings (scope boundary only)",
		"task-2 [DRAFT_CODE]: Add user API",
		"task-3 [MERGED]: Add tests",
	}
	for _, want := range wantContains {
		if !strings.Contains(prompt, want) {
			t.Errorf("buildPrompt() missing expected scoping content: %q", want)
		}
	}
	// Current task should NOT appear in siblings list
	if strings.Contains(prompt, "task-1 [") {
		t.Error("buildPrompt() should not include current task in sibling list")
	}
}

func TestBuildPrompt_RelevantTaskGraphDigest(t *testing.T) {
	now := time.Now().UTC()
	blockedReason := "waiting for migration plan"
	blockedSiblingReason := "waiting for repository owner"
	worktree := ".worktrees/task-current"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-current",
				Description: "Implement shared repository behavior",
				Status:      models.TaskStatusImplementing,
				RolePair:    "coding-pair",
				Priority:    1,
				DependsOn:   []string{"task-dep", "task-superseded-sibling"},
				SpecRef:     "specs/vision.md",
				DoneWhen:    "internal/repository.go behavior is validated",
				Scope:       "In scope: `internal/repository.go` and tests/repository_test.go",
				Worktree:    &worktree,
				Created:     now,
			},
			{
				ID:            "task-dep",
				Description:   "Prepare migration contract",
				Status:        models.TaskStatusBlocked,
				RolePair:      "code-planning-pair",
				Priority:      1,
				SpecRef:       "specs/migration.md",
				PlanRef:       "specs/plans/migration.md",
				BlockedReason: &blockedReason,
				Output: []models.OutputEntry{
					{PlanRef: "specs/plans/migration-output.md"},
				},
				Created: now,
			},
			{
				ID:          "task-sibling",
				Description: "Document repository behavior",
				Status:      models.TaskStatusMerged,
				RolePair:    "coding-pair",
				Priority:    2,
				SpecRef:     "specs/repository.md",
				PlanRef:     "specs/plans/repository.md",
				Scope:       "In scope: internal/repository.go and docs/repository.md",
				Created:     now,
			},
			{
				ID:          "task-other-sibling",
				Description: "Review unrelated CLI behavior",
				Status:      models.TaskStatusReady,
				RolePair:    "coding-pair",
				Priority:    3,
				SpecRef:     "specs/cli.md",
				PlanRef:     "specs/plans/cli.md",
				Scope:       "In scope: internal/repository.go and cmd/liza/cli.go",
				Created:     now,
			},
			{
				ID:            "task-blocked-sibling",
				Description:   "Coordinate repository behavior",
				Status:        models.TaskStatusBlocked,
				RolePair:      "coding-pair",
				Priority:      4,
				SpecRef:       "specs/blocked.md",
				Scope:         "In scope: internal/repository.go",
				BlockedReason: &blockedSiblingReason,
				Created:       now,
			},
			{
				ID:          "task-superseded-sibling",
				Description: "Superseded repository behavior",
				Status:      models.TaskStatusSuperseded,
				RolePair:    "coding-pair",
				Priority:    5,
				SpecRef:     "specs/superseded.md",
				PlanRef:     "specs/plans/superseded.md",
				Scope:       "In scope: internal/repository.go",
				Created:     now,
			},
			{
				ID:          "task-abandoned-sibling",
				Description: "Abandoned repository behavior",
				Status:      models.TaskStatusAbandoned,
				RolePair:    "coding-pair",
				Priority:    6,
				SpecRef:     "specs/abandoned.md",
				Scope:       "In scope: internal/repository.go",
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-current", "task-sibling", "task-other-sibling", "task-blocked-sibling", "task-superseded-sibling", "task-abandoned-sibling"},
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-current")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	for _, want := range []string{
		"Artifact refs below are repo-relative.",
		"git -C " + filepath.Join(tmpDir, worktree) + " show main:<file-ref>",
		"RELEVANT TASK GRAPH DIGEST",
		"Direct dependencies:",
		"task-dep [BLOCKED, code-planning-pair]: Prepare migration contract",
		"load: liza get task-dep --json",
		"Blocked related tasks:",
		"task-blocked-sibling [BLOCKED, coding-pair]: Coordinate repository behavior",
		"blocker: waiting for migration plan",
		"blocker: waiting for repository owner",
		"Siblings sharing file refs:",
		"task-other-sibling [DRAFT_CODE, coding-pair]: Review unrelated CLI behavior",
		"shared refs:",
		"internal/repository.go",
		"Completed artifacts:",
		"task-sibling [MERGED, coding-pair]: Document repository behavior",
		"plan: specs/plans/repository.md",
		"output: specs/plans/migration-output.md",
		"Plan siblings (scope boundary only):",
		"This task is 1 of 4 in the current sprint plan.",
		"task-sibling [MERGED]: Document repository behavior",
		"task-other-sibling [DRAFT_CODE]: Review unrelated CLI behavior",
		"task-blocked-sibling [BLOCKED]: Coordinate repository behavior",
		"SIBLING CONSISTENCY RULE:",
		"task-superseded-sibling [SUPERSEDED] — plan: specs/plans/superseded.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("buildPrompt() missing task graph digest content: %q", want)
		}
	}

	blockedStart := strings.Index(prompt, "Blocked related tasks:")
	siblingsStart := strings.Index(prompt, "Siblings sharing file refs:")
	completedStart := strings.Index(prompt, "Completed artifacts:")
	if blockedStart == -1 || siblingsStart == -1 || completedStart == -1 ||
		!(blockedStart < siblingsStart && siblingsStart < completedStart) {
		t.Fatalf("buildPrompt() missing ordered sibling/completed task graph sections")
	}
	blockedSection := prompt[blockedStart:siblingsStart]
	for _, want := range []string{
		"task-blocked-sibling [BLOCKED, coding-pair]: Coordinate repository behavior",
		"shared refs:",
		"internal/repository.go",
		"blocker: waiting for repository owner",
	} {
		if !strings.Contains(blockedSection, want) {
			t.Errorf("blocked related task section missing %q:\n%s", want, blockedSection)
		}
	}

	siblingsSection := prompt[siblingsStart:completedStart]
	for _, unwanted := range []string{
		"task-sibling [MERGED, coding-pair]",
		"task-superseded-sibling [SUPERSEDED, coding-pair]",
		"task-abandoned-sibling [ABANDONED, coding-pair]",
	} {
		if strings.Contains(siblingsSection, unwanted) {
			t.Errorf("terminal sibling should not render in shared-ref sibling section: %q\n%s", unwanted, siblingsSection)
		}
	}
	for _, unwanted := range []string{
		"task-superseded-sibling [SUPERSEDED]: Superseded repository behavior",
		"task-abandoned-sibling [ABANDONED]: Abandoned repository behavior",
	} {
		planStart := strings.Index(prompt, "Plan siblings (scope boundary only):")
		consistencyStart := strings.Index(prompt, "SIBLING CONSISTENCY RULE:")
		if planStart == -1 || consistencyStart == -1 || consistencyStart < planStart {
			t.Fatalf("buildPrompt() missing ordered plan siblings and consistency sections")
		}
		planSection := prompt[planStart:consistencyStart]
		if strings.Contains(planSection, unwanted) {
			t.Errorf("dead-path sibling should not render in plan sibling list: %q\n%s", unwanted, planSection)
		}
	}
}

func TestBuildPrompt_ArtifactRefsUseRepoRelativeFallback(t *testing.T) {
	now := time.Now().UTC()
	worktree := ".worktrees/task-1"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Implement feature",
				Status:      models.TaskStatusImplementing,
				RolePair:    "coding-pair",
				Priority:    1,
				SpecRef:     "specs/vision.md",
				EpicRef:     "specs/epics/feature.md#capability-signup",
				PlanRef:     "specs/plans/feature.md#task-1",
				ArchRef:     "specs/arch-plan/feature.md",
				Worktree:    &worktree,
				DoneWhen:    "Feature works",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}
	tmpDir := t.TempDir()
	absWorktree := filepath.Join(tmpDir, worktree)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	for _, want := range []string{
		"Artifact refs below are repo-relative.",
		"Read " + absWorktree + "/<ref> first",
		"git -C " + absWorktree + " show integration:<file-ref>",
		"Read the implementation plan at specs/plans/feature.md for task decomposition context.",
		"Read the architecture document at specs/arch-plan/feature.md for structural context and component boundaries.",
		"Read the epic at specs/epics/feature.md for requirement context",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	for _, notWant := range []string{
		filepath.Join(absWorktree, "specs/plans/feature.md"),
		filepath.Join(absWorktree, "specs/arch-plan/feature.md"),
		filepath.Join(absWorktree, "specs/epics/feature.md"),
		"specs/plans/feature.md#task-1",
		"specs/epics/feature.md#capability-signup for requirement context",
	} {
		if strings.Contains(prompt, notWant) {
			t.Errorf("prompt contains stale artifact rendering %q", notWant)
		}
	}
}

func TestBuildPrompt_ReviewerArtifactRefsUseIntegrationFallback(t *testing.T) {
	now := time.Now().UTC()
	worktree := ".worktrees/task-1"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:           "task-1",
				Description:  "Review feature",
				Status:       models.TaskStatusReviewing,
				RolePair:     "coding-pair",
				Priority:     1,
				SpecRef:      "specs/vision.md",
				PlanRef:      "specs/plans/feature.md#task-1",
				ArchRef:      "specs/arch-plan/feature.md",
				Worktree:     &worktree,
				DoneWhen:     "Feature works",
				BaseCommit:   testhelpers.StringPtr("base"),
				ReviewCommit: testhelpers.StringPtr("review"),
				Created:      now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}
	tmpDir := t.TempDir()
	absWorktree := filepath.Join(tmpDir, worktree)
	config := SupervisorConfig{
		Role:        "code-reviewer",
		AgentID:     "code-reviewer-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	for _, want := range []string{
		"Artifact refs below are repo-relative.",
		"Read " + absWorktree + "/<ref> first",
		"git -C " + absWorktree + " show integration:<file-ref>",
		"- Architecture document: specs/arch-plan/feature.md",
		"- Implementation plan: specs/plans/feature.md",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, filepath.Join(absWorktree, "specs/plans/feature.md")) {
		t.Errorf("prompt contains worktree-prefixed plan ref")
	}
}

func TestBuildPrompt_USWriterEpicRefFallbackPreservesFragment(t *testing.T) {
	now := time.Now().UTC()
	worktree := ".worktrees/us-1"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "us-1",
				Description: "Write signup stories",
				Status:      models.TaskStatus("WRITING_USER_STORIES"),
				RolePair:    "us-writing-pair",
				Priority:    1,
				SpecRef:     "specs/vision.md",
				EpicRef:     "specs/epics/feature.md#capability-signup",
				Worktree:    &worktree,
				DoneWhen:    "Stories are written",
				Scope:       "capability signup",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}
	tmpDir := t.TempDir()
	absWorktree := filepath.Join(tmpDir, worktree)
	config := SupervisorConfig{
		Role:        "us-writer",
		AgentID:     "us-writer-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "us-1")
	if err != nil {
		t.Fatalf("buildPrompt: %v", err)
	}

	for _, want := range []string{
		"The parent epic ref is specs/epics/feature.md.",
		"Read " + absWorktree + "/specs/epics/feature.md first",
		"git -C " + absWorktree + " show integration:specs/epics/feature.md",
		"Then use section #capability-signup.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "show integration:specs/epics/feature.md#capability-signup") {
		t.Error("fallback command must use file ref without fragment")
	}
}

// TestBuildPrompt_NoScopingForSinglePlannedTask verifies no scoping section
// when the sprint has only one planned task.
func TestBuildPrompt_NoScopingForSinglePlannedTask(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Solo task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Done",
				Scope:       "Everything",
				Iteration:   1,
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-1"},
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "main",
		},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if strings.Contains(prompt, "COLLECTIVE PLAN SCOPING") {
		t.Error("buildPrompt() should NOT contain scoping for single-task sprint")
	}
}

// TestBuildPrompt_CollectiveScopingOrdinal verifies the ordinal is computed
// correctly for non-first tasks in the sprint plan.
func TestBuildPrompt_CollectiveScopingOrdinal(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Add auth",
				Status:      models.TaskStatusMerged,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Auth works",
				Scope:       "Auth module",
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: "Add user API",
				Status:      models.TaskStatusImplementing,
				Priority:    2,
				SpecRef:     "spec.md",
				DoneWhen:    "API works",
				Scope:       "API module",
				Iteration:   1,
				Created:     now,
			},
			{
				ID:          "task-3",
				Description: "Add tests",
				Status:      models.TaskStatusReady,
				Priority:    3,
				SpecRef:     "spec.md",
				DoneWhen:    "Tests pass",
				Scope:       "Test module",
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-1", "task-2", "task-3"},
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "main",
		},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	// Build prompt for task-2 (second in plan)
	prompt, err := testBuildPrompt(t, state, config, "task-2")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "2 of 3 in the current sprint") {
		t.Error("buildPrompt() should show correct ordinal (2 of 3) for second task")
	}
	if strings.Contains(prompt, "1 of 3") {
		t.Error("buildPrompt() should NOT hardcode ordinal to 1")
	}
	// task-2 (current) should not appear in siblings
	if strings.Contains(prompt, "task-2 [") {
		t.Error("buildPrompt() should not include current task in sibling list")
	}
	// task-1 and task-3 should appear as siblings
	if !strings.Contains(prompt, "task-1 [MERGED]: Add auth") {
		t.Error("buildPrompt() should include task-1 as sibling")
	}
	if !strings.Contains(prompt, "task-3 [DRAFT_CODE]: Add tests") {
		t.Error("buildPrompt() should include task-3 as sibling")
	}
}

// TestBuildPrompt_NoScopingForUnplannedTask verifies that mid-sprint replacement
// tasks not in Sprint.Scope.Planned do not get the scoping section.
func TestBuildPrompt_NoScopingForUnplannedTask(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "specs/vision.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Original task",
				Status:      models.TaskStatusMerged,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Done",
				Scope:       "Module A",
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: "Another planned task",
				Status:      models.TaskStatusReady,
				Priority:    2,
				SpecRef:     "spec.md",
				DoneWhen:    "Done",
				Scope:       "Module B",
				Created:     now,
			},
			{
				// Mid-sprint replacement — not in planned[]
				ID:          "task-3-replacement",
				Description: "Replacement for blocked task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				SpecRef:     "spec.md",
				DoneWhen:    "Replacement done",
				Scope:       "Module C",
				Iteration:   1,
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-1", "task-2"}, // task-3-replacement not here
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "main",
		},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-3-replacement")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if strings.Contains(prompt, "COLLECTIVE PLAN SCOPING") {
		t.Error("buildPrompt() should NOT show scoping for unplanned replacement task")
	}
	if strings.Contains(prompt, "0 of") {
		t.Error("buildPrompt() should NOT render '0 of N' for unplanned task")
	}
}

// TestBuildPrompt_IntegrationFixPropagation verifies that when a coder task has
// IntegrationFix=true, the field propagates into RoleContextData and the
// integration-fix block renders in the prompt.
func TestBuildPrompt_IntegrationFixPropagation(t *testing.T) {
	now := time.Now().UTC()
	wt := ".worktrees/task-fix"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:             "task-fix",
				Description:    "Fix integration conflict",
				Status:         models.TaskStatusImplementing,
				Priority:       1,
				SpecRef:        "spec.md",
				DoneWhen:       "Conflict resolved",
				Scope:          "module A",
				Iteration:      1,
				IntegrationFix: true,
				Worktree:       &wt,
				Created:        now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{
			IntegrationBranch: "integration",
		},
	}

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-fix")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "INTEGRATION FIX MODE") {
		t.Error("prompt should contain INTEGRATION FIX MODE when task.IntegrationFix is true")
	}
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		input       string
		wantFile    string
		wantSection string
	}{
		{"specs/plans/EP-001.md#capability-cap-001---task-creation", "specs/plans/EP-001.md", "capability-cap-001---task-creation"},
		{"specs/plans/EP-001.md", "specs/plans/EP-001.md", ""},
		{"", "", ""},
		{"#section-only", "", "section-only"},
	}
	for _, tc := range tests {
		if got := paths.SplitRefFile(tc.input); got != tc.wantFile {
			t.Errorf("SplitRefFile(%q) = %q, want %q", tc.input, got, tc.wantFile)
		}
		if got := paths.SplitRefFragment(tc.input); got != tc.wantSection {
			t.Errorf("SplitRefFragment(%q) = %q, want %q", tc.input, got, tc.wantSection)
		}
	}
}

func TestCollectSiblingTasks_TruncatesLongDescriptions(t *testing.T) {
	now := time.Now().UTC()
	longDesc := strings.Repeat("x", 400)
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Current task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Created:     now,
			},
			{
				ID:          "task-2",
				Description: longDesc,
				Status:      models.TaskStatusReady,
				Priority:    2,
				Created:     now,
			},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-1", "task-2"},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	siblings, total, ordinal := collectSiblingTasks(state, "task-1")
	if total != 2 || ordinal != 1 {
		t.Fatalf("total=%d ordinal=%d, want 2, 1", total, ordinal)
	}
	if len(siblings) != 1 {
		t.Fatalf("len(siblings) = %d, want 1", len(siblings))
	}
	if len(siblings[0].Description) > 203 { // 200 + len("...")
		t.Errorf("sibling description not truncated: len=%d", len(siblings[0].Description))
	}
	if !strings.HasSuffix(siblings[0].Description, "...") {
		t.Error("truncated description should end with ellipsis")
	}
}

func TestCollectSiblingTasks_UsesVisiblePlanCountAndOrdinal(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{ID: "task-superseded", Description: "Superseded task", Status: models.TaskStatusSuperseded, Created: now},
			{ID: "task-current", Description: "Current task", Status: models.TaskStatusImplementing, Created: now},
			{ID: "task-active", Description: "Active task", Status: models.TaskStatusReady, Created: now},
			{ID: "task-abandoned", Description: "Abandoned task", Status: models.TaskStatusAbandoned, Created: now},
		},
		Sprint: models.Sprint{
			Scope: models.SprintScope{
				Planned: []string{"task-superseded", "task-current", "task-active", "task-abandoned"},
			},
		},
		Agents: make(map[string]models.Agent),
	}

	siblings, total, ordinal := collectSiblingTasks(state, "task-current")
	if total != 2 || ordinal != 1 {
		t.Fatalf("total=%d ordinal=%d, want visible total 2 and visible ordinal 1", total, ordinal)
	}
	if len(siblings) != 1 {
		t.Fatalf("len(siblings) = %d, want 1", len(siblings))
	}
	if siblings[0].ID != "task-active" {
		t.Fatalf("sibling ID = %q, want task-active", siblings[0].ID)
	}
}

// TestPromptSaving tests prompt file creation
func TestPromptSaving(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "agent-prompts")

	prompt := "Test prompt content"
	agentID := "coder-1"

	filePath, err := savePrompt(promptDir, agentID, prompt)
	if err != nil {
		t.Fatalf("savePrompt() error = %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(promptDir); os.IsNotExist(err) {
		t.Error("Prompt directory should be created")
	}

	// Verify file exists and has correct content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read prompt file: %v", err)
	}

	if string(content) != prompt {
		t.Errorf("Prompt content = %q, want %q", string(content), prompt)
	}

	// Verify filename format
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, agentID+"-") {
		t.Errorf("Filename should start with agent ID, got %s", filename)
	}
	if !strings.HasSuffix(filename, ".txt") {
		t.Errorf("Filename should end with .txt, got %s", filename)
	}
}

// TestSavePromptMultipleCalls tests that savePrompt creates unique filenames
func TestSavePromptMultipleCalls(t *testing.T) {
	tmpDir := t.TempDir()
	promptDir := filepath.Join(tmpDir, "prompts")

	// Save multiple prompts
	path1, err := savePrompt(promptDir, "coder-1", "prompt 1")
	if err != nil {
		t.Fatalf("savePrompt() error = %v", err)
	}

	// savePrompt uses second-resolution timestamps (20060102-150405).
	// Wait until the wall-clock second advances so the next call produces
	// a distinct filename, instead of polling savePrompt in a tight loop
	// which would create dozens of duplicate files as a side effect.
	startSec := time.Now().UTC().Truncate(time.Second)
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().UTC().Truncate(time.Second).Equal(startSec) {
		select {
		case <-deadline:
			t.Fatal("wall-clock second did not advance within timeout")
		case <-ticker.C:
		}
	}

	path2, err := savePrompt(promptDir, "coder-1", "prompt 2")
	if err != nil {
		t.Fatalf("savePrompt() error = %v", err)
	}

	// Verify paths are different
	if path1 == path2 {
		t.Error("savePrompt() should create unique filenames")
	}

	// Verify both files exist
	if _, err := os.Stat(path1); os.IsNotExist(err) {
		t.Error("First prompt file should exist")
	}
	if _, err := os.Stat(path2); os.IsNotExist(err) {
		t.Error("Second prompt file should exist")
	}
}

// TestOutputSaving tests agent output file creation
func TestOutputSaving(t *testing.T) {
	tmpDir := t.TempDir()
	outputsDir := filepath.Join(tmpDir, "agent-outputs")

	output := "Test agent output content"
	agentID := "claude-1"

	filePath, err := saveOutput(outputsDir, agentID, "txt", output, nil)
	if err != nil {
		t.Fatalf("saveOutput() error = %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(outputsDir); os.IsNotExist(err) {
		t.Error("Outputs directory should be created")
	}

	// Verify file exists and has correct content
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if string(content) != output {
		t.Errorf("Output content = %q, want %q", string(content), output)
	}

	// Verify filename format
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, agentID+"-") {
		t.Errorf("Filename should start with agent ID, got %s", filename)
	}
	if !strings.HasSuffix(filename, ".txt") {
		t.Errorf("Filename should end with .txt, got %s", filename)
	}
}

// TestBuildPrompt_CoderAttemptDisplay_Attempt2 verifies that the coder prompt
// at attempt 2 contains "ATTEMPT: 2" and "FINAL ATTEMPT".
func TestBuildPrompt_CoderAttemptDisplay_Attempt2(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "ATTEMPT: 2") {
		t.Error("coder prompt at attempt 2 should contain 'ATTEMPT: 2'")
	}
	if !strings.Contains(prompt, "FINAL ATTEMPT") {
		t.Error("coder prompt at attempt 2 should contain 'FINAL ATTEMPT'")
	}
}

// TestBuildPrompt_CoderAttemptDisplay_Attempt1 verifies that the coder prompt
// at attempt 1 contains "ATTEMPT: 1" but not "FINAL ATTEMPT".
func TestBuildPrompt_CoderAttemptDisplay_Attempt1(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     1,
				DoneWhen:    "Done",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "ATTEMPT: 1") {
		t.Error("coder prompt at attempt 1 should contain 'ATTEMPT: 1'")
	}
	if strings.Contains(prompt, "FINAL ATTEMPT") {
		t.Error("coder prompt at attempt 1 should NOT contain 'FINAL ATTEMPT'")
	}
}

// TestBuildPrompt_ReviewerAttemptDisplay_Attempt2 verifies that the reviewer prompt
// at attempt 2 contains "ATTEMPT: 2" and "FINAL ATTEMPT".
func TestBuildPrompt_ReviewerAttemptDisplay_Attempt2(t *testing.T) {
	now := time.Now().UTC()
	assignedTo := "coder-1"
	baseCommit := "abc123"
	reviewCommit := "def456"
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:           "task-1",
				Description:  "Test task",
				Status:       models.TaskStatusReadyForReview,
				Priority:     1,
				Iteration:    1,
				Attempt:      2,
				DoneWhen:     "Done",
				AssignedTo:   &assignedTo,
				BaseCommit:   &baseCommit,
				ReviewCommit: &reviewCommit,
				Created:      now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "code-reviewer",
		AgentID:     "code-reviewer-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "ATTEMPT: 2") {
		t.Error("reviewer prompt at attempt 2 should contain 'ATTEMPT: 2'")
	}
	if !strings.Contains(prompt, "FINAL ATTEMPT") {
		t.Error("reviewer prompt at attempt 2 should contain 'FINAL ATTEMPT'")
	}
}

// TestBuildTaskRoleContextData_AttemptNum_UsesEffectiveAttempt verifies that
// AttemptNum is populated via task.EffectiveAttempt(), not len(task.Attempted)+1.
func TestBuildTaskRoleContextData_AttemptNum_UsesEffectiveAttempt(t *testing.T) {
	now := time.Now().UTC()
	resolver := testResolver(t)

	makeState := func(attempt int) *models.State {
		return &models.State{
			Version: 1,
			Goal: models.Goal{
				ID:          "goal-1",
				Description: "Test goal",
				SpecRef:     "spec.md",
				Status:      models.GoalStatusInProgress,
				Created:     now,
			},
			Tasks: []models.Task{
				{
					ID:          "task-1",
					Description: "Test task",
					Status:      models.TaskStatusImplementing,
					Priority:    1,
					Iteration:   1,
					Attempt:     attempt,
					DoneWhen:    "Done",
					Created:     now,
				},
			},
			Agents: make(map[string]models.Agent),
			Config: models.Config{IntegrationBranch: "main"},
		}
	}

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	// Attempt: 2 → AttemptNum == 2
	state := makeState(2)
	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}
	if data.AttemptNum != 2 {
		t.Errorf("AttemptNum = %d, want 2 (Attempt=2)", data.AttemptNum)
	}

	// Attempt: 0 → AttemptNum == 1 (backward compat via EffectiveAttempt)
	state = makeState(0)
	data, err = buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData (second call): %v", err)
	}
	if data.AttemptNum != 1 {
		t.Errorf("AttemptNum = %d, want 1 (Attempt=0, backward compat)", data.AttemptNum)
	}
}

// TestBuildTaskRoleContextData_PriorAttemptOutcome_Attempt2 verifies that
// PriorAttemptOutcome is populated from the last new_attempt history entry's Reason
// when AttemptNum == 2, and is empty when AttemptNum == 1.
func TestBuildTaskRoleContextData_PriorAttemptOutcome_Attempt2(t *testing.T) {
	now := time.Now().UTC()
	resolver := testResolver(t)
	reason := "review cycle limit reached after 5 rejections"

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	// Attempt 2 with new_attempt history entry → PriorAttemptOutcome populated
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason},
				},
				Created: now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}
	if data.PriorAttemptOutcome != reason {
		t.Errorf("PriorAttemptOutcome = %q, want %q", data.PriorAttemptOutcome, reason)
	}

	// Attempt 1 → PriorAttemptOutcome empty (even with history)
	state.Tasks[0].Attempt = 1
	data, err = buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData (second call): %v", err)
	}
	if data.PriorAttemptOutcome != "" {
		t.Errorf("PriorAttemptOutcome = %q, want empty for attempt 1", data.PriorAttemptOutcome)
	}
}

// TestBuildTaskRoleContextData_PriorRejectionGate_Attempt2Iteration1_Empty verifies that
// at attempt 2, iteration 1, PriorRejection remains empty even with a non-empty RejectionReason.
// This confirms the task.Iteration > 1 gate is unchanged.
func TestBuildTaskRoleContextData_PriorRejectionGate_Attempt2Iteration1_Empty(t *testing.T) {
	now := time.Now().UTC()
	resolver := testResolver(t)
	rejectionReason := "code quality issues found"

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:              "task-1",
				Description:     "Test task",
				Status:          models.TaskStatusImplementing,
				Priority:        1,
				Iteration:       1,
				Attempt:         2,
				RejectionReason: &rejectionReason,
				DoneWhen:        "Done",
				Created:         now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}
	if data.PriorRejection != "" {
		t.Errorf("PriorRejection = %q, want empty at attempt 2 iteration 1", data.PriorRejection)
	}
}

// TestBuildPrompt_PriorAttemptOutcome_CoderAttempt2 verifies that the coder prompt
// at attempt 2 contains PRIOR ATTEMPT OUTCOME with reason, and at attempt 1 does not.
func TestBuildPrompt_PriorAttemptOutcome_CoderAttempt2(t *testing.T) {
	now := time.Now().UTC()
	reason := "review cycle limit reached"

	makeState := func(attempt int) *models.State {
		s := &models.State{
			Version: 1,
			Goal: models.Goal{
				ID:          "goal-1",
				Description: "Test goal",
				SpecRef:     "spec.md",
				Status:      models.GoalStatusInProgress,
				Created:     now,
			},
			Tasks: []models.Task{
				{
					ID:          "task-1",
					Description: "Test task",
					Status:      models.TaskStatusImplementing,
					Priority:    1,
					Iteration:   1,
					Attempt:     attempt,
					DoneWhen:    "Done",
					History: []models.TaskHistoryEntry{
						{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
						{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason},
					},
					Created: now,
				},
			},
			Agents: make(map[string]models.Agent),
			Config: models.Config{IntegrationBranch: "main"},
		}
		return s
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	// Attempt 2: prompt should contain PRIOR ATTEMPT OUTCOME with reason
	prompt, err := testBuildPrompt(t, makeState(2), config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}
	if !strings.Contains(prompt, "PRIOR ATTEMPT OUTCOME") {
		t.Error("coder prompt at attempt 2 should contain 'PRIOR ATTEMPT OUTCOME'")
	}
	if !strings.Contains(prompt, reason) {
		t.Errorf("coder prompt at attempt 2 should contain reason %q", reason)
	}

	// Attempt 1: prompt should NOT contain PRIOR ATTEMPT OUTCOME
	prompt, err = testBuildPrompt(t, makeState(1), config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}
	if strings.Contains(prompt, "PRIOR ATTEMPT OUTCOME") {
		t.Error("coder prompt at attempt 1 should NOT contain 'PRIOR ATTEMPT OUTCOME'")
	}
}

// TestBuildPrompt_PriorAttemptOutcome_CodePlannerAttempt2 verifies that the code-planner
// prompt at attempt 2 contains PRIOR ATTEMPT OUTCOME with reason.
func TestBuildPrompt_PriorAttemptOutcome_CodePlannerAttempt2(t *testing.T) {
	now := time.Now().UTC()
	reason := "iteration limit reached"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusCodePlanning,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason},
				},
				Created: now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "code-planner",
		AgentID:     "code-planner-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}
	if !strings.Contains(prompt, "PRIOR ATTEMPT OUTCOME") {
		t.Error("code-planner prompt at attempt 2 should contain 'PRIOR ATTEMPT OUTCOME'")
	}
	if !strings.Contains(prompt, reason) {
		t.Errorf("code-planner prompt at attempt 2 should contain reason %q", reason)
	}
}

// TestPipelineConfig_PriorAttempt_DoerRolesOnly verifies that prior-attempt
// appears in context-sections for exactly the doer roles and no others.
func TestPipelineConfig_PriorAttempt_DoerRolesOnly(t *testing.T) {
	resolver := testResolver(t)

	var rolesWithPriorAttempt []string
	for _, role := range resolver.AllRoleNames() {
		sections, err := resolver.ContextSections(role)
		if err != nil {
			t.Fatalf("ContextSections(%q) error: %v", role, err)
		}
		if slices.Contains(sections, "prior-attempt") {
			rolesWithPriorAttempt = append(rolesWithPriorAttempt, role)
		}
	}

	slices.Sort(rolesWithPriorAttempt)
	doerRoles := resolver.DoerRoleNames() // already sorted

	if !slices.Equal(rolesWithPriorAttempt, doerRoles) {
		t.Errorf("roles with prior-attempt = %v, want exactly doer roles %v", rolesWithPriorAttempt, doerRoles)
	}
}

// TestBuildPrompt_LimitsLine_FreshAttempt verifies that the coder prompt contains
// the updated LIMITS text referencing fresh attempts instead of plain BLOCKED escalation.
func TestBuildPrompt_LimitsLine_FreshAttempt(t *testing.T) {
	now := time.Now().UTC()
	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     1,
				DoneWhen:    "Done",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}

	if !strings.Contains(prompt, "task starts fresh attempt when limits reached") {
		t.Error("coder prompt should contain 'task starts fresh attempt when limits reached'")
	}
}

// TestBuildTaskRoleContextData_PriorAttemptRejection_Attempt2 verifies that
// PriorAttemptRejection is populated from the new_attempt history entry's Note
// when AttemptNum == 2 and Note is present.
func TestBuildTaskRoleContextData_PriorAttemptRejection_Attempt2(t *testing.T) {
	now := time.Now().UTC()
	resolver := testResolver(t)
	reason := "review cycle limit reached"
	note := "Needs improvement"

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason, Note: &note},
				},
				Created: now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}
	if data.PriorAttemptRejection != "Needs improvement" {
		t.Errorf("PriorAttemptRejection = %q, want %q", data.PriorAttemptRejection, "Needs improvement")
	}
}

// TestBuildTaskRoleContextData_PriorAttemptRejection_Attempt2_NilNote verifies that
// PriorAttemptRejection is empty when the new_attempt history entry has no Note.
func TestBuildTaskRoleContextData_PriorAttemptRejection_Attempt2_NilNote(t *testing.T) {
	now := time.Now().UTC()
	resolver := testResolver(t)
	reason := "review cycle limit reached"

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason},
				},
				Created: now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}
	if data.PriorAttemptRejection != "" {
		t.Errorf("PriorAttemptRejection = %q, want empty for nil Note", data.PriorAttemptRejection)
	}
}

// TestBuildPrompt_PriorAttemptRejection_CoderAttempt2 verifies that the coder prompt
// at attempt 2 contains "LAST REVIEWER FEEDBACK" and the feedback text when the
// new_attempt history entry has a Note.
func TestBuildPrompt_PriorAttemptRejection_CoderAttempt2(t *testing.T) {
	now := time.Now().UTC()
	reason := "review cycle limit reached"
	note := "The error handling in parse.go doesn't cover EOF"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				Attempt:     2,
				DoneWhen:    "Done",
				History: []models.TaskHistoryEntry{
					{Time: now.Add(-time.Hour), Event: models.TaskEventClaimed},
					{Time: now.Add(-time.Minute), Event: models.TaskEventNewAttempt, Reason: &reason, Note: &note},
				},
				Created: now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupPipelineConfig(t, tmpDir)
	config := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	prompt, err := testBuildPrompt(t, state, config, "task-1")
	if err != nil {
		t.Fatalf("BuildPrompt() error: %v", err)
	}
	if !strings.Contains(prompt, "LAST REVIEWER FEEDBACK") {
		t.Error("coder prompt at attempt 2 should contain 'LAST REVIEWER FEEDBACK'")
	}
	if !strings.Contains(prompt, note) {
		t.Errorf("coder prompt at attempt 2 should contain feedback text %q", note)
	}
}

// integrationTestPipelineYAML is a minimal pipeline config with integration roles
// for testing integration context population in buildTaskRoleContextData.
var integrationTestPipelineYAML = `pipeline:
  roles:
    integration-analyst:
      type: doer
      display-name: "Integration Analyst"
      context-sections:
        - assigned-task
    integration-reviewer:
      type: reviewer
      display-name: "Integration Reviewer"
      context-sections:
        - review-task
    coder:
      type: doer
      display-name: "Coder"
      context-sections:
        - assigned-task
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      context-sections:
        - review-task
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      context-sections:
        - orchestrator-dashboard
  role-pairs:
    integration-pair:
      doer: integration-analyst
      reviewer: integration-reviewer
      states:
        initial: DRAFT_INTEGRATION_ANALYSIS
        executing: ANALYZING_INTEGRATION
        submitted: INTEGRATION_ANALYSIS_TO_REVIEW
        reviewing: REVIEWING_INTEGRATION_ANALYSIS
        approved: INTEGRATION_ANALYSIS_APPROVED
        rejected: INTEGRATION_ANALYSIS_REJECTED
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
`

// TestBuildTaskRoleContextData_IntegrationAnalyst verifies that integration-analyst
// receives GoalBaseCommit and CompletedTasks populated from the state.
func TestBuildTaskRoleContextData_IntegrationAnalyst(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, integrationTestPipelineYAML)
	baseCommit := "abc123def456"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			BaseCommit:  &baseCommit,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "analysis-task",
				Description: "Analyze integration",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Analysis complete",
				Created:     now,
			},
			{
				ID:          "task-merged-1",
				Description: "First merged task",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Iteration:   1,
				DoneWhen:    "Tests pass for feature A",
				SpecRef:     "specs/feature-a.md",
				Created:     now,
			},
			{
				ID:          "task-merged-2",
				Description: "Second merged task",
				Status:      models.TaskStatusMerged,
				Priority:    3,
				Iteration:   1,
				DoneWhen:    "API endpoint returns 200",
				SpecRef:     "specs/feature-b.md",
				Created:     now,
			},
			{
				ID:          "task-implementing",
				Description: "Still in progress",
				Status:      models.TaskStatusImplementing,
				Priority:    4,
				Iteration:   1,
				DoneWhen:    "Should not appear",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}

	config := SupervisorConfig{
		Role:    "integration-analyst",
		AgentID: "integration-analyst-1",
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}

	if data.GoalBaseCommit != baseCommit {
		t.Errorf("GoalBaseCommit = %q, want %q", data.GoalBaseCommit, baseCommit)
	}
	if len(data.CompletedTasks) != 2 {
		t.Fatalf("CompletedTasks length = %d, want 2", len(data.CompletedTasks))
	}

	// Verify first merged task
	found := make(map[string]bool)
	for _, ct := range data.CompletedTasks {
		found[ct.ID] = true
		switch ct.ID {
		case "task-merged-1":
			if ct.DoneWhen != "Tests pass for feature A" {
				t.Errorf("task-merged-1 DoneWhen = %q, want %q", ct.DoneWhen, "Tests pass for feature A")
			}
			if ct.SpecRef != "specs/feature-a.md" {
				t.Errorf("task-merged-1 SpecRef = %q, want %q", ct.SpecRef, "specs/feature-a.md")
			}
		case "task-merged-2":
			if ct.DoneWhen != "API endpoint returns 200" {
				t.Errorf("task-merged-2 DoneWhen = %q, want %q", ct.DoneWhen, "API endpoint returns 200")
			}
			if ct.SpecRef != "specs/feature-b.md" {
				t.Errorf("task-merged-2 SpecRef = %q, want %q", ct.SpecRef, "specs/feature-b.md")
			}
		default:
			t.Errorf("unexpected completed task ID: %q", ct.ID)
		}
	}
	if !found["task-merged-1"] || !found["task-merged-2"] {
		t.Errorf("missing expected completed tasks: got IDs %v", found)
	}
}

// TestBuildTaskRoleContextData_IntegrationReviewer verifies that integration-reviewer
// receives the same integration context fields as the analyst.
func TestBuildTaskRoleContextData_IntegrationReviewer(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, integrationTestPipelineYAML)
	baseCommit := "abc123def456"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			BaseCommit:  &baseCommit,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "review-task",
				Description: "Review integration",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Review complete",
				Created:     now,
			},
			{
				ID:          "task-merged-1",
				Description: "Merged task",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Iteration:   1,
				DoneWhen:    "Feature works",
				SpecRef:     "specs/feature.md",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}

	config := SupervisorConfig{
		Role:    "integration-reviewer",
		AgentID: "integration-reviewer-1",
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}

	if data.GoalBaseCommit != baseCommit {
		t.Errorf("GoalBaseCommit = %q, want %q", data.GoalBaseCommit, baseCommit)
	}
	if len(data.CompletedTasks) != 1 {
		t.Fatalf("CompletedTasks length = %d, want 1", len(data.CompletedTasks))
	}
	if data.CompletedTasks[0].ID != "task-merged-1" {
		t.Errorf("CompletedTasks[0].ID = %q, want %q", data.CompletedTasks[0].ID, "task-merged-1")
	}
	if data.CompletedTasks[0].DoneWhen != "Feature works" {
		t.Errorf("CompletedTasks[0].DoneWhen = %q, want %q", data.CompletedTasks[0].DoneWhen, "Feature works")
	}
	if data.CompletedTasks[0].SpecRef != "specs/feature.md" {
		t.Errorf("CompletedTasks[0].SpecRef = %q, want %q", data.CompletedTasks[0].SpecRef, "specs/feature.md")
	}
}

// TestBuildTaskRoleContextData_CoderNoIntegrationFields verifies that coder role
// does not receive integration-specific fields even when the state has them.
func TestBuildTaskRoleContextData_CoderNoIntegrationFields(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, integrationTestPipelineYAML)
	baseCommit := "abc123def456"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			BaseCommit:  &baseCommit,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "coder-task",
				Description: "Implement feature",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Tests pass",
				Created:     now,
			},
			{
				ID:          "task-merged",
				Description: "Already merged",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Iteration:   1,
				DoneWhen:    "Done",
				SpecRef:     "specs/done.md",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "integration"},
	}

	config := SupervisorConfig{
		Role:    "coder",
		AgentID: "coder-1",
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}

	if data.GoalBaseCommit != "" {
		t.Errorf("GoalBaseCommit = %q, want empty for coder role", data.GoalBaseCommit)
	}
	if data.CompletedTasks != nil {
		t.Errorf("CompletedTasks = %v, want nil for coder role", data.CompletedTasks)
	}
}

// architectTestPipelineYAML is a minimal pipeline config with architect role
// for testing ArchRef and ParentTaskContexts population in buildTaskRoleContextData.
var architectTestPipelineYAML = `pipeline:
  roles:
    architect:
      type: doer
      display-name: "Architect"
      context-sections:
        - assigned-task
    architecture-reviewer:
      type: reviewer
      display-name: "Architecture Reviewer"
      context-sections:
        - review-task
    coder:
      type: doer
      display-name: "Coder"
      context-sections:
        - assigned-task
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      context-sections:
        - review-task
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      context-sections:
        - orchestrator-dashboard
  role-pairs:
    architecture-pair:
      doer: architect
      reviewer: architecture-reviewer
      states:
        initial: DRAFT_ARCHITECTURE
        executing: ARCHITECTING
        submitted: ARCHITECTURE_TO_REVIEW
        reviewing: REVIEWING_ARCHITECTURE
        approved: ARCHITECTURE_APPROVED
        rejected: ARCHITECTURE_REJECTED
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    architecture-subpipeline:
      steps:
        - architecture-pair

    coding-subpipeline:
      steps:
        - coding-pair
  pipeline-transitions:
    - name: architecture-to-coding
      from: architecture-subpipeline.architecture-pair.approved
      to: coding-subpipeline.coding-pair.initial
      trigger: manual
      cardinality: per-subtask
  entry-points:
    functional-spec: architecture-subpipeline.architecture-pair
    detailed-spec: architecture-subpipeline.architecture-pair
`

func TestBuildTaskRoleContextData_ArchRef(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectTestPipelineYAML)

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "arch-task-1",
				Description: "Design feature X",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Architecture document produced",
				EpicRef:     "specs/epics/feature-x.md#capability-auth",
				PlanRef:     "specs/plans/feature-x.md#task-breakdown",
				ArchRef:     "specs/arch-plan/feature-x.md",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	worktree := ".worktrees/arch-task-1"
	state.Tasks[0].Worktree = &worktree

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	config := SupervisorConfig{
		Role:        "architect",
		AgentID:     "architect-1",
		ProjectRoot: tmpDir,
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}

	if data.EpicRef != "specs/epics/feature-x.md" {
		t.Errorf("EpicRef = %q, want repo-relative file ref", data.EpicRef)
	}
	if data.EpicSection != "capability-auth" {
		t.Errorf("EpicSection = %q, want capability-auth", data.EpicSection)
	}
	if data.PlanRef != "specs/plans/feature-x.md" {
		t.Errorf("PlanRef = %q, want repo-relative file ref", data.PlanRef)
	}
	if data.PlanSection != "task-breakdown" {
		t.Errorf("PlanSection = %q, want task-breakdown", data.PlanSection)
	}
	if data.ArchRef != "specs/arch-plan/feature-x.md" {
		t.Errorf("ArchRef = %q, want repo-relative file ref", data.ArchRef)
	}
	if data.IntegrationBranch != "main" {
		t.Errorf("IntegrationBranch = %q, want main", data.IntegrationBranch)
	}
	if strings.Contains(data.ArchRef, tmpDir) || strings.Contains(data.PlanRef, ".worktrees/") {
		t.Errorf("artifact refs should not be worktree-prefixed: plan=%q arch=%q", data.PlanRef, data.ArchRef)
	}
}

func TestBuildTaskRoleContextData_ParentTaskContexts(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectTestPipelineYAML)

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "arch-task-1",
				Description: "Design feature X",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Architecture document produced",
				ParentTasks: []string{"us-1", "us-2"},
				Created:     now,
			},
			{
				ID:          "us-1",
				Description: "User can sign up with email and password",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Iteration:   1,
				DoneWhen:    "Signup flow works end-to-end",
				SpecRef:     "specs/goals/feature-x.md",
				PlanRef:     "specs/plans/signup.md",
				Created:     now,
			},
			{
				ID:          "us-2",
				Description: "User can reset password via email link",
				Status:      models.TaskStatusMerged,
				Priority:    3,
				Iteration:   1,
				DoneWhen:    "Password reset sends email and updates password",
				SpecRef:     "specs/goals/feature-x.md",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	config := SupervisorConfig{
		Role:        "architect",
		AgentID:     "architect-1",
		ProjectRoot: tmpDir,
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData: %v", err)
	}

	// Should have 2 parent task contexts
	if len(data.ParentTaskContexts) != 2 {
		t.Fatalf("ParentTaskContexts length = %d, want 2", len(data.ParentTaskContexts))
	}

	// Verify first parent task context
	ptc0 := data.ParentTaskContexts[0]
	if ptc0.ID != "us-1" {
		t.Errorf("ParentTaskContexts[0].ID = %q, want %q", ptc0.ID, "us-1")
	}
	if ptc0.Description != "User can sign up with email and password" {
		t.Errorf("ParentTaskContexts[0].Description = %q, want %q", ptc0.Description, "User can sign up with email and password")
	}
	if ptc0.DoneWhen != "Signup flow works end-to-end" {
		t.Errorf("ParentTaskContexts[0].DoneWhen = %q, want %q", ptc0.DoneWhen, "Signup flow works end-to-end")
	}
	if ptc0.SpecRef != "specs/goals/feature-x.md" {
		t.Errorf("ParentTaskContexts[0].SpecRef = %q, want %q", ptc0.SpecRef, "specs/goals/feature-x.md")
	}
	if ptc0.PlanRef != "specs/plans/signup.md" {
		t.Errorf("ParentTaskContexts[0].PlanRef = %q, want %q", ptc0.PlanRef, "specs/plans/signup.md")
	}

	// Verify second parent task context
	ptc1 := data.ParentTaskContexts[1]
	if ptc1.ID != "us-2" {
		t.Errorf("ParentTaskContexts[1].ID = %q, want %q", ptc1.ID, "us-2")
	}
	if ptc1.Description != "User can reset password via email link" {
		t.Errorf("ParentTaskContexts[1].Description = %q, want %q", ptc1.Description, "User can reset password via email link")
	}
	if ptc1.DoneWhen != "Password reset sends email and updates password" {
		t.Errorf("ParentTaskContexts[1].DoneWhen = %q, want %q", ptc1.DoneWhen, "Password reset sends email and updates password")
	}
	if ptc1.SpecRef != "specs/goals/feature-x.md" {
		t.Errorf("ParentTaskContexts[1].SpecRef = %q, want %q", ptc1.SpecRef, "specs/goals/feature-x.md")
	}
	if ptc1.PlanRef != "" {
		t.Errorf("ParentTaskContexts[1].PlanRef = %q, want empty", ptc1.PlanRef)
	}

	// Verify ParentTaskContexts is NOT populated for non-architect roles
	coderConfig := SupervisorConfig{
		Role:        "coder",
		AgentID:     "coder-1",
		ProjectRoot: "/project",
	}
	coderData, err := buildTaskRoleContextData(&state.Tasks[0], state, coderConfig, resolver)
	if err != nil {
		t.Fatalf("buildTaskRoleContextData (coder): %v", err)
	}
	if len(coderData.ParentTaskContexts) != 0 {
		t.Errorf("ParentTaskContexts for coder = %d, want 0", len(coderData.ParentTaskContexts))
	}
}

// architectE2EPipelineYAML has full context-sections for architect and
// architecture-reviewer, matching the production pipeline configuration.
var architectE2EPipelineYAML = `pipeline:
  roles:
    architect:
      type: doer
      display-name: "Architect"
      context-sections:
        - assigned-task
        - parent-tasks-context
        - worktree-rules
        - prior-rejection
        - prior-attempt
        - doer-state-transitions
        - architect-tools
        - implementation-phase
        - mandatory-docs
        - skills-affinity
      skills:
        - software-architecture-review
    architecture-reviewer:
      type: reviewer
      display-name: "Architecture Reviewer"
      context-sections:
        - review-task
        - worktree-rules
        - prior-rejection
        - reviewer-state-transitions
        - architecture-reviewer-tools
        - anomaly-logging
        - review-instructions
        - rejection-format
        - verdict-submission
        - mandatory-docs
        - skills-affinity
      skills:
        - systemic-thinking
        - software-architecture-review
    coder:
      type: doer
      display-name: "Coder"
      context-sections:
        - assigned-task
    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      context-sections:
        - review-task
    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      context-sections:
        - orchestrator-dashboard
  role-pairs:
    architecture-pair:
      doer: architect
      reviewer: architecture-reviewer
      states:
        initial: DRAFT_ARCHITECTURE
        executing: ARCHITECTING
        submitted: ARCHITECTURE_TO_REVIEW
        reviewing: REVIEWING_ARCHITECTURE
        approved: ARCHITECTURE_APPROVED
        rejected: ARCHITECTURE_REJECTED
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
  sub-pipelines:
    architecture-subpipeline:
      steps:
        - architecture-pair

    coding-subpipeline:
      steps:
        - coding-pair
  pipeline-transitions:
    - name: architecture-to-coding
      from: architecture-subpipeline.architecture-pair.approved
      to: coding-subpipeline.coding-pair.initial
      trigger: manual
      cardinality: per-subtask
  entry-points:
    functional-spec: architecture-subpipeline.architecture-pair
    detailed-spec: architecture-subpipeline.architecture-pair
`

func TestBuildPromptWithContext_Architect(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectE2EPipelineYAML)

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Build feature X",
			SpecRef:     "specs/goals/feature-x.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "arch-1",
				Description: "Define architecture for feature X",
				Status:      "ARCHITECTING",
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Architecture document produced",
				SpecRef:     "specs/goals/feature-x.md",
				ParentTasks: []string{"us-1", "us-2"},
				Created:     now,
			},
			{
				ID:          "us-1",
				Description: "User can sign up with email",
				Status:      models.TaskStatusMerged,
				Priority:    2,
				Iteration:   1,
				DoneWhen:    "Signup works end-to-end",
				SpecRef:     "specs/goals/feature-x.md",
				PlanRef:     "specs/plans/signup.md",
				Created:     now,
			},
			{
				ID:          "us-2",
				Description: "User can reset password",
				Status:      models.TaskStatusMerged,
				Priority:    3,
				Iteration:   1,
				DoneWhen:    "Password reset sends email",
				SpecRef:     "specs/goals/feature-x.md",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	worktree := ".worktrees/arch-1"
	state.Tasks[0].Worktree = &worktree

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	config := SupervisorConfig{
		Role:        "architect",
		AgentID:     "architect-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	strategy, err := NewRoleStrategy(config.Role, resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy error = %v", err)
	}

	prompt, err := strategy.BuildPrompt(state, config, "arch-1")
	if err != nil {
		t.Fatalf("BuildPrompt error = %v", err)
	}

	// Architect prompt must include parent tasks context, tools, state transitions, and implementation phase
	mustContain := []string{
		"PARENT TASKS (2)",
		"Artifact refs below are repo-relative.",
		"Read " + filepath.Join(tmpDir, worktree) + "/<ref> first",
		"git -C " + filepath.Join(tmpDir, worktree) + " show main:<file-ref>",
		"User can sign up with email",
		"User can reset password",
		"ARCHITECT TOOLS",
		"ARCHITECT STATE TRANSITIONS",
		"ARCHITECTING",
		"IMPLEMENTATION PHASE",
		"specs/arch-plan",
		"specs/goals/feature-x.md",
		"ASSIGNED ARCHITECTURE TASK",
		"Submission requires a new worktree commit for this task",
		"Do NOT submit the pre-change HEAD",
		"Submission proof: `liza submit-for-review` must actually run successfully after step 9g",
		"BOOTSTRAP-PRECOMMIT REQUIREMENTS",
		`Set "kind": "bootstrap-precommit"`,
		`"kind": "<optional typed marker — see BOOTSTRAP-PRECOMMIT REQUIREMENTS in IMPLEMENTATION PHASE>"`,
		"This task may provision pre-commit only via a project-scoped mechanism",
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt should contain %q", s)
		}
	}

	// Must not contain other role sections
	mustNotContain := []string{
		"CODER TOOLS",
		"CODER STATE TRANSITIONS",
		"CODE PLANNER TOOLS",
		"lines 41-44",
	}
	for _, s := range mustNotContain {
		if strings.Contains(prompt, s) {
			t.Errorf("prompt should NOT contain %q", s)
		}
	}
}

func TestBuildPromptWithContext_ArchitectureReviewer(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectE2EPipelineYAML)

	baseCommit := "abc123"
	reviewCommit := "def456"
	assignedTo := "architect-1"

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Build feature X",
			SpecRef:     "specs/goals/feature-x.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:           "arch-1",
				Description:  "Define architecture for feature X",
				Status:       "ARCHITECTURE_TO_REVIEW",
				Priority:     1,
				Iteration:    1,
				DoneWhen:     "Architecture document produced",
				SpecRef:      "specs/goals/feature-x.md",
				BaseCommit:   &baseCommit,
				ReviewCommit: &reviewCommit,
				AssignedTo:   &assignedTo,
				Created:      now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	worktree := ".worktrees/arch-1"
	state.Tasks[0].Worktree = &worktree

	tmpDir := t.TempDir()
	config := SupervisorConfig{
		Role:        "architecture-reviewer",
		AgentID:     "architecture-reviewer-1",
		ProjectRoot: tmpDir,
		SpecsDir:    filepath.Join(tmpDir, "specs"),
		StatePath:   filepath.Join(tmpDir, "state.yaml"),
	}

	strategy, err := NewRoleStrategy(config.Role, resolver)
	if err != nil {
		t.Fatalf("NewRoleStrategy error = %v", err)
	}

	prompt, err := strategy.BuildPrompt(state, config, "arch-1")
	if err != nil {
		t.Fatalf("BuildPrompt error = %v", err)
	}

	// Architecture reviewer prompt must include review checklist with structural gates and state transitions
	mustContain := []string{
		"ARCHITECTURE REVIEWER STATE TRANSITIONS",
		"REVIEWING_ARCHITECTURE",
		"ARCHITECTURE_APPROVED",
		"ARCHITECTURE_REJECTED",
		"ARCHITECTURE REVIEWER TOOLS",
		"REVIEW CHECKLIST",
		"Decomposition completeness",
		"Composability",
		"systemic-thinking",
		"ASSIGNED ARCHITECTURE REVIEW TASK",
	}
	for _, s := range mustContain {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt should contain %q", s)
		}
	}

	// Must not contain doer sections
	mustNotContain := []string{
		"IMPLEMENTATION PHASE",
		"ARCHITECT TOOLS",
		"ARCHITECT STATE TRANSITIONS",
		"CODER TOOLS",
		"BOOTSTRAP-PRECOMMIT REQUIREMENTS",
	}
	for _, s := range mustNotContain {
		if strings.Contains(prompt, s) {
			t.Errorf("prompt should NOT contain %q", s)
		}
	}
}

// TestBuildTaskRoleContextData_PreCommitFields_Architect asserts that for
// the architect role, all three PreCommit* fields are populated from the
// precommit helpers (config presence via git plumbing on the integration
// branch; in-flight detection via Kind-marker repo-wide scan; the
// canonical marker string).
func TestBuildTaskRoleContextData_PreCommitFields_Architect(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectTestPipelineYAML)

	makeBaseState := func() *models.State {
		return &models.State{
			Version: 1,
			Goal: models.Goal{
				ID:          "goal-1",
				Description: "Test goal",
				SpecRef:     "spec.md",
				Status:      models.GoalStatusInProgress,
				Created:     now,
			},
			Tasks: []models.Task{
				{
					ID:          "arch-task-1",
					Description: "Design feature X",
					Status:      models.TaskStatusImplementing,
					Priority:    1,
					Iteration:   1,
					DoneWhen:    "Architecture document produced",
					Created:     now,
				},
			},
			Agents: make(map[string]models.Agent),
			Config: models.Config{IntegrationBranch: "main"},
		}
	}

	t.Run("bootstrap-in-flight-config-absent", func(t *testing.T) {
		state := makeBaseState()
		state.Tasks = append(state.Tasks, models.Task{
			ID:       "bootstrap-1",
			Kind:     "bootstrap-precommit",
			Status:   models.TaskStatusImplementing,
			Priority: 1,
			Created:  now,
		})

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		config := SupervisorConfig{
			Role:        "architect",
			AgentID:     "architect-1",
			ProjectRoot: tmpDir,
		}

		data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
		if err != nil {
			t.Fatalf("buildTaskRoleContextData: %v", err)
		}
		if data.PreCommitConfigExists {
			t.Errorf("PreCommitConfigExists = true, want false (no config committed)")
		}
		if !data.PreCommitBootstrapInFlight {
			t.Errorf("PreCommitBootstrapInFlight = false, want true (one non-terminal bootstrap task planted)")
		}
		if data.PreCommitKind != "bootstrap-precommit" {
			t.Errorf("PreCommitKind = %q, want %q", data.PreCommitKind, "bootstrap-precommit")
		}
	})

	t.Run("config-present-no-bootstrap-in-flight", func(t *testing.T) {
		state := makeBaseState()

		tmpDir := t.TempDir()
		testhelpers.SetupTestGitRepo(t, tmpDir)
		cfg := filepath.Join(tmpDir, ".pre-commit-config.yaml")
		if err := os.WriteFile(cfg, []byte("repos: []\n"), 0644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		testhelpers.MustGit(t, tmpDir, "add", ".pre-commit-config.yaml")
		testhelpers.MustGit(t, tmpDir, "commit", "-m", "add precommit config")

		config := SupervisorConfig{
			Role:        "architect",
			AgentID:     "architect-1",
			ProjectRoot: tmpDir,
		}

		data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
		if err != nil {
			t.Fatalf("buildTaskRoleContextData: %v", err)
		}
		if !data.PreCommitConfigExists {
			t.Errorf("PreCommitConfigExists = false, want true (config committed on main)")
		}
		if data.PreCommitBootstrapInFlight {
			t.Errorf("PreCommitBootstrapInFlight = true, want false (no bootstrap task)")
		}
		if data.PreCommitKind != "bootstrap-precommit" {
			t.Errorf("PreCommitKind = %q, want %q", data.PreCommitKind, "bootstrap-precommit")
		}
	})
}

// TestBuildTaskRoleContextData_PreCommitFields_NonArchitect asserts that
// for non-architect roles, the architect-gated block is skipped: all
// three PreCommit* fields remain at zero and the precommit helper is
// NOT invoked. The gating proof uses a ProjectRoot pointing at a path
// without a .git directory — if the helper ran, git rev-parse would
// fail and buildTaskRoleContextData would return a non-nil error.
func TestBuildTaskRoleContextData_PreCommitFields_NonArchitect(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectTestPipelineYAML)

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "task-1",
				Description: "Test task",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Done",
				Created:     now,
			},
			{
				ID:       "bootstrap-1",
				Kind:     "bootstrap-precommit",
				Status:   models.TaskStatusImplementing,
				Priority: 1,
				Created:  now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "main"},
	}

	// A path without .git — if the helper were called, rev-parse would fail.
	nonGitDir := t.TempDir()

	for _, role := range []string{"coder", "code-reviewer"} {
		t.Run(role, func(t *testing.T) {
			config := SupervisorConfig{
				Role:        role,
				AgentID:     role + "-1",
				ProjectRoot: nonGitDir,
			}

			data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
			if err != nil {
				t.Fatalf("buildTaskRoleContextData: %v (unexpected — helper must not run for non-architect)", err)
			}
			if data.PreCommitConfigExists {
				t.Errorf("PreCommitConfigExists = true, want false for role %q", role)
			}
			if data.PreCommitBootstrapInFlight {
				t.Errorf("PreCommitBootstrapInFlight = true, want false for role %q", role)
			}
			if data.PreCommitKind != "" {
				t.Errorf("PreCommitKind = %q, want empty for role %q", data.PreCommitKind, role)
			}
		})
	}
}

// TestBuildTaskRoleContextData_PreCommitFields_HelperError asserts that
// a helper failure (e.g. invalid integration branch) is surfaced through
// buildTaskRoleContextData with the outer "precommit config check" wrap
// while preserving the inner ErrContextBuild sentinel.
func TestBuildTaskRoleContextData_PreCommitFields_HelperError(t *testing.T) {
	now := time.Now().UTC()
	resolver := loadTestResolver(t, architectTestPipelineYAML)

	state := &models.State{
		Version: 1,
		Goal: models.Goal{
			ID:          "goal-1",
			Description: "Test goal",
			SpecRef:     "spec.md",
			Status:      models.GoalStatusInProgress,
			Created:     now,
		},
		Tasks: []models.Task{
			{
				ID:          "arch-task-1",
				Description: "Design feature X",
				Status:      models.TaskStatusImplementing,
				Priority:    1,
				Iteration:   1,
				DoneWhen:    "Architecture document produced",
				Created:     now,
			},
		},
		Agents: make(map[string]models.Agent),
		Config: models.Config{IntegrationBranch: "does-not-exist"},
	}

	tmpDir := t.TempDir()
	testhelpers.SetupTestGitRepo(t, tmpDir)
	config := SupervisorConfig{
		Role:        "architect",
		AgentID:     "architect-1",
		ProjectRoot: tmpDir,
	}

	data, err := buildTaskRoleContextData(&state.Tasks[0], state, config, resolver)
	if err == nil {
		t.Fatalf("expected error, got nil (data=%+v)", data)
	}
	if data != nil {
		t.Errorf("data = %+v, want nil on error", data)
	}
	if !stderrors.Is(err, precommit.ErrContextBuild) {
		t.Errorf("errors.Is(err, precommit.ErrContextBuild) = false; err=%v", err)
	}
	msg := err.Error()
	if !strings.HasPrefix(msg, "precommit config check") {
		t.Errorf("error message %q does not start with %q", msg, "precommit config check")
	}
	if !strings.Contains(msg, "integration branch") {
		t.Errorf("error message %q missing inner %q phrase", msg, "integration branch")
	}
}

func writePromptTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

// Ensure pipeline import is used (linter guard).
var _ = pipeline.NewResolver
