# §BRAND_NAME_TITLE§ Logs Report Format

Use this format for `/§BRAND_NAME_LOWER§-logs` findings. Combine analyzer output from
`§BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt`, task friction evidence from `§BRAND_PROJECT_DIRNAME§/state.yaml`, and
raw log excerpts only when needed to validate a specific claim.

## 1. Summary

Date:
Scope:
Logs analyzed:
State file analyzed:
Roles covered:

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

## 3. Permission & Policy Friction

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

## 4. Log Friction Inventory

| Log file | Agent | Signal | Evidence | Related task | Interpretation |
|----------|-------|--------|----------|--------------|----------------|

Signals include struggle sequences, repeated tool errors, duplicate large
results, low-value chatter, empty turns, large result volume, MCP failures,
missing expected skill invocation, or missing initialization breadcrumbs.

### Errors

Summarize analyzer-reported errors before interpreting tool behavior.

| Log file | Agent | Error count | Tool/command | First failing action | Error summary | Related task |
|----------|-------|------------:|--------------|----------------------|---------------|--------------|

Use raw logs to verify repeated errors before proposing fixes. Do not count
benign no-match `rg`/`grep`/`diff` exit code 1 as friction unless the raw log
shows the agent treated it as a failure.

### Tool Result Breakdown

Use the analyzer's `TOOL RESULT BREAKDOWN` section to identify tools that return
large or repetitive payloads.

| Log file | Agent | Tool | Calls | Total result | Avg result | Max result | Interpretation |
|----------|-------|------|------:|-------------:|-----------:|-----------:|----------------|

Interpret high-volume tool output as friction only when it is unnecessary,
duplicated, or prevents progress. Large output from a targeted diagnostic may be
valid evidence rather than waste.

## 5. Cross-Correlation

| Friction ID | Task | State evidence | Log evidence | Likely cause | Confidence |
|-------------|------|----------------|--------------|--------------|------------|

Confidence:
- High: state and logs directly agree
- Medium: state shows symptom, logs show plausible cause
- Low: state shows symptom but log evidence is incomplete

## 6. Recommendations

Group recommendations by root cause, not by individual task.

| Priority | Recommendation | Fixes frictions | Evidence | Owner | Validation |
|----------|----------------|-----------------|----------|-------|------------|

## 7. Non-Findings / False Positives

| Signal | Why not a finding | Evidence |
|--------|-------------------|----------|

## 8. Appendix

### Files Analyzed

| File | Purpose |
|------|---------|

### Commands Run

```bash
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-log.py §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-log.py --summary-by-role §BRAND_PROJECT_DIRNAME§/agent-outputs/*.txt
python3 ~/§BRAND_GLOBAL_DIRNAME§/skills/§BRAND_NAME_LOWER§-logs/scripts/analyze-state.py §BRAND_PROJECT_DIRNAME§/state.yaml
```

### Raw Evidence Pointers

| Claim | Source |
|-------|--------|
