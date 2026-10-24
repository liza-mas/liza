# Code Plan: D57 Oracle Proof-Scope Correction — A1 CLI Windows Guard in proof-scope.md

## Source References
Source revision: "8e93a3904630d04cc0362f1f2d85cc601f8215df"

### Direct References
- "d57": "specs/goals/20261023-architecture-consolidation-decisions.md#D57 — D53 extended: the A1 CLI oracle (D31 git-only PATH) is excluded on Windows (run, 2026-10-24)" @ "59cfa7f3aeebaede64b9eabed1d19a7fff1632de"
- "d53": "specs/goals/20261023-architecture-consolidation-decisions.md#D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)" @ "59cfa7f3aeebaede64b9eabed1d19a7fff1632de"
- "d31": "specs/goals/20261023-architecture-consolidation-decisions.md#D31 — A1 CLI oracle cases run in a disposable Git project with a git-only PATH (run, 2026-10-23)" @ "59cfa7f3aeebaede64b9eabed1d19a7fff1632de"
- "d52": "specs/goals/20261023-architecture-consolidation-decisions.md#D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)" @ "59cfa7f3aeebaede64b9eabed1d19a7fff1632de"
- "d55": "specs/goals/20261023-architecture-consolidation-decisions.md#D55 — E1 revised for new code plans: no per-task `go test -race` (human, 2026-10-24)" @ "59cfa7f3aeebaede64b9eabed1d19a7fff1632de"
- "boundary": "specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Area-Boundary Checkpoint Contract" @ "f3664d92729ce5aacd10390ed2f4e816606eb6f0"
- "fu-task1": "specs/plans/20261023-architecture-consolidation/20261024-181457-d53-oracle-portability-followup-plan.md#Task 1: Exclude the A1 CLI oracle on Windows through a TestMain runtime guard" @ "a45e08736501cb672243a9ac3214d3f50a0ab42c"
- "fu-rules": "specs/plans/20261023-architecture-consolidation/20261024-181457-d53-oracle-portability-followup-plan.md#Common Rules" @ "a45e08736501cb672243a9ac3214d3f50a0ab42c"
- "fu-placement": "specs/plans/20261023-architecture-consolidation/20261024-181457-d53-oracle-portability-followup-plan.md#Placement" @ "a45e08736501cb672243a9ac3214d3f50a0ab42c"
- "ps-doc": "internal/testhelpers/oracle/proof-scope.md#Oracle proof scope" @ "8e93a3904630d04cc0362f1f2d85cc601f8215df"
- "ps-windows": "internal/testhelpers/oracle/proof-scope.md#Windows-skipped scopes" @ "8e93a3904630d04cc0362f1f2d85cc601f8215df"
- "ps-all": "internal/testhelpers/oracle/proof-scope.md#Runs on all three platforms" @ "8e93a3904630d04cc0362f1f2d85cc601f8215df"

### Obligation Coverage
- "PS-recipe" -> "d57", "ps-doc"
- "PS-discover" -> "d57", "fu-task1", "ps-doc"
- "PS-guard" -> "d57", "fu-task1", "ps-windows"
- "PS-content" -> "d57", "d53", "d31", "ps-windows", "ps-all"
- "PS-hygiene" -> "fu-rules"
- "PS-preserve" -> "d57", "fu-task1", "fu-rules", "d55", "ps-doc"

## Intent and Allocation

Based on: the task JSON (incl. typed `provider_dependencies` / `descendant_dependencies` and
`max_outputs` = 1, RP-7); operator human notes on `d53-oracle-portability-followup-plan(-amend-1)` at
2026-10-24T18:29:46Z, 18:35:33Z, 18:58:04Z (route (a), withdrawn), 19:10:21Z (route (c)) and 19:24:18Z
(route-(c) steps done); D31, D52–D57 at `59cfa7f3a`; the merged follow-up plan at `a45e08736`
(Task 1, Common Rules, Placement, Output) and its output JSON; reads at `8e93a3904` of
`internal/testhelpers/oracle/proof-scope.md` (whole file, 59 lines), `internal/oracle/a1/cli/main_test.go`
(whole file), `internal/oracle/a1/cli/boundary_test.go:36-45`, `internal/oracle/a4/main_test.go:1-56`,
`internal/oracle/a1/persist/tamper_test.go:105-116`, `.pre-commit-config.yaml`; the Reality Probes below.
The canceled amendment (`backup/d53-followup-amend-1`) was not read or adopted.

Success means one docs-only Must output whose child makes `proof-scope.md` true for the merged D57
guard: `a1/cli` is listed as Windows-excluded by a `TestMain` runtime guard, `a1/persist` and `sample`
stay listed as portable, and the enumeration recipe finds that guard. Validate this plan by payload
preflight, Acceptance Contract/output parity and independent plan review.

Doc Impact: this plan and its output JSON; then the child's `internal/testhelpers/oracle/proof-scope.md`
and its acceptance manifest only. `rg -l 'proof-scope' docs specs/architecture` finds no other document
describing the oracle platform inventory; the boundary record (`boundary-a1.md`) stays with
`arm-1-ar-11-cp-0-code-0`. Test Impact: none — docs-only; the child records `tdd_not_required` (Task 1
step 5) and runs the existing guard tests unchanged as evidence.

| Task | Intent | Signature | Authority |
|---|---|---|---|
| 1 | Correct `proof-scope.md` for the D57 A1 CLI Windows guard and its enumeration recipe | `proof-scope.md:8` recipe cannot see the guard (RP-1); `:42` says `a1/cli` carries no platform skip (RP-2) | `d57`; operator route (c) 19:10:21Z |

**Priority:** Must (the task is Must; route (c) places it before the A1 boundary rerun). No Should/Could
work. Provider Ask Inventory: N/A — no shared interface; one Markdown file and one manifest. No new
semantic decision: the plan documents delivered behaviour (`fu-task1`, merged at `78b1f0c7f`).

## Common Rules

- **Docs-only.** The child edits only `internal/testhelpers/oracle/proof-scope.md` and its manifest. No
  Go file, test, assertion, fixture, golden, `PROVENANCE`, harness code (`internal/testhelpers/oracle/**`
  other than `proof-scope.md`), production code, CI or Makefile change.
- **Providers untouched.** D53 follow-up outputs 0–2 (`d53-oracle-portability-followup-plan-code-0..2`),
  their plan, acceptance contracts, manifests and order are not edited (`fu-task1`, `fu-placement`).
- **No race command (`d55`).** No `-race`, `make test-race` or full-suite command is listed; nothing
  executable changes.
- **Holds.** No human hold, boundary block or D52 human-action hold is cleared or bypassed; no push,
  dispatch or PR.
- **Pre-commit** on the two touched files before the canonical commands (`fu-rules` Proof).

## Task 1: Correct the oracle proof-scope inventory for the D57 A1 CLI Windows guard

**Priority:** Must (`d57`).
**Direct references:** ["d57", "d53", "d31", "d52", "d55", "fu-task1", "fu-rules", "ps-doc", "ps-windows", "ps-all"]

**Owned files:** `internal/testhelpers/oracle/proof-scope.md`; manifest
`specs/plans/20261023-architecture-consolidation/acceptance/d57-oracle-proof-scope-plan-proof-scope.json`.
**Not edited:** every `internal/oracle/**` file (`a1/cli/main_test.go` and `boundary_test.go`
included), other `internal/testhelpers/oracle/` files, the D53 follow-up plan, its output JSON and
manifests, production, CI.

Edit exactly these four places of `proof-scope.md`; every other line stays byte-identical.

1. **Recipe (`:7-9` code block).** Replace the command with
   `git grep -nE '^//go:build|t\.Skip\(|os\.Exit\(0\)' -- internal/oracle`. Directly after the block,
   add one sentence: an `os.Exit(0)` hit in a `TestMain` before `oracle.RunMain` is a package-level
   runtime guard (the package reports pass with no test run); the A4 hit (`a4/main_test.go`,
   `a4-helper` subprocess mode) is helper dispatch, not a scope (RP-3). No other new hit exists at
   `8e93a3904` (RP-2); if the child's HEAD shows another `os.Exit(0)` hit, it classifies it the same
   way in that sentence or, if it is a scope this plan did not see, stops and reports.
2. **Authority (`:11`).** Name D57 beside D53 and D52.
3. **Windows-skipped scopes table.** Add one A1 row as the first data row (suite order):
   Suite `A1`; Scope `a1/cli` package (`TestMain` in `main_test.go`, decided by `windowsExclusion`);
   Mechanism: runtime guard — on Windows it prints one line naming D57/D31 and calls `os.Exit(0)` before
   `oracle.RunMain`; no `//go:build`, because `TestOracleCLIBoundary` rejects build restrictions; Source:
   D57 (D31 git-only PATH isolation unproven on Windows). Wording may be tightened; every listed fact
   stays.
4. **Runs on all three platforms (`:42`).** Restrict the A1 bullet to `a1/persist` and the `sample`
   goldens (no build constraint or platform skip; `a1/persist` branches on `runtime.GOOS` for Windows
   environment names in `tamper_test.go`, RP-8) and state that `a1/cli` runs on Linux and macOS only,
   pointing to the Windows-skipped scopes table. `a1/cli` appears nowhere else in that section.
5. **TDD waiver.** Before editing, write the pre-execution checkpoint with
   `--tdd-not-required "documentation-only: proof-scope.md and its acceptance manifest; no behaviour, test, fixture or golden change; the existing guard tests and the recipe grep are the evidence"`;
   repeat it in any later checkpoint before submission (MULTI_AGENT_MODE TDD Enforcement).
6. **Evidence.** Run the full recipe (canonical command 4) and record in the checkpoint that its only
   `os.Exit(0)` hits are the ones the sentence of step 1 classifies.

Unchanged: the Linux-only table, the other Windows-skipped rows, the non-Linux-twins bullet, Opt-in
proofs (its A1 rows state gates only) and Platform proof for N2.

**Done when:** `proof-scope.md` differs from `8e93a3904` only in the four places above; its recipe
contains `os\.Exit\(0\)` (`grep -nF` exits 0; it exits 1 at `8e93a3904`, RP-10); that recipe pattern
finds `internal/oracle/a1/cli/main_test.go`'s `os.Exit(0)` line (exits 1 with the old pattern, RP-9);
the Windows-skipped table has the A1 `a1/cli` row with the guard facts; the all-platforms section
names only `a1/persist` and `sample` for A1; `TestOracleCLIWindowsExclusion` and
`TestOracleCLIBoundary` pass unchanged; pre-commit passes; the checkpoint carries the TDD waiver and
the recipe classification; no other file changes.

Mappings: PS-recipe → `internal/testhelpers/oracle/proof-scope.md`, recipe block contains
`os\.Exit\(0\)`, index 2; PS-discover → `internal/oracle/a1/cli/main_test.go`, recipe pattern hits the
guard's `os.Exit(0)` line, index 3; PS-guard → `internal/oracle/a1/cli/main_test.go`,
`TestOracleCLIWindowsExclusion` (one D57/D31 line on windows, `""` on linux/darwin) and
`TestOracleCLIBoundary` (no build restriction), index 1; PS-hygiene →
`internal/testhelpers/oracle/proof-scope.md`, all applicable pre-commit hooks pass, index 0;
PS-content and PS-preserve use their `approved_reference_id`.

### Acceptance Contract

```json
{
  "version": 1,
  "manifest": "specs/plans/20261023-architecture-consolidation/acceptance/d57-oracle-proof-scope-plan-proof-scope.json",
  "obligations": ["PS-recipe", "PS-discover", "PS-guard", "PS-content", "PS-hygiene", "PS-preserve"],
  "validation": [
    "pre-commit run --files internal/testhelpers/oracle/proof-scope.md specs/plans/20261023-architecture-consolidation/acceptance/d57-oracle-proof-scope-plan-proof-scope.json",
    "env -i HOME=\"$HOME\" PATH=\"$PATH\" TMPDIR=/tmp go test -count=1 -v -run '^(TestOracleCLIWindowsExclusion|TestOracleCLIBoundary)$' ./internal/oracle/a1/cli/",
    "grep -nF 'os\\.Exit\\(0\\)' internal/testhelpers/oracle/proof-scope.md",
    "git grep -nE '^//go:build|t\\.Skip\\(|os\\.Exit\\(0\\)' -- internal/oracle/a1/cli/main_test.go",
    "git grep -nE '^//go:build|t\\.Skip\\(|os\\.Exit\\(0\\)' -- internal/oracle"
  ],
  "timeout_seconds": 600,
  "approved_proofs": [
    {"obligation_id": "PS-content", "reference_id": "d57", "rationale": "Prose accuracy cannot be asserted by a test: review proof-scope.md against main_test.go and boundary_test.go — the Windows-skipped A1 row names a1/cli, TestMain/windowsExclusion, the one-line D57/D31 notice, os.Exit(0) before oracle.RunMain and why no build constraint is used; the all-platforms section keeps a1/persist and sample and no longer lists a1/cli; the authority line names D57; the recipe note classifies every os.Exit(0) hit printed by command 4 (a1/cli guard, a4 helper dispatch)."},
    {"obligation_id": "PS-preserve", "reference_id": "d57", "rationale": "Review the diff: only proof-scope.md (the four planned places) and the manifest change; no Go, test, fixture, golden, harness, production, CI or D53 follow-up artifact changes; no race command; no hold cleared."}
  ]
}
```

## Dependencies and Shared-File Ownership

| Output | Waits on | Earliest sufficient artifact | Priority |
|---|---|---|---|
| 0 (Task 1) | `provider_dependencies`: `d53-oracle-portability-followup-plan` / `code-plan-to-coding` / outputs `[0,1,2]` | output 0's merge (`78b1f0c7f`, the guard the doc describes); outputs 1–2 add no input and are selected only for the complete placed-writer coverage that the boundary reservation with `max_outputs` = 3 requires (task scope) | Must |

The declaration is carried on the sole output itself, not left to the task's
`descendant_dependencies` or to prose. All three selected children are MERGED at `8e93a3904` (RP-7),
so the wait is satisfied at generation. No `depends_on`, `task_depends_on` or `inherit_inputs`; no
edge to or from `arm-1-ar-11-cp-0-code-0`. The selected providers are Must, so no Must→optional path
exists.

| File or definition | Writers | Ordering |
|---|---|---|
| `internal/testhelpers/oracle/proof-scope.md` | Task 1 only among open tasks; earlier writer `d53-oracle-portability-plan-code-3` (MERGED, `0f7023a91`) | Sole open writer (RP-6) |
| Acceptance manifest | Task 1 | New path |

## Placement

Rule: `d53` Order — corrections land before the A1 boundary's G8; the boundary then rebases, reruns
G1–G7 at the corrected integration SHA and holds for one D52 dispatched run at that exact SHA, green
on Linux, Windows and macOS (`d52`, `boundary`). The operator's route-(c) notes (19:10:21Z, 19:24:18Z)
place this docs correction under that rule: after follow-up output 0, before the A1 boundary rerun.
Correction lineage: this plan (new; supersedes nothing; adds no fourth output to the follow-up plan).

- **Predecessor:** `d53-oracle-portability-followup-plan-code-0` (MERGED; guard commit `78b1f0c7f`),
  declared as the typed provider wait selecting outputs `[0,1,2]`; `-code-1` and `-code-2` are MERGED
  and write no file of this task.
- **Proof-scope writer audit (RP-6):** the only writer of `proof-scope.md` in history is
  `0f7023a91` (`d53-oracle-portability-plan-code-3`, MERGED). Among 244 tasks, only that task, its plan,
  the ABANDONED `d53-oracle-portability-followup-plan-amend-1` and this task name `proof-scope`; no
  DRAFT_CODE or BLOCKED scope names `internal/testhelpers/oracle`. No started writer exists.
- **Successor:** `arm-1-ar-11-cp-0-code-0` (BLOCKED A1 boundary). The orchestrator holds it on this
  plan's sole generated output with its supported `reserve-provider`; this plan adds no edge to it.
- **Slot:** pre-boundary — immediately on current integration (`8e93a3904` or later), after D53
  follow-up outputs 0–2 merged and before the boundary rebases, reruns G1–G7 and awaits the
  exact-SHA green D52 Linux/Windows/macOS run. D55 and every human hold are unchanged.

## Reality Probes

All probes ran at `8e93a3904630d04cc0362f1f2d85cc601f8215df` in this worktree, 2026-10-24T19:51Z–19:56Z.

| ID | Assertion / command | Observation |
|---|---|---|
| RP-1 | `git grep -nE '^//go:build\|t\.Skip\(' -- internal/oracle` (current recipe) | 64 hits; A1/sample hits only the opt-in `t.Skip` in `a1/cli/tamper_test.go:25`, `a1/persist/tamper_test.go:28`; 0 `os.Exit(0)` hits — the D57 guard is invisible |
| RP-2 | `git grep -nE '^//go:build\|t\.Skip\(\|os\.Exit\(0\)' -- internal/oracle` (canonical 4) | 66 hits; the two new: `internal/oracle/a1/cli/main_test.go:16: os.Exit(0)`, `internal/oracle/a4/main_test.go:32: os.Exit(0)` |
| RP-3 | read `internal/oracle/a4/main_test.go:15-35` | `os.Exit(0)` ends the `a4-helper` subprocess branch after printing a summary; normal runs reach `oracle.RunMain(m)` — not a scope |
| RP-4 | `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -v -run '^(TestOracleCLIWindowsExclusion\|TestOracleCLIBoundary)$' ./internal/oracle/a1/cli/` (canonical 1) | 2 `--- PASS`, `ok … 0.002s` (wall time not measured); `main_test.go:13-29` guard prints `windowsExclusion` line and `os.Exit(0)` before `oracle.RunMain(m)`; `boundary_test.go:41` rejects `//go:build` / `// +build` |
| RP-5 | `pre-commit run --files internal/testhelpers/oracle/proof-scope.md specs/plans/…/acceptance/d53-oracle-portability-followup-plan-a1-windows-exclusion.json` (canonical 0 shape; an existing manifest stands in for the unborn one) | Passed: check-merge-conflict, check-json, end-of-file-fixer, trailing-whitespace, forbid-crlf, remove-crlf; all Go/Python hooks Skipped; rc 0, worktree clean (pre-commit 4.5.1) |
| RP-6 | `omni-ee get tasks --all --json` (full read: writer audit of all 244 tasks), filtered on scope/done_when/description for `proof-scope`, and on DRAFT_CODE/BLOCKED scope for `internal/testhelpers/oracle`; `git log -- internal/testhelpers/oracle/proof-scope.md` | matches: `d53-oracle-portability-plan` and `-code-3` (MERGED), `…-followup-plan-amend-1` (ABANDONED), this task; none for the second filter; one commit `0f7023a91`. Non-merged: 92 DRAFT_CODE, 1 BLOCKED (`arm-1-ar-11-cp-0-code-0`), this task |
| RP-7 | `omni-ee get tasks.d57-oracle-proof-scope-plan.{provider_dependencies,descendant_dependencies,max_outputs} --json`; children of the follow-up plan | provider and descendant (at `code-plan-to-coding`) both select `d53-oracle-portability-followup-plan`/`code-plan-to-coding`/`[0,1,2]`; `max_outputs` 1; `-code-0` MERGED `286087ef6`, `-code-1` MERGED `a87cd78cc`, `-code-2` MERGED `8e93a3904`; follow-up `max_outputs` 3 |
| RP-8 | `git grep -nE 'runtime\.GOOS' -- internal/oracle/a1 internal/oracle/sample` | `a1/persist`: only `tamper_test.go:110` (Windows env names); `sample`: none; `a1/cli`: `helpers_test.go`, `isolation_test.go`, `tamper_test.go`, `main_test.go:14` |
| RP-9 | `git grep -nE '^//go:build\|t\.Skip\(\|os\.Exit\(0\)' -- internal/oracle/a1/cli/main_test.go` (canonical 3); same with the old pattern | new: `main_test.go:16: os.Exit(0)`, rc 0; old: rc 1 |
| RP-10 | `grep -nF 'os\.Exit\(0\)' internal/testhelpers/oracle/proof-scope.md` (canonical 2) | rc 1 at `8e93a3904` (red before the child's edit) |

**Budget.** Estimate (basis: RP-4/RP-5 completed interactively within single tool calls; wall times
not recorded): pre-commit < 30 s, guard tests < 60 s including a warm-cache compile, three greps < 1 s
each: < 2 min total. `timeout_seconds` 600 leaves at least 5× headroom for a cold Go cache and load. No
command is deferred; none is a long shared suite.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | One reviewed docs-only output assigns `proof-scope.md` a bounded correction (four named places, rest byte-identical) | task done_when; `d57` | Task 1 (PS-preserve) | Covered |
| 2 | Distinguish portable `a1/persist` and `sample` from Windows-excluded `a1/cli` | task done_when; `ps-all` | Task 1 steps 3–4 (PS-content) | Covered |
| 3 | Document the D57/D31 `TestMain` runtime guard, its one-line diagnostic and `os.Exit(0)` before `oracle.RunMain` | task done_when; `d57`; `fu-task1` | Task 1 step 3 (PS-content, PS-guard) | Covered |
| 4 | Enumeration recipe discovers the guard, not only build constraints and `t.Skip` | task done_when; operator 19:10:21Z | Task 1 step 1 (PS-recipe, PS-discover) | Covered |
| 5 | Unique ATX `plan_ref` | task done_when | Output (`#Task 1: Correct the oracle proof-scope inventory for the D57 A1 CLI Windows guard`) | Covered |
| 6 | Falsifiable documentation checks | task done_when | Task 1 canonical 2–3 (exit 1 before, RP-9/10), approved proofs | Covered |
| 7 | Applicable pre-commit checks with matching filters | task done_when | canonical 0 (RP-5 hook selection) | Covered |
| 8 | Explicit docs-only TDD waiver instructions | task done_when; MULTI_AGENT_MODE | Task 1 step 5 | Covered |
| 9 | Machine-readable provider dependency selecting follow-up outputs `[0,1,2]`, output 0 semantic, complete coverage under `max_outputs` = 3, on the sole output | task done_when/scope | Dependencies; output `provider_dependencies` | Covered |
| 10 | Placement after output 0's implementation and before `arm-1-ar-11-cp-0-code-0` reruns G1–G7 and awaits exact-SHA green D52 CI; names predecessor, writer audit, successor, slot | task done_when/scope; `d53`; `d52`; `boundary` | Placement | Covered |
| 11 | No change to outputs 0–2, their acceptance or ordering; no fourth output or cap change | task done_when/scope; `fu-placement` | Common Rules; Task 1 Not edited | Covered |
| 12 | No production, harness code, test, assertion, fixture or golden change | task done_when | Common Rules; PS-preserve | Covered |
| 13 | Preserve every human hold and D55 (no race command) | task done_when; `d55` | Common Rules; Acceptance Contract (no race) | Covered |
| 14 | Content and recipe validated against the actual guard without changing tests | task scope | canonical 1, 3, 4 (PS-guard, PS-discover) | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: documentation-only, no behaviour change | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 (`proof-scope.md`) | Covered |

## Output

One entry, output[0] = Task 1, carrying the typed `provider_dependencies` above. Its `plan_ref`
anchors the Task 1 heading, whose Acceptance Contract matches the entry's ordered validation and
prerequisites. `desc`, `done_when` and `scope` are short projections of Task 1. This session writes
only the two planning artifacts.
