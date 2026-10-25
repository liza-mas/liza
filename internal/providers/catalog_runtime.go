package providers

import (
	"github.com/liza-mas/liza/internal/models"
)

func (c Catalog) RuntimeTools() map[string]models.AgentToolConfig {
	out := make(map[string]models.AgentToolConfig, len(c.Providers)*2)
	for _, p := range c.Providers {
		out[p.ID] = p.RuntimeToolConfig()
		if p.ACPRuntime != nil {
			out[p.ID+"-acp"] = p.ACPToolConfig()
		}
	}
	return out
}

func (p Provider) RuntimeToolConfig() models.AgentToolConfig {
	return runtimeToolConfig(p.ID, p.Backend, p.Runtime)
}

// ACPToolConfig returns the AgentToolConfig derived from the provider's
// acp_runtime block. Returns an empty config if the provider has no ACP
// runtime.
func (p Provider) ACPToolConfig() models.AgentToolConfig {
	if p.ACPRuntime == nil {
		return models.AgentToolConfig{}
	}
	return runtimeToolConfig(p.ID+"-acp", BackendACPX, *p.ACPRuntime)
}

func runtimeToolConfig(id string, backend Backend, rt Runtime) models.AgentToolConfig {
	required := append([]string(nil), rt.RequiredExecutables...)
	if len(required) == 0 && backend == BackendACPX {
		required = []string{"acpx"}
	}
	transport := rt.PromptTransport
	if transport == "" {
		transport = PromptTransportStdin
	}
	providerKey := rt.ProviderKey
	if providerKey == "" {
		providerKey = id
	}
	return models.AgentToolConfig{
		Backend:             string(backend),
		ProviderKey:         providerKey,
		Executable:          rt.Executable,
		PromptTransport:     string(transport),
		RunArgs:             append([]string(nil), rt.RunArgs...),
		LoggedRunArgs:       append([]string(nil), rt.LoggedRunArgs...),
		InteractiveArgs:     append([]string(nil), rt.InteractiveArgs...),
		ModelArgs:           append([]string(nil), rt.ModelArgs...),
		EnvFiles:            append([]string(nil), rt.EnvFiles...),
		RequiredExecutables: required,
		ContractKey:         rt.ContractKey,
		ACPXAgent:           rt.ACPXAgent,
		ACPXSessionName:     rt.ACPXSessionName,
		ACPXShowArgs:        append([]string(nil), rt.ACPXShowArgs...),
		ACPXEnsureArgs:      append([]string(nil), rt.ACPXEnsureArgs...),
		ACPXSetModeArgs:     append([]string(nil), rt.ACPXSetModeArgs...),
		ACPXPromptArgs:      append([]string(nil), rt.ACPXPromptArgs...),
		ACPXEventMode:       rt.ACPXEventMode,
	}
}
