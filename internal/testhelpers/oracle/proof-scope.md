# Oracle proof scope

Where oracle proof (`internal/oracle/**`) runs, per suite. Each row names the
constraint or skip and its source. Rationale lives in the source, not here.
Enumerated at coding HEAD with:

```
git grep -nE '^//go:build|t\.Skip\(|os\.Exit\(0\)' -- internal/oracle
```

An `os.Exit(0)` hit in a `TestMain` before `oracle.RunMain` is a package-level runtime guard (the package reports pass with no test run); the A4 hit (`a4/main_test.go`, `a4-helper` subprocess mode) is helper dispatch, not a scope.

Authority: D57, D53 and D52 (`specs/goals/20261023-architecture-consolidation-decisions.md`).
Opt-in sensitivity and regeneration proofs are listed last; they never run by default.

## Linux-only scopes

| Suite | Scope | Mechanism | Process facility | Source |
|---|---|---|---|---|
| A2 | every test file under `a2/artifact` and `a2/cli` | `//go:build linux` | POSIX shell stubs and file modes; `runtime.GOOS` branches in producers | AC3, `specs/arch-plan/20261023-architecture-consolidation/20261023-182300-arm-1-ar-2.md#Constraints` |
| A3 | `a3/control` (all tests, through `linuxOnly` in `normalise_test.go`) | `t.Skip` off Linux | process liveness, POSIX permissions | OC3/TC1, `20261023-183344-arm-1-ar-3.md#Constraints` |
| A3 | `a3/lifecycle` (`env_test.go`) | `t.Skip` off Linux | process and timezone proof | OC3, `20261023-183344-arm-1-ar-3.md#Constraints` |
| A7 | `a7/install` (`main_test.go`, `skipUnlessLinux`) | `t.Skip` off Linux | installed-tree modes, symlinks | OC3, `20261023-181043-arm-1-ar-7.md#Constraints` |
| A7 | `a7/present` (`main_test.go`) | `t.Skip` off Linux | process identity, Git symlinks | OC3, `20261023-181043-arm-1-ar-7.md#Constraints` |
| A9 | `TestOracleA9OrchestratorReap` (`orchestrator_test.go`), with `orchreap_linux_test.go`, `reap_linux_test.go`, `signals_linux_test.go`, `signals_reap_linux_test.go` | `t.Skip` and `//go:build linux` | child subreaper, reaping | D45, files named |
| A9 | I-01 `TestOracleA9RegistrationCollision`, I-02 capacity refusal (`oracleA9CapacityRefused`), I-05 `TestOracleA9AuthorityLost` (`lifecycle_linux_test.go`) | `//go:build linux`; `lifecycle_other_test.go` skips I-02 elsewhere | procfs reads (`procscan/process_status.go`); `/proc/<pid>/cmdline` identity check in `delete agent --force` | D53, `lifecycle_linux_test.go` |
| A4 | `TestOracleRepairSupersededDependencies/success` (`runD35Success`, `checkD35Validation` in `dependencies_d35_linux_test.go`) | `//go:build linux`; `dependencies_d35_other_test.go` skips elsewhere | procfs process scan in validation (`internal/commands/validate.go`, `validateNoZombieAgents`), characterized by `TestValidateWarnsWhenProcessScanUnavailable` | D53, `dependencies_d35_linux_test.go` |

The `refused` subtest of `TestOracleRepairSupersededDependencies` stays portable.

## Windows-skipped scopes

| Suite | Scope | Mechanism | Source |
|---|---|---|---|
| A1 | `a1/cli` package (`TestMain` in `main_test.go`, decided by `windowsExclusion`) | runtime guard: on Windows it prints one line naming D57/D31 and calls `os.Exit(0)` before `oracle.RunMain`; no `//go:build`, because `TestOracleCLIBoundary` rejects build restrictions | D57: D31 git-only PATH isolation unproven on Windows |
| A4 | every driver (`gate` in `main_test.go`) | `t.Skip` on Windows | D3 platform gate: Windows does not honour `TZ` (`20261023-182810-arm-1-ar-4.md#Scope 1: A4 oracle suite`) |
| A5 | `TestOracleA5`, `TestA5Isolation` (`oracle_test.go`, `driver_test.go`) | `t.Skip` on Windows | POSIX fixtures use shell commands and paths |
| A6 | determinism layer (`platformGate` in `determinism_test.go`) | `t.Skip` on Windows | Windows does not honour `TZ`; runner locks differ |
| A8 | prompt oracles (`oracle_test.go`, `mutation_test.go`) | `t.Skip` on Windows | prompt goldens embed POSIX native paths |
| A9 | package (`unixOnly` in `kit_test.go`) | `t.Skip` on Windows | AC4, `20261023-181325-arm-1-ar-9.md#Constraints` |

## Runs on all three platforms

- White-box suites (everything outside `internal/oracle/**`) run on Linux, Windows and macOS.
- A1 `a1/persist` and the `sample` goldens carry no build constraint or platform skip; `a1/persist` branches on `runtime.GOOS` for Windows environment names (`tamper_test.go`). `a1/cli` runs on Linux and macOS only (see Windows-skipped scopes).
- Non-Linux twins (`umask_other_test.go` in `a3/control` and `a7/install`, `orchreap_other_test.go`, `reap_other_test.go`, `signals_other_test.go` in `a9`) exist so each package compiles off Linux; they add no proof.

## Opt-in proofs

| Suite | Proof | Gate |
|---|---|---|
| A1 | per-golden sensitivity (`a1/cli/tamper_test.go`) | `ORACLE_CLI_TAMPER=1` |
| A1 | committed-candidate sensitivity (`a1/persist/tamper_test.go`) | `ORACLE_VERIFY_TAMPER=1` |
| A4, A6 | regeneration proofs (`regen_proof_test.go`, `regeneration_proof_test.go`) | `//go:build oracleproof` |
| A5 | single-byte golden sensitivity (`sensitivity_test.go`) | `ORACLE_A5_TAMPER=1` |
| A8 | starting-tree regeneration experiment (`mutation_test.go`) | `A8_ORACLE_MUTATION_CHECK=1` |

## Platform proof for N2

Off-Linux acceptance of the scopes above is the D52 `workflow_dispatch` run at the
boundary integration SHA: Linux, Windows and macOS all green in one run. The
boundary record names this scope (`specs/arch-plan/20261023-architecture-consolidation/20261023-162147-arm-1.md#Area-Boundary Checkpoint Contract`, G8).
