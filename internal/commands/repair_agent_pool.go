package commands

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agent"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/process"
)

type RepairAgentPoolOptions struct {
	ProjectRoot string
	Missing     bool
	CLI         string
	DryRun      bool
	Roles       []string
	// PendingSpawns counts, per role, started agent processes that have not
	// registered yet; they already cover demand and consume headroom.
	PendingSpawns map[string]int
	// PendingAgentIDs are explicit IDs those processes will register; new
	// starts must not reuse them.
	PendingAgentIDs map[string]bool
}

type MissingRoleWork struct {
	Role      string   `json:"role"`
	TaskIDs   []string `json:"task_ids"`
	TaskCount int      `json:"task_count"`
	// SpawnCount is how many agents to start: uncovered demand bounded by
	// the role's remaining max-instances headroom.
	SpawnCount int `json:"spawn_count"`
	// AgentIDs, when set, are the explicit IDs to start reviewers under,
	// one per start: IDs the claim filters accept for the demand, since the
	// auto-assigned ID could be a prior approver's.
	AgentIDs []string `json:"agent_ids,omitempty"`
	CLI      string   `json:"cli,omitempty"`
	// Reason explains demand that does not come from claimable tasks.
	Reason string `json:"reason,omitempty"`
}

// UnservableRoleWork is reviewer demand that an agent started with CLI could
// not claim, so starting one would only fill capacity.
type UnservableRoleWork struct {
	Role    string   `json:"role"`
	CLI     string   `json:"cli"`
	TaskIDs []string `json:"task_ids"`
	Reason  string   `json:"reason"`
}

type SpawnedAgent struct {
	Role    string `json:"role"`
	AgentID string `json:"agent_id,omitempty"`
	CLI     string `json:"cli"`
	Command string `json:"command"`
	PID     int    `json:"pid,omitempty"`
}

type FailedAgentSpawn struct {
	Role    string `json:"role"`
	CLI     string `json:"cli"`
	Command string `json:"command"`
	Error   string `json:"error"`
}

type DegradedAgentCapacity struct {
	AgentID     string `json:"agent_id"`
	Role        string `json:"role"`
	Reason      string `json:"reason"`
	LastError   string `json:"last_error,omitempty"`
	RecoverHint string `json:"recover_hint,omitempty"`
}

type RepairAgentPoolResult struct {
	CLI        string                    `json:"cli"`
	RoleCLIs   map[string]string         `json:"role_clis,omitempty"`
	DryRun     bool                      `json:"dry_run"`
	Missing    []MissingRoleWork         `json:"missing"`
	Unservable []UnservableRoleWork      `json:"unservable,omitempty"`
	Degraded   []DegradedAgentCapacity   `json:"degraded,omitempty"`
	Spawned    []SpawnedAgent            `json:"spawned,omitempty"`
	Failed     []FailedAgentSpawn        `json:"failed,omitempty"`
	Commands   []string                  `json:"commands,omitempty"`
	Validation []ValidationAgentCapacity `json:"validation,omitempty"`
}

// ValidationAgentCapacity is a task-specific observation, not a reusable proof
// of readiness. Unverified registrations may try preflight; failed ones need
// repair/revalidation rather than equivalent replacement processes.
type ValidationAgentCapacity struct {
	AgentID     string `json:"agent_id"`
	Role        string `json:"role"`
	TaskID      string `json:"task_id"`
	Status      string `json:"status"`
	Code        string `json:"code,omitempty"`
	RecoverHint string `json:"recover_hint"`
}

// repairAgentPoolSpawn starts one agent on cli; an empty agentID lets the
// agent auto-assign its ID. cliFromConfig starts it without --cli, so it
// resolves its models.yaml entry, model included, itself.
var repairAgentPoolSpawn = func(projectRoot, role, cli, agentID string, cliFromConfig bool) (int, error) {
	var extraArgs []string
	if agentID != "" {
		extraArgs = []string{"--agent-id", agentID}
	}
	spawn := process.SpawnAgent
	if cliFromConfig {
		spawn = process.SpawnConfiguredAgent
	}
	cmd, err := spawn(projectRoot, role, cli, extraArgs...)
	if err != nil {
		return 0, err
	}
	if cmd.Process == nil {
		return 0, nil
	}
	return cmd.Process.Pid, nil
}

// SetRepairAgentPoolSpawnForTest replaces the agent launcher so tests outside
// this package can observe the IDs agents are started under. It returns the
// restore function.
func SetRepairAgentPoolSpawnForTest(spawn func(projectRoot, role, cli, agentID string, cliFromConfig bool) (int, error)) func() {
	previous := repairAgentPoolSpawn
	repairAgentPoolSpawn = spawn
	return func() { repairAgentPoolSpawn = previous }
}

var EnvAutoRepairAgentPool = brand.EnvName("AUTO_REPAIR_AGENT_POOL")

func AutoRepairAgentPoolEnabledFromEnv() (bool, string) {
	value, ok := os.LookupEnv(EnvAutoRepairAgentPool)
	return parseAutoRepairAgentPoolEnv(value, ok)
}

func parseAutoRepairAgentPoolEnv(value string, ok bool) (bool, string) {
	if !ok {
		return true, ""
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true, ""
	}
	if strings.EqualFold(trimmed, "no") {
		return false, ""
	}
	enabled, err := strconv.ParseBool(trimmed)
	if err == nil {
		return enabled, ""
	}
	return true, fmt.Sprintf("%s=%q is not a recognized boolean; auto repair remains enabled", EnvAutoRepairAgentPool, value)
}

func RepairAgentPoolCommand(opts RepairAgentPoolOptions) error {
	result, err := RepairAgentPool(opts)
	if result != nil {
		printRepairAgentPoolResult(result)
	}
	return err
}

func RepairAgentPool(opts RepairAgentPoolOptions) (*RepairAgentPoolResult, error) {
	statePath := paths.New(opts.ProjectRoot).StatePath()
	state, err := db.For(statePath).Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read state: %w", err)
	}

	pr, err := ops.LoadResolverForModels(opts.ProjectRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to load pipeline resolver: %w", err)
	}

	availableCLIs := agent.AvailableCLIs(state.Config)
	if opts.CLI != "" && !slices.Contains(availableCLIs, opts.CLI) {
		return nil, fmt.Errorf("invalid CLI: %s (must be %s)", opts.CLI, strings.Join(availableCLIs, ", "))
	}

	roleModels, err := agent.LoadValidatedRoleModels(opts.ProjectRoot, pr.AllRoleNames(), state.Config)
	if err != nil {
		return nil, err
	}
	repairCLI := RepairCLI{Explicit: opts.CLI, RoleModels: roleModels}

	now := time.Now().UTC()
	missing, unservable := FindRoleCapacityDeficits(state, pr, repairCLI, opts.PendingAgentIDs, now)
	missing = append(missing, findMissingOrchestrator(state, pr, now)...)
	missing = subtractPendingSpawns(missing, opts.PendingSpawns)
	missing = filterMissingRoleWork(missing, opts.Roles)
	result := &RepairAgentPoolResult{
		CLI:        opts.CLI,
		DryRun:     opts.DryRun,
		Missing:    missing,
		Unservable: filterUnservableRoleWork(unservable, opts.Roles),
		Degraded:   findCurrentDegradedAgentCapacity(state),
		Validation: findValidationAgentCapacity(state, pr, opts.Roles),
	}

	commonImplicitCLI := ""
	heterogeneousImplicitCLI := false
	for i, roleWork := range missing {
		cliName, spawnCLI, err := resolveRepairCLI(repairCLI, roleWork.Role, state, pr)
		if err != nil {
			return nil, err
		}
		result.Missing[i].CLI = cliName
		if opts.CLI == "" {
			if result.RoleCLIs == nil {
				result.RoleCLIs = make(map[string]string)
			}
			result.RoleCLIs[roleWork.Role] = cliName
			if commonImplicitCLI == "" && !heterogeneousImplicitCLI {
				commonImplicitCLI = cliName
			} else if commonImplicitCLI != cliName {
				commonImplicitCLI = ""
				heterogeneousImplicitCLI = true
			}
		}
		starts := make([]SpawnedAgent, 0, roleWork.SpawnCount)
		for i := range roleWork.SpawnCount {
			start := SpawnedAgent{Role: roleWork.Role, CLI: cliName, Command: brand.Command("agent", roleWork.Role)}
			if spawnCLI != "" {
				start.Command += " --cli " + spawnCLI
			}
			if i < len(roleWork.AgentIDs) {
				start.AgentID = roleWork.AgentIDs[i]
				start.Command += " --agent-id " + start.AgentID
			}
			starts = append(starts, start)
			result.Commands = append(result.Commands, start.Command)
		}
		if opts.DryRun {
			continue
		}

		for _, start := range starts {
			pid, err := repairAgentPoolSpawn(opts.ProjectRoot, roleWork.Role, cliName, start.AgentID, spawnCLI == "")
			if err != nil {
				// The next start for this role would fail the same way.
				result.Failed = append(result.Failed, FailedAgentSpawn{
					Role:    roleWork.Role,
					CLI:     cliName,
					Command: start.Command,
					Error:   err.Error(),
				})
				break
			}
			start.PID = pid
			result.Spawned = append(result.Spawned, start)
		}
	}

	if opts.CLI == "" && !heterogeneousImplicitCLI {
		result.CLI = commonImplicitCLI
	}

	if len(result.Failed) > 0 {
		return result, repairAgentPoolSpawnError(result)
	}

	return result, nil
}

func filterMissingRoleWork(missing []MissingRoleWork, roles []string) []MissingRoleWork {
	if len(roles) == 0 {
		return missing
	}
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	filtered := make([]MissingRoleWork, 0, len(missing))
	for _, roleWork := range missing {
		if allowed[roleWork.Role] {
			filtered = append(filtered, roleWork)
		}
	}
	return filtered
}

// RepairCLI chooses the CLI pool repair starts agents with.
type RepairCLI struct {
	// Explicit is the operator's --cli; it applies to every role.
	Explicit string
	// RoleModels is the models.yaml selection, used when Explicit is empty.
	RoleModels agent.RoleModels
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
	selection, err := agent.ResolveLaunchSelection(agent.LaunchSelectionRequest{
		Role:       role,
		RoleType:   roleType,
		Config:     state.Config,
		RoleModels: sel.RoleModels,
	})
	if err != nil {
		return "", "", err
	}
	if selection.Source == agent.SelectionSourceModelsFile {
		cliName = selection.CLI
	} else {
		cliName = agent.ResolveDefaultCLIForRole(roleType, agent.CLIResolutionConfig{
			DefaultCLI:         state.Config.DefaultCLI,
			DefaultDoerCLI:     state.Config.DefaultDoerCLI,
			DefaultReviewerCLI: state.Config.DefaultReviewerCLI,
		})
		spawnCLI = cliName
	}
	availableCLIs := agent.AvailableCLIs(state.Config)
	if !slices.Contains(availableCLIs, cliName) {
		return "", "", fmt.Errorf("invalid CLI for role %s: %s (must be %s)", role, cliName, strings.Join(availableCLIs, ", "))
	}
	return cliName, spawnCLI, nil
}

func repairAgentPoolSpawnError(result *RepairAgentPoolResult) error {
	failures := make([]string, 0, len(result.Failed))
	for _, failure := range result.Failed {
		failures = append(failures, fmt.Sprintf("%s: %s", failure.Role, failure.Error))
	}
	planned := 0
	for _, roleWork := range result.Missing {
		planned += roleWork.SpawnCount
	}
	return fmt.Errorf("started %d of %d planned agent(s): %s",
		len(result.Spawned), planned, strings.Join(failures, "; "))
}

func filterUnservableRoleWork(unservable []UnservableRoleWork, roles []string) []UnservableRoleWork {
	if len(roles) == 0 {
		return unservable
	}
	filtered := make([]UnservableRoleWork, 0, len(unservable))
	for _, work := range unservable {
		if slices.Contains(roles, work.Role) {
			filtered = append(filtered, work)
		}
	}
	return filtered
}

// subtractPendingSpawns removes demand already covered by started agent
// processes that have not registered yet. Roles left with nothing to start
// are dropped.
func subtractPendingSpawns(missing []MissingRoleWork, pending map[string]int) []MissingRoleWork {
	if len(pending) == 0 {
		return missing
	}
	kept := missing[:0]
	for _, roleWork := range missing {
		roleWork.SpawnCount -= pending[roleWork.Role]
		if roleWork.SpawnCount > 0 {
			roleWork.AgentIDs = roleWork.AgentIDs[:min(len(roleWork.AgentIDs), roleWork.SpawnCount)]
			kept = append(kept, roleWork)
		}
	}
	return kept
}

type maxInstancesResolver interface {
	MaxInstances(role string) (int, error)
}

// FindRoleCapacityDeficits reports, per role, claimable work that idle
// claim-valid agents do not cover, and how many agents to start for it within
// the role's max-instances headroom. Headroom counts occupied registrations
// the way registration does, so a dead-but-leased agent keeps its slot
// without covering demand. Reviewer tasks that an agent started with the
// role's spawn CLI could not claim under any ID it could start with are
// returned as unservable instead; the others carry the ID to start with.
// reservedIDs are explicit IDs of started agents not registered yet.
func FindRoleCapacityDeficits(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reservedIDs map[string]bool, now time.Time) ([]MissingRoleWork, []UnservableRoleWork) {
	if state == nil || pr == nil {
		return nil, nil
	}

	window := agentLivenessWindow(state.Config)
	occupied := make(map[string]int)
	idle := make(map[string][]string)
	usable := make(map[string][]string)
	for _, agentID := range slices.Sorted(maps.Keys(state.Agents)) {
		agentState := state.Agents[agentID]
		if ops.AgentProcessOwnership(agentID, agentState, now).Occupied() {
			occupied[agentState.Role]++
		}
		if !agentHasLiveRegistration(agentState, now, window) ||
			agentHealthIsCurrentDegraded(state.AgentHealth[agentID], agentState) ||
			!ops.HasValidClaimRegistration(state, agentID, agentState.Role) {
			continue
		}
		usable[agentState.Role] = append(usable[agentState.Role], agentID)
		// Holding no task is what frees an agent to claim; lifecycle status
		// is not consulted.
		if agentState.CurrentTask == nil || *agentState.CurrentTask == "" {
			idle[agentState.Role] = append(idle[agentState.Role], agentID)
		}
	}

	reviewerPolicy, _ := pr.(ops.ReviewerClaimPolicyResolver)
	planners := make(map[string]*reviewerStartPlanner)
	plannedIDs := make(map[string][]string)
	matched := make(map[string]bool)
	idleDoersUsed := make(map[string]int)
	uncovered := make(map[string][]string)
	unservable := make(map[string][]string)

	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status.IsTerminal() || task.RolePair == "" {
			continue
		}
		doerRole, err := pr.DoerRole(task.RolePair)
		if err != nil {
			continue
		}
		reviewerRole, err := pr.ReviewerRole(task.RolePair)
		if err != nil {
			continue
		}

		// An agent that failed this task's validation needs repair and
		// revalidation, not an equivalent replacement process.
		validationFailedFor := func(role string) bool {
			return slices.ContainsFunc(usable[role], func(agentID string) bool {
				return models.ValidationTaskKnownFailed(state, task, agentID, now)
			})
		}

		if models.IsRoleTaskReady(state, task, doerRole, pr, now) && !validationFailedFor(doerRole) {
			if idleDoersUsed[doerRole] < len(idle[doerRole]) {
				idleDoersUsed[doerRole]++
			} else {
				uncovered[doerRole] = append(uncovered[doerRole], task.ID)
			}
		}

		if !models.IsRoleTaskReady(state, task, reviewerRole, pr, now) || validationFailedFor(reviewerRole) {
			continue
		}
		if reviewerPolicy == nil {
			uncovered[reviewerRole] = append(uncovered[reviewerRole], task.ID)
			continue
		}
		if matchIdleReviewer(state, task, reviewerRole, idle[reviewerRole], matched, reviewerPolicy, now) {
			continue
		}
		planner, ok := planners[reviewerRole]
		if !ok {
			planner = newReviewerStartPlanner(state, pr, repairCLI, reviewerRole, reservedIDs, now)
			planners[reviewerRole] = planner
		}
		if planner == nil {
			// The spawn path reports the CLI error; count the demand as usual.
			uncovered[reviewerRole] = append(uncovered[reviewerRole], task.ID)
			continue
		}
		agentID, ok := planner.assign(task, reviewerPolicy)
		if !ok {
			unservable[reviewerRole] = append(unservable[reviewerRole], task.ID)
			continue
		}
		uncovered[reviewerRole] = append(uncovered[reviewerRole], task.ID)
		plannedIDs[reviewerRole] = append(plannedIDs[reviewerRole], agentID)
	}

	maxResolver, _ := pr.(maxInstancesResolver)
	var missing []MissingRoleWork
	for _, role := range slices.Sorted(maps.Keys(uncovered)) {
		roleMax := 0
		if maxResolver != nil {
			roleMax, _ = maxResolver.MaxInstances(role)
		}
		headroom := models.EffectiveMaxInstances(roleMax, state.Config.MaxInstances) - occupied[role]
		taskIDs := uncovered[role]
		spawnCount := min(len(taskIDs), headroom)
		if spawnCount <= 0 {
			continue
		}
		sort.Strings(taskIDs)
		var agentIDs []string
		if ids := plannedIDs[role]; len(ids) > 0 {
			agentIDs = ids[:spawnCount]
		}
		missing = append(missing, MissingRoleWork{
			Role:       role,
			TaskIDs:    taskIDs,
			TaskCount:  len(taskIDs),
			SpawnCount: spawnCount,
			AgentIDs:   agentIDs,
		})
	}

	var notServable []UnservableRoleWork
	for _, role := range slices.Sorted(maps.Keys(unservable)) {
		taskIDs := unservable[role]
		sort.Strings(taskIDs)
		notServable = append(notServable, UnservableRoleWork{
			Role:    role,
			CLI:     planners[role].cli,
			TaskIDs: taskIDs,
			Reason:  "no reviewer this CLI could start would be allowed to claim these tasks (provider diversity or claim cooldown); they wait for an eligible reviewer",
		})
	}
	return missing, notServable
}

// matchIdleReviewer assigns task to the first unmatched idle reviewer that
// may claim it. Greedy matching can overestimate demand; the overshoot is
// bounded by max-instances and idle agents leave after their max-wait.
func matchIdleReviewer(state *models.State, task *models.Task, reviewerRole string, idle []string, matched map[string]bool, policy ops.ReviewerClaimPolicyResolver, now time.Time) bool {
	for _, agentID := range idle {
		if matched[agentID] {
			continue
		}
		if ops.ReviewerClaimEligible(ops.ReviewerClaimEligibilityInput{
			State: state, Task: task, AgentID: agentID,
			ReviewerRole: reviewerRole, Now: now, Resolver: policy,
		}) {
			matched[agentID] = true
			return true
		}
	}
	return false
}

// reviewerStartPlanner picks the IDs reviewers are started under. The
// launch path would auto-assign the first free <role>-N, which can be a prior
// approver's ID once that agent unregistered, so each start gets an explicit
// ID that the production claim filters accept for the task it is planned for.
// Candidates are evaluated in an isolated state projection holding one
// hypothetical reviewer registered the way a new one would be: the spawn CLI
// as provider and this process's PID for the live-process admission check.
type reviewerStartPlanner struct {
	projection *models.State
	role       string
	cli        string
	row        models.Agent
	taken      map[string]bool
	now        time.Time
}

func newReviewerStartPlanner(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reviewerRole string, reservedIDs map[string]bool, now time.Time) *reviewerStartPlanner {
	cliName, _, err := resolveRepairCLI(repairCLI, reviewerRole, state, pr)
	if err != nil {
		return nil
	}
	// Same occupancy as the launch-side allocator: IDs with an unexpired
	// lease, plus IDs of started agents still registering.
	taken := make(map[string]bool, len(state.Agents)+len(reservedIDs))
	for id, agentState := range state.Agents {
		if agentState.LeaseExpires != nil && agentState.LeaseExpires.After(now) {
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
	return &reviewerStartPlanner{
		projection: &projection,
		role:       reviewerRole,
		cli:        cliName,
		row: models.Agent{
			Role:         reviewerRole,
			Status:       models.AgentStatusIdle,
			Provider:     cliName,
			PID:          os.Getpid(),
			Heartbeat:    now,
			RegisteredAt: now,
			LeaseExpires: &leaseExpires,
		},
		taken: taken,
		now:   now,
	}
}

// assign returns the lowest free ID a new reviewer could claim task under
// and reserves it. Only IDs named by the task's approvals, history or
// validation records can be refused for the ID itself, so the search stops
// once every such ID could have been skipped; beyond that a refusal is about
// the provider.
func (p *reviewerStartPlanner) assign(task *models.Task, policy ops.ReviewerClaimPolicyResolver) (string, bool) {
	limit := len(p.taken) + len(task.Approvals) + len(task.History) + len(p.projection.ValidationReadiness) + 1
	for n := 1; n <= limit; n++ {
		agentID := fmt.Sprintf("%s-%d", p.role, n)
		if p.taken[agentID] {
			continue
		}
		previous, existed := p.projection.Agents[agentID]
		p.projection.Agents[agentID] = p.row
		eligible := ops.ReviewerClaimEligible(ops.ReviewerClaimEligibilityInput{
			State: p.projection, Task: task, AgentID: agentID,
			ReviewerRole: p.role, Now: p.now, Resolver: policy,
		})
		if existed {
			p.projection.Agents[agentID] = previous
		} else {
			delete(p.projection.Agents, agentID)
		}
		if eligible {
			p.taken[agentID] = true
			return agentID, true
		}
	}
	return "", false
}

func FindMissingRolesWithClaimableWork(state *models.State, pr models.PipelineResolver) []MissingRoleWork {
	if state == nil || pr == nil {
		return nil
	}

	registeredRoles := make(map[string]bool)
	registeredAgentsByRole := make(map[string][]string)
	now := time.Now().UTC()
	nilLeaseHeartbeatWindow := agentLivenessWindow(state.Config)
	for agentID, agentState := range state.Agents {
		if agentHasLiveRegistration(agentState, now, nilLeaseHeartbeatWindow) {
			if agentHealthIsCurrentDegraded(state.AgentHealth[agentID], agentState) {
				continue
			}
			registeredRoles[agentState.Role] = true
			registeredAgentsByRole[agentState.Role] = append(registeredAgentsByRole[agentState.Role], agentID)
		}
	}
	reviewerPolicy, _ := pr.(ops.ReviewerClaimPolicyResolver)

	missingRoleTasks := make(map[string][]string)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status.IsTerminal() || task.RolePair == "" {
			continue
		}

		doerRuntime, err := pr.DoerRole(task.RolePair)
		if err != nil {
			continue
		}
		reviewerRuntime, err := pr.ReviewerRole(task.RolePair)
		if err != nil {
			continue
		}

		if !registeredRoles[doerRuntime] && models.IsRoleTaskReady(state, task, doerRuntime, pr, now) {
			missingRoleTasks[doerRuntime] = append(missingRoleTasks[doerRuntime], task.ID)
		}
		if models.IsRoleTaskReady(state, task, reviewerRuntime, pr, now) {
			hasClaimEligibleReviewer := false
			hasValidationFailure := false
			for _, agentID := range registeredAgentsByRole[reviewerRuntime] {
				if !ops.HasValidClaimRegistration(state, agentID, reviewerRuntime) {
					continue
				}
				if models.ValidationTaskKnownFailed(state, task, agentID, now) {
					hasValidationFailure = true
					continue
				}
				if reviewerPolicy != nil && ops.ReviewerClaimEligible(ops.ReviewerClaimEligibilityInput{
					State: state, Task: task, AgentID: agentID,
					ReviewerRole: reviewerRuntime, Now: now, Resolver: reviewerPolicy,
				}) {
					hasClaimEligibleReviewer = true
					break
				}
			}
			if !hasClaimEligibleReviewer && !hasValidationFailure {
				missingRoleTasks[reviewerRuntime] = append(missingRoleTasks[reviewerRuntime], task.ID)
			}
		}
	}

	roles := make([]string, 0, len(missingRoleTasks))
	for role := range missingRoleTasks {
		roles = append(roles, role)
	}
	sort.Strings(roles)

	missing := make([]MissingRoleWork, 0, len(roles))
	for _, role := range roles {
		taskIDs := missingRoleTasks[role]
		sort.Strings(taskIDs)
		missing = append(missing, MissingRoleWork{
			Role:      role,
			TaskIDs:   taskIDs,
			TaskCount: len(taskIDs),
		})
	}
	return missing
}

// findMissingOrchestrator reports a running goal with no orchestrator holding
// effective ownership. It applies no grace: auto-repair gates it with
// orchestratorRepairDue, and a manual repair is deliberate. It re-evaluates
// the state RepairAgentPool just read, which narrows the window in which a
// watcher's older snapshot could start a second orchestrator; registration's
// singleton check remains the cross-process guarantee.
func findMissingOrchestrator(state *models.State, pr models.PipelineResolver, now time.Time) []MissingRoleWork {
	if state == nil || pr == nil {
		return nil
	}
	presence := evaluateOrchestratorPresence(state, pr, now)
	if !presence.Required || presence.Present {
		return nil
	}
	return []MissingRoleWork{orchestratorRoleWork(presence)}
}

// orchestratorRoleWork names the first orchestrator-type role; singleton
// ownership is by type, so one spawn restores the capacity.
func orchestratorRoleWork(presence orchestratorPresence) MissingRoleWork {
	return MissingRoleWork{
		Role:       presence.Roles[0],
		TaskIDs:    []string{},
		SpawnCount: 1,
		Reason:     "no live orchestrator while goal is IN_PROGRESS",
	}
}

func findValidationAgentCapacity(state *models.State, pr models.PipelineResolver, roles []string) []ValidationAgentCapacity {
	var result []ValidationAgentCapacity
	now := time.Now().UTC()
	window := agentLivenessWindow(state.Config)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if len(task.ValidationPrerequisites) == 0 {
			continue
		}
		for id, agent := range state.Agents {
			if len(roles) > 0 && !slices.Contains(roles, agent.Role) {
				continue
			}
			if !agentHasLiveRegistration(agent, now, window) || !models.IsRoleTaskReady(state, task, agent.Role, pr, now) {
				continue
			}
			entry := ValidationAgentCapacity{AgentID: id, Role: agent.Role, TaskID: task.ID, Status: "unverified", RecoverHint: "Let this session run fresh validation preflight; successful observations are not reusable launch authorization."}
			if record, ok := models.CurrentValidationReadiness(state, task, id, now); ok {
				entry.Status = record.Result
				entry.Code = record.Code
				if record.Result == "failed" {
					entry.RecoverHint = "Repair the named session prerequisite, then revalidate; unchanged failures retry after 60 seconds. Do not spawn an equivalent replacement context."
				}
			}
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TaskID != result[j].TaskID {
			return result[i].TaskID < result[j].TaskID
		}
		return result[i].AgentID < result[j].AgentID
	})
	return result
}

// agentLivenessWindow is how long a registration without a lease stays live on
// heartbeat alone. Shared so every reader of "is this agent live" — pool
// repair and stall diagnosis — agrees by construction rather than by three
// copies of the same expression.
func agentLivenessWindow(config models.Config) time.Duration {
	return models.AgentLivenessWindow(config)
}

func agentHasLiveRegistration(agentState models.Agent, now time.Time, nilLeaseHeartbeatWindow time.Duration) bool {
	return models.AgentRegistrationLive(agentState, now, nilLeaseHeartbeatWindow)
}

func agentHealthIsCurrentDegraded(health models.AgentHealth, agentState models.Agent) bool {
	return health.IsCurrentDegradedFor(agentState)
}

func agentHealthIsCurrentOrOrphanedDegraded(health models.AgentHealth, agentState models.Agent, hasAgent bool) bool {
	if health.State != models.AgentHealthDegraded {
		return false
	}
	if !hasAgent {
		return true
	}
	return health.IsCurrentDegradedFor(agentState)
}

func findCurrentDegradedAgentCapacity(state *models.State) []DegradedAgentCapacity {
	if state == nil || len(state.AgentHealth) == 0 {
		return nil
	}
	degraded := make([]DegradedAgentCapacity, 0, len(state.AgentHealth))
	for agentID, health := range state.AgentHealth {
		agentState, ok := state.Agents[agentID]
		if !agentHealthIsCurrentOrOrphanedDegraded(health, agentState, ok) {
			continue
		}
		degraded = append(degraded, DegradedAgentCapacity{
			AgentID:     agentID,
			Role:        health.Role,
			Reason:      health.Reason,
			LastError:   health.LastError,
			RecoverHint: health.RecoverHint,
		})
	}
	sort.Slice(degraded, func(i, j int) bool {
		return degraded[i].AgentID < degraded[j].AgentID
	})
	return degraded
}

func printRepairAgentPoolResult(result *RepairAgentPoolResult) {
	if len(result.Missing) == 0 && len(result.Degraded) == 0 && len(result.Validation) == 0 && len(result.Unservable) == 0 {
		fmt.Println("No role needs more agents for claimable work.")
		return
	}
	if len(result.Validation) > 0 {
		fmt.Println("Task validation capacity (last observation):")
		for _, entry := range result.Validation {
			fmt.Printf("  %s (%s), task %s: %s %s\n    hint: %s\n", entry.AgentID, entry.Role, entry.TaskID, entry.Status, entry.Code, entry.RecoverHint)
		}
	}

	if len(result.Degraded) > 0 {
		fmt.Println("Degraded agent capacity:")
		for _, degraded := range result.Degraded {
			fmt.Printf("  %s (%s): %s\n", degraded.AgentID, degraded.Role, degraded.Reason)
			if degraded.RecoverHint != "" {
				fmt.Printf("    hint: %s\n", degraded.RecoverHint)
			}
		}
		if len(result.Missing) > 0 {
			fmt.Println()
		}
	}

	if len(result.Unservable) > 0 {
		fmt.Println("Reviewer work a new agent could not claim (not started):")
		for _, work := range result.Unservable {
			fmt.Printf("  %s with --cli %s: %d task(s) (%s)\n    hint: %s\n", work.Role, work.CLI, len(work.TaskIDs), strings.Join(work.TaskIDs, ", "), work.Reason)
		}
		if len(result.Missing) > 0 {
			fmt.Println()
		}
	}

	if len(result.Missing) == 0 {
		return
	}

	printedHeading := false
	for _, roleWork := range result.Missing {
		if roleWork.Reason != "" {
			fmt.Printf("Missing orchestrator: %s (%s)\n", roleWork.Role, roleWork.Reason)
			continue
		}
		if !printedHeading {
			fmt.Println("Roles with claimable work not covered by idle agents:")
			printedHeading = true
		}
		fmt.Printf("  %s: %d task(s) (%s); starting %d agent(s)\n", roleWork.Role, roleWork.TaskCount, strings.Join(roleWork.TaskIDs, ", "), roleWork.SpawnCount)
	}

	if result.DryRun {
		fmt.Println()
		fmt.Println("Dry run; would spawn:")
		for _, command := range result.Commands {
			fmt.Printf("  %s\n", command)
		}
		return
	}

	fmt.Println()
	if len(result.Spawned) > 0 {
		fmt.Println("Started agent processes:")
		for _, spawned := range result.Spawned {
			if spawned.PID != 0 {
				fmt.Printf("  %s (pid %d)\n", spawned.Command, spawned.PID)
				continue
			}
			fmt.Printf("  %s\n", spawned.Command)
		}
	}
	if len(result.Failed) > 0 {
		fmt.Println("Failed to start agent processes:")
		for _, failed := range result.Failed {
			fmt.Printf("  %s: %s\n", failed.Command, failed.Error)
		}
	}
}
