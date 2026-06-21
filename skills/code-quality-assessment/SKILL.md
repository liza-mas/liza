---
name: code-quality-assessment
description: Quantitative and qualitative code quality assessment with prioritized refactoring recommendations
---

**REPORT_FILE** = `specs/architecture/code_quality_assessment.md`

**Target directories:** If the target directory for REPORT_FILE does not exist, create it. The assessment is the first artifact that justifies the directory's existence.

**Recommended Tools:** Make sure you've read ~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md
list_directory_tree and codebase_search (fast and token-efficient semantic search) may be specifically useful.

Quality is not a binary. Measure it, grade it, and direct investment where it will compound.

*Invoked for: periodic health checks, pre-refactoring assessment, onboarding orientation, or explicit code quality evaluation.*

---

# Process

Metrics Collection → Subsystem Analysis → Synthesis → Recommendations

**Templates anchor cognition.** Complete each phase before the next. The skill is a measurement framework to apply to what you found, not boxes to fill with platitudes.

**Report format:** See `references/report-format.md` for the output template.

---

# Modes

| Mode | When | Scope |
|------|------|-------|
| **Full Assessment** | First assessment, periodic health check, major milestone | All phases, all sections, full report |
| **Targeted Assessment** | Evaluate specific subsystem(s) or concern area | Scoped metrics + analysis for named components only |
| **Reassessment** | After refactoring or significant changes | Delta comparison against previous REPORT_FILE |
| **Enrichment** | Improve coverage of existing assessment | Independent analysis → merge → verify → update |
| **Quick Health Check** | Verify existing findings still hold | Metrics refresh + finding verification only |

**Phase applicability:**

| Mode | Phase 1 (Metrics) | Phase 2 (Subsystem Analysis) | Phase 3 (Synthesis) | Phase 4 (Recs) | Output |
|------|-------------------|------------------------------|---------------------|----------------|--------|
| **Full Assessment** | ✓ Complete | ✓ Complete | ✓ Complete | ✓ Complete | New REPORT_FILE |
| **Targeted Assessment** | Scoped | Scoped | Scoped | Scoped | Targeted section in REPORT_FILE |
| **Reassessment** | ✓ Fresh | Delta comparison | Update | Update | Revised REPORT_FILE |
| **Enrichment** | ✓ Fresh (independent) | Update (add to existing) | Update | Update | Revised REPORT_FILE |
| **Quick Health Check** | Refresh only | Verify only | — Skip | — Skip | Updated metrics + verification notes |

**Mode selection** (first match wins):
1. User explicitly requests Targeted → Targeted Assessment
2. User explicitly requests Reassessment → Reassessment
3. User explicitly requests Quick Health Check → Quick Health Check
4. REPORT_FILE exists → Enrichment
5. No existing report → Full Assessment

## Full Assessment

Use complete process: Phase 1 → Phase 2 → Phase 3 → Phase 4.

**Time Budget:** Phase 1 (Metrics) ~30% of effort. Phase 2 (Subsystem Analysis) ~40%. Phases 3+4 (Synthesis + Recommendations) ~30%. Most missed findings come from rushed metrics collection — especially the File Size Distribution scan. If you're tempted to skip ahead, you're under-investing in discovery.

**Pairing checkpoint:** After Phase 1, present the Metrics Dashboard and identified subsystems before proceeding to Phase 2. This catches scope gaps early (missed languages, wrong LOC counts, missing subsystems).

**Default output:** REPORT_FILE (if not specified and doesn't exist yet).

## Targeted Assessment

Scope to named subsystems only. Collect metrics only for the targeted components.

1. **Scope**: Confirm which subsystems/directories to assess
2. **Metrics**: Collect Phase 1 metrics scoped to targeted paths only
3. **Analysis**: Full Phase 2 for targeted subsystems only
4. **Output**: If REPORT_FILE exists, update targeted sections within it. If not, create a partial report covering only assessed subsystems.

## Reassessment

Requires existing REPORT_FILE.

1. Collect fresh metrics (full Phase 1)
2. For each subsystem, compare current metrics against previous
3. For each previous finding, verify: still relevant? severity changed? resolved?
4. Update ratings and grade
5. Highlight what changed. Tag changes: `*(reassessment YYYY-MM-DD)*`

## Enrichment

Same anti-anchoring protocol as the software-architecture-review skill.

**First check:** Verify REPORT_FILE exists. If it doesn't, this is Full Assessment, not Enrichment.

**Header check (BEFORE discovery):** Read REPORT_FILE until you find the `Mode:` line to extract:
- Pass number (e.g., "Mode: Enrichment (pass 3)")
- Previous lens from header
Your pass is N+1. Your lens continues the rotation from the previous lens.
**⚠️ STOP as soon as you have the Mode line. Do NOT scroll down or read findings.**

**⚠️ CRITICAL: You MUST NOT read REPORT_FILE findings until Step 2.** Reading findings early causes anchoring — you'll confirm existing findings instead of discovering new ones.

**Process:**

1. **Independent Analysis (Phase 1 + Phase 2)** — Complete as if no report exists. Explore the codebase fresh. Hold findings in memory. **Do not read the existing report.**

2. **Merge Phase** — *Only now* read REPORT_FILE. Compare your fresh findings against it.

3. **Verification** — For *each* finding in the existing report, verify:
   - Still accurate? (code may have changed)
   - Still relevant? (context may have shifted)
   - Severity still appropriate?
   - Mark each as: ✓ verified, ✗ stale, or ~ adjusted

4. **Gap Analysis** — List:
   - New findings from fresh pass not in existing report
   - Existing findings your fresh pass missed (and why)

5. **Update** — Revise REPORT_FILE with:
   - New findings added, tagged `*(pass N)*` or `*(pass N, [lens] lens)*`
   - Stale findings removed or marked resolved
   - Severity adjustments where warranted
   - Updated date and metrics in header

**Time Budget:** Independent Analysis (step 1) should be at least as thorough as merge + verification combined.

### Lens Rotation

Each enrichment pass uses a different primary lens. Continue from the previous pass's lens.

**Lenses:**
1. **Complexity** — LOC, function length, god classes/scripts, cyclomatic complexity, design-level complexity patterns
2. **Test Coverage** — Test gaps, untested critical paths, test quality patterns
3. **Dependencies** — Dependency count, staleness, vendoring, transitive depth
4. **Documentation** — Doc coverage, staleness risk, spec-code drift
5. **CI/Build** — Pipeline completeness, enforcement gaps, build hygiene

**Rotation order:**
Complexity → Dependencies → CI/Build → Test Coverage → Documentation → (wrap to Complexity)

The first 3 passes cover the highest-value lenses (Complexity, Dependencies, CI/Build) as primary.

**How to apply:** During Phase 1+2, start with your primary lens. Spend ~40% of discovery time on it before broadening. The leading lens gets deepest attention while context is fresh.

**Complexity lens — systematic scan:**
When Complexity is your primary lens:

1. **Structural scan** (start here): Run the LOC scan from Phase 1.3. Flag ALL files >500 LOC as
   potential god classes. For each, investigate before merge phase.

2. **Design-level scan** (after structural): For each complex function or file identified, ask
   **"why is this complex?"** before recommending how to fix it:
   - **Boolean-flag dispatch**: Function resolves a type/variant into boolean flags, then threads
     them through multiple phases. Fix: strategy/polymorphism, not extraction.
   - **Imperative ceremony**: Repetitive code that follows a pattern but isn't expressed as data.
     Fix: declarative definitions + loop, not file split.
   - **Responsibility mixing**: File contains unrelated concerns sharing no state. Fix: file split
     (this is the one case where extraction IS the right answer).
   - **Deep nesting**: Complex conditionals that could be flattened. Fix: early returns, guard
     clauses, or table-driven dispatch.

   The structural scan finds *where* complexity lives. The design-level scan identifies *what kind*
   of complexity it is. Different kinds need different remedies — recommending extraction for a
   design problem addresses the symptom, not the cause.

### Enrichment Iteration Guidance

**Recommended:** Run enrichment **3 times** for solid coverage. Additional passes provide diminishing returns.

**⚠️ MANDATORY after 3+ passes:** If pass number ≥ 3, present options before proceeding:
```
Pass [N] exists ([previous lens] lens). Per skill, 3 passes provide solid coverage.

Options:
1. Pass [N+1] Enrichment ([next lens] lens) — full independent discovery + merge
2. Reassessment — fresh metrics + delta comparison
3. Quick Health Check — verify existing findings still hold

Which approach?
```

## Quick Health Check

Fastest mode. No new discovery — verification only.

1. Refresh Phase 1 metrics
2. Compare against REPORT_FILE metrics — note any significant changes
3. For each existing finding, verify against current codebase state
4. Mark findings as: ✓ verified, ✗ stale, ~ adjusted
5. Update header date and metrics
6. Do NOT explore for new findings

---

# Phase 1: Metrics Collection

*Quantitative backbone of the assessment. Language-agnostic.*

## 1.1 Language Detection

Scan for manifest files to determine primary language(s):

| Manifest | Language |
|----------|----------|
| `go.mod` | Go |
| `package.json` | JavaScript/TypeScript |
| `pyproject.toml`, `requirements.txt`, `setup.py` | Python |
| `Cargo.toml` | Rust |
| `pom.xml`, `build.gradle`, `build.gradle.kts` | Java/Kotlin |
| `*.csproj`, `*.sln` | C#/.NET |
| `Gemfile` | Ruby |
| `mix.exs` | Elixir |

Multi-language projects: collect metrics per language, report the primary language first.

## 1.2 Repository Overview

Collect per language (prefer `cloc`, `scc`, or `tokei` when available; fall back to `wc -l`):

- **Production code**: LOC by language (exclude tests, vendored, generated)
- **Test code**: LOC across test files
- **Test-to-production ratio**: test LOC / production LOC
- **Test count**: number of test functions/cases
- **Documentation**: LOC across docs/specs/README files
- **Dependencies**: direct count from manifest files

**Language-specific collection hints:**

| Language | Production LOC | Test Files | Test Count | Dependencies |
|----------|---------------|------------|------------|--------------|
| Go | `*.go` excluding `*_test.go` | `*_test.go` | `rg "func Test"` | `require` blocks in go.mod |
| Python | `*.py` excluding `test_*`, `*_test.py` | `test_*.py`, `*_test.py` | `rg "def test_"` | pyproject.toml / requirements.txt |
| TS/JS | `*.ts`, `*.js` excluding `*.test.*`, `*.spec.*`, `node_modules/` | `*.test.*`, `*.spec.*` | `rg 'it\(|test\('` | `dependencies` in package.json |
| Rust | `*.rs` excluding `tests/` | `tests/`, `#[cfg(test)]` modules | `rg '#\[test\]'` | `[dependencies]` in Cargo.toml |

These are approximations for order-of-magnitude assessment, not precision tooling.

## 1.3 File Size Distribution

Identify files exceeding thresholds. List top 20 largest files:

```bash
# Go
find . -name "*.go" ! -name "*_test.go" ! -path "*/vendor/*" -exec wc -l {} + | sort -rn | head -20

# Python
find . -name "*.py" ! -path "*/__pycache__/*" ! -path "*/venv/*" ! -path "*/.venv/*" -exec wc -l {} + | sort -rn | head -20

# TypeScript/JavaScript
find . \( -name "*.ts" -o -name "*.tsx" -o -name "*.js" -o -name "*.jsx" \) ! -path "*/node_modules/*" -print0 | xargs -0 wc -l | sort -rn | head -20

# Rust
find . -name "*.rs" ! -path "*/target/*" -exec wc -l {} + | sort -rn | head -20

# Or use cloc/scc/tokei if available — they handle exclusions automatically
```

- Flag files >500 LOC as potential god classes/scripts
- Flag files >300 LOC as candidates for investigation

## 1.4 Dependency Assessment

- Count direct vs transitive dependencies (where tooling supports it)
- Note dependency minimalism or bloat relative to ecosystem norms
- Check for known problematic patterns: vendored copies of actively-maintained deps, pinned ancient versions

## 1.5 CI/CD Assessment

- Check for CI config: `.github/workflows/`, `.gitlab-ci.yml`, `Jenkinsfile`, `Makefile`, etc.
- Check for pre-commit: `.pre-commit-config.yaml`
- Note what CI covers: lint, test, build, coverage, deploy
- Check for coverage enforcement (threshold gates, not just reporting)

## 1.6 Code Hygiene Scan

Scan for patterns that indicate quality discipline or gaps:

- **Magic literals**: Search for hardcoded string values used in control flow, event dispatch, identity
  comparison, or configuration. Language-specific patterns:

  | Language | Scan Approach |
  |----------|---------------|
  | Go | `rg -n '"[a-z_]*"' -g '*.go' -g '!*_test.go'` filtered to control flow contexts |
  | Python | `rg -n "['\"]\w+['\"]" -g '*.py'` in if/match/dispatch contexts |
  | TS/JS | `rg -n "['\"]\w+['\"]" -g '*.ts'` in switch/if/event contexts |

  Not every string literal is a magic value. Focus on:
  - Strings used in **dispatch** (switch/if chains, event names, status checks)
  - Strings used as **identity** (agent IDs, role names, service names)
  - Strings that appear in **multiple files** (cross-module coupling via literal)
  - Strings that **shadow typed constants** (the type exists but literals bypass it)

  **Provenance classification** — for each category of magic literal found, classify:

  | Category | Example | Fix |
  |----------|---------|-----|
  | **System constant** | Event name, error code | Extract to typed constant |
  | **Configuration value** | Default port, timeout | Extract to config with default |
  | **User-supplied identity** | Agent ID, workspace name | Resolve from runtime state — a constant doesn't fix this |

  The third category is the most severe: a hardcoded value that should be dynamic means the system
  silently assumes a specific runtime configuration. A `const` only consolidates the assumption;
  it doesn't fix it.

- **Suppression markers**: Count `nolint`, `noqa`, `@ts-ignore`, `# type: ignore`, `eslint-disable`
- **Panic/exit calls**: `panic()`, `os.Exit()`, `process.exit()`, `sys.exit()` in non-main code
- **Untyped escape hatches**: `interface{}`, `any`, `Any`, `object` in production code
- **TODO/FIXME/HACK**: Count and assess whether tracked or abandoned

## 1.7 Metrics Dashboard

Assemble findings into the dashboard format from `references/report-format.md`.

---

# Phase 2: Subsystem Analysis

*Qualitative assessment of each component.*

## 2.1 Subsystem Identification

Walk the project structure. Identify natural subsystem boundaries:
- Top-level directories with distinct purposes
- Package/module boundaries
- Layers (domain, application, infrastructure, presentation) if identifiable

In Pairing mode: confirm identified subsystems before proceeding.

## 2.2 Rating Framework

Each subsystem gets a 1–5 star rating:

| Stars | Meaning |
|-------|---------|
| ★★★★★ | Exemplary — clean, well-tested, minimal concerns |
| ★★★★☆ | Strong — solid engineering with minor concerns |
| ★★★☆☆ | Adequate — functional but meaningful improvement opportunities |
| ★★☆☆☆ | Concerning — significant issues affecting maintainability or reliability |
| ★☆☆☆☆ | Critical — serious problems requiring immediate attention |

**Rating dimensions** (weight equally unless context justifies otherwise):
- **Code organization**: Cohesion, responsibility separation, file sizes
- **Test coverage**: Quality and depth of testing for this subsystem
- **API clarity**: Are interfaces and contracts clear?
- **Error handling**: How are failures managed?
- **Complexity management**: Is complexity proportional to the problem? When complexity is high,
  is it structural (file/function size — fixable by splitting) or design-level (wrong abstraction,
  boolean-flag dispatch, imperative ceremony — requires pattern change)? Rate lower when complexity
  has a design root cause, even if LOC is acceptable.

## 2.3 Analysis Template

For each subsystem:

```markdown
### [Subsystem Name] (`path/`) ★★★★☆

**Strengths:**
- [Specific strength with evidence]

**Concerns:**
- [Specific concern with evidence — file names, LOC counts, concrete observations]
```

**Discipline:** Every strength and concern must cite evidence. "Well-tested" is not a strength — "2.5:1 test-to-production ratio with table-driven subtests" is. "Large file" is not a concern — "`handlers.go` at 918 LOC mixing 30+ handler functions" is.

## 2.4 Cross-Cutting Sections

After subsystem analysis, assess these quality dimensions that span subsystems. Include each section only when there is substantive content — an empty section is worse than an absent one.

**Testing & Quality Infrastructure:**
- Test framework and patterns (stdlib vs third-party, table-driven, fixtures, etc.)
- Coverage approach (reporting, enforcement, thresholds)
- Test quality enforcement (parallel tests, race detection, sleep guards, test helper isolation)
- Integration/E2E test presence and coverage

**Pre-Commit & CI Pipeline:**
- Hook coverage by category (lint, format, type-check, security, file hygiene)
- CI stages and platforms
- Enforcement gaps (what runs but doesn't block, what doesn't run at all)

**Documentation & Specifications:**
- Doc volume and coverage
- Spec depth (if specs/ exists)
- Staleness risk (specs referencing outdated implementation details)
- Onboarding path clarity

---

# Phase 3: Synthesis

*Aggregate findings into overall assessment.*

## 3.1 Executive Summary

One paragraph: what the project is and its overall engineering quality.

**Key Strengths** (3–5 bullet points): The most impactful positive patterns. Synthesize — don't enumerate. "Clean architecture" is not a strength; "Strict layer separation with dependencies pointing inward — no infrastructure types leak into domain" is.

**Areas for Improvement** (3–5 bullet points): The most impactful concerns. Same evidence discipline.

## 3.2 Overall Grade

| Grade | Meaning |
|-------|---------|
| A+ | Exceptional — exemplary across all dimensions; teaching reference quality |
| A | Excellent — strong across all dimensions; concerns are minor |
| A- | Excellent with concerns — strong foundation, meaningful structural or coverage gaps |
| B+ | Good — solid engineering; several areas need attention |
| B | Good with gaps — functional and maintainable; notable testing or structural gaps |
| B- | Adequate — works but shows systematic underinvestment in quality |
| C+ | Below expectations — multiple significant concerns; improvement needed before scaling |
| C | Concerning — serious quality issues affecting reliability or maintainability |
| C- | Poor — widespread problems; refactoring required before feature work |
| D | Critical — fundamental issues; quality debt threatens project viability |
| F | Failing — unmaintainable; rebuild considerations warranted |

**Grading discipline:**
- Grade must be justified with specific reference to subsystem ratings and metrics
- State the deduction from the next-higher grade explicitly (e.g., "The deduction from A to A- is for file-level concentration in the MCP handlers and state model")
- Calibrate to ecosystem norms (see Reference: Language Ecosystem Norms)

---

# Phase 4: Recommendations

*Prioritized refactoring roadmap.*

## 4.1 Priority Framework

| Priority | Criteria | Typical Actions |
|----------|----------|-----------------|
| **P1: High Impact / Low Risk** | Structural improvements that don't change behavior. Clear, safe, high ROI. | File splits, module extraction, grouping, adding missing CI gates, extracting typed constants from magic literals |
| **P2: Medium Impact / Medium Risk** | Quality improvements requiring broader changes. | Coverage enforcement, test additions, API cleanup, dependency updates, design pattern introduction (strategy, declarative registration), resolving hardcoded identities |
| **P3: Strategic / Long-term** | Investments that compound over time. May require architecture changes. | Fuzz testing, spec-code automation, tooling, major decompositions |

## 4.2 Recommendation Template

For each recommendation:

```markdown
#### N.M [Title]
- **What**: [Specific files/components to change and how]
- **Risk**: [Low / Medium / High] — [rationale]
- **Impact**: [What improves and why it matters]
- **Depends on**: [Other recommendations, if any]
```

Every recommendation must trace to a finding in Phase 2 or Phase 3. No generic "best practices" without project-specific justification.

## 4.3 Recommendation Anti-Patterns

- Do not recommend what the project already does well
- Do not recommend architectural rewrites when structural cleanup suffices
- Do not recommend without stating the concrete problem it addresses
- Do not recommend file splits or method extraction as the remedy for every complexity concern.
  Ask "why is this complex?" first — if the root cause is a design issue (boolean-flag dispatch,
  imperative ceremony, missing polymorphism), recommend the design-level fix. Extraction addresses
  structural complexity; it does not fix design complexity. If you find yourself recommending only
  structural remedies, you are likely missing design-level concerns.
- Sequence matters: P1 should be achievable independently; P2 may depend on P1

---

# Persistence of Findings

**ISSUES_FILE** = `specs/architecture/architectural-issues.md`

Significant findings (subsystem concerns rated ★★☆☆☆ or below, P1 recommendations, cross-cutting concerns) should be persisted to ISSUES_FILE for long-term tracking.

**Persistence format:**

```markdown
### [Issue Title]

**Skill:** code-quality-assessment
**Category:** [Subsystem concern / Cross-cutting / RECOMMENDATION]

**Issue:** [Description]

**Implication:** [Why it matters]

**Direction:** [Suggested approach, if any]
```

**What to persist:**
- Subsystem concerns with ★★☆☆☆ or below
- All P1 recommendations
- Cross-cutting concerns with systemic impact

**What NOT to persist:**
- Low-priority style issues
- Findings already in ISSUES_FILE (check before adding)
- Transient issues resolved in the same session

## Scope Constraints

This skill assesses the whole repository, not individual diffs. Scope constraints apply to **persistence** (what gets written to ISSUES_FILE), not to **analysis** (what gets examined).

**§BRAND_NAME_TITLE§ mode (multi-agent):**
- Full repo assessment is the default — the skill exists for whole-codebase evaluation
- When invoked during a review task (not a standalone assessment), scope findings to what's relevant to the review context
- Persist only findings not already in ISSUES_FILE

**Pairing mode:**
- Do not re-raise issues already documented in ISSUES_FILE unless materially changed
- If changes worsen a documented issue, update the existing entry rather than duplicating

## Mode-Specific Confirmation

**Pairing mode:** Before saving findings to ISSUES_FILE:
```
Found [N] quality issues worth persisting:
1. [Issue title] — [one-line summary]
2. ...

Save to specs/architecture/architectural-issues.md? (y/n/select specific)
```
Wait for user confirmation.

**§BRAND_NAME_TITLE§ mode:** Save automatically after assessment completion.

---

# Integration

| Skill | Relationship |
|-------|-------------|
| **software-architecture-review** | Complementary. Code quality assesses health metrics and grading; architecture review assesses structural patterns, smells, and dependency direction. For deeper structural analysis, invoke architecture review. |
| **testing** | Downstream. Testing skill provides detailed test methodology; code quality assessment provides the bird's-eye view of testing adequacy. |
| **clean-code** | Downstream. Code quality assessment identifies refactoring targets; clean-code executes the transformations. |
| **code-review** | Orthogonal. Code review evaluates diffs; code quality evaluates the whole codebase. Assessment findings provide context for reviewers. |

---

# Reference: Metrics Calibration

Healthy ranges for calibrating assessments. These are norms, not targets.

| Metric | Healthy Range | Warning Signs |
|--------|--------------|---------------|
| Test-to-production ratio | 0.5:1 – 3:1 | <0.3:1 (undertested), >5:1 (possibly testing implementation details) |
| Max file LOC | <500 | >500 (god class candidate), >800 (almost certainly needs splitting) |
| Direct dependencies | Varies by ecosystem | >50 for a focused tool; >200 for any project |
| CI coverage enforcement | Present | Absent when test ratio is healthy (culture without enforcement) |
| TODOs in production code | 0 ideal | >10 untracked (deferred maintenance) |
| Pre-commit hooks | Present | None configured in a team project |
| Magic literals in dispatch | 0 (all typed constants) | >5 untyped strings in control flow (typo risk, no IDE support) |

---

# Reference: Language Ecosystem Norms

Calibration varies by ecosystem. Anchoring to wrong norms produces misleading grades.

| Language | Typical Test Approach | Dependency Norms | File Size Norms |
|----------|----------------------|------------------|-----------------|
| Go | Table-driven `t.Run()` subtests; stdlib testing | Minimal (stdlib-first) | Packages <500 LOC typical |
| Python | pytest; fixtures-heavy | pip ecosystem; moderate deps normal | Modules <300 LOC typical |
| TypeScript/JS | Jest/Vitest; mock-heavy | npm ecosystem; high dep count normal | Components <200 LOC typical |
| Rust | `#[test]` modules; property testing via proptest | Cargo ecosystem; moderate deps | Modules <500 LOC typical |
| Java/Kotlin | JUnit; Spring test | Maven/Gradle; high deps normal | Classes <300 LOC typical |
| C#/.NET | xUnit/NUnit; mock-heavy | NuGet; moderate deps | Classes <300 LOC typical |
| Ruby | RSpec/Minitest; fixtures | Bundler; moderate deps | Classes <200 LOC typical |
| Elixir | ExUnit; doctests | Hex; minimal deps typical | Modules <300 LOC typical |

A Go project with 50 dependencies is notable; a TypeScript project with 50 is unremarkable. Grade relative to ecosystem.
