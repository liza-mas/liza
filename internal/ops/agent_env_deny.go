package ops

import (
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/secretmask"
)

// AgentEnvDenyConfigKey names the operator-only list of host variables
// withheld from supervisors, sessions and copied env files (ADR-0213).
const AgentEnvDenyConfigKey = "config.agent_env_deny"

// SetAgentEnvDenyInput describes an operator write of config.agent_env_deny.
// Names replaces the whole list; an empty list clears it.
type SetAgentEnvDenyInput struct {
	Names   []string
	Replace bool
	Reason  string
}

// SetAgentEnvDeny stores config.agent_env_deny under the shared config rule:
// compare and write in one transaction; replacing a different non-empty list
// requires --replace with --reason. The Modify fence refuses a name that a
// live validation prerequisite requires. Running sessions are not changed.
func SetAgentEnvDeny(projectRoot string, input SetAgentEnvDenyInput) (*ConfigSetResult, error) {
	if err := models.ValidateAgentEnvDeny(input.Names); err != nil {
		return nil, &PreconditionError{Reason: err.Error()}
	}
	if input.Replace && strings.TrimSpace(input.Reason) == "" {
		return nil, &PreconditionError{Reason: "--replace requires a non-empty --reason"}
	}
	bb := db.For(paths.New(projectRoot).StatePath())
	result := &ConfigSetResult{Key: AgentEnvDenyConfigKey}
	var previous []string
	err := bb.Modify(func(state *models.State) error {
		previous = slices.Clone(state.Config.AgentEnvDeny)
		switch {
		case slices.Equal(previous, input.Names):
			result.Outcome = "unchanged"
		case len(previous) > 0 && !input.Replace:
			result.Outcome = "conflict"
			return &PreconditionError{
				Reason:  AgentEnvDenyConfigKey + " is already set to a different list; use --replace with --reason to replace it",
				Details: map[string]any{"key": AgentEnvDenyConfigKey, "conflict": "existing_value"},
			}
		default:
			state.Config.AgentEnvDeny = slices.Clone(input.Names)
			switch {
			case len(input.Names) == 0:
				result.Outcome = "cleared"
			case len(previous) > 0:
				result.Outcome = "replaced"
			default:
				result.Outcome = "set"
			}
		}
		return nil
	})
	if err != nil && result.Outcome != "conflict" {
		result.Outcome = "failed"
	}
	mask := secretmask.New()
	log.Printf("config_set key=%s outcome=%s actor=%q project=%q previous=%q value=%q reason=%q",
		AgentEnvDenyConfigKey, result.Outcome, "operator", mask.MaskText(projectRoot), strings.Join(previous, ","), strings.Join(input.Names, ","), boundPostWorktreeCmd(mask.MaskText(input.Reason)))
	if err != nil {
		return nil, fmt.Errorf("set %s: %w", AgentEnvDenyConfigKey, err)
	}
	return result, nil
}

// WithholdDeniedEnvironment removes config.agent_env_deny names from this
// process's environment, so every process it starts afterwards inherits none
// of them (ADR-0213). Supervisors and the deferred-validation runner call it
// at startup. A project without state withholds nothing; an unreadable state
// fails rather than run unscrubbed. It returns the removed names, never values.
// Removing a name from the list later cannot restore its value here: that
// takes a fresh process.
func WithholdDeniedEnvironment(projectRoot string) ([]string, error) {
	statePath := paths.New(projectRoot).StatePath()
	if _, err := os.Stat(statePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return nil, fmt.Errorf("read config.agent_env_deny before starting: %w", err)
	}
	deny := make(map[string]bool, len(state.Config.AgentEnvDeny))
	for _, name := range state.Config.AgentEnvDeny {
		deny[name] = true
	}
	var removed []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "" || !models.EnvNameIn(deny, name) {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return removed, fmt.Errorf("withhold %s: %w", name, err)
		}
		removed = append(removed, name)
	}
	return removed, nil
}
