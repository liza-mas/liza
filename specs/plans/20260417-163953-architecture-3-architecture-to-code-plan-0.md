# Code Plan — base_prompt.tmpl narrowing (architecture-3 §0)

**Parent task:** `architecture-3-architecture-to-code-plan-0`
**Architecture reference:** `specs/arch-plan/20260417-145405-architecture-3.md` §3
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md` §Preconditions / Prompt wording to narrow

## Scope summary

Single-intent textual narrowing of one bullet in the universal `BASH CONSTRAINTS` block of `base_prompt.tmpl`, plus the colocated assertion update in `builder_test.go`. No template-data variables introduced. No other prompt templates touched. No other test files touched. This plan covers exactly scope §0 of the architecture document; scopes §1, §2, §3 are owned by sibling code-planning tasks (`architecture-3-architecture-to-code-plan-1`, `…-2`, `…-3`).

## Why a single coding task

The architecture document §3.4 specifies a one-line replacement at `base_prompt.tmpl:36`. The test assertion at `builder_test.go:548` is the unit test that pins the rendered output for that specific line — the substring assertion list lives inside one `assertSection("bash-constraints", …)` call. TDD colocation applies (TASK DECOMPOSITION PRINCIPLE: "a behavior change and its unit tests are one intent, not two"). Splitting prompt edit from assertion update would (a) leave the repo with a failing test between merges, (b) violate atomic-intent for no parallelism gain (same files, must serialize anyway).

Per architecture §3.6 / Grep run during planning: no snapshot/golden file outside `base_prompt.tmpl` and `builder_test.go` contains the old wording (verified by `grep "install, bootstrap, or fix system-level tooling"` across the worktree — only the template, the test, the goal spec, and the architecture plan match; the latter two are documentation, not assertions). The scope therefore stays minimal and matches the parent task SCOPE statement verbatim.

## Task 1 — Replace base-prompt bullet and update assertion list

### Intent

Replace `internal/prompts/templates/base_prompt.tmpl` line 36 with the narrowed wording from goal-spec §Preconditions lines 25-27 (verbatim) and update the corresponding assertion in `internal/prompts/builder_test.go:548` from a single substring check against the old wording to three substring checks against the new wording per architecture plan §3.5.

### Current line (architecture §3.2)

```
- NEVER attempt to install, bootstrap, or fix system-level tooling.
```

### Replacement line (architecture §3.3, character-identical)

```
- NEVER install OS packages, language runtimes, IDE tooling, or global developer tools by default. Project-level config files (`.pre-commit-config.yaml`, `.editorconfig`, lint configs) are project work. Tools required to run those configs may be provisioned only when the task's explicit scope authorizes a project-scoped setup path; otherwise mark BLOCKED rather than mutating the host environment.
```

The leading hyphen and single space are preserved. Backticks around the three filenames are part of the verbatim text. No nested sub-bullets. No template actions (`{{ … }}`) introduced. Surrounding `BASH CONSTRAINTS` block context (lines 30-38, with 35 the `NEVER use !` line and 37 the `NEVER use "git add -A"` line) is preserved in order — line 36 is replaced in place between them.

### Assertion update (architecture §3.5)

In `internal/prompts/builder_test.go`, inside the `assertSection("bash-constraints", …)` call at lines 542-552, replace the single string literal at line 548:

```
"NEVER attempt to install, bootstrap, or fix system-level tooling",
```

with three string literals (each a stable substring of the new wording — these are the three substrings the architecture plan §3.5 specifies):

```
"NEVER install OS packages, language runtimes, IDE tooling",
"Project-level config files",
"mark BLOCKED rather than mutating the host environment",
```

Each substring appears exactly once in the new bullet text and is character-identical to a slice of it. The other entries in the slice (`"BASH CONSTRAINTS"`, `"NEVER combine cd and git in one command"`, etc.) are unchanged.

### Done when (verbatim — matches output[0].done_when character-identical)

internal/prompts/templates/base_prompt.tmpl line 36 (1-based, post-edit) contains the replacement text from §3.3 of specs/arch-plan/20260417-145405-architecture-3.md character-identical including backticks around the three filenames; internal/prompts/builder_test.go:548 assertion is updated to three substring checks per §3.5 ("NEVER install OS packages, language runtimes, IDE tooling", "Project-level config files", "mark BLOCKED rather than mutating the host environment"); go test ./internal/prompts/... passes.

### Scope (verbatim — matches output[0].scope character-identical)

internal/prompts/templates/base_prompt.tmpl (line 36 only), internal/prompts/builder_test.go (assertion at line 548 only). MUST NOT modify other prompt templates or test files. MUST NOT introduce new template variables.

### Spec ref (verbatim — matches output[0].spec_ref character-identical)

specs/goals/20260417-precommit-bootstrap.md#preconditions

(Canonical wording lives at lines 25-27 of that file; the architecture plan `specs/arch-plan/20260417-145405-architecture-3.md` §3 is the implementation prescription that this code-planning task derives from.)

### Plan ref (verbatim — matches output[0].plan_ref character-identical)

specs/plans/20260417-163953-architecture-3-architecture-to-code-plan-0.md

### Dependencies

None. This task touches no file shared with any sibling code-planning task in the architecture-3 family (`-1` touches `architect_tools.tmpl` + `implementation_phase.tmpl`; `-2` touches `goal spec` + new script; `-3` touches goal-spec table cells + new artifact directory). The architecture plan §2 (decomposition table) confirms scope §0 has no shared file with scopes §1, §2, §3.

The single substring-check that previously asserted the old wording was the **only** test-side reference to that bullet in the repo (verified via Grep at planning time across the worktree). No prompt-snapshot test outside `builder_test.go` references the old text.

### Risks

- **Snapshot churn (architecture §3.6):** verified absent — Grep across the worktree shows only template + test + spec + arch plan contain the old phrase. Spec and arch plan are documentation, intentionally not modified by this task.
- **Test failure on first compile:** TDD-internal — coder edits both files together inside the worktree before running `go test`. Per parent SCOPE the two files must change in the same commit anyway; this is the expected path.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Replace `base_prompt.tmpl:36` with narrowed wording (verbatim from goal-spec §Preconditions lines 25-27) | Goal spec §Preconditions / Prompt wording to narrow (lines 23-29); arch plan §3.3 | Task 1 | Covered |
| 2 | Preserve leading `- ` and surrounding BASH CONSTRAINTS block (lines 30-38) | Arch plan §3.4 | Task 1 | Covered |
| 3 | No new template-data variables introduced | Arch plan §3.4 / parent SCOPE | Task 1 | Covered |
| 4 | `internal/prompts/builder_test.go:548` assertion updated from one substring check to three substring checks against the new wording | Arch plan §3.5; parent done_when | Task 1 | Covered |
| 5 | `go test ./internal/prompts/...` passes | Parent done_when; arch plan §3.5 | Task 1 | Covered |
| 6 | No other prompt templates or test files modified | Parent SCOPE; arch plan §2 (scope §0 row) | Task 1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: prompt-text narrowing is a wording change inside the universal BASH CONSTRAINTS block. Behavior-level effect (agents emitting `.pre-commit-config.yaml` as project work) is exercised end-to-end by sibling code-planning tasks `-1`/`-2`/`-3` (architect-prompt delta + greenfield reproduction owns the e2e gate per goal-spec acceptance criterion #1). The unit-level assertion in `builder_test.go` is the appropriate test for this scope. | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: the goal spec (lines 25-27) and architecture plan (§3) are the documentation of this change and already exist — they are the source of the verbatim wording, not downstream consumers. ADR amendment for the related `kind` field is owned by `architecture-4-architecture-to-code-plan-0`. No user-facing docs reference the old `system-level tooling` wording. | N/A |

All rows Covered or N/A with justification. No GAP rows.

## Out of scope (this code-planning task)

- Architect-role prompt delta (`architect_tools.tmpl`, `implementation_phase.tmpl`) — owned by `architecture-3-architecture-to-code-plan-1`.
- Greenfield reproduction scaffolding (procedure doc, capture script, placeholder table) — owned by `architecture-3-architecture-to-code-plan-2`.
- Greenfield reproduction execution (live run, populated table, confirm-or-divert decision) — owned by `architecture-3-architecture-to-code-plan-3`.
- ADR-0036 amendment for the `kind` field extension — owned by `architecture-4-architecture-to-code-plan-0`.
- `internal/precommit/` package + RoleContextData extension — owned by `architecture-2-architecture-to-code-plan-0`.
- `kind` field on `OutputEntry`/`Task` + `proceed.go` dedup — owned by `architecture-1-architecture-to-code-plan-0` and `architecture-1-architecture-to-code-plan-1` (both already in CODE_PLANNING / DRAFT_CODING_PLAN status per blackboard).
