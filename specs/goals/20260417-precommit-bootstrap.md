# Pre-Commit Bootstrap for Greenfield Projects

## Problem Statement

`internal/prompts/templates/blocks/commit_workflow.tmpl:3` instructs coders to run pre-commit on every commit. The instruction assumes `.pre-commit-config.yaml` exists. In a greenfield project (`liza init` + vision doc, no pre-existing tooling), the file is absent.

**Depends on `20260417-precommit-hook-composition.md`** — without hook chaining in `.liza-hooks/pre-commit`, the config file is never auto-executed by the worktree commit hook. Bootstrap is a no-op without that spec landing first.

### Evidence

This is a **proactive hardening** change. The failure modes are mechanical consequences of an absent file plus an instruction that assumes its presence — they do not require empirical reproduction to justify the goal-level design.

**Rule 5 waiver (goal spec only):** implementation should reproduce a greenfield run and capture observed failure modes; may be deferred under TECH_DEBT when hypothesized modes are pre-accepted on inspection (see Acceptance Criteria). At the goal level, hypothesized modes drive scope:
- Coders inventing `.pre-commit-config.yaml` ad-hoc inside an unrelated coding task.
- Parallel coders producing conflicting configs.
- Coders silently skipping pre-commit when it errors with "no config".
- DoD step "Pre-commit passes on touched files" (CORE.md Rule 3) becoming vacuous.
Observed modes recorded in §Observed Failure Modes — Reproduction Run after reproduction lands.

If reproduction shows the actual failure mode is different from any of the above, the ratified design is revisited before implementation proceeds — this spec does not bind the implementation against reality.

## Preconditions

### Prompt wording to narrow

`internal/prompts/templates/base_prompt.tmpl:36` says: *"NEVER attempt to install, bootstrap, or fix system-level tooling."* The line sits under the BASH CONSTRAINTS block (alongside "NEVER use $()…", "NEVER combine cd and git…") — the original intent targets system package installs, language runtimes, and IDE setup, not project config files. Replacement text:

> NEVER install OS packages, language runtimes, IDE tooling, or global developer tools by default. Project-level config files (`.pre-commit-config.yaml`, `.editorconfig`, lint configs) are project work. Tools required to run those configs may be provisioned only when the task's explicit scope authorizes a project-scoped setup path; otherwise mark BLOCKED rather than mutating the host environment.

`base_prompt.tmpl` is on the implementation surface.

### Composition with post-submit commit guard

The post-submit commit guard (`fcb7edc7`, `f840debe`) and project pre-commit are not orthogonal — both want to run on `git commit` in a worktree. Composition is owned by `20260417-precommit-hook-composition.md`. This spec assumes that composition is in place.

### `pre-commit` binary install

The chain spec assumes `pre-commit` is on PATH; bootstrap's `done_when` (`pre-commit run --all-files`) requires the same. Greenfield projects may not have it installed.

**Resolution: project-scoped provisioning only.** The bootstrap task may install `pre-commit` only via a project-scoped mechanism — a project dep-manager entry (e.g. `uv add --dev pre-commit`, `npm install --save-dev pre-commit`, requirements-dev.txt + venv), a script in the repo, a devcontainer manifest. Host-level installs (`brew install`, `pip install --user`, `uv tool install`, `apt-get install`) are forbidden — they are exactly what the narrowed `base_prompt.tmpl:36` fences off.

Bootstrap task's `desc` includes:

> This task may provision `pre-commit` only via a project-scoped mechanism already available in the repo or explicitly defined by the task/config. Do not install OS packages or unrelated global tooling. If no authorized project-scoped path exists, mark BLOCKED.

If no project-scoped path exists, the coder marks BLOCKED. Planner-orchestrator may rescope by emitting a prerequisite `setup-dep-manager` task and re-emitting bootstrap with the new dep — this rescope is allowed but not mandated by this spec. Collapsing dep-manager and pre-commit bootstrap into one task is rejected (breaks idempotency and muddies failure diagnosis).

**Rationale:** the architect authorizes scope, not host-mutation policy. Permitting host-level installs as a base-prompt carve-out would set a precedent that erodes the contract every time a tool is missing.

## Design

The architecture step (ADR-0056) is the first agent step where the project's tech stack is decided. Bootstrap of stack-dependent tooling can therefore be planned no earlier than the architecture step.

### Q1: Architect emits bootstrap as `output[0]`

The architect's `output[]` becomes M code-planning task definitions per ADR-0036. The bootstrap entry produces a code-planning task → coding task chain dedicated to `.pre-commit-config.yaml`. All other architect `output[]` entries declare `depends_on: ["0"]`. **Phase-gate inheritance** (ADR-0048, `computeInheritedDeps` in `internal/ops/proceed.go:1076`) auto-propagates the dependency from code-planning to coding tasks — no new primitive needed.

- Uses existing `output[]` channel and existing dependency-inheritance machinery.
- Architect has full stack context (sees `arch_ref`).
- Normal review gates apply.

**Cost (accepted):** two extra review pairs (code-plan-review, code-review) for what is ultimately a config file. The trade-off is accepted because the alternatives below require new pipeline primitives — paying ceremony cost for a known design vs. paying engineering cost for a new one.

**Ruled-out alternatives** (preserved here so the rationale survives):
- *Code-planner emits bootstrap.* ADR-0056 created the architecture step explicitly to prevent code-planners from making cross-scope structural decisions. Bootstrap is repo-wide, not per-scope. M parallel code-planners each emitting bootstrap reintroduces the failure mode ADR-0056 fixed.
- *Pipeline-injected bootstrap (new task-creation path in `proceed.go`).* Would require a new task-creation primitive: review parity for a task without `output[]` provenance, `parent_task` linkage, `transitions_executed` bookkeeping, checkpoint visibility, replan handling, and a deviation from the ADR-0036 invariant that tasks come from agent `output[]`. Substantially heavier than reusing `output[]` + phase-gate inheritance.

### Q2: Idempotency — plan-time, dedup, execution-time

Three detection points, backed by one new typed marker.

**New primitive: `kind: bootstrap-precommit`.** A typed field on `OutputEntry` (`internal/models/task.go`) AND propagated to the persisted `Task`. This is the stable dedup key — string-matching on description/scope is rejected as too brittle.

**ADR-0036 impact.** ADR-0036 currently defines `OutputEntry` as a fixed four-field schema ("no extensibility beyond the four fields"). Adding `kind` extends that schema. Implementation must either amend ADR-0036 or write a superseding ADR; leaving it untouched would make the architecture docs immediately stale.

**Plan-time existence check (architect prompt context).** Two booleans added to the existing `RoleContextData` in `internal/prompts/role_context.go`, populated only for the architect role:
- `PreCommitConfigExists` — integration-branch HEAD has `.pre-commit-config.yaml`.
- `PreCommitBootstrapInFlight` — any non-terminal task repo-wide carries `kind: bootstrap-precommit`.

Architect omits the bootstrap entry when either is true. Backed by helpers in a new `internal/precommit/` package:
- `precommit.ConfigExistsOnIntegration(projectRoot, branch) (bool, error)` — uses git plumbing (e.g. `git cat-file -e <branch>:.pre-commit-config.yaml`), not direct filesystem read of the main working tree (which may include uncommitted human drift). Precedent: `checkSpecFileExists` in `internal/statevalidate/validate.go:92`.
- `precommit.BootstrapInFlight(state *models.State) bool` — repo-wide scan of non-terminal tasks by typed marker. "Non-terminal" includes BLOCKED (a blocked bootstrap will eventually merge once rescoped, so it genuinely is in flight).

**Rescope invariant.** When the planner-orchestrator rescopes a BLOCKED bootstrap task (e.g. after it blocks waiting for a prerequisite like `setup-dep-manager`), it MUST mark the blocked task `SUPERSEDED` as part of that rescope, before or simultaneous with emitting a replacement. Without the SUPERSEDE, `BootstrapInFlight` stays true and the replacement is correctly suppressed by dedup — so forgetting the SUPERSEDE surfaces as a dedup mis-fire, not silent success with two parallel bootstraps. This aligns with the existing rescope-via-SUPERSEDED convention in `specs/architecture/roles.md:111`.

**Terminology**: this is prompt-context enrichment / repo-state inspection. It is **not** a `pipeline.Resolver` responsibility — keep it out of `pipeline.Resolver` to avoid muddying that boundary.

**Failure handling**: if either helper errors (e.g. git lookup failure), the context builder returns the error; the supervisor/orchestrator converts it to an explicit task-blocking outcome. Not silent omission, not opaque crash.

**Authoritative dedup at proceed-time (`proceed.go`).** B (planning-time omission) is not sufficient on its own — uniqueness cannot depend on prompt compliance, and there is a render-to-proceed race. `proceed.go` performs a final dedup check on `kind: bootstrap-precommit` before creating children: if a non-terminal sibling/repo-wide task with that marker exists, skip child creation for that entry.

**Execution-time** (coder): when claiming the bootstrap task, re-check worktree disk + integration branch. If the file appeared after planning (human added it, or another in-flight task merged), the coder calls `liza mark-blocked` with reason "config already present at execution time". The planner-orchestrator handles via standard rescope flow (`SUPERSEDED`, per `specs/architecture/roles.md:111`). Coders cannot mark tasks SUPERSEDED themselves (`internal/prompts/templates/base_prompt.tmpl:54`).

"Exists" rule: file presence is sufficient. Content adequacy is the project's responsibility — empty or malformed config still counts as "present". Coders fail fast on `pre-commit run`, surfacing the bad config as a normal task failure rather than silently re-bootstrapping.

### Q3: Hook content — α (stack-derived) with constraints

Architect specifies a stack-appropriate hook set in free-form plan output, **never via prompt-template defaults or runtime heuristics**.

**Constraints:**
- Architect picks hook *categories* and *concrete hooks*. Version-selection policy remains out of scope (defer to hook-selection task or ADR).
- Worked example for at least one greenfield stack class lives in this spec / docs only — **never in prompts**. A concrete example in the architect prompt would anchor output too hard and drift toward a de facto default, eroding G1.1 even when labeled "illustrative."
- The architect prompt requires include of the install-authorization clause when emitting `kind: bootstrap-precommit` (per Preconditions / `pre-commit` binary install).

**Rationale (chosen):** best alignment with existing role boundaries (architect already makes repo-wide structural decisions per goal) and strongest day-1 protection. Not "least ceremony."

**Blast radius (acknowledged):** a bad α choice can stall the whole coding cohort until the bootstrap chain is fixed or respecified. Phase-gate inheritance gates downstream tasks on bootstrap approval, so a broken bootstrap is not a local coder problem — it is a cohort-wide block. Acceptable, but explicit.

**Ruled out:**
- *β minimal universal:* "add stack hooks later" has no clean trigger in the current pipeline; in practice it becomes "maybe never."
- *γ hybrid:* doubles ceremony (two bootstrap chains, four review pairs) and complicates dependency indexing for marginal benefit over α.

### Replan interaction

If the goal is re-planned after bootstrap is approved but before integration merge, the second architect invocation re-runs the plan-time checks (Q2). The integration-HEAD check still returns "config absent"; the `BootstrapInFlight` check returns true (the prior bootstrap task has not yet reached a terminal state), so the architect omits the entry. Belt-and-braces: even if the architect emits anyway, `proceed.go`'s authoritative `kind`-based dedup (Q2) skips child creation. Cross-goal parallelism (two goals racing on bootstrap) is handled by the same repo-wide scan.

### Brownfield indexing

When bootstrap is omitted (config already exists), no synthetic `depends_on: ["0"]` is added to other entries. Indices renumber from 0; inter-scope dependencies revert to whatever the architect explicitly emits. The "bootstrap-first" gating is a property of the bootstrap entry's existence, not a structural assumption other entries depend on.

### Bootstrap failure semantics

If the bootstrap coding task fails (e.g. `pre-commit run --all-files` fails on the seed config):
- Standard BLOCKED escalation per `specs/protocols/task-lifecycle.md:158` (Coder BLOCKED → planner notified → may rescope). No new escalation primitive.
- Phase-gate inheritance already prevents downstream tasks from claiming until bootstrap is approved.

No degraded "no-hooks" mode. The contract requires pre-commit; failing to bootstrap it is a real failure, not a soft skip (Rule 14).

## Acceptance Criteria

Implementation is "done" when:
1. **Greenfield run reproduced** and observed failure modes captured, OR hypothesized failure modes accepted on inspection with a TECH_DEBT entry covering deferred empirical reproduction. In either case the Observed Failure Modes section must be populated (with an evidence pointer of either reproduction artifacts OR "hypothesis inspection only").
2. Q3 design (α with constraints) implemented; architect prompt requires the install-authorization clause when emitting `kind: bootstrap-precommit`.
3. `internal/precommit/` package added with `ConfigExistsOnIntegration` and `BootstrapInFlight` helpers; `RoleContextData` extended with the two booleans, populated only for the architect role.
4. `kind` field added to `OutputEntry` and persisted `Task`; `proceed.go` performs authoritative `kind`-based dedup before child creation; cross-goal parallelism covered by repo-wide scan.
5. `base_prompt.tmpl:36` replaced with the narrowed wording (see Preconditions / Prompt wording to narrow). Touch list: `base_prompt.tmpl`, architect prompt template, bootstrap task's `done_when` phrasing, and `specs/architecture/ADR/0036-structured-task-output-and-scope-extensions.md` (amend or supersede to cover the `kind` field extension).
6. Context-builder errors surface as task-blocking outcomes via the orchestrator (not silent omission, not opaque crash).

## Worked Example (Python + uv)

For a greenfield Python project whose vision doc selects `uv` as the dep manager, the architect's `output[0]` might look like (free-form prose, not templated):

```
{
  "kind": "bootstrap-precommit",
  "desc": "Bootstrap pre-commit for this Python+uv project. Add pre-commit
    as a dev dependency via `uv add --dev pre-commit`. Write
    .pre-commit-config.yaml at repo root with hooks: trailing-whitespace,
    end-of-file-fixer, check-yaml, check-merge-conflict, ruff (lint+format),
    and mypy (type check). Validate with `uv run pre-commit run --all-files`.

    This task may provision pre-commit only via a project-scoped mechanism
    already available in the repo or explicitly defined by the task. Do not
    install OS packages or unrelated global tooling. If no authorized
    project-scoped path exists, mark BLOCKED.",
  "done_when": "uv run pre-commit run --all-files exits 0 against an empty
    repo state, and pyproject.toml records pre-commit as a dev dependency.",
  "scope": ".pre-commit-config.yaml, pyproject.toml, uv.lock"
}
```

Other architect `output[]` entries declare `depends_on: ["0"]`. Phase-gate inheritance gates downstream coding tasks on bootstrap merge.

This example lives in this spec for documentation; **it is never surfaced in agent prompts** (would anchor architect output and erode G1.1).

## Greenfield Reproduction Procedure

### Setup

1. Choose a scratch directory path outside any liza-managed worktree and export it. Recommended pattern: a date-suffixed path under `/tmp` or another writable scratch area. Example: `export REPRO_ROOT=/tmp/liza-precommit-repro-20260417-001`. Use any unique path; do NOT use command substitution to derive it. Create both project roots: `mkdir -p "$REPRO_ROOT/proj/specs/vision"` then `mkdir -p "$REPRO_ROOT/proj-parallel/specs/vision"`.
2. Initialize the primary greenfield project (no `cd`; use `git -C` and absolute paths throughout):
   - `git init "$REPRO_ROOT/proj"`
   - Use a file-writing tool (not shell heredoc) to create `"$REPRO_ROOT/proj/README.md"` (single line, e.g. "greenfield repro project") and `"$REPRO_ROOT/proj/specs/vision/greenfield.md"` (vision describing one trivial feature, e.g. "add a `hello` script printing 'hi'").
   - Stage explicitly by name: `git -C "$REPRO_ROOT/proj" add README.md specs/vision/greenfield.md`
   - Commit using a heredoc on stdin (the contract-permitted form for git commits):
     ```
     git -C "$REPRO_ROOT/proj" commit -F - <<'EOF'
     initial: README + greenfield vision
     EOF
     ```
   - Repeat the four bullets above for `"$REPRO_ROOT/proj-parallel"` (substituting `proj-parallel` for `proj` in every path).
3. Initialize liza in each project: `liza init "$REPRO_ROOT/proj"` then `liza init "$REPRO_ROOT/proj-parallel"`. Verify each project's state independently (one `test` invocation per assertion — do NOT chain with `&&` across the boundary):
   - `test -f "$REPRO_ROOT/proj/.liza/state.yaml"` (exit 0 expected)
   - `test ! -f "$REPRO_ROOT/proj/.pre-commit-config.yaml"` (exit 0 expected)
   - Same two `test` commands against `"$REPRO_ROOT/proj-parallel"`.
   - Confirm `command -v pre-commit` outside any project-scoped venv either returns nothing (exit 1) or returns a path the procedure agrees to ignore for this run.
4. Configure verbose logging for both supervisor cycles: `export LIZA_LOG_LEVEL=debug` and `export LIZA_TEE_AGENT_OUTPUT=1` (or the current equivalent — `liza --help` lists the active env-var names; the capture script in §5.4 picks up whichever log/tee paths the supervisor wrote to).

### Execution

5. Start the supervisor for the primary cycle: `liza supervise "$REPRO_ROOT/proj"` (consult `liza --help` for the current subcommand name; substitute the equivalent if `supervise` is renamed). Run it under a separate terminal or as a background job (`liza supervise "$REPRO_ROOT/proj" > "$REPRO_ROOT/proj/supervisor.stdout.log" 2>&1 &`). Allow it to run until either (a) the integration task reaches a terminal state, or (b) any task transitions to BLOCKED with a reason mentioning pre-commit, missing config, or install authorization.
6. Start the parallel cycle the same way: `liza supervise "$REPRO_ROOT/proj-parallel" > "$REPRO_ROOT/proj-parallel/supervisor.stdout.log" 2>&1 &`. The two supervisor processes operate against fully independent project trees (separate `state.yaml`, separate `.worktrees/`) so they cannot cooperate.
7. Wait for both cycles to reach a terminal state. Inspection commands (each is a single contract-compliant invocation — no chaining):
   - `liza --project "$REPRO_ROOT/proj" status --json`
   - `liza --project "$REPRO_ROOT/proj-parallel" status --json`
   - Repeat at intervals (operator's discretion) until both report a terminal sprint state or all tasks BLOCKED.

### Capture

8. Run the capture script once per cycle, passing `REPRO_ROOT` and the project subdirectory name as separate arguments: `bash scripts/repro/precommit-bootstrap-greenfield.sh "$REPRO_ROOT" proj` then `bash scripts/repro/precommit-bootstrap-greenfield.sh "$REPRO_ROOT" proj-parallel`. Each invocation is a single shell command — internal multi-step capture happens inside the script, so BASH CONSTRAINTS apply only to the invocation form (which is plain).
9. The script writes per-cycle artifacts under `"$REPRO_ROOT/observations/<cycle>/"` (where `<cycle>` is `proj` or `proj-parallel`), containing at minimum:
   - `state-snapshots/state-postinit.yaml`, `state-snapshots/state-postarchitect.yaml`, `state-snapshots/state-final.yaml` (the script copies `.liza/state.yaml` at three checkpoints — operator triggers the snapshots by re-running the script with a snapshot subcommand, OR the supervisor's own audit hooks if available).
   - `agent-outputs/` — full copy of `<cycle>/.liza/agent-outputs/`.
   - `supervisor.stdout.log` — copy of the supervisor's stdout/stderr capture from step 5/6.
   - `worktree-git-logs/<worktree-id>.log` — output of `git -C "$REPRO_ROOT/<cycle>/.worktrees/<worktree-id>" log --all --oneline --graph` for each worktree.
   - `prompts/` — copies of any rendered-prompt files the supervisor wrote (e.g. under `.liza/agent-prompts/` if `LIZA_TEE_AGENT_OUTPUT=1`).
   - `precommit-config-presence.txt` — output of `git -C "$REPRO_ROOT/<cycle>" ls-tree HEAD -- .pre-commit-config.yaml` (empty stdout = absent at HEAD; non-empty = present).

### Observation Recording

Populated after the run — see §Observed Failure Modes — Reproduction Run YYYY-MM-DD.

### Confirm-or-Divert Decision

Populated after the run — see the corresponding subsection under §Observed Failure Modes.

## Observed Failure Modes — Reproduction Run (Deferred — Hypotheses Accepted on Inspection 2026-04-17)

Reproduction artifacts: none — empirical run deferred per TECH_DEBT entry "Deferred greenfield reproduction for precommit-bootstrap" (see TECH_DEBT.md).

| # | Hypothesized mode (goal spec §Evidence, lines 14-17) | Observed? (yes/no/partial) | Evidence pointer | Notes |
|---|---|---|---|---|
| 1 | Coders inventing `.pre-commit-config.yaml` ad-hoc inside an unrelated coding task | accepted | hypothesis inspection only (no empirical run) | With `.pre-commit-config.yaml` absent and `commit_workflow.tmpl:3` unconditionally instructing coders to run pre-commit, a coder faced with the missing-config error has no scoped authorization to bootstrap it, so the mechanically available path of least resistance is to inline a config inside whatever coding task they hold — exactly the mode the design fences off. |
| 2 | Parallel coders producing conflicting configs | accepted | hypothesis inspection only (no empirical run) | Because the architecture step is the only gate that serializes repo-wide structural decisions and the absent config provides no dedup signal to per-scope coders, two coders holding independent scopes in the same sprint can each land the mode-1 inlined config concurrently, producing textually divergent `.pre-commit-config.yaml` blobs whose merge outcome is undefined. |
| 3 | Coders silently skipping pre-commit when it errors with "no config" | accepted | hypothesis inspection only (no empirical run) | The base prompt's "never bootstrap system-level tooling" clause gives the coder a plausible reading that absent-config is a host-state defect they are forbidden to touch, and the path of least resistance is to suppress the error and declare DoD, since no downstream check currently distinguishes "pre-commit ran cleanly" from "pre-commit was skipped with a note". |
| 4 | DoD step "Pre-commit passes on touched files" (CORE.md Rule 3) becoming vacuous | accepted | hypothesis inspection only (no empirical run) | If modes 1-3 occur, the Rule 3 DoD line "Pre-commit passes on touched files" either passes against a coder-invented config of unknown quality (mode 1/2) or is satisfied trivially by the absence of a runnable config (mode 3), meaning the contract's intended invariant — that committed code survives project-defined hooks — is no longer being enforced by the line that claims to enforce it. |

### Newly observed modes (not in the hypothesized list)
None observed — empirical reproduction deferred.

### Confirm-or-Divert Decision
Hypothesized modes accepted on inspection; design ratified pending deferred empirical reproduction (tracked in TECH_DEBT.md).

## Out of Scope

- Worktree hook composition — covered by `20260417-precommit-hook-composition.md`.
- Choice of specific hook *versions* per ecosystem (defer to hook-selection task or ADR).
- Brownfield migration of partial/broken existing `.pre-commit-config.yaml`. Brownfield with any config no-ops.
- Bootstrap of other contract-assumed tooling (`.editorconfig`, lint config). The pattern may generalize; not addressed here.
- Mandating a separate dep-manager bootstrap step. Absence of a project-scoped provisioning path is a valid BLOCKED outcome; planner-orchestrator may rescope by emitting a prerequisite setup task, but this is allowed, not required.

## Status

Design ratified 2026-04-17. **Implementation gated on greenfield reproduction** (acceptance criterion #1).
