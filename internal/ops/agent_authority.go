package ops

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
)

const missingAgentGeneration = "<missing>"

// AgentAuthorityError reports a caller that no longer owns the current
// registration generation for an agent ID.
type AgentAuthorityError struct {
	AgentID           string `json:"agent_id"`
	LosingGeneration  string `json:"-" yaml:"-"`
	CurrentGeneration string `json:"-" yaml:"-"`
}

func (e *AgentAuthorityError) Error() string {
	return fmt.Sprintf(
		"agent %s authority rejected: losing generation fingerprint %s, current generation fingerprint %s",
		nonEmptyGeneration(e.AgentID),
		generationFingerprint(e.LosingGeneration),
		generationFingerprint(e.CurrentGeneration),
	)
}

// SafeDetails exposes structured generation diagnostics to JSON error writers.
func (e *AgentAuthorityError) SafeDetails() map[string]any {
	return map[string]any{
		"agent_id":                       e.AgentID,
		"losing_generation_fingerprint":  generationFingerprint(e.LosingGeneration),
		"current_generation_fingerprint": generationFingerprint(e.CurrentGeneration),
	}
}

func generationFingerprint(value string) string {
	if value == "" {
		return missingAgentGeneration
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

// IsAgentAuthorityError reports whether err contains a rejected generation.
func IsAgentAuthorityError(err error) bool {
	var target *AgentAuthorityError
	return errors.As(err, &target)
}

// RequireAgentAuthority validates caller-held authority against the currently
// registered generation. It must run inside the mutation's blackboard lock.
func RequireAgentAuthority(state *models.State, authority models.AgentAuthority) error {
	currentGeneration := ""
	if state != nil {
		if agent, exists := state.Agents[authority.ID]; exists {
			currentGeneration = agent.Generation
		}
	}
	if authority.ID == "" || authority.Generation == "" || currentGeneration == "" || authority.Generation != currentGeneration {
		return &AgentAuthorityError{
			AgentID:           authority.ID,
			LosingGeneration:  authority.Generation,
			CurrentGeneration: currentGeneration,
		}
	}
	return nil
}

// ModifyWithAgentAuthority compares registration authority and applies the
// mutation inside one locked blackboard transaction.
func ModifyWithAgentAuthority(bb *db.Blackboard, authority models.AgentAuthority, mutate func(*models.State) error) error {
	return bb.Modify(func(state *models.State) error {
		if err := RequireAgentAuthority(state, authority); err != nil {
			return err
		}
		return mutate(state)
	})
}

func nonEmptyGeneration(value string) string {
	if value == "" {
		return missingAgentGeneration
	}
	return value
}
