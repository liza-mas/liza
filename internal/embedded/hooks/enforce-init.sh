#!/bin/bash
# PreToolUse hook: block non-Read tool calls until mandatory docs are read.
# Also allow a narrow set of read-only Bash commands for the broader session
# initialization docs so harmless compatibility reads do not trip the gate.
#
# Required reads:
#   - AGENT_TOOLS.md
#   - GUARDRAILS.md (or verified absent)
#   - One mode contract from the Mode Selection Gate
#   - Pairing only: existing REPOSITORY.md/docs/USAGE.md, ~/__BRAND_GLOBAL_DIRNAME__/COLLABORATION_CONTINUITY.md
#
# No external dependencies (no jq, no sed -i). Portable across Linux, macOS,
# and Windows under Git Bash.

input=$(cat)

# Extract a top-level JSON string value without jq, preserving escaped quotes.
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

# Extract string entries from a JSON array without jq. MCP filesystem
# read_multiple_files uses this shape for tool_input.paths. This deliberately
# supports only compact arrays of simple strings: unsupported JSON shapes yield
# no recognized paths, so the initialization gate remains closed.
json_array_vals() {
  local key="$1"

  echo "$input" |
    grep -o "\"$key\"[[:space:]]*:[[:space:]]*\\[[^]]*\\]" |
    sed -n -E "1{s/^\"$key\"[[:space:]]*:[[:space:]]*\\[//;s/\\]$//;s/\",[[:space:]]*\"/\\
/g;s/^\"//;s/\"$//;p;}"
}

is_windows_shell() {
  case "${OSTYPE:-}" in
    msys*|cygwin*|win32*) return 0 ;;
    *) return 1 ;;
  esac
}

tool_name=$(json_val tool_name)
session_id=$(json_val session_id)
cwd=$(json_val cwd)
command=$(json_val command)

# Canonicalise Windows path separators before any validation.
#
# On Windows the backslash is the path separator, but it is also a reserved
# character that can never appear in a filename — so rewriting it to "/" is
# lossless there. It is also what makes a native path comparable with
# $project_dir, which git reports forward-slashed even on Windows.
#
# Never do this on Unix: a backslash IS a legal filename character there, so
# normalising would make the guard validate a different path than the one the
# command actually reads, and would blunt the metacharacter rejection in
# is_safe_read_command_for_allowed_paths. The other metacharacters are
# untouched here, so ";", "&", "|", "<", ">", "`" and "$(" remain rejected.
if is_windows_shell; then
  command="${command//\\//}"
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

# Fallback: $PPID is the Claude Code process PID (POSIX — the parent that forked this shell).
# Stable for the session lifetime since all hook invocations share the same parent process.
state_root="${TMPDIR:-/tmp}"
state_root="${state_root%/}"
STATE_DIR="${state_root}/__BRAND_BINARY_NAME__-init-gate-${session_id:-ppid-$PPID}"
if ! mkdir -p "$STATE_DIR" 2>/dev/null; then
  echo "enforce-init: cannot create state dir $STATE_DIR, failing open" >&2
  exit 0
fi

# Fast path: gate already cleared.
if [[ -f "$STATE_DIR/CLEARED" ]]; then
  exit 0
fi

mark_absent_project_docs_if_needed() {
  if [[ ! -f "$STATE_DIR/GUARDRAILS.done" ]] && \
     [[ ! -f "$project_dir/GUARDRAILS.md" ]]; then
    touch "$STATE_DIR/GUARDRAILS.done"
  fi
  if [[ ! -f "$STATE_DIR/REPOSITORY.done" ]] && \
     [[ ! -f "$project_dir/REPOSITORY.md" ]]; then
    touch "$STATE_DIR/REPOSITORY.done"
  fi
  if [[ ! -f "$STATE_DIR/USAGE.done" ]] && \
     [[ ! -f "$project_dir/docs/USAGE.md" ]]; then
    touch "$STATE_DIR/USAGE.done"
  fi
}

# Initialize: auto-clear optional project docs if absent from project root.
mark_absent_project_docs_if_needed

reset_invalid_bash_read_marker() {
  rmdir "$STATE_DIR/INVALID_BASH_READ.seen" 2>/dev/null || true
}

# Read-only discovery tools: allow before gate clears.
if [[ "$tool_name" == "ToolSearch" || "$tool_name" == "Glob" ]]; then
  exit 0
fi

requires_pairing_companion_docs() {
  [[ -f "$STATE_DIR/MODE_PAIRING.done" ]]
}

clear_if_ready() {
  mark_absent_project_docs_if_needed

  if [[ ! -f "$STATE_DIR/AGENT_TOOLS.done" ]] || \
     [[ ! -f "$STATE_DIR/MODE.done" ]] || \
     [[ ! -f "$STATE_DIR/GUARDRAILS.done" ]]; then
    return 0
  fi

  if requires_pairing_companion_docs; then
    [[ -f "$STATE_DIR/REPOSITORY.done" ]] || return 0
    [[ -f "$STATE_DIR/USAGE.done" ]] || return 0
    [[ -f "$STATE_DIR/COLLABORATION_CONTINUITY.done" ]] || return 0
  fi

  touch "$STATE_DIR/CLEARED"
}

is_required_doc_path() {
  case "$1" in
    "~/__BRAND_GLOBAL_DIRNAME__/AGENT_TOOLS.md"|"$HOME"/__BRAND_GLOBAL_DIRNAME__/AGENT_TOOLS.md) return 0 ;;
    "~/__BRAND_GLOBAL_DIRNAME__/PAIRING_MODE.md"|"~/__BRAND_GLOBAL_DIRNAME__/MULTI_AGENT_MODE.md"|"~/__BRAND_GLOBAL_DIRNAME__/SUBAGENT_MODE.md") return 0 ;;
    "$HOME"/__BRAND_GLOBAL_DIRNAME__/PAIRING_MODE.md|"$HOME"/__BRAND_GLOBAL_DIRNAME__/MULTI_AGENT_MODE.md|"$HOME"/__BRAND_GLOBAL_DIRNAME__/SUBAGENT_MODE.md) return 0 ;;
    "$project_dir"/GUARDRAILS.md|GUARDRAILS.md|./GUARDRAILS.md) return 0 ;;
    *) return 1 ;;
  esac
}

is_session_init_doc_path() {
  case "$1" in
    "~/__BRAND_GLOBAL_DIRNAME__/CORE.md"|"$HOME"/__BRAND_GLOBAL_DIRNAME__/CORE.md) return 0 ;;
    REPOSITORY.md|./REPOSITORY.md|"$project_dir"/REPOSITORY.md) return 0 ;;
    docs/USAGE.md|./docs/USAGE.md|"$project_dir"/docs/USAGE.md) return 0 ;;
    "~/__BRAND_GLOBAL_DIRNAME__/COLLABORATION_CONTINUITY.md"|"$HOME"/__BRAND_GLOBAL_DIRNAME__/COLLABORATION_CONTINUITY.md) return 0 ;;
    *) is_required_doc_path "$1" ;;
  esac
}

mark_session_init_doc_path() {
  local file_path="$1" base

  is_session_init_doc_path "$file_path" || return 1
  base=$(basename "$file_path")
  case "$base" in
    CORE.md)          ;;
    AGENT_TOOLS.md)   touch "$STATE_DIR/AGENT_TOOLS.done" ;;
    PAIRING_MODE.md)  touch "$STATE_DIR/MODE.done" "$STATE_DIR/MODE_PAIRING.done" ;;
    MULTI_AGENT_MODE.md)
                      touch "$STATE_DIR/MODE.done" "$STATE_DIR/MODE_MULTI_AGENT.done" ;;
    SUBAGENT_MODE.md) touch "$STATE_DIR/MODE.done" "$STATE_DIR/MODE_SUBAGENT.done" ;;
    GUARDRAILS.md)    touch "$STATE_DIR/GUARDRAILS.done" ;;
    REPOSITORY.md)    touch "$STATE_DIR/REPOSITORY.done" ;;
    USAGE.md)         touch "$STATE_DIR/USAGE.done" ;;
    COLLABORATION_CONTINUITY.md)
                      touch "$STATE_DIR/COLLABORATION_CONTINUITY.done" ;;
    *) return 1 ;;
  esac
}

SAFE_READ_TARGET=""
is_safe_read_command_for_allowed_paths() {
  local allowed_path_fn="$1"
  local command_to_check="$2"
  local cmd range path

  SAFE_READ_TARGET=""

  # The first check rejects shell metacharacters that would make token-level
  # validation misleading. Quoted paths are intentionally unsupported here:
  # __BRAND_NAME_TITLE__ init docs do not need spaces or shell expansion.
  if echo "$command_to_check" | grep -qE '[;&|<>`\\]|[$][(]'; then
    return 1
  fi

  # shellcheck disable=SC2086
  set -- $command_to_check
  cmd="$1"
  shift || true

  case "$cmd" in
    cat)
      [[ "$#" -eq 1 ]] || return 1
      "$allowed_path_fn" "$1" || return 1
      SAFE_READ_TARGET="$1"
      ;;
    sed)
      # Allow common print ranges only. Explicitly excludes -i and extra files.
      [[ "$#" -eq 3 ]] || return 1
      [[ "$1" == "-n" ]] || return 1
      range="$2"
      range="${range#\'}"
      range="${range%\'}"
      range="${range#\"}"
      range="${range%\"}"
      [[ "$range" =~ ^[0-9]+(,[0-9]+)?p$ ]] || return 1
      "$allowed_path_fn" "$3" || return 1
      SAFE_READ_TARGET="$3"
      ;;
    *) return 1 ;;
  esac
}

is_safe_bash_init_read() {
  # The first check rejects shell metacharacters that would make token-level
  # validation misleading. Quoted paths are intentionally unsupported here:
  # __BRAND_NAME_TITLE__ init docs do not need spaces or shell expansion.
  is_safe_read_command_for_allowed_paths is_session_init_doc_path "$command"
}

is_safe_guardrails_conditional_read() {
  local trimmed guard_path inner_command

  trimmed=$(printf '%s' "$command" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

  # Allow exactly:
  # if [ -f <GUARDRAILS.md> ]; then <single safe read command on same path>; fi
  # Semicolons are accepted only for this exact wrapper shape.
  if echo "$trimmed" | grep -qE '[&|<>`\\]|[$][(]'; then
    return 1
  fi

  [[ "$trimmed" =~ ^if[[:space:]]+\[[[:space:]]+-f[[:space:]]+([^[:space:];]+)[[:space:]]+\][[:space:]]*\;[[:space:]]*then[[:space:]]+(.+)[[:space:]]*\;[[:space:]]*fi$ ]] || return 1
  guard_path="${BASH_REMATCH[1]}"
  inner_command="${BASH_REMATCH[2]}"

  is_required_doc_path "$guard_path" || return 1
  case "$guard_path" in
    *GUARDRAILS.md) ;;
    *) return 1 ;;
  esac

  is_safe_read_command_for_allowed_paths is_required_doc_path "$inner_command" || return 1
  [[ "$SAFE_READ_TARGET" == "$guard_path" ]]
}

is_safe_guardrails_existence_probe() {
  local trimmed probe_path

  trimmed=$(printf '%s' "$command" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  if echo "$trimmed" | grep -qE '[;&|<>`\\]|[$][(]'; then
    return 1
  fi

  if [[ "$trimmed" =~ ^test[[:space:]]+-f[[:space:]]+([^[:space:]]+)$ ]]; then
    probe_path="${BASH_REMATCH[1]}"
  elif [[ "$trimmed" =~ ^\[[[:space:]]+-f[[:space:]]+([^[:space:]]+)[[:space:]]+\]$ ]]; then
    probe_path="${BASH_REMATCH[1]}"
  elif [[ "$trimmed" =~ ^\[\[[[:space:]]+-f[[:space:]]+([^[:space:]]+)[[:space:]]+\]\]$ ]]; then
    probe_path="${BASH_REMATCH[1]}"
  else
    return 1
  fi

  is_required_doc_path "$probe_path" || return 1
  case "$probe_path" in
    *GUARDRAILS.md) return 0 ;;
    *) return 1 ;;
  esac
}

is_plain_echo_command() {
  local trimmed

  trimmed=$(printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  if echo "$trimmed" | grep -qE '[;<>`\\]|[$][(]'; then
    return 1
  fi

  [[ "$trimmed" =~ ^echo($|[[:space:]].*) ]]
}

is_safe_guardrails_probe_wrapper() {
  local trimmed after_prefix probe_path remainder then_branch else_branch

  trimmed=$(printf '%s' "$command" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
  if echo "$trimmed" | grep -qE '[;<>`\\]|[$][(]'; then
    return 1
  fi

  [[ "$trimmed" == test\ -f\ * ]] || return 1
  [[ "$trimmed" == *" && "* ]] || return 1
  [[ "$trimmed" == *" || "* ]] || return 1
  after_prefix="${trimmed#test -f }"
  probe_path="${after_prefix%% && *}"
  [[ -n "$probe_path" && "$probe_path" != "$after_prefix" ]] || return 1

  remainder="${after_prefix#"$probe_path"}"
  remainder="${remainder# && }"
  [[ "$remainder" == *' || '* ]] || return 1
  then_branch="${remainder% || *}"
  else_branch="${remainder##* || }"
  [[ -n "$then_branch" && -n "$else_branch" ]] || return 1

  is_required_doc_path "$probe_path" || return 1
  case "$probe_path" in
    *GUARDRAILS.md) ;;
    *) return 1 ;;
  esac

  if ! is_plain_echo_command "$then_branch"; then
    is_safe_read_command_for_allowed_paths is_required_doc_path "$then_branch" || return 1
    [[ "$SAFE_READ_TARGET" == "$probe_path" ]] || return 1
  fi

  is_plain_echo_command "$else_branch"
}

# Codex currently exposes shell reads through the Bash hook surface. Allow a
# narrow set of read-only commands for the mandatory docs so the gate can clear
# even when MCP filesystem read tools are unavailable.
if [[ "$tool_name" == "Bash" ]] && echo "$command" | grep -qE 'CORE\.md|AGENT_TOOLS\.md|PAIRING_MODE\.md|MULTI_AGENT_MODE\.md|SUBAGENT_MODE\.md|GUARDRAILS\.md|REPOSITORY\.md|docs/USAGE\.md|COLLABORATION_CONTINUITY\.md'; then
  if ! is_safe_bash_init_read && ! is_safe_guardrails_conditional_read && ! is_safe_guardrails_existence_probe && ! is_safe_guardrails_probe_wrapper; then
    repeated_invalid_read=0
    if ! mkdir "$STATE_DIR/INVALID_BASH_READ.seen" 2>/dev/null; then
      repeated_invalid_read=1
    fi
    cat <<EOF >&2
BLOCKED — session initialization allows Bash only for simple read-only doc commands.

Use exactly one file per command and one command per tool call.
Expected global contract root: ~/__BRAND_GLOBAL_DIRNAME__/
Allowed commands: cat, sed, an exact GUARDRAILS.md existence
probe, or a narrow \`test -f GUARDRAILS.md && ... || ...\` wrapper.
EOF
    if (( repeated_invalid_read )); then
      cat <<EOF >&2

STOP_RETRYING — another invalid initialization read was already rejected.
Do not try command or path variations. Report the initialization-hook failure.
EOF
    fi
    exit 2
  fi

  reset_invalid_bash_read_marker
  if [[ -n "$SAFE_READ_TARGET" ]]; then
    mark_session_init_doc_path "$SAFE_READ_TARGET"
  fi
  clear_if_ready
  exit 0
fi

# Read calls always pass through; track mandatory doc reads. Codex filesystem
# MCP tools use "path"/"paths" while Claude uses "file_path".
if [[ "$tool_name" == "Read" || "$tool_name" =~ ^mcp__filesystem__read ]]; then
  read_paths=$(printf '%s\n%s\n%s\n' \
    "$(json_val file_path)" \
    "$(json_val path)" \
    "$(json_array_vals paths)" |
    sed '/^$/d')
  recognized_init_read=0
  while IFS= read -r file_path; do
    [[ -z "$file_path" ]] && continue
    if mark_session_init_doc_path "$file_path"; then
      recognized_init_read=1
    fi
  done <<< "$read_paths"
  if (( recognized_init_read )); then
    reset_invalid_bash_read_marker
  fi
  clear_if_ready

  exit 0
fi

# Non-Read tool call: block if gate not cleared.
# Stderr is shown to the agent as the block reason (exit 2 protocol).
mark_absent_project_docs_if_needed
missing=""
[[ ! -f "$STATE_DIR/AGENT_TOOLS.done" ]] && missing="$missing
  - ~/__BRAND_GLOBAL_DIRNAME__/AGENT_TOOLS.md"
[[ ! -f "$STATE_DIR/MODE.done" ]] && missing="$missing
  - The applicable mode contract from the Mode Selection Gate"
if requires_pairing_companion_docs; then
  [[ -f "$project_dir/REPOSITORY.md" ]] && [[ ! -f "$STATE_DIR/REPOSITORY.done" ]] && missing="$missing
  - REPOSITORY.md (repo root)"
  [[ -f "$project_dir/docs/USAGE.md" ]] && [[ ! -f "$STATE_DIR/USAGE.done" ]] && missing="$missing
  - docs/USAGE.md (from repo root)"
  [[ ! -f "$STATE_DIR/COLLABORATION_CONTINUITY.done" ]] && missing="$missing
  - ~/__BRAND_GLOBAL_DIRNAME__/COLLABORATION_CONTINUITY.md"
fi
if [[ -f "$project_dir/GUARDRAILS.md" ]] && [[ ! -f "$STATE_DIR/GUARDRAILS.done" ]]; then
  missing="$missing
  - GUARDRAILS.md (project root)"
fi

cat <<EOF >&2
BLOCKED — session initialization incomplete.

You must Read these files before using any other tool:
$missing

Use Read, an MCP filesystem read tool, or a simple Bash read command
(cat, sed) on each path above, then retry your action.
EOF
exit 2
