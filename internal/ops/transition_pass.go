package ops

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/pipeline"
)

// defaultIntegrationBranch names the integration branch when state configures none.
const defaultIntegrationBranch = "integration"

// transitionPass is one ExecuteTransitionsReportWith pass. The entry operation
// fills the inputs it reads before the state lock (Git and artifact reads must
// not run inside Modify); the in-lock steps then collect, order and execute the
// due transitions and accumulate what the report and the post-commit alerts need.
type transitionPass struct {
	projectRoot   string
	triggerFilter string
	admission     TransitionAdmission
	resolver      *pipeline.Resolver
	handoff       PlanHandoffDomain
	blackboard    *db.Blackboard

	directChecks    map[string]directCodingGenerationCheck
	integrationHEAD string

	now              time.Time
	results          []ProceedResult
	failures         []TransitionFailure
	observedFailures []PlanHandoffFailure
	skippedHandoffs  []TransitionFailure
}

func (p *transitionPass) fail(taskID, transition string, err error) {
	p.failures = append(p.failures, TransitionFailure{SourceTaskID: taskID, Transition: transition, Error: err.Error()})
}

// readInputs performs the pre-lock reads: the direct-coding generation checks
// and, until goal.base_commit is recorded, the integration HEAD. It returns the
// state it read, nil when the read failed.
func (p *transitionPass) readInputs() *models.State {
	preState, err := p.blackboard.Read()
	if err != nil {
		return nil
	}
	p.directChecks = p.directCodingChecks(preState)
	if preState.Goal.BaseCommit == nil {
		branch := preState.Config.IntegrationBranch
		if branch == "" {
			branch = defaultIntegrationBranch
		}
		p.integrationHEAD, _ = git.New(p.projectRoot).GetCommitSHA(branch)
	}
	return preState
}

// directCodingChecks validates the artifacts of every merged coding allocation
// that this pass could generate children for.
func (p *transitionPass) directCodingChecks(preState *models.State) map[string]directCodingGenerationCheck {
	checks := make(map[string]directCodingGenerationCheck)
	for i := range preState.Tasks {
		task := &preState.Tasks[i]
		if task.Status != models.TaskStatusMerged || !models.HasCodingAllocation(task.Output) || task.PlanGenerationFenced() || task.PlanHandoffRetired() || task.TransitionsExecuted[models.ReplannedTransition] {
			continue
		}
		if p.needsGeneration(preState, task) {
			checks[task.ID] = validateDirectCodingGeneration(p.projectRoot, preState, task)
		}
	}
	return checks
}

// needsGeneration reports whether the task has a pending hand-off (outside an
// auto-only pass) or an executed transition that crash recovery must complete.
func (p *transitionPass) needsGeneration(preState *models.State, task *models.Task) bool {
	needs := p.triggerFilter != string(models.TransitionTriggerAuto) && p.handoff.Pending(task)
	for name, executed := range task.TransitionsExecuted {
		needs = needs || (executed && isTransitionIncomplete(preState, task, name, p.resolver))
	}
	return needs
}

// execute is the pass body, run inside the single state transaction.
func (p *transitionPass) execute(s *models.State) error {
	p.now = time.Now().UTC()
	pending := p.fenceDirectCoding(s, p.collect(s))
	sorted, cycles, downstreamBlocked := topoSortPending(pending, s)
	p.recordCycles(s, cycles, downstreamBlocked)
	p.executeSorted(s, sorted)
	p.snapshotBaseCommit(s)
	p.planChildren(s)
	return nil
}

// collect gathers the available transitions, then the incomplete ones (crash
// recovery), deduplicated by (task, transition).
func (p *transitionPass) collect(s *models.State) []pendingTx {
	pending := p.collectAvailable(s, nil)
	pending = p.collectIncomplete(s, pending)
	return dedupePending(pending)
}

// transitionSourceEligible filters the tasks a pass may expand. All tasks fan
// out from MERGED (after the merge handler); integration tasks previously
// bypassed merge, but that left orphaned worktrees. No-diff merges are handled
// gracefully by performCASMerge (ancestor check → fast-forward no-op).
// Replanned tasks must not spawn children: the replan replacement owns the
// downstream pipeline. Replan marks real transitions as executed (preventive);
// this is the defensive second layer.
func transitionSourceEligible(task *models.Task) bool {
	if task.RolePair == "" || task.Status != models.TaskStatusMerged {
		return false
	}
	return !task.TransitionsExecuted[models.ReplannedTransition] && !task.PlanHandoffRetired() && !task.PlanGenerationFenced()
}

// collectAvailable queues the not yet executed transitions the trigger filter
// and the admission select.
func (p *transitionPass) collectAvailable(s *models.State, pending []pendingTx) []pendingTx {
	for i := range s.Tasks {
		task := &s.Tasks[i]
		if !transitionSourceEligible(task) {
			continue
		}
		approvedStatus, err := p.resolver.ApprovedStatus(task.RolePair)
		if err != nil {
			p.fail(task.ID, "", fmt.Errorf("unknown role-pair %q: %w", task.RolePair, err))
			continue
		}
		for _, name := range p.availableTransitions(approvedStatus, task) {
			if p.admitsAvailable(s, task, name) {
				pending = p.queue(pending, i, task.ID, name, "")
			}
		}
	}
	return pending
}

func (p *transitionPass) availableTransitions(approvedStatus models.TaskStatus, task *models.Task) []string {
	switch p.triggerFilter {
	case string(models.TransitionTriggerAuto):
		return p.resolver.AvailableAutoTransitions(approvedStatus, task.TransitionsExecuted)
	case string(models.TransitionTriggerManual):
		return p.resolver.AvailableManualTransitions(approvedStatus, task.TransitionsExecuted)
	default: // "" — both
		available := p.resolver.AvailableManualTransitions(approvedStatus, task.TransitionsExecuted)
		return append(available, p.resolver.AvailableAutoTransitions(approvedStatus, task.TransitionsExecuted)...)
	}
}

// admitsAvailable reports whether the task's output selects the transition and
// the pass's admission authorizes its hand-off.
func (p *transitionPass) admitsAvailable(s *models.State, task *models.Task, name string) bool {
	if !p.resolver.TransitionApplies(name, task.Output) {
		return false
	}
	if p.admission == AdmitReviewed && p.handoff.TransitionFailure(s, task, name) != nil {
		return false
	}
	return !p.handoff.GatesTransition(task, name) || admitsHandoff(p.handoff, s, task, p.admission)
}

// collectIncomplete queues executed transitions whose children are not all present.
func (p *transitionPass) collectIncomplete(s *models.State, pending []pendingTx) []pendingTx {
	for i := range s.Tasks {
		task := &s.Tasks[i]
		if !transitionSourceEligible(task) {
			continue
		}
		for name := range task.TransitionsExecuted {
			if isTransitionIncomplete(s, task, name, p.resolver) {
				pending = p.queue(pending, i, task.ID, name, "crash recovery")
			}
		}
	}
	return pending
}

// queue appends the transition with its definition; a definition that cannot be
// resolved is reported as a failure instead, its error prefixed by errPrefix.
func (p *transitionPass) queue(pending []pendingTx, taskIdx int, taskID, name, errPrefix string) []pendingTx {
	tDef, err := buildTransitionDefFromPipeline(p.resolver, name)
	if err != nil {
		if errPrefix != "" {
			err = fmt.Errorf("%s: %w", errPrefix, err)
		}
		p.fail(taskID, name, err)
		return pending
	}
	tDef.requiredStatus = models.TaskStatusMerged
	return append(pending, pendingTx{taskID: taskID, taskIdx: taskIdx, name: name, tDef: tDef, origIdx: len(pending)})
}

// dedupePending drops repeats by (taskID, transitionName): the same transition
// may appear in both scans.
func dedupePending(pending []pendingTx) []pendingTx {
	seen := make(map[string]bool)
	deduped := pending[:0]
	for _, tx := range pending {
		key := tx.taskID + "\x00" + tx.name
		if !seen[key] {
			seen[key] = true
			deduped = append(deduped, tx)
		}
	}
	return deduped
}

// fenceDirectCoding refuses a coding allocation whose artifacts were not
// checked, or whose check no longer holds, before either ordinary generation or
// recovery can publish its coding children. Artifact reads ran outside Modify.
func (p *transitionPass) fenceDirectCoding(s *models.State, pending []pendingTx) []pendingTx {
	admitted := pending[:0]
	for _, candidate := range pending {
		task := s.FindTask(candidate.taskID)
		if models.HasCodingAllocation(task.Output) {
			check, checked := p.directChecks[task.ID]
			if !checked || check.err != nil {
				if check.err == nil {
					check.err = fmt.Errorf("direct coding allocation was not checked before generation")
				}
				p.fail(task.ID, candidate.name, check.err)
				continue
			}
			if err := check.validateState(s, task); err != nil {
				p.fail(task.ID, candidate.name, err)
				continue
			}
		}
		admitted = append(admitted, candidate)
	}
	return admitted
}

// recordCycles adds one idempotent history event per cycle member and reports
// every member, and every transition downstream of a cycle, as a failure.
func (p *transitionPass) recordCycles(s *models.State, cycles [][]pendingTx, downstreamBlocked []pendingTx) {
	for _, cycle := range cycles {
		cycleMemberIDs := make([]string, len(cycle))
		for i, tx := range cycle {
			cycleMemberIDs[i] = tx.taskID
		}
		slices.Sort(cycleMemberIDs)

		for _, tx := range cycle {
			task := &s.Tasks[tx.taskIdx]
			if !hasCycleBlockedEvent(task, tx.name, cycleMemberIDs) {
				task.History = append(task.History, models.TaskHistoryEntry{
					Time:  p.now,
					Event: models.TaskEventTransitionCycleBlocked,
					Extra: map[string]any{
						"transition":    tx.name,
						"cycle_members": cycleMemberIDs,
					},
				})
			}
			p.fail(tx.taskID, tx.name, fmt.Errorf("blocked by dependency cycle among %v", cycleMemberIDs))
		}
	}

	for _, tx := range downstreamBlocked {
		p.fail(tx.taskID, tx.name, errors.New("blocked by an upstream dependency cycle"))
	}
}

// executeSorted runs the ordered transitions, so upstream children exist before
// downstream inherited dependencies are computed. The first pending planning
// hand-off stamps the pass start, so plans arriving after this attempt stay fresh.
func (p *transitionPass) executeSorted(s *models.State, sorted []pendingTx) {
	planningAttemptStamped := false
	for _, tx := range sorted {
		task := s.FindTask(tx.taskID)
		// Earlier transitions in this pass may have repaired selected inputs.
		if p.admission == AdmitReviewed && p.handoff.TransitionFailure(s, task, tx.name) != nil {
			continue
		}
		if !planningAttemptStamped && p.handoff.Pending(task) && !task.TransitionsExecuted[tx.name] {
			passStart := p.now
			s.Sprint.Timeline.TransitionsAttemptedAt = &passStart
			planningAttemptStamped = true
		}
		if result, ok := p.executeOne(s, task, tx); ok {
			p.results = append(p.results, result)
		}
	}
}

// executeOne expands one transition; a refusal is classified and reported, and
// leaves the other transitions of the pass unaffected.
func (p *transitionPass) executeOne(s *models.State, task *models.Task, tx pendingTx) (ProceedResult, bool) {
	var originalDeps []string
	var inheritedDeps inheritedDepSet
	if task != nil {
		originalDeps = task.DependsOn
		if err := canonicalizeTaskDependsOnForTransition(s, p.resolver, task); err != nil {
			p.fail(tx.taskID, tx.name, fmt.Errorf("dependency canonicalization: %w", err))
			return ProceedResult{}, false
		}
		var depErr error
		inheritedDeps, depErr = computeInheritedDeps(s, task, tx.name, p.resolver)
		if depErr != nil {
			p.fail(tx.taskID, tx.name, fmt.Errorf("inherited deps: %w", depErr))
			return ProceedResult{}, false
		}
	}

	result := ProceedResult{SourceTaskID: tx.taskID, TransitionName: tx.name}
	if err := proceedTransaction(p.blackboard, s, p.projectRoot, tx.taskID, tx.name, tx.tDef, inheritedDeps, p.resolver, p.now, &result); err != nil {
		p.classifyRefusal(s, task, tx, originalDeps, err)
		return ProceedResult{}, false
	}
	return result, true
}

// classifyRefusal records a refused transition. A hand-off input refusal of an
// initial generation leaves no child or output mutation published; the earlier
// source-dependency normalization is undone and a material observation recorded.
// Normal waits (already executed, incomplete cohort) are not failures.
func (p *transitionPass) classifyRefusal(s *models.State, task *models.Task, tx pendingTx, originalDeps []string, err error) {
	var refusal *handoffInputError
	if task != nil && errors.As(err, &refusal) && p.handoff.GatesTransition(task, tx.name) && !task.TransitionsExecuted[tx.name] {
		task.DependsOn = originalDeps
		if observation := p.handoff.recordFailure(s, task, tx.name, refusal, p.now); observation != nil {
			p.observedFailures = append(p.observedFailures, *observation)
		}
	}
	if errors.Is(err, errTransitionAlreadyExecuted) || errors.Is(err, errManyToOneCohortIncomplete) {
		return
	}
	p.fail(tx.taskID, tx.name, err)
	// A passed plan's skip that no observation classifies stays retryable, but
	// must not live in a WARN line only (D-172).
	if task != nil && task.PlanCheckVerdictOf() == models.PlanCheckPassed && p.handoff.GatesTransition(task, tx.name) &&
		!task.TransitionsExecuted[tx.name] && p.handoff.TransitionFailure(s, task, tx.name) == nil {
		p.skippedHandoffs = append(p.skippedHandoffs, p.failures[len(p.failures)-1])
	}
}

// snapshotBaseCommit records goal.base_commit from the pre-read integration
// HEAD the first time coding-pair children are created.
func (p *transitionPass) snapshotBaseCommit(s *models.State) {
	if p.integrationHEAD == "" || s.Goal.BaseCommit != nil {
		return
	}
	for _, r := range p.results {
		tDef, err := buildTransitionDefFromPipeline(p.resolver, r.TransitionName)
		if err == nil && tDef.targetRolePair == "coding-pair" && len(r.ChildTaskIDs) > 0 {
			head := p.integrationHEAD
			s.Goal.BaseCommit = &head
			return
		}
	}
}

// planChildren adds the created children to the sprint scope, once each so
// crash recovery stays idempotent.
func (p *transitionPass) planChildren(s *models.State) {
	for _, r := range p.results {
		for _, childID := range r.ChildTaskIDs {
			if !slices.Contains(s.Sprint.Scope.Planned, childID) {
				s.Sprint.Scope.Planned = append(s.Sprint.Scope.Planned, childID)
			}
		}
	}
}

// afterCommit runs the post-commit work: retired-original cleanup and the
// hand-off alerts. Neither can fail the pass; failures become report warnings.
func (p *transitionPass) afterCommit() TransitionReport {
	for _, warning := range cleanupRetiredOriginals(p.blackboard, p.projectRoot, p.results) {
		log.Printf("WARNING: ExecuteAvailableTransitions: %s", warning)
	}
	var warnings []string
	for _, observation := range p.observedFailures {
		warnings = appendAlertWarning(warnings, "failure", writeHandoffFailureAlert(p.projectRoot, observation, p.now))
	}
	for _, skipped := range p.skippedHandoffs {
		warnings = appendAlertWarning(warnings, "skip", writeHandoffSkipAlert(p.projectRoot, skipped, p.now))
	}
	return TransitionReport{Results: p.results, Failures: p.failures, Warnings: warnings}
}

func appendAlertWarning(warnings []string, kind string, err error) []string {
	if err == nil {
		return warnings
	}
	warning := fmt.Sprintf("plan handoff %s alert write failed: %v", kind, err)
	log.Printf("WARNING: ExecuteAvailableTransitions: %s", warning)
	return append(warnings, warning)
}

// pendingTx represents a transition queued for execution in ExecuteAvailableTransitions.
type pendingTx struct {
	taskID  string
	taskIdx int
	name    string
	tDef    transitionDef
	origIdx int // original collection index for stable topo-sort tie-breaking
}

// topoSortPending sorts pending transitions by task DependsOn relationships using
// Kahn's algorithm. Tie-breaker is original collection index for deterministic ordering.
// Returns (sorted, cycles, blocked) where cycles are true SCC members and blocked
// are nodes downstream of those cycles.
func topoSortPending(pending []pendingTx, s *models.State) (sorted []pendingTx, cycles [][]pendingTx, blocked []pendingTx) {
	n := len(pending)
	if n == 0 {
		return nil, nil, nil
	}

	// Build task ID → pending indices map
	taskToPending := make(map[string][]int)
	for i, p := range pending {
		taskToPending[p.taskID] = append(taskToPending[p.taskID], i)
	}

	// Compute in-degrees: if pending[i]'s task depends on pending[j]'s task, j→i edge
	inDegree := make([]int, n)
	edges := make([][]int, n) // edges[j] = indices that j blocks
	for i, p := range pending {
		task := s.FindTask(p.taskID)
		if task == nil {
			continue
		}
		for _, depID := range task.DependsOn {
			if depIndices, ok := taskToPending[depID]; ok {
				for _, depIdx := range depIndices {
					edges[depIdx] = append(edges[depIdx], i)
					inDegree[i]++
				}
			}
		}
	}

	// Kahn's: process zero-in-degree nodes, stable by origIdx
	var queue []int
	for i := range pending {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}
	slices.SortFunc(queue, func(a, b int) int {
		return pending[a].origIdx - pending[b].origIdx
	})

	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		sorted = append(sorted, pending[idx])
		for _, blockedIdx := range edges[idx] {
			inDegree[blockedIdx]--
			if inDegree[blockedIdx] == 0 {
				queue = append(queue, blockedIdx)
				slices.SortFunc(queue, func(a, b int) int {
					return pending[a].origIdx - pending[b].origIdx
				})
			}
		}
	}

	// Remaining nodes have inDegree > 0. Some are in true cycles; others are
	// merely downstream of those cycles and cannot be ordered yet.
	var remaining []int
	for i := range pending {
		if inDegree[i] > 0 {
			remaining = append(remaining, i)
		}
	}
	if len(remaining) == 0 {
		return sorted, nil, nil
	}

	cycleIndices := make(map[int]bool)
	for _, scc := range findSCCs(remaining, edges) {
		if len(scc) < 2 && !hasSelfLoop(scc[0], edges) {
			continue
		}
		cycle := make([]pendingTx, len(scc))
		for i, idx := range scc {
			cycle[i] = pending[idx]
			cycleIndices[idx] = true
		}
		cycles = append(cycles, cycle)
	}

	for _, idx := range remaining {
		if !cycleIndices[idx] {
			blocked = append(blocked, pending[idx])
		}
	}

	return sorted, cycles, blocked
}

// findSCCs returns strongly connected components among the given node indices
// using Tarjan's algorithm, considering only edges within the subgraph.
func findSCCs(indices []int, edges [][]int) [][]int {
	inSubgraph := make(map[int]bool, len(indices))
	for _, i := range indices {
		inSubgraph[i] = true
	}

	var (
		stack   []int
		onStack = make(map[int]bool)
		disc    = make(map[int]int)
		low     = make(map[int]int)
		counter int
		sccs    [][]int
	)

	var visit func(v int)
	visit = func(v int) {
		disc[v] = counter
		low[v] = counter
		counter++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range edges[v] {
			if !inSubgraph[w] {
				continue
			}
			if _, seen := disc[w]; !seen {
				visit(w)
				if low[w] < low[v] {
					low[v] = low[w]
				}
			} else if onStack[w] && disc[w] < low[v] {
				low[v] = disc[w]
			}
		}

		if low[v] == disc[v] {
			var scc []int
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			sccs = append(sccs, scc)
		}
	}

	for _, v := range indices {
		if _, seen := disc[v]; !seen {
			visit(v)
		}
	}

	return sccs
}

func hasSelfLoop(idx int, edges [][]int) bool {
	for _, next := range edges[idx] {
		if next == idx {
			return true
		}
	}
	return false
}

// hasCycleBlockedEvent checks if a task already has a transition_cycle_blocked
// history entry for the given transition and cycle members (idempotency guard).
func hasCycleBlockedEvent(task *models.Task, transitionName string, cycleMembers []string) bool {
	for _, h := range task.History {
		if h.Event != models.TaskEventTransitionCycleBlocked {
			continue
		}
		if trans, ok := h.Extra["transition"].(string); ok && trans == transitionName {
			if slices.Equal(extraToStringSlice(h.Extra["cycle_members"]), cycleMembers) {
				return true
			}
		}
	}
	return false
}

// extraToStringSlice normalizes an Extra field value to []string.
// After YAML round-trip, []string becomes []any — this handles both.
func extraToStringSlice(v any) []string {
	switch typed := v.(type) {
	case []string:
		return typed
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}
