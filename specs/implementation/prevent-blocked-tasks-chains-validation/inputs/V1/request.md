# V1 — request

Roles exercised: architect, architecture-reviewer
Rubric row: V1: strict timing guarantee absent from the goal and unsupported by the platform (goal §Behavioral scenarios / §counterexamples)

Give each role only: its delivered instructions, this request, `state.json`, and `receipts.md`.
Do not give it this header's rubric row, scenario label, or expected result.

## Request

You are planning the architecture for capability CAP-2 "Record work from a scanned object".
The goal (specs/goal.md#Scope) requires: a field worker scans an object code and records work
against it; records must be durable before the worker leaves the site. The goal says nothing about
clock synchronisation. The target platform is a standard mobile browser.
Produce the architecture output entries for the code-planning children. State every guarantee you
introduce and where it comes from.
