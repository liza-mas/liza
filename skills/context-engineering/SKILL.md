---
name: context-engineering
description: "Analyze §BRAND_NAME_TITLE§ `§BRAND_PROJECT_DIRNAME§/agent-prompts/` and `§BRAND_PROJECT_DIRNAME§/agent-outputs/` from a context-engineering perspective: prompt payload shape, context budget use, cacheability, duplicated or missing context, instruction hierarchy, tool-output pressure, role-specific context fit, and prompt-output feedback loops. Use when diagnosing agent context bloat, prompt drift, poor agent handoffs, repeated misunderstandings, excessive tool output, or whether §BRAND_NAME_TITLE§ agents received the right information at the right time."
---

# Context Engineering

## Scope

Analyze only `§BRAND_PROJECT_DIRNAME§/agent-prompts/` and `§BRAND_PROJECT_DIRNAME§/agent-outputs/` unless the user names other artifacts.

This skill is complementary to `§BRAND_NAME_LOWER§-logs`: `§BRAND_NAME_LOWER§-logs` finds operational failures and token/tool patterns; this skill explains whether the prompt and context design caused or amplified those patterns.

## Protocol

### 1. Inventory Before Reading

Run the corpus indexer first:

```bash
python3 skills/context-engineering/scripts/context-corpus-index.py §BRAND_PROJECT_DIRNAME§
```

Use the generated index as the primary source for mechanical discovery: inventory, prompt/output pairing, size and pressure signals, outcome signals, common tools, MCP usage, and sample selection. The index is not evidence of causality by itself.

The indexer supports both Claude rich stream-json logs and Codex sparse `item.completed` logs. Check the reported format counts before assuming which fields are available.

Use indexer options deliberately:

```bash
python3 skills/context-engineering/scripts/context-corpus-index.py §BRAND_PROJECT_DIRNAME§ --json
python3 skills/context-engineering/scripts/context-corpus-index.py §BRAND_PROJECT_DIRNAME§ --max-pair-minutes 30
python3 skills/context-engineering/scripts/context-corpus-index.py §BRAND_PROJECT_DIRNAME§ --sample-limit 25
```

- Use `--json` when exact pair metadata, token fields, or full metrics are needed.
- Use `--max-pair-minutes` to control how strict same-role timestamp pairing should be.
- Use `--sample-limit` to expand or shrink top lists and the sampling plan.

If a `§BRAND_NAME_LOWER§-logs` report or analyzer output is available, use it as the first sampling guide. Prioritize roles, runs, or timestamps with repeated tool failures, broad tool-result volume, duplicated task-local material, growing prompts, low cache reuse for expected-stable prefixes, or blocked/rejected task outcomes.

If `§BRAND_NAME_LOWER§-logs` and context-engineering evidence disagree, report the disagreement explicitly and keep the narrower claim supported by direct prompt/output evidence. Example: `§BRAND_NAME_LOWER§-logs` may correctly flag token pressure while prompt shape is not the cause.

Before opening raw prompt/output files, use the index's **Sampling Plan** plus the sections relevant to the question: **Largest Prompts**, **Largest Outputs**, **High Tool-Output Pressure**, **Outcome Signal Mentions**, **Role Distribution**, **Prompt Size Trends**, **Common Tools**, **MCP Usage**, and **Pairing**.

Treat indexer outcome signals as text mentions until confirmed by structured state, blackboard, or source context. They are good sampling signals, not proof that a verdict or status transition occurred.

Use the script's pair confidence labels to guide evidence tier and confidence: `exact-stem`, `within-5m`, `within-30m`, `within-2h`, `low-confidence`, and `no-pair`. Report the pairing confidence or matching window for prompt-causality claims.

Classify evidence tier before making claims:
- **Output-only**: behavior, tool-pressure, token-pressure, struggle, rejection, or blocked-outcome finding
- **Prompt + output pair**: prompt-causality finding
- **Prompt + output + source template/config**: fix-localization finding
- **State/blackboard/spec support**: higher-confidence handoff, continuity, and missing-context finding

Fallback indexing when the script is unavailable:
- Count prompt files, output files, suffixes, and total bytes.
- Group files by role and timestamp parsed from filenames.
- Pair outputs to same-role nearest prior prompts and report the matching window.
- Use `wc -c` to identify largest prompts and outputs.
- Search output text for tool-output dominance, errors, rejected/blocked mentions, and broad exploratory reads.
- Limit claims to **output-only** and **prompt + output pair** findings unless source templates/configs/state files are also inspected.
- Do not make role-distribution, prompt-trend, aggregate pressure, or sampling-plan claims unless you computed the equivalent fallback data.

### 2. Prompt Size and Cacheability Audit

Use the index's largest-prompt, largest-output, token, cache, and pressure signals to choose prompts and outputs for deeper reading.

Use the index's **Role Distribution** table for per-role prompt/output averages, output-to-prompt ratios, and prompt size trend classification (stable, growing, shrinking). Use **Prompt Size Trends** for chronological size progressions on roles with ≥10 prompts or non-stable trends.

Separate expected fixed cost from avoidable bloat before raising findings:
- **Contract, guardrail, and tool instructions** are load-bearing stable context. Do not flag their volume solely because they are large. Raise an issue only when they are duplicated unnecessarily, rendered in a way that defeats provider prefix/block caching, or bury the current task badly enough to explain observed behavior.
- **Role/task prompts** are parametric by design. Do not flag low cacheability solely because task IDs, state, paths, timestamps, or acceptance criteria vary. Raise an issue only when avoidable dynamic bytes repeat across runs, prompt growth accumulates retry history without compression, or measured cache metrics show expected-stable prefixes are not being reused.
- **Provider/runtime transcript volume** is a logging/storage signal, not automatically an agent-quality or prompt-design problem. Treat rich JSON output size as evidence only after attributing the volume to avoidable agent/tool behavior, such as broad reads, huge command output, repeated diffs, or noisy failing tests.

Treat these as structural pressure signals:
- Prompt leaves insufficient room for expected tool output, code reads, and reasoning given that role's observed output-to-prompt ratio
- One role's rendered prompt is much larger than sibling roles because of avoidable dynamic or duplicated task-local material
- Output-to-prompt ratio >20x suggests tool-heavy exploration only when top items show avoidable broad reads, large diffs, repeated command output, or missing targeted file refs
- Output-to-prompt ratio <3x on a non-trivial task suggests the prompt may have crowded out work only after excluding stable load-bearing contract/context
- Prompt size trend classified as "growing" indicates accumulating state across iterations
- Repeated runs vary early in the prompt before expected-stable blocks, reducing prefix or block cache reuse

Classify prompt segments as:

| Segment | Cacheability question |
|---------|-----------------------|
| Stable prefix | Is this identical across runs so provider prompt caching can reuse it? |
| Semi-stable role context | Does it change only when role, contract, skill, or pipeline config changes? |
| Task-local context | Is it necessary for this run, or should it be referenced/deferred? |
| Volatile context | Does timestamped state, logs, or generated output appear earlier than needed? |

Flag high-leverage cacheability issues:
- Volatile state inserted before stable contract/role context
- Repeated boilerplate rendered differently across agents or restarts
- Large stable context repeated after task-local or timestamped material
- Prompt prefixes that differ only because of ordering, whitespace churn, or non-semantic metadata

Check salience order separately from cacheability. A stable prefix helps provider caching, but the agent must still find decision-relevant context quickly. Prefer rendered prompts where the usable task surface is easy to locate:
- Task and exact next action
- Acceptance criteria and validation plan
- Current state, prior failures, and pending decisions
- Constraints, guardrails, and role responsibilities
- Broader references and load-on-demand source pointers

Flag packing failures where stable but low-salience material buries the task, where broad references appear before the current decision surface, or where large artifacts are embedded when a precise source pointer would let the agent load them on demand.

Actively raise these prompt-shape issues when supported by evidence:
- duplicated task-local material within a prompt, across retries, or across handoffs where a compact summary would preserve the decision surface
- huge embedded artifacts where a file path, line range, artifact pointer, or bounded excerpt would let the agent load only what it needs
- poor salience ordering where the task, next action, acceptance criteria, prior failure, or validation plan is buried behind broad context
- prompt growth across retries or restarts without compression of prior attempts, decisions, rejected paths, and current hypothesis

### 3. Prompt Context Audit

For each sampled prompt, classify context into:

| Class | Question |
|-------|----------|
| Contract | Did the prompt include required mode, tool, and guardrail context? |
| Task | Is the concrete task unambiguous, bounded, and falsifiable? |
| Domain | Does the agent receive the specs, docs, skills, or state it needs? |
| Operational | Are commands, paths, worktree rules, validation requirements, and approval rules clear? |
| Noise | What content is duplicated, stale, irrelevant, or too broad for the role? |

Look for context-engineering failures:
- Underspecific prompts that force avoidable exploration instead of directing the agent to the right source refs, files, acceptance criteria, boundaries, or next action
- Missing source-of-truth refs, causing rediscovery or invention
- Overbroad contract/spec dumps that crowd out task-local details
- Role prompts that do not differ meaningfully across roles
- Conflicting instructions without priority or resolution
- Hidden assumptions embedded as facts
- Validation criteria that cannot falsify the intended outcome
- Tool instructions that are present but not operationally actionable

### 4. Output Behavior Audit

For each sampled output, trace behavior back to context:

- Did the agent follow the highest-priority relevant instruction?
- Did it ask for missing context, infer silently, or fabricate?
- Did it spend context on repeated initialization reads?
- Did it perform broad exploratory searches because the prompt lacked concrete refs, acceptance criteria, or boundaries?
- Did tool output dominate the transcript?
- Did the agent loop because the prompt lacked a decision boundary?
- Did review agents catch issues the original role could have prevented with better context?
- Do repeated reviewer rejections or blocked outcomes trace to missing, ambiguous, stale, or buried upstream context?

When outputs show failures, distinguish:
- **Prompt defect**: the needed instruction/context was absent, ambiguous, or buried.
- **Execution defect**: the prompt was adequate, but the agent ignored it.
- **System defect**: §BRAND_NAME_TITLE§ generated or routed the wrong context.
- **Evidence gap**: logs are insufficient to decide.

Use these heuristics:
- If the instruction was present, unambiguous, and salient, but the agent ignored it or contradicted it, lean **Execution defect**.
- If behavior is consistent with a plausible misread of buried, conflicting, stale, or underspecific context, lean **Prompt defect**.
- If the prompt should have contained a state/spec/task field but the rendered prompt omits or misroutes it, lean **System defect**.
- If the output alone shows bad behavior but no paired prompt was read, keep it **Evidence gap** or **Output-only**.
- If repeated agents fail the same way from similar prompts, prefer a prompt/system hypothesis over individual execution error.

### 5. Cross-Agent Context Flow

Compare prompt-output chains across roles:

- Architect -> reviewer: does the reviewer receive the architect's actual decision surface?
- Planner -> coder: are constraints converted into executable acceptance criteria?
- Coder -> reviewer: does the reviewer receive enough diff/test context to review behavior?
- Orchestrator -> role agents: are task boundaries, prior failures, and superseded work visible?
- Restart/session continuity: do prior failures, rejected paths, current hypothesis, pending validation, superseded work, and exact next action survive across handoff or agent restart?

Flag handoff compression failures where important nuance disappears, and handoff bloat where downstream agents receive full upstream artifacts when a structured digest would suffice.

**Adversarial pair overlap is not duplication.** §BRAND_NAME_TITLE§'s doer/reviewer pairs require both agents to receive the same context so the reviewer can independently verify the doer's work. High content overlap between paired roles (e.g., analyst + reviewer, coder + code-reviewer, writer + us-reviewer) is by design. Do not flag it as redundancy. The relevant questions for paired prompts are whether the shared context is too large, poorly ordered, or volatile — not whether it is duplicated.

A good compressed handoff preserves:
- Decisions made
- Rejected paths and why they were rejected
- Current hypothesis or implementation direction
- Validation evidence already gathered
- Exact next action
- Key files, specs, tasks, or state fields
- Confidence and source refs for non-obvious claims

### 6. Fix Localization

For each proposed fix, identify the smallest source artifact likely to implement it:
- Prompt template or block under `internal/prompts/templates/`
- Role or pipeline configuration
- Contract, guardrail, skill, spec, or docs source
- Blackboard or state field consumed by prompt generation
- Operational process outside prompt generation

**Existing-context check:** before recommending a change, verify whether the intended instruction or context already exists upstream but is not rendered, is rendered too late, or is buried by higher-volume context. This prevents recommending duplicate contract/spec/template text when the actual issue is rendering, ordering, routing, or salience.

Each finding's **Fix** must name:
- Source artifact to change
- Expected rendered prompt or output difference
- Validation method in a future `§BRAND_PROJECT_DIRNAME§/agent-prompts/` or `§BRAND_PROJECT_DIRNAME§/agent-outputs/` sample

Do not present a fix-localization finding until the relevant prompt/output pair and source template, config, contract, guardrail, skill, spec, state field, or operational process has been inspected.

### 7. Synthesis

Produce findings in this format:

```markdown
# Context Engineering Report

## Executive Summary

- Prompt corpus: [N prompts, size range, roles]
- Output corpus: [N outputs, size range, roles]
- Primary bottleneck: [one sentence]

When referring to a specific session in the Executive Summary or Findings, name
the concrete `§BRAND_PROJECT_DIRNAME§/agent-outputs/` log filename. If the claim depends on a
paired prompt, also name the concrete `§BRAND_PROJECT_DIRNAME§/agent-prompts/` filename.

## Findings

### P1/P2/P3: [Finding Title]

- **Evidence tier:** [output-only | prompt+output | prompt+output+source | state-supported]
- **Evidence:** [specific prompt/output files and concise observed fact]
- **Confidence:** [high/medium/low, based on prompt-output pairing quality and evidence completeness]
- **Context mechanism:** [why this context shape leads to the behavior]
- **Impact:** [cost, failure rate, review churn, blocked tasks, context pressure]
- **Fix:** [source artifact to change, expected rendered prompt/output difference]
- **Validation:** [future `§BRAND_PROJECT_DIRNAME§/agent-prompts/` or `§BRAND_PROJECT_DIRNAME§/agent-outputs/` sample that would prove the fix worked]

## Context Budget Opportunities

| Opportunity | Expected effect | Risk |
|-------------|-----------------|------|
| [dedupe/summarize/defer/load-on-demand] | [token or behavior impact] | [what could regress] |

## Non-Findings

Items checked that are acceptable. Include these to avoid repeated rediscovery.
```

Prioritize findings by observed effect, not theoretical prompt neatness.

## Work-Splitting Note

When subagent delegation is supported, consider delegating separable evidence-gathering slices:
- Per-role prompt-output audits
- Large-file sampling and evidence extraction
- Independent handoff-chain checks
- Validation of whether a proposed context fix would address the observed behavior

Recommend delegation only when slices are separable, evidence can be passed as raw artifacts, and the main agent can integrate findings without duplicating all reads.

## Guardrails

- Do not expose secrets. If prompt/output content appears sensitive, quote only redacted snippets.
- Do not claim a prompt caused an output unless the prompt-output pair was read.
- Do not rely on exact prompt/output filename stems for pairing; match by same role and nearest timestamp, then report the matching window.
- Do not claim fix localization unless the source artifact that generates or routes the relevant context was inspected.
- Do not recommend deleting contract or guardrail context solely because it is large; first decide whether it is cacheable, load-bearing, or duplicated.
- Do not raise prompt cacheability as a finding solely because task-local prompts are parametric. Identify avoidable dynamic content, bad stable/dynamic ordering, or cache metrics inconsistent with the intended stable prefix/block.
- Do not raise runtime output volume as a context-engineering finding solely because rich JSON transcripts are large. Attribute the volume to avoidable agent/tool behavior before calling it a prompt or context problem.
- Do not optimize only for token count. Context engineering optimizes task success per token, not minimum tokens.
- Prefer structured handoff summaries over broad truncation when downstream judgment depends on upstream decisions.
