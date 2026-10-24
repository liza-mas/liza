# Code Plan: D53 Oracle Portability Corrections

## Source References
Source revision: "6ef124d3e78667f8bf555854e499ad81cd0f1b98"

### Direct References
- "d53": "specs/goals/20261023-architecture-consolidation-decisions.md#D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "d52": "specs/goals/20261023-architecture-consolidation-decisions.md#D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "d24": "specs/goals/20261023-architecture-consolidation-decisions.md#D24 — Provenance of goldens changed by a C task (run, 2026-10-23)" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "goal-nfr": "specs/goals/20261023-architecture-consolidation.md#Non-Functional Constraints" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "goal-docs": "specs/goals/20261023-architecture-consolidation.md#Documentation" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "env-safe": "GUARDRAILS.md#G1.4: Environment-safe tests and diagnostics" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "adr-0219": "specs/architecture/ADR/0219-oracle-harness-and-chained-provenance.md#219 - Oracle Harness and Chained Provenance" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "boundary": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Area-Boundary Checkpoint Contract" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "a2-arch": "specs/arch-plan/20261023-architecture-consolidation/20261023-182300-arm-1-ar-2.md#Constraints" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "a3-arch": "specs/arch-plan/20261023-architecture-consolidation/20261023-183344-arm-1-ar-3.md#Constraints" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "a7-arch": "specs/arch-plan/20261023-architecture-consolidation/20261023-181043-arm-1-ar-7.md#Constraints" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "a9-arch": "specs/arch-plan/20261023-architecture-consolidation/20261023-181325-arm-1-ar-9.md#Constraints" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"
- "a4-arch": "specs/arch-plan/20261023-architecture-consolidation/20261023-182810-arm-1-ar-4.md#Scope 1: A4 oracle suite" @ "6ef124d3e78667f8bf555854e499ad81cd0f1b98"

### Obligation Coverage
- "N-store" -> "d53", "adr-0219", "goal-tests"
- "N-collide" -> "d53", "adr-0219"
- "N-rename" -> "d53", "goal-tests", "a2-arch"
- "N-guard" -> "d53"
- "N-provenance" -> "d24", "adr-0219", "goal-proof"
- "N-doc" -> "d53", "adr-0219", "goal-docs"
- "N-proof" -> "goal-proof", "env-safe", "d52", "boundary"
- "H-depth" -> "d53", "d52"
- "H-guard" -> "d53"
- "H-proof" -> "d53", "goal-proof", "d24"
- "H-env" -> "goal-proof", "env-safe", "d52", "boundary"
- "L-a9" -> "d53", "a9-arch", "goal-tests"
- "L-select" -> "d53", "goal-nfr"
- "L-stub" -> "d53", "goal-tests", "a9-arch"
- "L-provenance" -> "d24", "adr-0219", "goal-proof"
- "L-preserve" -> "d53", "goal-tests", "goal-frozen"
- "L-proof" -> "goal-proof", "env-safe", "d52", "boundary"
- "A4-cause" -> "d53", "goal-frozen", "a4-arch"
- "A4-scope" -> "d53", "a4-arch", "goal-tests"
- "A4-select" -> "d53", "goal-nfr"
- "A4-provenance" -> "d24", "adr-0219", "goal-proof"
- "A4-doc" -> "d53", "a2-arch", "a3-arch", "a7-arch", "a9-arch", "a4-arch", "goal-docs"
- "A4-preserve" -> "d53", "goal-tests", "goal-frozen"
- "A4-proof" -> "goal-proof", "env-safe", "d52", "boundary"

## Intent and Allocation

Based on: D53/D52/D24 and the goal sections above; CI run 40105658735 job logs read with
`gh run view 40105658735 -R liza-mas/liza --job <id> --log` (jobs 120906696449 Windows ops-1,
120906696782 Linux tests, 120906696655 macOS); reads at `6ef124d3e` of
`internal/oracle/a2/cli/helpers_test.go:151-204`, `internal/testhelpers/oracle/golden.go`,
`normalise.go:18-80`, `cmd/regen/{regenerate,materialise,history,compare,ledger}.go`,
`correction_start_test.go:20-70`, `internal/oracle/PROVENANCE`, `.github/workflows/ci-core.yml:20-200`,
`ci.yml:1-40,80-110,225-260`, `Makefile` CI/lint targets, `internal/oracle/a9/{doc,kit,stub,lifecycle}_test.go`
(sections), `internal/oracle/a4/{dependencies,main,case,conventions}_test.go` (sections),
`internal/procscan/{process_status,liza_agents,enumerate_unix}.go`, `internal/ops/{agent_process_status,delete_agent,delete_agent_process_unix}.go`,
`internal/commands/validate.go:40-190`, `cmd/liza/cmd_init.go` validate command, `internal/jsonout/envelope.go`,
ADR-0219, the A2/A3/A4/A7/A9 oracle architecture constraints, targeted task queries, and the Reality
Probes below. Diagnostics ran only in disposable clones under `/tmp` with
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=…`.

Success means four Must coding outputs that together remove every D53 failure class without changing a
fixture, golden content or asserted value: (1) filesystem-safe golden storage names, (2) full CI
history for test-running checkouts, (3) A9 off-Linux correctness, (4) the A4 difference with the
oracle proof-scope document. Validate this plan by payload preflight, reference and Acceptance
Contract parity, and independent plan review. Doc Impact: this plan, its output JSON, and the
children's manifests; Task 1 owns the storage-naming doc and the ADR-0219 clause; Task 4 owns the
proof-scope doc; the boundary record stays with `arm-1-ar-11-cp-0-code-0`. Test Impact: none for
planning; each child owns its regression and proof.

| Task | Intent | D53 bullet | Why a separate unit |
|---|---|---|---|
| 1 | Store goldens under filesystem-safe names on every OS, with a tracked-path guard | Generic fix 1 | Harness provider change; independent of the others |
| 2 | Fetch full history in every test-running CI checkout, with a workflow guard | Generic fix 2 | CI configuration; no oracle file |
| 3 | A9 off Linux: Linux-scope its process-facility scenarios; fix the stub's temp-path comparison | Linux scope; generic fix 3 | One package (`internal/oracle/a9`); both needed for A9 on macOS |
| 4 | A4 difference: characterize its procfs cause, Linux-scope the scenario, document the oracle proof scope | Generic fix 3 → Linux scope (RCA F4) | A4 package plus a white-box characterization; the doc names Task 3's delivered scope |

All four are **Must** (D53 orders them before the A1 boundary's G8). No Should/Could work exists.
Provider Ask Inventory: N/A — no shared interface is frozen; the harness mapping is internal to
`internal/testhelpers/oracle` and transparent to every suite.

**Commitment C-A4 (owner: this plan; settles at plan review).** D53 lists A4 under "not caused by
a Linux process facility … investigated and fixed as a defect or determinism issue". The
investigation (RCA F4) establishes that the difference *is* caused by a Linux process facility
(procfs absence makes `validate` add a designed warning), so D53's Linux-scope bullet governs and no
new ruling or production change is planned. If review rejects that classification, the exact
unresolved human decision is: *scope `repair-superseded-dependencies/success` to Linux under D53
(procfs), or change the frozen `validate --json` warning behaviour on hosts without procfs (outside
the C1–C6 closed list)*. Task 4 must then mark BLOCKED with that question rather than implement.

## Root Cause Analysis

### a. Evidence

Runtime evidence is the CI log of run 40105658735 at `1e3b803b75f396ec4f695359811fbf32a5f3310c`
(`headSha` from `gh run view 40105658735 -R liza-mas/liza --json headSha,conclusion,jobs`), whose
failing steps were: all seven Windows shards at `actions/checkout`; Linux tests at "Run race,
integration, and Python tests"; macOS at "Run unit and integration tests in parallel"; Linux lint
and build succeeded.

- **F1 Windows checkout.** Job 120906696449 log line 109: `##[error]error: invalid path
  'internal/oracle/a2/cli/testdata/golden/artifact/tool-result-filter/large/store/<ID>.txt'`, then
  git exit 128 (line 120). `git ls-files | grep -E '[<>:"|?*\\]'` lists exactly four tracked
  paths: `…/tool-result-filter/{large,redaction}/store/<ID>.txt` and
  `…/{large,redaction}/store/events/<EVENT-1>.json` (RP-1). No tracked path has a control
  character, trailing dot/space, reserved device name, or case-only collision (RP-1).
- **F2 shallow history.** Linux job 120906696782 log: `TestProviderCorrectionA8Start`
  (`correction_start_test.go:23`), `TestProviderCorrectionA9Start` (`fixed_sha_test.go:228`),
  `TestProviderCorrectionA5Start` (`prepared_project_test.go:223`) fail with `export immutable
  starting tree: exit status 128`; `TestRegenNamesAlteredSampleGolden` (`regenerate_test.go:117`)
  fails with `error worktree 96c9ad69a…: … fatal: invalid reference: 96c9ad69a…`. The macOS job
  shows the same four failures. The five `actions/checkout` steps (`ci-core.yml:34,110,179`,
  `ci.yml:93,236`) set only `persist-credentials: false`.
- **F3 A9 on macOS.** Job 120906696655: `TestOracleA9AuthorityLost` `lifecycle_test.go:124: A9
  i05-authority-lost run: agent exit deadline (10s)`; `TestOracleA9Stub` `stub_test.go:89:
  invocation record lost executable/argv/cwd`; `TestOracleA9RegistrationCollision`
  (golden `human/i01-collision/agent.txt` line 6) and `TestOracleA9AutoAssignID/capacity-refused`
  (golden `human/i02-refused/agent.txt` line 7): golden `raw state mismatched from procfs (pid
  exists but cmdline doe…`, actual `raw state unknown from signal(0) (process is alive; procfs u…`.
  Every other A9 test passed on macOS.
- **F4 A4 on macOS.** Same job: `TestOracleRepairSupersededDependencies/success`
  `dependencies_test.go:51: … success_validate_after.json (kind json) differs at line 7: golden "    }"
  actual "    },"`. The committed golden is the 9-line envelope `{"exit_code":0,"stdout":{"ok":true,
  "result":{"valid":true}}}` in canonical form.
- **Suite coverage.** macOS reported every package Linux did except `internal/oracle/a2/{artifact,cli}`,
  whose files all carry `//go:build linux` (A2 AC3, `a2-arch`). A3 and A7 oracle tests skip off
  Linux (OC3/TC1, `a3-arch`, `a7-arch`); A4 and A5 skip on Windows; A9 skips on Windows through
  `unixOnly` (`kit_test.go:127-132`, `a9-arch` AC4).

### b. Mechanism

- **F1.** `fileEffects` registers the event file stem as literal `<EVENT-1>`
  (`helpers_test.go:180-182`), normalises the inventory key, which also maps 40/64-hex IDs to
  `TokenID` `<ID>` (`normalise.go:24,32`), and passes `name+"/"+key` to `Check`
  (`helpers_test.go:183,202`). `Check` → `compareGolden` → `goldenPath` (`golden.go:20-35,68-94`)
  joins the logical name verbatim, so write mode created, and live mode reads, files whose names
  contain `<`/`>`. Windows forbids those characters, so checkout of the whole tree fails before any
  test runs; Windows would never read them (A2 is Linux-only).
- **F2.** The starting-tree consumers need object `96c9ad69a`: `git archive … correctionStart`
  (`correction_start_test.go:48-53`) and `git worktree add --detach dir <start>` (`materialise.go:57`,
  start from `PROVENANCE`). With checkout's default depth 1 (D53 attribution; no `fetch-depth`
  key is present) only the tested commit exists. `96c9ad69a` is an ancestor of HEAD
  (`git merge-base --is-ancestor`, 353 commits, RP-3), so full history resolves it.
- **F3a wording (I-01 collision, I-02 refusal).** `AgentProcessStatusForPID` reads
  `/proc/<pid>/cmdline` (`process_status.go:88-106`); without procfs and without a native command
  line it falls to `signalProcessStatus` (`:132-137`), and `AgentProcessObservation.Diagnostic`
  (`agent_process_status.go:58-66`) prints the raw state and source the goldens freeze. Linux
  process facility (procfs).
- **F3b authority fence (I-05).** `delete agent --force` signals the supervisor only if
  `isLizaAgentProcess` confirms it (`delete_agent.go:129-137`); on Unix that reads
  `/proc/<pid>/cmdline` (`delete_agent_process_unix.go:19-25`). Without procfs the supervisor is not
  signalled and the scenario's 10 s exit deadline expires. Linux process facility (process
  supervision). Source-traced; not reproduced (see d).
- **F3c stub cwd.** The stub records `os.Getwd()` (`stub_test.go:137`); the kit's project dir is
  the unresolved `t.TempDir()` path (`kit_test.go:137-138`). The assertion compares them as strings
  (`stub_test.go:88`). When `TMPDIR` contains a symlink (macOS `/var/folders` → `/private/var`), the
  recorded physical path differs from the logical one. Not a Linux process facility: a temp-path
  determinism defect of the assertion. Goldens are unaffected because the harness registers both
  forms of every directory (`normalise.go:58-76`).
- **F4.** A success `--json` envelope is `ok`, `result`, then `warnings` (omitempty), then `error`
  (`envelope.go:10-15`), so the only element that can follow `result` in a success document is
  `warnings`. `validate --json` collects warning lines and emits them (`cmd_init.go:373-397`).
  After the repair the state is valid, so `ValidateLoadedState` reaches `validateNoZombieAgents`
  (`validate.go:100-110`); `FindZombieAgents` finds no `/proc` and falls back to the native scan
  (`liza_agents.go:69-75`), which on non-Windows Unix returns `ErrProcessScanUnavailable`
  (`enumerate_unix.go:11-13`); `validate.go:167-169` then writes `WARNING: Live liza agent process
  scan skipped (procfs unavailable on this host)`. The pre-repair validation fails in
  `statevalidate` before process checks, which is why only `_after` differs. The warning is designed
  host-local behaviour (`validate.go:47-50`), not a defect: the golden freezes Linux procfs output.

### c. Reproduction

- **F1:** `git ls-files | grep -E '[<>:"|?*\\]'` prints the four paths (RP-1); Task 1's guard
  turns this state into its failing test.
- **F2:** a `git clone --depth 1` of HEAD reproduces exactly the four CI failures; after
  `git fetch --unshallow` the same command passes (RP-3). Task 2's guard fails on the four checkouts
  lacking `fetch-depth: 0`.
- **F3c:** `TMPDIR=/tmp/d53-link` (symlink to `/tmp/d53-real`) reproduces `stub_test.go:89` on
  Linux; with diagnostics the record shows `dir="/tmp/d53-real/…/project"` vs
  `want="/tmp/d53-link/…/project"`, executable and args equal; plain `TMPDIR=/tmp` passes (RP-4).
  Task 3's done_when requires this reproduction red before and green after the edit.
- **F4:** `liza validate --json` on the post-repair D35 project prints
  `{"ok":true,"result":{"valid":true}}` on the host and
  `{"ok":true,"result":{"valid":true},"warnings":["WARNING: Live liza agent process scan skipped (procfs unavailable on this host)"]}`
  inside a `bwrap` root without `/proc` (RP-5). Task 4's characterization drives the same
  `validate.go:167` path by injecting `ErrProcessScanUnavailable`.
- **F3a/F3b:** procfs absence cannot be reproduced for the A9 kit on Linux: the kit builds the CLI
  with `go`, which needs `/proc/self/exe` to locate GOROOT (RP-6).

### d. Verification scope

Checked: every claim above against source at `6ef124d3e` and the three CI job logs; F1, F2, F3c and
F4 reproduced on Linux in disposable clones; all current A2/A4/A9 goldens reproduce H1 at HEAD and
the oracle and harness suites pass with full history (RP-2, RP-7). Not checked: the macOS runner
itself (no access; its `/var/folders` → `/private/var` symlink is the standard macOS layout, and
`/var/folders/…` appears in the job log, line 176 — unverified on the runner); F3a/F3b by
reproduction (source-traced plus CI observation); any Windows test execution — no Windows job has
ever run the oracle or harness tests, because checkout failed.

### e. Sufficiency

After Tasks 1–4: Windows checkout has no invalid path and the guard keeps it so; every test-running
checkout resolves `96c9ad69a`; the four macOS A9 failures are either Linux-scoped (F3a/F3b) or
fixed (F3c); the A4 failure is Linux-scoped (F4). These causes make the observed run-40105658735
failures impossible. Residual paths, all disclosed and none claimed closed:

1. Windows runs the harness, regen and the Windows-enabled oracle tests (A1, A6, A8, sample and
   unscoped parts) for the first time; failures there are unknown.
2. macOS runs the starting-tree correction tests and `TestRegenNamesAlteredSampleGolden` for the
   first time (they pass on Linux under a symlinked `TMPDIR`, RP-4).
3. Suites with their own golden path code (`a1/cli/golden_test.go:248`,
   `a1/persist/helpers_test.go:86`, `a5`) bypass the harness mapping; their names are safe today
   and the guard rejects any unsafe tracked name.
4. Only the D52 run proves N2. A red D52 run keeps the area held, the maintainer commissions the
   fix (`d52`), and scoping a scenario for any reason other than a Linux process facility needs a
   new ruling (`d53`). No output claims final cross-platform acceptance.

### f. Adjacent findings (recorded, not planned)

- A2, A3 and A7 oracles are Linux-only by their architecture constraints (AC3, OC3/TC1), predating
  D53. Not changed; Task 4's proof-scope doc lists them with their sources.
- `arm-1-ar-20-cp-0-code-1` plans new A9 oracle cases (interactive launch, resume signal cleanup).
  Its kit-based cases may meet the same procfs or temp-path portability conditions on macOS; that
  remains its plan's concern under D53.
- No guard rejects case-only path collisions (none exist, RP-1).

## Common Rules

- **Oracle immutability (D53 exception).** Portability edits only: no fixture, golden content or
  asserted value changes; goldens change only by file name (Task 1). Moving a test body to a
  build-constrained file keeps its statements byte-identical apart from the enclosing declaration;
  imports are adjusted mechanically. Record every move in the Assertion Traceability map.
- **Provenance (D24/E5).** `internal/oracle/PROVENANCE` is not edited: no golden content changes,
  and every golden stays H1 (RP-2). Regenerate nothing; the regen check is the proof.
- **No production edits.** No file outside `internal/oracle/**`, `internal/testhelpers/oracle/**`,
  `internal/testguard/` (new files), `internal/commands/` (one new test file, Task 4),
  `.github/workflows/{ci,ci-core}.yml`, `specs/architecture/ADR/0219-…md` and the named docs.
- **White-box suites stay on every platform.** No build constraint or skip outside
  `internal/oracle/**`.
- **Environment (G1.4, E2–E4).** Run every command under `env -i HOME="$HOME" PATH="$PATH"
  TMPDIR=/tmp` (the F3c reproduction alone changes `TMPDIR` to a symlink in `/tmp`); disposable
  Git only; never print environment values; new guards report paths and names only. No push,
  dispatch, PR, installed-binary change or live runtime state.
- **TDD.** Each task commits its failing (or, for Task 4's characterization, green-on-unchanged-tree)
  test before the edit it guards, and records the run in its checkpoint.
- **Proof.** Pre-commit on touched files, then the canonical commands serially. Deferred commands
  (`make test`, `make test-race`) are batched on integration. Cn names validation index n.

## Assertion Traceability (original → retained)

| Assertion (original location at `6ef124d3e`) | Retained at | Platforms after |
|---|---|---|
| A2 `TestOracleA2ToolResultFilter` large/redaction effects (`tool_result_store_test.go:84`, goldens `…/store/<ID>.txt`, `…/events/<EVENT-1>.json`) | unchanged code; goldens renamed R100 to `%3CID%3E.txt`, `%3CEVENT-1%3E.json` | Linux (AC3, unchanged) |
| A2 inventory goldens listing `store/<ID>.txt` | unchanged bytes (logical names) | Linux |
| A9 `TestOracleA9RegistrationCollision` (`lifecycle_test.go:28-37`) | whole function in `internal/oracle/a9/lifecycle_linux_test.go` | Linux |
| A9 `TestOracleA9AuthorityLost` (`lifecycle_test.go:111-130`, with its comment) | whole function in `lifecycle_linux_test.go` | Linux |
| A9 `TestOracleA9AutoAssignID/capacity-refused` body (`lifecycle_test.go:57-66`) | body in a Linux-file helper registered by the unchanged subtest name; non-Linux counterpart skips naming D53 | Linux |
| A9 `TestOracleA9AutoAssignID/success` | unchanged in `lifecycle_test.go` | Linux, macOS |
| A9 `TestOracleA9Stub` cwd check (`stub_test.go:88`) | same file; compares symlink-resolved forms of both paths | Linux, macOS |
| A4 `runD35Success`, `checkD35Validation` (`dependencies_test.go:57-124`) | verbatim in `internal/oracle/a4/dependencies_d35_linux_test.go`; non-Linux `runD35Success` skips naming D53 | Linux |
| A4 `TestOracleRepairSupersededDependencies/refused` | unchanged in `dependencies_test.go` | Linux, macOS |

## Task 1: Store oracle goldens under filesystem-safe names with a tracked-path guard

**Priority:** Must (`d53` generic fix 1).
**Direct references:** ["d53", "d24", "adr-0219", "goal-tests", "goal-proof", "goal-docs", "env-safe", "a2-arch", "d52", "boundary"]

**Owned files:** `internal/testhelpers/oracle/golden.go`, `golden_test.go`, a new harness file for
the Windows-path predicate if preferred; new `internal/testhelpers/oracle/golden-names.md`; new
`internal/testguard/tracked_paths_test.go`; the four A2 goldens (renames only);
`specs/architecture/ADR/0219-oracle-harness-and-chained-provenance.md` (item 1 clause); manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-golden-names.json`.
**Not edited:** A2 test sources, golden contents, `PROVENANCE`, regen sources (their path
validator already accepts `%`, `ledger.go:61-78`).

1. **Red first.** Add `TestTrackedPathsAreWindowsSafe` (`internal/testguard`): run
   `git ls-files -z` at the repository root with an explicitly built environment, apply the
   harness predicate to every path, and fail listing each offending path with its reason (paths
   only, G1.4). A missing Git or repository fails; it never skips. Add harness tests
   `TestGoldenStorageNames` (mapping table, including `<ID>.txt` → `%3CID%3E.txt`, `%` → `%25`,
   safe names unchanged), `TestGoldenStorageNamesAreCollisionSafe` (`<ID>.txt` and literal
   `%3CID%3E.txt` map to distinct storage paths; write then live-read of each returns its own
   bytes; a write never lands at the raw logical name) and a refusal case for a name the mapping
   cannot make safe (reserved device name, trailing dot or space). Commit and record the red run
   (the guard lists the four F1 paths).
2. **Predicate (one definition).** Export from the harness one function (working name
   `WindowsPathProblem(path string) string`) that reports, per slash-separated component: a
   character in `<>:"|?*\`, a byte below 0x20, a trailing dot or space, or a reserved device name
   (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, case-insensitive, with or without
   extension). The guard and `goldenPath` both use it. The harness stays stdlib-only and compiles
   against the starting tree (`adr-0219` item 2).
3. **Mapping.** In `goldenPath`, after the existing validation, map each name component by
   percent-escaping `%`, `<`, `>`, `:`, `"`, `|`, `?`, `*` and bytes below 0x20 as `%` plus two
   uppercase hex digits; then refuse a mapped name that the predicate still rejects. The same
   function serves write and live read, so every `Check`/`CheckState` caller is covered without
   call-site edits; existing safe names map to themselves (RP-1: no tracked golden contains `%`).
4. **Rename.** `git mv` the four A2 goldens to their mapped names in the same commit as step 3;
   no content change.
5. **Docs.** `golden-names.md` (beside the existing harness docs) states the logical-name vs
   storage-name rule, the escape table, that inventories and `PROVENANCE` records use storage paths,
   and D53 as authority. In ADR-0219 item 1, qualify "Golden paths are never renamed" with one
   clause: storage-name-only renames that D53 requires are allowed, content unchanged. No other ADR
   edit.
6. **Evidence.** `git diff --find-renames=100% --name-status 6ef124d3e HEAD -- internal/oracle`
   shows exactly four `R100` lines and nothing else; `git ls-files | grep -E '[<>:"|?*\\]'` is empty;
   C3 lists the four mapped names as `ok … H1`.

**Done when:** `TestTrackedPathsAreWindowsSafe` failed on the four `<ID>`/`<EVENT-1>` paths
(recorded) and passes; the harness mapping tests pass; the four goldens are R100 renames to their
`%XX` names with no other `internal/oracle` change; C0–C6 pass; the regen check reports the four
mapped names H1; the manifest maps every allocated obligation.

Mappings: N-store → `internal/testhelpers/oracle/golden_test.go`, `TestGoldenStorageNames`, index 0;
N-collide → `golden_test.go`, `TestGoldenStorageNamesAreCollisionSafe`, index 0; N-guard →
`internal/testguard/tracked_paths_test.go`, `TestTrackedPathsAreWindowsSafe`, index 1; N-rename →
`internal/oracle/a2/cli/tool_result_store_test.go`, `TestOracleA2ToolResultFilter` large and
redaction effects read the renamed goldens, index 2; N-provenance →
`internal/testhelpers/oracle/cmd/regen/main.go`, check mode exits 0 with every A2 golden H1, index 3;
N-doc and N-proof use their `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-golden-names.json",
  "obligations": ["N-store", "N-collide", "N-rename", "N-guard", "N-provenance", "N-doc", "N-proof"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a2/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a2/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/testhelpers/oracle/... ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [7, 8],
  "approved_proofs": [
    {"obligation_id": "N-doc", "reference_id": "adr-0219", "rationale": "The storage-naming document and the ADR-0219 item 1 clause are reviewed for accuracy against the delivered mapping and D53; prose cannot be asserted by a test."},
    {"obligation_id": "N-proof", "reference_id": "goal-proof", "rationale": "Review that commands ran sanitized in disposable state, no environment value reached output, and nothing claims cross-platform acceptance before the D52 run owned by the boundary task."}
  ]
}
```

## Task 2: Fetch full history in every test-running CI checkout

**Priority:** Must (`d53` generic fix 2).
**Direct references:** ["d53", "d52", "d24", "goal-proof", "env-safe", "boundary"]

**Owned files:** `.github/workflows/ci-core.yml` (checkouts of jobs `linux`, `lint`, `macos`),
`.github/workflows/ci.yml` (checkout of job `test-windows-shard`); new
`internal/testguard/ci_checkout_history_test.go`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-ci-history.json`.
**Not edited:** any other workflow key, step or job; the aggregator job `test-windows`
(`ci.yml:229-260`), which runs only `go run ./scripts/ci/testshard` over shard artifacts.

1. **Red first.** Add `TestCIWorkflowCheckoutsFetchFullHistory`: parse `ci.yml` and `ci-core.yml`
   with the module's `gopkg.in/yaml.v3`; for every step whose `uses` starts with
   `actions/checkout@`, require `with.fetch-depth` equal to `0`, except in jobs of a named exemption
   set holding only `ci.yml` job `test-windows` with its reason. The test also fails if an exempt
   job gains a `run` step containing `go test` or `make`, so an exemption cannot start running tests
   silently. Failure messages name file, job and step index. Commit and record the red run (four
   checkouts).
2. **Edit.** Add `fetch-depth: 0` under `with:` of the four checkouts; nothing else changes.
3. **Evidence.** Record the shallow reproduction of RP-3 (depth-1 clone fails the four F2 tests;
   unshallowed passes) in the checkpoint. D52 CI is the final proof and is not claimed here.

**Done when:** the guard failed on the four checkouts without `fetch-depth: 0` (recorded) and
passes; the diff to the two workflows is exactly four added `fetch-depth: 0` lines; C0–C3 pass; the
manifest maps every allocated obligation.

Mappings: H-depth → `internal/testguard/ci_checkout_history_test.go`,
`TestCIWorkflowCheckoutsFetchFullHistory` checkout assertions, index 0; H-guard → same test,
exemption tripwire, index 0; H-proof → `internal/testhelpers/oracle/correction_start_test.go`,
`TestProviderCorrectionA8Start` and siblings plus `TestRegenNamesAlteredSampleGolden` resolve the
starting commit, index 1; H-env uses its `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-ci-history.json",
  "obligations": ["H-depth", "H-guard", "H-proof", "H-env"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run 'TestProviderCorrection|TestRegenNamesAlteredSampleGolden' ./internal/testhelpers/oracle/ ./internal/testhelpers/oracle/cmd/regen/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/testguard/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 1800,
  "deferred": [4, 5],
  "approved_proofs": [
    {"obligation_id": "H-env", "reference_id": "goal-proof", "rationale": "Review that the shallow reproduction and all commands ran in disposable clones under a sanitized environment, that no remote push or dispatch occurred, and that green CI is left to the D52 run of the boundary task."}
  ]
}
```

## Task 3: Make the A9 oracle correct off Linux

**Priority:** Must (`d53` Linux scope; generic fix 3).
**Direct references:** ["d53", "a9-arch", "goal-tests", "goal-frozen", "goal-nfr", "d24", "adr-0219", "goal-proof", "env-safe", "d52", "boundary"]

**Owned files:** `internal/oracle/a9/lifecycle_test.go`, new `lifecycle_linux_test.go`, new
`lifecycle_other_test.go` (`//go:build !linux`), `stub_test.go`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-a9.json`.
**Not edited:** A9 goldens, fixtures, kit files, `doc_test.go`, every other A9 test.

1. **Stub determinism (F3c), red first.** Before editing, run C0's `TestOracleA9Stub` with
   `TMPDIR` set to a symlink inside `/tmp` (as RP-4) and record the failure; plain `TMPDIR=/tmp`
   passes. Then change only the `Directory` comparison at `stub_test.go:88` to compare the
   `filepath.EvalSymlinks` forms of `records[0].Directory` and `p.dir` (a resolution error fails the
   test). The record's value is untouched, because launch goldens embed it
   (`launch_test.go:281`). Record the symlinked run green.
2. **Linux scope (F3a, F3b).** Move `TestOracleA9RegistrationCollision` and
   `TestOracleA9AuthorityLost` (with its comment) verbatim into `lifecycle_linux_test.go`
   (`//go:build linux`). Move the `capacity-refused` subtest body verbatim into a Linux-file helper
   that `TestOracleA9AutoAssignID` registers under the unchanged subtest name; its `!linux`
   counterpart only calls `t.Skip` naming D53 and the procfs liveness wording. Each new file starts
   with its reason: frozen wording from `procscan/process_status.go` procfs reads (I-01 collision,
   I-02 refusal); the `delete agent --force` identity check reads `/proc/<pid>/cmdline` (I-05).
   No other scenario is scoped: every other A9 test passed on macOS (RCA a).
3. **Evidence.** `GOOS=darwin` and `GOOS=windows` `go vet` (inside C3) compile the package with the
   new files; `go list -f '{{.TestGoFiles}}'` for `GOOS=darwin` excludes `lifecycle_linux_test.go`;
   the assertion-traceability rows for A9 hold; C2 reproduces every A9 golden H1.

**Done when:** `TestOracleA9Stub` failed under a symlinked `TMPDIR` before the edit and passes after
(both recorded), with the recorded directory value unchanged; the two whole functions and the
`capacity-refused` body live under `//go:build linux` with a stated reason and no statement change;
`TestOracleA9AutoAssignID/success` stays portable; C0–C4 pass; the manifest maps every allocated
obligation.

Mappings: L-a9 → `internal/oracle/a9/lifecycle_linux_test.go`, `TestOracleA9RegistrationCollision`,
`TestOracleA9AuthorityLost` and `TestOracleA9AutoAssignID/capacity-refused` run and pass on Linux,
index 0; L-stub → `internal/oracle/a9/stub_test.go`, `TestOracleA9Stub`, index 0; L-provenance →
`internal/testhelpers/oracle/cmd/regen/main.go`, check mode exits 0 with every A9 golden H1, index 2;
L-select → `Makefile`, `vet` target compiles A9 for linux, windows and darwin, index 3; L-preserve and
L-proof use their `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-a9.json",
  "obligations": ["L-a9", "L-stub", "L-provenance", "L-select", "L-preserve", "L-proof"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -v -run '^(TestOracleA9Stub|TestOracleA9RegistrationCollision|TestOracleA9AutoAssignID|TestOracleA9AuthorityLost)$' ./internal/oracle/a9/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a9/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a9/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/oracle/a9/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [5, 6],
  "approved_proofs": [
    {"obligation_id": "L-preserve", "reference_id": "goal-tests", "rationale": "Review the diff against the Assertion Traceability rows: moved statements are byte-identical, the stub edit changes only the comparison form, no golden or fixture changes, and no constraint is added outside internal/oracle/a9."},
    {"obligation_id": "L-proof", "reference_id": "goal-proof", "rationale": "Review sanitized execution, the recorded symlinked-TMPDIR red and green runs, and that macOS acceptance is left to the D52 run of the boundary task."}
  ]
}
```

## Task 4: Scope the A4 procfs-dependent scenario and document the oracle proof scope

**Priority:** Must (`d53` generic fix 3 investigated; Linux scope per C-A4).
**Direct references:** ["d53", "a4-arch", "a2-arch", "a3-arch", "a7-arch", "a9-arch", "goal-tests", "goal-frozen", "goal-nfr", "goal-docs", "d24", "adr-0219", "goal-proof", "env-safe", "d52", "boundary"]

**Owned files:** new `internal/commands/validate_process_scan_test.go`;
`internal/oracle/a4/dependencies_test.go`, new `dependencies_d35_linux_test.go`, new
`dependencies_d35_other_test.go` (`//go:build !linux`); new
`internal/testhelpers/oracle/proof-scope.md`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-a4.json`.
**Not edited:** production code, A4 goldens, fixtures, other A4 tests, the boundary record.

1. **Characterize the cause (green on the unchanged tree, before any oracle edit).** Add
   `TestValidateWarnsWhenProcessScanUnavailable`: with `findZombieAgents` replaced (as
   `inspect_agents_test.go:370-372` does) to return `procscan.ErrProcessScanUnavailable`,
   `ValidateCommandWithOptions` on a valid fixture state succeeds and writes exactly the
   `WARNING: Live … agent process scan skipped (procfs unavailable on this host)` line to the
   supplied warning writer (binary name through `brand`); with the scan available and no zombies it
   writes nothing. Restore the variable with `t.Cleanup`. Record the green run. This is the
   controlled regression for F4; it fixes no behaviour.
2. **Linux scope (F4).** Move `runD35Success` and `checkD35Validation` verbatim into
   `dependencies_d35_linux_test.go` (`//go:build linux`) with the reason: the `_after` validation
   golden freezes the procfs-present process scan (`commands/validate.go:161-170`). The
   `!linux` file defines `runD35Success` as `t.Skip` naming D53 and that reason.
   `TestOracleRepairSupersededDependencies` and its `refused` subtest stay unchanged; imports adjust
   mechanically.
3. **Proof-scope doc.** `proof-scope.md` lists, per suite, where oracle proof runs and why,
   enumerated from `git grep -nE '^//go:build|t\.Skip\(' -- internal/oracle` at coding HEAD rather
   than from this plan: at `6ef124d3e` that includes A2 Linux (AC3), A3 and A7 Linux (OC3/TC1), the
   A4 and A5 Windows skips, the A9 Windows skip (AC4) and opt-in sensitivity proofs; plus the D53
   Linux scope delivered by Task 3 (I-01 collision, I-02 refusal, I-05) and step 2
   (`repair-superseded-dependencies/success`), each with its process facility;
   white-box suites run on all three platforms; the D52 run at the boundary SHA is the N2 proof and
   the boundary record names the scope (`boundary`). Cite sources; do not restate rationale.
4. **Evidence.** `GOOS=darwin`/`windows` vet (inside C3) compiles the package; C2 reproduces every
   A4 golden H1; Assertion Traceability A4 rows hold.

**Done when:** `TestValidateWarnsWhenProcessScanUnavailable` passed on the unchanged tree (recorded)
and passes; the D35 success scenario sits under `//go:build linux` with its reason and no statement
change while `refused` stays portable; `proof-scope.md` names every Linux-only and Windows-skipped
oracle scope with its source; C0–C4 pass; the manifest maps every allocated obligation. If review
contests C-A4, mark BLOCKED with its unresolved decision instead.

Mappings: A4-cause → `internal/commands/validate_process_scan_test.go`,
`TestValidateWarnsWhenProcessScanUnavailable`, index 0; A4-scope →
`internal/oracle/a4/dependencies_d35_linux_test.go`, `TestOracleRepairSupersededDependencies/success`
runs and passes on Linux, index 1; A4-provenance → `internal/testhelpers/oracle/cmd/regen/main.go`,
check mode exits 0 with every A4 golden H1, index 2; A4-select → `Makefile`, `vet` target compiles A4
for linux, windows and darwin, index 3; A4-doc, A4-preserve and A4-proof use their
`approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d53-oracle-portability-plan-a4.json",
  "obligations": ["A4-cause", "A4-scope", "A4-provenance", "A4-select", "A4-doc", "A4-preserve", "A4-proof"],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -run '^TestValidateWarnsWhenProcessScanUnavailable$' -v ./internal/commands/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/a4/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a4/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/commands/ ./internal/oracle/a4/",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-race"
  ],
  "timeout_seconds": 3600,
  "deferred": [5, 6],
  "approved_proofs": [
    {"obligation_id": "A4-doc", "reference_id": "d53", "rationale": "The proof-scope document is reviewed for completeness against the delivered build constraints and skips and for citing each scope's source; prose cannot be asserted by a test."},
    {"obligation_id": "A4-preserve", "reference_id": "goal-tests", "rationale": "Review the diff against the A4 Assertion Traceability rows: moved statements are byte-identical, refused stays portable, and no golden, fixture or production file changes."},
    {"obligation_id": "A4-proof", "reference_id": "goal-proof", "rationale": "Review sanitized execution and that macOS acceptance is left to the D52 run of the boundary task."}
  ]
}
```

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | none | current integration (`6ef124d3e`) | Must |
| 1 (Task 2) | none | current integration | Must |
| 2 (Task 3) | none | current integration | Must |
| 3 (Task 4) | `depends_on` 2 | Task 3's merge: the A9 Linux-scoped files that `proof-scope.md` names | Must |

No `task_depends_on`, `provider_dependencies` or `inherit_inputs`: this root has no upstream phase,
and no child needs another task's output beyond the edge above.

| File or definition | Writers | Ordering |
|---|---|---|
| `internal/testguard/` package | Task 1 (`tracked_paths_test.go`), Task 2 (`ci_checkout_history_test.go`) | Commute: new files, distinct test names; neither adds a package-level identifier the other uses or names (each prefixes its helpers by file) |
| `internal/testhelpers/oracle/` | Task 1 (`golden.go`, tests, `golden-names.md`), Task 4 (`proof-scope.md` only) | Commute: distinct files; Task 4 also waits on Task 3 only |
| `internal/oracle/a9/` | Task 3 only | Sole writer |
| `internal/oracle/a4/`, `internal/commands/validate_process_scan_test.go` | Task 4 only | Sole writer |
| `.github/workflows/ci.yml`, `ci-core.yml` | Task 2 only | Sole writer |
| A2 goldens, ADR-0219 | Task 1 only | Sole writer |
| Acceptance manifests | one task each | Distinct paths |

## Placement

Rule: `d53` Order — the corrections land before the A1 boundary's G8; the boundary rebases on
`global-refactoring`, reruns G1–G7 and holds for a D52 run at that SHA. Correction lineage: this plan
(new; replaces no task).

- **Predecessors:** every writer of the owned files is merged into `6ef124d3e`; no task is started
  (task graph has no IMPLEMENTING or REVIEWING status, RP-8). No wait is declared.
- **Successors:** `arm-1-ar-11-cp-0-code-0` (BLOCKED A1 boundary). It is held by the provider
  reservations named in its blocked reason until every child of this plan merges; this plan adds no
  edge to or from it (no backward edge). Later unstarted A9 oracle writers
  (`arm-1-ar-20-cp-0-code-1`, additive new files only, after the A1 boundary by area sequencing)
  receive the harness mapping through `goldenPath` without edits.
- **Independent sibling:** `deferred-validation-19-timing-followup-plan` (MERGED) owns
  `internal/agent/checkpoint_summary_gate_test.go` and `internal/ops/await_verdict_test.go`; no file
  overlap and no dependency either way.
- **Slot:** immediately, on current integration, before the boundary rerun.

## Reality Probes

Run at `6ef124d3e78667f8bf555854e499ad81cd0f1b98` (worktree HEAD) in disposable clones
`/tmp/d53-diag` (full history) and `/tmp/d53-shallow`, 2026-10-24T15:55Z–16:20Z, under
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp` unless stated.

| ID | Assertion / command | Observation |
|---|---|---|
| RP-1 | `git ls-files \| grep -E '[<>:"\|?*\\]'`; control/trailing/device-name grep; case-folded `uniq -d`; `%` in golden names | the four F1 paths only; no other hit; no case collision; no golden contains `%` |
| RP-2 | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a2/cli/...`; `… -pkg ./internal/oracle/a4/...`; `… -pkg ./internal/oracle/a9/...`; `PROVENANCE` | exit 0 each: the four `<ID>`/`<EVENT-1>` goldens `ok … H1` (7.7 s); A4 220 `ok` lines (19.5 s); A9 383 `ok` lines, none other (2 min 12 s); ledger holds only `start 96c9ad69a…` |
| RP-3 | `git clone --depth 1`, then `go test -count=1 -run 'TestProviderCorrection\|TestRegenNamesAlteredSampleGolden' ./internal/testhelpers/oracle/ ./internal/testhelpers/oracle/cmd/regen/`; then `git fetch --unshallow`, same command; `git merge-base --is-ancestor 96c9ad69a HEAD` | shallow: the four CI failures verbatim (`export immutable starting tree: exit status 128`, `invalid reference: 96c9ad69a…`); unshallowed: `ok` both packages; ancestor, 353 commits |
| RP-4 | `TMPDIR=/tmp/d53-link` (→ `/tmp/d53-real`) `go test -count=1 -run '^TestOracleA9Stub$' ./internal/oracle/a9/`, with a temporary diagnostic in the clone; same for `./internal/testhelpers/oracle/ ./internal/testhelpers/oracle/cmd/regen/` and `TestOracleRepairSupersededDependencies` | stub fails at `stub_test.go:89`, `exe=true args=true dir="/tmp/d53-real/…/project" want="/tmp/d53-link/…/project"`; `TMPDIR=/tmp` passes; harness and regen `ok`; A4 `ok` (symlinks do not explain F4) |
| RP-5 | prebuilt `liza validate --json` on the post-repair D35 project copied from the A4 fixture, on the host and in `bwrap --ro-bind /usr /usr … --bind /tmp /tmp --dev /dev` (no `/proc`) | host `{"ok":true,"result":{"valid":true}}`; sandbox adds `"warnings":["WARNING: Live liza agent process scan skipped (procfs unavailable on this host)"]` |
| RP-6 | the A4 test binary and `go env GOCACHE` inside the same sandbox | `go: cannot find GOROOT directory: 'go' binary is trimmed and GOROOT is not set` — kit-level procfs-free runs are not reproducible locally (F3a/F3b UNPROBED by reproduction; held on CI observation and source) |
| RP-7 | `go test -count=1 ./internal/testhelpers/oracle/... ./internal/testguard/ ./internal/oracle/a2/cli/`; `go test -count=1 ./internal/oracle/a9/ ./internal/oracle/a4/` | `ok` (regen 15.1 s, a2/cli 2.0 s, testguard 0.08 s); a9 141.7 s, a4 26.9 s |
| RP-8 | `omni-ee get tasks --all --json` filtered (full read: needed for a writer audit across 86 unlisted DRAFT_CODE tasks) | statuses: 128 MERGED, 86 DRAFT_CODE, 4 DRAFT_CODING_PLAN, 2 CODE_PLANNING, 1 BLOCKED, 1 SUPERSEDED; no non-merged task names the owned files; `arm-1-ar-11-cp-0-code-0` BLOCKED on this plan and the timing plan; timing plan MERGED |
| RP-9 | `make lint` (Tasks 1–4 C-lint) | gofmt, `go vet` for linux/windows/darwin, archguard `ok`; 1 min 11 s |
| RP-10 | `go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/...` (Task 1 C4); `go test -race -count=1 ./internal/commands/` (Task 4 C4 part); `go test -race -count=1 ./internal/oracle/a9/` (Task 3 C4) | regen exit 0, no non-`ok` line, 3 min 13 s; commands race `ok` 57.3 s (1 min 15 s wall); A9 race `ok` 208.9 s (3 min 30 s wall) |
| RP-11 | `go test -count=1 -run '^$' -v ./internal/testguard/` | `ok … [no tests to run]`: selection and package compile; `TestTrackedPathsAreWindowsSafe`, `TestCIWorkflowCheckoutsFetchFullHistory`, the harness mapping tests and `TestValidateWarnsWhenProcessScanUnavailable` are PROSPECTIVE (Tasks 1, 2, 1, 4) |

**Budget.** Measured above on this host: Task 1 ≈ harness 15 s, testguard 1 s, A2 5 s, A2 regen 8 s,
full-oracle regen 193 s, lint 71 s, race a few minutes; Task 2 under 3 min; Task 3 ≈ focused
20 s, A9 142 s, A9 regen 132 s, lint 71 s, A9 race 209 s; Task 4 ≈ focused 10 s, A4 27 s, A4 regen
20 s, lint 71 s, commands+A4 race about 2 min. `make test` and `make test-race` are deferred. Budgets of
3600 s (Tasks 1, 3, 4) and 1800 s (Task 2) give at least double headroom for load.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Goldens stored under filesystem-safe names on every OS | `d53` generic fix 1 | Task 1 | Covered |
| 2 | Harness maps tokens in path components on store and read; collision-safe | `d53` generic fix 1 | Task 1 | Covered |
| 3 | Golden contents and assertions unchanged; `git mv` renames | `d53` generic fix 1, portability exception | Task 1 | Covered |
| 4 | Guard test rejects tracked paths invalid on Windows | `d53` generic fix 1 | Task 1 | Covered |
| 5 | Every test-running CI checkout fetches the starting commit's history | `d53` generic fix 2 | Task 2 | Covered |
| 6 | Cross-platform difference not from a Linux facility fixed as defect/determinism, A4 first | `d53` generic fix 3 | Task 4 (A4 investigated: procfs, C-A4), Task 3 (stub determinism) | Covered |
| 7 | Linux-process-facility scenarios scoped by `//go:build linux`, reason per file, A9 at least | `d53` Linux scope | Tasks 3, 4 | Covered |
| 8 | Oracle docs name the scope; boundary record names it | `d53` Linux scope; `boundary` | Task 4 (oracle doc); boundary record owned by `arm-1-ar-11-cp-0-code-0` | Covered |
| 9 | White-box suites keep running on all three platforms; no other scoping | `d53` Linux scope | Tasks 3, 4 (Common Rules) | Covered |
| 10 | No asserted value or fixture change; goldens change only by file name | `d53` portability edits; `goal-tests` | Tasks 1–4 | Covered |
| 11 | Provenance and regeneration preserved | `d24`; `adr-0219`; `goal-proof` E5 | Tasks 1, 3, 4 (regen checks) | Covered |
| 12 | Corrections land before A1 G8; boundary reruns G1–G7 then D52 run; no backward edge | `d53` Order; `d52` | Placement | Covered |
| 13 | No output claims final cross-platform acceptance before green D52 run | `d52`; `goal-nfr` N2 | Tasks 1–4 (proof obligations) | Covered |
| 14 | Sanitized environment, disposable state, no push/dispatch | `goal-proof` E2–E4; `env-safe` | Tasks 1–4 | Covered |
| 15 | Production behaviour change or scoping beyond D53 returned as decision | task done_when; `goal-frozen` | C-A4 (Task 4 BLOCKED path) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: no product behaviour changes; test-only and CI-configuration edits | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 (`golden-names.md`, ADR-0219 clause), Task 4 (`proof-scope.md`) | Covered |

## Output

Four entries, output[i] = Task i+1. Outputs 0–2 have no dependency; output 3 `depends_on` ["2"].
Each `plan_ref` anchors that task's section, whose Acceptance Contract matches the entry's ordered
validation and prerequisites. `desc`, `done_when` and `scope` are short projections of the task
sections. This session writes only the two planning artifacts.
