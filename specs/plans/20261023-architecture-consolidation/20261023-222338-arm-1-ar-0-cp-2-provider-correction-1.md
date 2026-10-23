# SI-3 Provider Correction: Explicit Oracle Capabilities

## Source References
Source revision: "011918bdec81793b38292efa0f60b844ea53909e"

### Direct References
- "provider-plan": "specs/plans/20261023-architecture-consolidation/20261023-184056-arm-1-ar-0-cp-2.md#Local decisions (owned by this plan)" @ "6bfc3929fd0a60d2c5e6fa45b9e92fafe1bd0e85"
- "provider-contract": "specs/arch-plan/20261023-architecture-consolidation/20261023-172626-arm-1-ar-0.md#Scope 3: Oracle harness and regeneration entry point" @ "c4b0e7a1e52d0ab07e6146db165c7a68e96cc189"
- "provider-asks": "specs/arch-plan/20261023-architecture-consolidation/20261023-172626-arm-1-ar-0.md#Provider asks (consumers of SI-1, SI-2, SI-3)" @ "c4b0e7a1e52d0ab07e6146db165c7a68e96cc189"
- "a8-contract": "specs/arch-plan/20261023-architecture-consolidation/20261023-180843-arm-1-ar-8.md#Scope 1: A8 prompt oracle" @ "53e8fdad0578be4019b27655d34e7bc100a284ce"
- "a8-determinism": "specs/arch-plan/20261023-architecture-consolidation/20261023-180843-arm-1-ar-8.md#Determinism Rules" @ "53e8fdad0578be4019b27655d34e7bc100a284ce"
- "a8-asks": "specs/arch-plan/20261023-architecture-consolidation/20261023-180843-arm-1-ar-8.md#Provider asks (consumer of SI-3)" @ "53e8fdad0578be4019b27655d34e7bc100a284ce"
- "a5-contract": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Scope 1: A5 oracle suite" @ "140354f397918868749d5cd21b0af365bec571fd"
- "a5-dataflow": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Data Flow" @ "140354f397918868749d5cd21b0af365bec571fd"
- "a5-determinism": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Determinism Rules" @ "140354f397918868749d5cd21b0af365bec571fd"
- "a5-asks": "specs/arch-plan/20261023-architecture-consolidation/20261023-182904-arm-1-ar-5.md#Provider asks (consumer of SI-3)" @ "140354f397918868749d5cd21b0af365bec571fd"
- "a9-contract": "specs/arch-plan/20261023-architecture-consolidation/20261023-181325-arm-1-ar-9.md#Scope 1: A9 oracle suite" @ "c91d2b50350233bd95453402d242311b55f44f2c"
- "a9-interfaces": "specs/arch-plan/20261023-architecture-consolidation/20261023-181325-arm-1-ar-9.md#Interfaces" @ "c91d2b50350233bd95453402d242311b55f44f2c"
- "a9-dataflow": "specs/arch-plan/20261023-architecture-consolidation/20261023-181325-arm-1-ar-9.md#Data Flow" @ "c91d2b50350233bd95453402d242311b55f44f2c"
- "goal-frozen": "specs/goals/20261023-architecture-consolidation.md#Frozen Surfaces"
- "goal-tests": "specs/goals/20261023-architecture-consolidation.md#Test Policy and Sequencing"
- "goal-proof": "specs/goals/20261023-architecture-consolidation.md#Proof Environment"
- "guard": "GUARDRAILS.md#Tier 1 (Hard Constraints)"
- "ownership": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Shared-File Ownership"
- "adr219": "specs/architecture/ADR/0219-oracle-harness-and-chained-provenance.md#Decision"

### Obligation Coverage
- "AC-root-only" -> "a8-contract", "a8-determinism", "a8-asks", "goal-frozen"
- "AC-prepared-project" -> "a5-contract", "a5-dataflow", "a5-asks"
- "AC-prepared-environment" -> "a5-determinism", "provider-plan", "guard", "goal-proof"
- "AC-fixed-sha" -> "a9-contract", "a9-dataflow", "a9-interfaces"
- "AC-normalisation-defaults" -> "provider-plan", "provider-contract", "goal-tests"
- "AC-run-defaults" -> "provider-plan", "provider-contract"
- "AC-start-root" -> "provider-contract", "goal-proof"
- "AC-start-project" -> "provider-contract", "goal-proof"
- "AC-start-sha" -> "provider-contract", "goal-proof"
- "AC-stdlib-harness" -> "provider-contract", "adr219"
- "AC-sample-regeneration" -> "provider-contract", "goal-proof", "goal-tests"

## Correction Authority and Boundary

This is the owner correction commissioned by blackboard task
`arm-1-ar-0-cp-2-provider-correction-1` (planning_change.kind=correction,
original_task_id=`arm-1-ar-0-cp-2`, trigger=consumer-interface-gap).
It corrects the provider's local D-3 through D-6 choices, not the consumer carriers.
The three BLOCKED records were read directly with targeted `omni-ee get <id> --json`:
`arm-1-ar-8-cp-0`, `arm-1-ar-5-cp-0`, `arm-1-ar-9-cp-0`.
Their gaps are contract gaps, not implementation violations of the old plan.

Effective correction reference after independent approval is this document's
**Corrected Capabilities** section; D-3–D-6 at `provider-plan` remain the historical
default contract. The additions below are opt-in. D-1, D-2, D-7–D-14 and the
architecture/ADR contracts remain unchanged by reference. No original plan, consumer,
production Go, existing oracle test, sample/area golden, PROVENANCE, regen command,
ADR index, installed asset or running binary is edited.

Success means independently reviewed child allocations deliver all three APIs with
separate falsifiable proofs and unchanged existing behavior. Validation: source and
allocation parity, payload preflight, pre-commit, immutable submission and peer review.
Doc Impact: this plan/output; children add API-specific correction notes below.
Test Impact: planning-only here; children own their executable tests and manifests.
Changes are reversible and entirely in the test-support boundary. No runtime
state/concurrency/authentication implementation is changed.

## Provider Ask Reconciliation

There is no epic or early Provider Ask Inventory in this run (`provider-asks`).
The assigned consumer tables, plus a search of merged architecture specs for
`SI-3` and `Provider Ask Inventory`, are the inventory for this correction.
Operations/fields/statuses below refer to test APIs; no lifecycle statuses are added.

| Interface / consumers | Operations and fields requested | Provider answer / disposition |
|---|---|---|
| SI-3 / A8, a8-asks | Root replacement on prompt bytes, raw prompt comparison | Task 1, exact registered root replacement before existing Prompt comparison |
| SI-3 / A5, a5-asks and a5-dataflow | Chosen project working directory; ordered preparation and operation; explicit environment; separate output and exit status | Task 2, reusable caller-owned project plus existing Result/RunOptions/ChildEnv |
| SI-3 / A9, a9-interfaces and a9-contract | Fixed-recipe SHAs retained after A9 normalisation through state/json/human comparison | Task 3, per-Normaliser exact SHA allowlist; no tokenisation of retained SHAs |
| SI-3 / A1–A9 | Existing kinds, missing-golden refusal, write-new, package filter, starting adapters | Existing provider outputs 0/1; no format or regeneration change |
| SI-3 / A3, A4, A6, A7 | Chosen invocation controls and local duration/identity preprocessing | Existing local fallbacks remain permitted by their Interfaces tables; this correction does not require adoption or change their scope |
| SI-3 / refactor/C/boundary scopes | Immutable oracle, adapter twins, provenance chain | Unchanged provider contract/ADR219; no new commitment |

The A3 lifecycle/control and A7 blockers in the supplied digest are not declared
closed by these capabilities. Their independent contract questions remain with their
owners. This is a bounded extension, not a new freeze of all consumer interfaces.
A5/A8/A9 consumers remain BLOCKED until the relevant coding output is MERGED and
verified. The operator must attach the applicable selected provider output when
resuming each lineage: A8 output 0, A5 output 1, A9 output 2, transition
`code-plan-to-coding`. Neither this plan ID nor its approval alone closes a blocker.
No supersedes field, consumer replacement, or collapsed consumer lineage is emitted.

## Corrected Capabilities

### C-A8: Exact case-root replacement

Add `ReplaceRoot(text, root, token string) string` in a new `root_paths.go`.
Supplying root registers that single literal for this invocation; the A8 driver
already resolves the case root with EvalSymlinks (`a8-determinism`). A caller
needing another spelling invokes the helper explicitly for that spelling.
Replace complete path-root occurrences and nothing else; empty root is a no-op.
Match the literal spelling, including repeated occurrences embedded in prompt text,
but reject a match glued to a name byte before or after the root (reuse isNameByte,
including its dot/hyphen/underscore classification). A slash or backslash after the
root starts its untouched path suffix. Do not infer other roots, clean input text, consult
os.TempDir, apply timestamp/UUID/hex/literal normalisation, or rewrite path suffix
separators. Thus a sibling path formed by appending a name suffix is not the root.
Raw bytes outside matched root spans, including invalid UTF-8, CRLF, ANSI, tabs,
spaces and the trailing newline, must survive unchanged.

Use a small scanner with stdlib string search and the existing boundary predicate.
A general configurable normalisation pipeline is unnecessary. Existing
`replaceDir` cannot be called directly: it also rewrites the suffix's separators.
D-3 Text is still the full normaliser. D-4/D-5 Prompt still compare raw bytes.
A8 uses `Check(Prompt, ..., []byte(ReplaceRoot(...)))`; never Text.

### C-A5: Caller-prepared project execution

Add `RunInProject(t testing.TB, bin string, args []string, project string,
base EnvBase, opts RunOptions) Result`. `project`, `base.Home`, and `base.Tmp`
are existing absolute directories in a caller-materialised disposable case;
the caller owns their creation, contents, lifetime and cleanup. The helper checks
that these are existing absolute directories before spawning; it does not create,
replace or clear them. It uses `base.Path` explicitly, with the same ChildEnv
validation and platform rules as today, and opts.Env/opts.Stdin unchanged.
The A5 caller uses Go=false, as its child is the tested CLI.

Reuse a private execution function in cli.go for Run and RunInProject; retain the
existing timeout, captured stdout/stderr, ordinary nonzero ExitCode, fatal start
failure, Result fields, normaliser registration and diagnostics policy.
Run keeps allocating fresh project/home/tmp and choosing host PATH exactly as D-6.
No RunOptions field or Run caller changes are required.

The A5 driver materialises once per case/mode, calls RunInProject for each ordered
preparation step, fails immediately on a failed preparation, snapshots before,
then calls RunInProject for the tested operation with the same directories.
The runner executes one invocation only; it does not own case sequencing or
infer whether a nonzero exit is expected. This keeps ordinary refusal tests usable.
No package/global working-directory change, implicit initialisation, production
hook, or driver-local replacement runner is introduced.

### C-A9: Exact fixed-SHA preservation

Add `(*Normaliser).KeepSHA(sha string) error`, accepting only complete 40- or
64-character ASCII hex strings; invalid registration returns a value-free error
without changing the allowlist. Store exact spellings per instance, never globally.
Repeated registration is idempotent. During Text's final hex-ID replacement,
return an exact registered match verbatim; replace other matches with TokenID as
before. Registration happens before use; no concurrent mutation guarantee is added.
Read-only use after registration and independent normalisers must be race-free.

This narrow change uses the existing hex matcher; it does not disable identifier
normalisation, change kind dispatch, or introduce placeholder sentinels which could
collide with input. Dir, Literal, timestamp and UUID behavior retain their order.
Explicit caller Literal substitutions still mean substitution; A9 registers its
fixed recipe SHAs with KeepSHA and never with Literal. A9's own equality-preserving
volatile tokens are applied first as required by `a9-dataflow`.
StateForm and the instance CheckState, Form(JSON/Human), and instance Check methods
therefore all retain registered SHAs. Package-level Check/CheckState still use a new,
unconfigured normaliser. State remains a before/after state pair, not Artifact.

D-3's default 40/64-hex replacement remains true when no SHA is registered; D-4 and
D-5 use that default or the explicitly configured instance. These are corrected
capabilities, not retroactive claims that the delivered API already supported them.

## Placement and Dependency Audit

`ownership` orders edits to the ADR index, protocols and lessons; none is owned here,
so this correction does not insert a writer into that ordered area chain.
The merged harness writers are `arm-1-ar-0-cp-2-code-0` and
`arm-1-ar-0-cp-2-code-1`; all children explicitly select upstream outputs [0,1].
The earliest supplied inputs are the delivered harness/sample and regen command,
respectively. Both are Must. No optional capability or unchecked-priority task is
on the resulting dependency paths.

Task 1 creates the shared starting-tree test helper. Tasks 2 and 3 depend on Task 1
only for this concrete test-support artifact, and may then run concurrently.
Task 1 adds root_paths.go; Task 2 alone edits cli.go; Task 3 alone edits normalise.go.
Each adds separately named tests, fixture subtree, documentation and manifest.
No task edits another task's file or common test registration. These additions
commute: they claim distinct names and preserve the other API's default behavior;
Task 2's existing use of Normaliser requires only its unchanged default contract.
There is no unspecified overlapping writer inside this output graph.

Task 1 owns `correction_start_test.go`: a test-only helper
`runCorrectionAtStart(t, testPattern)` shared by all three capabilities. Use real Git
to export the immutable start `96c9ad69aee983ce2b929dd681b6ba43bf920535` into a
temporary directory, overlay only the current `internal/testhelpers/oracle/**`,
and run `go test -tags oraclestart -count=1 -run <testPattern>
./internal/testhelpers/oracle` there under ChildEnv(Go=true).
The selected pattern must exclude all starting-tree wrapper tests, preventing
recursion, and must select the capability's positive/negative tests.
Require actual named test execution, not merely a zero exit with no tests.
The helper never checks out the agent's worktree or accesses live runtime state.
No production imports enter the harness. Task 1 supplies the helper, not another
area oracle, registry, or regen implementation. Tasks 2/3 consume it read-only.

## Reality Probes

All current-tree observations below are at integration snapshot
`011918bdec81793b38292efa0f60b844ea53909e`, taken 2026-10-23T22:23Z.
Historical carrier equality was checked with git diff --quiet where an earlier
pin was supplied. Reads of A5/A8 are at their last file commits
140354f397918868749d5cd21b0af365bec571fd / 53e8fdad0578be4019b27655d34e7bc100a284ce.
This plan's new behavior and starting-tree wrappers are PROSPECTIVE, owned by
Tasks 1–3; the existing defaults below are EVIDENCED.

| Probe | Command / source read | Observation and implication |
|---|---|---|
| RP1 | `git diff --quiet 6bfc3929fd0a60d2c5e6fa45b9e92fafe1bd0e85 HEAD -- specs/plans/20261023-architecture-consolidation/20261023-184056-arm-1-ar-0-cp-2.md`; same check for provider architecture at c4b0e7 and A9 carrier at c91d2b5 | All exit 0. Current-file section reads match those pinned carriers. |
| RP2 | Read normalise.go:43–104,131–165; kind.go; golden.go | Text replaces all hex IDs; replaceDir rewrites suffix slashes; Prompt is raw; instance CheckState calls its own StateForm. Additive seams above have real call paths. |
| RP3 | Read cli.go:138–204, env.go and cli_test.go; existing tests below | RunOptions has Env/Stdin only; Run allocates each call, calls ChildEnv, returns Result. No caller-prepared execution API exists. |
| RP4 | `scip-search symbols ... --name Normaliser --name RunOptions`; `scip-search impact ... --symbol <Normaliser.Text exact symbol> --one-line` with supplied worktree index | Text reaches JSON, human, state and A3 lifecycle. Source reads confirm kind composition; area regressions are required. |
| V0 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/testhelpers/oracle/...` | PASS: harness 0.822s, regen 29.406s. Includes default API and stdlib boundary tests. New capability and starting-tree tests join this command; their execution is prospective. |
| V1 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -race -count=1 ./internal/testhelpers/oracle/...` | PASS: harness 1.612s, regen 18.679s. New capability race checks join the same package selection. |
| V2 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 ./internal/oracle/...` | PASS: A2 artifact 0.425s, A3 lifecycle 22.022s, sample 9.516s. These are all existing selected packages at this snapshot; no new area package is required by this plan. |
| V3 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/sample/...` | PASS: help.txt H1 and error-envelope.json H1. This existing command overlays the harness at start and must still reproduce both unchanged goldens after every child. |
| V4 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make lint`; bounded setup probe `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make -n lint test test-e2e` | Dry-run selects sync-embedded, vet for linux/windows/darwin, archguard; setup/selection verified, full lint deferred to integration. |
| V5 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test`; same dry-run | Selects sync-embedded and go test ./...; regression-only, deferred. |
| V6 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp make test-e2e`; same dry-run | Selects -race -tags e2e and TestFullSprintSequence/TestTerminalDependencyRecovery; regression-only, deferred. |
| RP5 | Targeted blocked task JSON; a5/a8/a9 carrier CONTRACT and Interfaces/Determinism reads; SI-3 search in merged specs | Each requested capability is owned here and missing today; local fallback permissions of other areas do not waive these consumers' contracts. |

Budget: **estimate** 1800 seconds per child's undeferred batch. Current V0/V1
package durations total about 51 seconds; starting-tree tests add archive/overlay
and three focused test processes (plus a small fixture binary for A5), and V2/V3
build existing CLI/sample surfaces. Reserve up to 300 seconds per such cold build
and substantial concurrent-run headroom. There is no goal hard time ceiling.
V4–V6 are long shared regression commands, deferred at indices 4,5,6; no manifest
proof maps to them. Canonical prerequisites declare existing tools, never a binary
the child has yet to build. Pre-commit runs on every changed child file before
tests, separately from this acceptance batch.

## Task 1: Root-only prompt substitution

**Priority:** Must. **Intent:** enable A8's root-only substitution through SI-3.
**Owned:** new `internal/testhelpers/oracle/root_paths.go`, `root_paths_test.go`,
`correction_start_test.go`, optional fixtures under `testdata/correction-root/`,
`root-paths.md`, `testdata/acceptance-correction-root.json`.
**Input:** merged original outputs [0,1]. **Local dependencies:** none.
Implement C-A8 and the shared starting-tree helper allocated above.
Doc root-paths.md with C-A8 usage and the D-3/D-4/D-5 default distinction.

Proofs (all real API calls; expected bytes independently literal, not produced by Text):
- `TestRootOnlyBytes`: repeated resolved root replaced; every other byte retained,
  including timestamps with offsets, UUIDs, 40/64-hex values, other temp roots,
  tabs/CRLF/ANSI, backslash suffixes, invalid UTF-8 and final-newline differences.
- `TestRootOnlyAbsentAndEmpty`: no matching root, glued name prefixes/suffixes,
  sibling paths and empty registration are no-ops.
- `TestRootOnlyGoldenComparison`: in a disposable golden directory, public Prompt
  Check write/live roundtrip succeeds; changing a non-root timestamp/SHA/whitespace
  byte fails live comparison. Preserve original golden after the expected failure.
- `TestProviderCorrectionA8Start`: shared helper executes the above tests at the
  actual start tree. No current-tree-only tagged compilation is accepted as this proof.
- Existing normaliser, kind and golden tests retain their expectations; the stdlib
  import test remains green; V3 proves untouched sample regeneration.

**Done when:** these named positive/negative tests execute in V0 and at start,
V1 passes, and V2/V3 remain green with no existing goldens changed.
Manifest maps AC-root-only to the first three tests (V0), AC-start-root to the
wrapper (V0), AC-normalisation-defaults to existing normalise/kind tests (V0),
AC-stdlib-harness to TestHarnessImportsStdlibOnly (V0), and AC-sample-regeneration
to the existing sample test file exercised by V3. No proof exceptions.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "internal/testhelpers/oracle/testdata/acceptance-correction-root.json",
  "obligations": [
    "AC-root-only",
    "AC-start-root",
    "AC-normalisation-defaults",
    "AC-stdlib-harness",
    "AC-sample-regeneration"
  ],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/sample/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e"
  ],
  "timeout_seconds": 1800,
  "deferred": [
    4,
    5,
    6
  ],
  "approved_proofs": []
}
```

## Task 2: Prepared disposable project execution

**Priority:** Must. **Intent:** execute A5's ordered steps in one caller-prepared project.
**Owned:** `internal/testhelpers/oracle/cli.go`; new `prepared_project_test.go`,
`testdata/preparedprobe/main.go`, optional fixture files under
`testdata/correction-project/`, `prepared-project.md`,
`testdata/acceptance-correction-project.json`.
**Input:** original outputs [0,1], Task 1's starting-tree helper.
**Depends on:** Task 1. No dependency on Task 3.
Implement C-A5. Existing cli_test.go stays unchanged, enforcing the Run default.
The fixture executable uses only stdlib and emits names/booleans, never environment
values. prepared-project.md documents ownership, explicit EnvBase and failure handling.

- `TestPreparedProjectSequence`: caller writes a fixture file and creates a
  disposable Git repository; preparation invocation reads it and writes step-1,
  second preparation requires step-1 and writes step-2, tested invocation requires
  both and writes the final result. Assert same cwd/project/home/tmp and final
  files from outside the child; capture before and after around the tested step.
  Each call uses RunInProject and an explicit ChildEnv basis. A failed preparation
  returns its nonzero code and the test driver does not execute the final step.
- `TestPreparedProjectIsolation`: two independently materialised cases do not see
  each other's sentinel; later calls in one case see its earlier effects; Run's
  existing fresh-project test still passes.
- `TestPreparedProjectEnvironment`: explicit fixture extras and stdin arrive;
  a host-only sentinel name does not; prohibited policy-name override fails before
  executing the fixture; stdout/stderr and nonzero exit remain distinct.
- `TestPreparedProjectInvalidDirectories`: relative/missing/file-valued project,
  home or tmp fails before process execution without clearing a valid fixture.
  Error tests report names/booleans only.
- `TestProviderCorrectionA5Start`: shared helper runs these tests against the
  real start export, compiling the new API and fixture there.
  Read-only callers may run on independent cases concurrently for race proof;
  no concurrent mutation of the same fixture is promised.

**Done when:** sequence/isolation/environment/refusal and starting-tree tests
execute in V0, V1 passes, V2/V3 pass, and legacy Run tests remain unchanged.
Map AC-prepared-project to sequence/isolation/invalid-directory tests, environment
to its test, AC-run-defaults to existing cli_test.go, start-project to its wrapper,
stdlib and sample regeneration as Task 1. Manifest references only owned new proofs
or existing committed test files actually selected by the stated command index.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "internal/testhelpers/oracle/testdata/acceptance-correction-project.json",
  "obligations": [
    "AC-prepared-project",
    "AC-prepared-environment",
    "AC-run-defaults",
    "AC-start-project",
    "AC-stdlib-harness",
    "AC-sample-regeneration"
  ],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/sample/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e"
  ],
  "timeout_seconds": 1800,
  "deferred": [
    4,
    5,
    6
  ],
  "approved_proofs": []
}
```

## Task 3: Fixed-recipe SHA preservation

**Priority:** Must. **Intent:** retain A9's fixed SHAs through all harness comparisons.
**Owned:** `internal/testhelpers/oracle/normalise.go`; new `fixed_sha_test.go`,
optional fixtures under `testdata/correction-sha/`, `fixed-shas.md`,
`testdata/acceptance-correction-sha.json`.
**Input:** original outputs [0,1], Task 1's starting-tree helper.
**Depends on:** Task 1. No dependency on Task 2.
Implement C-A9. kind.go and golden.go remain unchanged: they already call the
configured Normaliser. fixed-shas.md documents registration before use, the
instance comparison methods, A9-first preprocessing, and unchanged D-3–D-5 defaults.

- `TestFixedSHAPreservation`: independently fixed 40/64-hex recipes, exact
  spelling, repeated occurrence, valid registration idempotence and invalid inputs.
  Unregistered same-length IDs, UUIDs and timestamps still normalise; one normaliser's
  registrations do not leak to another. Unknown SHA differing by one digit must
  not be treated as a retained SHA.
- `TestFixedSHAKinds`: state before/after, nested JSON string values, human table
  tokens, and A9-style preprocessed lease/volatile-identity tokens. Assert actual
  SHA bytes remain in all three forms with all other kind semantics unchanged.
- `TestFixedSHAGoldenComparison`: public instance CheckState/Check(JSON/Human)
  write/live in a disposable golden directory; matching fixed recipe passes;
  change one SHA digit and require a mismatch for each kind. Check output bytes
  contain the original SHA, not <ID> or a private sentinel. Default/package-level
  checks continue normalising the same SHA when no registration is used.
- `TestProviderCorrectionA9Start`: shared helper executes these tests at start.
  Run independent configured instances and concurrent read-only use after registration
  under V1; do not race registration against reads.

**Done when:** the named tests prove verbatim SHAs in state/json/human comparison
and refusal of an altered SHA; V0/V1/V2/V3 pass without old assertion/golden edits.
Map AC-fixed-sha to the first three tests, AC-start-sha to its wrapper,
AC-normalisation-defaults to the default checks and existing normalise/kind tests,
stdlib/sample obligations as Task 1.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "internal/testhelpers/oracle/testdata/acceptance-correction-sha.json",
  "obligations": [
    "AC-fixed-sha",
    "AC-start-sha",
    "AC-normalisation-defaults",
    "AC-stdlib-harness",
    "AC-sample-regeneration"
  ],
  "validation": [
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -race -count=1 ./internal/testhelpers/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 ./internal/oracle/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go run ./internal/testhelpers/oracle/cmd/regen -pkg ./internal/oracle/sample/...",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make lint",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp make test-e2e"
  ],
  "timeout_seconds": 1800,
  "deferred": [
    4,
    5,
    6
  ],
  "approved_proofs": []
}
```

## Spec Compliance Matrix

| # | Requirement (effective priority) | Source | Task(s) | Status |
|---|---|---|---|---|
| 1 | Must: root-only substitution preserving every non-root byte | a8-contract CONTRACT; a8-determinism Root token; assigned done_when | Task 1 | Covered |
| 2 | Must: raw prompt comparison detects non-root differences | goal-frozen Rendered agent prompts; provider-plan D-4/D-5 | Task 1 | Covered |
| 3 | Must: ordered preparation and operation share caller-materialised project | a5-contract Cases/Entry points; a5-dataflow | Task 2 | Covered |
| 4 | Must: explicit ChildEnv, caller extras, stdin, separated streams/exit; no host leakage | a5-determinism Environment; provider-plan D-6/D-14; guard G1.4 | Task 2 | Covered |
| 5 | Must: fixed recipe SHAs survive state/JSON/human comparison | a9-contract A9 normaliser; a9-dataflow | Task 3 | Covered |
| 6 | Must: other volatile IDs still normalise; A9 preprocessing precedes harness | a9-contract A9 normaliser; provider-plan D-3–D-5 | Task 3 | Covered |
| 7 | Must: existing APIs, callers, default behavior and committed goldens unchanged | assigned scope/done_when; goal-tests; provider-plan D-3–D-6 | Tasks 1–3 | Covered |
| 8 | Must: focused positive/negative proofs and race checks | assigned done_when; goal-proof E1; guard G1.4 | Tasks 1–3, V0/V1 | Covered |
| 9 | Must: stdlib-only start compilation and actual starting-commit regeneration | provider-contract Harness; goal-proof E5; assigned done_when | Tasks 1–3, start tests and V3 | Covered |
| 10 | Must: executable acceptance manifests with per-capability allocation | assigned description; three Acceptance Contracts | Tasks 1–3 | Covered |
| 11 | Must: correction explains D-3–D-6 versus opt-in capability | assigned description; Corrected Capabilities | Tasks 1–3 API notes; this plan | Covered |
| 12 | Must: no production, consumer, golden, ledger or regen-semantics changes | assigned scope; provider-contract Boundary; ownership | Tasks 1–3 exact ownership; review of diff | Covered |
| 13 | Must: independent consumer lineages stay held until implementation merges | assigned description; Provider Ask Reconciliation | Tasks 1–3 selected outputs; consumer resumption by operator after verification | Covered |
| 14 | Must: explicit ownership/dependency ordering | assigned scope; ownership | Tasks 1–3; Placement and Dependency Audit | Covered |
| E2E | N/A: no user-visible behavior; process composition is tested in the harness | Cross-cutting; goal-proof E1/E2 | Task 2; shared existing e2e regression V6 deferred | N/A |
| DOC | Must: test-support API correction notes | Cross-cutting; assigned scope | Tasks 1–3, distinct Markdown files next to APIs | Covered |

## Output

Three Must outputs correspond exactly to Tasks 1–3 and their anchored plan_ref.
All select original provider outputs [0,1]; outputs 1 and 2 depend_on ["0"] for
the shared starting-tree proof helper. No task_depends_on, supersedes, or optional
feature is needed. No runtime state or generated acceptance manifest is written
by this planning task; each coder commits its own manifest after implementing
the named proofs. Pre-existing sample and area test files may be manifest proof
references but remain outside child edit ownership.

Review checks production/consumer/golden absence from the diff as well as semantic
test assertions. Passing V0 cannot by itself prove all the requested capabilities:
each manifest must point at its named tests, and the starting wrapper must report
the selected tests actually ran. No alternate surface kind, unconditional
normalisation disable, old-test expectation change or regenerated existing golden
can satisfy this correction.
