# 134 - Acceptance Evidence Admission

## Status

ACCEPTED — prospective adoption approved 2026-09-12.

## Context

Test filenames, checkpoint prose and green command reports do not establish that
every allocated acceptance obligation has inspectable evidence. Missing mappings
can consume implementation review cycles even when the code has become correct.
[ADR-0133](0133-reference-first-planning-artifacts.md) supplies stable source
obligation IDs and authoritative pinned references; it intentionally leaves
semantic completeness to independent review.

## Decision

Compose an optional versioned Acceptance Contract with the selected coding-task
source section. The declaration allocates existing Source References obligation
IDs, a manifest path, exact canonical commands, execution timeout and explicit
non-executable proof exceptions. It does not duplicate requirement prose in task
state. The [shared grammar](../../../skills/shared/references/acceptance-evidence.md)
and [runtime protocol](../../protocols/acceptance-evidence.md) define the format
and lifecycle.

Only a direct, independently approved MERGED planning parent that allocated the
child's refs and commands can authorize strict acceptance. Pin its carrier commit,
blob and review identity on claim, and recheck provenance at admission. Previously
adopted sources cannot downgrade through marker removal. Marker-free legacy tasks
remain reviewable with explicit absence of machine-validated acceptance evidence.
There is no new ad-hoc approval command or bulk migration.

Require a committed, strictly decoded manifest with exactly one executable mapping
or independently approved referenced exception per allocated ID. Resolve files
from immutable Git objects and reject unsafe paths and non-regular blobs. Execute
all canonical commands on clean post-rebase candidate code, outside the state
lock, with bounded runtime/output and secret masking. A command supplied by the
manifest cannot override the independently reviewed command list.

Record a successful `acceptance_receipt` in state atomically with submitted status
and immutable review commit after generation, source, manifest and HEAD rechecks.
The receipt holds validated mappings, provenance, outcomes, timestamps and masked
output. Each result requires `command_sha256` for the exact canonical command;
assignment compares this digest, while `command` remains masked display text.
Storing it in its own referenced Git commit would make identity circular.
Failures preserve admission state and do not consume review cycles.

Reviewer assignment requires matching evidence and never executes commands. A
bad candidate is skipped without mutation so healthy work progresses; exhausted
or targeted claims report the actionable precondition. Explicit
`update-review-commit` repair executes fresh validation, including for missing or
stale evidence at unchanged HEAD, and atomically replaces the receipt while
invalidating prior reviewer assignment. Attempt cleanup clears live receipts but
preserves adopted authority; merged receipts remain audit evidence.

This extends [ADR-0007](0007-tdd-enforcement-in-mas.md)'s test-presence gate,
[ADR-0030](0030-code-enforced-agent-guardrails.md)'s runtime enforcement boundary,
[ADR-0072](0072-declared-validation-commands.md)'s canonical commands and
[ADR-0078](0078-repairable-review-boundary-metadata.md)'s explicit boundary repair.
It preserves the existing approval and immutable-commit mechanisms.

## Consequences

- A green incomplete declared suite fails before implementation review with
  field-level diagnostics. Reviewers receive complete mappings and outcomes and
  can inspect full receipts through existing task JSON inspection.
- Strict tasks add bounded execution latency and state storage. External services
  and ignored prerequisites remain project-owned; missing prerequisites block.
- A receipt proves execution and declared mapping completeness, not meaningful
  assertions or a semantically complete upstream allocation. Independent planning
  and code review, TDD and churn limits remain necessary.
- Known secret masking limits accidental exposure; commands still must produce
  sanitized output. Commit binding does not prove every external/environment input.
- The schema is additive and legacy state remains readable. Rollback proceeds in
  reverse dependency order: presentation surfaces, admission enforcement, then
  otherwise inert contract/parser types.

## Alternatives Considered

- **Self-reported committed results:** smaller but allow fabricated or stale
  success, preserving the evidence-trust gap.
- **Reviewer checklist only:** improves review instructions but missing mappings
  still spend reviewer capacity and admission remains unchecked.
- **Duplicate task-level requirement catalogue:** creates competing authority
  with ADR-0133. Reuse its IDs and pin provenance instead.
- **Domain-specific test recognition:** cannot serve arbitrary project stacks
  and would confuse syntactic recognition with semantic correctness.
