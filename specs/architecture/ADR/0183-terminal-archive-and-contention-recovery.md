# ADR-0183: Terminal Record Archival and Fenced Contention Recovery

## Status

ACCEPTED — 2026-10-06. This records the reviewed design; implementation and
performance validation remain separate acceptance gates.

## Context

State transactions still decode and emit retained terminal evidence while
holding the exclusive state lock. Receipt-only archival cannot remove terminal
history, lifecycle receipts and outputs, which dominate the measured run
described in [terminal-history debt](../../../TECH_DEBT.md#terminal-task-history-stays-in-live-state).
Terminal status does not make these fields dead: history defines transition
identity and assessment counts, lifecycle receipts prove exact replay, outputs
feed downstream generation, and terminal repair operations can change evidence.

[ADR-0174](0174-contention-safe-agent-recovery.md) supplies acquisition-only
supervisor retry. [ADR-0177](0177-liveness-side-records.md) removes heartbeat
writes from the state transaction. Ordinary fenced CLI mutations still return
bounded acquisition timeouts to provider sessions. An infrastructure-only block
that was assessed as waiting for contention repair has no typed recovery signal
when unrelated mutations subsequently succeed. D-41(c) and D-15's remainder
require these gaps addressed while preserving existing fences and gates.

## Decision

### Complete logical tasks, immutable physical terminal records

1. Keep every task ID in physical `state.yaml`. For archived MERGED, ABANDONED
   and SUPERSEDED tasks, retain `id`, `status`, `created` and `terminal_archive`
   `{sha256, archived_at}`. A lowercase SHA-256 derives the object path under
   `archive/objects/<sha[0:2]>/<sha>.json`; state never supplies an I/O path.
2. The strict JSON envelope is `{format_version: 2, task_id, field:
   "terminal_task", value_yaml: <complete task YAML string>}`. Payload YAML
   excludes its own terminal reference and restoration bookkeeping; it retains
   inline fields, scalar types, full history, output and lifecycle metadata.
   Use the existing safe scalar emission rules. Terminal eligibility and the
   payload's ID, status and created timestamp must match the physical row.
3. Database reads, including cached reads, and mutation callbacks observe the
   complete logical task. Task-sensitive pre-image validation restores the
   locked pre-image too. Physical storage and restoration bookkeeping do not
   affect `TaskTransitionID`, lifecycle replay or domain validation. Archive
   history physically without truncating or hiding any logical history entry.
   This supersedes the storage restriction that history must remain inline;
   audit completeness and logical identity remain mandatory.
4. Verify object bytes/digest and envelope identity on every read, including
   after a cache hit. Missing, corrupt, wrong-version or mismatched evidence
   fails explicitly with task, digest and path. A byte-bounded per-blackboard
   decoded-object cache avoids repeated payload parsing; cloned returned tasks
   cannot alias its mutable slices, maps or pointers. Eviction changes cost,
   never correctness.
5. Project a separate state view for publication; never replace the caller's
   complete task with its stub. A changed terminal task gets a new immutable
   object/reference. Install and fsync changed/new objects and their directory
   chain before publishing state. Unchanged already-published references need
   no new durability barrier; retry of an installation that failed before
   publication must repeat the barrier even if identical bytes already exist.
   Old objects remain available to old snapshots. Windows directory durability
   retains the existing best-effort limitation. No object sweep or deletion.
6. Share the existing raw object path/install/durability primitives through
   `internal/archiveobject`, avoiding an ops-to-db dependency cycle and duplicate
   durability logic. Acceptance-receipt APIs and format-version-1 objects remain
   compatible. Their inspection-only hydration stays separate: an archived
   receipt is still absent from the ordinary logical task until inspection.
7. `config.terminal_task_archival` defaults off. Operator maintenance
   `archive-terminal-tasks` explicitly enables it and drains bounded batches:
   at most eight tasks, soft 4 MiB, with the first object always progressing.
   Eligibility is rechecked under the lock. Enabled post-merge maintenance
   archives newly terminal records/backlog; later terminal mutations stay cold.
   An enabled zero-work pass uses a snapshot without taking the state lock.
   The first enable is a real config write even without eligible tasks.
8. Migrate through an archive-aware unnormalized decode; preserve detection of
   legacy fields without overwriting stubs as incomplete logical tasks. Offline
   state analysis restores objects relative to the runtime archive directory or
   an explicit archive directory, and rejects unreadable evidence. Portable
   snapshots carry immutable objects alongside state. Task-evidence guidance
   uses hydrated `get tasks`; physical YAML remains useful for top-level data
   and storage references.

### Retry only state-lock acquisition with original request identity

An explicit CLI `--request-id` plus `--expected-transition` opts that invocation
into cancellable state-lock acquisition retry. Carry cancellation context in
request options outside persisted identity/payload. Preserve the original
request pair, actor/generation authority and normalized payload across all
acquisitions, including submission preparation and finalization. CLI interrupt
and termination cancel waits.

No opt-in applies to missing/incomplete pairs, invalid input, diagnostic reads,
ordinary operator commands or unrelated locks. A started callback runs at most
once. Callback failure and nested lock timeout return to the caller; never
repeat the whole operation, Git work, indexing or prerequisite commands. Domain
checks still reject changed boundaries, stale authority and paused admission
after acquisition. Verdict outbox identity and replay semantics remain intact;
its persisted envelope never includes context.

### Explicit infrastructure holds, bound to one blocked episode

`mark-blocked` and `assess-blocked` accept an optional `state_lock_timeout` tag
through the same structural validator used by CLI, preflight and operations.
Only infrastructure-only waits qualify. Refuse a human ask, repair request,
open or closed rejection-RCA recovery obligation, unresolved lifecycle
preparation, or added unmet prerequisites. Never infer a tag from prose.
Assessment may explicitly adopt a legacy hold; a changed blocker cannot inherit
its tag silently.

A persisted `state_lock_hold` names the current BLOCKED episode timestamp,
canonical blocker identity and the publication sequence installing the tag.
Monotonic state `mutation_sequence` advances only with successful `Modify`
publication; legacy state starts at zero. Unchanged callbacks do not publish;
pending liveness folds remain real mutations. The no-op check clones stored
logical state before liveness overlay, avoiding a second decode or marshal.
Its clone/compare cost is included in transaction benchmarks.
Recovery requires a strictly later
published sequence: the hold's own write cannot authorize recovery. Sequence
changes alone do not alter ordinary fingerprints or task transition IDs.

Orchestrator PreWork/wait handling uses a snapshot no-work check followed by a
generation-fenced committing path. Revalidate the tag/boundary, episode,
blocker, mode, human/RCA/preparation gates, direct/provider dependencies and
preserved worktree health. Recover through the existing unassigned unblock path
with an exact request identity: restore the role-pair initial state with
`Continuation=true`, audit normal unblock and clear the tag. Do not assign a
provider, consume an iteration, execute repair commands, alter Git or override
pause/checkpoint admission.

The signal wakes an idle orchestrator even if its prior assessment fingerprint
is unchanged. Refused stale, changed or incompatible holds stay BLOCKED and
report the specific gate; repeated unchanged refusals must not cause repeated
no-op writes or endless provider wake turns. Ordinary blocks retain their wake
and manual recovery semantics.

## Consequences

- Physical state becomes smaller without removing graph identities or changing
  the logical task/transaction boundary. Active per-task/per-agent partitioning
  is excluded.
- Enablement requires upgrading and restarting every supervisor, watch and TUI
  together. Older binaries cannot interpret terminal stubs. Rollback requires
  complete logical restoration before returning to an older binary. With every
  run process stopped, `archive-terminal-tasks --restore-inline` atomically
  disables archival and publishes complete logical records. This restoring
  transaction pays the original full-state cost; objects are retained.
- Immutable object history consumes disk; interrupted transactions may leave
  unreachable objects. Preserving old snapshots takes priority over cleanup.
- Hydration, cloning, full validation and digest verification still cost time.
  Measure complete cold and warmed read-modify-publish transactions on identical
  evidence-rich fixtures. Smaller YAML or warm-cache timings alone do not prove
  contention reduction. No live-run speedup or deployment is claimed here.
- Acceptance requires lossless reads/mutations/old snapshots, cache-independent
  corruption detection, durability failure retries, raw-reader compatibility,
  acquisition cancellation and exactly-once callbacks, and repeated-observation
  negative tests for every recovery gate.

## Alternatives Considered

- Retain whole YAML and increase timeouts: avoids a storage-format change but
  preserves growth-dependent transaction cost and provider retry burden.
- Archive selected history prose only: preserves more physical structure but
  the documented saving ceiling is 18%, leaving major terminal payloads live.
- Partition active tasks/agents: potentially lowers cost further, but changes
  coordination boundaries and is explicitly outside this decision.
- Replay entire CLI operations: can duplicate external effects and refresh
  request identity; acquisition-only retry retains the existing fence.
- Parse blocker prose or auto-unblock after any successful write: cannot prove
  episode ownership or preserve human, dependency and admission gates.
