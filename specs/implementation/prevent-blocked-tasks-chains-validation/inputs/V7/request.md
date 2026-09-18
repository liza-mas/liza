# V7 — request

Roles exercised: orchestrator, operator
Wake trigger: BLOCKED_TASKS (runner metadata — selects the orchestrator's instruction template; stripped with the rubric row)
Rubric row: V7: lineage traced; old vs new cause; no unchanged retry; operator detects non-convergence (goal §Behavioral scenarios / §counterexamples)

Give each role only: its delivered instructions, this request, `state.json`, and `receipts.md`.
Do not give it this header's rubric row, scenario label, or expected result.

## Request

Task code-3-2 (access adapter) was BLOCKED on "token format undecided", superseded by
code-3-2b, which is now BLOCKED on "token format undecided" again before reaching the parsing
step. Decide the next action.
