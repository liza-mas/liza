# ADR-0175: Task Worktrees Copy Repo-Root Indexes

## Status

ACCEPTED — 2026-10-04. Supersedes ADR-0068's task worktree refresh.

## Context

Worktree creation, claim, reviewer worktree recovery and submit-for-review each
ran a full Stacklit generation, the SCIP language indexers and a Functional
Clusters build in the task worktree, whatever the task. In a planning-heavy MAS
run most tasks were documentation-only. Twelve Stacklit regenerations in 26
minutes, each about 140% CPU and 500 MB, kept host load at 20–30. That load
slowed state-lock holds, which failed claims, which retried and indexed again.

Since ADR-0156, lifecycle hooks and a post-merge trigger keep the repo-root
indexes current. A fresh worktree checks out the integration branch the root
was indexed from. Stacklit and Functional Clusters artifacts hold only
root-relative paths. SCIP document paths are relative too, but the index's
absolute project root is printed by every `scip-search` result, so an unchanged
copy would send agents to the repo-root checkout.

## Decision

1. Task worktrees never run an indexer. At each former refresh point, Liza
   copies `<project_root>/stacklit.json` and
   `<project_root>/functional-clusters.json` into the worktree, and writes each
   `<project_root>/<language>.scip` to the worktree's runtime `scip/` directory
   with `scip-search reroot --project-root <worktree>`, which rewrites only the
   project root.
2. Copies are published atomically, never wait on the repo-root refresh lock,
   replace any previous copy, and keep the existing git isolation
   (skip-worktree or private exclude), so they never dirty task diffs.
3. A missing repo-root artifact, a failed reroot, or a scip-search without
   `reroot` is a per-artifact warning; the worktree runs without that index.
   The existing environment gates still select which artifacts are provisioned.

## Consequences

- Claim-time indexing cost drops to file copies and one metadata rewrite per
  SCIP language; documentation-only work no longer re-parses the repository.
- A worktree index lags the worktree by at most one repo-root refresh and
  excludes the task's own edits, including at review submission. Reviewers
  still see existing callers of changed symbols; new code is in the diff.
- The repo-root index may include uncommitted or untracked root files, so
  agents may see paths absent from their worktree.
- A project has no worktree indexes until its first commit or merge produces
  repo-root indexes.
- Re-rooting requires a scip-search release with `reroot`.

## Alternatives Considered

- **Gate rebuilds on code changes since the indexed commit.** Keeps indexes
  exact for rework, but adds a freshness stamp and diff policy for an advisory
  tool whose agents already verify against source.
- **Index only for code roles.** Planners and architects lose orientation, and
  code claims still pay the full cost.
- **Point worktree prompts at the repo-root indexes.** No copy, but every SCIP
  result names the root checkout, inviting edits outside the worktree.
