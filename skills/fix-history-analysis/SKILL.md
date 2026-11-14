---
name: fix-history-analysis
description: Read-only analysis of fix history. Full mode classifies a window of fixes across repositories into structural weaknesses; light mode turns one period's prevention records into the recurring-review decision list. Use for a history-based weakness review or a retrospective's record review.
---

Analyzes fixes already in history and returns a report. It proposes; the human decides.

## Invocation

- **mode**: `light` or `full`.
- **repositories**: one or more local paths, ordered upstream first, each optionally labelled as a fork of an earlier one. Default: the current repository alone.
- **window**: `SINCE` and `UNTIL` git dates, both required, passed to the record query unchanged.

Load [light mode](references/light-mode.md) or [full mode](references/full-mode.md), not both. Both run the Period query of the [prevention record](../shared/references/prevention-record.md) as written there. Full mode delegates classification per [generic-subagent](../generic-subagent/SKILL.md).

## Rules (both modes)

**Read-only.** Collect with `git` read commands, a POSIX `sh`, `awk` and file reads. Write no file, commit or goal: drafts and proposals go in the returned report. Light mode never leaves its listed inputs; full mode may also read a linked ticket for a thin message.

**Evidence.** Every item cites SHAs and the command or `file:line` it came from. Mark each claim verified or not, and counts done by hand as such. Check chronology (committer dates) before any causal claim.

**Census.** The report opens with each repository's `HEAD` SHA, the window, and the exact commands whose output gave each count, so the counts reproduce.

**Populations.** Count and classify separately, dropping none:
- product fixes;
- in-run corrections: the fix and the code it repairs entered the main line through the same merge, detectable only where history has that merge;
- test or proof fixes: the diff touches only tests or proof fixtures.

**Young-code share.** For each fix F counted once, take the pre-image lines of each `git show -M -U0` hunk (`-a,b`: lines a..a+b-1; `-a,0` with a >= 1: line a, which the insertion follows) and blame them with `git blame --porcelain` on F's first parent. F repairs young code when its newest blamed line's committer time is at most 30 days (30 x 86400 s) before F's. A fix with no blameable line (new files only, insertion at line 0) is *not measurable*: count it apart and exclude it from both terms. Report `young/measurable` overall and per population, with the not-measurable count. The newest repaired line is the strongest sign of the change that broke it; older context in the same hunk would dilute it.

**Thin messages** lower confidence and send you to the diff. Full mode may also read a linked ticket. Say which was read, or that a ticket was not readable.

**Report.** Each section is a Markdown heading carrying the section's name, in the order the mode lists. Every item cites its evidence.
