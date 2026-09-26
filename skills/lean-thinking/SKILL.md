---
name: lean-thinking
description: Inventory wastes (useless, redundant) and frictions (errors, excess complexity, contention) in any flow — process, workflow, agent system, docs, or code — measured against what its consumer values.
---

Everything a flow does either delivers what its consumer values, enables that delivery, or is waste. Everything that makes delivery harder than it needs to be is friction. Name both with evidence; the owner decides what to change.

# Distinct From

| Skill | Its lens | This skill's lens |
|-------|----------|-------------------|
| `code-quality-assessment`, `clean-code` | Code smells, maintainability | Cost to the flow, not to the code |
| `systemic-thinking` | Coherence, fragility, future risk | Present cost of how work moves |
| `context-engineering` | Prompt payload fit | Whole-flow effort, time, rework |
| `have-you-considered` | Alternative approaches | What to stop, merge, or unblock |

Code-level issues are in scope only when they cost the flow (rework, waiting, relearning) — report that cost, not the smell.

# Workflow

```
1. FRAME THE VALUE STREAM
   - Consumer: who receives the output? (end user, next stage, reviewer, agent)
   - Value: what would they notice missing? If unclear → ask. Without it, "useless" is undefined.
   - Map trigger → delivered value: steps, handoffs, queues, artifacts, loops.
   - Mark each step: value-adding | enabling (verification, safety, compliance) | candidate waste.
   - Gather evidence: artifacts, logs, git history, timings, retries. Measure where possible
     (lead time vs touch time, rework loops, queue durations). Observe the running system
     (live state, what actors actually do), not only its declared configuration.

2. SWEEP EVERY CATEGORY (below) — full inventory, no early stop.

3. LOAD-BEARING CHECK each candidate (below).

4. RANK by avoidable cost over a stated horizon: frequency × unit cost (time, tokens, rework,
   cognitive load, consumed irreversible inputs), including what it causes downstream. Sunk
   cost does not rank. Compare in one unit where conversion is defensible; otherwise rank
   within each unit and state the judgement that orders them. Mark each cost measured or
   estimated. Findings that feed each other (e.g. retries saturating the lock that blocks
   recovery) rank once, as a loop.
```

# Categories

**Waste — useless or redundant work (muda)**

| Category | Question | Signals |
|----------|----------|---------|
| Unused | Produced but never (or not yet) consumed? | Dead code, unread docs, outputs no stage reads, config that never varies, speculative features |
| Redundant | Same value produced or held twice? | Duplicated logic, restated rules, parallel mechanisms, copies kept in sync by hand |
| Over-processing | More effort than the consumer needs? | Ceremony disproportionate to risk, re-validation of unchanged state, excess precision |
| Inventory | Partially done work waiting? | Stale branches, unmerged work, parked tasks, backlog rot, high WIP |
| Waiting | Time where nothing advances? | Queues, approval latency, idle actors, blocked tasks, slow feedback loops, status that hides the wait (stuck work shown as ready or idle) |
| Handoffs | Context lost when work changes hands? | Re-explanation, lossy summaries, knowledge in chat instead of artifacts |
| Relearning | Knowledge rediscovered instead of retained? | Repeated investigations, same question asked twice, uncaptured lessons |
| Switching | Effort spent navigating instead of doing? | Task switching, hunting for information, tool juggling |

**Friction — what makes delivery harder**

| Category | Question | Signals |
|----------|----------|---------|
| Defects | Where is output redone? | Rejections, reverts, retries, flaky checks, fix-the-fix loops, late detection (caught far downstream of where introduced), masked failures (reported as success, fail-open checks) |
| Complexity / overburden (muri) | What demands exceed capacity, or is harder than the problem requires? | Accidental complexity, special cases, large config surface, context exhaustion, steps a newcomer cannot follow |
| Unevenness (mura) | Where does flow lurch? | Batch-and-burst, uneven task sizing, spiky load, inconsistent conventions |
| Contention | Where do actors compete for a shared resource? | Locks, single reviewer or bottleneck role, merge conflicts, shared state, rate limits |

A step can carry several findings; report each under the category that best explains its cost.

**Agent systems:** tokens are usually the dominant unit cost. Measure them per delivered output
(e.g. per accepted task) and per step, then file heavy consumers under the category that explains
them: unchanged context re-read or re-injected → Redundant; re-exploring what an earlier agent
found → Relearning; retries and rejected rounds → Defects; polling or idle sessions → Waiting;
oversized prompts → Complexity / overburden.

**Multi-agent runs:** read [references/mas-run.md](references/mas-run.md) for evidence sources, repair-action metrics, and measurement pitfalls.

# Load-Bearing Check

Apparent waste is often protection. Before a candidate becomes a finding:

- **Purpose:** What does it guard against? Redundancy may be defense in depth; waiting may be a deliberate gate; ceremony may be the mechanism holding trust.
- **Coupling:** What else was justified by pointing at it? Removing it may void that justification.
- **Unknown purpose ≠ useless.** If the purpose cannot be established, report it as `CANDIDATE` and ask the owner.
- **Enabling work is not waste** — its excess may be.
- **Protection still has a running cost.** A load-bearing mechanism stays, but each time it fires, and each repair it forces afterwards, is friction: report it and target what trips it.

# Output Format

Markdown renders consecutive lines as one paragraph: keep each field a list item.

```markdown
## Value Stream
- **Consumer:** … — **Value:** …
- **Flow:** step → step → …
- **Lead time / touch time:** … (if measurable)

## Findings (ranked by cost)

### W-n | F-n  [Category] — title
- **Evidence:** <location, metric, log excerpt> [measured | estimated]
- **Cost:** <what it costs, how often, over what horizon> [avoidable | sunk]
- **Detection:** <step that caught it> (introduced at <step>) — defects only
- **Loop:** <W-n ↔ F-n> — only when findings feed each other
- **Load-bearing:** <what it protects and why it is still waste> | CANDIDATE — <open question for owner>
- **Countermeasure (hypothesis):** <option> — expect <metric change>; disproved if <observation>
- **Displacement:** <where the cost would move if removed>

## Coverage
- **Swept, no finding:** <categories>
- **Insufficient evidence:** <category — what could not be observed>
```

# Anti-Patterns

**FORBIDDEN:**
- **Waste without a consumer** — calling something useless without naming who does not value it
- **Fence removal** — labelling redundancy waste without the load-bearing check
- **Smell hunting** — style or code-quality issues with no demonstrated flow cost
- **Manufactured findings** — filling categories to look thorough; an empty category is a valid result
- **Local optimization** — a countermeasure that moves the cost to another step without saying so
- **Asserted ranking** — ordering by perceived importance instead of evidenced cost

**ALLOWED:**
- Concluding the flow is already lean
- Finding that the waste is in the value definition itself (building what no consumer needs)

# Integration

Standalone, invoked explicitly on any flow. Complements `have-you-considered` (alternatives for a found waste) and `systemic-thinking` (risk the countermeasures might introduce).
