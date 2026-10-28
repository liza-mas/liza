package agent

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/prompts"
)

// collectPlanPosition returns the visible count of planned tasks and the 1-based
// ordinal position of currentTaskID in the visible plan.
// Returns 0, 0 if no planned tasks or if currentTaskID is not in the planned list
// (e.g. mid-sprint replacement tasks created outside the original plan).
//
// Note: tasks not found by FindTask are silently skipped. This assumes the orchestrator keeps
// Sprint.Scope.Planned in sync with the task list (archived/removed tasks are pruned from planned[]).
func collectPlanPosition(state *models.State, currentTaskID string) (int, int) {
	planned := state.Sprint.Scope.Planned
	if len(planned) == 0 {
		return 0, 0
	}

	ordinal := 0
	visibleTotal := 0
	for _, id := range planned {
		task := state.FindTask(id)
		if task == nil {
			continue
		}
		if id == currentTaskID {
			visibleTotal++
			ordinal = visibleTotal
			continue
		}
		if isDeadPathPlanSibling(task.Status) {
			continue
		}
		visibleTotal++
	}

	// Suppress scoping for tasks not in the plan (mid-sprint replacements).
	// Returning 0 for totalPlanTasks ensures the template condition is false.
	if ordinal == 0 {
		return 0, 0
	}

	return visibleTotal, ordinal
}

func isDeadPathPlanSibling(status models.TaskStatus) bool {
	return status == models.TaskStatusAbandoned || status == models.TaskStatusSuperseded
}

func siblingTaskSummary(task *models.Task) prompts.SiblingTaskSummary {
	return prompts.SiblingTaskSummary{
		ID:          task.ID,
		Description: prompts.TruncateText(task.Description, taskGraphDescriptionLimit),
		Status:      string(task.Status),
		PlanRef:     task.PlanRef,
		RolePair:    task.RolePair,
	}
}

func collectPhaseDependencyTasks(state *models.State, current *models.Task) []prompts.SiblingTaskSummary {
	if len(current.DependsOn) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var dependencies []prompts.SiblingTaskSummary
	for _, depID := range current.DependsOn {
		dep := state.FindTask(depID)
		if dep == nil || dep.RolePair != current.RolePair || seen[dep.ID] {
			continue
		}
		dependencies = append(dependencies, siblingTaskSummary(dep))
		seen[dep.ID] = true
	}
	return dependencies
}

var (
	backtickRefPattern = regexp.MustCompile("`([^`]+)`")
	// Heuristic path extraction can match URL or date-like fragments; only
	// shared refs are surfaced, so most one-off false positives drop out.
	pathRefPattern = regexp.MustCompile(`[A-Za-z0-9_.@+/-]+/[A-Za-z0-9_.@+/-]+|[A-Za-z0-9_.@+-]+\.[A-Za-z0-9][A-Za-z0-9_.@+-]*`)
)

const (
	maxTaskGraphChildrenPerEntry = 8
	maxTaskGraphChildDeps        = 3
	maxTaskGraphEntries          = 16
	maxTaskGraphOmittedSampleIDs = 8

	// Truncation limits, in runes, of the task text a digest entry shows.
	taskGraphDescriptionLimit   = 96
	taskGraphBlockedReasonLimit = 120
)

// taskGraphRelation names why a task is in the digest. It is rendered as text
// and converted to string where an entry stores it.
type taskGraphRelation string

const (
	relationDependency       taskGraphRelation = "dependency"
	relationBlocked          taskGraphRelation = "blocked"
	relationSibling          taskGraphRelation = "sibling"
	relationFileOverlap      taskGraphRelation = "file-overlap"
	relationArtifactProducer taskGraphRelation = "artifact-producer"
	relationArtifactRef      taskGraphRelation = "artifact-ref"
)

// Digest ordering weights: the higher a weight, the earlier the entry stays
// when the digest is bounded.
const (
	taskGraphWeightDependency   = 100
	taskGraphWeightBlocked      = 90
	taskGraphWeightFileOverlap  = 80
	taskGraphWeightSibling      = 50
	taskGraphWeightDefault      = 30
	taskGraphWeightArtifactOnly = 10
)

func buildRelevantTaskGraph(state *models.State, current *models.Task) prompts.TaskGraphDigest {
	currentRefs := taskScopeRefs(current)
	childrenByParent := taskChildrenByParent(state)
	entries := make(map[string]*prompts.TaskGraphEntry)
	var ordered []*prompts.TaskGraphEntry

	addTask := func(task *models.Task, relation taskGraphRelation, sharedRefs []string) {
		if task == nil {
			return
		}
		entry := entries[task.ID]
		if entry == nil {
			newEntry := taskGraphEntry(task)
			entry = &newEntry
			entries[task.ID] = entry
			ordered = append(ordered, entry)
		}
		entry.Relations = appendUniqueString(entry.Relations, string(relation))
		entry.SharedRefs = appendUniqueStrings(entry.SharedRefs, sharedRefs...)
	}

	dependencies := make([]*models.Task, 0, len(current.DependsOn))
	for _, depID := range current.DependsOn {
		dep := state.FindTask(depID)
		if dep == nil {
			continue
		}
		dependencies = append(dependencies, dep)
		addTask(dep, relationDependency, nil)
		if dep.Status == models.TaskStatusBlocked {
			addTask(dep, relationBlocked, nil)
		}
	}

	siblings := plannedSiblings(state, current.ID)
	for _, sibling := range siblings {
		if sibling.Status == models.TaskStatusBlocked {
			addTask(sibling, relationBlocked, nil)
		}
	}

	for _, sibling := range siblings {
		if sibling.Status.IsTerminal() {
			continue
		}
		sharedRefs := intersectRefs(currentRefs, taskScopeRefs(sibling))
		if len(sharedRefs) > 0 {
			addTask(sibling, relationSibling, nil)
			addTask(sibling, relationFileOverlap, sharedRefs)
		}
	}

	for _, dep := range dependencies {
		if taskHasProducedOutputRefs(dep) {
			addTask(dep, relationArtifactProducer, nil)
		}
		if !isCompletedForDigest(state, dep) {
			continue
		}
		if taskHasTaskArtifactRefs(dep) {
			addTask(dep, relationArtifactRef, nil)
		}
	}

	for _, sibling := range siblings {
		if taskHasProducedOutputRefs(sibling) {
			addTask(sibling, relationArtifactProducer, nil)
		}
		if !isCompletedForDigest(state, sibling) {
			continue
		}
		if taskHasTaskArtifactRefs(sibling) {
			addTask(sibling, relationArtifactRef, nil)
		}
	}

	for _, sibling := range siblings {
		if !isDeadPathPlanSibling(sibling.Status) {
			addTask(sibling, relationSibling, nil)
		}
	}

	digest := prompts.TaskGraphDigest{
		Entries: make([]prompts.TaskGraphEntry, 0, len(ordered)),
	}
	for _, entry := range boundedTaskGraphEntries(ordered, &digest) {
		entry.Children, entry.RemainingChildren = summarizeTaskGraphChildren(childrenByParent[entry.ID])
		digest.Entries = append(digest.Entries, *entry)
	}
	return digest
}

func boundedTaskGraphEntries(ordered []*prompts.TaskGraphEntry, digest *prompts.TaskGraphDigest) []*prompts.TaskGraphEntry {
	if len(ordered) <= maxTaskGraphEntries {
		return ordered
	}

	ranked := append([]*prompts.TaskGraphEntry(nil), ordered...)
	sort.SliceStable(ranked, func(i, j int) bool {
		// Equal scores keep the existing candidate discovery order.
		return taskGraphPriority(ranked[i]) > taskGraphPriority(ranked[j])
	})

	selectedIDs := make(map[string]bool, maxTaskGraphEntries)
	for _, entry := range ranked[:maxTaskGraphEntries] {
		selectedIDs[entry.ID] = true
	}

	var bounded []*prompts.TaskGraphEntry
	var omitted []prompts.TaskGraphEntry
	for _, entry := range ordered {
		if selectedIDs[entry.ID] {
			bounded = append(bounded, entry)
			continue
		}
		omitted = append(omitted, *entry)
	}
	digest.Omitted = summarizeOmittedTaskGraphEntries(omitted)
	return bounded
}

func taskGraphPriority(entry *prompts.TaskGraphEntry) int {
	if entry == nil {
		return 0
	}
	if hasTaskGraphRelation(entry, relationDependency) {
		return taskGraphWeightDependency
	}
	if hasTaskGraphRelation(entry, relationBlocked) || entry.BlockedReason != "" || entry.RepairOperation != "" {
		return taskGraphWeightBlocked
	}
	if hasTaskGraphRelation(entry, relationFileOverlap) {
		return taskGraphWeightFileOverlap
	}
	if isArtifactOnlyTaskGraphEntry(entry) {
		return taskGraphWeightArtifactOnly
	}
	if hasTaskGraphRelation(entry, relationSibling) {
		return taskGraphWeightSibling
	}
	return taskGraphWeightDefault
}

func isArtifactOnlyTaskGraphEntry(entry *prompts.TaskGraphEntry) bool {
	if entry == nil {
		return false
	}
	if hasTaskGraphRelation(entry, relationDependency) ||
		hasTaskGraphRelation(entry, relationBlocked) ||
		hasTaskGraphRelation(entry, relationFileOverlap) ||
		entry.BlockedReason != "" ||
		entry.RepairOperation != "" {
		return false
	}
	return hasTaskGraphRelation(entry, relationArtifactProducer) || hasTaskGraphRelation(entry, relationArtifactRef)
}

func hasTaskGraphRelation(entry *prompts.TaskGraphEntry, relation taskGraphRelation) bool {
	return slices.Contains(entry.Relations, string(relation))
}

func summarizeOmittedTaskGraphEntries(entries []prompts.TaskGraphEntry) prompts.TaskGraphOmittedSummary {
	summary := prompts.TaskGraphOmittedSummary{Count: len(entries)}
	if len(entries) == 0 {
		return summary
	}

	statusCounts := make(map[string]int)
	relationCounts := make(map[string]int)
	for _, entry := range entries {
		statusCounts[entry.Status]++
		for _, relation := range entry.Relations {
			relationCounts[relation]++
		}
		if len(summary.SampleIDs) < maxTaskGraphOmittedSampleIDs {
			summary.SampleIDs = append(summary.SampleIDs, entry.ID)
		}
	}
	summary.RemainingSampleIDs = len(entries) - len(summary.SampleIDs)
	summary.StatusCounts = sortedTaskGraphCounts(statusCounts)
	summary.RelationCounts = sortedTaskGraphCounts(relationCounts)
	return summary
}

func sortedTaskGraphCounts(counts map[string]int) []prompts.TaskGraphCount {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	sorted := make([]prompts.TaskGraphCount, 0, len(keys))
	for _, key := range keys {
		sorted = append(sorted, prompts.TaskGraphCount{Name: key, Count: counts[key]})
	}
	return sorted
}

func taskChildrenByParent(state *models.State) map[string][]*models.Task {
	childrenByParent := make(map[string][]*models.Task)
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for _, parentID := range task.EffectiveParentTasks() {
			if parentID == "" {
				continue
			}
			childrenByParent[parentID] = append(childrenByParent[parentID], task)
		}
	}
	return childrenByParent
}

func summarizeTaskGraphChildren(children []*models.Task) ([]prompts.TaskGraphChildSummary, int) {
	limit := min(len(children), maxTaskGraphChildrenPerEntry)
	summaries := make([]prompts.TaskGraphChildSummary, 0, limit)
	for _, child := range children[:limit] {
		depLimit := min(len(child.DependsOn), maxTaskGraphChildDeps)
		summaries = append(summaries, prompts.TaskGraphChildSummary{
			ID:                 child.ID,
			Status:             string(child.Status),
			RolePair:           child.RolePair,
			DependsOn:          child.DependsOn[:depLimit],
			RemainingDependsOn: len(child.DependsOn) - depLimit,
		})
	}
	return summaries, len(children) - limit
}

func plannedSiblings(state *models.State, currentTaskID string) []*models.Task {
	inPlan := false
	for _, id := range state.Sprint.Scope.Planned {
		if id == currentTaskID {
			inPlan = true
			break
		}
	}
	if !inPlan {
		return nil
	}

	var siblings []*models.Task
	for _, id := range state.Sprint.Scope.Planned {
		if id == currentTaskID {
			continue
		}
		if task := state.FindTask(id); task != nil {
			siblings = append(siblings, task)
		}
	}
	return siblings
}

func taskGraphEntry(task *models.Task) prompts.TaskGraphEntry {
	entry := prompts.TaskGraphEntry{
		ID:          task.ID,
		Description: prompts.TruncateText(task.Description, taskGraphDescriptionLimit),
		Status:      string(task.Status),
	}
	if task.BlockedReason != nil {
		entry.BlockedReason = prompts.TruncateText(*task.BlockedReason, taskGraphBlockedReasonLimit)
	}
	if task.RepairRequest != nil {
		entry.RepairOperation = task.RepairRequest.Operation
		if task.RepairRequest.Target != "" {
			entry.RepairOperation += " -> " + task.RepairRequest.Target
		}
	}
	return entry
}

func taskScopeRefs(task *models.Task) []string {
	refs := extractFileRefs(task.Scope)
	refs = appendUniqueStrings(refs, extractFileRefs(task.DoneWhen)...)
	return refs
}

func extractFileRefs(text string) []string {
	var refs []string
	for _, match := range backtickRefPattern.FindAllStringSubmatch(text, -1) {
		if len(match) > 1 {
			refs = appendPathRef(refs, match[1])
		}
	}
	for _, match := range pathRefPattern.FindAllString(text, -1) {
		refs = appendPathRef(refs, match)
	}
	return refs
}

func appendPathRef(refs []string, ref string) []string {
	ref = strings.Trim(ref, " \t\r\n.,;:()[]{}\"'")
	if ref == "" || strings.Contains(ref, "*") || strings.Contains(ref, " ") {
		return refs
	}
	if !strings.Contains(ref, "/") && !strings.Contains(ref, ".") {
		return refs
	}
	return appendUniqueString(refs, ref)
}

func taskHasTaskArtifactRefs(task *models.Task) bool {
	if task == nil {
		return false
	}
	return task.SpecRef != "" || task.EpicRef != "" || task.PlanRef != "" || task.ArchRef != ""
}

func taskHasProducedOutputRefs(task *models.Task) bool {
	if task == nil {
		return false
	}
	for _, entry := range task.Output {
		if entry.SpecRef != "" || entry.EpicRef != "" || entry.PlanRef != "" || entry.ArchRef != "" {
			return true
		}
	}
	return false
}

func intersectRefs(left, right []string) []string {
	if len(left) == 0 || len(right) == 0 {
		return nil
	}
	rightSet := make(map[string]bool, len(right))
	for _, ref := range right {
		rightSet[ref] = true
	}
	var shared []string
	for _, ref := range left {
		if rightSet[ref] {
			shared = append(shared, ref)
		}
	}
	return shared
}

func isCompletedForDigest(state *models.State, task *models.Task) bool {
	if state == nil || task == nil {
		return false
	}
	return state.ResolveDependency(task.ID).Satisfied()
}

func appendUniqueStrings(refs []string, candidates ...string) []string {
	for _, candidate := range candidates {
		refs = appendUniqueString(refs, candidate)
	}
	return refs
}

func appendUniqueString(refs []string, candidate string) []string {
	if candidate == "" {
		return refs
	}
	for _, existing := range refs {
		if existing == candidate {
			return refs
		}
	}
	return append(refs, candidate)
}
