# Acceptance Evidence

Use this with [Reference-First Authoring](reference-first-authoring.md). Source
References owns requirement meaning; the code plan allocates its stable obligation
IDs to coding children. A manifest maps that allocation to proofs. Successful
execution records evidence for review, without certifying test adequacy.

## Reviewed declaration

Within each coding child's exact `plan_ref` heading, add one eligible ATX heading
named `Acceptance Contract` containing one fenced JSON object:

```json
{
  "version": 1,
  "manifest": "acceptance/boundary.json",
  "obligations": ["AC-identity", "AC-atomicity", "AC-observation", "AC-replay"],
  "validation": ["project-test-command --acceptance"],
  "timeout_seconds": 600,
  "approved_proofs": []
}
```

Replace the illustrative command with the project's executable command. Every ID
must already exist in the document's strict `Source References` → `Obligation
Coverage`. Allocate the child's complete subset without copying requirement prose.
The declaration's ordered `validation` must equal the child's `output[].validation`
and eventual task `validation`. Name the child's own proofs plus shared tests
covering code it changes. Place each proof at the level the architecture's proof
topology sets for its scope (at `technical-spec`, the goal's); absent one, at the
cheapest level that fails on regression, with real resources only where the
behavior lives in or composes across them. Prove each obligation once; higher
levels prove composition. A long shared suite needed for regression only goes in
`validation` once with its zero-based index in `"deferred": [i]` (ascending):
admission skips it and the engine runs it in a periodic batch on integration,
bisecting a failure to the merge that caused it. At least one command must stay
undeferred, no mapping may name a deferred index, and a command a runtime input
uses cannot be deferred. Commands
obey the existing command-safety contract; the coder's manifest cannot add or
replace them. Whole-document refs are valid only when exactly one declaration is
present.

Ground `timeout_seconds` in one of two ways. Either use a measured run of the same
commands on this project, naming where and when it was measured, plus headroom for
load. Or use an estimate labelled as one, with its basis. Record the basis on the
`## Reality Probes` entries of the validation commands, summed for the batch. Never
record it inside the Acceptance Contract section, which holds only its fence.

Check the budget against the execution envelope the task supplies. Without one, check
it against the proof topology or time budget that the architecture or goal states.
Where no bound is stated, the basis alone is enough. A budget that exceeds a stated
bound is not settled by a guess: mark the task BLOCKED, naming the bound. A shared
suite that would dominate the budget is a candidate for deferral (above).

The direct planning parent must independently approve and merge this source, with
retained review range and output entry allocating the child's refs and commands.
Claim pins source commit/blob and parent review identity. Unreviewed ad-hoc strict
tasks must go through reviewed planning. Source corrections require that same
authority; removing an adopted declaration cannot turn a strict task into legacy.

## Committed manifest

Commit the manifest at the declared path, with exactly one mapping per allocated ID:

```json
{
  "version": 1,
  "mappings": [
    {"obligation_id": "AC-identity", "file": "tests/boundary.test", "assertion": "invalid identity and scalar cases", "command_index": 0},
    {"obligation_id": "AC-atomicity", "file": "tests/boundary.test", "assertion": "rejection leaves state unchanged", "command_index": 0},
    {"obligation_id": "AC-observation", "file": "tests/boundary.test", "assertion": "fresh observer sees committed result", "command_index": 0},
    {"obligation_id": "AC-replay", "file": "tests/boundary.test", "assertion": "both replay schedules use independent sessions", "command_index": 0}
  ]
}
```

`file` names a regular committed file; `assertion` identifies the executable
test/assertion for reviewer inspection; `command_index` is zero-based into the
canonical command list. Missing, extra and duplicate IDs fail. Every canonical
command not deferred runs, including commands that do not directly own a mapping.

For an obligation that cannot use executable proof, the independently reviewed
declaration must explicitly authorize an exception:

```json
{"obligation_id": "AC-external-audit", "reference_id": "audit-record", "rationale": "External signed audit cannot be reproduced by the test runner"}
```

Place that entry in `approved_proofs`, allocate its ID in `obligations`, and
declare `audit-record` among the source's pinned direct Markdown references with
an existing exact heading. Reviewers must approve the rationale and proof itself.
Its manifest mapping is exclusively:

```json
{"obligation_id": "AC-external-audit", "approved_reference_id": "audit-record"}
```

Do not mix executable and exception fields. A coder-supplied approver name or
free-form waiver does not authorize a proof exception.

## Trust-boundary test design

Adapt these scenarios to the referenced domain contract and stack. Use separate
IDs where failures have independent acceptance meaning; the table's IDs illustrate
an existing source allocation rather than runtime-reserved names.

| Obligation | Observable assertion | Evidence to inspect |
|---|---|---|
| AC-identity | Reject missing, null, wrong-kind, wrong-scope and malformed identities; include wrong scalar types and boundary values required by the source | Each negative case reaches the real trust boundary and reports the specified failure |
| AC-atomicity | An operation rejected after partial internal work leaves no externally visible mutation | Before/after state snapshots and an observer outside the failed transaction |
| AC-observation | A successful operation is visible after commit to a fresh independent observer | A committed result read by another connection/process; rollback-only fixtures do not establish persistence |
| AC-replay | Concurrent creation/replay yields the specified outcomes in both relevant orders | Separate sessions, deterministic barriers proving overlap at the contested operation, final committed state and both return values |

For a replay/write race, schedule A starts and reaches the contested write; B
attempts the same operation while A remains pending; then release A and observe B.
Reverse the relevant order as a separate scenario. A thread-start flag, sleep,
sequential retry, or shared session does not establish contested overlap. Name
which barrier and observation demonstrate the contract. Include domain-specific
cases from the source; this template cannot infer omitted requirements.

## Submission and review

Keep staged, unstaged and untracked files clean. Submission validates mappings
before execution, rebases as usual, reloads the immutable post-rebase manifest,
and executes the exact reviewed commands outside the state lock. HEAD and source
identity must remain stable and commands must leave the worktree clean. Successful
admission atomically records `acceptance_receipt` with the review commit, source,
manifest blob, mappings, command outcomes, UTC times and masked output.
Each command result includes required `command_sha256`: SHA-256 of the exact
canonical command executed. Assignment checks that digest; `command` is masked
display text and does not supply command identity. Skipped commands appear in the
receipt's `deferred` with authority `contract` or `operator` (an operator ruling
can defer an exact command for in-flight tasks); reviewers treat them as not run.

Reviewers inspect full task JSON using the configured binary's
`get tasks <task-id> --format json`. Inspect assertions, source allocation and
execution output together. Missing/stale receipts block normal or retained review;
`update-review-commit <task-id>` reruns validation to repair evidence, including at
unchanged HEAD. Failure leaves the prior review state intact.

## Limits

- Source declarations and manifests: 256 KiB; at most 256 obligations and 64
  canonical commands. Strict JSON rejects unknown/duplicate keys and unsupported
  versions. Paths must be clean repository-relative non-credential paths, with
  no symlink/submodule proof files.
- Direct Reference targets: the cited section 256 KiB after line-ending
  normalization, in a file of at most 4 MiB.
- Total execution timeout: 600 seconds by default, configurable from 1 to 14400
  seconds in the reviewed declaration. Aggregate captured output: 1 MiB;
  overflow fails admission instead of reporting truncated success.
- Commands must emit sanitized output and contain no inline secrets. Known
  environment secrets and credential-bearing URL/DSN values are masked; arbitrary
  sensitive semantics cannot be inferred. No credential file is read by the gate.
- The commit pins tracked candidate code. Ignored prerequisites, databases and
  other external inputs retain project environment handling; a receipt does not
  establish their reproducibility. Missing prerequisites block validation.
- Legacy tasks without an adopted declaration remain reviewable and explicitly
  lack machine-validated acceptance evidence. No bulk migration occurs.
- A green but incomplete manifest cannot enter normal strict review. A complete
  manifest can still name a weak assertion or omit an upstream requirement;
  independent planning and code review remain necessary. Existing review churn
  and iteration limits still apply.
