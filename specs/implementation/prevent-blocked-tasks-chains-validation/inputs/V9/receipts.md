# V9 — read-only command receipts

Supplied receipts test how the role interprets closure; they do not show that any proposed command executed.

```
$ npm ci   (before)   → exit 1: npm ERR! code EINTEGRITY
$ npm cache clean --force && npm ci   (after) → exit 0
$ git -C .worktrees/code-3-1 status --short → (clean)
```
