# 31 - Configurable Post-Worktree Command

## Superseded In Part

ADR-0117 supersedes the failure-handling decision below. A configured
`post_worktree_cmd` that fails now fails closed rather than warning, and the
"command failures are silent (by design)" limitation no longer holds. The
configurable-hook mechanism, the trust model, and the stack-agnostic constraint
(G1.1) remain in force.

## Context and Problem Statement

Agent worktrees for Go projects failed builds because embedded assets (e.g., `internal/embedded/claude-settings.json`) were missing. The initial fix hardcoded `make sync-embedded` in `CreateWorktree()`, but this prevented Liza from working on another project than itself.

The problem surfaced when agent log analysis revealed ~7 wasted turns per session where agents tried to diagnose and work around the missing assets, often blocked by sandbox permissions.

## Considered Options

1. **Auto-detect build system** — unreliable across language ecosystems.
2. **Configurable hook** — user specifies the command at `liza init` time.

## Decision Outcome

Chose **Option 2**: replace the hardcoded call with a configurable `PostWorktreeCmd` field.

### Architecture

**Config schema** (`state.yaml`):
```yaml
config:
  post_worktree_cmd: "make sync-embedded"  # nil if not specified
```

**CLI surface:**
```
liza init --post-worktree-cmd "make sync-embedded"
```

**Execution model:**
- Command runs via `sh -c` in the worktree directory
- `RunPostWorktreeCmd()` is idempotent — safe on both new and existing worktrees
- Failures produce warnings, never block task claiming (permissive strategy) — **superseded by ADR-0117**
- Applied at all worktree creation points: direct creation, task claim, recovery, rejection reclaims

**Trust model:** Same boundary as Makefile/CI config — write access to `state.yaml` equals write access to the repository.

**Key files:**
- `internal/models/state.go` — `Config.PostWorktreeCmd *string`
- `internal/ops/wt_create.go` — `RunPostWorktreeCmd()` + hook calls
- `internal/ops/claim_task.go` — post-command after worktree provisioning
- `internal/agent/worktree_check.go` — reviewer worktree recovery

### Rationale

Agents should not waste turns on problems that can be solved deterministically via configuration. The permissive failure strategy (warn, don't block) reflects the current operational approach: let agents proceed and analyze logs in batches for systemic issues.

### Implementation Notes

The decision also triggered GUARDRAILS.md rule G1.1 (ADR-0032), which codifies the broader principle: no Liza-specific hardcoding in runtime code.

### Consequences

**Positive:**
- Liza becomes stack-agnostic — works for any language/framework
- Eliminates ~7 wasted agent turns per session on Go projects
- Single configuration point for project-specific worktree setup

**Limitations accepted:**
- User must know to set the flag at init time
- Command failures are silent (by design) — requires log analysis to detect — **superseded by ADR-0117**

## Amendment (2026-08-23)

The limitation "user must know to set the flag at init time" is now mitigated at
the source. When `--post-worktree-cmd` is absent and auto-detection cannot select
one command—because no Node layout exists or multiple Node projects are
present—`liza init` explains that task worktrees are fresh checkouts and asks for
confirmation before creating the workspace; declining cancels before any state is
written. Unambiguous Node layouts keep the existing suggestion prompt, `--yes`
auto-confirms, and scripted callers with no answerable input get the warning and
continue.

Implemented in `internal/commands/init.go` (`confirmMissingPostWorktreeCmd`).

## Amendment: runtime configuration (2026-09-12)

Operators can now use `config get config.post_worktree_cmd` and
`config set config.post_worktree_cmd "<command>"`; the general `get` query remains
supported. The key spelling is shared across reads and writes. Setting the command
does not execute it. Identical values are successful no-ops, while replacement of a
different value requires `--replace` and a reason. Comparison and mutation share one
locked transaction, with registration-generation fencing for identified agents and
no new capability granted to default roles. These conventions do not authenticate
human identity or introduce a new shell sandbox.

The existing post-merge Node detector already writes inside the MERGED transaction
and rechecks that the value is unset; operator configuration retains precedence.
No detector expansion, planner declarations, task fields, or prompt changes are
needed for this increment. Empty-repository init explains deferring configuration
until scaffolding is available without promising planner activation.

Audit records remain in the process log, including in JSON mode, to avoid extending
the blackboard schema. A greppable outcome and detector comparison help distinguish
custom workflows from missing conventional detection rules. Logs are not durable
state and must be retained externally for longitudinal evidence. The support
reference defines the validation procedure and this evidence's limits.

---
*Reconstructed from commits c2aba97, 0a53d76, cc20f98, 7f682dd (2026-02-27 to 2026-03-07)*
