# A1 named-value adoption inventory (arm-1-ar-10-cp-1-code-2)

Traversal: production Go of the 24 A1 packages in the Task 3 race command, platform files included (`*_unix.go`, `*_windows.go`), `_test.go` excluded. Method: AST scans at the submitted revision for (a) string literals whose value equals a constant of a defined string type (declarations skipped), (b) literals in positions typed as a defined string type, (c) every integer/float literal outside `const` declarations other than 0, 1, -1. Result below: every replaced use, then every retained occurrence with its reason. No value, API, configuration field or guard data changed.

## Replaced (value unchanged)

| Area | Where | Named by |
|---|---|---|
| Phase spellings | pipeline `validPhases`, `resolvePhase`, `validateRolePairStates`, decomposition-root and allocation-route checks (`config.go`, `config_validation.go`, `resolver_status.go`) | existing `StateCategory*` |
| Orchestrator role | models `state.go` FindOrchestratorID | existing `roles.Orchestrator` |
| Replan marker | models `replan_lineage.go`, `planning_change.go`, `provider_dependencies.go`; statevalidate `validate_plan_amendment.go`, `validate_provider_dependencies.go` | new `models.ReplannedTransition` |
| Attempt bound 2 | models `task.go` (2 uses); statevalidate `task_invariants.go` (2 uses) | new `models.MaxTaskAttempt` |
| Provider diversity minimum 2; premature-recovery generation 2 | models `task.go`, `integration.go` | `minDiverseApprovals`, `prematureRecoveryFirstGeneration` |
| Prerequisite and runtime-input bounds 64/4096/256/32/65536/128 | models `validation_prerequisites.go`, `agent_env_deny.go`, `runtime_inputs.go` | `maxValidationCommands` … `maxRuntimeInputRecipeLen` |
| Hex lengths 40/64, 32 bytes, 16-byte generation | models `deferred_validation.go`, `quarantined_verdict.go`, `agent.go`; statevalidate `task_acceptance.go`, `validate_lifecycle.go`, `validate_quarantined_verdict.go`, `task_invariants.go` | `gitSHA1HexLen`, `sha256HexLen`, `sha256Bytes`, `agentGenerationBytes` |
| Acceptance receipt bounds 256/64/1 MiB | statevalidate `task_acceptance.go` | `maxAcceptance*` |
| State-watch debounce 50 ms | db `watcher.go` | `stateWatchDebounce` |
| Lock retry cap 5 s, doubling, caller frames 32/8/skip 2 | filelock `filelock.go` | `maxAcquireBackoff`, `backoffGrowthFactor`, `lockCaller*` |
| Terminal archive format 2, entry overhead 32, search window 256, digest 64 | db `terminal_archive.go` | `terminalArchiveFormatVersion` … `terminalDigestHexLen` |
| File and directory modes 0644/0600/0700/0755 | db `blackboard.go`, `liveness.go`; filelock `hold_unix.go`; alerts; usage `records.go`; log; runtimeinputs `key.go`; archiveobject; atomicfile | `stateFileMode`, `holdLockFileMode`, `alertFileMode`, `recordDirMode`, `recordFileMode`, `logFileMode`, `keyDirMode`, `keyFileMode`, `objectDirMode`, `objectFileMode`, `dirMode` |
| Alert key digest prefix 8; line separator `": "` | alerts `alerts.go` | `alertKeyDigestBytes`, `levelCategorySeparator` |
| Tail window doubling | log `logger.go` | `tailWindowGrowthFactor` |
| Archive shard prefix 2 | archiveobject `objects.go` | `shardPrefixLen` |
| Runtime key ID prefix 8 | runtimeinputs `key.go` | `keyIDBytes` |
| Pattern thresholds 4/3/2 and excerpt limits 240/500/80 | analysis `patterns.go` (not the C2 functions below) | `planningRejectionThreshold` … `eventFieldExcerptLimit` |
| Quantiles 0.5/0.95 | usage `report.go` | `medianQuantile`, `p95Quantile` |
| Blocked-question cap 3 | payloadschema `mark_blocked.go`, `assess_blocked.go` | `maxBlockedQuestions` |
| Acceptance timeout 600, JSON depth 16, Markdown fence/indent/heading limits, heading levels 2/3, OID 40, drive root 3 | referencecontract | `defaultAcceptanceTimeoutSeconds` … `windowsDriveRootLen` |
| Drive prefix 2; rev-parse output lines 2 | paths `normalize.go`, `paths.go`; statevalidate `artifact_refs.go` | `driveLetterPrefixLen`, `revParseOutputLines` |
| Ref arities 2/3; allocation routes 2 | pipeline `config.go` | `twoPartRefLen`, `threePartRefLen`, `allocationRouteCount` |

## Retained numeric literals

| Location | Reason |
|---|---|
| analysis/patterns.go:447,449,503 | C2 provider-audit grouping (response priority 3/2, agent threshold 2, hit threshold 3): the scope allows only V7 adoption there, so its numeric literals are deliberately not named |
| archiveobject/objects.go:35 | `sha256.Size*2`: bytes to hex characters, structural |
| db/blackboard.go:211; db/terminal_archive.go:28 | already named package vars (`patientReadLockTimeout`, `terminalArchiveCacheMaxBytes`); tests assign them, so they stay `var` |
| db/blackboard.go:453,461,479 | slice capacity slack and regexp capture-group indexes of the YAML block-scalar rewriter |
| db/liveness.go:47 | `livenessRecordHashLen/2`: hex length to byte count, derived from a named constant |
| models/provider_dependencies.go:109,117 | `[2]string` composite map key, structural |
| models/sprint.go:177,181; usage/report.go:630 | percentage arithmetic |
| pipeline/config.go:442,445; referencecontract/contract.go:184 | element indexes into a split result or string (`parts[2]`, `value[1]`, `value[2]`) |
| render/duration.go:21-31 | unit conversions (60/24) |
| rolemodels/rolemodels.go:77 | YAML mapping node content stride (key, value pair) |
| usage/records.go:211,212 | regexp capture-group index |
| usage/records.go:301 | 24 hours per day, unit conversion |
| usage/report.go:741,747 | `[2]int` pair counters, structural |

Excluded by rule: `models.DefaultLeaseDurationSeconds` (C1) keeps name and value; role names stay untyped (ADR-0045).

## Retained string occurrences equal to a vocabulary value

| Location | Reason |
|---|---|
| analysis/patterns.go:286; models/history.go:347 | `debt_created` is an anomaly details key (data field name), not an anomaly type |
| db/terminal_archive.go:529 | `created` is a map key of the archive stub row |
| filelock/errors.go:41; filelock/filelock.go:189; models/planning_change.go:24,68,72 | `unknown` is a default operation or trigger label, not a rejection cause or provenance |
| models/quarantined_verdict.go:45 | verdict disposition set, a separate open vocabulary from history events and usage outcomes |
| models/task_claimability.go:156; statevalidate/validate_agent.go:164 | diagnostic code ids / message prose (frozen text) |
| models/validation_readiness.go:56; payloadschema/handoff.go:13 | readiness result string and handoff payload field name; not the deferred-validation status types |
| payloadschema/set_task_output.go:211 | free-text wildcard word in a scope phrase list |
| rolemodels/rolemodels.go:116,118 | `defaults` YAML keys, not role or claim types |
| roles/roles.go:85 | role-name lookup table key (role names stay dynamic per ADR-0045) |
| statevalidate/task_status_fields.go:123-208 | phase labels in violation ids and messages (frozen message text) |
| Empty-string `""` in typed positions (`models.TaskStatus("")`, `PlanAmendmentMode`, `TaskType`, `RoleType`, `ReviewImpact`, `DiscoveryUrgency`, `writeString`, `CircuitBreakerResponseType`) | zero value / "unset", not a vocabulary member |
