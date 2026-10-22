# 204 - Deferred Validation Tier

## Status

ACCEPTED. Amends [ADR-0134](0134-acceptance-evidence-admission.md) (a receipt may
certify a deferral) and adds a gate to final integration closure
([ADR-0113](0113-sliced-integration-analysis-and-final-closure.md)). Resolves
D-97 (a).

Amended 2026-10-14 ([ADR-0209](0209-admission-limits.md)): each probe takes its
command's admission slots from the pool canonical admission uses. Amended
2026-10-19 (D-223): a probe waits for them until they free, lanes prepare one
at a time, and a failed setup is retried once. Amended 2026-10-22 (D-230): a
command's pass is carried forward to a HEAD that changed nothing it covers.
Amended 2026-10-22 (D-181): a command red with a fix in flight is not re-run,
and an operator can abort the live batch.

## Context

Strict admission (ADR-0134) runs every canonical command of a child at every
submission, correction and repair. Plans listed long shared suites
(real-database, e2e, eval runs) in each child for regression alone, which put
them on the critical path many times per task (D-97). The 2026-10-10 amendment
to ADR-0134 told planners to drop those suites, leaving whole-suite regression to
goal integration. Goal integration attributes a failure by path ownership, not by
merge. Children already in flight had `done_when` texts naming the suites, and a
ruling (I-785) let a batch result satisfy them, but the engine could not act on
it.

## Decision

1. **Contract deferral.** An Acceptance Contract may list `deferred` indices
   into `validation`, strictly ascending. At least one command must stay
   executable. No manifest proof may map to a deferred index: a child's own
   proofs always run at admission. A command a runtime input uses runs only in
   the gate (ADR-0169), so the contract may not defer it. Planning preflight
   refuses both cases.
2. **Operator ruling.** `deferred-validation rule --command <exact> --ruling
   <decision>` records `config.deferred_validation.rulings`. A ruling defers the
   exact command for every strict task listing it, mapped commands included
   (the I-785 authority). It never applies to a runtime-input command.
   `unrule` withdraws it. Both are operator-only.
3. **Receipts certify deferral.** Admission skips deferred commands. The
   receipt gains `deferred[] {index, command (masked), command_sha256,
   timeout_seconds, authority: contract|operator}`, and `commands[]` holds the
   executed commands in canonical order. Reviewer assignment re-derives the
   deferrals: each recorded deferral must be granted by the reviewed contract
   or a current ruling, no contract deferral may be missing, and executed
   plus deferred commands must account for every canonical command. Withdrawing
   a ruling invalidates the receipts that relied on it. A ruling added after
   submission leaves a receipt that executed the command valid, because an
   execution is stronger evidence.
4. **Ledger.** The MERGED write appends the receipt's deferrals, with the
   canonical command text, to `goal.integration.deferred_validation.deferrals`,
   bound to the merge commit and the integration commit it started from.
   `retire --command --ruling` (operator-only) removes an obsolete command for
   every deferral recorded so far; a later deferral puts it back. Deferrals and
   retirements are append-only. A batch record is never edited, and its
   identity is never reused: `last_batch_id` keeps the high-water identity
   while retention keeps the newest 20 batches plus each command's latest pass.
5. **Periodic batch.** Merges and integration reconciliation start a detached
   `deferred-validation run` when a batch is due. A batch is due when the
   effective command set is not covered by a passed batch at integration HEAD
   and either `batch_every_merges` (default 5) forward merges landed since the
   baseline or the goal-integration gate needs a verdict. A failed batch, or
   three ended attempts, at an unchanged HEAD is not retried without `--force`.
   Below that cap, the orchestrator supervisor's poll retries a pending batch
   while integration waits on it, because a stable wait never wakes the
   orchestrator; a halt makes every trigger a quiet no-op.
   One runner holds an OS lock that its child processes inherit where the
   platform allows. When a step's process exits, the runner kills and awaits
   what remains of its process group before advancing, because releasing the
   lock unlocks the description descendants share; a descendant that leaves
   the group (setsid) is outside this fence. When the group cannot be emptied,
   the runner fails closed: it starts nothing more, records the batch as
   `aborted` (`descendants_unstoppable`), leaves the lock with the survivors and
   keeps its ledger record marked `unsettled`, which successors refuse and the
   gate reports until an operator settles it. Where the runner cannot establish
   that descendants ended at all (Windows), it refuses to run. Every Git read
   the runner makes during a batch is bounded and admitted like a command.
   A token in the ledger fences publication, so a dead runner's batch is
   recorded as `aborted` by its successor. A predecessor whose lock did not
   pass to its children, or whose record is unsettled, is refused until the
   operator runs `settle`. Every process start is admitted under the work-admission lock and
   refused while halted or STOPPED. Each probe force-checks out the revision,
   cleans it, provisions configured env files and runs `post_worktree_cmd`
   within `setup_timeout_seconds`, then requires a clean tree. A failed
   preparation makes the batch `infra_failed` (a bisect probe `unavailable`).
   A command that dirties the tree is `contaminated`, and the next command
   runs on a re-prepared tree.
6. **Bisect and routing.** A failing command is bisected over the forward
   merges (mutation receipts on `--ancestry-path`) since its latest pass, or
   since the start commit of its earliest effective deferral. Earliest means
   first in integration ancestry, not first persisted: a merge's integration
   test runs between its ref update and its MERGED write, so a later merge can
   record its deferral first. Periodic due-ness counts from the same baseline. A culprit task is
   named only when an executed pass precedes an executed failure; otherwise the
   failure stays unattributed with a reason. Failure is assumed monotonic. A
   red batch records an immediate discovery per culprit, which wakes the
   orchestrator, and an alert.
7. **Integration gate.** Before global integration is ready, the effective
   commands must be covered by a passed batch at integration HEAD. The gate
   waits with `deferred_validation_pending`, and blocks with
   `deferred_validation_failed` (naming the culprit tasks), with
   `deferred_validation_infra` after three batches ended without a verdict, or
   with `deferred_validation_unsettled` while a runner record is unsettled.
   A correction merge moves HEAD and clears the block.

## Consequences

- Admission time no longer scales with the long suites a plan lists; each
  regression is attributed to one merge rather than to path ownership.
- Deferred commands run in the batch environment, not the task session. Tool
  `env_files` are not loaded there (TECH_DEBT).
- Batches do not run on Windows; deferred commands there need a supported
  host or retirement (TECH_DEBT).
- A step whose processes cannot be stopped needs operator settlement.
- Bisect assumes monotonic failure; a flaky suite yields an unattributed or
  wrong culprit, which the operator can contest by `--force` or `retire`.

## Amendment (2026-10-14)

A step's post-run cleanliness check (`rev-parse HEAD`, `status`) finishes work
already admitted, so it is not admitted again. Admitting it let a halt
committed while a step ran record the finished step as `not_run`, dropping
hours of observed suite time (D-158). The check stays bounded, inherits the
runner lock, is reaped, never writes the worktree (`--no-optional-locks`) and
is refused once the runner is orphaned. Preparation, every step and every
bisect process stay admitted, so a halt still starts nothing new: the batch
keeps the finished step's result and records later commands `not_run`.

## Amendment (2026-10-16)

A batch ran its commands, then its bisects, one after another, so the
integration barrier cost the sum of the suites rather than the slowest one
(about 3 h 45 against 1 h 40 in one run, D-210). Each effective command now
runs in its own lane: a worktree (`worktree`, then `worktree-<n>`) where it
is prepared, probed at HEAD and, if it failed, bisected. Lanes run
concurrently; admission classes (ADR-0209) remain the only bound on how many
heavy commands overlap, so an unconfigured project now runs its deferred
commands in parallel, and suites that cannot share a resource need a class
with `max_concurrent: 1`. Lane worktrees are recreated one at a time before
any lane starts, because Git's worktree bookkeeping is shared; lanes a larger
batch left are removed. Results and culprits keep the effective command
order. A contaminated command no longer re-prepares a shared tree: no other
command uses its lane.

The first lane that ends the batch (a halt or stop refusal, an orphaned step,
an admission class that stays full) stops every lane from starting admitted
work and cancels the lanes' slot waits. A step already running in another
lane finishes and keeps its result, as under a halt. The fence check and each
process start happen under one runner lock, so no lane starts a process after
the fence is set. Within a lane no step starts while its previous step's
descendants may live; an orphaned step in any lane refuses every later start,
the finishing reads included. Narrowing a bisect to failing test IDs is
runner-specific and not done.

## Amendment (2026-10-16, D-160)

Periodic due-ness no longer shares the bisect baseline (§6). It counts
`batch_every_merges` forward merges from the latest batch, whatever its
outcome, and from the earliest deferral's start only before any batch. Counted
from the latest pass, a batch that failed or ended without a verdict left the
count past the interval, so every merge started another batch and the runner
ran back to back until one passed. Bisect still searches from the command's
latest pass, the newest known-good commit. The gate's request for a verdict at
HEAD and the per-HEAD attempt cap are unchanged.

## Amendment (2026-10-19, D-230)

The gate (§7) held both the generation request and completion, so every HEAD
move, a docs-only one included, cost a full batch before re-analysis and
another before completion: 1.3 to 3 h each in one run, up to about 15 h over
three generations. The gate now binds completion only. A global generation is
requested whatever the batch state at HEAD; a missing verdict also requests a
batch there, which merges and reconciliation start alongside the analysis.
Once the latest generation is clean at HEAD, completion waits
(`deferred_validation_pending`) or blocks (`deferred_validation_failed`,
`deferred_validation_infra`, `deferred_validation_unsettled`) as before. A
failed, exhausted or unsettled state requests no batch: the runner's own fences
(failed HEAD and attempt cap without `--force`, unsettled refusal) still decide
every launch. The orchestrator poll retries a batch whenever integration waits
with a batch requested, whatever the wait's reason, so an aborted batch is
retried while an analysis runs. The periodic batch (§5) stays an early-detection
and bisect signal that gates nothing; its failure reaches the orchestrator
through the culprit discovery.

A batch runs the effective set read when it starts. The gate judges the latest
batch at HEAD against the effective set when it evaluates: it covers only a
`passed` batch in which every effective command passed, so a superset pass
covers. A command retired during or after a batch that failed on it leaves the
batch `failed`: while another command stays effective the gate still reports
`deferred_validation_failed` and the trigger treats the HEAD as failed, so
recovery is `run --force` or a merge that moves HEAD. Retiring the last
effective command opens the gate.

Consequences: a batch that fails at a HEAD under analysis supersedes that
generation once its correction merges, spending one of
`max_global_integration_generations`. A red suite can yield both a culprit
correction and an analysis finding.

## Amendment (2026-10-19, D-223)

Deferred batches failed overnight on infrastructure alone, never on a test:
lane setups ran together and raced on shared tool state (concurrent `bun
install`s), the failure kept only `setup: exit status 1`, and a lane budgeted
at the 14400 s cap had no admission-slot wait (ADR-0209 §3: cap − budget), so
losing the slot race to a sibling lane or a submit aborted the whole batch.

- **Setup.** Lanes prepare one at a time: checkout, clean, env-file provisioning
  and `post_worktree_cmd` hold one runner lock, so setups never overlap; the
  final cleanliness check and the probes stay concurrent. A failed
  `post_worktree_cmd` (not halted) is retried once from a tree restored again
  (forced checkout and clean), so nothing the failed attempt wrote reaches the
  next. Each failed attempt is written, masked, to the runner's log
  (`runner.log`); a final failure's `infra_failed` reason carries the masked
  output (head and tail), an overflowed capture none. Bisect probes prepare the
  same way.
- **Slots.** No in-flight grace bounds the detached runner, so a probe waits for
  its slots until they free, whatever its budget; `admission_slot_wait_exceeded`
  no longer occurs. While a lane waits, it re-reads the mode every 10 s; a halt
  or stop ends the batch (`aborted`, `halted`) through the batch fence, so a slot
  won after that starts nothing even if the system resumed meanwhile.

Consequences: preparation costs the sum of the lanes' setups before the last
lane starts. A daemonized slot holder now keeps a batch waiting until the holder
exits or the operator halts, instead of costing one of the per-HEAD attempts.
A setup racing a task worktree's own setup in another process is not
serialized; the single retry covers a transient one.

## Amendment (2026-10-22, D-230 items 3–4, D-223 item 3)

The completion gate still needed a batch that executed every command at the
exact final HEAD, so a docs-only merge after the last pass, or a change no
command depends on, cost a full batch (1.3 to 2 h in one run). An operator
reran only the affected suites by hand (D168), with no record in the ledger.

The runner now decides, per effective command, whether its latest pass holds
at HEAD, and records the decision in the batch. A command is **carried
forward** (result `carried_forward`, `carried_from` the commit of that pass)
when all of these hold:

- its newest *execution* (`passed`, `failed` or `contaminated`; a carried or
  not-run result is no execution) passed, in a batch that did not end
  `descendants_unstoppable`. A newer executed failure always re-runs it;
- that pass's commit is HEAD or an ancestor of it;
- `git diff` between the two, with renames split into a deletion and an
  addition, changes nothing in the paths `config.deferred_validation.scopes`
  declares for the exact command (the whole tree when none), outside
  `config.deferred_validation.inert_paths`.

Entries are Git glob pathspecs, set by the operator in state.yaml; nothing
classifies paths by default. An empty entry, a declared scope without paths,
or an entry carrying its own pathspec magic (`:`) disables carry-forward for
the affected commands, and so does any Git failure: doubt executes. With no
configuration, only an identical tree carries. A later batch at the same HEAD
therefore re-runs only the commands that have not passed there (D-223 item 3),
and a merge or revert that leaves the tree unchanged reuses every pass. Lanes
are created only for executed commands; a batch that carries everything
prepares no worktree.

The gate is unchanged and still reads no Git: `Covers` counts a carried result
as a pass. Retention keeps each command's latest execution as well as its
latest pass, so a pruned newer failure can never let an older pass carry. Bisect
keeps its executed-pass baseline (§6). `run --force` carries nothing forward
and runs even at a covered HEAD. It is the recovery when a scope or inert path
was too narrow: editing the configuration does not reopen a recorded batch.

Consequences: a wrong scope or inert path hides a regression in its command
until an in-scope change or a forced run re-executes it.
Each carried result names its source commit, so the ledger shows which evidence
was reused. No waiver distinct from `retire` exists.

## Amendment (2026-10-22, D-181)

Once a command was red, attributed and owned by a fix task, every later batch
ran it and bisected it again (§6), re-proving the same culprit while holding
admission slots that canonical admissions share; in one run a 45-minute suite
kept two slots busy while five submits queued. No operator command could end a
live batch: a halt lets running steps finish (2026-10-14 amendment).

- **Known red.** A command is not run, bisected or given a lane, and is recorded
  `known_red` (`carried_from` the failure's commit, `owned_by` the fix), when
  its newest execution failed, in a batch not ended `descendants_unstoppable`,
  with exactly one culprit naming a task for it, and that (culprit, command)
  discovery was converted (`set-discovery-disposition`) to a task still in
  flight: unmerged, following `superseded_by` to a live replacement, and not
  abandoned. Ownership binds to the newest failure's culprit, so a live fix for
  an older culprit never suppresses a newer one; an unattributed or ambiguous
  failure executes. A `known_red` result fails the batch, so the gate blocks
  with `deferred_validation_failed`; its task list still names bisect culprits
  only, and its guidance names the fix. It records no culprit, discovery or
  alert. The fix's merge moves HEAD and the next batch executes the command;
  `run --force` skips nothing.
- **Abort.** `deferred-validation abort --ruling` (operator-only) records
  `runner.abort` on the record of a runner that holds the lock; it is refused
  when none does or the record is unsettled. The runner polls its own record
  every 10 s; on a request it fences new admitted starts, ends slot waits and
  cancels a kill context bound to admitted steps only. A step the kill
  interrupted while it ran is reaped as any step (an unemptied group still ends
  `descendants_unstoppable`) and recorded `not_run`, never `failed`, so it is
  not bisected; a step that exited before the abort keeps its result, and
  finishing cleanliness reads are never killed. Any abort observed before the
  batch is recorded ends it `aborted` (`aborted_by_operator`), even when no
  step needed killing; no trigger retries it at that HEAD, and the gate
  blocks with `deferred_validation_aborted` until a merge or `run --force`.
  Halt and stop semantics are unchanged.

Consequences: a second, different regression in a known-red command surfaces
only once the fix merges or dies. A request that lands after the runner
published and claimed its next batch aborts that batch; it is audited either
way, and `requested` means recorded, not done.
