package models

import (
	"fmt"
	"strings"
)

// DependencySatisfactionKind classifies why a dependency does or does not
// satisfy a downstream task.
type DependencySatisfactionKind string

const (
	DependencySatisfiedDirect               DependencySatisfactionKind = "satisfied_direct"
	DependencySatisfiedViaSupersession      DependencySatisfactionKind = "satisfied_via_supersession"
	DependencyUnsatisfiedPending            DependencySatisfactionKind = "unsatisfied_pending"
	DependencyUnsatisfiedPendingReplacement DependencySatisfactionKind = "unsatisfied_pending_replacement"
	DependencyUnsatisfiedSuperseded         DependencySatisfactionKind = "unsatisfied_superseded"
	DependencyInvalidMissing                DependencySatisfactionKind = "invalid_missing"
	DependencyInvalidCycle                  DependencySatisfactionKind = "invalid_cycle"
	DependencyInvalidProvider               DependencySatisfactionKind = "invalid_provider"
)

// DependencySatisfaction describes the resolved state of one depends_on target.
// Path is a flattened traversal path through superseded_by replacements; split
// replacement branches may appear as one compact list for diagnostics.
type DependencySatisfaction struct {
	DependencyID string
	Kind         DependencySatisfactionKind
	Path         []string
	BlockingIDs  []string
	Reason       string
}

// Satisfied reports whether this dependency is safe for downstream work to run.
func (d DependencySatisfaction) Satisfied() bool {
	return d.Kind == DependencySatisfiedDirect || d.Kind == DependencySatisfiedViaSupersession
}

// Invalid reports whether the dependency graph itself is malformed.
func (d DependencySatisfaction) Invalid() bool {
	return d.Kind == DependencyInvalidMissing || d.Kind == DependencyInvalidCycle || d.Kind == DependencyInvalidProvider
}

// ViaSupersession reports whether this result was affected by a superseded task.
func (d DependencySatisfaction) ViaSupersession() bool {
	return d.Kind == DependencySatisfiedViaSupersession ||
		d.Kind == DependencyUnsatisfiedPendingReplacement ||
		d.Kind == DependencyUnsatisfiedSuperseded ||
		(d.Invalid() && len(d.Path) > 1)
}

// Summary returns a compact human-readable description for diagnostics.
func (d DependencySatisfaction) Summary() string {
	if len(d.Path) == 0 {
		if d.Reason != "" {
			return fmt.Sprintf("%s (%s): %s", d.DependencyID, d.Kind, d.Reason)
		}
		return fmt.Sprintf("%s (%s)", d.DependencyID, d.Kind)
	}
	summary := fmt.Sprintf("%s (%s via %s)", d.DependencyID, d.Kind, strings.Join(d.Path, " -> "))
	if len(d.BlockingIDs) > 0 {
		summary += fmt.Sprintf("; blocking: %s", strings.Join(d.BlockingIDs, ", "))
	}
	if d.Reason != "" {
		summary += "; " + d.Reason
	}
	return summary
}

// DependencyResolver resolves task dependencies through superseded_by chains.
type DependencyResolver struct {
	state *State
	cache map[string]DependencySatisfaction
}

// NewDependencyResolver creates a resolver with per-pass memoization.
func NewDependencyResolver(state *State) *DependencyResolver {
	return &DependencyResolver{
		state: state,
		cache: make(map[string]DependencySatisfaction),
	}
}

// ResolveDependency resolves a dependency with a one-shot resolver. Use
// NewDependencyResolver when resolving dependencies in a loop so results are
// memoized across the pass.
func (s *State) ResolveDependency(depID string) DependencySatisfaction {
	return NewDependencyResolver(s).Resolve(depID)
}

// Resolve resolves one task ID as a dependency target.
func (r *DependencyResolver) Resolve(depID string) DependencySatisfaction {
	return r.resolve(depID, nil, nil)
}

// UnmetDependencies returns every dependency that is not satisfied.
func (r *DependencyResolver) UnmetDependencies(task *Task, resolvers ...PipelineResolver) []DependencySatisfaction {
	if task == nil {
		return nil
	}
	var unmet []DependencySatisfaction
	for _, depID := range task.DependsOn {
		result := r.Resolve(depID)
		if !result.Satisfied() {
			unmet = append(unmet, result)
		}
	}
	var pr PipelineResolver
	if len(resolvers) > 0 {
		pr = resolvers[0]
	}
	var tasks []Task
	if r != nil && r.state != nil {
		tasks = r.state.Tasks
	}
	unmet = append(unmet, UnmetProviderDependencies(task, tasks, pr)...)
	return unmet
}

func (r *DependencyResolver) resolve(depID string, stack []string, visiting map[string]bool) DependencySatisfaction {
	if r == nil || r.state == nil {
		return DependencySatisfaction{
			DependencyID: depID,
			Kind:         DependencyInvalidMissing,
			Path:         []string{depID},
			BlockingIDs:  []string{depID},
		}
	}
	if result, ok := r.cache[depID]; ok {
		return result
	}
	if visiting == nil {
		visiting = make(map[string]bool)
	}
	if visiting[depID] {
		path := append(append([]string(nil), stack...), depID)
		result := DependencySatisfaction{
			DependencyID: depID,
			Kind:         DependencyInvalidCycle,
			Path:         path,
			BlockingIDs:  []string{depID},
		}
		r.cache[depID] = result
		return result
	}

	task := r.state.FindTask(depID)
	if task == nil {
		result := DependencySatisfaction{
			DependencyID: depID,
			Kind:         DependencyInvalidMissing,
			Path:         []string{depID},
			BlockingIDs:  []string{depID},
		}
		r.cache[depID] = result
		return result
	}

	switch task.Status {
	case TaskStatusMerged:
		result := DependencySatisfaction{
			DependencyID: depID,
			Kind:         DependencySatisfiedDirect,
			Path:         []string{depID},
		}
		r.cache[depID] = result
		return result
	case TaskStatusSuperseded:
		if len(task.SupersededBy) == 0 {
			result := DependencySatisfaction{
				DependencyID: depID,
				Kind:         DependencyUnsatisfiedSuperseded,
				Path:         []string{depID},
				BlockingIDs:  []string{depID},
			}
			r.cache[depID] = result
			return result
		}
		return r.resolveSuperseded(task, stack, visiting)
	default:
		result := DependencySatisfaction{
			DependencyID: depID,
			Kind:         DependencyUnsatisfiedPending,
			Path:         []string{depID},
			BlockingIDs:  []string{depID},
		}
		r.cache[depID] = result
		return result
	}
}

func (r *DependencyResolver) resolveSuperseded(task *Task, stack []string, visiting map[string]bool) DependencySatisfaction {
	depID := task.ID
	nextStack := append(append([]string(nil), stack...), depID)
	nextVisiting := copyStringBoolMap(visiting)
	nextVisiting[depID] = true

	result := DependencySatisfaction{
		DependencyID: depID,
		Kind:         DependencySatisfiedViaSupersession,
		Path:         []string{depID},
	}
	for _, replacementID := range task.SupersededBy {
		replacement := r.resolve(replacementID, nextStack, nextVisiting)
		result.Path = appendUniqueStringsLocal(result.Path, replacement.Path...)
		if replacement.Satisfied() {
			continue
		}
		result.BlockingIDs = appendUniqueStringsLocal(result.BlockingIDs, replacement.BlockingIDs...)
		switch replacement.Kind {
		case DependencyInvalidCycle:
			result.Kind = DependencyInvalidCycle
		case DependencyInvalidMissing:
			if result.Kind != DependencyInvalidCycle {
				result.Kind = DependencyInvalidMissing
			}
		case DependencyUnsatisfiedSuperseded:
			if result.Kind != DependencyInvalidCycle && result.Kind != DependencyInvalidMissing {
				result.Kind = DependencyUnsatisfiedSuperseded
			}
		default:
			if result.Kind != DependencyInvalidCycle && result.Kind != DependencyInvalidMissing {
				result.Kind = DependencyUnsatisfiedPendingReplacement
			}
		}
	}
	if !result.Satisfied() && len(result.BlockingIDs) == 0 {
		result.BlockingIDs = []string{depID}
	}
	r.cache[depID] = result
	return result
}

func copyStringBoolMap(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func appendUniqueStringsLocal(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		seen := false
		for _, value := range values {
			if value == candidate {
				seen = true
				break
			}
		}
		if !seen {
			values = append(values, candidate)
		}
	}
	return values
}
