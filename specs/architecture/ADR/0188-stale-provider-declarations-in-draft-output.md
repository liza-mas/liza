# 188 - Stale Provider Declarations in Draft Output

## Status

ACCEPTED. Amends [ADR-0185](0185-stale-provider-declarations-on-unexpanded-plans.md)
(Decision 1) and [ADR-0186](0186-stale-selected-child-slots-on-unexpanded-plans.md)
(Decision 1).

## Context

ADR-0185 releases an unexpanded plan's output declaration from holding a
retired provider, and keeps "outputs of plans not yet MERGED" holding. In
practice a MERGED host plan could not be replanned because two BLOCKED
architecture tasks, each rejected once, and a third awaiting review declared it
in their unmerged output (D-69). Generation runs only from MERGED output, so
nothing had been generated from those declarations, and each had to be
re-authored and re-reviewed before anything could be. Holding them only
deferred the retirement until the drafts merged, when ADR-0185 would release
them anyway and each consumer would then need a replan instead of an in-place
rework.

## Decision

1. **Draft output.** Output is a draft when its owner is in its role pair's
   initial, executing, rejected, submitted, reviewing or quorum status, BLOCKED,
   or INTEGRATION_FAILED. The role pair's approved status, which reaches MERGED
   without another verdict, and any status the pipeline does not name are not.
2. **Retirement.** A draft output declaration goes stale under the
   unexpanded-plan rule: it holds neither a provider it names directly, on any
   retirement, nor a selected child retired permanently. A child replan still
   holds. Approved output holds as before.
3. **Retention.** The declaration stays unchanged. State validation accepts the
   retired provider or child in draft output; bounds, provenance and cycles are
   still checked.
4. **Re-authoring in place.** `set-task-output` refuses to author a declaration
   naming a retired provider or child, submission refuses stored output that
   still carries one, and an approving verdict on such output is refused and
   asks the reviewer to reject. The consumer keeps its identity, worktree and
   lineage.
5. **Escalation.** The retiring transaction adds a re-authoring question and a
   `provider_declaration_stale` history entry to each BLOCKED consumer whose
   draft output it leaves stale. Both change the consumer's assessment
   fingerprint, so the BLOCKED_TASKS wake re-triages it even without an
   ordinary edge on the provider. Other draft owners meet the submission or
   approval refusal on their own path (rework, review, integration-fix
   resubmission).
   `replan` warns about each draft it leaves stale.

## Consequences

- A provider whose consumers hold only draft output can be replanned or retired
  directly; each consumer re-authors its output against the successor.
- External reconciliation (`reconcile-merged`) records a git merge without a
  verdict. Applied to an INTEGRATION_FAILED draft carrying a stale declaration,
  it yields ADR-0185's unexpanded plan with a stale declaration: hand-off
  classification reports it, generation refuses it, and a replan re-authors it.
- A retirement while output is approved is still refused; once merged and
  unexpanded, ADR-0185 releases the direct reference.

## Alternatives Considered

1. Release only BLOCKED or rejected drafts (the operator's initial scope).
   Rejected: a draft awaiting review already held the same provider, so it
   would have surfaced as the next holder.
2. Block or reject every stale draft in the retiring transaction. Rejected: it
   disrupts active authoring and review ownership, which the refusals already
   route.
3. Refuse `reconcile-merged` on stale output. Rejected: state would contradict
   git with no in-place recovery, while the ADR-0185 state already keeps the
   stale plan from generating anything.
