# 226 - A7 Presentation Concerns and the Error Classification Contract

## Status

ACCEPTED.

## Context

The architecture-consolidation run splits the A7 area (presentation: `cmd/liza`,
`internal/commands`, `internal/tui`, `internal/interactive`, `internal/jsonout`,
plus `internal/ops/init_project.go`) along concerns until no A7 function or file
exceeds the size guard, resolves the `jsonout` to `ops` crossing, and moves the
area's finite control-flow vocabulary and magic values to named owners, with no
behaviour change and no new configuration field
([goal](../../goals/20261023-architecture-consolidation.md#structural-constraints),
K3, K7). At the starting commit `96c9ad69a` the area had 126 production files
and nine size offenders (9 files, 17 functions, one of them `ops.InitProject`),
and one allowlisted crossing, `jsonout` to `ops`. The decision is indexed in the
[ADR README](README.md).

[ADR-0021](0021-ops-service-layer-for-mutations.md) makes `ops` the application
layer and presentation its caller; [ADR-0092](0092-build-time-white-label-branding.md)
binds every touched presentation surface to the brand indirection. This ADR keeps
both. Per-area guard data follows
[ADR-0218](0218-architecture-guard-package-and-ratchet-data-seam.md); the A7 data
file `internal/testguard/archguard/area_a7_test.go` is the evidence.

## Decision

### Concern layout

A7 keeps its five packages and adds none. `commands` to `agent` and `tui` to
`agent` are package-level allowlist entries owned by the A9 scope; a new A7
package that needed `agent` would need an entry A7 may not write, and `tui` and
`interactive` import `commands`, whose files share private helpers. Offenders
were reduced by moving whole concerns into new files of the same package. The
"origin" commit is the task commit that delivered the split.

| Concern | Files (delivered) | Origin |
|---|---|---|
| Envelope classification | `jsonout/classify.go` (ordered slot evaluation), `jsonout/classify_slots.go` (per-slot code and message derivation, `errorCode` type); leaf `errors/classification.go` | `c6dcfdf8c` |
| Command definitions | `cmd/liza/cmd_task*.go` (task, blocked, dependencies, records, retire), `cmd_system*.go` (system, inspect), `cmd_tool_result*.go` (tool-result, store), `launch_resolve.go`, `launch_build.go`, `launch_exec.go`, `launch_pairing_seed.go` | `d7321e525`, `0150c27a0`, `7c9291a91` |
| Installation | `commands/init.go` (orchestration), `init_mas_inputs.go`, `init_mas_layout.go`, `init_mas_install.go`, `init_mas_state.go`, `init_integration_branch.go`, `init_pairing.go`, `init_contract_placement.go`, `init_mistral_contract.go`, `setup.go` | `e11874725`, `2c289761f` |
| Inspection | `commands/status.go` plus `status_snapshot.go`, `status_sections.go`, `status_orchestrator.go`, `status_phase_handoff.go`, `status_render.go`, `status_types.go`, `status_vocabulary.go`; `inspect_tasks.go` plus `inspect_task_assembly.go`, `inspect_task_render.go`, `inspect_task_types.go`, `inspect_vocabulary.go` | `bd42d1b34` |
| Agent-pool repair | `commands/repair_agent_pool.go` (entry), `repair_agent_pool_capacity.go`, `repair_agent_pool_start.go`, `repair_agent_pool_spawn.go`, `repair_agent_pool_render.go` | `e8da46d69` |
| Watch | `commands/watch.go` (loop) plus `watch_snapshot.go`, `watch_checks_tasks.go`, `watch_checks_agents.go`, `watch_checks_sprint.go`, `watch_checks_sentinels.go`, `watch_orchestrator.go`, `watch_diagnosis.go`, `watch_repair.go`, `watch_repair_spawn.go`, `watch_reconcile.go` | `6e24bb215` |
| TUI | `tui/update.go` (message dispatch), `update_keys.go`, `update_inline.go`, `activity.go`; `view.go` plus `view_tasks.go`, `view_agents.go`, `view_overlays.go`, `view_footer.go`, `view_activity.go` | `f71ac787f` |
| Vocabulary and magic values | residual substitutions across 83 files; inventory `specs/architecture/consolidation-run/a7-k7-inventory.md` | `d36ca99a3`, `bd43a89e5` |

Final sizes (measured at the closure HEAD `5b422e067`): `init.go` 516 lines (1,684
at start), `watch.go` 151 (2,084), `InitCommandWithConfig` 60 lines (471),
`RunAutoRepairAgentPool` 35 (133). The `init*` files total 2,163 lines and the
`watch*` files 2,339, with the longest function in either family 64 and 76
lines. The size guard rejects any A7 file over 800 lines or function over 100
lines; it passes with only the `InitProject` key left.

### SI-5 classification contract

`jsonout` no longer imports `ops` (its imports are now `errors`, `filelock`,
`models`). Leaf `errors` (`classification.go`) declares one capability interface
per `ops`-typed branch of the former `ClassifyError`:

| Capability | Facts exposed | Implemented by (`ops`) |
|---|---|---|
| `LifecycleOutcomeError` | `LifecycleOutcomeName()` | `*LifecycleError` |
| `StaleCallerError` | `StaleCallerMarker()` | `*AgentAuthorityError` |
| `PostWriteValidationFailure` | `PostWriteValidationMarker()` | `*PostWriteValidationError` |
| `PreconditionFailure` | `PreconditionReason()` | `*PreconditionError` |
| `UnblockRebaseConflict` | `UnblockRebaseConflictMarker()` | `*UnblockRebaseConflictError` |
| `IntegrationFailure` | `IntegrationFailureReason()` | `*IntegrationFailedError` |
| `OperationalFailure` | `OperationalCode()`, `OperationalMessage()`, `OperationalCause()` | `*OperationalError` |

Each implementing type asserts its capability at compile time (`var _ ... = (*T)(nil)`
in `agent_authority.go`, `lifecycle_result.go`, `operational_error.go`,
`post_write_validation_error.go`, `precondition_error.go`, `unblock_task.go`,
`wt_merge.go`). `Error()`, fields, `Unwrap`, `SafeDetails` and `LifecycleResult`
are unchanged. `jsonout` evaluates the slots with `errors.As` in the frozen order
(lifecycle, stale caller, the eight leaf-error branches, post-write, precondition,
unblock conflict, integration failure, operational, file lock, string fallback)
and composes every code and message itself, so the `--json` code table has one
owner.

**Why one capability per slot.** Branch precedence is the frozen surface. With one
capability per slot, the adapter's slot order decides, whatever the error chain
order: an operational error wrapping a lifecycle error still classifies as
lifecycle, and a joined chain follows the same order. A single "classify
yourself" interface would let the outermost `ops` type win and change
classification. The cross-slot precedence is pinned by the oracle test added
before the change (`5c7777263`, `adapter_envelope_precedence_test.go`). Rejected:
an allowlist entry (keeps presentation coupled to `ops` type identity for no
saving); capability declarations inside `jsonout` only (`ops` could not assert
conformance, so a rename would silently drop a branch).

### Alert-category placement

`alerts.Category` (`internal/alerts/categories.go`) owns the 26 category
constants that the watch checks and pool repair emit or match. `Alert.Category`
stays a string, because it is parsed back from `alerts.log` lines whose
categories include those of lifecycle writers in other packages; conversion
happens at construction (`Category.String`) and at comparison (`Category(s)`).
Spellings are byte-identical. Categories emitted by `ops` and `agent` are not
re-owned here; A9 adopts them when refactored.

### D-6 gap dispositions

| Gap | Disposition (oracle added before the change) |
|---|---|
| `repair-agent-pool` output and `--json` | `7e367ace2`: binary dry-run goldens and an adapter on `RepairAgentPool` through the spawn seam |
| Watch auto-repair decisions, spawns, alerts | `2275ac242`, `74d4a74e6`: adapter on `RunAutoRepairAgentPool`, including the unservable-reviewer warning |
| `launch` terminal backends, pairing seeding | `0bf078b15`: stubbed multiplexer and provider executables, Linux only |
| `ClassifyError` cross-slot precedence | `5c7777263` |
| TUI content, init wizard forms | review-only (S4); white-box view and update tests keep asserting |
| Watch header and loop timing | determinism exclusion; code unchanged |

Unexercised leaves, treated as the plan allows (relocated whole, K7 substitution
of a literal by a same-valued constant only): the production default of the spawn
seam (`repairAgentPoolSpawn`, `repair_agent_pool_spawn.go`) and `launchShell`
(`launch_exec.go`, the one `runtime.GOOS` branch, now using `goosWindows`).

### Vocabulary ownership

The complete occurrence-to-owner classification of the 179 A7 production files
(126 existing, 53 created by A7 tasks) is
[a7-k7-inventory.md](../consolidation-run/a7-k7-inventory.md); this ADR records
the placement rules.

| Vocabulary | Owner |
|---|---|
| `--json` error codes assigned by `jsonout` | `jsonout.errorCode` (14 constants); `ErrorDetail.Code` stays `string`; `OperationalError.Code` passes through |
| Alert categories | `alerts.Category` |
| Role types, history events, anomaly types | adopted from `roles` and `models` (A1) |
| Lifecycle safe actions, outcomes, effects; integration reasons | adopted from `ops` (A3, A6) by conversion; no second type |
| Inspect formats, targets, query roots, computed fields; status formats; phase hand-off state | `commands` (`inspectFormat`, `inspectTarget`, `inspectQueryRoot`, `computedField`, `statusFormat`, `phaseHandoffState`, `dashboardSectionFormat`, `worktreeState`) |
| Contract actions (init/setup placement) | `commands.ContractAction` |
| Wizard modes | `interactive.WizardMode` |
| TUI activity kinds, inline actions, modes, filters, column tiers | `tui` |
| Launch backends and presets | `cmd/liza` (`launchBackend`, `masLaunchPreset`) |
| Spellings whose owner keeps them unexported | named beside their A7 use; owner gaps and later-area adoption sites are tables 5 and 7 of the inventory |
| Role names, tool and provider identities, operation names, configured commands | dynamic (ADR-0045, K7) |

### Shim and exported-API audit

- **Shims.** `git grep` for `type X = Y` aliases over the A7 packages, `errors`
  and `alerts` at the closure HEAD finds none (none at the start either). No
  forwarder was added: the Shim Register is empty and no file the A7 tasks
  created exists only to re-export another symbol.
- **Exported surface**, compared by a `go/ast` listing of exported declarations
  (functions, methods, types, struct fields, constants, variables) of
  `commands`, `tui`, `interactive` and `jsonout` at `96c9ad69a` and at the
  closure HEAD (`cmd/liza` is `package main`). `jsonout` is unchanged. Additions
  are K7 vocabulary and its accessors: `commands.ContractAction` and
  `ContractAction{Default,Global,Local,Rename,Skip}`, `InspectFormat{JSON,Table,Value}`,
  `InspectTarget{Agents,Anomalies,Metrics,Tasks}`, `InspectFormatNames`,
  `StatusFormatNames`, `OperatorActor`; `tui.ActivityKind` and
  `ActivityKind{Alert,Anomaly,Log}`; `interactive.WizardMode` and
  `WizardMode{Full,Pairing}`. Six exported fields and one parameter take a defined
  string type in place of `string` (the K7 retyping the plan allows, callers
  conformed under X2): `InitParams.ContractActions`,
  `InitPairingParams.ContractAction` and `ContractActions`, `ActivityEntry.Source`,
  `InitWizardResult.Mode` and `ContractActions`, and the first parameter of
  `PrintPostInitSummary`. Nothing was removed or renamed.
- **Other packages.** `errors` gains the seven capabilities, `alerts` the
  category type and constants, and seven `ops` error types gain their capability
  methods plus a compile-time assertion; `ops` error types keep fields, text and
  `Unwrap`.

### Preserved Sites

Confirmed at the closure HEAD:

| Site | Observation |
|---|---|
| C1 reviewer start-planner lease | one expression, `models.DefaultLeaseDurationSeconds`, in `repair_agent_pool_start.go:158` |
| C4 init Git subprocesses | `check-ref-format --branch`, `branch <name> HEAD`, `rev-parse --verify <name>`, `rev-parse --verify HEAD` stay separate helpers together in `init_integration_branch.go:34,50,60,65`, built as at the start |
| C4 `ops.InitProject` | production diff against the start is one line (`string(models.TaskEventInitialization)`, the A1 event type conversion); its ratchet key remains |
| C6 sites | `bb.Write` in `init_mas_state.go:201` and `migrate.go:77`, `bb.Modify` in `tui/commands.go:330`; no new mutation site in A7 |
| `commands` and `tui` imports of `agent` | package-level edges unchanged (`cmd/liza`, `commands`, `tui` import `agent`; `interactive` and `jsonout` do not). The importing file count grew (5 to 8 in `cmd/liza` and `commands`) only because splits moved callers into new files; the only symbols added to the used set are `agent.OrchestratorWakeTrigger` and the `agent.WakeTrigger*` constants, which existed at the start and replace raw strings in the watch and status code (K7) |
| `cmd_launch.go` `--cwd` | flag definition unchanged |

### K1 inventory

A7 holds one blackboard transaction at both trees: `toggleAutoResumeCmd`'s
`bb.Modify` in `internal/tui/commands.go` (line 327 at `96c9ad69a`, line 330 at
the closure HEAD). Its closure flips `Config.AutoResume` and copies the new
value; it calls nothing, so zero Git- or `exec`-performing calls run inside it.
MAS init and migrate persist with `bb.Write` (`init.go:1382` and `migrate.go:77`
at the start; `init_mas_state.go:201` and `migrate.go:77` now), not a `Modify`
callback. The search covered `Modify`, `ModifyWithAgentAuthority`, `Write`,
`UpdateTask` and `UpdateAgent` calls over `cmd/liza`, `commands`, `tui`,
`interactive` and `jsonout` at both revisions. Method: source search plus a
direct read of the closures (the single callback has no callee to trace), not the
SSA reachability program the A6 record used. A7 has no inherited K1 exception
and adds none (`k1-ruling`).

### References

Every chain-document citation of A7 code resolves at the closure HEAD; none
needed a correction and no chain document was edited:

| Document | Citation | Resolves |
|---|---|---|
| `INVARIANTS.md` (section 5 inspection-active-lists row) | `inspect_tasks.go` | `taskVisibleInActiveList` remains in `internal/commands/inspect_tasks.go` |
| `specs/protocols/await-primitives.md` | `internal/commands/await_budget.go` (`awaitWithBudget`); `cmd/liza/cmd_review.go` (`--timeout-seconds`) | both present |
| `lessons/agents/agent-env-leaks-into-cli-tests.md` | `cmd/liza/mutation_wiring_test.go` (`executeRootCommand`) | present |

`state-machines.md`, `blackboard-schema.md` and `supervision-model.md` cite no
A7 code. No semantic drift was found in these citations.

### Registry note for the A8 boundary

Closure evidence for the issue registry, to be applied by the A8 boundary's G6
under the Update Policy of `architectural-issues.md` (this task does not edit
it):

| Issue | Evidence | Disposition |
|---|---|---|
| Decompose init.go (1,268 LOC) (`issue-init`) | resolved by `e11874725` (MAS phases) and `2c289761f` (pairing init, contract placement, setup); `init.go` 1,684 lines to 516, `InitCommandWithConfig` 471 to 60, `InitPairingCommand` 182 to 42, `createContractSymlinksForProviders` and `SetupCommand` under 100 lines | closure candidate |
| Decompose watch.go (1,407 LOC) (`issue-watch`) | resolved by `6e24bb215` (checks, diagnosis, auto-repair policy, alert output); `watch.go` 2,084 lines to 151, `RunAutoRepairAgentPool` 133 to 35; alert categories to `alerts.Category` | closure candidate |
| Duplicate Initialization Implementations (`issue-dup-init`) | `ops.InitProject` and its ratchet key are untouched | stays open: C4 owns it |
| Commands Layer Imports Agent Runtime (`issue-commands-agent`) | the `commands` and `tui` imports of `agent` are kept | stays open: the A9 scope owns it |
| Control-Flow Vocabulary Bypasses Domain Ownership (`issue-vocabulary`) | A7 vocabulary is placed, but the issue spans every area and owner gaps remain (inventory tables 5 and 7) | stays open until its whole scope is resolved |

### Notes for Scope 25

- Later-area adoption sites (inventory table 7): `ops` actor words
  (`operator`, `human`, `system`), circuit-breaker status, stored verdict, narrow
  actions, the `$` sentinel prefix and the `windows` platform name are named in
  A7 because their owners keep them unexported; they can be adopted once those
  owners export them.
- The substring rules of `jsonout` keep their order and patterns (the Frozen
  Surfaces row): typing the errors an agent branches on did not remove any
  prose rule, as `architecture-review` suggested; this run froze them.
- `A7` ratchet keeps one entry, `InitProject` (ceiling 229), for C4 to delete.

### Scope limits

No production code, historical ADR, `overview.md`, `architectural-issues.md`,
goal, contract or skill is edited by this record. The closure guard is
`TestAreaA7RatchetHoldsOnlyInitProject`.

## Consequences

**Positive:**
- Presentation no longer imports the application layer for classification; the
  K3 allowlist for A7 is empty.
- Each A7 concern has its own file set; the largest A7 function is under 100
  lines and the largest file under 800.
- `--json` codes have one owner, and the classification order is enforced by
  slot order rather than chain order.

**Limitations accepted:**
- Seven capability interfaces and their methods are more surface than one
  interface; the extra surface is the price of preserving branch precedence.
- Owner-gap names duplicate spellings that `ops`, `models` and `agent` keep
  unexported until those areas export them.
- The K1 record uses a source search, not a reachability trace.

## Alternatives Considered

1. **Allowlist `jsonout` to `ops` with a rationale.** Rejected: keeps the
   presentation layer coupled to `ops` type identity for no saving.
2. **One "classify yourself" interface on `ops` errors.** Rejected: the outermost
   `ops` type would win over a wrapped lifecycle error, changing a frozen
   `--json` code.
3. **A7 sub-packages.** Rejected: would export helpers shared by `commands`
   files, or need `agent` imports the A7 data may not allowlist.
4. **A command-registration framework for `cmd/liza`, a generic check runner
   for watch, a single Git runner for init.** Rejected: cobra already is the
   registration framework; watch checks differ in inputs, identities and repair
   side effects; the init Git runner pre-empts C4's design.
