# 217 - Dead-End Stages Are Commissioned With Their Delivery Task

## Status

ACCEPTED.

## Context

When a consumer needs a provider capability nobody allocated, the orchestrator
commissions a supplemental story with `add-tasks`. An added task has no
parent, and the only transition out of `us-writing-pair` (`us-to-coding`) is
many-to-one, keyed on a cohort parent. The executor can attempt that transition
for a parentless story, but has no successful generation path: when the story
merges, nothing downstream is created. Delivery therefore depends on a second
task, the architecture task depending on the story, that existed only in the
orchestrator's session behaviour. `add-tasks` committed items one by one, so
even a request carrying both could keep the story and lose the other.

In one run three supplemental stories were added without their architecture
task; four consumers waited 4.5–5 h each on providers that never existed, and
the orchestrator escalated the gap to the human as a held plan-check
expansion (D-33). Earlier planning guidance moves consumer asks before provider
freeze ([ADR-0192](0192-admission-checks-child-feasibility-known-consumers.md),
[ADR-0198](0198-frozen-interface-corrections-and-direct-coding-allocation.md),
D-31); it records that consumers written after the freeze still route gaps
late, and each late gap becomes a supplement.

## Decision

1. **Dead-end stage, from the frozen pipeline.** A role-pair is a dead end for
   added tasks when it is the source of at least one transition and every such
   transition is many-to-one. Pipeline-transitions suppressed by
   `config.no_follow_up` are ignored, so a stage the project deliberately ends
   at is terminal, not a dead end. Its *delivery targets* are the target
   role-pairs of those transitions. In the default pipeline only
   `us-writing-pair` is a dead end, with target `architecture-main-pair`.
2. **Commissioned with a delivery task.** `add-task` refuses a dead-end task.
   `add-tasks` admits one only with a *delivery companion* in the same request:
   an item of a delivery-target role-pair whose `depends` names it. Another
   dependent does not count. The refusal names the transition, the target
   role-pair and `add-tasks`.
3. **Atomic delivery groups.** A dead-end task and its companions form a group,
   committed in one state transaction: every member persists, or none does and
   each reports `delivery group [...]: <member>: <error>`. Request items whose
   in-request dependencies form a cycle through a group (an item built on a
   story that the group's delivery task needs) join it, so grouping never
   orders a member before its prerequisite. Units commit in a stable dependency
   order: the ready unit with the lowest request index first. Results stay
   positional. A request with no dead-end task keeps the independent item loop
   unchanged; in one that has one, an ordinary item naming a later item in
   `depends` now commits after it instead of failing.
4. **Scope.** Creation only: no transition, settlement or stored-task change.
   `replace-task` keeps its own consumer rewiring and is not guarded.

## Consequences

- A supplemental story cannot exist without the task that delivers it, and a
  failed companion leaves no story behind.
- The blocked-tasks wake states the rule, so the first request is complete.
- Absorbing a new story into an existing, unstarted architecture task (add the
  story, then retarget) is no longer possible; the story is commissioned with a
  new delivery task instead. No run has used the absorbed form.
- Stories already merged without a delivery task are untouched; the
  integration gate's `planning_unsettled` reason still lists them (D-225).
- `add-tasks` reads the pipeline and a state snapshot once more per request to
  plan its units.

## Alternatives Considered

1. **Generate the architecture task when a parentless story merges**
   (cohort of one). Rejected: it collides with hand-made `<story>-arm` IDs in
   live states, interacts with supersession lineage and D-225 settlement, and a
   consumer cannot depend on a task that exists only after the merge.
2. **Detection only** (flag a merged dead-end story with no delivery task).
   Shortens the wait but lets the gap occur and needs a new wake.
3. **Make every `add-tasks` request atomic.** Rejected: it changes documented
   partial-success semantics for every caller.
