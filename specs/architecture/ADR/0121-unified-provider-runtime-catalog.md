# 121 - Unified Provider Catalog with Synthesized ACP Variants

## Context and Problem Statement

[ADR-0096](0096-catalog-backed-provider-registry.md) moved provider setup and
execution metadata into a catalog. CLI and ACP variants still required separate
entries, although they represented the same provider and shared detection and
setup information. Their execution details could differ substantially: ACP
needed its own executable arguments and session management.

The intent was to make adding a new provider more declarative. Consolidating
variants keeps shared provider metadata in one declaration while retaining
independently addressable runtimes. Reducing metadata drift is a source-derived
benefit, not the user-confirmed primary trigger.

## User-Confirmed Intent

On 2026-09-06, the user supplied this historical intent: Make adding a new provider more declarative.

Historical alternative evaluations were not supplied.

## Considered Options

These are reconstructed comparisons, not a record of alternatives discussed at
the time:

1. Keep separate CLI and ACP provider entries, repeating shared metadata.
2. Declare both runtimes on one provider and synthesize addressable ACP variants.
3. Expose only one provider ID and make every caller select a runtime separately.

## Decision Outcome

Choose **Option 2**. A declared provider contains its normal `runtime` and an
optional `acp_runtime`. During catalog validation, the latter produces a virtual
`<id>-acp` provider with backend `acpx`. The virtual provider inherits the base
provider's setup and detection metadata and uses the ACP runtime unchanged.

For example:

```text
codex
  setup + detection
  runtime      -> Resolve("codex")
  acp_runtime  -> Resolve("codex-acp")
```

Validation builds the complete declared-plus-synthesized ID set before checking
aliases. An explicit ID or alias cannot silently collide with a generated ACP
identity.

The catalog exposes two deliberate views:

| Consumer | View |
|----------|------|
| Provider detection | Declared providers only, avoiding duplicate detection |
| Provider listing and resolution | Declared providers plus synthesized variants |

The same change introduces an informational `disabled` marker, exposed by
`providers list`. It communicates incomplete support; it does not prevent
resolution, detection, or setup and must not be treated as an authorization gate.

## Rationale

Shared provider identity belongs in one declaration, while runtime-specific
launch behavior remains explicit. Synthesis preserves the existing resolver
shape for callers using names such as `codex-acp`, without requiring duplicate
setup declarations. Separating listing from detection makes virtual execution
choices visible without reporting the same installed provider twice.

## Consequences

- CLI and ACP configuration can evolve together while keeping distinct launch
  arguments and session behavior.
- Catalog validation owns virtual IDs and their collision rules; consumers must
  choose the appropriate declared-only or complete view.
- Informational support status does not enforce runtime availability.
- ACP remains provider execution plumbing under
  [ADR-0085](0085-llm-agent-boundary-and-acp-observability.md), not a replacement
  for supervisor and blackboard coordination.

## Reconstruction Notes

The user approved inclusion of this record on 2026-09-06. Historical alternatives remain reconstructed; the user supplied the intent above. Evidence comes from the
commit and `internal/providers/catalog.go` validation, synthesis, and listing
boundaries.

---
*Reconstructed from commit e592a40d (2026-07-22).*
