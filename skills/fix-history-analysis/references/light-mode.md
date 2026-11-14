# Light mode

One period's prevention records, read against the project's invariants, guardrails and lessons: the recurring review's decision list. Shared rules are in [SKILL.md](../SKILL.md). The skill collects its inputs itself, with git and file reads only, and reads nothing else.

## Inputs

- The period's records: the Period query with the window.
- The invariant file (`INVARIANTS.md`) and the guardrails file (`GUARDRAILS.md`) at each repository root, read as found. An invariant's ID is the first backticked token of its Invariant cell. Trigger text is the `**Triggers:**` line of the invariant-compliance rule, or the rule's prose where the project wrote it so.
- The lessons, read as found: files the guardrails file indexes as lessons, else a `lessons/` directory at the root.
- Each listed fix's `git show` and blame on its parent (young-code share; gotcha causes).
- `git log` over the period for later commits whose message contains a *scheduled* record's owner reference.

## Report sections

In this order:

1. **Period and census**: window, `HEAD` SHA, Period query line count, records readable and unreadable. Unreadable: a listed fix whose record fields are empty, hold a value outside the record's closed sets, or hold two values in a single-valued field. List them by SHA and exclude them from every count below.
2. **Widened triggers**: clusters of two or more *not-consulted* records on one concept area (the same invariant ID, or code boundaries in the same parent directory) that no trigger text names. Give the proposed widening and the records.
3. **Gaps**: *gap* records grouped by concept, as candidate principle-level invariants or guardrails.
4. **Unenforced invariants**: rows whose Enforced cell says `unenforced`, and *unenforced* records, each with its concept.
5. **Open dispositions**: each *scheduled* record with its owner and any later commit citing the owner reference (still open, or with a later commit; owner `human` means awaiting the human). Each *accepted* record followed by a later fix on one of its concepts, as an acceptance a recurrence voids.
6. **Fixed-twice intervals**: per concept fixed two or more times, the days between consecutive fixes, those within 14 days marked: the data for recalibrating that window.
7. **Uncited invariants**: file IDs minus concepts cited in the period, for retain, merge or retire. Rows without an ID are counted apart as not computable, with the fix bodies that name them.
8. **Lesson candidates**: causes that are a gotcha rather than a shape, against existing lessons.
9. **Shares**: the *none-needed* share of readable records (a high share is a finding) and the young-code share.

With no invariant file, items 4 and 7 say *not computable*, never empty.

Every item cites its records. The skill proposes; the human decides.
