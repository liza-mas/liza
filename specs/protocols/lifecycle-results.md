# Lifecycle Results

Lifecycle commands report what happened and what the caller can safely do next.
These results supplement each operation's state, authorization, schema and Git
checks; they never authorize a forbidden transition.

## Result contract

The JSON envelope retains `ok`, `result`, `error` and the command's exit status.
Successful operations, including proven duplicate submissions, return `ok:true`.
Hard failures remain `ok:false` with a nonzero exit status and their original
error details; `result` carries the same structured recovery fields. An
observation that a task advanced is not successful execution of the request.
Text output exposes the same outcome and safe action.

The fields are flat within `result`:

| Field | Meaning |
|-------|---------|
| `operation`, `task_id` | Operation and task being observed |
| `outcome` | Classification below |
| `safe_action` | Exactly one server-selected next action |
| `task_status` | Authoritative status at the observation boundary |
| `current_assignee`, `current_reviewer` | Current owners, only after applicable authorization |
| `transition_id` | Current task boundary token, also available through task inspection |
| `completed_transition_id` | Original completion identity when replaying a retained receipt |
| `request_id` | Explicit request identity, when supplied |
| `effects` | `none`, `committed` or `unknown`; uncertainty never means safe replay |

Unavailable task state is reported as unknown, without fabricated status or
owner. A returned status is a snapshot, not a guarantee that another process
cannot subsequently advance it. Neither registration generations nor their
digests appear in responses, prompts, error text or logs.

| Outcome | Safe action | Interpretation |
|---------|-------------|----------------|
| `COMPLETED` | `continue` | This invocation committed its intended transition |
| `ALREADY_COMPLETED` | Server selects `continue` or `stop` | A retained receipt proves the same intent and immutable boundary completed; continue only while the caller still owns the relevant live boundary |
| `ALREADY_TRANSITIONED` | `stop` | Task moved past this operation; no assertion that this request succeeded |
| `STALE_CALLER` | `stop` | Generation or ownership no longer authorizes the caller |
| `STATE_CHANGED` | `requery` | Preconditions changed or this request's effects/outcome are uncertain |
| `RETRYABLE` | `retry` | Known transient contention before any operation effect |
| `INVALID_INPUT` | `correct_input` | Invalid, oversized or conflicting payload; preserve field-level diagnostics |
| `FORBIDDEN` | `stop` | Operation or role authorization denied |

The two possibilities shown for `ALREADY_COMPLETED` are server branches.
Every individual response carries one value, never a list or caller decision.
Replay returns compact completion evidence with fresh status and routing, not
an old instruction to continue after ownership has changed.

## Caller workflow

1. Inspect the task using `get <task-id> --json`; evaluate the operation against
   its current state and authority.
2. For explicit retry identity, choose `--request-id` and pair it with
   `--expected-transition` copied from the inspection's `transition_id`.
   Supplying only one flag is `INVALID_INPUT/correct_input`.
3. Execute serially and wait for the command to finish. Read `result.outcome`
   and `result.safe_action` even when `ok:false`.
4. Follow the safe action. `requery` means inspect and reevaluate possible
   effects; it does not permit blindly refreshing the token and retrying.
   `retry` preserves the original arguments under the existing bounded retry
   policy. `stop` ends work on that task, including worktree commands.

Keep the original request ID, expected transition and normalized payload
together across retries and process restart. Logical identity is task,
operation, actor/authority, original expected transition and request ID.
Changing the payload within that key is an invalid conflict. Refreshing the
expected token creates a **new logical invocation**, which requires reevaluation.
The system does not promise permanent uniqueness of arbitrary request IDs.
Current authority is checked before any receipt replay.

For submission, first create the intended commit and verify a clean worktree.
Run `git -C <worktree> rev-parse HEAD` once and retain its full output as the
submission SHA. Example command shapes (replace every placeholder with an
observed value; `<binary>` is the installed CLI):

```text
<binary> get <task-id> --json
git -C <worktree> rev-parse HEAD
<binary> submit-for-review <task-id> <full-sha> --agent-id <agent-id> --request-id <request-id> --expected-transition <original-token> --json
```

Reuse that literal SHA on retry; rebase may change worktree HEAD. Start
`await-verdict` only after submission finishes with `COMPLETED` or
`ALREADY_COMPLETED` and `safe_action=continue`. Never overlap an await or another
submission with an in-flight submission. Existing await verdict fields and
their documented `revise`/wait behavior remain compatible.

## Retained identity and expiry

Optional `Task.Lifecycle` metadata stores revision, completed receipts and at
most one unresolved preparation. Old task state without it remains valid and
starts at revision zero. Effective covered mutations, including legacy calls,
advance revision in their state transaction.

The current token covers task ID/creation time, revision, status,
attempt/iteration, owner/reviewer claim identity, review commit, history length
and last event identity. Protected tasks also bind their canonical validation
commands and prerequisite declarations; adding, changing or removing that
contract changes the token. Claim acquisition identity detects release/reacquire
ABA even if status and owner repeat. Passive heartbeat renewal, lease extension
and prerequisite-readiness observations do not create a new boundary. Restoring an entire historical
state file administratively is outside this guarantee.

Keep at most **four completed receipts per operation and sixteen per task**.
The matching window is the intersection of both limits; quieter operations
have no reserved slots, and interleaved operations can evict their receipts.
Prune oldest completion sequence atomically with a new completion. Each receipt
and preparation projection is at most **1 KiB serialized**. Store bounded
digests, SHAs, counters, verdict and event identity, not free-form notes, test
output or edge lists. Validate identity limits before effects; do not truncate
identities. Validate known operations, count/size bounds and unique monotonic
versions. A compact replay need not reproduce an unbounded original report.

Reviewer-claim receipts preserve legacy stored `review_commit` identifiers
using bounded identifier syntax (at most 128 bytes). This copied result does
not establish Git resolution; every other projected commit field and operation
requires a full lowercase Git object ID. Replay retains the original identifier
even after the live task's review boundary changes.

There is no time-based expiry. Terminal transitions retain the count-bounded
receipts so restart and cleanup retries can find them. A replay appends no
receipt, history event or preparation and performs no operation effects.
Without a retained receipt, an explicit request may start only if its original
expected token still matches the current boundary. An expired request instead
returns `STATE_CHANGED/requery` or provable `ALREADY_TRANSITIONED/stop`.

Legacy calls without either flag remain supported conservatively. Submission
can also match immutable input SHA. Its single existing
`submitted_for_review` history event records original input SHA and attempt
boundary alongside the final review commit; the bounded receipt retains the
input-to-review/base relationship. Check receipts before accessing a worktree,
including after cleanup. Prior submission history can detect an expired SHA
but cannot turn it into an exact replay: return requery/stop without mutation.
Intentionally submitting the same SHA in a later attempt requires a fresh
explicit request pair after reevaluation. Old history lacking original SHA
cannot establish that identity and is not backfilled speculatively.

This bounds receipt storage, not existing append-oriented domain history.
Submission provenance adds constant metadata per actual submission, not per
retry. Matching `HEAD` text, current target status, identical assessment notes
or the current integration HEAD alone never proves the same completed intent.

## Preparations, concurrency and restart

A multi-stage operation reserves its request and boundary atomically after
pure input and authority checks and before external effects, including session
prerequisite probes. The
unresolved preparation is separate from receipt retention and is never evicted
by receipt pruning. Finalization compares its reservation, commits the result
receipt, advances revision and clears the marker in one state transaction.
An interrupted preparation reports `effects=unknown` and `requery`; it does
not authorize repeating arbitrary setup, tests, deletion or merge. The single
exception is an effect proven by a durable, task-attributed record, described
for merge below.
When an invocation returns a normal error before committing its transition,
it may retire only the preparation it successfully reserved, in a blackboard
transaction that verifies current authority while holding the project lifecycle
lock. The reservation identity and boundary must still match the current task.
Retirement advances revision without a completion
receipt or a claim that external effects were rolled back. A denied concurrent
invocation cannot retire another invocation's preparation. Lost authority,
changed boundaries, process abandonment and panic retain any still-unresolved
preparation. Failure to commit the retirement transaction leaves the marker
intact. Retirement performs no external effects and does not require reacquiring
the task lock after a finalization timeout.
An observed failure of the configured idempotent post-worktree setup command
preserves the worktree and original error. After inspection and repair, a fresh
claim follows the existing setup recovery path. An interrupted command with
unknown completion retains its preparation.

Acceptance preflight refusals leave state unchanged. A normal refusal after
rebase or canonical acceptance execution retires its matching preparation as
above and reports `STATE_CHANGED/requery` with unknown effects: arbitrary
validation commands may already have run. It cannot admit review, record a
completion or change task history/ownership. The original explicit request
remains stale. Inspect the task and worktree, evaluate those effects, and repair
the evidence before creating a fresh request with the current transition and
immutable SHA. This permits correction in the same registered session without
blindly repeating a failed invocation. Process abandonment retains its marker
until inspected recovery or an authorized boundary transition resolves it.

Preparation fences its original ownership/attempt boundary, not all future
authorized work. Existing release/takeover, attempt rollover, restart-limit
blocking, cancellation/supersession and recovery transactions retire it when
they actually end that boundary. Denied/no-op calls and passive heartbeat
renewal do not retire it. Retirement advances revision, creates no completion
receipt for the abandoned request, and makes no claim that prior effects were
rolled back. A new claim follows existing recovery/setup semantics.

A newly registered caller with current authority and the required task
ownership/role may atomically retire a preparation from a different generation
and reserve a fresh request, even if task assignment and attempt are unchanged.
Reservation is not a precondition of that retirement: a metadata-only operation,
which reserves nothing, retires the same foreign-generation preparation at
completion. Both the pre-mutation check and the completion guard reach that
conclusion identically, so an admitted request is never refused after its work.
Validate current worktree/HEAD and capture its **current immutable SHA** for
that fresh submission. Same-generation concurrent calls at the same live
boundary requery without a second rebase/refresh. Stale/missing authority fails
before marker changes. Administrative calls without generation cannot infer
process death; they retain explicit recovery/ownership-boundary semantics.

Submission reuses the existing per-task claim/worktree lock under the project
lifecycle shared lock. Two task-locked sections surround **unlocked optional
index refreshes**:

1. Validate authority, identity and worktree; reserve preparation; rebase and
   capture the post-rebase candidate.
2. Release the task lock for SCIP, Stacklit and functional-cluster refreshes.
   A concurrent submit observes preparation and requeries. Authorized recovery
   or takeover may progress while indexing is paused.
3. Reacquire the task lock and compare generation, preparation, original
   boundary, worktree path/branch and HEAD before publishing one submission.
   Any mismatch rejects stale finalization; do not rebase or reserve again.

Refresh remains before the submitted transition but is warning-only if
recovery removes its worktree. Indexing adds zero task-lock hold time; core
rebase/abort has no finite deadline guarantee. Lock timeout before effects may
be `RETRYABLE/retry`; timeout reacquiring after rebase is
`STATE_CHANGED/requery` with unknown effects.

Preserve integration completion → mutation → blackboard-read ordering.
Preparation and completion state writes occur outside the integration mutation
lock. A Git/YAML crash gap does not establish exactly-once shell execution or
permit inferring an earlier merge result from live integration HEAD.
If integration may already have advanced but merge completion is uncertain,
retain the preparation: a returned error alone cannot make that merge safe to
repeat. Certainty comes from the durable mutation receipt, not from the error
and not from live HEAD: when a receipt attributes an integration-ref commit to
this task, that commit carries the approved review commit, and it is still an
ancestor of the integration ref, the effect is proven rather than uncertain.
A later attempt then retires the preparation and finishes the merge once. The
CAS merge re-verifies ancestry and performs no second ref write, and the task's
merge commit is taken from the receipt so an intervening merge is not
misattributed.
Administrative `recover-task --force` with missing/unreadable state keeps its
capability while explicitly reporting that durable receipt evidence is
unavailable.

## Coverage and observation counters

The contract covers submission, verdict, blocking/assessment, doer/reviewer
claims, release, merge, recovery and dependency mutation. Adjacent cancel,
supersede, unblock, task-output and handoff boundaries report it too. Each
operation retains its existing eligibility and validation: for example,
repairing terminal superseded dependency metadata is still permitted, while
an old release receipt cannot release a new claim.

Invalid and unauthorized calls remain side-effect-free for task/agent state,
receipts, Git and alerts. Matched replay skips duplicate domain events and
legacy stale-verdict/failure reports. Existing transition/verdict metrics
continue to derive from domain history.

Separate diagnostic counters use a fixed operation/outcome matrix in one
atomically replaced, locked file per sprint under the runtime directory's
`lifecycle-metrics/`. The filename hashes sprint ID, number and start time,
which are verified inside the file. No task, actor, request or payload becomes
a counter key. Record one final outcome at the outer invocation boundary after
operation locks are released; nested helpers do not count twice. The counter
lock is a leaf: hold no other lock and acquire no additional lock within it.

Attribute a late completion to the sprint captured by its invocation. Current
sprint metrics read only that sprint's counter identity; recheck the identity
in the metrics write transaction and requery on a sprint race. The projection
exposes availability and `observed_since`, not a claim of complete-sprint
coverage. A missing file means unavailable, not zero. First recording,
including after manual deletion, starts a new observation window. Existing
empty, truncated or malformed files remain unavailable with a warning rather
than silently resetting. Activity-log rotation cannot alter these counters.

Telemetry is best effort: process death or write failure can omit an
observation. Unknown sprint identity also makes telemetry unavailable.
These limitations never turn a committed mutation into a failure or a retry.
The diagnostic files are retained per sprint; no general pruning policy is
introduced here.

## Related documents

- [Task lifecycle](task-lifecycle.md)
- [Blackboard schema](../architecture/blackboard-schema.md)
- [System invariants](../../INVARIANTS.md)
- [Operational support](../../support-docs/SUPPORT.md)
