# Usage Attribution by Terminal Outcome

## Rationale

Aggregate fresh and cache-read token totals cannot say how much consumption
produced merged work, how much went to blocked or superseded tasks, and how much
was spent after a task stopped changing useful state. A high cache share can
therefore look healthy while retry storms and stale sessions consume large
absolute volumes.

This protocol adds the missing denominators. Provider turns are captured as
durable usage records; outcomes are **not** stored with them. The report derives
each task's outcome from durable task history at read time, so a later
correction to that history changes the next reading, and an explicit `as_of`
reproduces the earlier one.

Records are telemetry. They are written outside `state.yaml` and outside every
state lock; an append failure is logged and never turns a committed mutation
into a failure. This preserves the atomicity invariant
([INVARIANTS.md](../../INVARIANTS.md#5-concurrency--atomicity)) and the
best-effort counter rule of
[Lifecycle Results](lifecycle-results.md#coverage-and-observation-counters).

---

## The usage record

One record per provider turn, written by the supervisor event sink when the turn
completes. One JSON object per line.

| Field (JSON) | Meaning |
|---|---|
| `schema_version` | `1`. A line carrying another version is not interpreted by this version; it is skipped and counted as malformed. Bumped only with a documented migration |
| `record_id` | `sha256(agent_id \| supervisor_run_id \| session_id \| provider \| started_at RFC3339Nano)` — the dedup identity |
| `task_id` | Task under execution; empty for task-free turns, reported as `unattributed` |
| `role` | Runtime role name of the supervised agent |
| `agent_id` | Agent identity |
| `supervisor_run_id` | Opaque 128-bit hex identity minted once per supervisor process and carried on every record that process writes — the session/generation dimension |
| `session_id` | Provider session exactly as the provider reports it, falling back to the caller-supplied value |
| `provider` | Configured provider CLI name |
| `started_at`, `ended_at` | UTC interval of the provider turn |
| `fresh_input_tokens`, `cache_read_tokens`, `cache_write_tokens`, `output_tokens` | Token counts as the provider reported them |
| `provenance` | `terminal_authoritative`, `partial`, `unknown` or `conflicting` |
| `exit_code`, `warm_session` | Turn outcome and warm-session reuse. `warm_session` is reserved: no provider event carries warm-session reuse today, so capture leaves it false |

### Identity and deduplication

`record_id` covers the identity dimensions and the turn start only. Token counts
and provenance are deliberately excluded, so a replayed or corrected record
collapses onto the same identity. Handoffs, reviewer resubmission waits and
restarted supervisors therefore never double-count the same turn: the collapse
happens once, at load time, over the union of every window being reported.

Two records sharing a `record_id` with the same counts collapse to one and are
counted in `duplicates_collapsed`. Two records sharing a `record_id` with
different counts mark that identity `conflicting`; a conflicting record is
counted and excluded from authoritative totals.

### Provenance is pinned at write time

Provenance is decided when the record is written, from the content of the
provider's usage report, never re-derived later:

| Value | Decided when |
|---|---|
| `terminal_authoritative` | The provider reported a usage object with a non-zero fresh-input or output count |
| `partial` | Some fields arrived (cache-read or cache-write counts) while the fresh-input and output counts did not |
| `unknown` | No usage event arrived for the turn, or the provider reports no usage at all — the default CLI transport reports none today, and emits an explicit empty usage event so the quality is recorded rather than inferred from zero. Interactive supervisor launches record `unknown` provenance because the provider reports no usage on that path; only start and completion events arrive. A completed turn with no preceding start (the launch-gate failure path) is also `unknown`, over a zero-length interval |
| `conflicting` | Assigned at load time to an identity whose duplicates disagree on counts |

Pinning the quality with the record is what keeps a later before/after
comparison honest: work that reduces retry volume or log volume changes what is
measured, not how the measurement classifies itself.

Only `terminal_authoritative` records enter token totals. Unknown, partial and
conflicting records are counted explicitly and never folded into zero or into
authoritative totals.

### The registration generation is deliberately absent

No record carries the registration generation, or any digest or derivation of
it. [Lifecycle Results](lifecycle-results.md#result-contract) states that
"neither registration generations nor their digests appear in responses,
prompts, error text or logs", and a durable record rendered by a report command
is such a surface.

`supervisor_run_id` carries the session/generation dimension instead. It is 128
bits of randomness minted once per supervisor process, constant for that
process's lifetime, and therefore different after every supervisor restart and
on both sides of a handoff — on every provider path. It is never read from or
compared against registration state, confers no authority, and appears in no
lifecycle response, prompt or error text.

The provider-reported `session_id` alone would not serve: on the default CLI
path it is a copy of the task id, and the alternative provider path derives its
session name from the binary and agent id, which is constant across restarts.

**What is still not attributable.** Within one supervisor process on the default
CLI path, `session_id` adds no information, so two provider turns of that
process on the same task are separated only by `started_at` — which is why the
start time is part of `record_id`. Per-turn provider-side session continuity on
that path is not observable from these records. Restarted supervisors and
handoffs are attributable, because each is a distinct process with a distinct
`supervisor_run_id`.

---

## Storage

Records live in the `usage/` directory of the branded runtime directory, as
`records-YYYY-MM-DD.jsonl`, keyed by the UTC day of the turn's start.

- **Append**: `MkdirAll(0700)`, one leaf file lock per day, `O_APPEND` write of a
  single complete JSON line, then `Sync`. The lock is a leaf: no other lock is
  held while it is taken, and no lock is acquired within it.
- **Rotation**: a day file rolls to `records-YYYY-MM-DD.N.jsonl` when appending
  the next line would push it past the byte cap. Every numbered part of a day is
  covered by the same day lock, and all parts load together.
- **Line budget**: a record whose serialized line exceeds the per-line budget is
  rejected at append rather than written; a file whose final line is truncated
  beyond that budget loads the records before it and counts the tail as one
  malformed line.

### Unavailable is not zero

A missing records directory is reported as *unavailable*, with a warning — never
as an empty store. Every figure block of the report is omitted in that case
rather than rendered as zeros, and the same rule applies to the lifecycle
counter projection it embeds. A reader can always distinguish "no consumption"
from "not observed".

Malformed lines are skipped, counted, and surfaced as a warning; they never
abort a load.

### Retention

This version prunes nothing: records accumulate for the life of the project, and
sprint rollover does not discard them. That is deliberate — it is what keeps the
token dimension of sprint history readable across sprint boundaries — but it is
debt, not a settled policy.

**Payback trigger:** the usage directory growing past the documented per-file
cap in aggregate, or a general pruning policy landing for task history. Either
event requires a retention decision for this store; the register entry belongs
in `TECH_DEBT.md`.

---

## The useful state transition

A durable task event is **useful** if and only if it changes task content, the
ownership boundary, an accepted artifact or commit, or the terminal outcome.
Polling, release, re-entry and recovery bookkeeping are not useful. The rule is
an explicit two-list classification over durable event names — never a heuristic
over prose volume.

| Classification | Events |
|---|---|
| **Useful** | `created`, `claimed`, `pre_execution_checkpoint`, `task_output_set`, `submitted_for_review`, `review_commit_updated`, `approved`, `rejected`, `review_verdict_approved`, `review_verdict_rejected`, `merged`, `superseded`, `abandoned`, `blocked`, `unblocked`, `integration_failed`, `transition_executed`, `dependencies_rewritten`, `dependency_repair_applied`, `replacement_committed`, `rejection_rca_recorded`, `rejection_rca_resumed`, `handoff_initiated` |
| **Not useful** | `orchestrator_assessment`, `claim_released`, `doer_claim_released`, `review_claim_released`, `reclaimed_after_rejection`, `reassigned_after_rejection`, `new_attempt`, `owned_task_resumed`, `handoff_resumed`, `worktree_recovered`, `claimed_for_integration_fix`, `transition_cycle_blocked`, `transition_crash_recovery`, `planning`, `initialization`, `replanned`, `acceptance_commits_remapped` |
| **Unclassified** | Any event name in neither list |

**Dependency clause.** A direct dependency reaching `MERGED` is a useful
transition *for the dependent task*, timed by that dependency's `merged` event.
A task whose own history shows only polling entries after its dependency merged
still has that merge as its last useful point.

The direct dependency list is read at `as_of` too. For a later audited
dependency repair, the first subsequent `dependencies_rewritten` entry's
`expected_dependencies` records the complete pre-change list. Using that
snapshot keeps both the last useful transition and `post_transition` totals
stable for an unchanged historical window, even after multiple repairs.
At or after the repair, the repaired graph and the repair's own useful event
apply. Primary and baseline windows reconstruct their graphs independently.

A historical reading cannot infer a prior graph from today's dependencies.
If a referenced task's intervening rewrite lacks a valid pre-change snapshot,
the report returns an explicit historical-dependencies-unavailable error naming
the task and rewrite time. Older audit shapes without that evidence require
source-owner resolution; the reader does not invent it or add persistence.
Audit entries explicitly recording output-only changes do not change this graph.

**Unclassified events are not silently absorbed.** They are treated as not
useful *and* reported in the report's warnings, listing the exact names
encountered. Vocabulary drift therefore surfaces as a warning instead of quietly
changing the denominator.

The last useful transition of a task at a reading point is the latest useful
event in its own history at or before that point, or a direct dependency's
`merged` event when that is later. It carries the deciding event name, its time,
and the task's lifecycle revision.

---

## Outcome classes

| Class | Decided by |
|---|---|
| `merged` | A `merged` event |
| `superseded` | A `superseded` event |
| `abandoned` | An `abandoned` event |
| `blocked` | A `blocked` event, not cleared by a later `unblocked` |
| `active` | No deciding event, or the last deciding event is `unblocked` |
| `unattributed` | The record names no task, or names a task absent from state |

**`blocked` is an outcome for attribution although the status is not
`IsTerminal`.** The task state machine treats `BLOCKED` as a live status that
`unblocked` clears; the issue this protocol answers asks for tokens per outcome
including `blocked`, because blocked capability is exactly the consumption class
operators need ranked. The divergence is deliberate and local to attribution: it
changes no status vocabulary, and `unblocked` returns the task to `active` for
the next reading.

### Reproducibility at `as_of`

Task history is append-only, so the outcome of a task at any reading point is
the latest deciding event at or before that point. Every classified task carries
**outcome evidence**: the deciding event name, its time, and the lifecycle
revision. A reading taken with `as_of` set before a later `merged` event
reproduces the earlier class from the same history, with the same evidence.

This is what makes outcome changes caused by later reconciliation reproducible
without a reconciliation writer: nothing is stamped at write time, so nothing
has to be rewritten when history is corrected.

### Failure categories

Usage started at or after a task's last useful transition is classified into one
failure category, first match wins:

| Category | Evidence |
|---|---|
| `rejection_rca` | A `rejection_rca_recorded` entry in the tail — the token dimension of the rejection-RCA gate |
| `circuit_open` | A `reviewer_claim_circuit_open` anomaly naming the task in the tail |
| `duplicate_assessment` | `orchestrator_assessment` entries in the tail |
| `lifecycle_retry` | `new_attempt`, release or reclaim entries in the tail |
| `retry_tail` | None of the above, and the contributing records are authoritative |
| `unknown_provenance` | None of the above, and the contributing records are not authoritative |

---

## The report

Built from one record load over the union of the primary and baseline windows,
the durable task history read at `as_of`, and the sprint lifecycle counter
projection supplied by the caller.

| Field | Content |
|---|---|
| `window` | `since`, `until` and the `as_of` actually applied |
| `outcomes[]` | One row per outcome class × role: `tasks`, `records`, and a `{total, median, p95}` distribution for fresh, cache-read and output tokens. Distribution units are tasks, or individual records for `unattributed` usage; median and p95 use nearest rank, so both are observed values |
| `outcome_totals[]` | Per-outcome totals across roles; `tasks` counts distinct tasks, so a task worked by two roles counts once |
| `per_completed_task` | `merged_tasks`, `cache_read_tokens_per_merged_task`, `cache_hit_percent`. A ratio is absent, not zero, when its denominator is zero |
| `post_transition[]` | Rows by role × failure category with `tokens`, `records`, and bounded per-task detail naming each task's last useful transition; the largest tails are kept and the rest counted in `tasks_omitted` |
| `provenance` | Counts per quality plus `duplicates_collapsed`, `malformed_lines` and `conflicting_records` |
| `suppressed_calls` | Read-only projection of the lifecycle counter rows for suppressed duplicates and preflight rejections |
| `baseline`, `deltas[]` | Present only when a baseline window is supplied |
| `warnings[]` | Store unavailable, malformed lines skipped, records naming absent tasks, unclassified task events, counters unavailable |

Every figure block is omitted when its source is unavailable, and the reason
appears in `warnings[]`. Unavailability is never rendered as zero.

The default window is the current sprint timeline: its start, and its end or now
for a running sprint — the reading point is recorded as `as_of` rather than left
open. `as_of` defaults to the window end.

### The before/after comparison

Supplying a baseline window runs the same computation a second time over the
same record set, filtered to that window, and emits deltas: token totals per
outcome × role, and post-transition tokens per role × failure category, each as
`baseline`, `primary` and `change = primary − baseline`. The baseline report
carries no nested baseline of its own.

Because both windows are served by one load, identity dedup spans them: a turn
that falls in the baseline window only is absent from the primary rows, and no
record is counted twice across the comparison.

This is the surface for measuring retry circuit breakers, assessment
deduplication and tool-output externalization: each of those reduces the volume
the report joins against, so only a comparison with pinned per-record provenance
distinguishes a real reduction from a measurement change.

### The counter-derived suppressed block

`suppressed_calls` is a read-only projection of the per-sprint lifecycle counter
file described in
[Lifecycle Results](lifecycle-results.md#coverage-and-observation-counters). It
selects the rows that show suppressed duplicates and preflight rejections:

| Operation | Outcomes projected |
|---|---|
| `assess-blocked` | `NO_CHANGE` — duplicate blocked assessments the engine declined to repeat |
| `validate-payload` | `INVALID_INPUT` — payloads rejected before any state mutation |
| `submit-verdict` | Every non-`COMPLETED` outcome |

The projection carries the counter file's own `available`, `observed_since` and
warning rather than substituting zeros. A missing, empty or malformed counter
file means unavailable: the block has no rows and the report warns. This keeps
waiting or polling turns and rejected duplicate lifecycle calls visible instead
of silently discarded, without this protocol asserting complete-sprint coverage
that the counters themselves do not claim.

---

## Surfaces

| Surface | Content |
|---|---|
| `usage report` | The full report. Read-only: it reads files and state and mutates nothing, so it takes no role authorization row. Flags: `--since`, `--until`, `--as-of`, `--role`, `--task`, `--baseline-since`, `--baseline-until`, `--format`, and the repository's JSON envelope. Time bounds accept RFC 3339 or `YYYY-MM-DD` and are parsed before any read |
| `get metrics` | A compact usage summary for the current sprint window: per-outcome totals, cache-read tokens per merged task, cache-hit percent, provenance counts and warnings. No distributions, post-transition rows or deltas. An unavailable store omits the block and records the warning; no pre-existing metrics field changes |

Neither surface reads or writes lifecycle state, and no new task or state field
carries usage.

---

## Relationship to open architecture issues

Recorded for traceability; neither issue is closed by this protocol.

- **Sprint Metrics Lossy at Sprint Boundary** — keeping records outside
  `state.yaml` means sprint rollover does not discard them, so the token
  dimension survives the boundary that reduces `SprintMetrics` to a single
  field. The issue's structural loss of the other sprint metrics is untouched.
- **Metrics Collection Without Query Interface** — `usage report` is one query
  surface over one source, a partial step toward the missing unified layer. Lock
  timing, sprint archives and historical metrics remain unaddressed.

See [architectural-issues.md](../architecture/architectural-issues.md) for both
sections, and
[ADR-0144](../architecture/ADR/0144-usage-attribution-by-outcome.md) for the
report-time attribution decision and its rejected alternatives.
