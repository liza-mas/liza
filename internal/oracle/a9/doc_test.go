// Package a9 records default-brand agent-runtime surfaces against the starting
// commit 96c9ad69aee983ce2b929dd681b6ba43bf920535. Brand literals in this package
// intentionally assert default-brand behavior (G1.3); no production code is used
// as an in-process adapter by the lifecycle kit.
//
// Published inventory (success and principal refusal unless explicitly linked):
//
// I-01 registration: TestOracleA9Registration, TestOracleA9RegistrationCollision.
// I-02 automatic ID/cap: TestOracleA9AutoAssignID.
// I-03 heartbeat: TestOracleA9Heartbeat, TestOracleA9HeartbeatRefused.
// I-04 doer claim: TestOracleA9DoerClaim, TestOracleA9DoerClaimRefused (the
// starting binary idles on a named task its role cannot claim; the refusal is
// the unclaimed task and the absent provider call, ended by liza stop).
// I-05 exit release/unregister: TestOracleA9DoerClaim; authority fence:
// TestOracleA9AuthorityLost (supervisors run under the canonical binary name
// with "agent" as first argument, the identity liza delete agent --force checks).
// I-06 graceful restart: TestOracleA9Restart42, TestOracleA9Restart42Limit. Per
// decision D40 the limit case pins the starting behaviour of two plain exit-42
// calls at threshold 1: claim released, restart count 2, no restart-loop block.
// The BLOCKED branch stays unpinned: release precedes tracking, and no recipe
// reaching it was demonstrated without production or progress-signature changes.
// I-07 spin block: TestOracleA9SpinBlock (the eleventh execution is refused before
// launch, so the provider runs ten times); below threshold: TestOracleA9Restart42.
// I-08 review claim/lease: TestOracleA9ReviewerClaim, TestOracleA9ReviewerClaimRefused.
// I-09 recover reviewer worktree: TestOracleA9ReviewerWorktree; existing worktree:
// TestOracleA9ReviewerClaim.
// I-10 missing reviewer branch/worktree: TestOracleA9ReviewerWorktree.
// I-11 pending merge stall: TestOracleA9PendingMergeStall.
// I-12 orchestrator turn: TestOracleA9OrchestratorTurn.
// I-13 checkpoint trigger and I-14 undecided plan: TestOracleA9OrchestratorPlans.
// I-15 checkpoint summary: TestOracleA9CheckpointSummary.
// I-16 surplus pool retirement and held-claim refusal: TestOracleA9SurplusRetire.
// I-17 provider audit and I-18 clean task cleanup: TestOracleA9AuditAndCleanup.
// I-19 quota signal/WIP commit: TestOracleA9QuotaRaise.
// I-20 unavailable signal: TestOracleA9UnavailableRaise.
// I-21 provider signal match: TestOracleA9SignalMatch.
// I-22 spawn gate (exported adapter): TestOracleA9SpawnGate.
// I-23 launch sequence per catalog tool: TestOracleA9LaunchLines.
// I-24 explain per catalog tool: TestOracleA9ExplainLaunch.
// I-25 missing ACP runner: TestOracleA9LaunchRefused.
// I-26 runtime artifacts: asserted inside I-01 through I-23 snapshots; prompt
// bytes belong to A8, so only names and delivered/saved equality are recorded.
// Rows outside I-01/I-02/I-04-I-07/I-16 are allocated to subsequent additive
// groups, whose tests and fixtures are not part of the lifecycle kit delivery.
// Gap: --interactive launch is unreachable from the plain fixture. A refactor
// touching that surface must first add its coverage.
//
// Process ownership (D43/D45/D48): the supervisor-starting lifecycle and reviewer
// tests (lifecycle_test.go, lifecycle_linux_test.go, review_test.go) are
// top-level, nonparallel tests without parallel subtests, and start their
// supervisor through the kit entry points runSupervisor, startSupervisor and
// startNamedSupervisor. Those make the test process the child subreaper before the
// start and, on success, failure and cancellation, signal, reap and assert the
// absence of every child of the test process, adopted zombies included
// (supervisor_owner_linux_test.go proves each path). The generic run, execute,
// start and launch stay unowned for callers that start no supervisor, such as
// TestOracleA9PendingMergeStall; exit_release_test.go's parallel supervisor cases
// are outside this guarantee (residual D48 concern, recorded for global analysis).
//
// Environment and recipe:
// Every scenario uses an empty HOME, one bin directory as PATH, and ChildEnv
// with TZ=Etc/GMT-2, update/catalog guards and fixed Git identity/dates. The only
// host-tool symlink is git; providers and acpx exist only as scripted wrappers
// to this test binary. No host PATH directory or real provider is reachable.
// Wrapper-only ORACLE_A9_STUB/ORACLE_A9_RECORD select dispatch; records retain
// sorted environment names only, never values. The shell may add PWD.
// The committed recipe pins files, message and expected SHA; materialisation
// refuses a mismatch. Every repository commit with both fixed dates and a
// nonvolatile message, plus all recipe SHAs, is registered using KeepSHA before
// comparison. Runtime IDs differing by even one digit remain volatile.
//
// Normalisation (stdlib only, before the harness normaliser):
// Each golden document has a fresh timestamp and equality-token map, shared by
// its before and after sides. Instants number by first appearance; equality is
// preserved across zones. A positive whole-second delta references the lowest
// earlier token, spelling <Tn+Ks:cz>; otherwise <Tn:cz>. c is s for seconds or f
// for fractional RFC3339 (trimmed digit counts are equivalent); z is Z, O for
// numeric offset, or N for no designator. Equal derived timestamps reuse their
// derivation. Date-only names and YYYYMMDD-HHMMSS stamps become <DATE>/<STAMP>;
// listings sort after tokenisation, retaining duplicates.
// Volatile keys: generation, agent_generation, generation_digest, transition_id,
// expected_transition, request_id, payload_digest, digest, record_id,
// supervisor_run_id, run_id, pid and owner_pid. Unregistered 40/64-hex, UUIDs and
// liveness hash suffixes also map to <Vn>, preserving equality per document.
// Project, home and scenario-root paths then use harness directory placeholders.
// Fixed-recipe SHAs remain literal in state, JSON and human forms.
//
// Goldens use before/after state.yaml, before/launch launch.yaml (per-call
// variants when requested), listing.txt and one state pair per runtime signal,
// alert, liveness or usage file. Runtime filenames replace volatile components
// by a one-based ordinal among sorted files with that pattern. Human goldens
// keep exit code, stdout and stderr, excluding structured log records only.
// Prompt transport is recorded as equality/<PROMPT>, never prompt bytes.
// Generation is only through regen -mode write-new at the starting commit;
// live tests compare and never write. Each wait has a failure deadline.
// I-07 barriers advance beyond each saved prompt's filename second before
// releasing that call, avoiding timing-dependent filename collisions without
// changing the default threshold or normalising away file multiplicity.
// Any observed run-dependent timestamp relation is a blocker for the architecture
// owner; the timestamp rule is never weakened to accept it.
package a9
