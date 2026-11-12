package embedded

import (
	"maps"
	"path/filepath"
	"slices"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/paths"
)

// The paths below are where the project asset writers install their files.
// The writers and doctor both read them, so a checked path is always the one
// a writer produces.

// claudeHookContents are the hook scripts WriteHooks installs under .claude/hooks/.
var claudeHookContents = map[string][]byte{
	"enforce-init.sh":        enforceInitHookContent,
	"session-context.sh":     sessionContextHookContent,
	"git-guard.sh":           gitGuardHookContent,
	"rtk-guard.sh":           rtkGuardHookContent,
	"worktree-path-guard.sh": worktreePathGuardHookContent,
}

func claudeHooksDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".claude", "hooks")
}

func codexDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".codex")
}

func codexHooksDir(projectRoot string) string {
	return filepath.Join(codexDir(projectRoot), "hooks")
}

func copilotHooksDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".copilot", "hooks")
}

func copilotHooksConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".github", "hooks", CopilotHooksFileName())
}

// openCodeExecToolRel and openCodeResultPluginRel are relative to the project root.
func openCodeExecToolRel() string {
	return filepath.Join(".opencode", "tools", "exec.ts")
}

func openCodeResultPluginRel() string {
	return filepath.Join(".opencode", "plugins", brand.BinaryName+"-tool-result-budget.ts")
}

// ClaudeAssetPaths returns the files WriteClaudeSettings installs: the
// project settings and the hook scripts they reference.
func ClaudeAssetPaths(projectRoot string) []string {
	return append([]string{paths.New(projectRoot).ClaudeSettingsPath()}, scriptPaths(claudeHooksDir(projectRoot), claudeHookContents)...)
}

// CodexProjectAssetPaths returns the files WriteCodexProjectHooks installs:
// the project config carrying the hooks feature, hooks.json and the hook scripts.
func CodexProjectAssetPaths(projectRoot string) []string {
	return append([]string{
		filepath.Join(codexDir(projectRoot), "config.toml"),
		filepath.Join(codexDir(projectRoot), "hooks.json"),
	}, scriptPaths(codexHooksDir(projectRoot), codexHookContents)...)
}

// CopilotProjectAssetPaths returns the files WriteCopilotProjectHooks
// installs: the hook config and the hook scripts.
func CopilotProjectAssetPaths(projectRoot string) []string {
	return append([]string{copilotHooksConfigPath(projectRoot)}, scriptPaths(copilotHooksDir(projectRoot), copilotHookContents)...)
}

// OpenCodeAssetPaths returns the files WriteOpenCodeExecTool installs: the
// exec tool and the result-budget plugin.
func OpenCodeAssetPaths(projectRoot string) []string {
	return []string{
		filepath.Join(projectRoot, openCodeExecToolRel()),
		filepath.Join(projectRoot, openCodeResultPluginRel()),
	}
}

func scriptPaths(dir string, contents map[string][]byte) []string {
	names := slices.Sorted(maps.Keys(contents))
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, filepath.Join(dir, name))
	}
	return out
}
