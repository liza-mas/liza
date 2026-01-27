# Liza Troubleshooting Guide

Common issues and solutions when running Liza.

---

## Agent Registration

### COLLISION: agent already registered

**Error:**
```
COLLISION: planner-1 already registered until 2026-01-20T09:53:55Z
ERROR: Failed to register agent planner-1 (collision?)
```

**Cause:** Agent was killed (Ctrl+C, SIGKILL, crash) before it could unregister. The stale entry persists in `state.yaml`.

**Solutions:**

1. **Wait for lease expiry** — The timestamp shows when the lease expires. After that time, re-registration will succeed.

2. **Manual cleanup** — Remove the stale agent entry:
   ```bash
   yq -i 'del(.agents."<agent-id>")' .liza/state.yaml
   ```
   Example:
   ```bash
   yq -i 'del(.agents."planner-1")' .liza/state.yaml
   ```

3. **Use a different agent ID** — If running multiple instances:
   ```bash
   LIZA_AGENT_ID=planner-2 ~/.liza/scripts/liza-agent.sh planner
   ```

**Prevention:** Use `touch .liza/PAUSE` before stopping agents — they'll exit gracefully at next check.

---

## Watcher Alerts

### False positive alerts on fresh state

**Error:** Watcher fires alerts like `ORPHANED REJECTED` or `IMMEDIATE DISCOVERY` with empty task/agent names.

**Cause:** Empty arrays in `state.yaml` produce empty lines that get processed as valid entries.

**Solution:** Update to latest `liza-watch.sh` which includes empty-line guards.

### INVALID STATE alert

**Error:**
```
[timestamp] 🚨 INVALID STATE: INVALID: Agent coder-1 has status WORKING but lease expired
```

**Cause:** `state.yaml` has structural issues, missing required fields, or an agent's lease expired while working.

**Common causes:**
- Agent lease expired (task took longer than `lease_duration`)
- Task status invariant violation (e.g., CLAIMED without assigned_to)
- Missing required fields

**Solutions:**

1. **Lease expired:** Extend the lease or increase `config.lease_duration`:
   ```bash
   # Extend lease for stuck agent
   yq -i '.agents."coder-1".lease_expires = "2026-01-20T16:00:00Z"' .liza/state.yaml

   # Or increase default duration (seconds)
   yq -i '.config.lease_duration = 600' .liza/state.yaml
   ```

2. **Debug specific issue:**
   ```bash
   ~/.liza/scripts/liza-validate.sh .liza/state.yaml
   ```

---

## Worktree Issues

### Worktree directory not found

**Error:**
```
INVALID: CLAIMED task task-1 has worktree=.worktrees/task-1 but directory does not exist
```

**Cause:** Worktree was deleted manually or creation failed mid-operation.

**Solutions:**

1. **Recreate the worktree:**
   ```bash
   git worktree add .worktrees/task-1 -b task-1
   ```

2. **Reset task to UNCLAIMED** (if work was lost):
   ```bash
   yq -i '(.tasks[] | select(.id == "task-1")).status = "UNCLAIMED"' .liza/state.yaml
   yq -i '(.tasks[] | select(.id == "task-1")).assigned_to = null' .liza/state.yaml
   yq -i '(.tasks[] | select(.id == "task-1")).worktree = null' .liza/state.yaml
   ```

### Cannot remove worktree: branch is checked out

**Error:** When trying to clean up a worktree after task completion.

**Solution:**
```bash
git worktree remove .worktrees/task-1 --force
```

---

## State Validation Failures

### Missing required field

**Error:**
```
INVALID: missing required field 'sprint'
```

**Cause:** State file was manually edited or created with old init script.

**Solution:** Add the missing section. See `specs/architecture/blackboard-schema.md` for required structure.

### Task status invariant violation

**Error:**
```
INVALID: CLAIMED task without assigned_to: task-1
```

**Cause:** Task was partially updated — status changed but required fields weren't set.

**Solution:** Fix the invariant by adding the missing field:
```bash
yq -i '(.tasks[] | select(.id == "task-1")).assigned_to = "coder-1"' .liza/state.yaml
```

Or reset the task:
```bash
yq -i '(.tasks[] | select(.id == "task-1")).status = "UNCLAIMED"' .liza/state.yaml
```

---

## Integration Branch Issues

### Integration branch doesn't exist

**Error:** Code Reviewer fails to merge because `integration` branch is missing.

**Solution:**
```bash
git branch integration main
```

### Merge conflicts on integration

**Cause:** Multiple tasks modified same files and weren't rebased before merge.

**Solution:** The Code Reviewer should handle this, but if stuck:
```bash
git checkout integration
git merge --abort  # if mid-merge
# Then mark the conflicting task as INTEGRATION_FAILED in state.yaml
```

---

## Initialization Issues

### Error: .liza already exists

**Cause:** Running `liza-init.sh` on a project that's already initialized.

**Solutions:**

1. **Continue with existing state** — Just start the agents.

2. **Reset completely:**
   ```bash
   rm -rf .liza .worktrees
   ~/.liza/scripts/liza-init.sh "New goal description"
   ```

### Error: specs/vision.md required

**Cause:** Init requires a vision document before creating tasks.

**Solution:** Create the vision spec first:
```bash
mkdir -p specs
cat > specs/vision.md << 'EOF'
# Vision: Project Name

## Goal
[What you want to build]

## Requirements
[List of requirements]

## Constraints
[Technical constraints]

## Success Criteria
[How to verify completion]
EOF
```

---

## Performance Issues

### Agents polling too frequently

**Symptom:** High CPU usage, rapid log entries.

**Solution:** Adjust poll intervals in `state.yaml`:
```yaml
config:
  coder_poll_interval: 30      # seconds between checks
  planner_poll_interval: 60
  reviewer_poll_interval: 30
```

### yq commands slow on large state files

**Cause:** State file has grown large with history entries.

**Solution:** Archive completed tasks periodically:
```bash
# Move MERGED/ABANDONED tasks to archive
yq '.tasks[] | select(.status == "MERGED" or .status == "ABANDONED")' .liza/state.yaml > .liza/archive/tasks-$(date +%Y%m%d).yaml
yq -i '.tasks = [.tasks[] | select(.status != "MERGED" and .status != "ABANDONED")]' .liza/state.yaml
```

---

## Recovery Procedures

### Full state reset (nuclear option)

If state is corrupted beyond repair:
```bash
# Backup current state
cp -r .liza .liza.backup.$(date +%Y%m%d-%H%M%S)

# Remove and reinitialize
rm -rf .liza
~/.liza/scripts/liza-init.sh "Goal description"

# Manually migrate any in-progress work from backup if needed
```

### Agent stuck in WORKING state

If an agent crashed and left state as WORKING:
```bash
# Clear the agent entry
yq -i 'del(.agents."<agent-id>")' .liza/state.yaml

# If task was CLAIMED, either:
# 1. Reset to UNCLAIMED (loses work)
yq -i '(.tasks[] | select(.assigned_to == "<agent-id>")).status = "UNCLAIMED"' .liza/state.yaml
yq -i '(.tasks[] | select(.assigned_to == "<agent-id>")).assigned_to = null' .liza/state.yaml

# 2. Or keep CLAIMED and restart with same agent ID
```

---

## Getting Help

1. **Check alerts log:** `cat .liza/alerts.log`
2. **Check activity log:** `cat .liza/log.yaml`
3. **Validate state:** `~/.liza/scripts/liza-validate.sh .liza/state.yaml`
4. **Watch live:** `~/.liza/scripts/liza-watch.sh`
