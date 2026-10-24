// Package embedded provides embedded resource files (contracts, skills, settings)
// used during workspace initialization.
package embedded

import (
	"embed"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// Build-time variables (set via -ldflags during build)
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

//go:embed contracts/*.md
var contractsFS embed.FS

//go:embed skills
var skillsFS embed.FS

//go:embed support-docs/*.md
var supportDocsFS embed.FS

//go:embed "claude-settings.json"
var claudeSettingsContent []byte

//go:embed "codex-config.toml"
var codexConfigContent []byte

//go:embed "codex-hooks.json"
var codexHooksContent []byte

//go:embed "copilot-hooks.json"
var copilotHooksContent []byte

//go:embed "bash-policy.yaml"
var bashPolicyContent []byte

//go:embed "opencode-tools/exec.ts"
var opencodeExecToolContent []byte

//go:embed "pi-extensions/init-gate.ts"
var piInitGateContent []byte

//go:embed "hooks/enforce-init.sh"
var enforceInitHookContent []byte

//go:embed "hooks/session-context.sh"
var sessionContextHookContent []byte

//go:embed "hooks/git-guard.sh"
var gitGuardHookContent []byte

//go:embed "hooks/rtk-guard.sh"
var rtkGuardHookContent []byte

//go:embed "hooks/worktree-path-guard.sh"
var worktreePathGuardHookContent []byte

// Git-level pre-commit hook for task worktrees. Deliberately NOT in hooks/
// — that directory holds Claude Code PreToolUse hooks that get written to
// .claude/hooks/ and referenced from claude-settings.json. This one is a
// git-native pre-commit hook rendered per-worktree by
// RenderWorktreePreCommitHook and installed into the branded worktree hook directory
// (see ops.InstallWorktreePreCommitHook). Different transport, different
// lifecycle, different directory.
//
//go:embed "git-hooks/worktree-pre-commit.sh"
var worktreePreCommitHookContent []byte

//go:embed "guardrails-template.md"
var guardrailsTemplateContent []byte

//go:embed "claudeignore"
var claudeIgnoreContent []byte

//go:embed "pipeline.yaml"
var pipelineConfigContent []byte

const openCodeExecToolManagedHeaderTemplate = "// __BRAND_NAME_UPPER__ MANAGED FILE: OpenCode exec compatibility tool. Safe for __BRAND_NAME_TITLE__ to overwrite."

const piInitGateManagedHeaderTemplate = "// __BRAND_NAME_UPPER__ MANAGED FILE: pi init gate. Safe for __BRAND_NAME_TITLE__ to overwrite."

// OpenCodeExecToolManagedHeader identifies the project-local OpenCode exec
// compatibility tool as owned by the current brand.
func OpenCodeExecToolManagedHeader() string {
	return string(renderEmbeddedAsset([]byte(openCodeExecToolManagedHeaderTemplate)))
}

// OpenCodeExecToolContent returns the embedded OpenCode exec compatibility tool.
func OpenCodeExecToolContent() []byte {
	return renderEmbeddedAsset(opencodeExecToolContent)
}

// PiInitGateManagedHeader identifies the branded global pi init-gate extension
// as owned by the current brand.
func PiInitGateManagedHeader() string {
	return string(renderEmbeddedAsset([]byte(piInitGateManagedHeaderTemplate)))
}

// PiInitGateContent returns the embedded pi init-gate extension.
func PiInitGateContent() []byte {
	return renderEmbeddedAsset(piInitGateContent)
}

// PipelineConfigContent returns the raw embedded pipeline.yaml content.
// Used by init to auto-freeze when --config is not provided.
func PipelineConfigContent() []byte {
	return pipelineConfigContent
}

func renderEmbeddedAsset(content []byte) []byte {
	values := brand.RuntimeValues()
	replacer := strings.NewReplacer(
		"__BRAND_NAME_LOWER__", values.NameLower,
		"__BRAND_NAME_UPPER__", values.NameUpper,
		"__BRAND_NAME_TITLE__", values.NameTitle,
		"__BRAND_BINARY_NAME__", values.BinaryName,
		"__BRAND_GLOBAL_DIRNAME__", values.GlobalDirName,
		"__BRAND_PROJECT_DIRNAME__", values.ProjectDirName,
		"__BRAND_ENV_PREFIX__", values.EnvPrefix,
	)
	return []byte(replacer.Replace(string(content)))
}
