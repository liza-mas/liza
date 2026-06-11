# Liza Demo — Hello World Python CLI

This walkthrough demonstrates Liza orchestrating a multi-agent system to build a simple Python CLI from scratch.

**Goal:** Create a `hello` CLI that prints "Hello, World!" (or a custom name).

**Duration:** ~10-15 minutes of observation after setup.

---

## Prerequisites

- Claude Code CLI and git installed
- Go >= 1.25.5 installed
- `liza` Go binary in PATH (see `make install`)

See [Getting Started](../GETTING_STARTED.md) for first-run setup and
[Configuration](../support-docs/CONFIGURATION.md) for provider settings.

---

## Step 1: Create Project Repository

```bash
mkdir hello-cli && cd hello-cli
git init
```

---

## Step 2: Create Vision Spec

The Orchestrator needs a goal to decompose. Create `specs/vision.md`:

```bash
mkdir -p specs
cat > specs/vision.md << 'EOF'
# Vision: Hello CLI

## Goal

Create a Python CLI tool that greets users.

## Requirements

1. Command: `hello` (or `python -m hello`)
2. Default output: `Hello, World!`
3. Optional `--name` argument: `hello --name Alice` → `Hello, Alice!`
4. Exit code 0 on success

## Constraints

- Python 3.8+ compatible
- No external dependencies (stdlib only)
- Include basic tests

## Success Criteria

- `python -m hello` prints "Hello, World!"
- `python -m hello --name Bob` prints "Hello, Bob!"
- All tests pass
EOF
```

---

## Step 3: Configure Dev Tooling

Liza agents expect pre-commit and test coverage tooling. Set these up before the first commit.

```bash
cat > requirements-dev.txt << 'EOF'
pytest>=7.0
pytest-cov>=4.0
diff-cover>=7.0
EOF

pip install -r requirements-dev.txt
```

Create a minimal `.pre-commit-config.yaml`:

```bash
cat > .pre-commit-config.yaml << 'EOF'
default_stages: [pre-commit]
fail_fast: false

repos:
  - repo: https://github.com/pre-commit/pre-commit-hooks
    rev: v6.0.0
    hooks:
      - id: check-merge-conflict
      - id: end-of-file-fixer
      - id: trailing-whitespace

  - repo: https://github.com/astral-sh/ruff-pre-commit
    rev: v0.14.7
    hooks:
      - id: ruff
        args: [--fix, --exit-non-zero-on-fix]
      - id: ruff-format
EOF

pre-commit install
```

---

## Step 4: Initial Commit

Liza works on a git repository. Commit the initial spec and tooling:

```bash
git add .
git commit -m "Initial commit: vision spec and dev tooling"
```

---

## Step 5: Initialize Liza

```bash
liza setup  # one-time: installs contracts + skills to ~/.liza/
liza init "Build hello CLI" --spec specs/vision.md --entry-point functional-spec
```

The `--entry-point functional-spec` skips the specification phase (epic planning, user stories) and starts at architecture: architecture → code planning → coding. For a simple hello-world, this is the right entry point. Existing workflows using `--entry-point detailed-spec` still work as a legacy alias.

This creates:
- `.liza/state.yaml` — the blackboard
- `.liza/pipeline.yaml` — frozen pipeline config
- `.liza/log.yaml` — activity log
- `.liza/alerts.log` — watcher alerts
- `.claude/settings.json` — Claude Code project permissions
- `CLAUDE.md`, `AGENTS.md`, `GEMINI.md` — symlinks to `~/.liza/CORE.md`
- `GUARDRAILS.md` — project-specific constraints template
- `integration` branch — where approved work lands

Verify:
```bash
cat .liza/state.yaml
```

You should see:
```yaml
version: 1
goal:
  id: goal-<timestamp>
  description: "Build hello CLI"
  spec_ref: /absolute/path/to/specs/vision.md
  status: IN_PROGRESS
tasks: []
agents: {}
# ... more sections
```

---

## Step 6: Launch the Agent Window

Start the functional-spec role set in one WezTerm window:

```bash
cd hello-cli
liza launch wezterm mas --preset functional-spec
```

This opens `liza tui` plus panes for orchestrator, architect,
architecture-reviewer, code-planner, code-plan-reviewer, coder, and
code-reviewer. Agent output is automatically persisted to
`.liza/agent-outputs/` for later analysis, and prompt captures can be audited
with `/context-engineering` when available (see
[Analyzing Agent Logs](USAGE_MULTI_AGENTS.md#analyzing-agent-logs)). Each agent
command also accepts a `--cli` flag; pass it through the launcher when you want
one backend for every role:

```bash
liza launch wezterm mas --preset functional-spec --cli codex
```

When `--cli` is omitted, each role resolves its backend from role-specific config
(`config.default_doer_cli` for doers and orchestrators,
`config.default_reviewer_cli` for reviewers), then role-specific env, then
global defaults, then `claude`.

**CMUX support:** Liza also supports CMUX as an alternative to WezTerm. Use
`liza launch cmux mas` with the same flags to launch agents in CMUX panes
instead of WezTerm.

The Orchestrator will:
1. Read `specs/vision.md`
2. Create the initial architecture task
3. Monitor sprint progress and create checkpoints

---

## Step 7: Let Planning Run

The architecture and code-planning agents are already running in their panes.
They will claim work as it becomes available:

1. Architect defines the implementation structure from `specs/vision.md`.
2. Architecture reviewer approves or rejects that plan.
3. Code planner produces coding tasks.
4. Code plan reviewer approves or rejects those tasks.

Use the TUI pane to watch tasks and alerts. For a plain table view, run this in
another shell or an extra WezTerm pane:

```bash
watch -n 2 'liza get tasks --format table'
```

When a planning task reaches an approved terminal state, review the plan, then
transition to the next phase:

```bash
liza proceed <task-id> code-plan-to-coding
liza resume
```

---

## Step 8: Let Coding Run

The coder and code-reviewer panes are already running. Once coding tasks appear
after `proceed` + `resume`, they will claim and review work.

The Coder will:
1. Claim a coding task
2. Create a worktree (`.worktrees/task-N/`)
3. Implement the task
4. Run tests
5. Submit for review

The Code Reviewer will:
1. Claim submitted tasks
2. Review the code
3. Either APPROVE or REJECT with feedback
4. If APPROVED: merge to `integration` branch

Watch worktrees:
```bash
ls -la .worktrees/
```

---

## Step 9: Observe the Flow

With all agents running, watch the system:

**Task status:**
```bash
watch -n 2 'liza get tasks --format table'
```

**Full blackboard state (tasks, agents, metrics, anomalies):**
```bash
liza tui
```

**System status:**
```bash
watch -n 5 'liza status'
```

**Activity log:**
```bash
tail -f .liza/log.yaml
```

**Integration branch progress:**
```bash
watch -n 10 'git log integration --oneline 2>/dev/null || echo "No merges yet"'
```

---

## Expected Flow

```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Orchestrator │  │ Code Planner │  │  Plan Review  │  │    Coder    │  │ Code Review  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └──────┬──────┘  └──────┬───────┘
       │                 │                 │                  │                │
       │ Create planning │                 │                  │                │
       │ task            │                 │                  │                │
       │────────────────>│                 │                  │                │
       │                 │ Write plan +    │                  │                │
       │                 │ populate output │                  │                │
       │                 │────────────────>│                  │                │
       │                 │                 │ Review + Approve │                │
       │                 │                 │ Merge            │                │
       │                 │                 │                  │                │
       │ ═══ HUMAN GATE: liza proceed + resume ════════════  │                │
       │                 │                 │                  │                │
       │                 │                 │                  │ Claim task     │
       │                 │                 │                  │ Implement...   │
       │                 │                 │                  │───────────────>│
       │                 │                 │                  │                │ Review
       │                 │                 │                  │       APPROVED │
       │                 │                 │                  │<───────────────│
       │                 │                 │                  │                │ Merge to
       │                 │                 │                  │                │ integration
       │                 │                 │                  │ Claim next     │
       │                 │                 │                  │                │
      ...               ...               ...               ...              ...
```

---

## Step 11: Verify Results

Once all tasks reach MERGED status:

```bash
# Check task states
liza get tasks --format table

# Switch to integration branch
git checkout integration

# Test the CLI
python -m hello
# → Hello, World!

python -m hello --name "Liza"
# → Hello, Liza!

# Run tests
python -m pytest tests/ -v
```

---

## Example Sprint Results

After a successful sprint, you'll see output like this from the Orchestrator:

```
Sprint Progress:
  Planned tasks: 3
  Merged: 3
  Abandoned/Superseded: 0
  Blocked: 0
  In progress: 0

All 3 planned task(s) complete. Sprint done.
Unregistering agent: orchestrator-1
```

**Final Task States:**
```bash
liza get tasks --format table
```

```yaml
id: task-1
status: MERGED
title: "Create project structure"
---
id: task-2
status: MERGED
title: "Implement CLI with argparse"
---
id: task-3
status: MERGED
title: "Add unit tests"
```

**Integration Branch Commits:**
```bash
git log integration --oneline
```

```
a1b2c3d Merge task-3: Add unit tests
d4e5f6g Merge task-2: Implement CLI with argparse
h7i8j9k Merge task-1: Create project structure
l0m1n2o Initial commit: vision spec
```

**Sprint Metrics:**
```bash
liza get metrics
```

```yaml
id: sprint-1
goal_ref: goal-1234567890
scope:
  planned: [task-1, task-2, task-3]
  stretch: []
timeline:
  started: "2025-01-20T10:00:00Z"
  deadline: null
  checkpoint_at: null
  ended: "2025-01-20T10:12:00Z"
status: COMPLETED
metrics:
  tasks_done: 3
  tasks_in_progress: 0
  tasks_blocked: 0
  iterations_total: 0
  review_cycles_total: 4
retrospective: null
```

**Agent Activity Summary:**
```bash
liza get agents --format table
```

After completion, the agents section will be empty (agents unregister on exit):
```yaml
agents: {}
```

---

## Human Interventions

**Pause the system:**
```bash
liza pause
# All agents will pause at next check
```

**Resume:**
```bash
liza resume
```

**View alerts:**
```bash
cat .liza/alerts.log
```

**Trigger checkpoint (sprint boundary):**
```bash
liza sprint-checkpoint
```

**Abort everything:**
```bash
liza stop
# All agents will exit gracefully
```

**Signal handling:** Agents cleanly exit on `Ctrl+C` (SIGINT) or `kill` (SIGTERM). On exit, the agent unregisters and atomically releases any active task claim so no orphaned claims are left behind.

---

## Troubleshooting

**No tasks appearing?**
- Check Orchestrator terminal for errors
- Verify `specs/vision.md` exists and is readable
- Check `.liza/log.yaml` for Orchestrator activity

**Coder stuck?**
- Check worktree exists: `ls .worktrees/`
- Check task status: `liza get tasks --format table`
- Look for BLOCKED status with `blocked_reason`: `liza get <task-id>`

**Review taking too long?**
- Check reviewer terminal
- Check task status: `liza get tasks --format table`

**Debug agent interactively (-i option)**
- Terminate the agent and release the task: `liza release-claim <task-id> --role both`
- Get its prompt from `.liza/agent-prompts/`
- Run `liza agent <role> --cli <claude|codex|gemini|mistral|kimi> -i`
- Paste the prompt

Codex is a nice option for debugging too because it displays everything.
Run `liza agent coder --cli codex`

**Watcher alerts?**
- `LEASE EXPIRED`: Agent crashed, supervisor will restart
- `BLOCKED`: Task needs human input — check `blocked_questions`
- `REVIEW LOOP`: Too many rejections — may need spec clarification

For more issues and recovery procedures, see the full [Troubleshooting Guide](TROUBLESHOOTING.md).

---

## Cleanup

```bash
# Stop all agents (Ctrl+C in each terminal)

# Or force abort
liza stop

# Remove git worktrees and task branches
for wt in .worktrees/*/; do
    branch=$(basename "$wt")
    git worktree remove "$wt" --force 2>/dev/null
    git branch -D "$branch" 2>/dev/null
done

# Remove Liza state (keeps code)
rm -rf .liza .worktrees

# Or remove entire demo
cd .. && rm -rf hello-cli
```

For more cleanup scenarios, see [Troubleshooting Guide](TROUBLESHOOTING.md#worktree-issues).

---

## Next Steps

- Read [Architecture Overview](../specs/architecture/overview.md) for system design
- Read [Roles](../specs/architecture/roles.md) for agent capabilities
- Read [Task Lifecycle](../specs/protocols/task-lifecycle.md) for state transitions
- Try a more complex goal with multiple interdependent tasks
