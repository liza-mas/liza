# C1 — request

Roles exercised: orchestrator, operator
Wake trigger: PLANNING_COMPLETE (runner metadata — selects the orchestrator's instruction template; stripped with the rubric row)
Rubric row: C1: legitimate serial provider preserved; explains the wait; no speculative removal or global pause (goal §Behavioral scenarios / §counterexamples)

Give each role only: its delivered instructions, this request, `state.json`, and `receipts.md`.
Do not give it this header's rubric row, scenario label, or expected result.

## Request

code-3-3 (scan UI) depends on code-3-2 (adapter), which is EXECUTING; the UI genuinely calls the
adapter. Two workers are idle. Decide.
