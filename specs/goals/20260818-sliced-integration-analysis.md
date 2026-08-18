# Sliced Integration Analysis

## Objective

Improve the merge readiness of broad multi-agent runs by replacing the single
overloaded integration analysis with two levels of review:

1. bounded per-scope coverage before global analysis;
2. global analysis after all required local coverage and repairs settle,
   repeated after global fixes until the final integration state is clean or
   explicitly blocked.

The integration topology should mirror planning in reverse. Planning begins
with global coherence and fans out into narrower scopes; integration should
first validate those scopes and then fan them back into a global judgment.

## Problem

The current integration phase begins after every coding task for a goal reaches
a terminal outcome. One integration analyst receives the entire goal diff and
all completed tasks.

That model is appropriate for a cohesive change set. It becomes a cognitive
bottleneck when a run contains multiple unrelated GitHub issues or several
independent code-planning scopes. The analyst must simultaneously:

- reconstruct the intent of every planning scope;
- verify composition among the coding tasks within each scope;
- detect interactions across scopes;
- assess the aggregate branch as a merge candidate.

These are different levels of analysis. Combining them in one task increases
context pressure and makes shallow coverage more likely. A run can therefore
reach its graceful terminal state while still requiring extensive adversarial
review before it is confidently mergeable.

A scope containing only one coding-task lineage already receives local
adversarial review. Repeating that review in a slice pair adds cost without
composition to assess. The missing capability is bounded evidence for every
scope, with dedicated slice analysis only where intra-plan composition exists.

The current lifecycle has a second weakness: integration findings create fix
tasks, but the branch is not scanned again after those fixes merge. Integration
can complete without a branch-wide analyst observing the final integration
HEAD.

## Proposed Model

For a single-lineage scope, local coverage reuses a coding-review approval
attestation: the reviewed task and acceptance criteria, reviewed commit,
approver, validation, and merge commit. It does not imply that reviewer
reasoning was persisted.

The diagram shows scopes that receive a slice analysis. A scope containing only
one coding-task lineage bypasses its slice node and contributes its existing
coding-review approval attestation to the global coverage map.

```text
                          master code plan
                                  ↓
                    specialized code plans [N >= 2]
                                  ↓
          ┌───────────────────────┼───────────────────────┐
          ↓                       ↓                       ↓
   coding descendants      coding descendants      coding descendants
          ↓                       ↓                       ↓
  slice integration pair  slice integration pair  slice integration pair
          ↓                       ↓                       ↓
     slice repairs            slice repairs            slice repairs
          └───────────────────────┼───────────────────────┘
                                  ↓
                     global integration pair
                                  ↓
                             global fixes
                                  ↓
              global closure rerun, bounded until clean
```

### Slice Integration

A contributing plan scope is a pre-integration `code-planning-pair` task with
at least one coding-task lineage that produced merged work. A coding-task
lineage starts with one root coding task emitted by that plan, includes its
adversarial review, and includes any fix or replacement descendants. Two
lineages are distinct when their root coding tasks differ.

Pre-integration planning is settled only when every planning source capable of
producing coding work before integration is terminal and every eligible
coding-producing output and transition from those sources has been consumed.
Partial planning handoff may create coding work earlier, but it does not open
the integration coverage boundary.

The system evaluates the contributing plan set exactly once, after
pre-integration planning is settled and all resulting coding work is terminal.

If fewer than two contributing plan scopes exist, the workflow proceeds
directly to global integration without slice analyses. Otherwise, the system
creates bounded local coverage for every contributing scope. A scope with one
coding-task lineage reuses its existing coding-review approval attestation and
receives no slice analysis.
A scope with at least two distinct coding-task lineages that produced merged
work receives exactly one slice analysis.

Code-planning tasks created later by integration-fix escalation do not join the
contributing set and do not create slice analyses. They remain part of the
integration repair lineage and are covered by the next global analysis. This
prevents integration escalation from recursively expanding a barrier that has
already opened.

The slice analyst should receive:

- the originating code plan and architecture references;
- the descendant coding tasks and their acceptance criteria;
- the commits and affected paths attributable to those descendants;
- declared ownership, dependency, and interface metadata;
- the source state of the affected paths at the analysis snapshot.

Its responsibility is intra-plan composition: whether the coding tasks produced
from one plan work together and satisfy their shared intent. Findings should
continue through the existing integration-reviewer and coding-pair fix
lifecycle.

A slice analysis is bound to its analyzed descendant commits and source
snapshot. Slice analyses may run concurrently. Later sibling changes do not
reopen a completed slice: overlapping paths, sibling mutations, and other
cross-scope effects belong to the final global analysis.

A slice is resolved when it is clean or every approved finding has been
resolved by merged fix or replacement work. A superseded fix is resolved only
through completion of its replacement lineage. A blocked or abandoned finding
without completed replacement work blocks integration instead of leaving the
global barrier waiting indefinitely.

A clean slice is evidence about that slice only. It must not imply that the
whole goal is integrated.

### Global Integration

A global integration analysis may begin only after all coding and integration
repair work currently planned for the goal is terminal, pre-integration
planning is settled, every scope that requires a slice analysis has one, and
every created slice analysis is resolved.
If any slice is blocked, global integration must not begin.

It should receive a bounded coverage record for every contributing scope and
inspect the final goal-wide branch independently. Records may reuse
coding-review approval attestations or contain slice reports, but they are a
coverage map, not proof that the aggregate is correct.

The global analyst remains responsible for:

- interactions between contributing scopes;
- shared interfaces and cross-cutting behavior;
- aggregate test and specification agreement;
- architectural drift;
- emergent risks and omissions;
- goal-level merge readiness.

This keeps global judgment while removing the need for one agent to rediscover
all local composition concerns at the same time.

Non-trivial integration fixes promoted into full code-planning work remain
integration repairs. Their completed changes must be visible to a subsequent
global analysis; they do not create new slice analyses.

### Final Closure

If global analysis produces findings, another global analysis must run after
they are resolved by merged repair or completed replacement work. An unresolved
finding blocks integration.

Integration is complete only when a clean global analysis is bound to the
current integration HEAD.

Completion and integration-HEAD mutations must have a single linearizable
order. At completion's linearization point, integration HEAD must equal the
clean analysis's reviewed commit. A mutation ordered before completion prevents
success; a mutation ordered after completion makes that completion
non-successful at the mutation's linearization point. Correctness must not
depend on a later wake discovering stale evidence.

The integration-HEAD mutation path owns that invalidation. At the mutation's
logical linearization point, it must make any completion tied to the superseded
HEAD non-successful.

The finalization protocol must preserve ADR-0112's lock ordering and its
prohibition on blackboard state writes while the integration mutation lock is
held.

A mutation is observable whenever integration HEAD differs from the commit
reviewed by the clean analysis. That mismatch invalidates the evidence and
requires another global analysis.

The global scan/fix cycle must have a configurable generation bound with a
deterministic default. While budget remains, global fixes or any other
integration-HEAD mutation trigger another global analysis. Reaching the bound
without clean evidence for the current HEAD produces an explicit blocked or
exhausted state rather than a successful terminal state.

Slice work remains bounded by task and review iteration controls. Exhaustion or
unresolved terminal outcomes at the slice stage block integration before the
global barrier.

## Required Properties

- Partial planning handoff does not open the integration coverage boundary.
- The contributing plan set is evaluated exactly once only after every
  pre-integration planning source is terminal, every eligible coding-producing
  output and transition has been consumed, and all resulting coding work is
  terminal.
- Fewer than two contributing scopes produce no slice analyses.
- When multiple contributing scopes exist, every scope contributes a bounded
  local coverage record.
- A scope with one coding-task lineage reuses its coding-review approval
  attestation and produces no slice analysis.
- A scope with at least two distinct coding-task lineages that produced merged
  work produces exactly one slice analysis.
- Plans created by integration escalation remain integration repair work, do
  not join the contributing set, and do not create slice analyses.
- Task lineage identifies which coding and fix tasks belong to each slice.
- Each slice receives a bounded review surface attributable to its originating
  plan rather than the entire goal as its primary context.
- Each slice verdict records the descendant changes and source snapshot it
  analyzed.
- Global analysis waits until all currently planned coding and integration
  repair work is terminal, pre-integration planning is settled, every scope
  that requires a slice analysis has one, and every created slice analysis is
  resolved; missing or unresolved slice work blocks integration.
- Global analysis independently inspects the aggregate branch.
- Global fixes and later integration-HEAD mutations trigger another global scan
  while generation budget remains.
- Exhausting either slice work or global generations produces an explicit
  blocked outcome.
- Clean completion is tied to an immutable reviewed commit.
- Completion state, the clean reviewed commit, and integration HEAD have a
  linearizable relationship; concurrent mutation cannot leave successful
  completion tied to a stale HEAD.
- The integration-HEAD mutation path owns invalidation of completion tied to a
  superseded HEAD.
- Finalization preserves ADR-0112 lock ordering.
- The global generation limit is configurable and has a deterministic default.
- Wake evaluation and restart recovery cannot create duplicate slice or global
  analyses.
- The workflow remains stack-agnostic and preserves existing review and merge
  authorization boundaries.

## Success Criteria

The objective is achieved when:

1. integration coverage does not begin while a pre-integration planning source
   can still produce coding work or an eligible coding-producing output or
   transition remains unconsumed, or any resulting coding work is non-terminal;
2. the contributing set and whether each scope reuses an approval attestation
   or receives a slice analysis are reproducible at the settled pre-integration
   boundary, with no slices for fewer than two contributing scopes;
3. when multiple contributing scopes exist, every scope has a bounded coverage
   record, with a coding-review approval attestation for a one-lineage scope and
   exactly one slice analysis for a scope with at least two distinct lineages;
4. no global analysis becomes claimable while pre-integration planning is
   unsettled, planned coding or integration repair work is non-terminal, a
   required slice analysis is missing, or a slice is unresolved or blocked;
5. every slice analysis records a bounded review surface and immutable snapshot;
6. global analysis independently reviews the aggregate branch after all
   required local coverage is available and every created slice analysis is
   resolved;
7. successful integration has a linearization point at which the clean
   analysis's reviewed commit equals integration HEAD and completion state is
   successful;
8. controlled concurrency validation demonstrates that a HEAD mutation racing
   with finalization is ordered so that an earlier mutation prevents completion
   and a later mutation path invalidates it as the mutation linearizes, never
   leaving durable success for a stale HEAD;
9. later integration mutations cause re-analysis while budget remains and an
   explicit blocked outcome after exhaustion;
10. repeated wake evaluation and restart recovery create no duplicate slice or
   global generations.

## Trade-off

For a run with at least two contributing plan scopes, this topology adds `M`
slice integration pairs before the existing global pair, where `M` is the
number of scopes with at least two distinct coding-task lineages that
produced merged work. Single-lineage scopes add no slice pair. It exchanges
those additional agent cycles for bounded context, earlier defect localization,
clearer responsibility, and stronger evidence of merge readiness.

## Out of Scope

- Changing master-planning responsibilities or decomposition rules.
- Replacing coder or code-reviewer validation of integration fixes.
- Removing the global integration analysis.
- Defining stack-specific validation commands.
- Introducing new agent roles; role-pair specialization is the mechanism.

## Documentation Impact

Implementation of this objective must:

- add an ADR extending ADR-0055 with sliced analysis and final closure;
- supersede ADR-0055's accepted no-rescan limitation;
- update `specs/architecture/state-machines.md` and
  `specs/protocols/task-lifecycle.md`;
- update pipeline and operational documentation affected by new barriers,
  generations, and terminal outcomes;
- resolve or revise
  `architectural-issues.md#integration-closure-is-not-revalidated` only after
  the implementation and validation evidence exist.

## Planning Questions

- How should a slice change set be reconstructed when commits from multiple
  slices are interleaved?
- What state and generation metadata should distinguish slice, global, and
  closure analyses?
- How should the orchestrator establish that pre-integration planning is
  settled, then derive the contributing set and identify which scopes receive
  slice analyses?
- What finalization and mutation-side invalidation protocol makes completion
  linearizable with integration HEAD mutations while preserving ADR-0112 lock
  ordering?
- What configuration field and deterministic default should bound global
  scan/fix generations?
- Can existing declarative transitions express the required fan-in, or is a new
  completion barrier needed?
- What compact per-scope coverage record should represent coding-review
  approval attestations and slice reports uniformly without anchoring the
  global analyst's independent review?

## References

- `specs/architecture/ADR/0055-integration-sub-pipeline.md`
- `specs/architecture/ADR/0059-partial-planning-handoff.md`
- `specs/architecture/ADR/0067-master-planning-task-pattern.md`
- `specs/architecture/ADR/0112-serialize-integration-working-tree-mutations.md`
- `specs/architecture/architectural-issues.md#integration-closure-is-not-revalidated`
- `specs/architecture/architectural-issues.md#cross-pair-knowledge-required-by-single-pair-reviewers`
- `specs/architecture/architectural-issues.md#fan-out-amplifies-decomposition-errors-across-pipeline-stages`
- `specs/architecture/architectural-issues.md#single-goal-data-model-constrains-applicability`
- `internal/embedded/pipeline.yaml`
- `internal/models/task.go`
- `internal/models/sprint.go`
- `internal/models/config.go`
- `internal/agent/workdetection.go`
- `internal/prompts/templates/wake_coding_complete.tmpl`
