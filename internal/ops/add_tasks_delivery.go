package ops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/liza-mas/liza/internal/brand"
	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

// AddTasksInput represents the input for batch task creation.
type AddTasksInput struct {
	Tasks          []AddTaskInput
	OrchestratorID string
}

// AddTasksResult contains the outcome of batch task creation.
type AddTasksResult struct {
	Results []AddTaskItemResult `json:"results"`
}

// AddTaskItemResult contains the outcome of adding a single task in a batch.
type AddTaskItemResult struct {
	TaskID   string   `json:"task_id"`
	Success  bool     `json:"success"`
	Error    string   `json:"error"` // empty on success
	Warnings []string `json:"warnings"`
}

// AddTasks adds multiple tasks in a single call. Each task is added
// independently; failed tasks don't block subsequent ones.
func AddTasks(statePath, logPath string, input *AddTasksInput) (*AddTasksResult, error) {
	return addTasksWithOptionalAuthority(statePath, logPath, input, nil)
}

// AddTasksWithAuthority fences every independent item write with the same
// current orchestrator generation and stops the batch on authority loss.
func AddTasksWithAuthority(statePath, logPath string, input *AddTasksInput, authority models.AgentAuthority) (*AddTasksResult, error) {
	return addTasksWithOptionalAuthority(statePath, logPath, input, &authority)
}

func addTasksWithOptionalAuthority(statePath, logPath string, input *AddTasksInput, authority *models.AgentAuthority) (*AddTasksResult, error) {
	if len(input.Tasks) == 0 {
		return nil, &PreconditionError{Reason: "at least one task is required"}
	}
	orchestratorID := input.OrchestratorID
	if authority != nil {
		orchestratorID = authority.ID
	}
	if orchestratorID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	// Items commit one by one, so a placed writer must be commissioned alone:
	// its creation and its successors' reservations share one transaction.
	if len(input.Tasks) > 1 {
		for _, item := range input.Tasks {
			if len(item.ReserveSuccessors) > 0 || item.MaxOutputs != 0 {
				return nil, &PreconditionError{Reason: fmt.Sprintf("task %s carries reserve_successors or max_outputs; commission placed writers one per add-tasks request", item.ID)}
			}
		}
	}
	units := planDeliveryUnits(statePath, input.Tasks)
	if units == nil {
		result := &AddTasksResult{Results: make([]AddTaskItemResult, 0, len(input.Tasks))}
		for i := range input.Tasks {
			item, err := addOneOfTasks(statePath, logPath, &input.Tasks[i], orchestratorID, authority)
			if err != nil {
				return nil, err
			}
			result.Results = append(result.Results, item)
		}
		return result, nil
	}

	// Results stay positional whatever order the units commit in.
	results := make([]AddTaskItemResult, len(input.Tasks))
	for _, unit := range units {
		if len(unit) == 1 {
			item, err := addOneOfTasks(statePath, logPath, &input.Tasks[unit[0]], orchestratorID, authority)
			if err != nil {
				return nil, err
			}
			results[unit[0]] = item
			continue
		}
		items, err := addDeliveryGroup(statePath, logPath, input.Tasks, unit, orchestratorID, authority)
		if err != nil {
			return nil, err
		}
		for k, idx := range unit {
			results[idx] = items[k]
		}
	}
	return &AddTasksResult{Results: results}, nil
}

// addOneOfTasks adds one batch item on its own. Only an authority loss is
// returned as an error: it stops the batch.
func addOneOfTasks(statePath, logPath string, input *AddTaskInput, orchestratorID string, authority *models.AgentAuthority) (AddTaskItemResult, error) {
	var r *AddTaskResult
	var err error
	if authority == nil {
		r, err = AddTask(statePath, logPath, input, orchestratorID)
	} else {
		r, err = AddTaskWithAuthority(statePath, logPath, input, *authority)
		if IsAgentAuthorityError(err) {
			return AddTaskItemResult{}, err
		}
	}
	item := AddTaskItemResult{TaskID: input.ID}
	if err != nil {
		item.Error = err.Error()
	} else {
		item.Success = true
		item.TaskID = r.TaskID
		item.Warnings = r.Warnings
	}
	return item, nil
}

// deliveryRequirement describes a dead-end stage (D-33): every transition out
// of the role-pair is many-to-one, keyed on a cohort parent that an added task
// never has, so the task can move on only through a delivery task that depends
// on it and was commissioned with it.
type deliveryRequirement struct {
	transitions []string
	targets     []string
}

// deliveryRequirementFor returns the requirement for an added task of rolePair,
// or nil when the stage can move on alone: it has a per-subtask or one-to-one
// transition, or none at all (a terminal stage, or one whose pipeline
// transitions config.no_follow_up suppresses).
func deliveryRequirementFor(resolver *pipeline.Resolver, rolePair string, noFollowUp bool) *deliveryRequirement {
	req := &deliveryRequirement{}
	for _, td := range resolver.AllTransitions() {
		if noFollowUp && resolver.IsPipelineTransition(td.Name) {
			continue
		}
		if source, err := resolver.TransitionSourceRolePair(td.Name); err != nil || source != rolePair {
			continue
		}
		if td.Cardinality != models.CardinalityManyToOne {
			return nil
		}
		req.transitions = append(req.transitions, td.Name)
		if target, err := resolver.TransitionTargetRolePair(td.Name); err == nil && !slices.Contains(req.targets, target) {
			req.targets = append(req.targets, target)
		}
	}
	if len(req.transitions) == 0 {
		return nil
	}
	return req
}

func (r *deliveryRequirement) refusal(taskID, rolePair string) error {
	return &PreconditionError{Reason: fmt.Sprintf(
		"task %s (role_pair %s) cannot be delivered on its own: its only onward transition (%s) is many-to-one and needs a cohort parent that an added task does not have; commission it with %s in one request together with a %s task that depends on it",
		taskID, rolePair, strings.Join(r.transitions, ", "), brand.Command("add-tasks"), strings.Join(r.targets, " or "),
	)}
}

// deliveredBy reports whether candidate delivers source: it belongs to a
// delivery target role-pair and depends on source.
func (r *deliveryRequirement) deliveredBy(source string, candidate *AddTaskInput) bool {
	if !slices.Contains(r.targets, candidate.RolePair) {
		return false
	}
	return slices.ContainsFunc(candidate.DependsOn, func(dep string) bool { return strings.TrimSpace(dep) == source })
}

// planDeliveryUnits returns the commit order of a batch holding a dead-end
// task, or nil when it holds none and the plain item loop applies unchanged.
// Each unit lists request indices: a single item commits alone; several commit
// in one transaction. A dead-end task and its delivery companions form a
// group; units whose in-request dependencies form a cycle through a group
// merge into one unit, so grouping never orders a member before a
// prerequisite. Units then commit in a stable dependency order: the ready unit
// with the lowest request index first.
func planDeliveryUnits(statePath string, tasks []AddTaskInput) [][]int {
	resolver, _, err := loadResolver(filepath.Dir(filepath.Dir(statePath)))
	if err != nil {
		return nil
	}
	snapshot, err := db.For(statePath).ReadSnapshot()
	if err != nil {
		return nil
	}

	units := newRequestUnits(len(tasks))
	grouped := discoverDeliveryGroups(resolver, snapshot.Config.NoFollowUp, tasks, units)
	if len(grouped) == 0 {
		// A dead-end task without a companion is refused by its own add-task.
		return nil
	}
	dependsOn := requestPrerequisites(tasks)
	mergeGroupCycles(units, grouped, dependsOn)
	return orderDeliveryUnits(units, dependsOn)
}

// requestUnits is a union-find over request indices: each root is one unit.
type requestUnits []int

func newRequestUnits(n int) requestUnits {
	units := make(requestUnits, n)
	for i := range units {
		units[i] = i
	}
	return units
}

func (u requestUnits) find(i int) int {
	if u[i] != i {
		u[i] = u.find(u[i])
	}
	return u[i]
}

func (u requestUnits) union(a, b int) { u[u.find(a)] = u.find(b) }

// discoverDeliveryGroups unites every dead-end task with the companions that
// deliver it and returns the request indices that joined a group.
func discoverDeliveryGroups(resolver *pipeline.Resolver, noFollowUp bool, tasks []AddTaskInput, units requestUnits) map[int]bool {
	grouped := map[int]bool{}
	for s := range tasks {
		req := deliveryRequirementFor(resolver, tasks[s].RolePair, noFollowUp)
		if req == nil {
			continue
		}
		for c := range tasks {
			if c != s && req.deliveredBy(tasks[s].ID, &tasks[c]) {
				units.union(s, c)
				grouped[s], grouped[c] = true, true
			}
		}
	}
	return grouped
}

// requestPrerequisites returns the in-request dependencies of an item as
// request indices. A repeated ID resolves to its first occurrence.
func requestPrerequisites(tasks []AddTaskInput) func(int) []int {
	index := map[string]int{}
	for i, task := range tasks {
		if _, seen := index[task.ID]; !seen {
			index[task.ID] = i
		}
	}
	return func(i int) []int {
		var prereqs []int
		for _, dep := range tasks[i].DependsOn {
			if j, ok := index[strings.TrimSpace(dep)]; ok && j != i {
				prereqs = append(prereqs, j)
			}
		}
		return prereqs
	}
}

// mergeGroupCycles merges every cycle of units that runs through a group.
func mergeGroupCycles(units requestUnits, grouped map[int]bool, dependsOn func(int) []int) {
	n := len(units)
	edges := make([][]int, n)
	for i := 0; i < n; i++ {
		for _, j := range dependsOn(i) {
			if units.find(i) != units.find(j) {
				edges[units.find(i)] = append(edges[units.find(i)], units.find(j))
			}
		}
	}
	var roots []int
	groupRoot := map[int]bool{}
	for i := 0; i < n; i++ {
		if units.find(i) == i {
			roots = append(roots, i)
		}
		if grouped[i] {
			groupRoot[units.find(i)] = true
		}
	}
	for _, scc := range findSCCs(roots, edges) {
		if len(scc) > 1 && slices.ContainsFunc(scc, func(r int) bool { return groupRoot[r] }) {
			for _, r := range scc[1:] {
				units.union(r, scc[0])
			}
		}
	}
}

// orderDeliveryUnits orders the units by their prerequisites and the members
// of each unit by their in-unit prerequisites.
func orderDeliveryUnits(units requestUnits, dependsOn func(int) []int) [][]int {
	n := len(units)
	members := map[int][]int{}
	for i := 0; i < n; i++ {
		members[units.find(i)] = append(members[units.find(i)], i)
	}
	var order [][]int
	for _, root := range stableDependencyOrder(n, func(i int) bool { return units.find(i) == i }, func(r int) []int {
		var prereqs []int
		for _, m := range members[r] {
			for _, j := range dependsOn(m) {
				if units.find(j) != r {
					prereqs = append(prereqs, units.find(j))
				}
			}
		}
		return prereqs
	}, func(r int) int { return members[r][0] }) {
		order = append(order, orderUnitMembers(n, members[root], dependsOn))
	}
	return order
}

// orderUnitMembers orders a multi-item unit so members insert after their
// in-unit prerequisites; a single item is returned as is.
func orderUnitMembers(n int, members []int, dependsOn func(int) []int) []int {
	if len(members) < 2 {
		return members
	}
	inUnit := map[int]bool{}
	for _, m := range members {
		inUnit[m] = true
	}
	return stableDependencyOrder(n, func(i int) bool { return inUnit[i] }, func(m int) []int {
		var prereqs []int
		for _, j := range dependsOn(m) {
			if inUnit[j] {
				prereqs = append(prereqs, j)
			}
		}
		return prereqs
	}, func(m int) int { return m })
}

// stableDependencyOrder orders the nodes in [0,n) that include accepts:
// repeatedly the ready node (every prerequisite already placed) with the
// lowest anchor, or, when a cycle leaves none ready, the lowest anchor.
func stableDependencyOrder(n int, include func(int) bool, prereqs func(int) []int, anchor func(int) int) []int {
	var pending, order []int
	for i := 0; i < n; i++ {
		if include(i) {
			pending = append(pending, i)
		}
	}
	slices.SortStableFunc(pending, func(a, b int) int { return anchor(a) - anchor(b) })
	placed := map[int]bool{}
	for len(pending) > 0 {
		pick := 0
		for k, node := range pending {
			if !slices.ContainsFunc(prereqs(node), func(p int) bool { return !placed[p] }) {
				pick = k
				break
			}
		}
		placed[pending[pick]] = true
		order = append(order, pending[pick])
		pending = slices.Delete(pending, pick, pick+1)
	}
	return order
}

// addDeliveryGroup commits the batch items at indices in one transaction
// (D-33): every member persists, or none does and each reports the group
// error. Only an authority loss is returned as an error.
func addDeliveryGroup(statePath, logPath string, tasks []AddTaskInput, indices []int, orchestratorID string, authority *models.AgentAuthority) ([]AddTaskItemResult, error) {
	ids := make([]string, len(indices))
	for k, idx := range indices {
		ids[k] = tasks[idx].ID
	}
	fail := func(memberID string, err error) []AddTaskItemResult {
		results := make([]AddTaskItemResult, len(indices))
		for k := range indices {
			results[k] = AddTaskItemResult{TaskID: ids[k], Error: fmt.Sprintf("delivery group [%s]: %s: %v", strings.Join(ids, ", "), memberID, err)}
		}
		return results
	}

	projectRoot := filepath.Dir(filepath.Dir(statePath))
	built := make([]models.Task, len(indices))
	var resolver *pipeline.Resolver
	for k, idx := range indices {
		task, r, err := prepareAddTask(statePath, projectRoot, &tasks[idx])
		if err != nil {
			return fail(ids[k], err), nil
		}
		built[k], resolver = task, r
	}

	bb := db.For(statePath)
	var postValidationErr error
	var failedMember string
	err := lifecycleMutation(bb, authority)(func(state *models.State) error {
		for k, idx := range indices {
			req := deliveryRequirementFor(resolver, built[k].RolePair, state.Config.NoFollowUp)
			if req != nil && !slices.ContainsFunc(indices, func(c int) bool { return c != idx && req.deliveredBy(ids[k], &tasks[c]) }) {
				failedMember = ids[k]
				return req.refusal(ids[k], built[k].RolePair)
			}
		}
		for k, idx := range indices {
			failedMember = ids[k]
			if err := insertTaskInState(state, projectRoot, built[k], &tasks[idx], resolver); err != nil {
				return err
			}
			if err := reserveSuccessorsInState(state, resolver, &built[k], tasks[idx].ReserveSuccessors, orchestratorID); err != nil {
				return err
			}
		}
		failedMember = ""
		if models.HasProviderDependencies(state) {
			if err := statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, true, os.Stderr); err != nil {
				return &PreconditionError{Reason: fmt.Sprintf("add-task provider dependencies: %v", err)}
			}
		}
		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			postValidationErr = err
		}
		return nil
	})
	if err != nil {
		if authority != nil && IsAgentAuthorityError(err) {
			return nil, err
		}
		if failedMember == "" {
			failedMember = strings.Join(ids, ", ")
		}
		return fail(failedMember, fmt.Errorf("failed to add task: %w", err)), nil
	}

	results := make([]AddTaskItemResult, len(indices))
	for k, idx := range indices {
		r := addedTaskResult(logPath, orchestratorID, built[k], &tasks[idx], postValidationErr)
		results[k] = AddTaskItemResult{TaskID: r.TaskID, Success: true, Warnings: r.Warnings}
	}
	return results, nil
}
