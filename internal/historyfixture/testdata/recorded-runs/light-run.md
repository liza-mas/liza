# Recorded run: fix-history-analysis, light mode

## Header

- Skill and mode: `fix-history-analysis`, light.
- Date (UTC): 2026-11-14.
- `git --version`: git version 2.43.0.
- Materializer: `go run ./internal/preventionrecord/recordfixture/cmd/materialize <fixture>`, then the light overlay copied untracked into `<fixture>` as `INVARIANTS.md`, `GUARDRAILS.md` and `lessons/`.
- Fixture heads: i3 6981796d5625e5b0c513deba1f2b0a44618f9655
- Inputs: mode `light`; repository `<fixture>` (the only one); `SINCE='2026-03-01T00:00:00Z' UNTIL='2026-03-30T00:00:00Z'`.

## Report

Case ids below (`c02` …) are the fixture's `case/<id>` tags; each is followed by the abbreviated commit SHA it names.

### Period and census

- Window: `SINCE='2026-03-01T00:00:00Z' UNTIL='2026-03-30T00:00:00Z'`; `HEAD` of `<fixture>`: `6981796d5625e5b0c513deba1f2b0a44618f9655` (command: `git -C <fixture> rev-parse HEAD`).
- Commands whose output gave each count, run in `<fixture>` with `SINCE` and `UNTIL` exported:
  - C1, listed fixes: the line under `### Period query` of `skills/shared/references/prevention-record.md` (line 82), extracted with `sed -n 82p <that file>` and run as written with `sh`, its output piped to `wc -l`. Output: 15 lines (verified by the command).
  - C2, merges in the window: `git log --merges --since="$SINCE" --until="$UNTIL" --format=%h | wc -l`. Output: 0 (verified).
  - C3, files each readable fix touches: `git show --format= --name-only <sha>` for each readable SHA. Output: one file per fix, `cases/<id>.txt`, twelve distinct files (verified).
  - C4, pre-image lines per readable fix: `git show -M -U0 --format= <sha>`, searched for a `new file mode` header. Output: present for all 12 (verified).
  - C5, none-needed lines: C1's output piped to `awk -F'\t' '$6=="none-needed"' | wc -l`. Output: 1 (verified).
- Records readable: 12, unreadable: 3. These two counts are hand counts (each of C1's 15 lines read against the closed sets; no command separates them). Readable: c02 (7165d25), c03 (9e50f6b), c04 (27210ce), c05 (90f05d1), c06 (00b1cef), c07 (e47548f), c08 (81f1e48), c12 (0491063), c13 (7a3b6da), c14 (d0d8772), c17 (289ad6c), c18 (6981796).
- Populations of the 12 readable fixes (classified by hand from C2 and C3; none dropped):
  - Product fixes: 12.
  - In-run corrections: 0. The history has no merge (C2 = 0), so no fix and the code it repairs can have entered through one merge.
  - Test or proof fixes: 0. Every diff touches one sample file `cases/<id>.txt` (C3), none of them a test or proof fixture.
- Unreadable (hand count, see above), excluded from every count below:
  - c09 (0d41075): all record fields empty.
  - c10 (6d5ab53): disposition `deferred`, outside the closed set.
  - c11 (b784fe4): classification empty.
- Noted, not excluded (the unreadable rule tests only empty, out-of-set and multi-valued fields): c12 (0491063) carries owner `human` on a `removed` record; the final-block-only query line of c13 (7a3b6da) hides a second `Prevention-` block in an earlier paragraph (`git show -s 7a3b6da`). Both would be refused by the submit gate; they are the human's to judge.

### Widened triggers

- Trigger text read: `GUARDRAILS.md` `**Triggers:**` line names `internal/api/` only.
- Cluster: two *not-consulted* records on one parent directory, `internal/cache/`, which no trigger text names: c04 (27210ce, `internal/cache/store.go#Store.Put`, cause "no owner limited what the cache stored") and c05 (90f05d1, `internal/cache/evict.go#Evict`, cause "eviction trusted the caller's clock").
- Proposed widening: add `internal/cache/` to the invariant-compliance rule's triggers. No other *not-consulted* cluster exists in the period (verified: only c04 and c05 carry that class among readable records).

### Gaps

*Gap* records grouped by concept (candidates for a principle-level invariant or guardrail):
- `T0.2`: c02 (7165d25, "a closed store accepted writes"), c07 (e47548f, "the handler trusted a decoded identifier"). `T0.2` already has a row; the gap is its enforcement breadth, for the human to judge.
- `internal/queue/drain.go#Drain`: c13 (7a3b6da, "the drain loop never returned"), c18 (6981796, "the drain moved and its callers kept the old path"; also cites `internal/work/drain.go#Drain`, the bridge).
- `internal/api/` (two tokens, not equal byte for byte): c07 (`internal/api/[id].go#Handle`), c12 (0491063, `internal/api/[id].go`, "errors leaked internal detail").
- `S1.1`: c08 (81f1e48, "retries were unbounded by default"), accepted.

### Unenforced invariants

- Invariant file rows whose Enforced cell says `unenforced`: `T0.21` (every key is checked at the boundary that receives it).
- *Unenforced* records: c03 (9e50f6b, `T0.21`), c14 (d0d8772, `internal/queue/drain.go#Drain` and `T0.21`), c17 (289ad6c, `S1.1`).
- Conflict to resolve: the `S1.1` row says enforced while c17 classifies its concept *unenforced*; the file or the record is wrong.

### Open dispositions

Command: `git log --since="$SINCE" --until="$UNTIL" --format='%h %s%n%b'` searched for each owner reference (verified: `TASK-42` and `TASK-43` occur only in their own records).
- c03 (9e50f6b, `T0.21`): scheduled, owner `human`: awaiting the human.
- c04 (27210ce, `internal/cache/store.go#Store.Put`): scheduled, owner `TASK-42`: still open, no later commit cites `TASK-42`.
- c14 (d0d8772, `internal/queue/drain.go#Drain`, `T0.21`): scheduled, owner `TASK-43`: still open, no later commit cites `TASK-43`.
- Accepted: c08 (81f1e48, `S1.1`) is followed by a later fix on `S1.1`, c17 (289ad6c, 2026-03-28): the acceptance is voided by the recurrence.
- The human decides each: done with proof, still scheduled, or accepted.

### Fixed-twice intervals

Committer dates from the Period query; days between consecutive fixes on one concept (byte-equal tokens, readable records only):
- `T0.2`: c02 (7165d25, 2026-03-02) → c07 (e47548f, 2026-03-07): 5 days, within 14 (marked).
- `T0.21`: c03 (9e50f6b, 2026-03-03) → c14 (d0d8772, 2026-03-14): 11 days, within 14 (marked).
- `internal/queue/drain.go#Drain`: c13 (7a3b6da, 2026-03-13) → c14 (d0d8772, 2026-03-14): 1 day, within 14 (marked); c14 → c18 (6981796, 2026-03-29): 15 days.
- `S1.1`: c08 (81f1e48, 2026-03-08) → c17 (289ad6c, 2026-03-28): 20 days.
- Four concepts, three of them with an interval within 14 days: the data for recalibrating that window.

### Uncited invariants

- File IDs: `T0.2`, `T0.21`, `S1.1`, `R1.1`. Concepts cited by readable records in the period: `T0.2`, `T0.21`, `S1.1`.
- Uncited: `R1.1` (every report names the commit it describes): retain, merge or retire is the human's decision.
- Rows without an ID: 1 ("Configuration is read from one place"), not computable. No fix body in the period names it (searched for "configuration", "read from one place" and `R1.1` in the period's message bodies, none found).

### Lesson candidates

Judged by hand from each record's cause line (not verified beyond that):
- Gotcha, not covered by `lessons/cache-entry-limits.md`: c06 (00b1cef, "the path was resolved twice", none-needed); c05 (90f05d1, "eviction trusted the caller's clock").
- Covered: c04 (27210ce, "no owner limited what the cache stored") by `lessons/cache-entry-limits.md`; it is a shape (a missing owner), not a gotcha.

### Shares

- *None-needed* share of readable records: 1/12 (c06, 00b1cef); not a high share.
- Young-code share as `young/measurable` with the not-measurable count (command C4; no `git blame --porcelain` is possible, since no fix has a pre-image line):
  - overall: 0/0, 12 not measurable;
  - product fixes: 0/0, 12 not measurable;
  - in-run corrections: 0/0, 0 not measurable (empty population);
  - test or proof fixes: 0/0, 0 not measurable (empty population).
  Every one of the 12 readable fixes adds a new file (C4: `new file mode`), so none has a blameable line; all are excluded from both terms. The share is therefore undefined, not zero.

## Evidence map

| Acceptance item | Report section | SHAs |
|---|---|---|
| A widened trigger | Widened triggers | c04 27210ce, c05 90f05d1 |
| A concept fixed twice | Fixed-twice intervals | c02 7165d25, c07 e47548f (`T0.2`, 5 days) |
| An open scheduled disposition | Open dispositions | c03 9e50f6b, c04 27210ce, c14 d0d8772 |
| The rest of FR6's list | Gaps, Unenforced invariants, Uncited invariants, Lesson candidates, Shares | as cited in each section |

## Deviations

- The skill was not offered by this session's skill mechanism. The run followed `skills/fix-history-analysis/SKILL.md` and `references/light-mode.md`, read from the task worktree, step by step; no subagent or other tool read the repository.
- The Period query was extracted from the record document's line 82 and run with `sh` in `<fixture>` with `SINCE` and `UNTIL` set; the text is byte for byte the document's.
- This file records the second run. The first run's report omitted the per-population counts, the per-population young-code shares and the exact census commands that `SKILL.md` requires; the reviewer rejected it. It was not edited: the run was repeated, applying those rules and producing those outputs, over the same materialized repository (same `HEAD`), and this report replaces it.
- Absolute temporary paths in the commands above are shown as `<fixture>`.
