---
name: clean-code
description: Pre-commit Clean Code refactoring
---

Clean Code is a reader's gift — refactor for the next developer, not the compiler.

**Boy Scout Rule:** Leave the code cleaner than you found it.

# Input

| Argument | Scope Source | Pre-flight |
|----------|-------------|------------|
| (none) | `git diff --cached` | Full (staged changes required) |
| `<commit-sha>` | `git diff <sha>^..<sha>` | Tests + coverage + concurrent work only |

When a commit SHA is provided, changes are made directly to files — the user reviews and amends. Skip staging-dependent pre-flight steps (staged check, stash backup).

# Modes

| Mode | Scope | When |
|------|-------|------|
| **Staged** (default) | `git diff --cached` | Pre-commit cleanup |
| **Full-file** | Any file (no staging required) | Deeper refactoring session |

Announce mode: `"Cleaning in [mode]. Override?"`

# Exclusion Criteria

**Do not clean:**
- **Generated code** — will be overwritten on next generation
- **Vendored/third-party code** — clean upstream, not the copy
- **Code scheduled for deletion** — verify: open task/PR to remove?
- **Code with known bugs in staged area** — refactoring may mask the bug or make it harder to isolate. Fix bug first, then clean.
- **Files under active development on other branches** — see concurrent work check; requires acknowledgment

If exclusion is uncertain, ask. The default is to skip, not to clean.

# Pre-flight Checks

0. **Language detection** — detect from staged file extensions, load `languages/<lang>.md`. Populates `$TEST_CMD`, `$COVERAGE_CMD`, and all Tool Map variables. See Language-Specific Patterns. Mixed languages: see that section's mixed-language rule.

1. **Staged changes exist** *(skip when commit SHA provided)*
```bash
   git diff --cached --quiet && echo "Nothing staged" && exit 1
```

2. **Tests pass**
```bash
   $TEST_CMD || exit 1
```

3. **Coverage gate** (tiered by transformation risk)
  - Run: `$COVERAGE_CMD && diff-cover $COVERAGE_REPORT --compare-branch=HEAD`
  - `diff-cover` maps coverage to staged hunks (language-agnostic — any Cobertura XML)

  | Transformation Risk | Examples | Threshold |
  |---------------------|----------|-----------|
  | Mechanical | Rename, dead code removal, import cleanup | ≥30% |
  | Structural | Extract function, early return, split responsibility | ≥70% |
  | Behavioral-adjacent | CQS split, error handling isolation | ≥90% |

  - **Pairing:** Threshold = highest-risk transformation planned (applies to whole session)
  - **§BRAND_NAME_TITLE§:** Threshold caps allowed transformation set (see Mode-Specific Behavior)
  - **STOP if below threshold** — report uncovered lines (Pairing: all transformations blocked; §BRAND_NAME_TITLE§: only above-coverage transformations blocked)
  - Tools unavailable: warn, require explicit waiver (§BRAND_NAME_TITLE§: abort — no waiver mechanism)

4. **Diff size guard** — **STOP if >500 lines**: `"Reduce scope or switch to Full-file mode?"`

5. **Git stash backup** *(skip when commit SHA provided — the commit itself is the backup)*
```bash
   BACKUP=$(git stash create)
   [ -z "$BACKUP" ] && { echo "⚠️ No changes to backup — STOP"; exit 1; }
   git stash store -m "code-cleaner-backup-$(date +%s)" "$BACKUP"
   [ "$(git rev-parse stash@{0})" = "$BACKUP" ] || { echo "⚠️ Backup stash mismatch — STOP"; exit 1; }
```

6. **Concurrent work check**
```bash
   git diff --cached --name-only | while IFS= read -r file; do
     hits=$(git log --all --not HEAD --since="2 weeks" --oneline --source -- "$file" 2>/dev/null)
     [ -n "$hits" ] && echo "--- $file ---" && echo "$hits"
   done
```
  - Warn if other branches touch staged files. Not a hard stop — require acknowledgment.

**Pre-flight summary:**
```
Pre-flight:
  Staged files: N
  Tests: ✓ pass (X tests)
  Coverage: Y% of staged lines (threshold: ≥Z% — [risk level])
  Concurrent edits: none (or ⚠️ [file] — [branch])
  Backup: stash@{0} ✓ verified
Proceed (P)?
```

# Analysis Phase

Examine staged diff. For each change, identify:
1. **Clean Code violations** (see Principle Catalog)
2. **Bugs spotted** — flag separately, do not auto-fix

**Batch grouping:** Group by dependency chain (interacting transformations together). Independent transformations in separate batches. Within a batch: inner scope outward. When ambiguous: file proximity over principle similarity.

**Transformation classification:**

| Type | Scope | Risk | Extra validation |
|------|-------|------|-----------------|
| **Refactoring** | Within file/module | Low–Medium | Tests + types |
| **Restructuring** | Cross-module | High | Tests + types + import graph + circular dep check |

Restructuring (moving symbols, splitting modules, changing import hierarchy) requires: import/dependency graph before and after, `$IMPORT_CHECKER`, `$CYCLE_CHECKER` or clean build, test discovery verification, all consumers updated in same batch.

**Performance-sensitive code:** Extracting a function from a hot loop adds call overhead per iteration. Flag transformations touching hot loops, latency-critical paths, or large data structures in batch description. Not a blocker — reviewer awareness.

**Output format:**
```
Analysis:

Violations:
  - [file:lines] [principle] — [description]

Bugs identified (not auto-fixed):
  - [file:line] [description] — [suggested fix]

Proposed batches:
  1. [batch name] — [N transformations] (grouped by: [rationale])

Proceed with batch 1 (P)?
```

# Transformation Loop

For each batch:

1. **Describe** transformations textually
2. **Await approval**
3. **Apply** to files
4. **Validate:**
   a. Tests  b. `$TYPE_CHECKER` on touched files  c. Import consistency (extractions often leave stale imports)
  - ✓ Pass → **snapshot batch**, continue
  - ✗ Fail → **STOP**:
```
     Tests failed after [batch name].
     Options: (R)evert batch | (I)nvestigate | (F)orce continue
     (I): Show failure + affected code, propose hypothesis, await instruction. No autonomous fixing.
```
5. **Snapshot batch:**
```bash
   git stash create | xargs -I{} git stash store -m "clean-code-batch-N-$(date +%s)" {}
```

**Batch completion:**
```
Batch N applied:
  - [transformation 1]
  Tests: ✓ | Types: ✓ (or N/A) | Snapshot: stash@{0}

Next: [batch N+1 name] — [description]
Proceed (P) / Skip (S) / Stop (X)?
```

If no batches remain, proceed to Test Maintenance.

# Test Maintenance

**Trigger:** Any batch that extracts a function.

1. **Add unit tests** for extracted functions — test the function in isolation, not through the original caller. Cover edge cases that may have been implicit before.
2. **Remove redundant tests** — indirect tests (via caller) can be removed when direct tests exist. Keep integration tests verifying wiring.

```
Test maintenance: Added N, Removed M, Net +/- X
```

# Pre-commit Validation

After all batches:
```bash
   git diff --cached --name-only -z | xargs -0 pre-commit run --files
```
Also run `$TYPE_CHECKER` if project uses type checking.

| Result | Action |
|--------|--------|
| ✓ Pass | Final summary |
| ✗ Formatter | Apply output, re-run tests; revert if tests fail |
| ✗ Linter | Show violations, ask user |
| ✗ Type errors | Show errors, ask user |
| ✗ Unfixable (3 attempts) | Show failure, ask user |

If pre-commit not installed, skip with warning.

# Convergence

Loop terminates when: no violations remain, user stops, or **max 5 batches**.

**Max batches with violations remaining:**
```
⚠️ Max batches reached. Remaining violations:
  - [file:lines] [principle] — [description]
Options: (T)rack as follow-up | (C)ontinue 5 more | (S)top
```

**Idempotence:** Re-running on clean code must produce no changes.

**Final summary:**
```
Cleaning complete:

Batches: N | Transformations: M
  - [principle]: X instances
Bugs flagged: K
  - [file:line] [description]
Remaining violations: R

Backup: stash@{0} (pop/drop)
Batch snapshots: stash@{1}..stash@{N}

Suggested commit message:
---
refactor: [summary]

- [key transformation 1]
- [key transformation 2]
---
```

# Principle Catalog (Uncle Bob)

Equal priority — apply contextually.

| Principle | Signal | Transformation |
|-----------|--------|----------------|
| **Meaningful names** | Abbreviations, single letters, generic (`data`, `info`, `temp`) | Rename to express intent |
| **Small functions** | >20 lines, multiple indent levels | Extract function |
| **Single Responsibility** | Function does X and Y | Split into focused units |
| **DRY** | Repeated logic (not coincidental similarity) | Extract common abstraction |
| **Early return** | Nested conditionals, arrow code | Guard clauses |
| **No "what" comments** | Comment restates code | Delete or improve naming |
| **Explain "why" only** | Magic values, non-obvious decisions | Add rationale comment |
| **Immutability preferred** | Mutable state where avoidable | Use immutable structures |
| **One level of abstraction** | Function mixes high/low level | Extract or inline to normalize |
| **Command-Query Separation** | Function both mutates and returns | Split into command + query |
| **Minimal arguments** | >3 parameters | Introduce parameter object |
| **No flag arguments** | Boolean changes behavior | Split into two functions |
| **Error handling isolation** | Try/except mixed with logic | Separate error handling |
| **KISS** | Complex solution where simple one works | Simplify: fewer branches, less indirection, obvious over clever |
| **No nested ternaries** | Chained `? :` | if/else or switch |
| **Clarity over brevity** | Dense one-liners, clever compaction | Prefer explicit form; longer can be cleaner |
| **Dead code removal** | Unused imports/functions/variables, unreachable branches | Delete (use `$DEAD_CODE_TOOL`; require per-item approval — no batch deletes) |

## Language-Specific Patterns

Language file loaded at Pre-flight step 0, reused through Analysis and Transformation.

Provides:
1. **Tool Map** — `$TEST_CMD`, `$COVERAGE_CMD`, `$COVERAGE_REPORT`, `$TYPE_CHECKER`, `$DEAD_CODE_TOOL`, `$IMPORT_CHECKER`, `$CYCLE_CHECKER`. `$COVERAGE_CMD` must produce Cobertura XML at `$COVERAGE_REPORT`.
2. **Performance Patterns** — language-specific perf-sensitive transformations
3. **Idiom Patterns** — language-specific signals/transformations (extends Principle Catalog)

No language file: universal principles only, warn. Check `languages/` for available profiles.

**Mixed-language diffs:** Resolve per-batch — each batch targets one language. Pre-flight uses majority language's test/coverage tools; no majority → run each.

## Principle Conflicts

Priority ladder: **Correctness > Clarity > KISS > Context** (surrounding conventions break ties).

| Conflict | Resolution |
|----------|------------|
| DRY vs KISS | ≤3 lines used ≤2 times → prefer duplication. Extraction wins when shared concept has a meaningful name. |
| Small functions vs One level of abstraction | Keep together if splitting forces reader to jump between files for one logical operation. |
| Meaningful names vs Minimal arguments | Parameter object when names reveal hidden concept (`x, y, w, h` → `Rect`). Don't wrap unrelated params. |
| Immutability vs KISS | Prefer mutable when immutability requires copying large structures. Immutability wins at boundaries. |

No clear resolution: **flag conflict, present both options, do not choose.**

# Scope Discipline

**Staged mode:** Transform ONLY staged code. Propagation to unstaged files requires explicit approval. **Rename propagation gate:** If rename requires non-mechanical changes in other files, show affected files with descriptions and require confirmation (semantic complexity is the risk, not file count).

**Full-file mode:** Transform entire files with staged changes. Same propagation rules.

# Anti-patterns

**FORBIDDEN:**
- Refactoring without test coverage
- Mixing bug fixes with refactoring
- Touching unstaged files without approval
- Continuing after test failure without approval
- Renaming for preference (only for clarity)
- Modifying test assertions to pass (structural changes for extractions permitted — see Test Maintenance)
- Formatting-only changes (delegate to pre-commit; only touch formatting in lines already being refactored)
- Changing public APIs without explicit approval
- Over-compaction (if reviewer must "unpack" mentally, it's over-compacted)

**Extract/inline decision:** Extract when the name communicates intent better than inline code. Inline when the abstraction restates the code. "I'm not sure this is better" means don't do it.

# Mode-Specific Behavior

**Pairing (default):** All prompts apply as written. User confirms batches, chooses on failure, approves propagation.

**§BRAND_NAME_TITLE§ (multi-agent):** No interactive prompts.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| Mode announcement | Announce, no prompt |
| Pre-flight "Proceed?" | Auto-proceed if checks pass; coverage below threshold: downgrade; abort if <30% or non-coverage fail |
| Batch/transformation approval | Auto-proceed/apply |
| Test failure | Auto-revert batch |
| Between batches | Auto-proceed |
| Unstaged propagation | Auto within worktree |
| Public API changes | Allowed within task scope |
| Diff >500 lines | Abort, log anomaly |

**Coverage-gated downgrade (§BRAND_NAME_TITLE§ only):**

| Coverage | Allowed |
|----------|---------|
| ≥90% | All |
| ≥70% | Mechanical + structural |
| ≥30% | Mechanical only |
| <30% | Abort |

When downgraded, Analysis filters violations to allowed set. Log skipped violations.

§BRAND_NAME_TITLE§ anti-pattern overrides: "without user approval" → "without task scope authorization"; "explicit approval" → task scope serves as authorization.

# Integration

```
code → stage → **clean** → review → commit
```

Runs BEFORE code review. Reviewer sees clean code.

**Relations:** Testing skill if coverage insufficient. Code Review is complementary (cleaner: style/structure; review: correctness/architecture).
