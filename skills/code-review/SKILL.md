---
name: code-review
description: Two-sided code review protocol — reviewers raise findings, authors answer them. Use when reviewing code (PRs, pending changes) or when responding to review feedback, review comments, or a rejected verdict.
---

Code review is risk mitigation, not gatekeeping — catching what the author couldn't see, and occasionally sharing a better pattern when it genuinely helps.

Two-sided protocol. Reviewers load it to raise findings; authors load it to answer
them. Both sides read the whole file — a contested finding converges only when both
agree what `[blocker]` means and what closes one. Everything through *Review Summary
Format* is shared; *Answering Findings* is author-side.

# Review Context

Before reviewing, establish context:
- **Scope:** Default to staged files (`git diff --cached --name-only`, then `git diff --cached --stat`, then targeted path diffs). For PRs or commits, inspect changed files and stats before reading targeted hunks. Only broaden if explicitly asked.
- **Local working tree:** For local reviews, triage unstaged and untracked files before review. If related but not staged, surface this explicitly and include only if the review target is "pending changes"; if unrelated, ignore; if unclear, ask before including.
- **Initial scope:** Before reading the diff, record in one line what the change set out to do — take it from the author's stated intent when supplied (Pairing: Change Summary; multi-agent: task description and done-when; PRs: description and linked ticket), derive it only when it is not, and clarify with the author when it is unclear. This is the anchor for every later round: it bounds which *behavior* is required, not which code is inspected. Every submitted line is still reviewed for P0-P2 defects. Work beyond the initial scope is scope creep, not thoroughness — review it, then flag it `[overreach]`.
- **Timing:** Is now the right time for this functionality? Half-baked or premature additions warrant a `[question]`.
- **Approach:** Round 1 only. Is the approach sound, not merely correct — would another team take this shape, and was the first workable rung of CORE's Minimality Ladder taken? Raise an alternative only when it is named and its benefit demonstrable (`have-you-considered`); "I'd have done it differently" is not a finding. `[question]` when exploratory, `[concern]` when materially cheaper and still cheap to switch. After round 1 the approach is settled — reopening it is relitigation.
- **Absence:** Review what should be here and is not. Take the baseline from the author's declarations (Pairing: Change Summary success criteria, doc impact, test impact; multi-agent: done-when) and check each against the diff. Diff-first reading optimizes for what is present; absence has to be sought deliberately. Where no declarations exist, say so — it caps confidence.
- **Diff-first:** Read bounded diff context before source files. Prefer name-only/stat first, then targeted path or hunk diffs. Only read source when a finding needs surrounding context. Never pre-read the entire codebase.
- **Sources:** State what you have read before drawing conclusions, per CORE Rule 5, and extend it as you read. The summary line reports this list; it does not create it. A conclusion reached before its source was read is not grounded by reading that source afterwards.
- **Size:** Beyond 800 lines, 20 files, 80K chars total, or 20K chars in one file — large diffs hide bugs. Consider suggesting a split (PR) or incremental commits (pending); avoid unbounded full-diff reads, classify files and inspect targeted paths or hunks, and verify critical findings against source before tagging `[blocker]` or `[concern]`.
- **Reviewer limits:** If reviewing outside your expertise, say so. Make assumptions explicit.

# Review Modes

**Sanity** skims the diff for obvious issues — trivial changes, config, docs.
**Standard** is the default: full change set via targeted diffs, P0-P3, spot-checked tests.
**Deep** adds source context, all priorities, and data-flow tracing — security-sensitive
work, core architecture, unfamiliar domains. Escalate to Deep if the review surfaces
unexpected complexity.

Announce mode and scope: `"Reviewing [scope] in [mode] because [reason]. Adjust?"`

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

**Attention budget:** P0-P2 (70%) catch most production incidents. P3-P4 (20%).
P5-P6 (10%) only when egregious.

# Review Checklist

Not a sweep of what review normally covers — you already do that. These are the
checks that get skipped:

- [ ] Worst realistic misuse considered, not just declared inputs
- [ ] External calls and APIs verified to exist and behave as assumed
- [ ] Impact of code removal assessed — callers, dependencies
- [ ] Concurrent access considered
- [ ] Migrations reversible, and online-safe on large tables
- [ ] Not relying on implicit or undocumented configuration
- [ ] Operational surface documented — env vars, README/CHANGELOG, deployment steps
- [ ] Declared `Doc Impact` tested against the diff — a spec describing behavior this change alters is updated, or `none` is defensible
- [ ] Observability intact — logs actionable, metrics updated if behavior changed
- [ ] Rollback path exists, code and data
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
| `[overreach]` | Beyond initial scope, or a larger solution than the finding required — shrink or split | Yes, in corrective rounds |
| `[vestigial]` | Leftover from a superseded design — delete | Only when it worsens net value or risk |

**Structure:**
```
[tag] file:line — Brief issue

Why it matters: [impact if not addressed]
Likelihood: [how it is reached — entry point, input, condition]
Suggestion: [concrete alternative, if any]
```
For `[nit]` and trivial `[suggestion]`: one-liner is fine.

On `[blocker]` and `[concern]`, add `Closure condition:` — the observable state required for approval. Any `Suggestion:` is advisory; the author chooses the implementation. For findings about a mismatch between code and docs/spec/description, name which side is authoritative — otherwise the response defaults to changing code. A stated impact assessment bounds the response: escalating above it needs the reviewer's explicit agreement.

**Severity requires evidence.** `[blocker]` requires a concrete failure path, a violated
invariant, or a deterministic build or validation failure; a hypothetical naming none of
these is `[concern]` at most. Severity is likelihood × impact, assessed before the tag is
chosen, not defended after — and a low-likelihood failure that is catastrophic or
irreversible can still block.

**Repeated patterns:** Flag 2-3 occurrences, then ask the author to fix the pattern throughout.

# Review Anti-Patterns

**Don't:**
- Invent requirements or failure modes not implied by the stated goal
- Nitpick style without a style guide (let linters handle it)
- Suggest rewrites when the code works and is readable
- Block on "I would have done it differently" without concrete risk or a demonstrably cheaper named alternative
- Demand perfection — good enough ships, perfect doesn't
- Fill the Approach line with something that sounds considered — "sound" with nothing named is an empty field. Name the alternative you weighed and why it lost, or write "no alternative considered" and let that be visible
- Inflate severity to justify the review — a genuine nit can accompany an approval, but promoting one to `[concern]` or `[blocker]` costs a round it has not earned
- Accept style changes mixed with functional changes — ask to split
- Expand scope to untouched lines — file a bug or fix it yourself
- Stack a second guard on the same failure at the same trust boundary — that is `[overreach]`; independent controls at separate boundaries are defence in depth

**Do:**
- Ask "How would I solve this?" — use the difference to guide feedback
- Ask before demanding — frame findings as questions when uncertain, since you might be missing context
- Offer alternatives as questions ("What about...?") — proposing is the exposed move and the one most often skipped; the question form makes it cheap
- Distinguish preference from requirement
- Treat your own confusion as signal — future maintainers will struggle too
- Document in code, not PR — future readers won't see PR discussions
- Acknowledge one good decision per substantial review when genuine (brief, no cheerleading)
- Surface low-probability edge cases as `[suggestion]` with risk assessment (likelihood, impact, failure mode) — unless the failure is catastrophic or irreversible, which can still block. Don't suppress legitimate findings; document the tradeoff

# Approval Criteria

**Net value gate — mandatory at the approval boundary.** A clean ledger is not
approval. Every verdict states two values, one concrete sentence each: **as submitted**
and **with the findings resolved** — benefit obtained, complexity or risk retained,
versus not merging. No verdict is exempt; the assessment is the forcing function.

The resolved value decides. Negative as submitted but positive resolved is ordinary
Request Changes. Still marginal or negative once resolved means the change should not
exist in this shape — name what changed since it opened and produce a Decision Request,
never a unilateral close. Run the vestigial sweep first; it feeds this gate.

**Approve when:**
- No blockers remain
- Concerns addressed or explicitly accepted as tech debt (record in `TECH_DEBT.md`)
- Code is better than before (not perfect, better)
- You'd be comfortable debugging this at 2am
- `[suggestion]`/`[nit]` don't block — unblock progress while noting improvements
- Confidence is part of the verdict — do not approve at low confidence. Take the deeper pass, or state the limitation and let the human accept it explicitly
- "No notes" is the correct output for a clean change, not a failure to review. Approving fast is the effective strategy; genuine suggestions and nits ride along with the approval — only a blocking finding must be worth the round it costs

**Request changes when:**
- Blockers exist (P0-P2)
- `[overreach]`, or blocking `[vestigial]`, findings remain in a corrective round
- Significant concerns unaddressed without rationale
- Intent unclear and author hasn't clarified

**Comment without blocking when:**
- Only suggestions/nits remain
- Concerns acknowledged with reasonable deferral plan

# Answering Findings

Author-side. Load on receiving review feedback — a REJECTED verdict in
multi-agent mode, review comments in Pairing.

A corrective commit answers findings. It is not an opportunity to improve the
change. Run the reviewer's tests against it before submitting:

- **Minimal-fix test:** for each finding, what is the least change that makes it
  false? If the intended fix is materially larger it is `[overreach]`, and the
  reviewer will say so. Say it first. A new interface, dependency, migration, or
  abstraction introduced to answer a `[concern]` or `[suggestion]` is `[overreach]`
  by default.
- **Scope bound:** files named in the findings, plus their tests. Growth beyond
  that is declared and justified in the submission, or it becomes the next finding.
  The finding's stated impact bounds the fix; a larger fix is not better compliance.

**Contesting.** Complying with a finding that causes greater harm is not
compliance — it is the next defect. A finding may be returned unfixed as
`[contested]`, which requires a named concrete harm: the behavior that breaks,
the invariant violated, the cost incurred. "This would be complex" is not a
named harm and does not open a contest.

**When the reviewer escalates.** A Decision Request or `Recommend Reframe` is not a
finding to fix, and a corrective pass is not an answer to one. Address the decision on
its merits — agree, or refute its premise with evidence — and leave the call with the
human. Do not put your own approval request in front of the human while theirs is
unanswered: that is two asks, and one of them quietly disappears.

**Comply and record.** Below contesting: implement the fix and record the objection —
`trade_off` in multi-agent mode, the Change Summary trade-offs row in Pairing. It costs
no round and blocks nothing, and it is the right move for a harm too diffuse to name:
coupling introduced, future changes made harder, a shape that will be regretted. Silent
compliance on a real objection is the failure mode, not contesting.

A refuted contest is a successful contest. It surfaced evidence the reviewer had and the
author didn't, which is the point — being answered is not being wrong (CORE Rule 14).

The four permitted reviewer responses are defined in CORE Rule 12. Escalate's carrier
here: Pairing a Decision Request; multi-agent, declared in the rejection verdict and
carried to the human by the doer via `mark-blocked`.

If fixing A breaks B and fixing B breaks A the spec is broken, not the code (CORE Rule 11):
that is Escalate, not another round.

# Transition Reference

One definition for the review exchange. Contracts own state transitions and
permissions; this table owns the response vocabulary and its carriers. Where they
diverge, that is a defect in whichever change introduced it — fix both, do not pick.

| Event | Actor | Permitted response | Pairing carrier | Multi-agent carrier | Closure |
|-------|-------|--------------------|-----------------|---------------------|---------|
| Finding raised | Reviewer | A tag per *Feedback Format* | Review output | Rejection reason on `submit-verdict REJECTED` | Author answers it |
| Finding answered | Author | Fix; fix and record the objection; or contest naming a concrete harm | Reply to the review | Resubmission commit message; an empty commit when no code changes | Reviewer responds |
| Contest received | Reviewer | Accept, Counter, Refute, Escalate — never bare restatement | Review output | Verdict text | One of the four is stated |
| Contest accepted | Author | Record the trade-off | Change Summary, trade-offs row | Coder logs `trade_off` | Finding closed |
| No consensus | Author | Escalate | Decision Request as the approval request's `Ask` | `mark-blocked` — harm in `blocked_reason`, disagreement in `blocked_questions` | Human or Orchestrator rescopes |
| Reframe, or resolved net value not positive | Reviewer | `Recommend Reframe` | Decision Request; its own comment on a PR | `submit-verdict REJECTED` whose reason declares the reframe | Author marks BLOCKED rather than resubmitting |

# Re-Review Protocol

Reviews converge by bounding the change set, not by narrowing inspection. Every round reviews the current change set in full — the change set is what cannot grow.

**Continuation or independent.** A *continuation* is this reviewer's next round:
reconcile prior findings, do not re-derive them. An *independent* review is round
1 of its own whatever verdicts already exist — full change set, all findings
published including P3-P6 even when the verdict is Approve, no downgrading to
match an existing approval. Do not restate a finding another reviewer already raised
— a second review earns its cost by complementing, not echoing. Speak to a prior
finding only to disagree with its severity or its resolution.

Prior rounds are the ones this reviewer ran in this session. In Pairing the
session boundary is the reviewer boundary — a fresh session is an independent
reviewer, and review notes found on disk are evidence, not its own rounds.

**Every round:**

1. Reconcile each prior finding and classify it: **RESOLVED**, **ACCEPTED** (rationale accepted, no code change), **PARTIALLY ADDRESSED**, or **STILL PRESENT**. Thread resolution, acknowledgement, and outdated diff markers are not evidence.
2. Inspect the whole change set for P0-P2 every round. After round 1, only unresolved prior findings and new P0-P2 defects may block; new P3-P6 observations route to follow-up.
3. Check the change set against the initial scope — file count alone is a signal, not a verdict.
4. Report the ledger: `Round N — remaining X→Y, files A→B`.

Do not escalate methodology to match a prior reviewer. Review mode cannot exceed the original mode unless the change itself introduced new complexity or risk.

**Corrective commit review:**

The author's obligations are in *Answering Findings*; judge against those.

- **Undeclared growth is the finding:** name each file outside the answered findings' scope and ask for it to be split out or reverted. Declared and justified growth is not a finding — a scope extension in multi-agent mode, the round record in Pairing.
- **Collapse before extending:** when a finding concerns code that exists only because of the chosen fix, ask first whether shrinking the fix eliminates the finding. If it does, flag `[overreach]` on the fix rather than opening a round on the states it invented. Reviewing a surface the fix created is how loops fail to converge.
- **Vestigial sweep:** from round 3, or after any mid-review design change, read
  the accumulated change set whole rather than round by round. Code is
  `[vestigial]` on evidence that it exists only to serve a design since replaced
  and no longer earns its complexity — not on a count of callers. Block only when
  it worsens net value or creates correctness or security risk; otherwise raise a
  `[concern]`. Rounds see deltas; nobody sees the sum unless this runs deliberately.
- **Proportionality:** over-engineering accretes across rounds, not in the first
  proposal — each round's fix was justified locally and the sum may not be. When the
  sweep runs, ask whether the accumulated solution is still proportionate to the
  problem. Good enough beats perfect, and the reviewer's own concerns are the usual
  driver of the drift.
- **Suggestions:** a `[suggestion]` is addressed when it is inside the initial scope, or is a no-behavior-change edit to a file already in the change set. Everything else routes to follow-up. Deferring a suggestion is safe; losing it is not.

Stop when no `[blocker]`, `[overreach]`, or blocking `[vestigial]` remains, `[concern]` items are fixed or deferred with rationale, validation exercises the changed behavior, and no new P0-P2 issue was introduced. Remaining `[suggestion]`, `[nit]`, and low-risk `[question]` items do not justify another round.

**Divergence:** files growing while remaining findings do not fall is non-convergence. Stop, restate the original finding set, escalate to the author or human.

**Blast-radius proportionality:** depth scales with blast radius, not with prior review depth. Dev tooling, scripts, config and docs get at most one Standard review plus a focused verification pass. Production behavior without schema, auth or public API impact gets Standard plus focused re-reviews until blockers close. Auth, security, data integrity, migrations, public API and production runtime may have a Deep review, and later rounds still reconcile rather than re-derive.

Matching or exceeding a prior review's rigor to appear credible — rather than because the change warrants it — is a methodological arms race, and it is how loops stop converging. Prefer a smaller, evidence-focused re-review over a broader, more impressive one.

# Decision Request

When the loop is the problem rather than either side of it, the call belongs to
the human. Both roles use this form.

**Signature:** findings not falling while the change set grows; each round's
blockers landing in what the previous round's fix introduced; settled work held
across rounds by an unsettled subsystem; resolved net value marginal or negative.

The format is the content:

- **Adjacent to the decision.** In Pairing it is the last thing before the `Ask`,
  and the `Ask` is the decision it requests. On a PR it is its own top-level comment.
  What buries it is a soft heading and a list of fixes above it, not its position.
- **One recommendation.** Alternatives only when genuinely competitive. Never
  offer return-to-status-quo — if it were viable there would be no decision.
- **Name the cost of not deciding,** in the reader's units: rounds spent,
  verification budget, work held hostage.
- **One softener maximum.** "That's your call" is the whole quota.
- **Signal strength encodes need, not insistence.** A weak signal says "I can
  handle this." A strong one says "I need you here." Calibrate each raise to the
  actual need. A prior "no" is not a reason for the signal to drop — deference is
  not new evidence. If the need is unchanged the signal is unchanged; if it grew,
  so does the signal.

# Review Summary Format

**Compact** (Approve/Comment, zero blockers/concerns, ≤3 suggestions, approach sound, high confidence — size is not a criterion):
```
Review: [mode] — Approve
Net value: [one sentence: benefit obtained, complexity retained, versus not merging]
```

**Full** (everything else):
```
Review: [mode] — [verdict: Approve / Request Changes / Comment / Recommend Reframe]

Blockers: [count or "None"]
Concerns: [count or "None"]
Suggestions: [count or "None"]   ← None/None/None is a complete review

Overall: [1-2 sentence assessment]
Approach: [round 1 — sound, or the named alternative and its benefit]
Absence: [baseline used — nothing missing, or what is]
Sweep: [from round 3 or a mid-review design change — vestigial or disproportionate, or neither]
Net value: [as submitted — one line] / [with findings resolved — one line]
Blast Radius: [Low: internal refactor | Medium: logic change | High: migration/public API]
Confidence: [high: thorough | medium: focused on key areas | low: quick pass]
Sources: [what was read — diff, source files, specs, ADRs and decision records, validation output]
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
| `[overreach]` finding | Also log a `scope_deviation` anomaly |
| "Routes to follow-up" | Record in the verdict text — no anomaly. Reserve `debt_created` for a known deficiency retained in the implementation |
| Non-convergence (see Divergence) | Log `scope_deviation`, or `retry_loop` when the coder is cycling; `review_budget_exhausted` is planner-owned at 5 cycles — do not preempt it |
| `[vestigial]` finding | Same as `[overreach]` — also log a `scope_deviation` anomaly |
| `[contested]` response | Reviewer answers in the verdict text and logs nothing. If the reviewer accepts, the coder logs `trade_off` (coder-owned). If no consensus follows, the doer marks the task BLOCKED — see `MULTI_AGENT_MODE.md` Iteration Protocol |
| Decision Request | Not a reviewer artifact here. It reaches the human through the doer's `mark-blocked` reason and questions; the Orchestrator rescopes |
| Resolved net value marginal/negative | Record in the verdict text — no anomaly |
| Low confidence blocks approval | Issue REJECTED with `"insufficient information to complete review"` and log `reviewer_loop` — the existing path in `MULTI_AGENT_MODE.md`. The reviewer has no `mark-blocked`; `submit-verdict` is its only channel |
| `Recommend Reframe` verdict | Pairing and PR only. Submit REJECTED whose reason states the reframe and directs the doer to mark the task BLOCKED rather than resubmit |
