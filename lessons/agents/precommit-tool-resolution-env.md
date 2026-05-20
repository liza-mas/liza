---
title: "Pre-commit tool resolution from local caches"
trigger: "When pre-commit fails to resolve jscpd, goimports, or staticcheck in a Codex/Liza session"
keywords: [pre-commit, jscpd, goimports, staticcheck, proxy.golang.org, NPM_CONFIG_CACHE]
date: 2026-05-20
---

## Context

The Liza pre-commit config runs `jscpd` through `NPM_CONFIG_CACHE=/tmp/.npm-cache npx`
and runs Go tools through `go run ...@latest`. In sandboxed Codex sessions,
network proxy or DNS sockets may be blocked even when the required tool payloads
already exist in local caches.

## Failure Mode

Pre-commit can fail with `sh: 1: jscpd: not found` or Go module lookup errors
such as `proxy.golang.org ... socket: operation not permitted`. Re-running the
same hook does not help because `npx` is not seeing the cached `.bin` directory
and `go run @latest` still tries network module resolution.

## Solution

First check for cached tools, without installing global packages:

```bash
rtk rg --files /tmp/.npm-cache/_npx --glob '*jscpd*'
rtk rg --files /home/tangi/.cache/go --glob '*goimports*'
rtk rg --files /home/tangi/.cache/go --glob '*staticcheck*'
```

If present, rerun pre-commit with the cached `jscpd` bin on `PATH` and Go module
resolution pointed at the local module proxy cache:

```bash
rtk env PATH=/tmp/.npm-cache/_npx/<hash>/node_modules/.bin:/usr/local/go/bin:/home/tangi/.cache/go/bin:/home/tangi/.pyenv/shims:/usr/local/bin:/usr/bin:/bin GOPATH=/home/tangi/.cache/go GOMODCACHE=/home/tangi/.cache/go/pkg/mod GOCACHE=/home/tangi/.cache/go/build GOPROXY=file:///home/tangi/.cache/go/pkg/mod/cache/download GOSUMDB=off pre-commit run --files <files>
```

Do not edit `.pre-commit-config.yaml` or install global tools for this case.
If the caches are absent, mark the task blocked with the exact failing command
and stderr.

## References

- `.pre-commit-config.yaml`
- `lessons/agents/worktree-build-prerequisites.md`
