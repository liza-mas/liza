# Derived CODE_TO_REVIEW fixture

Scenario: a `coder` run on a `DRAFT_CODE` task (`i04-success` state, common
recipe, stub provider) whose session commits one file in the task worktree,
then runs `write-checkpoint` and `submit-for-review`, then `stop`.
The binary that produced it is the starting commit
`96c9ad69aee983ce2b929dd681b6ba43bf920535`, built outside this repository:

```text
git archive 96c9ad69aee983ce2b929dd681b6ba43bf920535 | tar -x -C <tmp>/src
make -C <tmp>/src sync-embedded
go -C <tmp>/src build -o <tmp>/liza ./cmd/liza
```

- `derive-stub.json` is the stub script of the run: it waits for
  `.liza/oracle-release`, then issues the three commands. The driver, not the
  stub, wrote `review-feature.txt` into `.worktrees/task-1` and committed it
  with the fixed identity and dates of the scenario environment, because the
  stub's working directory is the project root. The commit is the recipe in
  `recipe.json`; its SHA is `review_commit` of the state.
- `state.yaml` is the project state after the run. Relocation edits only:
  `config.mode` restored from `STOPPED` (set by the final `stop`) to `RUNNING`,
  and the `mode_changed_at`/`mode_changed_by` lines dropped. Timestamps,
  digests and receipts are the run's own and are inputs, not asserted values.
- `state-unrecorded-worktree.yaml` is the same state without the task's
  `worktree` path. The reviewer claim refuses a task that records a worktree
  whose directory is absent (`integration_failed`, review boundary mismatch), so
  I-09 and I-10 reach worktree recovery and block only through this form.

The fixture is an input and is never regenerated. The materialiser replays
`recipe.json` on the common fixture commit and refuses a different SHA.
