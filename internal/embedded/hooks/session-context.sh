#!/bin/bash
# SessionStart hook: provide proactive __BRAND_NAME_TITLE__ startup context.

input=$(cat)

json_val() {
  local key="$1" rest value ch escape=0
  local hex

  rest="${input#*\"$key\"}"
  if [[ "$rest" == "$input" ]]; then
    return 0
  fi

  rest="${rest#*:}"
  rest="${rest#"${rest%%[![:space:]]*}"}"
  if [[ "${rest:0:1}" != '"' ]]; then
    return 0
  fi
  rest="${rest:1}"

  value=""
  while [[ -n "$rest" ]]; do
    ch="${rest:0:1}"
    rest="${rest:1}"
    if (( escape )); then
      if [[ "$ch" == "u" && ${#rest} -ge 4 ]]; then
        hex="${rest:0:4}"
        rest="${rest:4}"
        case "$hex" in
          0022) value+='"' ;;
          0026) value+='&' ;;
          003c) value+='<' ;;
          003e) value+='>' ;;
          005c) value+='\\' ;;
          *) value+="u$hex" ;;
        esac
        escape=0
        continue
      fi
      value+="$ch"
      escape=0
      continue
    fi

    case "$ch" in
      \\) escape=1 ;;
      \") printf '%s' "$value"; return 0 ;;
      *) value+="$ch" ;;
    esac
  done
}

is_windows_shell() {
  case "${OSTYPE:-}" in
    msys*|cygwin*|win32*) return 0 ;;
    *) return 1 ;;
  esac
}

quote_for_shell() {
  local value="$1"
  printf "'%s'" "$(printf '%s' "$value" | sed "s/'/'\\\\''/g")"
}

json_escape() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  value=${value//$'\n'/\\n}
  value=${value//$'\r'/\\r}
  value=${value//$'\t'/\\t}
  printf '%s' "$value"
}

bounded_output() {
  local value="$1"
  printf '%s' "$value" | head -c 3000
}

repo_brand_index_hook_path() {
  local hook_path

  hook_path=$(git -C "$project_dir" rev-parse --git-path hooks/post-commit 2>/dev/null || true)
  if [[ -n "$hook_path" ]]; then
    # git returns this path relative to the repository, unless core.hooksPath is
    # absolute — then it hands back whatever form that setting holds, which on
    # Windows is C:\... and starts with no slash. Prefixing project_dir to that
    # yields a path that exists nowhere, and every index section below is then
    # silently skipped.
    case "$hook_path" in
      /*|[A-Za-z]:*) printf '%s' "$hook_path" ;;
      *) printf '%s/%s' "$project_dir" "$hook_path" ;;
    esac
    return 0
  fi

  printf '%s/.git/hooks/post-commit' "$project_dir"
}

truthy_env() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')
  [[ "$value" == "1" || "$value" == "true" ]]
}

branded_env_gate() {
  local branded_name="$1"
  local legacy_name="$2"

  if [[ "${!branded_name+x}" == "x" ]]; then
    printf '%s' "${!branded_name}"
    return 0
  fi
  printf '%s' "${!legacy_name:-}"
}

semble_offline_ready() {
  local tmpdir status

  command -v semble >/dev/null 2>&1 || return 1
  command -v timeout >/dev/null 2>&1 || return 1

  tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/__BRAND_BINARY_NAME__-semble.XXXXXX") || return 1
  if ! printf 'def brand_semble_prewarm(): pass\n' >"$tmpdir/prewarm.py"; then
    rm -rf "$tmpdir"
    return 1
  fi

  HF_HUB_OFFLINE=1 timeout 30s semble search "__brand_semble_prewarm__" "$tmpdir" --top-k 1 --content code >/dev/null 2>&1
  status=$?
  rm -rf "$tmpdir"
  return "$status"
}

root_sembleignore_safe() {
  local ignore_file="$project_dir/.sembleignore"
  local required_patterns=(
    "__BRAND_PROJECT_DIRNAME__/"
    ".worktrees/"
    "stacklit.json"
    "*.scip"
    ".env"
    ".env.*"
    "*.env"
    "credentials.*"
    "secrets.*"
    "*secret*.*"
    "*.pem"
    "*.key"
    "*.p12"
    "*.pfx"
    "*.jks"
    "*_rsa"
    "*_dsa"
    "*_ecdsa"
    "*_ed25519"
    "*.keystore"
    "*.truststore"
    "config/secrets/"
    "**/secrets/"
    "serviceAccountKey.json"
    "*-credentials.json"
  )
  local pattern

  [[ -f "$ignore_file" ]] || return 1
  for pattern in "${required_patterns[@]}"; do
    grep -Fxq "$pattern" "$ignore_file" 2>/dev/null || return 1
  done
}

cwd=$(json_val cwd)

# Canonicalise Windows path separators, as enforce-init.sh does.
#
# git rev-parse --show-toplevel reports a forward-slashed path even on Windows,
# while the cwd the agent sends is backslashed. Without this, whichever of the
# two answers wins decides the separator, and the fallback branch below emits
# paths like C:\Users\proj/stacklit.json — half native, half POSIX. On Windows
# the backslash is a reserved character that can never appear in a filename, so
# the rewrite is lossless; on Unix it is a legal filename character, so it must
# not be touched there.
if is_windows_shell; then
  cwd="${cwd//\\//}"
fi

project_dir="${CLAUDE_PROJECT_DIR:-}"
if [[ -z "$project_dir" ]]; then
  if [[ -n "$cwd" ]]; then
    project_dir=$(git -C "$cwd" rev-parse --show-toplevel 2>/dev/null || printf '%s' "$cwd")
  else
    project_dir=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
  fi
fi

# CLAUDE_PROJECT_DIR is set by the runtime and holds a native path, so it skips
# the canonicalisation applied to cwd above. Every path this hook prints is
# derived from project_dir, which would then be emitted half native and half
# POSIX depending only on which source supplied it.
if is_windows_shell; then
  project_dir="${project_dir//\\//}"
fi

brand_agent_id_var="__BRAND_ENV_PREFIX__""_AGENT_ID"
agent_id_value="${!brand_agent_id_var:-${LIZA_AGENT_ID:-}}"

context="MANDATORY: Read CORE.md and the documents listed as required. DO NOT fake reads. DO read them FULLY, one tool call at a time in the required order. DO NOT batch or parallelize reads. Complete the initialization sequence before doing ANYTHING else. User prompt is not a replacement and should be considered only after the init sequence is complete. "
if [[ -n "$agent_id_value" ]]; then
  context+="~/__BRAND_GLOBAL_DIRNAME__/MULTI_AGENT_MODE.md"
else
  context+="~/__BRAND_GLOBAL_DIRNAME__/PAIRING_MODE.md"
fi
context+=", ~/__BRAND_GLOBAL_DIRNAME__/AGENT_TOOLS.md"

if [[ -f "$project_dir/GUARDRAILS.md" ]]; then
  context+=", $project_dir/GUARDRAILS.md"
fi

if [[ -z "$agent_id_value" ]]; then
  pairing_docs=()
  [[ -f "$project_dir/REPOSITORY.md" ]] && pairing_docs+=("$project_dir/REPOSITORY.md")
  [[ -f "$project_dir/docs/USAGE.md" ]] && pairing_docs+=("$project_dir/docs/USAGE.md")
  pairing_docs+=("~/__BRAND_GLOBAL_DIRNAME__/COLLABORATION_CONTINUITY.md")
  for doc_path in "${pairing_docs[@]}"; do
    context+=", $doc_path"
  done
fi
context+=". Only after those reads, answer the user."

hook_path=$(repo_brand_index_hook_path)

if [[ -n "$agent_id_value" ]]; then
  printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"%s"}}\n' "$(json_escape "$context")"
  exit 0
fi

scip_files=()
if [[ -f "$hook_path" ]] && grep -q '__BRAND_BINARY_NAME__-index' "$hook_path" 2>/dev/null; then
  stacklit_path="$project_dir/stacklit.json"
  if [[ -f "$stacklit_path" ]]; then
    shell_stacklit_path=$(quote_for_shell "$stacklit_path")
  fi

  for scip_path in "$project_dir"/*.scip; do
    [[ -f "$scip_path" ]] || continue
    scip_files+=("$scip_path")
  done
fi

semble_enabled=false
brand_semble_gate_var="__BRAND_ENV_PREFIX__""_ENABLE_SEMBLE"
semble_gate=$(branded_env_gate "$brand_semble_gate_var" "LIZA_ENABLE_SEMBLE")
if truthy_env "$semble_gate" && root_sembleignore_safe && semble_offline_ready; then
  semble_enabled=true
  shell_project_dir=$(quote_for_shell "$project_dir")
fi

functional_clusters_enabled=false
functional_clusters_path="$project_dir/functional-clusters.json"
brand_functional_clusters_gate_var="__BRAND_ENV_PREFIX__""_ENABLE_FUNCTIONAL_CLUSTERS"
functional_clusters_gate=$(branded_env_gate "$brand_functional_clusters_gate_var" "LIZA_ENABLE_FUNCTIONAL_CLUSTERS")
if truthy_env "$functional_clusters_gate" && [[ -f "$functional_clusters_path" ]]; then
  functional_clusters_enabled=true
  shell_functional_clusters_path=$(quote_for_shell "$functional_clusters_path")
fi

adr_dir_available=false
if [[ -d "$project_dir/specs/architecture/ADR" ]]; then
  adr_dir_available=true
fi

if [[ -z "${shell_stacklit_path:-}" && "${#scip_files[@]}" -eq 0 && "$semble_enabled" != "true" && "$functional_clusters_enabled" != "true" && "$adr_dir_available" != "true" ]]; then
  printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"%s"}}\n' "$(json_escape "$context")"
  exit 0
fi

if [[ "$adr_dir_available" == "true" ]]; then
  context+="
 // ADRs: specs/architecture/ADR"
fi

if [[ -n "${shell_stacklit_path:-}" || "${#scip_files[@]}" -gt 0 ]]; then
  context+="
 __BRAND_NAME_TITLE__ repository indexes detected. Pairing mode can use these explicit repo-root index paths. They are refreshed after commits and do not reflect uncommitted changes; verify against source files before editing."
fi

if [[ -n "${shell_stacklit_path:-}" ]]; then
  context+="
 // Stacklit index: $stacklit_path
 // stacklit derive --ai-summary -i $shell_stacklit_path
 // stacklit find-module <query> -i $shell_stacklit_path
 // stacklit get-module <module> -i $shell_stacklit_path
 // stacklit get-dependencies <module> -i $shell_stacklit_path
 // stacklit get-hints -i $shell_stacklit_path
 // stacklit get-hot-files -i $shell_stacklit_path"
fi

if [[ "${#scip_files[@]}" -gt 0 ]]; then
  context+="
 // SCIP indexes:"
  for scip_path in "${scip_files[@]}"; do
    language=$(basename "$scip_path" .scip)
    case "$language" in
      go) display_language="Go" ;;
      typescript) display_language="TypeScript" ;;
      python) display_language="Python" ;;
      *) display_language="$language" ;;
    esac
    context+="
 // $display_language index: $scip_path"
  done
  context+="
 // scip-search symbols --index <index-path> --name Foo --name Bar
 // scip-search packages --index <index-path> --prefix com.example
 // scip-search references --index <index-path> --name Handler --one-line
 // scip-search references --index <index-path> --symbol '<exact-foo>' --symbol '<exact-bar>' --location-only
 // scip-search implementations --index <index-path> --name Interface --one-line (implementation rows may be absent for Python)
 // scip-search impact --index <index-path> --symbol '<exact-symbol>' --one-line
 // scip-search graph --index <index-path> --symbol '<exact-symbol>' --markdown
 // Use impact first for pre-edit blast radius; graph/impact can be noisy for large functions and Python indexes."
fi

if [[ -n "${shell_stacklit_path:-}" ]] && [[ "${#scip_files[@]}" -gt 0 ]]; then
  context+="
 // Orient with Stacklit first, then trace precisely with scip-search."
fi

if [[ -n "${shell_stacklit_path:-}" ]]; then
  context+="
 === Run \`stacklit derive --ai-summary -i $shell_stacklit_path\` at the end of the session initialization."
fi

if [[ "$semble_enabled" == "true" ]]; then
  context+="
 // Semble semantic search is available for this repo root: $project_dir
 // semble search \"where is review submission validated?\" $shell_project_dir
 // semble search \"agent CLI defaults\" $shell_project_dir --top-k 10
 // semble search \"where is task superseding specified?\" $shell_project_dir --content docs
 // semble search \"default CLI config\" $shell_project_dir --content config
 // semble find-related <file_path> <line> $shell_project_dir
 // Use --content with one of: code, docs, config, all; code is the default.
 // Semble returns candidate chunks, not proof; verify source files before editing.
 // Do not use rg for broad-scope or common-word conceptual queries."
fi

if [[ "$functional_clusters_enabled" == "true" ]]; then
  context+="
 // Functional clusters artifact: $functional_clusters_path
 // functional-clusters list --clusters $shell_functional_clusters_path
 // functional-clusters explain --clusters $shell_functional_clusters_path '<exact-member-symbol>'
 // Functional clusters are advisory and may be stale; verify against source files before editing."
fi

printf '{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"%s"}}\n' "$(json_escape "$context")"
