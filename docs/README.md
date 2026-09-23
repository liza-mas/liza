# Liza Documentation Index

## Quick Navigation

### User Guides

| Document | Purpose |
|----------|---------|
| [Getting Started](../GETTING_STARTED.md) | Install and first-run setup path |
| [Pairing Usage](../support-docs/USAGE_PAIRING.md) | Practical guide to human-agent pairing under contract |
| [Adversarial Pairing](../support-docs/ADVERSARIAL_PAIRING.md) | Doer/reviewer pairing through a shared Markdown blackboard |
| [Multi-Agent Usage](../support-docs/USAGE_MULTI_AGENTS.md) | Running Liza as a multi-agent system |
| [Demo](DEMO.md) | End-to-end walkthrough: Hello World Python CLI |
| [Recipes](RECIPES.md) | Step-by-step workflows for common operations |

### Operations

| Document | Purpose |
|----------|---------|
| [Configuration](../support-docs/CONFIGURATION.md) | System config, tuning parameters, environment variables |
| [Provider Models](provider-models.md) | Model selection for pi, Devin and Codex backends; pi init-gate enforcement |
| [Performance](PERFORMANCE.md) | Lock metrics, state caching, file system watching, tuning |
| [Testing](TESTING.md) | Running tests, coverage targets, test utilities |
| [Troubleshooting](../support-docs/TROUBLESHOOTING.md) | Common issues and solutions |

### Demo Benchmark

| Document | Purpose |
|----------|---------|
| [Hello Protocol](demo-benchmark/hello-protocol.md) | Session initialization behavior comparison across models |
| [Demo Comparison](demo-benchmark/demo-comparison.md) | Comparative analysis of five LLM providers on hello-cli |
| [Wrap-up](demo-benchmark/wrap-up.md) | Model capability synthesis across both benchmarks |
| [Claude trace](demo-benchmark/claude-demo-trace.md) | Claude (Opus 4.5) |
| [Codex trace](demo-benchmark/codex-demo-trace.md) | Codex (GPT-5.2) |
| [Gemini trace](demo-benchmark/gemini-demo-trace.md) | Gemini (2.5 Flash) |
| [Kimi trace](demo-benchmark/kimi-demo-trace.md) | Kimi 2.5 (Moonshot AI) |
| [Mistral trace](demo-benchmark/mistral-demo-trace.md) | Devstral-2 |

### Release Notes

| Document | Purpose |
|----------|---------|
| [v0.2.0](release_notes/RELEASE_NOTES_v0.2.0.md) | Go rewrite, tiered context, Subagent mode |
| [v0.1.1](release_notes/RELEASE_NOTES_v0.1.1.md) | Multi-LLM support, agent behavior hardening |
| [v0.1.0](release_notes/RELEASE_NOTES_v0.1.0.md) | First public release (alpha) |

### Other

| Document | Purpose |
|----------|---------|
| [Agent Testimony](agent-testimony/opus-4.6-first-session.md) | Opus 4.6 first session letter |

---

## Reading Order

**Getting started:**
1. [Getting Started](../GETTING_STARTED.md) — install and first-run setup
2. [Pairing Usage](../support-docs/USAGE_PAIRING.md) — how to pair with an agent
3. [Demo](DEMO.md) — see it in action
4. [Configuration](../support-docs/CONFIGURATION.md) — tune the system

**Running multi-agent sprints:**
1. [Multi-Agent Usage](../support-docs/USAGE_MULTI_AGENTS.md) — run the MAS after setup
2. [Recipes](RECIPES.md) — operational workflows
3. [Troubleshooting](../support-docs/TROUBLESHOOTING.md) — when things go wrong

---

## Related Documents

- [README.md](../README.md) — project overview
- [specs/](../specs/) — specifications (architecture, protocols, ADRs)
- [contracts/](../contracts/) — behavioral contracts (CORE, modes, tools)

---

## Document Status

| Category | Documents | Status |
|----------|-----------|--------|
| User Guides | 6 | Complete |
| Operations | 4 | Complete |
| Demo Benchmark | 8 | Complete |
| Release Notes | 3 | Complete |
| Other | 1 | — |
