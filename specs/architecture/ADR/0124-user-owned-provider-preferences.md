# 124 - User-Owned Provider Preferences and Explicit MAS Launch Policy

## Context and Problem Statement

Initialization had crossed two configuration ownership boundaries. The Codex
baseline injected model, reasoning effort, and personality defaults instead of
leaving those choices to the user. Claude project settings supplied and preserved
`permissions.defaultMode`, potentially shadowing the user's own preference.

The Claude change records an additional constraint: project-scope `auto` was
ignored by the provider while some other modes were honored. Consequently,
project settings could neither reliably configure unattended execution nor
consistently preserve interactive preferences. MAS agents still needed an
explicit unattended permission policy.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Respecting the user's own configuration is a default principle. Claude auto mode was chosen to avoid systematic approval gates on Bash commands.

Historical alternative evaluations were not supplied.

## Considered Options

The following comparisons are reconstructed from the baseline and implementation;
they are not claimed as historical alternatives formally considered:

1. Continue managing both personal preferences and execution policy in installed
   settings.
2. Stop writing new defaults but preserve every existing project entry.
3. Separate user preferences from required automation settings, with
   provider-specific migration and launch behavior.

## Decision Outcome

Choose **Option 3**, implemented for Codex and Claude as follows:

| Surface | Ownership and behavior |
|---------|------------------------|
| Codex model, reasoning effort, personality | Stop injecting defaults; preserve existing values because users may have changed them |
| Codex unattended baseline | Continue managed approval, workspace sandbox, network, and writable-root settings; add the `auto_review` preference |
| Claude project `defaultMode` | Remove it during settings merge and announce the removed value |
| Claude interactive permission preference | User settings own it |
| Claude MAS permission mode | Catalog launch arguments supply `--permission-mode auto`; project `agent_tools` can override launch configuration |

The migrations intentionally differ. An existing Codex preference cannot safely
be distinguished from a user customization, so removing earlier generated values
is left to the user. Leaving Claude's project `defaultMode` intact would preserve
the shadowing problem indefinitely, so initialization explicitly deletes it.

Claude launch arguments are updated in both the published catalog and embedded
fallback. Parity checks cover normal and logged launch arguments so the effective
runtime policy cannot depend on which catalog source is selected.

## Rationale

The Claude commit states the boundary directly: user settings belong to the user,
while launch arguments establish supervised execution policy. The Codex change
explicitly says to use the user default. Grouping these as one ownership decision
captures their common direction without claiming identical provider mechanics or
a universal configuration rule for every supported CLI.

## Consequences

- New Codex initialization no longer imposes a model, effort, or personality;
  upgrades can retain older values until the user removes them.
- Claude initialization changes existing project behavior deliberately. Projects
  relying on project `acceptEdits` must move interactive preferences to user
  settings or configure the required MAS launch override.
- Claude auto mode has capability constraints. The historical change documents
  an unsupported-model run refusing edits without a startup error; capability
  preflight remained a recorded detection gap, not a guarantee supplied here.
- Provider settings and catalog launch behavior need separate validation.

## Related Decisions and Provenance

Extends [ADR-0096](0096-catalog-backed-provider-registry.md). Contract placement
remains governed separately by
[ADR-0111](0111-capability-aware-global-first-contract-activation.md).
The user approved this grouping on 2026-09-06 and subsequently confirmed the ownership principle and auto-mode motivation above. Evidence comes from the two commits, settings-merge
implementation, and `support-docs/CONFIGURATION.md`; provider limitations above
describe that historical evidence rather than independently verified current
provider requirements.

---
*Reconstructed from commits 00111804 and c7e99f19 (2026-08-09).*
