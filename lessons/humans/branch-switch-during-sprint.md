---
title: "Branch switching during active sprint corrupts git index"
trigger: "When switching between main and integration while agents are running"
keywords: [git merge, wt_merge, branch switch, staged changes, index corruption, projectRoot]
date: 2026-02-24
---

## Context

Liza's `wt_merge` operation runs `git merge` in the main repository directory (`projectRoot`). This is a porcelain command that updates three things: the ref, the working tree, and the index — all relative to whatever branch is currently checked out in that directory.

## Failure Mode

If you switch the main working tree to a different branch (e.g., `main`) while agents are mid-sprint on `integration`, the merge operations still advance the `integration` ref correctly, but the working tree and index updates apply against the wrong branch context. When you switch back to `integration`, git restores the working tree from HEAD but the index retains stale entries — a patchwork of pre-merge file versions staged as regressions against the correctly merged HEAD.

Symptoms: `git status` shows many staged changes that appear to revert recently merged work. Staged blob SHAs match the parent commits of task merges, not the merge results.

## Solution

**During an active sprint, do not switch branches in the main working tree.** If you need to work on `main`:

1. Use a separate worktree: `git worktree add ../liza-main main`
2. Or wait for the sprint to complete (all tasks terminal) before switching

If you already hit this: the staged changes are safely discardable since all real work lives in the correctly merged commits. Run:
```
git reset HEAD
git checkout -- .
```

## References

- `internal/ops/wt_merge.go` (`mergeWorktree`) — `git.New(projectRoot)` binds merge to main checkout
- `internal/git/worktree.go:283` — `MergeBranch` uses porcelain `git merge`
