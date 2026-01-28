# Liza - Usage Guide

## Activation of the Contract for Pairing Agents

See [Contract Activation](../contracts/contract-activation.md).

## Liza

See [DEMO](DEMO.md) for a full example.

### Quick Start (Target Usage)

**Prerequisites:**
- Claude Code CLI, git and `yq` installed
- `yq` installed (YAML processor):
  `sudo wget https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64 -O /usr/local/bin/yq && sudo  chmod +x /usr/local/bin/yq` (dont use snap, codex cannot use it)

**1. Initialize**
```bash
# Create .liza/ directory with blackboard
~/.liza/scripts/liza-init.sh "[Goal description]" [spec_ref]

# spec_ref: Path to goal specification (default: specs/vision.md)
# Examples:
#   liza-init.sh "Implement retry logic"           # uses specs/vision.md
#   liza-init.sh "Add auth" specs/auth-feature.md  # uses custom spec

# Verify
cat .liza/state.yaml
```

**2. Start Agents (3 terminals)**

Terminal 1 — Planner:
```bash
LIZA_AGENT_ID=planner-1 ~/.liza/scripts/liza-agent.sh planner
```

Terminal 2 — Coder:
```bash
LIZA_AGENT_ID=coder-1 ~/.liza/scripts/liza-agent.sh coder
```

Terminal 3 — Code Reviewer:
```bash
LIZA_AGENT_ID=code-reviewer-1 ~/.liza/scripts/liza-agent.sh code-reviewer
```

**3. Observe**
```bash
# Run the watcher for alerts
~/.liza/scripts/liza-watch.sh
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
~/.liza/scripts/liza-checkpoint.sh "End of sprint 1"
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
| `release-claim.sh <task-id> [--role reviewer|coder|both] [--full] [--force] [--reason "..."]` | Manually release reviewer/coder claims on a task (crash recovery) |
| `liza-checkpoint.sh <message>` | Create a checkpoint (halt + summary) |
| `liza-init.sh <goal> [spec_ref]` | Initialize .liza/ directory with blackboard (spec_ref defaults to specs/vision.md) |

**Important:** The supervisor claims tasks *before* starting the Claude agent. This avoids interactive permission prompts in `-p` (non-interactive) mode. Agents receive their assigned task in the bootstrap prompt and should NOT call claim scripts directly.

See [Architecture Overview](../specs/architecture/overview.md) for detailed component descriptions.
