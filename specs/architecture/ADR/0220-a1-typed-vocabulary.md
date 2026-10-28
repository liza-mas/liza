# 220 - A1 Typed Vocabulary

## Status

ACCEPTED.

## Context

The architecture-consolidation run requires finite control-flow vocabulary to
become typed constants in its owning package, and other magic values to become
named constants, without a new configuration field or a role-name closure
([goal K7](../../goals/20261023-architecture-consolidation.md#structural-constraints)).
In the state core (A1), the closed sets that decide control flow — claim types,
role types, transition cardinalities, history events, anomaly types, rejection
causes and the like — are bare strings compared against literals in many
packages. Consumer-side literals can drift from the owner's validation, and
nothing separates one set from another. The decision is indexed in the
[ADR README](README.md).

[ADR-0045](0045-declarative-role-definitions.md) made role definitions
declarative: role *names* are user-configurable pipeline YAML and must stay
dynamic. Only the finite `type` attribute (doer, reviewer, orchestrator) is a
closed set.

This ADR records the placement and compatibility constraints approved in
[Scope 2 of the A1 architecture](../../arch-plan/20261023-architecture-consolidation/20261023-192211-arm-1-ar-10.md#scope-2-a1-typed-vocabulary-and-named-values).
It states the target contract; the coding tasks of that scope implement it.

## Decision

1. **One defined type over `string` per set, one constant per value, in the
   owning package.**

   | ID | Set | Owner |
   |---|---|---|
   | V1 | Claim type (`doer`, `reviewer`, `both`) | `roles` |
   | V2 | Role type (`doer`, `reviewer`, `orchestrator`) | `roles` |
   | V3 | Transition cardinality (`per-subtask`, `one-to-one`, `many-to-one`) | `models` |
   | V4 | Transition trigger (`manual`, `auto`) | `models` |
   | V5 | Allocation route (empty, `scope-decomposition`, `coding-allocation`) | `models` |
   | V6 | History event name (the `TaskEvent…` set) | `models` |
   | V7 | Anomaly type (the set `Anomaly.IsValidType` accepts) | `models` |
   | V8 | Rejection cause, recovery path, restore mode | `models` |
   | V9 | Inherit mode (`all`, `selected`, `none`) | `models` |
   | V10 | Discovery severity, discovery urgency | `models` |
   | V11 | Review impact level (`standard`, `significant`, `architecture`) | `pipeline` |
   | V12 | Provider-diversity mode (empty, `preferred`) | `pipeline` |

   The persisted or configured carriers of each set (pipeline role,
   transition and review-policy fields, `TaskHistoryEntry.Event`,
   `Anomaly.Type`, rejection-RCA and disposition fields, `InheritInputs.Mode`,
   discovery severity and urgency, and the `RoleType` and
   `ProviderDiversity` results of the resolvers) take the type. V1 and V11
   have no persisted carrier in A1; they type call and verdict-validation
   parameters. Already-typed sets (task status, task type, state category,
   agent and sprint status and the like) keep their types, and their raw
   literals inside A1 production code are replaced by the constants.

2. **Placement follows the import direction.** `roles` is imported by both
   `models` and `pipeline` and owns role vocabulary, so V1 and V2 live there
   and `models.PipelineResolver` can name V2. Transition vocabulary (V3–V5) is
   consumed by `models` (provider dependencies and reservations) and loaded by
   `pipeline`; since `pipeline` imports `models`, `models` is the lowest common
   owner, and it also owns the persisted sets V6–V10. V11 and V12 are consumed
   only by `pipeline` and its importers. `models` keeps importing no
   application, runtime, presentation or prompts package, and no package is
   added.

3. **AC2 — no codec methods on a vocabulary type.** No `MarshalYAML`,
   `UnmarshalYAML`, `MarshalText`, `UnmarshalText`, `MarshalJSON`,
   `UnmarshalJSON` or `IsZero` method is defined on a vocabulary type. The
   blackboard writer (`internal/db/yaml_write.go`) projects string-kinded
   values by kind and treats method-bearing types as opaque, so persisted bytes
   stay identical only without such methods. Stored fingerprints keep hashing
   the same bytes.

4. **AC3 — convert at a boxed-string boundary.** A vocabulary value never
   reaches an `any`-typed container, parameter or template datum where it is
   later asserted or switched as `string`; it is converted to `string` at that
   boundary, because a boxed defined type fails a `.(string)` assertion.

5. **Unknown values load as today (AC1).** A defined type over `string` holds
   any value, so YAML with an unknown value loads unchanged and is refused by
   the same validation with the same message and emission order. Tolerated
   open values (unknown rejection causes, legacy event names) stay tolerated.
   YAML tags, JSON keys, field names and persisted values are unchanged.

6. **ADR-0045 boundary: role types are typed, role names are not.** V2 types
   the finite role `type`. Role *names* stay untyped and dynamic, and the
   `roles` constants of built-in role names keep their current form; no
   configuration field is added. This refines ADR-0045 without contradicting
   it.

7. **Callers and adapters.** Callers outside A1 change by a conversion or by
   naming the constant, plus the conformance updates Go type identity forces: a
   non-A1 interface whose method an A1 type implements, or a function-typed
   receiver of an A1 function value, takes the V-type in the affected
   positions with all its implementations and test doubles, changing no
   consumer control flow and no value. Other non-A1 APIs are not retyped; the
   later areas adopt the constants when refactored. Every changed exported
   signature is a V-type substitution justified by the consumer-side literal
   variation it removes. Oracle adapters change by call-site-only edits with no
   golden, fixture or provenance change. `db`'s exported blackboard API keeps
   its signatures.

8. **Named values and no shim.** Magic values in A1 packages (timeouts,
   debounce intervals, thresholds, retry counts, attempt bounds) become named
   constants in the package that uses them, values unchanged.
   `models.DefaultLeaseDurationSeconds` keeps its name and value. No alias or
   forwarder is introduced; the A1 shim register stays empty.

## Consequences

- Distinct vocabulary types stop one set from being mixed with another by
  accident, and call sites name constants instead of repeating literals. The
  types do not check membership: an untyped string constant or an explicit
  conversion still compiles, so set membership stays enforced by the existing
  runtime validation. Each set has one declaration that focused tests compare
  with the values its validation accepts.
- Persisted bytes, messages and goldens are unchanged by construction: the
  types add no codec behavior and unknown values still load.
- A `string`-typed value placed where a vocabulary type is expected needs a
  conversion, and a typed value placed in an `any` asserted as `string` fails
  at run time. AC3 and focused tests for each type's constants against the
  values its validation accepts are the defense.
- Non-A1 areas keep their literals until their own refactor, so mixed typed and
  untyped use persists across area boundaries by design.

## Alternatives Considered

1. **Typed role names too:** rejected by ADR-0045 and K7; role names are
   user-defined pipeline data.
2. **Codec methods that validate on load:** rejected; they would change
   persisted projection (AC2) and the refusal text and order (AC1).
3. **Aliases for a gradual cut-over:** rejected; callers are updated in the same
   task and the shim register must be empty at the area boundary.
