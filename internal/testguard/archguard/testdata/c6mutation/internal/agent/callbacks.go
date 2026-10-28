package agent

import (
	"c6fixture/internal/db"
	"c6fixture/internal/models"
	"c6fixture/internal/ops"
)

func callbacks(bb *db.Blackboard) {
	_ = ops.ModifyWithAgentAuthority(bb, "a", func(s *models.State) error { return nil })
	mutate := func(s *models.State) error { return nil }
	_ = ops.ModifyWithAgentAuthority(bb, "a", mutate)
	api := ops.ModifyWithAgentAuthority
	_ = api
}
