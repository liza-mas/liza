package agent

import (
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
)

// systemPromptContractsEnv sets the variable that lets a session past
// enforce-init.sh and drops session-context.sh's read instruction, only when
// the launch injected the contracts; an inherited value is removed otherwise.
func systemPromptContractsEnv(base []string, injected bool) []string {
	name := brand.EnvName("SYSTEM_PROMPT_CONTRACTS")
	out := slices.DeleteFunc(slices.Clone(base), func(entry string) bool { return strings.HasPrefix(entry, name+"=") })
	if injected {
		out = append(out, name+"=1")
	}
	return out
}
