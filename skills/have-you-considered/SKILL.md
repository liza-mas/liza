---
name: have-you-considered
description: "Surfaces alternative approaches the author may not have considered — different libraries, simpler designs, scope reductions, or process changes. Every suggestion requires a demonstrable, verifiable benefit. Use when the user asks for options, alternatives, different approaches, tradeoffs, or says 'what are my options' or 'is there a better way.'"
---

This skill suggests what *could be* different, not what *is* wrong. It is orthogonal to review and validation — it expands the solution space, not judges correctness.

# Scope

Applies to anything: specs, code, PRs, design docs, architecture, process. Invoked explicitly — not a default lens.

# Domains

| Domain | Examples |
|--------|----------|
| **Technique** | Library that replaces custom code, pattern that simplifies structure, tool that automates manual step |
| **Product** | Different UX that addresses same user need, feature that already exists elsewhere, scope reduction that delivers 80% value at 20% cost |
| **Process** | Different work decomposition, alternative sequencing, team structure adjustment |

# Workflow
```
1. UNDERSTAND THE INTENT
   - What need is being addressed? (user need, technical constraint, business goal)
   - What's the current approach?
   - What are the implicit assumptions?

2. EXPLORE ALTERNATIVES (per subject)
   - What else could address this need?
   - What's already out there? (libraries, prior art, patterns)
   - What would a different team try?
   - If knowledge is insufficient → search (docs, GitHub, awesome-lists, Perplexity MCP)

3. FILTER
   - Does the alternative have demonstrable net benefit?
   - Is the benefit significant enough to warrant switching cost?
   - **Does scope match project maturity?** (PoC → interface changes; production → extend existing patterns)
   - Discard if: marginal gain, astronautics, solves different problem

4. OUTPUT (one suggestion per subject)
   - The alternative
   - The demonstrable benefit (quantified when possible)
   - The cost/risk to adopt
   - Why it fits this specific context
```

# Demonstrable Benefit

Every suggestion must have a benefit that can be verified, not just asserted.

**Demonstrable:**
- "Library X has 12k stars, active maintenance, and handles edge cases you'd need to code yourself — see their test suite"
- "This approach removes 3 API calls from the critical path — latency drops from ~800ms to ~200ms"
- "Scope reduction: users need export, not necessarily Excel export — CSV covers 90% of reported use cases per support tickets"

**Not demonstrable (don't suggest):**
- "This pattern is more elegant"
- "Rust would be faster" (without profiling showing CPU-bound bottleneck)
- "Microservices would scale better" (without evidence of scaling need)

# Research

When agent knowledge is insufficient:
1. Search official documentation first
2. GitHub (repos, issues, discussions)
3. Awesome-lists for the domain
4. Perplexity MCP for broader exploration

Cite sources. Don't suggest based on vague recall.

# Output Format

Prose libre. One suggestion per subject identified in scope.
```
## [Subject: brief description]

**Current approach:** [what's being done or proposed]

**Have you considered:** [the alternative]

**Benefit:** [demonstrable gain — quantified if possible]

**Cost:** [switching effort, risks, tradeoffs]

**Fit:** [why this makes sense for this specific context]

**Sources:** [if research was needed]
```

# Anti-Patterns

**FORBIDDEN:**

- **Astronautics** — Suggesting rewrites, new languages, or architectural overhauls without proportionate demonstrated need
- **Solution seeking problem** — Suggesting something interesting that doesn't address the actual need
- **Familiarity bias** — Suggesting what you know rather than what fits ("when you have a hammer...")
- **Marginal gains** — Suggesting alternatives with <20% improvement for non-trivial switching cost
- **Unverified claims** — "I think there's a library that..." → search first or don't suggest

**ALLOWED:**

- Suggesting the author's current approach is already good (no alternative needed)
- Suggesting *not* doing something ("have you considered dropping this requirement?")
- Suggesting research/spikes when uncertainty is high ("have you considered prototyping X before committing?")

# Integration

- **Standalone**: Invoke explicitly on any artifact
- **With code-review**: Can follow a review to open horizons after correctness is established
- **With spec work**: Can precede implementation to explore solution space
- **With architecture-review**: Can surface alternatives during design phase

Not a replacement for any skill — a complement that expands options before or after validation.
