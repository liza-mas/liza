---
title: "RTK passthrough for stdin-driven tools"
trigger: "When piping or redirecting stdin through an RTK-wrapped tool"
keywords: [rtk, rtk proxy, stdin, gocover-cobertura, empty coverage]
date: 2026-08-20
---

## Context

Some validation tools consume their primary input from stdin, including
`gocover-cobertura` when converting a Go coverage profile.

## Failure Mode

The optimized `rtk <command>` path can omit redirected stdin for these tools.
The command may still exit successfully while producing an empty report, such
as Cobertura XML with zero valid lines.

## Solution

When the result proves stdin was not consumed, use the narrow passthrough form:
`rtk proxy <command> < input-file`. Keep ordinary commands on the optimized RTK
path and verify that the generated report contains the expected records.

## References

- [Project guardrails](../../GUARDRAILS.md)
