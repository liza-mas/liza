# Recorded run: fix-history-analysis, full mode, reconciled

## Header

- Skill and mode: `fix-history-analysis`, full, with the second independent classification.
- Date (UTC): 2026-11-14.
- `git --version`: git version 2.43.0.
- Materializer: `go run ./internal/historyfixture/cmd/materialize <fixture>`; it printed `<fixture>/upstream` and `<fixture>/downstream`.
- Fixture heads: upstream 6b338d90454bd0999dcd78769036af9e5dcbed4f downstream e801e41f16fba9ec7b9c84acedd311e7164588fc
- Inputs: mode `full`; repositories `<fixture>/upstream`, then `<fixture>/downstream` labelled a fork of it; `SINCE='2026-01-01T00:00:00Z' UNTIL='2026-04-01T00:00:00Z'`.
- Delegation probe (first step): the session's Agent tool was exposed, so the probe passed and no human action was needed.
- Classifier subagents: six read-only subagents launched in one step per `generic-subagent` (`MODE: SUBAGENT`), two per batch. Within a batch both received the identical brief (same batch, same digest fields, no classification and no hint of a duplicate); no subagent saw another's digest or brief; each returned one digest.
  - batch A (upstream 780d480f85010829a9fd57fff01e4b5311c22adb, 4cd9e3a8a0497f4ac38ffc6b024589f578938697, 2ba358c5f03cb539afb41781adfe10931979e56d): classifier A1 `a49f2a0c28f9554a1`, classifier A2 `a0b0c17369eab7f70`.
  - batch B (upstream 9f1bb6bb7f67df18f97e5b5fd5c45da373c991ea, d7aaf9ca94bf8b8af8371fcfb10c5858183b6d4a, b062bd887b9a2d4ff3488a7ea82ee0ddd8348625): classifier B1 `af872c00d55cc0da3`, classifier B2 `a30f125cfd0f92694`.
  - batch C (downstream e801e41f16fba9ec7b9c84acedd311e7164588fc): classifier C1 `a1f5124fac47f2e62`, classifier C2 `a347a45c9dbc12d3b`.
  - The downstream duplicate 7272f58 was placed by the orchestrator before batching (Census C4) and was in no batch.

## Report

### Census

Source checkout: the repository holding the skill, where its record document is read. Fixture repository: `<fixture>/upstream` or `<fixture>/downstream`, where every history command runs, with `SINCE` and `UNTIL` set as in the inputs.

- `HEAD`, from `git rev-parse HEAD` in each fixture repository: upstream `6b338d90454bd0999dcd78769036af9e5dcbed4f`, downstream `e801e41f16fba9ec7b9c84acedd311e7164588fc`. Window: as above.
- Commands (output verified, read from the command):
  - C1, listed fix commits. In the source checkout, `sed -n 82p skills/shared/references/prevention-record.md > <fixture>/period-query.sh` (line 82 is the one-line `sh` block under `### Period query`); in each fixture repository, `sh ../period-query.sh`. Output: upstream 6 lines, downstream 2.
  - C2, reverts, in each fixture repository: `git log --format='%h %s' | grep -i '^[0-9a-f]* revert'`. Output: upstream 1 line (`6b338d9 Revert "feat: add a cache key helper"`), downstream 0.
  - C3, merges, in each fixture repository: `git rev-list --merges --count HEAD`. Output: upstream 1 (`44c5fe4`, `Merge branch 'topic'`, parents `dc1eb1b` and `b062bd8`), downstream 0.
  - C4, duplicates, in each fixture repository: `git show <sha> | git patch-id --stable`, and `git log --format=%B | grep -c 'cherry picked from'`. Output: upstream `780d480` and downstream `7272f58` both give patch-id `cd592b9ad808e57e6bde1edcaf9c4317b856af65`; `4cd9e3a` gives `e1248ba82034417c826376fbd34049800af66964`; downstream `e801e41` gives `d2167537a1c317a7721b3be0211376588c5ad4dd`; the `grep -c` count is 0 in both, so no `(cherry picked from commit ...)` line exists. `git merge-base --is-ancestor 780d480 HEAD` in downstream exits 1: the original is not in downstream's history.
  - C5, records: the fields 3 to 6 of C1's output (hand read). Only `9f1bb6b` carries a record (`scheduled`, owner `human`, `gap`, concept `src/window_check.py#in_window`); the other 7 listed lines have empty record fields.
- Counted once: 8 listed lines, one duplicate pair (`780d480` with `7272f58`): **7 fixes** (upstream 6, downstream's own 1) and **1 revert** (of a `feat`, gathered, not classified).
- Not gathered: `dc1eb1b` (`docs`), the merge `44c5fe4`, the feature commits.
- Chronology, from `git log --format='%h %cI %aI %s'` in each fixture repository (committer dates read before any causal claim): downstream `7272f58` is committed 2026-02-13, a day after upstream's `780d480` (2026-02-12), so the downstream copy follows its original.
- Placement: `780d480` and `7272f58` are one fix, attributed upstream (equal patch-ids; `780d480` is not an ancestor of downstream's `HEAD`).
- Thin messages: `e801e41` (subject only). The diff, blame and history were read by both classifiers; no ticket is referenced, so none was read.
- Classification: **reconciled, 0 unresolved** (see Reconciliation).

### Classifier digests

The six digests exactly as the subagents returned them, in the order A1, A2, B1, B2, C1, C2; the only edit is the fixture's temporary path written `<fixture>`. They were not rewritten after the comparison.

#### A1 (`a49f2a0c28f9554a1`, batch A)

~~~text
DIGEST (repository: upstream, <fixture>/upstream). History of the touched files is linear: no merge commits, so no fix is "in-run". Code was not executed (a python run was denied by the permission classifier), so behavior claims below come from reading diffs and blame only and are marked "not verified by command". Git read commands were all that ran.

1) 780d480f85010829a9fd57fff01e4b5311c22adb  "fix: accept padded and upper-case durations"
- population: product (src/duration.py; first parent 1c670d8; committed 2026-02-12)
- message vs diff: consistent. The diff changes `text[:-1]` to `text.strip()[:-1]` and `text[-1]` to `text.strip()[-1].lower()` (src/duration.py:7-8). That is padding and unit-case handling, as the message says. It strips twice and leaves `[-1]` on a possibly empty string, which the next fix addresses.
- untested condition (derived): input-form variance. Text with padding or upper case never appeared in the original test table (CASES holds "90s", "15m", "2h" only; tests/test_duration.py:3-7).
- structural cause (derived): the parser assumed canonical, pre-normalised input and had no normalisation step. The first repair patched each use site separately instead of normalising once at the boundary. That left a duplicated strip and an unguarded index into the possibly empty remainder.
- lineage: repairs 1c670d8 (the original feat that introduced the parser; 2 days earlier, 2026-02-10 to 2026-02-12). Shape seen at: none before it.
- evidence: `git show -M -U0 780d480`; `git blame --porcelain 780d480^`, where pre-image lines 7-8 are both from 1c670d8 (committer-time 1770724800).
- confidence: HIGH (the diff is two lines and the blame is unambiguous). Verified by command: yes for diff and blame. Behavioral effect: not verified by command.
- newest blamed line: 1c670d8, 2 days before the fix, young = yes.

2) 4cd9e3a8a0497f4ac38ffc6b024589f578938697  "fix: clean the text once before splitting it"
- population: product (src/duration.py; first parent 780d480; committed 2026-02-14)
- message vs diff: consistent. The body says "stripped the text twice and indexed an empty remainder". The diff replaces the two strips with a single `cleaned = text.strip().lower()`, then `value, unit = cleaned[:-1], cleaned[-1:]` (src/duration.py:7-8). The double strip and the `[-1]` index are both visible in the pre-image. The fix is partial: the slice `[-1:]` yields "" instead of raising IndexError, so an empty or blank input would fall through to `int("")`. By reading, the error type changes from IndexError to ValueError, which the message does not mention. Not verified by command.
- untested condition (derived): degenerate input (empty or whitespace-only text, where there is no unit character). Neither the original table nor the previous fix covered it.
- structural cause (derived): the previous repair added input handling without checking its boundary cases, so the problem was fixed in a second pass. The parse step has no explicit input validation. It relies on indexing and on `int()` or dict lookup raising, so malformed input produces incidental exception types.
- lineage: repairs 780d480 (2 days earlier; a fix repairing a fix, committer dates 2026-02-12 to 2026-02-14). Chain: 1c670d8 -> 780d480 -> 4cd9e3a on the same two lines.
- evidence: `git show -M -U0 4cd9e3a`; `git blame --porcelain 4cd9e3a^`, where pre-image lines 7-8 are both from 780d480 (committer-time 1770897600).
- confidence: HIGH on lineage and diff; MEDIUM on the effect on empty input (reasoned from code, not executed). Verified by command: diff and blame yes; runtime behavior no.
- newest blamed line: 780d480, 2 days before the fix, young = yes.

3) 2ba358c5f03cb539afb41781adfe10931979e56d  "fix: make the table test use its table"
- population: test or proof fix (tests/test_duration.py only; first parent 4cd9e3a; committed 2026-02-16)
- message vs diff: consistent. The pre-image `assert parse_duration("90s") == 90` inside `for text, expected in CASES` ignored both loop variables. The diff changes it to `assert parse_duration(text) == expected` (tests/test_duration.py:12).
- untested condition (derived): the test's oracle was not coupled to its own data. The loop always asserted the one fixed input "90s", so the "15m" and "2h" rows were never exercised. The parser fixes 780d480 and 4cd9e3a were never exercised by the table either. The corrected CASES still contains no padded, upper-case or empty rows, so those fixes remain unguarded by tests.
- structural cause (derived): a table-driven test with a hard-coded literal in the loop body and no check that each row reaches the assertion, so the test was vacuous from birth. It passed green in the same commit that introduced it.
- lineage: repairs 1c670d8 (the test's original line 12; 6 days earlier, 2026-02-10 to 2026-02-16). No prior-fix lineage.
- evidence: `git show -M -U0 2ba358c`; `git blame --porcelain 2ba358c^`, where line 12 is from 1c670d8 (committer-time 1770724800); `git show 2ba358c:tests/test_duration.py` shows CASES unchanged.
- confidence: HIGH. Verified by command: yes (diff, blame, file contents).
- newest blamed line: 1c670d8, 6 days before the fix, young = yes.

Cross-fix note: all three share the same introducing commit, 1c670d8 (2026-02-10). Fixes 1 and 2 repair the parser in sequence, and fix 3 repairs the test that should have caught them. The test was therefore vacuous while both parser repairs were made, and no row for the fix inputs has been added since.
~~~

#### A2 (`a0b0c17369eab7f70`, batch A)

~~~text
RESULT: complete. Git reads only. I ran no code, so every behavioral claim below is "verified: no". Nothing was written. An attempt to run the parser versions was denied by the classifier, and I dropped it.

Repository upstream (<fixture>/upstream). History is linear on main: 1c670d8 -> 780d480 -> 4cd9e3a -> 2ba358c, all on the first-parent chain. The only merge on main, 44c5fe4 (2026-03-08), brings in the unrelated retry-helper branch (ae06953, b062bd8). None of the three fixes entered through a merge, so none is an in-run correction.

**1. 780d480** (committed 2026-02-12)
- Population: product. src/duration.py only.
- Message vs diff: consistent. The message is "accept padded and upper-case durations". The diff changes `value = text[:-1]` to `text.strip()[:-1]` and `unit = text[-1]` to `text.strip()[-1].lower()` (src/duration.py:7-8).
- Untested condition: derived, input-shape variants (surrounding whitespace, upper-case unit).
  - Table test CASES hold only "90s", "15m", "2h".
  - The test body asserts the literal `parse_duration("90s") == 90` (tests/test_duration.py:12), so even those cases were never exercised.
  - The fix added no test (stat: only src/duration.py).
- Structural cause: the parser did no input normalization at its boundary. The only test was vacuous, so nothing could expose it. The fix then normalized per use site (once on each of the two lines) instead of once, which led to 4cd9e3a.
- Lineage: repairs 1c670d8, 2 days old (2026-02-10 -> 02-12).
- Newest blamed line: 1c670d8 (src/duration.py:7-8 on 780d480^), 2 days before the fix, young: yes.
- Evidence: `git show -M -U0 780d480`; `git blame --porcelain -L7,8 780d480^ -- src/duration.py` gives both lines to 1c670d8 (committer-time 1770724800).
- Confidence: HIGH. The diff is two lines and the blame is unambiguous.
- Verified by command: git show and blame, yes. Runtime behavior, no.

**2. 4cd9e3a** (committed 2026-02-14)
- Population: product. src/duration.py only.
- Message vs diff: consistent.
  - The message says "stripped the text twice and indexed an empty remainder".
  - The diff replaces the two `text.strip()` calls with a single `cleaned = text.strip().lower()`.
  - It also replaces `[-1]` with a slice, `value, unit = cleaned[:-1], cleaned[-1:]` (src/duration.py:7-8).
  - Caveat from reading only, not run: for empty or whitespace-only text the slice no longer raises IndexError. The call then reaches `int("")` or `UNITS[""]` and still fails, now with ValueError or KeyError. The failure changes type but remains.
  - The pre-image of 780d480 already had `text[-1]`, so the empty-input hazard predates the "previous repair".
- Untested condition: derived, degenerate input after normalization (empty or whitespace-only remainder). No test was added (stat: only src/duration.py). The test was still vacuous until 2ba358c.
- Structural cause: the prior repair (780d480) applied normalization at each index site and left the empty-input path unconsidered. Nothing exercised that path, so a second pass was needed.
- Lineage: repairs 780d480, 2 days old (2026-02-12 -> 02-14). Shape also seen at 780d480, a fix whose first attempt needed a follow-up.
- Newest blamed line: 780d480 (src/duration.py:7-8 on 4cd9e3a^), 2 days before the fix, young: yes.
- Evidence: `git show -M -U0 4cd9e3a`; `git blame --porcelain -L7,8 4cd9e3a^ -- src/duration.py` gives both lines to 780d480 (committer-time 1770897600).
- Confidence: MEDIUM. The diff-to-message match is clear, but the claim that the empty-input failure only changes type comes from reading, not execution.
- Verified by command: git show and blame, yes. Runtime behavior, no.

**3. 2ba358c** (committed 2026-02-16)
- Population: test or proof fix. tests/test_duration.py only.
- Message vs diff: consistent. The message says "the loop asserted one fixed input and never read its cases". The diff changes `assert parse_duration("90s") == 90` to `assert parse_duration(text) == expected` at tests/test_duration.py:12, inside `for text, expected in CASES`.
- Untested condition: derived. Every CASES row other than "90s" was never run, so the test was vacuous.
- Structural cause: the test loop bound variables it never used, so a passing test said nothing about "15m", "2h" or any other case. The repair is also incomplete as a net: CASES still has no padded, upper-case or empty entries, so the behaviors fixed in 780d480 and 4cd9e3a remain unasserted.
- Lineage: repairs 1c670d8, 6 days old (2026-02-10 -> 02-16). It did not repair the product fixes themselves.
- Newest blamed line: 1c670d8 (tests/test_duration.py:12 on 2ba358c^), 6 days before the fix, young: yes.
- Evidence: `git show -M -U0 2ba358c`; `git blame --porcelain -L12,12 2ba358c^ -- tests/test_duration.py` gives the line to 1c670d8 (committer-time 1770724800).
- Confidence: HIGH. One line, and the blame and message agree.
- Verified by command: git show and blame, yes.
~~~

#### B1 (`af872c00d55cc0da3`, batch B)

~~~text
DIGEST (repository: upstream; history on main is linear, no merge commits, so no fix qualifies as in-run)

1. repository upstream, SHA 9f1bb6bb7f67df18f97e5b5fd5c45da373c991ea (9f1bb6b), population: product
- message versus diff: consistent. The diff is src/window_check.py:7, `moment > end` -> `moment >= end`, which matches "treat the window end as exclusive". One note: the trailer says the bounds "had no stated rule", but line 1 of the pre-image already says "half-open window" (aeffdb0). The rule was stated and the code contradicted it.
- untested condition: derived, the boundary instant. The moment equals the end of one window and the start of the adjacent one. No test covers window_check; the only tests/ file is test_duration.py.
- structural cause: in_window did not follow the half-open contract stated in its own header comment, and nothing executable pinned the rule, so the code and the comment diverged at the end bound.
- lineage: repairs aeffdb0 (2026-01-05, 55 days old at the fix).
- evidence: `git show -M -U0 9f1bb6b`; `git blame --porcelain 9f1bb6b^ -- src/window_check.py` attributes pre-image line 7 to aeffdb0; `git ls-tree -r main` shows no test for window_check.
- confidence: HIGH, because the one-line diff and the blame agree with the header comment.
- verified: by command.
- newest blamed line: aeffdb0, 55 days before the fix. Young (<=30 days): no.

2. repository upstream, SHA d7aaf9ca94bf8b8af8371fcfb10c5858183b6d4a (d7aaf9c), population: product (a mislabeled change)
- message versus diff: misleading. The message says "reword the length comment: Comment-only change: no behaviour differs." The diff has two hunks. Line 13 is a comment reword. Line 18 changes code: `return a_start < b_end and b_start < a_end` -> `return a_start <= b_end and b_start <= a_end`. This is a behaviour change in overlaps(): windows that only touch at an instant now count as overlapping. That contradicts the half-open rule in the file header and the 9f1bb6b fix made two days earlier. The message hides a regression.
- untested condition: derived, adjacent half-open windows that touch at one instant (a.end == b.start). overlaps() has no test.
- structural cause: a behaviour edit was bundled into a change labeled comment-only, so it got no behaviour scrutiny. Nothing executable pins the half-open contract for overlaps(), and 9f1bb6b fixed in_window only, leaving the sibling function inconsistent.
- lineage: shape seen at 9f1bb6b (the same inclusive/exclusive boundary question in the same file, two days earlier). It repairs nothing. It introduces the opposite error to the one 9f1bb6b fixed.
- evidence: `git show -M -U0 d7aaf9c`; `git blame --porcelain d7aaf9c^` attributes the pre-image lines 13 and 18 to aeffdb0; `git show main:src/window_check.py` shows `<=` still on main.
- confidence: HIGH for the misleading message and the regression, which are visible directly in the diff.
- verified: by command.
- newest blamed line: aeffdb0 (2026-01-05), 57 days before the fix. Young: no.

3. repository upstream, SHA b062bd887b9a2d4ff3488a7ea82ee0ddd8348625 (b062bd8), population: product
- message versus diff: consistent. The diff is src/retry.py:6, `return delay` -> `return min(delay, cap)`, which matches "cap the retry delay". The file header already promised "up to a cap", and `cap` was an unused parameter.
- untested condition: slow or failing dependency. The delay `base * 2 ** attempt` only grows large when retries keep failing across many attempts. A derived reading is a high attempt count. No test exists for retry.py.
- structural cause: the helper was added with the `cap` parameter and the "up to a cap" contract but never applied the cap. It had no test and was not self-checked against its own comment.
- lineage: repairs ae06953 (2026-03-06, 1 day old at the fix).
- evidence: `git show -M -U0 b062bd8`; `git blame --porcelain b062bd8^ -- src/retry.py` attributes pre-image line 6 to ae06953; `git log --graph` shows ae06953 as b062bd8's direct parent. The later revert 6b338d9 is unrelated.
- confidence: HIGH.
- verified: by command.
- newest blamed line: ae06953, 1 day before the fix (committer time). Young: yes.
~~~

#### B2 (`a30f125cfd0f92694`, batch B)

~~~text
RESULT: complete. Read-only. All three fixes verified by command. History is linear, with no merge commits. `git log --graph` shows every SHA is a single-parent main commit, so none is an in-run correction. Test coverage on main is only tests/test_duration.py, with no tests for window_check or retry.

## 1. upstream 9f1bb6b (full SHA 9f1bb6bb7f67df18f97e5b5fd5c45da373c991ea), 2026-03-01
- **Population:** product.
- **Message vs diff:** consistent. The message says the window end is made exclusive. The diff changes src/window_check.py:7 from `moment > end` to `moment >= end`.
- **Untested condition:** derived, the boundary instant where a moment equals the window end, which is where adjacent windows meet. The test set has no case for it.
- **Structural cause:** the code contradicted a rule the file already stated, and no executable check tied the two together. The first line of the file, in the introducing commit, says "half-open window", and the pre-image used `>`, which is inclusive at the end. The commit's `Prevention-Cause` trailer ("no stated inclusive or exclusive rule") conflicts with this: the rule was stated in a comment, but nothing enforced it.
- **Lineage:** repairs aeffdb0, 55 days old (committer dates 2026-01-05 to 2026-03-01).
- **Evidence:**
  - `git show -M -U0 9f1bb6b`: one hunk at line 7.
  - `git blame --porcelain 9f1bb6b^`: all 18 lines are from aeffdb0.
  - `git show aeffdb0:src/window_check.py | head -3`: header reads "half-open window".
- **Newest blamed line:** aeffdb0 (committer time 1767614400), 55 days before the fix. Not young.
- **Confidence:** HIGH. The diff is one line and the blame is unambiguous.
- **Verified:** by command.

## 2. upstream d7aaf9c (full SHA d7aaf9ca94bf8b8af8371fcfb10c5858183b6d4a), 2026-03-03
- **Population:** product.
- **Message vs diff:** misleading. The message is "fix: reword the length comment — Comment-only change: no behaviour differs." The diff has two hunks:
  - Line 13 is a comment rewording, as described.
  - Line 18 changes the code in `overlaps`: `a_start < b_end and b_start < a_end` becomes `a_start <= b_end and b_start <= a_end`. Windows that merely touch at a boundary now count as overlapping.
  - This contradicts the half-open rule that 9f1bb6b made exclusive two days earlier, and the "half-open window" header comment.
  - The commit is not a repair. It introduces a regression and hides it under a comment-only label.
- **Untested condition:** derived, adjacent (touching) windows with `a_end == b_start`, the same boundary-equality condition as 9f1bb6b but in the sibling function `overlaps`. No test exists.
- **Structural cause:**
  - The same half-open rule is applied separately in two functions (`in_window` and `overlaps`), with no shared definition or test.
  - A fix to one function did not carry to the other.
  - The message is not checked against the diff.
- **Lineage:** shape seen at 9f1bb6b (2 days earlier). It is the same inclusive/exclusive boundary question, reversed in a sibling function. It repairs nothing.
- **Evidence:**
  - `git show -M -U0 d7aaf9c`: hunks at lines 13 and 18.
  - `git blame --porcelain d7aaf9c^`: pre-image lines 13 and 18 are both aeffdb0. Line 7 is 9f1bb6b but is not in the diff.
  - `git show d7aaf9c:src/window_check.py`: header says half-open, line 18 uses `<=`.
  - The `overlaps` signature and semantics at 9f1bb6b are the pre-image of this change.
- **Newest blamed line:** aeffdb0 (committer time 1767614400), 57 days before the fix (2026-01-05 to 2026-03-03). Not young.
- **Confidence:** HIGH for the message-versus-diff mismatch and the lineage. MEDIUM on the intent of the `overlaps` change, since only the diff shows it.
- **Verified:** by command.

## 3. upstream b062bd8 (full SHA b062bd887b9a2d4ff3488a7ea82ee0ddd8348625), 2026-03-07
- **Population:** product.
- **Message vs diff:** consistent. The message says the delay doubled without bound. The diff changes src/retry.py:6 from `return delay` to `return min(delay, cap)`. `cap` was an unused parameter.
- **Untested condition:** derived, large attempt counts where the doubling delay exceeds the cap. The `cap` argument was accepted but never applied. No test exists.
- **Structural cause:**
  - The function's header comment ("doubling each time up to a cap") and its signature (`cap` parameter) stated the contract.
  - The body never used `cap`, and nothing exercised the function.
  - The helper was added with no test.
- **Lineage:** repairs ae06953, 1 day old (committer dates 2026-03-06 to 2026-03-07). It repairs the helper's introducing commit.
- **Evidence:**
  - `git show -M -U0 b062bd8`: one hunk at line 6.
  - `git blame --porcelain b062bd8^`: all 6 lines are from ae06953.
  - `git show b062bd8:src/retry.py`: the header comment is line 1.
- **Newest blamed line:** ae06953 (committer time 1772798400), 1 day before the fix (86400 s). Young (<= 30 days).
- **Confidence:** HIGH.
- **Verified:** by command.

## Summary table

| SHA | Newest blamed commit | Age at fix | Young (<= 30 days) |
|---|---|---|---|
| 9f1bb6b | aeffdb0 | 55 days | no |
| d7aaf9c | aeffdb0 | 57 days | no |
| b062bd8 | ae06953 | 1 day | yes |

I did not read the downstream repository's content. Its log, which shows a fork carrying a subset of upstream commits, came back incidentally in one command. It is not used here.
~~~

#### C1 (`a1f5124fac47f2e62`, batch C)

~~~text
DIGEST
- repository: downstream (<fixture>/downstream)
- SHA: e801e41f16fba9ec7b9c84acedd311e7164588fc (2026-02-20, "fix: report line")
- population: product. History is linear with no merges, so it is not in-run, and it is not a test or proof fix. The fix touches only src/report.py.
- message versus diff: misleading. The message "fix: report line" has no body and does not say what was fixed. The diff is one line in src/report.py: `return label + ": " + count` becomes `return label + ": " + str(count)`. It fixes a TypeError when `count` is an int (str + int), which is exactly what the function's comment says it renders ("Render a count...").
- untested condition: derived, a non-string (int) argument to a function documented to take a count. It is not one of the preset conditions. The nearest is "other platform or provider", and it does not fit.
- structural cause: the function was added (0373b96) with no test (no tests/test_report.py; `git grep report e801e41 -- tests src` matches only src/report.py). The one line concatenated a str with a value the function's own contract says is numeric, so the first call with an int raised TypeError. Nothing in the commit exercised the function with its intended input type.
- lineage: repairs 0373b96, 7 days old (committer 2026-02-13T12:00Z to fix 2026-02-20T12:00Z). It is a repair of the code the commit introduced. Related sibling: 7272f58 (same day, 2026-02-13) fixed duration parsing for input variants (padded and upper-case text). That is not the same shape, only a similar kind of problem (untested input variants). Prior art is not claimed.
- evidence:
  - `git show -M -U0 e801e41`: one hunk at src/report.py:5, as above.
  - `git blame --porcelain e801e41^ -- src/report.py`: all 5 lines, including the pre-image line 5, are attributed to 0373b96 (committer-time 1770984000 = 2026-02-13T12:00Z).
  - `git log` shows linear parentage 0373b96, 7272f58, e801e41.
  - `git grep report e801e41 -- tests src` found no test of report.py.
- confidence: HIGH. The diff is a single line, blame is unambiguous (one commit), and the test absence was checked by grep. The reason an int was passed (a caller bug versus a design slip) is inferred from the code comment only, since no caller exists in the repo.
- verified: yes, by command (git show, git blame --porcelain, git log, git grep, all read-only).

NEWEST BLAMED LINE: commit 0373b96 (feat: add a report line renderer), committer 2026-02-13T12:00Z, 7 days before the fix. Young (<= 30 days): yes.
~~~

#### C2 (`a347a45c9dbc12d3b`, batch C)

~~~text
DIGEST ENTRY
- repository: downstream (<fixture>/downstream)
- SHA: e801e41f16fba9ec7b9c84acedd311e7164588fc ("fix: report line", committer 2026-02-20T12:00Z)
- population: product. History is linear (no merge commits, `git log --graph`), so the fix and the code it repairs did not enter main through one merge. It is not an in-run correction. It is not a test or proof fix: it changes src/report.py only.
- message versus diff: misleading. The subject "fix: report line" has no body and names no symptom. The diff is one line in src/report.py: `return label + ": " + count` became `return label + ": " + str(count)`. The real change is coercing a non-string `count` (an int) to str, which fixes a TypeError on `str + int`. The message does not say that.
- untested condition: derived, a caller-supplied argument of a different type than the code assumed (non-string count). None of your preset categories fits. The closest is "other platform or provider".
- structural cause: `report_line` is documented as "Render a count as a short report line", yet it concatenated `count` as if it were already a string. The function's own contract names a numeric input. The type was never exercised. The tree has no test for src/report.py (`git ls-tree -r e801e41` lists only tests/test_duration.py). No test was added with the fix.
- lineage: none. The nearest earlier fix is 7272f58 (2026-02-13, "fix: accept padded and upper-case durations", src/duration.py). It repairs a different file and normalizes string input. The shape is only loosely related (input not in the canonical form the code assumed), so I do not claim "shape seen at". The repaired line was introduced by 0373b96 (2026-02-13), 7 days before the fix.
- evidence:
  - `git show -M -U0 e801e41`: single hunk at src/report.py:5.
  - `git blame --porcelain e801e41^ -- src/report.py`: lines 1-5 all trace to 0373b96 ("feat: add a report line renderer", committer-time 1770984000 = 2026-02-13T12:00Z). The repaired line 5 is in that set.
  - `git log --graph`: e801e41 <- 7272f58 <- 0373b96 <- 1c670d8 <- aeffdb0, linear.
  - `git show e801e41:src/report.py`: 5 lines, `str(count)` at line 5.
- confidence: HIGH. The diff is one line, blame is unambiguous, and the type-coercion reading follows directly from the code and the file's header comment. The one inference is that callers pass an int, which no caller or test in the repo shows.
- verified: verified by command (git show, git blame --porcelain, git log, git ls-tree). The fixture contains no caller of report_line, so the int-argument cause is not confirmed by an observed call.

NEWEST BLAMED LINE
- Commit 0373b96, committer date 2026-02-13T12:00Z.
- Age before the fix: 7 days (2026-02-20 minus 2026-02-13).
- Young (<= 30 days): yes.
- All five pre-image lines come from the same commit, so there is no older blame to compare against.
~~~

### Reconciliation

Each batch was classified twice, independently; the six digests above were compared field by field (repository, SHA, population, message versus diff, untested condition, structural cause, lineage, evidence, confidence with its reason, verified mark) and every difference was settled by re-reading the source in the fixture repositories. Fields not listed below were equal in substance between the two classifiers of a batch: repositories and SHAs, message-versus-diff verdicts, the population of the six other fixes, the newest blamed commit and age of all seven, and the evidence and verified marks: for every fix both classifiers cite the same `git show -M -U0` hunks and `git blame --porcelain` pre-image attribution (A2 and B1 restrict blame with `-L` or `--` paths; the attributed commits are identical), and both mark diff and blame verified by command, with behavioural claims (A1 and A2 on `4cd9e3a`, C1 and C2 on the caller's int argument) marked not verified or inferred. Statuses: resolved (the source decides) or unresolved (it does not).

| # | Fix | Field | Classifier readings | Source re-read | Status |
|---|---|---|---|---|---|
| D1 | `4cd9e3a` | structural cause, confidence | A1: empty input changes from `IndexError` to `ValueError`; confidence HIGH on lineage, MEDIUM on this effect. A2: the failure becomes `ValueError` or `KeyError`; confidence MEDIUM. Neither ran the code. | `git show 4cd9e3a:src/duration.py`: `value, unit = cleaned[:-1], cleaned[-1:]` then `return int(value) * UNITS[unit]`. Python evaluates `int(value)` before `UNITS[unit]`; with `cleaned == ""`, `int("")` raises `ValueError` first, so `KeyError` cannot occur. Read, not executed (an attempt to execute it was refused by the session's classifier and dropped). | resolved: A1's reading; MEDIUM confidence kept, as the effect is hand-traced |
| D2 | `d7aaf9c` | structural cause, confidence | B1: a behaviour edit bundled in a change labelled comment-only, so it got no behaviour scrutiny; HIGH. B2: the half-open rule is applied separately in two functions with no shared definition, and the message is not checked against the diff; HIGH on the mismatch and lineage, MEDIUM on the intent. | `git show d7aaf9c:src/window_check.py`: the header says "half-open window"; `in_window` uses `moment >= end` (line 6) and `overlaps` uses `<=` on both ends (line 18); the message says "Comment-only change: no behaviour differs" and the diff has the comment hunk and the `overlaps` hunk. Both readings hold in the source and do not conflict. | resolved: both causes kept (Weaknesses 2 and 3) |
| D3 | `b062bd8` | untested condition | B1: "slow or failing dependency" (a derived reading, a high attempt count). B2: derived, a large attempt count whose delay exceeds the cap. | `git show b062bd8:src/retry.py`: `next_delay` is `base * 2 ** attempt` and `min(delay, cap)`, with no call to a dependency, so the preset condition does not fit. | resolved: B2's derived condition |
| D4 | `e801e41` | lineage | C1: `repairs 0373b96`, 7 days. C2: lineage `none`, while stating that the repaired line comes from `0373b96`, 7 days earlier. | `git blame --porcelain e801e41^ -- src/report.py`: all 5 lines are `0373b96`; the one-line diff changes `count` to `str(count)` on a line of that commit. | resolved: `repairs 0373b96`, 7 days (C1); C2's `none` meant no earlier shape, which also holds |
| D6 | `4cd9e3a` | lineage | A1: `repairs 780d480`, 2 days. A2: `repairs 780d480`, 2 days, and also "shape also seen at 780d480". | `git blame --porcelain 4cd9e3a^ -- src/duration.py`: pre-image lines 7 and 8 are `780d480`, the commit it repairs; `git show 780d480 -U0` shows those two lines are the ones `780d480` rewrote. The "shape seen" is the same commit as the repaired one, so it adds no second lineage. | resolved: `repairs 780d480`, 2 days (A1) |
| D7 | `780d480` | structural cause | A1: no normalisation step at the parser boundary; the first repair patched each use site, leaving a duplicated strip and an unguarded index. A2: the same, plus the table test being vacuous so nothing could expose it. | `git show 780d480 -U0`: the two rewritten lines each call `text.strip()` (`text.strip()[:-1]`, `text.strip()[-1].lower()`); `git show 1c670d8:tests/test_duration.py` line 12 is `assert parse_duration("90s") == 90` inside `for text, expected in CASES`, so the other rows never ran. Both facts hold. | resolved: both causes kept (Weaknesses 1) |
| D5 | `b062bd8` | population | Both B1 and B2 (agreeing): product, "history is linear". Not a divergence between them, but contradicted by C3. | `git rev-list --first-parent HEAD` in upstream lists `6b338d9 fb59847 44c5fe4 dc1eb1b d7aaf9c 9f1bb6b 2ba358c 4cd9e3a 780d480 1c670d8 aeffdb0`: neither `b062bd8` nor its repaired `ae06953` is on it; `git rev-list 44c5fe4^1..44c5fe4^2` lists exactly `b062bd8 ae06953`, entering through the merge `44c5fe4`. | resolved against both classifiers: in-run correction |

- Agreement is not proof: D5 is a shared error. Two independent classifiers that both read "linear" from a log view do not establish the in-run rule, which is decided by the merge and the first-parent chain (C3). The orchestrator applied that rule to every fix, not only `b062bd8`: upstream's other five fixes are on the first-parent chain.
- Unresolved divergences: none.
- Confidence: the effect of `4cd9e3a` on empty input stays MEDIUM (hand-traced); `e801e41`'s cause is MEDIUM in Weaknesses 4 (inferred from a one-line diff); the other causes keep the classifiers' HIGH.

### Populations

Classified from the reconciled digests and C3; none dropped.
- Product fixes (5): `780d480` (one fix with `7272f58`), `4cd9e3a`, `9f1bb6b`, `d7aaf9c`, downstream `e801e41`.
- In-run corrections (1): `b062bd8` (D5). It and the code it repairs (`ae06953`) entered `main` only through merge `44c5fe4`.
- Test or proof fixes (1): `2ba358c` (its diff touches only `tests/test_duration.py`; both classifiers agree).

### Weaknesses

Principle altitude, one per cause, with evidence and confidence.
1. **A repair is judged by reading, never by a test that can fail.** The duration parser and its table test entered together in `1c670d8`; the test's assertion ignored the loop variables (`tests/test_duration.py:12`), so only `"90s"` was ever exercised. Two source fixes landed on the same two lines before the test was repaired: `4cd9e3a` repairs `780d480` (chain `1c670d8` → `780d480` → `4cd9e3a`, a repair of a repair), and `2ba358c` repairs the test six days after it was written. After `2ba358c` the table still has no padded, upper-case or empty case, so what `780d480` and `4cd9e3a` changed stays untested (both classifiers of batch A). Confidence HIGH on the lineage (blame of the pre-image on each first parent, verified by both classifiers); MEDIUM on the cause, which is derived.
2. **A convention lives in a comment, once per function.** `src/window_check.py` states "half-open window" in its header comment; `in_window` and `overlaps` encode it separately and no test touches the file. `9f1bb6b` made `in_window` exclusive at the end (pre-image blamed to `aeffdb0`, 55 days old); two days later `d7aaf9c` made `overlaps` inclusive at both ends (D2). Confidence HIGH on the facts, MEDIUM on the cause (derived).
3. **A commit message describing less than its diff.** `d7aaf9c` says "Comment-only change: no behaviour differs" and is misleading: its diff also changes `return a_start < b_end and b_start < a_end` to `return a_start <= b_end and b_start <= a_end` in `overlaps`, so touching windows now overlap, contradicting the half-open rule `9f1bb6b` applied two days before. Caught by its diff by both classifiers of batch B. Confidence HIGH on the mismatch; MEDIUM that it is a regression rather than an intended change, since the commit states no intent (B2).
4. **A documented contract with no executable check.** `b062bd8` (`next_delay(attempt, base, cap)` ignored `cap`) and `e801e41` (a function documented to render a count concatenated it to a string) both repair a promise made by a signature or comment that nothing exercised; there is no test for `src/retry.py` or `src/report.py`. Confidence HIGH on `b062bd8`; MEDIUM on `e801e41`, whose cause is inferred from a one-line diff and an empty message, with no caller in the repository.
5. **A fork carries a fix without its repairs.** Downstream's `7272f58` is upstream's `780d480`, and downstream's `HEAD` lacks `4cd9e3a` and `2ba358c` (`git merge-base --is-ancestor 780d480 HEAD` exits 1 for the original; the repairs are not in its history). Confidence HIGH on the absence of the repairs, read from the patch-ids and log (hand read).
6. **A record's cause can disagree with its repository.** `9f1bb6b` records the cause "window bounds had no stated inclusive or exclusive rule", but the pre-image's first line already states a half-open rule; the rule was stated and not enforced. Both classifiers of batch B report it. Confidence HIGH.

### Coverage

Not computable: neither repository has an invariant file or a guardrails file (`git ls-files` lists only `src/`, `tests/` and, upstream, `docs/overview.md`), so no cause can be checked against them in the classification vocabulary (gap, unenforced, not consulted, none needed). The only classification present is `9f1bb6b`'s own record, *gap*, taken as stated and not verified against any file.

### Process findings

- Fixes repairing recent fixes (ages by committer dates): `4cd9e3a` repairs `780d480` (2 days); `780d480` repairs `1c670d8` (2 days); `2ba358c` repairs the test of `1c670d8` (6 days); `b062bd8` repairs `ae06953` (1 day, in-run); `e801e41` repairs downstream's `0373b96` (7 days).
- The same shape twice: boundary inclusivity in `src/window_check.py`, `9f1bb6b` (in `in_window`) then `d7aaf9c` (in `overlaps`), two days apart; the second declared comment-only.
- Concepts shared before being defined: the window convention (Weaknesses 2), written once as prose and re-implemented per function.
- Triggers drawn too narrowly: not computable (no guardrails file).
- Classification: reconciled, 0 unresolved. One population was corrected against the census (D5), which a single classification, or two agreeing ones, would have left wrong.

### Direction (young-code share)

Method, per fix `<sha>`, in the fixture repository: `git show -M -U0 <sha>` for the hunks, then `git blame --porcelain <sha>^ -- <file>` for each hunk's pre-image lines; a fix is young when the newest blamed line's committer time is at most 30 × 86400 s before the fix's. Each fix is counted once; the duplicate `7272f58` is not counted. Results, ages from committer epochs as reported identically by both classifiers of each batch:

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
| Two independent classifiers per batch | Header; Reconciliation | batches A, B, C; A1/A2, B1/B2, C1/C2 |
| Each divergence resolved or reported | Reconciliation (D1 to D7, 0 unresolved); Classifier digests | `780d480`, `4cd9e3a`, `d7aaf9c`, `b062bd8`, `e801e41` |
| Repair-of-a-repair chain | Weaknesses (1); Process findings | `1c670d8`, `780d480`, `4cd9e3a` |
| Cherry-pick duplicate counted once | Census (C4, Counted once, Placement) | `780d480` with `7272f58` |
| Young-code share by blame | Direction | young: `780d480`, `4cd9e3a`, `2ba358c`, `b062bd8`, `e801e41`; old: `9f1bb6b`, `d7aaf9c`; 5/7 |
| Misleading message caught by its diff | Weaknesses (3) | `d7aaf9c` |

## Deviations

- The skill was read from the source checkout's `skills/fix-history-analysis/` files (`SKILL.md`, `references/full-mode.md`), not through the session's skill mechanism: it is not installed in this session's skill list. The steps were followed as written.
- The Period query was run through an extracted file (`sed -n 82p … > <fixture>/period-query.sh`, then `sh ../period-query.sh`) instead of the skill's command substitution, because the session's shell rules forbid `$()`; the query text is unchanged.
- The revert listing (C2) used `git log --format='%h %s' | grep -i '^[0-9a-f]* revert'`, a simpler filter than the earlier full-run's awk; both find the one `Revert "…"` subject, and no `revert(`-typed subject exists in either repository.
- Placement (C4) was done by the orchestrator before batching, so the duplicate `7272f58` was in no classifier's input; the two classifiers of a batch therefore received identical, hint-free briefs.
- No classifier executed code; where a claim rests on reading (D1, Weaknesses 1) it is marked so. An attempt by the orchestrator to execute the parser on empty input was refused by the session's permission classifier and not repeated by another route.
- The orchestrating session ran C1 to C5 and every source re-read of the Reconciliation table itself; classifier digests and blame results were otherwise taken as returned.
- The skill's report-section list (Census; Populations; Weaknesses; Coverage; Process findings; Direction; Draft goal) is followed in order; the *Reconciliation* section is added after Census to hold the step's result.
