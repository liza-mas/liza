# Code Plan: Global Integration Generation Ceiling

## Intent and evidence

Add one persisted, stack-agnostic configuration contract for the maximum number of global integration analysis generations, and make both existing workspace initializers persist its normalized input with deterministic default `3`.

Success means both initialization contracts map omitted, zero, or negative input to `max_global_integration_generations: 3`; an explicit positive input such as `7` traverses each initializer unchanged into typed state and YAML; legacy YAML values normalize by the same model-owned rule; and the shared valid-state fixture carries the default.

Based on: `specs/goals/20260818-sliced-integration-analysis.md` sections Final Closure and Required Properties; the assigned task, parent master plan, and prior rejection; `internal/models/config.go`; `internal/ops/init_project.go`; `internal/commands/init.go`; `internal/testhelpers/fixtures.go`; focused reads of their existing tests; ADR-0055; ADR-0112; `INVARIANTS.md` Protection Matrix; and the Update Policy plus relevant initialization debt in `specs/architecture/architectural-issues.md`.

EVIDENCED: `internal/ops.InitProject` and `internal/commands.InitCommandWithConfig` accept independent parameter structs and construct independent `models.Config` literals. Preserving a caller-supplied positive value through initialization therefore requires the smallest matching input on each existing parameter struct; seeding only literal `3` values would not satisfy the assigned acceptance criterion.

EVIDENCED: adding `MaxGlobalIntegrationGenerations int` to `InitProjectParams` and `InitParams` is source-compatible with the current keyed struct literals. Both initializers can pass that value through the model-owned normalizer without adding a CLI flag, stack command, project path, provider assumption, or generation decision.

Doc Impact: none in this scoped plan; master-plan Task 11 owns configuration and lifecycle documentation after implementation evidence exists.

Test Impact: three package-local tests named `TestGlobalIntegrationGenerationLimitDefaults` prove the model contract and each initialization path. Their three package-specific validation commands, taken together, are the complete acceptance proof.

## Architecture and boundaries

```text
InitProjectParams.MaxGlobalIntegrationGenerations ----+
                                                     |
                                                     v
models.Config.MaxGlobalIntegrationGenerations <- NormalizeGlobalIntegrationGenerationLimit
                                                     ^
                                                     |
InitParams.MaxGlobalIntegrationGenerations ----------+
                         |
                         v
                persisted state.yaml
```

The model package owns the YAML field and effective-value rule, including the default value returned by the normalizer. Each initializer owns only its matching input and assignment into the persisted config. Later integration-progress and reconciliation tasks consume the model interface but remain outside this plan.

The change is reversible and schema-compatible: older state files decode an absent field as zero, and the exported normalizer maps every non-positive value to `3`. Positive values are not capped or rewritten. The two initializer inputs deliberately are not exposed as command-line flags in this scope.

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
| `Config.MaxGlobalIntegrationGenerations`; `NormalizeGlobalIntegrationGenerationLimit`; shared valid-state default | Task 1 | Tasks 2-3 and the master plan's progress, reconciliation, E2E, and documentation tasks |
| `InitProjectParams.MaxGlobalIntegrationGenerations`; non-interactive normalized persistence | Task 2 | Programmatic initialization callers and master-plan E2E coverage |
| `InitParams.MaxGlobalIntegrationGenerations`; command-initializer normalized persistence | Task 3 | Existing `InitCommandWithConfig` callers and master-plan E2E coverage |

## Validation contract

Each task uses one package-local `go test -json` command. Each `jq` predicate requires an exact passing event for the package-local `TestGlobalIntegrationGenerationLimitDefaults` and rejects every failing event, so an absent selector cannot pass. The conjunction of all three package-local commands is the complete proof: Task 1 proves schema and normalization, while Tasks 2 and 3 each prove default, non-positive, and positive input through their own initialization boundary and YAML persistence.

The parent task's supplied three-package command remains a broad smoke check, but its `any(...)` pass predicate does not prove that every package defines and passes the named test. It is not the sole aggregate evidence; downstream review must require all three output validations.

## Planned coding tasks

### Task 1 — Define the model-owned limit contract

Description: Define the persisted global integration generation limit, deterministic normalization, and shared valid-state default.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/models` proves absent, zero, and negative YAML values normalize to `3`; a positive value such as `7` remains `7` through normalization and YAML round-trip under `max_global_integration_generations`; and `testhelpers.CreateValidState` carries the normalized default without stack-specific data.

Scope: Own `internal/models/config.go`, create `internal/models/config_test.go`, and own `internal/testhelpers/fixtures.go`. Add `Config.MaxGlobalIntegrationGenerations` and `NormalizeGlobalIntegrationGenerationLimit`, and update the shared valid-state fixture. Do not add initializer inputs, command-line flags, generation decisions, documentation, or stack-specific commands.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Validation: `go test -json ./internal/models -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/models/config.go, internal/models/config_test.go, internal/testhelpers/fixtures.go]`; `owned_modules=[internal/models, internal/testhelpers]`; `read_only_depends_on=[]`; `interfaces_owned=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit, shared valid-state generation default]`; coverage: schema compatibility, normalization, positive-value preservation, YAML round-trip, and fixture parity.

### Task 2 — Preserve normalized input through non-interactive initialization

Description: Persist a normalized global integration generation limit through the non-interactive project initializer.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/ops` initializes separate workspaces through `InitProject` with `InitProjectParams.MaxGlobalIntegrationGenerations` set to `0`, a negative value, and `7`; reads each resulting state; observes `3`, `3`, and `7` respectively; and confirms each persisted YAML value under `max_global_integration_generations` matches the typed state.

Scope: Own `internal/ops/init_project.go` and `internal/ops/init_project_test.go`. Add `InitProjectParams.MaxGlobalIntegrationGenerations` as the smallest programmatic initializer input, normalize it through Task 1's model contract, and persist the result in the existing config literal. Do not add a command-line flag, alter cleanup or Git behavior, refactor duplicated initialization, or implement generation decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Task 1.

Validation: `go test -json ./internal/ops -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/ops/init_project.go, internal/ops/init_project_test.go]`; `owned_modules=[internal/ops]`; `read_only_depends_on=[0]`; `interfaces_owned=[InitProjectParams.MaxGlobalIntegrationGenerations, non-interactive normalized generation-limit persistence]`; `interfaces_consumed=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]`; coverage: omitted/default, negative, and positive input cross the programmatic initialization boundary into typed state and literal YAML.

### Task 3 — Preserve normalized input through command initialization

Description: Persist a normalized global integration generation limit through the command project initializer.

Done when: `TestGlobalIntegrationGenerationLimitDefaults` in `internal/commands` initializes separate workspaces through `InitCommandWithConfig` with `InitParams.MaxGlobalIntegrationGenerations` set to `0`, a negative value, and `7`; reads each resulting state; observes `3`, `3`, and `7` respectively; and confirms each persisted YAML value under `max_global_integration_generations` matches the typed state.

Scope: Own `internal/commands/init.go` and `internal/commands/init_test.go`. Add `InitParams.MaxGlobalIntegrationGenerations` as the smallest command-initializer input, normalize it through Task 1's model contract, and persist the result in the existing config literal. Do not add or wire a CLI flag, alter interactive setup behavior, refactor duplicated initialization, or implement generation decisions.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#final-closure`

Depends on: Task 1.

Validation: `go test -json ./internal/commands -run '^TestGlobalIntegrationGenerationLimitDefaults$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestGlobalIntegrationGenerationLimitDefaults") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/commands/init.go, internal/commands/init_test.go]`; `owned_modules=[internal/commands]`; `read_only_depends_on=[0]`; `interfaces_owned=[InitParams.MaxGlobalIntegrationGenerations, command-initializer normalized generation-limit persistence]`; `interfaces_consumed=[Config.MaxGlobalIntegrationGenerations, NormalizeGlobalIntegrationGenerationLimit]`; coverage: omitted/default, negative, and positive input cross the command initialization boundary into typed state and literal YAML.

## Architecture review

The normalizer remains at the stable model boundary, and the initializers only transport input into persisted state. This keeps the effective-value rule singular while satisfying the assigned requirement that positive configuration actually traverse initialization.

The main structural risk is the documented duplicate initialization implementation. Converging those implementations would be a separate refactor with a different intent and broader regression surface. Mirroring one integer input and one model-owned normalization call across the existing boundaries is the minimum scoped change; separate tests prevent either path from silently hardcoding `3` or dropping a positive value.

No new stack-specific behavior or generation decision is introduced. The new exported parameter fields widen only the existing initializer data carriers and preserve all existing keyed callers through their zero-value default.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | The global scan/fix cycle has a configurable persisted generation bound. | Final Closure | Task 1 | Covered |
| 2 | The global generation bound has deterministic default `3`. | Final Closure; assigned description | Tasks 1-3 | Covered |
| 3 | New workspaces persist `max_global_integration_generations: 3` through both existing initialization paths. | Assigned done_when | Tasks 2-3 | Covered |
| 4 | Legacy absent, zero, or negative values normalize to `3`. | Assigned done_when | Task 1 | Covered |
| 5 | Positive configured values remain unchanged through model normalization and YAML round-trip. | Assigned done_when | Task 1 | Covered |
| 6 | Positive value `7` supplied through each applicable initializer remains `7` in resulting typed state and YAML. | Assigned done_when; prior rejection closure condition | Tasks 2-3 | Covered |
| 7 | The shared valid-state fixture carries the deterministic default. | Assigned scope; coverage notes | Task 1 | Covered |
| 8 | The workflow remains stack-agnostic; no build-system, project-command, path, or provider assumption is introduced. | Required Properties; GUARDRAILS.md G1.1 | Tasks 1-3 | Covered |
| 9 | This scope defines configuration and initialization behavior only, without generation decisions or documentation. | Assigned scope | Tasks 1-3 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: master-plan Task 10 owns lifecycle-level E2E coverage after all integration components exist; this plan supplies focused model and initializer integration tests. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: master-plan Task 11 owns configuration and lifecycle documentation after implementation evidence exists; documentation is explicitly excluded here. | N/A |

## Pre-submit audit

- Three atomic tasks match three `output[]` entries in order.
- Task 1 owns the model contract; Tasks 2 and 3 own the smallest matching initializer inputs and are dependency-ordered after output index `0`.
- No file appears in more than one task scope, so Tasks 2 and 3 may execute concurrently.
- Every scoped requirement and acceptance criterion is covered; there is no GAP.
- Complete acceptance evidence is the conjunction of the three package-local validations, not the parent command's package-agnostic `any(...)` predicate.
- No task claims generation decisions, CLI flag wiring, documentation, E2E lifecycle coverage, or initialization refactoring.
