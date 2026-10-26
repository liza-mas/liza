package commands

import (
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/ops"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/rolemodels"
)

// Why a role has demand that no claimable task explains. They are shown in the
// plan and the rendered output.
const (
	orchestratorDemandReason = "no live orchestrator while goal is IN_PROGRESS"
	mergeOwnerDemandReason   = "approved task(s) with no live, healthy merge owner"
)

// The validation status of an agent with no recorded readiness, and the
// readiness result that needs repair rather than a replacement process
// (models.ValidationReadiness.Result).
const (
	validationCapacityUnverified = "unverified"
	validationReadinessFailed    = "failed"
)

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
// Approved merges no eligible reviewer owns need one reviewer of their role,
// unless a review start is already planned for it. reservedIDs are explicit
// IDs of started agents not registered yet.
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

// FindMissingRolesWithClaimableWork reports roles whose claimable work no live
// agent may claim, and reviewer roles with approved merges no eligible reviewer
// owns; roleModels supplies the reviewer lists that bind claims.
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
		if models.ApprovedMergeOutstanding(task, pr) && models.ApprovedMergeOwner(state, task, pr, now) == "" {
			missingRoleTasks[reviewerRuntime] = append(missingRoleTasks[reviewerRuntime], task.ID)
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
		Reason:     orchestratorDemandReason,
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
			entry := ValidationAgentCapacity{AgentID: id, Role: agent.Role, TaskID: task.ID, Status: validationCapacityUnverified, RecoverHint: "Let this session run fresh validation preflight; successful observations are not reusable launch authorization."}
			if record, ok := models.CurrentValidationReadiness(state, task, id, now); ok {
				entry.Status = record.Result
				entry.Code = record.Code
				if record.Result == validationReadinessFailed {
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

// FindRoleCapacity is FindRoleCapacityDeficits that also sets aside the work
// whose start would run a CLI blocked reports blocked. That work is classified
// per role and list item before any agent ID is reserved or headroom is
// counted, so a blocked item never takes a start another item could use.
func FindRoleCapacity(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reservedIDs map[string]bool, blocked ProviderBlock, now time.Time) RoleCapacity {
	if state == nil || pr == nil {
		return RoleCapacity{}
	}
	demand := newCapacityDemand(state, pr, repairCLI, reservedIDs, blocked, now)
	demand.scanRegistrations()
	for i := range state.Tasks {
		demand.classifyTask(&state.Tasks[i])
	}
	mergeStart := demand.ownerlessMergeStarts()
	return RoleCapacity{
		Missing:         demand.missingRoleWork(mergeStart),
		Unservable:      demand.unservableRoleWork(),
		ProviderBlocked: demand.providerBlockedRoleWork(),
	}
}

// startKind identifies one kind of start: a role and the models.yaml list
// item it runs (0 for none).
type startKind struct {
	role string
	item int
}

func compareStartKinds(a, b startKind) int {
	if c := strings.Compare(a.role, b.role); c != 0 {
		return c
	}
	return a.item - b.item
}

// capacityDemand accumulates, per role and start kind, the claimable work idle
// agents do not cover while FindRoleCapacity walks the registrations and tasks.
type capacityDemand struct {
	state       *models.State
	pr          models.PipelineResolver
	repairCLI   RepairCLI
	reservedIDs map[string]bool
	blocked     ProviderBlock
	now         time.Time

	// Registrations by role: slots occupied, idle claim-valid agents, and all
	// claim-valid agents.
	occupied map[string]int
	idle     map[string][]string
	usable   map[string][]string

	reviewerPolicy ops.ReviewerClaimPolicyResolver
	planners       map[string]*reviewerStartPlanner
	plannedIDs     map[string][]string
	plannedItems   map[string][]int
	matched        map[string]bool
	idleDoersUsed  map[string]int
	uncovered      map[string][]string
	ownerless      map[string][]string
	unservable     map[startKind][]string
	unservableCLI  map[startKind]string
	blockedTasks   map[startKind][]string
	blockedStarts  map[startKind]ProviderBlockedRoleWork
	roleCLIs       map[string]string
}

func newCapacityDemand(state *models.State, pr models.PipelineResolver, repairCLI RepairCLI, reservedIDs map[string]bool, blocked ProviderBlock, now time.Time) *capacityDemand {
	reviewerPolicy, _ := pr.(ops.ReviewerClaimPolicyResolver)
	return &capacityDemand{
		state:          state,
		pr:             pr,
		repairCLI:      repairCLI,
		reservedIDs:    reservedIDs,
		blocked:        blocked,
		now:            now,
		occupied:       make(map[string]int),
		idle:           make(map[string][]string),
		usable:         make(map[string][]string),
		reviewerPolicy: reviewerPolicy,
		planners:       make(map[string]*reviewerStartPlanner),
		plannedIDs:     make(map[string][]string),
		plannedItems:   make(map[string][]int),
		matched:        make(map[string]bool),
		idleDoersUsed:  make(map[string]int),
		uncovered:      make(map[string][]string),
		ownerless:      make(map[string][]string),
		unservable:     make(map[startKind][]string),
		unservableCLI:  make(map[startKind]string),
		blockedTasks:   make(map[startKind][]string),
		blockedStarts:  make(map[startKind]ProviderBlockedRoleWork),
		roleCLIs:       make(map[string]string),
	}
}

// scanRegistrations counts the slots each role occupies and which of its
// agents can claim.
func (d *capacityDemand) scanRegistrations() {
	window := agentLivenessWindow(d.state.Config)
	for _, agentID := range slices.Sorted(maps.Keys(d.state.Agents)) {
		agentState := d.state.Agents[agentID]
		if ops.AgentProcessOwnership(agentID, agentState, d.now, window).Occupied() {
			d.occupied[agentState.Role]++
		}
		if !agentHasLiveRegistration(agentState, d.now, window) ||
			agentHealthIsCurrentDegraded(d.state.AgentHealth[agentID], agentState) ||
			!ops.HasValidClaimRegistration(d.state, agentID, agentState.Role) {
			continue
		}
		d.usable[agentState.Role] = append(d.usable[agentState.Role], agentID)
		// Holding no task is what frees an agent to claim; lifecycle status
		// is not consulted.
		if agentState.CurrentTask == nil || *agentState.CurrentTask == "" {
			d.idle[agentState.Role] = append(d.idle[agentState.Role], agentID)
		}
	}
}

// setAside records task under kind when a start on cli is blocked.
func (d *capacityDemand) setAside(kind startKind, cli, taskID string) bool {
	if d.blocked == nil || cli == "" {
		return false
	}
	until, isBlocked := d.blocked(cli)
	if !isBlocked {
		return false
	}
	d.blockedTasks[kind] = append(d.blockedTasks[kind], taskID)
	d.blockedStarts[kind] = ProviderBlockedRoleWork{Role: kind.role, CLI: cli, Item: kind.item, Until: until}
	return true
}

// roleCLI is the CLI a role's starts run when no list item picks one; a
// resolution error is left to the spawn path to report.
func (d *capacityDemand) roleCLI(role string) string {
	cli, ok := d.roleCLIs[role]
	if !ok {
		cli, _, _ = resolveRepairCLI(d.repairCLI, role, d.state, d.pr)
		d.roleCLIs[role] = cli
	}
	return cli
}

// validationFailedFor reports whether a usable agent of role failed task's
// validation: it needs repair and revalidation, not an equivalent replacement
// process.
func (d *capacityDemand) validationFailedFor(task *models.Task, role string) bool {
	return slices.ContainsFunc(d.usable[role], func(agentID string) bool {
		return models.ValidationTaskKnownFailed(d.state, task, agentID, d.now)
	})
}

// classifyTask adds the demand one task makes on its doer and reviewer roles.
func (d *capacityDemand) classifyTask(task *models.Task) {
	if task.Status.IsTerminal() || task.RolePair == "" {
		return
	}
	doerRole, err := d.pr.DoerRole(task.RolePair)
	if err != nil {
		return
	}
	reviewerRole, err := d.pr.ReviewerRole(task.RolePair)
	if err != nil {
		return
	}
	// An approved merge no eligible reviewer owns is work for any reviewer
	// of the role, outside review-claim matching: whoever starts first
	// becomes its owner.
	if models.ApprovedMergeOutstanding(task, d.pr) && models.ApprovedMergeOwner(d.state, task, d.pr, d.now) == "" {
		d.ownerless[reviewerRole] = append(d.ownerless[reviewerRole], task.ID)
	}

	if models.IsRoleTaskReady(d.state, task, doerRole, d.pr, d.now) && !d.validationFailedFor(task, doerRole) {
		d.classifyDoerDemand(task, doerRole)
	}
	if !models.IsRoleTaskReady(d.state, task, reviewerRole, d.pr, d.now) || d.validationFailedFor(task, reviewerRole) {
		return
	}
	d.classifyReviewerDemand(task, reviewerRole)
}

func (d *capacityDemand) classifyDoerDemand(task *models.Task, doerRole string) {
	switch {
	case d.idleDoersUsed[doerRole] < len(d.idle[doerRole]):
		d.idleDoersUsed[doerRole]++
	case d.setAside(startKind{doerRole, 0}, d.roleCLI(doerRole), task.ID):
	default:
		d.uncovered[doerRole] = append(d.uncovered[doerRole], task.ID)
	}
}

func (d *capacityDemand) classifyReviewerDemand(task *models.Task, reviewerRole string) {
	if d.reviewerPolicy == nil {
		if !d.setAside(startKind{reviewerRole, 0}, d.roleCLI(reviewerRole), task.ID) {
			d.uncovered[reviewerRole] = append(d.uncovered[reviewerRole], task.ID)
		}
		return
	}
	slots, _ := d.repairCLI.RoleModels.ReviewSlots(reviewerRole)
	if matchIdleReviewer(d.state, task, reviewerRole, slots, d.idle[reviewerRole], d.matched, d.reviewerPolicy, d.now) {
		return
	}
	planner, ok := d.planners[reviewerRole]
	if !ok {
		planner = newReviewerStartPlanner(d.state, d.pr, d.repairCLI, reviewerRole, d.reservedIDs, d.now)
		d.planners[reviewerRole] = planner
	}
	if planner == nil {
		// The spawn path reports the CLI error; count the demand as usual.
		d.uncovered[reviewerRole] = append(d.uncovered[reviewerRole], task.ID)
		return
	}
	if item := planner.itemFor(task); d.setAside(startKind{reviewerRole, item}, planner.cliFor(item), task.ID) {
		return
	}
	agentID, item, ok := planner.assign(task, d.reviewerPolicy)
	if !ok {
		kind := startKind{reviewerRole, item}
		d.unservable[kind] = append(d.unservable[kind], task.ID)
		d.unservableCLI[kind] = planner.cliFor(item)
		return
	}
	d.uncovered[reviewerRole] = append(d.uncovered[reviewerRole], task.ID)
	d.plannedIDs[reviewerRole] = append(d.plannedIDs[reviewerRole], agentID)
	if item > 0 {
		d.plannedItems[reviewerRole] = append(d.plannedItems[reviewerRole], item)
	}
}

// ownerlessMergeStarts returns the roles that need one start for the merges no
// eligible reviewer owns. One reviewer owns every ownerless merge of its role,
// so they need one start, and none when a review start is already planned for
// the role.
func (d *capacityDemand) ownerlessMergeStarts() map[string]bool {
	mergeStart := make(map[string]bool)
	for _, role := range slices.Sorted(maps.Keys(d.ownerless)) {
		if len(d.uncovered[role]) > 0 {
			continue
		}
		cliBlocked := false
		for _, taskID := range d.ownerless[role] {
			cliBlocked = d.setAside(startKind{role, 0}, d.roleCLI(role), taskID)
		}
		mergeStart[role] = !cliBlocked
	}
	return mergeStart
}

// missingRoleWork bounds each role's uncovered demand by its max-instances
// headroom.
func (d *capacityDemand) missingRoleWork(mergeStart map[string]bool) []MissingRoleWork {
	maxResolver, _ := d.pr.(maxInstancesResolver)
	var missing []MissingRoleWork
	demandRoles := slices.Collect(maps.Keys(d.uncovered))
	for role, start := range mergeStart {
		if start {
			demandRoles = append(demandRoles, role)
		}
	}
	slices.Sort(demandRoles)
	for _, role := range demandRoles {
		roleMax := 0
		if maxResolver != nil {
			roleMax, _ = maxResolver.MaxInstances(role)
		}
		headroom := models.EffectiveMaxInstances(roleMax, d.state.Config.MaxInstances) - d.occupied[role]
		starts, reason := len(d.uncovered[role]), ""
		if mergeStart[role] {
			starts, reason = 1, mergeOwnerDemandReason
		}
		spawnCount := min(starts, headroom)
		if spawnCount <= 0 {
			continue
		}
		taskIDs := slices.Concat(d.uncovered[role], d.ownerless[role])
		sort.Strings(taskIDs)
		var agentIDs []string
		if ids := d.plannedIDs[role]; len(ids) > 0 {
			agentIDs = ids[:spawnCount]
		}
		var items []int
		if planned := d.plannedItems[role]; len(planned) > 0 {
			items = planned[:spawnCount]
		}
		missing = append(missing, MissingRoleWork{
			Role:         role,
			TaskIDs:      taskIDs,
			TaskCount:    len(taskIDs),
			SpawnCount:   spawnCount,
			AgentIDs:     agentIDs,
			Items:        items,
			Reason:       reason,
			headroom:     headroom,
			plannedIDs:   d.plannedIDs[role],
			plannedItems: d.plannedItems[role],
		})
	}
	return missing
}

func (d *capacityDemand) unservableRoleWork() []UnservableRoleWork {
	var notServable []UnservableRoleWork
	for _, kind := range slices.SortedFunc(maps.Keys(d.unservable), compareStartKinds) {
		taskIDs := d.unservable[kind]
		sort.Strings(taskIDs)
		notServable = append(notServable, UnservableRoleWork{
			Role:    kind.role,
			CLI:     d.unservableCLI[kind],
			Item:    kind.item,
			TaskIDs: taskIDs,
			Reason:  "no reviewer this CLI could start would be allowed to claim these tasks (provider diversity, claim cooldown or " + paths.ModelsFileName + " slot); they wait for an eligible reviewer",
		})
	}
	return notServable
}

func (d *capacityDemand) providerBlockedRoleWork() []ProviderBlockedRoleWork {
	var providerBlocked []ProviderBlockedRoleWork
	for _, kind := range slices.SortedFunc(maps.Keys(d.blockedStarts), compareStartKinds) {
		work := d.blockedStarts[kind]
		work.TaskIDs = d.blockedTasks[kind]
		sort.Strings(work.TaskIDs)
		providerBlocked = append(providerBlocked, work)
	}
	return providerBlocked
}
