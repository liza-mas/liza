#!/bin/bash
# Circuit breaker analysis - human-triggered pattern detection
# Usage: liza-analyze.sh [project_root]
#
# Reads anomalies from blackboard, applies pattern rules, generates report.
# For v1: human runs this during checkpoint review.
# For v2: integrated into liza-watch.sh for continuous monitoring.

set -euo pipefail

# --- Path Setup ---
PROJECT_ROOT="${1:-$(git rev-parse --show-toplevel)}"
readonly PROJECT_ROOT
readonly STATE="$PROJECT_ROOT/.liza/state.yaml"
readonly STATE_LOCK="$STATE.lock"
readonly REPORT="$PROJECT_ROOT/.liza/circuit_breaker_report.md"

# --- Helper Functions ---

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

# Count anomalies by type
count_type() {
    local type="$1"
    yq "[.anomalies[] | select(.type == \"$type\")] | length" "$STATE"
}

# Count anomalies with matching field value (duplicates)
count_matching() {
    local type="$1"
    local field="$2"
    yq "[.anomalies[] | select(.type == \"$type\") | .details.$field] | group_by(.) | map(select(length >= 2)) | length" "$STATE"
}

locked_yq() {
    flock -x "$STATE_LOCK" -c "yq -i '$1' '$STATE'"
}

# --- Pattern Detection ---

check_patterns() {
    local triggered=""
    local severity=""
    local pattern=""
    local evidence=""

    # retry_cluster: 3+ retry_loops with similar error_pattern
    local retry_count
    retry_count=$(count_type "retry_loop")
    if [ "$retry_count" -ge 3 ]; then
        local similar
        similar=$(count_matching "retry_loop" "error_pattern")
        if [ "$similar" -gt 0 ]; then
            triggered="true"
            pattern="retry_cluster"
            severity="ARCHITECTURE_FLAW"
            evidence="$retry_count retry_loop anomalies with similar error patterns"
        fi
    fi

    # debt_accumulation: 3+ trade_offs with debt_created=true
    local debt_count
    debt_count=$(yq '[.anomalies[] | select(.type == "trade_off" and .details.debt_created == true)] | length' "$STATE")
    if [ "$debt_count" -ge 3 ]; then
        triggered="true"
        pattern="debt_accumulation"
        severity="SCOPE_FLAW"
        evidence="$debt_count trade-offs creating technical debt"
    fi

    # assumption_cascade: 2+ assumption_violated with same assumption
    local assumption_similar
    assumption_similar=$(count_matching "assumption_violated" "assumption")
    if [ "$assumption_similar" -gt 0 ]; then
        triggered="true"
        pattern="assumption_cascade"
        severity="SPEC_FLAW"
        evidence="Same assumption violated across multiple tasks"
    fi

    # spec_gap_cluster: 2+ spec_ambiguity with same spec_ref
    local spec_similar
    spec_similar=$(count_matching "spec_ambiguity" "spec_ref")
    if [ "$spec_similar" -gt 0 ]; then
        triggered="true"
        pattern="spec_gap_cluster"
        severity="SPEC_FLAW"
        evidence="Multiple tasks hitting same spec ambiguity"
    fi

    # workaround_pattern: 2+ workarounds/trade_offs with similar root_cause
    local workaround_count
    workaround_count=$(yq '[.anomalies[] | select(.type == "workaround" or .type == "trade_off")] | length' "$STATE")
    if [ "$workaround_count" -ge 2 ]; then
        local workaround_similar
        workaround_similar=$(yq '[.anomalies[] | select(.type == "workaround" or .type == "trade_off") | .details.root_cause // .details.what] | group_by(.) | map(select(length >= 2)) | length' "$STATE")
        if [ "$workaround_similar" -gt 0 ]; then
            triggered="true"
            pattern="workaround_pattern"
            severity="ARCHITECTURE_FLAW"
            evidence="$workaround_count workarounds/trade-offs with similar root causes"
        fi
    fi

    # external_service_outage: 2+ external_blockers with same blocker_service
    local external_similar
    external_similar=$(count_matching "external_blocker" "blocker_service")
    if [ "$external_similar" -gt 0 ]; then
        local service
        service=$(yq '[.anomalies[] | select(.type == "external_blocker") | .details.blocker_service] | group_by(.) | map(select(length >= 2)) | .[0][0]' "$STATE")
        triggered="true"
        pattern="external_service_outage"
        severity="EXTERNAL_DEPENDENCY"
        evidence="Multiple tasks blocked by same external service: $service"
    fi

    if [ -n "$triggered" ]; then
        echo "$pattern:$severity:$evidence"
    else
        echo "OK"
    fi
}

# --- Main ---

timestamp=$(iso_timestamp)
result=$(check_patterns)

if [ "$result" == "OK" ]; then
    echo "Circuit breaker: OK — no patterns detected"

    # Update blackboard atomically (including history for audit trail)
    flock -x "$STATE_LOCK" -c "
        yq -i '.circuit_breaker.last_check = \"$timestamp\"' '$STATE'
        yq -i '.circuit_breaker.status = \"OK\"' '$STATE'
        yq -i '.circuit_breaker.history += [{\"timestamp\": \"$timestamp\", \"pattern\": null, \"result\": \"OK\"}]' '$STATE'
    "
    exit 0
fi

# Pattern triggered
pattern=$(echo "$result" | cut -d: -f1)
severity=$(echo "$result" | cut -d: -f2)
evidence=$(echo "$result" | cut -d: -f3-)

echo "🚨 CIRCUIT BREAKER TRIGGERED"
echo "Pattern: $pattern"
echo "Severity: $severity"
echo "Evidence: $evidence"

# Generate report
cat > "$REPORT" << EOF
# Circuit Breaker Report

**Triggered:** $timestamp
**Pattern:** $pattern
**Severity:** $severity

## Trigger Evidence

$evidence

## Anomalies (raw)

\`\`\`yaml
$(yq '.anomalies' "$STATE")
\`\`\`

## Human Decision Required

- [ ] Acknowledge report
- [ ] Confirm severity assessment
- [ ] Determine remediation
- [ ] Release checkpoint with decision logged
EOF

# Update blackboard atomically (including history for audit trail)
flock -x "$STATE_LOCK" -c "
    yq -i '.circuit_breaker.last_check = \"$timestamp\"' '$STATE'
    yq -i '.circuit_breaker.status = \"TRIGGERED\"' '$STATE'
    yq -i '.circuit_breaker.current_trigger.timestamp = \"$timestamp\"' '$STATE'
    yq -i '.circuit_breaker.current_trigger.pattern = \"$pattern\"' '$STATE'
    yq -i '.circuit_breaker.current_trigger.severity = \"$severity\"' '$STATE'
    yq -i '.circuit_breaker.current_trigger.report_file = \"$REPORT\"' '$STATE'
    yq -i '.circuit_breaker.history += [{\"timestamp\": \"$timestamp\", \"pattern\": \"$pattern\", \"severity\": \"$severity\", \"result\": \"TRIGGERED\"}]' '$STATE'
"

# Create CHECKPOINT file to halt agents
touch "$PROJECT_ROOT/.liza/CHECKPOINT"

echo ""
echo "Report written to: $REPORT"
echo "CHECKPOINT file created — agents will halt"
