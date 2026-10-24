package a8

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"gopkg.in/yaml.v3"
)

// Keep every production/module dependency here. This untagged adapter also
// compiles at the starting commit; its first future call-site edit must retain
// this content verbatim in an immutable oraclestart twin (SI-3 H1).
func renderPrompt(project, template string, f fixture) (string, error) {
	runtimeDir := paths.New(project).LizaDir()
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, "pipeline.yaml"), embedded.PipelineConfigContent(), 0o644); err != nil {
		return "", err
	}
	cfg, err := pipeline.LoadFrozen(project)
	if err != nil {
		return "", err
	}
	resolver := pipeline.NewResolver(cfg)
	var state models.State
	if err := yaml.Unmarshal([]byte(template), &state); err != nil {
		return "", err
	}
	if f.TaskID != "" {
		task := state.FindTask(f.TaskID)
		if task == nil || task.RolePair != f.Pair {
			return "", fmt.Errorf("manifest task/pair missing from state")
		}
		roleType, err := resolver.RoleType(f.Role)
		if err != nil {
			return "", err
		}
		wantStatus, err := resolver.ExecutingStatus(f.Pair)
		if roleType == "reviewer" {
			wantStatus, err = resolver.ReviewingStatus(f.Pair)
		}
		if err != nil || task.Status != wantStatus {
			return "", fmt.Errorf("fixture task is not in its role's executing/reviewing state")
		}
	}
	statePath := filepath.Join(runtimeDir, "state.yaml")
	blackboard := db.For(statePath)
	if err := blackboard.Write(&state); err != nil {
		return "", fmt.Errorf("blackboard write: %w", err)
	}
	snapshot, err := blackboard.ReadSnapshot()
	if err != nil {
		return "", fmt.Errorf("blackboard read: %w", err)
	}
	strategy, err := agent.NewRoleStrategy(f.Role, resolver)
	if err != nil {
		return "", err
	}
	return strategy.BuildPrompt(snapshot, agent.SupervisorConfig{
		Role: f.Role, AgentID: f.AgentID, ProjectRoot: project,
		SpecsDir: filepath.Join(project, "specs"), StatePath: statePath,
		CLIName: "claude", InitialTask: f.InitialTask,
	}, f.TaskID)
}
