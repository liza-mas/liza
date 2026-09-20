package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/liza-mas/liza/internal/db"
	"github.com/liza-mas/liza/internal/git"
	"github.com/liza-mas/liza/internal/models"
)

// AcceptanceCommitRemap records one merged planning parent whose acceptance
// evidence was rewritten, for operator review.
type AcceptanceCommitRemap struct {
	TaskID         string            `json:"task_id" yaml:"task_id"`
	Replaced       map[string]string `json:"replaced" yaml:"replaced"`
	HistoryEntries int               `json:"history_entries" yaml:"history_entries"`
}

// RepairAcceptanceCommitsResult reports what the repair changed and what it
// refused.
type RepairAcceptanceCommitsResult struct {
	Repaired []AcceptanceCommitRemap `json:"repaired" yaml:"repaired"`
	Skipped  map[string]string       `json:"skipped" yaml:"skipped"`
}

// RepairAcceptanceCommits restores the acceptance evidence of merged planning
// parents whose commits were rewritten out of the integration branch.
//
// Rebasing an integration branch that already carries merges gives every
// commit a new object id. A merged planning parent then names commits that are
// no longer ancestors of integration, and acceptance allocation refuses every
// child: "requires allocation by a direct independently approved merged
// planning parent". No amount of re-claiming recovers it, because the parent
// is MERGED and immutable through the normal lifecycle.
//
// The replacement for each orphaned commit is derived here, by patch id,
// against the integration branch: a rebase preserves content identity, so the
// rewritten commit is the one whose diff is identical. Derivation is
// deliberately internal. Accepting an operator-supplied commit would turn this
// into a way to declare any commit reviewed, which is exactly the property
// acceptance evidence exists to prevent.
//
// A parent is repaired only when every one of its commits has an identical
// replacement. Anything else — an ambiguous match, a merge commit with no
// content identity, a parent whose commits are still reachable — is skipped
// with a reason and left for inspection.
func RepairAcceptanceCommits(bb *db.Blackboard, projectRoot, integrationRef string) (RepairAcceptanceCommitsResult, error) {
	return repairAcceptanceCommits(bb, projectRoot, integrationRef, false)
}

// PlanAcceptanceCommitRepair derives the same mapping without writing it, so an
// operator can audit an evidence rewrite before it happens.
func PlanAcceptanceCommitRepair(bb *db.Blackboard, projectRoot, integrationRef string) (RepairAcceptanceCommitsResult, error) {
	return repairAcceptanceCommits(bb, projectRoot, integrationRef, true)
}

func repairAcceptanceCommits(bb *db.Blackboard, projectRoot, integrationRef string, dryRun bool) (RepairAcceptanceCommitsResult, error) {
	result := RepairAcceptanceCommitsResult{Skipped: map[string]string{}}
	if integrationRef == "" {
		return result, fmt.Errorf("acceptance commit repair requires an integration ref")
	}

	g := git.New(projectRoot)
	integration, err := g.ResolveCommit(integrationRef)
	if err != nil {
		return result, fmt.Errorf("resolve integration ref %q: %w", integrationRef, err)
	}

	state, err := bb.Read()
	if err != nil {
		return result, fmt.Errorf("read state: %w", err)
	}

	candidates := orphanedAcceptanceParents(g, state, integration)
	if len(candidates) == 0 {
		return result, nil
	}

	index, err := integrationPatchIndex(g, integration)
	if err != nil {
		return result, err
	}

	plans := make(map[string]map[string]string)
	for _, taskID := range candidates {
		task := state.FindTask(taskID)
		mapping, skipErr := planAcceptanceRemap(g, task, index, integration)
		if skipErr != nil {
			result.Skipped[taskID] = skipErr.Error()
			continue
		}
		if len(mapping) > 0 {
			plans[taskID] = mapping
		}
	}
	if len(plans) == 0 {
		return result, nil
	}

	if dryRun {
		for taskID, mapping := range plans {
			task := state.FindTask(taskID)
			result.Repaired = append(result.Repaired, AcceptanceCommitRemap{
				TaskID: taskID, Replaced: mapping, HistoryEntries: countRemappedHistory(task, mapping),
			})
		}
		sort.Slice(result.Repaired, func(i, j int) bool { return result.Repaired[i].TaskID < result.Repaired[j].TaskID })
		return result, nil
	}

	now := time.Now().UTC()
	err = bb.Modify(func(s *models.State) error {
		for taskID, mapping := range plans {
			task := s.FindTask(taskID)
			if task == nil {
				return fmt.Errorf("task %s disappeared during acceptance commit repair", taskID)
			}
			hits := applyAcceptanceRemap(task, mapping, integration, now)
			result.Repaired = append(result.Repaired, AcceptanceCommitRemap{
				TaskID: taskID, Replaced: mapping, HistoryEntries: hits,
			})
		}
		return nil
	})
	if err != nil {
		return RepairAcceptanceCommitsResult{Skipped: map[string]string{}}, fmt.Errorf("write repaired acceptance evidence: %w", err)
	}

	sort.Slice(result.Repaired, func(i, j int) bool { return result.Repaired[i].TaskID < result.Repaired[j].TaskID })
	return result, nil
}

// orphanedAcceptanceParents finds merged planning parents of non-merged tasks
// whose merge commit is no longer reachable from integration.
func orphanedAcceptanceParents(g *git.Git, state *models.State, integration string) []string {
	needed := map[string]bool{}
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status == models.TaskStatusMerged {
			continue
		}
		for _, parentID := range task.EffectiveParentTasks() {
			parent := state.FindTask(parentID)
			if parent == nil || parent.Status != models.TaskStatusMerged {
				continue
			}
			if parent.MergeCommit == nil || parent.ReviewCommit == nil || parent.BaseCommit == nil {
				continue
			}
			reachable, err := g.IsAncestor(*parent.MergeCommit, integration)
			if err == nil && reachable {
				continue
			}
			needed[parentID] = true
		}
	}
	out := make([]string, 0, len(needed))
	for id := range needed {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// integrationPatchIndex maps content identity to the commit carrying it on the
// integration branch. Ambiguous identities are recorded as empty so a caller
// can refuse them rather than pick one.
func integrationPatchIndex(g *git.Git, integration string) (map[string]string, error) {
	commits, err := g.CommitsIn(integration, 0)
	if err != nil {
		return nil, err
	}
	index := make(map[string]string, len(commits))
	for _, commit := range commits {
		id, idErr := g.PatchID(commit)
		if idErr != nil || id == "" {
			continue
		}
		if existing, seen := index[id]; seen && existing != commit {
			index[id] = ""
			continue
		}
		index[id] = commit
	}
	return index, nil
}

// planAcceptanceRemap derives the old->new commit mapping for one parent, or
// explains why the parent cannot be repaired.
func planAcceptanceRemap(g *git.Git, parent *models.Task, index map[string]string, integration string) (map[string]string, error) {
	if parent == nil {
		return nil, fmt.Errorf("task not found")
	}
	mapping := map[string]string{}
	for _, commit := range acceptanceCommitsOf(parent) {
		if _, done := mapping[commit]; done {
			continue
		}
		reachable, err := g.IsAncestor(commit, integration)
		if err == nil && reachable {
			continue // already good; leave it exactly as it is
		}
		id, idErr := g.PatchID(commit)
		if idErr != nil {
			return nil, fmt.Errorf("commit %s: %v", shortSHA(commit), idErr)
		}
		if id == "" {
			// A merge commit has no diff of its own, so it cannot be matched
			// by content. A rebase drops it and lands the reviewed work as a
			// plain commit on integration; the reviewed commit's replacement
			// is then the point where that work entered integration, which
			// is what merge_commit records. Only merge_commit may be resolved
			// this way, and only from a review commit that itself matched.
			if parent.MergeCommit == nil || commit != *parent.MergeCommit {
				return nil, fmt.Errorf("commit %s has no content identity (merge commit) and is not merge_commit; cannot match it by content", shortSHA(commit))
			}
			replacement, ok := mergedReviewReplacement(g, parent, mapping, index, integration)
			if !ok {
				return nil, fmt.Errorf("merge commit %s was dropped and the reviewed commit has no verified replacement to stand in for it", shortSHA(commit))
			}
			mapping[commit] = replacement
			continue
		}
		replacement, found := index[id]
		if !found {
			return nil, fmt.Errorf("commit %s has no content-identical replacement on integration", shortSHA(commit))
		}
		if replacement == "" {
			return nil, fmt.Errorf("commit %s matches more than one commit on integration", shortSHA(commit))
		}
		mapping[commit] = replacement
	}
	return mapping, nil
}

// acceptanceCommitsOf lists every commit that acceptance evidence reads for a
// parent, including the submission history entries that must agree with the
// reviewed commit.
func acceptanceCommitsOf(parent *models.Task) []string {
	commits := []string{}
	for _, c := range []*string{parent.BaseCommit, parent.ReviewCommit, parent.MergeCommit} {
		if c != nil && *c != "" {
			commits = append(commits, *c)
		}
	}
	for _, entry := range parent.History {
		if entry.Event == models.TaskEventSubmittedForReview && entry.Commit != nil && *entry.Commit != "" {
			commits = append(commits, *entry.Commit)
		}
	}
	return commits
}

// applyAcceptanceRemap rewrites every mapped commit on the parent, reports
// how many history entries were touched, and records the rewrite itself.
//
// The history entry is the compensating control for rewriting approval
// evidence: after it, a child's acceptance source names a review commit the
// parent's history never recorded a submission for, and the entry is what
// explains that to the next reader. The old->new pairs are carried verbatim.
func applyAcceptanceRemap(parent *models.Task, mapping map[string]string, integration string, now time.Time) int {
	replace := func(field **string) {
		if *field == nil {
			return
		}
		if next, ok := mapping[**field]; ok {
			updated := next
			*field = &updated
		}
	}
	replace(&parent.BaseCommit)
	replace(&parent.ReviewCommit)
	replace(&parent.MergeCommit)

	hits := 0
	for i := range parent.History {
		entry := &parent.History[i]
		if entry.Commit == nil {
			continue
		}
		if next, ok := mapping[*entry.Commit]; ok {
			updated := next
			entry.Commit = &updated
			hits++
		}
	}

	olds := make([]string, 0, len(mapping))
	for old := range mapping {
		olds = append(olds, old)
	}
	sort.Strings(olds)
	pairs := make([]string, 0, len(olds))
	replaced := make(map[string]any, len(olds))
	for _, old := range olds {
		pairs = append(pairs, shortSHA(old)+"->"+shortSHA(mapping[old]))
		replaced[old] = mapping[old]
	}
	note := fmt.Sprintf("acceptance evidence remapped by content identity onto %s: %s", shortSHA(integration), strings.Join(pairs, ", "))
	reason := "integration branch rewritten after this parent merged; commits matched by patch id"
	parent.History = append(parent.History, models.TaskHistoryEntry{
		Time:   now,
		Event:  string(models.TaskEventAcceptanceCommitsRemapped),
		Reason: &reason,
		Note:   &note,
		Extra: map[string]any{
			"replaced":        replaced,
			"integration":     integration,
			"history_entries": hits,
		},
	})
	return hits
}

// ShortCommit abbreviates a commit id for display without assuming its length.
func ShortCommit(commit string) string { return shortSHA(commit) }

// countRemappedHistory reports how many submission entries a mapping would
// touch, without modifying the task.
func countRemappedHistory(parent *models.Task, mapping map[string]string) int {
	if parent == nil {
		return 0
	}
	hits := 0
	for _, entry := range parent.History {
		if entry.Commit == nil {
			continue
		}
		if _, ok := mapping[*entry.Commit]; ok {
			hits++
		}
	}
	return hits
}

// mergedReviewReplacement finds where a parent's reviewed work landed on
// integration once its merge commit was dropped: the reviewed commit's own
// content-identical replacement, provided it is reachable from integration.
func mergedReviewReplacement(g *git.Git, parent *models.Task, mapping map[string]string, index map[string]string, integration string) (string, bool) {
	if parent.ReviewCommit == nil || *parent.ReviewCommit == "" {
		return "", false
	}
	review := *parent.ReviewCommit
	replacement, ok := mapping[review]
	if !ok {
		reachable, err := g.IsAncestor(review, integration)
		if err == nil && reachable {
			replacement = review
		} else {
			id, idErr := g.PatchID(review)
			if idErr != nil || id == "" {
				return "", false
			}
			replacement, ok = index[id]
			if !ok || replacement == "" {
				return "", false
			}
		}
	}
	reachable, err := g.IsAncestor(replacement, integration)
	if err != nil || !reachable {
		return "", false
	}
	return replacement, true
}
