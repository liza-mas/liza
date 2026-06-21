---
name: §BRAND_NAME_LOWER§-logs
description: Analyze §BRAND_NAME_TITLE§ agents logs
---

SCOPE:
The logs in `§BRAND_PROJECT_DIRNAME§/agent-outputs/` and task state in `§BRAND_PROJECT_DIRNAME§/state.yaml`
(nowhere else unless told otherwise explicitly).
The prompt may filter more specifically, e.g. a specific role, task, status,
or time range.

OBJECTIVE:
Find recurring task, review, integration, tool, context, and setup frictions;
correlate state symptoms with log evidence; propose fixes.

PROTOCOL:
1. Start by running the analyzer:
```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/coder-*.txt        # all coder agents
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/coder-1-*.txt # single agent
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-log.py --summary-by-role §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
```
By default, run the analyzer **per role**.
Use `--summary-by-role` when you need cross-role aggregate token, tool, MCP,
error, and skill-invocation totals.

2. Inspect `§BRAND_PROJECT_DIRNAME§/state.yaml` for task-level frictions before drawing conclusions:
```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-state.py §BRAND_PROJECT_DIRNAME§/state.yaml
```
   - tasks with `review_cycles_total >= 4`
   - tasks whose status is `INTEGRATION_FAILED`, `BLOCKED`, `SUPERSEDED`, or `ABANDONED`
   - if `review_cycles_total` is missing, count task `history` events named
     `rejected` or `review_verdict_rejected`

Lifecycle churn outranks aggregate log noise:
   - Treat any task with `review_cycles_total >= 4` or counted rejection events
     >= 4 as a P1 finding by default, even if its current status is `MERGED`.
   - Do not let high tool-error counts, token volume, or eventual merge status
     bury repeated review/retry cycles. A merged high-churn task is unhealthy
     convergence unless the evidence proves the retries were expected.
   - If analyzer counts and current task fields disagree, report both numbers
     and explain the likely distinction (for example, history/attempt total vs
     current `review_cycles` field). Prioritize using the higher history count
     until disproven by bounded evidence.
   - The highest-churn task must appear first in the summary table and in
     cross-correlation before setup/tool/context frictions.

Report sections: session header, permission/policy friction, token summary,
content breakdown, top items by size, tool usage, empty turns, skill invocations,
secret-word/init breadcrumb detection, turn timeline, tool result breakdown, MCP
usage, efficiency insights, and struggle sequences. Rich format adds per-turn
context growth, top longest turns, cost breakdown with system-prompt replay cost,
and MCP server status. Sparse logs have aggregate usage only; do not infer exact
per-turn growth or cost.

Permission/policy friction is operational setup friction, not ordinary task
failure. Keep it near the top and separate it from command exit failures. Split
policy blocks, missing allowlist entries, shell-shape rejections, filesystem
allowlist blocks, sleep/polling blocks, and §BRAND_NAME_TITLE§ project-root mismatches because
their fix surfaces differ.

3. Refine the analysis with the bounded query helper, not by manually reading
   raw log files. Use `query-log.py` to extract trimmed evidence windows for
   specific questions, for example:
```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/query-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/coder-3-*.txt --around-errors 3 --task architecture-4-code-planning-0-b-repair-0-coding-1
```
   - Manual raw-log reads are a last resort only when the query helper cannot
     answer a concrete evidence question; state the gap before doing so.
   - When referring to a specific session in your summary, include the log filename
     (for example `coder-1-20260417-171454.txt`) so the reader can trace the claim
     back to the exact source log quickly.

4. Before proposing a fix, check whether the fix is already implemented (e.g. an instruction already exists but agents ignore it):
   - Read one agent prompt of the relevant role in `§BRAND_PROJECT_DIRNAME§/agent-prompts/`
   - Check the contract files in `~/§BRAND_GLOBAL_DIRNAME§/` (CORE.md, AGENT_TOOLS.md, MULTI_AGENT_MODE.md)

5. Write the final report using `skills/§BRAND_NAME_LOWER§-logs/report-format.md`.

6. Propose fixes whenever possible.

FALSE POSITIVES:
- **Repeated contract reads** (~8KB per session): Agents read AGENT_TOOLS.md, GUARDRAILS.md, etc. during initialization. These are usually cache hits — negligible cost. Do not flag as waste unless the same payload is reread for no reason later in the session.
- **Rich JSON transcript volume**: Provider CLIs may emit full JSONL session transcripts containing runtime envelopes, tool calls, tool results, usage metadata, rate-limit events, and command output. Large `§BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt` files are not automatically agent reasoning bloat. Attribute volume to avoidable behavior before raising it: broad file reads, repeated large diffs, noisy failing tests, unbounded command output, or repeated tool loops.
- **Contract/prompt volume**: Contract and guardrail files are stable, load-bearing context and are often cacheable. Do not report their size as waste by itself. Parametric role/task prompts are expected to vary and may not cache well; flag only avoidable duplication, poor ordering that defeats stable-prefix reuse, prompt growth across retries, or dynamic artifacts that should have been referenced instead of embedded.

NOTE:
The skill contains a web tool for humans to inspect logs: ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/tools/§BRAND_BINARY_NAME§-session-analyzer.html
