# Architecture Consolidation — Decision Record

Companion to the goal document `20261023-architecture-consolidation.md` (not yet drafted).
Human and reviewer record; not an input to `init --spec`.

## Head

- **Idea (hypothesis, human, 2026-10-23):** Liza grew through small increments without architectural
  consolidation; tells are god files/functions and accumulated patches. Run a large refactoring that
  reconsiders the code architecture from a Clean Architecture / clean code perspective, applying
  design patterns (creational, structural, behavioral) where relevant, and removing magic values.
  Directions: `specs/architecture/code_quality_assessment.md`, `architecture-review.md`,
  `architectural-issues.md`.
- **Depth:** `full` (agent-proposed, human-accepted). No override.
- **Entry point:** `functional-spec` (agent-proposed, human-accepted with `p`, 2026-10-23).
- **Phase:** complete (2026-10-23). Final verdict: Ready with notes for `functional-spec` (D22).

## Decisions

### D1 — Design patterns are applied when relevant, not as a quota
- **What settled:** "I said 'when relevant'. I'm just hinting, not prescribing."
- **Who:** Human.
- **Why:** "Agents tend to underuse design patterns and prefer extending existing files and functions
  without refactoring on the go." — this is a stated cause of the current state.
- **Rejected:** Reading the idea as a pattern catalog to implement (agent's misreading, corrected).

### D2 — Test coverage is extended before controlled refactoring
- **What settled:** "Part of the goal would be to extend the test coverage where needed prior to any
  controlled refactoring."
- **Who:** Human.
- **Why:** Behaviour preservation needs an oracle before structure moves.
- **Still open:** which tests may be rewritten during refactoring vs frozen as the oracle; what "where
  needed" means.

### D3 — Refactor before the upcoming large-feature run
- **What settled:** A major Liza run introducing a large feature is imminent; refactoring comes first
  "to start on a cleaner base".
- **Who:** Human.
- **Still open:** whether other feature work is frozen while the refactor runs.

### D4 — Magic-value removal is in, within limits
- **What settled:** Limits accepted (G1.1 stack-agnostic config; ADR-0045 YAML-owned role
  identities), "yet there is reasonable work to do on this front".
- **Who:** Human.

### D5 — Entry point `functional-spec`
- **What settled:** The run starts at architecture; the architect designs the target structure with
  per-decision rationale. The human supplies settled behaviour and binding structural constraints.
- **Who:** Agent-proposed, human-accepted.
- **Why:** Product layer is thin for a refactor; the value is the target architecture, which the
  human wants designed, not dictated.
- **Rejected:** `general-objective` — adds epic sequencing but its product framing is mostly vacuous
  for a refactor. `technical-spec` — makes the human the architect.
- **Cost accepted:** No epic stage to sequence the scope, so phasing must arrive as constraints;
  every in-scope behaviour decision must arrive resolved with acceptance criteria.

### D6 — Behaviour frozen by default; closed list of forced changes
- **What settled:** Option B. Behaviour is preserved except where consolidation forces a choice;
  each such change is listed, ruled by the human with acceptance criteria, and executed as a
  separate task. A behaviour change not on the list is a run defect.
- **Who:** Agent-proposed (U2), human-accepted.
- **Rejected:** A (pure refactor) — bakes known warts into clean structure as deliberate design.
  C (refactor + registry fixes) — mixes intents; the oracle becomes "changed as intended", which
  makes broken tests ambiguous (T0.3).

### D7 — The closed list of forced behaviour changes
Agent-proposed (U3), human-accepted as a set 2026-10-23. Locations verified against the tree that day.

| # | Ruling | Behaviour change |
|---|---|---|
| C1 | Agent registration and reviewer claim honour configured `LeaseDuration` (`supervisor.go:134`, `strategy_reviewer.go:393`); `"terminal-1"` becomes a named constant | Only for non-default lease configs (default is 1800, `models/config.go:88`) |
| C2 | Canonical provider key drives diversity, stop signals, anomaly grouping; tool name drives launch | `codex` + `codex-acp` no longer satisfy diversity together |
| C3 | Stop-signal alias rule follows C2 | Provider-unavailable signal for an alias stops all aliases |
| C4 | One init owner using `gitenv` and an explicit project root (`ops/init_project.go:309–330`, `cmd/liza/cmd_launch.go:333`) | Locale forced to C; hung git errors instead of hanging |
| C5 | Persisted timestamps UTC (`ops/wt_merge.go:1284,1306`) | Two persisted fields move from local to UTC |
| C6 | Remaining direct writes routed through `ops` without added validation | None |

- **Won't (this run):** validation on the write path; worktree-refresh policy changes; `--json` error
  code changes; custom-role template behaviour; fan-out priority; sprint-metric preservation. Each
  can be consolidated while preserved; registry entries stay open.
- **Still open:** acceptance criteria per item (phase 2–3).

### D8 — Frozen observable surfaces
Agent-proposed (U4), human-accepted 2026-10-23.

| Surface | Rule |
|---|---|
| `state.yaml` schema and semantics | Frozen, except D7/C5 |
| CLI commands, flags, exit codes, `--json` shape and codes | Frozen |
| Error message text | Free only if the resulting `--json` code is unchanged (`jsonout/classify.go:190–202` classifies by substring) |
| Human-readable CLI/TUI output | Content frozen; layout/whitespace may change |
| Rendered agent prompts | Byte-identical over a fixed fixture set |
| Embedded/installed artifacts | Byte-identical |
| Log lines | Free |
| Internal Go APIs, packages, file layout | Free |

- **Rejected:** byte-exact human output (blocks TUI renderer consolidation); content-only prompt
  freeze (misses whitespace/ordering drift LLMs react to).

### D9 — Structural constraints for the architect
Agent-proposed (U5), human-accepted 2026-10-23.

| # | Constraint | Priority |
|---|---|---|
| K1 | G1.1–G1.4, all `INVARIANTS.md` tiers, lock discipline (no Git/exec inside `Modify`) | Must |
| K2 | Accepted ADRs bind; contradicting one needs a superseding ADR approved by the human at checkpoint | Must |
| K3 | Clean Architecture dependency rule binds (presentation/adapters → ops → domain); the four documented crossings (`prompts→ops`, `commands→agent`, `jsonout→ops`, `agent→db`) removed or allowlisted with rationale | Must |
| K4 | Import-direction check in `make lint` over the K3 allowlist | Should |
| K5 | No new third-party dependency for architecture (e.g. DI framework); Go idioms | Should |
| K6 | Each introduced pattern names the concrete variation/duplication it absorbs; no single speculative implementation | Must |
| K7 | Finite control-flow vocabulary typed in its owning package; operator-tunable values to config; user-configurable identities stay dynamic (ADR-0045) | Must |

- **K4 rationale:** cheap (precedent `check-testhelpers`); covers F0's blank-page case for the
  next run's agents. Kept at Should given F0 says a clean base suffices.

### D10 — Scope boundary and documentation deliverables
Agent-proposed (U6), human-accepted 2026-10-23.

- **In (Must):** all production Go in `cmd/` and `internal/` (architect prioritises using the
  evidence docs); prompt template structure under D8 byte-identity; Go tests (D2 coverage and
  restructuring that follows moved code).
- **In (Should):** `-coverpkg` on `make coverage` (so "where needed" is measured on a basis that does
  not under-report caller-exercised packages); K4 import check.
- **Out (Won't):** Python skill utilities; CI gate expansion beyond K4 (ruff, mypy, full race,
  Codecov); `plugin/acp`; content of contracts, skills, support docs.
- **Docs (Must):** `specs/architecture/overview.md` rewritten to the new structure — layers,
  dependency rule, where new code goes (audience: next run's agents, per F0); ADRs for structural
  decisions incl. K2 supersessions; `architectural-issues.md` closures per its Update Policy;
  current-doc path references (`specs/protocols` 7, `lessons/` 7, `skills/` 4 as of 2026-10-23).
- **Docs unchanged:** historical ADRs (532 path references).
- **Won't:** post-run quality reassessment (separate exercise).

### D11 — Test policy: separate frozen oracle
Agent-proposed (U7), human-accepted 2026-10-23 (option A).

- **Evidence:** 758 of 781 Go test files are white-box (same package), measured 2026-10-23.
- **Oracle:** before an area is refactored, black-box tests on its D8 frozen surfaces exist and pass
  (`--json` goldens, `state.yaml` effects, prompt/artifact byte-identity, human-output content).
  During refactor tasks the oracle may be added to, never edited.
- **White-box tests:** may move, be rewritten or deleted with their code if what they asserted stays
  asserted somewhere; the reviewer checks this.
- **D7 items:** tests asserting old behaviour change only inside that item's task.
- **Sequencing (Must):** per area, oracle task merges before any refactor task in that area; D7
  behaviour-change tasks follow the area's structural refactor.
- **"Where needed" (D2):** wherever a refactor task touches a frozen surface without an oracle.
- **Rejected:** B (all tests frozen — blocks restructuring as unexported functions disappear);
  C (free rewrite — the T0.3 path).

### D12 — Feature freeze with hotfix lane
Agent-proposed (U8), human-accepted 2026-10-23 (option A).

- No feature work lands on `main` until the run closes. Urgent fixes may land; the run's next task in
  the affected area absorbs them, and its reviewer verifies the fix survived the move.
- **Why:** ~57 commits/week recently; changes landing on moved code are the main way fixes get lost.
  Freeze is cheap because the feature run already waits for this one (D3).
- **Rejected:** B (full freeze — a production bug waits for the run); C (no freeze — repeated
  rebases across moved code, lost fixes).

### D13 — Non-functional constraints
Agent-proposed (U9), human-accepted 2026-10-23.

| # | Constraint | Priority |
|---|---|---|
| N1 | Stoppable at area boundaries: every merged task leaves `main` green and consistent; no half-moved area, no cross-task compatibility shims left open | Must |
| N2 | Platform parity: Linux, Windows, macOS CI green (macOS on `main` pushes, `ci.yml:27–34`) | Must |
| N3 | No runtime slowdown of CLI or supervisor loop; existing 6 benchmarks before/after, indicative only | Should |
| N4 | `make test` wall time within +25% of its time at run start | Should |

- No duration or budget ceiling: N1 makes early stop safe instead (human did not add one).

### D14 — Proof environment
Agent-proposed (U10), human-accepted 2026-10-23.

| # | Rule |
|---|---|
| E1 | Local proof in agent worktrees: `make test`, `make lint`, `make test-e2e`, package-scoped `go test -race`; CI on merge covers N2 |
| E2 | Real Git on disposable temp repos only; no real provider CLIs; opt-in native tests (`TOOL_RESULT_NATIVE_ENGINE`) out of proof |
| E3 | Self-hosting guard: the installed binary orchestrating the run is never replaced by agents (no `make install`, update, self-update); human only |
| E4 | Tests never touch this repo's live `.liza/` state; env-sensitive tests under `env -i` (G1.4; lesson `agent-env-leaks-into-cli-tests.md`) |
| E5 | Oracle goldens generated by the run from current code, input fixtures committed; a golden changes only inside the D7 task that changes its behaviour (D11) |

- Human supplies nothing beyond starting the run; N4 baseline recorded by the run.

### D15 — Size success criteria (strict target)
Agent-proposed (U11), human-accepted 2026-10-23 (option A).

- No production Go function over 100 lines; no production Go file over 800 lines (measurement as in
  Evidence). Exceptions listed in `overview.md` with rationale (e.g. declarative tables, Cobra
  registration).
- Work at baseline: 94 functions, 33 files.
- Cohesion is an explicit review criterion, guarding against mechanical splitting (with K6).
- **Rejected:** B (≤150/≤1,000 — leaves 50 functions of 100–150 lines, many in `ops`); C (direction
  only — unfalsifiable).
- **Other success criteria** follow from D7, D8/D11, D9 (K3/K4) and D10; drafted in phase 3.

### D16 — Acceptance criteria for D7
Agent-proposed (U12), human-accepted 2026-10-23.

| # | Acceptance criterion |
|---|---|
| C1 | With `lease_duration: 600`, a newly registered agent's lease expires 600 s after registration, heartbeat extends by 600 s, reviewer claims lease 600 s; with default config all stay 1800 s; stored terminal value still `"terminal-1"` |
| C2 | Agents launched via `codex` and `codex-acp` record the same canonical provider; two approvals from them do not satisfy provider diversity; launch command lines unchanged |
| C3 | A provider-unavailable signal raised for `codex` stops a `codex-acp` supervisor and vice versa; quota-signal behaviour unchanged |
| C4 | One init implementation; an equivalence test over both current entry points, written before convergence, passes after it; every init Git subprocess carries `gitenv` env and a timeout; branch creation targets the project root regardless of cwd |
| C5 | The two `wt_merge` timestamps persist in UTC; existing state with local-offset timestamps still loads |
| C6 | No production blackboard write outside `ops` and `db` (grep/K4-checked); state transitions unchanged per the D11 oracle |

### D17 — Phase 2 revisions
Agent-proposed (U13), human-accepted as a set 2026-10-23. These amend earlier decisions; where they
conflict, D17 wins.

| # | Attack | Revision |
|---|---|---|
| A1 | D8 froze state semantics except C5, but C1/C2 change stored values; running projects hold raw provider names | D8 exceptions are C1, C2, C5. C2 adds: existing state with raw tool names evaluates under the canonical rule |
| A2 | N1's no-shim rule forces each move into one giant task | Shims (type aliases, forwarders) allowed only if listed and removed before their area closes; stop points remain area boundaries |
| A3 | D15 exceptions are a loophole | Exceptions only for data-like code (tables, registrations, generated code); any other exception needs human approval at checkpoint |
| A4 | D11 "never edit the oracle" blocks fixing a flaky golden | Determinism-only oracle edits allowed if no asserted value changes; reviewer verifies |
| A5 | D8 "content frozen, layout free" has two readings | Content = values, labels, row and column order. Layout = whitespace, alignment, widths only |
| A6 | Baseline drifts under the run (supervisor refactors landing during this session) | In-flight work lands or stops before run start; the run records size and test-time baselines at its own start |

- **Attacked and held:** D6, D9, D10, D12, D14.
- **Risk noted, no revision:** D5 — one architect pass over ~136k LOC; per-area sequencing (D11) and
  the evidence docs give the architect a handle.

### D18 — Proof cost per acceptance criterion
Agent-proposed (U14), human-accepted 2026-10-23, with A for D15.

| Criterion | Proof |
|---|---|
| Prompt byte-identity | Goldens per role × representative task state (~13–26), rendered through the real builder |
| `--json` and human output | Goldens only for commands whose implementation a refactor task touches |
| Artifact byte-identity | Existing `embedded/consistency_test.go` + embedded tests; add only where a renderer moves |
| `state.yaml` effects | Before/after state goldens in `ops` integration tests for refactored operations |
| C1, C2 (+ A1 legacy fixture), C3, C5 | Focused unit tests |
| C4 | Equivalence test across both init entry points on temp repos, written before convergence |
| C6, K4 | One testguard-style check in `make lint` |
| D15 | Testguard test with thresholds and the A3 exception list; fails in this run and on future growth (doubles as a regrowth guard, knowingly accepted) |
| N3, N4 | Existing benchmarks; two timings |

- **Cost stated to human:** D15-as-test adds friction for the next feature run's agents when they
  grow a function past 100 lines; accepted as the signal F0 says they lack.
- **Rejected:** D15 as a one-off script at close.

### D19 — Cold review round 1 dispositions
Independent reviewer (fresh subagent, no session context) reported R1–R15 + notes, 2026-10-23.
Agent verified the load-bearing claims against the tree (no production caller of `ops.InitProject`;
integration branch `gh-issues` in `.liza/state.yaml`; CI triggers on `main`/PR/dispatch only;
`tui/commands.go:327` `bb.Modify`; 8 `agent` files with `ModifyWithAgentAuthority`;
`reclaimReviewLeaseDuration = 30m`; validation policy keyed by tool name). No finding contested.
Fixes agent-proposed, human-accepted as a set 2026-10-23 and applied to the goal document:

| # | Disposition |
|---|---|
| R1 | K3 uses the architecture-review §1.3 layer model; infrastructure callable from `ops`; no infrastructure inversion required |
| R2 | Oracle: call-site-only edits allowed when no fixture or asserted value changes |
| R3 | K7: new config fields only optional with default = today's literal; existing fields and embedded pipeline unchanged — **reopened by the Codex review, see U16** |
| R4 | C2: stored `agents[].provider` keeps the tool name; canonical key derived at policy evaluation; C2 removed from state exceptions |
| R5 | C4: CLI init path is the reference; `ops.InitProject` deleted; equivalence test dropped; launch `--cwd` Git site out of scope (Won't). D7's "locale forced to C" was wrong: the CLI path already uses `gitenv`; the change is timeout + explicit root |
| R6 | "Touched" = command whose `ops` entry operation the task refactors; S4 narrowed to oracle-covered surfaces; TUI by review |
| R7 | Dedicated integration branch `refactor/architecture-consolidation` (agent-proposed name, accepted with the set); N1 applies to it; local E1 per task; cross-OS CI via maintainer-opened PR at area boundaries; maintainer merges urgent fixes; agents never push |
| R8 | C6: mutation logic lives in named `ops` operations; adds `tui/commands.go:327` and the `ModifyWithAgentAuthority` closures. `heartbeat.go` → `db.WriteLivenessRecord` writes a separate liveness file, not the blackboard (agent-verified), so it stays out |
| R9 | S1 guard lands first with a shrinking ratchet list, separate from S2; empty at close |
| R10 | Urgent-fix golden update allowed in the absorbing task, matched to the fix diff |
| R11 | All goldens generated against the run's starting commit |
| R12 | C1 covers `reclaimReviewLeaseDuration` and `repair_agent_pool.go` default too |
| R13 | Docs add `INVARIANTS.md`, `state-machines.md`, `blackboard-schema.md`, `supervision-model.md`; `skills/` path updates dropped |
| R14 | Architect defines areas and cross-area sequencing; "prioritises" → "orders"; S1 coverage complete |
| R15 | Stale "Still open" item removed |
| Notes | S1 excludes `testguard`, `promptbench`; "F2" label removed; other local timestamps stay; error-text row wins over human-output row |

### Second reviewer (Codex, relayed by the human, 2026-10-23)
Reviewed the pre-round-1 document. Peer input, not human instruction.
- **X1 (K7 config):** converges with R3 but asks for either a narrowing to existing fields + constants,
  or explicit acceptance criteria for new settings — **open, U16**.
- **X2 (hotfix vs oracle):** converges with R10; its "fixed behaviour becomes the baseline" wording
  adopted.
- **X3 (artifact proof):** `internal/embedded/consistency_test.go` compares the current renderer to the
  current embedded copies (agent-verified), so it proves synchronization, not preservation. D18's
  "near zero cost" artifact proof was agent error — **open, U17**.

### D20 — Rulings on the Codex findings
Human, 2026-10-23: "Narrow K7, U17".
- **K7 narrowed (supersedes R3):** magic values become named constants in their owning package; no
  configuration field is added this run. Rationale: closed list honoured; new fields would reopen
  support-docs scope; no demonstrated tuning need. Cost accepted: e.g. watch thresholds stay code
  constants.
- **Artifact proof (amends D18):** run-start byte snapshots or hashes of every generated/installed
  artifact whose producing code a task changes; must fail on a renderer change even after
  `make sync-embedded`. Cost stated: low to moderate (hash manifest + test).

### D21 — Round 2 verdict and note closures
Same reviewer, round 2 (2026-10-23): blockers 7→0; R1–R15, notes and X1–X3 resolved (R6 accepted
with trade-off: presentation-only and agent-runtime refactors rely on the general "coverage where
needed" rule; R8 heartbeat refutation accepted). Verdict issued: **Ready with notes** for
`functional-spec` — superseded by the edits below, re-confirmation pending.
Closures agent-proposed (U18), human-accepted as a set 2026-10-23, applied:

| # | Closure |
|---|---|
| N1 | C2 canonical key = effective key: catalog `provider_key`, overridden by `agent_tools` `provider_key`, falling back to tool name |
| N2 | `prompts→ops` must be removed; the other three crossings may be allowlisted |
| N3 | `cmd/liza` is the composition root; other presentation→`agent` imports need an allowlist entry |
| N4 | Start SHA recorded with the baselines; golden/snapshot regeneration command committed |
| N5 | C6 check covers any mutating `Blackboard` method |
| N6 | Ratchet keyed by file + function; no moving/renaming an offender while over threshold |
| N7 | Failure mode: red CI at an area boundary blocks the next area; maintainer opens the fix task |
| N8 | K4 raised to Must |

### D22 — Round 3 verdict
Same reviewer, round 3 (2026-10-23), on the document after D21: blockers 0→0, N1–N8 resolved.
**Verdict: Ready with notes** for `functional-spec`.
- **A1 refuted by author, accepted by reviewer:** keyless custom tools already get their tool name as
  provider key (`AgentToolRegistry` → `mergeAgentToolConfig`, `launch_resolver.go:117–123, 393–394`).
  C2 unchanged; verdict stands.
- **Remaining notes — accepted with rationale, human ruling "ship as is" (2026-10-23):**
  - A2 (red CI at an area boundary): the maintainer pauses the run; agents cannot observe CI.
  - A3 (E2 vs start-commit regeneration): E2 governs tests; a read-only temp checkout of the start
    SHA is consistent with it.
  - S1 ratchet keying by receiver: implementation detail the guard's author can settle without
    guessing.
- **Phase 4 closed.** Every finding resolved, refuted-and-accepted, or accepted with rationale; no
  escalation open; no unclaimed marker open.

### D23 — Integration branch and sync-clean start (run, 2026-10-23)
Raised by the arm-1 architecture plan (OQ-001, OQ-003, "E0 Integration-Branch Hold"), relayed by the
operator. Human ruling 2026-10-23T16:46Z, applied by the operator:
- **OQ-001:** the run's integration branch is `global-refactoring`, the branch the run was configured
  with; this ruling supersedes the branch name in E0. It already meets E0's intent: dedicated, cut
  from `main` (`97ccf330c`) at run start, never pushed by agents. The rejected alternative
  (reconfigure onto `refactor/architecture-consolidation`) would re-provision in-flight worktrees for
  a name only. E0's text is left unedited: reviewed carriers pin the goal's "Proof Environment"
  section, and editing it (`b6f15def1`, restored by the operator 2026-10-23T17:05Z) made the arm-1
  reference `goal-proof` stale and stalled its second review. The E0 hold is satisfied by this ruling.
- **OQ-003:** the synced `internal/embedded/bash-policy.yaml` is committed on the integration branch so
  `make sync-embedded` leaves the starting tree unchanged. The run's starting commit is the head after
  this commit, recorded by Scope 0.

### D24 — Provenance of goldens changed by a C task (run, 2026-10-23)
Raised by the Scope 0 architecture (arm-1-ar-0, review R1/R3, iteration 2): master SI-3 H1
reproduces every oracle golden from the starting commit plus absorbed fixes, while E5 lets a C1–C6
task change a golden, which no starting-tree regeneration can reproduce. E5 and H1 share the gap.
Human ruling 2026-10-23T17:46Z, relayed by the operator — option (a′), chained provenance with
reproduction at each change point:
- A golden never changed by a C task is reproduced as H1 states (starting commit plus absorbed
  fixes).
- For a golden changed by a C task, the regeneration entry point (1) reproduces, from the starting
  baseline, the blob the golden held before its first C change; (2) for each C change of that
  golden, in order, checks out the commit that made it in a temporary detached worktree (a read-only
  checkout, as D22-A3 allows for the start SHA) and reproduces the blob it committed; (3) requires
  the current blob to equal the last reproduced one. Every golden value is thus reproduced from a
  named commit; repeated C changes form a chain.
- Change points are resolved from Git history of the golden path and the C task's ledger record;
  a record never names its own commit (closes R3).
- Rejected: (a) chained provenance without reproduction at the change point — leaves the changed
  value unreproduced; (b) literal H1 with a starting-tree backport per golden-changing C task —
  adds work to Scopes 16, 18 and 21–24 that the goal does not ask for.
- E5's text is not edited (reviewed carriers pin "Proof Environment"); this entry is the authority.

### D25 — Hand-off context for Oracle A6 and A8 code plans (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T19:17Z. The A6 and A8 architecture Scopes require their same-file
"Surface Inventory" (A6, A8) and "Determinism Rules" (A8) sections but declare no same-file Direct
References, so ADR-0179's narrowed code-planner presentation shows them only as pinned pointers.
The orchestrator found the reviewed-amendment route closed: both plans already have children, and a
contract-only amendment cannot edit Direct References (`internal/ops/contract_amendment.go:125`).
- Ruling: no amendment and no replan. An operator note on `arm-1-ar-6-cp-0` and `arm-1-ar-8-cp-0`
  (rendered as OPERATOR INPUT in their prompts, `internal/prompts/templates/blocks/task_corrective_context.tmpl`)
  names the sections the code plan must cover. Scope, outputs and acceptance are unchanged; the
  code-plan reviewer still checks full inventory coverage.
- Intent relied on: ADR-0179 "Narrow presentation must preserve complete validation and navigation
  to inherited authority"; goal S4 (every oracle-covered surface proven).
- Rejected: contract-only amendments copying each inventory into its CONTRACT (two reviewed rounds,
  duplicated tables); supersession (replan cost for a presentation gap).
- Kind: check-only (context routing). Revert: withdraw the notes; route a contract-only amendment.

### D26 — Oracle A8 cases T02 and T06: reachable marker branches (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T19:27Z. `arm-1-ar-8-cp-0` blocked: the A8 Surface Inventory
(`specs/arch-plan/20261023-architecture-consolidation/20261023-180843-arm-1-ar-8.md`) declares two
"Reaches" branches that the starting commit never renders for the case's role, verified by the
planner's probe at 96c9ad69a: T02 (epic-plan-reviewer) "validation plan from a checkpoint" —
`internal/prompts/templates/blocks/review_instructions.tmpl` renders DOER VALIDATION PLAN only in the
code-reviewer, code-plan-reviewer, us-reviewer and architecture-reviewer branches (lines 78, 167,
317, 366); T06 (us-reviewer) "scope extensions" — that context section is declared only for
code-reviewer in `internal/embedded/pipeline.yaml`. The CONTRACT forbids the unit from substituting a
render path; the plan has children, so a reviewed amendment cannot edit its inventory.
- Ruling: T02's Reaches entry becomes "master decomposition review" (=== MASTER DECOMPOSITION
  REVIEW === renders for epic-plan-reviewer at start); T06's becomes "validation plan from a
  checkpoint" (renders for us-reviewer at start); scope extensions stay covered by T16
  (code-reviewer). All 30 cases, their byte-exact goldens and the marker rule are unchanged.
- Intent relied on: goal "Rendered agent prompts | Byte-identical over a fixed fixture set" and S4;
  markers only prove a fixture reaches its declared branch, and the oracle records starting
  behaviour, which has no such branches.
- Rejected: dropping the marker obligation for the two cases (weakens the reachability check);
  dropping the cases (coverage loss); supersession of arm-1-ar-8 (replan cost).
- Kind: check-only (marker selection). Revert: restore the original entries and accept the two
  cases as unsatisfiable, or replan A8.

### D27 — Duplicate-code check excludes oracle test data (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T22:35Z. `arm-1-ar-3-cp-0-code-1` and `arm-1-ar-3-cp-1-code-0`
blocked on pre-commit's `jscpd` hook (whole-repo scan, threshold 5%): their committed goldens under
`internal/oracle/**/testdata/` raise duplication to 6.17%; the integration head alone measures
3.58%, and with `**/testdata/**` ignored the blocked worktree measures 1.70%.
- Ruling: add `**/testdata/**` to the hook's `--ignore` list (`.pre-commit-config.yaml`). Production
  Go stays fully checked; `*_test.go` was already ignored.
- Intent relied on: the hook's own comment, "counter the tendency of coding agents to duplicate
  code"; goldens are expected outputs, duplicated by design (goal Test Policy, oracle goldens).
- Rejected: raising the threshold (weakens the check on production code); per-task scope exceptions
  editing the same line (conflicting writers); deduplicating goldens (changes the oracle).
- Kind: check-only (scope of the duplicate check). Revert: drop the ignore entry.

### D28 — Agent row order in `status` is unspecified at the start (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T22:38Z. `arm-1-ar-7-cp-0-code-0` blocked: A7 Scope 1 needs a multi-role agent
fixture and the goal freezes human-output row order, but at the starting commit
`buildAgentStatuses` (`internal/commands/status.go:482-514` at 96c9ad69a) ranges over the agent map
and returns the slice unsorted, so the race-run golden differs by agent order.
- Ruling: the order of agent rows (human table and `--json` agents array) is not a frozen property —
  the starting behaviour has none. The A7 oracle compares agent rows order-insensitively; each row's
  content and every other row and column order stay asserted. Production code is not changed to
  sort them (that would be a behaviour change outside C1–C6).
- Intent relied on: goal Frozen Surfaces freeze observable behaviour; a map-iteration order is not
  observable as a stable value.
- Rejected: single-role fixtures (loses multi-role coverage); sorting in production (unlisted
  behaviour change). Kind: check-only. Revert: assert order and drop the multi-role fixture.

### D29 — Runtime state is not part of A7's installed-tree digest manifest (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T22:38Z. `arm-1-ar-7-cp-1` blocked: A7 Scope 2's installed-tree manifest takes a
content SHA-256 of every file under the project root except the AC6 runtime exclusions and `.git`,
which includes the project runtime `state.yaml`; init writes current timestamps and generated goal
IDs into it, so its digest is not reproducible, while the scope already records the normalised state
separately.
- Ruling: the runtime `state.yaml` stays listed in the manifest by path (presence asserted) but its
  content is asserted only by the scope's normalised state golden, not by a raw digest; every other
  installed file keeps its content digest.
- Intent relied on: goal "`state.yaml` schema and semantics" frozen and proven by state goldens
  (Oracle proof scope); artifact byte-identity covers generated and installed artifacts, not runtime
  state.
- Rejected: normalising the whole manifest with the general text normaliser (erases artifact
  digests); dropping the file from the manifest (loses its presence check). Kind: check-only.
  Revert: digest the raw file and accept non-reproducibility.

### D30 — Scope 13 (Refactor A4) keeps planning-order edges between its code plans (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T22:38Z. `arm-1-ar-13` blocked on accepted review finding R1: its sibling code
plans' coding writers should wait on each other without serialising planning, but
`descendant_dependencies` must name an existing provider task (ADR-0193,
`statevalidate.validateProviderOwner`), and the sibling code-plan providers do not exist yet.
- Ruling: R1 is waived for this scope. The architecture keeps its five planning-order edges (sibling
  code plans run in order), preserving the A3 boundary, the A4 shared-writer order, upstream default
  inheritance and the G8 hold. No engine capability is commissioned.
- Intent relied on: master Run Sequencing — refactor coding is strictly sequential and Scope 13's
  coding cannot start before Scope 12's coding merges; at ruling time no refactor coding had started
  (Scopes 10–12 in code planning), so serialised A4 planning is off the critical path.
- Rejected: commissioning a new declaration capability in the engine under refactor (outside this
  run's scope); a revised allocation merging the five writers (replan cost). Overrides the
  reviewer's R1 recommendation for this scope only. Kind: delivery-neutral (ordering).
  Revert: route a reviewed rescope if planning becomes critical.

### D31 — A1 CLI oracle cases run in a disposable Git project with a git-only PATH (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:01Z. `arm-1-ar-1-cp-0-code-0` blocked because the A1 carrier says no Git
repository is needed, and its code plan forbids CLI cases initialising Git and sets an empty CLI
`PATH`. The frozen CLI requires a Git project root: `cmd/liza/main.go` `requireExplicitProjectRoot`
leads to `requireExplicitGitProjectRoot`, and `internal/paths/paths.go` `GetProjectRootFromDir` runs
`git rev-parse`. Neither file has changed since the starting commit `96c9ad69a`. Every CLI case
therefore fails before reaching the surface it tests.
- Ruling: A1 CLI cases follow the merged A3 lifecycle oracle precedent
  (`internal/oracle/a3/lifecycle/conventions_test.go`). Each case gets a disposable fixture project
  initialised as a Git repository by the test. The CLI `PATH` contains only a `git` symlink (still
  provider-free), and the environment pins `GIT_CONFIG_NOSYSTEM` and the `GIT_AUTHOR_*` and
  `GIT_COMMITTER_*` names, emails and dates. Assertions, expected surfaces and normalisation of
  goldens are unchanged. No production or harness change.
- Intent relied on: goal Test Policy. Oracles pin the observable behaviour of the frozen starting
  CLI, which includes its Git-root requirement. The carrier's "no Git repository is needed" wording
  is a factual slip, not a contract choice.
- Rejected: a production or harness change to accept non-Git roots (alters frozen behaviour, outside
  A1 scope); in-process invocation bypassing root resolution (would not test the CLI surface).
  Kind: oracle-contract (test setup). Revert: a reviewed A1 carrier correction.

### D32 — check-yaml skips oracle StateForm goldens (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:08Z. `arm-1-ar-6-cp-0-code-0` blocked on `check-yaml`: the approved Scope 0
harness StateForm (`internal/testhelpers/oracle/kind.go`) writes `# before`/state/`# after`/state as one
text transcript in `.yaml` golden paths, and the A6 carrier mandates that form. The hook parses it as one
document with duplicate keys. The orchestrator already authorised the same exclusion for
`arm-1-ar-3-cp-1-code-0` (2026-10-23T22:23Z), but it lands only when that task merges.
- Ruling (scope exception): any oracle-area coding task whose diff adds StateForm `.yaml` goldens may
  add to `.pre-commit-config.yaml` check-yaml, byte for byte, the two lines
  `# StateForm goldens are before/after text transcripts, not YAML documents.` and
  `exclude: ^internal/oracle/(?:[^/]+/)+testdata/golden/state/.*\.ya?ml$`. These are the lines
  already in `arm-1-ar-3-cp-1-code-0`, so identical additions merge cleanly. A task whose branch
  already carries them after a rebase adds nothing. Fixture and ordinary YAML, every other hook,
  all oracle assertions and the harness form are unchanged. Same family as D27 (D-13).
- Not committed centrally because `check-useless-excludes` rejects an exclude that matches no file,
  and no state golden is on the integration branch yet. The hook is kept, not skipped.
- Rejected: changing the harness StateForm (would rewrite merged goldens and approved contracts);
  making A6 wait on A3's merge (serialises unrelated areas).
  Kind: delivery-neutral (tooling classification). Revert: drop the exclude line.

### D33 — K1 lock discipline is non-regression for inherited final revalidation (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:11Z. `arm-1-ar-14` (A5 refactor architecture) blocked on a source conflict
inside K1. The lock discipline forbids Git or `exec` inside a blackboard `Modify`, while INVARIANTS §5,
also bound by K1, requires final assignment and start to revalidate the worktree/review commit and the
integration revision. The baseline does that revalidation with Git inside the transaction:
`ValidationPreflight.CheckCurrent` (`internal/ops/validation_preflight.go:85`, which calls `GetCommitSHA`
and runs `git rev-parse`) is called inside `Modify`/`ModifyWithAgentAuthority` at `claim_task.go:597`,
`claim_reviewer_task.go:436`, `await_resubmission.go:666`, `resume_handoff.go:94`,
`resume_owned_task.go:133` and `validation_preflight.go:125,344`. These are the starting tree's lines;
the behaviour binds, not the line numbers.
- Ruling: for this behaviour-preserving run, K1's lock discipline is non-regression. No refactor adds a
  Git or `exec` call inside `Modify`. The inherited `CheckCurrent` revalidation stays inside the same
  final transaction, with the same checks, order and refusals. A refactor may move it with its
  operation (for example into a concern file or a named helper) but never out of the transaction,
  and never weakens it. Each area architecture that owns one of these sites lists it as a K1
  inherited exception. Reviewers check that the count does not grow.
- Intent relied on: goal Scope (behaviour-preserving) and the closed C1–C6 change list. Moving the
  revalidation outside the lock changes the concurrency semantics that INVARIANTS §5 protects and
  is not an authorised change.
- Rejected: a prerequisite synchronization design now (a behaviour and concurrency change outside
  C1–C6; needs a human scope decision and its own ADR); dropping the final revalidation (breaks
  INVARIANTS §5). Kind: oracle-neutral (constraint interpretation).
- Debt: Git inside `Modify` at the sites above. Payback trigger: a run whose goal admits a
  synchronization redesign. Revert: the human widens scope, or overrides this ruling.

### D34 — Byte-rewriting pre-commit hooks skip oracle goldens (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:12Z. `arm-1-ar-2-cp-1-code-0` blocked because `end-of-file-fixer` appended a
newline to baseline-produced artifact goldens. The approved comparator requires them byte-exact, and the
task scope forbids editing the shared hook file. This is the fourth repository-hook collision with oracle
goldens (D27 jscpd; check-yaml for A3 and A6, D32).
- Ruling: `.pre-commit-config.yaml` excludes `^internal/oracle/(?:[^/]+/)+testdata/golden/` from the
  hooks that rewrite or reject golden bytes: `end-of-file-fixer`, `trailing-whitespace`, `forbid-crlf`,
  `remove-crlf`. Golden bytes are owned by the oracle comparator and the starting-tree producer.
  Fixtures, drivers and all other files keep every hook. `check-useless-excludes` passes (goldens
  exist on the branch), and the change merges cleanly with the D32 check-yaml lines.
- Rejected: per-task scope exceptions (a fifth collision is predictable); normalising the goldens
  (breaks byte-exactness against the starting tree). Kind: delivery-neutral (tooling classification).
  Revert: drop the four exclude lines.

### D35 — The A4 D4 success fixture carries exactly the defect its operation repairs (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:23Z. `arm-1-ar-4-cp-0` blocked on review finding R1: A4 Scope 1 Fixtures
requires "committed `.liza` inputs valid at the starting commit". Inventory row D4
(`repair-superseded-dependencies`, success) succeeds only on a SUPERSEDED task whose `depends_on` names
a downstream role-pair task. `validate` rejects exactly that edge for every task status
(`internal/statevalidate/validate_deps.go` `validateDependencyDirection`). The success fixture therefore
cannot be valid; the operation exists to repair that defect.
- Ruling: for `TestOracleRepairSupersededDependencies/success` only, the committed fixture is valid at
  the starting commit except for one illegal downstream dependency edge on one SUPERSEDED task, the
  defect the operation removes. The case pins that defect: on the fixture, before the operation,
  `validate --json` at the starting commit reports exactly that one violation, and after the
  operation `validate` reports none. Both results join the case's goldens. Every other A4 fixture,
  including `/refused`, stays fully valid as Scope 1 states.
- Intent relied on: Scope 1 wants fixtures to be real inputs the starting tree accepts, so goldens
  pin genuine behaviour; the starting CLI accepts this state and completes the operation.
- Rejected: descoping D4 success (leaves the frozen surface's only success path unpinned); an
  architecture amendment of arm-1-ar-4 (this ruling is narrower and cheaper). Overrides R1's demand for
  an owner correction for this case only. Kind: oracle-contract (fixture validity). Revert: a reviewed
  A4 correction.

### D36 — A1 usage human output: every rendered heap address is volatile (run, 2026-10-23)
Operator, auto-approved, 2026-10-23T23:38Z. `arm-1-ar-1-cp-0-code-0` blocked again (after D31). The starting CLI's
human `usage` value output prints Go pointer addresses. The reviewed comparator masks pointer positions
only for `PerCompletedTask`, `Provenance` and `SuppressedCalls`. The committed fixture also renders two
`PostTransition.Tasks[].LastUseful` addresses (`*Transition`, `internal/usage/report.go`), so
`TestOracleUsageReport/human/usage-value.txt` fails on a heap address.
- Ruling: in A1 usage human output, the comparator masks the address at every position where the
  starting CLI renders a pointer-typed field of the usage report value. Positions are identified from
  the Go types, not by a hex regex: `PerCompletedTask`, `Provenance`, `SuppressedCalls`, `Baseline`
  and `PostTransition.Tasks[].LastUseful`, plus any other pointer-typed field the same rendering
  reaches. Every other token is preserved byte for byte. The fixture is unchanged.
- Intent relied on: the comparator's existing masks already treat heap addresses as volatile, and
  `LastUseful` is the same class. A memory address is never a behaviour to freeze.
- Rejected: changing the fixture so `LastUseful` is nil (drops coverage of the post-transition rows);
  a hex regex mask (would also hide real identifiers). Note for after the run: printing heap
  addresses in human output is a pre-existing defect. It is frozen here and not fixed (not in C1–C6).
  Kind: oracle-contract (volatility). Revert: a reviewed A1 carrier correction.

### D37 — A7 install human output: box border width is volatile (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T00:20Z. `arm-1-ar-7-cp-1-code-0` blocked: the starting CLI's `setup`/`init`
human output draws boxes (`╭─…─╮`, `╰─…─╯`) whose width follows the longest content line, which
includes the disposable case root. Path normalisation tokenises the root but leaves the border run
length, so `human/setup/default.txt` and the init/activation prerequisite boxes differ between runs.
The task's scope forbids harness edits.
- Ruling: the A7 install suite applies a package-local decoration normalisation after the harness
  path normalisation, in its own test files with a focused test. On a line made only of box-drawing
  corner and edge glyphs, each run of horizontal border glyphs (`─`, `═`, `━`) becomes one fixed
  token. Every other character on every line is preserved byte for byte, including corner glyphs,
  vertical edges, text, labels, row and token order. The harness `humanForm` is unchanged.
  Regenerate the A7 install goldens with write-new from the starting commit under this
  normalisation.
- Intent relied on: border width is a function of a volatile path length, the same class as path
  tokens (E-goal oracle volatility). The text inside the box is the behaviour that binds.
- Rejected: a harness `humanForm` change (shared contract, other areas' goldens); fixed-length
  case roots (not controllable through `t.TempDir`). Other areas meeting the same boxes adopt the
  same local rule, citing D37. Kind: oracle-contract (volatility). Revert: a reviewed harness
  correction.

### D38 — A3 lifecycle PID masking is positional, not by value (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T00:40Z. `arm-1-ar-3-cp-0-code-1` blocked on an unresolved reviewer blocker. The
inherited lifecycle mask (`internal/oracle/a3/lifecycle/mask_test.go`) builds a whole-word regex of the
live PID, replaces it globally, and also masks every JSON number equal to it. When the PID collides with
a fixed value (reviewer run at PID 60), `heartbeat_interval: 60` becomes `<live-pid>` and every
L1–L15 state case fails. The run is nondeterministic, and the mask erases genuine values.
- Ruling: bounded exception for this task to edit `internal/oracle/a3/lifecycle/mask_test.go` and
  `conventions_test.go`. The PID is masked only where the starting CLI writes a PID: PID-bearing
  fields identified by key/path, and human-output positions identified by their label. No masking
  is done by value. Add a focused regression that forces a PID colliding with a fixed value (for
  example 60) and asserts `heartbeat_interval: 60` survives. Production, contracts, existing golden
  bytes and paths, fixtures, stable identifiers and acceptance obligations are unchanged. The
  starting-commit H1 reproduction must still pass.
- Intent relied on: masks hide only volatile values (D36 and D37 are the same principle); masking by
  value corrupts fixed data and makes the oracle flaky.
- Rejected: leaving it (a flaky oracle and a falsified golden comparison); a harness change (the defect
  is package-local). Kind: oracle-contract (volatility). Revert: a reviewed A3 correction.

### D39 — Oracle A8 case T01: mandatory docs reached through a fixture pipeline value (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T00:53Z. `arm-1-ar-8-cp-0-code-0` blocked: inventory row T01 (epic-planner) must
reach "mandatory docs" (`=== MANDATORY DOCUMENTS ===`). At the starting commit `96c9ad69a`, every role in
`internal/embedded/pipeline.yaml` declares `mandatory-docs: []`. `Resolver.MandatoryDocs` returns that
list, and `templates/blocks/mandatory_docs.tmpl` omits the section when it is empty, so no case can reach
the branch under the unmodified frozen pipeline. Projects configure the list, so the branch is real
starting behaviour that a prompt refactor could break.
- Ruling: for T01 only, the A8 adapter freezes the embedded pipeline as mandated and then sets
  epic-planner `mandatory-docs` to a fixed list of documents committed in the T01 fixture. The render
  path (LoadFrozen → NewResolver → NewRoleStrategy → BuildPrompt) and every other case are unchanged.
  The other cases use the unmodified frozen pipeline. Production code is unchanged. The marker rule and
  T01's byte-exact golden from the starting commit stand.
- Intent relied on: goal "Rendered agent prompts | Byte-identical over a fixed fixture set"; a fixture
  may carry configuration, as A4 does with its committed pipeline copy (A4 D4).
- Rejected: dropping the T01 mandatory-docs entry as D26 did (leaves a configurable prompt branch
  unpinned); changing the embedded pipeline (a production behaviour change). Kind: oracle-contract
  (fixture configuration). Revert: a D26-style marker reselection.

### D40 — Oracle A9 case I-06 pins the starting behaviour of two exit-42 restarts (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T02:38Z. `arm-1-ar-9-cp-0-code-0` blocked with AWAITING HUMAN. Case I-06 expects
BLOCKED (restart loop) after two exit-42 calls at threshold 1. At the starting commit `96c9ad69a`,
`supervisor.go` calls `releaseAgentAfterExit` before `exit42Tracker.Handle`, `registration.go` returns an
ordinary coder claim to its initial state, and `supervisor_restart.go` blocks only an executing or
reviewing task still owned by that agent. The plain recipe therefore never reaches BLOCKED; baseline
regeneration timed out on `TestOracleA9Restart42Limit`. Orchestrator assessment: the conflict is
verified; a handoff recipe is unproven.
- Ruling: the oracle pins starting behaviour, as in D26. I-06 keeps its recipe (two plain exit-42
  calls at threshold 1) and records the outcome the starting binary actually produces (claim released
  per `registration.go`, no restart-loop block), with byte-exact goldens from write-new at the
  starting commit. The coder may additionally pin the BLOCKED branch through another recipe only if
  that recipe is demonstrated at the starting commit, needs no production change, and changes no
  progress-signature fields. Otherwise the branch stays unpinned. All other A9 rows are unchanged.
- Intent relied on: goal Test Policy (oracles freeze starting behaviour; E5) and D26. Expected
  outcomes that the starting code cannot produce are architecture predictions, not frozen behaviour.
- Note for after the run (not fixed here, outside C1–C6): the restart-loop protection appears
  unreachable for an ordinary coder exiting 42 (release precedes tracking). This is a candidate
  production defect for the next run.
- Rejected: weakening the observation into a timeout-tolerant check (an oracle that cannot fail);
  dropping I-06 (coverage loss). Kind: oracle-contract (expected outcome). Revert: a reviewed A9
  correction.

### D41 — Stabilise two inherited timing-flaky internal/agent tests (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T02:52Z. Deferred batches 2 (`make test`) and 8 (`make test-race`) went red
with the same signature, a failing `internal/agent`. The bisect named docs-only or test-only merges
(`2f3374a6d`, `be0c4d4ca`). The operator's capture (two `go test -race -count=1 -v ./internal/agent/`
runs at `aaac7a4a1`) passed once and failed once, on
`TestExecuteAgentProgressWatchdogStillBlocksDuringHandback` ("session was cancelled before the agent
saw its submit's outcome") and `TestExecuteAgentBlocksSubmitStillRunningWhenGraceEnds` ("executeAgent
= (1, <nil>), want exit 0"). Both live in `internal/agent/inflight_submit_deadline_test.go`, which has
not changed since the starting commit, and both rely on real-time budgets of 0.2–2 s that a loaded host
misses.
- Ruling: `deferred-validation-2-1-correction-plan` is repurposed to one bounded test-only task. It
  makes these two tests deterministic, through an injected or controlled clock or event
  synchronisation instead of wall-clock budgets, without weakening any assertion and without
  changing production code. Proof: `go test -race -count=20 -run '<the two tests>'
  ./internal/agent/` passes while the host runs the usual agent load, and the full package passes.
  Owned outside A1 (the A1 attribution is withdrawn).
- Rejected: marking the commands known-red or skipping the tests (hides a signal, CORE Rule 14);
  widening the budgets only (still load-dependent). Kind: test-stability (delivery-neutral). Revert:
  drop the task; batches stay intermittently red.

### D42 — Oracle A9 case I-11: pin the stall record's observable surface, not its 10-minute trigger (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T03:15Z. `arm-1-ar-9-cp-0-code-2` blocked: the I-11 success cell (pending-merge stall
record) has one producer, `internal/agent/strategy_reviewer.go` `recordPendingMergeStall`. It is reached
only after 3 quick retries plus 20 wake rounds of 30 s, about 606 s at minimum, through unexported
constants and an unexported strategy. That exceeds Go's default 10-minute package timeout, and a
test-only fixture cannot shorten it.
- Ruling: I-11's success cell pins the stall record's observable surface. A fixture state holds
  exactly the anomaly record the starting producer writes, with fields and values taken from
  `recordPendingMergeStall` at `96c9ad69a`. The case runs the starting CLI's anomaly read surfaces
  (`get anomalies`, human and `--json`) over it, with byte-exact goldens from write-new at the
  starting commit. The trigger path (retries, then stall rounds, then record) stays guarded by the
  existing white-box test `TestReviewerPreWork_PendingMergeStallRecordsAnomaly`
  (`internal/agent/strategy_reviewer_merge_wake_test.go:138`). Refactors must move that test with the
  code and keep it passing; reviewers of A9-area refactors check this. No production seam, no
  longer timeout, and the other A9 rows are unchanged.
- Intent relied on: oracles pin observable behaviour within the canonical validation budget, and
  white-box tests guard internals (goal Test Policy).
- Rejected: a production seam for the round count or interval (a behaviour-adjacent change outside
  oracle scope); a slow build-tagged suite with a longer timeout (10+ min on every batch); dropping
  I-11. Kind: oracle-contract (reachability within budget). Revert: a reviewed A9 correction.

### D43 — A9 oracle: host-process correlation is volatile; spawned supervisors are reaped (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T03:42Z. `arm-1-ar-9-cp-0-code-1` blocked at admission. The merged A9 kit
(`arm-1-ar-9-cp-0-code-0`) goldens `golden/human/i01-collision/agent.txt` and `i02-refused/agent.txt`
pin the starting binary's collision/capacity error. That error ends `correlation unavailable` when no
other agent supervisor is visible on the host, and `observer-visible matching pids [...]` when one is,
so the goldens depend on host processes. At the time, three orphan `liza agent coder` supervisors from
A9 spawn-gate scenarios (`TestOracleA9SpawnGatei22-*`, parent systemd, project directories deleted) were
still alive. The operator terminated them (SIGTERM, verified gone).
- Ruling 1 (determinism-only exception for `arm-1-ar-9-cp-0-code-1`): `newA9Normaliser` replaces that
  correlation suffix, whichever variant appears (`correlation unavailable` or `observer-visible
  matching pids [...]`), with one fixed token. The two kit goldens are regenerated from the starting
  commit with write-new. Every other byte, kit API and golden is unchanged.
- Ruling 2 (test hygiene for every A9 scenario that starts a supervisor, including
  `arm-1-ar-9-cp-0-code-4` spawn-gate rows): each test stops and reaps every process it started (whole
  process group) in `t.Cleanup`, and asserts that none outlives the test.
- Intent relied on: an oracle must be deterministic under the run's normal concurrency. Leaked
  supervisors waste resources and can launch real provider sessions.
- Rejected: requiring a quiet host (agents always run concurrently); pinning one variant (flaky).
  Kind: oracle-contract (volatility) and test hygiene. Revert: a reviewed A9 correction.

### D44 — Oracle A9 cases I-13, I-14, I-15: reachable cells, adapter for the pure guard, fixture provenance (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T03:53Z. `arm-1-ar-9-cp-0-code-3` blocked after review iteration 2 (4 blockers, two
routed to the source owner).
- I-13 refusal "a different checkpoint: trigger kept": the callback guard (`strategy_orchestrator.go:144`)
  differs from the gate (`:122`) only when a checkpoint is replaced between PreWork's snapshot and its
  `Modify`, which no external actor can interleave, and `orchestratorStrategy` is unexported.
  **Ruling:** keep the binary-reachable `:122` gate cell. Pin the guard's decision through an AC2 adapter
  over the exported pure predicate `ops.SameResumedTransitionCheckpoint` (`internal/ops/mode_change.go:622`),
  using fixture sprint pairs: same checkpoint gives true; a different checkpoint or sprint gives false,
  so the trigger is kept. Results are byte-exact from the starting commit.
- I-15 success: the inventory expects "pending summary and alert", but the starting code claims the
  obligation before the CLI runs and alerts only when the CLI fails. **Ruling:** pin the observed
  starting behaviour (pending summary, no alert), as in D40. Pin the refusal "summary already pending"
  through the binary-reachable in-flight guard (`checkpoint_summary.go:141`), using the coder's recipe:
  hold the first summary stub on a wait file, then `resume` and `sprint-checkpoint` to raise a second
  obligation.
- I-14 fixtures `i14-undecided` and `i14-decided`: hand-authored MERGED plan states are acceptable
  committed inputs (A4 D4 precedent) when each carries a derivation note naming the starting producer
  operation and the source of each non-trivial field, and starting-commit `validate --json` accepts it.
  A binary derivation is preferred where one exists within budget, but not required.
- Blocker 4 (the `i15-consumed` mid-run snapshot) is mechanical and needs no ruling.
- Kind: oracle-contract (reachability and provenance). Revert: a reviewed A9 correction.

### D45 — D43 reaping: a subreaper defines the bar (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T04:27Z. `arm-1-ar-9-cp-0-code-1` and `arm-1-ar-9-cp-0-code-4` both reached review cycle
3/5 on the single D43 ruling 2 blocker. The reviewer found successive gaps in `ps`-based process tracking:
the provider group was missed, the deadline path killed only part of the tree, and a descendant was
reparented before any snapshot once its leader had exited. Production `internal/process/spawn.go` starts
providers in a detached group, so they leave the supervisor's group.
- Ruling: on Linux, each A9 test process that starts supervisors calls
  `unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)` (`golang.org/x/sys`, already a dependency)
  before its first start, so every orphaned descendant reparents to the test process. Cleanup
  (`t.Cleanup`, also on deadline and failure paths) signals every remaining child and its process
  group, reaps them, and asserts that the test process has no remaining children. That assertion is
  complete regardless of leader exit timing and replaces `ps`-scan tracking. On non-Linux hosts,
  best-effort group termination is acceptable and must be documented in the test. Reviewers judge D43
  ruling 2 by this bar.
- Placement: each task may implement it in its own A9 test files now (no kit edit), and must keep the
  helper small and identical in shape. Debt: consolidate into the kit (`internal/oracle/a9`) during
  the A9 refactor scope. Payback trigger: the first A9-area refactor task.
- Rejected: continuing `ps`-scan refinements (racy by construction; risks the review cycle cap);
  editing the merged kit from two concurrent tasks (writer conflict). Kind: test hygiene. Revert:
  a reviewed A9 correction.

### D46 — Remove the time.Sleep calls merged by A9 oracle rows; the sleep budget stays (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T05:04Z. `deferred-validation-2-1-correction-plan-code-0` (the D41 fix; its 20x race
proof passed) blocked at admission because `make test` fails `internal/testguard` `TestSleepUsageBudget`
(`time.Sleep()` budget 16, got 17). Merged A9 rows added three sleeps: `internal/oracle/a9/launch_test.go:509`
and `:742` (`arm-1-ar-9-cp-0-code-4`), and `internal/oracle/a9/review_test.go:212` (`arm-1-ar-9-cp-0-code-2`).
They merged because `make test` was recorded known_red (D-16) while the flake fix was pending, so the
guard did not run on their admission.
- Ruling: the budget is a ratchet and stays at 16. The D41 task receives a bounded scope exception for
  those two files. It replaces the three sleeps with synchronisation that uses no `time.Sleep`: wait on
  the child's exit (`cmd.Wait` in a goroutine with a deadline select) for the process loops, and await an
  observable event (the claim attempt's state or log record) instead of the 1 s dwell before the absence
  assertion. Assertions, scenarios, goldens and every other file are unchanged. A ticker or `time.After`
  loop that merely renames the sleep does not satisfy the ruling.
- Rejected: raising `maxSleepCallsInTests` (defeats the ratchet); a separate task (the D41 task is
  idle on exactly this and no other writer touches these files). Kind: test hygiene. Revert: drop
  the exception.

### D47 — D46 extension: launchReap waits through the D45 subreaper in platform-tagged helpers (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T05:14Z. The D41/D46 task replaced two of the three A9 sleeps (`ba28e3010`). The third,
`launchReap` in `internal/oracle/a9/launch_test.go`, waits for scenario processes that are not children of the
test (its own probe builds a reparented orphan), so `cmd.Wait` returns ECHILD and the stdlib offers no portable
non-child exit event.
- Ruling: under D45, the test process is a child subreaper on Linux, so orphaned descendants reparent to it
  and become waitable. The scope exception extends to two new files,
  `internal/oracle/a9/reap_linux_test.go` (build tag `linux`) and `internal/oracle/a9/reap_other_test.go`
  (`!linux`), plus the existing call site in `launch_test.go`. On Linux, the helper sets
  `PR_SET_CHILD_SUBREAPER` (idempotent) and reaps with `syscall.Wait4` in a goroutine, using a deadline
  select and no `time.Sleep`. On other platforms, D45's best-effort group kill applies without a wait
  loop, documented in the file. `launchReap` keeps its assertions. The sleep budget stays at 16.
- Rejected: weakening the orphan check; a ticker or ps poll (D46). Kind: test hygiene. Revert: drop the
  exception.

### D48 — D45 completed: ownership by test isolation, not by process tracking (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T05:23Z. `arm-1-ar-9-cp-0-code-3` reached the high-churn RCA gate (4 rejections; the last
three on D45 cleanup). Implementations identify "their" descendants by observation (`ps` or `/proc` scans,
`HOME` in `/proc/<pid>/environ`, periodic trackers). A process that exits or is reparented before it is
observed escapes, because zombies have no environment. They filter at all because the subreaper is shared by
the whole test binary, and A9 has `t.Parallel` tests.
- Ruling (the definitive D43/D45 bar):
  - Isolation: every A9 test that starts supervisors is a top-level test that does not call
    `t.Parallel` and runs no parallel subtests. Go never runs such a test concurrently with parallel
    tests, so at its cleanup every child of the test process belongs to it.
  - Linux cleanup (in `t.Cleanup` and on deadline and failure paths): with `PR_SET_CHILD_SUBREAPER` set,
    enumerate `/proc/*/stat` entries whose ppid equals `os.Getpid()`, zombies included, with no
    environment or ownership filter. Signal each child's process group, then `syscall.Wait4` each with
    a deadline select (no `time.Sleep`). Repeat the enumeration until no child remains, then assert
    that none remains.
  - Elsewhere: D45 best effort.
  - One shared helper shape: tasks reuse D47's `reap_linux_test.go` and `reap_other_test.go` when they
    are present on their base, otherwise add them identically.
- Rejected: further observation-based tracking (racy by construction). Kind: test hygiene. Revert: a
  reviewed A9 correction.

### D49 — D33 generalised: every Git or exec inside Modify inherited from the starting tree is a K1 exception (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T07:27Z. `arm-1-ar-12-amend-3` (AWAITING HUMAN) and `arm-1-ar-12-cp-3` blocked on A12 Scope 4:
`internal/ops/recover_task.go` `recoverTaskFreshReset` (starting-tree lines 543–627) runs integration preflight
(`GetCommitSHA`), destructive cleanup and creation inside one `Modify`. Cleanup failure leaves the state
unchanged, creation failure publishes BLOCKED (ADR-0083), and success releases the snapshot's agents. Scope 3
raised the same class earlier (`state_lock_holds.go` automatic hold gate and refusal publication), settled by
`arm-1-ar-12-amend-2`. D33 named only the `CheckCurrent` sites, so each area re-raises the class.
- Ruling: D33's non-regression rule covers every Git or `exec` call made inside a blackboard `Modify`
  (including `ModifyWithAgentAuthority`) that exists at the starting commit `96c9ad69a`, not only
  `CheckCurrent`. Each such call stays in the same transaction, with the same order, checks, failure
  outcomes and publications (including ADR-0083 BLOCKED publication after cleanup or creation failure,
  and agent release on success). A refactor may move it with its operation, never out of the
  transaction, and never weakens it. Owning area architectures and plans list each site as a K1
  inherited exception, and reviewers check that the list does not grow. Code plans and amendments
  cite D49 rather than designing a lock, observation or synchronization policy. A synchronization
  redesign stays out of scope (not in C1–C6; D33).
- Applies now to A12 Scope 4 fresh-reset (preflight, cleanup, create) and A12 Scope 3 state-lock holds.
  Debt (extends D33): inherited Git inside `Modify` at these sites. Payback trigger unchanged.
- Kind: constraint interpretation. Revert: the human widens scope or overrides D33.

### D50 — A9 refusal barrier observes the post-decision watcher; stub records are written atomically (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T08:25Z. `deferred-validation-2-1-correction-plan-code-0` round 1 was rejected with two blockers
and blocked at 08:23Z.
(1) In `review_test.go` (I-08 refusal), the D46 barrier observes the state-lock owner record from
`reviewerStrategy.WaitForWork`, which is taken before the eligibility decision. A delayed wrong admission could be
pre-empted by `stop` and go unnoticed. The starting binary exposes no portable post-decision trace: the "no work" log
needs `DiagnosticLogging`, and enabling it would edit a fixture and golden. The one post-decision effect is the state
watcher that `waitForWorkEventDriven` creates (`waitforwork.go`). It is created only after `checkWork` refuses, and a
wrongly admitted claim returns before it exists.
(2) Under the operator-required `go test -race -count=3 ./internal/oracle/a9/...`, `TestOracleA9CheckpointSummary/i15-pending`
fails with "A9 collect: invocation record". The stub's `writeJSON` (`stub_test.go`) truncates and writes in place,
while `scenario.records` (`kit_test.go`) reads every `call-*.json` with no tolerance, so it can read a torn file.
- Ruling (1): on Linux, after the existing barrier, await the supervisor's inotify instance:
  `/proc/<supervisor pid>/fd/*` → `anon_inode:inotify`, deadline-bounded with no `time.Sleep`. It is a
  post-decision observable, and a wrong admission makes the await fail rather than pass. Other platforms keep the
  entry barrier plus a comment naming the residual gap. The eligibility logic is platform-independent, so Linux
  runs carry the regression coverage. Put the platform helper in the existing `reap_linux_test.go` and
  `reap_other_test.go`. If the supervisor already holds an inotify fd before the decision, report this and stop.
- Ruling (2): `writeJSON` writes atomically: a temp file in the same directory whose name `call-*.json` does not
  match, then `os.Rename`. This is a determinism-only oracle edit (goal Test Policy exception): no asserted value,
  fixture or golden changes. A retry or tolerance in `records()` is rejected because it would hide a torn record.
- Scope: the D47 exception plus `internal/oracle/a9/stub_test.go`. Unchanged: assertions, scenarios, fixtures,
  goldens, `maxSleepCallsInTests` (16), production code. Closure: the canonical batch plus
  `go test -race -count=3 ./internal/oracle/a9/...` pass.
- Rejected: restoring a 1 s dwell (sleep budget, D46); enabling `DiagnosticLogging` (fixture and golden edit);
  accepting the entry barrier everywhere (coverage gap shown by the reviewer).
- Kind: oracle test mechanics (D46–D48 family). Revert: drop the Linux await and keep the entry barrier.

### D51 — Started successor `arm-1-ar-10-cp-1-code-1` runs before the D41 follow-up correction; bounded resubmission for the named flakes (run, 2026-10-24)
Operator, auto-approved, 2026-10-24T12:52Z. `deferred-validation-16-1-d41-correction-plan-amend-1` blocked AWAITING HUMAN under
the started-required-successor rule (A4 Proof and Shared Ownership 149–153; A3 163–166; A2 523–528).
`arm-1-ar-10-cp-1-code-1` (IMPLEMENTING_CODE, claimed 12:12:50Z) has an undeferred proof (index 1:
`go test -race -timeout 30m -p 2 -count=1 ./internal/agent/ ./internal/ops/`) that executes the flaky tests the
correction fixes. Holding it (option B) stalls the A1 critical path until the correction's three children merge.
- Ruling (option A): the task keeps running and is not paused or handed back; the correction is placed after it.
  Task admission has no known-red exemption: `known_red` exists only in deferred validation
  (`internal/ops/deferred_validation.go:1509` `knownRed`), so a failing proof command refuses the submission. Hence
  the bounded rule below. If index 1 fails ONLY on these four named signatures, the coder resubmits without changing code:
  - `internal/agent/inflight_submit_deadline_test.go` `TestExecuteAgentLetsLateSubmitPublishPastSupervisorClocks` (:101)
  - `internal/agent/inflight_submit_deadline_test.go` `TestExecuteAgentBlocksUnadmittedLateSubmitAfterHandback` (:227/:230)
  - `internal/ops/await_verdict_test.go` `TestAwaitVerdict_Rejected_NewAttempt` (TIMEOUT instead of NEW_ATTEMPT)
  - `internal/ops/sprint_checkpoint_test.go` `TestCheckpointPassedPlansRefusesUnqualifiedState/attempted_after_its_pass` (:575)
  Each resubmission records the failing test names and lines.
- Disproof and limit: any failure outside those signatures, or a third consecutive refusal on them, means BLOCK
  with the evidence (the flake may be a regression the refactor introduced). No test edits, no `-run` filter, no
  `-count` change, and no weakening of the proof command.
- Placement: the correction's children wait on `arm-1-ar-10-cp-1-code-1` (started), then take the earliest legal slot
  before the unstarted consumers per the A4/A3/A2 rules. The planner cites D51.
- Rejected: option B (critical-path stall measured in hours for a flake class); a per-task known-red waiver
  (no such admission mechanism; it would need an engine change).
- Kind: delivery/ordering. Revert: switch to option B by handing the task back before its next submission.

### D52 — G8 boundary CI by `workflow_dispatch` on the pushed integration branch; one PR to `main` later (human, 2026-10-24)
Human decision ("go for your recommendation"), 2026-10-24T15:08Z. E1 prescribes a `global-refactoring`→`main` PR at each of the ~9
area boundaries, with macOS on the subsequent `main` push, so `main` would absorb every area as it closes. `ci.yml`
also triggers on `workflow_dispatch`, and `ci-core.yml:172` runs macOS for every event except `pull_request`.
- Ruling: `global-refactoring` is pushed to `origin`. At each area boundary, G8 evidence is a `workflow_dispatch` run of
  `ci.yml` on `global-refactoring`, whose tested SHA equals the boundary record's integration SHA, with green Linux,
  Windows and macOS jobs. The record cites the run URL, the tested SHA and each job's conclusion. Red CI keeps the next area held,
  and the maintainer commissions the fix (E1 unchanged). No PR is opened per boundary. One PR to `main` is opened at a
  point the human chooses; merging it needs the human's explicit approval.
- The human action at each boundary stays a human-action hold. The operator may push and trigger the run under this
  ruling, and the maintainer confirms the result. A boundary record must not claim G8 before the run's jobs complete.
- Publication accepted: `liza-mas/liza` is public, so the branch's commits, including force-added `specs/goals` files,
  become public. A pre-push scan of `origin/main..global-refactoring` (356 commits) found one pattern hit, the labelled
  synthetic key in `internal/oracle/a2/cli/testdata/fixtures/tool-result/redaction.txt`, and no real secret.
- Rejected: a PR plus a `main` merge per boundary (a merge decision every few hours, and `main` absorbs partially consolidated
  areas); deferring CI to the end of the run (platform breaks would compound across areas).
- Kind: proof mechanism (E1 form; N2 coverage kept and widened, since macOS now runs at every boundary). Revert: return to per-boundary PRs.

### D53 — Oracle portability for N2: generic fixes everywhere, process-supervision oracles Linux-only (human, 2026-10-24)
Human decision (option B), 2026-10-24T15:43Z. Early D52 CI run 40105658735 at `1e3b803b7` failed on all three platforms:
(1) Windows checkout fails on 4 A2 golden paths containing `<ID>`/`<EVENT-1>` (`<`/`>` are invalid on Windows; the effect
inventory keys golden files by the normalised path, `internal/oracle/a2/cli/helpers_test.go:181`, `TokenID`
`internal/testhelpers/oracle/normalise.go:24`); (2) Linux and macOS tests that export the starting tree fail with
"invalid reference: 96c9ad69a…", because every `actions/checkout` in `ci-core.yml` (lines 34, 110, 179) and `ci.yml` (93, 236)
uses the default `fetch-depth` (1); (3) macOS: A9 goldens freeze Linux-only wording ("raw state mismatched from procfs" vs
"unknown from signal(0)"); A9 stub record, authority-lost deadline; A4 `repair-superseded-dependencies/success` JSON
differs (extra element).
- Ruling, generic fixes (required on every platform):
  - Golden files are stored under filesystem-safe names on every OS. The harness maps tokens in path components when it stores and
    reads goldens. Golden contents and assertions are unchanged. A guard test rejects tracked paths that are invalid on Windows.
  - Every CI checkout that runs tests fetches the history that the starting commit needs (for example `fetch-depth: 0`).
  - A cross-platform golden difference that is not caused by a Linux process facility is investigated and fixed as a defect or a determinism issue
    (the A4 JSON difference first).
- Ruling, Linux scope: an oracle scenario whose frozen behaviour depends on Linux process facilities (procfs, signal
  semantics, reaping, process supervision; A9 at least) is scoped to Linux with a `//go:build linux` test constraint. The reason is
  stated per file, and the boundary record and oracle docs name the scope. White-box suites keep running on all three
  platforms. Scoping a scenario for any other reason needs a new ruling.
- These are portability edits under the oracle-immutability exceptions: no asserted value or fixture changes, and goldens change only by file name.
- Order: the corrections land before the A1 boundary's G8. The boundary task is then rebased, reruns G1–G7 at the new integration
  SHA, and holds for a D52 dispatched run at that SHA. G8 is green only when Linux, Windows and macOS all pass in one run.
  The pending timing follow-up (`deferred-validation-19-timing-followup-plan`) also lands before that rerun; because the boundary reruns
  its proofs, it does not need the bounded-resubmission rule of D51.
- Rejected: A (port every oracle scenario to Windows and macOS before A2: unbounded Windows work the goal did not ask for); C (no
  oracle tests off Linux: loses the portable signal).
- Kind: proof scope (N2 kept: CI green on all three platforms; the behaviour-preservation proof for process supervision is Linux-only).
  Revert: drop the build constraints and port the scenarios.

### D54 — `make test-race` leaves the deferred-validation batch; full-suite race runs in CI (human, 2026-10-24)
Human decision ("Drop make test-race from deferred validation"), 2026-10-24T15:53Z. The deferred `env -i … make test-race` (sha256
`640b69fd…`) took 19–28 min per batch (8–10 min for `make test`) beside four other lanes, failed in every batch
15–20 only on wall-clock tests that `-race` slows past their deadlines under load, and in 301 stored outputs found no data
race. The Linux CI job runs race on every D52 boundary run.
- Ruling: retire that exact command from the effective deferred set (`omni-ee deferred-validation retire`). A retirement covers
  only deferrals recorded before it (`internal/models/deferred_validation.go` `EffectiveCommands`/`retired`), and a later merge
  of a task deferring the command re-adds it. The operator re-retires it, citing D54, whenever it re-enters the effective set
  (checked each watch round).
- Unchanged: per-task scoped `go test -race` in acceptance contracts (E1), and race in CI at every area boundary.
- Kind: proof scope (local full-suite race replaced by the CI boundary race). Revert: unrule nothing; let the next deferral re-add it.

## Frictions (human, phase 1, 2026-10-23)

- **F0 — In-place growth:** agents extend existing files/functions instead of refactoring as they go
  (from D1). Concrete instance: `be0d0f490` added 36 lines inside `supervisor.go`, already flagged
  for decomposition.
  Refined by human: "agents do rather well when they find clean code. The problem appears when they
  start from a blank page. It's therefore important to clean the architecture before implementing a
  new major change." Consequence: the remedy is a clean base to imitate before the feature run, not a
  regrowth guard. A one-shot consolidation is the intended shape.
- **F1 — Token cost:** "God files have a direct impact on token consumption when agents read them."
- **F2 — Defects:** "Poorly structured code leads to more bugs even though it's difficult to trace
  the causality." Human: "widely accepted engineering wisdom accumulated over decades even though I
  have no evidence on this specific code base. Preventive refactoring is still desirable." Rationale,
  not a success criterion.
- **F3 — Impact assessment:** "The impact assessment of a change is also more difficult when
  unrelated concerns are mixed."

## Problem statement (settled)

Agent-drafted (U1), human-accepted 2026-10-23:

> Liza grew through increments without architectural consolidation: production Go nearly doubled in
> seven weeks, and unrelated concerns are now mixed in large files and long functions. This costs
> tokens when agents read them (F1), makes a change's impact harder to assess (F3), and — by
> long-standing engineering experience, though not evidenced on this codebase — breeds defects (F2).
> Agents keep code clean when they extend clean code; they degrade it when there is no sound
> structure to follow (F0). A major-feature run is about to start and will follow whatever structure
> it finds (D3). The base must therefore be consolidated first: a preventive, behaviour-preserving
> refactoring, preceded by test coverage extended where needed (D2).

"Blank page" read as "no sound structure to follow"; not contested.

## Evidence gathered (agent, 2026-10-23)

Measured with `git ls-files` + `wc -l`, production `cmd/` + `internal/` Go, excluding `_test.go` and
`internal/testhelpers/`. Function lengths not remeasured.

| | 2026-09-06 (`73a50ca8`) | 2026-10-23 |
|---|--:|--:|
| Production Go files | 312 (assessment) / 307 (same filter) | 558 |
| `internal/ops` LOC | 21,873 | 47,225 |
| Files > 500 LOC | 43 | 69 |
| `watch.go` / `proceed.go` | 1,407 / 1,563 | 2,084 / 2,052 |

Function lengths, 2026-10-23 (go/parser `FuncDecl` with body, declaration start to closing brace,
same definition as the assessment; 4,705 functions): >50: 384 · >80: 157 · >100: 94 · >150: 44
(24 on 2026-09-06) · >200: 23 · >300: 11. Longest: `prepareSubmitForReview` 562
(`ops/submit_review.go:93`), `mergeWorktree` 554, `RunSupervisor` 521, `InitCommandWithConfig` 471,
`submitVerdict` 405, `completeClaimTaskAfterValidation` 402.

File sizes, 2026-10-23: >500: 69 · >600: 55 · >700: 40 · >800: 33 · >900: 24 · >1000: 22.

Registry drift found 2026-10-23 (registry not updated; lifecycle authority stays there):
`internal/agent` direct state writes are down to `registration.go` and `checkpoint_summary.go`
(registry says 13 sites); `ProviderKey` is now read for Claude-specific checks
(`session_preflight.go:65`, `cli_agent.go:497`) though registration still stores the raw CLI name;
`ops/deferred_validation.go:1038` runs raw `git` but with `gitenv.Env()` and a timeout.

400 commits since the assessment. The three direction docs conclude "incremental correction, not a
rewrite"; that conclusion predates this growth.

## Still open

- None for authoring. Pre-run: in-flight work on `main` lands or stops; goal document committed
  (force-add; `specs/goals/` gitignored locally).

## Unclaimed markers

- U1 — Problem statement: resolved (accepted 2026-10-23).
- U2 — Behaviour-change scope rule: resolved (D6).
- U3 — Forced-choice list: resolved (D7).
- U4 — Frozen surfaces: resolved (D8).
- U5 — Structural constraints: resolved (D9).
- U6 — Scope and docs: resolved (D10).
- U7 — Test policy: resolved (D11).
- U8 — Concurrent changes: resolved (D12).
- U9 — NFRs: resolved (D13).
- U10 — Proof environment: resolved (D14).
- U11 — Size criteria: resolved (D15).
- U12 — D7 acceptance criteria: resolved (D16).
- U13 — Phase 2 revisions: resolved (D17).
- U14 — Proof cost: resolved (D18).

- U16 — K7: resolved (D20).
- U17 — Artifact proof: resolved (D20).
- U19 — Ship as is: resolved (D22).
- U15 — Draft-only content (Failure Modes "stop early → remove shims or revert the unfinished
  area's tasks", "unlisted behaviour change → escalated to the human", Structural Constraints "Why it
  binds" column): resolved, human-accepted with the full draft 2026-10-23. Sections 1–6, including
  Actors and the dated-evidence header line, accepted earlier the same day.

## Pre-run notes

- `specs/goals/` is ignored by `.local_gitignore:22`; the goal document must be committed before
  `init --spec`. Human: "I'll manage the commit of the goal" (2026-10-23).

## Next contribution only the human can make

Before `init --spec`: settle in-flight work on `main`, cut `refactor/architecture-consolidation`,
commit the goal document (force-add), then run init with `--entry-point functional-spec`.
