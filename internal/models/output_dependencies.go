package models

import (
	"fmt"
	"slices"
)

// NonTerminalTasksByKind identifies repo-wide incumbents for output generation
// and its dependency projection. The lexicographically first ID wins; tasks
// retired by this generation are excluded, and BLOCKED tasks remain in flight.
func NonTerminalTasksByKind(state *State, retiring map[string]bool) map[string]string {
	byKind := map[string][]string{}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Kind == "" || retiring[task.ID] || task.Status.IsTerminal() {
			continue
		}
		byKind[task.Kind] = append(byKind[task.Kind], task.ID)
	}
	incumbents := map[string]string{}
	for kind, ids := range byKind {
		slices.Sort(ids)
		incumbents[kind] = ids[0]
	}
	return incumbents
}

// ResolveOutputKindDedup preserves generation's skip reasons and maps every
// skipped position to its incumbent or first emitted sibling.
func ResolveOutputKindDedup(entries []OutputEntry, inFlight map[string]string, parentID, slug string) (skip map[int]string, remap map[int]string) {
	skip = map[int]string{}
	remap = map[int]string{}
	emitted := map[string]string{}
	for i, entry := range entries {
		if entry.Kind == "" {
			continue
		}
		if existingID, found := inFlight[entry.Kind]; found {
			skip[i] = fmt.Sprintf("kind %q already in flight on task %s", entry.Kind, existingID)
			remap[i] = existingID
			continue
		}
		if firstID, duplicate := emitted[entry.Kind]; duplicate {
			skip[i] = fmt.Sprintf("kind %q emitted earlier in same output[] at sibling %s", entry.Kind, firstID)
			remap[i] = firstID
			continue
		}
		emitted[entry.Kind] = fmt.Sprintf("%s-%s-%d", parentID, slug, i)
	}
	return
}

// ResolveOutputSiblings uses the same effective identities for actual child
// generation and for dependency checks before those children exist.
func ResolveOutputSiblings(entries []OutputEntry, inFlight map[string]string, parentID, slug string) ([]string, map[int]string, map[int]string) {
	siblings := make([]string, len(entries))
	for i := range entries {
		siblings[i] = fmt.Sprintf("%s-%s-%d", parentID, slug, i)
	}
	skip, remap := ResolveOutputKindDedup(entries, inFlight, parentID, slug)
	for i, id := range remap {
		siblings[i] = id
	}
	return siblings, skip, remap
}
