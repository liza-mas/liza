# I-13 guard fixture (decision D44)

Scenario: `TestOracleA9OrchestratorPlans/i13-guard`, an AC2 adapter. The
write-callback comparison in `orchestratorStrategy.PreWork`
(`strategy_orchestrator.go`) differs from its gate only when a checkpoint is
replaced between PreWork's snapshot and its write, which no process outside the
supervisor can interleave, and the strategy is unexported. The adapter pins the
callback's decision through the exported pure predicate
`ops.SameResumedTransitionCheckpoint` (`internal/ops/mode_change.go`).

`sprints.yaml` is hand-authored, not derived from a binary run: it holds only
the `models.Sprint` fields that predicate reads (`status`, `id`, `number`,
`checkpoint_trigger`, `timeline.checkpoint_at`) in the shape `state.yaml` stores
them, so the same checkpoint gives true and a newer checkpoint, a different
trigger, a different sprint or a sprint checkpointed again give false (the
trigger is kept). The values come from the starting commit's sources only;
the golden is generated from the starting commit through `write-new`.
