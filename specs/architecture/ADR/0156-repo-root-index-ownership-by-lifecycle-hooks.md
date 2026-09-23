# 156 - Repo-Root Index Ownership by Lifecycle Hooks and the Merge Trigger

## Status

ACCEPTED — implemented 2026-09-23. Partially supersedes ADR-0068: the
project-root refresh for orchestrator context. Task and reviewer worktree
refreshes stand.

## Context and Problem Statement

ADR-0068 refreshed project-root SCIP and Stacklit indexes in the orchestrator's
pre-execution step on every wake. The orchestrator also refreshed the root
Functional Clusters artifact there, although ADR-0095 names only Pairing hooks
and MAS worktrees. That placed repository maintenance inside orchestration:

- Every wake paid for indexing, up to a 90-second timeout per command, before the
  orchestrator could act.
- The root indexes moved only when an orchestrator woke, not when the tree moved.
- Pairing already kept repo-root indexes fresh through lifecycle Git hooks, which
  write SCIP to `<root>/<language>.scip`. The orchestrator wrote
  `<root>/.liza/scip/<language>.scip`. The same repository therefore had two
  writers and two layouts.

The hooks had defects of their own once they became the only owner. Concurrent
hook runs were not serialized, so an older run could publish over a newer one,
and generators wrote in place, so a reader could see a partial file. And a MAS
merge fires no hook at all: `wt-merge` advances the integration branch with
`update-ref` and syncs the working tree with a file-level checkout, which the
hooks deliberately ignore.

## Considered Options

1. **Keep the orchestrator refresh** and add serialization around it.
2. **Refresh synchronously inside the merge**, in addition to the hooks.
3. **Make the lifecycle hooks the single owner**, add a detached post-merge
   trigger, and serialize every writer through one coordinator.

For hook installation in MAS projects:

- **(a)** MAS init installs the hooks, with no repair.
- **(b)** A separate doctor-style command repairs them on request.
- **(c)** MAS init installs them and the orchestrator supervisor repairs them at
  startup.

## Decision Outcome

Chose **Option 3** with **(c)**.

- **Ownership.** Lifecycle Git hooks own repo-root Stacklit, SCIP and Functional
  Clusters artifacts in both modes. No agent refreshes them. Pairing init and MAS
  init install the hooks. On start, the orchestrator supervisor reinstalls them
  when they are missing or no longer match the current gates, `scip_search`
  config and repository layout. A repair failure is written to the alerts log and
  does not stop the supervisor.
- **Merge trigger.** A successful `wt-merge` launches the coordinator detached,
  with trigger `merge`, and does not wait for it. A launch failure is a merge
  warning, never a merge failure. Conflicts, rollbacks and lifecycle replays
  launch nothing.
- **One coordinator.** The hooks' dispatcher and the merge trigger both run the
  hidden `index-refresh` command. A trigger writes a request marker in the Git
  common directory, then tries a non-blocking lock there. A busy lock returns at
  once; the owner re-checks the marker after releasing and runs again for any
  request that landed meanwhile. The index script inherits the lock descriptor
  and closes it for every tool it starts.
- **Atomic publication.** The script generates every artifact in a per-run
  staging directory under the Git common directory and publishes each with a
  rename after its generator succeeds.
- **Discovery.** The orchestrator prompt lists `<root>/<language>.scip`, the
  layout the hooks write. Task and reviewer worktrees keep their
  runtime-refreshed indexes under `.liza/scip/`.

### Accepted limits

- **Windows.** The script does not inherit the lock, and no Job object ties it to
  the coordinator. If the coordinator dies mid-run, its script can overlap the
  next refresh.
- **Git directory on another filesystem.** The rename becomes a copy, so
  publication is no longer atomic. The coordinator logs a warning.
- **Bounded crash recovery.** If the coordinator dies, its script keeps the lock
  until it ends. A request made meanwhile is not run then; it waits for the next
  trigger.
- **Manual runs.** Running the index script directly bypasses the coordinator. A
  coordinated run clears the staging root and can remove a manual run's work in
  progress.
- **Dropped languages.** A language the hooks stop indexing, but that is still in
  `scip_search`, keeps its last root index file, and prompts keep listing it.

## Rationale

Option 1 keeps indexing on the wake path, so the latency and the wake-coupled
freshness remain, and it keeps two writers with two layouts.

Option 2 puts slow, failure-prone indexing on the path that must not fail. The
merge's contract is the integration ref, not the indexes, so the trigger is
detached and its failure is a warning.

Option 3 makes the tree's movement, not an agent's schedule, drive freshness,
and one coordinator gives every writer the same ordering and publication rules.

For installation, (a) leaves every project initialized before this change
without hooks until someone re-runs init, and (b) depends on an operator
noticing silently stale indexes. (c) was chosen by the maintainer: the
orchestrator already starts every MAS run, so repair costs one comparison per
start and needs no operator action.

## Consequences

- Orchestrator wakes no longer index. The root indexes follow commits,
  checkouts, rewrites and merges.
- Repo-root indexes have one writer, one layout and one publication rule in both
  modes.
- [Worktree Intelligence Refresh Has Multiple Owners](../architectural-issues.md#worktree-intelligence-refresh-has-multiple-owners)
  is unaffected: it covers worktree lifecycle paths, and the coordinator covers
  the repo root only.
  [Index Discovery Failures Are Unobservable](../architectural-issues.md#index-discovery-failures-are-unobservable)
  remains open: discovery still discards errors, including in the new
  root-layout lookup.
- Projects upgraded from ADR-0068 keep orphaned `<root>/.liza/scip/*.scip` files
  that nothing reads or refreshes.
- Every coordinated refresh writes its output to
  `<git-common-dir>/<binary>-index-refresh.log`, overwritten per run. Hook runs
  also echo it to the terminal that ran the Git command; the detached merge run
  writes only the log.

## Relationship to Prior Decisions

Partially supersedes ADR-0068 (Optional Repository Indexing with SCIP and
Stacklit) for project-root refresh. Extends ADR-0081 (Indexing Activation for
Setup and Init) by making MAS init install the hooks, and ADR-0095
(Functional-Cluster Index Lifecycle), whose repo-root artifact is now refreshed
only by the hooks. The lock uses the flock semantics of ADR-0061 (Flock-Only
Lock Authority).

## Evidence and Reconstruction

Commits `5c68b9d95` (orchestrator refresh removed), `93849bf7d` (staged atomic
publication), `66671569f` (MAS install and orchestrator repair), `be163762a`
and `f5c2a7581` (non-blocking lock and coordinator), `3b2c30ca4` (merge
trigger), `e113278ec` (root-layout discovery) and `fba9f61a2` (project-root
runtime refresh mode removed). The installation option was the maintainer's
explicit choice; the accepted limits were agreed in plan review before
implementation.

---
*Recorded from the implementing commits (2026-09-23).*
