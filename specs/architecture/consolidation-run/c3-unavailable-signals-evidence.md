# C3 Canonical Provider-Unavailable Signals: Evidence

Task `arm-1-ar-24-cp-0-code-0`. Tree before the change: `e63a111b1` (integration head at claim). Recorded
2026-10-27. Acceptance manifest:
`specs/plans/20261023-architecture-consolidation/acceptance/arm-1-ar-24-cp-0-c3.json`. Decision:
[ADR-0230](../ADR/0230-canonical-provider-unavailable-signals.md). Commits: `f751d845c` (production, tests, docs),
`7ffba17f6` (oracle change point and ledger).

## Site list at coding HEAD

The plan's file names moved under A9 and were re-found by function.

| Concern | Site (function, file) | Before | After |
|---|---|---|---|
| Record | `ProviderUnavailable`, `ClassifyProviderUnavailable` (`providersignal/provider_unavailable.go`) | `Provider` (label) only | `Tool` set to the CLI the classifier was asked about |
| Raise | `handleClassifiedProviderCrash` (`agent/supervisor.go`) | `WriteProviderUnavailableSignal(root, pu.Provider, pu.Message)` | `WriteProviderUnavailableSignal(root, pu)`; the alert call is unchanged |
| Storage | `ProviderUnavailableSignalPath`, `WriteProviderUnavailableSignal` | file `provider-unavailable-<ACPXAgentName(provider)>` | file `provider-unavailable-<encodeSignalName(tool)>`; `tool:` line only when tool differs from the label |
| Match | `CheckProviderUnavailableSignal` | `os.Stat` of the alias-normalised path | enumerate records, `keys().Same(record tool, tool)`; keys lazy |
| Supervisor stop | `handleProviderUnavailableSignal`, called from `admitIteration` (`agent/supervisor_turn.go`) | by `CLIName` | `providerKeysOfSnapshot(supervisorCtx, bb)`: published snapshot config, read only when a record exists |
| Orchestrator wake | `orchestratorStrategy.RevalidateWake` (`agent/strategy_orchestrator.go`) | by `CLIName` | `providerKeysOfConfig(state.Config)`: the state it revalidates; clause order unchanged |
| Spawn | `spawnAgent` (`process/spawn.go`) | check, then `readRuntimeConfig` | quota check, one `readRuntimeConfig`, unavailable check with keys from it, prerequisites |
| CLI resume | `ResumeCommand` (`commands/resume.go`) | glob, file-name provider, clear by rebuilt path | `ClearProviderUnavailableSignals`: rows by display provider, clear by enumerated path |
| TUI resume | `resumeSystemCmd` (`tui/commands.go`) | same loop | same call |

Removed exports: `ProviderUnavailableSignalGlob`, `ProviderFromUnavailableSignalFile`; `ClearProviderUnavailableSignal`
now takes the record path. No shim or alias was added. Quota code is untouched.

## Encoding (plan constraints)

- Legacy listing and clearing preserved: a file without a `tool:` line shows its literal file name (never
  percent-decoded) and is identified by it (`TestC3MatchLegacyRecords`, `TestC3MatchLegacyPercentNameKeepsLiteralIdentity`,
  `TestC3ResumeCommandListsAndClearsRecordsByHandle`). A new record whose tool differs from its label or from its
  encoded name carries a `tool:` line, the only source of its identity.
- Row order is the pre-C3 order: records sort by the file name their display provider had before C3 (alias-normalised
  label), not by physical name (`TestC3PresentKeepsPreC3RowOrderForAliasTools`: `acpx-codex` plus `claude` lists
  `claude`, `codex`).
- Unlistable directory (mode 0111): the stat-visible records of the registry tools sharing the checking tool's key, of
  the tool and of its alias-normalised name, under the literal and the encoded file name, are read and compared through
  the current keys, not by existence (`TestC3FailUnlistableDirectoryKeepsStatVisibleSignal`, `...MissingRecordDoesNotStop`,
  `...StillComparesCanonicalKeys`, `...FindsLegacyLiteralName`, `...StopsBothWaysOnActualRaise`,
  `...FindsSiblingCustomKeyRecord`). The registry tool names come from `providersignal.MatchKeys`, built from the same
  base and configuration as the C2 keys (`agentlaunch` untouched). A record of an unregistered, unrelated tool cannot be
  discovered without a listing.
- Catalog capture: for an unlistable directory the keys view is built when the check starts (its candidates come from
  the registry); for a listable one only when a record exists.
- Distinct effective keys are not conflated: one file per encoded launch tool (`TestC3StoreRecordsDoNotConflateDistinctTools`).
- Filesystem addressing is separate from configured text: `encodeSignalName` percent-encodes every byte outside
  `[A-Za-z0-9_-]` and a leading dot, is injective and cannot yield a separator or dot segment
  (`TestC3StoreToolNamesStayInsideSignalDirectory`).
- Clear handles are enumerated paths (`ClearProviderUnavailableSignal(path)`).
- No schema migration or new mode. A record whose tool equals its label has exactly the pre-C3 bytes, so the A9
  `i20-claude` and `i20-codex` state goldens pass unedited.

## Catalog and config capture per consumer

All consumers call `BuiltInAgentTools()` (C2's loader, cache and fallback) through `agentlaunch.NewProviderKeys`, outside
any `Modify` or lifecycle callback, at most once per evaluation (the `keys` func is invoked once and shared across the
records, `TestC3CheckReadsKeysOnlyWhenARecordExists`); no refresh or cache was added.

| Consumer | Config used | Capture point |
|---|---|---|
| Supervisor admission | last published snapshot (`bb.ReadSnapshot`, lock-free); an unreadable snapshot compares under the catalog alone | inside the lazy func, only if a record exists, in `admitIteration` (no transaction) |
| Orchestrator wake | the `state` argument of `RevalidateWake` | same expression as before, evaluated lazily |
| Spawn | `readRuntimeConfig(projectRoot)`, read once after the quota gate | `spawnAgent`, before the check |

## Mutation checks (red without the path, green after restore)

| ID | Mutation | Failing tests |
|---|---|---|
| M1 | `Check` compares `ACPXAgentName` of tool and record tool instead of `Same` | `TestC3MatchCustomSharedKey`, `TestC3MatchOverrideSeparatesAliases`, `TestC3MatchHonoursLoadedCatalog`, `TestC3MatchEvaluatesKeysAtCheckTime`, `TestC3MatchLegacyRecords`, `TestC3StoreRecordsDoNotConflateDistinctTools` |
| M2 | supervisor keys ignore the snapshot config | `TestC3SupervisorDecidesUnderSnapshotConfigAtCheckTime`, `TestC3SupervisorAndOrchestratorWakeAgree` |
| M3 | wake revalidation keys ignore `state.Config` | `TestC3SupervisorAndOrchestratorWakeAgree` |
| M4 | spawn keys ignore the runtime config | `TestC3SpawnRefusesByCanonicalKey` |
| M5 | classifier drops `Tool` | `TestC3CrashWriterKeepsOriginatingToolSeparateFromLabel` |

| M6 | the unlistable-directory fallback decides by existence, ignoring the keys | `TestC3FailUnlistableDirectoryKeepsStatVisibleSignal`, `...MissingRecordDoesNotStop`, `...StillComparesCanonicalKeys`, `...FindsLegacyLiteralName` |
| M6b | the fallback probes encoded names only | `TestC3FailUnlistableDirectoryFindsLegacyLiteralName` |
| M6c | the fallback omits sibling registry tools | `TestC3FailUnlistableDirectoryStopsBothWaysOnActualRaise`, `TestC3FailUnlistableDirectoryFindsSiblingCustomKeyRecord` |
| M7 | rows ordered by physical tool file name | `TestC3PresentKeepsPreC3RowOrderForAliasTools` |
| M8 | no `tool:` line for escaped names | `TestC3StoreToolNamesStayInsideSignalDirectory` |

Each mutation was applied alone, the named tests failed on the intended assertion, and the file was restored from a copy
(the restored tree equals the committed one).

## Old to new edit list

| Path | Change |
|---|---|
| `internal/oracle/a9/signals_test.go` | `i21-unavailable-codex-run` `blocked` false to true (D56 R1) |
| `.../human/i21-unavailable-codex-run/agent.txt` | stdout loses the four-line "System stopped" output the launched agent produced; the run exits 0 without a claim, stderr unchanged |
| `.../state/i21-unavailable-codex-run/launch.yaml`, `listing.txt`, `state.yaml` | no claim, task stays `DRAFT_CODE`, mutation sequence 3 to 2 |
| `.../state/i21-unavailable-codex-run/files/usage-records-1.jsonl` | deleted (no launch happens) |
| `internal/oracle/PROVENANCE` | six `changed <path> C3` lines (driver and five goldens) in commit `7ffba17f6` |

Existing tests changed only at call sites (`WriteProviderUnavailableSignal`, `CheckProviderUnavailableSignal`, handler
signature) or for the record encoding: `provider_unavailable_acp_test.go` now reads the `codex-acp` record with its
`tool:` line, and `TestProviderUnavailableSignal_NormalizesACPXProviderAliases` became
`TestProviderUnavailableSignal_AliasesObserveEachOthersRecord`; what each asserted (aliases stop each other, bounded
diagnostic, no stderr leakage, alert text) stays asserted. Test `TestMain` files pin the embedded catalog in
`providersignal`, `process` and `tui` and in the `integration` `TestMain`, as C2 did for `agent` and `commands`.

## Index 0 selection per package (`go test -list`, regex of index 0)

| Package | Selected tests | `TestC3…` |
|---|---|---|
| `providersignal` | 58 | 14 |
| `agent` | 23 | 4 |
| `process` | 9 | 3 |
| `commands` | 10 | 2 |
| `tui` | 9 | 2 |
| `integration` | 4 | 2 |
| `oracle/a9` | 3 | 0 |

## Other records

- Doc re-grep (`rg 'ProviderUnavailableSignalGlob|ProviderFromUnavailableSignalFile|WriteProviderUnavailableSignal|CheckProviderUnavailableSignal|handleProviderUnavailableSignal|provider-unavailable-'`
  over current docs): `supervision-model.md` (updated), `support-docs/SUPPORT.md` (describes `ls provider-unavailable-*`,
  still accurate; not edited per plan), `TECH_DEBT.md` (historical). ADR-0230 and its index row are added.
- Brand scan of added Go lines (G1.3): only existing helper identifiers (`SetupLizaDir`, `LizaDir`); no raw
  default-brand literal.
- No `go test -race` command (D55). Fixtures are disposable temp projects, `env -i` commands, no provider process,
  network or live state.

## Measured pre-submission runs (tree `7ffba17f6`, env -i)

| Command | Result | Time |
|---|---|---|
| index 0 focused batch over `./internal/...` | all packages ok, no failure | 38 s |
| `go test ./internal/oracle/a9/` (after the golden rewrite) | ok | 129 s |
| `regen -mode check -pkg ./internal/oracle/...` | exit 0, 2513 `ok` lines; the six C3 paths report `chain pre f751d845c… C3 7ffba17f6…` (`usage-records-1.jsonl` ends `deleted`) | 459 s |
| `go test ./internal/testguard/archguard/` | ok | 14 s |
| `make lint` | ok (cross-OS vet, K4/S1/C6 guards, embedded consistency) | under 580 s |

Index 0 is far inside the 7200 s admitted timeout; `make test`, `make test-e2e` and the full oracle run are executed by the
submission gate.
