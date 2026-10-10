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
	"github.com/liza-mas/liza/internal/rolemodels"
)

type RepairAgentPoolOptions struct {
	ProjectRoot string
	Missing     bool
	CLI         string
	DryRun      bool
	Roles       []string
	// PendingSpawns lists, per role, started agent processes that have not
	// registered yet, each as the models.yaml list item it was started with
	// (0 for none); they already cover demand and consume headroom.
	PendingSpawns map[string][]int
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
	// Items, when set, are the 1-based models.yaml list items to start those
	// reviewers with, parallel to AgentIDs: the entries bound to the review
	// slots they are planned for.
	Items []int `json:"models_items,omitempty"`
	// CLI is the CLI the role's starts run; empty when they run list items
	// on different CLIs (each start then records its own).
	CLI string `json:"cli,omitempty"`
	// Reason explains demand that does not come from claimable tasks.
	Reason string `json:"reason,omitempty"`

	// headroom and the uncut plan let pending starts cover the item demand
	// they were started for before the plan is cut to SpawnCount.
	headroom     int
	plannedIDs   []string
	plannedItems []int
}

// UnservableRoleWork is reviewer demand that an agent started with CLI could
// not claim, so starting one would only fill capacity.
type UnservableRoleWork struct {
	Role string `json:"role"`
	CLI  string `json:"cli"`
	// Item is the 1-based models.yaml list item the reviewer would start with.
	Item    int      `json:"models_item,omitempty"`
	TaskIDs []string `json:"task_ids"`
	Reason  string   `json:"reason"`
}

// startedWith names how a reviewer for this work would be started.
func (w UnservableRoleWork) startedWith() string {
	return startedWith(w.CLI, w.Item)
}

// startedWith names how an agent starting a list item (0 for none) on cli
// would be started.
func startedWith(cli string, item int) string {
	if item > 0 {
		return fmt.Sprintf("%s item %d (%s)", paths.ModelsFileName, item, cli)
	}
	return "--cli " + cli
}

// ProviderBlock reports whether starting an agent on cli is blocked by a
// provider quota signal, and until when. A nil ProviderBlock blocks nothing.
type ProviderBlock func(cli string) (until time.Time, blocked bool)

// QuotaProviderBlock blocks the CLIs whose quota signal is set in projectRoot.
func QuotaProviderBlock(projectRoot string) ProviderBlock {
	return func(cli string) (time.Time, bool) {
		return agent.QuotaBlockedUntil(projectRoot, cli)
	}
}

// ProviderBlockedRoleWork is claimable work that pool repair does not start an
// agent for because the CLI the start would run is quota-blocked. Repair has
// no fallback provider (ADR-0167), so the role has no usable provider until
// then.
type ProviderBlockedRoleWork struct {
	Role string `json:"role"`
	CLI  string `json:"cli"`
	// Item is the 1-based models.yaml list item the start would use.
	Item int `json:"models_item,omitempty"`
	// Until is zero when the signal cannot be read: blocked, expiry unknown.
	Until   time.Time `json:"until"`
	TaskIDs []string  `json:"task_ids"`
	// Reason explains demand that does not come from claimable tasks.
	Reason string `json:"reason,omitempty"`
}

// describe returns what is waiting and the remedy, for alerts and output.
func (w ProviderBlockedRoleWork) describe() (summary, hint string) {
	demand := fmt.Sprintf("%d claimable task(s) (%s)", len(w.TaskIDs), strings.Join(w.TaskIDs, ", "))
	if w.Reason != "" {
		demand = w.Reason
	}
	until := "an unknown time (its signal file cannot be read)"
	if !w.Until.IsZero() {
		until = w.Until.UTC().Format(time.RFC3339)
	}
	summary = fmt.Sprintf("%s: %s and no usable provider: %s is quota-exhausted until %s",
		w.Role, demand, startedWith(w.CLI, w.Item), until)
	remedy := fmt.Sprintf("change the role's CLI in %s or start one with `%s`", paths.ModelsFileName, brand.Command("agent", w.Role, "--cli", "<other-cli>"))
	if w.Item > 0 {
		remedy = fmt.Sprintf("change item %d in %s", w.Item, paths.ModelsFileName)
	}
	hint = fmt.Sprintf("auto repair starts agents after that; to staff earlier, %s, or delete %s once %s has capacity again",
		remedy, agent.QuotaSignalFile(w.CLI), w.CLI)
	return summary, hint
}

type SpawnedAgent struct {
	Role    string `json:"role"`
	AgentID string `json:"agent_id,omitempty"`
	CLI     string `json:"cli"`
	Item    int    `json:"models_item,omitempty"`
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
	CLI        string               `json:"cli"`
	RoleCLIs   map[string]string    `json:"role_clis,omitempty"`
	DryRun     bool                 `json:"dry_run"`
	Missing    []MissingRoleWork    `json:"missing"`
	Unservable []UnservableRoleWork `json:"unservable,omitempty"`
	// ProviderBlocked is work not started because its CLI is quota-blocked.
	ProviderBlocked []ProviderBlockedRoleWork `json:"provider_blocked,omitempty"`
	Degraded        []DegradedAgentCapacity   `json:"degraded,omitempty"`
	Spawned         []SpawnedAgent            `json:"spawned,omitempty"`
	Failed          []FailedAgentSpawn        `json:"failed,omitempty"`
	Commands        []string                  `json:"commands,omitempty"`
	Validation      []ValidationAgentCapacity `json:"validation,omitempty"`
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
// resolves its models.yaml entry, model included, itself; a non-zero
// modelsItem picks that item of the role's entry list.
var repairAgentPoolSpawn = func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error) {
	var extraArgs []string
	if agentID != "" {
		extraArgs = []string{"--agent-id", agentID}
	}
	if modelsItem > 0 {
		extraArgs = append(extraArgs, "--models-item", strconv.Itoa(modelsItem))
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
func SetRepairAgentPoolSpawnForTest(spawn func(projectRoot, role, cli, agentID string, cliFromConfig bool, modelsItem int) (int, error)) func() {
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

	roleModels, err := agent.LoadValidatedRoleModels(opts.ProjectRoot, agent.RoleTypesOf(pr), state.Config)
	if err != nil {
		return nil, err
	}
	repairCLI := RepairCLI{Explicit: opts.CLI, RoleModels: roleModels}

	now := time.Now().UTC()
	blocked := QuotaProviderBlock(opts.ProjectRoot)
	capacity := FindRoleCapacity(state, pr, repairCLI, opts.PendingAgentIDs, blocked, now)
	missing, providerBlocked := capacity.Missing, capacity.ProviderBlocked
	for _, roleWork := range findMissingOrchestrator(state, pr, now) {
		if work, isBlocked := blockedOrchestratorWork(roleWork, repairCLI, state, pr, blocked); isBlocked {
			providerBlocked = append(providerBlocked, work)
			continue
		}
		missing = append(missing, roleWork)
	}
	missing = subtractPendingSpawns(missing, opts.PendingSpawns)
	missing = filterMissingRoleWork(missing, opts.Roles)
	result := &RepairAgentPoolResult{
		CLI:        opts.CLI,
		DryRun:     opts.DryRun,
		Missing:    missing,
		Unservable: filterUnservableRoleWork(capacity.Unservable, opts.Roles),
		ProviderBlocked: slices.DeleteFunc(providerBlocked, func(work ProviderBlockedRoleWork) bool {
			return len(opts.Roles) > 0 && !slices.Contains(opts.Roles, work.Role)
		}),
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
		slots, _ := roleModels.ReviewSlots(roleWork.Role)
		starts := make([]SpawnedAgent, 0, roleWork.SpawnCount)
		for i := range roleWork.SpawnCount {
			start := SpawnedAgent{Role: roleWork.Role, CLI: cliName, Command: brand.Command("agent", roleWork.Role)}
			if i < len(roleWork.Items) {
				start.Item = roleWork.Items[i]
			}
			switch {
			case start.Item > 0:
				start.CLI = slots.Items[start.Item-1].CLI
				start.Command += " --models-item " + strconv.Itoa(start.Item)
			case spawnCLI != "":
				start.Command += " --cli " + spawnCLI
			}
			if i < len(roleWork.AgentIDs) {
				start.AgentID = roleWork.AgentIDs[i]
				start.Command += " --agent-id " + start.AgentID
			}
			starts = append(starts, start)
			result.Commands = append(result.Commands, start.Command)
		}
		roleCLI := cliName
		for _, start := range starts {
			if start.CLI != starts[0].CLI {
				roleCLI = ""
				break
			}
			roleCLI = start.CLI
		}
		result.Missing[i].CLI = roleCLI
		if opts.CLI == "" {
			if roleCLI != "" {
				if result.RoleCLIs == nil {
					result.RoleCLIs = make(map[string]string)
				}
				result.RoleCLIs[roleWork.Role] = roleCLI
			}
			switch {
			case roleCLI == "" || heterogeneousImplicitCLI || (commonImplicitCLI != "" && commonImplicitCLI != roleCLI):
				commonImplicitCLI = ""
				heterogeneousImplicitCLI = true
			default:
				commonImplicitCLI = roleCLI
			}
		}
		if opts.DryRun {
			continue
		}

		failedItems := make(map[int]bool)
		for _, start := range starts {
			if failedItems[start.Item] {
				continue
			}
			pid, err := repairAgentPoolSpawn(opts.ProjectRoot, roleWork.Role, start.CLI, start.AgentID, spawnCLI == "", start.Item)
			if err != nil {
				// The next start of the same selection would fail the same
				// way; another list item may still start.
				result.Failed = append(result.Failed, FailedAgentSpawn{
					Role:    roleWork.Role,
					CLI:     start.CLI,
					Command: start.Command,
					Error:   err.Error(),
				})
				failedItems[start.Item] = true
				continue
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
	capacity := FindRoleCapacity(state, pr, repairCLI, reservedIDs, nil, now)
	return capacity.Missing, capacity.Unservable
}

// RoleCapacity is pool repair's view of the claimable work idle agents do not
// cover.
type RoleCapacity struct {
	Missing         []MissingRoleWork
	Unservable      []UnservableRoleWork
	ProviderBlocked []ProviderBlockedRoleWork
}

// FindRoleCapacity is FindRoleCapacityDeficits that also sets aside the work
// whose start would run a CLI blocked reports blocked. That work is classified
// per role and list item before any agent ID is reserved or headroom is
// counted, so a blocked item never takes a start another item could use.
func FindRoleCapacity(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reservedIDs map[string]bool, blocked ProviderBlock, now time.Time) RoleCapacity {
	if state == nil || pr == nil {
		return RoleCapacity{}
	}

	window := agentLivenessWindow(state.Config)
	occupied := make(map[string]int)
	idle := make(map[string][]string)
	usable := make(map[string][]string)
	for _, agentID := range slices.Sorted(maps.Keys(state.Agents)) {
		agentState := state.Agents[agentID]
		if ops.AgentProcessOwnership(agentID, agentState, now, window).Occupied() {
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
	plannedItems := make(map[string][]int)
	matched := make(map[string]bool)
	idleDoersUsed := make(map[string]int)
	uncovered := make(map[string][]string)
	type startKind struct {
		role string
		item int
	}
	unservable := make(map[startKind][]string)
	unservableCLI := make(map[startKind]string)
	blockedTasks := make(map[startKind][]string)
	blockedStarts := make(map[startKind]ProviderBlockedRoleWork)
	roleCLIs := make(map[string]string)
	// setAside records task under kind when a start on cli is blocked.
	setAside := func(kind startKind, cli, taskID string) bool {
		if blocked == nil || cli == "" {
			return false
		}
		until, isBlocked := blocked(cli)
		if !isBlocked {
			return false
		}
		blockedTasks[kind] = append(blockedTasks[kind], taskID)
		blockedStarts[kind] = ProviderBlockedRoleWork{Role: kind.role, CLI: cli, Item: kind.item, Until: until}
		return true
	}
	// roleCLI is the CLI a role's starts run when no list item picks one; a
	// resolution error is left to the spawn path to report.
	roleCLI := func(role string) string {
		cli, ok := roleCLIs[role]
		if !ok {
			cli, _, _ = resolveRepairCLI(repairCLI, role, state, pr)
			roleCLIs[role] = cli
		}
		return cli
	}

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
			switch {
			case idleDoersUsed[doerRole] < len(idle[doerRole]):
				idleDoersUsed[doerRole]++
			case setAside(startKind{doerRole, 0}, roleCLI(doerRole), task.ID):
			default:
				uncovered[doerRole] = append(uncovered[doerRole], task.ID)
			}
		}

		if !models.IsRoleTaskReady(state, task, reviewerRole, pr, now) || validationFailedFor(reviewerRole) {
			continue
		}
		if reviewerPolicy == nil {
			if !setAside(startKind{reviewerRole, 0}, roleCLI(reviewerRole), task.ID) {
				uncovered[reviewerRole] = append(uncovered[reviewerRole], task.ID)
			}
			continue
		}
		slots, _ := repairCLI.RoleModels.ReviewSlots(reviewerRole)
		if matchIdleReviewer(state, task, reviewerRole, slots, idle[reviewerRole], matched, reviewerPolicy, now) {
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
		if item := planner.itemFor(task); setAside(startKind{reviewerRole, item}, planner.cliFor(item), task.ID) {
			continue
		}
		agentID, item, ok := planner.assign(task, reviewerPolicy)
		if !ok {
			kind := startKind{reviewerRole, item}
			unservable[kind] = append(unservable[kind], task.ID)
			unservableCLI[kind] = planner.cliFor(item)
			continue
		}
		uncovered[reviewerRole] = append(uncovered[reviewerRole], task.ID)
		plannedIDs[reviewerRole] = append(plannedIDs[reviewerRole], agentID)
		if item > 0 {
			plannedItems[reviewerRole] = append(plannedItems[reviewerRole], item)
		}
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
		var items []int
		if planned := plannedItems[role]; len(planned) > 0 {
			items = planned[:spawnCount]
		}
		missing = append(missing, MissingRoleWork{
			Role:         role,
			TaskIDs:      taskIDs,
			TaskCount:    len(taskIDs),
			SpawnCount:   spawnCount,
			AgentIDs:     agentIDs,
			Items:        items,
			headroom:     headroom,
			plannedIDs:   plannedIDs[role],
			plannedItems: plannedItems[role],
		})
	}

	byRoleThenItem := func(a, b startKind) int {
		if c := strings.Compare(a.role, b.role); c != 0 {
			return c
		}
		return a.item - b.item
	}
	var notServable []UnservableRoleWork
	for _, kind := range slices.SortedFunc(maps.Keys(unservable), byRoleThenItem) {
		taskIDs := unservable[kind]
		sort.Strings(taskIDs)
		notServable = append(notServable, UnservableRoleWork{
			Role:    kind.role,
			CLI:     unservableCLI[kind],
			Item:    kind.item,
			TaskIDs: taskIDs,
			Reason:  "no reviewer this CLI could start would be allowed to claim these tasks (provider diversity, claim cooldown or " + paths.ModelsFileName + " slot); they wait for an eligible reviewer",
		})
	}
	var providerBlocked []ProviderBlockedRoleWork
	for _, kind := range slices.SortedFunc(maps.Keys(blockedStarts), byRoleThenItem) {
		work := blockedStarts[kind]
		work.TaskIDs = blockedTasks[kind]
		sort.Strings(work.TaskIDs)
		providerBlocked = append(providerBlocked, work)
	}
	return RoleCapacity{Missing: missing, Unservable: notServable, ProviderBlocked: providerBlocked}
}

// blockedOrchestratorWork reports a missing orchestrator whose start CLI is
// blocked. Its start takes no headroom, so it is checked on its own.
func blockedOrchestratorWork(roleWork MissingRoleWork, repairCLI RepairCLI, state *models.State, pr models.PipelineResolver, blocked ProviderBlock) (ProviderBlockedRoleWork, bool) {
	if blocked == nil {
		return ProviderBlockedRoleWork{}, false
	}
	cli, _, err := resolveRepairCLI(repairCLI, roleWork.Role, state, pr)
	if err != nil {
		return ProviderBlockedRoleWork{}, false // the spawn path reports it
	}
	until, isBlocked := blocked(cli)
	if !isBlocked {
		return ProviderBlockedRoleWork{}, false
	}
	return ProviderBlockedRoleWork{Role: roleWork.Role, CLI: cli, Until: until, TaskIDs: roleWork.TaskIDs, Reason: roleWork.Reason}, true
}

// matchIdleReviewer assigns task to the first unmatched idle reviewer that
// may claim it. Greedy matching can overestimate demand; the overshoot is
// bounded by max-instances and idle agents leave after their max-wait.
func matchIdleReviewer(state *models.State, task *models.Task, reviewerRole string, slots rolemodels.Selection, idle []string, matched map[string]bool, policy ops.ReviewerClaimPolicyResolver, now time.Time) bool {
	for _, agentID := range idle {
		if matched[agentID] {
			continue
		}
		if ops.ReviewerClaimEligible(ops.ReviewerClaimEligibilityInput{
			State: state, Task: task, AgentID: agentID,
			ReviewerRole: reviewerRole, Now: now, Resolver: policy, Slots: slots,
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

// FindMissingRolesWithClaimableWork reports roles whose claimable work no live
// agent may claim; roleModels supplies the reviewer lists that bind claims.
func FindMissingRolesWithClaimableWork(state *models.State, pr models.PipelineResolver, roleModels rolemodels.File) []MissingRoleWork {
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
			slots, _ := roleModels.ReviewSlots(reviewerRuntime)
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
					ReviewerRole: reviewerRuntime, Now: now, Resolver: reviewerPolicy, Slots: slots,
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
	if len(result.Missing) == 0 && len(result.Degraded) == 0 && len(result.Validation) == 0 && len(result.Unservable) == 0 && len(result.ProviderBlocked) == 0 {
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
			fmt.Printf("  %s with %s: %d task(s) (%s)\n    hint: %s\n", work.Role, work.startedWith(), len(work.TaskIDs), strings.Join(work.TaskIDs, ", "), work.Reason)
		}
		if len(result.Missing) > 0 {
			fmt.Println()
		}
	}

	if len(result.ProviderBlocked) > 0 {
		fmt.Println("Roles with claimable work and no usable provider (not started):")
		for _, work := range result.ProviderBlocked {
			summary, hint := work.describe()
			fmt.Printf("  %s\n    hint: %s\n", summary, hint)
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
