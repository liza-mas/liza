package commands

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// Flags of the agent command that pool repair starts agents with.
const (
	agentFlagCLI        = "--cli"
	agentFlagAgentID    = "--agent-id"
	agentFlagModelsItem = "--models-item"
)

// RepairCLI chooses the CLI pool repair starts agents with.
type RepairCLI struct {
	// Explicit is the operator's --cli; it applies to every role.
	Explicit string
	// RoleModels is the models.yaml selection, used when Explicit is empty.
	// Its reviewer lists bind review slots even then, as claims read it.
	RoleModels rolemodels.File
}

// resolveRepairCLI returns the CLI an agent of role will run, and the --cli
// value to start it with. A role covered by models.yaml is started without
// --cli, so the agent resolves the same entry, model included, itself.
func resolveRepairCLI(sel RepairCLI, role string, state *models.State, pr models.PipelineResolver) (cliName, spawnCLI string, err error) {
	if sel.Explicit != "" {
		return sel.Explicit, sel.Explicit, nil
	}
	roleType, err := pr.RoleType(role)
	if err != nil {
		return "", "", err
	}
	selection, err := agentlaunch.ResolveLaunchSelection(agentlaunch.LaunchSelectionRequest{
		Role:       role,
		RoleType:   string(roleType),
		Config:     state.Config,
		RoleModels: sel.RoleModels,
	})
	if err != nil {
		return "", "", err
	}
	if selection.Source == agentlaunch.SelectionSourceModelsFile {
		cliName = selection.CLI
	} else {
		cliName = agentlaunch.ResolveDefaultCLIForRole(string(roleType), agentlaunch.CLIResolutionConfig{
			DefaultCLI:         state.Config.DefaultCLI,
			DefaultDoerCLI:     state.Config.DefaultDoerCLI,
			DefaultReviewerCLI: state.Config.DefaultReviewerCLI,
		})
		spawnCLI = cliName
	}
	availableCLIs := agentlaunch.AvailableCLIs(state.Config)
	if !slices.Contains(availableCLIs, cliName) {
		return "", "", fmt.Errorf("invalid CLI for role %s: %s (must be %s)", role, cliName, strings.Join(availableCLIs, ", "))
	}
	return cliName, spawnCLI, nil
}

// subtractPendingSpawns removes demand already covered by started agent
// processes that have not registered yet; each also takes one of the role's
// starts. Roles left with nothing to start are dropped.
func subtractPendingSpawns(missing []MissingRoleWork, pending map[string][]int) []MissingRoleWork {
	if len(pending) == 0 {
		return missing
	}
	kept := missing[:0]
	for _, roleWork := range missing {
		items := pending[roleWork.Role]
		if len(roleWork.plannedItems) > 0 {
			roleWork = coverPlannedItems(roleWork, items)
		} else {
			roleWork.SpawnCount -= len(items)
		}
		if roleWork.SpawnCount > 0 {
			roleWork.AgentIDs = roleWork.AgentIDs[:min(len(roleWork.AgentIDs), roleWork.SpawnCount)]
			kept = append(kept, roleWork)
		}
	}
	return kept
}

// coverPlannedItems lets each pending reviewer cover one planned start for
// the list item it was started with, then bounds the remaining starts by the
// headroom the pending ones leave. Matching by item keeps a pending item-1
// start from absorbing item-2 demand.
func coverPlannedItems(roleWork MissingRoleWork, pendingItems []int) MissingRoleWork {
	ids, items := slices.Clone(roleWork.plannedIDs), slices.Clone(roleWork.plannedItems)
	for _, item := range pendingItems {
		if i := slices.Index(items, item); i >= 0 {
			ids, items = slices.Delete(ids, i, i+1), slices.Delete(items, i, i+1)
		}
	}
	roleWork.SpawnCount = min(len(items), roleWork.headroom-len(pendingItems))
	roleWork.AgentIDs, roleWork.Items = nil, nil
	if roleWork.SpawnCount > 0 {
		roleWork.AgentIDs, roleWork.Items = ids[:roleWork.SpawnCount], items[:roleWork.SpawnCount]
	}
	return roleWork
}

// reviewerStartPlanner picks the IDs reviewers are started under. The
// launch path would auto-assign the first free <role>-N, which can be a prior
// approver's ID once that agent unregistered, so each start gets an explicit
// ID that the production claim filters accept for the task it is planned for.
// Candidates are evaluated in an isolated state projection holding one
// hypothetical reviewer registered the way a new one would be: the spawn CLI
// as provider and this process's PID for the live-process admission check.
// When a models.yaml list binds the role's review slots, each task is planned
// for the item bound to its next slot, registered with that item's CLI and
// model; all items share the role's ID reservations.
type reviewerStartPlanner struct {
	projection *models.State
	role       string
	slots      rolemodels.Selection
	// rows holds one hypothetical registration per list item started with
	// --models-item, or a single one for the spawn CLI.
	rows    []models.Agent
	perItem bool
	taken   map[string]bool
	now     time.Time
}

func newReviewerStartPlanner(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reviewerRole string, reservedIDs map[string]bool, now time.Time) *reviewerStartPlanner {
	cliName, _, err := resolveRepairCLI(repairCLI, reviewerRole, state, pr)
	if err != nil {
		return nil
	}
	// Same occupancy as the launch-side allocator: IDs of live registrations,
	// plus IDs of started agents still registering.
	taken := make(map[string]bool, len(state.Agents)+len(reservedIDs))
	window := agentLivenessWindow(state.Config)
	for id, agentState := range state.Agents {
		if agentHasLiveRegistration(agentState, now, window) {
			taken[id] = true
		}
	}
	for id := range reservedIDs {
		taken[id] = true
	}
	projection := *state
	projection.Agents = maps.Clone(state.Agents)
	if projection.Agents == nil {
		projection.Agents = make(map[string]models.Agent)
	}
	leaseExpires := now.Add(time.Duration(models.DefaultLeaseDurationSeconds) * time.Second)
	row := models.Agent{
		Role:         reviewerRole,
		Status:       models.AgentStatusIdle,
		Provider:     cliName,
		PID:          os.Getpid(),
		Heartbeat:    now,
		RegisteredAt: now,
		LeaseExpires: &leaseExpires,
	}
	planner := &reviewerStartPlanner{
		projection: &projection,
		role:       reviewerRole,
		rows:       []models.Agent{row},
		taken:      taken,
		now:        now,
	}
	planner.slots, planner.perItem = repairCLI.RoleModels.ReviewSlots(reviewerRole)
	// An explicit --cli starts every reviewer on that CLI; the slots still
	// bind what it may claim.
	if planner.perItem && repairCLI.Explicit == "" {
		planner.rows = make([]models.Agent, len(planner.slots.Items))
		for i, item := range planner.slots.Items {
			planner.rows[i] = row
			planner.rows[i].Provider, planner.rows[i].Model = item.CLI, item.Model
		}
	} else {
		planner.perItem = false
	}
	return planner
}

// itemFor returns the 1-based list item a reviewer for task starts with, or 0
// when starts do not pick an item.
func (p *reviewerStartPlanner) itemFor(task *models.Task) int {
	if !p.perItem {
		return 0
	}
	return min(len(task.Approvals), len(p.rows)-1) + 1
}

// cliFor returns the CLI a start for item runs.
func (p *reviewerStartPlanner) cliFor(item int) string {
	return p.rows[max(item-1, 0)].Provider
}

// assign returns the lowest free ID a new reviewer could claim task under,
// and the list item to start it with, and reserves the ID. Only IDs named by
// the task's approvals, history or validation records can be refused for the
// ID itself, so the search stops once every such ID could have been skipped;
// beyond that a refusal is about the provider, model or slot.
func (p *reviewerStartPlanner) assign(task *models.Task, policy ops.ReviewerClaimPolicyResolver) (string, int, bool) {
	item := p.itemFor(task)
	row := p.rows[max(item-1, 0)]
	limit := len(p.taken) + len(task.Approvals) + len(task.History) + len(p.projection.ValidationReadiness) + 1
	for n := 1; n <= limit; n++ {
		agentID := fmt.Sprintf("%s-%d", p.role, n)
		if p.taken[agentID] {
			continue
		}
		previous, existed := p.projection.Agents[agentID]
		p.projection.Agents[agentID] = row
		eligible := ops.ReviewerClaimEligible(ops.ReviewerClaimEligibilityInput{
			State: p.projection, Task: task, AgentID: agentID,
			ReviewerRole: p.role, Now: p.now, Resolver: policy, Slots: p.slots,
		})
		if existed {
			p.projection.Agents[agentID] = previous
		} else {
			delete(p.projection.Agents, agentID)
		}
		if eligible {
			p.taken[agentID] = true
			return agentID, item, true
		}
	}
	return "", item, false
}

// roleStartPlan is the starts one role's missing work gets.
type roleStartPlan struct {
	// cli is the CLI the role's starts run; empty when they run list items on
	// different CLIs (each start then records its own).
	cli string
	// spawnCLI is the --cli the starts are given; empty when a models.yaml
	// entry covers the role and the agent resolves it itself.
	spawnCLI string
	starts   []SpawnedAgent
}

// planRoleStarts names the starts for roleWork: the CLI, the models.yaml list
// item and the explicit agent ID of each, and the command that would run it.
func planRoleStarts(repairCLI RepairCLI, roleWork MissingRoleWork, state *models.State, pr models.PipelineResolver) (roleStartPlan, error) {
	cliName, spawnCLI, err := resolveRepairCLI(repairCLI, roleWork.Role, state, pr)
	if err != nil {
		return roleStartPlan{}, err
	}
	slots, _ := repairCLI.RoleModels.ReviewSlots(roleWork.Role)
	starts := make([]SpawnedAgent, 0, roleWork.SpawnCount)
	for i := range roleWork.SpawnCount {
		start := SpawnedAgent{Role: roleWork.Role, CLI: cliName, Command: brand.Command("agent", roleWork.Role)}
		if i < len(roleWork.Items) {
			start.Item = roleWork.Items[i]
		}
		switch {
		case start.Item > 0:
			start.CLI = slots.Items[start.Item-1].CLI
			start.Command += " " + agentFlagModelsItem + " " + strconv.Itoa(start.Item)
		case spawnCLI != "":
			start.Command += " " + agentFlagCLI + " " + spawnCLI
		}
		if i < len(roleWork.AgentIDs) {
			start.AgentID = roleWork.AgentIDs[i]
			start.Command += " " + agentFlagAgentID + " " + start.AgentID
		}
		starts = append(starts, start)
	}
	roleCLI := cliName
	for _, start := range starts {
		if start.CLI != starts[0].CLI {
			roleCLI = ""
			break
		}
		roleCLI = start.CLI
	}
	return roleStartPlan{cli: roleCLI, spawnCLI: spawnCLI, starts: starts}, nil
}
