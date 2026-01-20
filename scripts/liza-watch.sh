#!/bin/bash
# Monitor Liza blackboard and alert on conditions
# Usage: liza-watch.sh [project_root]

set -euo pipefail

# --- Configuration ---
readonly CHECK_INTERVAL=10

# --- Path Setup ---
PROJECT_ROOT="${1:-$(git rev-parse --show-toplevel)}"
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly ALERTS_LOG="$PROJECT_ROOT/.liza/alerts.log"
readonly LIZA_DIR="$PROJECT_ROOT/.liza"

# --- Helper Functions ---

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

epoch_now() {
    date +%s
}

to_epoch() {
    local timestamp="$1"
    date -d "$timestamp" +%s 2>/dev/null || echo 0
}

log_alert() {
    local level="$1"
    local message="$2"
    local timestamp
    timestamp=$(iso_timestamp)
    echo "[$timestamp] $level $message" | tee -a "$ALERTS_LOG" >&2

    # Desktop notification if available
    command -v notify-send >/dev/null && notify-send "Liza Alert" "$message" || true
}

# --- Check Functions ---

check_expired_leases() {
    local now
    now=$(epoch_now)

    # Check coder leases (agents with active tasks)
    while IFS= read -r line; do
        local agent lease task
        agent=$(echo "$line" | cut -d: -f1)
        lease=$(echo "$line" | cut -d: -f2)
        task=$(echo "$line" | cut -d: -f3)

        if [ -n "$lease" ] && [ "$lease" != "null" ]; then
            local lease_epoch
            lease_epoch=$(to_epoch "$lease")
            if [ "$now" -gt "$lease_epoch" ]; then
                log_alert "⚠️ LEASE EXPIRED:" "$agent on $task"
            fi
        fi
    done < <(yq -r '.agents | to_entries[] | select(.value.task != null) | "\(.key):\(.value.lease_expires):\(.value.task)"' "$STATE" 2>/dev/null)

    # Check Code Reviewer leases (READY_FOR_REVIEW tasks with active review claim)
    while IFS= read -r line; do
        local task reviewer lease
        task=$(echo "$line" | cut -d: -f1)
        reviewer=$(echo "$line" | cut -d: -f2)
        lease=$(echo "$line" | cut -d: -f3)

        if [ -n "$lease" ] && [ "$lease" != "null" ]; then
            local lease_epoch
            lease_epoch=$(to_epoch "$lease")
            if [ "$now" -gt "$lease_epoch" ]; then
                log_alert "⚠️ REVIEW LEASE EXPIRED:" "$reviewer on $task — review can be reclaimed"
            fi
        fi
    done < <(yq -r '.tasks[] | select(.status == "READY_FOR_REVIEW" and .reviewing_by != null) | "\(.id):\(.reviewing_by):\(.review_lease_expires)"' "$STATE" 2>/dev/null)
}

check_blocked_tasks() {
    local seen_file="/tmp/liza-watch-blocked-$$"
    touch "$seen_file"

    while IFS= read -r line; do
        local task reason
        task=$(echo "$line" | cut -d: -f1)
        reason=$(echo "$line" | cut -d: -f2-)

        if ! grep -q "^$task$" "$seen_file" 2>/dev/null; then
            log_alert "⚠️ BLOCKED:" "$task — $reason"
            echo "$task" >> "$seen_file"
        fi
    done < <(yq -r '.tasks[] | select(.status == "BLOCKED") | "\(.id):\(.blocked_reason // "no reason")"' "$STATE" 2>/dev/null)
}

# Detect REJECTED tasks where assigned coder is no longer active
check_orphaned_rejected() {
    local seen_file="/tmp/liza-watch-orphaned-$$"
    local grace_period=30
    local now
    now=$(epoch_now)

    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task assignee
        task=$(echo "$line" | cut -d: -f1)
        [ -z "$task" ] && continue
        assignee=$(echo "$line" | cut -d: -f2)
        [ -z "$assignee" ] && continue

        local agent_status
        agent_status=$(yq -r ".agents.\"$assignee\".status // \"MISSING\"" "$STATE" 2>/dev/null)

        if [ "$agent_status" != "WORKING" ]; then
            local first_seen
            first_seen=$(grep "^$task:" "$seen_file" 2>/dev/null | cut -d: -f2)
            if [ -z "$first_seen" ]; then
                echo "$task:$now" >> "$seen_file"
            elif [ $((now - first_seen)) -gt "$grace_period" ]; then
                log_alert "🚨 ORPHANED REJECTED:" "$task — assigned to $assignee but agent is $agent_status (orphaned ${grace_period}s+)"
                sed -i "/^$task:/d" "$seen_file" 2>/dev/null || true
            fi
        else
            sed -i "/^$task:/d" "$seen_file" 2>/dev/null || true
        fi
    done < <(yq -r '.tasks[] | select(.status == "REJECTED" and .assigned_to != null) | "\(.id):\(.assigned_to)"' "$STATE" 2>/dev/null)
}

check_review_loops() {
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task cycles
        task=$(echo "$line" | cut -d: -f1)
        cycles=$(echo "$line" | cut -d: -f2)
        cycles=${cycles:-0}
        if [ "$cycles" -ge 5 ]; then
            log_alert "🚨 REVIEW LOOP:" "$task — $cycles cycles (at cliff)"
        fi
    done < <(yq -r '.tasks[] | select(.review_cycles_current != null) | "\(.id):\(.review_cycles_current)"' "$STATE" 2>/dev/null)
}

check_integration_failures() {
    while IFS= read -r task; do
        log_alert "🚨 INTEGRATION FAILED:" "$task"
    done < <(yq -r '.tasks[] | select(.status == "INTEGRATION_FAILED") | .id' "$STATE" 2>/dev/null)
}

check_hypothesis_exhaustion() {
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task count
        task=$(echo "$line" | cut -d: -f1)
        count=$(echo "$line" | cut -d: -f2)
        count=${count:-0}
        if [ "$count" -ge 2 ]; then
            log_alert "🚨 HYPOTHESIS EXHAUSTION:" "$task — requires rescope"
        fi
    done < <(yq -r '.tasks[] | select(.failed_by != null) | "\(.id):\(.failed_by | length)"' "$STATE" 2>/dev/null)
}

check_reassigned() {
    local seen_file="/tmp/liza-watch-reassigned-$$"
    touch "$seen_file"

    while IFS=$'\t' read -r task current_assignee first_claimer; do
        if [ -n "$current_assignee" ] && [ -n "$first_claimer" ] && [ "$current_assignee" != "$first_claimer" ]; then
            if ! grep -q "^$task$" "$seen_file" 2>/dev/null; then
                log_alert "⚠️ REASSIGNED:" "$task — now $current_assignee (was $first_claimer), hypothesis exhaustion risk"
                echo "$task" >> "$seen_file"
            fi
        fi
    done < <(yq -r '.tasks[] | select(.status == "CLAIMED" and .history != null) |
        "\(.id)\t\(.assigned_to)\t\(.history | map(select(.event == "claimed")) | .[0].agent // "")"' "$STATE" 2>/dev/null)
}

check_approaching_limits() {
    # Coder iterations: warn at 8, cliff at 10
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task iter
        task=$(echo "$line" | cut -d: -f1)
        iter=$(echo "$line" | cut -d: -f2)
        iter=${iter:-0}
        if [ "$iter" -ge 8 ] && [ "$iter" -lt 10 ]; then
            log_alert "⚠️ APPROACHING LIMIT:" "$task — iteration $iter/10"
        fi
    done < <(yq -r '.tasks[] | select(.status == "CLAIMED" and .iteration != null) | "\(.id):\(.iteration)"' "$STATE" 2>/dev/null)

    # Review cycles: warn at 3, cliff at 5
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task cycles
        task=$(echo "$line" | cut -d: -f1)
        cycles=$(echo "$line" | cut -d: -f2)
        cycles=${cycles:-0}
        if [ "$cycles" -ge 3 ] && [ "$cycles" -lt 5 ]; then
            log_alert "⚠️ APPROACHING LIMIT:" "$task — review cycle $cycles/5"
        fi
    done < <(yq -r '.tasks[] | select(.review_cycles_current != null) | "\(.id):\(.review_cycles_current)"' "$STATE" 2>/dev/null)

    # Coder failures: warn at 1 IF review_cycles_current >= 3
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local task failures cycles
        task=$(echo "$line" | cut -d: -f1)
        failures=$(echo "$line" | cut -d: -f2)
        cycles=$(echo "$line" | cut -d: -f3)
        failures=${failures:-0}
        cycles=${cycles:-0}
        if [ "$failures" -eq 1 ] && [ "$cycles" -ge 3 ]; then
            log_alert "⚠️ APPROACHING LIMIT:" "$task — 1 coder failed + $cycles review cycles (hypothesis exhaustion risk)"
        fi
    done < <(yq -r '.tasks[] | select(.failed_by != null) | "\(.id):\(.failed_by | length):\(.review_cycles_current // 0)"' "$STATE" 2>/dev/null)
}

check_stalled() {
    local log="$LIZA_DIR/log.yaml"
    if [ -f "$log" ]; then
        local last_entry
        last_entry=$(yq '.[-1].timestamp' "$log" 2>/dev/null)
        if [ -n "$last_entry" ] && [ "$last_entry" != "null" ]; then
            local last_epoch now age
            last_epoch=$(to_epoch "$last_entry")
            now=$(epoch_now)
            age=$((now - last_epoch))

            if [ "$age" -gt 1800 ]; then
                log_alert "⚠️ STALLED:" "no progress for $((age / 60)) minutes"
            fi
        fi
    fi
}

check_stale_drafts() {
    local stale_threshold=1800
    local now
    now=$(epoch_now)

    while IFS= read -r line; do
        local task created
        task=$(echo "$line" | cut -d: -f1)
        created=$(echo "$line" | cut -d: -f2-)

        if [ -n "$created" ] && [ "$created" != "null" ]; then
            local created_epoch
            created_epoch=$(to_epoch "$created")
            if [ "$created_epoch" -gt 0 ]; then
                local age=$((now - created_epoch))
                if [ "$age" -gt "$stale_threshold" ]; then
                    log_alert "⚠️ STALE DRAFT:" "$task — created $((age / 60))min ago, never finalized (Planner crash?)"
                fi
            fi
        fi
    done < <(yq -r '.tasks[] | select(.status == "DRAFT") | "\(.id):\(.created)"' "$STATE" 2>/dev/null)
}

check_immediate_discoveries() {
    while IFS= read -r line; do
        [ -z "$line" ] && continue
        local disc_id desc
        disc_id=$(echo "$line" | cut -d: -f1)
        [ -z "$disc_id" ] && continue
        desc=$(echo "$line" | cut -d: -f2-)
        log_alert "🚨 IMMEDIATE DISCOVERY:" "$disc_id — $desc (Planner should wake)"
    done < <(yq -r '.discovered[] | select(.urgency == "immediate" and .converted_to_task == null) | "\(.id):\(.description)"' "$STATE" 2>/dev/null)
}

check_validity() {
    local validation_output
    if ! validation_output=$(~/.claude/scripts/liza-validate.sh "$STATE" 2>&1); then
        log_alert "🚨 INVALID STATE:" "$validation_output"
    fi
}

check_stale_checkpoint() {
    local checkpoint="$LIZA_DIR/CHECKPOINT"
    if [ -f "$checkpoint" ]; then
        local checkpoint_time
        checkpoint_time=$(head -1 "$checkpoint")
        if [ -n "$checkpoint_time" ]; then
            local checkpoint_epoch now age
            checkpoint_epoch=$(to_epoch "$checkpoint_time")
            now=$(epoch_now)
            age=$((now - checkpoint_epoch))

            if [ "$age" -gt 28800 ]; then
                log_alert "🚨 CHECKPOINT ABANDONED?:" "no human response for $((age / 3600)) hours"
            elif [ "$age" -gt 7200 ]; then
                log_alert "🚨 CHECKPOINT STUCK:" "no human response for $((age / 60)) minutes"
            elif [ "$age" -gt 1800 ]; then
                log_alert "⚠️ CHECKPOINT STALE:" "waiting for human for $((age / 60)) minutes"
            fi
        fi
    fi
}

check_stale_pause() {
    local pause_file="$LIZA_DIR/PAUSE"
    if [ -f "$pause_file" ]; then
        local now pause_mtime age
        now=$(epoch_now)
        pause_mtime=$(stat -c %Y "$pause_file" 2>/dev/null || echo 0)
        age=$((now - pause_mtime))

        if [ "$age" -gt 7200 ]; then
            log_alert "🚨 PAUSE FORGOTTEN?:" "PAUSE file exists for $((age / 60))min — remove to resume"
        elif [ "$age" -gt 1800 ]; then
            log_alert "⚠️ STALE PAUSE:" "PAUSE file exists for $((age / 60))min — forgotten?"
        fi
    fi
}

# --- Main Loop ---

echo "[$(date -u +%H:%M:%S)] Watching $LIZA_DIR/"

while true; do
    if [ -f "$STATE" ]; then
        check_expired_leases
        check_blocked_tasks
        check_orphaned_rejected
        check_review_loops
        check_integration_failures
        check_hypothesis_exhaustion
        check_reassigned
        check_approaching_limits
        check_stalled
        check_stale_drafts
        check_immediate_discoveries
        check_validity
        check_stale_checkpoint
        check_stale_pause
    fi
    sleep "$CHECK_INTERVAL"
done
