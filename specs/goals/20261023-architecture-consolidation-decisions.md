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
