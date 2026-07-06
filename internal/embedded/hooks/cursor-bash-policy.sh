#!/usr/bin/env bash
set -euo pipefail

root="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
input="$(cat)"

deny() {
  cat <<'JSON'
{"continue":true,"permission":"deny","user_message":"Cursor shell execution blocked by Liza bash-policy.","agent_message":"The Liza Cursor shell policy hook denied this shell command. Install or repair bash-policy, or adjust .bash-policy.yaml if this command should be allowed."}
JSON
}

if ! command -v bash-policy >/dev/null 2>&1; then
  deny
  exit 0
fi

if ! output="$(printf '%s' "$input" | bash-policy evaluate --provider codex --mode on --policy-artifact-root "$root" --safe-root "$root" --json)"; then
  deny
  exit 0
fi

case "$output" in
  *'"decision":"allow"'* | *'"decision":"no-op"'*)
    printf '{"continue":true,"permission":"allow"}\n'
    ;;
  *)
    deny
    ;;
esac
