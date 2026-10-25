# 222 - A3 Ops Kernel Concerns and Vocabulary Ownership

## Status

ACCEPTED.

## Context

The architecture-consolidation run splits the A3 area of `internal/ops` (the
ops kernel and execution lifecycle) along concerns until no A3 function or file
exceeds the size guard, and moves its finite control-flow vocabulary and magic
values to named owners without a new configuration field
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
Before the split, claim, blocked-episode, ownership-release and run-control
logic sat in a few very large files that mixed phases, policy and effect
ordering. The decision is indexed in the [ADR README](README.md).

[ADR-0021](0021-ops-service-layer-for-mutations.md) makes `internal/ops` the
service layer for state mutations and [ADR-0130](0130-generation-fenced-agent-authority.md)
fences agent writes by registration generation. This ADR keeps both: it
records how A3 is laid out inside that boundary and where its vocabulary lives.
[ADR-0220](0220-a1-typed-vocabulary.md) and
[ADR-0221](0221-a2-typed-vocabulary-ownership.md) settled the typed-vocabulary
rules for A1 and A2 (defined string types, no codec methods, conversion at
boxed-string boundaries, unknown values unchanged); this ADR applies them to A3.

It records the layout and placement approved in
[Scope 5 of the A3 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-203240-arm-1-ar-12.md#scope-5-kernel-vocabulary-and-run-control)
and delivered by its coding tasks. The occurrence-by-occurrence evidence is the
[A3 K7 inventory](../../plans/20261023-architecture-consolidation/acceptance/arm-1-ar-12-cp-4-inventory.md).

## Decision

### Concern layout

1. **The kernel stays inside `internal/ops`.** A3 adds no package, dependency,
   framework or configuration field, and keeps the exported operations and the
   concrete error types. Concerns are separated by private, concern-named files
   and functions. Generation fencing, request identity, receipts, error causes
   and locks remain separate concerns of the one package, and `ops` remains
   application-layer with no mutation crossing its boundary.

2. **Concern files, by operation family.**

   | Concern | Files (all `internal/ops/`) | Separated responsibility |
   |---|---|---|
   | Claim | `claim_task.go`, `claim_task_admission.go`, `claim_task_assignment.go`, `claim_task_worktree.go`, `claim_task_cleanup.go`, `claim_task_stranded.go`, `claim_task_strategy.go` | Request → authority and eligibility snapshot and strategy → worktree preparation → locked revalidation and assignment; cleanup distinguishes resources created here from a winning claimant's |
   | Blocked episode | `mark_blocked.go`, `mark_blocked_transaction.go`, `assess_blocked.go`, `assess_blocked_transaction.go`, `assess_blocked_awaited.go`, `assess_blocked_fingerprint.go`, `assess_hypothesis_exhausted.go`, `unblock_task.go`, `unblock_transaction.go` | Input validation → current episode, awaited set and human ask → fingerprint or transition → durable history and receipt; unblock keeps one restore body for preflight and final state, with optional rebase preparation outside the state lock |
   | Ownership release and recovery | `release_claim.go`, `release_claim_descriptor.go`, `handoff.go`, `recover_task.go` with `recover_task_snapshot.go`, `recover_task_fresh.go`, `recover_task_release.go`, `recover_task_artifacts.go`, `recover_agent.go`, `recover_agent_claims.go`, `transition_attempt.go` | Captured claim and artifact evidence → operation-specific preservation or retirement → atomic ownership update → permitted cleanup; release, handoff, recovery and attempt rollover keep their distinct effect order |
   | Kernel | `agent_authority.go`, `lifecycle_authority.go`, `lifecycle_invocation.go`, `lifecycle_receipt.go`, `lifecycle_result.go`, `lifecycle_metrics.go`, `ownership_lifecycle.go`, `operational_error.go`, `state_lock_holds.go`, `agent_lifecycle_lock.go`, `project_lifecycle_lock.go`, `project_lock.go`, `work_admission.go`, `admission_slot.go`, `runtime_input_*.go`, `config.go` | Shared generation authority, request identity, receipts and results, locks, admission and runtime inputs, configuration |
   | Run control | `mode_change.go`, `resume_transitions.go`, `set_auto_resume.go` | `resume` decides mode and sprint inside one locked transaction (`resumeDecision`); `reportResumeTransitions` runs after commit and reports failures without returning them, so the committed resume stands. Timestamps stay local to the decision |

   A helper extracted from a transaction receives explicit state or operation
   context, never acquires another state lock and never starts Git or process
   effects (INVARIANTS §5 lock discipline). Locks, runtime-input consumption and
   launch prerequisites keep their owner and effect order. No shim, alias or
   forwarder is introduced; the A3 shim register is empty, and the A3 size
   ratchet is empty.

3. **Rejected: a kernel sub-package.** Moving the kernel to
   `internal/ops/<kernel>` would relocate many mutually used state and receipt
   helpers, force exported seams between them and the operations, and add a
   migration with no K3 crossing to resolve. The private concern files give the
   same separation without it.

4. **Rejected: a generic lifecycle engine.** Claim, release, recovery,
   handoff, rollover and assess differ in transaction shape and effect order
   (for example rollover commits a sentinel then the final state before
   cleanup, while agent recovery performs Git effects outside the state
   modification and rechecks the generation afterward). A common engine would
   hide those orderings. The existing claim strategies and release descriptors
   (`release_claim_descriptor.go`) already absorb the real variation (K6).

### Vocabulary ownership

The ownership table of the plan numbers the families I1–I27. Every one resolves
to exactly one of: an A1 type adopted, an ops concern type, a retained owner, a
named constant, or a classified dynamic set.

5. **A1 vocabulary is adopted, not redefined (I1–I6, I9).**

   | ID | Set | Owner and A3 use |
   |---|---|---|
   | I1 | Claim type | `roles.ClaimType` (`roles.ClaimDoer`, `ClaimReviewer`, `ClaimBoth`); converted with `string(...)` at the command argument in `repair_doer_ownership.go` |
   | I2 | Role type | `roles.RoleTypeOrchestrator` on the resolver result in `resolve_orchestrator.go` |
   | I3 | Built-in role names | existing `roles.Orchestrator`, `roles.Coder`, `roles.CodeReviewer`; names stay dynamic (ADR-0045), no type |
   | I4 | History events | `models.TaskEvent…` |
   | I5 | Anomaly types | `models.AnomalyType…` (for example `AnomalyTypeAgentDegraded`) |
   | I6 | Cardinality, trigger, route, impact, diversity, inherit mode, discovery, RCA | A1 types V3–V5, V8–V12; the A3 re-probe found no occurrence, so nothing is adopted |
   | I9 | Lifecycle outcomes | existing `models.Lifecycle…`; reused, not redefined |

6. **A3 owns the finite sets that only its concerns decide on.** Each is a
   defined type over `string` with one constant per value, declared beside the
   code that branches on it, and has no codec or `IsZero` method:

   | ID | Type | Concern |
   |---|---|---|
   | I7 | `LifecycleSafeAction` (`continue`, `stop`, `requery`, `retry`, `correct_input`; `requery` and `correct_input` take their value from `models.FieldDiagnosticRequery` and `FieldDiagnosticCorrectInput`) | lifecycle result (kernel) |
   | I8 | `LifecycleEffects` (`none`, `unknown`, `committed`) | lifecycle result (kernel) |
   | I10 | `runtimeInputCode` (consumed, invalidated, missing, unavailable; existing full spelling) | runtime-input ledger |
   | I11 | `stateLockHoldReason` | state-lock holds |
   | I12 | `assessmentOutcome`, `awaitedState` | blocked episode |
   | I13 | `configSetOutcome` | configuration |
   | I16 | `preservedClaimRecovery` | claim strategy |
   | I24 | `ReviewClaimClass` (exported; the supervisor breaker keys on it) | claim failure |
   | I25 | `AgentDegradedReason` (exported) | agent health |

   `LifecycleSafeAction` and `LifecycleEffects` are exported, so every A4–A7
   caller and test double that receives or produces them conforms by type
   substitution or conversion. `ReviewClaimClass` conforms the same way in
   `internal/agent` and in the reviewer-claim producers. No caller logic
   changes (X2); untyped constant arguments at those callers compile unchanged
   and their literals are left for their own area to adopt (X7).

7. **Existing owners are retained (I14, I23).** `LimitAction`,
   `attemptStateCleanupProfile`, `admissionTier`, `resumeOrigin`,
   `AgentOwnershipState`, `reviewerCapacityInvalidReason` and the eight
   `*ConfigKey` constants keep their types and names. No parallel type is
   introduced.

8. **Named values, not types, for values that do not drive a finite
   decision (I15, I17–I22, I26).** They become named constants at the concern
   that uses them, with unchanged values and byte-identical lock names:
   lock and namespace labels (I15), timing defaults (I17), counts and
   thresholds such as `maxBlockedQuestions`, `hypothesisExhaustionFailures`,
   `preservedClaimEvidenceLimit` and `models.MaxTaskAttempt` (I18), sizes,
   key lengths and modes (I19), the circuit-breaker status pair (I20; no
   `models` constant exists and A3 does not edit `models`), Git tree entry
   modes (I21), the broadcast target, operation names and control sentinels
   (I22), and map-key and error-code names (I26). Test-mutable timing knobs
   such as `admissionLimitRefreshInterval` stay variables, initialised from
   their named default.

9. **Dynamic sets stay dynamic (I27).** Role names, tool and provider names,
   command names and arguments, diagnostic fields and classes, config values,
   agent, task and request IDs, and user text are not closed into types. Each
   retained literal is classified in the K7 inventory: named declaration,
   message or error text, or path and error-text matcher.

### AC3 string-boundary rule

10. **A vocabulary value is converted to `string` where it leaves its typed
    position for a string-kinded consumer.** A defined type boxed into an
    `any` fails a later `.(string)` assertion and changes how a `map[string]any`
    or template renders it, so conversion happens at insertion, never at the
    reader. In A3 the boundaries are:
    - `models.LifecycleOutcome.SafeAction`, `FieldDiagnostic.SafeAction`,
      `SafeDetails` and every `map[string]any` built for a lifecycle result;
    - anomaly `Details`, diagnostics and message composition (runtime-input
      codes, review-claim classes);
    - persisted and hashed fields: `StateLockHold.RefusedReason`, the
      refusal fingerprint, assessment dependency records
      (`assessmentDependency.Outcome`), the awaited-set projection, lifecycle
      payload hashing, and history `Extra`;
    - presentation-wire results (`ConfigSetResult.Outcome`), the agent-health
      `Reason`, and the repair-request operation of a preserved claim.

    These carriers keep their `string` kind, so wire fields, persisted bytes,
    hashed material and omission behaviour are unchanged. The prefix test on
    the `unblock_` hold reasons runs on the string form.

11. **No behaviour or surface change.** Error message text, `--json` codes,
    `state.yaml` effects and every oracle golden are unchanged. Types name
    finite decisions that already exist; they add no validation, default or
    alternate branch, and unknown values behave as before.

12. **Scope limits.** `jsonout`'s dependency on `ops` and the crossing
    resolution stay with A7. The vocabulary issue in
    [architectural-issues.md](../architectural-issues.md) stays open for the
    later areas, and the overview is rewritten only at the run's close.

## Consequences

- Every A3 file and function is within the size guard, and each concern can
  be read and tested without the others' phases; the cost is more files in
  one package.
- A misspelled or mixed-up lifecycle action, effect, runtime-input code,
  hold reason or review-claim class is a compile error for typed positions.
  The types do not check membership: an untyped constant or explicit
  conversion still compiles, so membership stays enforced by existing runtime
  validation.
- A caller that forgets the AC3 conversion compiles but fails at run time
  where a `string` assertion follows. The oracle goldens, the state-lock and
  lifecycle-result tests, and the explicit conversion at each boundary above
  are the defense.
- Exported type changes ripple to A4–A7 and `internal/agent` call sites and
  doubles once; later areas adopt the new constants when they are refactored.
- The unexported circuit-breaker status constants remain in `ops` until a
  later area gives `models` its own.

## Alternatives Considered

1. **Kernel sub-package:** rejected; see Decision 3.
2. **Generic lifecycle engine:** rejected; see Decision 4.
3. **Retype persisted model and wire fields to the new A3 types:** rejected;
   it would change A1 surfaces and risk persisted bytes, hash inputs and
   wire output. Those fields stay `string` and values are converted at the
   boundary.
4. **Close command names, diagnostic classes or config values into types:**
   rejected; these are open or user-supplied sets (I27).
5. **One shared vocabulary file for all A3 sets:** rejected; each set stays
   beside the concern that branches on it, as in ADR-0221.
