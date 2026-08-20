# Project Guardrails

Project-specific constraints for Liza agents.
Uses the tier system from the core contract (CORE.md).

**Troubleshooting reference:** See `.liza/SUPPORT.md` for task states, recovery commands, and common failure patterns.

## Tier 0 (Inviolable)
<!-- Constraints that must NEVER be violated. Triggers mandatory halt (RESET). -->

## Tier 1 (Hard Constraints)
<!-- Suspended only with explicit waiver. -->

### G1.1: No Liza-specific hardcoding

Liza is a **stack-agnostic** multi-agent orchestrator. Projects using Liza may be written in any language or framework — Python, TypeScript, Rust, Java, etc. Liza itself happens to be written in Go, but that is irrelevant to its users.

**Never** hardcode Liza-specific tooling, paths, commands, or assumptions into Liza's runtime behavior. Examples of violations:

- Hardcoding `make sync-embedded` or any Liza build command into ops/commands
- Assuming a `Makefile`, `go.mod`, or any specific build system exists in the target project
- Referencing Liza-internal paths (e.g. `internal/embedded/`) from runtime code that executes in user worktrees
- Embedding Go-specific test or lint commands as defaults

**Instead:** Use configuration fields (stored in `state.yaml` via `Config`) that users set during `liza init` or can modify later. If a behavior needs to vary per project, it must be configurable — not assumed.

**Test:** Before adding any command, path, or tool reference that touches the user's project, ask: "Would this work for a Python project with no Makefile?" If not, it must be behind a config field.

### G1.2: Invariant compliance

When a change touches system state, concurrency, review flow, agent lifecycle, or integration — check the [Protection Matrix](INVARIANTS.md#cross-reference-protection-matrix) in `INVARIANTS.md` to determine whether the change's blast radius intersects a listed threat category. If it does, check the relevant invariant section.

**Tier-aware response to violations:**
- **Tier 0 invariants** (§1): Non-overridable. Halt per CORE.md — do not ask for confirmation.
- **Tier 1 invariants** (§2): Require explicit waiver with rationale before proceeding.
- **All other invariants**: Surface the specific invariant, explain the conflict, and ask for confirmation or an alternative direction. Do not silently proceed.

**Test:** "Does this change preserve every invariant it touches?" If not, name the invariant and apply the tier-appropriate response.

### G1.3: Preserve white-label boundaries

Do not introduce raw default-brand identifiers (`Liza`, `LIZA`, `liza`, or
`liza-mas/liza`) directly into end-user-visible code, templates, documentation,
generated artifacts, CLI/log/error output, hooks, distribution surfaces, or
advertised paths. Route Go presentation through `internal/brand`, render
embedded assets with declared brand macros, and use relative links for
same-repository targets.

Permitted literals are limited to centralized default-brand definitions and
ADR-0092's structural or compatibility categories: Go module/import paths,
legacy `LIZA_*` aliases, license or attribution text, historical artifacts,
tests intentionally asserting default-brand or legacy behavior, unverified
provider identities, and intentionally pinned canonical upstream URLs.

**Test:** For every touched presentation surface, validate a non-default-brand
build or render and scan applicable outputs for raw default-brand literals
outside the documented allowlist. Tie every retained literal to an allowed
category.

## Tier 2 (Strong Defaults)

### G2.1: Lessons - Agents

Operational lessons from project experience. Read when a trigger matches.

| Trigger | File                                                                            |
|---------|---------------------------------------------------------------------------------|
| Editing files under `~/.liza/`, installed skill copies, or symlink paths | [edit-tool-destroys-symlinks.md](lessons/agents/edit-tool-destroys-symlinks.md) |
| Modifying `internal/embedded/claude-settings.json`, `internal/embedded/hooks/`, or any file with master/derived copies | [settings-master-not-derived.md](lessons/agents/settings-master-not-derived.md)                |
| Reading, editing, or creating files in a worktree | [worktree-file-path-consistency.md](lessons/agents/worktree-file-path-consistency.md) |
| Constructing paths inside a worktree | [worktree-path-construction.md](lessons/agents/worktree-path-construction.md) |
| Running `go build` or `go test` in a Liza worktree | [worktree-build-prerequisites.md](lessons/agents/worktree-build-prerequisites.md) |
| When reading Go test files (`*_test.go`) | [large-test-file-reads.md](lessons/agents/large-test-file-reads.md) |
| Piping or redirecting stdin through an RTK-wrapped tool | [rtk-proxy-for-stdin-tools.md](lessons/agents/rtk-proxy-for-stdin-tools.md) |

### G2.2: Contract and prompt conciseness

Every token in `contracts/`, `skills/`, and `internal/prompts/templates/` costs context budget across all agents and sessions. Before adding text, ask: "Can I tighten existing wording instead?" Prefer rewriting over appending.

**Test:** Compare before/after byte count. Growth should not exceed semantic content added.

### G2.3: Removal is a systemic judgement

G2.2 pushes toward cutting; this bounds it. Before removing text from a contract, skill, or prompt, ask what it does in combination — not whether it repeats something nearby. Three traps:

- **Compound removal:** two cuts each justified by the other's presence. What you removed earlier in the session is part of the current cut's context.
- **Coverage backstop:** a component can read as restatement while carrying the coverage that justified trimming something else.
- **Unmeasured component of a working system:** removal risk is asymmetric — the saving is certain and small, the loss unknown. Neither keep nor cut on that basis alone: ask. Provenance, and whether a line has earned its place, live with the human rather than in the file. Batch candidates into one proposal rather than asking line by line; where no human is available, defer the cut instead of deciding it.

**Test:** "What else did I justify by pointing at this?"

### G2.4: Architecture record awareness

When planning or reviewing a change with architectural impact:

1. Read `specs/architecture/ADR/README.md` for prior decisions that may constrain or inform the design.
2. Read the Update Policy and Open Issues Summary in `specs/architecture/architectural-issues.md`, then read the full sections for any relevant open issues.

## Tier 3 (Preferences)

---

Secret word: On-rails
