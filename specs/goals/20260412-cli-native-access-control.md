# CLI-Native Access Control

## Problem Statement

Liza exposes agent operations through two parallel surfaces: a CLI (`liza`) and an MCP server (`liza-mcp`). The MCP server was introduced partly for role-based access control (RBAC) — preventing agents from calling operations outside their role's allowed-operations list.

In practice, the MCP RBAC provides no real security boundary:

1. **Self-declared identity.** The agent passes its own `agent_id` as a parameter in every MCP tool call. The role is extracted from the ID by string parsing (`coder-1` -> `coder`). There is no authentication — any agent can claim any identity.
2. **CLI bypass.** When MCP is unavailable (memory pressure, server crash, Codex compatibility issues), agents fall back to CLI commands that have no RBAC checks. The security guarantee is only as strong as the weakest access path.
3. **Prompt-compliance equivalence.** Both MCP RBAC and "only use these CLI commands" prompt instructions depend on the same thing: the agent following its prompt faithfully. The middleware adds a second check for the same class of errors the prompt already prevents.

Meanwhile, MCP imposes concrete, recurring costs:

- **Token overhead.** Every tool schema (name, description, input schema with types/enums/descriptions) is loaded into every agent's context. ~30 tools x schema size = significant context budget consumed before the agent does any work.
- **Reliability.** Under memory pressure, MCP servers become unresponsive. Agents stall waiting for responses.
- **External dependency risk.** Codex broke custom MCP support without annotations [openai/codex#16685](https://github.com/openai/codex/issues/16685). Liza had to add annotations to every tool definition as a workaround.
- **Operational burden.** CLI fallback instructions must be injected into every agent prompt to handle MCP unavailability.
- **Two-surface maintenance.** CLI commands and MCP handlers are parallel surfaces over the same ops layer. Both carry validation logic, defaults, descriptions, and error messages. Changes must be reconciled across both (lesson: `cli-mcp-surface-sync.md`).

## Solution Overview

Move the two capabilities that MCP provides beyond raw CLI to the CLI itself, then remove the MCP server.

### 1. CLI RBAC via `--agent-id` validation

Add role validation to state-mutating commands before calling the ops layer. Read-only and human-only commands are excluded. The `identity.ExtractRole()` function and `pipeline.Resolver` already exist. The CLI just doesn't call them.

**Identity source taxonomy.** CLI commands use three different identity patterns today:

| Pattern | Source | Commands | RBAC treatment |
|---|---|---|---|
| Positional `<agent-id>` | `cobra.ExactArgs(2)` | `claim-task` | Validate role from positional arg |
| `--agent-id` flag | `requireAgentID(cmd)` | `submit-for-review`, `handoff`, `submit-verdict`, `await-verdict`, `await-resubmission`, `mark-blocked`, `write-checkpoint`, `set-task-output`, `wt-merge` | Validate role from flag |
| Auto-resolved orchestrator | `resolveOrchestratorID(cmd)` | `add-task`, `add-tasks`, `supersede-task`, `cancel-task`, `assess-blocked`, `assess-hypothesis-exhausted` | Validate resolved ID has orchestrator role |
| `--changed-by` (audit-only) | `resolveChangedBy(cmd)`, defaults to `"human"` | `pause`, `stop`, `start`, `resume`, `replan`, `release-claim` | **No RBAC** — these are human/supervisor operations |
| No identity | — | `get`, `status`, `validate`, `version`, `analyze`, `tui`, `init`, `wt-create`, `wt-delete`, `clear-stale-review-claims`, `sprint-checkpoint`, `update-sprint-metrics` | Read-only or human-only — no RBAC needed |

**Commands excluded from RBAC:**
- `--changed-by` commands: Human/supervisor operations (pause, stop, start, resume, replan, release-claim). Default `"human"` identity is audit trail, not access control.
- Read-only commands: `get`, `status`, `validate`, `version` — no state mutation beyond reads.
- Interactive/human commands: `init`, `tui`, `delete task` — require human presence.
- `wt-create`, `wt-delete`: Currently take no agent identity; RBAC would require adding `--agent-id`. Consider for v2.

**No syntax changes.** `claim-task` keeps its positional `<agent-id>`. The validation is internal — extract role from the identity source that already exists, check against pipeline resolver.

### 2. Structured output via `--json`

Add a `--json` flag to finite, non-interactive CLI commands. When set, output is JSON with typed error codes instead of human-formatted text. The ops layer already returns typed results (`ClaimTaskResult`, etc.) and typed errors (`NotFoundError`, `PreconditionError`, etc.). The CLI currently formats these for humans; `--json` serializes them directly.

**Scope of `--json`:**
- **Included** (finite request/response): `get`, `status`, `validate`, `version`, `claim-task`, `add-task`, `add-tasks`, `submit-for-review`, `submit-verdict`, `handoff`, `mark-blocked`, `release-claim`, `supersede-task`, `cancel-task`, `assess-blocked`, `assess-hypothesis-exhausted`, `write-checkpoint`, `set-task-output`, `set-discovery-disposition`, `wt-create`, `wt-delete`, `wt-merge`, `analyze`, `update-sprint-metrics`, `sprint-checkpoint`, `clear-stale-review-claims`, `await-verdict`, `await-resubmission`
- **Excluded** (interactive/streaming): `agent` (long-running supervisor), `tui`/`watch` (interactive dashboard), `init` (interactive setup with prompts), `proceed`, `replan`, `delete task` (interactive confirmation)

Error code mapping (reuse from MCP's `classifyError`):

| Error Type | Code | Meaning |
|---|---|---|
| `NotFoundError` | `not_found` | Resource doesn't exist |
| `PreconditionError` | `validation` | Wrong state, missing field |
| `PostWriteValidationError` | `validation` | Post-write check failed |
| `OperationalError` | `internal` | System-level failure |
| Lock timeout pattern | `lock_timeout` | Concurrent access |
| Race condition pattern | `race_condition` | State changed concurrently |

### 3. Remove MCP server and replace bootstrap path

Once CLI has RBAC and `--json`, the MCP server (`cmd/liza-mcp/`, `internal/mcp/`) provides no unique value. Remove it, along with:

- MCP tool schemas and handler code (`internal/mcp/`)
- MCP binary entrypoint (`cmd/liza-mcp/`)
- MCP-specific middleware (logging, role validation — now in CLI)
- MCP fallback instructions in agent prompts
- The `cli-mcp-surface-sync.md` lesson (single surface, no sync needed)

**Bootstrap replacement.** The following integration points currently depend on `liza-mcp` and need replacement:

| Integration point | Current behavior | Replacement |
|---|---|---|
| `internal/embedded/mcp.json` | Embedded MCP config written to `.mcp.json` | Remove embedded file. Remove `WriteMCPSettings()` call from `liza init`. Stop writing `.mcp.json` with Liza server entry. |
| `internal/commands/init.go:560` | `liza init` writes `.mcp.json` to project root | Remove the `WriteMCPSettings` call. If user has other MCP servers in `.mcp.json`, leave their config untouched. |
| `internal/ops/wt_create.go:128` | Copies `.mcp.json` into worktrees | Remove `.mcp.json` from the copy list (keep `.claude/settings.json` etc.) |
| `internal/agent/supervisor.go:373` | Codex launch passes `-c mcp_servers.liza.*` args | Remove MCP server args from `buildCodexArgs()`. Codex agents use CLI via Bash. |
| `internal/embedded/claude-settings.json:35-36` | `enableAllProjectMcpServers: true`, `enabledMcpjsonServers: ["liza"]` | Remove `enabledMcpjsonServers` entry. Keep `enableAllProjectMcpServers` if user has other MCP servers. |
| `internal/embedded/claude-settings.json:44-69` | Permission entries for `mcp__liza__*` tools | Replace with `Bash(liza:*)` permission (already present at line 97). Remove all `mcp__liza__*` entries. |
| `contracts/contract-activation.md` | Documents `mcp_servers.liza` configuration | Update to document CLI-based agent access. Remove `liza-mcp` references. |

**MCP resources.** `liza://state`, `liza://tasks`, `liza://agents` are read-only resources with direct CLI equivalents: `liza get tasks --json`, `liza get agents --json`, `liza status --json`. These are removed with no functionality loss.

## MVP Scope

### In scope

- `--agent-id` role validation on state-mutating CLI commands that already have an identity source (positional, `--agent-id`, or auto-resolved orchestrator — see identity taxonomy above). Commands using `--changed-by` and read-only commands are excluded.
- `--json` flag on finite, non-interactive CLI commands (see `--json` scope above)
- Removal of `cmd/liza-mcp/` binary and `internal/mcp/` package
- Update of `internal/embedded/mcp.json` to remove `liza-mcp` server entry
- Update of agent prompt templates to remove MCP fallback instructions and make CLI instructions more precise (exact commands, flags, expected output formats)
- Update of `AGENT_TOOLS.md` to remove Liza MCP tool references
- Update of all documentation referencing MCP or `liza-mcp` (`docs/`, `specs/`, `REPOSITORY.md`, `GUARDRAILS.md`, lessons, etc.)

### Out of scope

- Authenticated identity (tokens, secrets, process isolation) — same trust model as today
- Changes to non-Liza MCP servers (filesystem, JetBrains, perplexity, etc.)
- Changes to the ops layer — it stays unchanged
- CLI output formatting changes beyond adding `--json`

## Success Criteria

1. State-mutating commands with identity sources validate agent role before calling ops layer
2. `liza claim-task orchestrator-1 T1 --json` returns a structured error with code `validation` (orchestrator cannot claim tasks)
3. `liza claim-task coder-1 T1 --json` returns structured success
4. `liza-mcp` binary no longer exists; `internal/mcp/` package removed
5. Agent prompts contain no MCP fallback instructions; CLI instructions are precise (exact commands, flags, expected output)
6. Existing integration tests pass (ops layer unchanged)
7. No `mcp__liza__*` permission entries in embedded settings; no `.mcp.json` Liza server entry generated by `liza init`; no MCP args in Codex launch; byte count of Liza-generated prompt content decreases

## Risks and Assumptions

| Risk | Mitigation |
|---|---|
| Agents relied on MCP tool schemas for call construction | CLI `--help` and prompt instructions provide equivalent guidance; `--json` error messages are actionable |
| Logging/observability loss from MCP middleware removal | Add equivalent structured logging to CLI commands (tool name, duration, result) |
| Future need for true access control | This goal explicitly preserves the current trust model; real auth would require supervisor-injected credentials regardless of CLI vs MCP |

ASSUMPTION: No agent workflow depends on MCP-specific behavior beyond RBAC, structured responses, and resources. MCP resources (`liza://state`, `liza://tasks`, `liza://agents`) have direct CLI equivalents (`liza get` with `--json`). Verified: no agent prompt or supervisor code reads MCP resources — they use `liza get` or `liza status` already.
