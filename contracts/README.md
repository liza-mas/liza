# Contracts

Behavioral contracts that discipline AI agents into accountable engineering peers.
Their genesis is tailed at [Turning AI Coding Agents into Senior Engineering Peers](https://medium.com/@tangi.vass/turning-ai-coding-agents-into-senior-engineering-peers-c3d178621c9e).

## Status

Contract split complete. Original `CONTRACT_FOR_PAIRING_AGENTS_v3.md` preserved for reference.

## Files

| File | Purpose | Status |
|------|---------|--------|
| [CORE.md](CORE.md) | Entry point + universal rules | Complete |
| [PAIRING_MODE.md](PAIRING_MODE.md) | Human-supervised collaboration | Complete |
| [MULTI_AGENT_MODE.md](MULTI_AGENT_MODE.md) | Peer-supervised Liza system | Complete |
| [CONTRACT_FOR_PAIRING_AGENTS_v3.md](CONTRACT_FOR_PAIRING_AGENTS_v3.md) | Original monolithic contract (reference) | Preserved |

## Deployment

Symlink contracts into `~/.claude/`:

```bash
ln -sf /path/to/contracts/CORE.md ~/.claude/CLAUDE.md
ln -sf /path/to/contracts/PAIRING_MODE.md ~/.claude/PAIRING_MODE.md
ln -sf /path/to/contracts/MULTI_AGENT_MODE.md ~/.claude/MULTI_AGENT_MODE.md
```

Claude Code loads `~/.claude/CLAUDE.md` on session start, which triggers mode selection and loads the appropriate mode contract.

## Contract Hierarchy

```
CLAUDE.md (symlink)
    │
    ▼
CORE.md (entry point + universal rules)
    │
    └── Mode Selection Gate (auto-detect from bootstrap)
        │
        ├── Default (no Liza agent) → Read PAIRING_MODE.md
        │                              → Execute Session Initialization
        │                              → (read files, build models, greet)
        │
        └── "You are a Liza ... agent" → Read MULTI_AGENT_MODE.md
                                       → Execute Session Initialization
                                       → (read role/blackboard, silent)
```

**Key:** No response until Session Initialization from mode contract is complete.

## Content Summary

### CORE.md — Entry point + universal rules
- **Initialization Sequence** (mode selection → read mode contract → execute Session Initialization)
- Rule Priority Architecture (Tiers 0-3)
- **Execution State Machine** (generalized with mode-specific gate semantics)
- Golden Rules 1-14
- Skills Integration
- Protocol References (Debugging, Test, Architecture, Tools)
- Context Management
- Security Protocol
- Recovery Protocols
- Git Protocol
- Mental Models
- Anti-Gaming Clause
- Runtime Kernel

### PAIRING_MODE.md — Human-supervised collaboration
- Contract Authority (human overrides)
- Gate Semantics (Pairing): approval request → human approves
- Collaboration Philosophy and Modes
- Approval Request Standard
- Skills Integration
- Subagent Mode/Delegation
- Context Management
- Retrospective Protocol
- Magic Phrases
- Session Initialization
- Collaboration Continuity

### MULTI_AGENT_MODE.md — Peer-supervised Liza system
- Contract Authority (blackboard as source of truth)
- Role Definitions (links to specs)
- **Pre-Execution Checkpoint** (gate artifact for MAM)
- Gate Semantics (Multi-Agent): checkpoint written = gate cleared
- Task State Machine (blackboard workflow lifecycle)
- Blackboard Protocol
- Worktree Protocol
- Iteration Protocol
- Scope Discipline (spec is law)
- Communication Protocol
- Circuit Breaker
- Human Intervention Points

**Key Insight:** The Execution State Machine in CORE.md is universal — it forces structured thinking before action. The "gate" is the mechanism:
- **Pairing**: Gate = human approval (agent waits)
- **Multi-Agent**: Gate = checkpoint written to blackboard (self-clearing, but forces the same thinking; Code Reviewer verifies alignment later)

## Related Documents

- [specs/](../specs/) — detailed specifications
- [Implementation Phases](../specs/implementation/phases.md) — extraction timeline
