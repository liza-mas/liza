# Outcome Checks

`init` and `setup` report an outcome that is complete but not what was asked
for (a *degraded* outcome) as structured checks with a dedicated exit code.
Human warnings on stderr are unchanged. Decision record:
[ADR-0246](../architecture/ADR/0246-degraded-outcomes-as-checks.md).

## Format

With `--json`, stdout carries exactly one standard envelope
(`internal/jsonout.Envelope`, unchanged); everything else the command prints
goes to stderr. The result is a report:

```json
{"ok": true, "result": {"checks": [
  {"id": "contract.placement", "status": "failed", "severity": "warn",
   "provider": "claude", "path": "/home/u/.claude/CLAUDE.md",
   "message": "…", "fix": "move /home/u/.claude/CLAUDE.md aside, then re-run <binary> init"}
]}}
```

| Field | Meaning |
|-------|---------|
| `id` | Check definition, e.g. `contract.placement` |
| `status` | `ok`, `missing`, `failed` or `manual` |
| `severity` | `info`, `warn` or `error` (below) |
| `provider` | Provider the check concerns, when one does (optional) |
| `path` | Path the check concerns, when one does (optional) |
| `message` | What happened, in one sentence |
| `fix` | What the user can do (optional) |

Output never carries secrets or a tool's raw output: messages hold ids,
paths and error text from the filesystem only. The Go type is
`jsonout.Check`; `internal/jsonout/testdata/outcome-envelope.golden.json`
pins the wire format.

## Severity

An `ok` check is `info`. Any other status takes the severity its definition
declares, so one status can be a warning for one check and an error for
another. `warn`: the product works in a reduced way, such as a contract
active only for this repository. `error`: the product cannot work at all.
Every check `init` and `setup` declare today is `warn`.

## Outcomes and exit codes

| Outcome | Exit | Envelope |
|---------|------|----------|
| Success | 0 | `ok:true`, every check `ok` |
| Degraded | **4** | `ok:true`, at least one check not `ok` |
| Hard failure | 1 (unchanged) | `ok:false` with `error` |

Exit 4 applies with and without `--json`; in human mode a summary line lists
the non-ok checks on stderr after the warnings. A degraded outcome is always a
check, never a `warnings` string. Exit codes 2, 3 and 42 keep their meanings
(`internal/errors`).

## Producers

| Command | Checks |
|---------|--------|
| `init` (pairing and workspace) | `contract.placement` per contract provider; `init.asset` per asset it could not install (settings, hooks, policy, templates, bash-policy hooks) |
| `setup` | `setup.provider-link` per skill or extra link it could not create; `setup.global-file` per customizable file kept because the answer could not be read; `setup.asset` (pi init gate) |
| `toolchain configure --project --agents` | The activation's `init` checks, under `result.checks` beside the configuration; exit 4 when degraded |
| `toolchain doctor` | `toolchain.<tool-id>` per tool, all `warn`. Report-only: it keeps exit 0, because manual-only tools are never `ok` by design |

In-process callers read the same report through the optional `Report` field
of the init and setup params.

## What becomes a check

A warning becomes a check when the command did not apply something it was
asked to apply, and the user did not choose that. A user answering "no" to a
prompt is a choice, not degraded. Warnings that leave the request applied
stay stderr-only: legacy roots detected, both contract links active,
redundant links left in place, auto-detected index languages skipped,
untrusted activation metadata left unchanged.

`contract.placement` is derived from the final placement, not from the steps
that produced it, because a failed step can be recovered later (the local
fallback). The requested location is the provider's preferred one (global
when `prefer_global`, else the repo file), or the one an explicit conflict
action names: `rename` the repo file, `local` the local fallback, `global` the
global location. For a repo-first provider `global` resolves a conflict, so the
free repo file or the global location both count. `skip` accepts whatever is
active. A contract active anywhere else is `failed` with the requested path.
No active location at all stays a hard failure.

## Changing the format

Agents and scripts parse this output. A change to the fields, the severity
rule or the exit code is a breaking CLI change; the golden test fails until it
is made deliberately.
