# Blocked-Assessment Idempotency

`assess-blocked` records an `orchestrator_assessment` only when its effective
content differs from the latest assessment of that `BLOCKED` task. Comparison
and append occur inside the same exclusive state transaction, so concurrent
equivalent calls and process restarts cannot restart an unchanged append loop.
Authorization, generation fencing and lifecycle request checks still apply.

## Material identity

`BuildAssessmentFingerprint` in
[assess_blocked_fingerprint.go](../../internal/ops/assess_blocked_fingerprint.go)
hashes a canonical JSON object with exactly six material inputs; a declared
[awaited set](#awaited-set) takes the place of `descendants` and filters
`dependencies`:

| Input | Canonical value |
|---|---|
| `self` | Task status, count of history entries other than `orchestrator_assessment`, and the sorted set of direct `depends_on` IDs. Assessment-only activity and `Lifecycle.Revision` are excluded. |
| `dependencies` | Unique task IDs on every direct dependency's resolved replacement path, sorted by ID. Each record carries ID, creation time, outcome class, and sorted sets of effective parent, dependency and supersession edges. Missing tasks retain their ID and the `missing` class. |
| `descendants` | Transitive descendants beneath resolver-selected, non-superseded dependency roots, following effective parent links, plus their resolved replacements. The same sorted, deduplicated record projection is used as for dependencies. Roots themselves are excluded from the parent traversal. |
| `blocker` | Reason, ordered questions and structured repair request that the assessment would establish. History-only mode uses current task metadata; reconciliation uses the validated candidate metadata. |
| `disposition` | The candidate assessment note, including any stated recovery action, normalized as text. There is no separate disposition enum. |
| `human` | Count of durable human notes addressed to this task or `all`. A newly appended note intentionally permits reassessment even if its text repeats an earlier instruction. Notes addressed only to dependencies do not count. |

Fingerprint text normalization applies Unicode NFC, trims leading/trailing
whitespace and collapses each whitespace run to one space. It applies to reason,
questions, disposition, repair operation/target/command/evidence/validation and
dependency-update IDs/lists. JSON map keys are ordered deterministically;
typed records have fixed field order. Question, evidence, validation and repair
update/list order is preserved: these sequences are not sorted or deduplicated.
Nil and empty question lists both become `[]`; a nil repair request becomes the
zero repair record, with empty optional fields omitted by its JSON tags.

Reconciliation first uses the existing repair validator/normalizer: trim scalar
fields and dependency IDs, compact blank evidence/validation entries, and retain
explicit dependency lists. Fingerprint normalization determines comparison
identity; it does not rewrite all stored prose. Equivalent whitespace, Unicode
composition or map order cannot create a new identity. Arbitrary paraphrases or
duplicated sentences within a note are not semantically interpreted or removed.

The digest depends only on durable state and the candidate, not wall time,
process memory, actor identity or receipt revision. The consumer's own
non-assessment history count remains a structural lifecycle version; creation
times distinguish dependency task incarnations. Explicit human supersession is
represented by appended notes, not by editing old notes in place.

Dependency, descendant and awaited outcome classes are shared: `MERGED` is satisfied;
`BLOCKED`, `ABANDONED`, `INTEGRATION_FAILED`, and unreplaced `SUPERSEDED` are
failed/blocked; missing IDs are missing. Every other nonempty status is pending:
intermediate states belong to the configured pipeline, including architecture,
custom role pairs, rejection, rework and approval before merge. Their
claims, submissions, review claims and other history-only progress do not change
the fingerprint. Both pending-to-failed and failed-to-pending transitions are
material. A superseded node with replacements retains its replacement edges,
and every reachable replacement has its own outcome record, including missing
or cyclic paths. An empty status remains unknown and retains non-assessment
history sensitivity. The fingerprint stays pure over state without loading a
pipeline and does not validate whether nonempty statuses belong to that pipeline.
This projection does not alter dependency satisfaction or grant permission to
execute dependent work.

## Awaited set

`assess-blocked --awaits <id>[,<id>]` declares the existing unfinished tasks a
hold waits for, all of them, typically later-stage work the dependency-direction
rule forbids as an edge. IDs are trimmed and kept as a sorted set; repeats
merge.

Each member is classified over its resolved replacement path: **failed** if any
task on it is failed/blocked, missing or of unknown status, or the path cycles;
otherwise **satisfied** when dependency resolution satisfies it (a split needs
every replacement merged); otherwise **pending**. A non-empty set replaces
`descendants` with `awaited`: the sorted IDs and the set's state, `pending`
until every member is satisfied, then `satisfied`, or `failed` as soon as one
member fails, which also adds the outcome records of every member path so each
distinct failure is its own digest. The set also removes from `dependencies`
the records of tasks on its members' paths, so an awaited dependency's partial
progress is not material; uncovered records, including a sibling replacement of
a split dependency, remain. The hold therefore wakes once when the set completes
or once a member fails, plus human-note, self and uncovered-dependency changes.
Without a set the material is byte-identical to earlier versions, so recorded
digests stay valid.

A set belongs to one `BLOCKED` episode: it applies while no status-transition
event (per the status-age classification; unclassified events count) follows
its assessment in history order, so an unblock and re-block at the same instant
still end it. The wake reader, carry-forward, deadlock search and planning-output
detection all apply this rule. An assessment without `--awaits` carries the
episode's set forward minus satisfied members, reported as `awaited_tasks` with
`awaited_carried: true`; `--clear-awaits` drops it and conflicts with
`--awaits`.

The writer validates the explicit or carried set after replay and status checks
and before the no-change comparison, so an otherwise unchanged assessment is
rejected once its wait is stale. It rejects a self-wait, unknown IDs, IDs with
no pending task on their replacement path (listing each with its status), IDs
whose path has failed work (the all-of wait cannot complete until it is
reassessed), and a wait that leads back to the task. That search follows
effective dependencies through supersession, stops at `MERGED`, `ABANDONED` and
`SUPERSEDED` tasks, and follows the current-episode awaited sets only of tasks
currently `BLOCKED`; the rejection names the cycle. A carried set's rejection
adds the remedy: `--awaits` with the still-pending members, or `--clear-awaits`.

The guard is best-effort: later graph edits can close a cycle. The reader
ignores a set that is malformed (not a non-empty normalized string list) or now
leads back to the task; the digest then differs and the task wakes, without a
stated reason. Repeating the same `--awaits` names the cycle or the settled or
failed task ([ADR-0157](../architecture/ADR/0157-blocked-assessment-wait-for-set.md),
[ADR-0158](../architecture/ADR/0158-awaited-set-all-of-and-carry-forward.md)).

## Persistence and no-change result

The key `assessment_fingerprint_v2` is stored in `TaskHistoryEntry.Extra`,
inlined into the history entry in YAML; its value is a 64-character lowercase
hexadecimal SHA-256 digest of the canonical JSON. Only
the latest `orchestrator_assessment` retains it. On a new append, the writer
removes this key and the retired `assessment_fingerprint_v1` and
`dependency_descendant_wake_snapshot_v1` keys from earlier assessment entries,
preserving their notes and other audit fields. It neither stores the full
fingerprint input nor introduces a separate task-state field. A declared
awaited set is stored beside the digest as `awaited_tasks` and pruned the same
way; each assessment stores its effective set, explicit or carried, and
`--clear-awaits` stores none. Only the explicit `--awaits` IDs and the clear
flag are part of lifecycle request identity, each omitted when empty so earlier
identities are unchanged; a carried set is derived from state and never is.

After eligibility and request checks, the writer compares the candidate digest
with the latest assessment's valid digest before changing canonical blocker
metadata, pruning old cursor fields, appending history or completing a lifecycle
request. Equality returns successful JSON (`ok=true`) with:

| Field | Value |
|---|---|
| `outcome` | `NO_CHANGE` |
| `safe_action` | `stop` |
| `effects` | `none` |
| `changed` | `false` |

No history, receipt or alert is appended; task state, lifecycle boundary and
the serialized blackboard remain unchanged. The no-change sentinel aborts
serialization. Separate best-effort outcome telemetry may still advance after
operation locks are released, under the
[lifecycle counter contract](lifecycle-results.md#coverage-and-observation-counters).
Content equality is distinct from an exact retained request replay, which is
checked first and can return `ALREADY_COMPLETED` under the
[lifecycle result contract](lifecycle-results.md#result-contract).

With no latest assessment, or a missing/malformed fingerprint (including an
incorrect type, length or hexadecimal case), comparison fails open once: the
next otherwise valid assessment appends and establishes the canonical baseline.
An equivalent later call then returns `NO_CHANGE`. No migration rewrites legacy
notes or invents their missing baseline. Upgrade from v1 intentionally makes
each currently blocked, already-assessed task actionable once, costing one
`BLOCKED_TASKS` turn per affected run to establish v2 baselines. A changed material
input permits one new assessment; history-only calls preserve blocker metadata, while reconciliation
atomically replaces reason/questions/repair request and validates the candidate
state. A nil repair request in reconciliation clears the old request.

## Read/write relationship

[orchestrator_wake.go](../../internal/ops/orchestrator_wake.go) and the blocked
work detector use the same fingerprint builder and latest-entry validity check.
The reader supplies current canonical blocker metadata, the note from the
assessment that carries the digest, and that assessment's valid awaited set
while it belongs to the current episode. Missing/invalid
baselines are actionable; otherwise a differing digest is actionable.

The writer's material-change predicate is a **superset** of the reader's wake
predicate: every durable change that wakes a blocked task is visible to the
writer, while the writer additionally accepts a newly supplied blocker payload
or disposition that no read could predict. After an assessment is committed,
its own history entry and receipt revision do not provoke another wake. The
superset concerns fingerprint comparison, not eligibility: awaited-set
validation rejects a candidate, explicit or carried, that keeps a set whose
work has settled or failed or that now leads back to the task, so the next
assessment must declare a changed set or clear it. There is one comparison baseline, not independently advancing read and
write cursors.
This contract applies to `BLOCKED`; hypothesis-exhaustion wake behavior remains
separate.

The registered `assess-blocked` v1 payload schema validates structural input,
including `awaited_tasks` as a list of non-blank strings, before state
acquisition. Preflight and mutation share that validator; role,
generation, task status and state-dependent eligibility still belong to mutation.
See [payload validation](payload-validation.md) for the preflight boundary.

## Observability and limits

The approved [issue #157 digest](../plans/20260918-fix-gh-issues/20260918T153055Z-cpm-1-issue-digest.md#issue-157-deduplicate-blocked-task-assessments-by-lifecycle-version-and-blocker-fingerprint)
reports 29 assessments on one blocked task, with run state reaching 1.12 MB and
820 history entries. These are historical motivation, not measurements of this
implementation or a claim that all those entries were duplicates.

| Figure | Evidence and meaning |
|---|---|
| Assessments appended | `assess-blocked` / `COMPLETED` outcome count in the sprint observation window; durable assessment history records actual appends. |
| Duplicates suppressed | `assess-blocked` / `NO_CHANGE` outcome count. Receipt replays are counted separately as `ALREADY_COMPLETED`. |
| History entries avoided | One per observed `NO_CHANGE`, so the same count supplies this figure without another state field. |
| Bytes avoided per call | `suppressed_entry_bytes`: byte length of YAML serialization of the would-be history entry, including the candidate note and metadata. It is an entry-size estimate, not the full state-file delta. |

The byte figure is returned without logging the repeated full note. **There is
no durable byte aggregate.** The fixed operation/outcome counter matrix stores
counts only; summing retained per-call results is external analysis, not a
built-in sprint total. Availability and `observed_since` delimit coverage;
counter failures/process death can lose observations and never authorize an
operation retry. Early invalid input or authorization rejection, before sprint
capture, is not counted by this command. The
[byte-aggregate debt](../../TECH_DEBT.md#blocked-assessment-byte-aggregate)
records the extension trigger.

The implementation sources above establish the documented identity and
transaction behavior; [lifecycle_metrics.go](../../internal/ops/lifecycle_metrics.go)
establishes the count-only telemetry shape. Design rationale and rejected
alternatives are in [ADR-0141](../architecture/ADR/0141-blocked-assessment-idempotency.md)
and, for the awaited set, [ADR-0157](../architecture/ADR/0157-blocked-assessment-wait-for-set.md).
