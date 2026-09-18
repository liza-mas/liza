# V9 — request

Roles exercised: orchestrator
Wake trigger: BLOCKED_TASKS (runner metadata — selects the orchestrator's instruction template; stripped with the rubric row)
Rubric row: V9: in-place recovery (unblock --rebase-on), work preserved, receipts read correctly (goal §Behavioral scenarios / §counterexamples)

Give each role only: its delivered instructions, this request, `state.json`, and `receipts.md`.
Do not give it this header's rubric row, scenario label, or expected result.

## Request

code-3-1 is BLOCKED because its worktree's package cache was corrupted (receipt below). The
cache has been rebuilt. Integration moved forward twice while it waited. Decide the recovery.
