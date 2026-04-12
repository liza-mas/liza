---
title: "Update master files, not derived copies"
trigger: "When modifying claude-settings.json, hooks, or any file that has a master/embedded source and derived copies"
keywords: [claude-settings.json, .claude/settings.json, embedded, liza init, permissions, hooks]
date: 2026-03-06
---

## Context

Configuration files like `claude-settings.json` exist in two places:

1. **Master**: `internal/embedded/claude-settings.json` — source of truth, compiled into the binary via `go:embed`
2. **Derived**: `.claude/settings.json` — project-active copy, created by `liza init`

Similarly: `internal/embedded/hooks/enforce-init.sh` → `.claude/hooks/enforce-init.sh`.

## Failure Mode

Editing only the derived copy (`.claude/settings.json`) fixes the immediate problem but:
- Next `liza init` overwrites the fix from the stale embedded template
- Other projects initialized with `liza init` never get the fix
- The master reference diverges from what's actually deployed

## Solution

Always update the master in `internal/embedded/`, then re-run `liza init` or manually copy to the derived location.

## References

- `internal/embedded/claude-settings.json` — master
- `internal/embedded/hooks/enforce-init.sh` — master
- `internal/embedded/hooks/git-guard.sh` — master
- `internal/embedded/embedded.go:WriteClaudeSettings()` — settings merge logic
- `internal/embedded/embedded.go:WriteHooks()` — hook deployment
