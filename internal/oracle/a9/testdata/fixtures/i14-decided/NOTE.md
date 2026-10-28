# I-14 MERGED plan fixture (decision D44)

Scenario: `TestOracleA9OrchestratorPlans/i14-decided`. `state.yaml` is a
hand-authored input (D44; A4 D4 precedent), never regenerated. It stands for the
state the starting commit's pipeline leaves after a `code-planning-pair` task
reaches `MERGED`. The starting producer operations and the source of each
non-trivial field:

| Field | Starting producer | Source of the value |
|---|---|---|
| `status: MERGED`, `role_pair: code-planning-pair` | `ops.MergeWorktree` (`internal/ops/wt_merge.go`) pipeline transition to MERGED | pipeline `code-planning-pair` of `fixtures/common/pipeline.yaml` |
| `approved_by` | `ops.SubmitVerdict` APPROVED (`internal/ops/submit_verdict.go`, derived field) | fixed reviewer ID |
| `merge_commit` | `ops.MergeWorktree` (`t.MergeCommit`) | placeholder SHA: no merge ran, the field is only read as present |
| `output` (one entry) | the plan author's `submit-for-review` payload | the task's planned output item; `done_when`, `scope`, `spec_ref` are fixed text |
| `handoff_events` (`submission`, `completion`) | `ops.SubmitForReview` (`internal/ops/submit_review.go`) and `ops.MergeWorktree` (`internal/ops/wt_merge.go`) append them | fixed agents and dates; kept so the MERGED task has the audit shape `statevalidate` checks, not asserted by the scenario |
| `history: []` | the transition history those operations append | omitted: no asserted behaviour reads it |
| `sprint`, `config`, `goal` | common fixture state | `fixtures/common/state.yaml` shape with the plan planned in the sprint |

Acceptance: the starting commit `96c9ad69aee983ce2b929dd681b6ba43bf920535`,
built outside this repository (`git archive`, `make sync-embedded`,
`go build -buildvcs=false ./cmd/liza`), ran `liza validate --json` on this state
in a project holding the common recipe files and `pipeline.yaml`, and answered
`{"ok":true,"result":{"valid":true}}`.
