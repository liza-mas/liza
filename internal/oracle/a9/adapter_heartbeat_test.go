package a9

import (
	"context"
	"fmt"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
)

// heartbeatRefusal is what the exported heartbeat reported for a registration
// whose generation it no longer holds.
type heartbeatRefusal struct {
	// AuthorityError reports whether Start failed with an agent-authority error.
	AuthorityError bool
	// Returned reports whether Start stopped by itself, before its deadline.
	Returned bool
}

// heartbeatRefused is the I-03 refusal adapter (AC2). The binary cannot reach
// it: removing a live registration ends the whole session at once (R25), so no
// CLI path keeps a supervisor alive past the heartbeat's own authority check
// (R27). The fixture state is loaded for the default interval, the heartbeat is
// given the registered agent's ID with a stale generation, and Start runs under
// a deadline past one interval. Only exported entry points are called.
func heartbeatRefused(statePath, agentID string, deadline time.Duration) (heartbeatRefusal, error) {
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return heartbeatRefusal{}, fmt.Errorf("read fixture state: %w", err)
	}
	beat := agent.NewHeartbeat(agent.HeartbeatConfig{
		AgentID:   agentID,
		Authority: models.AgentAuthority{ID: agentID, Generation: "stale-generation"},
		StatePath: statePath,
		State:     state,
	})
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	err = beat.Start(ctx)
	return heartbeatRefusal{AuthorityError: ops.IsAgentAuthorityError(err), Returned: ctx.Err() == nil}, nil
}
