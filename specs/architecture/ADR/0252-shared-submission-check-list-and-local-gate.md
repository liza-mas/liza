# ADR-0252: Shared Submission Check List and Local Gate

## Status

ACCEPTED — 2026-11-14. Bug prevention discipline, FR10 (authors can run the
gate before submitting). Composes checks owned elsewhere: acceptance evidence
([ADR-0134](0134-acceptance-evidence-admission.md)), admission limits
([ADR-0209](0209-admission-limits.md)), the tool-result store
([ADR-0232](0232-provider-native-tool-result-budgets.md)) and the prevention
record check ([ADR-0250](0250-prevention-record-format-and-vocabulary.md)).
No existing ADR owns how submission's checks are composed: each of
[ADR-0030](0030-code-enforced-agent-guardrails.md) (TDD),
[ADR-0037](0037-rebase-conflict-detection.md) (rebase conflicts),
ADR-0134 and [ADR-0231](0231-submission-refuses-what-reference-consumers-would-refuse.md)
(reference refusals) owns one check, so an amendment to any would home the list
in a single check's record.

## Context

A doer learns what `submit-for-review` refuses by submitting. A command that
answers locally is only worth having if its answer is the gate's: a preflight
written beside the gate drifts from it, and a preflight that rebases the
branch or spends a `single_use` runtime input does harm the gate would not.
FR10 asks for one command that runs exactly the gate's checks on the author's
worktree and returns the gate's verdict, for any configured commands (G1.1).

Constraints:

- MC4: the gate defines its checks once; none is reimplemented for the local
  path.
- SC1: no check's order, input, refusal type, text or details moves.
- SC2: everything the gate does that changes state stays in its runner.
- SC3: the command reports `pass` only when every check that can refuse ran as
  at the gate.

## Decision

### D1 — Post-rebase checks run only on a proven no-op rebase

The gate rebases the task branch before its published-stage checks and its
canonical batch. The command never rebases, in place or in a scratch worktree.
It must prove, read-only, that the rebase would publish HEAD itself (identity)
and would succeed (eligibility):

- **C1** integration HEAD, resolved once from the configured integration
  branch, is an ancestor of HEAD and `integration HEAD..HEAD` holds no merge
  commit (a merge is flattened by the rebase);
- **C2** no staged, unstaged or unmerged change to a tracked file (untracked
  files do not stop a rebase);
- **C3** no merge, cherry-pick, revert, rebase or am state in the worktree's git
  directory;
- **C4** no executable `pre-rebase` hook in the effective hooks directory
  (`core.hooksPath` honoured). Git 2.43 does not run it on an up-to-date branch,
  other versions are not probed, so its presence is disclosed, not assumed
  harmless.

When all hold, the published-stage entries and the canonical batch run on HEAD
in the author's worktree. Otherwise the check stops after the candidate stage:
`rebase` is `not_run` with `rebase_pending` (C1) or `rebase_unverified:<tracked_changes |
operation_in_progress | pre_rebase_hook>`, every later check `not_run` for the
same reason, and the verdict `incomplete`.

### D2 — The list

`submissionChecks` in `internal/ops` is one ordered, closed declaration of 19
named entries, each an existing check function bound to a stage (`snapshot`,
`candidate`, `rebase`, `published`, `canonical`): `agent_authority`,
`pipeline`, `ownership`, `worktree_branch`, `planning_output_acceptance`,
`planning_output_runtime_inputs`, `output_ref_fragments`, `tdd`,
`prevention_records`, `candidate_carriers`, `acceptance_contract`, `rebase`,
`published_planning_output`, `published_output_ref_fragments`,
`published_carriers`, `plan_amendment`, `bounded_contract_correction`,
`review_boundary`, `canonical_validation`. The gate's runner walks the list and
does its own work between entries (the commit ref and replay, the admission
plan, the preparation marker, the rebase conflict route, the index refresh, the
runtime-input spend and the BLOCKED follow-ups, receipts, publication); the
local runner walks the same list. Entry 12 is the one place the two differ by
design, the rebase against its proof. Entries that consume runtime inputs or
slots take that as a mode of the shared function, never as a second
implementation. Five gate steps have no candidate verdict and are reported
`not_applicable`: `submit_hold`, `lifecycle_request` (payload schema, request
identity, receipt replay, commit-ref equality), `preparation`, `index_refresh`
and `publication_guards` (race rechecks of earlier entries). A table test ties
every name to a parity row, a declared subsumption (`published_*` apply the
candidate entries' functions to the same commit and range, `review_boundary`
checks the boundary the runner builds) or a disclosure row, and a second test
proves the gate evaluates the list in order.

### D3 — The command

`check-submission <task-id> [--agent-id <id>] [--max-wait <seconds>] [--json]`,
named through `brand.Command`, a sibling of `run-live` and `admit`. An agent
session's identity and authority are rechecked read-only and its role type must
be doer (a `PermissionError` otherwise, no new `allowed-operations` entry, so no
project's pipeline file needs an edit, G1.1); an operator names the agent and
needs no authority. Exit status: 0 `pass`, 5 `refused`, 6 `incomplete`, and as
every failure of the command 1, 2 for a lock timeout and 3 for an unknown task
(with `ok: false` and the error's envelope). The constants 5 and 6 are
command-local: exit 4 stays `ExitDegraded`, owned by
`specs/protocols/outcome-checks.md`. A refusal carries `{code, message,
details}` as the gate's error envelope renders that error. The result also
names `candidate_commit`, `integration_head`, every check with its stage,
status (`passed`, `refused`, `not_run`, `not_applicable`) and reason
(`after_refusal`, `rebase_pending`, `rebase_unverified:<condition>`,
`single_use_input:<command index>`, `admission_full`),
and the gate's
`deferred_commands`.

The command takes no state, lifecycle or task lock and reads state through
lock-free snapshots. It writes no blackboard transaction, receipt, anomaly,
runtime-input state, in-flight marker, submit hold or index refresh and changes
no ref, branch or HEAD; a change of branch or HEAD between its start and its end
is an operational failure. Canonical commands write in the worktree as at the
gate, and the gate's clean-worktree check applies after them.

*Refusals after the gate's preparation marker (human ruling, 2026-11-14T17:15Z).*
A refusal is pre-marker when its entry is 1-11 and post-marker when it is 13-19;
`rebase` (12) never refuses locally. The gate writes its marker after entry 11,
so a post-marker refusal (`published_*`, `plan_amendment`,
`bounded_contract_correction`, `review_boundary`, `canonical_validation`)
reaches the author as STATE_CHANGED, `requery`, effects `unknown`, where the
same refusal before the marker is INVALID_INPUT, `correct_input`, `none`. The
command made no marker and changed nothing, so for every post-marker refusal,
whatever the cause's type, it reports its own lifecycle: INVALID_INPUT,
`correct_input`, `none`, code `validation`, never STATE_CHANGED, `requery` or
`unknown`. Pre-marker refusals are exactly the gate's envelope.

Only the lifecycle fields differ, and each is asserted per side: `details.outcome`,
`details.safe_action`, `details.effects`, the `code` and the message prefix they
derive, `details.transition_id` (the gate's is written by its marker's
retirement and differs; the local value is the task's transition before the
run) and `details.request_id` (the preparation's request on the gate, absent
locally). `details.changed`, `details.diagnostics` and
`details.completed_transition_id` are absent on both sides. Error type, refusing
entry, reason byte for byte and every other envelope field are the gate's. The
basis is FR10, which asks the command to return the gate's verdict: the verdict
and its reason are the gate's, the recovery fields describe what the local run
did, and coverage gives way second to an unfalse effects claim. The one reason
difference that remains is the BLOCKED follow-up the gate words after a
runtime-input refusal, which stays gate-only like the other follow-ups.

### D4 — Declared prerequisites are the gate's runtime-input prerequisites

The gate refuses, before any command, a canonical batch whose declared runtime
inputs are unavailable. `validation_prerequisites`
([ADR-0136](0136-validation-session-prerequisites.md)) are enforced at claim and
provider start, not by submission, so the command does not evaluate them: FR10
asks for exactly the gate's checks. The parity matrix covers both readings of a
missing prerequisite, an unavailable runtime input and a command whose
executable is absent.

*Inspect mode* of `canonical_validation` resolves every declared input
read-only, as `run-live` does. An unavailable one is refused with the gate's
codes and text, without an anomaly, an invalidation or a block (those follow-ups
stay the gate's). Reusable values are delivered; `single_use` values are never
delivered, and the run stops before the first command that uses one:
`not_run` `single_use_input:<index>`, the earlier commands having run, or the
gate's refusal if one of them fails. Admission slots are taken at the local
tier, and a class that stays full for `--max-wait` is `not_run` `admission_full`
with no command run. The batch budget, deferrals, the timeout retry and the
ownership fence are the shared function's own.

### D5 — Prompt pointer

One pointer, defined once in `submission_phase.tmpl` and invoked from the
`submission-phase` block (coder, integration analyst) and from the submit step
of each planning doer, tells authors the command exists and that it runs the
canonical batch. It lands with its own task: it names a command that must exist
first, and its captures and role-budget baseline are regenerated apart from the
engine change.

### D7 — A failing command's output is kept by both runners

When a canonical command fails with output, the shared function keeps the masked
output in the project tool-result store and appends its retrieval command to the
refusal reason. The command keeps this unchanged: the artifact is
content-addressed, written with no telemetry event, and is neither blackboard
state, a receipt nor a branch, so both runners return a byte-identical reason for
the same output and the author can read the full output, the main use of a local
run. This adds a caller of the existing writer, not a writer or a kind of
content; store ownership, layout and lifetime stay ADR-0232's.

## Consequences

- An author learns the gate's verdict, in its words, before submitting; a
  refusal costs a local run, not a review cycle.
- Restructuring the gate around the list moves no verdict; the existing submit,
  acceptance, runtime-input, admission and in-flight tests pass unchanged. A new
  check is one list entry with its row, and the table test refuses an entry
  without one.
- A post-marker refusal reads INVALID_INPUT / `correct_input` / `none` here where
  `submit-for-review` reports STATE_CHANGED / `requery` / `unknown`, with the same
  reason; an author never needs to requery after a local run.
- A branch behind integration, with a tracked change or with a `pre-rebase`
  hook gets `incomplete`, not `pass`: the author rebases or cleans, then reruns.
  The cost is a run that cannot go further until the branch is current.
- Commands using a `single_use` input cannot be exercised locally; their proof
  stays the gate's receipt.
- The command is in the Protection Matrix as a defense against a local check that
  drifts from the gate; the §6 invariant states the property and links the
  parity fixtures that enforce it.

## Alternatives Considered

- **A scratch worktree rebased in place of the branch.** Needs the project's
  `post_worktree_cmd` setup (up to 1800 s by default), runs canonical commands
  outside the author's worktree and duplicates lane provisioning for one command.
- **Rebase and restore.** Rewrites the branch, which the command must not do, and
  an interruption leaves it rewritten.
- **Evaluating the published stage on the unrebased candidate.** An
  approximation reported as the gate's verdict.
- **Rewriting each check for the local path.** The drift this decision exists to
  prevent.
- **Skipping the output artifact locally.** The reason would differ from the
  gate's, or name an artifact never stored; inlining the output in the result
  would be a second rendering of one diagnostic.
- **A new `allowed-operations` entry.** Every project's pipeline file would need
  an edit; role type already says who may submit.
