# ADR-0236: Opt-In Contracts in the CLI System Prompt

## Status

ACCEPTED — 2026-10-25. Amends [ADR-0111](0111-capability-aware-global-first-contract-activation.md)
(the init gate no longer requires the CORE read for one opted-in launch path).

## Context

Every lane agent's bootstrap prompt starts with its own Agent ID, so two coder
prompts share a median 44-byte prefix (DEV-760 lane, 2026-10-25). The init gate
(`enforce-init.sh`) then forces each session to read CORE.md, the mode annex,
AGENT_TOOLS.md and GUARDRAILS.md with about 7 tool calls: ≈ 76 KB ≈ 19k tokens.
Those reads come after the agent-specific bytes, so the next agent cannot reuse
them from the provider prompt cache, and each call re-sends the whole context.
These init turns were ≈ 16 % of Claude's weighted cost on that lane.

Claude (`--append-system-prompt-file`) and Codex (`-c developer_instructions`)
both accept extra instructions in front of the first user message, where they
form a prefix that is the same for every agent.

## Decision

1. **Opt-in per project.** `use-system-prompt` builds the contracts verbatim
   into `.liza/system-prompts/{multi_agent,pairing,subagent}.md` (no timestamps
   or agent IDs) and writes a marker. `--revert` removes both. Without the
   marker, launches, the bootstrap prompt and the hooks behave exactly as
   before.
2. **Injected launches only.** In an opted-in project, a lane session (agent
   ID set) on the direct `claude` CLI, or on `codex` outside Windows, gets
   `multi_agent.md` in its system prompt. The file is rebuilt at launch when a
   source changed. Codex carries it as one argv string, and the launch fails
   with a clear error at the Linux 128 KiB argument limit. Windows caps the
   whole command line at 32,767 characters, so Codex is not injected there.
3. **Env-gated bypass, hook configuration unchanged.** Only a launch that injected the
   contracts sets `LIZA_SYSTEM_PROMPT_CONTRACTS=1`; every other launch removes
   an inherited value. `enforce-init.sh` exits 0 on it, and `session-context.sh`
   replaces its read instruction with "the contracts are in your system
   prompt". Hook configuration is not edited, so ACPX, other providers, Codex
   on Windows and pairing sessions keep the forced reads. Installed copies of
   those two scripts from an older binary are refreshed on enable, because
   they would still gate injected sessions. A pairing user may set
   the variable themself when they launch with `pairing.md`.
4. **Same weight as read files.** The built file's header says the contracts
   carry the weight of files read at session start and do not take precedence
   over the user's own CLAUDE.md/AGENTS.md. Without it, Codex let the contracts
   outrank the user's rules.

## Consequences

- **Trust assumption:** the variable is a claim that the contracts are already
  in the system prompt. Anything that can set an agent's environment can skip
  the gate. That is the same trust the launcher already holds over the agent's
  arguments and prompt. Agents inherit the variable in their shell, so tests
  that run the hooks must build their environment without it.
- **Measured (A/B, both CLIs, blind LLM judge):** the injected arm scores at
  least as well as the forced-read arm on every contract probe. A second
  Claude agent reads ≈ 26k more tokens from cache. On a small real task, cost
  drops ≈ 40 % for Claude and input tokens ≈ 46 % for Codex. On long lane
  sessions the fixed init cost is a smaller share, an estimated 8–15 %.
- **Default mode stays forced reads.** Making injection the default is a
  separate decision. It needs measurements on long lanes and on the providers
  that keep their own gates (pi, copilot, opencode).
- Where a provider link already loads CORE.md through instruction discovery
  (ADR-0111), CORE.md is in context twice, as it already was with the forced
  reads. Dropping it from the built file in that case has not been measured.
- Rollback: `use-system-prompt --revert`.
