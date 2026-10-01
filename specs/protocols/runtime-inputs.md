# Runtime Inputs

A live validation command may need an input that is spent when used (a
single-use fixture, such as a one-time enrolment code) or a credential that
must never enter an agent session. `runtime_inputs` declares those inputs on a
strict acceptance task. The operator records instances in a consumption ledger,
and the submission gate delivers them only to the commands that use them. This
protocol implements
[ADR-0169](../architecture/ADR/0169-runtime-input-provisioning.md).

## Declaration

Coding tasks and planning `output[]` entries accept the same optional list:

```yaml
validation:
  - sh tests/live/enrol.sh
runtime_inputs:
  - id: w03
    commands: [sh tests/live/enrol.sh]
    recipe: project.w03-fixture
    consumption: single_use      # or reusable
    env: [W03_FIXTURE]
  - id: principals
    commands: [sh tests/live/enrol.sh]
    recipe: project.member-credential
    consumption: reusable
    secret: true
    env: [MEMBER_CREDENTIAL]
    files: []                    # env names whose value is a file path
```

| Field | Rule |
|-------|------|
| `id` | `[a-z0-9][a-z0-9._-]*`, unique in the list. |
| `commands` | Exact members of the carrier's `validation`. |
| `recipe` | Dotted name defined in the registry at the integration commit. |
| `consumption` | `single_use` (spent by the gate run that uses it) or `reusable`. |
| `secret` | Values are masked everywhere the engine relays output. |
| `env` | Variable names, unique across the list. Process variables (`PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `PWD`, `TMPDIR`, `TEMP`, `TMP`, `TERM`, `LANG`, `TZ`, `LC_*`, `LD_*`, `DYLD_*`) and the engine's own branded and legacy variable namespaces are reserved. |
| `files` | Subset of `env` whose values are absolute paths to file artifacts. |
| `after` | Other ids this one depends on; acyclic. |

Any other key, including the deferred `binding`, is refused. Lists and strings
follow the `validation_prerequisites` bounds (64 items, 4096 bytes).

Admission refuses, as field-attributed `INVALID_INPUT`:

- runtime inputs on a task that is not a strict acceptance task: only its gate
  runs the canonical commands that consume them. `add-task` cannot create one;
  a planning output allocates them and a same-pair `replace-task` must keep
  them equal (they are part of the allocation predicate);
- an output unless its producer is a planning task and its consuming pairs
  generate coding tasks: architecture and main-plan outputs generate plans,
  and integration outputs generate coding tasks but only a planning parent can
  allocate a strict contract. A planning output whose child would not adopt a
  strict contract is refused as well;
- a recipe missing from the registry at integration, or no registry configured;
- a runtime-input `env` name that is also a `validation_prerequisites.env` name
  of the same carrier, a live task, or an output entry that can still generate
  children, in either order. Runtime-input names are a reserved class: every
  session launch strips them. `set-task-output` checks this inside the
  transaction that saves the output, where its names first enter the strip
  set, and `Modify` refuses any write introducing such a collision, whatever
  the writer, so concurrent declarations cannot both land.

## Registry

`config set config.runtime_input_registry <repo-relative path>` (operator-only;
a different value needs `--replace --reason`) names a committed YAML file:

```yaml
version: 1
recipes:
  project.w03-fixture:
    description: Issue one W03 enrolment fixture from the staging console.
```

Admission reads it at the integration commit, so a plan cannot declare a recipe
that only lands with the plan. In this version a recipe only has to exist; it
describes for the operator how an instance is produced. Planner and plan-review
prompts cover runtime inputs only when the key is set.

## Recording instances

The operator produces each instance by hand and records it:

```text
provision --record --task <id> [--task <id>...] --input <input-id> --file <envelope>
```

The envelope is an operator-owned `KEY=VALUE` file (at most 64 KiB) outside the
repository and its worktrees. It must define exactly the declaration's `env`
names. The parser refuses what the provider env-file format would silently
alter: surrounding whitespace, a carriage return, an inline ` #`, an empty value
or a repeated name. Secret values need at least 8 bytes, so masking cannot
shred ordinary output. `files` values must be absolute paths to regular files,
also outside the repository.

The instance identity is an HMAC-SHA256, under the operator key, of the
canonical materialization: the sorted names with their values, where a `files`
name contributes the SHA-256 of the file content instead of its path. Comments,
ordering and the file's location therefore do not change identity. Recording a
materialization already in the ledger returns it unchanged, so a consumed or
invalidated one never becomes available again. The only permitted change to an
existing instance is binding more tasks to a `reusable`, `available` one. A
`single_use` instance serves exactly one task. An `available` identity recorded
from a different envelope path is refused, naming the recorded path. Recording
the same identity as a different input is refused.

`provision` is operator-only: sessions with a branded agent ID are refused. As
for other operator commands, absence of an agent ID is not authentication.

### Operator key

The key is `runtime-input.key` in the global configuration directory under the
user's home, mode 0600, created on the first record while the ledger is empty.
It is never exported to child processes or written to state; instances record
only its `key_id`. While the ledger holds instances, a missing key, or one whose
`key_id` differs from theirs, fails closed at record, gate and readiness, since
a new key could not recognize spent materializations. Back the key up with your
secret sources and restore it to recover. The key is shared by every project
of the user: restore it before recording in any project, since the first
record in a project with an empty ledger creates a new key. Rotation is not available in this
version ([TECH_DEBT](../../TECH_DEBT.md#runtime-input-provisioning-should-set)).

## Ledger

`state.runtime_inputs` maps identity to an instance: recipe, input id,
consumption, secret flag, `key_id`, envelope path, variable names, bound tasks,
`state` (`available`, `consumed`, `invalidated`), registration time, and the
`consumed` (task, run id, agent, time) or `invalidated` (reason, time) record.
It stores no values and no unkeyed digest. Terminal instances are immutable;
every write goes through the blackboard, and `Modify` refuses an introduced
transition that changes a terminal instance, changes an available one's
fields other than bindings and state, removes one, or creates one in a state
other than `available` (the ADR-0165 introduced-only rule).

Cancel, supersede and integration recovery retire a task's bindings. A
`replace-task` whose replacement declares the input identically transfers the
binding. Otherwise an orphaned `single_use` instance becomes `invalidated`
(`task_retired`) and an orphaned `reusable` one stays `available`, unbound, for
re-recording.

## Gate acquisition

The strict gate (`submit-for-review` and `update-review-commit`) acquires inputs
after source, manifest and clean-worktree checks and immediately before the first
canonical command. For each declared input it selects the oldest bound
`available` instance whose envelope, read once, still verifies; an older one
whose artifact changed is invalidated and the next is tried. One
transaction then either records refusals or consumes every needed `single_use`
instance with a fresh run id. Consumption is therefore durable before any
command launches, and a run that fails or times out still spends its inputs.
Concurrent acquisitions of one instance have exactly one winner.

| Refusal | Meaning | Ledger write |
|---------|---------|--------------|
| `runtime_input_consumed:<id>` | The bound instance was spent. | none |
| `runtime_input_invalidated:<id>` | The instance was invalidated, including now: the envelope or a file artifact changed (`artifact_changed`). | invalidation |
| `runtime_input_missing:<id>` | No instance is bound. | none |
| `runtime_input_unavailable:<id>` | The envelope, a file artifact or the key cannot be read. | none |

A refusal runs no command, spends no review cycle and never marks
`INTEGRATION_FAILED`. It appends a `runtime_input_unavailable` anomaly (task,
input, code, operation, bound instance), deduplicated while the last one for
that task and input has the same code and bound instance, so recording a new
instance re-arms it. The circuit breaker does not count it: it is a
provisioning signal, not an agent failure.

Each command receives the caller environment with every reserved name removed,
plus only the variables of inputs whose `commands` list it. Secret values, and
their URL- and path-escaped forms, are masked in stored output, failure excerpts
and the archived receipt. A `files` artifact is verified before launch but
opened by the command itself; an operator-owned file changing during the run is
an accepted residual.

## Routing

- **Doer claim.** Claim checks that every input resolves (bound, available,
  readable, identity verified). A refusal blocks the task at once with reason
  `runtime_input_unavailable: <codes>` and a question naming the operator action:
  record an instance, then `unblock-task`. Until the operator records instances,
  BLOCKED is the normal first state of a runtime-input task.
- **Submission.** A gate refusal blocks the task through the `mark-blocked`
  core with the same text, after the gate's locks are released. If that write
  fails, the error tells the doer to stop and names the action.
- **Both blocks are human-owned.** The question, its refused inputs cut to
  whole codes within 1024 bytes plus a count of the rest, is recorded as the
  episode's `awaiting_human` ask, so the watch raises `AWAITING HUMAN` with it
  ([ADR-0172](../architecture/ADR/0172-human-owned-blocks.md)).
- **`update-review-commit`.** A refusal leaves the task state and receipt
  unchanged. When the caller is the task's claiming reviewer, the claim is
  released and the task returns to its submitted status.
- **Launch preflight.** Doers need every input to resolve; reviewers only the
  `reusable` ones, since the receipt proves the `single_use` runs. A failure is
  a preflight failure with the anomaly above.

Agent-facing messages name input ids and variable names, never envelope, file or
key paths.

## Session confinement

The deny set is every runtime-input `env` name declared by any task or output
entry, in any status, plus the names recorded on ledger instances. It is removed
at four boundaries:

1. the provider launch environment (names are logged, never values);
2. every gate command, before its authorized overlay;
3. `run-live`, before its overlay;
4. the copy of configured env files into a new worktree: a file mentioning a
   reserved name anywhere is not copied, and a warning names the variables.
   Shell syntax allows several assignments per line and multi-line values, so
   removing lines could leave a value behind.

An agent env file (`agent_tools.<cli>.env_files`) configures the agent CLI
itself. If it sets a runtime-input name, the launch fails with
`runtime_input_provider_collision:<NAME>` instead of stripping it silently: a
runtime input must not reuse the agent provider's own variable, such as its API
key. Remove the entry from the env file, or rename the runtime input.

A legacy `validation_prerequisites` entry that still requires a reserved name
fails preflight as `runtime_input_scrubbed`, naming the variable. An unrelated
gate that relied on an undeclared ambient variable loses it once that name is
declared as a runtime input anywhere.

## Local live runs

```text
run-live --task <id> [--timeout <seconds>] -- <command> [args...]
```

Runs one command without a shell, in the current directory, for the task's
assigned doer, its claiming reviewer or the operator. The environment is the
caller's, stripped of the deny set, plus the task's `reusable` inputs only:
`single_use` values never reach a local run, so no local run can spend one.
Running a canonical command that uses a `single_use` input (the joined argv, or
a `sh|bash|zsh|dash -c` wrapper) is refused before launch; reviewers rely on
the gate receipt for it. Output is captured (at most 1 MiB), masked and printed
after exit; the exit code is the command's. The timeout is 1 to 3600 seconds,
600 by default.

## Related documents

- [Acceptance evidence admission](acceptance-evidence.md)
- [Validation session prerequisites](validation-prerequisites.md)
- [Blackboard schema](../architecture/blackboard-schema.md)
- [Configuration reference](../../support-docs/CONFIGURATION.md)
