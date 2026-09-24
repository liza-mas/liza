# ADR-0164: Managed Codex Keeps the Workspace-Write Sandbox; the Hook Honors the Configured Mode

## Status

ACCEPTED — implemented 2026-09-24 as part of the tool-result budget change
(DEV-778). The maintained-ACP outcome (degrade rather than escalate) was chosen
by the maintainer on 2026-09-24 in response to review round 3.

## Context and Problem Statement

The agent-visible tool-result boundary rewrites Codex's `Bash` tool into a
managed runner so oversized outputs can be budgeted, deduplicated, and
sanitized without discarding evidence. The first implementation of that
boundary passed `--dangerously-bypass-approvals-and-sandbox` on every managed
non-interactive Codex launch, which disabled the OS-enforced workspace sandbox.

That contradicts the repository's deliberate posture. ADR-0124 records that
the Codex unattended baseline keeps managed approval, a workspace sandbox,
network, and writable-root settings. The embedded rendered config
(`internal/embedded/codex-config.toml`) sets `approval_policy = "never"` with
`sandbox_mode = "workspace-write"`, and the release notes describe replacing a
launch-time bypass workaround with `--full-auto` specifically *to keep the
OS-enforced sandbox active*. The support documentation also stated that the
engine "does not pass launch-time permission overrides", which the bypass flag
made false.

Additionally, the hook previously required `permission_mode ==
"bypassPermissions"` as a launch-flag artifact. With the bypass flag removed,
that same mode is still what the managed configuration renders:
`approval_policy = "never"` reports `bypassPermissions` to the hook, while the
config keeps `sandbox_mode = "workspace-write"`.

## Considered Options

1. **Keep the launch-time bypass flag** — simplest, but disables the OS sandbox
   and contradicts ADR-0124, the embedded config, the release notes, and the
   support documentation.
2. **Remove the flag and honor the configured mode at the hook.** The hook
   accepts exactly the mode the managed config renders
   (`bypassPermissions`) and denies every other mode, so a constrained or
   foreign session is never rewritten (and therefore never elevated). Launch
   carries no permission override, so the workspace-write sandbox stays active.
3. **Remove the flag but let the hook rewrite in any mode** — would elevate
   constrained sessions by rewriting their Bash into a bypass-capable runner.

## Decision Outcome

**Option 2.** `prepareCodexToolResultLaunch` no longer passes
`--dangerously-bypass-approvals-and-sandbox`. The managed launch keeps only
`--dangerously-bypass-hook-trust` on the direct CLI path, which is orthogonal:
it skips hash verification of the engine's own generated session hooks. It
touches neither approvals nor the sandbox.

`CodexHook` accepts a `PreToolUse` rewrite **only** for `permission_mode ==
"bypassPermissions"` — the exact mode the managed config reports — and denies
the rewrite for `default`, `acceptEdits`, `dontAsk`, `plan`, and empty modes.
The rewritten runner therefore executes under the configured workspace-write
sandbox, not under a bypass.

Where the managed posture cannot be expressed, the launch degrades to a native
launch without the tool-result boundary instead of escalating:

- **Maintained ACP adapter (`codex-acp`).** The adapter couples approval policy
  and sandbox inside its mode presets and always overrides the thread sandbox
  from the selected mode. No preset couples `approval_policy = never` with
  `sandbox_mode = workspace-write`: the workspace-write presets flip approval to
  on-request, where the hook denies every rewrite, and the only preset that
  pairs `never` with a sandbox setting is `agent-full-access`, which removes
  the workspace sandbox. An earlier revision of this change launched every
  managed `codex-acp` agent in `agent-full-access`; that escalation is
  rejected. `codex-acp` launches exactly as it did before the boundary existed
  (adapter default mode, `acpx --approve-all`), keeps its sandbox, and its
  tool results are not budgeted.
- **Windows direct Codex.** The boundary's Bash rewrite requires a POSIX shell
  wrapper that Windows lacks, so the direct path launches Codex natively.

## Rationale

The OS-enforced sandbox is the base of the managed posture; the tool-result
boundary must not trade it away to gain output budgeting. Gating the rewrite on
the exact mode the managed config produces keeps the boundary sound without
weakening the sandbox, and rejecting any other mode prevents the boundary from
becoming an elevation primitive. Where an adapter's own mode interface cannot
express the managed posture, the boundary is dropped for that adapter: a lost
context budget is recoverable, a lost sandbox is not.

## Consequences

- Managed direct Codex runs under `sandbox_mode = "workspace-write"` with
  `approval_policy = "never"`; the hook sees `bypassPermissions` and rewrites
  Bash into the managed runner.
- Any Codex session that is not the managed baseline (constrained approval
  modes, plugin-supplied modes, empty modes) is denied the rewrite, so such a
  session runs without the output boundary rather than elevated.
- Managed `codex-acp` runs in the adapter's default mode with its sandbox,
  without the tool-result boundary; oversize outputs are not budgeted there.
  Revisit if the adapter gains a preset (or a hook contract) that pairs
  non-interactive approval with the workspace-write sandbox.
- Windows direct launches run Codex natively without output capture; oversize
  outputs are not budgeted there.
- `support-docs/CONFIGURATION.md` states the truth for every Codex transport:
  the engine passes no launch-time permission override, flag or environment.

## Related Decisions and Provenance

Extends [ADR-0124](0124-user-owned-provider-preferences-and-explicit-mas-launch-policy.md),
whose "Codex unattended baseline" row is the source of the sandbox posture.
Recorded with the tool-result budget change, ported from a downstream fork
(DEV-778, downstream PR 84), where the sandbox fix and the ACP degrade landed
in review rounds before the squash. The ACP adapter behavior and the
Codex hook `permission_mode` mapping were verified against the adapter's
`AgentMode`/`CodexAcpClient` source and `codex-rs` `hook_runtime.rs` /
`cli/src/main.rs`.

---
*Recorded 2026-09-24 downstream; renumbered from downstream ADR 158 when ported.*
