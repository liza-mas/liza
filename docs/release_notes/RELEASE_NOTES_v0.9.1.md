# Liza v0.9.1

The main improvement in v0.9.1 is convergence: runs that previously stalled in
chains of blocked tasks now recover by cause. Conflicting instructions across
skills, wake templates and the operator guide were corrected against one shared
authoring vocabulary, and superseding a task stopped being the orchestrator's
default answer to a blocker.

Acceptance obligations gained machine-checked evidence, planning artifacts became
reference-first, and repeated lifecycle commands now return what actually
happened instead of a generic failure. Prompt payload and instruction growth are
measured per role rather than estimated, and inspection no longer competes with
the run it inspects.

114 commits since v0.9.0, through `d11cfe434`.

---

## Highlights

**Runs converge instead of chaining blocked tasks** -- Priority, consequential
commitments and proof stages now have one definition that six skills point at,
so a Must set cannot quietly become optional between stages. Planning reviewers
check that the next role can act from the artifact, not only that the artifact
is correct. The orchestrator recovers by cause -- stale ownership, wrong
dependency, corrected inputs, product ambiguity, unsupported assumption,
immutable ancestry -- with supersession reserved for the cases that need it.
Closure is consumer progress past the old failure point, not a new task ID.

**Acceptance evidence is admitted, not asserted** -- A coding task can carry a
versioned acceptance contract allocating obligation IDs, a manifest path, exact
canonical commands and explicit non-executable exceptions. Evidence resolves
from immutable Git objects at the reviewed commit, and only an independently
approved MERGED planning parent can authorize strict acceptance. Marker-free
legacy tasks stay reviewable with the absence of machine-validated evidence made
explicit. Obligations are tracked by content rather than by file, so a merge that
changes the proof text reports drift.

**Reference-first planning artifacts** -- Planning outputs declare stable source
obligation IDs and pinned references instead of restating requirement prose.
Children can inherit selected upstream outputs rather than a whole phase barrier,
and an orchestrator-only repair narrows inherited dependencies after generation.
Reference freshness is judged by section, and a task-introduced reference
resolves at the review commit.

**Verdicts survive generation fencing** -- Substantive review findings were
previously discarded when a reviewer's generation was fenced, letting a later
approval of the same commit merge without reconciling the disagreement. Verdict
evidence is now retained, bound to the reviewed commit, and conflicts require
audited orchestrator reconciliation. **This changes the `submit-verdict`
interface -- see Breaking Changes.**

**Lifecycle commands answer their own retries** -- Repeated or stale lifecycle
actions return authoritative outcomes derived from durable request receipts and
transition identities, rather than a generic failure that left the agent without
evidence for safe recovery. Replacement-task creation is transactional and
idempotent, blocked-task assessments deduplicate, payloads validate against a
shared schema registry before state mutation, and repeated reviewer-claim
failures open a circuit breaker instead of retrying forever.

**Context spend is measured, not estimated** -- Two instruments measure what
prompts actually cost: rendered payload through the real compositor on a
calibrated fixture, and instruction text per role variant against a committed
baseline with a 5% ceiling behind an opt-in gate. Ancestor-carrier references now
render as pointers, and direct references already inlined at the same path and
blob collapse to one line -- a 9.5% payload reduction on the measured corpus,
with nothing an agent could read removed.

**Operators can intervene without editing state** -- A bounded, provenance-
recorded note appends under the state lock and wakes an idle orchestrator through
a dedicated trigger, with per-note seen-stamping so a request is neither dropped
on a failed turn nor executed twice. The orchestrator must read notes before
reassessing.

**Inspection stops competing with the run** -- Status and get calls read one
uncached atomic snapshot instead of reloading locked runtime policy per task.
`time_in_status` has a single definition shared by the TUI, `get-tasks` and field
queries, which previously agreed on 4 of 93 tasks in a live run.

---

## Breaking Changes and Upgrade Notes

| Change | Impact and migration |
|--------|----------------------|
| `submit-verdict` requires `--review-commit` | The full immutable SHA actually reviewed (40 or 64 hex characters) must be supplied. Authenticated Go callers must pass the same explicit commit argument. Existing scripts must retain and pass their reviewed SHA rather than letting the command infer it. |
| Acceptance evidence before review | A task carrying an acceptance contract cannot reach review without a committed, strictly decoded manifest supplying one executable mapping or an approved exception per allocated obligation ID. Tasks without the marker remain reviewable and are reported as lacking machine-validated evidence. |
| Rejection RCA gate | After a configurable threshold of durable rejections (default four), a task enters `BLOCKED` with reason `rejection_rca_required` and needs a recorded RCA and disposition before iteration resumes. `unblock-task` remains the only restoration path and enforces the disposition's restore mode. |
| Cause-based recovery replaces supersession defaults | Orchestrator wake instructions no longer default to supersession for spec ambiguity or a wrong approach. Operator runbooks and automation that assumed supersede-then-redo should follow the recovery table instead. |
| Operator note inspection required | The orchestrator reads `human_notes` before another reassessment. Notes recorded before this build are unseen and produce one catch-up wake, which may include legacy audit entries. |
| Per-role instruction budget gate | `TestRoleBudget` asserts a 5% per-variant ceiling now that the baseline carries `gate: true`. Instruction changes that exceed it fail the suite until the baseline is deliberately rebased. |
| Prompt reference rendering | Ancestor carriers' declared references render as pointers rather than inline text. Agents that assumed every declared reference appears verbatim in the prompt must follow the pointer. |

See [Configuration](../../support-docs/CONFIGURATION.md) for worktree setup and
provider placement, and
[Troubleshooting](../../support-docs/TROUBLESHOOTING.md) for recovery flags and
version behavior.

---

## Features

| Feature | Description |
|---------|-------------|
| Shared authoring vocabulary | Defines MoSCoW binding rules, the consequential-commitment record and the proof-stage table at one owner, with six skills pointing at it; adds three review-boundary items and the rule that a finding cannot create a commitment. |
| Planner and reviewer handoff duties | Adds handoff-usable, priority-propagation, commitment-ownership and proof-stage checks to the planning reviewer checklists, plus minimal-Must mechanism for architecture and dependency meaning for code plans. |
| Cause-based orchestrator recovery | Replaces supersession defaults with a recovery table keyed by cause, carries replacement-lineage policy into three wakes, and requires closure evidence rather than a new task ID. |
| Operator convergence watch | Rewrites six operator-skill sections around delivery objective, blocking chain, governance-not-review planning audit, and cause-matched repair. |
| Acceptance evidence contracts | Adds versioned acceptance contracts allocating obligation IDs, manifest paths, canonical commands, timeouts and non-executable exceptions, with provenance rechecked at admission. |
| Content-bound proof reaffirmation | Tracks acceptance obligations by content rather than file, reports obligation drift after merges, and judges allocation on the reviewed section. |
| Reference-first planning artifacts | Replaces restated requirement prose with stable obligation IDs and pinned references resolved at the review commit. |
| Selective dependency generation | Adds `inherit_inputs` so a child waits for named upstream outputs instead of a whole phase barrier; omitted intent keeps the automatic barrier. |
| Inherited dependency narrowing | Adds orchestrator-only `narrow-inherited-dependencies` to apply selections after generation, failing closed as a whole. |
| Bounded operator notes | Adds a local operator-only file-input command appending bounded notes under the state lock with provenance, plus a `HUMAN_NOTE` wake trigger and note inspection before reassessment. |
| Prompt payload and role-budget instruments | Adds `promptbench` with a generated calibrated fixture measuring all four render sites, and `rolebudget` measuring every role variant's prompt and mandatory reads against a committed baseline. |
| Payload schema registry | Adds a versioned payload-schema registry with shared structural validators used at both the preflight and mutation boundaries, keeping preflight state-free and ungated. |
| Transactional task replacement | Commits replacement creation, consumer updates and supersession atomically, replaying from source receipts and validating declared preserved worktrees. |
| Rejection RCA gate | Gates high-churn tasks on a durable classified RCA and disposition, with restore modes enforced by `unblock-task`. |
| Usage attribution by outcome | Stores immutable usage records outside state and derives context token usage by terminal task outcome at report time. |
| Nonblocking inspection | Adds one-snapshot status reads, dotted task fields, `get-tasks` and field projections with consistent nested output and lifecycle redaction. |
| Runtime post-worktree command management | Allows the configured worktree setup command to be managed at runtime rather than only at initialization. |
| Stall-kind alerting | Reports which stall a `STALLED` alert actually found, rather than a single undifferentiated state. |
| Greenfield documentation elicitation | Has goal-writing elicit the shipped documentation surface and ADR convention when nothing exists on disk, with a self-suppressing readiness criterion. |
| Source-language propagation | Generates epics and stories in the language of their assigned source material, keeping identifiers verbatim; architecture plans stay English. |
| Decision records in the contract | Makes decision records part of the universal contract as durable memory and a Doc Impact category, with `architecture-planning` reading prior decisions and routing candidates. |
| Spec reconciliation at review | Lets an alters-documented-behavior question falsify a Doc Impact of "none", and has review test the declaration. |
| Operational analysis reports | Enriches the log-analysis and operator skills, and adds a context-engineering skill for prompt and handoff quality. |

---

## Fixes

| Fix | Impact |
|-----|--------|
| Retained fenced verdicts | Keeps bounded sanitized verdict evidence when generation fencing applies, binds authenticated verdicts to the reviewed commit, and requires audited reconciliation of conflicts. |
| Authoritative lifecycle outcomes | Returns durable receipts, transition identities and recovery outcomes for repeated or stale actions instead of a generic failure, preserving authorization and external-effect fences. |
| Restarted-generation preparations | Retires a lifecycle preparation left by an agent whose generation the registry has replaced, which previously blocked every other agent indefinitely. |
| Ownership tuple repair | Stops lease renewal on unassigned tasks and normalizes dangling leases during `migrate`, so a half-set tuple no longer refuses the commands that would repair it. |
| Write-state cause exposure | Carries the underlying submission failure to the agent through bounded, secret-masked details rather than discarding it. |
| Validation session prerequisites | Preflights declared command prerequisites in assigned agent sessions before assignment, reclaim and provider start, retaining sanitized readiness as audit evidence. |
| Blocked-assessment deduplication | Uses non-assessment history counts and one shared fingerprint predicate for transactional suppression and blocked-task wakes. |
| Reviewer-claim circuit breaker | Keys pre-claim reviewer failures by role, task, class and boundary, quarantining deterministic failures and backing off the rest. |
| Acceptance receipt preservation | Retains reviewed authorship after ownership release, preserves command output when saving receipts, exposes masked command failure diagnostics, and repairs evidence orphaned by an integration rebase. |
| Test-file admission coverage | Recognizes C# and Node module test filenames at submission. |
| Reference scope and freshness | Preserves assigned section scope in reference context, judges freshness by section, adopts integrated parents, and quotes the `git show` target in reference pointers. |
| Dependency and blocking correctness | Rejects downstream dependencies when blocking tasks, validates child acceptance before review submission, resolves selections on crash-recovered children, and persists retirement. |
| Merge resumption | Retries owned merges on a bounded wake and resumes an interrupted merge on proven effect. |
| Premature cohort analysis | Recovers empty-cohort analyses produced before their scope was populated. |
| Prompt construction failures | Releases task owners when prompt construction blocks, and blocks the reviewed task when a reviewer's prompt context fails. |
| Claim retention across background work | Keeps the claim when a session ends with a live background job, and moves background-job handling out of the supervisor. |
| Pause and progress handling | Rechecks pause after waiting for work and honors queued progress before timeout. |
| Provider diagnostics | Distinguishes provider diagnostics from quoted output in supervisor logs, and exposes missing agent authority diagnostics through the CLI. |
| State read performance | Caches parsed normalized state by file mtime and returns a deep copy per call, ending the reparse contention that starved state-lock holders. |
| Checkpoint and wake economy | Writes the checkpoint summary once per checkpoint rather than per merge, drops superseded wake snapshots on new assessments, and slows the ABORT fallback tick. |
| Consistent time in status | Derives `time_in_status` from one shared event classification across the TUI, `get-tasks` and field queries, surfacing vocabulary drift instead of silently shifting durations. |
| Alignment and repair evidence | Keeps task descriptions out of alignment summaries, and preserves task output and merged task evidence during integration repair and claim release. |
| Bounded advisory subprocesses | Bounds advisory index commands so a hung indexing subprocess cannot block review submission. |
| Sandbox provisioning | Auto-installs the `bwrap-userns-restrict` AppArmor profile on Ubuntu 24.04+, where the default userns restriction blocks the bubblewrap sandbox. |
| Windows portability | Addresses native CI portability failures, prompt and preflight fixtures, and canonical recovery worktree paths. |
| Shell-safe prompts | Makes the reviewer rejection recipe shell-safe and names the 600s foreground cap in bash constraints. |
| TUI layout | Rebalances panel columns. |
| Build and CI | Caps race-gate concurrency at two test binaries, stabilizes timeout and ISO timestamp assertions, and allows manual workflow dispatch. |

---

## Documentation

- Separates Pairing and Multi-Agent first steps in
  [Getting Started](../../GETTING_STARTED.md) and recommends post-run analysis
  before the next run.
- Makes active supervision the operator default and narrows the clean-code
  charter under multi-agent mode.
- Flags Windows support as experimental.
- Clarifies the file-editing fallback chain and documents search fallbacks when
  ripgrep is missing in [AGENT_TOOLS](../../contracts/AGENT_TOOLS.md).
- Adds lessons on treating a turn ended mid-validation as abandonment, and makes
  the pre-submission race run executable under the agent shell cap.
- Requires supervisor restart for receipt repair in the support docs.
- Records this cycle's architectural decisions -- acceptance evidence,
  reference-first planning, selective dependencies, verdict quarantine,
  lifecycle idempotency, operator notes, context budgets and the authoring
  vocabulary -- in the
  [ADR index](../../specs/architecture/ADR/README.md), now at ADR-0155.
- Refreshes stale architectural-issue evidence and the code quality assessment.

---

## Installation

**Quick install (macOS/Linux, latest published release):**

```bash
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash
```

**Windows (PowerShell, latest published release):**

```powershell
irm https://raw.githubusercontent.com/liza-mas/liza/main/install.ps1 | iex
```

Windows requires Git for Windows, with its `bash.exe` ahead of the WSL launcher
on `PATH`. Contract symlinks require Developer Mode or an elevated shell;
managed Git hooks can use wrapper scripts when symlinks are unavailable.

**From source:**

```bash
make install
```

To refresh installed contracts, skills, and support docs after upgrading, run
`liza setup --force` with the provider flags used by your installation. This
overwrites selected managed global files; retain custom tool guidance through
`--agent-tools`. Updating global assets does not replace an existing workspace's
frozen pipeline.
