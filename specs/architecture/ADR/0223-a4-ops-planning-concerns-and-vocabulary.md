# 223 - A4 Ops Planning Concerns and Vocabulary Ownership

## Status

ACCEPTED.

## Context

The architecture-consolidation run splits the A4 area of `internal/ops` (planning
and the task graph) along concerns until no A4 function or file exceeds the size
guard, and moves its finite control-flow vocabulary and magic values to named
owners without a new configuration field
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
Before the split, transition expansion, plan hand-off, dependency repair,
replacement and sprint coordination sat in a few very large files
(`proceed.go`, `add_tasks.go` and 21 functions over the limit) that mixed
validation, candidate construction, effect ordering and reporting. The decision
is indexed in the [ADR README](README.md).

[ADR-0021](0021-ops-service-layer-for-mutations.md) makes `internal/ops` the
service layer for state mutations, and [ADR-0048](0048-multi-phase-planning.md)
defines the phase-gate transition mechanism A4 implements. This ADR keeps both:
it records how A4 is laid out inside that boundary and where its vocabulary
lives. [ADR-0222](0222-a3-ops-kernel-concerns-and-vocabulary.md) settled the
kernel layout and the A3 vocabulary that A4 consumes, and
[ADR-0220](0220-a1-typed-vocabulary.md) the typed-vocabulary rules (defined
string types, no codec methods, conversion at boxed-string boundaries, unknown
values unchanged); this ADR applies them to A4.

It records the layout and placement approved in
[Scope 6 of the A4 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-205627-arm-1-ar-13.md#scope-6-sprint-coordination-and-a4-closure-readiness)
and delivered by its coding tasks. The occurrence-by-occurrence evidence is the
[A4 K7 inventory](../consolidation-run/a4-k7-inventory.md).

## Decision

### Concern layout

1. **Planning stays inside `internal/ops`.** A4 adds no package, dependency,
   framework or configuration field, and keeps every exported operation, input
   and result type. Concerns are separated by private, concern-named files and
   functions. `ops` remains application-layer, no mutation crosses its boundary,
   and the A4 guard data (layer, allowlist, ratchet, S2) is empty: no S1
   offender remains and no exception was needed.

2. **Concern files, by operation family.** Entry files keep their original
   name and the exported entry; the private files take the separated phases.

   | Concern | Files (all `internal/ops/`) | Separated responsibility |
   |---|---|---|
   | Transition expansion | `proceed.go`, `transition_pass.go`, `transition_children.go`, `transition_cohort.go`, `transition_inherited_deps.go`, `transition_recovery.go`, `add_tasks.go`, `add_tasks_delivery.go`, `set_task_output.go`, `set_task_output_checks.go`, `planning_output_acceptance.go`, `direct_coding_allocation.go` | Output or request → resolver and preflight → ordered per-subtask, one-to-one and many-to-one decisions → atomic child construction, cohort completion and inherited phase-gate dependencies → crash recovery and cleanup. Output authoring splits request checks, state checks and the checked write; task addition splits delivery-unit planning from creation |
   | Plan governance | `plan_check.go`, `plan_check_decision.go`, `plan_handoff.go`, `plan_handoff_failure.go`, `plan_handoff_undecided.go`, `plan_replacement.go`, `replan.go`, `amend_plan.go`, `plan_amendment_begin.go`, `contract_amendment.go`, `contract_amendment_compare.go`, `obligation_drift.go`, `obligation_drift_carrier.go` | Source and reviewed carrier → classification or correction comparison → fenced adoption → hand-off result. Hand-off classification, refusal and undecided records, the plan-declared replacement barrier, amendment begin/apply, bounded-contract comparison and obligation drift are separate files; the decision is made apart from the history and alert it produces |
   | Graph repair | `retarget_dependency.go`, `retarget_dependency_rewrite.go`, `narrow_inherited_dependencies.go`, `narrow_inherited_edges.go`, `apply_dependency_repair.go`, `dependency_repair_request.go`, `repair_superseded_dependencies.go`, `dependency_rewrite.go`, `dependency_direction.go`, `descendant_dependencies.go`, `provider_dependencies.go`, `defer_provider_dependency.go`, `reserve_provider.go`, `repair_arch_ref.go` | Explicit expected edges and selections → effective lineage and candidate rewrite → full locked validation → audit and result. Eligibility, planning of the rewrite and adoption are separate steps; the shared rewrite, direction and provider helpers stay shared |
   | Replacement and retirement | `replace_task.go`, `replace_task_admission.go`, `replace_task_candidate.go`, `supersede_task.go`, `cancel_task.go`, `delete_task.go`, `delete_task_removal.go`, `task_retirement_cleanup.go`, `predecessor_cleanup.go`, `lifecycle_deferred_retirement.go`, `task_archive.go`, `archive_terminal_tasks.go` | Original intent and ownership evidence → atomic replacement or retirement → durable receipt → post-commit cleanup and archive projection. Replace splits request checks, in-lock admission and candidate projection; delete splits blockers, in-lock removal and worktree cleanup; Git work stays outside the state transaction |
   | Sprint coordination | `pipeline_ops.go`, `advance_sprint.go`, `sprint_checkpoint.go`, `sprint_summary.go`, `update_sprint_metrics.go`, `set_discovery_disposition.go`, `analyze.go`, `orchestrator_wake.go`, `human_note.go` | Resolver, hand-off and integration evidence → checkpoint, wake and metric decision → existing state and report publication. Checkpoint admission, trigger resolution, report staging and the locked update are separate steps; completion authorization is its own concern in `pipeline_ops.go`; wake queries, metrics and breaker decisions stay distinct |

   A helper extracted from a transaction receives explicit state, resolver and
   request context and never acquires another state lock. Extraction adds no
   Git or process effect inside a `Modify` callback (INVARIANTS §5 lock
   discipline). Git or `exec` calls that the starting tree already made inside
   a callback are inherited K1 exceptions (D33, generalised by D49): they stay
   in the same transaction with the same checks, order and refusals, and may
   move with their operation but never out of the transaction nor weaker.
   Representative site: `AmendPlan` applies a correction inside its `Modify`
   callback (`applyPlanAmendment`), whose bounded-contract validation
   (`validateBoundedContractCorrection`) calls the extracted
   `compareChangedContractFiles` (`contract_amendment_compare.go`); that
   comparison reads `DiffFiles` and `TreeEntryAt` through the Git adapter,
   exactly as the code it was extracted from did. Operations keep their
   transaction, error, authority and effect order: completion linearization →
   integration mutation → blackboard read is preserved and no mutation lock is
   held across a blackboard write. Extraction adds no validation, swallows no
   error and keeps the first refusal. Timestamps stay at their call points,
   including the local-time sprint and report timestamps. No shim, alias or
   forwarder is introduced; the A4 shim register is empty.

3. **Rejected: capability sub-packages.** Splitting the families above into
   `internal/ops/<capability>` packages would move the state, receipt, graph
   and rewrite helpers that several families call (for example
   `applyDependencyUpdatesInState`, `supersedeTaskInState`,
   `cleanupPredecessorBranches`, `mergedPlanLineageRepair`) behind exported
   seams, and would add a migration with no K3 crossing to resolve. Private
   concern files give the same separation inside one package.

4. **Rejected: a generic transition or repair engine.** The families differ in
   atomicity and cleanup policy: transition expansion commits children and
   recovers a crashed pass, replacement retires an original under a durable
   receipt with Git cleanup after the commit, graph repair validates a complete
   candidate with no cleanup, sprint coordination publishes a report. A common
   engine would hide those orderings behind callbacks. The real variation is
   already absorbed by the existing cardinality-specific generators, the
   `PlanCheckAction` and `PlanHandoffClass` decisions and the per-operation
   candidate builders (K6).

### Retained exported surface

5. **The exported operations and types are unchanged.** `Proceed`,
   `ExecuteAvailableTransitions`, `ExecuteTransitionsReportWith`, `Replan`,
   `AddTasks*`, `SetTaskOutput*`, `AmendPlan`, `RecordPlanCheck`, the
   dependency-repair, replace, supersede, cancel, delete and archive entries,
   `SprintCheckpoint`, `CheckpointPassedPlans`, `AdvanceSprint`,
   `UpdateSprintMetrics*`, `CheckSuspiciousRates`, `Analyze`,
   `SetDiscoveryDisposition`, `AddHumanNote` and their input and result types
   keep their names and signatures.

6. **Plan hand-off classification stays exported for A8 and A9.** `prompts`
   still reads `PlanHandoffDomain` (`LoadPlanHandoffDomain`,
   `PlanningCompleteEligible`, `Classify`, `ReadyPlanCorrection`, `HasHeldPlan`),
   the `PlanHandoff*` classes, `PlanningPairsOnly` and the wake counters
   (`CountActionableBlockedTasks`, `UnseenHumanNotes`,
   `CountReadyManyToOneCohorts`); the runtime and commands call the transition,
   checkpoint, wake and note operations. A8 owns relocating the prompt-facing
   types (SI-5), after which these exports are its to change. A4 adds no
   forwarder for them.

7. **`pipelineBundle` keeps its concrete resolver.** The completion and
   transition code reads diversity policy through `pipelineBundle.resolver`
   (`*pipeline.Resolver`) next to the `models.PipelineResolver` interface. That
   plumbing and the tool-name behaviour are the C2 site and are preserved as is;
   canonical provider-key semantics remain C2's work.

### Vocabulary ownership

The plan numbers the A4 families J1–J22 and applies five owner rules: R1 adopt
the delivered owner constant, R2 follow the first owner's classification, R3
name an owner gap in `ops` without editing the closed owner, R4 name or type a
new A4 value in its concern, R5 classify literal data. Every family resolves to
exactly one of these. The evidence lists 1,761 literals in the 60 A4 files with
no literal left unclassified.

8. **Adopted A1 and A3 owners (R1, R2: J1–J10, J17, J19).** A4 consumes and
   never redefines these sets:

   | ID | Set | Owner and A4 use |
   |---|---|---|
   | J1 | Lifecycle safe actions and effects | A3 `LifecycleSafeAction` and `LifecycleEffects` constants in every A4 function that builds a lifecycle error or outcome; no raw safe-action or effects literal remains |
   | J2, J3 | Transition cardinality and trigger | `models.TransitionCardinality` and `models.TransitionTrigger` constants (for example `CardinalityManyToOne`); `string(...)` where a string-kinded carrier requires it |
   | J4 | Role type | `roles.RoleType` constants |
   | J5 | History events | `models.TaskEvent…` |
   | J6 | `replanned` marker | `models.ReplannedTransition` |
   | J7 | Circuit-breaker status `OK`/`TRIGGERED` | A3 `circuitBreakerStatusOK` and `circuitBreakerStatusTriggered` in `ops` (owner gap, below) |
   | J9 | Human-note broadcast target `all` | A3 `humanNoteForAll` |
   | J10 | Operator provenance | A3 `humanNoteSourceOperatorCLI`, `actorOperator`, `actorSystem` |
   | J17 | Transition-reference phase names | `pipeline.StateCategory…` constants |
   | J19 | Retained typed owners and sibling names | `TransitionAdmission`, `PlanCheckAction`, `PlanHandoffClass`, `upstreamKind`, `narrowAction`, `reservationChange`, `obligationDriftChange`, `models.PlanAmendmentMode`, `models.PlanCheckVerdict`, the per-operation name constants and `notFoundEntityTask`; no parallel type |

9. **A4 owns two new finite sets (R4: J11).** Each is a defined type over
   `string` with one constant per value, declared beside the code that branches
   on it, with no codec or `IsZero` method:

   | Type | Values | Concern |
   |---|---|---|
   | `discoveryDisposition` | `deferred`, `dismissed` (the sentinels that are not a task ID) | discovery disposition; its `isSentinel` method also serves the A6 caller `deferredFixInFlight` |
   | `handoffRefusalClass` | `output_validation`, `selective_inheritance`, `provider_retirement` | plan hand-off refusal; selects the fingerprint and retirement branches |

   The exported `PlanHandoffFailure.Class` stays a `string`, converted at the
   boundary, so the stored class and the failure fingerprint are unchanged. The
   refusal text naming both disposition sentinels is byte-identical.

10. **Owner gaps are named in `ops`, not fixed in the owner (R3).** A closed
    package outside `ops` that never named a value keeps its form (X1); A4 names
    the value once, at the consuming concern, and lists the other occurrences
    for their area to adopt (X7): `providerAuditDegradationPattern` (`analysis`
    repeats the literal and the C2 grouping is untouched), the rejection-RCA
    history keys that `analyze.go` reads and A5 writes (`blocked_class`,
    `gated_at`, `first_rejection_at`, `causes`, `recovery_path`),
    `addTaskOperation` (`payloadschema` registers the literal with no
    constant), `addHumanNoteOperation` (read by the prompt context), the
    transition-keyed history keys, and the alert categories `PLAN HANDOFF
    FAILED` and `PLAN DISPOSITION MISSING`. Where A1 did deliver a constant
    that the plan assumed missing, it was adopted instead.

11. **Named values, not types (R4: J12–J16, J18, J20).** They become named
    constants in the A4 concern that uses them, with unchanged values:
    the completion-authorization reason codes `integration_incomplete` and
    `integration_state_changed` and the lock and label names
    (`integrationCompletionLockName`, `completionProgressionLabelPrefix`; the
    state-changed code moved from A3 `mode_change.go`, whose two sites keep the
    identifier); the thresholds `suspiciousRateMinCount` (3),
    `suspiciousRatePercent` (95), `amendReasonMaxBytes` (4096) and the
    A3 `hypothesisExhaustionFailures` for the wake; the two retirement policy
    versions folded into the provider-refusal fingerprint
    (`reservationTransferPolicyVersion`, `selectedChildLineagePolicyVersion`);
    the report and archive file and directory modes; the history `Extra` and
    detail keys that A4 reads back (`transitionExtraKey`, `cycleMembersExtraKey`,
    `outputIndexExtraKey`, the replacement and obligation-drift keys).

12. **Configured identities stay dynamic (J14, J22).** Role-pair, transition,
    role and provider names come from the pipeline configuration and reach the
    code as variables. The two hard-coded role-pair spellings that are not read
    from configuration are named once and documented as fallbacks, not typed:
    `legacyPlanningRolePair` (`code-planning-pair`) and
    `baseCommitSnapshotRolePair` (`coding-pair`). Task, agent and discovery IDs
    and user text are likewise dynamic. No configuration field is added.

13. **Literal data is classified, not named (R5: J20, J21).** Field names of
    references (`plan_ref`, `arch_ref`, `epic_ref`, `spec_ref`), log action
    names, error and message text, the `sprint-%d` ID format, report headings
    and arithmetic identities (an SCC size of two, hours per day, a percent
    divisor) are not control-flow vocabulary. Same-spelling values with another
    meaning (for example the `manual` history marker key or the `none` display
    label) are recorded as false friends, not merged with the typed sets.

### AC3 string-boundary rule

14. **A vocabulary value is converted to `string` where it leaves its typed
    position for a string-kinded consumer**, as in
    [ADR-0222](0222-a3-ops-kernel-concerns-and-vocabulary.md): history `Extra`,
    anomaly and error `Details`, log actions, fingerprint and hash material,
    result fields and `map[string]any`. In A4 this covers the cardinality
    written to history, the handoff refusal class, the discovery disposition
    stored in a task reference and the safe action and effects in
    lifecycle details. Persisted bytes, hashed material and JSON field types
    are unchanged.

15. **No behaviour or surface change.** Error text, `--json` codes,
    `state.yaml` effects, report bytes, lock names, alert categories and every
    oracle golden are unchanged. Types name decisions that already exist; they
    add no validation, default or alternate branch.

16. **Scope limits.** The vocabulary issue and the `proceed.go` decomposition
    issue in [architectural-issues.md](../architectural-issues.md) stay open
    here: later areas still adopt the A4 names at their sites (A5–A9 are listed
    in the inventory), and A4 closures belong to the A5 boundary unit. The
    overview is rewritten only at the run's close.

## Consequences

- Every A4 file and function is within the size guard, and each concern can be
  read and tested without the others' phases; the cost is more files in one
  package and a longer file list per operation.
- A misspelled discovery disposition or hand-off refusal class is a compile
  error for typed positions. The types do not check membership: an untyped
  constant or explicit conversion still compiles, so membership stays enforced
  by existing runtime validation.
- A forgotten AC3 conversion compiles but changes how a value is stored or
  hashed. The spelling tests (`TestA4VocabularySpelling`), the fingerprint
  tests and the oracle goldens are the defense.
- The exported ops surface for A7–A9 is unchanged, so later areas move
  relocated types once, under their own boundary, not as part of A4.
- Owner-gap names live in `ops` until their owners are refactored; the same
  spelling then exists in two packages and the inventory lists each site.

## Alternatives Considered

1. **Capability sub-packages:** rejected; see Decision 3.
2. **Generic transition or repair engine:** rejected; see Decision 4.
3. **Retype the exported `PlanHandoffFailure.Class` or persisted fields:**
   rejected; it would change A1 and wire surfaces and risk stored fingerprints.
   Those carriers stay `string` and values are converted at the boundary.
4. **Close role-pair, transition or discovery IDs into types:** rejected;
   these are configured or user-supplied sets (J22).
5. **Edit `models`, `analysis` or `payloadschema` to add the missing
   constants:** rejected for this area; closed owners are not edited by A4
   (X1), and the owner-gap names are recorded for their area.
6. **One shared vocabulary file for all A4 sets:** rejected; each set stays
   beside the concern that branches on it, as in ADR-0221 and ADR-0222.
