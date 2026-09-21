# 144 - Usage Attribution by Terminal Outcome

## Status

ACCEPTED

## Context

The system reported aggregate fresh and cache-read token totals but could not
attribute consumption to terminal task outcomes, nor separate productive work
from tokens spent after a task stopped changing useful state. In one observed
run, 1.39 billion input tokens at a 97.3% cache share coexisted with a
133-attempt unchanged retry sequence and 29 assessments on a single blocked
task. The aggregate could not say how much of that reached merged work.

Two properties made the obvious designs wrong.

**Outcomes move after the fact.** A task's terminal outcome is not known when
its provider turns run, and it can change later through reconciliation: a
rejection, a supersession, a dependency repair. Any design that stamps an
outcome onto a usage record at write time either reports a stale outcome or
needs a reconciliation writer that rewrites durable telemetry — and a reading
taken last week could not then be reproduced.

**Telemetry must not endanger mutations.** Usage capture runs inside the
supervisor, on the path of every provider turn. All state mutations complete
inside one locked read-modify-write
([INVARIANTS.md](../../../INVARIANTS.md#5-concurrency--atomicity)); a telemetry
write that shared that lock, or that could fail a run, would trade a correctness
invariant for an observability feature.

A third constraint is contractual: registration generations and their digests
must not appear in responses, prompts, error text or logs
([Lifecycle Results](../../protocols/lifecycle-results.md#result-contract)),
yet the acceptance criteria ask for a stable agent generation/session dimension
so that restarts and handoffs are attributable without double-counting.

## Decision

**Store immutable usage records outside `state.yaml`, and derive outcomes at
report time from durable task history.**

- Records are appended one JSON line at a time to a per-day file in the branded
  runtime directory's `usage/`, under a leaf file lock, outside every state
  lock. An append failure is logged and never changes a run's result.
- A record carries identity, interval, token counts and a provenance quality
  pinned at write time. It carries no outcome, no lifecycle status and no
  registration generation.
- The report joins records to tasks by task id and classifies each task's
  outcome, last useful transition and failure category from durable history read
  at an explicit `as_of` — defaulting to the window end.
- The session/generation dimension is `supervisor_run_id`: an opaque 128-bit
  identity minted once per supervisor process. It is not derived from
  registration state and confers no authority, but it varies on every supervisor
  restart and across a handoff on every provider path, which is exactly what
  deduplication and multi-session attribution need.
- Dedup identity is a hash over the identity dimensions and the turn start,
  excluding token counts, so a replayed or corrected record collapses onto the
  same identity and a divergent duplicate is marked `conflicting` rather than
  summed.

The full record schema, event classification, outcome classes and report fields
are specified in
[usage-attribution.md](../../protocols/usage-attribution.md).

## Consequences

**Positive.**

- A later correction to a task's history changes the next reading, with no
  reconciliation writer and no telemetry rewrite; `--as-of` reproduces any
  earlier reading from the same durable history.
- Records outside `state.yaml` survive sprint rollover, which narrows the
  sprint-boundary metrics loss for the token dimension.
- Capture cannot fail a run or contend with the state lock, so the atomicity
  invariant is untouched.
- Provenance pinned per record keeps before/after comparisons honest when the
  very work being measured reduces log and history volume.

**Negative.**

- Every report pays a join: records must be matched against current task
  history, so reading is more expensive than reading a stored aggregate would
  be, and it requires state to be readable.
- A record naming a task that no longer exists in state reports as
  `unattributed` with a warning, rather than under its historical outcome.
- Records accumulate: this version prunes nothing. The retention debt and its
  payback trigger are recorded in the protocol spec.
- Within one supervisor process on the default CLI path, per-turn provider-side
  session continuity is not observable, because the provider reports a session
  identifier that is a copy of the task id. Turns are separated by start time
  alone.
- `blocked` is reported as an outcome class although the status is not
  `IsTerminal`, so attribution vocabulary diverges slightly from the task state
  machine's terminal set.

## Alternatives Considered

**Usage fields on `Task`.** Accumulate token counters on the task record in
`state.yaml`. Rejected: it puts telemetry inside the lock that protects state
mutations, so a counter write can fail or delay a committed transition; it grows
a shared model owned by another change stream; it cannot express per-turn
provenance or intervals without embedding a list that grows without bound; and
sprint rollover would discard it.

**Outcome stamped at write time.** Record the task's current status on each
usage record. Rejected: the outcome at turn time is usually not the terminal
one, so totals would be systematically wrong; keeping them correct requires a
reconciliation writer that rewrites durable telemetry, which destroys
reproducibility — a reading taken before the rewrite could never be reproduced.
Deriving at report time makes reproducibility the default rather than a feature.

**Reconstruct usage from existing log files.** Parse historical provider logs
instead of capturing records. Rejected: sparse logs carry no per-turn lifecycle
attribution, so the join that this decision exists to make is not available;
provenance could only be guessed after the fact, defeating the pinning that
keeps before/after comparisons honest. Records start at first write.

**The registration generation as the session dimension.** Rejected as
contractually forbidden: the lifecycle result contract keeps registration
generations and their digests out of every rendered surface, and a durable
record surfaced by a report command is such a surface. `supervisor_run_id`
supplies the same restart/handoff distinctness without touching registration
state.

**A general metrics query layer.** Build the unified query interface the open
architecture issue describes, and express usage attribution inside it. Rejected
as scope: this change delivers one query surface over one source. The broader
issue stays open, and the protocol spec records the relationship.
