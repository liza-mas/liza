#!/bin/bash
# Initialize Liza blackboard for new goal
# Usage: liza-init.sh "Goal description"

set -euo pipefail

# --- Path Setup ---
PROJECT_ROOT=$(git rev-parse --show-toplevel)
readonly PROJECT_ROOT
readonly LIZA_DIR="$PROJECT_ROOT/.liza"

# --- Helper Functions ---

die() {
    echo "Error: $*" >&2
    exit 1
}

iso_timestamp() {
    date -u +%Y-%m-%dT%H:%M:%SZ
}

# --- Validation ---

if [ -d "$LIZA_DIR" ]; then
    die ".liza already exists. Remove or use existing."
fi

if [ ! -f "$PROJECT_ROOT/specs/vision.md" ]; then
    die "specs/vision.md required before initialization.
Create vision document first. See templates/vision-template.md"
fi

# --- Initialize Directory Structure ---

mkdir -p "$LIZA_DIR"
mkdir -p "$LIZA_DIR/archive"

# --- Get Goal Description ---

goal_desc="${1:-}"
if [ -z "$goal_desc" ]; then
    read -rp "Goal description: " goal_desc
fi

# --- Generate IDs and Timestamps ---

timestamp=$(iso_timestamp)
goal_id="goal-$(date +%s)"  # Store once to avoid race between goal.id and sprint.goal_ref

# --- Create State File ---

cat > "$LIZA_DIR/state.yaml" << EOF
version: 1

goal:
  id: $goal_id
  description: ""
  created: $timestamp
  status: IN_PROGRESS
  alignment_history:
    - timestamp: $timestamp
      event: initialization
      summary: "Initial goal. No tasks defined yet."

tasks: []

agents: {}

discovered: []

handoff: {}

human_notes: []

anomalies: []

spec_changes: []

sprint:
  id: sprint-1
  goal_ref: $goal_id
  scope:
    planned: []
    stretch: []
  timeline:
    started: $timestamp
    deadline: null  # Human sets deadline
    checkpoint_at: null
    ended: null
  status: IN_PROGRESS
  metrics:
    tasks_done: 0
    tasks_in_progress: 0
    tasks_blocked: 0
    iterations_total: 0
    review_cycles_total: 0
  retrospective: null

circuit_breaker:
  last_check: null
  status: OK
  current_trigger: null
  history: []

config:
  max_coder_iterations: 10
  max_review_cycles: 5
  heartbeat_interval: 60
  lease_duration: 1800
  coder_poll_interval: 30
  coder_max_wait: 1800
  planner_poll_interval: 60
  planner_max_wait: 600
  reviewer_poll_interval: 30
  reviewer_max_wait: 1800
  integration_branch: integration
  escalation_webhook: null  # v1.1: URL for external notifications (not yet implemented)
EOF

# --- Create Log File ---

cat > "$LIZA_DIR/log.yaml" << EOF
- timestamp: $timestamp
  agent: system
  action: initialized
  detail: ""
EOF

# --- Create Supporting Files ---

touch "$LIZA_DIR/alerts.log"
touch "$LIZA_DIR/state.yaml.lock"

# --- Set User-Provided Description (YAML-safe) ---

GOAL_DESC="$goal_desc" yq -i '.goal.description = strenv(GOAL_DESC)' "$LIZA_DIR/state.yaml"
GOAL_DESC="$goal_desc" yq -i '.[0].detail = strenv(GOAL_DESC)' "$LIZA_DIR/log.yaml"

# --- Validate Branch State ---

current_branch=$(git branch --show-current)
if [ "$current_branch" != "main" ] && [ "$current_branch" != "master" ]; then
    echo "Warning: HEAD is on '$current_branch', not main/master."
    echo "Integration branch will be created from this commit."
    read -rp "Continue? (y/N) " confirm
    if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
        echo "Aborted. Switch to main/master before initializing."
        rm -rf "$LIZA_DIR"  # Clean up partial initialization
        exit 1
    fi
fi

# --- Create Integration Branch ---

git rev-parse --verify integration >/dev/null 2>&1 || \
    git branch integration HEAD

echo "Liza initialized at $LIZA_DIR"
echo "Integration branch: integration"
