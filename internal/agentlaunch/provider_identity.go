package agentlaunch

import (
	"maps"
	"strings"

	"github.com/liza-mas/liza/internal/models"
)

// ProviderKeys maps recorded tool names to the effective provider key policies
// compare: the catalog's provider_key, overridden by the project's agent_tools
// entry, falling back to the tool name. The tool name keeps addressing
// configuration, review slots, launch and display; the key only decides
// whether two tools are the same provider. Keys are derived when a policy is
// evaluated and never stored.
type ProviderKeys struct {
	byTool map[string]string
}

// NewProviderKeys derives the keys for every tool of base (the catalog-derived
// registry, as BuiltInAgentTools returns it) under config. It reads no catalog,
// environment or executable and leaves base unmodified, so one base captured
// outside a blackboard transaction serves every evaluation.
func NewProviderKeys(base map[string]models.AgentToolConfig, config models.Config) ProviderKeys {
	registry := maps.Clone(base)
	if registry == nil {
		registry = make(map[string]models.AgentToolConfig, len(config.AgentTools))
	}
	overlayAgentTools(registry, config)
	byTool := make(map[string]string, len(registry))
	for name, tool := range registry {
		byTool[name] = strings.TrimSpace(tool.ProviderKey)
	}
	return ProviderKeys{byTool: byTool}
}

// Key returns the provider key recorded for tool. A tool the registry does not
// know keeps its own name, and an empty recorded identity stays empty. For a
// registered tool the result equals ResolveLaunchPlan's ProviderKey.
func (k ProviderKeys) Key(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return ""
	}
	if key, ok := k.byTool[tool]; ok {
		return key
	}
	return tool
}

// Same reports whether two recorded tool names share a provider key.
func (k ProviderKeys) Same(a, b string) bool {
	return k.Key(a) == k.Key(b)
}
