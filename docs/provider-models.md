# Provider Model Selection: pi, Devin (SWE-2 Max), and GLM 5.3 Flash via Codex

Liza wraps provider **CLIs**, not their APIs. Model choice therefore happens at
the CLI layer — via each CLI's own configuration, or via Liza's `agent_tools`
state overrides. This doc shows how to run Liza agents on three specific
setups and how the behavioral-contract + guardrails enforcement behaves in
each.

## 1. Pi (`pi-coding-agent`) as a Liza backend

Pi is provider-agnostic: one CLI fronts z.ai, OpenRouter, Ollama, DeepSeek and
~200 other providers (see `pi --list-models`).

Activate it for a workspace:

```bash
liza init --pi                # pairing mode: contract + init-gate setup
liza init --pi --default-cli pi "goal" --spec specs/vision.md
```

What activation does:

- Contract: pi auto-discovers `AGENTS.md`. With `prefer_global`, the global
  `~/.pi/agent/AGENTS.md → ~/.liza/CORE.md` symlink carries the behavioral
  contract into every pi session, in any workspace.
- Enforcement: `liza init --pi` / `liza setup --pi` installs the **pi
  init-gate extension** at `~/.liza/extensions/liza-init-gate.ts`. The pi
  provider's `run_args` load it with `pi -p -e <gate>`. It blocks mutating
  tools (`bash`, `edit`, `write`, unknown custom tools) until `AGENT_TOOLS.md`,
  `MULTI_AGENT_MODE.md` (or `PAIRING_MODE.md`) and the project's
  `GUARDRAILS.md` have been read with the read tool — the pi counterpart of
  `.claude/hooks/enforce-init.sh`.
- API keys: drop a `pi.env` file at the repo root (`KEY=VALUE` lines). Liza
  injects it into the pi subprocess and registers the values with the secret
  masker. Use it for provider keys pi's own auth does not manage, e.g.
  `ZAI_API_KEY=...`.
- Model selection: pi reads its own `~/.pi/agent/settings.json`
  (`defaultModel`, `enabledModels`) or accepts
  `pi --provider zai --model glm-5.3-flash`. To pin a model for Liza-spawned
  pi agents, use an `agent_tools` override (see §2) with
  `run_args: [-p, --provider, zai, --model, glm-5.3-flash, -e, ...]`.

## 2. Devin as the model source: SWE-2 Max

Cognition's SWE-2 (Max reasoning effort) is reachable through the **Devin
CLI**, not an OpenAI-compatible endpoint, so Devin is the vehicle. Liza's
`devin` provider (and `devin-acp` via acpx) is catalog-built-in.

Select the model through the documented `agent_tools` state override —
`agent_tools.<id>.run_args` **replaces** the catalog argv for that tool, so
repeat the full shape including `{{prompt}}`:

```yaml
# <workspace>/.liza/state.yaml
agent_tools:
  devin:
    run_args: [--permission-mode, dangerous, --model, swe-2-max, -p, "{{prompt}}"]
    logged_run_args: [--permission-mode, dangerous, --model, swe-2-max, -p, "{{prompt}}"]
```

`liza agent coder --cli devin` then spawns
`devin --permission-mode dangerous --model swe-2-max -p "<prompt>"`. The same
override mechanism works for any catalog CLI (model pinning for pi, codex,
etc.) and for `devin-acp`'s `acpx_prompt_args` (acpx accepts `--model <id>`).

Contract enforcement for Devin: the catalog places the contract at
`.windsurf/rules/<brand>.md`, and every spawned agent's bootstrap prompt
already instructs reading `MULTI_AGENT_MODE.md`, `AGENT_TOOLS.md` and
`GUARDRAILS.md` before any work — provider-independent.

## 3. GLM 5.3 Flash through Codex with an API key

Configure Codex per z.ai's official instructions
(https://docs.z.ai/devpack/tool/codex). Add to `~/.codex/config.toml`:

```toml
[model_providers.zai]
name = "z.ai"
base_url = "https://api.z.ai/api/v1"
wire_api = "responses"
env_key = "ZAI_API_KEY"
```

and, on codex ≥ 0.155, a profile file `~/.codex/zai-glm.config.toml`:

```toml
model_provider = "zai"
model = "glm-5.3-flash"
model_reasoning_effort = "medium"
```

Export the key (`export ZAI_API_KEY=...`) and verify:

```bash
codex --profile zai-glm exec "Reply with OK"
```

The global codex `model`/`model_provider` stay untouched, so plain
`liza agent --cli codex` keeps using the previous default; opt into GLM with
the profile or `-c model_provider=zai -c model=glm-5.3-flash`. For
Liza-spawned codex agents, a repo-root `codex.env` with `ZAI_API_KEY=...` is
injected into the codex subprocess automatically (env-file mechanism, masked
in logs).

## Enforcement in both conditions

| Condition | Contract docs | GUARDRAILS.md | Mechanism |
|---|---|---|---|
| pi backend | `~/.pi/agent/AGENTS.md → CORE.md` + base prompt | required read | pi init-gate extension (tool_call blocking) |
| codex backend | `~/.codex/AGENTS.md → CORE.md` + base prompt | required read | SessionStart + PreToolUse hooks (`enforce-init.sh`) |
| devin backend | `.windsurf/rules/<brand>.md` + base prompt | required read | bootstrap prompt + base_prompt init sequence |

In every case the spawn-time bootstrap prompt (`internal/prompts/templates/base_prompt.tmpl`)
demands the contract reads before any other work; the per-provider hooks /
extensions make that demand *mechanically enforced*.
