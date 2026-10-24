package embedded

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

const supportDocEmbeddedPath = "support-docs/SUPPORT.md"

// PiInitGatePath returns the branded global path of the pi init-gate
// extension. It lives in the global directory (not per-project) so the pi
// provider's run_args can reference it for every workspace, and so pi never
// fails to load a missing project-local extension file.
func PiInitGatePath() (string, error) {
	home, err := paths.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home directory for pi init gate: %w", err)
	}
	return filepath.Join(home, brand.RuntimeValues().GlobalDirName, "extensions", "init-gate.ts"), nil
}

// WritePiInitGate writes the pi init-gate extension into the branded global
// extensions directory. Existing files are overwritten only when they carry
// the managed header, mirroring the OpenCode exec tool policy.
func WritePiInitGate() error {
	gatePath, err := PiInitGatePath()
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(gatePath); err == nil {
		if !bytes.HasPrefix(existing, []byte(PiInitGateManagedHeader())) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read pi init gate: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(gatePath), 0755); err != nil {
		return fmt.Errorf("failed to create pi extensions directory: %w", err)
	}
	if err := os.WriteFile(gatePath, PiInitGateContent(), 0644); err != nil {
		return fmt.Errorf("failed to write pi init gate: %w", err)
	}
	return nil
}

// WritePipelineConfig writes the embedded pipeline.yaml to the target directory.
// If the file doesn't exist, writes it unconditionally.
// If the file exists and stdin is nil, skips silently (test callers).
// If the file exists and stdin is non-nil, prompts the user to overwrite.
func WritePipelineConfig(targetDir string, stdin *bufio.Reader) error {
	return WritePipelineConfigWithOptions(targetDir, stdin, ConfirmOptions{})
}

func WritePipelineConfigWithOptions(targetDir string, stdin *bufio.Reader, opts ConfirmOptions) error {
	pipelinePath := filepath.Join(targetDir, "pipeline.yaml")
	if _, err := os.Stat(pipelinePath); err == nil {
		if stdin == nil {
			return nil
		}
		ok, err := confirmMergeWithOptions("pipeline.yaml already exists. Overwrite with embedded version? (y/n): ", stdin, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		// Back up existing file before overwriting
		if err := BackupFile(pipelinePath); err != nil {
			return fmt.Errorf("failed to backup pipeline.yaml: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking pipeline.yaml: %w", err)
	}
	if err := os.WriteFile(pipelinePath, pipelineConfigContent, 0644); err != nil {
		return fmt.Errorf("failed to write pipeline.yaml: %w", err)
	}
	return nil
}

// WriteGuardrails writes the embedded guardrails template to GUARDRAILS.md
// in the project root. Only writes if the file doesn't already exist.
// Non-fatal: returns nil if the file already exists.
func WriteGuardrails(projectRoot string) error {
	guardrailsPath := filepath.Join(projectRoot, "GUARDRAILS.md")
	if _, err := os.Stat(guardrailsPath); err == nil {
		// File already exists, don't overwrite
		return nil
	}
	if err := os.WriteFile(guardrailsPath, renderEmbeddedAsset(guardrailsTemplateContent), 0644); err != nil {
		return fmt.Errorf("failed to write GUARDRAILS.md: %w", err)
	}
	return nil
}

// WriteClaudeIgnore writes the embedded .claudeignore template to the project root.
// If the file already exists, prompts the user to overwrite.
// reader may be nil (non-interactive/test callers) — skips silently if file exists.
func WriteClaudeIgnore(projectRoot string, reader *bufio.Reader) error {
	return WriteClaudeIgnoreWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteClaudeIgnoreWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	ignorePath := filepath.Join(projectRoot, ".claudeignore")
	if _, err := os.Stat(ignorePath); err == nil {
		if reader == nil {
			return nil
		}
		ok, err := confirmMergeWithOptions(fmt.Sprintf(".claudeignore already exists. Overwrite with %s template? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	}
	if err := os.WriteFile(ignorePath, renderEmbeddedAsset(claudeIgnoreContent), 0644); err != nil {
		return fmt.Errorf("failed to write .claudeignore: %w", err)
	}
	return nil
}

// WriteBashPolicyConfig writes the embedded bash-policy.yaml to .bash-policy.yaml
// in the project root. Existing files are overwritten only after confirmation.
func WriteBashPolicyConfig(projectRoot string, reader *bufio.Reader) error {
	return WriteBashPolicyConfigWithOptions(projectRoot, reader, ConfirmOptions{})
}

func WriteBashPolicyConfigWithOptions(projectRoot string, reader *bufio.Reader, opts ConfirmOptions) error {
	if reader == nil {
		reader = bufio.NewReader(os.Stdin)
	}
	policyPath := filepath.Join(projectRoot, ".bash-policy.yaml")
	if _, err := os.Stat(policyPath); err == nil {
		ok, err := confirmMergeWithOptions(fmt.Sprintf(".bash-policy.yaml already exists. Overwrite with %s template? (y/n): ", brand.NameTitle), reader, opts)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking .bash-policy.yaml: %w", err)
	}
	if err := os.WriteFile(policyPath, bashPolicyContent, 0644); err != nil {
		return fmt.Errorf("failed to write .bash-policy.yaml: %w", err)
	}
	return nil
}

// WriteSupportDoc writes the canonical SUPPORT.md body to the runtime
// directory. Always overwrites — content tracks the binary version.
func WriteSupportDoc(lizaDir string) error {
	content, err := supportDocsFS.ReadFile(supportDocEmbeddedPath)
	if err != nil {
		return fmt.Errorf("failed to read embedded SUPPORT.md: %w", err)
	}
	supportPath := filepath.Join(lizaDir, "SUPPORT.md")
	if err := os.WriteFile(supportPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write SUPPORT.md: %w", err)
	}
	return nil
}

// RenderWorktreePreCommitHook returns the rendered pre-commit hook script for a
// task worktree with the liza binary path and task ID baked in. Callers write
// the result to the worktree's hooks directory (chmod 0755) and configure
// git's core.hooksPath to point at it.
func RenderWorktreePreCommitHook(lizaBin, taskID string) []byte {
	out := bytes.ReplaceAll(worktreePreCommitHookContent, []byte("__LIZA_BIN__"), []byte(lizaBin))
	out = bytes.ReplaceAll(out, []byte("__TASK_ID__"), []byte(taskID))
	return renderEmbeddedAsset(out)
}

// WriteHooks writes embedded hook scripts to .claude/hooks/ in the project root.
// Always overwrites — hooks are Liza infrastructure, not user-customizable.
func WriteHooks(projectRoot string) error {
	hooksDir := filepath.Join(projectRoot, ".claude", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create .claude/hooks directory: %w", err)
	}

	for name, content := range map[string][]byte{
		"enforce-init.sh":        enforceInitHookContent,
		"session-context.sh":     sessionContextHookContent,
		"git-guard.sh":           gitGuardHookContent,
		"rtk-guard.sh":           rtkGuardHookContent,
		"worktree-path-guard.sh": worktreePathGuardHookContent,
	} {
		hookPath := filepath.Join(hooksDir, name)
		if err := os.WriteFile(hookPath, renderEmbeddedAsset(content), 0755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// WriteOpenCodeExecTool writes Liza's project-local OpenCode exec compatibility
// tool to .opencode/tools/exec.ts. Existing user-owned files are preserved; only
// Liza-managed copies are overwritten.
func WriteOpenCodeExecTool(projectRoot string) error {
	if err := WriteOpenCodeResultPlugin(projectRoot); err != nil {
		return err
	}
	toolPath := filepath.Join(projectRoot, ".opencode", "tools", "exec.ts")
	existing, err := os.ReadFile(toolPath)
	if err == nil {
		if !bytes.HasPrefix(existing, []byte(OpenCodeExecToolManagedHeader())) {
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to read opencode exec tool: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(toolPath), 0755); err != nil {
		return fmt.Errorf("failed to create .opencode/tools directory: %w", err)
	}
	if err := os.WriteFile(toolPath, renderEmbeddedAsset(opencodeExecToolContent), 0644); err != nil {
		return fmt.Errorf("failed to write opencode exec tool: %w", err)
	}
	return nil
}
