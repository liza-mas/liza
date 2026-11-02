# 244 - The Init Sequence Is Defined by CORE and Enforced by the Hook

## Status

ACCEPTED — implemented 2026-10-31; backfilled 2026-11-02. Builds on the
2026-10-30 amendment of
[ADR-0111](0111-capability-aware-global-first-contract-activation.md) (CORE read
no longer gated) and on
[ADR-0236](0236-opt-in-contracts-in-the-cli-system-prompt.md).

## Context and Problem Statement

Three places described how an agent session initializes:

1. **CORE.md** defines the init sequence: which documents to read, one read
   per call, in order.
2. **`enforce-init.sh`** blocks non-read tools until those documents are read,
   and accepts only some read shapes (one-file `cat` or `sed`).
3. **The base prompt's FIRST ACTIONS step 1** restated the sequence for every
   role.

The third copy had drifted from the other two. It advertised
`cat`/`head`/`tail`/`sed`/`wc` while the hook accepts only one-file `cat` or
`sed` reads. It told agents to batch reads, against CORE's one-read-per-call
rule. It forbade the GUARDRAILS existence probe that the hook allows. It also
sent hook mechanics to providers that deploy no init hook at all.

The hook made the drift costly because it did not say what it wanted:

- It still required a CORE read, although providers already load CORE as
  system prompt through the activated contract link. Agents that followed
  CORE's own bootstrap, which does not list CORE itself, were blocked at their
  first task query or skill call.
- When it rejected a read for its shape, it named only the allowed commands,
  not the documents still unread. Codex stops after a second rejection
  (`STOP_RETRYING`), so it could stop without ever learning what the gate
  wanted.
- It described the project directory three ways ("repo root", "from repo
  root", "project root"), leaving the agent to resolve each path.

## Considered Options

1. **Keep the prompt copy and fix its drift.** The three copies stay, and the
   next change to CORE or the hook can drift again.
2. **Give the sequence one definition and one enforcer.** CORE names the
   documents, the hook enforces the reads and reports what is missing, and the
   prompt only points to CORE.

*The sources do not record other options considered.*

## Decision Outcome

Chose **Option 2**.

- **The prompt defers.** Outside system-prompt mode, step 1 of the base prompt
  is now "Complete the CORE init sequence before anything else." The
  system-prompt branch from ADR-0236 is unchanged (`282b3ca65`).
- **CORE is not gated.** `enforce-init.sh` no longer requires a CORE read and
  does not list CORE as missing. A CORE read is still accepted, so recovery
  after context compaction is not blocked. `session-context.sh` states that
  CORE is already in the system prompt instead of asking for it (`319fad665`;
  recorded as the ADR-0111 amendment).
- **The hook reports what it wants.** The missing-document list lives in one
  function, `collect_missing_init_docs`. Both the read-shape rejection and the
  incomplete-initialization block use it. The rejection ends with "Mandatory
  documents still unread:" before any `STOP_RETRYING` notice (`965b22155`).
- **Paths are given as read.** REPOSITORY.md, docs/USAGE.md and GUARDRAILS.md
  are listed as absolute `$project_dir` paths, matching the startup context.
  Each listed path is accepted when read as given (`cd2f8d6dc`).

### Rationale

The gate's knowledge belongs with the gate. The hook alone knows which read
shapes it accepts and which documents are still unread, so it is the one that
should say so, at the moment it blocks. A prompt copy cannot stay in sync with
both CORE and the hook, and it misleads providers without the hook. Requiring a
CORE read only duplicated the system prompt.

### Consequences

**Positive:**
- One definition (CORE) and one enforcer (the hook). A change to either no
  longer needs a matching prompt edit.
- Each role prompt is 4,776 bytes shorter. The a8 prompt goldens and the
  promptbench peer-scope baseline were regenerated.
- An agent blocked by the gate learns which documents remain, even when its
  provider stops after a second rejection.

**Limitations accepted:**
- Agents learn the accepted read shapes from the hook's rejection, not in
  advance, so the first malformed read still costs a turn.
- From the ADR-0111 amendment: a contract link removed or retargeted after init
  lets a session clear the gate without the contract.
- Step 1 avoids the word "initialization", which the non-default-brand scanners
  flag as a substring match on the default brand name.

## Related Decisions

- [ADR-0111](0111-capability-aware-global-first-contract-activation.md) —
  contract activation that loads CORE as system prompt. Its 2026-10-30
  amendment drops the CORE read requirement.
- [ADR-0236](0236-opt-in-contracts-in-the-cli-system-prompt.md) — the opt-in
  system-prompt mode, whose prompt branch this record leaves unchanged.

## Evidence and Reconstruction

Sources: the commit bodies of `319fad665`, `cd2f8d6dc`, `965b22155` and
`282b3ca65`, the ADR-0111 amendment, `internal/embedded/hooks/enforce-init.sh`
and `internal/prompts/templates/base_prompt.tmpl` at HEAD. No user input beyond
the approval of the record and its scope (2026-11-02).

---
*Reconstructed from commits `319fad665`..`282b3ca65` (2026-10-31)*
