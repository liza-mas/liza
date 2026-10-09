# 197 - Reviewed Plan Amendments and Validation Notes

## Status

ACCEPTED. Extends [ADR-0159](0159-orchestrator-plan-handoff-disposition.md)
and [ADR-0181](0181-provider-output-dependencies.md).

## Context

D-88 exposed a handoff deadlock: review requested a merged plan's correction,
but replan retires its provider identity and correctly refuses when a started
consumer still references it. Clearing a human hold does not release that
reference. Harmless hook/probe guidance also took this retirement route even
though the reviewed allocation did not need to change.

## Decision

1. **Separate reviewed work.** `amend-plan ORIGINAL --reason TEXT` creates a
   same-pair correction with `amends_plan: ORIGINAL`, through the ordinary
   planner/submission/independent-review/merge lifecycle. In the same state
   transaction, it fences ORIGINAL's handoff. ORIGINAL stays MERGED with its
   immutable review attribution, ID and existing output slots. Begin requires
   a planning output with no children, executed transitions, retirement or
   human hold; authorities follow operator or generation-fenced orchestrator
   rules. Correction tasks never generate children or serve as providers.
2. **Bounded adoption.** `amend-plan ORIGINAL --apply CORRECTION` accepts only
   its exact pending, independently approved MERGED correction with immutable
   ordered review/merge ancestry. Existing output slots retain description,
   completion criteria, scope, artifact references, kind, supersession,
   decomposition, changed and permission/classification fields. Only reviewed
   dependency, inherited-input, validation, validation-prerequisite and
   runtime-input fields may change; new producer slots may append. Count/index
   equality alone is insufficient identity evidence. Candidate graph and
   unexpanded-source checks run under the adopting lock. Apply clears pending
   and the old pass/notes, records applied provenance and retires only the
   correction's handoff; any later human hold remains sticky. A repeated apply
   is a no-op, never a hold release or payload update.
3. **Recover without releasing the fence.** `amend-plan ORIGINAL
   --replace-pending CORRECTION --reason TEXT` quarantines the exact MERGED
   unapplied or ABANDONED correction and creates fresh same-pair reviewed work
   atomically. Active corrections, wrong IDs, expanded originals and applied
   corrections refuse. The original fence and human hold remain. The fresh
   review must reconcile artifact changes already merged by its predecessor.
   No terminal task reopens and no abort releases a fence over drifted evidence.
4. **Preserve evidence.** Typed `plan_amendment` retains the original output
   manifest and ordered correction/applied/quarantined IDs. Original review
   metadata stays immutable; latest applied review is current acceptance and
   provider-reference authority. Original and earlier independently reviewed
   merged manifests, including reconciled quarantined predecessors, may prove
   the authorship of an unchanged allocation, never current authority. Require
   matching spans, allocation, proof references and ordered ancestry. Children
   keep ORIGINAL as their parent, with the actual effective reviewed commit;
   acceptance race digests include referenced correction records. Parent
   reference context retains original and relevant merged correction carriers.
5. **Advisory notes.** `plan-check ORIGINAL --pass --notes-file FILE` accepts
   strict JSON `{output_index, message}` records: unique existing non-dedup
   slots, nonblank UTF-8 messages up to 4096 bytes, total input at most 16384
   bytes, no unknown fields. Identical replay is a no-op; changed notes on an
   existing pass require clear/recheck before generation. Each selected child
   receives `validation_notes` with original parent/output provenance, including
   crash recovery, and both doer and reviewer see them. They do not alter
   canonical validation, acceptance, runtime inputs or provider declarations.

## Consequences

- Every manual, automatic, many-to-one and recovery generation path respects a
  pending amendment; pending work keeps integration unsettled. The original
  needs a fresh handoff disposition after adoption.
- Active correction review stays quiet. A MERGED or ABANDONED pending correction
  wakes PLANNING_COMPLETE for apply or fresh-review recovery, even when the
  original is held; neither readiness nor wake releases its fence/hold. An
  unchanged unhandled disposition is recorded and suppressed until new input.
- Started consumers retain their original provider declarations and ordinary
  satisfied dependencies. Retirement safeguards remain unchanged.
- Missing producers/provisioning, material scope/order/contracts and live input
  declarations require reviewed correction or human action. Probe/hook notes
  cannot excuse those omissions. Changes outside the slot boundary require
  replan with authorized consumer retirement.
- A merged correction can become unappliable after integration/provider drift;
  pending replacement obtains fresh review while retaining the safety fence.
- Acceptance-commit repair must handle amendment evidence or refuse explicitly
  before writing; it cannot rewrite immutable attribution as a shortcut.

## Alternatives Considered

- Reopen ORIGINAL: makes an ordinary dependency cease to be MERGED and mixes
  historical and current review evidence.
- Suppress provider retirement checks or retarget output indexes: breaks live
  consumer identity and independently reviewed allocation guarantees.
- Clear the fence after failed apply: authorizes generation over evidence that
  may already have changed in integration.
