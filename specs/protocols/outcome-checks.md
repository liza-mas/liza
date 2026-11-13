# Outcome Checks

`init` and `setup` report an outcome that is complete but not what was asked
for (a *degraded* outcome) as structured checks with a dedicated exit code.
Human warnings on stderr are unchanged. Decision record:
[ADR-0247](../architecture/ADR/0247-degraded-outcomes-as-checks.md).

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
Every check `init`, `setup` and `doctor` declare today is `warn`.

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
| `doctor` | `doctor.*` and `toolchain.<tool-id>` checks of the end state `setup` and `init` leave ([Doctor](#doctor)); exit 0, 4 or 1 like `init` |
| `refresh-project` | `refresh.asset` per release-owned project file: `ok` per file written, `failed` per hook script left alone (not a regular file) or hook config it cannot parse. In a repository `init` never prepared, no checks and exit 0. A hard failure's `error.message` names the path and the filesystem error |

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

## Doctor

`doctor` checks the end state `setup` and `init` leave, and changes nothing
([ADR-0248](../architecture/ADR/0248-doctor-verifies-setup-and-init.md)).
Neither `setup` nor `init` runs it: the user, an agent or a script does. It
reads the provider catalog cache without loading it, and it never calls
placement, repair or the Semble model query (which writes a temporary fixture).

| Check | When | Item | Severity |
|-------|------|------|----------|
| `doctor.provider-catalog` | always | the catalog cache: ok when it parses or is absent (the embedded catalog is used), `failed` when unreadable | `warn` |
| `doctor.global-file` | always | each contract file, each skill directory (all its files), `support-docs/`, `pipeline.yaml`: `missing` or `failed` (not a regular file) | `warn` |
| `doctor.skill-link` | each provider set up (skills linked into the global directory) or named | each release skill's link: `missing`, or `failed` when it points elsewhere or a file or directory holds it | `warn` |
| `doctor.provider-link` | same providers | each catalog extra link, as `doctor.skill-link` | `warn` |
| `toolchain.<tool-id>`, `doctor.toolchain-profile` | a toolchain profile is configured | each selected tool except manual-only ones; Semble without the model probe. An unreadable profile is one `doctor.toolchain-profile` check | `warn` |
| `doctor.global-contract` | `--provider` | the global contract location: `missing` when absent for a global-first provider (ok for a repo-first one), `failed` when occupied by a file or another link, or when it cannot be resolved | `warn` |
| `doctor.repo-contract` | `--provider` and `--repo` | a named provider with a repo contract file: active at the repo file, local fallback or global location, else `missing` | `warn` |
| `doctor.activation-state` | same | the provider activation state in the git directory: `missing`, or `failed` when untrusted | `warn` |
| `doctor.repo-asset` | same | each file a named provider's assets install (Claude settings and hooks, Codex config, hooks.json and hook scripts, Copilot hooks file and scripts, OpenCode exec tool and plugin, pi init gate) | `warn` |
| `doctor.workspace-state` | `--repo`, in a MAS workspace (MAS init's frozen `pipeline.yaml` or `models.yaml` is in the runtime directory; a pairing repository's runtime directory alone does not count) | `state.yaml`: `missing`, or `failed` when it cannot be read | `warn` |
| `doctor.index-hooks` | `--repo`, when the index plan the matching init uses is active (a MAS workspace without a readable state has no known plan and is not checked) | the index script, dispatcher and managed lifecycle hooks: `missing` names the first absent file | `warn` |

Checks are of presence and link targets, not content: a stale file or hook
set, and a prompt the user declined, both look installed or missing as they
are. Doctor writes no file itself; the toolchain probes run the user's
installed tools with `--version` and `--help`. A new `setup` or `init` step
that installs something adds its check here in the same change, reading its
path through the function the writer uses.

## Changing the format

Agents and scripts parse this output. A change to the fields, the severity
rule or the exit code is a breaking CLI change; the golden test fails until it
is made deliberately.
