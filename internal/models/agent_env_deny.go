package models

import (
	"fmt"
	"runtime"
	"slices"
	"strings"
)

// envNamesFoldCase reports whether variable names compare case-insensitively,
// as Windows environments do. A variable so tests can exercise both rules.
var envNamesFoldCase = runtime.GOOS == "windows"

// agentEnvDenyProtectedNames are process-critical on native Windows (home
// lookup, process creation, shell) and meaningless elsewhere, so the deny list
// refuses them on every platform, beyond the runtime-input reserved set.
var agentEnvDenyProtectedNames = []string{"USERPROFILE", "SYSTEMROOT", "COMSPEC"}

// EnvNameKey is the comparison key of a variable name under the platform's
// case rule.
func EnvNameKey(name string) string {
	if envNamesFoldCase {
		return strings.ToUpper(name)
	}
	return name
}

// ValidateAgentEnvDeny checks config.agent_env_deny names (ADR-0213). Denied
// names are removed from supervisors and every session, so they must never
// name what processes or the engine depend on.
func ValidateAgentEnvDeny(names []string) error {
	if len(names) > runtimeInputLimit {
		return fmt.Errorf("agent_env_deny holds at most %d names", runtimeInputLimit)
	}
	seen := make(map[string]bool, len(names))
	for i, name := range names {
		if len(name) > 256 || !prerequisiteEnvName.MatchString(name) {
			return fmt.Errorf("agent_env_deny[%d] must be a variable identifier of at most 256 bytes", i)
		}
		if runtimeInputNameReserved(name) || slices.Contains(agentEnvDenyProtectedNames, strings.ToUpper(name)) {
			return fmt.Errorf("agent_env_deny[%d] %s is a process or engine variable and cannot be withheld", i, name)
		}
		if seen[EnvNameKey(name)] {
			return fmt.Errorf("agent_env_deny[%d] %s is listed twice", i, name)
		}
		seen[EnvNameKey(name)] = true
	}
	return nil
}

// ReservedEnvNames returns every name withheld from agent sessions, gate
// commands and copied env files: runtime-input names and config.agent_env_deny.
// A runtime input still delivers its own value to its declared commands; the
// deny list only stops host inheritance.
func ReservedEnvNames(state *State) map[string]bool {
	names := RuntimeInputDenyNames(state)
	if state != nil {
		for _, name := range state.Config.AgentEnvDeny {
			names[name] = true
		}
	}
	return names
}

// EnvNameIn reports whether name is in names under the platform's case rule.
func EnvNameIn(names map[string]bool, name string) bool {
	if names[name] {
		return true
	}
	if !envNamesFoldCase {
		return false
	}
	key := EnvNameKey(name)
	for denied := range names {
		if EnvNameKey(denied) == key {
			return true
		}
	}
	return false
}
