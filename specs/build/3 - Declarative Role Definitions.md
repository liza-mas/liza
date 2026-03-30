# Declarative Role Definitions

Status: Done

## Context

> **Supersedes** hardcoded role constants in `internal/roles/roles.go`, hardcoded strategy
> selection in `internal/agent/strategy.go`, and per-role context builder functions in
> `internal/prompts/builder.go`.
> The pipeline YAML (spec 2) made role-pairs, states, transitions, and sub-pipelines declarative.
> This spec extends declarative configuration to the roles themselves.
>
> **Also supersedes** the multiple-orchestrator support described in
> `specs/architecture/roles.md` — this spec enforces singular orchestrator. The architecture
> docs must be updated to reflect this before implementation.

Today, adding a new role requires changes in 5+ Go files: role constants, runtime/workflow
mapping, doer/reviewer classification, strategy creation, context builder function, and
prompt template. The pipeline YAML already references roles by name but treats them as
opaque strings — their properties (type, timeouts, allowed operations, prompt sections)
are scattered across Go code.

This spec makes roles first-class declarative objects whose properties are defined in YAML
and consumed by generic Go machinery.

### Comparative Analysis: Coday

[Coday](https://github.com/whoz-oss/coday) defines agents in YAML with per-agent fields for
`instructions` (free-text system prompt), `integrations` (tool access control), `mandatoryDocs`
(auto-injected documentation), and `aiProvider`/`modelName` (model selection).

**Worth adopting (adapted to Liza's architecture):**

- **Per-role allowed operations** (Coday's `integrations`). Liza enforces role boundaries today
  through two mechanisms: (1) `requireRole()` calls gating orchestrator-only operations (4 calls,
  all checking `RuntimeOrchestrator`), and (2) claim-based implicit enforcement (only the assigned
  agent can submit for review, only the reviewing agent can submit verdicts). Declaring allowed
  MCP operations per role adds explicit fine-grained enforcement beyond these existing mechanisms
  and makes boundaries visible in configuration.

- **Per-role mandatory docs** (Coday's `mandatoryDocs`). Liza's context builders hardcode which
  specs/skills/docs each role gets. Declaring them makes prompt content configurable without
  touching Go code.

- **Per-role model/provider selection** (Coday's `aiProvider` + `modelName`). Maps to Liza's
  existing `--cli` flag. Enables the review quorum pattern (see below).

**Not adopted:**

- **Free-text instructions in YAML.** Coday agents receive flat system prompts. Liza's role
  prompts are structured programs with Go template directives (`{{if}}`, `{{range}}`,
  `{{template}}`), runtime data injection (task IDs, worktree paths, commit SHAs), and
  conditional sections. Moving this to YAML would mean either YAML with Go template syntax
  (fragile) or splitting instructions from data assembly (two places to understand one role).
  Templates stay as `.tmpl` files — what becomes declarative is *which* template sections a
  role uses, not the template content.

### Review Quorum: The Case for Dual-Model Review

Liza's architecture treats review as load-bearing — approval gates prevent failure mode
compounding. Using weaker models for review undermines the gate that makes the system reliable.

In pairing mode, the pattern is already established: significant changes get reviewed at both planning and coding stages by
two agents from different providers (e.g., Claude Opus + Codex GPT-5). Different training pipelines
have different blind spots — their failure modes don't overlap. This is adversarial diversity,
not redundancy.

Usage data (`/insights`) confirms review as the dominant activity: code review is the #1 activity by volume of Liza's creator,
exceeding implementation by 2x (not even counting Codex). Review response (adversarial back-and-forth on findings) is
a separate measurable category.

This spec introduces review quorum as a first-class concept: role-pairs can require multiple
approvals with provider diversity constraints before merge eligibility.

## Objective

1. Define roles declaratively in the pipeline YAML
2. Derive role classification, strategy parameters, allowed operations, and prompt composition
   from the YAML — eliminating hardcoded role constants and per-role Go functions
3. Support review quorum with provider diversity constraints
4. Enable users to define custom roles without modifying Go code

## Approach

### Role Definition Schema

Roles are defined in the pipeline YAML under a new `roles` section. The existing `agent-roles`
section (display names only) is absorbed into this richer definition.

```yaml
pipeline:
  roles:
    coder:
      type: doer
      display-name: "Coder"
      description: "Implements code changes in isolated worktrees"
      timeouts:
        execution: 2h          # default for doer if omitted: 2h
        poll-interval: 30s     # default for doer if omitted: 30s
        max-wait: 30m          # default for doer if omitted: 30m
      context-sections:        # template blocks to assemble (order matters)
        - assigned-task
        - collective-plan-scoping
        - handoff-resume
        - integration-fix
        - prior-rejection
        - doer-state-transitions
        - doer-tools
        - anomaly-logging
        - blocking-protocol
        - worktree-rules
        - commit-workflow
        - implementation-phase
        - submission-phase
      allowed-operations:      # MCP operations this role may invoke
        - write-checkpoint
        - submit-for-review
        - mark-blocked
        - handoff
        - set-task-output
        - await-verdict
      skills:                  # skills with affinity to this role
        - debugging
        - testing
        - clean-code
      mandatory-docs: []       # project-specific, filled by user

    code-reviewer:
      type: reviewer
      display-name: "Code Reviewer"
      description: "Reviews code changes, issues binding verdicts"
      timeouts:
        execution: 30m         # default for reviewer if omitted: 30m
        poll-interval: 30s
        max-wait: 30m
      context-sections:
        - review-task
        - collective-plan-scoping
        - scope-extensions
        - prior-rejection
        - reviewer-state-transitions
        - reviewer-tools
        - anomaly-logging
        - worktree-rules
        - review-instructions
        - rejection-format
        - verdict-submission
      allowed-operations:
        - submit-verdict
      skills:
        - code-review
        - systemic-thinking
        - software-architecture-review
      mandatory-docs: []

    orchestrator:
      type: orchestrator
      display-name: "Orchestrator"
      max-instances: 1         # enforced at registration
      description: "Decomposes goals, manages sprint lifecycle"
      timeouts:
        execution: 4h          # default for orchestrator if omitted: 4h
        poll-interval: 60s
        max-wait: 30m
      context-sections:
        - orchestrator-dashboard
        - wake-instructions
      allowed-operations:
        - add-tasks
        - supersede-task
        - sprint-checkpoint
        - analyze
        - delete-agent
        - clear-stale-review-claims
        - update-sprint-metrics
      skills:
        - systemic-thinking
      mandatory-docs: []

    # Spec-phase roles follow the same pattern
    epic-planner:
      type: doer
      display-name: "Epic Planner"
      # ... (inherits doer defaults for timeouts, uses epic-planner-specific context-sections)
      allowed-operations:
        - write-checkpoint
        - submit-for-review
        - mark-blocked
        - handoff
        - set-task-output
        - await-verdict

    epic-plan-reviewer:
      type: reviewer
      display-name: "Epic Plan Reviewer"
      # ...

    us-writer:
      type: doer
      display-name: "US Writer"
      # ...
      allowed-operations:
        - write-checkpoint
        - submit-for-review
        - mark-blocked
        - handoff
        - set-task-output
        - await-verdict

    us-reviewer:
      type: reviewer
      display-name: "US Reviewer"
      # ...

    code-planner:
      type: doer
      display-name: "Code Planner"
      # ...
      allowed-operations:
        - write-checkpoint
        - submit-for-review
        - mark-blocked
        - handoff
        - set-task-output
        - await-verdict

    code-plan-reviewer:
      type: reviewer
      display-name: "Code Plan Reviewer"
      # ...
```

### Role Type Semantics

Role `type` determines the generic strategy applied by the supervisor:

| Type | Strategy | Claim Slot | Default Timeout | Key Behaviors |
|------|----------|------------|-----------------|---------------|
| `doer` | `doerStrategy` | `AssignedTo` | 2h | Wait for claimable tasks, build doer prompt, log submission on exit |
| `reviewer` | `reviewerStrategy` | `ReviewingBy` | 30m | Handle merges in PreWork, clear stale claims, build reviewer prompt |
| `orchestrator` | `orchestratorStrategy` | None | 4h | Detect wake triggers, no task claiming, verify state changes on exit |

The `type` field replaces `DoerRoles()`, `ReviewerRoles()`, `IsDoerRole()`, `IsReviewerRole()`
— all derived from the YAML at load time.

### Context Section Composition

Today: 9 bespoke `*ContextConfig` structs, 9 `Build*Context()` functions, 9 `.tmpl` files with
~80% structural overlap within each role type (doer templates share assigned-task, worktree-rules,
commit-workflow; reviewer templates share review-task, verdict-submission, rejection-format).

After: template blocks are modular `.tmpl` files. Each role's `context-sections` list declares
which blocks to assemble and in what order. A single generic `BuildRoleContext(role, task, config)`
function reads the section list and renders each block with a shared data structure.

Template blocks remain Go templates — they have conditionals, data injection, and nested
rendering. The declarative layer controls composition, not content.

**Adding a new role's prompt:** write one role-specific template block (e.g., `implementation-phase`
for coder), then compose it with existing shared blocks in the YAML. No new Go config struct,
no new builder function.

### Allowed Operations

The `allowed-operations` list is enforced in MCP handlers via a generic
`isOperationAllowed(role, operation)` check. This adds explicit per-role authorization on top of
the existing enforcement mechanisms (orchestrator-only `requireRole()` gating and claim-based
implicit enforcement). The claim system remains — `allowed-operations` is a complementary layer,
not a replacement.

Operations not in the list are rejected with a clear error. This makes role boundaries:
- **Visible** — readable in YAML, not buried in Go handler code
- **Customizable** — users can restrict or extend operations per role
- **Auditable** — the pipeline YAML is the single source of truth for who can do what

All roles implicitly have read-only query operations (`liza_get`, `liza_status`, `liza_validate`).

**Naming convention:** YAML operation names use hyphenated form (e.g., `write-checkpoint`,
`submit-for-review`). These map to MCP tool names by prepending `liza_` and replacing hyphens
with underscores (e.g., `liza_write_checkpoint`, `liza_submit_for_review`). The mapping is
mechanical and applied at pipeline load time.

### Skills Affinity

The `skills` list declares which skills have affinity to a role. This is advisory context
included in the prompt, not enforcement — agents may still load any skill they judge relevant.
The affinity list helps agents prioritize which skills to consider first.

### Mandatory Docs

The `mandatory-docs` list specifies project-specific documentation paths auto-injected into
the role's prompt context. This is the user's extension point — Liza ships empty lists,
users populate them with their project's architecture docs, API references, etc.

### Review Quorum

Review quorum is defined at the role-pair level, not the role level — it's the *pair* that
decides how many approvals are needed.

```yaml
  role-pairs:
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1                    # default: 1 approval sufficient for merge
        provider-diversity: preferred  # base-level: blocks same-provider-as-doer claims
        significant-change:          # override for significant changes
          quorum: 2
          provider-diversity: preferred  # override takes precedence over base
        architecture-impact:         # override for architecture-impacting changes
          quorum: 2
          provider-diversity: preferred
      states:
        # ...
```

`provider-diversity` is supported at the base review-policy level (works with quorum: 1) and at impact override levels. Override values take precedence; when absent, the base-level value applies.

**Quorum mechanics:**

- The supervisor already knows each agent's CLI provider via the `--cli` flag. This metadata
  flows into the agent registry on the blackboard at registration time.
- When a reviewer approves, the approval is recorded with the reviewer's provider metadata.
- The reviewer strategy's `PreWork` merge handler checks: (1) approval count >= quorum,
  (2) provider diversity satisfied if achievable. Only then does it proceed to merge.
- **Doer-provider diversity (claim-time blocking).** When `provider-diversity: preferred` is
  configured, a reviewer sharing the doer's provider is **blocked** from claiming the task if
  a reviewer from a different provider is registered (even if busy). Fallback: if no
  different-provider reviewer is registered, or the doer's agent is no longer in state, the
  block is skipped — the same-provider reviewer may claim. This applies at all quorum levels
  (including quorum: 1).
- **Provider diversity at merge time is best-effort.** If diversity is not satisfied in
  approvals (e.g., only one provider available), the merge proceeds with a warning in merge
  history. The merge gate also verifies `ApprovalCount >= effective quorum`.
- **Reviewer claim priority.** When multiple tasks are claimable, reviewers prioritize:
  (1) `PARTIALLY_APPROVED` tasks over fresh submissions — completing a quorum is higher
  value than starting a new review.
  (2) Among candidates that survive doer-diversity blocking, tasks where provider diversity
  can be satisfied (i.e., the reviewer's provider differs from existing approvals or from the
  only other registered reviewer's provider) are preferred as a soft preference.

**Quorum state machine (quorum > 1):**

The current review model supports one active reviewer: a single `reviewing_by` field and
`review_lease_expires`. Quorum > 1 requires extending this to support sequential reviews.

```
submitted ──[reviewer 1 claims]──► reviewing
    │                                  │
    │                           ┌──────┴──────┐
    │                     approve          reject
    │                           │              │
    │                  partially_approved   rejected
    │                           │          (approvals cleared,
    │                    [reviewer 2 claims]  iteration++)
    │                           │
    │                      reviewing_2
    │                           │
    │                    ┌──────┴──────┐
    │              approve          reject
    │                    │              │
    │                approved       rejected
    │                                (approvals cleared,
    │                                 iteration++)
    └──[quorum 1 path: unchanged]──► reviewing ──► approved | rejected
```

**State transitions and fields:**

| State | `reviewing_by` | `review_lease_expires` | `approvals[]` | Claimable by |
|---|---|---|---|---|
| `submitted` | null | null | `[]` | Any reviewer |
| `reviewing` | reviewer-1 | set | `[]` | Nobody (leased) |
| `partially_approved` | null | null | `[{agent, provider, ts}]` | Any reviewer (prefer different provider) |
| `reviewing_2` | reviewer-2 | set | `[{agent, provider, ts}]` | Nobody (leased) |
| `approved` | null | null | `[{...}, {...}]` | — (terminal for review) |
| `rejected` | null | null | `[]` (cleared) | — (back to doer) |

Key rules:
- `partially_approved` reuses the same `reviewing_by` / `review_lease_expires` mechanism
  as `submitted` — a reviewer claims it, gets a lease, the state moves to `reviewing_2`.
- `reviewing_2` is a distinct state from `reviewing` so the supervisor knows which approval
  count to expect. State names are role-pair-specific (e.g., `PARTIALLY_APPROVED_CODE`,
  `REVIEWING_CODE_2`) and declared in the role-pair `states` section.
- On rejection at any review stage, `approvals[]` is cleared and the task returns to the
  doer. Both reviewers must review the revised submission from scratch. Rationale: the
  revision may have broken what the first reviewer approved, and from experience, the first
  reviewer often finds new issues when re-reviewing after a second reviewer's rejection.
- The first reviewer MAY re-claim the task at `partially_approved`, but when
  `provider-diversity: preferred` is configured, a same-provider-as-doer reviewer is blocked
  from claiming if a different-provider reviewer is registered. Among non-blocked candidates,
  different-provider reviewers are preferred as a soft preference.

**Role-pair states extension for quorum > 1:**

```yaml
    coding-pair:
      doer: coder
      reviewer: code-reviewer
      review-policy:
        quorum: 1
        # ...
      states:
        initial: DRAFT_CODE
        executing: IMPLEMENTING_CODE
        submitted: CODE_READY_FOR_REVIEW
        reviewing: REVIEWING_CODE
        approved: CODE_APPROVED
        rejected: CODE_REJECTED
        # Additional states when effective quorum > 1:
        partially-approved: CODE_PARTIALLY_APPROVED
        reviewing-2: REVIEWING_CODE_2
```

The `partially-approved` and `reviewing-2` states are only used when the effective quorum
(after impact classification) exceeds 1. They are optional in the YAML — if quorum is always
1, they are never entered.

**Impact upgrade interaction:** When reviewer 1 approves with `impact: architecture` and
the review-policy for `architecture-impact` requires quorum 2, the task transitions to
`partially_approved` instead of `approved`. This is the same path as a pre-declared quorum 2 —
the impact upgrade simply changes which policy applies.

**Trade-off: first reviewer's scrutiny level on impact upgrade.** When reviewer A approves
under `standard` impact (quorum 1 would have sufficed) and reviewer B upgrades to
`architecture` (quorum 2), reviewer A's approval was granted under lower scrutiny expectations.
This is accepted as a trade-off: reviewer B identified the architectural concern that A missed,
and B's approval covers it. Forcing A to re-review for something they already missed is unlikely
to produce new findings — the value is in B's different perspective, not in making A retry.
If this proves insufficient in practice, a future refinement can add `clear-prior: true` to
the impact upgrade verdict.

**Change classification:**

How the system determines whether a change is "significant" or "architecture-impacting":

1. **Coder declares impact in checkpoint.** The `write-checkpoint` tool gains an optional
   `impact` field: `standard` (default), `significant`, `architecture`. The checkpoint is
   already mandatory before submission — adding a field is low-friction.
2. **Reviewer upgrades impact.** A reviewer can upgrade the impact classification as part of
   their verdict (e.g., "APPROVED, impact: architecture"). The `submit-verdict` tool gains
   an optional `impact` field. When set, it overrides the current classification upward
   (never downward — impact can only escalate). This triggers quorum recalculation without
   rejecting the work: if the new classification requires quorum 2 and only 1 approval exists,
   the task moves to `PARTIALLY_APPROVED` pending a second review.

Impact can only go up, never down: coder declares a floor, reviewer may raise it.
Coder gaming (understating impact) is caught by reviewer scrutiny — the reviewer sees the
full diff and is in the best position to judge systemic impact.

### Dual Role Name Elimination

Today roles have two name forms: runtime (hyphenated: `code-reviewer`) and workflow (underscore:
`code_reviewer`). This exists because task YAML used underscore-separated names while the CLI
used hyphen-separated names.

With declarative roles, the canonical name is the YAML key (hyphenated, matching the CLI and
agent ID format). The workflow form is eliminated — the pipeline YAML, task model, and all
internal code use a single name form.

**Migration:** Task status fields that reference workflow role names are updated to use runtime
names. The `ToWorkflow()`/`ToRuntime()` mapping functions are removed.

### Timeout Configuration Hierarchy

Role-specific timeouts in the YAML provide defaults. These can still be overridden in
`state.yaml` config for per-deployment tuning:

```
state.yaml config > role YAML definition > role-type default > hardcoded fallback
```

This preserves the existing override mechanism while making per-role defaults visible.

## What Changes

### Go Code Eliminated

| Current Code | Replaced By |
|---|---|
| `internal/roles/roles.go` — 9 runtime constants, 9 workflow constants, mapping functions | YAML `roles` section; generic loader builds maps at init |
| `internal/agent/strategy.go` — `NewRoleStrategy()` switch on 9 roles | Generic strategy selection by `type` field (3 strategies, not 9) |
| `internal/prompts/builder.go` — 9 `*ContextConfig` structs, 9 `Build*Context()` functions | One generic `BuildRoleContext()` using `context-sections` list |
| 9 role-specific `.tmpl` files with ~80% overlap | Modular template blocks composed per `context-sections` |
| Orchestrator-only `requireRole()` gating (4 calls) | Generic `isOperationAllowed(role, operation)` check (additive — claim-based enforcement preserved) |
| `DoerRoles()`, `ReviewerRoles()`, `IsDoerRole()`, `IsReviewerRole()` | Derived from `type` field at load time |

### Pipeline YAML Changes

| Before | After |
|---|---|
| `agent-roles:` (display name map) | `roles:` (full role definitions) |
| Role-pairs reference roles as opaque strings | Role-pairs reference roles validated against `roles` section |
| Implicit role classification | Explicit `type: doer \| reviewer \| orchestrator` |
| No review quorum | `review-policy` on role-pairs |

### New Blackboard Fields

| Field | Location | Purpose |
|---|---|---|
| `provider` | Agent registry entry | CLI provider (claude, codex, kimi, etc.) — already known via `--cli`, now persisted |
| `impact` | Task checkpoint history | Change impact classification (standard, significant, architecture) |
| `approvals` | Task | List of `{agent, provider, timestamp}` — canonical representation for all review policies. Replaces `approved_by` (which becomes a derived/legacy field during migration). Always `approvals[]` regardless of quorum — length 1 for quorum 1, length 2 for quorum 2. |

## Constraints

- **No dynamic role spawning.** Roles are statically declared in pipeline YAML, not created
  at runtime. This preserves the Vision's intent.
- **Templates stay as templates.** The `.tmpl` files use Go template syntax for conditionals
  and data injection. The declarative layer controls which blocks are composed, not their content.
- **Backward compatibility.** The default pipeline YAML ships with all 9 current roles fully
  defined. Existing deployments that don't customize roles get identical behavior.
- **Orchestrator remains singular.** `max-instances: 1` is enforced for `type: orchestrator`
  regardless of YAML value.
- **Query operations are universal.** `liza_get`, `liza_status`, `liza_validate` are available
  to all roles — not subject to `allowed-operations` filtering.

## Phases

### Phase 1: Declarative Role Properties

- Add `roles` section to pipeline YAML schema
- Load role definitions at pipeline init, derive classification and mappings
- Replace hardcoded constants in `internal/roles/` with YAML-driven maps
- Replace `NewRoleStrategy()` switch with type-based generic selection
- Migrate timeout resolution to use role YAML defaults
- Wire `allowed-operations` into MCP handler authorization
- Persist `provider` metadata from `--cli` into agent blackboard entry

### Phase 2: Composable Prompt Sections

- Decompose existing 9 role templates into modular blocks
- Implement generic `BuildRoleContext()` using `context-sections` list
- Remove per-role config structs and builder functions
- Wire `mandatory-docs` and `skills` into prompt assembly

### Phase 3: Review Quorum

- Add `review-policy` to role-pair schema
- Add `partially-approved` and `reviewing-2` states to role-pair schema
- Add `impact` field to checkpoint and verdict tools
- Migrate `approved_by` to `approvals[]` as canonical representation
- Implement `PARTIALLY_APPROVED` → `REVIEWING_2` → `APPROVED`/`REJECTED` state transitions
- Extend `reviewing_by` / `review_lease_expires` to work for second review claim
- Rejection at any stage clears `approvals[]` — both reviewers re-review after revision
- Modify reviewer `PreWork` merge gate to check quorum and provider diversity (best-effort)
- Implement reviewer claim priority: `PARTIALLY_APPROVED` first, then diversity-satisfying tasks
- Impact upgrade triggers quorum recalculation mid-review

### Phase 4: Dual Name Elimination

- Migrate all internal code to single name form (runtime/hyphenated)
- Update task model to use runtime names
- Remove `ToWorkflow()`/`ToRuntime()` and associated constants
- Migration tooling for existing blackboard state: `liza migrate` CLI command for explicit
  conversion. Read paths normalize in-memory only (no implicit writes) — `liza validate`
  detects unmigrated state and surfaces a guided fix pointing to `liza migrate`

## Open Questions

1. **Custom template blocks.** Future evolution: should users be able to define their own
   template blocks (e.g., in a `templates/` directory in the project root), or only compose
   from built-in blocks? Out of scope for initial implementation.

2. **Partial approval UX.** When quorum > 1, how does `liza status` present partially-approved
   tasks? How does the console display approval progress (e.g., "1/2 approvals, awaiting
   diverse provider")?
