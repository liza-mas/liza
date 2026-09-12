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
	reason := "registration changed"
	if e.LosingGeneration == "" || e.CurrentGeneration == "" {
		reason = "missing registration authority"
	}
	return fmt.Sprintf("agent %s authority rejected: %s; stop", nonEmptyGeneration(e.AgentID), reason)
}

// SafeDetails identifies a rejected caller without disclosing registration values.
func (e *AgentAuthorityError) SafeDetails() map[string]any {
	return map[string]any{
		"agent_id":    e.AgentID,
		"outcome":     "STALE_CALLER",
		"safe_action": "stop",
		"effects":     "none",
	}
}

// LifecycleResult lets presentation adapters expose recovery even when authority
// was rejected before a task could safely be observed.
func (e *AgentAuthorityError) LifecycleResult() any { return e.SafeDetails() }

// generationFingerprint remains internal provenance for quarantined evidence.
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
