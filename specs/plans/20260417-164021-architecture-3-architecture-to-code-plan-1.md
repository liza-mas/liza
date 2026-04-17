# Code Plan — Architect-Role Prompt Delta (architecture-3 output[1] → coding)

**Source architecture:** `specs/arch-plan/20260417-145405-architecture-3.md` §4 (Scope §1 — Architect-Role Prompt Delta).
**Goal spec:** `specs/goals/20260417-precommit-bootstrap.md` (acceptance criterion #2; §Preconditions lines 41-44).
**Upstream code-planning task:** `architecture-3-architecture-to-code-plan-1`.

This plan emits ONE coding task. Rationale in §3 below.

---

## 1. Scope Boundary (reminder)

In-scope files for this plan's downstream coding task:

- `internal/prompts/templates/blocks/architect_tools.tmpl` — JSON schema example line only (current lines 13-15). Surrounding tool documentation (lines 1-12, 16-22) must not change.
- `internal/prompts/templates/blocks/implementation_phase.tmpl` — architect branch only (`{{- else if eq .Role "architect"}}` block, current lines 133-169). Coder / code-planner / epic-planner / us-writer branches must not change.
- `internal/agent/prompt_test.go` — architect-specific prompt-snapshot assertions only (`TestBuildPromptWithContext_Architect` in the `architectE2EPipelineYAML` E2E test, around lines 2039-2141; any adjacent architect-unit test). Tests for other roles must not change.

Out of scope for this plan (by owner):

- `internal/prompts/templates/base_prompt.tmpl` — owned by `architecture-3-architecture-to-code-plan-0` (base-prompt narrowing, Scope §0 in the arch plan).
- `internal/prompts/role_context.go` — owned by `architecture-2-architecture-to-code-plan-0` (adds `PreCommitConfigExists`, `PreCommitBootstrapInFlight`, `PreCommitKind` fields to `RoleContextData`).
- `internal/models/task.go`, `internal/ops/proceed.go` — owned by `architecture-1` coding tasks (MERGED).
- `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md` — owned by `architecture-4-architecture-to-code-plan-0`.
- New template files or `pipeline.yaml` edits — explicitly rejected by arch-plan §1.4 (YAGNI; G2.2 conciseness).

---

## 2. Cross-Architect Ordering (documentation only)

The downstream coding task consumes `{{.PreCommitConfigExists}}`, `{{.PreCommitBootstrapInFlight}}`, `{{.PreCommitKind}}` from `RoleContextData`. Those fields are added by `architecture-2-architecture-to-code-plan-0`. Sibling `depends_on` is not available across architect groups (arch-plan §4.7 risk; task description "depends on architecture-2 coding tasks merging first — cross-architect ordering enforced by orchestrator phase gating, not by sibling depends_on").

Therefore this plan's output[] entry sets `depends_on: []` (empty). The orchestrator is responsible for phase-gating merge of this plan's child coding task behind `architecture-2-architecture-to-code-plan-0`'s child. If that ordering is violated the template will fail to render (undefined field) and the coding task's tests will fail — the failure mode is loud, not silent.

---

## 3. Why One Coding Task

Arch-plan §4.2 lists two file edits (`architect_tools.tmpl` schema-line tweak + `implementation_phase.tmpl` sub-section insertion). Both edits serve a single intent: **surface `kind: bootstrap-precommit` emission guidance to the architect**. The `architect_tools.tmpl` line only adds a "see BOOTSTRAP-PRECOMMIT REQUIREMENTS" pointer — it has no standalone meaning without the sub-section in `implementation_phase.tmpl`. Splitting them would produce a broken interim state (pointer to a non-existent sub-section) and double the review overhead for no parallelism gain (both files need the same snapshot-test update and go into the same prompt).

The snapshot-test update in `internal/agent/prompt_test.go` is a direct consequence of the template edits (tests fail until assertions match the new rendered text). TDD colocation rule (from bootstrap context: "Never split 'implement X' from 'test X'") confirms this stays in the same task.

Result: one atomic coding task.

---

## 4. Coding Task — Task 1 (→ output[0])

### 4.1 desc (verbatim for set-task-output)

Update the architect-role prompt to (a) document `kind` as an optional OutputEntry field with bootstrap-precommit as the initial valid value, (b) surface the omission rule keyed off {{.PreCommitConfigExists}} and {{.PreCommitBootstrapInFlight}}, (c) require the install-authorization clause verbatim from goal-spec §Preconditions lines 41-44 in the desc of any emitted kind: bootstrap-precommit entry, and (d) require the bootstrap done_when to capture two stack-specialized invariants (clean pre-commit run + dep-manager dev-dependency record). Two file edits: (1) internal/prompts/templates/blocks/architect_tools.tmpl — extend the JSON schema example (lines 13-15) to include 'kind' as the seventh optional field with the verbatim hint string from §4.3 of the architecture plan; (2) internal/prompts/templates/blocks/implementation_phase.tmpl — insert the BOOTSTRAP-PRECOMMIT REQUIREMENTS sub-section between current step 6 and step 7 of the architect branch (lines 154-160), with text character-identical to §4.4 of the architecture plan including the verbatim install-authorization clause and the two-invariant done_when pattern. The sub-section must be gated by the architect role (inside the existing {{- else if eq .Role "architect"}} branch). Consumes RoleContextData fields PreCommitConfigExists, PreCommitBootstrapInFlight, PreCommitKind that architecture-2 adds; depends on architecture-2 coding tasks merging first (cross-architect ordering enforced by orchestrator phase gating, not by sibling depends_on). See architecture plan §4 for full rationale, exact wording for both files, insertion points, why-conditional-on-emission rather than render-time, risks (template-data dependency, field rename), and prompt-snapshot test update obligation.

### 4.2 done_when (verbatim for set-task-output)

internal/prompts/templates/blocks/architect_tools.tmpl JSON schema line includes 'kind' as the seventh field with the verbatim hint string from §4.3 of specs/arch-plan/20260417-145405-architecture-3.md; internal/prompts/templates/blocks/implementation_phase.tmpl architect branch contains the BOOTSTRAP-PRECOMMIT REQUIREMENTS sub-section with text character-identical to §4.4 (including the verbatim install-authorization clause from goal-spec lines 41-44 and the two-invariant done_when pattern); template renders without error for RoleContextData{Role: 'architect', PreCommitKind: 'bootstrap-precommit', PreCommitConfigExists: false, PreCommitBootstrapInFlight: false}; rendered output contains the literal substring 'Set "kind": "bootstrap-precommit"'; rendered output for a non-architect role does NOT contain 'BOOTSTRAP-PRECOMMIT REQUIREMENTS'; go test ./internal/prompts/... ./internal/agent/... passes; existing prompt-snapshot/golden tests of the architect role are updated to reflect the new sub-section.

### 4.3 scope (verbatim for set-task-output)

internal/prompts/templates/blocks/architect_tools.tmpl (JSON schema line in the body only — do NOT alter surrounding tool documentation), internal/prompts/templates/blocks/implementation_phase.tmpl (architect branch only — lines 133-169 of the current file; do NOT modify coder/code-planner/epic-planner/us-writer branches), internal/agent/prompt_test.go (architect-specific test snapshots only — do NOT touch unrelated tests). MUST NOT modify base_prompt.tmpl (owned by output[0]). MUST NOT modify internal/prompts/role_context.go (owned by architecture-2). MUST NOT introduce new template files or pipeline.yaml changes.

### 4.4 spec_ref

`specs/goals/20260417-precommit-bootstrap.md#preconditions`

### 4.5 plan_ref

`specs/plans/20260417-164021-architecture-3-architecture-to-code-plan-1.md`

### 4.6 depends_on

`[]` (empty — see §2 above for cross-architect ordering rationale).

### 4.7 Implementation notes for the coder (non-binding, for orientation)

These notes help the coder find anchors quickly. They are NOT part of done_when or scope; verbatim text comes from the referenced arch-plan sections.

**Edit 1 — `internal/prompts/templates/blocks/architect_tools.tmpl`** (arch-plan §4.3):

- Current lines 13-15 contain the JSON schema example starting `JSON file contains: [` and ending `]`. Replace the inner object line with the version shown verbatim in arch-plan §4.3 "To:" block. The new object adds `, "kind": "<optional typed marker — see BOOTSTRAP-PRECOMMIT REQUIREMENTS in IMPLEMENTATION PHASE>"` as the seventh field after `"depends_on": ["0", "2"]`.
- No other lines in this file change.

**Edit 2 — `internal/prompts/templates/blocks/implementation_phase.tmpl`** (arch-plan §4.4):

- Insertion point: between current step 6 (lines 154-159, ending at `Extract each field verbatim from the arch doc — do not compose from memory or paraphrase.`) and current step 7 (line 160, `Update worktree artifacts required by DONE WHEN.`).
- Insert the text shown verbatim in arch-plan §4.4 "New sub-section text" code block. The block begins `BOOTSTRAP-PRECOMMIT REQUIREMENTS:` and ends `{{- end}}`. It contains the verbatim install-authorization clause from goal-spec §Preconditions lines 41-44. It uses the `{{- if eq .Role "architect"}}` inner guard per arch-plan §4.4 rationale (redundancy accepted for self-documenting role-specificity).
- Subsequent step numbers (current step 7 `Update worktree artifacts…`, step 8 `Validate done_when…`, step 9 `PRE-SUBMIT SELF-CHECK…`, step 10 `Submit…`) may remain as they are — the inserted block is unnumbered per arch-plan §4.4.

**Edit 3 — `internal/agent/prompt_test.go`** (arch-plan §4.6 / §7.1):

- `TestBuildPromptWithContext_Architect` (around lines 2039-2141) uses a `mustContain := []string{...}` list followed by a `mustNotContain` list. Add to `mustContain` at minimum: `"BOOTSTRAP-PRECOMMIT REQUIREMENTS"` and `"Set \"kind\": \"bootstrap-precommit\""`. The existing architect test config does not set the PreCommit* fields, so they render as zero values (`false`, `false`, empty string) — confirm this matches arch-plan §4.4's template rendering (the omission rule lines will show `PreCommitConfigExists = false` etc., which is the intended test shape).
- Non-architect tests (`TestBuildPromptWithContext_ArchitectureReviewer` around line 2143, and any `Coder`/`CodePlanner` tests) should add `"BOOTSTRAP-PRECOMMIT REQUIREMENTS"` to their `mustNotContain` list to falsify leakage.
- If the coder finds the `PreCommitKind` field missing from `RoleContextData` at implementation time, the cross-architect ordering documented in §2 has not yet landed — the coder must surface this via `liza mark-blocked` per bootstrap rules (section "OPERATIONAL RULES"); do NOT work around by hardcoding the literal string in the template.

### 4.8 TDD shape for the coder

1. Update `mustContain` / `mustNotContain` assertions first (test-first). Run `go test ./internal/agent/...` — fails.
2. Apply Edit 1 to `architect_tools.tmpl`. Run tests — still fails.
3. Apply Edit 2 to `implementation_phase.tmpl`. Run tests — passes (if `RoleContextData` has the three PreCommit* fields).
4. Run `go test ./internal/prompts/... ./internal/agent/...` — both packages green.
5. Run pre-commit on the three touched files before commit.

---

## 5. Risks & Non-Risks

### 5.1 Risk: `architecture-2` coding task not merged at implementation time

See §2. The orchestrator owns phase-gating across architect groups. If violated, `go test` fails loudly (undefined template field). Mitigation at coder level: `liza mark-blocked` with diagnosis; do not hardcode.

### 5.2 Risk: `RoleContextData` field rename during `architecture-2` review

Per arch-plan §4.7. The three names are uniquely searchable (`PreCommitConfigExists`, `PreCommitBootstrapInFlight`, `PreCommitKind`). Mechanical find-and-replace if the names change. No design impact on this plan.

### 5.3 Risk: Existing snapshot/golden files beyond `prompt_test.go`

Arch-plan §7.1 warns about "any other golden file under `internal/`". Scan for architect-prompt snapshots before submitting:

- `grep -r "ARCHITECT TOOLS" internal/` to find any file asserting architect prompt structure.
- If a golden-file testdata mechanism exists (e.g. `testdata/golden/*.txt`), regenerate per project convention.

If new snapshot files surface, they are in-scope for the coding task (same intent) — the coder extends the touched-file list and records the update in the commit body.

### 5.4 Non-risk: G2.2 (contract conciseness)

Arch-plan §4.7 and §7.3 establish the sub-section is ~18 lines operationalizing goal-spec lines 41-44, 96-110, 138-159 (distillation ratio ~12%). Well within the G2.2 budget. No ADR-level conciseness concern.

### 5.5 Non-risk: Base-prompt narrowing (Scope §0) ordering

Arch-plan §7.5 lists "§1 lands before §0" as an acceptable interim state (architect sub-section cross-references base-prompt wording, but base-prompt wording may still be the pre-narrowed form — semantics slightly muddy, no incorrect behavior). This plan's coding task does NOT depend on Scope §0 merging first.

---

## 6. Spec Compliance Matrix

Requirements extracted from (a) goal-spec `specs/goals/20260417-precommit-bootstrap.md` filtered to the subset this plan owns per arch-plan §6 (acceptance criterion #2); (b) arch-plan §4 (Scope §1); (c) this task's `done_when`.

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Architect prompt requires the install-authorization clause verbatim from goal-spec §Preconditions lines 41-44 when emitting `kind: bootstrap-precommit` | goal-spec §Preconditions lines 41-44; arch-plan §4.1(c), §4.4 | Task 1 | Covered |
| 2 | Architect prompt documents `kind` as an optional OutputEntry field with `bootstrap-precommit` as the initial valid value | arch-plan §4.1(a), §4.3 | Task 1 | Covered |
| 3 | Architect prompt surfaces omission rule keyed off `{{.PreCommitConfigExists}}` and `{{.PreCommitBootstrapInFlight}}` | arch-plan §4.1(b), §4.4 OMISSION RULES | Task 1 | Covered |
| 4 | Bootstrap `done_when` pattern captures two stack-specialized invariants (clean pre-commit run + dep-manager dev-dependency record) | arch-plan §4.1(d), §4.4 EMIT RULES (done_when sub-bullet); goal-spec Worked Example line 155-156 | Task 1 | Covered |
| 5 | `architect_tools.tmpl` JSON schema line (current lines 13-15) includes `kind` as the seventh optional field with the verbatim hint string from arch-plan §4.3 | arch-plan §4.2 (Files touched), §4.3 | Task 1 | Covered |
| 6 | `implementation_phase.tmpl` architect branch contains BOOTSTRAP-PRECOMMIT REQUIREMENTS sub-section with text character-identical to arch-plan §4.4, inserted between current step 6 and step 7 | arch-plan §4.2, §4.4 | Task 1 | Covered |
| 7 | Sub-section is gated by the architect role (inside the existing `{{- else if eq .Role "architect"}}` branch) | arch-plan §4.4 (inner `{{- if eq .Role "architect"}}` guard and outer branch) | Task 1 | Covered |
| 8 | Template renders without error for `RoleContextData{Role: "architect", PreCommitKind: "bootstrap-precommit", PreCommitConfigExists: false, PreCommitBootstrapInFlight: false}` | done_when (this plan §4.2) | Task 1 | Covered |
| 9 | Rendered architect output contains the literal substring `Set "kind": "bootstrap-precommit"` | done_when (this plan §4.2) | Task 1 | Covered |
| 10 | Rendered output for a non-architect role does NOT contain `BOOTSTRAP-PRECOMMIT REQUIREMENTS` | done_when (this plan §4.2); arch-plan §4.4 | Task 1 | Covered |
| 11 | `go test ./internal/prompts/... ./internal/agent/...` passes | done_when (this plan §4.2) | Task 1 | Covered |
| 12 | Existing prompt-snapshot/golden tests of the architect role are updated to reflect the new sub-section | done_when (this plan §4.2); arch-plan §7.1 | Task 1 | Covered |
| 13 | MUST NOT modify `base_prompt.tmpl` | scope (this plan §4.3) | Task 1 | Covered (scope boundary) |
| 14 | MUST NOT modify `internal/prompts/role_context.go` | scope (this plan §4.3) | Task 1 | Covered (scope boundary) |
| 15 | MUST NOT introduce new template files or `pipeline.yaml` changes | scope (this plan §4.3); arch-plan §1.4 | Task 1 | Covered (scope boundary) |
| E2E | End-to-end test coverage for new behavior | Cross-cutting | Task 1 (via prompt-snapshot tests in `internal/agent/prompt_test.go` E2E block) | Covered |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: the prompt templates ARE the documentation surface for the architect agent. Goal-spec and arch-plan already record the design; no user-facing documentation file is changed by this prompt edit. The `kind` field's formal documentation in ADR-0036 is owned by `architecture-4-architecture-to-code-plan-0`. | N/A |

No GAP rows.

---

## 7. Shared-File Audit

Files appearing in this plan's scope (Task 1 only):

- `internal/prompts/templates/blocks/architect_tools.tmpl` — only Task 1 touches it within this plan. Out-of-plan owner: none other.
- `internal/prompts/templates/blocks/implementation_phase.tmpl` — only Task 1 touches it within this plan. Out-of-plan owner: none other (sibling `architecture-3-architecture-to-code-plan-0` touches `base_prompt.tmpl`, not this file).
- `internal/agent/prompt_test.go` — only Task 1 touches it within this plan. Sibling `architecture-3-architecture-to-code-plan-0` may update `internal/prompts/builder_test.go` but not `internal/agent/prompt_test.go` per its scope statement; no conflict.

Single task means no intra-plan shared-file conflicts possible. No `depends_on` entries are required inside this plan.

---

## 8. Cross-Reference Consistency

This plan references:

- `architecture-3-architecture-to-code-plan-0` as owner of `base_prompt.tmpl` and `internal/prompts/builder_test.go`. The sibling-task list in the bootstrap context confirms its scope includes `base_prompt.tmpl` and `builder_test.go`. ✅
- `architecture-2-architecture-to-code-plan-0` as owner of `RoleContextData` extension (three PreCommit* fields). The sibling-task description in the bootstrap context confirms this: "Add the `internal/precommit/` package (precommit.go) exporting `const Kind = \"bootstrap-precommit\"` …". ✅
- `architecture-1` (MERGED) as owner of `OutputEntry.Kind` field — used only as context; no forward reference that requires verification inside this plan.
- `architecture-4-architecture-to-code-plan-0` as owner of ADR-0036 amendment. The sibling-task description confirms it applies the four-part amendment to ADR-0036. ✅

All cross-references check out.

---

## 9. Pre-Submit Self-Check Tracker

- [x] Task Decomposition Principle: single intent (architect-prompt delta), `done_when` spans one package boundary (internal/prompts + internal/agent test file — both render the same surface; ≤2 packages rule satisfied), atomic exception applies for TDD colocation.
- [x] `done_when` falsifiable: each clause specifies an observable outcome (literal substring assertions, non-architect role absence, go test exit code, verbatim text match).
- [x] Spec Compliance Matrix present with E2E and DOC impact rows.
- [x] output[] completeness: one entry with desc, done_when, scope, spec_ref, plan_ref populated; depends_on set (empty — no sibling ordering).
- [x] output[] parity: desc/done_when/scope in §4.1/§4.2/§4.3 will be copied character-identical into `set-task-output`.
- [x] Shared-file audit: single task; no intra-plan conflicts.
- [x] Cross-reference consistency verified in §8.

Ready to submit.
