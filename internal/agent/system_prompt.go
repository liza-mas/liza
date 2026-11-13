package agent

import (
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// systemPromptContractsEnv sets the variables that let a session (and,
// separately, its subagents) past enforce-init.sh and drop session-context.sh's
// read instruction, only when the launch injected those contracts; inherited
// values are removed otherwise.
func systemPromptContractsEnv(base []string, injected, subagentInjected bool) []string {
	name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
	subagentName := brand.EnvName("SUBAGENT_SYSTEM_PROMPT_CONTRACTS")
	out := slices.DeleteFunc(slices.Clone(base), func(entry string) bool {
		return strings.HasPrefix(entry, name+"=") || strings.HasPrefix(entry, subagentName+"=")
	})
	if injected {
		out = append(out, name+"=1")
	}
	if subagentInjected {
		out = append(out, subagentName+"=1")
	}
	return out
}
