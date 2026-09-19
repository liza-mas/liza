package ops

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/errors"
	"github.com/liza-mas/liza/internal/log"
	"github.com/liza-mas/liza/internal/models"
	"github.com/liza-mas/liza/internal/paths"
	"github.com/liza-mas/liza/internal/pipeline"
	"github.com/liza-mas/liza/internal/statevalidate"
)

const narrowInheritedDependenciesOperation = "narrow-inherited-dependencies"

// NarrowSelection authors inherit_inputs for one output entry of a producer
// after its children were generated. Index names the producer's output[]
// position, which is also the generated child's position.
type NarrowSelection struct {
	Index         int                   `yaml:"index" json:"index"`
	InheritInputs *models.InheritInputs `yaml:"inherit_inputs" json:"inherit_inputs"`
}

// NarrowSelectionsFile is the on-disk shape accepted by the CLI.
type NarrowSelectionsFile struct {
	Outputs []NarrowSelection `yaml:"outputs" json:"outputs"`
}

// NarrowedChild reports what happened to one generated child.
type NarrowedChild struct {
	TaskID                string   `json:"task_id"`
	OutputIndex           int      `json:"output_index"`
	Status                string   `json:"status"`
	Action                string   `json:"action"` // narrowed | unchanged | skipped
	SkipReason            string   `json:"skip_reason,omitempty"`
	RemovedDependencies   []string `json:"removed_dependencies,omitempty"`
	AddedDependencies     []string `json:"added_dependencies,omitempty"`
	CanonicalDependencies []string `json:"canonical_dependencies,omitempty"`
}

// NarrowInheritedDependenciesResult contains the outcome of narrowing one
// producer's generated children.
type NarrowInheritedDependenciesResult struct {
	models.LifecycleOutcome
	ProducerID string          `json:"producer_id"`
	Transition string          `json:"transition"`
	Children   []NarrowedChild `json:"children"`
	Narrowed   int             `json:"narrowed"`
	Unchanged  int             `json:"unchanged"`
	Skipped    int             `json:"skipped"`
	Warnings   []string        `json:"warnings,omitempty"`
}

func (r *NarrowInheritedDependenciesResult) GetWarnings() []string {
	if r == nil {
		return nil
	}
	return r.Warnings
}

// LoadNarrowSelectionsFile parses a YAML or JSON selections file.
func LoadNarrowSelectionsFile(path string) ([]NarrowSelection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot read selections file: %v", err)}
	}
	var file NarrowSelectionsFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return nil, &PreconditionError{Reason: fmt.Sprintf("cannot parse selections file: %v", err)}
	}
	if len(file.Outputs) == 0 {
		return nil, &PreconditionError{Reason: "selections file names no outputs"}
	}
	return file.Outputs, nil
}

// NarrowInheritedDependencies applies post-hoc inherit_inputs to a MERGED
// producer's output[] and rewrites the depends_on of its generated children
// that are still in their initial status, removing the inherited phase-gate
// edges the selection does not keep (ADR-0137 applied after generation).
//
// It is a metadata repair operation. Terminal, claimed, blocked, executing,
// and reviewing children are left untouched and reported as skipped. Edges
// that did not come from phase-gate inheritance (siblings, task_depends_on,
// manual retargets) are never removed. It fails closed: any selection that
// cannot be resolved against the current upstream outputs errors the whole
// operation and nothing is written.
func NarrowInheritedDependencies(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason, agentID string) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, agentID, nil)
}

// NarrowInheritedDependenciesWithAuthority fences the rewrite with the
// orchestrator's registration generation.
func NarrowInheritedDependenciesWithAuthority(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason string, authority models.AgentAuthority) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, authority.ID, &authority)
}

// NarrowInheritedDependenciesWithAuthorityAndOptions fences an identified rewrite.
func NarrowInheritedDependenciesWithAuthorityAndOptions(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason string, authority models.AgentAuthority, opts LifecycleRequestOptions) (*NarrowInheritedDependenciesResult, error) {
	return narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName, selections, reason, authority.ID, &authority, opts)
}

func narrowInheritedDependenciesWithOptionalAuthority(projectRoot, producerID, transitionName string, selections []NarrowSelection, reason, agentID string, authority *models.AgentAuthority, options ...LifecycleRequestOptions) (returned *NarrowInheritedDependenciesResult, retErr error) {
	invocation := NewLifecycleInvocation(projectRoot)
	var opts LifecycleRequestOptions
	if len(options) > 0 {
		opts = options[0]
	}
	var observed *models.Task
	effects := "none"
	defer func() {
		retErr = WrapLifecycleError(narrowInheritedDependenciesOperation, observed, retErr, models.LifecycleInvalidInput, "correct_input", "none")
		var outcome models.LifecycleOutcome
		var warnings *[]string
		if returned != nil {
			outcome = returned.LifecycleOutcome
			warnings = &returned.Warnings
		}
		invocation.FinishResult(narrowInheritedDependenciesOperation, outcome, &retErr, warnings)
	}()
	if err := ValidateLifecycleRequestOptions(opts); err != nil {
		return nil, err
	}
	if producerID == "" {
		return nil, &PreconditionError{Reason: "producer task ID is required"}
	}
	if reason == "" {
		return nil, &PreconditionError{Reason: "reason is required"}
	}
	if agentID == "" {
		return nil, &PreconditionError{Reason: "orchestrator agent ID is required"}
	}
	if len(selections) == 0 {
		return nil, &PreconditionError{Reason: "at least one output selection is required"}
	}
	if err := validateNarrowSelectionsShape(selections); err != nil {
		return nil, err
	}

	lp := paths.New(projectRoot)
	bb := db.For(lp.StatePath())
	resolver, _, err := loadResolver(projectRoot)
	if err != nil {
		return nil, WrapLifecycleError(narrowInheritedDependenciesOperation, nil, fmt.Errorf("failed to load pipeline config: %w", err), models.LifecycleStateChanged, "requery", "none")
	}

	var result NarrowInheritedDependenciesResult
	now := time.Now().UTC()

	err = lifecycleMutation(bb, authority)(func(state *models.State) (callbackErr error) {
		defer func() {
			callbackErr = WrapLifecycleError(narrowInheritedDependenciesOperation, observed, callbackErr, models.LifecycleInvalidInput, "correct_input", "none")
		}()
		producer := state.FindTask(producerID)
		if producer == nil {
			return &errors.NotFoundError{Entity: "task", ID: producerID}
		}
		copy := *producer
		observed = &copy

		resolvedTransition, err := resolveNarrowTransition(producer, transitionName, resolver)
		if err != nil {
			return err
		}

		request, err := NewLifecycleRequest(narrowInheritedDependenciesOperation, producer, agentID, authority, opts, struct {
			Transition string
			Selections []NarrowSelection
			Reason     string
		}{resolvedTransition, selections, reason})
		if err != nil {
			return err
		}
		receipt, err := CheckLifecycleRequest(producer, request, state.Agents)
		if err != nil {
			return err
		}
		if receipt != nil {
			result = NarrowInheritedDependenciesResult{ProducerID: producerID, Transition: resolvedTransition, LifecycleOutcome: LifecycleReplayOutcome(producer, receipt, agentID)}
			return errLifecycleReplay
		}

		if producer.Status != models.TaskStatusMerged {
			return &PreconditionError{Reason: fmt.Sprintf("producer %s must be MERGED to narrow its generated children (status %s)", producerID, producer.Status)}
		}
		if producer.TransitionsExecuted["replanned"] {
			return &PreconditionError{Reason: fmt.Sprintf("producer %s was replanned; narrow the replacement planning task instead", producerID)}
		}
		if !producer.TransitionsExecuted[resolvedTransition] {
			return &PreconditionError{Reason: fmt.Sprintf("producer %s has not executed transition %q; there are no generated children to narrow", producerID, resolvedTransition)}
		}
		if len(producer.Output) == 0 {
			return &PreconditionError{Reason: fmt.Sprintf("producer %s has no output[] entries", producerID)}
		}

		td, err := resolver.Transition(resolvedTransition)
		if err != nil {
			return &PreconditionError{Reason: fmt.Sprintf("unknown transition %q: %v", resolvedTransition, err)}
		}
		if td.Cardinality != "per-subtask" {
			return &PreconditionError{Reason: fmt.Sprintf("transition %q has cardinality %q; only per-subtask fan-out can be narrowed", resolvedTransition, td.Cardinality)}
		}
		slug := td.TaskSlugOrName()

		// Author the intent on the producer first so that the persisted
		// selection is what the rewrite below was computed from.
		for _, selection := range selections {
			if selection.Index >= len(producer.Output) {
				return &PreconditionError{Reason: fmt.Sprintf("selection index %d is out of range: producer %s has %d output(s)", selection.Index, producerID, len(producer.Output))}
			}
			if err := models.ValidateInheritInputs(selection.InheritInputs, selection.Index); err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
			producer.Output[selection.Index].InheritInputs = cloneInheritInputs(selection.InheritInputs)
		}

		inherited, err := computeInheritedDeps(state, producer, resolvedTransition, resolver)
		if err != nil {
			return &PreconditionError{Reason: err.Error()}
		}
		wholePhase := dedupeStrings(inherited.all)

		result = NarrowInheritedDependenciesResult{ProducerID: producerID, Transition: resolvedTransition}
		selectedIndexes := make(map[int]bool, len(selections))
		for _, selection := range selections {
			selectedIndexes[selection.Index] = true
		}

		for i, entry := range producer.Output {
			if !selectedIndexes[i] {
				continue
			}
			childID := perSubtaskChildID(producerID, slug, i)
			child := state.FindTask(childID)
			if child == nil {
				return &PreconditionError{Reason: fmt.Sprintf("producer %s has transition %q executed but child %s is missing (needs crash recovery)", producerID, resolvedTransition, childID)}
			}
			report := NarrowedChild{TaskID: childID, OutputIndex: i, Status: string(child.Status)}

			if skip := narrowSkipReason(child, resolver, now); skip != "" {
				report.Action = "skipped"
				report.SkipReason = skip
				result.Children = append(result.Children, report)
				result.Skipped++
				continue
			}

			keep, err := inherited.forEntry(entry, i)
			if err != nil {
				return &PreconditionError{Reason: err.Error()}
			}
			keep = dedupeStrings(keep)

			// Edges the entry declares on its own — sibling indexes and
			// concrete task_depends_on — are protected even when the same ID
			// also arrived through inheritance: the planner named them.
			protected := make(map[string]bool, len(keep)+len(entry.DependsOn)+len(entry.TaskDependsOn))
			for _, dep := range keep {
				protected[dep] = true
			}
			for _, ref := range entry.DependsOn {
				idx, convErr := strconv.Atoi(ref)
				if convErr == nil && idx >= 0 && idx < len(producer.Output) {
					protected[perSubtaskChildID(producerID, slug, idx)] = true
				}
			}
			for _, dep := range entry.TaskDependsOn {
				protected[dep] = true
				canonical, _, err := canonicalizeDependencyID(state, dep, nil)
				if err != nil {
					return &PreconditionError{Reason: fmt.Sprintf("cannot canonicalize concrete dependency %s: %v", dep, err)}
				}
				for _, c := range canonical {
					protected[c] = true
				}
			}

			// Removable edges are exactly the whole-phase inherited children
			// the entry does not keep, in both their generated and their
			// canonical (post-supersession) spellings. Nothing else on the
			// child is touched.
			removable := make(map[string]bool)
			for _, dep := range wholePhase {
				if protected[dep] {
					continue
				}
				canonical, _, err := canonicalizeDependencyID(state, dep, nil)
				if err != nil {
					return &PreconditionError{Reason: fmt.Sprintf("cannot canonicalize inherited dependency %s: %v", dep, err)}
				}
				if slices.ContainsFunc(canonical, func(c string) bool { return protected[c] }) {
					continue
				}
				removable[dep] = true
				for _, c := range canonical {
					removable[c] = true
				}
			}

			var next []string
			var removed []string
			for _, dep := range child.DependsOn {
				if removable[dep] {
					removed = append(removed, dep)
					continue
				}
				next = append(next, dep)
			}
			var added []string
			for _, dep := range keep {
				if !slices.Contains(next, dep) {
					next = append(next, dep)
					added = append(added, dep)
				}
			}
			next = dedupeStrings(next)
			canonical, _, err := canonicalizeConcreteDependencyList(state, resolver, child.ID, child.RolePair, next)
			if err != nil {
				return err
			}
			if err := validateDependencyDirection(state, resolver, child.ID, child.RolePair, canonical); err != nil {
				return err
			}

			report.CanonicalDependencies = append([]string(nil), canonical...)
			if len(removed) == 0 && len(added) == 0 && slices.Equal(canonical, child.DependsOn) {
				report.Action = "unchanged"
				result.Children = append(result.Children, report)
				result.Unchanged++
				continue
			}

			sort.Strings(removed)
			sort.Strings(added)
			report.Action = "narrowed"
			report.RemovedDependencies = removed
			report.AddedDependencies = added
			child.DependsOn = canonical
			note := fmt.Sprintf("narrowed inherited dependencies from %s output[%d]: removed %d, added %d", producerID, i, len(removed), len(added))
			child.History = append(child.History, models.TaskHistoryEntry{
				Time:   now,
				Event:  models.TaskEventDependenciesRewritten,
				Agent:  &agentID,
				Reason: &reason,
				Note:   &note,
				Extra: map[string]any{
					"manual":                 true,
					"operation":              narrowInheritedDependenciesOperation,
					"producer":               producerID,
					"transition":             resolvedTransition,
					"output_index":           i,
					"removed_dependencies":   append([]string(nil), removed...),
					"added_dependencies":     append([]string(nil), added...),
					"canonical_dependencies": append([]string(nil), canonical...),
				},
			})
			result.Children = append(result.Children, report)
			result.Narrowed++
		}

		producerNote := fmt.Sprintf("authored inherit_inputs on %d output(s); narrowed %d, unchanged %d, skipped %d",
			len(selections), result.Narrowed, result.Unchanged, result.Skipped)
		producer.History = append(producer.History, models.TaskHistoryEntry{
			Time:   now,
			Event:  models.TaskEventDependenciesRewritten,
			Agent:  &agentID,
			Reason: &reason,
			Note:   &producerNote,
			Extra: map[string]any{
				"manual":     true,
				"operation":  narrowInheritedDependenciesOperation,
				"transition": resolvedTransition,
				"outputs":    selectedIndexList(selections),
			},
		})

		if err := statevalidate.ValidateState(state, projectRoot, false, io.Discard); err != nil {
			return err
		}

		result.LifecycleOutcome, err = CompleteLifecycleRequest(producer, request, models.LifecycleProjection{}, state.Agents)
		if err == nil {
			effects = "unknown"
		}
		return err
	})
	if isLifecycleReplay(err) {
		return &result, nil
	}
	if err != nil {
		return nil, WrapLifecycleError(narrowInheritedDependenciesOperation, observed, fmt.Errorf("failed to narrow inherited dependencies: %w", err), models.LifecycleStateChanged, "requery", effects)
	}

	logger := log.New(lp.LogPath())
	logEntry := log.Entry{
		Timestamp: now,
		Agent:     agentID,
		Action:    narrowInheritedDependenciesOperation,
		Task:      &producerID,
		Detail:    fmt.Sprintf("%s: narrowed=%d unchanged=%d skipped=%d: %s", result.Transition, result.Narrowed, result.Unchanged, result.Skipped, reason),
	}
	if err := logger.Append(logEntry); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("activity log write failed: %v", err))
	}

	return &result, nil
}

// narrowSkipReason returns why a generated child must not be rewritten, or ""
// when it is safe to narrow. Only children still in their role-pair's initial
// status with no live doer claim are rewritten: anything claimed, executing,
// under review, blocked, or terminal has scheduler state that a dependency
// rewrite would race with.
func narrowSkipReason(child *models.Task, resolver *pipeline.Resolver, now time.Time) string {
	if child.Status.IsTerminal() {
		return fmt.Sprintf("terminal status %s", child.Status)
	}
	initial, err := resolver.InitialStatus(child.RolePair)
	if err != nil {
		return fmt.Sprintf("cannot resolve initial status for role_pair %q: %v", child.RolePair, err)
	}
	if child.Status != initial {
		return fmt.Sprintf("status %s is not the initial status %s", child.Status, initial)
	}
	if child.AssignedTo != nil && !strings.HasPrefix(*child.AssignedTo, "$") &&
		child.LeaseExpires != nil && child.LeaseExpires.After(now) {
		return fmt.Sprintf("claimed by %s with a live lease", *child.AssignedTo)
	}
	return ""
}

func resolveNarrowTransition(producer *models.Task, transitionName string, resolver *pipeline.Resolver) (string, error) {
	if transitionName != "" {
		return transitionName, nil
	}
	var executed []string
	for name, done := range producer.TransitionsExecuted {
		if !done || name == "replanned" {
			continue
		}
		td, err := resolver.Transition(name)
		if err != nil || td.Cardinality != "per-subtask" {
			continue
		}
		executed = append(executed, name)
	}
	switch len(executed) {
	case 0:
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has no executed per-subtask transition; pass --transition explicitly", producer.ID)}
	case 1:
		return executed[0], nil
	default:
		sort.Strings(executed)
		return "", &PreconditionError{Reason: fmt.Sprintf("producer %s has several executed per-subtask transitions (%s); pass --transition explicitly", producer.ID, strings.Join(executed, ", "))}
	}
}

func validateNarrowSelectionsShape(selections []NarrowSelection) error {
	seen := make(map[int]bool, len(selections))
	for i, selection := range selections {
		if selection.Index < 0 {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d]: negative output index %d", i, selection.Index)}
		}
		if seen[selection.Index] {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d]: duplicate output index %d", i, selection.Index)}
		}
		seen[selection.Index] = true
		if selection.InheritInputs == nil {
			return &PreconditionError{Reason: fmt.Sprintf("selections[%d] (output %d): inherit_inputs is required; use mode %q to restore the whole-phase barrier", i, selection.Index, models.InheritModeAll)}
		}
	}
	return nil
}

func cloneInheritInputs(in *models.InheritInputs) *models.InheritInputs {
	if in == nil {
		return nil
	}
	out := &models.InheritInputs{Mode: in.Mode}
	for _, selection := range in.Selections {
		out.Selections = append(out.Selections, models.InputSelection{
			UpstreamTask: selection.UpstreamTask,
			Outputs:      append([]int(nil), selection.Outputs...),
		})
	}
	return out
}

func selectedIndexList(selections []NarrowSelection) []string {
	indexes := make([]int, 0, len(selections))
	for _, selection := range selections {
		indexes = append(indexes, selection.Index)
	}
	sort.Ints(indexes)
	out := make([]string, 0, len(indexes))
	for _, index := range indexes {
		out = append(out, strconv.Itoa(index))
	}
	return out
}
