package commands

import (
	"sync"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/analysis"
	"github.com/liza-mas/liza/internal/models"
)

// providerKeysOf derives the provider keys policy compares for state: the
// catalog-derived tool registry under the config of the state being evaluated.
// Callers evaluate outside any blackboard transaction and derive once per
// evaluation.
func providerKeysOf(state *models.State) agentlaunch.ProviderKeys {
	return agentlaunch.NewProviderKeys(agentlaunch.BuiltInAgentTools(), state.Config)
}

// providerKeyFor is providerKeysOf for analysis. Derivation is deferred to the
// first lookup, so a watch tick whose anomalies name no provider builds nothing.
func providerKeyFor(state *models.State) analysis.ProviderKeyFunc {
	var (
		once sync.Once
		keys agentlaunch.ProviderKeys
	)
	return func(recorded string) string {
		once.Do(func() { keys = providerKeysOf(state) })
		return keys.Key(recorded)
	}
}
