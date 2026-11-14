# Recorded run: fix-history-analysis, full mode

## Header

- Skill and mode: `fix-history-analysis`, full.
- Date (UTC): 2026-11-14.
- `git --version`: git version 2.43.0.
- Materializer: `go run ./internal/historyfixture/cmd/materialize <fixture>`; it printed `<fixture>/upstream` and `<fixture>/downstream`.
- Fixture heads: upstream 6b338d90454bd0999dcd78769036af9e5dcbed4f downstream e801e41f16fba9ec7b9c84acedd311e7164588fc
- Inputs: mode `full`; repositories `<fixture>/upstream`, then `<fixture>/downstream` labelled a fork of it; `SINCE='2026-01-01T00:00:00Z' UNTIL='2026-04-01T00:00:00Z'`.
- Classifier subagents (three read-only subagents launched per `generic-subagent`, `MODE: SUBAGENT`, one digest each; the session's delegation probe passed because its Agent tool was exposed):
  - batch A, subagent `a29f7349c363d98b6`: upstream 780d480f85010829a9fd57fff01e4b5311c22adb, 4cd9e3a8a0497f4ac38ffc6b024589f578938697, 2ba358c5f03cb539afb41781adfe10931979e56d, and downstream 7272f58ef2fff07b3e0695c4e2790f3644848177 (given as a candidate duplicate of 780d480, because its patch-id equals 780d480's).
  - batch B, subagent `a3787a5f500b7dac1`: upstream 9f1bb6bb7f67df18f97e5b5fd5c45da373c991ea, d7aaf9ca94bf8b8af8371fcfb10c5858183b6d4a, b062bd887b9a2d4ff3488a7ea82ee0ddd8348625.
  - batch C, subagent `a255190c5add2a3df`: downstream e801e41f16fba9ec7b9c84acedd311e7164588fc.

## Report

### Census

Two execution contexts. *Source checkout*: the repository holding the skill, where its record document is read. *Fixture repository*: `<fixture>/upstream` or `<fixture>/downstream`, where every history command runs. `SINCE='2026-01-01T00:00:00Z'` and `UNTIL='2026-04-01T00:00:00Z'` are exported in the fixture shell.

- `HEAD`, from `git rev-parse HEAD` in each fixture repository: upstream `6b338d90454bd0999dcd78769036af9e5dcbed4f`, downstream `e801e41f16fba9ec7b9c84acedd311e7164588fc`. Window: as above.
- Commands (output verified, read from the command):
  - C1, listed fix commits, in two steps. Step 1, in the source checkout, extract the Period query: `sed -n 82p skills/shared/references/prevention-record.md > <fixture>/period-query.sh` (line 82 is the one-line `sh` block under `### Period query`, line 79; the file holds that one line). Step 2, in each fixture repository: `sh ../period-query.sh | wc -l`. Output: upstream 6, downstream 2. The extracted file sits beside the two repositories, not in them.
  - C2, reverts, in each fixture repository: `git log --since="$SINCE" --until="$UNTIL" --format='%H %s' | awk '$2=="revert:" || $2 ~ /^revert(\(|!)/ || /^[0-9a-f]+ Revert "/'`. Output: upstream 1 line (`6b338d90454bd0999dcd78769036af9e5dcbed4f Revert "feat: add a cache key helper"`, reverting the feature `fb598476b79c86f08674e42cf02ca4793535f5e4`); downstream 0 lines.
  - C3, merges, in each fixture repository: `git rev-list --merges --count HEAD`. Output: upstream 1 (`44c5fe403824dcd234a414f33c55ae6ee19f2761`, `Merge branch 'topic'`), downstream 0.
  - C4, duplicates, in each fixture repository: `git show <sha> | git patch-id --stable` for the listed fixes, and `git log --format=%B | grep -c 'cherry picked from'`. Output: `780d480` (upstream) and `7272f58` (downstream) both give patch-id `cd592b9ad808e57e6bde1edcaf9c4317b856af65`; downstream's `e801e41` gives `d2167537a1c317a7721b3be0211376588c5ad4dd`; the `grep -c` count is 0 in both repositories, so no `(cherry picked from commit …)` line exists. `git merge-base --is-ancestor 780d480 HEAD` in downstream exits 1: the original is not in downstream's history.
  - C5, records: the output of `sh ../period-query.sh` in each repository, read field by field (hand count). Only `9f1bb6b` carries a record; the other 7 listed lines have empty record fields.
- Counted once: 8 listed lines, one duplicate pair (`780d480` with `7272f58`), so **7 fixes** (upstream 6, downstream's own 1) and **1 revert** (of a `feat`, not a fix: gathered, not classified).
- Not gathered: `dc1eb1b` (`docs`), the merge `44c5fe4`, the feature commits.
- Chronology, from `git log --format='%h %cI %aI'` in each fixture repository (committer dates read before any causal claim): `7272f58` has author date 2026-02-12 and committer date 2026-02-13; upstream's `780d480` is committed 2026-02-12, a day earlier, so the downstream copy follows its original.
- Placement: `780d480` and `7272f58` are one fix, attributed upstream (equal patch-ids; `780d480` is not in downstream's history; classifier A confirmed it from the diffs).
- Thin messages: `e801e41f16fba9ec7b9c84acedd311e7164588fc` (subject only). The diff, blame and history were read; no ticket is referenced, so none was read.

### Populations

Classified from the digests and C3; none dropped.
- Product fixes (5): `780d480` (one fix with `7272f58`), `4cd9e3a`, `9f1bb6b`, `d7aaf9c`, downstream `e801e41`.
- In-run corrections (1): `b062bd8`. It and the code it repairs (`ae06953`) entered `main` only through merge `44c5fe4`: neither is on `git rev-list --first-parent HEAD` (verified by count 0 in upstream), and `git show 44c5fe4:src/retry.py` already holds `return min(delay, cap)`.
- Test or proof fixes (1): `2ba358c` (its diff touches only `tests/test_duration.py`; verified by classifier A).

### Weaknesses

Principle altitude, one per cause, with evidence and confidence.
1. **A repair is judged by reading, never by a test that can fail.** The duration parser and its table test entered together in `1c670d8`, and the test could not fail on any case except `"90s"`: its assertion ignored the loop variable (`tests/test_duration.py:12`). Two source fixes landed on the same two lines before the test was repaired: `4cd9e3a` repairs `780d480` (chain `1c670d8` → `780d480` → `4cd9e3a`, a repair of a repair), and `2ba358c` repairs the test six days after it was written. Even after `2ba358c` the table has no padded, upper-case or empty case, so what `780d480` and `4cd9e3a` changed stays untested; `4cd9e3a` also changes the failure of an empty string from `IndexError` to `ValueError` (hand-traced by classifier A, not executed). Confidence HIGH on the lineage (blame of the pre-image on each first parent, verified); MEDIUM on the cause, which is derived.
2. **A convention lives in a comment, once per function.** `src/window_check.py` states "half-open window" in its header comment (line 1, `git show 9f1bb6b^:src/window_check.py`); `in_window` and `overlaps` encode it separately and no test touches the file. `9f1bb6b` made `in_window` exclusive at the end (pre-image blamed to `aeffdb0`, 55 days old); two days later `d7aaf9c` made `overlaps` inclusive at both ends. Confidence HIGH on the facts, MEDIUM on the cause (derived).
3. **A commit message describing less than its diff.** `d7aaf9c` says "Comment-only change: no behaviour differs" and is misleading: besides rewording a comment, its diff changes `return a_start < b_end and b_start < a_end` to `return a_start <= b_end and b_start <= a_end` in `overlaps`, a behaviour change (touching windows now overlap) that contradicts the half-open rule `9f1bb6b` applied two days before. A reader trusting the message passes a regression. Confidence HIGH on the mismatch (the diff is explicit); MEDIUM that it is a regression rather than an intended change, since the commit states no intent.
4. **A documented contract with no executable check.** `b062bd8` (`next_delay(attempt, base, cap)` ignored `cap`) and `e801e41` (a function documented to render a count concatenated it to a string) both repair a promise made by a signature or comment that nothing exercised; there is no test for `src/retry.py` or `src/report.py`. Confidence MEDIUM: the cause of `e801e41` is inferred from a one-line diff and an empty message, and the fix was not executed.
5. **A fork carries a fix without its repairs.** Downstream's `7272f58` is upstream's `780d480`, and downstream's `HEAD` still has the double-strip form in `src/duration.py` (`git show HEAD:src/duration.py`, lines 6 to 7) that `4cd9e3a` repaired upstream, and the vacuous assertion at `tests/test_duration.py:12`. Confidence HIGH (read by hand).
6. **A record's cause can disagree with its repository.** `9f1bb6b` records the cause "window bounds had no stated inclusive or exclusive rule", but the pre-image's first line already states a half-open rule; the rule was stated and not enforced. Confidence HIGH (classifier B, and read by hand).

### Coverage

Not computable: neither repository has an invariant file or a guardrails file (`git ls-files` in each lists only `src/`, `tests/` and, upstream, `docs/overview.md`), so no cause can be checked against them in the classification vocabulary (gap, unenforced, not consulted, none needed). The only classification present is `9f1bb6b`'s own record, *gap*, taken as stated and not verified against any file.

### Process findings

- Fixes repairing recent fixes (ages by committer dates): `4cd9e3a` repairs `780d480` (2 days); `780d480` repairs `1c670d8` (2 days); `2ba358c` repairs the test of `1c670d8` (6 days); `b062bd8` repairs `ae06953` (1 day, in-run); `e801e41` repairs downstream's `0373b96` (7 days).
- The same shape twice: boundary inclusivity in `src/window_check.py`, `9f1bb6b` (in `in_window`) then `d7aaf9c` (in `overlaps`), two days apart; the second declared comment-only.
- Concepts shared before being defined: the window convention (Weaknesses 2), written once as prose and re-implemented per function.
- Triggers drawn too narrowly: not computable (no guardrails file).
- Classification is *single, not reconciled*: each batch was classified once, so a divergence between two classifiers could not be measured.

### Direction (young-code share)

Method, per fix `<sha>`, in the fixture repository: `git show -M -U0 <sha>` for the hunks, then `git blame --porcelain <sha>^ -- <file>` for each hunk's pre-image lines; a fix is young when the newest blamed line's committer time is at most 30 × 86400 s before the fix's committer time. Each fix is counted once; the duplicate `7272f58` is not counted. Results, taken from the classifiers' blame output (ages from committer epochs):

| Fix | Population | Newest blamed line | Age | Young |
|---|---|---|---|---|
| `780d480` | product | `1c670d8` | 2.0 days | yes |
| `4cd9e3a` | product | `780d480` | 2.0 days | yes |
| `2ba358c` | proof | `1c670d8` | 6.0 days | yes |
| `9f1bb6b` | product | `aeffdb0` | 55.0 days | no |
| `d7aaf9c` | product | `aeffdb0` | 57.0 days | no |
| `b062bd8` | in-run | `ae06953` | 1.0 day | yes |
| `e801e41` | product | `0373b96` | 7.0 days | yes |

Share: **5/7 overall**; product 3/5, proof 1/1, in-run 1/1; not measurable 0.
Chronology: the two old fixes repair the code of `aeffdb0` (January); the five young ones repair code written one to seven days earlier.

### Draft goal

#### upstream

A past fix establishes a concern, not a current defect; an invariant already enforced is discharged with proof, not new code.
- Concern 1: a test that cannot fail lets a chain of repairs through (`1c670d8`, `780d480`, `4cd9e3a`, `2ba358c`). First check whether an enforcement already refuses an unused loop variable or an unread table in tests; if so, discharge it with proof. Otherwise decide the enforcement (a lint, or a mutation check on new tests).
- Concern 2: the window convention in `src/window_check.py` (`9f1bb6b`, `d7aaf9c`). Define it once, check that `in_window` and `overlaps` agree, and cover the boundary instant.
- Concern 3: a message that undersells its diff (`d7aaf9c`): whether review checks message against diff.
- `780d480` is attributed upstream and already lives there (the downstream commit is its copy), so its change needs no path to reach upstream.

#### downstream

A past fix establishes a concern, not a current defect; an invariant already enforced is discharged with proof, not new code.
- Concern 1: `e801e41` repairs the report line 7 days after it was written, and no test touches `src/report.py`. Its change exists only here, so its goal is drafted here and reached by a run in this repository.
- Concern 2: downstream carries `7272f58` (attributed upstream) without `4cd9e3a` and `2ba358c`; those reach downstream by cherry-picking them from upstream, with their tests.

## Evidence map

| Final-acceptance item | Report section | SHAs |
|---|---|---|
| Repair-of-a-repair chain | Weaknesses (1); Process findings | `1c670d8`, `780d480`, `4cd9e3a` |
| Cherry-pick duplicate counted once | Census (C4, Counted once, Placement) | `780d480` with `7272f58` |
| Young-code share by blame | Direction | young: `780d480`, `4cd9e3a`, `2ba358c`, `b062bd8`, `e801e41`; old: `9f1bb6b`, `d7aaf9c`; 5/7 |
| Misleading message caught by its diff | Weaknesses (3) | `d7aaf9c` |
| Delegated classification | Header | batches A, B, C |

## Deviations

- The skill was read from the source checkout's `skills/fix-history-analysis/` files (`SKILL.md`, `references/full-mode.md`), not through the session's skill mechanism: it is not installed in this session's skill list. The steps were followed as written.
- The Period query was run through an extracted file (`sed -n 82p … > <fixture>/period-query.sh`, then `sh ../period-query.sh`) instead of the skill's command substitution, because the session's shell rules forbid `$()`; the query text is unchanged.
- This is the third recording of this run. Each earlier recording was rejected in review (the revert filter given as prose; the Period query read by a path absent from the fixture) and none was patched into this one: this run repeats the whole method on a fresh materialization with three new classifier subagents, and its report is recorded as composed, with the temporary directory written `<fixture>`.
- The orchestrating session ran C1 to C4 and the chronology command itself, and read by hand the facts behind Weaknesses 5 and 6 and the in-run merge; classifier digests and blame results were otherwise taken as returned.
