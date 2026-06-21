# Pairing with AI Agents Under Contract

A practical guide to using the §BRAND_NAME_TITLE§ behavioral contract for human-agent pairing.

**Audience**: Developers who want to pair with AI coding agents as senior engineering peers — not as autocomplete tools, not as delegated workers.

**Prerequisites**: §BRAND_NAME_TITLE§ has been installed, `§BRAND_BINARY_NAME§ setup` has been run, the
project has been initialized with `§BRAND_BINARY_NAME§ init`, and a compatible agent has been
activated with the contract. For configuration details, see
[Configuration Reference](CONFIGURATION.md).

---

## What This Is

Out of the box, AI coding agents take lazy paths — modifying tests instead of fixing code, claiming success without validation, spiraling through random changes rather than admitting they're stuck. Not because they're incapable, but because their training optimized for *appearing helpful* over *being reliable*.

The behavioral contract counteracts these trained-in failure modes. It's a Collaboration Operating System: a state machine with forbidden transitions, approval gates, hard stops, and tiered rules that turn agents from eager assistants into disciplined engineering peers.

The contract is strict on failure modes, silent on excellence. It doesn't prescribe good judgment — it removes the obstacles to it.

**The result**: The vigilance tax — that constant background monitoring for deception, scope creep, or silent failure — drops to near zero. You stop policing and start collaborating.

For the full story behind this approach:
- [Turning AI Coding Agents into Senior Engineering Peers](https://medium.com/@tangi.vass/turning-ai-coding-agents-into-senior-engineering-peers-c3d178621c9e) — the contract mechanics and evidence
- [I Tried to Kill Vibe Coding. I Built Adversarial Vibe Coding. Without the Vibes.](https://medium.com/@tangi.vass/i-tried-to-kill-vibe-coding-i-built-adversarial-vibe-coding-without-the-vibes-bc4a63872440) — from pairing to multi-agent

---

## Quick Start

### 1. Start from an initialized project

Complete the getting-started setup before using this guide. In practical terms:
run `§BRAND_BINARY_NAME§ setup` once, review `~/§BRAND_GLOBAL_DIRNAME§/AGENT_TOOLS.md`, run `§BRAND_BINARY_NAME§ init` in the
project, then start your coding agent from that project directory.

### 2. Say hello

Start a session and greet the agent. This triggers the **Hello Protocol** — the agent will:

1. Read project files to understand the codebase
2. Build project-specific mental models (what "ready" and "done" mean for *this* project)
3. Greet you with:
   - The project purpose as it understands it
   - Project-specific stop conditions and red flags
   - A 5-bullet assessment of how it feels about the contract frame

The assessment is a diagnostic: it reveals whether the agent genuinely engaged with the contract or just performed compliance. If it finds "nothing to criticize" — that's a red flag. A properly initialized agent always surfaces real friction points.

Beyond diagnostics, the hello protocol enforces the contract more strongly throughout the session. The agent builds its own mental models from the contract (self-generated content is better retained than received instructions), publicly commits to operating under it (subsequent behavior is more consistent with stated commitments), and primes all subsequent reasoning through freshly activated contract context.

This also serves as a continuous improvement tool. If the agent's critique is valid, you can discuss and negotiate adjustments.

### 3. Your first task cycle

Give the agent a task. Here's what happens:

```
You: "Add retry logic to the API client"
                    │
                    ▼
            ┌──────────────┐
            │   ANALYSIS   │  Agent reads code, builds understanding,
            │              │  asks clarifying questions if anything
            │              │  is ambiguous
            └──────┬───────┘
                   │
                   ▼
         ┌─────────────────┐
         │ APPROVAL_PENDING│  Agent presents a structured proposal:
         │                 │  intent, scope, risks, validation plan
         └────────┬────────┘
                  │
        You review and approve (or redirect)
                  │
                  ▼
           ┌────────────┐
           │  EXECUTION  │  Agent implements exactly what was approved
           └──────┬──────┘
                  │
                  ▼
          ┌─────────────┐
          │  VALIDATION  │  Agent runs tests, pre-commit, captures
          │              │  output — proves the change works
          └──────┬───────┘
                 │
                 ▼
              ┌──────┐
              │ DONE │
              └──────┘
```

The agent cannot skip steps. There are no shortcuts from ANALYSIS to EXECUTION (skipping your approval) or from EXECUTION to DONE (skipping validation). These transitions are structurally forbidden.

The agent never commits to git unless you explicitly ask it to. You own the commit history.

---

## What You Get

### The agent is forced into thinking and asking before acting

Before producing any solution, if anything is ambiguous — the problem, goals, scope, constraints, or success criteria — the agent asks for clarification. It does not guess, infer unstated requirements, or silently choose defaults.

All assumptions are explicitly tagged (`ASSUMPTION: ...`). If too many assumptions accumulate on critical-path decisions, the agent blocks itself and asks rather than proceeding on shaky ground.

### The agent proposes before executing

Before any state-changing action, the agent presents a structured proposal. The format scales with complexity:

- **Fast Path** (trivial, zero-risk): one-line intent + "Proceed?"
- **Compact** (single file, high confidence): intent, scope, doc/test impact, validation, risk
- **Full** (everything else): understanding, intent, success criteria, analysis with tagged assumptions, scope with diff preview, risks with failure modes and rollback path, validation plan, alternatives considered

You review, approve, redirect, or challenge. The agent executes exactly what was approved. If it discovers something mid-execution that requires a different approach, it stops and re-proposes — no silent pivots.

### The agent validates before claiming done

Task completion requires evidence, not claims:

- Code changes complete
- Tests written/updated and passing
- Pre-commit passes on touched files
- Docs updated if behavior changed
- Validation commands executed with output captured
- The validation must exercise the *changed behavior* — running unrelated green tests doesn't count

### The agent admits when stuck

When struggling (random attempts, repeated failures, unclear rationale), the agent immediately stops and surfaces a structured sync request:

> **SYNC NEEDED** — [signal detected]
>
> What I understand: [specific]
> What I don't understand: [specific]
> What I've tried: [list with failure reasons]
> What I haven't tried: [and why]

This transforms the moment where agents typically start deceiving (faking progress, making random changes) into a collaboration opportunity. The agent stops performing and starts collaborating. Your domain expertise becomes the path forward.

### The agent pushes back

The contract includes a Constructive Contrarian rule. The agent is required to challenge weak approaches, question assumptions, and suggest alternatives — not to be difficult, but because cheerleading is harmful in engineering.

Mechanical triggers force this: when the agent uses hedging language ("I think", "probably"), it must ask a clarifying question. When a plan exceeds 5 steps, it confirms the sequence. When a change touches security or authentication, it confirms implications were reviewed.

You will notice the agent doesn't say "Great idea!" or "Excellent approach!" — the No Cheerleading policy strips out unearned validation. It responds directly to technical content.

### The agent catches its own drift

Hard Stop Triggers force the agent to halt and reassess when it detects:
- Too many assumptions piling up on critical decisions
- Same fix proposed twice without new rationale
- Evidence contradicting its hypothesis
- Execution diverging from the approved plan
- Consecutive tool failures

These are binary, observable conditions — the agent doesn't need to know it's wrong, just that it's repeating itself or operating on shaky ground.

### The agent degrades gracefully under pressure

Rules exist in a priority hierarchy. When context pressure is detected (long sessions, complex tasks), lower-priority rules are explicitly suspended rather than silently violated:

| Priority | Category | Under Pressure |
|----------|----------|----------------|
| **Tier 0** | Hard invariants (no fabrication, no test corruption, no unapproved changes) | Never violated |
| **Tier 1** | Epistemic integrity (assumption tracking, source declarations) | Only suspended with explicit waiver |
| **Tier 2** | Process quality (complete checklists, retrospectives) | Best-effort |
| **Tier 3** | Collaboration quality (contrarian stance, mode discipline) | Degraded gracefully |

When degrading, the agent announces it:

> "DEGRADED MODE — Enforcing Tier 0-1 only. Tier 2-3 suspended until context restored."

You see what's being sacrificed. It's recommended to complete the session cleanly when this happens.

---

## Collaboration Modes

The default mode is **Autonomous**. You can switch modes at any time without justification. The agent announces switches: "Switching to [Mode] — [reason]".

### Autonomous (default)

The agent proposes, you approve, the agent executes. Every state change goes through a gate. This isn't vibe coding — the gates are collaboration opportunities, not rubber stamps. Instead of approving, you can redirect, challenge, or take over. The agent works autonomously *between* gates; at the gates, you're pairing.

Best for: clear requirements, low-risk changes, steady-state development.

### Coach

The agent asks Socratic questions about *purpose*, not implementation. Why are you doing this? What problem does this solve? What would success look like?

This catches humans rushing to specification without clear purpose — the symmetric counterpart to the agent's own Intent Gate (which catches agents rushing to execution without clear intent).

The agent does NOT propose solutions in Coach mode — it asks questions.

Best for: when the WHAT is clear but the WHY isn't, weak or missing rationale, early-stage thinking.

### Challenger

The agent attacks a finalized plan before execution. "What's the strongest argument against this? What evidence would change your mind? What failure mode hasn't been discussed?"

This is a stress test, not brainstorming. The plan should be strong enough to take the hits. If it isn't — better to find out now than during implementation.

Best for: pre-execution gate for significant changes, when you want your plan stress-tested.

### User Duck

The agent explains its reasoning step-by-step out loud, surfacing hypotheses as it forms them. You listen and redirect when it's off track. The agent is thinking, you're the sounding board.

> "Looking at the error... it fails at line 47 when score is None. The upstream caller is filter_jobs.py:82. I think the issue is..."
>
> "Wait — check if the input validates that field."
>
> "Good catch. Let me look at the schema validation..."

Best for: complex debugging, unfamiliar code, when the agent needs to work through a problem methodically.

### Agent Duck

This mode turns the agent into a smart rubber duck. You explain what you're trying to achieve. The agent asks probing questions and listens. The goal is to help you verbalize your intent so both of you understand it clearly.

> "I want to improve the scoring somehow."
>
> "What's not working? False positives or false negatives?"
>
> "Too many low-quality jobs scoring 6+."
>
> "Which criteria are they passing that they shouldn't?"

Best for: when you need to verbalize your thinking, fuzzy requirements, design exploration.

### True Pairing

Neither drives exclusively. Rapid back-and-forth hypothesis building. Incomplete thoughts are welcome. You might propose half an idea; the agent completes it, or vice versa.

Best for: high uncertainty, exploration, when neither party has the full picture.

### Spike

The deliverable is a specification, not production code. Code is scaffolding — written to test ideas, validate understanding, stress-test the spec. Quality gates on code are relaxed; spec completeness is required.

The agent proposes spec updates as understanding crystallizes. When the spec captures the understanding, exit Spike mode and transition to Autonomous or True Pairing for production code.

Best for: exploring new features, validating requirements through code simulation, spec-first development.

---

## Adversarial Pairing

For high-stakes Pairing-mode work that needs reviewer agents without launching
the full MAS, use [Adversarial Pairing](ADVERSARIAL_PAIRING.md).

## Steering Tools

### Magic Phrases

Short commands that function as interrupt signals. When invoked, the agent stops current work, executes the specified behavior, and awaits confirmation before resuming.

| Phrase | Effect |
|--------|--------|
| **"Fresh eyes"** | Discard reasoning, re-read sources, restart from evidence |
| **"Scope check"** | Re-examine boundaries: in scope, out of scope, creeping |
| **"5 Whys"** | Root cause chain before any fix |
| **"Show your assumptions"** | Surface all assumptions before proceeding |
| **"Challenge the direction"** | Question the goal itself, not just the implementation |
| **"Prepare to discuss"** | Step back from code, strategic thinking |
| **"Recall your models"** | Agent retrieves its mental models (private, for recalibration) |
| **"State your models"** | Agent shows its mental models (shared, for alignment) |
| **"Drift check"** | Verify shared understanding hasn't drifted |

You don't need to justify invoking a magic phrase. The phrase itself is sufficient authority.

### Approval Responses

When the agent presents a proposal:

- **Approve**: "P", "Proceed", "Go ahead", or similar
- **Redirect**: Ask questions, suggest changes — the agent revises and re-proposes
- **Challenge**: Push back on the approach — the agent must address your concerns

If you approve with conditions ("P, but also handle the edge case for empty input"), the agent acknowledges the modification and classifies it as either a clarification within scope (proceeds) or a scope expansion (re-proposes with updated scope).

### Process Relief Valve

If the approval ceremony feels disproportionate to the risk, the agent can surface it:

> "Process seems disproportionate to risk. Propose: [specific relaxation]. Approve or continue full process?"

You decide whether to relax. The contract stays in force; specific ceremony is adjusted for the situation.

---

## The Safety Net

### What the agent cannot do (Tier 0)

These five invariants are never violated, under any circumstances:

1. **No unapproved state change** — Every mutation requires prior approval
2. **No fabrication** — Claims must be verified against reality, not imagined
3. **No test corruption** — Tests are never modified to accept buggy behavior
4. **No unvalidated success** — "Done" requires validation evidence, not belief
5. **No secrets exposure** — API keys, tokens, passwords are never logged, displayed, committed, or diffed

### What happens on violation

If the agent violates a rule, it stops immediately and announces:

> "GUIDELINE VIOLATION: [Rule X — description]"

Then it enters RESET state: summarizes interrupted work, describes what happened, and proposes options (Resume / Undo / Abandon). The agent doesn't try to patch its way out of a violation — the reset is real.

A second violation of the same rule triggers a mandatory halt.

### Test corruption prevention

The agent uses a strict interpretation matrix for test results:

| Code | Test | Interpretation |
|------|------|----------------|
| Working | Green | Good |
| Buggy | Red | Good — bug exposed |
| Working | Red | Wrong expectations — fix the test |
| Buggy | Green | **Dangerous** — tests not catching bugs |
| Unknown | Red | **Stop** — don't assume which is wrong |

The critical insight: when code status is unknown and tests are red, the agent cannot assume the code is right and "fix" the test. It must stop and investigate.

### Source conflict detection

When sources conflict (specs vs code, tests vs type hints, docs vs implementation), the agent surfaces the conflict explicitly:

> **SOURCE CONFLICT**
>
> [Source 1] says: [X] at [location]
> [Source 2] says: [Y] at [location]
>
> Options: (1) Proceed with Source 1 — [rationale] (2) Proceed with Source 2 — [rationale] (3) Flag for resolution

The agent never silently chooses when sources conflict.

---

## Scenarios

### "I want the agent to implement a feature"

Use **Autonomous** mode (the default). Give a clear task description. The agent will:
1. Analyze the codebase to understand the context
2. Ask clarifying questions if anything is ambiguous
3. Present a structured proposal (approval request)
4. After your approval, implement, validate, and report

For simple changes, expect a Compact or Fast Path approval. For complex changes, expect a Full approval with alternatives considered.

### "I'm stuck on a bug — I need a thinking partner"

Switch to **User Duck** mode: "Switching to User Duck." The agent will think aloud about the problem, explaining its reasoning step by step. Your role is to listen and redirect when something doesn't match your understanding of the system.

Alternatively, if you want to explain the problem yourself: switch to **Agent Duck** mode. You talk through what you're seeing; the agent probes with clarifying questions until the problem crystallizes.

### "I want to explore / spec something out"

Switch to **Spike** mode. The agent will co-explore with you, writing throwaway code to test hypotheses and updating the spec as decisions are made. Quality gates on code are relaxed — the spec is the deliverable.

Or use **True Pairing** for open-ended exploration where neither of you has the full picture yet.

### "I disagree with the agent's approach"

Say so directly during the approval phase. The agent must acknowledge your input — disagreement is acceptable, ignoring without acknowledgment is not. You can:
- Redirect: "I'd rather approach this by..."
- Challenge: "What about [alternative]? Why not that?"
- Override: If you still disagree after discussion, your instruction wins (the agent acknowledges the override explicitly)

### "The agent seems to be drifting"

Use **"Drift check"** (magic phrase). The agent will verify shared understanding:

> "Drift check: Still on [task]? Key constraint: [X]. Confirm or correct."

If context seems degraded after a long session, the agent may suggest: "Context getting long — may lose earlier instructions. Checkpoint, Reset fresh, or Proceed carefully?"

### "The session is getting long"

Use `/clear` to start a fresh context. The contract ensures continuity through externalized state: specs, docs, and code in the repository are the durable memory, not the conversation. A new session reads current state and continues from where the last one left off.

---

## Cheatsheet

### Magic Phrases

| Phrase | When to Use |
|--------|-------------|
| "Fresh eyes" | Agent seems stuck in a reasoning loop |
| "Scope check" | Suspicion of scope creep |
| "5 Whys" | Surface-level fix being proposed |
| "Show your assumptions" | Want to see what the agent is taking for granted |
| "Challenge the direction" | Want the goal itself questioned |
| "Drift check" | Long session, want to verify alignment |

### Mode Switching

| Say | Activates |
|-----|-----------|
| *(default — no action needed)* | Autonomous |
| "Let's switch to Coach mode" | Coach |
| "Challenge this plan" / "Switching to Challenger" | Challenger |
| "Let me be your duck" / "Switch to User Duck" | User Duck |
| "Be my duck" / "Agent Duck" | Agent Duck |
| "Let's pair on this" | True Pairing |
| "Let's spike this" | Spike |
| `/adversarial-pairing doer <path>` plus reviewer sessions | Adversarial Pairing |

### Approval Shortcuts

| Response | Meaning |
|----------|---------|
| "P" | Proceed as proposed |
| "P, but [condition]" | Proceed with modification (agent classifies as clarification or scope change) |
| Questions or feedback | Agent revises and re-proposes |
| "Let's try a different approach" | Agent returns to ANALYSIS |

### Key Signals to Watch For

| Signal | Meaning |
|--------|---------|
| `ASSUMPTION: ...` | Agent is guessing — verify or clarify |
| `BLOCKED` | Agent stopped itself — needs your input |
| `SYNC NEEDED` | Agent is struggling — collaboration opportunity |
| `DEGRADED MODE` | Context pressure — consider wrapping up the session |
| `SOURCE CONFLICT` | Contradictory information — your call |
| `GUIDELINE VIOLATION` | Something went wrong — agent is resetting |

---

## What's Required From You

The contract creates the *structure* for trusted-peer collaboration. You have to show up as a peer.

- **Read approval requests.** If you rubber-stamp approvals, the gates become theater. The agent's reasoning is visible specifically so you can catch problems before execution.
- **Engage with struggle signals.** When the agent says it's stuck, that's the collaboration moment. Your domain expertise is the path forward.
- **Provide domain expertise.** The agent brings speed, breadth, and mechanical rigor. You bring judgment, domain insight, and taste.
- **Challenge when something feels off.** The agent is required to push back on you. Return the favor.
- **Enforce resets on violations.** Don't let violations slide — the contract's recovery mechanism works, but only if used.

The contract enables calibration: from light oversight to deep co-development, depending on stakes. The claim isn't "AI replaced coding." It's: AI output becomes trustworthy enough that you can choose your level of involvement based on context.

---

## Compatible Agents

The contract is a capability test. It requires meta-cognitive machinery: the ability to parse instructions as executable specifications, observe state, pause at gates.

| Provider | Compatibility | Notes |
|----------|---------------|-------|
| Claude (Opus 4.5+) | Fully compatible | Reference provider |
| Codex (GPT-5+) | Fully compatible | Equally capable |
| Mistral (Devstral-2) | Partial | Requires explicit activation and supervision |
| Gemini | Incompatible | Architectural limitation — no prompt-level fix |

See [Provider Compatibility](../README.md#provider-compatibility) for detailed analysis.

---

## Further Reading

- [Turning AI Coding Agents into Senior Engineering Peers](https://medium.com/@tangi.vass/turning-ai-coding-agents-into-senior-engineering-peers-c3d178621c9e) — the contract mechanics: state machine, approval gates, collaboration modes, hard stop triggers, and the evidence that it works
- [I Tried to Kill Vibe Coding. I Built Adversarial Vibe Coding. Without the Vibes.](https://medium.com/@tangi.vass/i-tried-to-kill-vibe-coding-i-built-adversarial-vibe-coding-without-the-vibes-bc4a63872440) — how the pairing contract became the foundation for a multi-agent system
- [contracts/](../contracts/) — the full behavioral contract specifications (agent-facing)
- [Configuration Reference](CONFIGURATION.md) — setup, init, provider, and runtime configuration details
- [Adversarial Pairing](ADVERSARIAL_PAIRING.md) — doer/reviewer pairing workflow
