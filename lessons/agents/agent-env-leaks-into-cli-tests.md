---
title: "Agent identity env vars break cmd/liza tests"
trigger: "When a full test run fails only in cmd/liza with an RBAC denial naming your own agent ID"
keywords: [make test-race, cmd/liza, TestMutationCommandWiring, LIZA_AGENT_ID, LIZA_AGENT_GENERATION, "not allowed for role", executeRootCommand, RBAC denial]
date: 2026-09-19
---

## Context

Agent sessions run with `LIZA_AGENT_ID` and `LIZA_AGENT_GENERATION` exported.
`cmd/liza` tests drive the real root command through `executeRootCommand`, which
resolves the acting agent the same way the CLI does — including from the
environment. Tests that expect an unauthenticated or fixture-defined actor then
run as the session's agent instead.

## Failure Mode

`make test-race` fails only in `cmd/liza`, with an RBAC denial that quotes the
agent ID of the session running the tests:

```
--- FAIL: TestMutationCommandWiring/config_set_preserves_value_unless_explicitly_replaced
    mutation_wiring_test.go:26: operation "config-set-post-worktree-cmd" not allowed for role "coder" (agent coder-2)
```

The denial is real but the cause is the ambient environment, not the change
under test. Treating it as a regression sends the session into debugging code
that is fine, and the same failure follows any change because it is
environment-scoped.

## Solution

Clear the identity variables for the test process only:

```bash
env -u LIZA_AGENT_ID -u LIZA_AGENT_GENERATION make test-race
```

Confirm attribution before concluding anything: if the narrow test passes with
the variables cleared and fails with them set, the failure is the environment
leak and not the diff. Do not unset them for `liza` lifecycle commands — those
need the session identity.

## References

- `cmd/liza/mutation_wiring_test.go`
- [worktree-build-prerequisites.md](worktree-build-prerequisites.md)
