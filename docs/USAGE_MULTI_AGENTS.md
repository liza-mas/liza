# Liza - Usage Guide

## Activation of the Contract for Pairing Agents

See [Contract Activation](../contracts/contract-activation.md).

## Liza

See [DEMO](DEMO.md) for a full example.

### Key Concepts

**Goal** — A Liza workspace (`.liza/`) is bound to a single goal. The goal is defined at `liza init` with a description and a spec reference. All tasks, sprints, and agent activity within that workspace serve this goal. To pursue a different goal, remove `.liza/` and re-initialize. One project can only have one active goal at a time.

**Checkpoints** — Execution halts at defined points (sprint completion, planning output ready) so a human can review before the system continues. This is intentional — Liza defaults to human-gated transitions between pipeline phases. If you want uninterrupted execution, enable auto-resume (`liza init --auto-resume` or press `y` in the TUI).

**Worktrees** — Agents don't work directly on your main branch. Each task gets its own [git worktree](https://git-scm.com/docs/git-worktree) (under `.worktrees/task-N/`), giving agents isolated workspaces that can't interfere with each other or with your working copy. Completed work merges into the integration branch, then into main. This means Liza requires a git repository and only one Liza context per repository.

### Project Structure

```
~/.liza/                               # Created by `liza setup`
├── CORE.md                            # Universal rules + mode selection gate
├── PAIRING_MODE.md                    # Human-supervised collaboration
├── MULTI_AGENT_MODE.md                # Peer-supervised Liza system
├── AGENT_TOOLS.md                     # Agent tool contracts
├── COLLABORATION_CONTINUITY.md        # Session continuity
├── pipeline.yaml                      # Default pipeline config (role-pairs, transitions, entry-points)
└── skills/                            # Skill definitions
    ├── code-review/SKILL.md
    ├── debugging/SKILL.md
    ├── liza-logs/SKILL.md
    └── ...

<project>/
├── GUARDRAILS.md                  # Project-specific constraints (optional)
├── .liza/
│   ├── state.yaml                 # Current state
│   ├── pipeline.yaml              # Frozen pipeline config (validated at init from --config)
│   ├── log.yaml                   # Activity history
│   └── archive/                   # Terminal-state tasks
└── .worktrees/
    └── task-N/                    # Per-task workspace
```

### Project Guardrails

`GUARDRAILS.md` is an optional file at the project root that defines project-specific constraints for Liza agents. It uses the same tier system (Tier 0-3) from the core contract:

- **Tier 0 (Inviolable)** — Triggers mandatory halt (RESET) if violated
- **Tier 1 (Hard Constraints)** — Suspended only with explicit waiver
- **Tier 2 (Strong Defaults)** — Best-effort under pressure
- **Tier 3 (Preferences)** — Degraded gracefully

**How it's created:** `liza init` writes a template with empty tier sections. You can also create it manually.

**How to use it:** Fill in the tier sections with project-specific rules. Agents read and enforce `GUARDRAILS.md` automatically during their initialization sequence. If the file doesn't exist, agents are governed by the core contract only.

### Quick Start (Target Usage)

**Prerequisites:**
- Claude Code CLI and git installed
- Go >= 1.25.5 installed
- `liza` Go binary in PATH (see `make install`)

**Installing the Liza CLI:**

```bash
# Latest release (macOS/Linux)
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | bash

# Build from main branch (requires Go and make)
curl -fsSL https://raw.githubusercontent.com/liza-mas/liza/main/install.sh | BRANCH=main bash

# Or from a local clone
make install

# Verify
liza version
```

**1. Global Setup (one-time)**
```bash
liza setup          # installs contracts + skills to ~/.liza/
liza setup --force  # overwrite existing (e.g., after liza upgrade)
liza setup --agent-tools ~/my-agent-tools.md  # use custom AGENT_TOOLS.md
```

**2. Initialize Project**

> **Commit your spec file before running `liza init`.** Worktrees are created from the current branch — uncommitted files won't be visible to agents.

```bash
# Interactive wizard (recommended for first use):
liza init
# Walks through: mode selection → agent selection → project details → conflict resolution

# Or with explicit flags:
liza init "[Goal description]" --spec [spec_ref]

# spec_ref: Path to goal specification (default: specs/vision.md)
# Examples:
#   liza init "Implement retry logic"                        # uses specs/vision.md
#   liza init "Add auth" --spec specs/auth-feature.md        # uses custom spec
#
# Pipeline config (--config defaults to ~/.liza/pipeline.yaml, installed by liza setup):
#   liza init "Sub-pipelines phase 2" \
#     --post-worktree-cmd "make sync-embedded" \
#     --spec specs/build/2\ -\ Sub-pipelines\ and\ spec\ writing.md
#
# Worktree setup: If package.json is detected and --post-worktree-cmd is not set,
# liza init auto-suggests the right install command (npm/yarn/pnpm/bun).
# See docs/CONFIGURATION.md § "Worktree Setup" for details.
#
# Integration branch (--branch sets the branch name, default: "integration"):
#   liza init "Build auth system" --branch develop
#
# Entry points (--entry-point selects which sub-pipeline to start from):
#   liza init "Build auth system" --entry-point general-objective   # full pipeline: epic → US → code-plan → code
#   liza init "Implement from spec" --entry-point detailed-spec     # coding pipeline: architecture → code-plan → code
#   # If omitted, the orchestrator auto-classifies from the spec content.

# Verify
cat .liza/state.yaml
```

`liza init` creates:
- `.liza/state.yaml` — Blackboard state
- `.liza/pipeline.yaml` — Frozen pipeline config (validated copy of the selected `--config`, default: `~/.liza/pipeline.yaml`)
- `.liza/log.yaml` — Activity log
- `.claude/settings.json` — Claude Code project permissions (Liza CLI, skills, git/build commands)
- `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — Symlinks to `~/.liza/CORE.md`
- `GUARDRAILS.md` — Project-specific constraints template (if not already present)
- Integration branch (default `integration`, configurable via `--branch`) — For merging completed work

Contracts and skills live in `~/.liza/` (global, from `liza setup`), not in the project.
Operational reference content (blackboard fields, anomaly types, etc.) is inlined directly into agent prompts.

**3. Start Agents**

The TUI (`liza tui`) is the primary way to spawn and monitor agents. Press `s` to spawn with the configured default CLI (role names autocomplete from the pipeline config), or `S` to pick a specific CLI.

Alternatively, spawn agents from the CLI: `liza agent <role>`. Agent identity defaults to the first `{role}-N` not already registered with a valid lease (e.g., `coder-1`, or `coder-2` if `coder-1` is active). Override with `--agent-id` or the `LIZA_AGENT_ID` environment variable.

Roles are organized into three sub-pipelines (specification, coding, integration). Which agents you need depends on your entry point:

```
Roles:
  orchestrator            - Creates and manages task breakdown

  Specification phase (general-objective entry point):
  epic-planner            - Decomposes vision into epics
  epic-plan-reviewer      - Reviews epic decomposition
  us-writer               - Writes user stories from epics
  us-reviewer             - Reviews user stories

  Architecture phase (both entry points):
  architect               - Defines component boundaries, interfaces, and structural decisions
                            (receives parent task context from upstream US tasks or goal spec)
  architecture-reviewer   - Reviews architectural coherence and structural soundness

  Coding phase (both entry points):
  code-planner            - Claims and produces coding plans
  code-plan-reviewer      - Reviews coding plans and submits verdicts
  coder                   - Claims and implements coding tasks
  code-reviewer           - Reviews coding tasks and submits verdicts

  Integration phase (post-coding, orchestrator-triggered):
  integration-analyst     - Scans full branch diff for cross-task integration issues
  integration-reviewer    - Validates and enriches integration findings
```

**Minimal setup (detailed-spec entry point) — 7 agents:**
Spawn from the TUI (`s`): orchestrator, architect, architecture-reviewer, code-planner, code-plan-reviewer, coder, code-reviewer.

**Full pipeline (general-objective entry point) — 11 agents:**
All of the above plus: epic-planner, epic-plan-reviewer, us-writer, us-reviewer.

**Integration phase** agents (integration-analyst, integration-reviewer) are spawned by the orchestrator after all coding tasks for a goal complete. They are not needed at startup — spawn them when the orchestrator triggers the integration sub-pipeline.

Each agent command accepts a `--cli` flag to select the coding agent CLI: `claude`, `codex`, `gemini`, `mistral`, or `kimi`. When `--cli` is omitted, the default is resolved from `config.default_cli` in `state.yaml`, then `LIZA_DEFAULT_CLI` env var, then `claude`. Set the default at init time with `liza init --default-cli codex "..."`, or edit `state.yaml` directly.
In the TUI, `s` spawns with the configured default CLI; `S` prompts for CLI selection.

Agent output is automatically persisted to `.liza/agent-outputs/` (stdout as `.txt`, stderr as `.err`). Pass `--no-log` to disable. Persisted files are automatically masked — secret values from environment variables (API keys, tokens, passwords) are replaced with `***`. Live terminal output remains unmasked. Logging is automatically disabled in `-i` (interactive) mode.
See [Analyzing Agent Logs](#analyzing-agent-logs) for analysis tools.

Multiple agents of the same role can run in parallel (IDs auto-increment):
```bash
liza agent coder              # auto-assigns coder-1
liza agent coder              # auto-assigns coder-2
liza agent coder --agent-id coder-5   # explicit ID
```

**3. Observe and control**

`liza tui` shows live system state — agents, tasks, alerts, sprint metrics. Keyboard shortcuts: `s` spawn (default cli), `S` spawn (pick cli), `p` pause, `r` resume, `a` add task, `c` checkpoint, `y` yolo (toggle auto-resume), `Q` stop.

From the CLI:
```bash
# Pause all agents
liza pause

# Resume
liza resume

# Abort
liza stop

# Checkpoint (halt + generate summary)
liza sprint-checkpoint

# Activity log
cat .liza/log.yaml
```

**Signal handling:** Agents cleanly exit on `Ctrl+C` (SIGINT) or `kill` (SIGTERM). On exit, the agent unregisters and atomically releases any active task claim — the task returns to its initial state (doer, e.g. DRAFT_CODE) or submitted state (reviewer, e.g. CODE_READY_FOR_REVIEW) — so no orphaned claims are left behind.

**4. Review Results**
```bash
# Integration branch
git log integration --oneline
```

### Running Multiple Sprints

When all tasks in a sprint reach terminal state (MERGED/ABANDONED), `liza resume` marks the sprint COMPLETED. Running `liza resume` a second time archives the completed sprint, creates a new IN_PROGRESS sprint, and executes available pipeline transitions — creating child tasks for the next role-pair.

#### Auto-Resume (Yolo Mode)

By default, checkpoints and sprint completions require manual `liza resume`. Enable auto-resume to skip these gates and keep the system rolling:

- **At init time:** `liza init --auto-resume "Goal"`
- **At runtime:** Press `y` in the TUI to toggle (shows "Auto-resume: ON/OFF" on the status line)

When auto-resume is enabled, agents automatically call `liza resume` when they detect CHECKPOINT or COMPLETED sprint status. Use `p` (pause) for a hard stop — pause is never auto-resumed.

To start a completely fresh goal, remove the blackboard and re-initialize:

```bash
rm -rf .liza
liza init "<new goal>" --spec <spec_ref>
```

### Sprint Lifecycle & Human Gates

Liza runs in sprints. Each sprint executes one role-pair (doer + reviewer) from the pipeline.
Human checkpoints gate transitions between pairs (unless auto-resume is enabled).

#### Pipeline & Entry Points

The pipeline defines which role-pairs execute and how tasks flow between them:

```
general-objective entry point (full pipeline):
  epic-planning-pair → us-writing-pair → architecture-pair → code-planning-pair → coding-pair

detailed-spec entry point (coding pipeline):
  architecture-pair → code-planning-pair → coding-pair

integration sub-pipeline (post-coding, orchestrator-triggered):
  integration-pair → coding-pair (fix tasks)
```

Each transition between pairs is a **human gate** (unless auto-resume is enabled): the sprint completes, the human reviews, then runs `liza proceed <task-id> <transition>` followed by `liza resume`. With auto-resume, these transitions happen automatically.

The `integration-to-fix` transition is an exception — it uses `trigger: auto`, meaning fix tasks are created automatically when the integration reviewer approves findings, without a human gate.

#### Sprint Phases

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│  1. Orchestrator creates task for current pair          │
│  2. Doer claims task, does work, populates output[]     │
│  3. Reviewer approves → task merges                     │
│  4. All tasks done → SPRINT_COMPLETE                    │
│  5. Sprint checkpoints → HUMAN GATE (or auto-resumed)   │
│                                                         │
│  Human reviews results, then (manual mode):             │
│    liza resume                       (complete sprint)  │
│    liza resume                     (start next sprint)  │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

Transitions create child tasks from the parent's `output[]` entries (per-subtask cardinality), from the parent task itself (one-to-one cardinality), or from multiple parent tasks in a cohort (many-to-one cardinality). Available transitions are defined in `.liza/pipeline.yaml`.

#### What Humans Do at Checkpoints

When a sprint checkpoints (status: CHECKPOINT), all agents pause.
The human reviews the sprint summary and decides:

| Action | Command | When                                                                                                 |
|--------|---------|------------------------------------------------------------------------------------------------------|
| Accept & resume | `liza resume` | Satisfied with planner output, continue the sprint, start next sprint                                |
| Amend & replan | Edit plan file, commit, then `liza replan` | Want to change a planner's output before proceeding                                                  |
| Pipeline transition | `liza proceed <task-id> <transition>` | Create child tasks for the next role-pair from output[]. Automatically done in batch by `liza resume` |
| Pause for manual work | (no command) | Want to make manual changes before continuing                                                        |
| Abort | `liza stop` | Want to stop entirely                                                                                |

**`liza proceed`** creates child tasks from a completed task's `output[]` entries based on the pipeline transition's cardinality (`per-subtask`: one child per output entry, `one-to-one`: single child from parent, `many-to-one`: all sibling tasks in a cohort must reach approved status, then one child is created linked to all parents — used by the `us-to-coding` transition to fan N approved user stories into one architecture task). Use `liza status` to see available transitions for tasks at terminal states. After `proceed`, run `liza resume` to start the next sprint.

#### Replanning at Checkpoint

When a planning sprint checkpoints (trigger: `PLANNING_COMPLETE`), the planner's `output[]` entries represent the proposed task breakdown. The human may:

1. **Accept the plan** — run `liza resume` to continue
2. **Amend the plan** — edit the plan markdown file, commit, then run `liza replan`

`liza replan` invalidates the old planning task's output and creates a new planning task with the same role-pair and spec. The sprint returns to IN_PROGRESS and the planner agent picks up the new task, re-reads the amended plan, and regenerates `output[]`.

```bash
# Typical replan workflow
vim specs/plan.md                      # edit the plan
git add specs/plan.md && git commit -m "amend plan"
liza replan                            # auto-detects the planning task
# or, if multiple planning tasks exist:
liza replan code-planning-1            # specify task ID explicitly
```

The old task's output is preserved for audit (not cleared), just marked as superseded. Multiple replans increment the counter: `code-planning-1-replan-1`, `code-planning-1-replan-2`, etc.

#### Sprint Status Flow

```
IN_PROGRESS → CHECKPOINT ──→ COMPLETED ──→ (new sprint) IN_PROGRESS
                  │  ↑            ↑              ↑
                  │  │            │              └── liza resume (2nd: archive & advance)
                  │  │            └── liza resume (1st: all tasks terminal → mark COMPLETED)
                  │  └── orchestrator calls liza_sprint_checkpoint
                  │
                  ├── liza resume  (mid-sprint: not all terminal → back to IN_PROGRESS)
                  └── liza replan  (amend plan → new planning task → back to IN_PROGRESS)
```

**`liza resume` has two behaviors depending on sprint state:**
- **At CHECKPOINT** (not all tasks terminal): resumes the current sprint as IN_PROGRESS
- **At CHECKPOINT** (all tasks terminal): marks sprint COMPLETED. Run `liza resume` a second time to archive the sprint, create a new one, and execute available pipeline transitions

### CLI Commands

The `liza` binary provides all system operations. Key commands:

| Command | Purpose |
|---------|---------|
| **Setup & Init** | |
| `liza setup` | One-time global setup of contracts and skills to `~/.liza/` |
| `liza init <goal> --spec <spec_ref> [--branch <name>]` | Initialize `.liza/` directory with blackboard (spec_ref defaults to specs/vision.md, branch defaults to integration) |
| **Agents & Monitoring** | |
| `liza agent <role> [--agent-id <id>]` | Agent supervisor (start, restart, backoff loop; ID auto-assigned if omitted) |
| `liza tui` | Live TUI: spawn agents, monitor state, manage system |
| `liza status` | Show system and task status at a glance |
| **System Control** | |
| `liza pause` / `liza resume` | Pause/resume system (resume also advances CHECKPOINT → COMPLETED → new sprint) |
| `liza replan [task-id]` | Amend a planner's output at CHECKPOINT (invalidate old task, create new planning task) |
| `liza stop` / `liza start` | Stop/start system |
| `liza sprint-checkpoint` | Create a checkpoint (halt + summary) |
| **Task Operations** | |
| `liza add-task` | Add a new task to the state |
| `liza claim-task <task-id> <agent-id>` | Atomically claim a task for a doer agent (creates worktree, updates state) |
| `liza submit-for-review <task-id> <commit-sha>` | Submit a task for review (doer agents) |
| `liza submit-verdict <task-id> <APPROVED\|REJECTED> [reason]` | Submit a review verdict (reviewer agents; reason required for REJECTED) |
| `liza mark-blocked <task-id>` | Mark a task as BLOCKED with reason and questions |
| `liza assess-blocked <task-id>` | Record orchestrator assessment of a BLOCKED task (prevents re-wake loops) |
| `liza assess-hypothesis-exhausted <task-id>` | Record orchestrator assessment of a hypothesis-exhausted task (2+ coders failed) |
| `liza cancel-task <task-id> --reason "..."` | Cancel a task (transition to ABANDONED with audit trail) |
| `liza handoff <task-id> <summary> <next-action>` | Context-exhaustion handoff for a doer agent's claimed task |
| `liza supersede-task <task-id> [replacements] --reason "..."` | Mark a task as SUPERSEDED (with or without replacements) |
| `liza proceed <task-id> <transition>` | Execute inter-pair pipeline transition (e.g., code-plan-to-coding) |
| **Worktree Management** | |
| `liza wt-create <task-id>` | Create a worktree for an executing task |
| `liza wt-merge <task-id>` | Merge an approved task into the integration branch (reviewer agents) |
| `liza wt-delete <task-id>` | Delete a worktree for a completed/abandoned task |
| **Recovery** | |
| `liza recover-task <task-id>` | Recover by task ID (release claims + remove worktree/branch) |
| `liza recover-agent <agent-id>` | Recover by agent ID (release claim + remove worktree + delete agent) |
| `liza release-claim <task-id> [--role R]` | Release claim on a task (manual, granular recovery) |
| `liza delete agent <id>` / `liza delete task <id>` | Delete an agent or task from state |
| **Analysis** | |
| `liza validate` | Validate blackboard state against schema invariants |
| `liza analyze` | Run circuit breaker pattern detection |
| `liza update-sprint-metrics` | Recompute sprint metrics from current state |
| `liza clear-stale-review-claims` | Clear expired review leases |
| `liza get <query>` | Query state data (tasks, agents, etc.) |

**Important:** The supervisor claims tasks *before* starting the agent CLI. This avoids interactive permission prompts in non-interactive mode. Agents receive their assigned task in the bootstrap prompt and should NOT call claim commands directly.

See [Architecture Overview](../specs/architecture/overview.md) for detailed component descriptions.

### Configuring Claude Code

`liza init` creates the Claude Code configuration automatically:

**`.claude/settings.json`** — Permissions for Claude Code agents (Liza CLI permissions shown, other categories truncated):
```json
{
  "enableAllProjectMcpServers": true,
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Read(~/.claude/**)",
      "Bash(liza:*)"
    ]
  }
}
```

The full template also pre-approves skills (code-review, testing, debugging, etc.), git read/write commands, build tools (go, make, python), shell utilities, and web access (WebFetch, WebSearch, LSP). See `internal/embedded/claude-settings.json` for the complete list.

CLI commands (e.g., `liza add-task`) operate on `.liza/state.yaml` with proper locking. Agents use CLI commands via Bash with `--json` for structured output.

The settings template is embedded into the binary. `liza init` writes the active copy to `.claude/settings.json` in the project directory.

### Analyzing Agent Logs

Logs (captured by default, disable with `--no-log`) are NDJSON files (one JSON object per line) from `claude --verbose --output-format stream-json`. Two formats exist depending on the agent role:

| Format | First event | Seen in | Token detail |
|--------|-------------|---------|--------------|
| **Rich** | `type: system` | Orchestrator | Per-API-call breakdown (input, cache, output) |
| **Sparse** | `type: thread.started` | All doer and reviewer roles | Aggregate only (`turn.completed`) |

Both analysis tools auto-detect the format.

**LLM-assisted analysis** — use a coding agent to cross-correlate logs, diagnose patterns and propose fixes:

```
/liza-logs
```

This works with any coding agent (Claude Code, Codex, etc.) in pairing mode. The agent runs the analyzer, reads the reports, correlates errors across agents, and suggests actionable fixes.

**CLI analyzer** (`~/.liza/skills/liza-logs/scripts/analyze-log.py`) — stdlib-only Python 3.12+, for batch/CI use:

```bash
# Single file
python3 ~/.liza/skills/liza-logs/scripts/analyze-log.py .liza/agent-outputs/orchestrator-1-*.txt

# Multiple files
python3 ~/.liza/skills/liza-logs/scripts/analyze-log.py .liza/agent-outputs/*.txt
```

Report sections: session header, token summary (fresh/cached/output, cache hit rate), content breakdown by type (chars, estimated tokens, share %), top 10 items by size, tool call frequency. Rich format adds per-turn context growth and cost breakdown.

**Browser analyzer** (`liza-session-analyzer.html`) — drag-and-drop, visual charts:

```bash
open ~/.liza/skills/liza-logs/tools/liza-session-analyzer.html   # or xdg-open on Linux
```

Drop one or more log files. Produces the same analysis with bar charts for content breakdown and context growth.

**Raw inspection** with `jq` (no dependencies):

```bash
# Sparse format: extract items
jq -c 'select(.item) | .item | {type, text, command, tool, usage}
  | with_entries(select(.value != null))' .liza/agent-outputs/coder-1-*.txt

# Rich format: extract token usage per API call
jq -c 'select(.type == "assistant") | {id: .message.id, usage: .message.usage}' \
  .liza/agent-outputs/orchestrator-1-*.txt
```

**Interactive diagnosis** — open a regular coding agent session (`claude`, `codex`, etc.) in the project directory. It can read `.liza/state.yaml`, agent logs, and prompts — everything needed to diagnose issues interactively. The `/liza-logs` skill works this way.

### Submit, Await Verdict, Handle Result

Doer agents (coders, planners, writers) use a blocking workflow for review cycles:

1. `liza_submit_for_review` — submit completed work
2. `liza_await_verdict` — block until reviewer issues verdict
3. Handle verdict:
   - **REJECTED**: Fix issues, resubmit (session stays alive — no cold restart)
   - **APPROVED** / **NEW_ATTEMPT** / **TIMEOUT** / **ABORTED**: Exit normally

This reduces per-rejection overhead from ~47s (cold restart) to near-zero. The call is budget-aware — it refuses if the iteration limit would be exceeded on rejection.

### Review, Reject, Await Resubmission

Reviewer agents use a blocking workflow after non-terminal rejections:

1. `liza_submit_verdict` — issue REJECTED verdict with feedback
2. `liza_await_resubmission` — block until doer resubmits
3. Handle result:
   - **RESUBMITTED**: Review the new changes (session stays alive — no cold restart)
   - **TERMINAL** / **TIMEOUT** / **ABORTED**: Exit normally

This mirrors the doer-side `liza_await_verdict` flow, reducing per-rejection overhead from ~47s (cold restart) to near-zero for reviewers. Do not call after terminal rejections (`EscalatedToBlocked` or `NewAttemptTriggered`).

### Differences from Pairing Mode

| Aspect | Pairing Mode | Multi-Agent Mode |
|--------|--------------|------------------|
| Approval | Human approves | Peer agent approves |
| Gates | Approval request → wait | Pre-execution checkpoint → proceed |
| Communication | Conversation | Blackboard |
| Iteration | Human feedback | Reviewer agent feedback |
| Debugging | Debugging skill | Log anomaly, BLOCKED |
| Magic Phrases | Active | Not applicable |
| Session Init | Greet user | Silent execution |

### Supervisor Circuit Breaker

The supervisor automatically handles these conditions (transparent to agents):

| Condition | Action |
|-----------|--------|
| Agent crash loop (3× in 5min) | Supervisor stops the agent |
| Blackboard validation fails | All agents pause |
| Integration branch conflict | Task set to INTEGRATION_FAILED |
| Circuit-breaker pattern detected in anomalies | Set mode to `CIRCUIT_BREAKER_TRIPPED`, create sprint `CHECKPOINT`, write reports |
