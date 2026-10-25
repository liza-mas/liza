# 221 - A2 Typed Vocabulary Ownership

## Status

ACCEPTED.

## Context

The architecture-consolidation run requires finite control-flow vocabulary to
become typed constants in its owning package, and other magic values to become
named constants, without a new configuration field
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
In the platform-adapter area (A2), closed sets that decide control flow — SCIP
languages, provider backends and prompt transports, hook events, ACP methods,
preflight error codes, Git object types — are compared against bare string
literals in the adapters and their callers, and three packages carry private
copies of the same environment-gate truthiness parser. The decision is indexed
in the [ADR README](README.md).

[ADR-0220](0220-a1-typed-vocabulary.md) settled the A1 rules (defined string
types, no codec methods, conversion at boxed-string boundaries, unknown values
unchanged). This ADR applies them to A2 and records where each A2 set lives.

This ADR records the placement and compatibility constraints approved in
[Scope 5 of the A2 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-195624-arm-1-ar-11.md#scope-5-a2-typed-vocabulary-and-named-values).
It states the target contract; the coding tasks of that scope implement it.

## Decision

1. **One defined type over `string` per set, one constant per value, at the
   package that owns the vocabulary.**

   | ID | Set | Owner |
   |---|---|---|
   | W1 | SCIP language (`go`, `typescript`, `python`) | `scipsearch` |
   | W2 | Provider backend (`cli`, `acpx`) | `providers` |
   | W3 | Prompt transport (`stdin`, `arg`, `file`) | `providers` |
   | W4 | Agent-tools configure mode (`auto`, `skip`, `force`) | `toolchain` |
   | W5 | Package manager (`winget`, `scoop`, `choco`, `brew`, `apt-get`, `dnf`, `yum`, `pacman`, `zypper`) | `toolchain` |
   | W6 | Update channel (`stable`, `main`) | `updater` |
   | W7 | Hook event (`PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`) | `toolresult` |
   | W8 | Native tool name handled by rewriting (`Read`, `Grep`, `Glob`, `Bash`) | `toolresult` |
   | W9 | ACP method (`initialize`, `session/new`, `session/prompt`, `session/cancel`, `fs/read_text_file`, `terminal/create`, `terminal/output`, `terminal/wait_for_exit`, `terminal/kill`, `terminal/release`) | `toolresultacp` |
   | W10 | Preflight error code (the accepted codes and `prerequisite_failed`) | `sessionvalidation` |
   | W11 | Git object type and batch status (`blob`, `commit`, `missing`, submodule, deleted) | `git` |
   | W12 | Hook action and activation status already typed in `pairingindex`, and the other already-typed A2 sets | their owners |

   W1–W11 are new types. W12 adds no type: it replaces raw literals of
   already-typed sets in A2 production code with their existing constants. The
   listed carriers — language fields and indexer table keys, `Provider.Backend`,
   `Runtime.PromptTransport`, the configure mode field and normaliser, the
   package-manager candidate and parameter, the update-channel fields, the hook
   input event and tool fields, the ACP method values and `terminalCall`
   parameter, `Error.Code`, and the Git batch and query records — take the
   type.

2. **Protocol vocabularies live at the adapter that speaks the protocol.**
   Hook events and handled tool names (W7, W8) belong to `toolresult`, ACP
   methods (W9) to `toolresultacp`, preflight codes (W10) to
   `sessionvalidation`, Git object vocabulary (W11) to `git`. A vocabulary
   follows the code that parses or emits it, not the code that consumes the
   result.

3. **Provider launch vocabulary lives in `providers`, which A9 adopts.**
   Backend and transport (W2, W3) are owned by `providers`, the package that
   defines `Provider` and `Runtime`. The untyped backend and transport
   constants in the agent runtime are not retyped here: A9 adopts the
   `providers` types in their place when it is refactored (X7). A9's adoption
   is out of this scope.

4. **Truthiness lives in `envgate`.** The local truthy parsers of `scipsearch`,
   `semble` and `stacklit` duplicate `envgate.Truthy` exactly. Callers use
   `envgate.Truthy` and the three parsers are removed, the two exported ones
   with their outside callers updated in the same task. This absorbs a concrete
   duplication (K6) and adds no abstraction.

5. **Persisted A1 carriers keep `string`; values cross by explicit
   conversion in both directions.** `models.AgentToolConfig` backend and transport, and the
   configured SCIP languages, stay `string` because they are persisted or
   configured A1 carriers and A2 does not retype them. Values cross in two
   directions, each by explicit conversion at the A2 boundary: on entry to a
   typed A2 position (an exported A2 parameter fed from such a carrier, which
   keeps its owning W-type), the carrier's `string` is converted to the W-type;
   on output to a persisted A1 carrier, the typed value is converted back with
   `string(value)`. Package-manager generic command names and catalog map keys
   also stay strings, converted the same way at their boundary.

6. **Inherited A1 invariants (ADR-0220) apply to every W-type.**
   - AC2: no `MarshalYAML`, `UnmarshalYAML`, `MarshalText`, `UnmarshalText`,
     `MarshalJSON`, `UnmarshalJSON` or `IsZero` method on a vocabulary type, and
     no boxed typed value where a `string` assertion follows.
   - AC3: a vocabulary value reaching an `any` boundary is converted to
     `string` first.
   - Names, tags, persisted bytes and message text are unchanged.

7. **Typed names do not close open domains.** A defined type holds any value.
   Unknown input loads as today and is refused by the same validation with the
   same message, or passes through where it is tolerated today: other native
   tool names (W8), the default preflight code (W10), other ACP methods (W9).
   W9 covers the handled method vocabulary, not a validation allowlist: the
   empty-method response path, forwarding of unhandled methods, and the
   `terminal/` prefix routing into `terminalCall` with its existing
   capability check, terminal lookup, default error and wire error mapping in
   that order are unchanged.

8. **Callers and adapters.** Callers outside A2 change by a conversion or by
   naming the constant; untyped literal producers compile unchanged. Where Go
   type identity requires more — a non-A2 interface implemented by an A2 type,
   or a function-typed value receiving an A2 function — that declaration's
   affected positions take the W-type with all implementations and test
   doubles. No other non-A2 signature, field or control flow changes. Oracle
   adapters change by call-site-only edits.

9. **Named values and no shim.** Magic values in A2 packages — inline timeouts
   and deadlines, thresholds, retry counts and sentinels such as the `all` tool
   selector, the Codex `bypassPermissions` mode and shell names — become named
   constants in the package that uses them, values unchanged. Package variables
   that tests override stay variables. No alias or forwarder is introduced; the
   A2 shim register stays empty.

10. **Embedded splits stay top-level (BC2).** No package is added by the A2
    refactoring. Files split out of `internal/embedded` are new top-level
    `internal/embedded/*.go` files, never a sub-directory: the repository
    ignore rules track only top-level Go files there, and `sync-embedded`
    rewrites the asset directories, so a sub-directory would be silently
    untracked or clobbered. The link-time variables and `//go:embed` paths keep
    their package and names (BC3).

## Consequences

- One declaration per set replaces repeated literals, and distinct types stop
  one set from being mixed with another by accident. The types do not check
  membership: an untyped constant or an explicit conversion still compiles, so
  membership stays enforced by the existing runtime validation.
- Persisted bytes, messages and oracle goldens are unchanged by construction.
- Mixed typed and untyped use persists at the persisted A1 carriers and in the
  agent runtime until A9 adopts the `providers` types, by design.
- A `string`-typed value placed where a W-type is expected needs an explicit
  conversion; a typed value boxed in an `any` asserted as `string` fails at run
  time. Accepted and unknown values exercised through the affected validation
  and dispatch paths are the defense.
- Removing the three truthy parsers leaves `envgate.Truthy` as the single
  source of environment-gate truthiness.

## Alternatives Considered

1. **Central vocabulary package for all W sets:** rejected; it would add a
   package (BC2) and separate each vocabulary from the adapter that speaks it.
2. **Retype the persisted A1 carriers:** rejected; they are A1 surfaces
   outside this scope, and persisted bytes must not change.
3. **Closed allowlist for ACP methods or hook tools:** rejected; it would
   change unknown-method forwarding and pass-through behavior.
4. **Keep local truthy parsers:** rejected; they duplicate `envgate.Truthy`
   exactly and can drift.
5. **Sub-directory for split embedded files:** rejected; it would be untracked
   or clobbered by `sync-embedded`.
