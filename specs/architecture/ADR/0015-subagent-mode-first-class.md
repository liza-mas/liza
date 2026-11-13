# 15 - Subagent Mode as First-Class Contract Mode

## Context and Problem Statement

Subagent Mode was embedded as a section in PAIRING_MODE.md, but subagents aren't pairing-specific — any Task-spawned agent operates as a subagent regardless of parent mode. A Liza (MAS) agent spawning a subagent loaded the same behavioral contract, which was designed for a different context.

The full contract is overkill for subagents. They're typically read-only (research, analysis), don't interact with humans, and shouldn't perform tiered context recovery. Loading the full Pairing or MAS contract wastes context budget on irrelevant sections — greeting protocols, collaboration modes, approval semantics — that a research subagent will never use.

## Considered Options

1. **Keep as Pairing Mode subsection** — Minimal contract surface, but wrong home and wrong context cost
2. **Conditional sections in existing modes** — "If subagent, skip sections X, Y, Z"
3. **First-class contract mode** — Lightweight SUBAGENT_MODE.md with its own Mode Selection Gate entry

## Decision Outcome

Chose **Option 3**: Extract to SUBAGENT_MODE.md as the third contract mode, detected via `MODE: SUBAGENT` marker in the Task tool prompt.

With the dual-mode architecture established (ADR-0004) and tiered context degradation in place (ADR-0014), adding a third mode was a natural extension of existing patterns.

### Architecture

**Mode Selection Gate (updated):**

| Detection | Mode | Action |
|-----------|------|--------|
| First prompt contains "You are a Liza ... agent" | **Liza** | Read MULTI_AGENT_MODE.md |
| First prompt contains `MODE: SUBAGENT` | **Subagent** | Read SUBAGENT_MODE.md |
| Otherwise | **Pairing** (default) | Read PAIRING_MODE.md |

**What Subagent Mode strips:**
- No user interaction (caller is interface, not human)
- No greeting or mental model ceremony
- No approval gates (internal ceremony only — Intent Gate, DoR/DoD remain)
- No tiered context recovery (return partial results instead)
- No state-modifying actions

**What Subagent Mode keeps:**
- All Tier 0 invariants
- Security Protocol
- Scope discipline
- Uncertainty reporting
- Anti-deception rules

**Task Tool Rule** (added to CORE.md): All agents spawned via Task tool are subagents. Include `MODE: SUBAGENT` in every Task tool prompt.

### Rationale

Context usage is the primary concern. The specialization into distinct, complementary contracts loaded on demand means each agent type pays only for the contract surface it needs. Subagents are typically read-only research agents — the full approval ceremony, collaboration modes, and recovery protocols are context weight with no return.

The generic-subagent skill already defined subagent behavioral expectations. Extracting Subagent Mode aligns the contract architecture with the skill's implicit assumption that subagents operate differently.

### Consequences

**Positive:**
- Subagents load CORE + ~60 lines instead of CORE + ~300 (Pairing) or CORE + ~500 (MAM)
- Mode architecture now covers all three agent types in the system
- Context budget freed for actual research work
- Clean separation: subagent behavioral constraints live in the contract, delegation methodology lives in the skill

**Limitations accepted:**
- Three modes to maintain instead of two (but subagent mode is deliberately minimal)
- `MODE: SUBAGENT` marker must be included in every Task tool prompt — structural enforcement via convention, not tooling

---
*Reconstructed from commits c7c4554, 7c24e33, b8f7faf (2026-02-15)*

## Amendment (2026-11-13): Claude subagents initialize under their own gate (DEV-954)

Observed on Claude Code 2.1.322: a subagent gets no SessionStart, does not
inherit `--append-system-prompt-file`, and makes its tool calls under the
parent's `session_id`, so it inherited the parent's cleared init gate (and, in an
injected session, the variable that waives it) without reading SUBAGENT_MODE.md.
Explore, Plan and custom agents with `omitClaudeMd` also load no CLAUDE.md, so
they lack CORE. A fork reuses its parent's prompt.

- `SubagentStart` runs `session-context.sh`, which tells the subagent to
  initialize as a subagent. The text stays under 1 KB: Claude shows a subagent
  only a preview of a larger context.
- `enforce-init.sh` gives each subagent (a call carrying `agent_id`) its own
  state. Before any state-changing tool it requires CORE.md (a hook cannot tell
  whether CLAUDE.md loaded), SUBAGENT_MODE.md (no other mode file counts),
  AGENT_TOOLS.md and GUARDRAILS.md. Read, Grep, Glob, WebFetch, WebSearch,
  ToolSearch and `SubagentHandback` work before that, and Bash only for the
  recognized document reads. Accepted: a read-only subagent may research and
  return without initializing; a read-only agent without CLAUDE.md, such as
  Explore, then works without CORE.
- A headless (`-p`) Claude lane launched with injected contracts (ADR-0236)
  also gets `--append-subagent-system-prompt-file subagent.md` (SUBAGENT_MODE.md,
  AGENT_TOOLS.md and the project GUARDRAILS.md) and
  `LIZA_SUBAGENT_SYSTEM_PROMPT_CONTRACTS=1`; its subagents then need only the
  CORE.md read. Claude documents that flag for `-p` runs only, so an interactive
  launch keeps the reads. A fork ignores the variable, since it never receives
  the file. Every other launch removes an inherited value.
- A top-level session keeps accepting any mode file: CORE picks the mode from
  the first prompt, which a hook cannot see, and a CLI session started with a
  `MODE: SUBAGENT` brief runs in Subagent mode.

Codex and Copilot subagents are unchanged: their behavior was not observed.
