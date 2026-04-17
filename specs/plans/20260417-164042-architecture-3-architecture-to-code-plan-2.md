# Code Plan — Greenfield Reproduction Scaffolding

**Task:** `architecture-3-architecture-to-code-plan-2`
**Parent arch plan:** `specs/arch-plan/20260417-145405-architecture-3.md` (scope §2 / output[2])
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md`

This plan decomposes the scaffolding scope (acceptance criterion #1, scaffolding half) into one atomic coding task. The task is the sole owner of four tightly-coupled deliverables — three additions to the goal spec plus one new script — all serving the single intent: **produce the reproduction harness and placeholder observation section that output[3] consumes**.

---

## 1. Decomposition Rationale

Four deliverables, single intent, single task. Justification:

- **Intent is singular**: "add scaffolding for the greenfield reproduction run". Each deliverable exists only because the run needs it (a procedure to follow, a script to invoke, a table to populate, a cross-reference to point readers at).
- **Deliverables are cross-referential**: the goal-spec §Capture text in §5.3 step 8 invokes the script by path (`bash scripts/repro/precommit-bootstrap-greenfield.sh "$REPRO_ROOT" proj`). Writing the procedure without the script creates a dangling reference; writing the script without the procedure creates an orphan. Splitting would require either a shared-file dependency or accepting a broken intermediate state.
- **No shared file across siblings**: split would gain parallelism but no merge-conflict reduction (the only shared surface is the goal spec, and that is internal to this task).
- **done_when is unified** (parent arch plan §5.8 defines it as one criterion): splitting would fragment the criterion across two tasks, making each task's done_when dependent on the other's artifacts.
- **Atomic exception applies** (per TASK DECOMPOSITION PRINCIPLE): spec updates and a small script are cohesive when the script is referenced from the spec.

Output[3] (execution) is a separate sibling task (out of scope here) and depends on this task's merge.

---

## 2. Coding Task Definition

### Task 1 — Greenfield reproduction scaffolding

**desc:**

> Scaffold the greenfield reproduction harness for the pre-commit bootstrap goal. Four in-scope edits, all written to comply with project BASH CONSTRAINTS (no `$()` in agent-facing commands, no `cd`+`git` chaining, no `git add -A`):
>
> 1. Append a new `## Greenfield Reproduction Procedure` section to `specs/goals/20260417-precommit-bootstrap.md`, inserted immediately after the existing `## Worked Example (Python + uv)` section and immediately before the existing `## Out of Scope` section. Subsection headings are exactly, in order: `### Setup`, `### Execution`, `### Capture`, `### Observation Recording`, `### Confirm-or-Divert Decision`. Step text inside `### Setup`, `### Execution`, `### Capture` is character-identical to §5.3 of the architecture plan (`specs/arch-plan/20260417-145405-architecture-3.md`). `### Observation Recording` contains a one-sentence pointer to the placeholder section added by deliverable 2 ("Populated after the run — see §Observed Failure Modes — Reproduction Run YYYY-MM-DD."). `### Confirm-or-Divert Decision` contains a one-sentence pointer to the placeholder subsection added by deliverable 2 ("Populated after the run — see the corresponding subsection under §Observed Failure Modes.").
>
> 2. Append a new `## Observed Failure Modes — Reproduction Run YYYY-MM-DD` section immediately after the procedure section added in deliverable 1 (and before `## Out of Scope`). The section contains: the intro line `Reproduction artifacts: \`<path under repo, e.g. specs/goals/precommit-bootstrap-repro-artifacts/>\` (committed as part of this run).`; a markdown table with columns `| # | Hypothesized mode (goal spec §Evidence, lines 14-17) | Observed? (yes/no/partial) | Evidence pointer | Notes |` and four data rows (one per hypothesized mode as enumerated in goal-spec §Evidence lines 14-17) where the `Observed?`, `Evidence pointer`, and `Notes` cells contain the literal placeholder character `…` (U+2026 horizontal ellipsis); a `### Newly observed modes (not in the hypothesized list)` subsection whose body is the literal line `<list of any additional failure modes discovered during reproduction; if none, write "None observed.">`; a `### Confirm-or-Divert Decision` subsection whose body is the literal paragraph `<one paragraph: either "Hypothesized modes confirmed; design ratified as written" or "Observed modes diverge from hypothesis: <summary>; design revisited at <link to revised section / new spec>.">`. Section structure matches §5.5 of the architecture plan verbatim.
>
> 3. Add a single line to goal-spec `## Evidence` immediately after current line 17 (the line reading `- DoD step "Pre-commit passes on touched files" (CORE.md Rule 3) becoming vacuous.`). The new line reads exactly: `Observed modes recorded in §Observed Failure Modes — Reproduction Run after reproduction lands.` (plain line, no leading hyphen — it is a cross-reference, not a bullet in the hypothesized-modes list).
>
> 4. Create a new executable file `scripts/repro/precommit-bootstrap-greenfield.sh`. The file starts with `#!/usr/bin/env bash` and sets `set -euo pipefail`. Interface: `bash scripts/repro/precommit-bootstrap-greenfield.sh <REPRO_ROOT> <cycle> [<phase>]` where `<REPRO_ROOT>` is a writable absolute path, `<cycle>` is the project subdirectory name (`proj`, `proj-parallel`, or any caller-chosen name), and `<phase>` ∈ `{setup, snapshot, capture}` defaulting to `capture` when absent. On invalid args or invalid phase, print usage to stderr and exit 2. Phase responsibilities (per arch plan §5.4):
>    - `setup`: `mkdir -p "$REPRO_ROOT/$cycle/specs/vision"`; write `README.md` (single line) and `specs/vision/greenfield.md` (minimal vision placeholder describing one trivial feature) only if absent; `git init "$REPRO_ROOT/$cycle"` only if `$REPRO_ROOT/$cycle/.git` absent; stage the two files explicitly by name (`git -C "$REPRO_ROOT/$cycle" add README.md specs/vision/greenfield.md`); commit via heredoc on stdin (`git -C "$REPRO_ROOT/$cycle" commit -F - <<'EOF' ... EOF`) with message `initial: README + greenfield vision`; run `liza init "$REPRO_ROOT/$cycle"` only if `$REPRO_ROOT/$cycle/.liza/state.yaml` absent (if `liza` not on PATH, print a warning to stderr and continue — the script does not install liza); print the recommended env-var exports (`LIZA_LOG_LEVEL=debug`, `LIZA_TEE_AGENT_OUTPUT=1`) to stdout as informational text (the script cannot propagate env to the parent shell).
>    - `snapshot`: `mkdir -p "$REPRO_ROOT/observations/$cycle/state-snapshots"`; `cp "$REPRO_ROOT/$cycle/.liza/state.yaml" "$REPRO_ROOT/observations/$cycle/state-snapshots/state-$(date -u +%Y%m%dT%H%M%SZ).yaml"`; if source state.yaml absent, print an error to stderr and exit 1.
>    - `capture` (default): `mkdir -p "$REPRO_ROOT/observations/$cycle"` and the subdirs `agent-outputs`, `prompts`, `state-snapshots`, `worktree-git-logs`; copy `"$REPRO_ROOT/$cycle/.liza/agent-outputs/"` (if present) into `"$REPRO_ROOT/observations/$cycle/agent-outputs/"` via `cp -R`; copy `"$REPRO_ROOT/$cycle/.liza/agent-prompts/"` (if present) into `"$REPRO_ROOT/observations/$cycle/prompts/"`; copy `"$REPRO_ROOT/$cycle/supervisor.stdout.log"` (if present) into `"$REPRO_ROOT/observations/$cycle/"`; for each directory under `"$REPRO_ROOT/$cycle/.worktrees/"`, run `git -C <dir> log --all --oneline --graph` and redirect stdout to `"$REPRO_ROOT/observations/$cycle/worktree-git-logs/<basename>.log"`; run `git -C "$REPRO_ROOT/$cycle" ls-tree HEAD -- .pre-commit-config.yaml` and redirect stdout to `"$REPRO_ROOT/observations/$cycle/precommit-config-presence.txt"` (empty stdout means absent at HEAD); write an `observations-summary.txt` listing the artifact subpaths and a reminder that the operator populates the goal-spec table cells. Missing source directories are non-fatal — the script skips them, emits a diagnostic to stderr naming each skipped source, and continues.
>
> Script body constraints: uses only `git`, `mkdir`, `cp`, `find`, `date`, `test`, `echo`, `printf`, shell builtins, and other POSIX utilities. No `curl`, no `wget`, no package installers, no `apt`, `brew`, `pip`, `npm`, `uv`, `go install`. Operates entirely under paths rooted at `$REPRO_ROOT` — does not write outside `$REPRO_ROOT`. The script body MAY use `$(...)` command substitution and `&&` chaining — BASH CONSTRAINTS govern only the agent's direct shell invocations, not the contents of a script file the agent commits. This exemption is documented in arch plan §5.4 ("script body may use any standard shell features … BASH CONSTRAINTS govern the agent's shell invocations, not the script file's contents"). The file is committed with mode 0755 (`chmod +x`).
>
> Scaffolding-only boundary: the coder MUST NOT execute the reproduction run, MUST NOT populate any `…` placeholder, MUST NOT create a `specs/goals/precommit-bootstrap-repro-artifacts/` directory, and MUST NOT modify any prompt template, any Go source, or any spec other than `specs/goals/20260417-precommit-bootstrap.md`. Populating the placeholders is the explicit scope of the sibling execution task (output[3] of the parent arch plan), which depends on this task's merge.

**done_when:** (verbatim from parent arch plan §5.8, with the inline dry-run test specification preserved)

> `specs/goals/20260417-precommit-bootstrap.md` contains the new "Greenfield Reproduction Procedure" section with subsection headings exactly as listed in §5.2 of `specs/arch-plan/20260417-145405-architecture-3.md` and step text character-identical to §5.3; `scripts/repro/precommit-bootstrap-greenfield.sh` exists, is executable (`chmod +x`), implements the responsibilities listed in §5.4, and exits 0 when invoked with `phase=capture` against a populated `$REPRO_ROOT` in a dry-run integration test (populated = a minimally-seeded `$REPRO_ROOT/<cycle>` produced by a prior `phase=setup` invocation plus a synthetic `.liza/state.yaml` and a synthetic `supervisor.stdout.log`, mirroring the post-run shape §5.4 consumes — script contains no install commands or curl/wget invocations); the goal spec contains a placeholder "Observed Failure Modes — Reproduction Run YYYY-MM-DD" section with the table structure from §5.5 — every table cell in the four hypothesized-mode rows contains the literal placeholder character `…` (so output[3] can detect "populated" by absence of `…`); the "Newly observed modes" subsection contains the literal text `<list of any additional failure modes discovered during reproduction; if none, write "None observed.">`; the "Confirm-or-Divert Decision" subsection contains the literal placeholder paragraph from §5.5; a one-line cross-reference is added to goal-spec §Evidence (after line 17) reading "Observed modes recorded in §Observed Failure Modes — Reproduction Run after reproduction lands." `go test ./...` is unaffected; no liza-internal Go code changes.

**Falsifiable sub-checks the reviewer will run (decomposed from the above):**

- `grep -n '^## Greenfield Reproduction Procedure$' specs/goals/20260417-precommit-bootstrap.md` returns exactly one match, positioned (by line number) between the existing `## Worked Example (Python + uv)` match and the existing `## Out of Scope` match.
- `grep -n '^### Setup$\|^### Execution$\|^### Capture$\|^### Observation Recording$\|^### Confirm-or-Divert Decision$' specs/goals/20260417-precommit-bootstrap.md` returns those five headings in that order under the new section.
- The text under `### Setup`, `### Execution`, `### Capture` is a diff-free match against §5.3 of the arch plan (diff tool: `diff` against extracted slices of both files; the coder includes the extraction commands in the PR description so the reviewer can replay them).
- `grep -c '…' specs/goals/20260417-precommit-bootstrap.md` returns a baseline count — specifically, the four hypothesized-mode rows contribute 4×3 = 12 `…` placeholders in the Observed?/Evidence pointer/Notes cells (other `…` occurrences in the arch-plan-derived §5.3 step text are incidental and do not invalidate the check; the reviewer validates the 12-count delta by scoping to the table region).
- `grep -n '^Observed modes recorded in §Observed Failure Modes — Reproduction Run after reproduction lands\.$' specs/goals/20260417-precommit-bootstrap.md` returns exactly one match, and that match's line number is greater than the line number of `- DoD step "Pre-commit passes on touched files" (CORE.md Rule 3) becoming vacuous.` and less than the line number of the next `##` heading.
- `test -x scripts/repro/precommit-bootstrap-greenfield.sh` exits 0.
- `grep -E '(curl|wget|apt-get|brew install|pip install|npm install|uv add|go install)' scripts/repro/precommit-bootstrap-greenfield.sh` returns no matches.
- Dry-run integration test (coder writes this as part of the task and documents it in the PR; reviewer replays): using a scratch `$REPRO_ROOT` under `/tmp`, run the script with `phase=setup`, seed `$REPRO_ROOT/<cycle>/.liza/state.yaml` with a minimal placeholder and `$REPRO_ROOT/<cycle>/supervisor.stdout.log` with a placeholder, then run the script with `phase=capture`. The second invocation exits 0 and creates `$REPRO_ROOT/observations/<cycle>/` with `agent-outputs/` (empty or absent — skipped non-fatally), `prompts/`, `state-snapshots/`, `worktree-git-logs/`, `precommit-config-presence.txt`, and `observations-summary.txt`. The integration test lives only as documented shell commands in the PR description (no committed test file — per scope, only two files are touched).
- `go test ./...` from the worktree root exits 0 and produces the same pass/fail set as on `main` at the worktree's merge-base (reviewer verifies no new failures; since no Go files are touched, the set must be unchanged).

**scope:**

> `specs/goals/20260417-precommit-bootstrap.md` (three additions only: new `## Greenfield Reproduction Procedure` section inserted between `## Worked Example (Python + uv)` and `## Out of Scope`; new `## Observed Failure Modes — Reproduction Run YYYY-MM-DD` placeholder section inserted after the procedure section and before `## Out of Scope`; one-line cross-reference added to `## Evidence` immediately after current line 17). `scripts/repro/precommit-bootstrap-greenfield.sh` (new file, mode 0755). No other file may be modified. Specifically MUST NOT modify: any prompt template under `internal/prompts/templates/`, any `*.go` source, any ADR under `specs/architecture/ADR/`, any other spec under `specs/`, any other script under `scripts/`, `CLAUDE.md`, `AGENTS.md`, `GUARDRAILS.md`, or `CORE.md`. MUST NOT create: `specs/goals/precommit-bootstrap-repro-artifacts/` (that directory is output[3]'s scope). MUST NOT execute the reproduction run or populate any `…` placeholder.

**spec_ref:** `specs/goals/20260417-precommit-bootstrap.md#acceptance-criteria` (acceptance criterion #1, scaffolding half — owning the "procedure document + capture script + placeholder section" deliverables; execution half is output[3]'s scope)

**plan_ref:** `specs/plans/20260417-164042-architecture-3-architecture-to-code-plan-2.md`

**depends_on:** none (no sibling code-planning task writes to either of this task's in-scope files; output[3], the execution sibling, depends on this task but the dependency is declared on that sibling, not here)

---

## 3. Risks & Mitigations

- **Risk: character-identity drift against arch plan §5.3.** The coder extracts step text from memory or paraphrase. **Mitigation:** task `desc` explicitly names the source (`specs/arch-plan/20260417-145405-architecture-3.md` §5.3) and requires a `diff` between extracted slices. Reviewer runs the diff.
- **Risk: `…` placeholder character confused with three ASCII periods (`...`).** **Mitigation:** task `desc` specifies `…` (U+2026 horizontal ellipsis) literally; arch plan §5.5 uses the same character; reviewer grep uses the unicode character directly.
- **Risk: script misbehaves when source directories are missing (e.g. `agent-outputs/` absent before the first supervisor run).** **Mitigation:** task `desc` specifies non-fatal skipping with stderr diagnostic — consistent with §5.4 "capture phase is idempotent; re-running overwrites prior captures" and with output[3]'s need to call `phase=capture` even mid-run.
- **Risk: BASH CONSTRAINTS flag script body for `$()`.** **Mitigation:** the exemption is documented in arch plan §5.4 and restated in task `desc`. If a reviewer flags a `$()` inside the script, the desc provides the authoritative justification.
- **Risk: goal-spec insertion point drift if another merge adds a new section between `Worked Example` and `Out of Scope`.** **Mitigation:** insertion is positionally specified ("immediately after `## Worked Example (Python + uv)` and immediately before `## Out of Scope`") rather than by line number. If the surrounding structure changes, the coder preserves the "after worked example, before out of scope" invariant.
- **Risk: `### Observation Recording` and `### Confirm-or-Divert Decision` subsections under the procedure section collide with the `## Observed Failure Modes` section's own "Newly observed modes" and "Confirm-or-Divert Decision" subsections.** They are distinct sections at different heading levels. The procedure section's `### Observation Recording` / `### Confirm-or-Divert Decision` subsections point to the `## Observed Failure Modes` section (the placeholder table sits there). **Mitigation:** task `desc` explicitly sets the procedure-section subsections as one-sentence pointers, leaving the substantive placeholder content in the `## Observed Failure Modes` section only. Reviewer can grep for heading uniqueness by level.

---

## 4. Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Append `## Greenfield Reproduction Procedure` section after `## Worked Example (Python + uv)` and before `## Out of Scope` with the five subsection headings exactly (Setup, Execution, Capture, Observation Recording, Confirm-or-Divert Decision) | Task description deliverable (1); arch plan §5.2 | Task 1 | Covered |
| 2 | Step text in §Setup, §Execution, §Capture character-identical to §5.3 of the arch plan | Task description deliverable (1); arch plan §5.3 | Task 1 | Covered |
| 3 | Append `## Observed Failure Modes — Reproduction Run YYYY-MM-DD` placeholder section after the procedure section, with the table structure from §5.5 and `…` in every Observed?/Evidence pointer/Notes cell across four hypothesized-mode rows | Task description deliverable (2); arch plan §5.5 | Task 1 | Covered |
| 4 | `### Newly observed modes` subsection contains literal angle-bracketed placeholder text from §5.5 | Task description deliverable (2); arch plan §5.5 | Task 1 | Covered |
| 5 | `### Confirm-or-Divert Decision` subsection contains literal angle-bracketed placeholder paragraph from §5.5 | Task description deliverable (2); arch plan §5.5 | Task 1 | Covered |
| 6 | One-line cross-reference added to `## Evidence` immediately after current line 17, text exactly `Observed modes recorded in §Observed Failure Modes — Reproduction Run after reproduction lands.` | Task description deliverable (3) | Task 1 | Covered |
| 7 | Create `scripts/repro/precommit-bootstrap-greenfield.sh` as new, executable POSIX bash script with two-arg interface (REPRO_ROOT, cycle) plus optional third phase arg (setup\|snapshot\|capture, default capture) | Task description deliverable (4); arch plan §5.4 | Task 1 | Covered |
| 8 | `setup` phase automates §Setup steps 1-4 (mkdir, README/vision placeholders, git init, stage+commit, liza init if available, env-var informational output) | Task description deliverable (4); arch plan §5.4 | Task 1 | Covered |
| 9 | `snapshot` phase copies `.liza/state.yaml` to a timestamped path under `observations/<cycle>/state-snapshots/` | Task description deliverable (4); arch plan §5.4 | Task 1 | Covered |
| 10 | `capture` phase produces artifacts listed in §5.3 step 9 (state-snapshots, agent-outputs, supervisor log, worktree git logs, prompts, precommit-config-presence, observations-summary) | Task description deliverable (4); arch plan §5.3 step 9, §5.4 | Task 1 | Covered |
| 11 | Script uses only git/mkdir/cp/find/date/test/POSIX utilities; no install commands; no curl/wget; operates entirely under `$REPRO_ROOT` | Task description "Hard constraints" | Task 1 | Covered |
| 12 | Script body MAY use `$()` and `&&` internally (documented exemption) | Task description; arch plan §5.4 | Task 1 | Covered |
| 13 | Script exit code 0 on phase success | Task description; arch plan §5.4 | Task 1 | Covered |
| 14 | Dry-run integration test: `phase=capture` against populated `$REPRO_ROOT` exits 0 | done_when per arch plan §5.8 | Task 1 | Covered |
| 15 | `go test ./...` unaffected (no Go code changes) | Task description done_when | Task 1 | Covered |
| 16 | MUST NOT modify any prompt template, any Go source, any other spec, any other script | Task scope | Task 1 | Covered |
| 17 | MUST NOT execute the reproduction run or populate the observations table | Task scope | Task 1 | Covered |
| E2E | End-to-end test coverage for new behavior | Cross-cutting | Task 1 (script dry-run integration test documented in PR description; no new committed e2e because scope forbids new test files) | Covered (documented-only) |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 (the goal spec IS the documentation — deliverables 1-3 are the doc update) | Covered |

**Out-of-scope items (not given rows):** all acceptance criteria other than #1-scaffolding (covered by sibling architecture tasks per arch plan §6); hook-version selection; brownfield migration; artifact-directory creation; populating the `…` placeholders.

---

## 5. Pre-Submit Self-Check Results

- **(a) TASK DECOMPOSITION PRINCIPLE:** one task, single intent (scaffolding); done_when is unified; scope boundary is explicit; no conjunction smell at the intent level (the four deliverables are cohesive scaffolding). ✓
- **(b) done_when falsifiability:** each sub-check in §2 above is a concrete command or a concrete diff against the arch plan. No vague criteria. ✓
- **(c) Spec compliance matrix:** present (§4 above); every requirement mapped to Task 1; no GAP rows; E2E and DOC rows included with justification. ✓
- **(d) output[] completeness:** one entry covering one planned coding task; all required fields populated (desc, done_when, scope, spec_ref, plan_ref); no depends_on needed (no sibling code-planning task writes to this task's in-scope files). ✓
- **(e) output[] parity:** the `desc`, `done_when`, `scope`, and `spec_ref` strings in the `set-task-output` JSON payload are extracted from §2 of this plan character-identical (the submitter verifies by re-reading this plan before running `set-task-output`). ✓
- **(f) Shared-file audit:** `specs/goals/20260417-precommit-bootstrap.md` is shared with the execution sibling (output[3] of the parent arch plan), but that sibling is a separate code-planning task not planned here. The execution sibling's code-planning task will declare `depends_on: [this-task-id]` at the architect/orchestrator level. Within THIS plan, no shared file across siblings. ✓
- **(g) Cross-reference consistency:** all internal references in this plan point to §2 (the single task); the §Evidence cross-reference text is stated exactly and matches the task description. Arch-plan references (§5.2, §5.3, §5.4, §5.5, §5.8) are verified against the arch-plan file in the worktree. ✓

---

## 6. Submission Sequence

1. Run `liza write-checkpoint` declaring intent (single scaffolding task), validation plan (§2 sub-checks), and files to modify (`specs/plans/20260417-164042-architecture-3-architecture-to-code-plan-2.md`).
2. Commit this plan file.
3. Run `liza set-task-output` with the one-entry JSON payload constructed from §2 verbatim.
4. Run `liza submit-for-review`.
5. Run `liza await-verdict` (budget: 3 calls).
