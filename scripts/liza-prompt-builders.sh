# --- Prompt Builder Functions ---

# Build base bootstrap prompt (outputs to stdout)
build_base_prompt() {
    local goal_desc
    goal_desc=$(yq -r '.goal.description' "$STATE" 2>/dev/null || echo "See specs/vision.md")
    local role_title
    role_title=$(echo "$ROLE" | tr '-' ' ' | sed 's/\b\w/\u&/g')

    cat << EOF
You are a Liza $ROLE agent. Agent ID: $LIZA_AGENT_ID

=== BOOTSTRAP CONTEXT ===
ROLE: $ROLE
SPECS_LOCATION: $LIZA_SPECS
PROJECT: $PROJECT_ROOT
BLACKBOARD: $STATE
GOAL: $goal_desc

Read these specs before acting:
- Role definition: $LIZA_SPECS/architecture/roles.md (section: $role_title)
- Task lifecycle: $LIZA_SPECS/protocols/task-lifecycle.md
- Blackboard schema: $LIZA_SPECS/architecture/blackboard-schema.md
- State machines: $LIZA_SPECS/architecture/state-machines.md

OPERATIONAL RULES:
- You have FULL read/write access to .liza/ directory - USE IT DIRECTLY
- Do NOT ask for permission textually - just use Edit/Write tools
- The human will see tool permission prompts from Claude Code if needed
- Work autonomously: read specs, execute protocol, write to blackboard
- Exit when your current work unit is complete (task implemented, review done, etc.)

HELPER SCRIPTS:
- ~/.claude/scripts/liza-validate.sh <state.yaml> — Validate blackboard state

FORBIDDEN:
- Do NOT attempt to claim tasks - the supervisor has already claimed your task
- Do NOT manually modify task status to CLAIMED
- Do NOT skip worktrees or "simplify" the protocol
- Do NOT make architecture decisions - follow the spec exactly

FIRST ACTIONS:
1. Read your role definition from roles.md
2. Read the current blackboard state: $STATE
3. Read specs/vision.md in the project for the goal details
4. Execute your role's protocol - write directly to the blackboard
EOF
}

# Build planner context (outputs to stdout)
build_planner_context() {
    # Compute sprint state
    local total_tasks
    total_tasks=$(yq '.tasks | length' "$STATE" 2>/dev/null || echo 0)
    local merged
    merged=$(count_tasks '.status == "MERGED"')
    local blocked
    blocked=$(count_tasks '.status == "BLOCKED"')
    local integration_failed
    integration_failed=$(count_tasks '.status == "INTEGRATION_FAILED"')
    local in_progress
    in_progress=$(count_tasks '.status == "CLAIMED" or .status == "READY_FOR_REVIEW" or .status == "APPROVED"')
    local unclaimed
    unclaimed=$(count_tasks '.status == "UNCLAIMED"')
    local hypothesis_exhausted
    hypothesis_exhausted=$(yq '[.tasks[] | select(.failed_by != null and (.failed_by | length) >= 2)] | length' "$STATE" 2>/dev/null || echo 0)
    local immediate_discoveries
    immediate_discoveries=$(yq '[.discovered[] | select(.urgency == "immediate" and .converted_to_task == null)] | length' "$STATE" 2>/dev/null || echo 0)

    # Determine wake trigger
    local wake_trigger="UNKNOWN"
    if [ "$total_tasks" -eq 0 ]; then
        wake_trigger="INITIAL_PLANNING"
    elif [ "$blocked" -gt 0 ]; then
        wake_trigger="BLOCKED_TASKS"
    elif [ "$integration_failed" -gt 0 ]; then
        wake_trigger="INTEGRATION_FAILED"
    elif [ "$hypothesis_exhausted" -gt 0 ]; then
        wake_trigger="HYPOTHESIS_EXHAUSTED"
    elif [ "$immediate_discoveries" -gt 0 ]; then
        wake_trigger="IMMEDIATE_DISCOVERY"
    fi

    cat << EOF

=== PLANNING CONTEXT ===
WAKE TRIGGER: $wake_trigger

SPRINT STATE:
- Total tasks: $total_tasks
- Merged: $merged
- In progress: $in_progress
- Unclaimed: $unclaimed
- Blocked: $blocked
- Integration failed: $integration_failed
- Hypothesis exhausted: $hypothesis_exhausted
- Immediate discoveries: $immediate_discoveries

INSTRUCTIONS:
EOF

    # Context-specific instructions based on wake trigger
    case "$wake_trigger" in
        INITIAL_PLANNING)
            cat << 'EOF'
This is initial planning. Decompose the goal into tasks:

1. Read specs/vision.md thoroughly — understand the goal, constraints, success criteria

2. Identify the minimal set of tasks that achieve the goal

3. Analyze task dependencies:
   - Which tasks produce artifacts others need? (APIs, schemas, utilities)
   - Which tasks modify shared code that others will build on?
   - Can tasks run in parallel, or must they be sequential?
   - Draw the dependency graph mentally before writing tasks

4. For each task, define:
   - id: short kebab-case identifier (e.g., "add-auth-middleware")
   - description: what to build (1-2 sentences)
   - done_when: observable completion criteria (testable, specific)
   - scope: files/modules to touch, what's in/out of scope
   - priority: 1 (highest) to 5 (lowest)
   - depends_on: [task-ids] that must be MERGED before this task can be claimed
   - spec_ref: path to relevant spec section

5. TDD ENFORCEMENT (MANDATORY for code tasks):
   - Each code task MUST include its own tests — do NOT create separate "add tests" tasks
   - done_when criteria must be verifiable by tests the coder writes
   - Code Reviewer will reject code tasks without tests covering done_when
   - Exempt: documentation-only, config-only, or spec-only tasks (no code = no tests)
   - Rationale: Coder can't validate their work without tests; separate test tasks break TDD

6. Dependency guidelines:
   - depends_on: [] for tasks with no prerequisites (can start immediately)
   - depends_on: [task-a] for tasks that need task-a's output
   - Avoid long chains — prefer wide parallelism over deep sequences
   - If A depends on B depends on C, consider if A really needs C directly

7. Prefer small, independent tasks over large coupled ones
   - Each task = implementation + tests (not separate tasks)
   - A task is "small" if one coder can complete it in one session

8. Write tasks to blackboard with status: UNCLAIMED
EOF
            ;;
        BLOCKED_TASKS)
            cat << 'EOF'
Tasks are BLOCKED. Analyze and resolve:
1. Read blocked tasks from blackboard — understand blocker_reason
2. Determine if blocker is:
   - Missing dependency → create prerequisite task
   - Spec ambiguity → clarify spec, unblock task
   - External dependency → document, possibly supersede task
   - Wrong approach → supersede task, create alternative
3. Update blocked tasks: either unblock (status → UNCLAIMED) or supersede
4. Log decisions in task history
EOF
            ;;
        INTEGRATION_FAILED)
            cat << 'EOF'
Integration failed. Diagnose and plan fix:
1. Read INTEGRATION_FAILED tasks — check test output logs
2. Determine failure cause:
   - Merge conflict → task may need rebase, reassign
   - Test failure → create fix task or reassign original
   - Environment issue → document, create setup task
3. Either reassign task (status → UNCLAIMED) or create follow-up task
4. Consider if failure reveals spec gap — update specs if needed
EOF
            ;;
        HYPOTHESIS_EXHAUSTED)
            cat << 'EOF'
Multiple coders failed on same task. Re-evaluate:
1. Read task history — understand what was tried and why it failed
2. Determine if:
   - Task is impossible as specified → revise or supersede
   - Missing context/docs → add to task description
   - Needs different approach → update scope/guidance
   - Spec is wrong → fix spec first
3. Either update task and reassign, or supersede with new approach
4. Consider breaking into smaller tasks if too complex
EOF
            ;;
        IMMEDIATE_DISCOVERY)
            cat << 'EOF'
Urgent discoveries need attention:
1. Read discovered[] entries with urgency=immediate
2. For each, decide:
   - Convert to task → create task, set converted_to_task field
   - Defer → change urgency to "deferred" with rationale
   - Reject → document why in discovered entry
3. Prioritize new tasks appropriately (may be high priority)
4. Check if discoveries invalidate existing tasks
EOF
            ;;
    esac
}

# Build coder task context (outputs to stdout)
build_coder_context() {
    local task_desc
    task_desc=$(get_task_field "$CLAIMED_TASK_ID" "description")
    local task_done_when
    task_done_when=$(get_task_field "$CLAIMED_TASK_ID" "done_when")
    local task_scope
    task_scope=$(get_task_field "$CLAIMED_TASK_ID" "scope")

    cat << EOF

=== ASSIGNED TASK ===
TASK ID: $CLAIMED_TASK_ID
WORKTREE: $PROJECT_ROOT/$CLAIMED_WORKTREE
DESCRIPTION: $task_desc

DONE WHEN:
$task_done_when

SCOPE:
$task_scope

INSTRUCTIONS:
- The task is already CLAIMED for you. Do NOT run liza-claim-task.sh.
- Work ONLY in the worktree directory: cd $PROJECT_ROOT/$CLAIMED_WORKTREE
- TDD (code tasks): Write tests FIRST that verify done_when criteria, then implement until tests pass
- Tests are MANDATORY for code tasks — Code Reviewer will reject code without tests. Use the testing skill.
- Exempt: doc-only, config-only, or spec-only tasks (no code = no tests required)
- Use the clean-code skill at the end of the implementation
- When complete, run: $SCRIPT_DIR/liza-submit-for-review.sh <task-id> <commit-sha>
  (atomically sets READY_FOR_REVIEW, review_commit, and history)
EOF
}

# Build code-reviewer task context (outputs to stdout)
build_reviewer_context() {
    local task_desc
    task_desc=$(get_task_field "$REVIEW_TASK_ID" "description")
    local task_done_when
    task_done_when=$(get_task_field "$REVIEW_TASK_ID" "done_when")
    local task_assigned_to
    task_assigned_to=$(get_task_field "$REVIEW_TASK_ID" "assigned_to")
    local task_base_commit
    task_base_commit=$(get_task_field "$REVIEW_TASK_ID" "base_commit")

    cat << EOF

=== REVIEW TASK ===
TASK ID: $REVIEW_TASK_ID
WORKTREE: $PROJECT_ROOT/$REVIEW_WORKTREE
BASE COMMIT: $task_base_commit
REVIEW COMMIT: $REVIEW_COMMIT
AUTHOR: $task_assigned_to
DESCRIPTION: $task_desc

DONE WHEN:
$task_done_when

INSTRUCTIONS:
- The task is already assigned to you for review.
- Verify HEAD matches REVIEW_COMMIT: git -C $PROJECT_ROOT/$REVIEW_WORKTREE rev-parse HEAD. If mismatch, REJECT.
- Review ALL changes in the worktree (base_commit → review_commit), not just the latest commit.
  Each review is a fresh evaluation: "does this worktree satisfy the task?"
  Use: git -C $PROJECT_ROOT/$REVIEW_WORKTREE diff $task_base_commit..$REVIEW_COMMIT
- Apply the code-review skill to the full diff
- If change touches specs/, introduces new abstractions, adds state/lifecycle, or spans 3+ modules: also apply systemic-thinking skill
- TDD ENFORCEMENT (code tasks): REJECT if tests are missing or don't cover done_when criteria
- Exempt: doc-only, config-only, or spec-only tasks (no code = no tests required)
- Verify the done_when criteria are met AND tests exercise those criteria (for code tasks)
- If APPROVED/REJECTED: run $SCRIPT_DIR/liza-submit-verdict.sh <task-id> <APPROVED|REJECTED> [rejection_reason]
  (atomically updates verdict, review fields, and history; approved sets approved_by)
- Always update your agent status to IDLE when done
EOF
}
