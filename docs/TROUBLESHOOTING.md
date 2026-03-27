# Troubleshooting Guide

Common issues and solutions when running Liza.

---

## Agent Issues

### COLLISION: agent already registered

**Error:**
```
COLLISION: planner-1 already registered until 2026-01-20T09:53:55Z
ERROR: Failed to register agent planner-1 (collision?)
```

**Cause:** Agent was killed (Ctrl+C, SIGKILL, crash) before it could unregister. The stale entry persists in `state.yaml`.

**Solutions:**

1. **Recover and respawn** — one command does everything:
   ```bash
   liza recover-agent planner-1 --cli claude
   # Releases task claims, removes worktree, deletes agent, then respawns
   ```

2. **Recover without respawn:**
   ```bash
   liza recover-agent planner-1
   liza agent planner --agent-id planner-1
   ```

3. **Wait for lease expiry** — The timestamp shows when the lease expires. After that time, re-registration will succeed.

4. **Use a different agent ID:**
   ```bash
   liza agent planner --agent-id planner-2
   ```

**Prevention:** Use `liza pause` before stopping agents — they'll exit gracefully at next check.

### Agent Timeout (Execution Exceeds Time Limit)

**Symptoms:**
- Agent shows WORKING/REVIEWING/PLANNING status for extended period
- Logs show: "Agent execution timeout (CLI may be hung, will retry)"
- Agent status automatically resets to IDLE after timeout

**Timeouts by role:** Reviewer 30min, Coder 2hr, Planner 4hr. See [CONFIGURATION.md](CONFIGURATION.md#agent-execution-timeouts).

**Expected behavior:** Supervisor kills CLI, resets agent to IDLE, retries after 5s delay. This is self-recovering.

**When to investigate:**
- Frequent timeouts indicate underlying issues (bad test, broken tooling)
- Check agent prompt files (`.liza/agent-prompts/`) to see work in progress
- If timeouts recur on same task, consider marking it BLOCKED

### Agent stuck in loop, not claiming tasks

**Diagnosis:**
```bash
tail -f coder-1.log
# "Waiting for claimable tasks..." → No tasks available
# "Task task-1 blocked on dependencies" → Dependencies not met
```

**Common causes and fixes:**
- No claimable tasks → `liza add-task ...`
- Dependencies not met → check with `liza get tasks <dep-id>`
- System paused → `liza get config.mode` then `liza resume`
- Sprint at checkpoint → `liza get sprint.status` then `liza resume`

---

## Lock and Concurrency Issues

Liza uses file-based locking with classified error types for targeted diagnostics.

### Lock Error Classification

| Type | Meaning |
|------|---------|
| `lock error (timeout)` | Failed to acquire lock within timeout |
| `lock error (stale)` | Lock held by dead process |
| `lock error (disk_full)` | No space left on device |
| `lock error (permission)` | Permission denied on lock file |
| `lock error (filesystem)` | I/O error or filesystem issue |

### timeout — failed to acquire lock

**Diagnosis:**
```bash
ps aux | grep liza                     # Running processes
cat .liza/.state.yaml.lock.pid         # PID of lock holder
ps -p $(cat .liza/.state.yaml.lock.pid)  # Is holder alive?
```

**Common causes:** Long-running operation under lock, many agents competing, hung process, slow filesystem (network mount).

**Solutions:**
- If holder is alive and working → wait 30-60s, retry
- If holder is hung → `kill $(cat .liza/.state.yaml.lock.pid)`, lock auto-cleans on next access
- If high contention → reduce parallel agents (1-3 per role)
- If slow filesystem → `df -T .liza/`, move to local SSD

### stale — lock held by dead process

**Automatic cleanup:** Liza detects stale locks via PID tracking — checks if lock holder process exists, removes lock files if dead, then retries.

**Manual cleanup** (if auto-cleanup fails):
```bash
ps aux | grep liza                    # Verify NO liza processes running
rm .liza/.state.yaml.lock .liza/.state.yaml.lock.pid
```

**Common causes:** `kill -9` (SIGKILL), system crash, OOM killer. **Prevention:** Use `kill` or Ctrl+C for graceful shutdown.

### disk_full — no space left on device

```bash
df -h .liza/        # Check disk space
df -i .liza/        # Check inodes
du -sh .worktrees/* # Find large worktrees
```

**Free space:** Clean merged worktrees, archive old log files, `git gc --aggressive --prune=now`.

### permission — permission denied

```bash
ls -la .liza/.state.yaml.lock*   # Check lock file permissions
ls -ld .liza/                     # Check directory permissions
stat .liza/state.yaml             # Check ownership
```

**Common causes:** Lock file owned by different user (ran as root, now as user), NFS mount with restrictive permissions, SELinux/AppArmor blocking.

**Fix:** `sudo chown -R $(whoami):$(id -gn) .liza/`

### filesystem — I/O error

```bash
dmesg | grep -i error | tail -20   # Filesystem errors
touch .liza/test.txt && rm .liza/test.txt  # Test write
```

**Common causes:** Failing drive, filesystem corruption, network FS timeout, readonly mount. If on NFS/SMB, check network connectivity and remount.

### "state modified by another process, retry"

This is normal — the three-phase claim pattern detected a race condition and will retry automatically. If persistent, too many agents may be competing for the same tasks.

---

## State Validation Failures

### Missing required field

**Error:** `INVALID: missing required field 'sprint'`

**Cause:** State file manually edited or created with old init. **Solution:** Add the missing section per `specs/architecture/blackboard-schema.md`.

### Task status invariant violation

**Error:** `INVALID: IMPLEMENTING_CODE task without assigned_to: task-1`

**Fix the invariant** (edit `.liza/state.yaml` directly):
```yaml
# Option 1: Set assigned_to on the task
# Find the task entry and set:
assigned_to: coder-1

# Option 2: Reset to pipeline initial status
# Find the task entry and set its status to the initial state
# for its role-pair (e.g. DRAFT_CODE for coding-pair).
# See pipeline.yaml for the full list of initial states.
status: DRAFT_CODE   # adjust per role-pair
```

### Circular dependency detected

**Identify the cycle:**
```bash
liza get tasks --format table   # Shows depends_on for each task
# Example cycle: task-1 → task-2 → task-3 → task-1
```

**Break cycle** (edit `.liza/state.yaml` directly):
```yaml
# Find task-3 and remove "task-1" from its depends_on list
```

### Spec file not found

```bash
# Option 1: Create the spec file
mkdir -p specs && vi specs/vision.md

# Option 2: Update spec reference in state (edit .liza/state.yaml directly)
# Find the task entry and set:
#   spec_ref: docs/requirements.md
```

---

## Worktree Issues

### Worktree directory not found

**Error:** `INVALID: IMPLEMENTING task <task-id> has worktree=.worktrees/<task-id> but directory does not exist`

**Recreate:** `git worktree add .worktrees/<task-id> -b task/<task-id> <base-commit>`

*(Replace `<base-commit>` with the task's `base_commit` value from `liza get tasks <task-id>`.)*

**Or reset task** (if work was lost — edit `.liza/state.yaml` directly):
```yaml
# Find the <task-id> entry and set:
# Reset to the pipeline initial status for the task's role-pair
# (e.g. DRAFT_CODE for coding-pair). See pipeline.yaml for all initial states.
status: DRAFT_CODE   # adjust per role-pair
assigned_to: null
worktree: null
```

### Worktree already exists

```bash
# Option 1: Recover task (cleans worktree + branch + state)
liza recover-task <task-id> --force

# Option 2: Delete and recreate (task must be in terminal state)
liza wt-delete <task-id>
liza wt-create <task-id>
```

### Cannot remove worktree: branch is checked out

```bash
git worktree remove .worktrees/<task-id> --force
```

### Worktree directory is dirty

```bash
cd .worktrees/<task-id> && git status

# Commit, stash, or discard as appropriate
git add . && git commit -m "Save progress"   # save
git stash                                      # stash
git reset --hard HEAD                          # discard
```

### Invalid reference: task/<task-id>

Task branch doesn't exist:
```bash
git branch -a | grep task/          # List task branches
git branch task/<task-id> <base-commit>  # Recreate from base_commit
```

*(Replace `<base-commit>` with the task's `base_commit` value from `liza get tasks <task-id>`.)*

---

## Integration Issues

### Integration branch doesn't exist

```bash
git branch integration main
```

### Task marked INTEGRATION_FAILED

When an APPROVED task's merge to integration fails (conflict or test failure):
- Task status changes APPROVED → INTEGRATION_FAILED
- Merge is aborted, integration branch reverted
- Worktree preserved for conflict resolution

See [RECIPES.md](RECIPES.md#integration-failure-recovery) for the full recovery workflow.

**Quick diagnosis:**
```bash
liza get tasks task-1                    # Check failure details
cd .worktrees/task-1 && git status       # See conflicted files
```

**Merge conflict:** Edit files with conflict markers (`<<<<<<<`/`=======`/`>>>>>>>`), resolve, commit. **Test failure:** Run `go test ./...` to reproduce, fix, commit.

Either way: claim the task, fix in worktree, resubmit for review. The resolution goes through normal review before merge retry.

**Prevention:** Keep task scope small, merge integration branch into task branches frequently.

---

## Watcher Alerts

### False positive alerts on fresh state

**Symptom:** Alerts like `ORPHANED REJECTED` or `IMMEDIATE DISCOVERY` with empty task/agent names.

**Cause:** Empty arrays in `state.yaml` produce empty lines that get processed as valid entries.

**Solution:** Update to latest `liza` binary which includes empty-line guards.

### INVALID STATE alert

**Error:** `🚨 INVALID STATE: Agent coder-1 has status WORKING but lease expired`

**Common causes:** Agent lease expired (task took longer than `lease_duration`), task status invariant violation.

**Fix:** Extend the lease or increase `config.lease_duration`. See [CONFIGURATION.md](CONFIGURATION.md#configuration-matrix) for tuning parameters.

---

## Initialization Issues

### Error: .liza already exists

**Solutions:**
1. **Continue with existing state** — just start the agents.
2. **Reset completely:** `rm -rf .liza .worktrees && liza init "New goal"` (requires prior `liza setup`)

### Symlink creation fails on Windows

**Error:**
```
Warning: failed to create CLAUDE.md symlink: symlink ... A required privilege is not held by the client.
```

**Cause:** Windows requires either Developer Mode or Administrator privileges to create symbolic links. Liza uses symlinks in several places, and the impact depends on which link failed:

| Command | Links created | Impact if missing |
|---------|--------------|-------------------|
| `liza init` | Repo-root contract files (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md` → `~/.liza/CORE.md`) | Agents cannot find the behavioral contract from the project directory |
| `liza setup` | Skill links in CLI config dirs (`~/.claude/skills/`, `~/.codex/skills/`, etc.) → `~/.liza/skills/` | Agents cannot load skills (debugging, testing, code review, etc.) |
| `liza setup` | CLI-specific prompt files (e.g. `~/.vibe/prompts/liza.md`) | CLI prompt activation fails for the affected CLI |

**Solutions (pick one):**

1. **Enable Developer Mode** (recommended): Settings → System → For developers → toggle Developer Mode on. Then re-run `liza setup` and `liza init`.

2. **Run elevated**: Open your terminal as Administrator, then re-run the command.

### Error: specs/vision.md required

Create the vision spec first:
```bash
mkdir -p specs
cat > specs/vision.md << 'EOF'
# Vision: Project Name
## Goal
[What you want to build]
## Requirements
[List of requirements]
## Success Criteria
[How to verify completion]
EOF
```

---

## Performance Issues

See [PERFORMANCE.md](PERFORMANCE.md) for tuning parameters, benchmark targets, and lock metric interpretation.

### Agents not responding to changes (30s+ delay)

fsnotify may not work on network filesystems (NFS, SMB). Agents fall back to 30s polling. **Fix:** Use local filesystem, or accept the delay.

Also check: system mode is RUNNING (`liza get config.mode`), sprint status is IN_PROGRESS (`liza get sprint.status`).

### Slow state file reads (>50ms)

```bash
ls -lh .liza/state.yaml   # Check size (target <1MB)
time liza validate          # Time a read
```

**Common causes:** Large state file, slow disk, cache thrashing from external modifications (editors with auto-save, Dropbox syncing `.liza/`).

**Fix:** Archive completed tasks, use SSD, avoid external modifications.

### Slow task claims (5-10s)

Git worktree operations slow on large repos. **Fix:** Use SSD, consider sparse checkout (`git config core.sparseCheckout true`).

### Validation takes too long (>5s)

Large task list or complex dependency graph. **Fix:** Archive old tasks, `liza validate --skip-spec-check` on slow filesystems.

---

## Debugging Techniques

### Verbose output

```bash
liza -v validate              # Timing, detailed errors, internal ops
liza -v claim-task task-1 coder-1
```

### Inspect state

```bash
liza get tasks --format table   # All task statuses
liza get tasks task-1           # Single task detail
liza get agents --format table  # All agents
liza get metrics                # Sprint metrics
liza status                     # Full dashboard
```

### Review logs

```bash
cat .liza/log.yaml              # Activity log
cat .liza/alerts.log            # Alerts
ls .liza/agent-prompts/         # Generated agent prompts
cat .liza/agent-prompts/coder-1-*.txt  # What the agent was told
```

### Monitor locks

```bash
ls -la .liza/.state.yaml.lock*   # Lock file exists?
lsof .liza/.state.yaml.lock      # Who holds it?
cat .liza/.state.yaml.lock.pid   # Lock holder PID
```

### Watch real-time

```bash
watch -n 2 'liza get tasks --format table'
watch -n 2 'liza get agents --format table'
watch -n 5 'liza status'
watch -n 5 'ls -la .worktrees/'
```

### Debug report

```bash
cat > debug-report.txt <<EOF
=== Liza Debug Report ===
Version: $(liza version)
State Validation: $(liza validate 2>&1)
Tasks: $(liza get tasks --format table 2>&1)
Agents: $(liza get agents --format table 2>&1)
Worktrees: $(git worktree list)
Recent Alerts: $(tail -50 .liza/alerts.log 2>/dev/null)
Processes: $(ps aux | grep liza)
Lock Status: $(ls -la .liza/.state.yaml.lock* 2>&1)
EOF
```

---

## Recovery Procedures

### Agent crashed with IMPLEMENTING task (usage limit, OOM, etc.)

When a coder agent crashes (usage limit, OOM, SIGKILL) while a task is IMPLEMENTING:

**Recover by task ID** (recommended — you usually know the task, not the agent):
```bash
liza recover-task <task-id>
liza recover-task <task-id> --force    # if task not in state or agent PID alive
```

**Recover by agent ID** (when you know the agent):
```bash
liza recover-agent <agent-id> --cli claude   # recover + respawn
liza recover-agent <agent-id>                # recover only
```

Both commands perform full cleanup: release claims, remove worktree/branch, delete agent from state. Both are idempotent — safe to run multiple times. Use `--force` if the agent's PID is still alive or (for `recover-task`) if the task is no longer in state but git artifacts remain.

**Diagnosis (if needed):**
```bash
liza get tasks <task-id>          # Check status, assigned_to, lease_expires
liza get agents --format table    # Check agent status and lease
```

<details>
<summary>Manual recovery (granular control)</summary>

**If lease has expired** (current time > `lease_expires`):

```bash
liza release-claim <task-id> --role coder
liza agent coder
```

**If lease has NOT expired:**

```bash
liza delete agent <agent-id> --force
liza release-claim <task-id> --role coder
liza agent coder
```
</details>

### Full state reset (nuclear option)

```bash
cp -r .liza .liza.backup.$(date +%Y%m%d-%H%M%S)
rm -rf .liza
liza setup          # skip if ~/.liza/ already exists
liza init "Goal description"
# Manually migrate in-progress work from backup if needed
```

### Agent stuck in WORKING state

```bash
# Recover by task
liza recover-task <task-id>

# Or recover by agent
liza recover-agent coder-1

# Then restart
liza agent coder --agent-id coder-1
```

---

## Getting Help

1. **Check alerts:** `cat .liza/alerts.log`
2. **Check activity:** `cat .liza/log.yaml`
3. **Validate state:** `liza validate`
4. **Watch live:** `liza tui`
5. **Generate debug report** (see above)
