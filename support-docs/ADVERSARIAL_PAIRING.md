# Adversarial Pairing

Adversarial Pairing is the middle step between ordinary Pairing mode and the
full §BRAND_NAME_TITLE§ MAS. Use it when one agent should implement and multiple reviewers
should challenge the work, but a full autonomous sprint would be too heavy.

It runs multiple Pairing-mode sessions against a shared Markdown blackboard. Each
role typically runs in its own dedicated interactive pane: one pane for the doer
and one pane for each reviewer. The typical setup is one doer and several
reviewers on different models, so review disagreement exposes
model-specific blind spots. By default, the human remains the approval authority;
the blackboard coordinates doer/reviewer state, submitted artifacts, review
notes, validation output, and decisions.

The doer session is the only coding/control session and still stops at normal
Pairing approval gates before state changes unless started with `yolo`.
Reviewer sessions run autonomously: they read the blackboard, inspect submitted
artifacts, and write review notes or verdicts without asking the human for each
review action. The doer does not need to know reviewer IDs before creation. When
a reviewer session registers, it also adds itself to `required_reviewers`; late
reviewers are accepted and their responsibility takes effect from the current
stage onward.

Use it as:

```text
/adversarial-pairing <role-or-reviewer-id> <blackboard-path> [yolo]
```

In Codex interactive sessions, use `$adversarial-pairing ...` instead of a
leading slash so the text is submitted as a normal prompt rather than handled as
a Codex TUI command.

`role-or-reviewer-id` is `doer`, `reviewer`, or `reviewer-<id>`. Use
`reviewer-<id>` when you want the agent to receive both its reviewer role and
the stable ID it should use when registering in the blackboard. If you are
arranging panes manually, run each invocation in its own pane:

```text
/adversarial-pairing doer §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md
```

For multiple reviewer sessions, run in additional panes:

```text
/adversarial-pairing reviewer-claude §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md
/adversarial-pairing reviewer-codex §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md
```

If you want WezTerm to do the spawning for you, use the launch command. This
starts interactive doer plus reviewer CLI sessions in one WezTerm window with
the `$adversarial-pairing ...` invocation as each session's initial prompt. If
the blackboard does not exist yet, pass `--goal` so §BRAND_NAME_TITLE§ can initialize it
before reviewer panes start:

```bash
§BRAND_BINARY_NAME§ launch wezterm adversarial-pairing §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md \
  --goal "Fix retry-client behavior"
```

Defaults are three Codex panes: `--doer-cli codex` plus reviewers `codex` and
`codex-2=codex`. Customize reviewers with repeated `--reviewer` flags. Use
`id=cli` when the stable blackboard reviewer ID should differ from the CLI name:

```bash
§BRAND_BINARY_NAME§ launch wezterm adversarial-pairing §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md \
  --goal "Fix retry-client behavior" \
  --doer-cli claude \
  --reviewer claude \
  --reviewer openai=codex
```

When the blackboard already exists, omit `--goal` to reuse it. Reviewer panes
may start before the doer has finished creating the blackboard; reviewers should
wait and poll for creation rather than asking the human to restart or type
`poll`.

WezTerm prompt injection waits 2 seconds before sending the initial
`$adversarial-pairing ...` prompt to each pane. If a CLI starts slowly because
it loads MCP servers or other startup hooks, increase that wait with
`--prompt-delay`, for example `--prompt-delay 6s`.

**CMUX support:** §BRAND_NAME_TITLE§ also supports CMUX as an alternative to WezTerm for
adversarial-pairing launches. Use `§BRAND_BINARY_NAME§ launch cmux adversarial-pairing` with
the same role, reviewer, and goal flags. The doer-only `--yolo` flag is also
available:

```bash
§BRAND_BINARY_NAME§ launch cmux adversarial-pairing §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md \
  --goal "Fix retry-client behavior"
```

CMUX sends the `$adversarial-pairing ...` prompt as text to each pane and
submits it with a real enter key event, avoiding the TUI slash command issue
that affected the initial WezTerm implementation.

Use `yolo` only on the doer session when you want the doer to proceed through
doer-side human approval gates without pausing:

```text
/adversarial-pairing doer §BRAND_PROJECT_DIRNAME§/adversarial/retry-client.md yolo
```

`yolo` does not waive reviewer approvals, validation, stop conditions,
merge-conflict handling, or user stop instructions.

Tip: you can step in without ending the run. Ask the doer pane to pause polling
while you inspect state or redirect the workflow, then ask it to resume polling
when you want it to continue from the current blackboard phase.

When the blackboard file does not exist, or exists but does not yet contain the
goal, include a short goal paragraph in the doer session's first message along
with the `/adversarial-pairing doer <blackboard-path>` command. The doer records
that goal in the blackboard before planning so reviewer sessions share the same
task frame.

The blackboard path may be untracked and should not be committed unless you
explicitly want it preserved. During coding, the doer normally uses a dedicated
git worktree recorded in the blackboard; reviewers review the staged or
unstaged diff for the current review round. All implementation edits, including
`apply_patch`, must target that worktree. If an edit lands in the main checkout,
stop and report the affected files before repairing.

Typical flow:

1. The doer records the goal, evidence, and plan in the blackboard.
2. Reviewers, usually on different models, challenge the submitted plan and record verdicts.
3. After plan approval and human approval to code, the doer implements. In `yolo` mode, the doer treats the human approval step as delegated by invocation.
4. The doer submits the candidate diff for code review.
5. Reviewers request changes or approve. Suggestions are non-blocking, but the
   doer considers them before commit: implement low-risk in-scope suggestions
   with a follow-up review, or record a deferral rationale.
6. After reviewer approval, the doer commits on the topic branch (`COMMITTED`),
   merges into the base branch (`MERGED`), then deletes the dedicated worktree
   and merged topic branch (`CLEANED_UP`). Without `yolo`, the doer asks before
   those git state changes; with `yolo`, it proceeds unless a stop condition
   applies.

The skill includes `skills/adversarial-pairing/scripts/blackboard_state.py` for
agent-side polling and status decisions. Agents use it instead of hand-parsing
frontmatter; it reports phase, `yolo`, required reviewers, agent status, active
review target, missing or stale verdicts, the recorded worktree, and the next
actor. Routine blackboard mutations should use
`skills/adversarial-pairing/scripts/blackboard_op.py`; reserve raw full-file
writes for operations not covered by the semantic helper.

For debugging work, the blackboard can require explicit root-cause analysis and
red-test gates before implementation. That gives you the MAS-style discipline of
diagnosis review and failing-test review without handing the whole task to the
autonomous pipeline.

Best for: high-stakes Pairing-mode changes, complex debugging, architectural
edits that need a second agent's review, and situations where the full MAS would
be disproportionate.
