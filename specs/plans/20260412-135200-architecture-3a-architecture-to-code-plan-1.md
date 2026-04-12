# Code Plan: Remove MCP from Build, Release, and Install Tooling

**Task:** architecture-3a-architecture-to-code-plan-1
**Date:** 2026-04-12
**Architecture ref:** specs/arch-plan/20260412-112517-architecture-3a.md (Task 1)
**Spec ref:** specs/goals/20260412-cli-native-access-control.md (Section 3)

## Context

The MCP server binary (`liza-mcp`) is being removed as part of the CLI-native access
control migration. This task removes all `liza-mcp` references from the build system:
Makefile, GoReleaser config, install script, and gitignore. No Go source changes.

This task can execute in parallel with Task 0 (integration point removal) since no
files overlap. Task 2 (package deletion) depends on both.

## Task CP1: Remove liza-mcp from build, release, install, and gitignore

**Single intent:** Remove all liza-mcp binary references from build/release/install tooling.

### Makefile changes

All changes are in `/home/tangi/Workspace/liza/.worktrees/architecture-3a-architecture-to-code-plan-1/Makefile`.

1. **Remove `MCP_BINARY_NAME` variable** (line 5):
   Delete `MCP_BINARY_NAME=liza-mcp`.

2. **Remove MCP from `build:` target** (lines 31-32):
   Delete the two lines that echo and build `$(MCP_BINARY_NAME)`.

3. **Update stale `mcp.json` comment in `test:` target** (line 37):
   Change `# claude-settings.json, mcp.json, and hooks/ are mastered directly in internal/embedded/.`
   to `# claude-settings.json and hooks/ are mastered directly in internal/embedded/.`
   Rationale: `internal/embedded/mcp.json` is deleted by sibling Task 0; comment becomes stale.

4. **Remove MCP from `clean:` target** (lines 53-54):
   Delete `rm -f $(MCP_BINARY_NAME)` and `rm -f $(MCP_BINARY_NAME)-*`.

5. **Remove MCP from `install:` target** (line 67, lines 68-70):
   - Delete line 67: `$(SUDO) install -m 755 $(MCP_BINARY_NAME) $(INSTALL_DIR)/$(MCP_BINARY_NAME)`
   - Update warning message on line 69 to remove `/usr/local/bin/liza-mcp`:
     Change `'sudo rm /usr/local/bin/liza /usr/local/bin/liza-mcp'`
     to `'sudo rm /usr/local/bin/liza'`.

6. **Remove MCP from `build-all:` target** (lines 113-116):
   Delete the four `$(MCP_BINARY_NAME)` cross-compile lines (linux-amd64, darwin-amd64,
   darwin-arm64, windows-amd64).

7. **Remove MCP from `release:` target** (lines 128-133):
   Delete the comment `@# Build liza-mcp for all platforms` and the five
   `$(MCP_BINARY_NAME)` platform build lines.

8. **Remove MCP from `package:` target** (lines 148-152):
   Remove `$(MCP_BINARY_NAME)-<platform>` from each `tar -czf` and `zip` command.
   Each archive should contain only `$(BINARY_NAME)-<platform>`.

9. **Update `help:` target** (lines 159, 164, 170):
   - Line 159: `"Build liza and liza-mcp binaries"` -> `"Build liza binary"`
   - Line 164: `"Install liza and liza-mcp binaries"` -> `"Install liza binary"`
   - Line 170: `"Build both binaries for multiple platforms"` -> `"Build liza binary for multiple platforms"`

### .goreleaser.yaml changes

All changes are in `/home/tangi/Workspace/liza/.worktrees/architecture-3a-architecture-to-code-plan-1/.goreleaser.yaml`.

1. **Remove `liza-mcp` build entry** (lines 36-55):
   Delete the entire second build block:
   ```yaml
     - id: liza-mcp
       main: ./cmd/liza-mcp
       ...
   ```

2. **Remove commented-out Homebrew liza-mcp reference** (line 145):
   Delete `#       bin.install "liza-mcp"` from the commented-out Homebrew section.

### install.sh changes

All changes are in `/home/tangi/Workspace/liza/.worktrees/architecture-3a-architecture-to-code-plan-1/install.sh`.

1. **Remove `liza-mcp` from `cleanup_old_binaries()`** (line 75):
   Change `for old_bin in liza liza-mcp; do` to `for old_bin in liza; do`.

2. **Remove `liza-mcp` chmod** (line 123):
   Delete `[ -f "${tmp_dir}/liza-mcp" ] && chmod +x "${tmp_dir}/liza-mcp"`.

3. **Remove `liza-mcp` install (non-sudo path)** (line 143):
   Delete `[ -f "${tmp_dir}/liza-mcp" ] && mv "${tmp_dir}/liza-mcp" "${INSTALL_DIR}/liza-mcp"`.

4. **Remove `liza-mcp` install (sudo path)** (line 147):
   Delete `[ -f "${tmp_dir}/liza-mcp" ] && sudo mv "${tmp_dir}/liza-mcp" "${INSTALL_DIR}/liza-mcp"`.

### .gitignore changes

All changes are in `/home/tangi/Workspace/liza/.worktrees/architecture-3a-architecture-to-code-plan-1/.gitignore`.

1. **Remove `mcp.json` exception** (line 39):
   Delete `!internal/embedded/mcp.json`.
   Rationale: `internal/embedded/mcp.json` is deleted by sibling Task 0; exception becomes stale.

2. **Remove `/liza-mcp` entry** (line 42):
   Delete `/liza-mcp`.

### Verification

After all changes:
- `make build` succeeds and produces only the `liza` binary (no `liza-mcp`)
- `grep -r 'liza-mcp\|MCP_BINARY_NAME' Makefile` returns empty
- `grep 'liza-mcp' .goreleaser.yaml` returns empty
- `grep 'liza-mcp' install.sh` returns empty
- `grep -E '/liza-mcp|mcp\.json' .gitignore` returns empty

### TDD justification

No new tests required: this is a build-system-only change with no Go source modifications.
Verification is via `make build` success and grep assertions on the modified files.

## Spec Compliance Matrix

| # | Requirement | Source | Task(s) | Status |
|---|-------------|--------|---------|--------|
| 1 | Remove `MCP_BINARY_NAME` variable from Makefile | Arch plan Task 1 / done_when | CP1 | Covered |
| 2 | Remove liza-mcp build/install/release/cross-compile targets from Makefile | Arch plan Task 1 / done_when | CP1 | Covered |
| 3 | Remove liza-mcp build entry from .goreleaser.yaml | Arch plan Task 1 / done_when | CP1 | Covered |
| 4 | Remove liza-mcp references from install.sh | Arch plan Task 1 / done_when | CP1 | Covered |
| 5 | Remove /liza-mcp and mcp.json entries from .gitignore | Arch plan Task 1 / done_when | CP1 | Covered |
| 6 | `make build` succeeds and produces only `liza` binary | Arch plan Task 1 / done_when | CP1 | Covered |
| E2E | e2e test coverage for new behavior | Cross-cutting | N/A: build tooling removal, no runtime behavior change | N/A |
| DOC | Documentation updates for changed behavior | Cross-cutting | N/A: install.sh is self-documenting; user-facing docs are Phase 4 scope | N/A |
