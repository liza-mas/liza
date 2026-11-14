# Prevention record

What a bug fix leaves in the change history so that its prevention answer can be counted by a shell command, in any repository and any stack. The format is git trailers; the query is `git log` plus `awk`.

## Record

A **fix commit** is a commit whose subject has Conventional Commit type `fix`: `fix:`, `fix(<scope>):`, `fix!:` or `fix(<scope>)!:`. Each fix commit carries one record in the **final trailer block** of its message (the last paragraph, `Key: value` lines only). Reverts (type `revert`, or git's `Revert "…"`), merges and other types carry none.

A `Prevention-` key line outside the final trailer block makes the record malformed. This is how a careless squash shows: repair it by writing one record in one final block.

### Keys

Keys are a closed set, spelled exactly as below. Git matches keys case-insensitively, so a mis-cased key is read by the query as if well spelled; it is still malformed.

| Key | Cardinality | Value |
|---|---|---|
| `Prevention-Cause` | exactly 1 | one line: what made the bug possible, at the level of the system |
| `Prevention-Concept` | at least 1 | a concept identifier; one line per concept |
| `Prevention-Disposition` | exactly 1 | `removed`, `scheduled` or `accepted` |
| `Prevention-Owner` | exactly 1 when `scheduled`, else none | `human`, or one task or ticket reference token |
| `Prevention-Class` | exactly 1 | `gap`, `unenforced`, `not-consulted` or `none-needed` |

Example final block:

```
Prevention-Cause: the cache accepted writes without an owner for eviction
Prevention-Concept: internal/cache/store.go#Store.Put
Prevention-Disposition: scheduled
Prevention-Owner: TASK-42
Prevention-Class: not-consulted
```

### Concept identifier

One token, never containing whitespace, matched byte for byte (case-sensitive): two records share a concept only if their tokens are equal. Two kinds, told apart by form:

| Kind | Form | Example |
|---|---|---|
| Invariant ID | `[A-Za-z][A-Za-z0-9._-]*`: no `/`, no `#`; exactly the ID the project's invariant file gives the row | `T0.2` |
| Code boundary | repository-relative path containing at least one `/` (a root file is written `./<name>`), optionally `#<symbol>` with symbol `[A-Za-z0-9_.]+` | `internal/store/put.go#Store.Put` |

A fix touching several concepts lists each on its own `Prevention-Concept` line.

**Invariant without an ID.** Never edit the invariant file to mint one; invariants are the human's. Cite the code boundary that enforces or should enforce the property, and name the invariant in the commit body. A retrospective lists such invariants for the human, who may assign IDs.

**Renames.** An identifier is the boundary's path (and symbol) at the commit that records it; records are history and are never rewritten. A fixer who knows the boundary moved since an earlier record adds the earlier token as one more `Prevention-Concept` line, making the new record a bridge. A query then finds:

- by the earlier token: the earlier records and the bridge, not later records carrying only the current token;
- by the current token alone: the bridge and later records, never the records made before the move;
- by both tokens in one `CONCEPT`: every record on either side.

A lookup therefore passes every token the boundary is known to have had. Nothing tracks renames mechanically; two misses are accepted: a move nobody recorded in a bridge, and a lookup that passes only the current token without reading the bridge's earlier one. An invariant ID, stable across renames, is preferred wherever an invariant covers the shape.

### Dispositions

- `removed`: the shape is refused after this change. The commit body names the boundary that now refuses it, a negative test proving the refusal, and the sibling paths searched with their disposition. A reviewer judges this evidence.
- `scheduled`: an accountable owner removes it or decides on it: a task or ticket reference token, or `human` when the decision is the human's.
- `accepted`: why removing it is not worth it. Only the human accepts. An agent in multi-agent mode records `scheduled` with owner `human`.

`none-needed` is a classification, never a disposition: a disposition is still required. A recurrence of the shape voids an acceptance.

### Classes

`gap`: no invariant or guardrail covers the shape. `unenforced`: one covers it, nothing enforces it. `not-consulted`: one covers it, its trigger did not fire. `none-needed`: none is needed.

## Query

Lists the fix commits of a period, one line each, newest first, with seven TAB-separated fields: full SHA; committer date in UTC (`YYYY-MM-DDTHH:MM:SSZ`); concepts (space-separated, trailer order); disposition; owner (empty unless scheduled); class; subject. Only the final trailer block is read. Empty record fields mark a fix without a record; two values in a single-valued field, or concepts that miss an earlier paragraph's record, mark a malformed one. The subject is last, so a TAB inside it never shifts fields 1–6.

Variables, both git dates and both required (empty lists nothing):

- `SINCE`, `UNTIL`: the period; for a window of the last 14 days, `SINCE='14 days ago' UNTIL=now`.
- `CONCEPT`: one or more identifiers separated by spaces, written as in records. A fix is listed when any of its concepts equals any of them. No escaping is needed.

The history is `HEAD`'s unless a revision is appended after `git log`. Committer date is used because it is when a change landed on the branch (a cherry-pick takes a new one); `--since` filters on it. Git selects the period; `awk` tests the subject, so no `grep.*` setting changes the result and lines git adds for settings such as `log.showSignature` are dropped.

Minimum tools: git 2.22.0 and a POSIX `awk`.

### Period query

```sh
TZ=UTC git log --since="$SINCE" --until="$UNTIL" --date='format-local:%Y-%m-%dT%H:%M:%SZ' --format='%H%x09%cd%x09%(trailers:key=Prevention-Concept,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Disposition,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Owner,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Class,valueonly,separator=%x20)%x09%s' | awk 'BEGIN { FS = "\t" } $7 ~ /^fix(\([^)]*\))?!?: /'
```

### Concept query

```sh
TZ=UTC git log --since="$SINCE" --until="$UNTIL" --date='format-local:%Y-%m-%dT%H:%M:%SZ' --format='%H%x09%cd%x09%(trailers:key=Prevention-Concept,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Disposition,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Owner,valueonly,separator=%x20)%x09%(trailers:key=Prevention-Class,valueonly,separator=%x20)%x09%s' | CONCEPT="$CONCEPT" awk 'BEGIN { FS = "\t"; n = split(ENVIRON["CONCEPT"], t, " ") } $7 ~ /^fix(\([^)]*\))?!?: / { for (i = 1; i <= n; i++) if (index(" " $3 " ", " " t[i] " ")) { print; next } }'
```
