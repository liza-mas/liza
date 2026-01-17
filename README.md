# Liza

A disciplined peer-supervised multi-agent coding system that makes AI agents accountable engineering peers,
not just autonomous yet unreliable assistants.

## Genesis

It all started with deceiving agents—tests being repeatedly altered to pass despite explicit instructions.
A behavioral contract has been developed incrementally as a response to actually faced AI coding agent misbehaviours, to
discipline and turn them from eager assistants into reliable senior engineering peers.

Going more systematical led to covering 55+ LLM failure modes documented in the literature:
sycophancy, phantom fixes, scope creep, test corruption, hallucinated completions, and dozens more.
Each failure mode got a countermeasure. The countermeasures became rules. The rules became a contract.

The original contracts define collaboration modes (Autonomous, UserDuck, AgentDuck, Pairing, Spike), approval gates,
execution state machines, and behavioral constraints that prevent agents from silently drifting, fabricating success,
or corrupting tests to make code pass.

For the full story and detailed explanation of the contract: [Turning AI Coding Agents into Senior Engineering Peers](https://medium.com/@tangi.vass/turning-ai-coding-agents-into-senior-engineering-peers-c3d178621c9e).
The different [contract versions](contracts/) are included here.

After six months of contract-disciplined pairing, the approval gates became quite boring as violations disappeared and
requests got fulfilled as expected most of the time. Yet these gates are load-bearing and cannot be removed without
losing the benefits of the contract. Liza is what comes next: delegating approval to peer agents who operate under the
same contract, so humans can observe and provide direction without bottlenecking or rubber-stamping.

## The Problems

Single-agent coding works until it doesn't. The agent marks their task complete when it isn't. It "fixes" things you
didn't ask for. It claims tests pass when they don't (or have been silently greenwashed). At best human review catches these
failures—but human review doesn't scale.

Multi-agent systems promise coordination, but most inherit the same failure modes and add new ones: agents approve each
other's mistakes, drift collectively from the goal, or converge confidently on wrong solutions.

Multiple autonomous yet unreliable agents thus lead to vibe coding chaos.
Let's break down the problem:

| Problem                                               | Typical solution                                | Limits                                                                                             | Our different approach                                        | Benefits                                            |
|-------------------------------------------------------|-------------------------------------------------|----------------------------------------------------------------------------------------------------|---------------------------------------------------------------|-----------------------------------------------------|
| Coding agents are not trustworthy out of the box      | Complex prompts                                 | Prompts don't really bind the agents and are painful to use systematically                         | A behavioral contract countering the chatbot-inherited biases | Agents become trustworthy senior-level peer         |
| The path to satisfying task completion may be painful | Ralph Wiggum loop until completion              | Focuses on mitigating symptoms. Requires upfront stable specification (back to waterfall). Costly. | An externally validated completion (or actionable feedback)   | The coder-reviewer pair supports more complex tasks |
| Agents require prompts                                | Project specification frameworks (e.g. SpecKit) | Don't address agent reliability or collaboration                                                   | Specs are consumed automatically by the agents                | Structured yet autonomous execution                 |
| Multiple agents require human coordination            | Agent coordination frameworks (e.g. BMAD)       | Don't address agent reliability or efficient convergence                                           | A blackboard mechanism supports the agent coordination        | Flexible coordination                               |

## The Approach

Liza combines four ideas:

- **Behavioral contracts** (from the original work) discipline individual agents. Tier 0 invariants are never violated:
no unapproved state changes, no fabrication, no test corruption, no unvalidated success. Agents operate under explicit
rules, not vibes, turning them into trustworthy senior-level peers. Different modes enable pairing with humans or with
other agents.

- **Externally validated completion on Ralph-like loops** replaces self-certification. A coder agent cannot mark their
own work complete. A reviewer examines the work and issues a binding verdict. Approval means merge eligibility.
Rejection means specific, actionable feedback and another loop.

- **Specification system** externalizes context to make it durable. Agents are stateless—"every restart is a new mind
with old artifacts." Specs *are* those artifacts. The planner reads specs and docs to decompose goals. Coders read specs
to understand tasks without asking. Reviewers validate against specs, not just tests. Without specs, agents rediscover
requirements and repeat mistakes. With specs, they read shared understanding and execute.

- **Blackboard coordination** makes all state visible. A shared file tracks goals, tasks, assignments, and history.
Agents claim tasks, update status, and hand off work through the blackboard. Humans can observe everything, intervene
surgically, or pause the system entirely.

The human owns the intent and acts as observer and circuit-breaker, not a bottleneck. Peer agents approve.
Human authority is exercised through a kill switch, not an approval queue.

## Design Philosophy

> Systems that optimize for immediate output generate *muda*—defects, rework, and correction loops. By optimizing for trust, quality, and auditability, Liza eliminates these wasted cycles—and should reach completion sooner, not later. The contract proved it: Quality is the fastest path to real completion.

This means:

- **Bounded failure over prolonged negotiation.** If two coders fail the same task, the task is wrong—rescope it, don't reassign it.
- **Explicit state over implicit coordination.** Everything is in the blackboard. No hidden handshakes between agents.
- **Every restart is a new mind with old artifacts.** Agents don't assume continuity. They read state fresh and verify their claims.
- **Reviewer authority is real.** Reviewers don't suggest—they approve or reject. Coders address feedback specifically, not creatively.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Human                               │
│   (leads specs, observes terminals, reads blackboard,       │
│               kills agents, pauses system)                  │
└─────────────────────────────────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          ▼                   ▼                   ▼
    ┌───────────┐        ┌──────────┐        ┌──────────┐
    │ Planner   │        │  Coder   │        │ Reviewer │
    │           │        │          │        │          │
    │ Decomposes|        │ Claims   │        │ Examines │
    │ goal into │        │ tasks,   │        │ work,    │
    │ tasks,    │        │ iterates │        │ approves │
    │ rescopes  │        │ until    │        │ or       │
    │ on failure│        │ approved │        │ rejects, │
    │           │        │  review  │        │ merges   │
    └─────┬─────┘        └────┬─────┘        └────┬─────┘
          │                   │                   │
          └───────────────────┴───────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │   .liza/        │
                    │   state.yaml    │  ← blackboard
                    │   log.yaml      │  ← activity history
                    └─────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │  .worktrees/    │
                    │  task-1/        │  ← isolated workspaces
                    │  task-2/        │
                    └─────────────────┘
```

## Key Mechanisms

**Leases, not just heartbeats.** Agents hold time-bounded leases on tasks. A stale agent's task becomes reclaimable only
after the lease expires. No ambiguity about ownership.

**DRAFT tasks.** Planner writes tasks as DRAFT, finalizes to UNCLAIMED. Coders cannot claim half-written tasks.

**Commit SHA verification.** Coder records commit SHA when requesting review. Reviewer verifies the SHA before examining
work. No reviewing stale state.

**Reviewer-only merge.** Coders commit to their worktree. Only reviewers can merge to the integration branch. Authority
is structural, not advisory.

**Hypothesis exhaustion.** If two different coders fail the same task, the task framing is presumed wrong. Planner must
rescope—cannot just reassign unchanged.

**Rescoping audit trail.** When tasks are rescoped, original task becomes SUPERSEDED with explicit reason. New tasks
reference what they replace. No silent rewrites.

### Cost Gradient

The contract defines a cost gradient for where errors are discovered:

```
Thought → Words → Specs → Code → Tests → Docs → Commits
  ◄─────────────── cheaper ─────────────────────────►
```

Errors caught in specs cost less than errors caught in code. Errors caught in code cost less than errors caught in tests.
The spec system front-loads understanding so agents don't discover requirements by failing tests.

### Spec Discipline

From the contract:

- **Spec & TODO Trigger:** When clarification reveals scope ambiguity, propose adding/updating spec before implementation
- **Spec first, code second, doc third:** Order of operations matters
- **Session Continuity:** `specs/` and `docs/` are durable memory. Each session: read current state → perform atomic task → write updated state

In Liza multi-agent mode:
- Planner ensures specs exist before creating tasks
- Coders cannot claim tasks for under-specified work (triggers BLOCKED, not guessing)
- Reviewers reject work that doesn't match spec (not just work that doesn't pass tests)

---

## Naming

**Liza** combines two references:

**Lisa Simpson**—the disciplined, systematic counterpoint to Ralph Wiggum. The [Ralph Wiggum technique](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum) loops agents until they converge through sheer persistence. Lisa makes sure the work is actually right. Ralph iterates; Lisa thinks upfront and verifies.

**ELIZA**—the 1966 chatbot that demonstrated structured dialogue patterns. Liza is about structured collaboration patterns:
explicit states, binding verdicts, auditable transitions.

Liza is not autonomous. She is accountable.

## Status

Liza is currently a **detailed implementation plan**.

The contract restructuring, blackboard schema, coordination protocols, and tooling are specified.

See `specs/liza-implementation-plan.md` for the full specification.

## Typical structure of a project using Liza

Target structure:
```
~/.claude/
├── CLAUDE.md                      → contracts/LOADER.md (symlink)
├── contracts/
│   ├── LOADER.md                  # Mode selection gate
│   ├── CORE.md                    # Universal rules (Tiers, Golden Rules)
│   ├── PAIRING_MODE.md            # Human-supervised collaboration
│   └── MULTI_AGENT_MODE.md        # Peer-supervised Liza system
├── schemas/
│   └── liza-state.yaml            # Blackboard schema
└── scripts/
    ├── liza-init.sh               # Initialize blackboard
    ├── liza-lock.sh               # Atomic operations
    ├── liza-validate.sh           # Schema validation
    ├── liza-watch.sh              # Alarm monitor
    ├── liza-agent.sh              # Agent supervisor
    ├── wt-create.sh               # Create worktree
    ├── wt-merge.sh                # Merge (reviewer-only)
    └── wt-delete.sh               # Clean up worktree

<project>/
├── .liza/
│   ├── state.yaml                 # Current state
│   ├── log.yaml                   # Activity history
│   └── archive/                   # Terminal-state tasks
└── .worktrees/
    └── task-N/                    # Per-task workspace
```

## Requirements

- Claude Code (v1 target)
- Git with worktree support
- `yq` for YAML processing
- `flock` for file locking (standard on Linux)
- Bash 4+

## License

Apache 2.0

## Acknowledgments

The behavioral contract draws on research into LLM failure modes, sycophancy patterns, and code generation failures.
The multi-agent design incorporates ideas from:

- **[Ralph Wiggum technique](https://github.com/anthropics/claude-code/tree/main/plugins/ralph-wiggum)** — iteration until convergence
- **[SpecKit](https://github.com/github/spec-kit)** - Project specification
- **[BMAD Method](https://github.com/bmad-code-org/BMAD-METHOD)** — role templates and workflow patterns
- **Classical blackboard architecture** — shared state coordination

Liza synthesizes these into a system optimized for thoughtfulness, trust and auditability, leading to faster execution thanks to fewer cycles.
