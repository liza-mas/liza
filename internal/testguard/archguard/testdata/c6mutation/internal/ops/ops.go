// Package ops mirrors the named operations and the callback API.
package ops

import (
	"c6fixture/internal/db"
	"c6fixture/internal/models"
)

// ModifyWithAgentAuthority is the mutation callback API.
func ModifyWithAgentAuthority(bb *db.Blackboard, agent string, fn func(*models.State) error) error {
	return bb.Modify(fn)
}

// WithLock runs a lock-only callback.
func WithLock(fn func() error) error { return fn() }

// RegisterAgent is a named operation taking typed data.
func RegisterAgent(bb *db.Blackboard, id string) error {
	return ModifyWithAgentAuthority(bb, id, func(s *models.State) error {
		s.Mode = "registered"
		return nil
	})
}
