# Acceptance Evidence Admission

Strict coding tasks require a complete versioned proof mapping and successful
canonical execution before implementation review. The normative authoring grammar
and stack-neutral examples live in
[Acceptance Evidence](../../skills/shared/references/acceptance-evidence.md).
This protocol implements [ADR-0134](../architecture/ADR/0134-acceptance-evidence-admission.md).

## Authority and adoption

Select the task's `plan_ref`, otherwise `spec_ref`, at a captured integration
commit. A strict declaration is authoritative only when a direct, independently
approved MERGED planning or authorized direct-allocation architecture parent reviewed that file and allocated this child's refs and
ordered validation commands in `output[]`. The reviewed blob must match the
integration blob. Existing strict Source References continue to own requirement
meaning; the acceptance declaration allocates those IDs rather than creating a
second requirement catalogue.

Independent approval compares the reviewer with the planning author. A released
live assignment does not erase authorship: retained `submitted_for_review` history
for the exact parent `review_commit` supplies it. A retained assignee remains
compatible with legacy history, but conflicting identities, an unknown author,
or self-approval cannot authorize the allocation. Older submissions and claim
history cannot stand in for the reviewed boundary.

Claim records `acceptance_source`: ref, commit, blob, parent task and parent review
commit. Claim/reclaim checks parent evidence and allocation again in the final
transaction. Reviewed upstream corrections can refresh this snapshot and clear
the receipt; an adopted source cannot lose its marker to become legacy. Strict
sources discovered on an already executing task undergo the same checks at submit.
An unchanged independently reviewed historical parent source may remain adopted after a
strict contract correction only when ordered ancestry, the allocation and resolved proofs
still match effective current authority. Fresh children use the latest applied review;
successful stored receipts never gain authority by being rewritten. A pending amendment
fences applicable consumer admission/submission while its correction remains runnable.
Marker-free and reference-first-only legacy sources keep existing admission and
are identified as not machine acceptance-evidenced. A carrier with neither a
Source References nor an Acceptance Contract heading keeps its ref fragment as a
display hint: output submission and claim both admit an unresolvable one, while
a carrier with either heading has it resolved exactly at both.

## Admission boundaries

Planning submission preflights each prospective child's selected `plan_ref`
(otherwise `spec_ref`) in the candidate commit. Strict declarations use the same
parser as coding admission and must match the output's ordered validation commands.
Validation runs before submission preparation/rebase and again after rebase;
the final transaction requires the output allocation to remain unchanged. Regular
marker-free sources retain their existing behavior. Direct architecture allocation additionally
requires flat `coding_allocation: true` entries sharing one exact Scope, distinct exact
unit refs within that Scope, declared owned files and nonempty canonical commands; it
does not skip architecture's independent allocation review or the future coder's manifest.
Present nonregular or oversized
sources fail here using the same limits already enforced by coding admission.
This gate checks the declared
contract, not future implementation: it does not require the child's manifest,
proof files or runnable validation tools and does not establish parent approval.
Coding claim and submission still enforce those later boundaries independently.

Task creation (`add-task`, each `add-tasks` item, `replace-task`) runs claim's
allocation check on the candidate coding task at the current integration commit,
outside the state lock. It refuses, as field-attributed `INVALID_INPUT`, what
claim would refuse there: an unresolvable heading, validation that differs from
the reviewed commands, or a strict declaration without an allocating parent.
`add-task` cannot set a parent, so only the planning transition or a same-pair
`replace-task` of an allocated child can create a strictly allocated task. The
check adopts nothing and executes nothing. It judges one captured commit and does
not promise later claimability; a carrier present at integration must resolve at
creation even if a pending dependency would change it. Non-coding tasks, legacy
paths and carriers absent from integration are not judged. `replace-task` reports
the verdict after replay and returns `STATE_CHANGED` when the inherited parents'
evidence changed during the check. An unresolvable integration commit is
`RETRYABLE`.

1. Submission preflight resolves the committed manifest and validates exact-set
   obligation coverage before rebase or execution. Field diagnostics identify
   missing mappings, invalid proof files and unapproved exceptions.
2. After the existing rebase, reload source and manifest at the actual candidate
   boundary, require a clean worktree, and execute every canonical command not
   deferred (below) with the declared overall deadline and bounded masked output. Require unchanged
   HEAD and a clean worktree afterward. Execution occurs outside the state lock.
   A task declaring `runtime_inputs` acquires them here, after the clean-worktree
   check and before the first command: `single_use` instances are consumed
   durably, or the run is refused before anything executes
   ([runtime-inputs.md](runtime-inputs.md)).
3. Under the existing generation-fenced final transaction, recheck allocation,
   source, parent, validation, manifest and HEAD identities. Only success writes
   `acceptance_receipt` atomically with submitted status and `review_commit`.

The receipt contains version, immutable review commit, source identity, manifest
path/blob, complete mappings, masked command strings, exit codes, UTC timestamps
and masked output. Every command result requires `command_sha256`, the SHA-256
digest of the exact canonical command executed. Reviewer assignment compares this
digest with the current canonical command; the masked `command` field is display
text, not command identity. State holds execution results because committing a result that
names its own containing commit would require circular commit identity.
Precondition failures do not spend review cycles, release the doer, assign a
reviewer or mark `INTEGRATION_FAILED`.

### Deferred commands

A command is deferred when the reviewed contract lists its index in `deferred`
(authority `contract`) or an operator ruling in
`config.deferred_validation.rulings` names the exact command (authority
`operator`). A command a runtime input uses is never deferred: the contract may
not defer it, and a ruling does not apply to it. No manifest proof may map to a
contract-deferred index. Admission skips deferred commands, so `commands[]` holds
the executed commands in canonical order, and `deferred[]` records each skipped
one: `index`, masked `command`, `command_sha256`, `timeout_seconds` and
`authority`. The submission result lists them as `deferred_commands`. Reviewer
assignment requires each recorded deferral to be granted by the contract or a
current ruling, every contract deferral to be recorded, and executed plus
deferred commands to cover the canonical list. Withdrawing a ruling therefore
requires fresh admission for receipts that relied on it; a ruling added later
never invalidates an executed command. The MERGED write moves the deferrals to
the goal's deferred-validation ledger, whose batch satisfies them on integration
([ADR-0204](../architecture/ADR/0204-deferred-validation-tier.md)).

### Batch budget

Admission runs the executed commands within one batch budget: the reviewed
contract's `timeout_seconds`, raised to the largest `budget_exception` recorded
by `unblock-task --budget-exception --budget-ruling` on a coding task, never
lowered and never above the 14400 s cap. The same budget sets the in-flight
submit marker deadline. Receipts and `deferred[].timeout_seconds` keep the
reviewed value ([ADR-0207](../architecture/ADR/0207-admission-budget-exceptions.md)).

When an executed command matches a `config.admission_limits` class, the batch
first waits for that class's slot, before runtime inputs are acquired. The
budget starts once the slots are held; the wait is at most the cap minus the
budget, but a submit's is never under a 2 h floor; the marker then covers wait
and batch (at most cap + floor + grace). A submit waits
before preparing or rebasing; a class that stays full refuses
`acceptance.admission_slot` as `RETRYABLE` with no effects, and the same
submission is retried ([ADR-0209](../architecture/ADR/0209-admission-limits.md)).

With `config.admission_timeout_retry` set, a coding submit whose batch reaches
its own deadline re-runs the whole batch once, in the same slots and grant, if
the worktree is still clean and no `single_use` runtime input is declared. Its
budget is `budget_factor` × the batch budget, lowered to
`max_timeout_seconds`, the cap and the submit envelope (wait, batch and retry
within cap + floor); a retry shorter than the batch budget is not run. The
first marker covers the retry, and preparation refuses a batch or retry budget
other than the one that marker was published for (`STATE_CHANGED`, no command
run). A passing retry sets the receipt's `timeout_retry`; a timeout the policy
settles otherwise is refused as `batch_timeout` and the submit blocks the task,
fenced to its own transition ([ADR-0216](../architecture/ADR/0216-admission-timeout-retry-policy.md)).

A submit reports its queue position, preparation and batch on stderr, also
under `--json`, at each change and every 5 minutes; stdout carries only the
result. While a caller's submit of a task runs, that caller's next submit of
it queues and prepares nothing and returns `STATE_CHANGED`/`requery` at once
([ADR-0210](../architecture/ADR/0210-submit-progress-and-same-caller-hold.md)).

Failed execution returns the failing command's combined stdout/stderr in the
error, masked before an 8 KiB excerpt limit and explicitly marked if truncated.
Timeout and output-limit errors return no excerpt, since incomplete capture may
split a secret.
Diagnostic excerpts are not successful receipts; admission still fails and no
partial execution results are published as evidence.

During submit, canonical execution is fenced on the submitter's claim. Every 15
seconds a lock-free state snapshot is checked for the task still being in its
executing status, assigned to the submitting agent, under that agent's current
registration. Once that no longer holds, the running command's process group is
killed and submit returns a lifecycle error naming the lost ownership. The usual
outcome is `STATE_CHANGED`; it is `STALE_CALLER` when the registration itself is
gone. A snapshot that cannot be read never stops execution. Only the final
transaction admits. Repair through `update-review-commit` is not fenced.
Both successful output and failure diagnostics mask the current branded and legacy
agent-generation environment values as well as known secrets and connection values.

## Assignment, repair and cleanup

Normal and returning-reviewer assignment require a receipt matching the strict
source and current review boundary. The ordinary candidate loop collects
acceptance preconditions and skips invalid candidates without mutating them,
allowing healthy candidates to progress. An exhausted or explicit targeted claim
returns actionable diagnostics; the returning reviewer receives its task's error
directly. No commands execute during assignment.

Reviewers do not run a canonical command that uses a `single_use` runtime input:
the receipt is its evidence. They may run other live subsets with `run-live`.

`update-review-commit <task-id>` validates and executes strict evidence before
atomically replacing receipt and review boundary and releasing a prior reviewer.
It also repairs missing/stale evidence at unchanged HEAD. Failure preserves the
old task state. Legacy same-commit repair retains its existing no-op behavior.

Clear the live receipt whenever submitted-attempt metadata is cleared on
rejection, reclaim, recovery or retirement. Preserve adopted source identity to
prevent downgrade. Preserve a merged task's successful receipt as audit evidence:
once the task is terminal the receipt may leave live state for an immutable
archive object (see [Archived Task Fields](../architecture/blackboard-schema.md#archived-task-fields)),
and inspection restores it verbatim.
Reviewers read all mappings and command outcomes in their prompt, then use the
configured binary's `get tasks <task-id> --format json` for full recorded output.

## Validation boundary

This gate certifies declared mapping completeness and observed execution at a
commit. It does not certify assertion semantics, upstream requirement completeness,
environment reproducibility or arbitrary output secrecy. Ignored prerequisites
and external services remain project-owned. The authoring guide specifies limits,
masking expectations and independent review of non-executable exceptions.

Existing TDD, checkpoint, review-boundary, iteration and churn mechanisms remain
in force. Missing canonical prerequisites are admission failures, not successful
evidence and not grounds to substitute a weaker command.
