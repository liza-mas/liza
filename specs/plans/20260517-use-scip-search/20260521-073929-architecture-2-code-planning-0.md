# Code Plan: Runtime SCIP Indexing Service

Task ID: `architecture-2-code-planning-0`

Source artifacts:
- Goal spec: `specs/goals/20260517-use-scip-search.md`
- Architecture reference: `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- Existing upstream implementation boundary: `internal/scipsearch`

## Intent

Plan the internal runtime SCIP indexing service so later lifecycle tasks can call one package boundary for target language detection, enabled-language filtering, indexer command execution, worktree-local output paths, private linked-worktree ignore setup, structured results, concurrent worktree isolation, and prompt-safe available-index discovery.

## Source Basis

Based on:
- `specs/goals/20260517-use-scip-search.md`
- `specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md`
- `internal/scipsearch/scipsearch.go`
- `internal/scipsearch/scipsearch_test.go`
- `internal/git/worktree.go`
- `specs/architecture/ADR/README.md`

No assumptions are required.

## Planned Coding Tasks

### Task 1: Runtime SCIP Language Selection and Command Planning

**desc:** Runtime SCIP language selection and command planning: extend `internal/scipsearch` with runtime target-root language detection from git-tracked files, enabled-language filtering against `config.scip_search` through the existing activation contract, deterministic language ordering, `.liza/scip/<language>.scip` output path construction, fixed argv command specifications for `scip-go`, `scip-typescript`, and `scip-python`, and fakeable runtime command-plan seams. Out of scope: executing indexers, linked-worktree ignore setup, lifecycle call-site wiring, prompt wording, README, and Claude settings.

**done_when:** Unit tests in `internal/scipsearch` prove runtime target selection is a no-op when `LIZA_ENABLE_SCIP_SEARCH` is false or `config.scip_search` is empty, includes only languages that are both git-detected in the target root and configured, preserves deterministic `go`, `typescript`, `python` order, constructs absolute `<target>/.liza/scip/<language>.scip` output paths, and produces exact argv/working-directory command plans for `scip-go index --module-root <target> --skip-tests --output <path>`, `scip-typescript index --cwd <target> --output <path> <target>`, and `scip-python index --cwd <target> --output <path>`.

**scope:** In scope: `internal/scipsearch` runtime target selection, reuse of existing `RuntimeEnabled` and git-file detection semantics, deterministic enabled-language filtering, runtime command-plan types, absolute output path construction, and unit tests for selection and command mapping. Out of scope: command execution, index file writes, private gitdir ignore setup, lifecycle call-site wiring, init-time validation, prompt wording, README, Claude settings, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#runtime-scip-indexing-service-internalscipsearch-or-equivalent

**depends_on:** none

### Task 2: Runtime SCIP Refresh Execution and Available Index Discovery

**desc:** Runtime SCIP refresh execution and available index discovery: extend `internal/scipsearch` with a refresh operation that executes selected runtime command plans through a fakeable runner, writes successful index outputs under `<target>/.liza/scip/`, returns structured per-language successes and failures without suppressing other languages, bounds failure diagnostics for logs, and exposes a read-only available-index query that returns only existing successful absolute index paths. Out of scope: linked-worktree private ignore setup, lifecycle call-site wiring, prompt wording, README, and Claude settings.

**done_when:** Unit tests in `internal/scipsearch` prove refresh creates the `.liza/scip` parent directory, invokes the fake runtime runner once per selected language with the exact command plan from Task 1, reports each successful language with an absolute `<target>/.liza/scip/<language>.scip` path, reports a bounded per-language failure when one indexer fails while still returning other language successes, and available-index discovery returns only existing successful absolute paths while omitting missing files and failed languages.

**scope:** In scope: `internal/scipsearch` refresh execution for task-worktree and project-root targets, fakeable runtime runner interface with explicit working directory and argv fields, parent directory creation, structured `RefreshResult`/success/failure data, bounded diagnostics, available-index discovery, and unit tests using fake runners and temp roots. Out of scope: linked-worktree private gitdir exclude mutation, lifecycle call-site wiring, init-time validation, prompt wording, README, Claude settings, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#runtime-scip-indexing-service-internalscipsearch-or-equivalent

**depends_on:** Task 1

### Task 3: Task Worktree Private Ignore and Concurrent Isolation

**desc:** Task worktree private ignore and concurrent isolation: extend `internal/scipsearch` task-worktree refresh to resolve the linked worktree private gitdir, install an idempotent `.liza/scip/` entry in that gitdir's `info/exclude` before writing indexes, avoid the common repository `.git/info/exclude`, and prove concurrent task-worktree refreshes use distinct output files and private exclude files. Out of scope: lifecycle call-site wiring, init-time validation, prompt wording, README, and Claude settings.

**done_when:** Git-backed tests in `internal/scipsearch` prove task-worktree refresh resolves the private linked-worktree gitdir with `git rev-parse --git-dir`, appends `.liza/scip/` exactly once to each task worktree private `info/exclude` before index files are written, never modifies the common repository `.git/info/exclude`, leaves `git status --porcelain` clean after generated task indexes, and concurrently refreshing two linked task worktrees creates distinct absolute index paths with no shared output files or path collisions.

**scope:** In scope: `internal/scipsearch` task-worktree ignore helper, integration of ignore setup into task-kind refresh before index generation, git-backed temp-repository tests for linked worktree private gitdir behavior, clean status, idempotency, common-exclude non-mutation, and concurrent two-worktree isolation. Out of scope: project-root ignore mutation, lifecycle call-site wiring, init-time validation, prompt wording, README, Claude settings, and `.liza/agent-outputs/`.

**spec_ref:** specs/arch-plan/20260517-use-scip-search/20260521-015252-architecture-2.md#runtime-scip-indexing-service-internalscipsearch-or-equivalent

**depends_on:** Task 2

## Dependency Graph

Task 1 -> Task 2 -> Task 3

The chain is intentional because all tasks evolve `internal/scipsearch`, and each later task relies on the runtime types and behavior introduced by the previous task. Lifecycle wiring remains in sibling plans and must not be added here.

## Shared-File Audit

| File/Area | Task(s) | Dependency |
|---|---|---|
| `internal/scipsearch/scipsearch.go` | Task 1 may extend existing constants/helpers; Task 2 may reuse runtime selection; Task 3 may call task-kind refresh behavior | Task 2 depends on Task 1; Task 3 depends on Task 2 |
| `internal/scipsearch/scipsearch_test.go` | Task 1 may add unit coverage beside existing init/activation tests | None |
| `internal/scipsearch/refresh.go` or equivalent new runtime file | Task 1 defines command-plan types; Task 2 implements execution; Task 3 integrates private ignore setup | Task 2 depends on Task 1; Task 3 depends on Task 2 |
| `internal/scipsearch/refresh_test.go` or equivalent tests | Task 1, Task 2, and Task 3 may add scoped tests | Task 2 depends on Task 1; Task 3 depends on Task 2 |
| `internal/scipsearch/gitignore.go` or equivalent helper | Task 3 | None beyond Task 2 behavioral dependency |
| `internal/git/` | No planned production edit; Task 3 may use git commands or existing helpers from tests only | None |
| Lifecycle call sites under `internal/ops`, `internal/agent`, `internal/prompts`, and `cmd/liza` | No task | Out of scope for this plan |
| `.liza/agent-outputs/` | No task | Out of scope |

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Runtime behavior must use `LIZA_ENABLE_SCIP_SEARCH` plus non-empty `config.scip_search` as the strict activation gate. | Configuration Shape; Success Criteria 10; Architecture Constraints | Task 1 | Covered |
| 2 | Runtime service indexes only languages that are both detected in the target root and present in `config.scip_search`. | Configuration Shape semantics; assigned done_when; Architecture Runtime SCIP Indexing Service | Task 1 | Covered |
| 3 | Detection for the first milestone uses git-tracked evidence for Go, TypeScript, and Python. | Behavioral Decisions language detection bullets | Task 1 | Covered |
| 4 | Runtime language processing is deterministic for `go`, `typescript`, and `python`. | Architecture Runtime SCIP Indexing Service key decisions | Task 1 | Covered |
| 5 | Runtime indexer command mapping uses fixed argv arrays for `scip-go`, `scip-typescript`, and `scip-python` with explicit target-root working directories. | MVP Scope first milestone indexers; Indexer Requirements; Architecture Runtime SCIP Indexing Service key decisions | Task 1, Task 2 | Covered |
| 6 | Successful index outputs are written under `<target>/.liza/scip/<language>.scip`. | MVP Scope index storage bullets; Index Storage; assigned done_when | Task 1, Task 2 | Covered |
| 7 | The refresh service returns structured per-language successes and failures. | Architecture Runtime SCIP Indexing Service key decisions; assigned scope | Task 2 | Covered |
| 8 | A failed language indexer does not suppress other successful languages. | Required Agent Prompt Contract; Behavioral Decisions; Success Criteria 23; assigned done_when | Task 2 | Covered |
| 9 | Prompt consumers can discover only existing successful absolute index paths without receiving failure diagnostics. | Required Agent Prompt Contract; Index Storage explicit path semantics; Architecture Prompt Index Availability Boundary | Task 2 | Covered |
| 10 | Task-worktree generated indexes must not dirty `git status --porcelain`. | MVP Scope; Index Storage; Success Criteria 17; assigned done_when | Task 3 | Covered |
| 11 | Task-worktree ignore setup must use the linked worktree private gitdir exclude file and must not edit tracked `.gitignore` or the common repository `.git/info/exclude`. | Index Storage; Architecture Runtime SCIP Indexing Service key decisions; assigned done_when | Task 3 | Covered |
| 12 | Ignore setup must be idempotent. | Architecture Runtime SCIP Indexing Service key decisions; assigned done_when | Task 3 | Covered |
| 13 | Two linked worktrees must resolve distinct private gitdirs. | assigned done_when; Architecture Concurrency concern | Task 3 | Covered |
| 14 | Concurrent task worktrees must have independent index paths without path collisions or shared output files. | MVP Scope worktree safety; Success Criteria 22; Architecture Concurrent task isolation; assigned done_when | Task 3 | Covered |
| 15 | Runtime service must remain stack-agnostic and avoid Liza-specific project commands. | GUARDRAILS.md G1.1; Architecture Constraints | Task 1, Task 2, Task 3 | Covered |
| 16 | Runtime service must expose fakeable command runner seams for tests. | assigned scope; Architecture Testing concern | Task 1, Task 2 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: this plan is an internal runtime service boundary; lifecycle e2e coverage belongs to sibling wiring tasks that call this service. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: operator documentation is covered by merged `architecture-4-code-planning-0`; this task explicitly excludes README, prompt wording, and Claude settings. | N/A |

## Validation Plan

Each coding task should validate its own behavioral surface with focused Go tests:

- Task 1: `go test ./internal/scipsearch -run 'TestRuntime.*(Selection|Command|Plan|Detect)'`
- Task 2: `go test ./internal/scipsearch -run 'TestRuntime.*(Refresh|Available|Failure)'`
- Task 3: `go test ./internal/scipsearch -run 'TestRuntime.*(Worktree|Ignore|Concurrent)'`

The final coding task in this chain should also run `go test ./internal/scipsearch` and pre-commit on touched files. If shared helpers require mechanical updates to existing package documentation, keep those updates inside `internal/scipsearch` and avoid lifecycle packages.

## Pre-Submit Self-Check

- Task decomposition: three coding tasks, each with one observable implementation intent.
- Shared-file audit: all shared `internal/scipsearch` work is serialized with `depends_on`.
- Scope boundaries: no lifecycle call-site wiring, init-time validation, prompt wording, README, Claude settings, automatic installation, or `.liza/agent-outputs/` changes are planned.
- Cross-references: every responsibility named in the compliance matrix is owned by a task heading above.
