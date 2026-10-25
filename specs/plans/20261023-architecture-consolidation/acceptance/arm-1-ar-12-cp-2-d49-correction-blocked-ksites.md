# K1 site table for the D49 blocked-episode correction

Starting lines are at `99843b04e` (the plan's RP5 probe). Resulting lines are at the task's
final production commit. Every site stays inside the callback that already contained it,
in the same order against the checks around it; no Git call, lock or observation was added,
removed, moved out, retried, cached or duplicated.

| Site | Call | Starting line | Resulting file:line | Enclosing callback |
|---|---|---|---|---|
| B1 | `GetCommitSHA(*task.BaseCommit)` in `automaticStateLockHoldGate` | `state_lock_holds.go:103` | `state_lock_holds.go:103` (file untouched) | the unblock restore body `unblockCall.mutate` (`unblock_transaction.go:212`, via `lifecycleMutation(...)(call.mutate)`, `unblock_task.go:184`) and the `ModifyWithAgentAuthority` callback of `recordStateLockHoldRefusal` (`state_lock_holds.go:208`); the unlocked advisory calls (`state_lock_holds.go:168`, the unblock dry run) stay unlocked |
| B2 | `ValidateWorktreeHealth(task.ID)` in the same gate | `state_lock_holds.go:106` | `state_lock_holds.go:106` | same as B1 |
| B3 | `GetWorktreeBranch(...)` in the same gate | `state_lock_holds.go:109` | `state_lock_holds.go:109` | same as B1 |
| B4 | `ValidateWorktreeHealth(task.ID)` in `revalidateUnblockRebase` (call site after the unmet-dependency check, before `task.BaseCommit` update) | `unblock_task.go:683` (call `:314`) | `unblock_task.go:413` (call `unblock_transaction.go:281`, `unblockCall.recordAudit` from `mutate`) | the final `mutate` transaction (`unblock_task.go:184`) |
| B5 | `GetWorktreeBranch(...)` in `revalidateUnblockRebase` | `unblock_task.go:686` | `unblock_task.go:416` | same as B4 |
| B6 | `GetWorktreeHEAD(task.ID)` in `revalidateUnblockRebase` | `unblock_task.go:693` | `unblock_task.go:423` | same as B4 |
| B7 | `statevalidate.ValidateCandidate(state, bb.ReadSnapshot, projectRoot, false, os.Stderr)` on the assess reconcile path, after history and hold changes, before `CompleteLifecycleRequest` | `assess_blocked.go:456` (callback `:307`) | `assess_blocked_transaction.go:230` (`assessBlockedCall.record`, called from `mutate`) | the `lifecycleMutation(call.bb, authority)(call.mutate)` callback (`assess_blocked.go:284`) |
| B8 | `statevalidate.ValidateCandidate(..., false, ...)`, last step of the release callback | `release_blocked_chain.go:85` | `release_blocked_chain.go:85` (file untouched) | the `bb.Modify` callback (`release_blocked_chain.go:49`) |

Non-B Git effects stay outside `Modify`: `snapshotUnblockRebase`, the untracked-overwrite check,
the rebase, abort and post-rebase HEAD read (`maybeRebaseTaskBeforeUnblock`) and
`markUnblockRebaseConflict`.

Observed contract differences from the plan text, pinned as found:

- P7: `ValidateCandidate` refuses only violations the baseline lacks. A ref absent from both
  trees is a pre-existing violation and is reported as a stderr warning, not a refusal; neither
  assess reconcile nor release can introduce an artifact-ref violation.
- P5: the automatic gate's `validateUnblockDirectDependencies` check is unreachable, because
  every terminal non-MERGED dependency is already named `dependencies_pending` first.
- P9 holds the state lock around the first (preparation) transaction only: the lifecycle
  mutation hook is keyed by blackboard pointer and a retry-context request uses a derived
  blackboard, so no test seam exists between preparation and final for that path.
