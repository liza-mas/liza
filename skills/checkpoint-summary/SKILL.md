---
name: checkpoint-summary
description: Summarize artifacts produced by §BRAND_BINARY_NAME§ agents for human checkpoint review
mode: pairing
---

## Purpose

After agents complete a planning or writing phase (epic planning, story writing, spec
generation), summarize their output so a human can efficiently review what was decided,
what remains open, and where their attention is needed.

This skill answers: **"What did the agents produce, what did they decide, and what do I
need to weigh in on?"**

The agents already did the work — planning, reviewing, approving. This skill reads their
outputs and distills them into a checkpoint summary that respects the human's time.

**Distinct from spec-review**: spec-review audits spec quality. This skill *summarizes*
what was already reviewed and approved, surfacing only what needs human judgment.

## Trigger

Use this skill when:
- Epic planning completes and the human needs a checkpoint summary
- Story writing completes across multiple agents
- Any multi-agent phase produces artifacts the human hasn't read
- User asks "what did the agents produce?", "what needs my attention?", or "summarize the plans"
- Orchestrator requests a human checkpoint (§BRAND_NAME_TITLE§ mode)

## Inputs

The single entry point is **`§BRAND_PROJECT_DIRNAME§/state.yaml`** — the source of truth for all §BRAND_NAME_TITLE§ state.

From `state.yaml`, the skill reads:
- **`goal.spec_ref`**: the upstream source document the agents worked from
- **`tasks[]`**: each task with its scope, status, output capabilities, approvals, and history
- **Artifact refs** (read all that exist, in priority order):
  - `tasks[].plan_ref` / `tasks[].arch_ref` — task-level planning and architecture artifacts
  - `tasks[].output[].plan_ref` / `tasks[].output[].arch_ref` — output-entry artifacts
  - `tasks[].spec_ref` — task-level spec (may differ from `goal.spec_ref`)
- **`tasks[].approvals[]`**: review verdicts with provider/diversity context (canonical);
  fall back to `tasks[].approved_by` if `approvals[]` is absent
- **`tasks[].history[]`**: full event timeline (claimed, checkpoint, submitted, approved, merged)
- **`sprint.status`** and **`sprint.checkpoint_trigger`**: why the checkpoint was triggered

No other discovery is needed. Read every artifact file referenced by the ref fields above.
Read the upstream source (`goal.spec_ref`). Everything else is in the state file itself.

## Protocol

### Phase 1: Inventory

1. **Read `§BRAND_PROJECT_DIRNAME§/state.yaml`** to understand the full pipeline state: goal, tasks, agents,
   sprint status, and checkpoint trigger.

2. **Read the upstream source** (`goal.spec_ref`) to understand what the agents were working
   from — entities, decisions, constraints, interactions, scope boundaries.

3. **Read all produced artifacts** referenced by task-level refs (`plan_ref`, `arch_ref`)
   and output-entry refs (`output[].plan_ref`, `output[].arch_ref`). Skip entries where
   no ref field points to a file. For each artifact read:
   - What was produced (title, scope, capabilities/stories count)
   - What verdict the reviewers gave (`tasks[].approvals[]`; fall back to `approved_by`)
   - Key events from `tasks[].history[]` (rejections, re-reviews, anomalies)

### Phase 2: Extract

From the artifacts and agent outputs, extract three categories:

#### Decisions Made

Choices the agents made that the upstream source left open. For each:

- **What was decided**: the specific choice, in one sentence
- **Where**: which artifact and section (e.g., EP-002 CAP-003, or story-007 AC-2)
- **Confidence**: if the agent flagged it (HIGH/MEDIUM/LOW), report that; if they didn't
  flag it at all, mark it as **unflagged**
- **Reversible?**: can this be changed later, or does downstream work lock it in?
- **Departures from upstream**: decisions that contradict or reinterpret the upstream text
  get flagged separately — even if defensible, the human should know

#### Open Points

Items that remain unresolved after the agent work:

- **Open questions**: items agents explicitly flagged as needing human input (OQ-tagged)
- **Gaps**: things no artifact addresses that the upstream source expects
- **Cross-artifact inconsistencies**: places where sibling artifacts disagree
- **Underspecified areas**: concepts mentioned across multiple artifacts but never defined
  in any of them (e.g., "clusters and tags" referenced in filtering, listing, and CLI
  but never given a creation mechanism)

#### Risks

Implementation risks the artifacts create or carry forward:

- **Reuse assumptions**: references to existing components without compatibility assessment
- **Atomicity/complexity**: operations described as atomic that may be difficult to implement
- **Handoff gaps**: one artifact's output is another's input, but the interface isn't defined
- **Irreversible operations**: without undo or rollback being in scope

### Phase 3: Prioritize

Not everything needs human attention. Classify each item:

| Priority | Meaning | Action needed |
|----------|---------|---------------|
| **Decide** | Human must make a choice before next phase starts | Present the decision with options |
| **Confirm** | Agents made a reasonable choice — human should validate | Present the decision, default is accept |
| **Note** | Worth knowing, no action needed | Include in summary, don't interrupt for it |

**Prioritization heuristics:**
- Unflagged decisions that depart from upstream text → **Decide**
- LOW-confidence assumptions → **Decide**
- MEDIUM-confidence assumptions on irreversible operations → **Decide**
- MEDIUM-confidence assumptions on reversible operations → **Confirm**
- HIGH-confidence assumptions → **Note** (unless they depart from upstream)
- Gaps that block the next phase → **Decide**
- Gaps that affect only implementation details → **Note**
- Risks with no mitigation path → **Confirm**
- Risks with natural mitigation during implementation → **Note**

### Phase 4: Report

Present in this format. **Decide items first, then Confirm, then Notes.**

```markdown
# Checkpoint Summary: [Phase Name]

## Status

| Artifact | Scope | Verdict |
|----------|-------|---------|
| [name] | [one-line scope] | [Approved / Rejected / Conditional] |

> N decisions needing input · M items to confirm · K notes

## Decisions Needing Human Input

Items where the human must choose before the next phase starts.

### [Decision Title]

- **Context:** [What the upstream says or doesn't say]
- **Agent decision:** [What the agents chose]
- **Why it needs you:** [What makes this non-obvious — departure from upstream,
  irreversible, low confidence, or gap]
- **Options:** [If applicable — confirm agent choice, override with X, or defer]

## Decisions to Confirm

Agents made reasonable choices. Confirm or override.

### [Decision Title]

- **Agent decision:** [What was chosen]
- **Rationale:** [Why it's reasonable]
- **Override if:** [When the human might want something different]

## Open Points

Unresolved items carried forward.

### [Item Title]

- **What's open:** [Description]
- **Impact:** [What's affected if unresolved]
- **Where it surfaces:** [Which artifacts reference this]

## Risks

### [Risk Title]

- **What could go wrong:** [Description]
- **Which artifacts:** [Where this risk lives]
- **Mitigation:** [If any exists in the plans, or "none specified"]

## Notes

Brief items worth knowing but not requiring action.

- [Item]: [One-line description]
```

## Constraints

- **DO NOT** modify source artifacts — only the generated checkpoint summary report may be written
- **DO NOT** re-review or second-guess the reviewer's verdict — summarize it
- **DO NOT** bury decisions in long prose — one item, one heading, one clear question
- **DO** surface unflagged decisions — the highest-value findings are choices agents made
  without marking them as assumptions
- **DO** state the specific question the human needs to answer, not just "this needs review"
- **DO** keep the summary scannable — a human should grasp the state in under 2 minutes
- **DO** report the total count of decisions/open points/risks in the Status section so
  the human can gauge the review effort before diving in

## Anti-Patterns

- **Re-reviewing approved work**: The agents already reviewed. Don't re-evaluate the
  architecture, question design choices that were explicitly approved, or propose
  alternatives. Summarize what was decided.
- **Exhaustive listing**: Dumping every assumption from every artifact. The human doesn't
  need to see 40 HIGH-confidence assumptions that are obviously correct. Filter to what
  matters.
- **Missing the silent decisions**: Focusing on what agents flagged (assumptions, open
  questions) and ignoring what they didn't. The most important items are often decisions
  baked into the plan without being called out.
- **Vague attention items**: "The CLI design needs review" is not actionable. "The CLI
  plan doesn't expose project deletion, but the API supports it — should users be able
  to delete projects in v1?" is actionable.
- **Severity inflation**: Marking everything as "Decide". Most items are "Confirm" or
  "Note". Reserve "Decide" for items where the human genuinely has a choice to make that
  changes the downstream work.

## Integration

| Skill | Relationship |
|-------|-------------|
| **epic-writing** | Upstream producer. Summarize epic plans at the planning checkpoint. |
| **user-story-writing** | Upstream producer. Summarize stories at the story-writing checkpoint. |
| **spec-review** | Complementary. spec-review finds spec defects; this summarizes agent decisions. Different purposes, can run on the same artifacts. |

## Mode-Specific Behavior

**Pairing mode:** Present the summary interactively. Walk through "Decide" items one at a
time, collecting the human's decision before moving to the next. For "Confirm" items,
present as a batch — the human can scan and override selectively. End with a count of
decisions made and items still open.

**§BRAND_NAME_TITLE§ mode:** Checkpoint Summary operates autonomously within task scope. Write the
report to the worktree (e.g. `docs/checkpoint-summary.md`) and submit for review.
If any "Decide" items exist, mark BLOCKED with `blocked_reason` summarizing the
decisions needed and `blocked_questions` listing each one — the human must resolve
them before the next phase starts. If only "Confirm" and "Note" items exist, submit
normally.

| Pairing Prompt | §BRAND_NAME_TITLE§ Behavior |
|----------------|---------------|
| "N decisions need your input — walk through them?" | Mark BLOCKED; report in worktree |
| "Agents made N decisions — all look reasonable. Confirm?" | Submit for review; report in worktree |
| "No open points — ready for next phase" | Submit for review; report in worktree |
