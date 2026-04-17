# Code Plan — ADR-0036 Amendment (architecture-4)

**Task:** `architecture-4-architecture-to-code-plan-0`
**Parent arch plan:** `specs/arch-plan/20260417-152601-architecture-4.md`
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md` (anchor `#q2-idempotency--plan-time-dedup-execution-time`)
**Target file (the only file edited):** `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`

This plan decomposes architecture-4's documentation-only amendment into **one** coding task. There is exactly one file to edit and no structural seam along which the four edits could be split without fragmenting a single ADR into multiple half-views. Splitting further would add merge-conflict risk (all four edits touch the same ADR), so a single-intent, single-file task is correct.

---

## Task 1 — Apply the four-part amendment to ADR-0036

### Intent (single)

Edit `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md` in place, applying exactly the four amendments specified in architecture-4's arch plan §2.1–§2.4. No other file is touched. No Go source, test, or configuration change.

### Scope boundary (file-level)

**In scope (single file, edit):**
- `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`

**Out of scope (MUST NOT be touched by this task — `git diff --name-only` after the edit MUST list exactly the one ADR file above and nothing else):**
- `specs/architecture/ADR/README.md` — the summary table row for ADR-0036 stays as-is (arch plan §4 "Not touched").
- `specs/architecture/ADR/0048-multi-phase-planning.md` — its `**Extends:** ADR-0036 …` line is unchanged (arch plan §4, §3.3).
- `specs/architecture/ADR/0055-integration-sub-pipeline.md` — its `**Extends:** ADR-0036 …` line is unchanged (arch plan §4, §3.3).
- `specs/architecture/ADR/0056-architecture-step-many-to-one-transitions.md` — its `**Extends:** ADR-0036 …` line is unchanged (arch plan §4, §3.3).
- `specs/architecture/ADR/adr-backfill-state.yml` — no mid-life revision entry (arch plan §4).
- `internal/models/task.go` — `Kind` field addition belongs to `architecture-1-architecture-to-code-plan-0`; explicitly out of scope here.
- `internal/ops/proceed.go` — dedup primitive belongs to `architecture-1-architecture-to-code-plan-1`; explicitly out of scope here.
- Any Go source, any test, any prompt template, any other ADR, any config.

### The four exact edit sites (verbatim anchor text)

Every anchor text below is quoted verbatim from the current ADR file (as read during planning) so the doer can locate each site by exact string match.

#### Edit site 1 — §2.1 Schema-block pointer (INSERT NEW LINE)

**Anchor (verbatim — the close of the `OutputEntry` ```go code block, ending ADR-0036 L33):**

```
type OutputEntry struct {
    Desc     string `yaml:"desc"`
    DoneWhen string `yaml:"done_when"`
    Scope    string `yaml:"scope"`
    SpecRef  string `yaml:"spec_ref"`  // optional
}
```

The closing triple-backtick fence of that code block is the anchor. Insertion point: **immediately after** the closing ```` ``` ```` and **before** the blank line that precedes the `- Stored directly on `Task.Output []OutputEntry`` bullet.

**Insert exactly (one blank line, then the block below, then one blank line before the existing `- Stored directly on …` bullet):**

```
> **Note:** The four-field schema shown above is the original (2026-03 backfill) form. The struct has since been extended with additive optional fields — see the Revision History at the end of this ADR.
```

Source: arch plan §2.1.

#### Edit site 2 — §2.2 Limitations-accepted bullet (REVISE, strike-through)

**Anchor (verbatim — ADR-0036 L68–L70, inside "Consequences → Limitations accepted"):**

```
**Limitations accepted:**
- Output[] entries must match the expected schema — no extensibility beyond the four fields
- Scope extensions are advisory — reviewer can still reject
```

**Replace with exactly (first bullet struck through, second bullet unchanged):**

```
**Limitations accepted:**
- ~~Output[] entries must match the expected schema — no extensibility beyond the four fields~~ *(revised — see Revision History)*
- Scope extensions are advisory — reviewer can still reject
```

Mandatory strike-through syntax on the revised bullet:

```
~~Output[] entries must match the expected schema — no extensibility beyond the four fields~~ *(revised — see Revision History)*
```

The `~~…~~` markers, the exact em-dash `—` (U+2014) between "schema" and "no extensibility", the italic pointer `*(revised — see Revision History)*`, and the single space between the closing `~~` and the opening `*` are all required verbatim. The second bullet (`Scope extensions are advisory — reviewer can still reject`) is **not** changed. Source: arch plan §2.2.

#### Edit site 3 — §2.3 Footer line (EXTEND)

**Anchor (verbatim — ADR-0036 L73, the last non-empty line of the file):**

```
*Reconstructed from commits 45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)*
```

**Replace with exactly:**

```
*Reconstructed from commits 45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06); revised 2026-04-17 (pre-commit bootstrap goal — see Revision History).*
```

The original three commit SHAs (`45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)`) MUST remain present, unaltered; the appended segment `; revised 2026-04-17 (pre-commit bootstrap goal — see Revision History).` is added inside the same italic span before the closing `*`. Source: arch plan §2.3 (pattern mirrors ADR-0046's mutable-footer precedent).

#### Edit site 4 — §2.4 Revision History section (INSERT NEW SECTION)

**Anchor (verbatim — the horizontal-rule separator on ADR-0036 L72, currently immediately above the footer line):**

```
---
*Reconstructed from commits 45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)*
```

(i.e., the two-line block formed by the `---` separator and the footer is the tail of the file.)

**Insertion point:** immediately above the `---` horizontal-rule separator that currently precedes the footer, i.e., after the last bullet of "Consequences → Limitations accepted" (the unchanged "Scope extensions are advisory — reviewer can still reject" bullet) and before the `---` separator. In the post-edit file, the ordering from the "Consequences" block onward MUST be, in order:

1. The end of "Consequences" bullets (closing with the unchanged "Scope extensions are advisory — reviewer can still reject" bullet).
2. A blank line.
3. The new `## Revision History` section (content below).
4. A blank line.
5. The existing `---` horizontal-rule separator (unchanged, preserved where it already sits).
6. The revised footer line (from Edit site 3).

This placement invariant matches arch plan §2.4's "Placement invariant" paragraph verbatim: Revision History sits **above** the `---`, footer remains **below** it.

**Insert exactly (the following block, including its headings and tables):**

```
## Revision History

### 2026-04-17 — Schema-extensibility policy revised

**Trigger.** The pre-commit bootstrap goal (`specs/goals/20260417-precommit-bootstrap.md`, Q2) required adding a typed marker field `kind` to `OutputEntry` (and propagated to persisted `Task`) as the stable dedup primitive. The schema change itself is specified in the architecture plan for task `architecture-1`: `specs/arch-plan/20260417-141659-architecture-1.md` (§2.1 and §2.2).

**What changed.**

The "Limitations accepted" bullet stating *"Output[] entries must match the expected schema — no extensibility beyond the four fields"* is rescinded. That constraint was always aspirational: the live `OutputEntry` struct in `internal/models/task.go:262-269` already carries three additive optional fields beyond the original four, and the persisted `Task` struct carries two of them in turn.

Actual prior `OutputEntry` schema-field additions (chronological):

| Field added | Introducing ADR | Extends link declared? |
|-------------|-----------------|------------------------|
| `DependsOn []string` | ADR-0048 (Multi-Phase Planning) | Yes — ADR-0048 line 71: `**Extends:** ADR-0036 ... DependsOn in OutputEntry.` |
| `PlanRef string` | *none* — added via commit `ef80d629` ("feat(ops,prompts): add plan_ref propagation", 2026-03-17) | No ADR link declared; retroactively acknowledged here. |
| `ArchRef string` | ADR-0056 (Architecture Step) | Yes — ADR-0056 line 116: `**Extends:** ADR-0036 ... arch_ref on output[].` |

The `kind` field (see architecture-1's arch plan §2.1 for the exact struct tag and placement, and §2.2 for the propagated field on `Task`) is the fourth such additive extension, shipping with the pre-commit bootstrap goal.

Distinct category — *channel reuse, not schema growth*: ADR-0055 (Integration Sub-Pipeline) reuses `output[]` to create fix-tasks and declares `**Extends:** ADR-0036 ... output[] drives fix-task creation.` (ADR-0055 line 94), but it adds no new field to `OutputEntry` or `Task`. It is not counted among the schema-extension precedents above; it is a channel-reuse precedent that this policy does not govern directly.

This revision formalizes the additive-field practice as policy (see Current Policy below), retroactively acknowledges `PlanRef` as a prior extension that never received its own ADR link, and records the `Kind` addition under the same policy.

**Current policy — schema extension of `OutputEntry` and `Task`.**

Additive optional fields on `OutputEntry` and the propagated persisted `Task` are permitted without superseding this ADR, subject to all of the following:

1. **Additive only.** New fields MUST be optional with a zero-value default that is behaviorally inert (`omitempty` on YAML and JSON tags; empty string or empty slice is the default).
2. **Backward-compatible on load.** State files written before the field existed must decode without error into a zero-valued field. No migration pass may be required.
3. **No required-field semantics.** An empty value MUST NOT cause `validateOutputEntry` (or any successor validator) to reject the entry. Enforcement of the new field's semantics is the responsibility of the consuming code paths, not the generic struct validator.
4. **Provenance via `Extends:` link.** The introducing ADR MUST declare `**Extends:** ADR-0036 (Structured Task Output) — <field name> on OutputEntry.` (or equivalent wording) in its Consequences section, so the cross-reference graph stays navigable without in-place edits here.
5. **No breaking removals.** Removing a field that has ever shipped requires a superseding ADR (this ADR remains the authoritative record of the additive policy). This revision does not remove any existing field.

**Historical record.** The original "no extensibility beyond the four fields" wording remains struck through in the Consequences section above, not deleted, so readers of this document can see exactly what constraint was revised.

**Non-changes.** Every other claim in this ADR remains in force: the `output[]` channel, `liza_set_task_output` write path, atomic overwrite semantics, per-subtask cardinality consumption, the `ScopeExtensionEntry` model, the checkpoint serialization path, the reviewer template block, and the `GetLatestScopeExtensions()` helper. `scope_extensions` policy is unchanged in every respect.

**Cross-references.**
- Goal: `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time`
- Schema delta (code-side): `specs/arch-plan/20260417-141659-architecture-1.md` §2.1 (`OutputEntry`), §2.2 (`Task`), §2.3 (propagation), §2.4 (replan propagation)
- Prior extensions: ADR-0048, ADR-0055, ADR-0056
```

The doer MUST preserve the exact ordering of:
- The five numbered policy items (1) Additive only → (2) Backward-compatible on load → (3) No required-field semantics → (4) Provenance via Extends: link → (5) No breaking removals — in that order.
- The three table rows: `DependsOn` → `PlanRef` → `ArchRef` — in that chronological order.
- The separate `Distinct category — *channel reuse, not schema growth*` paragraph BELOW the table (not inside it): ADR-0055 is mentioned only in that paragraph, never added as a fourth table row. Conflating the two categories is a spec violation.

Planner latitude granted by arch plan §6.1: whitespace and blank-line choices between the sub-sections above, and a minor explicit `<a id="revision-history-2026-04-17"/>` anchor before the `### 2026-04-17 …` heading if cross-renderer anchor stability is a concern. Not required; omit by default.

Source: arch plan §2.4.

### Five-item Current Policy list (reproduced verbatim for reviewer reference)

The Revision History's Current Policy list MUST contain exactly these five items, in this order:

1. **Additive only.** New fields MUST be optional with a zero-value default that is behaviorally inert (`omitempty` on YAML and JSON tags; empty string or empty slice is the default).
2. **Backward-compatible on load.** State files written before the field existed must decode without error into a zero-valued field. No migration pass may be required.
3. **No required-field semantics.** An empty value MUST NOT cause `validateOutputEntry` (or any successor validator) to reject the entry. Enforcement of the new field's semantics is the responsibility of the consuming code paths, not the generic struct validator.
4. **Provenance via `Extends:` link.** The introducing ADR MUST declare `**Extends:** ADR-0036 (Structured Task Output) — <field name> on OutputEntry.` (or equivalent wording) in its Consequences section, so the cross-reference graph stays navigable without in-place edits here.
5. **No breaking removals.** Removing a field that has ever shipped requires a superseding ADR (this ADR remains the authoritative record of the additive policy). This revision does not remove any existing field.

### Schema-field-addition table (reproduced verbatim for reviewer reference)

The Revision History's "Actual prior `OutputEntry` schema-field additions (chronological)" table MUST contain exactly these three rows in this order, and MUST NOT include a fourth row for ADR-0055:

| Field added | Introducing ADR | Extends link declared? |
|-------------|-----------------|------------------------|
| `DependsOn []string` | ADR-0048 (Multi-Phase Planning) | Yes — ADR-0048 line 71: `**Extends:** ADR-0036 ... DependsOn in OutputEntry.` |
| `PlanRef string` | *none* — added via commit `ef80d629` ("feat(ops,prompts): add plan_ref propagation", 2026-03-17) | No ADR link declared; retroactively acknowledged here. |
| `ArchRef string` | ADR-0056 (Architecture Step) | Yes — ADR-0056 line 116: `**Extends:** ADR-0036 ... arch_ref on output[].` |

Attribution contract:
- `DependsOn` → ADR-0048.
- `PlanRef` → commit `ef80d629`, with the explicit "no ADR link declared" note in the third column.
- `ArchRef` → ADR-0056.

ADR-0055 appears ONLY in the separate paragraph below the table (channel reuse, not schema growth). **Schema-field growth and `output[]` channel reuse MUST NOT be conflated in the table.**

### Test Impact

`Test Impact: none — existing tests cover (this is a documentation-only ADR edit; no Go code or runtime behavior changes; no new test is meaningful). Validation is the `grep` / `git diff --name-only` checks under "Validation steps".`

Rationale accepted per arch plan §7.2 ("No Go files change; no tests are added or modified; no behavior changes").

### Doc Impact

`Doc Impact: specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md (the ADR itself — the amendment IS the doc change). No sibling docs require updates: README.md row still matches, no other ADR references the "four fields" constraint, the goal spec already cites ADR-0036 by name so its reference remains valid.`

### Validation steps (all MUST pass before submitting for review)

Run from the task worktree root (`.worktrees/architecture-4-architecture-to-code-plan-0/`):

1. **Scope — exactly one file changed.**
   Command: `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-architecture-to-code-plan-0 diff --name-only`
   Expected output (exactly): `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Any other path appearing = violation; revert and restart.

2. **Strike-through bullet — single occurrence of the original wording (now inside `~~…~~`).**
   Command: `grep -n 'no extensibility beyond the four fields' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly **one** line of output. The matched line MUST begin with the `- ~~Output[]` prefix — i.e., the struck-through form. Zero matches or two-or-more matches = violation.

3. **Strike-through syntax is exact.**
   Command: `grep -n '~~Output\[\] entries must match the expected schema — no extensibility beyond the four fields~~ \*(revised — see Revision History)\*' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly **one** match (the revised bullet). Zero matches = the strike-through syntax was altered.

4. **Second bullet unchanged.**
   Command: `grep -n 'Scope extensions are advisory — reviewer can still reject' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match (same line it occupies today).

5. **Footer preserves original commit SHAs and date range.**
   Command: `grep -n '45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match, on the footer line.

6. **Footer extension is present.**
   Command: `grep -n 'revised 2026-04-17 (pre-commit bootstrap goal — see Revision History)' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match, on the same footer line as check (5).

7. **Revision History section exists.**
   Command: `grep -n '^## Revision History$' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match.

8. **Dated subsection exists.**
   Command: `grep -n '^### 2026-04-17 — Schema-extensibility policy revised$' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match.

9. **Schema-block pointer Note exists immediately below the struct code block.**
   Command: `grep -n '^> \*\*Note:\*\* The four-field schema shown above is the original' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
   Expected: exactly one match.

10. **No-touch check on ADR-0048/0055/0056 `Extends:` lines.**
    Command: `git -C /home/tangi/Workspace/liza/.worktrees/architecture-4-architecture-to-code-plan-0 diff -- specs/architecture/ADR/0048-multi-phase-planning.md specs/architecture/ADR/0055-integration-sub-pipeline.md specs/architecture/ADR/0056-architecture-step-many-to-one-transitions.md specs/architecture/ADR/README.md specs/architecture/ADR/adr-backfill-state.yml`
    Expected: empty output (none of these files was modified).

11. **Pre-commit passes on the one touched file.**
    Command: `pre-commit run --files specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`
    Expected: all hooks pass (or report "(no files to check)" for hooks inapplicable to Markdown). Markdown-affecting hooks MUST pass.

Steps (1), (2), (3), (5), (10) together discharge the done_when facets (a)–(f) verbatim.

### done_when for the coding child task

Code plan specifies:

- (a) the four exact edit sites in ADR-0036 with verbatim anchor text for each (the close of the `OutputEntry` ```go code block, the `Limitations accepted:` bullet list, the existing footer line, the end-of-Consequences insertion point for the new Revision History section), matching §2.1, §2.2, §2.3, and §2.4 of the architecture plan respectively;
- (b) the exact strike-through syntax `~~Output[] entries must match the expected schema — no extensibility beyond the four fields~~ *(revised — see Revision History)*` for the revised bullet;
- (c) the five-item Current Policy list from §2.4 reproduced in order (additive only; backward-compatible on load; no required-field semantics; provenance via Extends: link; no breaking removals);
- (d) the three schema-field-addition rows in the "Actual prior OutputEntry schema-field additions" table — DependsOn attributed to ADR-0048, PlanRef attributed to commit ef80d629 with "no ADR link declared", ArchRef attributed to ADR-0056 — AND the separate paragraph below the table noting ADR-0055 as channel reuse rather than schema growth (schema-field growth and output[] channel reuse MUST NOT be conflated in the table);
- (e) an explicit no-touch list for README.md and for ADR-0048/0055/0056 with the expectation that `git diff --name-only` after the edit returns exactly `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`;
- (f) a validation step that `grep -n 'no extensibility beyond the four fields' specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md` returns exactly one match after the edit (the struck-through bullet) and that the original footer commit SHAs `45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)` remain present in the footer line.

Plan reviewable and submitted for review.

### Spec refs
- `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time`
- `specs/arch-plan/20260417-152601-architecture-4.md` §2.1–§2.4 (primary source for every verbatim block above)
- `specs/arch-plan/20260417-141659-architecture-1.md` §2.1–§2.4 (cited inside the Revision History prose; read-only reference, not edited)

### Depends on

Nothing. This task is independent of `architecture-1-architecture-to-code-plan-0` and `architecture-1-architecture-to-code-plan-1` — the ADR documents the additive-fields policy, while architecture-1 ships the Go `Kind` field and dedup primitive. They may land in either order without breaking each other (arch plan §6.1). The task's sibling-consistency declaration: the Revision History's "what changed" and "Current policy" wording mirrors architecture-1's arch plan terminology (`Kind` struct field, `kind` YAML/JSON tag, `bootstrap-precommit` initial value), so both plans remain internally consistent.

---

## Spec Compliance Matrix

Requirements drawn from the task's `done_when` facets (a)–(f), from arch plan §2.1–§2.4, and from `SCOPE` / out-of-scope clauses.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Four exact edit sites specified with verbatim anchor text: close of `OutputEntry` ```go code block | done_when (a) / arch plan §2.1 | Task 1 (§"Edit site 1") | Covered |
| 2 | Four exact edit sites — `Limitations accepted:` bullet list anchor | done_when (a) / arch plan §2.2 | Task 1 (§"Edit site 2") | Covered |
| 3 | Four exact edit sites — existing footer line anchor | done_when (a) / arch plan §2.3 | Task 1 (§"Edit site 3") | Covered |
| 4 | Four exact edit sites — end-of-Consequences insertion point for Revision History | done_when (a) / arch plan §2.4 | Task 1 (§"Edit site 4") | Covered |
| 5 | Exact strike-through syntax `~~Output[] entries must match the expected schema — no extensibility beyond the four fields~~ *(revised — see Revision History)*` | done_when (b) / arch plan §2.2 | Task 1 (§"Edit site 2", §"Mandatory strike-through syntax") | Covered |
| 6 | Five-item Current Policy list reproduced in order (additive only; backward-compatible on load; no required-field semantics; provenance via Extends: link; no breaking removals) | done_when (c) / arch plan §2.4 | Task 1 (§"Five-item Current Policy list") | Covered |
| 7 | Three schema-field-addition rows — DependsOn via ADR-0048, PlanRef via commit ef80d629 with "no ADR link declared", ArchRef via ADR-0056 | done_when (d) / arch plan §2.4 | Task 1 (§"Schema-field-addition table") | Covered |
| 8 | Separate paragraph below the table noting ADR-0055 as channel reuse rather than schema growth; schema growth and channel reuse MUST NOT be conflated | done_when (d) / arch plan §2.4 | Task 1 (§"Schema-field-addition table", §"Edit site 4") | Covered |
| 9 | Schema-block pointer Note inserted after OutputEntry code block | arch plan §2.1 / description (1) | Task 1 (§"Edit site 1") | Covered |
| 10 | Footer extended to append `; revised 2026-04-17 (pre-commit bootstrap goal — see Revision History).` while keeping original SHAs | description (3) / arch plan §2.3 | Task 1 (§"Edit site 3") | Covered |
| 11 | Revision History inserted immediately above the footer's horizontal-rule separator | description (4) / arch plan §2.4 "Placement invariant" | Task 1 (§"Edit site 4", placement ordering) | Covered |
| 12 | Revision History includes trigger paragraph, three-row table, channel-reuse paragraph, five-item Current Policy list, historical-record clause, non-changes clause, and cross-references block | description (4) / arch plan §2.4 | Task 1 (§"Edit site 4") | Covered |
| 13 | Revision History cross-references point to goal-spec Q2 anchor and to `specs/arch-plan/20260417-141659-architecture-1.md` §2.1–§2.4 | description (4) / arch plan §2.4, §3.2 | Task 1 (§"Edit site 4" cross-refs block) | Covered |
| 14 | Explicit no-touch list for README.md and ADR-0048/0055/0056 | done_when (e) / arch plan §4 | Task 1 (§"Scope boundary — out of scope", Validation step 10) | Covered |
| 15 | `git diff --name-only` after edit returns exactly the one ADR file | done_when (e) / arch plan §7.3 | Task 1 (Validation step 1) | Covered |
| 16 | `grep -n 'no extensibility beyond the four fields' …` returns exactly one match (the struck-through bullet) after the edit | done_when (f) / arch plan §5.1 | Task 1 (Validation step 2) | Covered |
| 17 | Original footer commit SHAs `45150db, 8e75dee, df10fe7 (2026-03-05 to 2026-03-06)` remain in the footer line | done_when (f) / arch plan §5.4 | Task 1 (Validation step 5) | Covered |
| 18 | No Go code, tests, or configuration change | SCOPE / description trailing clause | Task 1 (§"Scope boundary", §"Test Impact") | Covered |
| 19 | `Kind` struct-field addition owned by architecture-1's code-planning children (explicitly out of scope) | SCOPE "Out of scope" | Task 1 (§"Scope boundary — out of scope") | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: documentation-only ADR amendment; no user-visible behavior changes (arch plan §7.2) | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | Task 1 (the amendment IS the doc change for ADR-0036; README.md row already accurate per arch plan §4) | Covered |

No GAP rows. All requirements mapped to Task 1 or justified N/A.

---

## Shared-file audit

Only one task exists in this plan and it edits exactly one file (`specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md`). No sibling task in this plan edits that file. The prior planning tasks (`architecture-1-architecture-to-code-plan-0` editing `internal/models/task.go`, `architecture-1-architecture-to-code-plan-1` editing `internal/ops/proceed.go`) do not intersect this ADR file. No `depends_on` needed across plans (arch plan §6.1).

---

## Cross-reference consistency

- "owned by `architecture-1-architecture-to-code-plan-0`" (the `Kind` Go struct-field addition): confirmed against that task's published scope — it edits `internal/models/task.go` to add the `Kind` field. That scope is disjoint from this plan's.
- "owned by `architecture-1-architecture-to-code-plan-1`" (the `proceed.go` dedup primitive): confirmed against that task's published scope — it edits `internal/ops/proceed.go`. That scope is disjoint from this plan's.
- Cross-references to `specs/goals/20260417-precommit-bootstrap.md#q2-idempotency--plan-time-dedup-execution-time` and `specs/arch-plan/20260417-141659-architecture-1.md` §2.1–§2.4 inside the Revision History text are read-only references; they name files/sections that already exist in the repo and that will continue to exist after this task lands.

---

## Summary

One child task. One file edited. Four precisely-located amendments. Out-of-scope files listed explicitly with a `git diff --name-only` gate and a multi-file `git diff` gate. Success criteria are verbatim `grep` checks mapped 1:1 onto `done_when` facets (a)–(f).
