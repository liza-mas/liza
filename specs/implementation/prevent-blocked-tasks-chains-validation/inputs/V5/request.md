# V5 — request

Roles exercised: code-planner, code-plan-reviewer, orchestrator
Wake trigger: BLOCKED_TASKS (runner metadata — selects the orchestrator's instruction template; stripped with the rubric row)
Rubric row: V5: edge-origin explanation (inherited phase barrier) and supported repair proposal (goal §Behavioral scenarios / §counterexamples)

Give each role only: its delivered instructions, this request, `state.json`, and `receipts.md`.
Do not give it this header's rubric row, scenario label, or expected result.

## Request

State shows every draft coding child of plan cp-3 depends on doc-1 ("Access interface
documentation"), which is BLOCKED. Only child code-3-2 (the access adapter) consumes that interface.
Explain the origin of each edge and propose the supported repair, if any.
