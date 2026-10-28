# Golden storage names

A golden has a logical name, the `name` passed to `Check` and `CheckState`, and a
storage name, the file under `testdata/golden/<kind>/`. `goldenPath` maps one to
the other for write and for live read, so no caller maps names itself. Authority:
D53 (Windows checkouts reject `<` and `>`).

## Mapping

Each character below is replaced, in every path component, by `%` and two
uppercase hex digits of its byte. Every other byte is kept, so safe names map to
themselves.

| Logical | Stored | | Logical | Stored |
|---|---|---|---|---|
| `%` | `%25` | | `"` | `%22` |
| `<` | `%3C` | | `|` | `%7C` |
| `>` | `%3E` | | `?` | `%3F` |
| `:` | `%3A` | | `*` | `%2A` |
| byte below 0x20 | `%00`–`%1F` | | | |

`store/<ID>.txt` is stored as `store/%3CID%3E.txt`. Escaping `%` itself keeps the
mapping injective: the logical `%3CID%3E.txt` is stored as `%253CID%253E.txt` and
never shares a file with `<ID>.txt`.

After mapping, `goldenPath` refuses a name `WindowsPathProblem` still rejects: a
reserved device name (`CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`,
with or without extension) or a component ending in a dot or space. The guard
`internal/testguard` `TestTrackedPathsAreWindowsSafe` applies the same predicate to
every tracked path.

## What is logical and what is stored

- Golden **file paths** are storage paths: on disk, in regeneration output and in
  `PROVENANCE` `changed` records.
- Golden **contents** keep logical names. An A2 effect-inventory golden still
  lists `store/<ID>.txt`: `fileEffects` builds the inventory from logical keys
  before `Check` maps the file name (`internal/oracle/a2/cli/helpers_test.go`).

Tracked goldens containing `%` before D53: none, so existing names are unchanged
except the four A2 `<ID>` and `<EVENT-1>` files.
