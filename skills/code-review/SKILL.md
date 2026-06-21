---
name: code-review
description: Code Review Protocol
---

Code review is risk mitigation, not gatekeeping.
The goal is catching issues the author couldn't see — and occasionally sharing a better pattern when it genuinely helps.

# Review Context

Before reviewing, establish context:
- **Scope:** Default to staged files (`git diff --cached --name-only`, then `git diff --cached --stat`, then targeted path diffs). For PRs or commits, inspect changed files and stats before reading targeted hunks. Only broaden if explicitly asked.
- **Local working tree:** For local reviews, triage unstaged and untracked files before review. If related but not staged, surface this explicitly and include only if the review target is "pending changes"; if unrelated, ignore; if unclear, ask before including.
- **Intent:** Check ticket/description (PRs) or ask the author (pending). If unclear, clarify before reviewing.
- **Timing:** Is now the right time for this functionality? Half-baked or premature additions warrant a `[question]`.
- **Approach:** For complex changes, was the approach discussed before implementation? Catch architectural misalignment early — complete rewrites are painful.
- **Diff-first:** Read bounded diff context before source files. Prefer name-only/stat first, then targeted path or hunk diffs. Only read source when a finding needs surrounding context. Never pre-read the entire codebase.
- **Size:** If >800 lines or >20 files, consider suggesting a split (PR) or incremental commits (pending). Large diffs hide bugs.
- **Large diffs:** If truncated, >800 lines, >20 files, >80K chars total, or >20K chars in one file, avoid unbounded full-diff reads; classify files and inspect targeted paths/hunks. Verify critical findings against source before tagging `[blocker]` or `[concern]`.
- **Reviewer limits:** If reviewing outside your expertise, say so. Make assumptions explicit.

# Review Modes

| Mode | Scope | When |
|------|-------|------|
| **Sanity** | Skim diff, obvious issues | Trivial changes, config, docs, low-risk |
| **Standard** | Full change set via targeted diffs, P0-P3 checklist, spot-check tests | Most changes — balanced cost/coverage |
| **Deep** | Targeted diffs + source context, all priorities, trace data flow | Security-sensitive, core architecture, unfamiliar domain |

Announce mode: `"Reviewing in [mode] because [reason]. Adjust?"`

Default to Standard. Escalate to Deep if review surfaces unexpected complexity.

**Start high level, work down:** Focus on design and structure first. Defer naming, comments, and style until high-level issues are resolved — low-level notes often become moot after refactoring.

# Review Hierarchy

Review in this order. Stop and flag blockers immediately.

| Priority | Category | Focus |
|----------|----------|-------|
| P0 | **Security** | Injection, auth bypass, secrets exposure, unsafe deserialization |
| P1 | **Correctness** | Does it do what it claims? Edge cases? Error paths? |
| P2 | **Data integrity** | Validation, transactions, idempotency, race conditions |
| P3 | **Architecture & Operability** | Coupling, contracts, backward compat, observability, rollback |
| P4 | **Performance** | Only if measurable impact — N+1, unbounded growth, hot paths |
| P5 | **Maintainability** | Readability, naming, complexity, test quality |
| P6 | **Style** | Only if egregious or violates established conventions |

**Attention budget:** P0-P2 (70%) catch most production incidents — prioritize these. P3-P4 (20%) architectural and performance. P5-P6 (10%) only when egregious.

# Review Checklist

Not exhaustive — a mental sweep, not a checkbox exercise.

**Security (P0):**
- [ ] No hardcoded secrets
- [ ] Input validated at boundaries
- [ ] No injection vectors (SQL, command, XSS)
- [ ] Auth/authz not weakened
- [ ] Sensitive data not logged or exposed

**Correctness (P1):**
- [ ] Logic matches stated intent
- [ ] Edge cases handled (null, empty, boundary values)
- [ ] Error paths don't swallow failures silently
- [ ] Return values / exceptions match contract
- [ ] External calls/APIs verified to exist and behave as assumed
- [ ] Impact of code removal assessed (callers, dependencies)
- [ ] New behavior has tests; changed behavior has updated tests

**Data (P2):**
- [ ] Transactions wrap related mutations
- [ ] Concurrent access considered
- [ ] Migrations reversible or safe; online-safe (no long-running locks on large tables)

**Architecture & Operability (P3):**
- [ ] Respects existing patterns; no unnecessary coupling
- [ ] Public API changes intentional; backward compatibility considered
- [ ] Dependency additions justified; version changes assessed for breaking behavior
- [ ] Not relying on implicit/undocumented configuration
- [ ] New env vars documented and added to templates
- [ ] User-facing/operational changes documented; README/CHANGELOG updated
- [ ] Deployment procedure defined if relevant (db change, breaking change)
- [ ] Logs actionable (not noisy, not silent); errors include debug context
- [ ] Metrics/tracing updated if behavior changes
- [ ] Feature flags/kill switches respected if applicable
- [ ] Rollback path exists (code + data)

**Performance (P4):**
- [ ] No N+1 queries or unbounded loops
- [ ] Hot paths not degraded
- [ ] No premature optimization — flag if complexity added for hypothetical gains

**Maintainability (P5):**
- [ ] Readable without author's context
- [ ] Names reveal intent
- [ ] Comments explain *why*, not *what* — if code needs explanation, simplify it
- [ ] TODOs have ticket references — naked TODOs become stale
- [ ] Complexity proportional to problem
- [ ] Tests validate intent, not implementation; would fail on regression
- [ ] Mock-heavy implementation-testing flagged

# Feedback Format

**Severity tags:**

| Tag | Meaning | Blocking? |
|-----|---------|-----------|
| `[blocker]` | Must fix before merge — security, correctness, data integrity | Yes |
| `[concern]` | Should address — architecture, significant maintainability | Discuss |
| `[suggestion]` | Consider — minor improvements, alternatives | No |
| `[question]` | Clarify — reviewer may be missing context | No |
| `[nit]` | Take or leave — style, naming preference | No |
| `[appraisal]` | Acknowledge — good pattern, notable improvement | No |

**Structure:**
```
[tag] file:line — Brief issue

Why it matters: [impact if not addressed]
Suggestion: [concrete alternative, if any]
```
For `[nit]` and trivial `[suggestion]`: one-liner is fine.

**Tone:** Avoid "you" — focus on code, not coder. Use "we" or omit the subject. "You forgot to close the handle" → "File handle left open" or "Can we close the handle here?"

**Example:**
```
[blocker] auth/login.py:47 — SQL injection via username parameter

Why it matters: Allows auth bypass and data exfiltration
Suggestion: Use parameterized query: cursor.execute("SELECT ... WHERE user = %s", (username,))
```

**Repeated patterns:** Flag 2-3 occurrences, then ask the author to fix the pattern throughout.

# Review Anti-Patterns

**Don't:**
- Invent requirements or failure modes not implied by the stated goal
- Nitpick style without a style guide (let linters handle it)
- Suggest rewrites when the code works and is readable
- Block on "I would have done it differently" without concrete risk
- Miss security issues while debating naming
- Demand perfection — good enough ships, perfect doesn't
- Accept style changes mixed with functional changes — ask to split
- Expand scope to untouched lines — file a bug or fix it yourself

**Do:**
- Ask "How would I solve this?" — use the difference to guide feedback
- Frame findings as questions when uncertain — you might be missing context
- Ask questions before demanding changes
- Distinguish preference from requirement
- Consider author's experience level — teach, don't gatekeep
- Offer alternatives as questions ("What about...?") — teaches without prescribing
- Treat your own confusion as signal — future maintainers will struggle too
- Document in code, not PR — future readers won't see PR discussions
- Before posting: Is it true? (opinion ≠ truth) Is it necessary? (no nagging, no ego) Is it kind? (no shaming)
- Acknowledge one good decision per substantial review when genuine (brief, no cheerleading)
- Surface low-probability edge cases as `[suggestion]` with risk assessment (likelihood, impact, failure mode) — don't suppress legitimate findings; document the tradeoff

# Approval Criteria

**Approve when:**
- No blockers remain
- Concerns addressed or explicitly accepted as tech debt (record in `TECH_DEBT.md`)
- Code is better than before (not perfect, better)
- You'd be comfortable debugging this at 2am
- `[suggestion]`/`[nit]` don't block — unblock progress while noting improvements
- "No notes" is valid — don't feel compelled to find something wrong

**Request changes when:**
- Blockers exist (P0-P2)
- Significant concerns unaddressed without rationale
- Intent unclear and author hasn't clarified

**Comment without blocking when:**
- Only suggestions/nits remain
- Concerns acknowledged with reasonable deferral plan

# Re-Review Protocol

Iterative reviews converge by narrowing scope. A re-review after fixes is not a fresh review.

**Re-review scope:**

For round 2 after fixes, review only:
- whether prior `[blocker]` / `[concern]` findings were fixed at root cause
- whether the fix introduced new P0-P2 issues
- tests/docs directly added to prove or document the fix

Do not broaden into unrelated code, re-derive the whole diff, or escalate methodology to match a prior reviewer. Review mode cannot exceed the original review mode unless the fix itself introduced new complexity or risk.

**Convergence rule:**

- **Round 1:** Full review according to selected mode.
- **Round 2:** Focused verification of prior findings. New findings are allowed only if they are P0-P2 and introduced by the fix.
- **Round 3+:** Only allowed with an explicit question, such as "is X fixed?", "does Y still apply?", or "did this test prove the failure mode?" Open-ended re-review is not permitted.

Stop when:
- no `[blocker]` remains
- `[concern]` items are fixed, accepted as tech debt, or explicitly deferred with rationale
- validation evidence exercises the changed behavior
- no new P0-P2 issue was introduced by the fix

At that point, approve or comment. Remaining `[suggestion]`, `[nit]`, and low-risk `[question]` items do not justify another broad pass.

**Blast-radius proportionality:**

Review depth scales with blast radius, not with prior review depth.

- **Low blast radius:** dev tooling, internal scripts, config, docs. At most one Standard review plus one focused verification pass.
- **Medium blast radius:** production behavior without schema/auth/public API impact. Standard review plus focused re-reviews until blockers close.
- **High blast radius:** auth, security, data integrity, migrations, public API, production runtime. Deep review is allowed; re-review still narrows to fixes unless the design changed.

**Anti-pattern:**

**Methodological arms race:** matching or exceeding a prior review's rigor to appear credible rather than because the change warrants it. This creates non-converging review loops. Prefer a smaller, evidence-focused re-review over a broader, more impressive one.

# Failure Mode Sweep

Ask questions to surface risks — do not assume intent or invent scenarios. Before finalizing, ask:
- If this breaks, how would we notice?
- What's the blast radius?
- What's the worst realistic misuse?

If unsure, raise as `[question]` or `[concern]`, not `[blocker]`.

# Review Summary Format

**Compact** (Approve/Comment, zero blockers/concerns, ≤3 suggestions, ≤3 files, high confidence):
```
Review: [mode] — Approve
```

**Full** (everything else):
```
Review: [mode] — [verdict: Approve / Request Changes / Comment]

Blockers: [count or "None"]
Concerns: [count or "None"]
Suggestions: [count]

Overall: [1-2 sentence assessment]
Blast Radius: [Low: internal refactor | Medium: logic change | High: migration/public API]
Confidence: [high: thorough | medium: focused on key areas | low: quick pass]
Next step: [e.g., "Merge after minor suggestions" | "Ready for another look"]
```

# Mode-Specific Behavior

**Pairing (default):** All prompts apply. "Adjust?" allows human to override review mode.

**§BRAND_NAME_TITLE§ (multi-agent):** No interactive prompts.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| Mode announcement ("Adjust?") | Announce mode, no prompt |
| "Ask the author" / "Clarify" | Check task spec and blackboard; if still unclear, note as `[question]` |
| "Consider suggesting a split" | Note as `[concern]` — do not block review |
