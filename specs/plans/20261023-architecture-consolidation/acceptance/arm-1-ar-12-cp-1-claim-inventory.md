# Claim oracle inventory (arm-1-ar-12-cp-1)

Branch-to-proof map for the claim paths this task extracted. Public-surface
proof is a committed `internal/oracle/a3/lifecycle` case (json, human and state
goldens under `claim-task/`, written by `regen -mode write-new` from the
starting tree); an internal-only schedule is paired with a phase-controlled
`internal/ops` test. Existing goldens and fixtures are unchanged; the added
`maskBoundaries` case flag and `discoverBoundaries` helper are additive and
mask nothing in existing cases.

| Claim branch | Public-surface oracle | Phase-controlled ops test |
|---|---|---|
| fresh claim, success / not claimable | `TestOracleClaimTask` (delivered L1) | `TestClaimTask_ReadyTask` |
| integration-fix success / worktree missing | `TestOracleClaimTaskVariants` `integration-fix-success`, `integration-fix-refusal-worktree-missing` | `TestClaimTask_IntegrationFailed` |
| preserved-initial success / unresolvable base block | `preserved-initial-success`, `preserved-initial-refusal-base-unresolvable` | `TestClaimTask_PreservedInitial*` |
| rejected success | `rejected-success` | `TestClaimRejectedTask` |
| attempt rollover / BLOCKED escalation | `rejected-refusal-review-budget-new-attempt`, `rejected-refusal-review-budget-blocked` | `TestClaimTask_IterationCapAttempt1_TriggersNewAttempt`, `TestClaimTask_ReviewCapAttempt2_TriggersBlocked` |
| stranded takeover / pinned-transition refusal | `stranded-takeover-success`, `stranded-takeover-refusal-pinned-transition` | `claim_task_stranded_test.go` |
| exact receipt replay / stale expected transition | `receipt-replay`, `receipt-stale-transition` | `TestOwnershipLifecycleClaimReplayAfterReleaseAndReclaim` |
| setup (post-worktree command) failure | not observable through the oracle fixtures (no project command); A9 `TestOracleA9DoerClaim*` cover ordinary claim | `TestClaimTask_PostWorktreeCmdFailure*` |
| generation replaced / boundary changed before assignment | internal schedule | `claim_task_phases_test.go`, `TestClaimGenerationFence`, `TestClaimValidationLifecycleGenerationChangesBeforeAssignment` |
| losing-claimant cleanup / preserved work survives | internal schedule | `claim_task_phases_test.go` |
| preserved completion fence, both integration schedules | internal schedule | `TestClaimTask_PreservedInitialIntegrationMoveBeforeCriticalSectionFailsClosed`, `...MoveAfterEqualityWaitsForAssignment` |
