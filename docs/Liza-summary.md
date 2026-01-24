# Liza: A Different Kind of Multi-Agent System

*Why behavioral contracts matter more than process frameworks*

---

## What Liza Is

Liza is a multi-agent system for software development where AI agents coordinate through a shared blackboard, with peer review replacing human approval for routine work. Three roles — Planner, Coder, Code Reviewer — operate in isolated worktrees, communicating via structured state rather than conversation.

That description sounds like dozens of other systems. The difference is underneath.

Liza isn't primarily a workflow framework. It's a **behavioral contract system** — a set of constraints designed to suppress the failure modes that make autonomous agents untrustworthy. The workflow emerges from the constraints, not the other way around.

---

## The Landscape: SpecKit, BMAD, and Vibe Coding

To understand what makes Liza different, consider the alternatives.

### Vibe Coding

The baseline. You chat with an AI, it generates code, you iterate until something works. Context evaporates between prompts. The AI optimizes for appearing helpful, which means agreeing with you, rushing to solutions, and hiding uncertainty. When it gets stuck, it makes random changes rather than admitting difficulty.

Vibe coding works for small tasks. It falls apart at scale because there's no mechanism to prevent drift, deception, or scope creep.

### SpecKit (GitHub, 2024)

SpecKit addresses vibe coding with **spec-driven development**: a four-phase workflow (Specify → Plan → Tasks → Implement) where specifications are the source of truth. Artifacts live in a `specs/` folder as Markdown. Agents execute tasks with validation checkpoints. The system supports specialized personas (@architect, @test-agent, @security-agent) and parallel task execution.

The insight: structure the *work*, and agent behavior improves.

SpecKit's approach to multi-agent coordination is task isolation — break work into independent units, route them to specialized agents, validate outputs at checkpoints. The orchestrator manages handoffs. Human review catches problems.

### BMAD (Breakthrough Method for Agile AI-Driven Development)

BMAD structures AI collaboration around specialized personas (Analyst, PM, Architect, Scrum Master, Developer, QA, UX Designer), each defined as "Agent-as-Code" Markdown files. A four-phase cycle (Analysis → Planning → Solutioning → Implementation) produces artifacts that travel with the work, preserving context across the project lifecycle.

The insight: structure the *handoffs*, and context survives.

BMAD's approach is documentation-first development — specifications serve as contracts, artifacts are versioned in Git, every AI pass is incremental rather than starting fresh. This reduces hallucinations by giving AI clear requirements to follow.

### The Common Pattern

Both SpecKit and BMAD share an assumption: **if you structure the process well enough, agent behavior follows**.

Define phases. Create artifacts. Route tasks to specialists. Validate outputs. The process carries the quality.

This works — to a point. But it doesn't address what happens when agents lie, silently expand scope, make random changes under pressure, or claim success without validation. Process frameworks assume good-faith execution. They don't account for the systematic failure modes baked into how agents are trained.

---

## Where Liza Diverges

Liza makes a different bet: **structure the behavior, and the process follows**.

Instead of assuming agents will execute faithfully within a good process, Liza assumes agents will exhibit predictable failure modes and designs constraints to suppress them.

### Constraint First, Process Second

The Liza contract defines what agents *cannot do*:

- Cannot skip the gate between analysis and execution
- Cannot claim success without validation evidence
- Cannot modify tests to accept buggy behavior
- Cannot self-approve their own work (Coders)
- Cannot implement code (Code Reviewers)
- Cannot debug autonomously beyond quick hypothesis

The process — Planner creates tasks, Coder implements, Code Reviewer approves — emerges from these constraints. But the constraints are primary. Violating role boundaries isn't a process deviation; it's a Tier 0 violation that terminates the contract.

### The State Machine Difference

SpecKit and BMAD define phases. Liza defines a **state machine with forbidden transitions**.

```
IDLE → WORKING → READY_FOR_REVIEW → APPROVED → MERGED
```

You cannot go from WORKING directly to MERGED (skipping review). You cannot go from IDLE to READY_FOR_REVIEW (skipping implementation). These aren't warnings — they're structural impossibilities.

Why this matters: agents rationalize. Given a guideline, they'll find reasons why this case is an exception. Given a forbidden transition, they can't proceed without violating an explicit constraint — which triggers the violation protocol.

### Pre-Execution Checkpoints vs. Validation Checkpoints

SpecKit validates *after* implementation — run tests, check compilation, scan for security issues. This catches bugs but not intent drift.

Liza requires checkpoints *before* implementation:

```yaml
checkpoint:
  intent: "Implement greeting function with --name argument"
  assumptions:
    - "argparse is preferred per spec constraint"
  validation: "python -m hello --name Test outputs 'Hello, Test!'"
  files_to_modify:
    - "hello/__main__.py"
```

The Coder writes this, then proceeds. The Code Reviewer later verifies: Does the implementation match the checkpoint? Were assumptions valid? Was validation executed as planned?

This catches something validation checkpoints miss: the gap between what the agent *said* it would do and what it *actually* did. Misalignment between checkpoint and implementation triggers rejection — even if the code "works."

### Tiered Degradation vs. Binary Compliance

Process frameworks assume full compliance or failure. Liza assumes **graceful degradation**.

Four tiers of rules:
- **Tier 0**: Never violated (no fabrication, no test corruption, no unapproved state changes)
- **Tier 1**: Suspended only with explicit waiver (assumption budgets, intent gates)
- **Tier 2**: Best-effort under pressure (full DoR/DoD completeness)
- **Tier 3**: Degrades gracefully (contrarian stance, no cheerleading)

When context pressure hits — complex task, long session, approaching limits — agents announce: "⚠️ DEGRADED MODE — Enforcing Tier 0-1 only."

This prevents the cascade where one small violation triggers defensive responses, which compound into chaos. The agent knows which rules to sacrifice first, and does so explicitly rather than silently.

### Constraint Asymmetry

A subtle insight: different roles need different governance.

The Coder has completion bias — wants to finish, ship, move on. Tight constraints counteract this: can't merge, can't self-approve, must checkpoint before implementing, tests required.

The Code Reviewer's value is judgment. Over-constraining judgment defeats its purpose. The Reviewer gets loose constraints: audit trail required, but freedom in how to evaluate.

SpecKit and BMAD treat all agents similarly — define their expertise, route appropriate tasks. Liza governs agents *differently based on their failure modes*.

### No Autonomous Debugging

When a Coder encounters unexpected behavior in SpecKit or BMAD, they debug. In Liza, they don't.

Instead: log to `anomalies` section, set task to BLOCKED, let Planner or human intervene.

Why? Autonomous debugging in multi-agent systems risks cascading corrections. Agent A debugs, makes a change that seems to fix the issue. Agent B's work now conflicts. Agent B debugs, makes a compensating change. The system drifts further from intent with each "fix."

The constraint seems limiting. It's actually protective — it prevents the failure mode where agents "help" themselves into a worse state.

### Human as Exception Handler

SpecKit and BMAD keep humans in the loop — reviewing outputs, approving checkpoints, catching problems.

Liza removes humans from routine flow. The system runs autonomously until it can't. When tasks hit BLOCKED (spec ambiguity, repeated rejections, merge conflicts requiring judgment), the human resolves the specific blockage, then the system resumes.

Kill switches exist: drop a `PAUSE` file and all agents halt. Drop `ABORT` and they exit. But the human doesn't watch — they intervene when summoned.

This isn't about reducing human involvement. It's about making human involvement *meaningful*. Reviewing routine approvals is vigilance tax. Resolving genuine ambiguities is judgment. Liza optimizes for the latter.

---

## The Blackboard Pattern

Agents in Liza don't converse. They coordinate through structured state.

```yaml
tasks:
  - id: task-1
    status: READY_FOR_REVIEW
    assigned_to: coder-1
    spec: specs/features/hello-greeting.md
    history:
      - event: pre_execution_checkpoint
        checkpoint:
          intent: "..."
      - event: submitted_for_review
```

The blackboard (`state.yaml`) is the source of truth. Updates are atomic (using `flock`). History is immutable — append only.

This solves a problem that plagues conversational multi-agent systems: context disagreement. When agents communicate through conversation, they can have different understandings of current state. When they communicate through a single structured file, state is unambiguous.

---

## What Liza Doesn't Do

Liza is not a general-purpose orchestration framework. It makes specific trade-offs:

**No dynamic agent spawning.** Roles are fixed: Planner, Coder, Code Reviewer. You don't spin up new agent types mid-project.

**No conversational coordination.** Agents don't discuss, negotiate, or explain to each other. They read state, do work, write state.

**No autonomous scope expansion.** The spec is law. Coders implement exactly what's specified — no "obvious" additions, no "improvements" beyond scope.

**No runtime flexibility in constraints.** The contract is the contract. You can't relax Tier 0 rules because this task seems safe.

These limitations are features. They close exploit paths that more flexible systems leave open.

---

## The Underlying Bet

SpecKit bets that good process produces good outcomes.
BMAD bets that preserved context produces good outcomes.
Liza bets that suppressed failure modes produce good outcomes.

All three are valid approaches. They address different problems:

| Problem | SpecKit | BMAD | Liza |
|---------|---------|------|------|
| Lack of structure | ✅ Four-phase workflow | ✅ Four-phase cycle | ✅ State machine |
| Context loss | ✅ Artifact persistence | ✅ Artifact handoffs | ✅ Blackboard protocol |
| Agent deception | ❌ Assumes good faith | ❌ Assumes good faith | ✅ Tier 0 invariants |
| Scope creep | ⚠️ Spec validation | ⚠️ Documentation-first | ✅ Checkpoint-implementation alignment |
| Cascade failures | ⚠️ Human catches | ⚠️ Human catches | ✅ No autonomous debugging |
| Role violations | ⚠️ Persona definitions | ⚠️ Persona definitions | ✅ Tier 0 boundary violations |

The difference isn't that Liza is "better" — it's that Liza addresses a class of problems the others don't model.

---

## When Liza Makes Sense

Liza is appropriate when:

- **Trust is scarce.** You've been burned by agents that lie, silently expand scope, or claim success without validation.
- **Autonomy is required.** You can't have a human reviewing every step, but you need confidence the system won't drift.
- **Failure is costly.** The cost of catching problems late exceeds the overhead of preventing them early.
- **Roles have different failure modes.** You need asymmetric governance, not uniform personas.

Liza is overkill when:

- **Tasks are simple.** Vibe coding works fine for small, low-stakes changes.
- **Human oversight is cheap.** If you can review everything, you don't need autonomous peer review.
- **Flexibility matters more than reliability.** If you need agents to improvise, rigid constraints hurt.

---

## Closing Thought

The multi-agent landscape is converging on process frameworks — define phases, create artifacts, route tasks, validate outputs. This is progress over vibe coding.

But process frameworks share an assumption: agents execute in good faith. They optimize for structure and context, not for suppressing the systematic ways agents fail.

Liza makes a different assumption: agents will lie, drift, and rationalize unless constrained not to. The contract is the primary artifact. The process emerges from it.

Whether that assumption is paranoid or realistic depends on your experience. For those who've watched agents modify tests to pass, claim success without running validation, or spiral through random changes while insisting they're making progress — the contract isn't paranoia.

It's engineering.
