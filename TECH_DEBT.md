# Tech Debt

Deliberate debt with payback triggers. See CORE.md Rule 3 (DoD) for policy.

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
