# Liza - Usage Guide

## Activation of the Contract for Pairing Agents

See [Contract Activation](../contracts/contract-activation.md).

## Liza

See [DEMO](DEMO.md) for a full example.

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
- `liza` and `liza-mcp` Go binaries in PATH

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
#   liza init "Implement from spec" --entry-point detailed-spec     # coding pipeline only: code-plan → code
#   # If omitted, the orchestrator auto-classifies from the spec content.

# Verify
cat .liza/state.yaml
```

`liza init` creates:
- `.liza/state.yaml` — Blackboard state
- `.liza/pipeline.yaml` — Frozen pipeline config (validated copy of the selected `--config`, default: `~/.liza/pipeline.yaml`)
- `.liza/log.yaml` — Activity log
- `.claude/settings.json` — Claude Code project permissions (Liza MCP tools, skills, git/build commands)
- `.mcp.json` — MCP server configuration (tells Claude Code how to start liza-mcp)
- `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — Symlinks to `~/.liza/CORE.md`
- `GUARDRAILS.md` — Project-specific constraints template (if not already present)
- Integration branch (default `integration`, configurable via `--branch`) — For merging completed work

Contracts and skills live in `~/.liza/` (global, from `liza setup`), not in the project.
Operational reference content (blackboard fields, anomaly types, etc.) is inlined directly into agent prompts.

**3. Start Agents**

The TUI (`liza tui`) is the primary way to spawn and monitor agents. Press `s` to spawn — role names autocomplete from the pipeline config.

Alternatively, spawn agents from the CLI: `liza agent <role>`. Agent identity defaults to the first `{role}-N` not already registered with a valid lease (e.g., `coder-1`, or `coder-2` if `coder-1` is active). Override with `--agent-id` or the `LIZA_AGENT_ID` environment variable.

Roles are organized into two phases. Which agents you need depends on your entry point:

```
Roles:
  orchestrator        - Creates and manages task breakdown

  Specification phase (general-objective entry point):
  epic-planner        - Decomposes vision into epics
  epic-plan-reviewer  - Reviews epic decomposition
  us-writer           - Writes user stories from epics
  us-reviewer         - Reviews user stories

  Coding phase (both entry points):
  code-planner        - Claims and produces coding plans
  code-plan-reviewer  - Reviews coding plans and submits verdicts
  coder               - Claims and implements coding tasks
  code-reviewer       - Reviews coding tasks and submits verdicts
```

**Minimal setup (detailed-spec entry point) — 5 agents:**
Spawn from the TUI (`s`): orchestrator, code-planner, code-plan-reviewer, coder, code-reviewer.

**Full pipeline (general-objective entry point) — 9 agents:**
All of the above plus: epic-planner, epic-plan-reviewer, us-writer, us-reviewer.

Each agent command accepts a `--cli` flag to select the coding agent CLI: `claude` (default), `codex`, `gemini`, `mistral`, or `kimi`. For example: `liza agent coder --cli gemini`.
Selecting alternative agent CLI from the TUI is not supported yet.

Agent output is automatically persisted to `.liza/agent-outputs/` (stdout as `.txt`, stderr as `.err`). Pass `--no-log` to disable. Persisted files are automatically masked — secret values from environment variables (API keys, tokens, passwords) are replaced with `***`. Live terminal output remains unmasked. Logging is automatically disabled in `-i` (interactive) mode.
See [Analyzing Agent Logs](#analyzing-agent-logs) for analysis tools.

Multiple agents of the same role can run in parallel (IDs auto-increment):
```bash
liza agent coder              # auto-assigns coder-1
liza agent coder              # auto-assigns coder-2
liza agent coder --agent-id coder-5   # explicit ID
```

**3. Observe and control**

`liza tui` shows live system state — agents, tasks, alerts, sprint metrics. Keyboard shortcuts: `s` spawn, `p` pause, `r` resume, `a` add task, `c` checkpoint, `y` yolo (toggle auto-resume), `Q` stop.

`./console.sh` is deprecated.

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
  epic-planning-pair → us-writing-pair → code-planning-pair → coding-pair

detailed-spec entry point (coding only):
  code-planning-pair → coding-pair
```

Each transition between pairs is a **human gate** (unless auto-resume is enabled): the sprint completes, the human reviews, then runs `liza proceed <task-id> <transition>` followed by `liza resume`. With auto-resume, these transitions happen automatically.

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
│    liza resume                       (complete strint)  │
│    liza resume                     (start next sprint)  │
│                                                         │
└─────────────────────────────────────────────────────────┘
```

Transitions create child tasks from the parent's `output[]` entries (per-subtask cardinality) or from the parent task itself (one-to-one cardinality). Available transitions are defined in `.liza/pipeline.yaml`.

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

**`liza proceed`** creates child tasks from a completed task's `output[]` entries based on the pipeline transition's cardinality (`per-subtask`: one child per output entry, `one-to-one`: single child from parent). Use `liza status` to see available transitions for tasks at terminal states. After `proceed`, run `liza resume` to start the next sprint.

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
| `liza handoff <task-id> <summary> <next-action>` | Context-exhaustion handoff for a doer agent's claimed task |
| `liza supersede-task <task-id>` | Mark a task as SUPERSEDED by replacements |
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

### Configuring Claude Code (MCP)

Liza integrates with Claude Code through the Model Context Protocol (MCP). `liza init` creates the configuration automatically:

**`.mcp.json`** — MCP server configuration:
```json
{
  "mcpServers": {
    "liza": {
      "command": "liza-mcp",
      "args": ["--project-root", "."]
    }
  }
}
```

**`.claude/settings.json`** — Permissions for Claude Code agents (MCP tools shown in full, other categories truncated):
```json
{
  "enableAllProjectMcpServers": true,
  "enabledMcpjsonServers": ["liza"],
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Read(~/.claude/**)",
      "mcp__liza__liza_get",
      "mcp__liza__liza_status",
      "mcp__liza__liza_validate",
      "mcp__liza__liza_version",
      "mcp__liza__liza_add_tasks",
      "mcp__liza__liza_claim_task",
      "mcp__liza__liza_submit_for_review",
      "mcp__liza__liza_handoff",
      "mcp__liza__liza_submit_verdict",
      "mcp__liza__liza_mark_blocked",
      "mcp__liza__liza_assess_blocked",
      "mcp__liza__liza_release_claim",
      "mcp__liza__liza_supersede_task",
      "mcp__liza__liza_set_task_output",
      "mcp__liza__liza_wt_create",
      "mcp__liza__liza_wt_delete",
      "mcp__liza__liza_wt_merge",
      "mcp__liza__liza_analyze",
      "mcp__liza__liza_update_sprint_metrics",
      "mcp__liza__liza_sprint_checkpoint",
      "mcp__liza__liza_write_checkpoint",
      "mcp__liza__liza_delete_agent",
      "mcp__liza__liza_await_verdict",
      "mcp__liza__liza_await_resubmission"
    ]
  }
}
```

The full template also pre-approves skills (code-review, testing, debugging, etc.), git read/write commands, build tools (go, make, python), shell utilities, and web access (WebFetch, WebSearch, LSP). See `internal/embedded/claude-settings.json` for the complete list.

Both CLI commands (e.g., `liza add-task`) and MCP tools (e.g., `liza_add_tasks`) operate on the same `.liza/state.yaml` file. Claude Code agents use MCP tools for better error handling; the CLI is for manual use. `liza-mcp` starts gracefully even without `.liza/` — only `liza_version` works; all other tools return `NotInitializedError`.

The templates are embedded into the binary. `liza init` writes the active copies to `.claude/settings.json` and `.mcp.json` in the project directory.

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
