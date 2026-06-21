# Troubleshooting Guide

Common issues and solutions when running §BRAND_NAME_TITLE§.

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
   §BRAND_BINARY_NAME§ recover-agent planner-1 --cli claude
   # Releases task claims, removes worktree, deletes agent, then respawns
   ```

2. **Recover without respawn:**
   ```bash
   §BRAND_BINARY_NAME§ recover-agent planner-1
   §BRAND_BINARY_NAME§ agent planner --agent-id planner-1
   ```

3. **Wait for lease expiry** — The timestamp shows when the lease expires. After that time, re-registration will succeed.

4. **Use a different agent ID:**
   ```bash
   §BRAND_BINARY_NAME§ agent planner --agent-id planner-2
   ```

**Prevention:** Use `§BRAND_BINARY_NAME§ pause` before stopping agents — they'll exit gracefully at next check.

### Agent Timeout (Execution Exceeds Time Limit)

**Symptoms:**
- Agent shows WORKING/REVIEWING/PLANNING status for extended period
- Logs show: "Agent execution timeout (CLI may be hung, will retry)"
- Agent status automatically resets to IDLE after timeout

**Timeouts by role:** Reviewer 30min, Coder 2hr, Planner 4hr. See [CONFIGURATION.md](CONFIGURATION.md#agent-execution-timeouts).

**Expected behavior:** Supervisor kills CLI, resets agent to IDLE, retries after 5s delay. This is self-recovering.

**When to investigate:**
- Frequent timeouts indicate underlying issues (bad test, broken tooling)
- Check agent prompt files (`§BRAND_PROJECT_DIRNAME§/agent-prompts/`) to see work in progress
- If timeouts recur on same task, consider marking it BLOCKED

### Agent stuck in loop, not claiming tasks

**Diagnosis:**
```bash
tail -f coder-1.log
# "Waiting for claimable tasks..." → No tasks available
# "Task task-1 blocked on dependencies" → Dependencies not met
```

**Common causes and fixes:**
- No claimable tasks → `§BRAND_BINARY_NAME§ add-task ...`
- Dependencies not met → check with `§BRAND_BINARY_NAME§ get tasks <dep-id>`
- System paused → `§BRAND_BINARY_NAME§ get config.mode` then `§BRAND_BINARY_NAME§ resume`
- Sprint at checkpoint → `§BRAND_BINARY_NAME§ get sprint.status` then `§BRAND_BINARY_NAME§ resume`

---

## Lock and Concurrency Issues

§BRAND_NAME_TITLE§ uses file-based locking with classified error types for targeted diagnostics.

### Lock Error Classification

| Type | Meaning |
|------|---------|
| `lock error (timeout)` | Failed to acquire lock within timeout |
| `lock error (stale)` | Diagnostic owner metadata references a dead process |
| `lock error (disk_full)` | No space left on device |
| `lock error (permission)` | Permission denied on lock file |
| `lock error (filesystem)` | I/O error or filesystem issue |

### timeout — failed to acquire lock

**Diagnosis:**
```bash
ps aux | grep §BRAND_BINARY_NAME§                     # Running processes
lsof §BRAND_PROJECT_DIRNAME§/state.yaml.lock             # Process holding the kernel flock, if visible
cat §BRAND_PROJECT_DIRNAME§/state.yaml.lock.owner.json   # Best-effort diagnostic owner metadata
```

**Common causes:** Long-running operation under lock, many agents competing, hung process, slow filesystem (network mount).

**Solutions:**
- If holder is alive and working → wait 30-60s, retry
- If holder is hung → stop the owning `§BRAND_BINARY_NAME§` process; do not delete lock files based on PID metadata alone
- If high contention → reduce parallel agents (1-3 per role)
- If slow filesystem → `df -T §BRAND_PROJECT_DIRNAME§/`, move to local SSD

### owner metadata is diagnostic

§BRAND_NAME_TITLE§ uses `flock` for mutual exclusion. The `.lock` file and
`.lock.owner.json` metadata may remain after a successful operation; their
presence alone does not mean a lock is held. Owner metadata can be stale or
ambiguous across process namespaces, so §BRAND_NAME_TITLE§ does not use it to release locks.

**Manual cleanup is rarely appropriate:** only remove lock metadata after
confirming no `§BRAND_BINARY_NAME§` process is running and no process holds the flock.

### disk_full — no space left on device

```bash
df -h §BRAND_PROJECT_DIRNAME§/        # Check disk space
df -i §BRAND_PROJECT_DIRNAME§/        # Check inodes
du -sh .worktrees/* # Find large worktrees
```

**Free space:** Clean merged worktrees, archive old log files, `git gc --aggressive --prune=now`.

### permission — permission denied

```bash
ls -la §BRAND_PROJECT_DIRNAME§/state.yaml.lock*    # Check lock file permissions
ls -ld §BRAND_PROJECT_DIRNAME§/                     # Check directory permissions
stat §BRAND_PROJECT_DIRNAME§/state.yaml             # Check ownership
```

**Common causes:** Lock file owned by different user (ran as root, now as user), NFS mount with restrictive permissions, SELinux/AppArmor blocking.

**Fix:** `sudo chown -R $(whoami):$(id -gn) §BRAND_PROJECT_DIRNAME§/`

### filesystem — I/O error

```bash
dmesg | grep -i error | tail -20   # Filesystem errors
touch §BRAND_PROJECT_DIRNAME§/test.txt && rm §BRAND_PROJECT_DIRNAME§/test.txt  # Test write
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

**Fix the invariant** (edit `§BRAND_PROJECT_DIRNAME§/state.yaml` directly):
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
§BRAND_BINARY_NAME§ get tasks --format table   # Shows depends_on for each task
# Example cycle: task-1 → task-2 → task-3 → task-1
```

**Break cycle** (edit `§BRAND_PROJECT_DIRNAME§/state.yaml` directly):
```yaml
# Find task-3 and remove "task-1" from its depends_on list
```

### Spec file not found

```bash
# Option 1: Create the spec file
mkdir -p specs && vi specs/vision.md

# Option 2: Update spec reference in state (edit §BRAND_PROJECT_DIRNAME§/state.yaml directly)
# Find the task entry and set:
#   spec_ref: docs/requirements.md
```

---

## Worktree Issues

### Worktree directory not found

**Error:** `INVALID: IMPLEMENTING task <task-id> has worktree=.worktrees/<task-id> but directory does not exist`

**Recreate:** `git worktree add .worktrees/<task-id> -b task/<task-id> <base-commit>`

*(Replace `<base-commit>` with the task's `base_commit` value from `§BRAND_BINARY_NAME§ get tasks <task-id>`.)*

**Or reset task** (if work was lost — edit `§BRAND_PROJECT_DIRNAME§/state.yaml` directly):
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
§BRAND_BINARY_NAME§ recover-task <task-id> --force

# Option 2: Delete and recreate (task must be in terminal state)
§BRAND_BINARY_NAME§ wt-delete <task-id>
§BRAND_BINARY_NAME§ wt-create <task-id>
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

*(Replace `<base-commit>` with the task's `base_commit` value from `§BRAND_BINARY_NAME§ get tasks <task-id>`.)*

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

See [RECIPES.md](../docs/RECIPES.md#integration-failure-recovery) for the full recovery workflow.

**Quick diagnosis:**
```bash
§BRAND_BINARY_NAME§ get tasks task-1                    # Check failure details
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

**Solution:** Update to latest `§BRAND_BINARY_NAME§` binary which includes empty-line guards.

### INVALID STATE alert

**Error:** `🚨 INVALID STATE: Agent coder-1 has status WORKING but lease expired`

**Common causes:** Agent lease expired (task took longer than `lease_duration`), task status invariant violation.

**Fix:** Extend the lease or increase `config.lease_duration`. See [CONFIGURATION.md](CONFIGURATION.md#configuration-matrix) for tuning parameters.

### MISSING ROLE alert

**Error:** `⚠️ MISSING ROLE: no registered agent for role code-planner`

**Cause:** Tasks are immediately claimable by a pipeline role, but no live agent for that runtime role is registered.

**Fix:** In both interactive and headless TUI modes, §BRAND_NAME_TITLE§ automatically attempts `repair-agent-pool` by default. Successful automatic spawns are logged as informational `auto_repair_agent_spawned` entries in `log.yaml` and do not raise alerts; spawn failures raise `AUTO REPAIR FAILED`. Set `§BRAND_ENV_PREFIX§_AUTO_REPAIR_AGENT_POOL=0` (or `false`/`no`) to disable automatic repair. For manual repair, run `§BRAND_BINARY_NAME§ repair-agent-pool --dry-run` to preview the repair, then `§BRAND_BINARY_NAME§ repair-agent-pool --cli <name>` to spawn the missing roles. If `--cli` is omitted, §BRAND_NAME_TITLE§ uses role-specific config (`config.default_doer_cli` for doers and orchestrators, `config.default_reviewer_cli` for reviewers), then role-specific env (`§BRAND_ENV_PREFIX§_DEFAULT_DOER_CLI` for doers and orchestrators, `§BRAND_ENV_PREFIX§_DEFAULT_REVIEWER_CLI` for reviewers), then `config.default_cli`, then `§BRAND_ENV_PREFIX§_DEFAULT_CLI`, then `claude`.

`repair-agent-pool` counts live usable capacity, not only registered role presence. A live agent marked degraded for its current process epoch is ignored for capacity and shown in `§BRAND_BINARY_NAME§ status`, `§BRAND_BINARY_NAME§ get agents`, and `repair-agent-pool --dry-run`. If the degraded supervisor exits and unregisters, the health marker remains visible as degraded capacity context so operators can see why the role is being repaired. Use `§BRAND_BINARY_NAME§ clear-agent-degraded <agent-id>` after manual recovery if the marker should be cleared before the next successful claim.

Avoid running multiple `§BRAND_BINARY_NAME§ tui --headless` processes for the same project. Auto-repair backoff is per watcher process. Registration and `max-instances` checks are the final safety net, but duplicate watchers can briefly race before a newly spawned agent registers.

---

## Initialization Issues

### Error: §BRAND_PROJECT_DIRNAME§ already exists

**Solutions:**
1. **Continue with existing state** — just start the agents.
2. **Reset completely:** `rm -rf §BRAND_PROJECT_DIRNAME§ .worktrees && §BRAND_BINARY_NAME§ init "New goal"` (requires prior `§BRAND_BINARY_NAME§ setup`)

### Symlink creation fails on Windows

**Error:**
```
Warning: failed to create CLAUDE.md symlink: symlink ... A required privilege is not held by the client.
```

**Cause:** Windows requires either Developer Mode or Administrator privileges to create symbolic links. §BRAND_NAME_TITLE§ uses symlinks in several places, and the impact depends on which link failed:

| Command | Links created | Impact if missing |
|---------|--------------|-------------------|
| `§BRAND_BINARY_NAME§ init` | Repo-root contract files (`CLAUDE.md`, `AGENTS.md`, `GEMINI.md` → `~/§BRAND_GLOBAL_DIRNAME§/CORE.md`) | Agents cannot find the behavioral contract from the project directory |
| `§BRAND_BINARY_NAME§ setup` | Skill links in CLI config dirs (`~/.claude/skills/`, `~/.codex/skills/`, etc.) → `~/§BRAND_GLOBAL_DIRNAME§/skills/` | Agents cannot load skills (debugging, testing, code review, etc.) |
| `§BRAND_BINARY_NAME§ setup` | CLI-specific prompt files (e.g. `~/.vibe/prompts/§BRAND_BINARY_NAME§.md`) | CLI prompt activation fails for the affected CLI |

**Solutions (pick one):**

1. **Enable Developer Mode** (recommended): Settings → System → For developers → toggle Developer Mode on. Then re-run `§BRAND_BINARY_NAME§ setup` and `§BRAND_BINARY_NAME§ init`.

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

See [PERFORMANCE.md](../docs/PERFORMANCE.md) for tuning parameters, benchmark targets, and lock metric interpretation.

### Agents not responding to changes (30s+ delay)

fsnotify may not work on network filesystems (NFS, SMB). Agents fall back to 30s polling. **Fix:** Use local filesystem, or accept the delay.

Also check: system mode is RUNNING (`§BRAND_BINARY_NAME§ get config.mode`), sprint status is IN_PROGRESS (`§BRAND_BINARY_NAME§ get sprint.status`).

### Slow state file reads (>50ms)

```bash
ls -lh §BRAND_PROJECT_DIRNAME§/state.yaml   # Check size (target <1MB)
time §BRAND_BINARY_NAME§ validate          # Time a read
```

**Common causes:** Large state file, slow disk, cache thrashing from external modifications (editors with auto-save, Dropbox syncing `§BRAND_PROJECT_DIRNAME§/`).

**Fix:** Archive completed tasks, use SSD, avoid external modifications.

### Slow task claims (5-10s)

Git worktree operations slow on large repos. **Fix:** Use SSD, consider sparse checkout (`git config core.sparseCheckout true`).

### Validation takes too long (>5s)

Large task list or complex dependency graph. **Fix:** Archive old tasks, `§BRAND_BINARY_NAME§ validate --skip-spec-check` on slow filesystems.

---

## Debugging Techniques

### Verbose output

```bash
§BRAND_BINARY_NAME§ -v validate              # Timing, detailed errors, internal ops
§BRAND_BINARY_NAME§ -v claim-task task-1 coder-1
```

### Inspect state

```bash
§BRAND_BINARY_NAME§ get tasks --format table   # All task statuses
§BRAND_BINARY_NAME§ get tasks task-1           # Single task detail
§BRAND_BINARY_NAME§ get agents --format table  # All agents
§BRAND_BINARY_NAME§ get metrics                # Sprint metrics
§BRAND_BINARY_NAME§ status                     # Full dashboard
```

### Review logs

```bash
cat §BRAND_PROJECT_DIRNAME§/log.yaml              # Activity log
cat §BRAND_PROJECT_DIRNAME§/alerts.log            # Alerts
ls §BRAND_PROJECT_DIRNAME§/agent-prompts/         # Generated agent prompts
cat §BRAND_PROJECT_DIRNAME§/agent-prompts/coder-1-*.txt  # What the agent was told
```

### Monitor locks

```bash
ls -la §BRAND_PROJECT_DIRNAME§/state.yaml.lock*       # Lock and diagnostic metadata files
lsof §BRAND_PROJECT_DIRNAME§/state.yaml.lock          # Process holding the kernel flock, if visible
cat §BRAND_PROJECT_DIRNAME§/state.yaml.lock.owner.json # Best-effort owner metadata
```

### Watch real-time

```bash
watch -n 2 '§BRAND_BINARY_NAME§ get tasks --format table'
watch -n 2 '§BRAND_BINARY_NAME§ get agents --format table'
watch -n 5 '§BRAND_BINARY_NAME§ status'
watch -n 5 'ls -la .worktrees/'
```

### Debug report

```bash
cat > debug-report.txt <<EOF
=== §BRAND_NAME_TITLE§ Debug Report ===
Version: $(§BRAND_BINARY_NAME§ version)
State Validation: $(§BRAND_BINARY_NAME§ validate 2>&1)
Tasks: $(§BRAND_BINARY_NAME§ get tasks --format table 2>&1)
Agents: $(§BRAND_BINARY_NAME§ get agents --format table 2>&1)
Worktrees: $(git worktree list)
Recent Alerts: $(tail -50 §BRAND_PROJECT_DIRNAME§/alerts.log 2>/dev/null)
Processes: $(ps aux | grep §BRAND_BINARY_NAME§)
Lock Status: $(ls -la §BRAND_PROJECT_DIRNAME§/state.yaml.lock* 2>&1)
EOF
```

---

## Recovery Procedures

### Agent crashed with IMPLEMENTING task (usage limit, OOM, etc.)

When a coder agent crashes (usage limit, OOM, SIGKILL) while a task is IMPLEMENTING:

**Recover by task ID** (recommended — you usually know the task, not the agent):
```bash
§BRAND_BINARY_NAME§ recover-task <task-id>
§BRAND_BINARY_NAME§ recover-task <task-id> --force         # if agent PID is alive, or task is absent from state and only git artifacts remain
§BRAND_BINARY_NAME§ recover-task <task-id> --fresh         # explicitly discard task worktree/branch and recreate from integration
§BRAND_BINARY_NAME§ recover-task <task-id> --fresh --force # required for destructive reset while a claimant PID is alive
```

**Recover by agent ID** (when you know the agent):
```bash
§BRAND_BINARY_NAME§ recover-agent <agent-id> --cli claude   # recover + respawn
§BRAND_BINARY_NAME§ recover-agent <agent-id>                # recover only
```

`recover-task` preserves or reattaches coherent task work by default: the branch
must exist, the worktree must be healthy and clean, and submitted/reviewing tasks
must still have `review_commit == worktree HEAD`. Use `--fresh` only when
discarding the task branch/worktree is intentional. `--force` is separate: it
bypasses live-PID checks, and it enables git-only cleanup when the task is no
longer in state.

`recover-agent` performs full agent cleanup: release claim, remove worktree, and
delete the agent from state. Both commands are idempotent, but neither should be
used to unblock a `BLOCKED` task; after substrate recovery, use `§BRAND_BINARY_NAME§
unblock-task` for the guarded `BLOCKED` -> claimable transition.

**Diagnosis (if needed):**
```bash
§BRAND_BINARY_NAME§ get tasks <task-id>          # Check status, assigned_to, lease_expires
§BRAND_BINARY_NAME§ get agents --format table    # Check agent status and lease
```

<details>
<summary>Manual recovery (granular control)</summary>

**If lease has expired** (current time > `lease_expires`):

```bash
§BRAND_BINARY_NAME§ release-claim <task-id> --role coder
§BRAND_BINARY_NAME§ agent coder
```

**If lease has NOT expired:**

```bash
§BRAND_BINARY_NAME§ delete agent <agent-id> --force
§BRAND_BINARY_NAME§ release-claim <task-id> --role coder
§BRAND_BINARY_NAME§ agent coder
```
</details>

### Full state reset (nuclear option)

```bash
cp -r §BRAND_PROJECT_DIRNAME§ §BRAND_PROJECT_DIRNAME§.backup.$(date +%Y%m%d-%H%M%S)
rm -rf §BRAND_PROJECT_DIRNAME§
§BRAND_BINARY_NAME§ setup          # skip if ~/§BRAND_GLOBAL_DIRNAME§/ already exists
§BRAND_BINARY_NAME§ init "Goal description"
# Manually migrate in-progress work from backup if needed
```

### Agent stuck in WORKING state

```bash
# Recover by task
§BRAND_BINARY_NAME§ recover-task <task-id>
§BRAND_BINARY_NAME§ recover-task <task-id> --fresh    # only if stale work should be discarded

# Or recover by agent
§BRAND_BINARY_NAME§ recover-agent coder-1

# Then restart
§BRAND_BINARY_NAME§ agent coder --agent-id coder-1
```

---

## Getting Help

1. **Check alerts:** `cat §BRAND_PROJECT_DIRNAME§/alerts.log`
2. **Check activity:** `cat §BRAND_PROJECT_DIRNAME§/log.yaml`
3. **Validate state:** `§BRAND_BINARY_NAME§ validate`
4. **Watch live:** `§BRAND_BINARY_NAME§ tui`
5. **Generate debug report** (see above)
