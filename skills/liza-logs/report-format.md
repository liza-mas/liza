# §BRAND_NAME_TITLE§ Logs Report Format

Use this format for `/§BRAND_BINARY_NAME§-logs` findings and save the completed
report to `§BRAND_PROJECT_DIRNAME§/log-analysis.md`. Combine analyzer output from
`§BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt`, task friction evidence from `§BRAND_PROJECT_DIRNAME§/state.yaml`, and
raw log excerpts only when needed to validate a specific claim.

## 1. Summary

Date:
Scope:
Logs analyzed:
Supervisor logs analyzed:
State file analyzed:
Roles covered:
Changes made:

Start with the primary lifecycle friction. If any task has >=4 rejections or
review cycles, list the highest-churn task first even when it eventually merged.
Tool errors, token volume, and setup friction come after lifecycle churn unless
they are security/data-loss issues. Permission and policy friction is the
exception among setup friction: report it near the top because it changes how
all later tool-error counts should be interpreted.

| Priority | Friction | Evidence | Impact | Recommended fix |
|----------|----------|----------|--------|-----------------|

## 2. State Friction Inventory

Source: `§BRAND_PROJECT_DIRNAME§/state.yaml`

| Category | Count |
|----------|------:|
| Tasks with >=4 rejections | N |
| INTEGRATION_FAILED | N |
| BLOCKED | N |
| SUPERSEDED | N |
| ABANDONED | N |

### Primary Lifecycle Friction

Required when any high-rejection task exists. Reconcile analyzer/history counts
against current task fields instead of choosing the lower or more convenient
number.

| Task | Why this is primary | Analyzer/history count | Current task field | Final status | Evidence | Recommended fix |
|------|---------------------|-----------------------:|--------------------|--------------|----------|-----------------|

### High-Rejection Tasks

Include tasks where `review_cycles_total >= 4`. If `review_cycles_total` is
missing, count task `history` events named `rejected` or
`review_verdict_rejected`.

| Task | Status | Rejections | Attempt | Iteration | Assigned to | Last rejection summary | Evidence |
|------|--------|-----------:|--------:|----------:|-------------|------------------------|----------|

### Terminal / Stalled Tasks

#### INTEGRATION_FAILED

| Task | Parent/task link | Assigned to | Failure summary | Integration failure fields | Next action |
|------|------------------|-------------|-----------------|----------------------------|-------------|

#### BLOCKED

| Task | Blocked reason | Questions | Failed by | Related logs | Needed decision |
|------|----------------|-----------|-----------|--------------|-----------------|

#### SUPERSEDED

| Task | Rescope reason | Superseded by | Replacement status | Was this healthy? | Follow-up |
|------|----------------|---------------|--------------------|-------------------|-----------|

#### ABANDONED

| Task | Reason/history | Last status before abandon | Failed by | Impact | Follow-up |
|------|----------------|----------------------------|-----------|--------|-----------|

## 3. Per-Role Results

State whether token input includes cached input and whether error counts are
raw analyzer signals or classified actionable failures.

| Role | Logs | Input | Cache | Actions / errors | Struggles | Main finding |
|------|-----:|------:|------:|------------------:|----------:|--------------|

Use concrete representative log filenames for non-trivial role findings.

## 4. Permission & Policy Friction

Required when permission prompts, hook policy blocks, command-shape rejections,
filesystem allowlist blocks, or §BRAND_NAME_TITLE§ project-root mismatches appear.

| Category | Count | Example log | Example command/result | Likely fix surface |
|----------|------:|-------------|------------------------|--------------------|

Keep these categories distinct:
- policy blocks and command-shape rejections (`cd`, pipes, heredocs, shell expansions)
- missing allowlist entries or unsupported command forms
- filesystem allowlist blocks
- sleep/polling blocks
- §BRAND_NAME_TITLE§ CLI project-root mismatches

Do not mix permission/policy blocks with command exit failures such as failing
tests, validation errors, missing files, or lint failures.

## 5. Tool Usage

Aggregate shell commands by underlying executable: unwrap supported shell
launchers, ignore a leading `rtk`, discard executable path prefixes, and ignore
all arguments. For example, `rtk git status` and `git diff` both count as
`git`. State which non-command actions were excluded; do not equate Bash
invocation counts with broader analyzer action totals.

| Generic command | Calls | Share |
|-----------------|------:|------:|

| Role | Bash calls | Top normalized commands |
|------|-----------:|-------------------------|

### Reading Classification

| Target class | Calls | Share | Result volume |
|--------------|------:|------:|--------------:|

Separate legitimate initialization and targeted evidence gathering from
duplicated reads, oversized whole-artifact reads, and repeated result payloads.
Reading volume alone is not a finding.

### Tool Result Breakdown

Use the analyzer's `TOOL RESULT BREAKDOWN` section to identify tools that return
large or repetitive payloads.

| Log file | Agent | Tool | Calls | Total result | Avg result | Max result | Interpretation |
|----------|-------|------|------:|-------------:|-----------:|-----------:|----------------|

Interpret high-volume tool output as friction only when it is unnecessary,
duplicated, or prevents progress. Large output from a targeted diagnostic may be
valid evidence rather than waste.

## 6. Errors and Log Friction

| Log file | Agent | Signal | Evidence | Related task | Interpretation |
|----------|-------|--------|----------|--------------|----------------|

Signals include struggle sequences, repeated tool errors, duplicate large
results, low-value chatter, empty turns, large result volume, MCP failures,
missing expected skill invocation, or missing initialization breadcrumbs.

### Operational Friction

Keep operational friction separate from errors and permission/policy blocks.
It records provider-forced foreground timeout/backgrounding even when
`is_error` is false; command syntax requesting deliberate background execution
is not sufficient evidence.

Use `query-log.py --around-operational-friction N` for a bounded evidence
window. Add `--task`, `--max-field`, or `--json` when the question needs those
refinements. `--summary-by-role` reports operational friction by category and
role with example source logs.

| Category | Role | Example log | Tool | Command | Duration | Bounded result evidence |
|----------|------|-------------|------|---------|----------|-------------------------|

### Errors

Summarize analyzer-reported errors before interpreting tool behavior.

| Log file | Agent | Error count | Tool/command | First failing action | Error summary | Related task |
|----------|-------|------------:|--------------|----------------------|---------------|--------------|

Use raw logs to verify repeated errors before proposing fixes. Do not count
benign no-match `rg`/`grep`/`diff` exit code 1 as friction unless the raw log
shows the agent treated it as a failure.

### Significant Error Analysis

Classify actionable failures separately from benign or expected diagnostics,
including useful red tests, assertion gates, state discovery, missing-path
probes, and diff probes. State that significant events are not necessarily
distinct product defects.

| Classification | Errors | Share | Included events |
|----------------|-------:|------:|-----------------|

| Significant class | Errors | Share of all errors | Interpretation |
|-------------------|-------:|--------------------:|----------------|

### Role-Level Error Patterns

| Role | Errors | Dominant patterns | Representative log | Interpretation |
|------|-------:|-------------------|--------------------|----------------|

## 7. Struggle Sequences

A struggle sequence is a retry-cluster signal, not proof of poor reasoning.
Distinguish expected TDD or assertion-driven correction from repeated actions
that cannot progress without a state, capability, or environment change.

| Role | Sequences | Recurring sequence | Assessment |
|------|----------:|--------------------|------------|

## 8. Supervisor Evidence

Keep supervisor lifecycle failures separate from provider-tool errors. Use
bounded supervisor log evidence and quantify repeated identical failures by
count and duration when possible.

| Supervisor/role | Evidence file | Pattern | Frequency/duration | Interpretation |
|-----------------|---------------|---------|--------------------|----------------|

## 9. Usage, Content, and Initialization

### Usage and Content Authority

| Usage Source | Authority | Reporting rule |
|--------------|-----------|----------------|
| `terminal` | Authoritative aggregate fresh input, cache-create, cache-read, and output usage | Keep per-turn rows envelope-derived; do not reconstruct exact per-turn values from terminal totals |
| `envelope-partial` | Partial aggregate evidence when terminal usage is absent | Do not infer terminal-only values or present the aggregate as complete |
| `unknown` | No usable aggregate usage record; displayed zeros may be an undercount rather than measured zero | Treat aggregate token usage as unavailable and do not infer missing values |

Rich per-turn tables identify `Turn Usage Source` as assistant message envelopes
and state whether those rows reconcile with the aggregate. When they diverge,
use `TOKEN SUMMARY` for authoritative aggregate totals rather than summing rows.

For content accounting, count each string- or list-encoded `tool_result`
payload exactly once. Preserve ordinary user `text` as text; it is not a tool
result. In a `--summary-by-role` report, include `Usage Sources` and the
per-role `Partial` count. `Partial` counts only `envelope-partial` logs;
`unknown` logs remain visible in `Usage Sources` and must not be read as
complete merely because they are excluded from `Partial`.

### Aggregate Usage and Provenance

| Metric | Value | Authority / notes |
|--------|------:|-------------------|
| Fresh input | N | |
| Cache-create input | N | |
| Cache-read input | N | |
| Output | N | |
| Usage provenance | | terminal / envelope-partial / unknown counts |
| Transcript formats | | rich / sparse / unsupported counts |

### Content and Size Outliers

| Role/log | Content or tool | Size | Repeated? | Interpretation |
|----------|-----------------|-----:|-----------|----------------|

Attribute large provider transcript or tool-result volume to a concrete
behavior before calling it waste. Include top items by size and distinguish
routine cached context from avoidable repeated payloads.

### MCP, Skills, and Initialization

| Signal | Count / coverage | Evidence | Interpretation |
|--------|------------------|----------|----------------|

Report MCP usage, skill-invocation evidence, and telemetry gaps. Do not treat
zero recorded skill invocations as proof that skills were skipped when
transcript evidence shows explicit skill reads.

### Empty Turns and Breadcrumb Applicability

An empty turn has neither meaningful text nor tool activity; meaningful
tool-free text is not empty. Use the analyzer's corrected empty-turn count and
percentage rather than reclassifying turns from tool usage alone.

In `SECRET WORDS`, record breadcrumb applicability as `required`,
`not required`, or `unknown` according to agent/provider evidence. Native Claude
rich streams omit provider identity and are exempt; an explicit `claude` provider
is also exempt. Other identified providers and sparse logs require breadcrumbs,
regardless of model branding. Unsupported or undetected formats remain unknown.
Missing breadcrumbs are a finding only when required.
Keep unknown neutral rather than fabricating applicability.

Search only the first five non-empty assistant text blocks in rich logs or
agent-message text items in sparse logs, stopping earlier when breadcrumbs are
found. Tool-only and empty envelopes do not consume this initialization window;
text after the fifth block is outside the breadcrumb search.

### Rich-Log Detail

When rich logs provide the evidence, include per-turn context growth, the
longest turns, a bounded turn timeline, and cost breakdown with system-prompt
replay cost. Omit unavailable detail for sparse logs rather than inferring it.

## 10. Cross-Correlation

| Friction ID | Task | State evidence | Log evidence | Likely cause | Confidence |
|-------------|------|----------------|--------------|--------------|------------|

Confidence:
- High: state and logs directly agree
- Medium: state shows symptom, logs show plausible cause
- Low: state shows symptom but log evidence is incomplete

## 11. Recommendations

Group recommendations by root cause, not by individual task.

| Priority | Recommendation | Fixes frictions | Evidence | Owner | Validation |
|----------|----------------|-----------------|----------|-------|------------|

## 12. Non-Findings / False Positives

| Signal | Why not a finding | Evidence |
|--------|-------------------|----------|

## 13. Appendix

### Files Analyzed

| File | Purpose |
|------|---------|

### Commands Run

```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-log.py --summary-by-role §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/analyze-state.py §BRAND_PROJECT_DIRNAME§/state.yaml
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_BINARY_NAME§-logs/scripts/query-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt --around-operational-friction 3
```

### Raw Evidence Pointers

| Claim | Source |
|-------|--------|
