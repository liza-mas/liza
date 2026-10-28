//go:build linux

package artifact

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/paths"
)

func writeProject(project string) error {
	runtimeDir := paths.New(project).LizaDir()
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return err
	}
	options := embedded.ConfirmOptions{AutoConfirm: true}
	reader := bufio.NewReader(strings.NewReader(""))
	writers := []struct {
		name string
		run  func() error
	}{
		{"support doc", func() error { return embedded.WriteSupportDoc(runtimeDir) }},
		{"Claude settings and hooks", func() error { return embedded.WriteClaudeSettingsWithOptions(project, reader, options) }},
		{"Codex permissions", func() error { return embedded.WriteCodexProjectPermissionsWithOptions(project, reader, options) }},
		{"Codex project hooks", func() error { return embedded.WriteCodexProjectHooksWithOptions(project, reader, options) }},
		{"bash policy", func() error { return embedded.WriteBashPolicyConfigWithOptions(project, reader, options) }},
		{"OpenCode tool and plugin", func() error { return embedded.WriteOpenCodeExecTool(project) }},
		{"Copilot hooks", func() error { return embedded.WriteCopilotProjectHooksWithOptions(project, reader, options) }},
		{"guardrails", func() error { return embedded.WriteGuardrails(project) }},
		{"claudeignore", func() error { return embedded.WriteClaudeIgnoreWithOptions(project, reader, options) }},
		{"pipeline", func() error { return embedded.WritePipelineConfigWithOptions(runtimeDir, reader, options) }},
	}
	for _, writer := range writers {
		if err := writer.run(); err != nil {
			// Identify the surface without leaking environment-derived error text.
			return fmt.Errorf("write %s failed", writer.name)
		}
	}
	return nil
}

func writePiGate() error { return embedded.WritePiInitGate() }

func worktreePreCommit() []byte {
	return embedded.RenderWorktreePreCommitHook("/fixture/bin/agent-cli", "fixture-task-1")
}
