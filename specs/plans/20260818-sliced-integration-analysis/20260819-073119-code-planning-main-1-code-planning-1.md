# Code Plan: Global Integration Generation Ceiling

## Intent and evidence

Add one persisted, stack-agnostic configuration contract for the maximum number of global integration analysis generations, seed every new workspace with the deterministic value `3`, and preserve explicit positive configuration through normalization and YAML serialization.

Success means both project-initialization paths write `max_global_integration_generations: 3`; legacy zero and negative values resolve to `3`; positive values remain unchanged through normalization and YAML round-trip; and the shared valid-state fixture carries the same default.

Based on: `specs/goals/20260818-sliced-integration-analysis.md` sections Final Closure and Required Properties; the assigned task and its parent master plan; `internal/models/config.go`; `internal/ops/init_project.go`; `internal/commands/init.go`; `internal/testhelpers/fixtures.go`; focused reads of their existing tests; ADR-0031; ADR-0050; `INVARIANTS.md` Protection Matrix; and the Update Policy plus relevant initialization debt in `specs/architecture/architectural-issues.md`.

EVIDENCED: `internal/ops.InitProject` and `internal/commands.InitCommandWithConfig` construct independent `models.Config` literals, so both must seed the new field. The existing duplicate-initialization issue remains out of scope; this plan adds only the mirrored default assignments required for behavioral parity.

EVIDENCED: The parent plan owns exactly `Config.MaxGlobalIntegrationGenerations` and `NormalizeGlobalIntegrationGenerationLimit`. No initializer option, command-line flag, generation decision, or documentation change is introduced here. Configurability is the persisted YAML field; initialization only seeds its deterministic default.

Doc Impact: none in this scoped plan; the parent plan's documentation task owns configuration and lifecycle documentation after implementation evidence exists.

Test Impact: three package-local tests named `TestGlobalIntegrationGenerationLimitDefaults` prove the model contract and both initialization paths. The parent canonical validation runs all three packages together.

## Architecture and boundaries

```text
models.Config + NormalizeGlobalIntegrationGenerationLimit
                  |
          +-------+-------+
          |               |
 ops.InitProject   commands.InitCommandWithConfig
          |               |
          +-------+-------+
                  |
       persisted state.yaml default: 3
```

The model package owns the YAML field and the effective-value rule. Both initialization paths consume that rule and persist the default. Later integration-progress and reconciliation tasks consume the model interface but remain outside this plan.

The change is reversible and schema-compatible: older state files decode the absent field as zero, and the exported normalizer maps all non-positive values to `3`. Positive values are not capped or rewritten. No stack command, project path, build-system assumption, or provider-specific behavior enters the configuration contract.

## Dependency and ownership graph

```text
Task 1 model contract
       |          |
       v          v
Task 2 ops init  Task 3 command init
```

Tasks 2 and 3 depend on Task 1 and may run in parallel. No file is shared between tasks.

| Exact interface or behavior | Sole owner | Consumers |
|---|---|---|
| `Config.MaxGlobalIntegrationGenerations`; `NormalizeGlobalIntegrationGenerationLimit`; shared valid-state default | Task 1 | Tasks 2-3 and the parent plan's progress, reconciliation, E2E, and documentation tasks |
| Non-interactive `InitProject` persisted default | Task 2 | New non-interactive workspaces and parent E2E coverage |
| `InitCommandWithConfig` persisted default | Task 3 | New command-initialized workspaces and parent E2E coverage |

## Validation contract

Each task uses one package-local `go test -json` command. The `jq` predicate requires an exact passing event for `TestGlobalIntegrationGenerationLimitDefaults` and rejects every failing event, so an absent selector cannot pass. After all three tasks merge, the parent canonical command provides the aggregate proof across `internal/models`, `internal/ops`, and `internal/commands`.

## Planned coding tasks

### Task 1 — Define the persisted limit contract

Description: Define the persisted global integration generation limit, deterministic normalization, and shared valid-state default.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/models` proves absent, zero, and negative YAML values normalize to `3`; a positive value such as `7` remains `7` through normalization and YAML round-trip under `max_global_integration_generations`; and `testhelpers.CreateValidState` carries the normalized default without stack-specific data.

Scope: Own `internal/models/config.go`, create `internal/models/config_test.go`, and own `internal/testhelpers/fixtures.go`. Add `Config.MaxGlobalIntegrationGenerations` and `NormalizeGlobalIntegrationGenerationLimit`, and update the shared valid-state fixture. Do not add initialization parameters, command-line flags, generation decisions, documentation, or stack-specific commands.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test -json ./internal/models -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/config.go, internal/models/config_test.go, internal/testhelpers/fixtures.go]`; `owned_modules=[internal/models, internal/testhelpers]`; `read_only_depends_on=[]`; `interfaces_owned=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit, shared valid-state generation default]`; coverage: schema compatibility, normalization, positive-value preservation, YAML round-trip, and fixture parity.

### Task 2 — Seed non-interactive initialization

Description: Persist the deterministic global integration generation default from the non-interactive project initializer.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/ops` initializes a new workspace through `InitProject`, reads the resulting state, observes `Config.MaxGlobalIntegrationGenerations == 3`, and confirms the persisted YAML contains `max_global_integration_generations: 3`.

Scope: Own `internal/ops/init_project.go` and `internal/ops/init_project_test.go`. Consume Task 1's model contract to seed the default in the existing `InitProject` config literal. Do not add a new initializer parameter, alter cleanup or Git behavior, refactor duplicated initialization, or implement generation decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Task 1.

Validation: `go test -json ./internal/ops -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/init_project.go, internal/ops/init_project_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0]`; `interfaces_owned=[non-interactive initialization generation default]`; `interfaces_consumed=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]`; coverage: programmatic initialization persists the typed and literal YAML default.

### Task 3 — Seed command initialization

Description: Persist the deterministic global integration generation default from the command project initializer.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/commands` initializes a new workspace through `InitCommandWithConfig`, reads the resulting state, observes `Config.MaxGlobalIntegrationGenerations == 3`, and confirms the persisted YAML contains `max_global_integration_generations: 3`.

Scope: Own `internal/commands/init.go` and `internal/commands/init_test.go`. Consume Task 1's model contract to seed the default in the existing command initializer config literal. Do not add a new parameter or CLI flag, alter interactive setup behavior, refactor duplicated initialization, or implement generation decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Task 1.

Validation: `go test -json ./internal/commands -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/commands/init.go, internal/commands/init_test.go]`; `owned_modules=[internal/commands]`; `read_only_depends_on=[0]`; `interfaces_owned=[command initialization generation default]`; `interfaces_consumed=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]`; coverage: user-facing command initialization persists the typed and literal YAML default.

## Architecture review

The field belongs in `models.Config`, the stable persistence boundary, while the two existing initializers only consume the model rule. This preserves dependency direction (`commands` and `ops` depend on `models`) and avoids a third owner for normalization.

The main structural risk is the documented duplicate initialization implementation. Converging those implementations would be a separate refactor with a different intent and broader regression surface. Mirroring one model-owned default across the two existing literals is the minimum scoped change, and separate tests prevent either path from drifting silently.

No new architectural issue is introduced by this plan. The generation decision and exhaustion behavior remain owned by downstream tasks, so this configuration work cannot accidentally interpret lifecycle state.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | The global scan/fix cycle has a configurable generation bound. | Final Closure | Task 1 | Covered |
| 2 | The global generation bound has deterministic default `3`. | Final Closure; assigned description | Tasks 1-3 | Covered |
| 3 | New workspaces persist `max_global_integration_generations: 3` through both existing initialization paths. | Assigned done_when | Tasks 2-3 | Covered |
| 4 | Legacy absent or zero values normalize to `3`. | Assigned done_when | Task 1 | Covered |
| 5 | Legacy negative values normalize to `3`. | Assigned done_when | Task 1 | Covered |
| 6 | Positive configured values remain unchanged through normalization and YAML round-trip. | Assigned done_when | Task 1 | Covered |
| 7 | The workflow remains stack-agnostic; no build-system, project-command, path, or provider assumption is introduced. | Required Properties; GUARDRAILS.md G1.1 | Tasks 1-3 | Covered |
| 8 | This scope defines configuration and initialization defaults only, without generation decisions or documentation. | Assigned scope | Tasks 1-3 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: parent Task 10 owns lifecycle-level E2E coverage after all integration components exist; this plan supplies focused model and initializer tests. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: parent Task 11 owns configuration and lifecycle documentation after implementation evidence exists; documentation is explicitly excluded here. | N/A |

## Pre-submit audit

- Three atomic tasks match three `output[]` entries in order.
- Task 1 owns the shared model interface; Tasks 2 and 3 consume it and are dependency-ordered after output index `0`.
- No file appears in more than one task scope, so Tasks 2 and 3 may execute concurrently.
- Every scoped requirement and acceptance criterion is covered; there is no GAP.
- Each exact test selector must emit a passing JSON event, and every failing event rejects validation.
- No task claims generation decisions, CLI flag wiring, documentation, E2E lifecycle coverage, or initialization refactoring.
