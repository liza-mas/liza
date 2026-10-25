# D49 portability correction: A3 lifecycle evidence

Executed on Linux, Git 2.43.0, under `env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp`. Native Windows and
macOS execution is the next G8 run (D59); no agent host runs either.

## Variation evidence (CI run 40162651273, A2 G8 on `g8/a2-b3356c3`)

- Linux tests and macOS: `case_test.go:149: oracle: golden testdata/golden/state/unblock-task/rebase-on-refusal-conflict.txt (kind state) differs at line 132` (Git-version-dependent transcript in the evidence scalar).
- Windows rest-2, every `TestOracleUnblockTaskVariants/*` and `TestOracleAssessBlockedVariants/*` case: `case_test.go:71: start git: exec: "...\bin\git": executable file not found in %PATH%` (extensionless entry).
- YAML style: with Git output lacking the `Rebasing (1/1)\r` progress, the CLI's encoder writes the scalar as a literal block instead of a double-quoted scalar (probed with gopkg.in/yaml.v3); the normaliser canonicalises both shapes.

## Red before, on the pre-correction behaviour (scaffold: layout `git` on every platform, identity normaliser)

R5 `TestLifecycleEnvironmentPolicy` (environment unchanged by the correction on Linux): PASS before and after.

R1 `TestLifecycleGitLayout` and R4 `TestLifecycleGitTranscript*`, scaffold run:

```
--- FAIL: TestLifecycleGitLayout
    portability_test.go:102: windows: entry must carry an executable PATHEXT extension that is not .cmd/.bat
    portability_test.go:105: windows: entry must be the forwarding shim named git
--- FAIL: TestLifecycleGitTranscriptMasksVersions
    transcript_test.go:99: committed 2.43: masked text differs from the token form
    (same for: extra final line, changed hint wording, no progress line, no progress clipped final, missing final line, longer abbreviation)
--- FAIL: TestLifecycleGitTranscriptAssertsClassificationFacts
    transcript_test.go:142: CONFLICT and could not apply: missing fact not named in the failure
    transcript_test.go:142: README: missing fact not named in the failure
    transcript_test.go:155: an opted-in case without a transcript must fail, not pass unmasked
--- FAIL: TestLifecycleGitTranscriptOptIn
    transcript_test.go:165: an opted-in case must mask the transcript
```

After the correction all of them pass (`go test -count=1 ./internal/oracle/a3/lifecycle/`).

## Negative controls

- R2/R3 `TestLifecycleGitShimDefectsDetected`: a shim built from the shim source with `os.Args[1:]` replaced by `os.Args[1:len(os.Args)-1]` diverges from host Git on probe `argv`; one with `return exit.ExitCode()` replaced by `return 0` diverges on probe `missing`. Both detected; the real shim has no divergence (`TestLifecycleGitShimFidelity`).
- R4: removing the README assertion, masking the whole scalar, or removing the literal-block branch each turned the named R4 tests red; restored afterwards.

## Golden diff (`rebase-on-refusal-conflict.txt`, written with `ORACLE_WRITE=1` for this subtest only)

Two changed lines, 132 and 157; the production-owned text before the Git-produced remainder is byte-identical:

```
@@ -132 +132 @@
-            - "command=git -C .worktrees/task-1 rebase <commit:integration> target_ref=integration target_sha=<commit:integration> exit_code=1 stderr=rebase conflict: Rebasing (1/1)\rAuto-merging README\nCONFLICT ... Could not apply e5d6113... task edit"
+            - "command=git -C .worktrees/task-1 rebase <commit:integration> target_ref=integration target_sha=<commit:integration> exit_code=1 stderr=rebase conflict: <git-rebase-conflict-output>"
@@ -157 +157 @@
-            stdout_stderr_excerpt: "rebase conflict: Rebasing (1/1)\rAuto-merging README\nCONFLICT ... Could not apply e5d6113... task edit"
+            stdout_stderr_excerpt: "rebase conflict: <git-rebase-conflict-output>"
```

The JSON and human goldens of the case are unchanged (they hold no Git transcript).

## Regeneration

`go run ./internal/testhelpers/oracle/cmd/regen -mode check -pkg ./internal/oracle/a3/lifecycle` exits 0; every golden, including `state/unblock-task/rebase-on-refusal-conflict.txt`, reports `H1`.

## Canonical commands (all exit 0)

`go test -count=1 ./internal/oracle/a3/lifecycle/`, `make test`, `make lint` (includes `GOOS=windows go vet ./...` and `GOOS=darwin go vet ./...`), `make test-e2e`, the regen check above.

## Platform statement

Neither `TestOracleUnblockTaskVariants` nor `TestOracleAssessBlockedVariants` gained a skip, build constraint or `TestMain` guard. `proof-scope.md` names both as running on all three platforms.
