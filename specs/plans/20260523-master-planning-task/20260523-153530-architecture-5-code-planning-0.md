# Code Plan: Master Planning Acceptance Validation

## Sources

Based on:
- Task `architecture-5-code-planning-0` from blackboard via `liza get`.
- Goal spec `specs/goals/20260523-master-planning-task.md`.

This file preserves the plan artifact referenced by
`architecture-5-code-planning-0` output entries so post-merge state validation
can resolve the declared `plan_ref`.

## Task Graph

### Task 1: Entry-Point Routing Acceptance Tests

desc: Add CLI-level acceptance tests and validation matrix coverage proving the
embedded master-planning pipeline validates, all four entry points expose one
specialized simple route and one mapped master fan-out route, and
INITIAL_PLANNING rendering remains a one-task contract.

scope: Acceptance and regression tests for embedded pipeline validation,
entry-point simple versus fan-out routing, and INITIAL_PLANNING one-task
rendering. Depends on the INITIAL_PLANNING route-data and rendering tasks.

### Task 2: Master Decompose Child-Creation Tests

desc: Add ops-layer acceptance tests proving approved master planning tasks
auto-decompose into specialized children with dependency, artifact, and typed
decomposition metadata propagation, while `architecture-to-code-plan` continues
to bypass `code-planning-main-pair`.

scope: Ops-layer tests for master task approval to specialized child creation,
metadata propagation, dependency handling, and Case A code-planning bypass.
Depends on typed decomposition child propagation.

### Task 3: Full Flow Regression Tests

desc: Add integration acceptance tests proving master planning review quorum,
prompt differentiation, and full sprint transition behavior compose with master
decomposition without regressing specialized prompt or downstream artifact
behavior.

scope: Integration tests covering quorum-2 master review behavior, master
prompt differentiation, sprint transition composition, and preservation of
specialized prompt and downstream artifact behavior. Depends on master prompt
behavior and typed decomposition validation and propagation.

## Validation

- Run the standard acceptance and integration test suites owned by each child
  task.
- Confirm `liza validate` accepts the resulting state and pipeline.
- Runtime documentation updates are out of scope for these acceptance test
  tasks.
