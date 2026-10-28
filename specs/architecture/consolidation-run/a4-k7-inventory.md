# A4 K7 Inventory

Evidence for `arm-1-ar-13-cp-5` Task 2 (`arm-1-ar-13-cp-5-code-1`): the complete K7 occurrence-to-owner
classification of the A4 ops files, the substitutions applied, the owner gaps, the later-area adoption
sites and the check of the sibling K7 commitments. Source of truth for the rules is the plan section
"K7 Occurrence-to-Owner Inventory" (J1–J22, R1–R5).

## Coding HEAD

- Integration HEAD at claim (A1, A3, A4 oracle and Scopes 2–6 Task 1 delivered): `1368da851240acd7eafebb7551aacfba9f6de211`.
- Oracle-only checkpoint (seven `vocab-` cases, goldens generated from the starting tree by `regen -mode write-new`): `bb2329009a9b2a71c515311d349f6d7a136482f0`.
- The tables describe the tree after the substitutions of this task, committed on top of the oracle checkpoint; line numbers are that tree's. Table 2 is derived from `git diff bb2329009 -- internal/ops/`.

## Method

1. File list: the 41 `A4` files of the area partition plus the 19 concern files the A4 scopes created (names and creating scope in table 1). `init_project.go` (A7) and A3/A5/A6 concern files are out of the list.
2. Every string and numeric literal of those 60 production files was extracted with a `go/parser` scan (import paths, struct tags and rune literals excluded) with its syntactic context (comparison, `case`, composite field, map key or index, call argument, declaration…): **1761 literals**.
3. Each literal's value was matched against every string constant of `ops` and the exported constants of the A1 owner packages (`models`, `pipeline`, `roles`, `taskkind`, `paths`, `alerts`, `errors`, `payloadschema`, `referencecontract`, `statehygiene`, `statevalidate`, `analysis`, …). A same-value, same-meaning owner constant is adopted (R1); a false friend (same spelling, other meaning) is recorded in table 4.
4. A literal that is control-flow vocabulary without an owner is named (R4) or, for a finite set deciding control flow, typed (R4/J11); an owner gap outside `ops` is named in `ops` without editing the owner (R3, table 5).
5. Every remaining literal is classified (R5/J22) with a reason in table 3; the classifier fails on a literal it cannot place, so no literal is left unclassified or deferred.

Outcome vocabulary: **adopted** (existing owner constant used), **named** (new constant, value unchanged), **typed** (new defined type, one constant per value), **classified** (retained literal data or dynamic identity, with reason).

## Summary

| Outcome | Count |
|---|---|
| Substitution sites in this task (table 2: adopted, named or typed use sites) | 176 |
| Constant and type declarations added by this task (table 2) | 47 |
| Pre-existing named owner constants in A4 files (table 3b) | 49 |
| Retained literals classified R5/J22 (table 3, grouped by file and reason) | 1661 |
| Unclassified / deferred | 0 |

## 1. A4 files

| File | Origin | Lines | Literals scanned | Substitution sites | Declarations added | Pre-existing named constants |
|---|---|---|---|---|---|---|
| `proceed.go` | area partition | 638 | 63 | 9 | 1 | 0 |
| `replan.go` | area partition | 599 | 73 | 0 | 0 | 1 |
| `add_tasks.go` | area partition | 417 | 52 | 11 | 1 | 0 |
| `set_task_output.go` | area partition | 565 | 70 | 8 | 1 | 3 |
| `replace_task.go` | area partition | 180 | 5 | 0 | 0 | 0 |
| `supersede_task.go` | area partition | 464 | 57 | 1 | 0 | 1 |
| `retarget_dependency.go` | area partition | 337 | 38 | 1 | 0 | 4 |
| `narrow_inherited_dependencies.go` | area partition | 345 | 37 | 1 | 0 | 4 |
| `apply_dependency_repair.go` | area partition | 335 | 30 | 3 | 0 | 0 |
| `dependency_rewrite.go` | area partition | 423 | 41 | 0 | 0 | 1 |
| `dependency_direction.go` | area partition | 52 | 6 | 0 | 0 | 0 |
| `descendant_dependencies.go` | area partition | 99 | 14 | 1 | 0 | 0 |
| `provider_dependencies.go` | area partition | 350 | 44 | 5 | 0 | 0 |
| `defer_provider_dependency.go` | area partition | 123 | 25 | 3 | 0 | 1 |
| `reserve_provider.go` | area partition | 314 | 47 | 1 | 0 | 4 |
| `repair_superseded_dependencies.go` | area partition | 258 | 24 | 1 | 0 | 1 |
| `repair_arch_ref.go` | area partition | 162 | 32 | 3 | 0 | 1 |
| `plan_check.go` | area partition | 282 | 55 | 0 | 0 | 5 |
| `plan_replacement.go` | area partition | 460 | 47 | 3 | 0 | 0 |
| `plan_handoff.go` | area partition | 369 | 39 | 0 | 0 | 8 |
| `plan_handoff_failure.go` | area partition | 238 | 58 | 20 | 6 | 1 |
| `plan_handoff_undecided.go` | area partition | 182 | 30 | 5 | 1 | 1 |
| `amend_plan.go` | area partition | 268 | 65 | 2 | 1 | 0 |
| `contract_amendment.go` | area partition | 71 | 16 | 0 | 0 | 0 |
| `obligation_drift.go` | area partition | 450 | 27 | 20 | 8 | 4 |
| `planning_output_acceptance.go` | area partition | 247 | 40 | 0 | 0 | 0 |
| `direct_coding_allocation.go` | area partition | 119 | 11 | 1 | 0 | 0 |
| `pipeline_ops.go` | area partition | 518 | 24 | 8 | 5 | 0 |
| `advance_sprint.go` | area partition | 490 | 30 | 2 | 2 | 0 |
| `sprint_checkpoint.go` | area partition | 250 | 25 | 1 | 1 | 0 |
| `update_sprint_metrics.go` | area partition | 98 | 8 | 4 | 2 | 0 |
| `set_discovery_disposition.go` | area partition | 58 | 9 | 9 | 2 | 0 |
| `analyze.go` | area partition | 379 | 35 | 13 | 7 | 0 |
| `orchestrator_wake.go` | area partition | 364 | 14 | 2 | 0 | 0 |
| `human_note.go` | area partition | 81 | 14 | 6 | 1 | 0 |
| `cancel_task.go` | area partition | 225 | 12 | 1 | 0 | 0 |
| `delete_task.go` | area partition | 169 | 13 | 0 | 0 | 0 |
| `predecessor_cleanup.go` | area partition | 72 | 4 | 0 | 0 | 0 |
| `lifecycle_deferred_retirement.go` | area partition | 118 | 5 | 0 | 0 | 0 |
| `task_archive.go` | area partition | 154 | 16 | 0 | 0 | 1 |
| `archive_terminal_tasks.go` | area partition | 154 | 10 | 0 | 0 | 2 |
| `add_tasks_delivery.go` | concern file (Scope 2) | 445 | 37 | 0 | 0 | 0 |
| `contract_amendment_compare.go` | concern file (Scope 3) | 124 | 8 | 0 | 0 | 0 |
| `delete_task_removal.go` | concern file (Scope 5) | 104 | 10 | 2 | 0 | 0 |
| `dependency_repair_request.go` | concern file (Scope 4) | 77 | 10 | 0 | 0 | 0 |
| `narrow_inherited_edges.go` | concern file (Scope 4) | 324 | 40 | 1 | 0 | 0 |
| `obligation_drift_carrier.go` | concern file (Scope 3) | 154 | 7 | 0 | 0 | 0 |
| `plan_amendment_begin.go` | concern file (Scope 3) | 165 | 27 | 0 | 0 | 2 |
| `plan_check_decision.go` | concern file (Scope 3) | 282 | 41 | 0 | 0 | 0 |
| `replace_task_admission.go` | concern file (Scope 5) | 224 | 42 | 9 | 4 | 3 |
| `replace_task_candidate.go` | concern file (Scope 5) | 155 | 10 | 2 | 0 | 0 |
| `retarget_dependency_rewrite.go` | concern file (Scope 4) | 202 | 37 | 0 | 0 | 0 |
| `set_task_output_checks.go` | concern file (Scope 2) | 166 | 19 | 4 | 0 | 0 |
| `sprint_summary.go` | concern file (Scope 6) | 175 | 73 | 2 | 0 | 0 |
| `task_retirement_cleanup.go` | concern file (Scope 5) | 25 | 2 | 0 | 0 | 0 |
| `transition_children.go` | concern file (Scope 2) | 429 | 40 | 3 | 0 | 0 |
| `transition_cohort.go` | concern file (Scope 2) | 222 | 23 | 0 | 0 | 0 |
| `transition_inherited_deps.go` | concern file (Scope 2) | 191 | 7 | 0 | 0 | 0 |
| `transition_pass.go` | concern file (Scope 2) | 621 | 42 | 5 | 4 | 1 |
| `transition_recovery.go` | concern file (Scope 2) | 304 | 21 | 1 | 0 | 0 |

## 2. Adopted, named and typed (substitutions of this task)

One row per changed line and constant. `A3 conformance` rows are in A3 files; all others are in A4 files.

| file:line | J | Rule | Outcome | Constant | Replaces | Owner |
|---|---|---|---|---|---|---|
| `add_tasks.go:23` | J13 | R4 | declaration of named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:121` | J13 | R4 | named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:126` | J13 | R4 | named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:140` | J13 | R4 | named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:150` | J13 | R4 | named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:155` | J13 | R4 | named | `addTaskOperation` | "add-task" | A4 add_tasks.go (`payloadschema` owner gap: no constant) |
| `add_tasks.go:376` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `add_tasks.go:376` | J19 | R1 | adopted | `resumeOperation` | "resume" (command hint) | A3 mode_change.go |
| `add_tasks.go:387` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `add_tasks.go:387` | J19 | R1 | adopted | `resumeOperation` | "resume" (command hint) | A3 mode_change.go |
| `add_tasks.go:401` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `add_tasks.go:401` | J19 | R1 | adopted | `resumeOperation` | "resume" (command hint) | A3 mode_change.go |
| `advance_sprint.go:30` | J16 | R4 | declaration of named | `sprintArchiveDirMode` | 0755 | A4 advance_sprint.go |
| `advance_sprint.go:31` | J16 | R4 | declaration of named | `sprintArchiveFileMode` | 0644 | A4 advance_sprint.go |
| `advance_sprint.go:159` | J16 | R4 | named | `sprintArchiveDirMode` | 0755 | A4 advance_sprint.go |
| `advance_sprint.go:163` | J16 | R4 | named | `sprintArchiveFileMode` | 0644 | A4 advance_sprint.go |
| `amend_plan.go:20` | J15 | R4 | declaration of named | `amendReasonMaxBytes` | 4096 | A4 amend_plan.go |
| `amend_plan.go:96` | J15 | R4 | named | `amendReasonMaxBytes` | 4096 | A4 amend_plan.go |
| `amend_plan.go:97` | J15 | R4 | named | `amendReasonMaxBytes` | 4096 | A4 amend_plan.go |
| `analyze.go:23` | J8 | R3 | declaration of named | `providerAuditDegradationPattern` | "provider_audit_degradation" | A4 analyze.go; `analysis` owner gap |
| `analyze.go:26` | J16 | R4 | declaration of named | `circuitBreakerReportFileMode` | 0644 | A4 analyze.go |
| `analyze.go:31` | J18 | R4 | declaration of named | `rejectionRCABlockedClassKey` | "blocked_class" | A4 analyze.go (writer: A5 submit_verdict.go) |
| `analyze.go:32` | J18 | R4 | declaration of named | `rejectionRCAGatedAtKey` | "gated_at" | A4 analyze.go (writers: A5 rejection_rca.go, submit_verdict.go) |
| `analyze.go:33` | J18 | R4 | declaration of named | `rejectionRCAFirstRejectionAtKey` | "first_rejection_at" | A4 analyze.go (writer: A5 submit_verdict.go) |
| `analyze.go:34` | J18 | R4 | declaration of named | `rejectionRCACausesKey` | "causes" | A4 analyze.go (writer: A5 rejection_rca.go) |
| `analyze.go:35` | J18 | R4 | declaration of named | `rejectionRCARecoveryPathKey` | "recovery_path" | A4 analyze.go (writer: A5 rejection_rca.go) |
| `analyze.go:112` | J8 | R3 | named | `providerAuditDegradationPattern` | "provider_audit_degradation" | A4 analyze.go; `analysis` owner gap |
| `analyze.go:124` | J7 | R1/R2 | adopted | `circuitBreakerStatusOK` | "OK" | A3 mode_change.go (I20) |
| `analyze.go:131` | J7 | R1/R2 | adopted | `circuitBreakerStatusOK` | "OK" | A3 mode_change.go (I20) |
| `analyze.go:137` | J16 | R4 | named | `circuitBreakerReportFileMode` | 0644 | A4 analyze.go |
| `analyze.go:192` | J18 | R4 | named | `rejectionRCABlockedClassKey` | "blocked_class" | A4 analyze.go (writer: A5 submit_verdict.go) |
| `analyze.go:195` | J18 | R4 | named | `rejectionRCAGatedAtKey` | "gated_at" | A4 analyze.go (writers: A5 rejection_rca.go, submit_verdict.go) |
| `analyze.go:196` | J18 | R4 | named | `rejectionRCAFirstRejectionAtKey` | "first_rejection_at" | A4 analyze.go (writer: A5 submit_verdict.go) |
| `analyze.go:205` | J18 | R4 | named | `rejectionRCAGatedAtKey` | "gated_at" | A4 analyze.go (writers: A5 rejection_rca.go, submit_verdict.go) |
| `analyze.go:206` | J18 | R4 | named | `rejectionRCACausesKey` | "causes" | A4 analyze.go (writer: A5 rejection_rca.go) |
| `analyze.go:208` | J18 | R4 | named | `rejectionRCARecoveryPathKey` | "recovery_path" | A4 analyze.go (writer: A5 rejection_rca.go) |
| `analyze.go:329` | J7 | R1/R2 | adopted | `circuitBreakerStatusOK` | "OK" | A3 mode_change.go (I20) |
| `analyze.go:344` | J7 | R1/R2 | adopted | `circuitBreakerStatusTriggered` | "TRIGGERED" | A3 mode_change.go (I20) |
| `analyze.go:345` | J7 | R1/R2 | adopted | `circuitBreakerStatusTriggered` | "TRIGGERED" | A3 mode_change.go (I20) |
| `apply_dependency_repair.go:191` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `apply_dependency_repair.go:275` | J1 | R1 | adopted | `LifecycleEffectsNone` | "none" | A3 lifecycle_result.go |
| `apply_dependency_repair.go:275` | J1 | R1 | adopted | `LifecycleSafeActionRequery` | "requery" | A3 lifecycle_result.go |
| `cancel_task.go:142` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `defer_provider_dependency.go:56` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `defer_provider_dependency.go:99` | J10 | R1/R2 | adopted | `humanNoteSourceOperatorCLI` | "operator_cli" | A3 release_blocked_chain.go |
| `defer_provider_dependency.go:115` | J10 | R1 | adopted | `actorOperator` | "operator" | A3 helpers.go |
| `deferred_validation.go:1564` (X2 caller conformance, A6 file) | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `delete_task_removal.go:38` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `delete_task_removal.go:79` | J9 | R1/R2 | adopted | `humanNoteForAll` | "all" | A3 assess_blocked_fingerprint.go |
| `descendant_dependencies.go:66` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `direct_coding_allocation.go:69` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `human_note.go:19` | J10 | R4 | declaration of named | `addHumanNoteOperation` | "add-human-note" | A4 human_note.go |
| `human_note.go:49` | J10 | R1/R2 | adopted | `humanNoteSourceOperatorCLI` | "operator_cli" | A3 release_blocked_chain.go |
| `human_note.go:50` | J10 | R4 | named | `addHumanNoteOperation` | "add-human-note" | A4 human_note.go |
| `human_note.go:58` | J9 | R1/R2 | adopted | `humanNoteForAll` | "all" | A3 assess_blocked_fingerprint.go |
| `human_note.go:59` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `human_note.go:70` | J10 | R1 | adopted | `actorOperator` | "operator" | A3 helpers.go |
| `human_note.go:74` | J9 | R1/R2 | adopted | `humanNoteForAll` | "all" | A3 assess_blocked_fingerprint.go |
| `narrow_inherited_dependencies.go:186` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `narrow_inherited_edges.go:319` | J19 | R1 | adopted | `transitionSentinelPrefix` | "$" | A3 transition_attempt.go |
| `obligation_drift.go:71` | J18 | R4 | declaration of named | `driftDetailPath` | "path" | A4 obligation_drift.go |
| `obligation_drift.go:72` | J18 | R4 | declaration of named | `driftDetailHeading` | "heading" | A4 obligation_drift.go |
| `obligation_drift.go:73` | J18 | R4 | declaration of named | `driftDetailChange` | "change" | A4 obligation_drift.go |
| `obligation_drift.go:74` | J18 | R4 | declaration of named | `driftDetailReviewedSection` | "reviewed_section" | A4 obligation_drift.go |
| `obligation_drift.go:75` | J18 | R4 | declaration of named | `driftDetailCurrentSection` | "current_section" | A4 obligation_drift.go |
| `obligation_drift.go:76` | J18 | R4 | declaration of named | `driftDetailCarriers` | "carriers" | A4 obligation_drift.go |
| `obligation_drift.go:77` | J18 | R4 | declaration of named | `driftDetailObligations` | "obligations" | A4 obligation_drift.go |
| `obligation_drift.go:78` | J18 | R4 | declaration of named | `driftDetailReferenceIDs` | "reference_ids" | A4 obligation_drift.go |
| `obligation_drift.go:156` | J18 | R4 | named | `driftDetailCarriers` | "carriers" | A4 obligation_drift.go |
| `obligation_drift.go:157` | J18 | R4 | named | `driftDetailObligations` | "obligations" | A4 obligation_drift.go |
| `obligation_drift.go:158` | J18 | R4 | named | `driftDetailReferenceIDs` | "reference_ids" | A4 obligation_drift.go |
| `obligation_drift.go:164` | J18 | R4 | named | `driftDetailChange` | "change" | A4 obligation_drift.go |
| `obligation_drift.go:165` | J18 | R4 | named | `driftDetailReviewedSection` | "reviewed_section" | A4 obligation_drift.go |
| `obligation_drift.go:174` | J18 | R4 | named | `driftDetailPath` | "path" | A4 obligation_drift.go |
| `obligation_drift.go:175` | J18 | R4 | named | `driftDetailHeading` | "heading" | A4 obligation_drift.go |
| `obligation_drift.go:176` | J18 | R4 | named | `driftDetailChange` | "change" | A4 obligation_drift.go |
| `obligation_drift.go:177` | J18 | R4 | named | `driftDetailReviewedSection` | "reviewed_section" | A4 obligation_drift.go |
| `obligation_drift.go:178` | J18 | R4 | named | `driftDetailCurrentSection` | "current_section" | A4 obligation_drift.go |
| `obligation_drift.go:179` | J18 | R4 | named | `driftDetailCarriers` | "carriers" | A4 obligation_drift.go |
| `obligation_drift.go:180` | J18 | R4 | named | `driftDetailObligations` | "obligations" | A4 obligation_drift.go |
| `obligation_drift.go:181` | J18 | R4 | named | `driftDetailReferenceIDs` | "reference_ids" | A4 obligation_drift.go |
| `obligation_drift.go:384` | J18 | R4 | named | `driftDetailPath` | "path" | A4 obligation_drift.go |
| `obligation_drift.go:385` | J18 | R4 | named | `driftDetailHeading` | "heading" | A4 obligation_drift.go |
| `obligation_drift.go:386` | J18 | R4 | named | `driftDetailCurrentSection` | "current_section" | A4 obligation_drift.go |
| `obligation_drift.go:398` | J18 | R4 | named | `driftDetailChange` | "change" | A4 obligation_drift.go |
| `obligation_drift.go:413` | J18 | R4 | named | `driftDetailCarriers` | "carriers" | A4 obligation_drift.go |
| `obligation_drift.go:413` | J18 | R4 | named | `driftDetailObligations` | "obligations" | A4 obligation_drift.go |
| `obligation_drift.go:413` | J18 | R4 | named | `driftDetailReferenceIDs` | "reference_ids" | A4 obligation_drift.go |
| `orchestrator_wake.go:97` | J9 | R1/R2 | adopted | `humanNoteForAll` | "all" | A3 assess_blocked_fingerprint.go |
| `orchestrator_wake.go:285` | J15 | R1 | adopted | `hypothesisExhaustionFailures` | 2 | A3 hypothesis_exhaustion.go (I18) |
| `pipeline_ops.go:21` | J13 | R4 | declaration of named | `integrationCompletionLockName` | "integration-completion" | A4 pipeline_ops.go |
| `pipeline_ops.go:22` | J13 | R4 | declaration of named | `completionProgressionLabelPrefix` | "progression " | A4 pipeline_ops.go |
| `pipeline_ops.go:23` | J12 | R4 | declaration of named | `integrationIncompleteCode` | "integration_incomplete" | A4 pipeline_ops.go |
| `pipeline_ops.go:24` | J12 | R4 | declaration of named | `integrationStateChangedCode` | "integration_state_changed" (declaration relocated from A3 mode_change.go; both A3 sites keep the identifier) | A4 pipeline_ops.go |
| `pipeline_ops.go:30` | J14 | R4 | declaration of named | `legacyPlanningRolePair` | "code-planning-pair" | A4 pipeline_ops.go |
| `pipeline_ops.go:63` | J13 | R4 | named | `integrationCompletionLockName` | "integration-completion" | A4 pipeline_ops.go |
| `pipeline_ops.go:86` | J13 | R4 | named | `completionProgressionLabelPrefix` | "progression " | A4 pipeline_ops.go |
| `pipeline_ops.go:132` | J12 | R4 | named | `integrationIncompleteCode` | "integration_incomplete" | A4 pipeline_ops.go |
| `pipeline_ops.go:210` | J12 | R4 | named | `integrationStateChangedCode` | "integration_state_changed" (declaration relocated from A3 mode_change.go; both A3 sites keep the identifier) | A4 pipeline_ops.go |
| `pipeline_ops.go:220` | J12 | R4 | named | `integrationStateChangedCode` | "integration_state_changed" (declaration relocated from A3 mode_change.go; both A3 sites keep the identifier) | A4 pipeline_ops.go |
| `pipeline_ops.go:387` | J2 | R1 | adopted | `models.CardinalityManyToOne` | "many-to-one" | A1 models |
| `pipeline_ops.go:428` | J14 | R4 | named | `legacyPlanningRolePair` | "code-planning-pair" | A4 pipeline_ops.go |
| `pipeline_ops.go:462` | J14 | R4 | named | `legacyPlanningRolePair` | "code-planning-pair" | A4 pipeline_ops.go |
| `plan_handoff_failure.go:19` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:24` | J11 | R4 | declaration of typed | `handoffOutputRefusal` | "output_validation" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:24` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:25` | J11 | R4 | declaration of typed | `handoffInputRefusal` | "selective_inheritance" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:25` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:26` | J11 | R4 | declaration of typed | `handoffProviderRefusal` | "provider_retirement" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:26` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:30` | J15 | R4 | declaration of named | `reservationTransferPolicyVersion` | 1 | A4 plan_handoff_failure.go (D-89 retirement admission policy version, fingerprint material) |
| `plan_handoff_failure.go:31` | J15 | R4 | declaration of named | `selectedChildLineagePolicyVersion` | 1 | A4 plan_handoff_failure.go (D-167 retirement admission policy version, fingerprint material) |
| `plan_handoff_failure.go:35` | J20 | R4 | declaration of named | `planHandoffFailedAlertCategory` | "PLAN HANDOFF FAILED" (4 uses) | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:41` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:65` | J11 | R4 | typed | `handoffRefusalClass` | three untyped string constants selecting fingerprint and retirement branches (`handoffInputError.class`); the exported `PlanHandoffFailure.Class` stays `string`, converted at the boundary | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:66` | J11 | R4 | typed | `handoffInputRefusal` | "selective_inheritance" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:66` | J11 | R4 | typed | `handoffOutputRefusal` | "output_validation" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:66` | J11 | R4 | typed | `handoffProviderRefusal` | "provider_retirement" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:90` | J11 | R4 | typed | `handoffInputRefusal` | "selective_inheritance" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:124` | J11 | R4 | typed | `handoffProviderRefusal` | "provider_retirement" | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:139` | J15 | R4 | named | `reservationTransferPolicyVersion` | 1 | A4 plan_handoff_failure.go (D-89 retirement admission policy version, fingerprint material) |
| `plan_handoff_failure.go:139` | J15 | R4 | named | `selectedChildLineagePolicyVersion` | 1 | A4 plan_handoff_failure.go (D-167 retirement admission policy version, fingerprint material) |
| `plan_handoff_failure.go:157` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `plan_handoff_failure.go:160` | J18 | R4 | named | `outputIndexExtraKey` | "output_index" | A4 transition_pass.go |
| `plan_handoff_failure.go:221` | J20 | R4 | named | `planHandoffFailedAlertCategory` | "PLAN HANDOFF FAILED" (4 uses) | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:222` | J20 | R4 | named | `planHandoffFailedAlertCategory` | "PLAN HANDOFF FAILED" (4 uses) | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:224` | J19 | R1 | adopted | `resumeOperation` | "resume" (command hint) | A3 mode_change.go |
| `plan_handoff_failure.go:234` | J20 | R4 | named | `planHandoffFailedAlertCategory` | "PLAN HANDOFF FAILED" (4 uses) | A4 plan_handoff_failure.go |
| `plan_handoff_failure.go:235` | J20 | R4 | named | `planHandoffFailedAlertCategory` | "PLAN HANDOFF FAILED" (4 uses) | A4 plan_handoff_failure.go |
| `plan_handoff_undecided.go:20` | J20 | R4 | declaration of named | `planDispositionMissingAlertCategory` | "PLAN DISPOSITION MISSING" (2 uses) | A4 plan_handoff_undecided.go |
| `plan_handoff_undecided.go:65` | J9 | R1/R2 | adopted | `humanNoteForAll` | "all" | A3 assess_blocked_fingerprint.go |
| `plan_handoff_undecided.go:175` | J20 | R4 | named | `planDispositionMissingAlertCategory` | "PLAN DISPOSITION MISSING" (2 uses) | A4 plan_handoff_undecided.go |
| `plan_handoff_undecided.go:176` | J20 | R4 | named | `planDispositionMissingAlertCategory` | "PLAN DISPOSITION MISSING" (2 uses) | A4 plan_handoff_undecided.go |
| `plan_handoff_undecided.go:179` | J10 | R4 | named | `addHumanNoteOperation` | "add-human-note" | A4 human_note.go |
| `plan_handoff_undecided.go:180` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `plan_replacement.go:245` | J19 | R1 | adopted | `reserveProviderOperation` | "reserve-provider" (command hint) | A4 reserve_provider.go |
| `plan_replacement.go:285` | J10 | R1 | adopted | `actorSystem` | "system" (`planReplacementActor` removed: same meaning as A3 `actorSystem`) | A3 helpers.go |
| `plan_replacement.go:395` | J10 | R1 | adopted | `actorSystem` | "system" (`planReplacementActor` removed: same meaning as A3 `actorSystem`) | A3 helpers.go |
| `proceed.go:19` | J13 | R4 | declaration of named | `proceedOperation` | "proceed" | A4 proceed.go |
| `proceed.go:222` | J13 | R4 | named | `proceedOperation` | "proceed" | A4 proceed.go |
| `proceed.go:285` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `proceed.go:286` | J2 | R1 | adopted | `models.CardinalityManyToOne` | "many-to-one" | A1 models |
| `proceed.go:607` | J17 | R1 | adopted | `pipeline.StateCategoryInitial` | "initial" | A1 pipeline |
| `proceed.go:609` | J17 | R1 | adopted | `pipeline.StateCategoryExecuting` | "executing" | A1 pipeline |
| `proceed.go:611` | J17 | R1 | adopted | `pipeline.StateCategorySubmitted` | "submitted" | A1 pipeline |
| `proceed.go:613` | J17 | R1 | adopted | `pipeline.StateCategoryReviewing` | "reviewing" | A1 pipeline |
| `proceed.go:615` | J17 | R1 | adopted | `pipeline.StateCategoryApproved` | "approved" | A1 pipeline |
| `proceed.go:617` | J17 | R1 | adopted | `pipeline.StateCategoryRejected` | "rejected" | A1 pipeline |
| `provider_dependencies.go:117` | J6 | R1 | adopted | `models.ReplannedTransition` | "replanned" | A1 models/replan_lineage.go |
| `provider_dependencies.go:203` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `provider_dependencies.go:257` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `provider_dependencies.go:289` | J19 | R1 | adopted | `cancelTaskOperation` | "cancel-task" (command hint) | A4 cancel_task.go (Scope 5) |
| `provider_dependencies.go:289` | J19 | R1 | adopted | `replaceTaskOperation` | "replace-task" (command hint) | A4 replace_task.go (Scope 5) |
| `repair_arch_ref.go:86` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `repair_arch_ref.go:110` | J10 | R1/R2 | adopted | `humanNoteSourceOperatorCLI` | "operator_cli" | A3 release_blocked_chain.go |
| `repair_arch_ref.go:124` | J10 | R1 | adopted | `actorOperator` | "operator" | A3 helpers.go |
| `repair_superseded_dependencies.go:178` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `replace_task_admission.go:20` | J18 | R4 | declaration of named | `replacementExtraSourceNewTransition` | "source_new_transition_id" | A4 replace_task_admission.go |
| `replace_task_admission.go:21` | J18 | R4 | declaration of named | `replacementExtraRetargetedConsumers` | "retargeted_consumers" | A4 replace_task_admission.go |
| `replace_task_admission.go:33` | J19 | R4 | declaration of named | `replacePreservedFieldBaseCommit` | "base_commit" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:34` | J19 | R4 | declaration of named | `replacePreservedFieldWorktree` | "worktree" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:143` | J18 | R4 | named | `replacementExtraSourceNewTransition` | "source_new_transition_id" | A4 replace_task_admission.go |
| `replace_task_admission.go:144` | J18 | R4 | named | `replacementExtraRetargetedConsumers` | "retargeted_consumers" | A4 replace_task_admission.go |
| `replace_task_admission.go:196` | J19 | R4 | named | `replacePreservedFieldBaseCommit` | "base_commit" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:200` | J19 | R4 | named | `replacePreservedFieldWorktree` | "worktree" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:205` | J19 | R4 | named | `replacePreservedFieldBaseCommit` | "base_commit" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:208` | J19 | R4 | named | `replacePreservedFieldWorktree` | "worktree" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:212` | J19 | R4 | named | `replacePreservedFieldWorktree` | "worktree" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:216` | J19 | R4 | named | `replacePreservedFieldWorktree` | "worktree" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_admission.go:220` | J19 | R4 | named | `replacePreservedFieldBaseCommit` | "base_commit" | A4 replace_task_admission.go (Scope 5 block) |
| `replace_task_candidate.go:95` | J18 | R4 | named | `replacementExtraRetargetedConsumers` | "retargeted_consumers" | A4 replace_task_admission.go |
| `replace_task_candidate.go:115` | J18 | R4 | named | `replacementExtraSourceNewTransition` | "source_new_transition_id" | A4 replace_task_admission.go |
| `reserve_provider.go:98` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `retarget_dependency.go:198` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `runtime_input_admission.go:259` (A3 conformance) | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_discovery_disposition.go:13` | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:16` | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:16` | J11 | R4 | declaration of typed | `discoveryDispositionDeferred` | "deferred" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:17` | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:17` | J11 | R4 | declaration of typed | `discoveryDispositionDismissed` | "dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:21` | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:22` | J11 | R4 | typed | `discoveryDispositionDeferred` | "deferred" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:22` | J11 | R4 | typed | `discoveryDispositionDismissed` | "dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:34` | J11 | R4 | typed | `discoveryDisposition` | map[string]bool of "deferred"/"dismissed" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:37` | J11 | R4 | typed | `discoveryDispositionDeferred` | "deferred" | A4 set_discovery_disposition.go |
| `set_discovery_disposition.go:37` | J11 | R4 | typed | `discoveryDispositionDismissed` | "dismissed" | A4 set_discovery_disposition.go |
| `set_task_output.go:63` | J1 | R1 | adopted | `LifecycleEffectsNone` | "none" | A3 lifecycle_result.go |
| `set_task_output.go:63` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:73` | J1 | R1 | adopted | `LifecycleEffectsNone` | "none" | A3 lifecycle_result.go |
| `set_task_output.go:73` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:84` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:103` | J1/J19 | R1 | declaration of adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:138` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:175` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output.go:274` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output_checks.go:73` | J1 | R1 | adopted | `LifecycleEffectsNone` | "none" | A3 lifecycle_result.go |
| `set_task_output_checks.go:73` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `set_task_output_checks.go:81` | J1 | R1 | adopted | `LifecycleEffectsNone` | "none" | A3 lifecycle_result.go |
| `set_task_output_checks.go:81` | J1/J19 | R1 | adopted | `setTaskOutputOperation` | "set-task-output" | A1 payloadschema.SetTaskOutputOperation |
| `sprint_checkpoint.go:16` | J16 | R4 | declaration of named | `sprintSummaryFileMode` | 0644 | A4 sprint_checkpoint.go |
| `sprint_checkpoint.go:212` | J16 | R4 | named | `sprintSummaryFileMode` | 0644 | A4 sprint_checkpoint.go |
| `sprint_summary.go:140` | J7 | R1/R2 | adopted | `circuitBreakerStatusTriggered` | "TRIGGERED" | A3 mode_change.go (I20) |
| `sprint_summary.go:154` | J19 | R1 | adopted | `resumeOperation` | "resume" (command hint) | A3 mode_change.go |
| `supersede_task.go:240` | J19 | R1 | adopted | `notFoundEntityTask` | "task" | A3 helpers.go |
| `transition_children.go:276` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `transition_children.go:297` | J18 | R4 | named | `outputIndexExtraKey` | "output_index" | A4 transition_pass.go |
| `transition_children.go:304` | J18 | R4 | named | `outputIndexExtraKey` | "output_index" | A4 transition_pass.go |
| `transition_pass.go:23` | J18 | R4 | declaration of named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `transition_pass.go:24` | J18 | R4 | declaration of named | `cycleMembersExtraKey` | "cycle_members" | A4 transition_pass.go |
| `transition_pass.go:25` | J18 | R4 | declaration of named | `outputIndexExtraKey` | "output_index" | A4 transition_pass.go |
| `transition_pass.go:31` | J14 | R4 | declaration of named | `baseCommitSnapshotRolePair` | "coding-pair" | A4 transition_pass.go |
| `transition_pass.go:273` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `transition_pass.go:274` | J18 | R4 | named | `cycleMembersExtraKey` | "cycle_members" | A4 transition_pass.go |
| `transition_pass.go:368` | J14 | R4 | named | `baseCommitSnapshotRolePair` | "coding-pair" | A4 transition_pass.go |
| `transition_pass.go:595` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `transition_pass.go:596` | J18 | R4 | named | `cycleMembersExtraKey` | "cycle_members" | A4 transition_pass.go |
| `transition_recovery.go:52` | J18 | R4 | named | `transitionExtraKey` | "transition" | A4 transition_pass.go |
| `update_sprint_metrics.go:16` | J15 | R4 | declaration of named | `suspiciousRateMinCount` | 3 | A4 update_sprint_metrics.go |
| `update_sprint_metrics.go:17` | J15 | R4 | declaration of named | `suspiciousRatePercent` | 95 | A4 update_sprint_metrics.go |
| `update_sprint_metrics.go:76` | J15 | R4 | named | `suspiciousRateMinCount` | 3 | A4 update_sprint_metrics.go |
| `update_sprint_metrics.go:76` | J15 | R4 | named | `suspiciousRatePercent` | 95 | A4 update_sprint_metrics.go |
| `update_sprint_metrics.go:86` | J15 | R4 | named | `suspiciousRateMinCount` | 3 | A4 update_sprint_metrics.go |
| `update_sprint_metrics.go:86` | J15 | R4 | named | `suspiciousRatePercent` | 95 | A4 update_sprint_metrics.go |

## 3. Classified literals (R5 / J22)

Each row lists every line of one file carrying literals of one class. The class reasons: J18 detail keys are persisted audit/history/anomaly field names (and fingerprint hash material, whose spelling is the fingerprint) that no A4 decision reads back by spelling from persisted state (the keys A4 does read back are named in table 2; readers in other areas are in table 7); J20 is message, format, report, label or rendered-command text; J21 is an arithmetic or emptiness identity; J22 dynamic identities (role-pair, transition, role and provider names, task/agent/discovery IDs, user text) flow in as variables and never as literals — none appears in this table by construction.

| File | J | Rule | Outcome | Reason | Lines |
|---|---|---|---|---|---|
| `add_tasks.go` | J20 | R5 | classified | activity-log action label: log lines are a free surface (goal-frozen), not a decision value | 173 |
| `add_tasks.go` | J20 | R5 | classified | literal argument: label or format selector without control-flow vocabulary | 205 |
| `add_tasks.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 74,99,110,131,147,166,179,188,191,194,203,215,218,221,222,225,229,232,254,255,261,262,271,285,320,328,334,349,375,386,400 |
| `add_tasks.go` | J20 | R5 | classified | reference field name passed to the validator for its diagnostic | 196,199 |
| `add_tasks.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 125,126,144,217,224 |
| `add_tasks.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 73,190,193,202,214,220,248,252,267,278,392 |
| `add_tasks_delivery.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 51,58,65,166,167,167,382,382,421,434,436 |
| `add_tasks_delivery.go` | J20 | R5 | classified | rendered operator command name in guidance text (command identities are not closed into a type, A3 I22) | 167 |
| `add_tasks_delivery.go` | J21 | R5 | classified | a unit below two members needs no ordering (arity identity) | 326 |
| `add_tasks_delivery.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 50,62,64,64,71,85,86,90,158,200,272,281,290,291,292,303,317,349,356,357,366 |
| `add_tasks_delivery.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 57,418,433 |
| `advance_sprint.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 57,60,64,79,95,106,110,113,118,156,160,164,176,181,184,184,206 |
| `advance_sprint.go` | J20 | R5 | classified | rendered operator command name in guidance text (command identities are not closed into a type, A3 I22) | 184 |
| `advance_sprint.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 183,198,199,205,243,246,349 |
| `advance_sprint.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 240,380,429 |
| `amend_plan.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 66,88,91,94,97,108,111,114,117,147,162,169,175,178,186,191,200,204,213,217,225,233,253,256,259,261,264 |
| `amend_plan.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 81,87,107,110,113,151,152,152,190,203,212 |
| `amend_plan.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 53,55,78,83,87,87,88,90,91,93,93,93,94,96,96,97,103,107,110,113,173,216,220,222,235,246 |
| `analyze.go` | J20 | R5 | classified | literal argument: label or format selector without control-flow vocabulary | 153 |
| `analyze.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 86,138,148,154,308,311,317 |
| `analyze.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 72,72,72,72,222,241,244,245,248,278,302,302 |
| `analyze.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 123,168,208,310 |
| `analyze.go` | J21 | R5 | classified | median of an ordered sample: halving and parity identity | 243,244,245,245 |
| `apply_dependency_repair.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 310,311,312,313 |
| `apply_dependency_repair.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 132,135,138,150,208,211,238,242,249,249,252,269,272,275,279,282,308 |
| `apply_dependency_repair.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 103,104,240,265,305 |
| `apply_dependency_repair.go` | J21 | R5 | classified | delimiter or cutset of a string operation | 242 |
| `apply_dependency_repair.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 131,134,137 |
| `archive_terminal_tasks.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 19,53 |
| `archive_terminal_tasks.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 52,52,56,57,94,143 |
| `cancel_task.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 80,83,86,106,118,156,159,218 |
| `cancel_task.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 79,82,85 |
| `contract_amendment.go` | J20 | R5 | classified | literal argument: label or format selector without control-flow vocabulary | 55 |
| `contract_amendment.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 19,26,43,43,49,62 |
| `contract_amendment.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 58,61,68 |
| `contract_amendment.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 15,47,49,51,58,62 |
| `contract_amendment_compare.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 13,72,75,93,112,120 |
| `contract_amendment_compare.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 22,47 |
| `defer_provider_dependency.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 39,39,98,99,100,101,102,103 |
| `defer_provider_dependency.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 39,39,41,46,59,65,69,80,91,110,118,120 |
| `defer_provider_dependency.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 64,84,86 |
| `defer_provider_dependency.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 40 |
| `delete_task.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 28,31,34,73,92,96,129,159 |
| `delete_task.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 91,95,96 |
| `delete_task.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 72,128 |
| `delete_task_removal.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 23,26,78,97 |
| `delete_task_removal.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 22,23,25,26,37,56 |
| `dependency_direction.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 37,38,40 |
| `dependency_direction.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 36 |
| `dependency_direction.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 13,21 |
| `dependency_repair_request.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 40,41,42,43,44,45,52,67 |
| `dependency_repair_request.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 28,68 |
| `dependency_rewrite.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 57,58,59,60,417,418,419,420 |
| `dependency_rewrite.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 50,128,148,182,210,247,259,268,323,332,415,415 |
| `dependency_rewrite.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 75,76,80,108,122,174,177,198,202,203,205,261,292,296,325,351,411 |
| `dependency_rewrite.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 71,86,348 |
| `descendant_dependencies.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 52,55,59,65,82,84,94 |
| `descendant_dependencies.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 29,50,63,66,66,66 |
| `descendant_dependencies.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 58 |
| `direct_coding_allocation.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 25,30,63,74,86,89,96,105,115 |
| `direct_coding_allocation.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 24 |
| `direct_coding_allocation.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 88 |
| `human_note.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 49,50 |
| `human_note.go` | J20 | R5 | classified | activity-log action label: log lines are a free surface (goal-frozen), not a decision value | 71 |
| `human_note.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 34,37,40,43,54,65,72,78 |
| `human_note.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 33,36 |
| `lifecycle_deferred_retirement.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 41,47,114 |
| `lifecycle_deferred_retirement.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 105 |
| `lifecycle_deferred_retirement.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 48 |
| `narrow_inherited_dependencies.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 72,76,79,130,133,136,139,174,222,270,273,294,299,299,307,310,314 |
| `narrow_inherited_dependencies.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 78,138,147,148,293,295,296,306,335,340 |
| `narrow_inherited_dependencies.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 129,132,135,278,294,299 |
| `narrow_inherited_edges.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 272,273,274,275,276,277,278,279,295,296,297,298 |
| `narrow_inherited_edges.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 59,62,65,68,72,75,86,130,228,248,264,286,310,314,317,321 |
| `narrow_inherited_edges.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 67,164,164,220 |
| `narrow_inherited_edges.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 59,62,65,68,72,75,133,323 |
| `obligation_drift.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 185,253 |
| `obligation_drift.go` | J21 | R5 | classified | [2]string (carrier, commit) key: array length | 324,332 |
| `obligation_drift.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 141,234,338,338,361,430,444 |
| `obligation_drift.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 113,259,329,329 |
| `obligation_drift_carrier.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 51,71,81,84,103 |
| `obligation_drift_carrier.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 82,151 |
| `orchestrator_wake.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 240,243 |
| `orchestrator_wake.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 205,205,249,249,266,266,283,300,328,340,355,356 |
| `pipeline_ops.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 188,190,193 |
| `pipeline_ops.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 78,103,110,115,156,160,163,167,196,235,246,265,404,432 |
| `pipeline_ops.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 189 |
| `pipeline_ops.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 192 |
| `plan_amendment_begin.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 34,52,70,82,85,90,94,129,139,141,143,157 |
| `plan_amendment_begin.go` | J20 | R5 | classified | rendered operator command name in guidance text (command identities are not closed into a type, A3 I22) | 139,139,141,141 |
| `plan_amendment_begin.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 128 |
| `plan_amendment_begin.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 62,66,70,80,84,113,138,163 |
| `plan_check.go` | J20 | R5 | classified | activity-log action label: log lines are a free surface (goal-frozen), not a decision value | 175 |
| `plan_check.go` | J20 | R5 | classified | display or note label | 271 |
| `plan_check.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 74,105,117,122,125,133,177,180,202,207,219,224,227,230,235,238,241,244,248,264 |
| `plan_check.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 193,218 |
| `plan_check.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 117,117,122,122,124,125,125,128,128,133,133,219,223,224,227,229,230,235,237,238,240,241,243,244,248,270 |
| `plan_check_decision.go` | J20 | R5 | classified | display or note label | 242 |
| `plan_check_decision.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 40,53,77,80,109,113,116,120,124,130,141,143,151,159,166,170,182,184,186,188,197,202,230,233,238,252,256,263,275,281,281 |
| `plan_check_decision.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 183,278 |
| `plan_check_decision.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 31,115,185,204,236,260,279 |
| `plan_handoff.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 203,208,272,276,278,302,304,306,343,351,362,365 |
| `plan_handoff.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 94,94 |
| `plan_handoff.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 94,201,201,211,214,233,239,242,253,253,277,282,292,294,296,299,331 |
| `plan_handoff_failure.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 112,113,114,119,119,119,120,120,120,121,121,121,122,122,134,135,135,135,135,139,139 |
| `plan_handoff_failure.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 222,222,222,223,235,235,235,236 |
| `plan_handoff_failure.go` | J20 | R5 | classified | rendered operator command name in guidance text (command identities are not closed into a type, A3 I22) | 224,224,224 |
| `plan_handoff_failure.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 62,85,155,155,195 |
| `plan_handoff_failure.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 63,67,71,75,79,93,93,99,128,130,144,168,171,206 |
| `plan_handoff_undecided.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 70,70,70,71,71,71,72,72,75,76 |
| `plan_handoff_undecided.go` | J20 | R5 | classified | display or note label | 164 |
| `plan_handoff_undecided.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 172,176,176,177 |
| `plan_handoff_undecided.go` | J20 | R5 | classified | rendered operator command name in guidance text (command identities are not closed into a type, A3 I22) | 179,179 |
| `plan_handoff_undecided.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 54,57,91,91 |
| `plan_handoff_undecided.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 41,45,49,80,105,142,171 |
| `plan_replacement.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 398,398,398,399,399,399,400,430 |
| `plan_replacement.go` | J20 | R5 | classified | display or note label | 438 |
| `plan_replacement.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 80,170,244,245,283,284,286,341,345,348,354,364,394,452 |
| `plan_replacement.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 64,90,141,181,228,234,243,308,311,348,357,358,358,368,417,424,424 |
| `plan_replacement.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 48,64,76,86,103,225,239 |
| `planning_output_acceptance.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 82,83,86,87 |
| `planning_output_acceptance.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 26,29,52,54,75,80,87,104,144,144,149,158,162,176,183,191,200,200,202,244 |
| `planning_output_acceptance.go` | J20 | R5 | classified | operational-error phase or code label rendered to the caller; classified by A7 (jsonout), not closed here | 78,79 |
| `planning_output_acceptance.go` | J20 | R5 | classified | reference field name: wire/JSON field of the task output, rendered in diagnostics (J20) | 35,36,37,38,41 |
| `planning_output_acceptance.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 146,223,223 |
| `planning_output_acceptance.go` | J21 | R5 | classified | delimiter or cutset of a string operation | 139 |
| `planning_output_acceptance.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 25,28,98,136,204 |
| `predecessor_cleanup.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 24,43,61,66 |
| `proceed.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 286,287,288,289 |
| `proceed.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 24,28,97,116,119,151,158,161,166,189,194,197,224,249,255,315,320,328,348,351,355,358,368,371,405,419,421,471,481,492,508,512,516,520,567,573,573,579,599,603,620 |
| `proceed.go` | J21 | R5 | classified | 2-part phase reference arity and component index | 593,597 |
| `proceed.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 289,350,480,594,594,597 |
| `proceed.go` | J21 | R5 | classified | delimiter or cutset of a string operation | 590 |
| `proceed.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 221,418,599,602,602,603,620 |
| `proceed.go` | J21 | R5 | classified | switch on the split arity of a phase reference (2- or 3-part); grammar, not a threshold | 595 |
| `provider_dependencies.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 220,220,301 |
| `provider_dependencies.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 83,86,93,121,127,130,139,141,141,170,172,201,202,214,214,234,247,253,257,280,287,288,343,345 |
| `provider_dependencies.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 136,138,139,211,275,284 |
| `provider_dependencies.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 80,88,92,114,233,238,244,256,316,320,327 |
| `repair_arch_ref.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 109,110,111,112 |
| `repair_arch_ref.go` | J20 | R5 | classified | activity-log action label: log lines are a free surface (goal-frozen), not a decision value | 125 |
| `repair_arch_ref.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 42,45,49,52,59,63,68,76,82,89,96,99,102,118,127,129,141,144,157,159 |
| `repair_arch_ref.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 41,44,48,51,62,88 |
| `repair_superseded_dependencies.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 224,225,226,227 |
| `repair_superseded_dependencies.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 118,121,124,136,195,208,216,216,240,248,255 |
| `repair_superseded_dependencies.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 89,90,207 |
| `repair_superseded_dependencies.go` | J21 | R5 | classified | delimiter or cutset of a string operation | 249,250 |
| `repair_superseded_dependencies.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 117,120,123 |
| `replace_task.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 119,171,176,178 |
| `replace_task_admission.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 45,45,116,122,153,163,166,172,180,193,193,196,200,203,205,208,212,216,220 |
| `replace_task_admission.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 45,56,57,105,105,115,115,124,153,165,193 |
| `replace_task_admission.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 40,40,42,162,195 |
| `replace_task_candidate.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 77,95,95,95,97 |
| `replace_task_candidate.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 30,33,45 |
| `replace_task_candidate.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 134 |
| `replace_task_candidate.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 42 |
| `replan.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 336,337,338,441,442,443,444,445 |
| `replan.go` | J20 | R5 | classified | activity-log action label: log lines are a free surface (goal-frozen), not a decision value | 481 |
| `replan.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 53,61,66,84,102,110,154,176,191,194,198,202,206,210,216,234,246,278,280,329,341,369,433,449,450,467,483,486,502,518,527,527,537,544,591,598 |
| `replan.go` | J21 | R5 | classified | [2]string edge pair: array length | 560,566 |
| `replan.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 200,204,329,329,337,338,517,519,520,569,578,592,598 |
| `replan.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 52,109,232,246,246,279,363,499,534,541,542 |
| `reserve_provider.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 252,253,253,253,254,270,271,271,272 |
| `reserve_provider.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 73,77,83,87,106,132,137,143,145,180,184,201,204,207,237,243,246,248,262,294,311 |
| `reserve_provider.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 82,82,102,108,118,171,242,310,313 |
| `reserve_provider.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 72,218,311 |
| `retarget_dependency.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 276,277,278,279,280,281 |
| `retarget_dependency.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 135,138,141,144,161,251,258,261,274,289,295,324,327,334 |
| `retarget_dependency.go` | J20 | R5 | classified | operational-error phase or code label rendered to the caller; classified by A7 (jsonout), not closed here | 272 |
| `retarget_dependency.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 105,106,333 |
| `retarget_dependency.go` | J21 | R5 | classified | delimiter or cutset of a string operation | 258,294,321 |
| `retarget_dependency.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 134,137,140,143,304,323 |
| `retarget_dependency_rewrite.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 65,66,67,68,69,70,73,182,183,184,185,186 |
| `retarget_dependency_rewrite.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 19,24,28,31,46,78,78,99,102,106,109,112,128,130,139 |
| `retarget_dependency_rewrite.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 45,98,101,127,159,159,192,196 |
| `retarget_dependency_rewrite.go` | J21 | R5 | classified | token window of a recorded repair command (index offset of operation, task, dependency) | 192,196 |
| `set_discovery_disposition.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 29,32,37,47,56 |
| `set_discovery_disposition.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 28,31 |
| `set_task_output.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 175,175,176,176,177,191,212,212 |
| `set_task_output.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 63,136,177,193,207,223,227,230,248,275,275,304,307,338,396,408,411,414,417,430,442,468,502,503,514,515,546,548,550,552,554,558,561 |
| `set_task_output.go` | J20 | R5 | classified | reference field name: wire/JSON field of the task output, rendered in diagnostics (J20) | 243,244,245,246,375,377,380,382,385,387,390,392 |
| `set_task_output.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 264,270,441,500,524 |
| `set_task_output.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 77,205,229,303,407,464,527,540,555 |
| `set_task_output_checks.go` | J20 | R5 | classified | display or note label | 77 |
| `set_task_output_checks.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 16,19,33,58,73,81,97,113,133,136,154,159 |
| `set_task_output_checks.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 96 |
| `set_task_output_checks.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 15,18,31,153,156 |
| `sprint_checkpoint.go` | J20 | R5 | classified | literal argument: label or format selector without control-flow vocabulary | 202 |
| `sprint_checkpoint.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 19,30,96,104,119,126,128,181,204,210,215,219,239 |
| `sprint_checkpoint.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 54,157 |
| `sprint_checkpoint.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 51,142,149,160,204,210,215,219 |
| `sprint_summary.go` | J20 | R5 | classified | display or note label | 109 |
| `sprint_summary.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 30,31,35,36,37,38,39,42,46,48,51,61,62,63,64,65,72,73,74,75,76,77,78,79,80,85,86,87,88,89,90,91,92,93,94,95,100,102,106,107,113,115,120,121,128,129,131,133,138,139,141,142,144,149,150,151,152,153,154,155,167,171,174 |
| `sprint_summary.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 19,45,66,101,170 |
| `sprint_summary.go` | J21 | R5 | classified | unit-conversion identity (minutes, hours per day, percent) | 162,164,165,166 |
| `supersede_task.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 372,413,414,415,416,417,418,419,422,424,427,434,440,449,459,460 |
| `supersede_task.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 115,118,121,126,128,129,133,156,170,212,254,290,354,354,356,394,395,396,396,404,432,438,447,453,457 |
| `supersede_task.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 124,144,297,318,353,368 |
| `supersede_task.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 114,117,120,125,132,223,262,398,399 |
| `task_archive.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 40,61,92,95,100,105,111,114,118,120,122,124,139,143 |
| `task_archive.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 92 |
| `task_retirement_cleanup.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 16,22 |
| `transition_children.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 277,280,298,299,300 |
| `transition_children.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 17,22,29,102,169,195,210,237,240,384,393,394,405,408,411,414,417,419,423,426 |
| `transition_children.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 101,209,279,294,347 |
| `transition_children.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 194,238,324,329,337,376,404,407,410,413 |
| `transition_cohort.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 20,28,74,77,84,133,140,141,155,161,168,215 |
| `transition_cohort.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 27,76,107,135 |
| `transition_cohort.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 19,20,25,28,49,103,112 |
| `transition_inherited_deps.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 50,51,57,81,140,152 |
| `transition_inherited_deps.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 178 |
| `transition_pass.go` | J20 | R5 | classified | alert kind label of a warning | 396,399 |
| `transition_pass.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 148,193,206,221,241,278,283,317,323,392,408,409 |
| `transition_pass.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 219,234,356,368,428,459,467,468,469,473,486,490,496,558,559,611 |
| `transition_pass.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 70,132,148,153,205,363 |
| `transition_pass.go` | J21 | R5 | classified | strongly connected component below two members is acyclic unless it self-loops (graph identity) | 496 |
| `transition_recovery.go` | J18 | R5 | classified | detail, history or hash-material key: persisted field name, written here; no A4 decision reads it back by spelling from state (in-function map assembly stays here; readers in other areas: table 7) | 53,261,262 |
| `transition_recovery.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 33,43,88,170,183,230,247 |
| `transition_recovery.go` | J21 | R5 | classified | arity, zero or one test or index: arithmetic identity | 83,166,166,167,167,169,169,238,260,283 |
| `transition_recovery.go` | J21 | R5 | classified | empty-string test: absence of an optional value | 295 |
| `update_sprint_metrics.go` | J20 | R5 | classified | message, format or report text (single-use descriptive label, no control flow) | 38,54,66,78,90 |
| `update_sprint_metrics.go` | J21 | R5 | classified | unit-conversion identity (minutes, hours per day, percent) | 88 |

### 3b. Pre-existing named owner constants in A4 files (J19)

Named by earlier scopes or by A3 inside the A4 files; retained, adopted nowhere twice, no parallel definition.

| File | Constants |
|---|---|
| `archive_terminal_tasks.go` | `archiveTerminalTasksLockOperation`, `restoreTerminalTasksLockOperation` |
| `defer_provider_dependency.go` | `deferProviderDependencyOperation` |
| `dependency_rewrite.go` | `prerequisiteConsumerNotExecuting` |
| `narrow_inherited_dependencies.go` | `narrowInheritedDependenciesOperation`, `narrowActionNarrowed`, `narrowActionUnchanged`, `narrowActionSkipped` |
| `obligation_drift.go` | `obligationDriftRepinned`, `obligationDriftRetargeted`, `obligationDriftDropped`, `obligationDriftStale` |
| `plan_amendment_begin.go` | `amendCorrectionIDSuffix`, `replanCorrectionIDSuffix` |
| `plan_check.go` | `PlanCheckActionPass`, `PlanCheckActionHold`, `PlanCheckActionClear`, `PlanCheckActionReplace`, `PlanCheckActionRetire` |
| `plan_handoff.go` | `PlanHandoffNotSource`, `PlanHandoffOutOfDomain`, `PlanHandoffNeedsReview`, `PlanHandoffPassed`, `PlanHandoffWaitingUpstream`, `PlanHandoffNeedsReconciliation`, `PlanHandoffHeld`, `PlanHandoffAmendmentReady` |
| `plan_handoff_failure.go` | `handoffFailureVersion` |
| `plan_handoff_undecided.go` | `handoffUndecidedVersion` |
| `repair_arch_ref.go` | `repairArchRefOperation` |
| `repair_superseded_dependencies.go` | `repairSupersededDependenciesOperation` |
| `replace_task_admission.go` | `replaceFieldRequestID`, `replaceFieldExpectedTransition`, `replaceFieldReplacementID` |
| `replan.go` | `replanOperation` |
| `reserve_provider.go` | `reservationChangeAdded`, `reservationChangeReleased`, `reservationChangeAddedWithCap`, `reservationChangeCapTightened` |
| `retarget_dependency.go` | `retargetDependencyOperation`, `retargetDependencyRejectedAction`, `retargetDependencyCandidateValidation`, `retargetDependencyRejectedDetailMaxBytes` |
| `set_task_output.go` | `setTaskOutputPhaseValidate`, `setTaskOutputPhaseLoadPipeline`, `setTaskOutputPhasePersist` |
| `supersede_task.go` | `supersedeTaskOperation` |
| `task_archive.go` | `archiveObjectFormatVersion` |
| `transition_pass.go` | `defaultIntegrationBranch` |

## 4. False friends: same spelling, different meaning

| Occurrence | Looks like | Why it is not that vocabulary |
|---|---|---|
| `"manual"` history keys (`repair_superseded_dependencies.go`, `dependency_repair_request.go`, `narrow_inherited_edges.go`, `retarget_dependency_rewrite.go`) | `models.TransitionTriggerManual` | a marker key on a manual dependency-rewrite history entry, not a transition trigger |
| `"integration"` key in `repair_arch_ref.go` details | `models.TaskTypeIntegration`, `defaultIntegrationBranch` | detail key naming the integration ref of the repair evidence |
| `Action: "plan_check"`, `"arch_ref_repaired"` log actions | `models.TaskEventPlanCheck`, `models.TaskEventArchRefRepaired` | activity-log action labels; log lines are a free surface (goal-frozen), kept apart from history event names |
| `planCheckVerdictLabel` returning `"none"` | `models.InheritModeNone`, `LifecycleEffectsNone` | display label of an absent verdict |
| `currentAgent := "none"` in `set_task_output_checks.go` | `LifecycleEffectsNone` | message text for an unassigned task |
| `rolePairOf` returning `"missing"` | `assessmentOutcomeMissing` | diagnostic placeholder for a nil task, not an assessment outcome |
| `note: "passed"` in `plan_check_decision.go` | `PlanHandoffPassed`, `models.PlanCheckPassed` | history note text; asserted by the existing white-box test |
| `key "request_id"` in `replace_task_candidate.go` audit extra | `replaceFieldRequestID` | history audit key; the constant is a diagnostic field label of a refusal |
| `"replanned"` key in `plan_handoff_failure.go` hash material | `models.ReplannedTransition` | fingerprint material key; its spelling is part of the stored fingerprint |
| `> 4096` reason limit in `amend_plan.go` | `maxIntegrationDiagnosticOutputExcerptBytes` | unrelated 4096-byte bound |
| `>= 3` in `update_sprint_metrics.go` | `maxBlockedQuestions`, `AcceptanceAllocationRefusalThreshold`, `maxMergeRetries` | unrelated counts of the same value |
| `"provider_retirement"` detail keys in `provider_dependencies.go`, `plan_handoff_failure.go` | `handoffProviderRefusal` | detail/hash keys; the constant is a refusal class |

## 5. Owner gaps (R3) and owner findings

| Value | Owner | Finding | A4 treatment |
|---|---|---|---|
| circuit-breaker status `OK` / `TRIGGERED` (persisted `models.CircuitBreaker.Status`, history `Result`) | `models` declares none | A3 I20 already named `circuitBreakerStatusOK`/`Triggered` in `mode_change.go` (R2: A4 follows the first owner) | adopted in `analyze.go`, `sprint_summary.go`; no `models` edit (X1) |
| pattern `provider_audit_degradation` | `analysis/patterns.go` repeats the literal at 383 and 416 and names none | owner gap | `providerAuditDegradationPattern` in `analyze.go`; the C2 provider-audit grouping in `analysis` and `pipelineBundle` are untouched |
| history Extra keys `blocked_class`, `gated_at`, `first_rejection_at`, `causes`, `recovery_path` | written by A5 `rejection_rca.go`, `submit_verdict.go`; `models` names the struct fields but no history key | owner gap | `rejectionRCA*Key` in `analyze.go` (reader); writers listed in table 7 for A5 |
| `add-human-note` operation value read by `internal/agent/prompt_corrective_context.go` | none | owner gap | `addHumanNoteOperation` in `human_note.go`; the A8 reader listed in table 7 |
| operation `add-task` | `payloadschema` registers the literal at `add_task.go:42` without a constant | owner gap | `addTaskOperation` in `add_tasks.go`; spelling pinned by `TestA4VocabularySpelling` |
| `set-task-output` | `payloadschema.SetTaskOutputOperation` (A1) | delivered | `setTaskOutputOperation` is derived from it (one source) |
| transition-keyed history keys `transition`, `cycle_members`, `output_index`; replacement keys; obligation-drift keys | none | owner gap, read back by A4 itself | named in their concerns (`transition_pass.go`, `replace_task_admission.go`, `obligation_drift.go`) |
| alert categories `PLAN HANDOFF FAILED`, `PLAN DISPOSITION MISSING` | `alerts` leaves `Category` a free string | owner gap | named in `plan_handoff_failure.go`, `plan_handoff_undecided.go` (category is also the once-key prefix) |
| transitions-executed marker `replanned` | `models.ReplannedTransition` **is delivered** | scope-4 plan assumed no constant; the owner had one | residual `provider_dependencies.go` literal adopted |
| `integration_state_changed` / `integration_incomplete` | A3 named the first in `mode_change.go`; A4 owns the completion-authorization concern | R4 owner is A4 | both declared in `pipeline_ops.go`; the A3 declaration is removed and both A3 sites keep the identifier |

No V-set symbol A1 prospectively promised is missing: `models.TransitionCardinality`, `TransitionTrigger`, `AllocationRoute`, `roles.RoleType` and `pipeline.StateCategory` are delivered and used; no BLOCKED finding against A1 closure.

## 6. A3 and cross-area conformance sites edited by this task

| File | Site | Change |
|---|---|---|
| `mode_change.go` | const block (removed `integrationStateChangedCode`); sites that raise it unchanged | declaration moves to the A4 completion-authorization concern (`pipeline_ops.go`) with `integrationIncompleteCode`; both mode_change.go sites now use the A4 constant (substitution only, spelling unchanged) |
| `runtime_input_admission.go` | `runtimeInputAdmissionError(setTaskOutputOperation, …)` | A3 literal `"set-task-output"` replaced by the A4 constant (R4 same-meaning conformance) |
| `deferred_validation.go` (A6) | `deferredFixInFlight` | `validDispositionSentinels[...]` → `discoveryDisposition(...).isSentinel()` (X2: caller of the retyped A4 set; no logic change) |

Unchanged A3 sites with the same value, listed not edited: `lifecycle_metrics.go` operation-name list (diagnostic-counter keys, A3 I-row owner classification).

## 7. Later-area adoption sites (X7: adopted when those areas are refactored)

| Area | Site | Literal | A4 name |
|---|---|---|---|
| A8 | `internal/agent/prompt_corrective_context.go:54` | `operator_cli` | humanNoteSourceOperatorCLI / addHumanNoteOperation |
| A7 | `internal/commands/status.go:453` | `add-human-note` | addHumanNoteOperation |
| A8 | `internal/agent/prompt_corrective_context.go:39` | `all` | humanNoteForAll |
| A9 | `internal/agent/strategy_orchestrator.go:303` | `all` | humanNoteForAll |
| A7 | `internal/commands/agent_health.go:30` | `operator` | actorOperator |
| A6 | `internal/ops/deferred_validation_ruling.go:48` | `operator` | actorOperator |
| A7 | `internal/tui/commands.go:263` | `operator` | actorOperator |
| A7 | `internal/tui/commands.go:278` | `operator` | actorOperator |
| A7 | `internal/tui/commands.go:349` | `operator` | actorOperator |
| A9 | `internal/agent/planning_complete_verify.go:47` | `replanned` | models.ReplannedTransition |
| A7 | `internal/commands/inspect_tasks.go:585` | `replanned` | models.ReplannedTransition |
| A7 | `internal/commands/inspect_tasks.go:594` | `replanned` | models.ReplannedTransition |
| A6 | `internal/ops/integration_progress.go:1079` | `replanned` | models.ReplannedTransition |
| A6 | `internal/ops/integration_progress.go:364` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A6 | `internal/ops/integration_progress.go:423` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A6 | `internal/ops/integration_progress.go:724` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A6 | `internal/ops/integration_progress.go:950` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/pipeline/resolver_sliced.go:23` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/pipeline/resolver_sliced.go:24` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A8 | `internal/prompts/templates/wake_initial_planning.tmpl:171` | `code-planning-pair` | legacyPlanningRolePair (configured role-pair identity, dynamic) |
| A6 | `internal/ops/integration_progress.go:482` | `coding-pair` | baseCommitSnapshotRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/pipeline/resolver_sliced.go:40` | `coding-pair` | baseCommitSnapshotRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/pipeline/resolver_sliced.go:62` | `coding-pair` | baseCommitSnapshotRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/pipeline/resolver_sliced.go:73` | `coding-pair` | baseCommitSnapshotRolePair (configured role-pair identity, dynamic) |
| A1 | `internal/analysis/patterns.go:383` | `provider_audit_degradation` | providerAuditDegradationPattern |
| A1 | `internal/analysis/patterns.go:416` | `provider_audit_degradation` | providerAuditDegradationPattern |
| A1 | `internal/analysis/patterns.go:144` | `TRIGGERED` | circuitBreakerStatusTriggered |
| A1 | `internal/analysis/patterns.go:163` | `TRIGGERED` | circuitBreakerStatusTriggered |
| A7 | `internal/commands/watch.go:902` | `TRIGGERED` | circuitBreakerStatusTriggered |
| A1 | `internal/models/history.go:484` | `TRIGGERED` | circuitBreakerStatusTriggered |
| A5 | `internal/ops/rejection_rca.go:118` | `gated_at` | rejectionRCA*Key |
| A5 | `internal/ops/rejection_rca.go:177` | `gated_at` | rejectionRCA*Key |
| A5 | `internal/ops/submit_verdict.go:637` | `gated_at` | rejectionRCA*Key |
| A5 | `internal/ops/submit_verdict.go:634` | `blocked_class` | rejectionRCA*Key |
| A5 | `internal/ops/submit_verdict.go:638` | `first_rejection_at` | rejectionRCA*Key |
| A7 | `cmd/liza/cmd_task.go:1580` | `set-task-output` | setTaskOutputOperation |
| A7 | `cmd/liza/cmd_task.go:1617` | `set-task-output` | setTaskOutputOperation |
| A7 | `cmd/liza/cmd_validate_payload.go:39` | `set-task-output` | setTaskOutputOperation |
| A9 | `internal/agent/supervisor_runtime_failure.go:139` | `set-task-output` | setTaskOutputOperation |
| A7 | `internal/commands/validate_payload.go:37` | `set-task-output` | setTaskOutputOperation |
| A1 | `internal/models/lifecycle.go:202` | `set-task-output` | setTaskOutputOperation |
| A3 | `internal/ops/lifecycle_metrics.go:44` | `set-task-output` | setTaskOutputOperation |
| A7 | `cmd/liza/cmd_task.go:89` | `add-task` | addTaskOperation |
| A1 | `internal/payloadschema/add_task.go:42` | `add-task` | addTaskOperation |
| A7 | `internal/commands/inspect_tasks.go:434` | `repair_source_task` | repair-audit detail keys (J18 R5; written by A4) |
| A7 | `internal/commands/inspect_tasks.go:438` | `repair_request_cleared` | repair-audit detail keys (J18 R5; written by A4) |
| A1 | `internal/usage/report.go:404` | `rewrote_depends_on` | repair-audit detail keys (J18 R5; written by A4) |
| A1 | `internal/usage/report.go:407` | `expected_dependencies` | repair-audit detail keys (J18 R5; written by A4) |
| A7 | `cmd/liza/cmd_task.go:1777` | `dismissed` | discoveryDisposition sentinels |

## 8. Sibling K7 commitments (`scope2-k7` … `scope5-k7`)

| Commitment | Evidence at this HEAD | Result |
|---|---|---|
| scope2-k7 / scope3-k7: consume A1/A3 vocabulary, never redefine a set; convert typed values to `string` at `any`/history/anomaly sinks | `grep` of defined string types in the 60 files finds only ops-owned sets (`PlanCheckAction`, `TransitionAdmission`, `obligationDriftChange`, `narrowAction`, `PlanHandoffClass`, `upstreamKind`, `reservationChange`, the new `discoveryDisposition` and the new `handoffRefusalClass`); no V1–V12 type is re-declared; typed cardinality enters history through `string(models.CardinalityManyToOne)` (`proceed.go`, `transition_recovery.go`) | delivered |
| scope2-k7 / scope3-k7: undelivered owner constants leave the literal for Scope 6 | the literals this left (`replanned` marker, operator provenance, `all`, circuit-breaker status, completion codes) are resolved in tables 2 and 5 | reconciled |
| scope4-k7: adopt delivered A3 safe-action/effects and A1 cardinality constants in touched functions | one residual raw pair in `apply_dependency_repair.go` (`"requery"`, `"none"` into `WrapLifecycleError`) and one raw `"many-to-one"` in `pipeline_ops.go` remained; both adopted here | residuals fixed (table 2) |
| scope4-k7: `replanned` marker and operator provenance have no delivered constant | `models.ReplannedTransition`, A3 `humanNoteSourceOperatorCLI`/`actorOperator` exist | adopted (table 2) |
| scope4-k7 / J19: `consumer_not_executing` and the reservation-change type | `dependency_rewrite.go` declares `prerequisiteConsumerNotExecuting`; `reserve_provider.go` declares `reservationChange` with `reservationChangeAdded`, `reservationChangeReleased` and `reservationChangeAddedWithCap` (table 3b) | delivered |
| scope5-k7: A3 safe-action/effects constants in every touched function | no raw safe-action or effects literal remains in the 60 files (probe: quoted `continue|stop|requery|retry|correct_input` and effects values) | delivered |
| scope5-k7: operation-name literal repeated within a file becomes a file-local constant; archive labels | `replaceTaskOperation`, `supersedeTaskOperation`, `cancelTaskOperation`, `archiveTerminalTasksLockOperation`, `restoreTerminalTasksLockOperation` delivered; the repeated `preserved_base` member labels in `replace_task_admission.go` were still literals and are named here | delivered, one residual fixed |
| scope5-k7: Extra keys, the `all` target and `operator` actors stay for Scope 6 | resolved in tables 2, 3 and 7 | reconciled |
| A3 `a3-inventory`: I18 hypothesis threshold, I20 breaker status, I22 provenance and note target | adopted by `orchestrator_wake.go`, `analyze.go`/`sprint_summary.go`, `human_note.go` and siblings | delivered |

## 9. Dynamic identities (J22)

Role-pair, transition, role and provider names from configuration, task, agent and discovery IDs, and user text are never literals in the 60 files: they reach the code as variables (`rolePair`, `transitionName`, `td.targetRolePair`, `input.AgentID`, `taskID`, `disposition` when it is a task ID). The three hard-coded role-pair spellings that are not read from configuration are named once each and documented as legacy or snapshot fallbacks (`legacyPlanningRolePair`, `baseCommitSnapshotRolePair`; A6 `integration_progress.go` repeats them, table 7). No configuration field is added.
