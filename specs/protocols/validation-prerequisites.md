# Validation Session Prerequisites

`validation_prerequisites` declares the cheap checks an assigned session must
pass before it can execute a task's canonical `validation` commands. Worktree
setup success and a live registration alone do not establish that capability.
The canonical commands still run during task validation; preflight does not
replace them or change their expected results.

## Task contract

Tasks and downstream `output[]` entries accept the same optional structure,
including YAML task files and JSON task/output input:

```yaml
validation:
  - .venv/bin/python -m pytest tests/database
validation_prerequisites:
  - command: .venv/bin/python -m pytest tests/database
    env: [DATABASE_URL]
    executables: [.venv/bin/python]
    probes:
      - [.venv/bin/python, -c, "import pytest, psycopg"]
```

This is a project-specific example, not a built-in Python requirement. Declare
the actual interpreter, packages and tools used by each canonical command.
Keep credentials out of declarations: `env` contains variable names only, and
probe argv must not contain secret values.

When prerequisites are present, every canonical command must be unique and have
exactly one nonempty declaration. `command` matches the full command text,
including whitespace; it is not a list index or a shell expression to infer.
Reordering commands preserves the association. Editing one without updating its
declaration fails validation. Tasks without declarations retain the existing
validation workflow. Generated child tasks receive independent copies of their
output entry's declarations.

| Field | Meaning and limits |
|-------|--------------------|
| `command` | Exact canonical command; at most 64 commands/declarations, 4096 bytes per command. |
| `env` | At most 64 names per declaration; identifiers matching `[A-Za-z_][A-Za-z0-9_]*`, at most 256 bytes. Each value must be present and nonempty in the session. |
| `executables` | At most 64 names or paths per declaration; lookup uses the session's PATH and task worktree, including worktree-relative paths. |
| `probes` | At most 64 argv vectors per declaration, each with 1–32 arguments. Executed directly, without implicit shell splitting. |
| String and total bounds | Executable/argv strings are at most 4096 bytes and cannot contain NUL. Executable names and argv[0] cannot be blank. All declaration strings together are limited to 65536 bytes. |

An empty non-executable argument is valid. Probes must be cheap, safe to repeat,
and scoped to prerequisite checks rather than full suites. Each probe has a
10-second deadline; all checks together have a 30-second deadline. Probe stdout
and stderr are discarded at the process boundary, including failures.

## Execution context

The selected project `config.agent_tools.<tool>.validation_execution` must be
explicitly `local` for a declared task. This is the operator's assertion that
the provider's validation tools use the supplied local environment and worktree.
It does not change provider permissions or make a sandbox/remote wrapper local.
Unset or unsupported policies fail closed. `artifact-only` also fails closed:
signed validation artifacts are deferred and cannot authorize execution.

The supervisor resolves one effective environment snapshot for each attempt and
passes that snapshot to preflight and provider subprocesses. Executable lookup
uses its PATH, not the supervisor's ambient PATH. Probes run in the task worktree;
the provider retains its configured project-root startup directory. Configured
env-file overlays refresh for CLI and ACPX launches, including interactive runs.
Explicitly configured files that cannot be read fail the attempt. Catalog defaults
may be absent, even when a fetched catalog differs from the embedded version;
other read errors still fail the attempt. Empty overlays are valid files but
cannot satisfy a missing required variable. The existing KEY=VALUE format does
not perform shell expansion or quote interpretation.

These overlay/read-error rules also apply to launches of undeclared tasks.
Changing a setup command or merging dependencies does not update a running
supervisor's inherited environment. Filesystem repairs can become visible to
that process; inherited environment changes require a refreshed overlay or a
new supervisor/session followed by revalidation.

Protected ACPX session reuse is scoped to registration generation, task/commit,
contract, integration revision and the current process's environment identity.
A changed context or supervisor restart selects a new scope.

## Assignment, launch and recovery

Fresh claims, owned/handoff resumes, reviewer claims and await-based reclaim
must preflight before granting executable ownership. A reviewer waiting for
resubmission may retain a passive reservation, but must check the newly submitted
commit before returning to executable review. The final provider launch gate
checks again. No successful record is a reusable launch or reclaim permit.

Probes run outside the blackboard lock. The final transaction revalidates the
registration generation, task contract, worktree commit and integration revision;
a changed identity discards the observation. Registration and provider start keep
the existing lifecycle-lock ordering. Failed checks preserve worktree/output and
review evidence, release executable ownership coherently, and do not consume a
coding or review iteration as an implementation failure.

Preservation does not bypass the existing clean-worktree requirement for an
initial-state reclaim. If released doer work is dirty, commit the preserved
changes through the normal recovery workflow before retrying that claim;
preflight does not discard or silently commit them.

An ops command inside a current provider session can check its actual caller
environment with its branded agent ID and generation. This is the existing
trusted-agent model, not cryptographic process identity: a caller can forge its
own environment. An operator cannot prove another registration's environment.
For declared tasks, unblock without `--assign-to` and let the target supervisor
claim and preflight the restored task.

## Evidence and remediation

`state.validation_readiness[agent_id][task_id]` retains the latest sanitized
observation: generation, task ID, actual worktree HEAD (`commit`), separate
task review metadata (`review_commit` when present), integration SHA,
command/prerequisite digest, opaque environment fingerprint, timestamp, direct
method, result and fixed diagnostic code with check indices or a variable name.
It stores no raw environment, probe output or underlying process error.

The fingerprint is an HMAC with a random process-private key. Neither key nor
comparison domain is exported to children as authority. Fingerprints are
comparable only in their producing process; persisted or post-restart records
are audit evidence, never proof that another process has the same environment.

A recent failed registration is excluded for that task, without declaring its
whole role unusable. Automatic selection waits at least 60 seconds before probing
the same failed context again. The comparison includes project/worktree,
registration, tool configuration, command contract, commits and the producing
process's environment identity. Changed context permits a fresh attempt; stale
failure reporting expires after the retry interval. Final launch and authenticated
in-session ops always check again, including during that selection cooldown.
Pool repair reports unavailable or unverified validation capacity separately
from an absent role, preserving reviewer eligibility and diversity rules rather
than repeatedly spawning equivalent failed contexts.

Use `repair-agent-pool --dry-run` (or JSON output) to inspect capacity, then repair
the named prerequisite in the intended execution context. For `environment_missing`,
provide the named variable without printing its value. For `executable_missing`
or `probe_failed`, check the selected PATH/interpreter and the project's existing
dependency setup. `env_file_unavailable` identifies the configured entry index.
`execution_policy_required` requires a truthful execution policy;
`artifact_unsupported` cannot be bypassed with an artifact in this version.
Retry from the repaired session and let the fresh checks establish readiness.

## Related documents

- [Declared validation decision](../architecture/ADR/0072-declared-validation-commands.md)
- [Session prerequisite decision](../architecture/ADR/0136-validation-session-prerequisites.md)
- [Blackboard schema](../architecture/blackboard-schema.md)
- [Configuration reference](../../support-docs/CONFIGURATION.md#validation-execution-prerequisites)
- [Deferred signed-artifact support](../../TECH_DEBT.md#signed-validation-artifacts-for-unavailable-execution-contexts)
