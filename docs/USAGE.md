# Liza - Usage Guide

## Activation of the Contract for Pairing Agents

Check [Genesis](../README.md#genesis) for the features.

Create symlinks:
```
- `~/.claude/CLAUDE.md` -> `~/Workspace/liza/contracts/CORE.md`
- `~/.claude/PAIRING_MODE.md` -> `~/Workspace/liza/contracts/PAIRING_MODE.md`
- `~/.claude/MULTI_AGENT_MODE.md` -> `~/Workspace/liza/contracts/MULTI_AGENT_MODE.md`
- `~/.claude/AGENT_TOOLS.md` -> `~/Workspace/liza/contracts/AGENT_TOOLS.md`
- `~/.claude/skills` -> `~/Workspace/liza/contracts/skills`
- `~/.claude/COLLABORATION_CONTINUITY.md` -> `~/Workspace/liza/contracts/COLLABORATION_CONTINUITY.md`
- `~/.claude/scripts` -> `~/Workspace/liza/scripts`
```

In `~/.claude/settings.json`:
```json
{
  "permissions": {
    "allow": [
      "Read(~/.claude/**)"
    ]
  }
}
```

Verification:
- Run `claude`
- Prompt `hello`

## Liza

See [DEMO](DEMO.md) for a full example.

### Quick Start (Target Usage)

**Prerequisites:**
- Claude Code CLI installed
- `yq` installed (YAML processor)
- Git installed
- Project with `specs/vision.md` describing the goal

**1. Initialize**
```bash
# Create .liza/ directory with blackboard
~/.claude/scripts/liza-init.sh "[Goal description]"

# Verify
cat .liza/state.yaml
```

**2. Start Agents (3 terminals)**

Terminal 1 — Planner:
```bash
LIZA_AGENT_ID=planner-1 ~/.claude/scripts/liza-agent.sh planner
```

Terminal 2 — Coder:
```bash
LIZA_AGENT_ID=coder-1 ~/.claude/scripts/liza-agent.sh coder
```

Terminal 3 — Code Reviewer:
```bash
LIZA_AGENT_ID=code-reviewer-1 ~/.claude/scripts/liza-agent.sh code-reviewer
```

**3. Observe**
```bash
# Run the watcher for alerts
~/.claude/scripts/liza-watch.sh
```

```bash
# Watch blackboard state
watch -n 2 'yq ".tasks[] | pick([\"id\", \"status\", \"description\"])" .liza/state.yaml'
```

**4. Human Interventions**
```bash
# Pause all agents
touch .liza/PAUSE

# Resume
rm .liza/PAUSE

# Abort
touch .liza/ABORT

# Checkpoint (halt + generate summary)
~/.claude/scripts/liza-checkpoint.sh "End of sprint 1"
```

**5. Review Results**
```bash
# Activity log
cat .liza/log.yaml

# Integration branch
git log integration --oneline
```

### Helper Scripts

The supervisor (`liza-agent.sh`) uses helper scripts for state transitions:

| Script | Purpose |
|--------|---------|
| `liza-claim-task.sh <task-id> <agent-id>` | Atomically claim a task for a coder (creates worktree, updates state) |
| `liza-validate.sh <state.yaml>` | Validate blackboard state against schema invariants |
| `liza-watch.sh` | Monitor blackboard and alert on anomalies |
| `liza-checkpoint.sh <message>` | Create a checkpoint (halt + summary) |
| `liza-init.sh <goal>` | Initialize .liza/ directory with blackboard |

**Important:** The supervisor claims tasks *before* starting the Claude agent. This avoids interactive permission prompts in `-p` (non-interactive) mode. Agents receive their assigned task in the bootstrap prompt and should NOT call claim scripts directly.

See [Architecture Overview](../specs/architecture/overview.md) for detailed component descriptions.
