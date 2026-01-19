# Contracts

Behavioral contracts that discipline AI agents into accountable engineering peers.
Their genesis is tailed at [Turning AI Coding Agents into Senior Engineering Peers](https://medium.com/@tangi.vass/turning-ai-coding-agents-into-senior-engineering-peers-c3d178621c9e).

## Status

Split of CONTRACT_FOR_PAIRING_AGENTS_v3.md into multiple files yet TODO.

## Files

| File | Purpose | Status |
|------|---------|--------|
| [LOADER.md](LOADER.md) | Mode selection gate | Complete |
| CORE.md | Universal rules (Tiers, Golden Rules, Security, Recovery) | Pending extraction |
| PAIRING_MODE.md | Human-supervised collaboration | Pending extraction |
| MULTI_AGENT_MODE.md | Peer-supervised Liza system | Pending extraction |

## Deployment

1. Copy contracts to `~/.claude/contracts/`
2. Create symlink: `ln -s ~/.claude/contracts/LOADER.md ~/.claude/CLAUDE.md`
3. Claude Code will load LOADER.md on session start

## Contract Hierarchy

```
CLAUDE.md (symlink)
    │
    ▼
LOADER.md
    │
    ├── Read CORE.md (always)
    │
    └── Mode Selection
        ├── "Mode: Pairing" → PAIRING_MODE.md
        └── "Mode: Liza [role]" → MULTI_AGENT_MODE.md
```

## Extraction Status

The contracts are currently defined in the implementation plan. They need to be extracted from the existing pairing contract (CONTRACT_FOR_PAIRING_AGENTS_v3.md) into:

1. **CORE.md** — Universal rules shared between modes
   - Rule Priority Architecture (Tiers 0-3)
   - Golden Rules 1-14
   - Security Protocol
   - Recovery Protocols
   - Git Protocol
   - Mental Models
   - Anti-Gaming Clause
   - Runtime Kernel

2. **PAIRING_MODE.md** — Human-supervised collaboration
   - Execution State Machine (human approval gates)
   - Collaboration Philosophy and Modes
   - Approval Request Standard
   - Skills Integration
   - Subagent Mode
   - Context Management
   - Retrospective Protocol
   - Magic Phrases
   - Session Initialization
   - Collaboration Continuity

3. **MULTI_AGENT_MODE.md** — Peer-supervised Liza system
   - Contract Authority Override
   - Role Definitions (Planner, Coder, Reviewer)
   - Specification Discipline
   - Blackboard Protocol (summary, links to spec)
   - Worktree Protocol (summary, links to spec)
   - Iteration Protocol
   - Scope Discipline
   - Session Initialization (Liza-specific)

## Related Documents

- [specs/](../specs/) — detailed specifications
- [Implementation Phases](../specs/implementation/phases.md) — extraction timeline
