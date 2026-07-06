#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if ! command -v bash-policy >/dev/null 2>&1; then
  echo "bash-policy not found; install it or remove .cursor/hooks.json to disable the Cursor shell policy hook." >&2
  exit 1
fi

if ! output="$(bash-policy evaluate --provider codex --mode on --policy-artifact-root "$root" --safe-root "$root" --json)"; then
  echo "bash-policy evaluate failed; shell execution blocked." >&2
  exit 1
fi

case "$output" in
  *'"decision":"allow"'* | *'"decision":"no-op"'*)
    exit 0
    ;;
  *)
    echo "bash-policy blocked shell command." >&2
    exit 1
    ;;
esac
