---
name: software-architecture-review
description: Software Architecture Review Protocol
---

**REVIEW_FILE** = `docs/architecture-review.md`

**Recommended Tools:**
- **Warp Grep** (`warpgrep_codebase_search`) — Complement to regex for discovery-heavy phases. Use when:
  - Regex would be brittle (semantic concepts like "error handling", "data transformation")
  - You'd need multiple grep patterns to cover variations
  - Searching for behavioral patterns, not just text patterns

  Example queries by phase:
  - **Phase 1 Discovery**: "Find all HTTP client implementations", "Find where data is normalized between formats"
  - **Enrichment/Adversarial**: "Find try/except blocks that return None or pass silently", "Find file operations without existence checks"
  - **Comparisons**: "Find all base classes and compare their patterns", "Find all TypedDict or dataclass definitions"

  Start with Warp Grep for exploration, then use targeted regex/glob to verify specific patterns.

Architecture is about trade-offs, not truths. Raise questions, suggest directions, avoid astronautics.

*Invoked for: implementation planning, code review (P3 supplement), or explicit architectural evaluation.*

---

# Process

Discovery → Analysis → Recommendations

**Templates anchor cognition.** Complete Phase 1 before Phase 2. The skill is a lens to apply to what you found, not slots to fill.

---

# Modes

| Mode | When | Scope |
|------|------|-------|
| **Full Review** | Explicit architectural evaluation, major implementation planning | All phases, all sections |
| **Code Review Supplement** | Adding P3 architectural notes to a code review | Targeted analysis of changes only |
| **Enrichment** | Second+ pass to improve coverage of existing review | Independent analysis → merge → verify → update |
| **Enrichment (no lens)** | Avoid tunnel vision from lens focus | Same as Enrichment, but unconstrained exploration |
| **Adversarial** | Break out of convergent attention patterns | Randomized exploration → gap hunting → forced comparisons |
| **Adversarial (targeted)** | Suspect gaps in specific area | Specific file/component as entry, rest of Adversarial process |
| **Adversarial (entry point N)** | Force specific exploration pattern | Fixed entry point from table, rest of Adversarial process |

**Phase applicability:**

| Mode | Phase 1 (Discovery) | Phase 2 (Analysis) | Phase 3 (Recs) | Output |
|------|---------------------|--------------------| ---------------|--------|
| **Full Review** | ✓ Complete | ✓ Complete | ✓ Complete | New REVIEW_FILE |
| **Code Review Supplement** | — Skip | Targeted (relevant questions only) | — Skip | Inline notes in code review |
| **Enrichment** | ✓ Fresh (independent) | Update (add to existing) | Update (add to existing) | Revised review file |
| **Adversarial** | Own process (randomized) | Update (add to existing) | Update (add to existing) | Revised review file |

- **Complete**: Run the full phase from scratch
- **Update**: Add new findings to existing sections; verify existing findings still hold
- **Targeted**: Run only the parts relevant to the changed code

**Mode selection** (check in order — first match wins):
1. "no lens" in request → Enrichment (no lens) — **still follows full Enrichment protocol, just without lens rotation**
2. User explicitly requests Adversarial → Adversarial
3. User explicitly requests Full Review → Full Review (e.g., after major structural changes)
4. REVIEW_FILE exists → Enrichment
5. No existing review → Full Review

## Full Review

Use complete process: Phase 1 → Phase 2 → Phase 3 → Summary.

Appropriate for: new system reviews, periodic health checks, major refactoring decisions.

**Default output:** REVIEW_FILE (if not specified and doesn't exist yet).

**Time Budget:** Discovery (Phase 1) should be at least as thorough as Analysis + Recommendations combined. Most missed findings come from rushed discovery — especially the Coverage Checkpoint. If you're tempted to skip ahead, you're probably under-investing in discovery.

## Code Review Supplement

Skip full discovery — the code review already established context. Focus on:

1. **Context**: Read REVIEW_FILE if it exists — understand documented patterns and smells before evaluating changes
2. **Scope**: Only the changed files and their immediate dependencies
3. **Questions**: Which Analysis Framework questions are relevant to this change?
4. **Output**: Use the Integration with Code Review format (see end of skill)

```
Architectural notes:
- [smell/pattern observation relevant to this change]
- [dependency direction concern, if any]
- [trade-off worth discussing]
```

Tag as `[concern]` or `[suggestion]` per code review protocol.

## Enrichment

*For iterative refinement of an existing architecture review.*

**⚠️ This section covers both regular Enrichment AND "no lens" mode. No-lens skips lens rotation but follows the exact same protocol below.**

**Default file:** REVIEW_FILE

**First check:** Verify REVIEW_FILE exists. If it doesn't exist, this is Full Review, not Enrichment — go to that section instead.

**Header check (BEFORE discovery):** Read ONLY the first 10 lines of REVIEW_FILE to extract:
- Pass number from header (e.g., "Mode: Enrichment (pass 17)")
- Previous lens from header (e.g., "Boundaries lens")
Your pass is N+1. Your lens continues the rotation from previous lens (see Lens Rotation below).
**⚠️ STOP after reading the header. Do NOT scroll down or read findings.**

**When to use:** Second or subsequent pass over the same codebase, typically after changes or to improve coverage. Each review naturally finds different things — enrichment accumulates findings rather than replacing them.

**⚠️ CRITICAL: You MUST NOT read REVIEW_FILE findings until Step 2.** Reading findings early causes anchoring — you'll confirm existing findings instead of discovering new ones. The header check above is allowed (pass number + lens only). Complete Phase 1 Discovery fully before reading findings.

**Process:**

1. **Independent Analysis (Phase 1 Discovery)** — Complete the entire Phase 1 below as if no review exists. Explore the codebase fresh. Write your findings to a scratch area or hold in memory. **Do not read the existing review file.**

2. **Merge Phase** — *Only now* read REVIEW_FILE. Compare your fresh findings against it.

3. **Verification** — For *each* finding in the existing review, explicitly verify:
   - Still accurate? (code may have changed)
   - Still relevant? (context may have shifted)
   - Severity still appropriate?
   - Mark each as: ✓ verified, ✗ stale, or ~ adjusted

4. **Gap Analysis** — Explicitly list:
   - New findings from fresh pass not in existing review
   - Existing findings your fresh pass missed (and why — attention shift or genuine gap?)

5. **Update** — Revise REVIEW_FILE with:
   - New findings added, tagged `*(pass N)*` or `*(pass N, [lens] lens)*`
   - Stale findings removed or marked resolved
   - Severity adjustments where warranted
   - Updated date in header
   - Header: `Mode: Enrichment (pass N)` — or `Mode: Enrichment (pass N, no lens)` if no-lens mode

**Output:** Updated review document, not a separate file.

**Time Budget:** Independent Analysis (step 1) should be at least as thorough as the merge + verification steps combined. Don't rush it to get to the merge. The value of enrichment comes from fresh eyes — if you read the existing review first, you're just proofreading.

### Lens Rotation

Each enrichment pass uses a different primary lens to shift attention. **Continue from the previous pass's lens** — read the header to determine where you are in the rotation.

**Lenses:**
1. **Complexity** — LOC, function length, cyclomatic complexity, god scripts/classes
2. **Duplication** — Cross-file similarity, repeated patterns, copy-paste code
3. **Boundaries** — Import direction, layer violations, dependency flow
4. **Coupling** — Configuration hardcoding, tight dependencies, hidden state sharing
5. **Coverage** — Test gaps, untested critical paths, missing edge cases *(most attention-intensive — placed last so it's primary on Pass 3 when context is fresh but architecture is understood)*

**Rotation order (shift of 2):**
Complexity → Boundaries → Coverage → Duplication → Coupling → (wrap to Complexity)

*Why shift-of-2?* Covers the 3 highest-value lenses (Complexity, Boundaries, Coverage) as primary in just 3 passes — then Duplication and Coupling get primary focus in passes 4-5 if needed.

**How to determine your lens:**
1. Read the header: "Mode: Enrichment (pass N, [lens] lens)" or "Mode: Enrichment (pass N)"
2. Find previous lens in the rotation order above
3. Your lens is the NEXT one in the sequence (wrap from Coupling → Complexity)
4. If no lens specified in header (e.g., "no lens" or malformed), default to Complexity

Example: Previous was "Boundaries lens" → Your lens is Coverage

**How to apply:** During Phase 1 Discovery, start with your primary lens. Spend ~40% of discovery time on it before moving to the next. The leading lens gets deepest attention while context is fresh; later lenses get lighter coverage.

**Complexity lens — systematic god script scan:**
When Complexity is your primary lens, START with a systematic LOC scan before any exploration:
```bash
find . -name "*.py" -type f ! -path "*/__pycache__/*" -exec wc -l {} + | sort -rn | head -20
```
Flag ALL files >500 LOC as potential god scripts. For each:
1. Grep REVIEW_FILE for the filename + "god" (e.g., `grep -i "detect_platforms.*god\|god.*detect_platforms"`) — do NOT read full review
2. If not already flagged, investigate — god scripts tend to cluster other smells (duplication, coupling, silent failures)
3. Note findings for merge phase

This prevents god scripts from escaping detection due to entry point randomization.

### No-lens Mode

User can request enrichment without lens rotation (e.g., "review architecture, no lens"). Explore freely — let findings emerge from what catches attention rather than a predetermined focus. Use when:
- Lens passes feel too constrained (tunnel vision)
- You want to simulate fresh-eyes discovery
- Alternating with lens passes for breadth vs depth

**⚠️ No-lens is Enrichment, not a shortcut. Full protocol required:**
1. Check for existing REVIEW_FILE — if missing, this is Full Review, not no-lens enrichment
2. Check header for pass number (your pass is N+1)
3. Complete Phase 1 Discovery independently — do NOT read existing review yet
4. Merge Phase — read existing review, compare findings
5. Verify each existing finding (✓ verified, ✗ stale, ~ adjusted)
6. Gap Analysis — list new findings and what you missed
7. Update the file with **no-lens display format:**
   - Header: `Mode: Enrichment (pass N, no lens)`
   - Finding tags: `*(pass N, no lens)*`

**No-lens only skips lens selection. You MUST still: do independent discovery, merge with existing review, verify findings, and update the file.**

**Recommended iterations:** Run enrichment **3 times** for solid coverage. The rotation order (Complexity → Boundaries → Coverage) covers the highest-value lenses in the first 3 passes. Additional passes (Duplication, Coupling) provide diminishing returns unless actively extending the system.

**⚠️ MANDATORY after 3+ passes:** If pass number ≥ 3, you MUST present options before proceeding:
```
Pass [N] exists ([previous lens] lens). Per skill, 3 passes provide solid coverage.

Options:
1. Pass [N+1] Enrichment ([next lens in rotation] lens) — full independent discovery + merge
2. Adversarial mode — randomized exploration to break convergent patterns
3. Health check — quick verification existing findings still hold

Which approach? (1/2/3)
```
Do NOT proceed with enrichment without explicit user choice. The prompt exists because attention convergence makes additional standard passes less effective than Adversarial mode.

## Adversarial

*For breaking out of convergent attention patterns when enrichment plateaus.*

**Requires:** Existing REVIEW_FILE

**When to use:** After 3+ enrichment passes when findings have plateaued but you suspect gaps remain. Adversarial mode disrupts the exploration patterns that cause repeated passes to converge on the same findings.

**⚠️ CRITICAL: Do NOT read the existing review until Step 4.** The goal is to find what the review missed — reading it first defeats the purpose.

**Mindset:** "This review is incomplete. A critical reviewer would find issues that aren't documented. What did we miss?"

**Process:**

1. **Randomized Entry Point** — Do NOT start with directory tree. Pick using `1 + (current_minute mod 6)` or choose the entry point you're *least* naturally drawn to — the goal is to break your default exploration pattern:
   | # | Entry Point |
   |---|-------------|
   | 1 | Start from tests — what are they testing? What's NOT tested? |
   | 2 | Start from config — what's configurable? What's hardcoded that shouldn't be? |
   | 3 | Start from a random mid-sized file (100-300 LOC) — trace its dependencies |
   | 4 | Start from specs/ or docs/ — what's specified but not implemented? What's implemented but not specified? |
   | 5 | Start from error handling — grep for `except`, `raise`, `error` — how are failures handled? |
   | 6 | Start from data flow — pick an input, trace it to output |
   | 7 | Start from documented smells in existing review — investigate each for clustering issues |

   **ADRs are historical records:** Architecture Decision Records (`specs/architecture/ADR/`) capture decisions at a point in time, not current state. Path references, architectural patterns, and implementation details in ADRs may have evolved since writing. When using entry point #4 with ADRs:
   - Use ADRs to understand *intent* and *rationale*, not to verify current paths
   - Focus on *current* architectural issues, not ADR-vs-reality drift
   - Drift from ADRs is expected and not a finding — the codebase evolves
   - ADRs remain valuable for understanding *why* decisions were made

   **God script override:** If the review lists god scripts (>500 LOC files), consider starting from one NOT yet used as an adversarial entry point. God scripts cluster other smells — good ROI for finding new issues.

2. **Contrarian Questions** — For each component you examine, ask:
   - "What could go wrong here that isn't handled?"
   - "What assumption is this code making that might be wrong?"
   - "If this file changed, what else would break?"
   - "What's the same name used for different things? Different names for the same thing?"

3. **Forced Comparisons** — Pick 2-3 pairs and explicitly compare:
   - All `base.py` files — are they consistent?
   - All TypedDict/dataclass definitions — field naming conventions?
   - All files with similar names across directories
   - Config values vs actual usage — any drift?

4. **Gap Hunting** — *Now* read REVIEW_FILE. Three techniques:

   **Smell search:** For each smell category in the Reference: Smell Catalog that has NO finding in the review, actively search for it:
   - No "Hardcoded configuration" finding? Search for hardcoded values.
   - No "Non-idempotent operations" finding? Search for state mutations.
   - No "Primitive obsession" finding? Look for stringly-typed data.

   **Inverse grep:** Pick 3 keywords that appear frequently in existing findings (e.g., `config`, `async`, `test`). Search for files that DON'T contain them — what's happening in the unlabeled parts? This catches architectural islands — code that exists outside the main patterns.

   **Warp Grep queries:** Use natural language to find what regex misses:
   - "Find try/except blocks that return None or pass without logging"
   - "Find file open operations without existence checks"
   - "Find where data fields are renamed or transformed between different conventions"

5. **Second Look** — Pick 3 files that appear in existing findings and re-read them with fresh eyes. What else is there?

6. **Update** — Add new findings to review with tag: `*(Adversarial pass)*`

**Note:** Adversarial mode adds findings but does NOT verify/remove stale ones. This is intentional — Adversarial focuses on breaking attention patterns to find gaps. Use Enrichment mode periodically for comprehensive maintenance (verify + cleanup).

**Output format in header:**
```
**Mode:** Adversarial (after pass N)
```

**Time Budget:** Spend 60% on steps 1-3 (before reading existing review). The value comes from the different exploration path.

**Success metric:** Finding at least one issue not in the existing review. If you find nothing new, document what you searched and why — that's still valuable signal that the review is comprehensive.

### Adversarial Variants

**Adversarial (targeted):** Skip randomized entry point selection. Start from a specific file or component you suspect has gaps. Use when:
- A particular area feels under-examined
- Recent changes touched code not well-covered in the review
- You want to deep-dive a god script or known smell cluster

Specify in prompt: "Adversarial mode, starting from [file/component]"

**Adversarial (entry point N):** Force a specific entry point from the table above. Use when:
- You want repeatability over randomization
- A particular exploration pattern has proven effective
- You're systematically covering all entry points across multiple passes

Specify in prompt: "Adversarial mode, entry point 5" (or whichever number)

**Smell-driven entry (entry point 7):** A particularly effective variant — use the documented smells in the existing review as your starting points. Problems cluster: a god script often has coupling issues, silent failures, hardcoded config. Each documented smell becomes a lead to investigate for adjacent issues.

This inverts standard Adversarial's logic:
- Standard: randomize entry to *escape* attention convergence
- Smell-driven: use known problems as magnets because *problems cluster*

Both are valid and address different failure modes.

---

# Phase 1: Discovery

*Full Review and Enrichment modes. Code Review Supplement skips to targeted analysis. Adversarial mode uses its own process (see above).*

*Mandatory before analysis. Ensures nothing overlooked.*

## 1.1 Overview

State the system's purpose and data flow. Use two-row format showing stages and artifacts:

```
Stage1    →    Stage2    →    Stage3    →    Stage4
   ↓             ↓             ↓             ↓
artifact1    artifact2    artifact3    artifact4
```

## 1.2 Component Walkthrough

For each component:

```
### [Component Name] (`path/`)

**Purpose:** What it does

**Pattern:** How it's structured (if applicable)

**Observations:**
- Notable design decisions
- Interfaces with other components
- Potential concerns (feed Phase 2)
```

**Quantitative signals to note:**
- Large files (>300 LOC) — potential god class/script
- Similar code across components — potential duplication
- Imports from unexpected layers — potential boundary violation
- Hardcoded values that look like configuration — potential coupling
- External dependencies (third-party libraries, API calls) — influence architectural trade-offs, upgrade/security risks

## 1.3 Dependency Map

ASCII tree showing what depends on what:

```
shared/ (stable)
   ├── models.py      ← Used by all components
   ├── utils.py       ← Common utilities
   └── config.py      ← Centralized settings

application/ (volatile)
   ├── component_a/   ← Produces X
   └── component_b/   ← Consumes X, produces Y
```

Annotate stability (stable/volatile) and data flow direction.

## 1.4 Coverage Checkpoint

**Time Budget:** This checkpoint should take ~20% of Phase 1 time. Rushed checkpoints are the #1 source of missed findings.

Before moving to analysis, deliberately answer each question:

- **What exists that shouldn't?** (orphaned code, unused abstractions, specs not connected to implementation)
- **What's implicit that should be explicit?** (pipeline state, configuration, observability gaps)
- **What's missing from the walkthrough?** (tests, config, docs, specs — did you skip anything?)
- **What requires cross-file comparison?** (duplicated utilities, inconsistent patterns, boundary violations)

**Enforcement:** If you identify gaps, STOP. Go back and add them to the walkthrough. Don't carry forward "I should have looked at X" — look at it now.

Check README.md or equivalent for pointers to specs, design docs, or architectural decisions that might be orphaned.

**Deep inspection triggers:** If you noted any of these in the walkthrough, read the actual code:
- Files >300 LOC
- "Similar to X" observations
- Specs or design docs directories

---

# Phase 2: Analysis

*Apply these lenses to what Phase 1 discovered.*

## 2.1 Analysis Framework

Answer before making recommendations:

| # | Question | Assessment |
|---|----------|------------|
| 1 | **What problem is being solved?** | |
| 2 | **What are the change vectors?** (likely to change vs stable) | |
| 3 | **What are the constraints?** (team, timeline, existing patterns) | |
| 4 | **What's the cost of being wrong?** (reversibility) | |
| 5 | **Where do errors get handled? What happens when things fail?** (contained or propagated?) | |
| 6 | **What's the expected lifespan?** | |
| 7 | **What's the concurrency model?** | |
| 8 | **Who owns the data and its invariants?** | |
| 9 | **Where are the boundaries?** | |
| 10 | **What are the runtime constraints?** | |

## 2.2 Strengths

What's working well. Synthesize observations into architectural judgments — not "X exists" but "X is appropriate because...":

```
### [Architectural Judgment]

[What works, why it matters, what it enables]
```

Examples of synthesis:
- "Registry pattern exists" → "Extensibility: adding platforms requires no orchestration changes"
- "config.py exists" → "Configuration externalized: no magic numbers in business logic"
- "Base classes exist" → "Appropriate abstraction depth: extensible without over-engineering"

## 2.3 Smells

Problems detected. Use smell vocabulary from reference table:

```
Smell: [name] in [location from Phase 1]
Signal: [what triggered detection]
Impact: [why it matters]
Direction: [refactoring suggestion, not prescription]
```

## 2.4 Patterns

Patterns identified. Table format:

| Pattern | Where Used | Purpose |
|---------|------------|---------|
| [Name] | [Component/file] | [What problem it solves here] |

## 2.5 Test Coverage

Assess test structure and gaps. For detailed test analysis, use the **testing skill** — this section is architectural overview only.

**Questions:**
- What's well-tested vs undertested? (run coverage if available)
- Unit vs integration balance? (too many mocks = fragile; too few = slow)
- Are critical paths covered? (auth, payments, data mutations)
- Are error paths tested? (not just happy path)
- Test-to-code ratio? (0.5-1.5× is typical healthy range)

**Red flags:**
- 0% coverage on files with business logic
- Tests that only test mocks (no real behavior)
- No integration tests for multi-component flows
- Flaky tests (non-deterministic failures)

Note gaps as input to recommendations, not as prescriptive requirements. Reference testing skill for remediation guidance.

---

# Phase 3: Recommendations

Order by decreasing priority (High first, None last):

| Priority | Issue | Rationale | Action |
|----------|-------|-----------|--------|
| **High** | | | |
| **Medium** | | | |
| **Low** | | | |
| **None** | | | |

"None" is valid — explicitly stating what's not worth doing.

---

# Summary

One paragraph overall assessment:

- Architecture quality relative to scope/constraints
- Key strengths to preserve
- Primary risks or gaps
- Whether current state is appropriate or needs intervention

---

# Appendix: File Reference

| Component | Location |
|-----------|----------|
| [Name] | `path/to/directory/` |

Keep at directory level. Only list individual files for entry points or key abstractions.

---

# Reference: Principles as Heuristics

These are lenses, not laws. Apply when they clarify; ignore when they obscure.

| Principle | Question It Raises |
|-----------|-------------------|
| **SRP** | Does this unit have one reason to change? |
| **OCP** | Can we extend without modifying? Should we? |
| **LSP** | Can subtypes substitute without surprise? |
| **ISP** | Are clients forced to depend on methods they don't use? |
| **DIP** | Do high-level policies depend on low-level details? |
| **YAGNI** | Are we building for a future that may not arrive? |
| **KISS** | Is there a simpler way that still works? |
| **DRY** | Is this duplication, or coincidental similarity? |

**Tension acknowledgment**: YAGNI and OCP often conflict. DRY and clarity sometimes trade off. Name the tension, don't pretend it resolves.

**Reversibility preference:**
1. Reversible decisions
2. Localized irreversible decisions
3. Global irreversible decisions (last resort)

---

# Reference: Dependency Direction

```
Domain (stable) ← Application ← Infrastructure (volatile)
```

- Domain knows nothing of persistence, frameworks, UI
- Dependencies point inward toward stability
- Violation signal: importing framework types in domain logic

Not dogma — pragmatic for codebases expecting change. For scripts or throwaway code, skip it.

---

# Reference: Pattern Vocabulary

Patterns are solutions to recurring problems. Name them to communicate, not to impress:
- **Structural**: Adapter, Facade, Decorator, Composite
- **Behavioral**: Strategy, Observer, Command, State
- **Creational**: Factory, Builder (when construction is complex)

**Anti-pattern**: Suggesting patterns without naming the problem they solve here.

---

# Reference: Smell Catalog

| Smell | Signal | Direction |
|-------|--------|-----------|
| **Shotgun surgery** | One change touches many files | Missing abstraction |
| **Divergent change** | One file changes for unrelated reasons | Split responsibilities |
| **Feature envy** | Method uses another class's data extensively | Move method |
| **Inappropriate intimacy** | Classes know too much about each other | Introduce interface |
| **God class/module** | One unit does everything; >300 LOC or many unrelated methods | Extract cohesive pieces |
| **Speculative generality** | Abstractions for hypothetical futures | Remove until needed |
| **Primitive obsession** | Domain concepts as raw types | Introduce value objects |
| **Leaky abstraction** | Implementation details escape boundaries | Tighten interface |
| **Untestable by design** | Can't test without database/network/time | Inject dependencies, extract pure logic |
| **Non-idempotent operations** | Retries cause duplicates or corruption | Make operations safely repeatable |
| **Unstable interface** | Small changes ripple to many callers | Narrow or stabilize contract |
| **Unobservable behavior** | Can't tell what's happening in production | Add logging, metrics, tracing hooks |
| **Hardcoded configuration** | Tunable values hardcoded in source | Extract to config file or environment |
| **N+1 queries** | Loop making individual calls instead of batch | Batch operations, eager loading |
| **Unbounded operations** | No limits on collection size, query results, retries | Add pagination, limits, circuit breakers |
| **Secrets in code** | API keys, passwords, tokens in source | Move to environment variables, secret manager |
| **Missing access control** | No authorization checks on sensitive operations | Add permission gates at boundaries |

---

# Reference: Anti-Patterns (for the architect)

- **Astronautics**: Abstraction layers for their own sake. "What if we need to swap databases?" — Are we going to?
- **Pattern hunting**: Seeing patterns before seeing problems
- **Premature generalization**: Extracting abstractions from one example
- **Complexity budget blindness**: Every abstraction has a cost; some codebases can't afford it
- **Ignoring the team**: Elegant architecture nobody can maintain is not elegant
- **Naive Cathedral thinking**: Detailed target design before the problem is understood (real cathedrals were built incrementally — think of the skateboard-to-car agile metaphor)
- **Conway blindness**: Module boundaries that don't match team ownership
- **Orphaned abstractions**: Code with no clear owner — it will decay
- **Hero architecture**: Solutions only one person understands
- **Big bang rewrites**: Replacing systems in one shot — prefer incremental strangling
- **Exception creep**: "Just this once" becomes the norm — erosion happens one compromise at a time

**Calibration question**: If I removed this abstraction and inlined the code, what would break? If the answer is "nothing concrete" — reconsider.

---

# Integration with Code Review

When invoked during code review, add architectural perspective after P0-P2 (security, correctness, data):

```
Architectural notes:
- [smell/pattern observation]
- [dependency direction concern, if any]
- [trade-off worth discussing]
```

Tag as `[concern]` or `[suggestion]` per code review protocol. Architectural disagreements are rarely `[blocker]` unless they create correctness/security issues.

---

# Persistence of Findings

**ISSUES_FILE** = `docs/architectural-issues.md`

Significant findings (smells, structural concerns, high-priority recommendations) should be persisted to ISSUES_FILE for long-term tracking.

**What to persist:**
- Smells with Medium or High impact
- Structural concerns that affect system evolution
- High-priority recommendations from Phase 3
- Patterns that indicate systemic risk

**What NOT to persist:**
- Low-priority style issues
- Findings already in ISSUES_FILE (check before adding)
- Transient issues resolved in the same session

## Persistence Format

Each finding must include skill attribution:

```markdown
### [Issue Title]

**Skill:** software-architecture-review
**Category:** [Smell name or RECOMMENDATION]

**Issue:** [Description]

**Implication:** [Why it matters]

**Direction:** [Suggested approach, if any]
```

## Scope Constraints

The skill uses the full repo for context, but what to *raise* depends on mode:

**Liza mode (multi-agent):**
- Only raise issues **introduced by the considered commit**
- Pre-existing issues unrelated to the changes are out of scope
- Use repo context to evaluate *impact* of changes, not to audit the whole codebase
- Example: If considered commit adds a new god class, raise it. If a god class already exists elsewhere, ignore it unless the changes interact with it.

**Pairing mode:**
- Do not re-raise issues already documented in ISSUES_FILE unless they have materially changed
- Before raising an issue, check ISSUES_FILE — if already documented with same severity/scope, skip it
- If changes worsen a documented issue or shift its nature, update the existing entry rather than adding a duplicate

## Mode-Specific Behavior

**Pairing mode:** Before saving findings to ISSUES_FILE, present the list and ask:
```
Found [N] architectural issues worth persisting:
1. [Issue title] — [one-line summary]
2. ...

Save to docs/architectural-issues.md? (y/n/select specific)
```

Wait for user confirmation before writing.

**Liza mode (multi-agent):** Save findings automatically after review completion. No confirmation required — the skill is invoked by agents operating autonomously.

## Integration with Review Workflow

1. Complete analysis phases as normal
2. After Phase 3 (or Summary), identify persistable findings
3. Check ISSUES_FILE for duplicates
4. Apply mode-specific confirmation
5. Append new findings to appropriate section in ISSUES_FILE
