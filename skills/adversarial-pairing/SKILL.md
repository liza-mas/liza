---
name: adversarial-pairing
description: Coordinate Pairing-mode doer/reviewer sessions through a Markdown blackboard. Use when the user invokes /adversarial-pairing with role and blackboard-path arguments or asks multiple pairing agents to coordinate plan review, implementation, staged code review, and follow-up review rounds without §BRAND_NAME_TITLE§ multi-agent mode.
---

# Invocation

```text
/adversarial-pairing <role-or-reviewer-id> <blackboard-path> [yolo]
```

`role-or-reviewer-id` is `doer`, `reviewer`, or `reviewer-<id>`. `reviewer-<id>` gives the reviewer its stable blackboard ID, for example `reviewer-codex`.

`blackboard-path` is authoritative, may be untracked, and must not be committed unless the user explicitly asks.

`yolo` is doer-only. It sets `yolo: true` and waives doer-side human approval prompts. It does not waive reviewer approvals, validation, stop conditions, merge-conflict handling, unsafe cleanup checks, or user stop instructions.

# Core Rules

- Use this as a Pairing-mode coordination loop: one doer, zero or more reviewers, one shared Markdown blackboard.
- The provided `blackboard-path` is authoritative. Do not use sibling or similarly named blackboards as fallbacks.
- Treat YAML frontmatter as machine-pollable state; treat the Markdown body as durable context for goals, evidence, plans, review notes, validation, and decisions.
- Poll while `phase` is non-terminal unless this protocol says to stop and ask, the state helper says this reviewer may stop at `COMMITTED`, or the user explicitly stops the agent.
- Unexpected events are interruptions, not completion. After write conflicts, protocol violations, interrupted repair, or user-guided recovery, re-read state and resume polling if `phase` is still non-terminal.
- Use `skills/adversarial-pairing/scripts/blackboard_state.py` for frontmatter decisions. Do not parse frontmatter with ad hoc shell/YAML snippets.
- Prefer `skills/adversarial-pairing/scripts/blackboard_op.py` for creation, reviewer registration, review claims, artifact submission, verdicts, coding entry, commit/merge/cleanup milestones, block, and stop.
- Every blackboard write must use `blackboard_op.py` or `blackboard_write.py`; never bypass the sidecar OS lock.
- Helper command contracts in this skill and `blackboard_op.py <operation> --help` are authoritative. Do not inspect helper source during normal operation unless debugging/changing helpers or resolving observed doc/helper contradiction.
- One logical update equals one helper write: body note/artifact plus matching phase, counter, status, timestamp, and verdict changes.
- Respect field ownership. Preserve peer comments and peer-owned agent entries exactly.
- Phase gates are mandatory. Do not advance, create later artifacts, create a coding worktree, or implement unless the current gate is satisfied, `yolo: true` waives only a doer-side human prompt, or the user explicitly waives that exact gate.
- Skill source is `skills/adversarial-pairing/SKILL.md` in the repo. Installed copies under `~/§BRAND_GLOBAL_DIRNAME§/skills/` are derived; edit/sync them only after normal diff, approval, validation, and commit.

# Polling

Poll frontmatter every 60 seconds:

```bash
python3 skills/adversarial-pairing/scripts/blackboard_state.py --path <blackboard-path> --role-or-reviewer-id <role-or-reviewer-id> --json
```

Use the JSON as the routing authority:

- `terminal: true`: stop.
- Reviewer at `COMMITTED` with helper stop instruction: may stop; doer owns merge/cleanup.
- `next.actor` or `next.handoff_to` names this agent: do the indicated work.
- Otherwise: keep frontmatter-only polling.

Do not report `WAITING`, `IDLE`, registration, artifact submission, or review-wait state as completion.

Keep yourself scheduled with the available harness mechanism, such as wake/scheduler/background loop or staying in the command loop. Never make the human type `poll`; emit updates only on state changes, blockers, or user status requests.

# Context Hygiene

Between plan approval and coding, compact or refresh context. Keep goal, current phase/frontmatter, approved plan revision, unresolved reviewer constraints, gates, next action, validation commands, and active invariants. Drop resolved drafts and stale hypotheses. Re-read this skill after compaction. Compaction does not satisfy gates or authorize writes.

# Blackboard State

Use `blackboard_op.py create` for new blackboards. It owns the initial frontmatter shape. Required concepts:

- `phase`: see `Phases`; terminal phases are `CLEANED_UP`, `BLOCKED`, `STOPPED`.
- `yolo`, `work_type`, `rca_required`, `red_test_required`.
- counters: `analysis_revision`, `plan_revision`, `red_test_round`, `code_review_round`.
- audit/worktree fields: `repo_root`, `base_branch`, `base_sha`, `topic_branch`, `commit_sha`, `merged_at`, `merged_into`, `worktree`, `worktree_path`, `worktree_removed`.
- `required_reviewers`: reviewer IDs currently required for gates.
- `agents.<id>`: `role`, `status`, `last_seen`, reviewed counters, and verdict fields.

Allowed values:

- `role`: `doer` or `reviewer`.
- `status`: `DRAFT`, `IDLE`, `WAITING`, `WORKING`, `REVIEWING`, `APPROVED`, `CHANGES_REQUESTED`, `BLOCKED`, `STOPPED`.
- verdicts: `APPROVED`, `CHANGES_REQUESTED`, `COMMENT`, or `null`.

# Ownership

- Doer owns workflow phase/counters, `yolo`, `work_type`, gate booleans, audit fields, and worktree fields except reviewer claim transitions.
- Reviewers own only their own `agents.<id>` fields and may add only their own ID to `required_reviewers` during registration/repair.
- Other `required_reviewers` changes require explicit user instruction.
- Any agent may set `STOPPED` after direct user stop/abort.
- Any agent may set its own `status: BLOCKED`; global `BLOCKED` requires a concrete blocker note in the body.
- Reviewer-owned writes authorized here need no extra human approval, but still use helper/lock rules.
- Before any write, re-read state and verify the target revision/round is current.
- If two writes conflict or a write would overwrite peer state/comments, stop and ask the user how to reconcile.

# Worktree Rules

- Before `CODING`, after prior gates, the doer must create/select a dedicated git worktree and record its absolute path in `worktree`.
- If `worktree` is null before `CODING`, derive `<repo-root>/.worktrees/<blackboard-stem>` from `git rev-parse --show-toplevel` and the blackboard basename without one trailing `.md`.
- Ask before creating that default path unless `yolo: true`. Use a user-provided alternative if given.
- Stop/ask if the derived stem is empty, has unsafe path characters, resolves outside `<repo-root>/.worktrees/`, or collides with an unintended path.
- Do not implement in the main checkout unless the user explicitly approves a no-worktree workflow.
- Once `worktree` is set, run implementation, staging, validation, and review diff commands from that path. Reviewers also diff from `worktree`.
- Before edits, staging, validation, or review diffs, re-run `blackboard_state.py --json` and use its absolute `worktree`. If null, do not edit code.
- Codex: set shell `workdir` to the recorded worktree and apply patches only under that worktree. If `pwd` or `git rev-parse --show-toplevel` shows the main checkout during coding/follow-up, stop before editing.
- `apply_patch` is path-sensitive. Do not patch the main checkout and copy changes later. If a patch lands in the main checkout, stop, report affected files, and wait for explicit repair direction.
- The blackboard may remain outside the worktree.

# Reviewer Registration

- If invoked as `reviewer-<id>`, use `<id>`.
- If invoked as `reviewer` and identity is ambiguous, ask before registering.
- If no own agent entry exists, self-register under `agents.<id>` and add the same ID to `required_reviewers` in one locked write.
- If your agent entry exists but your ID is absent from `required_reviewers`, add yourself in the same locked write as status/last_seen update.
- Do not ask for human approval for this self-registration unless identity is ambiguous.
- Once required, a reviewer remains required unless explicit user instruction removes them.
- The doer usually does not know reviewer IDs before registration and must not invent them.
- If the user/launcher supplies expected reviewer IDs at creation, record them in `required_reviewers`. If any remain absent at a gate, wait. Proceeding requires explicit user/no-review waiver; record a body note naming absent IDs and remove those IDs from `required_reviewers` in the same locked write.
- Late reviewer during submitted/reviewing phase blocks that current gate until they record a current verdict.
- Late reviewer during doer-owned work phase becomes required for the next reviewable gate and all later gates.

# Lock Fallback

Use raw full-file writes only for operations not covered by `blackboard_op.py`.

1. Run `blackboard_state.py --json`; copy `sha256`.
2. Copy current blackboard to a temp file and make a scoped edit there.
3. Validate temp content with `blackboard_state.py --json`.
4. Write with:

```bash
python3 skills/adversarial-pairing/scripts/blackboard_write.py --path <blackboard-path> --content-file <tmp-content-file> --operation <short-operation-name> --expect-sha256 <sha256-read-before-edit>
```

For authorized creation, use `--create-if-missing`.

The writer locks `<blackboard-path>.lock`, re-reads under lock, checks SHA, atomically replaces, and writes owner diagnostics. If stale/timeout occurs, re-read and restart from current state. After repeated SHA conflicts, stop blind retries; wait for a quiet phase, switch to a semantic helper, or ask for coordination. Failed writes/retries are failed attempts; report them honestly.

Use UTC ISO-8601 timestamps ending in `Z`. Update body first and frontmatter last in prepared content. Verify prepared frontmatter has matching phase/status/verdict/counter before writing. Never hand-author a complete blackboard from scratch when preserving peer content matters.

Claude: create temp content with the Write tool. Bash commands must be one simple executable invocation; no compound shell, heredocs, command substitution, shell variables, glob/brace expansion, `cat`, `printf`, or inline file-writing snippets.

# Helper Operations

Use:

```bash
python3 skills/adversarial-pairing/scripts/blackboard_op.py <operation> --help
```

Supported operations:

- `create`
- `register-reviewer`
- `claim-review`
- `submit-artifact --target analysis|plan|red-test|code`
- `submit-followup-review`
- `submit-verdict`
- `request-changes`
- `enter-coding`
- `ready-to-commit`
- `mark-committed`
- `mark-merged`
- `mark-cleaned-up`
- `block`
- `stop`

Semantic ops lock, re-read, apply scoped deltas, enforce operation-specific phase/role/counter predicates, validate through the state parser, write atomically, and return summarized state. They do not prove caller identity or retry semantic deltas beyond lock acquisition; callers still follow the contention rule. Pairing-mode agents are trusted to pass their own ID.

# Phases

Lifecycle:

`DRAFT` -> `ANALYZING` -> `ANALYSIS_SUBMITTED`/`REVIEWING_ANALYSIS` -> `ANALYSIS_APPROVED` or `ANALYSIS_CHANGES_REQUESTED` -> `PLANNING` -> `PLANNING_SUBMITTED`/`REVIEWING_PLAN` -> `PLAN_APPROVED` or `PLAN_CHANGES_REQUESTED` -> optional `RED_TESTING` -> `RED_TEST_SUBMITTED`/`REVIEWING_RED_TEST` -> `RED_TEST_APPROVED` or `RED_TEST_CHANGES_REQUESTED` -> `CODING` -> `CODE_SUBMITTED`/`REVIEWING_CODE` -> `CODE_CHANGES_REQUESTED`/`FOLLOWUP_REVIEW` until approved -> `READY_TO_COMMIT` -> `COMMITTED` -> `MERGED` -> `CLEANED_UP`.

Terminal/error phases: `CLEANED_UP`, `BLOCKED`, `STOPPED`.

Phase meanings:

- `DRAFT`: blackboard exists; doer has not started planning.
- `ANALYZING`: doer performs RCA.
- `*_SUBMITTED`: artifact is ready for review.
- `REVIEWING_*`: reviewer has claimed/checking current artifact.
- `*_CHANGES_REQUESTED`: doer revises current artifact.
- `*_APPROVED`: required reviewers have no blocking comments for that gate.
- `RED_TESTING`: doer writes failing test/reproduction.
- `CODING`: doer implements approved plan in recorded worktree.
- `CODE_CHANGES_REQUESTED`: doer addresses code review comments as unstaged follow-up changes.
- `FOLLOWUP_REVIEW`: reviewers inspect unstaged follow-up diff.
- `READY_TO_COMMIT`: code review complete; doer owns commit/rebase/merge/cleanup.
- `COMMITTED`: reviewed commit exists on topic branch.
- `MERGED`: base branch contains reviewed commit.
- `CLEANED_UP`: dedicated worktree removed and merged topic branch deleted.
- `BLOCKED`: user input/external change required.
- `STOPPED`: user stopped/aborted workflow.

# Review Gates

- Reviewable phases remain reviewable until all required reviewers record current verdicts for the active revision/round.
- Missing/stale required reviewer records mean incomplete review.
- `APPROVED`: all required reviewers approved current revision/round.
- `CHANGES_REQUESTED`: at least one required reviewer requested changes for current revision/round.
- `COMMENT`: non-approving/non-blocking; no approval predicate is satisfied by comments alone.
- `submit-verdict`/`request-changes` moves completed analysis/plan/red-test gates to `*_APPROVED` or `*_CHANGES_REQUESTED`, and moves code changes to `CODE_CHANGES_REQUESTED`. Code approval is followed by `ready-to-commit`. The doer must not perform verdict-owned transitions separately.
- The doer may submit before reviewers register. If `required_reviewers` stays empty in a submitted/reviewing phase, wait for registration or obtain explicit no-review approval before advancing.

# Doer Protocol

Startup:

- If blackboard exists, immediately run `blackboard_state.py --json`.
- If missing, create it only when the user asked the doer to create it; otherwise stop and ask.
- Creation must use `blackboard_op.py create` or `blackboard_write.py --create-if-missing`; if the file appears meanwhile, re-read instead of overwriting.
- With `yolo`, create/update with `yolo: true`. Without `yolo`, create with `yolo: false` and do not downgrade an existing `yolo: true` unless the user explicitly asks.

Human gates when `yolo` is false/missing:

1. Before leaving `DRAFT` for `ANALYZING` or `PLANNING`, ask approval to begin from blackboard contents.
2. Before submitting RCA, show RCA and ask approval to submit for adversarial review.
3. Before submitting plan, show plan and ask approval to submit for adversarial review.
4. After `PLAN_APPROVED`, ask before coding unless prior approval explicitly included permission to proceed after reviewer approval.
5. Before each post-review git state change: commit, rebase, merge, worktree deletion, topic-branch deletion.

Workflow:

- For debugging work, use the debugging skill; treat RCA as distinct before planning when `rca_required: true`.
- Do not enter `PLANNING` from `ANALYSIS_SUBMITTED` unless the user explicitly waives the RCA approval gate.
- No extra human approval is required for `RED_TESTING -> RED_TEST_SUBMITTED`; it is part of the approved debugging workflow.
- If `red_test_required: true`, do not implement until a test/reproduction fails for the expected reason and reviewers approve it. If none is practical, ask user to approve an alternate validation path.
- During coding, follow normal Pairing approval/validation rules. If `yolo: true`, treat doer-side approval prompts as pre-approved.
- Do not address review comments until all required reviewers complete the current round.
- When a code round requests changes, stage the reviewed baseline before follow-up edits so reviewers can compare staged baseline vs unstaged follow-up. This workflow-specific staging needs no extra user intervention.
- Do not stage follow-up changes; reviewers inspect unstaged diff in `FOLLOWUP_REVIEW`.
- `APPROVED` means no blocking findings. Before commit, implement low-risk, in-scope, validation-covered suggestions or record deferral rationale. Implemented suggestions require `FOLLOWUP_REVIEW` and all required reviewers approving the new round.
- After final code approval, commit on topic branch, rebase onto base, merge into base, then delete dedicated worktree and merged topic branch.
- If cleanup fails after `MERGED`, move to `BLOCKED` with a note that merge succeeded and cleanup failed.

Merge/validation details:

- Before merge, inspect the base checkout. Stop for tracked dirty files unless explicitly known safe. For untracked files, compare incoming changed paths against untracked base paths; stop on path collision.
- Record the environment wrapper that made validation pass and reuse it for commit hooks. Keep stack-agnostic; Go `GOPATH`/`GOCACHE` is only an example.
- Report merge command exit status separately from post-merge hook/index diagnostics. Record hook command/source when known, warning output verbatim, and classification.

# Reviewer Protocol

Startup:

- If invoked with `yolo`, stop: `yolo` is doer-only.
- If blackboard is missing, poll for creation. After the configured retry budget/timeout, set own status `BLOCKED` if possible or report idle/blocker state with the missing path.
- If state helper says `needs_registration` or `needs_required_registration`, register before waiting for review work unless phase is terminal.

Review:

- Use `claim-review` when taking a submitted artifact.
- Append notes chronologically to the relevant body section and update own frontmatter fields in one helper write. Do not insert newest notes before older entries.
- Reviewer claim transitions, notes, and verdict/status updates authorized here need no human approval.
- For `ANALYSIS_SUBMITTED`/`REVIEWING_ANALYSIS`, review RCA quality: evidence, contradictions, falsifiability, root cause, and whether fixing the cause would make the failure impossible.
- For `PLANNING_SUBMITTED`/`REVIEWING_PLAN`, review the latest plan revision.
- For `RED_TEST_SUBMITTED`/`REVIEWING_RED_TEST`, verify the test/reproduction fails for the expected reason, would pass if fixed, tests behavior, and does not corrupt expectations.
- For `CODE_SUBMITTED`/`REVIEWING_CODE`, review staged changes:

```bash
git -C <worktree-from-blackboard_state> diff --cached --name-only
git -C <worktree-from-blackboard_state> diff --cached --stat
git -C <worktree-from-blackboard_state> diff --cached
```

- For `FOLLOWUP_REVIEW`, review unstaged follow-up changes:

```bash
git -C <worktree-from-blackboard_state> diff --name-only
git -C <worktree-from-blackboard_state> diff --stat
git -C <worktree-from-blackboard_state> diff
```

- Use the `code-review` skill for code review. Label reviews with target: analysis revision, plan revision, red-test round, staged code round, or unstaged follow-up round.

# Markdown Body

Use body sections for durable context. Standard sections: `Goal`, `Evidence`, `Plan Revisions`, `Plan Reviews`, `Implementation Notes`, `Code Review Rounds`, `Validation`, `Decisions`. Include `Root Cause Analysis` and `Red Tests` only when debugging or when corresponding gates are enabled.

Append new entries at the bottom of the relevant section. Do not rewrite history except obvious formatting before reviewers act.

# Stop Conditions

Stop and ask the user if:

- phase and body contradict;
- frontmatter is missing required fields or has invalid enum values;
- reviewable phase has empty `required_reviewers` and no explicit user-approved no-review workflow;
- required reviewer records are missing, stale, or contradictory for current revision/round;
- a reviewer reviewed an obsolete artifact revision/round;
- doer needs a mandatory human gate and `yolo` is false/missing;
- diff scope is ambiguous: staged, unstaged, or full pending state;
- blackboard path appears outside intended repo/worktree;
- write would overwrite another agent's state/comments;
- derived worktree is unsafe or collides;
- merge conflicts, dirty worktree cleanup risk, ambiguous branch identity, or cleanup outside dedicated worktree/topic branch occurs.
