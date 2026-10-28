package commands

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/agentlaunch"
	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/providersignal"
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
	return agentFlagCLI + " " + cli
}

// ProviderBlock reports whether starting an agent on cli is blocked by a
// provider quota signal, and until when. A nil ProviderBlock blocks nothing.
type ProviderBlock func(cli string) (until time.Time, blocked bool)

// QuotaProviderBlock blocks the CLIs whose quota signal is set in projectRoot.
func QuotaProviderBlock(projectRoot string) ProviderBlock {
	return func(cli string) (time.Time, bool) {
		return providersignal.QuotaBlockedUntil(projectRoot, cli)
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
		remedy, providersignal.QuotaSignalFile(w.CLI), w.CLI)
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

var EnvAutoRepairAgentPool = brand.EnvName("AUTO_REPAIR_AGENT_POOL")

func AutoRepairAgentPoolEnabledFromEnv() (bool, string) {
	value, ok := os.LookupEnv(EnvAutoRepairAgentPool)
	return parseAutoRepairAgentPoolEnv(value, ok)
}

// envBooleanNo is the spelling of false that strconv.ParseBool does not accept
// but the auto-repair switch does.
const envBooleanNo = "no"

func parseAutoRepairAgentPoolEnv(value string, ok bool) (bool, string) {
	if !ok {
		return true, ""
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true, ""
	}
	if strings.EqualFold(trimmed, envBooleanNo) {
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

// repairInputs is the state and configuration one pool repair evaluates.
type repairInputs struct {
	state     *models.State
	pr        models.PipelineResolver
	repairCLI RepairCLI
}

func loadRepairInputs(opts RepairAgentPoolOptions) (repairInputs, error) {
	statePath := paths.New(opts.ProjectRoot).StatePath()
	// Observation only: each start registers under the lock, where max-instances
	// is enforced, so a snapshot need not wait behind writers.
	state, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return repairInputs{}, fmt.Errorf("failed to read state: %w", err)
	}

	pr, err := ops.LoadResolverForModels(opts.ProjectRoot)
	if err != nil {
		return repairInputs{}, fmt.Errorf("failed to load pipeline resolver: %w", err)
	}

	availableCLIs := agentlaunch.AvailableCLIs(state.Config)
	if opts.CLI != "" && !slices.Contains(availableCLIs, opts.CLI) {
		return repairInputs{}, fmt.Errorf("invalid CLI: %s (must be %s)", opts.CLI, strings.Join(availableCLIs, ", "))
	}

	roleModels, err := agentlaunch.LoadValidatedRoleModels(opts.ProjectRoot, agentlaunch.RoleTypesOf(pr), state.Config)
	if err != nil {
		return repairInputs{}, err
	}
	return repairInputs{state: state, pr: pr, repairCLI: RepairCLI{Explicit: opts.CLI, RoleModels: roleModels}}, nil
}

// assessRepairDemand evaluates the capacity observations and the work not
// covered into a result with nothing started yet.
func assessRepairDemand(opts RepairAgentPoolOptions, in repairInputs) *RepairAgentPoolResult {
	now := time.Now().UTC()
	blocked := QuotaProviderBlock(opts.ProjectRoot)
	capacity := FindRoleCapacity(in.state, in.pr, in.repairCLI, opts.PendingAgentIDs, blocked, now)
	missing, providerBlocked := capacity.Missing, capacity.ProviderBlocked
	for _, roleWork := range findMissingOrchestrator(in.state, in.pr, now) {
		if work, isBlocked := blockedOrchestratorWork(roleWork, in.repairCLI, in.state, in.pr, blocked); isBlocked {
			providerBlocked = append(providerBlocked, work)
			continue
		}
		missing = append(missing, roleWork)
	}
	missing = subtractPendingSpawns(missing, opts.PendingSpawns)
	missing = filterMissingRoleWork(missing, opts.Roles)
	return &RepairAgentPoolResult{
		CLI:        opts.CLI,
		DryRun:     opts.DryRun,
		Missing:    missing,
		Unservable: filterUnservableRoleWork(capacity.Unservable, opts.Roles),
		ProviderBlocked: slices.DeleteFunc(providerBlocked, func(work ProviderBlockedRoleWork) bool {
			return len(opts.Roles) > 0 && !slices.Contains(opts.Roles, work.Role)
		}),
		Degraded:   findCurrentDegradedAgentCapacity(in.state),
		Validation: findValidationAgentCapacity(in.state, in.pr, opts.Roles),
	}
}

func RepairAgentPool(opts RepairAgentPoolOptions) (*RepairAgentPoolResult, error) {
	in, err := loadRepairInputs(opts)
	if err != nil {
		return nil, err
	}
	result := assessRepairDemand(opts, in)

	var implicit implicitRoleCLIs
	for i, roleWork := range result.Missing {
		plan, err := planRoleStarts(in.repairCLI, roleWork, in.state, in.pr)
		if err != nil {
			return nil, err
		}
		for _, start := range plan.starts {
			result.Commands = append(result.Commands, start.Command)
		}
		result.Missing[i].CLI = plan.cli
		if opts.CLI == "" {
			result.recordRoleCLI(roleWork.Role, plan.cli)
			implicit.observe(plan.cli)
		}
		if opts.DryRun {
			continue
		}
		executeRoleStarts(opts.ProjectRoot, roleWork.Role, plan, result)
	}

	if opts.CLI == "" && !implicit.heterogeneous {
		result.CLI = implicit.common
	}

	if len(result.Failed) > 0 {
		return result, repairAgentPoolSpawnError(result)
	}

	return result, nil
}

// recordRoleCLI notes the CLI role's starts run, when one CLI runs them all.
func (r *RepairAgentPoolResult) recordRoleCLI(role, cli string) {
	if cli == "" {
		return
	}
	if r.RoleCLIs == nil {
		r.RoleCLIs = make(map[string]string)
	}
	r.RoleCLIs[role] = cli
}

// implicitRoleCLIs tracks the one CLI every role starts on when --cli chose
// none; it stops being common as soon as a role mixes CLIs or two roles differ.
type implicitRoleCLIs struct {
	common        string
	heterogeneous bool
}

func (c *implicitRoleCLIs) observe(roleCLI string) {
	switch {
	case roleCLI == "" || c.heterogeneous || (c.common != "" && c.common != roleCLI):
		c.common = ""
		c.heterogeneous = true
	default:
		c.common = roleCLI
	}
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
