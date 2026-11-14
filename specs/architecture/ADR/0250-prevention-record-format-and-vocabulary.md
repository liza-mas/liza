# ADR-0250: Prevention Record Format and Vocabulary

## Status

ACCEPTED — 2026-11-14. Bug prevention discipline, FR2 (the record) with FR1's
semantics. Meaning of each key and value lives in
[`prevention-record.md`](../../../skills/shared/references/prevention-record.md);
this ADR records the decisions, not the vocabulary.

## Context

FR2 requires that a fix's answer to "what made the bug possible, and what now
refuses the shape" survive in the change history in a form a shell command can
count without reading prose, in any project and any stack (G1.1). FR4 matches
concepts across fixes, so a concept must be named by one identifier that two
records either share or do not. A human or agent should not need a parser
beyond git to list a period's fixes.

The master architecture probed how a record survives the two history rewrites
that happen to agent work: `cherry-pick -x` keeps a record carried in the
commit message's final trailer block, and a squash concatenates the messages
of the squashed commits, so every record but the last lands in an earlier
paragraph, outside the final block. A carrier that reads the final block
therefore sees a careless squash as a malformed record, and a repaired squash
(one merged block) as a valid one.

Free-text concepts never match, so FR4 would never fire; the identifier rule
is part of the decision, not a convention.

## Decision

1. **Carrier: git trailers in the final trailer block.** One record per
   commit. A `Prevention-` key line outside the final block makes the record
   malformed. The record applies to commits whose subject has Conventional
   Commit type `fix`; reverts and other types carry none.
2. **Closed key set.** `Prevention-Cause`, `Prevention-Concept`,
   `Prevention-Disposition`, `Prevention-Owner` and `Prevention-Class`, with
   the cardinalities and values the document defines. Keys are case-sensitive
   as written; git's `key=` matching is not, so a mis-cased key is treated as
   malformed by the validator rather than trusted from the query.
3. **Frozen concept identifier forms.** One whitespace-free token, matched
   byte for byte (case-sensitive): an *invariant ID*
   (`[A-Za-z][A-Za-z0-9._-]*`, no `/`, no `#`, exactly the ID the project's
   invariant file gives the row) or a *code boundary* (repository-relative path
   with at least one `/`, a root file written `./<name>`, optionally
   `#<symbol>` with symbol `[A-Za-z0-9_.]+`). No scope changes these forms;
   a change is an amendment of this ADR. An invariant without an ID is cited by
   the code boundary that enforces or should enforce it, with the invariant
   named in the commit body; the fixer never edits the invariant file to mint
   an ID.
4. **Renames: a bridge record, two accepted misses.** An identifier is the
   boundary's path (and symbol) at the commit that records it, and records are
   never rewritten. A fixer who knows the boundary moved adds the earlier token
   as one more `Prevention-Concept` line, making the new record a bridge. A
   query by the earlier token finds the earlier records and the bridge; by the
   current token alone, the bridge and later records only; by both tokens in
   one `CONCEPT`, every record on either side. Nothing tracks renames
   mechanically, so two misses are accepted: a move nobody recorded in a
   bridge, and a lookup that passes only the current token without reading the
   bridge's earlier one. Both are bounded by FR4's 14-day window (only a move
   between two fixes inside it exposes FR4). An invariant ID, stable across
   renames, is preferred wherever an invariant covers the shape.
5. **Query form.** Two shell one-liners in the document, a period query and a
   concept query: `git log` trailer formatting piped to one `awk` filter. A
   commit is listed when its subject has type `fix` (`fix:`,
   `fix(<scope>):`, `fix!:`, `fix(<scope>)!:`); there is no `--grep`, so no
   `grep.*` setting changes the result and lines git adds for settings such as
   `log.showSignature` fail the filter. A fix is listed by concept when any of
   its concepts equals, byte for byte, any identifier in `CONCEPT`. Output is
   seven TAB-separated fields, newest first; only the final trailer block is
   read. Minimum tools: git 2.22.0 and a POSIX `awk`. The document is the one
   copy of the query text; the proof executes it as written.
6. **Enforcement point.** The multi-agent submit gate checks every doer
   submission's commit range and refuses a fix commit whose record is missing
   or malformed, and any agent record with disposition `accepted`, as a
   correctable precondition refusal: no state transition, the author keeps the
   task. The refusal names the installed document and an agent-executable
   repair. The reviewer judges the substance of a well-formed record. In
   Pairing the record is part of the agent's definition of done.

## Consequences

- The vocabulary (keys, values, identifier forms) changes only by amendment of
  this ADR and of the document together; consumers cite the document's
  headings and restate nothing.
- The query reads only the final trailer block of `fix` commits. Empty record
  fields mean the final block holds no record, which is how omission is
  counted. A malformed record keeps whatever its final block holds (an unknown
  value, a misplaced owner, only the final block's concepts), so it is listed
  with populated fields; a human's fix that carries a valid record is listed
  like any other.
- Rename handling depends on the fixer's knowledge; the two misses above are
  accepted limits, not defects to be closed by tooling.
- The format needs git 2.22.0 or newer wherever the query runs.
- Human commits are indistinguishable from agent commits in a task range, so
  the gate treats every commit there as the agent's.

## Alternatives Considered

- **Prose in the commit body**: not countable without reading prose, and
  free-text concepts never match.
- **Git notes**: not carried by cherry-pick or squash by default, so the
  record would vanish exactly where history is rewritten.
- **A side file in the repository**: separates the answer from the change that
  made it and needs its own merge handling; it travels with a cherry-pick only
  when the file is part of the picked commit, and a shell command would have to
  parse the file to count records.
