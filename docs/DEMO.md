# Liza Demo — Hello World Python CLI

This walkthrough demonstrates Liza orchestrating a multi-agent system to build a simple Python CLI from scratch.

**Goal:** Create a `hello` CLI that prints "Hello, World!" (or a custom name).

**Duration:** ~10-15 minutes of observation after setup.

---

## Prerequisites

See [USAGE](USAGE.md) for the full setup.
---

## Step 1: Create Project Repository

```bash
mkdir hello-cli && cd hello-cli
git init
```

**Configure Permissions** so the agents may run headless:

`.claude/settings.local.json`:
```json
{
  "permissions": {
    "defaultMode": "acceptEdits",
    "allow": [
      "Skill(code-cleaning)",
      "Skill(code-review)",
      "Skill(debugging)",
      "Skill(software-architecture-review)",
      "Skill(spec-review)",
      "Skill(systemic-thinking)",
      "Skill(testing)",
      "Bash(git:*)",
      "Bash(yq:*)",
      "Bash(pre-commit:*)",
      "Bash(python:*)",
      "Bash(python3:*)",
      "Bash(pytest:*)",
      "Bash(shellcheck:*)",
      "Bash(bash:*)",
      "Bash(ls:*)",
      "Bash(cat:*)",
      "Bash(head:*)",
      "Bash(tail:*)",
      "Bash(wc:*)",
      "Bash(date:*)",
      "Bash(~/.claude/scripts/*)"
    ]
  }
}
```
You may need to add more allowed tools. Refer to "Debug a stuck agent interactively" below to identify the blocking commands.

---

## Step 2: Create Vision Spec

The Planner needs a goal to decompose. Create `specs/vision.md`:

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

## Step 3: Initial Commit

Liza works on a git repository. Commit the initial spec:

```bash
git add .
git commit -m "Initial commit: vision spec"
```

---

## Step 4: Initialize Liza

```bash
~/.claude/scripts/liza-init.sh "Build hello CLI per specs/vision.md"
```

This creates:
- `.liza/state.yaml` — the blackboard
- `.liza/log.yaml` — activity log
- `.liza/alerts.log` — watcher alerts
- `integration` branch — where approved work lands

Verify:
```bash
cat .liza/state.yaml
```

You should see:
```yaml
version: "1.0"
goal:
  id: goal-1
  description: "Build hello CLI per specs/vision.md"
  status: IN_PROGRESS
tasks: []
agents: {}
# ... more sections
```

---

## Step 5: Start the Watcher (Terminal 1)

Open a terminal for monitoring:

```bash
cd hello-cli
~/.claude/scripts/liza-watch.sh
```

This monitors for anomalies and alerts. Leave it running.

---

## Step 6: Start the Planner (Terminal 2)

```bash
cd hello-cli
LIZA_AGENT_ID=planner-1 ~/.claude/scripts/liza-agent.sh planner
```

The Planner will:
1. Read `specs/vision.md`
2. Decompose the goal into tasks
3. Create DRAFT tasks, then finalize to UNCLAIMED

Watch the blackboard update:
```bash
# In another terminal
watch -n 2 'yq ".tasks[] | pick([\"id\", \"status\", \"title\"])" .liza/state.yaml'
```

Expected tasks (Planner decides, but likely):
- `task-1`: Create project structure (setup.py, __init__.py)
- `task-2`: Implement CLI argument parsing
- `task-3`: Add greeting logic
- `task-4`: Write tests

---

## Step 7: Start the Coder (Terminal 3)

Once UNCLAIMED tasks appear:

```bash
cd hello-cli
LIZA_AGENT_ID=coder-1 ~/.claude/scripts/liza-agent.sh coder
```

The Coder will:
1. Claim an UNCLAIMED task
2. Create a worktree (`.worktrees/task-N/`)
3. Implement the task
4. Run tests
5. Submit for review (READY_FOR_REVIEW)

Watch worktrees:
```bash
ls -la .worktrees/
```

---

## Step 8: Start the Code Reviewer (Terminal 4)

```bash
cd hello-cli
LIZA_AGENT_ID=code-reviewer-1 ~/.claude/scripts/liza-agent.sh code-reviewer
```

The Code Reviewer will:
1. Claim READY_FOR_REVIEW tasks
2. Review the code
3. Either APPROVE or REJECT with feedback
4. If APPROVED: merge to `integration` branch

---

## Step 9: Observe the Flow

With all three agents running, watch the system:

** Task status:**
```bash
watch -n 2 'yq ".tasks[] | pick([\"id\", \"status\", \"title\"])" .liza/state.yaml'
```

**Blackboard state:**
```bash
watch -n 5 'yq . .liza/state.yaml | head -80'
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
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│   Planner   │     │    Coder    │     │  Reviewer   │
└──────┬──────┘     └──────┬──────┘     └──────┬──────┘
       │                   │                   │
       │ Create tasks      │                   │
       │ DRAFT → UNCLAIMED │                   │
       │───────────────────>                   │
       │                   │                   │
       │                   │ Claim task        │
       │                   │ UNCLAIMED→CLAIMED │
       │                   │                   │
       │                   │ Implement...      │
       │                   │                   │
       │                   │ Submit            │
       │                   │ CLAIMED→READY     │
       │                   │───────────────────>
       │                   │                   │
       │                   │                   │ Review
       │                   │                   │
       │                   │          APPROVED │
       │                   │<──────────────────│
       │                   │                   │
       │                   │                   │ Merge to
       │                   │                   │ integration
       │                   │                   │
       │                   │ Claim next task   │
       │                   │                   │
      ...                 ...                 ...
```

---

## Step 10: Verify Results

Once all tasks reach MERGED status:

```bash
# Check task states
yq '.tasks[] | pick(["id", "status"])' .liza/state.yaml

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

After a successful sprint, you'll see output like this from the Planner:

```
Sprint Progress:
  Planned tasks: 3
  Merged: 3
  Abandoned/Superseded: 0
  Blocked: 0
  In progress: 0

All 3 planned task(s) complete. Sprint done.
Unregistering agent: planner-1
```

**Final Task States:**
```bash
yq '.tasks[] | pick(["id", "status", "title"])' .liza/state.yaml
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
yq '.sprint' .liza/state.yaml
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
yq '.agents' .liza/state.yaml
```

After completion, the agents section will be empty (agents unregister on exit):
```yaml
agents: {}
```

---

## Human Interventions

**Pause the system:**
```bash
touch .liza/PAUSE
# All agents will pause at next check
```

**Resume:**
```bash
rm .liza/PAUSE
```

**View alerts:**
```bash
cat .liza/alerts.log
```

**Trigger checkpoint (sprint boundary):**
```bash
~/.claude/scripts/liza-checkpoint.sh "Sprint 1 complete"
# Generates .liza/checkpoints/checkpoint-YYYYMMDD-HHMMSS.md
```

**Abort everything:**
```bash
touch .liza/ABORT
# All agents will exit gracefully
```

---

## Troubleshooting

**No tasks appearing?**
- Check Planner terminal for errors
- Verify `specs/vision.md` exists and is readable
- Check `.liza/log.yaml` for Planner activity

**Coder stuck?**
- Check worktree exists: `ls .worktrees/`
- Check task status: `yq '.tasks[] | select(.status == "CLAIMED")' .liza/state.yaml`
- Look for BLOCKED status with `blocked_reason`

**Review taking too long?**
- Check Code Reviewer terminal
- Verify task is READY_FOR_REVIEW: `yq '.tasks[] | select(.status == "READY_FOR_REVIEW")' .liza/state.yaml`

**Debug a stuck agent interactively**
- Terminate the agent
- Get its prompt from `.liza/agent-prompts/`
- Run `claude --add-dir ../liza`
- Paste the prompt

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
touch .liza/ABORT

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
