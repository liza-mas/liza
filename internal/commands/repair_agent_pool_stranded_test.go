package commands

import (
	"slices"
	"testing"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/rolemodels"
	"github.com/liza-mas/liza/internal/testhelpers"
)

// D75 residual (2026-09-27 07:37Z shape): the only coder died holding an
// executing claim, its task lease and registration lease both expired, and no
// coder was live. Autorepair saw zero coder demand and spawned nobody for 40
// minutes. A stranded claim must count as coder work.
func TestFindMissingRolesWithClaimableWork_StrandedExecutingClaim(t *testing.T) {
	now := time.Now().UTC()
	expired := now.Add(-time.Minute)
	live := now.Add(time.Hour)

	buildState := func(holderLease time.Time) *models.State {
		state := testhelpers.CreateValidState()
		holder := "coder-1"
		task := testhelpers.BuildTaskByStatus("stranded", models.TaskStatusImplementing, now)
		task.AssignedTo = &holder
		task.LeaseExpires = &expired
		task.Worktree = testhelpers.StringPtr(".worktrees/stranded")
		task.BaseCommit = testhelpers.StringPtr("base123")
		state.Tasks = []models.Task{task}
		state.Agents = map[string]models.Agent{
			holder: {Role: "coder", Provider: "anthropic", Status: models.AgentStatusWorking,
				Generation: "g1", CurrentTask: &task.ID, Heartbeat: holderLease, LeaseExpires: &holderLease},
		}
		return state
	}

	projectRoot := writeRepairAgentPoolState(t, buildState(expired))
	resolver, err := ops.LoadResolverForModels(projectRoot)
	if err != nil {
		t.Fatalf("LoadResolverForModels() error = %v", err)
	}

	t.Run("dead holder makes the stranded task coder demand", func(t *testing.T) {
		missing := FindMissingRolesWithClaimableWork(buildState(expired), resolver, rolemodels.File{})
		var coder *MissingRoleWork
		for i := range missing {
			if missing[i].Role == models.RoleCoder {
				coder = &missing[i]
			}
		}
		if coder == nil {
			t.Fatalf("missing = %+v, want coder demand for the stranded task", missing)
		}
		if !slices.Equal(coder.TaskIDs, []string{"stranded"}) {
			t.Fatalf("coder demand tasks = %v, want [stranded]", coder.TaskIDs)
		}
	})

	t.Run("live holder is not demand", func(t *testing.T) {
		for _, work := range FindMissingRolesWithClaimableWork(buildState(live), resolver, rolemodels.File{}) {
			if work.Role == models.RoleCoder {
				t.Fatalf("missing = %+v, want no coder demand while the holder is registered", work)
			}
		}
	})
}
