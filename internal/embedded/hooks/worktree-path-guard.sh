#!/bin/bash
# PreToolUse hook: catch the .worktrees/<id>/<id>/ path-duplication bug.
#
# Agents occasionally concatenate the worktree root (already .worktrees/<id>)
# with a relative path that itself starts with the task id, producing
# .worktrees/<id>/<id>/… and burning turns on ENOENT before self-diagnosing.
# This hook fails fast on file-tool calls carrying that pattern.
#
# Coverage:
#   - Read, Write, Edit — VERIFIED (tool_input.file_path per Claude Code docs)
#   - MultiEdit — BEST-EFFORT, UNVERIFIED. Shipped because the hook is
#     one-sided safe: if MultiEdit does send file_path, we catch the bug;
#     if it sends something else, the json_val extraction returns empty
#     and we exit 0 (silent no-op, no false deny). Future maintainers:
#     do NOT treat this as confirmed protection until a real PreToolUse
#     payload is captured. See TECH_DEBT.md.
#   - NotebookEdit — NOT COVERED. Same unverified status as MultiEdit
#     but less common, so not worth shipping the matcher entry either.
#     See TECH_DEBT.md.
#
# Not gated on LIZA_AGENT_ID: the failure mode hits Pairing sessions too.
# Portable across Linux and macOS: uses bash 3.2+ regex + string compare,
# no GNU-only grep -P. No external dependencies (no jq).

input=$(cat)

# Extract JSON string values without jq. Matches "key": "value" anywhere in
# the blob — file_path is unique enough inside the Read/Write/Edit/MultiEdit
# payload shapes that a cross-key collision isn't a practical concern.
json_val() { echo "$input" | grep -o "\"$1\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | sed 's/.*:[[:space:]]*"//;s/"$//'; }
json_array_vals() { echo "$input" | grep -o "\"$1\"[[:space:]]*:[[:space:]]*\\[[^]]*\\]" | head -1 | sed -E "s/^\"$1\"[[:space:]]*:[[:space:]]*\\[//; s/\\]$//; s/\",[[:space:]]*\"/\\
/g; s/^\"//; s/\"$//"; }

# For apply_patch payloads, inspect only file headers rather than the whole
# patch body. This catches duplicated worktree paths in edited files without
# false-denying on arbitrary content that merely mentions them.
patch_targets=$(printf '%s' "$input" |
    sed 's/\\n/\
/g' |
    grep -Eo '(\*\*\* (Update|Delete|Add) File: |\+\+\+ |--- )[^"\\]*\.worktrees/[^"\\]*' |
    sed -E 's/^(\*\*\* (Update|Delete|Add) File: |\+\+\+ |--- )//')

targets=$(printf '%s\n%s\n%s\n%s' "$(json_val file_path)" "$(json_val path)" "$(json_array_vals paths)" "$patch_targets" | sed '/^$/d')

if [[ -z "$targets" ]]; then
    exit 0
fi

# Detect .worktrees/<segment>/<same-segment>. POSIX ERE has no backreferences,
# so split: capture the two segments after .worktrees/ and compare them as
# strings. Works with bash 3.2+ (macOS default). Deny if any extracted target
# has the duplicate segment.
while IFS= read -r target; do
    [[ -z "$target" ]] && continue
    while IFS= read -r candidate; do
        [[ -z "$candidate" ]] && continue
        if [[ "$candidate" =~ \.worktrees/([^/]+)/([^/]+) ]]; then
            first="${BASH_REMATCH[1]}"
            second="${BASH_REMATCH[2]}"
            if [[ "$first" == "$second" ]]; then
                deny_reason="Worktree path duplicates the task id segment (.worktrees/$first/$second). The worktree root already ends in the task id — strip the duplicate segment from your relative path. Path: $candidate"
                if [[ "${1:-}" == "copilot" ]]; then
                    printf '{"permissionDecision":"deny","permissionDecisionReason":"%s"}\n' "$deny_reason"
                else
                    cat <<EOF
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"$deny_reason"}}
EOF
                fi
                exit 0
            fi
        fi
    done <<EOF
$(echo "$target" | grep -Eo '\.worktrees/[^[:space:]"]+/[^[:space:]"]+')
EOF
done <<EOF
$targets
EOF
