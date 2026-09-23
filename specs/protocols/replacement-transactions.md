# Replacement Transactions

`replace-task` creates one replacement, applies declared consumer dependency
updates and supersedes the source in one state transaction. It requires a
current-generation orchestrator with the `replace-task` capability. Existing
creation, supersession and dependency-repair commands remain available.

## Canonical payload

The `--replacement-file` argument supplies one JSON object to the version 1
`replace-task` schema and the mutation boundary:

```json
{
  "source_task_id": "code-4",
  "reason": "reviewed correction with preserved implementation",
  "replacement": {
    "id": "code-4-r1",
    "role_pair": "coding-pair",
    "desc": "Complete the reviewed correction",
    "spec": "specs/correction.md",
    "done": "The correction passes its declared validation",
    "scope": "src/correction",
    "priority": 1,
    "depends": ["code-3"]
  },
  "consumers": [{
    "task_id": "code-5",
    "expected_depends_on": ["code-4"],
    "desired_depends_on": ["code-4-r1"]
  }],
  "preserved_base": {
    "base_commit": "<preserved-commit-sha>",
    "worktree": ".worktrees/code-4-r1"
  }
}
```

Replace the illustrative spec, scope and commit with real project values.
`replacement` uses the `AddTaskInput` JSON shape and shared version 1 `add-task`
schema: `id`, `role_pair`, `desc`, `spec`, `done`, `scope` and positive `priority`
are required. Optional fields are `type`, `plan_ref`, `validation`,
`validation_prerequisites`, `destructive_db`, `rca_required` and `depends`.
Pipeline-dependent constraints still apply at mutation.

A replacement whose `role_pair` equals the source's continues the same
allocation and inherits the source's `parent_task`/`parent_tasks`. Fan-in
cohorts and parent-scoped checks therefore still see the work; acceptance
adoption still requires an exact match with the parent's reviewed allocation.
`epic_ref` is not inherited, because the payload cannot restate it and
replacement is how a broken one is dropped. A many-to-one cohort represents
each SUPERSEDED member by its successors. Every supersession chain must stay
within the cohort lineage and end at members that are not superseded. When a
successor lacks the lineage or a chain cycles back on itself, the fan-in is
refused and names the member, rather than firing without that work.

`source_task_id` and `replacement.id` must differ. Each consumer has a unique
existing `task_id`, distinct from both source and replacement, and explicit
`expected_depends_on` and `desired_depends_on` arrays of unique nonempty IDs.
Empty arrays are meaningful; the entire `consumers` list may be empty. Expected
lists guard against stale dependency edits. `preserved_base` is optional, but
when present requires both fields. Caller authority and request flags are
invocation metadata, not fields of this payload.

Preflight complex input with `validate-payload replace-task --payload <path>`.
The same schema validates the original JSON before typed CLI decoding and
validates the ops input before state acquisition. Preflight does not establish
authority, source eligibility, dependency validity against live state, or Git
existence. See [Payload Validation](payload-validation.md).

## Request identity and replay

Both `--request-id <id>` and `--expected-transition <token>` are mandatory.
Read the source with `get <source-task-id> --json` to obtain its transition
token before forming a new request. Missing identity fields yield
`INVALID_INPUT`/`correct_input`; a replacement ID alone is not replay identity.

Preserve the original request ID, expected transition and payload across
retries. A fresh token creates a new logical request and requires reassessing
authority, source boundary and effects. Never refresh the token merely to make
an expired request execute.

The source's existing lifecycle receipt keys on operation, actor, registration
generation digest, request ID and expected transition; the payload digest is
compared separately. Current authority is checked before replay. Retained exact
receipts are matched before rejecting the old transition token, allowing replay
after source supersession. Replay verifies that durable `superseded_by` still
names the requested replacement and returns its original completion evidence
without duplicating history, receipts, cleanup or activity-log effects.
Later successor worktree cleanup does not invalidate that retained completion:
declared-base Git admission errors apply only when no exact receipt matches.
Generation values and digests never appear in caller-visible results or logs.

## Outcomes and safe actions

Results use the [Lifecycle Results](lifecycle-results.md#result-contract)
envelope, including on failure. Successful domain results add `source_task_id`,
`replacement_task_id`, `source_original_status`, `retargeted_consumers` and
optional `warnings` to the flat lifecycle fields.

`retargeted_consumers` is the sorted, deduplicated set of task IDs rewritten by
the whole transaction: explicit consumer updates and implicit supersession
canonicalization, including operational output dependencies. Empty or partial
consumer lists retain the existing canonicalization behavior. Tasks rewritten
by both steps appear once; unchanged tasks and earlier rewrites are excluded.

| Situation | Outcome | `changed` | `safe_action` | `effects` |
|---|---|---|---|---|
| Transaction committed | `COMPLETED` | `true` | `continue` | `committed` |
| Exact replay of retained receipt | `ALREADY_COMPLETED` | `false` | Server-selected; normally `stop` after supersession | `none` |
| Same identity, materially different payload | `CONFLICT` | Absent | `stop` | `none` |
| Replacement ID already occupied by another task | `CONFLICT` | Absent | `stop` | `none` |
| Invalid payload | `INVALID_INPUT` | Absent | `correct_input` | `none` |
| Source no longer eligible, after boundary checks | `ALREADY_TRANSITIONED` | Absent | `stop` | `none` |
| Stale consumer expected dependencies | `STATE_CHANGED` | Absent | `requery` | `none` |

The server returns one safe action, never a choice for the caller. Replay uses
`ALREADY_COMPLETED`, not `NO_CHANGE`. `transition_id` describes the current
source boundary; `completed_transition_id` identifies the retained completion.
Receipt replay does not grant ownership of the replacement.

The shared contract also covers stale callers (`STALE_CALLER`/`stop`), denied
authority (`FORBIDDEN`/`stop`), stale source boundaries
(`STATE_CHANGED`/`requery`), and proven pre-effect contention
(`RETRYABLE`/`retry`). A persistence failure after candidate completion is
uncertain: `STATE_CHANGED`/`requery`, `effects=unknown`, never permission for
blind replay. `INVALID_INPUT` and `CONFLICT` carry bounded field diagnostics;
consume their schema version, field, constraint, value class and recovery
action without parsing error prose. `changed` is absent on failures.

## Transaction and audit boundary

Structural checks reject malformed input before state acquisition. Declared-base
Git validation runs outside locks, retaining its result or error. Lock order is
project lifecycle shared lock, source ownership/worktree lock, then blackboard
lock. One generation-fenced mutation checks receipt identity and replay lineage
first. An exact replay returns the retained completion; a fresh request must
pass the retained Git validation result before candidate mutation. It then checks
source eligibility, ID collision and dependency expectations; builds the
replacement; applies consumer updates;
supersedes the source; validates the full candidate; and records completion.
The source must be in its role-pair initial or rejected status, `BLOCKED`, or
`INTEGRATION_FAILED`. Source supersession also canonicalizes affected consumers
and prunes illegal downstream source edges under the existing dependency rules.

Any candidate error, including failure after consumer updates, discards the
entire candidate. Replacement, source, consumers, history and receipt persist
together. Git cleanup, activity logging and telemetry are outside this atomic
state boundary; post-commit failures are warnings and do not undo success.
The source branch remains available for successor salvage.

One `replacement_committed` event on the source records source/replacement IDs,
prior/new source transition IDs, retargeted consumer IDs, request ID and the
resolved preserved commit when declared. The existing `superseded` event and
lifecycle receipt remain the audit carriers. They do not store another full
replacement payload or introduce new `Task`/`State` fields.

The event's consumer list is the same complete set returned at commit. Exact
retained replay reads that original list from the source event identified by
the receipt's completed transition, even if consumer dependencies or the source
boundary later change. It does not reconstruct the list from the request or
live graph. Missing matching completion audit yields `STATE_CHANGED`/`requery`
without mutation. Candidate failure persists neither the list nor graph changes.

## Preserved base

`preserved_base` validates an already materialized worktree; it never creates
one. For fresh requests, `base_commit` must resolve to a commit and
`worktree` must equal `.worktrees/<replacement-id>`, exist, be healthy, be on
`task/<replacement-id>`, and have the declared commit as an ancestor of HEAD.
These checks run before the state transaction and their result is enforced after
receipt matching, before any fresh candidate is built. Validated canonical commit
and worktree values are written onto the replacement.

A **`base_commit`-only declaration is refused** with `INVALID_INPUT` and a
diagnostic for the missing worktree. Claim strategy selects preserved work by
the worktree field; without it, fresh claim would overwrite `base_commit` with
integration HEAD, silently losing the declaration's meaning. An absent or
unhealthy declared worktree is likewise rejected. Omit `preserved_base` entirely
for an ordinary fresh claim from integration HEAD.

Materialization remains separate work. `wt-create` branches from integration
and does not by itself establish the declared preserved-base contract. Later
claim still performs its own preserved-worktree checks and rebase.

## Recorded ceilings and reconciliation

1. **Receipt-pruning replay window.** Receipts retain at most four per operation
   and sixteen per task, within the last sixteen successful completions.
   Once pruned, the original request no longer proves completion: requery or
   stop, never reinterpret the expired request as a fresh mutation. An unbounded
   replay guarantee would require a separately reviewed retention design.
2. **Generation-digest replay across restarts.** Restart with unchanged
   registration identity can reuse a retained receipt. Re-registration changes
   its key: the old-token request returns `STATE_CHANGED`/`requery` rather than
   proof of completion. Source lifecycle and ID checks still prevent another
   lineage. Cross-generation replay would require a shared-carrier change.
3. **Fail-closed candidate validation.** Unlike standalone `add-task`'s warning
   posture for unrelated corruption, replacement rejects any invalid complete
   candidate. Repair pre-existing corruption before replacement; no degraded
   success path is provided.
4. **Class 4 closes only for callers declaring a preserved base.** The observed
   preserved-source drift class is covered when the caller supplies both the
   commit and an existing valid worktree. Omission remains valid and deliberately
   uses integration HEAD. Automatic materialization and undeclared preservation
   intent remain outside this operation; future materialization work owns that
   remaining gap.
5. **Standalone primitives remain individually non-atomic as a replacement
   workflow.** Each keeps its own transaction; a sequence of `add-task`,
   dependency repair and `supersede-task` is not one atomic replacement. Callers
   must adopt `replace-task` to obtain this guarantee.

Master output 7 owns shared-register reconciliation: add the transaction and
replay invariants to `INVARIANTS.md`, describe the source event and existing
receipt use in `specs/architecture/blackboard-schema.md` (no new fields), index
[ADR-0143](../architecture/ADR/0143-transactional-task-replacement.md), and
consolidate these ceilings and their payback triggers in `TECH_DEBT.md`.

## Evidence basis

This protocol records the approved Output 4 scope and replacement plan interface,
checked against the merged replacement ops, shared lifecycle receipt/result
helpers and `add-task`/`replace-task` schema implementations. Historical incident
claims come from the approved issue digest, not a fresh inspection of run logs.
