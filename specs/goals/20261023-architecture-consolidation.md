# Architecture Consolidation Before the Next Major Feature

**Entry point:** `functional-spec` — the run starts at architecture. Product scope, behaviour, and the
constraints the architect must honour are settled here; the target structure is the architect's to
design.

**Evidence inputs:** [code quality assessment](../architecture/code_quality_assessment.md),
[architecture review](../architecture/architecture-review.md),
[architectural issues registry](../architecture/architectural-issues.md). These are dated
(2026-07 to 2026-09); where they disagree with the current tree, the tree wins.

## Problem Statement

Liza grew through increments without architectural consolidation. Between 2026-09-06 and
2026-10-23, production Go grew from 307 to 558 files, `internal/ops` from about 21,900 to 47,200
lines, and functions over 150 lines from 24 to 44. Unrelated concerns are now mixed in large files
and long functions.

This costs tokens when agents read those files, makes a change's impact harder to assess, and — by
long-standing engineering experience, though not evidenced on this codebase — breeds defects.

Agents keep code clean when they extend clean code; they degrade it when there is no sound structure
to follow. A major-feature run is about to start and will follow whatever structure it finds. The
base must therefore be consolidated first: a preventive, behaviour-preserving refactoring, preceded
by test coverage extended where needed.

## Actors

| Actor | Stake |
|---|---|
| Agents of the next feature run | Primary beneficiaries. They imitate the structure they find; they need small cohesive units, a visible dependency rule, and a documented answer to "where does new code go" |
| The maintainer | Reviews changes and assesses their impact; needs concerns separated so a change's blast radius is bounded. Opens the area-boundary pull requests and merges urgent fixes into the integration branch |
| Projects already running Liza | Must observe no behaviour change other than the closed list below |
| This run's agents | Must refactor without corrupting the oracle that proves behaviour is preserved |

## Solution Overview

A behaviour-preserving consolidation of Liza's production Go, organised by area. For each area, a
black-box oracle over its observable surfaces is established first; the structure is then reworked
under that oracle; any behaviour change the consolidation forces comes last, as its own task, and
only from a closed list.

The architect designs the target structure, including the partition into areas. It must follow the
dependency rule below, apply design patterns where they absorb real variation or duplication, and
give finite control-flow vocabulary a typed owner. The result is documented so the next run's agents
can find where new code belongs.

## Scope

**In — Must**
- All production Go under `cmd/` and `internal/`. Coverage is complete (see S1); the architect orders
  the work by area, using the evidence inputs.
- Prompt template structure under `internal/prompts/templates/`, with rendered output unchanged
  (see Frozen Surfaces).
- Go tests: extending coverage where a refactor needs an oracle, and restructuring white-box tests
  that follow moved code (see Test Policy).
- The six behaviour changes C1–C6, and no others.
- An import-direction check in `make lint` (K4).
- The documentation deliverables listed under Documentation.

**In — Should**
- `-coverpkg=./...` on `make coverage`, so "where coverage is needed" is measured on a basis that does
  not under-report packages exercised through their callers.

## Explicit Out of Scope (Won't, this run)

- Python skill utilities (separate registry item).
- CI gate expansion beyond K4: ruff, mypy, full race suite, Codecov wiring.
- `plugin/acp`.
- Content of contracts, skills, and support docs.
- Behaviour changes not on the C1–C6 list, specifically: validation on the blackboard write path;
  worktree-intelligence refresh policy; `--json` error-code changes; custom-role prompt behaviour;
  fan-out priority; sprint-metric preservation; Git hardening of `liza launch --cwd`
  (`cmd/liza/cmd_launch.go`). Each can be consolidated while preserved; their registry entries stay
  open.
- A post-run quality reassessment.

## Frozen Surfaces

Behaviour is preserved by default. "Behaviour" means the observable surfaces below; everything else
is free to change.

| Surface | Rule |
|---|---|
| `state.yaml` schema and semantics | Frozen, except as changed by C1 and C5 |
| CLI commands, flags, exit codes, `--json` shape and codes | Frozen |
| Error message text | May change only if the `--json` code it produces is unchanged. `internal/jsonout/classify.go` classifies untyped errors by substring, so classified messages are effectively frozen. This row governs error messages wherever they are printed, including human-readable output |
| Human-readable CLI and TUI output | Content frozen: values, labels, and row and column order. Layout free: whitespace, alignment, widths |
| Rendered agent prompts | Byte-identical over a fixed fixture set |
| Embedded and installed artifacts (settings, hooks, contracts, pipeline) | Byte-identical |
| Persisted timestamps | Unchanged, except the two merge timestamps in C5. Other local-time timestamps stay local |
| Log lines | Free |
| Internal Go APIs, package layout, file layout | Free |

A behaviour change not on the C1–C6 list is a defect of this run.

## Behaviour Changes (closed list)

Each item exists because consolidating its code forces a behaviour choice. Each is its own task,
executed after its area's structural refactor.

| # | Change | Acceptance criterion |
|---|---|---|
| C1 | Every agent and review lease honours the configured `lease_duration` instead of a hard-coded 1800 s / 30 min: agent registration and heartbeat (today `internal/agent/supervisor.go:134,175`), reviewer claims (`internal/agent/strategy_reviewer.go:393`), the re-review reclaim (`reclaimReviewLeaseDuration`, `internal/ops/await_resubmission.go:45`), and agent-pool repair (`internal/commands/repair_agent_pool.go:875`). The terminal value `"terminal-1"` becomes a named constant, unchanged | With `lease_duration: 600`, a newly registered agent's lease expires 600 s after registration, each heartbeat extends it by 600 s, reviewer claims and re-review reclaims take a 600 s lease, and agent-pool repair uses 600 s. With the default config, all stay 1800 s. The stored terminal value is still `"terminal-1"` |
| C2 | Provider diversity, stop signals, and anomaly grouping compare canonical provider keys, derived when the policy is evaluated. The canonical key is the effective one: the catalog's `provider_key`, overridden by a `provider_key` declared in `agent_tools`, falling back to the tool name for a tool the catalog does not know (as the launch resolver computes it today). The stored `agents[].provider` keeps the tool name; launch and every lookup keyed by tool name (e.g. `Config.AgentTools[...]` validation policy) keep using it | Two approvals from agents launched through `codex` and `codex-acp` do not satisfy diversity, nor do two approvals from custom tools declaring the same `provider_key`. Stored provider values, tool-name lookups, launch command lines, TUI and `--json` output are unchanged. Existing state evaluates under the same rule |
| C3 | Stop-signal alias handling follows C2 | A provider-unavailable signal raised for `codex` stops a `codex-acp` supervisor and vice versa. Quota-signal behaviour is unchanged |
| C4 | Initialization has one implementation. The CLI path (`internal/commands/init.go`) is the reference; `ops.InitProject` (`internal/ops/init_project.go`, no production caller) is deleted with its tests. Init Git subprocesses gain `gitenv`'s timeout and target the project root explicitly | `ops.InitProject` no longer exists. Every Git subprocess during init carries `gitenv` environment and a timeout. Branch creation targets the project root regardless of the current directory. Init output and resulting state are otherwise unchanged per the oracle |
| C5 | The two persisted merge timestamps become UTC (today local time at `internal/ops/wt_merge.go:1284,1306`) | Both timestamps persist in UTC. Existing state with local-offset timestamps still loads |
| C6 | Blackboard mutation logic lives in named `ops` operations. Today it also lives in `internal/agent/registration.go`, `internal/agent/checkpoint_summary.go`, `internal/commands/init.go`, `internal/commands/migrate.go`, `internal/tui/commands.go:327`, and in the `ops.ModifyWithAgentAuthority` closures of eight `internal/agent` files. No validation is added | No production call to any mutating `Blackboard` method (e.g. `Modify`, `Write`, `UpdateTask`, `UpdateAgent`) exists outside `ops` and `db`, and no mutation closure is passed into `ops` from outside it; enforced by a check in `make lint`. State transitions are unchanged per the oracle |

Line references are as of 2026-10-23 and will move; the behaviour they locate is what binds.

## Structural Constraints

The architect designs the target structure within these bounds.

| # | Constraint | Priority | Why it binds |
|---|---|---|---|
| K1 | Project guardrails G1.1–G1.4, every `INVARIANTS.md` tier, and the lock discipline (no Git or `exec` inside a blackboard `Modify`) | Must | Inherited project invariants |
| K2 | Accepted ADRs bind. Contradicting one requires a superseding ADR, approved by the human at a checkpoint | Must | The ADR record is the project's decision memory |
| K3 | Dependency rule, using the layer model of [architecture-review §1.3](../architecture/architecture-review.md#13-dependency-map): domain (`models`, `pipeline`, `roles`, `taskkind`) imports no application, runtime, or presentation package; `ops` is the application layer; presentation (`cmd/liza`, `commands`, `tui`, `jsonout`) and the agent runtime (`agent`) sit above `ops`; `prompts` receives resolved data and does not import `ops`. `cmd/liza` is the composition root and may import any layer; any other presentation import of `agent` needs an allowlist entry. `ops` may call infrastructure (`db`, `filelock`, `git`, `gitenv`, repository-intelligence adapters) directly; inverting infrastructure behind interfaces is not required. Shared leaf utilities (`paths`, `identity`, `errors`, `render`, `log`, `brand`) may be imported from any layer. The crossing `prompts→ops` is removed. The documented crossings `commands→agent`, `jsonout→ops`, and `agent→db` are removed or allowlisted with a stated rationale | Must | The direction the human chose for the consolidation, at a scale compatible with the feature run that follows |
| K4 | An import-direction check in `make lint` enforces K3 against its allowlist | Must | Gives the next run's agents a signal when they cross a boundary |
| K5 | No new third-party dependency for the architecture itself (no DI framework); standard Go idioms | Should | Keeps the dependency surface restrained |
| K6 | Each design pattern introduced names the concrete variation or duplication it absorbs. No single speculative implementation | Must | Patterns are applied when relevant, not by quota |
| K7 | Finite control-flow vocabulary (claim types, categories, cardinalities) becomes typed constants in its owning package. Other magic values (timeouts, thresholds, retry counts, literals) become named constants in the package that owns them. No configuration field is added this run. User-configurable identities, including YAML-defined role names (ADR-0045), stay dynamic | Must | Removes magic values without closing configurable sets or extending the configuration surface |

## Test Policy and Sequencing

758 of 781 Go test files (2026-10-23) are white-box, so moving code breaks tests by shape, not by
meaning. The policy separates the oracle from the tests that move with the code.

- **Oracle:** before an area is refactored, black-box tests over its frozen surfaces exist and pass:
  `--json` goldens, `state.yaml` effects, prompt and artifact byte-identity, human-output content.
- **Oracle immutability:** during refactor tasks the oracle may be added to, never edited, except for:
  - a determinism-only edit (ordering, timestamps, temp paths) that changes no asserted value;
  - a call-site-only edit that follows a moved internal API and changes no fixture and no asserted
    value;
  - a golden update in the task absorbing an urgent fix (see Concurrent Changes).

  The reviewer verifies each exception.
- **White-box tests** may be moved, rewritten, or deleted with their code, provided what they
  asserted remains asserted somewhere. The reviewer checks this.
- **Behaviour-change tests:** tests and goldens asserting old behaviour change only inside the
  corresponding C1–C6 task.
- **Sequencing (Must):** the S1 size guard lands first (see S1). Then, for each area, the oracle task
  merges before any refactor task in that area; C1–C6 tasks follow their area's structural refactor.
  The architect defines the areas and how a move that crosses areas is sequenced.
- **"Coverage where needed"** means: wherever a refactor task touches a frozen surface that has no
  oracle yet.

## Concurrent Changes

No feature work lands on `main` until this run closes. Urgent fixes may land on `main`; the
maintainer merges them into the integration branch, and the run's next task in the affected area
absorbs them. That task's reviewer verifies the fix survived the move. If the fix changes a value an
oracle golden asserts, that task updates the golden to the fixed behaviour, and the reviewer verifies
the golden diff matches the fix commit and no unrelated assertion changed. The fixed behaviour then
becomes the baseline.

Before the run starts, in-flight refactoring on `main` either lands or stops.

## Non-Functional Constraints

| # | Constraint | Shape | Priority |
|---|---|---|---|
| N1 | Stoppable at area boundaries: every merged task leaves the integration branch green and consistent. Temporary shims (type aliases, forwarders) are allowed only if listed and removed before their area closes | Bound | Must |
| N2 | Linux, Windows, and macOS CI stay green, checked at each area boundary (see Proof Environment) | Bound | Must |
| N3 | No slowdown of CLI commands or the supervisor loop, observed through the existing benchmarks before and after | Bound, indicative only | Should |
| N4 | `make test` wall time stays within +25% of its time at run start | Bound | Should |

No duration or budget ceiling applies: N1 makes stopping early safe instead.

## Proof Environment

Agents need no credentials and no external services.

| # | Rule |
|---|---|
| E0 | The run integrates into a dedicated branch, `refactor/architecture-consolidation`, cut from `main` at run start. Agents never push to a remote |
| E1 | Per task, proof runs locally in agent worktrees through existing targets: `make test`, `make lint`, `make test-e2e`, and `go test -race` scoped to touched packages. At each area boundary, the maintainer opens a pull request from the integration branch to `main`; its CI (Linux, Windows) and the macOS job on the subsequent `main` push cover N2 |
| E2 | Real Git only on disposable temporary repositories. No real provider CLIs. Opt-in native tests (`TOOL_RESULT_NATIVE_ENGINE`) are outside proof |
| E3 | The installed binary orchestrating this run is never replaced by agents: no `make install`, update, or self-update. Only the maintainer replaces it |
| E4 | Tests never touch this repository's live `.liza/` state. Environment-sensitive tests run under `env -i` (G1.4; lesson `lessons/agents/agent-env-leaks-into-cli-tests.md`) |
| E5 | Every oracle golden is generated against the run's starting commit, even when the oracle task is written later, with input fixtures committed. A golden changes only inside the C1–C6 task that changes its behaviour, in the task absorbing an urgent fix, or for a determinism-only fix |

The run records its own baselines at start: the starting commit SHA, the size measurements below, and `make test` wall time. The command that regenerates every golden and artifact snapshot from the starting commit is committed with the oracle, so a reviewer can reproduce any golden's provenance.

## Success Criteria

| # | Criterion | Proof |
|---|---|---|
| S1 | No production Go function exceeds 100 lines and no production Go file exceeds 800 lines. Measurement: `go/parser` `FuncDecl` with a body, declaration line to closing brace; physical file lines; `cmd/` and `internal/` excluding `_test.go`, `internal/testhelpers/`, `internal/testguard/`, and `internal/prompts/promptbench/` | A testguard-style test carrying the thresholds. It lands first, with a ratchet list of the offenders measured at run start that may only shrink, kept separate from the S2 exception list. Entries are keyed by file and function; an offender may not be moved or renamed while it is still over the threshold. At close the ratchet list is empty. The test stays in the repository after the run |
| S2 | Exceptions to S1 are data-like code only (tables, registrations, generated code), each listed with rationale in `specs/architecture/overview.md`. Any other exception was approved by the human at a checkpoint | Review of the exception list |
| S3 | Size reductions separate concerns rather than splitting functions mechanically; cohesion is an explicit review criterion for every refactor task | Review |
| S4 | Every oracle-covered surface is unchanged except as C1–C6 and absorbed urgent fixes specify. TUI content is checked by review | Oracle goldens (below); review |
| S5 | C1–C6 each meet their acceptance criterion | Focused unit tests; the C6 check |
| S6 | K3 holds: the import-direction check passes against an allowlist where every entry carries a rationale | The K4 check in `make lint` |
| S7 | N1–N4 hold | Per-merge `make test` on the integration branch; area-boundary CI; benchmarks; two timings |
| S8 | Documentation deliverables are present | Review |

**Oracle proof scope:**
- Prompt goldens: one per role × representative task state (about 13–26), rendered through the real
  prompt builder.
- `--json` and human-output goldens: for every command whose `ops` entry operation a refactor task
  refactors.
- Artifact byte-identity: byte snapshots or hashes, captured from the run's starting commit, of every
  generated or installed artifact whose producing code a task changes. A renderer change must fail
  this check even after `make sync-embedded`. The existing `internal/embedded/consistency_test.go`
  proves only that embedded copies are synchronized with the current renderer, not preservation.
- `state.yaml` effects: before/after goldens in `ops` integration tests for the operations being
  refactored.

## Documentation

| Deliverable | Audience |
|---|---|
| `specs/architecture/overview.md` rewritten for the new structure: layers, dependency rule, where new code goes, S2 exceptions | The next run's agents |
| ADRs for the structural decisions, including any K2 supersessions | Future architects |
| `specs/architecture/architectural-issues.md`: resolved items closed with traceability, per its Update Policy | Lifecycle authority |
| `INVARIANTS.md` enforcement references, `specs/architecture/state-machines.md`, `specs/architecture/blackboard-schema.md`, `specs/architecture/supervision-model.md`: code references updated to the new structure | Agents and reviewers applying G1.2 |
| Code path references updated in `specs/protocols/` and `lessons/` | Agents following them |

Historical ADRs and skill content are not edited.

## Failure Modes

| Failure | Expected handling |
|---|---|
| An oracle test fails after a refactor | The refactor is wrong; the task fails review. The oracle is not edited to pass |
| An oracle test is flaky | Determinism-only edit under the reviewer's check |
| A white-box test is deleted with no remaining assertion of what it covered | Review rejects the task |
| An urgent fix lands on `main` mid-run | Merged into the integration branch by the maintainer; absorbed by the next task in that area, as in Concurrent Changes |
| The run must stop early (e.g. to start the feature run) | Stop at an area boundary; open shims for an unfinished area are removed or the area's tasks reverted so the integration branch stays consistent |
| CI is red at an area boundary | The next area does not start until CI is green; the maintainer opens the fix task |
| A consolidation turns out to require a behaviour change not on C1–C6 | Not made in this run; escalated to the human |

## Risks and Assumptions

- **Risk — architect scale.** The architect designs across about 136,000 lines of production Go with
  no epic stage before it. Mitigation: per-area sequencing and the evidence inputs.
- **Risk — mechanical splitting** meets S1 without separating concerns. Mitigation: K6, S3.
- **Risk — S1 binds future work.** The size test fails future changes that grow a function past 100
  lines. Accepted: it is the structural signal agents lack when they have no clean precedent.
- **Assumption — defects.** Better structure reduces defects. Held from engineering experience, not
  measured here; not a success criterion.
- **Assumption — imitation.** Agents of the next run follow the structure they find. This is the
  premise of the run; its effect is observed only after this run.
- **Assumption — dated evidence.** The evidence inputs predate the current tree; the registry is
  partially stale (for example, it lists 13 direct `Modify` sites in `agent`; two remain, with the
  rest moved into `ops.ModifyWithAgentAuthority` closures). The architect re-verifies against the
  tree.
