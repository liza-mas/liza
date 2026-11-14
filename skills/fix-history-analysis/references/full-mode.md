# Full mode

A window of raw history across a product's repositories, classified by subagents into a digest, then reduced to weaknesses. Shared rules are in [SKILL.md](../SKILL.md).

## Gather

Per repository:
1. Run the Period query: fix commits and their record fields. A record, when present, is the starting point.
2. List reverts: `git log` subjects of type `revert` or git's `Revert "..."`.
3. Read each commit's full message and diff.

Placement: a commit reachable in two repositories with one SHA counts once. Two SHAs with equal `git patch-id --stable`, or a `(cherry picked from commit ...)` line, are one fix, attributed to the upstream-most repository.

## Classify

Split the fixes into batches. A read-only subagent classifies each batch per generic-subagent and returns the digest. A session that cannot delegate stops before classifying and reports full mode unavailable; it never substitutes an inline pass.

One digest entry per fix, with fields:
- repository, SHA, population;
- message versus diff: `consistent` or `misleading`, with the diff evidence;
- untested condition: one of upgrade, second instance, other platform or provider, inherited environment, existing user files, slow or failing dependency, or a derived one;
- structural cause: derived from the evidence, never from a preset list;
- lineage: `repairs <sha>` with its age, `shape seen at <sha>`, or none;
- evidence, confidence with its reason, verified mark.

Classify every batch twice, with two independent subagents given the same batch and no classification, neither seeing the other's digest. Compare the two digests field by field. For each divergence re-read the source (message, diff, blame) and report it *resolved*, with the evidence, or *unresolved*, with both readings. The report states the result as *reconciled* when no divergence is unresolved, else *reconciled, N unresolved*.

## Coverage and process causes

Coverage: check each cause against the invariant file and the guardrails file, in the record's classification vocabulary: gap, unenforced, not consulted, none needed.

Process causes, why fixes stayed at the instance:
- fixes repairing recent fixes;
- the same shape twice;
- triggers drawn too narrowly;
- concepts shared before being defined.

## Report sections

Census; Populations; Weaknesses (principle altitude, evidence and confidence per cause); Coverage; Process findings; Direction (young-code share); Draft goal (one per repository).

Each draft goal states that a past fix establishes a concern, not a current defect, and that an invariant already enforced is discharged with proof, not new code. A fix attributed upstream is drafted upstream, with how its change reaches there: a run upstream, or commits cherry-picked from the fork.
