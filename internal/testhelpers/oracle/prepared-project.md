# Prepared disposable projects

`RunInProject(t, bin, args, project, base, opts)` executes one invocation in an
existing disposable project. The caller creates and cleans up `project`,
`base.Home` and `base.Tmp`, all absolute directories, and owns their contents.
The runner validates these directories before spawning and never creates,
replaces or clears them. It does not change the test process's working directory.
`Run` continues to allocate a fresh project, home and tmp for every invocation.

Pass an explicit `EnvBase{Home: home, Path: path, Tmp: tmp}` for every step.
An empty Path stays empty. CLI children use `Go: false` (the zero value);
Go-toolchain children may opt into ChildEnv's cache policy with `Go: true`.
`RunOptions.Env` adds explicit fixture pairs, rejecting policy-name overrides;
`RunOptions.Stdin` supplies input. No other host variable is inherited beyond
ChildEnv's documented platform policy. Diagnostics about directories and policy
overrides name the field or variable without printing its value.

Materialise a fresh case per mode, then run preparation steps in order with the
same project and EnvBase. If any preparation has a nonzero `Result.ExitCode`,
fail the case immediately and do not invoke the tested step. After successful
preparation, read the before snapshot, invoke the tested step, and read the after
snapshot from the caller's files. An expected refusal is an ordinary nonzero
exit; stdout and stderr stay separate. Invalid directories, invalid environment
pairs and failure to start are fatal test failures. The existing two-minute
invocation timeout and Result normaliser directory registration apply to both
runners. The runner neither sequences cases nor decides which exits are expected.

Concurrent read-only invocations on independent cases are supported. Concurrent
mutation of one fixture is outside this contract.

`TestPreparedProjectSequence`, `TestPreparedProjectIsolation`,
`TestPreparedProjectEnvironment` and `TestPreparedProjectInvalidDirectories`
exercise the real stdlib-only `testdata/preparedprobe` executable. The sequence
uses a caller-created Git repository, fixture contents and markers in project,
home and tmp, with external reads before and after the tested step.
`TestProviderCorrectionA5Start` uses the shared starting-tree export helper to
compile the API and fixture and require all four proofs to run at the immutable
starting commit, under `oraclestart`.

The refusal proofs guard these paths:

- Failed preparation: exit 7 stops the driver; the tested-invoked marker stays
  absent. The tested operation writes that marker before checking prerequisites,
  so missing preparation cannot hide an accidental final invocation.
- Environment isolation: a nonempty host-only sentinel and the independently
  enumerated policy names detect inheritance. A deliberately empty base.Path
  detects substitution of the host PATH. Fixture booleans check extras and stdin
  without printing values. Override attempts use a real executable that writes
  an execution marker, so a successful spawn cannot silently pass the refusal.
- Directory refusal: empty, existing relative, missing and file-valued inputs
  are tried independently for project, HOME and TMPDIR. The fixture receives an
  absolute execution-marker path, so an invalid cwd cannot hide an invocation.
  Diagnostics must name the rejected field, and existing fixture bytes survive.
- Case isolation: one case has a marker and the other does not; independent
  observers must return opposite booleans. Reusing or clearing the first case
  cannot satisfy both assertions. Existing cli_test.go still proves Run's fresh
  project behavior, policy enforcement and exit-code handling without edits.

Validation uses the reviewed env-isolated harness commands, including race,
oracle regression and sample regeneration. Pre-commit covers every changed Go,
Markdown and JSON file: general whitespace/conflict hooks, check-json, jscpd,
go-fmt, goimports, go-vet, staticcheck, check-testhelpers and go-mod-tidy.

Assertion-strength checks used temporary mutations, each restored immediately:
inherit host environment → environment proof fails; bypass directory guards →
all twelve directory cases fail; append extras after ChildEnv validation → all
policy-override cases fail; reuse the first project → independent-case observer
fails; ignore failed preparation → driver and invocation-marker assertions fail;
suppress fatal spawn failure → start-failure proof fails. Each used
`env -i HOME="$HOME" PATH="$PATH" TMPDIR=/tmp go test -count=1 -run '^TestPreparedProject<name>$' ./internal/testhelpers/oracle`
with the corresponding test name. The restored focused suite must pass afterwards.
