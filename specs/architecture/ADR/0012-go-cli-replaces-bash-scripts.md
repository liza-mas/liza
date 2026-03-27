# 12 - Go CLI Replaces Bash Scripts

## Status

ACCEPTED — Supersedes ADR-0005. Changes enforcement mechanism of ADR-0011.

## Context and Problem Statement

Liza's orchestration layer used 18+ bash scripts in `scripts/` for all system mechanics: initialization, agent supervision, task lifecycle, worktree management, state locking, validation, and monitoring. While effective for the POC (ADR-0005), this approach had known limitations: limited error handling, no type checking on blackboard operations, manual testing only, and DRY violations across scripts.

## Considered Options

1. **Refactor bash scripts** — Fix DRY violations, add integration tests
2. **Python orchestrator** — Type-safe, testable, familiar (originally considered in ADR-0005)
3. **Go CLI binary** — Single binary, cobra subcommands, built-in locking and MCP server

## Decision Outcome

Chose **Option 3**: Replace all bash scripts with a single Go binary (`liza`) using cobra subcommands.

### Rationale

**Single binary deployment.** No runtime dependencies beyond the `liza` binary in PATH. Eliminates the bash/yq/flock dependency chain and the symlink deployment step (`~/.liza/scripts/`).

**Type-safe blackboard operations.** Go structs for state.yaml schema provide compile-time validation of field access. Eliminates the class of bugs where yq expressions silently produce wrong results.

**Structured commands.** Cobra subcommands (`liza init`, `liza validate`, `liza agent`, `liza wt-create`, etc.) replace positional-argument scripts. New commands (`liza pause`, `liza stop`, `liza status`, `liza get`) replace signal files with state fields.

**Built-in MCP server.** `liza-mcp` provides tool-based blackboard access for agents, eliminating shell-based tool calls.

**Internal locking.** Go's flock library replaces external `flock` calls. Locking is internal to the binary — agents no longer need `liza-lock.sh` wrapper.

### Command Mapping

| Old (Bash) | New (Go) |
|---|---|
| `scripts/liza-init.sh "goal" [spec]` | `liza init "goal" --spec spec` |
| `scripts/liza-validate.sh [state]` | `liza validate [state]` |
| `scripts/liza-lock.sh read\|write\|modify` | *(internal to binary)* |
| `scripts/liza-watch.sh` | `liza tui` |
| `scripts/liza-analyze.sh` | `liza analyze` |
| `scripts/liza-add-task.sh --id X ...` | `liza add-task --id X ...` |
| `scripts/liza-claim-task.sh <task> <agent>` | `liza claim-task <task> <agent>` |
| `scripts/liza-submit-for-review.sh <task> <sha>` | `liza submit-for-review <task> <sha>` |
| `scripts/liza-submit-verdict.sh <task> <V> [reason]` | `liza submit-verdict <task> <V> [reason]` |
| `scripts/wt-create.sh [--fresh] <task>` | `liza wt-create <task> [--fresh]` |
| `scripts/wt-delete.sh <task>` | `liza wt-delete <task>` |
| `scripts/wt-merge.sh <task>` | `liza wt-merge <task>` |
| `LIZA_AGENT_ID=x scripts/liza-agent.sh <role>` | `liza agent <role> --agent-id x` |
| `scripts/liza-checkpoint.sh` | `liza sprint-checkpoint` |
| `scripts/clear-stale-review-claims.sh` | `liza clear-stale-review-claims` |
| `scripts/release-claim.sh <task> [--role R]` | `liza release-claim <task> [--role R]` |
| `scripts/update-sprint-metrics.sh` | `liza update-sprint-metrics` |

New commands (no bash equivalent): `liza status`, `liza get`, `liza pause`, `liza stop`, `liza start`, `liza resume`, `liza mark-blocked`, `liza supersede-task`, `liza delete agent|task`, `liza version`.

Signal files → state fields: `.liza/PAUSE` → `config.mode: PAUSED` via `liza pause`, `.liza/ABORT` → `config.mode: STOPPED` via `liza stop`, `.liza/CHECKPOINT` → `sprint.status: CHECKPOINT` via `liza sprint-checkpoint`.

### Impact on ADR-0011

ADR-0011's principle — structural enforcement of agent status transitions — is preserved. The enforcement mechanism changes from bash scripts to Go CLI commands. Each command that modifies task state still atomically sets the acting agent's status.

### Supervision Model

The Go CLI preserves the bash scripts' action responsibility model: the supervisor guarantees infrastructure actions (registration, claiming, merging, heartbeat); MCP tools handle agent-initiated workflow actions (submit for review, verdicts, handoff). MCP tools also provide manual fallback for supervisor actions but don't shift responsibility. See [Supervision Model](../supervision-model.md) for the full matrix.

### Consequences

**Positive:**
- Single binary, zero runtime dependencies
- Type-safe blackboard operations
- Built-in MCP server for agent tool access
- Structured commands with help text and validation
- Internal locking eliminates wrapper scripts
- Testable in Go (unit + integration)

**Trade-offs:**
- Requires Go toolchain for development (not for deployment)
- Binary must be built and distributed (vs scripts that just need copying)
- `liza handoff` command not yet implemented (data model exists)

### Pending

- `liza handoff <task> <summary> <next>` — data model exists (`HandoffNote` struct, `handoff_pending` field) but no CLI command wraps it yet

---
*Decision date: 2026-02-15*
