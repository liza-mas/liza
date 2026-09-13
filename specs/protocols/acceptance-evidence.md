# Acceptance Evidence Admission

Strict coding tasks require a complete versioned proof mapping and successful
canonical execution before implementation review. The normative authoring grammar
and stack-neutral examples live in
[Acceptance Evidence](../../skills/shared/references/acceptance-evidence.md).
This protocol implements [ADR-0134](../architecture/ADR/0134-acceptance-evidence-admission.md).

## Authority and adoption

Select the task's `plan_ref`, otherwise `spec_ref`, at a captured integration
commit. A strict declaration is authoritative only when a direct, independently
approved MERGED planning parent reviewed that file and allocated this child's refs and
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
Marker-free and reference-first-only legacy sources keep existing admission and
are identified as not machine acceptance-evidenced.

## Admission boundaries

1. Submission preflight resolves the committed manifest and validates exact-set
   obligation coverage before rebase or execution. Field diagnostics identify
   missing mappings, invalid proof files and unapproved exceptions.
2. After the existing rebase, reload source and manifest at the actual candidate
   boundary, require a clean worktree, and execute every canonical command with
   the declared overall deadline and bounded masked output. Require unchanged
   HEAD and a clean worktree afterward. Execution occurs outside the state lock.
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

Failed execution returns the failing command's combined stdout/stderr in the
error, masked before an 8 KiB excerpt limit and explicitly marked if truncated.
Timeout and output-limit errors return no excerpt, since incomplete capture may
split a secret.
Diagnostic excerpts are not successful receipts; admission still fails and no
partial execution results are published as evidence.
Both successful output and failure diagnostics mask the current branded and legacy
agent-generation environment values as well as known secrets and connection values.

## Assignment, repair and cleanup

Normal and returning-reviewer assignment require a receipt matching the strict
source and current review boundary. The ordinary candidate loop collects
acceptance preconditions and skips invalid candidates without mutating them,
allowing healthy candidates to progress. An exhausted or explicit targeted claim
returns actionable diagnostics; the returning reviewer receives its task's error
directly. No commands execute during assignment.

`update-review-commit <task-id>` validates and executes strict evidence before
atomically replacing receipt and review boundary and releasing a prior reviewer.
It also repairs missing/stale evidence at unchanged HEAD. Failure preserves the
old task state. Legacy same-commit repair retains its existing no-op behavior.

Clear the live receipt whenever submitted-attempt metadata is cleared on
rejection, reclaim, recovery or retirement. Preserve adopted source identity to
prevent downgrade. Preserve a merged task's successful receipt as audit evidence.
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
