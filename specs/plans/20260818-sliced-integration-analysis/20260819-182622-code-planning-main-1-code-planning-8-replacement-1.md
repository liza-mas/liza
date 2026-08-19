# Code Plan: Route Effective Integration Completion Consumers

## Intent and evidence

Route the remaining non-mutating completion consumers and the supervisor's automatic terminal action through the authoritative integration progress, reconciliation, and race-safe terminal-stop contracts. Prompt rendering projects the decision without mutating state, agent wake/restart behavior requests only the reconciliation owner and delegates goal completion to `StopForGoalCompletion`, and status renders the same diagnostics without treating terminal task counts as integration success.

Success means one reviewed plan emits three TDD coding tasks for prompt wake projection, agent wake and supervisor terminal behavior, and read-only status diagnostics; the agent task has a concrete dependency on `code-planning-main-1-terminal-stop-linearization-1-coding-0`; no task owns `internal/ops`; and the final JSON-event validation requires a named `TestEffectiveIntegrationCompletionConsumers` pass from each of `internal/agent`, `internal/prompts`, and `internal/commands` while rejecting every Go failure event.

Based on:

- The full goal specification at `specs/goals/20260818-sliced-integration-analysis.md`, especially Required Properties 11-21 and Success Criteria 4 and 7-10.
- The repaired persistence/progress plan at `specs/plans/20260818-sliced-integration-analysis/20260819-152852-code-planning-main-1-replan-1-code-planning-3.md`, the progression-gate plan at `specs/plans/20260818-sliced-integration-analysis/20260819-161602-code-planning-main-1-code-planning-7.md`, and the approved terminal-stop plan at `specs/plans/20260818-sliced-integration-analysis/20260819-180543-code-planning-main-1-terminal-stop-linearization-1.md`.
- Targeted task-state reads proving concrete coding providers exist for `PROGRESS`, `RECONCILE`, `GATE`, and `TERMINAL-STOP`; in particular, `code-planning-main-1-terminal-stop-linearization-1-coding-0` is the emitted implementation task for `StopForGoalCompletion`.
- ADR-0112; `INVARIANTS.md` §§3.3-3.4, 5, 7-8, 12, 15, and the Protection Matrix; the Update Policy, Open Issues Summary, `Prompts Layer Imports Business Logic`, `Commands Layer Imports Agent Runtime`, and `Integration Closure Is Not Revalidated` sections in `specs/architecture/architectural-issues.md`.
- Stacklit orientation, Semble discovery, and direct source reads of the current completion paths in `internal/agent/workdetection.go`, `internal/agent/systemctl.go`, `internal/prompts/builder.go`, `internal/prompts/wake.go`, `internal/prompts/templates/wake_coding_complete.tmpl`, and `internal/commands/status.go`.
- The superseded consumer-plan task state and reviewer feedback: its ownership and decomposition were coherent, but the supervisor used a check-then-generic-`Stop` sequence and the aggregate predicate did not require the named test in all three packages.

Load-bearing claims:

- **EVIDENCED — authoritative policy:** `PROGRESS` alone derives waiting, requested, blocked, exhausted, stale, and effectively complete states; these consumers must map that decision rather than reproduce its predicates.
- **EVIDENCED — mutation ownership:** `RECONCILE` alone creates or projects slice/global analysis lifecycle work. Prompt and status paths remain read-only, while the agent invokes only the public reconciliation boundary for requested work.
- **EVIDENCED — terminal linearization:** `TERMINAL-STOP` owns `StopForGoalCompletion`, reserved automatic-stop ownership, post-stop verification, and mutation-side invalidation. The agent must not authorize completion and then call generic `Stop`.
- **EVIDENCED — progression compatibility:** `GATE` protects `SPRINT_COMPLETE` checkpoint, resume/archive, advance, and proceed while preserving pre-integration phase handoffs. This plan consumes those outcomes without moving their state-changing checks into agent, prompt, or command packages.
- **EVIDENCED — current divergence:** wake detection, dashboard rendering, post-run verification, automatic stop, and status currently derive completion through terminal-task or resume-result heuristics; the coding-complete template still emits manual `add-tasks` JSON.
- **EVIDENCED — aggregate proof:** comparing the unique package paths attached to named pass events against the exact three-package set rejects the prior false-green case where only one package emitted the named test; `all(.[]; .Action != "fail")` rejects every Go failure event.

Doc Impact: only this plan and its structured task output. The existing goal-level `DOC` task remains responsible for user-visible lifecycle, terminal, and operator documentation after implementation and acceptance evidence exist.

Test Impact: Tasks 1-3 each add or extend the package-local top-level `TestEffectiveIntegrationCompletionConsumers` using TDD. Task 3 also runs the durable three-package aggregate event check. The existing goal-level `E2E` task retains cross-component lifecycle and controlled-concurrency validation.

## Current task routing

| Alias | Concrete task ID | Responsibility |
|---|---|---|
| `PERSIST-BASE` | `code-planning-main-1-replan-1-code-planning-0-coding-0` | Base durable integration lifecycle and analysis evidence |
| `PERSIST-PATCH` | `code-planning-main-1-replan-1-code-planning-3-coding-0` | Settled zero-scope and plural one-lineage approval evidence |
| `PROGRESS` | `code-planning-main-1-replan-1-code-planning-3-coding-1` | Pure authoritative integration progress decision |
| `MUTATE` | `code-planning-main-1-replan-1-code-planning-1-coding-0` | Integration-ref mutation receipts and clean-source verification |
| `PROJECT` | `code-planning-main-1-replan-1-code-planning-2-coding-0` | Immutable analysis verdict projection |
| `RECONCILE` | `code-planning-main-1-replan-1-code-planning-2-coding-1` | Idempotent analysis materialization and blocked/exhausted projection |
| `GATE` | `code-planning-main-1-code-planning-7-coding-0` | State-changing effective-completion precondition |
| `TERMINAL-STOP` | `code-planning-main-1-terminal-stop-linearization-1-coding-0` | Race-safe `StopForGoalCompletion` and mutation-side invalidation |
| Task 1 / `PROMPTS` | output 0 | Read-only effective-completion wake projection |
| Task 2 / `AGENT` | output 1 | Agent wake/restart reconciliation and supervisor terminal behavior |
| Task 3 / `STATUS` | output 2 | Read-only lifecycle status diagnostics and aggregate consumer proof |
| `E2E` | `code-planning-main-1-code-planning-9` | Goal-level sliced lifecycle and finalization-race validation |
| `DOC` | `code-planning-main-1-code-planning-10` | ADR, lifecycle/operator documentation, and issue disposition |

## Architecture and ownership boundary

```text
PROGRESS decision + RECONCILE outcome
                  |
                  v
       Task 1 PROMPTS projection
        /                       \
 wake/dashboard              instructions
        \                       /
                  v
        Task 2 AGENT behavior --------> RECONCILE (requested work only)
                  |
                  +-------------------> TERMINAL-STOP (goal completion only)
                  |
                  v
        Task 3 STATUS diagnostics
                  |
                  v
   three-package named-pass acceptance proof
```

`internal/ops` retains all policy and mutation ownership. Task 1 owns the read-only mapping from authoritative progress and reconciliation outcomes to wake vocabulary. Task 2 owns when that projection wakes the orchestrator, what post-run state is acceptable, and when the supervisor invokes the terminal-stop operation. Task 3 owns only CLI diagnostics over the same projection. No consumer may derive generation eligibility, append tasks, mutate the integration ref, mint automatic-stop ownership, or bypass dependency-owned blocked/exhausted reasons.

The existing `prompts -> ops` read dependency and `commands -> agent` diagnostic dependency are documented architecture tensions. This plan uses their current read paths without adding another layer or moving mutation authority across them. A new query package or structural refactor would exceed the assigned repair scope.

## Consumer protocol

### Prompt wake projection

Task 1 defines one deterministic, read-only projection from the current `IntegrationProgressDecision` and reconciliation result to wake trigger, dashboard context, and instruction text:

1. A requested slice or global analysis renders reconciliation-needed context and instructions that delegate materialization to `ReconcileIntegrationAnalyses`; it never emits manual `add-tasks` JSON.
2. Waiting states render the specific missing barrier without claiming `SPRINT_COMPLETE`.
3. Blocked, exhausted, pipeline-upgrade, malformed, or unavailable states retain the dependency-owned stable reason/context and fail closed.
4. Only effective clean evidence bound to current integration HEAD may project `SPRINT_COMPLETE`.
5. Repeated rendering of unchanged state returns equivalent ordered context and performs no blackboard, task, sprint, mode, or Git mutation.

Preserve the priority and behavior of initial planning, blocked tasks, hypothesis exhaustion, immediate discoveries, partial planning handoff, many-to-one readiness, and other non-integration triggers.

### Agent wake, restart, and supervisor terminal behavior

Task 2 consumes Task 1's projection and the public dependency interfaces:

1. All-terminal ineffective integration state wakes for the projected reconciliation request or stable blocked/exhausted handling rather than `SPRINT_COMPLETE`.
2. The agent invokes only `ReconcileIntegrationAnalyses` for requested lifecycle work. Repeated wake and restart evaluation relies on its deterministic keys and idempotency; no agent code appends analysis tasks directly.
3. Post-run verification accepts durable requested analysis projection or the explicit blocked/exhausted outcome. It does not require a legacy single `integration-pair` task or a terminal checkpoint when integration remains ineffective.
4. Auto-resume retains `GATE`-protected `Resume`. When resume reports no carried work or transition, the supervisor invokes `StopForGoalCompletion`; only success from that operation returns the clean goal-complete sentinel.
5. Waiting, stale, blocked, exhausted, malformed, unavailable, or terminal-stop precondition failure never becomes a successful goal-complete stop. Generic `Stop` remains reserved for operator, safety, and circuit-breaker paths outside this goal-completion branch.

Task 2 does not recreate the terminal-stop protocol or inspect its reserved ownership token. The concrete dependency on `TERMINAL-STOP` guarantees the public race-safe operation exists before this agent consumer becomes claimable.

### Read-only status diagnostics

Task 3 reuses the authoritative agent wake projection and renders it in dashboard, JSON, and YAML status:

1. Requested, waiting, stale, blocked, exhausted, and unavailable decisions render stable diagnostic context without invoking reconciliation or any other mutation.
2. Terminal task counts remain queue/sprint facts, not integration completion proof.
3. `SPRINT_COMPLETE` appears only for effective clean evidence bound to current integration HEAD.
4. Existing agent health, capacity, work queues, phase handoff, checkpoint, and non-integration trigger reporting remains unchanged.

## Deterministic TDD proof

Write each package's failing top-level `TestEffectiveIntegrationCompletionConsumers` before implementation. Reuse dependency-owned progress fixtures and public boundaries; do not duplicate evaluator predicates in test helpers.

Task 1 (`internal/prompts`) proves:

- requested work renders reconciliation instructions without manual `add-tasks` JSON;
- waiting, stale, blocked, exhausted, and unavailable decisions omit `SPRINT_COMPLETE` and preserve stable reason/context;
- current-HEAD clean evidence alone renders `SPRINT_COMPLETE`;
- repeated rendering is byte-stable and leaves state deeply unchanged;
- existing non-integration wake priority remains intact.

Task 2 (`internal/agent`) proves:

- all-terminal ineffective state produces Task 1's requested or blocked/exhausted wake result;
- repeated wake and simulated restart calls create at most the deterministic analysis membership owned by `RECONCILE`;
- post-run verification accepts requested analysis projection or explicit blocked/exhausted closure and rejects false terminal success;
- auto-resume goal completion calls `StopForGoalCompletion` and never generic `Stop` for that branch;
- stale, waiting, blocked, exhausted, malformed, unavailable, and terminal-stop precondition failures do not return clean goal completion, while current-HEAD clean success does.

Task 3 (`internal/commands`) proves:

- status reports requested/waiting/stale context and preserves dependency-owned blocked/exhausted diagnostics;
- ineffective or unavailable evidence never reports `SPRINT_COMPLETE`, while current-HEAD clean evidence does;
- status output causes no lifecycle, task, sprint, mode, or Git mutation;
- existing non-integration status fields and trigger descriptions remain intact.

Use deterministic fixtures and bounded synchronization already owned by the dependency tests. No sleeps, timing-only assertions, mocked progress rules, direct state-file edits, or retry-until-green loops.

## Validation contract

Task-local validations require the named top-level pass and reject every Go failure event. After Task 3's dependencies merge, its aggregate validation additionally compares the distinct package paths for named pass events against the exact expected set:

`go test -json ./internal/agent ./internal/prompts ./internal/commands -run '^TestEffectiveIntegrationCompletionConsumers$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and .Test == "TestEffectiveIntegrationCompletionConsumers") | .Package] | unique | sort) == ["github.com/liza-mas/liza/internal/agent","github.com/liza-mas/liza/internal/commands","github.com/liza-mas/liza/internal/prompts"] and all(.[]; .Action != "fail")'`

This predicate fails when any package omits the named test, when a named test does not pass, or when any package/test/build event fails.

## Planned coding tasks

### Task 1 — Project authoritative integration progress into prompts

Description: Project authoritative integration progress into idempotent orchestrator wake instructions.

Done when: `TestEffectiveIntegrationCompletionConsumers` in `internal/prompts` proves ineffective completion renders reconciliation-needed or stable blocked/exhausted output without `SPRINT_COMPLETE`; the coding-complete template delegates analysis creation to `ReconcileIntegrationAnalyses` instead of emitting manual `add-tasks` JSON; repeated rendering preserves the same deterministic request context without mutation; and `SPRINT_COMPLETE` appears only for clean evidence whose immutable source equals current integration HEAD.

Scope: Own `internal/prompts/builder.go`, `internal/prompts/builder_test.go`, `internal/prompts/wake.go`, and `internal/prompts/templates/wake_coding_complete.tmpl`. Define the sole read-only mapping from `IntegrationProgressDecision` and reconciliation outcomes to wake trigger, dashboard diagnostics, and instructions; preserve non-integration trigger priority. Do not create analysis tasks, derive progress or generation rules, stop the system, mutate the integration ref, change operations-layer files, or change role context outside these files.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependencies: concrete `PROGRESS` task `code-planning-main-1-replan-1-code-planning-3-coding-1` and concrete `RECONCILE` task `code-planning-main-1-replan-1-code-planning-2-coding-1`.

Validation: `go test -json ./internal/prompts -run '^TestEffectiveIntegrationCompletionConsumers$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionConsumers") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/prompts/builder.go, internal/prompts/builder_test.go, internal/prompts/wake.go, internal/prompts/templates/wake_coding_complete.tmpl]`; `owned_modules=[internal/prompts]`; `read_only_depends_on=[]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-3-coding-1,code-planning-main-1-replan-1-code-planning-2-coding-1]`; `interfaces_owned=[effective-completion wake projection]`; `interfaces_consumed=[EvaluateIntegrationProgress, IntegrationProgressDecision, ReconcileIntegrationAnalyses]`; coverage: one pure presentation mapping replaces terminal-count success and manual analysis-task JSON.

### Task 2 — Make agent completion behavior authoritative

Description: Make agent wake, restart, and supervisor terminal behavior consume authoritative integration completion.

Done when: `TestEffectiveIntegrationCompletionConsumers` in `internal/agent` proves all-terminal ineffective states project reconciliation-needed or stable blocked/exhausted wake behavior without duplicate analysis membership; post-run verification accepts only the requested analysis projection or explicit blocked/exhausted result; auto-resume delegates goal completion exclusively to `StopForGoalCompletion` and never generic `Stop`; and clean goal completion occurs only when that operation succeeds for clean evidence bound to current integration HEAD.

Scope: Own `internal/agent/workdetection.go`, `internal/agent/workdetection_test.go`, `internal/agent/systemctl.go`, and `internal/agent/systemctl_test.go`. Consume Task 1's wake projection, invoke only public `ReconcileIntegrationAnalyses` for requested work, and invoke the dependency-owned public `StopForGoalCompletion` for automatic goal completion. Preserve source compatibility for detector callers, existing non-integration wake priority, pre-integration handoffs, and operator/safety stop behavior. Do not append analysis tasks directly, recreate progress, generation, gate, or terminal-stop rules, inspect automatic-stop tokens, mutate the integration ref, change operations-layer files, or alter non-orchestrator pause behavior.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependencies: Task 1; concrete `PROGRESS` task `code-planning-main-1-replan-1-code-planning-3-coding-1`; concrete `RECONCILE` task `code-planning-main-1-replan-1-code-planning-2-coding-1`; and concrete `TERMINAL-STOP` task `code-planning-main-1-terminal-stop-linearization-1-coding-0`.

Validation: `go test -json ./internal/agent -run '^TestEffectiveIntegrationCompletionConsumers$' -count=1 | jq -e -s 'any(.[]; .Action == "pass" and .Test == "TestEffectiveIntegrationCompletionConsumers") and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/agent/workdetection.go, internal/agent/workdetection_test.go, internal/agent/systemctl.go, internal/agent/systemctl_test.go]`; `owned_modules=[internal/agent]`; `read_only_depends_on=[0]`; `read_only_task_depends_on=[code-planning-main-1-replan-1-code-planning-3-coding-1,code-planning-main-1-replan-1-code-planning-2-coding-1,code-planning-main-1-terminal-stop-linearization-1-coding-0]`; `interfaces_owned=[authoritative agent integration wake behavior, supervisor terminal decision]`; `interfaces_consumed=[effective-completion wake projection, EvaluateIntegrationProgress, ReconcileIntegrationAnalyses, StopForGoalCompletion]`; coverage: all agent-side wake, restart, verification, and automatic goal-stop consumers fail closed through approved public interfaces.

### Task 3 — Report authoritative integration lifecycle status

Description: Report authoritative integration lifecycle status without deriving success from terminal task counts.

Done when: `TestEffectiveIntegrationCompletionConsumers` in `internal/commands` proves status reports deterministic reconciliation-needed context and preserves dependency-owned blocked/exhausted reasons without mutation; ineffective or unavailable evidence never reports `SPRINT_COMPLETE`; clean evidence reports `SPRINT_COMPLETE` only when its immutable source equals current integration HEAD; and the aggregate JSON-event check observes the named test pass from all three consumer packages with no Go failure event.

Scope: Own `internal/commands/status.go` and `internal/commands/status_test.go`. Reuse Task 2's authoritative agent diagnostic surface and Task 1's wake vocabulary to render read-only integration lifecycle status in dashboard, JSON, and YAML; preserve existing agent, queue, checkpoint, phase-handoff, and non-integration trigger reporting. Do not call reconciliation as a mutation, derive progress or generation rules, stop or resume the system, mutate the integration ref, change operations-layer files, or change CLI command wiring.

Spec ref: `specs/goals/20260818-sliced-integration-analysis.md#required-properties`

Dependency: Task 2, which transitively includes Task 1 and all authoritative provider dependencies.

Validation: `go test -json ./internal/agent ./internal/prompts ./internal/commands -run '^TestEffectiveIntegrationCompletionConsumers$' -count=1 | jq -e -s '([.[] | select(.Action == "pass" and .Test == "TestEffectiveIntegrationCompletionConsumers") | .Package] | unique | sort) == ["github.com/liza-mas/liza/internal/agent","github.com/liza-mas/liza/internal/commands","github.com/liza-mas/liza/internal/prompts"] and all(.[]; .Action != "fail")'`

Decomposition: `owned_files=[internal/commands/status.go, internal/commands/status_test.go]`; `owned_modules=[internal/commands]`; `read_only_depends_on=[1]`; `read_only_task_depends_on=[]`; `interfaces_owned=[integration lifecycle status projection, three-package completion-consumer acceptance proof]`; `interfaces_consumed=[authoritative agent integration wake behavior, effective-completion wake projection]`; coverage: status remains read-only and the final child provides the durable distinct-package named-pass gate.

## Architecture assessment

### Discovery

The current completion signal is reconstructed independently at five consumer points: agent wake detection, prompt dashboard wake selection, prompt instruction rendering, supervisor auto-resume stopping, and command status. The stable providers are the pure progress decision, idempotent reconciliation, state-changing progression gate, and terminal-stop operation. The volatile concern is presentation and control adaptation in the three assigned packages.

Dependency direction stays consistent with the current architecture: `prompts` reads `ops`, `agent` consumes `prompts`/`ops`, and `commands` reads the agent diagnostic surface. Mutations remain in `ops`. The tasks are serialized only where they consume a newly defined sibling interface; no owned file appears in multiple tasks.

### Analysis and recommendation

| Question | Assessment |
|---|---|
| Problem | Terminal task counts and resume-result heuristics can project success while integration evidence is waiting, stale, blocked, exhausted, or racing with HEAD mutation. |
| Cost of error | High: a false wake or status misleads operators; a generic automatic stop can halt re-analysis after stale completion. |
| Failure handling | Prompt and status fail closed with stable diagnostics; agent reconciliation is idempotent; terminal-stop precondition failures keep the supervisor non-terminal. |
| Concurrency | Consumers do not linearize HEAD or mode themselves. `TERMINAL-STOP` owns the race, while repeated wake/restart calls rely on `RECONCILE` deterministic identity. |
| Data ownership | `PROGRESS` owns truth, `RECONCILE` owns analysis mutation, `GATE` owns progression, `TERMINAL-STOP` owns automatic stop, and Tasks 1-3 own only their consumer adaptations. |
| Boundaries | No task touches `internal/ops`, durable schemas, pipeline topology, integration refs, generation policy, or documentation. |
| Reversibility | Three package-local TDD changes consume approved interfaces and can be reverted independently in dependency order. |

Considered alternatives:

1. Keep terminal-count detection and add local guards: rejected because it preserves divergent policy and duplicates the authoritative decision.
2. Let prompt/status call reconciliation: rejected because both are presentation paths and must remain read-only.
3. Recheck progress in agent and call generic `Stop`: rejected because it recreates the reviewed TOCTOU defect.
4. Consume `StopForGoalCompletion` and serialize only the real interface dependencies: selected because it preserves ownership and closes the repair task without operations-layer scope.

Three tasks are appropriate. Prompt projection, agent runtime behavior, and CLI status are distinct observable changes in different packages. Task 2 depends on Task 1 because it consumes the new projection; Task 3 depends on Task 2 because it reuses the agent diagnostic surface and is the aggregate acceptance gate. Splitting implementation from tests would violate TDD colocation; combining all packages would exceed the decomposition threshold.

No new architecture issue is introduced. The plan deliberately uses the existing documented `prompts -> ops` and `commands -> agent` read paths, does not worsen them with mutation authority, and contributes to the existing `integration-closure-is-not-revalidated` correction. Only `DOC` may revise or resolve that issue after goal-level evidence exists.

## Spec Compliance Matrix

Task 1, Task 2, and Task 3 are this plan's outputs. Other aliases identify retained goal-level owners and do not expand this plan's implementation scope.

| # | Requirement | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Partial planning handoff does not open integration coverage. | Required Property 1 | `PROGRESS`; `RECONCILE`; Task 2; `E2E` | Covered |
| 2 | The contributing plan set is evaluated exactly once only after every planning source, eligible output/transition, and resulting coding task settles. | Required Property 2 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 3 | Fewer than two contributing scopes produce no slice analyses. | Required Property 3 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 4 | Multiple contributing scopes each contribute bounded local coverage. | Required Property 4 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 5 | One-lineage scopes reuse coding-review approval attestations and produce no slice. | Required Property 5 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 6 | Multi-lineage scopes with merged work produce exactly one slice analysis. | Required Property 6 | `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 7 | Integration-escalation plans remain repair work outside the contributing set and create no slices. | Required Property 7 | `PERSIST-BASE`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 8 | Task lineage identifies coding, fix, and replacement tasks belonging to each slice. | Required Property 8 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 9 | Each slice receives a bounded surface attributable to its originating plan. | Required Property 9 | `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 10 | Each slice verdict records descendant changes and immutable source snapshot. | Required Property 10 | `PERSIST-BASE`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 11 | Global analysis waits for settled planning, terminal coding/repair work, complete required coverage, and resolved slices; missing or unresolved work blocks. | Required Property 11 | `PROGRESS`; `RECONCILE`; `GATE`; Tasks 1-3; `E2E` | Covered |
| 12 | Global analysis independently inspects the aggregate branch. | Required Property 12 | `PROJECT`; `E2E` | Covered |
| 13 | Global fixes and later integration-HEAD mutations trigger another scan while budget remains. | Required Property 13 | `PROGRESS`; `MUTATE`; `RECONCILE`; Tasks 1-2; `E2E` | Covered |
| 14 | Slice exhaustion and global-generation exhaustion produce explicit blocked outcomes. | Required Property 14 | `PROGRESS`; `RECONCILE`; Tasks 1-3; `E2E` | Covered |
| 15 | Clean completion is tied to an immutable reviewed commit. | Required Property 15 | `PERSIST-BASE`; `PROGRESS`; `PROJECT`; `GATE`; `TERMINAL-STOP`; Tasks 1-3; `E2E` | Covered |
| 16 | Completion state, clean reviewed commit, and integration HEAD have one linearizable relationship under concurrent mutation. | Required Property 16 | `PROGRESS`; `MUTATE`; `GATE`; `TERMINAL-STOP`; Task 2; `E2E` | Covered |
| 17 | The integration-HEAD mutation path invalidates completion tied to a superseded HEAD. | Required Property 17 | `MUTATE`; `TERMINAL-STOP`; `E2E` | Covered |
| 18 | Finalization preserves ADR-0112 lock ordering and performs no blackboard write under the mutation lock. | Required Property 18 | `MUTATE`; `GATE`; `TERMINAL-STOP`; `E2E`; `DOC` | Covered |
| 19 | The global generation limit is configurable with a deterministic default. | Required Property 19 | `PROGRESS`; `RECONCILE`; `E2E`; `DOC` | Covered |
| 20 | Wake evaluation and restart recovery create no duplicate slice or global analyses. | Required Property 20 | `PROGRESS`; `RECONCILE`; Tasks 1-2; `E2E` | Covered |
| 21 | Workflow remains stack-agnostic and preserves review and merge authorization boundaries. | Required Property 21 | All providers retain declared authority; Tasks 1-3 add no project command default | Covered |
| 22 | Coverage cannot begin while a planning source, output, transition, or resulting coding task remains unsettled. | Success Criterion 1 | `PROGRESS`; `RECONCILE`; Task 2; `E2E` | Covered |
| 23 | Cohort classification and zero-slice bypass are reproducible. | Success Criterion 2 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `RECONCILE`; `E2E` | Covered |
| 24 | Every multi-scope cohort member has an attestation or exactly one required slice. | Success Criterion 3 | `PERSIST-BASE`; `PERSIST-PATCH`; `PROGRESS`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 25 | No global analysis becomes claimable while any local barrier is unsettled, missing, unresolved, or blocked. | Success Criterion 4 | `PROGRESS`; `RECONCILE`; `GATE`; Tasks 1-3; `E2E` | Covered |
| 26 | Every slice records a bounded surface and immutable snapshot. | Success Criterion 5 | `PERSIST-BASE`; `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 27 | Global analysis independently reviews the aggregate after local coverage and slice resolution. | Success Criterion 6 | `PROJECT`; `RECONCILE`; `E2E` | Covered |
| 28 | Successful integration linearizes only when clean reviewed commit equals integration HEAD and completion is successful. | Success Criterion 7 | `PROGRESS`; `MUTATE`; `GATE`; `TERMINAL-STOP`; Tasks 1-3; `E2E` | Covered |
| 29 | Controlled concurrency proves both mutation/finalization orders never leave stale successful completion. | Success Criterion 8 | `MUTATE`; `TERMINAL-STOP`; `E2E` | Covered |
| 30 | Later mutations reanalyze within budget and block explicitly after exhaustion. | Success Criterion 9 | `PROGRESS`; `MUTATE`; `RECONCILE`; `TERMINAL-STOP`; Tasks 1-3; `E2E` | Covered |
| 31 | Repeated wake evaluation and restart recovery remain duplicate-free. | Success Criterion 10 | `PROGRESS`; `RECONCILE`; Tasks 1-2; `E2E` | Covered |
| 32 | No master-planning change, fix-review replacement, global-analysis removal, stack-specific validation default, or new role is introduced. | Out of Scope | Tasks 1-3 retain assigned consumer boundaries | Covered |
| 33 | ADR-0113 extends ADR-0055 and supersedes its no-rescan limitation. | Documentation Impact 1-2 | `DOC` | Covered |
| 34 | State-machine and task-lifecycle documentation describes the new lifecycle. | Documentation Impact 3 | `DOC` | Covered |
| 35 | Pipeline and operational documentation covers barriers, generations, and terminal outcomes. | Documentation Impact 4 | `DOC` | Covered |
| 36 | The integration-closure issue changes only after implementation and validation evidence exists. | Documentation Impact 5 | `E2E`; `DOC` | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | `E2E` (`code-planning-main-1-code-planning-9`); package-level consumer integration is colocated in Tasks 1-3 | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | `DOC` (`code-planning-main-1-code-planning-10`); no duplicate documentation task in this repair scope | Covered |

## Pre-submit audit

- Atomicity: three outputs each own one observable consumer adaptation and its colocated TDD proof.
- Provider ordering: Task 1 names concrete `PROGRESS` and `RECONCILE`; Task 2 depends on Task 1 and names concrete `PROGRESS`, `RECONCILE`, and `TERMINAL-STOP`; Task 3 depends on Task 2.
- Terminal repair closure: Task 2 invokes the approved public `StopForGoalCompletion` interface and does not own or alter any operations-layer file.
- Shared-file audit: no owned file appears in more than one output; the two sibling dependency edges represent interface consumption rather than collision avoidance.
- Policy boundary: progress, generation, reconciliation, mutation, progression gate, and terminal-stop semantics remain dependency-owned.
- Validation: Task 3's exact aggregate predicate requires the named test pass from all three package paths and rejects every Go failure event.
- Cross-references: every alias is bound in Current task routing and credited only for its declared responsibility.
- Compliance: every Required Property, Success Criterion, constraint, E2E impact, and documentation impact is covered with no GAP.
