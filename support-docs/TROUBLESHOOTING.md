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

### Agent exits immediately: worktree setup failed

An agent that stops claiming right after start, with `agent_health` recording
reason `claim_worktree_setup_failed`, hit a failing `post_worktree_cmd`. Worktree
setup fails closed, so the agent degrades instead of working in an unprepared
checkout (ADR-0117).

**Diagnosis:**
```bash
§BRAND_BINARY_NAME§ get agent_health --json   # reason, last_error, recover_hint
§BRAND_BINARY_NAME§ get config.post_worktree_cmd --json
```

**Fix:** the command's output is not recorded (it can carry secrets §BRAND_NAME_TITLE§
cannot mask), so run the command named in `recover_hint` inside the worktree it names,
fix whatever fails, then run `§BRAND_BINARY_NAME§ clear-agent-degraded <agent-id>`
and restart the agent. Fresh doer claims abort before the claim is recorded, so
the task keeps its prior status; a resumed task stays assigned to its agent and
is picked up again on restart; reviewer claims are released back to the
reviewable status, so no review work is lost. Do not work around it by unsetting
`post_worktree_cmd` — that restores the silent mode this check replaced.

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
For shared locks, owner metadata identifies only the most recent acquirer, not
the complete set of current holders.

Project cleanup also uses a project lifecycle lock stored in Git metadata.
Agent registration and worktree provisioning or recovery hold this lock while
they establish resources, including while configured post-worktree setup and
indexing run. Cleanup waits up to 30 minutes for those operations rather than
deleting a worktree during provisioning. If that wait times out, let the named
lifecycle operations finish and retry cleanup.

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

`repair-agent-pool` counts live usable capacity, not only registered role presence. For reviewer work, capacity requires a live usable agent that can pass the existing claim filters for the task, including prior-approval and configured provider-diversity eligibility. A live agent marked degraded for its current process epoch is ignored for capacity and shown in `§BRAND_BINARY_NAME§ status`, `§BRAND_BINARY_NAME§ get agents`, and `repair-agent-pool --dry-run`. If the degraded supervisor exits and unregisters, the health marker remains visible as degraded capacity context so operators can see why the role is being repaired. Use `§BRAND_BINARY_NAME§ clear-agent-degraded <agent-id>` after manual recovery if the marker should be cleared before the next successful claim.

Avoid running multiple `§BRAND_BINARY_NAME§ tui --headless` processes for the same project. Auto-repair backoff is per watcher process. Registration and `max-instances` checks are the final safety net, but duplicate watchers can briefly race before a newly spawned agent registers.

---

## Initialization Issues

### Existing initialization directories detected

When `§BRAND_PROJECT_DIRNAME§/` or `.worktrees/` exists, full workspace init
uses the same cleanup flow as `§BRAND_BINARY_NAME§ cleanup`: it lists runtime
directories, owned task worktrees, and associated task branches before asking
to delete them. Cleanup permanently removes runtime state, uncommitted
worktree files, and the listed task branches.

- To continue with the existing state, answer `n` and start the agents normally.
- To clean without re-initializing, run `§BRAND_BINARY_NAME§ cleanup`.
- To reset, answer `y`. For non-interactive cleanup or initialization, pass
  `--yes` to authorize the displayed deletion explicitly.
- Stop all agents first. Cleanup refuses live agents and any registered
  worktree that does not match `.worktrees/<task-id>` on `task/<task-id>`.

### Symlink creation fails on Windows

**Error:**
```
Warning: failed to create CLAUDE.md symlink: symlink ... A required privilege is not held by the client.
```

**Cause:** Windows requires either Developer Mode or Administrator privileges to create symbolic links. §BRAND_NAME_TITLE§ uses symlinks in several places, and the impact depends on which link failed:

| Command | Links created | Impact if missing |
|---------|--------------|-------------------|
| `§BRAND_BINARY_NAME§ init` | Provider contract links to `~/§BRAND_GLOBAL_DIRNAME§/CORE.md`: documented global instruction paths for global-first providers, repo-root files for repo-only providers | The affected provider cannot discover the behavioral contract from its active instruction path |
| `§BRAND_BINARY_NAME§ setup` | Skill links in CLI config dirs (`~/.claude/skills/`, `~/.codex/skills/`, etc.) → `~/§BRAND_GLOBAL_DIRNAME§/skills/` | Agents cannot load skills (debugging, testing, code review, etc.) |
| `§BRAND_BINARY_NAME§ setup` | CLI-specific prompt files (e.g. `~/.vibe/prompts/§BRAND_BINARY_NAME§.md`) | CLI prompt activation fails for the affected CLI |

**Solutions (pick one):**

1. **Enable Developer Mode** (recommended): Settings → System → For developers → toggle Developer Mode on. Then re-run `§BRAND_BINARY_NAME§ setup` and `§BRAND_BINARY_NAME§ init`.

   Same effect, no UI: merge a `.reg` file setting the registry value that toggle writes, then elevate to apply it (a UAC prompt merging one key is still a machine-wide change, unlike installing software as a different elevated account — see below). Takes effect immediately, no sign-out needed.

   ```reg
   Windows Registry Editor Version 5.00

   [HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\AppModelUnlock]
   "AllowDevelopmentWithoutDevLicense"=dword:00000001
   ```

2. **Run elevated**: Open your terminal as Administrator, then re-run the command.
   Check first that elevation keeps you as the same account — see the next
   section, where it does not.

### Elevation runs as a different account

**Symptoms** — several, and none of them names the cause:

```
# after an elevated `setup`
§BRAND_NAME_TITLE§ global config written to C:\Users\you\§BRAND_GLOBAL_DIRNAME§
# but from your normal session
Test-Path C:\Users\you\§BRAND_GLOBAL_DIRNAME§    ->  Access denied
Get-ChildItem ~/§BRAND_GLOBAL_DIRNAME§  ->  empty

# and an elevated `init`
Error: failed to determine project root: ... exit status 128
fatal: detected dubious ownership in repository at '...'
```

**Cause:** on a domain-joined machine, the administrator you elevate to may be a
**local** account that merely shares your short name. Windows gives it its own
profile — `C:\Users\you` next to `C:\Users\you.DOMAIN` — so `~` means something
different on each side, and Git sees a repository owned by someone else.

Compare both sides; different prefixes mean different accounts:

```powershell
whoami   # normal session,  e.g. PROGINOV\you
whoami   # elevated session, e.g. PORT_MACHINE\you
```

**Preferred fix — stop needing elevation.** Grant the working account the
privilege the symlinks require, and everything runs in the right profile with no
juggling: `secpol.msc` → Local Policies → User Rights Assignment → **Create
symbolic links** → add your account → sign out and back in. Verify with
`whoami /priv | Select-String Symbolic`. Group policy may revert this on refresh.

**Otherwise — elevate, but redirect.** §BRAND_NAME_TITLE§ resolves `~` from
`HOME` before falling back to the account's profile, so `HOME` is the lever. Git
needs telling separately that the repository is trustworthy for this account.

```powershell
$env:HOME = "C:\Users\you.DOMAIN"
git config --global --add safe.directory C:/path/to/your/repo
§BRAND_BINARY_NAME§ setup --claude
cd C:\path\to\your\repo
§BRAND_BINARY_NAME§ init --claude
```

- Keep it all in one shell session: `HOME` does not persist.
- Give Git the **resolved** path it prints in the error, not a junction pointing
  at it.
- With `HOME` redirected, `git config --global` writes into *your* `.gitconfig`,
  not the administrator's. That is usually what you want — later elevated
  sessions will read it back — and the entry is inert for you, since you own the
  repository. To keep your config untouched, point `GIT_CONFIG_GLOBAL` at a
  scratch file first; your `user.name` and `user.email` will then be unread,
  which is harmless for `init`.
- Files land in your profile but are created by the other account. ACL
  inheritance normally still grants you full control; if a later `--force` write
  fails, check with `icacls`.

**Do not elevate without redirecting `HOME`.** `init` would create `CLAUDE.md`
pointing at the *other* account's `~/§BRAND_GLOBAL_DIRNAME§/CORE.md` — a symlink
that looks correct, resolves to a directory your session cannot read, and fails
later at a point far from its cause.

### init reports "repo root has existing CLAUDE.md" and links globally

`§BRAND_BINARY_NAME§ init` only creates the contract link at the repository root
when that name is free. If something already occupies it, init falls back to the
global location and says so:

```
C:\Users\you\.claude\CLAUDE.md → C:\Users\you\§BRAND_GLOBAL_DIRNAME§\CORE.md (repo root has existing CLAUDE.md)
```

That fallback works, but it is not the same thing: the contract then applies to
every project you open, not this one.

A common cause on Windows is a contract link left behind by a WSL or MSYS
session, which points at a POSIX home that does not exist natively:

```bash
ls -la CLAUDE.md
lrwxrwxrwx ... CLAUDE.md -> /home/you/§BRAND_GLOBAL_DIRNAME§/CORE.md
```

The entry exists, so init sees the name as taken, while nothing can read it —
`cat CLAUDE.md` reports "No such file or directory" and PowerShell shows a
zero-length file with no target. Remove the dangling link and re-run init:

```powershell
Remove-Item -LiteralPath CLAUDE.md -Force
```

These files are listed in `.gitignore`, so removing one costs nothing in the
repository.

### Hooks do nothing, or bash reports "No such file or directory" on Windows

**Error:**
```
/c/Users/you/project/.claude/hooks/enforce-init.sh: No such file or directory
```
or a hook that silently never fires.

**Cause:** `bash` on PATH is the WSL launcher at `C:\Windows\System32\bash.exe`,
not Git for Windows. The WSL launcher cannot see `C:/...` paths — it expects
`/mnt/c/...` — so every hook invoked by its native path fails.

**Check:**
```powershell
(Get-Command bash).Source
```

**Fix:** Put Git for Windows ahead of `system32` on PATH. A user-level PATH entry
cannot win, because the machine PATH is evaluated first, so the Git `bin`
directory has to be prepended to the **machine** PATH (an elevated change):

```powershell
# Run as Administrator. Adjust the path to your Git installation.
$git = "$env:LOCALAPPDATA\Programs\Git\bin"
$machine = [System.Environment]::GetEnvironmentVariable('PATH', 'Machine')
[System.Environment]::SetEnvironmentVariable('PATH', "$git;$machine", 'Machine')
```

Open a new terminal afterwards, and confirm `bash --version` reports a Git
version rather than a Linux distribution.

### Hooks fail with `$'\r': command not found` on Windows

**Cause:** the shell scripts were checked out with CRLF line endings. `bash` reads
the carriage return as part of the command.

The repository pins `eol=lf` in `.gitattributes`, so a fresh clone is unaffected.
A clone made **before** that file existed keeps its CRLF working tree
indefinitely: `git status` looks clean, because Git compares normalized content
and hides the difference.

**Check:** `git ls-files --eol | grep w/crlf` — anything listed is CRLF on disk.

**Fix:** renormalize the working tree from the index.

```bash
git config core.autocrlf false
git ls-files --eol | grep 'w/crlf' | cut -f2 > /tmp/crlf-files
xargs -a /tmp/crlf-files -d '\n' rm -f
git checkout -- .
```

The index is never modified, so the tree can be restored with `git checkout -- .`
at any point. Commit any pending work first: this rewrites tracked files.

### A flag value arrives empty, shifted, or missing its quotes on Windows

**Cause:** Windows PowerShell 5.1 rewrites a native command's arguments before
the process sees them. Two rewrites lose content with no error, so the command
runs on something other than what was written:

- An **empty** value disappears entirely, taking the next flag's place.
  `--reason $r --agent-id a1` with `$r` empty reaches the process as
  `--reason --agent-id a1`, so the parser reads the reason as `--agent-id` and
  `a1` becomes a stray positional argument.
- **Double quotes inside a value are stripped.** `He said "no"` arrives as
  `He said no`, so a rejection reason quoting code or an error message is
  altered silently.

Spaces, accents, line breaks and a leading `--` all survive intact; only these
two cases lose content. PowerShell 7 (`pwsh`) has neither behaviour.

**Check:** run the command with `--reason` last. If the value was dropped, the
following flag shows up as the reason.

**Fix:** attach the value to the flag so an empty string still produces a token,
and escape embedded quotes with a backslash:

```powershell
$reason = 'the guard rejects \"draft\" states'
§BRAND_BINARY_NAME§ submit-verdict task-1 REJECTED --reason="$reason"
```

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
