# ADR-0167: Per-Role Model Selection

## Status

ACCEPTED — 2026-09-30. Step 1 implemented; step 2 decided, not implemented.

## Context

A MAS workspace selected a CLI per role type (`config.default_doer_cli`,
`config.default_reviewer_cli` and their `DEFAULT_*_CLI` env aliases) but never a
model. The only route to a model was an `agent_profiles` entry whose
`vars.model` is rendered into a custom `agent_tools` override: the built-in
`claude` and `codex` run args carried no model, and an undefined template
variable fails rendering, so choosing a model meant redefining the tool.
Selection was per role type, not per role, and the approvals of a quorum review
could not come from chosen, different models.

## Decision

`.liza/models.yaml` holds the operator's per-role CLI and model selection. It
is read at every agent start, so edits apply to agents started afterwards
without re-initialization; `pipeline.yaml`, frozen at init, would not allow
that.

```yaml
defaults:
  doer:     {cli: claude, model: claude-opus-5-5}   # doers and orchestrators
  reviewer: {cli: codex}
roles:
  architect: {cli: claude, model: claude-fable-5-1}
```

### Step 1 — one entry per role

- **Model is a launch field.** A tool declares `model_args` (`{{model}}` is the
  model): `claude` `--model`, `codex` its top-level `-m`, both prepended to the
  argv. A model for a tool without `model_args`, or on an ACPX backend, which
  passes no argv model, fails before registration.
  `model_args` lives only in the embedded catalog: released binaries reject
  unknown fields in the published catalog, and loaded tools merge field by
  field over embedded ones.
- **One selection.** An agent start resolves CLI, model and profile together;
  the first matching rule applies:

  | Start | CLI | Model | Profile |
  |---|---|---|---|
  | `--profile` | `--cli`, else the profile's | tool default; `--model` refused | explicit |
  | `--model` | `--cli`, else the entry's, else the role default | `--model` | none |
  | `--cli` | `--cli` | tool default | default profile vars |
  | `roles.<role>`, else `defaults.<doer\|reviewer>` | entry | entry | none |
  | otherwise | default profile, then the CLI chain | tool default | default profile |

  A first-class model and a profile are never both in effect, so one launch
  carries at most one model flag and the recorded model is the one passed. A
  missing file, or a role it does not cover, keeps the previous behavior.
- **Validation.** The file is decoded strictly at every start; an unknown key,
  role or CLI, a missing `cli`, a second YAML document, or a model on a tool
  that cannot pass one stops the start. Pool repair raises `AUTO REPAIR FAILED`, once per distinct error.
- **Spawning.** Pool repair and the TUI start a covered role without `--cli`,
  so the agent applies the same entry, model included, through the same
  function. Pool repair still projects reviewer eligibility with the entry's
  CLI.
- **Init.** `init --spec` writes the file. `--default-doer-cli` and
  `--default-reviewer-cli` seed `defaults` instead of state config;
  `--default-cli` seeds each default without its own flag and still sets
  `config.default_cli` for non-role launches. Roles without a flag stay
  commented out, so role env variables keep working.
- **Provenance.** The agent record and each approval record `model`. No policy
  reads it.

### Step 2 — slot-bound reviewer lists (not implemented)

A reviewer role's entry may become a list in priority order. The next review of
a task uses slot `len(task.Approvals)` — item 1 for the first approval, item 2
for the second; slots past the end reuse the last item, so one item under
quorum 2 means no diversity. A rejection clears approvals, so re-review
restarts at item 1. A reviewer claims only when its (cli, model) matches the
task's slot, and pool repair starts per task the reviewer its slot names.
Quorum and provider diversity stay pipeline policy; the claim-time diversity
gate would count only reviewers matching the current slot, so `preferred`
yields instead of stalling. Step 1 refuses lists until then.

## Consequences

- Choosing a model for `claude` or `codex` needs no tool redefinition, per role.
- A hand-edited, start-time source joins state config and profiles;
  `default_*_cli` and default profiles stay as lower layers. Workspaces
  initialized afterwards hold role CLI defaults in the file, not state config.
- Like `pipeline.yaml`, the file has no write guard against agents; an agent
  that edits it could choose the model that reviews its work.
- A file edit between pool repair's projection and the agent's start can make
  them differ; the agent's own selection is what it registers. Process listings
  show no CLI for agents started from the file.
- An `agent_tools` override that pins a model in its run args, combined with a
  file model for the same CLI, passes two model flags; the documentation says
  not to combine them.
- Selection does not touch two open issues:
  [INVARIANTS.md §6 Provider Diversity Not Enforced at Verdict Time](../architectural-issues.md#invariantsmd-6-provider-diversity-not-enforced-at-verdict-time)
  and
  [Canonical Provider Identity Lost Before Policy Enforcement](../architectural-issues.md#canonical-provider-identity-lost-before-policy-enforcement);
  provider stays the raw CLI name, and model is a second identity diversity
  does not use.

## Alternatives Considered

- **Models in `pipeline.yaml` roles.** Rejected: frozen at init, while model
  choice changes mid-run (quota, cost).
- **Extend `agent_profiles` and state config.** Rejected: profiles still need a
  tool override to pass a model, per-role lists are awkward through
  `config set`, and state is the blackboard, not an editing surface.
- **Spawners pass `--cli` and `--model`.** Rejected: an entry without a model
  would start as a plain `--cli` launch and pick up default profile vars.
- **Model from the file whenever the resolved CLI matches the entry's.**
  Rejected: an explicit profile with its own model flag would launch with two.
- **Fallback list** (next item when the first is unavailable). Not chosen: the
  list assigns quorum slots; quota fallback is a separate concern.
- **Error when a list is shorter than quorum.** Rejected: quorum does not
  require diversity; that policy belongs to the pipeline.
