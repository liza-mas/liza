package commands

import (
	"fmt"
	"os"

	bashpolicycli "github.com/liza-mas/liza/internal/bash-policy-cli"
	"github.com/liza-mas/liza/internal/embedded"
	"github.com/liza-mas/liza/internal/providers"
)

// activateContracts activates every explicitly selected contract before deploying
// init hooks or creating the runtime workspace. Failure leaves previous metadata
// intact.
func (m *masInit) activateContracts() error {
	var contractProviders []providers.Provider
	for _, provider := range m.selectedProviders {
		if provider.Setup.Contract.RepoFile != "" {
			contractProviders = append(contractProviders, provider)
		}
	}
	if len(contractProviders) == 0 {
		return nil
	}
	if err := activateProviderContracts(m.lizaPaths.ProjectRoot(), m.globalCoreFile, contractProviders, m.catalog, contractSymlinkOptions{
		ProviderActions: m.params.ContractActions,
	}); err != nil {
		return fmt.Errorf("activate provider contracts: %w", err)
	}
	return nil
}

// hasCursorHooks and hasCodexConfig report which provider-specific hook sets the
// selected providers need.
func (m *masInit) hasCursorHooks() bool {
	return providerHas(m.selectedProviders, providerNeedsCursorHooks)
}

func (m *masInit) hasCodexConfig() bool {
	return providerHasAsset(m.selectedProviders, func(a providers.ActivationAssets) bool {
		return a.CodexConfig || a.CodexHooks
	})
}

// installProviderAssets writes the provider settings, hooks and project
// templates. Every failure is a warning: init goes on without the asset.
func (m *masInit) installProviderAssets() {
	root := m.lizaPaths.ProjectRoot()

	// Write/merge Claude Code settings and deploy hooks to .claude/
	// This is non-fatal - if it fails, just warn
	// Note: This may prompt user for input if settings file exists
	if err := embedded.WriteClaudeSettingsWithOptions(root, m.stdin, m.confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write claude-settings.json: %v\n", err)
	}

	m.installCodexConfig()
	m.installBashPolicy()
	m.installProviderExtras()

	// Remove the stale MCP server entry from .mcp.json (written by older versions)
	if err := embedded.CleanStaleMCPEntry(root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to clean stale .mcp.json entry: %v\n", err)
	}

	// Write GUARDRAILS.md template to project root (non-fatal, like claude-settings)
	if err := embedded.WriteGuardrails(root); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write GUARDRAILS.md: %v\n", err)
	}

	// Write .claudeignore template (non-fatal, prompts if exists)
	if err := embedded.WriteClaudeIgnoreWithOptions(root, m.stdin, m.confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write .claudeignore: %v\n", err)
	}
}

func (m *masInit) installCodexConfig() {
	if !m.hasCodexConfig() {
		return
	}
	root := m.lizaPaths.ProjectRoot()
	if err := embedded.WriteCodexProjectPermissionsWithOptions(root, m.stdin, m.confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write codex config: %v\n", err)
	}
	if err := embedded.WriteCodexProjectHooksWithOptions(root, m.stdin, m.confirmOptions); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write codex hooks: %v\n", err)
	}
}

func (m *masInit) installBashPolicy() {
	root := m.lizaPaths.ProjectRoot()
	providerNames := bashPolicyProviders(true, m.hasCodexConfig(), m.hasCursorHooks())
	if root != "" && len(providerNames) > 0 && bashpolicycli.RuntimeEnabled() {
		if err := embedded.WriteBashPolicyConfigWithOptions(root, m.stdin, m.confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write .bash-policy.yaml: %v\n", err)
		}
	}

	bashPolicyStdin := bashPolicySubprocessStdin(m.rawStdin, m.stdin)
	runBashPolicyInits(root, providerNames, bashPolicyStdin, m.params.AutoConfirm)
}

func (m *masInit) installProviderExtras() {
	root := m.lizaPaths.ProjectRoot()
	if providerHasAsset(m.selectedProviders, func(a providers.ActivationAssets) bool { return a.OpenCodeExecTool }) {
		if err := embedded.WriteOpenCodeExecTool(root); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write opencode exec tool: %v\n", err)
		}
	}

	if providerHasAsset(m.selectedProviders, func(a providers.ActivationAssets) bool { return a.CopilotHooks }) {
		if err := embedded.WriteCopilotProjectHooksWithOptions(root, m.stdin, m.confirmOptions); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write copilot hooks: %v\n", err)
		}
	}

	if providerHasAsset(m.selectedProviders, func(a providers.ActivationAssets) bool { return a.PiExtension }) {
		if err := embedded.WritePiInitGate(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write pi init gate: %v\n", err)
		}
	}
}
