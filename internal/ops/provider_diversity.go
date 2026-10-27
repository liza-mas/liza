package ops

import (
	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/models"
)

// minDiverseApprovals is the approval count below which diversity cannot hold.
const minDiverseApprovals = 2

// ApprovalsHaveProviderDiversity reports whether approvals come from at least
// two distinct providers, comparing canonical provider keys rather than the
// recorded tool names (codex and codex-acp are one provider). It applies
// models.Task.HasProviderDiversity's rule: some approval after the first
// differs from the first.
func ApprovalsHaveProviderDiversity(approvals []models.Approval, keys agentlaunch.ProviderKeys) bool {
	if len(approvals) < minDiverseApprovals {
		return false
	}
	first := approvals[0].Provider
	for _, a := range approvals[1:] {
		if !keys.Same(a.Provider, first) {
			return true
		}
	}
	return false
}
