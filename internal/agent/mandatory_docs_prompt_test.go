package agent

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/testhelpers"
)

func TestMandatoryDocsCompletePrompts(t *testing.T) {
	for _, role := range []string{"coder", "code-reviewer", "orchestrator"} {
		for _, populated := range []bool{false, true} {
			name := role + "/empty"
			if populated {
				name = role + "/populated"
			}
			t.Run(name, func(t *testing.T) {
				projectRoot := t.TempDir()
				testhelpers.SetupPipelineConfig(t, projectRoot)
				cfg, err := pipeline.LoadEmbeddedReference()
				if err != nil {
					t.Fatal(err)
				}
				roleDef := cfg.Pipeline.Roles[role]
				// A configured list remains mandatory even without a context-section pointer.
				roleDef.ContextSections = []string{"skills-affinity"}
				roleDef.MandatoryDocs = nil
				relativeDoc := filepath.Join("specs", "quality-protocol.md")
				absoluteDoc := filepath.Join(t.TempDir(), "external-protocol.md")
				if populated {
					roleDef.MandatoryDocs = []string{relativeDoc, absoluteDoc}
				}
				cfg.Pipeline.Roles[role] = roleDef
				resolver := pipeline.NewResolver(cfg)
				worktree := filepath.Join(".worktrees", "task-1")
				state := &models.State{
					Goal:   models.Goal{Description: "Test goal", SpecRef: "specs/goal.md"},
					Tasks:  []models.Task{{ID: "task-1", Description: "Test task", Status: models.TaskStatusImplementing, DoneWhen: "Task complete", Worktree: &worktree}},
					Config: models.Config{IntegrationBranch: "main"},
				}
				config := SupervisorConfig{Role: role, AgentID: role + "-1", ProjectRoot: projectRoot, SpecsDir: filepath.Join(projectRoot, "specs"), StatePath: filepath.Join(projectRoot, paths.ProjectDirName(), "state.yaml")}
				var prompt string
				root := projectRoot
				if role == "orchestrator" {
					prompt, err = buildOrchestratorPromptContext(state, config, resolver)
				} else {
					root = filepath.Join(projectRoot, worktree)
					prompt, err = testBuildPromptWithContext(t, state, config, "task-1", resolver)
				}
				if err != nil {
					t.Fatal(err)
				}
				readStep := strings.Index(prompt, "Read every file listed under MANDATORY DOCUMENTS")
				executeStep := strings.Index(prompt, "Execute your role's protocol")
				if !populated {
					if readStep >= 0 || strings.Contains(prompt, "MANDATORY DOCUMENTS") {
						t.Fatalf("empty list introduced mandatory-document text: %s", prompt)
					}
					if !strings.Contains(prompt, "4. Execute your role's protocol") {
						t.Fatal("empty list changed the default startup step")
					}
					return
				}
				if readStep < 0 || executeStep <= readStep {
					t.Errorf("startup must require full reads before role execution: %s", prompt)
				}
				if strings.Count(prompt, "=== MANDATORY DOCUMENTS ===") != 1 {
					t.Errorf("configured documents must render exactly once: %s", prompt)
				}
				docBlock := strings.Index(prompt, "=== MANDATORY DOCUMENTS ===")
				if docBlock < 0 || docBlock >= strings.Index(prompt, "=== SKILLS AFFINITY ===") {
					t.Errorf("document block must precede role instructions: %s", prompt)
				}
				for _, want := range []string{"- " + filepath.Join(root, relativeDoc), "- " + absoluteDoc, "cannot be read, stop"} {
					if !strings.Contains(prompt, want) {
						t.Errorf("missing mandatory-document guidance %q", want)
					}
				}
			})
		}
	}
}
