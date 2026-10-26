# A7 K7 Inventory

Evidence for `arm-1-ar-17-cp-0` Task 11 (`arm-1-ar-17-cp-0-code-10`): the complete K7 occurrence-to-owner
classification of every A7 production file, the substitutions applied, the owner gaps, the later-area adoption
sites and the reconciliation of the plan's K7 vocabulary rows (`a7-k7`). Source of the rules is the plan section
"K7 Vocabulary" and the A6 inventory precedent (`a6-k7-inventory.md`, rules R1–R5).

## Coding HEAD

- Integration HEAD at claim (A1, A3–A6 delivered; A7 Tasks 2–10 merged, the last being `f71ac787f`): `f71ac787f938db5417c24a2b509af0edd16b75b9`.
- The tables describe the production tree of the code commit `d36ca99a346dac5fbc1f1f0b522a7cad5cf78a78`, committed on top of that HEAD; line numbers are that tree's (the inventory and its manifest are added by the commit after it, which edits no Go file). Table 2 is derived from `git diff f71ac787f d36ca99a3 -- cmd internal` restricted to non-test files.
- No oracle file, fixture or golden is added or edited by this task (`git diff --stat f71ac787f d36ca99a3 -- internal/oracle` is empty): every substitution keeps its value, so no oracle-first checkpoint is needed.
- Production files changed by this task: 81, all inside the five A7 packages; no file of `internal/alerts`, `internal/errors`, `internal/ops`, `internal/models` or any other owner package changes (table 6).

## Method

1. File list: the 126 non-test files of the five A7 packages (`cmd/liza`, `internal/commands`, `internal/interactive`, `internal/jsonout`, `internal/tui`) that exist at the run start `96c9ad69a` (area partition) plus the 53 files the A7 tasks created (names and creating task in table 1): 179 production files. The set is not a glob: the generator asserts that (partition at `96c9ad69a` − deleted) ∪ (added by `git diff --name-status 96c9ad69a..d36ca99a3`) equals the tree's non-test files of the five packages, and fails otherwise. `*_test.go` files, `internal/ops/init_project.go` (out of scope) and the 5 files of the adoption-only packages `internal/alerts` and `internal/errors` are outside table 1; those files are scanned for the adoption sites of table 6 and are not edited.
2. Every string, integer, float and character literal of those files was extracted with a `go/parser` scan (import paths and struct tags excluded) with its syntactic context (comparison, `case`, composite element or field, map key or index, call argument with its argument position, declaration…): **5860** literals at this tree.
3. Each literal was read at its use site. Same-value, same-meaning constants of the owner packages (`models`, `roles`, `pipeline`, `ops`, `alerts`, `errors`, `scipsearch`, `toolchain`, `procscan`, `interactive` and the A7 packages themselves) were matched by value; a match of the same meaning is adopted (R1). Same-value constants of a different meaning are false friends (table 4).
4. A literal that is finite control-flow vocabulary, or a repeated or unexplained magic value, without an owner is named in its A7 concern (R4), or — when the owner package keeps the same spelling unexported or unnamed — named in A7 beside its use without editing the owner (R3, table 5). A finite set that decides control flow or crosses a boundary as a closed vocabulary is typed (R2).
5. Every remaining literal is classified (R5) with a reason in table 3; the generator raises on a literal it cannot place, so none is unclassified or deferred. Role names, tool and provider identities, operation names and configured commands stay dynamic (ADR-0045, table 9).
6. Completeness check: the scan of the committed tree yields 5860 rows; each is a row of table 3 (5561 retained, grouped by file and reason) or part of a constant or variable declaration (299 rows, tables 2b and 3b).

Rules: **R1** adopt an existing owner constant or type; **R2** type a finite set; **R3** name in A7 a value whose owner keeps it unexported or unnamed; **R4** name an unowned value in its A7 concern; **R5** classify retained literal data or a dynamic identity with a reason.
Outcome vocabulary: **adopted**, **named** (new constant, value unchanged), **typed** (defined type, one constant per value), **helper** (new function or variable that replaces repeated literals), **classified** (retained literal data or dynamic identity, with reason).

## Summary

| Outcome | Count |
|---|---|
| Substitution sites in this task (table 2: adopted, named, typed or helper use sites) | 301 |
| Declarations added by this task (table 2b) | 138 = 129 constants, 4 types, 1 variable, 4 helper functions |
| Constants added inside function bodies by this task (counted in table 1 "named constants") | 0 |
| Pre-existing named constants in A7 files (table 3b) | 204 |
| Retained literals classified R5 (table 3, grouped by file and reason) | 5561 |
| Literals scanned at this tree | 5860 = 5561 retained (table 3) + 299 inside declarations of named constants and variables (tables 2b and 3b) |
| Unclassified / deferred | 0 |
| New defined types | 4 (`dashboardSectionFormat`, `inspectQueryRoot`, `computedField`, `worktreeState`) |
| BLOCKED findings against A1, A3 or A6 closure (prospective symbol not delivered) | 0 |
| Owner gaps recorded (table 5) | see table 5 (0 rows) |

## 1. A7 files

| File | Origin | Lines | Literals scanned | Substitution sites | Declarations added | Named constants in file |
|---|---|---|---|---|---|---|
| `cmd/liza/agent_generation.go` | area partition | 41 | 7 | 0 | 0 | 0 |
| `cmd/liza/cmd_admission_limit.go` | area partition | 118 | 40 | 0 | 0 | 0 |
| `cmd/liza/cmd_admit.go` | area partition | 76 | 13 | 0 | 0 | 0 |
| `cmd/liza/cmd_agent.go` | area partition | 650 | 225 | 2 | 2 | 2 |
| `cmd/liza/cmd_amend_plan.go` | area partition | 96 | 48 | 1 | 0 | 0 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | area partition | 80 | 19 | 1 | 0 | 0 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | area partition | 101 | 26 | 1 | 0 | 0 |
| `cmd/liza/cmd_check.go` | area partition | 87 | 8 | 0 | 0 | 0 |
| `cmd/liza/cmd_cleanup.go` | area partition | 55 | 10 | 0 | 0 | 0 |
| `cmd/liza/cmd_config.go` | area partition | 286 | 80 | 3 | 2 | 2 |
| `cmd/liza/cmd_defer_provider_dependency.go` | area partition | 78 | 24 | 0 | 0 | 0 |
| `cmd/liza/cmd_deferred_validation.go` | area partition | 188 | 44 | 0 | 0 | 0 |
| `cmd/liza/cmd_hook_stop_guard.go` | area partition | 24 | 2 | 0 | 0 | 0 |
| `cmd/liza/cmd_human_note.go` | area partition | 76 | 17 | 0 | 0 | 0 |
| `cmd/liza/cmd_index_refresh.go` | area partition | 41 | 9 | 0 | 0 | 0 |
| `cmd/liza/cmd_init.go` | area partition | 741 | 257 | 6 | 0 | 0 |
| `cmd/liza/cmd_launch.go` | area partition | 295 | 154 | 4 | 0 | 0 |
| `cmd/liza/cmd_plan_check.go` | area partition | 242 | 71 | 1 | 0 | 0 |
| `cmd/liza/cmd_provision.go` | area partition | 83 | 26 | 1 | 1 | 1 |
| `cmd/liza/cmd_reaffirm_proof.go` | area partition | 104 | 20 | 3 | 1 | 1 |
| `cmd/liza/cmd_reconcile_verdict.go` | area partition | 90 | 31 | 1 | 0 | 0 |
| `cmd/liza/cmd_recover_integration.go` | area partition | 74 | 23 | 1 | 0 | 0 |
| `cmd/liza/cmd_rejection_rca.go` | area partition | 174 | 29 | 1 | 0 | 0 |
| `cmd/liza/cmd_release_blocked_chain.go` | area partition | 75 | 14 | 0 | 0 | 0 |
| `cmd/liza/cmd_repair_arch_ref.go` | area partition | 70 | 19 | 0 | 0 | 0 |
| `cmd/liza/cmd_replace_task.go` | area partition | 118 | 24 | 0 | 0 | 0 |
| `cmd/liza/cmd_reserve_provider.go` | area partition | 98 | 27 | 0 | 0 | 0 |
| `cmd/liza/cmd_review.go` | area partition | 705 | 154 | 2 | 2 | 2 |
| `cmd/liza/cmd_run_live.go` | area partition | 76 | 19 | 0 | 0 | 0 |
| `cmd/liza/cmd_semble_ready.go` | area partition | 27 | 5 | 0 | 0 | 0 |
| `cmd/liza/cmd_system.go` | area partition | 554 | 104 | 0 | 0 | 1 |
| `cmd/liza/cmd_system_inspect.go` | private, Task 3 (`d7321e525`) | 300 | 58 | 5 | 0 | 0 |
| `cmd/liza/cmd_task.go` | area partition | 450 | 135 | 1 | 1 | 1 |
| `cmd/liza/cmd_task_blocked.go` | private, Task 3 (`d7321e525`) | 632 | 227 | 1 | 1 | 1 |
| `cmd/liza/cmd_task_dependencies.go` | private, Task 3 (`d7321e525`) | 514 | 96 | 1 | 2 | 2 |
| `cmd/liza/cmd_task_records.go` | private, Task 3 (`d7321e525`) | 327 | 77 | 0 | 0 | 0 |
| `cmd/liza/cmd_task_retire.go` | private, Task 3 (`d7321e525`) | 216 | 54 | 0 | 0 | 0 |
| `cmd/liza/cmd_tool_result.go` | area partition | 132 | 56 | 0 | 0 | 0 |
| `cmd/liza/cmd_tool_result_acp.go` | area partition | 38 | 19 | 0 | 0 | 0 |
| `cmd/liza/cmd_tool_result_claude.go` | area partition | 32 | 8 | 0 | 0 | 0 |
| `cmd/liza/cmd_tool_result_codex.go` | area partition | 54 | 18 | 0 | 0 | 2 |
| `cmd/liza/cmd_tool_result_run.go` | area partition | 53 | 30 | 0 | 0 | 0 |
| `cmd/liza/cmd_tool_result_store.go` | private, Task 3 (`d7321e525`) | 78 | 16 | 0 | 0 | 4 |
| `cmd/liza/cmd_toolchain.go` | area partition | 378 | 124 | 5 | 0 | 0 |
| `cmd/liza/cmd_usage.go` | area partition | 116 | 42 | 1 | 0 | 0 |
| `cmd/liza/cmd_validate_payload.go` | area partition | 110 | 28 | 0 | 0 | 0 |
| `cmd/liza/cmd_worktree.go` | area partition | 195 | 24 | 1 | 0 | 0 |
| `cmd/liza/completion_values.go` | area partition | 226 | 19 | 4 | 0 | 0 |
| `cmd/liza/launch_build.go` | private, Task 4 (`7c9291a91`) | 249 | 136 | 8 | 4 | 4 |
| `cmd/liza/launch_exec.go` | private, Task 4 (`7c9291a91`) | 396 | 126 | 4 | 0 | 8 |
| `cmd/liza/launch_pairing_seed.go` | private, Task 4 (`7c9291a91`) | 104 | 13 | 2 | 2 | 2 |
| `cmd/liza/launch_resolve.go` | private, Task 4 (`7c9291a91`) | 224 | 56 | 25 | 1 | 7 |
| `cmd/liza/lifecycle.go` | area partition | 160 | 25 | 1 | 0 | 0 |
| `cmd/liza/main.go` | area partition | 379 | 97 | 3 | 1 | 2 |
| `cmd/liza/rbac.go` | area partition | 116 | 12 | 0 | 0 | 0 |
| `cmd/liza/supervisor_logs.go` | area partition | 144 | 17 | 3 | 2 | 2 |
| `internal/commands/add_task.go` | area partition | 97 | 4 | 0 | 0 | 0 |
| `internal/commands/add_tasks.go` | area partition | 40 | 4 | 0 | 0 | 0 |
| `internal/commands/agent_health.go` | area partition | 48 | 3 | 1 | 1 | 1 |
| `internal/commands/analyze.go` | area partition | 92 | 31 | 0 | 0 | 0 |
| `internal/commands/apply_dependency_repair.go` | area partition | 52 | 5 | 0 | 0 | 0 |
| `internal/commands/assess_blocked.go` | area partition | 64 | 11 | 0 | 0 | 0 |
| `internal/commands/assess_hypothesis_exhausted.go` | area partition | 38 | 2 | 0 | 0 | 0 |
| `internal/commands/await_budget.go` | area partition | 40 | 5 | 0 | 0 | 1 |
| `internal/commands/await_resubmission.go` | area partition | 90 | 1 | 0 | 0 | 0 |
| `internal/commands/await_verdict.go` | area partition | 112 | 2 | 0 | 0 | 0 |
| `internal/commands/cancel_task.go` | area partition | 44 | 2 | 0 | 0 | 0 |
| `internal/commands/claim_task.go` | area partition | 70 | 12 | 0 | 0 | 0 |
| `internal/commands/cleanup.go` | area partition | 90 | 12 | 1 | 0 | 0 |
| `internal/commands/clear_stale_review_claims.go` | area partition | 11 | 0 | 0 | 0 | 0 |
| `internal/commands/delete_agent.go` | area partition | 74 | 9 | 2 | 1 | 1 |
| `internal/commands/delete_task.go` | area partition | 81 | 11 | 2 | 0 | 0 |
| `internal/commands/doc.go` | area partition | 3 | 0 | 0 | 0 | 0 |
| `internal/commands/format_helpers.go` | area partition | 70 | 12 | 3 | 4 | 3 |
| `internal/commands/handoff.go` | area partition | 25 | 3 | 0 | 0 | 0 |
| `internal/commands/init.go` | area partition | 509 | 50 | 3 | 0 | 0 |
| `internal/commands/init_contract_placement.go` | private, Task 6 (`2c289761f`) | 474 | 86 | 0 | 0 | 9 |
| `internal/commands/init_integration_branch.go` | private, Task 5 (`e11874725`) | 71 | 14 | 0 | 0 | 0 |
| `internal/commands/init_mas_inputs.go` | private, Task 5 (`e11874725`) | 206 | 32 | 0 | 0 | 1 |
| `internal/commands/init_mas_install.go` | private, Task 5 (`e11874725`) | 122 | 15 | 0 | 0 | 0 |
| `internal/commands/init_mas_layout.go` | private, Task 5 (`e11874725`) | 83 | 12 | 0 | 0 | 2 |
| `internal/commands/init_mas_state.go` | private, Task 5 (`e11874725`) | 252 | 35 | 2 | 0 | 7 |
| `internal/commands/init_mistral_contract.go` | private, Task 6 (`2c289761f`) | 113 | 21 | 1 | 0 | 1 |
| `internal/commands/init_pairing.go` | private, Task 6 (`2c289761f`) | 317 | 45 | 3 | 0 | 0 |
| `internal/commands/inspect.go` | area partition | 283 | 54 | 9 | 0 | 0 |
| `internal/commands/inspect_agents.go` | area partition | 362 | 57 | 6 | 0 | 0 |
| `internal/commands/inspect_anomalies.go` | area partition | 175 | 26 | 0 | 0 | 1 |
| `internal/commands/inspect_field.go` | area partition | 408 | 77 | 42 | 0 | 0 |
| `internal/commands/inspect_metrics.go` | area partition | 329 | 42 | 2 | 0 | 0 |
| `internal/commands/inspect_task_assembly.go` | private, Task 7 (`bd42d1b34`) | 242 | 13 | 5 | 5 | 5 |
| `internal/commands/inspect_task_render.go` | private, Task 7 (`bd42d1b34`) | 420 | 158 | 8 | 0 | 0 |
| `internal/commands/inspect_task_types.go` | private, Task 7 (`bd42d1b34`) | 130 | 0 | 0 | 0 | 0 |
| `internal/commands/inspect_tasks.go` | area partition | 249 | 13 | 3 | 0 | 0 |
| `internal/commands/inspect_time.go` | area partition | 82 | 4 | 0 | 0 | 0 |
| `internal/commands/inspect_vocabulary.go` | private, Task 7 (`bd42d1b34`) | 147 | 43 | 0 | 43 | 52 |
| `internal/commands/integration_failure_diagnostics.go` | area partition | 163 | 31 | 23 | 19 | 21 |
| `internal/commands/lifecycle.go` | area partition | 81 | 28 | 0 | 0 | 0 |
| `internal/commands/mark_blocked.go` | area partition | 59 | 6 | 0 | 0 | 0 |
| `internal/commands/migrate.go` | area partition | 82 | 3 | 0 | 0 | 0 |
| `internal/commands/narrow_inherited_dependencies.go` | area partition | 58 | 14 | 2 | 2 | 2 |
| `internal/commands/pause.go` | area partition | 23 | 5 | 0 | 0 | 0 |
| `internal/commands/proceed.go` | area partition | 33 | 7 | 0 | 0 | 0 |
| `internal/commands/prompt_vocabulary.go` | private, this task (Task 11) | 8 | 2 | 0 | 2 | 2 |
| `internal/commands/provider_activation.go` | area partition | 408 | 52 | 0 | 0 | 1 |
| `internal/commands/providers.go` | area partition | 131 | 24 | 0 | 0 | 1 |
| `internal/commands/recover_agent.go` | area partition | 91 | 18 | 1 | 0 | 0 |
| `internal/commands/recover_task.go` | area partition | 76 | 16 | 0 | 0 | 0 |
| `internal/commands/rejection_rca.go` | area partition | 73 | 7 | 0 | 0 | 0 |
| `internal/commands/release_claim.go` | area partition | 32 | 3 | 0 | 0 | 0 |
| `internal/commands/repair_acceptance_commits.go` | area partition | 83 | 18 | 0 | 0 | 0 |
| `internal/commands/repair_agent_pool.go` | area partition | 375 | 40 | 0 | 0 | 0 |
| `internal/commands/repair_agent_pool_capacity.go` | private, Task 8 (`e8da46d69`) | 622 | 34 | 0 | 0 | 4 |
| `internal/commands/repair_agent_pool_render.go` | private, Task 8 (`e8da46d69`) | 129 | 43 | 0 | 0 | 0 |
| `internal/commands/repair_agent_pool_spawn.go` | private, Task 8 (`e8da46d69`) | 83 | 10 | 0 | 0 | 0 |
| `internal/commands/repair_agent_pool_start.go` | private, Task 8 (`e8da46d69`) | 284 | 45 | 0 | 0 | 3 |
| `internal/commands/repair_superseded_dependencies.go` | area partition | 51 | 7 | 0 | 0 | 0 |
| `internal/commands/replace_task.go` | area partition | 28 | 3 | 0 | 0 | 0 |
| `internal/commands/replan.go` | area partition | 43 | 7 | 0 | 0 | 0 |
| `internal/commands/resume.go` | area partition | 76 | 23 | 0 | 0 | 0 |
| `internal/commands/retarget_dependency.go` | area partition | 57 | 7 | 0 | 0 | 0 |
| `internal/commands/set_discovery_disposition.go` | area partition | 18 | 2 | 0 | 0 | 0 |
| `internal/commands/set_task_output.go` | area partition | 47 | 2 | 0 | 0 | 0 |
| `internal/commands/setup.go` | area partition | 663 | 107 | 3 | 0 | 1 |
| `internal/commands/sprint_checkpoint.go` | area partition | 35 | 12 | 0 | 0 | 0 |
| `internal/commands/start.go` | area partition | 37 | 15 | 0 | 0 | 0 |
| `internal/commands/status.go` | area partition | 52 | 1 | 0 | 0 | 0 |
| `internal/commands/status_orchestrator.go` | private, Task 7 (`bd42d1b34`) | 120 | 31 | 1 | 1 | 1 |
| `internal/commands/status_phase_handoff.go` | private, Task 7 (`bd42d1b34`) | 188 | 24 | 0 | 0 | 0 |
| `internal/commands/status_render.go` | private, Task 7 (`bd42d1b34`) | 316 | 117 | 2 | 0 | 1 |
| `internal/commands/status_sections.go` | private, Task 7 (`bd42d1b34`) | 298 | 18 | 1 | 0 | 1 |
| `internal/commands/status_snapshot.go` | private, Task 7 (`bd42d1b34`) | 33 | 2 | 0 | 0 | 0 |
| `internal/commands/status_types.go` | private, Task 7 (`bd42d1b34`) | 157 | 0 | 0 | 0 | 0 |
| `internal/commands/status_vocabulary.go` | private, Task 7 (`bd42d1b34`) | 53 | 15 | 0 | 4 | 15 |
| `internal/commands/stop.go` | area partition | 21 | 3 | 0 | 0 | 0 |
| `internal/commands/stop_guard.go` | area partition | 187 | 39 | 3 | 3 | 5 |
| `internal/commands/submit_review.go` | area partition | 54 | 10 | 0 | 0 | 0 |
| `internal/commands/submit_verdict.go` | area partition | 65 | 20 | 1 | 1 | 1 |
| `internal/commands/supersede_task.go` | area partition | 52 | 6 | 0 | 0 | 0 |
| `internal/commands/unblock_task.go` | area partition | 62 | 6 | 0 | 0 | 0 |
| `internal/commands/update_review_commit.go` | area partition | 34 | 9 | 0 | 0 | 0 |
| `internal/commands/update_sprint_metrics.go` | area partition | 43 | 9 | 0 | 0 | 0 |
| `internal/commands/usage_report.go` | area partition | 202 | 34 | 3 | 0 | 0 |
| `internal/commands/validate.go` | area partition | 244 | 50 | 2 | 0 | 0 |
| `internal/commands/validate_payload.go` | area partition | 129 | 18 | 2 | 0 | 1 |
| `internal/commands/watch.go` | area partition | 151 | 27 | 0 | 4 | 20 |
| `internal/commands/watch_checks_agents.go` | private, Task 9 (`6e24bb215`) | 377 | 51 | 3 | 0 | 2 |
| `internal/commands/watch_checks_sentinels.go` | private, Task 9 (`6e24bb215`) | 144 | 11 | 2 | 0 | 2 |
| `internal/commands/watch_checks_sprint.go` | private, Task 9 (`6e24bb215`) | 220 | 20 | 3 | 0 | 3 |
| `internal/commands/watch_checks_tasks.go` | private, Task 9 (`6e24bb215`) | 252 | 15 | 6 | 0 | 3 |
| `internal/commands/watch_diagnosis.go` | private, Task 9 (`6e24bb215`) | 170 | 38 | 1 | 0 | 0 |
| `internal/commands/watch_orchestrator.go` | private, Task 9 (`6e24bb215`) | 145 | 15 | 0 | 0 | 2 |
| `internal/commands/watch_reconcile.go` | private, Task 9 (`6e24bb215`) | 192 | 15 | 1 | 0 | 1 |
| `internal/commands/watch_repair.go` | private, Task 9 (`6e24bb215`) | 447 | 42 | 9 | 8 | 17 |
| `internal/commands/watch_repair_spawn.go` | private, Task 9 (`6e24bb215`) | 122 | 20 | 2 | 2 | 2 |
| `internal/commands/watch_snapshot.go` | private, Task 9 (`6e24bb215`) | 108 | 1 | 0 | 0 | 1 |
| `internal/commands/write_checkpoint.go` | area partition | 29 | 4 | 0 | 0 | 0 |
| `internal/commands/wt_create.go` | area partition | 31 | 5 | 0 | 0 | 0 |
| `internal/commands/wt_delete.go` | area partition | 29 | 4 | 0 | 0 | 0 |
| `internal/commands/wt_merge.go` | area partition | 106 | 23 | 2 | 1 | 1 |
| `internal/interactive/init_wizard.go` | area partition | 294 | 63 | 0 | 0 | 2 |
| `internal/interactive/post_init.go` | area partition | 44 | 6 | 0 | 0 | 0 |
| `internal/interactive/tty.go` | area partition | 18 | 0 | 0 | 0 | 0 |
| `internal/jsonout/classify.go` | area partition | 115 | 35 | 0 | 0 | 0 |
| `internal/jsonout/classify_slots.go` | private, Task 2 (`c6dcfdf8c`) | 206 | 50 | 0 | 0 | 16 |
| `internal/jsonout/envelope.go` | area partition | 64 | 1 | 0 | 0 | 0 |
| `internal/tui/activity.go` | private, Task 10 (`f71ac787f`) | 159 | 16 | 0 | 0 | 9 |
| `internal/tui/commands.go` | area partition | 421 | 34 | 3 | 0 | 2 |
| `internal/tui/keymap.go` | area partition | 98 | 33 | 0 | 0 | 0 |
| `internal/tui/model.go` | area partition | 375 | 12 | 5 | 5 | 23 |
| `internal/tui/styles.go` | area partition | 287 | 52 | 22 | 0 | 0 |
| `internal/tui/update.go` | area partition | 201 | 3 | 0 | 0 | 2 |
| `internal/tui/update_inline.go` | private, Task 10 (`f71ac787f`) | 151 | 16 | 1 | 1 | 1 |
| `internal/tui/update_keys.go` | private, Task 10 (`f71ac787f`) | 227 | 21 | 1 | 0 | 0 |
| `internal/tui/view.go` | area partition | 217 | 31 | 1 | 3 | 7 |
| `internal/tui/view_activity.go` | private, Task 10 (`f71ac787f`) | 86 | 25 | 1 | 0 | 0 |
| `internal/tui/view_agents.go` | private, Task 10 (`f71ac787f`) | 343 | 79 | 4 | 5 | 15 |
| `internal/tui/view_footer.go` | private, Task 10 (`f71ac787f`) | 67 | 25 | 0 | 0 | 0 |
| `internal/tui/view_overlays.go` | private, Task 10 (`f71ac787f`) | 98 | 21 | 1 | 0 | 1 |
| `internal/tui/view_tasks.go` | private, Task 10 (`f71ac787f`) | 286 | 55 | 3 | 1 | 13 |

Totals: 179 files, 29800 lines, 5860 literals, 301 substitution sites, 138 declarations added.

## 2. Adopted, named and typed (substitutions of this task)

Derived from `git diff` of the non-test A7 files: each row is a changed line that uses a constant, variable or helper in place of the literal(s) shown, matched by the removed text. `composed` marks a row whose replacement composes several literals.

| Site | Family | Rule | Outcome | Constant | Literal | Owner |
|---|---|---|---|---|---|---|
| `cmd/liza/cmd_agent.go:198` | V12 | R3 | named | `contractKeyNone` | `"none"` | A7 `cmd/liza/cmd_agent.go`; owner keeps it unexported or unnamed (table 5) |
| `cmd/liza/cmd_agent.go:233` | V10 | R4 | named | `autoAssignAgentIDAttempts` | `5` | A7 `cmd/liza/cmd_agent.go` |
| `cmd/liza/cmd_amend_plan.go:65` | V4 | R1 | adopted | `roles.RoleTypeOrchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_archive_acceptance_receipts.go:39` | V5 | R1 | adopted | `ops.LifecycleSafeActionStop`, `ops.LifecycleEffectsNone` | `"stop"`, `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `cmd/liza/cmd_archive_terminal_tasks.go:44` | V5 | R1 | adopted | `ops.LifecycleSafeActionStop`, `ops.LifecycleEffectsNone` | `"stop"`, `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `cmd/liza/cmd_config.go:181` | V12 | R4 | named | `boolArgTrue`, `boolArgFalse` | `"true"`, `"false"` | A7 `cmd/liza/cmd_config.go` |
| `cmd/liza/cmd_config.go:184` | V12 | R4 | named | `boolArgTrue` | `"true"` | A7 `cmd/liza/cmd_config.go` |
| `cmd/liza/cmd_config.go:281` | V7 | R4 | named | `commands.InspectFormatValue` | `"value"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_init.go:182` | V12 | R1 | adopted | `interactive.WizardModePairing` | `"pairing"` | A7 `internal/interactive/init_wizard.go` |
| `cmd/liza/cmd_init.go:199` | V12 | R1 | adopted | `interactive.WizardModePairing` | `"pairing"` | A7 `internal/interactive/init_wizard.go` |
| `cmd/liza/cmd_init.go:227` | V12 | R1 | adopted | `interactive.WizardModeFull` | `"full"` | A7 `internal/interactive/init_wizard.go` |
| `cmd/liza/cmd_init.go:254` | V12 | R1 | adopted | `interactive.WizardModePairing` | `"pairing"` | A7 `internal/interactive/init_wizard.go` |
| `cmd/liza/cmd_init.go:290` | V12 | R1 | adopted | `interactive.WizardModeFull` | `"full"` | A7 `internal/interactive/init_wizard.go` |
| `cmd/liza/cmd_init.go:731` | V12 | R1 | adopted | `scipsearch.LanguageGo`, `scipsearch.LanguageTypeScript`, `scipsearch.LanguagePython` | `"go"`, `"typescript"`, `"python"` | `scipsearch/languages.go` |
| `cmd/liza/cmd_launch.go:113` | V4 | R1 | adopted | `roles.RoleTypeDoer` | `"doer"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_launch.go:123` | V9 | R4 | named | `pairingReviewerIDPrefix` | `"reviewer-"` | A7 `cmd/liza/launch_resolve.go` |
| `cmd/liza/cmd_launch.go:220` | V4 | R1 | adopted | `roles.RoleTypeDoer` | `"doer"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_launch.go:230` | V9 | R4 | named | `pairingReviewerIDPrefix` | `"reviewer-"` | A7 `cmd/liza/launch_resolve.go` |
| `cmd/liza/cmd_plan_check.go:86` | V4 | R1 | adopted | `roles.RoleTypeOrchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_provision.go:71` | V10 | R4 | named | `provisionInstanceIDWidth` | `16` | A7 `cmd/liza/cmd_provision.go` |
| `cmd/liza/cmd_reaffirm_proof.go:62` | V4 | R1 | adopted | `roles.RoleTypeOrchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_reaffirm_proof.go:90` | V10 | R4 | named | `shortObjectIDWidth` | `8` | A7 `cmd/liza/cmd_reaffirm_proof.go` |
| `cmd/liza/cmd_reaffirm_proof.go:91` | V10 | R4 | named | `shortObjectIDWidth` | `8` | A7 `cmd/liza/cmd_reaffirm_proof.go` |
| `cmd/liza/cmd_reconcile_verdict.go:53` | V4 | R1 | adopted | `roles.RoleTypeOrchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_recover_integration.go:33` | V5 | R1 | adopted | `ops.LifecycleSafeActionStop`, `ops.LifecycleEffectsNone` | `"stop"`, `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `cmd/liza/cmd_rejection_rca.go:116` | V4 | R1 | adopted | `roles.RoleTypeOrchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/cmd_review.go:79` | V12 | R4 | named | `headRef` | `"HEAD"` | A7 `cmd/liza/cmd_review.go` |
| `cmd/liza/cmd_review.go:308` | V12 | R4 | named | `stdinFileArg` | `"-"` | A7 `cmd/liza/cmd_review.go` |
| `cmd/liza/cmd_system_inspect.go:114` | V7 | R4 | named | `commands.InspectTargetTasks` | `"tasks"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_system_inspect.go:138` | V7 | R4 | named | `commands.InspectFormatJSON` | `"json"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_system_inspect.go:192` | V7 | R4 | named | `commands.InspectTargetTasks` | `"tasks"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_system_inspect.go:292` | V7 | R4 | helper | `commands.InspectFormatNames` | `"json", "yaml", "table", "value"` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_system_inspect.go:299` | V7 | R4 | helper | `commands.StatusFormatNames` | `"json", "yaml", "dashboard"` (composed) | A7 `internal/commands/status_vocabulary.go` |
| `cmd/liza/cmd_task.go:225` | V10 | R4 | named | `defaultTaskPriority` | `1` | A7 `cmd/liza/cmd_task.go` |
| `cmd/liza/cmd_task_blocked.go:478` | V10 | R3 | named | `maxBlockedQuestions` | `3` | A7 `cmd/liza/cmd_task_blocked.go`; owner keeps it unexported or unnamed (table 5) |
| `cmd/liza/cmd_task_dependencies.go:211` | V12 | R3 | named | `retargetRejectedDetailKey`, `retargetRejectedAction` | `"diagnostic_action"`, `"retarget_dependency_rejected"` | A7 `cmd/liza/cmd_task_dependencies.go`; owner keeps it unexported or unnamed (table 5) |
| `cmd/liza/cmd_toolchain.go:225` | V12 | R1 | adopted | `toolchain.AgentToolsModeAuto`, `toolchain.AgentToolsModeSkip`, `toolchain.AgentToolsModeForce` | `"auto"`, `"skip"`, `"force"` | `toolchain/configure.go` |
| `cmd/liza/cmd_toolchain.go:317` | V12 | R1 | adopted | `commands.ContractActionGlobal` | `"global"` | A7 `internal/commands/init_contract_placement.go` |
| `cmd/liza/cmd_toolchain.go:337` | V12 | R1 | adopted | `toolchain.ProfileLean` | `"lean"` | `toolchain/catalog.go` |
| `cmd/liza/cmd_toolchain.go:340` | V12 | R1 | adopted | `toolchain.ProfileBalanced` | `"balanced"` | `toolchain/catalog.go` |
| `cmd/liza/cmd_toolchain.go:343` | V12 | R1 | adopted | `toolchain.ProfileFull` | `"full"` | `toolchain/catalog.go` |
| `cmd/liza/cmd_usage.go:107` | V7 | R4 | named | `commands.InspectFormatTable` | `"table"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/cmd_worktree.go:162` | V4 | R1 | adopted | `roles.RoleTypeReviewer` | `"reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/completion_values.go:123` | V7 | R4 | named | `commands.InspectTargetTasks` | `"tasks"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/completion_values.go:124` | V7 | R4 | named | `commands.InspectTargetAgents` | `"agents"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/completion_values.go:125` | V7 | R4 | named | `commands.InspectTargetMetrics` | `"metrics"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/completion_values.go:126` | V7 | R4 | named | `commands.InspectTargetAnomalies` | `"anomalies"` | A7 `internal/commands/inspect_vocabulary.go` |
| `cmd/liza/launch_build.go:24` | V9 | R4 | named | `weztermSplitRight` | `"--right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:26` | V9 | R4 | named | `weztermSplitBottom` | `"--bottom"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:63` | V9 | R4 | named | `weztermSplitRight` | `"--right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:65` | V9 | R4 | named | `weztermSplitBottom` | `"--bottom"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:116` | V9 | R4 | named | `cmuxSplitRight` | `"right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:118` | V9 | R4 | named | `cmuxSplitDown` | `"down"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:155` | V9 | R4 | named | `cmuxSplitRight` | `"right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_build.go:157` | V9 | R4 | named | `cmuxSplitDown` | `"down"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_exec.go:145` | V9 | R4 | named | `cmuxSplitRight` | `"right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_exec.go:147` | V9 | R4 | named | `cmuxSplitDown` | `"down"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_exec.go:214` | V9 | R4 | named | `cmuxSplitRight` | `"right"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_exec.go:216` | V9 | R4 | named | `cmuxSplitDown` | `"down"` | A7 `cmd/liza/launch_build.go` |
| `cmd/liza/launch_pairing_seed.go:31` | V9 | R4 | named | `pairingBoardDirMode` | `0o755` | A7 `cmd/liza/launch_pairing_seed.go` |
| `cmd/liza/launch_pairing_seed.go:35` | V9 | R4 | named | `pairingBoardFileMode` | `0o644` | A7 `cmd/liza/launch_pairing_seed.go` |
| `cmd/liza/launch_resolve.go:64` | V4 | R1 | adopted | `roles.Orchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:65` | V4 | R1 | adopted | `roles.CodePlanner` | `"code-planner"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:66` | V4 | R1 | adopted | `roles.CodePlanReviewer` | `"code-plan-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:67` | V4 | R1 | adopted | `roles.Coder` | `"coder"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:68` | V4 | R1 | adopted | `roles.CodeReviewer` | `"code-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:71` | V4 | R1 | adopted | `roles.Orchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:72` | V4 | R1 | adopted | `roles.Architect` | `"architect"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:73` | V4 | R1 | adopted | `roles.ArchitectureReviewer` | `"architecture-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:74` | V4 | R1 | adopted | `roles.CodePlanner` | `"code-planner"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:75` | V4 | R1 | adopted | `roles.CodePlanReviewer` | `"code-plan-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:76` | V4 | R1 | adopted | `roles.Coder` | `"coder"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:77` | V4 | R1 | adopted | `roles.CodeReviewer` | `"code-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:80` | V4 | R1 | adopted | `roles.Orchestrator` | `"orchestrator"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:81` | V4 | R1 | adopted | `roles.EpicPlanner` | `"epic-planner"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:82` | V4 | R1 | adopted | `roles.EpicPlanReviewer` | `"epic-plan-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:83` | V4 | R1 | adopted | `roles.USWriter` | `"us-writer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:84` | V4 | R1 | adopted | `roles.USReviewer` | `"us-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:85` | V4 | R1 | adopted | `roles.Architect` | `"architect"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:86` | V4 | R1 | adopted | `roles.ArchitectureReviewer` | `"architecture-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:87` | V4 | R1 | adopted | `roles.CodePlanner` | `"code-planner"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:88` | V4 | R1 | adopted | `roles.CodePlanReviewer` | `"code-plan-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:89` | V4 | R1 | adopted | `roles.Coder` | `"coder"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:90` | V4 | R1 | adopted | `roles.CodeReviewer` | `"code-reviewer"` | A1 `roles/roles.go` |
| `cmd/liza/launch_resolve.go:215` | V9 | R4 | named | `pairingReviewerIDPrefix` | `"reviewer-"` | A7 `cmd/liza/launch_resolve.go` |
| `cmd/liza/launch_resolve.go:222` | V9 | R4 | named | `pairingReviewerIDPrefix` | `"reviewer-"` | A7 `cmd/liza/launch_resolve.go` |
| `cmd/liza/lifecycle.go:131` | V5 | R1 | adopted | `ops.LifecycleEffectsNone` | `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `cmd/liza/main.go:288` | V12 | R3 | named | `defaultChangedBy` | `"human"` | A7 `cmd/liza/main.go`; owner keeps it unexported or unnamed (table 5) |
| `cmd/liza/main.go:356` | V10 | R1 | adopted | `lizaerrors.ExitError` | `1` | A1 (adoption only) `errors/errors.go` |
| `cmd/liza/main.go:377` | V10 | R1 | adopted | `lizaerrors.ExitError` | `1` | A1 (adoption only) `errors/errors.go` |
| `cmd/liza/supervisor_logs.go:42` | V10 | R4 | named | `supervisorLogFileMode` | `0o644` | A7 `cmd/liza/supervisor_logs.go` |
| `cmd/liza/supervisor_logs.go:46` | V10 | R4 | named | `supervisorLogFileMode` | `0o644` | A7 `cmd/liza/supervisor_logs.go` |
| `cmd/liza/supervisor_logs.go:85` | V10 | R4 | named | `supervisorBootstrapFileMode` | `0o600` | A7 `cmd/liza/supervisor_logs.go` |
| `internal/commands/agent_health.go:34` | V12 | R3 | named | `OperatorActor` | `"operator"` | A7 `internal/commands/agent_health.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/cleanup.go:70` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/delete_agent.go:37` | V10 | R4 | named | `deleteAgentTerminateWait` | `5 * time.Second` | A7 `internal/commands/delete_agent.go` |
| `internal/commands/delete_agent.go:69` | V12 | R4 | named | `confirmAnswerY`, `confirmAnswerYes` | `"y"`, `"yes"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/delete_task.go:37` | V12 | R4 | named | `confirmAnswerY`, `confirmAnswerYes` | `"y"`, `"yes"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/delete_task.go:49` | V12 | R4 | named | `confirmAnswerY`, `confirmAnswerYes` | `"y"`, `"yes"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/format_helpers.go:54` | V7 | R2 | typed | `dashboardFormatKeyValue` | `"kv"` | A7 `internal/commands/format_helpers.go` |
| `internal/commands/format_helpers.go:58` | V7 | R2 | typed | `dashboardFormatTable` | `"table"` | A7 `internal/commands/format_helpers.go` |
| `internal/commands/format_helpers.go:62` | V7 | R2 | typed | `dashboardFormatText` | `"text"` | A7 `internal/commands/format_helpers.go` |
| `internal/commands/init.go:384` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/init.go:427` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/init.go:477` | V4 | R1 | adopted | `roles.RoleTypeDoer`, `roles.RoleTypeReviewer` | `"doer"`, `"reviewer"` | A1 `roles/roles.go` |
| `internal/commands/init_mas_state.go:24` | V12 | R3 | named | `circuitBreakerStatusOK` | `"OK"` | A7 `internal/commands/status_vocabulary.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/init_mas_state.go:44` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/init_mistral_contract.go:84` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/init_pairing.go:308` | V12 | R1 | adopted | `scipsearch.LanguageGo` | `"go"` | `scipsearch/languages.go` |
| `internal/commands/init_pairing.go:310` | V12 | R1 | adopted | `scipsearch.LanguageTypeScript` | `"typescript"` | `scipsearch/languages.go` |
| `internal/commands/init_pairing.go:312` | V12 | R1 | adopted | `scipsearch.LanguagePython` | `"python"` | `scipsearch/languages.go` |
| `internal/commands/inspect.go:98` | V12 | R4 | named | `queryPathSeparator` | `"."` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:100` | V7 | R4 | helper | `isReservedTasksComputedQuery` | `"tasks.completion_rate", "tasks.avg_iteration_count"` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:104` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:105` | V7 | R4 | named | `taskQueryPrefix` | `"task", "task.", "."` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:106` | V12 | R4 | named | `queryPathSeparator` | `"."` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:108` | V7 | R4 | helper | `isTaskComputedField` | `"age", "time_in_status", "transition_id"` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:115` | V7 | R4/R2 | named/typed | `inspectRootTasks`, `inspectRootTask` | `"tasks"`, `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:116` | V12 | R4 | named | `queryPathSeparator` | `"."` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect.go:119` | V7 | R2 | typed | `inspectRootAgent`, `inspectRootVersion` | `"agent"`, `"version"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_agents.go:136` | V7 | R2 | typed | `inspectRootAgent` | `"agent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_agents.go:307` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_agents.go:312` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_agents.go:317` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_agents.go:326` | V12 | R1 | adopted | `processStatusRunning` | `"running"` | A7 `internal/commands/status_vocabulary.go` |
| `internal/commands/inspect_agents.go:351` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:30` | V7 | R2 | typed | `inspectRootVersion` | `"version"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:80` | V12 | R4 | named | `sprintTimelinePath` | `"sprint.timeline"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:95` | V7/V12 | R4 | named | `inspectTagKey`, `tagOmitted` | `"inspect"`, `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:98` | V12 | R4 | named | `yamlTagKey` | `"yaml"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:99` | V12 | R4 | named | `tagOmitted` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:192` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:212` | V12 | R4 | named | `taskFieldGenerationDigest` | `"generation_digest"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:213` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:215` | V7 | R4 | helper | `isTaskComputedField` | `"transition_id", "age", "time_in_status"` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:250` | V7 | R2 | typed | `inspectRootAgent`, `inspectRootTask` | `"agent"`, `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:251` | V7 | R2 | typed | `inspectRootAgent`, `inspectRootTask` | `"agent"`, `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:256` | V7 | R4 | named | `inspectRootAgents` | `"agents"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:258` | V7 | R4 | named | `inspectRootTasks` | `"tasks"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:260` | V7 | R4 | named | `inspectRootSprint` | `"sprint"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:262` | V7 | R2 | typed | `inspectRootAgent` | `"agent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:264` | V7 | R2 | typed | `inspectRootAgent` | `"agent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:269` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:271` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:284` | V7 | R2 | typed | `computedFieldActiveCount` | `"active_count"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:292` | V7 | R2 | typed | `computedFieldUtilization` | `"utilization"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:302` | V10 | R4 | named | `percentScale` | `100` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:304` | V7 | R4 | named | `inspectRootAgents` | `"agents"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:311` | V7 | R2 | typed | `computedFieldCompletionRate` | `"completion_rate"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:321` | V10 | R4 | named | `percentScale` | `100` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:322` | V7 | R2 | typed | `computedFieldAvgIterationCount` | `"avg_iteration_count"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:332` | V7 | R4 | named | `inspectRootTasks` | `"tasks"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:339` | V7 | R2 | typed | `computedFieldElapsed` | `"elapsed"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:342` | V7 | R2 | typed | `computedFieldRemaining` | `"remaining"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:345` | V7 | R2 | typed | `computedFieldProgressPercent` | `"progress_percent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:355` | V10 | R4 | named | `percentScale` | `100` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:357` | V7 | R4 | named | `inspectRootSprint` | `"sprint"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:365` | V7 | R2 | typed | `inspectRootAgent` | `"agent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:369` | V7 | R2 | typed | `computedFieldTimeSinceHeartbeat` | `"time_since_heartbeat"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:372` | V7 | R2 | typed | `computedFieldTimeOnTask` | `"time_on_task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:374` | V12 | R4 | named | `zeroDurationText` | `"0s"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:378` | V12 | R4 | named | `zeroDurationText` | `"0s"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:383` | V7 | R2 | typed | `inspectRootAgent` | `"agent"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:391` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:398` | V7 | R2 | typed | `computedFieldTransitionID` | `"transition_id"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:400` | V7 | R2 | typed | `computedFieldAge` | `"age"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:403` | V7 | R2 | typed | `computedFieldTimeInStatus` | `"time_in_status"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_field.go:406` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_metrics.go:165` | V12 | R4 | named | `unknownRoleLabel` | `"unknown"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_metrics.go:195` | V10 | R4 | named | `percentScale` | `100` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_assembly.go:128` | V12 | R3 | named | `historyExtraRepairSourceTask` | `"repair_source_task"` | A7 `internal/commands/inspect_task_assembly.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/inspect_task_assembly.go:131` | V12 | R3 | named | `historyExtraOperation` | `"operation"` | A7 `internal/commands/inspect_task_assembly.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/inspect_task_assembly.go:132` | V12 | R3 | named | `historyExtraRepairRequestCleared` | `"repair_request_cleared"` | A7 `internal/commands/inspect_task_assembly.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/inspect_task_assembly.go:135` | V12 | R3 | named | `historyExtraAffectedTaskIDs` | `"affected_task_ids"` | A7 `internal/commands/inspect_task_assembly.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/inspect_task_assembly.go:139` | V12 | R3 | named | `historyExtraRepairValidation` | `"repair_validation"` | A7 `internal/commands/inspect_task_assembly.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/inspect_task_render.go:94` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:99` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:104` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:109` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:139` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:300` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:305` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_task_render.go:310` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_tasks.go:103` | V7 | R2 | typed | `inspectRootTask` | `"task"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/inspect_tasks.go:234` | V4 | R1 | adopted | `models.ReplannedTransition` | `"replanned"` | A1 `models/replan_lineage.go` |
| `internal/commands/inspect_tasks.go:243` | V4 | R1 | adopted | `models.ReplannedTransition` | `"replanned"` | A1 `models/replan_lineage.go` |
| `internal/commands/integration_failure_diagnostics.go:47` | V12 | R4 | named | `diagnosticUnknown` | `"unknown"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:61` | V12 | R3 | named | `historyExtraDiagnostic` | `"diagnostic"` | A7 `internal/commands/integration_failure_diagnostics.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/integration_failure_diagnostics.go:74` | V12 | R4 | named | `diagnosticKeyOperation` | `"operation"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:75` | V12 | R4 | named | `diagnosticKeyReason` | `"reason"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:76` | V12 | R4 | named | `diagnosticKeyStatus` | `"status"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:77` | V12 | R4 | named | `diagnosticKeyRecoveryHint` | `"recovery_hint"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:80` | V12 | R4/R2 | named/typed | `diagnosticKeyWorktreeState`, `worktreeStateNotRecorded` | `"worktree_state"`, `"not_recorded"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:81` | V12 | R4 | named | `diagnosticKeyWorktreeRecorded` | `"worktree_recorded"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:83` | V12 | R4 | named | `diagnosticKeyWorktree` | `"worktree"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:84` | V12 | R4 | named | `diagnosticKeyWorktreeRecorded` | `"worktree_recorded"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:88` | V12 | R4 | named | `diagnosticKeyFailedBy` | `"failed_by"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:95` | V12 | R4/R2 | named/typed | `diagnosticKeyWorktreeState`, `worktreeStateRecordedUnchecked` | `"worktree_state"`, `"recorded_unchecked"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:100` | V12 | R4/R2 | named/typed | `diagnosticKeyWorktreeState`, `worktreeStatePresent` | `"worktree_state"`, `"present"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:101` | V12 | R4 | named | `diagnosticKeyWorktreeMissing` | `"worktree_missing"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:103` | V12 | R4/R2 | named/typed | `diagnosticKeyWorktreeState`, `worktreeStateMissing` | `"worktree_state"`, `"missing"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:104` | V12 | R4 | named | `diagnosticKeyWorktreeMissing` | `"worktree_missing"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:106` | V12 | R4/R2 | named/typed | `diagnosticKeyWorktreeState`, `worktreeStateUnknown` | `"worktree_state"`, `"unknown"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:107` | V12 | R4 | named | `diagnosticKeyWorktreeCheckError` | `"worktree_check_error"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:122` | V12 | R3 | named | `historyExtraPRURL` | `"pr_url"` | A7 `internal/commands/integration_failure_diagnostics.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/integration_failure_diagnostics.go:138` | V12 | R4 | named | `diagnosticKeyOperation` | `"operation"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:139` | V12 | R4 | named | `diagnosticKeyReason` | `"reason"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:140` | V12 | R4 | named | `diagnosticKeyRecoveryHint` | `"recovery_hint"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/integration_failure_diagnostics.go:146` | V12 | R4 | named | `diagnosticUnknown` | `"unknown"` | A7 `internal/commands/integration_failure_diagnostics.go` |
| `internal/commands/narrow_inherited_dependencies.go:40` | V12 | R3 | named | `narrowChildActionNarrowed` | `"narrowed"` | A7 `internal/commands/narrow_inherited_dependencies.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/narrow_inherited_dependencies.go:48` | V12 | R3 | named | `narrowChildActionSkipped` | `"skipped"` | A7 `internal/commands/narrow_inherited_dependencies.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/recover_agent.go:75` | V5 | R1 | adopted | `ops.LifecycleSafeActionRequery`, `ops.LifecycleEffectsCommitted` | `"requery"`, `"committed"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `internal/commands/setup.go:325` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/setup.go:353` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/setup.go:641` | V12 | R4 | named | `confirmAnswerY` | `"y"` | A7 `internal/commands/prompt_vocabulary.go` |
| `internal/commands/status_orchestrator.go:67` | V12 | R3 | named | `integrationStatusComplete` | `"complete"` | A7 `internal/commands/status_orchestrator.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/status_render.go:95` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/status_render.go:101` | V12 | R4 | named | `emptyCell` | `"-"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/status_sections.go:99` | V12 | R3 | named | `circuitBreakerStatusOK` | `"OK"` | A7 `internal/commands/status_vocabulary.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/stop_guard.go:59` | V12 | R4 | named | `stopGuardDecisionBlock` | `"block"` | A7 `internal/commands/stop_guard.go` |
| `internal/commands/stop_guard.go:72` | V12 | R4 | named | `stopGuardKeyBackgroundJobs` | `"background_tasks"` | A7 `internal/commands/stop_guard.go` |
| `internal/commands/stop_guard.go:91` | V12 | R4 | named | `backgroundJobStatusRunning` | `"running"` | A7 `internal/commands/stop_guard.go` |
| `internal/commands/submit_verdict.go:46` | V12 | R3 | named | `verdictApproved` | `"APPROVED"` | A7 `internal/commands/submit_verdict.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/usage_report.go:39` | V7 | R4 | helper | `inspectFormats` | `"json", "yaml", "table", "value", ""` (composed) | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/usage_report.go:177` | V7 | R1 | adopted | `inspectFormatTable` | `"table"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/usage_report.go:179` | V7 | R1 | adopted | `inspectFormatTable` | `"table"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/validate.go:185` | V12 | R4 | named | `unknownRoleLabel` | `"unknown"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/validate.go:226` | V12 | R4 | named | `unknownRoleLabel` | `"unknown"` | A7 `internal/commands/inspect_vocabulary.go` |
| `internal/commands/validate_payload.go:62` | V5 | R1 | adopted | `ops.LifecycleSafeActionRequery`, `ops.LifecycleEffectsNone` | `"requery"`, `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `internal/commands/validate_payload.go:68` | V5 | R1 | adopted | `ops.LifecycleSafeActionContinue`, `ops.LifecycleEffectsNone` | `"continue"`, `"none"` | A3–A6 `ops` `ops/lifecycle_result.go` |
| `internal/commands/watch_checks_agents.go:86` | V12 | R1 | adopted | `procscan.AgentProcessMismatched` | `"mismatched"` | `procscan/process_status.go` |
| `internal/commands/watch_checks_agents.go:142` | V12 | R1 | adopted | `procscan.AgentProcessMismatched` | `"mismatched"` | `procscan/process_status.go` |
| `internal/commands/watch_checks_agents.go:282` | V12 | R3 | named | `sentinelAssigneePrefix` | `"$"` | A7 `internal/commands/watch.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/watch_checks_sentinels.go:110` | V12 | R3 | named | `sentinelAssigneePrefix` | `"$"` | A7 `internal/commands/watch.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/watch_checks_sentinels.go:127` | V12 | R4 | named | `alertIdentitySeparator` | `"\|"` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_sprint.go:37` | V12 | R3 | named | `circuitBreakerStatusTriggered` | `"TRIGGERED"` | A7 `internal/commands/status_vocabulary.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/watch_checks_sprint.go:128` | V10 | R4 | named | `finalTaskAttempt` | `2` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_sprint.go:143` | V10 | R4 | named | `finalTaskAttempt` | `2` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_tasks.go:55` | V12 | R4 | named | `alertIdentitySeparator` | `"\|"` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_tasks.go:80` | V12 | R3 | named | `sentinelAssigneePrefix` | `"$"` | A7 `internal/commands/watch.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/watch_checks_tasks.go:168` | V10 | R4 | named | `hypothesisExhaustionFailures` | `2` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_tasks.go:175` | V12 | R4 | named | `alertIdentitySeparator` | `"\|"` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_tasks.go:191` | V10 | R4 | named | `finalTaskAttempt` | `2` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_checks_tasks.go:228` | V12 | R4 | named | `alertIdentitySeparator` | `"\|"` | A7 `internal/commands/watch.go` |
| `internal/commands/watch_diagnosis.go:99` | V12 | R1 | adopted | `validationReadinessFailed` | `"failed"` | A7 `internal/commands/repair_agent_pool_capacity.go` |
| `internal/commands/watch_reconcile.go:159` | V12 | R4 | helper | `stuckAlertCacheKey` | `":"` (composed) | A7 `internal/commands/watch_reconcile.go` |
| `internal/commands/watch_repair.go:296` | V12 | R4 | named | `autoRepairPendingEntryFormat` | `"%s%s:%d:%s:%d"` (composed) | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:319` | V12 | R4 | named | `registeredProcessKeyFormat` | `"%s:%d"` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:331` | V12 | R4 | named | `autoRepairPendingEntrySeparator` | `":"` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:333` | V10 | R4 | named | `autoRepairPendingEntryFields` | `4` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:334` | V12 | R4 | named | `pendingEntryPID` | `1` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:335` | V12 | R4 | named | `pendingEntryModelsItem` | `3` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:337` | V12 | R4 | named | `pendingEntryRole` | `0` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:342` | V12 | R4 | named | `pendingEntryRole`, `pendingEntryAgentID` | `0`, `2` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair.go:344` | V12 | R4 | named | `registeredProcessKeyFormat` | `"%s:%d"` | A7 `internal/commands/watch_repair.go` |
| `internal/commands/watch_repair_spawn.go:92` | V12 | R3 | named | `autoRepairLogActor` | `"system"` | A7 `internal/commands/watch_repair_spawn.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/watch_repair_spawn.go:93` | V12 | R3 | named | `autoRepairLogActionSpawned` | `"auto_repair_agent_spawned"` | A7 `internal/commands/watch_repair_spawn.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/commands/wt_merge.go:97` | V10 | R4 | named | `shortCommitWidth` | `7` | A7 `internal/commands/wt_merge.go` |
| `internal/commands/wt_merge.go:98` | V10 | R4 | named | `shortCommitWidth` | `7` | A7 `internal/commands/wt_merge.go` |
| `internal/tui/commands.go:266` | V12 | R3 | named | `commands.OperatorActor` | `"operator"` | A7 `internal/commands/agent_health.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/tui/commands.go:281` | V12 | R3 | named | `commands.OperatorActor` | `"operator"` | A7 `internal/commands/agent_health.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/tui/commands.go:352` | V12 | R3 | named | `commands.OperatorActor` | `"operator"` | A7 `internal/commands/agent_health.go`; owner keeps it unexported or unnamed (table 5) |
| `internal/tui/model.go:177` | V10 | R4 | named | `columnTierFullMinWidth` | `160` | A7 `internal/tui/model.go` |
| `internal/tui/model.go:179` | V10 | R4 | named | `columnTierWideMinWidth` | `120` | A7 `internal/tui/model.go` |
| `internal/tui/model.go:181` | V10 | R4 | named | `columnTierStandardMinWidth` | `80` | A7 `internal/tui/model.go` |
| `internal/tui/model.go:214` | V8 | R4 | named | `taskFilterLabelActive` | `"active"` | A7 `internal/tui/model.go` |
| `internal/tui/model.go:216` | V8 | R4 | named | `taskFilterLabelActiveBlocked` | `"active+blocked"` | A7 `internal/tui/model.go` |
| `internal/tui/styles.go:31` | V4 | R1 | adopted | `models.AgentStatusWorking` | `"WORKING"` | A1 `models/agent.go` |
| `internal/tui/styles.go:32` | V4 | R1 | adopted | `models.AgentStatusPlanning` | `"PLANNING"` | A1 `models/agent.go` |
| `internal/tui/styles.go:33` | V4 | R1 | adopted | `models.AgentStatusStarting` | `"STARTING"` | A1 `models/agent.go` |
| `internal/tui/styles.go:34` | V4 | R1 | adopted | `models.AgentStatusReviewing` | `"REVIEWING"` | A1 `models/agent.go` |
| `internal/tui/styles.go:35` | V4 | R1 | adopted | `models.AgentStatusIdle` | `"IDLE"` | A1 `models/agent.go` |
| `internal/tui/styles.go:36` | V4 | R1 | adopted | `models.AgentStatusWaiting` | `"WAITING"` | A1 `models/agent.go` |
| `internal/tui/styles.go:37` | V4 | R1 | adopted | `models.AgentStatusHandoff` | `"HANDOFF"` | A1 `models/agent.go` |
| `internal/tui/styles.go:40` | V4 | R1 | adopted | `models.SystemModeRunning` | `"RUNNING"` | A1 `models/config.go` |
| `internal/tui/styles.go:41` | V4 | R1 | adopted | `models.SystemModePaused` | `"PAUSED"` | A1 `models/config.go` |
| `internal/tui/styles.go:42` | V4 | R1 | adopted | `models.SprintStatusCheckpoint` | `"CHECKPOINT"` | A1 `models/sprint.go` |
| `internal/tui/styles.go:43` | V4 | R1 | adopted | `models.SystemModeStopped` | `"STOPPED"` | A1 `models/config.go` |
| `internal/tui/styles.go:46` | V4 | R1 | adopted | `models.SprintStatusCompleted` | `"COMPLETED"` | A1 `models/sprint.go` |
| `internal/tui/styles.go:47` | V4 | R1 | adopted | `models.SprintStatusInProgress` | `"IN_PROGRESS"` | A1 `models/sprint.go` |
| `internal/tui/styles.go:48` | V4 | R1 | adopted | `models.SprintStatusAborted` | `"ABORTED"` | A1 `models/sprint.go` |
| `internal/tui/styles.go:51` | V4 | R1 | adopted | `models.TaskStatusMerged` | `"MERGED"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:52` | V4 | R1 | adopted | `models.TaskStatusBlocked` | `"BLOCKED"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:53` | V4 | R1 | adopted | `models.TaskStatusIntegrationFailed` | `"INTEGRATION_FAILED"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:54` | V4 | R1 | adopted | `models.TaskStatusAbandoned` | `"ABANDONED"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:55` | V4 | R1 | adopted | `models.TaskStatusSuperseded` | `"SUPERSEDED"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:56` | V4 | R1 | adopted | `models.TaskStatusDraft` | `"DRAFT"` | A1 `models/task_status.go` |
| `internal/tui/styles.go:147` | V4 | R1 | adopted | `models.AgentStatusIdle` | `"IDLE"` | A1 `models/agent.go` |
| `internal/tui/styles.go:148` | V4 | R1 | adopted | `models.AgentStatusWaiting` | `"WAITING"` | A1 `models/agent.go` |
| `internal/tui/update_inline.go:150` | V8 | R4 | named | `inlineConfirmYes` | `"y"` | A7 `internal/tui/update_inline.go` |
| `internal/tui/update_keys.go:169` | V8 | R4 | named | `inlineConfirmYes` | `"y"` | A7 `internal/tui/update_inline.go` |
| `internal/tui/view.go:67` | V10 | R4 | named | `headerFooterHeight` | `2` | A7 `internal/tui/view.go` |
| `internal/tui/view_activity.go:15` | V10 | R4 | named | `panelChromeWidth` | `4` | A7 `internal/tui/view.go` |
| `internal/tui/view_agents.go:237` | V3 | R1 | adopted | `alerts.CategoryCircuitBreaker` | `"CIRCUIT BREAKER"` | A1 (adoption only) `alerts/categories.go` |
| `internal/tui/view_agents.go:240` | V8 | R4 | named | `pausedBreakerLabel` | `"BREAKER"` | A7 `internal/tui/view_agents.go` |
| `internal/tui/view_agents.go:245` | V10 | R4 | named | `compactAgentIDWidth`, `compactAgentRoleWidth` | `20`, `16` | A7 `internal/tui/view_agents.go` |
| `internal/tui/view_agents.go:247` | V10 | R4 | named | `expandedAgentIDWidth`, `expandedAgentRoleWidth` | `25`, `21` | A7 `internal/tui/view_agents.go` |
| `internal/tui/view_overlays.go:78` | V10 | R4 | named | `overlayChromeWidth` | `8` | A7 `internal/tui/view.go` |
| `internal/tui/view_tasks.go:93` | V10 | R4 | named | `panelChromeWidth` | `4` | A7 `internal/tui/view.go` |
| `internal/tui/view_tasks.go:95` | V10 | R4 | named | `taskPanelTitleMinGap` | `2` | A7 `internal/tui/view_tasks.go` |
| `internal/tui/view_tasks.go:96` | V10 | R4 | named | `taskPanelTitleMinGap` | `2` | A7 `internal/tui/view_tasks.go` |

A further 32 uses sit in code this task added (the new helper bodies and declarations of table 2b); they replace no literal and are not listed as substitutions.

### 2b. Declarations added and declarations changed

| Site | Kind | Family | Rule | Declaration | Type | Value |
|---|---|---|---|---|---|---|
| `cmd/liza/cmd_agent.go:29` | constant | V10 | R4 | `autoAssignAgentIDAttempts` |  | `5` |
| `cmd/liza/cmd_agent.go:33` | constant | V12 | R3 | `contractKeyNone` |  | `"none"` |
| `cmd/liza/cmd_config.go:170` | constant | V12 | R4 | `boolArgTrue` |  | `"true"` |
| `cmd/liza/cmd_config.go:171` | constant | V12 | R4 | `boolArgFalse` |  | `"false"` |
| `cmd/liza/cmd_provision.go:17` | constant | V10 | R4 | `provisionInstanceIDWidth` |  | `16` |
| `cmd/liza/cmd_reaffirm_proof.go:87` | constant | V10 | R4 | `shortObjectIDWidth` |  | `8` |
| `cmd/liza/cmd_review.go:26` | constant | V12 | R4 | `headRef` |  | `"HEAD"` |
| `cmd/liza/cmd_review.go:27` | constant | V12 | R4 | `stdinFileArg` |  | `"-"` |
| `cmd/liza/cmd_task.go:26` | constant | V10 | R4 | `defaultTaskPriority` |  | `1` |
| `cmd/liza/cmd_task_blocked.go:445` | constant | V10 | R3 | `maxBlockedQuestions` |  | `3` |
| `cmd/liza/cmd_task_dependencies.go:201` | constant | V12 | R3 | `retargetRejectedDetailKey` |  | `"diagnostic_action"` |
| `cmd/liza/cmd_task_dependencies.go:202` | constant | V12 | R3 | `retargetRejectedAction` |  | `"retarget_dependency_rejected"` |
| `cmd/liza/launch_build.go:15` | constant | V9 | R4 | `weztermSplitRight` |  | `"--right"` |
| `cmd/liza/launch_build.go:16` | constant | V9 | R4 | `weztermSplitBottom` |  | `"--bottom"` |
| `cmd/liza/launch_build.go:17` | constant | V9 | R4 | `cmuxSplitRight` |  | `"right"` |
| `cmd/liza/launch_build.go:18` | constant | V9 | R4 | `cmuxSplitDown` |  | `"down"` |
| `cmd/liza/launch_pairing_seed.go:15` | constant | V9 | R4 | `pairingBoardDirMode` |  | `0o755` |
| `cmd/liza/launch_pairing_seed.go:16` | constant | V9 | R4 | `pairingBoardFileMode` |  | `0o644` |
| `cmd/liza/launch_resolve.go:206` | constant | V9 | R4 | `pairingReviewerIDPrefix` |  | `"reviewer-"` |
| `cmd/liza/main.go:282` | constant | V12 | R3 | `defaultChangedBy` |  | `"human"` |
| `cmd/liza/supervisor_logs.go:17` | constant | V10 | R4 | `supervisorLogFileMode` |  | `0o644` |
| `cmd/liza/supervisor_logs.go:18` | constant | V10 | R4 | `supervisorBootstrapFileMode` |  | `0o600` |
| `internal/commands/agent_health.go:22` | constant | V12 | R3 | `OperatorActor` |  | `"operator"` |
| `internal/commands/delete_agent.go:16` | constant | V10 | R4 | `deleteAgentTerminateWait` |  | `5 * time.Second` |
| `internal/commands/format_helpers.go:36` | type | V7 | R2 | `dashboardSectionFormat` |  | `string` |
| `internal/commands/format_helpers.go:39` | constant | V7 | R2 | `dashboardFormatKeyValue` | `dashboardSectionFormat` | `"kv"` |
| `internal/commands/format_helpers.go:40` | constant | V7 | R2 | `dashboardFormatTable` | `dashboardSectionFormat` | `"table"` |
| `internal/commands/format_helpers.go:41` | constant | V7 | R2 | `dashboardFormatText` | `dashboardSectionFormat` | `"text"` |
| `internal/commands/inspect_task_assembly.go:117` | constant | V12 | R3 | `historyExtraRepairSourceTask` |  | `"repair_source_task"` |
| `internal/commands/inspect_task_assembly.go:118` | constant | V12 | R3 | `historyExtraOperation` |  | `"operation"` |
| `internal/commands/inspect_task_assembly.go:119` | constant | V12 | R3 | `historyExtraRepairRequestCleared` |  | `"repair_request_cleared"` |
| `internal/commands/inspect_task_assembly.go:120` | constant | V12 | R3 | `historyExtraAffectedTaskIDs` |  | `"affected_task_ids"` |
| `internal/commands/inspect_task_assembly.go:121` | constant | V12 | R3 | `historyExtraRepairValidation` |  | `"repair_validation"` |
| `internal/commands/inspect_vocabulary.go:45` | constant | V7 | R4 | `InspectFormatJSON` |  | `string(inspectFormatJSON)` |
| `internal/commands/inspect_vocabulary.go:46` | constant | V7 | R4 | `InspectFormatTable` |  | `string(inspectFormatTable)` |
| `internal/commands/inspect_vocabulary.go:47` | constant | V7 | R4 | `InspectFormatValue` |  | `string(inspectFormatValue)` |
| `internal/commands/inspect_vocabulary.go:48` | constant | V7 | R4 | `InspectTargetTasks` |  | `string(inspectTargetTasks)` |
| `internal/commands/inspect_vocabulary.go:49` | constant | V7 | R4 | `InspectTargetAgents` |  | `string(inspectTargetAgents)` |
| `internal/commands/inspect_vocabulary.go:50` | constant | V7 | R4 | `InspectTargetMetrics` |  | `string(inspectTargetMetrics)` |
| `internal/commands/inspect_vocabulary.go:51` | constant | V7 | R4 | `InspectTargetAnomalies` |  | `string(inspectTargetAnomalies)` |
| `internal/commands/inspect_vocabulary.go:56` | function | V7 | R4 | `InspectFormatNames` |  |  |
| `internal/commands/inspect_vocabulary.go:65` | type | V7 | R2 | `inspectQueryRoot` |  | `string` |
| `internal/commands/inspect_vocabulary.go:68` | constant | V7 | R4 | `inspectRootAgents` |  | `inspectQueryRoot(inspectTargetAgents)` |
| `internal/commands/inspect_vocabulary.go:69` | constant | V7 | R4 | `inspectRootTasks` |  | `inspectQueryRoot(inspectTargetTasks)` |
| `internal/commands/inspect_vocabulary.go:70` | constant | V7 | R4 | `inspectRootSprint` |  | `inspectQueryRoot(inspectTargetSprint)` |
| `internal/commands/inspect_vocabulary.go:71` | constant | V7 | R2 | `inspectRootAgent` | `inspectQueryRoot` | `"agent"` |
| `internal/commands/inspect_vocabulary.go:72` | constant | V7 | R2 | `inspectRootTask` | `inspectQueryRoot` | `"task"` |
| `internal/commands/inspect_vocabulary.go:73` | constant | V7 | R2 | `inspectRootVersion` | `inspectQueryRoot` | `"version"` |
| `internal/commands/inspect_vocabulary.go:78` | type | V7 | R2 | `computedField` |  | `string` |
| `internal/commands/inspect_vocabulary.go:81` | constant | V7 | R2 | `computedFieldActiveCount` | `computedField` | `"active_count"` |
| `internal/commands/inspect_vocabulary.go:82` | constant | V7 | R2 | `computedFieldUtilization` | `computedField` | `"utilization"` |
| `internal/commands/inspect_vocabulary.go:83` | constant | V7 | R2 | `computedFieldCompletionRate` | `computedField` | `"completion_rate"` |
| `internal/commands/inspect_vocabulary.go:84` | constant | V7 | R2 | `computedFieldAvgIterationCount` | `computedField` | `"avg_iteration_count"` |
| `internal/commands/inspect_vocabulary.go:85` | constant | V7 | R2 | `computedFieldElapsed` | `computedField` | `"elapsed"` |
| `internal/commands/inspect_vocabulary.go:86` | constant | V7 | R2 | `computedFieldRemaining` | `computedField` | `"remaining"` |
| `internal/commands/inspect_vocabulary.go:87` | constant | V7 | R2 | `computedFieldProgressPercent` | `computedField` | `"progress_percent"` |
| `internal/commands/inspect_vocabulary.go:88` | constant | V7 | R2 | `computedFieldTimeSinceHeartbeat` | `computedField` | `"time_since_heartbeat"` |
| `internal/commands/inspect_vocabulary.go:89` | constant | V7 | R2 | `computedFieldTimeOnTask` | `computedField` | `"time_on_task"` |
| `internal/commands/inspect_vocabulary.go:90` | constant | V7 | R2 | `computedFieldTransitionID` | `computedField` | `"transition_id"` |
| `internal/commands/inspect_vocabulary.go:91` | constant | V7 | R2 | `computedFieldAge` | `computedField` | `"age"` |
| `internal/commands/inspect_vocabulary.go:92` | constant | V7 | R2 | `computedFieldTimeInStatus` | `computedField` | `"time_in_status"` |
| `internal/commands/inspect_vocabulary.go:96` | variable | V7 | R4 | `taskComputedFields` |  | `[]computedField{…}` |
| `internal/commands/inspect_vocabulary.go:98` | function | V7 | R4 | `isTaskComputedField` |  |  |
| `internal/commands/inspect_vocabulary.go:104` | constant | V7 | R4 | `tasksComputedQueryPrefix` |  | `string(inspectTargetTasks) + "."` |
| `internal/commands/inspect_vocabulary.go:105` | constant | V7 | R4 | `taskQueryPrefix` |  | `string(inspectRootTask) + "."` |
| `internal/commands/inspect_vocabulary.go:106` | constant | V12 | R4 | `queryPathSeparator` |  | `"."` |
| `internal/commands/inspect_vocabulary.go:107` | constant | V12 | R4 | `sprintTimelinePath` |  | `"sprint.timeline"` |
| `internal/commands/inspect_vocabulary.go:108` | constant | V12 | R4 | `taskFieldGenerationDigest` |  | `"generation_digest"` |
| `internal/commands/inspect_vocabulary.go:109` | constant | V7 | R4 | `inspectTagKey` |  | `"inspect"` |
| `internal/commands/inspect_vocabulary.go:110` | constant | V12 | R4 | `yamlTagKey` |  | `"yaml"` |
| `internal/commands/inspect_vocabulary.go:111` | constant | V12 | R4 | `tagOmitted` |  | `"-"` |
| `internal/commands/inspect_vocabulary.go:112` | constant | V12 | R4 | `zeroDurationText` |  | `"0s"` |
| `internal/commands/inspect_vocabulary.go:113` | constant | V12 | R4 | `unknownRoleLabel` |  | `"unknown"` |
| `internal/commands/inspect_vocabulary.go:114` | constant | V10 | R4 | `percentScale` |  | `100` |
| `internal/commands/inspect_vocabulary.go:115` | constant | V12 | R4 | `emptyCell` |  | `"-"` |
| `internal/commands/inspect_vocabulary.go:120` | function | V12 | R4 | `isReservedTasksComputedQuery` |  |  |
| `internal/commands/integration_failure_diagnostics.go:13` | constant | V12 | R4 | `diagnosticUnknown` |  | `"unknown"` |
| `internal/commands/integration_failure_diagnostics.go:19` | constant | V12 | R4 | `diagnosticKeyOperation` |  | `"operation"` |
| `internal/commands/integration_failure_diagnostics.go:20` | constant | V12 | R4 | `diagnosticKeyReason` |  | `"reason"` |
| `internal/commands/integration_failure_diagnostics.go:21` | constant | V12 | R4 | `diagnosticKeyStatus` |  | `"status"` |
| `internal/commands/integration_failure_diagnostics.go:22` | constant | V12 | R4 | `diagnosticKeyRecoveryHint` |  | `"recovery_hint"` |
| `internal/commands/integration_failure_diagnostics.go:23` | constant | V12 | R4 | `diagnosticKeyWorktreeState` |  | `"worktree_state"` |
| `internal/commands/integration_failure_diagnostics.go:24` | constant | V12 | R4 | `diagnosticKeyWorktreeRecorded` |  | `"worktree_recorded"` |
| `internal/commands/integration_failure_diagnostics.go:25` | constant | V12 | R4 | `diagnosticKeyWorktree` |  | `"worktree"` |
| `internal/commands/integration_failure_diagnostics.go:26` | constant | V12 | R4 | `diagnosticKeyWorktreeMissing` |  | `"worktree_missing"` |
| `internal/commands/integration_failure_diagnostics.go:27` | constant | V12 | R4 | `diagnosticKeyWorktreeCheckError` |  | `"worktree_check_error"` |
| `internal/commands/integration_failure_diagnostics.go:28` | constant | V12 | R4 | `diagnosticKeyFailedBy` |  | `"failed_by"` |
| `internal/commands/integration_failure_diagnostics.go:31` | constant | V12 | R3 | `historyExtraDiagnostic` |  | `"diagnostic"` |
| `internal/commands/integration_failure_diagnostics.go:32` | constant | V12 | R3 | `historyExtraPRURL` |  | `"pr_url"` |
| `internal/commands/integration_failure_diagnostics.go:36` | type | V12 | R2 | `worktreeState` |  | `string` |
| `internal/commands/integration_failure_diagnostics.go:39` | constant | V12 | R2 | `worktreeStateNotRecorded` | `worktreeState` | `"not_recorded"` |
| `internal/commands/integration_failure_diagnostics.go:40` | constant | V12 | R2 | `worktreeStateRecordedUnchecked` | `worktreeState` | `"recorded_unchecked"` |
| `internal/commands/integration_failure_diagnostics.go:41` | constant | V12 | R2 | `worktreeStatePresent` | `worktreeState` | `"present"` |
| `internal/commands/integration_failure_diagnostics.go:42` | constant | V12 | R2 | `worktreeStateMissing` | `worktreeState` | `"missing"` |
| `internal/commands/integration_failure_diagnostics.go:43` | constant | V12 | R2 | `worktreeStateUnknown` | `worktreeState` | `diagnosticUnknown` |
| `internal/commands/narrow_inherited_dependencies.go:23` | constant | V12 | R3 | `narrowChildActionNarrowed` |  | `"narrowed"` |
| `internal/commands/narrow_inherited_dependencies.go:24` | constant | V12 | R3 | `narrowChildActionSkipped` |  | `"skipped"` |
| `internal/commands/prompt_vocabulary.go:6` | constant | V12 | R4 | `confirmAnswerY` |  | `"y"` |
| `internal/commands/prompt_vocabulary.go:7` | constant | V12 | R4 | `confirmAnswerYes` |  | `"yes"` |
| `internal/commands/status_orchestrator.go:16` | constant | V12 | R3 | `integrationStatusComplete` |  | `"complete"` |
| `internal/commands/status_vocabulary.go:14` | constant | V7 | R2 | `statusFormatDashboard` | `statusFormat` | `"dashboard"` |
| `internal/commands/status_vocabulary.go:19` | function | V7 | R4 | `StatusFormatNames` |  |  |
| `internal/commands/status_vocabulary.go:44` | constant | V12 | R3 | `circuitBreakerStatusOK` |  | `"OK"` |
| `internal/commands/status_vocabulary.go:45` | constant | V12 | R3 | `circuitBreakerStatusTriggered` |  | `"TRIGGERED"` |
| `internal/commands/stop_guard.go:27` | constant | V12 | R4 | `stopGuardDecisionBlock` |  | `"block"` |
| `internal/commands/stop_guard.go:28` | constant | V12 | R4 | `stopGuardKeyBackgroundJobs` |  | `"background_tasks"` |
| `internal/commands/stop_guard.go:29` | constant | V12 | R4 | `backgroundJobStatusRunning` |  | `"running"` |
| `internal/commands/submit_verdict.go:40` | constant | V12 | R3 | `verdictApproved` |  | `"APPROVED"` |
| `internal/commands/watch.go:44` | constant | V10 | R4 | `finalTaskAttempt` |  | `2` |
| `internal/commands/watch.go:47` | constant | V10 | R4 | `hypothesisExhaustionFailures` |  | `2` |
| `internal/commands/watch.go:50` | constant | V12 | R3 | `sentinelAssigneePrefix` |  | `"$"` |
| `internal/commands/watch.go:52` | constant | V12 | R4 | `alertIdentitySeparator` |  | `"\|"` |
| `internal/commands/watch_repair.go:30` | constant | V12 | R4 | `autoRepairPendingEntryFormat` |  | `"%s:%d:%s:%d"` |
| `internal/commands/watch_repair.go:31` | constant | V12 | R4 | `autoRepairPendingEntrySeparator` |  | `":"` |
| `internal/commands/watch_repair.go:32` | constant | V10 | R4 | `autoRepairPendingEntryFields` |  | `4` |
| `internal/commands/watch_repair.go:37` | constant | V12 | R4 | `pendingEntryRole` |  | `iota(0)` |
| `internal/commands/watch_repair.go:38` | constant | V12 | R4 | `pendingEntryPID` |  | `iota(1)` |
| `internal/commands/watch_repair.go:39` | constant | V12 | R4 | `pendingEntryAgentID` |  | `iota(2)` |
| `internal/commands/watch_repair.go:40` | constant | V12 | R4 | `pendingEntryModelsItem` |  | `iota(3)` |
| `internal/commands/watch_repair.go:44` | constant | V12 | R4 | `registeredProcessKeyFormat` |  | `"%s:%d"` |
| `internal/commands/watch_repair_spawn.go:16` | constant | V12 | R3 | `autoRepairLogActor` |  | `"system"` |
| `internal/commands/watch_repair_spawn.go:17` | constant | V12 | R3 | `autoRepairLogActionSpawned` |  | `"auto_repair_agent_spawned"` |
| `internal/commands/wt_merge.go:14` | constant | V10 | R4 | `shortCommitWidth` |  | `7` |
| `internal/tui/model.go:169` | constant | V10 | R4 | `columnTierFullMinWidth` |  | `160` |
| `internal/tui/model.go:170` | constant | V10 | R4 | `columnTierWideMinWidth` |  | `120` |
| `internal/tui/model.go:171` | constant | V10 | R4 | `columnTierStandardMinWidth` |  | `80` |
| `internal/tui/model.go:206` | constant | V8 | R4 | `taskFilterLabelActive` |  | `"active"` |
| `internal/tui/model.go:207` | constant | V8 | R4 | `taskFilterLabelActiveBlocked` |  | `"active+blocked"` |
| `internal/tui/update_inline.go:146` | constant | V8 | R4 | `inlineConfirmYes` |  | `"y"` |
| `internal/tui/view.go:24` | constant | V10 | R4 | `panelChromeWidth` |  | `4` |
| `internal/tui/view.go:26` | constant | V10 | R4 | `overlayChromeWidth` |  | `8` |
| `internal/tui/view.go:28` | constant | V10 | R4 | `headerFooterHeight` |  | `2` |
| `internal/tui/view_agents.go:220` | constant | V10 | R4 | `compactAgentIDWidth` |  | `20` |
| `internal/tui/view_agents.go:221` | constant | V10 | R4 | `compactAgentRoleWidth` |  | `16` |
| `internal/tui/view_agents.go:222` | constant | V10 | R4 | `expandedAgentIDWidth` |  | `25` |
| `internal/tui/view_agents.go:223` | constant | V10 | R4 | `expandedAgentRoleWidth` |  | `21` |
| `internal/tui/view_agents.go:225` | constant | V8 | R4 | `pausedBreakerLabel` |  | `"BREAKER"` |
| `internal/tui/view_tasks.go:29` | constant | V10 | R4 | `taskPanelTitleMinGap` |  | `2` |

Declarations whose right side changed (the value is unchanged):

| Declaration | Before | After |
|---|---|---|
| `internal/commands/init_mas_state.go:24` `initCircuitBreakerStatus` | `"OK"` | `circuitBreakerStatusOK` |
| `internal/commands/integration_failure_diagnostics.go:47` `integrationFailureFallbackOperation` | `"unknown"` | `diagnosticUnknown` |

## 3. Classified literals (R5)

Rows are grouped by file and reason; the line numbers are the tree's, one per occurrence (a repeated number is a second occurrence on that line). Messages keep their exact wording: `internal/jsonout/classify.go` classifies untyped errors by substring and the human, `--json` and completion output is frozen (goal "Frozen surfaces").

| File | Kind | Reason | Count | Lines |
|---|---|---|---|---|
| `cmd/liza/agent_generation.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 26, 30 |
| `cmd/liza/agent_generation.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 25, 28, 35 |
| `cmd/liza/agent_generation.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 31, 32 |
| `cmd/liza/cmd_admission_limit.go` | message or display text | cobra help text (frozen --help output) | 3 | 18, 39, 68 |
| `cmd/liza/cmd_admission_limit.go` | message or display text | flag usage text (frozen --help output) | 5 | 105, 106, 107, 108, 111 |
| `cmd/liza/cmd_admission_limit.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 19, 40, 90, 99 |
| `cmd/liza/cmd_admission_limit.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 89, 108, 111 |
| `cmd/liza/cmd_admission_limit.go` | number | cobra argument arity of the command (frozen CLI surface) | 2 | 49, 69 |
| `cmd/liza/cmd_admission_limit.go` | number | flag default value (frozen CLI surface) | 1 | 105 |
| `cmd/liza/cmd_admission_limit.go` | number | position in a fixed-layout slice or arguments list | 2 | 60, 76 |
| `cmd/liza/cmd_admission_limit.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 5 | 105, 106, 107, 108, 111 |
| `cmd/liza/cmd_admission_limit.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 3 | 109, 110, 112 |
| `cmd/liza/cmd_admission_limit.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 5 | 51, 52, 53, 54, 71 |
| `cmd/liza/cmd_admission_limit.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 17, 38, 67 |
| `cmd/liza/cmd_admission_limit.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 3 | 34, 48, 48 |
| `cmd/liza/cmd_admission_limit.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 89 |
| `cmd/liza/cmd_admit.go` | message or display text | cobra help text (frozen --help output) | 1 | 18 |
| `cmd/liza/cmd_admit.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 19, 58, 74 |
| `cmd/liza/cmd_admit.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 36 |
| `cmd/liza/cmd_admit.go` | number | flag default value (frozen CLI surface) | 1 | 74 |
| `cmd/liza/cmd_admit.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 48 |
| `cmd/liza/cmd_admit.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 65 |
| `cmd/liza/cmd_admit.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 74 |
| `cmd/liza/cmd_admit.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 39 |
| `cmd/liza/cmd_admit.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 17 |
| `cmd/liza/cmd_admit.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 35, 35 |
| `cmd/liza/cmd_agent.go` | message or display text | cobra help text (frozen --help output) | 11 | 37, 346, 347, 410, 411, 461, 503, 504, 536, 549, 550 |
| `cmd/liza/cmd_agent.go` | message or display text | flag usage text (frozen --help output) | 32 | 590, 590, 590, 591, 592, 594, 594, 598, 599, 600, 601, 601, 602, 603, 604, 616, 617, 618, 621, 622, 623, 626, 627, 628, 634, 635, 636, 637, 638, 639, 648, 649 |
| `cmd/liza/cmd_agent.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 286 |
| `cmd/liza/cmd_agent.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 24 | 38, 111, 116, 152, 156, 188, 199, 200, 206, 211, 234, 296, 297, 299, 302, 304, 305, 306, 307, 309, 312, 315, 444, 462 |
| `cmd/liza/cmd_agent.go` | message or display text | placeholder shown in usage or dry-run text (display text) | 1 | 279 |
| `cmd/liza/cmd_agent.go` | message or display text | separator of a rendered list (display text) | 4 | 116, 188, 306, 312 |
| `cmd/liza/cmd_agent.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 29 | 99, 132, 155, 155, 198, 198, 210, 214, 277, 298, 301, 308, 314, 338, 443, 590, 591, 592, 599, 602, 603, 604, 622, 627, 634, 635, 636, 638, 639 |
| `cmd/liza/cmd_agent.go` | number | argument position the completion applies to | 5 | 582, 583, 584, 585, 586 |
| `cmd/liza/cmd_agent.go` | number | cobra argument arity of the command (frozen CLI surface) | 7 | 84, 84, 371, 419, 509, 537, 555 |
| `cmd/liza/cmd_agent.go` | number | flag default value (frozen CLI surface) | 1 | 594 |
| `cmd/liza/cmd_agent.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 100, 311 |
| `cmd/liza/cmd_agent.go` | number | position in a fixed-layout slice or arguments list | 7 | 97, 101, 387, 435, 523, 543, 557 |
| `cmd/liza/cmd_agent.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 25 | 590, 591, 592, 594, 598, 599, 600, 601, 616, 617, 618, 621, 622, 623, 626, 627, 628, 634, 635, 636, 637, 638, 639, 648, 649 |
| `cmd/liza/cmd_agent.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 3 | 640, 641, 642 |
| `cmd/liza/cmd_agent.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 28 | 124, 138, 139, 140, 141, 142, 143, 144, 145, 173, 388, 389, 390, 436, 437, 438, 485, 486, 487, 515, 516, 517, 518, 519, 520, 558, 559, 595 |
| `cmd/liza/cmd_agent.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 6 | 629, 630, 631, 643, 644, 645 |
| `cmd/liza/cmd_agent.go` | flag or CLI spelling | flag shorthand letter (frozen CLI surface) | 1 | 600 |
| `cmd/liza/cmd_agent.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 7 | 36, 345, 409, 460, 502, 535, 548 |
| `cmd/liza/cmd_agent.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 12 | 323, 323, 325, 325, 327, 327, 329, 329, 331, 331, 333, 333 |
| `cmd/liza/cmd_agent.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 83, 482 |
| `cmd/liza/cmd_agent.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 209 |
| `cmd/liza/cmd_agent.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 215 |
| `cmd/liza/cmd_agent.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 10 | 322, 324, 326, 328, 330, 330, 330, 330, 330, 330 |
| `cmd/liza/cmd_agent.go` | other literal | flag default value (frozen CLI surface) | 3 | 618, 623, 649 |
| `cmd/liza/cmd_amend_plan.go` | message or display text | cobra help text (frozen --help output) | 1 | 20 |
| `cmd/liza/cmd_amend_plan.go` | message or display text | flag usage text (frozen --help output) | 7 | 87, 88, 89, 90, 91, 92, 93 |
| `cmd/liza/cmd_amend_plan.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 48, 79 |
| `cmd/liza/cmd_amend_plan.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 10 | 47, 47, 47, 56, 87, 89, 90, 91, 92, 93 |
| `cmd/liza/cmd_amend_plan.go` | number | argument position the completion applies to | 1 | 86 |
| `cmd/liza/cmd_amend_plan.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 21 |
| `cmd/liza/cmd_amend_plan.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 47 |
| `cmd/liza/cmd_amend_plan.go` | number | position in a fixed-layout slice or arguments list | 1 | 50 |
| `cmd/liza/cmd_amend_plan.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 41 |
| `cmd/liza/cmd_amend_plan.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 7 | 87, 88, 89, 90, 91, 92, 93 |
| `cmd/liza/cmd_amend_plan.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 11 | 37, 38, 39, 40, 47, 47, 47, 47, 51, 55, 56 |
| `cmd/liza/cmd_amend_plan.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 3 | 42, 42, 42 |
| `cmd/liza/cmd_amend_plan.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 19 |
| `cmd/liza/cmd_amend_plan.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 56 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | message or display text | cobra help text (frozen --help output) | 2 | 23, 24 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | message or display text | flag usage text (frozen --help output) | 2 | 76, 77 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 39, 49, 70 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 38 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 48, 48, 57, 63 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 76, 77 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 46, 47 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 22 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 38 |
| `cmd/liza/cmd_archive_acceptance_receipts.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 1 | 39 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | message or display text | cobra help text (frozen --help output) | 2 | 24, 25 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | message or display text | flag usage text (frozen --help output) | 3 | 96, 97, 98 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 44, 54, 65, 87, 89 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 43 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 64, 64, 72, 78 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 3 | 96, 97, 98 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 5 | 51, 53, 53, 62, 63 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 23 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 43 |
| `cmd/liza/cmd_archive_terminal_tasks.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 1 | 44 |
| `cmd/liza/cmd_check.go` | message or display text | cobra help text (frozen --help output) | 2 | 30, 31 |
| `cmd/liza/cmd_check.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 58 |
| `cmd/liza/cmd_check.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 74, 75 |
| `cmd/liza/cmd_check.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 37 |
| `cmd/liza/cmd_check.go` | number | position in a fixed-layout slice or arguments list | 1 | 39 |
| `cmd/liza/cmd_check.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 29 |
| `cmd/liza/cmd_cleanup.go` | message or display text | flag usage text (frozen --help output) | 1 | 54 |
| `cmd/liza/cmd_cleanup.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 13, 14, 36, 38 |
| `cmd/liza/cmd_cleanup.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 46 |
| `cmd/liza/cmd_cleanup.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 54 |
| `cmd/liza/cmd_cleanup.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 25, 45 |
| `cmd/liza/cmd_cleanup.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 12 |
| `cmd/liza/cmd_config.go` | message or display text | cobra help text (frozen --help output) | 3 | 21, 31, 43 |
| `cmd/liza/cmd_config.go` | message or display text | flag usage text (frozen --help output) | 3 | 281, 282, 283 |
| `cmd/liza/cmd_config.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 17 | 22, 32, 44, 149, 176, 179, 182, 191, 198, 202, 216, 224, 233, 239, 254, 266, 269 |
| `cmd/liza/cmd_config.go` | message or display text | separator of a rendered list (display text) | 2 | 266, 269 |
| `cmd/liza/cmd_config.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 128, 175, 197, 223, 238, 243, 283 |
| `cmd/liza/cmd_config.go` | number | argument arity of a config subcommand | 2 | 34, 101 |
| `cmd/liza/cmd_config.go` | number | position in a fixed-layout slice or arguments list | 19 | 115, 118, 121, 124, 127, 181, 181, 184, 198, 200, 202, 202, 205, 208, 224, 226, 239, 242, 268 |
| `cmd/liza/cmd_config.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 3 | 281, 282, 283 |
| `cmd/liza/cmd_config.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 5 | 108, 109, 110, 178, 178 |
| `cmd/liza/cmd_config.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 1 | 285 |
| `cmd/liza/cmd_config.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 20, 30, 42 |
| `cmd/liza/cmd_config.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 11 | 26, 26, 32, 32, 99, 99, 99, 99, 99, 99, 99 |
| `cmd/liza/cmd_config.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 137 |
| `cmd/liza/cmd_config.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 1 | 242 |
| `cmd/liza/cmd_defer_provider_dependency.go` | message or display text | cobra help text (frozen --help output) | 2 | 16, 17 |
| `cmd/liza/cmd_defer_provider_dependency.go` | message or display text | flag usage text (frozen --help output) | 3 | 70, 71, 72 |
| `cmd/liza/cmd_defer_provider_dependency.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 41, 61 |
| `cmd/liza/cmd_defer_provider_dependency.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 40, 70, 71, 72 |
| `cmd/liza/cmd_defer_provider_dependency.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 32 |
| `cmd/liza/cmd_defer_provider_dependency.go` | number | position in a fixed-layout slice or arguments list | 1 | 50 |
| `cmd/liza/cmd_defer_provider_dependency.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 3 | 70, 71, 72 |
| `cmd/liza/cmd_defer_provider_dependency.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 3 | 73, 74, 75 |
| `cmd/liza/cmd_defer_provider_dependency.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 3 | 43, 44, 45 |
| `cmd/liza/cmd_defer_provider_dependency.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 15 |
| `cmd/liza/cmd_defer_provider_dependency.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 40 |
| `cmd/liza/cmd_deferred_validation.go` | message or display text | cobra help text (frozen --help output) | 14 | 18, 31, 32, 46, 47, 59, 60, 73, 74, 88, 89, 104, 105, 129 |
| `cmd/liza/cmd_deferred_validation.go` | message or display text | flag usage text (frozen --help output) | 3 | 175, 179, 182 |
| `cmd/liza/cmd_deferred_validation.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 19, 139 |
| `cmd/liza/cmd_deferred_validation.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 138, 175, 179 |
| `cmd/liza/cmd_deferred_validation.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 3 | 175, 179, 182 |
| `cmd/liza/cmd_deferred_validation.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 2 | 176, 180 |
| `cmd/liza/cmd_deferred_validation.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 8 | 38, 39, 52, 65, 66, 81, 97, 122 |
| `cmd/liza/cmd_deferred_validation.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 8 | 17, 30, 45, 58, 72, 87, 103, 128 |
| `cmd/liza/cmd_deferred_validation.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 138 |
| `cmd/liza/cmd_hook_stop_guard.go` | message or display text | cobra help text (frozen --help output) | 1 | 14 |
| `cmd/liza/cmd_hook_stop_guard.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 13 |
| `cmd/liza/cmd_human_note.go` | message or display text | cobra help text (frozen --help output) | 2 | 18, 19 |
| `cmd/liza/cmd_human_note.go` | message or display text | flag usage text (frozen --help output) | 1 | 72 |
| `cmd/liza/cmd_human_note.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 36, 41, 46, 63 |
| `cmd/liza/cmd_human_note.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 35, 72 |
| `cmd/liza/cmd_human_note.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 27 |
| `cmd/liza/cmd_human_note.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 44 |
| `cmd/liza/cmd_human_note.go` | number | position in a fixed-layout slice or arguments list | 1 | 52 |
| `cmd/liza/cmd_human_note.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 72 |
| `cmd/liza/cmd_human_note.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 1 | 73 |
| `cmd/liza/cmd_human_note.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 38 |
| `cmd/liza/cmd_human_note.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 17 |
| `cmd/liza/cmd_human_note.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 35 |
| `cmd/liza/cmd_index_refresh.go` | message or display text | cobra help text (frozen --help output) | 1 | 18 |
| `cmd/liza/cmd_index_refresh.go` | message or display text | flag usage text (frozen --help output) | 1 | 39 |
| `cmd/liza/cmd_index_refresh.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 28 |
| `cmd/liza/cmd_index_refresh.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 39 |
| `cmd/liza/cmd_index_refresh.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 22 |
| `cmd/liza/cmd_index_refresh.go` | external argv | external command argument: cmux, wezterm or Git vocabulary (not a Liza set; C4 Git literals are preserved) | 3 | 26, 26, 26 |
| `cmd/liza/cmd_index_refresh.go` | other literal | flag default value (frozen CLI surface) | 1 | 39 |
| `cmd/liza/cmd_init.go` | message or display text | cobra help text (frozen --help output) | 11 | 32, 297, 302, 318, 342, 357, 358, 441, 442, 468, 469 |
| `cmd/liza/cmd_init.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 2 | 326, 328 |
| `cmd/liza/cmd_init.go` | message or display text | flag usage text (frozen --help output) | 38 | 679, 680, 687, 688, 689, 690, 691, 692, 693, 694, 695, 696, 697, 698, 701, 702, 703, 704, 705, 706, 707, 708, 709, 710, 711, 712, 713, 713, 714, 714, 715, 715, 716, 717, 718, 721, 734, 736 |
| `cmd/liza/cmd_init.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 40 | 43, 44, 45, 51, 52, 92, 93, 143, 158, 184, 187, 234, 237, 240, 243, 310, 330, 332, 348, 353, 413, 488, 490, 515, 566, 590, 591, 622, 637, 639, 641, 658, 719, 720, 722, 723, 724, 725, 726, 735 |
| `cmd/liza/cmd_init.go` | message or display text | separator of a rendered list (display text) | 1 | 515 |
| `cmd/liza/cmd_init.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 17 | 331, 424, 432, 476, 511, 610, 610, 647, 653, 679, 689, 703, 705, 706, 713, 714, 715 |
| `cmd/liza/cmd_init.go` | number | cobra argument arity of the command (frozen CLI surface) | 3 | 128, 365, 474 |
| `cmd/liza/cmd_init.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 620, 629 |
| `cmd/liza/cmd_init.go` | number | length, count or arity comparison against a small number of arguments or elements | 11 | 142, 156, 156, 232, 233, 419, 477, 546, 546, 578, 657 |
| `cmd/liza/cmd_init.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 639, 660 |
| `cmd/liza/cmd_init.go` | number | position in a fixed-layout slice or arguments list | 3 | 259, 420, 478 |
| `cmd/liza/cmd_init.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 43 | 679, 680, 687, 688, 689, 690, 691, 692, 693, 694, 695, 696, 697, 698, 701, 702, 703, 704, 705, 706, 707, 708, 709, 710, 711, 712, 713, 714, 715, 716, 717, 718, 719, 720, 721, 722, 723, 724, 725, 726, 734, 735, 736 |
| `cmd/liza/cmd_init.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 33 | 69, 70, 71, 131, 132, 133, 134, 135, 136, 137, 138, 139, 140, 141, 156, 156, 177, 178, 179, 180, 260, 261, 262, 263, 264, 265, 367, 368, 369, 460, 461, 523, 544 |
| `cmd/liza/cmd_init.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 5 | 727, 728, 729, 730, 731 |
| `cmd/liza/cmd_init.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 12 | 502, 502, 502, 502, 502, 502, 502, 502, 502, 502, 502, 502 |
| `cmd/liza/cmd_init.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 10 | 31, 50, 91, 296, 301, 317, 341, 356, 440, 467 |
| `cmd/liza/cmd_init.go` | wire key | JSON key of the version or validation result object (frozen --json bytes) | 4 | 36, 37, 38, 398 |
| `cmd/liza/cmd_init.go` | dynamic identity | entry-point name of the default pipeline (pipeline configuration, ADR-0045; masLaunchPreset names the three launch presets in cmd/liza) | 4 | 727, 727, 727, 727 |
| `cmd/liza/cmd_init.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 8 | 497, 497, 497, 497, 497, 497, 497, 497 |
| `cmd/liza/cmd_init.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 1 | 642 |
| `cmd/liza/cmd_init.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 3 | 145, 148, 151 |
| `cmd/liza/cmd_init.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 431, 651 |
| `cmd/liza/cmd_init.go` | other literal | flag default value (frozen CLI surface) | 2 | 701, 704 |
| `cmd/liza/cmd_launch.go` | message or display text | cobra help text (frozen --help output) | 4 | 30, 88, 137, 195 |
| `cmd/liza/cmd_launch.go` | message or display text | flag usage text (frozen --help output) | 19 | 252, 253, 255, 259, 260, 262, 265, 266, 273, 274, 275, 276, 277, 281, 282, 289, 290, 291, 292 |
| `cmd/liza/cmd_launch.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 25 | 15, 20, 25, 31, 49, 63, 68, 75, 97, 107, 121, 138, 156, 170, 175, 182, 204, 214, 228, 254, 261, 267, 268, 283, 284 |
| `cmd/liza/cmd_launch.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 14 | 62, 66, 169, 173, 252, 253, 254, 259, 260, 261, 267, 274, 283, 290 |
| `cmd/liza/cmd_launch.go` | number | cobra argument arity of the command (frozen CLI surface) | 2 | 89, 196 |
| `cmd/liza/cmd_launch.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 56, 163 |
| `cmd/liza/cmd_launch.go` | number | length, count or arity comparison against a small number of arguments or elements | 6 | 45, 74, 110, 152, 181, 217 |
| `cmd/liza/cmd_launch.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 56, 163 |
| `cmd/liza/cmd_launch.go` | number | position in a fixed-layout slice or arguments list | 2 | 95, 202 |
| `cmd/liza/cmd_launch.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 25 | 252, 253, 254, 255, 259, 260, 261, 262, 265, 266, 267, 268, 273, 274, 275, 276, 277, 281, 282, 283, 284, 289, 290, 291, 292 |
| `cmd/liza/cmd_launch.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 18 | 42, 44, 53, 55, 99, 100, 101, 105, 109, 149, 151, 160, 162, 206, 207, 208, 212, 216 |
| `cmd/liza/cmd_launch.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 10 | 269, 270, 271, 278, 279, 285, 286, 287, 293, 294 |
| `cmd/liza/cmd_launch.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 5 | 14, 29, 87, 136, 194 |
| `cmd/liza/cmd_launch.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 4 | 267, 268, 283, 284 |
| `cmd/liza/cmd_launch.go` | external argv | subcommand word of the relaunch argv that follows the branded binary name | 4 | 58, 65, 165, 172 |
| `cmd/liza/cmd_launch.go` | dynamic identity | default reviewer launch spec: provider identity pair (dynamic, ADR-0045) | 2 | 111, 218 |
| `cmd/liza/cmd_launch.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 2 | 111, 218 |
| `cmd/liza/cmd_launch.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 4 | 78, 127, 185, 234 |
| `cmd/liza/cmd_launch.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 70, 177 |
| `cmd/liza/cmd_launch.go` | other literal | flag default value (frozen CLI surface) | 2 | 273, 289 |
| `cmd/liza/cmd_plan_check.go` | message or display text | cobra help text (frozen --help output) | 2 | 23, 24 |
| `cmd/liza/cmd_plan_check.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 3 | 208, 210, 212 |
| `cmd/liza/cmd_plan_check.go` | message or display text | flag usage text (frozen --help output) | 8 | 234, 235, 236, 237, 238, 239, 240, 241 |
| `cmd/liza/cmd_plan_check.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 19 | 74, 122, 125, 135, 141, 147, 154, 169, 174, 177, 186, 189, 193, 198, 214, 216, 219, 222, 225 |
| `cmd/liza/cmd_plan_check.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 12 | 134, 140, 146, 207, 215, 218, 221, 235, 236, 238, 240, 241 |
| `cmd/liza/cmd_plan_check.go` | number | argument position the completion applies to | 1 | 231 |
| `cmd/liza/cmd_plan_check.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 51 |
| `cmd/liza/cmd_plan_check.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 121, 188 |
| `cmd/liza/cmd_plan_check.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 172 |
| `cmd/liza/cmd_plan_check.go` | number | position in a fixed-layout slice or arguments list | 1 | 64 |
| `cmd/liza/cmd_plan_check.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 115 |
| `cmd/liza/cmd_plan_check.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 8 | 234, 235, 236, 237, 238, 239, 240, 241 |
| `cmd/liza/cmd_plan_check.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 11 | 105, 106, 107, 108, 109, 110, 111, 112, 113, 152, 156 |
| `cmd/liza/cmd_plan_check.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 22 |
| `cmd/liza/cmd_provision.go` | message or display text | cobra help text (frozen --help output) | 1 | 21 |
| `cmd/liza/cmd_provision.go` | message or display text | flag usage text (frozen --help output) | 4 | 77, 78, 79, 80 |
| `cmd/liza/cmd_provision.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 22, 51, 54, 70 |
| `cmd/liza/cmd_provision.go` | message or display text | separator of a rendered list (display text) | 1 | 71 |
| `cmd/liza/cmd_provision.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 50, 79, 80 |
| `cmd/liza/cmd_provision.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 4 | 77, 78, 79, 80 |
| `cmd/liza/cmd_provision.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 4 | 53, 56, 57, 58 |
| `cmd/liza/cmd_provision.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 20 |
| `cmd/liza/cmd_provision.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 41, 41 |
| `cmd/liza/cmd_provision.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 50 |
| `cmd/liza/cmd_reaffirm_proof.go` | message or display text | cobra help text (frozen --help output) | 3 | 19, 20, 22 |
| `cmd/liza/cmd_reaffirm_proof.go` | message or display text | flag usage text (frozen --help output) | 2 | 102, 103 |
| `cmd/liza/cmd_reaffirm_proof.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 75, 78 |
| `cmd/liza/cmd_reaffirm_proof.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 102, 103 |
| `cmd/liza/cmd_reaffirm_proof.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 38 |
| `cmd/liza/cmd_reaffirm_proof.go` | number | position in a fixed-layout slice or arguments list | 2 | 70, 70 |
| `cmd/liza/cmd_reaffirm_proof.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 102, 103 |
| `cmd/liza/cmd_reaffirm_proof.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 68, 69 |
| `cmd/liza/cmd_reaffirm_proof.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 18 |
| `cmd/liza/cmd_reaffirm_proof.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 1 | 22 |
| `cmd/liza/cmd_reaffirm_proof.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 65 |
| `cmd/liza/cmd_reconcile_verdict.go` | message or display text | cobra help text (frozen --help output) | 3 | 19, 20, 22 |
| `cmd/liza/cmd_reconcile_verdict.go` | message or display text | flag usage text (frozen --help output) | 1 | 80 |
| `cmd/liza/cmd_reconcile_verdict.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 67, 69 |
| `cmd/liza/cmd_reconcile_verdict.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 80 |
| `cmd/liza/cmd_reconcile_verdict.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 29 |
| `cmd/liza/cmd_reconcile_verdict.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 82, 85 |
| `cmd/liza/cmd_reconcile_verdict.go` | number | position in a fixed-layout slice or arguments list | 9 | 60, 60, 60, 63, 63, 63, 69, 69, 69 |
| `cmd/liza/cmd_reconcile_verdict.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 80 |
| `cmd/liza/cmd_reconcile_verdict.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 59 |
| `cmd/liza/cmd_reconcile_verdict.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 18 |
| `cmd/liza/cmd_reconcile_verdict.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 1 | 22 |
| `cmd/liza/cmd_reconcile_verdict.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 3 | 63, 63, 63 |
| `cmd/liza/cmd_reconcile_verdict.go` | data | reconcile-verdict disposition offered by completion (the ops dispositions are unexported, table 5) | 4 | 86, 86, 86, 86 |
| `cmd/liza/cmd_reconcile_verdict.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 56 |
| `cmd/liza/cmd_recover_integration.go` | message or display text | cobra help text (frozen --help output) | 2 | 17, 18 |
| `cmd/liza/cmd_recover_integration.go` | message or display text | flag usage text (frozen --help output) | 2 | 69, 70 |
| `cmd/liza/cmd_recover_integration.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 5 | 52, 53, 55, 56, 58 |
| `cmd/liza/cmd_recover_integration.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 33, 60 |
| `cmd/liza/cmd_recover_integration.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 32, 69 |
| `cmd/liza/cmd_recover_integration.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 24 |
| `cmd/liza/cmd_recover_integration.go` | number | position in a fixed-layout slice or arguments list | 1 | 41 |
| `cmd/liza/cmd_recover_integration.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 69, 70 |
| `cmd/liza/cmd_recover_integration.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 1 | 71 |
| `cmd/liza/cmd_recover_integration.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 39, 40 |
| `cmd/liza/cmd_recover_integration.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 16 |
| `cmd/liza/cmd_recover_integration.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 32 |
| `cmd/liza/cmd_recover_integration.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 1 | 33 |
| `cmd/liza/cmd_rejection_rca.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 137, 141, 149, 151 |
| `cmd/liza/cmd_rejection_rca.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 136, 171 |
| `cmd/liza/cmd_rejection_rca.go` | number | argument position the completion applies to | 1 | 172 |
| `cmd/liza/cmd_rejection_rca.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 71 |
| `cmd/liza/cmd_rejection_rca.go` | number | position in a fixed-layout slice or arguments list | 2 | 123, 126 |
| `cmd/liza/cmd_rejection_rca.go` | flag or CLI spelling | flag name (frozen CLI surface) | 2 | 33, 51 |
| `cmd/liza/cmd_rejection_rca.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 4 | 162, 162, 163, 163 |
| `cmd/liza/cmd_rejection_rca.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 2 | 68, 68 |
| `cmd/liza/cmd_rejection_rca.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 4 | 46, 46, 46, 46 |
| `cmd/liza/cmd_rejection_rca.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 7 | 36, 37, 46, 46, 46, 54, 55 |
| `cmd/liza/cmd_release_blocked_chain.go` | message or display text | cobra help text (frozen --help output) | 1 | 18 |
| `cmd/liza/cmd_release_blocked_chain.go` | message or display text | flag usage text (frozen --help output) | 1 | 71 |
| `cmd/liza/cmd_release_blocked_chain.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 19, 43, 61 |
| `cmd/liza/cmd_release_blocked_chain.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 42, 71 |
| `cmd/liza/cmd_release_blocked_chain.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 34 |
| `cmd/liza/cmd_release_blocked_chain.go` | number | position in a fixed-layout slice or arguments list | 1 | 50 |
| `cmd/liza/cmd_release_blocked_chain.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 71 |
| `cmd/liza/cmd_release_blocked_chain.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 1 | 72 |
| `cmd/liza/cmd_release_blocked_chain.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 45 |
| `cmd/liza/cmd_release_blocked_chain.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 17 |
| `cmd/liza/cmd_release_blocked_chain.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 42 |
| `cmd/liza/cmd_repair_arch_ref.go` | message or display text | cobra help text (frozen --help output) | 2 | 16, 17 |
| `cmd/liza/cmd_repair_arch_ref.go` | message or display text | flag usage text (frozen --help output) | 2 | 64, 65 |
| `cmd/liza/cmd_repair_arch_ref.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 36, 55 |
| `cmd/liza/cmd_repair_arch_ref.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 35, 64, 65 |
| `cmd/liza/cmd_repair_arch_ref.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 27 |
| `cmd/liza/cmd_repair_arch_ref.go` | number | position in a fixed-layout slice or arguments list | 1 | 44 |
| `cmd/liza/cmd_repair_arch_ref.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 64, 65 |
| `cmd/liza/cmd_repair_arch_ref.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 2 | 66, 67 |
| `cmd/liza/cmd_repair_arch_ref.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 38, 39 |
| `cmd/liza/cmd_repair_arch_ref.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 15 |
| `cmd/liza/cmd_repair_arch_ref.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 35 |
| `cmd/liza/cmd_replace_task.go` | message or display text | cobra help text (frozen --help output) | 2 | 22, 23 |
| `cmd/liza/cmd_replace_task.go` | message or display text | flag usage text (frozen --help output) | 1 | 117 |
| `cmd/liza/cmd_replace_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 92, 96, 103, 106 |
| `cmd/liza/cmd_replace_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 91, 117 |
| `cmd/liza/cmd_replace_task.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 102 |
| `cmd/liza/cmd_replace_task.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 117 |
| `cmd/liza/cmd_replace_task.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 90 |
| `cmd/liza/cmd_replace_task.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 21 |
| `cmd/liza/cmd_replace_task.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 8 | 35, 35, 35, 35, 35, 35, 35, 35 |
| `cmd/liza/cmd_replace_task.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 1 | 103 |
| `cmd/liza/cmd_replace_task.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 71 |
| `cmd/liza/cmd_replace_task.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 98 |
| `cmd/liza/cmd_reserve_provider.go` | message or display text | cobra help text (frozen --help output) | 1 | 16 |
| `cmd/liza/cmd_reserve_provider.go` | message or display text | flag usage text (frozen --help output) | 4 | 90, 91, 92, 93 |
| `cmd/liza/cmd_reserve_provider.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 17, 76, 78, 80 |
| `cmd/liza/cmd_reserve_provider.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 90, 91 |
| `cmd/liza/cmd_reserve_provider.go` | number | argument position the completion applies to | 1 | 94 |
| `cmd/liza/cmd_reserve_provider.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 38 |
| `cmd/liza/cmd_reserve_provider.go` | number | flag default value (frozen CLI surface) | 1 | 93 |
| `cmd/liza/cmd_reserve_provider.go` | number | position in a fixed-layout slice or arguments list | 2 | 46, 46 |
| `cmd/liza/cmd_reserve_provider.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 4 | 90, 91, 92, 93 |
| `cmd/liza/cmd_reserve_provider.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 4 | 47, 48, 49, 50 |
| `cmd/liza/cmd_reserve_provider.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 15 |
| `cmd/liza/cmd_reserve_provider.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 37 |
| `cmd/liza/cmd_reserve_provider.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 64 |
| `cmd/liza/cmd_review.go` | message or display text | cobra help text (frozen --help output) | 20 | 42, 43, 48, 118, 119, 122, 200, 201, 206, 332, 333, 349, 414, 415, 420, 485, 486, 491, 600, 601 |
| `cmd/liza/cmd_review.go` | message or display text | flag usage text (frozen --help output) | 8 | 685, 686, 687, 688, 691, 692, 694, 695 |
| `cmd/liza/cmd_review.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 23 | 33, 34, 303, 312, 320, 324, 406, 476, 545, 553, 555, 558, 561, 564, 567, 570, 573, 578, 581, 583, 586, 589, 592 |
| `cmd/liza/cmd_review.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 22 | 35, 288, 294, 299, 303, 312, 320, 323, 554, 557, 560, 563, 566, 572, 579, 580, 585, 588, 685, 686, 687, 688 |
| `cmd/liza/cmd_review.go` | number | argument position the completion applies to | 6 | 654, 655, 665, 666, 667, 668 |
| `cmd/liza/cmd_review.go` | number | cobra argument arity of the command (frozen CLI surface) | 9 | 58, 58, 131, 229, 229, 351, 432, 501, 614 |
| `cmd/liza/cmd_review.go` | number | length, count or arity comparison against a small number of arguments or elements | 7 | 80, 289, 302, 569, 591, 657, 660 |
| `cmd/liza/cmd_review.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 318 |
| `cmd/liza/cmd_review.go` | number | position in a fixed-layout slice or arguments list | 12 | 78, 81, 151, 152, 153, 249, 250, 290, 371, 445, 514, 627 |
| `cmd/liza/cmd_review.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 405 |
| `cmd/liza/cmd_review.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 10 | 678, 682, 685, 686, 687, 688, 691, 692, 694, 695 |
| `cmd/liza/cmd_review.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 10 | 275, 276, 294, 298, 302, 372, 373, 374, 375, 402 |
| `cmd/liza/cmd_review.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 1 | 693 |
| `cmd/liza/cmd_review.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 7 | 41, 117, 199, 331, 413, 484, 599 |
| `cmd/liza/cmd_review.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 6 | 48, 122, 206, 349, 420, 491 |
| `cmd/liza/cmd_review.go` | data | completion candidate value (frozen completion output) | 2 | 661, 661 |
| `cmd/liza/cmd_review.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 5 | 99, 170, 271, 462, 531 |
| `cmd/liza/cmd_review.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 35 |
| `cmd/liza/cmd_review.go` | other literal | flag default value (frozen CLI surface) | 1 | 695 |
| `cmd/liza/cmd_run_live.go` | message or display text | cobra help text (frozen --help output) | 1 | 15 |
| `cmd/liza/cmd_run_live.go` | message or display text | flag usage text (frozen --help output) | 2 | 73, 74 |
| `cmd/liza/cmd_run_live.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 16, 36 |
| `cmd/liza/cmd_run_live.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 35, 52, 73 |
| `cmd/liza/cmd_run_live.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 31 |
| `cmd/liza/cmd_run_live.go` | number | flag default value (frozen CLI surface) | 1 | 74 |
| `cmd/liza/cmd_run_live.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 64 |
| `cmd/liza/cmd_run_live.go` | number | lower bound clamp of a computed width, height or count | 1 | 66 |
| `cmd/liza/cmd_run_live.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 73, 74 |
| `cmd/liza/cmd_run_live.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 34, 38 |
| `cmd/liza/cmd_run_live.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 14 |
| `cmd/liza/cmd_run_live.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 30, 30 |
| `cmd/liza/cmd_semble_ready.go` | message or display text | cobra help text (frozen --help output) | 1 | 14 |
| `cmd/liza/cmd_semble_ready.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 19 |
| `cmd/liza/cmd_semble_ready.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 16 |
| `cmd/liza/cmd_semble_ready.go` | number | position in a fixed-layout slice or arguments list | 1 | 18 |
| `cmd/liza/cmd_semble_ready.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 13 |
| `cmd/liza/cmd_system.go` | message or display text | cobra help text (frozen --help output) | 17 | 27, 77, 78, 185, 186, 331, 332, 344, 345, 347, 369, 370, 402, 456, 457, 465, 468 |
| `cmd/liza/cmd_system.go` | message or display text | flag usage text (frozen --help output) | 8 | 529, 530, 531, 541, 542, 545, 548, 551 |
| `cmd/liza/cmd_system.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 17 | 28, 49, 130, 131, 149, 160, 174, 224, 226, 234, 235, 267, 268, 296, 297, 426, 427 |
| `cmd/liza/cmd_system.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 493, 529, 531, 545, 548, 551 |
| `cmd/liza/cmd_system.go` | number | argument position the completion applies to | 1 | 523 |
| `cmd/liza/cmd_system.go` | number | cobra argument arity of the command (frozen CLI surface) | 2 | 353, 404 |
| `cmd/liza/cmd_system.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 159, 223, 408 |
| `cmd/liza/cmd_system.go` | number | number of positional arguments the completion switch handles | 2 | 515, 517 |
| `cmd/liza/cmd_system.go` | number | position in a fixed-layout slice or arguments list | 3 | 355, 356, 409 |
| `cmd/liza/cmd_system.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 8 | 529, 530, 531, 541, 542, 545, 548, 551 |
| `cmd/liza/cmd_system.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 8 | 145, 158, 253, 282, 317, 417, 418, 419 |
| `cmd/liza/cmd_system.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 11 | 26, 76, 128, 184, 233, 266, 295, 330, 368, 425, 454 |
| `cmd/liza/cmd_system.go` | cobra definition | command alias (frozen CLI surface) | 2 | 129, 455 |
| `cmd/liza/cmd_system.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 13 | 49, 251, 315, 315, 315, 315, 315, 345, 347, 402, 440, 440, 468 |
| `cmd/liza/cmd_system.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 1 | 315 |
| `cmd/liza/cmd_system.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 1 | 220 |
| `cmd/liza/cmd_system_inspect.go` | message or display text | cobra help text (frozen --help output) | 3 | 25, 179, 198 |
| `cmd/liza/cmd_system_inspect.go` | message or display text | flag usage text (frozen --help output) | 8 | 286, 287, 288, 289, 290, 291, 297, 298 |
| `cmd/liza/cmd_system_inspect.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 2 | 112, 116 |
| `cmd/liza/cmd_system_inspect.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 26, 180, 199, 250, 294 |
| `cmd/liza/cmd_system_inspect.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 127, 286, 297 |
| `cmd/liza/cmd_system_inspect.go` | number | argument position the completion applies to | 1 | 279 |
| `cmd/liza/cmd_system_inspect.go` | number | cobra argument arity of the command (frozen CLI surface) | 2 | 94, 190 |
| `cmd/liza/cmd_system_inspect.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 114, 124 |
| `cmd/liza/cmd_system_inspect.go` | number | position in a fixed-layout slice or arguments list | 1 | 114 |
| `cmd/liza/cmd_system_inspect.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 9 | 286, 287, 288, 289, 290, 291, 294, 297, 298 |
| `cmd/liza/cmd_system_inspect.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 10 | 107, 108, 109, 110, 121, 122, 123, 124, 232, 233 |
| `cmd/liza/cmd_system_inspect.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 2 | 292, 299 |
| `cmd/liza/cmd_system_inspect.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 24, 178, 197 |
| `cmd/liza/cmd_system_inspect.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 7 | 93, 188, 188, 188, 189, 189, 218 |
| `cmd/liza/cmd_task.go` | message or display text | cobra help text (frozen --help output) | 7 | 30, 31, 94, 95, 298, 299, 333 |
| `cmd/liza/cmd_task.go` | message or display text | flag usage text (frozen --help output) | 15 | 428, 429, 430, 431, 432, 433, 435, 436, 437, 438, 439, 440, 441, 442, 449 |
| `cmd/liza/cmd_task.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 358, 360 |
| `cmd/liza/cmd_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 9 | 148, 150, 176, 348, 353, 362, 434, 443, 444 |
| `cmd/liza/cmd_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 25 | 144, 144, 147, 147, 149, 149, 156, 197, 279, 347, 428, 429, 430, 431, 432, 435, 437, 438, 439, 440, 441, 442, 443, 444, 449 |
| `cmd/liza/cmd_task.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 43 |
| `cmd/liza/cmd_task.go` | number | flag default value (frozen CLI surface) | 1 | 436 |
| `cmd/liza/cmd_task.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 224, 359, 412, 415 |
| `cmd/liza/cmd_task.go` | number | position in a fixed-layout slice or arguments list | 3 | 45, 46, 359 |
| `cmd/liza/cmd_task.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 18 | 428, 429, 430, 431, 432, 433, 434, 435, 436, 437, 438, 439, 440, 441, 442, 443, 444, 449 |
| `cmd/liza/cmd_task.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 36 | 141, 142, 153, 166, 167, 169, 170, 172, 173, 180, 181, 183, 184, 186, 187, 189, 190, 192, 193, 195, 196, 203, 204, 206, 207, 209, 209, 209, 213, 214, 216, 217, 219, 220, 279, 346 |
| `cmd/liza/cmd_task.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 1 | 445 |
| `cmd/liza/cmd_task.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 29, 93, 297 |
| `cmd/liza/cmd_task.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 333 |
| `cmd/liza/cmd_task.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 284, 434 |
| `cmd/liza/cmd_task.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 279 |
| `cmd/liza/cmd_task.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 1 | 377 |
| `cmd/liza/cmd_task.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 1 | 359 |
| `cmd/liza/cmd_task.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 1 | 198 |
| `cmd/liza/cmd_task_blocked.go` | message or display text | cobra help text (frozen --help output) | 9 | 22, 23, 31, 105, 106, 364, 365, 491, 492 |
| `cmd/liza/cmd_task_blocked.go` | message or display text | flag usage text (frozen --help output) | 39 | 579, 580, 581, 582, 583, 584, 585, 586, 587, 588, 589, 590, 597, 597, 598, 599, 600, 601, 602, 603, 604, 605, 611, 612, 613, 614, 615, 616, 617, 618, 619, 620, 621, 622, 623, 624, 625, 629, 630 |
| `cmd/liza/cmd_task_blocked.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 22 | 235, 240, 244, 255, 261, 285, 300, 304, 310, 313, 317, 330, 333, 336, 339, 342, 453, 462, 473, 476, 479, 483 |
| `cmd/liza/cmd_task_blocked.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 36 | 154, 159, 284, 285, 299, 312, 329, 332, 335, 355, 461, 472, 482, 579, 581, 582, 583, 586, 588, 590, 597, 598, 599, 600, 603, 605, 611, 612, 613, 615, 616, 617, 620, 623, 629, 630 |
| `cmd/liza/cmd_task_blocked.go` | number | argument position the completion applies to | 4 | 565, 566, 567, 568 |
| `cmd/liza/cmd_task_blocked.go` | number | cobra argument arity of the command (frozen CLI surface) | 4 | 46, 131, 387, 504 |
| `cmd/liza/cmd_task_blocked.go` | number | flag default value (frozen CLI surface) | 1 | 604 |
| `cmd/liza/cmd_task_blocked.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 452, 475 |
| `cmd/liza/cmd_task_blocked.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 238 |
| `cmd/liza/cmd_task_blocked.go` | number | position in a fixed-layout slice or arguments list | 4 | 66, 151, 407, 524 |
| `cmd/liza/cmd_task_blocked.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 38 | 579, 580, 581, 582, 583, 584, 585, 586, 587, 588, 589, 590, 597, 598, 599, 600, 601, 602, 603, 604, 605, 611, 612, 613, 614, 615, 616, 617, 618, 619, 620, 621, 622, 623, 624, 625, 629, 630 |
| `cmd/liza/cmd_task_blocked.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 3 | 591, 592, 606 |
| `cmd/liza/cmd_task_blocked.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 38 | 68, 69, 152, 153, 156, 157, 158, 161, 162, 165, 166, 170, 171, 250, 250, 258, 259, 267, 276, 283, 284, 291, 292, 293, 294, 295, 296, 298, 409, 448, 449, 450, 451, 459, 460, 468, 468, 526 |
| `cmd/liza/cmd_task_blocked.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 6 | 593, 594, 607, 608, 626, 631 |
| `cmd/liza/cmd_task_blocked.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 10 | 302, 302, 302, 302, 302, 323, 323, 323, 323, 323 |
| `cmd/liza/cmd_task_blocked.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 4 | 21, 104, 363, 490 |
| `cmd/liza/cmd_task_blocked.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 31, 597 |
| `cmd/liza/cmd_task_blocked.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 2 | 89, 219 |
| `cmd/liza/cmd_task_blocked.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 576 |
| `cmd/liza/cmd_task_dependencies.go` | message or display text | cobra help text (frozen --help output) | 5 | 22, 112, 229, 318, 394 |
| `cmd/liza/cmd_task_dependencies.go` | message or display text | flag usage text (frozen --help output) | 9 | 491, 492, 493, 496, 499, 500, 501, 505, 508 |
| `cmd/liza/cmd_task_dependencies.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 23, 113, 230, 319, 395 |
| `cmd/liza/cmd_task_dependencies.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 9 | 491, 492, 493, 496, 499, 500, 501, 505, 508 |
| `cmd/liza/cmd_task_dependencies.go` | number | argument position the completion applies to | 5 | 478, 479, 480, 481, 482 |
| `cmd/liza/cmd_task_dependencies.go` | number | cobra argument arity of the command (frozen CLI surface) | 6 | 46, 46, 125, 259, 328, 403 |
| `cmd/liza/cmd_task_dependencies.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 73 |
| `cmd/liza/cmd_task_dependencies.go` | number | position in a fixed-layout slice or arguments list | 8 | 66, 74, 145, 146, 147, 279, 348, 423 |
| `cmd/liza/cmd_task_dependencies.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 9 | 491, 492, 493, 496, 499, 500, 501, 505, 508 |
| `cmd/liza/cmd_task_dependencies.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 6 | 494, 497, 502, 503, 506, 509 |
| `cmd/liza/cmd_task_dependencies.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 11 | 68, 69, 70, 148, 159, 188, 280, 281, 282, 349, 424 |
| `cmd/liza/cmd_task_dependencies.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 5 | 21, 111, 228, 317, 393 |
| `cmd/liza/cmd_task_dependencies.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 7 | 45, 45, 45, 124, 258, 327, 402 |
| `cmd/liza/cmd_task_dependencies.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 5 | 93, 181, 303, 379, 454 |
| `cmd/liza/cmd_task_dependencies.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 74, 147 |
| `cmd/liza/cmd_task_dependencies.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 512 |
| `cmd/liza/cmd_task_records.go` | message or display text | cobra help text (frozen --help output) | 9 | 22, 23, 26, 106, 107, 153, 168, 261, 262 |
| `cmd/liza/cmd_task_records.go` | message or display text | flag usage text (frozen --help output) | 9 | 313, 314, 315, 316, 317, 318, 319, 320, 326 |
| `cmd/liza/cmd_task_records.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 236 |
| `cmd/liza/cmd_task_records.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 221, 223 |
| `cmd/liza/cmd_task_records.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 91, 211, 216, 225 |
| `cmd/liza/cmd_task_records.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 9 | 87, 210, 313, 314, 317, 318, 319, 320, 326 |
| `cmd/liza/cmd_task_records.go` | number | argument position the completion applies to | 2 | 304, 305 |
| `cmd/liza/cmd_task_records.go` | number | cobra argument arity of the command (frozen CLI surface) | 3 | 33, 169, 268 |
| `cmd/liza/cmd_task_records.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 222, 234 |
| `cmd/liza/cmd_task_records.go` | number | position in a fixed-layout slice or arguments list | 5 | 46, 189, 222, 281, 282 |
| `cmd/liza/cmd_task_records.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 9 | 313, 314, 315, 316, 317, 318, 319, 320, 326 |
| `cmd/liza/cmd_task_records.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 2 | 321, 322 |
| `cmd/liza/cmd_task_records.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 9 | 66, 67, 68, 69, 70, 71, 72, 87, 209 |
| `cmd/liza/cmd_task_records.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 21, 105, 260 |
| `cmd/liza/cmd_task_records.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 168, 168 |
| `cmd/liza/cmd_task_records.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 26, 153 |
| `cmd/liza/cmd_task_records.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 1 | 230 |
| `cmd/liza/cmd_task_records.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 2 | 62, 205 |
| `cmd/liza/cmd_task_records.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 1 | 222 |
| `cmd/liza/cmd_task_retire.go` | message or display text | cobra help text (frozen --help output) | 7 | 19, 20, 31, 37, 104, 167, 168 |
| `cmd/liza/cmd_task_retire.go` | message or display text | flag usage text (frozen --help output) | 6 | 202, 203, 204, 213, 214, 215 |
| `cmd/liza/cmd_task_retire.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 105, 157, 159 |
| `cmd/liza/cmd_task_retire.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 202, 203, 204 |
| `cmd/liza/cmd_task_retire.go` | number | argument position the completion applies to | 3 | 193, 194, 195 |
| `cmd/liza/cmd_task_retire.go` | number | cobra argument arity of the command (frozen CLI surface) | 3 | 38, 112, 172 |
| `cmd/liza/cmd_task_retire.go` | number | position in a fixed-layout slice or arguments list | 4 | 58, 59, 125, 174 |
| `cmd/liza/cmd_task_retire.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 6 | 202, 203, 204, 213, 214, 215 |
| `cmd/liza/cmd_task_retire.go` | flag or CLI spelling | flag name marked required (frozen CLI surface) | 2 | 205, 206 |
| `cmd/liza/cmd_task_retire.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 6 | 126, 127, 128, 175, 176, 177 |
| `cmd/liza/cmd_task_retire.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 18, 103, 166 |
| `cmd/liza/cmd_task_retire.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 4 | 37, 37, 111, 111 |
| `cmd/liza/cmd_task_retire.go` | dynamic identity | operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | 2 | 89, 146 |
| `cmd/liza/cmd_task_retire.go` | other literal | flag default value (frozen CLI surface) | 1 | 215 |
| `cmd/liza/cmd_task_retire.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 209 |
| `cmd/liza/cmd_tool_result.go` | message or display text | cobra help text (frozen --help output) | 4 | 17, 31, 95, 118 |
| `cmd/liza/cmd_tool_result.go` | message or display text | flag usage text (frozen --help output) | 15 | 18, 19, 20, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 112, 113 |
| `cmd/liza/cmd_tool_result.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 81, 84, 88 |
| `cmd/liza/cmd_tool_result.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 8 | 18, 62, 63, 64, 65, 66, 67, 68 |
| `cmd/liza/cmd_tool_result.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 95 |
| `cmd/liza/cmd_tool_result.go` | number | flag default value (frozen CLI surface) | 3 | 69, 112, 113 |
| `cmd/liza/cmd_tool_result.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 102 |
| `cmd/liza/cmd_tool_result.go` | number | position in a fixed-layout slice or arguments list | 1 | 105 |
| `cmd/liza/cmd_tool_result.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 15 | 18, 19, 20, 61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 112, 113 |
| `cmd/liza/cmd_tool_result.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 46 |
| `cmd/liza/cmd_tool_result.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 4 | 17, 31, 95, 118 |
| `cmd/liza/cmd_tool_result_acp.go` | message or display text | cobra help text (frozen --help output) | 1 | 16 |
| `cmd/liza/cmd_tool_result_acp.go` | message or display text | flag usage text (frozen --help output) | 4 | 33, 34, 35, 36 |
| `cmd/liza/cmd_tool_result_acp.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 33, 34, 35, 36 |
| `cmd/liza/cmd_tool_result_acp.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 16 |
| `cmd/liza/cmd_tool_result_acp.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 4 | 33, 34, 35, 36 |
| `cmd/liza/cmd_tool_result_acp.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 23, 26 |
| `cmd/liza/cmd_tool_result_acp.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 16 |
| `cmd/liza/cmd_tool_result_acp.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 2 | 24, 27 |
| `cmd/liza/cmd_tool_result_claude.go` | message or display text | cobra help text (frozen --help output) | 1 | 13 |
| `cmd/liza/cmd_tool_result_claude.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 13 |
| `cmd/liza/cmd_tool_result_claude.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 3 | 25, 25, 25 |
| `cmd/liza/cmd_tool_result_claude.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 3 | 15, 15, 15 |
| `cmd/liza/cmd_tool_result_codex.go` | message or display text | cobra help text (frozen --help output) | 1 | 16 |
| `cmd/liza/cmd_tool_result_codex.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 32 |
| `cmd/liza/cmd_tool_result_codex.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 19 |
| `cmd/liza/cmd_tool_result_codex.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 16 |
| `cmd/liza/cmd_tool_result_codex.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 3 | 29, 29, 29 |
| `cmd/liza/cmd_tool_result_codex.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 8 | 49, 49, 49, 49, 49, 51, 51, 51 |
| `cmd/liza/cmd_tool_result_run.go` | message or display text | cobra help text (frozen --help output) | 1 | 21 |
| `cmd/liza/cmd_tool_result_run.go` | message or display text | flag usage text (frozen --help output) | 7 | 45, 46, 47, 48, 49, 50, 51 |
| `cmd/liza/cmd_tool_result_run.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 17 |
| `cmd/liza/cmd_tool_result_run.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 46, 47, 48, 49, 50, 51 |
| `cmd/liza/cmd_tool_result_run.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 21 |
| `cmd/liza/cmd_tool_result_run.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 40 |
| `cmd/liza/cmd_tool_result_run.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 7 | 45, 46, 47, 48, 49, 50, 51 |
| `cmd/liza/cmd_tool_result_run.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 28, 31 |
| `cmd/liza/cmd_tool_result_run.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 21 |
| `cmd/liza/cmd_tool_result_run.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 2 | 29, 32 |
| `cmd/liza/cmd_tool_result_run.go` | other literal | flag default value (frozen CLI surface) | 1 | 45 |
| `cmd/liza/cmd_tool_result_store.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 33, 57 |
| `cmd/liza/cmd_tool_result_store.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 54, 66, 70, 74 |
| `cmd/liza/cmd_tool_result_store.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 69 |
| `cmd/liza/cmd_tool_result_store.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 4 | 48, 48, 49, 49 |
| `cmd/liza/cmd_tool_result_store.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 77 |
| `cmd/liza/cmd_toolchain.go` | message or display text | cobra help text (frozen --help output) | 2 | 47, 125 |
| `cmd/liza/cmd_toolchain.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 1 | 37 |
| `cmd/liza/cmd_toolchain.go` | message or display text | flag usage text (frozen --help output) | 13 | 203, 204, 205, 206, 207, 209, 210, 211, 212, 213, 234, 235, 236 |
| `cmd/liza/cmd_toolchain.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 27 | 22, 23, 28, 39, 67, 69, 72, 82, 94, 106, 138, 142, 173, 174, 175, 177, 178, 181, 184, 187, 208, 245, 258, 262, 286, 288, 291 |
| `cmd/liza/cmd_toolchain.go` | message or display text | separator of a rendered list (display text) | 2 | 291, 345 |
| `cmd/liza/cmd_toolchain.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 25 | 36, 68, 71, 92, 137, 137, 141, 160, 160, 176, 183, 186, 186, 204, 205, 208, 209, 212, 213, 245, 287, 290, 322, 357, 364 |
| `cmd/liza/cmd_toolchain.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 256 |
| `cmd/liza/cmd_toolchain.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 141, 303 |
| `cmd/liza/cmd_toolchain.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 14 | 203, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 234, 235, 236 |
| `cmd/liza/cmd_toolchain.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 14 | 53, 88, 89, 90, 91, 131, 132, 133, 134, 135, 136, 240, 241, 242 |
| `cmd/liza/cmd_toolchain.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 12 | 214, 215, 216, 217, 218, 219, 220, 221, 222, 223, 224, 225 |
| `cmd/liza/cmd_toolchain.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 5 | 21, 27, 46, 81, 124 |
| `cmd/liza/cmd_toolchain.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 326, 360 |
| `cmd/liza/cmd_toolchain.go` | other literal | environment variable name of an external tool or test hook | 2 | 323, 323 |
| `cmd/liza/cmd_toolchain.go` | other literal | flag default value (frozen CLI surface) | 2 | 203, 210 |
| `cmd/liza/cmd_usage.go` | message or display text | cobra help text (frozen --help output) | 2 | 20, 25 |
| `cmd/liza/cmd_usage.go` | message or display text | flag usage text (frozen --help output) | 8 | 107, 108, 109, 110, 111, 112, 113, 114 |
| `cmd/liza/cmd_usage.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 68, 78 |
| `cmd/liza/cmd_usage.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 108, 109, 110, 111, 112, 113, 114 |
| `cmd/liza/cmd_usage.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 8 | 107, 108, 109, 110, 111, 112, 113, 114 |
| `cmd/liza/cmd_usage.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 8 | 40, 41, 42, 43, 44, 45, 46, 47 |
| `cmd/liza/cmd_usage.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 2 | 19, 24 |
| `cmd/liza/cmd_usage.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 5 | 74, 74, 74, 99, 99 |
| `cmd/liza/cmd_validate_payload.go` | message or display text | cobra help text (frozen --help output) | 3 | 22, 23, 38 |
| `cmd/liza/cmd_validate_payload.go` | message or display text | flag usage text (frozen --help output) | 2 | 107, 108 |
| `cmd/liza/cmd_validate_payload.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 55, 66, 70, 101 |
| `cmd/liza/cmd_validate_payload.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 69, 107 |
| `cmd/liza/cmd_validate_payload.go` | number | cobra argument arity of the command (frozen CLI surface) | 1 | 40 |
| `cmd/liza/cmd_validate_payload.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 54, 65 |
| `cmd/liza/cmd_validate_payload.go` | number | position in a fixed-layout slice or arguments list | 1 | 73 |
| `cmd/liza/cmd_validate_payload.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 107, 108 |
| `cmd/liza/cmd_validate_payload.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 2 | 53, 68 |
| `cmd/liza/cmd_validate_payload.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 21 |
| `cmd/liza/cmd_validate_payload.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 8 | 38, 38, 38, 39, 39, 39, 39, 39 |
| `cmd/liza/cmd_worktree.go` | message or display text | cobra help text (frozen --help output) | 7 | 19, 20, 60, 61, 104, 105, 112 |
| `cmd/liza/cmd_worktree.go` | message or display text | flag usage text (frozen --help output) | 1 | 189 |
| `cmd/liza/cmd_worktree.go` | number | argument position the completion applies to | 3 | 181, 182, 183 |
| `cmd/liza/cmd_worktree.go` | number | cobra argument arity of the command (frozen CLI surface) | 3 | 29, 74, 125 |
| `cmd/liza/cmd_worktree.go` | number | position in a fixed-layout slice or arguments list | 3 | 31, 76, 127 |
| `cmd/liza/cmd_worktree.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 1 | 189 |
| `cmd/liza/cmd_worktree.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 32 |
| `cmd/liza/cmd_worktree.go` | flag or CLI spelling | flag name the completion function attaches to (frozen CLI surface) | 1 | 186 |
| `cmd/liza/cmd_worktree.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 3 | 18, 59, 103 |
| `cmd/liza/cmd_worktree.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 1 | 112 |
| `cmd/liza/completion_values.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 167, 171, 185, 199, 212 |
| `cmd/liza/completion_values.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 4 | 56, 183, 197, 209 |
| `cmd/liza/completion_values.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 65, 136 |
| `cmd/liza/completion_values.go` | number | number of positional arguments the completion switch handles | 2 | 73, 75 |
| `cmd/liza/completion_values.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 1 | 169 |
| `cmd/liza/completion_values.go` | data | field-query completion candidate (frozen completion output) | 4 | 118, 120, 121, 122 |
| `cmd/liza/completion_values.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 1 | 42 |
| `cmd/liza/launch_build.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 40 | 28, 30, 32, 34, 36, 38, 45, 47, 48, 49, 50, 51, 53, 54, 56, 57, 59, 60, 61, 67, 69, 71, 73, 75, 77, 79, 81, 83, 86, 88, 89, 91, 97, 127, 128, 136, 166, 173, 228, 230 |
| `cmd/liza/launch_build.go` | message or display text | placeholder shown in the dry-run launch script (display text) | 3 | 112, 151, 175 |
| `cmd/liza/launch_build.go` | message or display text | separator of a rendered list (display text) | 1 | 216 |
| `cmd/liza/launch_build.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 220 |
| `cmd/liza/launch_build.go` | number | length, count or arity comparison against a small number of arguments or elements | 10 | 25, 64, 96, 117, 135, 156, 174, 227, 238, 245 |
| `cmd/liza/launch_build.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 12 | 23, 25, 62, 64, 67, 115, 117, 127, 128, 154, 156, 166 |
| `cmd/liza/launch_build.go` | number | position in a fixed-layout slice or arguments list | 5 | 37, 87, 90, 107, 146 |
| `cmd/liza/launch_build.go` | external argv | cmux or wezterm CLI argument (external CLI vocabulary; the launch contract is pinned by the A7 launch oracle) | 39 | 104, 105, 106, 107, 121, 122, 122, 123, 124, 127, 127, 127, 128, 128, 128, 128, 143, 144, 145, 146, 160, 161, 161, 162, 163, 167, 167, 167, 168, 168, 168, 168, 179, 180, 181, 188, 189, 190, 191 |
| `cmd/liza/launch_build.go` | character | POSIX shell single-quote syntax of shellQuote | 3 | 221, 223, 223 |
| `cmd/liza/launch_build.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 8 | 238, 238, 238, 238, 238, 238, 238, 242 |
| `cmd/liza/launch_build.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 2 | 44, 44 |
| `cmd/liza/launch_build.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 4 | 223, 223, 231, 232 |
| `cmd/liza/launch_build.go` | other literal | name of the shell function the generated wezterm script defines | 1 | 246 |
| `cmd/liza/launch_build.go` | other literal | pairing skill invocation text composed into the launch prompt | 3 | 200, 200, 202 |
| `cmd/liza/launch_exec.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 26 | 39, 43, 61, 75, 79, 97, 111, 124, 136, 164, 167, 175, 188, 200, 226, 234, 237, 266, 270, 278, 284, 290, 297, 314, 329, 381 |
| `cmd/liza/launch_exec.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 290, 338, 368, 381, 392 |
| `cmd/liza/launch_exec.go` | number | external CLI argument (cmux workspace position) | 4 | 134, 155, 198, 224 |
| `cmd/liza/launch_exec.go` | number | length, count or arity comparison against a small number of arguments or elements | 7 | 38, 74, 110, 146, 174, 215, 371 |
| `cmd/liza/launch_exec.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 4 | 144, 146, 213, 215 |
| `cmd/liza/launch_exec.go` | number | position in a fixed-layout slice or arguments list | 7 | 132, 134, 155, 196, 198, 224, 371 |
| `cmd/liza/launch_exec.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 268 |
| `cmd/liza/launch_exec.go` | external argv | cmux or wezterm CLI argument (external CLI vocabulary; the launch contract is pinned by the A7 launch oracle) | 30 | 47, 48, 49, 50, 51, 53, 83, 84, 85, 86, 87, 89, 129, 130, 131, 132, 150, 151, 151, 152, 153, 193, 194, 195, 196, 219, 220, 220, 221, 222 |
| `cmd/liza/launch_exec.go` | external argv | external command argument: cmux, wezterm or Git vocabulary (not a Liza set; C4 Git literals are preserved) | 12 | 257, 257, 257, 261, 261, 261, 288, 288, 336, 336, 336, 336 |
| `cmd/liza/launch_exec.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 2 | 369, 371 |
| `cmd/liza/launch_exec.go` | other literal | POSIX shell path of the launcher script | 1 | 395 |
| `cmd/liza/launch_exec.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 9 | 138, 159, 166, 202, 228, 236, 269, 292, 313 |
| `cmd/liza/launch_exec.go` | other literal | cmux reference separator (kind:number) | 1 | 375 |
| `cmd/liza/launch_exec.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 6 | 344, 344, 348, 348, 352, 364 |
| `cmd/liza/launch_exec.go` | other literal | environment variable name of an external tool or test hook | 1 | 392 |
| `cmd/liza/launch_exec.go` | other literal | provider UI text the cmux readiness probe waits for in the pane | 3 | 356, 357, 358 |
| `cmd/liza/launch_exec.go` | platform identifier | runtime.GOOS platform identifier (Go names no constant) | 1 | 389 |
| `cmd/liza/launch_pairing_seed.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 2 | 52, 54 |
| `cmd/liza/launch_pairing_seed.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 23, 26, 32, 37, 41, 44, 46, 56 |
| `cmd/liza/launch_pairing_seed.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 25 |
| `cmd/liza/launch_resolve.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 13 | 100, 114, 122, 125, 129, 134, 138, 141, 145, 147, 173, 211, 218 |
| `cmd/liza/launch_resolve.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 16 | 97, 100, 104, 114, 161, 165, 181, 183, 194, 210, 211, 211, 217, 217, 218, 218 |
| `cmd/liza/launch_resolve.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 172 |
| `cmd/liza/launch_resolve.go` | number | position in a fixed-layout slice or arguments list | 2 | 215, 216 |
| `cmd/liza/launch_resolve.go` | number | split limit of an id=cli pair | 1 | 214 |
| `cmd/liza/launch_resolve.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 9 | 96, 160, 164, 168, 170, 171, 180, 182, 189 |
| `cmd/liza/launch_resolve.go` | external argv | external command argument: cmux, wezterm or Git vocabulary (not a Liza set; C4 Git literals are preserved) | 4 | 127, 127, 127, 127 |
| `cmd/liza/launch_resolve.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 143 |
| `cmd/liza/launch_resolve.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 213, 214 |
| `cmd/liza/lifecycle.go` | message or display text | flag usage text (frozen --help output) | 2 | 65, 66 |
| `cmd/liza/lifecycle.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 71, 77, 86, 89, 145 |
| `cmd/liza/lifecycle.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 8 | 56, 56, 65, 66, 85, 85, 91, 91 |
| `cmd/liza/lifecycle.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 25 |
| `cmd/liza/lifecycle.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 108 |
| `cmd/liza/lifecycle.go` | number | position in a fixed-layout slice or arguments list | 1 | 109 |
| `cmd/liza/lifecycle.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 2 | 65, 66 |
| `cmd/liza/lifecycle.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 4 | 82, 83, 85, 85 |
| `cmd/liza/lifecycle.go` | cobra definition | command name compared to skip the lifecycle context for that command | 1 | 108 |
| `cmd/liza/main.go` | message or display text | cobra help text (frozen --help output) | 2 | 45, 46 |
| `cmd/liza/main.go` | message or display text | flag usage text (frozen --help output) | 10 | 312, 313, 321, 321, 322, 327, 327, 332, 343, 343 |
| `cmd/liza/main.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 19 | 34, 35, 76, 90, 93, 106, 114, 123, 135, 138, 159, 170, 185, 212, 220, 275, 355, 360, 375 |
| `cmd/liza/main.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 29 | 51, 57, 61, 65, 69, 72, 85, 92, 105, 113, 122, 131, 134, 147, 159, 207, 212, 215, 216, 232, 244, 256, 262, 275, 299, 303, 313, 327, 343 |
| `cmd/liza/main.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 228, 237 |
| `cmd/liza/main.go` | flag or CLI spelling | flag name defined on the command (frozen CLI surface) | 7 | 312, 313, 321, 322, 327, 332, 343 |
| `cmd/liza/main.go` | flag or CLI spelling | flag name read from the parsed command line (frozen CLI surface) | 8 | 50, 153, 179, 205, 210, 251, 285, 337 |
| `cmd/liza/main.go` | flag or CLI spelling | flag shorthand letter (frozen CLI surface) | 2 | 312, 313 |
| `cmd/liza/main.go` | flag or CLI spelling | flag spelling compared in argument scanning (frozen CLI surface) | 1 | 195 |
| `cmd/liza/main.go` | cobra definition | cobra Use: command name and argument grammar (frozen CLI surface) | 1 | 44 |
| `cmd/liza/main.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 3 | 159, 327, 343 |
| `cmd/liza/main.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 301 |
| `cmd/liza/main.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 6 | 227, 228, 229, 235, 235, 236 |
| `cmd/liza/main.go` | other literal | flag default value (frozen CLI surface) | 1 | 322 |
| `cmd/liza/rbac.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 53, 63, 75, 92, 101, 114 |
| `cmd/liza/rbac.go` | message or display text | separator of a rendered list (display text) | 1 | 86 |
| `cmd/liza/rbac.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 38 |
| `cmd/liza/rbac.go` | dynamic identity | operation label recorded in a classified error (pass-through) | 2 | 29, 40 |
| `cmd/liza/rbac.go` | other literal | brackets around the allowed-types label of the permission message | 2 | 86, 86 |
| `cmd/liza/supervisor_logs.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 38, 44, 51, 77, 87, 111, 112, 123, 124, 125 |
| `cmd/liza/supervisor_logs.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 34, 34, 37, 37, 82 |
| `internal/commands/add_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 40, 45, 90, 93 |
| `internal/commands/add_tasks.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 26, 31, 33, 36 |
| `internal/commands/agent_health.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 38, 46 |
| `internal/commands/analyze.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 1 | 85 |
| `internal/commands/analyze.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 24 | 22, 35, 40, 42, 44, 45, 46, 47, 48, 49, 53, 55, 56, 58, 59, 62, 70, 73, 74, 75, 76, 78, 79, 89 |
| `internal/commands/analyze.go` | message or display text | separator of a rendered list (display text) | 1 | 91 |
| `internal/commands/analyze.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 34, 61 |
| `internal/commands/analyze.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 84 |
| `internal/commands/analyze.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 56, 59 |
| `internal/commands/apply_dependency_repair.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 37, 44, 46, 49 |
| `internal/commands/apply_dependency_repair.go` | message or display text | separator of a rendered list (display text) | 1 | 46 |
| `internal/commands/assess_blocked.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 9 | 33, 38, 43, 47, 49, 51, 55, 57, 61 |
| `internal/commands/assess_blocked.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 44 |
| `internal/commands/assess_blocked.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 37 |
| `internal/commands/assess_hypothesis_exhausted.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 29, 36 |
| `internal/commands/await_budget.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 35 |
| `internal/commands/await_budget.go` | number | lower bound clamp of a computed width, height or count | 3 | 28, 29, 33 |
| `internal/commands/await_resubmission.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 87 |
| `internal/commands/await_verdict.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 89 |
| `internal/commands/await_verdict.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 109 |
| `internal/commands/cancel_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 35, 42 |
| `internal/commands/claim_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 11 | 45, 47, 50, 53, 54, 55, 56, 58, 62, 64, 68 |
| `internal/commands/claim_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 60 |
| `internal/commands/cleanup.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 16, 43, 52, 54, 57, 58, 60, 61, 63, 68 |
| `internal/commands/cleanup.go` | message or display text | separator of a rendered list (display text) | 1 | 43 |
| `internal/commands/cleanup.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 42 |
| `internal/commands/delete_agent.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 39, 44, 47, 57, 63, 66, 70 |
| `internal/commands/delete_agent.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 29 |
| `internal/commands/delete_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 30, 33, 34, 38, 45, 58, 72, 75, 77, 79 |
| `internal/commands/delete_task.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 57 |
| `internal/commands/format_helpers.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 20, 22, 50, 63 |
| `internal/commands/format_helpers.go` | message or display text | separator of a rendered list (display text) | 2 | 25, 69 |
| `internal/commands/format_helpers.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 61, 66 |
| `internal/commands/format_helpers.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 10 |
| `internal/commands/handoff.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 15, 22, 23 |
| `internal/commands/init.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 131 |
| `internal/commands/init.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 21 | 230, 232, 360, 362, 364, 365, 366, 370, 372, 385, 399, 410, 412, 423, 424, 431, 467, 476, 479, 483, 489 |
| `internal/commands/init.go` | message or display text | separator of a rendered list (display text) | 4 | 360, 423, 431, 452 |
| `internal/commands/init.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 10 | 127, 306, 355, 355, 460, 463, 475, 475, 478, 504 |
| `internal/commands/init.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 173, 447 |
| `internal/commands/init.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 203, 323, 359 |
| `internal/commands/init.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 173, 174 |
| `internal/commands/init.go` | data | completion candidate value (frozen completion output) | 3 | 187, 188, 189 |
| `internal/commands/init.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 2 | 169, 186 |
| `internal/commands/init.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 130, 503 |
| `internal/commands/init_contract_placement.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 2 | 442, 444 |
| `internal/commands/init_contract_placement.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 43 | 181, 214, 232, 237, 241, 245, 265, 272, 276, 277, 280, 291, 315, 318, 321, 323, 326, 331, 339, 345, 350, 354, 355, 357, 363, 390, 396, 410, 412, 415, 418, 420, 423, 438, 446, 450, 454, 455, 458, 463, 466, 468, 471 |
| `internal/commands/init_contract_placement.go` | message or display text | separator of a rendered list (display text) | 1 | 396 |
| `internal/commands/init_contract_placement.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 22 | 70, 78, 81, 84, 84, 85, 90, 95, 102, 110, 117, 186, 192, 215, 217, 289, 338, 373, 380, 383, 385, 432 |
| `internal/commands/init_contract_placement.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 59, 224, 395, 443 |
| `internal/commands/init_contract_placement.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 311 |
| `internal/commands/init_contract_placement.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 396 |
| `internal/commands/init_contract_placement.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 82 |
| `internal/commands/init_contract_placement.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 422 |
| `internal/commands/init_contract_placement.go` | other literal | suffix of the backup a contract file is moved to | 1 | 310 |
| `internal/commands/init_integration_branch.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 14, 24, 36, 53, 67 |
| `internal/commands/init_integration_branch.go` | external argv | external command argument: cmux, wezterm or Git vocabulary (not a Liza set; C4 Git literals are preserved) | 9 | 34, 34, 50, 50, 60, 60, 65, 65, 65 |
| `internal/commands/init_mas_inputs.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 18 | 55, 60, 64, 96, 100, 104, 112, 119, 125, 135, 145, 148, 160, 169, 174, 176, 194, 197 |
| `internal/commands/init_mas_inputs.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 51, 70, 93, 117, 124 |
| `internal/commands/init_mas_inputs.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 54 |
| `internal/commands/init_mas_inputs.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 5 | 160, 160, 160, 160, 160 |
| `internal/commands/init_mas_inputs.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 171 |
| `internal/commands/init_mas_inputs.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 159 |
| `internal/commands/init_mas_install.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 11 | 28, 54, 63, 68, 73, 83, 86, 95, 107, 113, 119 |
| `internal/commands/init_mas_install.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 18, 93 |
| `internal/commands/init_mas_install.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 22, 93 |
| `internal/commands/init_mas_layout.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 30, 42, 47, 52, 68, 75, 80 |
| `internal/commands/init_mas_layout.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 72 |
| `internal/commands/init_mas_layout.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 2 | 46, 46 |
| `internal/commands/init_mas_state.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 15 | 37, 38, 40, 49, 65, 72, 203, 211, 219, 225, 231, 238, 239, 249, 250 |
| `internal/commands/init_mas_state.go` | message or display text | separator of a rendered list (display text) | 1 | 49 |
| `internal/commands/init_mas_state.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 34, 36 |
| `internal/commands/init_mas_state.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 48 |
| `internal/commands/init_mas_state.go` | number | zero: emptiness, count or sign test, initial value or default | 5 | 153, 154, 155, 156, 157 |
| `internal/commands/init_mas_state.go` | number | zero: no configured instance limit | 1 | 173 |
| `internal/commands/init_mas_state.go` | path | path segment or file name (layout of a directory the command reads or writes) | 3 | 62, 63, 70 |
| `internal/commands/init_mas_state.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 1 | 243 |
| `internal/commands/init_mistral_contract.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 15 | 22, 28, 36, 51, 57, 59, 62, 69, 74, 75, 77, 81, 86, 109, 111 |
| `internal/commands/init_mistral_contract.go` | message or display text | separator of a rendered list (display text) | 1 | 108 |
| `internal/commands/init_mistral_contract.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 34 |
| `internal/commands/init_mistral_contract.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 56 |
| `internal/commands/init_mistral_contract.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 92, 96 |
| `internal/commands/init_pairing.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 5 | 300, 309, 311, 313, 315 |
| `internal/commands/init_pairing.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 22 | 84, 92, 104, 109, 111, 167, 176, 206, 212, 215, 222, 231, 237, 243, 250, 257, 275, 281, 288, 290, 292, 293 |
| `internal/commands/init_pairing.go` | message or display text | separator of a rendered list (display text) | 1 | 290 |
| `internal/commands/init_pairing.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 13 | 67, 104, 109, 111, 120, 167, 173, 176, 181, 191, 220, 235, 248 |
| `internal/commands/init_pairing.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 79, 164, 220, 289 |
| `internal/commands/inspect.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 6 | 45, 49, 56, 96, 159, 182 |
| `internal/commands/inspect.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 42, 76, 84, 185, 188, 219, 279 |
| `internal/commands/inspect.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 16 | 55, 72, 76, 84, 96, 127, 159, 171, 176, 182, 185, 188, 219, 235, 263, 279 |
| `internal/commands/inspect.go` | number | argument position of the optional ID | 1 | 93 |
| `internal/commands/inspect.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 242 |
| `internal/commands/inspect.go` | number | length, count or arity comparison against a small number of arguments or elements | 10 | 47, 75, 95, 106, 158, 181, 181, 184, 206, 217 |
| `internal/commands/inspect.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 107 |
| `internal/commands/inspect.go` | number | position in a fixed-layout slice or arguments list | 3 | 88, 207, 221 |
| `internal/commands/inspect.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 279, 279 |
| `internal/commands/inspect.go` | wire key | key copied from a human note Extra map into the inspect row (frozen --json bytes) | 2 | 249, 249 |
| `internal/commands/inspect.go` | wire key | key or value of a JSON object written or read at the command boundary (frozen --json bytes or the provider hook protocol) | 3 | 245, 246, 247 |
| `internal/commands/inspect.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 53, 149 |
| `internal/commands/inspect_agents.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 4 | 249, 264, 300, 325 |
| `internal/commands/inspect_agents.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 11 | 101, 222, 224, 241, 243, 256, 270, 293, 327, 329, 342 |
| `internal/commands/inspect_agents.go` | message or display text | separator of a rendered list (display text) | 2 | 270, 272 |
| `internal/commands/inspect_agents.go` | message or display text | table column header or row label (frozen human output) | 16 | 252, 252, 252, 252, 252, 252, 303, 303, 303, 303, 303, 303, 303, 303, 303, 303 |
| `internal/commands/inspect_agents.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 14 | 62, 65, 209, 222, 224, 229, 241, 243, 278, 293, 313, 318, 350, 353 |
| `internal/commands/inspect_agents.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 59, 253, 268 |
| `internal/commands/inspect_agents.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 124, 248, 299, 324 |
| `internal/commands/inspect_agents.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 361 |
| `internal/commands/inspect_agents.go` | other literal | separator, whitespace or glyph of composed output text | 2 | 341, 356 |
| `internal/commands/inspect_anomalies.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 126, 174 |
| `internal/commands/inspect_anomalies.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 117, 119, 163 |
| `internal/commands/inspect_anomalies.go` | message or display text | separator of a rendered list (display text) | 1 | 166 |
| `internal/commands/inspect_anomalies.go` | message or display text | table column header or row label (frozen human output) | 6 | 129, 129, 129, 129, 129, 129 |
| `internal/commands/inspect_anomalies.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 81, 86, 91, 104, 117, 119, 151 |
| `internal/commands/inspect_anomalies.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 154, 160 |
| `internal/commands/inspect_anomalies.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 125, 150 |
| `internal/commands/inspect_anomalies.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 134 |
| `internal/commands/inspect_anomalies.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 137 |
| `internal/commands/inspect_field.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 199 |
| `internal/commands/inspect_field.go` | message or display text | separator of a rendered list (display text) | 1 | 47 |
| `internal/commands/inspect_field.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 21 | 20, 25, 34, 46, 49, 56, 57, 65, 66, 72, 82, 99, 130, 137, 156, 160, 192, 199, 203, 212, 250 |
| `internal/commands/inspect_field.go` | number | length, count or arity comparison against a small number of arguments or elements | 13 | 30, 52, 76, 198, 245, 250, 251, 263, 270, 293, 312, 323, 346 |
| `internal/commands/inspect_field.go` | number | path segment position | 1 | 88 |
| `internal/commands/inspect_field.go` | number | position in a fixed-layout slice or arguments list | 14 | 30, 57, 59, 62, 98, 221, 249, 257, 259, 261, 266, 267, 273, 274 |
| `internal/commands/inspect_field.go` | number | zero: emptiness, count or sign test, initial value or default | 10 | 93, 285, 294, 296, 313, 315, 324, 326, 347, 349 |
| `internal/commands/inspect_field.go` | data | NotFoundError.Entity label (error detail text, not read by any switch) | 6 | 21, 26, 31, 47, 82, 246 |
| `internal/commands/inspect_field.go` | data | field name carried in a payload diagnostic | 2 | 264, 271 |
| `internal/commands/inspect_field.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 226 |
| `internal/commands/inspect_field.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 6 | 24, 98, 187, 194, 211, 244 |
| `internal/commands/inspect_field.go` | other literal | field path separator or prefix of the rendered query path | 1 | 73 |
| `internal/commands/inspect_metrics.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 310 |
| `internal/commands/inspect_metrics.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 21 | 224, 226, 246, 248, 263, 265, 267, 276, 278, 282, 284, 288, 291, 294, 297, 302, 320, 321, 322, 323, 324 |
| `internal/commands/inspect_metrics.go` | message or display text | table column header or row label (frozen human output) | 7 | 313, 313, 313, 313, 313, 313, 313 |
| `internal/commands/inspect_metrics.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 211, 224, 226, 233, 246, 248, 256 |
| `internal/commands/inspect_metrics.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 200 |
| `internal/commands/inspect_metrics.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 194, 296, 309 |
| `internal/commands/inspect_metrics.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 254 |
| `internal/commands/inspect_metrics.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 258 |
| `internal/commands/inspect_task_assembly.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 235 |
| `internal/commands/inspect_task_assembly.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 207, 232 |
| `internal/commands/inspect_task_assembly.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 48, 125, 136, 140 |
| `internal/commands/inspect_task_assembly.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 125 |
| `internal/commands/inspect_task_render.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 3 | 88, 132, 293 |
| `internal/commands/inspect_task_render.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 65 | 23, 25, 42, 44, 63, 82, 106, 111, 118, 119, 148, 161, 162, 163, 164, 165, 166, 170, 175, 180, 183, 186, 189, 192, 200, 201, 202, 203, 213, 215, 218, 221, 224, 227, 230, 236, 239, 264, 266, 286, 312, 317, 323, 339, 340, 341, 342, 343, 347, 353, 358, 359, 362, 368, 372, 376, 380, 384, 388, 392, 399, 403, 407, 411, 417 |
| `internal/commands/inspect_task_render.go` | message or display text | separator of a rendered list (display text) | 11 | 180, 189, 195, 208, 230, 236, 239, 244, 247, 362, 392 |
| `internal/commands/inspect_task_render.go` | message or display text | table column header or row label (frozen human output) | 26 | 91, 91, 91, 91, 91, 91, 91, 91, 91, 135, 135, 135, 135, 135, 135, 135, 296, 296, 296, 296, 296, 296, 296, 296, 296, 296 |
| `internal/commands/inspect_task_render.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 24 | 11, 23, 25, 30, 42, 44, 49, 63, 68, 82, 214, 217, 220, 223, 226, 241, 252, 264, 266, 272, 286, 387, 398, 402 |
| `internal/commands/inspect_task_render.go` | number | length, count or arity comparison against a small number of arguments or elements | 19 | 87, 105, 110, 131, 138, 179, 188, 191, 206, 229, 235, 238, 292, 311, 361, 371, 375, 391, 410 |
| `internal/commands/inspect_task_render.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 10 | 172, 177, 207, 211, 233, 244, 349, 355, 364, 395 |
| `internal/commands/inspect_tasks.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 161, 192 |
| `internal/commands/inspect_tasks.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 158, 184, 207, 210, 237, 245 |
| `internal/commands/inspect_tasks.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 46 |
| `internal/commands/inspect_tasks.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 41, 105, 115, 180 |
| `internal/commands/inspect_time.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 17 |
| `internal/commands/inspect_time.go` | number | zero: emptiness, count or sign test, initial value or default | 3 | 18, 31, 51 |
| `internal/commands/inspect_vocabulary.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 18 |
| `internal/commands/integration_failure_diagnostics.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 136 |
| `internal/commands/integration_failure_diagnostics.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 79, 94, 123, 148 |
| `internal/commands/integration_failure_diagnostics.go` | number | length, count or arity comparison against a small number of arguments or elements | 5 | 53, 56, 87, 112, 155 |
| `internal/commands/integration_failure_diagnostics.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 56, 112 |
| `internal/commands/lifecycle.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 20, 27, 42, 44, 48, 50, 53, 69, 75, 78 |
| `internal/commands/lifecycle.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 17, 26, 36, 43, 77 |
| `internal/commands/lifecycle.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 55, 62 |
| `internal/commands/lifecycle.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 68 |
| `internal/commands/lifecycle.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 21 |
| `internal/commands/lifecycle.go` | number | position in a fixed-layout slice or arguments list | 3 | 26, 27, 27 |
| `internal/commands/lifecycle.go` | wire key | field name printed in the lifecycle outcome block (frozen human output) | 6 | 22, 22, 23, 23, 24, 24 |
| `internal/commands/mark_blocked.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 41, 48, 50, 53, 56 |
| `internal/commands/mark_blocked.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 49 |
| `internal/commands/migrate.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 23, 28, 78 |
| `internal/commands/narrow_inherited_dependencies.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 29, 36, 41, 43, 46, 49, 51, 55 |
| `internal/commands/narrow_inherited_dependencies.go` | message or display text | separator of a rendered list (display text) | 2 | 43, 46 |
| `internal/commands/narrow_inherited_dependencies.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 42, 45 |
| `internal/commands/pause.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 15, 18, 19, 20 |
| `internal/commands/pause.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 20 |
| `internal/commands/proceed.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 23, 25, 27, 29, 32 |
| `internal/commands/proceed.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 24 |
| `internal/commands/proceed.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 32 |
| `internal/commands/provider_activation.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 14 | 20, 39, 88, 103, 107, 114, 126, 129, 132, 139, 143, 187, 297, 303 |
| `internal/commands/provider_activation.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 11 | 103, 106, 107, 114, 138, 155, 187, 219, 276, 327, 366 |
| `internal/commands/provider_activation.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 349 |
| `internal/commands/provider_activation.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 334, 352 |
| `internal/commands/provider_activation.go` | number | position in a fixed-layout slice or arguments list | 1 | 355 |
| `internal/commands/provider_activation.go` | external argv | external command argument: cmux, wezterm or Git vocabulary (not a Liza set; C4 Git literals are preserved) | 2 | 101, 101 |
| `internal/commands/provider_activation.go` | path | path segment or file name (layout of a directory the command reads or writes) | 12 | 116, 385, 385, 388, 388, 391, 391, 394, 394, 400, 400, 400 |
| `internal/commands/provider_activation.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 1 | 159 |
| `internal/commands/provider_activation.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 3 | 155, 161, 161 |
| `internal/commands/provider_activation.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 2 | 186, 397 |
| `internal/commands/provider_activation.go` | other literal | path element: current or parent directory | 2 | 186, 186 |
| `internal/commands/providers.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 51 |
| `internal/commands/providers.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 9 | 28, 88, 92, 93, 94, 97, 102, 105, 105 |
| `internal/commands/providers.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 40 |
| `internal/commands/providers.go` | dynamic identity | legacy provider catalog schema v1 fallback path (removed with schema v1 support) | 5 | 121, 123, 123, 125, 127 |
| `internal/commands/providers.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 7 | 57, 57, 120, 122, 124, 126, 126 |
| `internal/commands/recover_agent.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 11 | 32, 43, 45, 47, 50, 53, 56, 59, 80, 82, 87 |
| `internal/commands/recover_agent.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 36, 46, 63, 79 |
| `internal/commands/recover_agent.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 2 | 18, 19 |
| `internal/commands/recover_agent.go` | external argv | subcommand word of the relaunch argv that follows the branded binary name | 1 | 17 |
| `internal/commands/recover_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 15 | 24, 33, 37, 41, 43, 45, 48, 51, 54, 57, 60, 63, 66, 69, 72 |
| `internal/commands/recover_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 47 |
| `internal/commands/rejection_rca.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 32, 42, 43, 66, 71 |
| `internal/commands/rejection_rca.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 2 | 30, 39 |
| `internal/commands/release_claim.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 18, 26, 29 |
| `internal/commands/repair_acceptance_commits.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 50, 52 |
| `internal/commands/repair_acceptance_commits.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 9 | 24, 28, 35, 43, 56, 61, 68, 78, 80 |
| `internal/commands/repair_acceptance_commits.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 21, 27 |
| `internal/commands/repair_acceptance_commits.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 62, 73 |
| `internal/commands/repair_acceptance_commits.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 55, 55, 72 |
| `internal/commands/repair_agent_pool.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 120 |
| `internal/commands/repair_agent_pool.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 82, 116, 124, 126, 128, 130, 208, 232, 237, 242 |
| `internal/commands/repair_agent_pool.go` | message or display text | separator of a rendered list (display text) | 2 | 116, 242 |
| `internal/commands/repair_agent_pool.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 13 | 117, 195, 198, 199, 202, 206, 241, 298, 308, 321, 339, 339, 340 |
| `internal/commands/repair_agent_pool.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 355, 368 |
| `internal/commands/repair_agent_pool.go` | number | length, count or arity comparison against a small number of arguments or elements | 6 | 81, 127, 274, 312, 348, 365 |
| `internal/commands/repair_agent_pool.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 3 | 126, 126, 126 |
| `internal/commands/repair_agent_pool.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 1 | 186 |
| `internal/commands/repair_agent_pool.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 1 | 201 |
| `internal/commands/repair_agent_pool.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 84 |
| `internal/commands/repair_agent_pool_capacity.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 3 | 228, 607, 607 |
| `internal/commands/repair_agent_pool_capacity.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 233 |
| `internal/commands/repair_agent_pool_capacity.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 120, 136, 409, 417, 451, 465, 563 |
| `internal/commands/repair_agent_pool_capacity.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 165, 171, 278 |
| `internal/commands/repair_agent_pool_capacity.go` | number | length, count or arity comparison against a small number of arguments or elements | 9 | 218, 222, 275, 327, 521, 533, 568, 574, 578 |
| `internal/commands/repair_agent_pool_capacity.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 207, 565 |
| `internal/commands/repair_agent_pool_capacity.go` | number | position in a fixed-layout slice or arguments list | 1 | 205 |
| `internal/commands/repair_agent_pool_capacity.go` | number | zero: emptiness, count or sign test, initial value or default | 4 | 482, 490, 538, 558 |
| `internal/commands/repair_agent_pool_render.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 20 | 13, 28, 30, 43, 45, 53, 55, 57, 69, 71, 82, 85, 97, 101, 104, 114, 117, 120, 124, 126 |
| `internal/commands/repair_agent_pool_render.go` | message or display text | separator of a rendered list (display text) | 2 | 71, 104 |
| `internal/commands/repair_agent_pool_render.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 56, 96, 105 |
| `internal/commands/repair_agent_pool_render.go` | number | length, count or arity comparison against a small number of arguments or elements | 17 | 12, 12, 12, 12, 12, 21, 40, 50, 60, 66, 73, 79, 87, 96, 113, 116, 123 |
| `internal/commands/repair_agent_pool_render.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 106 |
| `internal/commands/repair_agent_pool_spawn.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 49, 55 |
| `internal/commands/repair_agent_pool_spawn.go` | message or display text | separator of a rendered list (display text) | 1 | 56 |
| `internal/commands/repair_agent_pool_spawn.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 17, 67 |
| `internal/commands/repair_agent_pool_spawn.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 47 |
| `internal/commands/repair_agent_pool_spawn.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 20 |
| `internal/commands/repair_agent_pool_spawn.go` | number | zero: emptiness, count or sign test, initial value or default | 3 | 29, 32, 51 |
| `internal/commands/repair_agent_pool_start.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 67, 214 |
| `internal/commands/repair_agent_pool_start.go` | message or display text | separator of a rendered list (display text) | 1 | 67 |
| `internal/commands/repair_agent_pool_start.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 11 | 39, 44, 44, 53, 53, 67, 67, 178, 234, 266, 278 |
| `internal/commands/repair_agent_pool_start.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 256 |
| `internal/commands/repair_agent_pool_start.go` | number | length, count or arity comparison against a small number of arguments or elements | 6 | 76, 82, 87, 102, 108, 263 |
| `internal/commands/repair_agent_pool_start.go` | number | lower bound clamp of a computed width, height or count | 2 | 201, 211 |
| `internal/commands/repair_agent_pool_start.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 9 | 103, 103, 196, 196, 201, 211, 212, 213, 264 |
| `internal/commands/repair_agent_pool_start.go` | number | position in a fixed-layout slice or arguments list | 1 | 277 |
| `internal/commands/repair_agent_pool_start.go` | number | zero: emptiness, count or sign test, initial value or default | 2 | 79, 194 |
| `internal/commands/repair_agent_pool_start.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 258 |
| `internal/commands/repair_agent_pool_start.go` | other literal | separator, whitespace or glyph of composed output text | 6 | 265, 265, 267, 267, 271, 271 |
| `internal/commands/repair_superseded_dependencies.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 37, 44, 45, 46, 48 |
| `internal/commands/repair_superseded_dependencies.go` | message or display text | separator of a rendered list (display text) | 2 | 45, 46 |
| `internal/commands/replace_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 18, 23, 25 |
| `internal/commands/replan.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 28, 29, 30, 33, 35, 39 |
| `internal/commands/replan.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 13 |
| `internal/commands/resume.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 17 | 19, 23, 24, 26, 27, 29, 33, 35, 39, 42, 49, 52, 59, 61, 66, 71, 73 |
| `internal/commands/resume.go` | message or display text | separator of a rendered list (display text) | 1 | 66 |
| `internal/commands/resume.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 41 |
| `internal/commands/resume.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 34, 38, 65 |
| `internal/commands/resume.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 71 |
| `internal/commands/retarget_dependency.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 38, 45, 48, 51, 54 |
| `internal/commands/retarget_dependency.go` | message or display text | separator of a rendered list (display text) | 2 | 46, 48 |
| `internal/commands/set_discovery_disposition.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 13, 16 |
| `internal/commands/set_task_output.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 30, 45 |
| `internal/commands/setup.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 62 | 105, 111, 146, 152, 159, 163, 177, 188, 194, 204, 210, 255, 268, 305, 309, 311, 314, 316, 319, 323, 327, 341, 342, 344, 349, 356, 366, 368, 370, 380, 383, 385, 397, 398, 402, 434, 441, 447, 455, 473, 485, 507, 532, 540, 547, 551, 553, 555, 582, 589, 593, 615, 621, 622, 625, 626, 629, 634, 638, 646, 651, 662 |
| `internal/commands/setup.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 13 | 102, 156, 188, 194, 243, 433, 433, 498, 498, 509, 512, 571, 571 |
| `internal/commands/setup.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 217, 503, 503 |
| `internal/commands/setup.go` | number | length, count or arity comparison against a small number of arguments or elements | 10 | 156, 202, 300, 313, 379, 382, 384, 534, 584, 611 |
| `internal/commands/setup.go` | number | lipgloss padding or border cell count (style data) | 2 | 413, 413 |
| `internal/commands/setup.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 217, 218 |
| `internal/commands/setup.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 373 |
| `internal/commands/setup.go` | path | path segment or file name (layout of a directory the command reads or writes) | 5 | 370, 401, 401, 452, 563 |
| `internal/commands/setup.go` | data | completion candidate value (frozen completion output) | 2 | 230, 231 |
| `internal/commands/setup.go` | dynamic identity | name of a retired deployed skill that setup removes on upgrade (data list, not a vocabulary) | 1 | 34 |
| `internal/commands/setup.go` | dynamic identity | provider or CLI identity from the provider catalog (dynamic, ADR-0045) | 2 | 229, 391 |
| `internal/commands/setup.go` | other literal | ANSI 256 color code (style data) | 1 | 412 |
| `internal/commands/setup.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 1 | 662 |
| `internal/commands/setup.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 497 |
| `internal/commands/sprint_checkpoint.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 9 | 18, 21, 22, 23, 25, 28, 29, 31, 33 |
| `internal/commands/sprint_checkpoint.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 16 |
| `internal/commands/sprint_checkpoint.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 29, 33 |
| `internal/commands/start.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 15, 18, 19, 20, 21, 28, 29, 31 |
| `internal/commands/start.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 30 |
| `internal/commands/start.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 4 | 20, 20, 21, 21 |
| `internal/commands/start.go` | branded composition | environment variable suffix composed through brand.EnvName (the brand supplies the prefix) | 2 | 20, 21 |
| `internal/commands/status.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 19 |
| `internal/commands/status_orchestrator.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 7 | 46, 50, 68, 73, 75, 101, 103 |
| `internal/commands/status_orchestrator.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 48, 53, 55, 57, 59, 61, 63, 70 |
| `internal/commands/status_orchestrator.go` | message or display text | separator of a rendered list (display text) | 4 | 108, 111, 114, 119 |
| `internal/commands/status_orchestrator.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 40, 104, 116 |
| `internal/commands/status_orchestrator.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 107, 110, 113 |
| `internal/commands/status_orchestrator.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 5 | 105, 108, 111, 114, 117 |
| `internal/commands/status_phase_handoff.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 130, 146, 149, 153, 157, 161, 166 |
| `internal/commands/status_phase_handoff.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 184 |
| `internal/commands/status_phase_handoff.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 121 |
| `internal/commands/status_phase_handoff.go` | number | length, count or arity comparison against a small number of arguments or elements | 7 | 36, 39, 39, 39, 147, 159, 164 |
| `internal/commands/status_phase_handoff.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 8 | 125, 162, 162, 162, 162, 162, 162, 162 |
| `internal/commands/status_render.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 56 | 26, 27, 31, 38, 42, 43, 44, 45, 46, 47, 49, 52, 54, 61, 63, 68, 69, 75, 76, 77, 81, 83, 87, 89, 97, 115, 122, 141, 148, 149, 150, 152, 154, 158, 160, 164, 166, 170, 172, 174, 176, 180, 185, 188, 193, 195, 206, 207, 209, 220, 224, 226, 233, 296, 299, 302 |
| `internal/commands/status_render.go` | message or display text | separator of a rendered list (display text) | 2 | 231, 233 |
| `internal/commands/status_render.go` | message or display text | table column header or row label (frozen human output) | 30 | 80, 80, 80, 80, 92, 92, 92, 92, 92, 92, 92, 92, 140, 140, 140, 140, 140, 182, 182, 182, 182, 182, 190, 190, 190, 190, 190, 215, 216, 217 |
| `internal/commands/status_render.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 100, 173, 208, 223 |
| `internal/commands/status_render.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 6 | 32, 71, 123, 129, 237, 251 |
| `internal/commands/status_render.go` | number | length, count or arity comparison against a small number of arguments or elements | 16 | 30, 48, 51, 58, 70, 88, 96, 119, 151, 157, 163, 169, 179, 187, 219, 295 |
| `internal/commands/status_render.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 299 |
| `internal/commands/status_render.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 315 |
| `internal/commands/status_sections.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 87, 107, 120, 122 |
| `internal/commands/status_sections.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 99, 117, 172 |
| `internal/commands/status_sections.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 164, 244, 250 |
| `internal/commands/status_sections.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 70, 82, 262, 273 |
| `internal/commands/status_sections.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 120, 122 |
| `internal/commands/status_snapshot.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 25, 30 |
| `internal/commands/stop.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 14, 17, 18 |
| `internal/commands/stop_guard.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 53, 101, 106 |
| `internal/commands/stop_guard.go` | message or display text | separator of a rendered list (display text) | 2 | 106, 181 |
| `internal/commands/stop_guard.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 48, 74, 94, 104, 114 |
| `internal/commands/stop_guard.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 103, 118, 151, 171 |
| `internal/commands/stop_guard.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 125 |
| `internal/commands/stop_guard.go` | number | position in a fixed-layout slice or arguments list | 1 | 121 |
| `internal/commands/stop_guard.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 153 |
| `internal/commands/stop_guard.go` | flag or CLI spelling | flag spelling or JSON null parsed from a recorded command line | 5 | 73, 128, 128, 131, 131 |
| `internal/commands/stop_guard.go` | branded composition | environment variable suffix looked up through brand.LookupEnv (the brand supplies the prefix) | 1 | 48 |
| `internal/commands/stop_guard.go` | dynamic identity | CLI subcommand name of a session-holding wait (operation names stay dynamic, ADR-0045) | 2 | 20, 21 |
| `internal/commands/stop_guard.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 4 | 157, 157, 160, 160 |
| `internal/commands/stop_guard.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 4 | 114, 121, 130, 130 |
| `internal/commands/stop_guard.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 186 |
| `internal/commands/submit_review.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 16, 23, 24, 25, 27, 40, 47, 48, 49, 51 |
| `internal/commands/submit_verdict.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 12 | 16, 31, 47, 48, 50, 51, 52, 54, 56, 59, 60, 61 |
| `internal/commands/submit_verdict.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 55 |
| `internal/commands/submit_verdict.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 6 | 60, 60, 60, 61, 61, 61 |
| `internal/commands/supersede_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 3 | 37, 45, 48 |
| `internal/commands/supersede_task.go` | message or display text | separator of a rendered list (display text) | 1 | 46 |
| `internal/commands/supersede_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 23 |
| `internal/commands/supersede_task.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 44 |
| `internal/commands/unblock_task.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 35, 43, 46, 49, 53 |
| `internal/commands/unblock_task.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 42 |
| `internal/commands/update_review_commit.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 14, 17, 22, 23, 28, 29, 31 |
| `internal/commands/update_review_commit.go` | message or display text | placeholder shown in usage or dry-run text (display text) | 2 | 18, 24 |
| `internal/commands/update_sprint_metrics.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 17, 18, 19, 20, 21, 22, 23, 27 |
| `internal/commands/update_sprint_metrics.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 35 |
| `internal/commands/usage_report.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 40, 56, 86, 91, 107, 193, 199 |
| `internal/commands/usage_report.go` | message or display text | table column header or row label (frozen human output) | 10 | 182, 182, 182, 182, 182, 182, 182, 182, 182, 182 |
| `internal/commands/usage_report.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 51, 63, 63, 133, 133, 176 |
| `internal/commands/usage_report.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 195 |
| `internal/commands/usage_report.go` | flag or CLI spelling | flag name, flag spelling or environment suffix of a flag table (frozen CLI surface) | 5 | 44, 45, 46, 47, 48 |
| `internal/commands/usage_report.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 5 | 60, 60, 64, 64, 83 |
| `internal/commands/validate.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 230 |
| `internal/commands/validate.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 14 | 63, 68, 70, 76, 79, 142, 151, 156, 168, 172, 187, 189, 232, 235 |
| `internal/commands/validate.go` | message or display text | separator of a rendered list (display text) | 2 | 189, 235 |
| `internal/commands/validate.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 124, 145, 184, 225, 229 |
| `internal/commands/validate.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 2 | 181, 222 |
| `internal/commands/validate.go` | number | length, count or arity comparison against a small number of arguments or elements | 10 | 62, 78, 138, 145, 177, 193, 193, 199, 203, 218 |
| `internal/commands/validate.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 207 |
| `internal/commands/validate.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 5 | 189, 189, 189, 189, 189 |
| `internal/commands/validate.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 58 |
| `internal/commands/validate.go` | data | task reference field name shown in the unresolved-fragment warning | 4 | 123, 123, 123, 123 |
| `internal/commands/validate.go` | dynamic identity | operation label recorded in a classified error (pass-through) | 1 | 85 |
| `internal/commands/validate.go` | dynamic identity | operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | 2 | 61, 74 |
| `internal/commands/validate.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 2 | 132, 132 |
| `internal/commands/validate_payload.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 55 |
| `internal/commands/validate_payload.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 37 |
| `internal/commands/validate_payload.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 10 | 65, 79, 87, 89, 91, 96, 97, 108, 114, 118 |
| `internal/commands/validate_payload.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 63, 113 |
| `internal/commands/validate_payload.go` | data | field name carried in a payload diagnostic | 2 | 54, 124 |
| `internal/commands/validate_payload.go` | data | payload field that carries the entries of the operation | 1 | 37 |
| `internal/commands/watch.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 92, 100, 109, 126, 143, 145 |
| `internal/commands/watch.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 82 |
| `internal/commands/watch.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 78 |
| `internal/commands/watch.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 93 |
| `internal/commands/watch_checks_agents.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 4 | 85, 87, 141, 143 |
| `internal/commands/watch_checks_agents.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 20 | 37, 40, 60, 93, 122, 133, 150, 167, 172, 176, 179, 230, 238, 243, 248, 253, 266, 343, 345, 350 |
| `internal/commands/watch_checks_agents.go` | message or display text | placeholder shown in usage or dry-run text (display text) | 1 | 272 |
| `internal/commands/watch_checks_agents.go` | message or display text | separator of a rendered list (display text) | 1 | 346 |
| `internal/commands/watch_checks_agents.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 20 | 75, 127, 182, 194, 205, 221, 271, 279, 280, 280, 283, 283, 288, 289, 289, 293, 293, 297, 340, 347 |
| `internal/commands/watch_checks_agents.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 350, 350 |
| `internal/commands/watch_checks_agents.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 348 |
| `internal/commands/watch_checks_sentinels.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 25, 80, 97, 126 |
| `internal/commands/watch_checks_sentinels.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 30, 59 |
| `internal/commands/watch_checks_sentinels.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 3 | 25, 81, 81 |
| `internal/commands/watch_checks_sprint.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 58, 101, 129, 131, 144, 146, 217, 218 |
| `internal/commands/watch_checks_sprint.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 3 | 25, 65, 117 |
| `internal/commands/watch_checks_sprint.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 126, 141 |
| `internal/commands/watch_checks_sprint.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 212 |
| `internal/commands/watch_checks_sprint.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 88 |
| `internal/commands/watch_checks_sprint.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 2 | 59, 59 |
| `internal/commands/watch_checks_tasks.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 40, 86 |
| `internal/commands/watch_checks_tasks.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 44, 99, 101, 131, 173, 201, 225, 246 |
| `internal/commands/watch_checks_tasks.go` | message or display text | separator of a rendered list (display text) | 1 | 175 |
| `internal/commands/watch_checks_tasks.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 111 |
| `internal/commands/watch_diagnosis.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 4 | 38, 48, 64, 66 |
| `internal/commands/watch_diagnosis.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 9 | 42, 44, 46, 58, 61, 70, 73, 76, 100 |
| `internal/commands/watch_diagnosis.go` | message or display text | separator of a rendered list (display text) | 6 | 38, 66, 71, 71, 74, 76 |
| `internal/commands/watch_diagnosis.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 31, 36, 78, 137 |
| `internal/commands/watch_diagnosis.go` | number | length, count or arity comparison against a small number of arguments or elements | 15 | 34, 34, 37, 41, 41, 43, 45, 54, 57, 65, 69, 69, 72, 75, 91 |
| `internal/commands/watch_orchestrator.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 122, 124 |
| `internal/commands/watch_orchestrator.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 76, 119, 129, 131 |
| `internal/commands/watch_orchestrator.go` | message or display text | separator of a rendered list (display text) | 2 | 120, 122 |
| `internal/commands/watch_orchestrator.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 58 |
| `internal/commands/watch_orchestrator.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 61, 121 |
| `internal/commands/watch_orchestrator.go` | number | position in a fixed-layout slice or arguments list | 1 | 125 |
| `internal/commands/watch_orchestrator.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 125 |
| `internal/commands/watch_reconcile.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 51 |
| `internal/commands/watch_reconcile.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 143 |
| `internal/commands/watch_reconcile.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 27, 54 |
| `internal/commands/watch_reconcile.go` | number | zero: emptiness, count or sign test, initial value or default | 2 | 31, 52 |
| `internal/commands/watch_reconcile.go` | character | single character of a scanned syntax (shell word, identifier, key=value or timestamp bracket) | 8 | 67, 67, 68, 68, 69, 69, 70, 70 |
| `internal/commands/watch_repair.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 1 | 387 |
| `internal/commands/watch_repair.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 416 |
| `internal/commands/watch_repair.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 120, 142, 259, 376, 414, 422 |
| `internal/commands/watch_repair.go` | message or display text | separator of a rendered list (display text) | 1 | 423 |
| `internal/commands/watch_repair.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 118, 302, 302, 337, 356 |
| `internal/commands/watch_repair.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 218 |
| `internal/commands/watch_repair.go` | number | length, count or arity comparison against a small number of arguments or elements | 5 | 97, 318, 337, 405, 415 |
| `internal/commands/watch_repair.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 288 |
| `internal/commands/watch_repair.go` | number | zero: emptiness, count or sign test, initial value or default | 4 | 332, 332, 436, 440 |
| `internal/commands/watch_repair.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 3 | 414, 414, 414 |
| `internal/commands/watch_repair.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 406 |
| `internal/commands/watch_repair_spawn.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 7 | 39, 67, 87, 104, 109, 117, 119 |
| `internal/commands/watch_repair_spawn.go` | message or display text | separator of a rendered list (display text) | 2 | 117, 119 |
| `internal/commands/watch_repair_spawn.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 72, 107, 112 |
| `internal/commands/watch_repair_spawn.go` | number | length, count or arity comparison against a small number of arguments or elements | 6 | 62, 74, 82, 88, 103, 118 |
| `internal/commands/write_checkpoint.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 14, 17, 24, 27 |
| `internal/commands/wt_create.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 5 | 15, 19, 22, 24, 28 |
| `internal/commands/wt_delete.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 15, 19, 24, 27 |
| `internal/commands/wt_merge.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 21 | 56, 57, 58, 60, 61, 62, 64, 65, 67, 70, 71, 73, 74, 77, 78, 87, 89, 93, 100, 101, 104 |
| `internal/commands/wt_merge.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 1 | 66 |
| `internal/interactive/init_wizard.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 272 |
| `internal/interactive/init_wizard.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 14 | 46, 48, 59, 72, 86, 87, 91, 96, 99, 100, 268, 270, 274, 283 |
| `internal/interactive/init_wizard.go` | message or display text | separator of a rendered list (display text) | 1 | 237 |
| `internal/interactive/init_wizard.go` | message or display text | wizard option label or value (display text) | 14 | 49, 61, 62, 63, 64, 65, 66, 67, 102, 103, 104, 105, 276, 278 |
| `internal/interactive/init_wizard.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 10 | 90, 102, 145, 152, 165, 180, 198, 232, 242, 273 |
| `internal/interactive/init_wizard.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 6 | 148, 217, 217, 229, 264, 264 |
| `internal/interactive/init_wizard.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 71, 209, 257 |
| `internal/interactive/init_wizard.go` | path | path segment or file name (layout of a directory the command reads or writes) | 1 | 250 |
| `internal/interactive/init_wizard.go` | data | wizard option value (provider catalog identity or action word) | 7 | 61, 62, 63, 64, 65, 66, 67 |
| `internal/interactive/init_wizard.go` | dynamic identity | entry-point name of the default pipeline (pipeline configuration, ADR-0045; masLaunchPreset names the three launch presets in cmd/liza) | 3 | 103, 104, 105 |
| `internal/interactive/init_wizard.go` | other literal | example spec reference shown by the init wizard (display default) | 1 | 41 |
| `internal/interactive/post_init.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 2 | 17, 28 |
| `internal/interactive/post_init.go` | message or display text | separator of a rendered list (display text) | 1 | 16 |
| `internal/interactive/post_init.go` | number | lipgloss padding or border cell count (style data) | 2 | 39, 39 |
| `internal/interactive/post_init.go` | other literal | ANSI 256 color code (style data) | 1 | 38 |
| `internal/jsonout/classify.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 3 | 23, 28, 38 |
| `internal/jsonout/classify.go` | message or display text | message substring that jsonout classifies an untyped error by (frozen error text) | 1 | 35 |
| `internal/jsonout/classify.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 24 | 21, 21, 26, 26, 26, 26, 32, 32, 32, 33, 33, 33, 34, 34, 34, 34, 35, 35, 65, 67, 69, 71, 73, 101 |
| `internal/jsonout/classify.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 61, 61, 75, 75 |
| `internal/jsonout/classify.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 3 | 99, 100, 100 |
| `internal/jsonout/classify_slots.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 95, 175 |
| `internal/jsonout/classify_slots.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 183 |
| `internal/jsonout/classify_slots.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 31 | 69, 69, 85, 85, 93, 93, 101, 101, 109, 109, 117, 117, 125, 125, 133, 133, 141, 141, 149, 149, 157, 157, 165, 165, 173, 173, 181, 181, 192, 192, 194 |
| `internal/jsonout/envelope.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 27 |
| `internal/tui/activity.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 1 | 156 |
| `internal/tui/activity.go` | message or display text | separator of a rendered list (display text) | 1 | 158 |
| `internal/tui/activity.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 87, 106, 135, 145 |
| `internal/tui/activity.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 148 |
| `internal/tui/activity.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 144 |
| `internal/tui/activity.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 115 |
| `internal/tui/commands.go` | message or display text | message or hint text of a classified error, diagnostic or prompt (jsonout classifies untyped errors by substring) | 7 | 257, 257, 257, 270, 321, 342, 372 |
| `internal/tui/commands.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 3 | 298, 338, 340 |
| `internal/tui/commands.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 12 | 120, 255, 268, 283, 293, 300, 303, 306, 319, 336, 354, 370 |
| `internal/tui/commands.go` | message or display text | separator of a rendered list (display text) | 1 | 306 |
| `internal/tui/commands.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 199, 317 |
| `internal/tui/commands.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 213 |
| `internal/tui/commands.go` | number | length, count or arity comparison against a small number of arguments or elements | 3 | 114, 215, 305 |
| `internal/tui/commands.go` | branded composition | branded command word composed through brand.Command (G1.3: the brand supplies the binary name) | 1 | 300 |
| `internal/tui/commands.go` | other literal | free-text reason or note recorded with the operation | 2 | 352, 368 |
| `internal/tui/keymap.go` | message or display text | key binding spelling and help text shown in the footer and help overlay (frozen TUI output) | 33 | 25, 26, 26, 29, 30, 30, 33, 34, 34, 37, 38, 38, 41, 42, 42, 45, 46, 46, 49, 50, 50, 53, 54, 54, 57, 58, 58, 61, 62, 62, 65, 66, 66 |
| `internal/tui/model.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 218, 299, 317, 318 |
| `internal/tui/model.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 1 | 361 |
| `internal/tui/model.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 201 |
| `internal/tui/model.go` | number | zero width: the initial value before the first window-size message | 1 | 363 |
| `internal/tui/styles.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 2 | 128, 141 |
| `internal/tui/styles.go` | number | lipgloss padding or border cell count (style data) | 8 | 215, 215, 250, 250, 263, 263, 269, 269 |
| `internal/tui/styles.go` | number | lower bound clamp of a computed width, height or count | 3 | 228, 232, 236 |
| `internal/tui/styles.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 3 | 228, 232, 236 |
| `internal/tui/styles.go` | other literal | ANSI 256 color code (style data) | 25 | 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 212, 213, 220, 230, 234, 238, 243, 248, 249, 255, 260, 261, 285 |
| `internal/tui/styles.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 9 | 115, 117, 119, 121, 123, 125, 134, 136, 138 |
| `internal/tui/styles.go` | other literal | separator, whitespace or glyph of composed output text | 2 | 154, 156 |
| `internal/tui/update.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 178 |
| `internal/tui/update_inline.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 45, 51, 86, 105, 120, 124 |
| `internal/tui/update_inline.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 56, 74, 96, 98, 103, 116, 130 |
| `internal/tui/update_inline.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 44, 119 |
| `internal/tui/update_keys.go` | message or display text | key binding spelling and help text shown in the footer and help overlay (frozen TUI output) | 7 | 13, 14, 15, 16, 16, 17, 17 |
| `internal/tui/update_keys.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 71, 92, 107, 127 |
| `internal/tui/update_keys.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 5 | 55, 140, 141, 172, 214 |
| `internal/tui/update_keys.go` | number | length, count or arity comparison against a small number of arguments or elements | 4 | 110, 201, 206, 219 |
| `internal/tui/update_keys.go` | number | zero: emptiness, count or sign test, initial value or default | 1 | 54 |
| `internal/tui/view.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 55, 161, 174, 175, 177, 178, 207, 216 |
| `internal/tui/view.go` | message or display text | separator of a rendered list (display text) | 1 | 200 |
| `internal/tui/view.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 6 | 61, 68, 71, 94, 97, 198 |
| `internal/tui/view.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 197, 204 |
| `internal/tui/view.go` | number | lower bound clamp of a computed width, height or count | 3 | 74, 185, 186 |
| `internal/tui/view.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 3 | 144, 185, 207 |
| `internal/tui/view.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 205 |
| `internal/tui/view_activity.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 14, 53, 54, 56, 62, 64, 70, 72 |
| `internal/tui/view_activity.go` | message or display text | separator of a rendered list (display text) | 1 | 33 |
| `internal/tui/view_activity.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 39, 61, 68, 69 |
| `internal/tui/view_activity.go` | number | length, count or arity comparison against a small number of arguments or elements | 2 | 17, 38 |
| `internal/tui/view_activity.go` | number | lower bound clamp of a computed width, height or count | 5 | 15, 18, 22, 25, 34 |
| `internal/tui/view_activity.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 18, 34 |
| `internal/tui/view_activity.go` | number | small count, ordinal or initial value local to its expression | 1 | 22 |
| `internal/tui/view_activity.go` | other literal | argument of a helper: flag or file label, external-tool key, template name or time layout (data of its call, see the use site) | 1 | 46 |
| `internal/tui/view_activity.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 33 |
| `internal/tui/view_agents.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 3 | 50, 211, 303 |
| `internal/tui/view_agents.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 6 | 46, 167, 177, 200, 268, 305 |
| `internal/tui/view_agents.go` | message or display text | separator of a rendered list (display text) | 2 | 66, 311 |
| `internal/tui/view_agents.go` | message or display text | table column header text (frozen human output) | 9 | 88, 89, 94, 95, 96, 97, 103, 104, 109 |
| `internal/tui/view_agents.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 10 | 67, 135, 177, 189, 210, 230, 235, 291, 300, 306 |
| `internal/tui/view_agents.go` | number | initial length or capacity of a slice or map (zero length, or a size derived from the source) | 3 | 75, 323, 335 |
| `internal/tui/view_agents.go` | number | length, count or arity comparison against a small number of arguments or elements | 8 | 49, 51, 157, 164, 234, 284, 299, 320 |
| `internal/tui/view_agents.go` | number | lower bound clamp of a computed width, height or count | 3 | 62, 199, 277 |
| `internal/tui/view_agents.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 6 | 173, 202, 237, 279, 287, 294 |
| `internal/tui/view_agents.go` | number | zero: emptiness, count or sign test, initial value or default | 2 | 285, 292 |
| `internal/tui/view_agents.go` | other literal | ANSI 256 color code (style data) | 1 | 176 |
| `internal/tui/view_agents.go` | other literal | delimiter, prefix, suffix or probe string of a text operation | 4 | 189, 233, 234, 294 |
| `internal/tui/view_agents.go` | other literal | separator, whitespace or glyph of composed output text | 7 | 52, 66, 66, 68, 145, 213, 307 |
| `internal/tui/view_footer.go` | message or display text | display word or placeholder printed to the user (frozen human output) | 5 | 55, 55, 57, 57, 57 |
| `internal/tui/view_footer.go` | message or display text | key hint text shown in the footer (frozen TUI output) | 1 | 55 |
| `internal/tui/view_footer.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 4 | 55, 57, 57, 57 |
| `internal/tui/view_footer.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 46, 46, 64, 64 |
| `internal/tui/view_footer.go` | message or display text | separator of a rendered list (display text) | 2 | 48, 66 |
| `internal/tui/view_footer.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 3 | 55, 57, 61 |
| `internal/tui/view_footer.go` | number | position in a fixed-layout slice or arguments list | 2 | 64, 64 |
| `internal/tui/view_footer.go` | other literal | separator, whitespace or glyph of composed output text | 4 | 20, 33, 46, 64 |
| `internal/tui/view_overlays.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 2 | 56, 56 |
| `internal/tui/view_overlays.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 4 | 25, 44, 70, 70 |
| `internal/tui/view_overlays.go` | message or display text | separator of a rendered list (display text) | 2 | 73, 94 |
| `internal/tui/view_overlays.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 7 | 19, 22, 23, 31, 39, 42, 61 |
| `internal/tui/view_overlays.go` | number | length, count or arity comparison against a small number of arguments or elements | 1 | 80 |
| `internal/tui/view_overlays.go` | number | lower bound clamp of a computed width, height or count | 1 | 25 |
| `internal/tui/view_overlays.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 2 | 25, 81 |
| `internal/tui/view_overlays.go` | other literal | separator, whitespace or glyph of composed output text | 1 | 70 |
| `internal/tui/view_tasks.go` | message or display text | message, label or hint text of the output (frozen human output; jsonout classifies untyped errors by substring) | 1 | 72 |
| `internal/tui/view_tasks.go` | message or display text | message, prompt or display text written to the user (frozen human output) | 8 | 54, 59, 86, 98, 182, 230, 233, 258 |
| `internal/tui/view_tasks.go` | message or display text | separator of a rendered list (display text) | 2 | 66, 210 |
| `internal/tui/view_tasks.go` | message or display text | table column header text (frozen human output) | 9 | 130, 131, 136, 137, 138, 143, 148, 149, 150 |
| `internal/tui/view_tasks.go` | empty | empty string: absence of an optional value (test, reset, default or return) | 4 | 73, 200, 233, 244 |
| `internal/tui/view_tasks.go` | number | length, count or arity comparison against a small number of arguments or elements | 6 | 53, 58, 82, 165, 207, 221 |
| `internal/tui/view_tasks.go` | number | lower bound clamp of a computed width, height or count | 4 | 63, 257, 280, 283 |
| `internal/tui/view_tasks.go` | number | one or two: ordinal, arity or minimum-gap arithmetic | 1 | 260 |
| `internal/tui/view_tasks.go` | other literal | ANSI 256 color code (style data) | 2 | 89, 232 |
| `internal/tui/view_tasks.go` | other literal | separator, whitespace or glyph of composed output text | 5 | 66, 66, 74, 173, 244 |

Totals by kind: message or display text 2288, empty 784, number 841, flag or CLI spelling 776, cobra definition 120, branded composition 203, external argv 104, path 31, wire key 31, data 37, dynamic identity 84, character 27, other literal 234, platform identifier 1 = 5561.

### 3b. Pre-existing named constants and variables in A7 files

Constants and variables that existed before this task (Tasks 2–10 and earlier); their literals are the declaration rows of the scan.

| Site | Name | Type | Value |
|---|---|---|---|
| `cmd/liza/cmd_system.go:23` | `defaultTUICheckIntervalSeconds` |  | `10` |
| `cmd/liza/cmd_tool_result_codex.go:18` | `maxHookBytes` |  | `128 << 20` |
| `cmd/liza/cmd_tool_result_codex.go:42` | `reason` |  | `"Tool-result processing failed; the original output is withheld. Check the managed artifact store an` |
| `cmd/liza/cmd_tool_result_store.go:16` | `toolResultDefaultThresholdBytes` |  | `32768` |
| `cmd/liza/cmd_tool_result_store.go:17` | `toolResultDefaultDigestBytes` |  | `4096` |
| `cmd/liza/cmd_tool_result_store.go:18` | `toolResultMinBudgetBytes` |  | `1024` |
| `cmd/liza/cmd_tool_result_store.go:20` | `toolResultMaxThresholdBytes` |  | `65536` |
| `cmd/liza/launch_exec.go:17` | `weztermExecutable` |  | `string(launchBackendWezterm)` |
| `cmd/liza/launch_exec.go:18` | `cmuxExecutable` |  | `string(launchBackendCmux)` |
| `cmd/liza/launch_exec.go:24` | `cmuxInteractiveReadyTimeout` |  | `60 * time.Second` |
| `cmd/liza/launch_exec.go:26` | `cmuxInteractiveSettleDelay` |  | `2 * time.Second` |
| `cmd/liza/launch_exec.go:28` | `cmuxPromptSubmitDelay` |  | `2 * time.Second` |
| `cmd/liza/launch_exec.go:30` | `cmuxScreenPollInterval` |  | `500 * time.Millisecond` |
| `cmd/liza/launch_exec.go:32` | `cmuxPromptSubmitAttempts` |  | `5` |
| `cmd/liza/launch_exec.go:34` | `cmuxScreenReadLines` |  | `"80"` |
| `cmd/liza/launch_resolve.go:24` | `launchBackendWezterm` | `launchBackend` | `"wezterm"` |
| `cmd/liza/launch_resolve.go:25` | `launchBackendCmux` | `launchBackend` | `"cmux"` |
| `cmd/liza/launch_resolve.go:32` | `masPresetTechnicalSpec` | `masLaunchPreset` | `"technical-spec"` |
| `cmd/liza/launch_resolve.go:33` | `masPresetFunctionalSpec` | `masLaunchPreset` | `"functional-spec"` |
| `cmd/liza/launch_resolve.go:34` | `masPresetGeneralObjective` | `masLaunchPreset` | `"general-objective"` |
| `cmd/liza/launch_resolve.go:39` | `defaultWeztermPromptDelay` |  | `2 * time.Second` |
| `cmd/liza/main.go:30` | `agentGenerationEnvSuffix` |  | `"AGENT_GENERATION"` |
| `internal/commands/await_budget.go:11` | `maxAwaitInterval` |  | `100 * time.Second` |
| `internal/commands/init_contract_placement.go:17` | `globalContractFileName` |  | `"CORE.md"` |
| `internal/commands/init_contract_placement.go:21` | `mistralConfigDirName` |  | `".vibe"` |
| `internal/commands/init_contract_placement.go:22` | `mistralPromptsDirName` |  | `"prompts"` |
| `internal/commands/init_contract_placement.go:23` | `mistralConfigFileName` |  | `"config.toml"` |
| `internal/commands/init_contract_placement.go:33` | `ContractActionDefault` | `ContractAction` | `""` |
| `internal/commands/init_contract_placement.go:35` | `ContractActionGlobal` | `ContractAction` | `"global"` |
| `internal/commands/init_contract_placement.go:37` | `ContractActionRename` | `ContractAction` | `"rename"` |
| `internal/commands/init_contract_placement.go:39` | `ContractActionLocal` | `ContractAction` | `"local"` |
| `internal/commands/init_contract_placement.go:41` | `ContractActionSkip` | `ContractAction` | `"skip"` |
| `internal/commands/init_mas_inputs.go:23` | `defaultInitBranch` |  | `"integration"` |
| `internal/commands/init_mas_layout.go:16` | `initDirMode` | `os.FileMode` | `0755` |
| `internal/commands/init_mas_layout.go:17` | `initFileMode` | `os.FileMode` | `0644` |
| `internal/commands/init_mas_state.go:20` | `initStateVersion` |  | `1` |
| `internal/commands/init_mas_state.go:21` | `initPipelineVersion` |  | `3` |
| `internal/commands/init_mas_state.go:22` | `initSprintID` |  | `"sprint-1"` |
| `internal/commands/init_mas_state.go:23` | `initSprintNumber` |  | `1` |
| `internal/commands/init_mas_state.go:24` | `initCircuitBreakerStatus` |  | `circuitBreakerStatusOK` |
| `internal/commands/init_mas_state.go:25` | `initAlignmentSummary` |  | `"Initial goal. No tasks defined yet."` |
| `internal/commands/init_mas_state.go:26` | `initGoalIDFormat` |  | `"goal-%d"` |
| `internal/commands/init_mistral_contract.go:16` | `mistralSystemPromptKey` |  | `"system_prompt_id"` |
| `internal/commands/inspect_anomalies.go:14` | `anomalyDetailsSummaryLimitBytes` |  | `200` |
| `internal/commands/inspect_vocabulary.go:10` | `inspectFormatJSON` | `inspectFormat` | `"json"` |
| `internal/commands/inspect_vocabulary.go:11` | `inspectFormatYAML` | `inspectFormat` | `"yaml"` |
| `internal/commands/inspect_vocabulary.go:12` | `inspectFormatTable` | `inspectFormat` | `"table"` |
| `internal/commands/inspect_vocabulary.go:13` | `inspectFormatValue` | `inspectFormat` | `"value"` |
| `internal/commands/inspect_vocabulary.go:24` | `inspectTargetConfig` | `inspectTarget` | `"config"` |
| `internal/commands/inspect_vocabulary.go:25` | `inspectTargetSprint` | `inspectTarget` | `"sprint"` |
| `internal/commands/inspect_vocabulary.go:26` | `inspectTargetTasks` | `inspectTarget` | `"tasks"` |
| `internal/commands/inspect_vocabulary.go:27` | `inspectTargetAgents` | `inspectTarget` | `"agents"` |
| `internal/commands/inspect_vocabulary.go:28` | `inspectTargetMetrics` | `inspectTarget` | `"metrics"` |
| `internal/commands/inspect_vocabulary.go:29` | `inspectTargetAnomalies` | `inspectTarget` | `"anomalies"` |
| `internal/commands/inspect_vocabulary.go:30` | `inspectTargetQuarantinedVerdicts` | `inspectTarget` | `"quarantined_verdicts"` |
| `internal/commands/inspect_vocabulary.go:31` | `inspectTargetProofReaffirmations` | `inspectTarget` | `"proof_reaffirmations"` |
| `internal/commands/inspect_vocabulary.go:32` | `inspectTargetHumanNotes` | `inspectTarget` | `"human_notes"` |
| `internal/commands/inspect_vocabulary.go:132` | `maxTableDescriptionLen` |  | `50` |
| `internal/commands/inspect_vocabulary.go:133` | `descriptionEllipsis` |  | `"..."` |
| `internal/commands/integration_failure_diagnostics.go:47` | `integrationFailureFallbackOperation` |  | `diagnosticUnknown` |
| `internal/commands/integration_failure_diagnostics.go:48` | `integrationFailureFallbackReason` |  | `"legacy INTEGRATION_FAILED state has no structured diagnostic"` |
| `internal/commands/integration_failure_diagnostics.go:49` | `integrationFailureFallbackHint` |  | `"inspect task history; if a PR was merged externally, run reconcile-merged with --merge-commit and -` |
| `internal/commands/provider_activation.go:18` | `repoContractActivationStateVersion` |  | `1` |
| `internal/commands/providers.go:14` | `catalogContractPolicyVersion` |  | `2` |
| `internal/commands/repair_agent_pool_capacity.go:19` | `orchestratorDemandReason` |  | `"no live orchestrator while goal is IN_PROGRESS"` |
| `internal/commands/repair_agent_pool_capacity.go:20` | `mergeOwnerDemandReason` |  | `"approved task(s) with no live, healthy merge owner"` |
| `internal/commands/repair_agent_pool_capacity.go:27` | `validationCapacityUnverified` |  | `"unverified"` |
| `internal/commands/repair_agent_pool_capacity.go:28` | `validationReadinessFailed` |  | `"failed"` |
| `internal/commands/repair_agent_pool_start.go:21` | `agentFlagCLI` |  | `"--cli"` |
| `internal/commands/repair_agent_pool_start.go:22` | `agentFlagAgentID` |  | `"--agent-id"` |
| `internal/commands/repair_agent_pool_start.go:23` | `agentFlagModelsItem` |  | `"--models-item"` |
| `internal/commands/setup.go:22` | `setupAgentToolsFile` |  | `"AGENT_TOOLS.md"` |
| `internal/commands/status_render.go:200` | `maxIntegrationSectionIDs` |  | `10` |
| `internal/commands/status_sections.go:16` | `anomalyTimestampLayout` |  | `"2006-01-02 15:04"` |
| `internal/commands/status_vocabulary.go:10` | `statusFormatJSON` | `statusFormat` | `"json"` |
| `internal/commands/status_vocabulary.go:11` | `statusFormatYAML` | `statusFormat` | `"yaml"` |
| `internal/commands/status_vocabulary.go:28` | `phaseHandoffReady` | `phaseHandoffState` | `"READY"` |
| `internal/commands/status_vocabulary.go:29` | `phaseHandoffPartialReady` | `phaseHandoffState` | `"PARTIAL_READY"` |
| `internal/commands/status_vocabulary.go:30` | `phaseHandoffCheckpointed` | `phaseHandoffState` | `"CHECKPOINTED"` |
| `internal/commands/status_vocabulary.go:31` | `phaseHandoffCompleted` | `phaseHandoffState` | `"COMPLETED"` |
| `internal/commands/status_vocabulary.go:32` | `phaseHandoffDispositionRequired` | `phaseHandoffState` | `"DISPOSITION_REQUIRED"` |
| `internal/commands/status_vocabulary.go:33` | `phaseHandoffRepairRequired` | `phaseHandoffState` | `"REPAIR_REQUIRED"` |
| `internal/commands/status_vocabulary.go:34` | `phaseHandoffMergeRequired` | `phaseHandoffState` | `"MERGE_REQUIRED"` |
| `internal/commands/status_vocabulary.go:39` | `wakeTriggerIntegrationFailed` | `agent.OrchestratorWakeTrigger` | `"INTEGRATION_FAILED"` |
| `internal/commands/status_vocabulary.go:50` | `phaseHandoffMissingTaskStatus` |  | `"MISSING"` |
| `internal/commands/status_vocabulary.go:52` | `processStatusRunning` |  | `"running"` |
| `internal/commands/stop_guard.go:15` | `stopGuardCommandLimit` |  | `200` |
| `internal/commands/stop_guard.go:66` | `unreadable` |  | `"The Stop hook could not read the background job list, so a job you started may still be running. En` |
| `internal/commands/validate_payload.go:17` | `ValidatePayloadOperation` |  | `"validate-payload"` |
| `internal/commands/watch.go:17` | `DefaultCheckInterval` |  | `10 * time.Second` |
| `internal/commands/watch.go:18` | `StallThreshold` |  | `30 * time.Minute` |
| `internal/commands/watch.go:19` | `StaleDraftThreshold` |  | `30 * time.Minute` |
| `internal/commands/watch.go:20` | `CheckpointStaleThreshold` |  | `30 * time.Minute` |
| `internal/commands/watch.go:21` | `CheckpointStuckThreshold` |  | `2 * time.Hour` |
| `internal/commands/watch.go:22` | `CheckpointAbandonedThreshold` |  | `8 * time.Hour` |
| `internal/commands/watch.go:23` | `PauseStaleThreshold` |  | `30 * time.Minute` |
| `internal/commands/watch.go:24` | `PauseForgottenThreshold` |  | `2 * time.Hour` |
| `internal/commands/watch.go:25` | `StaleSentinelThreshold` |  | `2 * time.Minute` |
| `internal/commands/watch.go:26` | `AutoRepairAgentPoolBackoff` |  | `60 * time.Second` |
| `internal/commands/watch.go:27` | `AutoRepairAgentPoolMaxStarts` |  | `3` |
| `internal/commands/watch.go:31` | `AutoRepairAgentPoolPendingTimeout` |  | `5 * time.Minute` |
| `internal/commands/watch.go:34` | `AutoRepairAgentPoolSuppressionCooldown` |  | `5 * time.Minute` |
| `internal/commands/watch.go:37` | `OrchestratorMissingGracePeriod` |  | `60 * time.Second` |
| `internal/commands/watch.go:61` | `AlertLevelWarning` |  | `alerts.AlertLevelWarning` |
| `internal/commands/watch.go:62` | `AlertLevelCritical` |  | `alerts.AlertLevelCritical` |
| `internal/commands/watch_checks_agents.go:18` | `missingRoleCachePrefix` |  | `"missing-role:"` |
| `internal/commands/watch_checks_agents.go:338` | `maxListed` |  | `5` |
| `internal/commands/watch_checks_sentinels.go:14` | `sentinelCachePrefix` |  | `"sentinel:"` |
| `internal/commands/watch_checks_sentinels.go:38` | `autoResumeCheckpointGrace` |  | `6 * time.Minute` |
| `internal/commands/watch_checks_sprint.go:16` | `circuitBreakerAlertKey` |  | `"circuit_breaker:alert"` |
| `internal/commands/watch_checks_sprint.go:17` | `sprintStalledAlertKey` |  | `"sprint_stalled:alert"` |
| `internal/commands/watch_checks_sprint.go:20` | `approachingLimitWindow` |  | `2` |
| `internal/commands/watch_checks_tasks.go:16` | `legacyBlockedCachePrefix` |  | `"blocked:"` |
| `internal/commands/watch_checks_tasks.go:17` | `finalAttemptCachePrefix` |  | `"attempt2:"` |
| `internal/commands/watch_checks_tasks.go:19` | `reviewLoopCliffCycles` |  | `5` |
| `internal/commands/watch_orchestrator.go:17` | `orchestratorMissingSinceKey` |  | `"orchestrator-missing:since"` |
| `internal/commands/watch_orchestrator.go:18` | `orchestratorMissingAlertedKey` |  | `"orchestrator-missing:alerted"` |
| `internal/commands/watch_reconcile.go:11` | `stuckAlertCachePrefix` |  | `"stuck-alert:"` |
| `internal/commands/watch_repair.go:23` | `autoRepairAgentPoolCachePrefix` |  | `"auto-repair-agent-pool:"` |
| `internal/commands/watch_repair.go:24` | `autoRepairAgentPoolStartCountPrefix` |  | `"auto-repair-agent-pool-start-count:"` |
| `internal/commands/watch_repair.go:25` | `autoRepairAgentPoolSuppressedPrefix` |  | `"auto-repair-agent-pool-suppressed:"` |
| `internal/commands/watch_repair.go:26` | `autoRepairAgentPoolPendingPrefix` |  | `"auto-repair-agent-pool-pending:"` |
| `internal/commands/watch_repair.go:45` | `autoRepairAgentPoolPendingTimeoutPrefix` |  | `"auto-repair-agent-pool-pending-timeout:"` |
| `internal/commands/watch_repair.go:46` | `autoRepairAgentPoolUnservablePrefix` |  | `"auto-repair-agent-pool-unservable:"` |
| `internal/commands/watch_repair.go:47` | `autoRepairAgentPoolProviderBlockedPrefix` |  | `"auto-repair-agent-pool-provider-blocked:"` |
| `internal/commands/watch_repair.go:48` | `autoRepairAgentPoolEnvWarningKey` |  | `"auto-repair-agent-pool-env-warning"` |
| `internal/commands/watch_repair.go:52` | `autoRepairModelsErrorKey` |  | `"auto-repair-models-error:"` |
| `internal/commands/watch_snapshot.go:15` | `pipelineConfigErrorKey` |  | `"pipeline-config-error"` |
| `internal/interactive/init_wizard.go:22` | `WizardModePairing` | `WizardMode` | `"pairing"` |
| `internal/interactive/init_wizard.go:24` | `WizardModeFull` | `WizardMode` | `"full"` |
| `internal/jsonout/classify_slots.go:19` | `codeNotFound` | `errorCode` | `"not_found"` |
| `internal/jsonout/classify_slots.go:20` | `codeRaceCondition` | `errorCode` | `"race_condition"` |
| `internal/jsonout/classify_slots.go:21` | `codeValidation` | `errorCode` | `"validation"` |
| `internal/jsonout/classify_slots.go:22` | `codePermissionDenied` | `errorCode` | `"permission_denied"` |
| `internal/jsonout/classify_slots.go:23` | `codeStaleCaller` | `errorCode` | `"stale_caller"` |
| `internal/jsonout/classify_slots.go:24` | `codeProjectRoot` | `errorCode` | `"project_root"` |
| `internal/jsonout/classify_slots.go:25` | `codePipelineConfig` | `errorCode` | `"pipeline_config"` |
| `internal/jsonout/classify_slots.go:26` | `codeStateSchema` | `errorCode` | `"state_schema"` |
| `internal/jsonout/classify_slots.go:27` | `codeWorktreeContext` | `errorCode` | `"worktree_context"` |
| `internal/jsonout/classify_slots.go:28` | `codeUnblockConflict` | `errorCode` | `"unblock_rebase_conflict"` |
| `internal/jsonout/classify_slots.go:29` | `codeLockTimeout` | `errorCode` | `"lock_timeout"` |
| `internal/jsonout/classify_slots.go:30` | `codeLockStale` | `errorCode` | `"lock_stale"` |
| `internal/jsonout/classify_slots.go:31` | `codeFilesystem` | `errorCode` | `"filesystem"` |
| `internal/jsonout/classify_slots.go:32` | `codeInternal` | `errorCode` | `"internal"` |
| `internal/jsonout/classify_slots.go:33` | `messageInternal` |  | `"internal error"` |
| `internal/jsonout/classify_slots.go:34` | `messagePostWriteFails` |  | `"validation failed: precondition not met"` |
| `internal/tui/activity.go:18` | `ActivityKindLog` | `ActivityKind` | `"log"` |
| `internal/tui/activity.go:19` | `ActivityKindAlert` | `ActivityKind` | `"alert"` |
| `internal/tui/activity.go:20` | `ActivityKindAnomaly` | `ActivityKind` | `"anomaly"` |
| `internal/tui/activity.go:26` | `activityActionStateReadError` |  | `"state_read_error"` |
| `internal/tui/activity.go:27` | `activityActionWriteError` |  | `"write_error"` |
| `internal/tui/activity.go:28` | `activityActionWatcherError` |  | `"watcher_error"` |
| `internal/tui/activity.go:33` | `levelWarning` |  | `string(alerts.AlertLevelWarning)` |
| `internal/tui/activity.go:34` | `levelCritical` |  | `string(alerts.AlertLevelCritical)` |
| `internal/tui/activity.go:38` | `maxActivityEntries` |  | `200` |
| `internal/tui/commands.go:223` | `tickInterval` |  | `10 * time.Second` |
| `internal/tui/commands.go:361` | `terminateWait` |  | `5 * time.Second` |
| `internal/tui/model.go:21` | `InputModeNormal` | `InputMode` | `iota(0)` |
| `internal/tui/model.go:22` | `InputModeInline` | `InputMode` | `iota(1)` |
| `internal/tui/model.go:29` | `InlineActionNone` | `InlineAction` | `iota(0)` |
| `internal/tui/model.go:30` | `InlineActionSpawn` | `InlineAction` | `iota(1)` |
| `internal/tui/model.go:31` | `InlineActionSpawnWith` | `InlineAction` | `iota(2)` |
| `internal/tui/model.go:32` | `InlineActionSpawnCLI` | `InlineAction` | `iota(3)` |
| `internal/tui/model.go:33` | `InlineActionPause` | `InlineAction` | `iota(4)` |
| `internal/tui/model.go:34` | `InlineActionStopConfirm` | `InlineAction` | `iota(5)` |
| `internal/tui/model.go:35` | `InlineActionTerminate` | `InlineAction` | `iota(6)` |
| `internal/tui/model.go:36` | `InlineActionTerminateConfirm` | `InlineAction` | `iota(7)` |
| `internal/tui/model.go:161` | `ColumnTierMinimal` | `ColumnTier` | `iota(0)` |
| `internal/tui/model.go:162` | `ColumnTierStandard` | `ColumnTier` | `iota(1)` |
| `internal/tui/model.go:163` | `ColumnTierWide` | `ColumnTier` | `iota(2)` |
| `internal/tui/model.go:164` | `ColumnTierFull` | `ColumnTier` | `iota(3)` |
| `internal/tui/model.go:193` | `TaskFilterAll` | `TaskFilter` | `iota(0)` |
| `internal/tui/model.go:194` | `TaskFilterActive` | `TaskFilter` | `iota(1)` |
| `internal/tui/model.go:195` | `TaskFilterActiveBlocked` | `TaskFilter` | `iota(2)` |
| `internal/tui/model.go:196` | `taskFilterCount` | `TaskFilter` | `iota(3)` |
| `internal/tui/update.go:11` | `cmdResultFlashDelay` |  | `3 * time.Second` |
| `internal/tui/update.go:12` | `alertBannerFlashDelay` |  | `10 * time.Second` |
| `internal/tui/view.go:16` | `panelOverhead` |  | `4` |
| `internal/tui/view.go:17` | `minPanelHeight` |  | `4` |
| `internal/tui/view.go:18` | `minActivityHeight` |  | `5` |
| `internal/tui/view.go:32` | `emptyCell` |  | `"—"` |
| `internal/tui/view_agents.go:21` | `agentStatusColumnWidth` |  | `14` |
| `internal/tui/view_agents.go:22` | `agentCLIColumnWidth` |  | `10` |
| `internal/tui/view_agents.go:23` | `agentHealthColumnWidth` |  | `10` |
| `internal/tui/view_agents.go:24` | `agentTimeOnTaskColumnWidth` |  | `14` |
| `internal/tui/view_agents.go:25` | `agentLastHeartbeatColumnWidth` |  | `16` |
| `internal/tui/view_agents.go:26` | `agentPIDColumnWidth` |  | `10` |
| `internal/tui/view_agents.go:219` | `expandedIdentityMinWidth` |  | `90` |
| `internal/tui/view_agents.go:252` | `rowPrefixWidth` |  | `2` |
| `internal/tui/view_agents.go:253` | `panelBorderWidth` |  | `2` |
| `internal/tui/view_agents.go:254` | `minCurrentTask` |  | `6` |
| `internal/tui/view_overlays.go:49` | `helpOverlayColumnWidth` |  | `28` |
| `internal/tui/view_tasks.go:18` | `taskStatusColumnWidth` |  | `29` |
| `internal/tui/view_tasks.go:19` | `taskAttemptColumnWidth` |  | `6` |
| `internal/tui/view_tasks.go:20` | `taskAssignedColumnWidth` |  | `16` |
| `internal/tui/view_tasks.go:21` | `taskReviewingColumnWidth` |  | `16` |
| `internal/tui/view_tasks.go:22` | `taskDepsColumnWidth` |  | `16` |
| `internal/tui/view_tasks.go:23` | `taskAgeColumnWidth` |  | `8` |
| `internal/tui/view_tasks.go:24` | `taskTimeInStatusColumnWidth` |  | `16` |
| `internal/tui/view_tasks.go:25` | `taskDescriptionMinWidth` |  | `16` |
| `internal/tui/view_tasks.go:26` | `taskRowPrefixWidth` |  | `2` |
| `internal/tui/view_tasks.go:27` | `taskPanelBorderWidth` |  | `2` |
| `internal/tui/view_tasks.go:265` | `preferredIDWidth` |  | `67` |
| `internal/tui/view_tasks.go:266` | `minIDWidth` |  | `8` |

Package variables whose initializer is a literal (replaceable at test time or set at link time, so they stay variables):

| Site | Variable |
|---|---|
| `cmd/liza/cmd_task.go:284` | `operatorInvocationHelp` |
| `cmd/liza/cmd_task.go:284` | `operatorInvocationHelp` |
| `cmd/liza/main.go:25` | `Version` |
| `cmd/liza/main.go:26` | `GitCommit` |
| `cmd/liza/main.go:27` | `BuildDate` |
| `internal/commands/status_sections.go:285` | `processStatusProcRoot` |

### 3c. Typed sets of A7 and the tests that pin them

| Type | Constants | Persisted or crossing as | Pinned by |
|---|---|---|---|
| `liza.launchBackend` (`launch_resolve.go:21`) | `launchBackendWezterm` = `"wezterm"`, `launchBackendCmux` = `"cmux"` | selector of the launch subcommand family (CLI value); never persisted | TestLaunchBackendVocabularyKeepsCLIValues (1 test references the type or its constants) |
| `liza.masLaunchPreset` (`launch_resolve.go:29`) | `masPresetTechnicalSpec` = `"technical-spec"`, `masPresetFunctionalSpec` = `"functional-spec"`, `masPresetGeneralObjective` = `"general-objective"` | MAS launch preset naming the role set (CLI value); never persisted | TestA7MASLaunchPresetsKeepRoleNames, TestMASLaunchPresetVocabularyKeepsCLIValues (2 tests reference the type or its constants) |
| `alerts.AlertLevel` (`alerts.go:15`) | `AlertLevelWarning` = `"⚠️"`, `AlertLevelCritical` = `"🚨"` | level glyph written into the alert log line (`internal/alerts`, adoption only) | TestCategoryConstantsKeepTheirLogSpelling (4 tests reference the type or its constants) |
| `alerts.Category` (`categories.go:7`) | `CategoryApproachingLimit` = `"APPROACHING LIMIT"`, `CategoryAttempt` = `"ATTEMPT"`, `CategoryAutoRepairFailed` = `"AUTO REPAIR FAILED"`, `CategoryAutoRepairUnservable` = `"AUTO REPAIR UNSERVABLE"`, `CategoryAwaitingHuman` = `"AWAITING HUMAN"`, `CategoryBlocked` = `"BLOCKED"`, `CategoryCircuitBreaker` = `"CIRCUIT BREAKER"`, `CategoryDeadAgentProcess` = `"DEAD AGENT PROCESS"`, `CategoryHypothesisExhaustion` = `"HYPOTHESIS EXHAUSTION"`, `CategoryImmediateDiscovery` = `"IMMEDIATE DISCOVERY"`, `CategoryIntegrationFailed` = `"INTEGRATION FAILED"`, `CategoryInvalidAgentOwnership` = `"INVALID AGENT OWNERSHIP"`, `CategoryInvalidState` = `"INVALID STATE"`, `CategoryLeaseExpired` = `"LEASE EXPIRED"`, `CategoryMissingRole` = `"MISSING ROLE"`, `CategoryNoUsableProvider` = `"NO USABLE PROVIDER"`, `CategoryOrchestratorMissing` = `"ORCHESTRATOR MISSING"`, `CategoryOrphanedRejected` = `"ORPHANED REJECTED"`, `CategoryPipelineConfig` = `"PIPELINE CONFIG"`, `CategoryRegisteredAgentProcess` = `"REGISTERED AGENT PROCESS"`, `CategoryReviewLeaseExpired` = `"REVIEW LEASE EXPIRED"`, `CategoryReviewLoop` = `"REVIEW LOOP"`, `CategorySprintStalled` = `"SPRINT STALLED"`, `CategoryStaleDraft` = `"STALE DRAFT"`, `CategoryStaleSentinel` = `"STALE SENTINEL"`, `CategoryStalled` = `"STALLED"` | alert category written into and parsed from the alert log line, converted at construction and comparison (`internal/alerts`, adoption only) | TestCategoryConstantsKeepTheirLogSpelling (6 tests reference the type or its constants) |
| `commands.dashboardSectionFormat` (new) (`format_helpers.go:36`) | `dashboardFormatKeyValue` = `"kv"`, `dashboardFormatTable` = `"table"`, `dashboardFormatText` = `"text"` | section kind of the status dashboard renderer; in memory | TestA7DashboardSectionFormatsKeepRendering (1 test references the type or its constants) |
| `commands.ContractAction` (`init_contract_placement.go:29`) | `ContractActionDefault` = `""`, `ContractActionGlobal` = `"global"`, `ContractActionRename` = `"rename"`, `ContractActionLocal` = `"local"`, `ContractActionSkip` = `"skip"` | contract placement action chosen by the wizard or the flag; in memory | TestContractActionPredicatesKeepFormerDecisions, TestContractActionValuesKeepFormerSpelling (10 tests reference the type or its constants) |
| `commands.inspectTarget` (`inspect_vocabulary.go:21`) | `inspectTargetConfig` = `"config"`, `inspectTargetSprint` = `"sprint"`, `inspectTargetTasks` = `"tasks"`, `inspectTargetAgents` = `"agents"`, `inspectTargetMetrics` = `"metrics"`, `inspectTargetAnomalies` = `"anomalies"`, `inspectTargetQuarantinedVerdicts` = `"quarantined_verdicts"`, `inspectTargetProofReaffirmations` = `"proof_reaffirmations"`, `inspectTargetHumanNotes` = `"human_notes"` | inspect query target (CLI value) | TestA7InspectVocabularyKeepsAcceptedSpellings (1 test references the type or its constants) |
| `commands.inspectQueryRoot` (new) (`inspect_vocabulary.go:65`) | `inspectRootAgent` = `"agent"`, `inspectRootTask` = `"task"`, `inspectRootVersion` = `"version"` | root segment of an inspect query path; converted to `NotFoundError.Entity` text | TestA7InspectVocabularyKeepsAcceptedSpellings (1 test references the type or its constants) |
| `commands.inspectFormat` (`inspect_vocabulary.go:7`) | `inspectFormatJSON` = `"json"`, `inspectFormatYAML` = `"yaml"`, `inspectFormatTable` = `"table"`, `inspectFormatValue` = `"value"` | `--format` of the inspect commands (CLI value) | TestA7InspectVocabularyKeepsAcceptedSpellings, TestInspectFormatValuesKeepFormerSpelling (8 tests reference the type or its constants) |
| `commands.computedField` (new) (`inspect_vocabulary.go:78`) | `computedFieldActiveCount` = `"active_count"`, `computedFieldUtilization` = `"utilization"`, `computedFieldCompletionRate` = `"completion_rate"`, `computedFieldAvgIterationCount` = `"avg_iteration_count"`, `computedFieldElapsed` = `"elapsed"`, `computedFieldRemaining` = `"remaining"`, `computedFieldProgressPercent` = `"progress_percent"`, `computedFieldTimeSinceHeartbeat` = `"time_since_heartbeat"`, `computedFieldTimeOnTask` = `"time_on_task"`, `computedFieldTransitionID` = `"transition_id"`, `computedFieldAge` = `"age"`, `computedFieldTimeInStatus` = `"time_in_status"` | name of a computed field in an inspect query path | TestA7InspectVocabularyKeepsAcceptedSpellings (1 test references the type or its constants) |
| `commands.worktreeState` (new) (`integration_failure_diagnostics.go:36`) | `worktreeStateNotRecorded` = `"not_recorded"`, `worktreeStateRecordedUnchecked` = `"recorded_unchecked"`, `worktreeStatePresent` = `"present"`, `worktreeStateMissing` = `"missing"`, `worktreeStateUnknown` = `diagnosticUnknown` | `worktree_state` of the integration-failure diagnostic, converted into the `map[string]any` detail | TestA7IntegrationFailureFallbackKeepsDiagnosticShape (1 test references the type or its constants) |
| `commands.phaseHandoffState` (`status_vocabulary.go:25`) | `phaseHandoffReady` = `"READY"`, `phaseHandoffPartialReady` = `"PARTIAL_READY"`, `phaseHandoffCheckpointed` = `"CHECKPOINTED"`, `phaseHandoffCompleted` = `"COMPLETED"`, `phaseHandoffDispositionRequired` = `"DISPOSITION_REQUIRED"`, `phaseHandoffRepairRequired` = `"REPAIR_REQUIRED"`, `phaseHandoffMergeRequired` = `"MERGE_REQUIRED"` | `state` of the status phase-handoff section (`--json`/`--format yaml` bytes), converted at assembly | TestPhaseHandoffStatesKeepFormerSpelling (2 tests reference the type or its constants) |
| `commands.statusFormat` (`status_vocabulary.go:7`) | `statusFormatJSON` = `"json"`, `statusFormatYAML` = `"yaml"`, `statusFormatDashboard` = `"dashboard"` | `--format` of the status command (CLI value) | TestA7StatusVocabularyKeepsAcceptedSpellings, TestStatusFormatValuesKeepFormerSpelling (3 tests reference the type or its constants) |
| `interactive.WizardMode` (`init_wizard.go:18`) | `WizardModePairing` = `"pairing"`, `WizardModeFull` = `"full"` | init wizard result mode; selects the post-init summary | TestWizardModeValuesKeepFormerSpelling (1 test references the type or its constants) |
| `jsonout.errorCode` (`classify_slots.go:16`) | `codeNotFound` = `"not_found"`, `codeRaceCondition` = `"race_condition"`, `codeValidation` = `"validation"`, `codePermissionDenied` = `"permission_denied"`, `codeStaleCaller` = `"stale_caller"`, `codeProjectRoot` = `"project_root"`, `codePipelineConfig` = `"pipeline_config"`, `codeStateSchema` = `"state_schema"`, `codeWorktreeContext` = `"worktree_context"`, `codeUnblockConflict` = `"unblock_rebase_conflict"`, `codeLockTimeout` = `"lock_timeout"`, `codeLockStale` = `"lock_stale"`, `codeFilesystem` = `"filesystem"`, `codeInternal` = `"internal"` | `ErrorDetail.Code` of the `--json` envelope, converted at the envelope; `OperationalError.Code` passes through (open set) | TestA7ErrorCodesKeepTheirSpellings (1 test references the type or its constants) |
| `tui.ActivityKind` (`activity.go:15`) | `ActivityKindLog` = `"log"`, `ActivityKindAlert` = `"alert"`, `ActivityKindAnomaly` = `"anomaly"` | kind of an activity-feed entry; in memory | TestActivityKindsKeepTheirSpellings (1 test references the type or its constants) |
| `tui.ColumnTier` (`model.go:158`) | `ColumnTierMinimal` = `iota(0)`, `ColumnTierStandard` = `iota(1)`, `ColumnTierWide` = `iota(2)`, `ColumnTierFull` = `iota(3)` | TUI column tier chosen from the terminal width; in memory | TestA7ColumnTiersKeepTheirBoundaries, TestColumnTierEnum, TestColumnTierForWidth, TestRenderAgentPanel_ColumnTierFull, TestRenderAgentPanel_ColumnTierMinimal, TestRenderAgentPanel_ColumnTierStandard, TestRenderAgentPanel_ColumnTierWide, TestRenderAgentPanel_StandardTierFitsNarrowWidth, TestRenderAgentPanel_UnparkedAgentKeepsStatus, TestRenderHelpOverlay_ContainsExpectedKeyLabels, TestRenderTaskPanel_ColumnTierFull, TestRenderTaskPanel_ColumnTierMinimal, TestRenderTaskPanel_ColumnTierStandard, TestRenderTaskPanel_ColumnTierWide, TestRenderTaskPanel_TaskIDShrinksToFitFullTier, TestUpdateWindowSizeMsgSetsColumnTier (63 tests reference the type or its constants) |
| `tui.InputMode` (`model.go:18`) | `InputModeNormal` = `iota(0)`, `InputModeInline` = `iota(1)` | TUI input mode; in memory | TestHandleInlineKey_EnterSpawnWithMissingRoleTypeKeepsNormalMode, TestInputModeEnum, TestRenderFooter_InlineModeShowsLabel, TestRenderFooter_NormalMode_ContainsExpectedKeyLabels (42 tests reference the type or its constants) |
| `tui.TaskFilter` (`model.go:190`) | `TaskFilterAll` = `iota(0)`, `TaskFilterActive` = `iota(1)`, `TaskFilterActiveBlocked` = `iota(2)`, `taskFilterCount` = `iota(3)` | TUI task filter; in memory | TestA7TaskFilterLabelsKeepTheirSpellings (5 tests reference the type or its constants) |
| `tui.InlineAction` (`model.go:26`) | `InlineActionNone` = `iota(0)`, `InlineActionSpawn` = `iota(1)`, `InlineActionSpawnWith` = `iota(2)`, `InlineActionSpawnCLI` = `iota(3)`, `InlineActionPause` = `iota(4)`, `InlineActionStopConfirm` = `iota(5)`, `InlineActionTerminate` = `iota(6)`, `InlineActionTerminateConfirm` = `iota(7)` | TUI inline action; in memory | TestHandleInlineKey_EnterSpawnWithMissingRoleTypeKeepsNormalMode, TestInlineActionEnum (33 tests reference the type or its constants) |

No A7 type duplicates an A1, A3 or A6 set: roles are `roles.RoleType`, history events and anomalies are `models.TaskEventName` and `models.AnomalyType`, safe actions and effects are `ops.LifecycleSafeAction` and `ops.LifecycleEffects`, integration reasons are `ops.IntegrationReason` (table 8).

White-box tests added by this task (one new file per package; each pins the spelling of the constants it names against the literal it replaced, so a value change fails by name):

| Test file | Tests |
|---|---|
| `cmd/liza/k7_a7_vocabulary_test.go` | TestA7CompletionValuesKeepAcceptedSpellings, TestA7MASLaunchPresetsKeepRoleNames, TestA7ValidateRoleTypeNamesAllowedTypes, TestA7PairingLaunchVocabularyKeepsSpellings, TestA7NamedMagicValuesKeepTheirValues |
| `internal/commands/k7_a7_vocabulary_test.go` | TestA7InspectVocabularyKeepsAcceptedSpellings, TestA7StatusVocabularyKeepsAcceptedSpellings, TestA7KernelVocabularyKeepsWireSpellings, TestA7HistoryExtraKeysKeepTheirSpellings, TestA7WatchVocabularyKeepsKeyShapes, TestA7IntegrationFailureFallbackKeepsDiagnosticShape, TestA7DashboardSectionFormatsKeepRendering, TestA7ScipPlanSyntaxKeepsLanguageSpellings |
| `internal/jsonout/k7_a7_vocabulary_test.go` | TestA7ErrorCodesKeepTheirSpellings |
| `internal/tui/k7_a7_vocabulary_test.go` | TestA7ColumnTiersKeepTheirBoundaries, TestA7TaskFilterLabelsKeepTheirSpellings, TestA7StatusMapKeysKeepModelSpellings, TestA7LayoutAndAnswerConstantsKeepTheirValues |

## 4. False friends: same spelling, different meaning

Each group below was matched by the value scan (a constant of this task, or a literal of an A7 file, whose spelling another constant or literal also carries) and deliberately **not** merged: the names mean different things, and a shared constant would couple them.

| Spelling | Meanings | Disposition |
|---|---|---|
| `"none"` | `contractKeyNone` (cmd/liza/cmd_agent.go:33: the provider catalog's key for a CLI without a contract file); `ops.LifecycleEffectsNone` (internal/ops/lifecycle_result.go:35: an effects class); `models.InheritModeNone`, `models.RestoreModeNone`, `ops.postWorktreeDetectionNone` (other sets); display placeholders (cmd/liza/cmd_plan_check.go:208, internal/commands/analyze.go:85) | Only `contractKeyNone` is named in A7. The effects class is adopted as `ops.LifecycleEffectsNone` at `lifecycle.go` and the three `WrapLifecycleError` sites (table 2); the placeholders are display text (table 3) |
| `"unknown"` | `unknownRoleLabel` (internal/commands/inspect_vocabulary.go:113: the label of an empty role in the metrics and validation rows), `diagnosticUnknown` and `worktreeStateUnknown` (internal/commands/integration_failure_diagnostics.go:13: an absent diagnostic field and a failed worktree probe), the link-time build metadata defaults (cmd/liza/main.go:26, cmd/liza/main.go:27), `procscan.AgentProcessUnknown`, `models.RejectionCauseUnknown`, `ops.LifecycleEffectsUnknown` | The role label and the diagnostic fallback are two names (different views, different fallbacks); the fallback operation and the unknown worktree state deliberately share `diagnosticUnknown` (same file, same meaning). Build metadata stays a link-time variable (table 3b); the owner constants are other sets |
| `"operator"` | `OperatorActor` (internal/commands/agent_health.go:22: the actor recorded for an operation the human operator runs), `ops.actorOperator` (internal/ops/helpers.go:20, unexported), `models.AcceptanceDeferralOperator` (internal/models/acceptance.go:109: the origin of an acceptance deferral) | `OperatorActor` named in `commands` (R3, table 5) and adopted by `tui/commands.go`; the models constant names a different attribute and is not adopted |
| `"human"` | `defaultChangedBy` (cmd/liza/main.go:282: the audit actor of a command without `--changed-by`), `ops.actorHuman` (internal/ops/helpers.go:21, unexported), the `human` oracle kind of the test helpers (outside A7) | Named once in `cmd/liza` (R3, table 5); the test-helper kind is a different set in a different package |
| `"system"` | `autoRepairLogActor` (internal/commands/watch_repair_spawn.go:16: the agent field of the log entry auto-repair appends), `ops.actorSystem` (internal/ops/helpers.go:22, unexported), the handoff-event agent of `internal/agent/claiming.go` (A9) | Named once in `commands` (R3, table 5); the other two sites belong to their owners (table 7) |
| `"APPROVED"` | `verdictApproved` (internal/commands/submit_verdict.go:40: the stored verdict an approval carries), `ops.VerdictApproved` (internal/ops/await_verdict_outcome.go:20: an `AwaitVerdictOutcome`, exported), `ops.submittedVerdictApproved` (internal/ops/verdict_transition.go:19: the stored verdict, unexported) | `verdictApproved` is the stored verdict (R3, table 5). `ops.VerdictApproved` names an await outcome, not the stored verdict, so it is not adopted for `printVerdictResult` |
| `"task"`, `"agent"` | `inspectRootTask`, `inspectRootAgent` (internal/commands/inspect_vocabulary.go:72: roots of an inspect query path), `ops.notFoundEntityTask`, `ops.notFoundEntityAgent` (internal/ops/helpers.go:14, unexported), the `agent` subcommand word of the relaunch argv (internal/commands/recover_agent.go:17) | The inspect roots are typed and converted to the `NotFoundError.Entity` text at 13 inspect sites (table 2); the ops entity names are the owner's and stay unadopted (table 5); the subcommand word is argv (table 3) |
| `"running"` | `processStatusRunning` (internal/commands/status_vocabulary.go:52: the display status `procscan.AgentProcessStatus.DisplayStatus` returns), `backgroundJobStatusRunning` (internal/commands/stop_guard.go:29: the status of a background job in the Claude Code Stop-hook envelope), `models.SystemModeRunning` (`"RUNNING"`, a system mode) | Two names for two producers (a process probe and the provider's hook envelope); the system mode is a different case and set |
| `"block"` | `stopGuardDecisionBlock` (internal/commands/stop_guard.go:27: the decision the Stop-hook command emits), the Codex hook decision (cmd/liza/cmd_tool_result_codex.go:51) | Each is the wire value of a different provider hook protocol; the Codex one stays a classified wire literal (table 3) |
| `"yaml"`, `"table"` | `yamlTagKey` (internal/commands/inspect_vocabulary.go:110: the struct-tag key the field resolver reads), `inspectFormatYAML` / `statusFormatYAML` (output formats), `dashboardFormatTable` (internal/commands/format_helpers.go:40: a dashboard section kind) and `inspectFormatTable` | Four names; the tag key, the two output formats and the section kind are different sets that happen to share a spelling |
| `"operation"` | `diagnosticKeyOperation` (internal/commands/integration_failure_diagnostics.go:19: a key of the integration-failure diagnostic), `historyExtraOperation` (internal/commands/inspect_task_assembly.go:118: a key of the dependency-repair history `Extra`), the `operation` key of `errors` details and of the human-note `Extra` copy (internal/commands/inspect.go:249) | Different maps written by different producers; each is named (or classified) with its own map |
| `"status"` | `diagnosticKeyStatus` (internal/commands/integration_failure_diagnostics.go:21), `ops.deferredValidationSubcommandStatus` (internal/ops/deferred_validation.go:90: a branded subcommand), the `STATUS` column header | Diagnostic key named; the subcommand is the owner's; the header is display text |
| `"worktree"` | `diagnosticKeyWorktree` (internal/commands/integration_failure_diagnostics.go:25), `ops.deferredLaneWorktreeName` (internal/ops/deferred_validation.go:68), `ops.replacePreservedFieldWorktree` (internal/ops/replace_task_admission.go:34), the Git `worktree` subcommand (C4 argv) | Four meanings; only the diagnostic key is A7's |
| `"missing"`, `"MISSING"` | `worktreeStateMissing` (internal/commands/integration_failure_diagnostics.go:42), `phaseHandoffMissingTaskStatus` (`"MISSING"`, a planned task absent from state), the agent status word of internal/commands/watch_checks_tasks.go:86, `toolchain.DoctorMissing` and the other `missing` sets | Each is a different set; the watch display word stays a display placeholder (table 3) |
| `"narrowed"`, `"skipped"` | `narrowChildActionNarrowed`, `narrowChildActionSkipped` (internal/commands/narrow_inherited_dependencies.go:23), `ops.narrowActionNarrowed`, `ops.narrowActionSkipped` (internal/ops/narrow_inherited_dependencies.go:114, unexported), `ops.deferredValidationOutcomeSkipped`, `toolchain.InstallSkipped` | The A7 names mirror the ops action words (same meaning, R3, table 5); the deferred-validation and toolchain constants are other sets |
| `"OK"`, `"TRIGGERED"` | `circuitBreakerStatusOK`, `circuitBreakerStatusTriggered` (internal/commands/status_vocabulary.go:44), `ops.circuitBreakerStatus*` (internal/ops/mode_change.go:45, unexported), the literals of `models.CircuitBreaker.IsValidStatus` | Same meaning as the ops constants; named in `commands` (R3, table 5) because the owners keep them unexported |
| `"complete"` | `integrationStatusComplete` (internal/commands/status_orchestrator.go:16: the status of a clean integration completion), the footer hint word (internal/tui/view_footer.go:57), the `…Complete` wake triggers of `internal/agent` | The status is named (R3, table 5); the hint is display text; the triggers are `agent.WakeTrigger` values |
| `"active"` | `taskFilterLabelActive` (internal/tui/model.go:206: the TUI filter label), `usage.OutcomeActive` | Different sets in different packages |
| `"reviewer"`, `"doer"` | the role types `roles.RoleTypeReviewer`, `roles.RoleTypeDoer`; the `--reviewer` flag name; the `reviewer` JSON key of the status queue section (`status_types.go:146`); the `reviewer-` agent ID prefix (`pairingReviewerIDPrefix`) | Role types are adopted where the value is a role type (table 2); the flag, the JSON key and the ID prefix are different vocabularies and stay named or classified on their own |
| `"y"`, `"yes"` | `confirmAnswerY`, `confirmAnswerYes` (internal/commands/prompt_vocabulary.go:6: accepted typed answers of a console prompt), `inlineConfirmYes` (internal/tui/update_inline.go:146: the key of the TUI inline confirmation) | Console answers and a TUI key are different inputs; each set is named in its own package |

## 5. Owner gaps (R3) and owner findings

Each A7 name below decides control flow or crosses a boundary on a spelling whose owner package keeps it unexported (or names no constant), so A7 cannot adopt it without editing the owner (out of scope: C1–C6 and other areas' logic). A7 names the value beside its use, with a comment that points at the owner, and records the gap here. The owner-side sites are table 7.

| Value | A7 name | Gap | Disposition |
|---|---|---|---|
| `"operator"` | `OperatorActor` (internal/commands/agent_health.go:22) | `ops.actorOperator` (internal/ops/helpers.go:20) is unexported; `models` names no actor | Named and exported in `commands` because the TUI (`tui/commands.go`, three sites) imports `commands`; `ops` is not edited |
| `"human"` | `defaultChangedBy` (cmd/liza/main.go:282) | `ops.actorHuman` (internal/ops/helpers.go:21) is unexported | Named beside `resolveChangedBy` |
| `"system"`, `"auto_repair_agent_spawned"` | `autoRepairLogActor`, `autoRepairLogActionSpawned` (internal/commands/watch_repair_spawn.go:16) | `ops.actorSystem` (internal/ops/helpers.go:22) is unexported; the log action word has no owner | Named beside the one log entry auto-repair appends; the action word is pinned by the A7 oracle golden (`auto-repair-pool/backoff.txt`) |
| `"OK"`, `"TRIGGERED"` | `circuitBreakerStatusOK`, `circuitBreakerStatusTriggered` (internal/commands/status_vocabulary.go:44) | `ops.circuitBreakerStatus*` (internal/ops/mode_change.go:45) is unexported; `models.CircuitBreaker.IsValidStatus` compares literals | Named in `commands`; `initCircuitBreakerStatus` derives from the same constant |
| `"APPROVED"` (stored verdict) | `verdictApproved` (internal/commands/submit_verdict.go:40) | `ops.submittedVerdictApproved` (internal/ops/verdict_transition.go:19) is unexported; the exported `ops.VerdictApproved` is an await outcome (table 4) | Named beside `printVerdictResult` |
| `"narrowed"`, `"skipped"` | `narrowChildActionNarrowed`, `narrowChildActionSkipped` (internal/commands/narrow_inherited_dependencies.go:23) | `ops.narrowAction*` (internal/ops/narrow_inherited_dependencies.go:114) is unexported | Named beside the narrow-inherited-dependencies renderer |
| `"$"` (sentinel assignee prefix) | `sentinelAssigneePrefix` (internal/commands/watch.go:50) | `ops.transitionSentinelPrefix` (internal/ops/transition_attempt.go:29) is unexported; `models`, `statevalidate` repeat the literal (table 7) | Named in the watch checks that test an assignee for a sentinel |
| `"retarget_dependency_rejected"`, `"diagnostic_action"` | `retargetRejectedAction`, `retargetRejectedDetailKey` (cmd/liza/cmd_task_dependencies.go:202) | `ops.retargetDependencyRejectedAction` (internal/ops/retarget_dependency.go:22) is unexported and the key is a literal of the ops writer (ops/retarget_dependency.go:281) | Named beside the verbose-diagnostic writer |
| `3` (questions per blocker) | `maxBlockedQuestions` (cmd/liza/cmd_task_blocked.go:445) | `ops.maxBlockedQuestions` (internal/ops/assess_blocked.go:231) is unexported | Named beside `assessBlockedOptionsFromFlags`; the refusal message keeps its frozen text |
| history `Extra` keys `repair_source_task`, `operation`, `repair_request_cleared`, `affected_task_ids`, `repair_validation` | `historyExtra*` (internal/commands/inspect_task_assembly.go:117) | The ops dependency-repair writers spell them as literals (ops/dependency_repair_request.go:43; ops/dependency_repair_request.go:52; ops/apply_dependency_repair.go:310) | Named beside the repair-receipt reader |
| history `Extra` keys `diagnostic`, `pr_url`; diagnostic keys `operation`, `reason`, `recovery_hint`, `merge_commit`, … | `historyExtraDiagnostic`, `historyExtraPRURL`, `diagnosticKey*` (internal/commands/integration_failure_diagnostics.go:19) | The ops writer builds the diagnostic with literal keys (ops/merge_failure.go:155) and `pr_url` (ops/reconcile_merged.go:138) | Named beside the reader and the legacy fallback that builds the same shape; A6 recorded the `recovery_hint` key as its own gap |
| `"none"` (contract key) | `contractKeyNone` (cmd/liza/cmd_agent.go:33) | `providers.Provider.ContractKey` (providers/catalog.go:154) and `agent.ContractKeyForCLI` (agent/cli.go:88) name no sentinel | Named beside the warning that skips CLIs without a contract file |
| `"complete"` (integration status) | `integrationStatusComplete` (internal/commands/status_orchestrator.go:16) | `prompts` writes the status as a literal (prompts/wake.go:100) and `internal/agent` compares it (agent/workdetection.go:258) | Named beside `buildOrchestratorStatus` |
| `"running"` (process display status) | `processStatusRunning` (internal/commands/status_vocabulary.go:52; named by Task 7) | `procscan.AgentProcessStatus.DisplayStatus` returns the literal (procscan/process_status.go:63) | Already named in A7; recorded here as the owner gap |
| `NotFoundError.Entity` `"task"`, `"agent"` | `inspectRootTask`, `inspectRootAgent` | `ops.notFoundEntityTask`, `ops.notFoundEntityAgent` (internal/ops/helpers.go:14) are unexported | The inspect sites convert A7's own query roots; the ops entity names are not adopted (table 4) |
| `"accepted"`, `"refuted"`, `"superseded"`, `"escalated"` | classified data (completion candidates of `reconcile-verdict`) | `ops.reconcileDisposition*` (internal/ops/quarantined_verdict.go:36) is unexported | Classified (table 3): the list is a completion candidate set and decides no A7 control flow |
| `"windows"` (`runtime.GOOS`) | literal at one site | Go names no platform constant (cmd/liza/launch_exec.go:389); `ops` repeats the literal (ops/admit.go:58; ops/deferred_validation.go:182; ops/post_worktree_setup.go:247) | Classified as a platform identifier (table 3), not named locally, to avoid a second copy of a shared literal |

Prospective A1/A3/A6 symbols: none was undelivered. The A1 vocabulary (`models.TaskEventName`, `AnomalyType`, `InheritMode`, `roles.RoleType`), the A3 lifecycle vocabulary (`ops.LifecycleSafeAction`, `LifecycleEffects`, `models.LifecycleOutcome`) and the A6 `ops.IntegrationReason` are present at the coding HEAD (the generator fails with BLOCKED otherwise), so there is no BLOCKED finding against any area's closure.

## 6. Conformance sites edited outside A7 files

None. No production file outside the five A7 packages changes: `git diff --name-only f71ac787f d36ca99a3 -- cmd internal` names only files of `cmd/liza`, `internal/commands`, `internal/interactive`, `internal/jsonout` and `internal/tui` (the generator asserts this), `internal/oracle/**` is untouched, and no `ops`, `models`, `roles`, `prompts`, `agent`, `alerts` or `errors` file is edited, so no X2 caller edit exists. The adoption-only packages are read, not written: A7 uses `alerts.Category*` (`internal/alerts/categories.go`) and the `internal/errors` types as they are. Scanned for adoption sites and unchanged: `internal/alerts/alerts.go`, `internal/alerts/categories.go`, `internal/errors/classification.go`, `internal/errors/doc.go`, `internal/errors/errors.go`.

No white-box test is edited: the only test changes are the four new files of table 3c (all `A`, asserted by the generator against `git diff --name-status`), because every substitution keeps the compared value and no test expression names a constant that changed.

## 7. Later-area adoption sites (X7: adopted when those areas are refactored)

| Owner-side site | Spelling | A7 name | Area |
|---|---|---|---|
| models/history.go:484 | `"OK"`, `"TRIGGERED"` | `circuitBreakerStatus*` | A1 (`models`) |
| models/diagnostics.go:123, models/diagnostics.go:280; models/task_claimability.go:35; statevalidate/task_assignments.go:24; statevalidate/validate_agent.go:68 | `"$"` sentinel prefix | `sentinelAssigneePrefix` | A1 (`models`, `statevalidate`) |
| agent/claiming.go:680 | `"system"` log actor | `autoRepairLogActor` | A9 (`agent`) |
| prompts/wake.go:100; agent/workdetection.go:258 | `"complete"` integration status | `integrationStatusComplete` | A8 (`prompts`), A9 (`agent`) |
| procscan/process_status.go:63 | `"running"` display status | `processStatusRunning` | `procscan` owner |
| internal/ops/helpers.go:20, internal/ops/helpers.go:21, internal/ops/helpers.go:22 | `"operator"`, `"human"`, `"system"` actors | `OperatorActor`, `defaultChangedBy`, `autoRepairLogActor` | A3–A6 `ops`: export or share the actor words, then A7 adopts them |
| internal/ops/mode_change.go:45; internal/ops/verdict_transition.go:19; internal/ops/narrow_inherited_dependencies.go:114; internal/ops/transition_attempt.go:29 | circuit-breaker status, stored verdict, narrow actions, sentinel prefix | `circuitBreakerStatus*`, `verdictApproved`, `narrowChildAction*`, `sentinelAssigneePrefix` | A3–A6 `ops`: export, then A7 adopts |
| internal/ops/retarget_dependency.go:22; internal/ops/assess_blocked.go:231 | retarget rejection action, questions per blocker | `retargetRejectedAction`, `maxBlockedQuestions` | A3–A6 `ops`: export, then A7 adopts |
| ops/dependency_repair_request.go:52; ops/merge_failure.go:155; ops/reconcile_merged.go:138 | history `Extra` and diagnostic keys | `historyExtra*`, `diagnosticKey*` | A3–A6 `ops`: name the keys, then A7 adopts |
| internal/ops/quarantined_verdict.go:36 | reconcile dispositions | completion list classified in table 3 | A3–A6 `ops`: export, then A7 adopts |
| agent/cli.go:88; providers/catalog.go:154 | `"none"` contract key | `contractKeyNone` | provider catalog owner |

The `internal/agent` and `internal/ops` literals above are untouched by this task: A9 and the A3–A6 areas own them, and an A7-only edit there would be a behaviour-neutral change outside the approved scope.

## 8. Plan K7 vocabulary rows

Reconciliation of the eleven rows of the plan section "K7 Vocabulary" (`a7-k7`) with the tree. "Sites" counts the rows of table 2 that fall in the family; "residual" counts the literals of A7 files (outside declarations) that still carry a value of the family's set, each classified in table 3.

| Plan row | Reconciliation at the coding HEAD |
|---|---|
| `--json` error codes (`not_found`, `validation`, `permission_denied`, `stale_caller`, …) | Delivered by Task 2: `jsonout.errorCode` (internal/jsonout/classify_slots.go, 14 constants) with `ErrorDetail.Code` staying `string`; codes from `OperationalError.Code` pass through. Residual: 3 remain, all classified (flag or CLI spelling 3; table 3). This task pins the 14 spellings by name (`TestA7ErrorCodesKeepTheirSpellings`). Sites: 0 |
| Lifecycle outcome names read by classification (`INVALID_INPUT`, `FORBIDDEN`) | Adopted: `jsonout` references `models.Lifecycle*` 2 times in 1 files; no second type exists. Residual: none remains. Sites: 0 |
| Alert categories emitted and matched by watch and the TUI | Delivered by Task 9: `alerts.Category` (internal/alerts/categories.go, 26 constants), `Alert.Category` stays `string`; A7 references the constants 52 times in 12 files. This task adopts the last two spellings that Task 9 left: `CIRCUIT BREAKER` in `tui/view_agents.go` and `INVALID STATE` in `watch_reconcile.go`. Residual: 2 remain, all classified (message or display text 2; table 3). Literals emitted in A3–A6 `ops` and A9 stay with their owners (table 7). Sites: 1 |
| Role types in watch and pool switches, history events, anomaly types | Adopted: `roles.RoleType*` 22 references in 12 files (this task adds `validateRoleType`'s typed parameter and its 10 call sites in 8 files, the MAS launch presets, the pairing prompts and the `defaults:` keys of the generated `models.yaml`), `models.TaskEvent*` / `models.Anomaly*` 14 references in 5 files. Residual role-type spellings: 6 remain, all classified (flag or CLI spelling 6; table 3); residual event and anomaly spellings: 1 remain, all classified (data 1; table 3). Sites: 56 |
| Lifecycle safe actions, outcomes, effect classes | Adopted: `ops.LifecycleSafeAction*` / `LifecycleEffects*` 16 references in 6 files; no second type is defined. This task adopts the last raw spellings: the three `WrapLifecycleError` sites and `lifecycle.go` (`stop`/`none`), `recover_agent.go` and `validate_payload.go`. Residual: 5 remain, all classified (cobra definition 1, message or display text 3, wire key 1; table 3). Sites: 7 |
| Integration reasons switched on by `commands/wt_merge.go` | Adopted by conversion since Task 5/2: `ops.IntegrationReason*` 4 references in 1 files (`wt_merge.go`, `jsonout/classify.go`). Residual: none remains. Sites: 0 |
| Inspect output formats and inspect targets | Delivered by Task 7 (`inspectFormat`, `inspectTarget`, `statusFormat`, `phaseHandoffState`); this task adds `inspectQueryRoot`, `computedField` and `dashboardSectionFormat`, exports the spellings `cmd/liza` needs (`InspectFormatNames`, `StatusFormatNames`, `InspectTarget*`, `InspectFormatJSON`) so the completion and routing code stops repeating them, and replaces `usage_report.go`'s private format list with `inspectFormats`. CLI values are unchanged (pinned by `TestA7InspectVocabularyKeepsAcceptedSpellings`, `TestA7StatusVocabularyKeepsAcceptedSpellings`). Residual: 40 remain, all classified (external argv 3, branded composition 16, cobra definition 2, flag or CLI spelling 17, wire key 2; table 3). Sites: 58 |
| TUI activity-entry kinds, inline actions, modes | Delivered: `ActivityKind`, `InlineAction`, `InputMode`, `TaskFilter`, `ColumnTier` (internal/tui/activity.go, internal/tui/model.go, internal/tui/model.go, internal/tui/model.go, internal/tui/model.go). This task names the status-color keys by `models` constants, the column-tier boundaries, the filter labels, the panel and overlay chrome and the agent column widths. Residual: 2 remain, all classified (flag or CLI spelling 2; table 3). Sites: 5 |
| Launch backends, presets | Delivered by Task 4 (`launchBackend`, `masLaunchPreset`, cmd/liza/launch_resolve.go); this task builds the MAS role presets from `roles.RoleType*`/role constants, names the cmux and wezterm split directions and the reviewer ID prefix. The cmux/wezterm argv words are external CLI vocabulary (table 3, `external argv`). Residual: 6 remain, all classified (dynamic identity 6; table 3). Sites: 18 |
| Thresholds, flash and prompt delays, poll intervals, retry counts | Named in their concern, values unchanged (24 constants added by this task: `autoAssignAgentIDAttempts`, `autoRepairPendingEntryFields`, `columnTierFullMinWidth`, `columnTierStandardMinWidth`, `columnTierWideMinWidth`, `compactAgentIDWidth`, `compactAgentRoleWidth`, `defaultTaskPriority`, `deleteAgentTerminateWait`, `expandedAgentIDWidth`, `expandedAgentRoleWidth`, `finalTaskAttempt`, `headerFooterHeight`, `hypothesisExhaustionFailures`, `maxBlockedQuestions`, `overlayChromeWidth`, `panelChromeWidth`, `percentScale`, `provisionInstanceIDWidth`, `shortCommitWidth`, `shortObjectIDWidth`, `supervisorBootstrapFileMode`, `supervisorLogFileMode`, `taskPanelTitleMinGap`); the watch thresholds were already named, and test-replaceable timing variables stay variables (table 3b). Sites: 34 |
| Role names, tool names, configured commands, catalog identities | Stay dynamic (ADR-0045, K7): table 9 |

Beyond the plan's initial inventory (RP11 was a starting list), the scan found A7-local finite vocabularies that decide control flow or cross a boundary; they are named in A7 and counted as family V12: 123 sites (`OperatorActor`, `alertIdentitySeparator`, `autoRepairLogActionSpawned`, `autoRepairLogActor`, `autoRepairPendingEntryFormat`, `autoRepairPendingEntrySeparator`, `backgroundJobStatusRunning`, `boolArgFalse`, `boolArgTrue`, `circuitBreakerStatusOK`, `circuitBreakerStatusTriggered`, `confirmAnswerY`, `confirmAnswerYes`, `contractKeyNone`, `defaultChangedBy`, `diagnosticKeyFailedBy`, `diagnosticKeyOperation`, `diagnosticKeyReason`, `diagnosticKeyRecoveryHint`, `diagnosticKeyStatus`, `diagnosticKeyWorktree`, `diagnosticKeyWorktreeCheckError`, `diagnosticKeyWorktreeMissing`, `diagnosticKeyWorktreeRecorded`, `diagnosticKeyWorktreeState`, `diagnosticUnknown`, `emptyCell`, `headRef`, `historyExtraAffectedTaskIDs`, `historyExtraDiagnostic`, `historyExtraOperation`, `historyExtraPRURL`, `historyExtraRepairRequestCleared`, `historyExtraRepairSourceTask`, `historyExtraRepairValidation`, `integrationStatusComplete`, `narrowChildActionNarrowed`, `narrowChildActionSkipped`, `pendingEntryAgentID`, `pendingEntryModelsItem`, `pendingEntryPID`, `pendingEntryRole`, `queryPathSeparator`, `registeredProcessKeyFormat`, `retargetRejectedAction`, `retargetRejectedDetailKey`, `sentinelAssigneePrefix`, `sprintTimelinePath`, `stdinFileArg`, `stopGuardDecisionBlock`, `stopGuardKeyBackgroundJobs`, `tagOmitted`, `taskFieldGenerationDigest`, `unknownRoleLabel`, `verdictApproved`, `worktreeStateMissing`, `worktreeStateNotRecorded`, `worktreeStatePresent`, `worktreeStateRecordedUnchecked`, `worktreeStateUnknown`, `yamlTagKey`, `zeroDurationText`).

Proof of "no behaviour change": `git diff --stat` of `internal/oracle` is empty (no golden, fixture or oracle file is edited); the changed lines that mention `exec` or the Git helpers number 0 added and 0 removed (a naming change does not add a subprocess, a Git call or a `Modify` closure, so K1 and the Preserved Sites C1, C4 and C6 are untouched); the exported surface grows only by `InspectFormatJSON`, `InspectFormatNames`, `InspectFormatTable`, `InspectFormatValue`, `InspectTargetAgents`, `InspectTargetAnomalies`, `InspectTargetMetrics`, `InspectTargetTasks`, `OperatorActor`, `StatusFormatNames` (strings, a function and a constant for `cmd/liza` and the TUI to share), and no existing exported name changes its signature or value; every named value is pinned against the literal it replaced by the `TestA7*` tests of table 3c.

## 9. Dynamic identities

| Identity | Where | Why it stays dynamic |
|---|---|---|
| CLI subcommand name of a session-holding wait (operation names stay dynamic, ADR-0045) | internal/commands/stop_guard.go:20, internal/commands/stop_guard.go:21 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| default reviewer launch spec: provider identity pair (dynamic, ADR-0045) | cmd/liza/cmd_launch.go:111, cmd/liza/cmd_launch.go:218 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| entry-point name of the default pipeline (pipeline configuration, ADR-0045; masLaunchPreset names the three launch presets in cmd/liza) | cmd/liza/cmd_init.go:727, cmd/liza/cmd_init.go:727, cmd/liza/cmd_init.go:727, cmd/liza/cmd_init.go:727, internal/interactive/init_wizard.go:103, internal/interactive/init_wizard.go:104, internal/interactive/init_wizard.go:105 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| legacy provider catalog schema v1 fallback path (removed with schema v1 support) | internal/commands/providers.go:121, internal/commands/providers.go:123, internal/commands/providers.go:123, internal/commands/providers.go:125, internal/commands/providers.go:127 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| name of a retired deployed skill that setup removes on upgrade (data list, not a vocabulary) | internal/commands/setup.go:34 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| operation label recorded in a classified error (pass-through) | cmd/liza/rbac.go:29, cmd/liza/rbac.go:40, internal/commands/validate.go:85 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| operation label recorded in the lifecycle error or audit entry (operations stay dynamic, ADR-0045) | cmd/liza/cmd_archive_acceptance_receipts.go:39, cmd/liza/cmd_archive_terminal_tasks.go:44, cmd/liza/cmd_recover_integration.go:33, cmd/liza/cmd_replace_task.go:103, internal/commands/rejection_rca.go:30, internal/commands/rejection_rca.go:39, internal/commands/validate.go:61, internal/commands/validate.go:74 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| operation name checked against the caller role permissions (RBAC key; ADR-0045 keeps operations dynamic) | cmd/liza/cmd_config.go:137, cmd/liza/cmd_reaffirm_proof.go:65, cmd/liza/cmd_reconcile_verdict.go:56, cmd/liza/cmd_replace_task.go:71, cmd/liza/cmd_reserve_provider.go:64, cmd/liza/cmd_review.go:99, cmd/liza/cmd_review.go:170, cmd/liza/cmd_review.go:271, cmd/liza/cmd_review.go:462, cmd/liza/cmd_review.go:531, cmd/liza/cmd_task.go:377, cmd/liza/cmd_task_blocked.go:89, cmd/liza/cmd_task_blocked.go:219, cmd/liza/cmd_task_dependencies.go:93, cmd/liza/cmd_task_dependencies.go:181, cmd/liza/cmd_task_dependencies.go:303, cmd/liza/cmd_task_dependencies.go:379, cmd/liza/cmd_task_dependencies.go:454, cmd/liza/cmd_task_records.go:62, cmd/liza/cmd_task_records.go:205, cmd/liza/cmd_task_retire.go:89, cmd/liza/cmd_task_retire.go:146 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| provider or CLI identity from the provider catalog (dynamic, ADR-0045) | cmd/liza/cmd_agent.go:322, cmd/liza/cmd_agent.go:324, cmd/liza/cmd_agent.go:326, cmd/liza/cmd_agent.go:328, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_agent.go:330, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_init.go:497, cmd/liza/cmd_launch.go:111, cmd/liza/cmd_launch.go:218, cmd/liza/launch_exec.go:369, cmd/liza/launch_exec.go:371, internal/commands/init.go:169, internal/commands/init.go:186, internal/commands/init_mas_state.go:243, internal/commands/providers.go:57, internal/commands/providers.go:57, internal/commands/providers.go:120, internal/commands/providers.go:122, internal/commands/providers.go:124, internal/commands/providers.go:126, internal/commands/providers.go:126, internal/commands/setup.go:229, internal/commands/setup.go:391 | Catalog, configuration and operation identities are open sets (ADR-0045); a constant would close them |
| agent IDs, task IDs, plan IDs, worktree paths, commits, process IDs | all files | Runtime identities, not vocabulary |
| role names, role pairs | `cmd/liza`, `commands`, `tui` (read from the pipeline and `models.yaml`) | Roles and role pairs are configured through the pipeline (ADR-0045); only the three role types are a closed set and are adopted (table 8) |
| configured commands (`post_worktree_cmd`, validation commands, provider CLI commands) | `commands/init*.go`, `cmd/liza/cmd_init.go` | User-configured values (G1.1); never constants |
