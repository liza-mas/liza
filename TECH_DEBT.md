# Tech Debt

Deliberate debt with payback triggers. See CORE.md Rule 3 (DoD) for policy.

## Status changes invisible to history-derived time in status

**What:** `models.TimeInStatus` derives time in status from the most recent
status-transition event in task history. Several status changes are not
recoverable that way, so affected tasks report time since their previous
recorded transition, which is too long:

- **Conditional claim release.** `claim_released`
  (`internal/agent/registration.go`), `doer_claim_released` and
  `review_claim_released` (`internal/ops/release_claim.go:206`) assign a
  released status only when the task was in the matching active status; the
  entry records nothing separating that case from a no-op release. The same
  `doer_claim_released` name is also written by
  `internal/ops/await_verdict.go:841`, which clears ownership without touching
  status — one event name, two different status effects.
- **Attempt rollover.** `TransitionToNewAttempt` writes `new_attempt` in phase 1,
  which explicitly preserves status, then transitions to the initial status in
  finalization (`internal/ops/transition_attempt.go`) without writing a further
  entry. The transition has no timestamp of its own; `new_attempt` precedes it.
- **Silent status writes.** `internal/ops/recover_task.go:273` resets status to
  the initial state on the preserve path and
  `internal/ops/validation_preflight.go:398,411` releases ownership, both
  writing no history entry at all.

**Why deferred:** The correct fix is a durable `status_changed_at` on
`models.Task`, assigned at `TransitionWith` plus the direct status writes — a
schema field and every mutation site, to fix a display metric whose three
surfaces had first to agree at all. Convergence was the reported defect; this
residual is a smaller, separable correctness gap. Reclassifying `new_attempt`
as a transition would approximate the rollover case within seconds, but it
  would make a reservation event stand for a finalization effect and still leave the
conditional and silent cases wrong.

**Payback trigger:** The first report of a stale `time_in_status` on a task
that was released, rolled over to a new attempt, or recovered without an
intervening recorded transition, or the first consumer that branches on the
value rather than displaying it. Add `status_changed_at`, set it wherever
`Task.Status` is assigned, and keep the history derivation as the fallback for
tasks predating the field.

## Terminal task history stays in live state

**What:** Archival moves only terminal tasks' `acceptance_receipt` out of live
`state.yaml` ([Archived Task Fields](specs/architecture/blackboard-schema.md#archived-task-fields)).
Terminal task history stays live, including `orchestrator_assessment` (about
12% of a measured 5 MB run state) and `pre_execution_checkpoint` (about 8%),
as do lifecycle receipts (about 7%).
Whole terminal-record archival and separating hot agent metadata from task
history are follow-ups to [contention-safe recovery](specs/architecture/ADR/0174-contention-safe-agent-recovery.md),
not changes to the current transaction boundary.

**Why deferred:** Removing history entries changes history counts that live
code relies on: `TaskTransitionID` hashes `len(task.History)`
(`internal/models/lifecycle.go`), feeding lifecycle request/replay outcomes;
`ops.TasksAssessedBetween` compares per-task assessment counts across an
orchestrator turn of up to four hours to mark human notes consumed; and
`claim_failure_anomaly.go`, `review_execution.go` and `replace_task.go` use
history lengths or offsets. The usage report, `analyze` and `inspect` also
read the archived kinds. Each needs a logical history count (live plus
archived) or an archive-aware read before any entry can move.

**Payback trigger:** Reached 2026-10-04 on a 5.7 MB run state: a no-op
`Modify` held the lock 4.4–9.2 s under run load (YAML decode, marshal and
re-parse, linear in bytes), and terminal tasks were 80% of the bytes
(`history` 1.68 MB, `lifecycle` 1.36 MB, `output` 0.63 MB). Locked reads now
decode after releasing the lock; writes still pay the full cost.

**Next step:** Terminal status alone does not make these fields dead. A MERGED
task's `output` feeds child generation, transition crash recovery, inherited
dependencies, replan, integration settlement and runtime-input scrubbing;
`TaskTransitionID` hashes `Lifecycle.Revision`, and terminal-task requests
replay from receipts. Beyond the count consumers above, terminal history
content drives the blocked-recovery cap, integration-fix validation,
cycle-blocked planner detection, post-merge verdict matching, acceptance
authorship and sprint metrics, and is appended after terminal transitions.
The only count-preserving slice found, moving note/reason/extra of terminal
`rejected`, `pre_execution_checkpoint` and `orchestrator_assessment` entries,
saves 18% of the bytes. Decide on separate hot-state storage (agent
heartbeats and leases, or terminal-task records) and its cross-record fencing in
an ADR before further archival.

## Direct unblock assignment bypasses claim-stage review caps

**What:** `unblock-task --assign-to` assigns the doer directly instead of going
through `claim-task`'s review-budget escalation. A task unblocked at its review
cap can therefore resume despite retaining that exhausted budget. Ordinary
preserved continuation claims now enforce the review cap.

**Why deferred:** This predates contention recovery and needs the direct-assignment
path reconciled with the explicit review-budget resets in rejection-RCA recovery.
The current fix changes interrupted claims, not direct operator assignment.

**Payback trigger:** The first use of `--assign-to` on a task at its review cap,
or any change making direct unblock assignment share normal claim enforcement.
Add the same escalation check without inventing a budget reset.

## Archive objects have no sweep; Windows directory durability

**What:** An archive transaction that fails after writing an object (state
publication or durability-barrier failure) leaves a complete, unreferenced
object; a retry reuses it, but an abandoned attempt keeps it forever. A crash
mid-write can also leave a `*.tmp.*` file beside the objects. On Windows the
directory fsyncs of the durability barrier are skipped because a directory
cannot be opened for sync, so crash ordering between an object and the state
that references it is best-effort there, like state publication itself.

**Why deferred:** Orphans are bounded by failed attempts and never read as
objects; a sweep must prove an object is referenced by no state snapshot an
operator may still inspect, which needs a retention policy of its own.

**Payback trigger:** The archive directory holds more unreferenced than
referenced bytes, disk pressure is reported, or a Windows crash is observed to
leave a reference to a missing object. Add a sweep of objects unreferenced by
current state and older than a retention window, and remove stale temp files.

## Quarantined verdict retention

**What:** Issue #153 retains unique quarantined judgments, generation
fingerprints, and append-only reconciliation history in active `state.yaml`.
Deduplication limits repeated identical attempts but does not bound unique
findings or their provenance and decision history.

**Why deferred:** Automatic eviction could erase an unresolved merge barrier or
the reason it was cleared. Safe archival needs a queryable audit identity and a
durable lookup for still-relevant holds across restart.

**Payback trigger:** Before a run reaches 1,000 unique quarantined records or
1 MiB of serialized quarantine evidence, or before operating a run expected to
exceed either threshold. These are operational triggers, not measured
performance cliffs. Archive resolved terminal evidence while preserving
unresolved/relevant holds and queryable audit identity; never silently drop
evidence to enforce a cap.

**Related:** [Blackboard Growth Without Pruning](specs/architecture/architectural-issues.md#blackboard-growth-without-pruning).

## Signed validation artifacts for unavailable execution contexts

**What:** Issue #154's optional signed validation artifact capability is deferred.
`validation_execution: artifact-only` fails closed and cannot exempt a task from
direct session preflight. There is no artifact task field, verifier or trusted-key
configuration in this change.

**Why deferred:** Cryptographic trust setup is separate from assignment and
concurrency enforcement. Keeping it out bounds this change while preventing an
unverified artifact from becoming an execution-readiness bypass.

**Payback trigger:** A project needs a validation exemption for an intentionally
unavailable direct execution context. Before enabling `artifact-only`, define
immutable sanitized payloads, exact task/command/commit binding, explicit project
policy, trust-key handling, and adversarial verification tests for tampering,
staleness and untrusted signers. See [ADR-0136](specs/architecture/ADR/0136-validation-session-prerequisites.md).

## Missing planning output is not a hard status/validate finding

**What:** Issue #150's requested hard `status`/`validate` finding for missing
`output[]` remains unimplemented. The repair-path regression proves that an
integration-fix claim previously erased output, but the reporter's affected task
histories have not been checked, so that mechanism is not yet confirmed as the
cause of the reported incidents.

**Why deferred:** The current change preserves output during integration repair
and adds write receipts and diagnostics. A global validation rule needs to
distinguish tasks whose configured transitions consume output from tasks that
legitimately have none; applying a blanket rule would reject valid state.

**Payback trigger:** Before closing #150, inspect the affected task histories for
`claimed_for_integration_fix` and settle the pipeline-aware missing-output rule.
Add status/validate coverage for output-consuming transitions and valid empty
cases; investigate a second loss path if the histories do not match the repair
reproduction.

## Windows ops tests can retain stdio handles after timeout

**What:** A process started by an `internal/ops` test can retain an inherited
stdout or stderr handle after the parent is terminated, preventing `go test`
from observing EOF and returning. The Windows CI package timeout is 30 minutes;
each Windows test shard bounds its test step at 35 minutes and its job at 45
so this fails visibly instead of occupying a runner indefinitely. Every shard
uploads its `go test -json` stream as the `windows-test-<shard>` artifact, and
its log prints the still-running top-level tests every minute.

**Why deferred:** Isolating the retaining descendant and changing its Windows
process/pipe ownership requires a native handle-level reproduction. The current
Windows-support change establishes a bounded CI signal but does not yet provide
the evidence needed to change process lifetime behavior safely.

**Payback trigger:** On the next Windows CI timeout, use the retained
`go test -json` artifact and heartbeat to name the stalled test, capture the
process tree, identify the descendant holding the pipe, and add
a focused regression that proves the command returns after cancellation. Remove
the job-level workaround once five consecutive Windows CI runs complete without
the package timeout or retained-handle stall.

## Toolchain selects two unusable tools on Windows

**What:** `bash-policy` is selected for Windows but its source fallback does not
compile because file locking uses `syscall.Flock` without platform build tags.
`scip-python` installs but fails during module initialization because it inserts
the Windows backslash separator into a regular expression without escaping it.
Toolchain doctor therefore reports both tools as failed, and Python projects do
not get SCIP indexing on Windows.

**Why deferred:** Both failures originate in upstream tool implementations. A
project-local fork or compatibility shim would create ownership and release
work outside the scope of native Windows support; keeping the failures visible
in doctor is safer than treating installation alone as success.

**Payback trigger:** On the next `bash-policy` or `scip-python` version update,
run each tool's version command and a minimal functional smoke test on Windows.
Remove this entry when upstream releases pass those checks. Before claiming the
full toolchain profile is supported on Windows, either complete that upgrade or
exclude the unsupported tools from Windows selection with an explicit reason.

## RTK has no native Windows arm64 release artifact

**What:** The toolchain catalog uses RTK's
`rtk-x86_64-pc-windows-msvc.zip` for every Windows architecture. The project
ships a Windows arm64 binary, so those users receive the x64 RTK executable and
depend on the host's x64 emulation. RTK v0.46.0 publishes Windows x64 but no
Windows arm64 archive.

**Why deferred:** There is no upstream Windows arm64 artifact to select. Naming
an inferred download URL would make installation fail deterministically, while
maintaining a project-owned RTK cross-build would duplicate another project's
release pipeline.

**Payback trigger:** On each RTK version update, inspect the upstream release
assets. When a Windows arm64 archive appears, make the catalog URL
architecture-aware and cover amd64 and arm64 selection in tests. If x64
emulation fails for a supported Windows arm64 user first, add a documented
source-build fallback or mark RTK unsupported on that architecture.

## Orchestrator wake prompts grew 4.9% without a concision review

**What:** The GH-issues campaign added lifecycle, diagnostics and RCA guidance
across seven outputs. Measured on the tightest variant, orchestrator wake
HUMAN_NOTE grew from 9,918 to 10,401 rendered bytes (+4.9%) before the last
prompt task reached it, consuming 483 of the 496 bytes the 5% per-variant
ceiling allows. All eight over-ceiling variants were orchestrator wake prompts,
the smallest in the pipeline at 9.9-12.6 KB against 20-28 KB elsewhere; no
other variant exceeded +2.1%. The baseline was re-anchored at the new sizes
rather than the growth being reviewed line by line.

**Why deferred:** The alternative was blocking `cpm-1-cp-3-code-5` — which
contributed 177 of those 660 bytes and cannot regenerate the baseline, its plan
placing that outside any child's ownership — behind a concision repair nobody
owned. Re-anchoring keeps the gate enabled at the same 5% and moves the
question off the critical path; it does not answer it.

**Payback trigger:** Before the next campaign adds role-neutral prompt text, or
when any orchestrator wake variant next approaches the ceiling, review what the
campaign added to those templates and reclaim what is not load-bearing.
G2.2 governs the tightening and G2.3 bounds it: a small prompt paying three
times the proportional cost of a large one is the signal that role-neutral
placement is wrong, not that the ceiling is.

## Race-gate tests carry fixed wall-clock tolerances

**What:** Several `internal/ops` tests assert against absolute durations rather
than against progress: `await_resubmission_test.go` allows 800ms for a delayed
watcher to return within its original deadline (`:574`), 2s for a lease to
appear on entry (`:1013`), and 10s for review ownership (`:1075`, racing an
`AwaitResubmission` call given the same 10s). They pass in isolation and fail
under a loaded `make test-race`, measured at 818ms against the 800ms budget.
`make test-race` now runs with `-p 2` to keep the gate trustworthy, which
removes the contention rather than the fragility.

**Why deferred:** Four tasks were simultaneously blocked by the gate they
needed in order to land anything, including the repair tasks routed at the
failures themselves. Capping concurrency unblocks that without touching test
semantics; rewriting the tolerances is a change to shared test files that the
blocked work would have to validate through the same gate. Raising an assertion
limit to make a red test green would be suppression, so the rewrite has to
replace absolute durations with progress-based waits, which is real work.

**Payback trigger:** When the gate next fails on a wall-clock tolerance, or
before `-p 2` is raised for wall-time reasons, convert these assertions to poll
until a package-level deadline instead of a fixed budget. Whoever raises `-p`
owns the evidence that the tolerances survive the contention.

## CI does not yet enforce the split test targets

**What:** Routine `make test` no longer enables the race detector or writes a
fixed `coverage.out`. The current CI workflow still invokes only `make test` and
then attempts to upload `coverage.out`, so CI neither runs the new
`make test-race` concurrency gate nor generates the profile expected by its
Codecov step. Final local validation is enforced through the worktree build
prerequisite lesson in the meantime.

**Why deferred:** The test-suite performance goal explicitly excludes CI
pipeline configuration. Keeping target semantics and CI orchestration as
separate changes avoids silently expanding a performance implementation into a
workflow-policy change.

**Payback trigger:** Before the next CI workflow change or release cut, add a
dedicated `make test-race` step, run `make coverage` where Codecov upload is
desired, and update the upload step to consume an intentionally retained
profile rather than the target's self-cleaning temporary file.

## CLI, commands, and integration tests cache nondeterministically

**What:** Unchanged `make test` runs consistently cache `internal/testguard`
after its source walk was bounded, but `cmd/liza`, `internal/commands`, and
`internal/tui` re-run on every repeat, and `internal/integration` re-runs
intermittently — it reported `(cached)` on one repeat and re-ran on the next
with the tree untouched. Their test logs carry run-specific temporary working
directories and environment/file observations, so Go computes a fresh
test-cache input for some or all of these processes each run. Wall time for an
unchanged repeat therefore varies with which packages happen to hit: measured
repeats span 45.4s to 103.1s across two checkouts.

**Why deferred:** Removing the remaining cache inputs requires a separate audit
of process-wide cwd/environment mutation across the CLI, commands, and
integration suites. That is materially broader than correcting the reviewed
performance claim and the two local Makefile controls.

**Payback trigger:** Before claiming a reliable unchanged full-suite run under
60s, or the next time test-suite latency is prioritized, use
`GODEBUG=gocachetest=1` and test-log traces to remove run-specific inputs from
`cmd/liza`, `internal/commands`, and `internal/integration`. Close on cache
status, not wall clock: two consecutive unchanged runs in which every
test-bearing package reports `(cached)`. A wall-clock threshold can be met by
variance alone — one measured repeat already came in at 45.4s with none of
these inputs removed.

## Legacy built-in readiness surface

**What:** `tasks.legacy_coder_claimable`,
`tasks.legacy_code_reviewer_reviewable`, and their dashboard/work-queue lines
remain always-on for compatibility. They retain the former built-in,
lifecycle-level semantics alongside the ownership-aware, configured-role
`claimable_by_role` and `reviewable_by_role` fields.

**Why deferred:** Removing or redefining the legacy fields can break unknown
structured-output and dashboard consumers. Preserving their historical meaning
keeps those consumers stable while the configured-role surface becomes the
scheduling authority.

**Payback trigger:** Once supported consumers have migrated to
`claimable_by_role` and `reviewable_by_role`, and a repository/integration scan
finds no remaining legacy field or work-queue consumers, announce deprecation
and remove the legacy fields and lines in the next compatible release boundary.

## Free-text mutation flags can consume registered flag tokens

**What:** The CLI guards `--reason` against an empty, unquoted shell variable
causing it to consume the next registered flag token. Other free-text mutation
inputs remain outside that guard:

- `reconcile-merged --pr-url` — **source-verified, not runtime-reproduced**:
  the same scalar-string shape as `--reason`, persisted without URL validation.
  Extending the registered-token guard to this flag is a cheap follow-up;
  validating URL syntax is separate semantic work.
- `mark-blocked --questions` — **source-verified, not runtime-reproduced**:
  a `StringSlice` whose operation validation checks count, not non-empty content,
  so it needs per-element handling in addition to the token guard.
- `mark-blocked --repair-*` text fields — **suspected by source inspection, not
  runtime-reproduced**: their presence-oriented validation may accept consumed
  flag tokens, but the interacting field set needs a dedicated reproduction.

In contrast, `--impact` is domain-validated and `--merge-commit` must resolve
locally before mutation.

**Why deferred:** Adding `--pr-url` to the scalar registered-token guard is
mechanically cheap but outside the reason-specific fix. Questions and repair
fields have different value shapes and need field-specific behavioral tests.
Semantic URL, question-content, and repair-request validation remains distinct
from swallowed-token detection.

**Payback trigger:** The next addition or modification of a free-text mutation
flag, or the first report of swallowed-token corruption outside `--reason`.
At that point, extend the scalar guard to `--pr-url`, census the remaining
free-text flags, and add semantic boundary validation for the affected field
family.

## Concurrent merge sync/test/restore window is not fully isolated

**What:** Each integration ref/index mutation is atomic across processes, but the
integration mutation lock is released while integration tests run. Concurrent
merges that modify the same repo-relative path can therefore interleave one
merge's test window with another merge's sync or restore and leave the checked-out
working tree stale even though the integration ref remains correct.

**Why deferred:** Holding the lock across integration tests would serialize the
full pipeline. Running tests in an isolated integration worktree avoids that cost
but is a larger lifecycle change than the index-lock collision fix.

**Payback trigger:** The first project where concurrent merges are observed or
expected to modify the same repo-relative path. At that point, run integration
tests in isolated worktrees or deliberately serialize the full sync/test/restore
window.

## Provider activation evidence paths duplicate setup-writer knowledge

**What:** `hasRepoContractActivationEvidence` restates five repo-relative
activation artifact paths independently of the code paths that write those
artifacts. A writer-path change can therefore silently weaken legacy ownership
attribution.

**Why deferred:** The artifacts do not share one writer boundary. In particular,
Cursor's hook artifact is not produced by the embedded-writer layer, so extracting
one authoritative path table is not a local mechanical change.

**Payback trigger:** The next time a provider activation artifact path or its
writer changes, centralize the affected path metadata and consume it from both
the writer and legacy activation-evidence detection.

## Retired task artifact refs are non-blocking during merge validation

**What:** `ValidateArtifactRefs` and task invariant artifact-ref checks ignore refs on `SUPERSEDED` and `ABANDONED` tasks. This prevents stale superseded/WIP artifacts from blocking unrelated merges, but it also means retired task artifact loss is not enforced by global validation.

**Why deferred:** The correct lifecycle fix is to require superseded replacements to be self-contained or artifact-backed, and to retire stale refs explicitly when they are no longer protected. The current change is a narrow unblock for cross-task contamination while candidate-scoped validation and supersede invariants are still being implemented.

**Payback trigger:** When the supersede invariant and candidate/lifecycle-scoped artifact validation are implemented, remove the retired-task skip and enforce either artifact presence or explicit ref retirement at supersede time.

## ParentTask (singular) field deprecation

**What:** `models.Task.ParentTask *string` coexists with `ParentTasks []string`. `EffectiveParentTasks()` bridges both, and `buildChildTask` writes only `ParentTasks`. But `ParentTask` remains in the struct and is populated by existing YAML state files.

**Why deferred:** Removing it requires migrating all active state files (in-flight sprints across user projects). No correctness risk while `EffectiveParentTasks()` handles both.

**Payback trigger:** When no active state files use `parent_task` (singular) — check with `grep -r "parent_task:" ~/.liza/state.yaml` across deployments. At that point, remove the field from the struct and drop the fallback branch in `EffectiveParentTasks()`.

## Worktree path guard: unverified payload shapes (MultiEdit, NotebookEdit)

**What:** `internal/embedded/hooks/worktree-path-guard.sh` extracts `file_path` from the PreToolUse payload to catch `.worktrees/<id>/<id>/` duplication. For Read/Write/Edit the field name is documented. For MultiEdit and NotebookEdit it is not — as of 2026-04-17, neither tool has its PreToolUse hook schema in the public Claude Code docs.

**Current state:**
- MultiEdit: matcher IS registered in `claude-settings.json`. Best-effort coverage — if MultiEdit sends `file_path`, the hook catches the bug; if not, it silently no-ops (no false deny because the extraction returns empty). Do NOT treat as confirmed protection.
- NotebookEdit: matcher NOT registered. Less common use, and promoting MultiEdit was the lower-risk experiment.

**Why deferred:** shipping claims of coverage we haven't verified would mislead future maintainers. The asymmetry (MultiEdit registered, NotebookEdit not) is intentional — MultiEdit is a higher-probability vector for the target bug given its Edit-like semantics.

**Payback trigger:** Next time MultiEdit or NotebookEdit is invoked during an agent session, capture the raw PreToolUse payload via a temporary debug hook (e.g., `cat >> /tmp/payloads.jsonl`). Promote MultiEdit to VERIFIED or teach the script the real field name; add NotebookEdit matcher once its shape is confirmed.

## Deferred greenfield reproduction for precommit-bootstrap

**What:** The greenfield reproduction procedure defined in `specs/goals/20260417-precommit-bootstrap.md` §Greenfield Reproduction Procedure is deferred. That procedure seeds two synthetic greenfield projects under a scratch `REPRO_ROOT`, runs `liza init` on each, starts two parallel supervisors against currently-unfixed prompts, waits for both cycles to reach a terminal state (up to 5 iterations per project), captures per-cycle artifacts (`state-snapshots/`, `agent-outputs/`, `supervisor.stdout.log`, `worktree-git-logs/`, `prompts/`, `precommit-config-presence.txt`) via `scripts/repro/precommit-bootstrap-greenfield.sh`, sanitizes user-home prefixes and scrubs credential-shaped files, and commits the sanitized tree under `specs/goals/precommit-bootstrap-repro-artifacts/<YYYY-MM-DD>/`. In place of executing that procedure, the four hypothesized failure modes from §Evidence were accepted on inspection and recorded in §Observed Failure Modes with the evidence pointer `hypothesis inspection only (no empirical run)`.

**Why deferred:** The reproduction is operator-scale wall-clock work — two full supervisor cycles running end-to-end against an intentionally broken baseline, each potentially iterating up to five times, plus capture and sanitization — and the current sprint is prioritizing landing the remediation stack (Q2 dedup, Q3 architect-prompt bootstrap entry, `internal/precommit/` context helpers, ADR-0036 amend) over producing baseline evidence of a failure mode the design already remediates. The four hypothesized modes are mechanically motivated by the combination of `.pre-commit-config.yaml` being absent in a greenfield `liza init` tree and `commit_workflow.tmpl:3` unconditionally instructing coders to run pre-commit on every commit — each mode is a direct consequence of that composition rather than a speculative behavior, so accepting them on inspection is sufficient to justify the scoped design without blocking the sprint on wall-clock reproduction work.

**Payback trigger:** Any production bootstrap failure whose observed mode does not match one of the four hypothesized modes enumerated in specs/goals/20260417-precommit-bootstrap.md §Evidence.

## Provider readiness preflight before agent registration

**What:** `liza agent` registers and heartbeats the supervisor before proving that the selected provider can create a usable session. Provider-scoped crash classification now catches Codex `~/.codex/sessions` access failures after the first failed execution and writes `.liza/provider-unavailable-<provider>`, but the first agent on a broken provider can still register, claim work, and crash once before the signal exists.

**Why deferred:** A correct preflight needs provider-specific readiness semantics that do not consume quota, create durable sessions unnecessarily, or depend on stack-specific project commands. The current fix bounds the production failure from an unbounded restart loop to one classified crash per provider failure, while preserving stack-agnostic runtime behavior.

**Payback trigger:** Add a provider capability/preflight layer when a second provider-startup failure class is observed, or when Codex exposes a stable non-mutating readiness command for session-store accessibility.

## No detection of auto-mode-incapable Claude models

**What:** MAS agents are launched with `--permission-mode auto` (claude entry in `provider-catalog.yaml`, mirrored in `internal/providers/embedded.go`). Auto mode has documented model and organization requirements — see [permission modes](https://code.claude.com/docs/en/permission-modes) for the current list, which names Haiku, Sonnet 4.5, Opus 4.5, and claude-3 models as unsupported on every provider. Liza does not check whether the configured model meets them.

Observed behavior when the model is unsupported (`claude -p --permission-mode auto --model claude-haiku-4-5-20251001`, 2026-08-09): no startup error; the run proceeded, refused its `Write` tool call with a request for permission, wrote no file, and exited 2. The identical invocation with `--permission-mode acceptEdits` created the file. Not established here: whether every unsupported model behaves this way, or whether any warning reaches stderr. An organization-level `permissions.disableAutoMode` is a separate and louder case — documented to reject `--permission-mode auto` at startup — so it is not part of this gap.

**Why deferred:** Detecting it means parsing `--model` out of args and profiles and maintaining Anthropic's list of auto-capable models inside Liza — a vendor-owned list that drifts silently and would be wrong in exactly the cases that matter. Liza also does not set the model by default, so in the common configuration there is nothing to inspect. The constraint is documented in `support-docs/CONFIGURATION.md` instead.

**Payback trigger:** First report of a MAS agent that runs but produces no edits. If Claude Code gains a queryable capability check (something like `claude auto-mode config` reporting gate status for the resolved model), use that rather than a hardcoded list.

## Replan retarget skips terminal producers with unconsumed output

**What:** `replan.go`'s retarget loop rewrites downstream `DependsOn` from the
replanned task to its replacement, but skips tasks where
`Status.IsTerminal()`. `MERGED` is terminal *and* is the status children are
generated from (`proceed.go` accepts it as a source, and
`IsUnconsumedPlanningOutput` in `advance_sprint.go` describes the shape). So a
`MERGED` producer with unconsumed `output[]` keeps pointing at the replanned
upstream. At generation, `computeInheritedDeps` hits the `replanned` guard,
skips that upstream, and the producer's children inherit no phase-gate barrier
from the replacement at all.

**Why deferred:** The fix changes dependency generation for runs that use no
selective inputs, so it is not W6's to make. It needs its own review and its
own regression evidence rather than riding on ADR-0137.

**Findability:** `replan.go` already detects this exact condition and
downgrades it to a human warning — "task %s is %s and depends on replanned
task %s — consider replanning %s too". Treat that warning as the
known-incomplete half of the retarget, not as the mitigation it currently is.
`IsUnconsumedPlanningOutput` (`advance_sprint.go`) is the predicate the fix
needs and already exists.

**Scope note:** ADR-0137's degradation pass makes the *persisted selection*
honest in this case — it retires to whole-phase inheritance and records the
retirement — but it cannot recover the barrier, because a replanned task has
no children by design. The lost barrier is this entry, not that one.

**Payback trigger:** The next observed instance of a phase-gate barrier lost
after a replan, or any change touching `replan.go`'s retarget loop —
whichever comes first. This path currently relies on a human reading a
warning, so it should not wait for a third trigger.

## Prompt benchmark cannot detect fixture drift against a live run

**What:** `internal/prompts/promptbench` measures a synthetic fixture
calibrated once from a real run (`testdata/run-calibration.json`). The plan
that chose a synthetic fixture over vendoring real prompts named drift as its
known limitation and a `-state <path>` mode — render a live `state.yaml` and
report the same per-row table — as the drift detector. The flag exists and
skips; the renderer behind it is not built.

**Why deferred:** Rendering real state needs the full prompt-build path
(carrier resolution against a git repository, role resolution, task graph),
which is a separate piece of work from the measurement pass. Building it under
the same change would have widened a benchmark commit into an agent-package
integration.

**Payback trigger:** Before the next regeneration of `testdata/baseline.json`
for any reason other than an intended payload change, or before the next
multi-agent run at a scale comparable to the calibration run — whichever comes
first. Without it, a fixture that has drifted from real prompt shape will keep
reporting reductions that real runs do not see.

## Blocked-assessment byte aggregate

**What:** Issue #157 returns `suppressed_entry_bytes` for each content-equivalent
`assess-blocked` call, but persists no byte total. The sprint outcome matrix
counts appends and suppressed entries only; per-call byte sizes cannot be
reconstructed from that count alone.

**Why deferred:** The fixed metrics schema has no byte accumulator. Extending
it requires compatible reads, sprint attribution and atomic counter updates;
recording suppression in task history would defeat the no-change invariant.

**Payback trigger:** Before a report or acceptance criterion requires a durable
per-sprint total of assessment bytes avoided, extend the metrics schema and
prove restart/concurrency correctness without adding task history or receipts.
Preserve observation-window and unavailable-data semantics. See the
[protocol](specs/protocols/blocked-assessment-idempotency.md#observability-and-limits)
and [ADR-0141](specs/architecture/ADR/0141-blocked-assessment-idempotency.md).

## Payload-schema path-helper allowlist

**What:** Task-ID syntax and `spec_ref` / `plan_ref` worktree-prefix normalization remain outside schema validation because `internal/paths` is not directly importable.

**Why deferred:** V1 obeys the reviewed direct-import boundary; neither an allowlist expansion nor a pure-helper split is owned by this change. Mutation retains the checks.

**Payback trigger:** A schema needs task-ID syntax or ref normalization. Decide a pure `internal/paths` split or an explicit allowlist revision before extending schema scope.

**Related:** [Allowlist-narrowing debt](specs/protocols/payload-validation.md#allowlist-narrowing-debt), [ADR-0142](specs/architecture/ADR/0142-payload-preflight-validation.md).

## Usage-record retention

**What:** Durable provider records in the runtime directory's `usage/` have no pruning or retention policy; day and size rotation do not bound aggregate storage.

**Why deferred:** Records deliberately survive sprint rollover to preserve the token dimension of sprint history. The usage-attribution change establishes capture and reporting, leaving retention unsettled.

**Payback trigger:** The usage directory growing past the documented per-file cap in aggregate, or a general pruning policy landing for task history. Either requires a retention decision for this store.

**Related:** [Usage retention](specs/protocols/usage-attribution.md#retention), [ADR-0144](specs/architecture/ADR/0144-usage-attribution-by-outcome.md).

## RCA assign restore conflicts with session preflight (F1)

**What:** The runtime defect is fixed by
[ADR-0170](specs/architecture/ADR/0170-continuation-is-the-default-unblock.md):
an `assign`-mode disposition now restores unassigned as a continuation, so a task
with `validation_prerequisites` (which still refuses `--assign-to`) is claimed by
its supervisor after a fresh target-session preflight, without consuming an
iteration. Before, it had no restore form at all. What remains is proof.

**Outstanding proof:** AC-161-6 and AC-161-8 end-to-end recovery proof: for
prerequisite-bearing coding and planning tasks, a successful fresh check, the
supervisor's claim with unchanged product iteration and retained RCA audit, and
no execution ownership after a failed or stale check. `internal/ops` covers the
unassigned restore of a prerequisite-bearing `lifecycle_repair` task only. The
[slice finding](specs/plans/20260918-fix-gh-issues/20260921-integration-slice-cpm-1-cp-6.md#f1--non-product-recovery-strands-prerequisite-bearing-tasks)
records the original reproduction.

**Payback trigger:** The next run that restores a prerequisite-bearing task
after a `capability_reroute` or `lifecycle_repair` disposition, or an
integration test of that path.

## Invocation telemetry conflicts with payload-validation boundaries (F4)

**What:** Invocation-time sprint telemetry takes the exclusive state lock before
structural validation on mutation paths. Project-local preflight computes its
verdict first, but still reads and locks state for telemetry before rendering.
The complete command therefore conflicts with its no-state contract; this is
not a claim that the pure schema validator reads state.

**Contested obligations:** The
[payload-validation boundary](specs/protocols/payload-validation.md#validation-boundary-normative)
still requires both:

> Mutation validation MUST occur before `db.For(...)` acquires the state lock.

> Schema validators MUST take no live state and perform no state, lock or Git operations.

Its [preflight contract](specs/protocols/payload-validation.md#preflight-command)
also retains this row:

| Aspect | Contract |
|---|---|
| State access | None: no `state.yaml` read, state lock, history append, Git operation or lifecycle receipt. Validation works outside an initialized project. |

The outer-observation allowance does not settle whether invocation telemetry
is exempt from that row or the mutation ordering requirement. These obligations
remain visible and contested; this entry grants no exemption.

**Inspected evidence:** The following source locations were verified at worktree
base `952c1cb45b6a6de0fede91a68a134110a88028a6`; the report's line numbers still
match current source:

| Source | Observed ordering |
|---|---|
| [internal/ops/lifecycle_invocation.go:32-40](internal/ops/lifecycle_invocation.go#L32-L40) | `NewLifecycleInvocation` calls `db.For(...).Read()` at line 34 to capture sprint identity before returning. |
| [internal/db/blackboard.go:121-134](internal/db/blackboard.go#L121-L134), [internal/filelock/filelock.go:150-153](internal/filelock/filelock.go#L150-L153) | The read takes the exclusive state lock before reading the state file. |
| [internal/ops/mark_blocked.go:64,93-95](internal/ops/mark_blocked.go#L64-L95) | Invocation construction precedes structural validation at line 95, despite the adjacent no-state-path comment. |
| [internal/ops/submission_lifecycle.go:38](internal/ops/submission_lifecycle.go#L38), [internal/ops/submit_review.go:90-94](internal/ops/submit_review.go#L90-L94) | Submission constructs the observation before reaching the structural validator at line 93. |
| [internal/ops/set_task_output.go:64,132-135](internal/ops/set_task_output.go#L64-L135) | The report's additional output-path observation holds: invocation construction precedes manifest validation at line 135. |
| [internal/ops/handoff.go:50,67-69](internal/ops/handoff.go#L50-L69) | The report's additional handoff observation holds: invocation construction precedes structural validation at line 69. |
| [cmd/liza/cmd_validate_payload.go:73-76,91-101](cmd/liza/cmd_validate_payload.go#L73-L101) | Preflight computes the verdict, then synchronously constructs an invocation for telemetry before rendering. Outside a project, the telemetry helper returns without that read. |

Thus a malformed mutation can wait for the state lock before its structural
verdict; preflight can wait after validation but before returning the result.
This is an ordering/contract contradiction, not an allegation of state
corruption or unauthorized writes. No new lock-held runtime probe is claimed.

**Why deferred:** The operator note for `integration-global-2` at
`2026-09-21T03:22:42.896045101Z` authorized recording F4 in this debt entry and
the payload protocol only, not runtime repair or a master-contract amendment.
The preserved evidence is commit `72a9b099d57f8a34bf6214abca66e1d00b3f78d5`,
path `specs/plans/20260918-fix-gh-issues/20260921-integration-global-2.md`,
sections “Blocking source conflict: cp-3 F4 remains unresolved” and “Separately
retained RCA limitation”. It is preserved analysis, not an immutable verdict.

**Decision owner:** The master
[Shared Contracts source owner](specs/plans/20260918-fix-gh-issues/20260918T153055Z-cpm-1-master-plan.md#shared-contracts-owned-by-output-0),
routed through the orchestrator, must decide the boundary and sprint-attribution
semantics and assign authorized shared-boundary repair ownership.

**Candidates, neither adopted:**

1. Explicitly exempt invocation telemetry from the no-state/pre-lock rules.
   This requires an authoritative change to the master/protocol commitments,
   including the preflight State access None row; preserving invocation-time
   attribution does not itself authorize that exception or its blocking read.
2. Move telemetry after validation consistently across `mark-blocked`, submission
   and preflight, also reconciling the output and handoff paths above. Preflight
   already observes after validation: moving mutation calls alone leaves its
   complete CLI state access and pre-render lock wait unresolved. The owner must
   decide whether and how telemetry can coexist with a completely state-free
   preflight. Moving sprint capture later can change attribution across rollover;
   dropping capture loses promised counter observations. Neither trade-off is
   authorized here. The existing
   [counter contract](specs/protocols/lifecycle-results.md#coverage-and-observation-counters)
   attributes late completion to the sprint captured by the invocation.

**Payback trigger:** An authoritative boundary/sprint-attribution decision by
that source owner, followed by authorized repair and contract reconciliation.
Lock-held malformed-payload regression evidence must demonstrate the chosen
response/telemetry behavior at the affected boundaries and preserve the chosen
sprint attribution before this debt can close. A fresh source-bound global
integration review is then required for aggregate acceptance.

**Independent limitation and outstanding proof:** The
[cp-6 F1 debt](#rca-assign-restore-conflicts-with-session-preflight-f1) is
independent; ADR-0170 fixed its runtime defect, and its AC-161-6 and AC-161-8
recovery proof remains outstanding, as the preserved report also records.
Documentation and supersession resolve no runtime defect of F4 and supply no
immutable clean global integration acceptance.

## Supervisor state-read lock exhaustion is still fatal

**What:** D69 gave five supervisor state reads a 60s patient lock wait
(`db.(*Blackboard).ReadContextPatient`):

- the prompt read (`internal/agent/supervisor.go`)
- the provider-start authority read (`internal/agent/systemctl.go`)
- the review-watchdog start read (`internal/agent/review_execution.go`)
- the launch validation-preflight read (`ops.PrepareValidationPreflightContext`)
- agent-ID auto-assignment (`internal/agent/registration.go`)

Once that wait elapses, the lock timeout still exits the supervisor. Supervisor-path
writes still exit after the ordinary 10s wait, including the validation-readiness
write in `prepareValidationPreflight`. A supervisor that dies after a claim can
orphan it until lease expiry (operator note D75). The mitigation's effect on the
death rate was not measured. Agent-ID auto-assignment waits without cancellation,
because its signature carries no context.

**Why deferred:** Making these failures non-fatal needs loop-level recovery in
`RunSupervisor` for a partially executed turn: a claimed task, a built prompt, or a
started preflight. Those semantics have not been designed. Removing the saturation
itself is D69 Steps 2–3: snapshot reads for the TUI and read-only CLI commands,
per-entity validation under the lock, and archiving terminal tasks.

**Payback trigger:** Either of these:

- any supervisor exit whose final error is `lock error (timeout)`;
- D69 Steps 2–3 land and such exits still recur.

## `no_follow_up` runtime policy read outside the mutation lock

**What:** `loadResolverWithRuntimePolicy` (`internal/ops/pipeline_ops.go`) reads
`Config.NoFollowUp` in its own locked read, which `resolverOptionsFromState`
releases before the resolver is used.

- `Proceed` rejects a disabled pipeline transition with that resolver
  (`resolveTransitionDefFrom`).
- `ExecuteAvailableTransitionsReport` selects child-creating transitions with it
  inside `Modify`.

Neither rechecks `NoFollowUp` in the locked `Modify` callback, so a concurrent
policy change can be missed. On any read error, including a lock timeout,
`resolverOptionsFromState` returns no options. The permissive resolver is then
used silently, so `no_follow_up` is ignored for that call.

**Why deferred:** D69 Step 2 converts only observation-only reads to lock-free
snapshots. This read feeds a mutation without being checked again under the lock,
so it stays locked. Fixing it means revalidating the runtime policy inside the
mutation and failing closed on a read error. That is a separate policy change.

**Payback trigger:** Either of these:

- a `no_follow_up` run executes a pipeline transition;
- the next change to how `proceed` loads its resolver.

## Blocked-alert once-ledger grows without pruning

**What:** `alerts.Write` records one key per blocked episode and message in
`alerts.log.once` beside `alerts.log` (D66), and reads the whole ledger under its
lock before every BLOCKED write. Nothing prunes keys of episodes that ended.

**Why deferred:** Growth is bounded by blocked episodes: the 2026-09-23/24 omni
run logged 112 BLOCKED lines, 27 of them same-episode duplicates, so at most 85
keys in two days. alerts.log itself is never pruned either. Pruning needs a rule for when an episode can no longer be
written again, which the ledger cannot tell from its keys alone.

**Payback trigger:** Either of these:

- the ledger exceeds 1 MB;
- BLOCKED writes show up in watch tick latency.

## Creation acceptance check reports inner read failures as invalid input

**What:** `add-task` and `replace-task` run claim's `loadAcceptanceInput` on the
candidate (D52). A Git read failure inside it (`cannot inspect integration
source`, a blob read error, or a parent check that folds an error into "not
allocated") comes back as the same `AcceptanceEvidenceError` as a content fault,
so creation refuses it as `INVALID_INPUT`/`correct_input` instead of
`RETRYABLE`. Only an unresolvable integration commit is classified as retryable.

**Why deferred:** separating them needs an origin class on
`AcceptanceEvidenceError`, which the D63 change adds to the same type and the
same call sites. The refusal is stateless and its reason names the read that
failed, so a spurious one costs the orchestrator one retry.

**Payback trigger:** Either of these:

- D63's fault class on `AcceptanceEvidenceError` merges (map its I/O class to `RETRYABLE` at creation);
- an orchestrator edits a valid task after a creation refusal caused by a read failure.

## Failed-retirement markers outlive a process that never claims again

**What:** A claim or submission that fails after reserving retires its own
lifecycle preparation on the patient state-lock wait; when even that write
fails, only an in-process doer or reviewer claim queues it for its next claim in
the project (D60). The marker still blocks every claim of the task when the
process exits first, when it makes no further claim (for example, it goes on to
work another task for its whole lease), or when the failed invocation was
`submit-for-review`, which runs in a short-lived CLI process and queues
nothing. A restart (new generation) or inspected recovery clears it, as before.

**Why deferred:** a durable pending-retirement record would need the same
contended state lock that failed, and letting a same-generation retry adopt
the marker cannot tell a returned invocation from an abandoned CLI subprocess,
which the lifecycle contract keeps fenced.

**Payback trigger:** any `unresolved preparation remains` stall after this fix
whose preparing invocation had returned, found in supervisor logs as
`failed to retire lifecycle preparation` with no later retirement.

## Critical-path pressure is review-only

**What:** D51 made superfluous dependencies a reviewable defect at every
planning level (ADR-0150 amendment), but two mechanisms stay as they were:

- **Whole-phase inheritance by default.** An output that omits `inherit_inputs`
  still waits for every child of every upstream phase (ADR-0048,
  `models.InheritModeAll`), so one upper-level ordering edge becomes a coding
  barrier unless a planner opts out and a reviewer notices.
- **No critical-path visibility.** No role is shown the dependency chain length
  it creates; planners and reviewers judge each edge alone.

**Why deferred:** changing the inheritance default alters every generated child's
dependencies (ADR-0048 phase-gate contract, replan and narrowing paths) and
would not have caught the implementation-bound client edges that dominated the
incident, which named their consumed artifact. Measuring the chain needs a
graph view in planning context that does not exist yet.

**Payback trigger:** a run after this change where a generated child carries an
inherited edge it does not consume, or where idle agents with zero claimable
tasks recur behind a dependency chain (lean W-1 falsifier).

## Plan-declared replacement leaves three gaps (ADR-0161)

**What:** (1) A task named in `supersedes` by a passed plan that has not yet
transitioned stays claimable until the next transition pass; a coder can start
it minutes before it is retired. (2) A replacement stated only in prose
(no `supersedes`) is still invisible: the original and its duplicate stay live
and `validate` reports nothing. (3) When generation refuses a plan the
classifier passed (pre-existing unrelated corruption, a planner-authored cycle
between outputs, a Kind-dedup skip), the automatic pass only logs a warning:
the plan stays passed, ungenerated and without an orchestrator wake.

**Why deferred:** (1) needs claim eligibility to consult every passed plan's
outputs on each claim, a claim-path cost for a window that had no observed
incident. (2) has no reliable detector: the prose is inconsistent and the tasks
share no structured scope key, so a heuristic would be noisy. (3) needs a
durable refusal record the classifier reads, with a release path once the
cause is repaired; the known causes are rare once inherited dependencies
exclude retired originals.

**Payback trigger:** (1) a claim of a task later retired by the same pass's
plan; (2) any duplicate-work incident on a corrective plan whose outputs omit
`supersedes`; (3) a `Pipeline transition failed` warning naming `plan
replacement` or `supersedes` for a plan classified `passed`.

## Blocked-recovery cap can be bypassed without lineage (ADR-0171)

**What:** The cap counts recoveries only through `superseded_by`/`supersedes`.
An agent that cancels or no-replacement-supersedes a capped task and adds an
unlinked successor (`cancel-task` + `add-tasks`) starts a fresh lineage, and
neither the cap nor `blocked_replacement_chain` sees it. Separately, a plan
output authored before `changed` existed is refused at generation when its
original is now `BLOCKED`, and an automatic pass only logs that refusal: see
gap (3) of the ADR-0161 entry above.

**Why deferred:** Detecting an unlinked successor shares gap (2) above: tasks
share no structured scope key, so a heuristic would be noisy. The lineage
policy already forbids prose-only and unlinked replacement.

**Payback trigger:** A run where a capped task is cancelled or superseded
without replacements and new work for the same scope follows, or a
`blocked_replacement_chain` halt whose lineage omits a predecessor the evidence
shows existed.

## Stranded executing claims without `base_commit` stay manual (D75)

**What:** `models.StrandedDoerClaimReason` requires `worktree` and
`base_commit`, because the takeover continues the preserved branch. An
executing claim without them, such as an integration-fix claim, stays assigned
to its dead holder and needs `recover-task`.

**Why deferred:** Taking such a claim over needs a different continuation per
claim kind (integration-fix recreates from the failed merge), not a preserved
branch. No incident has involved one.

**Payback trigger:** An operator journal records a stranded executing claim
without `base_commit`.

## Unattributable merge effect stays fenced without a recovery command (D106)

**What:** When a `wt-merge` preparation remains and the integration ref reaches
the approved commit with no attributable mutation receipt, the merge owner
refuses: the effect is neither proven nor proven absent. The owner's bounded
retries end in `pending_merge_stalled`, and no command settles the case after
inspection.

**Why deferred:** The case needs a way for Git to carry the approved commit
without this task's receipt, such as a manual merge or a lost receipt write.
Neither has been observed. An inspected-settlement command needs its own
authority and evidence design.

**Payback trigger:** One occurrence of the "without an attributable receipt"
refusal, or a second fenced-merge incident of any case.

## `approvedMergeOwner` keeps an inline registration-liveness copy

**What:** `internal/agent/claiming.go` `approvedMergeOwner` repeats the lease /
heartbeat-window rule that `models.AgentRegistrationLive` now owns for pool
repair, status, watch and stranded-claim takeover.

**Why deferred:** It is out of scope for the D75/D106 fixes and its behavior
is identical today. Changing merge ownership selection belongs in its own
change.

**Payback trigger:** The next change to either liveness rule.

## Failed WIP adoption can leave a staged index or a residual WIP commit (D59)

**What:** When claim-time adoption of a preserved worktree's uncommitted work
fails after `git add -A` (the commit fails), the task is blocked with the work
staged. When the worktree is still dirty after the WIP commit (a writer still
active), the task is blocked with that commit kept on the task branch. Nothing
is deleted, but the repair starts from a state different from the one the
previous owner left.

**Why deferred:** Restoring the exact pre-adoption index and HEAD needs a
restoration transaction, which is more machinery than either rare failure
justifies. Both states are recoverable: after `unblock-task`, the next claim
adopts again (staging is idempotent), or a human commits.

**Payback trigger:** A claim blocked with "adopting it failed" or "still dirty
after adopting".
## File-lock admission waits can exceed the production timeout on loaded Windows hosts

**What:** `filelock.DefaultLockTimeout` (10s) bounds how long `WithLock` waits
for admission. Under synthetic ten-way contention on the 2-vCPU Windows CI
runner, seven of ten contenders exhausted that window while each hold wrote
owner metadata through four file operations (DEV-783). The concurrency test
was decoupled from the production default — it now uses a 2-minute hang guard —
but the production value itself is unvalidated for queued contenders on a
loaded Windows host.

**Why deferred:** The only evidence is one CI observation under synthetic
contention; no Windows user has reported a lock timeout. Changing the default
or making it platform-aware affects every caller on every platform, so it
waits for a real-world signal rather than tuning to a CI runner.

**Payback trigger:** A lock-timeout report from a Windows user, or repeated
Windows CI observations of admission waits approaching `DefaultLockTimeout`.
At that point raise the default, make it configurable, or reduce per-hold
metadata writes; remove this entry once the default is validated under load
on Windows.

## Runtime input provisioning: Should set

**What:** [ADR-0169](specs/architecture/ADR/0169-runtime-input-provisioning.md)
ships the recorded-instance path only. Deferred:

- **Provisioner and approvals.** Recipes are descriptions; the operator produces
  every instance by hand. No recipe execution, first-run approval or new-scope
  approval.
- **Source-bound invalidation.** No `binding: source_commit`: an instance is
  not invalidated when the code that produced or consumes it changes. The key
  is refused at admission rather than accepted and ignored.
- **Churn budget.** No `runtime_input_churn` signal for a task that spends
  fixtures repeatedly.
- **Minted tokens (JWT).** Credentials are static envelope values.
- **Key rotation and identity reset.** A lost operator key fails closed while
  the ledger holds instances; the only recovery is restoring the key from
  backup. A new key cannot recognize spent materializations, so rotation needs
  a design that carries spent identity across key versions.
- **Ledger growth.** `state.runtime_inputs` instances are permanent and never
  archived.

**Why deferred:** The recorded-instance path removes the harms observed (manual
fixture regeneration outside the pipeline, reviewers spending fixtures,
credentials in agent env files) and stands alone. The rest needs its own
review, and rotation needs a safe design.

**Payback trigger:** Provisioner and approvals: the first project needing
unattended re-provisioning. Source binding: the first stale fixture accepted
after its source changed. Churn: a task spending more than a handful of fixtures
without reaching review. Rotation: the first key loss or a required rotation.
Ledger growth: `state.runtime_inputs` noticeably slowing state reads, or more
than a few thousand instances.
