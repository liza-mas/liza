package agent

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/prompts"
	"github.com/liza-mas/liza/internal/referencecontract"
)

// populatePlannerContext projects existing state into advisory navigation. It
// creates no dependencies and never promotes unmerged artifacts to authority.
func populatePlannerContext(task *models.Task, state *models.State, config SupervisorConfig, data *prompts.RoleContextData) error {
	if data.RoleType != "doer" || (config.Role != models.RoleArchitect && config.Role != models.RoleCodePlanner) {
		return nil
	}
	data.ArchSection = paths.SplitRefFragment(task.ArchRef)
	providers, issues := plannerProviderTasks(task, state, config.Role)
	data.PlannerProviderIssues = issues
	for _, provider := range providers {
		row := prompts.PlannerProviderSummary{ID: provider.id, Reasons: provider.reasons}
		producingTask := state.FindTask(provider.id)
		if producingTask == nil {
			row.Status = "MISSING"
			row.Unavailable = "task not found"
		} else {
			row.Status = string(producingTask.Status)
			row.Artifacts, row.Unavailable = plannerProviderArtifacts(producingTask, state)
		}
		data.PlannerProviders = append(data.PlannerProviders, row)
	}
	data.PlannerFormatPrecedent = plannerFormatPrecedent(task, state, config.Role)
	return populatePlannerRework(task, git.New(config.ProjectRoot), data)
}

type plannerProvider struct {
	id      string
	reasons []string
}

func plannerProviderTasks(task *models.Task, state *models.State, role string) ([]plannerProvider, []string) {
	var providers []plannerProvider
	positions := map[string]int{}
	add := func(id, reason string) {
		if id == "" || id == task.ID {
			return
		}
		if position, ok := positions[id]; ok {
			providers[position].reasons = appendUniqueString(providers[position].reasons, reason)
			return
		}
		positions[id] = len(providers)
		providers = append(providers, plannerProvider{id: id, reasons: []string{reason}})
	}
	for _, id := range task.DependsOn {
		add(id, "dependency")
	}
	if task.Decomposition != nil {
		for _, id := range task.Decomposition.ReadOnlyTaskDependsOn {
			add(id, "read-only dependency")
		}
	}

	// Discovery after explicit edges is sorted by actual task identity.
	discovered := map[string][]string{}
	mark := func(id, reason string) {
		discovered[id] = appendUniqueString(discovered[id], reason)
	}
	var issues []string
	if task.Decomposition != nil {
		for _, consumed := range task.Decomposition.InterfacesConsumed {
			owners := plannerInterfaceOwners(state, task.ID, consumed)
			if len(owners) == 0 {
				issues = append(issues, fmt.Sprintf("interface %q: no owner found", consumed))
			} else if len(owners) > 1 {
				issues = append(issues, fmt.Sprintf("interface %q: ambiguous owners [%s]; no provider commitment inferred", consumed, strings.Join(owners, ", ")))
			}
			for _, id := range owners {
				reason := "consumed interface " + consumed
				if len(owners) > 1 {
					reason = "ambiguous interface candidate " + consumed
				}
				mark(id, reason)
			}
		}
	}
	cohorts := map[string]bool{}
	if parent := task.CohortParentID(); parent != "" {
		cohorts[parent] = true
	}
	if role == models.RoleCodePlanner {
		for _, parentID := range task.EffectiveParentTasks() {
			if parent := state.FindTask(parentID); parent != nil && parent.CohortParentID() != "" {
				cohorts[parent.CohortParentID()] = true
			}
		}
	}
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if candidate.ID == task.ID || !cohorts[candidate.CohortParentID()] {
			continue
		}
		if kind := plannerTaskKind(candidate); kind != models.TaskTypeArchitecture && kind != models.TaskTypePlanning {
			continue
		}
		fragment := paths.SplitRefFragment(candidate.ArchRef)
		if fragment == "Scope 0" || strings.HasPrefix(fragment, "Scope 0:") {
			mark(candidate.ID, "explicit Scope 0")
		} else if fragment == "" && strings.HasPrefix(candidate.ID, candidate.CohortParentID()+"-") && strings.HasSuffix(candidate.ID, "-0") {
			// Only the persisted canonical generated identity supplies the index;
			// never turn it into an invented Scope heading or coding-unit ID.
			mark(candidate.ID, "foundation candidate (generated output index 0; legacy unanchored ref)")
		}
	}
	var ids []string
	for id := range discovered {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		for _, reason := range discovered[id] {
			add(id, reason)
		}
	}
	return providers, issues
}

func plannerInterfaceOwners(state *models.State, self, consumed string) []string {
	var exact, tokenMatches []string
	token := plannerInterfaceToken(consumed)
	for _, task := range state.Tasks {
		if task.ID == self || task.Decomposition == nil {
			continue
		}
		for _, owned := range task.Decomposition.InterfacesOwned {
			if owned == consumed {
				exact = appendUniqueString(exact, task.ID)
			} else if token != "" && plannerInterfaceToken(owned) == token {
				tokenMatches = appendUniqueString(tokenMatches, task.ID)
			}
		}
	}
	if len(exact) > 0 {
		slices.Sort(exact)
		return exact
	}
	slices.Sort(tokenMatches)
	return tokenMatches
}

func plannerInterfaceToken(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSuffix(fields[0], ":")
}

// plannerTaskKind uses persisted type, with structural historical fallback.
// A planning output may inherit arch_ref, so plan outputs take precedence.
func plannerTaskKind(task *models.Task) models.TaskType {
	if task.Type != "" {
		return task.Type
	}
	for _, output := range task.Output {
		if output.PlanRef != "" {
			return models.TaskTypePlanning
		}
	}
	for _, output := range task.Output {
		if output.ArchRef != "" {
			return models.TaskTypeArchitecture
		}
	}
	if task.PlanRef != "" {
		return models.TaskTypeCoding
	}
	if task.ArchRef != "" {
		return models.TaskTypePlanning
	}
	return ""
}

func plannerLineage(provider *models.Task, state *models.State) []*models.Task {
	children := taskChildrenByParent(state)
	for _, cohort := range children {
		sort.Slice(cohort, func(i, j int) bool { return cohort[i].ID < cohort[j].ID })
	}
	queue := []*models.Task{provider}
	seen := map[string]bool{}
	var lineage []*models.Task
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if seen[current.ID] {
			continue
		}
		seen[current.ID] = true
		lineage = append(lineage, current)
		for _, child := range children[current.ID] {
			kind := plannerTaskKind(child)
			if kind == models.TaskTypeArchitecture || kind == models.TaskTypePlanning || kind == models.TaskTypeCoding {
				queue = append(queue, child)
			}
		}
	}
	return lineage
}

func plannerProducedRefs(task *models.Task, kind string) []string {
	var refs []string
	taskKind := plannerTaskKind(task)
	for _, output := range task.Output {
		if kind == "architecture" && taskKind == models.TaskTypeArchitecture && output.ArchRef != "" {
			refs = appendUniqueString(refs, output.ArchRef)
		}
		if kind == "plan" && taskKind == models.TaskTypePlanning && output.PlanRef != "" {
			refs = appendUniqueString(refs, output.PlanRef)
		}
	}
	return refs
}

func plannerArtifactOwners(state *models.State, kind, ref string) []*models.Task {
	var exact, sameFile []*models.Task
	for i := range state.Tasks {
		task := &state.Tasks[i]
		for _, produced := range plannerProducedRefs(task, kind) {
			if produced == ref {
				exact = append(exact, task)
				break
			}
			if paths.SplitRefFile(produced) == paths.SplitRefFile(ref) {
				if !slices.Contains(sameFile, task) {
					sameFile = append(sameFile, task)
				}
			}
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return sameFile
}

// Generated manifest ownership in the consumer's ancestry outranks historical
// tasks elsewhere that reused a path or heading. Search each ancestry layer
// together so a many-parent ambiguity cannot be resolved by iteration order.
func plannerArtifactOwnersForTask(state *models.State, consumer *models.Task, kind, ref string) []*models.Task {
	queue := []*models.Task{consumer}
	seen := map[string]bool{}
	var closestFileOwners []*models.Task
	for len(queue) > 0 {
		var next, exact, sameFile []*models.Task
		for _, candidate := range queue {
			if candidate == nil || seen[candidate.ID] {
				continue
			}
			seen[candidate.ID] = true
			for _, produced := range plannerProducedRefs(candidate, kind) {
				if produced == ref {
					if !slices.Contains(exact, candidate) {
						exact = append(exact, candidate)
					}
				} else if paths.SplitRefFile(produced) == paths.SplitRefFile(ref) && !slices.Contains(sameFile, candidate) {
					sameFile = append(sameFile, candidate)
				}
			}
			for _, parentID := range candidate.EffectiveParentTasks() {
				if parent := state.FindTask(parentID); parent != nil {
					next = append(next, parent)
				}
			}
		}
		if len(exact) > 0 {
			return exact
		}
		if len(closestFileOwners) == 0 && len(sameFile) > 0 {
			closestFileOwners = sameFile
		}
		queue = next
	}
	if len(closestFileOwners) > 0 {
		return closestFileOwners
	}
	return plannerArtifactOwners(state, kind, ref)
}

func plannerArtifactPointer(producer *models.Task, kind, ref string) prompts.PlannerArtifactPointer {
	pointer := prompts.PlannerArtifactPointer{Kind: kind, Ref: ref, Refs: []string{ref}, File: paths.SplitRefFile(ref), ProducerID: producer.ID}
	if producer.Status != models.TaskStatusMerged {
		pointer.Unresolved = "producer is " + string(producer.Status) + "; unavailable as merged authority"
	} else if producer.ReviewCommit == nil || *producer.ReviewCommit == "" {
		pointer.Unresolved = "merged producer has no reviewed commit attribution"
	} else {
		pointer.Commit = *producer.ReviewCommit
	}
	return pointer
}

func plannerProviderArtifacts(provider *models.Task, state *models.State) ([]prompts.PlannerArtifactPointer, string) {
	lineage := plannerLineage(provider, state)
	var artifacts []prompts.PlannerArtifactPointer
	positions := map[string]int{}
	add := func(kind, ref string, producer, consumer *models.Task) int {
		var owners []*models.Task
		if producer == nil && ref != "" {
			owners = plannerArtifactOwnersForTask(state, consumer, kind, ref)
			if len(owners) == 1 {
				producer = owners[0]
			}
		}
		key := kind + "\x00" + paths.SplitRefFile(ref)
		if producer != nil {
			key += "\x00" + producer.ID
		} else if len(owners) > 1 {
			var ownerIDs []string
			for _, owner := range owners {
				ownerIDs = append(ownerIDs, owner.ID)
			}
			slices.Sort(ownerIDs)
			key += "\x00ambiguous\x00" + strings.Join(ownerIDs, "\x00")
		}
		if position, ok := positions[key]; ok {
			artifacts[position].Refs = appendUniqueString(artifacts[position].Refs, ref)
			return position
		}
		pointer := prompts.PlannerArtifactPointer{Kind: kind, Ref: ref, Refs: []string{ref}, File: paths.SplitRefFile(ref), Unresolved: "no producing task attribution"}
		if producer != nil {
			pointer = plannerArtifactPointer(producer, kind, ref)
		} else if len(owners) > 1 {
			var ids []string
			for _, owner := range owners {
				ids = append(ids, owner.ID)
			}
			slices.Sort(ids)
			pointer.Unresolved = "ambiguous producing tasks [" + strings.Join(ids, ", ") + "]"
		}
		positions[key] = len(artifacts)
		artifacts = append(artifacts, pointer)
		return len(artifacts) - 1
	}
	// Outputs supply author attribution, while scalar refs name inputs and
	// must resolve through the actual producer rather than the consuming task.
	for _, task := range lineage {
		for _, kind := range []string{"architecture", "plan"} {
			for _, ref := range plannerProducedRefs(task, kind) {
				add(kind, ref, task, task)
			}
		}
	}
	for _, task := range lineage {
		if plannerTaskKind(task) == models.TaskTypeCoding && task.PlanRef == "" {
			position := add("plan", "", nil, task)
			artifacts[position].Unresolved = "coding descendants have no plan_ref"
			artifacts[position].Units = append(artifacts[position].Units, prompts.PlannerUnitPointer{ID: task.ID, Status: string(task.Status)})
		}
		namedRefs := []struct{ kind, ref string }{{"architecture", task.ArchRef}, {"plan", task.PlanRef}}
		for _, output := range task.Output {
			namedRefs = append(namedRefs, struct{ kind, ref string }{"architecture", output.ArchRef}, struct{ kind, ref string }{"plan", output.PlanRef})
		}
		for _, named := range namedRefs {
			if named.ref == "" {
				continue
			}
			position := add(named.kind, named.ref, nil, task)
			if named.kind == "plan" && named.ref == task.PlanRef && plannerTaskKind(task) == models.TaskTypeCoding {
				artifacts[position].Units = append(artifacts[position].Units, prompts.PlannerUnitPointer{ID: task.ID, Status: string(task.Status), PlanRef: task.PlanRef})
			}
		}
	}
	for i := range artifacts {
		sort.Slice(artifacts[i].Units, func(a, b int) bool { return artifacts[i].Units[a].ID < artifacts[i].Units[b].ID })
	}
	if len(provider.Output) == 0 {
		return artifacts, "no produced output recorded"
	}
	if len(artifacts) == 0 {
		return artifacts, "no architecture or plan artifact recorded"
	}
	return artifacts, ""
}

func plannerFormatPrecedent(task *models.Task, state *models.State, role string) *prompts.PlannerArtifactPointer {
	kind := "architecture"
	if role == models.RoleCodePlanner {
		kind = "plan"
	}
	var candidates []*models.Task
	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if candidate.ID != task.ID && candidate.RolePair == task.RolePair && candidate.Status == models.TaskStatusMerged && candidate.ReviewCommit != nil && *candidate.ReviewCommit != "" && len(plannerProducedRefs(candidate, kind)) > 0 {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		aSibling := task.CohortParentID() != "" && a.CohortParentID() == task.CohortParentID()
		bSibling := task.CohortParentID() != "" && b.CohortParentID() == task.CohortParentID()
		if aSibling != bSibling {
			return aSibling
		}
		if !plannerMergedAt(a).Equal(plannerMergedAt(b)) {
			return plannerMergedAt(a).After(plannerMergedAt(b))
		}
		return a.ID < b.ID
	})
	if len(candidates) == 0 {
		return nil
	}
	producer := candidates[0]
	pointer := plannerArtifactPointer(producer, kind, plannerProducedRefs(producer, kind)[0])
	return &pointer
}

func plannerMergedAt(task *models.Task) time.Time {
	var latest time.Time
	for _, history := range task.History {
		if history.Event == models.TaskEventMerged && history.Time.After(latest) {
			latest = history.Time
		}
	}
	return latest
}

var plannerCitationPattern = regexp.MustCompile("(?m)(?:`([^`\\n]+\\.[mM][dD]):([0-9]+)(?:[-–][0-9]+)?`|([A-Za-z0-9_./+@-]+\\.[mM][dD]):([0-9]+))")

func populatePlannerRework(task *models.Task, repo *git.Git, data *prompts.RoleContextData) error {
	if task.Iteration < 2 || data.PriorRejection == "" {
		return nil
	}
	var rejection *models.TaskHistoryEntry
	for i := len(task.History) - 1; i >= 0; i-- {
		if task.History[i].Event == models.TaskEventRejected || task.History[i].Event == models.TaskEventReviewVerdictRejected {
			rejection = &task.History[i]
			break
		}
	}
	if rejection == nil || rejection.Commit == nil || *rejection.Commit == "" {
		data.PlannerReworkUnresolved = append(data.PlannerReworkUnresolved, "latest rejection has no retained commit")
		return nil
	}
	commit := *rejection.Commit
	feedback := data.PriorRejection
	if rejection.Reason != nil && *rejection.Reason != "" {
		feedback = *rejection.Reason
	}
	if rejection.Note != nil {
		feedback += "\n" + *rejection.Note
	}
	var lookups []git.BlobPath
	var files []string
	matches := plannerCitationPattern.FindAllStringSubmatch(feedback, -1)
	for _, match := range matches {
		file := match[1]
		if file == "" {
			file = match[3]
		}
		if !slices.Contains(files, file) {
			files = append(files, file)
			lookups = append(lookups, git.BlobPath{Revision: commit, Path: file})
		}
	}
	if len(files) == 0 {
		return nil
	}
	objects, err := repo.BlobOIDs(lookups)
	if err != nil {
		return fmt.Errorf("resolve planner rejection citations: %w", err)
	}
	content := map[string]string{}
	for i, file := range files {
		if objects[i].Err != nil {
			data.PlannerReworkUnresolved = append(data.PlannerReworkUnresolved, file+" @ "+commit+": "+objects[i].Err.Error())
			continue
		}
		body, err := repo.ReadBlob(commit, file)
		if err != nil {
			return fmt.Errorf("read planner rejection citation %q: %w", file, err)
		}
		content[file] = body
	}
	positions := map[string]int{}
	for _, match := range matches {
		file, lineText := match[1], match[2]
		if file == "" {
			file, lineText = match[3], match[4]
		}
		body, exists := content[file]
		if !exists {
			continue
		}
		line, _ := strconv.Atoi(lineText)
		heading, start, end, err := referencecontract.SectionAtLine(body, line)
		if err != nil {
			data.PlannerReworkUnresolved = appendUniqueString(data.PlannerReworkUnresolved, fmt.Sprintf("%s:%s @ %s: %v", file, lineText, commit, err))
			continue
		}
		key := file + "\x00" + heading
		if position, ok := positions[key]; ok {
			if !slices.Contains(data.PlannerReworkSections[position].CitedLines, line) {
				data.PlannerReworkSections[position].CitedLines = append(data.PlannerReworkSections[position].CitedLines, line)
			}
			continue
		}
		positions[key] = len(data.PlannerReworkSections)
		data.PlannerReworkSections = append(data.PlannerReworkSections, prompts.PlannerReworkPointer{File: file, Heading: heading, Commit: commit, StartLine: start, EndLine: end, CitedLines: []int{line}})
	}
	return nil
}
