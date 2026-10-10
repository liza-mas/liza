# 198 - Frozen Interface Corrections and Direct Coding Allocation

## Status

ACCEPTED. Extends [ADR-0192](0192-admission-checks-child-feasibility-known-consumers.md)
and [ADR-0197](0197-reviewed-plan-amendments-and-validation-notes.md).

## Context

Issue 18 observed repeated architecture reopening during coding: missing consumer
operations, fields and statuses reached providers after interface freeze; unused-plan
amendments could not repair expanded architecture, and destructive replan could cascade
through provider selectors. A single architecture Scope also repeated allocation in a
code-planning stage, while task listings mixed outstanding handoffs with completed history.

## Decision

1. **Early consumer asks at existing checkpoints.** Epics record a Provider Ask Inventory
   before downstream decomposition: interface ID, consumer epic/ref and source-backed
   operations, fields and statuses (or explicit none). Provider authors and independent
   reviewers reconcile coverage with assigned epics and obligations, answer or concretely
   defer every ask, and also cover merged consumers. Unavailable declared asks prevent the
   affected scope's freeze; unresolved needs require allocated investigation/correction and
   a legal wait on the corrected contract. D-78 child-feasibility checks remain. These are
   semantic duties, without a new global phase or executable edges inferred from strings.
2. **Strict identity-preserving correction modes.** `amend-plan ORIGINAL --contract
   --reason TEXT` commissions independently reviewed correction of an architecture's
   existing referenced contract prose, including after expansion. `replan ORIGINAL
   --preserve-output-identity` commissions same-pair `ORIGINAL-replan-N` work through
   reciprocal amendment lineage; it neither retires ORIGINAL nor rewrites consumer
   selectors. Both adopt through `amend-plan ORIGINAL --apply CORRECTION`. Their complete
   ordered output manifest must remain identical, including refs, ownership, classifications,
   commands, prerequisites, provider/runtime declarations and fanout. Architecture edits are
   limited to an existing referenced Scope's exact nested `#### CONTRACT` subsection; the
   surrounding Scope and other subsections stay byte-identical and bare refs refuse edits.
   For unused nonarchitecture planning originals, preserving identity permits referenced
   plan prose changes outside frozen strict acceptance allocations. New files, implementation
   changes, acceptance allocation changes and altered approved resolved proofs refuse.
   The CONTRACT subsection must already exist at the reviewed base; missing or duplicate
   subsections refuse. Expanded use is architecture-only;
   preserving an unused planning output is also supported. Legacy empty-mode output-edit rules and
   destructive replan retain their earlier semantics.
3. **Authority and fences remain explicit.** ORIGINAL stays MERGED with immutable review
   attribution, children and transition markers. Pending correction fences generation and
   applicable consumer admission/submission. In every mode, including legacy empty mode,
   declared input/parent ancestry remains fenced through MERGED intermediates. This accepts
   temporary conservative waits instead of inferring which contracts an intermediate passed on.
   Correction work remains runnable but cannot
   generate or serve as a provider. Apply requires independent review, ordered integration
   ancestry, unchanged allocation/proof evidence, current artifact checks and locked candidate
   validation. Incompatible adoption retains the fence for reviewed pending replacement.
   Historical child source SHAs survive only when their independently reviewed ancestry and
   allocation/resolved proofs still match current authority; receipts are never rewritten to
   make evidence pass. Fresh children adopt the latest applied review. Human holds survive.
4. **One Scope may allocate coding directly.** Flat output entries opt in with
   `coding_allocation: true`, all sharing one exact `arch_ref` Scope. Each distinct exact
   `plan_ref` names a coding-unit heading inside that Scope with strict Acceptance Contract,
   future coder-authored manifest path, `decomposition.owned_files` and nonempty canonical
   validation matching the declaration. Design and allocation receive architecture review
   together. Specialized `architecture-to-coding` and master `architecture-main-to-coding`
   are named manual per-subtask routes; quorum and orchestrator disposition remain. Explicit
   `when: coding-allocation` and `when: scope-decomposition` select exclusive routes without
   changing legacy transition names or target identities. Fanout retains specialized
   architecture/code-planning. RCA-required work, kind markers and one-intermediate-layer
   `descendant_dependencies` refuse direct allocation; immediate provider waits preserve writer
   order. No frozen configuration is migrated automatically; marked output on an unsupported
   legacy topology refuses rather than silently choosing a different route.
5. **Make residual repair visible.** Typed `planning_change` records correction/replan kind,
   explicit trigger and original task. `get metrics` groups creation once per logical task by
   UTC day, kind, trigger and original architecture classification, including archived records;
   it discloses unknown attribution and missing creation times without guessing from IDs or
   reason prose. `get tasks` and `get-tasks` default to inspection-only active work; `--all`
   requests history and `--active` remains explicit. Outstanding selected handoffs (including
   held/refused/undecided work), pending amendment originals and exact merged pending
   corrections stay visible. Completed selected routes, retired/replanned originals and
   applied/quarantined corrections do not. Explicit task/field lookups remain complete.
   Lifecycle, assignment and claim terminal predicates are unchanged.

## Consequences

- Stable provider/output identity avoids a consumer replan when contract prose changes within
  unchanged executable allocations. Incompatible scope or proof changes still require a
  different reviewed recovery path; these modes do not offer an output alias graph.
- Early inventory completeness remains author/reviewer judgment. Rendered tests establish
  guidance presence, not consumer completeness; undeclared future consumers still route gaps.
- Direct coding removes repeated planning only when the architect supplies the full reviewed
  allocation. It does not remove independent review, acceptance execution, writer ordering,
  provider readiness or manual handoff disposition.
- Local validation can establish these mechanisms and refusal boundaries. Reductions in the
  reported daily architecture reopenings and planning/coding ratio require a comparable
  subsequent run; they are not claimed by implementation tests.

## Alternatives Considered

1. A global consumer-inventory phase: rejected in favor of existing semantic checkpoints.
2. Provider aliases, selector rewrites or nested coding units: rejected; existing amendment
   provenance and flat output positions preserve the smaller identity boundary.
3. Broad shared terminal-predicate changes: rejected; inspection visibility must not change
   assignment singularity or prior-owner reconciliation.
